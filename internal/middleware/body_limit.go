package middleware

import (
	"net/http"
)

// BodyLimitConfig configures the request body size limit middleware.
type BodyLimitConfig struct {
	// MaxSize is the maximum request body size in bytes.
	// Default: 10 MB (10 * 1024 * 1024).
	MaxSize int64
}

// DefaultBodyLimitConfig returns sensible defaults.
func DefaultBodyLimitConfig() BodyLimitConfig {
	return BodyLimitConfig{
		MaxSize: 10 * 1024 * 1024, // 10 MB
	}
}

// BodyLimitMiddleware enforces a maximum request body size.
// Requests exceeding the limit receive a 413 Request Entity Too Large response.
type BodyLimitMiddleware struct {
	config BodyLimitConfig
}

// NewBodyLimitMiddleware creates a new body limit middleware.
func NewBodyLimitMiddleware(config BodyLimitConfig) *BodyLimitMiddleware {
	if config.MaxSize <= 0 {
		config.MaxSize = 10 * 1024 * 1024
	}
	return &BodyLimitMiddleware{config: config}
}

// Name returns the middleware name.
func (b *BodyLimitMiddleware) Name() string { return "body_limit" }

// Priority returns the middleware priority (runs very early).
func (b *BodyLimitMiddleware) Priority() int { return PriorityRealIP - 50 } // 250

// Wrap wraps the handler with body size enforcement.
func (b *BodyLimitMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > b.config.MaxSize {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		// Wrap the body with a limit reader to enforce even if Content-Length is absent
		r.Body = http.MaxBytesReader(w, r.Body, b.config.MaxSize)
		next.ServeHTTP(w, r)
	})
}
