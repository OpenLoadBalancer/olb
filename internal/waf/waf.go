// Package waf provides Web Application Firewall functionality for security filtering.
package waf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Action represents the action to take when a rule matches.
type Action string

const (
	// ActionBlock blocks the request.
	ActionBlock Action = "block"
	// ActionLog logs the request but allows it.
	ActionLog Action = "log"
	// ActionChallenge presents a challenge (CAPTCHA, etc).
	ActionChallenge Action = "challenge"
)

// Severity represents the severity of a rule match.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Rule represents a WAF security rule.
type Rule struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Enabled     bool     `json:"enabled" yaml:"enabled"`
	Action      Action   `json:"action" yaml:"action"`
	Severity    Severity `json:"severity" yaml:"severity"`
	Score       int      `json:"score" yaml:"score"`

	// Match conditions
	Targets  []string `json:"targets" yaml:"targets"`   // e.g., "args", "headers", "body", "uri"
	Patterns []string `json:"patterns" yaml:"patterns"` // Regex patterns
	Methods  []string `json:"methods" yaml:"methods"`   // HTTP methods to check

	// Compiled patterns (populated at load time)
	compiledPatterns []*regexp.Regexp
}

// Validate validates the rule.
func (r *Rule) Validate() error {
	if r.ID == "" {
		return errors.New("rule ID is required")
	}
	if r.Name == "" {
		return errors.New("rule name is required")
	}
	if len(r.Targets) == 0 {
		return errors.New("rule must have at least one target")
	}
	if len(r.Patterns) == 0 {
		return errors.New("rule must have at least one pattern")
	}

	// Compile patterns
	for _, pattern := range r.Patterns {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
		r.compiledPatterns = append(r.compiledPatterns, re)
	}

	if r.Action == "" {
		r.Action = ActionBlock
	}
	if r.Severity == "" {
		r.Severity = SeverityMedium
	}
	if r.Score == 0 {
		r.Score = 5
	}

	return nil
}

// Match checks if the rule matches the request.
func (r *Rule) Match(req *http.Request, body []byte) *Match {
	if !r.Enabled {
		return nil
	}

	// Check HTTP method
	if len(r.Methods) > 0 {
		methodMatch := false
		for _, m := range r.Methods {
			if strings.EqualFold(req.Method, m) {
				methodMatch = true
				break
			}
		}
		if !methodMatch {
			return nil
		}
	}

	// Check each target
	for _, target := range r.Targets {
		value := r.getTargetValue(req, body, target)
		if value == "" {
			continue
		}

		// Check each pattern
		for i, re := range r.compiledPatterns {
			if re.MatchString(value) {
				return &Match{
					RuleID:       r.ID,
					RuleName:     r.Name,
					Target:       target,
					Pattern:      r.Patterns[i],
					MatchedValue: truncate(value, 100),
					Action:       r.Action,
					Severity:     r.Severity,
					Score:        r.Score,
					Timestamp:    time.Now(),
				}
			}
		}
	}

	return nil
}

// getTargetValue extracts the target value from the request.
func (r *Rule) getTargetValue(req *http.Request, body []byte, target string) string {
	switch target {
	case "uri", "url":
		return req.URL.String()
	case "path":
		return req.URL.Path
	case "query", "args":
		// Check both raw and decoded query for encoded attack patterns
		raw := req.URL.RawQuery
		decoded, err := url.QueryUnescape(raw)
		if err != nil {
			return raw
		}
		if decoded != raw {
			return raw + "\n" + decoded
		}
		return raw
	case "method":
		return req.Method
	case "headers":
		var parts []string
		for name, values := range req.Header {
			for _, v := range values {
				parts = append(parts, name+": "+v)
			}
		}
		return strings.Join(parts, "\n")
	case "user_agent", "user-agent":
		return req.UserAgent()
	case "referer":
		return req.Referer()
	case "body", "post_args":
		return string(body)
	case "remote_ip", "remote_addr":
		return req.RemoteAddr
	case "host":
		return req.Host
	case "content_type", "content-type":
		return req.Header.Get("Content-Type")
	default:
		// Check for specific header, arg, or cookie
		if strings.HasPrefix(target, "arg_") {
			return req.URL.Query().Get(target[4:])
		}
		if strings.HasPrefix(target, "header_") {
			return req.Header.Get(target[7:])
		}
		if strings.HasPrefix(target, "cookie_") {
			cookie, _ := req.Cookie(target[7:])
			if cookie != nil {
				return cookie.Value
			}
		}
	}
	return ""
}

// Match represents a WAF rule match.
type Match struct {
	RuleID       string    `json:"rule_id"`
	RuleName     string    `json:"rule_name"`
	Target       string    `json:"target"`
	Pattern      string    `json:"pattern"`
	MatchedValue string    `json:"matched_value"`
	Action       Action    `json:"action"`
	Severity     Severity  `json:"severity"`
	Score        int       `json:"score"`
	Timestamp    time.Time `json:"timestamp"`
}

