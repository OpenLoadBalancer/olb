package waf

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("Enabled should be true by default")
	}
	if config.Mode != "blocking" {
		t.Errorf("Mode = %q, want blocking", config.Mode)
	}
	if config.DefaultAction != ActionBlock {
		t.Errorf("DefaultAction = %v, want ActionBlock", config.DefaultAction)
	}
	if config.AnomalyScore != 10 {
		t.Errorf("AnomalyScore = %d, want 10", config.AnomalyScore)
	}
	if len(config.Rules) == 0 {
		t.Error("Default rules should not be empty")
	}
}

func TestRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    *Rule
		wantErr bool
	}{
		{
			name: "valid rule",
			rule: &Rule{
				ID:       "test-001",
				Name:     "Test Rule",
				Targets:  []string{"args"},
				Patterns: []string{"test"},
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			rule: &Rule{
				Name:     "Test Rule",
				Targets:  []string{"args"},
				Patterns: []string{"test"},
			},
			wantErr: true,
		},
		{
			name: "missing name",
			rule: &Rule{
				ID:       "test-001",
				Targets:  []string{"args"},
				Patterns: []string{"test"},
			},
			wantErr: true,
		},
		{
			name: "no targets",
			rule: &Rule{
				ID:       "test-001",
				Name:     "Test Rule",
				Patterns: []string{"test"},
			},
			wantErr: true,
		},
		{
			name: "no patterns",
			rule: &Rule{
				ID:      "test-001",
				Name:    "Test Rule",
				Targets: []string{"args"},
			},
			wantErr: true,
		},
		{
			name: "invalid pattern",
			rule: &Rule{
				ID:       "test-001",
				Name:     "Test Rule",
				Targets:  []string{"args"},
				Patterns: []string{"[invalid"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRule_Validate_Defaults(t *testing.T) {
	rule := &Rule{
		ID:       "test-001",
		Name:     "Test Rule",
		Targets:  []string{"args"},
		Patterns: []string{"test"},
	}

	err := rule.Validate()
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}

	if rule.Action != ActionBlock {
		t.Errorf("Action = %v, want ActionBlock", rule.Action)
	}
	if rule.Severity != SeverityMedium {
		t.Errorf("Severity = %v, want SeverityMedium", rule.Severity)
	}
	if rule.Score != 5 {
		t.Errorf("Score = %d, want 5", rule.Score)
	}
}

func TestRule_Match(t *testing.T) {
	rule := &Rule{
		ID:       "test-001",
		Name:     "Test Rule",
		Enabled:  true,
		Targets:  []string{"args"},
		Patterns: []string{"attack"},
		Action:   ActionBlock,
		Severity: SeverityHigh,
		Score:    10,
	}
	rule.Validate()

	// Create request with attack in query
	req := httptest.NewRequest("GET", "http://example.com/?q=attack", nil)

	match := rule.Match(req, nil)
	if match == nil {
		t.Fatal("Expected match")
	}
	if match.RuleID != "test-001" {
		t.Errorf("RuleID = %q, want test-001", match.RuleID)
	}
	if match.Action != ActionBlock {
		t.Errorf("Action = %v, want ActionBlock", match.Action)
	}
}

func TestRule_Match_Disabled(t *testing.T) {
	rule := &Rule{
		ID:       "test-001",
		Name:     "Test Rule",
		Enabled:  false,
		Targets:  []string{"args"},
		Patterns: []string{"attack"},
	}
	rule.Validate()

	req := httptest.NewRequest("GET", "http://example.com/?q=attack", nil)

	match := rule.Match(req, nil)
	if match != nil {
		t.Error("Disabled rule should not match")
	}
}

func TestRule_Match_MethodFilter(t *testing.T) {
	rule := &Rule{
		ID:       "test-001",
		Name:     "Test Rule",
		Enabled:  true,
		Methods:  []string{"POST"},
		Targets:  []string{"body"},
		Patterns: []string{"attack"},
	}
	rule.Validate()

	// GET request should not match
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	match := rule.Match(req, []byte("attack"))
	if match != nil {
		t.Error("GET request should not match POST-only rule")
	}

	// POST request should match
	req = httptest.NewRequest("POST", "http://example.com/", nil)
	match = rule.Match(req, []byte("attack"))
	if match == nil {
		t.Error("POST request should match")
	}
}

func TestRule_getTargetValue(t *testing.T) {
	rule := &Rule{}

	tests := []struct {
		target   string
		expected string
		setup    func(*http.Request)
	}{
		{"uri", "http://example.com/path?q=test", nil},
		{"path", "/path", nil},
		{"method", "GET", nil},
		{"user_agent", "TestAgent", func(r *http.Request) { r.Header.Set("User-Agent", "TestAgent") }},
		{"host", "example.com", nil},
		{"arg_q", "test", nil},
		{"header_X-Custom", "value", func(r *http.Request) { r.Header.Set("X-Custom", "value") }},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/path?q=test", nil)
			if tt.setup != nil {
				tt.setup(req)
			}

			got := rule.getTargetValue(req, nil, tt.target)
			if got != tt.expected {
				t.Errorf("getTargetValue(%q) = %q, want %q", tt.target, got, tt.expected)
			}
		})
	}
}

func TestNew(t *testing.T) {
	config := DefaultConfig()
	waf, err := New(config)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if waf.config != config {
		t.Error("Config mismatch")
	}
	if len(waf.rules) == 0 {
		t.Error("WAF should have default rules")
	}
}

func TestNew_NilConfig(t *testing.T) {
	waf, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) error: %v", err)
	}

	if waf.config == nil {
		t.Error("Config should use defaults when nil")
	}
}

