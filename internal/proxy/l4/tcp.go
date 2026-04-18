// Package l4 implements Layer 4 (TCP/UDP) proxying.
package l4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// TCPProxyConfig configures TCP proxy behavior.
type TCPProxyConfig struct {
	// DialTimeout is the timeout for connecting to backends.
	DialTimeout time.Duration

	// IdleTimeout is the timeout for idle connections.
	IdleTimeout time.Duration

	// BufferSize is the size of the copy buffer.
	BufferSize int

	// MaxConnections is the maximum number of concurrent connections (0 = unlimited).
	MaxConnections int

	// EnableTCPKeepalive enables TCP keepalive.
	EnableTCPKeepalive bool

	// TCPKeepalivePeriod is the TCP keepalive period.
	TCPKeepalivePeriod time.Duration
}

// DefaultTCPProxyConfig returns a default TCP proxy configuration.
func DefaultTCPProxyConfig() *TCPProxyConfig {
	return &TCPProxyConfig{
		DialTimeout:        10 * time.Second,
		IdleTimeout:        60 * time.Second,
		BufferSize:         32 * 1024, // 32KB
		MaxConnections:     10000,     // 10K concurrent connections max
		EnableTCPKeepalive: true,
		TCPKeepalivePeriod: 30 * time.Second,
	}
}

// TCPProxy implements a Layer 4 TCP proxy.
type TCPProxy struct {
	config   *TCPProxyConfig
	balancer Balancer
	pool     *backend.Pool

	// Connection tracking
	activeConns atomic.Int64
	connWg      sync.WaitGroup

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// Balancer selects a backend from a pool.
type Balancer interface {
	Next(ctx *backend.RequestContext, backends []*backend.Backend) *backend.Backend
}

// NewTCPProxy creates a new TCP proxy.
func NewTCPProxy(pool *backend.Pool, balancer Balancer, config *TCPProxyConfig) *TCPProxy {
	if config == nil {
		config = DefaultTCPProxyConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &TCPProxy{
		config:   config,
		balancer: balancer,
		pool:     pool,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start starts the TCP proxy (called by listener).
func (p *TCPProxy) Start() error {
	return nil
}

// Stop gracefully stops the TCP proxy.
func (p *TCPProxy) Stop(ctx context.Context) error {
	p.cancel()

	// Wait for connections to close with timeout
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[tcp] panic recovered in drain: %v", r)
			}
		}()
		p.connWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HandleConnection handles a single TCP connection.
func (p *TCPProxy) HandleConnection(clientConn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tcp-proxy] panic recovered in HandleConnection: %v", r)
		}
	}()
	defer clientConn.Close()

	// Check max connections (atomic CAS to prevent TOCTOU overshoot)
	if p.config.MaxConnections > 0 {
		maxConns := int64(p.config.MaxConnections)
		for {
			current := p.activeConns.Load()
			if current >= maxConns {
				return
			}
			if p.activeConns.CompareAndSwap(current, current+1) {
				break
			}
		}
	} else {
		p.activeConns.Add(1)
	}
	defer p.activeConns.Add(-1)

	p.connWg.Add(1)
	defer p.connWg.Done()

	// Check context
	select {
	case <-p.ctx.Done():
		return
	default:
	}

	// Get healthy backends
	backends := p.pool.GetHealthyBackends()
	if len(backends) == 0 {
		return
	}

	// Select backend
	selected := p.balancer.Next(nil, backends)
	backend.ReleaseHealthyBackends(backends)
	if selected == nil {
		return
	}

	// Acquire connection slot
	if !selected.AcquireConn() {
		return
	}
	defer selected.ReleaseConn()

	// Connect to backend
	backendConn, err := p.dialBackend(selected)
	if err != nil {
		selected.RecordError()
		return
	}
	defer backendConn.Close()

	// Proxy the connection
	p.proxyConnections(clientConn, backendConn)
}

// dialBackend connects to a backend.
func (p *TCPProxy) dialBackend(b *backend.Backend) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   p.config.DialTimeout,
		KeepAlive: p.config.TCPKeepalivePeriod,
	}

	conn, err := dialer.Dial("tcp", b.Address)
	if err != nil {
		return nil, err
	}

	// Set TCP keepalive if enabled
	if p.config.EnableTCPKeepalive {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(p.config.TCPKeepalivePeriod)
		}
	}

	return conn, nil
}

