// Package conn provides connection management for OpenLoadBalancer.
package conn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// TrackedConn wraps a net.Conn with metadata for tracking.
type TrackedConn struct {
	net.Conn
	id         string
	createdAt  time.Time
	remoteAddr string
	localAddr  string
	backendID  string
	bytesIn    atomic.Int64
	bytesOut   atomic.Int64
	closed     atomic.Bool
	onClose    func()
}

// NewTrackedConn creates a new tracked connection.
func NewTrackedConn(conn net.Conn, id string, onClose func()) *TrackedConn {
	return &TrackedConn{
		Conn:       conn,
		id:         id,
		createdAt:  time.Now(),
		remoteAddr: conn.RemoteAddr().String(),
		localAddr:  conn.LocalAddr().String(),
		onClose:    onClose,
	}
}

// ID returns the connection ID.
func (c *TrackedConn) ID() string {
	return c.id
}

// CreatedAt returns when the connection was created.
func (c *TrackedConn) CreatedAt() time.Time {
	return c.createdAt
}

// BackendID returns the associated backend ID (if any).
func (c *TrackedConn) BackendID() string {
	return c.backendID
}

// SetBackendID sets the associated backend ID.
func (c *TrackedConn) SetBackendID(id string) {
	c.backendID = id
}

// BytesIn returns the number of bytes received.
func (c *TrackedConn) BytesIn() int64 {
	return c.bytesIn.Load()
}

// BytesOut returns the number of bytes sent.
func (c *TrackedConn) BytesOut() int64 {
	return c.bytesOut.Load()
}

// IsClosed returns true if the connection is closed.
func (c *TrackedConn) IsClosed() bool {
	return c.closed.Load()
}

// Read implements net.Conn.Read with byte counting.
func (c *TrackedConn) Read(p []byte) (n int, err error) {
	n, err = c.Conn.Read(p)
	if n > 0 {
		c.bytesIn.Add(int64(n))
	}
	return n, err
}

// Write implements net.Conn.Write with byte counting.
func (c *TrackedConn) Write(p []byte) (n int, err error) {
	n, err = c.Conn.Write(p)
	if n > 0 {
		c.bytesOut.Add(int64(n))
	}
	return n, err
}

// Close implements net.Conn.Close.
func (c *TrackedConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.onClose != nil {
			c.onClose()
		}
		return c.Conn.Close()
	}
	return nil
}

// Stats returns connection statistics.
func (c *TrackedConn) Stats() Stats {
	return Stats{
		ID:         c.id,
		RemoteAddr: c.remoteAddr,
		LocalAddr:  c.localAddr,
		BackendID:  c.backendID,
		CreatedAt:  c.createdAt,
		BytesIn:    c.bytesIn.Load(),
		BytesOut:   c.bytesOut.Load(),
		Duration:   time.Since(c.createdAt),
	}
}

// Stats contains connection statistics.
type Stats struct {
	ID         string
	RemoteAddr string
	LocalAddr  string
	BackendID  string
	CreatedAt  time.Time
	BytesIn    int64
	BytesOut   int64
	Duration   time.Duration
}

// Manager manages all connections with limits and tracking.
type Manager struct {
	mu sync.RWMutex

	// Configuration
	maxConnections int
	maxPerSource   int
	maxPerBackend  int
	drainTimeout   time.Duration

	// Connection tracking
	connections   map[string]*TrackedConn
	sourceCounts  map[string]int
	backendCounts map[string]int
	totalCount    atomic.Int64

	// Connection ID generator
	idCounter atomic.Uint64
}

// Config contains configuration for the connection manager.
type Config struct {
	// MaxConnections is the global maximum number of connections.
	// 0 means unlimited.
	MaxConnections int

	// MaxPerSource is the maximum number of connections per source IP.
	// 0 means unlimited.
	MaxPerSource int

	// MaxPerBackend is the maximum number of connections per backend.
	// 0 means unlimited.
	MaxPerBackend int

	// DrainTimeout is the maximum time to wait for connections to close.
	DrainTimeout time.Duration
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxConnections: 10000,
		MaxPerSource:   100,
		MaxPerBackend:  1000,
		DrainTimeout:   30 * time.Second,
	}
}

