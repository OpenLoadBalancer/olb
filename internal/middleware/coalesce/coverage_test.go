package coalesce

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCov_Stop verifies the Stop method closes the done channel.
func TestCov_Stop(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	mw := New(config)

	// Stop should close the done channel
	mw.Stop()

	// Calling Stop again should not panic (double-close guard)
	mw.Stop()
}

// TestCov_StopCancelsCleanup verifies that Stop cancels pending cleanup goroutines
// so they do not wait for the full TTL.
func TestCov_StopCancelsCleanup(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 5 * time.Second // long TTL
	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// After serving, a cleanup goroutine is running with a 5s TTL.
	// Stop should cancel it via the done channel.
	start := time.Now()
	mw.Stop()
	elapsed := time.Since(start)

	// Stop itself should be fast (not block for 5s)
	if elapsed > 1*time.Second {
		t.Errorf("Stop took too long: %v — cleanup goroutine may not be respecting done channel", elapsed)
	}

	// Verify inflight map is cleaned up — give a small window for goroutine
	time.Sleep(50 * time.Millisecond)
	stats := mw.Stats()
	if stats["inflight_requests"].(int) != 0 {
		t.Errorf("Expected 0 inflight after stop, got %d", stats["inflight_requests"])
	}
}

// TestCov_DefaultKeyFunc_IfModifiedSince covers the If-Modified-Since header branch.
func TestCov_DefaultKeyFunc_IfModifiedSince(t *testing.T) {
	req := httptest.NewRequest("GET", "/path", nil)
	req.Header.Set("If-Modified-Since", "Wed, 21 Oct 2015 07:28:00 GMT")

	key := DefaultKeyFunc(req)
	if key == "" {
		t.Error("Key should not be empty")
	}

	// Verify the key contains the ims prefix
	found := false
	for _, prefix := range []string{"|ims:"} {
		for i := 0; i+len(prefix) <= len(key); i++ {
			if key[i:i+len(prefix)] == prefix {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("Expected key to contain '|ims:' prefix, got %q", key)
	}
}

// TestCov_DefaultKeyFunc_AllHeaders covers all header branches together.
func TestCov_DefaultKeyFunc_AllHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/path?query=value", nil)
	req.Header.Set("If-None-Match", "etag-val")
	req.Header.Set("If-Modified-Since", "Wed, 21 Oct 2015 07:28:00 GMT")
	req.Header.Set("Accept", "application/json")

	key := DefaultKeyFunc(req)

	// Verify all three header-derived segments are present
	for _, substr := range []string{"|etag:etag-val", "|ims:", "|accept:application/json"} {
		found := false
		for i := 0; i+len(substr) <= len(key); i++ {
			if key[i:i+len(substr)] == substr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Key missing expected substring %q, got %q", substr, key)
		}
	}
}

// TestCov_ExcludePathExactMatch covers the case where the request path exactly
// matches an exclude path (len(r.URL.Path) == len(path)).
func TestCov_ExcludePathExactMatch(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/api/public"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Exact match on excluded path
	req := httptest.NewRequest("GET", "/api/public", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for exact excluded path, got %d", callCount)
	}
}

// TestCov_ExcludePathTrailingSlash covers the case where the exclude path ends
// with a slash (path[len(path)-1] == '/').
func TestCov_ExcludePathTrailingSlash(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/static/"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Path starts with /static/ and exclude ends with /
	req := httptest.NewRequest("GET", "/static/css/main.css", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for trailing-slash excluded path, got %d", callCount)
	}
}

// TestCov_InflightCapacity verifies that when the inflight map is at capacity,
// requests fall through without coalescing.
func TestCov_InflightCapacity(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 2 * time.Second // keep entries alive during test

	mw := New(config)

	// Fill the inflight map to capacity manually
	mw.mu.Lock()
	for i := 0; i < maxInflightEntries; i++ {
		mw.inflight[string(rune(i%128)+'a')+string(rune(i))] = &inflight{
			done:    make(chan struct{}),
			waiters: 0,
		}
	}
	mw.mu.Unlock()

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// This request should pass through without coalescing since map is full
	req := httptest.NewRequest("GET", "/capacity-test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for pass-through, got %d", callCount)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Cleanup: close done channels to allow goroutines to finish
	mw.Stop()
}

// TestCov_ServeFromInflightNilResponse covers the case where the inflight
// request completes but has a nil response (coalescing error).
func TestCov_ServeFromInflightNilResponse(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	// Manually inject an inflight entry with nil response and already closed done channel.
	// We call serveFromInflight directly to exercise the nil-response branch.
	entry := &inflight{
		done:     make(chan struct{}),
		response: nil, // explicitly nil
		waiters:  1,
	}
	close(entry.done)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nil-test", nil)

	// Call serveFromInflight directly to exercise the nil-response branch
	mw.serveFromInflight(rec, req, entry)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for nil response, got %d", rec.Code)
	}
	if rec.Body.String() != "Coalescing error\n" {
		t.Errorf("Expected 'Coalescing error' body, got %q", rec.Body.String())
	}

	mw.Stop()
}

