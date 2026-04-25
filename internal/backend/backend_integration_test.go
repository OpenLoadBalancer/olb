package backend

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestFullPoolLifecycle tests the complete flow of pool and backend operations
func TestFullPoolLifecycle(t *testing.T) {
	pm := NewPoolManager()

	// Create pool
	pool := NewPool("web-servers", "roundrobin")

	// Set a mock balancer
	pool.SetBalancer(&mockBalancer{name: "mock"})

	// Add backends
	backends := []*Backend{
		NewBackend("web1", "10.0.0.1:8080"),
		NewBackend("web2", "10.0.0.2:8080"),
		NewBackend("web3", "10.0.0.3:8080"),
	}

	for _, b := range backends {
		b.SetState(StateUp)
		if err := pool.AddBackend(b); err != nil {
			t.Fatalf("Failed to add backend %s: %v", b.ID, err)
		}
	}

	// Add pool to manager
	if err := pm.AddPool(pool); err != nil {
		t.Fatalf("Failed to add pool: %v", err)
	}

	// Simulate requests
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		b, err := pool.NextBackend(ctx)
		if err != nil {
			t.Fatalf("NextBackend() error: %v", err)
		}
		if b == nil {
			t.Fatal("NextBackend() returned nil")
		}

		// Simulate connection lifecycle
		if !b.AcquireConn() {
			t.Fatal("AcquireConn() failed")
		}

		// Simulate request processing
		latency := time.Duration(10+i%50) * time.Millisecond
		b.RecordRequest(latency, int64(100+i))

		b.ReleaseConn()
	}

	// Verify stats
	stats := pool.Stats()
	if stats.TotalBackends != 3 {
		t.Errorf("TotalBackends = %v, want %v", stats.TotalBackends, 3)
	}
	if stats.HealthyBackends != 3 {
		t.Errorf("HealthyBackends = %v, want %v", stats.HealthyBackends, 3)
	}

	// Verify each backend has requests distributed
	totalRequests := int64(0)
	for _, bs := range stats.BackendStats {
		totalRequests += bs.TotalRequests
	}
	if totalRequests != 100 {
		t.Errorf("Total requests across backends = %v, want %v", totalRequests, 100)
	}

	// Drain a backend
	if err := pool.DrainBackend("web2"); err != nil {
		t.Errorf("DrainBackend() error: %v", err)
	}

	b2 := pool.GetBackend("web2")
	if b2.State() != StateDraining {
		t.Errorf("Backend state after drain = %v, want %v", b2.State(), StateDraining)
	}

	// Draining backends should not receive new connections
	// but should still be "healthy" (active)
	// All 3 backends are still healthy (2 Up + 1 Draining)
	healthyCount := pool.HealthyCount()
	if healthyCount != 3 {
		t.Errorf("HealthyCount after drain = %v, want %v", healthyCount, 3)
	}

	// Remove the drained backend
	if err := pool.RemoveBackend("web2"); err != nil {
		t.Errorf("RemoveBackend() error: %v", err)
	}

	if pool.BackendCount() != 2 {
		t.Errorf("BackendCount after remove = %v, want %v", pool.BackendCount(), 2)
	}

	// Remove pool
	if err := pm.RemovePool("web-servers"); err != nil {
		t.Errorf("RemovePool() error: %v", err)
	}

	if pm.PoolCount() != 0 {
		t.Errorf("PoolCount after remove = %v, want %v", pm.PoolCount(), 0)
	}
}

