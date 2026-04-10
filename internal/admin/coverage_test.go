package admin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/health"
)

// basicAuth encodes username:password for HTTP Basic auth.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

// --- Raft Proposer Mocks ---

type mockRaftProposer struct {
	proposalErr error
	proposals   []string
}

func (m *mockRaftProposer) ProposeSetConfig(configJSON []byte) error {
	m.proposals = append(m.proposals, "set-config")
	return m.proposalErr
}

func (m *mockRaftProposer) ProposeUpdateBackend(pool string, backendJSON []byte) error {
	m.proposals = append(m.proposals, "update-backend:"+pool)
	return m.proposalErr
}

func (m *mockRaftProposer) ProposeDeleteBackend(pool, backendID string) error {
	m.proposals = append(m.proposals, "delete-backend:"+pool+":"+backendID)
	return m.proposalErr
}

// serveRequest is a helper to dispatch requests through the server's handler chain.
func serveRequest(s *Server, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)
	return w
}

// --- Raft-mode Handler Tests ---

func TestAddBackend_RaftMode(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("existing", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)
	hc := newMockHealthChecker()

	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   pm,
		HealthChecker: hc,
		RaftProposer:  proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"id":"new-backend","address":"127.0.0.1:9090","weight":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(proposer.proposals) != 1 || proposer.proposals[0] != "update-backend:test-pool" {
		t.Errorf("proposals = %v, want [update-backend:test-pool]", proposer.proposals)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data["status"] != "proposed" {
		t.Errorf("data.status = %v, want proposed", data["status"])
	}
}

func TestAddBackend_RaftMode_Error(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("existing", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	proposer := &mockRaftProposer{proposalErr: fmt.Errorf("raft unavailable")}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		PoolManager:  pm,
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"id":"new-backend","address":"127.0.0.1:9090"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backends/test-pool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := serveRequest(s, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestRemoveBackend_RaftMode(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("to-remove", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		PoolManager:  pm,
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool/to-remove", nil)
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if len(proposer.proposals) != 1 || proposer.proposals[0] != "delete-backend:test-pool:to-remove" {
		t.Errorf("proposals = %v", proposer.proposals)
	}
}

func TestRemoveBackend_RaftMode_Error(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("to-remove", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	proposer := &mockRaftProposer{proposalErr: fmt.Errorf("raft error")}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		PoolManager:  pm,
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/test-pool/to-remove", nil)
	w := serveRequest(s, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestUpdateBackend_RaftMode(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		PoolManager:  pm,
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"weight":50}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(proposer.proposals) != 1 {
		t.Errorf("proposals = %v, want 1 proposal", proposer.proposals)
	}
}

func TestUpdateBackend_RaftMode_Error(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)

	proposer := &mockRaftProposer{proposalErr: fmt.Errorf("raft error")}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		PoolManager:  pm,
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"weight":50}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/backends/test-pool/b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := serveRequest(s, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestUpdateConfig_RaftMode(t *testing.T) {
	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"listeners":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(proposer.proposals) != 1 || proposer.proposals[0] != "set-config" {
		t.Errorf("proposals = %v", proposer.proposals)
	}
}

func TestUpdateConfig_RaftMode_Error(t *testing.T) {
	proposer := &mockRaftProposer{proposalErr: fmt.Errorf("raft error")}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"listeners":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := serveRequest(s, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestUpdateConfig_Standalone_NoReload(t *testing.T) {
	s, err := NewServer(&Config{
		Address:  "127.0.0.1:0",
		OnReload: nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := serveRequest(s, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// --- Events Endpoint Tests ---

func TestGetEvents(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	pool.AddBackend(b1)
	pool.AddBackend(b2)

	pm := newMockPoolManager()
	pm.AddPool(pool)
	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusHealthy
	hc.statuses["b2"] = health.StatusUnhealthy
	hc.results["b1"] = &health.Result{Healthy: true, Timestamp: time.Now()}
	hc.results["b2"] = &health.Result{Healthy: false, Error: fmt.Errorf("connection refused"), Timestamp: time.Now()}

	s, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   pm,
		HealthChecker: hc,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)
	if len(data) == 0 {
		t.Error("expected events, got empty array")
	}

	// Should have health events for b1 and b2 plus system-start
	foundUnhealthy := false
	for _, ev := range data {
		e, _ := ev.(map[string]any)
		if e["type"] == "warning" {
			foundUnhealthy = true
			break
		}
	}
	if !foundUnhealthy {
		t.Error("expected at least one warning event for unhealthy backend")
	}
}

func TestGetEvents_NilComponents(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)
	// Should have at least the system-start event
	if len(data) == 0 {
		t.Error("expected at least system-start event")
	}
}

func TestGetEvents_MethodNotAllowed(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", nil)
	w := serveRequest(s, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- Middleware Status with non-nil provider ---

func TestGetMiddlewareStatus_WithProvider(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		MiddlewareStatus: func() any {
			return []map[string]any{
				{"name": "rate_limit", "enabled": true},
				{"name": "cors", "enabled": false},
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware/status", nil)
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)
	if len(data) != 2 {
		t.Errorf("data length = %d, want 2", len(data))
	}
}

// --- Auth Failure Lockout Tests ---

func TestAuthFailure_LockoutAfterFailures(t *testing.T) {
	hash, _ := HashPassword("testpass")
	auth := &AuthConfig{
		Username: "admin",
		Password: hash,
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

	// Send 5 wrong passwords
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
		req.Header.Set("Authorization", "Basic "+basicAuth("admin", "wrong"))
		req.RemoteAddr = "192.168.1.1:1234"
		w := serveRequest(s, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("attempt %d: status = %d, want 401", i+1, w.Code)
		}
	}

	// 6th request should be locked out (429)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Basic "+basicAuth("admin", "testpass"))
	req.RemoteAddr = "192.168.1.1:1234"
	w := serveRequest(s, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("after lockout: status = %d, want 429", w.Code)
	}
}

func TestAuthFailure_SuccessClearsCount(t *testing.T) {
	hash, _ := HashPassword("testpass")
	auth := &AuthConfig{
		Username: "admin",
		Password: hash,
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

	// 3 failures
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
		req.Header.Set("Authorization", "Basic "+basicAuth("admin", "wrong"))
		req.RemoteAddr = "192.168.2.1:1234"
		serveRequest(s, req)
	}

	// Successful auth should clear count
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Basic "+basicAuth("admin", "testpass"))
	req.RemoteAddr = "192.168.2.1:1234"
	w := serveRequest(s, req)
	if w.Code != http.StatusOK {
		t.Fatalf("success: status = %d, want 200", w.Code)
	}

	// 5 more failures after reset triggers lockout
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
		req.Header.Set("Authorization", "Basic "+basicAuth("admin", "wrong"))
		req.RemoteAddr = "192.168.2.1:1234"
		serveRequest(s, req)
	}

	// 5th failure after reset triggers lockout
	req = httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Basic "+basicAuth("admin", "wrong"))
	req.RemoteAddr = "192.168.2.1:1234"
	w = serveRequest(s, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("after 5 failures post-reset: status = %d, want 429", w.Code)
	}
}

func TestAuthFailure_DifferentIPsIndependent(t *testing.T) {
	hash, _ := HashPassword("testpass")
	auth := &AuthConfig{
		Username: "admin",
		Password: hash,
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

	// Lock out IP 1
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
		req.Header.Set("Authorization", "Basic "+basicAuth("admin", "wrong"))
		req.RemoteAddr = "10.0.0.1:1234"
		serveRequest(s, req)
	}

	// IP 2 should still work
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Basic "+basicAuth("admin", "testpass"))
	req.RemoteAddr = "10.0.0.2:1234"
	w := serveRequest(s, req)

	if w.Code != http.StatusOK {
		t.Errorf("different IP: status = %d, want 200", w.Code)
	}
}

// --- Rate Limiter maxVisitors cap Test ---

func TestRateLimiter_MaxVisitorsCap(t *testing.T) {
	s, err := NewServer(&Config{
		Address:              "127.0.0.1:0",
		RateLimitMaxRequests: 5,
		RateLimitWindow:      "1m",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fill up to 100000 unique IPs
	for i := 0; i < 100000; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1234", i/65536, (i/256)%256, i%256)
		serveRequest(s, req)
	}

	// 100001st unique IP should be rejected (429)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.RemoteAddr = "99.99.99.99:1234"
	w := serveRequest(s, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 (max visitors cap)", w.Code)
	}
}

// --- SetRaftProposer Test ---

func TestSetRaftProposer(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	proposer := &mockRaftProposer{}
	s.SetRaftProposer(proposer)

	s.mu.RLock()
	p := s.raftProposer
	s.mu.RUnlock()

	if p != proposer {
		t.Error("raftProposer not set")
	}
}

// --- readJSONBody strict validation test ---

func TestReadJSONBody_DisallowUnknownFields(t *testing.T) {
	body := `{"id":"b1","address":"127.0.0.1:8080","weight":10,"unknown_field":"bad"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	var dst AddBackendRequest
	err := readJSONBody(req, &dst)
	if err == nil {
		t.Error("expected error for unknown fields, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error should mention unknown field, got: %v", err)
	}
}

func TestReadJSONBody_SizeLimit(t *testing.T) {
	// Create a body larger than 1MB
	largeBody := strings.Repeat(`{"id":"b1","address":"127.0.0.1:8080","weight":10,"padding":"`, 1) + strings.Repeat("x", 2<<20) + `"}`

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")

	var dst AddBackendRequest
	err := readJSONBody(req, &dst)
	if err == nil {
		t.Error("expected error for oversized body, got nil")
	}
}
