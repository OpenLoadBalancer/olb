package oauth2

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuth2_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called when OAuth2 is disabled")
	}
}

func TestOAuth2_MissingToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestOAuth2_InvalidTokenFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid token format")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestOAuth2_RejectsWithoutValidationMethod(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without a validation method configured")
	}))

	// Valid JWT format token, but no JWKS URL or introspection URL configured
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyMTIzIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestOAuth2_RejectsOpaqueTokenWithoutIntrospection(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without validation method")
	}))

	// Opaque token (not JWT format) — should be rejected without introspection URL
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer opaque-token-12345")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestOAuth2_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/public"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for excluded paths")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestOAuth2_InsufficientScope(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "
	config.Scopes = []string{"write", "admin"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with insufficient scope")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid.token.here")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 (no validation method), got %d", rec.Code)
	}
}

func TestOAuth2_NoPrefix(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "" // No prefix required

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without validation method")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 (no validation method), got %d", rec.Code)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Header != "Authorization" {
		t.Errorf("Default Header should be 'Authorization', got '%s'", config.Header)
	}
	if config.Prefix != "Bearer " {
		t.Errorf("Default Prefix should be 'Bearer ', got '%s'", config.Prefix)
	}
	if config.CacheDuration != "1h" {
		t.Errorf("Default CacheDuration should be '1h', got '%s'", config.CacheDuration)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 212 {
		t.Errorf("Expected priority 212, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "oauth2" {
		t.Errorf("Expected name 'oauth2', got '%s'", mw.Name())
	}
}

func TestHasRequiredScopes(t *testing.T) {
	config := DefaultConfig()
	config.Scopes = []string{"read", "write"}

	mw, _ := New(config)

	tests := []struct {
		tokenScope string
		want       bool
	}{
		{"read write", true},
		{"read write admin", true},
		{"read", false},
		{"write", false},
		{"", false},
		{"admin", false},
	}

	for _, tt := range tests {
		got := mw.hasRequiredScopes(tt.tokenScope)
		if got != tt.want {
			t.Errorf("hasRequiredScopes(%q) = %v, want %v", tt.tokenScope, got, tt.want)
		}
	}
}

func TestContextHelpers(t *testing.T) {
	// Test WithTokenInfo and GetTokenInfo
	info := &TokenInfo{
		Subject:   "user123",
		Scope:     "read write",
		Active:    true,
		TokenType: "Bearer",
	}

	ctx := WithTokenInfo(t.Context(), info)
	retrieved := GetTokenInfo(ctx)

	if retrieved == nil {
		t.Fatal("GetTokenInfo returned nil")
	}

	if retrieved.Subject != "user123" {
		t.Errorf("Subject = %s, want user123", retrieved.Subject)
	}

	// Test GetSubject
	subject := GetSubject(ctx)
	if subject != "user123" {
		t.Errorf("GetSubject = %s, want user123", subject)
	}

	// Test GetScopes
	scopes := GetScopes(ctx)
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("GetScopes = %v, want [read write]", scopes)
	}

	// Test HasScope
	if !HasScope(ctx, "read") {
		t.Error("HasScope(read) should be true")
	}
	if !HasScope(ctx, "write") {
		t.Error("HasScope(write) should be true")
	}
	if HasScope(ctx, "admin") {
		t.Error("HasScope(admin) should be false")
	}
}

func TestGetTokenInfo_NoInfo(t *testing.T) {
	ctx := t.Context()
	info := GetTokenInfo(ctx)

	if info != nil {
		t.Error("GetTokenInfo should return nil when no info is set")
	}
}

func TestGetSubject_NoInfo(t *testing.T) {
	ctx := t.Context()
	subject := GetSubject(ctx)

	if subject != "" {
		t.Errorf("GetSubject should return empty string, got %s", subject)
	}
}

