// Package l4 implements Layer 4 (TCP/UDP) proxying.
package l4

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// UDPProxyConfig configures UDP proxy behavior.
type UDPProxyConfig struct {
	// ListenAddr is the address to listen on (e.g., ":53", "0.0.0.0:5353").
	ListenAddr string

	// BackendPool is the name of the backend pool to use.
	BackendPool string

	// SessionTimeout is the maximum lifetime of a UDP session.
	// After this duration, the session is removed regardless of activity.
	SessionTimeout time.Duration

	// MaxSessions is the maximum number of concurrent UDP sessions.
	// When this limit is reached, new sessions are rejected.
	MaxSessions int

	// BufferSize is the size of the datagram read buffer in bytes.
	BufferSize int

	// IdleTimeout is the timeout for idle sessions with no activity.
	// Sessions with no packets forwarded within this duration are cleaned up.
	IdleTimeout time.Duration

	// CleanupInterval is how often the session cleanup goroutine runs.
	// Defaults to half the IdleTimeout.
	CleanupInterval time.Duration
}

// DefaultUDPProxyConfig returns a default UDP proxy configuration.
func DefaultUDPProxyConfig() *UDPProxyConfig {
	return &UDPProxyConfig{
		ListenAddr:      ":0",
		BackendPool:     "",
		SessionTimeout:  30 * time.Second,
		MaxSessions:     10000,
		BufferSize:      65535,
		IdleTimeout:     60 * time.Second,
		CleanupInterval: 30 * time.Second,
	}
}

// UDPSession tracks a client-to-backend mapping for UDP proxying.
// Each unique client address gets its own session with a dedicated
// backend connection for receiving replies.
type UDPSession struct {
	// clientAddr is the original client's UDP address.
	clientAddr *net.UDPAddr

	// backendAddr is the selected backend's UDP address.
	backendAddr *net.UDPAddr

	// backendConn is the per-session connection to the backend.
	// Used to receive replies from the backend and route them back
	// to the correct client.
	backendConn *net.UDPConn

	// backend is the selected backend for stats tracking.
	backend *backend.Backend

	// lastActivity is the time of last packet forwarded (either direction).
	lastActivity atomic.Value // time.Time

	// created is the time the session was created.
	created time.Time

	// packetsIn counts packets from client to backend.
	packetsIn atomic.Int64

	// packetsOut counts packets from backend to client.
	packetsOut atomic.Int64

	// bytesIn counts bytes from client to backend.
	bytesIn atomic.Int64

	// bytesOut counts bytes from backend to client.
	bytesOut atomic.Int64

	// closed marks whether the session has been closed.
	closed atomic.Bool
}

// newUDPSession creates a new UDP session.
func newUDPSession(clientAddr, backendAddr *net.UDPAddr, backendConn *net.UDPConn, b *backend.Backend) *UDPSession {
	s := &UDPSession{
		clientAddr:  clientAddr,
		backendAddr: backendAddr,
		backendConn: backendConn,
		backend:     b,
		created:     time.Now(),
	}
	s.lastActivity.Store(time.Now())
	return s
}

// touch updates the last activity time.
func (s *UDPSession) touch() {
	s.lastActivity.Store(time.Now())
}

// LastActivity returns the time of last activity.
func (s *UDPSession) LastActivity() time.Time {
	v := s.lastActivity.Load()
	if v == nil {
		return s.created
	}
	if t, ok := v.(time.Time); ok {
		return t
	}
	return s.created
}

// close closes the session and its backend connection.
func (s *UDPSession) close() {
	if s.closed.CompareAndSwap(false, true) {
		if s.backendConn != nil {
			s.backendConn.Close()
		}
		if s.backend != nil {
			s.backend.ReleaseConn()
		}
	}
}

// UDPProxyStats holds runtime statistics for the UDP proxy.
type UDPProxyStats struct {
	// PacketsForwarded is the total number of packets forwarded (both directions).
	PacketsForwarded int64

	// BytesForwarded is the total bytes forwarded (both directions).
	BytesForwarded int64

	// ActiveSessions is the number of currently active sessions.
	ActiveSessions int64

	// TotalSessions is the total number of sessions created.
	TotalSessions int64

	// DroppedPackets is the total number of packets dropped (e.g., max sessions reached).
	DroppedPackets int64
}

// UDPProxy implements a Layer 4 UDP proxy.
// It listens on a UDP address, maps incoming client datagrams to
// backend servers via session tracking, and forwards replies back
// to the correct clients.
type UDPProxy struct {
	config   *UDPProxyConfig
	balancer Balancer
	pool     *backend.Pool

	// listenerConn is the main listening UDP connection.
	listenerConn *net.UDPConn

	// sessions maps client address strings to their sessions.
	sessions map[string]*UDPSession
	mu       sync.RWMutex

	// Stats
	packetsForwarded atomic.Int64
	bytesForwarded   atomic.Int64
	totalSessions    atomic.Int64
	droppedPackets   atomic.Int64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// running indicates whether the proxy is currently active.
	running atomic.Bool
}

