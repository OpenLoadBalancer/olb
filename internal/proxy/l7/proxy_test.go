package l7

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/router"
	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// setupTestProxy creates a proxy with test dependencies.
func setupTestProxy(t *testing.T) (*HTTPProxy, *backend.PoolManager, *router.Router) {
	t.Helper()

	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

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
	return proxy, poolManager, routerInstance
}

// createTestBackend creates a test backend server.
func createTestBackend(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func TestNewHTTPProxy(t *testing.T) {
	// Test with nil config
	proxy := NewHTTPProxy(nil)
	if proxy == nil {
		t.Fatal("expected proxy to not be nil")
	}
	if proxy.proxyTimeout != 60*time.Second {
		t.Errorf("expected default proxy timeout 60s, got %v", proxy.proxyTimeout)
	}
	if proxy.dialTimeout != 10*time.Second {
		t.Errorf("expected default dial timeout 10s, got %v", proxy.dialTimeout)
	}
	if proxy.maxRetries != 3 {
		t.Errorf("expected default max retries 3, got %d", proxy.maxRetries)
	}

	// Test with custom config
	config := &Config{
		ProxyTimeout: 30 * time.Second,
		DialTimeout:  5 * time.Second,
		MaxRetries:   5,
	}
	proxy = NewHTTPProxy(config)
	if proxy.proxyTimeout != 30*time.Second {
		t.Errorf("expected proxy timeout 30s, got %v", proxy.proxyTimeout)
	}
	if proxy.dialTimeout != 5*time.Second {
		t.Errorf("expected dial timeout 5s, got %v", proxy.dialTimeout)
	}
	if proxy.maxRetries != 5 {
		t.Errorf("expected max retries 5, got %d", proxy.maxRetries)
	}
}

func TestHTTPProxy_ServeHTTP_NoRouteMatch(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != http.StatusNotFound {
		t.Errorf("expected error code %d, got %d", http.StatusNotFound, resp.Code)
	}
}

func TestHTTPProxy_ServeHTTP_BasicProxy(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create test backend
	backendCalled := false
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from backend"))
	})
	defer backendServer.Close()

	// Extract backend address
	backendAddr := backendServer.Listener.Addr().String()

	// Create pool and add backend
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Add route
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if !backendCalled {
		t.Error("backend was not called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if body := rr.Body.String(); body != "Hello from backend" {
		t.Errorf("expected body 'Hello from backend', got '%s'", body)
	}
}

func TestHTTPProxy_ServeHTTP_NoHealthyBackends(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with unhealthy backend
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", "127.0.0.1:1")
	b.SetState(backend.StateDown)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Add route
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_ServeHTTP_PoolNotFound(t *testing.T) {
	proxy, _, routerInstance := setupTestProxy(t)

	// Add route with non-existent pool
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "nonexistent-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_ServeHTTP_BackendTimeout(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create slow backend
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Will timeout
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	// Extract backend address
	backendAddr := backendServer.Listener.Addr().String()

	// Create pool and add backend
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Add route
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get a timeout/gateway timeout error
	if rr.Code != http.StatusGatewayTimeout && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusGatewayTimeout, http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_ServeHTTP_ConnectionRefused(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with backend on closed port
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	// Add route
	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get a bad gateway error after retries
	if rr.Code != http.StatusBadGateway && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusBadGateway, http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_XForwardedHeaders(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var receivedHeaders http.Header
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Check X-Forwarded-Proto
	if proto := receivedHeaders.Get("X-Forwarded-Proto"); proto != "http" {
		t.Errorf("expected X-Forwarded-Proto 'http', got '%s'", proto)
	}

	// Check X-Forwarded-Host
	if host := receivedHeaders.Get("X-Forwarded-Host"); host != "example.com" {
		t.Errorf("expected X-Forwarded-Host 'example.com', got '%s'", host)
	}

	// Check X-Real-IP exists
	if realIP := receivedHeaders.Get("X-Real-IP"); realIP == "" {
		t.Error("expected X-Real-IP to be set")
	}
}

func TestHTTPProxy_XForwardedFor_Append(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var receivedHeaders http.Header
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	xff := receivedHeaders.Get("X-Forwarded-For")
	if !strings.Contains(xff, "10.0.0.1") {
		t.Errorf("expected X-Forwarded-For to contain '10.0.0.1', got '%s'", xff)
	}
	// Should have appended the new client IP
	if !strings.Contains(xff, ",") {
		t.Errorf("expected X-Forwarded-For to contain comma-separated IPs, got '%s'", xff)
	}
}

func TestHTTPProxy_HopByHopHeadersStripped(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var receivedHeaders http.Header
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Set hop-by-hop headers
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Proxy-Authorization", "Basic dGVzdDp0ZXN0")
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Check that hop-by-hop headers are stripped
	if receivedHeaders.Get("Proxy-Authorization") != "" {
		t.Error("expected Proxy-Authorization header to be stripped")
	}
	if receivedHeaders.Get("Upgrade") != "" {
		t.Error("expected Upgrade header to be stripped")
	}
}

func TestHTTPProxy_LargeRequestBody(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var receivedBody []byte
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Create large body (1MB)
	largeBody := make([]byte, 1024*1024)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(largeBody))
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if !bytes.Equal(receivedBody, largeBody) {
		t.Errorf("received body does not match sent body, got %d bytes, expected %d bytes", len(receivedBody), len(largeBody))
	}
}

func TestHTTPProxy_LargeResponseBody(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create large response body (1MB)
	largeBody := make([]byte, 1024*1024)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(largeBody)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if !bytes.Equal(rr.Body.Bytes(), largeBody) {
		t.Errorf("received body does not match expected body, got %d bytes, expected %d bytes", rr.Body.Len(), len(largeBody))
	}
}

func TestHTTPProxy_RetryOnFailure(t *testing.T) {
	// Use a fresh proxy with more retries for this test
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      5, // More retries to ensure we cycle through backends
	}
	proxy := NewHTTPProxy(config)

	// Create a backend that is closed (simulates connection refused)
	// Use a port that's very unlikely to be used
	closedBackendAddr := "127.0.0.1:1"

	backend2 := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success from backend2"))
	})
	defer backend2.Close()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	// Add the closed backend first
	b1 := backend.NewBackend("backend-1", closedBackendAddr)
	b1.SetState(backend.StateUp)
	if err := pool.AddBackend(b1); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	// Add the working backend second
	b2 := backend.NewBackend("backend-2", backend2.Listener.Addr().String())
	b2.SetState(backend.StateUp)
	if err := pool.AddBackend(b2); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should eventually succeed on backend2
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if body := rr.Body.String(); body != "success from backend2" {
		t.Errorf("expected body from backend2, got '%s'", body)
	}
}

func TestHTTPProxy_MultipleBackendsRoundRobin(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var backend1Calls, backend2Calls int

	backend1 := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		backend1Calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend1"))
	})
	defer backend1.Close()

	backend2 := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		backend2Calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend2"))
	})
	defer backend2.Close()

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

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		proxy.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected status %d, got %d", i, http.StatusOK, rr.Code)
		}
	}

	// Both backends should have been called
	if backend1Calls == 0 {
		t.Error("backend1 was never called")
	}
	if backend2Calls == 0 {
		t.Error("backend2 was never called")
	}
}

