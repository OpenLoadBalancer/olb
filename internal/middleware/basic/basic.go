// Package basic provides HTTP Basic Authentication middleware.
package basic

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Config configures Basic Authentication.
type Config struct {
	Enabled        bool
	AllowPlaintext bool              // Allow plaintext passwords (not recommended)              // Enable Basic Auth
	Users          map[string]string // username -> bcrypt-hashed password or plain (for testing)
	Realm          string            // Auth realm (default: "Restricted")
	ExcludePaths   []string          // Paths to exclude
	Hash           string            // Password hash type: "bcrypt", "sha256", "plain"
}

// DefaultConfig returns default Basic Auth configuration.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Realm:   "Restricted",
		Users:   make(map[string]string),
		Hash:    "bcrypt",
	}
}

// Middleware provides Basic Authentication.
type Middleware struct {
	config Config
	hashes map[string][]byte // Pre-computed password hashes
	mu     sync.RWMutex
}

// New creates a new Basic Auth middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config: config,
		hashes: make(map[string][]byte),
	}

	// Pre-compute hashes for faster comparison
	for user, pass := range config.Users {
		switch config.Hash {
		case "sha256":
			h := sha256.Sum256([]byte(pass))
			m.hashes[user] = h[:]
		case "plain":
			if !config.AllowPlaintext {
				return nil, fmt.Errorf("plaintext passwords rejected: use sha256 or bcrypt, or set allow_plaintext: true")
			}
			log.Printf("WARNING: basic auth user %q uses plaintext password storage — use 'sha256' or 'bcrypt' hash instead", user)
			m.hashes[user] = []byte(pass)
		case "bcrypt":
			// Bcrypt passwords are already hashed; store the bcrypt hash as-is for runtime comparison
			m.hashes[user] = []byte(pass)
		default:
			return nil, fmt.Errorf("unsupported hash algorithm %q (supported: sha256, bcrypt, plain)", config.Hash)
		}
	}

	return m, nil
}

// ZeroSecrets clears pre-computed password hashes from memory.
// Call this during shutdown to reduce the window where secrets are in memory.
func (m *Middleware) ZeroSecrets() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for user, hash := range m.hashes {
		for i := range hash {
			hash[i] = 0
		}
		delete(m.hashes, user)
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "basic_auth"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 220 // After JWT (210), before Real IP (300)
}

// Wrap wraps the handler with Basic Authentication.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	realm := m.config.Realm
	if realm == "" {
		realm = "Restricted"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract credentials
		username, password, ok := extractCredentials(r)
		if !ok {
			m.unauthorized(w, realm)
			return
		}

		// Validate credentials
		if !m.validateCredentials(username, password) {
			m.unauthorized(w, realm)
			return
		}

		// Add username to context for downstream use
		ctx := WithUsername(r.Context(), username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractCredentials extracts username and password from Authorization header.
func extractCredentials(r *http.Request) (username, password string, ok bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", "", false
	}

	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return "", "", false
	}

	encoded := strings.TrimPrefix(auth, prefix)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}

	// credentials are in format "username:password"
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// validateCredentials validates username and password.
func (m *Middleware) validateCredentials(username, password string) bool {
	m.mu.RLock()
	expectedHash, exists := m.hashes[username]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	switch m.config.Hash {
	case "sha256":
		h := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(h[:], expectedHash) == 1
	case "plain":
		return subtle.ConstantTimeCompare([]byte(password), expectedHash) == 1
	case "bcrypt":
		return bcrypt.CompareHashAndPassword(expectedHash, []byte(password)) == nil
	default:
		// Should never reach here — New() rejects unknown hash types
		return false
	}
}

// unauthorized writes unauthorized response.
func (m *Middleware) unauthorized(w http.ResponseWriter, realm string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
	http.Error(w, `{"error":"unauthorized","message":"authentication required"}`, http.StatusUnauthorized)
}

// contextKey is the key for username in context.
type contextKey int

const usernameKey contextKey = 0

// WithUsername adds username to context.
func WithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameKey, username)
}

// GetUsername retrieves username from context.
func GetUsername(ctx context.Context) string {
	if username, ok := ctx.Value(usernameKey).(string); ok {
		return username
	}
	return ""
}
