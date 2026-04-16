package jwt

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"hash"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJWT_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when JWT is disabled")
	}
}

func TestJWT_MissingToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Required = true
	config.Secret = "test-secret"

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

func TestJWT_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Required = true
	config.ExcludePaths = []string{"/health"}

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
}

func TestJWT_InvalidFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Required = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "invalid-token-format")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestClaims_Context(t *testing.T) {
	claims := &Claims{
		Subject:  "user123",
		Issuer:   "test-issuer",
		Audience: []string{"test-audience"},
	}

	// Test WithClaims and GetClaims
	ctx := WithClaims(t.Context(), claims)
	retrieved := GetClaims(ctx)

	if retrieved == nil {
		t.Fatal("GetClaims returned nil")
	}

	if retrieved.Subject != claims.Subject {
		t.Errorf("Subject mismatch: got %s, want %s", retrieved.Subject, claims.Subject)
	}

	// Test GetSubject
	subject := GetSubject(ctx)
	if subject != claims.Subject {
		t.Errorf("GetSubject returned %s, want %s", subject, claims.Subject)
	}

	// Test GetAudience
	audience := GetAudience(ctx)
	if len(audience) != 1 || audience[0] != "test-audience" {
		t.Errorf("GetAudience returned %v, want [test-audience]", audience)
	}
}

func TestClaims_NoClaims(t *testing.T) {
	// Test GetClaims without setting claims
	ctx := t.Context()
	claims := GetClaims(ctx)
	if claims != nil {
		t.Error("GetClaims should return nil when no claims are set")
	}

	// Test GetSubject without claims
	subject := GetSubject(ctx)
	if subject != "" {
		t.Errorf("GetSubject should return empty string, got %s", subject)
	}

	// Test GetAudience without claims
	audience := GetAudience(ctx)
	if audience != nil {
		t.Errorf("GetAudience should return nil, got %v", audience)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Algorithm != "HS256" {
		t.Errorf("Default Algorithm should be HS256, got %s", config.Algorithm)
	}
	if config.Header != "Authorization" {
		t.Errorf("Default Header should be Authorization, got %s", config.Header)
	}
	if config.Prefix != "Bearer " {
		t.Errorf("Default Prefix should be 'Bearer ', got %s", config.Prefix)
	}
	if config.Required != true {
		t.Error("Default Required should be true")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 210 {
		t.Errorf("Expected priority 210, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "jwt" {
		t.Errorf("Expected name 'jwt', got %s", mw.Name())
	}
}

func TestExtractToken(t *testing.T) {
	config := DefaultConfig()
	config.Prefix = "Bearer "
	mw, _ := New(config)

	tests := []struct {
		name      string
		header    string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			header:    "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantToken: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantErr:   false,
		},
		{
			name:      "missing prefix",
			header:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "empty header",
			header:    "",
			wantToken: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
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

func TestValidateToken_InvalidFormat(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	// Test invalid token formats
	invalidTokens := []string{
		"invalid",
		"part1.part2",             // Only 2 parts
		"part1.part2.part3.part4", // 4 parts
		"",                        // Empty
	}

	for _, token := range invalidTokens {
		_, err := mw.validateToken(token)
		if err == nil {
			t.Errorf("validateToken(%q) should return error", token)
		}
	}
}

// generateTestToken creates a valid JWT token for testing.
func generateTestToken(header, claims map[string]any, secret string) string {
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	data := headerB64 + "." + claimsB64

	// Compute signature
	var hasher func() hash.Hash
	alg, _ := header["alg"].(string)
	switch alg {
	case "HS384":
		hasher = sha512.New384
	case "HS512":
		hasher = sha512.New
	default:
		hasher = sha256.New
	}

	h := hmac.New(hasher, []byte(secret))
	h.Write([]byte(data))
	sig := h.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return data + "." + sigB64
}

func TestValidateToken_ValidToken(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate valid token
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iss": "test-issuer",
			"aud": []string{"test-audience"},
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	claims, err := mw.validateToken(token)
	if err != nil {
		t.Errorf("validateToken() returned error: %v", err)
	}

	if claims == nil {
		t.Fatal("validateToken() returned nil claims")
	}

	if claims.Subject != "user123" {
		t.Errorf("Subject = %s, want user123", claims.Subject)
	}

	if claims.Issuer != "test-issuer" {
		t.Errorf("Issuer = %s, want test-issuer", claims.Issuer)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate token with wrong secret
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now,
			"exp": now + 3600,
		},
		"wrong-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with invalid signature")
	}
}

func TestValidateToken_AlgorithmMismatch(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate token with different algorithm
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS384", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with algorithm mismatch")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate expired token
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now - 7200,
			"exp": now - 3600, // Expired 1 hour ago
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with expired token")
	}
}

func TestValidateToken_NotBefore(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate token not yet valid
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now,
			"exp": now + 7200,
			"nbf": now + 3600, // Not valid for 1 hour
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with nbf in future")
	}
}

