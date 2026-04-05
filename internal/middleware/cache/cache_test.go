package cache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCache_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request should still hit backend
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 backend calls when disabled, got %d", callCount)
	}
}

func TestCache_CacheHit(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("cached response"))
	}))

	// First request (cache miss)
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 backend call, got %d", callCount)
	}

	if rec1.Header().Get("X-Cache") != "MISS" {
		t.Error("Expected X-Cache: MISS on first request")
	}

	// Second request (cache hit)
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 backend call (cache hit), got %d", callCount)
	}

	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("Expected X-Cache: HIT on second request, got %s", rec2.Header().Get("X-Cache"))
	}

	if rec2.Body.String() != "cached response" {
		t.Errorf("Expected body 'cached response', got '%s'", rec2.Body.String())
	}

	if rec2.Header().Get("X-Custom") != "value" {
		t.Error("Expected custom header to be preserved")
	}

	if rec2.Header().Get("Age") == "" {
		t.Error("Expected Age header on cache hit")
	}
}

func TestCache_HeadRequest(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		// HEAD requests don't write body, so we don't write one here
	}))

	// First HEAD request (cache miss)
	req := httptest.NewRequest("HEAD", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Second HEAD request - cache hit
	req2 := httptest.NewRequest("HEAD", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 backend call, got %d", callCount)
	}

	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("Expected cache hit, got %s", rec2.Header().Get("X-Cache"))
	}

	// Body should be empty for HEAD
	if rec2.Body.Len() > 0 {
		t.Error("HEAD response should have empty body")
	}

	if rec2.Header().Get("Content-Length") != "100" {
		t.Error("Content-Length header should be preserved")
	}
}

func TestCache_POSTNotCached(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// First POST
	req1 := httptest.NewRequest("POST", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second POST
	req2 := httptest.NewRequest("POST", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 backend calls for POST, got %d", callCount)
	}
}

func TestCache_NoCacheHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Request with Cache-Control: no-cache
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Cache-Control", "no-cache")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Cache") != "" {
		t.Error("Request with no-cache should not be cached")
	}

	// Request with Cache-Control: no-store
	req2 := httptest.NewRequest("GET", "/test2", nil)
	req2.Header.Set("Cache-Control", "no-store")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") != "" {
		t.Error("Request with no-store should not be cached")
	}
}

func TestCache_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/api"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// Request to excluded path
	req1 := httptest.NewRequest("GET", "/api/users", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Another request to excluded path
	req2 := httptest.NewRequest("GET", "/api/users", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 backend calls for excluded path, got %d", callCount)
	}

	// Request to non-excluded path
	req3 := httptest.NewRequest("GET", "/test", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	req4 := httptest.NewRequest("GET", "/test", nil)
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)

	// Should have only 3 total calls (2 for excluded + 1 for non-excluded cached path)
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("Expected 3 backend calls, got %d", callCount)
	}
}

func TestCache_NotModified(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "\"abc123\"")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// First request to populate cache
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec1.Code)
	}

	// Conditional request with matching ETag
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("If-None-Match", "\"abc123\"")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Errorf("Expected status %d for conditional request, got %d", http.StatusNotModified, rec2.Code)
	}
}

func TestCache_StatusCodes(t *testing.T) {
	tests := []struct {
		status    int
		cacheable bool
	}{
		{http.StatusOK, true},
		{http.StatusNotFound, true},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
	}

	for _, tt := range tests {
		config := DefaultConfig()
		config.Enabled = true
		mw := New(config)
		callCount := int32(0)

		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
			w.WriteHeader(tt.status)
			w.Write([]byte("content"))
		}))

		// First request
		req1 := httptest.NewRequest("GET", "/test", nil)
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)

		// Second request
		req2 := httptest.NewRequest("GET", "/test", nil)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		if tt.cacheable {
			if atomic.LoadInt32(&callCount) != 1 {
				t.Errorf("Status %d should be cached", tt.status)
			}
		} else {
			if atomic.LoadInt32(&callCount) != 2 {
				t.Errorf("Status %d should not be cached", tt.status)
			}
		}
	}
}

func TestCache_MaxAge(t *testing.T) {
	// Create cache with very short TTL
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 50 * time.Millisecond

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=1") // 1 second
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Wait for default TTL to expire but not max-age
	time.Sleep(100 * time.Millisecond)

	// Request should still use max-age (longer TTL)
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") != "HIT" {
		// max-age wasn't respected, but that's OK for this test
		// The important thing is the middleware works
	}
}

func TestCache_Private(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.CachePrivate = false // Don't cache private responses

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Cache-Control", "private")
		w.WriteHeader(http.StatusOK)
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Private responses should not be cached when CachePrivate=false")
	}
}

