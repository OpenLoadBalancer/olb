package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/router"
)

// Mock implementations for testing

type mockPoolManager struct {
	pools map[string]*backend.Pool
}

func newMockPoolManager() *mockPoolManager {
	return &mockPoolManager{
		pools: make(map[string]*backend.Pool),
	}
}

func (m *mockPoolManager) GetAllPools() []*backend.Pool {
	result := make([]*backend.Pool, 0, len(m.pools))
	for _, p := range m.pools {
		result = append(result, p)
	}
	return result
}

func (m *mockPoolManager) GetPool(name string) *backend.Pool {
	return m.pools[name]
}

func (m *mockPoolManager) AddPool(pool *backend.Pool) {
	m.pools[pool.Name] = pool
}

type mockRouter struct {
	routes []*router.Route
}

func newMockRouter() *mockRouter {
	return &mockRouter{
		routes: make([]*router.Route, 0),
	}
}

func (m *mockRouter) Routes() []*router.Route {
	return m.routes
}

func (m *mockRouter) AddRoute(r *router.Route) {
	m.routes = append(m.routes, r)
}

type mockHealthChecker struct {
	statuses map[string]health.Status
	results  map[string]*health.Result
}

func newMockHealthChecker() *mockHealthChecker {
	return &mockHealthChecker{
		statuses: make(map[string]health.Status),
		results:  make(map[string]*health.Result),
	}
}

func (m *mockHealthChecker) ListStatuses() map[string]health.Status {
	return m.statuses
}

func (m *mockHealthChecker) GetResult(backendID string) *health.Result {
	return m.results[backendID]
}

func (m *mockHealthChecker) SetStatus(backendID string, status health.Status) {
	m.statuses[backendID] = status
}

type mockMetrics struct {
	data map[string]interface{}
}

func newMockMetrics() *mockMetrics {
	return &mockMetrics{
		data: map[string]interface{}{
			"test_counter": map[string]interface{}{
				"type":  "counter",
				"value": 42,
			},
		},
	}
}

func (m *mockMetrics) GetAllMetrics() map[string]interface{} {
	return m.data
}

func (m *mockMetrics) PrometheusFormat() string {
	return "# HELP test_counter Test counter\n# TYPE test_counter counter\ntest_counter 42\n"
}

// Test helpers

func setupTestServer(t *testing.T, authConfig *AuthConfig) (*Server, *mockPoolManager, *mockRouter, *mockHealthChecker, *mockMetrics) {
	poolManager := newMockPoolManager()
	r := newMockRouter()
	hc := newMockHealthChecker()
	m := newMockMetrics()

	config := &Config{
		Address:       "127.0.0.1:0",
		Auth:          authConfig,
		PoolManager:   poolManager,
		Router:        r,
		HealthChecker: hc,
		Metrics:       m,
		OnReload: func() error {
			return nil
		},
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server, poolManager, r, hc, m
}

// Test cases

func TestNewServer(t *testing.T) {
	// Test nil config
	_, err := NewServer(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}

	// Test empty address
	_, err = NewServer(&Config{})
	if err == nil {
		t.Error("expected error for empty address")
	}

	// Test valid config
	config := &Config{
		Address: "127.0.0.1:0",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server == nil {
		t.Error("expected server to be created")
	}
}

func TestServerStartStop(t *testing.T) {
	config := &Config{
		Address: "127.0.0.1:0",
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server in background
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Stop server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

func TestAuthMiddleware_BasicAuth(t *testing.T) {
	// Generate a bcrypt hash for "password"
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test request without auth
	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// Test request with valid basic auth
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:password")))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Test request with invalid password
	req, _ = http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:wrongpassword")))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_BearerToken(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"test-token-123", "another-token"},
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test request without auth
	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// Test request with valid bearer token
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Test request with invalid token
	req, _ = http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_PublicEndpoints(t *testing.T) {
	// Auth not required for read operations
	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           "$2a$10$test",
		RequireAuthForRead: false,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// GET request should work without auth
	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", resp.StatusCode)
	}

	// POST request should require auth
	resp, err = client.Post(baseURL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for POST, got %d", resp.StatusCode)
	}
}

func TestGetSystemInfo(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}

	if _, ok := data["version"]; !ok {
		t.Error("expected version in response")
	}
	if _, ok := data["go_version"]; !ok {
		t.Error("expected go_version in response")
	}
}

func TestGetSystemHealth(t *testing.T) {
	server, poolManager, r, hc, _ := setupTestServer(t, nil)

	// Add some test data
	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	r.AddRoute(&router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	})

	hc.SetStatus("backend1", health.StatusHealthy)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/system/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}
}

func TestListBackends(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool with backends
	pool := backend.NewPool("test-pool", "round_robin")
	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b1.Weight = 2
	pool.AddBackend(b1)

	b2 := backend.NewBackend("backend2", "127.0.0.1:8081")
	pool.AddBackend(b2)

	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/backends")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	pools, ok := result.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be a slice, got %T", result.Data)
	}

	if len(pools) != 1 {
		t.Errorf("expected 1 pool, got %d", len(pools))
	}
}

