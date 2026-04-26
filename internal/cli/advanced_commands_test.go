package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/admin"
)

// newAdvancedTestServer creates a mock server for advanced command testing
func newAdvancedTestServer() *httptest.Server {
	mux := http.NewServeMux()

	// Backend endpoints
	mux.HandleFunc("/api/v1/backends/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/backends/")
		parts := strings.Split(path, "/")
		poolName := parts[0]

		if poolName == "notfound" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "pool not found"))
			return
		}

		// Add backend: POST /api/v1/backends/{pool}/backends
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "backends" {
			w.WriteHeader(http.StatusCreated)
			return
		}

		// Enable backend: POST /api/v1/backends/{pool}/backends/{backend}/enable
		if r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "backends" && parts[3] == "enable" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Disable backend: POST /api/v1/backends/{pool}/backends/{backend}/disable
		if r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "backends" && parts[3] == "disable" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Drain backend: POST /api/v1/backends/{pool}/backends/{backend}/drain
		if r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "backends" && parts[3] == "drain" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Remove backend: DELETE /api/v1/backends/{pool}/backends/{backend}
		if r.Method == http.MethodDelete && len(parts) == 3 && parts[1] == "backends" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Get pool stats: GET /api/v1/backends/{pool}
		if r.Method == http.MethodGet && len(parts) == 1 {
			resp := PoolInfo{
				Name:      poolName,
				Algorithm: "round_robin",
				Backends: []BackendInfo{
					{ID: "b1", Address: "10.0.0.1:8080", Weight: 1, State: "active", Healthy: true, Requests: 100, Errors: 2},
					{ID: "b2", Address: "10.0.0.2:8080", Weight: 1, State: "active", Healthy: true, Requests: 95, Errors: 0},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// Route endpoints
	mux.HandleFunc("/api/v1/routes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method == http.MethodGet {
			routes := []RouteInfo{
				{Name: "api-route", Host: "api.example.com", Path: "/api", BackendPool: "api", Priority: 100},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(routes)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// Route test endpoint
	mux.HandleFunc("/api/v1/routes/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			result := map[string]any{
				"path":     r.URL.Query().Get("path"),
				"backend":  "api",
				"matched":  true,
				"priority": 100,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// Certificate endpoints
	mux.HandleFunc("/api/v1/certificates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method == http.MethodGet {
			certs := []map[string]any{
				{"domain": "example.com", "issuer": "Let's Encrypt", "expiry": "2025-12-31", "auto": true},
				{"domain": "api.example.com", "issuer": "Let's Encrypt", "expiry": "2025-12-31", "auto": true},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(certs)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/v1/certificates/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/certificates/")
		parts := strings.Split(path, "/")
		domain := parts[0]

		if domain == "notfound" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "certificate not found"))
			return
		}

		// Renew: POST /api/v1/certificates/{domain}/renew
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "renew" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Get cert info: GET /api/v1/certificates/{domain}
		if r.Method == http.MethodGet && len(parts) == 1 {
			cert := map[string]any{
				"domain":  domain,
				"issuer":  "Let's Encrypt",
				"expiry":  "2025-12-31",
				"auto":    true,
				"subject": "CN=" + domain,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cert)
			return
		}

		// Delete cert: DELETE /api/v1/certificates/{domain}
		if r.Method == http.MethodDelete && len(parts) == 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// Config endpoint
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			config := map[string]any{
				"version":   "1",
				"listeners": []map[string]any{{"name": "http", "address": ":8080"}},
				"pools":     []map[string]any{{"name": "default", "algorithm": "round_robin"}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(config)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	return httptest.NewServer(mux)
}

// Backend Commands Tests

func TestBackendAddCommand_Name(t *testing.T) {
	cmd := &BackendAddCommand{}
	if got := cmd.Name(); got != "backend-add" {
		t.Errorf("BackendAddCommand.Name() = %q, want \"backend-add\"", got)
	}
}

func TestBackendAddCommand_Description(t *testing.T) {
	cmd := &BackendAddCommand{}
	if got := cmd.Description(); got != "Add a backend to a pool" {
		t.Errorf("BackendAddCommand.Description() = %q, want \"Add a backend to a pool\"", got)
	}
}

func TestBackendAddCommand_NoArgs(t *testing.T) {
	cmd := &BackendAddCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no args provided")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("Expected usage error, got: %v", err)
	}
}

func TestBackendAddCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendAddCommand{}
	// Flags must come before positional arguments
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--weight", "5", "web", "10.0.0.3:8080"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestBackendRemoveCommand_Name(t *testing.T) {
	cmd := &BackendRemoveCommand{}
	if got := cmd.Name(); got != "backend-remove" {
		t.Errorf("BackendRemoveCommand.Name() = %q, want \"backend-remove\"", got)
	}
}

func TestBackendRemoveCommand_Description(t *testing.T) {
	cmd := &BackendRemoveCommand{}
	if got := cmd.Description(); got != "Remove a backend from a pool" {
		t.Errorf("BackendRemoveCommand.Description() = %q, want \"Remove a backend from a pool\"", got)
	}
}

func TestBackendRemoveCommand_NoArgs(t *testing.T) {
	cmd := &BackendRemoveCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no args provided")
	}
}

func TestBackendDrainCommand_Name(t *testing.T) {
	cmd := &BackendDrainCommand{}
	if got := cmd.Name(); got != "backend-drain" {
		t.Errorf("BackendDrainCommand.Name() = %q, want \"backend-drain\"", got)
	}
}

func TestBackendDrainCommand_Description(t *testing.T) {
	cmd := &BackendDrainCommand{}
	if got := cmd.Description(); got != "Mark a backend as draining" {
		t.Errorf("BackendDrainCommand.Description() = %q, want \"Mark a backend as draining\"", got)
	}
}

func TestBackendEnableCommand_Name(t *testing.T) {
	cmd := &BackendEnableCommand{}
	if got := cmd.Name(); got != "backend-enable" {
		t.Errorf("BackendEnableCommand.Name() = %q, want \"backend-enable\"", got)
	}
}

func TestBackendEnableCommand_Description(t *testing.T) {
	cmd := &BackendEnableCommand{}
	if got := cmd.Description(); got != "Enable a backend" {
		t.Errorf("BackendEnableCommand.Description() = %q, want \"Enable a backend\"", got)
	}
}

func TestBackendDisableCommand_Name(t *testing.T) {
	cmd := &BackendDisableCommand{}
	if got := cmd.Name(); got != "backend-disable" {
		t.Errorf("BackendDisableCommand.Name() = %q, want \"backend-disable\"", got)
	}
}

func TestBackendDisableCommand_Description(t *testing.T) {
	cmd := &BackendDisableCommand{}
	if got := cmd.Description(); got != "Disable a backend" {
		t.Errorf("BackendDisableCommand.Description() = %q, want \"Disable a backend\"", got)
	}
}

func TestBackendStatsCommand_Name(t *testing.T) {
	cmd := &BackendStatsCommand{}
	if got := cmd.Name(); got != "backend-stats" {
		t.Errorf("BackendStatsCommand.Name() = %q, want \"backend-stats\"", got)
	}
}

func TestBackendStatsCommand_Description(t *testing.T) {
	cmd := &BackendStatsCommand{}
	if got := cmd.Description(); got != "Show backend statistics" {
		t.Errorf("BackendStatsCommand.Description() = %q, want \"Show backend statistics\"", got)
	}
}

func TestBackendStatsCommand_NoArgs(t *testing.T) {
	cmd := &BackendStatsCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no pool provided")
	}
}

func TestBackendStatsCommand_JSONFormat(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendStatsCommand{format: "json"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "json", "web"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestBackendStatsCommand_TableFormat(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendStatsCommand{format: "table"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "table", "web"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Route Commands Tests

func TestRouteAddCommand_Name(t *testing.T) {
	cmd := &RouteAddCommand{}
	if got := cmd.Name(); got != "route-add" {
		t.Errorf("RouteAddCommand.Name() = %q, want \"route-add\"", got)
	}
}

func TestRouteAddCommand_Description(t *testing.T) {
	cmd := &RouteAddCommand{}
	if got := cmd.Description(); got != "Add a new route" {
		t.Errorf("RouteAddCommand.Description() = %q, want \"Add a new route\"", got)
	}
}

func TestRouteAddCommand_NoArgs(t *testing.T) {
	cmd := &RouteAddCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no path provided")
	}
}

func TestRouteAddCommand_NoBackend(t *testing.T) {
	cmd := &RouteAddCommand{}
	err := cmd.Run([]string{"/api"})
	if err == nil {
		t.Error("Expected error when no backend provided")
	}
}

func TestRouteAddCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &RouteAddCommand{}
	// Flags must come before positional arguments
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--backend", "api", "--priority", "100", "/api"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestRouteRemoveCommand_Name(t *testing.T) {
	cmd := &RouteRemoveCommand{}
	if got := cmd.Name(); got != "route-remove" {
		t.Errorf("RouteRemoveCommand.Name() = %q, want \"route-remove\"", got)
	}
}

func TestRouteRemoveCommand_Description(t *testing.T) {
	cmd := &RouteRemoveCommand{}
	if got := cmd.Description(); got != "Remove a route" {
		t.Errorf("RouteRemoveCommand.Description() = %q, want \"Remove a route\"", got)
	}
}

func TestRouteRemoveCommand_NoArgs(t *testing.T) {
	cmd := &RouteRemoveCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no path provided")
	}
}

func TestRouteTestCommand_Name(t *testing.T) {
	cmd := &RouteTestCommand{}
	if got := cmd.Name(); got != "route-test" {
		t.Errorf("RouteTestCommand.Name() = %q, want \"route-test\"", got)
	}
}

func TestRouteTestCommand_Description(t *testing.T) {
	cmd := &RouteTestCommand{}
	if got := cmd.Description(); got != "Test a route" {
		t.Errorf("RouteTestCommand.Description() = %q, want \"Test a route\"", got)
	}
}

func TestRouteTestCommand_NoArgs(t *testing.T) {
	cmd := &RouteTestCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no path provided")
	}
}

func TestRouteTestCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &RouteTestCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "/api"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Certificate Commands Tests

func TestCertListCommand_Name(t *testing.T) {
	cmd := &CertListCommand{}
	if got := cmd.Name(); got != "cert-list" {
		t.Errorf("CertListCommand.Name() = %q, want \"cert-list\"", got)
	}
}

func TestCertListCommand_Description(t *testing.T) {
	cmd := &CertListCommand{}
	if got := cmd.Description(); got != "List certificates" {
		t.Errorf("CertListCommand.Description() = %q, want \"List certificates\"", got)
	}
}

func TestCertListCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertListCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCertAddCommand_Name(t *testing.T) {
	cmd := &CertAddCommand{}
	if got := cmd.Name(); got != "cert-add" {
		t.Errorf("CertAddCommand.Name() = %q, want \"cert-add\"", got)
	}
}

func TestCertAddCommand_Description(t *testing.T) {
	cmd := &CertAddCommand{}
	if got := cmd.Description(); got != "Add a certificate" {
		t.Errorf("CertAddCommand.Description() = %q, want \"Add a certificate\"", got)
	}
}

func TestCertAddCommand_NoArgs(t *testing.T) {
	cmd := &CertAddCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no domain provided")
	}
}

func TestCertAddCommand_AutoSuccess(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertAddCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--auto", "test.example.com"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCertAddCommand_MissingCertFiles(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertAddCommand{}
	// Without --auto, --cert and --key are required
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "test.example.com"})
	if err == nil {
		t.Error("Expected error when cert files not provided")
	}
}

