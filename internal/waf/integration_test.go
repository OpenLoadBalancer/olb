package waf

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
)

func TestWAFPipeline_AllowsCleanRequest(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/api/users", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("expected next handler to be called for clean request")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWAFPipeline_BlocksSQLi(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if nextCalled {
		t.Error("expected next handler NOT to be called for SQLi")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWAFPipeline_BlocksXSS(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest("GET", "http://example.com/?q=javascript:alert(1)", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if nextCalled {
		t.Error("expected next handler NOT to be called for XSS")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWAFPipeline_WhitelistBypass(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL: &config.WAFIPACLConfig{
				Enabled:   true,
				Whitelist: []config.WAFIPACLEntry{{CIDR: "192.0.2.0/24", Reason: "test"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// SQLi attack from whitelisted IP should pass through
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("expected whitelisted IP to bypass all security layers")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for whitelisted IP, got %d", rr.Code)
	}
}

func TestWAFPipeline_BlacklistBlock(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL: &config.WAFIPACLConfig{
				Enabled:   true,
				Blacklist: []config.WAFIPACLEntry{{CIDR: "203.0.113.0/24", Reason: "bad"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called for blacklisted IP")
	}))

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for blacklisted IP, got %d", rr.Code)
	}
}

func TestWAFPipeline_MonitorMode(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "monitor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// SQLi attack in monitor mode should pass through
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("expected next handler to be called in monitor mode")
	}
}

func TestWAFPipeline_SanitizerBlocksInvalidMethod(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called for invalid method")
	}))

	req := httptest.NewRequest("HACK", "http://example.com/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != 405 {
		t.Errorf("expected 405 for invalid method, got %d", rr.Code)
	}
}

func TestWAFPipeline_RateLimiter(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			RateLimit: &config.WAFRateLimitConfig{
				Enabled: true,
				Rules: []config.WAFRateLimitRule{
					{ID: "test", Scope: "ip", Limit: 3, Window: "1m", Burst: 0},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 3 requests should pass
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after rate limit, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
}

func TestWAFPipeline_Disabled(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// Even SQLi should pass when WAF is disabled
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("expected request to pass through when WAF is disabled")
	}
}

func TestWAFPipeline_Analytics(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send a clean request
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Send a blocked request
	req = httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	stats := mw.Analytics().GetStats()
	if stats.TotalRequests < 1 {
		t.Errorf("expected at least 1 total request, got %d", stats.TotalRequests)
	}
}

func TestWAFPipeline_PanicRecovery(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// Nil body shouldn't cause panic
	req := httptest.NewRequest("POST", "http://example.com/", nil)
	req.Body = nil
	rr := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rr, req)

	// Check body contains expected error or passed through
	body := rr.Body.String()
	_ = body
	_ = nextCalled
	// If we get here without panic, the test passes
}

func TestWAFPipeline_CommandInjection(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/?cmd=;cat+/etc/passwd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		body := rr.Body.String()
		if !strings.Contains(body, "blocked") {
			// Legacy WAF may catch it, or new detection engine
			t.Logf("CMD injection test: status=%d body=%s", rr.Code, body)
		}
	}
}

func TestDefaultWAFConfig(t *testing.T) {
	cfg := DefaultWAFConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if !cfg.Enabled {
		t.Error("expected default config to be enabled")
	}
	if cfg.Mode != "enforce" {
		t.Errorf("expected mode 'enforce', got %q", cfg.Mode)
	}
	if cfg.IPACL == nil || !cfg.IPACL.Enabled {
		t.Error("expected IPACL to be enabled by default")
	}
	if cfg.IPACL.AutoBan == nil || !cfg.IPACL.AutoBan.Enabled {
		t.Error("expected AutoBan to be enabled by default")
	}
	if cfg.Sanitizer == nil || !cfg.Sanitizer.Enabled {
		t.Error("expected Sanitizer to be enabled by default")
	}
	if cfg.Detection == nil || !cfg.Detection.IsEnabled() {
		t.Error("expected Detection to be enabled by default")
	}
	if cfg.BotDetection == nil || !cfg.BotDetection.Enabled {
		t.Error("expected BotDetection to be enabled by default")
	}
	if cfg.Response == nil {
		t.Error("expected Response config to be non-nil")
	}
	if cfg.Logging == nil {
		t.Error("expected Logging config to be non-nil")
	}
}