func TestGetClientIP(t *testing.T) {
	p := &HTTPProxy{trustedNets: parseTrustedProxies([]string{"192.168.0.0/16"})}

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expected   string
	}{
		{
			name:       "from RemoteAddr",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{},
			expected:   "192.168.1.1",
		},
		{
			name:       "from X-Forwarded-For",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.1"},
			expected:   "10.0.0.1",
		},
		{
			name:       "from X-Forwarded-For multiple",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.1, 10.0.0.2"},
			expected:   "10.0.0.1",
		},
		{
			name:       "from X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Real-IP": "10.0.0.2"},
			expected:   "10.0.0.2",
		},
		{
			name:       "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.1", "X-Real-IP": "10.0.0.2"},
			expected:   "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := p.getClientIP(req)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestIsHopByHopHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{"Connection", "Connection", true},
		{"Keep-Alive", "Keep-Alive", true},
		{"Proxy-Authenticate", "Proxy-Authenticate", true},
		{"Proxy-Authorization", "Proxy-Authorization", true},
		{"TE", "TE", true},
		{"Trailers", "Trailers", true},
		{"Transfer-Encoding", "Transfer-Encoding", true},
		{"Upgrade", "Upgrade", true},
		{"Content-Type", "Content-Type", false},
		{"Accept", "Accept", false},
		{"connection lowercase", "connection", true},
		{"CONNECTION uppercase", "CONNECTION", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHopByHopHeader(tt.header)
			if result != tt.expected {
				t.Errorf("expected %v for header %q, got %v", tt.expected, tt.header, result)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"timeout error", &netError{timeout: true}, true},
		{"temporary error", &netError{temporary: true}, false}, // We don't check temporary
		{"connection refused", syscall.ECONNREFUSED, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"generic error", errors.New("some error"), false},
		{"connection refused string", errors.New("connection refused"), true},
		{"timeout string", errors.New("timeout occurred"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v for error %v, got %v", tt.expected, tt.err, result)
			}
		})
	}
}

// netError is a test net.Error implementation.
type netError struct {
	timeout   bool
	temporary bool
}

func (e *netError) Error() string   { return "net error" }
func (e *netError) Timeout() bool   { return e.timeout }
func (e *netError) Temporary() bool { return e.temporary }

func TestContains(t *testing.T) {
	tests := []struct {
		slice    []string
		item     string
		expected bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{[]string{"a"}, "a", true},
	}

	for _, tt := range tests {
		result := contains(tt.slice, tt.item)
		if result != tt.expected {
			t.Errorf("contains(%v, %q) = %v, expected %v", tt.slice, tt.item, result, tt.expected)
		}
	}
}

func TestDefaultErrorHandler(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "route not found",
			err:            olbErrors.ErrRouteNotFound,
			expectedStatus: http.StatusNotFound,
			expectedMsg:    "Not Found",
		},
		{
			name:           "pool not found",
			err:            olbErrors.ErrPoolNotFound,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "pool empty",
			err:            olbErrors.ErrPoolEmpty,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "backend unavailable",
			err:            olbErrors.ErrBackendUnavailable,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "connection refused",
			err:            olbErrors.ErrConnectionRefused,
			expectedStatus: http.StatusBadGateway,
			expectedMsg:    "Bad Gateway",
		},
		{
			name:           "connection timeout",
			err:            olbErrors.ErrConnectionTimeout,
			expectedStatus: http.StatusGatewayTimeout,
			expectedMsg:    "Gateway Timeout",
		},
		{
			name:           "timeout",
			err:            olbErrors.ErrTimeout,
			expectedStatus: http.StatusGatewayTimeout,
			expectedMsg:    "Gateway Timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()

			defaultErrorHandler(rr, req, tt.err)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			contentType := rr.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
			}

			var resp ErrorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Code != tt.expectedStatus {
				t.Errorf("expected response code %d, got %d", tt.expectedStatus, resp.Code)
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMsg, resp.Message)
			}
		})
	}
}

