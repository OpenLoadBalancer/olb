// Package middleware provides HTTP middleware infrastructure for OpenLoadBalancer.
package middleware

import (
	"bytes"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// PriorityRetry is the execution priority for retry middleware.
// It should run after routing/auth but before metrics/logging,
// so retries are transparent to outer middleware.
const PriorityRetry = 750

// RetryConfig configures the retry middleware.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (default: 3).
	MaxRetries int

	// RetryOn is the list of HTTP status codes that trigger a retry (default: 502, 503, 504).
	RetryOn []int

	// RetryMethods is the list of HTTP methods eligible for retry (default: GET, HEAD, OPTIONS, PUT, DELETE).
	// Only idempotent methods should be retried by default.
	RetryMethods []string

	// BackoffInitial is the initial backoff delay before the first retry (default: 100ms).
	BackoffInitial time.Duration

	// BackoffMax is the maximum backoff delay (default: 5s).
	BackoffMax time.Duration

	// BackoffMultiplier is the exponential backoff multiplier (default: 2.0).
	BackoffMultiplier float64

	// EnableJitter adds randomness to backoff delays to avoid thundering herd (default: true).
	EnableJitter bool
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		RetryOn:           []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
		RetryMethods:      []string{http.MethodGet, http.MethodHead, http.MethodOptions},
		BackoffInitial:    100 * time.Millisecond,
		BackoffMax:        5 * time.Second,
		BackoffMultiplier: 2.0,
		EnableJitter:      true,
	}
}

// RetryMiddleware implements automatic retry logic for failed upstream requests.
// It buffers responses and only sends them to the client after deciding whether
// to retry, preventing partial response writes on retryable failures.
type RetryMiddleware struct {
	config    RetryConfig
	retryOn   map[int]bool
	methods   map[string]bool
	randMu    sync.Mutex
	rand      *rand.Rand
	sleepFunc func(time.Duration) // injectable for testing
}

// NewRetryMiddleware creates a new retry middleware with the given configuration.
// Zero-value fields in config are replaced with defaults.
func NewRetryMiddleware(config RetryConfig) *RetryMiddleware {
	defaults := DefaultRetryConfig()

	if config.MaxRetries <= 0 {
		config.MaxRetries = defaults.MaxRetries
	}
	if len(config.RetryOn) == 0 {
		config.RetryOn = defaults.RetryOn
	}
	if len(config.RetryMethods) == 0 {
		config.RetryMethods = defaults.RetryMethods
	}
	if config.BackoffInitial <= 0 {
		config.BackoffInitial = defaults.BackoffInitial
	}
	if config.BackoffMax <= 0 {
		config.BackoffMax = defaults.BackoffMax
	}
	if config.BackoffMultiplier <= 0 {
		config.BackoffMultiplier = defaults.BackoffMultiplier
	}
	// EnableJitter defaults to true; since bool zero is false, we check
	// if the entire config was zero-valued by checking MaxRetries before
	// defaulting was applied. The caller must explicitly set EnableJitter=false
	// to disable it. We handle this by always using the provided value —
	// DefaultRetryConfig sets it to true, and callers can override.

	retryOnMap := make(map[int]bool, len(config.RetryOn))
	for _, code := range config.RetryOn {
		retryOnMap[code] = true
	}

	methodsMap := make(map[string]bool, len(config.RetryMethods))
	for _, method := range config.RetryMethods {
		methodsMap[method] = true
	}

	return &RetryMiddleware{
		config:    config,
		retryOn:   retryOnMap,
		methods:   methodsMap,
		rand:      rand.New(rand.NewSource(time.Now().UnixNano())),
		sleepFunc: time.Sleep,
	}
}

// Name returns the middleware name.
func (m *RetryMiddleware) Name() string {
	return "retry"
}

// Priority returns the middleware priority.
func (m *RetryMiddleware) Priority() int {
	return PriorityRetry
}

