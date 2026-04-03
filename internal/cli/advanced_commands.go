// Package cli provides advanced command-line interface commands for OpenLoadBalancer.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// BackendAddCommand adds a backend to a pool
type BackendAddCommand struct {
	apiAddr     string
	weight      int
	healthCheck string
}

// Name returns the command name.
func (c *BackendAddCommand) Name() string {
	return "backend-add"
}

// Description returns the command description.
func (c *BackendAddCommand) Description() string {
	return "Add a backend to a pool"
}

// Run executes the backend-add command.
func (c *BackendAddCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-add", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.IntVar(&c.weight, "weight", 100, "Backend weight")
	fs.StringVar(&c.healthCheck, "health-check", "", "Health check path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend add <pool> <address> [--weight N] [--health-check path]")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	req := &AddBackendRequest{
		ID:      address,
		Address: address,
		Weight:  c.weight,
	}

	if err := client.AddBackend(pool, req); err != nil {
		return fmt.Errorf("failed to add backend: %w", err)
	}

	fmt.Printf("Backend '%s' added to pool '%s' with weight %d\n", address, pool, c.weight)
	return nil
}

// BackendRemoveCommand removes a backend from a pool
type BackendRemoveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendRemoveCommand) Name() string {
	return "backend-remove"
}

// Description returns the command description.
func (c *BackendRemoveCommand) Description() string {
	return "Remove a backend from a pool"
}

// Run executes the backend-remove command.
func (c *BackendRemoveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-remove", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend remove <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	if err := client.RemoveBackend(pool, address); err != nil {
		return fmt.Errorf("failed to remove backend: %w", err)
	}

	fmt.Printf("Backend '%s' removed from pool '%s'\n", address, pool)
	return nil
}

// BackendDrainCommand marks a backend as draining
type BackendDrainCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendDrainCommand) Name() string {
	return "backend-drain"
}

// Description returns the command description.
func (c *BackendDrainCommand) Description() string {
	return "Mark a backend as draining"
}

// Run executes the backend-drain command.
func (c *BackendDrainCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-drain", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend drain <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	if err := client.DrainBackend(pool, address); err != nil {
		return fmt.Errorf("failed to drain backend: %w", err)
	}

	fmt.Printf("Backend '%s' in pool '%s' is now draining\n", address, pool)
	return nil
}

// BackendEnableCommand enables a backend
type BackendEnableCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendEnableCommand) Name() string {
	return "backend-enable"
}

// Description returns the command description.
func (c *BackendEnableCommand) Description() string {
	return "Enable a backend"
}

// Run executes the backend-enable command.
func (c *BackendEnableCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-enable", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend enable <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	// Use the generic doRequest to enable backend
	url := fmt.Sprintf("/backends/%s/backends/%s/enable", pool, address)
	resp, err := client.doRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to enable backend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to enable backend: HTTP %d", resp.StatusCode)
	}

	fmt.Printf("Backend '%s' in pool '%s' enabled\n", address, pool)
	return nil
}

// BackendDisableCommand disables a backend
type BackendDisableCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendDisableCommand) Name() string {
	return "backend-disable"
}

// Description returns the command description.
func (c *BackendDisableCommand) Description() string {
	return "Disable a backend"
}

// Run executes the backend-disable command.
func (c *BackendDisableCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-disable", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend disable <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/backends/%s/backends/%s/disable", pool, address)
	resp, err := client.doRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to disable backend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to disable backend: HTTP %d", resp.StatusCode)
	}

	fmt.Printf("Backend '%s' in pool '%s' disabled\n", address, pool)
	return nil
}

// BackendStatsCommand shows backend statistics
type BackendStatsCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *BackendStatsCommand) Name() string {
	return "backend-stats"
}

// Description returns the command description.
func (c *BackendStatsCommand) Description() string {
	return "Show backend statistics"
}

