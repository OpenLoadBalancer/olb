package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// setupRBACTestServer creates a test server with bearer tokens that have
// both admin and readonly roles configured. Returns an httptest.Server.
func setupRBACTestServer(t *testing.T, authConfig *AuthConfig) (*httptest.Server, *mockPoolManager) {
	t.Helper()

	poolManager := newMockPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(backend.StateUp)
	pool.AddBackend(b1)
	poolManager.AddPool(pool)

	config := &Config{
		Address:       "127.0.0.1:0",
		Auth:          authConfig,
		PoolManager:   poolManager,
		Router:        newMockRouter(),
		HealthChecker: newMockHealthChecker(),
		Metrics:       newMockMetrics(),
		OnReload:      func() error { return nil },
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(server.server.Handler)
	return ts, poolManager
}

// TestRBAC_ReadOnlyCanReadGETEndpoints verifies that a read-only user can
// access all GET endpoints.
func TestRBAC_ReadOnlyCanReadGETEndpoints(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens: []string{"admin-token", "readonly-token"},
		BearerRoles: map[string]string{
			"admin-token":    RoleAdmin,
			"readonly-token": RoleReadOnly,
		},
		RequireAuthForRead: true,
	}

	ts, _ := setupRBACTestServer(t, authConfig)
	defer ts.Close()

	client := ts.Client()

	getEndpoints := []struct {
		name string
		path string
	}{
		{"system info", "/api/v1/system/info"},
		{"system health", "/api/v1/system/health"},
		{"version", "/api/v1/version"},
		{"pools", "/api/v1/pools"},
		{"backends", "/api/v1/backends"},
		{"routes", "/api/v1/routes"},
		{"health", "/api/v1/health"},
		{"metrics", "/api/v1/metrics"},
		{"prometheus", "/metrics"},
		{"config", "/api/v1/config"},
		{"certificates", "/api/v1/certificates"},
		{"events", "/api/v1/events"},
		{"pool detail", "/api/v1/pools/test-pool"},
		{"backend detail", "/api/v1/backends/test-pool/b1"},
	}

	for _, ep := range getEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", ts.URL+ep.path, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer readonly-token")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("read-only user got 401 on GET %s (auth should succeed)", ep.path)
			}
			if resp.StatusCode == http.StatusForbidden {
				t.Errorf("read-only user got 403 on GET %s (should be allowed)", ep.path)
			}
		})
	}
}

// TestRBAC_ReadOnlyBlockedFromWriteEndpoints verifies that a read-only user
// gets 403 Forbidden on all state-changing endpoints.
func TestRBAC_ReadOnlyBlockedFromWriteEndpoints(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens: []string{"admin-token", "readonly-token"},
		BearerRoles: map[string]string{
			"admin-token":    RoleAdmin,
			"readonly-token": RoleReadOnly,
		},
		RequireAuthForRead: true,
	}

	ts, _ := setupRBACTestServer(t, authConfig)
	defer ts.Close()

	// Reset reload cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	client := ts.Client()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"reload config", "POST", "/api/v1/system/reload", ""},
		{"add backend", "POST", "/api/v1/backends/test-pool", `{"id":"b2","address":"127.0.0.1:8081"}`},
		{"update backend", "PATCH", "/api/v1/backends/test-pool/b1", `{"weight":5}`},
		{"delete backend", "DELETE", "/api/v1/backends/test-pool/b1", ""},
		{"drain backend", "POST", "/api/v1/backends/test-pool/b1/drain", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, body)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.Header.Set("Authorization", "Bearer readonly-token")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("read-only user expected 403 on %s %s, got %d", tt.method, tt.path, resp.StatusCode)
				return
			}

			// Verify error response format
			var result Response
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if result.Success {
				t.Error("expected failure response")
			}
			if result.Error == nil || result.Error.Code != "FORBIDDEN" {
				t.Errorf("expected FORBIDDEN error, got %v", result.Error)
			}
		})
	}
}

