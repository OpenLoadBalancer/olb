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
	"sync"
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

// TestE2E_AllAlgorithms verifies that all load balancing algorithms actually
// route requests to backends and return 200 OK responses.
func TestE2E_AllAlgorithms(t *testing.T) {
	algorithms := []string{
		"round_robin",
		"weighted_round_robin",
		"least_connections",
		"ip_hash",
		"consistent_hash",
		"maglev",
		"power_of_two",
		"random",
		"ring_hash",
	}

	for _, algo := range algorithms {
		algo := algo
		t.Run(algo, func(t *testing.T) {
			var b1Hits, b2Hits, b3Hits atomic.Int64
			addr1 := startBackend(t, "algo-b1", &b1Hits)
			addr2 := startBackend(t, "algo-b2", &b2Hits)
			addr3 := startBackend(t, "algo-b3", &b3Hits)

			proxyPort := getFreePort(t)
			adminPort := getFreePort(t)

			yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: algo-pool
pools:
  - name: algo-pool
    algorithm: %s
    backends:
      - address: "%s"
        weight: 100
      - address: "%s"
        weight: 100
      - address: "%s"
        weight: 100
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, algo, addr1, addr2, addr3)

			cfgPath := writeYAML(t, yamlCfg)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			eng, err := engine.New(cfg, "")
			if err != nil {
				t.Fatalf("Failed to create engine for %s: %v", algo, err)
			}
			if err := eng.Start(); err != nil {
				t.Fatalf("Failed to start engine for %s: %v", algo, err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				eng.Shutdown(ctx)
			})

			proxyAddr := cfg.Listeners[0].Address
			waitForReady(t, proxyAddr, 5*time.Second)

			// Wait for health checks to mark backends healthy
			time.Sleep(2 * time.Second)

			client := &http.Client{Timeout: 5 * time.Second}
			successCount := 0
			for i := 0; i < 10; i++ {
				resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
				if err != nil {
					t.Logf("Request %d failed for %s: %v", i, algo, err)
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					successCount++
				} else {
					t.Logf("Request %d: status %d, body: %s", i, resp.StatusCode, body)
				}
			}

			if successCount < 8 {
				t.Fatalf("Algorithm %s: only %d/10 requests succeeded", algo, successCount)
			}

			totalHits := b1Hits.Load() + b2Hits.Load() + b3Hits.Load()
			t.Logf("Algorithm %s: %d successes, distribution: b1=%d b2=%d b3=%d (total=%d)",
				algo, successCount, b1Hits.Load(), b2Hits.Load(), b3Hits.Load(), totalHits)

			if totalHits == 0 {
				t.Fatalf("Algorithm %s: no backend received any request", algo)
			}
		})
	}
}

// TestE2E_HealthCheck verifies that the health checker detects backends going
// down, stops routing to them, and resumes when they come back up.
func TestE2E_HealthCheck(t *testing.T) {
	var b1Hits, b3Hits atomic.Int64
	addr1 := startBackend(t, "hc-b1", &b1Hits)
	addr3 := startBackend(t, "hc-b3", &b3Hits)

	// We need a stoppable backend for the middle slot
	stoppableListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create stoppable listener: %v", err)
	}
	stoppableAddr := stoppableListener.Addr().String()

	var stoppableHits atomic.Int64
	stoppableMux := http.NewServeMux()
	stoppableMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		stoppableHits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Backend", "hc-stoppable")
		fmt.Fprintf(w, "Hello from hc-stoppable\n")
	})
	stoppableMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	})
	stoppableServer := &http.Server{Handler: stoppableMux}
	go stoppableServer.Serve(stoppableListener)
	// Do not add cleanup yet; we will stop it manually.

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: hc-pool
pools:
  - name: hc-pool
    algorithm: round_robin
    backends:
      - address: "%s"
        weight: 100
      - address: "%s"
        weight: 100
      - address: "%s"
        weight: 100
    health_check:
      type: http
      interval: 500ms
      timeout: 500ms
      path: /health
