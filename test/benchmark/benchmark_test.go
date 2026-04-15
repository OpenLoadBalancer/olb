// Package benchmark provides comprehensive performance benchmarks for OpenLoadBalancer.
//
// Run with: go test -bench=. -benchmem -count=3 ./test/benchmark/
package benchmark

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/proxy/l7"
	"github.com/openloadbalancer/olb/internal/router"
	"github.com/openloadbalancer/olb/pkg/utils"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newBackendServer creates an httptest.Server that responds with a payload of
// the given size. The caller must call Close() when done.
func newBackendServer(payloadSize int) *httptest.Server {
	body := bytes.Repeat([]byte("x"), payloadSize)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume request body to simulate a real backend
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
}

// hostPort extracts host:port from the server URL (strips http://).
func hostPort(s *httptest.Server) string {
	return s.Listener.Addr().String()
}

// setupProxyStack wires router -> pool -> balancer -> proxy with an optional
// middleware chain. The returned cleanup function must be called.
func setupProxyStack(b *testing.B, backendAddr string, chain *middleware.Chain) (*l7.HTTPProxy, func()) {
	// Router
	r := router.NewRouter()
	r.AddRoute(&router.Route{
		Name:        "default",
		Path:        "/",
		BackendPool: "test-pool",
	})

	// Backend pool
	pm := backend.NewPoolManager()
	pool := backend.NewPool("test-pool", "round_robin")
	be := backend.NewBackend("be-1", backendAddr)
	be.SetState(backend.StateUp)
	pool.AddBackend(be)
	bal := balancer.NewRoundRobin()
	pool.SetBalancer(bal)
	pm.AddPool(pool)

	if chain == nil {
		chain = middleware.NewChain()
	}

	proxy := l7.NewHTTPProxy(&l7.Config{
		Router:          r,
		PoolManager:     pm,
		MiddlewareChain: chain,
		MaxRetries:      1,
	})

	return proxy, func() { proxy.Close() }
}

// createBackends creates n backends with sequential addresses.
func createBackends(n int) []*backend.Backend {
	backends := make([]*backend.Backend, n)
	for i := 0; i < n; i++ {
		be := backend.NewBackend(
			fmt.Sprintf("be-%d", i),
			fmt.Sprintf("10.0.0.%d:%d", i%256, 8080+i),
		)
		be.SetState(backend.StateUp)
		be.Weight = int32(1 + i%5)
		backends[i] = be
	}
	return backends
}

// ---------------------------------------------------------------------------
// HTTP Proxy Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkHTTPProxy_SmallPayload(b *testing.B) {
	b.ReportAllocs()

	srv := newBackendServer(1024) // 1 KB
	defer srv.Close()

	proxy, cleanup := setupProxyStack(b, hostPort(srv), nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", w.Code)
		}
	}
	b.SetBytes(1024)
}

func BenchmarkHTTPProxy_LargePayload(b *testing.B) {
	b.ReportAllocs()

	const payloadSize = 1 << 20 // 1 MB
	srv := newBackendServer(payloadSize)
	defer srv.Close()

	proxy, cleanup := setupProxyStack(b, hostPort(srv), nil)
	defer cleanup()

	payload := bytes.Repeat([]byte("y"), payloadSize)
	body := bytes.NewReader(payload)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/", body)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body.Reset(payload) // rewind
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", w.Code)
		}
	}
	b.SetBytes(payloadSize)
}

func BenchmarkHTTPProxy_ConcurrentRequests(b *testing.B) {
	b.ReportAllocs()

	srv := newBackendServer(1024)
	defer srv.Close()

	proxy, cleanup := setupProxyStack(b, hostPort(srv), nil)
	defer cleanup()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				b.Fatalf("unexpected status %d", w.Code)
			}
		}
	})
	b.SetBytes(1024)
}

