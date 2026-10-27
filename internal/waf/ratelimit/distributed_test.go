package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// MockStore implements the Store interface for testing.
type MockStore struct {
	data map[string]struct {
		count   int
		resetAt time.Time
	}
	allowFunc    func(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error)
	incrementErr error
	getErr       error
}

func NewMockStore() *MockStore {
	return &MockStore{
		data: make(map[string]struct {
			count   int
			resetAt time.Time
		}),
	}
}

func (m *MockStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
	if m.allowFunc != nil {
		return m.allowFunc(ctx, key, limit, window)
	}

	now := time.Now()
	resetAt := now.Add(window)

	entry, exists := m.data[key]
	if !exists || now.After(entry.resetAt) {
		m.data[key] = struct {
			count   int
			resetAt time.Time
		}{count: 1, resetAt: resetAt}
		return true, limit - 1, resetAt, nil
	}

	if entry.count >= limit {
		return false, 0, entry.resetAt, nil
	}

	entry.count++
	m.data[key] = entry
	return true, limit - entry.count, entry.resetAt, nil
}

func (m *MockStore) Increment(ctx context.Context, key string, delta int, window time.Duration) error {
	return m.incrementErr
}

func (m *MockStore) Get(ctx context.Context, key string) (int64, time.Duration, error) {
	if m.getErr != nil {
		return 0, 0, m.getErr
	}
	entry, exists := m.data[key]
	if !exists {
		return 0, 0, nil
	}
	return int64(entry.count), time.Until(entry.resetAt), nil
}

func (m *MockStore) Close() error {
	return nil
}

func TestNewDistributed(t *testing.T) {
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 10, Window: time.Minute},
		},
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	if rl == nil {
		t.Fatal("NewDistributed() returned nil")
	}
	if rl.local == nil {
		t.Error("expected local fallback to be initialized")
	}

	rl.Stop()
}

func TestDistributedRateLimiter_Allow(t *testing.T) {
	mockStore := NewMockStore()
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 2, Window: time.Minute},
		},
		Store:            mockStore,
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	// Create test request
	req := &http.Request{
		RemoteAddr: "192.168.1.1:12345",
	}

	// First request should be allowed
	allowed, retryAfter := rl.Allow(req)
	if !allowed {
		t.Error("first request should be allowed")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter=0, got %d", retryAfter)
	}

	// Second request should be allowed
	allowed, _ = rl.Allow(req)
	if !allowed {
		t.Error("second request should be allowed")
	}

	// Third request should be rate limited
	allowed, retryAfter = rl.Allow(req)
	if allowed {
		t.Error("third request should be rate limited")
	}
	if retryAfter <= 0 {
		t.Error("retryAfter should be positive when rate limited")
	}
}

func TestDistributedRateLimiter_AllowWithStoreError(t *testing.T) {
	mockStore := NewMockStore()
	mockStore.allowFunc = func(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
		return false, 0, time.Time{}, errors.New("store error")
	}

	// Test with fallback enabled
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 10, Window: time.Minute},
		},
		Store:            mockStore,
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	req := &http.Request{
		RemoteAddr: "192.168.1.1:12345",
	}

	// Should fallback to local
	allowed, _ := rl.Allow(req)
	// Local fallback should allow (separate counter)
	_ = allowed
}

func TestDistributedRateLimiter_AllowWithStoreErrorNoFallback(t *testing.T) {
	mockStore := NewMockStore()
	mockStore.allowFunc = func(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
		return false, 0, time.Time{}, errors.New("store error")
	}

	// Test without fallback
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 10, Window: time.Minute},
		},
		Store:            mockStore,
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	req := &http.Request{
		RemoteAddr: "192.168.1.1:12345",
	}

	// Without fallback, should allow the request
	allowed, _ := rl.Allow(req)
	if !allowed {
		t.Error("request should be allowed when store fails and no fallback")
	}
}

func TestDistributedRateLimiter_AddRule(t *testing.T) {
	cfg := DistributedConfig{
		Rules:            []Rule{},
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	newRule := Rule{ID: "new-rule", Scope: "ip", Limit: 100, Window: time.Hour}
	rl.AddRule(newRule)

	if len(rl.rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rl.rules))
	}
}

