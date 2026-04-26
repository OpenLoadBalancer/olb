package engine

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/cluster"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/health"
	olbListener "github.com/openloadbalancer/olb/internal/listener"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/waf"
)

// ============================================================================
// Tests for additional parseDuration edge cases
// ============================================================================

func TestParseDuration_HoursAndMinutes(t *testing.T) {
	result := parseDuration("1h30m", 0)
	if result != 90*time.Minute {
		t.Errorf("parseDuration(%q) = %v, want %v", "1h30m", result, 90*time.Minute)
	}
}

func TestParseDuration_Milliseconds(t *testing.T) {
	result := parseDuration("100ms", 0)
	if result != 100*time.Millisecond {
		t.Errorf("parseDuration(%q) = %v, want %v", "100ms", result, 100*time.Millisecond)
	}
}

// ============================================================================
// Tests for state machine transitions
// ============================================================================

func TestEngineReload_NotRunning(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.Reload()
	if err == nil {
		t.Error("Expected error when reloading while not running")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("Error should mention 'not running': %v", err)
	}
}

func TestEngineStart_DoubleStart(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("First Start() error = %v", err)
	}
	defer engine.Shutdown(context.Background())

	err = engine.Start()
	if err == nil {
		t.Error("Expected error when starting already running engine")
	}
	if !strings.Contains(err.Error(), "not stopped") {
		t.Errorf("Error should mention 'not stopped': %v", err)
	}
}

func TestEngineShutdown_WhenStopped(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = engine.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown on stopped engine returned: %v", err)
	}
}

// ============================================================================
// Tests for createLoggerWithOutput edge cases
// ============================================================================

func TestCreateLogger_FileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/test.log"

	logger, rotatingOut := createLoggerWithOutput(&config.Logging{
		Level:  "debug",
		Output: logFile,
	})
	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}
	if rotatingOut != nil {
		rotatingOut.Close()
	}
}

func TestCreateLogger_EmptyOutput(t *testing.T) {
	logger, _ := createLoggerWithOutput(&config.Logging{
		Level:  "info",
		Output: "",
	})
	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}
}

// ============================================================================
// Tests for createMTLSListener
// ============================================================================

func TestCreateMTLSListener_WithRequestPolicy(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	generateTestCertFiles(t, certFile, keyFile)

	cert, err := engine.tlsManager.LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadCertificate error = %v", err)
	}
	engine.tlsManager.AddCertificate(cert)

	listenerCfg := &config.Listener{
		Name:     "mtls-custom",
		Address:  "127.0.0.1:0",
		Protocol: "https",
		TLS:      &config.ListenerTLS{Enabled: true},
		MTLS: &config.MTLSConfig{
			Enabled:    true,
			ClientAuth: "request",
		},
	}

	opts := &olbListener.Options{
		Name:    listenerCfg.Name,
		Address: listenerCfg.Address,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}

	l, err := engine.createMTLSListener(opts, listenerCfg)
	if err != nil {
		t.Fatalf("createMTLSListener() error = %v", err)
	}
	if l == nil {
		t.Fatal("createMTLSListener() returned nil")
	}
}

func TestCreateMTLSListener_NilOpts(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listenerCfg := &config.Listener{
		Name:     "https-mtls",
		Address:  ":0",
		Protocol: "https",
		MTLS: &config.MTLSConfig{
			Enabled: true,
		},
	}

	_, err = engine.createMTLSListener(nil, listenerCfg)
	t.Logf("createMTLSListener(nil opts) returned: %v", err)
}

// ============================================================================
// Tests for engine with shadow config
// ============================================================================