func BenchmarkHTTPProxy_WithMiddleware(b *testing.B) {
	b.ReportAllocs()

	srv := newBackendServer(1024)
	defer srv.Close()

	chain := middleware.NewChain()
	chain.Use(NewNoopMiddleware("security", middleware.PrioritySecurity))
	chain.Use(middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{
		Generate: true,
	}))
	chain.Use(middleware.NewHeadersMiddleware(middleware.HeadersConfig{
		ResponseSet: map[string]string{
			"X-Powered-By": "OLB",
			"Server":       "OpenLoadBalancer",
		},
		SecurityPreset: middleware.SecurityPresetBasic,
	}))
	corsMW, err := middleware.NewCORSMiddleware(middleware.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	})
	if err != nil {
		b.Fatal(err)
	}
	chain.Use(corsMW)

	proxy, cleanup := setupProxyStack(b, hostPort(srv), chain)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", w.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Router Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRouter_StaticRoutes(b *testing.B) {
	b.ReportAllocs()

	r := router.NewRouter()
	for i := 0; i < 100; i++ {
		r.AddRoute(&router.Route{
			Name:        fmt.Sprintf("route-%d", i),
			Path:        fmt.Sprintf("/api/v1/service%d/resource", i),
			BackendPool: "pool-1",
		})
	}

	// Warm the router
	req := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/service50/resource", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := r.Match(req)
		if !ok {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkRouter_ParameterRoutes(b *testing.B) {
	b.ReportAllocs()

	r := router.NewRouter()
	for i := 0; i < 50; i++ {
		r.AddRoute(&router.Route{
			Name:        fmt.Sprintf("param-route-%d", i),
			Path:        fmt.Sprintf("/api/v%d/users/:id/posts/:postId", i),
			BackendPool: "pool-1",
		})
	}

	req := httptest.NewRequest(http.MethodGet, "http://localhost/api/v25/users/12345/posts/67890", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := r.Match(req)
		if !ok {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkRouter_WildcardRoutes(b *testing.B) {
	b.ReportAllocs()

	r := router.NewRouter()
	for i := 0; i < 20; i++ {
		r.AddRoute(&router.Route{
			Name:        fmt.Sprintf("wildcard-route-%d", i),
			Path:        fmt.Sprintf("/static/v%d/*filepath", i),
			BackendPool: "pool-1",
		})
	}

	req := httptest.NewRequest(http.MethodGet, "http://localhost/static/v10/css/app.min.css", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := r.Match(req)
		if !ok {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkRouter_HostMatching(b *testing.B) {
	b.ReportAllocs()

	r := router.NewRouter()
	// Add routes for several exact hosts
	hosts := []string{"api.example.com", "www.example.com", "admin.example.com",
		"app.example.com", "cdn.example.com"}
	for i, host := range hosts {
		r.AddRoute(&router.Route{
			Name:        fmt.Sprintf("host-route-%d", i),
			Host:        host,
			Path:        "/",
			BackendPool: "pool-1",
		})
	}
	// Add wildcard host
	r.AddRoute(&router.Route{
		Name:        "wildcard-host",
		Host:        "*.example.com",
		Path:        "/",
		BackendPool: "pool-2",
	})

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil)
	req.Host = "api.example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := r.Match(req)
		if !ok {
			b.Fatal("expected match")
		}
	}
}

// ---------------------------------------------------------------------------
// Balancer Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRoundRobin_Next(b *testing.B) {
	b.ReportAllocs()

	rr := balancer.NewRoundRobin()
	backends := createBackends(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := rr.Next(nil, backends)
		if result == nil {
			b.Fatal("expected non-nil backend")
		}
	}
}

func BenchmarkWeightedRoundRobin_Next(b *testing.B) {
	b.ReportAllocs()

	wrr := balancer.NewWeightedRoundRobin()
	backends := createBackends(10)
	for _, be := range backends {
		wrr.Add(be)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := wrr.Next(nil, backends)
		if result == nil {
			b.Fatal("expected non-nil backend")
		}
	}
}

func BenchmarkConsistentHash_Next(b *testing.B) {
	b.ReportAllocs()

	ch := balancer.NewConsistentHash(150)
	backends := createBackends(10)
	for _, be := range backends {
		ch.Add(be)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := ch.Next(nil, backends)
		if result == nil {
			b.Fatal("expected non-nil backend")
		}
	}
}

func BenchmarkMaglev_Next(b *testing.B) {
	b.ReportAllocs()

	m := balancer.NewMaglev()
	backends := createBackends(10)
	for _, be := range backends {
		m.Add(be)
	}
	// Force initial build of lookup table
	m.Next(nil, backends)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := m.Next(nil, backends)
		if result == nil {
			b.Fatal("expected non-nil backend")
		}
	}
}

func BenchmarkLeastConnections_Next(b *testing.B) {
	b.ReportAllocs()

	lc := balancer.NewLeastConnections()
	backends := createBackends(10)
	for _, be := range backends {
		lc.Add(be)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := lc.Next(nil, backends)
		if result == nil {
			b.Fatal("expected non-nil backend")
		}
	}
}

// Parallel balancer benchmarks to test contention.

func BenchmarkRoundRobin_Next_Parallel(b *testing.B) {
	b.ReportAllocs()

	rr := balancer.NewRoundRobin()
	backends := createBackends(10)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rr.Next(nil, backends)
		}
	})
}

