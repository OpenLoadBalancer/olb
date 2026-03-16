package waf

import (
	"net/http"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/waf/botdetect"
	"github.com/openloadbalancer/olb/internal/waf/detection"
	"github.com/openloadbalancer/olb/internal/waf/detection/cmdi"
	"github.com/openloadbalancer/olb/internal/waf/detection/pathtraversal"
	"github.com/openloadbalancer/olb/internal/waf/detection/sqli"
	"github.com/openloadbalancer/olb/internal/waf/detection/ssrf"
	"github.com/openloadbalancer/olb/internal/waf/detection/xss"
	"github.com/openloadbalancer/olb/internal/waf/detection/xxe"
	"github.com/openloadbalancer/olb/internal/waf/ipacl"
	"github.com/openloadbalancer/olb/internal/waf/ratelimit"
	"github.com/openloadbalancer/olb/internal/waf/response"
	"github.com/openloadbalancer/olb/internal/waf/sanitizer"
)

// WAFMiddleware is the main WAF entry point that chains all 6 security layers.
// It implements the middleware.Middleware interface.
type WAFMiddleware struct {
	config      *config.WAFConfig
	ipACL       *ipacl.IPAccessList
	rateLimiter *ratelimit.RateLimiter
	sanitizer   *sanitizer.Sanitizer
	legacyWAF   *WAF // existing regex-based WAF
	detection   *detection.Engine
	botDetect   *botdetect.BotDetector
	response    *response.Protection
	events      *EventLogger
	analytics   *Analytics
	metrics     *WAFMetrics
	mode        string // "enforce", "monitor", "disabled"
}

// WAFMiddlewareConfig holds all dependencies for creating a WAFMiddleware.
type WAFMiddlewareConfig struct {
	Config          *config.WAFConfig
	NodeID          string
	MetricsRegistry *metrics.Registry
}

