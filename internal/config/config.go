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
	Profiling  *ProfilingConfig  `yaml:"profiling" json:"profiling"`
}

// ProfilingConfig represents profiling/debugging configuration.
type ProfilingConfig struct {
	Enabled              bool   `yaml:"enabled" json:"enabled"`
	PprofAddr            string `yaml:"pprof_addr" json:"pprof_addr"`
	CPUProfilePath       string `yaml:"cpu_profile_path" json:"cpu_profile_path"`
	MemProfilePath       string `yaml:"mem_profile_path" json:"mem_profile_path"`
	BlockProfileRate     int    `yaml:"block_profile_rate" json:"block_profile_rate"`
	MutexProfileFraction int    `yaml:"mutex_profile_fraction" json:"mutex_profile_fraction"`
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
	Timeout        *TimeoutConfig        `yaml:"timeout" json:"timeout"`
	MaxBodySize    *MaxBodySizeConfig    `yaml:"max_body_size" json:"max_body_size"`
}

// TimeoutConfig represents request timeout configuration.
type TimeoutConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Timeout string `yaml:"timeout" json:"timeout"` // e.g. "60s", "30s"
}

// MaxBodySizeConfig represents request body size limit configuration.
type MaxBodySizeConfig struct {
	Enabled bool  `yaml:"enabled" json:"enabled"`
	MaxSize int64 `yaml:"max_size" json:"max_size"` // bytes, default 10MB
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
	Enabled      bool                `yaml:"enabled" json:"enabled"`
	Mode         string              `yaml:"mode" json:"mode"` // "enforce", "monitor", "disabled"
	IPACL        *WAFIPACLConfig     `yaml:"ip_acl" json:"ip_acl"`
	RateLimit    *WAFRateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
	Sanitizer    *WAFSanitizerConfig `yaml:"sanitizer" json:"sanitizer"`
	Detection    *WAFDetectionConfig `yaml:"detection" json:"detection"`
	BotDetection *WAFBotConfig       `yaml:"bot_detection" json:"bot_detection"`
	Response     *WAFResponseConfig  `yaml:"response" json:"response"`
	Logging      *WAFLoggingConfig   `yaml:"logging" json:"logging"`
}

// WAFIPACLConfig configures IP access control lists.
type WAFIPACLConfig struct {
	Enabled   bool              `yaml:"enabled" json:"enabled"`
	Whitelist []WAFIPACLEntry   `yaml:"whitelist" json:"whitelist"`
	Blacklist []WAFIPACLEntry   `yaml:"blacklist" json:"blacklist"`
	AutoBan   *WAFAutoBanConfig `yaml:"auto_ban" json:"auto_ban"`
}

type WAFIPACLEntry struct {
	CIDR    string `yaml:"cidr" json:"cidr"`
	Reason  string `yaml:"reason" json:"reason"`
	Expires string `yaml:"expires" json:"expires"` // ISO 8601
}

type WAFAutoBanConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	DefaultTTL string `yaml:"default_ttl" json:"default_ttl"` // e.g. "1h"
	MaxTTL     string `yaml:"max_ttl" json:"max_ttl"`         // e.g. "24h"
}

// WAFRateLimitConfig configures WAF-integrated rate limiting.
type WAFRateLimitConfig struct {
	Enabled      bool               `yaml:"enabled" json:"enabled"`
	SyncInterval string             `yaml:"sync_interval" json:"sync_interval"` // e.g. "5s"
	Rules        []WAFRateLimitRule `yaml:"rules" json:"rules"`
}

type WAFRateLimitRule struct {
	ID           string   `yaml:"id" json:"id"`
	Scope        string   `yaml:"scope" json:"scope"` // "ip", "path", "ip+path", "header:X-API-Key", "global"
	Paths        []string `yaml:"paths" json:"paths"` // glob patterns
	Limit        int      `yaml:"limit" json:"limit"`
	Window       string   `yaml:"window" json:"window"` // e.g. "1m"
	Burst        int      `yaml:"burst" json:"burst"`
	Action       string   `yaml:"action" json:"action"` // "block", "throttle"
	AutoBanAfter int      `yaml:"auto_ban_after" json:"auto_ban_after"`
}

