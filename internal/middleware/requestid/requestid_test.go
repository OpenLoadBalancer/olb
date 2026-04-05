package requestid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if Get(r.Context()) != "" {
			t.Error("Request ID should not be set when disabled")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}

	if rec.Header().Get("X-Request-ID") != "" {
		t.Error("Response should not have request ID header when disabled")
	}
}

func TestRequestID_Generate(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Generate = true

	mw := New(config)

	var capturedID string
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = Get(r.Context())
		if capturedID == "" {
			t.Error("Request ID should be generated")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	responseID := rec.Header().Get("X-Request-ID")
	if responseID == "" {
		t.Error("Response should have request ID header")
	}

	if capturedID != responseID {
		t.Error("Context ID and response ID should match")
	}

	if len(responseID) != 32 {
		t.Errorf("Expected 32 character hex ID, got %d chars", len(responseID))
	}
}

func TestRequestID_PreserveExisting(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Generate = true

	mw := New(config)

	existingID := "existing-request-id-123"
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := Get(r.Context())
		if id != existingID {
			t.Errorf("Expected existing ID %q, got %q", existingID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != existingID {
		t.Error("Response should preserve existing request ID")
	}
}

func TestRequestID_NoGenerate(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Generate = false

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Get(r.Context()) != "" {
			t.Error("Request ID should not be generated when Generate is false")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "" {
		t.Error("Response should not have request ID when not generated")
	}
}

func TestRequestID_NoResponse(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Generate = true
	config.Response = false

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Get(r.Context()) == "" {
			t.Error("Request ID should be in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "" {
		t.Error("Response should not have request ID header when Response is false")
	}
}

func TestRequestID_CustomHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Header = "X-Trace-ID"

	mw := New(config)

	existingID := "custom-trace-id"
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Get(r.Context()) != existingID {
			t.Error("Should read from custom header")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Trace-ID", existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Trace-ID") != existingID {
		t.Error("Response should use custom header name")
	}
}

func TestRequestID_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Generate = true
	config.ExcludePaths = []string{"/health", "/metrics"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Get(r.Context()) != "" {
			t.Error("Request ID should not be set for excluded paths")
		}
		w.WriteHeader(http.StatusOK)
	}))

	tests := []string{"/health", "/health/live", "/metrics", "/metrics/prometheus"}
	for _, path := range tests {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("X-Request-ID") != "" {
			t.Errorf("Response should not have request ID for excluded path %s", path)
		}
	}
}

func TestRequestID_AllowedPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Generate = true
	config.ExcludePaths = []string{"/health"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Get(r.Context()) == "" {
			t.Error("Request ID should be set for non-excluded paths")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("Response should have request ID for non-excluded path")
	}
}

func TestRequestID_CustomLength(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Length = 8 // 16 hex chars

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if len(id) != 16 {
		t.Errorf("Expected 16 character ID with length=8, got %d chars", len(id))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Header != "X-Request-ID" {
		t.Errorf("Default Header should be X-Request-ID, got %s", config.Header)
	}
	if config.Generate != true {
		t.Error("Default Generate should be true")
	}
	if config.Length != 16 {
		t.Errorf("Default Length should be 16, got %d", config.Length)
	}
	if config.Response != true {
		t.Error("Default Response should be true")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 90 {
		t.Errorf("Expected priority 90, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "requestid" {
		t.Errorf("Expected name 'requestid', got '%s'", mw.Name())
	}
}

func TestGet_EmptyContext(t *testing.T) {
	ctx := context.Background()
	if Get(ctx) != "" {
		t.Error("Get should return empty string for empty context")
	}
}

func TestSet(t *testing.T) {
	ctx := context.Background()
	id := "test-id-123"
	ctx = Set(ctx, id)

	if Get(ctx) != id {
		t.Errorf("Expected ID %q, got %q", id, Get(ctx))
	}
}

func TestGenerateID_Length(t *testing.T) {
	tests := []int{8, 16, 32}
	for _, length := range tests {
		id := generateID(length)
		expectedLen := length * 2 // hex encoding doubles the size
		if len(id) != expectedLen {
			t.Errorf("generateID(%d) returned %d chars, expected %d", length, len(id), expectedLen)
		}

		// Verify it's valid hex
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Errorf("generateID returned non-hex character: %c", c)
			}
		}
	}
}

func TestFallbackID(t *testing.T) {
	id := fallbackID()
	if len(id) != 32 {
		t.Errorf("fallbackID should return 32 char hex string, got %d chars", len(id))
	}

	// Should be deterministic
	id2 := fallbackID()
	if id != id2 {
		t.Error("fallbackID should be deterministic")
	}
}

func TestNew_DefaultValues(t *testing.T) {
	config := Config{
		Enabled: true,
		// Leave other fields empty
	}

	mw := New(config)

	if mw.config.Header != "X-Request-ID" {
		t.Errorf("Expected default header X-Request-ID, got %s", mw.config.Header)
	}
	if mw.config.Length != 16 {
		t.Errorf("Expected default length 16, got %d", mw.config.Length)
	}
}