// proxyConnections proxies data between client and backend.
func (p *TCPProxy) proxyConnections(clientConn, backendConn net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				// Panic recovered — close connections to prevent leak
				backendConn.Close()
				clientConn.Close()
			}
		}()
		p.copyWithTimeout(backendConn, clientConn)
		// Signal backend that client is done sending
		backendConn.Close()
	}()

	// Backend -> Client
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				// Panic recovered — close connections to prevent leak
				clientConn.Close()
				backendConn.Close()
			}
		}()
		p.copyWithTimeout(clientConn, backendConn)
		// Signal client that backend is done sending
		clientConn.Close()
	}()

	// Wait for both directions to complete
	wg.Wait()
}

// copyWithTimeout copies data with an idle timeout.
// If IdleTimeout is 0, a default of 5 minutes is used to prevent goroutines
// from blocking indefinitely on unresponsive connections.
//
// On Linux, io.CopyBuffer uses splice(2) via net.TCPConn.ReadFrom for
// zero-copy transfer between TCP connections.
func (p *TCPProxy) copyWithTimeout(dst, src net.Conn) error {
	timeout := p.config.IdleTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	buf := make([]byte, p.config.BufferSize)
	if len(buf) == 0 {
		buf = make([]byte, 32*1024)
	}

	for {
		// Check context before each iteration to allow prompt cancellation
		select {
		case <-p.ctx.Done():
			src.SetReadDeadline(time.Now())
			return p.ctx.Err()
		default:
		}

		src.SetReadDeadline(time.Now().Add(timeout))

		n, err := io.CopyBuffer(dst, src, buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if n > 0 {
					continue
				}
				return nil
			}
			return err
		}
		return nil
	}
}

// isNormalCloseError checks if an error is a normal connection close.
func isNormalCloseError(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Check for common close errors
	errStr := err.Error()
	if containsAny(errStr, []string{
		"use of closed network connection",
		"broken pipe",
		"connection reset",
		"connection refused",
	}) {
		return true
	}
	return false
}

func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// TCPListener implements a TCP listener for the proxy.
type TCPListener struct {
	name     string
	address  string
	proxy    *TCPProxy
	listener net.Listener
	running  atomic.Bool
	mu       sync.RWMutex
	startErr error
}

// TCPListenerOptions configures the TCP listener.
type TCPListenerOptions struct {
	Name    string
	Address string
	Proxy   *TCPProxy
}

// NewTCPListener creates a new TCP listener.
func NewTCPListener(opts *TCPListenerOptions) (*TCPListener, error) {
	if opts == nil {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "options are required")
	}
	if opts.Name == "" {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "listener name is required")
	}
	if opts.Address == "" {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "listener address is required")
	}
	if opts.Proxy == nil {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "proxy is required")
	}

	return &TCPListener{
		name:    opts.Name,
		address: opts.Address,
		proxy:   opts.Proxy,
	}, nil
}

// Start begins listening for TCP connections.
func (l *TCPListener) Start() error {
	if l.running.Load() {
		return olbErrors.New(olbErrors.CodeAlreadyExist, "listener already running")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running.Load() {
		return olbErrors.New(olbErrors.CodeAlreadyExist, "listener already running")
	}

	// Create TCP listener
	tcpListener, err := net.Listen("tcp", l.address)
	if err != nil {
		return olbErrors.Wrapf(err, olbErrors.CodeUnavailable, "failed to listen on %s", l.address)
	}

	l.listener = tcpListener
	l.running.Store(true)
	l.startErr = nil

	// Start accepting connections
	go l.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections.
func (l *TCPListener) acceptLoop() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			if l.running.Load() {
				l.mu.Lock()
				l.startErr = err
				l.mu.Unlock()
			}
			return
		}

		// Handle connection in goroutine
		go l.proxy.HandleConnection(conn)
	}
}

// Stop stops the TCP listener.
func (l *TCPListener) Stop(ctx context.Context) error {
	if !l.running.Load() {
		return olbErrors.New(olbErrors.CodeUnavailable, "listener not running")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running.Load() {
		return olbErrors.New(olbErrors.CodeUnavailable, "listener not running")
	}

	l.running.Store(false)

	if l.listener != nil {
		l.listener.Close()
	}

	// Stop the proxy
	return l.proxy.Stop(ctx)
}

// Name returns the listener name.
func (l *TCPListener) Name() string {
	return l.name
}

// Address returns the listener address.
func (l *TCPListener) Address() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.listener != nil {
		return l.listener.Addr().String()
	}
	return l.address
}