func TestWAF_Process_Allowed(t *testing.T) {
	waf, _ := New(nil)

	// Normal request should be allowed
	req := httptest.NewRequest("GET", "http://example.com/?q=hello", nil)

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if !result.Allowed {
		t.Error("Normal request should be allowed")
	}
}

func TestWAF_Process_Blocked_SQLi(t *testing.T) {
	waf, _ := New(nil)

	// SQL injection attempt
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	// Manually set RawQuery to bypass parsing
	req.URL.RawQuery = "id=1'+OR+'1'='1"

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if result.Allowed {
		t.Error("SQL injection should be blocked")
	}
	if result.Action != ActionBlock {
		t.Errorf("Action = %v, want ActionBlock", result.Action)
	}
	if len(result.Matches) == 0 {
		t.Error("Should have match details")
	}
}

func TestWAF_Process_Blocked_XSS(t *testing.T) {
	waf, _ := New(nil)

	// XSS attempt - match raw query that contains the script tag
	req := httptest.NewRequest("GET", "http://example.com/?q=<script>alert(1)</script>", nil)
	// Manually set RawQuery to bypass parsing
	req.URL.RawQuery = "q=<script>alert(1)</script>"

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if result.Allowed {
		t.Error("XSS should be blocked")
	}
}

func TestWAF_Process_Blocked_PathTraversal(t *testing.T) {
	waf, _ := New(nil)

	// Path traversal attempt
	req := httptest.NewRequest("GET", "http://example.com/../../../etc/passwd", nil)

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if result.Allowed {
		t.Error("Path traversal should be blocked")
	}
}

func TestWAF_Process_DetectionMode(t *testing.T) {
	config := &Config{
		Enabled:       true,
		Mode:          "detection",
		DefaultAction: ActionBlock,
		AnomalyScore:  10,
		Rules:         DefaultRules(),
	}
	waf, _ := New(config)

	// SQL injection in detection mode (URL encoded)
	req := httptest.NewRequest("GET", "http://example.com/?id=1%27+OR+%271%27=%271", nil)

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	// In detection mode, requests are allowed but logged
	if !result.Allowed {
		t.Error("Detection mode should allow requests")
	}
	if len(result.Matches) == 0 {
		t.Error("Should have match details in detection mode")
	}
}

func TestWAF_Process_Disabled(t *testing.T) {
	config := &Config{
		Enabled: false,
		Rules:   DefaultRules(),
	}
	waf, _ := New(config)

	// Even SQL injection should be allowed when disabled
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	// Manually set RawQuery to bypass parsing
	req.URL.RawQuery = "id=1'+OR+'1'='1"

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if !result.Allowed {
		t.Error("WAF disabled should allow all requests")
	}
}

func TestWAF_Process_WithBody(t *testing.T) {
	waf, _ := New(nil)

	// POST request with SQL injection in body
	body := []byte("username=admin'--&password=anything")
	req := httptest.NewRequest("POST", "http://example.com/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	result, err := waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if result.Allowed {
		t.Error("SQL injection in body should be blocked")
	}

	// Body should be restored for downstream handlers
	restoredBody, _ := io.ReadAll(req.Body)
	if !bytes.Equal(restoredBody, body) {
		t.Error("Body should be restored for downstream handlers")
	}
}

func TestWAF_AddRule(t *testing.T) {
	waf, _ := New(nil)

	rule := &Rule{
		ID:       "custom-001",
		Name:     "Custom Rule",
		Targets:  []string{"args"},
		Patterns: []string{"evil"},
	}

	err := waf.AddRule(rule)
	if err != nil {
		t.Fatalf("AddRule error: %v", err)
	}

	rules := waf.GetRules()
	found := false
	for _, r := range rules {
		if r.ID == "custom-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Custom rule should be added")
	}
}

