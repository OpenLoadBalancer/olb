package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/metrics"
)

// A reload whose middleware cannot be built must be rejected and leave the
// running chain in place, rather than swapping in a chain that silently
// lacks the broken (auth) middleware.
func TestApplyConfig_RejectsUnbuildableMiddleware(t *testing.T) {
	cfg := createTestConfig()
	eng, err := New(cfg, createTempConfigFile(t, cfg))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before := eng.middlewareChain

	newCfg := createTestConfig()
	newCfg.Middleware = &config.MiddlewareConfig{
		BasicAuth: &config.BasicAuthConfig{Enabled: true, Users: map[string]string{"u": "p"}, Hash: "md5"},
	}
	if err := eng.applyConfig(newCfg); err == nil {
		t.Fatal("applyConfig accepted a basic_auth config its constructor rejects")
	}
	if eng.middlewareChain != before {
		t.Error("middleware chain was swapped despite the failed reload")
	}
}

func TestNew_RejectsUnbuildableMiddleware(t *testing.T) {
	cfg := createTestConfig()
	cfg.Middleware = &config.MiddlewareConfig{
		APIKey: &config.APIKeyConfig{Enabled: true, Keys: map[string]string{"k": "v"}, Hash: "md5"},
	}
	if _, err := New(cfg, ""); err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("New error = %v, want an api_key construction error", err)
	}
}

// The config docs give hash: sha256 as the API key default. Omitting it used
// to fail construction and drop API key auth entirely.
func TestAPIKeyMiddleware_DefaultHashEnforcesAuth(t *testing.T) {
	cfg := createTestConfig()
	cfg.Middleware = &config.MiddlewareConfig{
		APIKey: &config.APIKeyConfig{Enabled: true, Keys: map[string]string{"client": "s3cret"}},
	}
	chain, err := createMiddlewareChain(cfg, createTestLogger(t), metrics.NewRegistry())
	if err != nil {
		t.Fatalf("createMiddlewareChain: %v", err)
	}
	h := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("request without key: status %d, want 401", rec.Code)
	}
}

// jwt.DefaultConfig uses HS256; an omitted algorithm used to fail
// construction and drop JWT auth entirely.
func TestJWTMiddleware_DefaultAlgorithmEnforcesAuth(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	cfg := createTestConfig()
	cfg.Middleware = &config.MiddlewareConfig{
		JWT: &config.JWTConfig{Enabled: true, Secret: secret, Required: true},
	}
	chain, err := createMiddlewareChain(cfg, createTestLogger(t), metrics.NewRegistry())
	if err != nil {
		t.Fatalf("createMiddlewareChain: %v", err)
	}
	h := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("request without token: status %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+hs256Token(t, secret))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("request with a valid HS256 token: status %d", rec.Code)
	}
}

func hs256Token(t *testing.T, secret string) string {
	t.Helper()
	return hs256TokenWithClaims(t, secret, map[string]any{"sub": "test", "exp": 4102444800})
}

func hs256TokenWithClaims(t *testing.T, secret string, c map[string]any) string {
	t.Helper()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	payload := enc.EncodeToString(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + enc.EncodeToString(mac.Sum(nil))
}

// strict_transport_policy.enabled: false must turn HSTS off. The field did
// not exist, so the shipped example's "enabled: false" still sent a one-year
// includeSubDomains HSTS header.
func TestSecureHeaders_HSTSEnabledFlag(t *testing.T) {
	off, on := false, true
	for _, tc := range []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"enabled false", &off, false},
		{"enabled true", &on, true},
		{"enabled unset (back-compat)", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestConfig()
			cfg.Middleware = &config.MiddlewareConfig{
				SecureHeaders: &config.SecureHeadersConfig{
					Enabled:               true,
					StrictTransportPolicy: &config.HSTSConfig{Enabled: tc.enabled, MaxAge: 31536000},
				},
			}
			chain, err := createMiddlewareChain(cfg, createTestLogger(t), metrics.NewRegistry())
			if err != nil {
				t.Fatalf("createMiddlewareChain: %v", err)
			}
			rec := httptest.NewRecorder()
			chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
				ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if got := rec.Header().Get("Strict-Transport-Security") != ""; got != tc.want {
				t.Errorf("HSTS header present = %v, want %v", got, tc.want)
			}
		})
	}
}

// A token without exp used to be accepted forever because the engine never
// passed RequireExpiration to the JWT middleware.
func TestJWTMiddleware_RequiresExpirationByDefault(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	noExp := hs256TokenWithClaims(t, secret, map[string]any{"sub": "test"})
	off := false
	for _, tc := range []struct {
		name    string
		require *bool
		wantOK  bool
	}{
		{"default rejects missing exp", nil, false},
		{"require_expiration false accepts it", &off, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestConfig()
			cfg.Middleware = &config.MiddlewareConfig{
				JWT: &config.JWTConfig{
					Enabled: true, Secret: secret, Required: true,
					ClaimsValidation: config.JWTClaimsValidation{RequireExpiration: tc.require},
				},
			}
			chain, err := createMiddlewareChain(cfg, createTestLogger(t), metrics.NewRegistry())
			if err != nil {
				t.Fatalf("createMiddlewareChain: %v", err)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+noExp)
			rec := httptest.NewRecorder()
			chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
			if ok := rec.Code != http.StatusUnauthorized; ok != tc.wantOK {
				t.Errorf("status %d, accepted = %v, want %v", rec.Code, ok, tc.wantOK)
			}
		})
	}
}
