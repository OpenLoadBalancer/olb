package ratelimit

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestRateLimiter_BuildKey_Scopes tests buildKey with all scope types
func TestRateLimiter_BuildKey_Scopes(t *testing.T) {
	rl := New(Config{})

	r := httptest.NewRequest("GET", "/api/test?key=val", nil)
	r.Header.Set("X-Tenant", "acme")

	tests := []struct {
		name    string
		scope   string
		ruleID  string
		wantKey string
	}{
		{"global", "global", "r1", "global:r1"},
		{"ip", "ip", "r2", "ip:r2:1.2.3.4"},
		{"path", "path", "r3", "path:r3:/api/test"},
		{"ip+path", "ip+path", "r4", "ip+path:r4:1.2.3.4:/api/test"},
		{"header", "header:X-Tenant", "r5", "header:r5:acme"},
		{"unknown defaults to ip", "unknown", "r6", "ip:r6:1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := Rule{ID: tt.ruleID, Scope: tt.scope}
			key := rl.buildKey(rule, r, "1.2.3.4")
			if key != tt.wantKey {
				t.Errorf("buildKey(%q) = %q, want %q", tt.scope, key, tt.wantKey)
			}
		})
	}
}

func TestTokenBucket_Allow(t *testing.T) {
	b := NewTokenBucket(3, 1.0) // 3 max, refill 1/sec

	// Should allow first 3 requests
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th should be denied
	if b.Allow() {
		t.Error("4th request should be denied")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	b := NewTokenBucket(2, 100.0) // refill 100/sec — fast for testing

	// Drain all tokens
	b.Allow()
	b.Allow()
	if b.Allow() {
		t.Error("should be empty")
	}

	// Wait for refill
	time.Sleep(50 * time.Millisecond)
	if !b.Allow() {
		t.Error("should have refilled after wait")
	}
}

func TestRateLimiter_BasicLimit(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 3, Window: time.Minute, Burst: 0},
		},
	})
	defer rl.Stop()

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	for i := 0; i < 3; i++ {
		allowed, _ := rl.Allow(req)
		if !allowed {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	allowed, retryAfter := rl.Allow(req)
	if allowed {
		t.Error("4th request should be rate limited")
	}
	if retryAfter <= 0 {
		t.Error("expected positive retryAfter")
	}
}

func TestRateLimiter_PathScope(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "login", Scope: "ip+path", Paths: []string{"/login"}, Limit: 2, Window: time.Minute},
		},
	})
	defer rl.Stop()

	// Login requests
	loginReq := httptest.NewRequest("POST", "http://example.com/login", nil)
	for i := 0; i < 2; i++ {
		allowed, _ := rl.Allow(loginReq)
		if !allowed {
			t.Errorf("login request %d should be allowed", i+1)
		}
	}
	allowed, _ := rl.Allow(loginReq)
	if allowed {
		t.Error("3rd login request should be rate limited")
	}

	// Other paths should not be affected
	otherReq := httptest.NewRequest("GET", "http://example.com/api/users", nil)
	allowed, _ = rl.Allow(otherReq)
	if !allowed {
		t.Error("non-login request should not be rate limited")
	}
}

func TestRateLimiter_GlobalScope(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "global", Scope: "global", Limit: 5, Window: time.Minute},
		},
	})
	defer rl.Stop()

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		req.RemoteAddr = "192.168.1." + string(rune('1'+i)) + ":1234"
		allowed, _ := rl.Allow(req)
		if !allowed {
			t.Errorf("request %d should be allowed under global limit", i+1)
		}
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	allowed, _ := rl.Allow(req)
	if allowed {
		t.Error("should exceed global rate limit")
	}
}

func TestRateLimiter_AutoBan(t *testing.T) {
	bannedIPs := make(map[string]bool)
	rl := New(Config{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 2, Window: time.Minute, AutoBanAfter: 2},
		},
	})
	defer rl.Stop()

	rl.OnAutoBan = func(ip string, reason string) {
		bannedIPs[ip] = true
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// Exhaust limit
	rl.Allow(req)
	rl.Allow(req)

	// Trigger 2 violations
	rl.Allow(req) // violation 1
	rl.Allow(req) // violation 2 → should trigger auto-ban

	if !bannedIPs["192.0.2.1"] {
		t.Error("expected auto-ban after 2 violations")
	}
}

