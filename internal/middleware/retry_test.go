package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryMiddleware_SuccessfulRequest_NoRetry(t *testing.T) {
	mw := NewRetryMiddleware(DefaultRetryConfig())

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
	if rec.Body.String() != "success" {
		t.Errorf("Expected body 'success', got '%s'", rec.Body.String())
	}

	retryCount := rec.Header().Get("X-Retry-Count")
	if retryCount != "0" {
		t.Errorf("Expected X-Retry-Count '0', got '%s'", retryCount)
	}
}

func TestRetryMiddleware_RetryOn502(t *testing.T) {
	config := DefaultRetryConfig()
	config.BackoffInitial = time.Millisecond
	config.BackoffMax = 10 * time.Millisecond
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {} // skip real sleep in test

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if callCount != 3 {
		t.Errorf("Expected 3 calls (1 initial + 2 retries), got %d", callCount)
	}
	if rec.Body.String() != "success" {
		t.Errorf("Expected body 'success', got '%s'", rec.Body.String())
	}

	retryCount := rec.Header().Get("X-Retry-Count")
	if retryCount != "2" {
		t.Errorf("Expected X-Retry-Count '2', got '%s'", retryCount)
	}
}

func TestRetryMiddleware_RetryOn503(t *testing.T) {
	config := DefaultRetryConfig()
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}
}

func TestRetryMiddleware_RetryOn504(t *testing.T) {
	config := DefaultRetryConfig()
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}
}

func TestRetryMiddleware_MaxRetriesExhausted(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxRetries = 2
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad gateway"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should return the last failed response
	if rec.Code != http.StatusBadGateway {
		t.Errorf("Expected status %d after max retries, got %d", http.StatusBadGateway, rec.Code)
	}
	// 1 initial + 2 retries = 3 total calls
	if callCount != 3 {
		t.Errorf("Expected 3 calls (1 + 2 retries), got %d", callCount)
	}

	retryCount := rec.Header().Get("X-Retry-Count")
	if retryCount != "2" {
		t.Errorf("Expected X-Retry-Count '2', got '%s'", retryCount)
	}
}

func TestRetryMiddleware_PostNotRetried(t *testing.T) {
	config := DefaultRetryConfig()
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadGateway)
	}))

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// POST is not idempotent, should not retry
	if rec.Code != http.StatusBadGateway {
		t.Errorf("Expected status %d for non-retryable POST, got %d", http.StatusBadGateway, rec.Code)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 call for non-retryable POST, got %d", callCount)
	}

	// X-Retry-Count header should not be set for non-retryable methods
	retryCount := rec.Header().Get("X-Retry-Count")
	if retryCount != "" {
		t.Errorf("Expected no X-Retry-Count header for non-retryable method, got '%s'", retryCount)
	}
}

func TestRetryMiddleware_PatchNotRetried(t *testing.T) {
	config := DefaultRetryConfig()
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	req := httptest.NewRequest(http.MethodPatch, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if callCount != 1 {
		t.Errorf("Expected 1 call for non-retryable PATCH, got %d", callCount)
	}
}

func TestRetryMiddleware_ExponentialBackoffTiming(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        3,
		RetryOn:           []int{http.StatusBadGateway},
		BackoffInitial:    100 * time.Millisecond,
		BackoffMax:        5 * time.Second,
		BackoffMultiplier: 2.0,
		EnableJitter:      false, // disable jitter for deterministic timing
	}
	mw := NewRetryMiddleware(config)

	var delays []time.Duration
	var mu sync.Mutex
	mw.sleepFunc = func(d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if len(delays) != 3 {
		t.Fatalf("Expected 3 backoff delays, got %d", len(delays))
	}

	// Attempt 1: 100ms * 2^0 = 100ms
	if delays[0] != 100*time.Millisecond {
		t.Errorf("First backoff: expected 100ms, got %v", delays[0])
	}
	// Attempt 2: 100ms * 2^1 = 200ms
	if delays[1] != 200*time.Millisecond {
		t.Errorf("Second backoff: expected 200ms, got %v", delays[1])
	}
	// Attempt 3: 100ms * 2^2 = 400ms
	if delays[2] != 400*time.Millisecond {
		t.Errorf("Third backoff: expected 400ms, got %v", delays[2])
	}
}

func TestRetryMiddleware_BackoffMaxCap(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        5,
		RetryOn:           []int{http.StatusBadGateway},
		BackoffInitial:    1 * time.Second,
		BackoffMax:        3 * time.Second,
		BackoffMultiplier: 4.0,
		EnableJitter:      false,
	}
	mw := NewRetryMiddleware(config)

	var delays []time.Duration
	var mu sync.Mutex
	mw.sleepFunc = func(d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for i, d := range delays {
		if d > 3*time.Second {
			t.Errorf("Delay %d exceeded max: got %v, max is 3s", i, d)
		}
	}

	// Attempt 1: 1s * 4^0 = 1s
	if delays[0] != 1*time.Second {
		t.Errorf("First backoff: expected 1s, got %v", delays[0])
	}
	// Attempt 2: 1s * 4^1 = 4s -> capped to 3s
	if delays[1] != 3*time.Second {
		t.Errorf("Second backoff: expected 3s (capped), got %v", delays[1])
	}
}

func TestRetryMiddleware_JitterAddsRandomness(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        1,
		RetryOn:           []int{http.StatusBadGateway},
		BackoffInitial:    100 * time.Millisecond,
		BackoffMax:        5 * time.Second,
		BackoffMultiplier: 2.0,
		EnableJitter:      true,
	}

	// Use a single middleware instance so the internal rand state advances
	mw := NewRetryMiddleware(config)

	// Run multiple requests through the same middleware to collect jitter values
	delaySet := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		var delay time.Duration
		mw.sleepFunc = func(d time.Duration) {
			delay = d
		}

		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		delaySet[delay] = true

		// With jitter, delay should be between 100ms and 150ms (100ms + 0-50% jitter)
		if delay < 100*time.Millisecond || delay > 150*time.Millisecond {
			t.Errorf("Jitter delay %v outside expected range [100ms, 150ms]", delay)
		}
	}

	// With 20 iterations and random jitter from advancing rand state,
	// we should get more than 1 unique delay value
	if len(delaySet) < 2 {
		t.Errorf("Expected jitter to produce varied delays, got %d unique values", len(delaySet))
	}
}

