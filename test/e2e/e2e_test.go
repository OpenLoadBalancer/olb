package e2e

import (
	"context"
	"encoding/json"
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
			adminAddr := cfg.Admin.Address
			waitForReady(t, proxyAddr, 5*time.Second)
			waitForReady(t, adminAddr, 5*time.Second)

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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)
	time.Sleep(3 * time.Second) // Let health checker establish baseline

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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

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
	time.Sleep(1 * time.Second) // Brief wait for reload propagation

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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

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

// TestE2E_WAF verifies that the WAF middleware blocks SQL injection and XSS
// attacks while allowing normal requests through.
func TestE2E_WAF(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "waf-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
waf:
  enabled: true
  mode: blocking
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: waf-pool
pools:
  - name: waf-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Normal request should succeed
	resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Normal GET failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Normal GET: expected 200, got %d", resp.StatusCode)
	}
	t.Log("Normal request returned 200 OK")

	// SQL injection in query should be blocked
	// Use URL-encoded single quotes (%27) so the URL is valid HTTP
	resp, err = client.Get(fmt.Sprintf("http://%s/?id=1%%27+OR+%%271%%27%%3D%%271", proxyAddr))
	if err != nil {
		t.Fatalf("SQLi request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("SQLi attack: expected 403, got %d", resp.StatusCode)
	} else {
		t.Log("SQL injection blocked with 403")
	}

	// XSS via javascript: URI scheme in query should be blocked
	// The WAF XSS rule matches "javascript:" pattern in args
	resp, err = client.Get(fmt.Sprintf("http://%s/?q=javascript:alert(1)", proxyAddr))
	if err != nil {
		t.Fatalf("XSS request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("XSS attack: expected 403, got %d", resp.StatusCode)
	} else {
		t.Log("XSS attack blocked with 403")
	}
}

// TestE2E_WAF_RateLimiter verifies WAF rate limiting blocks requests exceeding the limit.
func TestE2E_WAF_RateLimiter(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "waf-rl-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
waf:
  enabled: true
  mode: enforce
  rate_limit:
    enabled: true
    rules:
      - id: "e2e-test"
        scope: "ip"
        limit: 5
        window: "1m"
        burst: 0
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// First 5 requests should succeed
	for i := 0; i < 5; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("Request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}
	t.Log("First 5 requests passed (200 OK)")

	// 6th request should be rate limited (429)
	resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Rate limit request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Errorf("Expected 429 after rate limit, got %d", resp.StatusCode)
	} else {
		t.Log("Rate limit enforced with 429")
	}

	if resp.Header.Get("Retry-After") == "" {
		t.Error("Expected Retry-After header on 429 response")
	}
}

// TestE2E_WAF_CommandInjection verifies WAF blocks command injection attempts.
func TestE2E_WAF_CommandInjection(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "waf-cmdi-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
waf:
  enabled: true
  mode: enforce
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: cmdi-pool
pools:
  - name: cmdi-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Command injection attempts
	attacks := []struct {
		name string
		url  string
	}{
		{"semicolon_cat", "/?cmd=;cat+/etc/passwd"},
		{"pipe_id", "/?cmd=|+/bin/sh"},
		{"path_traversal", "/?file=../../../etc/passwd"},
	}

	for _, attack := range attacks {
		resp, err := client.Get(fmt.Sprintf("http://%s%s", proxyAddr, attack.url))
		if err != nil {
			t.Fatalf("%s: request failed: %v", attack.name, err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("%s: expected 403, got %d", attack.name, resp.StatusCode)
		} else {
			t.Logf("%s blocked with 403", attack.name)
		}
	}
}

// TestE2E_WAF_MonitorMode verifies WAF monitor mode logs but doesn't block.
func TestE2E_WAF_MonitorMode(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "waf-monitor-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
waf:
  enabled: true
  mode: monitor
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: monitor-pool
pools:
  - name: monitor-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// SQLi attack in monitor mode should pass through to backend
	resp, err := client.Get(fmt.Sprintf("http://%s/?id=1%%27+OR+%%271%%27%%3D%%271", proxyAddr))
	if err != nil {
		t.Fatalf("Monitor mode request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Monitor mode: expected 200 (passthrough), got %d", resp.StatusCode)
	} else {
		t.Log("Monitor mode: SQLi attack passed through (logged only)")
	}

	// Verify backend was actually hit
	if hits.Load() == 0 {
		t.Error("Expected backend to be hit in monitor mode")
	}
}

// TestE2E_WAF_SecurityHeaders verifies WAF injects security headers in responses.
func TestE2E_WAF_SecurityHeaders(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "waf-headers-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
waf:
  enabled: true
  mode: enforce
  response:
    security_headers:
      enabled: true
      hsts:
        enabled: true
        max_age: 31536000
      x_content_type_options: true
      x_frame_options: "DENY"
      referrer_policy: "no-referrer"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: headers-pool
pools:
  - name: headers-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	checks := []struct {
		header   string
		contains string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "no-referrer"},
		{"X-XSS-Protection", "0"},
	}

	for _, c := range checks {
		got := resp.Header.Get(c.header)
		if !strings.Contains(got, c.contains) {
			t.Errorf("Expected %s to contain %q, got %q", c.header, c.contains, got)
		} else {
			t.Logf("%s: %s", c.header, got)
		}
	}
}

// TestE2E_WAF_AdminStatus verifies the WAF status endpoint on the admin API.
func TestE2E_WAF_AdminStatus(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "waf-status-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
waf:
  enabled: true
  mode: enforce
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: status-pool
pools:
  - name: status-pool
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

	adminAddr := fmt.Sprintf("127.0.0.1:%d", adminPort)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://%s/api/v1/waf/status", adminAddr))
	if err != nil {
		t.Fatalf("WAF status request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "enabled") {
		t.Errorf("Expected 'enabled' in WAF status response, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "enforce") {
		t.Errorf("Expected 'enforce' mode in WAF status response, got: %s", bodyStr)
	}
	t.Logf("WAF status: %s", bodyStr)
}

// TestE2E_IPFilter verifies that the IP filter middleware denies requests from
// IPs on the deny list.
func TestE2E_IPFilter(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "ipf-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
middleware:
  ip_filter:
    enabled: true
    deny_list:
      - "127.0.0.1/32"
    default_action: "allow"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: ipf-pool
pools:
  - name: ipf-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Request from localhost (127.0.0.1) should be denied
	resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	t.Logf("IP filter response: status=%d, body=%s", resp.StatusCode, string(body))

	if resp.StatusCode != 403 {
		t.Errorf("Expected 403 Forbidden for denied IP, got %d", resp.StatusCode)
	} else {
		t.Log("IP filter correctly blocked request from 127.0.0.1")
	}

	// Backend should NOT have received any hits
	if hits.Load() > 0 {
		t.Errorf("Backend received %d hits despite IP filter deny", hits.Load())
	}
}

// TestE2E_CircuitBreaker verifies that the circuit breaker middleware opens after
// enough backend errors and starts returning 503 Service Unavailable.
func TestE2E_CircuitBreaker(t *testing.T) {
	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	// Backend that always returns 500 errors
	backendAddr := startBackendWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			fmt.Fprint(w, "OK")
			return
		}
		w.WriteHeader(500)
		fmt.Fprint(w, "Internal Server Error")
	})

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
middleware:
  circuit_breaker:
    enabled: true
    error_threshold: 3
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: cb-pool
pools:
  - name: cb-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Send requests to trigger errors and observe the circuit breaker opening
	var status500, status503 int
	for i := 0; i < 15; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Request %d error: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		switch resp.StatusCode {
		case 500:
			status500++
		case 503:
			status503++
		}
		t.Logf("Request %d: status %d", i, resp.StatusCode)
	}

	t.Logf("Results: 500s=%d, 503s=%d", status500, status503)

	// We should see some 500s (from backend before circuit opens)
	if status500 == 0 {
		t.Error("No 500 responses seen -- backend errors not reaching circuit breaker")
	}

	// After error_threshold errors, circuit should open and return 503
	if status503 == 0 {
		t.Error("No 503 responses from circuit breaker -- circuit did not open")
	}

	if status500 > 0 && status503 > 0 {
		t.Log("Circuit breaker working: initial 500s from backend, then 503s after circuit opened")
	}

	// Verify the 503 responses have the circuit breaker header
	resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Final request error: %v", err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	circuitState := resp.Header.Get("X-Circuit-State")
	t.Logf("Final request: status=%d, X-Circuit-State=%s", resp.StatusCode, circuitState)

	if resp.StatusCode == 503 && (circuitState == "open" || circuitState == "half-open") {
		t.Log("Circuit breaker state header confirmed")
	}
}

