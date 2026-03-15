package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testHandler returns a handler that writes the given status, body, and
// optional headers. It also increments the provided counter so tests can
// track how many times the backend was actually invoked.
func testHandler(status int, body string, counter *int, extraHeaders map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if counter != nil {
			*counter++
		}
		for k, v := range extraHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	})
}

func TestCacheMiddleware_Name(t *testing.T) {
	c := NewCacheMiddleware(DefaultCacheConfig())
	if c.Name() != "cache" {
		t.Errorf("Name() = %q, want %q", c.Name(), "cache")
	}
}

func TestCacheMiddleware_Priority(t *testing.T) {
	c := NewCacheMiddleware(DefaultCacheConfig())
	if c.Priority() != PriorityCache {
		t.Errorf("Priority() = %d, want %d", c.Priority(), PriorityCache)
	}
}

func TestCacheMiddleware_MissThenHit(t *testing.T) {
	var calls int
	backend := testHandler(200, "hello", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// First request — miss.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/path", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	if rr1.Code != 200 {
		t.Fatalf("first request status = %d, want 200", rr1.Code)
	}
	if rr1.Body.String() != "hello" {
		t.Fatalf("first request body = %q, want %q", rr1.Body.String(), "hello")
	}
	if rr1.Header().Get("X-Cache") != "MISS" {
		t.Errorf("first request X-Cache = %q, want MISS", rr1.Header().Get("X-Cache"))
	}
	if calls != 1 {
		t.Fatalf("backend calls = %d, want 1", calls)
	}

	// Second request — hit.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/path", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != 200 {
		t.Fatalf("second request status = %d, want 200", rr2.Code)
	}
	if rr2.Body.String() != "hello" {
		t.Fatalf("second request body = %q, want %q", rr2.Body.String(), "hello")
	}
	if rr2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("second request X-Cache = %q, want HIT", rr2.Header().Get("X-Cache"))
	}
	if calls != 1 {
		t.Fatalf("backend should not be called again, calls = %d", calls)
	}
}

func TestCacheMiddleware_CacheKeyDifferentQueries(t *testing.T) {
	var calls int
	backend := testHandler(200, "response", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Request with ?a=1
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/path?a=1", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	// Request with ?a=2 — different query, should miss.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/path?a=2", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls for different queries, got %d", calls)
	}

	// Same query with params in different order should produce the same key.
	keyA := CacheKey(httptest.NewRequest("GET", "http://example.com/?b=2&a=1", nil))
	keyB := CacheKey(httptest.NewRequest("GET", "http://example.com/?a=1&b=2", nil))
	if keyA != keyB {
		t.Errorf("keys should be equal for reordered params: %q != %q", keyA, keyB)
	}
}

func TestCacheMiddleware_TTLExpiry(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.DefaultTTL = 50 * time.Millisecond
	cfg.StaleWhileRevalidate = 0

	var calls int
	backend := testHandler(200, "fresh", &calls, nil)
	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	// Prime the cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/ttl", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// Should be a miss now.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/ttl", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 calls after TTL expiry, got %d", calls)
	}
	if rr2.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q after TTL expiry, want MISS", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_MaxEntriesEviction(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.MaxEntries = 3

	cache := NewCacheMiddleware(cfg)
	backend := testHandler(200, "body", nil, nil)
	handler := cache.Wrap(backend)

	// Insert 4 entries (capacity is 3).
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/entry"+strconv.Itoa(i), nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Should have evicted the oldest entry.
	if cache.Len() != 3 {
		t.Errorf("cache.Len() = %d, want 3", cache.Len())
	}
}

func TestCacheMiddleware_MaxSizeEviction(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.MaxSize = 100 // 100 bytes total

	body := strings.Repeat("x", 40) // 40 bytes each
	cache := NewCacheMiddleware(cfg)
	backend := testHandler(200, body, nil, nil)
	handler := cache.Wrap(backend)

	// Insert 3 entries (3*40 = 120 > 100, so some must be evicted).
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/size"+strconv.Itoa(i), nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	if cache.Size() > cfg.MaxSize {
		t.Errorf("cache.Size() = %d, exceeds MaxSize %d", cache.Size(), cfg.MaxSize)
	}
}

