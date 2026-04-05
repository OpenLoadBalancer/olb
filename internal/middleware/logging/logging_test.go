package logging

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLogging_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}
}

func TestLogging_CommonFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "common"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Capture output would require redirecting stdout, skip for now
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestLogging_CombinedFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "combined"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))

	req := httptest.NewRequest("POST", "/api/users", nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req.Header.Set("Referer", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rec.Code)
	}
}

func TestLogging_JSONFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "json"
	config.Fields = []string{"timestamp", "method", "path", "status"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should complete without error
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestLogging_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "common"
	config.ExcludePaths = []string{"/health", "/metrics"}

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/health/live", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called for excluded path")
	}
}

func TestLogging_ExcludeStatus(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "common"
	config.ExcludeStatus = []int{404, 304}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/notfound", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should complete (logging skipped for 404)
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestLogging_MinDuration(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "common"
	config.MinDuration = "1s"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/fast", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Fast request should be skipped due to MinDuration filter
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestLogging_CustomFormat(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "custom"
	config.CustomFormat = "[$timestamp] $method $path -> $status"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/api/item", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestLogging_BytesSent(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "json"
	config.Fields = []string{"bytes"}

	mw := New(config)

	body := []byte("Hello, World!")
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.Len() != len(body) {
		t.Errorf("Expected body length %d, got %d", len(body), rec.Body.Len())
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Format != "combined" {
		t.Errorf("Default Format should be 'combined', got '%s'", config.Format)
	}
	if len(config.Fields) == 0 {
		t.Error("Default Fields should not be empty")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 80 {
		t.Errorf("Expected priority 80, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "logging" {
		t.Errorf("Expected name 'logging', got '%s'", mw.Name())
	}
}

func TestResponseRecorder(t *testing.T) {
	rec := httptest.NewRecorder()
	rr := &responseRecorder{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// Test WriteHeader
	rr.WriteHeader(http.StatusCreated)
	if rr.statusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rr.statusCode)
	}

	// Test double WriteHeader (should be ignored)
	rr.WriteHeader(http.StatusOK)
	if rr.statusCode != http.StatusCreated {
		t.Error("Second WriteHeader should not change status")
	}

	// Test Write
	data := []byte("test data")
	n, err := rr.Write(data)
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected %d bytes written, got %d", len(data), n)
	}
	if rr.bytesSent != len(data) {
		t.Errorf("Expected bytesSent %d, got %d", len(data), rr.bytesSent)
	}

	// Test Header
	h := rr.Header()
	if h == nil {
		t.Error("Header() should return non-nil map")
	}
}

func TestBuildCombinedLogEntry(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	mw := New(config)

	req := httptest.NewRequest("GET", "/path", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://example.com")

	rec := &responseRecorder{
		statusCode: http.StatusOK,
		bytesSent:  1234,
	}

	entry := mw.buildCombinedLogEntry(req, rec, time.Millisecond*150, time.Now())

	if !strings.Contains(entry, "GET /path") {
		t.Error("Combined log should contain request line")
	}
	if !strings.Contains(entry, "200") {
		t.Error("Combined log should contain status code")
	}
	if !strings.Contains(entry, "Mozilla/5.0") {
		t.Error("Combined log should contain user agent")
	}
}

func TestBuildCommonLogEntry(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	mw := New(config)

	req := httptest.NewRequest("POST", "/api", nil)
	rec := &responseRecorder{
		statusCode: http.StatusCreated,
		bytesSent:  0,
	}

	entry := mw.buildCommonLogEntry(req, rec, time.Second, time.Now())

	if !strings.Contains(entry, "POST /api") {
		t.Error("Common log should contain request line")
	}
	if !strings.Contains(entry, "201") {
		t.Error("Common log should contain status code")
	}
}

func TestBuildJSONEntry(t *testing.T) {
	config := DefaultConfig()
	config.Format = "json"
	config.Fields = []string{"method", "path", "status", "bytes"}
	config.RequestHeaders = []string{"Content-Type"}

	mw := New(config)

	req := httptest.NewRequest("PUT", "/resource", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-123")

	rec := &responseRecorder{
		statusCode: http.StatusNoContent,
		bytesSent:  0,
	}

	entry := mw.buildJSONEntry(req, rec, time.Millisecond*50, time.Now())

	if !strings.Contains(entry, "\"method\":\"PUT\"") {
		t.Error("JSON log should contain method")
	}
	if !strings.Contains(entry, "\"path\":\"/resource\"") {
		t.Error("JSON log should contain path")
	}
	if !strings.Contains(entry, "\"status\":204") {
		t.Error("JSON log should contain status")
	}
}

func TestBuildCustomEntry(t *testing.T) {
	config := DefaultConfig()
	config.Format = "custom"
	config.CustomFormat = "$method $status"

	mw := New(config)

	req := httptest.NewRequest("PATCH", "/item", nil)
	rec := &responseRecorder{
		statusCode: http.StatusOK,
	}

	entry := mw.buildCustomEntry(req, rec, time.Millisecond, time.Now())

	if !strings.Contains(entry, "PATCH") {
		t.Error("Custom log should contain method")
	}
	if !strings.Contains(entry, "200") {
		t.Error("Custom log should contain status")
	}
}

func TestBuildCustomEntry_EmptyFormat(t *testing.T) {
	config := DefaultConfig()
	config.Format = "custom"
	config.CustomFormat = ""

	mw := New(config)

	req := httptest.NewRequest("GET", "/", nil)
	rec := &responseRecorder{
		statusCode: http.StatusOK,
	}

	// Should fall back to combined format
	entry := mw.buildCustomEntry(req, rec, time.Millisecond, time.Now())

	// Combined format has specific pattern: IP - - [timestamp] "..."
	if !strings.Contains(entry, "[") || !strings.Contains(entry, "GET /") {
		t.Error("Empty custom format should fall back to combined")
	}
}

func TestEscapeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello\"world", "hello\\\"world"},
		{"hello\\world", "hello\\\\world"},
		{"hello\nworld", "hello\\nworld"},
		{"hello\tworld", "hello\\tworld"},
	}

	for _, tc := range tests {
		result := escapeJSON(tc.input)
		if result != tc.expected {
			t.Errorf("escapeJSON(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestNew_MinDuration(t *testing.T) {
	config := Config{
		Enabled:     true,
		MinDuration: "500ms",
	}
	mw := New(config)

	if mw.minDuration != 500*time.Millisecond {
		t.Errorf("Expected minDuration 500ms, got %v", mw.minDuration)
	}
}

func TestNew_InvalidMinDuration(t *testing.T) {
	config := Config{
		Enabled:     true,
		MinDuration: "invalid",
	}
	mw := New(config)

	if mw.minDuration != 0 {
		t.Error("Invalid MinDuration should result in 0")
	}
}

func TestBuildLogEntry_UnknownFormat(t *testing.T) {
	config := DefaultConfig()
	config.Format = "unknown"

	mw := New(config)

	req := httptest.NewRequest("GET", "/", nil)
	rec := &responseRecorder{
		statusCode: http.StatusOK,
	}

	entry := mw.buildLogEntry(req, rec, time.Millisecond, time.Now())

	// Should default to combined
	if !strings.Contains(entry, "GET /") {
		t.Error("Unknown format should default to combined")
	}
}

func TestLogging_AllFields(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Format = "json"
	config.Fields = []string{
		"timestamp", "method", "path", "query", "status", "duration",
		"bytes", "ip", "user_agent", "referer", "request_id", "host", "proto",
	}

	mw := New(config)

	req := httptest.NewRequest("GET", "/search?q=test", nil)
	req.Host = "api.example.com"
	req.Header.Set("User-Agent", "Test")
	req.Header.Set("Referer", "https://ref.example.com")
	req.Header.Set("X-Request-ID", "abc-123")

	rec := &responseRecorder{
		statusCode: http.StatusOK,
		bytesSent:  100,
	}

	entry := mw.buildJSONEntry(req, rec, time.Millisecond*100, time.Now())

	required := []string{
		"\"timestamp\"", "\"method\"", "\"path\"", "\"query\"",
		"\"status\"", "\"duration_ms\"", "\"bytes_sent\"",
		"\"client_ip\"", "\"user_agent\"", "\"referer\"",
		"\"request_id\"", "\"host\"", "\"proto\"",
	}

	for _, field := range required {
		if !strings.Contains(entry, field) {
			t.Errorf("JSON entry should contain %s", field)
		}
	}
}
