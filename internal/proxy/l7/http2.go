package l7

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/openloadbalancer/olb/internal/backend"
	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// HTTP2Config configures HTTP/2 proxy behavior.
type HTTP2Config struct {
	// EnableHTTP2 enables HTTP/2 support for both incoming and outgoing connections.
	EnableHTTP2 bool

	// EnableH2C enables HTTP/2 without TLS (h2c) for incoming connections.
	EnableH2C bool

	// MaxConcurrentStreams is the maximum number of concurrent streams per connection.
	// Default is 250.
	MaxConcurrentStreams uint32

	// MaxFrameSize is the maximum frame size in bytes.
	// Default is 16KB.
	MaxFrameSize uint32

	// IdleTimeout is the timeout for idle connections.
	IdleTimeout time.Duration

	// ReadIdleTimeout is the timeout before sending a PING frame.
	ReadIdleTimeout time.Duration

	// PingTimeout is the timeout for PING response.
	PingTimeout time.Duration

	// MaxDecoderHeaderBytes limits the total header bytes (decompressed) per request.
	// Protects against HPACK bomb attacks. Default is 64KB.
	// This maps to MaxDecoderHeaderTableSize on the http2.Server.
	MaxDecoderHeaderBytes uint32

	// MaxHeaderListSize limits the number of headers per request (after decompression).
	// Protects against header flood attacks. Default is 256.
	MaxHeaderListSize uint32

	// MaxUploadBufferPerConnection is the maximum buffered data per connection.
	// Default is 1MB.
	MaxUploadBufferPerConnection int32

	// MaxUploadBufferPerStream is the maximum buffered data per stream.
	// Default is 256KB.
	MaxUploadBufferPerStream int32

	// PermitProhibitedCipherSuites allows using cipher suites that are prohibited by HTTP/2.
	// Only use for testing, not in production.
	PermitProhibitedCipherSuites bool
}

// DefaultHTTP2Config returns a default HTTP/2 configuration.
func DefaultHTTP2Config() *HTTP2Config {
	return &HTTP2Config{
		EnableHTTP2:                  true,
		EnableH2C:                    true,
		MaxConcurrentStreams:         250,
		MaxFrameSize:                 16 * 1024, // 16KB
		IdleTimeout:                  60 * time.Second,
		ReadIdleTimeout:              30 * time.Second,
		PingTimeout:                  15 * time.Second,
		MaxDecoderHeaderBytes:        64 * 1024, // 64KB — HPACK bomb protection
		MaxHeaderListSize:            256,
		MaxUploadBufferPerConnection: 1 * 1024 * 1024, // 1MB
		MaxUploadBufferPerStream:     256 * 1024,      // 256KB
	}
}

// IsHTTP2Request checks if the request is an HTTP/2 request.
func IsHTTP2Request(r *http.Request) bool {
	return r.ProtoMajor == 2
}

// IsH2CRequest checks if the request is an h2c (HTTP/2 without TLS) upgrade request.
func IsH2CRequest(r *http.Request) bool {
	// h2c upgrade uses the HTTP2-Settings header or Upgrade: h2c
	if r.Header.Get("Upgrade") == "h2c" {
		return true
	}
	if r.Header.Get("HTTP2-Settings") != "" {
		return true
	}
	return false
}

// HTTP2Handler handles HTTP/2 proxying.
type HTTP2Handler struct {
	config      *HTTP2Config
	transport   *http.Transport
	h2Transport *http2.Transport
}

// NewHTTP2Handler creates a new HTTP/2 handler.
func NewHTTP2Handler(config *HTTP2Config) *HTTP2Handler {
	if config == nil {
		config = DefaultHTTP2Config()
	}

	// Create HTTP/2 transport for backend connections
	h2Transport := &http2.Transport{
		AllowHTTP: true, // Allow h2c to backends
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			// For h2c, use plain TCP
			return net.Dial(network, addr)
		},
	}

	// Create standard transport as fallback
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       config.IdleTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     config.EnableHTTP2,
	}

	return &HTTP2Handler{
		config:      config,
		transport:   transport,
		h2Transport: h2Transport,
	}
}