// WAFSanitizerConfig configures request sanitization.
type WAFSanitizerConfig struct {
	Enabled           bool                   `yaml:"enabled" json:"enabled"`
	MaxHeaderSize     int                    `yaml:"max_header_size" json:"max_header_size"`
	MaxHeaderCount    int                    `yaml:"max_header_count" json:"max_header_count"`
	MaxBodySize       int64                  `yaml:"max_body_size" json:"max_body_size"`
	MaxURLLength      int                    `yaml:"max_url_length" json:"max_url_length"`
	MaxCookieSize     int                    `yaml:"max_cookie_size" json:"max_cookie_size"`
	MaxCookieCount    int                    `yaml:"max_cookie_count" json:"max_cookie_count"`
	BlockNullBytes    bool                   `yaml:"block_null_bytes" json:"block_null_bytes"`
	NormalizeEncoding bool                   `yaml:"normalize_encoding" json:"normalize_encoding"`
	StripHopByHop     bool                   `yaml:"strip_hop_by_hop" json:"strip_hop_by_hop"`
	AllowedMethods    []string               `yaml:"allowed_methods" json:"allowed_methods"`
	PathOverrides     []WAFSanitizerOverride `yaml:"path_overrides" json:"path_overrides"`
}

type WAFSanitizerOverride struct {
	Path        string `yaml:"path" json:"path"`
	MaxBodySize int64  `yaml:"max_body_size" json:"max_body_size"`
}

// WAFDetectionConfig configures the WAF detection engine.
type WAFDetectionConfig struct {
	Enabled    bool                    `yaml:"enabled" json:"enabled"`
	Mode       string                  `yaml:"mode" json:"mode"` // "enforce", "monitor"
	Threshold  WAFDetectionThreshold   `yaml:"threshold" json:"threshold"`
	Detectors  WAFDetectorsConfig      `yaml:"detectors" json:"detectors"`
	Exclusions []WAFDetectionExclusion `yaml:"exclusions" json:"exclusions"`
}

type WAFDetectionThreshold struct {
	Block int `yaml:"block" json:"block"` // default: 50
	Log   int `yaml:"log" json:"log"`     // default: 25
}

type WAFDetectorConfig struct {
	Enabled         bool    `yaml:"enabled" json:"enabled"`
	ScoreMultiplier float64 `yaml:"score_multiplier" json:"score_multiplier"`
}

type WAFDetectorsConfig struct {
	SQLi          WAFDetectorConfig `yaml:"sqli" json:"sqli"`
	XSS           WAFDetectorConfig `yaml:"xss" json:"xss"`
	PathTraversal WAFDetectorConfig `yaml:"path_traversal" json:"path_traversal"`
	CMDi          WAFDetectorConfig `yaml:"cmdi" json:"cmdi"`
	XXE           WAFDetectorConfig `yaml:"xxe" json:"xxe"`
	SSRF          WAFDetectorConfig `yaml:"ssrf" json:"ssrf"`
}

type WAFDetectionExclusion struct {
	Path      string   `yaml:"path" json:"path"`
	Detectors []string `yaml:"detectors" json:"detectors"`
	Reason    string   `yaml:"reason" json:"reason"`
	Condition string   `yaml:"condition" json:"condition"` // "always", "whitelist"
}

// WAFBotConfig configures bot detection.
type WAFBotConfig struct {
	Enabled        bool                `yaml:"enabled" json:"enabled"`
	Mode           string              `yaml:"mode" json:"mode"` // "enforce", "monitor"
	TLSFingerprint *WAFTLSFPConfig     `yaml:"tls_fingerprint" json:"tls_fingerprint"`
	UserAgent      *WAFUserAgentConfig `yaml:"user_agent" json:"user_agent"`
	Behavior       *WAFBehaviorConfig  `yaml:"behavior" json:"behavior"`
}

type WAFTLSFPConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	KnownBotsAction string `yaml:"known_bots_action" json:"known_bots_action"` // "block", "log"
	UnknownAction   string `yaml:"unknown_action" json:"unknown_action"`
	MismatchAction  string `yaml:"mismatch_action" json:"mismatch_action"`
}

type WAFUserAgentConfig struct {
	Enabled            bool `yaml:"enabled" json:"enabled"`
	BlockEmpty         bool `yaml:"block_empty" json:"block_empty"`
	BlockKnownScanners bool `yaml:"block_known_scanners" json:"block_known_scanners"`
}

type WAFBehaviorConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	Window             string `yaml:"window" json:"window"` // e.g. "5m"
	RPSThreshold       int    `yaml:"rps_threshold" json:"rps_threshold"`
	ErrorRateThreshold int    `yaml:"error_rate_threshold" json:"error_rate_threshold"`
}

// WAFResponseConfig configures response protection.
type WAFResponseConfig struct {
	SecurityHeaders *WAFSecurityHeadersConfig `yaml:"security_headers" json:"security_headers"`
	DataMasking     *WAFDataMaskingConfig     `yaml:"data_masking" json:"data_masking"`
	ErrorPages      *WAFErrorPagesConfig      `yaml:"error_pages" json:"error_pages"`
}

type WAFSecurityHeadersConfig struct {
	Enabled               bool           `yaml:"enabled" json:"enabled"`
	HSTS                  *WAFHSTSConfig `yaml:"hsts" json:"hsts"`
	XContentTypeOptions   bool           `yaml:"x_content_type_options" json:"x_content_type_options"`
	XFrameOptions         string         `yaml:"x_frame_options" json:"x_frame_options"`
	ReferrerPolicy        string         `yaml:"referrer_policy" json:"referrer_policy"`
	PermissionsPolicy     string         `yaml:"permissions_policy" json:"permissions_policy"`
	ContentSecurityPolicy string         `yaml:"content_security_policy" json:"content_security_policy"`
}

type WAFHSTSConfig struct {
	Enabled           bool `yaml:"enabled" json:"enabled"`
	MaxAge            int  `yaml:"max_age" json:"max_age"`
	IncludeSubdomains bool `yaml:"include_subdomains" json:"include_subdomains"`
	Preload           bool `yaml:"preload" json:"preload"`
}

type WAFDataMaskingConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	MaskCreditCards  bool `yaml:"mask_credit_cards" json:"mask_credit_cards"`
	MaskSSN          bool `yaml:"mask_ssn" json:"mask_ssn"`
	MaskEmails       bool `yaml:"mask_emails" json:"mask_emails"`
	MaskAPIKeys      bool `yaml:"mask_api_keys" json:"mask_api_keys"`
	StripStackTraces bool `yaml:"strip_stack_traces" json:"strip_stack_traces"`
}

type WAFErrorPagesConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Mode    string `yaml:"mode" json:"mode"` // "production", "development"
}

// WAFLoggingConfig configures WAF event logging.
type WAFLoggingConfig struct {
	Level      string `yaml:"level" json:"level"`   // "debug", "info", "warn", "error"
	Format     string `yaml:"format" json:"format"` // "json", "text"
	LogAllowed bool   `yaml:"log_allowed" json:"log_allowed"`
	LogBlocked bool   `yaml:"log_blocked" json:"log_blocked"`
	LogBody    bool   `yaml:"log_body" json:"log_body"`
}

// ListenerTLS represents TLS configuration for a listener.
// Can be a simple bool (true/false) or a struct with cert/key paths.
type ListenerTLS struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file" json:"key_file"`
}

