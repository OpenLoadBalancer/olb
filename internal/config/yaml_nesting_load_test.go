package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Before the YAML parser fix, rate_limit here leaked to the document root
// (after the deeper ip_filter.allow_list block) and was silently ignored,
// and the unquoted CIDR was split into junk tokens.
func TestLoad_YAMLMiddlewareSiblingAfterList(t *testing.T) {
	src := `listeners:
  - name: http
    address: ":8080"
    protocol: http
    routes:
      - path: /
        pool: p
pools:
  - name: p
    algorithm: round_robin
    backends:
      - address: "127.0.0.1:9000"
middleware:
  ip_filter:
    enabled: true
    allow_list:
      - 10.0.0.0/8
  rate_limit:
    enabled: true
    requests_per_second: 100
`
	path := filepath.Join(t.TempDir(), "olb.yaml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Middleware == nil || cfg.Middleware.RateLimit == nil || !cfg.Middleware.RateLimit.Enabled {
		t.Fatalf("middleware.rate_limit was lost: %+v", cfg.Middleware)
	}
	if got := cfg.Middleware.IPFilter.AllowList; len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Errorf("ip_filter.allow_list = %q, want [10.0.0.0/8]", got)
	}
}
