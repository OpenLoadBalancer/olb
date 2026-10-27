package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/metrics"
)

func serve(t *testing.T, mw *config.MiddlewareConfig, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	cfg := createTestConfig()
	cfg.Middleware = mw
	chain, err := createMiddlewareChain(cfg, createTestLogger(t), metrics.NewRegistry())
	if err != nil {
		t.Fatalf("createMiddlewareChain: %v", err)
	}
	rec := httptest.NewRecorder()
	chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-RemoteAddr", r.RemoteAddr)
	})).ServeHTTP(rec, req)
	return rec
}

// A request_id block that only sets the header name used to switch off
// generation and the response header, so no ID was produced at all.
func TestRequestID_BlockWithoutFlagsKeepsDefaults(t *testing.T) {
	rec := serve(t, &config.MiddlewareConfig{
		RequestID: &config.RequestIDConfig{Enabled: true, Header: "X-Correlation-ID"},
	}, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("X-Correlation-ID") == "" {
		t.Error("no request ID in the response; generate/response defaults were lost")
	}

	off := false
	rec = serve(t, &config.MiddlewareConfig{
		RequestID: &config.RequestIDConfig{Enabled: true, Response: &off},
	}, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("X-Request-ID") != "" {
		t.Error("response: false was ignored")
	}
}

// real_ip with only trusted_proxies used to look at no header at all.
func TestRealIP_TrustedProxiesOnlyUsesDefaultHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := serve(t, &config.MiddlewareConfig{
		RealIP: &config.RealIPConfig{Enabled: true, TrustedProxies: []string{"10.0.0.0/8"}},
	}, req)
	if got := rec.Header().Get("X-Seen-RemoteAddr"); got != "203.0.113.9" && got != "203.0.113.9:4000" {
		t.Errorf("downstream saw %q; the forwarded client IP was not applied", got)
	}
}

// hash: plain needs allow_plaintext, which the engine config could not
// express, so it always failed to start.
func TestBasicAuth_PlainHashWithAllowPlaintext(t *testing.T) {
	mw := func(allow bool) *config.MiddlewareConfig {
		return &config.MiddlewareConfig{BasicAuth: &config.BasicAuthConfig{
			Enabled: true, Hash: "plain", AllowPlaintext: allow, Users: map[string]string{"admin": "pw"},
		}}
	}
	cfg := createTestConfig()
	cfg.Middleware = mw(false)
	if _, err := createMiddlewareChain(cfg, createTestLogger(t), metrics.NewRegistry()); err == nil {
		t.Fatal("plaintext passwords were accepted without allow_plaintext")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "pw")
	if rec := serve(t, mw(true), req); rec.Code != http.StatusOK {
		t.Errorf("valid credentials with allow_plaintext: status %d", rec.Code)
	}
	if rec := serve(t, mw(true), httptest.NewRequest(http.MethodGet, "/", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("no credentials: status %d, want 401", rec.Code)
	}
}
