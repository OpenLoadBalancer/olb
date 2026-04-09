// Package l7 provides L7 (HTTP) proxy functionality for OpenLoadBalancer.
package l7

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/router"
	"github.com/openloadbalancer/olb/internal/security"
	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// Hop-by-hop headers that should be stripped from requests and responses.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// hopByHopSet is a pre-built lookup map for fast hop-by-hop header detection.
var hopByHopSet = func() map[string]bool {
	m := make(map[string]bool, len(hopByHopHeaders))
	for _, h := range hopByHopHeaders {
		m[strings.ToLower(h)] = true
	}
	return m
}()

// HTTPProxy is an L7 HTTP reverse proxy.
type HTTPProxy struct {
	router          *router.Router
	poolManager     *backend.PoolManager
	connPoolManager *conn.PoolManager
	healthChecker   *health.Checker
	middlewareChain *middleware.Chain

	// Protocol-specific handlers
	wsHandler      *WebSocketHandler
	grpcHandler    *GRPCHandler
	grpcWebHandler *GRPCWebHandler
	sseHandler     *SSEHandler

	// Shadow traffic
	shadowManager *ShadowManager

	// Configuration
	proxyTimeout        time.Duration
	dialTimeout         time.Duration
	maxRetries          int
	maxIdleConns        int
	maxIdleConnsPerHost int
	idleConnTimeout     time.Duration

	// Error handling (protected by atomic for concurrent access)
	errorHandler atomic.Value // stores func(http.ResponseWriter, *http.Request, error)

	// HTTP client for proxying (with custom transport for connection pooling)
	client *http.Client
}

// Config contains configuration for the HTTP proxy.
type Config struct {
	Router              *router.Router
	PoolManager         *backend.PoolManager
	ConnPoolManager     *conn.PoolManager
	HealthChecker       *health.Checker
	MiddlewareChain     *middleware.Chain
	ProxyTimeout        time.Duration
	DialTimeout         time.Duration
	MaxRetries          int
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	ShadowConfig        *ShadowConfig
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		ProxyTimeout:        60 * time.Second,
		DialTimeout:         10 * time.Second,
		MaxRetries:          3,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

// NewHTTPProxy creates a new HTTP proxy with the given configuration.
func NewHTTPProxy(config *Config) *HTTPProxy {
	if config == nil {
		config = DefaultConfig()
	}

	// Set defaults
	proxyTimeout := config.ProxyTimeout
	if proxyTimeout == 0 {
		proxyTimeout = 60 * time.Second
	}
	dialTimeout := config.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 10 * time.Second
	}
	maxRetries := config.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	maxIdleConns := config.MaxIdleConns
	if maxIdleConns == 0 {
		maxIdleConns = 100
	}
	maxIdleConnsPerHost := config.MaxIdleConnsPerHost
	if maxIdleConnsPerHost == 0 {
		maxIdleConnsPerHost = 10
	}
	idleConnTimeout := config.IdleConnTimeout
	if idleConnTimeout == 0 {
		idleConnTimeout = 90 * time.Second
	}

	p := &HTTPProxy{
		router:              config.Router,
		poolManager:         config.PoolManager,
		connPoolManager:     config.ConnPoolManager,
		healthChecker:       config.HealthChecker,
		middlewareChain:     config.MiddlewareChain,
		wsHandler:           NewWebSocketHandler(nil),
		grpcHandler:         NewGRPCHandler(nil),
		grpcWebHandler:      NewGRPCWebHandler(NewGRPCHandler(nil)),
		sseHandler:          NewSSEHandler(nil),
		proxyTimeout:        proxyTimeout,
		dialTimeout:         dialTimeout,
		maxRetries:          maxRetries,
		maxIdleConns:        maxIdleConns,
		maxIdleConnsPerHost: maxIdleConnsPerHost,
		idleConnTimeout:     idleConnTimeout,
		errorHandler:        func() atomic.Value { v := atomic.Value{}; v.Store(defaultErrorHandler); return v }(),
	}

	// Initialize shadow manager if configured
	if config.ShadowConfig != nil && config.ShadowConfig.Enabled {
		p.shadowManager = NewShadowManager(*config.ShadowConfig)
	}

	// Create HTTP client with custom transport
	p.client = &http.Client{
		Timeout:   proxyTimeout,
		Transport: p.createTransport(),
		// Don't follow redirects - let the client handle them
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return p
}

// createTransport creates an HTTP transport with connection pooling.
func (p *HTTPProxy) createTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Use connection pool if available
			if p.connPoolManager != nil {
				// Extract backend ID from context if available
				if backendID, ok := ctx.Value(backendIDKey).(string); ok {
					pool := p.connPoolManager.GetPool(backendID, addr)
					return pool.Get(ctx)
				}
			}
			// Fallback to direct dial
			dialer := &net.Dialer{
				Timeout:   p.dialTimeout,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, addr)
		},
		MaxIdleConns:          p.maxIdleConns,
		MaxIdleConnsPerHost:   p.maxIdleConnsPerHost,
		IdleConnTimeout:       p.idleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// Disable compression - we'll handle it in middleware if needed
		DisableCompression: true,
	}
}

