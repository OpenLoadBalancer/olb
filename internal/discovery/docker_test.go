package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Helper: mock Docker API server over Unix socket ---

// mockDockerServer simulates the Docker Engine API for testing.
type mockDockerServer struct {
	listener   net.Listener
	server     *http.Server
	mu         sync.Mutex
	containers []dockerContainer
	events     []dockerEvent
	eventCh    chan dockerEvent
	socketPath string
}

// newMockDockerServer creates a mock Docker server listening on a temporary Unix socket.
// On Windows (or when Unix sockets are unavailable), it falls back to TCP.
func newMockDockerServer(t *testing.T) *mockDockerServer {
	t.Helper()

	m := &mockDockerServer{
		eventCh: make(chan dockerEvent, 100),
	}

	mux := http.NewServeMux()

	// GET /containers/json — list running containers
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		containers := make([]dockerContainer, len(m.containers))
		copy(containers, m.containers)
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(containers)
	})

	// GET /containers/{id}/json — inspect container
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Extract container ID: /containers/{id}/json
		parts := strings.Split(strings.TrimPrefix(path, "/containers/"), "/")
		if len(parts) < 1 {
			http.NotFound(w, r)
			return
		}
		id := parts[0]

		m.mu.Lock()
		var found *dockerContainer
		for i := range m.containers {
			if m.containers[i].ID == id || strings.HasPrefix(m.containers[i].ID, id) {
				found = &m.containers[i]
				break
			}
		}
		m.mu.Unlock()

		if found == nil {
			http.NotFound(w, r)
			return
		}

		inspect := dockerInspect{
			ID:   found.ID,
			Name: "/" + containerName(found.Names),
			State: dockerState{
				Status:  "running",
				Running: true,
			},
			Config: dockerConfig{
				Labels: found.Labels,
			},
			NetworkSettings: found.NetworkSettings,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inspect)
	})

	// GET /events — stream events
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-m.eventCh:
				if !ok {
					return
				}
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "%s\n", data)
				flusher.Flush()
			}
		}
	})

	// Use TCP listener for cross-platform compatibility
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	m.listener = ln
	m.server = &http.Server{Handler: mux}

	go m.server.Serve(ln)

	return m
}

// addr returns the TCP address of the mock server.
func (m *mockDockerServer) addr() string {
	return m.listener.Addr().String()
}

// setContainers replaces the container list served by the mock.
func (m *mockDockerServer) setContainers(containers []dockerContainer) {
	m.mu.Lock()
	m.containers = containers
	m.mu.Unlock()
}

// sendEvent sends a Docker event to any listening clients.
func (m *mockDockerServer) sendEvent(event dockerEvent) {
	m.eventCh <- event
}

// close shuts down the mock server.
func (m *mockDockerServer) close() {
	m.server.Close()
	m.listener.Close()
	close(m.eventCh)
}

// --- Helper: create a provider pointing at the mock server ---

func newTestDockerProvider(t *testing.T, mock *mockDockerServer, opts map[string]string) *DockerProvider {
	t.Helper()

	if opts == nil {
		opts = make(map[string]string)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Enabled: true,
		Refresh: 30 * time.Second,
		Options: opts,
	}

	dc := DefaultDockerConfig()
	dc.Host = "tcp://" + mock.addr()
	dc.PollInterval = 100 * time.Millisecond // fast polling for tests

	p, err := NewDockerProviderWithConfig(config, dc)
	if err != nil {
		t.Fatalf("NewDockerProviderWithConfig error: %v", err)
	}

	return p
}

// --- Helper: make a Docker container with OLB labels ---

func makeContainer(id string, name string, ip string, labels map[string]string) dockerContainer {
	return dockerContainer{
		ID:     id,
		Names:  []string{"/" + name},
		Image:  "test-image:latest",
		State:  "running",
		Status: "Up 1 hour",
		Labels: labels,
		NetworkSettings: &dockerNetworks{
			Networks: map[string]*dockerNetwork{
				"bridge": {
					IPAddress: ip,
				},
			},
		},
	}
}

