package config

import (
	"os"
	"strings"
	"testing"
)

// ============================================================================
// ExpandEnvWithPrefixes tests
// ============================================================================

func TestExpandEnvWithPrefixes_EmptyPrefixes(t *testing.T) {
	os.Setenv("TEST_VAR", "value")
	defer os.Unsetenv("TEST_VAR")

	// Empty prefixes should behave like ExpandEnv (expand all)
	got := ExpandEnvWithPrefixes("${TEST_VAR}", nil)
	if got != "value" {
		t.Errorf("ExpandEnvWithPrefixes with nil prefixes = %q, want %q", got, "value")
	}

	got = ExpandEnvWithPrefixes("${TEST_VAR}", []string{})
	if got != "value" {
		t.Errorf("ExpandEnvWithPrefixes with empty prefixes = %q, want %q", got, "value")
	}
}

func TestExpandEnvWithPrefixes_MatchingPrefix(t *testing.T) {
	os.Setenv("OLB_HOST", "example.com")
	defer os.Unsetenv("OLB_HOST")

	got := ExpandEnvWithPrefixes("${OLB_HOST}", []string{"OLB_"})
	if got != "example.com" {
		t.Errorf("ExpandEnvWithPrefixes matching prefix = %q, want %q", got, "example.com")
	}
}

func TestExpandEnvWithPrefixes_NonMatchingPrefix(t *testing.T) {
	os.Setenv("SECRET_KEY", "secret123")
	defer os.Unsetenv("SECRET_KEY")

	// Variable doesn't match allowed prefix, should be left as-is
	got := ExpandEnvWithPrefixes("${SECRET_KEY}", []string{"OLB_"})
	if got != "${SECRET_KEY}" {
		t.Errorf("ExpandEnvWithPrefixes non-matching prefix = %q, want %q", got, "${SECRET_KEY}")
	}
}

func TestExpandEnvWithPrefixes_MatchingPrefixWithDefault(t *testing.T) {
	os.Setenv("OLB_PORT", "9090")
	defer os.Unsetenv("OLB_PORT")

	// Matching prefix, var is set => use actual value
	got := ExpandEnvWithPrefixes("${OLB_PORT:-8080}", []string{"OLB_"})
	if got != "9090" {
		t.Errorf("ExpandEnvWithPrefixes matching+set = %q, want %q", got, "9090")
	}
}

func TestExpandEnvWithPrefixes_MatchingPrefixDefaultUsed(t *testing.T) {
	// Matching prefix, var not set => use default
	got := ExpandEnvWithPrefixes("${OLB_MISSING_XYZ:-fallback}", []string{"OLB_"})
	if got != "fallback" {
		t.Errorf("ExpandEnvWithPrefixes matching+default = %q, want %q", got, "fallback")
	}
}

func TestExpandEnvWithPrefixes_NonMatchingPrefixWithDefault(t *testing.T) {
	// Non-matching prefix with default syntax => left as-is including default syntax
	got := ExpandEnvWithPrefixes("${SECRET_KEY:-fallback}", []string{"OLB_"})
	if got != "${SECRET_KEY:-fallback}" {
		t.Errorf("ExpandEnvWithPrefixes non-matching+default = %q, want %q", got, "${SECRET_KEY:-fallback}")
	}
}

func TestExpandEnvWithPrefixes_MultiplePrefixes(t *testing.T) {
	os.Setenv("OLB_ADDR", ":8080")
	os.Setenv("APP_DEBUG", "true")
	defer os.Unsetenv("OLB_ADDR")
	defer os.Unsetenv("APP_DEBUG")

	got := ExpandEnvWithPrefixes("${OLB_ADDR} ${APP_DEBUG}", []string{"OLB_", "APP_"})
	if got != ":8080 true" {
		t.Errorf("ExpandEnvWithPrefixes multiple prefixes = %q, want %q", got, ":8080 true")
	}
}

// ============================================================================
// Validate branches
// ============================================================================

func TestValidate_InvalidProtocol(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{
			Name: "test", Address: ":8080", Protocol: "invalid",
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid protocol")
	}
	if !strings.Contains(err.Error(), "invalid protocol") {
		t.Errorf("error = %v, want invalid protocol error", err)
	}
}

