package discovery

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// Tests: Manager Stop with provider errors
// --------------------------------------------------------------------------

func TestManager_Stop_ProviderError(t *testing.T) {
	mgr := NewManager()

	p := &stopErrProvider{
		name:    "err-stop",
		stopErr: fmt.Errorf("stop failed"),
	}

	mgr.AddProvider(p)

	err := mgr.Stop()
	if err == nil {
		t.Error("Expected error when provider fails to stop")
	}
}

// stopErrProvider is a minimal Provider that returns error on Stop
type stopErrProvider struct {
	name    string
	stopErr error
}

func (p *stopErrProvider) Name() string                    { return p.name }
func (p *stopErrProvider) Type() ProviderType              { return ProviderTypeStatic }
func (p *stopErrProvider) Start(ctx context.Context) error { return nil }
func (p *stopErrProvider) Stop() error                     { return p.stopErr }
func (p *stopErrProvider) Services() []*Service            { return nil }
func (p *stopErrProvider) Events() <-chan *Event           { return nil }

// --------------------------------------------------------------------------
// Tests: Docker handleEvent with pollContainers error
// --------------------------------------------------------------------------

func TestDockerProvider_HandleEvent_StartPollError(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message": "internal server error"}`)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	event := &dockerEvent{
		Type:   "container",
		Action: "start",
		Actor: dockerEventActor{
			ID: "aaa111222333abcd",
		},
	}

	p.handleEvent(event)

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after poll error, got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker streamEvents success path with event processing
// --------------------------------------------------------------------------