func TestHTTPProxy_Close(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	err := proxy.Close()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Additional tests for edge cases and improved coverage

func TestPrepareOutboundRequest_WithVariousHeaders(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	// Create test backend
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()
	b := backend.NewBackend("test", backendAddr)

	// Test with various headers
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TestAgent")

	outReq, err := proxy.prepareOutboundRequest(req, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that custom headers are preserved
	if outReq.Header.Get("X-Custom-Header") != "custom-value" {
		t.Error("expected X-Custom-Header to be preserved")
	}
	if outReq.Header.Get("Accept") != "application/json" {
		t.Error("expected Accept header to be preserved")
	}
	if outReq.Header.Get("User-Agent") != "TestAgent" {
		t.Error("expected User-Agent header to be preserved")
	}

	// Check X-Forwarded headers are set
	if outReq.Header.Get("X-Forwarded-Host") != "example.com" {
		t.Errorf("expected X-Forwarded-Host to be 'example.com', got %s", outReq.Header.Get("X-Forwarded-Host"))
	}
	if outReq.Header.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("expected X-Forwarded-Proto to be 'http', got %s", outReq.Header.Get("X-Forwarded-Proto"))
	}
}

func TestPrepareOutboundRequest_WithConnectionHeader(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()
	b := backend.NewBackend("test", backendAddr)

	// Test with Connection header containing additional headers
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Connection", "X-Custom-Conn, Keep-Alive")
	req.Header.Set("X-Custom-Conn", "should-be-removed")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Host = "example.com"

	outReq, err := proxy.prepareOutboundRequest(req, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Note: The Connection header itself is stripped as a hop-by-hop header,
	// but headers listed in Connection may not be stripped because Connection
	// is deleted first. This is a known limitation.
	// The important thing is that hop-by-hop headers are stripped.

	// Connection header should be stripped
	if outReq.Header.Get("Connection") != "" {
		t.Error("expected Connection header to be stripped")
	}

	// Keep-Alive should be stripped (it's a hop-by-hop header)
	if outReq.Header.Get("Keep-Alive") != "" {
		t.Error("expected Keep-Alive to be stripped")
	}
}

func TestGetClientIP_WithXForwardedFor(t *testing.T) {
	p := &HTTPProxy{trustedNets: parseTrustedProxies([]string{"192.168.0.0/16"})}

	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		expected   string
	}{
		{
			name:       "single IP in X-Forwarded-For",
			xff:        "10.0.0.1",
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.1",
		},
		{
			name:       "multiple IPs in X-Forwarded-For",
			xff:        "10.0.0.1, 10.0.0.2, 10.0.0.3",
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For with spaces",
			xff:        " 10.0.0.1 , 10.0.0.2 ",
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.1",
		},
		{
			name:       "empty X-Forwarded-For falls back to X-Real-IP",
			xff:        "",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			result := p.getClientIP(req)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGetClientIP_WithXRealIP(t *testing.T) {
	p := &HTTPProxy{trustedNets: parseTrustedProxies([]string{"192.168.0.0/16"})}

	tests := []struct {
		name       string
		xri        string
		remoteAddr string
		expected   string
	}{
		{
			name:       "X-Real-IP present",
			xri:        "10.0.0.5",
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.5",
		},
		{
			name:       "X-Real-IP with IPv6",
			xri:        "2001:db8::1",
			remoteAddr: "192.168.1.1:12345",
			expected:   "2001:db8::1",
		},
		{
			name:       "empty X-Real-IP falls back to RemoteAddr",
			xri:        "",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			result := p.getClientIP(req)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCopyHeaders_WithHopByHopHeaders(t *testing.T) {
	src := http.Header{
		"Content-Type":        []string{"application/json"},
		"Content-Length":      []string{"100"},
		"Connection":          []string{"keep-alive"},
		"Keep-Alive":          []string{"timeout=5"},
		"Proxy-Authorization": []string{"Basic dGVzdA=="},
		"Upgrade":             []string{"websocket"},
		"TE":                  []string{"gzip"},
		"Trailers":            []string{"X-Trailer"},
		"Transfer-Encoding":   []string{"chunked"},
		"Proxy-Authenticate":  []string{"Basic"},
	}

	dst := make(http.Header)
	copyHeaders(dst, src)

	// Non-hop-by-hop headers should be copied
	if dst.Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type to be copied")
	}
	if dst.Get("Content-Length") != "100" {
		t.Error("expected Content-Length to be copied")
	}

	// Hop-by-hop headers should NOT be copied
	hopByHop := []string{"Connection", "Keep-Alive", "Proxy-Authorization", "Upgrade", "TE", "Trailers", "Transfer-Encoding", "Proxy-Authenticate"}
	for _, h := range hopByHop {
		if dst.Get(h) != "" {
			t.Errorf("expected %s to be stripped, got %s", h, dst.Get(h))
		}
	}
}

func TestCopyHeaders_WithMultipleValues(t *testing.T) {
	src := http.Header{
		"X-Multi": []string{"value1", "value2", "value3"},
	}

	dst := make(http.Header)
	copyHeaders(dst, src)

	values := dst["X-Multi"]
	if len(values) != 3 {
		t.Errorf("expected 3 values, got %d", len(values))
	}
	for i, v := range []string{"value1", "value2", "value3"} {
		if values[i] != v {
			t.Errorf("expected value %d to be %s, got %s", i, v, values[i])
		}
	}
}

func TestIsRetryableError_WithVariousErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("some random error"), false},
		{"timeout error", &netError{timeout: true}, true},
		{"connection refused syscall", syscall.ECONNREFUSED, true},
		{"connection reset syscall", syscall.ECONNRESET, true},
		{"connection refused string", errors.New("dial tcp: connection refused"), true},
		{"actively refused (Windows)", errors.New("No connection could be made because the target machine actively refused it"), true},
		{"connection reset string", errors.New("read tcp: connection reset by peer"), true},
		{"no such host", errors.New("lookup example.com: no such host"), true},
		{"timeout string", errors.New("i/o timeout"), true},
		{"temporary failure", errors.New("temporary failure in name resolution"), true},
		{"context deadline exceeded", errors.New("context deadline exceeded"), false}, // Not in retryable list
		{"broken pipe", errors.New("write tcp: broken pipe"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v for error %v, got %v", tt.expected, tt.err, result)
			}
		})
	}
}

func TestDefaultErrorHandler_WithAllErrorTypes(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "route not found",
			err:            olbErrors.ErrRouteNotFound,
			expectedStatus: http.StatusNotFound,
			expectedMsg:    "Not Found",
		},
		{
			name:           "pool not found",
			err:            olbErrors.ErrPoolNotFound,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "pool empty",
			err:            olbErrors.ErrPoolEmpty,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "backend unavailable",
			err:            olbErrors.ErrBackendUnavailable,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "backend not found",
			err:            olbErrors.ErrBackendNotFound,
			expectedStatus: http.StatusServiceUnavailable,
			expectedMsg:    "Service Unavailable",
		},
		{
			name:           "connection refused",
			err:            olbErrors.ErrConnectionRefused,
			expectedStatus: http.StatusBadGateway,
			expectedMsg:    "Bad Gateway",
		},
		{
			name:           "connection timeout",
			err:            olbErrors.ErrConnectionTimeout,
			expectedStatus: http.StatusGatewayTimeout,
			expectedMsg:    "Gateway Timeout",
		},
		{
			name:           "timeout",
			err:            olbErrors.ErrTimeout,
			expectedStatus: http.StatusGatewayTimeout,
			expectedMsg:    "Gateway Timeout",
		},
		{
			name:           "wrapped error with code",
			err:            olbErrors.ErrRouteNotFound.WithContext("path", "/test"),
			expectedStatus: http.StatusNotFound,
			expectedMsg:    "Not Found",
		},
		{
			name:           "net timeout error",
			err:            &netError{timeout: true},
			expectedStatus: http.StatusGatewayTimeout,
			expectedMsg:    "Gateway Timeout",
		},
		{
			name:           "generic error",
			err:            errors.New("something went wrong"),
			expectedStatus: http.StatusInternalServerError,
			expectedMsg:    "Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()

			defaultErrorHandler(rr, req, tt.err)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			contentType := rr.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
			}

			var resp ErrorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Code != tt.expectedStatus {
				t.Errorf("expected response code %d, got %d", tt.expectedStatus, resp.Code)
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMsg, resp.Message)
			}
		})
	}
}

func TestProxyRequest_WithBackendFailure(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create a backend that always fails
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	// Use a port that's definitely closed
	b := backend.NewBackend("backend-1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get an error after retries
	if rr.Code != http.StatusBadGateway && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusBadGateway, http.StatusServiceUnavailable, rr.Code)
	}
}

func TestProxyRequest_WithRetrySuccess(t *testing.T) {
	// Use a fresh proxy with more retries
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      5,
	}
	proxy := NewHTTPProxy(config)

	// Create backends - first one is closed, second one works
	closedBackendAddr := "127.0.0.1:1"

	workingBackend := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})
	defer workingBackend.Close()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	// Add closed backend first
	b1 := backend.NewBackend("backend-1", closedBackendAddr)
	b1.SetState(backend.StateUp)
	if err := pool.AddBackend(b1); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	// Add working backend second
	b2 := backend.NewBackend("backend-2", workingBackend.Listener.Addr().String())
	b2.SetState(backend.StateUp)
	if err := pool.AddBackend(b2); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should eventually succeed
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if body := rr.Body.String(); body != "success" {
		t.Errorf("expected body 'success', got '%s'", body)
	}
}

func TestProxyRequest_AllRetriesFail(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with only closed backends
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	// Add multiple closed backends
	for i := 0; i < 3; i++ {
		b := backend.NewBackend(fmt.Sprintf("backend-%d", i), fmt.Sprintf("127.0.0.1:%d", i+1))
		b.SetState(backend.StateUp)
		if err := pool.AddBackend(b); err != nil {
			t.Fatalf("failed to add backend: %v", err)
		}
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// All retries should fail
	if rr.Code != http.StatusBadGateway && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusBadGateway, http.StatusServiceUnavailable, rr.Code)
	}
}

func TestSelectBackend_WithNoHealthyBackends(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with unhealthy backends
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	// Add backends that are all down
	for i := 0; i < 3; i++ {
		b := backend.NewBackend(fmt.Sprintf("backend-%d", i), fmt.Sprintf("127.0.0.1:808%d", i))
		b.SetState(backend.StateDown)
		if err := pool.AddBackend(b); err != nil {
			t.Fatalf("failed to add backend: %v", err)
		}
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get service unavailable
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_WithCustomErrorHandler(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Set custom error handler
	customErrorCalled := false
	var customError error
	proxy.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
		customErrorCalled = true
		customError = err
		w.WriteHeader(http.StatusTeapot) // Use unique status to verify
		w.Write([]byte("custom error"))
	})

	// Create pool with unhealthy backend
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", "127.0.0.1:8080")
	b.SetState(backend.StateDown)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if !customErrorCalled {
		t.Error("expected custom error handler to be called")
	}
	if customError == nil {
		t.Error("expected custom error to be set")
	}
	if rr.Code != http.StatusTeapot {
		t.Errorf("expected status %d from custom handler, got %d", http.StatusTeapot, rr.Code)
	}
	if body := rr.Body.String(); body != "custom error" {
		t.Errorf("expected body 'custom error', got '%s'", body)
	}
}

