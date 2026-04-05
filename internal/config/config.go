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
	GeoDNS     *GeoDNSConfig     `yaml:"geodns" json:"geodns"`
	Shadow     *ShadowConfig     `yaml:"shadow" json:"shadow"`
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
	CSP            *CSPConfig            `yaml:"csp" json:"csp"`
	Compression    *CompressionConfig    `yaml:"compression" json:"compression"`
	CircuitBreaker *CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`
	Retry          *RetryConfig          `yaml:"retry" json:"retry"`
	Cache          *CacheConfig          `yaml:"cache" json:"cache"`
	IPFilter       *IPFilterConfig       `yaml:"ip_filter" json:"ip_filter"`
	Headers        *HeadersConfig        `yaml:"headers" json:"headers"`
	Timeout        *TimeoutConfig        `yaml:"timeout" json:"timeout"`
	MaxBodySize    *MaxBodySizeConfig    `yaml:"max_body_size" json:"max_body_size"`
	Validator      *ValidatorConfig      `yaml:"validator" json:"validator"`
	StripPrefix    *StripPrefixConfig    `yaml:"strip_prefix" json:"strip_prefix"`
	JWT            *JWTConfig            `yaml:"jwt" json:"jwt"`
	OAuth2         *OAuth2Config         `yaml:"oauth2" json:"oauth2"`
	BasicAuth      *BasicAuthConfig      `yaml:"basic_auth" json:"basic_auth"`
	APIKey         *APIKeyConfig         `yaml:"api_key" json:"api_key"`
	HMAC           *HMACConfig           `yaml:"hmac" json:"hmac"`
	Transformer    *TransformerConfig    `yaml:"transformer" json:"transformer"`
	RequestID      *RequestIDConfig      `yaml:"request_id" json:"request_id"`
	Logging        *LoggingConfig        `yaml:"logging" json:"logging"`
	Metrics        *MetricsConfig        `yaml:"metrics" json:"metrics"`
	Rewrite        *RewriteConfig        `yaml:"rewrite" json:"rewrite"`
	ForceSSL       *ForceSSLConfig       `yaml:"forcessl" json:"forcessl"`
	CSRF           *CSRFConfig           `yaml:"csrf" json:"csrf"`
	SecureHeaders  *SecureHeadersConfig  `yaml:"secure_headers" json:"secure_headers"`
	Coalesce       *CoalesceConfig       `yaml:"coalesce" json:"coalesce"`
	BotDetection   *BotDetectionConfig   `yaml:"bot_detection" json:"bot_detection"`
	RealIP         *RealIPConfig         `yaml:"real_ip" json:"real_ip"`
	Trace          *TraceConfig          `yaml:"trace" json:"trace"`
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

// JWTConfig represents JWT authentication configuration.
type JWTConfig struct {
	Enabled          bool                `yaml:"enabled" json:"enabled"`
	Secret           string              `yaml:"secret" json:"secret"`                       // HMAC secret (for HS256/HS384/HS512)
	PublicKey        string              `yaml:"public_key" json:"public_key"`               // Ed25519 public key path or base64 (for EdDSA)
	Algorithm        string              `yaml:"algorithm" json:"algorithm"`                 // HS256, HS384, HS512, EdDSA
	Header           string              `yaml:"header" json:"header"`                       // Authorization header name (default: "Authorization")
	Prefix           string              `yaml:"prefix" json:"prefix"`                       // Token prefix (default: "Bearer ")
	Required         bool                `yaml:"required" json:"required"`                   // Require JWT for all requests
	ExcludePaths     []string            `yaml:"exclude_paths" json:"exclude_paths"`         // Paths to exclude from JWT validation
	ClaimsValidation JWTClaimsValidation `yaml:"claims_validation" json:"claims_validation"` // Additional claim validation
}

// JWTClaimsValidation configures additional claim validation.
type JWTClaimsValidation struct {
	Issuer   string `yaml:"issuer" json:"issuer"`     // Expected issuer
	Audience string `yaml:"audience" json:"audience"` // Expected audience
}

// BasicAuthConfig represents Basic Authentication configuration.
type BasicAuthConfig struct {
	Enabled      bool              `yaml:"enabled" json:"enabled"`
	Users        map[string]string `yaml:"users" json:"users"`                 // username -> password (hashed or plain)
	Realm        string            `yaml:"realm" json:"realm"`                 // Auth realm (default: "Restricted")
	ExcludePaths []string          `yaml:"exclude_paths" json:"exclude_paths"` // Paths to exclude
	Hash         string            `yaml:"hash" json:"hash"`                   // Password hash: "sha256", "plain" (default: "sha256")
}

// StripPrefixConfig represents path prefix stripping configuration.
type StripPrefixConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Prefix        string `yaml:"prefix" json:"prefix"`                 // Prefix to strip from request path (e.g., "/api/v1")
	RedirectSlash bool   `yaml:"redirect_slash" json:"redirect_slash"` // Redirect /prefix to /prefix/
}

// ValidatorConfig represents request/response validation configuration.
type ValidatorConfig struct {
	Enabled          bool              `yaml:"enabled" json:"enabled"`
	ValidateRequest  bool              `yaml:"validate_request" json:"validate_request"`   // Validate incoming requests
	ValidateResponse bool              `yaml:"validate_response" json:"validate_response"` // Validate outgoing responses
	MaxBodySize      int64             `yaml:"max_body_size" json:"max_body_size"`         // Maximum body size to validate (default: 1MB)
	ContentTypes     []string          `yaml:"content_types" json:"content_types"`         // Content types to validate
	RequiredHeaders  map[string]string `yaml:"required_headers" json:"required_headers"`   // Header name -> regex pattern
	ForbiddenHeaders []string          `yaml:"forbidden_headers" json:"forbidden_headers"` // Headers that must not be present
	QueryRules       map[string]string `yaml:"query_rules" json:"query_rules"`             // Query param -> regex pattern
	PathPatterns     map[string]string `yaml:"path_patterns" json:"path_patterns"`         // Path prefix -> pattern
	ExcludePaths     []string          `yaml:"exclude_paths" json:"exclude_paths"`         // Paths to exclude from validation
	RejectOnFailure  bool              `yaml:"reject_on_failure" json:"reject_on_failure"` // Reject request on validation failure
	LogOnly          bool              `yaml:"log_only" json:"log_only"`                   // Only log violations, don't reject
}

// OAuth2Config represents OAuth2/OIDC authentication configuration.
type OAuth2Config struct {
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	IssuerURL        string   `yaml:"issuer_url" json:"issuer_url"`               // OIDC issuer URL
	ClientID         string   `yaml:"client_id" json:"client_id"`                 // OAuth2 client ID
	ClientSecret     string   `yaml:"client_secret" json:"-"`                     // OAuth2 client secret
	JwksURL          string   `yaml:"jwks_url" json:"jwks_url"`                   // JWKS endpoint URL (optional)
	Audience         string   `yaml:"audience" json:"audience"`                   // Expected audience
	Scopes           []string `yaml:"scopes" json:"scopes"`                       // Required scopes
	Header           string   `yaml:"header" json:"header"`                       // Authorization header name (default: "Authorization")
	Prefix           string   `yaml:"prefix" json:"prefix"`                       // Token prefix (default: "Bearer ")
	ExcludePaths     []string `yaml:"exclude_paths" json:"exclude_paths"`         // Paths to exclude
	IntrospectionURL string   `yaml:"introspection_url" json:"introspection_url"` // Token introspection endpoint (optional)
	CacheDuration    string   `yaml:"cache_duration" json:"cache_duration"`       // JWKS cache duration (default: "1h")
}

// APIKeyConfig represents API Key authentication configuration.
type APIKeyConfig struct {
	Enabled      bool              `yaml:"enabled" json:"enabled"`
	Keys         map[string]string `yaml:"keys" json:"keys"`                   // key_id -> api_key (or hash)
	Header       string            `yaml:"header" json:"header"`               // Header name (default: "X-API-Key")
	QueryParam   string            `yaml:"query_param" json:"query_param"`     // Query parameter name (alternative)
	ExcludePaths []string          `yaml:"exclude_paths" json:"exclude_paths"` // Paths to exclude
	Hash         string            `yaml:"hash" json:"hash"`                   // Key hash: "sha256", "plain" (default: "sha256")
}

// HMACConfig represents HMAC signature verification configuration.
type HMACConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	Secret          string   `yaml:"secret" json:"-"`                          // HMAC secret key
	Algorithm       string   `yaml:"algorithm" json:"algorithm"`               // Hash algorithm: "sha256", "sha512"
	Header          string   `yaml:"header" json:"header"`                     // Signature header (default: "X-Signature")
	Prefix          string   `yaml:"prefix" json:"prefix"`                     // Signature prefix (e.g., "sha256=")
	Encoding        string   `yaml:"encoding" json:"encoding"`                 // Signature encoding: "hex", "base64"
	UseBody         bool     `yaml:"use_body" json:"use_body"`                 // Include body in signature
	ExcludePaths    []string `yaml:"exclude_paths" json:"exclude_paths"`       // Paths to exclude
	TimestampHeader string   `yaml:"timestamp_header" json:"timestamp_header"` // Optional timestamp header
	MaxAge          string   `yaml:"max_age" json:"max_age"`                   // Maximum age for timestamp
}

// TransformerConfig represents response transformation configuration.
type TransformerConfig struct {
	Enabled          bool              `yaml:"enabled" json:"enabled"`
	Compress         bool              `yaml:"compress" json:"compress"`                     // Enable gzip compression
	CompressLevel    int               `yaml:"compress_level" json:"compress_level"`         // Gzip level (1-9)
	MinCompressSize  int               `yaml:"min_compress_size" json:"min_compress_size"`   // Minimum size to compress
	AddHeaders       map[string]string `yaml:"add_headers" json:"add_headers"`               // Headers to add
	RemoveHeaders    []string          `yaml:"remove_headers" json:"remove_headers"`         // Headers to remove
	RewriteBody      map[string]string `yaml:"rewrite_body" json:"rewrite_body"`             // Pattern -> replacement
	ExcludePaths     []string          `yaml:"exclude_paths" json:"exclude_paths"`           // Paths to exclude
	ExcludeMIMETypes []string          `yaml:"exclude_mime_types" json:"exclude_mime_types"` // MIME types to exclude
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

// CSPConfig represents Content Security Policy configuration.
type CSPConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	DefaultSrc      []string `yaml:"default_src" json:"default_src"`           // default-src directive
	ScriptSrc       []string `yaml:"script_src" json:"script_src"`             // script-src directive
	StyleSrc        []string `yaml:"style_src" json:"style_src"`               // style-src directive
	ImgSrc          []string `yaml:"img_src" json:"img_src"`                   // img-src directive
	ConnectSrc      []string `yaml:"connect_src" json:"connect_src"`           // connect-src directive
	FontSrc         []string `yaml:"font_src" json:"font_src"`                 // font-src directive
	ObjectSrc       []string `yaml:"object_src" json:"object_src"`             // object-src directive
	MediaSrc        []string `yaml:"media_src" json:"media_src"`               // media-src directive
	FrameSrc        []string `yaml:"frame_src" json:"frame_src"`               // frame-src directive
	FrameAncestors  []string `yaml:"frame_ancestors" json:"frame_ancestors"`   // frame-ancestors directive
	FormAction      []string `yaml:"form_action" json:"form_action"`           // form-action directive
	BaseURI         []string `yaml:"base_uri" json:"base_uri"`                 // base-uri directive
	UpgradeInsecure bool     `yaml:"upgrade_insecure" json:"upgrade_insecure"` // upgrade-insecure-requests
	BlockAllMixed   bool     `yaml:"block_all_mixed" json:"block_all_mixed"`   // block-all-mixed-content
	ReportURI       string   `yaml:"report_uri" json:"report_uri"`             // report-uri directive
	ReportTo        string   `yaml:"report_to" json:"report_to"`               // report-to directive
	NonceScript     bool     `yaml:"nonce_script" json:"nonce_script"`         // Generate nonce for scripts
	NonceStyle      bool     `yaml:"nonce_style" json:"nonce_style"`           // Generate nonce for styles
	UnsafeInline    bool     `yaml:"unsafe_inline" json:"unsafe_inline"`       // Allow 'unsafe-inline'
	UnsafeEval      bool     `yaml:"unsafe_eval" json:"unsafe_eval"`           // Allow 'unsafe-eval'
	ExcludePaths    []string `yaml:"exclude_paths" json:"exclude_paths"`       // Paths to exclude
}

// RequestIDConfig represents request ID configuration.
type RequestIDConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`                 // Enable request ID generation
	Header       string   `yaml:"header" json:"header"`                   // Header name (default: X-Request-ID)
	Generate     bool     `yaml:"generate" json:"generate"`               // Generate ID if not present
	Length       int      `yaml:"length" json:"length"`                   // ID length in bytes (default: 16)
	Response     bool     `yaml:"response" json:"response"`               // Include ID in response headers
	ExcludePaths []string `yaml:"exclude_paths" json:"exclude_paths"`     // Paths to exclude
}

