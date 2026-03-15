package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultProviderConfig(t *testing.T) {
	config := DefaultProviderConfig()

	if config.Enabled != true {
		t.Error("Enabled should be true by default")
	}
	if config.Refresh != 30*time.Second {
		t.Errorf("Refresh = %v, want 30s", config.Refresh)
	}
	if config.HealthCheck != true {
		t.Error("HealthCheck should be true by default")
	}
}

func TestProviderConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *ProviderConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &ProviderConfig{
				Type: "static",
				Name: "test",
			},
			wantErr: false,
		},
		{
			name: "missing type",
			config: &ProviderConfig{
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			config: &ProviderConfig{
				Type: "static",
			},
			wantErr: true,
		},
		{
			name: "refresh too short",
			config: &ProviderConfig{
				Type:    "static",
				Name:    "test",
				Refresh: 100 * time.Millisecond,
			},
			wantErr: false, // Should be adjusted to 1s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProviderConfig_Validate_AdjustsRefresh(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Refresh: 100 * time.Millisecond,
	}

	err := config.Validate()
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}

	if config.Refresh != time.Second {
		t.Errorf("Refresh = %v, want 1s", config.Refresh)
	}
}

func TestService_FullAddress(t *testing.T) {
	service := &Service{
		Address: "192.168.1.1",
		Port:    8080,
	}

	addr := service.FullAddress()
	if addr != "192.168.1.1:8080" {
		t.Errorf("FullAddress() = %q, want 192.168.1.1:8080", addr)
	}
}

func TestNewManager(t *testing.T) {
	manager := NewManager()
	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	if manager.providers == nil {
		t.Error("providers map should not be nil")
	}
}

func TestManager_AddProvider(t *testing.T) {
	manager := NewManager()
	provider := &mockProvider{
		baseProvider: newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig()),
	}

	err := manager.AddProvider(provider)
	if err != nil {
		t.Errorf("AddProvider error: %v", err)
	}

	// Adding duplicate should fail
	err = manager.AddProvider(provider)
	if err == nil {
		t.Error("Expected error for duplicate provider")
	}
}

func TestManager_GetProvider(t *testing.T) {
	manager := NewManager()
	provider := &mockProvider{
		baseProvider: newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig()),
	}

	manager.AddProvider(provider)

	got, ok := manager.GetProvider("test")
	if !ok {
		t.Error("Expected to find provider")
	}
	if got.Name() != "test" {
		t.Errorf("Name = %q, want test", got.Name())
	}

	_, ok = manager.GetProvider("nonexistent")
	if ok {
		t.Error("Should not find nonexistent provider")
	}
}

func TestManager_Providers(t *testing.T) {
	manager := NewManager()

	// Add multiple providers
	for i := 0; i < 3; i++ {
		provider := &mockProvider{
			baseProvider: newBaseProvider(
				fmt.Sprintf("test-%d", i),
				ProviderTypeStatic,
				DefaultProviderConfig(),
			),
		}
		manager.AddProvider(provider)
	}

	providers := manager.Providers()
	if len(providers) != 3 {
		t.Errorf("Providers() returned %d providers, want 3", len(providers))
	}
}

func TestManager_RemoveProvider(t *testing.T) {
	manager := NewManager()
	provider := &mockProvider{
		baseProvider: newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig()),
	}

	manager.AddProvider(provider)

	err := manager.RemoveProvider("test")
	if err != nil {
		t.Errorf("RemoveProvider error: %v", err)
	}

	_, ok := manager.GetProvider("test")
	if ok {
		t.Error("Provider should have been removed")
	}

	// Removing nonexistent should fail
	err = manager.RemoveProvider("test")
	if err == nil {
		t.Error("Expected error for nonexistent provider")
	}
}

func TestManager_Start(t *testing.T) {
	manager := NewManager()
	provider := &mockProvider{
		baseProvider: newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig()),
	}

	manager.AddProvider(provider)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := manager.Start(ctx)
	if err != nil {
		t.Errorf("Start error: %v", err)
	}

	if !provider.started {
		t.Error("Provider should have been started")
	}
}

func TestManager_Stop(t *testing.T) {
	manager := NewManager()
	provider := &mockProvider{
		baseProvider: newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig()),
	}

	manager.AddProvider(provider)

	ctx := context.Background()
	manager.Start(ctx)

	err := manager.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}
}