// NewUDPProxy creates a new UDP proxy.
func NewUDPProxy(pool *backend.Pool, balancer Balancer, config *UDPProxyConfig) *UDPProxy {
	if config == nil {
		config = DefaultUDPProxyConfig()
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = config.IdleTimeout / 2
		if config.CleanupInterval < time.Second {
			config.CleanupInterval = time.Second
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &UDPProxy{
		config:   config,
		balancer: balancer,
		pool:     pool,
		sessions: make(map[string]*UDPSession),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start binds to the listen address and starts the receive loop and cleanup goroutine.
func (p *UDPProxy) Start() error {
	if p.running.Load() {
		return errors.New("udp proxy already running")
	}

	addr, err := net.ResolveUDPAddr("udp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve listen address %s: %w", p.config.ListenAddr, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.config.ListenAddr, err)
	}

	p.listenerConn = conn
	p.running.Store(true)

	// Start the receive loop
	p.wg.Add(1)
	go p.receiveLoop()

	// Start session cleanup
	p.wg.Add(1)
	go p.sessionCleanup()

	return nil
}

// Stop gracefully stops the UDP proxy, closing all sessions and the listener.
func (p *UDPProxy) Stop() error {
	if !p.running.CompareAndSwap(true, false) {
		return errors.New("udp proxy not running")
	}

	// Signal all goroutines to stop
	p.cancel()

	// Close the listener to unblock the receive loop
	if p.listenerConn != nil {
		p.listenerConn.Close()
	}

	// Close all sessions
	p.mu.Lock()
	for key, session := range p.sessions {
		session.close()
		delete(p.sessions, key)
	}
	p.mu.Unlock()

	// Wait for goroutines to finish
	p.wg.Wait()

	return nil
}

// receiveLoop reads datagrams from the listener and forwards them to backends.
func (p *UDPProxy) receiveLoop() {
	defer p.wg.Done()

	buf := make([]byte, p.config.BufferSize)

	for {
		// Check if we're shutting down
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		// Set a read deadline so we periodically check ctx
		p.listenerConn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, clientAddr, err := p.listenerConn.ReadFromUDP(buf)
		if err != nil {
			if !p.running.Load() {
				return
			}
			// Check for timeout - just continue to re-check ctx
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Other errors - stop if not running
			if p.ctx.Err() != nil {
				return
			}
			slog.Error("backend read error", "error", err)
			return
		}

		if n == 0 {
			continue
		}

		// Copy the data to avoid buffer reuse issues
		data := make([]byte, n)
		copy(data, buf[:n])

		// Process the datagram
		p.handleDatagram(clientAddr, data)
	}
}

// handleDatagram processes a single received datagram.
func (p *UDPProxy) handleDatagram(clientAddr *net.UDPAddr, data []byte) {
	key := clientAddr.String()

	// Try to find an existing session
	p.mu.RLock()
	session, exists := p.sessions[key]
	p.mu.RUnlock()

	if exists && !session.closed.Load() {
		// Forward to existing backend
		p.forwardToBackend(session, data)
		return
	}

	// Create a new session
	session, err := p.createSession(clientAddr)
	if err != nil {
		p.droppedPackets.Add(1)
		return
	}

	// Forward the datagram
	p.forwardToBackend(session, data)
}

// createSession creates a new session for a client address.
func (p *UDPProxy) createSession(clientAddr *net.UDPAddr) (*UDPSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := clientAddr.String()

	// Double-check if session was created by another goroutine
	if session, exists := p.sessions[key]; exists && !session.closed.Load() {
		return session, nil
	}

	// Check max sessions - evict the least recently used session to make room.
	// This prevents an attacker from spoofing source addresses to exhaust the
	// session table and deny service to legitimate clients (CWE-770).
	if p.config.MaxSessions > 0 && len(p.sessions) >= p.config.MaxSessions {
		var oldestKey string
		var oldestTime time.Time
		for k, s := range p.sessions {
			if s.closed.Load() {
				// Prefer evicting already-closed sessions
				oldestKey = k
				break
			}
			la := s.LastActivity()
			if oldestKey == "" || la.Before(oldestTime) {
				oldestKey = k
				oldestTime = la
			}
		}
		if oldestKey != "" {
			if old, ok := p.sessions[oldestKey]; ok {
				old.close()
				delete(p.sessions, oldestKey)
				slog.Debug("udp proxy: evicted oldest session to make room", "evicted_addr", oldestKey)
			}
		}
	}

	// Get healthy backends
	backends := p.pool.GetHealthyBackends()
	if len(backends) == 0 {
		return nil, errors.New("no healthy backends available")
	}

	// Select a backend
	selected := p.balancer.Next(nil, backends)
	backend.ReleaseHealthyBackends(backends)
	if selected == nil {
		return nil, errors.New("balancer returned no backend")
	}

	// Acquire a connection slot on the backend
	if !selected.AcquireConn() {
		return nil, fmt.Errorf("backend %s at max connections", selected.ID)
	}

	// Resolve backend address
	backendAddr, err := net.ResolveUDPAddr("udp", selected.Address)
	if err != nil {
		selected.ReleaseConn()
		return nil, fmt.Errorf("failed to resolve backend address %s: %w", selected.Address, err)
	}

	// Create a per-session UDP connection to the backend.
	// Binding to :0 gives us a unique local port so we can receive
	// replies from this specific backend.
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		selected.ReleaseConn()
		return nil, fmt.Errorf("failed to dial backend %s: %w", selected.Address, err)
	}

	session := newUDPSession(clientAddr, backendAddr, backendConn, selected)
	p.sessions[key] = session
	p.totalSessions.Add(1)

	// Start a goroutine to receive replies from this backend
	p.wg.Add(1)
	go p.receiveFromBackend(session)

	return session, nil
}

// forwardToBackend sends a datagram from a client to the selected backend.
func (p *UDPProxy) forwardToBackend(session *UDPSession, data []byte) {
	if session.closed.Load() {
		return
	}

	_, err := session.backendConn.Write(data)
	if err != nil {
		if session.backend != nil {
			session.backend.RecordError()
		}
		slog.Debug("udp proxy: write to backend failed", "backend", session.backendAddr.String(), "error", err)
		return
	}

	session.touch()
	session.packetsIn.Add(1)
	session.bytesIn.Add(int64(len(data)))
	p.packetsForwarded.Add(1)
	p.bytesForwarded.Add(int64(len(data)))
}

// receiveFromBackend reads replies from a backend and forwards them to the client.
func (p *UDPProxy) receiveFromBackend(session *UDPSession) {
	defer p.wg.Done()

	buf := make([]byte, p.config.BufferSize)

	for {
		// Check if we're shutting down or session is closed
		if session.closed.Load() {
			return
		}

		select {
		case <-p.ctx.Done():
			return
		default:
		}

		// Set a read deadline to periodically check for shutdown
		session.backendConn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, err := session.backendConn.Read(buf)
		if err != nil {
			if session.closed.Load() {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if p.ctx.Err() != nil {
				return
			}
			continue
		}

		if n == 0 {
			continue
		}

		// Forward reply to client via the listener connection
		if !p.running.Load() {
			return
		}

		_, err = p.listenerConn.WriteToUDP(buf[:n], session.clientAddr)
		if err != nil {
			if !p.running.Load() {
				return
			}
			continue
		}

		session.touch()
		session.packetsOut.Add(1)
		session.bytesOut.Add(int64(n))
		p.packetsForwarded.Add(1)
		p.bytesForwarded.Add(int64(n))

		// Record stats on the backend
		if session.backend != nil {
			session.backend.RecordRequest(0, int64(n))
		}
	}
}

// sessionCleanup periodically removes expired sessions.
func (p *UDPProxy) sessionCleanup() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.cleanExpiredSessions()
		}
	}
}