func BenchmarkWeightedRoundRobin_Next_Parallel(b *testing.B) {
	b.ReportAllocs()

	wrr := balancer.NewWeightedRoundRobin()
	backends := createBackends(10)
	for _, be := range backends {
		wrr.Add(be)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wrr.Next(nil, backends)
		}
	})
}

// ---------------------------------------------------------------------------
// Middleware Chain Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkMiddlewareChain_Empty(b *testing.B) {
	b.ReportAllocs()

	chain := middleware.NewChain()
	handler := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkMiddlewareChain_Full(b *testing.B) {
	b.ReportAllocs()

	chain := middleware.NewChain()
	chain.Use(NewNoopMiddleware("security", middleware.PrioritySecurity))
	chain.Use(middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{
		Generate: true,
	}))
	chain.Use(middleware.NewHeadersMiddleware(middleware.HeadersConfig{
		RequestSet: map[string]string{
			"X-Custom-Header": "value",
		},
		ResponseSet: map[string]string{
			"X-Powered-By": "OLB",
			"Server":       "OpenLoadBalancer",
		},
		SecurityPreset: middleware.SecurityPresetStrict,
	}))
	corsMW2, err := middleware.NewCORSMiddleware(middleware.CORSConfig{
		AllowedOrigins:   []string{"https://example.com", "https://app.example.com"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Request-Id"},
		AllowCredentials: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	chain.Use(corsMW2)
	chain.Use(NewNoopMiddleware("metrics", middleware.PriorityMetrics))
	chain.Use(NewNoopMiddleware("access-log", middleware.PriorityAccessLog))

	handler := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.Header.Set("Origin", "https://example.com")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

// ---------------------------------------------------------------------------
// Buffer Pool Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkMiddlewareChain_ThenOverhead measures the per-request overhead of Then().
// This is the critical hot path - proxy.go calls Then() on every request.
func BenchmarkMiddlewareChain_ThenOverhead(b *testing.B) {
	b.ReportAllocs()

	chain := middleware.NewChain()
	chain.Use(NewNoopMiddleware("security", middleware.PrioritySecurity))
	chain.Use(middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{
		Generate: true,
	}))
	chain.Use(NewNoopMiddleware("metrics", middleware.PriorityMetrics))
	chain.Use(NewNoopMiddleware("access-log", middleware.PriorityAccessLog))

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler := chain.Then(final)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
}

func BenchmarkMiddlewareChain_CachedVsThen(b *testing.B) {
	b.ReportAllocs()

	chain := middleware.NewChain()
	chain.Use(NewNoopMiddleware("security", middleware.PrioritySecurity))
	chain.Use(middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{Generate: true}))
	chain.Use(NewNoopMiddleware("metrics", middleware.PriorityMetrics))

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	b.Run("per-request Then()", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			handler := chain.Then(final)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}
	})

	b.Run("cached handler", func(b *testing.B) {
		b.ReportAllocs()
		cached := chain.Then(final)
		for i := 0; i < b.N; i++ {
			cached.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}
	})
}