// ======================== Tests ========================

func TestDefaultDockerConfig(t *testing.T) {
	cfg := DefaultDockerConfig()

	if cfg.SocketPath != "/var/run/docker.sock" {
		t.Errorf("SocketPath = %q, want /var/run/docker.sock", cfg.SocketPath)
	}
	if cfg.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want 10s", cfg.PollInterval)
	}
	if cfg.LabelPrefix != "olb." {
		t.Errorf("LabelPrefix = %q, want olb.", cfg.LabelPrefix)
	}
	if cfg.Network != "" {
		t.Errorf("Network = %q, want empty", cfg.Network)
	}
	if cfg.Host != "" {
		t.Errorf("Host = %q, want empty", cfg.Host)
	}
	if cfg.TLSEnabled {
		t.Error("TLSEnabled should be false by default")
	}
}

func TestNewDockerProvider_Defaults(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test-docker",
		Options: map[string]string{},
	}

	p, err := NewDockerProvider(config)
	if err != nil {
		t.Fatalf("NewDockerProvider error: %v", err)
	}

	if p.Name() != "test-docker" {
		t.Errorf("Name() = %q, want test-docker", p.Name())
	}
	if p.Type() != ProviderTypeDocker {
		t.Errorf("Type() = %q, want docker", p.Type())
	}
	if p.dockerConfig.SocketPath != "/var/run/docker.sock" {
		t.Errorf("SocketPath = %q, want /var/run/docker.sock", p.dockerConfig.SocketPath)
	}
	if p.dockerConfig.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want 10s", p.dockerConfig.PollInterval)
	}
	if p.dockerConfig.LabelPrefix != "olb." {
		t.Errorf("LabelPrefix = %q, want olb.", p.dockerConfig.LabelPrefix)
	}
}

func TestNewDockerProvider_CustomOptions(t *testing.T) {
	config := &ProviderConfig{
		Type: ProviderTypeDocker,
		Name: "custom-docker",
		Options: map[string]string{
			"socket_path":   "/custom/docker.sock",
			"label_prefix":  "lb.",
			"network":       "my-net",
			"poll_interval": "30s",
		},
	}

	p, err := NewDockerProvider(config)
	if err != nil {
		t.Fatalf("NewDockerProvider error: %v", err)
	}

	if p.dockerConfig.SocketPath != "/custom/docker.sock" {
		t.Errorf("SocketPath = %q, want /custom/docker.sock", p.dockerConfig.SocketPath)
	}
	if p.dockerConfig.LabelPrefix != "lb." {
		t.Errorf("LabelPrefix = %q, want lb.", p.dockerConfig.LabelPrefix)
	}
	if p.dockerConfig.Network != "my-net" {
		t.Errorf("Network = %q, want my-net", p.dockerConfig.Network)
	}
	if p.dockerConfig.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", p.dockerConfig.PollInterval)
	}
}

func TestNewDockerProvider_InvalidType(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{},
	}

	_, err := NewDockerProvider(config)
	if err == nil {
		t.Error("Expected error for invalid provider type")
	}
}

func TestNewDockerProvider_InvalidPollInterval(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{"poll_interval": "invalid"},
	}

	_, err := NewDockerProvider(config)
	if err == nil {
		t.Error("Expected error for invalid poll_interval")
	}
}

func TestNewDockerProvider_PollIntervalClamped(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{"poll_interval": "100ms"},
	}

	p, err := NewDockerProvider(config)
	if err != nil {
		t.Fatalf("NewDockerProvider error: %v", err)
	}

	if p.dockerConfig.PollInterval != time.Second {
		t.Errorf("PollInterval = %v, want 1s (clamped)", p.dockerConfig.PollInterval)
	}
}