func TestManager_AllServices(t *testing.T) {
	manager := NewManager()

	// Create providers with services
	provider1 := &mockProvider{
		baseProvider: newBaseProvider("test1", ProviderTypeStatic, DefaultProviderConfig()),
	}
	provider1.services["svc1"] = &Service{ID: "svc1", Name: "service1"}
	provider1.services["svc2"] = &Service{ID: "svc2", Name: "service2"}

	provider2 := &mockProvider{
		baseProvider: newBaseProvider("test2", ProviderTypeStatic, DefaultProviderConfig()),
	}
	provider2.services["svc3"] = &Service{ID: "svc3", Name: "service3"}

	manager.AddProvider(provider1)
	manager.AddProvider(provider2)

	services := manager.AllServices()
	if len(services) != 3 {
		t.Errorf("AllServices() returned %d services, want 3", len(services))
	}
}

func TestServiceFilter_Matches(t *testing.T) {
	tests := []struct {
		name    string
		filter  *ServiceFilter
		service *Service
		want    bool
	}{
		{
			name:    "nil filter",
			filter:  nil,
			service: &Service{ID: "test"},
			want:    true,
		},
		{
			name:   "healthy match",
			filter: &ServiceFilter{Healthy: boolPtr(true)},
			service: &Service{
				ID:      "test",
				Healthy: true,
			},
			want: true,
		},
		{
			name:   "healthy no match",
			filter: &ServiceFilter{Healthy: boolPtr(true)},
			service: &Service{
				ID:      "test",
				Healthy: false,
			},
			want: false,
		},
		{
			name:   "tags match",
			filter: &ServiceFilter{Tags: []string{"web", "api"}},
			service: &Service{
				ID:   "test",
				Tags: []string{"web", "api", "v1"},
			},
			want: true,
		},
		{
			name:   "tags no match",
			filter: &ServiceFilter{Tags: []string{"web", "api"}},
			service: &Service{
				ID:   "test",
				Tags: []string{"web"},
			},
			want: false,
		},
		{
			name:   "meta match",
			filter: &ServiceFilter{Meta: map[string]string{"env": "prod"}},
			service: &Service{
				ID:   "test",
				Meta: map[string]string{"env": "prod", "region": "us-east"},
			},
			want: true,
		},
		{
			name:   "meta no match",
			filter: &ServiceFilter{Meta: map[string]string{"env": "prod"}},
			service: &Service{
				ID:   "test",
				Meta: map[string]string{"env": "dev"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(tt.service)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterServices(t *testing.T) {
	services := []*Service{
		{ID: "1", Healthy: true, Tags: []string{"web"}},
		{ID: "2", Healthy: false, Tags: []string{"web"}},
		{ID: "3", Healthy: true, Tags: []string{"api"}},
	}

	filter := &ServiceFilter{
		Healthy: boolPtr(true),
		Tags:    []string{"web"},
	}

	filtered := FilterServices(services, filter)
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(filtered))
	}
	if filtered[0].ID != "1" {
		t.Errorf("Expected service 1, got %s", filtered[0].ID)
	}
}

func TestFilterServices_NilFilter(t *testing.T) {
	services := []*Service{
		{ID: "1"},
		{ID: "2"},
	}

	filtered := FilterServices(services, nil)
	if len(filtered) != 2 {
		t.Errorf("Expected 2 services with nil filter, got %d", len(filtered))
	}
}

func TestRegisterProviderFactory(t *testing.T) {
	// Create a test factory
	factoryCalled := false
	testFactory := func(config *ProviderConfig) (Provider, error) {
		factoryCalled = true
		return &mockProvider{
			baseProvider: newBaseProvider(config.Name, config.Type, config),
		}, nil
	}

	// Register factory
	RegisterProviderFactory("test", testFactory)

	// Create provider
	config := &ProviderConfig{
		Type: "test",
		Name: "test-provider",
	}

	provider, err := CreateProvider(config)
	if err != nil {
		t.Fatalf("CreateProvider error: %v", err)
	}

	if !factoryCalled {
		t.Error("Factory was not called")
	}

	if provider.Name() != "test-provider" {
		t.Errorf("Name = %q, want test-provider", provider.Name())
	}
}

func TestCreateProvider_UnknownType(t *testing.T) {
	config := &ProviderConfig{
		Type: "nonexistent",
		Name: "test",
	}

	_, err := CreateProvider(config)
	if err == nil {
		t.Error("Expected error for unknown provider type")
	}
}

func TestCreateProvider_InvalidConfig(t *testing.T) {
	config := &ProviderConfig{
		Type: "",
		Name: "test",
	}

	_, err := CreateProvider(config)
	if err == nil {
		t.Error("Expected error for invalid config")
	}
}

func TestBaseProvider_Stop(t *testing.T) {
	bp := newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig())
	ctx, cancel := context.WithCancel(context.Background())
	bp.ctx = ctx
	bp.cancel = cancel

	err := bp.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be cancelled")
	}
}

// Helper function
func boolPtr(b bool) *bool {
	return &b
}

// Mock provider for testing
type mockProvider struct {
	*baseProvider
	started bool
}

func (p *mockProvider) Start(ctx context.Context) error {
	p.started = true
	return nil
}

// ---- baseProvider tests ----

func TestBaseProvider_AddRemoveService(t *testing.T) {
	bp := newBaseProvider("test", ProviderTypeStatic, DefaultProviderConfig())

	svc := &Service{
		ID:      "svc-1",
		Name:    "test-service",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	}

	bp.addService(svc)

	services := bp.Services()
	if len(services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(services))
	}

	// Update same service (no change) should not emit event
	bp.addService(svc)

	// Update service with change
	updatedSvc := &Service{
		ID:      "svc-1",
		Name:    "test-service",
		Address: "10.0.0.2",
		Port:    8080,
		Healthy: true,
	}
	bp.addService(updatedSvc)

	// Remove
	bp.removeService("svc-1")
	services = bp.Services()
	if len(services) != 0 {
		t.Errorf("Expected 0 services after remove, got %d", len(services))
	}

	// Remove non-existent should not panic
	bp.removeService("nonexistent")
}

