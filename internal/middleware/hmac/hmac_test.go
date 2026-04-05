package hmac

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHMAC_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when HMAC is disabled")
	}
}

func TestHMAC_MissingSignature(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without signature")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestHMAC_InvalidSignature(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid signature")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Signature", "invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestHMAC_ValidSignature(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate valid signature
	message := "GET\n/test\n"
	sig, err := GenerateSignature("secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid signature")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestHMAC_WithBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"
	config.UseBody = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"name":"test"}`
	// Message includes method, path, newline, then body
	message := "POST\n/test\n" + body
	sig, err := GenerateSignature("secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	var receivedBody string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedBody != body {
		t.Errorf("Body should be preserved, got %s", receivedBody)
	}
}

func TestHMAC_WithQueryString(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Message includes method, path, query string, newline
	message := "GET\n/test\npage=1&limit=10\n"
	sig, err := GenerateSignature("secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test?page=1&limit=10", nil)
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid signature")
	}
}

func TestHMAC_WithPrefix(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"
	config.Prefix = "sha256="

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	message := "GET\n/test\n"
	sig, err := GenerateSignature("secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Signature", "sha256="+sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid prefixed signature")
	}
}

func TestHMAC_Base64Encoding(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"
	config.Encoding = "base64"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	message := "GET\n/test\n"
	sig, err := GenerateSignature("secret", message, "sha256", "base64")
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid base64 signature")
	}
}

func TestHMAC_SHA512(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"
	config.Algorithm = "sha512"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	message := "GET\n/test\n"
	sig, err := GenerateSignature("secret", message, "sha512", "hex")
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid sha512 signature")
	}
}

func TestHMAC_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/metrics"}
	config.Secret = "secret"

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

func TestHMAC_CustomHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"
	config.Header = "X-HMAC-Signature"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	message := "GET\n/test\n"
	sig, err := GenerateSignature("secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-HMAC-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with custom header")
	}
}

func TestHMAC_WithoutBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"
	config.UseBody = false

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Message without body
	message := "POST\n/test\n"
	sig, err := GenerateSignature("secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	var receivedBody string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"name":"test"}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedBody != body {
		t.Errorf("Body should be preserved, got %s", receivedBody)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Algorithm != "sha256" {
		t.Errorf("Default Algorithm should be 'sha256', got '%s'", config.Algorithm)
	}
	if config.Header != "X-Signature" {
		t.Errorf("Default Header should be 'X-Signature', got '%s'", config.Header)
	}
	if config.Encoding != "hex" {
		t.Errorf("Default Encoding should be 'hex', got '%s'", config.Encoding)
	}
	if config.UseBody != true {
		t.Error("Default UseBody should be true")
	}
	if config.MaxAge != "5m" {
		t.Errorf("Default MaxAge should be '5m', got '%s'", config.MaxAge)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 213 {
		t.Errorf("Expected priority 213, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "hmac" {
		t.Errorf("Expected name 'hmac', got '%s'", mw.Name())
	}
}

func TestGenerateSignature(t *testing.T) {
	tests := []struct {
		algorithm string
		encoding  string
	}{
		{"sha256", "hex"},
		{"sha256", "base64"},
		{"sha512", "hex"},
		{"sha512", "base64"},
	}

	for _, tt := range tests {
		sig, err := GenerateSignature("secret", "message", tt.algorithm, tt.encoding)
		if err != nil {
			t.Errorf("GenerateSignature(%s, %s) error: %v", tt.algorithm, tt.encoding, err)
			continue
		}
		if sig == "" {
			t.Error("Generated signature should not be empty")
		}
	}
}

func TestGenerateSignature_Defaults(t *testing.T) {
	// Test with empty algorithm and encoding (should use defaults)
	sig, err := GenerateSignature("secret", "message", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sig == "" {
		t.Error("Generated signature should not be empty")
	}
}

func TestHMAC_WrongSecret(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "correct-secret"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Generate signature with wrong secret
	message := "GET\n/test\n"
	sig, err := GenerateSignature("wrong-secret", message, "sha256", "hex")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with wrong secret")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func BenchmarkHMAC_Verification(b *testing.B) {
	config := DefaultConfig()
	config.Enabled = true
	config.Secret = "secret"

	mw, _ := New(config)

	message := "GET\n/test\n"
	sig, _ := GenerateSignature("secret", message, "sha256", "hex")

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Signature", sig)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkGenerateSignature(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateSignature("secret", "message", "sha256", "hex")
	}
}