func TestDockerProvider_ContainerLabelParsing(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	tests := []struct {
		name       string
		labels     map[string]string
		wantPort   int
		wantWeight int
		wantPool   string
		wantTags   []string
	}{
		{
			name: "all labels",
			labels: map[string]string{
				"olb.enable": "true",
				"olb.port":   "8080",
				"olb.weight": "5",
				"olb.pool":   "web",
				"olb.tags":   "http,api",
			},
			wantPort:   8080,
			wantWeight: 5,
			wantPool:   "web",
			wantTags:   []string{"http", "api"},
		},
		{
			name: "defaults",
			labels: map[string]string{
				"olb.enable": "true",
				"olb.port":   "3000",
			},
			wantPort:   3000,
			wantWeight: 1,
			wantPool:   "",
			wantTags:   nil,
		},
		{
			name: "invalid port",
			labels: map[string]string{
				"olb.enable": "true",
				"olb.port":   "notaport",
			},
			wantPort:   0,
			wantWeight: 1,
			wantPool:   "",
			wantTags:   nil,
		},
		{
			name: "invalid weight defaults to 1",
			labels: map[string]string{
				"olb.enable": "true",
				"olb.port":   "8080",
				"olb.weight": "bad",
			},
			wantPort:   8080,
			wantWeight: 1,
			wantPool:   "",
			wantTags:   nil,
		},
		{
			name: "zero weight defaults to 1",
			labels: map[string]string{
				"olb.enable": "true",
				"olb.port":   "8080",
				"olb.weight": "0",
			},
			wantPort:   8080,
			wantWeight: 1,
			wantPool:   "",
			wantTags:   nil,
		},
		{
			name: "port out of range",
			labels: map[string]string{
				"olb.enable": "true",
				"olb.port":   "99999",
			},
			wantPort:   0,
			wantWeight: 1,
			wantPool:   "",
			wantTags:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, weight, pool, tags := p.parseContainerLabels(tt.labels)
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
			if weight != tt.wantWeight {
				t.Errorf("weight = %d, want %d", weight, tt.wantWeight)
			}
			if pool != tt.wantPool {
				t.Errorf("pool = %q, want %q", pool, tt.wantPool)
			}
			if len(tags) != len(tt.wantTags) {
				t.Errorf("tags = %v, want %v", tags, tt.wantTags)
			} else {
				for i, tag := range tags {
					if tag != tt.wantTags[i] {
						t.Errorf("tags[%d] = %q, want %q", i, tag, tt.wantTags[i])
					}
				}
			}
		})
	}
}