func TestCacheMiddleware_CacheControlNoStore(t *testing.T) {
	var calls int
	backend := testHandler(200, "secret", &calls, map[string]string{
		"Cache-Control": "no-store",
	})
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// First request.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/nostore", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	// Second request — should still miss because no-store.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/nostore", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (no-store), got %d", calls)
	}
	if rr2.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_CacheControlNoCache(t *testing.T) {
	var calls int
	backend := testHandler(200, "revalidate-me", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/nocache", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Request with Cache-Control: no-cache — should bypass cache.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/nocache", nil)
	req2.Header.Set("Cache-Control", "no-cache")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (no-cache forces revalidation), got %d", calls)
	}
}

func TestCacheMiddleware_CacheControlMaxAge(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.RespectCacheControl = true

	var calls int
	backend := testHandler(200, "max-age-response", &calls, map[string]string{
		"Cache-Control": "max-age=1",
	})
	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/maxage", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	// Immediate second request should be a hit.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/maxage", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("X-Cache = %q, want HIT", rr2.Header().Get("X-Cache"))
	}
	if calls != 1 {
		t.Errorf("expected 1 backend call, got %d", calls)
	}
}

func TestCacheMiddleware_CacheControlSMaxAge(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.RespectCacheControl = true
	cfg.StaleWhileRevalidate = 0

	var calls int
	backend := testHandler(200, "s-maxage-response", &calls, map[string]string{
		"Cache-Control": "s-maxage=1, max-age=3600",
	})
	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/smaxage", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Should use s-maxage (1s) not max-age (3600s).
	// Wait for s-maxage to expire.
	time.Sleep(1100 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/smaxage", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (s-maxage expired), got %d", calls)
	}
}