func TestRetryMiddleware_CustomRetryOnStatusCodes(t *testing.T) {
	config := RetryConfig{
		MaxRetries:        2,
		RetryOn:           []int{http.StatusInternalServerError, http.StatusServiceUnavailable},
		BackoffInitial:    time.Millisecond,
		EnableJitter:      false,
		BackoffMultiplier: 1.0,
	}
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}
}

func TestRetryMiddleware_RetryCountHeader(t *testing.T) {
	tests := []struct {
		name          string
		failCount     int
		maxRetries    int
		expectedCount string
	}{
		{"no retries needed", 0, 3, "0"},
		{"one retry", 1, 3, "1"},
		{"two retries", 2, 3, "2"},
		{"three retries exhausted", 4, 3, "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultRetryConfig()
			config.MaxRetries = tt.maxRetries
			mw := NewRetryMiddleware(config)
			mw.sleepFunc = func(d time.Duration) {}

			callCount := 0
			handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if callCount <= tt.failCount {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			retryCount := rec.Header().Get("X-Retry-Count")
			if retryCount != tt.expectedCount {
				t.Errorf("Expected X-Retry-Count '%s', got '%s'", tt.expectedCount, retryCount)
			}
		})
	}
}

func TestRetryMiddleware_NonRetryableStatusPassThrough(t *testing.T) {
	config := DefaultRetryConfig()
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	statusCodes := []int{
		http.StatusBadRequest,          // 400
		http.StatusUnauthorized,        // 401
		http.StatusForbidden,           // 403
		http.StatusNotFound,            // 404
		http.StatusMethodNotAllowed,    // 405
		http.StatusInternalServerError, // 500 (not in default retry list)
	}

	for _, status := range statusCodes {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			callCount := 0
			handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				w.WriteHeader(status)
				w.Write([]byte("error"))
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			callCount = 0
			handler.ServeHTTP(rec, req)

			if rec.Code != status {
				t.Errorf("Expected status %d to pass through, got %d", status, rec.Code)
			}
			if callCount != 1 {
				t.Errorf("Expected 1 call for non-retryable status %d, got %d", status, callCount)
			}
		})
	}
}

func TestRetryMiddleware_DefaultConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries 3, got %d", config.MaxRetries)
	}

	expectedRetryOn := map[int]bool{502: true, 503: true, 504: true}
	if len(config.RetryOn) != 3 {
		t.Fatalf("Expected 3 RetryOn codes, got %d", len(config.RetryOn))
	}
	for _, code := range config.RetryOn {
		if !expectedRetryOn[code] {
			t.Errorf("Unexpected RetryOn code: %d", code)
		}
	}

	expectedMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
	}
	if len(config.RetryMethods) != 3 {
		t.Fatalf("Expected 3 RetryMethods, got %d", len(config.RetryMethods))
	}
	for _, method := range config.RetryMethods {
		if !expectedMethods[method] {
			t.Errorf("Unexpected RetryMethod: %s", method)
		}
	}

	if config.BackoffInitial != 100*time.Millisecond {
		t.Errorf("Expected BackoffInitial 100ms, got %v", config.BackoffInitial)
	}
	if config.BackoffMax != 5*time.Second {
		t.Errorf("Expected BackoffMax 5s, got %v", config.BackoffMax)
	}
	if config.BackoffMultiplier != 2.0 {
		t.Errorf("Expected BackoffMultiplier 2.0, got %f", config.BackoffMultiplier)
	}
	if !config.EnableJitter {
		t.Error("Expected EnableJitter to be true by default")
	}
}

func TestRetryMiddleware_Name(t *testing.T) {
	mw := NewRetryMiddleware(DefaultRetryConfig())
	if mw.Name() != "retry" {
		t.Errorf("Expected name 'retry', got '%s'", mw.Name())
	}
}