func TestGetScopes_NoInfo(t *testing.T) {
	ctx := t.Context()
	scopes := GetScopes(ctx)

	if scopes != nil {
		t.Errorf("GetScopes should return nil, got %v", scopes)
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		prefix    string
		headerVal string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			header:    "Authorization",
			prefix:    "Bearer ",
			headerVal: "Bearer my-token",
			wantToken: "my-token",
			wantErr:   false,
		},
		{
			name:      "no prefix",
			header:    "Authorization",
			prefix:    "",
			headerVal: "my-token",
			wantToken: "my-token",
			wantErr:   false,
		},
		{
			name:    "missing header",
			header:  "Authorization",
			prefix:  "Bearer ",
			wantErr: true,
		},
		{
			name:      "wrong prefix",
			header:    "Authorization",
			prefix:    "Bearer ",
			headerVal: "Basic my-token",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.Header = tt.header
			config.Prefix = tt.prefix

			mw, _ := New(config)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.headerVal != "" {
				req.Header.Set(tt.header, tt.headerVal)
			}

			token, err := mw.extractToken(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if token != tt.wantToken {
				t.Errorf("extractToken() = %v, want %v", token, tt.wantToken)
			}
		})
	}
}

func TestValidateToken_RejectsEmpty(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	_, err := mw.validateToken(t.Context(), "")
	if err == nil {
		t.Error("Expected error for empty token")
	}
}

func TestValidateToken_RejectsWithoutConfig(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	// JWT format but no JWKS URL — should reject
	_, err := mw.validateToken(t.Context(), "header.payload.signature")
	if err == nil {
		t.Error("Expected error when no validation method is configured")
	}

	// Opaque token without introspection URL — should reject
	_, err = mw.validateToken(t.Context(), "opaque-token")
	if err == nil {
		t.Error("Expected error when no validation method is configured for opaque token")
	}
}