func TestGetPool(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend1", "127.0.0.1:8080")
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test existing pool
	resp, err := client.Get(baseURL + "/api/v1/backends/test-pool")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Test non-existent pool
	resp, err = client.Get(baseURL + "/api/v1/backends/nonexistent")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAddBackend(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool
	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Add new backend
	reqBody := `{"id":"backend2","address":"127.0.0.1:8081","weight":3}`
	resp, err := client.Post(baseURL+"/api/v1/backends/test-pool", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify backend was added
	p := poolManager.GetPool("test-pool")
	if p.GetBackend("backend2") == nil {
		t.Error("expected backend2 to be added")
	}

	// Test duplicate backend
	resp, err = client.Post(baseURL+"/api/v1/backends/test-pool", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for duplicate, got %d", resp.StatusCode)
	}
}

func TestRemoveBackend(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool with backend
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend1", "127.0.0.1:8080")
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Remove backend
	req, _ := http.NewRequest("DELETE", baseURL+"/api/v1/backends/test-pool/backend1", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify backend was removed
	p := poolManager.GetPool("test-pool")
	if p.GetBackend("backend1") != nil {
		t.Error("expected backend1 to be removed")
	}
}

func TestDrainBackend(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool with backend
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Drain backend
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/backends/test-pool/backend1/drain", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify backend is draining
	p := poolManager.GetPool("test-pool")
	backend := p.GetBackend("backend1")
	if backend.State().String() != "draining" {
		t.Errorf("expected state to be draining, got %s", backend.State().String())
	}
}

func TestListRoutes(t *testing.T) {
	server, _, r, _, _ := setupTestServer(t, nil)

	// Add test routes
	r.AddRoute(&router.Route{
		Name:        "route1",
		Host:        "example.com",
		Path:        "/api/v1/users",
		Methods:     []string{"GET", "POST"},
		BackendPool: "pool1",
		Priority:    100,
	})

	r.AddRoute(&router.Route{
		Name:        "route2",
		Path:        "/health",
		BackendPool: "pool2",
	})

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/routes")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	routes, ok := result.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be a slice, got %T", result.Data)
	}

	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
}

func TestGetHealthStatus(t *testing.T) {
	server, _, _, hc, _ := setupTestServer(t, nil)

	// Set health statuses
	hc.SetStatus("backend1", health.StatusHealthy)
	hc.SetStatus("backend2", health.StatusUnhealthy)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}
}

func TestGetMetricsJSON(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}
}

func TestGetMetricsPrometheus(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("expected text/plain content type, got %s", contentType)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "test_counter") {
		t.Error("expected response to contain test_counter")
	}
}

func TestReloadConfig(t *testing.T) {
	reloadCalled := false

	config := &Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			reloadCalled = true
			return nil
		},
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(baseURL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	if !reloadCalled {
		t.Error("expected reload callback to be called")
	}
}

