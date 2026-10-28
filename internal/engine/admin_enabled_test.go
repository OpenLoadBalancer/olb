package engine

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestAdminIsEnabled(t *testing.T) {
	var nilAdmin *config.Admin
	if !nilAdmin.IsEnabled() {
		t.Error("nil admin must default to enabled")
	}
	if !(&config.Admin{}).IsEnabled() {
		t.Error("omitted enabled key must default to enabled")
	}
	if !(&config.Admin{Enabled: boolPtr(true)}).IsEnabled() {
		t.Error("enabled: true must be enabled")
	}
	if (&config.Admin{Enabled: boolPtr(false)}).IsEnabled() {
		t.Error("enabled: false must be disabled")
	}
}

func TestEngineStart_AdminDisabledDoesNotListen(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	cfg := createTestConfig()
	cfg.Admin = &config.Admin{Enabled: boolPtr(false), Address: addr}
	e, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Shutdown(t.Context())

	time.Sleep(200 * time.Millisecond)
	if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		c.Close()
		t.Fatalf("admin server listening on %s although admin.enabled is false", addr)
	}
}

func TestMetricsEndpointConfig(t *testing.T) {
	// The Web UI answers unknown paths with its SPA shell (200), so judge by
	// the Prometheus content type rather than the status.
	served := func(m *config.Metrics, path string) bool {
		t.Helper()
		cfg := createTestConfig()
		cfg.Admin = &config.Admin{Address: "127.0.0.1:0"}
		cfg.Metrics = m
		e, err := New(cfg, "")
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		e.adminServer.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code == 200 && strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain")
	}
	if !served(&config.Metrics{Path: "/prom"}, "/prom") {
		t.Error("custom path not served")
	}
	if served(&config.Metrics{Path: "/prom"}, "/metrics") {
		t.Error("default path must be gone when a custom one is set")
	}
	if served(&config.Metrics{Enabled: boolPtr(false)}, "/metrics") {
		t.Error("endpoint served although disabled")
	}
	if !served(nil, "/metrics") {
		t.Error("default path not served")
	}
}
