// Package config provides configuration parsing for OpenLoadBalancer.
// Supports YAML, JSON, and TOML formats with environment variable substitution.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config represents the top-level configuration.
type Config struct {
	Version   string      `yaml:"version" json:"version"`
	Listeners []*Listener `yaml:"listeners" json:"listeners"`
	Pools     []*Pool     `yaml:"pools" json:"pools"`
	TLS       *TLSConfig  `yaml:"tls" json:"tls"`
	Admin     *Admin      `yaml:"admin" json:"admin"`
	Logging   *Logging    `yaml:"logging" json:"logging"`
	Metrics   *Metrics    `yaml:"metrics" json:"metrics"`
}

// Listener represents an L4/L7 listener.
type Listener struct {
	Name    string   `yaml:"name" json:"name"`
	Address string   `yaml:"address" json:"address"`
	Protocol string  `yaml:"protocol" json:"protocol"`
	TLS     bool     `yaml:"tls" json:"tls"`
	Routes  []*Route `yaml:"routes" json:"routes"`
}

// Route represents a routing rule.
type Route struct {
	Path    string `yaml:"path" json:"path"`
	Host    string `yaml:"host" json:"host"`
	Methods []string `yaml:"methods" json:"methods"`
	Pool    string `yaml:"pool" json:"pool"`
}

// Pool represents a backend pool.
type Pool struct {
	Name        string       `yaml:"name" json:"name"`
	Algorithm   string       `yaml:"algorithm" json:"algorithm"`
	HealthCheck *HealthCheck `yaml:"health_check" json:"health_check"`
	Backends    []*Backend   `yaml:"backends" json:"backends"`
}

// Backend represents a backend server.
type Backend struct {
	ID      string `yaml:"id" json:"id"`
	Address string `yaml:"address" json:"address"`
	Weight  int    `yaml:"weight" json:"weight"`
}

// HealthCheck represents health check configuration.
type HealthCheck struct {
	Type     string `yaml:"type" json:"type"`
	Path     string `yaml:"path" json:"path"`
	Interval string `yaml:"interval" json:"interval"`
	Timeout  string `yaml:"timeout" json:"timeout"`
}

// TLSConfig represents TLS configuration.
type TLSConfig struct {
	CertFile string   `yaml:"cert_file" json:"cert_file"`
	KeyFile  string   `yaml:"key_file" json:"key_file"`
	ACME     *ACME    `yaml:"acme" json:"acme"`
}

// ACME represents ACME/Let's Encrypt configuration.
type ACME struct {
	Enabled  bool     `yaml:"enabled" json:"enabled"`
	Email    string   `yaml:"email" json:"email"`
	Domains  []string `yaml:"domains" json:"domains"`
}

// Admin represents admin API configuration.
type Admin struct {
	Address string `yaml:"address" json:"address"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// Logging represents logging configuration.
type Logging struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
	Output string `yaml:"output" json:"output"`
}

// Metrics represents metrics configuration.
type Metrics struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path" json:"path"`
}

// ExpandEnv substitutes environment variables in a string.
// Supports ${VAR} and ${VAR:-default} syntax.
func ExpandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		// Check for default value syntax: VAR:-default
		if idx := strings.Index(key, ":-"); idx > 0 {
			varName := key[:idx]
			defaultValue := key[idx+2:]
			if val := os.Getenv(varName); val != "" {
				return val
			}
			return defaultValue
		}
		return os.Getenv(key)
	})
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Version == "" {
		c.Version = "1"
	}

	if len(c.Listeners) == 0 {
		return fmt.Errorf("at least one listener is required")
	}

	for i, l := range c.Listeners {
		if l.Name == "" {
			return fmt.Errorf("listener %d: name is required", i)
		}
		if l.Address == "" {
			return fmt.Errorf("listener %s: address is required", l.Name)
		}
	}

	return nil
}