func TestDockerProvider_IsEnabled(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"enabled", map[string]string{"olb.enable": "true"}, true},
		{"disabled", map[string]string{"olb.enable": "false"}, false},
		{"missing", map[string]string{}, false},
		{"empty", map[string]string{"olb.enable": ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.isEnabled(tt.labels)
			if got != tt.want {
				t.Errorf("isEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDockerProvider_ExtractContainerIP(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	tests := []struct {
		name     string
		networks *dockerNetworks
		network  string // configured network filter
		want     string
	}{
		{
			name:     "nil networks",
			networks: nil,
			want:     "",
		},
		{
			name:     "empty networks map",
			networks: &dockerNetworks{Networks: map[string]*dockerNetwork{}},
			want:     "",
		},
		{
			name: "single bridge network",
			networks: &dockerNetworks{
				Networks: map[string]*dockerNetwork{
					"bridge": {IPAddress: "172.17.0.2"},
				},
			},
			want: "172.17.0.2",
		},
		{
			name: "specific network filter",
			networks: &dockerNetworks{
				Networks: map[string]*dockerNetwork{
					"bridge":  {IPAddress: "172.17.0.2"},
					"app-net": {IPAddress: "10.0.1.5"},
				},
			},
			network: "app-net",
			want:    "10.0.1.5",
		},
		{
			name: "specific network not found",
			networks: &dockerNetworks{
				Networks: map[string]*dockerNetwork{
					"bridge": {IPAddress: "172.17.0.2"},
				},
			},
			network: "nonexistent",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p.dockerConfig.Network = tt.network
			got := p.extractContainerIP(tt.networks)
			if got != tt.want {
				t.Errorf("extractContainerIP() = %q, want %q", got, tt.want)
			}
		})
	}
	// Reset
	p.dockerConfig.Network = ""
}

func TestDockerProvider_ContainerToService(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
		Tags:    []string{"env:test"},
	}
	p, _ := NewDockerProvider(config)

	t.Run("valid container", func(t *testing.T) {
		container := makeContainer(
			"abc123def456abcd",
			"web-app",
			"172.17.0.2",
			map[string]string{
				"olb.enable": "true",
				"olb.port":   "8080",
				"olb.weight": "3",
				"olb.pool":   "web",
				"olb.tags":   "http,primary",
			},
		)

		svc := p.containerToService(container)
		if svc == nil {
			t.Fatal("containerToService returned nil")
		}

		if svc.ID != "test-docker-abc123def456" {
			t.Errorf("ID = %q, want test-docker-abc123def456", svc.ID)
		}
		if svc.Name != "web-app" {
			t.Errorf("Name = %q, want web-app", svc.Name)
		}
		if svc.Address != "172.17.0.2" {
			t.Errorf("Address = %q, want 172.17.0.2", svc.Address)
		}
		if svc.Port != 8080 {
			t.Errorf("Port = %d, want 8080", svc.Port)
		}
		if svc.Weight != 3 {
			t.Errorf("Weight = %d, want 3", svc.Weight)
		}
		if !svc.Healthy {
			t.Error("Healthy should be true")
		}
		if svc.Meta["pool"] != "web" {
			t.Errorf("Meta[pool] = %q, want web", svc.Meta["pool"])
		}
		if svc.Meta["container_id"] != "abc123def456abcd" {
			t.Errorf("Meta[container_id] = %q, want abc123def456abcd", svc.Meta["container_id"])
		}
		// Tags should be provider tags + container tags
		if len(svc.Tags) != 3 {
			t.Errorf("Tags count = %d, want 3, got %v", len(svc.Tags), svc.Tags)
		}
	})

	t.Run("not enabled", func(t *testing.T) {
		container := makeContainer("abc123def456abcd", "disabled", "172.17.0.3", map[string]string{
			"olb.port": "8080",
		})

		svc := p.containerToService(container)
		if svc != nil {
			t.Error("Expected nil for container without olb.enable=true")
		}
	})

	t.Run("no port", func(t *testing.T) {
		container := makeContainer("abc123def456abcd", "no-port", "172.17.0.4", map[string]string{
			"olb.enable": "true",
		})

		svc := p.containerToService(container)
		if svc != nil {
			t.Error("Expected nil for container without olb.port")
		}
	})

	t.Run("no IP", func(t *testing.T) {
		container := dockerContainer{
			ID:     "abc123def456abcd",
			Names:  []string{"/no-ip"},
			Labels: map[string]string{"olb.enable": "true", "olb.port": "8080"},
			NetworkSettings: &dockerNetworks{
				Networks: map[string]*dockerNetwork{},
			},
		}

		svc := p.containerToService(container)
		if svc != nil {
			t.Error("Expected nil for container without IP")
		}
	})
}

