// Package middleware provides HTTP middleware components for OpenLoadBalancer.
package middleware

import (
	"net/http"
	"strings"
	"sync"
)

// Security preset constants.
const (
	SecurityPresetNone   = ""
	SecurityPresetBasic  = "basic"  // X-Content-Type-Options, X-Frame-Options
	SecurityPresetStrict = "strict" // + HSTS, CSP, Referrer-Policy
)

// HeadersConfig configures the Headers middleware.
type HeadersConfig struct {
	RequestAdd     map[string]string // Add request headers
	RequestSet     map[string]string // Set request headers (replace)
	RequestRemove  []string          // Remove request headers
	ResponseAdd    map[string]string // Add response headers
	ResponseSet    map[string]string // Set response headers
	ResponseRemove []string          // Remove response headers
	SecurityPreset string            // "", "basic", "strict", "custom"
}

// HeadersMiddleware modifies request and response headers.
type HeadersMiddleware struct {
	config HeadersConfig
}

// NewHeadersMiddleware creates a new Headers middleware.
func NewHeadersMiddleware(config HeadersConfig) *HeadersMiddleware {
	return &HeadersMiddleware{
		config: config,
	}
}

// Name returns the middleware name.
func (m *HeadersMiddleware) Name() string {
	return "headers"
}

// Priority returns the middleware priority.
func (m *HeadersMiddleware) Priority() int {
	return PriorityHeaders
}

// Wrap wraps the next handler with header manipulation functionality.
func (m *HeadersMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Modify request headers (Remove -> Set -> Add)
		m.modifyRequestHeaders(r)

		// Create a wrapped response writer from pool to capture and modify response headers
		wrapped := headerResponseWriterPool.Get().(*headerResponseWriter)
		wrapped.ResponseWriter = w
		wrapped.middleware = m
		wrapped.written = false

		next.ServeHTTP(wrapped, r)

		// Return to pool (clear middleware ref but keep ResponseWriter
		// for any post-serve Unwrap() callers)
		wrapped.middleware = nil
		wrapped.written = false
		headerResponseWriterPool.Put(wrapped)
	})
}

// modifyRequestHeaders modifies the request headers in order: Remove, Set, Add.
func (m *HeadersMiddleware) modifyRequestHeaders(r *http.Request) {
	// Remove headers first
	for _, header := range m.config.RequestRemove {
		r.Header.Del(header)
	}

	// Set headers (replace existing)
	for header, value := range m.config.RequestSet {
		r.Header.Set(header, value)
	}

	// Add headers
	for header, value := range m.config.RequestAdd {
		r.Header.Add(header, value)
	}
}

// applySecurityPreset applies the configured security preset to response headers.
func (m *HeadersMiddleware) applySecurityPreset(w http.ResponseWriter) {
	switch m.config.SecurityPreset {
	case SecurityPresetBasic:
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

	case SecurityPresetStrict:
		// Basic headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// Additional strict headers
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	}
}

// headerResponseWriterPool pools the small wrapper struct to reduce per-request allocations.
var headerResponseWriterPool = sync.Pool{
	New: func() any {
		return &headerResponseWriter{}
	},
}

// headerResponseWriter wraps http.ResponseWriter to modify response headers.
type headerResponseWriter struct {
	http.ResponseWriter
	middleware *HeadersMiddleware
	written    bool
}

// WriteHeader captures the status and applies header modifications.
func (hw *headerResponseWriter) WriteHeader(status int) {
	if hw.written {
		return
	}
	hw.written = true

	// Apply security preset first (can be overridden by Set/Add)
	hw.middleware.applySecurityPreset(hw.ResponseWriter)

	// Apply response header modifications: Remove -> Set -> Add
	// Remove headers
	for _, header := range hw.middleware.config.ResponseRemove {
		hw.ResponseWriter.Header().Del(header)
	}

	// Set headers (replace existing)
	for header, value := range hw.middleware.config.ResponseSet {
		hw.ResponseWriter.Header().Set(header, value)
	}

	// Add headers
	for header, value := range hw.middleware.config.ResponseAdd {
		hw.ResponseWriter.Header().Add(header, value)
	}

	hw.ResponseWriter.WriteHeader(status)
}

// Write ensures WriteHeader is called before writing.
func (hw *headerResponseWriter) Write(b []byte) (int, error) {
	if !hw.written {
		hw.WriteHeader(http.StatusOK)
	}
	return hw.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter.
func (hw *headerResponseWriter) Unwrap() http.ResponseWriter {
	return hw.ResponseWriter
}

// hasHeader checks if a header exists (case-insensitive).
func hasHeader(headers http.Header, name string) bool {
	_, ok := headers[http.CanonicalHeaderKey(name)]
	return ok
}

// canonicalizeHeaders converts header names to canonical form.
func canonicalizeHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		result[http.CanonicalHeaderKey(k)] = v
	}
	return result
}

// canonicalizeHeaderSlice converts a slice of header names to canonical form.
func canonicalizeHeaderSlice(headers []string) []string {
	result := make([]string, len(headers))
	for i, h := range headers {
		result[i] = http.CanonicalHeaderKey(h)
	}
	return result
}

// containsHeader checks if a header is in a slice (case-insensitive).
func containsHeader(slice []string, target string) bool {
	target = strings.ToLower(target)
	for _, s := range slice {
		if strings.ToLower(s) == target {
			return true
		}
	}
	return false
}
