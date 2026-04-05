package basic

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuth_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when Basic Auth is disabled")
	}
}

func TestBasicAuth_MissingCredentials(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Users = map[string]string{
		"admin": "password123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without credentials")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("Expected WWW-Authenticate header")
	}
}

func TestBasicAuth_InvalidCredentials(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Users = map[string]string{
		"admin": "password123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid credentials")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic YWRtaW46d3JvbmdwYXNz") // admin:wrongpass
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestBasicAuth_ValidCredentials(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Users = map[string]string{
		"admin": "password123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedUsername string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUsername = GetUsername(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic YWRtaW46cGFzc3dvcmQxMjM=") // admin:password123
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedUsername != "admin" {
		t.Errorf("Expected username 'admin', got %s", receivedUsername)
	}
}

func TestBasicAuth_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/public"}
	config.Users = map[string]string{
		"admin": "password123",
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

	// Test excluded path
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

func TestBasicAuth_SHA256(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "sha256"
	config.Users = map[string]string{
		"admin": "password123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedUsername string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUsername = GetUsername(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic YWRtaW46cGFzc3dvcmQxMjM=") // admin:password123
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedUsername != "admin" {
		t.Errorf("Expected username 'admin', got %s", receivedUsername)
	}
}

func TestBasicAuth_InvalidAuthHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Users = map[string]string{
		"admin": "password123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		header string
	}{
		{"missing prefix", "YWRtaW46cGFzc3dvcmQxMjM="},
		{"wrong scheme", "Bearer token123"},
		{"invalid base64", "Basic !!!invalid!!!"},
		{"no colon", "Basic YWRtaW4="}, // admin (no password)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Handler should not be called")
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401, got %d", rec.Code)
			}
		})
	}
}

func TestBasicAuth_UnknownUser(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Users = map[string]string{
		"admin": "password123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	// unknownuser:password
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic dW5rbm93bnVzZXI6cGFzc3dvcmQ=")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Realm != "Restricted" {
		t.Errorf("Default Realm should be 'Restricted', got %s", config.Realm)
	}
	if config.Hash != "sha256" {
		t.Errorf("Default Hash should be 'sha256', got %s", config.Hash)
	}
	if len(config.Users) != 0 {
		t.Error("Default Users should be empty")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 220 {
		t.Errorf("Expected priority 220, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "basic_auth" {
		t.Errorf("Expected name 'basic_auth', got %s", mw.Name())
	}
}

func TestContextHelpers(t *testing.T) {
	// Test WithUsername and GetUsername
	ctx := WithUsername(t.Context(), "testuser")
	username := GetUsername(ctx)

	if username != "testuser" {
		t.Errorf("GetUsername returned %s, want testuser", username)
	}

	// Test GetUsername with no username
	ctx2 := t.Context()
	username2 := GetUsername(ctx2)
	if username2 != "" {
		t.Errorf("GetUsername should return empty string, got %s", username2)
	}
}

func TestExtractCredentials(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantUser   string
		wantPass   string
		wantOK     bool
	}{
		{
			name:       "valid credentials",
			authHeader: "Basic YWRtaW46cGFzc3dvcmQxMjM=",
			wantUser:   "admin",
			wantPass:   "password123",
			wantOK:     true,
		},
		{
			name:       "empty header",
			authHeader: "",
			wantUser:   "",
			wantPass:   "",
			wantOK:     false,
		},
		{
			name:       "wrong scheme",
			authHeader: "Bearer token123",
			wantUser:   "",
			wantPass:   "",
			wantOK:     false,
		},
		{
			name:       "invalid base64",
			authHeader: "Basic !!!invalid!!!",
			wantUser:   "",
			wantPass:   "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			user, pass, ok := extractCredentials(req)
			if ok != tt.wantOK {
				t.Errorf("extractCredentials() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if user != tt.wantUser {
				t.Errorf("extractCredentials() user = %v, want %v", user, tt.wantUser)
			}
			if pass != tt.wantPass {
				t.Errorf("extractCredentials() pass = %v, want %v", pass, tt.wantPass)
			}
		})
	}
}