func TestBase64urlDecode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"dGVzdA", "test"},
		{"dGVzdA==", "test"},
		{"", ""},
		{"Zm9vYmFy", "foobar"},
	}

	for _, tt := range tests {
		got, err := base64urlDecode(tt.input)
		if err != nil {
			t.Errorf("base64urlDecode(%q) error: %v", tt.input, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("base64urlDecode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNew_EnabledWithConfig(t *testing.T) {
	config := Config{
		Enabled:       true,
		JwksURL:       "https://example.com/.well-known/jwks.json",
		CacheDuration: "30m",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if mw.jwksCache == nil {
		t.Error("jwksCache should be initialized when enabled")
	}
	if mw.jwksCache.fetchURL != config.JwksURL {
		t.Errorf("fetchURL = %q, want %q", mw.jwksCache.fetchURL, config.JwksURL)
	}
	if mw.jwksCache.duration != 30*time.Minute {
		t.Errorf("cache duration = %v, want 30m", mw.jwksCache.duration)
	}
	if mw.httpClient == nil {
		t.Error("httpClient should be initialized")
	}
}

func TestNew_EnabledWithInvalidCacheDuration(t *testing.T) {
	config := Config{
		Enabled:       true,
		JwksURL:       "https://example.com/jwks",
		CacheDuration: "invalid",
	}
	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if mw.jwksCache.duration != 1*time.Hour {
		t.Errorf("cache duration should default to 1h for invalid input, got %v", mw.jwksCache.duration)
	}
}

// --- JWT construction helpers ---

func base64urlEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func buildJWT(header, claims map[string]interface{}, signFn func([]byte) ([]byte, string)) string {
	hBytes, _ := json.Marshal(header)
	cBytes, _ := json.Marshal(claims)
	signingInput := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes)
	sig, kid := signFn([]byte(signingInput))
	h := header
	if kid != "" {
		h["kid"] = kid
	}
	hBytes, _ = json.Marshal(h)
	return base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + "." + base64urlEncode(sig)
}

func signRSA(key *rsa.PrivateKey, hash crypto.Hash) func([]byte) ([]byte, string) {
	return func(input []byte) ([]byte, string) {
		h := hash.New()
		h.Write(input)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, hash, h.Sum(nil))
		return sig, ""
	}
}

func signECDSA(key *ecdsa.PrivateKey) func([]byte) ([]byte, string) {
	return func(input []byte) ([]byte, string) {
		h := crypto.SHA256.New()
		h.Write(input)
		r, s, _ := ecdsa.Sign(rand.Reader, key, h.Sum(nil))
		keyBytes := (key.Curve.Params().N.BitLen() + 7) / 8
		sig := make([]byte, 2*keyBytes)
		r.FillBytes(sig[:keyBytes])
		s.FillBytes(sig[keyBytes:])
		return sig, ""
	}
}

// jwksHandler creates an httptest handler serving JWKS from the given RSA key.
func jwksHandler(rsaKey *rsa.PublicKey, kid string) http.HandlerFunc {
	n := base64urlEncode(rsaKey.N.Bytes())
	e := base64urlEncode(big.NewInt(int64(rsaKey.E)).Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","use":"sig","kid":"%s","alg":"RS256","n":"%s","e":"%s"}]}`, kid, n, e)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}
}

func ecJWKSHandler(ecKey *ecdsa.PublicKey, kid, alg, crv string) http.HandlerFunc {
	x := base64urlEncode(ecKey.X.Bytes())
	y := base64urlEncode(ecKey.Y.Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"EC","use":"sig","kid":"%s","alg":"%s","crv":"%s","x":"%s","y":"%s"}]}`, kid, alg, crv, x, y)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}
}

// --- RSA JWT validation tests ---

func TestValidateJWT_RS256(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "iss": "https://example.com",
			"aud": "my-client", "exp": time.Now().Add(1 * time.Hour).Unix(),
			"iat": time.Now().Unix(), "scope": "read write",
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	info, err := mw.validateToken(t.Context(), token)
	if err != nil {
		t.Fatalf("validateToken failed: %v", err)
	}
	if info.Subject != "user1" {
		t.Errorf("Subject = %q, want user1", info.Subject)
	}
	if info.Issuer != "https://example.com" {
		t.Errorf("Issuer = %q, want https://example.com", info.Issuer)
	}
	if !info.Active {
		t.Error("Token should be active")
	}
}

func TestValidateJWT_RS384(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	claims := map[string]interface{}{
		"sub": "user1", "exp": time.Now().Add(1 * time.Hour).Unix(),
	}

	token := buildRSJWT(rsaKey, "RS384", "key1", claims)
	_, err := mw.validateToken(t.Context(), token)
	if err != nil {
		t.Fatalf("validateToken RS384 failed: %v", err)
	}
}

func TestValidateJWT_RS512(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	claims := map[string]interface{}{
		"sub": "user1", "exp": time.Now().Add(1 * time.Hour).Unix(),
	}

	token := buildRSJWT(rsaKey, "RS512", "key1", claims)
	_, err := mw.validateToken(t.Context(), token)
	if err != nil {
		t.Fatalf("validateToken RS512 failed: %v", err)
	}
}

func buildRSJWT(key *rsa.PrivateKey, alg, kid string, claims map[string]interface{}) string {
	var hash crypto.Hash
	switch alg {
	case "RS256":
		hash = crypto.SHA256
	case "RS384":
		hash = crypto.SHA384
	case "RS512":
		hash = crypto.SHA512
	}
	hBytes, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT", "kid": kid})
	cBytes, _ := json.Marshal(claims)
	signingInput := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes)
	h := hash.New()
	h.Write([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, hash, h.Sum(nil))
	return base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + "." + base64urlEncode(sig)
}

// --- ECDSA JWT validation tests ---

func TestValidateJWT_ES256(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := httptest.NewServer(ecJWKSHandler(&ecKey.PublicKey, "eckey1", "ES256", "P-256"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	token := buildECJWT(ecKey, "ES256", "eckey1", map[string]interface{}{
		"sub": "ecuser", "exp": time.Now().Add(1 * time.Hour).Unix(),
	})

	info, err := mw.validateToken(t.Context(), token)
	if err != nil {
		t.Fatalf("validateToken ES256 failed: %v", err)
	}
	if info.Subject != "ecuser" {
		t.Errorf("Subject = %q, want ecuser", info.Subject)
	}
}

func TestValidateJWT_ES384(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	srv := httptest.NewServer(ecJWKSHandler(&ecKey.PublicKey, "eckey1", "ES384", "P-384"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	hBytes, _ := json.Marshal(map[string]string{"alg": "ES384", "typ": "JWT", "kid": "eckey1"})
	claims := map[string]interface{}{"sub": "ecuser", "exp": time.Now().Add(1 * time.Hour).Unix()}
	cBytes, _ := json.Marshal(claims)
	signingInput := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes)
	h := crypto.SHA384.New()
	h.Write([]byte(signingInput))
	r, s, _ := ecdsa.Sign(rand.Reader, ecKey, h.Sum(nil))
	keyBytes := (ecKey.Curve.Params().N.BitLen() + 7) / 8
	sig := make([]byte, 2*keyBytes)
	r.FillBytes(sig[:keyBytes])
	s.FillBytes(sig[keyBytes:])
	token := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + "." + base64urlEncode(sig)

	_, err := mw.validateToken(t.Context(), token)
	if err != nil {
		t.Fatalf("validateToken ES384 failed: %v", err)
	}
}

func TestValidateJWT_ES512(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	srv := httptest.NewServer(ecJWKSHandler(&ecKey.PublicKey, "eckey1", "ES512", "P-521"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	hBytes, _ := json.Marshal(map[string]string{"alg": "ES512", "typ": "JWT", "kid": "eckey1"})
	claims := map[string]interface{}{"sub": "ecuser", "exp": time.Now().Add(1 * time.Hour).Unix()}
	cBytes, _ := json.Marshal(claims)
	signingInput := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes)
	h := crypto.SHA512.New()
	h.Write([]byte(signingInput))
	r, s, _ := ecdsa.Sign(rand.Reader, ecKey, h.Sum(nil))
	keyBytes := (ecKey.Curve.Params().N.BitLen() + 7) / 8
	sig := make([]byte, 2*keyBytes)
	r.FillBytes(sig[:keyBytes])
	s.FillBytes(sig[keyBytes:])
	token := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + "." + base64urlEncode(sig)

	_, err := mw.validateToken(t.Context(), token)
	if err != nil {
		t.Fatalf("validateToken ES512 failed: %v", err)
	}
}

func buildECJWT(key *ecdsa.PrivateKey, alg, kid string, claims map[string]interface{}) string {
	hBytes, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT", "kid": kid})
	cBytes, _ := json.Marshal(claims)
	signingInput := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes)
	h := crypto.SHA256.New()
	h.Write([]byte(signingInput))
	r, s, _ := ecdsa.Sign(rand.Reader, key, h.Sum(nil))
	keyBytes := (key.Curve.Params().N.BitLen() + 7) / 8
	sig := make([]byte, 2*keyBytes)
	r.FillBytes(sig[:keyBytes])
	s.FillBytes(sig[keyBytes:])
	return base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + "." + base64urlEncode(sig)
}

func createMiddlewareWithJWKS(t *testing.T, jwksURL string) *Middleware {
	t.Helper()
	mw, err := New(Config{
		Enabled:       true,
		JwksURL:       jwksURL,
		CacheDuration: "1h",
	})
	if err != nil {
		t.Fatal(err)
	}
	return mw
}

// --- Wrap() full flow tests ---

func TestWrap_RSAFullFlow_Success(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	cfg.Audience = "my-client"
	cfg.IssuerURL = "https://example.com"

	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		info := GetTokenInfo(r.Context())
		if info == nil {
			t.Error("TokenInfo should be in context")
		} else if info.Subject != "user1" {
			t.Errorf("Subject = %q, want user1", info.Subject)
		}
		w.WriteHeader(http.StatusOK)
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "iss": "https://example.com",
			"aud": "my-client", "exp": time.Now().Add(1 * time.Hour).Unix(),
			"iat": time.Now().Unix(), "scope": "read write",
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestWrap_ExpiredToken(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler for expired token")
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "exp": time.Now().Add(-1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "token expired") {
		t.Errorf("Body should contain 'token expired', got: %s", rec.Body.String())
	}
}

func TestWrap_NotBeforeFuture(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler for not-yet-valid token")
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "exp": time.Now().Add(1 * time.Hour).Unix(),
			"nbf": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_InvalidIssuer(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	cfg.IssuerURL = "https://expected.com"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler for wrong issuer")
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "iss": "https://wrong.com",
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_InvalidAudience(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	cfg.Audience = "expected-aud"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler for wrong audience")
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "aud": "wrong-aud",
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_AudienceArrayMatch(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	cfg.Audience = "my-client"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "aud": []string{"my-client", "other-client"},
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called — audience matches one in array")
	}
}

func TestWrap_SufficientScopes(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	cfg.Scopes = []string{"read", "write"}
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "scope": "read write admin",
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called — sufficient scopes")
	}
}

func TestWrap_InsufficientScopes_Returns403(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	cfg.Scopes = []string{"admin"}
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler — insufficient scopes")
	}))

	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "scope": "read write",
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(rsaKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want 403", rec.Code)
	}
}

func TestWrap_InvalidSignature(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = srv.URL
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler with invalid signature")
	}))

	// Sign with wrong key
	token := buildJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "key1"},
		map[string]interface{}{
			"sub": "user1", "exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		signRSA(otherKey, crypto.SHA256),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_NoneAlgorithm(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = "http://unused"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler with 'none' algorithm")
	}))

	hBytes, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	cBytes, _ := json.Marshal(map[string]interface{}{"sub": "hacker", "exp": time.Now().Add(1 * time.Hour).Unix()})
	token := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + "."

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_EmptyAlgorithm(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = "http://unused"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler with empty algorithm")
	}))

	hBytes, _ := json.Marshal(map[string]string{"alg": "", "typ": "JWT"})
	cBytes, _ := json.Marshal(map[string]interface{}{"sub": "hacker"})
	token := base64urlEncode(hBytes) + "." + base64urlEncode(cBytes) + ".c2ln"

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_MalformedJWT_InvalidHeader(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = "http://unused"
	cfg.IssuerURL = "https://example.com"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler")
	}))

	// "not-json!!!" base64url encoded
	token := base64urlEncode([]byte("not-json!!!")) + "." + base64urlEncode([]byte(`{"sub":"x"}`)) + ".c2ln"

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_MalformedJWT_InvalidClaims(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = "http://unused"
	cfg.IssuerURL = "https://example.com"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler")
	}))

	hBytes, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	token := base64urlEncode(hBytes) + "." + base64urlEncode([]byte("not-json!!!")) + ".c2ln"

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_MalformedJWT_InvalidBase64(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.JwksURL = "http://unused"
	cfg.IssuerURL = "https://example.com"
	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer !!!invalid!!!.")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

// --- JWKS cache tests ---

func TestGetSigningKey_CacheHit(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		jwksHandler(&rsaKey.PublicKey, "key1")(w, r)
	}))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	// First call fetches from server
	_, err := mw.getSigningKey("key1")
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 JWKS fetch, got %d", callCount)
	}

	// Second call should use cache
	_, err = mw.getSigningKey("key1")
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 JWKS fetch (cached), got %d", callCount)
	}
}

func TestGetSigningKey_KeyNotFound(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	_, err := mw.getSigningKey("nonexistent")
	if err == nil {
		t.Error("Expected error for unknown key ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestGetSigningKey_NilCache(t *testing.T) {
	mw, _ := New(Config{Enabled: false})
	_, err := mw.getSigningKey("key1")
	if err == nil {
		t.Error("Expected error when JWKS cache is nil")
	}
}

func TestFetchJWKS_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)
	err := mw.fetchJWKS()
	if err == nil {
		t.Error("Expected error for non-200 JWKS response")
	}
}

func TestFetchJWKS_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)
	err := mw.fetchJWKS()
	if err == nil {
		t.Error("Expected error for invalid JSON response")
	}
}

func TestFetchJWKS_EmptyURL(t *testing.T) {
	mw := createMiddlewareWithJWKS(t, "")
	mw.jwksCache.fetchURL = ""
	err := mw.fetchJWKS()
	if err == nil {
		t.Error("Expected error for empty JWKS URL")
	}
}

func TestFetchJWKS_SkipsNonSigKeys(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	n := base64urlEncode(rsaKey.N.Bytes())
	e := base64urlEncode(big.NewInt(int64(rsaKey.E)).Bytes())

	// Include both sig and enc use keys
	jwks := fmt.Sprintf(`{"keys":[
		{"kty":"RSA","use":"enc","kid":"enc-key","n":"%s","e":"%s"},
		{"kty":"RSA","use":"sig","kid":"sig-key","n":"%s","e":"%s"}
	]}`, n, e, n, e)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(jwks))
	}))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)
	err := mw.fetchJWKS()
	if err != nil {
		t.Fatal(err)
	}

	// enc-key should be skipped, sig-key should be present
	if _, ok := mw.jwksCache.keys["enc-key"]; ok {
		t.Error("enc-use key should be skipped")
	}
	if _, ok := mw.jwksCache.keys["sig-key"]; !ok {
		t.Error("sig-use key should be present")
	}
}

func TestFetchJWKS_SkipsInvalidKeys(t *testing.T) {
	jwks := `{"keys":[
		{"kty":"RSA","use":"sig","kid":"bad-key","n":"invalid!!!base64","e":"AQAB"},
		{"kty":"unknown","use":"sig","kid":"unk-key"}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(jwks))
	}))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)
	err := mw.fetchJWKS()
	if err != nil {
		t.Fatal(err)
	}
	if len(mw.jwksCache.keys) != 0 {
		t.Errorf("Expected 0 valid keys, got %d", len(mw.jwksCache.keys))
	}
}

