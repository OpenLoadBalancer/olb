package botdetection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBotDetection_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "Googlebot")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBotDetection_GoodBot_Allowed(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.AllowVerified = true

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for good bot, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Check bot detection header
	if rec.Header().Get("X-Bot-Detected") != "good" {
		t.Error("Expected X-Bot-Detected header to be 'good'")
	}
}

func TestBotDetection_BadBot_Blocked(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.BlockKnownBots = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for blocked bot")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "bot detected") {
		t.Error("Expected response body to contain 'bot detected'")
	}
}

func TestBotDetection_MissingUserAgent(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for missing UA")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	// No User-Agent
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestBotDetection_CustomRule(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.UserAgentRules = []UserAgentRule{
		{
			Pattern: "custom-bot",
			Action:  ActionBlock,
			Name:    "custom-rule",
		},
	}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "my-custom-bot/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestBotDetection_HeaderRule(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.CustomHeaders = []HeaderRule{
		{
			Header:  "X-Bot-Signature",
			Pattern: "bad-bot",
			Action:  ActionBlock,
			Name:    "header-detection",
		},
	}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Bot-Signature", "bad-bot-v1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestBotDetection_Challenge(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionChallenge
	config.ChallengePath = "/challenge"

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("Expected status %d, got %d", http.StatusFound, rec.Code)
	}

	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/challenge") || !strings.Contains(location, "return=") || !strings.Contains(location, "protected") {
		t.Errorf("Expected redirect to challenge with return param, got %s", location)
	}
}

func TestBotDetection_Throttle(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionThrottle

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Header().Get("X-Bot-Detected") != "bad" {
		t.Error("Expected X-Bot-Detected header")
	}
}

func TestBotDetection_LogOnly(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Header().Get("X-Bot-Detected") != "bad" {
		t.Error("Expected X-Bot-Detected header")
	}

	if rec.Header().Get("X-Bot-Name") == "" {
		t.Error("Expected X-Bot-Name header")
	}
}

func TestBotDetection_ExcludePaths(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.ExcludePaths = []string{"/public"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/public/data", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for excluded path, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBotDetection_RateThreshold(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog
	config.RequestRateThreshold = 3

	mw := New(config)

	// Simulate requests from same IP
	for i := 0; i < 5; i++ {
		callCount := int32(0)
		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func TestBotDetection_AllowVerifiedDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.AllowVerified = false
	config.BlockKnownBots = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called when verified bots are not allowed")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1)")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestBotDetection_BlockKnownDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.BlockKnownBots = false

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call when block known bots is disabled, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBotDetection_CaseInsensitive(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.BlockKnownBots = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	// Test case insensitivity for bad bot detection
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "ScRaPy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	// Test case insensitivity for good bot detection
	handler2 := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	config2 := DefaultConfig()
	config2.Enabled = true
	config2.AllowVerified = true
	mw2 := New(config2)
	handler2 = mw2.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("User-Agent", "GOOGLEBOT/2.1")
	rec2 := httptest.NewRecorder()
	handler2.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("Expected status %d for good bot, got %d", http.StatusOK, rec2.Code)
	}
}

func TestBotDetection_ExtractBotName(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1)")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Bot-Name") != "Googlebot" {
		t.Errorf("Expected bot name 'Googlebot', got '%s'", rec.Header().Get("X-Bot-Name"))
	}
}

