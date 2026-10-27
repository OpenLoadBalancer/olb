package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

// Reload rebuilt the L7 proxy with hard-coded 60s/10s/3 and no idle-conn
// settings, silently dropping server.proxy_timeout, dial_timeout,
// max_retries and the idle pool settings until the next restart.
func TestApplyConfig_KeepsServerProxySettings(t *testing.T) {
	withServer := func() *config.Config {
		cfg := createTestConfig()
		cfg.Server = &config.ServerConfig{
			ProxyTimeout:        "7s",
			DialTimeout:         "2s",
			MaxRetries:          5,
			MaxIdleConns:        42,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     "33s",
		}
		return cfg
	}
	cfg := withServer()
	eng, err := New(cfg, createTempConfigFile(t, cfg))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := eng.applyConfig(withServer()); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	p := reflect.ValueOf(eng.proxy).Elem()
	durations := map[string]time.Duration{"proxyTimeout": 7 * time.Second, "dialTimeout": 2 * time.Second, "idleConnTimeout": 33 * time.Second}
	for field, want := range durations {
		if got := time.Duration(p.FieldByName(field).Int()); got != want {
			t.Errorf("after reload %s = %v, want %v", field, got, want)
		}
	}
	ints := map[string]int64{"maxRetries": 5, "maxIdleConns": 42, "maxIdleConnsPerHost": 4}
	for field, want := range ints {
		if got := p.FieldByName(field).Int(); got != want {
			t.Errorf("after reload %s = %d, want %d", field, got, want)
		}
	}
}

func TestInitializePools_RejectsUnknownAlgorithm(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].Algorithm = "lest_connections"
	eng, err := New(cfg, "")
	if err != nil {
		return // rejected early: fine
	}
	if err := eng.initializePools(); err == nil {
		t.Fatal("initializePools accepted an unknown balancing algorithm; it used to fall back to round robin silently")
	}
}
