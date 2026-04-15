// Package oauth2 provides OAuth2/OIDC authentication middleware.
package oauth2

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
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
	keys      map[string]crypto.PublicKey
	expiresAt time.Time
	mu        sync.RWMutex
	duration  time.Duration
	fetchURL  string
	client    *http.Client
}

// jwtHeader represents the JWT header.
type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid"`
}

// jwtClaims represents the JWT claims.
type jwtClaims struct {
	Issuer     string   `json:"iss"`
	Subject    string   `json:"sub"`
	Audience   audience `json:"aud"`
	Expiration int64    `json:"exp"`
	IssuedAt   int64    `json:"iat"`
	NotBefore  int64    `json:"nbf"`
	Scope      string   `json:"scope"`
	ClientID   string   `json:"client_id"`
	TokenType  string   `json:"token_type"`
}

// audience handles both string and []string audience claims.
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = []string{single}
		return nil
	}
	var multi []string
	if err := json.Unmarshal(data, &multi); err != nil {
		return err
	}
	*a = multi
	return nil
}

// jwksResponse represents a JWKS endpoint response.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey represents a JSON Web Key.
type jwkKey struct {
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	N         string `json:"n,omitempty"`
	E         string `json:"e,omitempty"`
	X         string `json:"x,omitempty"`
	Y         string `json:"y,omitempty"`
	Crv       string `json:"crv,omitempty"`
}

// introspectionResponse represents an RFC 7662 token introspection response.
type introspectionResponse struct {
	Active     bool   `json:"active"`
	Scope      string `json:"scope"`
	ClientID   string `json:"client_id"`
	Subject    string `json:"sub"`
	TokenType  string `json:"token_type"`
	Expiration int64  `json:"exp"`
	IssuedAt   int64  `json:"iat"`
	NotBefore  int64  `json:"nbf"`
	Audience   string `json:"aud"`
	Issuer     string `json:"iss"`
}

// New creates a new OAuth2 middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	cacheDuration := 1 * time.Hour
	if config.CacheDuration != "" {
		if d, err := time.ParseDuration(config.CacheDuration); err == nil {
			cacheDuration = d
		}
	}

	m := &Middleware{
		config: config,
		jwksCache: &jwksCache{
			keys:     make(map[string]crypto.PublicKey),
			duration: cacheDuration,
			fetchURL: config.JwksURL,
		},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	m.jwksCache.client = m.httpClient

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
		tokenInfo, err := m.validateToken(r.Context(), token)
		if err != nil {
			m.unauthorized(w, "invalid token")
			return
		}

		if !tokenInfo.Active {
			m.unauthorized(w, "token is not active")
			return
		}

		// Check token expiration
		if tokenInfo.Expiration > 0 && time.Now().Unix() > tokenInfo.Expiration {
			m.unauthorized(w, "token expired")
			return
		}

		// Check not-before
		if tokenInfo.NotBefore > 0 && time.Now().Unix() < tokenInfo.NotBefore {
			m.unauthorized(w, "token not yet valid")
			return
		}

		// Validate issuer if configured
		if m.config.IssuerURL != "" && tokenInfo.Issuer != "" && subtle.ConstantTimeCompare([]byte(tokenInfo.Issuer), []byte(m.config.IssuerURL)) != 1 {
			m.unauthorized(w, "invalid issuer")
			return
		}

		// Validate audience if configured
		if m.config.Audience != "" && len(tokenInfo.Audience) > 0 {
			found := false
			for _, aud := range tokenInfo.Audience {
				if subtle.ConstantTimeCompare([]byte(aud), []byte(m.config.Audience)) == 1 {
					found = true
					break
				}
			}
			if !found {
				m.unauthorized(w, "invalid audience")
				return
			}
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

// validateToken validates the access token using JWKS or token introspection.
func (m *Middleware) validateToken(ctx context.Context, token string) (*TokenInfo, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}

	// If an introspection URL is configured, use RFC 7662 introspection.
	if m.config.IntrospectionURL != "" {
		return m.introspectToken(ctx, token)
	}

	// Try JWT validation if we have a JWKS URL or the token looks like a JWT.
	parts := strings.Split(token, ".")
	if len(parts) == 3 && (m.config.JwksURL != "" || m.config.IssuerURL != "") {
		return m.validateJWT(token, parts)
	}

	// If we reach here, there is no way to validate this token.
	return nil, errors.New("no validation method configured: set JwksURL, IssuerURL, or IntrospectionURL")
}

// validateJWT parses and validates a JWT token.
func (m *Middleware) validateJWT(token string, parts []string) (*TokenInfo, error) {
	// Decode header
	headerBytes, err := base64urlDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decoding JWT header: %w", err)
	}

	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parsing JWT header: %w", err)
	}

	// Reject "none" algorithm
	if header.Algorithm == "none" || header.Algorithm == "" {
		return nil, errors.New("unsupported JWT algorithm")
	}

	// Decode claims
	claimsBytes, err := base64urlDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding JWT claims: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("parsing JWT claims: %w", err)
	}

	// Verify signature
	signingInput := parts[0] + "." + parts[1]
	signatureBytes, err := base64urlDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decoding JWT signature: %w", err)
	}

	if err := m.verifySignature(header, signingInput, signatureBytes); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	return &TokenInfo{
		Subject:    claims.Subject,
		Issuer:     claims.Issuer,
		Audience:   claims.Audience,
		Expiration: claims.Expiration,
		IssuedAt:   claims.IssuedAt,
		NotBefore:  claims.NotBefore,
		Scope:      claims.Scope,
		ClientID:   claims.ClientID,
		TokenType:  "Bearer",
		Active:     true,
	}, nil
}

