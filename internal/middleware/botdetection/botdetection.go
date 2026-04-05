// Package botdetection provides bot and crawler detection middleware.
package botdetection

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Action represents the action to take when a bot is detected.
type Action string

const (
	ActionAllow     Action = "allow"     // Allow the request
	ActionBlock     Action = "block"     // Block with 403
	ActionChallenge Action = "challenge" // Return challenge (CAPTCHA, etc.)
	ActionThrottle  Action = "throttle"  // Apply rate limiting
	ActionLog       Action = "log"       // Just log the detection
)

// Config configures bot detection.
type Config struct {
	Enabled              bool            // Enable bot detection
	Action               Action          // Action to take on detection
	BlockKnownBots       bool            // Block known bad bots
	AllowVerified        bool            // Allow verified good bots (Google, Bing, etc.)
	UserAgentRules       []UserAgentRule // User-Agent based rules
	RequestRateThreshold int             // Requests per minute threshold
	JA3Fingerprints      []string        // Known bot JA3 fingerprints
	ChallengePath        string          // Path to redirect challenges to
	ExcludePaths         []string        // Paths to exclude from detection
	CustomHeaders        []HeaderRule    // Header-based detection rules
}

// UserAgentRule defines a rule based on User-Agent string.
type UserAgentRule struct {
	Pattern string // Regex pattern
	Action  Action // Action for this rule
	Name    string // Rule name for logging
}

// HeaderRule defines a rule based on request headers.
type HeaderRule struct {
	Header  string // Header name
	Pattern string // Regex pattern to match
	Action  Action
	Name    string
}

// DefaultConfig returns default bot detection configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:              false,
		Action:               ActionLog,
		BlockKnownBots:       true,
		AllowVerified:        true,
		RequestRateThreshold: 100,
	}
}

// knownGoodBots are verified search engine crawlers.
var knownGoodBots = []string{
	"Googlebot", "Bingbot", "Slurp", "DuckDuckBot", "Baiduspider",
	"YandexBot", "facebookexternalhit", "Twitterbot", "LinkedInBot",
}

// knownBadBots are commonly malicious bots.
var knownBadBots = []string{
	"scrapy", "scraping", "curl", "wget", "python-requests",
	"java", "scrapinghub", "bot/", "spider/", "crawler/",
	"ahrefsbot", "semrushbot", "mj12bot", "dotbot",
}

// BotInfo contains information about a detected bot.
type BotInfo struct {
	Detected  bool      `json:"detected"`
	Type      string    `json:"type"`   // "good", "bad", "suspicious"
	Name      string    `json:"name"`   // Bot name if identified
	Reason    string    `json:"reason"` // Detection reason
	Score     int       `json:"score"`  // Detection score (0-100)
	Timestamp time.Time `json:"timestamp"`
}

// Middleware provides bot detection functionality.
type Middleware struct {
	config       Config
	uaRules      []*regexp.Regexp
	badPatterns  []*regexp.Regexp
	goodPatterns []*regexp.Regexp
	ipTracker    *ipTracker
}

// ipTracker tracks request rates per IP.
type ipTracker struct {
	mu       sync.RWMutex
	requests map[string][]time.Time
	window   time.Duration
}

// New creates a new bot detection middleware.
func New(config Config) *Middleware {
	m := &Middleware{
		config:    config,
		ipTracker: newIPTracker(time.Minute),
	}

	// Compile User-Agent rules
	for _, rule := range config.UserAgentRules {
		if re, err := regexp.Compile(rule.Pattern); err == nil {
			m.uaRules = append(m.uaRules, re)
		}
	}

	// Compile known bad bot patterns
	for _, pattern := range knownBadBots {
		if re, err := regexp.Compile(`(?i)` + pattern); err == nil {
			m.badPatterns = append(m.badPatterns, re)
		}
	}

	// Compile known good bot patterns
	for _, pattern := range knownGoodBots {
		if re, err := regexp.Compile(`(?i)` + pattern); err == nil {
			m.goodPatterns = append(m.goodPatterns, re)
		}
	}

	return m
}

// newIPTracker creates a new IP request tracker.
func newIPTracker(window time.Duration) *ipTracker {
	return &ipTracker{
		requests: make(map[string][]time.Time),
		window:   window,
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "botdetection"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 95 // Early detection, after IP filter (100)
}

// Wrap wraps the handler with bot detection.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Detect bot
		info := m.detect(r)

		// Store bot info in context for other middleware
		if info.Detected {
			r = r.WithContext(contextWithBotInfo(r.Context(), info))
		}

		// Take action based on detection and config
		action := m.determineAction(info)

		switch action {
		case ActionBlock:
			m.block(w, r, info)
			return
		case ActionChallenge:
			m.challenge(w, r)
			return
		case ActionThrottle:
			// Throttle is handled by rate limiter, just mark the request
			w.Header().Set("X-Bot-Detected", info.Type)
			w.Header().Set("X-Bot-Name", info.Name)
			next.ServeHTTP(w, r)
		case ActionLog:
			// Just add header and continue
			if info.Detected {
				w.Header().Set("X-Bot-Detected", info.Type)
				w.Header().Set("X-Bot-Name", info.Name)
			}
			next.ServeHTTP(w, r)
		default: // ActionAllow
			// Even when allowing, add bot detection headers if a bot was detected
			if info.Detected {
				w.Header().Set("X-Bot-Detected", info.Type)
				w.Header().Set("X-Bot-Name", info.Name)
			}
			next.ServeHTTP(w, r)
		}
	})
}

