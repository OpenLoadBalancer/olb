package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openloadbalancer/olb/internal/config/yaml"
)

// Loader loads configuration from files.
type Loader struct {
	// ExpandEnv enables environment variable expansion.
	// Default: true
	ExpandEnv bool
}

// NewLoader creates a new configuration loader.
func NewLoader() *Loader {
	return &Loader{
		ExpandEnv: true,
	}
}

// Load loads configuration from a file.
func (l *Loader) Load(path string) (*Config, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	configData := string(data)
	if l.ExpandEnv {
		configData = ExpandEnv(configData)
	}

	// Parse based on file extension
	var cfg Config

	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.UnmarshalString(configData, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		// JSON is a subset of YAML, use YAML parser
		if err := yaml.UnmarshalString(configData, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		// Try YAML as default
		if err := yaml.UnmarshalString(configData, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Apply defaults
	l.applyDefaults(&cfg)

	return &cfg, nil
}

// applyDefaults applies default values to the configuration.
func (l *Loader) applyDefaults(cfg *Config) {
	if cfg.Version == "" {
		cfg.Version = "1"
	}

	// Apply defaults to listeners
	for _, listener := range cfg.Listeners {
		if listener.Protocol == "" {
			listener.Protocol = "http"
		}
	}

	// Apply defaults to pools
	for _, pool := range cfg.Pools {
		if pool.Algorithm == "" {
			pool.Algorithm = "round_robin"
		}
		if pool.HealthCheck == nil {
			pool.HealthCheck = &HealthCheck{
				Type:     "http",
				Path:     "/health",
				Interval: "10s",
				Timeout:  "5s",
			}
		}
		// Apply defaults to backends
		for _, backend := range pool.Backends {
			if backend.Weight == 0 {
				backend.Weight = 100
			}
		}
	}

	// Apply defaults to admin
	if cfg.Admin == nil {
		cfg.Admin = &Admin{
			Enabled: true,
			Address: ":8080",
		}
	}

	// Apply defaults to logging
	if cfg.Logging == nil {
		cfg.Logging = &Logging{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		}
	}

	// Apply defaults to metrics
	if cfg.Metrics == nil {
		cfg.Metrics = &Metrics{
			Enabled: true,
			Path:    "/metrics",
		}
	}
}

// Load loads configuration from the specified file.
func Load(path string) (*Config, error) {
	loader := NewLoader()
	return loader.Load(path)
}