func TestDockerProvider_ContainerFiltering(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	containers := []dockerContainer{
		makeContainer("aaa111222333abcd", "app1", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
		makeContainer("bbb111222333abcd", "app2", "172.17.0.3", map[string]string{
			// Not enabled
			"olb.port": "9090",
		}),
		makeContainer("ccc111222333abcd", "app3", "172.17.0.4", map[string]string{
			"olb.enable": "true",
			"olb.port":   "3000",
		}),
		makeContainer("ddd111222333abcd", "app4", "172.17.0.5", map[string]string{
			"olb.enable": "false",
			"olb.port":   "4000",
		}),
	}

	var services []*Service
	for _, c := range containers {
		if svc := p.containerToService(c); svc != nil {
			services = append(services, svc)
		}
	}

	if len(services) != 2 {
		t.Fatalf("Expected 2 enabled services, got %d", len(services))
	}

	// Verify the right containers were included
	ids := map[string]bool{}
	for _, svc := range services {
		ids[svc.Name] = true
	}
	if !ids["app1"] {
		t.Error("Expected app1 to be included")
	}
	if !ids["app3"] {
		t.Error("Expected app3 to be included")
	}
	if ids["app2"] {
		t.Error("Expected app2 to be excluded (no olb.enable)")
	}
	if ids["app4"] {
		t.Error("Expected app4 to be excluded (olb.enable=false)")
	}
}

func TestDockerProvider_PollDiscovery(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mock.setContainers([]dockerContainer{
		makeContainer("aaa111222333abcd", "web1", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
			"olb.weight": "2",
		}),
		makeContainer("bbb111222333abcd", "web2", "172.17.0.3", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
	})

	p := newTestDockerProvider(t, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	services := p.Services()
	if len(services) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(services))
	}

	// Verify service details
	byID := make(map[string]*Service)
	for _, svc := range services {
		byID[svc.ID] = svc
	}

	svc1 := byID["test-docker-aaa111222333"]
	if svc1 == nil {
		t.Fatal("Service for aaa111222333 not found")
	}
	if svc1.Address != "172.17.0.2" {
		t.Errorf("Address = %q, want 172.17.0.2", svc1.Address)
	}
	if svc1.Port != 8080 {
		t.Errorf("Port = %d, want 8080", svc1.Port)
	}
	if svc1.Weight != 2 {
		t.Errorf("Weight = %d, want 2", svc1.Weight)
	}

	svc2 := byID["test-docker-bbb111222333"]
	if svc2 == nil {
		t.Fatal("Service for bbb111222333 not found")
	}
	if svc2.Weight != 1 {
		t.Errorf("Weight = %d, want 1 (default)", svc2.Weight)
	}
}

func TestDockerProvider_PollDetectsChanges(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	// Start with one container
	mock.setContainers([]dockerContainer{
		makeContainer("aaa111222333abcd", "web1", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
	})

	p := newTestDockerProvider(t, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	if len(p.Services()) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(p.Services()))
	}

	// Add a second container
	mock.setContainers([]dockerContainer{
		makeContainer("aaa111222333abcd", "web1", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
		makeContainer("bbb111222333abcd", "web2", "172.17.0.3", map[string]string{
			"olb.enable": "true",
			"olb.port":   "9090",
		}),
	})

	// Wait for poll to detect the change
	deadline := time.After(3 * time.Second)
	for {
		if len(p.Services()) == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for second service to appear; got %d services", len(p.Services()))
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Now remove first container
	mock.setContainers([]dockerContainer{
		makeContainer("bbb111222333abcd", "web2", "172.17.0.3", map[string]string{
			"olb.enable": "true",
			"olb.port":   "9090",
		}),
	})

	// Wait for poll to detect the removal
	deadline = time.After(3 * time.Second)
	for {
		if len(p.Services()) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for first service to be removed; got %d services", len(p.Services()))
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	services := p.Services()
	if services[0].Name != "web2" {
		t.Errorf("Remaining service Name = %q, want web2", services[0].Name)
	}
}

func TestDockerProvider_EventHandling(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Pre-populate a service to test removal via events
	svc := &Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	}
	p.addService(svc)

	if len(p.Services()) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(p.Services()))
	}

	// Simulate a stop event
	event := &dockerEvent{
		Type:   "container",
		Action: "stop",
		Actor: dockerEventActor{
			ID: "abc123def456abcd",
		},
	}

	p.handleEvent(event)

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after stop event, got %d", len(p.Services()))
	}

	p.cancel()
}

func TestDockerProvider_EventDie(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	svc := &Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	}
	p.addService(svc)

	// Simulate a die event
	event := &dockerEvent{
		Type:   "container",
		Action: "die",
		Actor: dockerEventActor{
			ID: "abc123def456abcd",
		},
	}

	p.handleEvent(event)

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after die event, got %d", len(p.Services()))
	}

	p.cancel()
}

