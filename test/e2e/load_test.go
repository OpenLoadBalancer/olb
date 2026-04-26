package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/engine"
)

// isCI returns true when running in a CI environment.
func isCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}

// loadTestThreshold returns the error-rate threshold to use for load tests.
// In CI, thresholds are relaxed by 2x to account for resource contention.
func loadTestThreshold(basePercent float64) float64 {
	if isCI() {
		return basePercent * 2.0
	}
	return basePercent
}

// LoadTestResult holds the results of a load test run.
type LoadTestResult struct {
	TotalRequests    int64
	SuccessCount     int64
	ErrorCount       int64
	TotalDuration    time.Duration
	RequestsPerSec   float64
	AvgLatency       time.Duration
	MinLatency       time.Duration
	MaxLatency       time.Duration
	P50Latency       time.Duration
	P95Latency       time.Duration
	P99Latency       time.Duration
	BackendHits      map[string]int64
	ConcurrencyLevel int
}

// runLoadTest executes a load test against the given proxy address.
func runLoadTest(t *testing.T, proxyAddr string, concurrency int, totalRequests int64, useTLS bool, path ...string) *LoadTestResult {
	t.Helper()

	var successCount, errorCount atomic.Int64
	var latencySum atomic.Int64
	var minLatency, maxLatency atomic.Int64
	minLatency.Store(int64(time.Hour))
	maxLatency.Store(0)

	// Collect latency samples for percentile calculation
	latencyMu := sync.Mutex{}
	latencies := make([]time.Duration, 0, totalRequests)

	scheme := "http"
	transport := &http.Transport{
		MaxIdleConns:        concurrency * 2,
		MaxIdleConnsPerHost: concurrency * 2,
		IdleConnTimeout:     90 * time.Second,
	}
	if useTLS {
		scheme = "https"
		transport = &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"},
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
		}
	}

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	// Distribute requests across workers
	requestsPerWorker := totalRequests / int64(concurrency)
	extra := totalRequests % int64(concurrency)

	start := time.Now()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		count := requestsPerWorker
		if int64(w) < extra {
			count++
		}
		if count == 0 {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int64(0); i < count; i++ {
				reqStart := time.Now()
				reqPath := "/"
				if len(path) > 0 {
					reqPath = path[0]
				}
				resp, err := client.Get(fmt.Sprintf("%s://%s%s", scheme, proxyAddr, reqPath))
				elapsed := time.Since(reqStart)

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

				// Track latency
				latencyNs := elapsed.Nanoseconds()
				latencySum.Add(latencyNs)

				// Update min/max atomically
				for {
					current := minLatency.Load()
					if latencyNs >= current || minLatency.CompareAndSwap(current, latencyNs) {
						break
					}
				}
				for {
					current := maxLatency.Load()
					if latencyNs <= current || maxLatency.CompareAndSwap(current, latencyNs) {
						break
					}
				}

				latencyMu.Lock()
				latencies = append(latencies, elapsed)
				latencyMu.Unlock()
			}
		}()
	}

	wg.Wait()
	totalDuration := time.Since(start)

	succ := successCount.Load()
	errs := errorCount.Load()
	total := succ + errs

	result := &LoadTestResult{
		TotalRequests:    total,
		SuccessCount:     succ,
		ErrorCount:       errs,
		TotalDuration:    totalDuration,
		RequestsPerSec:   float64(total) / totalDuration.Seconds(),
		AvgLatency:       time.Duration(latencySum.Load() / max64(total, 1)),
		MinLatency:       time.Duration(minLatency.Load()),
		MaxLatency:       time.Duration(maxLatency.Load()),
		ConcurrencyLevel: concurrency,
	}

	// Calculate percentiles
	if len(latencies) > 0 {
		sorted := make([]time.Duration, len(latencies))
		copy(sorted, latencies)
		// Simple selection sort for small arrays, quicksort for large
		if len(sorted) > 1 {
			quickSort(sorted)
		}
		result.P50Latency = sorted[len(sorted)*50/100]
		result.P95Latency = sorted[len(sorted)*95/100]
		result.P99Latency = sorted[len(sorted)*99/100]
	}

	return result
}