// SetErrorHandler sets a custom error handler.
func (p *HTTPProxy) SetErrorHandler(handler func(http.ResponseWriter, *http.Request, error)) {
	p.errorHandler.Store(handler)
}

// ShadowManager returns the shadow manager, or nil if shadowing is not enabled.
func (p *HTTPProxy) ShadowManager() *ShadowManager {
	return p.shadowManager
}

// getErrorHandler returns the current error handler.
func (p *HTTPProxy) getErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return p.errorHandler.Load().(func(http.ResponseWriter, *http.Request, error))
}

// contextKey is a private type for context keys.
type contextKey int

const (
	backendIDKey contextKey = iota
)

// ServeHTTP implements http.Handler.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Match route
	routeMatch, ok := p.router.Match(r)
	if !ok {
		p.getErrorHandler()(w, r, olbErrors.ErrRouteNotFound)
		return
	}

	// Create request context
	reqCtx := middleware.NewRequestContext(r, w)
	defer reqCtx.Release()

	// Set route in context
	reqCtx.Route = routeMatch.Route

	// Build middleware chain with proxy handler
	handler := p.middlewareChain.Then(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		p.proxyHandler(rw, req, reqCtx, routeMatch)
	}))

	handler.ServeHTTP(w, r)
}

// proxyHandler handles the actual proxying.
// It detects protocol-specific requests (WebSocket, gRPC, SSE) and delegates
// to the appropriate handler, falling back to standard HTTP proxying.
func (p *HTTPProxy) proxyHandler(w http.ResponseWriter, r *http.Request, reqCtx *middleware.RequestContext, routeMatch *router.RouteMatch) {
	// Validate request for smuggling indicators before proxying
	if err := security.ValidateRequest(r); err != nil {
		p.getErrorHandler()(w, r, olbErrors.Wrap(err, olbErrors.CodeInvalidRequest, "request validation failed"))
		return
	}

	// Get backend pool
	pool := p.poolManager.GetPool(routeMatch.Route.BackendPool)
	if pool == nil {
		p.getErrorHandler()(w, r, olbErrors.ErrPoolNotFound.WithContext("pool", routeMatch.Route.BackendPool))
		return
	}

	// Fire-and-forget shadow request (non-blocking, best-effort)
	if p.shadowManager != nil && p.shadowManager.ShouldShadowRequest(r) {
		p.shadowManager.ShadowRequest(r)
	}

	// Check for protocol-specific requests that bypass the retry loop.
	// These long-lived connections (WebSocket, gRPC streaming, SSE) are
	// handled by dedicated handlers with their own connection lifecycle.
	isWS := IsWebSocketUpgrade(r) && p.wsHandler != nil
	isGRPCWeb := IsGRPCWebRequest(r) && p.grpcWebHandler != nil
	isGRPC := !isGRPCWeb && IsGRPCRequest(r) && p.grpcHandler != nil
	isSSE := IsSSERequest(r) && p.sseHandler != nil

	if isWS || isGRPCWeb || isGRPC || isSSE {
		// Select a backend for the protocol-specific handler
		selectedBackend := p.selectBackend(pool)
		if selectedBackend == nil {
			p.getErrorHandler()(w, r, olbErrors.ErrBackendUnavailable.WithContext("pool", pool.Name))
			return
		}
		reqCtx.Backend = selectedBackend

		var err error
		switch {
		case isWS:
			err = p.wsHandler.HandleWebSocket(w, r, selectedBackend)
		case isGRPCWeb:
			err = p.grpcWebHandler.HandleGRPCWeb(w, r, selectedBackend)
		case isGRPC:
			err = p.grpcHandler.HandleGRPC(w, r, selectedBackend)
		case isSSE:
			err = p.sseHandler.HandleSSE(w, r, selectedBackend)
		}
		if err != nil {
			selectedBackend.RecordError()
			p.getErrorHandler()(w, r, err)
		}
		return
	}

	// Standard HTTP proxy with retry logic
	var lastErr error
	var attemptedBackends []string

	for attempt := 0; attempt < p.maxRetries; attempt++ {
		// Get healthy backends
		healthyBackends := pool.GetHealthyBackends()

		// Filter out already attempted backends
		availableBackends := make([]*backend.Backend, 0, len(healthyBackends))
		for _, b := range healthyBackends {
			if !contains(attemptedBackends, b.ID) {
				availableBackends = append(availableBackends, b)
			}
		}

		if len(availableBackends) == 0 {
			backend.ReleaseHealthyBackends(healthyBackends)
			if len(healthyBackends) == 0 {
				p.getErrorHandler()(w, r, olbErrors.ErrPoolEmpty.WithContext("pool", pool.Name))
			} else {
				p.getErrorHandler()(w, r, olbErrors.ErrBackendUnavailable.WithContext("reason", "all backends attempted"))
			}
			return
		}

		// Select backend using balancer
		selectedBackend := pool.GetBalancer().Next(availableBackends)
		backend.ReleaseHealthyBackends(healthyBackends)
		if selectedBackend == nil {
			p.getErrorHandler()(w, r, olbErrors.ErrBackendUnavailable.WithContext("pool", pool.Name))
			return
		}

		attemptedBackends = append(attemptedBackends, selectedBackend.ID)

		// Set backend in context
		reqCtx.Backend = selectedBackend

		// Attempt to proxy request
		err := p.proxyRequest(w, r, reqCtx, selectedBackend)
		if err == nil {
			// Success
			return
		}

		lastErr = err
		selectedBackend.RecordError()

		// Check if error is retryable
		if !isRetryableError(err) {
			break
		}
	}

	p.getErrorHandler()(w, r, olbErrors.ErrBackendUnavailable.WithContext("reason", lastErr.Error()))
}