// --- Token introspection tests ---

func TestIntrospectToken_Active(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("Expected form content type")
		}
		// Check basic auth
		user, pass, ok := r.BasicAuth()
		if !ok || user != "my-client" || pass != "my-secret" {
			t.Errorf("Basic auth = %s/%s, want my-client/my-secret", user, pass)
		}
		w.Write([]byte(`{"active":true,"sub":"user1","iss":"https://example.com","aud":"my-client","scope":"read write","exp":1893456000,"client_id":"my-client"}`))
	}))
	defer srv.Close()

	mw, err := New(Config{
		Enabled:          true,
		IntrospectionURL: srv.URL,
		ClientID:         "my-client",
		ClientSecret:     "my-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := mw.introspectToken(t.Context(), "some-token")
	if err != nil {
		t.Fatalf("introspectToken failed: %v", err)
	}
	if !info.Active {
		t.Error("Token should be active")
	}
	if info.Subject != "user1" {
		t.Errorf("Subject = %q, want user1", info.Subject)
	}
	if info.Scope != "read write" {
		t.Errorf("Scope = %q, want 'read write'", info.Scope)
	}
	if len(info.Audience) != 1 || info.Audience[0] != "my-client" {
		t.Errorf("Audience = %v, want [my-client]", info.Audience)
	}
}

func TestIntrospectToken_Inactive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active":false}`))
	}))
	defer srv.Close()

	mw, err := New(Config{Enabled: true, IntrospectionURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mw.introspectToken(t.Context(), "revoked-token")
	if err == nil {
		t.Error("Expected error for inactive token")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("Error should mention 'not active', got: %v", err)
	}
}

func TestIntrospectToken_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	mw, err := New(Config{Enabled: true, IntrospectionURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mw.introspectToken(t.Context(), "token")
	if err == nil {
		t.Error("Expected error for non-200 introspection response")
	}
}

func TestIntrospectToken_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	mw, err := New(Config{Enabled: true, IntrospectionURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mw.introspectToken(t.Context(), "token")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestIntrospectToken_NoClientCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if ok {
			t.Error("Should not send basic auth without client credentials")
		}
		w.Write([]byte(`{"active":true,"sub":"user1"}`))
	}))
	defer srv.Close()

	mw, err := New(Config{Enabled: true, IntrospectionURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mw.introspectToken(t.Context(), "token")
	if err != nil {
		t.Fatal(err)
	}
}

func TestWrap_IntrospectionFullFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active":true,"sub":"introspected-user","scope":"read","exp":` + fmt.Sprintf("%d", time.Now().Add(1*time.Hour).Unix()) + `}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IntrospectionURL = srv.URL
	cfg.ClientID = "my-client"
	cfg.ClientSecret = "my-secret"

	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		info := GetTokenInfo(r.Context())
		if info == nil || info.Subject != "introspected-user" {
			t.Errorf("Wrong token info in context: %v", info)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer opaque-token-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for valid introspected token")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}
}

func TestWrap_IntrospectionInactiveToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active":false}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IntrospectionURL = srv.URL

	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler for inactive token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer revoked")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

func TestWrap_IntrospectionExpiredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(`{"active":true,"sub":"user1","exp":%d}`, time.Now().Add(-1*time.Hour).Unix())))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IntrospectionURL = srv.URL

	mw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not call handler for expired token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
}