// NewWAFMiddleware creates a new WAFMiddleware with all layers.
func NewWAFMiddleware(cfg WAFMiddlewareConfig) (*WAFMiddleware, error) {
	wafCfg := cfg.Config
	if wafCfg == nil {
		wafCfg = &config.WAFConfig{Enabled: true, Mode: "enforce"}
	}

	analytics := NewAnalytics()

	wafMetrics := RegisterWAFMetrics(cfg.MetricsRegistry)

	events := NewEventLogger(EventLoggerConfig{
		NodeID:     cfg.NodeID,
		LogBlocked: true,
		LogAllowed: false,
		Analytics:  analytics,
		Metrics:    wafMetrics,
	})
	if wafCfg.Logging != nil {
		events.logAllowed = wafCfg.Logging.LogAllowed
		events.logBlocked = wafCfg.Logging.LogBlocked
	}

	mw := &WAFMiddleware{
		config:    wafCfg,
		events:    events,
		analytics: analytics,
		metrics:   wafMetrics,
		mode:      normalizeMode(wafCfg.Mode),
	}

	// Layer 1: IP ACL
	if wafCfg.IPACL != nil && wafCfg.IPACL.Enabled {
		aclCfg := buildIPACLConfig(wafCfg.IPACL)
		acl, err := ipacl.New(aclCfg)
		if err != nil {
			return nil, err
		}
		mw.ipACL = acl
	}

	// Layer 2: Rate Limiter
	if wafCfg.RateLimit != nil && wafCfg.RateLimit.Enabled {
		rlCfg := ratelimit.Config{}
		for _, r := range wafCfg.RateLimit.Rules {
			window, _ := time.ParseDuration(r.Window)
			if window == 0 {
				window = time.Minute
			}
			rlCfg.Rules = append(rlCfg.Rules, ratelimit.Rule{
				ID:           r.ID,
				Scope:        r.Scope,
				Paths:        r.Paths,
				Limit:        r.Limit,
				Window:       window,
				Burst:        r.Burst,
				Action:       r.Action,
				AutoBanAfter: r.AutoBanAfter,
			})
		}
		rl := ratelimit.New(rlCfg)
		// Wire auto-ban to IP ACL if both are enabled
		if mw.ipACL != nil {
			rl.OnAutoBan = func(ip string, reason string) {
				mw.ipACL.Ban(ip, time.Hour, reason)
			}
		}
		mw.rateLimiter = rl
	}

	// Layer 3: Sanitizer
	if wafCfg.Sanitizer != nil && wafCfg.Sanitizer.Enabled {
		mw.sanitizer = sanitizer.New(buildSanitizerConfig(wafCfg.Sanitizer))
	} else {
		mw.sanitizer = sanitizer.New(sanitizer.DefaultConfig())
	}

	// Layer 4: Legacy regex WAF (existing detection)
	legacyCfg := DefaultConfig()
	// Map "enforce" → "blocking" for legacy WAF compatibility
	switch wafCfg.Mode {
	case "enforce", "blocking", "block":
		legacyCfg.Mode = "blocking"
	case "monitor":
		legacyCfg.Mode = "detection"
	default:
		legacyCfg.Mode = "blocking"
	}
	legacyWAF, err := New(legacyCfg)
	if err != nil {
		return nil, err
	}
	mw.legacyWAF = legacyWAF

	// Layer 4: New detection engine with specialized detectors
	detCfg := detection.Config{
		Threshold: detection.DefaultThreshold(),
	}
	if wafCfg.Detection != nil {
		if wafCfg.Detection.Threshold.Block > 0 {
			detCfg.Threshold.Block = wafCfg.Detection.Threshold.Block
		}
		if wafCfg.Detection.Threshold.Log > 0 {
			detCfg.Threshold.Log = wafCfg.Detection.Threshold.Log
		}
		detCfg.Multipliers = map[string]float64{
			"sqli":           wafCfg.Detection.Detectors.SQLi.ScoreMultiplier,
			"xss":            wafCfg.Detection.Detectors.XSS.ScoreMultiplier,
			"path_traversal": wafCfg.Detection.Detectors.PathTraversal.ScoreMultiplier,
			"cmdi":           wafCfg.Detection.Detectors.CMDi.ScoreMultiplier,
			"xxe":            wafCfg.Detection.Detectors.XXE.ScoreMultiplier,
			"ssrf":           wafCfg.Detection.Detectors.SSRF.ScoreMultiplier,
		}
		for _, ex := range wafCfg.Detection.Exclusions {
			detCfg.Exclusions = append(detCfg.Exclusions, detection.Exclusion{
				PathPattern: ex.Path,
				Detectors:   ex.Detectors,
			})
		}
	}
	engine := detection.NewEngine(detCfg)

	// Register all detectors (check enabled flags if detection config exists)
	dc := wafCfg.Detection
	if dc == nil || dc.Detectors.SQLi.Enabled {
		engine.Register(sqli.New())
	}
	if dc == nil || dc.Detectors.XSS.Enabled {
		engine.Register(xss.New())
	}
	if dc == nil || dc.Detectors.PathTraversal.Enabled {
		engine.Register(pathtraversal.New())
	}
	if dc == nil || dc.Detectors.CMDi.Enabled {
		engine.Register(cmdi.New())
	}
	if dc == nil || dc.Detectors.XXE.Enabled {
		engine.Register(xxe.New())
	}
	if dc == nil || dc.Detectors.SSRF.Enabled {
		engine.Register(ssrf.New())
	}
	mw.detection = engine

	// Layer 5: Bot Detection
	if wafCfg.BotDetection != nil && wafCfg.BotDetection.Enabled {
		bdCfg := botdetect.Config{
			BehaviorEnabled: true,
		}
		if wafCfg.BotDetection.UserAgent != nil {
			bdCfg.UAEnabled = wafCfg.BotDetection.UserAgent.Enabled
			bdCfg.UABlockEmpty = wafCfg.BotDetection.UserAgent.BlockEmpty
			bdCfg.UABlockKnownScanners = wafCfg.BotDetection.UserAgent.BlockKnownScanners
		}
		if wafCfg.BotDetection.TLSFingerprint != nil {
			bdCfg.TLSFingerprintEnabled = wafCfg.BotDetection.TLSFingerprint.Enabled
		}
		if wafCfg.BotDetection.Behavior != nil {
			bdCfg.BehaviorEnabled = wafCfg.BotDetection.Behavior.Enabled
			if wafCfg.BotDetection.Behavior.Window != "" {
				if w, err := time.ParseDuration(wafCfg.BotDetection.Behavior.Window); err == nil {
					bdCfg.BehaviorConfig.Window = w
				}
			}
			if wafCfg.BotDetection.Behavior.RPSThreshold > 0 {
				bdCfg.BehaviorConfig.RPSThreshold = float64(wafCfg.BotDetection.Behavior.RPSThreshold)
			}
		}
		mw.botDetect = botdetect.New(bdCfg)
	}

	// Layer 6: Response Protection
	if wafCfg.Response != nil {
		rp := response.DefaultProtection()
		if wafCfg.Response.SecurityHeaders != nil && wafCfg.Response.SecurityHeaders.Enabled {
			sh := wafCfg.Response.SecurityHeaders
			if sh.HSTS != nil {
				rp.Headers.HSTSEnabled = sh.HSTS.Enabled
				if sh.HSTS.MaxAge > 0 {
					rp.Headers.HSTSMaxAge = sh.HSTS.MaxAge
				}
				rp.Headers.HSTSIncludeSubdomains = sh.HSTS.IncludeSubdomains
				rp.Headers.HSTSPreload = sh.HSTS.Preload
			}
			rp.Headers.XContentTypeOptions = sh.XContentTypeOptions
			if sh.XFrameOptions != "" {
				rp.Headers.XFrameOptions = sh.XFrameOptions
			}
			if sh.ReferrerPolicy != "" {
				rp.Headers.ReferrerPolicy = sh.ReferrerPolicy
			}
			if sh.ContentSecurityPolicy != "" {
				rp.Headers.CSP = sh.ContentSecurityPolicy
			}
		}
		if wafCfg.Response.DataMasking != nil && wafCfg.Response.DataMasking.Enabled {
			dm := wafCfg.Response.DataMasking
			rp.Masking.MaskCreditCards = dm.MaskCreditCards
			rp.Masking.MaskSSN = dm.MaskSSN
			rp.Masking.MaskEmails = dm.MaskEmails
			rp.Masking.MaskAPIKeys = dm.MaskAPIKeys
			rp.Masking.StripStackTraces = dm.StripStackTraces
		}
		if wafCfg.Response.ErrorPages != nil {
			rp.ErrorPages.Enabled = wafCfg.Response.ErrorPages.Enabled
			rp.ErrorPages.Mode = wafCfg.Response.ErrorPages.Mode
		}
		mw.response = rp
	}

	return mw, nil
}

