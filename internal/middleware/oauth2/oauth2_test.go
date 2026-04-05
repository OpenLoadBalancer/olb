package oauth2

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuth2_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when OAuth2 is disabled")
	}
}

func TestOAuth2_MissingToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestOAuth2_InvalidTokenFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid token format")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestOAuth2_ValidToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedSubject string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := GetTokenInfo(r.Context())
		if info != nil {
			receivedSubject = info.Subject
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Valid JWT format token (will pass mock validation)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyMTIzIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedSubject != "user" {
		t.Errorf("Expected subject 'user', got '%s'", receivedSubject)
	}
}

func TestOAuth2_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/public"}

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

func TestOAuth2_InsufficientScope(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "
	config.Scopes = []string{"write", "admin"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with insufficient scope")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid.token.here")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestOAuth2_NoPrefix(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "" // No prefix required

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestOAuth2_OpaqueToken(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Prefix = "Bearer "

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedActive bool
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := GetTokenInfo(r.Context())
		if info != nil {
			receivedActive = info.Active
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Opaque token (not JWT format)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer opaque-token-12345")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if !receivedActive {
		t.Error("Expected token to be active")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Header != "Authorization" {
		t.Errorf("Default Header should be 'Authorization', got '%s'", config.Header)
	}
	if config.Prefix != "Bearer " {
		t.Errorf("Default Prefix should be 'Bearer ', got '%s'", config.Prefix)
	}
	if config.CacheDuration != "1h" {
		t.Errorf("Default CacheDuration should be '1h', got '%s'", config.CacheDuration)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 212 {
		t.Errorf("Expected priority 212, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "oauth2" {
		t.Errorf("Expected name 'oauth2', got '%s'", mw.Name())
	}
}

func TestHasRequiredScopes(t *testing.T) {
	config := DefaultConfig()
	config.Scopes = []string{"read", "write"}

	mw, _ := New(config)

	tests := []struct {
		tokenScope string
		want       bool
	}{
		{"read write", true},
		{"read write admin", true},
		{"read", false},
		{"write", false},
		{"", false},
		{"admin", false},
	}

	for _, tt := range tests {
		got := mw.hasRequiredScopes(tt.tokenScope)
		if got != tt.want {
			t.Errorf("hasRequiredScopes(%q) = %v, want %v", tt.tokenScope, got, tt.want)
		}
	}
}

func TestContextHelpers(t *testing.T) {
	// Test WithTokenInfo and GetTokenInfo
	info := &TokenInfo{
		Subject:   "user123",
		Scope:     "read write",
		Active:    true,
		TokenType: "Bearer",
	}

	ctx := WithTokenInfo(t.Context(), info)
	retrieved := GetTokenInfo(ctx)

	if retrieved == nil {
		t.Fatal("GetTokenInfo returned nil")
	}

	if retrieved.Subject != "user123" {
		t.Errorf("Subject = %s, want user123", retrieved.Subject)
	}

	// Test GetSubject
	subject := GetSubject(ctx)
	if subject != "user123" {
		t.Errorf("GetSubject = %s, want user123", subject)
	}

	// Test GetScopes
	scopes := GetScopes(ctx)
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("GetScopes = %v, want [read write]", scopes)
	}

	// Test HasScope
	if !HasScope(ctx, "read") {
		t.Error("HasScope(read) should be true")
	}
	if !HasScope(ctx, "write") {
		t.Error("HasScope(write) should be true")
	}
	if HasScope(ctx, "admin") {
		t.Error("HasScope(admin) should be false")
	}
}

func TestGetTokenInfo_NoInfo(t *testing.T) {
	ctx := t.Context()
	info := GetTokenInfo(ctx)

	if info != nil {
		t.Error("GetTokenInfo should return nil when no info is set")
	}
}

func TestGetSubject_NoInfo(t *testing.T) {
	ctx := t.Context()
	subject := GetSubject(ctx)

	if subject != "" {
		t.Errorf("GetSubject should return empty string, got %s", subject)
	}
}

func TestGetScopes_NoInfo(t *testing.T) {
	ctx := t.Context()
	scopes := GetScopes(ctx)

	if scopes != nil {
		t.Errorf("GetScopes should return nil, got %v", scopes)
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		prefix    string
		headerVal string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			header:    "Authorization",
			prefix:    "Bearer ",
			headerVal: "Bearer my-token",
			wantToken: "my-token",
			wantErr:   false,
		},
		{
			name:      "no prefix",
			header:    "Authorization",
			prefix:    "",
			headerVal: "my-token",
			wantToken: "my-token",
			wantErr:   false,
		},
		{
			name:    "missing header",
			header:  "Authorization",
			prefix:  "Bearer ",
			wantErr: true,
		},
		{
			name:      "wrong prefix",
			header:    "Authorization",
			prefix:    "Bearer ",
			headerVal: "Basic my-token",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.Header = tt.header
			config.Prefix = tt.prefix

			mw, _ := New(config)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.headerVal != "" {
				req.Header.Set(tt.header, tt.headerVal)
			}

			token, err := mw.extractToken(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if token != tt.wantToken {
				t.Errorf("extractToken() = %v, want %v", token, tt.wantToken)
			}
		})
	}
}

func TestValidateToken(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "JWT format",
			token:   "header.payload.signature",
			wantErr: false,
		},
		{
			name:    "opaque token",
			token:   "opaque-token-string",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := mw.validateToken(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && info == nil {
				t.Error("validateToken() should return info when no error")
			}
		})
	}
}