func TestManager_AggregateEvents(t *testing.T) {
	manager := NewManager()

	p1 := &mockProvider{
		baseProvider: newBaseProvider("p1", ProviderTypeStatic, DefaultProviderConfig()),
	}
	p2 := &mockProvider{
		baseProvider: newBaseProvider("p2", ProviderTypeStatic, DefaultProviderConfig()),
	}

	manager.AddProvider(p1)
	manager.AddProvider(p2)

	agg := manager.AggregateEvents()
	if agg == nil {
		t.Fatal("AggregateEvents() returned nil")
	}
}

// ---- Consul Provider Tests ----

// mockConsulServer simulates a Consul API for testing.
type mockConsulServer struct{}

func newMockConsulServer() *httptest.Server {
	mock := &mockConsulServer{}
	return httptest.NewServer(http.HandlerFunc(mock.handleRequest))
}

func (m *mockConsulServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/catalog/services":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":["v1","http"],"api":["v2"]}`)
	case hasSubstr(r.URL.Path, "/v1/catalog/service/"):
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"ServiceAddress":"10.0.0.1","ServicePort":8080,"ServiceTags":["v1"],"ServiceMeta":{},"ServiceName":"web","ServiceID":"web-1","Node":"node-1","Datacenter":"dc1"}]`)
	default:
		http.NotFound(w, r)
	}
}

// hasSubstr checks if s contains substr (avoids name collision with docker_test.go helpers).
func hasSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewConsulProvider(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"address": "127.0.0.1:8500",
		},
	}

	provider, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	if provider.Name() != "test-consul" {
		t.Errorf("Name() = %q, want test-consul", provider.Name())
	}
	if provider.Type() != ProviderTypeConsul {
		t.Errorf("Type() = %q, want consul", provider.Type())
	}
}

func TestNewConsulProvider_InvalidType(t *testing.T) {
	config := &ProviderConfig{
		Type: ProviderTypeStatic,
		Name: "test",
	}

	_, err := NewConsulProvider(config)
	if err == nil {
		t.Error("Expected error for invalid provider type")
	}
}

func TestNewConsulProvider_DefaultAddress(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{},
	}

	provider, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	if provider.address != "127.0.0.1:8500" {
		t.Errorf("address = %q, want 127.0.0.1:8500", provider.address)
	}
}

func TestConsulProvider_BuildURL(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"address": "consul.example.com:8500",
			"token":   "my-token",
		},
	}

	provider, _ := NewConsulProvider(config)

	u := provider.buildURL("/v1/catalog/services", nil)
	if !hasSubstr(u, "consul.example.com:8500") {
		t.Errorf("URL should contain host: %s", u)
	}
	if !hasSubstr(u, "token=my-token") {
		t.Errorf("URL should contain token: %s", u)
	}
}

func TestConsulProvider_ShouldWatchService(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"service": "web",
		},
	}

	provider, _ := NewConsulProvider(config)

	if !provider.shouldWatchService("web") {
		t.Error("Should watch service 'web'")
	}
	if provider.shouldWatchService("api") {
		t.Error("Should not watch service 'api'")
	}
}

func TestConsulProvider_ShouldWatchService_NoFilter(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{},
	}

	provider, _ := NewConsulProvider(config)

	if !provider.shouldWatchService("anything") {
		t.Error("Should watch any service when no filter is set")
	}
}

