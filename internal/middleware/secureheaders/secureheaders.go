// Package secureheaders provides security headers middleware.
package secureheaders

import (
	"net/http"
	"strconv"
	"strings"
)

// Config configures security headers.
type Config struct {
	Enabled                       bool        // Enable security headers
	XFrameOptions                 string      // X-Frame-Options: DENY, SAMEORIGIN, ALLOW-FROM uri
	XContentTypeOptions           bool        // X-Content-Type-Options: nosniff
	XXSSProtection                string      // X-XSS-Protection: 0, 1, 1; mode=block
	ReferrerPolicy                string      // Referrer-Policy: no-referrer, etc.
	ContentSecurityPolicy         string      // Content-Security-Policy (if CSP middleware not used)
	StrictTransportPolicy         *HSTSConfig // Strict-Transport-Security (HSTS)
	XPermittedCrossDomainPolicies string      // X-Permitted-Cross-Domain-Policies
	XDownloadOptions              string      // X-Download-Options: noopen
	XDNSPrefetchControl           string      // X-DNS-Prefetch-Control: off
	PermissionsPolicy             string      // Permissions-Policy
	CrossOriginEmbedderPolicy     string      // Cross-Origin-Embedder-Policy
	CrossOriginOpenerPolicy       string      // Cross-Origin-Opener-Policy
	CrossOriginResourcePolicy     string      // Cross-Origin-Resource-Policy
	CacheControl                  string      // Cache-Control override
	ExcludePaths                  []string    // Paths to exclude
}

// HSTSConfig configures HTTP Strict Transport Security.
type HSTSConfig struct {
	MaxAge            int  // max-age in seconds
	IncludeSubdomains bool // includeSubDomains
	Preload           bool // preload
}

// DefaultConfig returns default security headers configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:                   false,
		XFrameOptions:             "DENY",
		XContentTypeOptions:       true,
		XXSSProtection:            "1; mode=block",
		ReferrerPolicy:            "strict-origin-when-cross-origin",
		XDownloadOptions:          "noopen",
		XDNSPrefetchControl:       "off",
		CrossOriginEmbedderPolicy: "require-corp",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
	}
}

// Middleware provides security headers functionality.
type Middleware struct {
	config Config
}

// New creates a new secure headers middleware.
func New(config Config) *Middleware {
	return &Middleware{config: config}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "secureheaders"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 750 // Late in the chain, before transformer (850)
}

// Wrap wraps the handler with security headers.
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

		// Set security headers before calling next
		m.setHeaders(w, r)

		next.ServeHTTP(w, r)
	})
}

// setHeaders sets all configured security headers.
func (m *Middleware) setHeaders(w http.ResponseWriter, r *http.Request) {
	h := w.Header()

	// X-Frame-Options (clickjacking protection)
	if m.config.XFrameOptions != "" {
		h.Set("X-Frame-Options", m.config.XFrameOptions)
	}

	// X-Content-Type-Options (MIME sniffing protection)
	if m.config.XContentTypeOptions {
		h.Set("X-Content-Type-Options", "nosniff")
	}

	// X-XSS-Protection (legacy XSS filter, mostly for older browsers)
	if m.config.XXSSProtection != "" {
		h.Set("X-XSS-Protection", m.config.XXSSProtection)
	}

	// Referrer-Policy
	if m.config.ReferrerPolicy != "" {
		h.Set("Referrer-Policy", m.config.ReferrerPolicy)
	}

	// Content-Security-Policy (if not using dedicated CSP middleware)
	if m.config.ContentSecurityPolicy != "" {
		h.Set("Content-Security-Policy", m.config.ContentSecurityPolicy)
	}

	// Strict-Transport-Security (HSTS)
	if m.config.StrictTransportPolicy != nil {
		h.Set("Strict-Transport-Security", m.buildHSTS())
	}

	// X-Permitted-Cross-Domain-Policies (Adobe Flash/PDF)
	if m.config.XPermittedCrossDomainPolicies != "" {
		h.Set("X-Permitted-Cross-Domain-Policies", m.config.XPermittedCrossDomainPolicies)
	}

	// X-Download-Options (IE download protection)
	if m.config.XDownloadOptions != "" {
		h.Set("X-Download-Options", m.config.XDownloadOptions)
	}

	// X-DNS-Prefetch-Control
	if m.config.XDNSPrefetchControl != "" {
		h.Set("X-DNS-Prefetch-Control", m.config.XDNSPrefetchControl)
	}

	// Permissions-Policy (formerly Feature-Policy)
	if m.config.PermissionsPolicy != "" {
		h.Set("Permissions-Policy", m.config.PermissionsPolicy)
	}

	// Cross-Origin-Embedder-Policy (COEP)
	if m.config.CrossOriginEmbedderPolicy != "" {
		h.Set("Cross-Origin-Embedder-Policy", m.config.CrossOriginEmbedderPolicy)
	}

	// Cross-Origin-Opener-Policy (COOP)
	if m.config.CrossOriginOpenerPolicy != "" {
		h.Set("Cross-Origin-Opener-Policy", m.config.CrossOriginOpenerPolicy)
	}

	// Cross-Origin-Resource-Policy (CORP)
	if m.config.CrossOriginResourcePolicy != "" {
		h.Set("Cross-Origin-Resource-Policy", m.config.CrossOriginResourcePolicy)
	}

	// Cache-Control override
	if m.config.CacheControl != "" {
		h.Set("Cache-Control", m.config.CacheControl)
	}
}

// buildHSTS builds the Strict-Transport-Security header value.
func (m *Middleware) buildHSTS() string {
	if m.config.StrictTransportPolicy == nil {
		return ""
	}

	parts := []string{
		"max-age=" + strconv.Itoa(m.config.StrictTransportPolicy.MaxAge),
	}

	if m.config.StrictTransportPolicy.IncludeSubdomains {
		parts = append(parts, "includeSubDomains")
	}

	if m.config.StrictTransportPolicy.Preload {
		parts = append(parts, "preload")
	}

	return strings.Join(parts, "; ")
}

// RecommendedConfig returns a recommended secure configuration.
func RecommendedConfig() Config {
	return Config{
		Enabled:             true,
		XFrameOptions:       "DENY",
		XContentTypeOptions: true,
		XXSSProtection:      "1; mode=block",
		ReferrerPolicy:      "strict-origin-when-cross-origin",
		StrictTransportPolicy: &HSTSConfig{
			MaxAge:            31536000, // 1 year
			IncludeSubdomains: true,
			Preload:           false,
		},
		XDownloadOptions:          "noopen",
		XDNSPrefetchControl:       "off",
		CrossOriginEmbedderPolicy: "require-corp",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "cross-origin",
	}
}

// PermissiveConfig returns a more permissive configuration for development.
func PermissiveConfig() Config {
	return Config{
		Enabled:                 true,
		XFrameOptions:           "SAMEORIGIN",
		XContentTypeOptions:     true,
		XXSSProtection:          "1; mode=block",
		ReferrerPolicy:          "no-referrer-when-downgrade",
		XDownloadOptions:        "noopen",
		XDNSPrefetchControl:     "off",
		CrossOriginOpenerPolicy: "same-origin-allow-popups",
	}
}
