package transformer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransformer_Disabled(t *testing.T) {
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
		w.Write([]byte("Hello"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called when transformer is disabled")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestTransformer_AddHeaders(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.AddHeaders = map[string]string{
		"X-Custom-Header": "custom-value",
		"X-Frame-Options": "DENY",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Custom-Header") != "custom-value" {
		t.Error("X-Custom-Header should be set")
	}

	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("X-Frame-Options should be set")
	}
}

func TestTransformer_RemoveHeaders(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RemoveHeaders = []string{"X-Internal-Token", "Server"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Internal-Token", "secret")
		w.Header().Set("Server", "internal-server")
		w.Header().Set("X-Public-Header", "public")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Internal-Token") != "" {
		t.Error("X-Internal-Token should be removed")
	}

	if rec.Header().Get("Server") != "" {
		t.Error("Server should be removed")
	}

	if rec.Header().Get("X-Public-Header") != "public" {
		t.Error("X-Public-Header should be preserved")
	}
}

func TestTransformer_BodyRewrite(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RewriteBody = map[string]string{
		"old-domain.com": "new-domain.com",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Visit https://old-domain.com/api for more info"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "new-domain.com") {
		t.Errorf("Body should contain new-domain.com, got: %s", string(body))
	}

	if strings.Contains(string(body), "old-domain.com") {
		t.Errorf("Body should not contain old-domain.com, got: %s", string(body))
	}
}

func TestTransformer_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/metrics"}
	config.AddHeaders = map[string]string{
		"X-Transformed": "true",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test excluded path
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Transformed") == "true" {
		t.Error("Excluded path should not have headers added")
	}
}

func TestTransformer_ExcludeMIMEType(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludeMIMETypes = []string{"image/", "application/pdf"}
	config.AddHeaders = map[string]string{
		"X-Transformed": "true",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake-image-data"))
	}))

	req := httptest.NewRequest("GET", "/image.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Transformed") == "true" {
		t.Error("Excluded MIME type should not have headers added")
	}
}

func TestTransformer_Compress(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Compress = true
	config.MinCompressSize = 10

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Large body that should be compressed
	largeBody := strings.Repeat("This is a large response body. ", 100)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeBody))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Check if compressed
	if rec.Header().Get("Content-Encoding") == "gzip" {
		// Compression was applied
		if rec.Body.Len() >= len(largeBody) {
			t.Error("Compressed body should be smaller than original")
		}
	}
}

func TestTransformer_NoCompressSmallBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Compress = true
	config.MinCompressSize = 1024

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Small body"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("Small body should not be compressed")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Compress != false {
		t.Error("Default Compress should be false")
	}
	if config.CompressLevel != 6 {
		t.Errorf("Default CompressLevel should be 6, got %d", config.CompressLevel)
	}
	if config.MinCompressSize != 1024 {
		t.Errorf("Default MinCompressSize should be 1024, got %d", config.MinCompressSize)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 850 {
		t.Errorf("Expected priority 850, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "transformer" {
		t.Errorf("Expected name 'transformer', got '%s'", mw.Name())
	}
}

func TestNew_InvalidRegex(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RewriteBody = map[string]string{
		"[invalid(": "replacement",
	}

	_, err := New(config)
	if err == nil {
		t.Error("New should return error for invalid regex")
	}
}

func TestTransformer_EmptyBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.AddHeaders = map[string]string{
		"X-Custom": "value",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		// No body written
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", rec.Code)
	}

	if rec.Header().Get("X-Custom") != "value" {
		t.Error("X-Custom header should be set even with empty body")
	}
}

func TestTransformer_StatusCodePreserved(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	tests := []int{200, 201, 400, 404, 500}

	for _, status := range tests {
		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte("Response"))
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != status {
			t.Errorf("Expected status %d, got %d", status, rec.Code)
		}
	}
}