// LoggingConfig represents HTTP request logging configuration.
type LoggingConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`                   // Enable request logging
	Format          string   `yaml:"format" json:"format"`                     // Format: json, combined, common, custom
	CustomFormat    string   `yaml:"custom_format" json:"custom_format"`       // Custom format template
	Fields          []string `yaml:"fields" json:"fields"`                     // JSON fields to include
	ExcludePaths    []string `yaml:"exclude_paths" json:"exclude_paths"`       // Paths to exclude from logging
	ExcludeStatus   []int    `yaml:"exclude_status" json:"exclude_status"`     // Status codes to exclude
	MinDuration     string   `yaml:"min_duration" json:"min_duration"`         // Only log slower requests (e.g., "100ms")
	RequestHeaders  []string `yaml:"request_headers" json:"request_headers"`   // Request headers to log
	ResponseHeaders []string `yaml:"response_headers" json:"response_headers"` // Response headers to log
}

// MetricsConfig represents HTTP request metrics configuration.
type MetricsConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`                 // Enable metrics collection
	Namespace      string   `yaml:"namespace" json:"namespace"`             // Metrics namespace prefix
	Subsystem      string   `yaml:"subsystem" json:"subsystem"`             // Metrics subsystem
	ExcludePaths   []string `yaml:"exclude_paths" json:"exclude_paths"`     // Paths to exclude
	ExcludeMethods []string `yaml:"exclude_methods" json:"exclude_methods"` // HTTP methods to exclude
	EnableLatency  bool     `yaml:"enable_latency" json:"enable_latency"`   // Enable latency histograms
	EnableSize     bool     `yaml:"enable_size" json:"enable_size"`         // Enable size metrics
	EnableActive   bool     `yaml:"enable_active" json:"enable_active"`     // Enable active requests gauge
	LatencyBuckets []float64 `yaml:"latency_buckets" json:"latency_buckets"` // Latency buckets in seconds
}