func TestValidate_ListenerTLSCertWithoutKey(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{
			Name: "test", Address: ":443",
			TLS: &ListenerTLS{Enabled: true, CertFile: "/etc/ssl/cert.pem"},
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for TLS cert without key")
	}
	if !strings.Contains(err.Error(), "cert_file and key_file") {
		t.Errorf("error = %v, want cert/key mismatch error", err)
	}
}

func TestValidate_ListenerTLSKeyWithoutCert(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{
			Name: "test", Address: ":443",
			TLS: &ListenerTLS{Enabled: true, KeyFile: "/etc/ssl/key.pem"},
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for TLS key without cert")
	}
	if !strings.Contains(err.Error(), "cert_file and key_file") {
		t.Errorf("error = %v, want cert/key mismatch error", err)
	}
}

func TestValidate_ListenerMTLSNoClientCAs(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{
			Name: "test", Address: ":443",
			MTLS: &MTLSConfig{Enabled: true},
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for MTLS without client_cas")
	}
	if !strings.Contains(err.Error(), "mtls client_cas") {
		t.Errorf("error = %v, want mtls client_cas error", err)
	}
}

func TestValidate_BackendWeightNegative(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{Name: "test", Address: ":80"}},
		Pools: []*Pool{{
			Name: "pool",
			Backends: []*Backend{
				{Address: "10.0.0.1:8080", Weight: -5},
			},
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative backend weight")
	}
	if !strings.Contains(err.Error(), "weight must be non-negative") {
		t.Errorf("error = %v, want weight error", err)
	}
}

func TestValidate_BackendAddressNoColon(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{Name: "test", Address: ":80"}},
		Pools: []*Pool{{
			Name: "pool",
			Backends: []*Backend{
				{Address: "justahost"},
			},
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for backend address without colon")
	}
	if !strings.Contains(err.Error(), "must be host:port") {
		t.Errorf("error = %v, want host:port error", err)
	}
}

func TestValidate_ListenerAddressNoColon(t *testing.T) {
	cfg := &Config{
		Listeners: []*Listener{{
			Name: "test", Address: "noport",
		}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for listener address without colon")
	}
	if !strings.Contains(err.Error(), "must contain port") {
		t.Errorf("error = %v, want must contain port error", err)
	}
}

// ============================================================================
// validateServer tests
// ============================================================================

func TestValidate_ServerInvalidShutdownTimeout(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{ShutdownTimeout: "not-a-duration"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid shutdown_timeout")
	}
	if !strings.Contains(err.Error(), "invalid shutdown_timeout") {
		t.Errorf("error = %v, want invalid shutdown_timeout error", err)
	}
}

func TestValidate_ServerInvalidListenerStopTimeout(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{ListenerStopTimeout: "bad"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid listener_stop_timeout")
	}
	if !strings.Contains(err.Error(), "invalid listener_stop_timeout") {
		t.Errorf("error = %v, want invalid listener_stop_timeout error", err)
	}
}

func TestValidate_ServerInvalidProxyDrainWindow(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{ProxyDrainWindow: "xyz"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid proxy_drain_window")
	}
	if !strings.Contains(err.Error(), "invalid proxy_drain_window") {
		t.Errorf("error = %v, want invalid proxy_drain_window error", err)
	}
}

func TestValidate_ServerInvalidRollbackCheckInterval(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{RollbackCheckInterval: "abc"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid rollback_check_interval")
	}
	if !strings.Contains(err.Error(), "invalid rollback_check_interval") {
		t.Errorf("error = %v, want invalid rollback_check_interval error", err)
	}
}

func TestValidate_ServerInvalidRollbackMonitorDuration(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{RollbackMonitorDuration: "bad"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid rollback_monitor_duration")
	}
	if !strings.Contains(err.Error(), "invalid rollback_monitor_duration") {
		t.Errorf("error = %v, want invalid rollback_monitor_duration error", err)
	}
}

func TestValidate_ServerNegativeMaxConnectionsPerSource(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{MaxConnectionsPerSource: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative max_connections_per_source")
	}
	if !strings.Contains(err.Error(), "max_connections_per_source") {
		t.Errorf("error = %v, want max_connections_per_source error", err)
	}
}

func TestValidate_ServerNegativeMaxRetries(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Server = &ServerConfig{MaxRetries: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative max_retries")
	}
	if !strings.Contains(err.Error(), "max_retries") {
		t.Errorf("error = %v, want max_retries error", err)
	}
}

// ============================================================================
// validateHealthCheck tests
// ============================================================================

func TestValidate_HealthCheckInvalidType(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Type: "invalid",
		},
	}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid health check type")
	}
	if !strings.Contains(err.Error(), "invalid health_check type") {
		t.Errorf("error = %v, want invalid health_check type error", err)
	}
}

