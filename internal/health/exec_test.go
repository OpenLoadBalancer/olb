package health

import (
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestResolveExecTemplate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		address string
		host    string
		port    string
		want    string
	}{
		{
			name:    "no templates",
			input:   "curl",
			address: "10.0.0.1:8080",
			host:    "10.0.0.1",
			port:    "8080",
			want:    "curl",
		},
		{
			name:    "address template",
			input:   "ping -c 1 {{.Address}}",
			address: "10.0.0.1:8080",
			host:    "10.0.0.1",
			port:    "8080",
			want:    "ping -c 1 10.0.0.1:8080",
		},
		{
			name:    "host template",
			input:   "curl http://{{.Host}}:{{.Port}}/health",
			address: "10.0.0.1:8080",
			host:    "10.0.0.1",
			port:    "8080",
			want:    "curl http://10.0.0.1:8080/health",
		},
		{
			name:    "port template",
			input:   "nc -z {{.Host}} {{.Port}}",
			address: "db.example.com:5432",
			host:    "db.example.com",
			port:    "5432",
			want:    "nc -z db.example.com 5432",
		},
		{
			name:    "mixed templates",
			input:   "{{.Host}}:{{.Port}}={{.Address}}",
			address: "192.168.1.1:9090",
			host:    "192.168.1.1",
			port:    "9090",
			want:    "192.168.1.1:9090=192.168.1.1:9090",
		},
		{
			name:    "empty string",
			input:   "",
			address: "10.0.0.1:8080",
			host:    "10.0.0.1",
			port:    "8080",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveExecTemplate(tt.input, tt.address, tt.host, tt.port)
			if got != tt.want {
				t.Errorf("resolveExecTemplate(%q, %q, %q, %q) = %q, want %q",
					tt.input, tt.address, tt.host, tt.port, got, tt.want)
			}
		})
	}
}

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
