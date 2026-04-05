// Package jwt provides JWT authentication middleware.
package jwt

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ed25519"
)

// Config configures JWT authentication.
type Config struct {
	Enabled          bool
	Secret           string           // HMAC secret
	PublicKey        []byte           // Ed25519 public key
	Algorithm        string           // HS256, HS384, HS512, EdDSA
	Header           string           // Authorization header name
	Prefix           string           // Token prefix (e.g., "Bearer ")
	Required         bool             // Require JWT for all requests
	ExcludePaths     []string         // Paths to exclude
	ClaimsValidation ClaimsValidation // Additional claim validation
}

// ClaimsValidation configures additional claim validation.
type ClaimsValidation struct {
	Issuer   string // Expected issuer
	Audience string // Expected audience
}

// DefaultConfig returns default JWT configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:   false,
		Algorithm: "HS256",
		Header:    "Authorization",
		Prefix:    "Bearer ",
		Required:  true,
	}
}

// Middleware provides JWT authentication.
type Middleware struct {
	config    Config
	publicKey ed25519.PublicKey
	mu        sync.RWMutex
}

// New creates a new JWT middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config: config,
	}

	// Parse Ed25519 public key if using EdDSA
	if config.Algorithm == "EdDSA" {
		if len(config.PublicKey) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 public key")
		}
		m.publicKey = ed25519.PublicKey(config.PublicKey)
	}

	return m, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "jwt"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 210 // After security, before real IP
}

// Wrap wraps the handler with JWT authentication.
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
			if m.config.Required {
				m.unauthorized(w, err.Error())
				return
			}
			// Token not required, continue
			next.ServeHTTP(w, r)
			return
		}

		// Validate token
		claims, err := m.validateToken(token)
		if err != nil {
			m.unauthorized(w, "invalid token")
			return
		}

		// Add claims to context
		ctx := r.Context()
		ctx = WithClaims(ctx, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractToken extracts JWT from request.
func (m *Middleware) extractToken(r *http.Request) (string, error) {
	// Get header
	auth := r.Header.Get(m.config.Header)
	if auth == "" {
		return "", errors.New("authorization header missing")
	}

	// Remove prefix
	if m.config.Prefix != "" {
		if !strings.HasPrefix(auth, m.config.Prefix) {
			return "", errors.New("invalid token format")
		}
		auth = strings.TrimPrefix(auth, m.config.Prefix)
	}

	return auth, nil
}

// validateToken validates a JWT token.
func (m *Middleware) validateToken(token string) (*Claims, error) {
	// Split token
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	// Decode header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}

	var header Header
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, err
	}

	// Verify algorithm
	if header.Algorithm != m.config.Algorithm {
		return nil, errors.New("algorithm mismatch")
	}

	// Decode claims
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	var claims Claims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, err
	}

	// Validate claims
	if err := m.validateClaims(&claims); err != nil {
		return nil, err
	}

	// Verify signature
	if err := m.verifySignature(parts[0], parts[1], parts[2]); err != nil {
		return nil, err
	}

	return &claims, nil
}

// validateClaims validates JWT claims.
func (m *Middleware) validateClaims(claims *Claims) error {
	now := time.Now().Unix()

	// Check expiration
	if claims.ExpiresAt > 0 && now > claims.ExpiresAt {
		return errors.New("token expired")
	}

	// Check not before
	if claims.NotBefore > 0 && now < claims.NotBefore {
		return errors.New("token not yet valid")
	}

	// Check issued at
	if claims.IssuedAt > 0 && now < claims.IssuedAt {
		return errors.New("invalid issued at")
	}

	// Validate issuer
	if m.config.ClaimsValidation.Issuer != "" {
		if subtle.ConstantTimeCompare([]byte(claims.Issuer), []byte(m.config.ClaimsValidation.Issuer)) != 1 {
			return errors.New("invalid issuer")
		}
	}

	// Validate audience
	if m.config.ClaimsValidation.Audience != "" {
		found := false
		for _, aud := range claims.Audience {
			if subtle.ConstantTimeCompare([]byte(aud), []byte(m.config.ClaimsValidation.Audience)) == 1 {
				found = true
				break
			}
		}
		if !found {
			return errors.New("invalid audience")
		}
	}

	return nil
}

// verifySignature verifies JWT signature.
func (m *Middleware) verifySignature(header, claims, signature string) error {
	data := header + "." + claims
	sigBytes, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return err
	}

	switch m.config.Algorithm {
	case "HS256", "HS384", "HS512":
		return m.verifyHMAC(data, sigBytes)
	case "EdDSA":
		return m.verifyEd25519(data, sigBytes)
	default:
		return errors.New("unsupported algorithm")
	}
}

// verifyHMAC verifies HMAC signature.
func (m *Middleware) verifyHMAC(data string, signature []byte) error {
	// For HMAC, we need a pre-configured secret
	// This is a simplified implementation
	// In production, use a proper HMAC library
	expectedSig := computeHMAC(data, m.config.Secret, m.config.Algorithm)
	if subtle.ConstantTimeCompare(signature, expectedSig) != 1 {
		return errors.New("invalid signature")
	}
	return nil
}

// verifyEd25519 verifies Ed25519 signature.
func (m *Middleware) verifyEd25519(data string, signature []byte) error {
	if !ed25519.Verify(m.publicKey, []byte(data), signature) {
		return errors.New("invalid signature")
	}
	return nil
}

// computeHMAC computes HMAC signature.
func computeHMAC(data, secret, algorithm string) []byte {
	var hasher func() hash.Hash
	switch algorithm {
	case "HS256":
		hasher = sha256.New
	case "HS384":
		hasher = sha512.New384
	case "HS512":
		hasher = sha512.New
	default:
		hasher = sha256.New
	}

	h := hmac.New(hasher, []byte(secret))
	h.Write([]byte(data))
	return h.Sum(nil)
}

// unauthorized writes unauthorized response.
func (m *Middleware) unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, `{"error":"unauthorized","message":"`+message+`"}`, http.StatusUnauthorized)
}

// Header represents JWT header.
type Header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

// Claims represents JWT claims.
type Claims struct {
	Subject   string                 `json:"sub,omitempty"`
	Issuer    string                 `json:"iss,omitempty"`
	Audience  []string               `json:"aud,omitempty"`
	ExpiresAt int64                  `json:"exp,omitempty"`
	NotBefore int64                  `json:"nbf,omitempty"`
	IssuedAt  int64                  `json:"iat,omitempty"`
	JWTID     string                 `json:"jti,omitempty"`
	Custom    map[string]interface{} `json:"-"`
}

// contextKey is the key for claims in context.
type contextKey int

const claimsKey contextKey = 0

// WithClaims adds claims to context.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// GetClaims retrieves claims from context.
func GetClaims(ctx context.Context) *Claims {
	if claims, ok := ctx.Value(claimsKey).(*Claims); ok {
		return claims
	}
	return nil
}

// GetSubject retrieves subject from context.
func GetSubject(ctx context.Context) string {
	if claims := GetClaims(ctx); claims != nil {
		return claims.Subject
	}
	return ""
}

// GetAudience retrieves audience from context.
func GetAudience(ctx context.Context) []string {
	if claims := GetClaims(ctx); claims != nil {
		return claims.Audience
	}
	return nil
}
