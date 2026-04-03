package admin

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

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
}

// AuthMiddleware creates authentication middleware.
// By default, all endpoints require authentication. When RequireAuthForRead
// is explicitly false, GET requests to public health endpoints are allowed
// without authentication for load balancer health probes.
func AuthMiddleware(config *AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When RequireAuthForRead is false, allow unauthenticated GET to
			// public health endpoints only. All other GET requests still require auth.
			if !config.RequireAuthForRead && r.Method == http.MethodGet && isPublicHealthEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Check for authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				config.writeUnauthorized(w, "missing authorization header")
				return
			}

			// Try bearer token auth first
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				if config.validateBearerToken(token) {
					next.ServeHTTP(w, r)
					return
				}
				config.writeUnauthorized(w, "invalid bearer token")
				return
			}

			// Try basic auth
			if strings.HasPrefix(authHeader, "Basic ") {
				encoded := strings.TrimPrefix(authHeader, "Basic ")
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					config.writeUnauthorized(w, "invalid basic auth encoding")
					return
				}

				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) != 2 {
					config.writeUnauthorized(w, "invalid basic auth format")
					return
				}

				username, password := parts[0], parts[1]
				if config.validateBasicAuth(username, password) {
					next.ServeHTTP(w, r)
					return
				}
				config.writeUnauthorized(w, "invalid credentials")
				return
			}

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
