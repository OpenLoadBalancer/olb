// Package cli provides command-line interface commands for OpenLoadBalancer.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/engine"
	"github.com/openloadbalancer/olb/pkg/version"
)

// Commands is the list of available commands.
var Commands = []Command{
	&StartCommand{},
	&StopCommand{},
	&ReloadCommand{},
	&StatusCommand{},
	&VersionCommand{},
	&ConfigCommand{},
	&BackendCommand{},
	&HealthCommand{},
	// Advanced CLI commands (Phase 3.15)
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
	// Cluster CLI commands (Phase 4.6)
	&ClusterStatusCommand{},
	&ClusterJoinCommand{},
	&ClusterLeaveCommand{},
	&ClusterMembersCommand{},
}

// FindCommand finds a command by name.
func FindCommand(name string) Command {
	for _, cmd := range Commands {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

// StartCommand starts the load balancer.
type StartCommand struct {
	config  string
	daemon  bool
	pidFile string
}

// Name returns the command name.
func (c *StartCommand) Name() string {
	return "start"
}

// Description returns the command description.
func (c *StartCommand) Description() string {
	return "Start the load balancer"
}

// Run executes the start command.
func (c *StartCommand) Run(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	fs.StringVar(&c.config, "config", "olb.yaml", "Config file path")
	fs.StringVar(&c.config, "c", "olb.yaml", "Config file path (shorthand)")
	fs.BoolVar(&c.daemon, "daemon", false, "Run in background")
	fs.BoolVar(&c.daemon, "d", false, "Run in background (shorthand)")
	fs.StringVar(&c.pidFile, "pid-file", "/var/run/olb.pid", "PID file path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate config exists
	if _, err := os.Stat(c.config); err != nil {
		return fmt.Errorf("config file not found: %s", c.config)
	}

	// Handle daemon mode
	if c.daemon {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("daemon mode is not supported on Windows")
		}
		// Fork process (Unix only)
		if err := forkDaemon(c.config, c.pidFile); err != nil {
			return fmt.Errorf("failed to fork daemon: %w", err)
		}
		return nil
	}

	// Write PID file
	if err := writePIDFile(c.pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer removePIDFile(c.pidFile)

	// Load config
	cfg, err := config.Load(c.config)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create and start engine
	eng, err := engine.New(cfg, c.config)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	if err := eng.Start(); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	fmt.Printf("OpenLoadBalancer started with config: %s\n", c.config)

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			fmt.Println("Received SIGHUP, reloading configuration...")
			if err := eng.Reload(); err != nil {
				fmt.Printf("Reload failed: %v\n", err)
			}
		case syscall.SIGTERM, syscall.SIGINT:
			fmt.Println("Received shutdown signal, stopping...")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			eng.Shutdown(ctx)
			return nil
		}
	}

	return nil
}

// StopCommand stops the load balancer.
type StopCommand struct {
	pidFile string
}

// Name returns the command name.
func (c *StopCommand) Name() string {
	return "stop"
}

// Description returns the command description.
func (c *StopCommand) Description() string {
	return "Stop the load balancer"
}

// Run executes the stop command.
func (c *StopCommand) Run(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	fs.StringVar(&c.pidFile, "pid-file", "/var/run/olb.pid", "PID file path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Read PID from file
	pid, err := readPIDFile(c.pidFile)
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	// Send SIGTERM to process
	if err := sendSignal(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	fmt.Printf("Sent SIGTERM to process %d\n", pid)

	// Wait for process to exit
	if err := waitForProcessExit(pid, 10*time.Second); err != nil {
		return fmt.Errorf("timeout waiting for process to exit: %w", err)
	}

	// Remove PID file
	if err := removePIDFile(c.pidFile); err != nil {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}

	fmt.Println("OpenLoadBalancer stopped successfully")
	return nil
}

// ReloadCommand reloads the configuration.
type ReloadCommand struct {
	pidFile string
	apiAddr string
}

// Name returns the command name.
func (c *ReloadCommand) Name() string {
	return "reload"
}

// Description returns the command description.
func (c *ReloadCommand) Description() string {
	return "Reload configuration"
}

// Run executes the reload command.
func (c *ReloadCommand) Run(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ExitOnError)
	fs.StringVar(&c.pidFile, "pid-file", "/var/run/olb.pid", "PID file path")
	fs.StringVar(&c.apiAddr, "api-addr", "", "Admin API address (e.g., localhost:8081)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Try signal first (SIGHUP to PID)
	pid, err := readPIDFile(c.pidFile)
	if err == nil {
		if sigErr := sendSignal(pid, syscall.SIGHUP); sigErr == nil {
			fmt.Printf("Sent SIGHUP to process %d\n", pid)
			return nil
		}
	}

	// Fall back to admin API
	apiAddr := c.apiAddr
	if apiAddr == "" {
		apiAddr = "localhost:8081"
	}

	url := fmt.Sprintf("http://%s/api/v1/system/reload", apiAddr)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to reload via API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	fmt.Println("Configuration reloaded successfully")
	return nil
}

// StatusCommand shows system status.
type StatusCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *StatusCommand) Name() string {
	return "status"
}

// Description returns the command description.
func (c *StatusCommand) Description() string {
	return "Show system status"
}

// Run executes the status command.
func (c *StatusCommand) Run(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Query admin API for system info
	infoURL := fmt.Sprintf("http://%s/api/v1/system/info", c.apiAddr)
	infoResp, err := http.Get(infoURL)
	if err != nil {
		return fmt.Errorf("failed to query system info: %w", err)
	}
	defer infoResp.Body.Close()

	if infoResp.StatusCode != http.StatusOK {
		return fmt.Errorf("system info API returned status %d", infoResp.StatusCode)
	}

	var info map[string]interface{}
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		return fmt.Errorf("failed to decode system info: %w", err)
	}

	// Query admin API for health
	healthURL := fmt.Sprintf("http://%s/api/v1/system/health", c.apiAddr)
	healthResp, err := http.Get(healthURL)
	if err != nil {
		return fmt.Errorf("failed to query health: %w", err)
	}
	defer healthResp.Body.Close()

	var health map[string]interface{}
	if healthResp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
			health = map[string]interface{}{"status": "unknown"}
		}
	} else {
		health = map[string]interface{}{"status": "unhealthy"}
	}

	// Format and display output
	switch c.format {
	case "json":
		output := map[string]interface{}{
			"info":   info,
			"health": health,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("OpenLoadBalancer Status")
		fmt.Println("=======================")
		fmt.Printf("Version: %v\n", info["version"])
		fmt.Printf("Uptime:  %v\n", info["uptime"])
		fmt.Printf("Health:  %v\n", health["status"])
		if listeners, ok := info["listeners"].(float64); ok {
			fmt.Printf("Listeners: %.0f\n", listeners)
		}
		if backends, ok := info["backends"].(float64); ok {
			fmt.Printf("Backends: %.0f\n", backends)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// VersionCommand shows version information.
type VersionCommand struct{}

// Name returns the command name.
func (c *VersionCommand) Name() string {
	return "version"
}

// Description returns the command description.
func (c *VersionCommand) Description() string {
	return "Show version information"
}

// Run executes the version command.
func (c *VersionCommand) Run(args []string) error {
	fmt.Printf("OpenLoadBalancer %s\n", version.String())
	return nil
}

// ConfigCommand handles configuration commands.
type ConfigCommand struct {
	subcommand string
}

// Name returns the command name.
func (c *ConfigCommand) Name() string {
	return "config"
}

// Description returns the command description.
func (c *ConfigCommand) Description() string {
	return "Configuration commands"
}

// Run executes the config command.
func (c *ConfigCommand) Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: olb config <validate>")
	}

	c.subcommand = args[0]
	subArgs := args[1:]

	switch c.subcommand {
	case "validate":
		return c.runValidate(subArgs)
	default:
		return fmt.Errorf("unknown config subcommand: %s", c.subcommand)
	}
}

func (c *ConfigCommand) runValidate(args []string) error {
	var configPath string
	fs := flag.NewFlagSet("config validate", flag.ExitOnError)
	fs.StringVar(&configPath, "config", "olb.yaml", "Config file path")
	fs.StringVar(&configPath, "c", "olb.yaml", "Config file path (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Load and parse config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	fmt.Printf("Configuration file '%s' is valid\n", configPath)
	fmt.Printf("  Version:   %s\n", cfg.Version)
	fmt.Printf("  Listeners: %d\n", len(cfg.Listeners))
	fmt.Printf("  Pools:     %d\n", len(cfg.Pools))
	return nil
}

// BackendCommand handles backend management commands.
type BackendCommand struct {
	subcommand string
	apiAddr    string
	format     string
}

// Name returns the command name.
func (c *BackendCommand) Name() string {
	return "backend"
}

// Description returns the command description.
func (c *BackendCommand) Description() string {
	return "Backend management"
}

// Run executes the backend command.
func (c *BackendCommand) Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: olb backend <list>")
	}

	c.subcommand = args[0]
	subArgs := args[1:]

	switch c.subcommand {
	case "list":
		return c.runList(subArgs)
	default:
		return fmt.Errorf("unknown backend subcommand: %s", c.subcommand)
	}
}