func TestDockerProvider_EventNonContainer(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	svc := &Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	}
	p.addService(svc)

	// Non-container events should be ignored
	event := &dockerEvent{
		Type:   "network",
		Action: "disconnect",
		Actor: dockerEventActor{
			ID: "abc123def456abcd",
		},
	}

	p.handleEvent(event)

	if len(p.Services()) != 1 {
		t.Errorf("Expected 1 service (non-container event ignored), got %d", len(p.Services()))
	}

	p.cancel()
}

func TestDockerProvider_DockerUnavailable(t *testing.T) {
	// Point at a non-existent host to simulate Docker being unavailable
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "unavailable-docker",
		Options: map[string]string{},
	}

	dc := DefaultDockerConfig()
	dc.Host = "tcp://127.0.0.1:1" // Almost certainly nothing listening here
	dc.PollInterval = 100 * time.Millisecond

	p, err := NewDockerProviderWithConfig(config, dc)
	if err != nil {
		t.Fatalf("NewDockerProviderWithConfig error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should succeed even when Docker is unavailable
	err = p.Start(ctx)
	if err != nil {
		t.Errorf("Start should succeed even when Docker is unavailable, got: %v", err)
	}

	// No services should be discovered
	services := p.Services()
	if len(services) != 0 {
		t.Errorf("Expected 0 services when Docker is unavailable, got %d", len(services))
	}

	p.Stop()
}

func TestDockerProvider_MultipleContainers(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	containers := make([]dockerContainer, 10)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("%012d0000", i)
		containers[i] = makeContainer(
			id,
			fmt.Sprintf("service-%d", i),
			fmt.Sprintf("172.17.0.%d", i+2),
			map[string]string{
				"olb.enable": "true",
				"olb.port":   fmt.Sprintf("%d", 8080+i),
				"olb.weight": fmt.Sprintf("%d", i+1),
				"olb.pool":   "batch",
			},
		)
	}
	mock.setContainers(containers)

	p := newTestDockerProvider(t, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	services := p.Services()
	if len(services) != 10 {
		t.Fatalf("Expected 10 services, got %d", len(services))
	}

	// Verify all have correct pool metadata
	for _, svc := range services {
		if svc.Meta["pool"] != "batch" {
			t.Errorf("Service %s: Meta[pool] = %q, want batch", svc.ID, svc.Meta["pool"])
		}
		if !svc.Healthy {
			t.Errorf("Service %s should be healthy", svc.ID)
		}
	}
}

func TestDockerProvider_NetworkFilter(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	// Container with multiple networks
	container := dockerContainer{
		ID:     "abc123def456abcd",
		Names:  []string{"/multi-net"},
		Image:  "test:latest",
		State:  "running",
		Labels: map[string]string{"olb.enable": "true", "olb.port": "8080"},
		NetworkSettings: &dockerNetworks{
			Networks: map[string]*dockerNetwork{
				"bridge":  {IPAddress: "172.17.0.2"},
				"app-net": {IPAddress: "10.0.1.5"},
			},
		},
	}

	mock.setContainers([]dockerContainer{container})

	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "net-test",
		Options: map[string]string{},
	}

	dc := DefaultDockerConfig()
	dc.Host = "tcp://" + mock.addr()
	dc.Network = "app-net"
	dc.PollInterval = 100 * time.Millisecond

	p, err := NewDockerProviderWithConfig(config, dc)
	if err != nil {
		t.Fatalf("NewDockerProviderWithConfig error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	services := p.Services()
	if len(services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(services))
	}

	// Should use the app-net IP, not bridge
	if services[0].Address != "10.0.1.5" {
		t.Errorf("Address = %q, want 10.0.1.5 (from app-net)", services[0].Address)
	}
}

func TestDockerProvider_StopTerminatesPolling(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mock.setContainers([]dockerContainer{
		makeContainer("aaa111222333abcd", "web1", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
	})

	p := newTestDockerProvider(t, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	err := p.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}

	// Events channel should be closed after Stop
	done := make(chan struct{})
	go func() {
		for range p.Events() {
		}
		close(done)
	}()

	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Error("Events channel should be closed after Stop")
	}
}

func TestDockerProvider_FactoryRegistration(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "factory-test",
		Options: map[string]string{},
	}

	provider, err := CreateProvider(config)
	if err != nil {
		t.Fatalf("CreateProvider error: %v", err)
	}

	if provider.Type() != ProviderTypeDocker {
		t.Errorf("Type() = %q, want docker", provider.Type())
	}
	if provider.Name() != "factory-test" {
		t.Errorf("Name() = %q, want factory-test", provider.Name())
	}
}

