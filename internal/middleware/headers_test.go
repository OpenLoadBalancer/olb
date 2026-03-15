package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeadersMiddleware_Name(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{})
	if m.Name() != "headers" {
		t.Errorf("expected name 'headers', got '%s'", m.Name())
	}
}

func TestHeadersMiddleware_Priority(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{})
	if m.Priority() != PriorityHeaders {
		t.Errorf("expected priority %d, got %d", PriorityHeaders, m.Priority())
	}
}

func TestHeadersMiddleware_RequestAdd(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		RequestAdd: map[string]string{
			"X-Custom-Header": "custom-value",
			"X-Another":       "another-value",
		},
	})

	var capturedReq *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if capturedReq == nil {
		t.Fatal("request was not captured")
	}

	if got := capturedReq.Header.Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("expected X-Custom-Header 'custom-value', got '%s'", got)
	}

	if got := capturedReq.Header.Get("X-Another"); got != "another-value" {
		t.Errorf("expected X-Another 'another-value', got '%s'", got)
	}
}

func TestHeadersMiddleware_RequestSet(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		RequestSet: map[string]string{
			"X-Existing": "new-value",
		},
	})

	var capturedReq *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Existing", "old-value")
	req.Header.Add("X-Existing", "another-old-value")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if capturedReq == nil {
		t.Fatal("request was not captured")
	}

	// Set should replace all existing values
	values := capturedReq.Header["X-Existing"]
	if len(values) != 1 || values[0] != "new-value" {
		t.Errorf("expected X-Existing to be ['new-value'], got %v", values)
	}
}

func TestHeadersMiddleware_RequestRemove(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		RequestRemove: []string{"X-Remove-Me", "X-Also-Remove"},
	})

	var capturedReq *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Keep-Me", "value")
	req.Header.Set("X-Remove-Me", "value")
	req.Header.Set("X-Also-Remove", "value")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if capturedReq == nil {
		t.Fatal("request was not captured")
	}

	if capturedReq.Header.Get("X-Keep-Me") != "value" {
		t.Error("X-Keep-Me should be preserved")
	}

	if capturedReq.Header.Get("X-Remove-Me") != "" {
		t.Error("X-Remove-Me should be removed")
	}

	if capturedReq.Header.Get("X-Also-Remove") != "" {
		t.Error("X-Also-Remove should be removed")
	}
}

func TestHeadersMiddleware_RequestHeaderOrder(t *testing.T) {
	// Test that headers are modified in order: Remove -> Set -> Add
	m := NewHeadersMiddleware(HeadersConfig{
		RequestRemove: []string{"X-Test"},
		RequestSet: map[string]string{
			"X-Test": "set-value",
		},
		RequestAdd: map[string]string{
			"X-Test": "add-value",
		},
	})

	var capturedReq *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Test", "original")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if capturedReq == nil {
		t.Fatal("request was not captured")
	}

	// After Remove (removes original), Set (adds "set-value"), Add (adds "add-value")
	values := capturedReq.Header["X-Test"]
	if len(values) != 2 || values[0] != "set-value" || values[1] != "add-value" {
		t.Errorf("expected X-Test to be ['set-value', 'add-value'], got %v", values)
	}
}

func TestHeadersMiddleware_ResponseAdd(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseAdd: map[string]string{
			"X-Response-Add": "added-value",
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Response-Add"); got != "added-value" {
		t.Errorf("expected X-Response-Add 'added-value', got '%s'", got)
	}
}

func TestHeadersMiddleware_ResponseSet(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseSet: map[string]string{
			"Content-Type": "application/custom",
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Type"); got != "application/custom" {
		t.Errorf("expected Content-Type 'application/custom', got '%s'", got)
	}
}

func TestHeadersMiddleware_ResponseRemove(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseRemove: []string{"X-Remove-Response"},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Keep-Response", "value")
		w.Header().Set("X-Remove-Response", "value")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if rr.Header().Get("X-Keep-Response") != "value" {
		t.Error("X-Keep-Response should be preserved")
	}

	if rr.Header().Get("X-Remove-Response") != "" {
		t.Error("X-Remove-Response should be removed")
	}
}

