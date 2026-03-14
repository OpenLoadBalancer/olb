package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Algorithm != TokenBucket {
		t.Errorf("Algorithm = %v, want TokenBucket", config.Algorithm)
	}
	if config.Backend != MemoryBackend {
		t.Errorf("Backend = %v, want MemoryBackend", config.Backend)
	}
	if config.Rate != 100 {
		t.Errorf("Rate = %v, want 100", config.Rate)
	}
	if config.Burst != 150 {
		t.Errorf("Burst = %v, want 150", config.Burst)
	}
	if config.Window != time.Minute {
		t.Errorf("Window = %v, want 1m", config.Window)
	}
	if config.KeyPrefix != "ratelimit:" {
		t.Errorf("KeyPrefix = %q, want ratelimit:", config.KeyPrefix)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "zero rate",
			config: &Config{
				Rate:  0,
				Burst: 10,
			},
			wantErr: true,
		},
		{
			name: "negative rate",
			config: &Config{
				Rate:  -1,
				Burst: 10,
			},
			wantErr: true,
		},
		{
			name: "zero burst",
			config: &Config{
				Rate:  100,
				Burst: 0,
			},
			wantErr: true,
		},
		{
			name: "zero window adjusted",
			config: &Config{
				Rate:   100,
				Burst:  10,
				Window: 0,
			},
			wantErr: false, // Should default to 1m
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewLimiter(t *testing.T) {
	config := DefaultConfig()
	limiter, err := NewLimiter(config)
	if err != nil {
		t.Fatalf("NewLimiter error: %v", err)
	}
	defer limiter.Close()

	if limiter.config != config {
		t.Error("Config mismatch")
	}
	if limiter.store == nil {
		t.Error("Store should not be nil")
	}
}

func TestNewLimiter_NilConfig(t *testing.T) {
	limiter, err := NewLimiter(nil)
	if err != nil {
		t.Fatalf("NewLimiter(nil) error: %v", err)
	}
	defer limiter.Close()

	if limiter.config == nil {
		t.Error("Config should use defaults when nil")
	}
}

func TestNewLimiter_InvalidBackend(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   "invalid",
		Rate:      100,
		Burst:     10,
	}

	_, err := NewLimiter(config)
	if err == nil {
		t.Error("Expected error for invalid backend")
	}
}

func TestLimiter_Allow(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   MemoryBackend,
		Rate:      10,
		Burst:     5,
		KeyPrefix: "test:",
	}

	limiter, err := NewLimiter(config)
	if err != nil {
		t.Fatalf("NewLimiter error: %v", err)
	}
	defer limiter.Close()

	// First requests should be allowed (burst)
	for i := 0; i < 5; i++ {
		result, err := limiter.Allow("client1")
		if err != nil {
			t.Fatalf("Allow error: %v", err)
		}
		if !result.Allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
		if result.Limit != 5 {
			t.Errorf("Limit = %d, want 5", result.Limit)
		}
	}

	// 6th request should be denied
	result, err := limiter.Allow("client1")
	if err != nil {
		t.Fatalf("Allow error: %v", err)
	}
	if result.Allowed {
		t.Error("6th request should be denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when denied")
	}

	// Different client should still be allowed
	result2, err := limiter.Allow("client2")
	if err != nil {
		t.Fatalf("Allow error: %v", err)
	}
	if !result2.Allowed {
		t.Error("Different client should be allowed")
	}
}

func TestLimiter_AllowN(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   MemoryBackend,
		Rate:      10,
		Burst:     10,
	}

	limiter, err := NewLimiter(config)
	if err != nil {
		t.Fatalf("NewLimiter error: %v", err)
	}
	defer limiter.Close()

	// Allow 5 at once
	result, err := limiter.AllowN("client", 5)
	if err != nil {
		t.Fatalf("AllowN error: %v", err)
	}
	if !result.Allowed {
		t.Error("AllowN(5) should be allowed with burst 10")
	}

	// Allow 5 more
	result, err = limiter.AllowN("client", 5)
	if err != nil {
		t.Fatalf("AllowN error: %v", err)
	}
	if !result.Allowed {
		t.Error("AllowN(5) should be allowed with remaining 5")
	}

	// Should deny 1 more
	result, err = limiter.AllowN("client", 1)
	if err != nil {
		t.Fatalf("AllowN error: %v", err)
	}
	if result.Allowed {
		t.Error("AllowN(1) should be denied when depleted")
	}
}

func TestLimiter_Get(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   MemoryBackend,
		Rate:      10,
		Burst:     10,
	}

	limiter, err := NewLimiter(config)
	if err != nil {
		t.Fatalf("NewLimiter error: %v", err)
	}
	defer limiter.Close()

	// Get state for new key
	state, err := limiter.Get("newkey")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if state.Tokens < 9.5 || state.Tokens > 10.5 {
		t.Errorf("Tokens = %f, want ~10", state.Tokens)
	}

	// Use some tokens
	limiter.AllowN("newkey", 3)

	// Get updated state
	state, err = limiter.Get("newkey")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if state.Tokens < 6.5 || state.Tokens > 7.5 {
		t.Errorf("Tokens = %f, want ~7", state.Tokens)
	}
}