func TestValidateToken_InvalidIssuer(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.ClaimsValidation.Issuer = "expected-issuer"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate token with wrong issuer
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iss": "wrong-issuer",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with wrong issuer")
	}
}

func TestValidateToken_InvalidAudience(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.ClaimsValidation.Audience = "expected-audience"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate token with wrong audience
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"aud": []string{"wrong-audience"},
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with wrong audience")
	}
}

func TestJWT_ValidTokenRequest(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate valid token
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	var receivedSubject string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSubject = GetSubject(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedSubject != "user123" {
		t.Errorf("Expected subject 'user123', got %s", receivedSubject)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.Required = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate token with wrong secret
	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now,
			"exp": now + 3600,
		},
		"wrong-secret",
	)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestJWT_Optional_NotProvided(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.Required = false // Token not required

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
	// No Authorization header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when JWT is optional and not provided")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestComputeHMAC(t *testing.T) {
	data := "test-data"
	secret := "test-secret"

	// Test HS256
	sig256 := computeHMAC(data, secret, "HS256")
	if len(sig256) != 32 {
		t.Errorf("HS256 signature length = %d, want 32", len(sig256))
	}

	// Test HS384
	sig384 := computeHMAC(data, secret, "HS384")
	if len(sig384) != 48 {
		t.Errorf("HS384 signature length = %d, want 48", len(sig384))
	}

	// Test HS512
	sig512 := computeHMAC(data, secret, "HS512")
	if len(sig512) != 64 {
		t.Errorf("HS512 signature length = %d, want 64", len(sig512))
	}

	// Test determinism
	sig256_2 := computeHMAC(data, secret, "HS256")
	if !hmac.Equal(sig256, sig256_2) {
		t.Error("computeHMAC should be deterministic")
	}

	// Different data should produce different signature
	sigDifferent := computeHMAC("different-data", secret, "HS256")
	if hmac.Equal(sig256, sigDifferent) {
		t.Error("Different data should produce different signatures")
	}
}

// generateEd25519TestToken creates a valid EdDSA JWT token for testing.
func generateEd25519TestToken(header, claims map[string]any, privateKey ed25519.PrivateKey) string {
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	data := headerB64 + "." + claimsB64

	sig := ed25519.Sign(privateKey, []byte(data))
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return data + "." + sigB64
}

func TestNew_EdDSA_ValidPublicKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "EdDSA"
	config.PublicKey = pub

	mw, err := New(config)
	if err != nil {
		t.Fatalf("New() with valid Ed25519 key returned error: %v", err)
	}
	if mw == nil {
		t.Fatal("New() returned nil middleware")
	}
}

