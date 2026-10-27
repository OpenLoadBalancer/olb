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
