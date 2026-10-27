package waf

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func TestSetRequestContext(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	ctx := detection.NewRequestContext(req)
	defer detection.ReleaseRequestContext(ctx)

	req2 := SetRequestContext(req, ctx)
	got := GetRequestContext(req2)
	if got == nil {
		t.Fatal("expected non-nil RequestContext")
	}
	if got != ctx {
		t.Error("expected same context back")
	}
}

func TestNewEvent(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.RemoteAddr = "10.0.0.1:5000"
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("User-Agent", "TestBot/1.0")

	ctx := detection.NewRequestContext(req)
	defer detection.ReleaseRequestContext(ctx)

	evt := NewEvent(ctx, "detection", "block")
	if evt.Layer != "detection" {
		t.Errorf("expected layer 'detection', got %q", evt.Layer)
	}
	if evt.Action != "block" {
		t.Errorf("expected action 'block', got %q", evt.Action)
	}
	if evt.RemoteIP != "10.0.0.1" {
		t.Errorf("expected remote IP '10.0.0.1', got %q", evt.RemoteIP)
	}
	if evt.Method != "GET" {
		t.Errorf("expected method 'GET', got %q", evt.Method)
	}
	if evt.Path != "/test" {
		t.Errorf("expected path '/test', got %q", evt.Path)
	}
	if evt.UserAgent != "TestBot/1.0" {
		t.Errorf("expected user agent 'TestBot/1.0', got %q", evt.UserAgent)
	}
	if evt.RequestID != "req-123" {
		t.Errorf("expected request ID 'req-123', got %q", evt.RequestID)
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestNewEvent_NilContext(t *testing.T) {
	evt := NewEvent(nil, "ip_acl", "allow")
	if evt.Layer != "ip_acl" {
		t.Errorf("expected layer 'ip_acl', got %q", evt.Layer)
	}
	if evt.Action != "allow" {
		t.Errorf("expected action 'allow', got %q", evt.Action)
	}
	if evt.RemoteIP != "" {
		t.Errorf("expected empty remote IP for nil context, got %q", evt.RemoteIP)
	}
}

func TestEventLogger_LogEvent_AllowFiltering(t *testing.T) {
	var buf bytes.Buffer
	analytics := NewAnalytics()

	logger := NewEventLogger(EventLoggerConfig{
		Writer:     &buf,
		NodeID:     "test-node",
		LogAllowed: false,
		LogBlocked: true,
		Analytics:  analytics,
	})

	// Allowed event should not be written (but still recorded in analytics)
	allowEvt := &WAFEvent{
		Action:   "allow",
		RemoteIP: "1.1.1.1",
	}
	logger.LogEvent(allowEvt)

	if buf.Len() > 0 {
		t.Error("expected no output for allowed event when LogAllowed=false")
	}

	// Blocked event should be written
	blockEvt := &WAFEvent{
		Action:   "block",
		RemoteIP: "2.2.2.2",
	}
	logger.LogEvent(blockEvt)

	if buf.Len() == 0 {
		t.Error("expected output for blocked event when LogBlocked=true")
	}
}

func TestEventLogger_LogEvent_NilEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(EventLoggerConfig{Writer: &buf})
	// Should not panic
	logger.LogEvent(nil)
}

func TestEventLogger_LogEvent_BypassFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(EventLoggerConfig{
		Writer:     &buf,
		LogAllowed: false,
		LogBlocked: true,
	})

	// Bypass action follows the "allow" path in filtering
	evt := &WAFEvent{Action: "bypass"}
	logger.LogEvent(evt)

	if buf.Len() > 0 {
		t.Error("expected no output for bypass event when LogAllowed=false")
	}
}

func TestEventLogger_LogEvent_ChallengeFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(EventLoggerConfig{
		Writer:     &buf,
		LogAllowed: true,
		LogBlocked: false,
	})

	// Challenge action follows the "block" path in filtering
	evt := &WAFEvent{Action: "challenge"}
	logger.LogEvent(evt)

	if buf.Len() > 0 {
		t.Error("expected no output for challenge event when LogBlocked=false")
	}
}

