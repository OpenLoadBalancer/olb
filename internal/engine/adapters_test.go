package engine

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/router"
)

func TestModifyBackend_Enable(t *testing.T) {
	pm := backend.NewPoolManager()
	pool := backend.NewPool("web", "round_robin")
	b := backend.NewBackend("127.0.0.1:8080", "127.0.0.1:8080")
	b.SetState(backend.StateDown)
	if err := pool.AddBackend(b); err != nil {
		t.Fatal(err)
	}
	pm.AddPool(pool)

	p := &engineBackendProvider{poolMgr: pm}

	if err := p.ModifyBackend("enable", "web", "127.0.0.1:8080"); err != nil {
		t.Fatalf("enable error: %v", err)
	}
	if b.State() != backend.StateUp {
		t.Errorf("expected StateUp, got %v", b.State())
	}
}

func TestModifyBackend_Disable(t *testing.T) {
	pm := backend.NewPoolManager()
	pool := backend.NewPool("web", "round_robin")
	b := backend.NewBackend("127.0.0.1:8080", "127.0.0.1:8080")
	if err := pool.AddBackend(b); err != nil {
		t.Fatal(err)
	}
	pm.AddPool(pool)

	p := &engineBackendProvider{poolMgr: pm}

	if err := p.ModifyBackend("disable", "web", "127.0.0.1:8080"); err != nil {
		t.Fatalf("disable error: %v", err)
	}
	if b.State() != backend.StateDown {
		t.Errorf("expected StateDown, got %v", b.State())
	}
}

func TestModifyBackend_NotFoundPool(t *testing.T) {
	pm := backend.NewPoolManager()
	p := &engineBackendProvider{poolMgr: pm}

	if err := p.ModifyBackend("enable", "nonexistent", "127.0.0.1:8080"); err == nil {
		t.Error("expected error for nonexistent pool")
	}
}

func TestModifyBackend_NotFoundBackend(t *testing.T) {
	pm := backend.NewPoolManager()
	pool := backend.NewPool("web", "round_robin")
	pm.AddPool(pool)

	p := &engineBackendProvider{poolMgr: pm}

	if err := p.ModifyBackend("enable", "web", "127.0.0.1:9999"); err == nil {
		t.Error("expected error for nonexistent backend")
	}
	if err := p.ModifyBackend("disable", "web", "127.0.0.1:9999"); err == nil {
		t.Error("expected error for nonexistent backend on disable")
	}
}

func TestModifyRoute_Update(t *testing.T) {
	rtr := router.NewRouter()
	p := &engineRouteProvider{rtr: rtr}

	// Add a route first
	if err := p.ModifyRoute("add", "example.com", "/api", "pool-a"); err != nil {
		t.Fatalf("add route: %v", err)
	}

	// Update to a new backend pool
	if err := p.ModifyRoute("update", "example.com", "/api", "pool-b"); err != nil {
		t.Fatalf("update route: %v", err)
	}
}

func TestModifyRoute_UnknownAction(t *testing.T) {
	rtr := router.NewRouter()
	p := &engineRouteProvider{rtr: rtr}

	if err := p.ModifyRoute("invalid", "example.com", "/api", "pool-a"); err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestIsMWEnabled(t *testing.T) {
	// nil middleware config
	if isMWEnabled(nil, "rate_limit") {
		t.Error("nil config should return false")
	}

	// Empty config — all nil sub-configs
	mw := &config.MiddlewareConfig{}
	for _, id := range []string{"rate_limit", "cors", "csp", "compression", "circuit_breaker", "retry",
		"cache", "ip_filter", "headers", "timeout", "max_body_size", "jwt", "oauth2", "basic_auth",
		"api_key", "hmac", "transformer", "request_id", "logging", "metrics", "rewrite", "forcessl",
		"csrf", "secure_headers", "coalesce", "bot_detection", "real_ip", "trace", "validator", "strip_prefix"} {
		if isMWEnabled(mw, id) {
			t.Errorf("empty config: %s should be disabled", id)
		}
	}

	// Unknown middleware
	if isMWEnabled(mw, "nonexistent") {
		t.Error("unknown id should return false")
	}

	// Enabled middleware
	mw.RateLimit = &config.RateLimitConfig{Enabled: true}
	mw.CORS = &config.CORSConfig{Enabled: true}
	mw.CSP = &config.CSPConfig{Enabled: true}
	mw.Cache = &config.CacheConfig{Enabled: true}
	mw.JWT = &config.JWTConfig{Enabled: true}
	mw.CSRF = &config.CSRFConfig{Enabled: true}
	mw.Metrics = &config.MetricsConfig{Enabled: true}
	mw.Logging = &config.LoggingConfig{Enabled: true}

	if !isMWEnabled(mw, "rate_limit") {
		t.Error("rate_limit should be enabled")
	}
	if !isMWEnabled(mw, "cors") {
		t.Error("cors should be enabled")
	}
	if !isMWEnabled(mw, "csp") {
		t.Error("csp should be enabled")
	}
	if !isMWEnabled(mw, "cache") {
		t.Error("cache should be enabled")
	}
	if !isMWEnabled(mw, "jwt") {
		t.Error("jwt should be enabled")
	}
	if !isMWEnabled(mw, "csrf") {
		t.Error("csrf should be enabled")
	}
	if !isMWEnabled(mw, "metrics") {
		t.Error("metrics should be enabled")
	}
	if !isMWEnabled(mw, "logging") {
		t.Error("logging should be enabled")
	}

	// Disabled middleware (present but not enabled)
	mw.Compression = &config.CompressionConfig{Enabled: false}
	if isMWEnabled(mw, "compression") {
		t.Error("compression should be disabled")
	}
}

func TestBuildMiddlewareStatus(t *testing.T) {
	e := &Engine{
		config: &config.Config{
			Middleware: &config.MiddlewareConfig{
				RateLimit: &config.RateLimitConfig{Enabled: true},
				CORS:      &config.CORSConfig{Enabled: false},
			},
		},
	}

	status := e.buildMiddlewareStatus()
	if len(status) == 0 {
		t.Fatal("expected non-empty status list")
	}

	found := map[string]bool{}
	for _, s := range status {
		found[s.ID] = s.Enabled
	}

	if !found["rate_limit"] {
		t.Error("rate_limit should be enabled")
	}
	if found["cors"] {
		t.Error("cors should be disabled")
	}
	if found["cache"] {
		t.Error("cache should be disabled (nil)")
	}
}

func TestBuildMiddlewareStatus_NilConfig(t *testing.T) {
	e := &Engine{config: nil}
	status := e.buildMiddlewareStatus()
	if len(status) == 0 {
		t.Fatal("expected non-empty status list even with nil config")
	}
	for _, s := range status {
		if s.Enabled {
			t.Errorf("middleware %s should be disabled with nil config", s.ID)
		}
	}
}

func TestGetMCPAddress_Adapter(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "explicit MCP address",
			cfg:  &config.Config{Admin: &config.Admin{MCPAddress: ":9091"}},
			want: ":9091",
		},
		{
			name: "derived from admin address",
			cfg:  &config.Config{Admin: &config.Admin{Address: ":9090"}},
			want: ":9091",
		},
		{
			name: "nil admin defaults to 8081",
			cfg:  &config.Config{},
			want: "127.0.0.1:8081",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMCPAddress(tt.cfg)
			if got != tt.want {
				t.Errorf("getMCPAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