// TestConcurrentRequestsToMultipleBackends tests concurrent request handling
func TestConcurrentRequestsToMultipleBackends(t *testing.T) {
	pm := NewPoolManager()
	pool := NewPool("api-servers", "leastconn")

	// Set a mock balancer
	pool.SetBalancer(&mockBalancer{name: "mock"})

	// Add backends
	for i := 1; i <= 5; i++ {
		address := "10.0.0." + string(rune('0'+i)) + ":8080"
		b := NewBackend(string(rune('0'+i)), address)
		b.SetState(StateUp)
		b.SetMaxConns(100)
		pool.AddBackend(b)
	}

	pm.AddPool(pool)

	var wg sync.WaitGroup
	numWorkers := 50
	requestsPerWorker := 100

	ctx := context.Background()
	// Simulate concurrent requests
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < requestsPerWorker; j++ {
				b, err := pool.NextBackend(ctx)
				if err != nil {
					t.Errorf("NextBackend() error: %v", err)
					continue
				}
				if b == nil {
					t.Error("NextBackend() returned nil")
					continue
				}

				if !b.AcquireConn() {
					t.Errorf("Worker %d: AcquireConn() failed for backend %s", workerID, b.ID)
					continue
				}

				// Simulate some work
				time.Sleep(time.Microsecond * 100)

				latency := time.Duration(1+workerID%10) * time.Millisecond
				b.RecordRequest(latency, 1024)

				b.ReleaseConn()
			}
		}(i)
	}

	wg.Wait()

	// Verify all requests were recorded
	totalRequests := int64(0)
	stats := pool.Stats()
	for _, bs := range stats.BackendStats {
		totalRequests += bs.TotalRequests
	}

	expectedRequests := int64(numWorkers * requestsPerWorker)
	if totalRequests != expectedRequests {
		t.Errorf("Total requests = %v, want %v", totalRequests, expectedRequests)
	}

	// Verify all active connections were released
	for id, bs := range stats.BackendStats {
		if bs.ActiveConns != 0 {
			t.Errorf("Backend %s has %d active connections, want 0", id, bs.ActiveConns)
		}
	}
}

// TestPoolRemovalCascade tests that pool removal cascades properly
func TestPoolRemovalCascade(t *testing.T) {
	pm := NewPoolManager()

	// Create pool with backends
	pool := NewPool("test-pool", "roundrobin")
	for i := 0; i < 5; i++ {
		b := NewBackend(string(rune('a'+i)), "127.0.0.1:8080")
		b.SetState(StateUp)
		pool.AddBackend(b)
	}

	pm.AddPool(pool)

	// Verify initial state
	if pm.BackendCount() != 5 {
		t.Fatalf("Initial BackendCount = %v, want %v", pm.BackendCount(), 5)
	}

	// Get reference to backend before removal
	backendRef := pm.GetBackend("test-pool", "a")
	if backendRef == nil {
		t.Fatal("GetBackend(a) returned nil")
	}

	// Set some state on the backend
	backendRef.AcquireConn()
	backendRef.RecordRequest(10*time.Millisecond, 1024)

	// Remove the pool
	if err := pm.RemovePool("test-pool"); err != nil {
		t.Fatalf("RemovePool() error: %v", err)
	}

	// Verify pool is gone
	if pm.GetPool("test-pool") != nil {
		t.Error("Pool should be removed from manager")
	}

	// Backend objects still exist (they're not garbage collected)
	// but they're no longer accessible through the manager
	if pm.GetBackend("test-pool", "a") != nil {
		t.Error("Backend should not be accessible after pool removal")
	}

	// The backend reference we held should still have its state
	if backendRef.ActiveConns() != 1 {
		t.Errorf("Backend still has %d active connections, want 1", backendRef.ActiveConns())
	}
	if backendRef.TotalRequests() != 1 {
		t.Errorf("Backend still has %d total requests, want 1", backendRef.TotalRequests())
	}
}

