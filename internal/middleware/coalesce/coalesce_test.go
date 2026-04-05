package coalesce

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// barrier blocks all goroutines until Release is called, ensuring they start together.
type barrier struct {
	wg    sync.WaitGroup
	ready chan struct{}
}

func newBarrier(n int) *barrier {
	b := &barrier{ready: make(chan struct{})}
	b.wg.Add(n)
	return b
}

func (b *barrier) wait()    { b.wg.Done(); <-b.ready }
func (b *barrier) release() { b.wg.Wait(); close(b.ready) }

func TestCoalesce_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestCoalesce_NonGETMethod(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusCreated)
	}))

	// POST should not be coalesced
	req := httptest.NewRequest("POST", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestCoalesce_SingleRequest(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.String() != "response" {
		t.Errorf("Expected body 'response', got '%s'", rec.Body.String())
	}

	if rec.Header().Get("X-Coalesced") != "false" {
		t.Error("Request should not be marked as coalesced")
	}
}

func TestCoalesce_ConcurrentRequests(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 500 * time.Millisecond

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow backend
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))

	// Send 5 concurrent requests
	var wg sync.WaitGroup
	bar := newBarrier(5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bar.wait()
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
			}
		}()
	}
	bar.release()

	wg.Wait()

	// Should only call backend once
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 backend call, got %d", callCount)
	}
}

func TestCoalesce_DifferentKeys(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Different paths should not be coalesced
	paths := []string{"/test1", "/test2", "/test3"}
	for _, path := range paths {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("Expected 3 calls, got %d", callCount)
	}
}

func TestCoalesce_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/api/public"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Excluded path should not be coalesced
	req := httptest.NewRequest("GET", "/api/public/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestCoalesce_MaxRequests(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.MaxRequests = 2

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send 5 concurrent requests (max 2 should coalesce)
	var wg sync.WaitGroup
	bar := newBarrier(5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bar.wait()
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}()
	}
	bar.release()

	wg.Wait()

	// Should have more than 1 call due to max requests limit
	if atomic.LoadInt32(&callCount) < 2 {
		t.Errorf("Expected at least 2 calls due to max requests limit, got %d", callCount)
	}
}

func TestCoalesce_CustomKeyFunc(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.KeyFunc = func(r *http.Request) string {
		// Only coalesce based on path, ignoring query
		return r.URL.Path
	}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // Ensure concurrent requests have time to join
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Different query params should be coalesced with custom key func
	var wg sync.WaitGroup
	urls := []string{"/test?a=1", "/test?a=2", "/test?a=3"}
	bar := newBarrier(len(urls))
	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			bar.wait()
			req := httptest.NewRequest("GET", u, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}(url)
	}
	bar.release()

	wg.Wait()

	// Should only call backend once
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestCoalesce_ResponseHeaders(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))

	var wg sync.WaitGroup
	bar := newBarrier(3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bar.wait()
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Header().Get("X-Custom-Header") != "custom-value" {
				t.Error("Custom header should be preserved")
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Error("Content-Type should be preserved")
			}
		}()
	}
	bar.release()

	wg.Wait()
}

func TestCoalesce_HEADMethod(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Ensure concurrent requests have time to join
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	}))

	// HEAD should be coalesced — use a ready channel to ensure first request
	// registers as inflight before others attempt to join.
	var wg sync.WaitGroup
	started := make(chan struct{})
	bar := newBarrier(3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bar.wait()
			if idx == 0 {
				close(started) // signal first request is in the handler
			} else {
				<-started // wait for first request to be in-flight
			}
			req := httptest.NewRequest("HEAD", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
			}
		}(i)
	}
	bar.release()

	wg.Wait()

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for HEAD, got %d", callCount)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.TTL != 100*time.Millisecond {
		t.Errorf("Default TTL should be 100ms, got %v", config.TTL)
	}
	if config.MaxRequests != 0 {
		t.Errorf("Default MaxRequests should be 0, got %d", config.MaxRequests)
	}
	if config.KeyFunc == nil {
		t.Error("Default KeyFunc should not be nil")
	}
}

func TestDefaultKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/path?query=value", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-None-Match", "abc123")

	key := DefaultKeyFunc(req)

	if key == "" {
		t.Error("Key should not be empty")
	}

	// Should include method and path
	if key[:10] != "GET|/path|" {
		t.Errorf("Key should start with 'GET|/path|', got %s", key[:10])
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 160 {
		t.Errorf("Expected priority 160, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "coalesce" {
		t.Errorf("Expected name 'coalesce', got '%s'", mw.Name())
	}
}

func TestNew_Defaults(t *testing.T) {
	config := Config{
		Enabled: true,
		// Leave KeyFunc and TTL empty
	}

	mw := New(config)

	if mw.config.KeyFunc == nil {
		t.Error("KeyFunc should default to DefaultKeyFunc")
	}
	if mw.config.TTL != 100*time.Millisecond {
		t.Errorf("TTL should default to 100ms, got %v", mw.config.TTL)
	}
}

func TestStats(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	stats := mw.Stats()
	if _, ok := stats["inflight_requests"]; !ok {
		t.Error("Stats should contain inflight_requests")
	}
}

func TestCoalesce_TTLOut(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 50 * time.Millisecond

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Second request should not be coalesced
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 calls after TTL, got %d", callCount)
	}
}