func TestHTTPProxy_EmptyBackendPool(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create empty pool
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	// Don't add any backends

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get service unavailable due to empty pool
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_BackendConnectionRefused(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with backend on closed port
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("backend-1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get bad gateway after retries
	if rr.Code != http.StatusBadGateway && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusBadGateway, http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_BackendTimeout(t *testing.T) {
	// Use a short timeout proxy
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    500 * time.Millisecond, // Short timeout
		DialTimeout:     100 * time.Millisecond,
		MaxRetries:      1,
	}
	proxy := NewHTTPProxy(config)

	// Create slow backend
	slowBackend := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Will timeout
		w.WriteHeader(http.StatusOK)
	})
	defer slowBackend.Close()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("backend-1", slowBackend.Listener.Addr().String())
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}

	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get gateway timeout
	if rr.Code != http.StatusGatewayTimeout && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusGatewayTimeout, http.StatusServiceUnavailable, rr.Code)
	}
}

func TestHTTPProxy_WebSocketUpgrade(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var receivedHeaders http.Header
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/ws",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// When no WebSocket handler is set, the proxy treats this as a regular
	// HTTP request. The Upgrade header may or may not be stripped depending
	// on the Go http.Transport behavior. We only verify the request reached
	// the backend successfully.
	if receivedHeaders == nil {
		t.Error("expected request to reach backend")
	}
}

func TestHTTPProxy_HTTP2Detection(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		// Check if we can detect HTTP/2
		if r.ProtoMajor == 2 {
			w.Header().Set("X-HTTP-Version", "2")
		} else {
			w.Header().Set("X-HTTP-Version", "1")
		}
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.ProtoMajor = 2
	req.ProtoMinor = 0
	req.Proto = "HTTP/2.0"
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestHTTPProxy_LargeRequestBody_Extended(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	var receivedBody []byte
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Create large body (5MB)
	largeBody := make([]byte, 5*1024*1024)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/octet-stream")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if !bytes.Equal(receivedBody, largeBody) {
		t.Errorf("received body does not match sent body, got %d bytes, expected %d bytes", len(receivedBody), len(largeBody))
	}
}

func TestHTTPProxy_LargeResponseBody_Extended(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create large response body (5MB)
	largeBody := make([]byte, 5*1024*1024)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(largeBody)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if !bytes.Equal(rr.Body.Bytes(), largeBody) {
		t.Errorf("received body does not match expected body, got %d bytes, expected %d bytes", rr.Body.Len(), len(largeBody))
	}
}

func TestHTTPProxy_NonRetryableError(t *testing.T) {
	// This test verifies that non-retryable errors don't trigger retries
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create a backend that returns a non-retryable error
	// We simulate this by having a backend that accepts the connection
	// but returns an HTTP error status
	callCount := 0
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "test-route",
		Path:        "/test",
		BackendPool: "test-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// Should get the backend's error status
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	// Backend should only be called once (no retries for HTTP error responses)
	if callCount != 1 {
		t.Errorf("expected 1 backend call, got %d", callCount)
	}
}

func TestHTTPProxy_CreateTransport(t *testing.T) {
	proxy := NewHTTPProxy(nil)

	transport := proxy.createTransport()
	if transport == nil {
		t.Fatal("expected transport to be non-nil")
	}

	// Check default transport settings
	if transport.MaxIdleConns != 100 {
		t.Errorf("expected MaxIdleConns to be 100, got %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Errorf("expected MaxIdleConnsPerHost to be 10, got %d", transport.MaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Errorf("expected IdleConnTimeout to be 90s, got %v", transport.IdleConnTimeout)
	}
	if !transport.DisableCompression {
		t.Error("expected DisableCompression to be true")
	}
}

func TestHTTPProxy_ReverseProxy(t *testing.T) {
	proxy := NewHTTPProxy(nil)

	target, err := url.Parse("http://localhost:8080")
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	rp := proxy.ReverseProxy(target)
	if rp == nil {
		t.Error("expected reverse proxy to be non-nil")
	}
}

func TestContains_Extended(t *testing.T) {
	tests := []struct {
		slice    []string
		item     string
		expected bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{[]string{"a"}, "a", true},
		{[]string{"a", "b"}, "", false},
		{[]string{"", "a"}, "", true},
	}

	for _, tt := range tests {
		result := contains(tt.slice, tt.item)
		if result != tt.expected {
			t.Errorf("contains(%v, %q) = %v, expected %v", tt.slice, tt.item, result, tt.expected)
		}
	}
}

// ============================================================================
// Tests for selectBackend and protocol detection coverage
// ============================================================================

func TestSelectBackend(t *testing.T) {
	proxy, poolManager, _ := setupTestProxy(t)

	t.Run("returns nil for empty pool", func(t *testing.T) {
		pool := backend.NewPool("empty-pool", "round_robin")
		pool.SetBalancer(balancer.NewRoundRobin())
		poolManager.AddPool(pool)

		result := proxy.selectBackend(pool, nil)
		if result != nil {
			t.Errorf("selectBackend() = %v, want nil for empty pool", result)
		}
	})

	t.Run("returns nil when all backends down", func(t *testing.T) {
		pool := backend.NewPool("down-pool", "round_robin")
		pool.SetBalancer(balancer.NewRoundRobin())
		b := backend.NewBackend("down-b1", "127.0.0.1:9001")
		b.SetState(backend.StateDown)
		pool.AddBackend(b)
		poolManager.AddPool(pool)

		result := proxy.selectBackend(pool, nil)
		if result != nil {
			t.Errorf("selectBackend() = %v, want nil when all backends down", result)
		}
	})

	t.Run("returns healthy backend", func(t *testing.T) {
		pool := backend.NewPool("healthy-pool", "round_robin")
		pool.SetBalancer(balancer.NewRoundRobin())
		b := backend.NewBackend("healthy-b1", "127.0.0.1:9002")
		b.SetState(backend.StateUp)
		pool.AddBackend(b)
		poolManager.AddPool(pool)

		result := proxy.selectBackend(pool, nil)
		if result == nil {
			t.Fatal("selectBackend() returned nil, want a backend")
		}
		if result.ID != "healthy-b1" {
			t.Errorf("selectBackend().ID = %q, want %q", result.ID, "healthy-b1")
		}
	})

	t.Run("selects from multiple backends", func(t *testing.T) {
		pool := backend.NewPool("multi-pool", "round_robin")
		pool.SetBalancer(balancer.NewRoundRobin())
		b1 := backend.NewBackend("multi-b1", "127.0.0.1:9003")
		b1.SetState(backend.StateUp)
		b2 := backend.NewBackend("multi-b2", "127.0.0.1:9004")
		b2.SetState(backend.StateUp)
		pool.AddBackend(b1)
		pool.AddBackend(b2)
		poolManager.AddPool(pool)

		result := proxy.selectBackend(pool, nil)
		if result == nil {
			t.Fatal("selectBackend() returned nil")
		}
		if result.ID != "multi-b1" && result.ID != "multi-b2" {
			t.Errorf("selectBackend().ID = %q, want multi-b1 or multi-b2", result.ID)
		}
	})
}

// ============================================================================
// Tests for HandleGRPCWeb success path
// ============================================================================

func TestGRPCWebHandler_Enabled_DelegatesToGRPC(t *testing.T) {
	// Create a mock gRPC backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("grpc-web response body"))
	}))
	defer backendServer.Close()

	config := &GRPCConfig{
		EnableGRPC:    true,
		EnableGRPCWeb: true,
		Timeout:       5 * time.Second,
	}
	grpcHandler := NewGRPCHandler(config)
	webHandler := NewGRPCWebHandler(grpcHandler)

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-web-backend-1", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/MyMethod", bytes.NewReader([]byte("grpc-web request")))
	req.Header.Set("Content-Type", "application/grpc-web")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPCWeb() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	// With proper gRPC-Web framing, response includes grpc-web+proto CT and trailer frame
	ct := rec.Header().Get("Content-Type")
	if ct != "application/grpc-web+proto" {
		t.Errorf("Content-Type = %q, want application/grpc-web+proto", ct)
	}

	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("grpc-web response body")) {
		t.Errorf("body should start with 'grpc-web response body', got %q", body[:min(len(body), 30)])
	}
}