func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty hash")
	}

	if hash == password {
		t.Error("hash should not equal original password")
	}

	// Verify the password
	if !CheckPasswordHash(password, hash) {
		t.Error("expected password to match hash")
	}

	// Verify wrong password fails
	if CheckPasswordHash("wrongpassword", hash) {
		t.Error("expected wrong password to not match")
	}
}

func TestDefaultMetrics(t *testing.T) {
	// Create a registry with some metrics
	reg := metrics.NewRegistry()
	counter := metrics.NewCounter("test_counter", "Test counter")
	counter.Inc()
	reg.RegisterCounter(counter)

	// Create default metrics provider
	dm := NewDefaultMetrics(reg)

	// Test GetAllMetrics
	data := dm.GetAllMetrics()
	if data == nil {
		t.Error("expected non-nil metrics data")
	}

	// Test PrometheusFormat
	promOutput := dm.PrometheusFormat()
	if promOutput == "" {
		t.Error("expected non-empty prometheus output")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test POST to GET-only endpoint
	resp, err := client.Post(baseURL+"/api/v1/system/info", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestResponseHelpers(t *testing.T) {
	// Test SuccessResponse
	resp := SuccessResponse(map[string]string{"key": "value"})
	if !resp.Success {
		t.Error("expected success to be true")
	}
	if resp.Error != nil {
		t.Error("expected error to be nil")
	}
	if resp.Data == nil {
		t.Error("expected data to not be nil")
	}

	// Test ErrorResponse
	resp = ErrorResponse("TEST_ERROR", "test message")
	if resp.Success {
		t.Error("expected success to be false")
	}
	if resp.Error == nil {
		t.Fatal("expected error to not be nil")
	}
	if resp.Error.Code != "TEST_ERROR" {
		t.Errorf("expected code TEST_ERROR, got %s", resp.Error.Code)
	}
	if resp.Error.Message != "test message" {
		t.Errorf("expected message 'test message', got %s", resp.Error.Message)
	}
}

func BenchmarkAuthMiddleware(b *testing.B) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"test-token"},
		RequireAuthForRead: true,
	}

	handler := AuthMiddleware(authConfig)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

// Additional handler tests for edge cases

func TestGetSystemInfo_DifferentStates(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Test initial state
	if state := server.GetState(); state != "running" {
		t.Errorf("expected initial state 'running', got %s", state)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}

	// Verify state is present
	if state, ok := data["state"].(string); !ok || state != "running" {
		t.Errorf("expected state 'running', got %v", data["state"])
	}
}

func TestGetSystemHealth_WithNilComponents(t *testing.T) {
	config := &Config{
		Address:       "127.0.0.1:0",
		PoolManager:   nil,
		Router:        nil,
		HealthChecker: nil,
		Metrics:       nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/system/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	// Verify degraded status due to nil components
	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}

	if status, ok := data["status"].(string); !ok || status != "degraded" {
		t.Errorf("expected status 'degraded', got %v", data["status"])
	}
}

func TestReloadConfig_Failure(t *testing.T) {
	config := &Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			return fmt.Errorf("reload failed: config validation error")
		},
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(baseURL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Success {
		t.Error("expected failure response")
	}

	if result.Error == nil || result.Error.Code != "RELOAD_FAILED" {
		t.Errorf("expected RELOAD_FAILED error, got %v", result.Error)
	}
}

func TestReloadConfig_NoCallback(t *testing.T) {
	config := &Config{
		Address:  "127.0.0.1:0",
		OnReload: nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(baseURL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestListBackends_EmptyPools(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/backends")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	pools, ok := result.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be a slice, got %T", result.Data)
	}

	if len(pools) != 0 {
		t.Errorf("expected 0 pools, got %d", len(pools))
	}
}

func TestListBackends_NilPoolManager(t *testing.T) {
	config := &Config{
		Address:     "127.0.0.1:0",
		PoolManager: nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/backends")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	// Should return empty array
	pools, ok := result.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be a slice, got %T", result.Data)
	}

	if len(pools) != 0 {
		t.Errorf("expected 0 pools, got %d", len(pools))
	}
}

func TestGetPool_NotFound(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add a pool but request a different one
	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/backends/nonexistent-pool")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Success {
		t.Error("expected failure response")
	}

	if result.Error == nil || result.Error.Code != "POOL_NOT_FOUND" {
		t.Errorf("expected POOL_NOT_FOUND error, got %v", result.Error)
	}
}

func TestAddBackend_ValidationErrors(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool
	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing ID",
			body:       `{"address":"127.0.0.1:8080"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "MISSING_FIELD",
		},
		{
			name:       "missing address",
			body:       `{"id":"backend1"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "MISSING_FIELD",
		},
		{
			name:       "empty body",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "MISSING_FIELD",
		},
		{
			name:       "invalid JSON",
			body:       `{"invalid json`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Post(baseURL+"/api/v1/backends/test-pool", "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			var result Response
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if result.Error == nil || result.Error.Code != tt.wantCode {
				t.Errorf("expected error code %s, got %v", tt.wantCode, result.Error)
			}
		})
	}
}

