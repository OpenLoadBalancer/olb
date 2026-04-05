package secureheaders

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecureHeaders_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}

	// No security headers should be set
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("X-Frame-Options should not be set when disabled")
	}
}

func TestSecureHeaders_DefaultHeaders(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Check default headers
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("Expected X-Frame-Options DENY, got %s", rec.Header().Get("X-Frame-Options"))
	}

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Expected X-Content-Type-Options nosniff, got %s", rec.Header().Get("X-Content-Type-Options"))
	}

	if rec.Header().Get("X-XSS-Protection") != "1; mode=block" {
		t.Errorf("Expected X-XSS-Protection 1; mode=block, got %s", rec.Header().Get("X-XSS-Protection"))
	}

	if rec.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Errorf("Expected Referrer-Policy strict-origin-when-cross-origin, got %s", rec.Header().Get("Referrer-Policy"))
	}
}

func TestSecureHeaders_HSTS(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.StrictTransportPolicy = &HSTSConfig{
		MaxAge:            31536000,
		IncludeSubdomains: true,
		Preload:           true,
	}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	hsts := rec.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Fatal("HSTS header should be set")
	}

	if hsts != "max-age=31536000; includeSubDomains; preload" {
		t.Errorf("Unexpected HSTS value: %s", hsts)
	}
}

func TestSecureHeaders_CSP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ContentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline'"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if csp != config.ContentSecurityPolicy {
		t.Errorf("Expected CSP %s, got %s", config.ContentSecurityPolicy, csp)
	}
}

func TestSecureHeaders_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/api/public", "/health"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/health/live", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("X-Frame-Options should not be set for excluded paths")
	}
}

func TestSecureHeaders_CustomHeaders(t *testing.T) {
	config := Config{
		Enabled:                       true,
		XFrameOptions:                 "SAMEORIGIN",
		XContentTypeOptions:           false,
		XXSSProtection:                "0",
		ReferrerPolicy:                "no-referrer",
		XPermittedCrossDomainPolicies: "none",
		XDownloadOptions:              "",
		XDNSPrefetchControl:           "on",
		PermissionsPolicy:             "camera=(), microphone=()",
		CrossOriginEmbedderPolicy:     "unsafe-none",
		CrossOriginOpenerPolicy:       "unsafe-none",
		CrossOriginResourcePolicy:     "cross-origin",
	}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("Expected X-Frame-Options SAMEORIGIN, got %s", rec.Header().Get("X-Frame-Options"))
	}

	if rec.Header().Get("X-Content-Type-Options") != "" {
		t.Error("X-Content-Type-Options should not be set when disabled")
	}

	if rec.Header().Get("X-XSS-Protection") != "0" {
		t.Errorf("Expected X-XSS-Protection 0, got %s", rec.Header().Get("X-XSS-Protection"))
	}

	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Expected Referrer-Policy no-referrer, got %s", rec.Header().Get("Referrer-Policy"))
	}

	if rec.Header().Get("X-Permitted-Cross-Domain-Policies") != "none" {
		t.Errorf("Expected X-Permitted-Cross-Domain-Policies none, got %s", rec.Header().Get("X-Permitted-Cross-Domain-Policies"))
	}

	if rec.Header().Get("X-Download-Options") != "" {
		t.Error("X-Download-Options should not be set when empty")
	}

	if rec.Header().Get("X-DNS-Prefetch-Control") != "on" {
		t.Errorf("Expected X-DNS-Prefetch-Control on, got %s", rec.Header().Get("X-DNS-Prefetch-Control"))
	}

	if rec.Header().Get("Permissions-Policy") != "camera=(), microphone=()" {
		t.Errorf("Expected Permissions-Policy camera=(), microphone=(), got %s", rec.Header().Get("Permissions-Policy"))
	}
}

func TestSecureHeaders_CacheControl(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.CacheControl = "no-store, max-age=0"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Errorf("Expected Cache-Control no-store, max-age=0, got %s", rec.Header().Get("Cache-Control"))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.XFrameOptions != "DENY" {
		t.Errorf("Default XFrameOptions should be DENY, got %s", config.XFrameOptions)
	}
	if !config.XContentTypeOptions {
		t.Error("Default XContentTypeOptions should be true")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 750 {
		t.Errorf("Expected priority 750, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "secureheaders" {
		t.Errorf("Expected name 'secureheaders', got '%s'", mw.Name())
	}
}

func TestBuildHSTS(t *testing.T) {
	mw := New(DefaultConfig())

	// Test nil HSTS config
	if mw.buildHSTS() != "" {
		t.Error("HSTS should be empty when nil")
	}

	// Test basic HSTS
	mw.config.StrictTransportPolicy = &HSTSConfig{
		MaxAge: 3600,
	}
	if mw.buildHSTS() != "max-age=3600" {
		t.Errorf("Expected max-age=3600, got %s", mw.buildHSTS())
	}

	// Test with includeSubDomains
	mw.config.StrictTransportPolicy.IncludeSubdomains = true
	if mw.buildHSTS() != "max-age=3600; includeSubDomains" {
		t.Errorf("Expected max-age=3600; includeSubDomains, got %s", mw.buildHSTS())
	}

	// Test with preload
	mw.config.StrictTransportPolicy.Preload = true
	if mw.buildHSTS() != "max-age=3600; includeSubDomains; preload" {
		t.Errorf("Expected max-age=3600; includeSubDomains; preload, got %s", mw.buildHSTS())
	}
}

func TestRecommendedConfig(t *testing.T) {
	config := RecommendedConfig()

	if !config.Enabled {
		t.Error("RecommendedConfig should have Enabled=true")
	}
	if config.XFrameOptions != "DENY" {
		t.Error("RecommendedConfig should have XFrameOptions=DENY")
	}
	if config.StrictTransportPolicy == nil {
		t.Fatal("RecommendedConfig should have HSTS")
	}
	if config.StrictTransportPolicy.MaxAge != 31536000 {
		t.Errorf("RecommendedConfig HSTS max-age should be 31536000, got %d", config.StrictTransportPolicy.MaxAge)
	}
}

func TestPermissiveConfig(t *testing.T) {
	config := PermissiveConfig()

	if !config.Enabled {
		t.Error("PermissiveConfig should have Enabled=true")
	}
	if config.XFrameOptions != "SAMEORIGIN" {
		t.Errorf("PermissiveConfig should have XFrameOptions=SAMEORIGIN, got %s", config.XFrameOptions)
	}
	if config.StrictTransportPolicy != nil {
		t.Error("PermissiveConfig should not have HSTS")
	}
}

func TestSecureHeaders_CrossOriginPolicies(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.CrossOriginEmbedderPolicy = "require-corp"
	config.CrossOriginOpenerPolicy = "same-origin"
	config.CrossOriginResourcePolicy = "same-site"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Cross-Origin-Embedder-Policy") != "require-corp" {
		t.Errorf("Unexpected COEP: %s", rec.Header().Get("Cross-Origin-Embedder-Policy"))
	}

	if rec.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" {
		t.Errorf("Unexpected COOP: %s", rec.Header().Get("Cross-Origin-Opener-Policy"))
	}

	if rec.Header().Get("Cross-Origin-Resource-Policy") != "same-site" {
		t.Errorf("Unexpected CORP: %s", rec.Header().Get("Cross-Origin-Resource-Policy"))
	}
}