func TestHeadersMiddleware_ResponseHeaderOrder(t *testing.T) {
	// Test that response headers are modified in order: Remove -> Set -> Add
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseRemove: []string{"X-Test"},
		ResponseSet: map[string]string{
			"X-Test": "set-value",
		},
		ResponseAdd: map[string]string{
			"X-Test": "add-value",
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "original")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	// After Remove (removes original), Set (adds "set-value"), Add (adds "add-value")
	values := rr.Header()["X-Test"]
	if len(values) != 2 || values[0] != "set-value" || values[1] != "add-value" {
		t.Errorf("expected X-Test to be ['set-value', 'add-value'], got %v", values)
	}
}

func TestHeadersMiddleware_SecurityPresetBasic(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		SecurityPreset: SecurityPresetBasic,
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options 'nosniff', got '%s'", got)
	}

	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options 'DENY', got '%s'", got)
	}

	if got := rr.Header().Get("X-XSS-Protection"); got != "1; mode=block" {
		t.Errorf("expected X-XSS-Protection '1; mode=block', got '%s'", got)
	}

	// Should NOT have strict headers
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Error("Strict-Transport-Security should not be set in basic preset")
	}
}

func TestHeadersMiddleware_SecurityPresetStrict(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		SecurityPreset: SecurityPresetStrict,
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	// Basic headers
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options 'nosniff', got '%s'", got)
	}

	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options 'DENY', got '%s'", got)
	}

	if got := rr.Header().Get("X-XSS-Protection"); got != "1; mode=block" {
		t.Errorf("expected X-XSS-Protection '1; mode=block', got '%s'", got)
	}

	// Strict headers
	if got := rr.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("expected Strict-Transport-Security 'max-age=31536000; includeSubDomains', got '%s'", got)
	}

	if got := rr.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Errorf("expected Content-Security-Policy 'default-src 'self'', got '%s'", got)
	}

	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("expected Referrer-Policy 'strict-origin-when-cross-origin', got '%s'", got)
	}
}

func TestHeadersMiddleware_SecurityPresetNone(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		SecurityPreset: SecurityPresetNone,
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	// Should not have any security headers
	if rr.Header().Get("X-Content-Type-Options") != "" {
		t.Error("X-Content-Type-Options should not be set")
	}

	if rr.Header().Get("X-Frame-Options") != "" {
		t.Error("X-Frame-Options should not be set")
	}
}

