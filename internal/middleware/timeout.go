package middleware

import (
	"context"
	"net/http"
	"time"
)

// TimeoutConfig configures the request timeout middleware.
type TimeoutConfig struct {
	// Timeout is the maximum duration for the entire request.
	Timeout time.Duration

	// Message is the response body when a timeout occurs.
	// Defaults to "request timeout".
	Message string
}

// DefaultTimeoutConfig returns sensible defaults.
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		Timeout: 60 * time.Second,
		Message: "request timeout",
	}
}

// TimeoutMiddleware enforces a maximum request processing time.
// If the handler does not complete within the configured duration,
// the client receives a 504 Gateway Timeout response.
type TimeoutMiddleware struct {
	config TimeoutConfig
}

// NewTimeoutMiddleware creates a new timeout middleware.
func NewTimeoutMiddleware(config TimeoutConfig) *TimeoutMiddleware {
	if config.Timeout <= 0 {
		config.Timeout = 60 * time.Second
	}
	if config.Message == "" {
		config.Message = "request timeout"
	}
	return &TimeoutMiddleware{config: config}
}

// Name returns the middleware name.
func (t *TimeoutMiddleware) Name() string { return "timeout" }

// Priority returns the middleware priority (runs early, before most processing).
func (t *TimeoutMiddleware) Priority() int { return PriorityRateLimit - 50 } // 450

// Wrap wraps the handler with timeout enforcement.
func (t *TimeoutMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), t.config.Timeout)
		defer cancel()

		r = r.WithContext(ctx)

		done := make(chan struct{})
		tw := &timeoutWriter{ResponseWriter: w}

		go func() {
			next.ServeHTTP(tw, r)
			close(done)
		}()

		select {
		case <-done:
			// Handler completed normally
		case <-ctx.Done():
			// Timeout reached — only write if handler hasn't written yet
			if !tw.Written() {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusGatewayTimeout)
				w.Write([]byte(t.config.Message))
			}
		}
	})
}

// timeoutWriter tracks whether a response has been written.
type timeoutWriter struct {
	http.ResponseWriter
	written bool
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.written = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.written = true
	return tw.ResponseWriter.Write(b)
}

// Written returns true if the handler has written a response.
func (tw *timeoutWriter) Written() bool {
	return tw.written
}