// IsRunning returns true if the listener is active.
func (l *TCPListener) IsRunning() bool {
	return l.running.Load()
}

// StartError returns any error that occurred during startup.
func (l *TCPListener) StartError() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.startErr
}

// SimpleBalancer is a simple round-robin balancer for TCP.
type SimpleBalancer struct {
	counter atomic.Uint64
}

// NewSimpleBalancer creates a new simple balancer.
func NewSimpleBalancer() *SimpleBalancer {
	return &SimpleBalancer{}
}

// Next selects the next backend using round-robin.
func (b *SimpleBalancer) Next(ctx *backend.RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	idx := b.counter.Add(1) % uint64(len(backends))
	return backends[idx]
}

// CopyBidirectional copies data bidirectionally between two connections.
// It returns when both directions complete.
func CopyBidirectional(conn1, conn2 net.Conn, idleTimeout time.Duration) (int64, int64, error) {
	var bytes1to2, bytes2to1 int64
	errChan := make(chan error, 2)

	// Conn1 -> Conn2
	go func() {
		defer func() {
			if r := recover(); r != nil {
				conn2.Close()
				conn1.Close()
				errChan <- fmt.Errorf("panic in copy: %v", r)
				return
			}
		}()
		n, err := copyWithBuffer(conn2, conn1, idleTimeout)
		bytes1to2 = n
		errChan <- err
		conn2.Close()
	}()

	// Conn2 -> Conn1
	go func() {
		defer func() {
			if r := recover(); r != nil {
				conn1.Close()
				conn2.Close()
				errChan <- fmt.Errorf("panic in copy: %v", r)
				return
			}
		}()
		n, err := copyWithBuffer(conn1, conn2, idleTimeout)
		bytes2to1 = n
		errChan <- err
		conn1.Close()
	}()

	// Wait for both
	err1 := <-errChan
	err2 := <-errChan

	// Return first error if any
	if err1 != nil && !isNormalCloseError(err1) {
		return bytes1to2, bytes2to1, err1
	}
	if err2 != nil && !isNormalCloseError(err2) {
		return bytes1to2, bytes2to1, err2
	}

	return bytes1to2, bytes2to1, nil
}

// copyWithBuffer copies data with a buffer and idle timeout.
// If idleTimeout is 0, a default of 5 minutes is used to prevent goroutines
// from blocking indefinitely on unresponsive connections.
//
// On Linux, io.CopyBuffer uses splice(2) via net.TCPConn.ReadFrom for
// zero-copy transfer between TCP connections.
func copyWithBuffer(dst, src net.Conn, idleTimeout time.Duration) (int64, error) {
	timeout := idleTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	buf := make([]byte, 32*1024)
	var total int64

	for {
		src.SetReadDeadline(time.Now().Add(timeout))

		n, err := io.CopyBuffer(dst, src, buf)
		total += n
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if n > 0 {
					continue
				}
				return total, nil
			}
			return total, err
		}
		return total, nil
	}
}

// ParseTCPAddress parses a TCP address with optional port.
func ParseTCPAddress(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Try adding default port
		if addr[0] == ':' {
			return net.JoinHostPort("", addr[1:]), nil
		}
		return net.JoinHostPort(addr, "80"), nil
	}
	return net.JoinHostPort(host, port), nil
}

// IsTCPConn checks if a connection is a TCP connection.
func IsTCPConn(conn net.Conn) bool {
	_, ok := conn.(*net.TCPConn)
	return ok
}

// GetTCPConn gets the underlying TCP connection if available.
func GetTCPConn(conn net.Conn) *net.TCPConn {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		return tcpConn
	}
	return nil
}

// SetTCPNoDelay sets the TCP_NODELAY option.
func SetTCPNoDelay(conn net.Conn, noDelay bool) error {
	if tcpConn := GetTCPConn(conn); tcpConn != nil {
		return tcpConn.SetNoDelay(noDelay)
	}
	return errors.New("not a TCP connection")
}

// SetTCPKeepAlive sets TCP keepalive options.
func SetTCPKeepAlive(conn net.Conn, keepAlive bool, period time.Duration) error {
	if tcpConn := GetTCPConn(conn); tcpConn != nil {
		if err := tcpConn.SetKeepAlive(keepAlive); err != nil {
			return err
		}
		if keepAlive {
			return tcpConn.SetKeepAlivePeriod(period)
		}
		return nil
	}
	return errors.New("not a TCP connection")
}
