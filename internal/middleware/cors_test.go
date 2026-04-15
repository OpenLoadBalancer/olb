package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCORSMiddleware_Name(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name() != "cors" {
		t.Errorf("expected name 'cors', got '%s'", m.Name())
	}
}

func TestCORSMiddleware_Priority(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Priority() != PriorityCORS {
		t.Errorf("expected priority %d, got %d", PriorityCORS, m.Priority())
	}
}

func TestCORSMiddleware_PreflightRequest(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		ExposedHeaders:   []string{"X-Custom-Header"},
		AllowCredentials: true,
		MaxAge:           86400 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for preflight")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	// Check CORS headers
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin 'https://example.com', got '%s'", got)
	}

	if got := rr.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Errorf("expected Access-Control-Allow-Methods 'GET, POST, OPTIONS', got '%s'", got)
	}

	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials 'true', got '%s'", got)
	}

	if got := rr.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("expected Access-Control-Max-Age '86400', got '%s'", got)
	}

	// Check Vary header
	if got := rr.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("expected Vary header to contain 'Origin', got '%s'", got)
	}
}

func TestCORSMiddleware_ActualRequest(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST"},
		ExposedHeaders:   []string{"X-Custom-Header"},
		AllowCredentials: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("next handler should be called for actual request")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Check CORS headers
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin 'https://example.com', got '%s'", got)
	}

	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials 'true', got '%s'", got)
	}

	if got := rr.Header().Get("Access-Control-Expose-Headers"); got != "X-Custom-Header" {
		t.Errorf("expected Access-Control-Expose-Headers 'X-Custom-Header', got '%s'", got)
	}

	// Check Vary header
	if got := rr.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("expected Vary header to contain 'Origin', got '%s'", got)
	}
}

func TestCORSMiddleware_WildcardOrigin(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST"},
	})
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://any-origin.com")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected Access-Control-Allow-Origin '*', got '%s'", got)
	}
}

func TestCORSMiddleware_WildcardOriginWithCredentials(t *testing.T) {
	// When AllowCredentials is true, wildcard origin should be rejected at construction
	_, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowCredentials: true,
	})
	if err == nil {
		t.Fatal("expected error when creating CORS middleware with wildcard origin and credentials enabled")
	}
	if !strings.Contains(err.Error(), "AllowedOrigins cannot contain '*'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCORSMiddleware_SpecificOriginMatching(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"https://example.com", "https://app.example.com"},
		AllowedMethods: []string{"GET"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		origin  string
		allowed bool
	}{
		{"https://example.com", true},
		{"https://app.example.com", true},
		{"https://evil.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rr := httptest.NewRecorder()
			m.Wrap(next).ServeHTTP(rr, req)

			gotOrigin := rr.Header().Get("Access-Control-Allow-Origin")
			if tt.allowed {
				if gotOrigin != tt.origin {
					t.Errorf("expected Access-Control-Allow-Origin '%s', got '%s'", tt.origin, gotOrigin)
				}
			} else {
				if gotOrigin != "" {
					t.Errorf("expected no Access-Control-Allow-Origin for disallowed origin, got '%s'", gotOrigin)
				}
			}
		})
	}
}

func TestCORSMiddleware_CredentialsWithoutWildcard(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowCredentials: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin 'https://example.com', got '%s'", got)
	}

	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials 'true', got '%s'", got)
	}
}

func TestCORSMiddleware_PreflightWithRequestedHeaders(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-Custom"},
	})
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for preflight")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	// Should allow the requested headers
	allowedHeaders := rr.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowedHeaders, "Content-Type") {
		t.Errorf("expected Access-Control-Allow-Headers to contain 'Content-Type', got '%s'", allowedHeaders)
	}
	if !strings.Contains(allowedHeaders, "Authorization") {
		t.Errorf("expected Access-Control-Allow-Headers to contain 'Authorization', got '%s'", allowedHeaders)
	}
}

func TestCORSMiddleware_PreflightDisallowedOrigin(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
	})
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for preflight")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://evil.com")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	// Should not set CORS headers for disallowed origin
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin for disallowed origin, got '%s'", got)
	}
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"*"},
	})
	if err != nil {
		t.Fatal(err)
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Request without Origin header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("next handler should be called")
	}

	// Vary header should still be set
	if got := rr.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("expected Vary header to contain 'Origin', got '%s'", got)
	}
}

func TestCORSMiddleware_CaseInsensitiveOrigin(t *testing.T) {
	m, err := NewCORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"https://EXAMPLE.COM"},
		AllowedMethods: []string{"GET"},
	})
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rr := httptest.NewRecorder()
	m.Wrap(next).ServeHTTP(rr, req)

	// Should match case-insensitively
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin 'https://example.com', got '%s'", got)
	}
}