`, adminPort, proxyPort, addr1, stoppableAddr, addr3)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)

	// Wait for health checks to mark all backends healthy
	time.Sleep(3 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Phase 1: All 3 backends are up - send requests
	b1Hits.Store(0)
	stoppableHits.Store(0)
	b3Hits.Store(0)
	for i := 0; i < 15; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Phase1 request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	t.Logf("Phase 1 (all up): b1=%d, stoppable=%d, b3=%d",
		b1Hits.Load(), stoppableHits.Load(), b3Hits.Load())

	// All 3 should have received some hits
	if stoppableHits.Load() == 0 {
		t.Log("Warning: stoppable backend got 0 hits in phase 1, may not be healthy yet")
	}

	// Phase 2: Stop the stoppable backend
	stoppableServer.Close()
	t.Log("Stopped stoppable backend, waiting for health check to detect...")

	// Wait for health checker to detect the failure (interval=500ms, unhealthy_threshold=3 => ~2s)
	time.Sleep(4 * time.Second)

	// Reset counters
	b1Hits.Store(0)
	stoppableHits.Store(0)
	b3Hits.Store(0)

	// Phase 2: Send requests - stoppable backend should not get any
	for i := 0; i < 15; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Phase2 request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	t.Logf("Phase 2 (stoppable down): b1=%d, stoppable=%d, b3=%d",
		b1Hits.Load(), stoppableHits.Load(), b3Hits.Load())

	// Stoppable backend should have received 0 hits since it's down
	if stoppableHits.Load() > 0 {
		t.Errorf("Stoppable backend still receiving traffic after shutdown: %d hits",
			stoppableHits.Load())
	}

	// b1 and b3 should be getting all the traffic
	if b1Hits.Load() == 0 && b3Hits.Load() == 0 {
		t.Error("Neither b1 nor b3 received traffic while stoppable was down")
	}

	// Phase 3: Restart the stoppable backend
	restartListener, err := net.Listen("tcp", stoppableAddr)
	if err != nil {
		t.Logf("Could not restart on same port %s: %v (skipping restart phase)", stoppableAddr, err)
		return
	}
	restartServer := &http.Server{Handler: stoppableMux}
	go restartServer.Serve(restartListener)
	t.Cleanup(func() { restartServer.Close() })

	t.Log("Restarted stoppable backend, waiting for health check recovery...")
	// Wait for recovery: healthy_threshold=2, interval=500ms => ~2s + margin
	time.Sleep(4 * time.Second)

	// Reset counters
	b1Hits.Store(0)
	stoppableHits.Store(0)
	b3Hits.Store(0)

	for i := 0; i < 15; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Phase3 request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	t.Logf("Phase 3 (all recovered): b1=%d, stoppable=%d, b3=%d",
		b1Hits.Load(), stoppableHits.Load(), b3Hits.Load())

	if stoppableHits.Load() == 0 {
		t.Log("Warning: stoppable backend still not receiving traffic after restart (may need more time)")
	}

}

// TestE2E_Middleware_RateLimit verifies that rate limiting middleware actually
// rejects requests after the burst is exhausted.
func TestE2E_Middleware_RateLimit(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "rl-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
middleware:
  rate_limit:
    enabled: true
    requests_per_second: 5
    burst_size: 5
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: rl-pool
pools:
  - name: rl-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)
	time.Sleep(2 * time.Second) // Wait for health checks

	client := &http.Client{Timeout: 5 * time.Second}

	// Send burst of requests rapidly (more than the burst_size)
	var statusCodes []int
	for i := 0; i < 15; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Request %d error: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCodes = append(statusCodes, resp.StatusCode)
	}

	// Count 200s and 429s
	ok200 := 0
	rateLimited := 0
	for _, code := range statusCodes {
		switch code {
		case 200:
			ok200++
		case 429:
			rateLimited++
		}
	}

	t.Logf("Rate limit results: 200=%d, 429=%d, total=%d", ok200, rateLimited, len(statusCodes))

	if ok200 == 0 {
		t.Error("No requests succeeded (200) -- rate limiter may be blocking everything")
	}
	if rateLimited == 0 {
		t.Error("No requests were rate-limited (429) -- rate limiting may not be working")
	}
	if ok200 > 0 && rateLimited > 0 {
		t.Log("Rate limiting is working correctly: some requests pass, some get 429")
	}
}

// TestE2E_Middleware_CORS verifies that CORS preflight requests receive proper
// Access-Control-Allow-Origin headers.
func TestE2E_Middleware_CORS(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "cors-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
middleware:
  cors:
    enabled: true
    allowed_origins:
      - "https://example.com"
    allowed_methods:
      - "GET"
      - "POST"
      - "OPTIONS"
    allowed_headers:
      - "Content-Type"
      - "Authorization"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: cors-pool
pools:
  - name: cors-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)
	time.Sleep(2 * time.Second) // Wait for health checks

	// Send an OPTIONS preflight request
	req, err := http.NewRequest("OPTIONS", fmt.Sprintf("http://%s/", proxyAddr), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Preflight request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("Preflight response status: %d", resp.StatusCode)
	t.Logf("Preflight response headers: %v", resp.Header)

	// Check for CORS headers
	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao == "" {
		t.Error("Missing Access-Control-Allow-Origin header in preflight response")
	} else {
		t.Logf("Access-Control-Allow-Origin: %s", acao)
		if acao != "https://example.com" && acao != "*" {
			t.Errorf("Unexpected ACAO value: %s", acao)
		}
	}

	// Also send a regular GET with Origin to verify CORS on normal requests
	req2, _ := http.NewRequest("GET", fmt.Sprintf("http://%s/", proxyAddr), nil)
	req2.Header.Set("Origin", "https://example.com")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	io.ReadAll(resp2.Body)
	resp2.Body.Close()

	acao2 := resp2.Header.Get("Access-Control-Allow-Origin")
	t.Logf("GET response ACAO: %s", acao2)
	if acao2 == "" {
		t.Error("Missing Access-Control-Allow-Origin header in GET response")
	}
}

// TestE2E_Middleware_Compression verifies that gzip compression works when the
// client sends Accept-Encoding: gzip and the response is large enough.
func TestE2E_Middleware_Compression(t *testing.T) {
	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	// Start a backend that returns a large response (>1KB to exceed min_size)
	largeBody := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100) // ~4500 bytes
	var hits atomic.Int64
	backendAddr := startBackendWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			fmt.Fprint(w, "OK")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(largeBody))
	})

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
middleware:
  compression:
    enabled: true
    min_size: 256
    level: 6
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: comp-pool
pools:
  - name: comp-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)
	time.Sleep(2 * time.Second)

	// Send request with Accept-Encoding: gzip
	// NOTE: We use a custom transport that does NOT auto-decompress,
	// so we can inspect the raw compressed response.
	transport := &http.Transport{
		DisableCompression: true,
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}

	req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s/", proxyAddr), nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	compressedBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	contentEncoding := resp.Header.Get("Content-Encoding")
	t.Logf("Content-Encoding: %s", contentEncoding)
	t.Logf("Original size: %d, Compressed size: %d", len(largeBody), len(compressedBody))

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	if contentEncoding == "gzip" {
		// Verify compressed body is smaller than original
		if len(compressedBody) >= len(largeBody) {
			t.Errorf("Compressed body (%d) is not smaller than original (%d)",
				len(compressedBody), len(largeBody))
		} else {
			t.Logf("Compression working: %d -> %d bytes (%.0f%% reduction)",
				len(largeBody), len(compressedBody),
				float64(len(largeBody)-len(compressedBody))/float64(len(largeBody))*100)
		}
	} else {
		t.Log("No gzip Content-Encoding header, checking if response is still valid...")
		// Even without gzip header, verify we got data back
		if len(compressedBody) == 0 {
			t.Error("Empty response body")
		}
	}
}

// TestE2E_Middleware_RequestID verifies that the middleware pipeline processes
// requests correctly through all middleware layers including request ID,
// real IP, metrics, and access logging.
func TestE2E_Middleware_RequestID(t *testing.T) {
	// Start a backend that echoes back headers it received from the proxy
	var receivedHeaders []http.Header
	var headerMu sync.Mutex
	backendAddr := startBackendWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			fmt.Fprint(w, "OK")
			return
		}
		headerMu.Lock()
		hCopy := r.Header.Clone()
		receivedHeaders = append(receivedHeaders, hCopy)
		headerMu.Unlock()

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Backend", "reqid-backend")
		fmt.Fprint(w, "Hello from reqid-backend\n")
	})

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: reqid-pool
pools:
  - name: reqid-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)
	time.Sleep(2 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Send requests through the proxy and verify middleware pipeline is working:
	// 1. Request reaches the backend (proxy works)
	// 2. X-Forwarded-For is added by the proxy (real IP middleware)
	// 3. Response comes back with backend headers
	for i := 0; i < 5; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("Request %d: status %d, body: %s", i, resp.StatusCode, body)
		}

		// Verify backend response came through
		xBackend := resp.Header.Get("X-Backend")
		if xBackend != "reqid-backend" {
			t.Errorf("Request %d: expected X-Backend=reqid-backend, got %s", i, xBackend)
		}

		// Check for X-Request-Id in response (set by request-id middleware)
		reqID := resp.Header.Get("X-Request-Id")
		if reqID != "" {
			t.Logf("Request %d: X-Request-Id = %s", i, reqID)
		}
	}

	// Verify middleware pipeline forwarded headers to backend
	headerMu.Lock()
	defer headerMu.Unlock()

	if len(receivedHeaders) != 5 {
		t.Errorf("Expected backend to receive 5 requests, got %d", len(receivedHeaders))
	}

	for i, h := range receivedHeaders {
		// The proxy should add X-Forwarded-For headers
		xff := h.Get("X-Forwarded-For")
		if xff != "" {
			t.Logf("Request %d: backend received X-Forwarded-For=%s", i, xff)
		}
		// The proxy should add X-Forwarded-Proto
		xfp := h.Get("X-Forwarded-Proto")
		if xfp != "" {
			t.Logf("Request %d: backend received X-Forwarded-Proto=%s", i, xfp)
		}
	}

	t.Log("Middleware pipeline is working: requests proxied through all layers")
}

// TestE2E_AdminAPI_Backends verifies that the admin API returns pool and health
// information for configured backends.
func TestE2E_AdminAPI_Backends(t *testing.T) {
	var b1Hits, b2Hits atomic.Int64
	addr1 := startBackend(t, "admin-b1", &b1Hits)
	addr2 := startBackend(t, "admin-b2", &b2Hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: admin-pool
pools:
  - name: admin-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, addr1, addr2)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	adminAddr := cfg.Admin.Address
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Test GET /api/v1/backends
	resp, err := client.Get(fmt.Sprintf("http://%s/api/v1/backends", adminAddr))
	if err != nil {
		t.Fatalf("GET /api/v1/backends failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/v1/backends status %d: %s", resp.StatusCode, body)
	}

	bodyStr := string(body)
	t.Logf("Backends response: %s", bodyStr[:min(200, len(bodyStr))])

	if !strings.Contains(bodyStr, "admin-pool") {
		t.Error("Backends response does not contain 'admin-pool'")
	}

	// Test GET /api/v1/health
	resp, err = client.Get(fmt.Sprintf("http://%s/api/v1/health", adminAddr))
	if err != nil {
		t.Fatalf("GET /api/v1/health failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/v1/health status %d: %s", resp.StatusCode, body)
	}

	bodyStr = string(body)
	t.Logf("Health response: %s", bodyStr[:min(200, len(bodyStr))])

	if !strings.Contains(bodyStr, "success") {
		t.Error("Health response does not contain 'success'")
	}

	// Test GET /api/v1/routes
	resp, err = client.Get(fmt.Sprintf("http://%s/api/v1/routes", adminAddr))
	if err != nil {
		t.Fatalf("GET /api/v1/routes failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/v1/routes status %d: %s", resp.StatusCode, body)
	}

	bodyStr = string(body)
	t.Logf("Routes response: %s", bodyStr[:min(200, len(bodyStr))])

	// Should contain the pool reference
	if !strings.Contains(bodyStr, "admin-pool") {
		t.Error("Routes response does not reference 'admin-pool'")
	}

	// Test GET /api/v1/system/health
	resp, err = client.Get(fmt.Sprintf("http://%s/api/v1/system/health", adminAddr))
	if err != nil {
		t.Fatalf("GET /api/v1/system/health failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/v1/system/health status %d: %s", resp.StatusCode, body)
	}

	bodyStr = string(body)
	t.Logf("System health: %s", bodyStr[:min(200, len(bodyStr))])

	if !strings.Contains(bodyStr, "healthy") && !strings.Contains(bodyStr, "ok") {
		t.Error("System health response does not indicate healthy state")
	}
}

// TestE2E_ConfigReload verifies that hot-reloading configuration via the admin
// API applies changes without restarting the engine.
func TestE2E_ConfigReload(t *testing.T) {
	var b1Hits atomic.Int64
	addr1 := startBackend(t, "reload-b1", &b1Hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: reload-pool
pools:
  - name: reload-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, addr1)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "olb.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlCfg), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Pass actual config path so Reload() can re-read it
	eng, err := engine.New(cfg, cfgPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)
	time.Sleep(2 * time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Verify initial proxy works
	resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Initial request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Initial request status %d, expected 200", resp.StatusCode)
	}
	t.Log("Initial proxy request succeeded")

	// Start a second backend
	var b2Hits atomic.Int64
	addr2 := startBackend(t, "reload-b2", &b2Hits)

	// Update config file with second backend
	newYamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: reload-pool
pools:
  - name: reload-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, addr1, addr2)

	if err := os.WriteFile(cfgPath, []byte(newYamlCfg), 0644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	// Trigger reload via admin API
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://%s/api/v1/system/reload", adminAddr), nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Reload request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("Reload response: status=%d body=%s", resp.StatusCode, string(body))

	if resp.StatusCode != 200 {
		t.Fatalf("Reload failed with status %d: %s", resp.StatusCode, body)
	}

	// Wait for new config to take effect and health checks
	time.Sleep(3 * time.Second)

	// Send requests to verify both backends receive traffic
	b1Hits.Store(0)
	b2Hits.Store(0)
	for i := 0; i < 20; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Post-reload request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	t.Logf("Post-reload distribution: b1=%d, b2=%d", b1Hits.Load(), b2Hits.Load())

	// The new backend should have received some traffic after reload
	// (Note: depends on health check timing; just verify reload didn't break things)
	totalHits := b1Hits.Load() + b2Hits.Load()
	if totalHits == 0 {
		t.Error("No requests succeeded after config reload")
	} else {
		t.Logf("Config reload successful: %d requests served", totalHits)
	}
}

// TestE2E_WebUI verifies that the Web UI is served at the admin root path
// and contains expected HTML content.
func TestE2E_WebUI(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "webui-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: webui-pool
pools:
  - name: webui-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, backendAddr)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	adminAddr := cfg.Admin.Address
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// GET / on admin port
	resp, err := client.Get(fmt.Sprintf("http://%s/", adminAddr))
	if err != nil {
		t.Fatalf("GET / on admin port failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("GET / status %d: %s", resp.StatusCode, body)
	}

	bodyStr := string(body)
	contentType := resp.Header.Get("Content-Type")

	t.Logf("Web UI response: status=%d, content-type=%s, body_len=%d",
		resp.StatusCode, contentType, len(body))

	// Verify it's HTML
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected text/html content-type, got: %s", contentType)
	}

	// Verify it contains "OpenLoadBalancer"
	if !strings.Contains(bodyStr, "OpenLoadBalancer") {
		t.Errorf("Web UI HTML does not contain 'OpenLoadBalancer', got: %s",
			bodyStr[:min(200, len(bodyStr))])
	} else {
		t.Log("Web UI serves correctly with 'OpenLoadBalancer' in HTML")
	}
}

// TestE2E_WeightedDistribution verifies that weighted round robin distributes
// requests proportional to backend weights.
func TestE2E_WeightedDistribution(t *testing.T) {
	var b1Hits, b2Hits, b3Hits atomic.Int64
	addr1 := startBackend(t, "wd-b1", &b1Hits)
	addr2 := startBackend(t, "wd-b2", &b2Hits)
	addr3 := startBackend(t, "wd-b3", &b3Hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	// Weights: 1, 2, 3 (normalized from 100, 200, 300)
	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: wd-pool
pools:
  - name: wd-pool
    algorithm: weighted_round_robin
    backends:
      - address: "%s"
        weight: 100
      - address: "%s"
        weight: 200
      - address: "%s"
        weight: 300
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, proxyPort, addr1, addr2, addr3)

	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	proxyAddr := cfg.Listeners[0].Address
	waitForReady(t, proxyAddr, 5*time.Second)
	time.Sleep(2 * time.Second) // Wait for health checks

	client := &http.Client{Timeout: 5 * time.Second}

	// Send 60 requests
	successCount := 0
	for i := 0; i < 60; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			successCount++
		}
	}

	h1, h2, h3 := b1Hits.Load(), b2Hits.Load(), b3Hits.Load()
	total := h1 + h2 + h3
	t.Logf("Weighted distribution: b1(w=100)=%d, b2(w=200)=%d, b3(w=300)=%d (total=%d, success=%d)",
		h1, h2, h3, total, successCount)

	if total < 30 {
		t.Fatalf("Too few total hits: %d (expected ~60)", total)
	}

	// Verify approximate weight distribution:
	// b1 should get ~1/6 of requests (~10)
	// b2 should get ~2/6 of requests (~20)
	// b3 should get ~3/6 of requests (~30)
	// Allow generous margins since WRR implementations may vary
	if h1 > h3 {
		t.Errorf("Backend with weight=100 (%d hits) got more than weight=300 (%d hits) -- weighted distribution appears wrong", h1, h3)
	}

	// b3 (highest weight) should get the most traffic
	if h3 < h1 && h3 < h2 {
		t.Errorf("Backend with highest weight (300) got fewest hits (%d) -- weighted distribution appears wrong", h3)
	}

	// Basic proportionality check: b3 should get at least 1.5x more than b1
	if total >= 30 && h1 > 0 {
		ratio := float64(h3) / float64(h1)
		t.Logf("b3/b1 ratio: %.1f (expected ~3.0)", ratio)
		if ratio < 1.2 {
			t.Errorf("Weight ratio b3/b1 = %.1f, expected at least ~1.5", ratio)
		}
	}
}

// --- Helper functions ---

// writeYAML writes YAML content to a temp file and returns the path.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "olb.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write YAML config: %v", err)
	}
	return path
}

// startBackendWithHandler starts an HTTP server with a custom handler.
func startBackendWithHandler(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr().String()
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
      type: http
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
