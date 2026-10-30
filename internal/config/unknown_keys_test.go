package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/config/yaml"
)

func rawYAML(t *testing.T, src string) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := yaml.UnmarshalString(src, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestUnknownKeys_ReportsTyposWithSuggestions(t *testing.T) {
	got := UnknownKeys(rawYAML(t, `
middleware:
  rate_limt:
    enabled: true
  circuit_breaker:
    enabled: true
    open_duraton: 30s
pools:
  - name: p
    algoritm: round_robin
    backends:
      - adress: "127.0.0.1:1"
`))
	want := []string{
		`unknown config key "middleware.circuit_breaker.open_duraton" (did you mean "open_duration"?)`,
		`unknown config key "middleware.rate_limt" (did you mean "rate_limit"?)`,
		`unknown config key "pools.algoritm" (did you mean "algorithm"?)`,
		`unknown config key "pools.backends.adress" (did you mean "address"?)`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUnknownKeys_IgnoresFreeFormMapsAndValidConfigs(t *testing.T) {
	got := UnknownKeys(rawYAML(t, `
middleware:
  basic_auth:
    enabled: true
    users:
      anyone-at-all: secret
  headers:
    enabled: true
    response_add:
      X-Anything: value
`))
	if len(got) != 0 {
		t.Errorf("valid config reported: %v", got)
	}
}

func TestUnknownKeys_NoSuggestionWhenNothingIsClose(t *testing.T) {
	got := UnknownKeys(rawYAML(t, "totally_unrelated_setting: 1\n"))
	if len(got) != 1 || strings.Contains(got[0], "did you mean") {
		t.Errorf("got %v, want one message without a suggestion", got)
	}
}

func TestLoader_WarnsOnUnknownKeysButStillLoads(t *testing.T) {
	src := `listeners:
  - name: http
    address: ":8080"
    protocol: http
    routes:
      - path: /
        pool: p
pools:
  - name: p
    algoritm: round_robin
    backends:
      - address: "127.0.0.1:9000"
`
	path := filepath.Join(t.TempDir(), "olb.yaml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLoader()
	cfg, err := l.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil || len(cfg.Pools) != 1 {
		t.Fatalf("config did not load: %+v", cfg)
	}
	if len(l.Warnings) != 1 || !strings.Contains(l.Warnings[0], `"pools.algoritm"`) {
		t.Errorf("Warnings = %v, want the algoritm typo", l.Warnings)
	}
}

func TestValidate_RejectsPerRouteMiddleware(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{Name: "http", Address: ":0", Protocol: "http", Routes: []*Route{
			{Path: "/", Pool: "p", Middleware: []map[string]any{{"name": "basic_auth"}}},
		}}},
		Pools: []*Pool{{Name: "p", Backends: []*Backend{{Address: "127.0.0.1:1"}}}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "per-route middleware") {
		t.Fatalf("expected per-route middleware error, got %v", err)
	}
}

func TestValidate_RejectsBotDetectionThrottle(t *testing.T) {
	for name, bd := range map[string]*BotDetectionConfig{
		"global":      {Enabled: true, Action: "throttle"},
		"user_agent":  {Enabled: true, UserAgentRules: []BotUserAgentRule{{Pattern: "x", Action: "throttle"}}},
		"header_rule": {Enabled: true, CustomHeaders: []BotHeaderRule{{Header: "X", Pattern: "y", Action: "throttle"}}},
	} {
		cfg := &Config{Middleware: &MiddlewareConfig{BotDetection: bd}}
		if err := cfg.validateBotDetection(); err == nil {
			t.Errorf("%s: expected throttle to be rejected", name)
		}
	}
	ok := &Config{Middleware: &MiddlewareConfig{BotDetection: &BotDetectionConfig{Action: "block"}}}
	if err := ok.validateBotDetection(); err != nil {
		t.Errorf("block should be valid: %v", err)
	}
}

func TestValidate_RejectsUnsupportedWAFRateLimit(t *testing.T) {
	mk := func(rl *WAFRateLimitConfig) *Config {
		return &Config{WAF: &WAFConfig{Enabled: true, RateLimit: rl}}
	}
	bad := []*WAFRateLimitConfig{
		{Enabled: true, Rules: []WAFRateLimitRule{{ID: "a", Action: "throttle"}}},
		{Enabled: true, Store: &RateLimitStoreConfig{Type: "redis"}},
	}
	for i, rl := range bad {
		if err := mk(rl).validateWAF(); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	good := &WAFRateLimitConfig{Enabled: true, Store: &RateLimitStoreConfig{Type: "memory"}, Rules: []WAFRateLimitRule{{ID: "a", Action: "block"}}}
	if err := mk(good).validateWAF(); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}