// RewriteRule represents a single URL rewrite rule.
type RewriteRule struct {
	Pattern     string `yaml:"pattern" json:"pattern"`         // Regex pattern to match
	Replacement string `yaml:"replacement" json:"replacement"` // Replacement string
	Flag        string `yaml:"flag" json:"flag"`               // Flag: last, break, redirect, permanent
}

// RewriteConfig represents URL rewrite configuration.
type RewriteConfig struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`             // Enable URL rewriting
	Rules        []RewriteRule `yaml:"rules" json:"rules"`                 // Rewrite rules
	ExcludePaths []string      `yaml:"exclude_paths" json:"exclude_paths"` // Paths to exclude
}

// ForceSSLConfig represents HTTPS enforcement configuration.
type ForceSSLConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`             // Enable HTTPS enforcement
	Permanent    bool     `yaml:"permanent" json:"permanent"`         // Use 301 instead of 307
	ExcludePaths []string `yaml:"exclude_paths" json:"exclude_paths"` // Paths to exclude
	ExcludeHosts []string `yaml:"exclude_hosts" json:"exclude_hosts"` // Hosts to exclude
	Port         int      `yaml:"port" json:"port"`                   // HTTPS port (default: 443)
	HeaderKey    string   `yaml:"header_key" json:"header_key"`       // TLS termination header
	HeaderValue  string   `yaml:"header_value" json:"header_value"`   // Expected header value
}