func quickSort(a []time.Duration) {
	if len(a) < 2 {
		return
	}
	lo, hi := 0, len(a)-1
	pivot := len(a) / 2
	a[pivot], a[hi] = a[hi], a[pivot]
	for i := range a {
		if a[i] < a[hi] {
			a[i], a[lo] = a[lo], a[i]
			lo++
		}
	}
	a[lo], a[hi] = a[hi], a[lo]
	quickSort(a[:lo])
	quickSort(a[lo+1:])
}

func printLoadTestResult(t *testing.T, name string, r *LoadTestResult) {
	t.Helper()
	t.Logf("=== Load Test: %s (concurrency=%d) ===", name, r.ConcurrencyLevel)
	t.Logf("  Total requests: %d (%d success, %d errors)", r.TotalRequests, r.SuccessCount, r.ErrorCount)
	t.Logf("  Duration: %v", r.TotalDuration.Round(time.Millisecond))
	t.Logf("  RPS: %.0f", r.RequestsPerSec)
	t.Logf("  Latency: avg=%v min=%v max=%v", r.AvgLatency.Round(time.Microsecond), r.MinLatency.Round(time.Microsecond), r.MaxLatency.Round(time.Microsecond))
	t.Logf("  Percentiles: p50=%v p95=%v p99=%v", r.P50Latency.Round(time.Microsecond), r.P95Latency.Round(time.Microsecond), r.P99Latency.Round(time.Microsecond))
}

// setupLoadTestEnv creates a full load test environment with backends and proxy.
func setupLoadTestEnv(t *testing.T, numBackends int) (proxyAddr string, backendHits []*atomic.Int64, cleanup func()) {
	t.Helper()

	// Start backends
	backendHits = make([]*atomic.Int64, numBackends)
	backendAddrs := make([]string, numBackends)

	for i := 0; i < numBackends; i++ {
		hits := &atomic.Int64{}
		backendHits[i] = hits
		name := fmt.Sprintf("backend-%d", i)

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to start %s: %v", name, err)
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("X-Backend", name)
			fmt.Fprintf(w, "OK from %s", name)
		})
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			fmt.Fprint(w, "OK")
		})
		mux.HandleFunc("/load-test", func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(200)
			fmt.Fprint(w, "OK")
		})

		server := &http.Server{Handler: mux}
		go server.Serve(listener)

		backendAddrs[i] = listener.Addr().String()
		t.Cleanup(func() { server.Close() })
	}

	// Build config
	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	var backendYAML strings.Builder
	for _, addr := range backendAddrs {
		fmt.Fprintf(&backendYAML, "      - address: \"%s\"\n", addr)
	}

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: load-pool
pools:
  - name: load-pool
    algorithm: round_robin
    backends:
%s    health_check:
      type: http
      interval: 1s
      timeout: 500ms
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendYAML.String())

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

	addr := fmt.Sprintf("127.0.0.1:%d", proxyPH.Port())
	waitForReady(t, addr, 5*time.Second)
	waitForHealthyProxy(t, addr, 5*time.Second)

	cleanup = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	}
	t.Cleanup(cleanup)

	return addr, backendHits, cleanup
}

// TestLoadTest_100Concurrent tests 100 concurrent connections with 10K requests.
func TestLoadTest_100Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}
	if runtime.NumCPU() < 2 && isCI() {
		t.Skip("Skipping load test: insufficient CPUs in CI environment")
	}

	proxyAddr, backendHits, _ := setupLoadTestEnv(t, 3)

	result := runLoadTest(t, proxyAddr, 100, 10000, false)
	printLoadTestResult(t, "100 concurrent", result)

	// Allow small error rate due to single-machine resource contention
	errorRate := float64(result.ErrorCount) / float64(max64(result.TotalRequests, 1)) * 100
	threshold := loadTestThreshold(1.0)
	if errorRate > threshold {
		t.Errorf("Error rate %.2f%% exceeds %.0f%% threshold (%d errors out of %d, CI=%v)", errorRate, threshold, result.ErrorCount, result.TotalRequests, isCI())
	}

	if result.P99Latency > 100*time.Millisecond {
		t.Logf("WARNING: P99 latency %v exceeds 100ms target (may be CI noise)", result.P99Latency)
	}

	// Verify backend distribution
	totalHits := int64(0)
	for _, hits := range backendHits {
		totalHits += hits.Load()
	}
	if totalHits != result.SuccessCount {
		t.Logf("Backend total hits (%d) vs success count (%d) — may differ due to health checks", totalHits, result.SuccessCount)
	}

	t.Logf("Backend distribution: %d/%d/%d", backendHits[0].Load(), backendHits[1].Load(), backendHits[2].Load())
}