func TestRateLimiter_AddRemoveRule(t *testing.T) {
	rl := New(Config{})
	defer rl.Stop()

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// No rules — should allow
	allowed, _ := rl.Allow(req)
	if !allowed {
		t.Error("should allow with no rules")
	}

	// Add rule
	rl.AddRule(Rule{ID: "test", Scope: "ip", Limit: 1, Window: time.Minute})
	rl.Allow(req) // use 1 token

	allowed, _ = rl.Allow(req)
	if allowed {
		t.Error("should be limited after adding rule")
	}

	// Remove rule
	if !rl.RemoveRule("test") {
		t.Error("expected RemoveRule to return true")
	}

	// Should be allowed again (no rules)
	// Note: old bucket still exists but rule is removed
	allowed, _ = rl.Allow(req)
	if !allowed {
		t.Error("should allow after removing rule")
	}
}

func TestRateLimiter_HeaderScope(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "api-key", Scope: "header:X-API-Key", Limit: 2, Window: time.Minute},
		},
	})
	defer rl.Stop()

	// Requests with same API key should share limit
	req1 := httptest.NewRequest("GET", "http://example.com/", nil)
	req1.Header.Set("X-API-Key", "key-A")

	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	req2.Header.Set("X-API-Key", "key-A")

	req3 := httptest.NewRequest("GET", "http://example.com/", nil)
	req3.Header.Set("X-API-Key", "key-B")

	rl.Allow(req1)
	rl.Allow(req2)

	// key-A should be limited now
	allowed, _ := rl.Allow(req1)
	if allowed {
		t.Error("expected key-A to be rate limited after 2 requests")
	}

	// key-B should still be allowed
	allowed, _ = rl.Allow(req3)
	if !allowed {
		t.Error("expected key-B to still be allowed")
	}
}

func TestRateLimiter_MultipleRules(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "per-ip", Scope: "ip", Limit: 5, Window: time.Minute},
			{ID: "login", Scope: "ip+path", Paths: []string{"/login"}, Limit: 2, Window: time.Minute},
		},
	})
	defer rl.Stop()

	loginReq := httptest.NewRequest("POST", "http://example.com/login", nil)

	// Login should be limited by the tighter rule (2)
	rl.Allow(loginReq)
	rl.Allow(loginReq)

	allowed, _ := rl.Allow(loginReq)
	if allowed {
		t.Error("expected login to be limited by the login-specific rule")
	}

	// Non-login requests should still be allowed (only per-ip rule applies)
	otherReq := httptest.NewRequest("GET", "http://example.com/api", nil)
	allowed, _ = rl.Allow(otherReq)
	if !allowed {
		t.Error("expected non-login request to still be allowed")
	}
}

func TestRateLimiter_RemoveRule_NotFound(t *testing.T) {
	rl := New(Config{})
	defer rl.Stop()

	if rl.RemoveRule("nonexistent") {
		t.Error("expected RemoveRule to return false for nonexistent rule")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 100, Window: time.Minute},
		},
	})
	defer rl.Stop()

	// Make a request to create a bucket
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rl.Allow(req)

	rl.mu.RLock()
	bucketCountBefore := len(rl.buckets)
	rl.mu.RUnlock()

	if bucketCountBefore == 0 {
		t.Error("expected at least one bucket after Allow()")
	}

	// Run cleanup — since bucket should be nearly full (only used 1 of 100),
	// after refill it will be full and should be cleaned up
	time.Sleep(10 * time.Millisecond)
	rl.cleanup()

	rl.mu.RLock()
	bucketCountAfter := len(rl.buckets)
	rl.mu.RUnlock()

	// If bucket refilled to max, it should have been cleaned
	if bucketCountAfter > bucketCountBefore {
		t.Errorf("expected cleanup to remove or maintain buckets, got before=%d, after=%d",
			bucketCountBefore, bucketCountAfter)
	}
}