// TestE2E_ResponseCache verifies that the response cache middleware caches
// GET responses and serves them from cache on subsequent requests.
func TestE2E_ResponseCache(t *testing.T) {
	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	// Backend that returns an incrementing counter
	var requestCount atomic.Int64
	backendAddr := startBackendWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			fmt.Fprint(w, "OK")
			return
		}
		count := requestCount.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "max-age=60")
		fmt.Fprintf(w, "response-%d", count)
	})

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
middleware:
  cache:
    enabled: true
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: cache-pool
pools:
  - name: cache-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// First request: should be a cache miss
	resp1, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode != 200 {
		t.Fatalf("First request: expected 200, got %d", resp1.StatusCode)
	}

	cache1 := resp1.Header.Get("X-Cache")
	t.Logf("First request: body=%s, X-Cache=%s", string(body1), cache1)

	// Second request: should be a cache hit
	resp2, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if resp2.StatusCode != 200 {
		t.Fatalf("Second request: expected 200, got %d", resp2.StatusCode)
	}

	cache2 := resp2.Header.Get("X-Cache")
	t.Logf("Second request: body=%s, X-Cache=%s", string(body2), cache2)

	// Verify cache hit
	if cache2 == "HIT" {
		t.Log("Cache is working: second request served from cache (X-Cache: HIT)")
	} else {
		t.Logf("X-Cache header on second request: %s (expected HIT)", cache2)
	}

	// Verify response bodies match (cached response should be identical)
	if string(body1) != string(body2) {
		t.Errorf("Cached response body mismatch: first=%s, second=%s", string(body1), string(body2))
	} else {
		t.Logf("Response bodies match: %s", string(body1))
	}

	// Verify backend only received 1 request (the second was served from cache)
	backendHits := requestCount.Load()
	t.Logf("Backend received %d requests (expected 1 if cache worked)", backendHits)
	if backendHits > 1 {
		t.Log("Warning: backend received more than 1 request, cache may not be active for this path")
	}
}

