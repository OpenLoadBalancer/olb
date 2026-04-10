package admin

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	// Basic auth
	Username string
	Password string // bcrypt hashed

	// Bearer token auth
	BearerTokens []string // API keys

	// Options
	RequireAuthForRead bool

	// Auth failure rate limiter (initialized lazily)
	failureLimiter *authFailureLimiter
}

// authFailureLimiter tracks per-IP authentication failures and locks out
// IPs that exceed the threshold within the lockout window.
type authFailureLimiter struct {
	mu      sync.Mutex
	entries map[string]*authFailureEntry
	stopCh  chan struct{}
	stopped bool
}

const (
	authFailureMaxAttempts = 5               // failures before lockout
	authFailureLockout     = 5 * time.Minute // lockout duration
	authFailureCleanup     = 1 * time.Minute // cleanup interval
)

type authFailureEntry struct {
	count       int
	lockedUntil time.Time
}

func newAuthFailureLimiter() *authFailureLimiter {
	l := &authFailureLimiter{
		entries: make(map[string]*authFailureEntry),
		stopCh:  make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

func (l *authFailureLimiter) isLocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		return false
	}
	if e.lockedUntil.IsZero() {
		// Not locked out yet (hasn't reached failure threshold)
		return false
	}
	if time.Now().Before(e.lockedUntil) {
		return true
	}
	// Lockout expired, clear
	delete(l.entries, ip)
	return false
}

func (l *authFailureLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		e = &authFailureEntry{}
		l.entries[ip] = e
	}
	e.count++
	if e.count >= authFailureMaxAttempts {
		e.lockedUntil = time.Now().Add(authFailureLockout)
	}
}

func (l *authFailureLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, ip)
}

func (l *authFailureLimiter) cleanupLoop() {
	ticker := time.NewTicker(authFailureCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for ip, e := range l.entries {
				if now.After(e.lockedUntil) {
					delete(l.entries, ip)
				}
			}
			l.mu.Unlock()
		}
	}
}

func (l *authFailureLimiter) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.stopped {
		close(l.stopCh)
		l.stopped = true
	}
}

// Close stops the auth failure limiter's background goroutine.
func (c *AuthConfig) Close() {
	if c.failureLimiter != nil {
		c.failureLimiter.stop()
	}
}

// AuthMiddleware creates authentication middleware.
// By default, all endpoints require authentication. When RequireAuthForRead
// is explicitly false, GET requests to public health endpoints are allowed
// without authentication for load balancer health probes.
func AuthMiddleware(config *AuthConfig) func(http.Handler) http.Handler {
	// Initialize the failure limiter
	if config.failureLimiter == nil {
		config.failureLimiter = newAuthFailureLimiter()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When RequireAuthForRead is false, allow unauthenticated GET to
			// public health endpoints only. All other GET requests still require auth.
			if !config.RequireAuthForRead && r.Method == http.MethodGet && isPublicHealthEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract client IP for rate limiting
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)

			// Check if IP is locked out due to too many failures
			if config.failureLimiter.isLocked(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "300")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(ErrorResponse("TOO_MANY_FAILURES", "too many auth failures, try again later"))
				return
			}

			// Check for authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				config.failureLimiter.recordFailure(ip)
				config.writeUnauthorized(w, "missing authorization header")
				return
			}

			// Try bearer token auth first
			if token, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
				if config.validateBearerToken(token) {
					config.failureLimiter.recordSuccess(ip)
					next.ServeHTTP(w, r)
					return
				}
				config.failureLimiter.recordFailure(ip)
				config.writeUnauthorized(w, "invalid bearer token")
				return
			}

			// Try basic auth
			if encoded, ok := strings.CutPrefix(authHeader, "Basic "); ok {
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					config.failureLimiter.recordFailure(ip)
					config.writeUnauthorized(w, "invalid basic auth encoding")
					return
				}

				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) != 2 {
					config.failureLimiter.recordFailure(ip)
					config.writeUnauthorized(w, "invalid basic auth format")
					return
				}

				username, password := parts[0], parts[1]
				if config.validateBasicAuth(username, password) {
					config.failureLimiter.recordSuccess(ip)
					next.ServeHTTP(w, r)
					return
				}
				config.failureLimiter.recordFailure(ip)
				config.writeUnauthorized(w, "invalid credentials")
				return
			}

			config.failureLimiter.recordFailure(ip)
			config.writeUnauthorized(w, "unsupported authorization scheme")
		})
	}
}

// validateBasicAuth validates username and password against configured credentials.
func (c *AuthConfig) validateBasicAuth(username, password string) bool {
	// Constant-time username comparison
	if subtle.ConstantTimeCompare([]byte(username), []byte(c.Username)) != 1 {
		return false
	}

	// Verify bcrypt password
	err := bcrypt.CompareHashAndPassword([]byte(c.Password), []byte(password))
	return err == nil
}

// validateBearerToken validates a bearer token against configured tokens.
func (c *AuthConfig) validateBearerToken(token string) bool {
	for _, validToken := range c.BearerTokens {
		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(token), []byte(validToken)) == 1 {
			return true
		}
	}
	return false
}

// writeUnauthorized writes a 401 Unauthorized response.
func (c *AuthConfig) writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	resp := ErrorResponse("UNAUTHORIZED", message)
	json.NewEncoder(w).Encode(resp)
}

// HashPassword generates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash compares a password with a bcrypt hash.
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// isPublicHealthEndpoint returns true for endpoints that are safe to expose
// without authentication (used for load balancer health probes).
func isPublicHealthEndpoint(path string) bool {
	switch path {
	case "/health", "/api/v1/system/health", "/api/v1/health":
		return true
	default:
		return false
	}
}