// selectBackend picks a healthy backend from the pool using the configured balancer.
// Returns nil if no healthy backend is available.
func (p *HTTPProxy) selectBackend(pool *backend.Pool) *backend.Backend {
	healthyBackends := pool.GetHealthyBackends()
	if len(healthyBackends) == 0 {
		return nil
	}
	selected := pool.GetBalancer().Next(healthyBackends)
	backend.ReleaseHealthyBackends(healthyBackends)
	return selected
}

// proxyRequest proxies a single request to a backend.
func (p *HTTPProxy) proxyRequest(w http.ResponseWriter, r *http.Request, reqCtx *middleware.RequestContext, b *backend.Backend) error {
	// Acquire connection slot
	if !b.AcquireConn() {
		return errors.New("backend at max connections")
	}
	defer b.ReleaseConn()

	// Prepare outbound request
	outReq, err := p.prepareOutboundRequest(r, b)
	if err != nil {
		return err
	}

	// Add backend ID to context for connection pooling
	ctx := context.WithValue(outReq.Context(), backendIDKey, b.ID)
	outReq = outReq.WithContext(ctx)

	// Record start time for latency tracking
	start := time.Now()

	// Execute request
	resp, err := p.client.Do(outReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Record latency
	latency := time.Since(start)

	// Copy response headers (excluding hop-by-hop)
	copyHeaders(w.Header(), resp.Header)

	// Write status code
	w.WriteHeader(resp.StatusCode)
	reqCtx.StatusCode = resp.StatusCode

	// Stream response body
	bytesOut, err := io.Copy(w, resp.Body)
	if err != nil {
		return err
	}

	// Record metrics
	b.RecordRequest(latency, bytesOut)
	reqCtx.UpdateBytesOut(bytesOut)

	return nil
}

// prepareOutboundRequest creates the outbound request with proper headers.
func (p *HTTPProxy) prepareOutboundRequest(r *http.Request, b *backend.Backend) (*http.Request, error) {
	// Clone the request
	outReq := r.Clone(r.Context())

	// Set the URL to point to the backend (using cached URL)
	backendURL := b.GetURL()

	outReq.URL.Scheme = backendURL.Scheme
	outReq.URL.Host = backendURL.Host
	outReq.Host = r.Host   // Preserve original Host header
	outReq.RequestURI = "" // Must be empty for client requests

	// Set X-Forwarded-For
	clientIP := getClientIP(r)
	if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
		outReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		outReq.Header.Set("X-Forwarded-For", clientIP)
	}

	// Set X-Real-IP
	outReq.Header.Set("X-Real-IP", clientIP)

	// Set X-Forwarded-Proto
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	outReq.Header.Set("X-Forwarded-Proto", proto)

	// Set X-Forwarded-Host
	outReq.Header.Set("X-Forwarded-Host", r.Host)

	// Handle Connection header values (must be before stripping Connection itself)
	if connHeaders := outReq.Header.Get("Connection"); connHeaders != "" {
		for _, h := range strings.Split(connHeaders, ",") {
			outReq.Header.Del(strings.TrimSpace(h))
		}
	}

	// Strip hop-by-hop headers
	for _, header := range hopByHopHeaders {
		outReq.Header.Del(header)
	}

	return outReq, nil
}

