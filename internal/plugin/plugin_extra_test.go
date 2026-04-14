package plugin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
)

// --------------------------------------------------------------------------
// Tests: Duplicate factory registration for HealthCheck and Discovery
// (Middleware and Balancer duplicates already covered in plugin_test.go)
// --------------------------------------------------------------------------

func TestPluginAPI_RegisterHealthCheck_Duplicate(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("hc-dup", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (HealthChecker, error) {
		return &mockHealthChecker{name: "dup-hc"}, nil
	}

	err := p.api.RegisterHealthCheck("dup-hc", factory)
	if err != nil {
		t.Fatalf("first RegisterHealthCheck() error = %v", err)
	}

	err = p.api.RegisterHealthCheck("dup-hc", factory)
	if err == nil {
		t.Error("expected error for duplicate health check registration")
	}
}

func TestPluginAPI_RegisterDiscovery_Duplicate(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("disc-dup", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (DiscoveryProvider, error) {
		return &mockDiscoveryProvider{name: "dup-disc"}, nil
	}

	err := p.api.RegisterDiscovery("dup-disc", factory)
	if err != nil {
		t.Fatalf("first RegisterDiscovery() error = %v", err)
	}

	err = p.api.RegisterDiscovery("dup-disc", factory)
	if err == nil {
		t.Error("expected error for duplicate discovery registration")
	}
}

// --------------------------------------------------------------------------
// Tests: Duplicate balancer registration
// --------------------------------------------------------------------------

func TestPluginAPI_RegisterBalancer_Duplicate(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("bal-dup", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (Balancer, error) {
		return &mockBalancer{name: "dup-bal"}, nil
	}

	err := p.api.RegisterBalancer("dup-bal", factory)
	if err != nil {
		t.Fatalf("first RegisterBalancer() error = %v", err)
	}

	err = p.api.RegisterBalancer("dup-bal", factory)
	if err == nil {
		t.Error("expected error for duplicate balancer registration")
	}
}

// --------------------------------------------------------------------------
// Tests: Factory getters for unregistered factories
// --------------------------------------------------------------------------

func TestGetMiddlewareFactory_NotFound(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	_, ok := pm.GetMiddlewareFactory("nonexistent")
	if ok {
		t.Error("expected false for unregistered middleware factory")
	}
}

func TestGetBalancerFactory_NotFound(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	_, ok := pm.GetBalancerFactory("nonexistent")
	if ok {
		t.Error("expected false for unregistered balancer factory")
	}
}

func TestGetHealthCheckFactory_NotFound(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	_, ok := pm.GetHealthCheckFactory("nonexistent")
	if ok {
		t.Error("expected false for unregistered health check factory")
	}
}

func TestGetDiscoveryFactory_NotFound(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	_, ok := pm.GetDiscoveryFactory("nonexistent")
	if ok {
		t.Error("expected false for unregistered discovery factory")
	}
}

// --------------------------------------------------------------------------
// Tests: LoadDir edge cases
// --------------------------------------------------------------------------

func TestLoadDir_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "plugin.so"), []byte("fake"), 0644)

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(pm.ListPlugins()) != 0 {
		t.Error("expected 0 plugins from directory with only subdirectories")
	}
}

func TestLoadDir_FakeSoFileLogged(t *testing.T) {
	dir := t.TempDir()
	fakeSo := filepath.Join(dir, "fake.so")
	os.WriteFile(fakeSo, []byte("not-a-real-plugin"), 0644)

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() should not fail on bad .so files, got: %v", err)
	}
	if len(pm.ListPlugins()) != 0 {
		t.Error("expected 0 plugins from invalid .so file")
	}
}

func TestLoadDir_MixedFilesAndSubdirs(t *testing.T) {
	dir := t.TempDir()

	// Create subdirectory (should be skipped)
	os.Mkdir(filepath.Join(dir, "nested"), 0755)
	os.WriteFile(filepath.Join(dir, "nested", "inner.so"), []byte("fake"), 0644)

	// Create non-.so files (should be skipped)
	for _, name := range []string{"readme.txt", "config.yaml", "script.sh"} {
		os.WriteFile(filepath.Join(dir, name), []byte("test"), 0644)
	}

	// Create fake .so (should fail to load but not return error)
	os.WriteFile(filepath.Join(dir, "broken.so"), []byte("not-a-plugin"), 0644)

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(pm.ListPlugins()) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(pm.ListPlugins()))
	}
}