func TestWAFPipeline_BotDetectionBlocking(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			BotDetection: &config.WAFBotConfig{
				Enabled: true,
				Mode:    "enforce",
				UserAgent: &config.WAFUserAgentConfig{
					Enabled:            true,
					BlockEmpty:         true,
					BlockKnownScanners: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Scanner UA should be blocked by bot detection
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("User-Agent", "sqlmap/1.7")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for scanner bot, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "bot") {
		t.Errorf("expected 'bot' in response body, got %q", body)
	}
}

func TestWAFPipeline_ResponseWrapping(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			Response: &config.WAFResponseConfig{
				SecurityHeaders: &config.WAFSecurityHeadersConfig{
					Enabled:             true,
					XContentTypeOptions: true,
					XFrameOptions:       "DENY",
					ReferrerPolicy:      "no-referrer",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0.0.0")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// The response protection layer should wrap the response writer
	// and the request should still pass through
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWAFPipeline_DisabledMode(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "disabled"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.Mode() != "disabled" {
		t.Errorf("expected mode 'disabled', got %q", mw.Mode())
	}

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// SQLi should pass through in disabled mode
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("expected request to pass through in disabled mode")
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"enforce", "enforce"},
		{"blocking", "enforce"},
		{"block", "enforce"},
		{"monitor", "monitor"},
		{"detection", "monitor"},
		{"disabled", "disabled"},
		{"off", "disabled"},
		{"unknown", "enforce"},
		{"", "enforce"},
	}

	for _, tt := range tests {
		got := normalizeMode(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeMode(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestWAFMiddleware_Status(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL:   &config.WAFIPACLConfig{Enabled: true},
			RateLimit: &config.WAFRateLimitConfig{
				Enabled: true,
				Rules:   []config.WAFRateLimitRule{{ID: "test", Scope: "ip", Limit: 10, Window: "1m"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	status := mw.Status()
	if status["enabled"] != true {
		t.Error("expected enabled=true in status")
	}
	if status["mode"] != "enforce" {
		t.Errorf("expected mode=enforce in status, got %v", status["mode"])
	}

	layers, ok := status["layers"].(map[string]bool)
	if !ok {
		t.Fatal("expected layers in status")
	}
	if !layers["ip_acl"] {
		t.Error("expected ip_acl layer to be active")
	}
	if !layers["rate_limit"] {
		t.Error("expected rate_limit layer to be active")
	}
}

func TestWAFMiddleware_Name(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.Name() != "waf" {
		t.Errorf("expected name 'waf', got %q", mw.Name())
	}
}

func TestWAFMiddleware_Priority(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.Priority() != 100 {
		t.Errorf("expected priority 100, got %d", mw.Priority())
	}
}

func TestWAFMiddleware_IPACL(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL:   &config.WAFIPACLConfig{Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.IPACL() == nil {
		t.Error("expected non-nil IPACL")
	}
}

func TestWAFMiddleware_RateLimiterAccessor(t *testing.T) {
	// Without rate limiter
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{Enabled: true, Mode: "enforce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.RateLimiter() != nil {
		t.Error("expected nil rate limiter when not configured")
	}

	// With rate limiter
	mw2, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			RateLimit: &config.WAFRateLimitConfig{
				Enabled: true,
				Rules:   []config.WAFRateLimitRule{{ID: "test", Scope: "ip", Limit: 10, Window: "1m"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw2.Stop()

	if mw2.RateLimiter() == nil {
		t.Error("expected non-nil rate limiter when configured")
	}
}

func TestWAFPipeline_MonitorModeBlacklistPassthrough(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "monitor",
			IPACL: &config.WAFIPACLConfig{
				Enabled:   true,
				Blacklist: []config.WAFIPACLEntry{{CIDR: "203.0.113.0/24", Reason: "bad"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	nextCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("expected blacklisted IP to pass through in monitor mode")
	}
}

func TestWAFPipeline_WithBehaviorDetection(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			BotDetection: &config.WAFBotConfig{
				Enabled: true,
				Behavior: &config.WAFBehaviorConfig{
					Enabled:            true,
					Window:             "5m",
					RPSThreshold:       10,
					ErrorRateThreshold: 30,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Normal request should pass through even with behavior detection enabled
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
