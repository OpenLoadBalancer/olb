package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

// The shadow manager was built from config but never handed to the proxy,
// and shadow.targets were never added, so mirroring did nothing.
func TestShadow_MirrorsToTargetPool(t *testing.T) {
	var primaryHits, shadowHits atomic.Int64
	// Count only proxied requests, not the health checker's /health probes.
	count := func(c *atomic.Int64) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				c.Add(1)
			}
		}
	}
	primary := httptest.NewServer(count(&primaryHits))
	defer primary.Close()
	shadow := httptest.NewServer(count(&shadowHits))
	defer shadow.Close()

	cfg := &config.Config{
		Version: "1",
		Admin:   &config.Admin{Address: "127.0.0.1:0"},
		Listeners: []*config.Listener{{
			Name: "http", Address: "127.0.0.1:0", Protocol: "http",
			Routes: []*config.Route{{Path: "/", Pool: "primary"}},
		}},
		Pools: []*config.Pool{
			{Name: "primary", Algorithm: "round_robin", Backends: []*config.Backend{{Address: strings.TrimPrefix(primary.URL, "http://")}}},
			{Name: "mirror", Algorithm: "round_robin", Backends: []*config.Backend{{Address: strings.TrimPrefix(shadow.URL, "http://")}}},
		},
		Shadow: &config.ShadowConfig{
			Enabled:    true,
			Percentage: 100,
			Targets:    []config.ShadowTarget{{Pool: "mirror", Percentage: 100}},
		},
	}
	eng, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	resp, err := http.Get("http://" + eng.listeners[0].Address() + "/mirrored")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for shadowHits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if primaryHits.Load() != 1 {
		t.Errorf("primary hits = %d, want 1", primaryHits.Load())
	}
	if shadowHits.Load() != 1 {
		t.Errorf("shadow hits = %d, want 1 (request was not mirrored)", shadowHits.Load())
	}
}