// Config contains WAF configuration.
type Config struct {
	Enabled       bool     `json:"enabled" yaml:"enabled"`
	Mode          string   `json:"mode" yaml:"mode"` // "blocking" or "detection"
	DefaultAction Action   `json:"default_action" yaml:"default_action"`
	Rules         []*Rule  `json:"rules" yaml:"rules"`
	RuleFiles     []string `json:"rule_files" yaml:"rule_files"`
	AnomalyScore  int      `json:"anomaly_score" yaml:"anomaly_score"` // Threshold for blocking
}

// DefaultConfig returns a default WAF configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:       true,
		Mode:          "blocking",
		DefaultAction: ActionBlock,
		AnomalyScore:  10,
		Rules:         DefaultRules(),
	}
}

// WAF is the Web Application Firewall.
type WAF struct {
	config *Config
	rules  []*Rule
	mu     sync.RWMutex
	logger Logger
}

// Logger is the interface for WAF logging.
type Logger interface {
	Log(match *Match, req *http.Request)
}

// New creates a new WAF.
func New(config *Config) (*WAF, error) {
	if config == nil {
		config = DefaultConfig()
	}

	waf := &WAF{
		config: config,
		logger: &defaultLogger{},
	}

	// Add built-in rules
	for _, rule := range config.Rules {
		if err := rule.Validate(); err != nil {
			return nil, fmt.Errorf("invalid rule %q: %w", rule.ID, err)
		}
		waf.rules = append(waf.rules, rule)
	}

	return waf, nil
}

// SetLogger sets the WAF logger.
func (w *WAF) SetLogger(logger Logger) {
	w.logger = logger
}

// Process processes a request through the WAF.
func (w *WAF) Process(req *http.Request) (*Result, error) {
	if !w.config.Enabled {
		return &Result{Allowed: true}, nil
	}

	// Read body if present
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(req.Body, 1024*1024)) // 1MB limit
		if err != nil {
			return nil, err
		}
		// Restore body for downstream handlers
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	w.mu.RLock()
	rules := w.rules
	w.mu.RUnlock()

	var matches []*Match
	var totalScore int

	// Check each rule
	for _, rule := range rules {
		match := rule.Match(req, body)
		if match != nil {
			matches = append(matches, match)
			totalScore += match.Score

			// Log the match
			if w.logger != nil {
				w.logger.Log(match, req)
			}

			// In blocking mode with critical/high severity, block immediately
			if (w.config.Mode == "blocking" || w.config.Mode == "block") && rule.Action == ActionBlock &&
				(rule.Severity == SeverityCritical || rule.Severity == SeverityHigh) {
				return &Result{
					Allowed:   false,
					Action:    ActionBlock,
					Matches:   matches,
					Score:     totalScore,
					Threshold: w.config.AnomalyScore,
				}, nil
			}
		}
	}

	// Check anomaly score threshold
	if w.config.Mode == "blocking" && totalScore >= w.config.AnomalyScore {
		return &Result{
			Allowed:   false,
			Action:    ActionBlock,
			Matches:   matches,
			Score:     totalScore,
			Threshold: w.config.AnomalyScore,
		}, nil
	}

	// Request is allowed (may have logged matches)
	return &Result{
		Allowed:   true,
		Matches:   matches,
		Score:     totalScore,
		Threshold: w.config.AnomalyScore,
	}, nil
}

// Result represents the result of WAF processing.
type Result struct {
	Allowed   bool     `json:"allowed"`
	Action    Action   `json:"action"`
	Matches   []*Match `json:"matches"`
	Score     int      `json:"score"`
	Threshold int      `json:"threshold"`
}

// IsBlocked returns true if the request was blocked.
func (r *Result) IsBlocked() bool {
	return !r.Allowed
}

