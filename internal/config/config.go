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
	Version    string            `yaml:"version" json:"version"`
	Listeners  []*Listener       `yaml:"listeners" json:"listeners"`
	Pools      []*Pool           `yaml:"pools" json:"pools"`
	TLS        *TLSConfig        `yaml:"tls" json:"tls"`
	Admin      *Admin            `yaml:"admin" json:"admin"`
	Logging    *Logging          `yaml:"logging" json:"logging"`
	Metrics    *Metrics          `yaml:"metrics" json:"metrics"`
	Cluster    *ClusterConfig    `yaml:"cluster" json:"cluster"`
	Middleware *MiddlewareConfig `yaml:"middleware" json:"middleware"`
	WAF        *WAFConfig        `yaml:"waf" json:"waf"`
}

// MiddlewareConfig represents middleware configuration.
type MiddlewareConfig struct {
	RateLimit      *RateLimitConfig      `yaml:"rate_limit" json:"rate_limit"`
	CORS           *CORSConfig           `yaml:"cors" json:"cors"`
	Compression    *CompressionConfig    `yaml:"compression" json:"compression"`
	CircuitBreaker *CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`
	Retry          *RetryConfig          `yaml:"retry" json:"retry"`
	Cache          *CacheConfig          `yaml:"cache" json:"cache"`
	IPFilter       *IPFilterConfig       `yaml:"ip_filter" json:"ip_filter"`
	Headers        *HeadersConfig        `yaml:"headers" json:"headers"`
}

type RateLimitConfig struct {
	Enabled           bool    `yaml:"enabled" json:"enabled"`
	RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
	BurstSize         int     `yaml:"burst_size" json:"burst_size"`
}

type CORSConfig struct {
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	AllowedOrigins   []string `yaml:"allowed_origins" json:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods" json:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers" json:"allowed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials" json:"allow_credentials"`
	MaxAge           int      `yaml:"max_age" json:"max_age"`
}

type CompressionConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	MinSize int  `yaml:"min_size" json:"min_size"`
	Level   int  `yaml:"level" json:"level"`
}

type CircuitBreakerConfig struct {
	Enabled            bool    `yaml:"enabled" json:"enabled"`
	ErrorThreshold     int     `yaml:"error_threshold" json:"error_threshold"`
	ErrorRateThreshold float64 `yaml:"error_rate_threshold" json:"error_rate_threshold"`
	OpenDuration       string  `yaml:"open_duration" json:"open_duration"`
}

type RetryConfig struct {
	Enabled    bool  `yaml:"enabled" json:"enabled"`
	MaxRetries int   `yaml:"max_retries" json:"max_retries"`
	RetryOn    []int `yaml:"retry_on" json:"retry_on"`
}

type CacheConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	MaxEntries int    `yaml:"max_entries" json:"max_entries"`
	DefaultTTL string `yaml:"default_ttl" json:"default_ttl"`
}

type IPFilterConfig struct {
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	AllowList     []string `yaml:"allow_list" json:"allow_list"`
	DenyList      []string `yaml:"deny_list" json:"deny_list"`
	DefaultAction string   `yaml:"default_action" json:"default_action"`
}

type HeadersConfig struct {
	Enabled     bool              `yaml:"enabled" json:"enabled"`
	RequestAdd  map[string]string `yaml:"request_add" json:"request_add"`
	ResponseAdd map[string]string `yaml:"response_add" json:"response_add"`
}

type WAFConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Mode    string `yaml:"mode" json:"mode"`
}

// Listener represents an L4/L7 listener.
type Listener struct {
	Name     string      `yaml:"name" json:"name"`
	Address  string      `yaml:"address" json:"address"`
	Protocol string      `yaml:"protocol" json:"protocol"`
	TLS      bool        `yaml:"tls" json:"tls"`
	Routes   []*Route    `yaml:"routes" json:"routes"`
	MTLS     *MTLSConfig `yaml:"mtls" json:"mtls"`
	Pool     string      `yaml:"pool" json:"pool"` // backend pool for L4 (tcp/udp) listeners
}

// MTLSConfig represents mTLS configuration for a listener.
type MTLSConfig struct {
	Enabled    bool     `yaml:"enabled" json:"enabled"`
	ClientAuth string   `yaml:"client_auth" json:"client_auth"`
	ClientCAs  []string `yaml:"client_cas" json:"client_cas"`
}

// Route represents a routing rule.
type Route struct {
	Path    string   `yaml:"path" json:"path"`
	Host    string   `yaml:"host" json:"host"`
	Methods []string `yaml:"methods" json:"methods"`
	Pool    string   `yaml:"pool" json:"pool"`
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
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file" json:"key_file"`
	ACME     *ACME  `yaml:"acme" json:"acme"`
}

// ACME represents ACME/Let's Encrypt configuration.
type ACME struct {
	Enabled bool     `yaml:"enabled" json:"enabled"`
	Email   string   `yaml:"email" json:"email"`
	Domains []string `yaml:"domains" json:"domains"`
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

// ClusterConfig represents cluster configuration.
type ClusterConfig struct {
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	NodeID        string   `yaml:"node_id" json:"node_id"`
	BindAddr      string   `yaml:"bind_addr" json:"bind_addr"`
	BindPort      int      `yaml:"bind_port" json:"bind_port"`
	Peers         []string `yaml:"peers" json:"peers"`
	DataDir       string   `yaml:"data_dir" json:"data_dir"`
	ElectionTick  string   `yaml:"election_tick" json:"election_tick"`
	HeartbeatTick string   `yaml:"heartbeat_tick" json:"heartbeat_tick"`
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
