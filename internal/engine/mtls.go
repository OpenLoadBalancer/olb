package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/listener"
	"github.com/openloadbalancer/olb/internal/security"
	olbTLS "github.com/openloadbalancer/olb/internal/tls"
)

// createMTLSListener creates an HTTPS listener with mTLS (mutual TLS) support.
// It loads client CA certificates and configures client certificate verification.
func (e *Engine) createMTLSListener(opts *listener.Options, listenerCfg *config.Listener) (listener.Listener, error) {
	mtlsCfg := &olbTLS.MTLSConfig{
		Enabled:   true,
		ClientCAs: listenerCfg.MTLS.ClientCAs,
	}

	// Parse client auth policy
	if listenerCfg.MTLS.ClientAuth != "" {
		policy, err := olbTLS.ParseClientAuthPolicy(listenerCfg.MTLS.ClientAuth)
		if err != nil {
			return nil, fmt.Errorf("invalid client_auth policy: %w", err)
		}
		mtlsCfg.ClientAuth = policy
	} else {
		mtlsCfg.ClientAuth = olbTLS.RequireAndVerifyClientCert
	}

	// Build mTLS-enabled TLS config using the MTLSManager
	tlsConfig, err := e.mtlsManager.BuildServerTLSConfig(
		listenerCfg.Name,
		mtlsCfg,
		e.tlsManager.GetCertificateCallback(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build mTLS config: %w", err)
	}

	// Apply security-hardened TLS defaults (min version, cipher suites, curves)
	secureDefaults := security.DefaultTLSConfig()
	if tlsConfig.MinVersion < secureDefaults.MinVersion {
		tlsConfig.MinVersion = secureDefaults.MinVersion
	}
	if len(tlsConfig.CipherSuites) == 0 {
		tlsConfig.CipherSuites = secureDefaults.CipherSuites
	}
	if len(tlsConfig.CurvePreferences) == 0 {
		tlsConfig.CurvePreferences = secureDefaults.CurvePreferences
	}

	// Create a custom HTTPS listener that uses our mTLS-configured tls.Config
	return newMTLSHTTPSListener(opts, tlsConfig)
}

// mtlsHTTPSListener is an HTTPS listener with custom TLS configuration for mTLS.
type mtlsHTTPSListener struct {
	name       string
	address    string
	handler    http.Handler
	tlsConfig  *tls.Config
	server     *http.Server
	ln         interface{ Addr() fmt.Stringer }
	running    bool
	mu         sync.RWMutex
	actualAddr string
}

// newMTLSHTTPSListener creates a new HTTPS listener with mTLS support.
func newMTLSHTTPSListener(opts *listener.Options, tlsConfig *tls.Config) (*mtlsHTTPSListener, error) {
	return &mtlsHTTPSListener{
		name:      opts.Name,
		address:   opts.Address,
		handler:   opts.Handler,
		tlsConfig: tlsConfig,
	}, nil
}

// Start begins listening for mTLS connections.
func (l *mtlsHTTPSListener) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return fmt.Errorf("listener already running")
	}

	srv := &http.Server{
		Addr:           l.address,
		Handler:        l.handler,
		TLSConfig:      l.tlsConfig,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	l.server = srv
	l.running = true

	// Use ListenAndServeTLS with empty cert/key since tlsConfig has GetCertificate
	go func() {
		err := srv.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
		}
	}()

	// Store the address (may differ from configured if port 0 was used)
	l.actualAddr = l.address

	return nil
}

// Stop gracefully shuts down the mTLS HTTPS listener.
func (l *mtlsHTTPSListener) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running {
		return fmt.Errorf("listener not running")
	}

	l.running = false
	if l.server != nil {
		return l.server.Shutdown(ctx)
	}
	return nil
}

// Name returns the listener name.
func (l *mtlsHTTPSListener) Name() string { return l.name }

// Address returns the listener address.
func (l *mtlsHTTPSListener) Address() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.actualAddr
}

// IsRunning returns true if the listener is active.
func (l *mtlsHTTPSListener) IsRunning() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.running
}