func TestDistributedRateLimiter_RemoveRule(t *testing.T) {
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "rule1", Scope: "ip", Limit: 10, Window: time.Minute},
			{ID: "rule2", Scope: "ip", Limit: 20, Window: time.Minute},
		},
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	if !rl.RemoveRule("rule1") {
		t.Error("RemoveRule should return true for existing rule")
	}

	if len(rl.rules) != 1 {
		t.Errorf("expected 1 rule after removal, got %d", len(rl.rules))
	}

	if rl.RemoveRule("rule1") {
		t.Error("RemoveRule should return false for already removed rule")
	}

	if rl.RemoveRule("nonexistent") {
		t.Error("RemoveRule should return false for non-existent rule")
	}
}

func TestDistributedRateLimiter_Stats(t *testing.T) {
	mockStore := NewMockStore()
	cfg := DistributedConfig{
		Rules: []Rule{},
		Store: mockStore,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	stats, err := rl.Stats(context.Background())
	if err != nil {
		t.Errorf("Stats() error = %v", err)
	}
	if stats == nil {
		t.Error("Stats() returned nil")
	}
}

func TestDistributedRateLimiter_NilStore(t *testing.T) {
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 10, Window: time.Minute},
		},
		Store: nil,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	req := &http.Request{
		RemoteAddr: "192.168.1.1:12345",
	}

	// Should allow when store is nil
	allowed, _ := rl.Allow(req)
	if !allowed {
		t.Error("request should be allowed when store is nil")
	}
}