// Name returns the middleware name.
func (mw *WAFMiddleware) Name() string { return "waf" }

// Priority returns the middleware priority.
func (mw *WAFMiddleware) Priority() int { return 100 }

// Wrap wraps the next handler with the WAF middleware.
func (mw *WAFMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mw.mode == "disabled" || !mw.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		// Panic recovery — fail-open
		defer func() {
			if err := recover(); err != nil {
				next.ServeHTTP(w, r)
			}
		}()

		remoteIP := extractIP(r.RemoteAddr)

		// Layer 1: IP ACL
		if mw.ipACL != nil {
			switch mw.ipACL.Check(remoteIP) {
			case ipacl.ActionBypass:
				evt := &WAFEvent{
					Timestamp: time.Now(),
					RemoteIP:  remoteIP,
					Method:    r.Method,
					Path:      r.URL.Path,
					UserAgent: r.UserAgent(),
					Layer:     "ip_acl",
					Action:    "bypass",
				}
				mw.events.LogEvent(evt)
				next.ServeHTTP(w, r)
				return
			case ipacl.ActionBlock:
				evt := &WAFEvent{
					Timestamp: time.Now(),
					RemoteIP:  remoteIP,
					Method:    r.Method,
					Path:      r.URL.Path,
					UserAgent: r.UserAgent(),
					Layer:     "ip_acl",
					Action:    "block",
				}
				mw.events.LogEvent(evt)
				if mw.mode == "enforce" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"error":"blocked by WAF","layer":"ip_acl"}`))
					return
				}
			}
		}

		// Layer 2: Rate Limiter
		if mw.rateLimiter != nil {
			allowed, retryAfter := mw.rateLimiter.Allow(r)
			if !allowed {
				evt := &WAFEvent{
					Timestamp: time.Now(),
					RemoteIP:  remoteIP,
					Method:    r.Method,
					Path:      r.URL.Path,
					UserAgent: r.UserAgent(),
					Layer:     "rate_limit",
					Action:    "block",
				}
				mw.events.LogEvent(evt)
				if mw.mode == "enforce" {
					ratelimit.WriteRateLimitResponse(w, retryAfter)
					return
				}
			}
		}

		// Layer 3: Request Sanitizer
		sanResult, valErr := mw.sanitizer.Process(r)
		if valErr != nil {
			evt := &WAFEvent{
				Timestamp: time.Now(),
				RemoteIP:  remoteIP,
				Method:    r.Method,
				Path:      r.URL.Path,
				UserAgent: r.UserAgent(),
				Layer:     "sanitizer",
				Action:    "block",
				Message:   valErr.Message,
			}
			mw.events.LogEvent(evt)
			if mw.mode == "enforce" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(valErr.Status)
				w.Write([]byte(`{"error":"` + valErr.Message + `","layer":"sanitizer"}`))
				return
			}
		}

		// Build RequestContext from sanitizer result (pooled)
		ctx := NewRequestContext(r)
		defer ReleaseRequestContext(ctx)
		if sanResult != nil {
			if sanResult.DecodedPath != "" {
				ctx.DecodedPath = sanResult.DecodedPath
			}
			if sanResult.DecodedQuery != "" {
				ctx.DecodedQuery = sanResult.DecodedQuery
			}
			if sanResult.DecodedBody != "" {
				ctx.DecodedBody = sanResult.DecodedBody
			}
		}
		r = ctx.SetOnRequest(r)

		// Layer 4: WAF Detection (legacy regex engine)
		if mw.legacyWAF != nil {
			result, err := mw.legacyWAF.Process(r)
			if err == nil && result != nil && result.IsBlocked() {
				evt := &WAFEvent{
					Timestamp:  time.Now(),
					RemoteIP:   remoteIP,
					Method:     r.Method,
					Path:       r.URL.Path,
					UserAgent:  r.UserAgent(),
					Layer:      "detection",
					Action:     "block",
					TotalScore: result.Score,
					LatencyNS:  time.Since(start).Nanoseconds(),
				}
				for _, m := range result.Matches {
					evt.Findings = append(evt.Findings, Finding{
						Detector: m.RuleID,
						Score:    m.Score,
						Location: m.Target,
						Evidence: m.MatchedValue,
						Rule:     m.RuleName,
					})
				}
				mw.events.LogEvent(evt)
				if mw.mode == "enforce" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"error":"blocked by WAF","layer":"detection"}`))
					return
				}
			}
		}

		// Layer 4b: New detection engine (tokenizer-based detectors)
		if mw.detection != nil {
			detResult := mw.detection.Detect(ctx)
			if detResult.Blocked {
				evt := &WAFEvent{
					Timestamp:  time.Now(),
					RemoteIP:   remoteIP,
					Method:     r.Method,
					Path:       r.URL.Path,
					UserAgent:  r.UserAgent(),
					Layer:      "detection",
					Action:     "block",
					TotalScore: detResult.TotalScore,
					Findings:   detResult.Findings,
					LatencyNS:  time.Since(start).Nanoseconds(),
				}
				mw.events.LogEvent(evt)
				if mw.mode == "enforce" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"error":"blocked by WAF","layer":"detection"}`))
					return
				}
			}
		}

		// Layer 5: Bot Detection
		if mw.botDetect != nil {
			botResult := mw.botDetect.Analyze(r)
			if botResult.Blocked {
				evt := &WAFEvent{
					Timestamp:  time.Now(),
					RemoteIP:   remoteIP,
					Method:     r.Method,
					Path:       r.URL.Path,
					UserAgent:  r.UserAgent(),
					Layer:      "bot",
					Action:     "block",
					TotalScore: botResult.Score,
					Findings: []Finding{{
						Detector: "bot",
						Score:    botResult.Score,
						Rule:     botResult.Rule,
						Evidence: botResult.Details,
					}},
					LatencyNS: time.Since(start).Nanoseconds(),
				}
				mw.events.LogEvent(evt)
				if mw.mode == "enforce" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"error":"blocked by WAF","layer":"bot"}`))
					return
				}
			}
		}

		// Log allowed request
		evt := &WAFEvent{
			Timestamp: time.Now(),
			RemoteIP:  remoteIP,
			Method:    r.Method,
			Path:      r.URL.Path,
			UserAgent: r.UserAgent(),
			Layer:     "pipeline",
			Action:    "allow",
			LatencyNS: time.Since(start).Nanoseconds(),
		}
		mw.events.LogEvent(evt)

		// Layer 6: Response Protection (wrap ResponseWriter)
		responseWriter := w
		if mw.response != nil {
			pw := mw.response.Wrap(w)
			responseWriter = pw
			defer func() {
				if f, ok := pw.(interface{ Flush() }); ok {
					f.Flush()
				}
			}()
		}

		next.ServeHTTP(responseWriter, r)
	})
}

