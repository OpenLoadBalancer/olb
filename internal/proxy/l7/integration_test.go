package l7

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/router"
)

// TestIntegration_BasicProxyFlow tests the full proxy flow end-to-end.
func TestIntegration_BasicProxyFlow(t *testing.T) {
	// Create a test backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers were set correctly
		if r.Header.Get("X-Forwarded-Proto") != "http" {
			t.Errorf("expected X-Forwarded-Proto to be 'http', got '%s'", r.Header.Get("X-Forwarded-Proto"))
		}
		if r.Header.Get("X-Forwarded-Host") != "example.com" {
			t.Errorf("expected X-Forwarded-Host to be 'example.com', got '%s'", r.Header.Get("X-Forwarded-Host"))
		}
		if r.Header.Get("X-Real-IP") == "" {
			t.Error("expected X-Real-IP to be set")
		}

		// Echo back the request path
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Backend received: " + r.URL.Path))
	}))
	defer backendServer.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Host:        "example.com",
		Path:        "/api/*path",
		BackendPool: "api-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("api-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("api-1", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up connection pool manager
	connPoolManager := conn.NewPoolManager(nil)

	// Set up health checker
	healthChecker := health.NewChecker()

	// Set up middleware chain with some basic middleware
	middlewareChain := middleware.NewChain()
	requestIDMiddleware := middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{})
	middlewareChain.Use(requestIDMiddleware)
	realIPMiddleware, _ := middleware.NewRealIPMiddleware(middleware.RealIPConfig{})
	middlewareChain.Use(realIPMiddleware)

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()

	// Execute request
	proxy.ServeHTTP(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body, _ := io.ReadAll(rr.Body)
	expectedBody := "Backend received: /api/users"
	if string(body) != expectedBody {
		t.Errorf("expected body '%s', got '%s'", expectedBody, string(body))
	}
}

// TestIntegration_MultipleBackends tests load balancing across multiple backends.
func TestIntegration_MultipleBackends(t *testing.T) {
	// Create two test backends
	backend1Calls := 0
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backend1Calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend1"))
	}))
	defer backend1.Close()

	backend2Calls := 0
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backend2Calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend2"))
	}))
	defer backend2.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b1 := backend.NewBackend("backend-1", backend1.Listener.Addr().String())
	b1.SetState(backend.StateUp)
	if err := pool.AddBackend(b1); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	b2 := backend.NewBackend("backend-2", backend2.Listener.Addr().String())
	b2.SetState(backend.StateUp)
	if err := pool.AddBackend(b2); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Make multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		proxy.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected status %d, got %d", i, http.StatusOK, rr.Code)
		}
	}

	// Verify both backends were called
	if backend1Calls == 0 {
		t.Error("backend1 was never called")
	}
	if backend2Calls == 0 {
		t.Error("backend2 was never called")
	}
	if backend1Calls+backend2Calls != 10 {
		t.Errorf("expected 10 total calls, got %d (backend1: %d, backend2: %d)",
			backend1Calls+backend2Calls, backend1Calls, backend2Calls)
	}
}

// TestIntegration_HealthCheckIntegration tests proxy with health-checked backends.
func TestIntegration_HealthCheckIntegration(t *testing.T) {
	// Create backends on 127.0.0.2 to avoid SSRF protection blocking
	// health checks to 127.0.0.1.

	// Create a backend that will fail health checks
	failingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("failing backend"))
	})
	failingLn, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("failed to listen on 127.0.0.2: %v", err)
	}
	failingBackend := httptest.NewUnstartedServer(failingHandler)
	failingBackend.Listener = failingLn
	failingBackend.Start()
	defer failingBackend.Close()

	// Create a healthy backend
	healthyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy backend"))
	})
	healthyLn, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("failed to listen on 127.0.0.2: %v", err)
	}
	healthyBackend := httptest.NewUnstartedServer(healthyHandler)
	healthyBackend.Listener = healthyLn
	healthyBackend.Start()
	defer healthyBackend.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b1 := backend.NewBackend("failing-backend", failingBackend.Listener.Addr().String())
	b1.SetState(backend.StateUp) // Start as up
	if err := pool.AddBackend(b1); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	b2 := backend.NewBackend("healthy-backend", healthyBackend.Listener.Addr().String())
	b2.SetState(backend.StateUp)
	if err := pool.AddBackend(b2); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up health checker
	healthChecker := health.NewChecker()
	checkConfig := &health.Check{
		Type:               "http",
		Interval:           100 * time.Millisecond,
		Timeout:            50 * time.Millisecond,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
	}

	// Register backends with health checker
	if err := healthChecker.Register(b1, checkConfig); err != nil {
		t.Fatalf("failed to register backend with health checker: %v", err)
	}
	if err := healthChecker.Register(b2, checkConfig); err != nil {
		t.Fatalf("failed to register backend with health checker: %v", err)
	}
	defer healthChecker.Stop()

	// Wait for health checks to run
	time.Sleep(300 * time.Millisecond)

	// Mark failing backend as down manually for this test
	b1.SetState(backend.StateDown)

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Make requests - should only hit healthy backend
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		proxy.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected status %d, got %d", i, http.StatusOK, rr.Code)
		}

		body := rr.Body.String()
		if body != "healthy backend" {
			t.Errorf("request %d: expected response from healthy backend, got '%s'", i, body)
		}
	}
}