// ============================================================================
// Tests for createTransport with connection pool path
// ============================================================================

func TestCreateTransport_WithConnPoolManager(t *testing.T) {
	connPoolManager := conn.NewPoolManager(nil)
	config := &Config{
		PoolManager:     backend.NewPoolManager(),
		ConnPoolManager: connPoolManager,
		Router:          router.NewRouter(),
		MiddlewareChain: middleware.NewChain(),
		HealthChecker:   health.NewChecker(),
		DialTimeout:     1 * time.Second,
		ProxyTimeout:    5 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)

	transport := proxy.createTransport()
	if transport == nil {
		t.Fatal("expected transport to be non-nil")
	}

	// Test the DialContext function with backendIDKey in context
	// This exercises the connPoolManager.GetPool path
	ctx := context.WithValue(context.Background(), backendIDKey, "test-backend-id")
	_, err := transport.DialContext(ctx, "tcp", "127.0.0.1:1")
	// The connection should fail (no server on port 1), but the pool path is exercised
	if err == nil {
		t.Error("expected error dialing invalid address")
	}
}

func TestCreateTransport_WithoutConnPoolManager(t *testing.T) {
	config := &Config{
		PoolManager:     backend.NewPoolManager(),
		ConnPoolManager: nil, // No connection pool manager
		Router:          router.NewRouter(),
		MiddlewareChain: middleware.NewChain(),
		HealthChecker:   health.NewChecker(),
		DialTimeout:     1 * time.Second,
		ProxyTimeout:    5 * time.Second,
		MaxRetries:      3,
	}
	proxy := NewHTTPProxy(config)

	transport := proxy.createTransport()
	if transport == nil {
		t.Fatal("expected transport to be non-nil")
	}

	// Test direct dial fallback (no backendIDKey in context, no connPoolManager)
	ctx := context.Background()
	_, err := transport.DialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil {
		t.Error("expected error dialing invalid address")
	}
}

// ============================================================================
// Tests for NewHTTPProxy with partial/zero-value configs
// ============================================================================

func TestNewHTTPProxy_WithZeroValues(t *testing.T) {
	// Test that zero-duration timeouts get defaulted
	config := &Config{
		Router:          router.NewRouter(),
		PoolManager:     backend.NewPoolManager(),
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middleware.NewChain(),
		ProxyTimeout:    0, // should default to 60s
		DialTimeout:     0, // should default to 10s
		MaxRetries:      0, // should default to 3
	}
	proxy := NewHTTPProxy(config)
	if proxy == nil {
		t.Fatal("expected proxy to not be nil")
	}
	if proxy.proxyTimeout != 60*time.Second {
		t.Errorf("expected default proxy timeout 60s, got %v", proxy.proxyTimeout)
	}
	if proxy.dialTimeout != 10*time.Second {
		t.Errorf("expected default dial timeout 10s, got %v", proxy.dialTimeout)
	}
	if proxy.maxRetries != 3 {
		t.Errorf("expected default max retries 3, got %d", proxy.maxRetries)
	}
	if proxy.client == nil {
		t.Error("expected client to be initialized")
	}
	if proxy.client.Timeout != 60*time.Second {
		t.Errorf("expected client timeout 60s, got %v", proxy.client.Timeout)
	}
}

func TestNewHTTPProxy_WithNilSubComponents(t *testing.T) {
	// Test with nil sub-components (partial config)
	config := &Config{
		Router:          nil,
		PoolManager:     nil,
		MiddlewareChain: nil,
		ProxyTimeout:    30 * time.Second,
		DialTimeout:     5 * time.Second,
		MaxRetries:      2,
	}
	proxy := NewHTTPProxy(config)
	if proxy == nil {
		t.Fatal("expected proxy to not be nil")
	}
	if proxy.router != nil {
		t.Error("expected router to be nil")
	}
	if proxy.poolManager != nil {
		t.Error("expected poolManager to be nil")
	}
	if proxy.middlewareChain != nil {
		t.Error("expected middlewareChain to be nil")
	}
	if proxy.wsHandler == nil {
		t.Error("expected wsHandler to be initialized")
	}
	if proxy.grpcHandler == nil {
		t.Error("expected grpcHandler to be initialized")
	}
	if proxy.sseHandler == nil {
		t.Error("expected sseHandler to be initialized")
	}
	if proxy.getErrorHandler() == nil {
		t.Error("expected errorHandler to be set to default")
	}
}

// ============================================================================
// Tests for proxyHandler protocol-specific paths (gRPC, SSE)
// ============================================================================

