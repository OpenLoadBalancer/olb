// Package oauth2 provides OAuth2/OIDC authentication middleware.
package oauth2

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config configures OAuth2/OIDC authentication.
type Config struct {
	Enabled          bool     // Enable OAuth2
	IssuerURL        string   // OIDC issuer URL (e.g., "https://accounts.google.com")
	ClientID         string   // OAuth2 client ID
	ClientSecret     string   // OAuth2 client secret (for token introspection)
	JwksURL          string   // JWKS endpoint URL (optional, discovered from issuer)
	Audience         string   // Expected audience
	Scopes           []string // Required scopes
	Header           string   // Authorization header name (default: "Authorization")
	Prefix           string   // Token prefix (default: "Bearer ")
	ExcludePaths     []string // Paths to exclude
	IntrospectionURL string   // Token introspection endpoint (optional)
	CacheDuration    string   // JWKS cache duration (default: "1h")
}

// DefaultConfig returns default OAuth2 configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		Header:        "Authorization",
		Prefix:        "Bearer ",
		CacheDuration: "1h",
	}
}

// TokenInfo represents validated token information.
type TokenInfo struct {
	Subject     string   `json:"sub"`
	Issuer      string   `json:"iss"`
	Audience    []string `json:"aud"`
	Expiration  int64    `json:"exp"`
	IssuedAt    int64    `json:"iat"`
	NotBefore   int64    `json:"nbf"`
	Scope       string   `json:"scope"`
	ClientID    string   `json:"client_id"`
	TokenType   string   `json:"token_type"`
	Active      bool     `json:"active"`
	Permissions []string `json:"permissions"`
}

// Middleware provides OAuth2/OIDC authentication.
type Middleware struct {
	config     Config
	jwksCache  *jwksCache
	httpClient *http.Client
	mu         sync.RWMutex
}

// jwksCache caches JWKS keys.
type jwksCache struct {
	keys      map[string]interface{}
	expiresAt time.Time
	mu        sync.RWMutex
}

// New creates a new OAuth2 middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config: config,
		jwksCache: &jwksCache{
			keys: make(map[string]interface{}),
		},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	return m, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "oauth2"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 212 // After JWT (210), before API Key (215)
}

// Wrap wraps the handler with OAuth2 authentication.
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

		// Extract token
		token, err := m.extractToken(r)
		if err != nil {
			m.unauthorized(w, err.Error())
			return
		}

		// Validate token
		tokenInfo, err := m.validateToken(token)
		if err != nil {
			m.unauthorized(w, "invalid token")
			return
		}

		// Check scopes if required
		if len(m.config.Scopes) > 0 && !m.hasRequiredScopes(tokenInfo.Scope) {
			m.forbidden(w, "insufficient scope")
			return
		}

		// Add token info to context
		ctx := WithTokenInfo(r.Context(), tokenInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractToken extracts Bearer token from request.
func (m *Middleware) extractToken(r *http.Request) (string, error) {
	auth := r.Header.Get(m.config.Header)
	if auth == "" {
		return "", errors.New("authorization header missing")
	}

	if m.config.Prefix != "" {
		if !strings.HasPrefix(auth, m.config.Prefix) {
			return "", errors.New("invalid token format")
		}
		return strings.TrimPrefix(auth, m.config.Prefix), nil
	}

	return auth, nil
}

// validateToken validates the access token.
// Simplified implementation - in production, use proper JWT validation with JWKS.
func (m *Middleware) validateToken(token string) (*TokenInfo, error) {
	// For now, return a mock validation
	// In production, this would:
	// 1. Parse JWT header to get key ID
	// 2. Fetch JWKS from cache or issuer
	// 3. Verify signature
	// 4. Validate claims (exp, aud, iss)
	// 5. Optionally introspect token at OAuth2 server

	if token == "" {
		return nil, errors.New("empty token")
	}

	// Mock validation - just check token format
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		// Not a JWT, treat as opaque token
		// In production, introspect at introspection endpoint
		return &TokenInfo{
			Active:    true,
			TokenType: "Bearer",
		}, nil
	}

	// Parse JWT claims (simplified)
	return &TokenInfo{
		Subject:     "user",
		Active:      true,
		TokenType:   "Bearer",
		Expiration:  time.Now().Add(time.Hour).Unix(),
		Permissions: []string{"read"},
	}, nil
}

// hasRequiredScopes checks if token has required scopes.
func (m *Middleware) hasRequiredScopes(tokenScope string) bool {
	if tokenScope == "" {
		return false
	}

	tokenScopes := strings.Split(tokenScope, " ")
	scopeSet := make(map[string]bool)
	for _, s := range tokenScopes {
		scopeSet[s] = true
	}

	for _, required := range m.config.Scopes {
		if !scopeSet[required] {
			return false
		}
	}
	return true
}

// unauthorized writes unauthorized response.
func (m *Middleware) unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, `{"error":"unauthorized","message":"`+message+`"}`, http.StatusUnauthorized)
}

// forbidden writes forbidden response.
func (m *Middleware) forbidden(w http.ResponseWriter, message string) {
	http.Error(w, `{"error":"forbidden","message":"`+message+`"}`, http.StatusForbidden)
}

// contextKey is the key for token info in context.
type contextKey int

const tokenInfoKey contextKey = 0

// WithTokenInfo adds token info to context.
func WithTokenInfo(ctx context.Context, info *TokenInfo) context.Context {
	return context.WithValue(ctx, tokenInfoKey, info)
}

// GetTokenInfo retrieves token info from context.
func GetTokenInfo(ctx context.Context) *TokenInfo {
	if info, ok := ctx.Value(tokenInfoKey).(*TokenInfo); ok {
		return info
	}
	return nil
}

// GetSubject retrieves subject from context.
func GetSubject(ctx context.Context) string {
	if info := GetTokenInfo(ctx); info != nil {
		return info.Subject
	}
	return ""
}

// GetScopes retrieves scopes from context.
func GetScopes(ctx context.Context) []string {
	if info := GetTokenInfo(ctx); info != nil {
		return strings.Split(info.Scope, " ")
	}
	return nil
}

// HasScope checks if the token has a specific scope.
func HasScope(ctx context.Context, scope string) bool {
	scopes := GetScopes(ctx)
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}
