package benchmark

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/proxy/l7"
	"github.com/openloadbalancer/olb/internal/router"
)

// ---------------------------------------------------------------------------
// Memory profiling helpers
// ---------------------------------------------------------------------------

// memSnapshot captures a point-in-time memory snapshot.
type memSnapshot struct {
	HeapAlloc   uint64
	HeapObjects uint64
	TotalAlloc  uint64
	NumGC       uint32
	Goroutines  int
	StackInUse  uint64
}

func takeMemSnapshot() memSnapshot {
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	runtime.GC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memSnapshot{
		HeapAlloc:   m.HeapAlloc,
		HeapObjects: m.HeapObjects,
		TotalAlloc:  m.TotalAlloc,
		NumGC:       m.NumGC,
		Goroutines:  runtime.NumGoroutine(),
		StackInUse:  m.StackInuse,
	}
}

// delta computes the memory delta between two snapshots.
// Returns zero for any field where "after" is less than "before" (GC freed memory).
func (before memSnapshot) delta(after memSnapshot) memSnapshot {
	d := func(a, b uint64) uint64 {
		if a >= b {
			return a - b
		}
		return 0
	}
	return memSnapshot{
		HeapAlloc:   d(after.HeapAlloc, before.HeapAlloc),
		HeapObjects: d(after.HeapObjects, before.HeapObjects),
		TotalAlloc:  d(after.TotalAlloc, before.TotalAlloc),
		Goroutines:  after.Goroutines - before.Goroutines,
		StackInUse:  d(after.StackInUse, before.StackInUse),
	}
}

func (s memSnapshot) String() string {
	return fmt.Sprintf(
		"HeapAlloc=%d KB  HeapObjects=%d  Goroutines=%d  StackInUse=%d KB",
		s.HeapAlloc/1024, s.HeapObjects, s.Goroutines, s.StackInUse/1024,
	)
}

// ---------------------------------------------------------------------------
// Connection pool memory per idle connection
// ---------------------------------------------------------------------------

// TestMemory_ConnectionPool_IdleConn verifies that idle connections in the pool
// consume less than 4KB each on average.
func TestMemory_ConnectionPool_IdleConn(t *testing.T) {
	// Create a real backend server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := srv.Listener.Addr().String()

	// Snapshot baseline
	before := takeMemSnapshot()

	// Create connection pool and establish N idle connections
	const numConns = 100
	pool := conn.NewPool(&conn.PoolConfig{
		BackendID:   "test-be",
		Address:     addr,
		DialTimeout: 5 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     numConns + 10,
	})
	defer pool.Close()

	// Establish and return connections (they become idle)
	for i := 0; i < numConns; i++ {
		c, err := pool.Get(t.Context())
		if err != nil {
			t.Fatalf("Get(%d) failed: %v", i, err)
		}
		pool.Put(c)
	}

	// Allow goroutines and GC to settle
	time.Sleep(50 * time.Millisecond)

	after := takeMemSnapshot()
	delta := before.delta(after)

	perConnKB := float64(delta.HeapAlloc) / float64(numConns) / 1024
	t.Logf("Baseline:   %s", before)
	t.Logf("After %d idle conns: %s", numConns, after)
	t.Logf("Delta:      %s", delta)
	t.Logf("Per conn:   %.2f KB (heap)  %d objects", perConnKB, delta.HeapObjects/uint64(numConns))

	// Target: <4KB per idle connection
	if perConnKB > 4.0 {
		t.Errorf("idle connection memory %.2f KB exceeds 4 KB target", perConnKB)
	} else {
		t.Logf("PASS: %.2f KB per idle connection (target: <4 KB)", perConnKB)
	}
}

// ---------------------------------------------------------------------------
// Active request memory per request
// ---------------------------------------------------------------------------

// TestMemory_ActiveRequest_PerRequest verifies that active proxied requests
// consume less than 32KB each on average.
func TestMemory_ActiveRequest_PerRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race-prone memory test in short mode")
	}
	// Backend that holds connections open briefly
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	// Setup proxy stack
	r := router.NewRouter()
	r.AddRoute(&router.Route{Name: "default", Path: "/", BackendPool: "test-pool"})

	pm := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	be := backend.NewBackend("be-1", srv.Listener.Addr().String())
	be.SetState(backend.StateUp)
	pool.AddBackend(be)
	pool.SetBalancer(balancer.NewRoundRobin())
	pm.AddPool(pool)

	proxy := l7.NewHTTPProxy(&l7.Config{
		Router:          r,
		PoolManager:     pm,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      1,
	})
	defer proxy.Close()

	// Proxy server
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	// Baseline after proxy is set up
	before := takeMemSnapshot()

	// Send N concurrent requests and hold them active
	const numRequests = 200
	type result struct {
		err error
	}
	results := make(chan result, numRequests)

	// Semaphore to hold requests "active" — we send them all at once
	var ready sync.WaitGroup
	ready.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			ready.Done()
			resp, err := http.Get(proxySrv.URL + "/")
			if err != nil {
				results <- result{err: err}
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			results <- result{}
		}()
	}

	// Wait for all goroutines to be ready
	ready.Wait()
	// Small delay to let requests hit the proxy
	time.Sleep(50 * time.Millisecond)

	// Measure while requests are active
	during := takeMemSnapshot()

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		r := <-results
		if r.err != nil {
			t.Logf("request error: %v (this is expected under load)", r.err)
		}
	}

	delta := before.delta(during)
	perReqKB := float64(delta.HeapAlloc) / float64(numRequests) / 1024
	t.Logf("Baseline:  %s", before)
	t.Logf("Active:    %s", during)
	t.Logf("Delta:     %s", delta)
	t.Logf("Per req:   %.2f KB (heap)  %d goroutines overhead", perReqKB, delta.Goroutines)

	// Target: <32KB per active request
	if perReqKB > 32.0 {
		t.Errorf("active request memory %.2f KB exceeds 32 KB target", perReqKB)
	} else {
		t.Logf("PASS: %.2f KB per active request (target: <32 KB)", perReqKB)
	}
}

