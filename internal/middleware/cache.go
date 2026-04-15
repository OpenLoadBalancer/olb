// Package middleware provides HTTP middleware infrastructure for OpenLoadBalancer.
package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PriorityCache is the execution priority for the cache middleware.
// It runs after compression and before the final handler so that
// cached responses are served before hitting the backend.
const PriorityCache = 750

// CacheStatus represents the result of a cache lookup.
type CacheStatus string

const (
	// CacheHit means the response was served from cache.
	CacheHit CacheStatus = "HIT"
	// CacheMiss means the response was not in cache.
	CacheMiss CacheStatus = "MISS"
	// CacheStale means a stale cached response was served while revalidating.
	CacheStale CacheStatus = "STALE"
)

// CacheConfig configures the response cache middleware.
type CacheConfig struct {
	// MaxSize is the maximum total size of cached response bodies in bytes.
	// Default: 100MB (104857600).
	MaxSize int64

	// MaxEntries is the maximum number of cached responses.
	// Default: 10000.
	MaxEntries int

	// DefaultTTL is the default time-to-live for cached responses.
	// Default: 5 minutes.
	DefaultTTL time.Duration

	// MinResponseSize is the minimum response body size to cache.
	// Default: 0 (cache all sizes).
	MinResponseSize int64

	// MaxResponseSize is the maximum response body size to cache.
	// Default: 10MB (10485760).
	MaxResponseSize int64

	// CacheableMethods lists the HTTP methods whose responses may be cached.
	// Default: GET, HEAD.
	CacheableMethods []string

	// CacheableStatuses lists the HTTP status codes whose responses may be cached.
	// Default: 200, 301, 404.
	CacheableStatuses []int

	// RespectCacheControl controls whether Cache-Control headers from
	// the origin are honoured. Default: true.
	RespectCacheControl bool

	// StaleWhileRevalidate is the duration after TTL expiry during which a
	// stale response may be served while a background refresh occurs.
	// Default: 30 seconds.
	StaleWhileRevalidate time.Duration

	// CacheAuthenticatedRequests controls whether requests containing an
	// Authorization header are eligible for caching. Default: false.
	CacheAuthenticatedRequests bool
}

// DefaultCacheConfig returns a CacheConfig populated with sensible defaults.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize:              104857600, // 100 MB
		MaxEntries:           10000,
		DefaultTTL:           5 * time.Minute,
		MinResponseSize:      0,
		MaxResponseSize:      10485760, // 10 MB
		CacheableMethods:     []string{"GET", "HEAD"},
		CacheableStatuses:    []int{200, 301, 404},
		RespectCacheControl:  true,
		StaleWhileRevalidate: 30 * time.Second,
	}
}

// CachedResponse holds a complete HTTP response that has been cached.
type CachedResponse struct {
	// StatusCode is the HTTP status code of the cached response.
	StatusCode int

	// Headers contains a copy of the response headers.
	Headers http.Header

	// Body is the response body bytes.
	Body []byte

	// CachedAt is the time the response was stored in the cache.
	CachedAt time.Time

	// TTL is the time-to-live for this cached response.
	TTL time.Duration

	// Stale indicates whether this entry has passed its TTL but is still
	// within the stale-while-revalidate window.
	Stale bool
}

// Expired returns true if the cached response has exceeded its TTL.
func (cr *CachedResponse) Expired() bool {
	return time.Since(cr.CachedAt) > cr.TTL
}

// Age returns the duration since the response was cached.
func (cr *CachedResponse) Age() time.Duration {
	return time.Since(cr.CachedAt)
}

// cacheEntry wraps CachedResponse with internal bookkeeping.
type cacheEntry struct {
	response *CachedResponse
	key      string
	size     int64 // size of the body in bytes
}

// CacheMiddleware is an HTTP response cache that stores complete responses
// in memory and serves them for subsequent identical requests.
//
// Thread-safe for concurrent use.
type CacheMiddleware struct {
	config CacheConfig

	mu      sync.RWMutex
	entries map[string]*cacheEntry
	order   []string // LRU order: index 0 = most recently used

	currentSize  int64
	currentCount int

	// methods is a set of cacheable methods for O(1) lookup.
	methods map[string]bool
	// statuses is a set of cacheable statuses for O(1) lookup.
	statuses map[int]bool

	// revalidating tracks keys currently being revalidated to avoid
	// duplicate background refreshes.
	revalidating sync.Map

	// stats
	hits   atomic.Int64
	misses atomic.Int64
}