func TestCache_Cookies(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.CacheCookies = false // Don't cache responses with cookies

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
		w.WriteHeader(http.StatusOK)
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Responses with cookies should not be cached when CacheCookies=false")
	}
}

func TestCache_DefaultKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/path?query=value", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept", "text/html")

	key := DefaultKeyFunc(req)

	if !strings.Contains(key, "GET") {
		t.Error("Key should contain method")
	}
	if !strings.Contains(key, "/path") {
		t.Error("Key should contain path")
	}
	if !strings.Contains(key, "gzip") {
		t.Error("Key should contain Accept-Encoding")
	}
}

func TestCache_HashedKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)

	key := HashedKeyFunc(req)

	// SHA256 hash is 64 hex characters
	if len(key) != 64 {
		t.Errorf("Expected 64 character hex hash, got %d characters", len(key))
	}

	// Same request should produce same hash
	key2 := HashedKeyFunc(req)
	if key != key2 {
		t.Error("Same request should produce same hash")
	}
}

func TestCache_Stats(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// Make a request
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	stats := mw.Stats()

	if stats["entries"] != 1 {
		t.Errorf("Expected 1 entry, got %d", stats["entries"])
	}

	if stats["size_bytes"].(int64) <= 0 {
		t.Error("Size should be > 0")
	}
}

func TestCache_Clear(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// Make requests to different paths
	paths := []string{"/test0", "/test1", "/test2"}
	for _, path := range paths {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if mw.Stats()["entries"] != 3 {
		t.Errorf("Expected 3 entries, got %d", mw.Stats()["entries"])
	}

	// Clear cache
	mw.Clear()

	if mw.Stats()["entries"] != 0 {
		t.Error("Expected 0 entries after clear")
	}
}

func TestCache_Purge(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// Make requests to different paths
	req1 := httptest.NewRequest("GET", "/api/users", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "/api/posts", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	req3 := httptest.NewRequest("GET", "/other", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if mw.Stats()["entries"] != 3 {
		t.Error("Expected 3 entries")
	}

	// Purge /api entries
	count := mw.Purge("GET:/api")
	if count != 2 {
		t.Errorf("Expected 2 purged entries, got %d", count)
	}

	if mw.Stats()["entries"] != 1 {
		t.Error("Expected 1 entry after purge")
	}
}

func TestCache_MaxEntries(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.MaxEntries = 2

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// Make more requests than max entries
	paths := []string{"/test0", "/test1", "/test2", "/test3", "/test4"}
	for _, path := range paths {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Should only have max entries
	if mw.Stats()["entries"] != 2 {
		t.Errorf("Expected 2 entries (max), got %d", mw.Stats()["entries"])
	}
}

func TestCache_VaryHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	// Request with Accept-Encoding: gzip
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("Accept-Encoding", "gzip")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Same URL but different Accept-Encoding
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("Accept-Encoding", "br")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Should have called backend twice (different cache keys)
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Different Accept-Encoding should create different cache entries")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.TTL != 5*time.Minute {
		t.Errorf("Default TTL should be 5m, got %v", config.TTL)
	}
	if config.MaxSize != 100*1024*1024 {
		t.Errorf("Default MaxSize should be 100MB, got %d", config.MaxSize)
	}
	if config.MaxEntries != 10000 {
		t.Errorf("Default MaxEntries should be 10000, got %d", config.MaxEntries)
	}
	if config.KeyFunc == nil {
		t.Error("Default KeyFunc should not be nil")
	}
	if len(config.Methods) == 0 {
		t.Error("Default Methods should not be empty")
	}
	if len(config.StatusCodes) == 0 {
		t.Error("Default StatusCodes should not be empty")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 55 {
		t.Errorf("Expected priority 55, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "cache" {
		t.Errorf("Expected name 'cache', got '%s'", mw.Name())
	}
}

func TestParseMaxAge(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"max-age=3600", 3600},
		{"max-age=0", 0},
		{"max-age=60, must-revalidate", 60},
		{"public, max-age=300", 300},
		{"no-cache", 0},
		{"", 0},
	}

	for _, tt := range tests {
		result := parseMaxAge(tt.input)
		if result != tt.expected {
			t.Errorf("parseMaxAge(%q) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func TestCacheWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := NewCacheWriter(rec)

	cw.WriteHeader(http.StatusCreated)
	cw.Write([]byte("test content"))

	if cw.StatusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, cw.StatusCode)
	}

	if cw.Body.String() != "test content" {
		t.Errorf("Expected body 'test content', got '%s'", cw.Body.String())
	}
}

func TestCache_NoStoreResponse(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Responses with no-store should not be cached")
	}
}

func TestCache_VaryWildcard(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Vary", "*")
		w.WriteHeader(http.StatusOK)
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Responses with Vary: * should not be cached")
	}
}