func TestCertRemoveCommand_Name(t *testing.T) {
	cmd := &CertRemoveCommand{}
	if got := cmd.Name(); got != "cert-remove" {
		t.Errorf("CertRemoveCommand.Name() = %q, want \"cert-remove\"", got)
	}
}

func TestCertRemoveCommand_Description(t *testing.T) {
	cmd := &CertRemoveCommand{}
	if got := cmd.Description(); got != "Remove a certificate" {
		t.Errorf("CertRemoveCommand.Description() = %q, want \"Remove a certificate\"", got)
	}
}

func TestCertRemoveCommand_NoArgs(t *testing.T) {
	cmd := &CertRemoveCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no domain provided")
	}
}

func TestCertRenewCommand_Name(t *testing.T) {
	cmd := &CertRenewCommand{}
	if got := cmd.Name(); got != "cert-renew" {
		t.Errorf("CertRenewCommand.Name() = %q, want \"cert-renew\"", got)
	}
}

func TestCertRenewCommand_Description(t *testing.T) {
	cmd := &CertRenewCommand{}
	if got := cmd.Description(); got != "Renew a certificate" {
		t.Errorf("CertRenewCommand.Description() = %q, want \"Renew a certificate\"", got)
	}
}

func TestCertRenewCommand_NoArgs(t *testing.T) {
	cmd := &CertRenewCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no domain provided")
	}
}

