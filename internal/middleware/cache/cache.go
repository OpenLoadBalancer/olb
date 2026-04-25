// Package cache provides HTTP response caching middleware.
package cache

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures HTTP response caching.
type Config struct {
	Enabled        bool              // Enable caching
	TTL            time.Duration     // Default cache TTL
	MaxSize        int64             // Max cache size in bytes (0 = unlimited)
	MaxEntries     int               // Max number of cached entries (0 = unlimited)
	KeyFunc        CacheKeyFunc      // Function to generate cache keys
	Methods        []string          // HTTP methods to cache (default: GET, HEAD)
	StatusCodes    []int             // Status codes to cache (default: 200, 203, 204, 206, 300, 301, 404, 405, 410, 414, 501)
	VaryHeaders    []string          // Headers to vary cache on
	ExcludePaths   []string          // Paths to exclude from caching
	ExcludeHeaders map[string]string // Response headers that prevent caching (header -> pattern)
	CachePrivate   bool              // Cache responses with Cache-Control: private
	CacheCookies   bool              // Cache responses with cookies
}

// CacheKeyFunc generates a cache key from a request.
type CacheKeyFunc func(r *http.Request) string

// DefaultKeyFunc generates a key based on method, URL, and Vary headers.
func DefaultKeyFunc(r *http.Request) string {
	key := r.Method + ":" + r.URL.String()

	// Include Accept-Encoding for compression variations
	if enc := r.Header.Get("Accept-Encoding"); enc != "" {
		key += ":" + enc
	}

	// Include Accept header for content negotiation
	if accept := r.Header.Get("Accept"); accept != "" {
		key += ":" + accept
	}

	return key
}

// HashedKeyFunc generates a SHA256 hash of the key (for shorter keys).
func HashedKeyFunc(r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(DefaultKeyFunc(r)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// DefaultConfig returns default cache configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:    false,
		TTL:        5 * time.Minute,
		MaxSize:    100 * 1024 * 1024, // 100 MB
		MaxEntries: 10000,
		KeyFunc:    DefaultKeyFunc,
		Methods:    []string{"GET", "HEAD"},
		StatusCodes: []int{
			http.StatusOK,                   // 200
			http.StatusNonAuthoritativeInfo, // 203
			http.StatusNoContent,            // 204
			http.StatusPartialContent,       // 206
			http.StatusMultipleChoices,      // 300
			http.StatusMovedPermanently,     // 301
			http.StatusNotFound,             // 404
			http.StatusMethodNotAllowed,     // 405
			http.StatusGone,                 // 410
			http.StatusRequestURITooLong,    // 414
			http.StatusNotImplemented,       // 501
		},
		VaryHeaders:    []string{"Accept", "Accept-Encoding", "Accept-Language"},
		ExcludeHeaders: make(map[string]string),
		CachePrivate:   false,
		CacheCookies:   false,
	}
}

// cachedResponse represents a cached HTTP response.
type cachedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Timestamp  time.Time
	TTL        time.Duration
	ETag       string
}

// isExpired checks if the cached response has expired.
func (c *cachedResponse) isExpired() bool {
	return time.Since(c.Timestamp) > c.TTL
}

// size returns the approximate size of the cached response in bytes.
func (c *cachedResponse) size() int64 {
	size := int64(len(c.Body))
	for key, values := range c.Header {
		size += int64(len(key))
		for _, v := range values {
			size += int64(len(v))
		}
	}
	return size
}

// Middleware provides HTTP response caching.
type Middleware struct {
	config Config
	cache  map[string]*cachedResponse
	mu     sync.RWMutex
	size   int64 // Current cache size in bytes
}

// New creates a new cache middleware.
func New(config Config) *Middleware {
	if config.KeyFunc == nil {
		config.KeyFunc = DefaultKeyFunc
	}
	if config.TTL == 0 {
		config.TTL = 5 * time.Minute
	}
	if len(config.Methods) == 0 {
		config.Methods = []string{"GET", "HEAD"}
	}
	if len(config.StatusCodes) == 0 {
		config.StatusCodes = []int{200, 203, 204, 206, 300, 301, 404, 405, 410, 414, 501}
	}

	return &Middleware{
		config: config,
		cache:  make(map[string]*cachedResponse),
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "cache"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 55 // After MaxBodySize(50), before RealIP(15) and ForceSSL(70)
}

// Wrap wraps the handler with caching.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if method is cacheable
		if !m.isMethodCacheable(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) && (len(r.URL.Path) == len(path) || r.URL.Path[len(path)] == '/' || path[len(path)-1] == '/') {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check Cache-Control: no-cache or no-store
		if cc := r.Header.Get("Cache-Control"); cc != "" {
			if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Generate cache key
		key := m.config.KeyFunc(r)

		// Try to serve from cache
		if cached := m.get(key); cached != nil {
			// Check conditional request (If-None-Match / If-Modified-Since)
			if m.isNotModified(r, cached) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			m.serveFromCache(w, r, cached)
			return
		}

		// Capture response
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)

		// Store in cache if cacheable
		if m.isCacheable(rec, r) {
			m.store(key, rec)
		}

		// Write response to client
		m.writeResponse(w, rec)
	})
}