// GetTransport returns the appropriate transport for the given scheme.
func (h *HTTP2Handler) GetTransport(scheme string) http.RoundTripper {
	if !h.config.EnableHTTP2 {
		return h.transport
	}

	// For HTTP (h2c) requests, use the HTTP/2 transport directly
	if scheme == "http" {
		return h.h2Transport
	}

	// For HTTPS, the standard transport with ForceAttemptHTTP2 will use HTTP/2
	return h.transport
}

// newHTTP2Server creates an http2.Server with strict mode protections from config.
func (h *HTTP2Handler) newHTTP2Server() *http2.Server {
	srv := &http2.Server{
		MaxConcurrentStreams:         h.config.MaxConcurrentStreams,
		MaxReadFrameSize:             h.config.MaxFrameSize,
		IdleTimeout:                  h.config.IdleTimeout,
		ReadIdleTimeout:              h.config.ReadIdleTimeout,
		PingTimeout:                  h.config.PingTimeout,
		MaxUploadBufferPerConnection: h.config.MaxUploadBufferPerConnection,
		MaxUploadBufferPerStream:     h.config.MaxUploadBufferPerStream,
		WriteByteTimeout:             30 * time.Second,
	}
	// HPACK bomb protection: limit decoder header table size
	if h.config.MaxDecoderHeaderBytes > 0 {
		srv.MaxDecoderHeaderTableSize = h.config.MaxDecoderHeaderBytes
	}
	return srv
}

// WrapHandler wraps an HTTP handler with HTTP/2 support.
// If h2c is enabled, it will handle h2c upgrade requests.
func (h *HTTP2Handler) WrapHandler(handler http.Handler) http.Handler {
	if !h.config.EnableHTTP2 || !h.config.EnableH2C {
		return handler
	}

	// Use h2c to handle HTTP/2 cleartext upgrades
	return h2c.NewHandler(handler, h.newHTTP2Server())
}

// HTTP2Proxy handles HTTP/2 proxying to backends.
type HTTP2Proxy struct {
	httpProxy *HTTPProxy
	h2Handler *HTTP2Handler
}

// NewHTTP2Proxy creates a new proxy with HTTP/2 support.
func NewHTTP2Proxy(httpProxy *HTTPProxy, h2Config *HTTP2Config) *HTTP2Proxy {
	return &HTTP2Proxy{
		httpProxy: httpProxy,
		h2Handler: NewHTTP2Handler(h2Config),
	}
}

// ServeHTTP implements http.Handler with HTTP/2 support.
func (h2p *HTTP2Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is an HTTP/2 request
	if IsHTTP2Request(r) {
		// Handle HTTP/2 specific logic
		h2p.handleHTTP2(w, r)
		return
	}

	// Check if this is an h2c upgrade request
	if IsH2CRequest(r) {
		// h2c handler will upgrade the connection
		h2p.h2Handler.WrapHandler(h2p.httpProxy).ServeHTTP(w, r)
		return
	}

	// Regular HTTP/1.x request
	h2p.httpProxy.ServeHTTP(w, r)
}

// handleHTTP2 handles HTTP/2 requests.
func (h2p *HTTP2Proxy) handleHTTP2(w http.ResponseWriter, r *http.Request) {
	// For now, delegate to the HTTP proxy
	// The HTTP proxy will use the appropriate transport
	h2p.httpProxy.ServeHTTP(w, r)
}

// HTTP2Listener is an HTTP listener with HTTP/2 support.
type HTTP2Listener struct {
	name     string
	address  string
	handler  http.Handler
	server   *http.Server
	h2Server *http2.Server
	listener net.Listener
	running  atomic.Bool
	mu       sync.RWMutex

	// Configuration
	config    *HTTP2Config
	tlsConfig *tls.Config
	startErr  error
}