// AddRule adds a rule to the WAF.
func (w *WAF) AddRule(rule *Rule) error {
	if err := rule.Validate(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.rules = append(w.rules, rule)
	return nil
}

// RemoveRule removes a rule by ID.
func (w *WAF) RemoveRule(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i, rule := range w.rules {
		if rule.ID == id {
			w.rules = append(w.rules[:i], w.rules[i+1:]...)
			return true
		}
	}
	return false
}

// GetRules returns all rules.
func (w *WAF) GetRules() []*Rule {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Return a copy
	rules := make([]*Rule, len(w.rules))
	copy(rules, w.rules)
	return rules
}

// defaultLogger is a no-op logger.
type defaultLogger struct{}

func (l *defaultLogger) Log(match *Match, req *http.Request) {
	// Default: no-op, can be replaced with structured logging
}

// JSONLogger logs matches as JSON.
type JSONLogger struct {
	Writer io.Writer
}

// Log logs a match as JSON.
func (l *JSONLogger) Log(match *Match, req *http.Request) {
	entry := struct {
		*Match
		RemoteAddr string `json:"remote_addr"`
		Method     string `json:"method"`
		URI        string `json:"uri"`
		UserAgent  string `json:"user_agent"`
	}{
		Match:      match,
		RemoteAddr: req.RemoteAddr,
		Method:     req.Method,
		URI:        req.URL.String(),
		UserAgent:  req.UserAgent(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("waf: failed to marshal audit log entry: %v", err)
		return
	}
	if _, err := l.Writer.Write(append(data, '\n')); err != nil {
		log.Printf("waf: failed to write audit log entry: %v", err)
	}
}

// truncate truncates a string to max length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// DefaultRules returns the default set of security rules.
func DefaultRules() []*Rule {
	return []*Rule{
		// SQL Injection
		{
			ID:          "sqli-001",
			Name:        "SQL Injection - Basic",
			Description: "Detects common SQL injection patterns",
			Enabled:     true,
			Action:      ActionBlock,
			Severity:    SeverityCritical,
			Score:       10,
			Targets:     []string{"args", "body", "headers"},
			Patterns: []string{
				`(\%27)|(\')|(\-\-)|(\%23)|(#)`,
				`((\%3D)|(=))[^\n]*((\%27)|(\')|(\-\-)|(\%3B)|(;))`,
				`\b(SELECT|INSERT|UPDATE|DELETE|DROP|UNION|ALTER|CREATE)\b.*\b(FROM|INTO|TABLE|DATABASE)\b`,
				`\b(AND|OR)\b\s+\d+\s*=\s*\d+`,
				`\b(SLEEP|BENCHMARK|WAITFOR|DELAY)\s*\(`,
			},
		},
		// XSS
		{
			ID:          "xss-001",
			Name:        "Cross-Site Scripting (XSS)",
			Description: "Detects XSS attack patterns",
			Enabled:     true,
			Action:      ActionBlock,
			Severity:    SeverityHigh,
			Score:       8,
			Targets:     []string{"args", "body", "headers"},
			Patterns: []string{
				`<script[^>]*>`,
				`javascript:`,
				`on\w+\s*=\s*["']?[^"'>]+`,
				`<iframe`,
				`<object`,
				`<embed`,
				`eval\s*\(`,
				`expression\s*\(`,
			},
		},
		// Path Traversal
		{
			ID:          "path-001",
			Name:        "Path Traversal",
			Description: "Detects directory traversal attempts",
			Enabled:     true,
			Action:      ActionBlock,
			Severity:    SeverityHigh,
			Score:       7,
			Targets:     []string{"uri", "args"},
			Patterns: []string{
				`\.\./`,
				`\.\.\\`,
				`%2e%2e/`,
				`%2e%2e%2f`,
				`\.\.//`,
				`etc/passwd`,
				`etc/shadow`,
				`boot\.ini`,
				`win\.ini`,
			},
		},
		// Remote File Inclusion
		{
			ID:          "rfi-001",
			Name:        "Remote File Inclusion",
			Description: "Detects RFI attack patterns",
			Enabled:     true,
			Action:      ActionBlock,
			Severity:    SeverityHigh,
			Score:       7,
			Targets:     []string{"args", "body"},
			Patterns: []string{
				`https?://[^\s\"<>]+`,
				`ftp://[^\s\"<>]+`,
				`php://`,
				`file://`,
				`data://`,
			},
		},
		// Command Injection
		{
			ID:          "cmdi-001",
			Name:        "Command Injection",
			Description: "Detects command injection patterns",
			Enabled:     true,
			Action:      ActionBlock,
			Severity:    SeverityCritical,
			Score:       10,
			Targets:     []string{"args", "body", "headers"},
			Patterns: []string{
				`[;&|]\s*\w+`,
				`\$\(`,
				`` + "`" + `[^` + "`" + `]*` + "`" + ``,
				`\|\s*\w+`,
				`;\s*\w+`,
				`bash\s+-[ci]`,
				`sh\s+-[ci]`,
				`cmd\.exe`,
				`powershell`,
			},
		},
		// Common Attack Signatures
		{
			ID:          "attack-001",
			Name:        "Common Attack Signatures",
			Description: "Detects common attack signatures",
			Enabled:     true,
			Action:      ActionLog,
			Severity:    SeverityMedium,
			Score:       3,
			Targets:     []string{"uri", "args", "user_agent"},
			Patterns: []string{
				`sqlmap`,
				`nikto`,
				`nmap`,
				`masscan`,
				`zgrab`,
				`gobuster`,
				`dirbuster`,
				`wfuzz`,
				`burp`,
				`acunetix`,
			},
		},
		// Large Request Body
		{
			ID:          "size-001",
			Name:        "Oversized Content-Type",
			Description: "Detects suspicious content types",
			Enabled:     true,
			Action:      ActionLog,
			Severity:    SeverityLow,
			Score:       2,
			Targets:     []string{"content_type"},
			Patterns: []string{
				`application/x-www-form-urlencoded.*charset`,
			},
		},
	}
}