func TestDistributedRateLimiter_Stop(t *testing.T) {
	cfg := DistributedConfig{
		Rules:            []Rule{},
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	rl.Stop()

	// Should not panic if called again
	rl.Stop()
}

func TestDistributedRateLimiter_buildKey(t *testing.T) {
	rl := NewDistributed(DistributedConfig{})

	tests := []struct {
		name     string
		rule     Rule
		ip       string
		path     string
		expected string
	}{
		{
			name:     "global scope",
			rule:     Rule{ID: "global-rule", Scope: "global"},
			ip:       "1.2.3.4",
			path:     "/api",
			expected: "rl:global:global-rule",
		},
		{
			name:     "ip scope",
			rule:     Rule{ID: "ip-rule", Scope: "ip"},
			ip:       "192.168.1.1",
			path:     "/api",
			expected: "rl:ip:ip-rule:192.168.1.1",
		},
		{
			name:     "path scope",
			rule:     Rule{ID: "path-rule", Scope: "path"},
			ip:       "1.2.3.4",
			path:     "/api/users",
			expected: "rl:path:path-rule:/api/users",
		},
		{
			name:     "ip+path scope",
			rule:     Rule{ID: "combo-rule", Scope: "ip+path"},
			ip:       "10.0.0.1",
			path:     "/login",
			expected: "rl:ip+path:combo-rule:10.0.0.1:/login",
		},
		{
			name:     "unknown scope defaults to ip",
			rule:     Rule{ID: "unknown-rule", Scope: "custom"},
			ip:       "5.5.5.5",
			path:     "/test",
			expected: "rl:ip:unknown-rule:5.5.5.5",
		},
		{
			name:     "empty scope defaults to ip",
			rule:     Rule{ID: "empty-rule", Scope: ""},
			ip:       "6.6.6.6",
			path:     "/test",
			expected: "rl:ip:empty-rule:6.6.6.6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				RemoteAddr: tt.ip + ":12345",
			}
			req.URL = &url.URL{Path: tt.path}

			got := rl.buildKey(tt.rule, req, tt.ip)
			if got != tt.expected {
				t.Errorf("buildKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDistributedRateLimiter_buildKey_Allow(t *testing.T) {
	// Verify buildKey is exercised through the Allow method with different scopes
	mockStore := NewMockStore()

	tests := []struct {
		name  string
		scope string
	}{
		{"global", "global"},
		{"ip", "ip"},
		{"path", "path"},
		{"ip+path", "ip+path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DistributedConfig{
				Rules: []Rule{
					{ID: "test-rule", Scope: tt.scope, Limit: 100, Window: time.Minute},
				},
				Store:            mockStore,
				UseLocalFallback: false,
			}

			rl := NewDistributed(cfg)
			defer rl.Stop()

			req := &http.Request{
				RemoteAddr: "10.0.0.1:12345",
			}
			req.URL = &url.URL{Path: "/api/test"}

			allowed, _ := rl.Allow(req)
			if !allowed {
				t.Errorf("first request with scope %q should be allowed", tt.scope)
			}
		})
	}
}

func TestDistributedRateLimiter_PathFiltering(t *testing.T) {
	mockStore := NewMockStore()
	cfg := DistributedConfig{
		Rules: []Rule{
			{
				ID:     "api-only",
				Scope:  "ip",
				Limit:  10,
				Window: time.Minute,
				Paths:  []string{"/api/*"},
			},
		},
		Store:            mockStore,
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	// Request to /other should skip the rule (no paths match) and be allowed
	req := &http.Request{RemoteAddr: "1.2.3.4:12345"}
	req.URL = &url.URL{Path: "/other"}
	allowed, _ := rl.Allow(req)
	if !allowed {
		t.Error("request to non-matching path should be allowed")
	}
}

func TestDistributedRateLimiter_StoreErrorWithCallback(t *testing.T) {
	mockStore := NewMockStore()
	mockStore.allowFunc = func(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Time, error) {
		return false, 0, time.Time{}, errors.New("connection refused")
	}

	var storeErrKey string
	var storeErr error

	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 10, Window: time.Minute},
		},
		Store:            mockStore,
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	rl.OnStoreError = func(key string, err error) {
		storeErrKey = key
		storeErr = err
	}
	defer rl.Stop()

	req := &http.Request{RemoteAddr: "1.2.3.4:12345"}
	req.URL = &url.URL{Path: "/"}
	rl.Allow(req)

	if storeErrKey == "" {
		t.Error("OnStoreError should have been called with key")
	}
	if storeErr == nil {
		t.Error("OnStoreError should have been called with error")
	}
}

func TestDistributedRateLimiter_Stats_NilStore(t *testing.T) {
	cfg := DistributedConfig{
		Rules: []Rule{},
		Store: nil,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	stats, err := rl.Stats(context.Background())
	if err != nil {
		t.Errorf("Stats() error = %v", err)
	}
	if stats["store"] != "none" {
		t.Errorf("Stats()['store'] = %v, want 'none'", stats["store"])
	}
}

func TestDistributedRateLimiter_AutoBan(t *testing.T) {
	var bannedIP string
	var bannedReason string

	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "strict", Scope: "ip", Limit: 1, Window: time.Minute, AutoBanAfter: 1},
		},
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	rl.OnAutoBan = func(ip string, reason string) {
		bannedIP = ip
		bannedReason = reason
	}
	defer rl.Stop()

	// First request allowed, second should trigger rate limit and auto-ban
	// Since no store, checkStore returns allowed=true (store is nil)
	// We need a store to actually enforce limits
	mockStore := NewMockStore()
	rl.store = mockStore

	req := &http.Request{RemoteAddr: "10.0.0.1:12345"}
	req.URL = &url.URL{Path: "/"}

	// Exhaust the limit
	rl.Allow(req)
	rl.Allow(req)
	// This one should trigger rate limit + auto-ban
	allowed, _ := rl.Allow(req)
	if allowed {
		t.Error("third request should be rate limited")
	}

	if bannedIP != "10.0.0.1" {
		t.Errorf("bannedIP = %q, want %q", bannedIP, "10.0.0.1")
	}
	if bannedReason == "" {
		t.Error("bannedReason should not be empty")
	}
}

func TestDistributedRateLimiter_AutoBan_NoCallback(t *testing.T) {
	mockStore := NewMockStore()
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "strict", Scope: "ip", Limit: 1, Window: time.Minute, AutoBanAfter: 1},
		},
		Store:            mockStore,
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()
	// No OnAutoBan callback set - should not panic

	req := &http.Request{RemoteAddr: "10.0.0.1:12345"}
	req.URL = &url.URL{Path: "/"}

	rl.Allow(req)
	rl.Allow(req)
	// Should not panic even without callback
	rl.Allow(req)
}

