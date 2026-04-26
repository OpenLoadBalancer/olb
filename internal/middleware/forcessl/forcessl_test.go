package forcessl

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestIsFromTrustedProxy_TrustedIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TrustedProxies = []string{"10.0.0.0/8", "172.16.0.0/12"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// Request from a trusted proxy with X-Forwarded-Proto: https should be treated as HTTPS
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "10.1.2.3:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for trusted proxy with forwarded proto https")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestIsFromTrustedProxy_UntrustedIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	// Request from an untrusted IP with X-Forwarded-Proto: https should NOT be trusted
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for untrusted proxy with forwarded headers")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect from untrusted proxy, got status %d", rec.Code)
	}
}

func TestIsFromTrustedProxy_UntrustedIPWithXScheme(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for untrusted proxy")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Scheme", "https")
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect from untrusted proxy, got status %d", rec.Code)
	}
}

func TestIsFromTrustedProxy_NoTrustedProxiesConfigured(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	// TrustedProxies is nil/empty — all forwarded headers are trusted (default behavior)

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when TrustedProxies is empty (headers trusted from any IP)")
	}
}

func TestIsFromTrustedProxy_InvalidCIDR(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TrustedProxies = []string{"not-a-valid-cidr", "10.0.0.0/8"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// Should match the second valid CIDR
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "10.0.0.5:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called; invalid CIDR entries should be skipped")
	}
}

func TestIsFromTrustedProxy_InvalidRemoteIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when remote IP is unparseable")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "not-an-ip"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect for invalid remote IP, got status %d", rec.Code)
	}
}

func TestIsFromTrustedProxy_IPv6Trusted(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.TrustedProxies = []string{"::1/128", "fd00::/8"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "[::1]:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for trusted IPv6 loopback")
	}
}

func TestBuildHTTPSURL_EmptyHost(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	// When both r.Host and r.URL.Host are empty, buildHTTPSURL returns ""
	// and http.Redirect writes a redirect to "/" as the location.
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = ""
	req.URL.Host = ""
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// buildHTTPSURL returns "" which http.Redirect turns into a redirect to ""
	// which the Go stdlib resolves to the current path.
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect status, got %d", rec.Code)
	}
}

func TestBuildHTTPSURL_HostWithSlash(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	// Set a Host containing "/" to trigger the fallback to r.URL.Host
	req.Host = "evil/host.com"
	req.URL = &url.URL{Path: "/test", Host: "safe.example.com"}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://safe.example.com/test"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestBuildHTTPSURL_HostWithBackslash(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	// Set a Host containing "\" to trigger the fallback to r.URL.Host
	req.Host = "evil\\host.com"
	req.URL = &url.URL{Path: "/test", Host: "safe.example.com"}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://safe.example.com/test"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}

func TestBuildHTTPSURL_EmptyHostEmptyURLHost(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	// Host contains "/" (triggers fallback), URL.Host is empty => buildHTTPSURL returns ""
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "bad/host"
	req.URL = &url.URL{Path: "/test", Host: ""}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// buildHTTPSURL returns "" — http.Redirect still writes a redirect
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect status, got %d", rec.Code)
	}
}

func TestIsFromTrustedProxy_CustomHeaderFromUntrusted(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.HeaderKey = "X-Custom-Proto"
	config.HeaderValue = "https"
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for untrusted proxy with custom header")
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Custom-Proto", "https")
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect, got status %d", rec.Code)
	}
}

func TestIsFromTrustedProxy_CustomHeaderFromTrusted(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.HeaderKey = "X-Custom-Proto"
	config.HeaderValue = "https"
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Custom-Proto", "https")
	req.RemoteAddr = "10.0.0.5:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for trusted proxy with custom header")
	}
}

func TestBuildHTTPSURL_CustomPortWithQueryString(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Port = 8443

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "http://example.com/path?key=val", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://example.com:8443/path?key=val"
	if location != expected {
		t.Errorf("Expected Location %s, got %s", expected, location)
	}
}
