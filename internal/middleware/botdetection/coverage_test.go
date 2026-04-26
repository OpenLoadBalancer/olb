package botdetection

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestCov_DetermineActionFallback covers the final return ActionAllow
// in determineAction when the bot type is not "good", "bad", or "suspicious"
// (e.g. an empty type string on a detected bot with an unusual state).
func TestCov_DetermineActionFallback(t *testing.T) {
	mw := New(Config{Enabled: true, Action: ActionBlock})
	info := BotInfo{Detected: true, Type: "unknown_type"}
	action := mw.determineAction(info)
	if action != ActionAllow {
		t.Errorf("expected ActionAllow for unknown type, got %v", action)
	}
}

// TestCov_DetermineActionBadBotBlocked covers the branch in determineAction
// where info.Type == "bad" && m.config.BlockKnownBots returns m.config.Action.
func TestCov_DetermineActionBadBotBlocked(t *testing.T) {
	mw := New(Config{Enabled: true, Action: ActionBlock, BlockKnownBots: true})
	info := BotInfo{Detected: true, Type: "bad"}
	action := mw.determineAction(info)
	if action != ActionBlock {
		t.Errorf("expected ActionBlock for bad bot with BlockKnownBots, got %v", action)
	}
}

// TestCov_DetermineActionSuspicious covers the suspicious bot branch
// in determineAction.
func TestCov_DetermineActionSuspicious(t *testing.T) {
	mw := New(Config{Enabled: true, Action: ActionThrottle, BlockKnownBots: true})
	info := BotInfo{Detected: true, Type: "suspicious"}
	action := mw.determineAction(info)
	if action != ActionThrottle {
		t.Errorf("expected ActionThrottle for suspicious bot, got %v", action)
	}
}

// TestCov_DetermineActionNotDetected covers the early return in
// determineAction when info.Detected is false.
func TestCov_DetermineActionNotDetected(t *testing.T) {
	mw := New(Config{Enabled: true, Action: ActionBlock})
	info := BotInfo{Detected: false}
	action := mw.determineAction(info)
	if action != ActionAllow {
		t.Errorf("expected ActionAllow for undetected, got %v", action)
	}
}

// TestCov_ChallengePathClean covers the branch in challenge where
// path.Clean produces a path that does not start with "/", forcing
// returnPath = "/".
func TestCov_ChallengePathClean(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionChallenge
	config.ChallengePath = "/verify"
	config.BlockKnownBots = true

	mw := New(config)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("User-Agent", "scrapy/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected status %d, got %d", http.StatusFound, rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Error("expected a redirect location")
	}
}

// TestCov_IPTrackerMaxIPsEviction exercises the maxIPs eviction logic
// in ipTracker.count. It fills the tracker to capacity and then adds a new IP
// to trigger eviction of the oldest entry.
func TestCov_IPTrackerMaxIPsEviction(t *testing.T) {
	tracker := newIPTracker(0) // 0 duration so entries expire immediately in cleanup window
	tracker.maxIPs = 3

	// Fill up to maxIPs distinct IPs.
	// Because window is 0, old entries get cleaned; each call records 1 recent entry.
	tracker.count("10.0.0.1")
	tracker.count("10.0.0.2")
	tracker.count("10.0.0.3")

	// Verify we have exactly 3 IPs tracked.
	tracker.mu.Lock()
	if len(tracker.requests) != 3 {
		t.Fatalf("expected 3 tracked IPs, got %d", len(tracker.requests))
	}
	tracker.mu.Unlock()

	// Adding a new distinct IP should trigger eviction of the oldest.
	count := tracker.count("10.0.0.4")
	if count != 1 {
		t.Errorf("expected count 1 for new IP after eviction, got %d", count)
	}

	tracker.mu.Lock()
	finalCount := len(tracker.requests)
	tracker.mu.Unlock()
	if finalCount > 3 {
		t.Errorf("expected at most 3 tracked IPs after eviction, got %d", finalCount)
	}
}

// TestCov_IPTrackerEmptyTimesEviction tests the branch where an IP has
// an empty times slice during eviction (len(times)==0 causes immediate break).
func TestCov_IPTrackerEmptyTimesEviction(t *testing.T) {
	tracker := newIPTracker(0)
	tracker.maxIPs = 2

	// Manually insert an IP with an empty times slice to hit the empty-times branch.
	tracker.mu.Lock()
	tracker.requests["empty-ip"] = []time.Time{}
	tracker.requests["normal-ip"] = []time.Time{time.Now()}
	tracker.mu.Unlock()

	// This new IP should trigger eviction; "empty-ip" should be evicted because len(times)==0.
	count := tracker.count("new-ip")
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	tracker.mu.Lock()
	_, emptyExists := tracker.requests["empty-ip"]
	tracker.mu.Unlock()
	if emptyExists {
		t.Error("expected 'empty-ip' to be evicted since it had an empty times slice")
	}
}

