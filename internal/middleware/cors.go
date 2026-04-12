// Package middleware provides HTTP middleware components for OpenLoadBalancer.
package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	AllowedOrigins   []string      // ["https://example.com", "*"] or ["*"] for all
	AllowedMethods   []string      // ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
	AllowedHeaders   []string      // ["Content-Type", "Authorization"]
	ExposedHeaders   []string      // headers to expose to client
	AllowCredentials bool          // allow cookies/auth headers
	MaxAge           time.Duration // preflight cache duration
}

// CORSMiddleware handles Cross-Origin Resource Sharing (CORS) headers.
type CORSMiddleware struct {
	config          CORSConfig
	allowAllOrigins bool
	allowedOrigins  map[string]bool
	allowedHeaders  map[string]bool
	allowedMethods  map[string]bool
	// Pre-computed joined strings to avoid per-request strings.Join allocations.
	allowedMethodsStr  string
	allowedHeadersStr  string
	exposedHeadersStr  string
	maxAgeStr          string
}

// NewCORSMiddleware creates a new CORS middleware.
func NewCORSMiddleware(config CORSConfig) *CORSMiddleware {
	m := &CORSMiddleware{
		config:         config,
		allowedOrigins: make(map[string]bool),
		allowedHeaders: make(map[string]bool),
		allowedMethods: make(map[string]bool),
	}

	// Process allowed origins
	// Reject wildcard origins when credentials are enabled to prevent
	// effectively disabling same-origin policy for credentialed requests.
	if config.AllowCredentials {
		for _, origin := range config.AllowedOrigins {
			if origin == "*" {
				panic(fmt.Sprintf("CORS misconfiguration: AllowedOrigins cannot contain '*' when AllowCredentials is true — specify explicit origins instead"))
			}
		}
	}
	for _, origin := range config.AllowedOrigins {
		if origin == "*" {
			m.allowAllOrigins = true
			break
		}
		m.allowedOrigins[strings.ToLower(origin)] = true
	}

	// Process allowed headers (case-insensitive storage)
	for _, header := range config.AllowedHeaders {
		m.allowedHeaders[http.CanonicalHeaderKey(header)] = true
	}

	// Process allowed methods (uppercase)
	for _, method := range config.AllowedMethods {
		m.allowedMethods[strings.ToUpper(method)] = true
	}

	// Pre-compute joined strings for response headers (config is immutable after construction)
	if len(config.AllowedMethods) > 0 {
		m.allowedMethodsStr = strings.Join(config.AllowedMethods, ", ")
	}
	if len(config.AllowedHeaders) > 0 {
		m.allowedHeadersStr = strings.Join(config.AllowedHeaders, ", ")
	}
	if len(config.ExposedHeaders) > 0 {
		m.exposedHeadersStr = strings.Join(config.ExposedHeaders, ", ")
	}
	if config.MaxAge > 0 {
		m.maxAgeStr = strconv.Itoa(int(config.MaxAge.Seconds()))
	}

	return m
}

// Name returns the middleware name.
func (m *CORSMiddleware) Name() string {
	return "cors"
}

// Priority returns the middleware priority.
func (m *CORSMiddleware) Priority() int {
	return PriorityCORS
}

// Wrap wraps the next handler with CORS functionality.
func (m *CORSMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a preflight request
		if r.Method == http.MethodOptions {
			m.handlePreflight(w, r)
			return
		}

		// Handle actual request
		m.handleActual(w, r, next)
	})
}

// isOriginAllowed checks if the given origin is allowed.
func (m *CORSMiddleware) isOriginAllowed(origin string) bool {
	if m.allowAllOrigins {
		return true
	}
	return m.allowedOrigins[strings.ToLower(origin)]
}

// handlePreflight handles CORS preflight (OPTIONS) requests.
func (m *CORSMiddleware) handlePreflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")

	// Always set Vary: Origin for proper caching
	w.Header().Add("Vary", "Origin")

	// Check if origin is allowed
	if !m.isOriginAllowed(origin) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Set Access-Control-Allow-Origin
	// Note: When AllowCredentials is true, we cannot use wildcard
	if m.allowAllOrigins && !m.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}

	// Set Allow-Credentials
	if m.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	// Set allowed methods
	if len(m.config.AllowedMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", m.allowedMethodsStr)
	}

	// Set allowed headers
	// Check for Access-Control-Request-Headers
	requestedHeaders := r.Header.Get("Access-Control-Request-Headers")
	if requestedHeaders != "" {
		// If specific headers were requested, validate them against our allowed list
		requested := strings.Split(requestedHeaders, ",")
		var allowed []string
		for _, h := range requested {
			h = strings.TrimSpace(h)
			if m.allowedHeaders[http.CanonicalHeaderKey(h)] || m.allowAllOrigins {
				allowed = append(allowed, h)
			}
		}
		if len(allowed) > 0 {
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(allowed, ", "))
		}
	} else if len(m.config.AllowedHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", m.allowedHeadersStr)
	}

	// Set max age
	if m.config.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", m.maxAgeStr)
	}

	// Return 204 No Content for preflight
	w.WriteHeader(http.StatusNoContent)
}

// handleActual handles actual (non-preflight) CORS requests.
func (m *CORSMiddleware) handleActual(w http.ResponseWriter, r *http.Request, next http.Handler) {
	origin := r.Header.Get("Origin")

	// Always set Vary: Origin for proper caching
	w.Header().Add("Vary", "Origin")

	// Check if origin is allowed
	if !m.isOriginAllowed(origin) {
		next.ServeHTTP(w, r)
		return
	}

	// Set Access-Control-Allow-Origin
	// Note: When AllowCredentials is true, we cannot use wildcard
	if m.allowAllOrigins && !m.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}

	// Set Allow-Credentials
	if m.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	// Set exposed headers
	if len(m.config.ExposedHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", m.exposedHeadersStr)
	}

	next.ServeHTTP(w, r)
}