// TestMultiplePoolsIsolation tests that multiple pools are properly isolated
func TestMultiplePoolsIsolation(t *testing.T) {
	pm := NewPoolManager()

	// Create web pool
	webPool := NewPool("web", "roundrobin")
	for i := 1; i <= 3; i++ {
		address := "10.0.1." + string(rune('0'+i)) + ":8080"
		b := NewBackend("web"+string(rune('0'+i)), address)
		b.SetState(StateUp)
		webPool.AddBackend(b)
	}
	pm.AddPool(webPool)

	// Create api pool
	apiPool := NewPool("api", "leastconn")
	for i := 1; i <= 2; i++ {
		address := "10.0.2." + string(rune('0'+i)) + ":8080"
		b := NewBackend("api"+string(rune('0'+i)), address)
		b.SetState(StateUp)
		apiPool.AddBackend(b)
	}
	pm.AddPool(apiPool)

	// Verify isolation
	if pm.PoolCount() != 2 {
		t.Errorf("PoolCount = %v, want %v", pm.PoolCount(), 2)
	}

	// Web pool should have 3 backends
	if webPool.BackendCount() != 3 {
		t.Errorf("Web pool BackendCount = %v, want %v", webPool.BackendCount(), 3)
	}

	// API pool should have 2 backends
	if apiPool.BackendCount() != 2 {
		t.Errorf("API pool BackendCount = %v, want %v", apiPool.BackendCount(), 2)
	}

	// Total backends should be 5
	if pm.BackendCount() != 5 {
		t.Errorf("Total BackendCount = %v, want %v", pm.BackendCount(), 5)
	}

	// Backend lookups should be isolated
	if pm.GetBackend("web", "api1") != nil {
		t.Error("Should not find api1 in web pool")
	}
	if pm.GetBackend("api", "web1") != nil {
		t.Error("Should not find web1 in api pool")
	}
	if pm.GetBackend("web", "web1") == nil {
		t.Error("Should find web1 in web pool")
	}

	// Cross-pool lookup should work
	b, poolName := pm.GetBackendAcrossPools("api2")
	if b == nil {
		t.Error("GetBackendAcrossPools(api2) returned nil")
	}
	if poolName != "api" {
		t.Errorf("GetBackendAcrossPools(api2) pool = %v, want %v", poolName, "api")
	}

	// Stats should be separate
	allStats := pm.BackendStats()
	if len(allStats) != 2 {
		t.Errorf("BackendStats length = %v, want %v", len(allStats), 2)
	}
	if _, ok := allStats["web"]; !ok {
		t.Error("BackendStats missing web pool")
	}
	if _, ok := allStats["api"]; !ok {
		t.Error("BackendStats missing api pool")
	}

	// Remove one pool, other should remain
	if err := pm.RemovePool("web"); err != nil {
		t.Fatalf("RemovePool(web) error: %v", err)
	}

	if pm.PoolCount() != 1 {
		t.Errorf("PoolCount after remove = %v, want %v", pm.PoolCount(), 1)
	}
	if pm.GetPool("api") == nil {
		t.Error("API pool should still exist")
	}
}

// TestBackendFailover tests backend state transitions during failover scenarios
func TestBackendFailover(t *testing.T) {
	pool := NewPool("test-pool", "roundrobin")

	// Set a mock balancer
	pool.SetBalancer(&mockBalancer{name: "mock"})

	// Add backends
	b1 := NewBackend("primary", "10.0.0.1:8080")
	b1.SetState(StateUp)
	b2 := NewBackend("secondary", "10.0.0.2:8080")
	b2.SetState(StateUp)

	pool.AddBackend(b1)
	pool.AddBackend(b2)

	// Initially both are healthy
	healthy := pool.GetHealthyBackends()
	if len(healthy) != 2 {
		t.Errorf("Initial healthy count = %v, want %v", len(healthy), 2)
	}

	// Simulate primary failure
	b1.TransitionState(StateDown)

	// Now only secondary should be healthy
	healthy = pool.GetHealthyBackends()
	if len(healthy) != 1 {
		t.Errorf("Healthy count after failure = %v, want %v", len(healthy), 1)
	}
	if healthy[0].ID != "secondary" {
		t.Errorf("Healthy backend = %v, want secondary", healthy[0].ID)
	}

	// NextBackend should always return secondary
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		b, err := pool.NextBackend(ctx)
		if err != nil {
			t.Fatalf("NextBackend() error: %v", err)
		}
		if b == nil {
			t.Fatal("NextBackend() returned nil")
		}
		if b.ID != "secondary" {
			t.Errorf("NextBackend() = %v, want secondary", b.ID)
		}
	}

	// Simulate primary recovery
	b1.TransitionState(StateUp)

	// Both should be healthy again
	healthy = pool.GetHealthyBackends()
	if len(healthy) != 2 {
		t.Errorf("Healthy count after recovery = %v, want %v", len(healthy), 2)
	}
}