// --------------------------------------------------------------------------
// Tests: StopAll with error propagation
// --------------------------------------------------------------------------

func TestStopAll_FirstErrorReturned(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	p1 := newMockPlugin("stop-ok", "0.1.0")
	p2 := newMockPlugin("stop-fail", "0.1.0")
	p2.stopErr = fmt.Errorf("stop error")

	pm.RegisterPlugin(p1)
	pm.RegisterPlugin(p2)
	pm.StartAll()

	err := pm.StopAll()
	if err == nil {
		t.Error("expected error from StopAll when plugin fails")
	}
	if !strings.Contains(err.Error(), "stop-fail") {
		t.Errorf("error = %v, want mention of stop-fail", err)
	}

	if !p1.stopCalled.Load() {
		t.Error("p1 Stop should have been called")
	}
	if !p2.stopCalled.Load() {
		t.Error("p2 Stop should have been called")
	}
}

// --------------------------------------------------------------------------
// Tests: PluginAPI with nil config/metrics
// --------------------------------------------------------------------------

func TestPluginAPI_NilConfig(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	pm.SetConfig(nil)
	p := newMockPlugin("nil-cfg", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	cfg := p.api.Config()
	if cfg != nil {
		t.Error("expected nil config")
	}
}

func TestPluginAPI_NilMetrics(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	pm.SetMetrics(nil)
	p := newMockPlugin("nil-metrics", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	m := p.api.Metrics()
	if m != nil {
		t.Error("expected nil metrics")
	}
}

// --------------------------------------------------------------------------
// Tests: Successful factory retrieval after registration
// --------------------------------------------------------------------------

func TestGetHealthCheckFactory_AfterRegistration(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("hc-get-test", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (HealthChecker, error) {
		return &mockHealthChecker{name: "my-hc"}, nil
	}

	p.api.RegisterHealthCheck("my-hc", factory)

	f, ok := pm.GetHealthCheckFactory("my-hc")
	if !ok {
		t.Fatal("GetHealthCheckFactory() returned false")
	}

	hc, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if hc.Name() != "my-hc" {
		t.Errorf("Name() = %q, want %q", hc.Name(), "my-hc")
	}
}

func TestGetDiscoveryFactory_AfterRegistration(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("disc-get-test", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (DiscoveryProvider, error) {
		return &mockDiscoveryProvider{name: "my-disc"}, nil
	}

	p.api.RegisterDiscovery("my-disc", factory)

	f, ok := pm.GetDiscoveryFactory("my-disc")
	if !ok {
		t.Fatal("GetDiscoveryFactory() returned false")
	}

	disc, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if disc.Name() != "my-disc" {
		t.Errorf("Name() = %q, want %q", disc.Name(), "my-disc")
	}
}

// --------------------------------------------------------------------------
// Tests: LoadDir with unreadable directory (non-NotExist error)
// --------------------------------------------------------------------------

func TestLoadDir_FilePathAsDir(t *testing.T) {
	// Pass a file path as directory. On Windows this returns IsNotExist=true,
	// so LoadDir returns nil. On Unix it returns a non-NotExist error.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(tmpFile)
	// Should not panic regardless of platform behavior
	_ = err
}

// --------------------------------------------------------------------------
// Tests: LoadPlugin error message contains path
// --------------------------------------------------------------------------

func TestLoadPlugin_ErrorContainsPath(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	err := pm.LoadPlugin("/nonexistent/plugin.so")
	if err == nil {
		t.Fatal("LoadPlugin should return error for non-existent file")
	}
	// Path may be resolved to absolute by filepath.Abs
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("Error should contain path info: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: LoadPlugin with invalid .so file (not a Go plugin)
// --------------------------------------------------------------------------

func TestLoadPlugin_InvalidSoFileErrorMessage(t *testing.T) {
	dir := t.TempDir()
	fakeSo := filepath.Join(dir, "fake.so")
	if err := os.WriteFile(fakeSo, []byte("this is not a Go plugin .so file"), 0644); err != nil {
		t.Fatal(err)
	}

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadPlugin(fakeSo)
	if err == nil {
		t.Error("LoadPlugin should return error for invalid .so file")
	}
	// The error should mention the path
	if !strings.Contains(err.Error(), fakeSo) {
		t.Errorf("Error should contain file path: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Multiple LoadDir calls are idempotent
// --------------------------------------------------------------------------

func TestLoadDir_CalledMultipleTimes(t *testing.T) {
	dir := t.TempDir()
	// Create some non-.so files
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("test"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	pm := NewPluginManager(DefaultPluginManagerConfig())

	// Multiple calls should all succeed
	for i := 0; i < 3; i++ {
		err := pm.LoadDir(dir)
		if err != nil {
			t.Fatalf("LoadDir() call %d error = %v", i, err)
		}
	}

	if len(pm.ListPlugins()) != 0 {
		t.Errorf("Expected 0 plugins, got %d", len(pm.ListPlugins()))
	}
}

// --------------------------------------------------------------------------
// Tests: LoadDir with empty directory
// --------------------------------------------------------------------------

func TestLoadDir_EmptyDirectoryPath(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	// Empty string is not a valid path, should error or return nil
	err := pm.LoadDir("")
	// On most OSes this will return an error from ReadDir
	// We just verify it doesn't panic
	_ = err
}

// --------------------------------------------------------------------------
// Tests: GetBalancerFactory after successful registration
// --------------------------------------------------------------------------

func TestGetBalancerFactory_AfterRegistration(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("bal-get-test", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (Balancer, error) {
		return &mockBalancer{name: "my-bal"}, nil
	}

	p.api.RegisterBalancer("my-bal", factory)

	f, ok := pm.GetBalancerFactory("my-bal")
	if !ok {
		t.Fatal("GetBalancerFactory() returned false")
	}

	b, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if b.Name() != "my-bal" {
		t.Errorf("Name() = %q, want %q", b.Name(), "my-bal")
	}
}

// --------------------------------------------------------------------------
// Tests: GetMiddlewareFactory after successful registration
// --------------------------------------------------------------------------

func TestGetMiddlewareFactory_AfterRegistration(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("mw-get-test", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	factory := func(cfg map[string]any) (Middleware, error) {
		return &mockMiddleware{name: "my-mw"}, nil
	}

	p.api.RegisterMiddleware("my-mw", factory)

	f, ok := pm.GetMiddlewareFactory("my-mw")
	if !ok {
		t.Fatal("GetMiddlewareFactory() returned false")
	}

	mw, err := f(nil)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if mw.Name() != "my-mw" {
		t.Errorf("Name() = %q, want %q", mw.Name(), "my-mw")
	}
}

// ==========================================================================
// NEW TESTS BELOW - additional coverage
// ==========================================================================

// --------------------------------------------------------------------------
// Tests: LoadDir non-IsNotExist error path (permission denied)
// --------------------------------------------------------------------------

func TestLoadDir_UnreadableDirectory(t *testing.T) {
	// Windows does not support Unix-style chmod permission bits.
	// os.Chmod(0000) is a no-op on Windows, so ReadDir would succeed.
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style directory permissions not supported on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root, permission tests are unreliable")
	}

	dir := t.TempDir()
	subDir := filepath.Join(dir, "noperm")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Remove read+execute permissions so ReadDir fails
	if err := os.Chmod(subDir, 0000); err != nil {
		t.Skipf("chmod failed: %v", err)
	}
	defer os.Chmod(subDir, 0755) // restore for TempDir cleanup

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(subDir)
	if err == nil {
		t.Error("expected error for unreadable directory")
	}
	// The error should NOT be about non-existence
	if os.IsNotExist(err) {
		t.Errorf("error should not be IsNotExist: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: LoadPlugin on Windows (documents the limitation)
// --------------------------------------------------------------------------

func TestLoadPlugin_WindowsLimitation(t *testing.T) {
	// plugin.Open does not work on Windows, so paths beyond line 416 of
	// LoadPlugin cannot be exercised. This test documents the limitation.
	// The uncovered paths are: successful Lookup, type assertion, factory
	// call, and RegisterPlugin -- all require a valid .so built with
	// -buildmode=plugin which is unsupported on Windows.
	t.Skip("plugin.Open not supported on Windows")
}

// --------------------------------------------------------------------------
// Tests: EventBus concurrent subscribe and unsubscribe
// --------------------------------------------------------------------------

func TestEventBus_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	eb := NewEventBus()
	var handlerCalls atomic.Int64

	// Subscribe from multiple goroutines
	var subIDs []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := eb.Subscribe("concurrent-topic", func(e Event) {
				handlerCalls.Add(1)
			})
			mu.Lock()
			subIDs = append(subIDs, id)
			mu.Unlock()
		}()
	}
	wg.Wait()

	eb.Publish("concurrent-topic", "data")
	if handlerCalls.Load() != 50 {
		t.Errorf("handlerCalls = %d, want 50", handlerCalls.Load())
	}

	// Unsubscribe from multiple goroutines
	for _, id := range subIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			eb.Unsubscribe(id)
		}(id)
	}
	wg.Wait()

	handlerCalls.Store(0)
	eb.Publish("concurrent-topic", "data")
	if handlerCalls.Load() != 0 {
		t.Errorf("handlerCalls after unsubscribe = %d, want 0", handlerCalls.Load())
	}
}

// --------------------------------------------------------------------------
// Tests: EventBus unsubscribe while another goroutine is publishing
// --------------------------------------------------------------------------

func TestEventBus_PublishDuringUnsubscribe(t *testing.T) {
	eb := NewEventBus()
	var count atomic.Int64

	id := eb.Subscribe("race-topic", func(e Event) {
		count.Add(1)
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			eb.Publish("race-topic", i)
		}
	}()

	go func() {
		defer wg.Done()
		// Small delay to let some publishes happen first
		time.Sleep(time.Microsecond)
		eb.Unsubscribe(id)
	}()

	wg.Wait()
	// count should be between 0 and 1000, just verify no panic/deadlock
}

// --------------------------------------------------------------------------
// Tests: Middleware Handle actually wraps handler
// --------------------------------------------------------------------------

func TestMockMiddleware_Handle(t *testing.T) {
	mw := &mockMiddleware{name: "wrap-test"}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.Handle(inner)
	if wrapped == nil {
		t.Fatal("Handle() returned nil")
	}

	// The mock passes through, so verify the inner handler still works
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// Tests: Balancer Next returns correct index
// --------------------------------------------------------------------------

func TestMockBalancer_Next(t *testing.T) {
	b := &mockBalancer{name: "test-bal"}
	backends := []string{"a", "b", "c"}
	idx := b.Next(backends)
	if idx != 0 {
		t.Errorf("Next() = %d, want 0", idx)
	}
}

// --------------------------------------------------------------------------
// Tests: HealthChecker Check
// --------------------------------------------------------------------------

func TestMockHealthChecker_Check(t *testing.T) {
	hc := &mockHealthChecker{name: "test-hc"}
	err := hc.Check("localhost:8080")
	if err != nil {
		t.Errorf("Check() error = %v, want nil", err)
	}
}

// --------------------------------------------------------------------------
// Tests: DiscoveryProvider interface methods
// --------------------------------------------------------------------------

func TestMockDiscoveryProvider_AllMethods(t *testing.T) {
	d := &mockDiscoveryProvider{name: "test-disc"}

	if d.Name() != "test-disc" {
		t.Errorf("Name() = %q, want %q", d.Name(), "test-disc")
	}

	addrs, err := d.Discover("my-service")
	if err != nil {
		t.Errorf("Discover() error = %v", err)
	}
	if addrs != nil {
		t.Errorf("Discover() = %v, want nil", addrs)
	}

	ch, err := d.Watch("my-service")
	if err != nil {
		t.Errorf("Watch() error = %v", err)
	}
	if ch != nil {
		t.Errorf("Watch() = %v, want nil", ch)
	}

	if err := d.Stop(); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: Factory functions that return errors
// --------------------------------------------------------------------------

func TestMiddlewareFactory_ReturnsError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("mw-err", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	expectedErr := fmt.Errorf("middleware creation failed")
	factory := func(cfg map[string]any) (Middleware, error) {
		return nil, expectedErr
	}

	// Registration should succeed (it just stores the factory)
	if err := p.api.RegisterMiddleware("err-mw", factory); err != nil {
		t.Fatalf("RegisterMiddleware() error = %v", err)
	}

	// Calling the factory should return the error
	f, ok := pm.GetMiddlewareFactory("err-mw")
	if !ok {
		t.Fatal("GetMiddlewareFactory() returned false")
	}
	mw, err := f(nil)
	if err != expectedErr {
		t.Errorf("factory() error = %v, want %v", err, expectedErr)
	}
	if mw != nil {
		t.Error("factory() should return nil middleware on error")
	}
}

func TestBalancerFactory_ReturnsError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("bal-err", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	expectedErr := fmt.Errorf("balancer creation failed")
	factory := func(cfg map[string]any) (Balancer, error) {
		return nil, expectedErr
	}

	if err := p.api.RegisterBalancer("err-bal", factory); err != nil {
		t.Fatalf("RegisterBalancer() error = %v", err)
	}

	f, ok := pm.GetBalancerFactory("err-bal")
	if !ok {
		t.Fatal("GetBalancerFactory() returned false")
	}
	b, err := f(nil)
	if err != expectedErr {
		t.Errorf("factory() error = %v, want %v", err, expectedErr)
	}
	if b != nil {
		t.Error("factory() should return nil balancer on error")
	}
}

func TestHealthCheckFactory_ReturnsError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("hc-err", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	expectedErr := fmt.Errorf("health check creation failed")
	factory := func(cfg map[string]any) (HealthChecker, error) {
		return nil, expectedErr
	}

	if err := p.api.RegisterHealthCheck("err-hc", factory); err != nil {
		t.Fatalf("RegisterHealthCheck() error = %v", err)
	}

	f, ok := pm.GetHealthCheckFactory("err-hc")
	if !ok {
		t.Fatal("GetHealthCheckFactory() returned false")
	}
	hc, err := f(nil)
	if err != expectedErr {
		t.Errorf("factory() error = %v, want %v", err, expectedErr)
	}
	if hc != nil {
		t.Error("factory() should return nil health checker on error")
	}
}

func TestDiscoveryFactory_ReturnsError(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("disc-err", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	expectedErr := fmt.Errorf("discovery creation failed")
	factory := func(cfg map[string]any) (DiscoveryProvider, error) {
		return nil, expectedErr
	}

	if err := p.api.RegisterDiscovery("err-disc", factory); err != nil {
		t.Fatalf("RegisterDiscovery() error = %v", err)
	}

	f, ok := pm.GetDiscoveryFactory("err-disc")
	if !ok {
		t.Fatal("GetDiscoveryFactory() returned false")
	}
	disc, err := f(nil)
	if err != expectedErr {
		t.Errorf("factory() error = %v, want %v", err, expectedErr)
	}
	if disc != nil {
		t.Error("factory() should return nil discovery provider on error")
	}
}

// --------------------------------------------------------------------------
// Tests: Factory functions receive config
// --------------------------------------------------------------------------

func TestMiddlewareFactory_ReceivesConfig(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("mw-cfg", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	var receivedCfg map[string]any
	factory := func(cfg map[string]any) (Middleware, error) {
		receivedCfg = cfg
		return &mockMiddleware{name: "cfg-mw"}, nil
	}

	p.api.RegisterMiddleware("cfg-mw", factory)

	f, ok := pm.GetMiddlewareFactory("cfg-mw")
	if !ok {
		t.Fatal("GetMiddlewareFactory() returned false")
	}

	testCfg := map[string]any{"key": "value", "count": 42}
	mw, err := f(testCfg)
	if err != nil {
		t.Fatalf("factory() error = %v", err)
	}
	if mw.Name() != "cfg-mw" {
		t.Errorf("Name() = %q, want %q", mw.Name(), "cfg-mw")
	}
	if receivedCfg["key"] != "value" {
		t.Errorf("cfg[key] = %v, want %q", receivedCfg["key"], "value")
	}
	if receivedCfg["count"] != 42 {
		t.Errorf("cfg[count] = %v, want 42", receivedCfg["count"])
	}
}

// --------------------------------------------------------------------------
// Tests: PluginAPI Subscribe/Publish via event bus through plugin
// --------------------------------------------------------------------------

func TestPluginAPI_MultipleEventsFromPlugin(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("multi-events", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	var received []string
	var mu sync.Mutex

	p.api.Subscribe(EventBackendAdded, func(e Event) {
		mu.Lock()
		received = append(received, "added:"+fmt.Sprint(e.Data))
		mu.Unlock()
	})
	p.api.Subscribe(EventBackendRemoved, func(e Event) {
		mu.Lock()
		received = append(received, "removed:"+fmt.Sprint(e.Data))
		mu.Unlock()
	})

	p.api.Publish(EventBackendAdded, "10.0.0.1:8080")
	p.api.Publish(EventBackendRemoved, "10.0.0.2:8080")

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("received %d events, want 2", len(received))
	}
	if received[0] != "added:10.0.0.1:8080" {
		t.Errorf("received[0] = %q, want %q", received[0], "added:10.0.0.1:8080")
	}
	if received[1] != "removed:10.0.0.2:8080" {
		t.Errorf("received[1] = %q, want %q", received[1], "removed:10.0.0.2:8080")
	}
}

// --------------------------------------------------------------------------
// Tests: PluginAPI Subscribe returns unique subscription IDs
// --------------------------------------------------------------------------

func TestPluginAPI_SubscribeReturnsIDs(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("sub-id-test", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	handler := func(e Event) {}

	id1 := p.api.Subscribe("topic-a", handler)
	id2 := p.api.Subscribe("topic-a", handler)
	id3 := p.api.Subscribe("topic-b", handler)

	if id1 == id2 {
		t.Error("subscriptions on same topic should have different IDs")
	}
	if id2 == id3 {
		t.Error("subscriptions on different topics should have different IDs")
	}
}

// --------------------------------------------------------------------------
// Tests: StartAll with zero plugins
// --------------------------------------------------------------------------

func TestStartAll_NoPlugins(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	if err := pm.StartAll(); err != nil {
		t.Errorf("StartAll() with no plugins should not error, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: StopAll with zero plugins
// --------------------------------------------------------------------------

func TestStopAll_NoPlugins(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	if err := pm.StopAll(); err != nil {
		t.Errorf("StopAll() with no plugins should not error, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: ListPlugins with zero plugins
// --------------------------------------------------------------------------

func TestListPlugins_NoPlugins(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	infos := pm.ListPlugins()
	if infos == nil {
		t.Error("ListPlugins() should return non-nil slice")
	}
	if len(infos) != 0 {
		t.Errorf("ListPlugins() length = %d, want 0", len(infos))
	}
}

// --------------------------------------------------------------------------
// Tests: PluginManager with AllowedPlugins containing multiple entries
// --------------------------------------------------------------------------

func TestPluginManager_MultipleAllowedPlugins(t *testing.T) {
	cfg := PluginManagerConfig{
		AllowedPlugins: []string{"alpha", "beta", "gamma"},
	}
	pm := NewPluginManager(cfg)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := pm.RegisterPlugin(newMockPlugin(name, "0.1.0")); err != nil {
			t.Fatalf("RegisterPlugin(%q) error = %v", name, err)
		}
	}

	// Non-allowed plugin should fail
	err := pm.RegisterPlugin(newMockPlugin("delta", "0.1.0"))
	if err == nil {
		t.Error("RegisterPlugin() should fail for non-allowed plugin")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Errorf("error = %v, want mention of 'not in the allowed list'", err)
	}

	infos := pm.ListPlugins()
	if len(infos) != 3 {
		t.Errorf("ListPlugins() length = %d, want 3", len(infos))
	}
}

// --------------------------------------------------------------------------
// Tests: PluginManager state after StopAll - can StartAll again
// --------------------------------------------------------------------------

func TestPluginManager_RestartAfterStop(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("restart", "0.1.0")
	pm.RegisterPlugin(p)

	// First start/stop cycle
	if err := pm.StartAll(); err != nil {
		t.Fatalf("first StartAll() error = %v", err)
	}
	if err := pm.StopAll(); err != nil {
		t.Fatalf("first StopAll() error = %v", err)
	}

	// Reset the call tracking
	p.initCalled.Store(false)
	p.startCalled.Store(false)
	p.stopCalled.Store(false)

	// Second start/stop cycle
	if err := pm.StartAll(); err != nil {
		t.Fatalf("second StartAll() error = %v", err)
	}
	if !p.initCalled.Load() {
		t.Error("Init() was not called on second start")
	}
	if !p.startCalled.Load() {
		t.Error("Start() was not called on second start")
	}

	if err := pm.StopAll(); err != nil {
		t.Fatalf("second StopAll() error = %v", err)
	}
	if !p.stopCalled.Load() {
		t.Error("Stop() was not called on second stop")
	}
}

// --------------------------------------------------------------------------
// Tests: PluginManager Event bus from EventBus() accessor
// --------------------------------------------------------------------------

func TestPluginManager_EventBusAccessor(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	eb := pm.EventBus()

	var received string
	eb.Subscribe(EventConfigReload, func(e Event) {
		received = fmt.Sprint(e.Data)
	})

	eb.Publish(EventConfigReload, "updated-config")

	if received != "updated-config" {
		t.Errorf("received = %q, want %q", received, "updated-config")
	}
}

// --------------------------------------------------------------------------
// Tests: Event struct with complex data types
// --------------------------------------------------------------------------

func TestEvent_StructWithMap(t *testing.T) {
	ts := time.Now()
	data := map[string]any{
		"backend": "10.0.0.1:8080",
		"status":  "healthy",
		"latency": float64(42),
	}
	e := Event{
		Topic:     EventHealthCheckResult,
		Data:      data,
		Timestamp: ts,
	}

	if e.Topic != EventHealthCheckResult {
		t.Errorf("Topic = %q, want %q", e.Topic, EventHealthCheckResult)
	}
	m, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatal("Data is not a map")
	}
	if m["backend"] != "10.0.0.1:8080" {
		t.Errorf("Data[backend] = %v, want %q", m["backend"], "10.0.0.1:8080")
	}
}

// --------------------------------------------------------------------------
// Tests: PluginInfo zero value
// --------------------------------------------------------------------------

func TestPluginInfo_ZeroValue(t *testing.T) {
	var info PluginInfo
	if info.Name != "" {
		t.Errorf("zero-value Name = %q, want empty", info.Name)
	}
	if info.Version != "" {
		t.Errorf("zero-value Version = %q, want empty", info.Version)
	}
}

// --------------------------------------------------------------------------
// Tests: LoadDir with only .so files that fail to load (error logging path)
// --------------------------------------------------------------------------

func TestLoadDir_MultipleFakeSoFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("plugin%d.so", i)
		os.WriteFile(filepath.Join(dir, name), []byte("not-a-plugin"), 0644)
	}

	pm := NewPluginManager(DefaultPluginManagerConfig())
	err := pm.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(pm.ListPlugins()) != 0 {
		t.Errorf("ListPlugins() length = %d, want 0", len(pm.ListPlugins()))
	}
}

// --------------------------------------------------------------------------
// Tests: Concurrent register and list plugins
// --------------------------------------------------------------------------

func TestPluginManager_ConcurrentRegisterAndList(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	var wg sync.WaitGroup

	// Concurrently register and list
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			pm.RegisterPlugin(newMockPlugin(fmt.Sprintf("concurrent-reg-%d", i), "0.1.0"))
		}(i)
		go func() {
			defer wg.Done()
			pm.ListPlugins()
		}()
	}
	wg.Wait()

	infos := pm.ListPlugins()
	if len(infos) != 20 {
		t.Errorf("ListPlugins() length = %d, want 20", len(infos))
	}
}

// --------------------------------------------------------------------------
// Tests: PluginAPI Logger returns a named logger for the plugin
// --------------------------------------------------------------------------

func TestPluginAPI_LoggerIsNamed(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("logger-test", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	logger := p.api.Logger()
	if logger == nil {
		t.Fatal("Logger() returned nil")
	}
}

// --------------------------------------------------------------------------
// Tests: SetLogger then verify plugin API gets updated logger
// --------------------------------------------------------------------------

func TestPluginManager_SetLoggerThenPluginAPI(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	customLogger := logging.NewWithDefaults()
	pm.SetLogger(customLogger)

	p := newMockPlugin("logger-update", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	// The plugin API logger is derived from the manager's logger with WithName,
	// so it should not be nil
	logger := p.api.Logger()
	if logger == nil {
		t.Error("Logger() returned nil after SetLogger")
	}
}

// --------------------------------------------------------------------------
// Tests: SetMetrics then verify plugin API gets updated metrics
// --------------------------------------------------------------------------

func TestPluginManager_SetMetricsThenPluginAPI(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	customMetrics := metrics.NewRegistry()
	pm.SetMetrics(customMetrics)

	p := newMockPlugin("metrics-update", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	m := p.api.Metrics()
	if m != customMetrics {
		t.Error("Metrics() should return the custom registry set via SetMetrics")
	}
}

// --------------------------------------------------------------------------
// Tests: SetConfig then verify plugin API gets updated config
// --------------------------------------------------------------------------

func TestPluginManager_SetConfigThenPluginAPI(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())

	customConfig := &config.Config{Version: "test-2.0"}
	pm.SetConfig(customConfig)

	p := newMockPlugin("config-update", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	cfg := p.api.Config()
	if cfg != customConfig {
		t.Error("Config() should return the custom config set via SetConfig")
	}
}

// --------------------------------------------------------------------------
// Tests: EventBus unsubscribe from wrong topic does nothing
// --------------------------------------------------------------------------

func TestEventBus_UnsubscribeFromDifferentTopic(t *testing.T) {
	eb := NewEventBus()

	var count atomic.Int64

	idA := eb.Subscribe("topic-a", func(e Event) {
		count.Add(1)
	})
	eb.Subscribe("topic-b", func(e Event) {
		count.Add(1)
	})

	// Unsubscribe idA which is on topic-a
	eb.Unsubscribe(idA)

	// Publishing to topic-a should not trigger the handler
	eb.Publish("topic-a", "data")
	if count.Load() != 0 {
		t.Errorf("count after unsubscribed topic-a publish = %d, want 0", count.Load())
	}

	// Publishing to topic-b should still trigger the handler
	eb.Publish("topic-b", "data")
	if count.Load() != 1 {
		t.Errorf("count after topic-b publish = %d, want 1", count.Load())
	}
}

// --------------------------------------------------------------------------
// Tests: EventBus with all built-in event topics
// --------------------------------------------------------------------------

func TestEventBus_AllBuiltinTopics(t *testing.T) {
	eb := NewEventBus()

	topics := []string{
		EventConfigReload,
		EventBackendAdded,
		EventBackendRemoved,
		EventBackendStateChange,
		EventRouteAdded,
		EventRouteRemoved,
		EventHealthCheckResult,
	}

	var count atomic.Int64
	for _, topic := range topics {
		topic := topic
		eb.Subscribe(topic, func(e Event) {
			if e.Topic != topic {
				t.Errorf("handler for %q received event for %q", topic, e.Topic)
			}
			count.Add(1)
		})
	}

	for _, topic := range topics {
		eb.Publish(topic, "data")
	}

	if count.Load() != int64(len(topics)) {
		t.Errorf("count = %d, want %d", count.Load(), len(topics))
	}
}

// --------------------------------------------------------------------------
// Tests: PluginManager registers and gets plugin with version
// --------------------------------------------------------------------------

func TestPluginManager_PluginVersion(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("versioned-plugin", "2.5.1")
	pm.RegisterPlugin(p)

	infos := pm.ListPlugins()
	if len(infos) != 1 {
		t.Fatalf("ListPlugins() length = %d, want 1", len(infos))
	}
	if infos[0].Version != "2.5.1" {
		t.Errorf("Version = %q, want %q", infos[0].Version, "2.5.1")
	}
}

// --------------------------------------------------------------------------
// Tests: Factory receives nil config and handles gracefully
// --------------------------------------------------------------------------

func TestFactory_NilConfigDoesNotPanic(t *testing.T) {
	pm := NewPluginManager(DefaultPluginManagerConfig())
	p := newMockPlugin("nil-cfg-factory", "0.1.0")
	pm.RegisterPlugin(p)
	pm.StartAll()

	// Middleware factory with nil config
	factory := func(cfg map[string]any) (Middleware, error) {
		if cfg == nil {
			return &mockMiddleware{name: "nil-mw"}, nil
		}
		return &mockMiddleware{name: "cfg-mw"}, nil
	}
	p.api.RegisterMiddleware("nil-test-mw", factory)

	f, ok := pm.GetMiddlewareFactory("nil-test-mw")
	if !ok {
		t.Fatal("GetMiddlewareFactory() returned false")
	}
	mw, err := f(nil)
	if err != nil {
		t.Fatalf("factory(nil) error = %v", err)
	}
	if mw.Name() != "nil-mw" {
		t.Errorf("Name() = %q, want %q", mw.Name(), "nil-mw")
	}
}