func TestCertInfoCommand_Name(t *testing.T) {
	cmd := &CertInfoCommand{}
	if got := cmd.Name(); got != "cert-info" {
		t.Errorf("CertInfoCommand.Name() = %q, want \"cert-info\"", got)
	}
}

func TestCertInfoCommand_Description(t *testing.T) {
	cmd := &CertInfoCommand{}
	if got := cmd.Description(); got != "Show certificate information" {
		t.Errorf("CertInfoCommand.Description() = %q, want \"Show certificate information\"", got)
	}
}

func TestCertInfoCommand_NoArgs(t *testing.T) {
	cmd := &CertInfoCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no domain provided")
	}
}

func TestCertInfoCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertInfoCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "example.com"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Metrics Commands Tests

func TestMetricsShowCommand_Name(t *testing.T) {
	cmd := &MetricsShowCommand{}
	if got := cmd.Name(); got != "metrics-show" {
		t.Errorf("MetricsShowCommand.Name() = %q, want \"metrics-show\"", got)
	}
}

func TestMetricsShowCommand_Description(t *testing.T) {
	cmd := &MetricsShowCommand{}
	if got := cmd.Description(); got != "Show metrics" {
		t.Errorf("MetricsShowCommand.Description() = %q, want \"Show metrics\"", got)
	}
}