func TestEventLogger_LogEvent_SetsNodeID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(EventLoggerConfig{
		Writer:     &buf,
		NodeID:     "node-42",
		LogBlocked: true,
	})

	evt := &WAFEvent{Action: "block"}
	logger.LogEvent(evt)

	if evt.NodeID != "node-42" {
		t.Errorf("expected node ID 'node-42', got %q", evt.NodeID)
	}
}

func TestEventLogger_LogEvent_SetsTimestamp(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(EventLoggerConfig{
		Writer:     &buf,
		LogBlocked: true,
	})

	evt := &WAFEvent{Action: "block"}
	logger.LogEvent(evt)

	if evt.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp to be set")
	}
}

func TestTopKTracker_EvictLowest(t *testing.T) {
	tracker := newTopKTracker(3) // max 3 items

	// Add 3 IPs
	tracker.Increment("ip1")
	tracker.Increment("ip1")
	tracker.Increment("ip1") // count 3

	tracker.Increment("ip2")
	tracker.Increment("ip2") // count 2

	tracker.Increment("ip3") // count 1

	// Adding ip4 should evict the lowest (ip3 with count 1)
	tracker.Increment("ip4")

	top := tracker.TopN(10)
	if len(top) > 3 {
		t.Errorf("expected max 3 items after eviction, got %d", len(top))
	}

	// ip3 should have been evicted (lowest count)
	for _, item := range top {
		if item.IP == "ip3" {
			t.Error("expected ip3 to be evicted as lowest count")
		}
	}
}

func TestBuildSanitizerConfig(t *testing.T) {
	cfg := &config.WAFSanitizerConfig{
		Enabled:           true,
		MaxHeaderSize:     16384,
		MaxHeaderCount:    200,
		MaxBodySize:       20 * 1024 * 1024,
		MaxURLLength:      16384,
		MaxCookieSize:     8192,
		MaxCookieCount:    100,
		BlockNullBytes:    boolPtr(true),
		NormalizeEncoding: boolPtr(true),
		StripHopByHop:     boolPtr(true),
		AllowedMethods:    []string{"GET", "POST", "PUT"},
		PathOverrides: []config.WAFSanitizerOverride{
			{Path: "/upload/*", MaxBodySize: 50 * 1024 * 1024},
		},
	}

	sc := buildSanitizerConfig(cfg)

	if sc.MaxHeaderSize != 16384 {
		t.Errorf("expected MaxHeaderSize 16384, got %d", sc.MaxHeaderSize)
	}
	if sc.MaxHeaderCount != 200 {
		t.Errorf("expected MaxHeaderCount 200, got %d", sc.MaxHeaderCount)
	}
	if sc.MaxBodySize != 20*1024*1024 {
		t.Errorf("expected MaxBodySize 20MB, got %d", sc.MaxBodySize)
	}
	if sc.MaxURLLength != 16384 {
		t.Errorf("expected MaxURLLength 16384, got %d", sc.MaxURLLength)
	}
	if sc.MaxCookieSize != 8192 {
		t.Errorf("expected MaxCookieSize 8192, got %d", sc.MaxCookieSize)
	}
	if sc.MaxCookieCount != 100 {
		t.Errorf("expected MaxCookieCount 100, got %d", sc.MaxCookieCount)
	}
	if !sc.BlockNullBytes {
		t.Error("expected BlockNullBytes true")
	}
	if !sc.NormalizeEncoding {
		t.Error("expected NormalizeEncoding true")
	}
	if !sc.StripHopByHop {
		t.Error("expected StripHopByHop true")
	}
	if len(sc.AllowedMethods) != 3 {
		t.Errorf("expected 3 allowed methods, got %d", len(sc.AllowedMethods))
	}
	if len(sc.PathOverrides) != 1 {
		t.Errorf("expected 1 path override, got %d", len(sc.PathOverrides))
	}
}

