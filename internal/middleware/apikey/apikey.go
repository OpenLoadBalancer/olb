// Package apikey provides API Key authentication middleware.
package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Config configures API Key Authentication.
type Config struct {
	Enabled      bool               // Enable API Key Auth
	Keys         map[string]string  // key_id -> api_key (hashed or plain)
	Header       string             // Header name (default: "X-API-Key")
	ExcludePaths []string           // Paths to exclude
	Hash         string             // Key hash type: "sha256", "plain"
	KeyMetadata  map[string]KeyInfo // key_id -> metadata (name, permissions, etc.)
}

// KeyInfo contains metadata about an API key.
type KeyInfo struct {
	Name        string            `json:"name"`
	Permissions []string          `json:"permissions"`
	Metadata    map[string]string `json:"metadata"`
}

// DefaultConfig returns default API Key configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		Keys:        make(map[string]string),
		Header:      "X-API-Key",
		Hash:        "sha256",
		KeyMetadata: make(map[string]KeyInfo),
	}
}

// Middleware provides API Key Authentication.
type Middleware struct {
	config Config
	hashes map[string][]byte // Pre-computed key hashes
	mu     sync.RWMutex
}

// New creates a new API Key middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config: config,
		hashes: make(map[string][]byte),
	}

	// Pre-compute hashes for faster comparison
	for keyID, key := range config.Keys {
		switch config.Hash {
		case "sha256":
			mac := hmac.New(sha256.New, []byte(key))
			mac.Write([]byte(keyID))
			m.hashes[keyID] = mac.Sum(nil)
		case "plain":
			m.hashes[keyID] = []byte(key)
		default:
			return nil, fmt.Errorf("unsupported hash algorithm %q (supported: sha256, plain)", config.Hash)
		}
	}

	return m, nil
}

// ZeroSecrets clears pre-computed key hashes from memory.
// Call this during shutdown to reduce the window where secrets are in memory.
func (m *Middleware) ZeroSecrets() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for keyID, hash := range m.hashes {
		for i := range hash {
			hash[i] = 0
		}
		delete(m.hashes, keyID)
	}
}

func (m *Middleware) Name() string {
	return "api_key"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 215 // After JWT (210), before Basic Auth (220)
}

// Wrap wraps the handler with API Key authentication.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	header := m.config.Header
	if header == "" {
		header = "X-API-Key"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) && (len(r.URL.Path) == len(path) || r.URL.Path[len(path)] == '/' || path[len(path)-1] == '/') {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract API key from header
		apiKey := extractAPIKey(r, header)
		if apiKey == "" {
			m.unauthorized(w, "API key required")
			return
		}

		// Validate key
		keyID, valid := m.validateKey(apiKey)
		if !valid {
			m.unauthorized(w, "invalid API key")
			return
		}

		// Add key info to context for downstream use
		ctx := r.Context()
		ctx = WithAPIKeyID(ctx, keyID)

		// Add metadata if available
		if meta, ok := m.config.KeyMetadata[keyID]; ok {
			ctx = WithKeyInfo(ctx, meta)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractAPIKey extracts API key from the request header.
func extractAPIKey(r *http.Request, header string) string {
	if header != "" {
		return r.Header.Get(header)
	}
	return ""
}

// validateKey validates the API key and returns the key ID if valid.
func (m *Middleware) validateKey(apiKey string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch m.config.Hash {
	case "sha256":
		for keyID, expectedHash := range m.hashes {
			mac := hmac.New(sha256.New, []byte(apiKey))
			mac.Write([]byte(keyID))
			if subtle.ConstantTimeCompare(mac.Sum(nil), expectedHash) == 1 {
				return keyID, true
			}
		}
	case "plain":
		for keyID, expectedKey := range m.hashes {
			if subtle.ConstantTimeCompare([]byte(apiKey), expectedKey) == 1 {
				return keyID, true
			}
		}
	default:
		// Should never reach here — New() rejects unknown hash types
		return "", false
	}

	return "", false
}

// unauthorized writes unauthorized response.
func (m *Middleware) unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "unauthorized",
		"message": message,
	})
}

// contextKey is the key for API key info in context.
type contextKey int

const (
	apiKeyIDKey contextKey = 0
	keyInfoKey  contextKey = 1
)

// WithAPIKeyID adds API key ID to context.
func WithAPIKeyID(ctx context.Context, keyID string) context.Context {
	return context.WithValue(ctx, apiKeyIDKey, keyID)
}

// GetAPIKeyID retrieves API key ID from context.
func GetAPIKeyID(ctx context.Context) string {
	if keyID, ok := ctx.Value(apiKeyIDKey).(string); ok {
		return keyID
	}
	return ""
}

// WithKeyInfo adds key metadata to context.
func WithKeyInfo(ctx context.Context, info KeyInfo) context.Context {
	return context.WithValue(ctx, keyInfoKey, info)
}

// GetKeyInfo retrieves key metadata from context.
func GetKeyInfo(ctx context.Context) KeyInfo {
	if info, ok := ctx.Value(keyInfoKey).(KeyInfo); ok {
		return info
	}
	return KeyInfo{}
}

// HasPermission checks if the key has a specific permission.
func HasPermission(ctx context.Context, permission string) bool {
	info := GetKeyInfo(ctx)
	for _, p := range info.Permissions {
		if p == permission || p == "*" {
			return true
		}
	}
	return false
}
