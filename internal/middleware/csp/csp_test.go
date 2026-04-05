package csp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSP_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when CSP is disabled")
	}

	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("CSP header should not be set when disabled")
	}
}

func TestCSP_DefaultPolicy(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("CSP header should be set")
	}

	// Check for expected directives
	if !strings.Contains(csp, "default-src 'self'") {
		t.Error("CSP should contain default-src 'self'")
	}

	if !strings.Contains(csp, "upgrade-insecure-requests") {
		t.Error("CSP should contain upgrade-insecure-requests")
	}
}

func TestCSP_CustomDirectives(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.DefaultSrc = []string{"'self'", "https://cdn.example.com"}
	config.ScriptSrc = []string{"'self'", "https://scripts.example.com"}
	config.ImgSrc = []string{"'self'", "data:", "https://images.example.com"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "https://cdn.example.com") {
		t.Error("CSP should contain custom default-src")
	}

	if !strings.Contains(csp, "https://scripts.example.com") {
		t.Error("CSP should contain custom script-src")
	}

	if !strings.Contains(csp, "data:") {
		t.Error("CSP should contain data: for img-src")
	}
}

func TestCSP_UnsafeInline(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.UnsafeInline = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "'unsafe-inline'") {
		t.Error("CSP should contain 'unsafe-inline'")
	}
}

func TestCSP_UnsafeEval(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.UnsafeEval = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "'unsafe-eval'") {
		t.Error("CSP should contain 'unsafe-eval'")
	}
}

func TestCSP_ReportURI(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ReportURI = "/csp-report"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "report-uri /csp-report") {
		t.Error("CSP should contain report-uri")
	}
}

func TestCSP_BlockAllMixedContent(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.BlockAllMixed = true
	config.UpgradeInsecure = false

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "block-all-mixed-content") {
		t.Error("CSP should contain block-all-mixed-content")
	}

	if strings.Contains(csp, "upgrade-insecure-requests") {
		t.Error("CSP should not contain upgrade-insecure-requests when disabled")
	}
}

func TestCSP_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/api/public", "/health"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("CSP header should not be set for excluded paths")
	}
}

func TestCSP_WithNonce(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.NonceScript = true
	config.NonceStyle = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "'nonce-") {
		t.Error("CSP should contain nonce")
	}

	if rec.Header().Get("X-Script-Nonce") == "" {
		t.Error("X-Script-Nonce header should be set")
	}

	if rec.Header().Get("X-Style-Nonce") == "" {
		t.Error("X-Style-Nonce header should be set")
	}
}

func TestCSP_FrameAncestors(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.FrameAncestors = []string{"'none'"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Error("CSP should contain frame-ancestors 'none'")
	}
}

func TestCSP_FormAction(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.FormAction = []string{"'self'", "https://forms.example.com"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "form-action") {
		t.Error("CSP should contain form-action directive")
	}
}

func TestCSP_BaseURI(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.BaseURI = []string{"'self'"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "base-uri") {
		t.Error("CSP should contain base-uri directive")
	}
}

func TestCSP_ObjectSrcNone(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ObjectSrc = []string{"'none'"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "object-src 'none'") {
		t.Error("CSP should contain object-src 'none'")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if len(config.DefaultSrc) != 1 || config.DefaultSrc[0] != "'self'" {
		t.Error("Default DefaultSrc should be ['self']")
	}
	if config.UpgradeInsecure != true {
		t.Error("Default UpgradeInsecure should be true")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 610 {
		t.Errorf("Expected priority 610, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "csp" {
		t.Errorf("Expected name 'csp', got '%s'", mw.Name())
	}
}

func TestGetPolicy(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.DefaultSrc = []string{"'self'"}

	mw, _ := New(config)

	policy := mw.GetPolicy()
	if policy == "" {
		t.Error("GetPolicy should return non-empty policy")
	}

	if !strings.Contains(policy, "default-src") {
		t.Error("Policy should contain default-src")
	}
}

func TestCSP_ReportTo(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ReportTo = "csp-endpoint"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "report-to csp-endpoint") {
		t.Error("CSP should contain report-to directive")
	}
}

func TestCSP_MediaSrc(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.MediaSrc = []string{"'self'", "https://media.example.com"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "media-src") {
		t.Error("CSP should contain media-src directive")
	}
}

func TestCSP_FrameSrc(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.FrameSrc = []string{"'self'", "https://frames.example.com"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "frame-src") {
		t.Error("CSP should contain frame-src directive")
	}
}

func TestCSP_ConnectSrc(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ConnectSrc = []string{"'self'", "https://api.example.com"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "connect-src") {
		t.Error("CSP should contain connect-src directive")
	}
}

func TestCSP_FontSrc(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.FontSrc = []string{"'self'", "https://fonts.example.com"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "font-src") {
		t.Error("CSP should contain font-src directive")
	}
}