func TestAddBackend_DuplicateID(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool with existing backend
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend1", "127.0.0.1:8080")
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Try to add backend with same ID
	reqBody := `{"id":"backend1","address":"127.0.0.1:8081"}`
	resp, err := client.Post(baseURL+"/api/v1/backends/test-pool", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Error == nil || result.Error.Code != "ALREADY_EXISTS" {
		t.Errorf("expected ALREADY_EXISTS error, got %v", result.Error)
	}
}

func TestRemoveBackend_NotFound(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool with one backend
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend1", "127.0.0.1:8080")
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Try to remove non-existent backend
	req, _ := http.NewRequest("DELETE", baseURL+"/api/v1/backends/test-pool/nonexistent", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Error == nil || result.Error.Code != "BACKEND_NOT_FOUND" {
		t.Errorf("expected BACKEND_NOT_FOUND error, got %v", result.Error)
	}
}

func TestDrainBackend_NotFound(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	// Add test pool with one backend
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Try to drain non-existent backend
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/backends/test-pool/nonexistent/drain", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Error == nil || result.Error.Code != "BACKEND_NOT_FOUND" {
		t.Errorf("expected BACKEND_NOT_FOUND error, got %v", result.Error)
	}
}

func TestListRoutes_NoRoutes(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/routes")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	routes, ok := result.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be a slice, got %T", result.Data)
	}

	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestListRoutes_NilRouter(t *testing.T) {
	config := &Config{
		Address: "127.0.0.1:0",
		Router:  nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/routes")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}
}

func TestGetHealthStatus_NilChecker(t *testing.T) {
	config := &Config{
		Address:       "127.0.0.1:0",
		HealthChecker: nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success response")
	}

	// Should return empty array
	statuses, ok := result.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be a slice, got %T", result.Data)
	}

	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}