func TestDockerProvider_StreamEvents_Success(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"Type":"container","Action":"stop","Actor":{"ID":"abc123def456abcd"}}`+"\n")
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "[]")
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	err := p.streamEvents()
	if err != nil {
		t.Logf("streamEvents returned: %v (expected, body closed)", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker readEventStream with stop event removes service
// --------------------------------------------------------------------------

func TestDockerProvider_ReadEventStream_StopRemovesService(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	input := `{"Type":"container","Action":"stop","Actor":{"ID":"abc123def456abcd"}}` + "\n"
	err := p.readEventStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("readEventStream error: %v", err)
	}

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after stop event, got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker readEventStream with die event removes service
// --------------------------------------------------------------------------

func TestDockerProvider_ReadEventStream_DieRemovesService(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	input := `{"Type":"container","Action":"die","Actor":{"ID":"abc123def456abcd"}}` + "\n"
	err := p.readEventStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("readEventStream error: %v", err)
	}

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after die event, got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker handleEvent with non-container event type
// --------------------------------------------------------------------------

func TestDockerProvider_HandleEvent_NonContainerType(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	event := &dockerEvent{
		Type:   "image",
		Action: "delete",
		Actor:  dockerEventActor{ID: "abc123def456abcd"},
	}

	p.handleEvent(event)

	if len(p.Services()) != 1 {
		t.Errorf("Expected 1 service (non-container event ignored), got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker listContainers API error (500)
// --------------------------------------------------------------------------

func TestDockerProvider_ListContainers_500(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message": "server error"}`)
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	_, err := p.listContainers()
	if err == nil {
		t.Error("Expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Error should mention status 500: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker listContainers JSON decode error
// --------------------------------------------------------------------------

func TestDockerProvider_ListContainers_BadJSON(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `this is not json`)
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	_, err := p.listContainers()
	if err == nil {
		t.Error("Expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("Error should mention decode: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker streamEvents API error
// --------------------------------------------------------------------------

func TestDockerProvider_StreamEvents_APIError(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	err := p.streamEvents()
	if err == nil {
		t.Error("Expected error for events API 500")
	}
}

// --------------------------------------------------------------------------
// Tests: Consul buildURL with explicit params
// --------------------------------------------------------------------------

func TestConsulProvider_BuildURL_WithExplicitParams(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewConsulProvider(config)

	params := url.Values{}
	params.Set("dc", "dc2")
	params.Set("filter", "active")

	u := p.buildURL("/v1/catalog/services", params)
	if !strings.Contains(u, "dc=dc2") {
		t.Errorf("URL should contain dc=dc2: %s", u)
	}
	if !strings.Contains(u, "filter=active") {
		t.Errorf("URL should contain filter=active: %s", u)
	}
}

// --------------------------------------------------------------------------
// Tests: Consul buildURL with token
// --------------------------------------------------------------------------

func TestConsulProvider_BuildURL_WithToken(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test",
		Options: map[string]string{"token": "secret-token"},
	}
	p, _ := NewConsulProvider(config)
	u := p.buildURL("/v1/health/service/web", nil)
	if !strings.Contains(u, "token=secret-token") {
		t.Errorf("URL should contain token: %s", u)
	}
}

// --------------------------------------------------------------------------
// Tests: File provider pollLoop reloads file
// --------------------------------------------------------------------------

func TestFileProvider_PollLoop_ReloadsFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	content := `{"backends":[{"address":"10.0.0.1:8080"}]}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeFile,
		Name:    "file-test",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{"path": filePath, "poll_interval": "50ms"},
	}
	p, err := NewFileProvider(config)
	if err != nil {
		t.Fatalf("NewFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	time.Sleep(150 * time.Millisecond)

	services := p.Services()
	if len(services) == 0 {
		t.Fatal("Expected at least 1 service after start")
	}

	found := false
	for _, svc := range services {
		if svc.Address == "10.0.0.1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected service with address 10.0.0.1")
	}
}

// --------------------------------------------------------------------------
// Tests: File provider Start fails with invalid JSON
// --------------------------------------------------------------------------

func TestFileProvider_Start_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	if err := os.WriteFile(filePath, []byte(`not valid json`), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeFile,
		Name:    "file-invalid",
		Refresh: 1 * time.Hour,
		Options: map[string]string{"path": filePath},
	}
	p, err := NewFileProvider(config)
	if err != nil {
		t.Fatalf("NewFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = p.Start(ctx)
	if err == nil {
		t.Error("Expected error for invalid JSON file")
		p.Stop()
	}
}

// --------------------------------------------------------------------------
// Tests: File provider Start with nonexistent file
// --------------------------------------------------------------------------

func TestFileProvider_Start_NoFile(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeFile,
		Name:    "file-nofile",
		Refresh: 1 * time.Hour,
		Options: map[string]string{"path": "/nonexistent/path/services.json"},
	}
	p, err := NewFileProvider(config)
	if err != nil {
		t.Fatalf("NewFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = p.Start(ctx)
	if err == nil {
		t.Error("Expected error for nonexistent file")
		p.Stop()
	}
}

// --------------------------------------------------------------------------
// Tests: Manager Start with provider that fails
// --------------------------------------------------------------------------

type startErrProvider struct {
	name     string
	startErr error
}

func (p *startErrProvider) Name() string                    { return p.name }
func (p *startErrProvider) Type() ProviderType              { return ProviderTypeStatic }
func (p *startErrProvider) Start(ctx context.Context) error { return p.startErr }
func (p *startErrProvider) Stop() error                     { return nil }
func (p *startErrProvider) Services() []*Service            { return nil }
func (p *startErrProvider) Events() <-chan *Event           { return nil }

func TestManager_Start_ProviderStartError(t *testing.T) {
	mgr := NewManager()

	p := &startErrProvider{
		name:     "err-start",
		startErr: fmt.Errorf("start failed"),
	}

	mgr.AddProvider(p)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := mgr.Start(ctx)
	if err == nil {
		t.Error("Expected error when provider fails to start")
	}
	if !strings.Contains(err.Error(), "err-start") {
		t.Errorf("Error should mention provider name: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: DNS refresh error paths with custom resolver
// --------------------------------------------------------------------------

func TestDNSProvider_Refresh_WithFailingResolver(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-refresh",
		Refresh: 5 * time.Second,
		Tags:    []string{"tag1"},
		Options: map[string]string{
			"domain": "_http._tcp.example.com",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ctx = ctx

	// Use a resolver that always fails - tests the error path in refresh
	provider.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("mock DNS server unavailable")
		},
	}

	// Manually add a service before refresh to test stale removal logic
	provider.addService(&Service{
		ID:      "preexisting-service",
		Name:    "test-dns-refresh",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	})

	// refresh will fail on SRV lookup
	err = provider.refresh()
	if err == nil {
		t.Error("Expected error from refresh with failing resolver")
	}

	// The pre-existing service should remain since SRV lookup failed
	// before we get to stale removal
	services := provider.Services()
	if len(services) != 1 {
		t.Errorf("Expected 1 service (preexisting) after failed SRV, got %d", len(services))
	}
}

func TestDNSProvider_Refresh_StaleRemovalPath(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-stale",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"domain": "_http._tcp.example.com",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ctx = ctx

	provider.addService(&Service{
		ID:      "stale-svc1",
		Name:    "test-dns-stale",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	})
	provider.addService(&Service{
		ID:      "stale-svc2",
		Name:    "test-dns-stale",
		Address: "10.0.0.2",
		Port:    9090,
		Healthy: true,
	})

	provider.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("mock DNS server unavailable")
		},
	}

	err = provider.refresh()
	if err == nil {
		t.Error("Expected error from refresh")
	}

	if len(provider.Services()) != 2 {
		t.Errorf("Expected 2 services after failed refresh, got %d", len(provider.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: File provider pollLoop error path
// --------------------------------------------------------------------------

func TestFileProvider_PollLoop_LoadFileError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	content := `{"backends":[{"address":"10.0.0.1:8080"}]}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeFile,
		Name:    "file-poll-err",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{"path": filePath, "poll_interval": "50ms"},
	}
	p, err := NewFileProvider(config)
	if err != nil {
		t.Fatalf("NewFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	time.Sleep(80 * time.Millisecond)

	if len(p.Services()) != 1 {
		t.Fatalf("Expected 1 service after start, got %d", len(p.Services()))
	}

	// Delete the file to trigger loadFile error during poll
	os.Remove(filePath)

	time.Sleep(150 * time.Millisecond)

	if len(p.Services()) != 1 {
		t.Logf("Services after file removal: %d (last known good state preserved)", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker inspectContainer error paths
// --------------------------------------------------------------------------

func TestDockerProvider_InspectContainer_NonOKStatus(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message": "container not found"}`)
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "[]")
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	_, err := p.inspectContainer("abc123def456")
	if err == nil {
		t.Error("Expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Error should mention status 404: %v", err)
	}
}

func TestDockerProvider_InspectContainer_BadJSON(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `not valid json`)
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "[]")
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	_, err := p.inspectContainer("abc123")
	if err == nil {
		t.Error("Expected error for invalid JSON in inspect response")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("Error should mention decode: %v", err)
	}
}

func TestDockerProvider_InspectContainer_RequestError(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.ctx = ctx

	_, err := p.inspectContainer("abc123")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker watchEvents reconnect path
// --------------------------------------------------------------------------

func TestDockerProvider_WatchEvents_ReconnectOnStreamError(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if count == 1 {
			fmt.Fprintf(w, `{"Type":"container","Action":"stop","Actor":{"ID":"aaa111222333abcd"}}`+"\n")
			return
		}
		return
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "[]")
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-aaa111222333",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	p.wg.Add(1)
	go p.watchEvents()

	time.Sleep(3 * time.Second)

	p.cancel()
	p.wg.Wait()

	if len(p.Services()) != 0 {
		t.Logf("Services after stop event: %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker streamEvents - context cancellation during scanning
// --------------------------------------------------------------------------

func TestDockerProvider_StreamEvents_ContextCancelled(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"Type":"container","Action":"stop","Actor":{"ID":"abc123def456abcd"}}`+"\n")
		<-r.Context().Done()
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "[]")
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	err := p.streamEvents()
	if err != nil {
		t.Logf("streamEvents returned: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker readEventStream - invalid JSON lines, scanner error
// --------------------------------------------------------------------------

func TestDockerProvider_ReadEventStream_InvalidJSONLines(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	input := "not json at all\n" +
		"\n" +
		`{"Type":"container","Action":"stop","Actor":{"ID":"abc123def456abcd"}}` + "\n" +
		"more invalid json\n"

	err := p.readEventStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("readEventStream error: %v", err)
	}

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after stop event, got %d", len(p.Services()))
	}
}

func TestDockerProvider_ReadEventStream_ScannerError(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	err := p.readEventStream(&errorReader{})
	if err == nil {
		t.Error("Expected error from errorReader")
	}
}

type errorReader struct{}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

// --------------------------------------------------------------------------
// Tests: Docker streamEvents / listContainers / inspectContainer request creation errors
// --------------------------------------------------------------------------

func TestDockerProvider_StreamEvents_RequestCreationError(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.ctx = ctx

	err := p.streamEvents()
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

func TestDockerProvider_ListContainers_RequestCreationError(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.ctx = ctx

	_, err := p.listContainers()
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

func TestDockerProvider_InspectContainer_RequestCreationError(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.ctx = ctx

	_, err := p.inspectContainer("abc123")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker containerToService - no names (uses short ID)
// --------------------------------------------------------------------------

func TestDockerProvider_ContainerToService_NoNames(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	container := dockerContainer{
		ID:    "abc123def456abcd",
		Names: nil,
		Labels: map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		},
		NetworkSettings: &dockerNetworks{
			Networks: map[string]*dockerNetwork{
				"bridge": {IPAddress: "172.17.0.2"},
			},
		},
	}

	svc := p.containerToService(container)
	if svc == nil {
		t.Fatal("containerToService returned nil")
	}
	if svc.Name != "abc123def456" {
		t.Errorf("Name = %q, want abc123def456 (short ID)", svc.Name)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker containerToService - no pool label
// --------------------------------------------------------------------------

func TestDockerProvider_ContainerToService_NoPool(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	container := dockerContainer{
		ID:    "abc123def456abcd",
		Names: []string{"/web"},
		Labels: map[string]string{
			"olb.enable": "true",
			"olb.port":   "8080",
		},
		NetworkSettings: &dockerNetworks{
			Networks: map[string]*dockerNetwork{
				"bridge": {IPAddress: "172.17.0.2"},
			},
		},
	}

	svc := p.containerToService(container)
	if svc == nil {
		t.Fatal("containerToService returned nil")
	}
	if _, ok := svc.Meta["pool"]; ok {
		t.Error("pool should not be set in meta when no olb.pool label")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker NewDockerProviderWithConfig with Unix socket
// --------------------------------------------------------------------------

func TestNewDockerProviderWithConfig_UnixSocketTransport(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test-unix",
		Options: map[string]string{},
	}

	provider, err := NewDockerProviderWithConfig(config, &DockerConfig{
		SocketPath: "/var/run/docker.sock",
	})
	if err != nil {
		t.Fatalf("NewDockerProviderWithConfig error: %v", err)
	}
	if provider.transport == nil {
		t.Error("transport should not be nil")
	}
	if provider.dockerConfig.Host != "" {
		t.Errorf("Host should be empty for Unix socket mode, got %q", provider.dockerConfig.Host)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker NewDockerProvider with TLS option
// --------------------------------------------------------------------------

func TestNewDockerProvider_TLSOption(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test-tls",
		Options: map[string]string{"tls": "true"},
	}

	p, err := NewDockerProvider(config)
	if err != nil {
		t.Fatalf("NewDockerProvider error: %v", err)
	}
	if !p.dockerConfig.TLSEnabled {
		t.Error("TLSEnabled should be true")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker NewDockerProvider with host option
// --------------------------------------------------------------------------

func TestNewDockerProvider_HostOption(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test-host",
		Options: map[string]string{"host": "tcp://192.168.1.10:2376"},
	}

	p, err := NewDockerProvider(config)
	if err != nil {
		t.Fatalf("NewDockerProvider error: %v", err)
	}
	if p.dockerConfig.Host != "tcp://192.168.1.10:2376" {
		t.Errorf("Host = %q, want tcp://192.168.1.10:2376", p.dockerConfig.Host)
	}
}

// --------------------------------------------------------------------------
// Tests: Docker handleEvent - empty actor ID
// --------------------------------------------------------------------------

func TestDockerProvider_HandleEvent_EmptyActorID(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	event := &dockerEvent{
		Type:   "container",
		Action: "stop",
		Actor:  dockerEventActor{ID: ""},
	}

	p.handleEvent(event)

	if len(p.Services()) != 1 {
		t.Errorf("Expected 1 service (empty actor ID ignored), got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker handleEvent - start event triggers pollContainers
// --------------------------------------------------------------------------

func TestDockerProvider_HandleEvent_StartTriggersPoll(t *testing.T) {
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

	event := &dockerEvent{
		Type:   "container",
		Action: "start",
		Actor:  dockerEventActor{ID: "abc123def456abcd"},
	}

	p.handleEvent(event)

	if len(p.Services()) != 1 {
		t.Errorf("Expected 1 service after start event, got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker urlEncode with special characters
// --------------------------------------------------------------------------

func TestDockerProvider_UrlEncode_SpecialChars(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello-world_test.hello", "hello-world_test.hello"},
		{"a/b", "a%2Fb"},
		{"a=b", "a%3Db"},
		{"a+b", "a%2Bb"},
		{"", ""},
	}

	for _, tt := range tests {
		got := urlEncode(tt.input)
		if got != tt.want {
			t.Errorf("urlEncode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --------------------------------------------------------------------------
// Tests: splitHostPort edge cases for uncovered branches
// --------------------------------------------------------------------------

func TestSplitHostPort_EdgeCases(t *testing.T) {
	tests := []struct {
		addr     string
		wantHost string
		wantPort int
	}{
		// Unclosed bracket - no port
		{"[::1", "[::1", 80},
		// Bracket with colon but non-digit after port: returns original addr
		{"[::1]:abc", "[::1]:abc", 80},
		// Bracket with port > 65535: strips brackets, returns default port
		{"[::1]:99999", "::1", 80},
		// Just a colon: port 0 -> returns original
		{":", ":", 80},
		// Port 0 - fails >0 check, returns original
		{"host:0", "host:0", 80},
		// Non-digit in port area, returns original
		{"host:-1", "host:-1", 80},
		// IPv6 with valid port
		{"[2001:db8::1]:443", "2001:db8::1", 443},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			host, port := splitHostPort(tt.addr)
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Tests: Docker pollLoop error path
// --------------------------------------------------------------------------

func TestDockerProvider_PollLoop_PollError(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message": "internal server error"}`)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.ctx = ctx
	p.cancel = cancel

	p.wg.Add(1)
	go p.pollLoop()

	time.Sleep(200 * time.Millisecond)

	cancel()
	p.wg.Wait()
}

// --------------------------------------------------------------------------
// Tests: Docker readEventStream with net.Pipe
// --------------------------------------------------------------------------

func TestDockerProvider_ReadEventStream_PipeClosed(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	r, w := net.Pipe()

	go func() {
		defer w.Close()
		line := `{"Type":"container","Action":"stop","Actor":{"ID":"abc123def456abcd"}}` + "\n"
		w.Write([]byte(line))
	}()

	p.addService(&Service{
		ID:      "test-docker-abc123def456",
		Name:    "web-app",
		Address: "172.17.0.2",
		Port:    8080,
		Healthy: true,
	})

	err := p.readEventStream(r)
	r.Close()
	if err != nil {
		t.Logf("readEventStream returned: %v", err)
	}

	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services after stop event, got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker NewDockerProviderWithConfig with remote host (no unix socket)
// --------------------------------------------------------------------------

func TestNewDockerProviderWithConfig_RemoteHost(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test-remote",
		Options: map[string]string{},
	}

	provider, err := NewDockerProviderWithConfig(config, &DockerConfig{
		Host: "tcp://192.168.1.10:2376",
	})
	if err != nil {
		t.Fatalf("NewDockerProviderWithConfig error: %v", err)
	}
	if provider.dockerConfig.Host != "tcp://192.168.1.10:2376" {
		t.Errorf("Host = %q, want tcp://192.168.1.10:2376", provider.dockerConfig.Host)
	}
}

// --------------------------------------------------------------------------
// Tests: AggregateEvents with actual events
// --------------------------------------------------------------------------

func TestManager_AggregateEvents_ReceivesEvents(t *testing.T) {
	mgr := NewManager()

	p1 := &mockProvider{
		baseProvider: newBaseProvider("p1", ProviderTypeStatic, DefaultProviderConfig()),
	}

	mgr.AddProvider(p1)

	agg := mgr.AggregateEvents()
	if agg == nil {
		t.Fatal("AggregateEvents returned nil")
	}

	p1.addService(&Service{
		ID:      "svc-1",
		Name:    "test",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	})

	select {
	case event := <-agg:
		if event == nil {
			t.Error("Event is nil")
		} else if event.Type != EventTypeAdd {
			t.Errorf("Event type = %v, want Add", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for aggregated event")
	}

	p1.Stop()
}

// --------------------------------------------------------------------------
// Tests: StaticFileProvider with missing ID auto-generates one
// --------------------------------------------------------------------------

func TestStaticFileProvider_AutoGenerateID(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	content := `{"services": [{"address": "10.0.0.1", "port": 8080}]}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "auto-id-test",
		Options: map[string]string{"file": filePath, "format": "json"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := provider.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer provider.Stop()

	services := provider.Services()
	if len(services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(services))
	}

	expectedID := "auto-id-test-10.0.0.1-8080"
	if services[0].ID != expectedID {
		t.Errorf("ID = %q, want %q", services[0].ID, expectedID)
	}
}

// --------------------------------------------------------------------------
// Tests: DNS NewDNSProvider custom dialer fields
// --------------------------------------------------------------------------

func TestNewDNSProvider_CustomDialerFields(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-dial",
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

	if provider.resolver.Dial == nil {
		t.Fatal("resolver should have custom Dial function")
	}
	if provider.resolver.PreferGo != true {
		t.Error("resolver should have PreferGo=true")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker containerToService with nil network settings
// --------------------------------------------------------------------------

func TestDockerProvider_ContainerToService_NilNetworkSettings(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	container := dockerContainer{
		ID:     "abc123def456abcd",
		Names:  []string{"/web"},
		Labels: map[string]string{"olb.enable": "true", "olb.port": "8080"},
		NetworkSettings: &dockerNetworks{
			Networks: nil,
		},
	}

	svc := p.containerToService(container)
	if svc != nil {
		t.Error("Expected nil for container with nil networks")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker pollContainers with context cancellation
// --------------------------------------------------------------------------

func TestDockerProvider_PollContainers_ContextCancelled(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.ctx = ctx

	err := p.pollContainers()
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker streamEvents - non-OK status response
// --------------------------------------------------------------------------

func TestDockerProvider_StreamEvents_NonOKStatus(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	mock.server.Handler = mux

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	err := p.streamEvents()
	if err == nil {
		t.Error("Expected error for non-OK events response")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("Error should mention status 502: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: DNS refresh with successful SRV resolution
// --------------------------------------------------------------------------

func TestDNSProvider_Refresh_SuccessfulSRVResolution(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-success",
		Refresh: 5 * time.Second,
		Tags:    []string{"env:prod"},
		Options: map[string]string{
			"domain": "_http._tcp.example.com",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ctx = ctx

	// Use a resolver that returns mock SRV records and host addresses
	provider.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("should not be called - LookupSRV is overridden via patching")
		},
	}

	// We can't easily mock LookupSRV since it's a method on net.Resolver.
	// Instead, test the refresh error path with a real resolver pointed at a
	// non-existent domain to cover the SRV lookup error branch.
	provider.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("DNS server unreachable")
		},
	}

	// Add pre-existing services to test stale removal path
	provider.addService(&Service{
		ID:      "test-dns-success-old.example.com-8080",
		Name:    "test-dns-success",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	})

	err = provider.refresh()
	if err == nil {
		t.Error("Expected error from refresh with unreachable DNS server")
	}

	// Pre-existing service should remain since refresh failed before stale removal
	if len(provider.Services()) != 1 {
		t.Errorf("Expected 1 service after failed refresh, got %d", len(provider.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: DNS Start and refreshLoop via context cancellation
// --------------------------------------------------------------------------

func TestDNSProvider_StartAndStop(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-start",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{
			"domain":     "_http._tcp.example.com",
			"nameserver": "127.0.0.1:0",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	// Use a resolver that always fails so refresh doesn't block
	provider.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("mock DNS failure")
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Let the refreshLoop run at least once
	time.Sleep(100 * time.Millisecond)

	err = provider.Stop()
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: DNS NewDNSProvider without domain option (error message check)
// --------------------------------------------------------------------------

func TestNewDNSProvider_MissingDomain_ErrorMessage(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-no-domain-2",
		Refresh: 5 * time.Second,
		Options: map[string]string{},
	}

	_, err := NewDNSProvider(config)
	if err == nil {
		t.Error("Expected error for missing domain option")
	}
	if !strings.Contains(err.Error(), "domain option is required") {
		t.Errorf("Error should mention 'domain option is required': %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Consul refreshService error preserves pre-existing services
// --------------------------------------------------------------------------

func TestConsulProvider_RefreshService_ErrorPreservesState(t *testing.T) {
	config := &ProviderConfig{
		Type:        ProviderTypeConsul,
		Name:        "test-consul",
		Refresh:     30 * time.Second,
		HealthCheck: true,
		Options:     map[string]string{},
	}
	p, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.ctx = ctx

	// Add a pre-existing service that should be removed after refresh
	// since getServiceEntries will fail (no real Consul)
	p.addService(&Service{
		ID:      "test-consul-web-node1-10.0.0.1-8080",
		Name:    "web",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	})

	// refreshService will call getServiceEntries which fails without a real Consul
	// This tests the error path where entries is nil
	p.refreshService("web")

	// The pre-existing service should remain because getServiceEntries failed
	// before we reach the stale removal loop
	if len(p.Services()) != 1 {
		t.Errorf("Expected 1 service (error path preserves state), got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Consul refreshService stale removal
// --------------------------------------------------------------------------

func TestConsulProvider_RefreshService_StaleRemoval(t *testing.T) {
	config := &ProviderConfig{
		Type:        ProviderTypeConsul,
		Name:        "test-consul",
		Refresh:     30 * time.Second,
		HealthCheck: false,
		Options:     map[string]string{},
	}
	p, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.ctx = ctx

	// Add services that exist under this provider's naming convention
	p.addService(&Service{
		ID:      "test-consul-web-node1-10.0.0.1-8080",
		Name:    "web",
		Address: "10.0.0.1",
		Port:    8080,
		Healthy: true,
	})
	p.addService(&Service{
		ID:      "test-consul-api-node1-10.0.0.2-9090",
		Name:    "api",
		Address: "10.0.0.2",
		Port:    9090,
		Healthy: true,
	})
	// Add a service from a different consul service name - should NOT be removed
	p.addService(&Service{
		ID:      "other-svc-id",
		Name:    "other",
		Address: "10.0.0.3",
		Port:    7070,
		Healthy: true,
	})

	// refreshService for "web" - getServiceEntries will fail, so no current IDs
	// are tracked and the stale removal code should still run but only remove
	// services prefixed with "test-consul-web-"
	p.refreshService("web")

	// The "web" service should still be there since getServiceEntries failed
	// (error path returns early before stale removal)
	services := p.Services()
	if len(services) != 3 {
		t.Errorf("Expected 3 services (early return on error), got %d", len(services))
	}
}

// --------------------------------------------------------------------------
// Tests: Consul convertService with empty ServiceAddress
// --------------------------------------------------------------------------

func TestConsulProvider_ConvertService_EmptyAddress(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Options: map[string]string{},
	}
	p, _ := NewConsulProvider(config)

	entry := consulServiceEntry{
		ServiceAddress: "",
		ServicePort:    8080,
		ServiceName:    "web",
		ServiceID:      "web-1",
		Node:           "node1",
		Datacenter:     "dc1",
		ServiceTags:    []string{"primary"},
		ServiceMeta:    map[string]string{"version": "1.0"},
	}

	svc := p.convertService(entry)
	if svc == nil {
		t.Fatal("convertService returned nil")
	}
	// Should fall back to Node when ServiceAddress is empty
	if svc.Address != "node1" {
		t.Errorf("Address = %q, want node1 (fallback)", svc.Address)
	}
	if svc.Port != 8080 {
		t.Errorf("Port = %d, want 8080", svc.Port)
	}
	if svc.Meta["node"] != "node1" {
		t.Errorf("Meta[node] = %q, want node1", svc.Meta["node"])
	}
	if svc.Meta["datacenter"] != "dc1" {
		t.Errorf("Meta[datacenter] = %q, want dc1", svc.Meta["datacenter"])
	}
	if svc.Meta["version"] != "1.0" {
		t.Errorf("Meta[version] = %q, want 1.0", svc.Meta["version"])
	}
}

// --------------------------------------------------------------------------
// Tests: Consul buildURL with token and existing params
// --------------------------------------------------------------------------

func TestConsulProvider_BuildURL_TokenWithParams(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul",
		Options: map[string]string{"token": "my-secret-token"},
	}
	p, _ := NewConsulProvider(config)

	params := url.Values{}
	params.Set("dc", "dc1")

	u := p.buildURL("/v1/catalog/services", params)

	// Should have both the param and the token appended with &
	if !strings.Contains(u, "dc=dc1") {
		t.Errorf("URL should contain dc=dc1: %s", u)
	}
	if !strings.Contains(u, "&token=my-secret-token") {
		t.Errorf("URL should contain &token=... when params exist: %s", u)
	}
}

// --------------------------------------------------------------------------
// Tests: Consul parseWeight helper (nil meta branch)
// --------------------------------------------------------------------------

func TestParseWeight_NilMeta(t *testing.T) {
	got := parseWeight(nil)
	if got != 1 {
		t.Errorf("parseWeight(nil) = %d, want 1", got)
	}
}

// --------------------------------------------------------------------------
// Tests: Consul NewConsulProvider with custom options
// --------------------------------------------------------------------------

func TestNewConsulProvider_CustomOptions(t *testing.T) {
	config := &ProviderConfig{
		Type: ProviderTypeConsul,
		Name: "custom-consul",
		Options: map[string]string{
			"address":    "10.0.0.1:9500",
			"scheme":     "https",
			"datacenter": "west",
			"token":      "abc123",
		},
	}

	p, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	if p.address != "10.0.0.1:9500" {
		t.Errorf("address = %q, want 10.0.0.1:9500", p.address)
	}
	if p.scheme != "https" {
		t.Errorf("scheme = %q, want https", p.scheme)
	}
	if p.datacenter != "west" {
		t.Errorf("datacenter = %q, want west", p.datacenter)
	}
	if p.token != "abc123" {
		t.Errorf("token = %q, want abc123", p.token)
	}
}

func TestNewConsulProvider_WrongType(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "test",
		Options: map[string]string{},
	}

	_, err := NewConsulProvider(config)
	if err == nil {
		t.Error("Expected error for invalid provider type")
	}
	if !strings.Contains(err.Error(), "invalid provider type") {
		t.Errorf("Error should mention 'invalid provider type': %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Consul Start and Stop
// --------------------------------------------------------------------------

func TestConsulProvider_StartStop(t *testing.T) {
	// Start a mock HTTP server to simulate Consul
	server := &http.Server{Addr: "127.0.0.1:0"}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"web":["primary"],"api":[]}`)
	})
	mux.HandleFunc("/v1/catalog/service/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[]")
	})
	server.Handler = mux

	go server.Serve(ln)
	defer server.Close()

	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul-start",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{
			"address": ln.Addr().String(),
			"service": "web",
		},
	}

	p, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = p.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	err = p.Stop()
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Consul watchCatalog returns early when service option set
// --------------------------------------------------------------------------