func TestCacheMiddleware_OnlyGETandHEADCached(t *testing.T) {
	var calls int
	backend := testHandler(200, "body", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// GET should be cached.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/methods", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/methods", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("GET second request X-Cache = %q, want HIT", rr2.Header().Get("X-Cache"))
	}

	// HEAD should be cached.
	calls = 0
	req3 := httptest.NewRequest(http.MethodHead, "http://example.com/head-test", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req3)

	req4 := httptest.NewRequest(http.MethodHead, "http://example.com/head-test", nil)
	rr4 := httptest.NewRecorder()
	handler.ServeHTTP(rr4, req4)
	if rr4.Header().Get("X-Cache") != "HIT" {
		t.Errorf("HEAD second request X-Cache = %q, want HIT", rr4.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_POSTNotCached(t *testing.T) {
	var calls int
	backend := testHandler(200, "body", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// POST should not be cached.
	req1 := httptest.NewRequest(http.MethodPost, "http://example.com/post", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "http://example.com/post", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("POST should not be cached, expected 2 calls, got %d", calls)
	}
	if rr2.Header().Get("X-Cache") != "MISS" {
		t.Errorf("POST X-Cache = %q, want MISS", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_XCacheHeader(t *testing.T) {
	backend := testHandler(200, "body", nil, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Miss.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/xcache", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Header().Get("X-Cache") != "MISS" {
		t.Errorf("first X-Cache = %q, want MISS", rr1.Header().Get("X-Cache"))
	}

	// Hit.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/xcache", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("second X-Cache = %q, want HIT", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_AgeHeader(t *testing.T) {
	backend := testHandler(200, "body", nil, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/age", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Wait a moment.
	time.Sleep(1100 * time.Millisecond)

	// Second request should have Age >= 1.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/age", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	ageStr := rr2.Header().Get("Age")
	if ageStr == "" {
		t.Fatal("Age header not set")
	}
	age, err := strconv.Atoi(ageStr)
	if err != nil {
		t.Fatalf("Age header %q is not an integer: %v", ageStr, err)
	}
	if age < 1 {
		t.Errorf("Age = %d, expected >= 1", age)
	}
}

func TestCacheMiddleware_StaleWhileRevalidate(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.DefaultTTL = 50 * time.Millisecond
	cfg.StaleWhileRevalidate = 5 * time.Second

	var mu sync.Mutex
	calls := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(200)
		w.Write([]byte("body"))
	})

	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/stale", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Wait for TTL to expire but remain within stale window.
	time.Sleep(100 * time.Millisecond)

	// Should get a STALE response.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/stale", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Header().Get("X-Cache") != "STALE" {
		t.Errorf("X-Cache = %q, want STALE", rr2.Header().Get("X-Cache"))
	}
	if rr2.Body.String() != "body" {
		t.Errorf("body = %q, want %q", rr2.Body.String(), "body")
	}

	// Wait a moment for background revalidation to complete.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	c := calls
	mu.Unlock()
	if c < 2 {
		t.Errorf("expected background revalidation to trigger, calls = %d", c)
	}
}

func TestCacheMiddleware_AuthorizationHeaderSkipsCache(t *testing.T) {
	var calls int
	backend := testHandler(200, "secret", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Request with Authorization header should not be cached.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/auth", nil)
	req1.Header.Set("Authorization", "Bearer token123")
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/auth", nil)
	req2.Header.Set("Authorization", "Bearer token123")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (Authorization skips cache), got %d", calls)
	}
	if rr2.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_AuthorizationHeaderCachedWhenConfigured(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.CacheAuthenticatedRequests = true

	var calls int
	backend := testHandler(200, "allowed", &calls, nil)
	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/authok", nil)
	req1.Header.Set("Authorization", "Bearer token123")
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/authok", nil)
	req2.Header.Set("Authorization", "Bearer token123")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 1 {
		t.Errorf("expected 1 backend call when CacheAuthenticatedRequests=true, got %d", calls)
	}
	if rr2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("X-Cache = %q, want HIT", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_Purge(t *testing.T) {
	backend := testHandler(200, "body", nil, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Prime cache.
	req := httptest.NewRequest(http.MethodGet, "http://example.com/purge-me", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if cache.Len() != 1 {
		t.Fatalf("cache.Len() = %d, want 1", cache.Len())
	}

	// Purge the entry.
	key := CacheKey(req)
	if !cache.Purge(key) {
		t.Error("Purge returned false for existing key")
	}
	if cache.Len() != 0 {
		t.Errorf("cache.Len() = %d after purge, want 0", cache.Len())
	}

	// Purge non-existent key.
	if cache.Purge("nonexistent") {
		t.Error("Purge returned true for non-existent key")
	}
}

func TestCacheMiddleware_PurgeAll(t *testing.T) {
	backend := testHandler(200, "body", nil, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Prime cache with several entries.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/purge-all/"+strconv.Itoa(i), nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if cache.Len() != 5 {
		t.Fatalf("cache.Len() = %d, want 5", cache.Len())
	}

	cache.PurgeAll()

	if cache.Len() != 0 {
		t.Errorf("cache.Len() = %d after PurgeAll, want 0", cache.Len())
	}
	if cache.Size() != 0 {
		t.Errorf("cache.Size() = %d after PurgeAll, want 0", cache.Size())
	}
}

func TestCacheMiddleware_CacheControlPrivate(t *testing.T) {
	var calls int
	backend := testHandler(200, "private-data", &calls, map[string]string{
		"Cache-Control": "private",
	})
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/private", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/private", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (private response), got %d", calls)
	}
}

func TestCacheMiddleware_NonCacheableStatus(t *testing.T) {
	var calls int
	backend := testHandler(500, "error", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/500", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/500", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (500 not cacheable), got %d", calls)
	}
}

func TestCacheMiddleware_ResponseTooLarge(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.MaxResponseSize = 10

	var calls int
	backend := testHandler(200, strings.Repeat("x", 20), &calls, nil)
	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/large", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/large", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (response too large), got %d", calls)
	}
	if cache.Len() != 0 {
		t.Errorf("cache.Len() = %d, want 0 (response too large)", cache.Len())
	}
}

func TestCacheMiddleware_Stats(t *testing.T) {
	backend := testHandler(200, "body", nil, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Miss.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/stats", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Hit.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/stats", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	hits, misses := cache.Stats()
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
	if misses != 1 {
		t.Errorf("misses = %d, want 1", misses)
	}
}

func TestCacheMiddleware_ConcurrentAccess(t *testing.T) {
	backend := testHandler(200, "concurrent", nil, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "http://example.com/concurrent?n="+strconv.Itoa(n%5), nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != 200 {
				t.Errorf("concurrent request %d status = %d", n, rr.Code)
			}
		}(i)
	}
	wg.Wait()

	// Should have at most 5 unique entries (5 distinct query strings).
	if cache.Len() > 5 {
		t.Errorf("cache.Len() = %d, want <= 5", cache.Len())
	}
}

func TestCacheMiddleware_RespectCacheControlDisabled(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.RespectCacheControl = false

	var calls int
	// Even with no-store, response should be cached when RespectCacheControl is false.
	backend := testHandler(200, "cached-anyway", &calls, map[string]string{
		"Cache-Control": "no-store",
	})
	cache := NewCacheMiddleware(cfg)
	handler := cache.Wrap(backend)

	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/norespect", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/norespect", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 1 {
		t.Errorf("expected 1 backend call (RespectCacheControl=false), got %d", calls)
	}
	if rr2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("X-Cache = %q, want HIT", rr2.Header().Get("X-Cache"))
	}
}

func TestCacheMiddleware_CachedResponseHeaders(t *testing.T) {
	backend := testHandler(200, "with-headers", nil, map[string]string{
		"Content-Type": "application/json",
		"X-Custom":     "value",
	})
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/headers", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Hit — should return original headers.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/headers", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rr2.Header().Get("Content-Type"))
	}
	if rr2.Header().Get("X-Custom") != "value" {
		t.Errorf("X-Custom = %q, want value", rr2.Header().Get("X-Custom"))
	}
}

func TestCacheMiddleware_RequestNoStoreBypassesCache(t *testing.T) {
	var calls int
	backend := testHandler(200, "body", &calls, nil)
	cache := NewCacheMiddleware(DefaultCacheConfig())
	handler := cache.Wrap(backend)

	// Prime cache.
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/req-nostore", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Request with Cache-Control: no-store should bypass.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/req-nostore", nil)
	req2.Header.Set("Cache-Control", "no-store")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if calls != 2 {
		t.Errorf("expected 2 backend calls (request no-store), got %d", calls)
	}
}

func TestParseCacheControl(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		noCache bool
		noStore bool
		private bool
		maxAge  time.Duration
		sMaxAge time.Duration
	}{
		{
			name: "empty",
		},
		{
			name:    "no-cache",
			value:   "no-cache",
			noCache: true,
		},
		{
			name:    "no-store",
			value:   "no-store",
			noStore: true,
		},
		{
			name:    "private",
			value:   "private",
			private: true,
		},
		{
			name:   "max-age",
			value:  "max-age=300",
			maxAge: 300 * time.Second,
		},
		{
			name:    "s-maxage",
			value:   "s-maxage=60",
			sMaxAge: 60 * time.Second,
		},
		{
			name:    "combined",
			value:   "public, max-age=3600, s-maxage=60",
			maxAge:  3600 * time.Second,
			sMaxAge: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parseCacheControl(tt.value)
			if d.noCache != tt.noCache {
				t.Errorf("noCache = %v, want %v", d.noCache, tt.noCache)
			}
			if d.noStore != tt.noStore {
				t.Errorf("noStore = %v, want %v", d.noStore, tt.noStore)
			}
			if d.private != tt.private {
				t.Errorf("private = %v, want %v", d.private, tt.private)
			}
			if d.maxAge != tt.maxAge {
				t.Errorf("maxAge = %v, want %v", d.maxAge, tt.maxAge)
			}
			if d.sMaxAge != tt.sMaxAge {
				t.Errorf("sMaxAge = %v, want %v", d.sMaxAge, tt.sMaxAge)
			}
		})
	}
}

func TestCacheKey_Deterministic(t *testing.T) {
	req1 := httptest.NewRequest("GET", "http://example.com/path?a=1&b=2", nil)
	req2 := httptest.NewRequest("GET", "http://example.com/path?a=1&b=2", nil)

	k1 := CacheKey(req1)
	k2 := CacheKey(req2)

	if k1 != k2 {
		t.Errorf("identical requests should produce identical keys: %q != %q", k1, k2)
	}

	// Different method should produce different key.
	req3 := httptest.NewRequest("HEAD", "http://example.com/path?a=1&b=2", nil)
	k3 := CacheKey(req3)
	if k1 == k3 {
		t.Error("different methods should produce different keys")
	}
}

func TestDefaultCacheConfig(t *testing.T) {
	cfg := DefaultCacheConfig()

	if cfg.MaxSize != 104857600 {
		t.Errorf("MaxSize = %d, want 104857600", cfg.MaxSize)
	}
	if cfg.MaxEntries != 10000 {
		t.Errorf("MaxEntries = %d, want 10000", cfg.MaxEntries)
	}
	if cfg.DefaultTTL != 5*time.Minute {
		t.Errorf("DefaultTTL = %v, want 5m", cfg.DefaultTTL)
	}
	if cfg.MinResponseSize != 0 {
		t.Errorf("MinResponseSize = %d, want 0", cfg.MinResponseSize)
	}
	if cfg.MaxResponseSize != 10485760 {
		t.Errorf("MaxResponseSize = %d, want 10485760", cfg.MaxResponseSize)
	}
	if !cfg.RespectCacheControl {
		t.Error("RespectCacheControl should default to true")
	}
	if cfg.StaleWhileRevalidate != 30*time.Second {
		t.Errorf("StaleWhileRevalidate = %v, want 30s", cfg.StaleWhileRevalidate)
	}
	if cfg.CacheAuthenticatedRequests {
		t.Error("CacheAuthenticatedRequests should default to false")
	}
	if len(cfg.CacheableMethods) != 2 {
		t.Errorf("CacheableMethods len = %d, want 2", len(cfg.CacheableMethods))
	}
	if len(cfg.CacheableStatuses) != 3 {
		t.Errorf("CacheableStatuses len = %d, want 3", len(cfg.CacheableStatuses))
	}
}

// --- Tests for responseCapturer.WriteHeader ---

func TestResponseCapturer_WriteHeader_OnlyOnce(t *testing.T) {
	// The responseCapturer should only forward the first WriteHeader call.
	inner := httptest.NewRecorder()
	rc := &responseCapturer{
		ResponseWriter: inner,
		body:           &bytes.Buffer{},
		statusCode:     http.StatusOK,
	}

	rc.WriteHeader(http.StatusCreated)
	if rc.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %d, want %d", rc.statusCode, http.StatusCreated)
	}

	// Write body marks wroteBody = true
	rc.Write([]byte("hello"))

	// Second WriteHeader should be ignored (wroteBody is true)
	rc.WriteHeader(http.StatusNotFound)
	if rc.statusCode != http.StatusCreated {
		t.Errorf("statusCode changed to %d after body write, want %d", rc.statusCode, http.StatusCreated)
	}
}

func TestResponseCapturer_Write_SetsWroteBody(t *testing.T) {
	inner := httptest.NewRecorder()
	rc := &responseCapturer{
		ResponseWriter: inner,
		body:           &bytes.Buffer{},
		statusCode:     http.StatusOK,
	}

	if rc.wroteBody {
		t.Error("wroteBody should be false initially")
	}

	rc.Write([]byte("data"))

	if !rc.wroteBody {
		t.Error("wroteBody should be true after Write")
	}
}

// --- Tests for discardResponseWriter ---

func TestDiscardResponseWriter(t *testing.T) {
	d := &discardResponseWriter{header: make(http.Header)}

	// Header should be accessible
	d.Header().Set("X-Test", "value")
	if d.Header().Get("X-Test") != "value" {
		t.Error("Header should be settable")
	}

	// Write should succeed and discard
	n, err := d.Write([]byte("hello"))
	if err != nil {
		t.Errorf("Write error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}

	// WriteHeader should not panic
	d.WriteHeader(http.StatusOK)
}

func TestDiscardResponseWriter_WriteHeader_MultipleStatusCodes(t *testing.T) {
	d := &discardResponseWriter{header: make(http.Header)}

	// WriteHeader should not panic with various status codes
	d.WriteHeader(http.StatusCreated)
	d.WriteHeader(http.StatusNotFound)
	d.WriteHeader(http.StatusInternalServerError)

	// Headers should still be accessible after WriteHeader calls
	d.Header().Set("X-After", "write-header")
	if d.Header().Get("X-After") != "write-header" {
		t.Error("Header should be settable after WriteHeader")
	}
}

func TestDiscardResponseWriter_WriteAfterWriteHeader(t *testing.T) {
	d := &discardResponseWriter{header: make(http.Header)}

	d.WriteHeader(http.StatusOK)

	// Write should still succeed after WriteHeader
	n, err := d.Write([]byte("after header"))
	if err != nil {
		t.Errorf("Write error = %v", err)
	}
	if n != 12 {
		t.Errorf("Write returned %d, want 12", n)
	}
}

func TestDiscardResponseWriter_WriteHeader_Direct(t *testing.T) {
	dw := &discardResponseWriter{header: make(http.Header)}
	dw.WriteHeader(200) // should not panic
	dw.WriteHeader(404) // second call, should not panic
}