// CSRFConfig represents CSRF protection configuration.
type CSRFConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`             // Enable CSRF protection
	CookieName     string   `yaml:"cookie_name" json:"cookie_name"`     // CSRF cookie name
	HeaderName     string   `yaml:"header_name" json:"header_name"`     // Header to check for token
	FieldName      string   `yaml:"field_name" json:"field_name"`       // Form field name
	ExcludePaths   []string `yaml:"exclude_paths" json:"exclude_paths"` // Paths to exclude
	ExcludeMethods []string `yaml:"exclude_methods" json:"exclude_methods"` // Methods that don't require CSRF
	CookiePath     string   `yaml:"cookie_path" json:"cookie_path"`     // Cookie path
	CookieDomain   string   `yaml:"cookie_domain" json:"cookie_domain"` // Cookie domain
	CookieMaxAge   int      `yaml:"cookie_max_age" json:"cookie_max_age"` // Cookie max age in seconds
	CookieSecure   bool     `yaml:"cookie_secure" json:"cookie_secure"` // Secure cookie flag
	CookieHTTPOnly bool     `yaml:"cookie_httponly" json:"cookie_httponly"` // HTTPOnly cookie flag
	TokenLength    int      `yaml:"token_length" json:"token_length"`   // Token length in bytes
}

// SecureHeadersConfig represents security headers configuration.
type SecureHeadersConfig struct {
	Enabled                       bool     `yaml:"enabled" json:"enabled"`                                   // Enable security headers
	XFrameOptions                 string   `yaml:"x_frame_options" json:"x_frame_options"`                   // X-Frame-Options
	XContentTypeOptions           bool     `yaml:"x_content_type_options" json:"x_content_type_options"`     // X-Content-Type-Options
	XXSSProtection                string   `yaml:"x_xss_protection" json:"x_xss_protection"`                 // X-XSS-Protection
	ReferrerPolicy                string   `yaml:"referrer_policy" json:"referrer_policy"`                   // Referrer-Policy
	ContentSecurityPolicy         string   `yaml:"content_security_policy" json:"content_security_policy"`   // Content-Security-Policy
	StrictTransportPolicy         *HSTSConfig `yaml:"strict_transport_policy" json:"strict_transport_policy"` // HSTS config
	XPermittedCrossDomainPolicies string   `yaml:"x_permitted_cross_domain_policies" json:"x_permitted_cross_domain_policies"`
	XDownloadOptions              string   `yaml:"x_download_options" json:"x_download_options"`             // X-Download-Options
	XDNSPrefetchControl           string   `yaml:"x_dns_prefetch_control" json:"x_dns_prefetch_control"`     // X-DNS-Prefetch-Control
	PermissionsPolicy             string   `yaml:"permissions_policy" json:"permissions_policy"`             // Permissions-Policy
	CrossOriginEmbedderPolicy     string   `yaml:"cross_origin_embedder_policy" json:"cross_origin_embedder_policy"` // COEP
	CrossOriginOpenerPolicy       string   `yaml:"cross_origin_opener_policy" json:"cross_origin_opener_policy"`     // COOP
	CrossOriginResourcePolicy     string   `yaml:"cross_origin_resource_policy" json:"cross_origin_resource_policy"` // CORP
	CacheControl                  string   `yaml:"cache_control" json:"cache_control"`                       // Cache-Control
	ExcludePaths                  []string `yaml:"exclude_paths" json:"exclude_paths"`                       // Paths to exclude
}

// HSTSConfig configures HTTP Strict Transport Security.
type HSTSConfig struct {
	MaxAge            int  `yaml:"max_age" json:"max_age"`                         // max-age in seconds
	IncludeSubdomains bool `yaml:"include_subdomains" json:"include_subdomains"`     // includeSubDomains
	Preload           bool `yaml:"preload" json:"preload"`                         // preload
}

// CoalesceConfig represents request coalescing configuration.
type CoalesceConfig struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`             // Enable request coalescing
	TTL          string        `yaml:"ttl" json:"ttl"`                     // Coalescing window duration
	MaxRequests  int           `yaml:"max_requests" json:"max_requests"`   // Maximum requests to coalesce
	ExcludePaths []string      `yaml:"exclude_paths" json:"exclude_paths"` // Paths to exclude
}

