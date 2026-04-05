package apikey

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKey_Disabled(t *testing.T) {
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
		t.Error("Handler should have been called when API Key auth is disabled")
	}
}

func TestAPIKey_MissingKey(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Keys = map[string]string{
		"key1": "secret123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without API key")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_InvalidKey(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Keys = map[string]string{
		"key1": "secret123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid API key")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_ValidKeyHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Keys = map[string]string{
		"key1": "secret123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedKeyID string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKeyID = GetAPIKeyID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedKeyID != "key1" {
		t.Errorf("Expected key ID 'key1', got '%s'", receivedKeyID)
	}
}

func TestAPIKey_ValidKeyQueryParam(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Keys = map[string]string{
		"key1": "secret123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedKeyID string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKeyID = GetAPIKeyID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test?api_key=secret123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedKeyID != "key1" {
		t.Errorf("Expected key ID 'key1', got '%s'", receivedKeyID)
	}
}

func TestAPIKey_HeaderPreferredOverQuery(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Keys = map[string]string{
		"key1": "header-key",
		"key2": "query-key",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedKeyID string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKeyID = GetAPIKeyID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Both header and query param provided, header should win
	req := httptest.NewRequest("GET", "/test?api_key=query-key", nil)
	req.Header.Set("X-API-Key", "header-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if receivedKeyID != "key1" {
		t.Errorf("Expected key ID 'key1' from header, got '%s'", receivedKeyID)
	}
}

func TestAPIKey_ExcludedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/public"}
	config.Keys = map[string]string{
		"key1": "secret123",
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

func TestAPIKey_SHA256(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "sha256"
	config.Keys = map[string]string{
		"key1": "secret123",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedKeyID string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKeyID = GetAPIKeyID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedKeyID != "key1" {
		t.Errorf("Expected key ID 'key1', got '%s'", receivedKeyID)
	}
}

func TestAPIKey_CustomHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Header = "Authorization"
	config.Keys = map[string]string{
		"key1": "my-api-key",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	var receivedKeyID string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKeyID = GetAPIKeyID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "my-api-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if receivedKeyID != "key1" {
		t.Errorf("Expected key ID 'key1', got '%s'", receivedKeyID)
	}
}

func TestAPIKey_MultipleKeys(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Hash = "plain"
	config.Keys = map[string]string{
		"key1": "secret1",
		"key2": "secret2",
		"key3": "secret3",
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		key      string
		expected string
	}{
		{"secret1", "key1"},
		{"secret2", "key2"},
		{"secret3", "key3"},
	}

	for _, tt := range tests {
		var receivedKeyID string
		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedKeyID = GetAPIKeyID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", tt.key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if receivedKeyID != tt.expected {
			t.Errorf("Key %s: expected ID '%s', got '%s'", tt.key, tt.expected, receivedKeyID)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Header != "X-API-Key" {
		t.Errorf("Default Header should be 'X-API-Key', got '%s'", config.Header)
	}
	if config.QueryParam != "api_key" {
		t.Errorf("Default QueryParam should be 'api_key', got '%s'", config.QueryParam)
	}
	if config.Hash != "sha256" {
		t.Errorf("Default Hash should be 'sha256', got '%s'", config.Hash)
	}
	if len(config.Keys) != 0 {
		t.Error("Default Keys should be empty")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 215 {
		t.Errorf("Expected priority 215, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "api_key" {
		t.Errorf("Expected name 'api_key', got '%s'", mw.Name())
	}
}

func TestContextHelpers(t *testing.T) {
	// Test WithAPIKeyID and GetAPIKeyID
	ctx := WithAPIKeyID(t.Context(), "key123")
	keyID := GetAPIKeyID(ctx)

	if keyID != "key123" {
		t.Errorf("GetAPIKeyID returned %s, want key123", keyID)
	}

	// Test GetAPIKeyID with no key
	ctx2 := t.Context()
	keyID2 := GetAPIKeyID(ctx2)
	if keyID2 != "" {
		t.Errorf("GetAPIKeyID should return empty string, got %s", keyID2)
	}
}

func TestKeyInfoContext(t *testing.T) {
	info := KeyInfo{
		Name:        "Test Key",
		Permissions: []string{"read", "write"},
		Metadata: map[string]string{
			"owner": "test-user",
		},
	}

	ctx := WithKeyInfo(t.Context(), info)
	retrieved := GetKeyInfo(ctx)

	if retrieved.Name != "Test Key" {
		t.Errorf("KeyInfo.Name = %s, want Test Key", retrieved.Name)
	}
}

func TestHasPermission(t *testing.T) {
	info := KeyInfo{
		Name:        "Test Key",
		Permissions: []string{"read", "write"},
	}

	ctx := WithKeyInfo(t.Context(), info)

	if !HasPermission(ctx, "read") {
		t.Error("HasPermission should return true for 'read'")
	}

	if !HasPermission(ctx, "write") {
		t.Error("HasPermission should return true for 'write'")
	}

	if HasPermission(ctx, "delete") {
		t.Error("HasPermission should return false for 'delete'")
	}
}

func TestHasPermission_Wildcard(t *testing.T) {
	info := KeyInfo{
		Name:        "Admin Key",
		Permissions: []string{"*"},
	}

	ctx := WithKeyInfo(t.Context(), info)

	if !HasPermission(ctx, "read") {
		t.Error("HasPermission should return true with wildcard")
	}

	if !HasPermission(ctx, "delete") {
		t.Error("HasPermission should return true with wildcard")
	}
}

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		queryParam string
		headerVal  string
		queryVal   string
		wantKey    string
	}{
		{
			name:      "from header",
			header:    "X-API-Key",
			headerVal: "my-key",
			wantKey:   "my-key",
		},
		{
			name:       "from query",
			queryParam: "api_key",
			queryVal:   "query-key",
			wantKey:    "query-key",
		},
		{
			name:       "header preferred",
			header:     "X-API-Key",
			queryParam: "api_key",
			headerVal:  "header-key",
			queryVal:   "query-key",
			wantKey:    "header-key",
		},
		{
			name:    "empty",
			wantKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.queryVal != "" {
				req.URL.RawQuery = "api_key=" + tt.queryVal
			}
			if tt.headerVal != "" {
				req.Header.Set(tt.header, tt.headerVal)
			}

			key := extractAPIKey(req, tt.header, tt.queryParam)
			if key != tt.wantKey {
				t.Errorf("extractAPIKey() = %v, want %v", key, tt.wantKey)
			}
		})
	}
}