func TestLimiter_TokenRefill(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   MemoryBackend,
		Rate:      10, // 10 tokens per second
		Burst:     5,
	}

	limiter, err := NewLimiter(config)
	if err != nil {
		t.Fatalf("NewLimiter error: %v", err)
	}
	defer limiter.Close()

	// Use all tokens
	limiter.AllowN("client", 5)

	// Should be denied
	result, _ := limiter.Allow("client")
	if result.Allowed {
		t.Error("Should be denied after using all tokens")
	}

	// Wait for refill
	time.Sleep(200 * time.Millisecond)

	// Should have 2 tokens now (10 * 0.2 = 2)
	result, _ = limiter.AllowN("client", 1)
	if !result.Allowed {
		t.Error("Should be allowed after refill")
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   MemoryBackend,
		Rate:      1000,
		Burst:     1000,
	}

	limiter, err := NewLimiter(config)
	if err != nil {
		t.Fatalf("NewLimiter error: %v", err)
	}
	defer limiter.Close()

	var wg sync.WaitGroup
	allowed := atomic.Int32{}
	denied := atomic.Int32{}

	// 10 goroutines, each making 100 requests
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				result, _ := limiter.Allow("shared")
				if result.Allowed {
					allowed.Add(1)
				} else {
					denied.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// Total allowed should not exceed burst
	if allowed.Load() > 1000 {
		t.Errorf("Allowed = %d, should not exceed 1000", allowed.Load())
	}

	// Most should be allowed (1000 burst / 1000 requests)
	if allowed.Load() < 900 {
		t.Errorf("Allowed = %d, expected at least 900", allowed.Load())
	}
}

func TestMemoryStore_Cleanup(t *testing.T) {
	config := &Config{
		Rate:  10,
		Burst: 10,
	}

	store := newMemoryStore(config)
	defer store.Close()

	// Add some buckets
	for i := 0; i < 10; i++ {
		store.Allow(fmt.Sprintf("client%d", i), 1)
	}

	if len(store.buckets) != 10 {
		t.Fatalf("Expected 10 buckets, got %d", len(store.buckets))
	}

	// Close store to prevent cleanup goroutine from interfering
	store.Close()

	// Note: Cleanup runs every minute, so we can't easily test it in unit tests
	// The cleanup goroutine is started in NewLimiter
}

func TestNewMultiZoneLimiter(t *testing.T) {
	mzl := NewMultiZoneLimiter()
	if mzl == nil {
		t.Fatal("NewMultiZoneLimiter returned nil")
	}
	if mzl.zones == nil {
		t.Error("zones map should not be nil")
	}
}

func TestMultiZoneLimiter_AddZone(t *testing.T) {
	mzl := NewMultiZoneLimiter()

	zone := &Zone{
		Name:  "api",
		Rate:  100,
		Burst: 150,
	}

	err := mzl.AddZone(zone)
	if err != nil {
		t.Fatalf("AddZone error: %v", err)
	}

	limiter, ok := mzl.GetZone("api")
	if !ok {
		t.Fatal("Zone not found")
	}
	if limiter == nil {
		t.Error("Limiter should not be nil")
	}
}

func TestMultiZoneLimiter_RemoveZone(t *testing.T) {
	mzl := NewMultiZoneLimiter()

	zone := &Zone{
		Name:  "api",
		Rate:  100,
		Burst: 150,
	}

	mzl.AddZone(zone)
	mzl.RemoveZone("api")

	_, ok := mzl.GetZone("api")
	if ok {
		t.Error("Zone should have been removed")
	}
}

func TestMultiZoneLimiter_Check(t *testing.T) {
	mzl := NewMultiZoneLimiter()
	defer mzl.Close()

	// Add two zones
	mzl.AddZone(&Zone{
		Name:  "strict",
		Rate:  1,
		Burst: 1,
	})
	mzl.AddZone(&Zone{
		Name:  "lenient",
		Rate:  1000,
		Burst: 1000,
	})

	// First request should pass both
	allowed, results := mzl.Check("client1")
	if !allowed {
		t.Error("First request should be allowed")
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Second request should fail strict but pass lenient
	allowed, results = mzl.Check("client1")
	if allowed {
		t.Error("Second request should be denied (strict zone)")
	}
	if results["strict"].Allowed {
		t.Error("Strict zone should deny")
	}
	if !results["lenient"].Allowed {
		t.Error("Lenient zone should allow")
	}
}

func TestKeyFunc(t *testing.T) {
	tests := []struct {
		name     string
		fn       KeyFunc
		expected string
	}{
		{
			name:     "KeyByIP",
			fn:       KeyByIP,
			expected: "ip:unknown",
		},
		{
			name:     "KeyByHeader",
			fn:       KeyByHeader("X-API-Key"),
			expected: "header:X-API-Key",
		},
		{
			name:     "KeyByCookie",
			fn:       KeyByCookie("session_id"),
			expected: "cookie:session_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(nil)
			if got != tt.expected {
				t.Errorf("KeyFunc() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func BenchmarkLimiter_Allow(b *testing.B) {
	config := &Config{
		Algorithm: TokenBucket,
		Backend:   MemoryBackend,
		Rate:      10000,
		Burst:     10000,
	}

	limiter, _ := NewLimiter(config)
	defer limiter.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			limiter.Allow(fmt.Sprintf("client%d", i%100))
			i++
		}
	})
}
