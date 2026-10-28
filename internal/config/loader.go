package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/openloadbalancer/olb/internal/config/hcl"
	"github.com/openloadbalancer/olb/internal/config/toml"
	"github.com/openloadbalancer/olb/internal/config/yaml"
)

// Loader loads configuration from files.
type Loader struct {
	// ExpandEnv enables environment variable expansion.
	// Default: true
	ExpandEnv bool

	// Warnings holds the non-fatal findings of the last Load, such as
	// unknown config keys.
	Warnings []string

	// AllowedEnvPrefixes restricts which environment variables can be expanded.
	// When non-empty, only variables whose names start with one of the listed
	// prefixes (e.g. "OLB_") will be substituted; others are left as-is.
	// When empty (default), all environment variables are expanded (backward compatible).
	AllowedEnvPrefixes []string
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
		if len(l.AllowedEnvPrefixes) == 0 {
			log.Println("WARNING: env var expansion is unrestricted — any host environment variable can be read. Set allowed_env_prefixes (e.g. [\"OLB_\"]) to limit exposure.")
		}
		configData = ExpandEnvWithPrefixes(configData, l.AllowedEnvPrefixes)
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
	case ".toml":
		if err := toml.Decode([]byte(configData), &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse TOML config: %w", err)
		}
	case ".hcl":
		if err := hcl.Decode([]byte(configData), &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse HCL config: %w", err)
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

	// Unknown keys are ignored by the decoders; surface them so a typo does
	// not pass silently.
	l.Warnings = unknownKeyWarnings(path, configData)
	for _, w := range l.Warnings {
		log.Printf("WARNING: %s: %s", path, w)
	}

	// Apply defaults
	l.applyDefaults(&cfg)

	return &cfg, nil
}

// unknownKeyWarnings re-parses the config into a generic map and reports
// keys that are not config fields. Parse failures are not reported here:
// the typed decode above already succeeded.
func unknownKeyWarnings(path, data string) []string {
	var raw map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		m, err := toml.Parse([]byte(data))
		if err != nil {
			return nil
		}
		raw = m
	case ".hcl":
		m, err := hcl.Parse([]byte(data))
		if err != nil {
			return nil
		}
		raw = m
	default:
		if err := yaml.UnmarshalString(data, &raw); err != nil {
			return nil
		}
	}
	return UnknownKeys(raw)
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
			pool.HealthCheck = DefaultHealthCheck()
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
		enabled := true
		cfg.Admin = &Admin{
			Enabled: &enabled,
			// Loopback: the admin API has no auth unless configured.
			Address: "127.0.0.1:8080",
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
		enabled := true
		cfg.Metrics = &Metrics{
			Enabled: &enabled,
			Path:    "/metrics",
		}
	}
}

// Load loads configuration from the specified file.
func Load(path string) (*Config, error) {
	loader := NewLoader()
	return loader.Load(path)
}

// DefaultHealthCheck is the health check a pool gets when none is configured.
func DefaultHealthCheck() *HealthCheck {
	return &HealthCheck{
		Type:     "http",
		Path:     "/health",
		Interval: "10s",
		Timeout:  "5s",
	}
}