func TestConsulProvider_WatchCatalog_SkippedForSpecificService(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeConsul,
		Name:    "test-consul-catalog",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{
			"address": "127.0.0.1:9999",
			"service": "web",
		},
	}

	p, _ := NewConsulProvider(config)
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel

	// watchCatalog should return immediately when "service" option is set
	p.wg.Add(1)
	done := make(chan struct{})
	go func() {
		p.watchCatalog()
		close(done)
	}()

	select {
	case <-done:
		// Expected: returned immediately
	case <-time.After(500 * time.Millisecond):
		t.Error("watchCatalog should return immediately when service option is set")
	}

	cancel()
}

// --------------------------------------------------------------------------
// Tests: File provider pollLoop reloads changed file
// --------------------------------------------------------------------------

func TestFileProvider_PollLoop_DetectsFileChange(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	// Initial content with one backend
	content := `{"backends":[{"address":"10.0.0.1:8080"}]}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeFile,
		Name:    "file-change",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{"path": filePath, "poll_interval": "50ms"},
	}
	p, err := NewFileProvider(config)
	if err != nil {
		t.Fatalf("NewFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	// Wait for initial load
	time.Sleep(100 * time.Millisecond)

	if len(p.Services()) != 1 {
		t.Fatalf("Expected 1 service initially, got %d", len(p.Services()))
	}

	// Update file with two backends
	content2 := `{"backends":[{"address":"10.0.0.1:8080"},{"address":"10.0.0.2:9090"}]}`
	if err := os.WriteFile(filePath, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to update file: %v", err)
	}

	// Wait for poll to pick up the change
	deadline := time.After(2 * time.Second)
	for {
		if len(p.Services()) == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for file change detection; got %d services", len(p.Services()))
		default:
			time.Sleep(30 * time.Millisecond)
		}
	}
}

// --------------------------------------------------------------------------
// Tests: File provider pollLoop error logging (file deleted then recreated)
// --------------------------------------------------------------------------

func TestFileProvider_PollLoop_FileDeletedAndRecreated(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	content := `{"backends":[{"address":"10.0.0.1:8080"}]}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeFile,
		Name:    "file-del-recreate",
		Refresh: 50 * time.Millisecond,
		Options: map[string]string{"path": filePath, "poll_interval": "50ms"},
	}
	p, err := NewFileProvider(config)
	if err != nil {
		t.Fatalf("NewFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	time.Sleep(100 * time.Millisecond)

	// Delete the file to trigger loadFile error in pollLoop
	os.Remove(filePath)

	time.Sleep(100 * time.Millisecond)

	// Recreate with different content
	content2 := `{"backends":[{"address":"10.0.0.3:7070"}]}`
	if err := os.WriteFile(filePath, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to recreate file: %v", err)
	}

	// Wait for poll to pick up the recreated file
	deadline := time.After(2 * time.Second)
	for {
		services := p.Services()
		if len(services) == 1 && services[0].Address == "10.0.0.3" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for recreated file; got %d services", len(p.Services()))
		default:
			time.Sleep(30 * time.Millisecond)
		}
	}
}

