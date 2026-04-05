package csrf

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRF_Disabled(t *testing.T) {
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

	req := httptest.NewRequest("POST", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called when disabled")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestCSRF_SafeMethod(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// GET request should not require CSRF
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for safe methods")
	}

	// Check that cookie was set
	cookies := rec.Result().Cookies()
	var hasCSRFCookie bool
	for _, c := range cookies {
		if c.Name == "csrf_token" && c.Value != "" {
			hasCSRFCookie = true
			break
		}
	}
	if !hasCSRFCookie {
		t.Error("CSRF cookie should be set for safe methods")
	}
}

func TestCSRF_ValidToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// First, get a valid token via GET request
	getReq := httptest.NewRequest("GET", "/test", nil)
	getRec := httptest.NewRecorder()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(getRec, getReq)

	// Extract the CSRF cookie
	var token string
	for _, c := range getRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			token = c.Value
			break
		}
	}

	if token == "" {
		t.Fatal("Failed to get CSRF token")
	}

	// Now make POST request with valid token
	postReq := httptest.NewRequest("POST", "/test", nil)
	postReq.Header.Set("X-CSRF-Token", token)
	postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})

	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, postRec.Code)
	}
}

func TestCSRF_MissingCookie(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when CSRF cookie is missing")
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-CSRF-Token", "some-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "CSRF token missing from cookie") {
		t.Error("Error message should indicate missing cookie")
	}
}

func TestCSRF_MissingToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when CSRF token is missing")
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "some-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "CSRF token missing from request") {
		t.Error("Error message should indicate missing token")
	}
}

func TestCSRF_InvalidToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when CSRF token is invalid")
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "valid-token"})
	req.Header.Set("X-CSRF-Token", "invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "CSRF token mismatch") {
		t.Error("Error message should indicate token mismatch")
	}
}

func TestCSRF_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/api/public", "/webhooks"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/public/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for excluded paths")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestCSRF_CustomHeaderName(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.HeaderName = "X-XSRF-Token"

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Get token first
	getReq := httptest.NewRequest("GET", "/test", nil)
	getRec := httptest.NewRecorder()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(getRec, getReq)

	var token string
	for _, c := range getRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			token = c.Value
			break
		}
	}

	// Use custom header
	postReq := httptest.NewRequest("POST", "/test", nil)
	postReq.Header.Set("X-XSRF-Token", token)
	postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})

	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, postRec.Code)
	}
}

func TestCSRF_FormToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Get token first
	getReq := httptest.NewRequest("GET", "/test", nil)
	getRec := httptest.NewRecorder()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(getRec, getReq)

	var token string
	for _, c := range getRec.Result().Cookies() {
		if c.Name == "csrf_token" {
			token = c.Value
			break
		}
	}

	// Use form field
	formData := "csrf_token=" + token
	postReq := httptest.NewRequest("POST", "/test", strings.NewReader(formData))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})

	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, postRec.Code)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.CookieName != "csrf_token" {
		t.Errorf("Default CookieName should be csrf_token, got %s", config.CookieName)
	}
	if config.HeaderName != "X-CSRF-Token" {
		t.Errorf("Default HeaderName should be X-CSRF-Token, got %s", config.HeaderName)
	}
	if config.TokenLength != 32 {
		t.Errorf("Default TokenLength should be 32, got %d", config.TokenLength)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 200 {
		t.Errorf("Expected priority 200, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "csrf" {
		t.Errorf("Expected name 'csrf', got '%s'", mw.Name())
	}
}

func TestNew_DefaultValues(t *testing.T) {
	config := Config{
		Enabled: true,
		// Leave other fields empty
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Verify defaults were applied
	if mw.config.CookieName != "csrf_token" {
		t.Errorf("Expected default CookieName csrf_token, got %s", mw.config.CookieName)
	}
	if mw.config.HeaderName != "X-CSRF-Token" {
		t.Errorf("Expected default HeaderName X-CSRF-Token, got %s", mw.config.HeaderName)
	}
	if mw.config.TokenLength != 32 {
		t.Errorf("Expected default TokenLength 32, got %d", mw.config.TokenLength)
	}
}

func TestRequiresValidation(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, _ := New(config)

	// Safe methods should not require validation
	safeMethods := []string{"GET", "HEAD", "OPTIONS", "TRACE"}
	for _, method := range safeMethods {
		if mw.requiresValidation(method) {
			t.Errorf("Method %s should not require validation", method)
		}
	}

	// Unsafe methods should require validation
	unsafeMethods := []string{"POST", "PUT", "PATCH", "DELETE"}
	for _, method := range unsafeMethods {
		if !mw.requiresValidation(method) {
			t.Errorf("Method %s should require validation", method)
		}
	}
}

func TestRequiresValidation_Custom(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludeMethods = []string{"GET", "POST"} // Custom: POST is safe

	mw, _ := New(config)

	if mw.requiresValidation("POST") {
		t.Error("POST should not require validation with custom config")
	}

	if !mw.requiresValidation("PUT") {
		t.Error("PUT should require validation with custom config")
	}
}

func TestCompareTokens(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	// Same tokens should match
	if !mw.compareTokens("abc", "abc") {
		t.Error("Same tokens should match")
	}

	// Different tokens should not match
	if mw.compareTokens("abc", "def") {
		t.Error("Different tokens should not match")
	}
}

func TestGenerateToken(t *testing.T) {
	token1, err := generateToken(32)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token1 == "" {
		t.Error("Token should not be empty")
	}

	// Tokens should be different each time
	token2, err := generateToken(32)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token1 == token2 {
		t.Error("Tokens should be unique")
	}
}

func TestGetToken(t *testing.T) {
	// Request with cookie
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test-token"})

	token := GetToken(req)
	if token != "test-token" {
		t.Errorf("Expected token test-token, got %s", token)
	}

	// Request without cookie
	req2 := httptest.NewRequest("GET", "/test", nil)
	token2 := GetToken(req2)
	if token2 != "" {
		t.Errorf("Expected empty token, got %s", token2)
	}
}

func TestTokenNotRegenerated(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, _ := New(config)

	// First request sets token
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec1, req1)

	var token1 string
	for _, c := range rec1.Result().Cookies() {
		if c.Name == "csrf_token" {
			token1 = c.Value
			break
		}
	}

	// Second request with cookie should not regenerate
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.AddCookie(&http.Cookie{Name: "csrf_token", Value: token1})
	rec2 := httptest.NewRecorder()

	handler.ServeHTTP(rec2, req2)

	// Check no new cookie was set
	cookies2 := rec2.Result().Cookies()
	for _, c := range cookies2 {
		if c.Name == "csrf_token" {
			t.Error("Token should not be regenerated when cookie exists")
		}
	}
}
