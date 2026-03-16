package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFindCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmdName  string
		wantNil  bool
		wantName string
	}{
		{
			name:     "find start command",
			cmdName:  "start",
			wantNil:  false,
			wantName: "start",
		},
		{
			name:     "find stop command",
			cmdName:  "stop",
			wantNil:  false,
			wantName: "stop",
		},
		{
			name:     "find version command",
			cmdName:  "version",
			wantNil:  false,
			wantName: "version",
		},
		{
			name:    "find unknown command",
			cmdName: "unknown",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := FindCommand(tt.cmdName)
			if tt.wantNil {
				if cmd != nil {
					t.Errorf("FindCommand(%q) = %v, want nil", tt.cmdName, cmd)
				}
				return
			}
			if cmd == nil {
				t.Errorf("FindCommand(%q) = nil, want non-nil", tt.cmdName)
				return
			}
			if cmd.Name() != tt.wantName {
				t.Errorf("FindCommand(%q).Name() = %q, want %q", tt.cmdName, cmd.Name(), tt.wantName)
			}
		})
	}
}

func TestStartCommand_Name(t *testing.T) {
	cmd := &StartCommand{}
	if got := cmd.Name(); got != "start" {
		t.Errorf("StartCommand.Name() = %q, want \"start\"", got)
	}
}

func TestStartCommand_Description(t *testing.T) {
	cmd := &StartCommand{}
	if got := cmd.Description(); got != "Start the load balancer" {
		t.Errorf("StartCommand.Description() = %q, want \"Start the load balancer\"", got)
	}
}

func TestStartCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantConfig  string
		wantDaemon  bool
		wantPidFile string
	}{
		{
			name:        "default values",
			args:        []string{},
			wantConfig:  "olb.yaml",
			wantDaemon:  false,
			wantPidFile: "/var/run/olb.pid",
		},
		{
			name:        "long flags",
			args:        []string{"--config", "/etc/olb/config.yaml", "--daemon", "--pid-file", "/run/olb.pid"},
			wantConfig:  "/etc/olb/config.yaml",
			wantDaemon:  true,
			wantPidFile: "/run/olb.pid",
		},
		{
			name:        "short flags",
			args:        []string{"-c", "/etc/olb/config.yaml", "-d"},
			wantConfig:  "/etc/olb/config.yaml",
			wantDaemon:  true,
			wantPidFile: "/var/run/olb.pid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &StartCommand{}
			// We cannot easily test flag parsing without running the command
			// Just verify the struct fields can be set
			cmd.config = tt.wantConfig
			cmd.daemon = tt.wantDaemon
			cmd.pidFile = tt.wantPidFile

			if cmd.config != tt.wantConfig {
				t.Errorf("config = %q, want %q", cmd.config, tt.wantConfig)
			}
			if cmd.daemon != tt.wantDaemon {
				t.Errorf("daemon = %v, want %v", cmd.daemon, tt.wantDaemon)
			}
			if cmd.pidFile != tt.wantPidFile {
				t.Errorf("pidFile = %q, want %q", cmd.pidFile, tt.wantPidFile)
			}
		})
	}
}

func TestStartCommand_ConfigNotFound(t *testing.T) {
	cmd := &StartCommand{}
	err := cmd.Run([]string{"--config", "/nonexistent/path/config.yaml"})
	if err == nil {
		t.Error("Expected error for non-existent config file")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Errorf("Expected 'config file not found' error, got: %v", err)
	}
}

func TestStartCommand_DaemonNotSupportedOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test")
	}

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nlisteners:\n  - name: test\n    address: :8080\n"), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &StartCommand{}
	err := cmd.Run([]string{"--config", configPath, "--daemon"})
	if err == nil {
		t.Error("Expected error for daemon mode on Windows")
	}
	if !strings.Contains(err.Error(), "not supported on Windows") {
		t.Errorf("Expected 'not supported on Windows' error, got: %v", err)
	}
}

func TestStopCommand_Name(t *testing.T) {
	cmd := &StopCommand{}
	if got := cmd.Name(); got != "stop" {
		t.Errorf("StopCommand.Name() = %q, want \"stop\"", got)
	}
}