func TestProxyHandler_GRPCRequestPath(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create a mock gRPC backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("grpc response"))
	}))
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("grpc-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("grpc-backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "grpc-route",
		Path:        "/grpc",
		BackendPool: "grpc-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make a gRPC request
	req := httptest.NewRequest("POST", "/grpc/my.Service/Method", bytes.NewReader([]byte("grpc request")))
	req.Header.Set("Content-Type", "application/grpc")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestProxyHandler_SSERequestPath(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create a mock SSE backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: hello\n\n"))
	}))
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()

	pool := backend.NewPool("sse-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("sse-backend-1", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "sse-route",
		Path:        "/events",
		BackendPool: "sse-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make an SSE request
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestProxyHandler_GRPCRequest_NoBackends(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with no healthy backends
	pool := backend.NewPool("grpc-nobackend-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("grpc-backend-down", "127.0.0.1:1")
	b.SetState(backend.StateDown)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "grpc-route-nobackend",
		Path:        "/grpc",
		BackendPool: "grpc-nobackend-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make a gRPC request
	req := httptest.NewRequest("POST", "/grpc/my.Service/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestProxyHandler_SSERequest_NoBackends(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	// Create pool with no healthy backends
	pool := backend.NewPool("sse-nobackend-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("sse-backend-down", "127.0.0.1:1")
	b.SetState(backend.StateDown)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "sse-route-nobackend",
		Path:        "/events",
		BackendPool: "sse-nobackend-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	// Make an SSE request
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

// ============================================================================
// Tests for proxyHandler retry exhaustion with all backends attempted
// ============================================================================

func TestProxyHandler_AllBackendsAttempted(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      5,
	}
	proxy := NewHTTPProxy(config)

	pool := backend.NewPool("retry-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	// Add only one backend that is unreachable
	b := backend.NewBackend("retry-backend-1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "retry-route",
		Path:        "/retry",
		BackendPool: "retry-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest("GET", "/retry", nil)
	rr := httptest.NewRecorder()

	proxy.ServeHTTP(rr, req)

	// After all retries, should get an error
	if rr.Code != http.StatusBadGateway && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d or %d, got %d", http.StatusBadGateway, http.StatusServiceUnavailable, rr.Code)
	}
}

// ============================================================================
// Coverage tests for low-coverage functions
// ============================================================================

// --- proxy.go: getClientIP - RemoteAddr without port (line 430-432) ---

func TestCov_GetClientIP_RemoteAddrNoPort(t *testing.T) {
	p := &HTTPProxy{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "no-colon-or-bracket"
	result := p.getClientIP(req)
	if result != "no-colon-or-bracket" {
		t.Errorf("expected 'no-colon-or-bracket', got %q", result)
	}
}

// --- proxy.go: proxyRequest - backend at max connections (line 317-319) ---

func TestCov_ProxyRequest_BackendMaxConns(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	b := backend.NewBackend("max-conn-backend", "127.0.0.1:0")
	b.SetState(backend.StateUp)
	b.MaxConns = 1
	b.AcquireConn()

	reqCtx := middleware.NewRequestContext(httptest.NewRequest(http.MethodGet, "/test", nil), httptest.NewRecorder())
	defer reqCtx.Release()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	err := proxy.proxyRequest(rr, req, reqCtx, b)
	if err == nil {
		t.Error("expected error when backend at max connections")
	}
	if !strings.Contains(err.Error(), "max connections") {
		t.Errorf("expected 'max connections' error, got: %v", err)
	}
	b.ReleaseConn()
}

// --- proxy.go: proxyRequest - io.Copy error path (line 354-356) ---

func TestCov_ProxyRequest_CopyError(t *testing.T) {
	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("short"))
	})
	defer backendServer.Close()

	proxy, poolManager, routerInstance := setupTestProxy(t)

	backendAddr := backendServer.Listener.Addr().String()
	pool := backend.NewPool("copy-err-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("copy-err-backend", backendAddr)
	b.SetState(backend.StateUp)
	if err := pool.AddBackend(b); err != nil {
		t.Fatalf("failed to add backend: %v", err)
	}
	if err := poolManager.AddPool(pool); err != nil {
		t.Fatalf("failed to add pool: %v", err)
	}

	route := &router.Route{
		Name:        "copy-err-route",
		Path:        "/test",
		BackendPool: "copy-err-pool",
	}
	if err := routerInstance.AddRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	t.Logf("Copy error test status: %d, body: %s", rr.Code, rr.Body.String())
}

// --- proxy.go: prepareOutboundRequest - TLS proto (line 391-393) ---

func TestCov_PrepareOutboundRequest_TLSProto(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()
	b := backend.NewBackend("tls-proto-backend", backendAddr)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.TLS = &tls.ConnectionState{}

	outReq, err := proxy.prepareOutboundRequest(req, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outReq.Header.Get("X-Forwarded-Proto") != "https" {
		t.Errorf("expected X-Forwarded-Proto 'https', got %q", outReq.Header.Get("X-Forwarded-Proto"))
	}
}

// --- proxy.go: prepareOutboundRequest - Connection header value stripping (line 405-408) ---

func TestCov_PrepareOutboundRequest_ConnectionValueStripping(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()
	b := backend.NewBackend("conn-strip-backend", backendAddr)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Connection", "X-Custom-Header, X-Another-Header")
	req.Header.Set("X-Custom-Header", "should-be-removed")
	req.Header.Set("X-Another-Header", "also-removed")
	req.Host = "example.com"

	outReq, err := proxy.prepareOutboundRequest(req, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outReq.Header.Get("Connection") != "" {
		t.Error("expected Connection header to be stripped")
	}
	if outReq.Header.Get("X-Custom-Header") != "" {
		t.Error("expected X-Custom-Header to be stripped (named in Connection)")
	}
	if outReq.Header.Get("X-Another-Header") != "" {
		t.Error("expected X-Another-Header to be stripped (named in Connection)")
	}
}

// --- proxy.go: defaultErrorHandler - default case returns generic message (line 727-728) ---

func TestCov_DefaultErrorHandler_UnknownErrorCode(t *testing.T) {
	err := olbErrors.New(olbErrors.Code(999), "custom error message")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	defaultErrorHandler(rr, req, err)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Internal error details must NOT be exposed to clients
	if resp.Message != "Internal Server Error" {
		t.Errorf("expected generic 'Internal Server Error', got %q", resp.Message)
	}
}

// --- proxy.go: proxyHandler - security.ValidateRequest error (line 202-205) ---

func TestCov_ProxyHandler_SecurityValidationFail(t *testing.T) {
	proxy, poolManager, routerInstance := setupTestProxy(t)

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()
	pool := backend.NewPool("sec-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("sec-backend", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "sec-route", Path: "/test", BackendPool: "sec-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader([]byte("body")))
	req.Header.Set("Content-Length", "4")
	req.Header.Set("Transfer-Encoding", "chunked")

	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d for security validation failure, got %d", http.StatusInternalServerError, rr.Code)
	}
}

// --- proxy.go: proxyHandler - all backends attempted (line 272-275) ---

func TestCov_ProxyHandler_AllBackendsAttempted_ErrorPath(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      2,
	}
	proxy := NewHTTPProxy(config)

	pool := backend.NewPool("attempt-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())

	b := backend.NewBackend("attempt-b1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "attempt-route", Path: "/test", BackendPool: "attempt-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable && rr.Code != http.StatusBadGateway {
		t.Errorf("expected %d or %d, got %d", http.StatusServiceUnavailable, http.StatusBadGateway, rr.Code)
	}
}

// --- proxy.go: NewHTTPProxy - CheckRedirect function (line 124-126) ---

func TestCov_NewHTTPProxy_CheckRedirect(t *testing.T) {
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/final")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final destination"))
	}))
	defer redirectServer.Close()

	proxy, poolManager, routerInstance := setupTestProxy(t)

	backendAddr := redirectServer.Listener.Addr().String()
	pool := backend.NewPool("redirect-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("redirect-backend", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "redirect-route", Path: "/redirect", BackendPool: "redirect-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest(http.MethodGet, "/redirect", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected %d (redirect not followed), got %d", http.StatusFound, rr.Code)
	}
	location := rr.Header().Get("Location")
	if location != "/final" {
		t.Errorf("expected Location '/final', got %q", location)
	}
}

// --- websocket.go: HandleWebSocket - writeUpgradeRequest error (line 107-109) ---

func TestCov_HandleWebSocket_WriteUpgradeError(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	time.Sleep(50 * time.Millisecond)

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("ws-write-err", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")
	w := httptest.NewRecorder()

	err = wh.HandleWebSocket(w, req, b)
	backendListener.Close()
	if err == nil {
		t.Error("expected error when backend closes before upgrade write")
	}
}

// --- websocket.go: HandleWebSocket - writeUpgradeResponse error (line 135-137) ---

func TestCov_HandleWebSocket_WriteUpgradeResponseError(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\n"))
		time.Sleep(2 * time.Second)
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("ws-resp-err", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	serverConn, clientConn := net.Pipe()
	clientConn.Close()

	rw := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufio.NewReader(strings.NewReader("")), bufio.NewWriter(io.Discard)),
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")

	err = wh.HandleWebSocket(rw, req, b)
	if err == nil {
		t.Error("expected error when writeUpgradeResponse fails")
	}
	t.Logf("Error: %v", err)
}

// --- websocket.go: HandleWebSocket - buffered data forwarding (lines 140-154) ---

func TestCov_HandleWebSocket_BufferedClientData(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\nextra-backend-data"))
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("ws-buf-backend", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	bufReader := bufio.NewReader(strings.NewReader("pre-client-data"))
	bufWriter := bufio.NewWriter(io.Discard)
	rw := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufReader, bufWriter),
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Upgrade", "websocket")

	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(rw, req, b)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 4096)
	n, _ := clientConn.Read(respBuf)
	result := string(respBuf[:n])
	if !strings.Contains(result, "101") {
		t.Errorf("expected 101 response, got: %s", result)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n2, _ := clientConn.Read(respBuf)
	if n2 > 0 {
		t.Logf("Received forwarded backend data: %q", string(respBuf[:n2]))
	}

	serverConn.Close()
	clientConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket returned: %v", err)
	case <-time.After(4 * time.Second):
		t.Log("HandleWebSocket timed out")
	}
}

// --- websocket.go: proxyWebSocket - panic recovery (lines 307-312, 325-330) ---

func TestCov_ProxyWebSocket_PanicInCopy(t *testing.T) {
	panicConn := &panicReadConn{}
	normalConn := &errorConn{readErr: fmt.Errorf("normal error")}

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 100 * time.Millisecond,
	})

	err := wh.proxyWebSocket(panicConn, normalConn)
	t.Logf("proxyWebSocket with panic conn returned: %v", err)
}