func TestRetryMiddleware_Priority(t *testing.T) {
	mw := NewRetryMiddleware(DefaultRetryConfig())
	if mw.Priority() != PriorityRetry {
		t.Errorf("Expected priority %d, got %d", PriorityRetry, mw.Priority())
	}
}

func TestRetryMiddleware_IdempotentMethodsRetried(t *testing.T) {
	// Only truly idempotent methods should be retried by default.
	// PUT and DELETE are excluded to prevent data corruption.
	methods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			config := DefaultRetryConfig()
			config.MaxRetries = 1
			mw := NewRetryMiddleware(config)
			mw.sleepFunc = func(d time.Duration) {}

			callCount := 0
			handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if callCount == 1 {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(method, "/test", nil)
			rec := httptest.NewRecorder()
			callCount = 0
			handler.ServeHTTP(rec, req)

			if callCount != 2 {
				t.Errorf("%s: expected 2 calls (1 + 1 retry), got %d", method, callCount)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("%s: expected status %d after retry, got %d", method, http.StatusOK, rec.Code)
			}
		})
	}
}

func TestRetryMiddleware_ResponseBodyPreserved(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxRetries = 2
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.Header().Set("X-Backend", "failed-"+strconv.Itoa(callCount))
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("service unavailable"))
			return
		}
		w.Header().Set("X-Backend", "success")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("Expected JSON body, got '%s'", rec.Body.String())
	}
	if rec.Header().Get("X-Backend") != "success" {
		t.Errorf("Expected X-Backend 'success', got '%s'", rec.Header().Get("X-Backend"))
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", rec.Header().Get("Content-Type"))
	}
}

func TestRetryMiddleware_ConcurrentAccess(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxRetries = 1
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	var totalCalls int64

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&totalCalls, 1)
		// First call returns 502, retry returns 200
		if r.Header.Get("X-Test-Fail") == "true" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d in concurrent test, got %d", http.StatusOK, rec.Code)
			}
		}()
	}

	wg.Wait()

	// Each request should have been called exactly once (no retries since 200)
	if atomic.LoadInt64(&totalCalls) != int64(numGoroutines) {
		t.Errorf("Expected %d total calls, got %d", numGoroutines, atomic.LoadInt64(&totalCalls))
	}
}

func TestRetryMiddleware_CustomRetryMethods(t *testing.T) {
	// Configure POST as retryable (non-default)
	config := RetryConfig{
		MaxRetries:        1,
		RetryOn:           []int{http.StatusBadGateway},
		RetryMethods:      []string{http.MethodPost, http.MethodGet},
		BackoffInitial:    time.Millisecond,
		BackoffMultiplier: 1.0,
		EnableJitter:      false,
	}
	mw := NewRetryMiddleware(config)
	mw.sleepFunc = func(d time.Duration) {}

	callCount := 0
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// POST should now be retried with custom config
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected POST to be retried with custom config, got status %d", rec.Code)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 calls for retryable POST, got %d", callCount)
	}
}

func TestRetryMiddleware_ZeroValueConfigUsesDefaults(t *testing.T) {
	// All zero values should use defaults
	mw := NewRetryMiddleware(RetryConfig{})

	if mw.config.MaxRetries != 3 {
		t.Errorf("Expected default MaxRetries 3, got %d", mw.config.MaxRetries)
	}
	if mw.config.BackoffInitial != 100*time.Millisecond {
		t.Errorf("Expected default BackoffInitial 100ms, got %v", mw.config.BackoffInitial)
	}
	if mw.config.BackoffMax != 5*time.Second {
		t.Errorf("Expected default BackoffMax 5s, got %v", mw.config.BackoffMax)
	}
	if mw.config.BackoffMultiplier != 2.0 {
		t.Errorf("Expected default BackoffMultiplier 2.0, got %f", mw.config.BackoffMultiplier)
	}
	if len(mw.config.RetryOn) != 3 {
		t.Errorf("Expected 3 default RetryOn codes, got %d", len(mw.config.RetryOn))
	}
	if len(mw.config.RetryMethods) != 3 {
		t.Errorf("Expected 3 default RetryMethods, got %d", len(mw.config.RetryMethods))
	}
}

func TestRetryMiddleware_BufferedResponseWriter(t *testing.T) {
	bw := newBufferedResponseWriter(5 * 1024 * 1024)
	defer bw.release()

	// Test Header
	bw.Header().Set("X-Test", "value")
	if bw.Header().Get("X-Test") != "value" {
		t.Error("Expected header to be set")
	}

	// Test WriteHeader
	bw.WriteHeader(http.StatusCreated)
	if bw.statusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, bw.statusCode)
	}

	// Test Write
	n, err := bw.Write([]byte("hello world"))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if n != 11 {
		t.Errorf("Expected 11 bytes written, got %d", n)
	}
	if bw.body.String() != "hello world" {
		t.Errorf("Expected body 'hello world', got '%s'", bw.body.String())
	}
}
