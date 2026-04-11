package health

import (
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestCheckExec_Success(t *testing.T) {
	c := NewChecker()
	defer c.Stop()

	b := backend.NewBackend("test-1", "127.0.0.1:8080")
	cfg := &Check{
		Type:     "exec",
		Command:  "echo",
		Args:     []string{"-n", ""},
		Timeout:  5 * time.Second,
		Interval: 10 * time.Second,
	}

	result := c.checkExec(b, cfg)
	if !result.Healthy {
		t.Errorf("expected healthy, got error: %v", result.Error)
	}
}

func TestCheckExec_Failure(t *testing.T) {
	c := NewChecker()
	defer c.Stop()

	b := backend.NewBackend("test-1", "127.0.0.1:8080")
	cfg := &Check{
		Type:     "exec",
		Command:  "false", // exits with code 1
		Timeout:  5 * time.Second,
		Interval: 10 * time.Second,
	}

	result := c.checkExec(b, cfg)
	if result.Healthy {
		t.Error("expected unhealthy for exit code 1")
	}
	if result.Error == nil {
		t.Error("expected error for failed command")
	}
}

func TestCheckExec_NoCommand(t *testing.T) {
	c := NewChecker()
	defer c.Stop()

	b := backend.NewBackend("test-1", "127.0.0.1:8080")
	cfg := &Check{
		Type:     "exec",
		Timeout:  5 * time.Second,
		Interval: 10 * time.Second,
	}

	result := c.checkExec(b, cfg)
	if result.Healthy {
		t.Error("expected unhealthy for missing command")
	}
	if result.Error == nil {
		t.Error("expected error for missing command")
	}
}

func TestCheckExec_Timeout(t *testing.T) {
	c := NewChecker()
	defer c.Stop()

	b := backend.NewBackend("test-1", "127.0.0.1:8080")
	cfg := &Check{
		Type:     "exec",
		Command:  "sleep",
		Args:     []string{"10"},
		Timeout:  100 * time.Millisecond,
		Interval: 10 * time.Second,
	}

	result := c.checkExec(b, cfg)
	if result.Healthy {
		t.Error("expected unhealthy for timeout")
	}
	if result.Error == nil {
		t.Error("expected error for timeout")
	}
}

func TestCheckExec_WithBackendAddress(t *testing.T) {
	c := NewChecker()
	defer c.Stop()

	b := backend.NewBackend("test-1", "192.168.1.1:9090")
	cfg := &Check{
		Type:     "exec",
		Command:  "echo",
		Args:     []string{"-n"},
		Timeout:  5 * time.Second,
		Interval: 10 * time.Second,
	}

	result := c.checkExec(b, cfg)
	if !result.Healthy {
		t.Errorf("expected healthy, got error: %v", result.Error)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string that exceeds the limit", 10, "this is a ..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateString(tt.input, tt.maxLen)
		if got != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
		}
	}
}
