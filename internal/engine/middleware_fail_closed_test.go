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
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{"sub": "test", "exp": 4102444800})
	if err != nil {
		t.Fatal(err)
	}
	payload := enc.EncodeToString(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + enc.EncodeToString(mac.Sum(nil))
}
