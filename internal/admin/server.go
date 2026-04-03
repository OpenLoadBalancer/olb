// Package admin provides the Admin API server for OpenLoadBalancer.
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/router"
)

// ClusterAdmin is the interface for cluster management operations.
// It is optional and may be nil if clustering is not enabled.
type ClusterAdmin interface {
	RegisterAdminEndpoints(mux *http.ServeMux)
}

// ConfigGetter returns the current configuration as a serializable value.
type ConfigGetter interface {
	GetConfig() interface{}
}

// CertLister lists loaded TLS certificates.
type CertLister interface {
	ListCertificates() []CertInfoView
}

// CertInfoView is a JSON-friendly view of a TLS certificate.
type CertInfoView struct {
	Names      []string `json:"names"`
	Expiry     int64    `json:"expiry"`
	IsWildcard bool     `json:"is_wildcard"`
}

// Server provides the Admin API HTTP server.
type Server struct {
	addr      string
	server    *http.Server
	config    *AuthConfig
	startTime time.Time

	// CORS
	allowedOrigins []string

	// Component references (interfaces)
	poolManager   PoolManager
	router        Router
	healthChecker HealthChecker
	metrics       Metrics

	// Optional components
	clusterAdmin ClusterAdmin // optional, nil if clustering not enabled
	webUI        http.Handler // optional, nil if web UI not available
	configGetter ConfigGetter // optional, for GET /api/v1/config
	certLister   CertLister   // optional, for GET /api/v1/certificates
	wafStatus    func() any   // optional WAF status provider

	// Callbacks
	onReload func() error

	// State
	mu    sync.RWMutex
	state string
}

// Config holds the server configuration.
type Config struct {
	Address       string
	Auth          *AuthConfig
	PoolManager   PoolManager
	Router        Router
	HealthChecker HealthChecker
	Metrics       Metrics
	OnReload      func() error

	// CORS configuration
	// AllowedOrigins restricts which origins can make cross-origin requests.
	// When empty, defaults to same-origin only (no CORS headers).
	// Use "*" to allow all origins (credentials won't be sent).
	// Use specific origins like ["https://admin.example.com"] for production.
	AllowedOrigins []string

	// Optional components
	ClusterAdmin ClusterAdmin // optional cluster management
	WebUI        http.Handler // optional web UI handler
	ConfigGetter ConfigGetter // optional config provider
	CertLister   CertLister   // optional certificate lister
	WAFStatus    func() any   // optional WAF status provider
}

// PoolManager interface for backend pool operations.
type PoolManager interface {
	GetAllPools() []*backend.Pool
	GetPool(name string) *backend.Pool
}

// Router interface for route operations.
type Router interface {
	Routes() []*router.Route
}

// HealthChecker interface for health check operations.
type HealthChecker interface {
	ListStatuses() map[string]health.Status
	GetResult(backendID string) *health.Result
}

// Metrics interface for metrics operations.
type Metrics interface {
	GetAllMetrics() map[string]interface{}
	PrometheusFormat() string
}

// NewServer creates a new Admin API server.
func NewServer(config *Config) (*Server, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if config.Address == "" {
		return nil, fmt.Errorf("address is required")
	}

	s := &Server{
		addr:           config.Address,
		config:         config.Auth,
		poolManager:    config.PoolManager,
		router:         config.Router,
		healthChecker:  config.HealthChecker,
		metrics:        config.Metrics,
		onReload:       config.OnReload,
		clusterAdmin:   config.ClusterAdmin,
		webUI:          config.WebUI,
		configGetter:   config.ConfigGetter,
		allowedOrigins: config.AllowedOrigins,
		certLister:    config.CertLister,
		wafStatus:     config.WAFStatus,
		startTime:     time.Now(),
		state:         "running",
	}

	s.setupRoutes()
	return s, nil
}