// NewCacheMiddleware creates a new response cache middleware.
func NewCacheMiddleware(config CacheConfig) *CacheMiddleware {
	// Apply defaults for zero-value fields.
	if config.MaxSize <= 0 {
		config.MaxSize = 104857600
	}
	if config.MaxEntries <= 0 {
		config.MaxEntries = 10000
	}
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}
	if config.MaxResponseSize <= 0 {
		config.MaxResponseSize = 10485760
	}
	if len(config.CacheableMethods) == 0 {
		config.CacheableMethods = []string{"GET", "HEAD"}
	}
	if len(config.CacheableStatuses) == 0 {
		config.CacheableStatuses = []int{200, 301, 404}
	}
	// StaleWhileRevalidate of 0 is valid (disables stale serving).
	// A negative value is treated as 0 (disabled).
	if config.StaleWhileRevalidate < 0 {
		config.StaleWhileRevalidate = 0
	}

	methods := make(map[string]bool, len(config.CacheableMethods))
	for _, m := range config.CacheableMethods {
		methods[strings.ToUpper(m)] = true
	}

	statuses := make(map[int]bool, len(config.CacheableStatuses))
	for _, s := range config.CacheableStatuses {
		statuses[s] = true
	}

	return &CacheMiddleware{
		config:   config,
		entries:  make(map[string]*cacheEntry, 256),
		order:    make([]string, 0, 256),
		methods:  methods,
		statuses: statuses,
	}
}

// Name returns the middleware name.
func (c *CacheMiddleware) Name() string {
	return "cache"
}

// Priority returns the middleware priority.
func (c *CacheMiddleware) Priority() int {
	return PriorityCache
}

// Wrap wraps the next handler with caching logic.
func (c *CacheMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check whether this request is cacheable.
		if !c.isRequestCacheable(r) {
			w.Header().Set("X-Cache", string(CacheMiss))
			next.ServeHTTP(w, r)
			return
		}

		key := CacheKey(r)

		// Attempt cache lookup.
		if cached, status := c.get(key); cached != nil {
			c.serveCached(w, cached, status)

			// If stale, trigger background revalidation.
			if status == CacheStale {
				c.revalidateInBackground(key, next, r)
			}
			return
		}

		// Cache miss — capture the response.
		c.misses.Add(1)
		rec := &responseCapturer{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		// After the handler has written the response, decide whether to cache.
		c.maybeCacheResponse(key, r, rec)

		// Set cache miss header (may have already been written in some edge
		// cases, but Header().Set before body write is fine).
		w.Header().Set("X-Cache", string(CacheMiss))
	})
}

// CacheKey generates a deterministic cache key from the request.
// Format: method + host + path + sorted query parameters.
func CacheKey(r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(r.Method))
	h.Write([]byte("|"))
	h.Write([]byte(r.Host))
	h.Write([]byte("|"))
	h.Write([]byte(r.URL.Path))
	h.Write([]byte("|"))

	// Sort query parameters for determinism.
	query := r.URL.Query()
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		vals := query[k]
		sort.Strings(vals)
		for _, v := range vals {
			h.Write([]byte(k))
			h.Write([]byte("="))
			h.Write([]byte(v))
			h.Write([]byte("&"))
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// get looks up a cache entry and returns the response together with its
// cache status.  Returns (nil, "") on miss.
func (c *CacheMiddleware) get(key string) (*CachedResponse, CacheStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ent, ok := c.entries[key]
	if !ok {
		return nil, ""
	}

	resp := ent.response

	if !resp.Expired() {
		// Fresh hit.
		c.promoteKey(key)
		c.hits.Add(1)
		return resp, CacheHit
	}

	// Expired — check stale-while-revalidate window.
	staleDeadline := resp.CachedAt.Add(resp.TTL).Add(c.config.StaleWhileRevalidate)
	if time.Now().Before(staleDeadline) {
		c.promoteKey(key)
		c.hits.Add(1)
		cp := *resp
		cp.Stale = true
		return &cp, CacheStale
	}

	// Beyond stale window — evict.
	c.removeKeyLocked(key)
	return nil, ""
}

// serveCached writes a cached response to the client.
func (c *CacheMiddleware) serveCached(w http.ResponseWriter, cached *CachedResponse, status CacheStatus) {
	// Copy headers.
	for k, vals := range cached.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	// Set cache-specific headers.
	w.Header().Set("X-Cache", string(status))
	age := max(int(cached.Age().Seconds()), 0)
	w.Header().Set("Age", strconv.Itoa(age))

	w.WriteHeader(cached.StatusCode)
	if len(cached.Body) > 0 {
		_, _ = w.Write(cached.Body)
	}
}

// revalidateInBackground triggers an asynchronous re-fetch for the given key.
func (c *CacheMiddleware) revalidateInBackground(key string, next http.Handler, origReq *http.Request) {
	// Avoid duplicate revalidation for the same key.
	if _, loaded := c.revalidating.LoadOrStore(key, true); loaded {
		return
	}

	go func() {
		defer c.revalidating.Delete(key)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[cache] panic recovered in background revalidation: %v", r)
			}
		}()
		// context is canceled as soon as the handler returns, which would
		// immediately kill the background revalidation.
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer bgCancel()
		req := origReq.Clone(bgCtx)

		rec := &responseCapturer{
			ResponseWriter: &discardResponseWriter{header: make(http.Header)},
			body:           &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, req)
		c.maybeCacheResponse(key, req, rec)
	}()
}

