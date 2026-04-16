// Package requestid provides request ID generation middleware for distributed tracing.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// Config configures request ID middleware.
type Config struct {
	Enabled      bool     // Enable request ID generation
	Header       string   // Header to read/write request ID (default: X-Request-ID)
	Generate     bool     // Generate ID if not present in request
	Length       int      // ID length in bytes (default: 16, results in 32 hex chars)
	Response     bool     // Include ID in response headers
	ExcludePaths []string // Paths to exclude
}

// DefaultConfig returns default request ID configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:  false,
		Header:   "X-Request-ID",
		Generate: true,
		Length:   16,
		Response: true,
	}
}

// contextKey is the key type for request ID in context.
type contextKey struct{}

var requestIDKey = &contextKey{}

// Middleware provides request ID functionality.
type Middleware struct {
	config Config
}

// New creates a new request ID middleware.
func New(config Config) *Middleware {
	if config.Header == "" {
		config.Header = "X-Request-ID"
	}
	if config.Length == 0 {
		config.Length = 16
	}

	return &Middleware{config: config}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "requestid"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 90 // Early in chain, before most processing
}

// Wrap wraps the handler with request ID functionality.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if len(r.URL.Path) >= len(path) && r.URL.Path[:len(path)] == path {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Get or generate request ID
		requestID := r.Header.Get(m.config.Header)
		if requestID == "" && m.config.Generate {
			requestID = generateID(m.config.Length)
		}

		// Add to context if we have an ID
		if requestID != "" {
			r = r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))

			// Set on request header so downstream consumers (WAF, access log) can read it
			r.Header.Set(m.config.Header, requestID)

			// Add to response headers if configured
			if m.config.Response {
				w.Header().Set(m.config.Header, requestID)
			}
		}

		next.ServeHTTP(w, r)
	})
}

// generateID generates a random hex-encoded ID.
func generateID(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// Fallback to simple counter + time if crypto/rand fails
		return fallbackID()
	}
	return hex.EncodeToString(b)
}

// fallbackID returns a simple fallback ID.
func fallbackID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(i * 7) // Simple deterministic pattern
	}
	return hex.EncodeToString(b)
}

// Get extracts the request ID from context.
func Get(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Set adds a request ID to context (useful for testing).
func Set(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}