// getClientIP extracts the client IP from the request.
// Only trusts X-Forwarded-For/X-Real-IP when the direct peer is a trusted proxy.
func getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Only trust proxy headers if the direct connection comes from a
	// private/loopback address (trusted proxy). For public-facing deployments,
	// the middleware-layer trusted proxy config provides finer control.
	if isPrivateOrLoopback(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	return host
}

// isPrivateOrLoopback checks if an IP belongs to a private or loopback range.
func isPrivateOrLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.IsPrivate()
}

// copyHeaders copies headers from source to destination, excluding hop-by-hop headers.
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		// Skip hop-by-hop headers
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// isHopByHopHeader checks if a header is a hop-by-hop header.
func isHopByHopHeader(name string) bool {
	return hopByHopSet[strings.ToLower(name)]
}

// isRetryableError checks if an error warrants a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific error types
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Timeout errors are retryable
		if netErr.Timeout() {
			return true
		}
	}

	// Check for connection refused
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	// Check for connection reset
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	// Check for specific error messages
	errStr := err.Error()
	retryableStrings := []string{
		"connection refused",
		"actively refused", // Windows specific
		"connection reset",
		"no such host",
		"timeout",
		"temporary failure",
	}

	lowerErr := strings.ToLower(errStr)
	for _, s := range retryableStrings {
		if strings.Contains(lowerErr, s) {
			return true
		}
	}

	return false
}

// contains checks if a string slice contains a value.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ErrorResponse represents a JSON error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// defaultErrorHandler writes a JSON error response.
func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	code := http.StatusInternalServerError
	message := "Internal Server Error"

	// Map error codes to HTTP status codes
	if olbErr, ok := err.(*olbErrors.Error); ok {
		switch olbErr.Code {
		case olbErrors.CodeRouteNotFound:
			code = http.StatusNotFound
			message = "Not Found"
		case olbErrors.CodePoolNotFound:
			code = http.StatusServiceUnavailable
			message = "Service Unavailable"
		case olbErrors.CodePoolEmpty:
			code = http.StatusServiceUnavailable
			message = "Service Unavailable"
		case olbErrors.CodeBackendUnavailable:
			code = http.StatusServiceUnavailable
			message = "Service Unavailable"
		case olbErrors.CodeBackendNotFound:
			code = http.StatusServiceUnavailable
			message = "Service Unavailable"
		case olbErrors.CodeConnectionRefused:
			code = http.StatusBadGateway
			message = "Bad Gateway"
		case olbErrors.CodeConnectionTimeout:
			code = http.StatusGatewayTimeout
			message = "Gateway Timeout"
		case olbErrors.CodeTimeout:
			code = http.StatusGatewayTimeout
			message = "Gateway Timeout"
		default:
			message = olbErr.Message
		}
	}

	// Check for specific error types
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		code = http.StatusGatewayTimeout
		message = "Gateway Timeout"
	}

	response := ErrorResponse{
		Error:   "", // Do not expose internal error details to clients
		Code:    code,
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(response)
}

// ReverseProxy returns a standard library reverse proxy for advanced use cases.
// This can be used when more control is needed over the proxy behavior.
func (p *HTTPProxy) ReverseProxy(target *url.URL) *httputil.ReverseProxy {
	return httputil.NewSingleHostReverseProxy(target)
}

// Close cleans up resources used by the proxy.
func (p *HTTPProxy) Close() error {
	if p.client != nil {
		p.client.CloseIdleConnections()
	}
	return nil
}