// --- parseJWK / parseRSAPublicKey / parseECPublicKey tests ---

func TestParseJWK_RSA(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	n := base64urlEncode(rsaKey.N.Bytes())
	e := base64urlEncode(big.NewInt(int64(rsaKey.E)).Bytes())

	pub, err := parseJWK(jwkKey{KeyType: "RSA", N: n, E: e})
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatal("Expected *rsa.PublicKey")
	}
	if rsaPub.E != rsaKey.E {
		t.Errorf("Exponent = %d, want %d", rsaPub.E, rsaKey.E)
	}
}

func TestParseJWK_EC(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	x := base64urlEncode(ecKey.X.Bytes())
	y := base64urlEncode(ecKey.Y.Bytes())

	pub, err := parseJWK(jwkKey{KeyType: "EC", X: x, Y: y, Crv: "P-256"})
	if err != nil {
		t.Fatal(err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("Expected *ecdsa.PublicKey")
	}
	if ecPub.X.Cmp(ecKey.X) != 0 {
		t.Error("X mismatch")
	}
}

func TestParseJWK_UnsupportedType(t *testing.T) {
	_, err := parseJWK(jwkKey{KeyType: "oct"})
	if err == nil {
		t.Error("Expected error for unsupported key type")
	}
}

func TestParseRSAPublicKey_InvalidBase64(t *testing.T) {
	_, err := parseRSAPublicKey("!!!invalid!!!", "AQAB")
	if err == nil {
		t.Error("Expected error for invalid base64 modulus")
	}
}

func TestParseRSAPublicKey_InvalidExponentBase64(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	n := base64urlEncode(rsaKey.N.Bytes())
	_, err := parseRSAPublicKey(n, "!!!invalid!!!")
	if err == nil {
		t.Error("Expected error for invalid base64 exponent")
	}
}

func TestParseRSAPublicKey_InvalidExponent(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	n := base64urlEncode(rsaKey.N.Bytes())
	e := base64urlEncode(big.NewInt(1).Bytes()) // exponent 1 is invalid (< 2)
	_, err := parseRSAPublicKey(n, e)
	if err == nil {
		t.Error("Expected error for invalid RSA exponent")
	}
}

func TestParseECPublicKey_InvalidXBase64(t *testing.T) {
	_, err := parseECPublicKey("!!!invalid!!!", "AQAB", "P-256")
	if err == nil {
		t.Error("Expected error for invalid X base64")
	}
}

func TestParseECPublicKey_InvalidYBase64(t *testing.T) {
	_, err := parseECPublicKey("AQAB", "!!!invalid!!!", "P-256")
	if err == nil {
		t.Error("Expected error for invalid Y base64")
	}
}

func TestParseECPublicKey_UnsupportedCurve(t *testing.T) {
	_, err := parseECPublicKey("AQAB", "AQAB", "P-UNKNOWN")
	if err == nil {
		t.Error("Expected error for unsupported curve")
	}
}

func TestParseECPublicKey_PointNotOnCurve(t *testing.T) {
	x := base64urlEncode(big.NewInt(99999).Bytes())
	y := base64urlEncode(big.NewInt(99999).Bytes())
	_, err := parseECPublicKey(x, y, "P-256")
	if err == nil {
		t.Error("Expected error for point not on curve")
	}
}

func TestParseECPublicKey_P384(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	x := base64urlEncode(ecKey.X.Bytes())
	y := base64urlEncode(ecKey.Y.Bytes())
	pub, err := parseECPublicKey(x, y, "P-384")
	if err != nil {
		t.Fatal(err)
	}
	if pub.Curve != elliptic.P384() {
		t.Error("Expected P-384 curve")
	}
}

func TestParseECPublicKey_P521(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	x := base64urlEncode(ecKey.X.Bytes())
	y := base64urlEncode(ecKey.Y.Bytes())
	pub, err := parseECPublicKey(x, y, "P-521")
	if err != nil {
		t.Fatal(err)
	}
	if pub.Curve != elliptic.P521() {
		t.Error("Expected P-521 curve")
	}
}

// --- verifyRSA / verifyECDSA negative tests ---

func TestVerifyRSA_WrongKeyType(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	err := verifyRSA(ecKey.Public(), crypto.SHA256, "input", []byte("sig"))
	if err == nil {
		t.Error("Expected error when passing non-RSA key")
	}
}

func TestVerifyRSA_InvalidSignature(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	err := verifyRSA(rsaKey.Public(), crypto.SHA256, "input", []byte("invalid-signature"))
	if err == nil {
		t.Error("Expected error for invalid RSA signature")
	}
}

func TestVerifyECDSA_WrongKeyType(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	err := verifyECDSA(rsaKey.Public(), crypto.SHA256, "input", []byte("sig"))
	if err == nil {
		t.Error("Expected error when passing non-ECDSA key")
	}
}

func TestVerifyECDSA_InvalidSignatureLength(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	err := verifyECDSA(ecKey.Public(), crypto.SHA256, "input", []byte("short"))
	if err == nil {
		t.Error("Expected error for invalid ECDSA signature length")
	}
}

func TestVerifyECDSA_InvalidSignature(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyBytes := (ecKey.Curve.Params().N.BitLen() + 7) / 8
	sig := make([]byte, 2*keyBytes)
	for i := range sig {
		sig[i] = 0xFF
	}
	err := verifyECDSA(ecKey.Public(), crypto.SHA256, "input", sig)
	if err == nil {
		t.Error("Expected error for invalid ECDSA signature")
	}
}

// --- audience unmarshal tests ---

func TestAudience_UnmarshalJSON_String(t *testing.T) {
	var a audience
	err := json.Unmarshal([]byte(`"single-aud"`), &a)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0] != "single-aud" {
		t.Errorf("Expected [single-aud], got %v", a)
	}
}