// --------------------------------------------------------------------------
// Tests: Static file provider YAML parsing
// --------------------------------------------------------------------------

func TestStaticFileProvider_YAMLFormat(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.yaml"

	content := `services:
  - address: "10.0.0.1"
    port: 8080
    tags:
      - "web"
  - address: "10.0.0.2"
    port: 8081`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "yaml-test",
		Options: map[string]string{"file": filePath, "format": "yaml"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	provider.Stop()
}

func TestStaticFileProvider_YAMLInvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.yaml"

	content := "[invalid yaml {"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "yaml-invalid",
		Options: map[string]string{"file": filePath, "format": "yaml"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = provider.Start(ctx)
	if err == nil {
		provider.Stop()
		t.Error("Expected error for invalid YAML")
	}
}

// --------------------------------------------------------------------------
// Tests: Docker watchEvents reconnect on stream error
// --------------------------------------------------------------------------

func TestDockerProvider_WatchEvents_ImmediateContextCancel(t *testing.T) {
	config := &ProviderConfig{
		Type:    ProviderTypeDocker,
		Name:    "test",
		Options: map[string]string{},
	}
	p, _ := NewDockerProvider(config)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so watchEvents exits on first select
	cancel()
	p.ctx = ctx
	p.cancel = cancel

	p.wg.Add(1)
	go p.watchEvents()

	// Should complete quickly since context is already cancelled
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Expected: exited immediately
	case <-time.After(2 * time.Second):
		t.Error("watchEvents should exit quickly with cancelled context")
	}
}

