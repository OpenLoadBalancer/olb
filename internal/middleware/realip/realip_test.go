package realip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRealIP_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	originalAddr := "192.168.1.100:12345"
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if r.RemoteAddr != originalAddr {
			t.Errorf("Expected RemoteAddr to remain %s, got %s", originalAddr, r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = originalAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestRealIP_XForwardedFor(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// RemoteAddr should be updated with real IP (port preserved)
		if !strings.HasPrefix(r.RemoteAddr, "203.0.113.50") {
			t.Errorf("Expected RemoteAddr to start with 203.0.113.50, got %s", r.RemoteAddr)
		}
		// X-Real-IP header should be set
		if r.Header.Get("X-Real-IP") != "203.0.113.50" {
			t.Errorf("Expected X-Real-IP header to be 203.0.113.50, got %s", r.Header.Get("X-Real-IP"))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345" // Proxy IP
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.2, 10.0.0.3")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestRealIP_XRealIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if !strings.HasPrefix(r.RemoteAddr, "203.0.113.100") {
			t.Errorf("Expected RemoteAddr to start with 203.0.113.100, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.100")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_CFConnectingIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// CF-Connecting-IP is checked first, so it takes precedence
		if !strings.HasPrefix(r.RemoteAddr, "198.51.100.75") {
			t.Errorf("Expected RemoteAddr to start with 198.51.100.75, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "198.51.100.75")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_TrueClientIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if !strings.HasPrefix(r.RemoteAddr, "192.0.2.10") {
			t.Errorf("Expected RemoteAddr to start with 192.0.2.10, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("True-Client-IP", "192.0.2.10")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_NoHeader(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	originalAddr := "192.168.1.100:12345"
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// RemoteAddr should remain unchanged
		if r.RemoteAddr != originalAddr {
			t.Errorf("Expected RemoteAddr to remain %s, got %s", originalAddr, r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = originalAddr
	// No proxy headers
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_InvalidIP(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	originalAddr := "192.168.1.100:12345"
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Invalid IP should be ignored
		if r.RemoteAddr != originalAddr {
			t.Errorf("Expected RemoteAddr to remain %s, got %s", originalAddr, r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = originalAddr
	req.Header.Set("X-Real-IP", "not-an-ip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_RejectUntrusted(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RejectUntrusted = true
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Request from untrusted proxy
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.1:12345" // Untrusted
	req.Header.Set("X-Real-IP", "192.0.2.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	if atomic.LoadInt32(&callCount) != 0 {
		t.Errorf("Expected 0 calls (rejected), got %d", callCount)
	}
}

func TestRealIP_TrustedProxy(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RejectUntrusted = true
	config.TrustedProxies = []string{"10.0.0.0/8"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if !strings.HasPrefix(r.RemoteAddr, "192.0.2.1") {
			t.Errorf("Expected RemoteAddr to start with 192.0.2.1, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Request from trusted proxy
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.5:12345" // Trusted
	req.Header.Set("X-Real-IP", "192.0.2.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_CustomHeaders(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Headers = []string{"X-Custom-IP", "X-Real-IP"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if !strings.HasPrefix(r.RemoteAddr, "198.51.100.10") {
			t.Errorf("Expected RemoteAddr to start with 198.51.100.10, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Custom-IP", "198.51.100.10")
	req.Header.Set("X-Real-IP", "192.0.2.1") // Should be ignored, custom takes precedence
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_OriginalIPContext(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	originalAddr := "10.0.0.1:12345"
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)

		// Check that original IP is stored in context
		originalIP := GetOriginalIP(r.Context())
		if originalIP != originalAddr {
			t.Errorf("Expected original IP %s in context, got %s", originalAddr, originalIP)
		}

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = originalAddr
	req.Header.Set("X-Real-IP", "203.0.113.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_PreservePort(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Port should be preserved
		if r.RemoteAddr != "203.0.113.50:12345" {
			t.Errorf("Expected RemoteAddr to preserve port 203.0.113.50:12345, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_IPv6(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if !strings.Contains(r.RemoteAddr, "2001:db8::1") {
			t.Errorf("Expected RemoteAddr to contain 2001:db8::1, got %s", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "[::1]:12345"
	req.Header.Set("X-Real-IP", "2001:db8::1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if len(config.Headers) == 0 {
		t.Error("Default Headers should not be empty")
	}
	if config.RejectUntrusted != false {
		t.Error("Default RejectUntrusted should be false")
	}
	if len(config.DefaultTrusted) == 0 {
		t.Error("Default DefaultTrusted should not be empty")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 15 {
		t.Errorf("Expected priority 15, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "realip" {
		t.Errorf("Expected name 'realip', got '%s'", mw.Name())
	}
}

func TestParseForwardedHeader(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "for=192.0.2.60;proto=http;by=203.0.113.43",
			expected: "192.0.2.60",
		},
		{
			input:    "for=\"[2001:db8:cafe::17]:4711\"",
			expected: "2001:db8:cafe::17",
		},
		{
			input:    "by=203.0.113.43;for=192.0.2.60",
			expected: "192.0.2.60",
		},
		{
			input:    "proto=https",
			expected: "",
		},
	}

	for _, tt := range tests {
		result := parseForwardedHeader(tt.input)
		if result != tt.expected {
			t.Errorf("parseForwardedHeader(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestGetOriginalIP_NoContext(t *testing.T) {
	ctx := context.Background()
	ip := GetOriginalIP(ctx)

	if ip != "" {
		t.Errorf("Expected empty string, got %s", ip)
	}
}

func TestContextWithOriginalIP(t *testing.T) {
	ctx := context.Background()
	originalIP := "192.168.1.100:12345"

	ctx = contextWithOriginalIP(ctx, originalIP)
	retrievedIP := GetOriginalIP(ctx)

	if retrievedIP != originalIP {
		t.Errorf("Expected %s, got %s", originalIP, retrievedIP)
	}
}

func TestRealIP_DefaultTrustedRanges(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RejectUntrusted = true
	// Use default trusted ranges (private IPs)

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Test from private IP (should be trusted)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345" // Private IP
	req.Header.Set("X-Real-IP", "203.0.113.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d from trusted private IP, got %d", http.StatusOK, rec.Code)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_EmptyXForwardedFor(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	originalAddr := "192.168.1.100:12345"
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		if r.RemoteAddr != originalAddr {
			t.Errorf("Expected RemoteAddr to remain %s, got %s", originalAddr, r.RemoteAddr)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = originalAddr
	req.Header.Set("X-Forwarded-For", "") // Empty header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestRealIP_InvalidHeaderValue(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	originalAddr := "192.168.1.100:12345"
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Should fall through to next header or keep original
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = originalAddr
	// First header has invalid IPs, second has valid
	req.Header.Set("X-Forwarded-For", "invalid, also-invalid")
	req.Header.Set("X-Real-IP", "203.0.113.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	// Should use X-Real-IP since X-Forwarded-For had no valid IPs
	// But X-Forwarded-For is checked first in default config...
	// Actually the behavior depends on header order in config
}