func TestBuildIPACLConfig_WithExpires(t *testing.T) {
	expiry := time.Now().Add(time.Hour).Format(time.RFC3339)
	cfg := &config.WAFIPACLConfig{
		Enabled: true,
		Whitelist: []config.WAFIPACLEntry{
			{CIDR: "10.0.0.0/8", Reason: "internal", Expires: expiry},
		},
		Blacklist: []config.WAFIPACLEntry{
			{CIDR: "1.2.3.0/24", Reason: "bad", Expires: expiry},
		},
		AutoBan: &config.WAFAutoBanConfig{
			Enabled:    true,
			DefaultTTL: "30m",
			MaxTTL:     "2h",
		},
	}

	aclCfg := buildIPACLConfig(cfg)

	if len(aclCfg.Whitelist) != 1 {
		t.Errorf("expected 1 whitelist entry, got %d", len(aclCfg.Whitelist))
	}
	if aclCfg.Whitelist[0].Expires.IsZero() {
		t.Error("expected non-zero whitelist expiry")
	}
	if len(aclCfg.Blacklist) != 1 {
		t.Errorf("expected 1 blacklist entry, got %d", len(aclCfg.Blacklist))
	}
	if aclCfg.Blacklist[0].Expires.IsZero() {
		t.Error("expected non-zero blacklist expiry")
	}
	if !aclCfg.AutoBan.Enabled {
		t.Error("expected auto-ban enabled")
	}
	if aclCfg.AutoBan.DefaultTTL != 30*time.Minute {
		t.Errorf("expected default TTL 30m, got %v", aclCfg.AutoBan.DefaultTTL)
	}
	if aclCfg.AutoBan.MaxTTL != 2*time.Hour {
		t.Errorf("expected max TTL 2h, got %v", aclCfg.AutoBan.MaxTTL)
	}
}

