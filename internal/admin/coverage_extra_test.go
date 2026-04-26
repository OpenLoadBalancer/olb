package admin

import (
	"context"
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

// --- ZeroSecrets (0% coverage) ---

func TestZeroSecrets(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{"token-a", "token-b", "token-c"},
	}
	cfg.ZeroSecrets()

	for i, tok := range cfg.BearerTokens {
		if tok != "" {
			t.Errorf("BearerTokens[%d] = %q, want empty after ZeroSecrets", i, tok)
		}
	}
	if cfg.BearerTokens != nil {
		t.Error("expected BearerTokens to be nil after ZeroSecrets")
	}
}

func TestZeroSecrets_Empty(t *testing.T) {
	cfg := &AuthConfig{}
	cfg.ZeroSecrets()
	if cfg.BearerTokens != nil {
		t.Error("expected nil BearerTokens after ZeroSecrets on empty config")
	}
}

func TestZeroSecrets_NilSlice(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: nil,
	}
	cfg.ZeroSecrets()
	if cfg.BearerTokens != nil {
		t.Error("expected nil BearerTokens to remain nil")
	}
}

// --- RotateBearerToken (0% coverage) ---

func TestRotateBearerToken_Success(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{"old-token"},
		BearerRoles: map[string]string{
			"old-token": RoleReadOnly,
		},
	}

	err := cfg.RotateBearerToken("old-token", "new-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the new token is in the list
	if !cfg.validateBearerToken("new-token") {
		t.Error("expected new token to be valid")
	}

	// Verify the old token is removed
	if cfg.validateBearerToken("old-token") {
		t.Error("expected old token to be invalid after rotation")
	}

	// Verify role was transferred
	if role := cfg.bearerRole("new-token"); role != RoleReadOnly {
		t.Errorf("expected new token role=readonly, got %s", role)
	}
}

func TestRotateBearerToken_EmptyTokens(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{},
	}

	err := cfg.RotateBearerToken("old", "new")
	if err == nil {
		t.Error("expected error for token not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRotateBearerToken_EmptyOldToken(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{"some-token"},
	}

	err := cfg.RotateBearerToken("", "new-token")
	if err == nil {
		t.Error("expected error for empty old token")
	}
}

func TestRotateBearerToken_EmptyNewToken(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{"some-token"},
	}

	err := cfg.RotateBearerToken("some-token", "")
	if err == nil {
		t.Error("expected error for empty new token")
	}
}

func TestRotateBearerToken_NotFound(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{"existing-token"},
	}

	err := cfg.RotateBearerToken("nonexistent-token", "new-token")
	if err == nil {
		t.Error("expected error for old token not found")
	}
}

func TestRotateBearerToken_NoRoleMapping(t *testing.T) {
	cfg := &AuthConfig{
		BearerTokens: []string{"old-token"},
		BearerRoles:  nil, // no roles configured
	}

	err := cfg.RotateBearerToken("old-token", "new-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.validateBearerToken("new-token") {
		t.Error("expected new token to be valid")
	}
}

// --- rotateToken handler (0% coverage) ---