// BotDetectionConfig represents bot detection middleware configuration.
type BotDetectionConfig struct {
	Enabled              bool                  `yaml:"enabled" json:"enabled"`                           // Enable bot detection
	Action               string                `yaml:"action" json:"action"`                             // Action: allow, block, challenge, throttle, log
	BlockKnownBots       bool                  `yaml:"block_known_bots" json:"block_known_bots"`         // Block known bad bots
	AllowVerified        bool                  `yaml:"allow_verified" json:"allow_verified"`             // Allow verified good bots (Googlebot, etc.)
	RequestRateThreshold int                   `yaml:"request_rate_threshold" json:"request_rate_threshold"` // Requests per minute threshold
	JA3Fingerprints      []string              `yaml:"ja3_fingerprints" json:"ja3_fingerprints"`         // Known bot JA3 fingerprints
	ChallengePath        string                `yaml:"challenge_path" json:"challenge_path"`             // Path to redirect challenges to
	ExcludePaths         []string              `yaml:"exclude_paths" json:"exclude_paths"`               // Paths to exclude from detection
	UserAgentRules       []BotUserAgentRule    `yaml:"user_agent_rules" json:"user_agent_rules"`         // User-Agent based rules
	CustomHeaders        []BotHeaderRule       `yaml:"custom_headers" json:"custom_headers"`             // Header-based detection rules
}

