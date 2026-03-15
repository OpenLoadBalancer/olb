package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/engine"
)

// TestFullE2E_ProxyActuallyWorks is the ultimate test:
// Start real backends, start the engine, send HTTP requests, verify responses.
func TestFullE2E_ProxyActuallyWorks(t *testing.T) {
	// --- Step 1: Start 3 backend HTTP servers ---
	var backend1Hits, backend2Hits, backend3Hits atomic.Int64

	b1 := startBackend(t, "backend-1", &backend1Hits)
	b2 := startBackend(t, "backend-2", &backend2Hits)
	b3 := startBackend(t, "backend-3", &backend3Hits)

	t.Logf("Backends: %s, %s, %s", b1, b2, b3)

	// --- Step 2: Create config ---
	cfgPath := writeConfig(t, b1, b2, b3)
	t.Logf("Config: %s", cfgPath)

	// --- Step 3: Load config and create engine ---
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "") // empty path disables config file watcher
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// --- Step 4: Start engine ---
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	// Wait for listener to be ready
	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)

	// --- Step 5: Send requests through the proxy ---
	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < 30; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("Request %d: status %d, body: %s", i, resp.StatusCode, body)
		}

		bodyStr := string(body)
		if !strings.Contains(bodyStr, "backend-") {
			t.Fatalf("Request %d: unexpected body: %s", i, bodyStr)
		}
	}

	// --- Step 6: Verify round-robin distribution ---
	total := backend1Hits.Load() + backend2Hits.Load() + backend3Hits.Load()
	t.Logf("Distribution: b1=%d, b2=%d, b3=%d (total=%d)",
		backend1Hits.Load(), backend2Hits.Load(), backend3Hits.Load(), total)

	if total != 30 {
		t.Errorf("Expected 30 total hits, got %d", total)
	}

	// Each backend should get ~10 requests (round robin)
	for name, hits := range map[string]int64{
		"backend-1": backend1Hits.Load(),
		"backend-2": backend2Hits.Load(),
		"backend-3": backend3Hits.Load(),
	} {
		if hits < 5 || hits > 15 {
			t.Errorf("%s got %d hits, expected ~10 (round robin)", name, hits)
		}
	}

	// --- Step 7: Test admin API ---
	adminAddr := cfg.Admin.Address
	resp, err := client.Get(fmt.Sprintf("http://%s/api/v1/system/info", adminAddr))
	if err != nil {
		t.Fatalf("Admin API failed: %v", err)
	}
	adminBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Admin API status %d: %s", resp.StatusCode, adminBody)
	}
	if !strings.Contains(string(adminBody), "version") {
		t.Errorf("Admin API response missing version: %s", adminBody)
	}

	t.Logf("Admin API OK: %s", string(adminBody)[:min(100, len(adminBody))])

	// --- Step 8: Test metrics endpoint ---
	resp, err = client.Get(fmt.Sprintf("http://%s/api/v1/metrics", adminAddr))
	if err != nil {
		t.Fatalf("Metrics API failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Metrics status %d", resp.StatusCode)
	}

	t.Log("=== E2E TEST PASSED: Proxy works end-to-end! ===")
}

// startBackend starts a simple HTTP server that responds with its name.
func startBackend(t *testing.T, name string, hits *atomic.Int64) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start %s: %v", name, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Backend", name)
		fmt.Fprintf(w, "Hello from %s\n", name)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	return listener.Addr().String()
}

// writeConfig creates a minimal YAML config for testing.
func writeConfig(t *testing.T, b1, b2, b3 string) string {
	t.Helper()

	// Find free ports for proxy and admin
	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yaml := fmt.Sprintf(`global:
  workers: 2

admin:
  address: "127.0.0.1:%d"

listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: test-pool

pools:
  - name: test-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
      - address: "%s"
    health_check:
      enabled: true
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, b1, b2, b3)

	dir := t.TempDir()
	path := filepath.Join(dir, "olb.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	return path
}

func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("getFreePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func waitForReady(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for %s to be ready", addr)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