func TestNew_EdDSA_InvalidPublicKeySize(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "EdDSA"
	config.PublicKey = []byte("too-short-key")

	mw, err := New(config)
	if err == nil {
		t.Error("New() should fail with invalid Ed25519 public key size")
	}
	if mw != nil {
		t.Error("New() should return nil middleware on error")
	}
	if err.Error() != "invalid Ed25519 public key" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestNew_EdDSA_EmptyPublicKey(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "EdDSA"
	config.PublicKey = nil

	mw, err := New(config)
	if err == nil {
		t.Error("New() should fail with nil Ed25519 public key")
	}
	if mw != nil {
		t.Error("New() should return nil middleware on error")
	}
}

func TestValidateToken_EdDSA_Valid(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "" // Not used for EdDSA
	config.Algorithm = "EdDSA"
	config.PublicKey = pub

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateEd25519TestToken(
		map[string]any{"alg": "EdDSA", "typ": "JWT"},
		map[string]any{
			"sub": "user-eddsa",
			"iss": "test-issuer",
			"iat": now,
			"exp": now + 3600,
		},
		priv,
	)

	claims, err := mw.validateToken(token)
	if err != nil {
		t.Fatalf("validateToken() returned error: %v", err)
	}
	if claims.Subject != "user-eddsa" {
		t.Errorf("Subject = %s, want user-eddsa", claims.Subject)
	}
	if claims.Issuer != "test-issuer" {
		t.Errorf("Issuer = %s, want test-issuer", claims.Issuer)
	}
}

func TestValidateToken_EdDSA_InvalidSignature(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Different key for signing
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "EdDSA"
	config.PublicKey = pub

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateEd25519TestToken(
		map[string]any{"alg": "EdDSA", "typ": "JWT"},
		map[string]any{
			"sub": "user-eddsa",
			"iat": now,
			"exp": now + 3600,
		},
		wrongPriv,
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with wrong Ed25519 signature")
	}
}

func TestValidateToken_EdDSA_InvalidSignatureBase64(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "EdDSA"
	config.PublicKey = pub

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Construct token with invalid base64 in signature part
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT"}`))
	claimsB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user"}`))
	invalidToken := headerB64 + "." + claimsB64 + ".!!!invalid-base64!!!"

	_, err = mw.validateToken(invalidToken)
	if err == nil {
		t.Error("validateToken() should fail with invalid base64 signature")
	}
}

func TestVerifySignature_UnsupportedAlgorithm(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "RS256" // Unsupported
	config.Secret = "test-secret"

	_, err := New(config)
	if err == nil {
		t.Fatal("New() should reject unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported JWT algorithm") {
		t.Errorf("Expected 'unsupported JWT algorithm', got: %v", err)
	}
}

func TestJWT_EdDSA_FullRequest(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Enabled = true
	config.Algorithm = "EdDSA"
	config.PublicKey = pub

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateEd25519TestToken(
		map[string]any{"alg": "EdDSA", "typ": "JWT"},
		map[string]any{
			"sub": "eddsa-user",
			"iat": now,
			"exp": now + 3600,
		},
		priv,
	)

	var receivedSubject string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSubject = GetSubject(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if receivedSubject != "eddsa-user" {
		t.Errorf("Expected subject 'eddsa-user', got %s", receivedSubject)
	}
}

func TestJWT_EdDSA_InvalidSignatureRequest(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Enabled = true
	config.Required = true
	config.Algorithm = "EdDSA"
	config.PublicKey = pub

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateEd25519TestToken(
		map[string]any{"alg": "EdDSA", "typ": "JWT"},
		map[string]any{
			"sub": "eddsa-user",
			"iat": now,
			"exp": now + 3600,
		},
		wrongPriv,
	)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid EdDSA token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestValidateToken_HS384(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS384"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS384", "typ": "JWT"},
		map[string]any{
			"sub": "user384",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	claims, err := mw.validateToken(token)
	if err != nil {
		t.Fatalf("validateToken() HS384 returned error: %v", err)
	}
	if claims.Subject != "user384" {
		t.Errorf("Subject = %s, want user384", claims.Subject)
	}
}

func TestValidateToken_HS512(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS512"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS512", "typ": "JWT"},
		map[string]any{
			"sub": "user512",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	claims, err := mw.validateToken(token)
	if err != nil {
		t.Fatalf("validateToken() HS512 returned error: %v", err)
	}
	if claims.Subject != "user512" {
		t.Errorf("Subject = %s, want user512", claims.Subject)
	}
}

func TestValidateToken_InvalidHeaderBase64(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Token with invalid base64 in header part
	_, err = mw.validateToken("!!!invalid!!!." + "eyJzdWIiOiJ1c2VyIn0." + "c2ln")
	if err == nil {
		t.Error("validateToken() should fail with invalid header base64")
	}
}

func TestValidateToken_InvalidHeaderJSON(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Valid base64 but invalid JSON in header
	invalidJSON := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	claimsB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user"}`))
	_, err = mw.validateToken(invalidJSON + "." + claimsB64 + ".c2ln")
	if err == nil {
		t.Error("validateToken() should fail with invalid header JSON")
	}
}

func TestValidateToken_InvalidClaimsBase64(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	_, err = mw.validateToken(headerB64 + ".!!!invalid!!!.c2ln")
	if err == nil {
		t.Error("validateToken() should fail with invalid claims base64")
	}
}

func TestValidateToken_InvalidClaimsJSON(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	invalidClaims := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	_, err = mw.validateToken(headerB64 + "." + invalidClaims + ".c2ln")
	if err == nil {
		t.Error("validateToken() should fail with invalid claims JSON")
	}
}

func TestValidateToken_InvalidSignatureBase64(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claimsJSON, _ := json.Marshal(map[string]any{"sub": "user", "iat": now, "exp": now + 3600})
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	_, err = mw.validateToken(headerB64 + "." + claimsB64 + ".!!!invalid-base64!!!")
	if err == nil {
		t.Error("validateToken() should fail with invalid signature base64")
	}
}

func TestValidateToken_ValidIssuer(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.ClaimsValidation.Issuer = "correct-issuer"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iss": "correct-issuer",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	claims, err := mw.validateToken(token)
	if err != nil {
		t.Fatalf("validateToken() should succeed with correct issuer: %v", err)
	}
	if claims.Issuer != "correct-issuer" {
		t.Errorf("Issuer = %s, want correct-issuer", claims.Issuer)
	}
}

func TestValidateToken_ValidAudience(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.ClaimsValidation.Audience = "expected-audience"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"aud": []string{"other-audience", "expected-audience"},
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	claims, err := mw.validateToken(token)
	if err != nil {
		t.Fatalf("validateToken() should succeed with matching audience: %v", err)
	}
	if len(claims.Audience) != 2 {
		t.Errorf("Expected 2 audiences, got %d", len(claims.Audience))
	}
}

func TestValidateToken_AudienceMissingFromClaims(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.ClaimsValidation.Audience = "expected-audience"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail when audience required but missing from token")
	}
}

func TestValidateToken_IssuedAtInFuture(t *testing.T) {
	config := DefaultConfig()
	config.Secret = "test-secret"
	config.Algorithm = "HS256"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "user123",
			"iat": now + 3600, // Issued in the future
			"exp": now + 7200,
		},
		"test-secret",
	)

	_, err = mw.validateToken(token)
	if err == nil {
		t.Error("validateToken() should fail with iat in future")
	}
}

func TestJWT_CustomHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.Header = "X-Custom-Auth"
	config.Prefix = "Token "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "custom-header-user",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	var receivedSubject string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSubject = GetSubject(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-Auth", "Token "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if receivedSubject != "custom-header-user" {
		t.Errorf("Expected subject 'custom-header-user', got %s", receivedSubject)
	}
}