// Wrap wraps the next handler with retry logic.
func (m *RetryMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the method is retryable
		if !m.isRetryableMethod(r.Method) {
			// Non-retryable method: pass through directly
			next.ServeHTTP(w, r)
			return
		}

		var lastRecorder *bufferedResponseWriter
		attempts := 0

		for attempt := 0; attempt <= m.config.MaxRetries; attempt++ {
			// Wait before retry (not before first attempt)
			if attempt > 0 {
				delay := m.calculateBackoff(attempt)
				m.sleepFunc(delay)
			}

			// Create a buffered response writer to capture the response
			recorder := newBufferedResponseWriter()
			next.ServeHTTP(recorder, r)
			lastRecorder = recorder
			attempts = attempt + 1

			// If the status code is not retryable, stop retrying
			if !m.isRetryableStatus(recorder.statusCode) {
				break
			}

			// If this was the last allowed attempt, stop
			if attempt == m.config.MaxRetries {
				break
			}
		}

		// Write the final response to the real client
		// Set retry count header
		w.Header().Set("X-Retry-Count", strconv.Itoa(attempts-1))

		// Copy headers from the buffered response
		for key, values := range lastRecorder.header {
			for _, v := range values {
				w.Header().Set(key, v)
			}
		}

		w.WriteHeader(lastRecorder.statusCode)
		if lastRecorder.body.Len() > 0 {
			w.Write(lastRecorder.body.Bytes())
		}
	})
}

// isRetryableMethod checks if the HTTP method is eligible for retry.
func (m *RetryMiddleware) isRetryableMethod(method string) bool {
	return m.methods[method]
}

// isRetryableStatus checks if the HTTP status code should trigger a retry.
func (m *RetryMiddleware) isRetryableStatus(status int) bool {
	return m.retryOn[status]
}

// calculateBackoff computes the backoff delay for the given attempt number (1-based).
// Formula: delay = min(initial * multiplier^(attempt-1) + jitter, max)
// Jitter is a random value between 0 and 50% of the calculated delay.
func (m *RetryMiddleware) calculateBackoff(attempt int) time.Duration {
	// Calculate base delay: initial * multiplier^(attempt-1)
	base := float64(m.config.BackoffInitial) * math.Pow(m.config.BackoffMultiplier, float64(attempt-1))

	// Cap at max
	if base > float64(m.config.BackoffMax) {
		base = float64(m.config.BackoffMax)
	}

	delay := time.Duration(base)

	// Add jitter: random value 0-50% of calculated delay
	if m.config.EnableJitter {
		m.randMu.Lock()
		jitter := time.Duration(m.rand.Float64() * 0.5 * float64(delay))
		m.randMu.Unlock()
		delay += jitter

		// Re-cap after jitter
		if delay > m.config.BackoffMax {
			delay = m.config.BackoffMax
		}
	}

	return delay
}

// bufferedResponseWriter captures an HTTP response in memory so we can
// decide whether to retry before committing the response to the client.
type bufferedResponseWriter struct {
	header     http.Header
	body       bytes.Buffer
	statusCode int
	written    bool
}

// pool for recycling bufferedResponseWriter objects.
var bufferedWriterPool = sync.Pool{
	New: func() any {
		return &bufferedResponseWriter{
			header: make(http.Header),
		}
	},
}

// newBufferedResponseWriter creates a new buffered response writer.
func newBufferedResponseWriter() *bufferedResponseWriter {
	bw := bufferedWriterPool.Get().(*bufferedResponseWriter)
	// Reset state
	bw.body.Reset()
	bw.statusCode = http.StatusOK
	bw.written = false
	// Clear old headers
	for k := range bw.header {
		delete(bw.header, k)
	}
	return bw
}

// Header returns the header map.
func (bw *bufferedResponseWriter) Header() http.Header {
	return bw.header
}

// Write buffers the response body.
func (bw *bufferedResponseWriter) Write(b []byte) (int, error) {
	if !bw.written {
		bw.written = true
	}
	return bw.body.Write(b)
}

// WriteHeader captures the status code.
func (bw *bufferedResponseWriter) WriteHeader(statusCode int) {
	bw.statusCode = statusCode
	bw.written = true
}

// release returns the buffered writer to the pool.
func (bw *bufferedResponseWriter) release() {
	bw.body.Reset()
	for k := range bw.header {
		delete(bw.header, k)
	}
	bw.statusCode = http.StatusOK
	bw.written = false
	bufferedWriterPool.Put(bw)
}