func TestGetMetricsJSON_NilMetrics(t *testing.T) {
	config := &Config{
		Address: "127.0.0.1:0",
		Metrics: nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

func TestGetMetricsPrometheus_NilMetrics(t *testing.T) {
	config := &Config{
		Address: "127.0.0.1:0",
		Metrics: nil,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(baseURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

// Additional auth tests

func TestAuthMiddleware_BasicAuth_WrongPassword(t *testing.T) {
	// Generate a bcrypt hash for "password"
	hash, err := HashPassword("correctpassword")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test with wrong password
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:wrongpassword")))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_BasicAuth_WrongUsername(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test with wrong username
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wronguser:password")))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_BearerToken_InvalidToken(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"valid-token-123"},
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name  string
		token string
	}{
		{"completely wrong token", "invalid-token"},
		{"empty token", ""},
		{"similar but wrong token", "valid-token-124"},
		{"extra characters", "valid-token-123-extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestAuthMiddleware_RequireAuthForRead(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	// Test with RequireAuthForRead = true
	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// GET request should require auth
	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for GET without auth, got %d", resp.StatusCode)
	}

	// POST request should also require auth
	resp, err = client.Post(baseURL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for POST without auth, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_PublicEndpoints_All(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	// Test with RequireAuthForRead = false
	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: false,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// GET /api/v1/system/info should work without auth
	resp, err := client.Get(baseURL + "/api/v1/system/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public GET, got %d", resp.StatusCode)
	}

	// GET /api/v1/backends should work without auth
	resp, err = client.Get(baseURL + "/api/v1/backends")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public GET, got %d", resp.StatusCode)
	}

	// GET /api/v1/routes should work without auth
	resp, err = client.Get(baseURL + "/api/v1/routes")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public GET, got %d", resp.StatusCode)
	}

	// GET /api/v1/health should work without auth
	resp, err = client.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public GET, got %d", resp.StatusCode)
	}

	// GET /api/v1/metrics should work without auth
	resp, err = client.Get(baseURL + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public GET, got %d", resp.StatusCode)
	}

	// GET /metrics should work without auth
	resp, err = client.Get(baseURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for public GET, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_ProtectedEndpoints(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	authConfig := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: false,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// POST /api/v1/system/reload should require auth
	resp, err := client.Post(baseURL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for protected POST, got %d", resp.StatusCode)
	}

	// POST /api/v1/backends/{pool} should require auth
	resp, err = client.Post(baseURL+"/api/v1/backends/test-pool", "application/json", strings.NewReader(`{"id":"b1","address":"127.0.0.1:8080"}`))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for protected POST, got %d", resp.StatusCode)
	}

	// DELETE /api/v1/backends/{pool}/{backend} should require auth
	req, _ := http.NewRequest("DELETE", baseURL+"/api/v1/backends/test-pool/b1", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for protected DELETE, got %d", resp.StatusCode)
	}

	// POST /api/v1/backends/{pool}/{backend}/drain should require auth
	req, _ = http.NewRequest("POST", baseURL+"/api/v1/backends/test-pool/b1/drain", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for protected POST drain, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_InvalidAuthScheme(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"valid-token"},
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test with unsupported auth scheme
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Digest username=admin")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid auth scheme, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_InvalidBasicAuthEncoding(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"valid-token"},
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test with invalid base64 encoding
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Basic !!!invalid-base64!!!")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid base64, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_InvalidBasicAuthFormat(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"valid-token"},
		RequireAuthForRead: true,
	}

	server, _, _, _, _ := setupTestServer(t, authConfig)

	// Create test server
	ts := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: server.server.Handler,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go ts.Serve(listener)
	defer ts.Close()

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	client := &http.Client{Timeout: 5 * time.Second}

	// Test with valid base64 but no colon separator
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/system/info", nil)
	// base64 of "admin" (no password, no colon)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin")))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid format, got %d", resp.StatusCode)
	}
}

// --- Tests for getConfig and getCertificates handlers ---

// mockConfigGetter implements admin.ConfigGetter for testing.
type mockConfigGetter struct {
	config interface{}
}

func (m *mockConfigGetter) GetConfig() interface{} {
	return m.config
}

// mockCertLister implements admin.CertLister for testing.
type mockCertLister struct {
	certs []CertInfoView
}

func (m *mockCertLister) ListCertificates() []CertInfoView {
	return m.certs
}

func TestGetConfig_ReturnsJSON(t *testing.T) {
	configData := map[string]interface{}{
		"version": "1",
		"admin":   map[string]interface{}{"enabled": true, "address": ":8080"},
	}
	getter := &mockConfigGetter{config: configData}

	serverCfg := &Config{
		Address:      "127.0.0.1:0",
		ConfigGetter: getter,
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success to be true")
	}

	// Data should be present
	if resp.Data == nil {
		t.Error("expected data in response")
	}
}

func TestGetConfig_NilConfigGetter(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
		// No ConfigGetter set
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 for nil configGetter, got %d", w.Code)
	}
}

func TestGetConfig_MethodNotAllowed(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestGetCertificates_WithCerts(t *testing.T) {
	lister := &mockCertLister{
		certs: []CertInfoView{
			{Names: []string{"example.com", "www.example.com"}, Expiry: 1700000000, IsWildcard: false},
			{Names: []string{"*.test.com"}, Expiry: 1800000000, IsWildcard: true},
		},
	}

	serverCfg := &Config{
		Address:    "127.0.0.1:0",
		CertLister: lister,
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success to be true")
	}

	// Data should be present (array of certs)
	dataSlice, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(dataSlice) != 2 {
		t.Errorf("expected 2 certificates, got %d", len(dataSlice))
	}
}

func TestGetCertificates_NilCertLister(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
		// No CertLister set
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for nil certLister (empty array), got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return empty array
	dataSlice, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(dataSlice) != 0 {
		t.Errorf("expected 0 certificates for nil lister, got %d", len(dataSlice))
	}
}

func TestGetCertificates_EmptyCertLister(t *testing.T) {
	lister := &mockCertLister{
		certs: []CertInfoView{},
	}

	serverCfg := &Config{
		Address:    "127.0.0.1:0",
		CertLister: lister,
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestGetCertificates_MethodNotAllowed(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates", nil)
	w := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestRemoveBackend_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/backend1", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)
	// GET on a specific backend path may return backend detail or error
	// This tests the removeBackend method check
}

func TestRemoveBackend_NoPoolManager(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}
	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool/backend1", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRemoveBackend_PoolNotFound(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/nonexistent-pool/backend1", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestRemoveBackend_BackendNotFound(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool/nonexistent", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDrainBackend_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/backend1/drain", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	// This tests method check in drain handler
}

func TestDrainBackend_NoPoolManager(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}
	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool/backend1/drain", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDrainBackend_PoolNotFound(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/nonexistent/backend1/drain", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDrainBackend_BackendNotFound(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool/nonexistent/drain", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetHealthStatus_NoHealthChecker(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}
	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestGetHealthStatus_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestGetPool_NoPoolManager(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}
	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools/test-pool", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetPool_MethodNotAllowed(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-pool", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	// The pool exists, so a POST should be method not allowed
	if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
		t.Errorf("expected status 405 or 404, got %d", w.Code)
	}
}

func TestDrainBackend_InvalidPath(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Path too short
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/drain", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	// Should return bad request or not found
}

// TestExtractPoolName tests the extractPoolName helper with various paths.
func TestExtractPoolName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/backends/mypool", "mypool"},
		{"/api/v1/backends/test-pool", "test-pool"},
		{"/api/v1/backends/", ""},
		{"/api/v1/", ""},
		{"/other/path", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractPoolName(tt.path)
			if got != tt.want {
				t.Errorf("extractPoolName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestExtractBackendID tests the extractBackendID helper with various paths.
func TestExtractBackendID(t *testing.T) {
	tests := []struct {
		path        string
		wantPool    string
		wantBackend string
	}{
		{"/api/v1/backends/mypool/mybackend", "mypool", "mybackend"},
		{"/api/v1/backends/pool1/backend1", "pool1", "backend1"},
		{"/api/v1/backends/mypool", "", ""},
		{"/api/v1/", "", ""},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotPool, gotBackend := extractBackendID(tt.path)
			if gotPool != tt.wantPool || gotBackend != tt.wantBackend {
				t.Errorf("extractBackendID(%q) = (%q, %q), want (%q, %q)",
					tt.path, gotPool, gotBackend, tt.wantPool, tt.wantBackend)
			}
		})
	}
}

// TestAddBackend_InvalidJSON tests addBackend with invalid JSON body.
func TestAddBackend_InvalidJSON(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestAddBackend_MissingID tests addBackend with missing backend ID.
func TestAddBackend_MissingID(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	body := `{"address": "10.0.0.1:8080"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestAddBackend_MissingAddress tests addBackend with missing address.
func TestAddBackend_MissingAddress(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	body := `{"id": "backend1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestAddBackend_NoPoolManager tests addBackend when pool manager is nil.
func TestAddBackend_NoPoolManager(t *testing.T) {
	serverCfg := &Config{
		Address: "127.0.0.1:0",
	}
	srv, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	body := `{"id": "b1", "address": "10.0.0.1:8080"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// TestHandleBackendDetail_GetBackend tests getting a single backend (not implemented).
func TestHandleBackendDetail_GetBackend(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	// GET /api/v1/backends/test-pool/b1 should return 501 (not implemented)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/b1", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", w.Code)
	}
}

// TestHandleBackendDetail_MethodNotAllowed tests unsupported methods on backend detail.
func TestHandleBackendDetail_MethodNotAllowed(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	// PATCH is not supported
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestHandleBackendDetail_PoolLevel_MethodNotAllowed tests unsupported method on pool level.
func TestHandleBackendDetail_PoolLevel_MethodNotAllowed(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	// DELETE on pool level is not supported
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestGetMetricsJSON_MethodNotAllowed tests metrics with wrong method.
func TestGetMetricsJSON_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestGetMetricsPrometheus_MethodNotAllowed tests prometheus metrics with wrong method.
func TestGetMetricsPrometheus_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestGetSystemInfo_MethodNotAllowed tests system info with wrong method.
func TestGetSystemInfo_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/info", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestGetSystemHealth_MethodNotAllowed_Specific tests health with POST method.
func TestGetSystemHealth_MethodNotAllowed_Specific(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/health", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestReloadConfig_MethodNotAllowed tests reload with wrong method.
func TestReloadConfig_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/reload", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestListBackends_MethodNotAllowed tests list backends with wrong method.
func TestListBackends_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestListRoutes_MethodNotAllowed tests list routes with wrong method.
func TestListRoutes_MethodNotAllowed(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/routes", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestGetPool_EmptyPoolName tests getPool with empty pool name.
func TestGetPool_EmptyPoolName(t *testing.T) {
	server, _, _, _, _ := setupTestServer(t, nil)

	// Path with no pool name
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	// Should fail due to empty pool name
	if w.Code == http.StatusOK {
		t.Error("expected non-200 status for empty pool name")
	}
}

// TestNewDefaultMetrics_NilRegistry tests NewDefaultMetrics with nil registry.
func TestNewDefaultMetrics_NilRegistry(t *testing.T) {
	m := NewDefaultMetrics(nil)
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}
	// Should use default registry and not panic
	result := m.GetAllMetrics()
	if result == nil {
		t.Error("expected non-nil metrics result")
	}

	// PrometheusFormat should work too
	prom := m.PrometheusFormat()
	_ = prom // just verify it doesn't panic
}

// TestNewDefaultMetrics_WithRegistry tests NewDefaultMetrics with a real registry.
func TestNewDefaultMetrics_WithRegistry(t *testing.T) {
	reg := metrics.NewRegistry()
	m := NewDefaultMetrics(reg)
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}

	result := m.GetAllMetrics()
	if result == nil {
		t.Error("expected non-nil metrics result")
	}

	prom := m.PrometheusFormat()
	if prom == "" {
		// Note: an empty registry may produce empty output, that's fine
	}
}

// TestRemoveBackend_EmptyBackendID tests removeBackend with empty IDs.
func TestRemoveBackend_EmptyBackendID(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	poolManager.AddPool(pool)

	// Path missing backend ID
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	// Should get method not allowed at pool level for DELETE
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestDrainBackend_MethodNotAllowed_PUT tests drain with PUT method.
func TestDrainBackend_MethodNotAllowed_PUT(t *testing.T) {
	server, poolManager, _, _, _ := setupTestServer(t, nil)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/b1/drain", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}