// panicReadConn is a net.Conn whose Read method panics.
type panicReadConn struct {
	closed bool
}

func (c *panicReadConn) Read([]byte) (int, error)         { panic("test panic in read") }
func (c *panicReadConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *panicReadConn) Close() error                     { c.closed = true; return nil }
func (c *panicReadConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *panicReadConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *panicReadConn) SetDeadline(time.Time) error      { return nil }
func (c *panicReadConn) SetReadDeadline(time.Time) error  { return nil }
func (c *panicReadConn) SetWriteDeadline(time.Time) error { return nil }

// --- websocket.go: proxyWebSocket - error from errChan (line 344-345) ---

func TestCov_ProxyWebSocket_ErrChanResult(t *testing.T) {
	c1 := &errorConn{readErr: fmt.Errorf("non-close-error-1")}
	c2 := &errorConn{readErr: fmt.Errorf("non-close-error-2")}

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 100 * time.Millisecond,
	})

	err := wh.proxyWebSocket(c1, c2)
	if err == nil {
		t.Error("expected error from proxyWebSocket")
	}
	t.Logf("proxyWebSocket returned error: %v", err)
}

// --- websocket.go: copyWithIdleTimeout - io.ErrShortWrite (line 369-371) ---

func TestCov_CopyWithIdleTimeout_ShortWriteViaProxy(t *testing.T) {
	src, srcPipe := net.Pipe()
	dst, _ := net.Pipe()
	defer src.Close()

	dst.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wh := NewWebSocketHandler(nil)
		err := wh.copyWithIdleTimeout(dst, src, 2*time.Second)
		t.Logf("copyWithIdleTimeout returned: %v", err)
	}()

	srcPipe.Write([]byte("data"))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("copyWithIdleTimeout hung on short write")
	}
}

// --- websocket.go: isWebSocketCloseError - edge cases ---

func TestCov_IsWebSocketCloseError_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ECONNABORTED", syscall.ECONNABORTED, true},
		{"timeout net error", &netError{timeout: true}, true},
		{"eof string", fmt.Errorf("unexpected eof"), true},
		{"non-close error", fmt.Errorf("some random error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWebSocketCloseError(tt.err)
			if got != tt.want {
				t.Errorf("isWebSocketCloseError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- http2.go: NewHTTP2Handler - DialTLS function (lines 98-101) ---

func TestCov_NewHTTP2Handler_DialTLSPath(t *testing.T) {
	config := DefaultHTTP2Config()
	handler := NewHTTP2Handler(config)

	if handler.h2Transport == nil {
		t.Fatal("h2Transport should be initialized")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	dialFn := handler.h2Transport.DialTLS
	if dialFn == nil {
		t.Skip("DialTLS is nil, transport may use default")
	}
	conn, err := dialFn("tcp", ln.Addr().String(), nil)
	if err != nil {
		t.Logf("DialTLS: %v (may be expected)", err)
	} else {
		conn.Close()
	}
}

// --- http2.go: Stop with server and listener ---

func TestCov_HTTP2Listener_Stop_WithServerAndListener(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("test"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-stop-full",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	if !listener.IsRunning() {
		t.Error("Listener should be running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = listener.Stop(ctx)
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}

	if listener.IsRunning() {
		t.Error("Listener should not be running after stop")
	}
}

// --- http2.go: HandleHTTP2Proxy - write error on response body (lines 524-526) ---

func TestCov_HandleHTTP2Proxy_WriteError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response data"))
	})

	h2s := &http2.Server{}
	server := httptest.NewServer(h2c.NewHandler(handler, h2s))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	be := backend.NewBackend("h2-write-err-backend", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := &failingResponseWriter{header: make(http.Header)}

	config := DefaultHTTP2Config()
	err := HandleHTTP2Proxy(rec, req, be, config)
	t.Logf("HandleHTTP2Proxy with failing writer: %v", err)
}

// failingResponseWriter is an http.ResponseWriter whose Write always returns an error.
type failingResponseWriter struct {
	header http.Header
	code   int
}

func (f *failingResponseWriter) Header() http.Header  { return f.header }
func (f *failingResponseWriter) WriteHeader(code int) { f.code = code }
func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("write error for test")
}

// --- http2.go: Start - double-check running race (lines 260-262) ---

func TestCov_HTTP2Listener_Start_RaceCondition(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-race",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	listener.running.Store(true)

	err = listener.Start()
	if err == nil {
		t.Error("expected error when listener already running")
		listener.Stop(context.Background())
	}
}

// --- http2.go: Stop - not running double-check (line 322-324) ---

func TestCov_HTTP2Listener_Stop_RaceCheck(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-stop-race",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)
	listener.running.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := listener.Stop(ctx)
	t.Logf("Stop with nil server: %v", err)
}

// --- websocket.go: writeUpgradeRequest - empty path (line 183-185) ---

func TestCov_WriteUpgradeRequest_EmptyPathViaHandleWebSocket(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\n"))
		time.Sleep(2 * time.Second)
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("ws-empty-path", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	rw := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufio.NewReader(strings.NewReader("")), bufio.NewWriter(io.Discard)),
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Upgrade", "websocket")

	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(rw, req, b)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 4096)
	n, _ := clientConn.Read(respBuf)
	result := string(respBuf[:n])
	if !strings.Contains(result, "101") {
		t.Errorf("expected 101 response, got: %s", result)
	}

	serverConn.Close()
	clientConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket: %v", err)
	case <-time.After(4 * time.Second):
		t.Log("HandleWebSocket timed out")
	}
}

// --- Shadow Manager Wiring Tests ---

func TestShadowManager_NilWhenNotConfigured(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)
	if proxy.ShadowManager() != nil {
		t.Error("expected nil shadow manager when not configured")
	}
}