// TestLoadTest_250Concurrent tests 250 concurrent connections with 20K requests.
// This tests the proxy under moderate load on a single machine.
func TestLoadTest_250Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}
	if runtime.NumCPU() < 2 && isCI() {
		t.Skip("Skipping load test: insufficient CPUs in CI environment")
	}

	proxyAddr, backendHits, _ := setupLoadTestEnv(t, 5)

	result := runLoadTest(t, proxyAddr, 250, 20000, false)
	printLoadTestResult(t, "250 concurrent", result)

	// Allow up to 5% error rate at high concurrency (single-machine port exhaustion)
	errorRate := float64(result.ErrorCount) / float64(result.TotalRequests)
	threshold := loadTestThreshold(5.0) / 100.0
	if errorRate > threshold {
		t.Errorf("Error rate %.2f%% exceeds %.0f%% threshold (%d errors out of %d, CI=%v)", errorRate*100, threshold*100, result.ErrorCount, result.TotalRequests, isCI())
	}

	if result.P99Latency > 500*time.Millisecond {
		t.Logf("WARNING: P99 latency %v exceeds 500ms at 1K concurrency", result.P99Latency)
	}

	t.Logf("Backend distribution: %d/%d/%d/%d/%d",
		backendHits[0].Load(), backendHits[1].Load(), backendHits[2].Load(),
		backendHits[3].Load(), backendHits[4].Load())
}

// TestLoadTest_SustainedTraffic tests sustained traffic over 5 seconds.
func TestLoadTest_SustainedTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}
	if runtime.NumCPU() < 2 && isCI() {
		t.Skip("Skipping load test: insufficient CPUs in CI environment")
	}

	proxyAddr, _, _ := setupLoadTestEnv(t, 3)

	// Run for 5 seconds with 50 concurrent clients
	var totalSuccess, totalErrors atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < 50; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			for {
				select {
				case <-ctx.Done():
					return
				default:
					resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
					if err != nil {
						totalErrors.Add(1)
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 400 {
						totalSuccess.Add(1)
					} else {
						totalErrors.Add(1)
					}
				}
			}
		}()
	}

	wg.Wait()

	success := totalSuccess.Load()
	errors := totalErrors.Load()
	total := success + errors

	t.Logf("=== Sustained Traffic Test (5s, 50 workers) ===")
	t.Logf("  Total requests: %d (%d success, %d errors)", total, success, errors)
	t.Logf("  RPS: %.0f", float64(total)/5.0)
	t.Logf("  Error rate: %.2f%%", float64(errors)/float64(max64(total, 1))*100)

	if errors > 0 {
		errorRate := float64(errors) / float64(total) * 100
		threshold := loadTestThreshold(1.0)
		if errorRate > threshold {
			t.Errorf("Error rate %.2f%% exceeds %.0f%% threshold during sustained traffic (CI=%v)", errorRate, threshold, isCI())
		}
	}
}

// TestLoadTest_TLSThroughput tests TLS throughput with concurrent connections.
func TestLoadTest_TLSThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}
	if runtime.NumCPU() < 2 && isCI() {
		t.Skip("Skipping load test: insufficient CPUs in CI environment")
	}

	// Start backend
	var hits atomic.Int64
	backendAddr := startBackend(t, "tls-load-backend", &hits)

	// Generate TLS cert
	certPEM, keyPEM := generateSelfSignedCert(t, []string{"localhost"})
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, certPEM, 0644)
	os.WriteFile(keyFile, keyPEM, 0600)

	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
