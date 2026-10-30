package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPoolDiscovery_FileProviderFeedsPool(t *testing.T) {
	file := filepath.Join(t.TempDir(), "backends.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"backends":[{"address":"127.0.0.1:19001"}]}`)

	cfg := createTestConfig()
	cfg.Pools[0].Discovery = &config.PoolDiscovery{
		Provider: "file",
		Options:  map[string]string{"path": file, "poll_interval": "100ms"},
	}
	e, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Shutdown(t.Context())

	addrs := func() map[string]bool {
		out := map[string]bool{}
		for _, b := range e.poolManager.GetPool("test-pool").GetAllBackends() {
			out[b.Address] = true
		}
		return out
	}

	waitFor(t, "discovered backend to appear", func() bool { return addrs()["127.0.0.1:19001"] })
	if !addrs()["127.0.0.1:8081"] {
		t.Error("static backend must stay")
	}

	// A reload swaps in fresh pools; discovery must refill them.
	if err := e.applyConfig(cfg); err != nil {
		t.Fatalf("reload: %v", err)
	}
	waitFor(t, "discovered backend after reload", func() bool { return addrs()["127.0.0.1:19001"] })

	write(`{"backends":[{"address":"127.0.0.1:19002"}]}`)
	waitFor(t, "backend swap", func() bool { a := addrs(); return a["127.0.0.1:19002"] && !a["127.0.0.1:19001"] })
	if !addrs()["127.0.0.1:8081"] {
		t.Error("static backend must survive discovery changes")
	}
}

func TestPoolDiscovery_ConfigValidation(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].Discovery = &config.PoolDiscovery{Provider: "etcd"}
	if err := cfg.Validate(); err == nil {
		t.Error("unknown discovery provider must be rejected")
	}
	cfg.Pools[0].Discovery = &config.PoolDiscovery{Provider: "file", Refresh: "10ms"}
	if err := cfg.Validate(); err == nil {
		t.Error("refresh below 1s must be rejected")
	}
	cfg.Pools[0].Discovery = &config.PoolDiscovery{Provider: "file", Refresh: "5s"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid discovery config rejected: %v", err)
	}
}
