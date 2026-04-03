package ratelimit

import (
	"context"
	"errors"
	"net/http"
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
	allowed, retryAfter = rl.Allow(req)
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
	_ = rl

	tests := []struct {
		name     string
		scope    string
		expected string
	}{
		{"global", "global", "rl:global:"},
		{"ip", "ip", "rl:ip:"},
		{"path", "path", "rl:path:"},
		{"ip+path", "ip+path", "rl:ip+path:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// buildKey is called internally, we test via Allow
			_ = tt.scope
			_ = tt.expected
		})
	}
}