func BenchmarkBufferPool_GetPut_Small(b *testing.B) {
	b.ReportAllocs()

	pool := utils.NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(utils.SmallBufferSize)
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_GetPut_Medium(b *testing.B) {
	b.ReportAllocs()

	pool := utils.NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(utils.MediumBufferSize)
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_GetPut_Large(b *testing.B) {
	b.ReportAllocs()

	pool := utils.NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(utils.LargeBufferSize)
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_GetPut_XLarge(b *testing.B) {
	b.ReportAllocs()

	pool := utils.NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(utils.XLargeBufferSize)
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_GetPut_Parallel(b *testing.B) {
	b.ReportAllocs()

	pool := utils.NewBufferPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get(utils.MediumBufferSize)
			pool.Put(buf)
		}
	})
}

func BenchmarkBufferPool_Vs_MakeSlice(b *testing.B) {
	b.Run("Pool", func(b *testing.B) {
		b.ReportAllocs()
		pool := utils.NewBufferPool()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get(utils.MediumBufferSize)
			pool.Put(buf)
		}
	})

	b.Run("MakeSlice", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := make([]byte, utils.MediumBufferSize)
			_ = buf
		}
	})
}

// ---------------------------------------------------------------------------
// LRU Cache Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkLRUCache_Get_Hit(b *testing.B) {
	b.ReportAllocs()

	cache := utils.MustNewLRU[string, string](1000)
	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Put(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%1000)
		_, ok := cache.Get(key)
		if !ok {
			b.Fatal("expected hit")
		}
	}
}

func BenchmarkLRUCache_Get_Miss(b *testing.B) {
	b.ReportAllocs()

	cache := utils.MustNewLRU[string, string](1000)
	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Put(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("miss-%d", i)
		_, _ = cache.Get(key)
	}
}

func BenchmarkLRUCache_Put(b *testing.B) {
	b.ReportAllocs()

	cache := utils.MustNewLRU[string, string](1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Keys cycle so we exercise eviction after first 1000
		key := fmt.Sprintf("key-%d", i%2000)
		cache.Put(key, "value")
	}
}

func BenchmarkLRUCache_GetSet_Parallel(b *testing.B) {
	b.ReportAllocs()

	cache := utils.MustNewLRU[string, string](1000)
	for i := 0; i < 1000; i++ {
		cache.Put(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%1000)
			if i%4 == 0 {
				cache.Put(key, "updated")
			} else {
				cache.Get(key)
			}
			i++
		}
	})
}

// ---------------------------------------------------------------------------
// Connection Manager Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkConnManager_Accept(b *testing.B) {
	b.ReportAllocs()

	mgr := conn.NewManager(&conn.Config{
		MaxConnections: 100000,
		MaxPerSource:   10000,
		MaxPerBackend:  10000,
	})

	// Create a pipe-based listener for fast local connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Accept connections in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			b.Fatal(err)
		}
		tracked, err := mgr.Accept(c)
		if err != nil {
			b.Fatal(err)
		}
		tracked.Close()
	}
	b.StopTimer()
	listener.Close()
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Router Scaling Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRouter_10Routes(b *testing.B)   { benchmarkRouterScale(b, 10) }
func BenchmarkRouter_100Routes(b *testing.B)  { benchmarkRouterScale(b, 100) }
func BenchmarkRouter_500Routes(b *testing.B)  { benchmarkRouterScale(b, 500) }
func BenchmarkRouter_1000Routes(b *testing.B) { benchmarkRouterScale(b, 1000) }