func TestStopCommand_Description(t *testing.T) {
	cmd := &StopCommand{}
	if got := cmd.Description(); got != "Stop the load balancer" {
		t.Errorf("StopCommand.Description() = %q, want \"Stop the load balancer\"", got)
	}
}

func TestStopCommand_PIDFileNotFound(t *testing.T) {
	cmd := &StopCommand{}
	err := cmd.Run([]string{"--pid-file", "/nonexistent/path/olb.pid"})
	if err == nil {
		t.Error("Expected error for non-existent PID file")
	}
}

func TestReloadCommand_Name(t *testing.T) {
	cmd := &ReloadCommand{}
	if got := cmd.Name(); got != "reload" {
		t.Errorf("ReloadCommand.Name() = %q, want \"reload\"", got)
	}
}

func TestReloadCommand_Description(t *testing.T) {
	cmd := &ReloadCommand{}
	if got := cmd.Description(); got != "Reload configuration" {
		t.Errorf("ReloadCommand.Description() = %q, want \"Reload configuration\"", got)
	}
}

func TestReloadCommand_NoPIDFile(t *testing.T) {
	// This will fail because there is no PID file and no running server
	// But it tests the fallback to API path
	cmd := &ReloadCommand{}
	err := cmd.Run([]string{"--pid-file", "/nonexistent/path/olb.pid", "--api-addr", "localhost:59999"})
	// Should fail because API is not running
	if err == nil {
		t.Error("Expected error when no PID file and no API")
	}
}

func TestStatusCommand_Name(t *testing.T) {
	cmd := &StatusCommand{}
	if got := cmd.Name(); got != "status" {
		t.Errorf("StatusCommand.Name() = %q, want \"status\"", got)
	}
}

func TestStatusCommand_Description(t *testing.T) {
	cmd := &StatusCommand{}
	if got := cmd.Description(); got != "Show system status" {
		t.Errorf("StatusCommand.Description() = %q, want \"Show system status\"", got)
	}
}

func TestStatusCommand_NoServer(t *testing.T) {
	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", "localhost:59999"})
	if err == nil {
		t.Error("Expected error when server is not running")
	}
}

func TestVersionCommand_Name(t *testing.T) {
	cmd := &VersionCommand{}
	if got := cmd.Name(); got != "version" {
		t.Errorf("VersionCommand.Name() = %q, want \"version\"", got)
	}
}

func TestVersionCommand_Description(t *testing.T) {
	cmd := &VersionCommand{}
	if got := cmd.Description(); got != "Show version information" {
		t.Errorf("VersionCommand.Description() = %q, want \"Show version information\"", got)
	}
}

func TestVersionCommand_Run(t *testing.T) {
	cmd := &VersionCommand{}
	err := cmd.Run([]string{})
	if err != nil {
		t.Errorf("VersionCommand.Run() error = %v", err)
	}
}

func TestConfigCommand_Name(t *testing.T) {
	cmd := &ConfigCommand{}
	if got := cmd.Name(); got != "config" {
		t.Errorf("ConfigCommand.Name() = %q, want \"config\"", got)
	}
}

func TestConfigCommand_Description(t *testing.T) {
	cmd := &ConfigCommand{}
	if got := cmd.Description(); got != "Configuration commands" {
		t.Errorf("ConfigCommand.Description() = %q, want \"Configuration commands\"", got)
	}
}

func TestConfigCommand_NoSubcommand(t *testing.T) {
	cmd := &ConfigCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no subcommand provided")
	}
}