// TestRBAC_AdminCanAccessWriteEndpoints verifies that an admin user can
// access state-changing endpoints.
func TestRBAC_AdminCanAccessWriteEndpoints(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens: []string{"admin-token", "readonly-token"},
		BearerRoles: map[string]string{
			"admin-token":    RoleAdmin,
			"readonly-token": RoleReadOnly,
		},
		RequireAuthForRead: true,
	}

	ts, _ := setupRBACTestServer(t, authConfig)
	defer ts.Close()

	// Reset reload cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	client := ts.Client()

	// Test reload (POST) — admin should succeed
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/system/reload", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("admin user got 403 on POST /api/v1/system/reload (should be allowed)")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("admin user got 401 on POST /api/v1/system/reload (auth failed)")
	}

	// Test add backend (POST) — admin should succeed
	body := `{"id":"b2","address":"127.0.0.1:9091"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/backends/test-pool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer admin-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("admin user got 403 on POST /api/v1/backends/test-pool (should be allowed)")
	}

	// Test update backend (PATCH) — admin should succeed
	body = `{"weight":10}`
	req, _ = http.NewRequest("PATCH", ts.URL+"/api/v1/backends/test-pool/b2", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer admin-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("admin user got 403 on PATCH backend (should be allowed)")
	}

	// Test delete backend (DELETE) — admin should succeed
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v1/backends/test-pool/b2", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("admin user got 403 on DELETE backend (should be allowed)")
	}
}

// TestRBAC_BearerTokenDefaultAdmin verifies that bearer tokens without an
// explicit role in BearerRoles get the admin role (backward compatible).
func TestRBAC_BearerTokenDefaultAdmin(t *testing.T) {
	authConfig := &AuthConfig{
		BearerTokens:       []string{"token-without-role"},
		BearerRoles:        nil, // no roles configured
		RequireAuthForRead: true,
	}

	ts, _ := setupRBACTestServer(t, authConfig)
	defer ts.Close()

	// Reset reload cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	client := ts.Client()

	// POST (state-changing) should succeed — token gets admin role by default
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/system/reload", nil)
	req.Header.Set("Authorization", "Bearer token-without-role")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("token without explicit role got 403 (should default to admin)")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("token without explicit role got 401 (auth failed)")
	}
}

// TestRBAC_BasicAuthAlwaysAdmin verifies that basic auth users always get
// the admin role regardless of any BearerRoles configuration.
func TestRBAC_BasicAuthAlwaysAdmin(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	authConfig := &AuthConfig{
		Username:     "admin",
		Password:     hash,
		BearerTokens: []string{"readonly-token"},
		BearerRoles: map[string]string{
			"readonly-token": RoleReadOnly,
		},
		RequireAuthForRead: true,
	}

	ts, _ := setupRBACTestServer(t, authConfig)
	defer ts.Close()

	// Reset reload cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	client := ts.Client()

	// Basic auth POST (state-changing) should succeed — basic auth always admin
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/system/reload", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:password")))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("basic auth user got 403 (should always be admin)")
	}
}

// TestRBAC_NoAuthDefaultsAdmin verifies that when no auth is configured,
// all requests pass through (no RBAC restriction, backward compatible).
func TestRBAC_NoAuthDefaultsAdmin(t *testing.T) {
	// Reset reload cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	poolManager := newMockPoolManager()

	server, err := NewServer(&Config{
		Address:       "127.0.0.1:0",
		PoolManager:   poolManager,
		Router:        newMockRouter(),
		HealthChecker: newMockHealthChecker(),
		Metrics:       newMockMetrics(),
		OnReload:      func() error { return nil },
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	client := ts.Client()

	// POST without auth should succeed — no auth means no RBAC check
	resp, err := client.Post(ts.URL+"/api/v1/system/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("unauthenticated request got 403 when no auth configured (should pass through)")
	}
}

// TestRequireAdminRole_Unit tests the requireAdminRole middleware directly.
func TestRequireAdminRole_Unit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := requireAdminRole(handler)

	tests := []struct {
		name       string
		method     string
		role       string // empty means no role in context
		wantStatus int
	}{
		{"GET with no role", "GET", "", http.StatusOK},
		{"GET with readonly role", "GET", RoleReadOnly, http.StatusOK},
		{"GET with admin role", "GET", RoleAdmin, http.StatusOK},
		{"POST with admin role", "POST", RoleAdmin, http.StatusOK},
		{"POST with readonly role", "POST", RoleReadOnly, http.StatusForbidden},
		{"PUT with readonly role", "PUT", RoleReadOnly, http.StatusForbidden},
		{"PATCH with readonly role", "PATCH", RoleReadOnly, http.StatusForbidden},
		{"DELETE with readonly role", "DELETE", RoleReadOnly, http.StatusForbidden},
		{"POST with no role (backward compat)", "POST", "", http.StatusOK},
		{"HEAD with readonly role", "HEAD", RoleReadOnly, http.StatusOK},
		{"OPTIONS with readonly role", "OPTIONS", RoleReadOnly, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.role != "" {
				ctx := context.WithValue(req.Context(), roleContextKey{}, tt.role)
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

// TestBearerRole tests the bearerRole helper method.
func TestBearerRole(t *testing.T) {
	tests := []struct {
		name        string
		bearerRoles map[string]string
		token       string
		wantRole    string
	}{
		{
			name:        "nil BearerRoles defaults to admin",
			bearerRoles: nil,
			token:       "any-token",
			wantRole:    RoleAdmin,
		},
		{
			name:        "empty BearerRoles defaults to admin",
			bearerRoles: map[string]string{},
			token:       "any-token",
			wantRole:    RoleAdmin,
		},
		{
			name:        "explicit readonly role",
			bearerRoles: map[string]string{"my-token": RoleReadOnly},
			token:       "my-token",
			wantRole:    RoleReadOnly,
		},
		{
			name:        "explicit admin role",
			bearerRoles: map[string]string{"my-token": RoleAdmin},
			token:       "my-token",
			wantRole:    RoleAdmin,
		},
		{
			name:        "token not in map defaults to admin",
			bearerRoles: map[string]string{"other-token": RoleReadOnly},
			token:       "my-token",
			wantRole:    RoleAdmin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AuthConfig{
				BearerRoles: tt.bearerRoles,
			}
			got := cfg.bearerRole(tt.token)
			if got != tt.wantRole {
				t.Errorf("bearerRole(%q) = %q, want %q", tt.token, got, tt.wantRole)
			}
		})
	}
}

// TestRoleFromContext tests the RoleFromContext helper.
func TestRoleFromContext(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		wantRole string
	}{
		{"empty context defaults to admin", "", RoleAdmin},
		{"admin role", RoleAdmin, RoleAdmin},
		{"readonly role", RoleReadOnly, RoleReadOnly},
		{"unknown role passes through", "custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.role != "" {
				ctx = context.WithValue(ctx, roleContextKey{}, tt.role)
			}
			got := RoleFromContext(ctx)
			if got != tt.wantRole {
				t.Errorf("RoleFromContext() = %q, want %q", got, tt.wantRole)
			}
		})
	}
}

// TestIsWriteMethod tests the isWriteMethod helper.
func TestIsWriteMethod(t *testing.T) {
	writeMethods := []string{"POST", "PUT", "PATCH", "DELETE"}
	readMethods := []string{"GET", "HEAD", "OPTIONS", "CONNECT", "TRACE"}

	for _, m := range writeMethods {
		if !isWriteMethod(m) {
			t.Errorf("isWriteMethod(%q) = false, want true", m)
		}
	}
	for _, m := range readMethods {
		if isWriteMethod(m) {
			t.Errorf("isWriteMethod(%q) = true, want false", m)
		}
	}
}
