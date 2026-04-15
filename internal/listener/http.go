package listener

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// HTTPListener implements an HTTP listener with graceful shutdown support.
type HTTPListener struct {
	name     string
	address  string
	handler  http.Handler
	server   *http.Server
	listener net.Listener
	running  atomic.Bool
	mu       sync.RWMutex

	// Configuration
	readTimeout    time.Duration
	writeTimeout   time.Duration
	idleTimeout    time.Duration
	headerTimeout  time.Duration
	maxHeaderBytes int

	// Optional connection manager
	connManager ConnManager

	// Error tracking
	startErr error
}

// NewHTTPListener creates a new HTTP listener with the given options.
func NewHTTPListener(opts *Options) (*HTTPListener, error) {
	opts = mergeOptions(opts)

	if opts.Name == "" {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "listener name is required")
	}

	if opts.Address == "" {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "listener address is required")
	}

	if opts.Handler == nil {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "handler is required")
	}

	return &HTTPListener{
		name:           opts.Name,
		address:        opts.Address,
		handler:        opts.Handler,
		readTimeout:    opts.ReadTimeout,
		writeTimeout:   opts.WriteTimeout,
		idleTimeout:    opts.IdleTimeout,
		headerTimeout:  opts.HeaderTimeout,
		maxHeaderBytes: opts.MaxHeaderBytes,
		connManager:    opts.ConnManager,
	}, nil
}

// Start begins listening for connections.
// This method is safe for concurrent use.
func (l *HTTPListener) Start() error {
	// Fast path: check if already running
	if l.running.Load() {
		return olbErrors.New(olbErrors.CodeAlreadyExist, "listener already running")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring lock
	if l.running.Load() {
		return olbErrors.New(olbErrors.CodeAlreadyExist, "listener already running")
	}

	// Create TCP listener
	tcpListener, err := net.Listen("tcp", l.address)
	if err != nil {
		return olbErrors.Wrapf(err, olbErrors.CodeUnavailable, "failed to listen on %s", l.address)
	}

	// Wrap with connection manager if provided
	if l.connManager != nil {
		tcpListener = &managedListener{
			Listener:    tcpListener,
			connManager: l.connManager,
		}
	}

	l.listener = tcpListener

	// Create HTTP server with configured timeouts
	l.server = &http.Server{
		Handler:           l.handler,
		ReadHeaderTimeout: l.headerTimeout,
		ReadTimeout:       l.readTimeout,
		WriteTimeout:      l.writeTimeout,
		IdleTimeout:       l.idleTimeout,
		MaxHeaderBytes:    l.maxHeaderBytes,
	}

	// Set running state before starting goroutine
	l.running.Store(true)
	l.startErr = nil

	// Start serving in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[http] panic recovered in listener: %v", r)
			}
		}()
		defer l.running.Store(false)

		err := l.server.Serve(l.listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.mu.Lock()
			l.startErr = err
			l.mu.Unlock()
		}
	}()

	return nil
}

// Stop gracefully shuts down the listener.
// It waits for existing connections to complete or until the context is canceled.
func (l *HTTPListener) Stop(ctx context.Context) error {
	if !l.running.Load() {
		return olbErrors.New(olbErrors.CodeUnavailable, "listener not running")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running.Load() {
		return olbErrors.New(olbErrors.CodeUnavailable, "listener not running")
	}

	// Shutdown the server gracefully
	if l.server != nil {
		if err := l.server.Shutdown(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return olbErrors.Wrap(err, olbErrors.CodeTimeout, "shutdown timeout")
			}
			return olbErrors.Wrap(err, olbErrors.CodeInternal, "shutdown failed")
		}
	}

	// Close the listener (in case Shutdown didn't)
	if l.listener != nil {
		l.listener.Close()
	}

	l.running.Store(false)
	return nil
}

// Name returns the listener name.
func (l *HTTPListener) Name() string {
	return l.name
}

// Address returns the listener address.
// If the listener is running and was bound to port 0, this returns the actual address.
func (l *HTTPListener) Address() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.listener != nil {
		return l.listener.Addr().String()
	}
	return l.address
}

// IsRunning returns true if the listener is active.
func (l *HTTPListener) IsRunning() bool {
	return l.running.Load()
}

// StartError returns any error that occurred during server startup/serve.
func (l *HTTPListener) StartError() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.startErr
}

// managedListener wraps a net.Listener to track connections.
type managedListener struct {
	net.Listener
	connManager ConnManager
}

// Accept accepts a connection and wraps it with tracking.
func (l *managedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	tracked, err := l.connManager.Accept(conn)
	if err != nil {
		// Connection was rejected (e.g., limit exceeded)
		return nil, err
	}

	return tracked, nil
}