func TestJWT_NoPrefix(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "test-secret"
	config.Algorithm = "HS256"
	config.Header = "Authorization"
	config.Prefix = "" // No prefix

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	token := generateTestToken(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{
			"sub": "noprefix-user",
			"iat": now,
			"exp": now + 3600,
		},
		"test-secret",
	)

	var receivedSubject string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSubject = GetSubject(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", token) // No prefix, just the token
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if receivedSubject != "noprefix-user" {
		t.Errorf("Expected subject 'noprefix-user', got %s", receivedSubject)
	}
}

func TestUnauthorized_ResponseFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Required = true
	config.Secret = "test-secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}

	// Check WWW-Authenticate header
	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth != "Bearer" {
		t.Errorf("Expected WWW-Authenticate 'Bearer', got %q", wwwAuth)
	}

	// Check response body contains expected JSON
	body := rec.Body.String()
	if body == "" {
		t.Error("Response body should not be empty")
	}
}

func TestJWT_ClaimsID(t *testing.T) {
	claims := &Claims{
		Subject: "user123",
		JWTID:   "unique-token-id",
	}

	ctx := WithClaims(t.Context(), claims)
	retrieved := GetClaims(ctx)
	if retrieved == nil {
		t.Fatal("GetClaims returned nil")
	}
	if retrieved.JWTID != "unique-token-id" {
		t.Errorf("JWTID = %s, want unique-token-id", retrieved.JWTID)
	}
}

func TestUnauthorized_SafeJSONOutput(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "test-secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	// Simulate a message with JSON-breaking characters
	mw.unauthorized(rec, `injection","extra":"bad`)

	body := rec.Body.String()
	// The response must be valid JSON (json.Marshal escapes special chars)
	var parsed map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, body)
	}
	if parsed["message"] != `injection","extra":"bad` {
		t.Errorf("message not properly escaped, got: %s", parsed["message"])
	}
}