// isMethodCacheable checks if the HTTP method can be cached.
func (m *Middleware) isMethodCacheable(method string) bool {
	for _, m := range m.config.Methods {
		if m == method {
			return true
		}
	}
	return false
}

// isStatusCacheable checks if the status code can be cached.
func (m *Middleware) isStatusCacheable(status int) bool {
	for _, s := range m.config.StatusCodes {
		if s == status {
			return true
		}
	}
	return false
}

// get retrieves a cached response.
func (m *Middleware) get(key string) *cachedResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cached, ok := m.cache[key]
	if !ok {
		return nil
	}

	if cached.isExpired() {
		// Don't delete here to avoid lock contention, let cleanup handle it
		return nil
	}

	return cached
}

// store stores a response in the cache.
func (m *Middleware) store(key string, rec *httptest.ResponseRecorder) {
	// Check max entries limit
	if m.config.MaxEntries > 0 && len(m.cache) >= m.config.MaxEntries {
		m.evictOldest()
	}

	body := rec.Body.Bytes()

	// Check max size
	if m.config.MaxSize > 0 {
		entrySize := int64(len(body))
		if entrySize > m.config.MaxSize {
			return // Entry too large
		}
	}

	// Copy headers
	header := make(http.Header)
	for k, v := range rec.Header() {
		header[k] = v
	}

	// Generate ETag if not present
	etag := header.Get("ETag")
	if etag == "" && len(body) > 0 {
		h := sha256.New()
		h.Write(body)
		etag = fmt.Sprintf("%x", h.Sum(nil))[:16]
		header.Set("ETag", etag)
	}

	cached := &cachedResponse{
		StatusCode: rec.Code,
		Header:     header,
		Body:       body,
		Timestamp:  time.Now(),
		TTL:        m.config.TTL,
		ETag:       etag,
	}

	// Respect Cache-Control max-age
	if cc := rec.Header().Get("Cache-Control"); cc != "" {
		if maxAge := parseMaxAge(cc); maxAge > 0 {
			cached.TTL = time.Duration(maxAge) * time.Second
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check size again under lock
	if m.config.MaxSize > 0 && m.size+cached.size() > m.config.MaxSize {
		// Try to make room
		for m.size+cached.size() > m.config.MaxSize && len(m.cache) > 0 {
			m.evictOldestUnderLock()
		}
		if m.size+cached.size() > m.config.MaxSize {
			return // Still too big
		}
	}

	m.cache[key] = cached
	m.size += cached.size()
}

// evictOldest evicts the oldest cache entry (called outside lock).
func (m *Middleware) evictOldest() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictOldestUnderLock()
}

// evictOldestUnderLock evicts the oldest cache entry (must hold lock).
func (m *Middleware) evictOldestUnderLock() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range m.cache {
		if oldestKey == "" || cached.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.Timestamp
		}
	}

	if oldestKey != "" {
		m.size -= m.cache[oldestKey].size()
		delete(m.cache, oldestKey)
	}
}