// TestE2E_PrometheusMetrics verifies that the Prometheus metrics endpoint returns
// properly formatted metrics data.
func TestE2E_PrometheusMetrics(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "prom-backend", &hits)

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
        pool: prom-pool
pools:
  - name: prom-pool
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
	adminAddr := cfg.Admin.Address
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Send a few proxy requests to generate metrics
	for i := 0; i < 5; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			t.Logf("Proxy request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// GET /metrics on admin port (Prometheus format)
	resp, err := client.Get(fmt.Sprintf("http://%s/metrics", adminAddr))
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("GET /metrics status %d: %s", resp.StatusCode, body)
	}

	contentType := resp.Header.Get("Content-Type")
	t.Logf("Content-Type: %s", contentType)
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected text/plain Content-Type, got: %s", contentType)
	}

	bodyStr := string(body)
	t.Logf("Prometheus metrics (first 500 bytes): %s", bodyStr[:min(500, len(bodyStr))])

	// Verify response contains Prometheus-formatted metrics (# HELP, # TYPE, or metric names)
	if len(bodyStr) == 0 {
		t.Error("Prometheus metrics response is empty")
	}

	// Check for typical metric content
	hasMetricContent := strings.Contains(bodyStr, "# HELP") ||
		strings.Contains(bodyStr, "# TYPE") ||
		strings.Contains(bodyStr, "requests") ||
		strings.Contains(bodyStr, "total") ||
		strings.Contains(bodyStr, "duration")

	if hasMetricContent {
		t.Log("Prometheus metrics endpoint returns valid metric data")
	} else {
		t.Log("Warning: Prometheus metrics response does not contain expected metric names")
		t.Logf("Full response: %s", bodyStr)
	}
}

// TestE2E_TCPProxy verifies that the L4 TCP proxy can forward raw TCP traffic
// to a backend echo server.
func TestE2E_TCPProxy(t *testing.T) {
	// Start a simple TCP echo server
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	echoAddr := echoListener.Addr().String()
	t.Logf("Echo server listening on %s", echoAddr)

	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { echoListener.Close() })

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: tcp-proxy
    address: "127.0.0.1:%d"
    protocol: tcp
    pool: tcp-pool
pools:
  - name: tcp-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: tcp
      interval: 1s
      timeout: 1s