// detect analyzes the request for bot signatures.
func (m *Middleware) detect(r *http.Request) BotInfo {
	info := BotInfo{
		Detected:  false,
		Timestamp: time.Now(),
		Score:     0,
	}

	ua := strings.ToLower(r.UserAgent())
	if ua == "" {
		info.Score += 30 // No User-Agent is suspicious
		info.Reason = "missing user-agent"
		info.Detected = true
	}

	// Check for good bots
	if m.config.AllowVerified {
		for _, pattern := range m.goodPatterns {
			if pattern.MatchString(ua) {
				info.Detected = true
				info.Type = "good"
				info.Name = extractBotName(ua)
				info.Score = 0
				return info
			}
		}
	}

	// Check for bad bots
	if m.config.BlockKnownBots {
		for _, pattern := range m.badPatterns {
			if pattern.MatchString(ua) {
				info.Detected = true
				info.Type = "bad"
				info.Name = extractBotName(ua)
				info.Score = 90
				info.Reason = "known bad bot signature"
				return info
			}
		}
	}

	// Check custom User-Agent rules
	for i, rule := range m.config.UserAgentRules {
		if i < len(m.uaRules) && m.uaRules[i].MatchString(ua) {
			info.Detected = true
			info.Type = "suspicious"
			info.Score += 40
			if info.Reason == "" {
				info.Reason = "custom rule: " + rule.Name
			}
		}
	}

	// Check request rate
	if m.config.RequestRateThreshold > 0 {
		ip := r.RemoteAddr
		if count := m.ipTracker.count(ip); count > m.config.RequestRateThreshold {
			info.Detected = true
			info.Type = "suspicious"
			info.Score += 30
			info.Reason = "high request rate: " + string(rune(count)) + "/min"
		}
	}

	// Check custom headers
	for _, rule := range m.config.CustomHeaders {
		if val := r.Header.Get(rule.Header); val != "" {
			if matched, _ := regexp.MatchString(rule.Pattern, val); matched {
				info.Detected = true
				info.Type = "suspicious"
				info.Score += 25
				if info.Reason == "" {
					info.Reason = "header rule: " + rule.Name
				}
			}
		}
	}

	// Additional heuristics
	if info.Score < 50 && info.Detected {
		info.Type = "suspicious"
	} else if info.Score >= 50 {
		info.Type = "bad"
	}

	return info
}

// determineAction decides what action to take.
func (m *Middleware) determineAction(info BotInfo) Action {
	if !info.Detected {
		return ActionAllow
	}

	// Good bots are always allowed
	if info.Type == "good" && m.config.AllowVerified {
		return ActionAllow
	}

	// Bad bots are blocked if configured
	if info.Type == "bad" && m.config.BlockKnownBots {
		return m.config.Action
	}

	// Suspicious bots use the configured action
	if info.Type == "suspicious" {
		return m.config.Action
	}

	return ActionAllow
}

// block blocks the request.
func (m *Middleware) block(w http.ResponseWriter, r *http.Request, info BotInfo) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"Access denied","reason":"bot detected","type":"` + info.Type + `"}`))
}

// challenge redirects to challenge page.
func (m *Middleware) challenge(w http.ResponseWriter, r *http.Request) {
	if m.config.ChallengePath != "" {
		http.Redirect(w, r, m.config.ChallengePath+"?return="+r.URL.Path, http.StatusFound)
	} else {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"Challenge required"}`))
	}
}

// count returns the request count for an IP in the tracking window.
func (t *ipTracker) count(ip string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	// Clean old entries and count recent ones
	var recent []time.Time
	for _, ts := range t.requests[ip] {
		if ts.After(cutoff) {
			recent = append(recent, ts)
		}
	}

	// Add current request
	recent = append(recent, now)
	t.requests[ip] = recent

	return len(recent)
}

// context key for bot info
type botInfoKey struct{}

// contextWithBotInfo adds bot info to context.
func contextWithBotInfo(ctx context.Context, info BotInfo) context.Context {
	return context.WithValue(ctx, botInfoKey{}, info)
}

// GetBotInfo retrieves bot info from request context (helper).
func GetBotInfo(ctx context.Context) *BotInfo {
	if info, ok := ctx.Value(botInfoKey{}).(BotInfo); ok {
		return &info
	}
	return nil
}
func extractBotName(ua string) string {
	// Try to find a known bot name
	for _, bot := range knownGoodBots {
		if strings.Contains(strings.ToLower(ua), strings.ToLower(bot)) {
			return bot
		}
	}
	for _, bot := range knownBadBots {
		if strings.Contains(strings.ToLower(ua), strings.ToLower(bot)) {
			return bot
		}
	}
	return "unknown"
}

// GetBotInfoFromRequest retrieves bot info from request context (helper).
func GetBotInfoFromRequest(r *http.Request) *BotInfo {
	return GetBotInfo(r.Context())
}
