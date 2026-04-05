package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripPrefix_Simple(t *testing.T) {
	mw := StripPrefix("/api/v1")

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/users" {
		t.Errorf("Expected path '/users', got '%s'", capturedPath)
	}
}

func TestStripPrefix_NoPrefix(t *testing.T) {
	mw := StripPrefix("/api/v1")

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// Path doesn't start with prefix
	req := httptest.NewRequest("GET", "/other/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/other/path" {
		t.Errorf("Expected path '/other/path', got '%s'", capturedPath)
	}
}

func TestStripPrefix_ExactMatch(t *testing.T) {
	mw := StripPrefix("/api/v1")

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// Exact match becomes /
	req := httptest.NewRequest("GET", "/api/v1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/" {
		t.Errorf("Expected path '/', got '%s'", capturedPath)
	}
}

func TestStripPrefix_WithRedirect(t *testing.T) {
	mw := NewStripPrefixMiddleware(StripPrefixConfig{
		Prefix:        "/api",
		RedirectSlash: true,
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for redirect")
	}))

	// Exact match with redirect
	req := httptest.NewRequest("GET", "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status 301, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	if loc != "/api/" {
		t.Errorf("Expected Location '/api/', got '%s'", loc)
	}
}

func TestStripPrefix_WithoutRedirect(t *testing.T) {
	mw := NewStripPrefixMiddleware(StripPrefixConfig{
		Prefix:        "/api",
		RedirectSlash: false,
	})

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// Exact match without redirect
	req := httptest.NewRequest("GET", "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/" {
		t.Errorf("Expected path '/', got '%s'", capturedPath)
	}
}

func TestStripPrefix_EmptyPrefix(t *testing.T) {
	mw := StripPrefix("")

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/some/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/some/path" {
		t.Errorf("Expected path '/some/path', got '%s'", capturedPath)
	}
}

func TestStripPrefix_PreservesQueryString(t *testing.T) {
	mw := StripPrefix("/api")

	var capturedQuery string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/users?limit=10&offset=20", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedQuery != "limit=10&offset=20" {
		t.Errorf("Expected query 'limit=10&offset=20', got '%s'", capturedQuery)
	}
}

func TestStripPrefix_PreservesMethod(t *testing.T) {
	mw := StripPrefix("/api")

	var capturedMethod string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedMethod != "POST" {
		t.Errorf("Expected method 'POST', got '%s'", capturedMethod)
	}
}

func TestStripPrefix_PreservesHeaders(t *testing.T) {
	mw := StripPrefix("/api")

	var capturedHeader string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedHeader != "custom-value" {
		t.Errorf("Expected header 'custom-value', got '%s'", capturedHeader)
	}
}

func TestStripPrefix_TrailingSlash(t *testing.T) {
	// Prefix with trailing slash should be normalized
	mw := NewStripPrefixMiddleware(StripPrefixConfig{
		Prefix: "/api/",
	})

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/users" {
		t.Errorf("Expected path '/users', got '%s'", capturedPath)
	}
}

func TestStripPrefix_NestedPrefix(t *testing.T) {
	mw := StripPrefix("/api/v1/service-a")

	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/service-a/resource", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedPath != "/resource" {
		t.Errorf("Expected path '/resource', got '%s'", capturedPath)
	}
}

func TestStripPrefixMiddleware_Name(t *testing.T) {
	mw := StripPrefix("/api")

	if mw.Name() != "strip_prefix" {
		t.Errorf("Expected name 'strip_prefix', got '%s'", mw.Name())
	}
}

func TestStripPrefixMiddleware_Priority(t *testing.T) {
	mw := StripPrefix("/api")

	if mw.Priority() != 150 {
		t.Errorf("Expected priority 150, got %d", mw.Priority())
	}
}

func TestStripPrefix_RequestURI(t *testing.T) {
	mw := StripPrefix("/api")

	var capturedRequestURI string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/users?page=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedRequestURI != "/users" {
		t.Errorf("Expected RequestURI '/users', got '%s'", capturedRequestURI)
	}
}

func TestStripPrefix_PartialMatch(t *testing.T) {
	mw := StripPrefix("/api")

	// Path starts with "/api" but continues immediately with more characters
	var capturedPath string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// "/apiv2" starts with "/api", so it matches and strips to "/v2"
	req := httptest.NewRequest("GET", "/apiv2/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The prefix "/api" is stripped from "/apiv2/users" -> "v2/users"
	if capturedPath != "v2/users" {
		t.Errorf("Expected path 'v2/users', got '%s'", capturedPath)
	}
}
