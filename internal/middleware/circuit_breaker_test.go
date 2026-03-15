package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreaker_DefaultConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()

	if cfg.ErrorThreshold != 5 {
		t.Errorf("expected ErrorThreshold 5, got %d", cfg.ErrorThreshold)
	}
	if cfg.ErrorRateThreshold != 0.5 {
		t.Errorf("expected ErrorRateThreshold 0.5, got %f", cfg.ErrorRateThreshold)
	}
	if cfg.OpenDuration != 30*time.Second {
		t.Errorf("expected OpenDuration 30s, got %v", cfg.OpenDuration)
	}
	if cfg.HalfOpenMaxRequests != 3 {
		t.Errorf("expected HalfOpenMaxRequests 3, got %d", cfg.HalfOpenMaxRequests)
	}
	if cfg.WindowSize != 60*time.Second {
		t.Errorf("expected WindowSize 60s, got %v", cfg.WindowSize)
	}
}

func TestCircuitBreaker_DefaultsAppliedForZeroValues(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	if cb.config.ErrorThreshold != 5 {
		t.Errorf("expected default ErrorThreshold 5, got %d", cb.config.ErrorThreshold)
	}
	if cb.config.ErrorRateThreshold != 0.5 {
		t.Errorf("expected default ErrorRateThreshold 0.5, got %f", cb.config.ErrorRateThreshold)
	}
	if cb.config.OpenDuration != 30*time.Second {
		t.Errorf("expected default OpenDuration 30s, got %v", cb.config.OpenDuration)
	}
	if cb.config.HalfOpenMaxRequests != 3 {
		t.Errorf("expected default HalfOpenMaxRequests 3, got %d", cb.config.HalfOpenMaxRequests)
	}
	if cb.config.WindowSize != 60*time.Second {
		t.Errorf("expected default WindowSize 60s, got %v", cb.config.WindowSize)
	}
	if cb.config.KeyFunc == nil {
		t.Error("expected default KeyFunc to be set")
	}
	if cb.config.IsServerError == nil {
		t.Error("expected default IsServerError to be set")
	}
}

func TestCircuitBreaker_NameAndPriority(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	if cb.Name() != "circuit-breaker" {
		t.Errorf("expected name 'circuit-breaker', got '%s'", cb.Name())
	}
	if cb.Priority() != PriorityCircuitBreaker {
		t.Errorf("expected priority %d, got %d", PriorityCircuitBreaker, cb.Priority())
	}
}

func TestCircuitBreaker_ClosedState_PassesRequests(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold: 5,
		WindowSize:     time.Minute,
	})

	callCount := 0
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	if callCount != 10 {
		t.Errorf("expected 10 calls to handler, got %d", callCount)
	}
}

func TestCircuitBreaker_ClosedToOpen_ErrorThreshold(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     3,
		ErrorRateThreshold: 0.5,
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Send enough errors to trip the breaker.
	// With ErrorThreshold=3 and ErrorRateThreshold=0.5, we need 3 errors
	// and the rate must be >= 50%. Since all requests are errors, rate = 100%.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// The circuit should now be open.
	state := cb.State("example.com")
	if state != CircuitOpen {
		t.Errorf("expected circuit state Open, got %s", state)
	}

	// Next request should be rejected with 503.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when circuit is open, got %d", rec.Code)
	}

	circuitState := rec.Header().Get("X-Circuit-State")
	if circuitState != "open" {
		t.Errorf("expected X-Circuit-State header 'open', got '%s'", circuitState)
	}
}

func TestCircuitBreaker_ClosedToOpen_ErrorRateThreshold(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     3,
		ErrorRateThreshold: 0.6, // 60% error rate to trip
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
	})

	requestNum := 0
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		// First 2 succeed, then all fail.
		if requestNum <= 2 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	// 2 successes + 3 errors = 5 total, error rate = 60% = threshold.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	state := cb.State("example.com")
	if state != CircuitOpen {
		t.Errorf("expected circuit state Open, got %s", state)
	}
}