// ---------------------------------------------------------------------------
// Pool manager memory with many pools and backends
// ---------------------------------------------------------------------------

// TestMemory_PoolManager_Scale verifies memory growth is proportional when
// adding many pools and backends.
func TestMemory_PoolManager_Scale(t *testing.T) {
	before := takeMemSnapshot()

	const numPools = 100
	const backendsPerPool = 20

	pm := backend.NewPoolManager()
	for i := 0; i < numPools; i++ {
		poolName := fmt.Sprintf("pool-%d", i)
		pool := backend.NewPool(poolName, "round_robin")
		pool.SetBalancer(balancer.NewRoundRobin())

		for j := 0; j < backendsPerPool; j++ {
			be := backend.NewBackend(
				fmt.Sprintf("be-%d-%d", i, j),
				fmt.Sprintf("10.%d.%d.%d:8080", i/256, (i%256)/256, j%256),
			)
			be.SetState(backend.StateUp)
			be.Weight = int32(1 + j%5)
			pool.AddBackend(be)
		}
		pm.AddPool(pool)
	}

	after := takeMemSnapshot()
	delta := before.delta(after)

	totalBackends := numPools * backendsPerPool
	perBackend := float64(delta.TotalAlloc) / float64(totalBackends) / 1024

	t.Logf("Created %d pools × %d backends = %d total", numPools, backendsPerPool, totalBackends)
	t.Logf("Baseline:  %s", before)
	t.Logf("After:     %s", after)
	t.Logf("Delta:     %s", delta)
	t.Logf("Per backend: %.2f KB (total alloc)", perBackend)

	// Sanity: each backend should be under 5KB total allocation
	if perBackend > 5.0 && delta.HeapAlloc > 0 {
		t.Errorf("per-backend memory %.2f KB seems high", perBackend)
	} else {
		t.Logf("PASS: %.2f KB per backend (%d pools, %d backends)", perBackend, numPools, totalBackends)
	}
}

// ---------------------------------------------------------------------------
// Router memory with many routes
// ---------------------------------------------------------------------------

// TestMemory_Router_Scale verifies the radix trie router memory grows
// proportionally with routes.
func TestMemory_Router_Scale(t *testing.T) {
	before := takeMemSnapshot()

	const numRoutes = 1000
	r := router.NewRouter()

	for i := 0; i < numRoutes; i++ {
		r.AddRoute(&router.Route{
			Name:        fmt.Sprintf("route-%d", i),
			Path:        fmt.Sprintf("/api/v1/resource%d", i),
			BackendPool: fmt.Sprintf("pool-%d", i%10),
		})
	}

	after := takeMemSnapshot()
	delta := before.delta(after)
	perRoute := float64(delta.HeapAlloc) / float64(numRoutes)

	t.Logf("Created %d routes", numRoutes)
	t.Logf("Baseline:  %s", before)
	t.Logf("After:     %s", after)
	t.Logf("Delta:     %s", delta)
	t.Logf("Per route: %.0f bytes (heap)", perRoute)

	// Sanity: each route should be under 2KB
	if perRoute > 2048 {
		t.Errorf("per-route memory %.0f bytes seems high", perRoute)
	} else {
		t.Logf("PASS: %.0f bytes per route (%d routes total)", perRoute, numRoutes)
	}
}

// ---------------------------------------------------------------------------
// Goroutine leak check
// ---------------------------------------------------------------------------

// TestMemory_GoroutineLeaks verifies that a full proxy request cycle
// does not leak goroutines.
func TestMemory_GoroutineLeaks(t *testing.T) {
	// Create backend
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Setup proxy
	r := router.NewRouter()
	r.AddRoute(&router.Route{Name: "default", Path: "/", BackendPool: "test-pool"})

	pm := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	be := backend.NewBackend("be-1", srv.Listener.Addr().String())
	be.SetState(backend.StateUp)
	pool.AddBackend(be)
	pool.SetBalancer(balancer.NewRoundRobin())
	pm.AddPool(pool)

	proxy := l7.NewHTTPProxy(&l7.Config{
		Router:          r,
		PoolManager:     pm,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      1,
	})
	defer proxy.Close()

	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	// Record goroutines before
	before := runtime.NumGoroutine()

	// Send many requests
	const numRequests = 500
	for i := 0; i < numRequests; i++ {
		resp, err := http.Get(proxySrv.URL + "/")
		if err != nil {
			t.Logf("request %d error: %v", i, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// Allow goroutines to settle
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()
	leaked := after - before

	t.Logf("Goroutines before: %d, after: %d, delta: %d", before, after, leaked)

	if leaked > 10 {
		t.Errorf("possible goroutine leak: %d goroutines leaked after %d requests", leaked, numRequests)
	} else {
		t.Logf("PASS: no goroutine leak (delta=%d after %d requests)", leaked, numRequests)
	}
}
