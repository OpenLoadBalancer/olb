// Package transformer provides response transformation middleware.
package transformer

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/openloadbalancer/olb/internal/security"
)

// Config configures response transformation.
type Config struct {
	Enabled          bool              // Enable transformation
	Compress         bool              // Enable gzip compression
	CompressLevel    int               // Gzip compression level (1-9)
	MinCompressSize  int               // Minimum size to compress (default: 1024)
	AddHeaders       map[string]string // Headers to add to response
	RemoveHeaders    []string          // Headers to remove from response
	RewriteBody      map[string]string // Pattern -> replacement for body rewrite
	JSONTransform    *JSONTransform    // JSON-specific transformations
	ExcludePaths     []string          // Paths to exclude
	ExcludeMIMETypes []string          // MIME types to exclude from transformation
}

// JSONTransform configures JSON-specific transformations.
type JSONTransform struct {
	AddFields    map[string]interface{} // Fields to add to JSON responses
	RemoveFields []string               // Fields to remove from JSON responses
}

// DefaultConfig returns default transformer configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Compress:        false,
		CompressLevel:   6,
		MinCompressSize: 1024,
		AddHeaders:      make(map[string]string),
	}
}

// Middleware provides response transformation.
type Middleware struct {
	config   Config
	patterns map[string]*regexp.Regexp
	pool     sync.Pool
}

// New creates a new transformer middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config:   config,
		patterns: make(map[string]*regexp.Regexp),
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}

	// Compile regex patterns for body rewrite
	for pattern, replacement := range config.RewriteBody {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		m.patterns[pattern] = re
		// Store replacement in a way we can access it later
		_ = replacement
	}

	return m, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "transformer"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 850 // After compression (800), before access log (1000)
}

// Wrap wraps the handler with response transformation.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) && (len(r.URL.Path) == len(path) || r.URL.Path[len(path)] == '/' || path[len(path)-1] == '/') {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Create response writer wrapper
		wrapped := &responseWriter{
			ResponseWriter: w,
			config:         m.config,
			patterns:       m.patterns,
			pool:           &m.pool,
			request:        r,
		}

		next.ServeHTTP(wrapped, r)

		// Apply transformations
		wrapped.applyTransform()
	})
}

// responseWriter wraps http.ResponseWriter to capture response.
type responseWriter struct {
	http.ResponseWriter
	config   Config
	patterns map[string]*regexp.Regexp
	pool     *sync.Pool
	request  *http.Request
	buffer   *bytes.Buffer
	status   int
	written  bool
}

// WriteHeader captures the status code.
func (w *responseWriter) WriteHeader(status int) {
	w.status = status
}

// Write captures the response body.
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.buffer == nil {
		w.buffer = w.pool.Get().(*bytes.Buffer)
		w.buffer.Reset()
	}
	return w.buffer.Write(p)
}

// Header returns the header map.
func (w *responseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// applyTransform applies transformations and writes the response.
func (w *responseWriter) applyTransform() {
	// Check if we should transform this response
	contentType := w.ResponseWriter.Header().Get("Content-Type")
	if w.shouldExcludeMIMEType(contentType) {
		w.writeRaw()
		return
	}

	// Get the body
	var body []byte
	if w.buffer != nil {
		body = w.buffer.Bytes()
		w.pool.Put(w.buffer)
		w.buffer = nil
	}

	// Apply body transformations
	body = w.transformBody(body, contentType)

	// Apply headers
	w.transformHeaders()

	// Apply compression if enabled
	if w.config.Compress && len(body) >= w.config.MinCompressSize {
		body = w.compressBody(body)
	}

	// Write the final response
	if w.status > 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	if len(body) > 0 {
		_, _ = w.ResponseWriter.Write(body)
	}
}

// writeRaw writes the captured response without transformation.
func (w *responseWriter) writeRaw() {
	if w.status > 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	if w.buffer != nil {
		_, _ = w.ResponseWriter.Write(w.buffer.Bytes())
		w.pool.Put(w.buffer)
	}
}

// transformBody applies body transformations.
func (w *responseWriter) transformBody(body []byte, contentType string) []byte {
	if len(body) == 0 {
		return body
	}

	// Apply regex replacements
	for pattern, re := range w.patterns {
		replacement := w.config.RewriteBody[pattern]
		body = re.ReplaceAll(body, []byte(replacement))
	}

	// Apply JSON transformations
	if w.config.JSONTransform != nil && strings.Contains(contentType, "application/json") {
		body = w.transformJSON(body)
	}

	return body
}

// transformJSON applies JSON-specific transformations.
func (w *responseWriter) transformJSON(body []byte) []byte {
	// For now, just return body as-is
	// In production, this would parse JSON, modify, and re-serialize
	return body
}

// transformHeaders applies header transformations.
func (w *responseWriter) transformHeaders() {
	// Remove forbidden headers
	for _, header := range w.config.RemoveHeaders {
		w.ResponseWriter.Header().Del(header)
	}

	// Add new headers (sanitize values to prevent CRLF injection)
	for name, value := range w.config.AddHeaders {
		w.ResponseWriter.Header().Set(name, security.SanitizeHeaderValue(value))
	}
}

// compressBody compresses the body using gzip.
func (w *responseWriter) compressBody(body []byte) []byte {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, w.config.CompressLevel)
	if err != nil {
		return body
	}

	if _, err := gz.Write(body); err != nil {
		return body
	}

	if err := gz.Close(); err != nil {
		return body
	}

	// Only use compressed version if it's smaller
	if buf.Len() < len(body) {
		w.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		w.ResponseWriter.Header().Del("Content-Length")
		return buf.Bytes()
	}

	return body
}

// shouldExcludeMIMEType checks if MIME type should be excluded.
func (w *responseWriter) shouldExcludeMIMEType(contentType string) bool {
	for _, exclude := range w.config.ExcludeMIMETypes {
		if strings.Contains(contentType, exclude) {
			return true
		}
	}
	return false
}