func TestValidate_HealthCheckHTTPNoPath(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Type: "http",
		},
	}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for http health check without path")
	}
	if !strings.Contains(err.Error(), "path is required for http type") {
		t.Errorf("error = %v, want path required error", err)
	}
}

func TestValidate_HealthCheckInvalidInterval(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Interval: "not-a-duration",
		},
	}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid health check interval")
	}
	if !strings.Contains(err.Error(), "invalid health_check interval") {
		t.Errorf("error = %v, want invalid interval error", err)
	}
}

func TestValidate_HealthCheckTimeoutGeInterval(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Interval: "5s",
			Timeout:  "5s",
		},
	}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for timeout >= interval")
	}
	if !strings.Contains(err.Error(), "timeout must be less than interval") {
		t.Errorf("error = %v, want timeout < interval error", err)
	}
}

func TestValidate_HealthCheckTimeoutWithoutInterval(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Timeout: "not-a-duration",
		},
	}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid health check timeout without interval")
	}
	if !strings.Contains(err.Error(), "invalid health_check timeout") {
		t.Errorf("error = %v, want invalid timeout error", err)
	}
}

func TestValidate_HealthCheckInvalidTimeoutWithValidInterval(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Interval: "10s",
			Timeout:  "bad",
		},
	}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid health check timeout with valid interval")
	}
	if !strings.Contains(err.Error(), "invalid health_check timeout") {
		t.Errorf("error = %v, want invalid timeout error", err)
	}
}

func TestValidate_HealthCheckValidTimeoutLessThanInterval(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Interval: "10s",
			Timeout:  "3s",
		},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_HealthCheckValidIntervalOnly(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Pools = []*Pool{{
		Name: "pool",
		Backends: []*Backend{{Address: "10.0.0.1:8080"}},
		HealthCheck: &HealthCheck{
			Interval: "10s",
		},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateAdmin tests
// ============================================================================

func TestValidate_AdminInvalidAddress(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Admin = &Admin{
		Address: "noport",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for admin address without port")
	}
	if !strings.Contains(err.Error(), "admin: invalid address") {
		t.Errorf("error = %v, want admin invalid address error", err)
	}
}

func TestValidate_AdminNegativeRateLimit(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Admin = &Admin{
		Address:              ":9090",
		RateLimitMaxRequests: -5,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative admin rate_limit_max_requests")
	}
	if !strings.Contains(err.Error(), "rate_limit_max_requests must be non-negative") {
		t.Errorf("error = %v, want rate_limit_max_requests error", err)
	}
}

func TestValidate_AdminInvalidRateLimitWindow(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Admin = &Admin{
		Address:         ":9090",
		RateLimitWindow: "not-a-duration",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid admin rate_limit_window")
	}
	if !strings.Contains(err.Error(), "invalid rate_limit_window") {
		t.Errorf("error = %v, want invalid rate_limit_window error", err)
	}
}

func TestValidate_AdminValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Admin = &Admin{
		Address:              ":9090",
		RateLimitMaxRequests: 100,
		RateLimitWindow:      "1m",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateLogging tests
// ============================================================================

func TestValidate_LoggingInvalidLevel(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Logging = &Logging{
		Level: "verbose",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid logging level")
	}
	if !strings.Contains(err.Error(), "logging: invalid level") {
		t.Errorf("error = %v, want invalid logging level error", err)
	}
}

func TestValidate_LoggingInvalidFormat(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Logging = &Logging{
		Format: "xml",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid logging format")
	}
	if !strings.Contains(err.Error(), "logging: invalid format") {
		t.Errorf("error = %v, want invalid logging format error", err)
	}
}

func TestValidate_LoggingValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Logging = &Logging{
		Level:  "info",
		Format: "json",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateCluster tests
// ============================================================================

func TestValidate_ClusterBindPortOutOfRange(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled:  true,
		BindPort: 70000,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for cluster bind_port out of range")
	}
	if !strings.Contains(err.Error(), "bind_port must be between 0 and 65535") {
		t.Errorf("error = %v, want bind_port range error", err)
	}
}

func TestValidate_ClusterBindPortNegative(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled:  true,
		BindPort: -1,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative cluster bind_port")
	}
	if !strings.Contains(err.Error(), "bind_port must be between 0 and 65535") {
		t.Errorf("error = %v, want bind_port range error", err)
	}
}

func TestValidate_ClusterEmptyPeer(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled: true,
		Peers:   []string{""},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty cluster peer")
	}
	if !strings.Contains(err.Error(), "peer 0: address is required") {
		t.Errorf("error = %v, want peer address required error", err)
	}
}

func TestValidate_ClusterPeerNoColon(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled: true,
		Peers:   []string{"justahost"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for cluster peer without colon")
	}
	if !strings.Contains(err.Error(), "must be host:port") {
		t.Errorf("error = %v, want host:port error", err)
	}
}

func TestValidate_ClusterInvalidElectionTick(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled:      true,
		ElectionTick: "bad",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid election_tick")
	}
	if !strings.Contains(err.Error(), "invalid election_tick") {
		t.Errorf("error = %v, want invalid election_tick error", err)
	}
}

func TestValidate_ClusterInvalidHeartbeatTick(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled:       true,
		HeartbeatTick: "bad",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid heartbeat_tick")
	}
	if !strings.Contains(err.Error(), "invalid heartbeat_tick") {
		t.Errorf("error = %v, want invalid heartbeat_tick error", err)
	}
}