func TestBotDetection_ChallengeNoPath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionChallenge
	config.ChallengePath = "" // No challenge path

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "scrapy/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d when no challenge path, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestBotDetection_SuspiciousScore(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog
	config.UserAgentRules = []UserAgentRule{
		{
			Pattern: "suspicious",
			Action:  ActionLog,
			Name:    "suspicious-rule",
		},
	}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "my-suspicious-client")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Header().Get("X-Bot-Detected") != "suspicious" {
		t.Errorf("Expected X-Bot-Detected header to be 'suspicious', got '%s'", rec.Header().Get("X-Bot-Detected"))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Action != ActionLog {
		t.Errorf("Default Action should be log, got %s", config.Action)
	}
	if config.BlockKnownBots != true {
		t.Error("Default BlockKnownBots should be true")
	}
	if config.AllowVerified != true {
		t.Error("Default AllowVerified should be true")
	}
	if config.RequestRateThreshold != 100 {
		t.Errorf("Default RequestRateThreshold should be 100, got %d", config.RequestRateThreshold)
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 95 {
		t.Errorf("Expected priority 95, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "botdetection" {
		t.Errorf("Expected name 'botdetection', got '%s'", mw.Name())
	}
}

func TestIPTracker_Count(t *testing.T) {
	tracker := newIPTracker(time.Minute)

	// Initial count should be 1 (first request adds itself)
	count := tracker.count("192.168.1.1")
	if count != 1 {
		t.Errorf("Expected count 1 after first request, got %d", count)
	}

	// Second request from same IP
	count = tracker.count("192.168.1.1")
	if count != 2 {
		t.Errorf("Expected count 2 after second request, got %d", count)
	}

	// Different IP
	count = tracker.count("192.168.1.2")
	if count != 1 {
		t.Errorf("Expected count 1 for different IP, got %d", count)
	}
}

func TestIPTracker_WindowCleanup(t *testing.T) {
	// Create tracker with very short window for testing
	tracker := newIPTracker(50 * time.Millisecond)

	// Add some requests
	tracker.count("192.168.1.1")
	tracker.count("192.168.1.1")

	// Wait for window to expire
	time.Sleep(100 * time.Millisecond)

	// Count should reset (old entries cleaned, current added)
	count := tracker.count("192.168.1.1")
	if count != 1 {
		t.Errorf("Expected count 1 after window cleanup, got %d", count)
	}
}

func TestBotDetection_MultipleRules(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.UserAgentRules = []UserAgentRule{
		{
			Pattern: "bot-1",
			Action:  ActionBlock,
			Name:    "rule-1",
		},
		{
			Pattern: "bot-2",
			Action:  ActionBlock,
			Name:    "rule-2",
		},
	}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	// Test first rule
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("User-Agent", "my-bot-1-client")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for rule 1, got %d", http.StatusForbidden, rec1.Code)
	}

	// Test second rule
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("User-Agent", "my-bot-2-client")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for rule 2, got %d", http.StatusForbidden, rec2.Code)
	}
}

func TestBotDetection_InvalidRegex(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.UserAgentRules = []UserAgentRule{
		{
			Pattern: "[invalid(", // Invalid regex
			Action:  ActionBlock,
			Name:    "invalid-rule",
		},
	}

	// Should not panic with invalid regex
	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "normal-browser")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBotDetection_JA3Fingerprints(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog
	config.JA3Fingerprints = []string{"abc123"}

	mw := New(config)

	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "normal-client")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBotDetection_AllKnownGoodBots(t *testing.T) {
	goodBots := []string{
		"Googlebot",
		"Bingbot",
		"Slurp",
		"DuckDuckBot",
		"Baiduspider",
		"YandexBot",
		"facebookexternalhit",
		"Twitterbot",
		"LinkedInBot",
	}

	for _, bot := range goodBots {
		config := DefaultConfig()
		config.Enabled = true
		config.AllowVerified = true
		config.Action = ActionBlock

		mw := New(config)

		callCount := int32(0)
		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("User-Agent", bot+"/1.0")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("Expected 1 call for %s, got %d", bot, callCount)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d for %s, got %d", http.StatusOK, bot, rec.Code)
		}
	}
}

func TestBotDetection_AllKnownBadBots(t *testing.T) {
	badBots := []string{
		"scrapy",
		"scraping",
		"curl",
		"wget",
		"python-requests",
		"java",
		"scrapinghub",
		"bot/",
		"spider/",
		"crawler/",
		"ahrefsbot",
		"semrushbot",
		"mj12bot",
		"dotbot",
	}

	for _, bot := range badBots {
		config := DefaultConfig()
		config.Enabled = true
		config.BlockKnownBots = true
		config.Action = ActionBlock

		mw := New(config)

		callCount := int32(0)
		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("User-Agent", bot+"/1.0")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status %d for %s, got %d", http.StatusForbidden, bot, rec.Code)
		}
	}
}

func TestGetBotInfo(t *testing.T) {
	// GetBotInfo is currently a placeholder
	ctx := context.Background()
	info := GetBotInfo(ctx)

	if info != nil {
		t.Error("GetBotInfo should return nil for empty context")
	}
}

func TestContextWithBotInfo(t *testing.T) {
	// Test that contextWithBotInfo works correctly
	ctx := context.Background()
	info := BotInfo{
		Detected: true,
		Type:     "bad",
		Name:     "TestBot",
	}

	result := contextWithBotInfo(ctx, info)
	retrievedInfo := GetBotInfo(result)

	if retrievedInfo == nil {
		t.Error("GetBotInfo should return non-nil after setting")
	}

	if retrievedInfo.Name != "TestBot" {
		t.Errorf("Expected bot name 'TestBot', got '%s'", retrievedInfo.Name)
	}
}