func TestHeadersMiddleware_SecurityPresetOverride(t *testing.T) {
	// Security preset can be overridden by ResponseSet
	m := NewHeadersMiddleware(HeadersConfig{
		SecurityPreset: SecurityPresetBasic,
		ResponseSet: map[string]string{
			"X-Frame-Options": "SAMEORIGIN",
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	// Preset sets DENY, but ResponseSet overrides to SAMEORIGIN
	if got := rr.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("expected X-Frame-Options 'SAMEORIGIN' (overridden), got '%s'", got)
	}

	// Other preset headers should still be present
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options 'nosniff', got '%s'", got)
	}
}

func TestHeadersMiddleware_ResponseWriterImplementsInterfaces(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseAdd: map[string]string{
			"X-Added": "value",
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Test that we can write
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("response body"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	if body := rr.Body.String(); body != "response body" {
		t.Errorf("expected body 'response body', got '%s'", body)
	}

	if got := rr.Header().Get("X-Added"); got != "value" {
		t.Errorf("expected X-Added 'value', got '%s'", got)
	}
}

func TestHeadersMiddleware_WriteImplicitHeader(t *testing.T) {
	// When Write is called without WriteHeader, it should implicitly call WriteHeader(200)
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseAdd: map[string]string{
			"X-Added": "value",
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("response"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if got := rr.Header().Get("X-Added"); got != "value" {
		t.Errorf("expected X-Added 'value', got '%s'", got)
	}
}

func TestHeadersMiddleware_MultipleWrites(t *testing.T) {
	// WriteHeader should only be effective once
	m := NewHeadersMiddleware(HeadersConfig{})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusOK) // Should be ignored
		w.Write([]byte("response"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}
}

func TestHeadersMiddleware_CaseInsensitiveHeaderMatching(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		RequestRemove: []string{"x-custom-header"},
		RequestSet: map[string]string{
			"content-type": "application/json",
		},
	})

	var capturedReq *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-CUSTOM-HEADER", "value")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if capturedReq == nil {
		t.Fatal("request was not captured")
	}

	// Header removal should be case-insensitive
	if capturedReq.Header.Get("X-Custom-Header") != "" {
		t.Error("X-Custom-Header should be removed (case-insensitive)")
	}

	// Header setting uses canonical form
	if capturedReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", capturedReq.Header.Get("Content-Type"))
	}
}

func TestHeaderResponseWriter_Unwrap(t *testing.T) {
	m := NewHeadersMiddleware(HeadersConfig{
		ResponseAdd: map[string]string{
			"X-Added": "value",
		},
	})

	var wrappedWriter http.ResponseWriter
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrappedWriter = w
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	inner := httptest.NewRecorder()

	m.Wrap(next).ServeHTTP(inner, req)

	// The handler receives a headerResponseWriter
	hw, ok := wrappedWriter.(*headerResponseWriter)
	if !ok {
		t.Fatal("expected wrappedWriter to be *headerResponseWriter")
	}

	// Unwrap should return the original inner ResponseWriter
	unwrapped := hw.Unwrap()
	if unwrapped == nil {
		t.Fatal("Unwrap() returned nil")
	}
	if unwrapped != inner {
		t.Error("Unwrap() did not return the original ResponseWriter")
	}
}

// --- Tests for helper functions ---

func TestHasHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "text/html")
	headers.Set("X-Custom", "value")

	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{"existing header", "Content-Type", true},
		{"existing header lowercase", "content-type", true},
		{"existing custom header", "X-Custom", true},
		{"non-existent header", "X-Missing", false},
		{"empty header name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasHeader(headers, tt.header); got != tt.expected {
				t.Errorf("hasHeader(%q) = %v, want %v", tt.header, got, tt.expected)
			}
		})
	}
}

func TestContainsHeader(t *testing.T) {
	slice := []string{"Content-Type", "X-Custom", "Authorization"}

	tests := []struct {
		name     string
		target   string
		expected bool
	}{
		{"exact match", "Content-Type", true},
		{"case insensitive match", "content-type", true},
		{"another match", "authorization", true},
		{"not found", "X-Missing", false},
		{"empty target", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsHeader(slice, tt.target); got != tt.expected {
				t.Errorf("containsHeader(%q) = %v, want %v", tt.target, got, tt.expected)
			}
		})
	}

	// Test with empty slice
	if containsHeader([]string{}, "anything") {
		t.Error("containsHeader with empty slice should return false")
	}
}

func TestCanonicalizeHeaders(t *testing.T) {
	input := map[string]string{
		"content-type":  "text/html",
		"x-custom":      "value",
		"AUTHORIZATION": "Bearer token",
	}

	result := canonicalizeHeaders(input)

	if len(result) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(result))
	}

	if result["Content-Type"] != "text/html" {
		t.Errorf("expected Content-Type header, got keys: %v", result)
	}
	if result["X-Custom"] != "value" {
		t.Errorf("expected X-Custom header, got keys: %v", result)
	}
	if result["Authorization"] != "Bearer token" {
		t.Errorf("expected Authorization header, got keys: %v", result)
	}

	// Test with empty map
	empty := canonicalizeHeaders(map[string]string{})
	if len(empty) != 0 {
		t.Errorf("expected 0 headers for empty input, got %d", len(empty))
	}
}

func TestCanonicalizeHeaderSlice(t *testing.T) {
	input := []string{"content-type", "x-custom", "AUTHORIZATION"}
	result := canonicalizeHeaderSlice(input)

	if len(result) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(result))
	}
	if result[0] != "Content-Type" {
		t.Errorf("expected Content-Type, got %q", result[0])
	}
	if result[1] != "X-Custom" {
		t.Errorf("expected X-Custom, got %q", result[1])
	}
	if result[2] != "Authorization" {
		t.Errorf("expected Authorization, got %q", result[2])
	}
}