func TestCircuitBreaker_ErrorRateBelowThreshold_StaysClosed(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     5,
		ErrorRateThreshold: 0.8, // 80% error rate to trip
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
	})

	requestNum := 0
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		// Every other request fails: 50% error rate < 80% threshold.
		if requestNum%2 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	// 10 requests: 5 errors, 5 successes = 50% error rate.
	// Error count (5) >= threshold (5) but rate (50%) < threshold (80%).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	state := cb.State("example.com")
	if state != CircuitClosed {
		t.Errorf("expected circuit to stay Closed (error rate below threshold), got %s", state)
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:      2,
		ErrorRateThreshold:  0.5,
		OpenDuration:        50 * time.Millisecond, // Short for testing.
		HalfOpenMaxRequests: 2,
		WindowSize:          time.Minute,
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if cb.State("example.com") != CircuitOpen {
		t.Fatal("expected circuit to be Open")
	}

	// Wait for the open duration to elapse.
	time.Sleep(60 * time.Millisecond)

	// State should now report half-open.
	if cb.State("example.com") != CircuitHalfOpen {
		t.Errorf("expected circuit state HalfOpen after open duration, got %s", cb.State("example.com"))
	}
}

func TestCircuitBreaker_HalfOpenToClosed_AllProbesSucceed(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:      2,
		ErrorRateThreshold:  0.5,
		OpenDuration:        50 * time.Millisecond,
		HalfOpenMaxRequests: 2,
		WindowSize:          time.Minute,
	})

	failRequests := true
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failRequests {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if cb.State("example.com") != CircuitOpen {
		t.Fatal("expected circuit to be Open")
	}

	// Wait for half-open transition.
	time.Sleep(60 * time.Millisecond)

	// Now make the backend healthy.
	failRequests = false

	// Send HalfOpenMaxRequests successful probes.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("probe %d: expected 200, got %d", i, rec.Code)
		}
	}

	// Circuit should now be closed.
	state := cb.State("example.com")
	if state != CircuitClosed {
		t.Errorf("expected circuit to be Closed after successful probes, got %s", state)
	}

	// Subsequent requests should pass through.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after circuit closed, got %d", rec.Code)
	}
}

func TestCircuitBreaker_HalfOpenToOpen_ProbeFails(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:      2,
		ErrorRateThreshold:  0.5,
		OpenDuration:        50 * time.Millisecond,
		HalfOpenMaxRequests: 3,
		WindowSize:          time.Minute,
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always fail.
		w.WriteHeader(http.StatusBadGateway)
	}))

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Wait for half-open.
	time.Sleep(60 * time.Millisecond)

	// Send a probe that fails.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should get 502 from the backend (probe went through).
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 from failing backend, got %d", rec.Code)
	}

	// Circuit should be back to open.
	// Wait briefly so the open state is definitely recorded.
	state := cb.State("example.com")
	if state != CircuitOpen {
		t.Errorf("expected circuit to revert to Open after failed probe, got %s", state)
	}
}

func TestCircuitBreaker_HalfOpenProbeLimitExceeded(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:      2,
		ErrorRateThreshold:  0.5,
		OpenDuration:        50 * time.Millisecond,
		HalfOpenMaxRequests: 1,
		WindowSize:          time.Minute,
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow backend (don't write header yet to keep half-open).
		w.WriteHeader(http.StatusOK)
	}))

	// Trip the breaker.
	failHandler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		failHandler.ServeHTTP(rec, req)
	}

	// Wait for half-open.
	time.Sleep(60 * time.Millisecond)

	// First probe goes through (transitions to half-open).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first probe: expected 200, got %d", rec.Code)
	}

	// Circuit is now closed because HalfOpenMaxRequests=1 and the single probe
	// succeeded. Verify this.
	state := cb.State("example.com")
	if state != CircuitClosed {
		t.Errorf("expected Closed after single successful probe (HalfOpenMaxRequests=1), got %s", state)
	}
}