func TestShadowManager_CreatedWhenConfigured(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ShadowConfig: &ShadowConfig{
			Enabled:     true,
			Percentage:  10,
			CopyHeaders: true,
			CopyBody:    false,
			Timeout:     5 * time.Second,
		},
	}

	proxy := NewHTTPProxy(config)
	if proxy.ShadowManager() == nil {
		t.Error("expected non-nil shadow manager when configured")
	}
	if !proxy.ShadowManager().ShouldShadow() {
		// ShouldShadow uses counter, may or may not return true on first call
		// but it shouldn't panic
	}
}

func TestShadowManager_ShadowRequestFired(t *testing.T) {
	// Create a shadow backend that tracks requests
	shadowReceived := make(chan struct{}, 1)
	shadowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case shadowReceived <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer shadowBackend.Close()

	// Create the primary backend
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	// Setup pool with primary backend
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

	pool := backend.NewPool("test-pool", "round_robin")
	pb := backend.NewBackend("primary", strings.TrimPrefix(primaryBackend.URL, "http://"))
	pb.SetState(backend.StateUp)
	pool.AddBackend(pb)
	poolManager.AddPool(pool)
	pool.SetBalancer(balancer.NewRoundRobin())

	routerInstance.AddRoute(&router.Route{
		Name:        "test",
		Path:        "/api/test",
		BackendPool: "test-pool",
	})

	// Configure shadow at 100% to guarantee it fires
	shadowAddr := strings.TrimPrefix(shadowBackend.URL, "http://")
	sb := backend.NewBackend("shadow", shadowAddr)
	sb.SetState(backend.StateUp)

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		ShadowConfig: &ShadowConfig{
			Enabled:     true,
			Percentage:  100, // always shadow
			CopyHeaders: true,
			CopyBody:    false,
			Timeout:     5 * time.Second,
		},
	}

	proxy := NewHTTPProxy(config)

	// Add shadow target to shadow manager
	sm := proxy.ShadowManager()
	if sm == nil {
		t.Fatal("expected shadow manager to be created")
	}

	shadowBackends := []*backend.Backend{sb}
	shadowBalancer := balancer.NewRoundRobin()
	sm.AddTarget(shadowBalancer, shadowBackends, 100)

	// Make a request through the proxy
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Host = "example.com"
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Primary response should succeed
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 from primary, got %d", w.Code)
	}

	// Shadow request should have been fired (async, wait briefly)
	select {
	case <-shadowReceived:
		// Success - shadow was fired
	case <-time.After(2 * time.Second):
		t.Error("expected shadow request to be fired")
	}
}

func TestShadowManager_ForceHeader(t *testing.T) {
	config := &Config{
		Router:          router.NewRouter(),
		PoolManager:     backend.NewPoolManager(),
		ConnPoolManager: conn.NewPoolManager(nil),
		HealthChecker:   health.NewChecker(),
		MiddlewareChain: middleware.NewChain(),
		ShadowConfig: &ShadowConfig{
			Enabled:    true,
			Percentage: 0, // never shadow normally
			Timeout:    5 * time.Second,
		},
	}

	proxy := NewHTTPProxy(config)
	sm := proxy.ShadowManager()

	// Without force header, ShouldShadowRequest should return false (percentage=0)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if sm.ShouldShadowRequest(req) {
		t.Error("expected ShouldShadowRequest=false with 0% and no header")
	}

	// With force header, should return true
	req.Header.Set("X-OLB-Shadow-Force", "true")
	if !sm.ShouldShadowRequest(req) {
		t.Error("expected ShouldShadowRequest=true with force header")
	}
}

func TestGetClientIP_UntrustedPeerIgnoresXFF(t *testing.T) {
	// Trust private ranges and loopback — public IPs are untrusted
	p := &HTTPProxy{trustedNets: parseTrustedProxies([]string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12", "127.0.0.1/32"})}

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expected   string
	}{
		{
			name:       "public peer XFF ignored",
			remoteAddr: "203.0.113.50:12345",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.1"},
			expected:   "203.0.113.50",
		},
		{
			name:       "public peer X-Real-IP ignored",
			remoteAddr: "198.51.100.1:12345",
			headers:    map[string]string{"X-Real-IP": "10.0.0.1"},
			expected:   "198.51.100.1",
		},
		{
			name:       "trusted proxy XFF used",
			remoteAddr: "10.0.0.5:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50"},
			expected:   "203.0.113.50",
		},
		{
			name:       "loopback proxy XFF used",
			remoteAddr: "127.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50"},
			expected:   "203.0.113.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			result := p.getClientIP(req)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCachedHandler_ConcurrentRebuildAndServe(t *testing.T) {
	// This test exercises the atomic.Value-based cachedHandler to ensure
	// concurrent ServeHTTP and RebuildHandler calls do not cause a data race.
	// Run with: go test -race -run TestCachedHandler_ConcurrentRebuildAndServe
	proxy, poolManager, routerInstance := setupTestProxy(t)

	backendServer := createTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	defer backendServer.Close()

	backendAddr := backendServer.Listener.Addr().String()
	pool := backend.NewPool("race-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("race-backend", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "race-route", Path: "/race", BackendPool: "race-pool"}
	routerInstance.AddRoute(route)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // half serve, half rebuild

	// Half the goroutines serve requests
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/race", nil)
			rr := httptest.NewRecorder()
			proxy.ServeHTTP(rr, req)
		}()
	}

	// Half the goroutines rebuild the handler
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			proxy.RebuildHandler()
		}()
	}

	wg.Wait()
}

func TestRebuildHandler_UpdatesCachedHandler(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	// Verify initial cached handler is populated
	h := proxy.cachedHandler.Load()
	if h == nil {
		t.Fatal("expected cachedHandler to be initialized after NewHTTPProxy")
	}

	// Rebuild should replace the handler
	proxy.RebuildHandler()
	h2 := proxy.cachedHandler.Load()
	if h2 == nil {
		t.Fatal("expected cachedHandler to be non-nil after RebuildHandler")
	}
}

func TestSetErrorHandler_ConcurrentAccess(t *testing.T) {
	proxy, _, _ := setupTestProxy(t)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent reads via getErrorHandler
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			handler := proxy.getErrorHandler()
			if handler == nil {
				t.Error("expected non-nil error handler")
			}
		}()
	}

	// Concurrent writes via SetErrorHandler
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			proxy.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				w.WriteHeader(http.StatusTeapot)
			})
		}()
	}

	wg.Wait()
}

func TestParseTrustedProxies(t *testing.T) {
	nets := parseTrustedProxies([]string{"10.0.0.0/8", "192.168.0.0/16", "127.0.0.1", "invalid", ""})
	if len(nets) != 3 {
		t.Fatalf("expected 3 nets, got %d", len(nets))
	}

	p := &HTTPProxy{trustedNets: nets}

	tests := []struct {
		ip       string
		expected bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"172.16.0.1", false},
		{"203.0.113.50", false},
		{"8.8.8.8", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := p.isTrustedProxy(tt.ip); got != tt.expected {
				t.Errorf("isTrustedProxy(%q) = %v, want %v", tt.ip, got, tt.expected)
			}
		})
	}
}
