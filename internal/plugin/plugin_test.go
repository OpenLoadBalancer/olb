package plugin

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
)

// --------------------------------------------------------------------------
// Mock plugin for testing
// --------------------------------------------------------------------------

type mockPlugin struct {
	name    string
	version string

	initCalled  atomic.Bool
	startCalled atomic.Bool
	stopCalled  atomic.Bool
	stopOrder   *[]string // shared slice to track stop order

	initErr  error
	startErr error
	stopErr  error

	api PluginAPI
}

func newMockPlugin(name, version string) *mockPlugin {
	return &mockPlugin{name: name, version: version}
}

func (p *mockPlugin) Name() string    { return p.name }
func (p *mockPlugin) Version() string { return p.version }

func (p *mockPlugin) Init(api PluginAPI) error {
	p.initCalled.Store(true)
	p.api = api
	return p.initErr
}

func (p *mockPlugin) Start() error {
	p.startCalled.Store(true)
	return p.startErr
}

func (p *mockPlugin) Stop() error {
	p.stopCalled.Store(true)
	if p.stopOrder != nil {
		*p.stopOrder = append(*p.stopOrder, p.name)
	}
	return p.stopErr
}

// --------------------------------------------------------------------------
// Mock middleware
// --------------------------------------------------------------------------

type mockMiddleware struct {
	name string
}

func (m *mockMiddleware) Name() string { return m.name }
func (m *mockMiddleware) Handle(next http.Handler) http.Handler {
	return next
}

// --------------------------------------------------------------------------
// Mock balancer
// --------------------------------------------------------------------------

type mockBalancer struct {
	name string
}

func (b *mockBalancer) Name() string        { return b.name }
func (b *mockBalancer) Next(_ []string) int { return 0 }

// --------------------------------------------------------------------------
// Mock health checker
// --------------------------------------------------------------------------

type mockHealthChecker struct {
	name string
}

func (h *mockHealthChecker) Name() string         { return h.name }
func (h *mockHealthChecker) Check(_ string) error { return nil }

// --------------------------------------------------------------------------
// Mock discovery provider
// --------------------------------------------------------------------------

type mockDiscoveryProvider struct {
	name string
}

func (d *mockDiscoveryProvider) Name() string                            { return d.name }
func (d *mockDiscoveryProvider) Discover(_ string) ([]string, error)     { return nil, nil }
func (d *mockDiscoveryProvider) Watch(_ string) (<-chan []string, error) { return nil, nil }
func (d *mockDiscoveryProvider) Stop() error                             { return nil }

// --------------------------------------------------------------------------
// Tests: PluginManager creation & config defaults
// --------------------------------------------------------------------------

func TestDefaultPluginManagerConfig(t *testing.T) {
	cfg := DefaultPluginManagerConfig()

	if cfg.PluginDir != "plugins/" {
		t.Errorf("PluginDir = %q, want %q", cfg.PluginDir, "plugins/")
	}
	if !cfg.AutoLoad {
		t.Error("AutoLoad = false, want true")
	}
	if len(cfg.AllowedPlugins) != 0 {
		t.Errorf("AllowedPlugins length = %d, want 0", len(cfg.AllowedPlugins))
	}
}

func TestNewPluginManager(t *testing.T) {
	cfg := DefaultPluginManagerConfig()
	pm := NewPluginManager(cfg)

	if pm == nil {
		t.Fatal("NewPluginManager returned nil")
	}
	if pm.managerConfig.PluginDir != "plugins/" {
		t.Errorf("PluginDir = %q, want %q", pm.managerConfig.PluginDir, "plugins/")
	}
	if pm.eventBus == nil {
		t.Error("eventBus is nil")
	}
	if pm.logger == nil {
		t.Error("logger is nil")
	}
	if pm.metrics == nil {
		t.Error("metrics is nil")
	}
	if pm.config == nil {
		t.Error("config is nil")
	}
	if len(pm.plugins) != 0 {
		t.Errorf("plugins count = %d, want 0", len(pm.plugins))
	}
}