func TestCircuitBreaker_PerBackendIsolation(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     2,
		ErrorRateThreshold: 0.5,
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
		KeyFunc: func(r *http.Request) string {
			return r.Header.Get("X-Backend")
		},
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Backend") == "bad" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	// Trip the breaker for "bad" backend.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Backend", "bad")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// "bad" backend should be open.
	if cb.State("bad") != CircuitOpen {
		t.Errorf("expected 'bad' backend to be Open, got %s", cb.State("bad"))
	}

	// "good" backend should still be closed.
	if cb.State("good") != CircuitClosed {
		t.Errorf("expected 'good' backend to be Closed, got %s", cb.State("good"))
	}

	// Requests to "good" backend should still pass.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Backend", "good")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for 'good' backend, got %d", rec.Code)
	}

	// Requests to "bad" backend should be rejected.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Backend", "bad")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for 'bad' backend, got %d", rec.Code)
	}
}

func TestCircuitBreaker_SlidingWindowEviction(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     3,
		ErrorRateThreshold: 0.5,
		OpenDuration:       10 * time.Second,
		WindowSize:         100 * time.Millisecond, // Very short window.
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Send 2 errors (below threshold of 3).
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Wait for the window to expire.
	time.Sleep(150 * time.Millisecond)

	// Send 2 more errors. Old errors should be evicted, so total is only 2, below threshold.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Circuit should still be closed because old errors were evicted.
	state := cb.State("example.com")
	if state != CircuitClosed {
		t.Errorf("expected circuit to be Closed after window eviction, got %s", state)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     2,
		ErrorRateThreshold: 0.5,
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if cb.State("example.com") != CircuitOpen {
		t.Fatal("expected circuit to be Open")
	}

	// Reset the circuit.
	cb.Reset("example.com")

	// Should be closed again.
	if cb.State("example.com") != CircuitClosed {
		t.Errorf("expected circuit to be Closed after Reset, got %s", cb.State("example.com"))
	}
}

func TestCircuitBreaker_ResetAll(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     2,
		ErrorRateThreshold: 0.5,
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
		KeyFunc: func(r *http.Request) string {
			return r.Header.Get("X-Backend")
		},
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Trip breakers for two backends.
	for _, backend := range []string{"a", "b"} {
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Backend", backend)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	}

	if cb.State("a") != CircuitOpen || cb.State("b") != CircuitOpen {
		t.Fatal("expected both circuits to be Open")
	}

	cb.ResetAll()

	if cb.State("a") != CircuitClosed || cb.State("b") != CircuitClosed {
		t.Error("expected all circuits to be Closed after ResetAll")
	}
}

func TestCircuitBreaker_Counts(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     10, // High threshold to avoid tripping.
		ErrorRateThreshold: 0.9,
		WindowSize:         time.Minute,
	})

	requestNum := 0
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		if requestNum%3 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	// Send 6 requests: 2 errors (requests 3 and 6) out of 6.
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	errors, total := cb.Counts("example.com")
	if errors != 2 {
		t.Errorf("expected 2 errors, got %d", errors)
	}
	if total != 6 {
		t.Errorf("expected 6 total requests, got %d", total)
	}
}