// cleanExpiredSessions removes sessions that have exceeded their idle or session timeout.
func (p *UDPProxy) cleanExpiredSessions() {
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	for key, session := range p.sessions {
		lastActive := session.LastActivity()
		idle := now.Sub(lastActive)
		age := now.Sub(session.created)

		// Remove if idle too long or session lifetime exceeded
		if idle > p.config.IdleTimeout || age > p.config.SessionTimeout {
			session.close()
			delete(p.sessions, key)
		}
	}
}

// Stats returns a snapshot of the UDP proxy statistics.
func (p *UDPProxy) Stats() UDPProxyStats {
	p.mu.RLock()
	activeSessions := int64(len(p.sessions))
	p.mu.RUnlock()

	return UDPProxyStats{
		PacketsForwarded: p.packetsForwarded.Load(),
		BytesForwarded:   p.bytesForwarded.Load(),
		ActiveSessions:   activeSessions,
		TotalSessions:    p.totalSessions.Load(),
		DroppedPackets:   p.droppedPackets.Load(),
	}
}

// ActiveSessions returns the number of currently active sessions.
func (p *UDPProxy) ActiveSessions() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return int64(len(p.sessions))
}

// IsRunning returns true if the proxy is currently running.
func (p *UDPProxy) IsRunning() bool {
	return p.running.Load()
}

// ListenAddr returns the actual address the proxy is listening on.
// This is useful when ListenAddr was ":0" and the OS chose a port.
func (p *UDPProxy) ListenAddr() net.Addr {
	if p.listenerConn != nil {
		return p.listenerConn.LocalAddr()
	}
	return nil
}