// Listener represents an L4/L7 listener.
type Listener struct {
	Name     string       `yaml:"name" json:"name"`
	Address  string       `yaml:"address" json:"address"`
	Protocol string       `yaml:"protocol" json:"protocol"`
	TLS      *ListenerTLS `yaml:"tls" json:"tls"`
	Routes   []*Route     `yaml:"routes" json:"routes"`
	MTLS     *MTLSConfig  `yaml:"mtls" json:"mtls"`
	Pool     string       `yaml:"pool" json:"pool"`
}

// IsTLS returns true if TLS is enabled for this listener.
func (l *Listener) IsTLS() bool {
	return l.TLS != nil && l.TLS.Enabled
}

// MTLSConfig represents mTLS configuration for a listener.
type MTLSConfig struct {
	Enabled    bool     `yaml:"enabled" json:"enabled"`
	ClientAuth string   `yaml:"client_auth" json:"client_auth"`
	ClientCAs  []string `yaml:"client_cas" json:"client_cas"`
}

// Route represents a routing rule.
type Route struct {
	Name       string                   `yaml:"name" json:"name"`
	Path       string                   `yaml:"path" json:"path"`
	Host       string                   `yaml:"host" json:"host"`
	Methods    []string                 `yaml:"methods" json:"methods"`
	Pool       string                   `yaml:"pool" json:"pool"`
	Middleware []map[string]interface{} `yaml:"middleware" json:"middleware"`
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
	Address    string `yaml:"address" json:"address"`
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	MCPAddress string `yaml:"mcp_address" json:"mcp_address"`
	MCPToken   string `yaml:"mcp_token" json:"mcp_token"`
	MCPAudit   bool   `yaml:"mcp_audit" json:"mcp_audit"`
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

	// Validate listeners
	listenerNames := make(map[string]bool)
	for i, l := range c.Listeners {
		if l.Name == "" {
			return fmt.Errorf("listener %d: name is required", i)
		}
		if listenerNames[l.Name] {
			return fmt.Errorf("listener %s: duplicate name", l.Name)
		}
		listenerNames[l.Name] = true

		if l.Address == "" {
			return fmt.Errorf("listener %s: address is required", l.Name)
		}

		// Validate address format (must be parseable as host:port)
		if _, _, err := parseAddress(l.Address); err != nil {
			return fmt.Errorf("listener %s: invalid address %q: %w", l.Name, l.Address, err)
		}

		// Validate routes reference pools
		for j, route := range l.Routes {
			if route.Pool == "" {
				return fmt.Errorf("listener %s: route %d: pool is required", l.Name, j)
			}
		}
	}

	// Validate pools
	poolNames := make(map[string]bool)
	for i, p := range c.Pools {
		if p.Name == "" {
			return fmt.Errorf("pool %d: name is required", i)
		}
		if poolNames[p.Name] {
			return fmt.Errorf("pool %s: duplicate name", p.Name)
		}
		poolNames[p.Name] = true

		// Validate backends
		for j, b := range p.Backends {
			if b.Address == "" {
				return fmt.Errorf("pool %s: backend %d: address is required", p.Name, j)
			}
		}
	}

	// Validate route pool references
	for _, l := range c.Listeners {
		for _, route := range l.Routes {
			if route.Pool != "" && !poolNames[route.Pool] {
				return fmt.Errorf("listener %s: route references non-existent pool %q", l.Name, route.Pool)
			}
		}
		if l.Pool != "" && !poolNames[l.Pool] {
			return fmt.Errorf("listener %s: references non-existent pool %q", l.Name, l.Pool)
		}
	}

	return nil
}

// parseAddress validates and splits an address like ":8080" or "127.0.0.1:8080".
func parseAddress(addr string) (string, string, error) {
	if strings.HasPrefix(addr, ":") {
		return "", addr[1:], nil
	}
	host, port, found := strings.Cut(addr, ":")
	if !found {
		return addr, "", nil
	}
	return host, port, nil
}