func TestAudience_UnmarshalJSON_Array(t *testing.T) {
	var a audience
	err := json.Unmarshal([]byte(`["aud1","aud2"]`), &a)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || a[0] != "aud1" || a[1] != "aud2" {
		t.Errorf("Expected [aud1 aud2], got %v", a)
	}
}

func TestAudience_UnmarshalJSON_Invalid(t *testing.T) {
	var a audience
	err := json.Unmarshal([]byte(`123`), &a)
	if err == nil {
		t.Error("Expected error for invalid audience JSON")
	}
}

// --- jsonEscape test ---

func TestJsonEscape(t *testing.T) {
	tests := []struct {
		input  string
		substr string // substring that must appear in output
	}{
		{"simple", "simple"},
		{`<script>`, `\u003cscript\u003e`},
	}
	for _, tt := range tests {
		got := jsonEscape(tt.input)
		if !strings.Contains(got, tt.substr) {
			t.Errorf("jsonEscape(%q) = %q, want to contain %q", tt.input, got, tt.substr)
		}
	}
}

// --- response writing tests ---

func TestUnauthorized_ResponseFormat(t *testing.T) {
	mw, _ := New(DefaultConfig())
	rec := httptest.NewRecorder()
	mw.unauthorized(rec, "test message")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
}