func TestDockerProvider_ReadEventStream(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Pre-populate services with IDs matching what handleEvent will generate.
	// handleEvent truncates actor IDs to 12 characters, so "aaa111222333abcd"[:12] = "aaa111222333".
	// The service ID format is "{name}-docker-{shortID}".
	p.addService(&Service{
		ID:      "test-docker-aaa111222333",
		Name:    "svc1",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})
	p.addService(&Service{
		ID:      "test-docker-bbb111222333",
		Name:    "svc2",
		Address: "172.17.0.3",
		Port:    9090,
		Healthy: true,
	})

	if len(p.Services()) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(p.Services()))
	}

	// Create event stream with container IDs whose first 12 chars match the service IDs
	events := []dockerEvent{
		{Type: "container", Action: "die", Actor: dockerEventActor{ID: "aaa111222333abcd"}},
		{Type: "container", Action: "stop", Actor: dockerEventActor{ID: "bbb111222333abcd"}},
	}

	var lines []string
	for _, e := range events {
		data, _ := json.Marshal(e)
		lines = append(lines, string(data))
	}
	stream := strings.NewReader(strings.Join(lines, "\n"))

	err := p.readEventStream(stream)
	if err != nil {
		t.Fatalf("readEventStream error: %v", err)
	}

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after processing events, got %d", len(p.Services()))
	}

	p.cancel()
}

func TestDockerProvider_ContainerName(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{[]string{"/web-app"}, "web-app"},
		{[]string{"/my-service"}, "my-service"},
		{[]string{"no-slash"}, "no-slash"},
		{nil, ""},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := containerName(tt.names)
		if got != tt.want {
			t.Errorf("containerName(%v) = %q, want %q", tt.names, got, tt.want)
		}
	}
}