func TestWAF_RemoveRule(t *testing.T) {
	waf, _ := New(nil)

	// Add then remove
	waf.AddRule(&Rule{
		ID:       "temp-001",
		Name:     "Temp Rule",
		Targets:  []string{"args"},
		Patterns: []string{"test"},
	})

	removed := waf.RemoveRule("temp-001")
	if !removed {
		t.Error("Should return true when rule removed")
	}

	// Try to remove again
	removed = waf.RemoveRule("temp-001")
	if removed {
		t.Error("Should return false for non-existent rule")
	}
}

func TestResult_IsBlocked(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		expected bool
	}{
		{
			name:     "blocked",
			result:   &Result{Allowed: false},
			expected: true,
		},
		{
			name:     "allowed",
			result:   &Result{Allowed: true},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.IsBlocked()
			if got != tt.expected {
				t.Errorf("IsBlocked() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := &JSONLogger{Writer: &buf}

	match := &Match{
		RuleID:       "test-001",
		RuleName:     "Test",
		Target:       "args",
		Pattern:      "test",
		MatchedValue: "test123",
		Action:       ActionBlock,
		Severity:     SeverityHigh,
		Score:        10,
	}

	req := httptest.NewRequest("GET", "http://example.com/?q=test123", nil)
	req.Header.Set("User-Agent", "TestBot")

	logger.Log(match, req)

	output := buf.String()
	if !strings.Contains(output, "test-001") {
		t.Error("Log should contain rule ID")
	}
	if !strings.Contains(output, "TestBot") {
		t.Error("Log should contain user agent")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
		{"exact", 5, "exact"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()

	if len(rules) == 0 {
		t.Fatal("DefaultRules should not be empty")
	}

	// Check for specific rule IDs
	expectedIDs := map[string]bool{
		"sqli-001":   false,
		"xss-001":    false,
		"path-001":   false,
		"rfi-001":    false,
		"cmdi-001":   false,
		"attack-001": false,
	}

	for _, rule := range rules {
		if _, ok := expectedIDs[rule.ID]; ok {
			expectedIDs[rule.ID] = true
		}
	}

	for id, found := range expectedIDs {
		if !found {
			t.Errorf("Default rule %q not found", id)
		}
	}
}

func TestWAF_SetLogger(t *testing.T) {
	waf, err := New(nil)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var buf bytes.Buffer
	customLogger := &JSONLogger{Writer: &buf}

	waf.SetLogger(customLogger)

	// Verify the logger is set by processing a request that triggers a match
	req := httptest.NewRequest("GET", "http://example.com/?id=1'+OR+'1'='1", nil)
	req.URL.RawQuery = "id=1'+OR+'1'='1"

	_, err = waf.Process(req)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	// The custom logger should have received log entries
	if buf.Len() == 0 {
		t.Error("Expected log output from custom logger")
	}
	if !strings.Contains(buf.String(), "sqli-001") {
		t.Error("Log output should contain SQL injection rule ID")
	}
}

func TestJSONLogger_Log(t *testing.T) {
	var buf bytes.Buffer
	logger := &JSONLogger{Writer: &buf}

	match := &Match{
		RuleID:       "test-rule",
		RuleName:     "Test Rule",
		Target:       "args",
		Pattern:      "test-pattern",
		MatchedValue: "matched-value",
		Action:       ActionBlock,
		Severity:     SeverityCritical,
		Score:        10,
	}

	req := httptest.NewRequest("POST", "http://example.com/api?key=value", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("User-Agent", "SecurityScanner/1.0")

	logger.Log(match, req)

	output := buf.String()

	// Verify all expected fields are present in the JSON output
	if !strings.Contains(output, `"rule_id":"test-rule"`) {
		t.Error("Log output should contain rule_id")
	}
	if !strings.Contains(output, `"remote_addr":"192.168.1.100:12345"`) {
		t.Error("Log output should contain remote_addr")
	}
	if !strings.Contains(output, `"method":"POST"`) {
		t.Error("Log output should contain method")
	}
	if !strings.Contains(output, `"user_agent":"SecurityScanner/1.0"`) {
		t.Error("Log output should contain user_agent")
	}
	if !strings.Contains(output, `"severity":"critical"`) {
		t.Error("Log output should contain severity")
	}
	// Output should end with newline
	if output[len(output)-1] != '\n' {
		t.Error("Log output should end with newline")
	}
}

func TestDefaultLogger_Log(t *testing.T) {
	// Default logger is a no-op, should not panic
	logger := &defaultLogger{}
	match := &Match{
		RuleID: "test",
	}
	req := httptest.NewRequest("GET", "http://example.com/", nil)

	// Should not panic
	logger.Log(match, req)
}

func BenchmarkWAF_Process(b *testing.B) {
	waf, _ := New(nil)
	req := httptest.NewRequest("GET", "http://example.com/?q=hello", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		waf.Process(req)
	}
}

func BenchmarkWAF_Process_SQLi(b *testing.B) {
	waf, _ := New(nil)
	req := httptest.NewRequest("GET", "http://example.com/?id=1' OR '1'='1", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		waf.Process(req)
	}
}