// maybeCacheResponse decides whether to store the captured response.
func (c *CacheMiddleware) maybeCacheResponse(key string, _ *http.Request, rec *responseCapturer) {
	// Check status code.
	if !c.statuses[rec.statusCode] {
		return
	}

	bodyLen := int64(rec.body.Len())

	// Check body size constraints.
	if bodyLen < c.config.MinResponseSize {
		return
	}
	if bodyLen > c.config.MaxResponseSize {
		return
	}

	// Determine TTL from response headers.
	ttl := c.determineTTL(rec.Header())
	if ttl <= 0 {
		return
	}

	// Check response Cache-Control directives.
	if c.config.RespectCacheControl {
		cc := parseCacheControl(rec.Header().Get("Cache-Control"))
		if cc.noStore || cc.private {
			return
		}
	}

	// Build the cached response.
	headers := make(http.Header, len(rec.Header()))
	for k, vals := range rec.Header() {
		headers[k] = append([]string(nil), vals...)
	}
	// Remove hop-by-hop headers from cache.
	headers.Del("Connection")
	headers.Del("Keep-Alive")
	headers.Del("Transfer-Encoding")

	cached := &CachedResponse{
		StatusCode: rec.statusCode,
		Headers:    headers,
		Body:       append([]byte(nil), rec.body.Bytes()...),
		CachedAt:   time.Now(),
		TTL:        ttl,
	}

	c.put(key, cached)
}

// determineTTL returns the TTL for a response, taking into account
// Cache-Control headers when RespectCacheControl is enabled.
func (c *CacheMiddleware) determineTTL(header http.Header) time.Duration {
	if !c.config.RespectCacheControl {
		return c.config.DefaultTTL
	}

	cc := parseCacheControl(header.Get("Cache-Control"))

	// no-store means do not cache at all.
	if cc.noStore {
		return 0
	}

	// s-maxage takes precedence for shared caches.
	if cc.sMaxAge > 0 {
		return cc.sMaxAge
	}

	// max-age.
	if cc.maxAge > 0 {
		return cc.maxAge
	}

	return c.config.DefaultTTL
}

// put inserts or updates a cached response, enforcing size and entry limits.
func (c *CacheMiddleware) put(key string, resp *CachedResponse) {
	size := int64(len(resp.Body))

	c.mu.Lock()
	defer c.mu.Unlock()

	// If the key already exists, remove the old entry first.
	if old, ok := c.entries[key]; ok {
		c.currentSize -= old.size
		c.currentCount--
		c.removeFromOrder(key)
	}

	// Evict entries until we have room.
	for c.currentCount >= c.config.MaxEntries && len(c.order) > 0 {
		c.evictOldestLocked()
	}
	for c.currentSize+size > c.config.MaxSize && len(c.order) > 0 {
		c.evictOldestLocked()
	}

	ent := &cacheEntry{
		response: resp,
		key:      key,
		size:     size,
	}
	c.entries[key] = ent
	c.order = append([]string{key}, c.order...)
	c.currentSize += size
	c.currentCount++
}

// promoteKey moves a key to the front of the LRU order.
// Caller must hold c.mu.
func (c *CacheMiddleware) promoteKey(key string) {
	for i, k := range c.order {
		if k == key {
			// Shift elements right and place key at front.
			copy(c.order[1:i+1], c.order[0:i])
			c.order[0] = key
			return
		}
	}
}

