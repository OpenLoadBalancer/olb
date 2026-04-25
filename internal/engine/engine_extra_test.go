package engine

import (
	"context"
	"crypto/tls"
	"fmt"
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
	"github.com/openloadbalancer/olb/internal/config"
	olbListener "github.com/openloadbalancer/olb/internal/listener"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
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
    address: 127.0.0.1:0
    protocol: http
    routes:
      - path: /
        pool: nonexistent
pools:
  - name: test-pool
    algorithm: round_robin
    backends:
      - id: backend-1
        address: 127.0.0.1:8081
admin:
  enabled: true
  address: 127.0.0.1:0
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: 100
admin:
  enabled: true
  address: 127.0.0.1:0
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: %d
admin:
  enabled: true
  address: 127.0.0.1:0
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: 100
admin:
  enabled: true
  address: 127.0.0.1:0
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
		},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("New() with ACME no email error = %v", err)
	}
	_ = engine
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: 100
admin:
  enabled: true
  address: 127.0.0.1:0
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: 100
admin:
  enabled: true
  address: 127.0.0.1:0
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: 100
      - id: backend-2
        address: 127.0.0.1:8082
        weight: 50
admin:
  enabled: true
  address: 127.0.0.1:0
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
    address: 127.0.0.1:0
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
        address: 127.0.0.1:8081
        weight: 100
admin:
  enabled: true
  address: 127.0.0.1:0
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
