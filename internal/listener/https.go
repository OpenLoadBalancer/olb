package listener

import (
	"crypto/tls"
	"errors"
	"log"
	"net"
	"net/http"

	olbTLS "github.com/openloadbalancer/olb/internal/tls"
	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// HTTPSListener implements an HTTPS listener with SNI-based certificate selection.
type HTTPSListener struct {
	*HTTPListener
	tlsManager *olbTLS.Manager
}

// NewHTTPSListener creates a new HTTPS listener with the given options and TLS manager.
func NewHTTPSListener(opts *Options, tlsManager *olbTLS.Manager) (*HTTPSListener, error) {
	if tlsManager == nil {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "TLS manager is required")
	}

	// Create the base HTTP listener
	httpListener, err := NewHTTPListener(opts)
	if err != nil {
		return nil, err
	}

	return &HTTPSListener{
		HTTPListener: httpListener,
		tlsManager:   tlsManager,
	}, nil
}

// Start begins listening for TLS connections.
func (l *HTTPSListener) Start() error {
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

	// Build TLS configuration with secure defaults
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		// Prefer server cipher suites
		PreferServerCipherSuites: true,
		// Set cipher suites for TLS 1.2 (TLS 1.3 has fixed cipher suites)
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		// Use the TLS manager's GetCertificate callback for SNI
		GetCertificate: l.tlsManager.GetCertificateCallback(),
	}

	// Wrap the TCP listener with TLS
	tlsListener := tls.NewListener(tcpListener, tlsConfig)

	// Wrap with connection manager if provided
	if l.connManager != nil {
		tlsListener = &managedListener{
			Listener:    tlsListener,
			connManager: l.connManager,
		}
	}

	l.listener = tlsListener

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
				log.Printf("[https] panic recovered in listener: %v", r)
			}
		}()
		defer l.running.Store(false)

		err := l.server.Serve(l.listener)
		if err != nil && !isClosedError(err) {
			l.mu.Lock()
			l.startErr = err
			l.mu.Unlock()
		}
	}()

	return nil
}

// isClosedError checks if an error is due to the listener being closed.
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, http.ErrServerClosed) {
		return true
	}
	// Check for various "closed" error conditions
	if opErr, ok := err.(*net.OpError); ok {
		return opErr.Err.Error() == "use of closed network connection"
	}
	return err.Error() == "http: Server closed"
}