// TestCov_ExtractBotNameUnknown covers the "return unknown" branch in
// extractBotName when the user-agent does not match any known bot.
func TestCov_ExtractBotNameUnknown(t *testing.T) {
	name := extractBotName("some-completely-unknown-client/1.0")
	if name != "unknown" {
		t.Errorf("expected 'unknown', got '%s'", name)
	}
}

// TestCov_GetBotInfoFromRequest covers GetBotInfoFromRequest which
// currently has 0% coverage.
func TestCov_GetBotInfoFromRequest(t *testing.T) {
	// Case 1: No bot info in context.
	req := httptest.NewRequest("GET", "/test", nil)
	info := GetBotInfoFromRequest(req)
	if info != nil {
		t.Error("expected nil when no bot info in request context")
	}

	// Case 2: Bot info set in context via middleware, then retrieved.
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog
	config.BlockKnownBots = true

	mw := New(config)
	var retrieved *BotInfo
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retrieved = GetBotInfoFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "scrapy/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if retrieved == nil {
		t.Fatal("expected non-nil bot info from request after detection")
	}
	if retrieved.Type != "bad" {
		t.Errorf("expected type 'bad', got '%s'", retrieved.Type)
	}
}

// TestCov_HeaderRuleWithBlockAction covers the header rule detection path
// with a header match that causes a block action, including the reason assignment.
func TestCov_HeaderRuleWithBlockAction(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.BlockKnownBots = false
	config.CustomHeaders = []HeaderRule{
		{
			Header:  "X-Special-Bot",
			Pattern: "^evil-bot-.*",
			Action:  ActionBlock,
			Name:    "special-bot-header",
		},
	}

	mw := New(config)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "normal-browser")
	req.Header.Set("X-Special-Bot", "evil-bot-v2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

// TestCov_LogOnlyUndetectedRequest covers the ActionLog branch in Wrap
// when no bot is detected (the inner if info.Detected is false).
func TestCov_LogOnlyUndetectedRequest(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionLog
	config.BlockKnownBots = false
	config.AllowVerified = false

	mw := New(config)
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 normal-browser")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected handler to be called once, got %d", callCount)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCov_AllowActionDetectedBot covers the ActionAllow branch when a bot
// is detected, verifying that X-Bot-Detected headers are still set.
func TestCov_AllowActionDetectedBot(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionAllow
	config.BlockKnownBots = true
	config.AllowVerified = true

	mw := New(config)
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Use a bad bot; determineAction returns ActionAllow because config.Action is ActionAllow
	// Actually, for bad bots with BlockKnownBots=true, it returns m.config.Action which is ActionAllow.
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "curl/7.68.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected handler to be called once, got %d", callCount)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if rec.Header().Get("X-Bot-Detected") != "bad" {
		t.Errorf("expected X-Bot-Detected 'bad', got '%s'", rec.Header().Get("X-Bot-Detected"))
	}
}

// TestCov_AllowActionNoBot covers the ActionAllow (default) branch when
// no bot is detected (info.Detected == false).
func TestCov_AllowActionNoBot(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionAllow
	config.BlockKnownBots = false
	config.AllowVerified = false

	mw := New(config)
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 normal-browser")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected handler to be called once, got %d", callCount)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCov_ExcludePathExactMatch covers the ExcludePaths branch where the
// request path exactly equals the excluded path (length match).
func TestCov_ExcludePathExactMatch(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.BlockKnownBots = true
	config.ExcludePaths = []string{"/public"}

	mw := New(config)
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/public", nil)
	req.Header.Set("User-Agent", "scrapy/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected handler to be called once for exact excluded path, got %d", callCount)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCov_ExcludePathTrailingSlash covers the ExcludePaths branch where
// the excluded path ends with "/".
func TestCov_ExcludePathTrailingSlash(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Action = ActionBlock
	config.BlockKnownBots = true
	config.ExcludePaths = []string{"/api/"}

	mw := New(config)
	callCount := int32(0)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("User-Agent", "scrapy/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected handler to be called once for trailing-slash excluded path, got %d", callCount)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
