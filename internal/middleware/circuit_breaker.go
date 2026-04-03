// Package middleware provides HTTP middleware infrastructure for OpenLoadBalancer.
package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed allows requests to pass through normally.
	CircuitClosed CircuitState = iota
	// CircuitOpen blocks all requests and returns 503.
	CircuitOpen
	// CircuitHalfOpen allows a limited number of probe requests.
	CircuitHalfOpen
)

// String returns the string representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// PriorityCircuitBreaker is the priority for the circuit breaker middleware.
// It runs after rate limiting but before routing/proxying.
const PriorityCircuitBreaker = 550

// CircuitBreakerConfig configures the circuit breaker middleware.
type CircuitBreakerConfig struct {
	// ErrorThreshold is the number of errors in the sliding window that
	// triggers the circuit to open. Default: 5.
	ErrorThreshold int

	// ErrorRateThreshold is the percentage of errors (0.0-1.0) that triggers
	// the circuit to open. Only evaluated when there are at least
	// ErrorThreshold requests in the window. Default: 0.5 (50%).
	ErrorRateThreshold float64

	// OpenDuration is how long the circuit stays open before transitioning
	// to half-open. Default: 30s.
	OpenDuration time.Duration

	// HalfOpenMaxRequests is the maximum number of probe requests allowed
	// in the half-open state. Default: 3.
	HalfOpenMaxRequests int

	// WindowSize is the size of the sliding window for tracking errors.
	// The window is time-based. Default: 60s.
	WindowSize time.Duration

	// KeyFunc extracts the circuit breaker key from a request.
	// This determines per-backend isolation. Default: uses request Host header.
	KeyFunc func(r *http.Request) string

	// IsServerError determines whether a response status code should be
	// counted as an error. Default: status >= 500.
	IsServerError func(statusCode int) bool

	// OnStateChange is an optional callback invoked when a circuit changes state.
	OnStateChange func(key string, from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns a CircuitBreakerConfig with sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		ErrorThreshold:      5,
		ErrorRateThreshold:  0.5,
		OpenDuration:        30 * time.Second,
		HalfOpenMaxRequests: 3,
		WindowSize:          60 * time.Second,
	}
}

// circuitEntry tracks the state and error history for a single backend key.
type circuitEntry struct {
	mu sync.Mutex

	state     CircuitState
	openSince time.Time // when the circuit was last opened

	// Sliding window of request outcomes (ring buffer of timestamps).
	// Each entry records the timestamp of an error.
	errors []time.Time

	// Total requests in the current window (errors + successes).
	requests []time.Time

	// Half-open probe tracking.
	halfOpenRequests  int // number of requests let through in half-open
	halfOpenSuccesses int // number of successful probe requests
}

// CircuitBreakerMiddleware implements the circuit breaker pattern per backend.
type CircuitBreakerMiddleware struct {
	config   CircuitBreakerConfig
	circuits sync.Map // map[string]*circuitEntry
}

// NewCircuitBreaker creates a new circuit breaker middleware with the given config.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreakerMiddleware {
	// Apply defaults for zero values.
	if config.ErrorThreshold <= 0 {
		config.ErrorThreshold = 5
	}
	if config.ErrorRateThreshold <= 0 {
		config.ErrorRateThreshold = 0.5
	}
	if config.OpenDuration <= 0 {
		config.OpenDuration = 30 * time.Second
	}
	if config.HalfOpenMaxRequests <= 0 {
		config.HalfOpenMaxRequests = 3
	}
	if config.WindowSize <= 0 {
		config.WindowSize = 60 * time.Second
	}
	if config.KeyFunc == nil {
		config.KeyFunc = func(r *http.Request) string {
			return r.Host
		}
	}
	if config.IsServerError == nil {
		config.IsServerError = func(statusCode int) bool {
			return statusCode >= 500
		}
	}

	return &CircuitBreakerMiddleware{
		config: config,
	}
}