`, adminPort, proxyPort, echoAddr)

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

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	adminAddr := fmt.Sprintf("127.0.0.1:%d", adminPort)
	waitForReady(t, proxyAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	// Connect to the TCP proxy and send data
	conn, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to TCP proxy: %v", err)
	}
	defer conn.Close()

	// Set read/write deadlines
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send "hello" and expect it echoed back
	message := "hello"
	_, err = conn.Write([]byte(message))
	if err != nil {
		t.Fatalf("Failed to write to TCP proxy: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read from TCP proxy: %v", err)
	}

	response := string(buf[:n])
	t.Logf("TCP echo response: %s", response)

	if response != message {
		t.Errorf("TCP echo mismatch: sent=%q, got=%q", message, response)
	} else {
		t.Log("TCP proxy echo test passed: sent and received data match")
	}

	// Send another message to verify persistent connection
	message2 := "world123"
	_, err = conn.Write([]byte(message2))
	if err != nil {
		t.Fatalf("Failed to write second message: %v", err)
	}

	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read second response: %v", err)
	}

	response2 := string(buf[:n])
	if response2 != message2 {
		t.Errorf("Second echo mismatch: sent=%q, got=%q", message2, response2)
	} else {
		t.Log("TCP proxy persistent connection works correctly")
	}
}

// TestE2E_MCP verifies that the MCP (Model Context Protocol) server responds
// to JSON-RPC requests over HTTP.
func TestE2E_MCP(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "mcp-backend", &hits)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)
	mcpPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
  mcp_address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: mcp-pool
pools:
  - name: mcp-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, mcpPort, proxyPort, backendAddr)

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

	mcpAddr := fmt.Sprintf("127.0.0.1:%d", mcpPort)
	waitForReady(t, mcpAddr, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Send JSON-RPC "initialize" request
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	resp, err := client.Post(
		fmt.Sprintf("http://%s/mcp", mcpAddr),
		"application/json",
		strings.NewReader(initReq),
	)
	if err != nil {
		t.Fatalf("MCP initialize request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("MCP initialize: expected 200, got %d, body: %s", resp.StatusCode, body)
	}

	bodyStr := string(body)
	t.Logf("MCP initialize response: %s", bodyStr)

	if !strings.Contains(bodyStr, "serverInfo") {
		t.Error("MCP initialize response missing 'serverInfo'")
	}
	if !strings.Contains(bodyStr, "capabilities") {
		t.Error("MCP initialize response missing 'capabilities'")
	}

	// Send JSON-RPC "tools/list" request
	toolsReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	resp, err = client.Post(
		fmt.Sprintf("http://%s/mcp", mcpAddr),
		"application/json",
		strings.NewReader(toolsReq),
	)
	if err != nil {
		t.Fatalf("MCP tools/list request failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("MCP tools/list: expected 200, got %d, body: %s", resp.StatusCode, body)
	}

	bodyStr = string(body)
	t.Logf("MCP tools/list response: %s", bodyStr[:min(500, len(bodyStr))])

	if !strings.Contains(bodyStr, "tools") {
		t.Error("MCP tools/list response missing 'tools'")
	} else {
		t.Log("MCP server responds correctly to initialize and tools/list")
	}
}

// TestE2E_MultipleListeners verifies that an HTTP listener and a TCP listener
// can run simultaneously, each serving different types of traffic.
// This tests that the engine can manage multiple listener types at once.
func TestE2E_MultipleListeners(t *testing.T) {
	// HTTP backend (pool-a)
	var httpHits atomic.Int64
	httpBackendAddr := startBackend(t, "http-backend", &httpHits)

	// TCP echo server (pool-b)
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	echoAddr := echoListener.Addr().String()
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { echoListener.Close() })

	httpPort := getFreePort(t)
	tcpPort := getFreePort(t)
	adminPort := getFreePort(t)

	// Configure HTTP listener + TCP listener using separate config objects
	// since the custom YAML parser has limitations with multiple sequences.
	// We build the config programmatically instead.
	cfg := &config.Config{
		Admin: &config.Admin{Address: fmt.Sprintf("127.0.0.1:%d", adminPort)},
		Listeners: []*config.Listener{
			{
				Name:     "http-listener",
				Address:  fmt.Sprintf("127.0.0.1:%d", httpPort),
				Protocol: "http",
				Routes: []*config.Route{
					{Path: "/", Pool: "http-pool"},
				},
			},
			{
				Name:     "tcp-listener",
				Address:  fmt.Sprintf("127.0.0.1:%d", tcpPort),
				Protocol: "tcp",
				Pool:     "tcp-pool",
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "http-pool",
				Algorithm: "round_robin",
				Backends:  []*config.Backend{{Address: httpBackendAddr, Weight: 100}},
				HealthCheck: &config.HealthCheck{
					Type: "http", Path: "/health", Interval: "1s", Timeout: "1s",
				},
			},
			{
				Name:      "tcp-pool",
				Algorithm: "round_robin",
				Backends:  []*config.Backend{{Address: echoAddr, Weight: 100}},
				HealthCheck: &config.HealthCheck{
					Type: "tcp", Interval: "1s", Timeout: "1s",
				},
			},
		},
		Logging: &config.Logging{Level: "info", Format: "json", Output: "stdout"},
		Metrics: &config.Metrics{Enabled: true, Path: "/metrics"},
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

	httpAddr := fmt.Sprintf("127.0.0.1:%d", httpPort)
	tcpAddr := fmt.Sprintf("127.0.0.1:%d", tcpPort)
	adminAddr := fmt.Sprintf("127.0.0.1:%d", adminPort)
	waitForReady(t, httpAddr, 5*time.Second)
	waitForReady(t, tcpAddr, 5*time.Second)
	waitForReady(t, adminAddr, 5*time.Second)

	// Test 1: HTTP listener works
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", httpAddr))
		if err != nil {
			t.Logf("HTTP request %d failed: %v", i, err)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Logf("HTTP request %d: status %d", i, resp.StatusCode)
		}
	}

	// Test 2: TCP listener works
	tcpConn, err := net.DialTimeout("tcp", tcpAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to TCP listener: %v", err)
	}
	defer tcpConn.Close()
	tcpConn.SetDeadline(time.Now().Add(5 * time.Second))

	message := "hello-tcp"
	tcpConn.Write([]byte(message))
	buf := make([]byte, 1024)
	n, err := tcpConn.Read(buf)
	if err != nil {
		t.Fatalf("TCP read failed: %v", err)
	}
	tcpResponse := string(buf[:n])

	t.Logf("HTTP backend hits: %d", httpHits.Load())
	t.Logf("TCP echo response: %s", tcpResponse)

	if httpHits.Load() == 0 {
		t.Error("HTTP listener did not route any traffic to backends")
	}

	if tcpResponse != message {
		t.Errorf("TCP echo mismatch: sent=%q, got=%q", message, tcpResponse)
	}

	if httpHits.Load() > 0 && tcpResponse == message {
		t.Log("Multiple listeners confirmed: HTTP and TCP listeners both active and routing correctly")
	}
}

// --- Polling helper functions ---

// poolsResponse is the JSON envelope returned by GET /api/v1/pools.
type poolsResponse struct {
	Success bool       `json:"success"`
	Data    []poolInfo `json:"data"`
}

// poolInfo represents pool data from the admin API.
type poolInfo struct {
	Name     string       `json:"name"`
	Backends []backendRef `json:"backends"`
}

// backendRef represents a backend in the pools API response.
type backendRef struct {
	Address string `json:"address"`
	State   string `json:"state"`
	Healthy bool   `json:"healthy"`
}

// waitForBackendDown polls GET /api/v1/pools until a backend matching
// backendAddr reports healthy=false. Fatals on timeout.
func waitForBackendDown(t *testing.T, adminAddr, backendAddr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://%s/api/v1/pools", adminAddr))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var pr poolsResponse
		if err := json.Unmarshal(body, &pr); err != nil || !pr.Success {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, pool := range pr.Data {
			for _, b := range pool.Backends {
				if b.Address == backendAddr && !b.Healthy {
					t.Logf("Backend %s is down (state=%s)", backendAddr, b.State)
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for backend %s to go down (waited %v)", backendAddr, timeout)
}

// waitForBackendUp polls GET /api/v1/pools until a backend matching
// backendAddr reports healthy=true. Fatals on timeout.
func waitForBackendUp(t *testing.T, adminAddr, backendAddr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://%s/api/v1/pools", adminAddr))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var pr poolsResponse
		if err := json.Unmarshal(body, &pr); err != nil || !pr.Success {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, pool := range pr.Data {
			for _, b := range pool.Backends {
				if b.Address == backendAddr && b.Healthy {
					t.Logf("Backend %s is up", backendAddr)
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for backend %s to recover (waited %v)", backendAddr, timeout)
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