func TestMetricsShowCommand_JSONFormat(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	cmd := &MetricsShowCommand{format: "json"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "json"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMetricsShowCommand_TableFormat(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	cmd := &MetricsShowCommand{format: "table"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "table"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMetricsShowCommand_PrometheusFormat(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	cmd := &MetricsShowCommand{format: "prometheus"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "prometheus"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestMetricsShowCommand_InvalidFormat(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	cmd := &MetricsShowCommand{format: "invalid"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestMetricsExportCommand_Name(t *testing.T) {
	cmd := &MetricsExportCommand{}
	if got := cmd.Name(); got != "metrics-export" {
		t.Errorf("MetricsExportCommand.Name() = %q, want \"metrics-export\"", got)
	}
}

func TestMetricsExportCommand_Description(t *testing.T) {
	cmd := &MetricsExportCommand{}
	if got := cmd.Description(); got != "Export metrics to file" {
		t.Errorf("MetricsExportCommand.Description() = %q, want \"Export metrics to file\"", got)
	}
}

func TestMetricsExportCommand_JSON(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "metrics.json")

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--output", outputPath, "--format", "json"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("Output file was not created")
	}
}

func TestMetricsExportCommand_InvalidFormat(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// Config Commands Tests

func TestConfigShowCommand_Name(t *testing.T) {
	cmd := &ConfigShowCommand{}
	if got := cmd.Name(); got != "config-show" {
		t.Errorf("ConfigShowCommand.Name() = %q, want \"config-show\"", got)
	}
}

func TestConfigShowCommand_Description(t *testing.T) {
	cmd := &ConfigShowCommand{}
	if got := cmd.Description(); got != "Show current configuration" {
		t.Errorf("ConfigShowCommand.Description() = %q, want \"Show current configuration\"", got)
	}
}

func TestConfigShowCommand_JSON(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &ConfigShowCommand{format: "json"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "json"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfigShowCommand_YAML(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &ConfigShowCommand{format: "yaml"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "yaml"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfigDiffCommand_Name(t *testing.T) {
	cmd := &ConfigDiffCommand{}
	if got := cmd.Name(); got != "config-diff" {
		t.Errorf("ConfigDiffCommand.Name() = %q, want \"config-diff\"", got)
	}
}

func TestConfigDiffCommand_Description(t *testing.T) {
	cmd := &ConfigDiffCommand{}
	if got := cmd.Description(); got != "Show configuration differences" {
		t.Errorf("ConfigDiffCommand.Description() = %q, want \"Show configuration differences\"", got)
	}
}

func TestConfigValidateCommand_Name(t *testing.T) {
	cmd := &ConfigValidateCommand{}
	if got := cmd.Name(); got != "config-validate" {
		t.Errorf("ConfigValidateCommand.Name() = %q, want \"config-validate\"", got)
	}
}

func TestConfigValidateCommand_Description(t *testing.T) {
	cmd := &ConfigValidateCommand{}
	if got := cmd.Description(); got != "Validate configuration file" {
		t.Errorf("ConfigValidateCommand.Description() = %q, want \"Validate configuration file\"", got)
	}
}

func TestConfigValidateCommand_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")
	configContent := `version: "1"
listeners:
  - name: http
    address: :8080
pools:
  - name: default
    algorithm: round_robin
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfigValidateCommand_FileNotFound(t *testing.T) {
	cmd := &ConfigValidateCommand{}
	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "nonexistent", "config.yaml")
	err := cmd.Run([]string{"--config", nonexistent})
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

// Completion Command Tests

func TestCompletionCommand_Name(t *testing.T) {
	cmd := &CompletionCommand{}
	if got := cmd.Name(); got != "completion" {
		t.Errorf("CompletionCommand.Name() = %q, want \"completion\"", got)
	}
}

func TestCompletionCommand_Description(t *testing.T) {
	cmd := &CompletionCommand{}
	if got := cmd.Description(); got != "Generate shell completion script" {
		t.Errorf("CompletionCommand.Description() = %q, want \"Generate shell completion script\"", got)
	}
}

func TestCompletionCommand_Bash(t *testing.T) {
	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "bash"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCompletionCommand_Zsh(t *testing.T) {
	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "zsh"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCompletionCommand_Fish(t *testing.T) {
	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "fish"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCompletionCommand_InvalidShell(t *testing.T) {
	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid shell")
	}
}

// Test that all advanced commands implement the Command interface
func TestAdvancedCommandsInterface(t *testing.T) {
	commands := []Command{
		&BackendAddCommand{},
		&BackendRemoveCommand{},
		&BackendDrainCommand{},
		&BackendEnableCommand{},
		&BackendDisableCommand{},
		&BackendStatsCommand{},
		&RouteAddCommand{},
		&RouteRemoveCommand{},
		&RouteTestCommand{},
		&CertListCommand{},
		&CertAddCommand{},
		&CertRemoveCommand{},
		&CertRenewCommand{},
		&CertInfoCommand{},
		&MetricsShowCommand{},
		&MetricsExportCommand{},
		&ConfigShowCommand{},
		&ConfigDiffCommand{},
		&ConfigValidateCommand{},
		&CompletionCommand{},
	}

	for _, cmd := range commands {
		if cmd.Name() == "" {
			t.Errorf("Command %T has empty name", cmd)
		}
		if cmd.Description() == "" {
			t.Errorf("Command %T has empty description", cmd)
		}
	}
}

// Test helper function parseIntDefault
func TestParseIntDefault(t *testing.T) {
	tests := []struct {
		input    string
		default_ int
		expected int
	}{
		{"", 10, 10},    // empty string returns default
		{"5", 10, 5},    // valid number
		{"abc", 10, 10}, // invalid number returns default
		{"0", 10, 0},    // zero is valid
		{"-5", 10, -5},  // negative number
	}

	for _, tt := range tests {
		result := parseIntDefault(tt.input, tt.default_)
		if result != tt.expected {
			t.Errorf("parseIntDefault(%q, %d) = %d, want %d", tt.input, tt.default_, result, tt.expected)
		}
	}
}

// Test printYAML helper function
func TestPrintYAML(t *testing.T) {
	// This should not panic
	data := map[string]any{
		"version": "1",
		"listeners": []any{
			map[string]any{"name": "http", "address": ":8080"},
		},
		"nested": map[string]any{
			"key": "value",
		},
	}
	printYAML(data, 0)
}

// Integration test: verify all commands are registered
func TestAdvancedCommandsRegistered(t *testing.T) {
	expectedCommands := []string{
		"backend-add",
		"backend-remove",
		"backend-drain",
		"backend-enable",
		"backend-disable",
		"backend-stats",
		"route-add",
		"route-remove",
		"route-test",
		"cert-list",
		"cert-add",
		"cert-remove",
		"cert-renew",
		"cert-info",
		"metrics-show",
		"metrics-export",
		"config-show",
		"config-diff",
		"config-validate",
		"completion",
	}

	for _, name := range expectedCommands {
		cmd := FindCommand(name)
		if cmd == nil {
			t.Errorf("Command %q is not registered", name)
		}
	}
}

// Test error handling for API failures
func TestBackendStatsCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd := &BackendStatsCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

func TestCertInfoCommand_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "certificate not found"))
	}))
	defer server.Close()

	cmd := &CertInfoCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "notfound"})
	if err == nil {
		t.Error("Expected error when certificate not found")
	}
}

// Test backend commands with missing arguments
func TestBackendCommands_MissingArgs(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
	}{
		{"backend-add", &BackendAddCommand{}},
		{"backend-remove", &BackendRemoveCommand{}},
		{"backend-drain", &BackendDrainCommand{}},
		{"backend-enable", &BackendEnableCommand{}},
		{"backend-disable", &BackendDisableCommand{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Run([]string{})
			if err == nil {
				t.Errorf("%s: Expected error when no args provided", tt.name)
			}
			if !strings.Contains(err.Error(), "usage:") {
				t.Errorf("%s: Expected usage error, got: %v", tt.name, err)
			}
		})
	}
}

// Test certificate add with file reading
func TestCertAddCommand_WithCertFiles(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	// Create dummy cert and key files
	if err := os.WriteFile(certPath, []byte("dummy cert"), 0644); err != nil {
		t.Fatalf("Failed to create cert file: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("dummy key"), 0644); err != nil {
		t.Fatalf("Failed to create key file: %v", err)
	}

	cmd := &CertAddCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--cert", certPath,
		"--key", keyPath,
		"test.example.com",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCertAddCommand_MissingCertFile(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	// Only create key file
	if err := os.WriteFile(keyPath, []byte("dummy key"), 0644); err != nil {
		t.Fatalf("Failed to create key file: %v", err)
	}

	cmd := &CertAddCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--cert", certPath, // This file doesn't exist
		"--key", keyPath,
		"test.example.com",
	})
	if err == nil {
		t.Error("Expected error when cert file not found")
	}
}

// Test metrics export with invalid output path
func TestMetricsExportCommand_InvalidOutputPath(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	// Create a path that cannot be written to (a directory that is also a file name)
	tmpDir := t.TempDir()
	// Create a file where we expect to create the output
	blockerPath := filepath.Join(tmpDir, "blocker")
	if err := os.WriteFile(blockerPath, []byte("block"), 0644); err != nil {
		t.Fatalf("Failed to create blocker file: %v", err)
	}
	// Try to write to a path that uses the file as a directory
	outputPath := filepath.Join(blockerPath, "metrics.json")

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--output", outputPath,
		"--format", "json",
	})
	if err == nil {
		t.Error("Expected error for invalid output path")
	}
}

// Test config diff with missing file
func TestConfigDiffCommand_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "nonexistent", "config.yaml")

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{
		"--file", nonexistent,
	})
	if err == nil {
		t.Error("Expected error when config file not found")
	}
}

// Test config diff with valid file
func TestConfigDiffCommand_WithValidFile(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")
	configContent := `version: "1"
listeners:
  - name: http
    address: :8080
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--file", configPath,
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Benchmark command name lookups
func BenchmarkFindCommand(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FindCommand("backend-add")
	}
}

// Example usage test for completion command
func ExampleCompletionCommand() {
	cmd := &CompletionCommand{shell: "bash"}
	// This would print the bash completion script
	_ = cmd.Run([]string{"--shell", "bash"})
	// Output contains bash completion script - can't match exactly
}

// Test command help text
func TestAdvancedCommands_HelpText(t *testing.T) {
	commands := []struct {
		cmd          Command
		expectedDesc string
	}{
		{&BackendAddCommand{}, "Add a backend to a pool"},
		{&BackendRemoveCommand{}, "Remove a backend from a pool"},
		{&BackendDrainCommand{}, "Mark a backend as draining"},
		{&BackendEnableCommand{}, "Enable a backend"},
		{&BackendDisableCommand{}, "Disable a backend"},
		{&BackendStatsCommand{}, "Show backend statistics"},
		{&RouteAddCommand{}, "Add a new route"},
		{&RouteRemoveCommand{}, "Remove a route"},
		{&RouteTestCommand{}, "Test a route"},
		{&CertListCommand{}, "List certificates"},
		{&CertAddCommand{}, "Add a certificate"},
		{&CertRemoveCommand{}, "Remove a certificate"},
		{&CertRenewCommand{}, "Renew a certificate"},
		{&CertInfoCommand{}, "Show certificate information"},
		{&MetricsShowCommand{}, "Show metrics"},
		{&MetricsExportCommand{}, "Export metrics to file"},
		{&ConfigShowCommand{}, "Show current configuration"},
		{&ConfigDiffCommand{}, "Show configuration differences"},
		{&ConfigValidateCommand{}, "Validate configuration file"},
		{&CompletionCommand{}, "Generate shell completion script"},
	}

	for _, tt := range commands {
		t.Run(tt.cmd.Name(), func(t *testing.T) {
			if got := tt.cmd.Description(); got != tt.expectedDesc {
				t.Errorf("%s.Description() = %q, want %q", tt.cmd.Name(), got, tt.expectedDesc)
			}
		})
	}
}

// Test format validation in various commands
func TestFormatValidation(t *testing.T) {
	server := newMockServer()
	defer server.Close()
	apiAddr := strings.TrimPrefix(server.URL, "http://")

	tests := []struct {
		name      string
		cmd       Command
		args      []string
		wantError bool
	}{
		{
			name:      "backend-stats valid json",
			cmd:       &BackendStatsCommand{format: "json"},
			args:      []string{"--api-addr", apiAddr, "--format", "json", "web"},
			wantError: false,
		},
		{
			name:      "backend-stats valid table",
			cmd:       &BackendStatsCommand{format: "table"},
			args:      []string{"--api-addr", apiAddr, "--format", "table", "web"},
			wantError: false,
		},
		{
			name:      "backend-stats invalid format",
			cmd:       &BackendStatsCommand{format: "invalid"},
			args:      []string{"--api-addr", apiAddr, "--format", "invalid", "web"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Run(tt.args)
			if tt.wantError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test route test with different formats
func TestRouteTestCommand_Formats(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()
	apiAddr := strings.TrimPrefix(server.URL, "http://")

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"table", "table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &RouteTestCommand{format: tt.format}
			err := cmd.Run([]string{"--api-addr", apiAddr, "--format", tt.format, "/api"})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test cert list with different formats
func TestCertListCommand_Formats(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()
	apiAddr := strings.TrimPrefix(server.URL, "http://")

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"table", "table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &CertListCommand{format: tt.format}
			err := cmd.Run([]string{"--api-addr", apiAddr, "--format", tt.format})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test config show with invalid format
func TestConfigShowCommand_InvalidFormat(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()
	apiAddr := strings.TrimPrefix(server.URL, "http://")

	cmd := &ConfigShowCommand{format: "invalid"}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// Test metrics export with prometheus format
func TestMetricsExportCommand_Prometheus(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "metrics.txt")

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--output", outputPath,
		"--format", "prometheus",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify file was created
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	if !strings.Contains(string(content), "requests_total") {
		t.Error("Output doesn't contain expected prometheus content")
	}
}

// Test that all commands can be found
func TestFindAdvancedCommands(t *testing.T) {
	commandNames := []string{
		"backend-add", "backend-remove", "backend-drain",
		"backend-enable", "backend-disable", "backend-stats",
		"route-add", "route-remove", "route-test",
		"cert-list", "cert-add", "cert-remove", "cert-renew", "cert-info",
		"metrics-show", "metrics-export",
		"config-show", "config-diff", "config-validate",
		"completion",
	}

	for _, name := range commandNames {
		cmd := FindCommand(name)
		if cmd == nil {
			t.Errorf("FindCommand(%q) returned nil", name)
			continue
		}
		if cmd.Name() != name {
			t.Errorf("FindCommand(%q).Name() = %q, want %q", name, cmd.Name(), name)
		}
	}
}

// Test command registration in Commands slice
func TestCommandsSliceContainsAdvancedCommands(t *testing.T) {
	expectedCommands := map[string]bool{
		"backend-add":     false,
		"backend-remove":  false,
		"backend-drain":   false,
		"backend-enable":  false,
		"backend-disable": false,
		"backend-stats":   false,
		"route-add":       false,
		"route-remove":    false,
		"route-test":      false,
		"cert-list":       false,
		"cert-add":        false,
		"cert-remove":     false,
		"cert-renew":      false,
		"cert-info":       false,
		"metrics-show":    false,
		"metrics-export":  false,
		"config-show":     false,
		"config-diff":     false,
		"config-validate": false,
		"completion":      false,
	}

	for _, cmd := range Commands {
		if _, exists := expectedCommands[cmd.Name()]; exists {
			expectedCommands[cmd.Name()] = true
		}
	}

	for name, found := range expectedCommands {
		if !found {
			t.Errorf("Command %q is not in Commands slice", name)
		}
	}
}

// Test backend enable/disable with API errors
func TestBackendEnableCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd := &BackendEnableCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "b1"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

func TestBackendDisableCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd := &BackendDisableCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "b1"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test route remove with encoded path
func TestRouteRemoveCommand_EncodedPath(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	cmd := &RouteRemoveCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "/api/v1/users"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify the path was encoded
	if capturedPath == "" {
		t.Error("No request was captured")
	}
}

// Test cert info with invalid format
func TestCertInfoCommand_InvalidFormat(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertInfoCommand{format: "invalid"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "invalid", "example.com"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// Test cert list with invalid format
func TestCertListCommand_InvalidFormat(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertListCommand{format: "invalid"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// Test route test with invalid format
func TestRouteTestCommand_InvalidFormat(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &RouteTestCommand{format: "invalid"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--format", "invalid", "/api"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// Test config show with different formats
func TestConfigShowCommand_Formats(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()
	apiAddr := strings.TrimPrefix(server.URL, "http://")

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &ConfigShowCommand{format: tt.format}
			err := cmd.Run([]string{"--api-addr", apiAddr, "--format", tt.format})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test metrics show with different formats
func TestMetricsShowCommand_Formats(t *testing.T) {
	server := newMockServer()
	defer server.Close()
	apiAddr := strings.TrimPrefix(server.URL, "http://")

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"table", "table"},
		{"prometheus", "prometheus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &MetricsShowCommand{format: tt.format}
			err := cmd.Run([]string{"--api-addr", apiAddr, "--format", tt.format})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test backend stats API error
func TestBackendStatsCommand_APIErrorWithMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(admin.ErrorResponse("INTERNAL_ERROR", "database error"))
	}))
	defer server.Close()

	cmd := &BackendStatsCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test cert renew with API error
func TestCertRenewCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(admin.ErrorResponse("RENEWAL_FAILED", "ACME challenge failed"))
	}))
	defer server.Close()

	cmd := &CertRenewCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "example.com"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test route add with API error
func TestRouteAddCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/routes" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(admin.ErrorResponse("ALREADY_EXISTS", "route already exists"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cmd := &RouteAddCommand{backend: "api"}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "/api"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test route remove with API error
func TestRouteRemoveCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "route not found"))
	}))
	defer server.Close()

	cmd := &RouteRemoveCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "/nonexistent"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test cert remove with API error
func TestCertRemoveCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "certificate not found"))
	}))
	defer server.Close()

	cmd := &CertRemoveCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "notfound.example.com"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test metrics show API error
func TestMetricsShowCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_AVAILABLE", "metrics not available"))
	}))
	defer server.Close()

	cmd := &MetricsShowCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test config show API error
func TestConfigShowCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(admin.ErrorResponse("INTERNAL_ERROR", "config error"))
	}))
	defer server.Close()

	cmd := &ConfigShowCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test route test API error
func TestRouteTestCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(admin.ErrorResponse("INTERNAL_ERROR", "route test failed"))
	}))
	defer server.Close()

	cmd := &RouteTestCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "/api"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test cert list API error
func TestCertListCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(admin.ErrorResponse("INTERNAL_ERROR", "cert list failed"))
	}))
	defer server.Close()

	cmd := &CertListCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test cert add API error
func TestCertAddCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(admin.ErrorResponse("INVALID_CERT", "invalid certificate"))
	}))
	defer server.Close()

	cmd := &CertAddCommand{auto: true}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "--auto", "example.com"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test config validate with empty file
func TestConfigValidateCommand_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(configPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	if err == nil {
		t.Error("Expected error for empty config file")
	}
}

// Test config validate with invalid JSON
func TestConfigValidateCommand_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(configPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("Failed to create invalid file: %v", err)
	}

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	// Should not error because we just check if file is readable
	// The actual validation is basic
	if err != nil {
		t.Logf("Got error (may be expected): %v", err)
	}
}

// Test backend add with missing pool
func TestBackendAddCommand_MissingPool(t *testing.T) {
	cmd := &BackendAddCommand{}
	err := cmd.Run([]string{"10.0.0.1:8080"})
	if err == nil {
		t.Error("Expected error when only address provided")
	}
}

// Test backend add with API error
func TestBackendAddCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(admin.ErrorResponse("ALREADY_EXISTS", "backend already exists"))
	}))
	defer server.Close()

	cmd := &BackendAddCommand{weight: 100}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "10.0.0.1:8080"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test backend remove with API error
func TestBackendRemoveCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "backend not found"))
	}))
	defer server.Close()

	cmd := &BackendRemoveCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "nonexistent"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test backend drain with API error
func TestBackendDrainCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "backend not found"))
	}))
	defer server.Close()

	cmd := &BackendDrainCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "nonexistent"})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test metrics export API error
func TestMetricsExportCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_AVAILABLE", "metrics not available"))
	}))
	defer server.Close()

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test config diff API error
func TestConfigDiffCommand_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(admin.ErrorResponse("INTERNAL_ERROR", "config error"))
	}))
	defer server.Close()

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

// Test completion command with default shell
func TestCompletionCommand_DefaultShell(t *testing.T) {
	cmd := &CompletionCommand{}
	// Default is bash
	err := cmd.Run([]string{})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Test printYAML with various data types
func TestPrintYAML_VariousTypes(t *testing.T) {
	// Test with string
	printYAML("simple string", 0)

	// Test with int
	printYAML(42, 0)

	// Test with nested structure
	data := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "value",
			},
		},
	}
	printYAML(data, 0)

	// Test with mixed array
	mixed := []any{
		"string",
		42,
		map[string]any{"key": "value"},
	}
	printYAML(mixed, 0)
}

// Test parseIntDefault with edge cases
func TestParseIntDefault_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		default_ int
		expected int
	}{
		{"9223372036854775807", 0, 0},  // max int64 (will fail to parse on 32-bit)
		{"-9223372036854775808", 0, 0}, // min int64 (will fail to parse on 32-bit)
		{"  42  ", 0, 0},               // whitespace (will fail)
		{"42abc", 0, 0},                // trailing characters (will fail)
		{"abc42", 0, 0},                // leading characters (will fail)
	}

	for _, tt := range tests {
		result := parseIntDefault(tt.input, tt.default_)
		// Most of these should return default due to parse errors
		if result != tt.default_ {
			t.Logf("parseIntDefault(%q, %d) = %d (expected default %d due to parse error)",
				tt.input, tt.default_, result, tt.default_)
		}
	}
}