// IPACL returns the IP access list for external management.
func (mw *WAFMiddleware) IPACL() *ipacl.IPAccessList { return mw.ipACL }

// Analytics returns the analytics tracker.
func (mw *WAFMiddleware) Analytics() *Analytics { return mw.analytics }

// RateLimiter returns the rate limiter for external management.
func (mw *WAFMiddleware) RateLimiter() interface{} {
	if mw.rateLimiter == nil {
		return nil
	}
	return mw.rateLimiter
}

// Mode returns the current WAF mode.
func (mw *WAFMiddleware) Mode() string { return mw.mode }

// Status returns a summary of WAF configuration status.
func (mw *WAFMiddleware) Status() map[string]any {
	status := map[string]any{
		"enabled": mw.config.Enabled,
		"mode":    mw.mode,
		"layers": map[string]bool{
			"ip_acl":     mw.ipACL != nil,
			"rate_limit": mw.rateLimiter != nil,
			"sanitizer":  mw.sanitizer != nil,
			"detection":  mw.detection != nil,
			"bot_detect": mw.botDetect != nil,
			"response":   mw.response != nil,
		},
	}
	if mw.analytics != nil {
		stats := mw.analytics.GetStats()
		status["stats"] = stats
	}
	return status
}

// Stop shuts down background goroutines.
func (mw *WAFMiddleware) Stop() {
	if mw.ipACL != nil {
		mw.ipACL.Stop()
	}
	if mw.rateLimiter != nil {
		mw.rateLimiter.Stop()
	}
	if mw.botDetect != nil {
		mw.botDetect.Stop()
	}
}