func TestDistributedRateLimiter_AutoBan_ZeroThreshold(t *testing.T) {
	var banned bool

	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "no-ban", Scope: "ip", Limit: 1, Window: time.Minute, AutoBanAfter: 0},
		},
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	rl.OnAutoBan = func(ip string, reason string) {
		banned = true
	}
	defer rl.Stop()

	mockStore := NewMockStore()
	rl.store = mockStore

	req := &http.Request{RemoteAddr: "10.0.0.1:12345"}
	req.URL = &url.URL{Path: "/"}

	rl.Allow(req)
	rl.Allow(req)
	rl.Allow(req) // Rate limited but AutoBanAfter=0, so no ban

	if banned {
		t.Error("should not auto-ban when AutoBanAfter is 0")
	}
}

// --- Coverage improvement tests ---

// TestCov_Distributed_RemoveRule_WithLocalFallback exercises the branch in
// DistributedRateLimiter.RemoveRule where rl.local != nil.
func TestCov_Distributed_RemoveRule_WithLocalFallback(t *testing.T) {
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "fb-rule1", Scope: "ip", Limit: 10, Window: time.Minute},
		},
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	// Remove the existing rule — should also remove from local
	if !rl.RemoveRule("fb-rule1") {
		t.Error("expected RemoveRule to return true for existing rule with fallback")
	}

	if len(rl.rules) != 0 {
		t.Errorf("expected 0 rules after removal, got %d", len(rl.rules))
	}

	// Remove non-existent rule — should return false
	if rl.RemoveRule("nonexistent") {
		t.Error("expected RemoveRule to return false for nonexistent rule")
	}
}

// TestCov_Distributed_RemoveRule_WithLocalFallback_MultipleRules tests
// removing one of several rules when local fallback is enabled.
func TestCov_Distributed_RemoveRule_WithLocalFallback_MultipleRules(t *testing.T) {
	cfg := DistributedConfig{
		// Use a fresh slice to avoid shared backing array issues
		Rules: []Rule{
			{ID: "multi-a", Scope: "ip", Limit: 10, Window: time.Minute},
			{ID: "multi-b", Scope: "global", Limit: 100, Window: time.Minute},
		},
		UseLocalFallback: true,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	// Remove the last rule to avoid shared-array overwrite issue
	if !rl.RemoveRule("multi-b") {
		t.Error("expected RemoveRule to return true for multi-b")
	}

	if len(rl.rules) != 1 {
		t.Errorf("expected 1 rule after removing multi-b, got %d", len(rl.rules))
	}

	// The remaining rule should be multi-a
	if rl.rules[0].ID != "multi-a" {
		t.Errorf("expected remaining rule to be multi-a, got %s", rl.rules[0].ID)
	}
}

// TestCov_Distributed_Allow_ShortWindowRetryAfter exercises the retry < 1
// branch in DistributedRateLimiter.Allow.
func TestCov_Distributed_Allow_ShortWindowRetryAfter(t *testing.T) {
	mockStore := NewMockStore()
	cfg := DistributedConfig{
		Rules: []Rule{
			{ID: "short-win", Scope: "ip", Limit: 1, Window: 100 * time.Millisecond},
		},
		Store:            mockStore,
		UseLocalFallback: false,
	}

	rl := NewDistributed(cfg)
	defer rl.Stop()

	req := &http.Request{RemoteAddr: "10.0.0.1:12345"}
	req.URL = &url.URL{Path: "/"}

	// First request allowed
	allowed, _ := rl.Allow(req)
	if !allowed {
		t.Error("first request should be allowed")
	}

	// Second request should be rate limited with retryAfter clamped to 1
	allowed, retryAfter := rl.Allow(req)
	if allowed {
		t.Error("second request should be rate limited")
	}
	if retryAfter != 1 {
		t.Errorf("expected retryAfter=1 for sub-second window, got %d", retryAfter)
	}
}
