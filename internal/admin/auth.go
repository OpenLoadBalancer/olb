package admin

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Role constants for RBAC.
const (
	RoleAdmin    = "admin"
	RoleReadOnly = "readonly"
)

// roleContextKey is the context key for storing the authenticated user's role.
type roleContextKey struct{}

// RoleFromContext extracts the role string from the request context.
// Returns RoleAdmin if no role is set (backward compatible default).
func RoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(roleContextKey{}).(string)
	if role == "" {
		return RoleAdmin
	}
	return role
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	// Basic auth
	Username string
	Password string // bcrypt hashed

	// Bearer token auth
	BearerTokens []string // API keys

	// BearerRoles maps bearer tokens to their RBAC role.
	// Tokens not in this map default to RoleAdmin for backward compatibility.
	// When nil or empty, all authenticated users have the admin role.
	BearerRoles map[string]string

	// Options
	RequireAuthForRead bool

	// Auth failure rate limiter (initialized lazily)
	failureLimiter *authFailureLimiter

	// tokenMu protects BearerTokens and BearerRoles for safe rotation at runtime.
	tokenMu sync.RWMutex
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
	maxAuthEntries         = 100_000         // cap on tracked IPs to prevent memory exhaustion
)

type authFailureEntry struct {
	count       int
	lockedUntil time.Time
	lastAccess  time.Time
}

func newAuthFailureLimiter() *authFailureLimiter {
	l := &authFailureLimiter{
		entries: make(map[string]*authFailureEntry),
		stopCh:  make(chan struct{}),
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[auth] panic recovered in cleanup: %v", r)
			}
		}()
		l.cleanupLoop()
	}()
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

// getClientIP extracts the real client IP from the request, respecting
// X-Real-Ip and X-Forwarded-For headers when the admin server is behind
// a reverse proxy. Falls back to r.RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Real-Ip first (set by nginx and similar proxies)
	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		if ip := net.ParseIP(realIP); ip != nil {
			return realIP
		}
	}
	// Check X-Forwarded-For (set by many proxies; first entry is the client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Fallback to RemoteAddr (host:port — strip port)
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func (l *authFailureLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		if len(l.entries) >= maxAuthEntries {
			now := time.Now()
			// First pass: evict expired (lockout elapsed) entries
			for k, v := range l.entries {
				if !v.lockedUntil.IsZero() && now.After(v.lockedUntil) {
					delete(l.entries, k)
				}
			}
			// Second pass: if still full, evict the oldest non-locked entry (LRU-style)
			if len(l.entries) >= maxAuthEntries {
				var oldestIP string
				var oldestTime time.Time
				for k, v := range l.entries {
					// Skip currently locked entries — they must remain tracked
					if !v.lockedUntil.IsZero() && now.Before(v.lockedUntil) {
						continue
					}
					if oldestIP == "" || v.lastAccess.Before(oldestTime) {
						oldestIP = k
						oldestTime = v.lastAccess
					}
				}
				if oldestIP != "" {
					delete(l.entries, oldestIP)
				}
				// If all entries are locked, we cannot evict safely;
				// still allow the new entry so the IP is tracked.
			}
		}
		e = &authFailureEntry{
			lastAccess: time.Now(),
		}
		l.entries[ip] = e
	}
	e.lastAccess = time.Now()
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

// ZeroSecrets clears sensitive fields (bearer tokens) from memory.
// Note: Go strings are immutable and may be copied by the runtime, so
// this provides best-effort scrubbing for the slice-backed data only.
func (c *AuthConfig) ZeroSecrets() {
	for i := range c.BearerTokens {
		// Strings in Go are immutable and their backing memory is shared;
		// we cannot reliably zero them. Clear the slice reference instead.
		c.BearerTokens[i] = ""
	}
	c.BearerTokens = nil
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
			ip := getClientIP(r)

			// Check if IP is locked out due to too many failures
			if config.failureLimiter.isLocked(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "300")
				w.WriteHeader(http.StatusTooManyRequests)
				if err := json.NewEncoder(w).Encode(ErrorResponse("TOO_MANY_FAILURES", "too many auth failures, try again later")); err != nil {
					log.Printf("admin: failed to encode lockout response: %v", err)
				}
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
					role := config.bearerRole(token)
					ctx := context.WithValue(r.Context(), roleContextKey{}, role)
					next.ServeHTTP(w, r.WithContext(ctx))
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
					// Basic auth users always get admin role (backward compatible)
					ctx := context.WithValue(r.Context(), roleContextKey{}, RoleAdmin)
					next.ServeHTTP(w, r.WithContext(ctx))
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
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	for _, validToken := range c.BearerTokens {
		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(token), []byte(validToken)) == 1 {
			return true
		}
	}
	return false
}

// bearerRole returns the RBAC role for the given bearer token.
// If the token has an explicit role in BearerRoles, that role is returned.
// Otherwise, RoleAdmin is returned for backward compatibility.
func (c *AuthConfig) bearerRole(token string) string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	if c.BearerRoles != nil {
		if role, ok := c.BearerRoles[token]; ok {
			return role
		}
	}
	return RoleAdmin
}

// RotateBearerToken replaces an existing bearer token with a new one.
// It validates that oldToken exists in the current token list, then replaces
// it with newToken. This is thread-safe and can be called at runtime without
// restarting the server. Returns an error if oldToken is not found.
func (c *AuthConfig) RotateBearerToken(oldToken, newToken string) error {
	if oldToken == "" || newToken == "" {
		return fmt.Errorf("old and new tokens must not be empty")
	}

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Find and replace the old token
	found := false
	for i, t := range c.BearerTokens {
		if subtle.ConstantTimeCompare([]byte(t), []byte(oldToken)) == 1 {
			// Preserve any role mapping for the old token
			if c.BearerRoles != nil {
				if role, ok := c.BearerRoles[oldToken]; ok {
					delete(c.BearerRoles, oldToken)
					c.BearerRoles[newToken] = role
				}
			}
			c.BearerTokens[i] = newToken
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("old token not found in bearer token list")
	}

	log.Printf("[auth] bearer token rotated successfully")
	return nil
}

// writeUnauthorized writes a 401 Unauthorized response.
func (c *AuthConfig) writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	resp := ErrorResponse("UNAUTHORIZED", message)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("admin: failed to encode unauthorized response: %v", err)
	}
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
// Only the simple /health up/down probe is public; the /api/v1/... variants
// expose detailed operational state and always require authentication.
func isPublicHealthEndpoint(path string) bool {
	return path == "/health"
}