// removeFromOrder removes a key from the LRU order slice.
// Caller must hold c.mu.
func (c *CacheMiddleware) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// removeKeyLocked removes an entry by key.
// Caller must hold c.mu.
func (c *CacheMiddleware) removeKeyLocked(key string) {
	if ent, ok := c.entries[key]; ok {
		c.currentSize -= ent.size
		c.currentCount--
		delete(c.entries, key)
		c.removeFromOrder(key)
	}
}

// evictOldestLocked removes the least recently used entry.
// Caller must hold c.mu.
func (c *CacheMiddleware) evictOldestLocked() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[len(c.order)-1]
	c.order = c.order[:len(c.order)-1]
	if ent, ok := c.entries[oldest]; ok {
		c.currentSize -= ent.size
		c.currentCount--
		delete(c.entries, oldest)
	}
}

// isRequestCacheable determines whether a request is eligible for caching.
func (c *CacheMiddleware) isRequestCacheable(r *http.Request) bool {
	// Check HTTP method.
	if !c.methods[r.Method] {
		return false
	}

	// Skip requests with Authorization header unless configured otherwise.
	if !c.config.CacheAuthenticatedRequests && r.Header.Get("Authorization") != "" {
		return false
	}

	// Respect request Cache-Control headers.
	if c.config.RespectCacheControl {
		cc := parseCacheControl(r.Header.Get("Cache-Control"))
		if cc.noStore {
			return false
		}
		if cc.noCache {
			return false
		}
	}

	return true
}

// Purge removes a specific entry from the cache by key.
// Returns true if the entry was found and removed.
func (c *CacheMiddleware) Purge(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[key]; !ok {
		return false
	}
	c.removeKeyLocked(key)
	return true
}

// PurgeAll removes all entries from the cache.
func (c *CacheMiddleware) PurgeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry, 256)
	c.order = c.order[:0]
	c.currentSize = 0
	c.currentCount = 0
}

// Len returns the number of entries currently in the cache.
func (c *CacheMiddleware) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentCount
}

// Size returns the total size of cached response bodies in bytes.
func (c *CacheMiddleware) Size() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize
}

// Stats returns cache hit/miss statistics.
func (c *CacheMiddleware) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// --- Cache-Control parsing ---

// cacheDirectives holds parsed Cache-Control header values.
type cacheDirectives struct {
	noCache bool
	noStore bool
	private bool
	maxAge  time.Duration
	sMaxAge time.Duration
}

// parseCacheControl parses a Cache-Control header value into directives.
func parseCacheControl(value string) cacheDirectives {
	var d cacheDirectives
	if value == "" {
		return d
	}

	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)

		switch {
		case lower == "no-cache":
			d.noCache = true
		case lower == "no-store":
			d.noStore = true
		case lower == "private":
			d.private = true
		case strings.HasPrefix(lower, "s-maxage="):
			if val, err := strconv.Atoi(strings.TrimPrefix(lower, "s-maxage=")); err == nil && val >= 0 {
				d.sMaxAge = time.Duration(val) * time.Second
			}
		case strings.HasPrefix(lower, "max-age="):
			if val, err := strconv.Atoi(strings.TrimPrefix(lower, "max-age=")); err == nil && val >= 0 {
				d.maxAge = time.Duration(val) * time.Second
			}
		}
	}

	return d
}

// --- Response capturer ---

// responseCapturer captures the status code, headers, and body written by
// a handler so they can be inspected and optionally cached.
type responseCapturer struct {
	http.ResponseWriter
	body        *bytes.Buffer
	statusCode  int
	wroteHeader bool
	wroteBody   bool
}

// WriteHeader captures the status code and forwards it.
func (rc *responseCapturer) WriteHeader(status int) {
	if rc.wroteHeader {
		return
	}
	rc.wroteHeader = true
	rc.statusCode = status
	rc.ResponseWriter.WriteHeader(status)
}

// Write captures the response body and forwards it.
func (rc *responseCapturer) Write(b []byte) (int, error) {
	if !rc.wroteBody {
		rc.wroteBody = true
	}
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}

// --- discard response writer (for background revalidation) ---

// discardResponseWriter is an http.ResponseWriter that discards all output.
// Used for background revalidation where we don't have a real client.
type discardResponseWriter struct {
	header http.Header
}

func (d *discardResponseWriter) Header() http.Header {
	return d.header
}

func (d *discardResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (d *discardResponseWriter) WriteHeader(_ int) {}

// Ensure CacheMiddleware implements the Middleware interface.
var _ Middleware = (*CacheMiddleware)(nil)

// Ensure discardResponseWriter implements http.ResponseWriter.
var _ http.ResponseWriter = (*discardResponseWriter)(nil)