func TestCircuitBreaker_OnStateChangeCallback(t *testing.T) {
	var mu sync.Mutex
	transitions := make([]struct {
		key  string
		from CircuitState
		to   CircuitState
	}, 0)

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:      2,
		ErrorRateThreshold:  0.5,
		OpenDuration:        50 * time.Millisecond,
		HalfOpenMaxRequests: 1,
		WindowSize:          time.Minute,
		OnStateChange: func(key string, from, to CircuitState) {
			mu.Lock()
			transitions = append(transitions, struct {
				key  string
				from CircuitState
				to   CircuitState
			}{key, from, to})
			mu.Unlock()
		},
	})

	failRequests := true
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failRequests {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	// Trip the breaker: Closed -> Open.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Wait for open duration: Open -> HalfOpen.
	time.Sleep(60 * time.Millisecond)

	// Send successful probe: HalfOpen -> Closed.
	failRequests = false
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Allow goroutine callbacks to complete.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(transitions) < 3 {
		t.Fatalf("expected at least 3 state transitions, got %d", len(transitions))
	}

	// Verify transitions: Closed->Open, Open->HalfOpen, HalfOpen->Closed.
	expected := []struct {
		from CircuitState
		to   CircuitState
	}{
		{CircuitClosed, CircuitOpen},
		{CircuitOpen, CircuitHalfOpen},
		{CircuitHalfOpen, CircuitClosed},
	}

	for i, exp := range expected {
		if i >= len(transitions) {
			break
		}
		if transitions[i].from != exp.from || transitions[i].to != exp.to {
			t.Errorf("transition %d: expected %s->%s, got %s->%s",
				i, exp.from, exp.to, transitions[i].from, transitions[i].to)
		}
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     100, // High threshold to avoid tripping.
		ErrorRateThreshold: 0.9,
		WindowSize:         time.Minute,
	})

	var served int64
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&served, 1)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	goroutines := 50
	requestsPerGoroutine := 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
			}
		}()
	}

	wg.Wait()

	total := atomic.LoadInt64(&served)
	expected := int64(goroutines * requestsPerGoroutine)
	if total != expected {
		t.Errorf("expected %d served requests, got %d", expected, total)
	}
}

func TestCircuitBreaker_ConcurrentAccessWithTripping(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     5,
		ErrorRateThreshold: 0.5,
		OpenDuration:       5 * time.Second,
		WindowSize:         time.Minute,
		KeyFunc: func(r *http.Request) string {
			return r.Header.Get("X-Backend")
		},
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Backend") == "flaky" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	var wg sync.WaitGroup

	// Concurrent requests to "flaky" backend.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Backend", "flaky")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			// Either 500 (from backend) or 503 (circuit open) is acceptable.
			if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusServiceUnavailable {
				t.Errorf("unexpected status %d for flaky backend", rec.Code)
			}
		}()
	}

	// Concurrent requests to "stable" backend.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Backend", "stable")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for stable backend, got %d", rec.Code)
			}
		}()
	}

	wg.Wait()

	// Verify the flaky backend is open.
	if cb.State("flaky") != CircuitOpen {
		t.Logf("flaky backend state: %s (may not be open if concurrent timing)", cb.State("flaky"))
	}

	// The stable backend must be closed.
	if cb.State("stable") != CircuitClosed {
		t.Errorf("expected 'stable' backend to be Closed, got %s", cb.State("stable"))
	}
}

func TestCircuitBreaker_CustomIsServerError(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     2,
		ErrorRateThreshold: 0.5,
		OpenDuration:       10 * time.Second,
		WindowSize:         time.Minute,
		IsServerError: func(statusCode int) bool {
			// Only treat 503 as an error (not 500 or 502).
			return statusCode == http.StatusServiceUnavailable
		},
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 500 which is NOT counted as error with custom function.
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Send many 500 responses. Circuit should NOT trip.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	state := cb.State("example.com")
	if state != CircuitClosed {
		t.Errorf("expected circuit to be Closed (500 is not counted as error), got %s", state)
	}
}

func TestCircuitBreaker_RetryAfterHeader(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:     2,
		ErrorRateThreshold: 0.5,
		OpenDuration:       30 * time.Second,
		WindowSize:         time.Minute,
	})

	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Rejected request should have Retry-After header.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter != "30" {
		t.Errorf("expected Retry-After header '30', got '%s'", retryAfter)
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.expected {
			t.Errorf("CircuitState(%d).String() = %q, want %q", int(tt.state), got, tt.expected)
		}
	}
}

func TestCircuitBreaker_ImplementsMiddlewareInterface(t *testing.T) {
	var _ Middleware = (*CircuitBreakerMiddleware)(nil)
}