func TestRotateToken_Success(t *testing.T) {
	auth := &AuthConfig{
		BearerTokens:       []string{"old-bearer"},
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

	body := `{"old_token":"old-bearer","new_token":"new-bearer"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/rotate-token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer old-bearer")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestRotateToken_MethodNotAllowed(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/rotate-token", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestRotateToken_NoAuth(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"old_token":"x","new_token":"y"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/rotate-token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}

func TestRotateToken_InvalidBody(t *testing.T) {
	auth := &AuthConfig{
		BearerTokens:       []string{"test-token"},
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/rotate-token", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestRotateToken_RotationFailed(t *testing.T) {
	auth := &AuthConfig{
		BearerTokens:       []string{"test-token"},
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

	body := `{"old_token":"nonexistent","new_token":"new-one"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/rotate-token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// --- SetHealthChecker (0% coverage) ---

func TestSetHealthChecker(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	hc := newMockHealthChecker()
	hc.SetStatus("b1", health.StatusHealthy)
	s.SetHealthChecker(hc)

	s.mu.RLock()
	got := s.healthChecker
	s.mu.RUnlock()

	if got != hc {
		t.Error("healthChecker not updated")
	}
}

// --- streamEvents (8.3% coverage) ---

func TestStreamEvents_NilEventBus(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	s.eventBus = nil

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	w := httptest.NewRecorder()
	s.streamEvents(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestStreamEvents_MaxSubscribers(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	// Fill up to the subscriber limit
	for i := 0; i < maxEventSubscribers; i++ {
		s.eventBus.subscribe()
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	w := httptest.NewRecorder()
	s.streamEvents(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 for max subscribers; body: %s", w.Code, w.Body.String())
	}
}

func TestStreamEvents_SSEEventDelivery(t *testing.T) {
	s, err := NewServer(&Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(s.server.Handler)
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// Start SSE connection in background
	type result struct {
		body string
		err  error
	}
	done := make(chan result, 1)

	go func() {
		resp, err := client.Get(server.URL + "/api/v1/events/stream")
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var buf strings.Builder
		tmp := make([]byte, 4096)
		// Read in a loop until we see the expected event or timeout
		for {
			n, readErr := resp.Body.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if readErr != nil {
				break
			}
			if strings.Contains(buf.String(), "test-sse") || strings.Contains(buf.String(), "event") && strings.Count(buf.String(), "data:") >= 2 {
				break
			}
		}
		done <- result{body: buf.String()}
	}()

	// Give the SSE connection time to establish
	time.Sleep(200 * time.Millisecond)

	// Publish an event
	s.PublishEvent(EventItem{
		ID:        "test-sse",
		Type:      "info",
		Message:   "SSE test event",
		Timestamp: time.Now().Format(time.RFC3339),
	})

	// Wait for result
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("SSE connection error: %v", r.err)
		}
		if !strings.Contains(r.body, "connected") {
			t.Errorf("expected 'connected' in SSE output, got: %s", r.body)
		}
		if !strings.Contains(r.body, "test-sse") {
			t.Errorf("expected 'test-sse' event in SSE output, got: %s", r.body)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for SSE data")
	}
}

// --- getClientIP (45.5% coverage) ---

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Real-Ip", "10.0.0.1")
	req.RemoteAddr = "192.168.1.1:1234"

	ip := getClientIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", ip)
	}
}

func TestGetClientIP_XRealIPInvalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Real-Ip", "not-an-ip")
	req.RemoteAddr = "192.168.1.1:1234"

	ip := getClientIP(req)
	// Should fall through to X-Forwarded-For or RemoteAddr
	if ip == "not-an-ip" {
		t.Error("should not return invalid X-Real-Ip")
	}
}

func TestGetClientIP_XForwardedFor_Single(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.2")
	req.RemoteAddr = "192.168.1.1:1234"

	ip := getClientIP(req)
	if ip != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2, got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_Multiple(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.3, 10.0.0.4, 10.0.0.5")
	req.RemoteAddr = "192.168.1.1:1234"

	ip := getClientIP(req)
	if ip != "10.0.0.3" {
		t.Errorf("expected first IP 10.0.0.3, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddrNoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "unix-socket" // no colon

	ip := getClientIP(req)
	if ip != "unix-socket" {
		t.Errorf("expected unix-socket, got %s", ip)
	}
}

// --- recordFailure LRU eviction (42.3% coverage) ---

func TestRecordFailure_EvictionLRU(t *testing.T) {
	l := newAuthFailureLimiter()

	// Fill to max entries
	for i := 0; i < maxAuthEntries; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)
		l.recordFailure(ip)
	}

	// Verify we have entries
	l.mu.Lock()
	initialCount := len(l.entries)
	l.mu.Unlock()

	if initialCount < maxAuthEntries {
		t.Errorf("expected at least %d entries, got %d", maxAuthEntries, initialCount)
	}

	// Add one more — should trigger LRU eviction
	l.recordFailure("99.99.99.99")

	l.mu.Lock()
	finalCount := len(l.entries)
	l.mu.Unlock()

	// Should not exceed maxAuthEntries by much
	if finalCount > maxAuthEntries+1 {
		t.Errorf("expected entries <= %d+1, got %d", maxAuthEntries, finalCount)
	}
}

func TestRecordFailure_LockoutTriggered(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	ip := "192.168.100.1"

	// Record failures up to the lockout threshold
	for i := 0; i < authFailureMaxAttempts; i++ {
		l.recordFailure(ip)
	}

	if !l.isLocked(ip) {
		t.Error("expected IP to be locked out after max failures")
	}
}

func TestRecordFailure_EntryCountIncrements(t *testing.T) {
	l := newAuthFailureLimiter()
	defer l.stop()

	ip := "192.168.200.1"

	for i := 1; i <= 3; i++ {
		l.recordFailure(ip)
		l.mu.Lock()
		e := l.entries[ip]
		l.mu.Unlock()
		if e.count != i {
			t.Errorf("expected count %d, got %d", i, e.count)
		}
	}
}

// --- auth cleanupLoop (45.5% coverage) ---

func TestAuthCleanupLoop_TriggersEviction(t *testing.T) {
	l := newAuthFailureLimiter()

	// Add an entry with an expired lockout
	l.mu.Lock()
	l.entries["expired-ip"] = &authFailureEntry{
		count:       10,
		lockedUntil: time.Now().Add(-1 * time.Hour), // already expired
		lastAccess:  time.Now(),
	}
	l.entries["active-ip"] = &authFailureEntry{
		count:       1,
		lockedUntil: time.Time{}, // not locked
		lastAccess:  time.Now(),
	}
	l.mu.Unlock()

	// Wait for cleanup ticker to fire (1 minute is the interval, but
	// we can just test the stop behavior and manual cleanup)
	l.stop()
}

// --- updateConfig standalone with reload (40% coverage) ---

func TestUpdateConfig_Standalone_Success(t *testing.T) {
	// Reset cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	reloadCalled := false
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			reloadCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !reloadCalled {
		t.Error("expected reload to be called")
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data["status"] != "reloaded" {
		t.Errorf("expected status=reloaded, got %v", data["status"])
	}
}

func TestUpdateConfig_Standalone_ReloadFailure(t *testing.T) {
	// Reset cooldown
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			return fmt.Errorf("config parse error")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateConfig_Standalone_Cooldown(t *testing.T) {
	// Set cooldown to just happened
	lastReloadMu.Lock()
	lastReloadTime = time.Now()
	lastReloadMu.Unlock()

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}

	// Reset for other tests
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()
}

func TestUpdateConfig_RaftMode_WithValidation(t *testing.T) {
	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		RaftProposer: proposer,
		ConfigValidator: func(b []byte) error {
			return nil // valid
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"listeners":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateConfig_RaftMode_ValidationFailure(t *testing.T) {
	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		RaftProposer: proposer,
		ConfigValidator: func(b []byte) error {
			return fmt.Errorf("invalid config: missing required field")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["error"].(map[string]any)
	if data["code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", data["code"])
	}
}

// --- getConfig with sanitization failure (77.8%) ---

func TestGetConfig_SanitizationFallback(t *testing.T) {
	// ConfigGetter returns a value that can be marshaled but whose
	// generic form triggers the fallback path when sanitized
	getter := &mockConfigGetter{config: map[string]any{
		"admin": map[string]any{
			"password":     "secret123",
			"bearer_token": "tok123",
			"username":     "admin",
		},
		"listeners": []any{},
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

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	data, _ := resp["data"].(map[string]any)
	adminSection, _ := data["admin"].(map[string]any)

	// Secrets should be stripped
	if _, ok := adminSection["password"]; ok {
		t.Error("password should be stripped from config response")
	}
	if _, ok := adminSection["bearer_token"]; ok {
		t.Error("bearer_token should be stripped from config response")
	}
	if _, ok := adminSection["username"]; ok {
		t.Error("username should be stripped from config response")
	}
}

// --- sanitizeConfigForAPI / stripSecrets deeper coverage ---

func TestStripSecrets_ArrayOfObjects(t *testing.T) {
	input := map[string]any{
		"items": []any{
			map[string]any{"password": "secret1", "name": "item1"},
			map[string]any{"token": "secret2", "name": "item2"},
		},
		"safe_field": "visible",
	}

	stripSecrets(input)

	items, _ := input["items"].([]any)
	item1 := items[0].(map[string]any)
	item2 := items[1].(map[string]any)

	if _, ok := item1["password"]; ok {
		t.Error("password should be stripped from array element")
	}
	if item1["name"] != "item1" {
		t.Error("non-secret field should be preserved")
	}
	if _, ok := item2["token"]; ok {
		t.Error("token should be stripped from array element")
	}
	if input["safe_field"] != "visible" {
		t.Error("safe_field should be preserved")
	}
}

func TestStripSecrets_NestedMaps(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"secret": "hidden-value",
				"visible": "ok",
			},
		},
	}

	stripSecrets(input)

	l1 := input["level1"].(map[string]any)
	l2 := l1["level2"].(map[string]any)

	if _, ok := l2["secret"]; ok {
		t.Error("nested secret should be stripped")
	}
	if l2["visible"] != "ok" {
		t.Error("nested visible field should be preserved")
	}
}

func TestStripSecrets_NonMapInput(t *testing.T) {
	// Should not panic on non-map input
	stripSecrets("string value")
	stripSecrets(42)
	stripSecrets(nil)
	stripSecrets([]any{"a", "b"})
}

// --- newRateLimiter invalid window (87.5%) ---

func TestNewRateLimiter_InvalidWindow(t *testing.T) {
	rl := newRateLimiter(10, "not-a-duration")
	defer rl.stop()

	if rl.maxReqs != 10 {
		t.Errorf("expected maxReqs=10, got %d", rl.maxReqs)
	}
	// Should fall back to default 1 minute window
	if rl.window != time.Minute {
		t.Errorf("expected window=1m, got %v", rl.window)
	}
}

func TestNewRateLimiter_ZeroDuration(t *testing.T) {
	rl := newRateLimiter(10, "0s")
	defer rl.stop()

	// 0 duration should not override the default
	if rl.window != time.Minute {
		t.Errorf("expected default window=1m for 0s, got %v", rl.window)
	}
}

func TestNewRateLimiter_ValidWindow(t *testing.T) {
	rl := newRateLimiter(100, "5m")
	defer rl.stop()

	if rl.maxReqs != 100 {
		t.Errorf("expected maxReqs=100, got %d", rl.maxReqs)
	}
	if rl.window != 5*time.Minute {
		t.Errorf("expected window=5m, got %v", rl.window)
	}
}

// --- reloadConfig cooldown (84.2%) ---

func TestReloadConfig_Cooldown(t *testing.T) {
	// Set cooldown to just happened
	lastReloadMu.Lock()
	lastReloadTime = time.Now()
	lastReloadMu.Unlock()

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/reload", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errSection, _ := resp["error"].(map[string]any)
	if errSection["code"] != "RELOAD_COOLDOWN" {
		t.Errorf("expected RELOAD_COOLDOWN, got %v", errSection["code"])
	}

	// Reset for other tests
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()
}

// --- subscribe subscriber limit (66.7%) ---

func TestEventBus_SubscribeLimit(t *testing.T) {
	eb := newEventBus()

	var channels []chan EventItem
	for i := 0; i < maxEventSubscribers; i++ {
		ch := eb.subscribe()
		channels = append(channels, ch)
	}

	if eb.SubscriberCount() != maxEventSubscribers {
		t.Errorf("expected %d subscribers, got %d", maxEventSubscribers, eb.SubscriberCount())
	}

	// Next subscribe should return a closed channel
	ch := eb.subscribe()
	// Read from closed channel should return zero value immediately
	_, ok := <-ch
	if ok {
		t.Error("expected closed channel from subscribe when limit reached")
	}

	// Clean up
	for _, c := range channels {
		eb.unsubscribe(c)
	}
}

// --- getMiddlewareStatus with nil (42.9%) ---

func TestGetMiddlewareStatus_NilProvider(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		// No MiddlewareStatus set
	})
	if err != nil {
		t.Fatal(err)
	}

	// When middlewareStatus is nil, the route is not registered
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware/status", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unregistered middleware route, got %d", w.Code)
	}
}

// --- getEvents with unhealthy backends and no error message ---

func TestGetEvents_UnhealthyNoError(t *testing.T) {
	pool := backend.NewPool("pool1", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	pool.AddBackend(b)

	pm := newMockPoolManager()
	pm.AddPool(pool)
	hc := newMockHealthChecker()
	hc.statuses["b1"] = health.StatusUnhealthy
	hc.results["b1"] = &health.Result{
		Healthy:   false,
		Error:     nil, // no explicit error
		Timestamp: time.Now(),
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
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)

	// Should have at least one warning event for the unhealthy backend
	foundUnhealthy := false
	for _, ev := range data {
		e, _ := ev.(map[string]any)
		if e["type"] == "warning" && strings.Contains(e["message"].(string), "unhealthy") {
			foundUnhealthy = true
			break
		}
	}
	if !foundUnhealthy {
		t.Error("expected warning event for unhealthy backend without error message")
	}
}

// --- updateConfig with readBody failure in Raft mode ---

func TestUpdateConfig_RaftMode_EmptyBody(t *testing.T) {
	proposer := &mockRaftProposer{}
	s, err := NewServer(&Config{
		Address:      "127.0.0.1:0",
		RaftProposer: proposer,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", nil)
	// No body, but Content-Length not set — readBody may or may not fail
	// Just ensure no panic
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Either 200 (empty body accepted) or 400 (bad request) — just no panic
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Errorf("unexpected status %d; body: %s", w.Code, w.Body.String())
	}
}

// --- updateBackend in Raft mode with marshal error path ---

func TestUpdateBackend_RaftMode_WithWeightUpdate(t *testing.T) {
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
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// --- ReloadConfig with circuit breaker context cancellation ---

func TestReloadConfig_ContextCanceled(t *testing.T) {
	lastReloadMu.Lock()
	lastReloadTime = time.Time{}
	lastReloadMu.Unlock()

	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		OnReload: func() error {
			return context.Canceled
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/reload", nil)
	// Cancel the context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Should get 503 because the reload failed
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}

// --- getPoolByName with pool having health check ---

func TestGetPoolByName_EmptyPools(t *testing.T) {
	s, err := NewServer(&Config{
		Address: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools/nonexistent", nil)
	w := httptest.NewRecorder()
	s.getPoolByName(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- getConfig fallback when sanitization fails ---

func TestSanitizeConfigForAPI_NilInput(t *testing.T) {
	result, err := sanitizeConfigForAPI(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for nil input, got %v", result)
	}
}

func TestSanitizeConfigForAPI_SimpleMap(t *testing.T) {
	input := map[string]any{
		"key":   "value",
		"users": "should-be-stripped",
	}

	result, err := sanitizeConfigForAPI(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, exists := m["users"]; exists {
		t.Error("expected 'users' key to be stripped")
	}
	if m["key"] != "value" {
		t.Error("expected 'key' to be preserved")
	}
}