func TestValidate_ClusterValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Cluster = &ClusterConfig{
		Enabled:       true,
		BindPort:      7946,
		Peers:         []string{"10.0.0.2:7946"},
		ElectionTick:  "2s",
		HeartbeatTick: "500ms",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateTLS tests
// ============================================================================

func TestValidate_TLSCertWithoutKey(t *testing.T) {
	cfg := validBaseConfig()
	cfg.TLS = &TLSConfig{
		CertFile: "/etc/ssl/cert.pem",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for TLS cert without key")
	}
	if !strings.Contains(err.Error(), "tls: cert_file and key_file") {
		t.Errorf("error = %v, want TLS cert/key mismatch error", err)
	}
}

func TestValidate_TLSKeyWithoutCert(t *testing.T) {
	cfg := validBaseConfig()
	cfg.TLS = &TLSConfig{
		KeyFile: "/etc/ssl/key.pem",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for TLS key without cert")
	}
	if !strings.Contains(err.Error(), "tls: cert_file and key_file") {
		t.Errorf("error = %v, want TLS cert/key mismatch error", err)
	}
}

func TestValidate_TLSACMENoEmail(t *testing.T) {
	cfg := validBaseConfig()
	cfg.TLS = &TLSConfig{
		ACME: &ACME{
			Enabled: true,
			Domains: []string{"example.com"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for ACME without email")
	}
	if !strings.Contains(err.Error(), "tls.acme: email is required") {
		t.Errorf("error = %v, want ACME email required error", err)
	}
}

func TestValidate_TLSACMENoDomains(t *testing.T) {
	cfg := validBaseConfig()
	cfg.TLS = &TLSConfig{
		ACME: &ACME{
			Enabled: true,
			Email:   "admin@example.com",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for ACME without domains")
	}
	if !strings.Contains(err.Error(), "tls.acme: at least one domain is required") {
		t.Errorf("error = %v, want ACME domains required error", err)
	}
}

func TestValidate_TLSValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.TLS = &TLSConfig{
		CertFile: "/etc/ssl/cert.pem",
		KeyFile:  "/etc/ssl/key.pem",
		ACME: &ACME{
			Enabled: true,
			Email:   "admin@example.com",
			Domains: []string{"example.com"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateWAF tests
// ============================================================================

func TestValidate_WAFInvalidMode(t *testing.T) {
	cfg := validBaseConfig()
	cfg.WAF = &WAFConfig{
		Enabled: true,
		Mode:    "aggressive",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid WAF mode")
	}
	if !strings.Contains(err.Error(), "waf: invalid mode") {
		t.Errorf("error = %v, want invalid WAF mode error", err)
	}
}

func TestValidate_WAFDetectionInvalidMode(t *testing.T) {
	cfg := validBaseConfig()
	cfg.WAF = &WAFConfig{
		Enabled: true,
		Mode:    "enforce",
		Detection: &WAFDetectionConfig{
			Enabled: true,
			Mode:    "invalid",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid WAF detection mode")
	}
	if !strings.Contains(err.Error(), "waf.detection: invalid mode") {
		t.Errorf("error = %v, want invalid detection mode error", err)
	}
}

func TestValidate_WAFRateLimitRuleNoID(t *testing.T) {
	cfg := validBaseConfig()
	cfg.WAF = &WAFConfig{
		Enabled: true,
		Mode:    "enforce",
		RateLimit: &WAFRateLimitConfig{
			Rules: []WAFRateLimitRule{
				{ID: "", Limit: 100, Window: "1m"},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for WAF rate limit rule without ID")
	}
	if !strings.Contains(err.Error(), "waf.rate_limit: rule 0: id is required") {
		t.Errorf("error = %v, want rule id required error", err)
	}
}

func TestValidate_WAFRateLimitRuleInvalidWindow(t *testing.T) {
	cfg := validBaseConfig()
	cfg.WAF = &WAFConfig{
		Enabled: true,
		Mode:    "enforce",
		RateLimit: &WAFRateLimitConfig{
			Rules: []WAFRateLimitRule{
				{ID: "r1", Window: "not-a-duration"},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid WAF rate limit window")
	}
	if !strings.Contains(err.Error(), "invalid window") {
		t.Errorf("error = %v, want invalid window error", err)
	}
}

func TestValidate_WAFValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.WAF = &WAFConfig{
		Enabled: true,
		Mode:    "enforce",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_WAFDetectionValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.WAF = &WAFConfig{
		Enabled: true,
		Mode:    "enforce",
		Detection: &WAFDetectionConfig{
			Enabled: true,
			Mode:    "monitor",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateProfiling tests
// ============================================================================

func TestValidate_ProfilingInvalidPprofAddr(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Profiling = &ProfilingConfig{
		Enabled:   true,
		PprofAddr: "noport",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for profiling pprof_addr without port")
	}
	if !strings.Contains(err.Error(), "profiling: invalid pprof_addr") {
		t.Errorf("error = %v, want invalid pprof_addr error", err)
	}
}

func TestValidate_ProfilingNegativeBlockProfileRate(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Profiling = &ProfilingConfig{
		Enabled:          true,
		BlockProfileRate: -1,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative block_profile_rate")
	}
	if !strings.Contains(err.Error(), "block_profile_rate must be non-negative") {
		t.Errorf("error = %v, want block_profile_rate error", err)
	}
}

func TestValidate_ProfilingNegativeMutexProfileFraction(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Profiling = &ProfilingConfig{
		Enabled:              true,
		MutexProfileFraction: -1,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative mutex_profile_fraction")
	}
	if !strings.Contains(err.Error(), "mutex_profile_fraction must be non-negative") {
		t.Errorf("error = %v, want mutex_profile_fraction error", err)
	}
}

func TestValidate_ProfilingValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Profiling = &ProfilingConfig{
		Enabled:              true,
		PprofAddr:            ":6060",
		BlockProfileRate:     1,
		MutexProfileFraction: 1,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateGeoDNS tests
// ============================================================================

func TestValidate_GeoDNSMissingDBPath(t *testing.T) {
	cfg := validBaseConfig()
	cfg.GeoDNS = &GeoDNSConfig{
		Enabled: true,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for GeoDNS without db_path")
	}
	if !strings.Contains(err.Error(), "geodns: db_path is required") {
		t.Errorf("error = %v, want db_path required error", err)
	}
}

func TestValidate_GeoDNSRuleNoPool(t *testing.T) {
	cfg := validBaseConfig()
	cfg.GeoDNS = &GeoDNSConfig{
		Enabled: true,
		DBPath:  "/path/to/mmdb",
		Rules: []GeoDNSRule{
			{Country: "US", Pool: ""},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for GeoDNS rule without pool")
	}
	if !strings.Contains(err.Error(), "geodns: rule 0: pool is required") {
		t.Errorf("error = %v, want rule pool required error", err)
	}
}

func TestValidate_GeoDNSValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.GeoDNS = &GeoDNSConfig{
		Enabled: true,
		DBPath:  "/path/to/mmdb",
		Rules: []GeoDNSRule{
			{Country: "US", Pool: "us-pool"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateShadow tests
// ============================================================================

func TestValidate_ShadowNoTargets(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Shadow = &ShadowConfig{
		Enabled: true,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for shadow without targets")
	}
	if !strings.Contains(err.Error(), "shadow: at least one target") {
		t.Errorf("error = %v, want at least one target error", err)
	}
}

func TestValidate_ShadowTargetNoPool(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Shadow = &ShadowConfig{
		Enabled: true,
		Targets: []ShadowTarget{
			{Pool: "", Percentage: 50},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for shadow target without pool")
	}
	if !strings.Contains(err.Error(), "shadow: target 0: pool is required") {
		t.Errorf("error = %v, want target pool required error", err)
	}
}

func TestValidate_ShadowTargetNegativePercentage(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Shadow = &ShadowConfig{
		Enabled: true,
		Targets: []ShadowTarget{
			{Pool: "test-pool", Percentage: -10},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for shadow target with negative percentage")
	}
	if !strings.Contains(err.Error(), "percentage must be between 0 and 100") {
		t.Errorf("error = %v, want percentage range error", err)
	}
}

func TestValidate_ShadowTargetPercentageOver100(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Shadow = &ShadowConfig{
		Enabled: true,
		Targets: []ShadowTarget{
			{Pool: "test-pool", Percentage: 150},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for shadow target with percentage > 100")
	}
	if !strings.Contains(err.Error(), "percentage must be between 0 and 100") {
		t.Errorf("error = %v, want percentage range error", err)
	}
}

func TestValidate_ShadowInvalidTimeout(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Shadow = &ShadowConfig{
		Enabled: true,
		Targets: []ShadowTarget{
			{Pool: "test-pool", Percentage: 50},
		},
		Timeout: "not-a-duration",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid shadow timeout")
	}
	if !strings.Contains(err.Error(), "shadow: invalid timeout") {
		t.Errorf("error = %v, want invalid timeout error", err)
	}
}

func TestValidate_ShadowValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Shadow = &ShadowConfig{
		Enabled: true,
		Targets: []ShadowTarget{
			{Pool: "test-pool", Percentage: 50},
		},
		Timeout: "5s",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

// ============================================================================
// validateMiddleware additional coverage
// ============================================================================

func TestValidate_MiddlewareRateLimitNegativeRPS(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		RateLimit: &RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: -1,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative rate limit requests_per_second")
	}
	if !strings.Contains(err.Error(), "requests_per_second must be non-negative") {
		t.Errorf("error = %v, want requests_per_second error", err)
	}
}

func TestValidate_MiddlewareCacheNegativeMaxSize(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		Cache: &CacheConfig{
			Enabled: true,
			MaxSize: -1,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative cache max_size")
	}
	if !strings.Contains(err.Error(), "max_size must be non-negative") {
		t.Errorf("error = %v, want max_size error", err)
	}
}

func TestValidate_MiddlewareCircuitBreakerNegativeThreshold(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:        true,
			ErrorThreshold: -1,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative circuit breaker error_threshold")
	}
	if !strings.Contains(err.Error(), "error_threshold must be non-negative") {
		t.Errorf("error = %v, want error_threshold error", err)
	}
}

func TestValidate_MiddlewareCircuitBreakerBadRate(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:             true,
			ErrorRateThreshold:  1.5,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for circuit breaker error_rate_threshold > 1")
	}
	if !strings.Contains(err.Error(), "error_rate_threshold must be between 0 and 1") {
		t.Errorf("error = %v, want error_rate_threshold range error", err)
	}
}

func TestValidate_MiddlewareMaxBodySizeNegative(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		MaxBodySize: &MaxBodySizeConfig{
			Enabled: true,
			MaxSize: -1,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative max_body_size")
	}
	if !strings.Contains(err.Error(), "max_body_size: max_size must be non-negative") {
		t.Errorf("error = %v, want max_body_size error", err)
	}
}

func TestValidate_MiddlewareTraceNegativeSampleRate(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		Trace: &TraceConfig{
			Enabled:     true,
			SampleRate:  -0.5,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative trace sample_rate")
	}
	if !strings.Contains(err.Error(), "sample_rate must be between 0 and 1") {
		t.Errorf("error = %v, want sample_rate error", err)
	}
}

func TestValidate_MiddlewareCompressionNegativeLevel(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		Compression: &CompressionConfig{
			Enabled: true,
			Level:   -2,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for compression level < -1")
	}
	if !strings.Contains(err.Error(), "level must be between -1 and 9") {
		t.Errorf("error = %v, want level range error", err)
	}
}

func TestValidate_MiddlewareCompressionValidLevelMinus1(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		Compression: &CompressionConfig{
			Enabled: true,
			Level:   -1,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_MiddlewareOAuth2WithJwksURL(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		OAuth2: &OAuth2Config{
			Enabled:  true,
			JwksURL:  "https://example.com/.well-known/jwks.json",
			ClientID: "test",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_MiddlewareValidHMAC(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		HMAC: &HMACConfig{
			Enabled: true,
			Secret:  "my-hmac-secret",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_MiddlewareValidAPIKey(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		APIKey: &APIKeyConfig{
			Enabled: true,
			Keys:    map[string]string{"k1": "v1"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_MiddlewareValidJWTWithPublicKey(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		JWT: &JWTConfig{
			Enabled:   true,
			PublicKey: "public-key-data",
			Algorithm: "EdDSA",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_MiddlewareCoalesceValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		Coalesce: &CoalesceConfig{
			Enabled: true,
			TTL:     "100ms",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_MiddlewareIPFilterWithDenyList(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = &MiddlewareConfig{
		IPFilter: &IPFilterConfig{
			Enabled:  true,
			DenyList: []string{"192.168.0.0/16"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}

func TestValidate_NilMiddleware(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Middleware = nil
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with nil middleware should pass: %v", err)
	}
}

// ============================================================================
// Validate - valid config with all sections populated
// ============================================================================

func TestValidate_FullConfig(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Listeners: []*Listener{{
			Name:     "https",
			Address:  ":443",
			Protocol: "https",
			TLS:      &ListenerTLS{Enabled: true, CertFile: "/cert.pem", KeyFile: "/key.pem"},
			MTLS:     &MTLSConfig{Enabled: true, ClientCAs: []string{"/ca.pem"}},
			Routes:   []*Route{{Path: "/", Pool: "backend"}},
		}},
		Pools: []*Pool{{
			Name:      "backend",
			Algorithm: "round_robin",
			Backends:  []*Backend{{Address: "10.0.0.1:8080", Weight: 100}},
			HealthCheck: &HealthCheck{
				Type:     "http",
				Path:     "/health",
				Interval: "10s",
				Timeout:  "3s",
			},
		}},
		Server: &ServerConfig{
			ShutdownTimeout:         "30s",
			ListenerStopTimeout:     "5s",
			ProxyDrainWindow:        "5s",
			RollbackCheckInterval:   "15s",
			RollbackMonitorDuration: "30s",
			MaxConnections:          10000,
			MaxConnectionsPerSource: 100,
			MaxRetries:              3,
		},
		Admin: &Admin{
			Address:              ":9090",
			RateLimitMaxRequests: 100,
			RateLimitWindow:      "1m",
		},
		Logging: &Logging{
			Level:  "info",
			Format: "json",
		},
		Cluster: &ClusterConfig{
			Enabled:       true,
			BindPort:      7946,
			Peers:         []string{"10.0.0.2:7946"},
			ElectionTick:  "2s",
			HeartbeatTick: "500ms",
		},
		TLS: &TLSConfig{
			CertFile: "/cert.pem",
			KeyFile:  "/key.pem",
			ACME: &ACME{
				Enabled: true,
				Email:   "admin@example.com",
				Domains: []string{"example.com"},
			},
		},
		WAF: &WAFConfig{
			Enabled: true,
			Mode:    "enforce",
			Detection: &WAFDetectionConfig{
				Enabled: true,
				Mode:    "enforce",
			},
			RateLimit: &WAFRateLimitConfig{
				Rules: []WAFRateLimitRule{
					{ID: "r1", Window: "1m", Limit: 100},
				},
			},
		},
		Profiling: &ProfilingConfig{
			Enabled:              true,
			PprofAddr:            ":6060",
			BlockProfileRate:     1,
			MutexProfileFraction: 1,
		},
		GeoDNS: &GeoDNSConfig{
			Enabled: true,
			DBPath:  "/path/to/mmdb",
			Rules: []GeoDNSRule{
				{Country: "US", Pool: "us-pool"},
			},
		},
		Shadow: &ShadowConfig{
			Enabled: true,
			Targets: []ShadowTarget{
				{Pool: "shadow-pool", Percentage: 10},
			},
			Timeout: "5s",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() on full config failed: %v", err)
	}
}
