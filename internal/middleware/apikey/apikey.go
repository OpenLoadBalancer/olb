// Package apikey provides API Key authentication middleware.
package apikey

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
)

// Config configures API Key Authentication.
type Config struct {
	Enabled      bool               // Enable API Key Auth
	Keys         map[string]string  // key_id -> api_key (hashed or plain)
	Header       string             // Header name (default: "X-API-Key")
	QueryParam   string             // Query parameter name (alternative to header)
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
		QueryParam:  "api_key",
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
			// For API keys, we hash the entire key for lookup
			// and store the key_id for metadata retrieval
			h := sha256.Sum256([]byte(key))
			m.hashes[keyID] = h[:]
		case "plain":
			m.hashes[keyID] = []byte(key)
		default:
			m.hashes[keyID] = []byte(key)
		}
	}

	return m, nil
}

// Name returns the middleware name.
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
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract API key
		apiKey := extractAPIKey(r, header, m.config.QueryParam)
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

// extractAPIKey extracts API key from header or query parameter.
func extractAPIKey(r *http.Request, header, queryParam string) string {
	// Try header first
	if header != "" {
		key := r.Header.Get(header)
		if key != "" {
			return key
		}
	}

	// Fall back to query parameter
	if queryParam != "" {
		key := r.URL.Query().Get(queryParam)
		if key != "" {
			return key
		}
	}

	return ""
}

// validateKey validates the API key and returns the key ID if valid.
func (m *Middleware) validateKey(apiKey string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch m.config.Hash {
	case "sha256":
		h := sha256.Sum256([]byte(apiKey))
		for keyID, expectedHash := range m.hashes {
			if subtle.ConstantTimeCompare(h[:], expectedHash) == 1 {
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
		for keyID, expectedKey := range m.hashes {
			if subtle.ConstantTimeCompare([]byte(apiKey), expectedKey) == 1 {
				return keyID, true
			}
		}
	}

	return "", false
}

// unauthorized writes unauthorized response.
func (m *Middleware) unauthorized(w http.ResponseWriter, message string) {
	http.Error(w, `{"error":"unauthorized","message":"`+message+`"}`, http.StatusUnauthorized)
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