func benchmarkRouterScale(b *testing.B, n int) {
	b.ReportAllocs()

	r := router.NewRouter()
	for i := 0; i < n; i++ {
		r.AddRoute(&router.Route{
			Name:        fmt.Sprintf("route-%d", i),
			Path:        fmt.Sprintf("/svc/%d/resource/%d/action", i/10, i),
			BackendPool: "pool-1",
		})
	}

	// Target the middle route to avoid best/worst case
	target := n / 2
	path := fmt.Sprintf("/svc/%d/resource/%d/action", target/10, target)
	req := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := r.Match(req)
		if !ok {
			b.Fatal("expected match")
		}
	}
}

// ---------------------------------------------------------------------------
// Balancer Scaling Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRoundRobin_5Backends(b *testing.B)   { benchmarkRoundRobinScale(b, 5) }
func BenchmarkRoundRobin_50Backends(b *testing.B)  { benchmarkRoundRobinScale(b, 50) }
func BenchmarkRoundRobin_200Backends(b *testing.B) { benchmarkRoundRobinScale(b, 200) }

func benchmarkRoundRobinScale(b *testing.B, n int) {
	b.ReportAllocs()

	rr := balancer.NewRoundRobin()
	backends := createBackends(n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr.Next(nil, backends)
	}
}

func BenchmarkLeastConnections_5Backends(b *testing.B)   { benchmarkLCScale(b, 5) }
func BenchmarkLeastConnections_50Backends(b *testing.B)  { benchmarkLCScale(b, 50) }
func BenchmarkLeastConnections_200Backends(b *testing.B) { benchmarkLCScale(b, 200) }

func benchmarkLCScale(b *testing.B, n int) {
	b.ReportAllocs()

	lc := balancer.NewLeastConnections()
	backends := createBackends(n)
	for _, be := range backends {
		lc.Add(be)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lc.Next(nil, backends)
	}
}

// ---------------------------------------------------------------------------
// End-to-End Proxy Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkHTTPProxy_RealNetwork(b *testing.B) {
	b.ReportAllocs()

	srv := newBackendServer(256)
	defer srv.Close()

	proxy, cleanup := setupProxyStack(b, hostPort(srv), nil)
	defer cleanup()

	// Start a real proxy server
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	client := proxySrv.Client()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(proxySrv.URL + "/")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
	}
	b.SetBytes(256)
}

func BenchmarkHTTPProxy_RealNetwork_Parallel(b *testing.B) {
	b.ReportAllocs()

	srv := newBackendServer(256)
	defer srv.Close()

	proxy, cleanup := setupProxyStack(b, hostPort(srv), nil)
	defer cleanup()

	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	client := proxySrv.Client()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(proxySrv.URL + "/")
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
	b.SetBytes(256)
}

// ---------------------------------------------------------------------------
// Middleware Overhead Measurement
// ---------------------------------------------------------------------------

func BenchmarkMiddleware_RequestID(b *testing.B) {
	b.ReportAllocs()

	mw := middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{
		Generate: true,
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkMiddleware_Headers(b *testing.B) {
	b.ReportAllocs()

	mw := middleware.NewHeadersMiddleware(middleware.HeadersConfig{
		RequestSet: map[string]string{
			"X-Custom-1": "val1",
			"X-Custom-2": "val2",
		},
		ResponseSet: map[string]string{
			"X-Powered-By":           "OLB",
			"Server":                 "OpenLoadBalancer",
			"X-Content-Type-Options": "nosniff",
		},
		SecurityPreset: middleware.SecurityPresetStrict,
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkMiddleware_CORS(b *testing.B) {
	b.ReportAllocs()

	mw, err := middleware.NewCORSMiddleware(middleware.CORSConfig{
		AllowedOrigins:   []string{"https://example.com", "https://app.example.com"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})
	if err != nil {
		b.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.Header.Set("Origin", "https://example.com")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

// ---------------------------------------------------------------------------
// Request Context Pool Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRequestContext_NewRelease(b *testing.B) {
	b.ReportAllocs()

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := middleware.NewRequestContext(req, w)
		ctx.Release()
	}
}

func BenchmarkRequestContext_SetGet(b *testing.B) {
	b.ReportAllocs()

	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	w := httptest.NewRecorder()
	ctx := middleware.NewRequestContext(req, w)
	defer ctx.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Set("key", "value")
		_, _ = ctx.Get("key")
	}
}

// ---------------------------------------------------------------------------
// Response Writer Pool Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkResponseWriter_NewRelease(b *testing.B) {
	b.ReportAllocs()

	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rw := middleware.NewResponseWriter(w)
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("OK"))
		if releaser, ok := rw.(interface{ Release() }); ok {
			releaser.Release()
		}
	}
}

// ---------------------------------------------------------------------------
// Backend Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkBackend_AcquireRelease(b *testing.B) {
	b.ReportAllocs()

	be := backend.NewBackend("be-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !be.AcquireConn() {
			b.Fatal("failed to acquire")
		}
		be.ReleaseConn()
	}
}

