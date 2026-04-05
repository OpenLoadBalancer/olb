// Package middleware provides HTTP middleware components.
package middleware

import (
	"net/http"
	"strings"
)

// StripPrefixConfig configures path prefix stripping.
type StripPrefixConfig struct {
	Prefix string // Prefix to strip from request path
	// RedirectSlash if true, redirects /prefix to /prefix/
	RedirectSlash bool
}

// StripPrefixMiddleware strips a prefix from the request path.
type StripPrefixMiddleware struct {
	prefix string
	config StripPrefixConfig
}

// NewStripPrefixMiddleware creates a new StripPrefixMiddleware.
func NewStripPrefixMiddleware(config StripPrefixConfig) *StripPrefixMiddleware {
	// Ensure prefix starts with /
	prefix := config.Prefix
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	// Remove trailing slash for matching
	prefix = strings.TrimSuffix(prefix, "/")

	return &StripPrefixMiddleware{
		prefix: prefix,
		config: config,
	}
}

// Name returns the middleware name.
func (m *StripPrefixMiddleware) Name() string {
	return "strip_prefix"
}

// Priority returns the middleware priority.
// Runs early (priority 150) to modify path before routing.
func (m *StripPrefixMiddleware) Priority() int {
	return 150
}

// Wrap wraps the handler with prefix stripping.
func (m *StripPrefixMiddleware) Wrap(next http.Handler) http.Handler {
	if m.prefix == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path starts with prefix
		if !strings.HasPrefix(r.URL.Path, m.prefix) {
			next.ServeHTTP(w, r)
			return
		}

		// Handle redirect-slash: /prefix -> /prefix/
		if m.config.RedirectSlash && r.URL.Path == m.prefix {
			http.Redirect(w, r, m.prefix+"/", http.StatusMovedPermanently)
			return
		}

		// Strip the prefix
		newPath := strings.TrimPrefix(r.URL.Path, m.prefix)
		if newPath == "" {
			newPath = "/"
		}

		// Create new URL with stripped path
		newURL := *r.URL
		newURL.Path = newPath

		// Create new request with modified path
		newReq := r.Clone(r.Context())
		newReq.URL = &newURL
		newReq.RequestURI = newPath

		next.ServeHTTP(w, newReq)
	})
}

// StripPrefix is a convenience function for simple prefix stripping.
func StripPrefix(prefix string) *StripPrefixMiddleware {
	return NewStripPrefixMiddleware(StripPrefixConfig{
		Prefix:        prefix,
		RedirectSlash: false,
	})
}