// verifySignature verifies the JWT signature using the appropriate algorithm.
func (m *Middleware) verifySignature(header jwtHeader, signingInput string, signature []byte) error {
	// Fetch the key from JWKS cache
	key, err := m.getSigningKey(header.KeyID)
	if err != nil {
		return err
	}

	switch header.Algorithm {
	case "RS256":
		return verifyRSA(key, crypto.SHA256, signingInput, signature)
	case "RS384":
		return verifyRSA(key, crypto.SHA384, signingInput, signature)
	case "RS512":
		return verifyRSA(key, crypto.SHA512, signingInput, signature)
	case "ES256":
		return verifyECDSA(key, crypto.SHA256, signingInput, signature)
	case "ES384":
		return verifyECDSA(key, crypto.SHA384, signingInput, signature)
	case "ES512":
		return verifyECDSA(key, crypto.SHA512, signingInput, signature)
	default:
		return fmt.Errorf("unsupported JWT algorithm: %s", header.Algorithm)
	}
}

// verifyRSA verifies an RSA-PKCS1v15 signature.
func verifyRSA(pubKey crypto.PublicKey, hash crypto.Hash, signingInput string, signature []byte) error {
	rsaKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("not an RSA public key")
	}
	h := hash.New()
	h.Write([]byte(signingInput))
	return rsa.VerifyPKCS1v15(rsaKey, hash, h.Sum(nil), signature)
}

// verifyECDSA verifies an ECDSA signature (raw r||s format as used in JWT).
func verifyECDSA(pubKey crypto.PublicKey, hash crypto.Hash, signingInput string, signature []byte) error {
	ecKey, ok := pubKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("not an ECDSA public key")
	}
	h := hash.New()
	h.Write([]byte(signingInput))

	// ECDSA signatures in JWT are r || s, each half the size of the curve order.
	keyBytes := (ecKey.Curve.Params().N.BitLen() + 7) / 8
	if len(signature) != 2*keyBytes {
		return fmt.Errorf("invalid ECDSA signature length: got %d, want %d", len(signature), 2*keyBytes)
	}

	r := new(big.Int).SetBytes(signature[:keyBytes])
	s := new(big.Int).SetBytes(signature[keyBytes:])
	if !ecdsa.Verify(ecKey, h.Sum(nil), r, s) {
		return errors.New("ECDSA signature verification failed")
	}
	return nil
}

// getSigningKey retrieves a signing key from the JWKS cache or fetches it.
func (m *Middleware) getSigningKey(kid string) (crypto.PublicKey, error) {
	if m.jwksCache == nil {
		return nil, errors.New("JWKS not configured")
	}

	m.jwksCache.mu.RLock()
	if time.Now().Before(m.jwksCache.expiresAt) && len(m.jwksCache.keys) > 0 {
		if key, ok := m.jwksCache.keys[kid]; ok {
			m.jwksCache.mu.RUnlock()
			return key, nil
		}
	}
	m.jwksCache.mu.RUnlock()

	// Fetch fresh keys
	if err := m.fetchJWKS(); err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}

	m.jwksCache.mu.RLock()
	defer m.jwksCache.mu.RUnlock()
	if key, ok := m.jwksCache.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key ID %q not found in JWKS", kid)
}