// Name returns the middleware name.
func (cb *CircuitBreakerMiddleware) Name() string {
	return "circuit-breaker"
}

// Priority returns the middleware priority.
func (cb *CircuitBreakerMiddleware) Priority() int {
	return PriorityCircuitBreaker
}

// Wrap wraps the next handler with circuit breaker logic.
func (cb *CircuitBreakerMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := cb.config.KeyFunc(r)
		entry := cb.getOrCreateEntry(key)

		entry.mu.Lock()
		now := time.Now()

		switch entry.state {
		case CircuitOpen:
			// Check if the open duration has elapsed.
			if now.Sub(entry.openSince) >= cb.config.OpenDuration {
				oldState := entry.state
				entry.state = CircuitHalfOpen
				entry.halfOpenRequests = 0
				entry.halfOpenSuccesses = 0
				// Allow this request through as a probe.
				entry.halfOpenRequests++
				entry.mu.Unlock()

				// Fire callback after releasing the lock.
				if cb.config.OnStateChange != nil {
					cb.config.OnStateChange(key, oldState, CircuitHalfOpen)
				}

				cb.serveAndRecord(next, w, r, key, entry)
				return
			}
			// Circuit is still open; reject the request.
			entry.mu.Unlock()
			w.Header().Set("X-Circuit-State", "open")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(cb.config.OpenDuration.Seconds())))
			http.Error(w, "Service Unavailable (circuit breaker open)", http.StatusServiceUnavailable)
			return

		case CircuitHalfOpen:
			// Allow up to HalfOpenMaxRequests probes.
			if entry.halfOpenRequests >= cb.config.HalfOpenMaxRequests {
				// Already at probe limit; reject.
				entry.mu.Unlock()
				w.Header().Set("X-Circuit-State", "half-open")
				http.Error(w, "Service Unavailable (circuit breaker half-open, probe limit reached)", http.StatusServiceUnavailable)
				return
			}
			entry.halfOpenRequests++
			entry.mu.Unlock()
			cb.serveAndRecord(next, w, r, key, entry)
			return

		case CircuitClosed:
			entry.mu.Unlock()
			cb.serveAndRecord(next, w, r, key, entry)
			return
		}

		// Fallback (should not reach here).
		entry.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// serveAndRecord serves the request through the next handler and records the outcome.
func (cb *CircuitBreakerMiddleware) serveAndRecord(next http.Handler, w http.ResponseWriter, r *http.Request, key string, entry *circuitEntry) {
	// Wrap the response writer to capture the status code.
	rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
	next.ServeHTTP(rec, r)

	// Record the outcome.
	isError := cb.config.IsServerError(rec.statusCode)
	now := time.Now()

	var oldState, newState CircuitState
	changed := false

	entry.mu.Lock()

	switch entry.state {
	case CircuitClosed:
		// Record in sliding window.
		entry.requests = append(entry.requests, now)
		if isError {
			entry.errors = append(entry.errors, now)
		}

		// Evict old entries outside the window.
		cb.evictOldEntries(entry, now)

		// Check if the circuit should open.
		errorCount := len(entry.errors)
		totalCount := len(entry.requests)

		if errorCount >= cb.config.ErrorThreshold {
			// Check error rate if there are enough requests.
			errorRate := float64(errorCount) / float64(totalCount)
			if errorRate >= cb.config.ErrorRateThreshold {
				oldState = entry.state
				entry.state = CircuitOpen
				newState = CircuitOpen
				entry.openSince = now
				changed = true
			}
		}

	case CircuitHalfOpen:
		if isError {
			// Probe failed; reopen the circuit.
			oldState = entry.state
			entry.state = CircuitOpen
			newState = CircuitOpen
			entry.openSince = now
			changed = true
		} else {
			entry.halfOpenSuccesses++
			// If all probes succeeded, close the circuit.
			if entry.halfOpenSuccesses >= cb.config.HalfOpenMaxRequests {
				oldState = entry.state
				entry.state = CircuitClosed
				newState = CircuitClosed
				changed = true
				// Reset the sliding window on recovery.
				entry.errors = entry.errors[:0]
				entry.requests = entry.requests[:0]
			}
		}
	}

	entry.mu.Unlock()

	// Fire callback after releasing the lock to avoid deadlocks.
	if changed && cb.config.OnStateChange != nil {
		cb.config.OnStateChange(key, oldState, newState)
	}
}