func TestForbidden_ResponseFormat(t *testing.T) {
	mw, _ := New(DefaultConfig())
	rec := httptest.NewRecorder()
	mw.forbidden(rec, "no access")

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want 403", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
}

// --- Unsupported algorithm in verifySignature ---

func TestVerifySignature_UnsupportedAlgorithm(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	mw := createMiddlewareWithJWKS(t, srv.URL)

	err := mw.verifySignature(
		jwtHeader{Algorithm: "PS256", KeyID: "key1"},
		"input",
		[]byte("sig"),
	)
	if err == nil {
		t.Error("Expected error for unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Error should mention 'unsupported', got: %v", err)
	}
}

// --- JWKS server unreachable ---

func TestFetchJWKS_UnreachableServer(t *testing.T) {
	mw := createMiddlewareWithJWKS(t, "http://127.0.0.1:1/nonexistent")
	err := mw.fetchJWKS()
	if err == nil {
		t.Error("Expected error for unreachable JWKS server")
	}
}

// --- validateToken with introspection URL takes priority ---

func TestValidateToken_IntrospectionPriorityOverJWT(t *testing.T) {
	introspectionCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		introspectionCalled = true
		w.Write([]byte(`{"active":true,"sub":"via-introspection"}`))
	}))
	defer srv.Close()

	mw, err := New(Config{
		Enabled:          true,
		IntrospectionURL: srv.URL,
		JwksURL:          "http://unused",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Token looks like JWT but introspection should be used
	info, err := mw.validateToken(t.Context(), "header.payload.signature")
	if err != nil {
		t.Fatal(err)
	}
	if !introspectionCalled {
		t.Error("Introspection endpoint should have been called")
	}
	if info.Subject != "via-introspection" {
		t.Errorf("Subject = %q, want via-introspection", info.Subject)
	}
}

// --- Wrap with excluded sub-path ---

func TestWrap_ExcludedSubPath(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(jwksHandler(&rsaKey.PublicKey, "key1"))
	defer srv.Close()

	mw, err := New(Config{
		Enabled:       true,
		JwksURL:       srv.URL,
		ExcludePaths:  []string{"/public"},
		CacheDuration: "1h",
	})
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/public/sub/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for excluded sub-path")
	}
}