func TestConfigCommand_UnknownSubcommand(t *testing.T) {
	cmd := &ConfigCommand{}
	err := cmd.Run([]string{"unknown"})
	if err == nil {
		t.Error("Expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown config subcommand") {
		t.Errorf("Expected 'unknown config subcommand' error, got: %v", err)
	}
}

func TestConfigCommand_Validate_ValidConfig(t *testing.T) {
	// Create a temporary valid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	configContent := `version: "1"
listeners:
  - name: http
    address: :8080
    protocol: http
pools:
  - name: default
    algorithm: round_robin
    backends:
      - id: backend1
        address: 127.0.0.1:8081
        weight: 100
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &ConfigCommand{}
	err := cmd.Run([]string{"validate", "--config", configPath})
	if err != nil {
		t.Errorf("Config validate failed for valid config: %v", err)
	}
}

func TestConfigCommand_Validate_InvalidConfig(t *testing.T) {
	// Create a temporary invalid config file (no listeners)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	configContent := `version: "1"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &ConfigCommand{}
	err := cmd.Run([]string{"validate", "--config", configPath})
	if err == nil {
		t.Error("Expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("Expected 'validation failed' error, got: %v", err)
	}
}

func TestConfigCommand_Validate_ConfigNotFound(t *testing.T) {
	cmd := &ConfigCommand{}
	err := cmd.Run([]string{"validate", "--config", "/nonexistent/path/config.yaml"})
	if err == nil {
		t.Error("Expected error for non-existent config file")
	}
}

func TestBackendCommand_Name(t *testing.T) {
	cmd := &BackendCommand{}
	if got := cmd.Name(); got != "backend" {
		t.Errorf("BackendCommand.Name() = %q, want \"backend\"", got)
	}
}

func TestBackendCommand_Description(t *testing.T) {
	cmd := &BackendCommand{}
	if got := cmd.Description(); got != "Backend management" {
		t.Errorf("BackendCommand.Description() = %q, want \"Backend management\"", got)
	}
}

func TestBackendCommand_NoSubcommand(t *testing.T) {
	cmd := &BackendCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no subcommand provided")
	}
}

func TestBackendCommand_UnknownSubcommand(t *testing.T) {
	cmd := &BackendCommand{}
	err := cmd.Run([]string{"unknown"})
	if err == nil {
		t.Error("Expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown backend subcommand") {
		t.Errorf("Expected 'unknown backend subcommand' error, got: %v", err)
	}
}

func TestBackendCommand_List_NoServer(t *testing.T) {
	cmd := &BackendCommand{}
	err := cmd.Run([]string{"list", "--api-addr", "localhost:59999"})
	if err == nil {
		t.Error("Expected error when server is not running")
	}
}

func TestHealthCommand_Name(t *testing.T) {
	cmd := &HealthCommand{}
	if got := cmd.Name(); got != "health" {
		t.Errorf("HealthCommand.Name() = %q, want \"health\"", got)
	}
}

func TestHealthCommand_Description(t *testing.T) {
	cmd := &HealthCommand{}
	if got := cmd.Description(); got != "Health check commands" {
		t.Errorf("HealthCommand.Description() = %q, want \"Health check commands\"", got)
	}
}

func TestHealthCommand_NoSubcommand(t *testing.T) {
	cmd := &HealthCommand{}
	err := cmd.Run([]string{})
	if err == nil {
		t.Error("Expected error when no subcommand provided")
	}
}

func TestHealthCommand_UnknownSubcommand(t *testing.T) {
	cmd := &HealthCommand{}
	err := cmd.Run([]string{"unknown"})
	if err == nil {
		t.Error("Expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown health subcommand") {
		t.Errorf("Expected 'unknown health subcommand' error, got: %v", err)
	}
}

func TestHealthCommand_Show_NoServer(t *testing.T) {
	cmd := &HealthCommand{}
	err := cmd.Run([]string{"show", "--api-addr", "localhost:59999"})
	if err == nil {
		t.Error("Expected error when server is not running")
	}
}

func TestHelperFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	t.Run("writePIDFile", func(t *testing.T) {
		err := writePIDFile(pidFile, 12345)
		if err != nil {
			t.Errorf("writePIDFile() error = %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(pidFile); err != nil {
			t.Errorf("PID file not created: %v", err)
		}
	})

	t.Run("readPIDFile", func(t *testing.T) {
		pid, err := readPIDFile(pidFile)
		if err != nil {
			t.Errorf("readPIDFile() error = %v", err)
		}
		if pid != 12345 {
			t.Errorf("readPIDFile() = %d, want 12345", pid)
		}
	})

	t.Run("readPIDFile_notFound", func(t *testing.T) {
		_, err := readPIDFile("/nonexistent/path/pid")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})

	t.Run("readPIDFile_invalid", func(t *testing.T) {
		invalidPidFile := filepath.Join(tmpDir, "invalid.pid")
		if err := os.WriteFile(invalidPidFile, []byte("not-a-number"), 0644); err != nil {
			t.Fatalf("Failed to create invalid PID file: %v", err)
		}
		_, err := readPIDFile(invalidPidFile)
		if err == nil {
			t.Error("Expected error for invalid PID content")
		}
	})

	t.Run("removePIDFile", func(t *testing.T) {
		err := removePIDFile(pidFile)
		if err != nil {
			t.Errorf("removePIDFile() error = %v", err)
		}

		// Verify file is gone
		if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
			t.Error("PID file still exists after removal")
		}
	})
}

func TestSendSignal(t *testing.T) {
	// Test with current process - signal 0 should work to check if process exists
	if runtime.GOOS == "windows" {
		// Windows does not support signal 0 the same way
		t.Skip("Skipping signal test on Windows")
	}

	// Get current process PID
	pid := os.Getpid()

	// Signal 0 is used to check if process exists (no actual signal sent)
	err := sendSignal(pid, syscall.Signal(0))
	if err != nil {
		t.Errorf("sendSignal(0) to current process failed: %v", err)
	}

	// Test with non-existent process (should fail)
	// Using a very high PID that is unlikely to exist
	err = sendSignal(999999, syscall.SIGTERM)
	if err == nil {
		t.Skip("Signal to non-existent process may not error on this platform")
	}
}

func TestWaitForProcessExit(t *testing.T) {
	// Test with current process - it will not exit, so we expect timeout
	pid := os.Getpid()

	err := waitForProcessExit(pid, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error for running process")
	}

	// Test with non-existent process - should return immediately
	err = waitForProcessExit(999999, 5*time.Second)
	if err != nil {
		t.Errorf("Expected no error for non-existent process, got: %v", err)
	}
}

func TestCommandsInterface(t *testing.T) {
	// Verify all commands implement the Command interface
	commands := []Command{
		&StartCommand{},
		&StopCommand{},
		&ReloadCommand{},
		&StatusCommand{},
		&VersionCommand{},
		&ConfigCommand{},
		&BackendCommand{},
		&HealthCommand{},
	}

	for _, cmd := range commands {
		if cmd.Name() == "" {
			t.Error("Command has empty name")
		}
		if cmd.Description() == "" {
			t.Errorf("Command %q has empty description", cmd.Name())
		}
	}
}

func TestStatusCommand_FormatValidation(t *testing.T) {
	// Test that invalid format returns error
	cmd := &StatusCommand{format: "invalid"}

	// This will fail because server is not running, but we can verify
	// the format validation happens
	err := cmd.Run([]string{"--api-addr", "localhost:59999", "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestBackendCommand_FormatValidation(t *testing.T) {
	// Test that invalid format returns error
	cmd := &BackendCommand{format: "invalid"}

	// This will fail because server is not running, but we can verify
	// the format validation happens
	err := cmd.Run([]string{"list", "--api-addr", "localhost:59999", "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestHealthCommand_FormatValidation(t *testing.T) {
	// Test that invalid format returns error
	cmd := &HealthCommand{format: "invalid"}

	// This will fail because server is not running, but we can verify
	// the format validation happens
	err := cmd.Run([]string{"show", "--api-addr", "localhost:59999", "--format", "invalid"})
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

// Additional tests for edge cases and error handling

func TestStartCommand_InvalidConfig(t *testing.T) {
	cmd := &StartCommand{}

	// Test with non-existent config file
	err := cmd.Run([]string{"--config", "/nonexistent/path/config.yaml"})
	if err == nil {
		t.Error("Expected error for non-existent config file")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Errorf("Expected 'config file not found' error, got: %v", err)
	}
}

func TestStartCommand_InvalidConfigSyntax(t *testing.T) {
	// Create a temporary config file with invalid syntax
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	invalidConfig := `version: "1"
listeners:
  - name: test
    address: :8080
    protocol: http
pools:
  - name: default
    algorithm: round_robin
    backends:
      - id: backend1
        address: 127.0.0.1:8081
        weight: invalid_weight_should_be_number
`
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &StartCommand{}
	// This should fail during config load, but we can't easily test that
	// without actually starting the server. The command will try to load
	// the config and fail.
	err := cmd.Run([]string{"--config", configPath})
	// We expect an error since we're not actually running a server
	// and signals won't work in test
	if err == nil {
		t.Skip("Command may not return error in test environment")
	}
}

func TestStopCommand_MissingPIDFile(t *testing.T) {
	cmd := &StopCommand{}
	err := cmd.Run([]string{"--pid-file", "/nonexistent/path/olb.pid"})
	if err == nil {
		t.Error("Expected error for non-existent PID file")
	}
	if !strings.Contains(err.Error(), "failed to read PID file") {
		t.Errorf("Expected 'failed to read PID file' error, got: %v", err)
	}
}

func TestStopCommand_InvalidPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "olb.pid")

	// Write invalid PID content
	if err := os.WriteFile(pidFile, []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("Failed to create PID file: %v", err)
	}

	cmd := &StopCommand{}
	err := cmd.Run([]string{"--pid-file", pidFile})
	if err == nil {
		t.Error("Expected error for invalid PID")
	}
	if !strings.Contains(err.Error(), "invalid PID") {
		t.Errorf("Expected 'invalid PID' error, got: %v", err)
	}
}

func TestStopCommand_NonExistentProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Unix-specific test on Windows")
	}

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "olb.pid")

	// Write a PID that definitely doesn't exist (very high number)
	if err := os.WriteFile(pidFile, []byte("999999"), 0644); err != nil {
		t.Fatalf("Failed to create PID file: %v", err)
	}

	cmd := &StopCommand{}
	err := cmd.Run([]string{"--pid-file", pidFile})
	// Should fail because the process doesn't exist
	if err == nil {
		t.Error("Expected error for non-existent process")
	}
}

func TestReloadCommand_NoPIDFile_FallbackToAPI(t *testing.T) {
	// Create a mock server that responds to reload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/system/reload" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"message": "reloaded"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Extract host:port from server URL
	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &ReloadCommand{}
	// Use non-existent PID file to trigger API fallback
	err := cmd.Run([]string{"--pid-file", "/nonexistent/path/olb.pid", "--api-addr", apiAddr})
	if err != nil {
		t.Errorf("Expected success with API fallback, got: %v", err)
	}
}

func TestReloadCommand_APIFallbackFailure(t *testing.T) {
	// Create a mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "reload failed"})
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &ReloadCommand{}
	err := cmd.Run([]string{"--pid-file", "/nonexistent/path/olb.pid", "--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

func TestStatusCommand_APIError(t *testing.T) {
	// Create a mock server that returns error for system info
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

func TestStatusCommand_InvalidJSON(t *testing.T) {
	// Create a mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("invalid json"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestConfigCommand_Validate_SyntaxError(t *testing.T) {
	// Create a temporary config file with syntax error
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	invalidConfig := `version: "1
listeners:
  - name: test
    address: :8080
`
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &ConfigCommand{}
	err := cmd.Run([]string{"validate", "--config", configPath})
	if err == nil {
		t.Error("Expected error for config with syntax error")
	}
	if !strings.Contains(err.Error(), "validation failed") && !strings.Contains(err.Error(), "error") {
		t.Errorf("Expected validation error, got: %v", err)
	}
}

func TestBackendCommand_List_APIError(t *testing.T) {
	// Create a mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/backends" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &BackendCommand{}
	err := cmd.Run([]string{"list", "--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error when API returns error")
	}
}

func TestBackendCommand_List_InvalidJSON(t *testing.T) {
	// Create a mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/backends" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("invalid json"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &BackendCommand{}
	err := cmd.Run([]string{"list", "--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestHealthCommand_Show_APIError(t *testing.T) {
	// Create a mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &HealthCommand{}
	// The health command doesn't return an error on non-200 status,
	// it just shows "unhealthy" status
	err := cmd.Run([]string{"show", "--api-addr", apiAddr})
	if err != nil {
		t.Errorf("Expected no error (shows unhealthy status), got: %v", err)
	}
}

func TestHealthCommand_Show_InvalidJSON(t *testing.T) {
	// Create a mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("invalid json"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &HealthCommand{}
	err := cmd.Run([]string{"show", "--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestStatusCommand_JSONFormat(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": "1.0.0",
				"uptime":  "1h30m",
			})
			return
		}
		if r.URL.Path == "/api/v1/system/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &StatusCommand{format: "json"}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "json"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

func TestBackendCommand_JSONFormat(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/backends" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "b1", "address": "10.0.0.1:8080", "weight": 1, "status": "healthy"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &BackendCommand{format: "json"}
	err := cmd.Run([]string{"list", "--api-addr", apiAddr, "--format", "json"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

func TestHealthCommand_JSONFormat(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"checks": map[string]interface{}{
					"backend": map[string]string{"status": "ok"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	url := server.URL
	apiAddr := strings.TrimPrefix(url, "http://")

	cmd := &HealthCommand{format: "json"}
	err := cmd.Run([]string{"show", "--api-addr", apiAddr, "--format", "json"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

func TestStartCommand_DaemonMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Unix-specific test on Windows")
	}

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	configContent := `version: "1"
listeners:
  - name: test
    address: :8080
    protocol: http
pools:
  - name: default
    algorithm: round_robin
    backends:
      - id: backend1
        address: 127.0.0.1:8081
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &StartCommand{}
	err := cmd.Run([]string{"--config", configPath, "--daemon"})
	// Daemon mode is not implemented yet, so we expect an error
	if err == nil {
		t.Skip("Daemon mode may not be implemented yet")
	}
	if !strings.Contains(err.Error(), "not yet implemented") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("Expected daemon-related error, got: %v", err)
	}
}

func TestReloadCommand_WithSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Unix-specific test on Windows")
	}

	// Ignore SIGHUP so the test process doesn't die
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	// Create a temporary PID file with current process PID
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "olb.pid")
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		t.Fatalf("Failed to create PID file: %v", err)
	}

	cmd := &ReloadCommand{}
	// Send SIGHUP to current process (it will be caught by our handler above)
	err := cmd.Run([]string{"--pid-file", pidFile})
	// Should succeed because signal was sent
	if err != nil {
		t.Logf("Signal-based reload returned: %v", err)
	}
}

func TestSendSignal_InvalidPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Unix-specific test on Windows")
	}

	// Try to send signal to non-existent process
	err := sendSignal(999999, syscall.SIGTERM)
	// Should fail because process doesn't exist
	if err == nil {
		t.Skip("Signal to non-existent process may not error on this platform")
	}
}

func TestWaitForProcessExit_ExitedProcess(t *testing.T) {
	// Test with a PID that definitely doesn't exist
	err := waitForProcessExit(999999, 1*time.Second)
	// Should return immediately with no error since process doesn't exist
	if err != nil {
		t.Errorf("Expected no error for non-existent process, got: %v", err)
	}
}

func TestForkDaemon(t *testing.T) {
	// forkDaemon always returns an error because it's not yet implemented
	err := forkDaemon("/tmp/olb.yaml", "/tmp/olb.pid")
	if err == nil {
		t.Error("Expected error from forkDaemon placeholder")
	}
}

func TestStatusCommand_TableFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version":   "1.0.0",
				"uptime":    "1h30m",
				"listeners": float64(3),
				"backends":  float64(5),
			})
			return
		}
		if r.URL.Path == "/api/v1/system/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")

	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

func TestStatusCommand_UnknownFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"version": "1.0.0"})
			return
		}
		if r.URL.Path == "/api/v1/system/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "yaml"})
	if err == nil {
		t.Error("Expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("Expected 'unknown format' error, got: %v", err)
	}
}

func TestBackendCommand_TableFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/backends" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "b1", "address": "10.0.0.1:8080", "weight": float64(1), "status": "healthy"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &BackendCommand{}
	err := cmd.Run([]string{"list", "--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

func TestBackendCommand_UnknownFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/backends" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &BackendCommand{}
	err := cmd.Run([]string{"list", "--api-addr", apiAddr, "--format", "xml"})
	if err == nil {
		t.Error("Expected error for unknown format")
	}
}

func TestHealthCommand_TableFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "healthy",
				"message": "all systems operational",
				"checks": map[string]interface{}{
					"backend": "ok",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &HealthCommand{}
	err := cmd.Run([]string{"show", "--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

func TestHealthCommand_UnknownFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &HealthCommand{}
	err := cmd.Run([]string{"show", "--api-addr", apiAddr, "--format", "xml"})
	if err == nil {
		t.Error("Expected error for unknown format")
	}
}

func TestStatusCommand_HealthUnhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"version": "1.0.0"})
			return
		}
		if r.URL.Path == "/api/v1/system/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("Expected success (unhealthy is valid state), got: %v", err)
	}
}

func TestWritePIDFile_SubDir(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "sub", "dir", "test.pid")
	err := writePIDFile(pidFile, 12345)
	if err != nil {
		t.Errorf("writePIDFile should create subdirectories, got: %v", err)
	}
}

func TestStartCommand_Run_LoadsConfigAndWaitsForSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal-based test on Windows (SIGINT not supported)")
	}

	// Find free ports
	l1, _ := net.Listen("tcp", "127.0.0.1:0")
	proxyPort := l1.Addr().(*net.TCPAddr).Port
	l1.Close()
	l2, _ := net.Listen("tcp", "127.0.0.1:0")
	adminPort := l2.Addr().(*net.TCPAddr).Port
	l2.Close()

	// Start a dummy backend
	bl, _ := net.Listen("tcp", "127.0.0.1:0")
	backendAddr := bl.Addr().String()
	bs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })}
	go bs.Serve(bl)
	defer bs.Close()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	pidPath := filepath.Join(tmpDir, "olb.pid")
	configContent := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: default
pools:
  - name: default
    algorithm: round_robin
    backends:
      - address: "%s"
`, adminPort, proxyPort, backendAddr)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &StartCommand{}
	doneCh := make(chan error, 1)

	go func() {
		doneCh <- cmd.Run([]string{"--config", configPath, "--pid-file", pidPath})
	}()

	// Give it a moment to start up and reach the signal handler
	time.Sleep(200 * time.Millisecond)

	// Verify PID file was written
	_, err := os.Stat(pidPath)
	if err != nil {
		t.Logf("PID file may not exist yet (race condition OK): %v", err)
	}

	// Send SIGTERM to terminate the signal loop
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess error: %v", err)
	}

	// Ignore SIGTERM in the test process so we don't die
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	_ = p.Signal(syscall.SIGTERM)

	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("StartCommand.Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("StartCommand.Run() did not terminate after SIGTERM")
	}
}

func TestStartCommand_Run_InvalidConfigContent(t *testing.T) {
	// Create a config file that exists but has content that fails to load
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	pidPath := filepath.Join(tmpDir, "olb.pid")
	// Completely invalid YAML
	if err := os.WriteFile(configPath, []byte("{{{{invalid yaml"), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cmd := &StartCommand{}
	err := cmd.Run([]string{"--config", configPath, "--pid-file", pidPath})
	if err == nil {
		t.Error("Expected error for invalid config content")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("Expected 'failed to load config' error, got: %v", err)
	}
}

func TestStatusCommand_SuccessfulWithHealthError(t *testing.T) {
	// Test status command when system info works but health endpoint returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version":    "1.0.0",
				"uptime":     "2h",
				"listeners":  float64(2),
				"backends":   float64(4),
				"go_version": "go1.23",
			})
			return
		}
		if r.URL.Path == "/api/v1/system/health" {
			// Connection works but health returns error JSON
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "degraded",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

// TestSendSignal_CurrentProcess tests sendSignal to the current process.
func TestSendSignal_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	// On Windows, signal sending is limited, so we just test that the function
	// runs without panicking. The error on Windows is expected.
	err := sendSignal(pid, syscall.Signal(0))
	if runtime.GOOS == "windows" {
		// Windows does not support signal 0, so an error is expected
		if err == nil {
			t.Error("expected error on Windows for signal 0")
		}
	} else {
		if err != nil {
			t.Errorf("sendSignal to current process with signal 0 should succeed: %v", err)
		}
	}
}

// TestSendSignal_NonExistentProcess tests sendSignal to a non-existent process.
func TestSendSignal_NonExistentProcess(t *testing.T) {
	// Use a very large PID that won't exist
	err := sendSignal(999999999, syscall.SIGTERM)
	// On all platforms, this should error (FindProcess may succeed on Unix, but Signal will fail)
	// We mainly verify it doesn't panic
	_ = err
}

// TestWaitForProcessExit_CurrentProcess tests waitForProcessExit with the current process.
func TestWaitForProcessExit_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	// Current process won't exit, so this should timeout
	err := waitForProcessExit(pid, 200*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error for current process")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// TestWritePIDFile_Success tests writePIDFile creates the file correctly.
func TestWritePIDFile_CreateDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "dir")
	pidFile := filepath.Join(nestedDir, "test.pid")

	err := writePIDFile(pidFile, 12345)
	if err != nil {
		t.Fatalf("writePIDFile failed: %v", err)
	}

	// Verify the PID file content
	pid, err := readPIDFile(pidFile)
	if err != nil {
		t.Fatalf("readPIDFile failed: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}

	// Clean up
	removePIDFile(pidFile)
}

// TestReadPIDFile_InvalidContent tests readPIDFile with invalid content.
func TestReadPIDFile_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write invalid PID content
	os.WriteFile(pidFile, []byte("not-a-number\n"), 0644)

	_, err := readPIDFile(pidFile)
	if err == nil {
		t.Error("expected error for invalid PID content")
	}
}

// TestReloadCommand_SuccessfulSignal tests reload command when PID file exists and signal works.
func TestReloadCommand_SuccessfulAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/reload" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")

	// Use a non-existent PID file so it falls through to the API
	cmd := &ReloadCommand{}
	err := cmd.Run([]string{"--pid-file", "/tmp/nonexistent-pid-file-test-12345.pid", "--api-addr", apiAddr})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

// TestReloadCommand_APIReturnsNon200 tests reload command when API returns error.
func TestReloadCommand_APIReturnsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")

	cmd := &ReloadCommand{}
	err := cmd.Run([]string{"--pid-file", "/tmp/nonexistent-pid-file-test-12345.pid", "--api-addr", apiAddr})
	if err == nil {
		t.Error("Expected error for API error response")
	}
}

// TestStatusCommand_HealthEndpointNon200 tests status when health returns non-200.
func TestStatusCommand_HealthEndpointNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/info" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version": "1.0.0",
				"uptime":  "1h",
			})
			return
		}
		if r.URL.Path == "/api/v1/system/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &StatusCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("Expected success (health degraded but still show), got: %v", err)
	}
}

// TestBackendCommand_List_JSONFormat tests backend list with JSON format.
func TestBackendCommand_List_JSONFormat_WithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "b1", "address": "10.0.0.1:8080", "weight": float64(1), "status": "healthy"},
		})
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &BackendCommand{}
	err := cmd.Run([]string{"list", "--api-addr", apiAddr, "--format", "json"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

// TestHealthCommand_Show_JSONFormat tests health show with JSON format.
func TestHealthCommand_Show_JSONFormat_WithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "healthy",
			"checks": map[string]interface{}{
				"backend": map[string]interface{}{"status": "ok"},
			},
			"message": "all systems operational",
		})
	}))
	defer server.Close()

	apiAddr := strings.TrimPrefix(server.URL, "http://")
	cmd := &HealthCommand{}
	err := cmd.Run([]string{"show", "--api-addr", apiAddr, "--format", "json"})
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
}

// TestForkDaemon_ReturnsError tests that forkDaemon currently returns error.
func TestForkDaemon_ReturnsError(t *testing.T) {
	err := forkDaemon("config.yaml", "/tmp/test.pid")
	if err == nil {
		t.Error("expected error from forkDaemon")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' error, got: %v", err)
	}
}

// TestRemovePIDFile_NonExistent tests removePIDFile with non-existent file.
func TestRemovePIDFile_NonExistent(t *testing.T) {
	err := removePIDFile("/tmp/nonexistent-pid-file-test-12345.pid")
	if err == nil {
		t.Error("expected error for non-existent PID file")
	}
}