// buildIPACLConfig converts config types to ipacl package types.
func buildIPACLConfig(cfg *config.WAFIPACLConfig) ipacl.Config {
	c := ipacl.Config{}
	for _, entry := range cfg.Whitelist {
		ec := ipacl.EntryConfig{CIDR: entry.CIDR, Reason: entry.Reason}
		if entry.Expires != "" {
			if t, err := time.Parse(time.RFC3339, entry.Expires); err == nil {
				ec.Expires = t
			}
		}
		c.Whitelist = append(c.Whitelist, ec)
	}
	for _, entry := range cfg.Blacklist {
		ec := ipacl.EntryConfig{CIDR: entry.CIDR, Reason: entry.Reason}
		if entry.Expires != "" {
			if t, err := time.Parse(time.RFC3339, entry.Expires); err == nil {
				ec.Expires = t
			}
		}
		c.Blacklist = append(c.Blacklist, ec)
	}
	if cfg.AutoBan != nil {
		c.AutoBan = ipacl.AutoBanConfig{Enabled: cfg.AutoBan.Enabled}
		if cfg.AutoBan.DefaultTTL != "" {
			if d, err := time.ParseDuration(cfg.AutoBan.DefaultTTL); err == nil {
				c.AutoBan.DefaultTTL = d
			}
		}
		if cfg.AutoBan.MaxTTL != "" {
			if d, err := time.ParseDuration(cfg.AutoBan.MaxTTL); err == nil {
				c.AutoBan.MaxTTL = d
			}
		}
	}
	return c
}

// buildSanitizerConfig converts config types to sanitizer package types.
func buildSanitizerConfig(cfg *config.WAFSanitizerConfig) sanitizer.Config {
	sc := sanitizer.DefaultConfig()
	if cfg.MaxHeaderSize > 0 {
		sc.MaxHeaderSize = cfg.MaxHeaderSize
	}
	if cfg.MaxHeaderCount > 0 {
		sc.MaxHeaderCount = cfg.MaxHeaderCount
	}
	if cfg.MaxBodySize > 0 {
		sc.MaxBodySize = cfg.MaxBodySize
	}
	if cfg.MaxURLLength > 0 {
		sc.MaxURLLength = cfg.MaxURLLength
	}
	if cfg.MaxCookieSize > 0 {
		sc.MaxCookieSize = cfg.MaxCookieSize
	}
	if cfg.MaxCookieCount > 0 {
		sc.MaxCookieCount = cfg.MaxCookieCount
	}
	sc.BlockNullBytes = cfg.BlockNullBytes
	sc.NormalizeEncoding = cfg.NormalizeEncoding
	sc.StripHopByHop = cfg.StripHopByHop
	if len(cfg.AllowedMethods) > 0 {
		sc.AllowedMethods = cfg.AllowedMethods
	}
	for _, o := range cfg.PathOverrides {
		sc.PathOverrides = append(sc.PathOverrides, sanitizer.PathOverride{
			Pattern:     o.Path,
			MaxBodySize: o.MaxBodySize,
		})
	}
	return sc
}

// normalizeMode maps various mode strings to the canonical "enforce", "monitor", or "disabled".
func normalizeMode(mode string) string {
	switch mode {
	case "enforce", "blocking", "block":
		return "enforce"
	case "monitor", "detection":
		return "monitor"
	case "disabled", "off":
		return "disabled"
	default:
		return "enforce"
	}
}
