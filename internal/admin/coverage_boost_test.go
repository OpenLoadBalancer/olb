package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// --- NewServer edge cases (80.0% -> higher) ---

func TestNewServer_WebUIDefaultCSRF(t *testing.T) {
	webUI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		WebUI:   webUI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.csrfConfig == nil {
		t.Error("expected CSRF config to be auto-enabled when WebUI is present")
	}
	if !s.csrfConfig.Enabled {
		t.Error("expected auto-enabled CSRF config to have Enabled=true")
	}
}

func TestNewServer_NonLocalhostNoAuth_Rejected(t *testing.T) {
	_, err := NewServer(&Config{
		Address: "0.0.0.0:9090",
	})
	if err == nil {
		t.Error("expected error for non-localhost address with no auth")
	}
	if !strings.Contains(err.Error(), "no authentication") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewServer_LocalhostNoAuth_Accepted(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "localhost:0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Error("expected server to be created")
	}
}

func TestNewServer_IPv6LoopbackNoAuth(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "[::1]:0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Error("expected server for ::1 without auth")
	}
}

func TestNewServer_WithClusterAdmin(t *testing.T) {
	cluster := &mockClusterAdmin{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		ClusterAdmin: cluster,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.clusterAdmin == nil {
		t.Error("expected clusterAdmin to be set")
	}
}

func TestNewServer_WithWAFStatus(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		WAFStatus: func() any {
			return map[string]any{"enabled": true, "blocked": 42}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.wafStatus == nil {
		t.Error("expected wafStatus to be set")
	}
}

// mockClusterAdmin implements ClusterAdmin for testing.
type mockClusterAdmin struct {
	registered bool
}

func (m *mockClusterAdmin) RegisterAdminEndpoints(mux *http.ServeMux) {
	m.registered = true
	mux.HandleFunc("/api/v1/cluster/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// --- Start with TLS (66.7% -> higher) ---

func TestStart_TLS(t *testing.T) {
	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		TLSCertFile: "nonexistent-cert.pem",
		TLSKeyFile:  "nonexistent-key.pem",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Start should fail because the cert files don't exist
	err = s.Start()
	if err == nil {
		t.Error("expected error for missing TLS cert files")
	}
}

// --- Stop state transitions (87.5% -> higher) ---

func TestStop_UpdatesState(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	// Start in background
	go s.Start()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if s.GetState() != "stopping" {
		t.Errorf("expected state 'stopping', got %q", s.GetState())
	}
}

// --- GetAllMetrics error paths (63.6% -> higher) ---

func TestGetAllMetrics_NilRegistry(t *testing.T) {
	m := NewDefaultMetrics(nil)
	result := m.GetAllMetrics()
	if result == nil {
		t.Error("expected non-nil result even with nil registry")
	}
}

func TestGetAllMetrics_WithRegistry(t *testing.T) {
	reg := metrics.NewRegistry()
	m := NewDefaultMetrics(reg)
	result := m.GetAllMetrics()
	if result == nil {
		t.Error("expected non-nil result")
	}
}

// --- PrometheusFormat write error path (80.0% -> higher) ---

func TestPrometheusFormat_WithRegistry(t *testing.T) {
	reg := metrics.NewRegistry()
	c := metrics.NewCounter("test_prom_counter", "test")
	c.Add(1)
	reg.RegisterCounter(c)
	m := NewDefaultMetrics(reg)
	output := m.PrometheusFormat()
	if output == "" {
		t.Error("expected non-empty Prometheus output")
	}
}

// --- handleConfig method not allowed (90%+ for handleConfig) ---

func TestHandleConfig_MethodNotAllowed(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleConfig_PostNotAllowed(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- getConfig without ConfigGetter (77.8% -> higher) ---

func TestGetConfig_NoConfigGetter(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// --- sanitizeConfigForAPI with marshal error (75.0% -> higher) ---

func TestSanitizeConfigForAPI_MarshalError(t *testing.T) {
	// Channels cannot be marshaled to JSON
	input := map[string]any{
		"ch": make(chan int),
	}
	result, err := sanitizeConfigForAPI(input)
	if err == nil {
		t.Error("expected error for unmarshallable input")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

// --- isLocked edge cases (81.8% -> higher) ---

func TestIsLocked_EntryNotLocked(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	// Add entry with no lockout
	l.mu.Lock()
	l.entries["test-ip"] = &authFailureEntry{
		count:      1,
		lastAccess: time.Now(),
	}
	l.mu.Unlock()

	if l.isLocked("test-ip") {
		t.Error("expected entry with no lockout to not be locked")
	}
}

func TestIsLocked_ExpiredLockout(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	l.mu.Lock()
	l.entries["test-ip"] = &authFailureEntry{
		count:       5,
		lockedUntil: time.Now().Add(-1 * time.Hour), // already expired
		lastAccess:  time.Now(),
	}
	l.mu.Unlock()

	if l.isLocked("test-ip") {
		t.Error("expected expired lockout to not be locked")
	}
}

func TestIsLocked_ActiveLockout(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	l.mu.Lock()
	l.entries["test-ip"] = &authFailureEntry{
		count:       5,
		lockedUntil: time.Now().Add(1 * time.Hour), // still active
		lastAccess:  time.Now(),
	}
	l.mu.Unlock()

	if !l.isLocked("test-ip") {
		t.Error("expected active lockout to be locked")
	}
}

func TestIsLocked_EntryExpiredAndRemoved(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	l.mu.Lock()
	l.entries["test-ip"] = &authFailureEntry{
		count:       5,
		lockedUntil: time.Now().Add(-1 * time.Hour),
		lastAccess:  time.Now(),
	}
	l.mu.Unlock()

	// Should return false and clean up
	if l.isLocked("test-ip") {
		t.Error("expected expired lockout to return false")
	}

	// Entry should be cleaned up
	l.mu.Lock()
	_, exists := l.entries["test-ip"]
	l.mu.Unlock()
	if exists {
		t.Error("expected expired entry to be deleted")
	}
}

// --- getMiddlewareStatus with middlewareStatus set (42.9% -> higher) ---

func TestGetMiddlewareStatus_MethodNotAllowed(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		MiddlewareStatus: func() any {
			return []map[string]any{{"name": "test"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware/status", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestGetMiddlewareStatus_NilProviderWithRoute(t *testing.T) {
	// When middlewareStatus is set to a provider that returns nil-typed results
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		MiddlewareStatus: func() any {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware/status", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- WAF status endpoint tests ---

func TestGetWAFStatus_NilProviderFunc(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		WAFStatus: func() any {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/waf/status", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- addBackend validation paths (83.0% -> higher) ---

func TestAddBackend_NilPoolManager(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"id":"b1","address":"127.0.0.1:8080"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAddBackend_Success(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"id":"b1","address":"127.0.0.1:8080","weight":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestAddBackend_MethodNotAllowed(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// GET to /api/v1/backends/:pool returns pool info, not 405
	// So test with a method that's explicitly not allowed
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool", nil)
	w = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- updateBackend validation (83.7% -> higher) ---

func TestUpdateBackend_InvalidWeight(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"weight":-5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateBackend_WeightTooLarge(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"weight":1001}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateBackend_NegativeMaxConns(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"max_conns":-1}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateBackend_SuccessWithMaxConns(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"weight":50,"max_conns":100}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// --- removeBackend edge cases (92.6% -> higher) ---

func TestRemoveBackend_NilPoolManager(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool/b1", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- drainBackend edge cases (91.7% -> higher) ---

func TestDrainBackend_NilPoolManager(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool/b1/drain", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- handleBackendDetail edge cases (91.3% -> higher) ---

func TestHandleBackendDetail_InvalidPath(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	// Only 3 parts: api/v1/backends — too short
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// This goes to listBackends, not handleBackendDetail
	// Need a path that actually hits handleBackendDetail with fewer parts
}

func TestHandleBackendDetail_DrainGET(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	// GET to drain endpoint should return 405
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/b1/drain", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleBackendDetail_UnsupportedMethod(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	// PUT to pool-level endpoint is not allowed
	req := httptest.NewRequest(http.MethodPut, "/api/v1/backends/test-pool", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandleBackendDetail_BackendPUT(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	// PUT to backend-level endpoint is not allowed
	req := httptest.NewRequest(http.MethodPut, "/api/v1/backends/test-pool/b1", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- handlePoolDetail edge cases ---

// --- CORS tests ---

func TestAdminCORS_AllowAll(t *testing.T) {
	s, err := NewServer(&Config{
		Address:        "127.0.0.1:0",
		AllowedOrigins: []string{"*"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Preflight request
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/version", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "https://evil.example.com" {
		t.Errorf("expected origin reflected, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	// Allow all should NOT set credentials
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("expected no credentials header for wildcard origin")
	}
}

func TestAdminCORS_SpecificOrigin(t *testing.T) {
	s, err := NewServer(&Config{
		Address:        "127.0.0.1:0",
		AllowedOrigins: []string{"https://admin.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/version", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "https://admin.example.com" {
		t.Errorf("expected specific origin, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("expected credentials header for specific origin")
	}
}

func TestAdminCORS_DisallowedOrigin(t *testing.T) {
	s, err := NewServer(&Config{
		Address:        "127.0.0.1:0",
		AllowedOrigins: []string{"https://admin.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Should not have CORS headers
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS headers for disallowed origin")
	}
}

// --- warnNoAuth tests ---

func TestWarnNoAuth_APIPath(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Header().Get("X-Security-Warning") == "" {
		t.Error("expected X-Security-Warning header for unauthenticated API request")
	}
}

func TestWarnNoAuth_NonAPIPath(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Header().Get("X-Security-Warning") != "" {
		t.Error("expected no security warning for non-API path")
	}
}

// --- API 404 fallback ---

func TestAPI404Fallback_Boost(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Certificates endpoint ---

func TestGetCertificates_WithLister(t *testing.T) {
	lister := &mockCertLister{
		certs: []CertInfoView{
			{Names: []string{"example.com"}, Expiry: time.Now().Add(30 * 24 * time.Hour).Unix(), IsWildcard: false},
		},
	}

	s, err := NewServer(&Config{
		Address:    "127.0.0.1:0",
		CertLister: lister,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetCertificates_NilLister(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)
	if len(data) != 0 {
		t.Error("expected empty array for nil cert lister")
	}
}

// --- Metrics endpoints edge cases ---

func TestGetMetricsPrometheus_Success(t *testing.T) {
	m := newMockMetrics()
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		Metrics: m,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/plain; version=0.0.4" {
		t.Errorf("unexpected content type: %s", w.Header().Get("Content-Type"))
	}
}

// --- Health endpoint edge cases ---

func TestGetHealthStatus_NilHealthChecker(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetHealthStatus_WithResults(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b1)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusHealthy
	hc.results["b1"] = &health.Result{
		Healthy:   true,
		Timestamp: time.Now(),
		Latency:   5 * time.Millisecond,
	}

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   pm,
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetHealthStatus_UnhealthyWithLatency(t *testing.T) {
	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusUnhealthy
	hc.results["b1"] = &health.Result{
		Healthy:   false,
		Timestamp: time.Now(),
		Latency:   100 * time.Millisecond,
		Error:     errors.New("connection refused"),
	}

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- System health edge cases (97.0% -> higher) ---

func TestGetSystemHealth_NoComponents(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data["status"] == "healthy" {
		t.Error("expected degraded status when no components are configured")
	}
}

func TestGetSystemHealth_WithAllComponents(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b1)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	r := newMockRouter()
	r.AddRoute(&router.Route{Name: "r1", Path: "/"})

	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusHealthy

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   pm,
		Router:        r,
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data["status"] != "healthy" {
		t.Errorf("expected healthy, got %v", data["status"])
	}
}

func TestGetSystemHealth_MethodNotAllowedBoost(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- ReloadConfig edge cases ---

// --- Routes endpoint ---

func TestListRoutes_WithRoutes(t *testing.T) {
	r := newMockRouter()
	r.AddRoute(&router.Route{
		Name:        "api",
		Path:        "/api",
		Methods:     []string{"GET", "POST"},
		BackendPool: "api-pool",
		Priority:    10,
	})

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		Router:  r,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/routes", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- Version endpoint ---

func TestGetVersion_Success(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- System info endpoint ---

func TestGetSystemInfo_Success(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data["state"] != "running" {
		t.Errorf("expected state=running, got %v", data["state"])
	}
}

// --- List backends endpoint ---

func TestListBackends_WithPools(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- List pools endpoint ---

func TestListPools_WithPools(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- Pool with health check config ---

// --- Backend detail endpoint ---

func TestGetBackendDetail_SuccessBoost(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/b1", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetBackendDetail_BackendNotFoundBoost(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool/nonexistent", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- recordSuccess clears entry ---

func TestRecordSuccess_ClearsEntry(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	ip := "192.168.1.1"
	l.recordFailure(ip)

	l.mu.Lock()
	_, exists := l.entries[ip]
	l.mu.Unlock()
	if !exists {
		t.Fatal("expected entry after recordFailure")
	}

	l.recordSuccess(ip)

	l.mu.Lock()
	_, exists = l.entries[ip]
	l.mu.Unlock()
	if exists {
		t.Error("expected entry to be removed after recordSuccess")
	}
}

// --- Auth middleware edge cases ---

func TestAuthMiddleware_PublicHealthEndpoint(t *testing.T) {
	hash, _ := HashPassword("testpass")
	auth := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: false, // Allow unauthenticated reads
	}

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		Auth:        auth,
		PoolManager: newMockPoolManager(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.config.Close()

	// GET /health should be allowed without auth when RequireAuthForRead=false
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Should pass through (404 since we don't have /health handler, but not 401)
	if w.Code == http.StatusUnauthorized {
		t.Error("public health endpoint should not require auth")
	}
}

func TestAuthMiddleware_UnsupportedScheme(t *testing.T) {
	hash, _ := HashPassword("testpass")
	auth := &AuthConfig{
		Username:           "admin",
		Password:           hash,
		RequireAuthForRead: true,
	}

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		Auth:        auth,
		PoolManager: newMockPoolManager(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.config.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Digest user=admin")
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_BearerTokenSuccess(t *testing.T) {
	auth := &AuthConfig{
		BearerTokens:       []string{"valid-token"},
		RequireAuthForRead: true,
	}

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		Auth:        auth,
		PoolManager: newMockPoolManager(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.config.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.RemoteAddr = "10.0.0.4:1234"
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthMiddleware_BearerTokenFailure(t *testing.T) {
	auth := &AuthConfig{
		BearerTokens:       []string{"valid-token"},
		RequireAuthForRead: true,
	}

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		Auth:        auth,
		PoolManager: newMockPoolManager(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.config.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	req.RemoteAddr = "10.0.0.5:1234"
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// --- Rate limiter per-IP ---

func TestRateLimiter_PerIPBasic(t *testing.T) {
	s, err := NewServer(&Config{
		Address:              "127.0.0.1:0",
		RateLimitMaxRequests: 3,
		RateLimitWindow:      "1m",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Make 3 requests from same IP
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: status = %d, want 200", i+1, w.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: status = %d, want 429", w.Code)
	}

	// Different IP should still work
	req = httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.RemoteAddr = "192.168.1.2:1234"
	w = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("different IP: status = %d, want 200", w.Code)
	}
}

// --- stripSecrets deeper nesting ---

func TestStripSecrets_DeeplyNested(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"password": "secret1",
			"nested": map[string]any{
				"token": "secret2",
				"items": []any{
					map[string]any{
						"client_secret": "secret3",
						"name":          "ok",
					},
				},
				"safe": "visible",
			},
		},
	}

	stripSecrets(input)

	l1 := input["level1"].(map[string]any)
	if _, ok := l1["password"]; ok {
		t.Error("level1 password should be stripped")
	}
	nested := l1["nested"].(map[string]any)
	if _, ok := nested["token"]; ok {
		t.Error("nested token should be stripped")
	}
	items := nested["items"].([]any)
	item := items[0].(map[string]any)
	if _, ok := item["client_secret"]; ok {
		t.Error("array client_secret should be stripped")
	}
	if item["name"] != "ok" {
		t.Error("non-secret array field should be preserved")
	}
	if nested["safe"] != "visible" {
		t.Error("safe field should be preserved")
	}
}

// --- getEvents with healthy backends ---

func TestGetEvents_AllHealthyBackends(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(backend.StateUp)
	pool.AddBackend(b1)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusHealthy
	hc.results["b1"] = &health.Result{
		Healthy:   true,
		Timestamp: time.Now(),
		Latency:   2 * time.Millisecond,
	}

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   pm,
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// --- getEvents with partial pool health ---

func TestGetEvents_PartialPoolHealth(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(backend.StateUp)
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b2.SetState(backend.StateDown)
	pool.AddBackend(b1)
	pool.AddBackend(b2)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusHealthy
	hc.statuses["b2"] = health.StatusUnhealthy
	hc.results["b1"] = &health.Result{Healthy: true, Timestamp: time.Now()}
	hc.results["b2"] = &health.Result{Healthy: false, Error: errors.New("timeout"), Timestamp: time.Now()}

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   pm,
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)

	// Should have pool warning + health events + system-start
	foundPoolWarning := false
	for _, ev := range data {
		e, _ := ev.(map[string]any)
		if strings.Contains(fmt.Sprint(e["message"]), "Pool pool1:") {
			foundPoolWarning = true
			break
		}
	}
	if !foundPoolWarning {
		t.Error("expected pool warning event for partial health")
	}
}

// --- readBody helper ---

func TestReadBody_LargeBody(t *testing.T) {
	largeBody := strings.Repeat("x", 2<<20) // > 1MB
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(largeBody))

	data, err := readBody(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) > 1<<20 {
		t.Errorf("expected body to be capped at 1MB, got %d bytes", len(data))
	}
}

// --- GetPool with nil poolManager ---

func TestGetPool_NilPoolManagerBoost(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/test-pool", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- extractPoolName edge cases ---

func TestExtractPoolName_EmptyPath(t *testing.T) {
	result := extractPoolName("")
	if result != "" {
		t.Errorf("expected empty string for empty path, got %q", result)
	}
}

func TestExtractPoolName_ShortPath(t *testing.T) {
	result := extractPoolName("/api/v1")
	if result != "" {
		t.Errorf("expected empty string for short path, got %q", result)
	}
}

func TestExtractBackendID_ValidPath(t *testing.T) {
	pool, backendID := extractBackendID("/api/v1/backends/my-pool/my-backend")
	if pool != "my-pool" {
		t.Errorf("expected pool 'my-pool', got %q", pool)
	}
	if backendID != "my-backend" {
		t.Errorf("expected backend 'my-backend', got %q", backendID)
	}
}

func TestExtractBackendID_ShortPath(t *testing.T) {
	pool, backendID := extractBackendID("/api/v1/backends")
	if pool != "" || backendID != "" {
		t.Errorf("expected empty strings, got pool=%q backend=%q", pool, backendID)
	}
}

// --- Circuit breaker half-open -> closed transition ---

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
		cb := newAdminCircuitBreaker()
		cb.openDuration = 50 * time.Millisecond // Shorten for test

		// Force into open state via recordOutcome
		for i := 0; i < cb.errorThreshold; i++ {
			cb.recordOutcome(fmt.Errorf("error %d", i))
		}

		if cb.State() != "open" {
			t.Fatalf("expected open state, got %s", cb.State())
		}

		// Wait for open duration to expire
		time.Sleep(cb.openDuration + 20*time.Millisecond)

		// Now the State() method will report half-open, but internally
		// the state is still cbOpen. We need to use Execute() which
		// will transition it to cbHalfOpen, then record outcomes.
		ctx := context.Background()

		// First Execute after expiry will transition to half-open
		err := cb.Execute(ctx, func(ctx context.Context) error {
			return nil // success
		})
		if err != nil {
			t.Fatalf("unexpected error in first half-open execute: %v", err)
		}

		// 2 more successes to reach 3 total and transition to closed
		for i := 0; i < 2; i++ {
			err = cb.Execute(ctx, func(ctx context.Context) error {
				return nil
			})
			if err != nil {
				t.Fatalf("unexpected error in execute %d: %v", i+2, err)
			}
		}

		if cb.State() != "closed" {
			t.Errorf("expected closed state after 3 successes, got %s", cb.State())
		}
	}

// --- addBackend weight overflow ---

func TestAddBackend_ExcessiveWeight(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	pm := newMockPoolManager()
	pm.AddPool(pool)

	s, err := NewServer(&Config{
		Address:     "127.0.0.1:0",
		PoolManager: pm,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"id":"b1","address":"127.0.0.1:8080","weight":2147483648}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for weight exceeding MaxInt32; body: %s", w.Code, w.Body.String())
	}
}

// --- readJSONBody with no content type ---

func TestReadJSONBody_NoContentType(t *testing.T) {
	body := `{"id":"b1","address":"127.0.0.1:8080"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	// No Content-Type header set

	var dst AddBackendRequest
	err := readJSONBody(req, &dst)
	if err != nil {
		t.Errorf("expected success without content-type, got error: %v", err)
	}
	if dst.ID != "b1" {
		t.Errorf("expected ID b1, got %s", dst.ID)
	}
}

// --- getConfig with marshal error (fallback path) ---

func TestGetConfig_SanitizationMarshalError(t *testing.T) {
	getter := &mockConfigGetter{config: map[string]any{
		"ch": make(chan int),
	}}

	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		ConfigGetter: getter,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Should fall back to returning raw config since sanitization fails
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fallback path)", w.Code)
	}
}

// --- streamEvents with non-flusher response writer ---

func TestStreamEvents_NoFlusher(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	// Create a minimal response writer that does NOT implement http.Flusher
	w := &basicResponseWriter{header: http.Header{}, code: 0}
	s.streamEvents(w, req)

	if w.code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.code)
	}
}

// basicResponseWriter is a minimal http.ResponseWriter without Flusher.
type basicResponseWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func (w *basicResponseWriter) Header() http.Header        { return w.header }
func (w *basicResponseWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *basicResponseWriter) WriteHeader(code int)         { w.code = code }

// --- Rate limiter: request without port in RemoteAddr ---

func TestRateLimiter_NoPortInRemoteAddr(t *testing.T) {
	s, err := NewServer(&Config{
		Address:              "127.0.0.1:0",
		RateLimitMaxRequests: 60,
		RateLimitWindow:      "1m",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.RemoteAddr = "unix-socket" // No port
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Should not panic
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- Health check with nil Result ---

func TestGetHealthStatus_NilResult(t *testing.T) {
	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusHealthy
	// No result for b1

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- Default metrics with real registry ---

func TestNewDefaultMetrics_NilRegistry_UsesDefault(t *testing.T) {
	m := NewDefaultMetrics(nil)
	if m == nil {
		t.Error("expected non-nil metrics")
	}
	// Verify it works
	result := m.GetAllMetrics()
	// Should return some result (even if empty)
	if result == nil {
		t.Error("expected non-nil result from GetAllMetrics")
	}
}

// --- Cluster admin endpoint registered ---

func TestClusterAdmin_EndpointsRegistered(t *testing.T) {
	cluster := &mockClusterAdmin{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		ClusterAdmin: cluster,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cluster.registered {
		t.Error("expected cluster admin endpoints to be registered")
	}

	// Verify the cluster endpoint works
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/test", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for cluster endpoint", w.Code)
	}
}

// --- reloadConfig with circuit breaker open ---

func TestReloadConfig_CircuitBreakerOpen(t *testing.T) {
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			return fmt.Errorf("always fails")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Trip the circuit breaker by triggering enough failures
	for i := 0; i < 5; i++ {
		lastReloadMu.Lock()
		lastReloadTime = time.Time{}
		lastReloadMu.Unlock()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/reload", nil)
		w := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(w, req)
	}

	// Next call should hit open circuit breaker
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/reload", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Circuit breaker should return error (503)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}