func BenchmarkBackend_RecordRequest(b *testing.B) {
	b.ReportAllocs()

	be := backend.NewBackend("be-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		be.RecordRequest(1000000, 1024) // 1ms, 1KB
	}
}

func BenchmarkBackend_AcquireRelease_Parallel(b *testing.B) {
	b.ReportAllocs()

	be := backend.NewBackend("be-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if be.AcquireConn() {
				be.ReleaseConn()
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Backend Pool Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkPool_GetHealthyBackends(b *testing.B) {
	b.ReportAllocs()

	pool := backend.NewPool("bench-pool", "round_robin")
	for i := 0; i < 20; i++ {
		be := backend.NewBackend(fmt.Sprintf("be-%d", i), fmt.Sprintf("10.0.0.%d:8080", i))
		if i%3 == 0 {
			be.SetState(backend.StateDown)
		} else {
			be.SetState(backend.StateUp)
		}
		pool.AddBackend(be)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		healthy := pool.GetHealthyBackends()
		if len(healthy) == 0 {
			b.Fatal("expected healthy backends")
		}
	}
}

func BenchmarkPoolManager_GetPool(b *testing.B) {
	b.ReportAllocs()

	pm := backend.NewPoolManager()
	for i := 0; i < 50; i++ {
		pool := backend.NewPool(fmt.Sprintf("pool-%d", i), "round_robin")
		pm.AddPool(pool)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool := pm.GetPool(fmt.Sprintf("pool-%d", i%50))
		if pool == nil {
			b.Fatal("expected pool")
		}
	}
}

// ---------------------------------------------------------------------------
// Router Swap (Hot Reload) Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRouter_Swap(b *testing.B) {
	b.ReportAllocs()

	r := router.NewRouter()
	routes := make([]*router.Route, 100)
	for i := 0; i < 100; i++ {
		routes[i] = &router.Route{
			Name:        fmt.Sprintf("route-%d", i),
			Path:        fmt.Sprintf("/api/v1/service%d", i),
			BackendPool: "pool-1",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := r.Swap(routes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ---------------------------------------------------------------------------
// String Building Benchmarks (used internally)
// ---------------------------------------------------------------------------

func BenchmarkStringJoin_vs_Builder(b *testing.B) {
	parts := []string{"192.168.1.1", "10.0.0.1", "172.16.0.1"}

	b.Run("Join", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = strings.Join(parts, ", ")
		}
	})

	b.Run("Builder", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var sb strings.Builder
			for j, p := range parts {
				if j > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(p)
			}
			_ = sb.String()
		}
	})
}

// ---------------------------------------------------------------------------
// NoopMiddleware helper (for benchmarking chain overhead without side effects)
// ---------------------------------------------------------------------------

// NoopMiddleware is a zero-cost middleware used to measure chain dispatch overhead.
type NoopMiddleware struct {
	name     string
	priority int
}

func NewNoopMiddleware(name string, priority int) *NoopMiddleware {
	return &NoopMiddleware{name: name, priority: priority}
}

func (m *NoopMiddleware) Name() string  { return m.name }
func (m *NoopMiddleware) Priority() int { return m.priority }
func (m *NoopMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
