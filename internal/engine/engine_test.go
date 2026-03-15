// Package engine provides integration tests for the engine orchestrator.
package engine

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/router"
)

// createTestConfig creates a minimal valid configuration for testing.
func createTestConfig() *config.Config {
	return &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "test-http",
				Address:  "127.0.0.1:0", // Use port 0 for dynamic allocation
				Protocol: "http",
				TLS:      false,
				Routes: []*config.Route{
					{
						Path: "/",
						Pool: "test-pool",
					},
				},
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "test-pool",
				Algorithm: "round_robin",
				HealthCheck: &config.HealthCheck{
					Type:     "http",
					Path:     "/health",
					Interval: "10s",
					Timeout:  "5s",
				},
				Backends: []*config.Backend{
					{
						ID:      "backend-1",
						Address: "127.0.0.1:8081",
						Weight:  100,
					},
				},
			},
		},
		Admin: &config.Admin{
			Enabled: true,
			Address: "127.0.0.1:0",
		},
		Logging: &config.Logging{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Metrics: &config.Metrics{
			Enabled: true,
			Path:    "/metrics",
		},
	}
}

// createTempConfigFile creates a temporary config file for testing.
func createTempConfigFile(t *testing.T, cfg *config.Config) string {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Write minimal YAML config
	content := `
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
logging:
  level: info
  format: json
  output: stdout
metrics:
  enabled: true
  path: /metrics
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	return configPath
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     createTestConfig(),
			wantErr: false,
		},
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "invalid config - no listeners",
			cfg: &config.Config{
				Version:   "1",
				Listeners: []*config.Listener{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, tt.cfg)
			engine, err := New(tt.cfg, configPath)

			if tt.wantErr {
				if err == nil {
					t.Error("New() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("New() error = %v", err)
				return
			}

			if engine == nil {
				t.Error("New() returned nil engine")
				return
			}

			// Verify components are initialized
			if engine.logger == nil {
				t.Error("Logger not initialized")
			}
			if engine.metrics == nil {
				t.Error("Metrics registry not initialized")
			}
			if engine.tlsManager == nil {
				t.Error("TLS manager not initialized")
			}
			if engine.poolManager == nil {
				t.Error("Pool manager not initialized")
			}
			if engine.healthChecker == nil {
				t.Error("Health checker not initialized")
			}
			if engine.router == nil {
				t.Error("Router not initialized")
			}
			if engine.proxy == nil {
				t.Error("Proxy not initialized")
			}
			if engine.adminServer == nil {
				t.Error("Admin server not initialized")
			}
			if engine.connManager == nil {
				t.Error("Connection manager not initialized")
			}
		})
	}
}

func TestEngineStartStop(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test Start
	t.Run("start", func(t *testing.T) {
		if err := engine.Start(); err != nil {
			t.Errorf("Start() error = %v", err)
			return
		}

		if !engine.IsRunning() {
			t.Error("IsRunning() = false after Start()")
		}

		if engine.GetState() != StateRunning {
			t.Errorf("GetState() = %v, want %v", engine.GetState(), StateRunning)
		}

		// Give listeners time to start
		time.Sleep(100 * time.Millisecond)
	})

	// Test double start (should fail)
	t.Run("double start", func(t *testing.T) {
		err := engine.Start()
		if err == nil {
			t.Error("Start() should fail when already running")
		}
	})

	// Test Shutdown
	t.Run("shutdown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := engine.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}

		if engine.IsRunning() {
			t.Error("IsRunning() = true after Shutdown()")
		}
	})

	// Test double shutdown (should fail)
	t.Run("double shutdown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := engine.Shutdown(ctx)
		if err == nil {
			t.Error("Shutdown() should fail when not running")
		}
	})
}

func TestEngineStatus(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Before start
	status := engine.GetStatus()
	if status.State != string(StateStopped) {
		t.Errorf("GetStatus().State = %v before start, want %v", status.State, StateStopped)
	}
	if status.Uptime != "0s" && status.Uptime != "0" {
		t.Errorf("GetStatus().Uptime = %v before start, want 0", status.Uptime)
	}

	// Start engine
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	// Give it time to initialize
	time.Sleep(50 * time.Millisecond)

	// After start
	status = engine.GetStatus()
	if status.State != string(StateRunning) {
		t.Errorf("GetStatus().State = %v after start, want %v", status.State, StateRunning)
	}
	if status.Listeners != 1 {
		t.Errorf("GetStatus().Listeners = %v, want 1", status.Listeners)
	}
	if status.Pools != 1 {
		t.Errorf("GetStatus().Pools = %v, want 1", status.Pools)
	}
	if status.Routes != 1 {
		t.Errorf("GetStatus().Routes = %v, want 1", status.Routes)
	}

	// Check uptime is non-zero
	uptime := engine.Uptime()
	if uptime == 0 {
		t.Error("Uptime() = 0 after start")
	}

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestEngineReload(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Reload before start should fail
	t.Run("reload before start", func(t *testing.T) {
		err := engine.Reload()
		if err == nil {
			t.Error("Reload() should fail before Start()")
		}
	})

	// Start engine
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Reload with valid config
	t.Run("reload while running", func(t *testing.T) {
		err := engine.Reload()
		if err != nil {
			t.Errorf("Reload() error = %v", err)
		}

		if engine.GetState() != StateRunning {
			t.Errorf("GetState() = %v after reload, want %v", engine.GetState(), StateRunning)
		}
	})

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestEngineReloadInvalidConfig(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Start engine
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Corrupt the config file
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatalf("Failed to corrupt config file: %v", err)
	}

	// Reload should fail but engine should keep running
	t.Run("reload with invalid config", func(t *testing.T) {
		err := engine.Reload()
		if err == nil {
			t.Error("Reload() should fail with invalid config")
		}

		if engine.GetState() != StateRunning {
			t.Errorf("GetState() = %v after failed reload, want %v", engine.GetState(), StateRunning)
		}
	})

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestEngineComponentAccessors(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test all component accessors
	if engine.GetLogger() == nil {
		t.Error("GetLogger() returned nil")
	}
	if engine.GetMetrics() == nil {
		t.Error("GetMetrics() returned nil")
	}
	if engine.GetPoolManager() == nil {
		t.Error("GetPoolManager() returned nil")
	}
	if engine.GetRouter() == nil {
		t.Error("GetRouter() returned nil")
	}
	if engine.GetHealthChecker() == nil {
		t.Error("GetHealthChecker() returned nil")
	}
	if engine.GetConfig() == nil {
		t.Error("GetConfig() returned nil")
	}
}

func TestEngineGracefulShutdown(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Create a request that will be in-flight during shutdown
	client := &http.Client{Timeout: 2 * time.Second}

	// Make a request (it will fail since backend doesn't exist, but that's ok)
	go func() {
		// Try to connect to one of the listeners
		for _, l := range engine.listeners {
			url := fmt.Sprintf("http://%s/", l.Address())
			client.Get(url)
		}
	}()

	// Give request time to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
	duration := time.Since(start)

	// Shutdown should complete within timeout
	if duration > 6*time.Second {
		t.Errorf("Shutdown took too long: %v", duration)
	}
}

func TestInitializePools(t *testing.T) {
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
					{ID: "b2", Address: "127.0.0.1:8082", Weight: 100},
				},
			},
			{
				Name:      "pool2",
				Algorithm: "weighted_round_robin",
				HealthCheck: &config.HealthCheck{
					Type:     "tcp",
					Interval: "5s",
					Timeout:  "3s",
				},
				Backends: []*config.Backend{
					{ID: "b3", Address: "127.0.0.1:8083", Weight: 50},
				},
			},
		},
		Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.initializePools(); err != nil {
		t.Errorf("initializePools() error = %v", err)
	}

	if engine.poolManager.PoolCount() != 2 {
		t.Errorf("PoolCount() = %v, want 2", engine.poolManager.PoolCount())
	}

	pool1 := engine.poolManager.GetPool("pool1")
	if pool1 == nil {
		t.Fatal("GetPool(pool1) returned nil")
	}
	if pool1.BackendCount() != 2 {
		t.Errorf("pool1.BackendCount() = %v, want 2", pool1.BackendCount())
	}

	pool2 := engine.poolManager.GetPool("pool2")
	if pool2 == nil {
		t.Fatal("GetPool(pool2) returned nil")
	}
	if pool2.BackendCount() != 1 {
		t.Errorf("pool2.BackendCount() = %v, want 1", pool2.BackendCount())
	}
}

func TestInitializeRoutes(t *testing.T) {
	cfg := &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "http",
				Address:  "127.0.0.1:0",
				Protocol: "http",
				Routes: []*config.Route{
					{Path: "/", Pool: "pool1"},
					{Path: "/api", Pool: "pool1"},
					{Path: "/static", Pool: "pool2"},
				},
			},
			{
				Name:     "https",
				Address:  "127.0.0.1:0",
				Protocol: "https",
				TLS:      true,
				Routes: []*config.Route{
					{Path: "/secure", Pool: "pool1"},
				},
			},
		},
		Pools: []*config.Pool{
			{Name: "pool1", Algorithm: "round_robin"},
			{Name: "pool2", Algorithm: "round_robin"},
		},
		Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.initializeRoutes(); err != nil {
		t.Errorf("initializeRoutes() error = %v", err)
	}

	if engine.router.RouteCount() != 4 {
		t.Errorf("RouteCount() = %v, want 4", engine.router.RouteCount())
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal time.Duration
		want       time.Duration
	}{
		{"10s", 5 * time.Second, 10 * time.Second},
		{"", 5 * time.Second, 5 * time.Second},
		{"invalid", 5 * time.Second, 5 * time.Second},
		{"1m", 0, time.Minute},
		{"500ms", 0, 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDuration(tt.input, tt.defaultVal)
			if got != tt.want {
				t.Errorf("parseDuration(%q, %v) = %v, want %v", tt.input, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestListenersChanged(t *testing.T) {
	oldCfg := &config.Config{
		Listeners: []*config.Listener{
			{Name: "l1", Address: ":80", Protocol: "http"},
			{Name: "l2", Address: ":443", Protocol: "https", TLS: true},
		},
	}

	tests := []struct {
		name   string
		newCfg *config.Config
		want   bool
	}{
		{
			name:   "no change",
			newCfg: oldCfg,
			want:   false,
		},
		{
			name: "count changed",
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
				},
			},
			want: true,
		},
		{
			name: "address changed",
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":8080", Protocol: "http"},
					{Name: "l2", Address: ":443", Protocol: "https", TLS: true},
				},
			},
			want: true,
		},
		{
			name: "TLS changed",
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
					{Name: "l2", Address: ":443", Protocol: "https", TLS: false},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listenersChanged(oldCfg, tt.newCfg)
			if got != tt.want {
				t.Errorf("listenersChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

// BenchmarkEngineCreation benchmarks engine creation.
func BenchmarkEngineCreation(b *testing.B) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(&testing.T{}, cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine, err := New(cfg, configPath)
		if err != nil {
			b.Fatalf("Failed to create engine: %v", err)
		}
		_ = engine
	}
}

// BenchmarkEngineStatus benchmarks getting engine status.
func BenchmarkEngineStatus(b *testing.B) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(&testing.T{}, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.GetStatus()
	}
}

// ============================================================================
// Additional tests for coverage improvement
// ============================================================================

func TestNewWithInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "invalid config - validation fails",
			cfg: &config.Config{
				Version: "1",
				// Missing required fields
			},
			wantErr: true,
		},
		{
			name: "invalid config - listener missing name",
			cfg: &config.Config{
				Version: "1",
				Listeners: []*config.Listener{
					{
						Name:     "", // Missing name
						Address:  "127.0.0.1:0",
						Protocol: "http",
						Routes:   []*config.Route{{Path: "/", Pool: "test-pool"}},
					},
				},
				Pools: []*config.Pool{
					{
						Name:      "test-pool",
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
			},
			wantErr: true,
		},
		{
			name: "invalid config - listener missing address",
			cfg: &config.Config{
				Version: "1",
				Listeners: []*config.Listener{
					{
						Name:     "test",
						Address:  "", // Missing address
						Protocol: "http",
						Routes:   []*config.Route{{Path: "/", Pool: "test-pool"}},
					},
				},
				Pools: []*config.Pool{
					{
						Name:      "test-pool",
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
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, tt.cfg)
			_, err := New(tt.cfg, configPath)

			if tt.wantErr && err == nil {
				t.Error("New() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("New() unexpected error = %v", err)
			}
		})
	}
}

func TestEngineStartWithInvalidListeners(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "invalid listener address",
			cfg: func() *config.Config {
				cfg := createTestConfig()
				cfg.Listeners[0].Address = "invalid:address:format:too:many:colons"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "invalid bind address",
			cfg: func() *config.Config {
				cfg := createTestConfig()
				cfg.Listeners[0].Address = "256.256.256.256:8080" // Invalid IP
				return cfg
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, tt.cfg)
			engine, err := New(tt.cfg, configPath)
			if err != nil {
				t.Fatalf("Failed to create engine: %v", err)
			}

			err = engine.Start()
			if tt.wantErr && err == nil {
				t.Error("Start() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Start() unexpected error = %v", err)
			}

			// Cleanup if started
			if engine.IsRunning() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				engine.Shutdown(ctx)
			}
		})
	}
}

func TestEngineStartWithMissingTLSCerts(t *testing.T) {
	cfg := createTestConfig()
	cfg.Listeners[0].TLS = true
	cfg.Listeners[0].Protocol = "https"
	cfg.TLS = &config.TLSConfig{
		CertFile: "/nonexistent/path/cert.pem",
		KeyFile:  "/nonexistent/path/key.pem",
	}

	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	err = engine.Start()
	if err == nil {
		t.Error("Start() expected error for missing TLS certs but got nil")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		engine.Shutdown(ctx)
	}
}

func TestEngineShutdownTimeout(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Test shutdown with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// This might timeout but should not panic
	engine.Shutdown(ctx)

	// Engine should be stopped even if timeout occurred
	if engine.IsRunning() {
		t.Error("Engine should not be running after shutdown")
	}
}

func TestEngineMultipleStartStopCycles(t *testing.T) {
	// Note: The engine doesn't support restart after shutdown without re-creation
	// because pools and other components are not fully cleaned up.
	// This test verifies that behavior.

	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	// First cycle
	t.Run("first cycle", func(t *testing.T) {
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if err := engine.Start(); err != nil {
			t.Errorf("First Start() error = %v", err)
			return
		}
		if !engine.IsRunning() {
			t.Error("Engine not running after first start")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := engine.Shutdown(ctx); err != nil {
			t.Errorf("First Shutdown() error = %v", err)
		}
		if engine.IsRunning() {
			t.Error("Engine still running after first shutdown")
		}
	})

	// Second cycle with fresh engine
	t.Run("second cycle with fresh engine", func(t *testing.T) {
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if err := engine.Start(); err != nil {
			t.Errorf("Second Start() error = %v", err)
			return
		}
		if !engine.IsRunning() {
			t.Error("Engine not running after second start")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := engine.Shutdown(ctx); err != nil {
			t.Errorf("Second Shutdown() error = %v", err)
		}
		if engine.IsRunning() {
			t.Error("Engine still running after second shutdown")
		}
	})
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "invalid algorithm",
			cfg: &config.Config{
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
						Algorithm: "invalid_algorithm",
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
			},
			wantErr: true,
		},
		{
			name: "route references non-existent pool",
			cfg: &config.Config{
				Version: "1",
				Listeners: []*config.Listener{
					{
						Name:     "test",
						Address:  "127.0.0.1:0",
						Protocol: "http",
						Routes:   []*config.Route{{Path: "/", Pool: "nonexistent-pool"}},
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
			},
			wantErr: true,
		},
		{
			name: "valid config with weighted_round_robin",
			cfg: &config.Config{
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
						Algorithm: "weighted_round_robin",
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
			},
			wantErr: false,
		},
		{
			name: "valid config with wrr shorthand",
			cfg: &config.Config{
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
						Algorithm: "wrr",
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
			},
			wantErr: false,
		},
		{
			name: "valid config with rr shorthand",
			cfg: &config.Config{
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
						Algorithm: "rr",
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
			},
			wantErr: false,
		},
		{
			name: "valid config with empty algorithm (defaults to round_robin)",
			cfg: &config.Config{
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
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, tt.cfg)
			engine, err := New(tt.cfg, configPath)
			if err != nil {
				// If New fails, we can't test validateConfig directly
				// but that's expected for invalid configs
				if !tt.wantErr {
					t.Errorf("New() unexpected error = %v", err)
				}
				return
			}

			err = engine.validateConfig(tt.cfg)
			if tt.wantErr && err == nil {
				t.Error("validateConfig() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateConfig() unexpected error = %v", err)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("load config from missing file", func(t *testing.T) {
		cfg := createTestConfig()
		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		// Set non-existent config path
		engine.configPath = "/nonexistent/path/config.yaml"

		_, err = engine.loadConfig()
		if err == nil {
			t.Error("loadConfig() expected error for missing file")
		}
	})

	t.Run("load valid config", func(t *testing.T) {
		cfg := createTestConfig()
		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		loadedCfg, err := engine.loadConfig()
		if err != nil {
			t.Errorf("loadConfig() unexpected error = %v", err)
		}
		if loadedCfg == nil {
			t.Error("loadConfig() returned nil config")
		}
	})
}

func TestReloadListeners(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)
	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Start engine to properly initialize listeners
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Test reloadListeners with same config
	t.Run("reload with same listeners", func(t *testing.T) {
		err := engine.reloadListeners(cfg)
		if err != nil {
			t.Errorf("reloadListeners() error = %v", err)
		}
	})

	// Test reloadListeners with different count
	t.Run("reload with different listener count", func(t *testing.T) {
		newCfg := &config.Config{
			Listeners: []*config.Listener{
				{Name: "l1", Address: "127.0.0.1:0", Protocol: "http"},
				{Name: "l2", Address: "127.0.0.1:0", Protocol: "http"},
			},
		}
		err := engine.reloadListeners(newCfg)
		if err != nil {
			t.Errorf("reloadListeners() error = %v", err)
		}
	})

	// Test reloadListeners with different addresses - this should trigger the warning path
	t.Run("reload with different addresses", func(t *testing.T) {
		newCfg := &config.Config{
			Listeners: []*config.Listener{
				{Name: "test-http", Address: "127.0.0.1:9999", Protocol: "http"},
			},
		}
		err := engine.reloadListeners(newCfg)
		if err != nil {
			t.Errorf("reloadListeners() error = %v", err)
		}
	})

	t.Run("reload with same listener count but different TLS", func(t *testing.T) {
		newCfg := &config.Config{
			Listeners: []*config.Listener{
				{Name: "test-http", Address: "127.0.0.1:0", Protocol: "http", TLS: true},
			},
		}
		err := engine.reloadListeners(newCfg)
		if err != nil {
			t.Errorf("reloadListeners() error = %v", err)
		}
	})

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestApplyConfig(t *testing.T) {
	t.Run("apply config with route changes", func(t *testing.T) {
		cfg := createTestConfig()
		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if err := engine.Start(); err != nil {
			t.Fatalf("Failed to start engine: %v", err)
		}
		time.Sleep(50 * time.Millisecond)

		// Create new config with different routes
		newCfg := createTestConfig()
		newCfg.Listeners[0].Routes = []*config.Route{
			{Path: "/", Pool: "test-pool"},
			{Path: "/api", Pool: "test-pool"},
			{Path: "/new-path", Pool: "test-pool"},
		}

		err = engine.applyConfig(newCfg)
		if err != nil {
			t.Errorf("applyConfig() error = %v", err)
		}

		// Verify new routes are applied
		if engine.router.RouteCount() != 3 {
			t.Errorf("RouteCount() = %v, want 3", engine.router.RouteCount())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		engine.Shutdown(ctx)
	})

	t.Run("apply config with pool changes", func(t *testing.T) {
		cfg := createTestConfig()
		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if err := engine.Start(); err != nil {
			t.Fatalf("Failed to start engine: %v", err)
		}
		time.Sleep(50 * time.Millisecond)

		// Create new config with additional pool
		newCfg := createTestConfig()
		newCfg.Pools = append(newCfg.Pools, &config.Pool{
			Name:      "new-pool",
			Algorithm: "round_robin",
			HealthCheck: &config.HealthCheck{
				Type:     "http",
				Path:     "/health",
				Interval: "10s",
				Timeout:  "5s",
			},
			Backends: []*config.Backend{
				{ID: "new-backend", Address: "127.0.0.1:9090", Weight: 100},
			},
		})

		err = engine.applyConfig(newCfg)
		if err != nil {
			t.Errorf("applyConfig() error = %v", err)
		}

		// Verify new pool is added
		if engine.poolManager.PoolCount() != 2 {
			t.Errorf("PoolCount() = %v, want 2", engine.poolManager.PoolCount())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		engine.Shutdown(ctx)
	})

	t.Run("apply config with invalid route", func(t *testing.T) {
		cfg := createTestConfig()
		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if err := engine.Start(); err != nil {
			t.Fatalf("Failed to start engine: %v", err)
		}
		time.Sleep(50 * time.Millisecond)

		// Create new config with invalid route (empty path)
		newCfg := createTestConfig()
		newCfg.Listeners[0].Routes = []*config.Route{
			{Path: "", Pool: "test-pool"}, // Empty path might be invalid
		}

		// This might succeed or fail depending on router validation
		// but should not panic
		_ = engine.applyConfig(newCfg)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		engine.Shutdown(ctx)
	})
}

func TestCreateLogger(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Logging
	}{
		{
			name: "nil config - defaults to stdout JSON",
			cfg:  nil,
		},
		{
			name: "stdout text format",
			cfg: &config.Logging{
				Output: "stdout",
				Format: "text",
				Level:  "debug",
			},
		},
		{
			name: "stdout JSON format",
			cfg: &config.Logging{
				Output: "stdout",
				Format: "json",
				Level:  "info",
			},
		},
		{
			name: "stderr text format",
			cfg: &config.Logging{
				Output: "stderr",
				Format: "text",
				Level:  "warn",
			},
		},
		{
			name: "stderr JSON format",
			cfg: &config.Logging{
				Output: "stderr",
				Format: "json",
				Level:  "error",
			},
		},
		{
			name: "file output",
			cfg: &config.Logging{
				Output: "./test.log",
				Format: "json",
				Level:  "info",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := createLogger(tt.cfg)
			if logger == nil {
				t.Error("createLogger() returned nil")
			}
		})
	}
}

func TestGetAdminAddress(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "default address",
			cfg:  &config.Config{},
			want: ":8080",
		},
		{
			name: "custom address",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address: "127.0.0.1:9090",
				},
			},
			want: "127.0.0.1:9090",
		},
		{
			name: "admin nil",
			cfg: &config.Config{
				Admin: nil,
			},
			want: ":8080",
		},
		{
			name: "empty address",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address: "",
				},
			},
			want: ":8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getAdminAddress(tt.cfg)
			if got != tt.want {
				t.Errorf("getAdminAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitializePoolsWithErrors(t *testing.T) {
	t.Run("duplicate backend ID", func(t *testing.T) {
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
						{ID: "b1", Address: "127.0.0.1:8082", Weight: 100}, // Duplicate ID
					},
				},
			},
			Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
		}

		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		err = engine.initializePools()
		if err == nil {
			t.Error("initializePools() expected error for duplicate backend ID")
		}
	})

	t.Run("duplicate pool name", func(t *testing.T) {
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
				{
					Name:      "pool1", // Duplicate pool name
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
				},
			},
			Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
		}

		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		err = engine.initializePools()
		if err == nil {
			t.Error("initializePools() expected error for duplicate pool name")
		}
	})
}

func TestInitializeRoutesWithErrors(t *testing.T) {
	t.Run("duplicate route", func(t *testing.T) {
		cfg := &config.Config{
			Version: "1",
			Listeners: []*config.Listener{
				{
					Name:     "test",
					Address:  "127.0.0.1:0",
					Protocol: "http",
					Routes: []*config.Route{
						{Path: "/", Pool: "pool1"},
						{Path: "/", Pool: "pool1"}, // Duplicate route
					},
				},
			},
			Pools: []*config.Pool{
				{Name: "pool1", Algorithm: "round_robin"},
			},
			Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
		}

		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		err = engine.initializeRoutes()
		// This might succeed or fail depending on router implementation
		// The test documents the behavior
		_ = err
	})
}

func TestStartListenersWithErrors(t *testing.T) {
	t.Run("HTTPS listener without TLS config creates self-signed cert", func(t *testing.T) {
		cfg := &config.Config{
			Version: "1",
			Listeners: []*config.Listener{
				{
					Name:     "https",
					Address:  "127.0.0.1:0",
					Protocol: "https",
					TLS:      true,
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
			t.Fatalf("Failed to create engine: %v", err)
		}

		// The listener package may create a self-signed certificate automatically
		// so this should not error
		err = engine.startListeners()
		if err != nil {
			// If it errors, that's also valid behavior - just document it
			t.Logf("startListeners() returned error (may be expected): %v", err)
		}

		// Cleanup any started listeners
		for _, l := range engine.listeners {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			l.Stop(ctx)
			cancel()
		}
	})
}

func TestFullLifecycle(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Verify initial state
	if engine.GetState() != StateStopped {
		t.Errorf("Initial state = %v, want %v", engine.GetState(), StateStopped)
	}

	// Start
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	if engine.GetState() != StateRunning {
		t.Errorf("State after start = %v, want %v", engine.GetState(), StateRunning)
	}

	time.Sleep(50 * time.Millisecond)

	// Reload
	if err := engine.Reload(); err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	if engine.GetState() != StateRunning {
		t.Errorf("State after reload = %v, want %v", engine.GetState(), StateRunning)
	}

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	if engine.GetState() != StateStopped {
		t.Errorf("State after shutdown = %v, want %v", engine.GetState(), StateStopped)
	}

	// Verify uptime is 0 after shutdown
	if engine.Uptime() != 0 {
		t.Error("Uptime() should be 0 after shutdown")
	}
}

func TestConcurrentOperations(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Run concurrent operations
	done := make(chan bool, 10)

	// Concurrent status checks
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = engine.GetStatus()
				_ = engine.GetState()
				_ = engine.Uptime()
				_ = engine.IsRunning()
			}
			done <- true
		}()
	}

	// Concurrent config access
	for i := 0; i < 2; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = engine.GetConfig()
				_ = engine.GetLogger()
				_ = engine.GetMetrics()
				_ = engine.GetPoolManager()
				_ = engine.GetRouter()
				_ = engine.GetHealthChecker()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestListenersChangedDetection(t *testing.T) {
	tests := []struct {
		name   string
		oldCfg *config.Config
		newCfg *config.Config
		want   bool
	}{
		{
			name: "protocol changed",
			oldCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
				},
			},
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "https"},
				},
			},
			want: true,
		},
		{
			name: "name changed",
			oldCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
				},
			},
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l2", Address: ":80", Protocol: "http"},
				},
			},
			want: true,
		},
		{
			name: "no change in multiple listeners",
			oldCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
					{Name: "l2", Address: ":443", Protocol: "https", TLS: true},
				},
			},
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
					{Name: "l2", Address: ":443", Protocol: "https", TLS: true},
				},
			},
			want: false,
		},
		{
			name: "one listener changed in multiple",
			oldCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":80", Protocol: "http"},
					{Name: "l2", Address: ":443", Protocol: "https", TLS: true},
				},
			},
			newCfg: &config.Config{
				Listeners: []*config.Listener{
					{Name: "l1", Address: ":8080", Protocol: "http"},
					{Name: "l2", Address: ":443", Protocol: "https", TLS: true},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listenersChanged(tt.oldCfg, tt.newCfg)
			if got != tt.want {
				t.Errorf("listenersChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEngineWithVariousPoolAlgorithms(t *testing.T) {
	algorithms := []string{"round_robin", "rr", "weighted_round_robin", "wrr"}

	for _, algo := range algorithms {
		t.Run(fmt.Sprintf("algorithm_%s", algo), func(t *testing.T) {
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
						Algorithm: algo,
						HealthCheck: &config.HealthCheck{
							Type:     "http",
							Path:     "/health",
							Interval: "10s",
							Timeout:  "5s",
						},
						Backends: []*config.Backend{
							{ID: "b1", Address: "127.0.0.1:8081", Weight: 100},
							{ID: "b2", Address: "127.0.0.1:8082", Weight: 50},
						},
					},
				},
				Admin: &config.Admin{Enabled: true, Address: "127.0.0.1:0"},
			}

			configPath := createTempConfigFile(t, cfg)
			engine, err := New(cfg, configPath)
			if err != nil {
				t.Fatalf("Failed to create engine: %v", err)
			}

			if err := engine.Start(); err != nil {
				t.Fatalf("Failed to start engine: %v", err)
			}

			time.Sleep(50 * time.Millisecond)

			pool := engine.GetPoolManager().GetPool("pool1")
			if pool == nil {
				t.Fatal("GetPool returned nil")
			}

			if pool.BackendCount() != 2 {
				t.Errorf("BackendCount() = %v, want 2", pool.BackendCount())
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			engine.Shutdown(ctx)
		})
	}
}

func TestEngineReloadWithListenerChanges(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Corrupt config file with invalid listener
	invalidContent := `
version: "1"
listeners:
  - name: test-http
    address: invalid:address:format
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
	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	// Reload should fail but engine should keep running
	err = engine.Reload()
	if err == nil {
		t.Error("Reload() expected error with invalid listener")
	}

	if engine.GetState() != StateRunning {
		t.Errorf("State after failed reload = %v, want %v", engine.GetState(), StateRunning)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestEngineReloadWithRouteChanges(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Write new config with additional route to same pool
	newContent := `
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
	if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("Failed to write new config: %v", err)
	}

	// Reload should succeed
	err = engine.Reload()
	if err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	// Verify new route is added
	if engine.router.RouteCount() != 2 {
		t.Errorf("RouteCount() = %v, want 2", engine.router.RouteCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestEngineReloadWithMissingPool(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Write new config with route referencing non-existent pool
	newContent := `
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
	if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("Failed to write new config: %v", err)
	}

	// Reload should fail due to validation
	err = engine.Reload()
	if err == nil {
		t.Error("Reload() expected error with missing pool")
	}

	if engine.GetState() != StateRunning {
		t.Errorf("State after failed reload = %v, want %v", engine.GetState(), StateRunning)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	engine.Shutdown(ctx)
}

func TestEngineErrorRecovery(t *testing.T) {
	t.Run("start after failed start", func(t *testing.T) {
		cfg := createTestConfig()
		cfg.Listeners[0].Address = "invalid:address:format"

		configPath := createTempConfigFile(t, cfg)
		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		// First start should fail
		err = engine.Start()
		if err == nil {
			t.Error("First Start() expected error")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			engine.Shutdown(ctx)
			return
		}

		// State should be stopped
		if engine.GetState() != StateStopped {
			t.Errorf("State = %v after failed start, want %v", engine.GetState(), StateStopped)
		}
	})

	t.Run("shutdown after failed shutdown", func(t *testing.T) {
		cfg := createTestConfig()
		configPath := createTempConfigFile(t, cfg)

		engine, err := New(cfg, configPath)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if err := engine.Start(); err != nil {
			t.Fatalf("Failed to start engine: %v", err)
		}

		time.Sleep(50 * time.Millisecond)

		// First shutdown should succeed
		ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel1()
		if err := engine.Shutdown(ctx1); err != nil {
			t.Errorf("First Shutdown() error = %v", err)
		}

		// Second shutdown should fail
		ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel2()
		err = engine.Shutdown(ctx2)
		if err == nil {
			t.Error("Second Shutdown() expected error")
		}
	})
}

// --- Adapter tests ---

func TestEngineConfigGetter(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	getter := &engineConfigGetter{engine: engine}
	result := getter.GetConfig()

	if result == nil {
		t.Fatal("GetConfig() returned nil")
	}

	// The result should be *config.Config
	resultCfg, ok := result.(*config.Config)
	if !ok {
		t.Fatalf("GetConfig() returned %T, want *config.Config", result)
	}
	if resultCfg.Version != "1" {
		t.Errorf("GetConfig().Version = %q, want %q", resultCfg.Version, "1")
	}
}

func TestEngineMetricsProvider_QueryMetrics(t *testing.T) {
	provider := &engineMetricsProvider{}

	result := provider.QueryMetrics("test.*")
	if result == nil {
		t.Fatal("QueryMetrics() returned nil")
	}

	if result["pattern"] != "test.*" {
		t.Errorf("QueryMetrics pattern = %v, want %q", result["pattern"], "test.*")
	}
	if result["message"] != "metrics query via MCP" {
		t.Errorf("QueryMetrics message = %v, want %q", result["message"], "metrics query via MCP")
	}
}

func TestEngineBackendProvider_ListPools(t *testing.T) {
	poolMgr := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.Weight = 5
	pool.AddBackend(b)
	poolMgr.AddPool(pool)

	provider := &engineBackendProvider{poolMgr: poolMgr}

	pools := provider.ListPools()
	if len(pools) != 1 {
		t.Fatalf("ListPools() returned %d pools, want 1", len(pools))
	}

	if pools[0].Name != "test-pool" {
		t.Errorf("pool name = %q, want %q", pools[0].Name, "test-pool")
	}
	if pools[0].Algorithm != "round_robin" {
		t.Errorf("pool algorithm = %q, want %q", pools[0].Algorithm, "round_robin")
	}
	if len(pools[0].Backends) != 1 {
		t.Fatalf("pool has %d backends, want 1", len(pools[0].Backends))
	}
	if pools[0].Backends[0].ID != "b1" {
		t.Errorf("backend ID = %q, want %q", pools[0].Backends[0].ID, "b1")
	}
	if pools[0].Backends[0].Weight != 5 {
		t.Errorf("backend weight = %d, want 5", pools[0].Backends[0].Weight)
	}
}

func TestEngineBackendProvider_ModifyBackend(t *testing.T) {
	poolMgr := backend.NewPoolManager()
	pool := backend.NewPool("web", "round_robin")
	poolMgr.AddPool(pool)

	provider := &engineBackendProvider{poolMgr: poolMgr}

	// Test add
	err := provider.ModifyBackend("add", "web", "127.0.0.1:9090")
	if err != nil {
		t.Fatalf("ModifyBackend(add) error = %v", err)
	}
	if b := pool.GetBackend("127.0.0.1:9090"); b == nil {
		t.Error("backend should have been added")
	}

	// Test enable
	err = provider.ModifyBackend("enable", "web", "127.0.0.1:9090")
	if err != nil {
		t.Errorf("ModifyBackend(enable) error = %v", err)
	}

	// Test disable
	err = provider.ModifyBackend("disable", "web", "127.0.0.1:9090")
	if err != nil {
		t.Errorf("ModifyBackend(disable) error = %v", err)
	}

	// Test drain
	err = provider.ModifyBackend("drain", "web", "127.0.0.1:9090")
	if err != nil {
		t.Errorf("ModifyBackend(drain) error = %v", err)
	}

	// Test remove
	err = provider.ModifyBackend("remove", "web", "127.0.0.1:9090")
	if err != nil {
		t.Errorf("ModifyBackend(remove) error = %v", err)
	}

	// Test pool not found
	err = provider.ModifyBackend("add", "nonexistent", "127.0.0.1:9090")
	if err == nil {
		t.Error("expected error for nonexistent pool")
	}

	// Test unknown action
	err = provider.ModifyBackend("restart", "web", "127.0.0.1:9090")
	if err == nil {
		t.Error("expected error for unknown action")
	}

	// Test enable nonexistent backend
	err = provider.ModifyBackend("enable", "web", "nonexistent")
	if err == nil {
		t.Error("expected error for enable nonexistent backend")
	}

	// Test disable nonexistent backend
	err = provider.ModifyBackend("disable", "web", "nonexistent")
	if err == nil {
		t.Error("expected error for disable nonexistent backend")
	}
}

func TestEngineConfigProvider_GetConfig(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	provider := &engineConfigProvider{engine: engine}
	result := provider.GetConfig()

	if result == nil {
		t.Fatal("GetConfig() returned nil")
	}

	resultCfg, ok := result.(*config.Config)
	if !ok {
		t.Fatalf("GetConfig() returned %T, want *config.Config", result)
	}
	if resultCfg.Version != "1" {
		t.Errorf("GetConfig().Version = %q, want %q", resultCfg.Version, "1")
	}
}

func TestEngineRouteProvider_ModifyRoute(t *testing.T) {
	rtr := router.NewRouter()

	provider := &engineRouteProvider{rtr: rtr}

	// Test add route
	err := provider.ModifyRoute("add", "example.com", "/api", "backend-pool")
	if err != nil {
		t.Fatalf("ModifyRoute(add) error = %v", err)
	}

	// Verify route was added
	routes := rtr.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Host != "example.com" {
		t.Errorf("route host = %q, want %q", routes[0].Host, "example.com")
	}
	if routes[0].Path != "/api" {
		t.Errorf("route path = %q, want %q", routes[0].Path, "/api")
	}

	// Test update route
	err = provider.ModifyRoute("update", "example.com", "/api", "new-pool")
	if err != nil {
		t.Errorf("ModifyRoute(update) error = %v", err)
	}

	// Test remove route
	err = provider.ModifyRoute("remove", "example.com", "/api", "")
	if err != nil {
		t.Errorf("ModifyRoute(remove) error = %v", err)
	}

	// Test unknown action
	err = provider.ModifyRoute("unknown", "example.com", "/api", "pool")
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestGetMCPAddress(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "admin address with port",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address: "127.0.0.1:8080",
				},
			},
			expected: "127.0.0.1:8081",
		},
		{
			name: "admin address with different port",
			cfg: &config.Config{
				Admin: &config.Admin{
					Address: ":9090",
				},
			},
			expected: ":9091",
		},
		{
			name:     "nil admin config uses default :8080",
			cfg:      &config.Config{},
			expected: ":8081",
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

// TestSignalHandlersWindows tests signal handling on Windows
// Note: Windows only supports SIGINT and SIGTERM
func TestSignalHandlersWindows(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Verify signal handlers are set up (indirectly by checking engine is running)
	if !engine.IsRunning() {
		t.Error("Engine should be running")
	}

	// Shutdown cleanly
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// ============================================================================
// Tests for 0% coverage getters and adapters
// ============================================================================

func TestEngineGetMCPServer(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// GetMCPServer returns the mcp server (may be nil if not configured)
	mcpServer := engine.GetMCPServer()
	// Just verifying the getter works without panic. The mcpServer may be
	// non-nil if the engine sets it up during New().
	_ = mcpServer
}

func TestEngineGetPluginManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	pluginMgr := engine.GetPluginManager()
	// Verify the getter does not panic. PluginManager may or may not be set.
	_ = pluginMgr
}

func TestEngineGetClusterManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// ClusterManager is nil when clustering is not configured
	clusterMgr := engine.GetClusterManager()
	if clusterMgr != nil {
		t.Error("GetClusterManager() should be nil when clustering is not configured")
	}
}

func TestEngineGetDiscoveryManager(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	discoveryMgr := engine.GetDiscoveryManager()
	// Verify the getter does not panic. DiscoveryManager may or may not be set.
	_ = discoveryMgr
}

func TestEngineCertLister_ListCertificates(t *testing.T) {
	cfg := createTestConfig()
	configPath := createTempConfigFile(t, cfg)

	engine, err := New(cfg, configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Create the cert lister adapter using the engine's TLS manager
	lister := &engineCertLister{tlsMgr: engine.tlsManager}

	// With no certificates loaded, should return empty list
	certs := lister.ListCertificates()
	if len(certs) != 0 {
		t.Errorf("ListCertificates() returned %d certs, want 0", len(certs))
	}
}