tls:
  cert_file: "%s"
  key_file: "%s"
listeners:
  - name: https
    address: "127.0.0.1:%d"
    protocol: http
    tls:
      enabled: true
    routes:
      - path: /
        pool: tls-load-pool
pools:
  - name: tls-load-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 500ms
      path: /health
`, adminPH.Port(), toForwardSlash(certFile), toForwardSlash(keyFile), proxyPH.Port(), backendAddr)

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
	waitForHealthyProxyTLS(t, proxyAddr, 5*time.Second)

	result := runLoadTest(t, proxyAddr, 50, 5000, true)
	printLoadTestResult(t, "TLS throughput (50 concurrent)", result)

	// TLS handshake overhead is expected to reduce throughput vs plain HTTP
	errorRate := float64(result.ErrorCount) / float64(max64(result.TotalRequests, 1))
	threshold := loadTestThreshold(5.0) / 100.0
	if errorRate > threshold {
		t.Errorf("TLS error rate %.2f%% exceeds %.0f%% threshold (CI=%v)", errorRate*100, threshold*100, isCI())
	}

	t.Logf("TLS throughput: %.0f RPS with %d concurrent connections", result.RequestsPerSec, result.ConcurrencyLevel)
}

// TestLoadTest_BackendFailureRecovery tests that the proxy recovers from backend failures.
func TestLoadTest_BackendFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}
	if runtime.NumCPU() < 2 && isCI() {
		t.Skip("Skipping load test: insufficient CPUs in CI environment")
	}

	// Start 3 backends, we'll kill one mid-test
	backendListeners := make([]net.Listener, 3)
	backendServers := make([]*http.Server, 3)
	backendAddrs := make([]string, 3)

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("fail-backend-%d", i)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to start %s: %v", name, err)
		}
		backendListeners[i] = listener
		backendAddrs[i] = listener.Addr().String()

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "OK from %s", name)
		})
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		})

		server := &http.Server{Handler: mux}
		backendServers[i] = server
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
        pool: fail-pool
pools:
  - name: fail-pool
    algorithm: round_robin
    backends:
      - address: "%s"
      - address: "%s"
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 500ms
      path: /health
`, adminPH.Port(), proxyPH.Port(), backendAddrs[0], backendAddrs[1], backendAddrs[2])

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

	// Phase 1: Send traffic with all backends up
	client := &http.Client{Timeout: 3 * time.Second}
	var phase1Success, phase1Errors atomic.Int64
	for i := 0; i < 100; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			phase1Errors.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			phase1Success.Add(1)
		} else {
			phase1Errors.Add(1)
		}
	}

	t.Logf("Phase 1 (all backends): %d success, %d errors", phase1Success.Load(), phase1Errors.Load())
	if phase1Errors.Load() > 0 {
		t.Errorf("Phase 1: Expected zero errors with all backends up, got %d", phase1Errors.Load())
	}

	// Phase 2: Kill backend 2
	backendServers[1].Close()
	t.Log("Killed backend-1, sending traffic during failover...")

	// Send traffic during failover (health check should detect failure within ~1s)
	var phase2Success, phase2Errors atomic.Int64
	for i := 0; i < 100; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			phase2Errors.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			phase2Success.Add(1)
		} else {
			phase2Errors.Add(1)
		}
	}

	t.Logf("Phase 2 (backend down): %d success, %d errors", phase2Success.Load(), phase2Errors.Load())

	// Phase 3: Wait for health check to detect failure, then send traffic
	waitForBackendDown(t, fmt.Sprintf("127.0.0.1:%d", adminPH.Port()), backendAddrs[1], 10*time.Second)

	var phase3Success, phase3Errors atomic.Int64
	for i := 0; i < 100; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/", proxyAddr))
		if err != nil {
			phase3Errors.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			phase3Success.Add(1)
		} else {
			phase3Errors.Add(1)
		}
	}

	t.Logf("Phase 3 (after failover): %d success, %d errors", phase3Success.Load(), phase3Errors.Load())
	if phase3Errors.Load() > 10 {
		t.Errorf("Phase 3: Expected near-zero errors after failover, got %d", phase3Errors.Load())
	}
}