// TestHealthCheckIntegration tests health check recording
func TestHealthCheckIntegration(t *testing.T) {
	b := NewBackend("test", "127.0.0.1:8080")

	// Initial state
	if !b.LastCheck().IsZero() {
		t.Error("LastCheck should be zero initially")
	}
	if b.CheckFailCount() != 0 {
		t.Errorf("CheckFailCount initial = %v, want 0", b.CheckFailCount())
	}

	// Record successful check
	b.RecordHealthCheck(true)
	if b.LastCheck().IsZero() {
		t.Error("LastCheck should be set after health check")
	}
	if b.CheckFailCount() != 0 {
		t.Errorf("CheckFailCount after success = %v, want 0", b.CheckFailCount())
	}

	// Record failed checks
	b.RecordHealthCheck(false)
	b.RecordHealthCheck(false)
	b.RecordHealthCheck(false)

	if b.CheckFailCount() != 3 {
		t.Errorf("CheckFailCount after 3 failures = %v, want 3", b.CheckFailCount())
	}

	// Success resets counter
	b.RecordHealthCheck(true)
	if b.CheckFailCount() != 0 {
		t.Errorf("CheckFailCount after reset = %v, want 0", b.CheckFailCount())
	}
}

// TestMaxConnectionsLimit tests connection limit enforcement
func TestMaxConnectionsLimit(t *testing.T) {
	b := NewBackend("test", "127.0.0.1:8080")
	b.SetMaxConns(5)
	b.SetState(StateUp)

	// Acquire max connections
	for i := 0; i < 5; i++ {
		if !b.AcquireConn() {
			t.Errorf("AcquireConn %d should succeed", i+1)
		}
	}

	// Next acquire should fail
	if b.AcquireConn() {
		t.Error("AcquireConn 6 should fail (max reached)")
	}

	if b.ActiveConns() != 5 {
		t.Errorf("ActiveConns = %v, want 5", b.ActiveConns())
	}

	// Release one
	b.ReleaseConn()
	if b.ActiveConns() != 4 {
		t.Errorf("ActiveConns after release = %v, want 4", b.ActiveConns())
	}

	// Now acquire should succeed
	if !b.AcquireConn() {
		t.Error("AcquireConn after release should succeed")
	}
}

// TestPoolManagerSnapshotConsistency tests snapshot consistency
func TestPoolManagerSnapshotConsistency(t *testing.T) {
	pm := NewPoolManager()

	// Create pools with backends
	for i := 0; i < 3; i++ {
		p := NewPool(string(rune('a'+i)), "roundrobin")
		for j := 0; j < 3; j++ {
			b := NewBackend(string(rune('a'+i))+"-"+string(rune('0'+j)), "127.0.0.1:8080")
			b.SetState(StateUp)
			b.AcquireConn()
			p.AddBackend(b)
		}
		pm.AddPool(p)
	}

	// Take snapshot
	snapshot := pm.Snapshot()

	// Verify snapshot contents
	if len(snapshot) != 3 {
		t.Errorf("Snapshot length = %v, want 3", len(snapshot))
	}

	// Modify original pools
	for _, p := range pm.GetAllPools() {
		p.Name = "modified"
	}

	// Snapshot should be unchanged
	for name := range snapshot {
		if name == "modified" {
			t.Error("Snapshot was affected by original modification")
		}
	}

	// But manager should see modifications
	for _, p := range pm.GetAllPools() {
		if p.Name != "modified" {
			t.Error("Manager pool was not modified")
		}
	}
}

// mockBalancer is a simple mock balancer for integration tests
type mockBalancer struct {
	name string
}

func (m *mockBalancer) Name() string {
	return m.name
}

func (m *mockBalancer) Next(ctx *RequestContext, backends []*Backend) *Backend {
	if len(backends) > 0 {
		return backends[0]
	}
	return nil
}

func (m *mockBalancer) Add(backend *Backend) {}

func (m *mockBalancer) Remove(id string) {}

func (m *mockBalancer) Update(backend *Backend) {}