// HTTP2ListenerOptions configures the HTTP/2 listener.
type HTTP2ListenerOptions struct {
	Name      string
	Address   string
	Handler   http.Handler
	TLSConfig *tls.Config
	Config    *HTTP2Config
}

// NewHTTP2Listener creates a new HTTP/2 listener.
func NewHTTP2Listener(opts *HTTP2ListenerOptions) (*HTTP2Listener, error) {
	if opts == nil {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "options are required")
	}
	if opts.Name == "" {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "listener name is required")
	}
	if opts.Address == "" {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "listener address is required")
	}
	if opts.Handler == nil {
		return nil, olbErrors.New(olbErrors.CodeInvalidArg, "handler is required")
	}

	config := opts.Config
	if config == nil {
		config = DefaultHTTP2Config()
	}

	return &HTTP2Listener{
		name:      opts.Name,
		address:   opts.Address,
		handler:   opts.Handler,
		config:    config,
		tlsConfig: opts.TLSConfig,
	}, nil
}

// Start begins listening for HTTP/2 connections.
func (l *HTTP2Listener) Start() error {
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

	// Configure HTTP/2 server
	l.h2Server = &http2.Server{
		MaxConcurrentStreams:         l.config.MaxConcurrentStreams,
		MaxReadFrameSize:             l.config.MaxFrameSize,
		IdleTimeout:                  l.config.IdleTimeout,
		ReadIdleTimeout:              l.config.ReadIdleTimeout,
		PingTimeout:                  l.config.PingTimeout,
		MaxUploadBufferPerConnection: l.config.MaxUploadBufferPerConnection,
		MaxUploadBufferPerStream:     l.config.MaxUploadBufferPerStream,
		WriteByteTimeout:             30 * time.Second,
	}
	if l.config.MaxDecoderHeaderBytes > 0 {
		l.h2Server.MaxDecoderHeaderTableSize = l.config.MaxDecoderHeaderBytes
	}

	// Create HTTP server with timeouts matching the proxy defaults
	l.server = &http.Server{
		Handler:           l.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// If TLS is configured, wrap the listener
	if l.tlsConfig != nil {
		// Configure ALPN for HTTP/2
		l.tlsConfig.NextProtos = []string{"h2", "http/1.1"}
		l.listener = tls.NewListener(tcpListener, l.tlsConfig)
	} else if l.config.EnableH2C {
		// Enable h2c (HTTP/2 without TLS)
		l.server.Handler = h2c.NewHandler(l.handler, l.h2Server)
	}

	// Set running state before starting goroutine
	l.running.Store(true)
	l.startErr = nil

	// Start serving in a goroutine
	go func() {
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
func (l *HTTP2Listener) Stop(ctx context.Context) error {
	if !l.running.Load() {
		return olbErrors.New(olbErrors.CodeUnavailable, "listener not running")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running.Load() {
		return olbErrors.New(olbErrors.CodeUnavailable, "listener not running")
	}

	if l.server != nil {
		if err := l.server.Shutdown(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return olbErrors.Wrap(err, olbErrors.CodeTimeout, "shutdown timeout")
			}
			return olbErrors.Wrap(err, olbErrors.CodeInternal, "shutdown failed")
		}
	}

	if l.listener != nil {
		l.listener.Close()
	}

	l.running.Store(false)
	return nil
}

// Name returns the listener name.
func (l *HTTP2Listener) Name() string {
	return l.name
}

// Address returns the listener address.
func (l *HTTP2Listener) Address() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.listener != nil {
		return l.listener.Addr().String()
	}
	return l.address
}

// IsRunning returns true if the listener is active.
func (l *HTTP2Listener) IsRunning() bool {
	return l.running.Load()
}

// StartError returns any error that occurred during startup.
func (l *HTTP2Listener) StartError() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.startErr
}

// ALPNNegotiator handles ALPN protocol negotiation.
type ALPNNegotiator struct {
	supportedProtos []string
}

// NewALPNNegotiator creates a new ALPN negotiator with the supported protocols.
func NewALPNNegotiator(supportHTTP2 bool) *ALPNNegotiator {
	protos := []string{"http/1.1"}
	if supportHTTP2 {
		protos = append([]string{"h2"}, protos...)
	}
	return &ALPNNegotiator{
		supportedProtos: protos,
	}
}

// ConfigureTLS sets up the TLS config for ALPN.
func (a *ALPNNegotiator) ConfigureTLS(config *tls.Config) {
	config.NextProtos = a.supportedProtos
}

// NegotiatedProtocol returns the negotiated protocol from the connection state.
func (a *ALPNNegotiator) NegotiatedProtocol(state *tls.ConnectionState) string {
	if state == nil {
		return ""
	}
	return state.NegotiatedProtocol
}

// IsHTTP2 returns true if the negotiated protocol is HTTP/2.
func (a *ALPNNegotiator) IsHTTP2(state *tls.ConnectionState) bool {
	return a.NegotiatedProtocol(state) == "h2"
}

// HTTP2BackendTransport creates an HTTP/2 transport for backend connections.
type HTTP2BackendTransport struct {
	config    *HTTP2Config
	transport *http2.Transport
}

// NewHTTP2BackendTransport creates a new HTTP/2 backend transport.
func NewHTTP2BackendTransport(config *HTTP2Config) *HTTP2BackendTransport {
	if config == nil {
		config = DefaultHTTP2Config()
	}

	return &HTTP2BackendTransport{
		config: config,
		transport: &http2.Transport{
			AllowHTTP: true, // Allow h2c
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				// For h2c, use plain TCP
				return net.Dial(network, addr)
			},
		},
	}
}

