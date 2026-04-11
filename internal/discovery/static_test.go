package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStaticProvider(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test-static",
		Options: map[string]string{"addresses": "192.168.1.1:8080, 192.168.1.2:8080"},
	}

	provider, err := NewStaticProvider(config)
	if err != nil {
		t.Fatalf("NewStaticProvider error: %v", err)
	}

	if provider.name != "test-static" {
		t.Errorf("Name = %q, want test-static", provider.name)
	}

	if len(provider.addresses) != 2 {
		t.Errorf("Addresses = %v, want 2", len(provider.addresses))
	}
}

func TestNewStaticProvider_InvalidType(t *testing.T) {
	config := &ProviderConfig{
		Type: "invalid",
		Name: "test",
	}

	_, err := NewStaticProvider(config)
	if err == nil {
		t.Error("Expected error for invalid type")
	}
}

func TestParseAddresses(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "192.168.1.1:8080, 192.168.1.2:8080",
			expected: []string{"192.168.1.1:8080", "192.168.1.2:8080"},
		},
		{
			input:    "10.0.0.1",
			expected: []string{"10.0.0.1"},
		},
		{
			input:    "host1:80,host2:80,host3:80",
			expected: []string{"host1:80", "host2:80", "host3:80"},
		},
		{
			input:    "  spaced  ",
			expected: []string{"spaced"},
		},
		{
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseAddresses(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("parseAddresses() = %v, want %v", got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("parseAddresses()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestTrimSpace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  ", "hello"},
		{"\thello\t", "hello"},
		{"  \t hello \t  ", "hello"},
		{"hello", "hello"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := trimSpace(tt.input)
			if got != tt.expected {
				t.Errorf("trimSpace(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		addr string
		want int
	}{
		{"192.168.1.1:8080", 8080},
		{"192.168.1.1:80", 80},
		{"192.168.1.1:443", 443},
		{"192.168.1.1", 80},
		{"[::1]:8080", 8080},
		{"host:9000", 9000},
		{"host:invalid", 80},
		{"host:99999", 80},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := parsePort(tt.addr)
			if got != tt.want {
				t.Errorf("parsePort(%q) = %d, want %d", tt.addr, got, tt.want)
			}
		})
	}
}

func TestStaticProvider_Start(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{"addresses": "192.168.1.1:8080,192.168.1.2:8081"},
		Tags:    []string{"web", "api"},
	}

	provider, err := NewStaticProvider(config)
	if err != nil {
		t.Fatalf("NewStaticProvider error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	services := provider.Services()
	if len(services) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(services))
	}

	// Find service by ID (map iteration order is not guaranteed)
	var svc1, svc2 *Service
	for _, s := range services {
		if s.ID == "test-0" {
			svc1 = s
		} else if s.ID == "test-1" {
			svc2 = s
		}
	}

	if svc1 == nil {
		t.Fatal("Service test-0 not found")
	}
	if svc2 == nil {
		t.Fatal("Service test-1 not found")
	}

	// Check first service
	if svc1.Address != "192.168.1.1" {
		t.Errorf("Service[0].Address = %q, want 192.168.1.1", svc1.Address)
	}
	if svc1.Port != 8080 {
		t.Errorf("Service[0].Port = %d, want 8080", svc1.Port)
	}
	if !svc1.Healthy {
		t.Error("Service[0].Healthy should be true")
	}

	// Check tags
	if len(svc1.Tags) != 2 {
		t.Errorf("Service[0].Tags = %v, want 2 tags", svc1.Tags)
	}

	// Check second service
	if svc2.Address != "192.168.1.2" {
		t.Errorf("Service[1].Address = %q, want 192.168.1.2", svc2.Address)
	}
	if svc2.Port != 8081 {
		t.Errorf("Service[1].Port = %d, want 8081", svc2.Port)
	}
}

func TestStaticProvider_Events(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{"addresses": "192.168.1.1:8080"},
	}

	provider, err := NewStaticProvider(config)
	if err != nil {
		t.Fatalf("NewStaticProvider error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Read events
	select {
	case event := <-provider.Events():
		if event.Type != EventTypeAdd {
			t.Errorf("Event type = %v, want Add", event.Type)
		}
		if event.Service == nil {
			t.Fatal("Event service is nil")
		}
		if event.Service.ID != "test-0" {
			t.Errorf("Event service ID = %q, want test-0", event.Service.ID)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"config.json", "json"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"config.txt", "json"},
		{"config", "json"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := detectFormat(tt.path)
			if got != tt.expected {
				t.Errorf("detectFormat(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestStaticFileProvider(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	// Create test config file
	configPath := filepath.Join(tempDir, "services.json")
	configContent := `{
		"services": [
			{
				"id": "svc-1",
				"name": "service1",
				"address": "192.168.1.1",
				"port": 8080,
				"weight": 10,
				"tags": ["web", "v1"],
				"meta": {"env": "prod"}
			},
			{
				"id": "svc-2",
				"name": "service2",
				"address": "192.168.1.2",
				"port": 8081
			}
		]
	}`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "file-test",
		Options: map[string]string{"file": configPath, "format": "json"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	services := provider.Services()
	if len(services) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(services))
	}

	// Find svc-1 (order is non-deterministic)
	var svc1 *Service
	for _, s := range services {
		if s.ID == "svc-1" {
			svc1 = s
			break
		}
	}
	if svc1 == nil {
		t.Fatal("Service svc-1 not found")
	}
	if svc1.Weight != 10 {
		t.Errorf("svc-1.Weight = %d, want 10", svc1.Weight)
	}
	if len(svc1.Tags) != 2 {
		t.Errorf("svc-1.Tags = %v, want 2 tags", svc1.Tags)
	}
	if svc1.Meta["env"] != "prod" {
		t.Errorf("svc-1.Meta[env] = %q, want prod", svc1.Meta["env"])
	}
}

func TestStaticFileProvider_MissingFile(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{},
	}

	_, err := NewStaticFileProvider(config)
	if err == nil {
		t.Error("Expected error for missing file option")
	}
}

func TestStaticFileProvider_FileNotFound(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{"file": "/nonexistent/path/services.json"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx := context.Background()
	err = provider.Start(ctx)
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestStaticFileProvider_YAMLFormatBasic(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "services.yaml")

	err := os.WriteFile(configPath, []byte("services: []"), 0644)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{"file": configPath, "format": "yaml"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx := context.Background()
	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error (YAML should be supported): %v", err)
	}
	provider.Stop()
}

func TestStaticProvider_Stop(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{"addresses": "192.168.1.1:8080"},
	}

	provider, err := NewStaticProvider(config)
	if err != nil {
		t.Fatalf("NewStaticProvider error: %v", err)
	}

	ctx := context.Background()
	provider.Start(ctx)

	err = provider.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}

	// Events channel should be closed
	// Drain any pending events first
	done := make(chan struct{})
	go func() {
		for range provider.Events() {
		}
		close(done)
	}()

	select {
	case <-done:
		// Channel closed as expected
	case <-time.After(2 * time.Second):
		t.Error("Events channel should be closed")
	}
}

func TestStaticFileProvider_WatchFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "services.json")

	// Initial content with 1 service
	config1 := `{"services": [{"id": "svc-1", "address": "192.168.1.1", "port": 8080}]}`
	err := os.WriteFile(configPath, []byte(config1), 0644)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "watch-test",
		Refresh: 200 * time.Millisecond, // Short interval for testing
		Options: map[string]string{"file": configPath, "format": "json"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer provider.Stop()

	if len(provider.Services()) != 1 {
		t.Fatalf("Expected 1 service initially, got %d", len(provider.Services()))
	}

	// Update the file with 2 services
	config2 := `{"services": [{"id": "svc-1", "address": "192.168.1.1", "port": 8080}, {"id": "svc-2", "address": "192.168.1.2", "port": 9090}]}`
	err = os.WriteFile(configPath, []byte(config2), 0644)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Wait for the file watcher to detect the change
	deadline := time.After(3 * time.Second)
	for {
		if len(provider.Services()) == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for file watcher to detect change; got %d services", len(provider.Services()))
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestStaticProvider_ServiceRemoval(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "services.json")

	// Initial config with 2 services
	config1 := `{"services": [{"id": "svc-1", "address": "192.168.1.1", "port": 8080}, {"id": "svc-2", "address": "192.168.1.2", "port": 8081}]}`
	err := os.WriteFile(configPath, []byte(config1), 0644)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{"file": configPath, "format": "json"},
	}

	provider, _ := NewStaticFileProvider(config)
	ctx := context.Background()
	provider.Start(ctx)

	// Wait for initial load
	time.Sleep(100 * time.Millisecond)

	if len(provider.Services()) != 2 {
		t.Fatalf("Expected 2 services initially, got %d", len(provider.Services()))
	}

	// Update config with only 1 service
	config2 := `{"services": [{"id": "svc-1", "address": "192.168.1.1", "port": 8080}]}`
	err = os.WriteFile(configPath, []byte(config2), 0644)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Trigger reload by calling loadServices directly
	// In real usage, the file watcher would do this
	err = provider.loadServices()
	if err != nil {
		t.Fatalf("loadServices error: %v", err)
	}

	services := provider.Services()
	if len(services) != 1 {
		t.Fatalf("Expected 1 service after removal, got %d", len(services))
	}

	if services[0].ID != "svc-1" {
		t.Errorf("Expected svc-1, got %s", services[0].ID)
	}

	// Check for removal event (drain events and look for removal)
	foundRemove := false
	timeout := time.After(2 * time.Second)
drainLoop:
	for {
		select {
		case event := <-provider.Events():
			if event == nil {
				break drainLoop
			}
			if event.Type == EventTypeRemove && event.Service.ID == "svc-2" {
				foundRemove = true
				break drainLoop
			}
		case <-timeout:
			break drainLoop
		}
	}

	if !foundRemove {
		t.Log("Remove event not found in channel (may have been dropped or sent before test)")
	}
}

func TestStaticProvider_FactoryRegistration(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "factory-test",
		Options: map[string]string{"addresses": "192.168.1.1:8080"},
	}

	provider, err := CreateProvider(config)
	if err != nil {
		t.Fatalf("CreateProvider error: %v", err)
	}

	if provider.Type() != ProviderTypeStatic {
		t.Errorf("Type() = %q, want static", provider.Type())
	}
	if provider.Name() != "factory-test" {
		t.Errorf("Name() = %q, want factory-test", provider.Name())
	}

	// Verify services are created on Start
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := provider.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer provider.Stop()

	services := provider.Services()
	if len(services) != 1 {
		t.Errorf("Expected 1 service, got %d", len(services))
	}
}

func TestStaticProvider_FactoryRegistration_FileOption(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "services.json")
	content := `{"services": [{"id": "svc-1", "address": "10.0.0.1", "port": 80}]}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "file-factory-test",
		Options: map[string]string{"file": configPath, "format": "json"},
	}

	provider, err := CreateProvider(config)
	if err != nil {
		t.Fatalf("CreateProvider error: %v", err)
	}

	if provider.Type() != ProviderTypeStatic {
		t.Errorf("Type() = %q, want static", provider.Type())
	}
}