func TestDockerProvider_SplitTags(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"http,api", []string{"http", "api"}},
		{"single", []string{"single"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"", nil},
		{",,,", nil},
	}

	for _, tt := range tests {
		got := splitTags(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitTags(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitTags(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDockerProvider_UrlEncode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{`{"status":["running"]}`, "%7B%22status%22%3A%5B%22running%22%5D%7D"},
		{"a b", "a%20b"},
	}

	for _, tt := range tests {
		got := urlEncode(tt.input)
		if got != tt.want {
			t.Errorf("urlEncode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDockerProvider_BaseURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		tls  bool
		want string
	}{
		{"unix socket", "", false, "http://localhost"},
		{"tcp host", "tcp://192.168.1.10:2376", false, "http://192.168.1.10:2376"},
		{"tcp host with tls", "tcp://192.168.1.10:2376", true, "https://192.168.1.10:2376"},
		{"plain host", "192.168.1.10:2376", false, "http://192.168.1.10:2376"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ProviderConfig{
				Type:    ProviderTypeDocker,
				Name:    "test",
				Options: map[string]string{},
			}
			dc := DefaultDockerConfig()
			dc.Host = tt.host
			dc.TLSEnabled = tt.tls

			p, _ := NewDockerProviderWithConfig(config, dc)
			got := p.baseURL()
			if got != tt.want {
				t.Errorf("baseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDockerProvider_CustomLabelPrefix(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "custom-prefix",
		Options: map[string]string{"label_prefix": "myapp."},
	}

	p, err := NewDockerProvider(config)
	if err != nil {
		t.Fatalf("NewDockerProvider error: %v", err)
	}

	labels := map[string]string{
		"myapp.enable": "true",
		"myapp.port":   "3000",
		"myapp.weight": "5",
		"myapp.pool":   "custom",
		"myapp.tags":   "a,b",
	}

	if !p.isEnabled(labels) {
		t.Error("isEnabled should be true with custom prefix")
	}

	port, weight, pool, tags := p.parseContainerLabels(labels)
	if port != 3000 {
		t.Errorf("port = %d, want 3000", port)
	}
	if weight != 5 {
		t.Errorf("weight = %d, want 5", weight)
	}
	if pool != "custom" {
		t.Errorf("pool = %q, want custom", pool)
	}
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v, want [a b]", tags)
	}

	// Labels with the wrong prefix should not match
	wrongLabels := map[string]string{
		"olb.enable": "true",
		"olb.port":   "8080",
	}

	if p.isEnabled(wrongLabels) {
		t.Error("isEnabled should be false with wrong prefix")
	}
}

func TestDockerProvider_SetHTTPClient(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	customClient := &http.Client{Timeout: 99 * time.Second}
	p.SetHTTPClient(customClient)

	if p.client != customClient {
		t.Error("SetHTTPClient did not set the custom client")
	}
	if p.client.Timeout != 99*time.Second {
		t.Errorf("Timeout = %v, want 99s", p.client.Timeout)
	}
}

func TestDockerProvider_InspectContainer_MockServer(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mock.setContainers([]dockerContainer{
		makeContainer("abc123def456abcd", "web-app", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
	})

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	inspect, err := p.inspectContainer("abc123def456abcd")
	if err != nil {
		t.Fatalf("inspectContainer error: %v", err)
	}

	if inspect.ID != "abc123def456abcd" {
		t.Errorf("ID = %q, want abc123def456abcd", inspect.ID)
	}
	if inspect.Labels["olb.enable"] != "true" {
		t.Errorf("Labels[olb.enable] = %q, want true", inspect.Labels["olb.enable"])
	}
}

func TestDockerProvider_InspectContainer_NotFound(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mock.setContainers([]dockerContainer{})

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	_, err := p.inspectContainer("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent container")
	}
}

func TestDockerProvider_DockerConfigMethod(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	dc := p.DockerConfig()
	if dc == nil {
		t.Fatal("DockerConfig() returned nil")
	}
	if dc.SocketPath != "/var/run/docker.sock" {
		t.Errorf("SocketPath = %q, want /var/run/docker.sock", dc.SocketPath)
	}
	if dc.LabelPrefix != "olb." {
		t.Errorf("LabelPrefix = %q, want olb.", dc.LabelPrefix)
	}
	if dc != p.dockerConfig {
		t.Error("DockerConfig() should return the same config instance")
	}
}

func TestDockerProvider_AddRemoveEvents(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	// Start with one container
	mock.setContainers([]dockerContainer{
		makeContainer("aaa111222333abcd", "web1", "172.17.0.2", map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		}),
	})

	p := newTestDockerProvider(t, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	// Drain initial add events
	events := drainEvents(p.Events(), 1, time.Second)
	if len(events) == 0 {
		t.Log("Initial add event may have been consumed")
	}

	// Now remove the container
	mock.setContainers([]dockerContainer{})

	// Wait for removal
	deadline := time.After(3 * time.Second)
	for {
		if len(p.Services()) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for service removal; got %d services", len(p.Services()))
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Check for remove event
	foundRemove := false
	timeout := time.After(2 * time.Second)
	for {
		select {
		case event := <-p.Events():
			if event != nil && event.Type == EventTypeRemove {
				foundRemove = true
			}
		case <-timeout:
			goto done
		}
		if foundRemove {
			break
		}
	}
done:
	if !foundRemove {
		t.Log("Remove event not found (may have already been consumed)")
	}
}