// setupRoutes configures the HTTP routes.
func (s *Server) setupRoutes() {
	mux := http.NewServeMux()

	// System endpoints
	mux.HandleFunc("/api/v1/system/info", s.getSystemInfo)
	mux.HandleFunc("/api/v1/system/health", s.getSystemHealth)
	mux.HandleFunc("/api/v1/system/reload", s.reloadConfig)

	// Backend endpoints
	mux.HandleFunc("/api/v1/backends", s.listBackends)
	mux.HandleFunc("/api/v1/backends/", s.handleBackendDetail)

	// Route endpoints
	mux.HandleFunc("/api/v1/routes", s.listRoutes)

	// Health endpoint
	mux.HandleFunc("/api/v1/health", s.getHealthStatus)

	// Metrics endpoints
	mux.HandleFunc("/api/v1/metrics", s.getMetricsJSON)
	mux.HandleFunc("/metrics", s.getMetricsPrometheus)

	// Config endpoint
	mux.HandleFunc("/api/v1/config", s.getConfig)

	// Certificates endpoint
	mux.HandleFunc("/api/v1/certificates", s.getCertificates)

	// WAF status endpoint (optional)
	if s.wafStatus != nil {
		mux.HandleFunc("/api/v1/waf/status", s.getWAFStatus)
	}

	// Cluster endpoints (optional)
	if s.clusterAdmin != nil {
		s.clusterAdmin.RegisterAdminEndpoints(mux)
	}

	// Web UI (optional) — serves static dashboard at root
	if s.webUI != nil {
		mux.Handle("/", s.webUI)
	}

	// Apply rate limiting before auth to prevent brute force attacks
	var handler http.Handler = mux
	handler = adminRateLimit(handler)

	// Apply auth middleware if configured
	if s.config != nil {
		handler = AuthMiddleware(s.config)(handler)
	}

	// Apply CORS for admin API (restricted to allowed origins)
	handler = adminCORS(s.allowedOrigins)(handler)

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// Start starts the Admin API server.
func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

// Stop gracefully stops the Admin API server.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.state = "stopping"
	s.mu.Unlock()

	return s.server.Shutdown(ctx)
}

// GetState returns the current server state.
func (s *Server) GetState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// handleBackendDetail handles requests to /api/v1/backends/...
func (s *Server) handleBackendDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Check if this is a drain request
	if strings.HasSuffix(path, "/drain") {
		if r.Method == http.MethodPost {
			s.drainBackend(w, r)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
		return
	}

	// Count path segments to determine if it's a pool or backend request
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")

	// /api/v1/backends/:pool (4 parts: api, v1, backends, :pool)
	// /api/v1/backends/:pool/:backend (5 parts)
	if len(parts) == 4 {
		// Pool-level request
		switch r.Method {
		case http.MethodGet:
			s.getPool(w, r)
		case http.MethodPost:
			s.addBackend(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	} else if len(parts) >= 5 {
		// Backend-level request
		// Check for sub-resource (e.g., /api/v1/backends/:pool/:backend/drain)
		if len(parts) >= 6 && parts[5] == "drain" {
			s.drainBackend(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.getBackendDetail(w, r)
		case http.MethodPatch:
			s.updateBackend(w, r)
		case http.MethodDelete:
			s.removeBackend(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	} else {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
	}
}

// defaultMetrics implements the Metrics interface using the default registry.
type defaultMetrics struct {
	registry *metrics.Registry
}

// NewDefaultMetrics creates a new default metrics provider.
func NewDefaultMetrics(registry *metrics.Registry) Metrics {
	if registry == nil {
		registry = metrics.DefaultRegistry
	}
	return &defaultMetrics{registry: registry}
}

// GetAllMetrics returns all metrics in JSON-compatible format.
func (m *defaultMetrics) GetAllMetrics() map[string]interface{} {
	result := make(map[string]interface{})

	var buf bytes.Buffer
	handler := metrics.NewJSONHandler(m.registry)
	if err := handler.WriteMetrics(&buf); err == nil {
		// Parse the JSON output
		var metrics map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &metrics); err == nil {
			return metrics
		}
	}

	return result
}

// PrometheusFormat returns metrics in Prometheus exposition format.
func (m *defaultMetrics) PrometheusFormat() string {
	var buf bytes.Buffer
	handler := metrics.NewPrometheusHandler(m.registry)
	handler.WriteMetrics(&buf)
	return buf.String()
}

// adminRateLimit provides basic rate limiting for the admin API to prevent
// brute-force attacks. Allows 30 requests per minute per source IP.
func adminRateLimit(next http.Handler) http.Handler {
	type visitor struct {
		count    int
		lastSeen time.Time
	}
	var (
		mu       sync.Mutex
		visitors = make(map[string]*visitor)
		maxReqs  = 30
		window   = time.Minute
	)

	// Cleanup stale entries periodically
	go func() {
		for {
			time.Sleep(window)
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > window {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		mu.Lock()
		v, exists := visitors[ip]
		if !exists || time.Since(v.lastSeen) > window {
			visitors[ip] = &visitor{count: 1, lastSeen: time.Now()}
			mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		v.count++
		v.lastSeen = time.Now()
		if v.count > maxReqs {
			mu.Unlock()
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// adminCORS wraps a handler with CORS headers for the admin API.
// When AllowedOrigins is configured, only those origins are reflected back.
// When empty, no CORS headers are set (same-origin policy applies).
func adminCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	// Build a set for O(1) lookup
	originSet := make(map[string]bool, len(allowedOrigins))
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		originSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && (allowAll || originSet[origin]) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if !allowAll {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