// isCacheable checks if a response should be cached.
func (m *Middleware) isCacheable(rec *httptest.ResponseRecorder, r *http.Request) bool {
	// Check status code
	if !m.isStatusCacheable(rec.Code) {
		return false
	}

	// Check Cache-Control headers
	cc := rec.Header().Get("Cache-Control")
	if cc != "" {
		// Check no-store
		if strings.Contains(cc, "no-store") {
			return false
		}
		// Check private (unless configured to cache private)
		if strings.Contains(cc, "private") && !m.config.CachePrivate {
			return false
		}
		// Check no-cache (unless has ETag or Last-Modified for conditional)
		if strings.Contains(cc, "no-cache") && rec.Header().Get("ETag") == "" && rec.Header().Get("Last-Modified") == "" {
			return false
		}
	}

	// Check for cookies (unless configured to cache with cookies)
	if !m.config.CacheCookies {
		if rec.Header().Get("Set-Cookie") != "" || r.Header.Get("Cookie") != "" {
			return false
		}
	}

	// Check excluded response headers
	for header, pattern := range m.config.ExcludeHeaders {
		if rec.Header().Get(header) != "" {
			if pattern == "" || strings.Contains(rec.Header().Get(header), pattern) {
				return false
			}
		}
	}

	// Check Vary: * (uncacheable)
	if vary := rec.Header().Get("Vary"); vary == "*" {
		return false
	}

	// Check body size
	if m.config.MaxSize > 0 && int64(rec.Body.Len()) > m.config.MaxSize {
		return false
	}

	return true
}

// isNotModified checks if the cached response is not modified.
func (m *Middleware) isNotModified(r *http.Request, cached *cachedResponse) bool {
	// Check If-None-Match (ETag)
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if inm == cached.ETag || inm == "*" {
			return true
		}
	}

	// Check If-Modified-Since
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if imstime, err := http.ParseTime(ims); err == nil {
			if time.Since(cached.Timestamp) < time.Since(imstime) {
				return true
			}
		}
	}

	return false
}

// serveFromCache serves a cached response.
func (m *Middleware) serveFromCache(w http.ResponseWriter, r *http.Request, cached *cachedResponse) {
	// Copy headers
	for key, values := range cached.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Add cache headers
	w.Header().Set("X-Cache", "HIT")
	age := int(time.Since(cached.Timestamp).Seconds())
	w.Header().Set("Age", strconv.Itoa(age))

	// Handle HEAD requests
	if r.Method == "HEAD" {
		w.WriteHeader(cached.StatusCode)
		return
	}

	// Write response
	w.WriteHeader(cached.StatusCode)
	_, _ = w.Write(cached.Body)
}

// writeResponse writes the recorded response to the client.
func (m *Middleware) writeResponse(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	// Copy headers
	for key, values := range rec.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Add cache headers
	w.Header().Set("X-Cache", "MISS")

	// Write response
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// parseMaxAge parses max-age directive from Cache-Control header.
func parseMaxAge(cc string) int {
	// Simple parser for max-age=N
	idx := strings.Index(cc, "max-age=")
	if idx == -1 {
		return 0
	}

	start := idx + len("max-age=")
	end := start
	for end < len(cc) && (cc[end] >= '0' && cc[end] <= '9') {
		end++
	}

	if end > start {
		if age, err := strconv.Atoi(cc[start:end]); err == nil {
			return age
		}
	}

	return 0
}

// Stats returns cache statistics.
func (m *Middleware) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var expired int
	for _, cached := range m.cache {
		if cached.isExpired() {
			expired++
		}
	}

	return map[string]any{
		"entries":     len(m.cache),
		"expired":     expired,
		"size_bytes":  m.size,
		"max_entries": m.config.MaxEntries,
		"max_size":    m.config.MaxSize,
		"default_ttl": m.config.TTL.String(),
	}
}

// Purge removes all cached entries matching a pattern (simple prefix match).
func (m *Middleware) Purge(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for key := range m.cache {
		if strings.HasPrefix(key, prefix) {
			m.size -= m.cache[key].size()
			delete(m.cache, key)
			count++
		}
	}

	return count
}

// Clear removes all cached entries.
func (m *Middleware) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache = make(map[string]*cachedResponse)
	m.size = 0
}

// CacheWriter wraps http.ResponseWriter to capture response for caching.
type CacheWriter struct {
	http.ResponseWriter
	StatusCode int
	Body       *bytes.Buffer
	Written    bool
}

// NewCacheWriter creates a new cache writer.
func NewCacheWriter(w http.ResponseWriter) *CacheWriter {
	return &CacheWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
		Body:           &bytes.Buffer{},
	}
}

// WriteHeader captures the status code.
func (cw *CacheWriter) WriteHeader(code int) {
	if cw.Written {
		return
	}
	cw.StatusCode = code
	cw.Written = true
	cw.ResponseWriter.WriteHeader(code)
}

// Write captures the body.
func (cw *CacheWriter) Write(p []byte) (int, error) {
	if !cw.Written {
		cw.WriteHeader(http.StatusOK)
	}
	cw.Body.Write(p)
	return cw.ResponseWriter.Write(p)
}
