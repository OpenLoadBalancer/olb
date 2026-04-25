// Package coalesce provides request coalescing middleware for preventing cache stampedes.
package coalesce

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Config configures request coalescing.
type Config struct {
	Enabled      bool            // Enable request coalescing
	TTL          time.Duration   // How long to wait for coalescing window
	MaxRequests  int             // Maximum requests to coalesce (0 = unlimited)
	KeyFunc      CoalesceKeyFunc // Function to generate coalesce key
	ExcludePaths []string        // Paths to exclude
}

// CoalesceKeyFunc generates a key for request coalescing.
// Requests with the same key will be coalesced.
type CoalesceKeyFunc func(r *http.Request) string

// DefaultKeyFunc generates a key based on method, URL, and relevant headers.
func DefaultKeyFunc(r *http.Request) string {
	// Key includes method, path, and query string
	key := r.Method + "|" + r.URL.Path + "|" + r.URL.RawQuery

	// Include relevant cache-related headers
	if etag := r.Header.Get("If-None-Match"); etag != "" {
		key += "|etag:" + etag
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		key += "|ims:" + ims
	}
	if accept := r.Header.Get("Accept"); accept != "" {
		key += "|accept:" + accept
	}

	return key
}

// DefaultConfig returns default coalescing configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		TTL:         100 * time.Millisecond,
		MaxRequests: 0,
		KeyFunc:     DefaultKeyFunc,
	}
}

// maxInflightEntries limits the total number of coalesced request entries
// in the inflight map to prevent unbounded memory growth under load.
const maxInflightEntries = 10000

// inflight represents an in-flight request being coalesced.
type inflight struct {
	mu       sync.Mutex
	done     chan struct{}
	response *http.Response
	body     []byte
	err      error
	waiters  int
}

// Middleware provides request coalescing functionality.
type Middleware struct {
	config   Config
	inflight map[string]*inflight
	mu       sync.RWMutex
	done     chan struct{} // signals cleanup goroutines to exit
}

// New creates a new request coalescing middleware.
func New(config Config) *Middleware {
	if config.KeyFunc == nil {
		config.KeyFunc = DefaultKeyFunc
	}
	if config.TTL == 0 {
		config.TTL = 100 * time.Millisecond
	}

	// Cap inflight entries to prevent unbounded map growth
	if config.MaxRequests <= 0 {
		config.MaxRequests = 5000
	}
	return &Middleware{
		config:   config,
		inflight: make(map[string]*inflight),
		done:     make(chan struct{}),
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "coalesce"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 160 // After validator (145), before auth (200)
}

// Wrap wraps the handler with request coalescing.
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

		// Only coalesce safe, cacheable methods
		if r.Method != "GET" && r.Method != "HEAD" {
			next.ServeHTTP(w, r)
			return
		}

		// Generate coalesce key
		key := m.config.KeyFunc(r)

		// Atomically join existing inflight request or create a new one.
		inflight, created := m.getOrCreateInflight(key)
		if !created {
			if inflight == nil {
				// Inflight map at capacity — pass through without coalescing
				next.ServeHTTP(w, r)
				return
			}
			m.serveFromInflight(w, r, inflight)
			return
		}

		// Execute the actual request
		m.executeRequest(w, r, next, inflight, key)
	})
}

// getOrCreateInflight atomically checks for an existing inflight request
// and joins it, or creates a new one if none exists. Returns the inflight
// entry and whether this caller created it.
func (m *Middleware) getOrCreateInflight(key string) (*inflight, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if there's an existing inflight request we can join
	if existing, exists := m.inflight[key]; exists {
		existing.mu.Lock()
		select {
		case <-existing.done:
			// Already completed, create a new one
			existing.mu.Unlock()
		default:
			if m.config.MaxRequests > 0 && existing.waiters >= m.config.MaxRequests {
				existing.mu.Unlock()
			} else {
				existing.waiters++
				existing.mu.Unlock()
				return existing, false
			}
		}
	}

	// Create new inflight entry, unless the map is at capacity
	if len(m.inflight) >= maxInflightEntries {
		return nil, false
	}

	inflight := &inflight{
		done:    make(chan struct{}),
		waiters: 0,
	}
	m.inflight[key] = inflight
	return inflight, true
}

// executeRequest executes the actual request and broadcasts the result.
func (m *Middleware) executeRequest(w http.ResponseWriter, r *http.Request, next http.Handler, inflight *inflight, key string) {
	// Create a response recorder to capture the response
	rec := httptest.NewRecorder()

	// Execute the request
	next.ServeHTTP(rec, r)

	// Capture the response
	inflight.mu.Lock()
	inflight.response = &http.Response{
		StatusCode: rec.Code,
		Header:     rec.Header(),
	}
	inflight.body = rec.Body.Bytes()
	close(inflight.done)
	inflight.mu.Unlock()

	// Clean up after TTL
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[coalesce] panic recovered in cleanup: %v", r)
			}
		}()
		select {
		case <-time.After(m.config.TTL):
		case <-m.done:
		}
		m.mu.Lock()
		delete(m.inflight, key)
		m.mu.Unlock()
	}()

	// Write response to original writer
	m.writeResponse(w, rec)
}

// serveFromInflight serves a response from an existing inflight request.
func (m *Middleware) serveFromInflight(w http.ResponseWriter, r *http.Request, inflight *inflight) {
	// Wait for the inflight request to complete
	<-inflight.done

	inflight.mu.Lock()
	response := inflight.response
	body := inflight.body
	inflight.mu.Unlock()

	if response == nil {
		http.Error(w, "Coalescing error", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Add coalescing header
	w.Header().Set("X-Coalesced", "true")

	// Write status and body
	w.WriteHeader(response.StatusCode)
	if r.Method != "HEAD" {
		_, _ = w.Write(body)
	}
}

// writeResponse writes the recorded response to the writer.
func (m *Middleware) writeResponse(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	// Copy headers
	for key, values := range rec.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Add coalescing header
	w.Header().Set("X-Coalesced", "false")

	// Write status and body
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// Stats returns coalescing statistics.
func (m *Middleware) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"inflight_requests": len(m.inflight),
	}
}

// Stop cancels all pending cleanup goroutines.
func (m *Middleware) Stop() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
}

// responseWriter wraps http.ResponseWriter for body capture.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
	written    bool
}

// newResponseWriter creates a new response writer.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

// WriteHeader captures the status code.
func (rw *responseWriter) WriteHeader(code int) {
	if rw.written {
		return
	}
	rw.statusCode = code
	rw.written = true
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the body.
func (rw *responseWriter) Write(p []byte) (int, error) {
	rw.body.Write(p)
	return rw.ResponseWriter.Write(p)
}

// ReadBody returns the captured body.
func (rw *responseWriter) ReadBody() io.Reader {
	return bytes.NewReader(rw.body.Bytes())
}