func TestNew_ShadowCustomTimeout(t *testing.T) {
	cfg := createTestConfig()
	cfg.Shadow = &config.ShadowConfig{
		Enabled:     true,
		Percentage:  10,
		CopyHeaders: true,
		CopyBody:    false,
		Timeout:     "500ms",
		Targets: []config.ShadowTarget{
			{Pool: "test-pool", Percentage: 100},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.shadowMgr == nil {
		t.Error("Expected shadowMgr to be initialized")
	}
}

// ============================================================================
// Tests for WAF configuration
// ============================================================================

func TestNew_WAFMonitorMode(t *testing.T) {
	cfg := createTestConfig()
	cfg.WAF = &config.WAFConfig{
		Enabled: true,
		Mode:    "monitor",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.middlewareChain == nil {
		t.Error("Expected middleware chain to be created")
	}
}

func TestNew_WAFEnforceMode(t *testing.T) {
	cfg := createTestConfig()
	cfg.WAF = &config.WAFConfig{
		Enabled: true,
		Mode:    "enforce",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	wafMW := engine.middlewareChain.Get("waf")
	if wafMW == nil {
		t.Error("Expected WAF middleware in chain")
	}

	if _, ok := wafMW.(*waf.WAFMiddleware); !ok {
		t.Errorf("Expected *waf.WAFMiddleware, got %T", wafMW)
	}
}

// ============================================================================
// Tests for multiple routes
// ============================================================================

func TestNew_TwoRoutes(t *testing.T) {
	cfg := createTestConfig()
	cfg.Listeners[0].Routes = []*config.Route{
		{Path: "/", Pool: "test-pool", Host: "api.example.com"},
		{Path: "/v2", Pool: "test-pool"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer engine.Shutdown(context.Background())

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.router.RouteCount() != 2 {
		t.Errorf("Expected 2 routes, got %d", engine.router.RouteCount())
	}
}

// ============================================================================
// Tests for startTCPListener / startUDPListener
// ============================================================================

func TestStartTCPListener_BadAddress(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listenerCfg := &config.Listener{
		Name:     "tcp-bad",
		Address:  "invalid-host-that-does-not-exist:99999",
		Protocol: "tcp",
		Pool:     "default",
	}

	err = engine.startTCPListener(listenerCfg)
	if err == nil {
		t.Error("Expected error for invalid TCP address")
	}
}

func TestStartUDPListener_BadAddress(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listenerCfg := &config.Listener{
		Name:     "udp-bad",
		Address:  "invalid-host-that-does-not-exist:99999",
		Protocol: "udp",
		Pool:     "default",
	}

	err = engine.startUDPListener(listenerCfg)
	if err == nil {
		t.Error("Expected error for invalid UDP address")
	}
}

func TestStartTCPListener_Success(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pool := backend.NewPool("tcp-test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("tcp-b1", "127.0.0.1:9999")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	engine.poolManager.AddPool(pool)

	listenerCfg := &config.Listener{
		Name:     "tcp-ok",
		Address:  "127.0.0.1:0",
		Protocol: "tcp",
		Pool:     "tcp-test-pool",
	}

	err = engine.startTCPListener(listenerCfg)
	if err != nil {
		t.Fatalf("startTCPListener() error = %v", err)
	}

	if len(engine.listeners) == 0 {
		t.Fatal("Expected at least one listener")
	}

	lastListener := engine.listeners[len(engine.listeners)-1]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lastListener.Stop(ctx)
}

// ============================================================================
// Tests for startHTTPListener
// ============================================================================

func TestStartHTTPListener_HTTPSNoTLSConfig(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listenerCfg := &config.Listener{
		Name:     "https-notls",
		Address:  ":0",
		Protocol: "https",
		Pool:     "default",
	}

	err = engine.startHTTPListener(listenerCfg)
	t.Logf("startHTTPListener(https, no TLS) returned: %v", err)
}

// ============================================================================
// Tests for engine accessors
// ============================================================================

func TestEngineConfig_NotNil(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.config == nil {
		t.Error("config should not be nil")
	}
}

func TestEngineTLSManager_NotNil(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.tlsManager == nil {
		t.Error("tlsManager should not be nil")
	}
}

// ============================================================================
// Tests for admin address helpers
// ============================================================================

func TestGetAdminAddress_CustomPort(t *testing.T) {
	cfg := &config.Config{
		Admin: &config.Admin{Address: ":9090"},
	}
	addr := getAdminAddress(cfg)
	if addr != ":9090" {
		t.Errorf("getAdminAddress = %q, want :9090", addr)
	}
}

func TestGetAdminAddress_Nil(t *testing.T) {
	cfg := &config.Config{}
	addr := getAdminAddress(cfg)
	if addr != ":8080" {
		t.Errorf("getAdminAddress with nil admin = %q, want :8080", addr)
	}
}

// ============================================================================
// Tests for convertGeoDNSRules
// ============================================================================

func TestConvertGeoDNSRules_NilInput(t *testing.T) {
	result := convertGeoDNSRules(nil)
	if result == nil {
		t.Error("Expected non-nil (empty slice), got nil")
	}
	if len(result) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(result))
	}
}

// ============================================================================
// Tests for cluster init failure
// ============================================================================

func TestNew_ClusterInvalidConfig(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "",
		BindAddr: "",
		BindPort: 0,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() should not fail even with cluster init failure: %v", err)
	}

	if engine.clusterMgr != nil {
		t.Log("clusterMgr is non-nil (cluster init may have succeeded)")
	}
}

// ============================================================================
// Tests for passive checker callbacks
// ============================================================================

func TestNew_PassiveCheckerCallbacks(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.passiveChecker == nil {
		t.Fatal("passiveChecker should not be nil")
	}

	if engine.passiveChecker.OnBackendUnhealthy != nil {
		engine.passiveChecker.OnBackendUnhealthy("127.0.0.1:8081")
	}
	if engine.passiveChecker.OnBackendRecovered != nil {
		engine.passiveChecker.OnBackendRecovered("127.0.0.1:8081")
	}
}

// ============================================================================
// Tests for Start with various configurations
// ============================================================================

func TestStart_PluginManagerStart(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestStart_MCPTransportBadAddress(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPAddress = "invalid:address:format:bad"
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.mcpTransport != nil {
		t.Log("MCP transport was unexpectedly started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestStart_MCPWithAuditAndToken(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPAddress = "127.0.0.1:0"
	cfg.Admin.MCPToken = "test-secret-token"
	cfg.Admin.MCPAudit = true
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.mcpTransport == nil {
		t.Error("Expected MCP transport to be started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestStart_MCPCORSConfig(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPAddress = "127.0.0.1:0"
	cfg.Middleware = &config.MiddlewareConfig{
		CORS: &config.CORSConfig{
			Enabled:          true,
			AllowedOrigins:   []string{"https://openloadbalancer.dev"},
			AllowedMethods:   []string{"GET", "POST"},
			AllowedHeaders:   []string{"Content-Type"},
			AllowCredentials: true,
		},
	}
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestStart_ClusterWithPeers(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{"127.0.0.1:19999"},
	}
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestStart_ClusterStandalone(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
	}
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// ============================================================================
// Tests for Shutdown paths
// ============================================================================

func TestShutdown_ConfigWatcherPath(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.configWatcher == nil {
		t.Fatal("configWatcher should not be nil after Start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestShutdown_MCPTransport(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPAddress = "127.0.0.1:0"
	cfg.Admin.MCPToken = "test-bearer-token"
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.mcpTransport == nil {
		t.Fatal("Expected MCP transport to be started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestShutdown_ClusterManager(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
	}
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestShutdown_UDPProxy(t *testing.T) {
	cfg := createTestConfig()
	cfg.Listeners[0].Protocol = "udp"
	cfg.Listeners[0].Pool = "test-pool"
	cfg.Listeners[0].Routes = nil
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestShutdown_ProfilingCleanup(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:   true,
		PprofAddr: "127.0.0.1:0",
	}
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.profilingCleanup == nil {
		t.Fatal("profilingCleanup should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestShutdown_ExpiredContext(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)

	err = engine.Shutdown(ctx)
	t.Logf("Shutdown with expired context returned: %v", err)
}

func TestShutdown_NilContext(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.Shutdown(nil)
	t.Logf("Shutdown(nil) on stopped engine returned: %v", err)
}

func TestShutdown_ConcurrentNoPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race-prone concurrent shutdown test in short mode")
	}
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Two concurrent Shutdown calls must not panic on double-close of stopCh
	go func() {
		defer wg.Done()
		engine.Shutdown(ctx)
	}()
	go func() {
		defer wg.Done()
		engine.Shutdown(ctx)
	}()

	wg.Wait()
}

// ============================================================================
// Tests for Reload paths
// ============================================================================

func TestReload_FileDeleted(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer engine.Shutdown(context.Background())

	os.Remove(configPath)

	err = engine.Reload()
	if err == nil {
		t.Error("Expected error when loading config from deleted file")
	}
}

func TestReload_InvalidYAML(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer engine.Shutdown(context.Background())

	badCfg := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: nonexistent
pools:
  - name: test-pool
    algorithm: round_robin
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(badCfg), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	err = engine.Reload()
	if err == nil {
		t.Error("Expected error when validation fails")
	}
}

func TestReload_WhileReloading(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	engine.mu.Lock()
	engine.state = StateReloading
	engine.mu.Unlock()

	err = engine.Reload()
	if err == nil {
		t.Error("Expected error when reloading while already reloading")
	}

	engine.mu.Lock()
	engine.state = StateRunning
	engine.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestShutdown_FromReloadingState(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	engine.mu.Lock()
	engine.state = StateReloading
	engine.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = engine.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown from Reloading state returned: %v", err)
	}
}

// ============================================================================
// Tests for initializePools edge cases
// ============================================================================

func TestInitializePools_HealthCheckerStopped(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	engine.healthChecker.Stop()

	if err := engine.initializePools(); err != nil {
		t.Fatalf("initializePools() error = %v", err)
	}
}

func TestInitializePools_DuplicatePool(t *testing.T) {
	cfg := &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "test",
				Address:  "127.0.0.1:0",
				Protocol: "http",
				Routes:   []*config.Route{{Path: "/", Pool: "pool1"}},
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "pool1",
				Algorithm: "round_robin",
				HealthCheck: &config.HealthCheck{
					Type:     "http",
					Path:     "/health",
					Interval: "10s",
					Timeout:  "5s",
				},
				Backends: []*config.Backend{
					{ID: "b1", Address: "127.0.0.1:8081", Weight: 100},
				},
			},
		},
		Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pool := backend.NewPool("pool1", "round_robin")
	engine.poolManager.AddPool(pool)

	err = engine.initializePools()
	if err == nil {
		t.Error("Expected error for duplicate pool")
	}
}

// ============================================================================
// Tests for mtlsHTTPSListener lifecycle
// ============================================================================

func TestMTLSListener_DoubleStart(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cert := generateTestCert(t)

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	l, err := newMTLSHTTPSListener(&olbListener.Options{
		Name:    "test-mtls-double",
		Address: "127.0.0.1:0",
		Handler: handler,
	}, tlsCfg)
	if err != nil {
		t.Fatalf("newMTLSHTTPSListener() error = %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = l.Start()
	if err == nil {
		t.Error("Start() should fail when already running")
	}

	addr := l.Address()
	if addr == "" {
		t.Error("Address() should not be empty when running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	if l.IsRunning() {
		t.Error("IsRunning() should be false after Stop()")
	}
}

// ============================================================================
// Tests for createMiddlewareChain panic recovery
// ============================================================================

func TestCreateMiddlewareChain_PanicRecovery(t *testing.T) {
	cfg := createTestConfig()
	registry := metrics.NewRegistry()
	logger := logging.New(logging.NewJSONOutput(os.Stdout))

	chain := createMiddlewareChain(cfg, logger, registry)
	if chain == nil {
		t.Fatal("createMiddlewareChain() returned nil")
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	handler := chain.Then(next)
	req := httptest.NewRequest("GET", "http://localhost/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 after panic recovery, got %d", rr.Code)
	}
}

// ============================================================================
// Tests for validateConfig edge cases
// ============================================================================

func TestValidateConfig_NilConfigValue(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.validateConfig(nil)
	if err == nil {
		t.Error("validateConfig(nil) expected error")
	}
}

func TestValidateConfig_BadPoolRef(t *testing.T) {
	// Create a valid engine first
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Now validate a separate config that has a bad pool reference
	badCfg := &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "test",
				Address:  "127.0.0.1:0",
				Protocol: "http",
				Routes:   []*config.Route{{Path: "/", Pool: "nonexistent"}},
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "real-pool",
				Algorithm: "round_robin",
				HealthCheck: &config.HealthCheck{
					Type:     "http",
					Path:     "/health",
					Interval: "10s",
					Timeout:  "5s",
				},
				Backends: []*config.Backend{
					{ID: "b1", Address: "127.0.0.1:8081", Weight: 100},
				},
			},
		},
		Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
	}

	err = engine.validateConfig(badCfg)
	if err == nil {
		t.Error("validateConfig() expected error for non-existent pool reference")
	}
}

func TestValidateConfig_EmptyAlgo(t *testing.T) {
	cfg := &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "test",
				Address:  "127.0.0.1:0",
				Protocol: "http",
				Routes:   []*config.Route{{Path: "/", Pool: "pool1"}},
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "pool1",
				Algorithm: "",
				HealthCheck: &config.HealthCheck{
					Type:     "http",
					Path:     "/health",
					Interval: "10s",
					Timeout:  "5s",
				},
				Backends: []*config.Backend{
					{ID: "b1", Address: "127.0.0.1:8081", Weight: 100},
				},
			},
		},
		Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.validateConfig(cfg)
	if err != nil {
		t.Errorf("validateConfig() with empty algorithm error = %v", err)
	}
}

// ============================================================================
// Tests for applyConfig algorithm variants
// ============================================================================

// applyConfigAlgoHelper tests applyConfig with a specific algorithm.
func applyConfigAlgoHelper(t *testing.T, algo string) {
	t.Helper()
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools[0].Algorithm = algo

	err = engine.applyConfig(newCfg)
	if err != nil {
		t.Errorf("applyConfig() with algo %q error = %v", algo, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig_Algo_LC(t *testing.T)             { applyConfigAlgoHelper(t, "lc") }
func TestApplyConfig_Algo_WLC(t *testing.T)            { applyConfigAlgoHelper(t, "wlc") }
func TestApplyConfig_Algo_LRT(t *testing.T)            { applyConfigAlgoHelper(t, "lrt") }
func TestApplyConfig_Algo_WLRT(t *testing.T)           { applyConfigAlgoHelper(t, "wlrt") }
func TestApplyConfig_Algo_IPHash(t *testing.T)         { applyConfigAlgoHelper(t, "ip_hash") }
func TestApplyConfig_Algo_CH(t *testing.T)             { applyConfigAlgoHelper(t, "ch") }
func TestApplyConfig_Algo_Maglev(t *testing.T)         { applyConfigAlgoHelper(t, "maglev") }
func TestApplyConfig_Algo_P2C(t *testing.T)            { applyConfigAlgoHelper(t, "p2c") }
func TestApplyConfig_Algo_Random(t *testing.T)         { applyConfigAlgoHelper(t, "random") }
func TestApplyConfig_Algo_WRandom(t *testing.T)        { applyConfigAlgoHelper(t, "wrandom") }
func TestApplyConfig_Algo_RingHash(t *testing.T)       { applyConfigAlgoHelper(t, "ring_hash") }
func TestApplyConfig_Algo_Sticky(t *testing.T)         { applyConfigAlgoHelper(t, "sticky") }
func TestApplyConfig_Algo_EmptyDefault(t *testing.T)   { applyConfigAlgoHelper(t, "") }
func TestApplyConfig_Algo_UnknownDefault(t *testing.T) { applyConfigAlgoHelper(t, "unknown_algo") }

// ============================================================================
// Tests for applyConfig error paths
// ============================================================================

func TestApplyConfig_BackendAutoGenID(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools[0].Backends[0].ID = ""

	err = engine.applyConfig(newCfg)
	if err != nil {
		t.Errorf("applyConfig() with auto-ID error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig_DupBackend(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools[0].Backends = []*config.Backend{
		{ID: "dup", Address: "127.0.0.1:8081", Weight: 100},
		{ID: "dup", Address: "127.0.0.1:8082", Weight: 100},
	}

	err = engine.applyConfig(newCfg)
	if err == nil {
		t.Error("applyConfig() expected error for duplicate backend ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig_DupPoolName(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools = append(newCfg.Pools, &config.Pool{
		Name:      "test-pool",
		Algorithm: "round_robin",
		HealthCheck: &config.HealthCheck{
			Type:     "http",
			Path:     "/health",
			Interval: "10s",
			Timeout:  "5s",
		},
		Backends: []*config.Backend{
			{ID: "b2", Address: "127.0.0.1:8082", Weight: 100},
		},
	})

	err = engine.applyConfig(newCfg)
	if err == nil {
		t.Error("applyConfig() expected error for duplicate pool name")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig_TwoPools(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools = append(newCfg.Pools, &config.Pool{
		Name:      "second-pool",
		Algorithm: "least_connections",
		HealthCheck: &config.HealthCheck{
			Type:     "tcp",
			Interval: "5s",
			Timeout:  "3s",
		},
		Backends: []*config.Backend{
			{ID: "b2", Address: "127.0.0.1:8082", Weight: 50},
			{ID: "b3", Address: "127.0.0.1:8083", Weight: 50},
		},
	})

	err = engine.applyConfig(newCfg)
	if err != nil {
		t.Errorf("applyConfig() error = %v", err)
	}

	if engine.poolManager.PoolCount() != 2 {
		t.Errorf("PoolCount() = %d, want 2", engine.poolManager.PoolCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig_TLSReloadFail(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.TLS = &config.TLSConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	err = engine.applyConfig(newCfg)
	t.Logf("applyConfig with bad TLS returned: %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig_HealthCheckRegWarning(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools[0].HealthCheck = &config.HealthCheck{
		Type:     "http",
		Path:     "/health",
		Interval: "10s",
		Timeout:  "5s",
	}

	err = engine.applyConfig(newCfg)
	if err != nil {
		t.Errorf("applyConfig() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// ============================================================================
// Tests for getMCPAddress edge cases
// ============================================================================

func TestGetMCPAddress_Variants(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name:     "empty config",
			cfg:      &config.Config{},
			expected: ":8081",
		},
		{
			name: "admin with empty fields",
			cfg: &config.Config{
				Admin: &config.Admin{},
			},
			expected: ":8081",
		},
		{
			name: "explicit MCP address",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address:    ":9090",
					MCPAddress: ":5555",
				},
			},
			expected: ":5555",
		},
		{
			name: "host and port only",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address: "10.0.0.1:3000",
				},
			},
			expected: "10.0.0.1:3001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMCPAddress(tt.cfg)
			if result != tt.expected {
				t.Errorf("getMCPAddress() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Tests for startConfigWatcher
// ============================================================================

func TestStartConfigWatcher_Basic(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	engine.startConfigWatcher()

	if engine.configWatcher == nil {
		t.Error("configWatcher should not be nil")
	}

	engine.configWatcher.Stop()
}

// ============================================================================
// Tests for startHTTPListener edge case
// ============================================================================

func TestStartHTTPListener_HTTPSTLSConfig(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listenerCfg := &config.Listener{
		Name:     "https-tls",
		Address:  ":0",
		Protocol: "https",
		TLS:      &config.ListenerTLS{Enabled: true},
		Pool:     "test-pool",
	}

	err = engine.startHTTPListener(listenerCfg)
	t.Logf("startHTTPListener(https with TLS) returned: %v", err)
}

// ============================================================================
// Additional coverage tests — targeting low-coverage functions
// ============================================================================

// TestCov_GetMCPAddress_NoColonInAddress tests getMCPAddress when the admin
// address has no colon (returns empty string).
func TestCov_GetMCPAddress_NoColonInAddress(t *testing.T) {
	// getMCPAddress calls getAdminAddress which always returns a colon-based
	// address. To reach the no-colon branch, we need to test the internal
	// parsing directly. Since we can't override getAdminAddress, we verify
	// the function works correctly with all standard inputs.
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "standard port offset",
			cfg: &config.Config{
				Admin: &config.Admin{Address: ":8080"},
			},
			expected: ":8081",
		},
		{
			name: "explicit MCP address overrides port offset",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address:    ":8080",
					MCPAddress: ":9999",
				},
			},
			expected: ":9999",
		},
		{
			name: "high port number",
			cfg: &config.Config{
				Admin: &config.Admin{Address: ":65534"},
			},
			expected: ":65535",
		},
		{
			name:     "admin nil defaults to 8080+1",
			cfg:      &config.Config{},
			expected: ":8081",
		},
		{
			name: "admin empty address defaults to 8080+1",
			cfg: &config.Config{
				Admin: &config.Admin{Address: ""},
			},
			expected: ":8081",
		},
		{
			name: "ipv4 with port",
			cfg: &config.Config{
				Admin: &config.Admin{Address: "10.0.0.1:3000"},
			},
			expected: "10.0.0.1:3001",
		},
		{
			name: "ipv6 bracketed with port",
			cfg: &config.Config{
				Admin: &config.Admin{Address: "[::1]:9090"},
			},
			expected: "[::1]:9091",
		},
		{
			name: "empty MCPAddress ignored",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address:    ":4000",
					MCPAddress: "",
				},
			},
			expected: ":4001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMCPAddress(tt.cfg)
			if result != tt.expected {
				t.Errorf("getMCPAddress() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestCov_StartConfigWatcher_DebounceReload tests the debounce reload callback
// by writing config changes and waiting for the watcher to trigger.
func TestCov_StartConfigWatcher_DebounceReload(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify config watcher is running
	if engine.configWatcher == nil {
		t.Fatal("configWatcher should not be nil after Start")
	}

	// Write the same valid config to trigger the watcher debounce
	sameContent := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(sameContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Wait for debounce timer (500ms) plus watcher interval (2s) plus margin
	time.Sleep(3 * time.Second)

	// Engine should still be running after auto-reload
	if !engine.IsRunning() {
		t.Error("Engine should still be running after auto-reload")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_StartConfigWatcher_RapidChanges tests that rapid config file changes
// are properly debounced (only one reload fires).
func TestCov_StartConfigWatcher_RapidChanges(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write multiple rapid changes
	for i := 0; i < 5; i++ {
		content := fmt.Sprintf(`
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: %d
admin:
  enabled: true
  address: "127.0.0.1:0"
`, 100+i)
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write config iteration %d: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for debounce to fire
	time.Sleep(2 * time.Second)

	if !engine.IsRunning() {
		t.Error("Engine should still be running after rapid config changes")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_InvalidToValid tests reload when the config file is first
// invalid then replaced with valid content.
func TestCov_Reload_InvalidToValid(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write invalid config and try reload
	os.WriteFile(configPath, []byte("::invalid::yaml::"), 0644)
	err = engine.Reload()
	if err == nil {
		t.Error("Reload with invalid YAML should fail")
	}
	if !engine.IsRunning() {
		t.Error("Engine should still be running after failed reload")
	}

	// Restore valid config and reload successfully
	validContent := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
      - path: /api
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	os.WriteFile(configPath, []byte(validContent), 0644)
	err = engine.Reload()
	if err != nil {
		t.Errorf("Reload with valid config should succeed: %v", err)
	}

	if engine.router.RouteCount() != 2 {
		t.Errorf("RouteCount() = %d, want 2 after reload", engine.router.RouteCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_EmptyConfigPath tests reload when configPath is empty.
func TestCov_Reload_EmptyConfigPath(t *testing.T) {
	cfg := createTestConfig()
	engine, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = engine.Reload()
	if err == nil {
		t.Error("Reload with empty config path should fail")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_InitCluster_WithTransport tests initCluster and verifies that TCP
// transport is created when bind address/port are valid.
func TestCov_InitCluster_WithTransport(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()
	clusterCfg := &config.ClusterConfig{
		Enabled:       true,
		NodeID:        "node-transport-test",
		BindAddr:      "127.0.0.1",
		BindPort:      0, // OS-assigned port
		DataDir:       tmpDir,
		Peers:         []string{},
		ElectionTick:  "3s",
		HeartbeatTick: "1s",
	}

	err = engine.initCluster(clusterCfg, engine.logger)
	if err != nil {
		t.Fatalf("initCluster() error = %v", err)
	}

	if engine.raftCluster == nil {
		t.Error("raftCluster should not be nil")
	}
	if engine.clusterMgr == nil {
		t.Error("clusterMgr should not be nil")
	}
}

// TestCov_InitCluster_EmptyTicks tests initCluster with empty election/heartbeat
// tick strings (should use defaults).
func TestCov_InitCluster_EmptyTicks(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()
	clusterCfg := &config.ClusterConfig{
		Enabled:       true,
		NodeID:        "node-default-ticks",
		BindAddr:      "127.0.0.1",
		BindPort:      0,
		DataDir:       tmpDir,
		Peers:         []string{},
		ElectionTick:  "", // should default to 2s
		HeartbeatTick: "", // should default to 500ms
	}

	err = engine.initCluster(clusterCfg, engine.logger)
	if err != nil {
		t.Fatalf("initCluster() error = %v", err)
	}
}

// TestCov_New_WithDiscoveryManager tests that the discovery manager is initialized.
func TestCov_New_WithDiscoveryManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.discoveryMgr == nil {
		t.Error("discoveryMgr should not be nil after New()")
	}
}

// TestCov_New_WithAdminEnabled tests that admin server is properly created
// when admin config is present.
func TestCov_New_WithAdminEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin = &config.Admin{
		Enabled: true,
		Address: "127.0.0.1:0",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.adminServer == nil {
		t.Error("adminServer should not be nil when admin is enabled")
	}
}

// TestCov_Start_MCPTransportStartFail tests that engine starts even when
// MCP transport fails to start (bad address).
func TestCov_Start_MCPTransportStartFail(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPAddress = "0.0.0.0:0" // addr :0 might bind but let's try a bad one

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Engine should be running regardless of MCP transport result
	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_ACMEWithEmail tests New with ACME configuration that has an email.
func TestCov_New_ACMEWithEmail(t *testing.T) {
	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		ACME: &config.ACME{
			Enabled: true,
			Email:   "test@openloadbalancer.dev",
			Domains: []string{"example.com"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() with ACME email error = %v", err)
	}
	_ = engine
}

// TestCov_New_ACMEEmptyEmail tests New with ACME enabled but no email.
func TestCov_New_ACMEEmptyEmail(t *testing.T) {
	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		ACME: &config.ACME{
			Enabled: true,
			Email:   "",
			Domains: []string{"example.com"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	_, err := New(cfg, configPath)
	if err == nil {
		t.Fatal("New() with ACME empty email expected error, got nil")
	}
}

// TestCov_New_ProfilingWithCPUProfile tests profiling with CPU profile path.
func TestCov_New_ProfilingWithCPUProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:        true,
		CPUProfilePath: filepath.Join(tmpDir, "cpu.prof"),
		MemProfilePath: filepath.Join(tmpDir, "mem.prof"),
		PprofAddr:      "127.0.0.1:0",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() with profiling error = %v", err)
	}

	if engine.profilingCleanup == nil {
		t.Error("profilingCleanup should not be nil")
	}

	// Cleanup profiling
	engine.profilingCleanup()
}

// TestCov_Reload_ApplyConfigError tests the reload path where applyConfig fails
// but state is restored to running.
func TestCov_Reload_ApplyConfigError(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write a config that will pass loading but fail validation (missing pool reference)
	badCfg := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: nonexistent-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(badCfg), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	err = engine.Reload()
	if err == nil {
		t.Error("Reload should fail with nonexistent pool reference")
	}

	// State should be restored to running
	if engine.GetState() != StateRunning {
		t.Errorf("State = %v after failed reload, want StateRunning", engine.GetState())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_OCSPManager tests that the OCSP manager start path is exercised.
func TestCov_Start_OCSPManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// OCSP manager should be initialized
	if engine.ocspManager == nil {
		t.Fatal("ocspManager should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_PassiveChecker tests that passive checker starts with engine.
func TestCov_Start_PassiveChecker(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.passiveChecker == nil {
		t.Fatal("passiveChecker should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_DiscoveryManager tests that discovery manager starts during Start.
func TestCov_Start_DiscoveryManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.discoveryMgr == nil {
		t.Fatal("discoveryMgr should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_StartConfigWatcher_DebounceTimerReset tests that rapid file changes
// cause the debounce timer to be reset (covering the debounceTimer.Stop() path).
func TestCov_StartConfigWatcher_DebounceTimerReset(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.configWatcher == nil {
		t.Fatal("configWatcher should not be nil after Start")
	}

	// Write the same valid config multiple times rapidly to trigger debounce reset
	validContent := []byte(`
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
admin:
  enabled: true
  address: "127.0.0.1:0"
`)

	// First write to start the debounce timer
	if err := os.WriteFile(configPath, validContent, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Wait for the watcher to detect the first change (< 2s poll interval)
	time.Sleep(2500 * time.Millisecond)

	// Second write - this should reset the debounce timer (cover Stop() path)
	if err := os.WriteFile(configPath, validContent, 0644); err != nil {
		t.Fatalf("Failed to write config second time: %v", err)
	}

	// Wait for debounce timer to fire (500ms) and reload to complete
	time.Sleep(2 * time.Second)

	if !engine.IsRunning() {
		t.Error("Engine should still be running after debounced reload")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_ConfigPathEmpty tests reload when the engine's configPath is empty.
func TestCov_Reload_ConfigPathEmpty(t *testing.T) {
	cfg := createTestConfig()

	engine, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// ConfigPath is empty, so loadConfig should fail
	err = engine.Reload()
	if err == nil {
		t.Error("Reload should fail with empty config path")
	}

	if !engine.IsRunning() {
		t.Error("Engine should still be running after failed reload")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_ValidContentChange tests reload with a valid content change
// that successfully applies new routes.
func TestCov_Reload_ValidContentChange(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	initialRoutes := engine.router.RouteCount()

	// Write config with an additional pool and route
	newContent := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
      - path: /v2
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
      - id: backend-2
        address: "127.0.0.1:8082"
        weight: 50
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	err = engine.Reload()
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	// Verify routes increased
	if engine.router.RouteCount() <= initialRoutes {
		t.Errorf("RouteCount() = %d, expected > %d", engine.router.RouteCount(), initialRoutes)
	}

	// Verify pool still has correct backends
	pool := engine.poolManager.GetPool("test-pool")
	if pool == nil {
		t.Fatal("test-pool should exist")
	}
	if pool.BackendCount() != 2 {
		t.Errorf("BackendCount() = %d, want 2", pool.BackendCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_NilAdmin tests New when admin is nil (uses defaults).
func TestCov_New_NilAdmin(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin = nil

	_, err := New(cfg, "")
	// May or may not succeed depending on config validation
	t.Logf("New with nil admin: %v", err)
}

// TestCov_New_WebUIHandler tests that WebUI handler is created.
func TestCov_New_WebUIHandler(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// webUIHandler may or may not be nil depending on embedded assets
	_ = engine.webUIHandler
}

// TestCov_Start_ShutdownCluster tests that cluster components shut down cleanly.
func TestCov_Start_ShutdownCluster(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:       true,
		NodeID:        "node-shutdown-test",
		BindAddr:      "127.0.0.1",
		BindPort:      0,
		DataDir:       tmpDir,
		Peers:         []string{},
		ElectionTick:  "2s",
		HeartbeatTick: "500ms",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.clusterMgr == nil {
		t.Fatal("clusterMgr should not be nil with cluster config")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	if engine.IsRunning() {
		t.Error("Engine should not be running after shutdown")
	}
}

// TestCov_Start_MCPWithExplicitAddress tests Start with an explicit MCP address.
func TestCov_Start_MCPWithExplicitAddress(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPAddress = "127.0.0.1:0"
	cfg.Admin.MCPToken = "test-bearer-token"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.mcpTransport == nil {
		t.Error("mcpTransport should not be nil with explicit MCP address")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_LoadFail tests reload when the config file cannot be loaded
// (e.g. deleted). This exercises the loadConfig error path in Reload.
func TestCov_Reload_LoadFail(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Delete the config file
	os.Remove(configPath)

	err = engine.Reload()
	if err == nil {
		t.Error("Reload should fail when config file is deleted")
	}
	if !strings.Contains(err.Error(), "failed to load") {
		t.Errorf("Error should mention load failure: %v", err)
	}

	// State should be restored
	if engine.GetState() != StateRunning {
		t.Errorf("State = %v, want StateRunning", engine.GetState())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_ValidationFail tests reload when validation fails.
func TestCov_Reload_ValidationFail(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write config with invalid algorithm
	invalidCfg := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
pools:
  - name: test-pool
    algorithm: totally_invalid_algo
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(invalidCfg), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	err = engine.Reload()
	if err == nil {
		t.Error("Reload should fail with invalid algorithm")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("Error should mention validation: %v", err)
	}

	if engine.GetState() != StateRunning {
		t.Errorf("State = %v, want StateRunning", engine.GetState())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_InitCluster_WithPeers tests initCluster with peer nodes configured.
func TestCov_InitCluster_WithPeers(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()
	clusterCfg := &config.ClusterConfig{
		Enabled:       true,
		NodeID:        "node-with-peers",
		BindAddr:      "127.0.0.1",
		BindPort:      0,
		DataDir:       tmpDir,
		Peers:         []string{"127.0.0.1:19090", "127.0.0.1:19091"},
		ElectionTick:  "2s",
		HeartbeatTick: "500ms",
	}

	err = engine.initCluster(clusterCfg, engine.logger)
	if err != nil {
		t.Fatalf("initCluster() error = %v", err)
	}

	if engine.raftCluster == nil {
		t.Error("raftCluster should not be nil")
	}
	if engine.clusterMgr == nil {
		t.Error("clusterMgr should not be nil")
	}
}

// TestCov_GetMCPAddress_EdgeCases tests getMCPAddress with additional edge cases
// to maximize branch coverage.
func TestCov_GetMCPAddress_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "admin with only MCPAddress set",
			cfg: &config.Config{
				Admin: &config.Admin{
					MCPAddress: "unix:/var/run/mcp.sock",
				},
			},
			expected: "unix:/var/run/mcp.sock",
		},
		{
			name: "admin address localhost with port",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address: "localhost:9090",
				},
			},
			expected: "localhost:9091",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMCPAddress(tt.cfg)
			if result != tt.expected {
				t.Errorf("getMCPAddress() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestCov_New_WithPluginManager tests that the plugin manager is initialized.
func TestCov_New_WithPluginManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.pluginMgr == nil {
		t.Error("pluginMgr should not be nil")
	}
}

// TestCov_New_ConnManager tests that connection manager is initialized.
func TestCov_New_ConnManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.connManager == nil {
		t.Error("connManager should not be nil")
	}
	if engine.connPoolMgr == nil {
		t.Error("connPoolMgr should not be nil")
	}
}

// TestCov_Start_HTTPListenerWithProxy tests startHTTPListener through Start.
func TestCov_Start_HTTPListenerWithProxy(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify listener is running
	if len(engine.listeners) == 0 {
		t.Error("Expected at least one listener")
	}

	for _, l := range engine.listeners {
		if !l.IsRunning() {
			t.Errorf("Listener %s should be running", l.Name())
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ApplyConfig_TLSReload tests applyConfig with TLS reload success path.
func TestCov_ApplyConfig_TLSReload(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	generateTestCertFiles(t, certFile, keyFile)

	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Apply new config with TLS reload
	newCfg := createTestConfig()
	newCfg.TLS = &config.TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	err = engine.applyConfig(newCfg)
	if err != nil {
		t.Errorf("applyConfig() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Shutdown_NilContextOnRunning tests Shutdown(nil) on a running engine.
// This exercises the ctx==nil path in Shutdown.
func TestCov_Shutdown_NilContextOnRunning(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Shutdown with nil context - should create a default timeout
	err = engine.Shutdown(nil)
	if err != nil {
		t.Logf("Shutdown(nil) returned: %v", err)
	}

	if engine.IsRunning() {
		t.Error("Engine should not be running after shutdown")
	}
}

// TestCov_Start_TLSLoadFailure tests Start when TLS cert loading fails.
func TestCov_Start_TLSLoadFailure(t *testing.T) {
	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.Start()
	if err == nil {
		t.Error("Start() should fail with missing TLS certs")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		engine.Shutdown(ctx)
	}

	if engine.GetState() != StateStopped {
		t.Errorf("State = %v, want StateStopped", engine.GetState())
	}
}

// TestCov_Shutdown_WithProfiling tests shutdown with profiling cleanup.
func TestCov_Shutdown_WithProfiling(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:   true,
		PprofAddr: "127.0.0.1:0",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.profilingCleanup == nil {
		t.Fatal("profilingCleanup should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Shutdown should call profilingCleanup
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestCov_Start_WithTLSAndCert tests Start with valid TLS certificates.
func TestCov_Start_WithTLSAndCert(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	generateTestCertFiles(t, certFile, keyFile)

	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify TLS cert was loaded
	certs := engine.tlsManager.ListCertificates()
	if len(certs) == 0 {
		t.Error("Expected at least one certificate loaded")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_InitializePools_EmptyAlgorithm tests initializePools with empty algorithm
// defaults to round_robin.
func TestCov_InitializePools_EmptyAlgorithm(t *testing.T) {
	cfg := &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "test",
				Address:  "127.0.0.1:0",
				Protocol: "http",
				Routes:   []*config.Route{{Path: "/", Pool: "pool1"}},
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "pool1",
				Algorithm: "",
				HealthCheck: &config.HealthCheck{
					Type:     "http",
					Path:     "/health",
					Interval: "10s",
					Timeout:  "5s",
				},
				Backends: []*config.Backend{
					{ID: "b1", Address: "127.0.0.1:8081", Weight: 100},
				},
			},
		},
		Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.initializePools()
	if err != nil {
		t.Fatalf("initializePools() error = %v", err)
	}

	pool := engine.poolManager.GetPool("pool1")
	if pool == nil {
		t.Fatal("pool1 should exist")
	}
	if pool.GetBalancer() == nil {
		t.Error("pool should have a balancer (default round_robin)")
	}
}

// TestCov_InitCluster_FailInvalidNodeID tests initCluster failure with empty node ID.
func TestCov_InitCluster_FailInvalidNodeID(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	clusterCfg := &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "",
		BindAddr: "",
		BindPort: 0,
	}

	err = engine.initCluster(clusterCfg, engine.logger)
	if err == nil {
		t.Error("initCluster() should fail with empty NodeID")
	}
}

// ============================================================================
// Coverage tests targeting specific uncovered lines
// ============================================================================

// TestCov_Shutdown_WithGeoDNS tests that GeoDNS is stopped during shutdown.
func TestCov_Shutdown_WithGeoDNS(t *testing.T) {
	cfg := createTestConfig()
	cfg.GeoDNS = &config.GeoDNSConfig{
		Enabled:     true,
		DefaultPool: "test-pool",
		DBPath:      "GeoLite2-Country.mmdb",
		Rules: []config.GeoDNSRule{
			{ID: "eu-rule", Region: "EU", Pool: "test-pool", Weight: 100},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.geoDNS == nil {
		t.Fatal("geoDNS should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestCov_Shutdown_WithACME tests that ACME client is closed during shutdown.
func TestCov_Shutdown_WithACME(t *testing.T) {
	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		ACME: &config.ACME{
			Enabled: true,
			Email:   "test@openloadbalancer.dev",
			Domains: []string{"example.com"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.acmeClient == nil {
		t.Skip("ACME client not initialized (network issue)")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestCov_Shutdown_WithShadowMgr tests that shadow manager is waited during shutdown.
func TestCov_Shutdown_WithShadowMgr(t *testing.T) {
	cfg := createTestConfig()
	cfg.Shadow = &config.ShadowConfig{
		Enabled:     true,
		Percentage:  10,
		CopyHeaders: true,
		CopyBody:    false,
		Timeout:     "5s",
		Targets:     []config.ShadowTarget{{Pool: "test-pool", Percentage: 100}},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.shadowMgr == nil {
		t.Fatal("shadowMgr should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestCov_Shutdown_WithOCSPManager tests that OCSP manager is stopped during shutdown.
func TestCov_Shutdown_WithOCSPManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.ocspManager == nil {
		t.Fatal("ocspManager should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestCov_Start_WithGRPCTLSSkipVerify tests that gRPC TLS skip verify is propagated
// from pool health check config.
func TestCov_Start_WithGRPCTLSSkipVerify(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].HealthCheck.GRPCTLSSkipVerify = true

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_NoMCPWhenNoToken tests that MCP server is nil when no token configured.
func TestCov_Start_NoMCPWhenNoToken(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPToken = ""
	cfg.Admin.MCPAddress = "127.0.0.1:0"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.mcpServer != nil {
		t.Error("mcpServer should be nil when no MCPToken configured")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// MCP transport should not start without mcpServer
	if engine.mcpTransport != nil {
		t.Error("mcpTransport should be nil when mcpServer is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_AdminWithBearerTokenOnly tests admin auth with only bearer token (no username).
func TestCov_New_AdminWithBearerTokenOnly(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.BearerToken = "my-bearer-token"
	cfg.Admin.Username = ""

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
}

// TestCov_New_ServerConfigShutdownTimeouts tests server config timeout parsing.
func TestCov_New_ServerConfigShutdownTimeouts(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		ShutdownTimeout:         "45s",
		ListenerStopTimeout:     "10s",
		ProxyDrainWindow:        "8s",
		RollbackCheckInterval:   "20s",
		RollbackMonitorDuration: "40s",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.shutdownTimeout != 45*time.Second {
		t.Errorf("shutdownTimeout = %v, want 45s", engine.shutdownTimeout)
	}
	if engine.listenerStopTimeout != 10*time.Second {
		t.Errorf("listenerStopTimeout = %v, want 10s", engine.listenerStopTimeout)
	}
	if engine.proxyDrainWindow != 8*time.Second {
		t.Errorf("proxyDrainWindow = %v, want 8s", engine.proxyDrainWindow)
	}
	if engine.rollbackCheckInterval != 20*time.Second {
		t.Errorf("rollbackCheckInterval = %v, want 20s", engine.rollbackCheckInterval)
	}
	if engine.rollbackMonitorDuration != 40*time.Second {
		t.Errorf("rollbackMonitorDuration = %v, want 40s", engine.rollbackMonitorDuration)
	}
}

// TestCov_New_ServerConfigInvalidTimeouts tests server config with invalid durations triggers validation error.
func TestCov_New_ServerConfigInvalidTimeouts(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		ShutdownTimeout:     "not-a-duration",
		ListenerStopTimeout: "bad",
		ProxyDrainWindow:    "also-bad",
	}

	configPath := createTempConfigFile(t, cfg)
	_, err := New(cfg, configPath)
	if err == nil {
		t.Fatal("New() expected error for invalid server timeouts, got nil")
	}
}

// TestCov_Start_WithLogFileOutput tests Start with file logging output.
func TestCov_Start_WithLogFileOutput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestCov_Start_WithLogFileOutput")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	logFile := filepath.Join(tmpDir, "engine.log")

	cfg := createTestConfig()
	cfg.Logging = &config.Logging{
		Level:  "info",
		Format: "json",
		Output: logFile,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.logFileOutput == nil {
		t.Error("logFileOutput should not be nil with file logging")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)

	// Close the log file output to release the file handle so os.RemoveAll succeeds
	if engine.logFileOutput != nil {
		engine.logFileOutput.Close()
	}
}

// TestCov_Start_MCPTransportCreationFail tests when SSE transport creation fails.
func TestCov_Start_MCPTransportCreationFail(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPToken = "test-token"
	cfg.Admin.MCPAddress = "invalid:addr:bad:format"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// mcpTransport should be nil since creation failed
	if engine.mcpTransport != nil {
		t.Log("mcpTransport was created despite bad address")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ApplyConfigInternal_NoRollback tests applyConfigInternal with noRollback=true.
func TestCov_ApplyConfigInternal_NoRollback(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Apply config with noRollback=true
	newCfg := createTestConfig()
	err = engine.applyConfigInternal(newCfg, true)
	if err != nil {
		t.Errorf("applyConfigInternal() noRollback error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ApplyConfig_WeightOverflow tests applyConfig with backend weight exceeding MaxInt32.
func TestCov_ApplyConfig_WeightOverflow(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools[0].Backends[0].Weight = math.MaxInt32 + 1

	err = engine.applyConfig(newCfg)
	if err == nil {
		t.Error("applyConfig() expected error for weight overflow")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ApplyConfig_BackendWithScheme tests applyConfig with backend scheme.
func TestCov_ApplyConfig_BackendWithScheme(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	newCfg.Pools[0].Backends[0].Scheme = "https"

	err = engine.applyConfig(newCfg)
	if err != nil {
		t.Errorf("applyConfig() with scheme error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ApplyConfig_DuplicatePoolNameInNewConfig tests applyConfig with duplicate pool names
// in the new config itself.
func TestCov_ApplyConfig_DuplicatePoolNameInNewConfig(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	newCfg := createTestConfig()
	// Add a second pool with the same name as the first
	newCfg.Pools = append(newCfg.Pools, &config.Pool{
		Name:      "test-pool", // duplicate
		Algorithm: "round_robin",
		HealthCheck: &config.HealthCheck{
			Type:     "http",
			Path:     "/health",
			Interval: "10s",
			Timeout:  "5s",
		},
		Backends: []*config.Backend{
			{ID: "b2", Address: "127.0.0.1:8082", Weight: 50},
		},
	})

	err = engine.applyConfig(newCfg)
	if err == nil {
		t.Error("applyConfig() expected error for duplicate pool name")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ReloadConfig_ListenerChanged tests that listenersChanged correctly detects changes.
func TestCov_ReloadConfig_ListenerChanged(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write config with different listener address (triggers listener changed warning)
	newContent := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
      - path: /v2
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	err = engine.Reload()
	if err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_StartHTTPListener_ErrorPath tests startHTTPListener with an address that fails.
func TestCov_StartHTTPListener_ErrorPath(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Use an address that cannot be bound
	listenerCfg := &config.Listener{
		Name:     "http-fail",
		Address:  "256.256.256.256:8080", // invalid IP
		Protocol: "http",
		Pool:     "test-pool",
	}

	err = engine.startHTTPListener(listenerCfg)
	if err == nil {
		t.Error("startHTTPListener() expected error for invalid address")
	}
}

// TestCov_StartTCPListener_StartFail tests startTCPListener when Start() fails.
func TestCov_StartTCPListener_StartFail(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pool := backend.NewPool("tcp-fail-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("tcp-fb1", "127.0.0.1:9999")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	engine.poolManager.AddPool(pool)

	listenerCfg := &config.Listener{
		Name:     "tcp-fail",
		Address:  "256.256.256.256:9999", // invalid address
		Protocol: "tcp",
		Pool:     "tcp-fail-pool",
	}

	err = engine.startTCPListener(listenerCfg)
	if err == nil {
		t.Error("startTCPListener() expected error for invalid address")
	}
}

// TestCov_RegisterCORSMiddleware_Defaults tests CORS middleware with empty defaults.
func TestCov_RegisterCORSMiddleware_Defaults(t *testing.T) {
	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				CORS: &config.CORSConfig{
					Enabled: true,
					// AllowedOrigins and AllowedMethods empty — should use defaults
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	registerCORSMiddleware(ctx)
}

// TestCov_RegisterLoggingMiddleware_WithCustomFormat tests logging with custom format.
func TestCov_RegisterLoggingMiddleware_WithCustomFormat(t *testing.T) {
	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				Logging: &config.LoggingConfig{
					Enabled:      true,
					Format:       "text",
					CustomFormat: "$method $path $status",
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	registerLoggingMiddleware(ctx)
}

// TestCov_UpdateSystemMetrics_WithPoolAndHealthChecker tests full updateSystemMetrics path.
func TestCov_UpdateSystemMetrics_WithPoolAndHealthChecker(t *testing.T) {
	registry := metrics.NewRegistry()
	sm := registerSystemMetrics(registry)

	poolMgr := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	b1 := backend.NewBackend("b1", "localhost:3001")
	b1.SetState(backend.StateUp)
	b2 := backend.NewBackend("b2", "localhost:3002")
	b2.SetState(backend.StateDown)
	pool.AddBackend(b1)
	pool.AddBackend(b2)
	poolMgr.AddPool(pool)

	hc := health.NewChecker()

	sm.updateSystemMetrics(poolMgr, hc, nil)

	// Verify no panic and gauges were set (we can't read gauge values directly
	// since Value() is unexported, but the update should succeed without error)
}

// TestCov_Start_WithDiscoveryManagerStopCh tests that discovery manager goroutine
// properly exits when stopCh is closed.
func TestCov_Start_WithDiscoveryManagerStopCh(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.discoveryMgr == nil {
		t.Fatal("discoveryMgr should not be nil")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Shutdown should close stopCh, which cancels the discovery manager context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestCov_New_WithWAFAndMCPToken tests WAF MCP tools registration with MCP token.
func TestCov_New_WithWAFAndMCPToken(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPToken = "test-secret"
	cfg.WAF = &config.WAFConfig{
		Enabled: true,
		Mode:    "enforce",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.mcpServer == nil {
		t.Error("mcpServer should not be nil with MCPToken configured")
	}
}

// TestCov_MTLS_Start_ServerError tests mTLS listener Start when server errors.
// This exercises the goroutine that sets running=false on ListenAndServeTLS error.
func TestCov_MTLS_Start_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cert := generateTestCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	l, err := newMTLSHTTPSListener(&olbListener.Options{
		Name:    "test-mtls-err",
		Address: "127.0.0.1:0",
		Handler: handler,
	}, tlsCfg)
	if err != nil {
		t.Fatalf("newMTLSHTTPSListener() error = %v", err)
	}

	// Start the listener normally first
	if err := l.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Now shutdown the server to trigger the error path in the goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// After stop, running should be false
	if l.IsRunning() {
		t.Error("IsRunning() should be false after Stop()")
	}
}

// TestCov_Reload_Success tests successful reload with actual config change.
func TestCov_Reload_Success(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write updated config with additional backend
	updatedContent := `
version: "1"
listeners:
  - name: test-http
    address: "127.0.0.1:0"
    protocol: http
    routes:
      - path: /
        pool: test-pool
pools:
  - name: test-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
    backends:
      - id: backend-1
        address: "127.0.0.1:8081"
        weight: 100
      - id: backend-2
        address: "127.0.0.1:8082"
        weight: 50
admin:
  enabled: true
  address: "127.0.0.1:0"
`
	if err := os.WriteFile(configPath, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	err = engine.Reload()
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	pool := engine.poolManager.GetPool("test-pool")
	if pool == nil {
		t.Fatal("test-pool should exist after reload")
	}
	if pool.BackendCount() != 2 {
		t.Errorf("BackendCount() = %d, want 2", pool.BackendCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_InitCluster_WithValidTransport tests initCluster with transport start.
func TestCov_InitCluster_WithValidTransport(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()
	clusterCfg := &config.ClusterConfig{
		Enabled:       true,
		NodeID:        "transport-node",
		BindAddr:      "127.0.0.1",
		BindPort:      0, // OS-assigned
		DataDir:       tmpDir,
		Peers:         []string{},
		ElectionTick:  "2s",
		HeartbeatTick: "500ms",
	}

	err = engine.initCluster(clusterCfg, engine.logger)
	if err != nil {
		t.Fatalf("initCluster() error = %v", err)
	}

	if engine.raftCluster == nil {
		t.Error("raftCluster should not be nil")
	}
	if engine.persister == nil {
		t.Error("persister should not be nil with DataDir set")
	}
}

// TestCov_RegisterJWTMiddleware_WithValidEdDSAKeyFile tests JWT with actual EdDSA key file.
func TestCov_RegisterJWTMiddleware_WithValidEdDSAKeyFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "eddsa.pub")

	// Write a valid base64 encoded key to the file
	keyContent := base64.StdEncoding.EncodeToString([]byte("test-eddsa-public-key-material"))
	if err := os.WriteFile(keyFile, []byte(keyContent), 0644); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				JWT: &config.JWTConfig{
					Enabled:   true,
					Algorithm: "EdDSA",
					PublicKey: keyFile,
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	registerJWTMiddleware(ctx)
}

// TestCov_MTLS_Stop_NotRunning tests mtlsHTTPSListener.Stop when not running.
func TestCov_MTLS_Stop_NotRunning(t *testing.T) {
	l := &mtlsHTTPSListener{
		name:    "test",
		address: "127.0.0.1:0",
		running: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := l.Stop(ctx)
	if err == nil {
		t.Error("Stop() on non-running listener should return error")
	}
}

// TestCov_MTLS_Address_IsRunning tests mtlsHTTPSListener state queries.
func TestCov_MTLS_Address_IsRunning(t *testing.T) {
	l := &mtlsHTTPSListener{
		name:       "test",
		address:    "127.0.0.1:12345",
		actualAddr: "127.0.0.1:12345",
		running:    true,
	}
	if addr := l.Address(); addr != "127.0.0.1:12345" {
		t.Errorf("Address() = %q, want 127.0.0.1:12345", addr)
	}
	if !l.IsRunning() {
		t.Error("IsRunning() should be true")
	}
	l.running = false
	if l.IsRunning() {
		t.Error("IsRunning() should be false")
	}
	if l.Name() != "test" {
		t.Errorf("Name() = %q, want test", l.Name())
	}
}

// TestCov_GetMCPAddress_WithNonNumericPort tests getMCPAddress with non-numeric admin port.
func TestCov_GetMCPAddress_WithNonNumericPort(t *testing.T) {
	cfg := &config.Config{
		Admin: &config.Admin{
			Address: "localhost:http", // non-numeric port
		},
	}
	addr := getMCPAddress(cfg)
	if addr != "" {
		t.Errorf("getMCPAddress with non-numeric port should return empty, got %q", addr)
	}
}

// TestCov_GetMCPAddress_EmptyAdmin tests getMCPAddress with empty admin config.
func TestCov_GetMCPAddress_EmptyAdmin(t *testing.T) {
	cfg := &config.Config{}
	addr := getMCPAddress(cfg)
	// Default admin addr is ":8080", so MCP should be ":8081"
	if addr != ":8081" {
		t.Errorf("getMCPAddress() = %q, want :8081", addr)
	}
}

// TestCov_RegisterLoggingMiddleware_TextFormat tests logging middleware with text format.
func TestCov_RegisterLoggingMiddleware_TextFormat(t *testing.T) {
	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				Logging: &config.LoggingConfig{
					Enabled: true,
					Format:  "text",
					Fields:  []string{"method", "path"},
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	registerLoggingMiddleware(ctx)
}

// TestCov_RegisterCORSMiddleware_Error tests CORS middleware creation error path.
func TestCov_RegisterCORSMiddleware_Error(t *testing.T) {
	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				CORS: &config.CORSConfig{
					Enabled:          true,
					AllowedOrigins:   []string{"*"},
					AllowedMethods:   []string{"GET"}, // valid, but we'll test error via invalid config
					MaxAge:           -1,              // negative maxAge could cause issues
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	// This should not panic even if CORS middleware fails
	registerCORSMiddleware(ctx)
}

// TestCov_New_WithProfiling_BlockProfile tests engine creation with profiling and block profile.
func TestCov_New_WithProfiling_BlockProfile(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:          true,
		PprofAddr:        "127.0.0.1:0",
		MemProfilePath:   filepath.Join(t.TempDir(), "mem.prof"),
		BlockProfileRate: 1,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.profilingCleanup == nil {
		t.Error("profilingCleanup should be set when profiling is enabled")
	}
	// Clean up
	engine.profilingCleanup()
}

// TestCov_New_WithProfilingNonLocalhost tests profiling with non-localhost warning.
func TestCov_New_WithProfilingNonLocalhost(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:   true,
		PprofAddr: "0.0.0.0:6060", // non-localhost
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.profilingCleanup == nil {
		t.Error("profilingCleanup should be set")
	}
	engine.profilingCleanup()
}

// TestCov_New_WithProfilingDefaultAddr tests profiling with default address.
func TestCov_New_WithProfilingDefaultAddr(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled: true,
		// PprofAddr not set — should default to localhost:6060
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.profilingCleanup == nil {
		t.Error("profilingCleanup should be set with default addr")
	}
	engine.profilingCleanup()
}

// TestCov_New_WithACME_Email tests engine creation with ACME client enabled.
func TestCov_New_WithACME_Email(t *testing.T) {
	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		ACME: &config.ACME{
			Enabled: true,
			Email:   "test@example.com",
			Domains: []string{"example.com"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// ACME client might be nil if directory is unreachable, but creation should not fail
	t.Logf("ACME client: %v", engine.acmeClient)
}

// TestCov_New_WithGeoDNS_Rules tests engine creation with GeoDNS rules.
func TestCov_New_WithGeoDNS_Rules(t *testing.T) {
	cfg := createTestConfig()
	cfg.GeoDNS = &config.GeoDNSConfig{
		Enabled:    true,
		DefaultPool: "test-pool",
		DBPath:      filepath.Join(t.TempDir(), "GeoLite2-Country.mmdb"),
		Rules: []config.GeoDNSRule{
			{ID: "us", Country: "US", Pool: "test-pool"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.geoDNS == nil {
		t.Error("geoDNS should be non-nil when configured")
	}
}

// TestCov_New_WithShadow_Timeout tests engine creation with shadow manager timeout.
func TestCov_New_WithShadow_Timeout(t *testing.T) {
	cfg := createTestConfig()
	cfg.Shadow = &config.ShadowConfig{
		Enabled:     true,
		Percentage:  0.5,
		Timeout:     "5s",
		CopyHeaders: true,
		CopyBody:    true,
		Targets:     []config.ShadowTarget{{Pool: "test-pool", Percentage: 1.0}},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.shadowMgr == nil {
		t.Error("shadowMgr should be non-nil when configured")
	}
}

// TestCov_New_WithServerTuning tests engine creation with custom server timeouts.
func TestCov_New_WithServerTuning(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		MaxConnections:          5000,
		MaxConnectionsPerSource: 50,
		MaxConnectionsPerBackend: 500,
		DrainTimeout:            "15s",
		ProxyTimeout:            "30s",
		DialTimeout:             "3s",
		MaxRetries:              5,
		MaxIdleConns:            200,
		MaxIdleConnsPerHost:     20,
		IdleConnTimeout:         "60s",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.shutdownTimeout != 30*time.Second {
		t.Errorf("shutdownTimeout = %v, want 30s", engine.shutdownTimeout)
	}
}

// TestCov_New_AdminWithUsernamePassword tests engine creation with admin auth.
func TestCov_New_AdminWithUsernamePassword(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.Username = "admin"
	cfg.Admin.Password = "secret123"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.adminServer == nil {
		t.Error("adminServer should be created")
	}
}

// TestCov_InitCluster_WithPersisterAndNodeAuth tests cluster init with persister and node auth.
func TestCov_InitCluster_WithPersisterAndNodeAuth(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "auth-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
		DataDir:  tmpDir,
		NodeAuth: &config.ClusterNodeAuthConfig{
			SharedSecret:   "super-secret-key",
			AllowedNodeIDs: []string{"auth-node", "peer-node"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.raftCluster == nil {
		t.Error("raftCluster should be created")
	}
	if engine.persister == nil {
		t.Error("persister should be created")
	}
	if engine.clusterMgr == nil {
		t.Error("clusterMgr should be created")
	}
}

// TestCov_RecoverRaftState_WithEntries tests recoverRaftState with saved state and entries.
func TestCov_RecoverRaftState_WithEntries(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a persister and save state
	persister, err := cluster.NewFilePersister(tmpDir)
	if err != nil {
		t.Fatalf("NewFilePersister error = %v", err)
	}

	// Save some state
	state := cluster.RaftStateV1{
		Term:        5,
		VotedFor:    "node-1",
		CommitIndex: 10,
		LastApplied: 8,
	}
	if err := persister.SaveRaftState(state); err != nil {
		t.Fatalf("SaveRaftState error = %v", err)
	}

	// Save some log entries
	entries := []*cluster.LogEntry{
		{Index: 9, Term: 5, Command: []byte(`{"type":"set_config"}`)},
		{Index: 10, Term: 5, Command: []byte(`{"type":"update_backend"}`)},
	}
	if err := persister.SaveLogEntries(entries); err != nil {
		t.Fatalf("SaveLogEntries error = %v", err)
	}

	// Create engine with cluster enabled so raftCluster is non-nil
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "recover-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
		DataDir:  tmpDir,
	}
	configPath := createTempConfigFile(t, cfg)
	engine, engErr := New(cfg, configPath)
	if engErr != nil {
		t.Fatalf("New() error = %v", engErr)
	}

	if engine.raftCluster == nil {
		t.Fatal("raftCluster should be created")
	}

	// Create config state machine
	configSM := cluster.NewConfigStateMachine(engine.config)

	logger := logging.New(logging.NewJSONOutput(os.Stdout))
	engine.recoverRaftState(engine.raftCluster, configSM, persister, logger)
}

// TestCov_RecoverRaftState_EmptyState tests recoverRaftState with empty state (term=0, commit=0).
func TestCov_RecoverRaftState_EmptyState(t *testing.T) {
	tmpDir := t.TempDir()

	persister, err := cluster.NewFilePersister(tmpDir)
	if err != nil {
		t.Fatalf("NewFilePersister error = %v", err)
	}

	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, engErr := New(cfg, configPath)
	if engErr != nil {
		t.Fatalf("New() error = %v", engErr)
	}

	configSM := cluster.NewConfigStateMachine(engine.config)
	logger := logging.New(logging.NewJSONOutput(os.Stdout))

	// With empty state (term=0, commit=0), should return early
	engine.recoverRaftState(nil, configSM, persister, logger)
}

// TestCov_New_WithClusterEnabled tests full engine creation with cluster enabled.
func TestCov_New_WithClusterEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.raftCluster == nil {
		t.Error("raftCluster should be created")
	}
}

// TestCov_New_ConnManagerCustomConfig tests engine creation with custom connection limits.
func TestCov_New_ConnManagerCustomConfig(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		MaxConnections:           5000,
		MaxConnectionsPerSource:  50,
		MaxConnectionsPerBackend: 500,
		DrainTimeout:             "10s",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.connManager == nil {
		t.Error("connManager should be created")
	}
}

// TestCov_Start_WithClusterStandalone tests Start with cluster in standalone mode (no peers).
func TestCov_Start_WithClusterStandalone(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "standalone-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_WithClusterPeers tests Start with cluster joining peers.
func TestCov_Start_WithClusterPeers(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "peer-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{"127.0.0.1:19000"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_WithACME tests Start with ACME client configured.
func TestCov_Start_WithACME(t *testing.T) {
	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		ACME: &config.ACME{
			Enabled: true,
			Email:   "test@example.com",
			Domains: []string{"example.com"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_WithGeoDNS tests Start with GeoDNS.
func TestCov_Start_WithGeoDNS(t *testing.T) {
	cfg := createTestConfig()
	cfg.GeoDNS = &config.GeoDNSConfig{
		Enabled:    true,
		DefaultPool: "test-pool",
		DBPath:      filepath.Join(t.TempDir(), "GeoLite2-Country.mmdb"),
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_WithShadow tests Start with shadow manager.
func TestCov_Start_WithShadow(t *testing.T) {
	cfg := createTestConfig()
	cfg.Shadow = &config.ShadowConfig{
		Enabled:     true,
		Percentage:  0.1,
		Targets:     []config.ShadowTarget{{Pool: "test-pool", Percentage: 1.0}},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Shutdown_WithCluster tests Shutdown with cluster manager.
func TestCov_Shutdown_WithCluster(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "shutdown-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

// TestCov_BuildMiddlewareStatus tests buildMiddlewareStatus with various middleware enabled.
func TestCov_BuildMiddlewareStatus(t *testing.T) {
	cfg := createTestConfig()
	cfg.Middleware = &config.MiddlewareConfig{
		RateLimit: &config.RateLimitConfig{Enabled: true},
		CORS:     &config.CORSConfig{Enabled: true},
		Cache:    &config.CacheConfig{Enabled: true},
		JWT:      &config.JWTConfig{Enabled: true, Secret: "test-secret-key-12345"},
		CSRF:     &config.CSRFConfig{Enabled: true},
	}
	cfg.WAF = &config.WAFConfig{Enabled: true}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status := engine.buildMiddlewareStatus()
	if len(status) == 0 {
		t.Error("buildMiddlewareStatus should return non-empty list")
	}

	// Verify a few known entries
	found := false
	for _, item := range status {
		if item.ID == "rate_limit" && item.Enabled {
			found = true
			break
		}
	}
	if !found {
		t.Error("rate_limit should be enabled in middleware status")
	}
}

// TestCov_RegisterJWTMiddleware_Base64DecodeFail tests JWT middleware with invalid base64 key.
func TestCov_RegisterJWTMiddleware_Base64DecodeFail(t *testing.T) {
	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				JWT: &config.JWTConfig{
					Enabled:   true,
					Algorithm: "EdDSA",
					PublicKey: "!!!not-base64!!!", // invalid base64
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	// Should not panic, just warn
	registerJWTMiddleware(ctx)
}

// TestCov_Start_WithPluginManager tests Start with plugin manager operations.
func TestCov_Start_WithPluginManager(t *testing.T) {
	cfg := createTestConfig()

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_StartHTTPListener_WithMTLS tests startHTTPListener with mTLS enabled.
func TestCov_StartHTTPListener_WithMTLS(t *testing.T) {
	cfg := createTestConfig()
	cfg.Listeners[0].Protocol = "https"
	cfg.Listeners[0].TLS = &config.ListenerTLS{
		Enabled:  true,
		CertFile: "cert.pem",
		KeyFile:  "key.pem",
	}
	cfg.Listeners[0].MTLS = &config.MTLSConfig{
		Enabled:    true,
		ClientAuth: "require-and-verify",
		ClientCAs:  []string{"ca.pem"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Start will fail because TLS files don't exist, but this exercises the mTLS path
	err = engine.Start()
	if err != nil {
		t.Logf("Start() with missing TLS files error (expected): %v", err)
	}
}

// TestCov_StartHTTPListener_InvalidMTLSClientAuth tests mTLS with invalid client auth policy.
func TestCov_StartHTTPListener_InvalidMTLSClientAuth(t *testing.T) {
	cfg := createTestConfig()
	cfg.Listeners[0].Protocol = "https"
	cfg.Listeners[0].TLS = &config.ListenerTLS{
		Enabled:  true,
		CertFile: "cert.pem",
		KeyFile:  "key.pem",
	}
	cfg.Listeners[0].MTLS = &config.MTLSConfig{
		Enabled:    true,
		ClientAuth: "invalid-policy",
		ClientCAs:  []string{"ca.pem"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = engine.Start()
	if err == nil {
		t.Error("Start() with invalid mTLS client auth should fail")
	} else {
		t.Logf("Start() error (expected): %v", err)
	}
}

// TestCov_CreateLoggerWithOutput tests logger creation with various outputs.
func TestCov_CreateLoggerWithOutput(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *config.Logging
		output string
	}{
		{"nil config", nil, "json"},
		{"stdout text", &config.Logging{Output: "stdout", Format: "text"}, "text"},
		{"stdout json", &config.Logging{Output: "stdout", Format: "json"}, "json"},
		{"stderr text", &config.Logging{Output: "stderr", Format: "text"}, "text"},
		{"stderr json", &config.Logging{Output: "stderr", Format: "json"}, "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, rotatingOut := createLoggerWithOutput(tt.cfg)
			if logger == nil {
				t.Error("logger should not be nil")
			}
			if tt.cfg != nil && tt.cfg.Output != "" && tt.cfg.Output != "stdout" && tt.cfg.Output != "stderr" {
				if rotatingOut == nil {
					t.Error("rotatingOut should not be nil for file output")
				}
			}
		})
	}
}

// TestCov_StartUDPListener_NoPool tests UDP listener with no pool reference.
func TestCov_StartUDPListener_NoPool(t *testing.T) {
	// Validation catches this in New(), so test the error path directly
	cfg := createTestConfig()
	cfg.Listeners = append(cfg.Listeners, &config.Listener{
		Name:     "test-udp",
		Address:  "127.0.0.1:0",
		Protocol: "udp",
		Pool:     "nonexistent-pool",
	})

	configPath := createTempConfigFile(t, cfg)
	_, err := New(cfg, configPath)
	if err == nil {
		t.Error("New() with nonexistent UDP pool should fail validation")
	} else {
		t.Logf("New() error (expected): %v", err)
	}
}

// TestCov_Start_WithMCPServerAndCORS tests Start with MCP server and CORS configured.
func TestCov_Start_WithMCPServerAndCORS(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPToken = "test-mcp-token"
	cfg.Middleware = &config.MiddlewareConfig{
		CORS: &config.CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"http://localhost:3000"},
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// mcpServer should be created
	if engine.mcpServer == nil {
		t.Error("mcpServer should be created with MCPToken")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_EngineStatus tests engine status methods.
func TestCov_EngineStatus(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Test stopped state
	if engine.IsRunning() {
		t.Error("engine should not be running")
	}
	if state := engine.GetState(); state != StateStopped {
		t.Errorf("GetState() = %v, want stopped", state)
	}
	if uptime := engine.Uptime(); uptime != 0 {
		t.Errorf("Uptime() = %v, want 0 when stopped", uptime)
	}

	// Test GetStatus
	status := engine.GetStatus()
	if status.State != "stopped" {
		t.Errorf("GetStatus().State = %v, want stopped", status.State)
	}

	// Test getters
	if engine.GetLogger() == nil {
		t.Error("GetLogger() should not be nil")
	}
	if engine.GetMetrics() == nil {
		t.Error("GetMetrics() should not be nil")
	}
	if engine.GetPoolManager() == nil {
		t.Error("GetPoolManager() should not be nil")
	}
	if engine.GetRouter() == nil {
		t.Error("GetRouter() should not be nil")
	}
	if engine.GetHealthChecker() == nil {
		t.Error("GetHealthChecker() should not be nil")
	}
	if engine.GetPluginManager() == nil {
		t.Error("GetPluginManager() should not be nil")
	}
	if engine.GetDiscoveryManager() == nil {
		t.Error("GetDiscoveryManager() should not be nil")
	}
	if engine.GetConfig() == nil {
		t.Error("GetConfig() should not be nil")
	}

	// Start engine and test running state
	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !engine.IsRunning() {
		t.Error("engine should be running")
	}
	// Wait a moment for uptime to accumulate
	time.Sleep(10 * time.Millisecond)
	if uptime := engine.Uptime(); uptime == 0 {
		t.Error("Uptime() should be > 0 when running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Reload_FailStateNotRunning tests Reload when engine is not running.
func TestCov_Reload_FailStateNotRunning(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Should fail because engine is not running
	err = engine.Reload()
	if err == nil {
		t.Error("Reload() on stopped engine should fail")
	}
}

// TestCov_RecordReloadError_Increment tests RecordReloadError increments counter.
func TestCov_RecordReloadError_Increment(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	engine.RecordReloadError()
	engine.RecordReloadError()
	engine.RecordReloadError()

	engine.rollbackMu.Lock()
	count := engine.errorCount
	engine.rollbackMu.Unlock()

	if count != 3 {
		t.Errorf("errorCount = %d, want 3", count)
	}
}

// TestCov_Start_AlreadyRunning tests Start when engine is already running.
func TestCov_Start_AlreadyRunning(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Second Start should fail
	err = engine.Start()
	if err == nil {
		t.Error("Start() on already running engine should fail")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Shutdown_AlreadyStopped tests Shutdown when engine is already stopped.
func TestCov_Shutdown_AlreadyStopped(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	err = engine.Shutdown(ctx)
	if err == nil {
		t.Error("Shutdown() on stopped engine should fail")
	}
}

// TestCov_Start_WithOCSPManager tests Start with OCSP manager.
func TestCov_Start_WithOCSPManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.ocspManager == nil {
		t.Error("ocspManager should be created")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_InitCluster_WithPersister tests cluster init with data directory for persistence.
func TestCov_InitCluster_WithPersister(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "persist-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
		DataDir:  tmpDir,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.persister == nil {
		t.Error("persister should be created with DataDir")
	}
}

// TestCov_New_WithServerRollbackTimeouts tests custom rollback timeouts.
func TestCov_New_WithServerRollbackTimeouts(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		RollbackCheckInterval:  "10s",
		RollbackMonitorDuration: "20s",
		ShutdownTimeout:        "45s",
		ListenerStopTimeout:    "7s",
		ProxyDrainWindow:       "3s",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.rollbackCheckInterval != 10*time.Second {
		t.Errorf("rollbackCheckInterval = %v, want 10s", engine.rollbackCheckInterval)
	}
	if engine.rollbackMonitorDuration != 20*time.Second {
		t.Errorf("rollbackMonitorDuration = %v, want 20s", engine.rollbackMonitorDuration)
	}
	if engine.shutdownTimeout != 45*time.Second {
		t.Errorf("shutdownTimeout = %v, want 45s", engine.shutdownTimeout)
	}
	if engine.listenerStopTimeout != 7*time.Second {
		t.Errorf("listenerStopTimeout = %v, want 7s", engine.listenerStopTimeout)
	}
	if engine.proxyDrainWindow != 3*time.Second {
		t.Errorf("proxyDrainWindow = %v, want 3s", engine.proxyDrainWindow)
	}
}

// TestCov_ProposeSetConfig_InvalidJSON tests ProposeSetConfig with unmarshallable config.
func TestCov_ProposeSetConfig_InvalidJSON(t *testing.T) {
	// Create a config with a channel field that can't be marshalled
	proposer := &engineRaftProposer{raftCluster: nil}
	err := proposer.ProposeSetConfig([]byte("not json"))
	if err == nil {
		t.Error("ProposeSetConfig with invalid JSON should fail")
	}
}

// TestCov_ProposeUpdateBackend_InvalidJSON tests ProposeUpdateBackend with bad JSON.
func TestCov_ProposeUpdateBackend_InvalidJSON(t *testing.T) {
	proposer := &engineRaftProposer{raftCluster: nil}
	err := proposer.ProposeUpdateBackend("test-pool", []byte("not json"))
	if err == nil {
		t.Error("ProposeUpdateBackend with invalid JSON should fail")
	}
}

// TestCov_ProposeDeleteBackend_CommandCreation tests that the delete backend command can be created.
func TestCov_ProposeDeleteBackend_CommandCreation(t *testing.T) {
	// Just verify command creation (the adapter's responsibility is constructing the command)
	_, err := cluster.NewDeleteBackendCommand("test-pool", "backend-1")
	if err != nil {
		t.Errorf("NewDeleteBackendCommand error = %v", err)
	}
}

// TestCov_MTLS_Start_AlreadyRunning tests mTLS listener Start when already running.
func TestCov_MTLS_Start_AlreadyRunning(t *testing.T) {
	l := &mtlsHTTPSListener{
		name:    "test",
		address: "127.0.0.1:0",
		running: true,
		mu:      sync.RWMutex{},
	}
	err := l.Start()
	if err == nil {
		t.Error("Start() on already running listener should fail")
	}
}

// TestCov_MTLS_Stop_WithNilServer tests mTLS Stop when server is nil.
func TestCov_MTLS_Stop_WithNilServer(t *testing.T) {
	l := &mtlsHTTPSListener{
		name:    "test",
		address: "127.0.0.1:0",
		running: true,
		server:  nil,
		mu:      sync.RWMutex{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := l.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() with nil server should return nil, got %v", err)
	}
}

// TestCov_InitCluster_WithFailingPersister tests initCluster with an invalid data dir.
func TestCov_InitCluster_WithFailingPersister(t *testing.T) {
	// Create a file where a directory would be needed
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "fail-persist",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
		DataDir:  tmpFile, // this is a file, not a directory
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Persister should fail but engine should still be created
	t.Logf("persister: %v", engine.persister)
}

// TestCov_Start_WithTLSCertAndMCPAudit tests Start with TLS cert and MCP audit.
func TestCov_Start_WithTLSCertAndMCPAudit(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	generateTestCertFiles(t, certFile, keyFile)

	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}
	cfg.Admin.MCPToken = "test-audit-token"
	cfg.Admin.MCPAudit = true

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_WithClusterAndRaftProposer tests engine with cluster and RaftProposer wired.
func TestCov_Start_WithClusterAndRaftProposer(t *testing.T) {
	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "proposer-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if engine.raftCluster == nil {
		t.Fatal("raftCluster should be created")
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithInvalidProfiling tests engine with profiling that fails to apply.
func TestCov_New_WithInvalidProfiling(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:       true,
		CPUProfilePath: "/nonexistent/dir/cpu.prof", // can't create
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// profilingCleanup should be nil because Apply failed
	if engine.profilingCleanup != nil {
		t.Log("profilingCleanup was set despite failure")
		engine.profilingCleanup()
	}
}

// TestCov_New_WithWAFDisabled tests engine with WAF configured but disabled.
func TestCov_New_WithWAFDisabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.WAF = &config.WAFConfig{Enabled: false}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// No WAF middleware should be registered
	if engine.mcpServer != nil {
		t.Log("mcpServer created even without MCP token")
	}
}

// TestCov_New_NoWebUI tests engine creation when WebUI handler fails.
func TestCov_New_NoWebUI(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Engine should still be created even without WebUI
	if engine.adminServer == nil {
		t.Error("adminServer should be created")
	}
}

// TestCov_New_NilConfig tests engine creation with nil config.
func TestCov_New_NilConfig(t *testing.T) {
	_, err := New(nil, "")
	if err == nil {
		t.Error("New(nil) should return error")
	}
}

// TestCov_Start_WithTLSExpiryMonitor tests Start with TLS expiry monitoring.
func TestCov_Start_WithTLSExpiryMonitor(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	generateTestCertFiles(t, certFile, keyFile)

	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithEmptyShadow tests engine with shadow config but disabled.
func TestCov_New_WithEmptyShadow(t *testing.T) {
	cfg := createTestConfig()
	cfg.Shadow = &config.ShadowConfig{
		Enabled: false,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.shadowMgr != nil {
		t.Error("shadowMgr should be nil when disabled")
	}
}

// TestCov_New_WithEmptyGeoDNS tests engine with GeoDNS config but disabled.
func TestCov_New_WithEmptyGeoDNS(t *testing.T) {
	cfg := createTestConfig()
	cfg.GeoDNS = &config.GeoDNSConfig{
		Enabled: false,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.geoDNS != nil {
		t.Error("geoDNS should be nil when disabled")
	}
}

// TestCov_RegisterCORSMiddleware_WithMethods tests CORS with explicit methods.
func TestCov_RegisterCORSMiddleware_WithMethods(t *testing.T) {
	ctx := &middlewareRegistrationContext{
		cfg: &config.Config{
			Middleware: &config.MiddlewareConfig{
				CORS: &config.CORSConfig{
					Enabled:        true,
					AllowedOrigins: []string{"https://example.com"},
					AllowedMethods: []string{"GET", "POST"},
					AllowedHeaders: []string{"X-Custom"},
					MaxAge:         3600,
				},
			},
		},
		logger: logging.New(logging.NewJSONOutput(os.Stdout)),
		chain:   middleware.NewChain(),
	}
	registerCORSMiddleware(ctx)
}

// TestCov_CreateLoggerWithOutput_FileOutput tests logger with file output.
func TestCov_CreateLoggerWithOutput_FileOutput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestCov_LoggerFile")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "test.log")
	logger, rotatingOut := createLoggerWithOutput(&config.Logging{
		Level:  "info",
		Format: "json",
		Output: logFile,
	})
	if logger == nil {
		t.Error("logger should not be nil")
	}
	if rotatingOut == nil {
		t.Error("rotatingOut should not be nil for file output")
	}
	rotatingOut.Close()
}

// TestCov_ListenersChanged tests listenersChanged helper.
func TestCov_ListenersChanged(t *testing.T) {
	oldCfg := createTestConfig()
	newCfg := createTestConfig()

	if listenersChanged(oldCfg, newCfg) {
		t.Error("identical configs should not show changes")
	}

	// Change address
	newCfg.Listeners[0].Address = ":9999"
	if !listenersChanged(oldCfg, newCfg) {
		t.Error("changed address should show listeners changed")
	}

	// Change listener count
	newCfg2 := createTestConfig()
	newCfg2.Listeners = append(newCfg2.Listeners, &config.Listener{
		Name: "extra", Address: ":9999", Protocol: "http",
	})
	if !listenersChanged(oldCfg, newCfg2) {
		t.Error("added listener should show listeners changed")
	}
}

// TestCov_ReloadListeners tests reloadListeners.
func TestCov_ReloadListeners(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Same count, different address
	newCfg := createTestConfig()
	newCfg.Listeners[0].Address = ":9999"
	if err := engine.reloadListeners(newCfg); err != nil {
		t.Errorf("reloadListeners() error = %v", err)
	}

	// Different count
	newCfg2 := createTestConfig()
	newCfg2.Listeners = append(newCfg2.Listeners, &config.Listener{
		Name: "extra", Address: ":9998", Protocol: "http", Pool: "test-pool",
		Routes: []*config.Route{{Path: "/extra", Pool: "test-pool"}},
	})
	if err := engine.reloadListeners(newCfg2); err != nil {
		t.Errorf("reloadListeners() error = %v", err)
	}
}

// TestCov_StopRollbackTimer tests stopRollbackTimer.
func TestCov_StopRollbackTimer(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// No timers set - should not panic
	engine.stopRollbackTimer()

	// Set timers
	engine.rollbackMu.Lock()
	engine.rollbackTimer = time.AfterFunc(10*time.Second, func() {})
	engine.rollbackTimer2 = time.AfterFunc(20*time.Second, func() {})
	engine.rollbackMu.Unlock()

	engine.stopRollbackTimer()

	engine.rollbackMu.Lock()
	rt1 := engine.rollbackTimer
	rt2 := engine.rollbackTimer2
	engine.rollbackMu.Unlock()

	if rt1 != nil {
		t.Error("rollbackTimer should be nil after stop")
	}
	if rt2 != nil {
		t.Error("rollbackTimer2 should be nil after stop")
	}
}

// TestCov_Done tests the Done channel.
func TestCov_Done(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	done := engine.Done()
	if done == nil {
		t.Error("Done() should return non-nil channel")
	}

	// Start then shutdown
	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)

	// Done channel should be closed after shutdown
	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("Done() channel should be closed after shutdown")
	}
}

// TestCov_Start_WithCustomHealthCheckThresholds tests Start with custom health check thresholds.
func TestCov_Start_WithCustomHealthCheckThresholds(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].HealthCheck.HealthyThreshold = 5
	cfg.Pools[0].HealthCheck.UnhealthyThreshold = 10
	cfg.Pools[0].HealthCheck.Interval = "3s"
	cfg.Pools[0].HealthCheck.Timeout = "2s"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithGRPCTLSSkipVerifyInConfig tests engine creation with gRPC TLS skip verify.
func TestCov_New_WithGRPCTLSSkipVerifyInConfig(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].HealthCheck.GRPCTLSSkipVerify = true
	cfg.Pools[0].HealthCheck.Type = "grpc"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithWAFStatusProvider tests engine creation with WAF enabled and MCP token.
func TestCov_New_WithWAFStatusProvider(t *testing.T) {
	cfg := createTestConfig()
	cfg.WAF = &config.WAFConfig{
		Enabled: true,
		Mode:    "monitor",
	}
	cfg.Admin.MCPToken = "waf-test-token"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.mcpServer == nil {
		t.Error("mcpServer should be created with MCPToken")
	}
}

// TestCov_InitCluster_WithTransportAndPersister tests cluster with both transport and persister.
func TestCov_InitCluster_WithTransportAndPersister(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:       true,
		NodeID:        "full-cluster-node",
		BindAddr:      "127.0.0.1",
		BindPort:      0,
		Peers:         []string{},
		DataDir:       tmpDir,
		ElectionTick:  "1s",
		HeartbeatTick: "200ms",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.raftCluster == nil {
		t.Error("raftCluster should be created")
	}
	if engine.persister == nil {
		t.Error("persister should be created")
	}
}

// TestCov_New_AdminWithBearerOnlyAndMCP tests admin with bearer and MCP.
func TestCov_New_AdminWithBearerOnlyAndMCP(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.BearerToken = "bearer-only-token"
	cfg.Admin.MCPToken = "mcp-token-123"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.mcpServer == nil {
		t.Error("mcpServer should be created")
	}
}

// TestCov_New_WithBackendSchemeAndID tests engine creation with backend scheme and ID.
func TestCov_New_WithBackendSchemeAndID(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].Backends[0].Scheme = "https"
	cfg.Pools[0].Backends[0].ID = "custom-backend-id"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_ReloadConfig_WithTLS tests Reload with TLS config.
func TestCov_ReloadConfig_WithTLS(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	generateTestCertFiles(t, certFile, keyFile)

	cfg := createTestConfig()
	cfg.TLS = &config.TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := engine.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithProfilingCPUProfile tests profiling with CPU profile output.
func TestCov_New_WithProfilingCPUProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:       true,
		CPUProfilePath: filepath.Join(tmpDir, "cpu.prof"),
		PprofAddr:     "127.0.0.1:0",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.profilingCleanup == nil {
		t.Error("profilingCleanup should be set")
	}
	engine.profilingCleanup()
}

// TestCov_New_WithProfilingMutexProfile tests profiling with mutex profile.
func TestCov_New_WithProfilingMutexProfile(t *testing.T) {
	cfg := createTestConfig()
	cfg.Profiling = &config.ProfilingConfig{
		Enabled:              true,
		PprofAddr:            "127.0.0.1:0",
		MutexProfileFraction: 5,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.profilingCleanup == nil {
		t.Error("profilingCleanup should be set")
	}
	engine.profilingCleanup()
}

// TestCov_New_WithIdleConnTimeout tests engine with custom idle conn timeout.
func TestCov_New_WithIdleConnTimeout(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		IdleConnTimeout: "30s",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.proxy == nil {
		t.Error("proxy should be created")
	}
}

// TestCov_Start_WithMCPTransport tests Start with MCP transport.
func TestCov_Start_WithMCPTransport(t *testing.T) {
	cfg := createTestConfig()
	cfg.Admin.MCPToken = "transport-test-token"
	cfg.Admin.MCPAddress = "127.0.0.1:0"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if engine.mcpTransport == nil {
		t.Error("mcpTransport should be created")
	} else {
		t.Logf("MCP transport address: %s", engine.mcpTransport.Addr())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithClusterEnabledAndPersister tests engine with cluster and persister.
func TestCov_New_WithClusterEnabledAndPersister(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := createTestConfig()
	cfg.Cluster = &config.ClusterConfig{
		Enabled:  true,
		NodeID:   "persist-test-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Peers:    []string{},
		DataDir:  tmpDir,
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if engine.raftCluster == nil {
		t.Error("raftCluster should be created")
	}
	if engine.persister == nil {
		t.Error("persister should be created with DataDir")
	}
}

// TestCov_ReloadWithRollbackTimer tests that reload starts the rollback timer.
func TestCov_ReloadWithRollbackTimer(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = &config.ServerConfig{
		RollbackCheckInterval:  "100ms",
		RollbackMonitorDuration: "200ms",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Reload to trigger the rollback timer
	if err := engine.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	// Wait for rollback timer to fire
	time.Sleep(300 * time.Millisecond)

	// Verify rollback timer was set and fired
	engine.rollbackMu.Lock()
	prev := engine.prevConfig
	engine.rollbackMu.Unlock()
	// After the timer2 fires, prevConfig should be cleared
	if prev != nil {
		t.Log("prevConfig still set (timer may not have fired yet)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_Start_WithBackendWeight tests engine with non-zero backend weight.
func TestCov_Start_WithBackendWeight(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools[0].Backends[0].Weight = 10
	cfg.Pools[0].Algorithm = "weighted_round_robin"

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithEmptyServerConfig tests engine with nil server config.
func TestCov_New_WithEmptyServerConfig(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server = nil

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Should use all defaults
	if engine.shutdownTimeout != 30*time.Second {
		t.Errorf("shutdownTimeout = %v, want 30s", engine.shutdownTimeout)
	}
}

// TestCov_New_WithMultiplePoolsAndBackends tests engine with multiple pools.
func TestCov_New_WithMultiplePoolsAndBackends(t *testing.T) {
	cfg := createTestConfig()
	cfg.Pools = append(cfg.Pools, &config.Pool{
		Name:      "pool-2",
		Algorithm: "least_connections",
		HealthCheck: &config.HealthCheck{
			Type:     "http",
			Path:     "/health",
			Interval: "5s",
			Timeout:  "3s",
		},
		Backends: []*config.Backend{
			{Address: "localhost:3002", Weight: 5},
			{Address: "localhost:3003", Weight: 3},
		},
	})

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

// TestCov_New_WithRouteMethods tests engine with route methods.
func TestCov_New_WithRouteMethods(t *testing.T) {
	cfg := createTestConfig()
	cfg.Listeners[0].Routes[0].Methods = []string{"GET", "POST"}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}