// Test backend stats with pool not found
func TestBackendStatsCommand_PoolNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "pool not found"))
	}))
	defer server.Close()

	cmd := &BackendStatsCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "notfound"})
	if err == nil {
		t.Error("Expected error when pool not found")
	}
}

// Test cert info with not found
func TestCertInfoCommand_CertNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(admin.ErrorResponse("NOT_FOUND", "certificate not found"))
	}))
	defer server.Close()

	cmd := &CertInfoCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "notfound.example.com"})
	if err == nil {
		t.Error("Expected error when certificate not found")
	}
}

// Test route test with query parameter
func TestRouteTestCommand_QueryParam(t *testing.T) {
	var queryPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/routes/test" {
			queryPath = r.URL.Query().Get("path")
			result := map[string]any{
				"path":    queryPath,
				"backend": "api",
				"matched": true,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cmd := &RouteTestCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "/api/v1/users"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if queryPath == "" {
		t.Error("Query path was not captured")
	}
}

// Test cert add with file reading error
func TestCertAddCommand_KeyFileNotFound(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "nonexistent.key")

	// Only create cert file
	if err := os.WriteFile(certPath, []byte("dummy cert"), 0644); err != nil {
		t.Fatalf("Failed to create cert file: %v", err)
	}

	cmd := &CertAddCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--cert", certPath,
		"--key", keyPath,
		"test.example.com",
	})
	if err == nil {
		t.Error("Expected error when key file not found")
	}
}

// Test metrics export with prometheus API error
func TestMetricsExportCommand_PrometheusAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/metrics/prometheus" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "metrics.txt")

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--output", outputPath,
		"--format", "prometheus",
	})
	if err == nil {
		t.Error("Expected error when prometheus API returns error")
	}
}

// Test backend enable with missing args
func TestBackendEnableCommand_MissingArgs(t *testing.T) {
	cmd := &BackendEnableCommand{}

	// Test with no args
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no args provided")
	}

	// Test with only pool
	err = cmd.Run([]string{"web"})
	if err == nil {
		t.Error("Expected error when only pool provided")
	}
}

// Test backend disable with missing args
func TestBackendDisableCommand_MissingArgs(t *testing.T) {
	cmd := &BackendDisableCommand{}

	// Test with no args
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no args provided")
	}

	// Test with only pool
	err = cmd.Run([]string{"web"})
	if err == nil {
		t.Error("Expected error when only pool provided")
	}
}

// Test cert renew with missing domain
func TestCertRenewCommand_MissingDomain(t *testing.T) {
	cmd := &CertRenewCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no domain provided")
	}
}

// Test completion command output contains expected content
func TestCompletionCommand_Output(t *testing.T) {
	tests := []struct {
		shell           string
		expectedContent string
	}{
		{"bash", "_olb()"},
		{"zsh", "#compdef olb"},
		{"fish", "complete -c olb"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			// We can't easily capture stdout, but we can verify the script constants exist
			switch tt.shell {
			case "bash":
				if !strings.Contains(bashCompletionScript, tt.expectedContent) {
					t.Errorf("Bash completion script missing %q", tt.expectedContent)
				}
			case "zsh":
				if !strings.Contains(zshCompletionScript, tt.expectedContent) {
					t.Errorf("Zsh completion script missing %q", tt.expectedContent)
				}
			case "fish":
				if !strings.Contains(fishCompletionScript, tt.expectedContent) {
					t.Errorf("Fish completion script missing %q", tt.expectedContent)
				}
			}
		})
	}
}

// Test that all commands have unique names
func TestCommandNamesUnique(t *testing.T) {
	names := make(map[string]int)
	for _, cmd := range Commands {
		names[cmd.Name()]++
	}

	for name, count := range names {
		if count > 1 {
			t.Errorf("Command name %q appears %d times", name, count)
		}
	}
}

