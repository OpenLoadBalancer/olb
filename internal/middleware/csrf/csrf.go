// Package csrf provides Cross-Site Request Forgery protection middleware.
package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// Config configures CSRF protection.
type Config struct {
	Enabled        bool     // Enable CSRF protection
	CookieName     string   // CSRF cookie name (default: csrf_token)
	HeaderName     string   // Header to check for token (default: X-CSRF-Token)
	FieldName      string   // Form field name (default: csrf_token)
	ExcludePaths   []string // Paths to exclude from CSRF check
	ExcludeMethods []string // Methods that don't require CSRF (default: GET, HEAD, OPTIONS, TRACE)
	CookiePath     string   // Cookie path (default: /)
	CookieDomain   string   // Cookie domain
	CookieMaxAge   int      // Cookie max age in seconds (default: 86400)
	CookieSecure   bool     // Secure cookie flag
	CookieHTTPOnly bool     // HTTPOnly cookie flag (default: true)
	TokenLength    int      // Token length in bytes (default: 32)
}

// DefaultConfig returns default CSRF configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		CookieName:     "csrf_token",
		HeaderName:     "X-CSRF-Token",
		FieldName:      "csrf_token",
		ExcludeMethods: []string{"GET", "HEAD", "OPTIONS", "TRACE"},
		CookiePath:     "/",
		CookieMaxAge:   86400,
		CookieSecure:   true,
		CookieHTTPOnly: true,
		TokenLength:    32,
	}
}

// Middleware provides CSRF protection functionality.
type Middleware struct {
	config Config
}

// New creates a new CSRF middleware.
func New(config Config) (*Middleware, error) {
	if config.CookieName == "" {
		config.CookieName = "csrf_token"
	}
	if config.HeaderName == "" {
		config.HeaderName = "X-CSRF-Token"
	}
	if config.FieldName == "" {
		config.FieldName = "csrf_token"
	}
	if config.CookiePath == "" {
		config.CookiePath = "/"
	}
	if config.TokenLength == 0 {
		config.TokenLength = 32
	}
	if len(config.ExcludeMethods) == 0 {
		config.ExcludeMethods = []string{"GET", "HEAD", "OPTIONS", "TRACE"}
	}

	return &Middleware{config: config}, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "csrf"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 200
}

// Wrap wraps the handler with CSRF protection.
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

		// Check if method requires CSRF validation
		if !m.requiresValidation(r.Method) {
			m.setToken(w, r)
			next.ServeHTTP(w, r)
			return
		}

		// Get the expected token from cookie
		cookie, err := r.Cookie(m.config.CookieName)
		if err != nil || cookie.Value == "" {
			m.handleError(w, r, "CSRF token missing from cookie")
			return
		}
		expectedToken := cookie.Value

		// Get the provided token from header or form
		providedToken := m.extractToken(r)
		if providedToken == "" {
			m.handleError(w, r, "CSRF token missing from request")
			return
		}

		// Validate token using constant-time comparison
		if !m.compareTokens(expectedToken, providedToken) {
			m.handleError(w, r, "CSRF token mismatch")
			return
		}

		// Rotate token after successful validation to limit exposure window
		m.rotateToken(w)
		next.ServeHTTP(w, r)
	})
}

// requiresValidation checks if the HTTP method requires CSRF validation.
func (m *Middleware) requiresValidation(method string) bool {
	upperMethod := strings.ToUpper(method)
	for _, excluded := range m.config.ExcludeMethods {
		if upperMethod == strings.ToUpper(excluded) {
			return false
		}
	}
	return true
}

// extractToken extracts the CSRF token from the request (header or form).
func (m *Middleware) extractToken(r *http.Request) string {
	// Check header first
	token := r.Header.Get(m.config.HeaderName)
	if token != "" {
		return token
	}

	// Check form value
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		if r.FormValue(m.config.FieldName) != "" {
			return r.FormValue(m.config.FieldName)
		}
	}

	return ""
}

// compareTokens compares two tokens using constant-time comparison.
func (m *Middleware) compareTokens(a, b string) bool {
	aBytes, errA := base64.URLEncoding.DecodeString(a)
	bBytes, errB := base64.URLEncoding.DecodeString(b)

	if errA != nil || errB != nil {
		return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
	}

	return subtle.ConstantTimeCompare(aBytes, bBytes) == 1
}

// setToken sets or refreshes the CSRF token cookie.
func (m *Middleware) setToken(w http.ResponseWriter, r *http.Request) {
	// Check if token already exists
	cookie, err := r.Cookie(m.config.CookieName)
	if err == nil && cookie.Value != "" {
		return
	}

	// Generate new token
	token, err := generateToken(m.config.TokenLength)
	if err != nil {
		return
	}

	// Set cookie
	cookie = &http.Cookie{
		Name:     m.config.CookieName,
		Value:    token,
		Path:     m.config.CookiePath,
		Domain:   m.config.CookieDomain,
		MaxAge:   m.config.CookieMaxAge,
		Secure:   m.config.CookieSecure,
		HttpOnly: m.config.CookieHTTPOnly,
		SameSite: http.SameSiteStrictMode,
	}

	http.SetCookie(w, cookie)
}

// rotateToken generates a new CSRF token after successful validation
// to limit the window of exposure if a token is compromised.
func (m *Middleware) rotateToken(w http.ResponseWriter) {
	token, err := generateToken(m.config.TokenLength)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.config.CookieName,
		Value:    token,
		Path:     m.config.CookiePath,
		Domain:   m.config.CookieDomain,
		MaxAge:   m.config.CookieMaxAge,
		Secure:   m.config.CookieSecure,
		HttpOnly: m.config.CookieHTTPOnly,
		SameSite: http.SameSiteStrictMode,
	})
}

// handleError handles CSRF validation errors.
func (m *Middleware) handleError(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	resp, _ := json.Marshal(map[string]string{"error": "CSRF validation failed", "message": message})
	_, _ = w.Write(resp)
}

// generateToken generates a random CSRF token.
func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// GetToken retrieves the CSRF token from the request (helper for templates).
func GetToken(r *http.Request) string {
	cookie, err := r.Cookie("csrf_token")
	if err != nil {
		return ""
	}
	return cookie.Value
}