// fetchJWKS fetches and caches the JWKS keys from the configured endpoint.
func (m *Middleware) fetchJWKS() error {
	if m.jwksCache.fetchURL == "" {
		return errors.New("JWKS URL not configured")
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", m.jwksCache.fetchURL, nil)
	if err != nil {
		return err
	}

	resp, err := m.jwksCache.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return err
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parsing JWKS response: %w", err)
	}

	keys := make(map[string]crypto.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Use != "" && jwk.Use != "sig" {
			continue
		}
		pubKey, err := parseJWK(jwk)
		if err != nil {
			continue // skip invalid keys
		}
		if jwk.KeyID != "" {
			keys[jwk.KeyID] = pubKey
		}
	}

	m.jwksCache.mu.Lock()
	m.jwksCache.keys = keys
	m.jwksCache.expiresAt = time.Now().Add(m.jwksCache.duration)
	m.jwksCache.mu.Unlock()

	return nil
}

// parseJWK converts a JWK key to a crypto.PublicKey.
func parseJWK(jwk jwkKey) (crypto.PublicKey, error) {
	switch jwk.KeyType {
	case "RSA":
		return parseRSAPublicKey(jwk.N, jwk.E)
	case "EC":
		return parseECPublicKey(jwk.X, jwk.Y, jwk.Crv)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", jwk.KeyType)
	}
}

// parseRSAPublicKey constructs an RSA public key from JWK parameters.
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64urlDecode(nStr)
	if err != nil {
		return nil, fmt.Errorf("decoding RSA modulus: %w", err)
	}
	eBytes, err := base64urlDecode(eStr)
	if err != nil {
		return nil, fmt.Errorf("decoding RSA exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	if e.Int64() < 2 || e.Int64() > 1<<31-1 {
		return nil, errors.New("invalid RSA exponent")
	}

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// parseECPublicKey constructs an ECDSA public key from JWK parameters.
func parseECPublicKey(xStr, yStr, crv string) (*ecdsa.PublicKey, error) {
	xBytes, err := base64urlDecode(xStr)
	if err != nil {
		return nil, fmt.Errorf("decoding EC X: %w", err)
	}
	yBytes, err := base64urlDecode(yStr)
	if err != nil {
		return nil, fmt.Errorf("decoding EC Y: %w", err)
	}

	var curve elliptic.Curve
	switch crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve: %s", crv)
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)

	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("EC point not on curve")
	}

	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

// introspectToken validates a token using RFC 7662 token introspection.
func (m *Middleware) introspectToken(ctx context.Context, token string) (*TokenInfo, error) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, "POST", m.config.IntrospectionURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Use client credentials for authentication
	if m.config.ClientID != "" {
		req.SetBasicAuth(m.config.ClientID, m.config.ClientSecret)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("introspection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return nil, err
	}

	var intro introspectionResponse
	if err := json.Unmarshal(body, &intro); err != nil {
		return nil, fmt.Errorf("parsing introspection response: %w", err)
	}

	if !intro.Active {
		return nil, errors.New("token is not active")
	}

	info := &TokenInfo{
		Subject:    intro.Subject,
		Issuer:     intro.Issuer,
		Expiration: intro.Expiration,
		IssuedAt:   intro.IssuedAt,
		NotBefore:  intro.NotBefore,
		Scope:      intro.Scope,
		ClientID:   intro.ClientID,
		TokenType:  intro.TokenType,
		Active:     true,
	}
	if intro.Audience != "" {
		info.Audience = []string{intro.Audience}
	}

	return info, nil
}

// base64urlDecode decodes a base64url-encoded string (no padding).
func base64urlDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	resp, _ := json.Marshal(map[string]string{"error": "unauthorized", "message": message})
	_, _ = w.Write(resp)
}

// forbidden writes forbidden response.
func (m *Middleware) forbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	resp, _ := json.Marshal(map[string]string{"error": "forbidden", "message": message})
	_, _ = w.Write(resp)
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