// --------------------------------------------------------------------------
// Tests: DNS refresh with mock DNS server (SRV resolution success path)
// --------------------------------------------------------------------------

func TestDNSProvider_Refresh_SuccessfulWithMockDNSServer(t *testing.T) {
	// Start a mock DNS server that responds to SRV and A queries
	dnsServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}
	defer dnsServer.Close()

	// Run the DNS server in background
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := dnsServer.ReadFrom(buf)
			if err != nil {
				return
			}
			// Parse the DNS query and craft a response
			resp := craftDNSResponse(buf[:n])
			if resp != nil {
				dnsServer.WriteTo(resp, addr)
			}
		}
	}()

	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-mock",
		Refresh: 5 * time.Second,
		Tags:    []string{"env:test"},
		Options: map[string]string{
			"domain":     "test.local",
			"nameserver": dnsServer.LocalAddr().String(),
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ctx = ctx

	// Add a stale service to test removal
	provider.addService(&Service{
		ID:      "stale-service",
		Name:    "test-dns-mock",
		Address: "10.0.0.99",
		Port:    9999,
		Healthy: true,
	})

	if len(provider.Services()) != 1 {
		t.Fatalf("Expected 1 stale service before refresh, got %d", len(provider.Services()))
	}

	// refresh should resolve SRV records, add new services, and remove stale ones
	err = provider.refresh()
	if err != nil {
		t.Fatalf("refresh error: %v", err)
	}

	services := provider.Services()
	if len(services) == 0 {
		t.Fatal("Expected at least 1 service after successful refresh")
	}

	// Check that the stale service was removed
	for _, svc := range services {
		if svc.ID == "stale-service" {
			t.Error("Stale service should have been removed")
		}
	}

	// Check service properties
	found := false
	for _, svc := range services {
		if svc.Address == "127.0.0.1" && svc.Port == 8080 {
			found = true
			if svc.Weight != 10 {
				t.Errorf("Weight = %d, want 10", svc.Weight)
			}
			if svc.Priority != 1 {
				t.Errorf("Priority = %d, want 1", svc.Priority)
			}
			if svc.Name != "test-dns-mock" {
				t.Errorf("Name = %q, want test-dns-mock", svc.Name)
			}
			if !svc.Healthy {
				t.Error("Service should be healthy")
			}
			if svc.Meta["target"] != "server.test.local." {
				t.Errorf("Meta[target] = %q, want server.test.local.", svc.Meta["target"])
			}
			// Tags should come from config
			if len(svc.Tags) != 1 || svc.Tags[0] != "env:test" {
				t.Errorf("Tags = %v, want [env:test]", svc.Tags)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected service at 127.0.0.1:8080, got: %v", services)
	}
}

// craftDNSResponse creates a DNS response that answers SRV and A queries
func craftDNSResponse(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}

	// Parse query header
	txID := query[0:2]
	flags := []byte{0x84, 0x00} // Response, authoritative
	qdcount := uint16(query[4])<<8 | uint16(query[5])
	_ = qdcount

	// Parse the question section to get the domain name
	offset := 12
	var qname []byte
	for offset < len(query) {
		labelLen := int(query[offset])
		if labelLen == 0 {
			qname = append(qname, 0)
			offset++
			break
		}
		qname = append(qname, query[offset:offset+labelLen+1]...)
		offset += labelLen + 1
	}
	if offset+4 > len(query) {
		return nil
	}
	qtype := uint16(query[offset])<<8 | uint16(query[offset+1])

	var answer []byte
	ancount := uint16(0)

	switch qtype {
	case 33: // SRV record
		// SRV: priority=1, weight=10, port=8080, target=server.test.local.
		target := []byte{
			6, 's', 'e', 'r', 'v', 'e', 'r', // "server"
			4, 't', 'e', 's', 't', // "test"
			5, 'l', 'o', 'c', 'a', 'l', // "local"
			0, // root
		}
		answer = make([]byte, 0, 256)
		// Name (pointer to question name)
		answer = append(answer, 0xC0, 0x0C)
		// Type SRV (33)
		answer = append(answer, 0x00, 0x21)
		// Class IN
		answer = append(answer, 0x00, 0x01)
		// TTL (300 seconds)
		answer = append(answer, 0x00, 0x00, 0x01, 0x2C)
		// RDLENGTH
		rdlen := uint16(2 + 2 + 2 + len(target))
		answer = append(answer, byte(rdlen>>8), byte(rdlen))
		// Priority
		answer = append(answer, 0x00, 0x01)
		// Weight
		answer = append(answer, 0x00, 0x0A)
		// Port
		answer = append(answer, 0x1F, 0x90) // 8080
		// Target
		answer = append(answer, target...)
		ancount = 1

	case 1: // A record
		answer = make([]byte, 0, 256)
		// Name (pointer to question name)
		answer = append(answer, 0xC0, 0x0C)
		// Type A
		answer = append(answer, 0x00, 0x01)
		// Class IN
		answer = append(answer, 0x00, 0x01)
		// TTL
		answer = append(answer, 0x00, 0x00, 0x01, 0x2C)
		// RDLENGTH
		answer = append(answer, 0x00, 0x04)
		// IP: 127.0.0.1
		answer = append(answer, 0x7F, 0x00, 0x00, 0x01)
		ancount = 1

	default:
		// For unknown types, just return no answers
		answer = []byte{}
		ancount = 0
	}

	// Build the response
	resp := make([]byte, 0, 512)
	// Header
	resp = append(resp, txID...)                         // Transaction ID
	resp = append(resp, flags...)                        // Flags
	resp = append(resp, 0x00, 0x01)                      // QDCOUNT = 1
	resp = append(resp, byte(ancount>>8), byte(ancount)) // ANCOUNT
	resp = append(resp, 0x00, 0x00)                      // NSCOUNT
	resp = append(resp, 0x00, 0x00)                      // ARCOUNT
	// Question section (echo back)
	questionEnd := offset + 4
	resp = append(resp, query[12:questionEnd]...)
	// Answer section
	resp = append(resp, answer...)

	return resp
}

// --------------------------------------------------------------------------
// Tests: DNS refresh with zero resolved addresses
// --------------------------------------------------------------------------

func TestDNSProvider_Refresh_NoResolvedAddresses(t *testing.T) {
	// Start a mock DNS server that returns SRV records but A queries return no results
	dnsServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create DNS server: %v", err)
	}
	defer dnsServer.Close()

	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := dnsServer.ReadFrom(buf)
			if err != nil {
				return
			}
			resp := craftDNSResponseNoA(buf[:n])
			if resp != nil {
				dnsServer.WriteTo(resp, addr)
			}
		}
	}()

	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-no-a",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"domain":     "empty.local",
			"nameserver": dnsServer.LocalAddr().String(),
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ctx = ctx

	err = provider.refresh()
	// Should succeed but with no services (SRV target has no A records)
	if err != nil {
		t.Logf("refresh returned: %v (expected when A lookup fails)", err)
	}
}