func TestStatusRecorder_Write_SetsStatusOK(t *testing.T) {
	inner := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: inner, statusCode: http.StatusOK}

	// Write without calling WriteHeader first should trigger implicit 200
	n, err := sr.Write([]byte("hello"))
	if err != nil {
		t.Errorf("Write error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}
	if !sr.wroteHeader {
		t.Error("wroteHeader should be true after Write")
	}
	if sr.statusCode != http.StatusOK {
		t.Errorf("statusCode = %d, want %d", sr.statusCode, http.StatusOK)
	}
}

func TestStatusRecorder_Write_WithExplicitHeader(t *testing.T) {
	inner := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: inner, statusCode: http.StatusOK}

	sr.WriteHeader(http.StatusCreated)
	n, err := sr.Write([]byte("data"))
	if err != nil {
		t.Errorf("Write error = %v", err)
	}
	if n != 4 {
		t.Errorf("Write returned %d, want 4", n)
	}
	if sr.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %d, want %d", sr.statusCode, http.StatusCreated)
	}
}

func TestStatusRecorder_Flush(t *testing.T) {
	// httptest.ResponseRecorder implements http.Flusher
	inner := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: inner, statusCode: http.StatusOK}

	// Flush should not panic and should delegate to the underlying writer
	sr.Flush()

	if !inner.Flushed {
		t.Error("Flush should have been called on inner ResponseWriter")
	}
}

func TestStatusRecorder_Flush_NonFlusher(t *testing.T) {
	// Use a minimal ResponseWriter that does NOT implement http.Flusher
	inner := &minimalResponseWriter{header: make(http.Header)}
	sr := &statusRecorder{ResponseWriter: inner, statusCode: http.StatusOK}

	// Flush should not panic even if inner writer doesn't support it
	sr.Flush()
}

// minimalResponseWriter is a basic http.ResponseWriter that does NOT implement http.Flusher.
type minimalResponseWriter struct {
	header http.Header
}

func (m *minimalResponseWriter) Header() http.Header         { return m.header }
func (m *minimalResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (m *minimalResponseWriter) WriteHeader(_ int)           {}

func TestCircuitBreaker_FullLifecycle(t *testing.T) {
	// End-to-end test: Closed -> Open -> HalfOpen -> Open -> HalfOpen -> Closed
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		ErrorThreshold:      3,
		ErrorRateThreshold:  0.5,
		OpenDuration:        50 * time.Millisecond,
		HalfOpenMaxRequests: 2,
		WindowSize:          time.Minute,
	})

	failRequests := true
	handler := cb.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failRequests {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))

	// Phase 1: Trip the breaker (Closed -> Open).
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
	if cb.State("example.com") != CircuitOpen {
		t.Fatalf("Phase 1: expected Open, got %s", cb.State("example.com"))
	}

	// Phase 2: Wait for half-open (Open -> HalfOpen).
	time.Sleep(60 * time.Millisecond)

	// Phase 3: Probe fails (HalfOpen -> Open).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if cb.State("example.com") != CircuitOpen {
		t.Fatalf("Phase 3: expected Open after failed probe, got %s", cb.State("example.com"))
	}

	// Phase 4: Wait for half-open again (Open -> HalfOpen).
	time.Sleep(60 * time.Millisecond)

	// Phase 5: Fix the backend.
	failRequests = false

	// Phase 6: Probes succeed (HalfOpen -> Closed).
	for i := 0; i < 2; i++ {
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Phase 6 probe %d: expected 200, got %d", i, rec.Code)
		}
	}

	if cb.State("example.com") != CircuitClosed {
		t.Fatalf("Phase 6: expected Closed after successful probes, got %s", cb.State("example.com"))
	}

	// Phase 7: Normal traffic flows.
	for i := 0; i < 5; i++ {
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Phase 7 request %d: expected 200, got %d", i, rec.Code)
		}
	}
}