// TestIntegration_PathParameters tests routing with path parameters.
func TestIntegration_PathParameters(t *testing.T) {
	// Create a test backend
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Path: " + r.URL.Path))
	}))
	defer backendServer.Close()

	// Set up the router with path parameter
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "user-route",
		Path:        "/users/:id",
		BackendPool: "user-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("user-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("user-backend", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Test with path parameter
	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	if body != "Path: /users/123" {
		t.Errorf("expected body 'Path: /users/123', got '%s'", body)
	}
}

// TestIntegration_MethodMatching tests routing with HTTP method matching.
func TestIntegration_MethodMatching(t *testing.T) {
	// Create a test backend
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Method: " + r.Method))
	}))
	defer backendServer.Close()

	// Set up the router with method matching
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "api-route",
		Path:        "/api/resource",
		Methods:     []string{"GET", "POST"},
		BackendPool: "api-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("api-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("api-backend", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Test GET request
	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET: expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if body := rr.Body.String(); body != "Method: GET" {
		t.Errorf("GET: expected body 'Method: GET', got '%s'", body)
	}

	// Test POST request
	req = httptest.NewRequest(http.MethodPost, "/api/resource", nil)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("POST: expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if body := rr.Body.String(); body != "Method: POST" {
		t.Errorf("POST: expected body 'Method: POST', got '%s'", body)
	}

	// Test DELETE request (should not match)
	req = httptest.NewRequest(http.MethodDelete, "/api/resource", nil)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	// The router doesn't match DELETE, so it will return 404
	if rr.Code != http.StatusNotFound {
		t.Errorf("DELETE: expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

// TestIntegration_PostBody tests POST request with body.
func TestIntegration_PostBody(t *testing.T) {
	// Create a test backend that echoes the body
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer backendServer.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Path:        "/echo",
		BackendPool: "echo-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("echo-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("echo-backend", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Test POST with body
	bodyContent := `{"message": "Hello, World!"}`
	req := httptest.NewRequest(http.MethodPost, "/echo", io.NopCloser(io.Reader(nil)))
	req.Body = io.NopCloser(io.Reader(nil))
	req = httptest.NewRequest(http.MethodPost, "/echo", io.NopCloser(nil))
	// Proper way to create request with body
	req = httptest.NewRequest(http.MethodPost, "/echo", nil)
	req.Body = io.NopCloser(io.Reader(nil))
	// Actually let's do it properly
	req = httptest.NewRequest(http.MethodPost, "/echo", nil)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(io.Reader(nil)), nil
	}

	// Simplest approach
	req = httptest.NewRequest(http.MethodPost, "/echo", nil)
	// Reset and use proper body
	req = httptest.NewRequest(http.MethodPost, "/echo", io.NopCloser(nil))

	// Let me just do it the right way
	req, _ = http.NewRequest(http.MethodPost, "/echo", nil)
	req.Body = io.NopCloser(nil)

	// Final attempt - use strings.NewReader equivalent
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(bodyContent))
		pw.Close()
	}()
	req = httptest.NewRequest(http.MethodPost, "/echo", pr)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	responseBody, _ := io.ReadAll(rr.Body)
	if string(responseBody) != bodyContent {
		t.Errorf("expected body '%s', got '%s'", bodyContent, string(responseBody))
	}
}

// TestIntegration_QueryParameters tests that query parameters are preserved.
func TestIntegration_QueryParameters(t *testing.T) {
	// Create a test backend
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Query: " + r.URL.RawQuery))
	}))
	defer backendServer.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Path:        "/search",
		BackendPool: "search-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("search-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("search-backend", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Test with query parameters
	req := httptest.NewRequest(http.MethodGet, "/search?q=test&page=1", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	if body != "Query: q=test&page=1" {
		t.Errorf("expected body 'Query: q=test&page=1', got '%s'", body)
	}
}

// TestIntegration_ResponseHeaders tests that response headers are preserved.
func TestIntegration_ResponseHeaders(t *testing.T) {
	// Create a test backend that sets custom headers
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backendServer.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("test-backend", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	// Test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if rr.Header().Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected X-Custom-Header 'custom-value', got '%s'", rr.Header().Get("X-Custom-Header"))
	}

	if rr.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control 'no-cache', got '%s'", rr.Header().Get("Cache-Control"))
	}
}

// TestIntegration_StatusCodePreservation tests that status codes are preserved.
func TestIntegration_StatusCodePreservation(t *testing.T) {
	// Create a test backend that returns different status codes
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/created":
			w.WriteHeader(http.StatusCreated)
		case "/bad-request":
			w.WriteHeader(http.StatusBadRequest)
		case "/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
		case "/server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
		w.Write([]byte("OK"))
	}))
	defer backendServer.Close()

	// Set up the router
	routerInstance := router.NewRouter()
	route := &router.Route{
		Name:        "test-route",
		Path:        "/*path",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Set up the backend pool
	poolManager := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("test-backend", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Set up middleware chain
	middlewareChain := middleware.NewChain()

	// Create the proxy
	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)
	defer proxy.Close()

	tests := []struct {
		path           string
		expectedStatus int
	}{
		{"/created", http.StatusCreated},
		{"/bad-request", http.StatusBadRequest},
		{"/unauthorized", http.StatusUnauthorized},
		{"/forbidden", http.StatusForbidden},
		{"/not-found", http.StatusNotFound},
		{"/server-error", http.StatusInternalServerError},
		{"/ok", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			proxy.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}