func TestConsulProvider_ConvertService(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{},
	}

	provider, _ := NewConsulProvider(config)

	entry := consulServiceEntry{
		ServiceAddress: "10.0.0.1",
		ServicePort:    8080,
		ServiceTags:    []string{"v1", "http"},
		ServiceMeta:    map[string]string{"version": "1.0"},
		ServiceName:    "web",
		ServiceID:      "web-1",
		Node:           "node-1",
		Datacenter:     "dc1",
	}

	svc := provider.convertService(entry)
	if svc == nil {
		t.Fatal("convertService returned nil")
	}
	if svc.Address != "10.0.0.1" {
		t.Errorf("Address = %q, want 10.0.0.1", svc.Address)
	}
	if svc.Port != 8080 {
		t.Errorf("Port = %d, want 8080", svc.Port)
	}
	if svc.Name != "web" {
		t.Errorf("Name = %q, want web", svc.Name)
	}
	if svc.Meta["datacenter"] != "dc1" {
		t.Errorf("Meta[datacenter] = %q, want dc1", svc.Meta["datacenter"])
	}
}

func TestConsulProvider_ConvertService_FallbackToNode(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test",
		Refresh: 5 * time.Second,
		Options: map[string]string{},
	}

	provider, _ := NewConsulProvider(config)

	entry := consulServiceEntry{
		ServiceAddress: "", // empty
		ServicePort:    8080,
		Node:           "node-1",
	}

	svc := provider.convertService(entry)
	if svc.Address != "node-1" {
		t.Errorf("Address = %q, want node-1 (fallback to Node)", svc.Address)
	}
}

func TestConsulProvider_GetServices_MockServer(t *testing.T) {
	server := newMockConsulServer()
	defer server.Close()

	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"address": server.Listener.Addr().String(),
			"scheme":  "http",
		},
	}

	provider, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	services, err := provider.getServices()
	if err != nil {
		t.Fatalf("getServices error: %v", err)
	}

	if len(services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(services))
	}
}

func TestConsulProvider_StartStop_MockServer(t *testing.T) {
	server := newMockConsulServer()
	defer server.Close()

	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Refresh: 100 * time.Millisecond,
		Options: map[string]string{
			"address": server.Listener.Addr().String(),
			"scheme":  "http",
			"service": "web",
		},
	}

	provider, _ := NewConsulProvider(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Wait for initial refresh
	time.Sleep(200 * time.Millisecond)

	services := provider.Services()
	if len(services) == 0 {
		t.Error("Expected services to be discovered")
	}

	err = provider.Stop()
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

func TestParseWeight(t *testing.T) {
	tests := []struct {
		meta map[string]string
		want int
	}{
		{map[string]string{"weight": "5"}, 5},
		{map[string]string{"weight": "invalid"}, 1},
		{map[string]string{}, 1},
		{nil, 1},
	}

	for _, tt := range tests {
		got := parseWeight(tt.meta)
		if got != tt.want {
			t.Errorf("parseWeight(%v) = %d, want %d", tt.meta, got, tt.want)
		}
	}
}

// ---- DNS Provider Tests ----

func TestNewDNSProvider_ValidConfig(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"domain": "_http._tcp.example.com",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	if provider.Name() != "test-dns" {
		t.Errorf("Name() = %q, want test-dns", provider.Name())
	}
	if provider.Type() != ProviderTypeDNS {
		t.Errorf("Type() = %q, want dns", provider.Type())
	}
	if provider.domain != "_http._tcp.example.com" {
		t.Errorf("domain = %q, want _http._tcp.example.com", provider.domain)
	}
	if provider.resolver == nil {
		t.Error("resolver should not be nil")
	}
}

func TestNewDNSProvider_MissingDomain(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns",
		Refresh: 5 * time.Second,
		Options: map[string]string{},
	}

	_, err := NewDNSProvider(config)
	if err == nil {
		t.Error("Expected error for missing domain option")
	}
}

func TestNewDNSProvider_WithNameserver(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"domain":     "_http._tcp.example.com",
			"nameserver": "8.8.8.8:53",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	if provider.nameserver != "8.8.8.8:53" {
		t.Errorf("nameserver = %q, want 8.8.8.8:53", provider.nameserver)
	}
	if provider.resolver.Dial == nil {
		t.Error("resolver should have custom Dial when nameserver is set")
	}
}

func TestDNSProvider_StartStop(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns",
		Refresh: 100 * time.Millisecond,
		Options: map[string]string{
			"domain": "_http._tcp.invalid.example.com",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should succeed even if DNS lookup fails (it logs but continues)
	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Let the refresh loop run once
	time.Sleep(200 * time.Millisecond)

	err = provider.Stop()
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

// Ensure unused imports are consumed.
var _ = json.Marshal
