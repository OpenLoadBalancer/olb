package forcessl

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForceSSL_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called when disabled")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestForceSSL_AlreadyHTTPS(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "https://example.com/test", nil)
	req.TLS = &tls.ConnectionState{} // Simulate TLS
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for HTTPS requests")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestForceSSL_XForwardedProto(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when X-Forwarded-Proto is https")
	}
}

func TestForceSSL_PermanentRedirect(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Permanent = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for HTTP requests")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status %d, got %d", http.StatusMovedPermanently, rec.Code)
	}

	location := rec.Header().Get("Location")
	expected := "https://example.com/test"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestForceSSL_TemporaryRedirect(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Permanent = false

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for HTTP requests")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("Expected status %d, got %d", http.StatusTemporaryRedirect, rec.Code)
	}
}

func TestForceSSL_WithQueryString(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com/search?q=test&page=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://example.com/search?q=test&page=1"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestForceSSL_CustomPort(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Port = 8443

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://example.com:8443/test"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestForceSSL_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/.well-known"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/health/live", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for excluded paths")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestForceSSL_ExcludeHost(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludeHosts = []string{"localhost", "127.0.0.1"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://localhost/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for excluded hosts")
	}

	// Test with port
	called = false
	req2 := httptest.NewRequest("GET", "http://127.0.0.1:8080/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if !called {
		t.Error("Handler should be called for excluded hosts with port")
	}
}

func TestForceSSL_CustomHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.HeaderKey = "X-Scheme"
	config.HeaderValue = "secure"

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Scheme", "secure")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when custom header matches")
	}
}

func TestForceSSL_XSchemeHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Scheme", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when X-Scheme is https")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Permanent != true {
		t.Error("Default Permanent should be true")
	}
	if config.Port != 443 {
		t.Errorf("Default Port should be 443, got %d", config.Port)
	}
	if config.HeaderKey != "X-Forwarded-Proto" {
		t.Errorf("Default HeaderKey should be X-Forwarded-Proto, got %s", config.HeaderKey)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 70 {
		t.Errorf("Expected priority 70, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "forcessl" {
		t.Errorf("Expected name 'forcessl', got '%s'", mw.Name())
	}
}

func TestNew_DefaultPort(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	// Port is already 443 in DefaultConfig

	mw := New(config)

	// Test that it works with default port
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://example.com/test" // No port since 443 is default
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestNew_DefaultHeaderKey(t *testing.T) {
	config := Config{
		Enabled:   true,
		HeaderKey: "", // Should default to X-Forwarded-Proto
	}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with default header key")
	}
}

func TestIsSecureRequest(t *testing.T) {
	// Test TLS
	req1 := httptest.NewRequest("GET", "https://example.com/test", nil)
	req1.TLS = &tls.ConnectionState{}
	if !IsSecureRequest(req1) {
		t.Error("IsSecureRequest should return true for TLS")
	}

	// Test X-Forwarded-Proto
	req2 := httptest.NewRequest("GET", "http://example.com/test", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	if !IsSecureRequest(req2) {
		t.Error("IsSecureRequest should return true for X-Forwarded-Proto: https")
	}

	// Test X-Scheme
	req3 := httptest.NewRequest("GET", "http://example.com/test", nil)
	req3.Header.Set("X-Scheme", "https")
	if !IsSecureRequest(req3) {
		t.Error("IsSecureRequest should return true for X-Scheme: https")
	}

	// Test insecure
	req4 := httptest.NewRequest("GET", "http://example.com/test", nil)
	if IsSecureRequest(req4) {
		t.Error("IsSecureRequest should return false for plain HTTP")
	}
}

func TestBuildHTTPSURL_WithPortInHost(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com:8080/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	// Port should be stripped and replaced with default HTTPS port (443)
	expected := "https://example.com/test"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestForceSSL_PathPreservation(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/", "https://example.com/"},
		{"http://example.com/api/v1/users", "https://example.com/api/v1/users"},
		{"http://example.com/path/with/many/levels", "https://example.com/path/with/many/levels"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest("GET", tc.input, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		location := rec.Header().Get("Location")
		if location != tc.expected {
			t.Errorf("For input %s, expected %s, got %s", tc.input, tc.expected, location)
		}
	}
}