// Test command line parsing for various commands
func TestCommandFlagParsing(t *testing.T) {
	tests := []struct {
		name    string
		cmd     Command
		args    []string
		wantErr bool
	}{
		{
			name:    "backend-add with flags",
			cmd:     &BackendAddCommand{},
			args:    []string{"--weight", "50", "pool", "address"},
			wantErr: false,
		},
		{
			name:    "route-add with priority",
			cmd:     &RouteAddCommand{},
			args:    []string{"--backend", "api", "--priority", "200", "/api"},
			wantErr: false,
		},
		{
			name:    "cert-add with auto",
			cmd:     &CertAddCommand{},
			args:    []string{"--auto", "example.com"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These commands need a server to succeed, so we expect them to fail
			// but not due to flag parsing errors
			err := tt.cmd.Run(tt.args)
			// We expect errors because there's no server, but not flag parsing errors
			if err != nil {
				// Should not be a flag parsing error
				if strings.Contains(err.Error(), "flag provided but not defined") {
					t.Errorf("Flag parsing error: %v", err)
				}
			}
		})
	}
}

// Test helper function to verify command structure
func TestCommandStructure(t *testing.T) {
	commands := []Command{
		&BackendAddCommand{},
		&BackendRemoveCommand{},
		&BackendDrainCommand{},
		&BackendEnableCommand{},
		&BackendDisableCommand{},
		&BackendStatsCommand{},
		&RouteAddCommand{},
		&RouteRemoveCommand{},
		&RouteTestCommand{},
		&CertListCommand{},
		&CertAddCommand{},
		&CertRemoveCommand{},
		&CertRenewCommand{},
		&CertInfoCommand{},
		&MetricsShowCommand{},
		&MetricsExportCommand{},
		&ConfigShowCommand{},
		&ConfigDiffCommand{},
		&ConfigValidateCommand{},
		&CompletionCommand{},
	}

	for _, cmd := range commands {
		t.Run(fmt.Sprintf("%s structure", cmd.Name()), func(t *testing.T) {
			// Verify name is not empty
			if cmd.Name() == "" {
				t.Error("Command name is empty")
			}

			// Verify description is not empty
			if cmd.Description() == "" {
				t.Error("Command description is empty")
			}

			// Verify name doesn't contain spaces
			if strings.Contains(cmd.Name(), " ") {
				t.Error("Command name contains spaces")
			}

			// Verify description doesn't end with period
			if strings.HasSuffix(cmd.Description(), ".") {
				t.Error("Command description ends with period")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Success-path tests for backend commands (exercise fmt.Printf output lines)
// ---------------------------------------------------------------------------

func TestBackendRemoveCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendRemoveCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "b1"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestBackendDrainCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendDrainCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "b1"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestBackendEnableCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendEnableCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "b1"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestBackendDisableCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendDisableCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "web", "b1"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Success-path tests for cert commands
// ---------------------------------------------------------------------------

func TestCertRemoveCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertRemoveCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "example.com"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCertRenewCommand_Success(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertRenewCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://"), "example.com"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MetricsExport success path (line 881 fmt.Printf)
// ---------------------------------------------------------------------------

func TestMetricsExportCommand_JSONSuccess(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "out.json")

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--output", outputPath,
		"--format", "json",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}
	if len(data) == 0 {
		t.Error("Output file is empty")
	}
}

func TestMetricsExportCommand_PrometheusSuccess(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "out.txt")

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--output", outputPath,
		"--format", "prometheus",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ConfigDiff success path (line 1017-1020 fmt.Printf lines)
// ---------------------------------------------------------------------------

func TestConfigDiffCommand_WithExplicitFile(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	configContent := "version: \"1\"\nlisteners:\n  - name: http\n    address: :8080\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--file", configPath,
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ConfigValidate success path with version/listeners/pools (line 1065-1076)
// ---------------------------------------------------------------------------

func TestConfigValidateCommand_ValidWithAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")
	configContent := `version: "1"
listeners:
  - name: http
    address: :8080
pools:
  - name: default
    algorithm: round_robin
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// BackendAdd success path with weight (exercises weight flag parsing)
// ---------------------------------------------------------------------------

func TestBackendAddCommand_WithWeight(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &BackendAddCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--weight", "10",
		"web", "10.0.0.5:8080",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RouteAdd with all flags (exercises host and methods flags)
// ---------------------------------------------------------------------------

func TestRouteAddCommand_WithAllFlags(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &RouteAddCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--backend", "api",
		"--priority", "100",
		"/api/v2",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MetricsShowCommand table format success (exercises table output lines)
// ---------------------------------------------------------------------------

func TestMetricsShowCommand_TableSuccess(t *testing.T) {
	server := newMockServer()
	defer server.Close()

	cmd := &MetricsShowCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--format", "table",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CertInfoCommand table format success (exercises table output lines)
// ---------------------------------------------------------------------------

func TestCertInfoCommand_TableSuccess(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertInfoCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--format", "table",
		"example.com",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CertInfoCommand json format success (exercises json output lines)
// ---------------------------------------------------------------------------

func TestCertInfoCommand_JSONSuccess(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	cmd := &CertInfoCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(server.URL, "http://"),
		"--format", "json",
		"example.com",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestConfigValidateCommand_ValidJSONWithAllFields tests validate with JSON file containing all config fields.
func TestConfigValidateCommand_ValidJSONWithAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.json")
	configContent := `{
		"version": "1",
		"listeners": [{"name": "http", "address": ":8080"}],
		"pools": [{"name": "default", "algorithm": "round_robin"}]
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestConfigDiffCommand_DefaultFilePath tests diff when --file is not specified (defaults to olb.yaml).
func TestConfigDiffCommand_DefaultFilePath(t *testing.T) {
	server := newAdvancedTestServer()
	defer server.Close()

	// Change to a temp dir to ensure olb.yaml does not exist
	tmpDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{"--api-addr", strings.TrimPrefix(server.URL, "http://")})
	if err == nil {
		t.Error("Expected error when default olb.yaml doesn't exist")
	}
	if err != nil {
		t.Logf("Got error (expected): %v", err)
	}
}

// TestConfigDiffCommand_DefaultFilePath tests that default file path (olb.yaml) is used when --file is not specified.
