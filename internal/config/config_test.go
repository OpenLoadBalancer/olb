package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("EMPTY_VAR", "")

	tests := []struct {
		input    string
		expected string
	}{
		{"${TEST_VAR}", "test_value"},
		{"prefix_${TEST_VAR}_suffix", "prefix_test_value_suffix"},
		{"${EMPTY_VAR:-default}", "default"},
		{"${MISSING_VAR:-default}", "default"},
		{"no vars", "no vars"},
	}

	for _, tt := range tests {
		got := ExpandEnv(tt.input)
		if got != tt.expected {
			t.Errorf("ExpandEnv(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestConfig_Validate(t *testing.T) {
	// Valid config
	cfg := &Config{
		Listeners: []*Listener{
			{Name: "http", Address: ":80"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}

	// Missing listeners
	cfg2 := &Config{}
	if err := cfg2.Validate(); err == nil {
		t.Error("Validate() should fail without listeners")
	}

	// Missing listener name
	cfg3 := &Config{
		Listeners: []*Listener{
			{Address: ":80"},
		},
	}
	if err := cfg3.Validate(); err == nil {
		t.Error("Validate() should fail without listener name")
	}
}

func TestLoader_Load(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
version: "1"
listeners:
  - name: http
    address: ":80"
    routes:
      - path: /
        pool: backend

pools:
  - name: backend
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(configFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}

	if len(cfg.Listeners) != 1 {
		t.Errorf("len(Listeners) = %d, want 1", len(cfg.Listeners))
	}

	if cfg.Listeners[0].Name != "http" {
		t.Errorf("Listeners[0].Name = %q, want %q", cfg.Listeners[0].Name, "http")
	}

	if len(cfg.Pools) != 1 {
		t.Errorf("len(Pools) = %d, want 1", len(cfg.Pools))
	}
}

func TestLoader_LoadWithEnv(t *testing.T) {
	os.Setenv("LISTENER_PORT", "9090")
	os.Setenv("BACKEND_ADDR", "10.0.1.20:8080")

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
version: "1"
listeners:
  - name: http
    address: ":${LISTENER_PORT}"
    routes:
      - path: /
        pool: backend

pools:
  - name: backend
    backends:
      - address: "${BACKEND_ADDR}"
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(configFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Listeners[0].Address != ":9090" {
		t.Errorf("Address = %q, want %q", cfg.Listeners[0].Address, ":9090")
	}

	if cfg.Pools[0].Backends[0].Address != "10.0.1.20:8080" {
		t.Errorf("Backend address = %q, want %q", cfg.Pools[0].Backends[0].Address, "10.0.1.20:8080")
	}
}

func TestLoader_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
listeners:
  - name: http
    address: ":80"

pools:
  - name: backend
    backends:
      - address: "10.0.1.10:8080"
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(configFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Check defaults
	if cfg.Listeners[0].Protocol != "http" {
		t.Errorf("Protocol = %q, want %q", cfg.Listeners[0].Protocol, "http")
	}

	if cfg.Pools[0].Algorithm != "round_robin" {
		t.Errorf("Algorithm = %q, want %q", cfg.Pools[0].Algorithm, "round_robin")
	}

	if cfg.Pools[0].HealthCheck == nil {
		t.Error("HealthCheck should have default value")
	}

	if cfg.Pools[0].Backends[0].Weight != 100 {
		t.Errorf("Weight = %d, want %d", cfg.Pools[0].Backends[0].Weight, 100)
	}

	if cfg.Admin == nil {
		t.Error("Admin should have default value")
	}

	if cfg.Logging == nil {
		t.Error("Logging should have default value")
	}

	if cfg.Metrics == nil {
		t.Error("Metrics should have default value")
	}
}

func TestConfig_HealthCheck(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
listeners:
  - name: http
    address: ":80"

pools:
  - name: backend
    health_check:
      path: /healthz
      interval: 5s
      timeout: 2s
    backends:
      - address: "10.0.1.10:8080"
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(configFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Pools[0].HealthCheck.Path != "/healthz" {
		t.Errorf("HealthCheck.Path = %q, want %q", cfg.Pools[0].HealthCheck.Path, "/healthz")
	}

	if cfg.Pools[0].HealthCheck.Interval != "5s" {
		t.Errorf("HealthCheck.Interval = %q, want %q", cfg.Pools[0].HealthCheck.Interval, "5s")
	}
}

func TestConfig_TLS(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
listeners:
  - name: https
    address: ":443"
    tls: true

tls:
  cert_file: /etc/ssl/cert.pem
  key_file: /etc/ssl/key.pem
  acme:
    enabled: true
    email: admin@example.com
    domains:
      - example.com
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(configFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.TLS == nil {
		t.Fatal("TLS config is nil")
	}

	if cfg.TLS.CertFile != "/etc/ssl/cert.pem" {
		t.Errorf("CertFile = %q, want %q", cfg.TLS.CertFile, "/etc/ssl/cert.pem")
	}

	if !cfg.TLS.ACME.Enabled {
		t.Error("ACME should be enabled")
	}

	if len(cfg.TLS.ACME.Domains) != 1 {
		t.Errorf("len(Domains) = %d, want 1", len(cfg.TLS.ACME.Domains))
	}
}