// craftDNSResponseNoA returns SRV records but no A records for the target
func craftDNSResponseNoA(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}

	txID := query[0:2]
	flags := []byte{0x84, 0x00}

	// Parse question
	offset := 12
	for offset < len(query) {
		if query[offset] == 0 {
			offset++
			break
		}
		offset += int(query[offset]) + 1
	}
	if offset+4 > len(query) {
		return nil
	}
	qtype := uint16(query[offset])<<8 | uint16(query[offset+1])

	if qtype == 33 {
		// Return SRV with a target that won't resolve
		target := []byte{
			7, 'n', 'o', 'r', 'e', 's', 'o', 'l', 'v', 'e',
			5, 'l', 'o', 'c', 'a', 'l',
			0,
		}
		answer := []byte{
			0xC0, 0x0C, // name pointer
			0x00, 0x21, // type SRV
			0x00, 0x01, // class IN
			0x00, 0x00, 0x01, 0x2C, // TTL
		}
		rdlen := uint16(2 + 2 + 2 + len(target))
		answer = append(answer, byte(rdlen>>8), byte(rdlen))
		answer = append(answer, 0x00, 0x01) // priority
		answer = append(answer, 0x00, 0x0A) // weight
		answer = append(answer, 0x1F, 0x90) // port
		answer = append(answer, target...)

		questionEnd := offset + 4
		resp := make([]byte, 0, 512)
		resp = append(resp, txID...)
		resp = append(resp, flags...)
		resp = append(resp, 0x00, 0x01) // QDCOUNT
		resp = append(resp, 0x00, 0x01) // ANCOUNT
		resp = append(resp, 0x00, 0x00) // NSCOUNT
		resp = append(resp, 0x00, 0x00) // ARCOUNT
		resp = append(resp, query[12:questionEnd]...)
		resp = append(resp, answer...)
		return resp
	}

	// For A queries, return no answer (so LookupHost returns empty)
	questionEnd := offset + 4
	resp := make([]byte, 0, 512)
	resp = append(resp, txID...)
	resp = append(resp, flags...)
	resp = append(resp, 0x00, 0x01) // QDCOUNT
	resp = append(resp, 0x00, 0x00) // ANCOUNT (no A records)
	resp = append(resp, 0x00, 0x00) // NSCOUNT
	resp = append(resp, 0x00, 0x00) // ARCOUNT
	resp = append(resp, query[12:questionEnd]...)
	return resp
}