// evictOldEntries removes entries outside the sliding window.
func (cb *CircuitBreakerMiddleware) evictOldEntries(entry *circuitEntry, now time.Time) {
	cutoff := now.Add(-cb.config.WindowSize)

	// Evict old requests.
	newStart := 0
	for newStart < len(entry.requests) && entry.requests[newStart].Before(cutoff) {
		newStart++
	}
	if newStart > 0 {
		entry.requests = append(entry.requests[:0], entry.requests[newStart:]...)
	}

	// Evict old errors.
	newStart = 0
	for newStart < len(entry.errors) && entry.errors[newStart].Before(cutoff) {
		newStart++
	}
	if newStart > 0 {
		entry.errors = append(entry.errors[:0], entry.errors[newStart:]...)
	}
}

// getOrCreateEntry retrieves or creates a circuit entry for the given key.
func (cb *CircuitBreakerMiddleware) getOrCreateEntry(key string) *circuitEntry {
	if v, ok := cb.circuits.Load(key); ok {
		return v.(*circuitEntry)
	}
	entry := &circuitEntry{
		state:    CircuitClosed,
		errors:   make([]time.Time, 0, 16),
		requests: make([]time.Time, 0, 16),
	}
	actual, _ := cb.circuits.LoadOrStore(key, entry)
	return actual.(*circuitEntry)
}

// State returns the current circuit state for the given key.
// Returns CircuitClosed if no entry exists for the key.
func (cb *CircuitBreakerMiddleware) State(key string) CircuitState {
	v, ok := cb.circuits.Load(key)
	if !ok {
		return CircuitClosed
	}
	entry := v.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Check if an open circuit should transition to half-open.
	if entry.state == CircuitOpen && time.Since(entry.openSince) >= cb.config.OpenDuration {
		return CircuitHalfOpen
	}
	return entry.state
}

// Reset resets the circuit breaker for the given key back to the closed state.
func (cb *CircuitBreakerMiddleware) Reset(key string) {
	cb.circuits.Delete(key)
}

// ResetAll resets all circuit breakers back to the closed state.
func (cb *CircuitBreakerMiddleware) ResetAll() {
	cb.circuits.Range(func(key, _ any) bool {
		cb.circuits.Delete(key)
		return true
	})
}

// Counts returns the current error and total request counts within the sliding
// window for the given key. Returns (0, 0) if no entry exists.
func (cb *CircuitBreakerMiddleware) Counts(key string) (errors, total int) {
	v, ok := cb.circuits.Load(key)
	if !ok {
		return 0, 0
	}
	entry := v.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now()
	cb.evictOldEntries(entry, now)

	return len(entry.errors), len(entry.requests)
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code.
func (sr *statusRecorder) WriteHeader(code int) {
	if sr.wroteHeader {
		return
	}
	sr.statusCode = code
	sr.wroteHeader = true
	sr.ResponseWriter.WriteHeader(code)
}

// Write calls WriteHeader(200) if not already called, then writes.
func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wroteHeader {
		sr.WriteHeader(http.StatusOK)
	}
	return sr.ResponseWriter.Write(b)
}

// Flush implements http.Flusher if the underlying writer supports it.
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Ensure CircuitBreakerMiddleware implements the Middleware interface.
var _ Middleware = (*CircuitBreakerMiddleware)(nil)