// NewManager creates a new connection manager.
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	return &Manager{
		maxConnections: config.MaxConnections,
		maxPerSource:   config.MaxPerSource,
		maxPerBackend:  config.MaxPerBackend,
		drainTimeout:   config.DrainTimeout,
		connections:    make(map[string]*TrackedConn),
		sourceCounts:   make(map[string]int),
		backendCounts:  make(map[string]int),
	}
}

// Accept wraps a connection with tracking.
// Returns an error if connection limits are exceeded.
func (m *Manager) Accept(conn net.Conn) (*TrackedConn, error) {
	remoteAddr := conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check global limit
	if m.maxConnections > 0 && len(m.connections) >= m.maxConnections {
		_ = conn.Close() // best-effort cleanup
		return nil, errors.New("global connection limit exceeded")
	}

	// Check per-source limit
	if m.maxPerSource > 0 && m.sourceCounts[host] >= m.maxPerSource {
		_ = conn.Close() // best-effort cleanup
		return nil, errors.New("per-source connection limit exceeded")
	}

	// Generate connection ID
	id := m.nextID()

	// Create tracked connection
	tracked := NewTrackedConn(conn, id, func() {
		m.release(id, host)
	})

	// Track the connection
	m.connections[id] = tracked
	m.sourceCounts[host]++
	m.totalCount.Add(1)

	return tracked, nil
}

// release removes a connection from tracking.
func (m *Manager) release(id string, sourceHost string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.connections[id]; exists {
		delete(m.connections, id)
		m.sourceCounts[sourceHost]--
		if m.sourceCounts[sourceHost] <= 0 {
			delete(m.sourceCounts, sourceHost)
		}

		// Decrement backend count if set
		backendID := ""
		for _, conn := range m.connections {
			if conn.id == id {
				backendID = conn.backendID
				break
			}
		}
		if backendID != "" {
			m.backendCounts[backendID]--
			if m.backendCounts[backendID] <= 0 {
				delete(m.backendCounts, backendID)
			}
		}

		m.totalCount.Add(-1)
	}
}

// AssociateBackend associates a connection with a backend.
func (m *Manager) AssociateBackend(connID string, backendID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.connections[connID]
	if !exists {
		return errors.New("connection not found")
	}

	// Check per-backend limit
	if m.maxPerBackend > 0 && m.backendCounts[backendID] >= m.maxPerBackend {
		return errors.New("per-backend connection limit exceeded")
	}

	// Update old backend count if set
	oldBackendID := conn.backendID
	if oldBackendID != "" {
		m.backendCounts[oldBackendID]--
		if m.backendCounts[oldBackendID] <= 0 {
			delete(m.backendCounts, oldBackendID)
		}
	}

	// Set new backend
	conn.SetBackendID(backendID)
	m.backendCounts[backendID]++

	return nil
}

// nextID generates a unique connection ID.
func (m *Manager) nextID() string {
	return fmt.Sprintf("conn-%d-%d", time.Now().UnixNano(), m.idCounter.Add(1))
}

// ActiveConnections returns a snapshot of all active connections.
func (m *Manager) ActiveConnections() []*Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Stats, 0, len(m.connections))
	for _, conn := range m.connections {
		stats := conn.Stats()
		result = append(result, &stats)
	}
	return result
}

// ActiveCount returns the number of active connections.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}

// TotalCount returns the total number of connections handled.
func (m *Manager) TotalCount() int64 {
	return m.totalCount.Load()
}

// SourceCount returns the number of connections from a source.
func (m *Manager) SourceCount(sourceHost string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sourceCounts[sourceHost]
}

// BackendCount returns the number of connections to a backend.
func (m *Manager) BackendCount(backendID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backendCounts[backendID]
}

// Drain waits for all connections to close or timeout.
func (m *Manager) Drain(ctx context.Context) error {
	timeout := m.drainTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("drain timeout")
		case <-ticker.C:
			if m.ActiveCount() == 0 {
				return nil
			}
		}
	}
}

// CloseAll forcibly closes all active connections.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	conns := make([]*TrackedConn, 0, len(m.connections))
	for _, conn := range m.connections {
		conns = append(conns, conn)
	}
	m.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close() // best-effort cleanup
	}
}

// GetConnection returns a connection by ID.
func (m *Manager) GetConnection(id string) *TrackedConn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[id]
}
