// Package forcessl provides HTTPS enforcement middleware.
package forcessl

import (
	"net/http"
	"strconv"
	"strings"
)

// Config configures HTTPS enforcement.
type Config struct {
	Enabled      bool     // Enable HTTPS enforcement
	Permanent    bool     // Use 301 (permanent) instead of 307 (temporary)
	ExcludePaths []string // Paths to exclude (e.g., health checks)
	ExcludeHosts []string // Hosts to exclude from redirect
	Port         int      // HTTPS port (default: 443)
	HeaderKey    string   // Header to check for TLS termination (e.g., X-Forwarded-Proto)
	HeaderValue  string   // Expected header value for TLS (e.g., https)
}

// DefaultConfig returns default HTTPS enforcement configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		Permanent:   true,
		Port:        443,
		HeaderKey:   "X-Forwarded-Proto",
		HeaderValue: "https",
	}
}

// Middleware provides HTTPS enforcement functionality.
type Middleware struct {
	config Config
}

// New creates a new Force SSL middleware.
func New(config Config) *Middleware {
	if config.Port == 0 {
		config.Port = 443
	}
	if config.HeaderKey == "" {
		config.HeaderKey = "X-Forwarded-Proto"
	}
	return &Middleware{config: config}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "forcessl"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 70 // Very early, after Metrics/Logging but before auth
}

// Wrap wraps the handler with HTTPS enforcement.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check excluded hosts
		for _, host := range m.config.ExcludeHosts {
			if r.Host == host || strings.HasPrefix(r.Host, host+":") {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check if request is already HTTPS
		if m.isHTTPS(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Build HTTPS URL
		target := m.buildHTTPSURL(r)

		// Redirect
		if m.config.Permanent {
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		} else {
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		}
	})
}

// isHTTPS checks if the request is using HTTPS.
func (m *Middleware) isHTTPS(r *http.Request) bool {
	// Check TLS directly
	if r.TLS != nil {
		return true
	}

	// Check header (for TLS termination at load balancer)
	if m.config.HeaderKey != "" {
		value := r.Header.Get(m.config.HeaderKey)
		if strings.EqualFold(value, m.config.HeaderValue) {
			return true
		}
	}

	// Check common forwarded proto headers
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	if scheme := r.Header.Get("X-Scheme"); scheme == "https" {
		return true
	}

	return false
}

// buildHTTPSURL builds the HTTPS redirect URL.
func (m *Middleware) buildHTTPSURL(r *http.Request) string {
	// Get host without port
	host := r.Host
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		host = host[:colonIdx]
	}

	// Build URL
	var target string
	if m.config.Port == 443 {
		target = "https://" + host + r.URL.Path
	} else {
		target = "https://" + host + ":" + strconv.Itoa(m.config.Port) + r.URL.Path
	}

	// Preserve query string
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	return target
}

// IsSecureRequest checks if a request is secure (helper function).
func IsSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	if scheme := r.Header.Get("X-Scheme"); scheme == "https" {
		return true
	}
	return false
}