// --------------------------------------------------------------------------
// Tests: RegisterPlugin
// --------------------------------------------------------------------------

func TestRegisterPlugin(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("test-plugin", "1.0.0")

	err := pm.RegisterPlugin(p)
	if err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	// Verify it's registered
	got, ok := pm.GetPlugin("test-plugin")
	if !ok {
		t.Fatal("GetPlugin() returned false")
	}
	if got.Name() != "test-plugin" {
		t.Errorf("Name() = %q, want %q", got.Name(), "test-plugin")
	}
}

func TestRegisterPlugin_DuplicateName(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p1 := newMockPlugin("duplicate", "1.0.0")
	p2 := newMockPlugin("duplicate", "2.0.0")

	if err := pm.RegisterPlugin(p1); err != nil {
		t.Fatalf("first RegisterPlugin() error = %v", err)
	}

	err := pm.RegisterPlugin(p2)
	if err == nil {
		t.Fatal("second RegisterPlugin() should have returned an error")
	}
}

func TestRegisterPlugin_NotAllowed(t *testing.T) {
	cfg := PluginManagerConfig{
		AllowedPlugins: []string{"allowed-plugin"},
	}
	pm := NewPluginManager(cfg)
	p := newMockPlugin("blocked-plugin", "1.0.0")

	err := pm.RegisterPlugin(p)
	if err == nil {
		t.Fatal("RegisterPlugin() should have returned an error for non-allowed plugin")
	}
}

func TestRegisterPlugin_Allowed(t *testing.T) {
	cfg := PluginManagerConfig{
		AllowedPlugins: []string{"allowed-plugin"},
	}
	pm := NewPluginManager(cfg)
	p := newMockPlugin("allowed-plugin", "1.0.0")

	err := pm.RegisterPlugin(p)
	if err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Plugin Init/Start/Stop lifecycle
// --------------------------------------------------------------------------

func TestPluginLifecycle_InitStartStop(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("lifecycle", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	if err := pm.StartAll(); err != nil {
		t.Fatalf("StartAll() error = %v", err)
	}

	if !p.initCalled.Load() {
		t.Error("Init() was not called")
	}
	if !p.startCalled.Load() {
		t.Error("Start() was not called")
	}

	if err := pm.StopAll(); err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}

	if !p.stopCalled.Load() {
		t.Error("Stop() was not called")
	}
}

func TestPluginLifecycle_InitError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("fail-init", "1.0.0")
	p.initErr = fmt.Errorf("init failed")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	err := pm.StartAll()
	if err == nil {
		t.Fatal("StartAll() should have returned an error")
	}
}

func TestPluginLifecycle_StartError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("fail-start", "1.0.0")
	p.startErr = fmt.Errorf("start failed")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	err := pm.StartAll()
	if err == nil {
		t.Fatal("StartAll() should have returned an error")
	}
}

// --------------------------------------------------------------------------
// Tests: StopAll reverse order
// --------------------------------------------------------------------------

func TestStopAll_ReverseOrder(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	var stopOrder []string

	p1 := newMockPlugin("first", "1.0.0")
	p1.stopOrder = &stopOrder
	p2 := newMockPlugin("second", "1.0.0")
	p2.stopOrder = &stopOrder
	p3 := newMockPlugin("third", "1.0.0")
	p3.stopOrder = &stopOrder

	// Register in order: first, second, third
	for _, p := range []Plugin{p1, p2, p3} {
		if err := pm.RegisterPlugin(p); err != nil {
			t.Fatalf("RegisterPlugin() error = %v", err)
		}
	}

	if err := pm.StartAll(); err != nil {
		t.Fatalf("StartAll() error = %v", err)
	}

	if err := pm.StopAll(); err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}

	// Should stop in reverse order: third, second, first
	expected := []string{"third", "second", "first"}
	if len(stopOrder) != len(expected) {
		t.Fatalf("stop order length = %d, want %d", len(stopOrder), len(expected))
	}
	for i, name := range expected {
		if stopOrder[i] != name {
			t.Errorf("stop order[%d] = %q, want %q", i, stopOrder[i], name)
		}
	}
}

