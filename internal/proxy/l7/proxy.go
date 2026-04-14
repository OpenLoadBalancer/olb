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
	"github.com/openloadbalancer/olb/pkg/utils"
)

// requestState bundles per-request state into a single context value to avoid
// multiple context.WithValue allocations per request.
type requestState struct {
	reqCtx     *middleware.RequestContext
	routeMatch *router.RouteMatch
}

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

// hopByHopCanonicalSet provides lookup using http.CanonicalHeaderKey format
// to avoid strings.ToLower allocation on each response header.
var hopByHopCanonicalSet = func() map[string]bool {
	m := make(map[string]bool, len(hopByHopHeaders))
	for _, h := range hopByHopHeaders {
		m[http.CanonicalHeaderKey(h)] = true
	}
	return m
}()

// HTTPProxy is an L7 HTTP reverse proxy.
type HTTPProxy struct {
	router          *router.Router
	poolManager     *backend.PoolManager
	connPoolManager *conn.PoolManager
	healthChecker   *health.Checker
	passiveChecker  *health.PassiveChecker
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

	// Trusted proxy networks for X-Forwarded-For handling
	trustedNets []*net.IPNet

	// Error handling (protected by atomic for concurrent access)
	errorHandler atomic.Value // stores func(http.ResponseWriter, *http.Request, error)

	// Cached middleware chain handler (rebuilt when middleware changes)
	// Protected by atomic.Value for concurrent access during ServeHTTP + RebuildHandler
	cachedHandler atomic.Value // stores http.Handler

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
	PassiveChecker      *health.PassiveChecker
	ShadowConfig        *ShadowConfig
	TrustedProxies      []string // CIDR ranges of trusted proxy servers
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
		passiveChecker:      config.PassiveChecker,
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

	// Parse trusted proxy CIDRs for XFF handling
	p.trustedNets = parseTrustedProxies(config.TrustedProxies)

	// Propagate trusted proxy nets to protocol-specific handlers
	p.wsHandler.SetTrustedNets(p.trustedNets)
	p.grpcHandler.SetTrustedNets(p.trustedNets)
	p.grpcWebHandler.grpcHandler.SetTrustedNets(p.trustedNets)
	p.sseHandler.SetTrustedNets(p.trustedNets)

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

	p.buildCachedHandler()

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
	if v := p.errorHandler.Load(); v != nil {
		if fn, ok := v.(func(http.ResponseWriter, *http.Request, error)); ok {
			return fn
		}
	}
	return defaultErrorHandler
}

// contextKey is a private type for context keys.
type contextKey int

const (
	backendIDKey contextKey = iota
	requestStateKey
)

// buildCachedHandler builds and caches the middleware chain handler.
// Must be called whenever the middleware chain changes.
func (p *HTTPProxy) buildCachedHandler() {
	if p.middlewareChain == nil {
		p.cachedHandler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rs, ok := r.Context().Value(requestStateKey).(*requestState)
			if !ok {
				http.Error(w, "internal error: missing request state", http.StatusInternalServerError)
				return
			}
			p.proxyHandler(w, r, rs.reqCtx, rs.routeMatch)
		}))
		return
	}
	p.cachedHandler.Store(p.middlewareChain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs, ok := r.Context().Value(requestStateKey).(*requestState)
		if !ok {
			http.Error(w, "internal error: missing request state", http.StatusInternalServerError)
			return
		}
		p.proxyHandler(w, r, rs.reqCtx, rs.routeMatch)
	})))
}

