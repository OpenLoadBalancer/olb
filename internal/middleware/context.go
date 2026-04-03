// Package middleware provides HTTP middleware components for OpenLoadBalancer.
package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/router"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey int

const (
	// clientIPKey is the context key for storing the client IP.
	clientIPKey contextKey = iota
)

// WithClientIP returns a new context with the client IP stored.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey, ip)
}

// ClientIPFromContext retrieves the client IP from the context.
// Returns empty string if not found.
func ClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if ip, ok := ctx.Value(clientIPKey).(string); ok {
		return ip
	}
	return ""
}

// RequestContext holds per-request state that is passed through the middleware chain.
type RequestContext struct {
	// Request is the original HTTP request.
	Request *http.Request

	// Response is the wrapped response writer.
	Response ResponseWriter

	// Route is the matched route configuration (may be nil if not matched yet).
	Route *router.Route

	// Backend is the selected backend for this request (may be nil if not selected yet).
	Backend *backend.Backend

	// StartTime is when the request processing began.
	StartTime time.Time

	// RequestID is the unique identifier for this request.
	RequestID string

	// ClientIP is the real client IP address (after processing X-Forwarded-For, etc.).
	ClientIP string

	// BytesIn is the number of bytes read from the client.
	BytesIn int64

	// BytesOut is the number of bytes written to the client.
	BytesOut int64

	// StatusCode is the HTTP status code (captured from response).
	StatusCode int

	// Metadata holds arbitrary key-value data for middleware communication.
	// Use Set() and Get() for thread-safe access.
	metadata map[string]any

	// mu protects metadata for concurrent access.
	mu sync.RWMutex
}

// pool for recycling RequestContext objects.
var requestContextPool = sync.Pool{
	New: func() any {
		return &RequestContext{
			metadata: make(map[string]any),
		}
	},
}

// NewRequestContext creates a new RequestContext for the given request.
// The ResponseWriter is wrapped to capture metadata.
func NewRequestContext(req *http.Request, rw http.ResponseWriter) *RequestContext {
	ctx := requestContextPool.Get().(*RequestContext)
	ctx.Request = req
	ctx.Response = NewResponseWriter(rw)
	ctx.Route = nil
	ctx.Backend = nil
	ctx.StartTime = time.Now()
	ctx.RequestID = ""
	ctx.ClientIP = ""
	ctx.BytesIn = 0
	ctx.BytesOut = 0
	ctx.StatusCode = 0
	// Clear metadata map
	for k := range ctx.metadata {
		delete(ctx.metadata, k)
	}
	return ctx
}

// Set stores a value in the metadata map.
func (c *RequestContext) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadata[key] = value
}

// Get retrieves a value from the metadata map.
// Returns the value and true if found, nil and false otherwise.
func (c *RequestContext) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.metadata[key]
	return val, ok
}

// GetString retrieves a string value from metadata.
// Returns empty string if not found or not a string.
func (c *RequestContext) GetString(key string) string {
	val, ok := c.Get(key)
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

// GetInt retrieves an int value from metadata.
// Returns 0 if not found or not an int.
func (c *RequestContext) GetInt(key string) int {
	val, ok := c.Get(key)
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	default:
		return 0
	}
}

// GetBool retrieves a bool value from metadata.
// Returns false if not found or not a bool.
func (c *RequestContext) GetBool(key string) bool {
	val, ok := c.Get(key)
	if !ok {
		return false
	}
	b, ok := val.(bool)
	if !ok {
		return false
	}
	return b
}

// Delete removes a key from the metadata map.
func (c *RequestContext) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.metadata, key)
}

// Has checks if a key exists in the metadata map.
func (c *RequestContext) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.metadata[key]
	return ok
}

// Duration returns the elapsed time since request processing began.
func (c *RequestContext) Duration() time.Duration {
	return time.Since(c.StartTime)
}

// UpdateStatus updates the status code in the context.
// This is typically called after the response is written.
func (c *RequestContext) UpdateStatus(status int) {
	c.StatusCode = status
}

// UpdateBytesOut updates the bytes out counter.
func (c *RequestContext) UpdateBytesOut(n int64) {
	c.BytesOut += n
}

// UpdateBytesIn updates the bytes in counter.
func (c *RequestContext) UpdateBytesIn(n int64) {
	c.BytesIn += n
}

// Release returns the RequestContext to the pool for reuse.
// This should be called when the request is complete.
func (c *RequestContext) Release() {
	// Release the wrapped response writer
	if rw, ok := c.Response.(*responseWriter); ok {
		rw.Release()
	}
	// Clear fields to help GC
	c.Request = nil
	c.Response = nil
	c.Route = nil
	c.Backend = nil
	c.RequestID = ""
	c.ClientIP = ""
	c.StatusCode = 0
	c.BytesIn = 0
	c.BytesOut = 0
	// Clear metadata
	c.mu.Lock()
	for k := range c.metadata {
		delete(c.metadata, k)
	}
	c.mu.Unlock()
	requestContextPool.Put(c)
}

// AllMetadata returns a copy of all metadata.
func (c *RequestContext) AllMetadata() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]any, len(c.metadata))
	for k, v := range c.metadata {
		result[k] = v
	}
	return result
}