func (c *BackendCommand) runList(args []string) error {
	fs := flag.NewFlagSet("backend list", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Query admin API
	url := fmt.Sprintf("http://%s/api/v1/backends", c.apiAddr)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to query backends: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var backends []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&backends); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Format and display
	switch c.format {
	case "json":
		data, err := json.MarshalIndent(backends, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("Backends")
		fmt.Println("========")
		fmt.Printf("%-20s %-30s %-10s %-10s\n", "ID", "Address", "Weight", "Status")
		fmt.Println(strings.Repeat("-", 72))
		for _, b := range backends {
			id, _ := b["id"].(string)
			addr, _ := b["address"].(string)
			weight, _ := b["weight"].(float64)
			status, _ := b["status"].(string)
			fmt.Printf("%-20s %-30s %-10.0f %-10s\n", id, addr, weight, status)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// HealthCommand handles health check commands.
type HealthCommand struct {
	subcommand string
	apiAddr    string
	format     string
}

// Name returns the command name.
func (c *HealthCommand) Name() string {
	return "health"
}

// Description returns the command description.
func (c *HealthCommand) Description() string {
	return "Health check commands"
}

// Run executes the health command.
func (c *HealthCommand) Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: olb health <show>")
	}

	c.subcommand = args[0]
	subArgs := args[1:]

	switch c.subcommand {
	case "show":
		return c.runShow(subArgs)
	default:
		return fmt.Errorf("unknown health subcommand: %s", c.subcommand)
	}
}

func (c *HealthCommand) runShow(args []string) error {
	fs := flag.NewFlagSet("health show", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Query admin API
	url := fmt.Sprintf("http://%s/api/v1/health", c.apiAddr)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to query health: %w", err)
	}
	defer resp.Body.Close()

	var health map[string]interface{}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	} else {
		health = map[string]interface{}{
			"status": "unhealthy",
			"code":   resp.StatusCode,
		}
	}

	// Format and display
	switch c.format {
	case "json":
		data, err := json.MarshalIndent(health, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("Health Status")
		fmt.Println("=============")
		fmt.Printf("Status: %v\n", health["status"])
		if msg, ok := health["message"].(string); ok && msg != "" {
			fmt.Printf("Message: %s\n", msg)
		}
		if checks, ok := health["checks"].(map[string]interface{}); ok {
			fmt.Println("\nChecks:")
			for name, check := range checks {
				fmt.Printf("  %s: %v\n", name, check)
			}
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// Helper functions

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pid, nil
}

func writePIDFile(path string, pid int) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

func removePIDFile(path string) error {
	return os.Remove(path)
}

func sendSignal(pid int, sig syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(sig)
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		process, err := os.FindProcess(pid)
		if err != nil {
			return nil // Process not found, assume exited
		}
		// On Unix, FindProcess always succeeds, need to send signal 0 to check
		if runtime.GOOS != "windows" {
			if err := process.Signal(syscall.Signal(0)); err != nil {
				return nil // Process not running
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout")
}

func forkDaemon(configPath, pidFile string) error {
	// This is a placeholder for Unix daemon forking
	// In a real implementation, this would use syscall.ForkExec
	return fmt.Errorf("daemon mode not yet implemented")
}