// TestCov_CoalescedHEADNoBody verifies that coalesced HEAD requests do not
// write a body to the waiter.
func TestCov_CoalescedHEADNoBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 500 * time.Millisecond

	mw := New(config)

	unblock := make(chan struct{})
	var wg sync.WaitGroup

	backendCalled := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendCalled, 1)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("should-not-appear-in-head"))
		<-unblock
	}))

	// First request: the actual executor (HEAD) — blocks on unblock channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("HEAD", "/head-test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	// Wait for the first request to register as inflight
	time.Sleep(30 * time.Millisecond)

	// Second request: coalesced waiter (HEAD) — will block on inflight.done
	var waiterWg sync.WaitGroup
	var waiterRec *httptest.ResponseRecorder
	waiterWg.Add(1)
	go func() {
		defer waiterWg.Done()
		req := httptest.NewRequest("HEAD", "/head-test", nil)
		waiterRec = httptest.NewRecorder()
		handler.ServeHTTP(waiterRec, req)
	}()

	// Wait for the waiter to actually join the inflight
	time.Sleep(30 * time.Millisecond)

	// Unblock the first request so both can complete
	close(unblock)

	wg.Wait()
	waiterWg.Wait()

	if atomic.LoadInt32(&backendCalled) != 1 {
		t.Errorf("Expected 1 backend call, got %d", backendCalled)
	}

	if waiterRec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", waiterRec.Code)
	}
	if waiterRec.Body.Len() != 0 {
		t.Errorf("Coalesced HEAD should have empty body, got %q", waiterRec.Body.String())
	}
	if waiterRec.Header().Get("X-Coalesced") != "true" {
		t.Error("Coalesced HEAD should have X-Coalesced: true")
	}

	mw.Stop()
}

// TestCov_CoalescedGETBody verifies that coalesced GET requests receive the body.
func TestCov_CoalescedGETBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 500 * time.Millisecond

	mw := New(config)

	unblock := make(chan struct{})
	var wg sync.WaitGroup

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello-world"))
		<-unblock
	}))

	// First request: executor — blocks on unblock
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/get-body", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	// Wait for inflight entry to be created
	time.Sleep(30 * time.Millisecond)

	// Second request: waiter — should block on inflight.done then get the same body
	var waiterWg sync.WaitGroup
	var waiterRec *httptest.ResponseRecorder
	waiterWg.Add(1)
	go func() {
		defer waiterWg.Done()
		req := httptest.NewRequest("GET", "/get-body", nil)
		waiterRec = httptest.NewRecorder()
		handler.ServeHTTP(waiterRec, req)
	}()

	time.Sleep(30 * time.Millisecond)

	// Let both complete
	close(unblock)

	wg.Wait()
	waiterWg.Wait()

	if waiterRec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", waiterRec.Code)
	}
	if waiterRec.Body.String() != "hello-world" {
		t.Errorf("Expected body 'hello-world', got %q", waiterRec.Body.String())
	}
	if waiterRec.Header().Get("X-Coalesced") != "true" {
		t.Error("Should be marked as coalesced")
	}
	if waiterRec.Header().Get("Content-Type") != "text/plain" {
		t.Error("Content-Type header should be preserved for waiter")
	}

	mw.Stop()
}

// TestCov_MaxRequestsZeroUnlimited verifies that MaxRequests=0 defaults to 5000
// in New() and allows many coalesced waiters.
func TestCov_MaxRequestsZeroUnlimited(t *testing.T) {
	config := Config{
		Enabled:     true,
		TTL:         500 * time.Millisecond,
		MaxRequests: 0, // should default to 5000
	}

	mw := New(config)
	if mw.config.MaxRequests != 5000 {
		t.Errorf("Expected MaxRequests to default to 5000, got %d", mw.config.MaxRequests)
	}

	backendCalled := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&backendCalled, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	// Send many concurrent requests — all should be coalesced
	const n = 20
	var wg sync.WaitGroup
	bar := newBarrier(n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bar.wait()
			req := httptest.NewRequest("GET", "/unlimited", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("Expected 200, got %d", rec.Code)
			}
		}()
	}
	bar.release()
	wg.Wait()

	if atomic.LoadInt32(&backendCalled) != 1 {
		t.Errorf("Expected 1 backend call with unlimited coalescing, got %d", backendCalled)
	}

	mw.Stop()
}

// TestCov_CompletedInflightCreatesNew covers the branch where an existing inflight
// entry has already completed (done channel closed), so a new entry is created.
func TestCov_CompletedInflightCreatesNew(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 500 * time.Millisecond

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// First request completes immediately
	req1 := httptest.NewRequest("GET", "/recreate", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Small sleep to let the inflight entry be marked done but not yet cleaned up
	time.Sleep(10 * time.Millisecond)

	// Second request should find the completed entry and create a new one
	req2 := httptest.NewRequest("GET", "/recreate", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("Expected 2 backend calls (completed inflight forces new), got %d", callCount)
	}

	mw.Stop()
}

// TestCov_StatsDuringInflight verifies Stats returns correct count while requests
// are in-flight.
func TestCov_StatsDuringInflight(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TTL = 500 * time.Millisecond

	mw := New(config)

	unblock := make(chan struct{})
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/stats-test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	// Give the goroutine time to start
	time.Sleep(30 * time.Millisecond)

	stats := mw.Stats()
	count := stats["inflight_requests"].(int)
	if count < 1 {
		t.Errorf("Expected at least 1 inflight request, got %d", count)
	}

	close(unblock)
	wg.Wait()
	mw.Stop()
}