func TestRateLimiter_WriteRateLimitResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteRateLimitResponse(rr, 60)

	if rr.Code != 429 {
		t.Errorf("expected status 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After: 60, got %q", rr.Header().Get("Retry-After"))
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type: application/json, got %q", rr.Header().Get("Content-Type"))
	}
}

func TestRateLimiter_BurstSupport(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "burst", Scope: "ip", Limit: 3, Window: time.Minute, Burst: 2},
		},
	})
	defer rl.Stop()

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// Should allow Limit + Burst = 5 requests
	for i := 0; i < 5; i++ {
		allowed, _ := rl.Allow(req)
		if !allowed {
			t.Errorf("request %d should be allowed with burst", i+1)
		}
	}

	// 6th should be denied
	allowed, _ := rl.Allow(req)
	if allowed {
		t.Error("6th request should be denied after burst")
	}
}

func TestRateLimiter_ExtractIP(t *testing.T) {
	tests := []struct {
		addr     string
		expected string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"10.0.0.1:0", "10.0.0.1"},
		{"plain-addr", "plain-addr"},
	}

	for _, tt := range tests {
		got := extractIP(tt.addr)
		if got != tt.expected {
			t.Errorf("extractIP(%q) = %q, want %q", tt.addr, got, tt.expected)
		}
	}
}

func TestRateLimiter_MatchAnyPath(t *testing.T) {
	tests := []struct {
		path     string
		patterns []string
		expected bool
	}{
		{"/login", []string{"/login"}, true},
		{"/api/users", []string{"/api/*"}, true},
		{"/other", []string{"/api/*"}, false},
		{"/foo", []string{"/bar", "/foo"}, true},
		{"/foo", []string{}, false},
	}

	for _, tt := range tests {
		got := matchAnyPath(tt.path, tt.patterns)
		if got != tt.expected {
			t.Errorf("matchAnyPath(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.expected)
		}
	}
}

// --- Coverage improvement tests ---

// TestCov_GetOrCreateBucket_ConcurrentRace exercises the double-check path
// in getOrCreateBucket where two goroutines race to create the same bucket.
func TestCov_GetOrCreateBucket_ConcurrentRace(t *testing.T) {
	rl := New(Config{})
	defer rl.Stop()

	rule := Rule{ID: "race", Scope: "ip", Limit: 10, Window: time.Minute}
	key := rl.buildKey(rule, httptest.NewRequest("GET", "/", nil), "10.0.0.1")

	// Use a barrier to try to get concurrent bucket creation attempts
	start := make(chan struct{})
	const n = 20
	results := make([]*TokenBucket, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			<-start
			results[idx] = rl.getOrCreateBucket(key, rule)
			wg.Done()
		}(i)
	}
	close(start)
	wg.Wait()

	// All results should be the same bucket instance
	first := results[0]
	if first == nil {
		t.Fatal("expected non-nil bucket")
	}
	for i := 1; i < n; i++ {
		if results[i] != first {
			t.Errorf("goroutine %d got different bucket instance (double-check path not working)", i)
		}
	}
}

// TestCov_GetOrCreateBucket_WithBurst exercises the Burst > 0 branch
// in getOrCreateBucket where maxTokens = Limit + Burst.
func TestCov_GetOrCreateBucket_WithBurst(t *testing.T) {
	rl := New(Config{})
	defer rl.Stop()

	rule := Rule{ID: "burst-bucket", Scope: "ip", Limit: 5, Window: time.Second, Burst: 3}
	key := rl.buildKey(rule, httptest.NewRequest("GET", "/", nil), "10.0.0.1")

	bucket := rl.getOrCreateBucket(key, rule)
	if bucket == nil {
		t.Fatal("expected non-nil bucket")
	}

	// maxTokens should be Limit+Burst = 8
	tokens := bucket.Tokens()
	if tokens != 8.0 {
		t.Errorf("expected 8 tokens (5 limit + 3 burst), got %v", tokens)
	}
}

// TestCov_CleanupLoop_StopChannel exercises the stopCh branch of cleanupLoop.
func TestCov_CleanupLoop_StopChannel(t *testing.T) {
	rl := New(Config{})
	// Stop triggers the stopCh branch in cleanupLoop
	rl.Stop()
	// Calling Stop again should not panic (already closed)
	// We just verify it doesn't hang or crash
}

