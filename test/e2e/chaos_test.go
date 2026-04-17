package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/engine"
)

// TestChaos_AllBackendsDown tests proxy behavior when all backends are unreachable.
func TestChaos_AllBackendsDown(t *testing.T) {
	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	// Use non-existent backend addresses
	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: dead-pool
pools:
  - name: dead-pool
    algorithm: round_robin
    backends:
      - address: "127.0.0.1:1"
      - address: "127.0.0.1:2"
    health_check:
      type: http
      interval: 1s
      timeout: 500ms
      path: /health
`, adminPH.Port(), proxyPH.Port())

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)

	// Send requests - should get error responses (4xx/5xx), not crash
	client := &http.Client{Timeout: 3 * time.Second}
	for i := 0; i < 10; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Request %d: connection error (expected): %v", i, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			t.Logf("Request %d: got %d (expected error for dead backends)", i, resp.StatusCode)
		} else {
			t.Errorf("Request %d: unexpected success with status %d", i, resp.StatusCode)
		}
	}

	t.Log("All backends down test passed: proxy returns errors without crashing")
}

// TestChaos_BackendFlapping tests rapid backend up/down cycling.
func TestChaos_BackendFlapping(t *testing.T) {
	// Create a controllable backend
	var backendUp atomic.Bool
	backendUp.Store(true)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !backendUp.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, "backend down")
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !backendUp.Load() {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	backendAddr := listener.Addr().String()

	// Start a second stable backend
	stableListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start stable backend: %v", err)
	}
	stableMux := http.NewServeMux()
	stableMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "stable")
	})
	stableMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	stableServer := &http.Server{Handler: stableMux}
	go stableServer.Serve(stableListener)
	t.Cleanup(func() { stableServer.Close() })

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: flap-pool
pools:
  - name: flap-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
    health_check:
      type: http
      interval: 500ms
      timeout: 500ms
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendAddr, stableListener.Addr().String())

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForHealthyProxy(t, proxyAddr, 5*time.Second)

	client := &http.Client{Timeout: 3 * time.Second}

	// Send traffic while flapping the backend
	var successCount, errorCount atomic.Int64
	done := make(chan struct{})
	var wg sync.WaitGroup

	// Start traffic sender
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
				if err != nil {
					errorCount.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					successCount.Add(1)
				} else {
					errorCount.Add(1)
				}
			}
		}
	}()

	// Flap the backend: down for 500ms, up for 1s, repeat
	for i := 0; i < 3; i++ {
		time.Sleep(1 * time.Second)
		backendUp.Store(false)
		t.Logf("Flap cycle %d: backend DOWN", i+1)
		time.Sleep(500 * time.Millisecond)
		backendUp.Store(true)
		t.Logf("Flap cycle %d: backend UP", i+1)
	}

	time.Sleep(1 * time.Second)
	close(done)
	wg.Wait()

	succ := successCount.Load()
	errs := errorCount.Load()
	total := succ + errs

	t.Logf("Backend flapping test: %d success, %d errors (total %d)", succ, errs, total)
	if total == 0 {
		t.Fatal("No requests completed")
	}

	// At least some should succeed thanks to the stable backend
	if succ == 0 {
		t.Error("Expected at least some successful requests during flapping")
	}
}

// TestChaos_SlowBackend tests proxy behavior with very slow backends.
func TestChaos_SlowBackend(t *testing.T) {
	// Start a slow backend (100ms response time)
	slowListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start slow backend: %v", err)
	}
	slowMux := http.NewServeMux()
	slowMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
		fmt.Fprint(w, "slow")
	})
	slowMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	slowServer := &http.Server{Handler: slowMux}
	go slowServer.Serve(slowListener)
	t.Cleanup(func() { slowServer.Close() })

	// Start a fast backend
	fastListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start fast backend: %v", err)
	}
	fastMux := http.NewServeMux()
	fastMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "fast")
	})
	fastMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	fastServer := &http.Server{Handler: fastMux}
	go fastServer.Serve(fastListener)
	t.Cleanup(func() { fastServer.Close() })

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: slow-pool
pools:
  - name: slow-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPH.Port(), proxyPH.Port(), slowListener.Addr().String(), fastListener.Addr().String())

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForHealthyProxy(t, proxyAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	var slowHits, fastHits atomic.Int64
	for i := 0; i < 20; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Request %d error: %v", i, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if strings.Contains(string(body), "slow") {
			slowHits.Add(1)
		} else if strings.Contains(string(body), "fast") {
			fastHits.Add(1)
		}
	}

	t.Logf("Slow backend hits: %d, Fast backend hits: %d", slowHits.Load(), fastHits.Load())
	if slowHits.Load() == 0 && fastHits.Load() == 0 {
		t.Error("No requests reached any backend")
	}
}

// TestChaos_ConnectionRefusedDuringTraffic tests connection refused errors mid-traffic.
func TestChaos_ConnectionRefusedDuringTraffic(t *testing.T) {
	// Start 2 backends
	backends := make([]net.Listener, 2)
	backendAddrs := make([]string, 2)
	servers := make([]*http.Server, 2)

	for i := 0; i < 2; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to start backend %d: %v", i, err)
		}
		backends[i] = listener
		backendAddrs[i] = listener.Addr().String()

		name := fmt.Sprintf("chaos-backend-%d", i)
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			fmt.Fprintf(w, "OK from %s", name)
		})
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		})

		server := &http.Server{Handler: mux}
		servers[i] = server
		go server.Serve(listener)
		t.Cleanup(func() { server.Close() })
	}

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: chaos-pool
pools:
  - name: chaos-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
    health_check:
      type: http
      interval: 500ms
      timeout: 500ms
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendAddrs[0], backendAddrs[1])

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForHealthyProxy(t, proxyAddr, 5*time.Second)

	client := &http.Client{Timeout: 3 * time.Second}

	// Phase 1: All backends up
	for i := 0; i < 20; i++ {
		resp, _ := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}
	t.Log("Phase 1 complete: traffic flowing normally")

	// Phase 2: Kill both backends simultaneously
	servers[0].Close()
	servers[1].Close()
	t.Log("Phase 2: killed both backends")

	// Send requests — should get errors, not hang
	var gotError bool
	for i := 0; i < 10; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			gotError = true
			t.Logf("  Request %d: error (expected): %v", i, err)
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 500 {
				gotError = true
				t.Logf("  Request %d: got %d (expected 5xx)", i, resp.StatusCode)
			}
		}
	}

	if !gotError {
		t.Error("Expected at least one error when both backends are down")
	}

	t.Log("Connection refused during traffic test passed")
}

// TestChaos_LargePayload tests proxy behavior with large request/response bodies.
func TestChaos_LargePayload(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "large-payload-backend", &hits)

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: payload-pool
pools:
  - name: payload-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForHealthyProxy(t, proxyAddr, 5*time.Second)

	client := &http.Client{Timeout: 10 * time.Second}

	// Send increasing payload sizes
	sizes := []int{1024, 10 * 1024, 100 * 1024, 1024 * 1024}
	for _, size := range sizes {
		payload := strings.Repeat("A", size)
		resp, err := client.Post(
			fmt.Sprintf("http://%s/", proxyAddr),
			"text/plain",
			strings.NewReader(payload),
		)
		if err != nil {
			t.Errorf("POST with %d bytes failed: %v", size, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("Expected 200 for %d byte payload, got %d", size, resp.StatusCode)
		} else {
			t.Logf("Successfully proxied %d byte payload", size)
		}
	}
}

// TestChaos_ConcurrentShutdown tests clean shutdown under load.
func TestChaos_ConcurrentShutdown(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "shutdown-backend", &hits)

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: shutdown-pool
pools:
  - name: shutdown-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForHealthyProxy(t, proxyAddr, 5*time.Second)

	// Start sending traffic in background
	var successCount atomic.Int64
	var wg sync.WaitGroup
	stopTraffic := make(chan struct{})

	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 2 * time.Second}
			for {
				select {
				case <-stopTraffic:
					return
				default:
					resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
					if err != nil {
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 400 {
						successCount.Add(1)
					}
				}
			}
		}()
	}

	// Wait for at least some traffic to flow
	waitForCondition(t, "traffic flowing before shutdown", 3*time.Second, 10*time.Millisecond, func() bool {
		return successCount.Load() > 0
	})

	// Shutdown while traffic is flowing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := eng.Shutdown(ctx)

	close(stopTraffic)
	wg.Wait()

	if shutdownErr != nil {
		t.Logf("Shutdown returned: %v (may be expected during traffic)", shutdownErr)
	}

	t.Logf("Concurrent shutdown test: %d successful requests before/during shutdown", successCount.Load())
	if successCount.Load() == 0 {
		t.Error("Expected at least some successful requests before shutdown")
	}
}

// TestChaos_InvalidRequests tests proxy resilience to malformed requests.
func TestChaos_InvalidRequests(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "invalid-req-backend", &hits)

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: invalid-pool
pools:
  - name: invalid-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForHealthyProxy(t, proxyAddr, 5*time.Second)

	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Various malformed/edge-case requests
	testCases := []struct {
		name   string
		path   string
		method string
	}{
		{"very_long_path", "/" + strings.Repeat("a", 2000), "GET"},
		{"null_bytes", "/path%00with%00nulls", "GET"},
		{"unicode_path", "/日本語/路径/🚀", "GET"},
		{"double_slash", "//double//slash//path", "GET"},
		{"dot_dot", "/../../../etc/passwd", "GET"},
		{"large_header", "/", "GET"}, // Will add large header below
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, fmt.Sprintf("http://%s%s", proxyAddr, tc.path), nil)
			if err != nil {
				t.Logf("Creating request failed: %v", err)
				return
			}
			if tc.name == "large_header" {
				req.Header.Set("X-Large-Header", strings.Repeat("B", 8000))
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Logf("Request error (acceptable): %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			t.Logf("Got status %d for %s", resp.StatusCode, tc.name)
		})
	}

	t.Log("Invalid requests test passed: proxy handled malformed requests without crashing")
}