// BotUserAgentRule defines a rule based on User-Agent string.
type BotUserAgentRule struct {
	Pattern string `yaml:"pattern" json:"pattern"` // Regex pattern
	Action  string `yaml:"action" json:"action"`   // Action: allow, block, challenge, throttle, log
	Name    string `yaml:"name" json:"name"`       // Rule name for logging
}

// BotHeaderRule defines a rule based on request headers.
type BotHeaderRule struct {
	Header  string `yaml:"header" json:"header"`   // Header name
	Pattern string `yaml:"pattern" json:"pattern"` // Regex pattern to match
	Action  string `yaml:"action" json:"action"`   // Action
	Name    string `yaml:"name" json:"name"`       // Rule name
}

// RealIPConfig represents RealIP middleware configuration.
type RealIPConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`                   // Enable RealIP extraction
	Headers         []string `yaml:"headers" json:"headers"`                   // Headers to check (in order of preference)
	TrustedProxies  []string `yaml:"trusted_proxies" json:"trusted_proxies"`   // CIDR ranges of trusted proxies
	RejectUntrusted bool     `yaml:"reject_untrusted" json:"reject_untrusted"` // Reject requests from untrusted proxies
}

// TraceConfig represents distributed tracing configuration.
type TraceConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`                   // Enable tracing
	ServiceName     string   `yaml:"service_name" json:"service_name"`         // Service name for spans
	ServiceVersion  string   `yaml:"service_version" json:"service_version"`   // Service version
	Propagators     []string `yaml:"propagators" json:"propagators"`           // Propagation formats: w3c, b3, b3multi, jaeger
	SampleRate      float64  `yaml:"sample_rate" json:"sample_rate"`           // Sampling rate (0.0 to 1.0)
	BaggageHeaders  []string `yaml:"baggage_headers" json:"baggage_headers"`   // Headers to propagate as baggage
	ExcludePaths    []string `yaml:"exclude_paths" json:"exclude_paths"`       // Paths to exclude from tracing
	MaxBaggageItems int      `yaml:"max_baggage_items" json:"max_baggage_items"` // Max baggage items per request
	MaxBaggageSize  int      `yaml:"max_baggage_size" json:"max_baggage_size"`   // Max total baggage size in bytes
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
	Enabled        bool              `yaml:"enabled" json:"enabled"`                   // Enable caching
	MaxEntries     int               `yaml:"max_entries" json:"max_entries"`           // Max number of cached entries
	DefaultTTL     string            `yaml:"default_ttl" json:"default_ttl"`           // Default cache TTL (e.g., "5m")
	MaxSize        int64             `yaml:"max_size" json:"max_size"`                 // Max cache size in bytes
	Methods        []string          `yaml:"methods" json:"methods"`                     // HTTP methods to cache
	StatusCodes    []int             `yaml:"status_codes" json:"status_codes"`         // Status codes to cache
	VaryHeaders    []string          `yaml:"vary_headers" json:"vary_headers"`         // Headers to vary cache on
	ExcludePaths   []string          `yaml:"exclude_paths" json:"exclude_paths"`       // Paths to exclude from caching
	CachePrivate   bool              `yaml:"cache_private" json:"cache_private"`       // Cache private responses
	CacheCookies   bool              `yaml:"cache_cookies" json:"cache_cookies"`       // Cache responses with cookies
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
	Enabled      bool                  `yaml:"enabled" json:"enabled"`
	SyncInterval string                `yaml:"sync_interval" json:"sync_interval"` // e.g. "5s"
	Store        *RateLimitStoreConfig `yaml:"store" json:"store"`
	Rules        []WAFRateLimitRule    `yaml:"rules" json:"rules"`
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
	Name       string           `yaml:"name" json:"name"`
	Path       string           `yaml:"path" json:"path"`
	Host       string           `yaml:"host" json:"host"`
	Methods    []string         `yaml:"methods" json:"methods"`
	Pool       string           `yaml:"pool" json:"pool"`
	Middleware []map[string]any `yaml:"middleware" json:"middleware"`
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
	MCPToken   string `yaml:"mcp_token" json:"-"`
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