// RebuildHandler rebuilds the cached middleware chain handler.
// Call this after modifying the middleware chain.
func (p *HTTPProxy) RebuildHandler() {
	p.buildCachedHandler()
}

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

	// Store per-request state in a single context value to reduce allocations
	ctx := context.WithValue(r.Context(), requestStateKey, &requestState{
		reqCtx:     reqCtx,
		routeMatch: routeMatch,
	})
	r = r.WithContext(ctx)

	// Use cached handler (built once, not per-request)
	if v := p.cachedHandler.Load(); v != nil {
		if h, ok := v.(http.Handler); ok {
			h.ServeHTTP(w, r)
			return
		}
	}
	http.Error(w, "proxy not initialized", http.StatusServiceUnavailable)
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
		selectedBackend := p.selectBackend(pool, reqCtx)
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
			if p.passiveChecker != nil {
				p.passiveChecker.RecordFailure(selectedBackend.Address)
			}
			p.getErrorHandler()(w, r, err)
		}
		return
	}

	// Standard HTTP proxy with retry logic
	var lastErr error
	// Use a fixed-size stack array for attempted backend IDs to avoid
	// heap allocation in the common case (retries are rare).
	var attemptedBuf [8]string
	attemptedBackends := attemptedBuf[:0]

	for attempt := 0; attempt < p.maxRetries; attempt++ {
		// Get healthy backends
		healthyBackends := pool.GetHealthyBackends()

		// On first attempt, all healthy backends are available � skip filtering.
		// On retries, filter out already-attempted backends.
		var availableBackends []*backend.Backend
		if attempt == 0 {
			availableBackends = healthyBackends
		} else {
			availableBackends = make([]*backend.Backend, 0, len(healthyBackends))
			for _, b := range healthyBackends {
				if !contains(attemptedBackends, b.ID) {
					availableBackends = append(availableBackends, b)
				}
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
		selectedBackend := pool.GetBalancer().Next(&backend.RequestContext{ClientIP: reqCtx.ClientIP, Request: reqCtx.Request}, availableBackends)
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
			if p.passiveChecker != nil {
				p.passiveChecker.RecordSuccess(selectedBackend.Address)
			}
			// Success
			return
		}

		lastErr = err
		selectedBackend.RecordError()
		if p.passiveChecker != nil {
			p.passiveChecker.RecordFailure(selectedBackend.Address)
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			break
		}
	}

	p.getErrorHandler()(w, r, olbErrors.ErrBackendUnavailable.WithContext("reason", lastErr.Error()))
}

// selectBackend picks a healthy backend from the pool using the configured balancer.
// Returns nil if no healthy backend is available.
func (p *HTTPProxy) selectBackend(pool *backend.Pool, reqCtx *middleware.RequestContext) *backend.Backend {
	healthyBackends := pool.GetHealthyBackends()
	if len(healthyBackends) == 0 {
		return nil
	}
	var balancerCtx *backend.RequestContext
	if reqCtx != nil {
		balancerCtx = &backend.RequestContext{
			ClientIP: reqCtx.ClientIP,
			Request:  reqCtx.Request,
		}
	}
	selected := pool.GetBalancer().Next(balancerCtx, healthyBackends)
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

	// Stream response body using pooled buffer
	buf := utils.Get(32 * 1024)
	defer utils.Put(buf)
	bytesOut, err := io.CopyBuffer(w, resp.Body, buf)
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
	clientIP := p.getClientIP(r)
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
// Only trusts X-Forwarded-For/X-Real-IP when the direct peer is in TrustedProxies.
// When TrustedProxies is empty, proxy headers are never trusted (secure default).
func (p *HTTPProxy) getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Only trust proxy headers if the direct connection originates from
	// a configured trusted proxy network.
	if p.isTrustedProxy(host) {
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

// isTrustedProxy reports whether ip belongs to a configured trusted proxy network.
func (p *HTTPProxy) isTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range p.trustedNets {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// parseTrustedProxies parses a list of CIDR strings into net.IPNet slices.
// Bare IPs are treated as /32 (IPv4) or /128 (IPv6). Invalid entries are skipped.
func parseTrustedProxies(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		s := cidr
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				s += "/32"
			} else {
				s += "/128"
			}
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// trustedClientIP extracts the client IP from a request using the given trusted proxy nets.
// If trustedNets is empty, proxy headers are never trusted (secure default).
func trustedClientIP(r *http.Request, trustedNets []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	ip := net.ParseIP(host)
	if ip != nil {
		for _, cidr := range trustedNets {
			if cidr.Contains(ip) {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					first, _, _ := strings.Cut(xff, ",")
					return strings.TrimSpace(first)
				}
				if xri := r.Header.Get("X-Real-IP"); xri != "" {
					return strings.TrimSpace(xri)
				}
				break
			}
		}
	}

	return host
}
// Uses canonical lookup for normal http.Header keys (no allocation) and falls back
// to lowercase lookup for raw map keys that bypass http.Header normalization.
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		// Skip hop-by-hop headers — try canonical form first (no allocation),
		// then fall back to lowercase for literal map keys.
		if hopByHopCanonicalSet[key] || hopByHopSet[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Add(key, security.SanitizeHeaderValue(value))
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
			message = "Internal Server Error"
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