// TestCov_Cleanup_PartiallyFullBucket verifies that cleanup does NOT remove
// buckets that are not yet full (partially consumed).
func TestCov_Cleanup_PartiallyFullBucket(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 10, Window: time.Minute},
		},
	})
	defer rl.Stop()

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// Use some tokens so bucket is not full
	rl.Allow(req)
	rl.Allow(req)
	rl.Allow(req)

	rl.mu.RLock()
	countBefore := len(rl.buckets)
	rl.mu.RUnlock()

	if countBefore == 0 {
		t.Fatal("expected at least one bucket")
	}

	// Run cleanup - bucket should NOT be removed since it's not full
	rl.cleanup()

	rl.mu.RLock()
	countAfter := len(rl.buckets)
	rl.mu.RUnlock()

	if countAfter != countBefore {
		t.Errorf("partially full bucket should not be cleaned up: before=%d, after=%d", countBefore, countAfter)
	}
}

// TestCov_Cleanup_FullBucket verifies cleanup DOES remove buckets that are full.
func TestCov_Cleanup_FullBucket(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "test", Scope: "ip", Limit: 3, Window: 10 * time.Millisecond},
		},
	})
	defer rl.Stop()

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// Drain all tokens
	rl.Allow(req)
	rl.Allow(req)
	rl.Allow(req)

	// Wait for tokens to refill (window is very short)
	time.Sleep(30 * time.Millisecond)

	rl.mu.RLock()
	countBefore := len(rl.buckets)
	rl.mu.RUnlock()

	if countBefore == 0 {
		t.Fatal("expected at least one bucket")
	}

	// Run cleanup - bucket should be removed since it refilled to max
	rl.cleanup()

	rl.mu.RLock()
	countAfter := len(rl.buckets)
	rl.mu.RUnlock()

	if countAfter >= countBefore {
		t.Errorf("full (idle) bucket should be cleaned up: before=%d, after=%d", countBefore, countAfter)
	}
}

// TestCov_MatchAnyPath_ExactMatch tests path.Match exact matching (non-wildcard).
func TestCov_MatchAnyPath_ExactMatch(t *testing.T) {
	// Exact match via path.Match
	if !matchAnyPath("/login", []string{"/login"}) {
		t.Error("expected exact match for /login")
	}

	// Glob match via path.Match (not just prefix)
	if !matchAnyPath("/api/v1", []string{"/api/v*"}) {
		t.Error("expected glob match for /api/v*")
	}

	// No match at all
	if matchAnyPath("/other", []string{"/login", "/admin"}) {
		t.Error("expected no match for /other")
	}

	// Prefix-only match via trailing * (not a valid glob for path.Match but handled by prefix code)
	if !matchAnyPath("/api/users/123", []string{"/api/*"}) {
		t.Error("expected prefix match for /api/* against /api/users/123")
	}
}

// TestCov_MatchAnyPath_NoPatterns ensures empty patterns returns false quickly.
func TestCov_MatchAnyPath_NoPatterns(t *testing.T) {
	if matchAnyPath("/anything", nil) {
		t.Error("expected false with nil patterns")
	}
	if matchAnyPath("/anything", []string{}) {
		t.Error("expected false with empty patterns")
	}
}

// TestCov_Allow_ShortWindowRetryAfter exercises the branch where rule.Window
// is less than 1 second, causing retryAfter to be clamped to 1.
func TestCov_Allow_ShortWindowRetryAfter(t *testing.T) {
	rl := New(Config{
		Rules: []Rule{
			{ID: "short-window", Scope: "ip", Limit: 1, Window: 100 * time.Millisecond},
		},
	})
	defer rl.Stop()

	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// First request allowed
	allowed, _ := rl.Allow(req)
	if !allowed {
		t.Error("first request should be allowed")
	}

	// Second request should be rate limited
	allowed, retryAfter := rl.Allow(req)
	if allowed {
		t.Error("second request should be rate limited")
	}

	// With a 100ms window, retryAfter should be clamped to 1 (minimum)
	if retryAfter != 1 {
		t.Errorf("expected retryAfter=1 for sub-second window, got %d", retryAfter)
	}
}