// GeoDNSConfig represents Geo-DNS routing configuration.
type GeoDNSConfig struct {
	Enabled     bool         `yaml:"enabled" json:"enabled"`
	DefaultPool string       `yaml:"default_pool" json:"default_pool"`
	Rules       []GeoDNSRule `yaml:"rules" json:"rules"`
}

// GeoDNSRule defines a geographic routing rule.
type GeoDNSRule struct {
	ID       string            `yaml:"id" json:"id"`
	Country  string            `yaml:"country" json:"country"` // ISO 3166-1 alpha-2 or "*"
	Region   string            `yaml:"region" json:"region"`   // State/Province code
	Pool     string            `yaml:"pool" json:"pool"`
	Fallback string            `yaml:"fallback" json:"fallback"`
	Weight   int               `yaml:"weight" json:"weight"`
	Headers  map[string]string `yaml:"headers" json:"headers"`
}

// ShadowConfig represents request shadowing/mirroring configuration.
type ShadowConfig struct {
	Enabled     bool           `yaml:"enabled" json:"enabled"`
	Targets     []ShadowTarget `yaml:"targets" json:"targets"`
	Percentage  float64        `yaml:"percentage" json:"percentage"`
	CopyHeaders bool           `yaml:"copy_headers" json:"copy_headers"`
	CopyBody    bool           `yaml:"copy_body" json:"copy_body"`
	Timeout     string         `yaml:"timeout" json:"timeout"`
}

// ShadowTarget defines a shadow/mirror target.
type ShadowTarget struct {
	Pool       string  `yaml:"pool" json:"pool"`
	Percentage float64 `yaml:"percentage" json:"percentage"`
}

// RateLimitStoreConfig represents rate limit storage backend configuration.
type RateLimitStoreConfig struct {
	Type     string            `yaml:"type" json:"type"` // "memory", "redis"
	Address  string            `yaml:"address" json:"address"`
	Password string            `yaml:"password" json:"-"`
	Database int               `yaml:"database" json:"database"`
	Options  map[string]string `yaml:"options" json:"options"`
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
