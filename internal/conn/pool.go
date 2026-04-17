// Package conn provides connection management for OpenLoadBalancer.
package conn

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Pool is a pool of reusable connections to a backend.
type Pool struct {
	mu sync.Mutex

	// BackendID is the ID of the backend this pool connects to.
	backendID string

	// Address is the network address of the backend.
	address string

	// MaxSize is the maximum number of idle connections to maintain.
	maxSize int

	// MaxLifetime is the maximum time a connection can be reused.
	maxLifetime time.Duration

	// IdleTimeout is the maximum time a connection can be idle.
	idleTimeout time.Duration

	// DialTimeout is the timeout for establishing new connections.
	dialTimeout time.Duration

	// Connections
	idle   []*PooledConn
	active int
	closed bool

	// Idle eviction
	stopCh chan struct{}
	wg     sync.WaitGroup // tracks eviction goroutine for clean shutdown

	// Statistics
	hits      int64
	misses    int64
	evictions int64
}

// PoolConfig contains configuration for a connection pool.
type PoolConfig struct {
	BackendID   string
	Address     string
	MaxSize     int
	MaxLifetime time.Duration
	IdleTimeout time.Duration
	DialTimeout time.Duration
}

// DefaultPoolConfig returns a default pool configuration.
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxSize:     10,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	}
}

// NewPool creates a new connection pool.
func NewPool(config *PoolConfig) *Pool {
	if config == nil {
		config = DefaultPoolConfig()
	}

	p := &Pool{
		backendID:   config.BackendID,
		address:     config.Address,
		maxSize:     config.MaxSize,
		maxLifetime: config.MaxLifetime,
		idleTimeout: config.IdleTimeout,
		dialTimeout: config.DialTimeout,
		idle:        make([]*PooledConn, 0, config.MaxSize),
		stopCh:      make(chan struct{}),
	}

	// Start idle connection eviction goroutine
	p.wg.Add(1)
	go p.evictIdle()

	return p
}

// evictIdle periodically removes expired idle connections.
func (p *Pool) evictIdle() {
	defer p.wg.Done()
	interval := p.idleTimeout / 2
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > 5*time.Minute {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				return
			}
			remaining := make([]*PooledConn, 0, len(p.idle))
			evicted := 0
			for _, conn := range p.idle {
				if conn.isExpired(p.maxLifetime, p.idleTimeout) {
					conn.Conn.Close()
					evicted++
				} else {
					remaining = append(remaining, conn)
				}
			}
			p.evictions += int64(evicted)
			p.idle = remaining
			p.mu.Unlock()
		}
	}
}

// Get gets a connection from the pool.
// If no idle connection is available, a new one is created.
func (p *Pool) Get(ctx context.Context) (net.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, errors.New("pool is closed")
	}

	// Try to get an idle connection
	for len(p.idle) > 0 {
		conn := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]

		// Check if connection is still valid
		if conn.isExpired(p.maxLifetime, p.idleTimeout) {
			// Close underlying connection directly
			conn.Conn.Close()
			continue
		}

		p.hits++
		p.active++
		conn.pool = p
		return conn, nil
	}

	// No idle connection available, create a new one.
	// We must unlock for the blocking dial, but snapshot the closed state
	// to avoid using stale values after re-acquiring the lock.
	p.mu.Unlock()
	rawConn, err := p.dial(ctx)
	p.mu.Lock()

	if err != nil {
		p.misses++
		return nil, err
	}

	// Re-check: pool may have been closed while we were dialing.
	if p.closed {
		p.mu.Unlock()
		rawConn.Close()
		p.misses++
		return nil, errors.New("pool is closed")
	}

	p.misses++
	p.active++
	now := time.Now()
	conn := &PooledConn{
		Conn:      rawConn,
		pool:      p,
		createdAt: now,
		lastUsed:  now,
	}
	return conn, nil
}

