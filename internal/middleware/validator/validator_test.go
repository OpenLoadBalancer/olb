package validator

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidator_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when validator is disabled")
	}
}

func TestValidator_RequiredHeader_Missing(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RequiredHeaders = map[string]string{
		"X-Request-ID": "^[a-zA-Z0-9-]+$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with missing header")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_RequiredHeader_Invalid(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RequiredHeaders = map[string]string{
		"X-API-Version": "^v[0-9]+$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid header")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Version", "invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_RequiredHeader_Valid(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RequiredHeaders = map[string]string{
		"X-API-Version": "^v[0-9]+$",
	}

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
	req.Header.Set("X-API-Version", "v1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid header")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestValidator_ForbiddenHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ForbiddenHeaders = []string{"X-Internal-Token"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with forbidden header")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_QueryParam_Missing(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.QueryRules = map[string]string{
		"page": "^[0-9]+$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with missing query param")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_QueryParam_Invalid(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.QueryRules = map[string]string{
		"page": "^[0-9]+$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid query param")
	}))

	req := httptest.NewRequest("GET", "/test?page=abc", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_QueryParam_Valid(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.QueryRules = map[string]string{
		"page": "^[0-9]+$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test?page=123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid query param")
	}
}

func TestValidator_InvalidJSON(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ContentTypes = []string{"application/json"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid JSON")
	}))

	req := httptest.NewRequest("POST", "/test", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_ValidJSON(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ContentTypes = []string{"application/json"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid JSON")
	}
}

func TestValidator_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/public"}
	config.RequiredHeaders = map[string]string{
		"X-Required": ".+",
	}

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

func TestValidator_LogOnly(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.LogOnly = true
	config.RejectOnFailure = false
	config.RequiredHeaders = map[string]string{
		"X-Required": ".+",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var errors []ValidationError
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errors = GetValidationErrors(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 in log-only mode, got %d", rec.Code)
	}

	if len(errors) == 0 {
		t.Error("Expected validation errors in context")
	}
}

func TestValidator_PathPattern(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.PathPatterns = map[string]string{
		"/api/": "^/api/v[0-9]+/.*$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid path pattern")
	}))

	req := httptest.NewRequest("GET", "/api/invalid/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestValidator_PathPattern_Valid(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.PathPatterns = map[string]string{
		"/api/": "^/api/v[0-9]+/.*$",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called with valid path pattern")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.ValidateRequest != true {
		t.Error("Default ValidateRequest should be true")
	}
	if config.MaxBodySize != 1024*1024 {
		t.Errorf("Default MaxBodySize should be 1MB, got %d", config.MaxBodySize)
	}
	if config.RejectOnFailure != true {
		t.Error("Default RejectOnFailure should be true")
	}
	if config.LogOnly != false {
		t.Error("Default LogOnly should be false")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 145 {
		t.Errorf("Expected priority 145, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "validator" {
		t.Errorf("Expected name 'validator', got '%s'", mw.Name())
	}
}

func TestContextHelpers(t *testing.T) {
	errors := []ValidationError{
		{Field: "header", Message: "missing", Type: "header"},
		{Field: "body", Message: "invalid", Type: "body"},
	}

	ctx := WithValidationErrors(t.Context(), errors)
	retrieved := GetValidationErrors(ctx)

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 errors, got %d", len(retrieved))
	}

	if retrieved[0].Field != "header" {
		t.Errorf("Expected first error field 'header', got '%s'", retrieved[0].Field)
	}

	if !HasValidationErrors(ctx) {
		t.Error("HasValidationErrors should return true")
	}

	// Test no errors
	ctx2 := t.Context()
	if HasValidationErrors(ctx2) {
		t.Error("HasValidationErrors should return false when no errors")
	}
}

func TestValidator_LargeBody(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.MaxBodySize = 100 // 100 bytes max
	config.ContentTypes = []string{"application/json"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// Body larger than MaxBodySize
	largeBody := bytes.Repeat([]byte("a"), 200)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Large bodies should be skipped (not validated)
	if !called {
		t.Error("Handler should be called for large bodies (skipped validation)")
	}
}

func TestNew_InvalidRegex(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RequiredHeaders = map[string]string{
		"X-Test": "[invalid(",
	}

	_, err := New(config)
	if err == nil {
		t.Error("New should return error for invalid regex")
	}
}

func TestValidationError_Response(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RequiredHeaders = map[string]string{
		"X-Required": ".+",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("Response should have Content-Type: application/json")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "validation_failed") {
		t.Error("Response should contain 'validation_failed' error")
	}
	if !strings.Contains(body, "violations") {
		t.Error("Response should contain 'violations' field")
	}
}