func TestWAFPipeline_WithCustomSanitizerConfig(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			Sanitizer: &config.WAFSanitizerConfig{
				Enabled:        true,
				MaxHeaderSize:  4096,
				MaxHeaderCount: 50,
				MaxBodySize:    1024 * 1024,
				MaxURLLength:   4096,
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

	req := httptest.NewRequest("GET", "http://example.com/api", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWAFPipeline_WithDetectionExclusions(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			Detection: &config.WAFDetectionConfig{
				Enabled: true,
				Mode:    "enforce",
				Threshold: config.WAFDetectionThreshold{
					Block: 50,
					Log:   25,
				},
				Detectors: config.WAFDetectorsConfig{
					SQLi:          config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
					XSS:           config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
					PathTraversal: config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
					CMDi:          config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
					XXE:           config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
					SSRF:          config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	// Verify the detection engine is wired
	if mw.detection == nil {
		t.Error("expected detection engine to be configured")
	}
}

func TestWAFPipeline_WithTLSFingerprint(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			BotDetection: &config.WAFBotConfig{
				Enabled: true,
				TLSFingerprint: &config.WAFTLSFPConfig{
					Enabled: true,
				},
				UserAgent: &config.WAFUserAgentConfig{
					Enabled: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.botDetect == nil {
		t.Error("expected bot detector to be configured")
	}
}

func TestWAFPipeline_NilWAFConfig(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.config == nil {
		t.Error("expected non-nil config after nil input")
	}
}

func TestWAFPipeline_WithLoggingConfig(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			Logging: &config.WAFLoggingConfig{
				LogAllowed: true,
				LogBlocked: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if !mw.events.logAllowed {
		t.Error("expected logAllowed to be true")
	}
	if !mw.events.logBlocked {
		t.Error("expected logBlocked to be true")
	}
}

func TestWAFPipeline_WithResponseConfig(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			Response: &config.WAFResponseConfig{
				SecurityHeaders: &config.WAFSecurityHeadersConfig{
					Enabled:               true,
					XContentTypeOptions:   true,
					XFrameOptions:         "DENY",
					ReferrerPolicy:        "no-referrer",
					ContentSecurityPolicy: "default-src 'self'",
					HSTS: &config.WAFHSTSConfig{
						Enabled:           true,
						MaxAge:            31536000,
						IncludeSubdomains: true,
						Preload:           true,
					},
				},
				DataMasking: &config.WAFDataMaskingConfig{
					Enabled:          true,
					MaskCreditCards:  true,
					MaskSSN:          true,
					MaskEmails:       true,
					MaskAPIKeys:      true,
					StripStackTraces: true,
				},
				ErrorPages: &config.WAFErrorPagesConfig{
					Enabled: true,
					Mode:    "production",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	if mw.response == nil {
		t.Error("expected response protection to be configured")
	}
}

func TestWAFMiddleware_Stop_AllLayers(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL:   &config.WAFIPACLConfig{Enabled: true},
			RateLimit: &config.WAFRateLimitConfig{
				Enabled: true,
				Rules:   []config.WAFRateLimitRule{{ID: "test", Scope: "ip", Limit: 10, Window: "1m"}},
			},
			BotDetection: &config.WAFBotConfig{
				Enabled: true,
				Behavior: &config.WAFBehaviorConfig{
					Enabled: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic
	mw.Stop()
}

func TestWAFPipeline_RateLimitWithAutoBan(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
		Config: &config.WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			IPACL: &config.WAFIPACLConfig{
				Enabled: true,
				AutoBan: &config.WAFAutoBanConfig{
					Enabled:    true,
					DefaultTTL: "1m",
					MaxTTL:     "1h",
				},
			},
			RateLimit: &config.WAFRateLimitConfig{
				Enabled: true,
				Rules: []config.WAFRateLimitRule{
					{ID: "test", Scope: "ip", Limit: 2, Window: "1m", AutoBanAfter: 3},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Stop()

	// The auto-ban callback should be wired
	if mw.ipACL == nil {
		t.Error("expected IPACL to be configured for auto-ban wiring")
	}
	if mw.rateLimiter == nil {
		t.Error("expected rate limiter to be configured")
	}
}

func TestAnalytics_TopKTracker_TopN_Sorting(t *testing.T) {
	tracker := newTopKTracker(100)

	// Add IPs with different counts
	for i := 0; i < 5; i++ {
		tracker.Increment("ip-low")
	}
	for i := 0; i < 50; i++ {
		tracker.Increment("ip-high")
	}
	for i := 0; i < 20; i++ {
		tracker.Increment("ip-mid")
	}

	top := tracker.TopN(3)
	if len(top) != 3 {
		t.Fatalf("expected 3 items, got %d", len(top))
	}

	// Should be sorted descending
	if top[0].IP != "ip-high" {
		t.Errorf("expected top IP to be ip-high, got %s", top[0].IP)
	}
	if top[1].IP != "ip-mid" {
		t.Errorf("expected 2nd IP to be ip-mid, got %s", top[1].IP)
	}
	if top[2].IP != "ip-low" {
		t.Errorf("expected 3rd IP to be ip-low, got %s", top[2].IP)
	}
}

func TestAnalytics_TopN_LargerThanAvailable(t *testing.T) {
	tracker := newTopKTracker(100)
	tracker.Increment("only-ip")

	top := tracker.TopN(10)
	if len(top) != 1 {
		t.Errorf("expected 1 item, got %d", len(top))
	}
}

func TestAnalytics_GetTimeline_LargeMinutes(t *testing.T) {
	a := NewAnalytics()
	timeline := a.GetTimeline(2000) // more than 1440
	if len(timeline) != 1440 {
		t.Errorf("expected capped at 1440, got %d", len(timeline))
	}
}