// Put returns a connection to the pool.
// If the pool is full or the connection is expired, it is closed.
func (p *Pool) Put(conn net.Conn) {
	pc, ok := conn.(*PooledConn)
	if !ok {
		// Not a pooled connection, just close it
		conn.Close()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.active--

	if p.closed || pc.isExpired(p.maxLifetime, p.idleTimeout) || len(p.idle) >= p.maxSize {
		// Close underlying connection directly to avoid recursive Put
		pc.Conn.Close()
		return
	}

	pc.lastUsed = time.Now()
	pc.pool = p
	p.idle = append(p.idle, pc)
}

// dial creates a new connection to the backend.
func (p *Pool) dial(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: p.dialTimeout,
	}
	return dialer.DialContext(ctx, "tcp", p.address)
}

// Close closes all connections in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		return nil
	}

	p.closed = true

	// Stop eviction goroutine
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}

	// Close all idle connections
	for _, conn := range p.idle {
		conn.Conn.Close()
	}
	p.idle = p.idle[:0]
	p.mu.Unlock()

	// Wait for eviction goroutine to finish
	p.wg.Wait()

	return nil
}

// Stats returns pool statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	return PoolStats{
		BackendID: p.backendID,
		Address:   p.address,
		Idle:      len(p.idle),
		Active:    p.active,
		MaxSize:   p.maxSize,
		Hits:      p.hits,
		Misses:    p.misses,
		Evictions: p.evictions,
	}
}

// PoolStats contains pool statistics.
type PoolStats struct {
	BackendID string
	Address   string
	Idle      int
	Active    int
	MaxSize   int
	Hits      int64
	Misses    int64
	Evictions int64
}

// PooledConn is a connection that can be returned to a pool.
type PooledConn struct {
	net.Conn
	pool      *Pool
	createdAt time.Time
	lastUsed  time.Time
	closeOnce atomic.Bool
}

// isExpired returns true if the connection has exceeded max lifetime or idle timeout.
func (c *PooledConn) isExpired(maxLifetime, idleTimeout time.Duration) bool {
	if maxLifetime > 0 && time.Since(c.createdAt) > maxLifetime {
		return true
	}
	if idleTimeout > 0 && time.Since(c.lastUsed) > idleTimeout {
		return true
	}
	return false
}

// Close returns the connection to the pool or closes it.
// Safe for concurrent use — only the first call takes effect.
func (c *PooledConn) Close() error {
	if !c.closeOnce.CompareAndSwap(false, true) {
		return nil
	}
	if c.pool != nil {
		c.pool.Put(c)
		return nil
	}
	return c.Conn.Close()
}

// PoolManager manages connection pools for multiple backends.
type PoolManager struct {
	mu     sync.RWMutex
	pools  map[string]*Pool
	config *PoolConfig
}

// NewPoolManager creates a new pool manager.
func NewPoolManager(config *PoolConfig) *PoolManager {
	return &PoolManager{
		pools:  make(map[string]*Pool),
		config: config,
	}
}

// GetPool gets or creates a pool for a backend.
func (pm *PoolManager) GetPool(backendID, address string) *Pool {
	pm.mu.RLock()
	pool, exists := pm.pools[backendID]
	pm.mu.RUnlock()

	if exists {
		return pool
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := pm.pools[backendID]; exists {
		return pool
	}

	config := pm.config
	if config == nil {
		config = DefaultPoolConfig()
	}

	pool = NewPool(&PoolConfig{
		BackendID:   backendID,
		Address:     address,
		MaxSize:     config.MaxSize,
		MaxLifetime: config.MaxLifetime,
		IdleTimeout: config.IdleTimeout,
		DialTimeout: config.DialTimeout,
	})
	pm.pools[backendID] = pool

	return pool
}

// RemovePool removes a pool for a backend.
func (pm *PoolManager) RemovePool(backendID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pool, exists := pm.pools[backendID]
	if !exists {
		return errors.New("pool not found")
	}

	delete(pm.pools, backendID)
	return pool.Close()
}

// Close closes all pools.
func (pm *PoolManager) Close() {
	pm.mu.Lock()
	pools := make([]*Pool, 0, len(pm.pools))
	for _, pool := range pm.pools {
		pools = append(pools, pool)
	}
	pm.pools = make(map[string]*Pool)
	pm.mu.Unlock()

	for _, pool := range pools {
		pool.Close()
	}
}

// Stats returns statistics for all pools.
func (pm *PoolManager) Stats() map[string]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]PoolStats, len(pm.pools))
	for id, pool := range pm.pools {
		stats[id] = pool.Stats()
	}
	return stats
}