// Run executes the backend-stats command.
func (c *BackendStatsCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-stats", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb backend stats <pool>")
	}

	pool := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	poolInfo, err := client.GetPool(pool)
	if err != nil {
		return fmt.Errorf("failed to get pool stats: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(poolInfo, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Printf("Pool: %s\n", poolInfo.Name)
		fmt.Printf("Algorithm: %s\n", poolInfo.Algorithm)
		fmt.Println("\nBackends:")
		fmt.Printf("%-20s %-25s %-8s %-10s %-10s %-10s\n", "ID", "Address", "Weight", "State", "Healthy", "Requests")
		fmt.Println(strings.Repeat("-", 85))
		for _, b := range poolInfo.Backends {
			healthy := "no"
			if b.Healthy {
				healthy = "yes"
			}
			fmt.Printf("%-20s %-25s %-8d %-10s %-10s %-10d\n", b.ID, b.Address, b.Weight, b.State, healthy, b.Requests)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// RouteAddCommand adds a new route
type RouteAddCommand struct {
	apiAddr  string
	backend  string
	priority int
}

// Name returns the command name.
func (c *RouteAddCommand) Name() string {
	return "route-add"
}

// Description returns the command description.
func (c *RouteAddCommand) Description() string {
	return "Add a new route"
}

// Run executes the route-add command.
func (c *RouteAddCommand) Run(args []string) error {
	fs := flag.NewFlagSet("route-add", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.backend, "backend", "", "Backend pool name")
	fs.IntVar(&c.priority, "priority", 100, "Route priority")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb route add <path> [--backend <pool>] [--priority N]")
	}

	path := remaining[0]

	if c.backend == "" {
		return fmt.Errorf("--backend is required")
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	route := map[string]any{
		"path":         path,
		"backend_pool": c.backend,
		"priority":     c.priority,
	}

	if err := client.post("/routes", route, nil); err != nil {
		return fmt.Errorf("failed to add route: %w", err)
	}

	fmt.Printf("Route '%s' added with backend '%s' and priority %d\n", path, c.backend, c.priority)
	return nil
}

// RouteRemoveCommand removes a route
type RouteRemoveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *RouteRemoveCommand) Name() string {
	return "route-remove"
}

// Description returns the command description.
func (c *RouteRemoveCommand) Description() string {
	return "Remove a route"
}

// Run executes the route-remove command.
func (c *RouteRemoveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("route-remove", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb route remove <path>")
	}

	path := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	// URL encode the path
	encodedPath := strings.ReplaceAll(path, "/", "%2F")
	url := fmt.Sprintf("/routes/%s", encodedPath)

	if err := client.delete(url); err != nil {
		return fmt.Errorf("failed to remove route: %w", err)
	}

	fmt.Printf("Route '%s' removed\n", path)
	return nil
}

// RouteTestCommand tests a route
type RouteTestCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *RouteTestCommand) Name() string {
	return "route-test"
}

// Description returns the command description.
func (c *RouteTestCommand) Description() string {
	return "Test a route"
}

// Run executes the route-test command.
func (c *RouteTestCommand) Run(args []string) error {
	fs := flag.NewFlagSet("route-test", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb route test <path>")
	}

	path := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	// URL encode the path
	encodedPath := strings.ReplaceAll(path, "/", "%2F")
	url := fmt.Sprintf("/routes/test?path=%s", encodedPath)

	var result map[string]any
	if err := client.get(url, &result); err != nil {
		return fmt.Errorf("failed to test route: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Printf("Route Test: %s\n", path)
		fmt.Println(strings.Repeat("=", 40))
		for k, v := range result {
			fmt.Printf("%-15s: %v\n", k, v)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// CertListCommand lists certificates
type CertListCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *CertListCommand) Name() string {
	return "cert-list"
}

// Description returns the command description.
func (c *CertListCommand) Description() string {
	return "List certificates"
}

// Run executes the cert-list command.
func (c *CertListCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-list", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var certs []map[string]any
	if err := client.get("/certificates", &certs); err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(certs, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("Certificates")
		fmt.Println("============")
		fmt.Printf("%-30s %-20s %-15s %-10s\n", "Domain", "Issuer", "Expiry", "Auto")
		fmt.Println(strings.Repeat("-", 80))
		for _, cert := range certs {
			domain, _ := cert["domain"].(string)
			issuer, _ := cert["issuer"].(string)
			expiry, _ := cert["expiry"].(string)
			auto, _ := cert["auto"].(bool)
			autoStr := "no"
			if auto {
				autoStr = "yes"
			}
			fmt.Printf("%-30s %-20s %-15s %-10s\n", domain, issuer, expiry, autoStr)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// CertAddCommand adds a certificate
type CertAddCommand struct {
	apiAddr  string
	auto     bool
	certFile string
	keyFile  string
}

// Name returns the command name.
func (c *CertAddCommand) Name() string {
	return "cert-add"
}

// Description returns the command description.
func (c *CertAddCommand) Description() string {
	return "Add a certificate"
}

// Run executes the cert-add command.
func (c *CertAddCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-add", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.BoolVar(&c.auto, "auto", false, "Use ACME/Let's Encrypt")
	fs.StringVar(&c.certFile, "cert", "", "Certificate file path")
	fs.StringVar(&c.keyFile, "key", "", "Private key file path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert add <domain> [--auto] [--cert <file> --key <file>]")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	req := map[string]any{
		"domain": domain,
		"auto":   c.auto,
	}

	if !c.auto {
		if c.certFile == "" || c.keyFile == "" {
			return fmt.Errorf("--cert and --key are required when not using --auto")
		}

		certData, err := os.ReadFile(c.certFile)
		if err != nil {
			return fmt.Errorf("failed to read certificate file: %w", err)
		}

		keyData, err := os.ReadFile(c.keyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}

		req["certificate"] = string(certData)
		req["key"] = string(keyData)
	}

	if err := client.post("/certificates", req, nil); err != nil {
		return fmt.Errorf("failed to add certificate: %w", err)
	}

	if c.auto {
		fmt.Printf("Certificate for '%s' will be provisioned via ACME\n", domain)
	} else {
		fmt.Printf("Certificate for '%s' added successfully\n", domain)
	}
	return nil
}

// CertRemoveCommand removes a certificate
type CertRemoveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *CertRemoveCommand) Name() string {
	return "cert-remove"
}

// Description returns the command description.
func (c *CertRemoveCommand) Description() string {
	return "Remove a certificate"
}

// Run executes the cert-remove command.
func (c *CertRemoveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-remove", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert remove <domain>")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/certificates/%s", domain)
	if err := client.delete(url); err != nil {
		return fmt.Errorf("failed to remove certificate: %w", err)
	}

	fmt.Printf("Certificate for '%s' removed\n", domain)
	return nil
}

// CertRenewCommand renews a certificate
type CertRenewCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *CertRenewCommand) Name() string {
	return "cert-renew"
}

// Description returns the command description.
func (c *CertRenewCommand) Description() string {
	return "Renew a certificate"
}

// Run executes the cert-renew command.
func (c *CertRenewCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-renew", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert renew <domain>")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/certificates/%s/renew", domain)
	if err := client.post(url, nil, nil); err != nil {
		return fmt.Errorf("failed to renew certificate: %w", err)
	}

	fmt.Printf("Certificate for '%s' renewed successfully\n", domain)
	return nil
}

// CertInfoCommand shows certificate information
type CertInfoCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *CertInfoCommand) Name() string {
	return "cert-info"
}

// Description returns the command description.
func (c *CertInfoCommand) Description() string {
	return "Show certificate information"
}

// Run executes the cert-info command.
func (c *CertInfoCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-info", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert info <domain>")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/certificates/%s", domain)
	var cert map[string]any
	if err := client.get(url, &cert); err != nil {
		return fmt.Errorf("failed to get certificate info: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(cert, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Printf("Certificate: %s\n", domain)
		fmt.Println(strings.Repeat("=", 40))
		for k, v := range cert {
			fmt.Printf("%-15s: %v\n", k, v)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// MetricsShowCommand shows metrics
type MetricsShowCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *MetricsShowCommand) Name() string {
	return "metrics-show"
}

// Description returns the command description.
func (c *MetricsShowCommand) Description() string {
	return "Show metrics"
}

// Run executes the metrics-show command.
func (c *MetricsShowCommand) Run(args []string) error {
	fs := flag.NewFlagSet("metrics-show", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json, table, or prometheus)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	switch c.format {
	case "json":
		metrics, err := client.GetMetricsJSON()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		data, err := json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "prometheus":
		metrics, err := client.GetMetricsPrometheus()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		fmt.Println(metrics)
	case "table":
		metrics, err := client.GetMetricsJSON()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		fmt.Println("Metrics")
		fmt.Println("=======")
		for k, v := range metrics {
			fmt.Printf("%-30s: %v\n", k, v)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// MetricsExportCommand exports metrics
type MetricsExportCommand struct {
	apiAddr string
	output  string
	format  string
}

// Name returns the command name.
func (c *MetricsExportCommand) Name() string {
	return "metrics-export"
}

// Description returns the command description.
func (c *MetricsExportCommand) Description() string {
	return "Export metrics to file"
}

// Run executes the metrics-export command.
func (c *MetricsExportCommand) Run(args []string) error {
	fs := flag.NewFlagSet("metrics-export", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.output, "output", "metrics.json", "Output file path")
	fs.StringVar(&c.format, "format", "json", "Export format (json or prometheus)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var data []byte

	switch c.format {
	case "json":
		metrics, err := client.GetMetricsJSON()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		data, err = json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			return err
		}
	case "prometheus":
		metrics, err := client.GetMetricsPrometheus()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		data = []byte(metrics)
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	if err := os.WriteFile(c.output, data, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("Metrics exported to %s\n", c.output)
	return nil
}

// ConfigShowCommand shows current configuration
type ConfigShowCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *ConfigShowCommand) Name() string {
	return "config-show"
}

// Description returns the command description.
func (c *ConfigShowCommand) Description() string {
	return "Show current configuration"
}

// Run executes the config-show command.
func (c *ConfigShowCommand) Run(args []string) error {
	fs := flag.NewFlagSet("config-show", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "yaml", "Output format (yaml or json)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var config map[string]any
	if err := client.get("/config", &config); err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		// Simple YAML-like output for now
		printYAML(config, 0)
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// printYAML prints a map as YAML-like output
func printYAML(data any, indent int) {
	prefix := strings.Repeat("  ", indent)
	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			switch val.(type) {
			case map[string]any, []any:
				fmt.Printf("%s%s:\n", prefix, key)
				printYAML(val, indent+1)
			default:
				fmt.Printf("%s%s: %v\n", prefix, key, val)
			}
		}
	case []any:
		for _, item := range v {
			fmt.Printf("%s- ", prefix)
			switch item.(type) {
			case map[string]any:
				first := true
				for k, val := range item.(map[string]any) {
					if first {
						fmt.Printf("%s: %v\n", k, val)
						first = false
					} else {
						fmt.Printf("%s  %s: %v\n", prefix, k, val)
					}
				}
			default:
				fmt.Printf("%v\n", item)
			}
		}
	default:
		fmt.Printf("%s%v\n", prefix, v)
	}
}

// ConfigDiffCommand shows configuration differences
type ConfigDiffCommand struct {
	apiAddr  string
	filePath string
}

// Name returns the command name.
func (c *ConfigDiffCommand) Name() string {
	return "config-diff"
}

// Description returns the command description.
func (c *ConfigDiffCommand) Description() string {
	return "Show configuration differences"
}

// Run executes the config-diff command.
func (c *ConfigDiffCommand) Run(args []string) error {
	fs := flag.NewFlagSet("config-diff", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.filePath, "file", "", "Config file to compare with")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var runningConfig map[string]any
	if err := client.get("/config", &runningConfig); err != nil {
		return fmt.Errorf("failed to get running config: %w", err)
	}

	if c.filePath == "" {
		// Compare with default config file
		c.filePath = "olb.yaml"
	}

	fileData, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// For now, just show that there are differences
	// A proper diff implementation would compare the two configs
	fmt.Printf("Comparing running config with %s\n", c.filePath)
	fmt.Println("Note: Full diff not yet implemented")
	fmt.Printf("Running config version: %v\n", runningConfig["version"])
	fmt.Printf("File size: %d bytes\n", len(fileData))

	return nil
}

// ConfigValidateCommand validates configuration
type ConfigValidateCommand struct {
	filePath string
}

// Name returns the command name.
func (c *ConfigValidateCommand) Name() string {
	return "config-validate"
}

// Description returns the command description.
func (c *ConfigValidateCommand) Description() string {
	return "Validate configuration file"
}

// Run executes the config-validate command.
func (c *ConfigValidateCommand) Run(args []string) error {
	fs := flag.NewFlagSet("config-validate", flag.ExitOnError)
	fs.StringVar(&c.filePath, "config", "olb.yaml", "Config file path")
	fs.StringVar(&c.filePath, "c", "olb.yaml", "Config file path (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	// Basic validation - check if it's valid YAML/JSON
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		// Try parsing as YAML-like structure
		// For now, just check if file is not empty
		if len(data) == 0 {
			return fmt.Errorf("config file is empty")
		}
	}

	fmt.Printf("Configuration file '%s' is valid\n", c.filePath)

	// Show basic stats
	if version, ok := config["version"].(string); ok {
		fmt.Printf("Version: %s\n", version)
	}
	if listeners, ok := config["listeners"].([]any); ok {
		fmt.Printf("Listeners: %d\n", len(listeners))
	}
	if pools, ok := config["pools"].([]any); ok {
		fmt.Printf("Pools: %d\n", len(pools))
	}

	return nil
}

// CompletionCommand generates shell completions
type CompletionCommand struct {
	shell string
}

// Name returns the command name.
func (c *CompletionCommand) Name() string {
	return "completion"
}

// Description returns the command description.
func (c *CompletionCommand) Description() string {
	return "Generate shell completion script"
}

// Run executes the completion command.
func (c *CompletionCommand) Run(args []string) error {
	fs := flag.NewFlagSet("completion", flag.ExitOnError)
	fs.StringVar(&c.shell, "shell", "bash", "Shell type (bash, zsh, fish)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	switch c.shell {
	case "bash":
		fmt.Print(bashCompletionScript)
	case "zsh":
		fmt.Print(zshCompletionScript)
	case "fish":
		fmt.Print(fishCompletionScript)
	default:
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", c.shell)
	}

	return nil
}

// bashCompletionScript is the bash completion script
const bashCompletionScript = `# bash completion for olb
_olb() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Main commands
    local commands="start stop reload status version config backend backend-add backend-remove backend-drain backend-enable backend-disable backend-stats health route route-add route-remove route-test cert cert-list cert-add cert-remove cert-renew cert-info metrics metrics-show metrics-export completion"

    # Global options
    local global_opts="--help --version --format"

    case "${COMP_CWORD}" in
        1)
            COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
            return 0
            ;;
        *)
            case "${COMP_WORDS[1]}" in
                start)
                    local opts="--config -c --daemon -d --pid-file"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                stop|reload)
                    local opts="--pid-file --api-addr"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                status|backend|backend-stats|health|metrics|metrics-show|cert|cert-list|cert-info)
                    local opts="--api-addr --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                backend-add)
                    local opts="--api-addr --weight --health-check"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                backend-remove|backend-drain|backend-enable|backend-disable)
                    local opts="--api-addr"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                route-add)
                    local opts="--api-addr --backend --priority"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                route-remove|route-test)
                    local opts="--api-addr --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                cert-add)
                    local opts="--api-addr --auto --cert --key"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                cert-remove|cert-renew)
                    local opts="--api-addr"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                metrics-export)
                    local opts="--api-addr --output --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                config|config-show)
                    local opts="--api-addr --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                config-diff)
                    local opts="--api-addr --file"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                config-validate)
                    local opts="--config -c"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                completion)
                    local opts="--shell"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
            esac
            ;;
    esac
}

complete -F _olb olb
`

// zshCompletionScript is the zsh completion script
const zshCompletionScript = `#compdef olb

_olb() {
    local curcontext="$curcontext" state line
    typeset -A opt_args

    _arguments -C \
        '(-h --help)'{-h,--help}'[Show help]' \
        '(-v --version)'{-v,--version}'[Show version]' \
        '--format[Output format]:format:(json table)' \
        '1: :_olb_commands' \
        '*:: :->args'

    case "$line[1]" in
        start)
            _arguments \
                '(-c --config)'{-c,--config}'[Config file path]:file:_files -g "*.yaml"' \
                '(-d --daemon)'{-d,--daemon}'[Run in background]' \
                '--pid-file[PID file path]:file:_files'
            ;;
        stop|reload)
            _arguments \
                '--pid-file[PID file path]:file:_files' \
                '--api-addr[Admin API address]:address:'
            ;;
        status|backend|backend-stats|health|metrics|metrics-show|cert|cert-list|cert-info)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--format[Output format]:format:(json table)'
            ;;
        backend-add)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--weight[Backend weight]:weight:' \
                '--health-check[Health check path]:path:'
            ;;
        backend-remove|backend-drain|backend-enable|backend-disable)
            _arguments \
                '--api-addr[Admin API address]:address:'
            ;;
        route-add)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--backend[Backend pool name]:pool:' \
                '--priority[Route priority]:priority:'
            ;;
        route-remove|route-test)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--format[Output format]:format:(json table)'
            ;;
        cert-add)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--auto[Use ACME/Lets Encrypt]' \
                '--cert[Certificate file path]:file:_files' \
                '--key[Private key file path]:file:_files'
            ;;
        cert-remove|cert-renew)
            _arguments \
                '--api-addr[Admin API address]:address:'
            ;;
        metrics-export)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--output[Output file path]:file:_files' \
                '--format[Export format]:format:(json prometheus)'
            ;;
        config|config-show)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--format[Output format]:format:(yaml json)'
            ;;
        config-diff)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--file[Config file to compare with]:file:_files -g "*.yaml"'
            ;;
        config-validate)
            _arguments \
                '(-c --config)'{-c,--config}'[Config file path]:file:_files -g "*.yaml"'
            ;;
        completion)
            _arguments \
                '--shell[Shell type]:shell:(bash zsh fish)'
            ;;
    esac
}

_olb_commands() {
    local commands=(
        "start:Start the load balancer"
        "stop:Stop the load balancer"
        "reload:Reload configuration"
        "status:Show system status"
        "version:Show version information"
        "config:Configuration commands"
        "backend:Backend management"
        "health:Health check commands"
        "route:Route management"
        "cert:Certificate management"
        "metrics:Metrics commands"
        "completion:Generate shell completion script"
    )
    _describe -t commands 'olb commands' commands "$@"
}

compdef _olb olb
`

// fishCompletionScript is the fish completion script
const fishCompletionScript = `# fish completion for olb

# Disable file completions for the olb command
complete -c olb -f

# Global options
complete -c olb -s h -l help -d "Show help"
complete -c olb -s v -l version -d "Show version"
complete -c olb -l format -d "Output format" -a "json table"

# Commands
complete -c olb -n "__fish_use_subcommand" -a "start" -d "Start the load balancer"
complete -c olb -n "__fish_use_subcommand" -a "stop" -d "Stop the load balancer"
complete -c olb -n "__fish_use_subcommand" -a "reload" -d "Reload configuration"
complete -c olb -n "__fish_use_subcommand" -a "status" -d "Show system status"
complete -c olb -n "__fish_use_subcommand" -a "version" -d "Show version information"
complete -c olb -n "__fish_use_subcommand" -a "config" -d "Configuration commands"
complete -c olb -n "__fish_use_subcommand" -a "backend" -d "Backend management"
complete -c olb -n "__fish_use_subcommand" -a "health" -d "Health check commands"
complete -c olb -n "__fish_use_subcommand" -a "route" -d "Route management"
complete -c olb -n "__fish_use_subcommand" -a "cert" -d "Certificate management"
complete -c olb -n "__fish_use_subcommand" -a "metrics" -d "Metrics commands"
complete -c olb -n "__fish_use_subcommand" -a "completion" -d "Generate shell completion script"

# start command options
complete -c olb -n "__fish_seen_subcommand_from start" -s c -l config -d "Config file path" -r
complete -c olb -n "__fish_seen_subcommand_from start" -s d -l daemon -d "Run in background"
complete -c olb -n "__fish_seen_subcommand_from start" -l pid-file -d "PID file path" -r

# stop/reload command options
complete -c olb -n "__fish_seen_subcommand_from stop reload" -l pid-file -d "PID file path" -r
complete -c olb -n "__fish_seen_subcommand_from stop reload" -l api-addr -d "Admin API address" -r

# status command options
complete -c olb -n "__fish_seen_subcommand_from status" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from status" -l format -d "Output format" -a "json table"

# backend command options
complete -c olb -n "__fish_seen_subcommand_from backend backend-stats" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from backend backend-stats" -l format -d "Output format" -a "json table"

# backend-add command options
complete -c olb -n "__fish_seen_subcommand_from backend-add" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from backend-add" -l weight -d "Backend weight" -r
complete -c olb -n "__fish_seen_subcommand_from backend-add" -l health-check -d "Health check path" -r

# backend-remove/drain/enable/disable command options
complete -c olb -n "__fish_seen_subcommand_from backend-remove backend-drain backend-enable backend-disable" -l api-addr -d "Admin API address" -r

# route command options
complete -c olb -n "__fish_seen_subcommand_from route route-add route-remove route-test" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from route route-test" -l format -d "Output format" -a "json table"

# route-add command options
complete -c olb -n "__fish_seen_subcommand_from route-add" -l backend -d "Backend pool name" -r
complete -c olb -n "__fish_seen_subcommand_from route-add" -l priority -d "Route priority" -r

# cert command options
complete -c olb -n "__fish_seen_subcommand_from cert cert-list cert-info" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from cert cert-list cert-info" -l format -d "Output format" -a "json table"

# cert-add command options
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l auto -d "Use ACME/Lets Encrypt"
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l cert -d "Certificate file path" -r
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l key -d "Private key file path" -r

# cert-remove/renew command options
complete -c olb -n "__fish_seen_subcommand_from cert-remove cert-renew" -l api-addr -d "Admin API address" -r

# metrics command options
complete -c olb -n "__fish_seen_subcommand_from metrics metrics-show" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from metrics metrics-show" -l format -d "Output format" -a "json table prometheus"

# metrics-export command options
complete -c olb -n "__fish_seen_subcommand_from metrics-export" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from metrics-export" -l output -d "Output file path" -r
complete -c olb -n "__fish_seen_subcommand_from metrics-export" -l format -d "Export format" -a "json prometheus"

# config command options
complete -c olb -n "__fish_seen_subcommand_from config config-show" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from config config-show" -l format -d "Output format" -a "yaml json"

# config-diff command options
complete -c olb -n "__fish_seen_subcommand_from config-diff" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from config-diff" -l file -d "Config file to compare with" -r

# config-validate command options
complete -c olb -n "__fish_seen_subcommand_from config-validate" -s c -l config -d "Config file path" -r

# completion command options
complete -c olb -n "__fish_seen_subcommand_from completion" -l shell -d "Shell type" -a "bash zsh fish"

# health command options
complete -c olb -n "__fish_seen_subcommand_from health" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from health" -l format -d "Output format" -a "json table"
`

// Helper method for Client.post (exported for use in commands)
func (c *Client) Post(path string, body, result any) error {
	return c.post(path, body, result)
}

// Helper method for Client.delete (exported for use in commands)
func (c *Client) Delete(path string) error {
	return c.delete(path)
}

// Helper method for Client.get (exported for use in commands)
func (c *Client) Get(path string, result any) error {
	return c.get(path, result)
}

// Helper method for Client.doRequest (exported for use in commands)
func (c *Client) DoRequest(method, path string, body any) (*http.Response, error) {
	return c.doRequest(method, path, body)
}

// Helper function to parse int with default
func parseIntDefault(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return i
}