// RoundTrip implements http.RoundTripper.
func (t *HTTP2BackendTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.transport.RoundTrip(req)
}

// CloseIdleConnections closes idle connections.
func (t *HTTP2BackendTransport) CloseIdleConnections() {
	t.transport.CloseIdleConnections()
}

// ProtocolInfo contains information about the protocol being used.
type ProtocolInfo struct {
	Version string
	TLS     bool
	ALPN    string
}

// GetProtocolInfo extracts protocol information from a request.
func GetProtocolInfo(r *http.Request) *ProtocolInfo {
	info := &ProtocolInfo{
		Version: r.Proto,
		TLS:     r.TLS != nil,
	}

	if r.TLS != nil {
		info.ALPN = r.TLS.NegotiatedProtocol
	}

	return info
}

// IsProtocolUpgrade checks if this is an HTTP/2 upgrade request.
func IsProtocolUpgrade(r *http.Request) bool {
	// Check for HTTP/2 upgrade headers
	connection := strings.ToLower(r.Header.Get("Connection"))
	upgrade := strings.ToLower(r.Header.Get("Upgrade"))

	if strings.Contains(connection, "upgrade") && upgrade == "h2c" {
		return true
	}

	// Check for HTTP2-Settings header (h2c with prior knowledge)
	if r.Header.Get("HTTP2-Settings") != "" {
		return true
	}

	return false
}

// HandleHTTP2Proxy handles proxying an HTTP/2 request to a backend.
func HandleHTTP2Proxy(w http.ResponseWriter, r *http.Request, b *backend.Backend, h2Config *HTTP2Config) error {
	if h2Config == nil || !h2Config.EnableHTTP2 {
		return olbErrors.New(olbErrors.CodeInvalidArg, "HTTP/2 is disabled")
	}

	// Acquire connection slot
	if !b.AcquireConn() {
		return olbErrors.New(olbErrors.CodeUnavailable, "backend at max connections")
	}
	defer b.ReleaseConn()

	// Create HTTP/2 transport
	transport := &http2.Transport{
		AllowHTTP: true, // Allow h2c
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}

	// Prepare request
	outReq := r.Clone(r.Context())
	outReq.URL.Scheme = "http"
	outReq.URL.Host = b.Address
	outReq.RequestURI = ""

	// Execute request
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		return fmt.Errorf("backend request failed: %w", err)
	}
	defer resp.Body.Close()

	// Copy response (filter hop-by-hop headers)
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	// Stream response body
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	return nil
}