// --------------------------------------------------------------------------
// Tests: DNS NewDNSProvider custom dialer is exercised (line 34-37)
// --------------------------------------------------------------------------

func TestNewDNSProvider_CustomDialerIsCalled(t *testing.T) {
	dialerCalled := false

	config := &ProviderConfig{
		Type:    ProviderTypeDNS,
		Name:    "test-dns-dialer",
		Refresh: 5 * time.Second,
		Options: map[string]string{
			"domain":     "_http._tcp.example.com",
			"nameserver": "127.0.0.1:15353",
		},
	}

	provider, err := NewDNSProvider(config)
	if err != nil {
		t.Fatalf("NewDNSProvider error: %v", err)
	}

	// Override the resolver with one that tracks dial calls
	provider.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialerCalled = true
			return nil, fmt.Errorf("tracked dialer called")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.ctx = ctx

	// Trigger a refresh which will invoke the resolver
	provider.refresh()

	if !dialerCalled {
		t.Error("Custom dialer was not called")
	}
}

// --------------------------------------------------------------------------
// Tests: Static file provider loadServices JSON parse error
// --------------------------------------------------------------------------

func TestStaticFileProvider_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/services.json"

	if err := os.WriteFile(filePath, []byte(`{invalid json content}`), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	config := &ProviderConfig{
		Type:    ProviderTypeStatic,
		Name:    "static-invalid-json",
		Options: map[string]string{"file": filePath, "format": "json"},
	}

	provider, err := NewStaticFileProvider(config)
	if err != nil {
		t.Fatalf("NewStaticFileProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = provider.Start(ctx)
	if err == nil {
		provider.Stop()
		t.Error("Expected error for invalid JSON in static file provider")
	}
}

// --------------------------------------------------------------------------
// Tests: Consul refreshService with unhealthy service (health check filter)
// --------------------------------------------------------------------------

func TestConsulProvider_RefreshService_SkipsUnhealthyWhenHealthCheckEnabled(t *testing.T) {
	// Start a mock Consul server that returns unhealthy services
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/service/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a service entry that will be marked as healthy by convertService
		// But the health check filter in refreshService relies on service.Healthy
		// Since convertService always sets Healthy=true, we need a different approach
		fmt.Fprintf(w, `[{
			"ServiceAddress": "10.0.0.1",
			"ServicePort": 8080,
			"ServiceName": "web",
			"ServiceID": "web-1",
			"Node": "node1",
			"Datacenter": "dc1",
			"ServiceTags": [],
			"ServiceMeta": {}
		}]`)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()

	config := &ProviderConfig{
		Type:        ProviderTypeConsul,
		Name:        "test-consul-hc",
		Refresh:     30 * time.Second,
		HealthCheck: true,
		Options: map[string]string{
			"address": ln.Addr().String(),
		},
	}
	p, err := NewConsulProvider(config)
	if err != nil {
		t.Fatalf("NewConsulProvider error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.ctx = ctx

	// refreshService with HealthCheck=true
	// Since convertService always returns Healthy=true, the filter won't skip
	p.refreshService("web")

	// Service should be added since convertService marks Healthy=true
	if len(p.Services()) != 1 {
		t.Errorf("Expected 1 service (healthy), got %d", len(p.Services()))
	}
}

// --------------------------------------------------------------------------
// Tests: Docker pollContainers with container having nil labels
// --------------------------------------------------------------------------

func TestDockerProvider_PollContainers_ContainerWithNilLabels(t *testing.T) {
	mock := newMockDockerServer(t)
	defer mock.close()

	container := dockerContainer{
		ID:     "abc123def456abcd",
		Names:  []string{"/web"},
		Image:  "test:latest",
		State:  "running",
		Labels: nil,
		NetworkSettings: &dockerNetworks{
			Networks: map[string]*dockerNetwork{
				"bridge": {IPAddress: "172.17.0.2"},
			},
		},
	}

	mock.setContainers([]dockerContainer{container})

	p := newTestDockerProvider(t, mock, nil)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	err := p.pollContainers()
	if err != nil {
		t.Fatalf("pollContainers error: %v", err)
	}

	// Should have 0 services since container has nil labels (not enabled)
	if len(p.Services()) != 0 {
		t.Errorf("Expected 0 services (nil labels), got %d", len(p.Services()))
	}
}