func TestStopAll_ErrorContinues(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	p1 := newMockPlugin("ok-plugin", "1.0.0")
	p2 := newMockPlugin("err-plugin", "1.0.0")
	p2.stopErr = fmt.Errorf("stop error")

	if err := pm.RegisterPlugin(p1); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	if err := pm.RegisterPlugin(p2); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	if err := pm.StartAll(); err != nil {
		t.Fatalf("StartAll() error = %v", err)
	}

	err := pm.StopAll()
	if err == nil {
		t.Fatal("StopAll() should have returned an error")
	}

	// Both plugins should have had Stop called despite the error
	if !p1.stopCalled.Load() {
		t.Error("p1.Stop() was not called")
	}
	if !p2.stopCalled.Load() {
		t.Error("p2.Stop() was not called")
	}
}

// --------------------------------------------------------------------------
// Tests: ListPlugins & GetPlugin
// --------------------------------------------------------------------------

func TestListPlugins(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	if err := pm.RegisterPlugin(newMockPlugin("alpha", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	if err := pm.RegisterPlugin(newMockPlugin("beta", "2.0.0")); err != nil {
		t.Fatal(err)
	}

	infos := pm.ListPlugins()
	if len(infos) != 2 {
		t.Fatalf("ListPlugins() length = %d, want 2", len(infos))
	}

	// Should be in load order: alpha, beta
	if infos[0].Name != "alpha" {
		t.Errorf("infos[0].Name = %q, want %q", infos[0].Name, "alpha")
	}
	if infos[1].Name != "beta" {
		t.Errorf("infos[1].Name = %q, want %q", infos[1].Name, "beta")
	}
	if infos[0].Version != "1.0.0" {
		t.Errorf("infos[0].Version = %q, want %q", infos[0].Version, "1.0.0")
	}
	if infos[1].Version != "2.0.0" {
		t.Errorf("infos[1].Version = %q, want %q", infos[1].Version, "2.0.0")
	}
}

func TestGetPlugin_NotFound(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	_, ok := pm.GetPlugin("nonexistent")
	if ok {
		t.Error("GetPlugin() should return false for nonexistent plugin")
	}
}

// --------------------------------------------------------------------------
// Tests: PluginAPI factory registration
// --------------------------------------------------------------------------

func TestPluginAPI_RegisterMiddleware(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("mw-plugin", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}

	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	// Plugin should have received the API during Init
	if p.api == nil {
		t.Fatal("plugin API is nil after Init")
	}

	factory := func(cfg map[string]interface{}) (Middleware, error) {
		return &mockMiddleware{name: "custom-mw"}, nil
	}

	err := p.api.RegisterMiddleware("custom-mw", factory)
	if err != nil {
		t.Fatalf("RegisterMiddleware() error = %v", err)
	}

	// Verify retrieval
	f, ok := pm.GetMiddlewareFactory("custom-mw")
	if !ok {
		t.Fatal("GetMiddlewareFactory() returned false")
	}

	mw, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if mw.Name() != "custom-mw" {
		t.Errorf("middleware Name() = %q, want %q", mw.Name(), "custom-mw")
	}
}

func TestPluginAPI_RegisterMiddleware_Duplicate(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("mw-dup", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	factory := func(cfg map[string]interface{}) (Middleware, error) {
		return &mockMiddleware{name: "dup"}, nil
	}

	if err := p.api.RegisterMiddleware("dup", factory); err != nil {
		t.Fatal(err)
	}

	err := p.api.RegisterMiddleware("dup", factory)
	if err == nil {
		t.Fatal("second RegisterMiddleware() should have returned an error")
	}
}

func TestPluginAPI_RegisterBalancer(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("bal-plugin", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	factory := func(cfg map[string]interface{}) (Balancer, error) {
		return &mockBalancer{name: "custom-balancer"}, nil
	}

	err := p.api.RegisterBalancer("custom-balancer", factory)
	if err != nil {
		t.Fatalf("RegisterBalancer() error = %v", err)
	}

	f, ok := pm.GetBalancerFactory("custom-balancer")
	if !ok {
		t.Fatal("GetBalancerFactory() returned false")
	}

	b, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if b.Name() != "custom-balancer" {
		t.Errorf("balancer Name() = %q, want %q", b.Name(), "custom-balancer")
	}
}

func TestPluginAPI_RegisterHealthCheck(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("hc-plugin", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	factory := func(cfg map[string]interface{}) (HealthChecker, error) {
		return &mockHealthChecker{name: "custom-hc"}, nil
	}

	err := p.api.RegisterHealthCheck("custom-hc", factory)
	if err != nil {
		t.Fatalf("RegisterHealthCheck() error = %v", err)
	}

	f, ok := pm.GetHealthCheckFactory("custom-hc")
	if !ok {
		t.Fatal("GetHealthCheckFactory() returned false")
	}

	hc, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if hc.Name() != "custom-hc" {
		t.Errorf("health checker Name() = %q, want %q", hc.Name(), "custom-hc")
	}
}

func TestPluginAPI_RegisterDiscovery(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("disc-plugin", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	factory := func(cfg map[string]interface{}) (DiscoveryProvider, error) {
		return &mockDiscoveryProvider{name: "custom-disc"}, nil
	}

	err := p.api.RegisterDiscovery("custom-disc", factory)
	if err != nil {
		t.Fatalf("RegisterDiscovery() error = %v", err)
	}

	f, ok := pm.GetDiscoveryFactory("custom-disc")
	if !ok {
		t.Fatal("GetDiscoveryFactory() returned false")
	}

	dp, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if dp.Name() != "custom-disc" {
		t.Errorf("discovery provider Name() = %q, want %q", dp.Name(), "custom-disc")
	}
}

func TestPluginAPI_LoggerMetricsConfig(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("api-test", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	if p.api.Logger() == nil {
		t.Error("Logger() returned nil")
	}
	if p.api.Metrics() == nil {
		t.Error("Metrics() returned nil")
	}
	if p.api.Config() == nil {
		t.Error("Config() returned nil")
	}
}

// --------------------------------------------------------------------------
// Tests: EventBus
// --------------------------------------------------------------------------

func TestEventBus_SubscribePublish(t *testing.T) {
	eb := NewEventBus()
	var received Event
	var called bool

	eb.Subscribe("test.event", func(e Event) {
		received = e
		called = true
	})

	eb.Publish("test.event", "hello")

	if !called {
		t.Fatal("handler was not called")
	}
	if received.Topic != "test.event" {
		t.Errorf("Topic = %q, want %q", received.Topic, "test.event")
	}
	if received.Data != "hello" {
		t.Errorf("Data = %v, want %q", received.Data, "hello")
	}
	if received.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus()
	callCount := 0

	id := eb.Subscribe("test.event", func(e Event) {
		callCount++
	})

	eb.Publish("test.event", nil)
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}

	eb.Unsubscribe(id)

	eb.Publish("test.event", nil)
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1 (handler should not be called after unsubscribe)", callCount)
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	eb := NewEventBus()
	var count atomic.Int32

	eb.Subscribe("multi", func(e Event) {
		count.Add(1)
	})
	eb.Subscribe("multi", func(e Event) {
		count.Add(1)
	})
	eb.Subscribe("multi", func(e Event) {
		count.Add(1)
	})

	eb.Publish("multi", nil)

	if count.Load() != 3 {
		t.Errorf("count = %d, want 3", count.Load())
	}
}

func TestEventBus_NoSubscribers(t *testing.T) {
	eb := NewEventBus()
	// Should not panic when publishing to a topic with no subscribers
	eb.Publish("no-listeners", "data")
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	eb := NewEventBus()
	var count atomic.Int64

	eb.Subscribe("concurrent", func(e Event) {
		count.Add(1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				eb.Publish("concurrent", j)
			}
		}()
	}
	wg.Wait()

	if count.Load() != 10000 {
		t.Errorf("count = %d, want 10000", count.Load())
	}
}

func TestEventBus_SubscribeReturnsUniqueIDs(t *testing.T) {
	eb := NewEventBus()
	handler := func(e Event) {}

	id1 := eb.Subscribe("topic", handler)
	id2 := eb.Subscribe("topic", handler)
	id3 := eb.Subscribe("other", handler)

	if id1 == id2 {
		t.Error("id1 should not equal id2")
	}
	if id2 == id3 {
		t.Error("id2 should not equal id3")
	}
}

func TestEventBus_UnsubscribeNonexistent(t *testing.T) {
	eb := NewEventBus()
	// Should not panic
	eb.Unsubscribe("nonexistent-id")
}

func TestEventBus_PluginAPISubscribePublish(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("event-plugin", "1.0.0")

	if err := pm.RegisterPlugin(p); err != nil {
		t.Fatal(err)
	}
	if err := pm.StartAll(); err != nil {
		t.Fatal(err)
	}

	var received Event
	p.api.Subscribe(EventConfigReload, func(e Event) {
		received = e
	})

	p.api.Publish(EventConfigReload, "new-config")

	if received.Topic != EventConfigReload {
		t.Errorf("Topic = %q, want %q", received.Topic, EventConfigReload)
	}
	if received.Data != "new-config" {
		t.Errorf("Data = %v, want %q", received.Data, "new-config")
	}
}

// --------------------------------------------------------------------------
// Tests: LoadDir
// --------------------------------------------------------------------------

func TestLoadDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(pm.ListPlugins()) != 0 {
		t.Errorf("plugins count = %d, want 0", len(pm.ListPlugins()))
	}
}

func TestLoadDir_NonexistentDir(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir("/nonexistent/plugin/directory")
	if err != nil {
		t.Fatalf("LoadDir() should not error for nonexistent directory, got: %v", err)
	}
}

func TestLoadDir_SkipsNonSoFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some non-.so files
	for _, name := range []string{"readme.txt", "config.yaml", "script.sh"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(pm.ListPlugins()) != 0 {
		t.Errorf("plugins count = %d, want 0 (non-.so files should be skipped)", len(pm.ListPlugins()))
	}
}

// --------------------------------------------------------------------------
// Tests: EventBus with built-in event constants
// --------------------------------------------------------------------------

func TestBuiltinEventConstants(t *testing.T) {
	// Verify the constants are defined correctly
	events := []string{
		EventConfigReload,
		EventBackendAdded,
		EventBackendRemoved,
		EventBackendStateChange,
		EventRouteAdded,
		EventRouteRemoved,
		EventHealthCheckResult,
	}

	expected := []string{
		"config.reload",
		"backend.added",
		"backend.removed",
		"backend.state_change",
		"route.added",
		"route.removed",
		"health.check_result",
	}

	for i, e := range events {
		if e != expected[i] {
			t.Errorf("event[%d] = %q, want %q", i, e, expected[i])
		}
	}
}

// --------------------------------------------------------------------------
// Tests: PluginInfo
// --------------------------------------------------------------------------

func TestPluginInfo_Fields(t *testing.T) {
	info := PluginInfo{
		Name:        "test",
		Version:     "1.0.0",
		Description: "A test plugin",
		Author:      "Author Name",
		License:     "MIT",
	}

	if info.Name != "test" {
		t.Errorf("Name = %q, want %q", info.Name, "test")
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.0.0")
	}
	if info.Description != "A test plugin" {
		t.Errorf("Description = %q, want %q", info.Description, "A test plugin")
	}
	if info.Author != "Author Name" {
		t.Errorf("Author = %q, want %q", info.Author, "Author Name")
	}
	if info.License != "MIT" {
		t.Errorf("License = %q, want %q", info.License, "MIT")
	}
}

// --------------------------------------------------------------------------
// Tests: Event struct
// --------------------------------------------------------------------------

func TestEvent_Struct(t *testing.T) {
	now := time.Now()
	e := Event{
		Topic:     "test.topic",
		Data:      42,
		Timestamp: now,
	}

	if e.Topic != "test.topic" {
		t.Errorf("Topic = %q, want %q", e.Topic, "test.topic")
	}
	if e.Data != 42 {
		t.Errorf("Data = %v, want 42", e.Data)
	}
	if e.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, now)
	}
}

// --------------------------------------------------------------------------
// Tests: PluginManager EventBus accessor
// --------------------------------------------------------------------------

func TestPluginManager_EventBus(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	eb := pm.EventBus()

	if eb == nil {
		t.Fatal("EventBus() returned nil")
	}

	// Verify it's the same instance
	if eb != pm.eventBus {
		t.Error("EventBus() should return the manager's event bus instance")
	}
}

// --------------------------------------------------------------------------
// Tests: Multiple plugins full integration
// --------------------------------------------------------------------------

func TestMultiplePlugins_FullIntegration(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	// Register 3 plugins
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("plugin-%d", i)
		if err := pm.RegisterPlugin(newMockPlugin(name, "1.0.0")); err != nil {
			t.Fatalf("RegisterPlugin(%q) error = %v", name, err)
		}
	}

	// List should show all 3
	infos := pm.ListPlugins()
	if len(infos) != 3 {
		t.Fatalf("ListPlugins() length = %d, want 3", len(infos))
	}

	// Start all
	if err := pm.StartAll(); err != nil {
		t.Fatalf("StartAll() error = %v", err)
	}

	// All should be gettable
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("plugin-%d", i)
		p, ok := pm.GetPlugin(name)
		if !ok {
			t.Errorf("GetPlugin(%q) returned false", name)
		}
		if p.Name() != name {
			t.Errorf("Name() = %q, want %q", p.Name(), name)
		}
	}

	// Stop all
	if err := pm.StopAll(); err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Concurrent access safety
// --------------------------------------------------------------------------

func TestPluginManager_SetLogger(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	// The default logger is set during NewPluginManager
	if pm.logger == nil {
		t.Fatal("logger should not be nil after creation")
	}

	// Create a new logger and set it
	newLogger := logging.NewWithDefaults()
	pm.SetLogger(newLogger)

	if pm.logger != newLogger {
		t.Error("SetLogger did not update the logger")
	}
}

func TestPluginManager_SetMetrics(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	if pm.metrics == nil {
		t.Fatal("metrics should not be nil after creation")
	}

	newRegistry := metrics.NewRegistry()
	pm.SetMetrics(newRegistry)

	if pm.metrics != newRegistry {
		t.Error("SetMetrics did not update the metrics registry")
	}
}

func TestPluginManager_SetConfig(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	if pm.config == nil {
		t.Fatal("config should not be nil after creation")
	}

	newConfig := &config.Config{}
	pm.SetConfig(newConfig)

	if pm.config != newConfig {
		t.Error("SetConfig did not update the config")
	}
}

func TestPluginManager_LoadPlugin_NonExistentFile(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	err := pm.LoadPlugin("/nonexistent/path/plugin.so")
	if err == nil {
		t.Error("LoadPlugin should return error for non-existent .so file")
	}
}

func TestPluginManager_LoadPlugin_InvalidFile(t *testing.T) {
	// Create a temp file that is not a valid .so
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.so")
	if err := os.WriteFile(path, []byte("not a plugin"), 0644); err != nil {
		t.Fatal(err)
	}

	pm := NewPluginManager(DefaultPluginManagerConfig())

	err := pm.LoadPlugin(path)
	if err == nil {
		t.Error("LoadPlugin should return error for invalid .so file")
	}
}

func TestPluginManager_ConcurrentAccess(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	// Pre-register some plugins
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("concurrent-%d", i)
		if err := pm.RegisterPlugin(newMockPlugin(name, "1.0.0")); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pm.ListPlugins()
			pm.GetPlugin(fmt.Sprintf("concurrent-%d", i%5))
		}(i)
	}

	wg.Wait()
}
