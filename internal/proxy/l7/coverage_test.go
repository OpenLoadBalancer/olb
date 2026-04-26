package l7

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/router"
)

// ============================================================================
// proxy.go coverage
// ============================================================================

// TestCov_getErrorHandler_Fallback tests getErrorHandler when the atomic
// value is nil, falling through to defaultErrorHandler.
func TestCov_getErrorHandler_Fallback(t *testing.T) {
	p := NewHTTPProxy(nil)
	// Clear the error handler to trigger the fallback path
	p.errorHandler = atomic.Value{}
	handler := p.getErrorHandler()
	if handler == nil {
		t.Fatal("expected non-nil error handler")
	}
	// Verify it is the defaultErrorHandler by invoking it
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler(rr, req, fmt.Errorf("test"))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from default handler, got %d", rr.Code)
	}
}

// TestCov_buildCachedHandler_NilMiddleware_MissingState tests the
// buildCachedHandler path where middlewareChain is nil and the request
// context is missing the requestState value.
func TestCov_buildCachedHandler_NilMiddleware_MissingState(t *testing.T) {
	p := NewHTTPProxy(nil)
	// middlewareChain is nil by default since we pass nil config extras
	p.buildCachedHandler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	// Don't set requestStateKey in context; the handler should write 500

	v := p.cachedHandler.Load()
	if v == nil {
		t.Fatal("cachedHandler should not be nil")
	}
	h, ok := v.(http.Handler)
	if !ok {
		t.Fatal("cachedHandler is not http.Handler")
	}
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing request state, got %d", rr.Code)
	}
}

// TestCov_buildCachedHandler_WithMiddleware_MissingState tests the
// buildCachedHandler path where middlewareChain is present and the request
// context is missing the requestState value.
func TestCov_buildCachedHandler_WithMiddleware_MissingState(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		MiddlewareChain: middleware.NewChain(),
	}
	p := NewHTTPProxy(config)
	p.buildCachedHandler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	v := p.cachedHandler.Load()
	h, ok := v.(http.Handler)
	if !ok {
		t.Fatal("cachedHandler is not http.Handler")
	}
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing request state, got %d", rr.Code)
	}
}

// TestCov_ServeHTTP_ProxyNotInitialized tests the fallback path where the
// cached handler cannot be loaded from the atomic.Value.
func TestCov_ServeHTTP_ProxyNotInitialized(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()

	p := NewHTTPProxy(&Config{
		Router:      routerInstance,
		PoolManager: poolManager,
	})
	// Add a route so the router matches
	route := &router.Route{Name: "test-route", Path: "/", BackendPool: "test-pool"}
	routerInstance.AddRoute(route)
	// Add a pool so router match succeeds
	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	poolManager.AddPool(pool)

	// Clear the cachedHandler to trigger the fallback
	p.cachedHandler = atomic.Value{} // empty, no value stored

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	p.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestCov_parseTrustedProxies_IPv6AndInvalid tests parseTrustedProxies with
// IPv6 bare IPs (=> /128) and invalid CIDR strings.
func TestCov_parseTrustedProxies_IPv6AndInvalid(t *testing.T) {
	nets := parseTrustedProxies([]string{
		"::1",          // bare IPv6 => /128
		"not-an-ip",    // invalid => skip
		"10.0.0.1/abc", // invalid CIDR => skip
		"192.168.1.1",  // bare IPv4 => /32
		"2001:db8::1",  // bare IPv6 => /128
	})
	if len(nets) != 3 {
		t.Fatalf("expected 3 nets, got %d", len(nets))
	}
	// Verify IPv6 /128
	foundV6Loopback := false
	for _, n := range nets {
		if n.Contains(net.ParseIP("::1")) {
			foundV6Loopback = true
		}
	}
	if !foundV6Loopback {
		t.Error("expected ::1/128 in trusted nets")
	}
}

// TestCov_proxyHandler_PassiveCheckerOnGRPCErr tests proxyHandler's protocol-
// specific path where a gRPC request fails and passiveChecker records it.
func TestCov_proxyHandler_PassiveCheckerOnGRPCErr(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	pc := health.NewPassiveChecker(nil)

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		PassiveChecker:  pc,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      1,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("grpc-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", closedPortAddr(t))
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "grpc-route", Path: "/", BackendPool: "grpc-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc")
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)
	// Should get an error response (backend is unreachable)
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for unreachable gRPC backend")
	}
}

// TestCov_proxyHandler_PassiveCheckerOnSSEErr tests proxyHandler's protocol-
// specific path where an SSE request fails and passiveChecker records it.
func TestCov_proxyHandler_PassiveCheckerOnSSEErr(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	pc := health.NewPassiveChecker(nil)

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		PassiveChecker:  pc,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      1,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("sse-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", closedPortAddr(t))
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "sse-route", Path: "/", BackendPool: "sse-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for unreachable SSE backend")
	}
}

// TestCov_proxyHandler_AllBackendsAttempted tests the retry loop where all
// backends have been attempted and filtered out.
func TestCov_proxyHandler_AllBackendsAttempted(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      3,
		DialTimeout:     500 * time.Millisecond,
		ProxyTimeout:    1 * time.Second,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("retry-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	// Single backend that will fail, so on retry it gets filtered
	b := backend.NewBackend("b1", closedPortAddr(t))
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "retry-route", Path: "/", BackendPool: "retry-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	// Should get 503 (all backends attempted)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestCov_proxyHandler_RetryWithPassiveChecker tests retry with a passive
// checker present to cover the RecordFailure path on retry errors.
func TestCov_proxyHandler_RetryWithPassiveChecker(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	pc := health.NewPassiveChecker(nil)

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		PassiveChecker:  pc,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      3,
		DialTimeout:     500 * time.Millisecond,
		ProxyTimeout:    1 * time.Second,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("retry-pc-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", closedPortAddr(t))
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "retry-pc-route", Path: "/", BackendPool: "retry-pc-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestCov_proxyHandler_NonRetryableError tests the break path when an error
// is not retryable.
func TestCov_proxyHandler_NonRetryableError(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      3,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("nonretry-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	addr := backendServer.Listener.Addr().String()
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "nonretry-route", Path: "/", BackendPool: "nonretry-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)
	// Should succeed (200) since the backend is reachable
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for reachable backend, got %d", rr.Code)
	}
}

// ============================================================================
// shadow.go coverage
// ============================================================================

// TestCov_ShadowManager_Wait tests the Wait method.
func TestCov_ShadowManager_Wait(t *testing.T) {
	t.Run("non-nil manager", func(t *testing.T) {
		sm := NewShadowManager(ShadowConfig{Enabled: true, Percentage: 100})
		// Should not block or panic
		sm.Wait()
	})
	t.Run("nil manager", func(t *testing.T) {
		var sm *ShadowManager
		sm.Wait()
	})
}

// TestCov_ShadowManager_ShouldShadowRequest_Nil tests ShouldShadowRequest on nil.
func TestCov_ShadowManager_ShouldShadowRequest_Nil(t *testing.T) {
	var sm *ShadowManager
	req := httptest.NewRequest("GET", "/", nil)
	if sm.ShouldShadowRequest(req) {
		t.Error("nil ShouldShadowRequest should return false")
	}
}

// TestCov_ShadowRequest_BodyExceedsMaxSize tests the path where body exceeds
// the configured MaxBodySize and gets restored via MultiReader.
func TestCov_ShadowRequest_BodyExceedsMaxSize(t *testing.T) {
	var received atomic.Int32
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: false,
		CopyBody:    true,
		MaxBodySize: 5, // Very small limit
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	be := backend.NewBackend("shadow-small", backendAddr)
	be.SetState(backend.StateUp)
	sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100.0)

	// Send a body larger than MaxBodySize
	largeBody := bytes.NewReader([]byte("this body is definitely longer than five bytes"))
	req := httptest.NewRequest(http.MethodPost, "/test", largeBody)
	req.Host = "example.com"

	sm.ShadowRequest(req)

	// Wait for shadow request goroutine
	sm.Wait()

	// Verify body was restored (can be re-read)
	restored, _ := io.ReadAll(req.Body)
	if string(restored) != "this body is definitely longer than five bytes" {
		t.Errorf("body not properly restored, got %q", string(restored))
	}
}

// TestCov_ShadowRequest_BodyCopySuccess tests the path where body is within
// MaxBodySize and gets properly copied and restored.
func TestCov_ShadowRequest_BodyCopySuccess(t *testing.T) {
	var receivedBody []byte
	var mu sync.Mutex
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBody = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: true,
		CopyBody:    true,
		MaxBodySize: 1024,
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	be := backend.NewBackend("shadow-body", backendAddr)
	be.SetState(backend.StateUp)
	sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100.0)

	body := bytes.NewReader([]byte("small test body"))
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Host = "example.com"

	sm.ShadowRequest(req)
	sm.Wait()

	// Verify body was restored
	restored, _ := io.ReadAll(req.Body)
	if string(restored) != "small test body" {
		t.Errorf("body not restored, got %q", string(restored))
	}

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	if string(receivedBody) != "small test body" {
		t.Errorf("shadow body = %q, want 'small test body'", string(receivedBody))
	}
	mu.Unlock()
}

// TestCov_sendShadow_CustomDenyHeaders tests sendShadow with custom deny headers.
func TestCov_sendShadow_CustomDenyHeaders(t *testing.T) {
	var receivedHeaders http.Header
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: true,
		CopyBody:    false,
		DenyHeaders: []string{"X-Secret", "Authorization"},
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("X-Secret", "secret-value")
	req.Header.Set("X-Public", "public-value")
	req.Header.Set("Authorization", "Bearer token")

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("test", backendAddr)},
		Percentage: 100.0,
	}

	sm.sendShadow(req, backendAddr, target, nil)
	time.Sleep(200 * time.Millisecond)

	if receivedHeaders.Get("X-Secret") != "" {
		t.Error("X-Secret should be stripped by custom deny headers")
	}
	if receivedHeaders.Get("Authorization") != "" {
		t.Error("Authorization should be stripped by custom deny headers")
	}
	if receivedHeaders.Get("X-Public") != "public-value" {
		t.Error("X-Public should be copied")
	}
}

// TestCov_sendShadow_InvalidURL tests sendShadow with a URL that causes
// http.NewRequestWithContext to fail.
func TestCov_sendShadow_InvalidURL(t *testing.T) {
	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: false,
		CopyBody:    false,
		Timeout:     100 * time.Millisecond,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	// Set a URL with control characters that will fail http.NewRequestWithContext
	req.URL = &url.URL{
		Scheme: "http",
		Host:   "invalid host with spaces:12345",
		Path:   "/test",
	}

	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("test", "127.0.0.1:8080")},
		Percentage: 100.0,
	}

	// Should not panic; NewRequestWithContext returns error => early return
	sm.sendShadow(req, "invalid host with spaces:12345", target, nil)
}

// TestCov_ShadowRequest_SemaphoreFull tests the semaphore-full drop path by
// filling the semaphore with a custom ShadowManager.
func TestCov_ShadowRequest_SemaphoreFull(t *testing.T) {
	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: false,
		CopyBody:    false,
		Timeout:     5 * time.Second,
	}
	sm := NewShadowManager(config)

	be := backend.NewBackend("test", "127.0.0.1:0")
	be.SetState(backend.StateUp)
	sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100.0)

	// Fill the semaphore
	for i := 0; i < maxConcurrentShadow; i++ {
		sm.sem <- struct{}{}
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"

	// This should hit the semaphore-full path and increment droppedTotal
	sm.ShadowRequest(req)

	sm.Wait()

	// Drain semaphore
	for i := 0; i < maxConcurrentShadow; i++ {
		<-sm.sem
	}

	// Verify dropped count > 0
	if sm.droppedTotal.Get() == 0 {
		t.Error("expected dropped total > 0 when semaphore is full")
	}
}

// ============================================================================
// websocket.go coverage
// ============================================================================

// TestCov_HandleWebSocket_MaxConnsPath tests the MaxConns code path with
// connection tracking and the defer decrement.
func TestCov_HandleWebSocket_MaxConnsPath(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\n"))
		time.Sleep(100 * time.Millisecond)
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		MaxConns:        10,
		IdleTimeout:     200 * time.Millisecond,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	bufReader := bufio.NewReader(strings.NewReader(""))
	bufWriter := bufio.NewWriter(io.Discard)
	rw := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufReader, bufWriter),
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")

	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(rw, req, b)
	}()

	// Read 101 response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 4096)
	n, _ := clientConn.Read(respBuf)
	if !strings.Contains(string(respBuf[:n]), "101") {
		t.Errorf("expected 101, got: %s", string(respBuf[:n]))
	}

	// Verify active connections was incremented during the request
	if wh.ActiveConns() == 0 {
		t.Error("expected active conns > 0 during WebSocket handling")
	}

	clientConn.Close()
	serverConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket: %v", err)
	case <-time.After(3 * time.Second):
		t.Log("HandleWebSocket timed out")
	}

	// After completion, connections should be decremented
	if wh.ActiveConns() != 0 {
		t.Errorf("expected 0 active conns after close, got %d", wh.ActiveConns())
	}
}

// TestCov_HandleWebSocket_WSConnFail tests the path where
// the backend connection fails.
func TestCov_HandleWebSocket_WSConnFail(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})

	// Backend with ws:// prefix that will strip properly but write to closed conn
	b := backend.NewBackend("b1", "ws://"+closedPortAddr(t))
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "test-key")

	err := wh.HandleWebSocket(w, req, b)
	if err == nil {
		t.Error("expected error for failed backend connection")
	}
}

// TestCov_HandleWebSocket_BufferedClientAndBackendData tests the path where
// both client and backend have buffered data after the upgrade handshake.
func TestCov_HandleWebSocket_BufferedClientAndBackendData(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		// Send 101 with extra data after headers (will be in backendBuf)
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\nbackend-extra-data"))
		// Wait then close
		time.Sleep(200 * time.Millisecond)
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     300 * time.Millisecond,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	// Client with pre-buffered data
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	clientBufData := "pre-buffered-client"
	bufReader := bufio.NewReader(strings.NewReader(clientBufData))
	bufWriter := bufio.NewWriter(io.Discard)
	rw := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufReader, bufWriter),
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")

	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(rw, req, b)
	}()

	// Read the 101 response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 8192)
	n, _ := clientConn.Read(respBuf)
	result := string(respBuf[:n])
	if !strings.Contains(result, "101") {
		t.Errorf("expected 101 response, got: %s", result)
	}

	clientConn.Close()
	serverConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket returned: %v", err)
	case <-time.After(3 * time.Second):
		t.Log("HandleWebSocket timed out")
	}
}

// TestCov_HandleWebSocket_BackendReadResponseError tests where the backend
// sends an incomplete response that fails to parse.
func TestCov_HandleWebSocket_BackendReadResponseError(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		// Send incomplete response then close
		conn.Write([]byte("HTTP/1.1 101 "))
		conn.Close()
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	w := httptest.NewRecorder()

	err = wh.HandleWebSocket(w, req, b)
	if err == nil {
		t.Error("expected error for incomplete backend response")
	}
}

// TestCov_writeUpgradeRequest_EmptyPath tests writeUpgradeRequest when path is empty.
func TestCov_writeUpgradeRequest_EmptyPath(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	req := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: ""},
		Header: http.Header{},
	}
	b := backend.NewBackend("b1", "10.0.0.1:8080")

	done := make(chan struct{})
	go func() {
		defer close(done)
		wh := NewWebSocketHandler(nil)
		wh.writeUpgradeRequest(conn2, req, b)
	}()

	buf := make([]byte, 4096)
	conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn1.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	result := string(buf[:n])
	if !strings.Contains(result, "GET / HTTP/1.1") {
		t.Errorf("expected 'GET /' for empty path, got: %s", result)
	}
	<-done
}

// ============================================================================
// grpc.go coverage
// ============================================================================

// TestCov_HandleGRPC_BackendAtMaxConns tests gRPC handler when backend is at
// max connections.
func TestCov_HandleGRPC_BackendAtMaxConns(t *testing.T) {
	gh := NewGRPCHandler(nil)
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	b.SetMaxConns(1)
	b.AcquireConn()
	defer b.ReleaseConn()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc")

	err := gh.HandleGRPC(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "max connections") {
		t.Errorf("expected max connections error, got %v", err)
	}
}

// TestCov_HandleGRPC_WithTrailers tests the trailers copy path.
func TestCov_HandleGRPC_WithTrailers(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trailer", "Grpc-Status")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
		// Set trailer after body
		w.(http.Flusher).Flush()
	}))
	defer backendServer.Close()

	gh := NewGRPCHandler(nil)
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc")

	err := gh.HandleGRPC(w, req, b)
	// The request may fail since the backend is HTTP/1.1, not gRPC,
	// but we just need to exercise the transport path
	t.Logf("HandleGRPC result: %v", err)
}

// TestCov_prepareGRPCRequest_InvalidBackendAddress tests the url.Parse error path.
func TestCov_prepareGRPCRequest_InvalidBackendAddress(t *testing.T) {
	gh := NewGRPCHandler(nil)
	b := backend.NewBackend("b1", "invalid host with spaces:12345")
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc")

	outReq, err := gh.prepareGRPCRequest(req, b)
	if err == nil {
		t.Error("expected error for invalid backend address")
		_ = outReq
	}
}

// TestCov_encodeTrailersAsGRPCWebFrame_EstimateBelow32 tests the path where
// the estimated size is below 32.
func TestCov_encodeTrailersAsGRPCWebFrame_EstimateBelow32(t *testing.T) {
	// Empty trailers => estimated stays at 64 (>= 32), so we need to make
	// the estimate go below 32. Since estimated starts at 64 and the loop
	// adds to it, we need no trailers but set estimated < 32.
	// The code sets estimated = 64 initially then adds for each trailer.
	// estimated < 32 can only happen if we somehow have negative additions.
	// Actually reading the code more carefully:
	//   estimated := 64
	//   for ... { estimated += ... }
	//   if estimated < 32 { estimated = 32 }
	// This can only be hit if the loop doesn't execute (no trailers) and
	// estimated starts at 64 which is >= 32. So the "if estimated < 32"
	// is dead code. Let's just test with empty trailers.
	frame := encodeTrailersAsGRPCWebFrame(http.Header{})
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}
	// Should have 5-byte header + "grpc-status: 0\r\n"
	if frame[0] != 0x80 {
		t.Errorf("expected trailer flag 0x80, got 0x%02x", frame[0])
	}
}

// TestCov_parseGRPCFrame_Oversized tests the path where frame length exceeds max.
func TestCov_parseGRPCFrame_Oversized(t *testing.T) {
	// Create a reader with a frame header that has length > 4MB
	header := make([]byte, 5)
	header[0] = 0                                        // not compressed
	binary.BigEndian.PutUint32(header[1:5], 5*1024*1024) // 5MB > 4MB max
	reader := bytes.NewReader(header)

	_, err := parseGRPCFrame(reader)
	if err == nil {
		t.Fatal("expected error for oversized frame")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected exceeds maximum error, got: %v", err)
	}
}

// TestCov_writeGRPCFrame_NoData tests writeGRPCFrame with empty data.
func TestCov_writeGRPCFrame_NoData(t *testing.T) {
	var buf bytes.Buffer
	frame := &gRPCFrame{
		Compressed: false,
		Length:     0,
		Data:       nil,
	}
	err := writeGRPCFrame(&buf, frame)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if buf.Len() != 5 {
		t.Errorf("expected 5 bytes (header only), got %d", buf.Len())
	}
}

// TestCov_HandleGRPCWeb_TextMode tests gRPC-Web text mode decode path.
func TestCov_HandleGRPCWeb_TextMode(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("grpc-response"))
	}))
	defer backendServer.Close()

	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	// Encode body as base64 (text mode)
	originalBody := []byte("grpc-text-body")
	encodedBody := base64.StdEncoding.EncodeToString(originalBody)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(encodedBody)))
	req.Header.Set("Content-Type", "application/grpc-web-text+proto")

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	t.Logf("HandleGRPCWeb text mode: %v", err)
}

// TestCov_HandleGRPCWeb_ResponseBodyError tests the response body read error path.
func TestCov_HandleGRPCWeb_ResponseBodyError(t *testing.T) {
	// Create a server that closes connection mid-response
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read request
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		// Send incomplete response then close
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/grpc\r\nTransfer-Encoding: chunked\r\n\r\n"))
		conn.Close()
	}()

	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	w := httptest.NewRecorder()
	err = gwh.HandleGRPCWeb(w, req, b)
	t.Logf("HandleGRPCWeb response error: %v", err)
}

// TestCov_HandleGRPCWeb_GrpcStatusHeader tests the path where Grpc-Status
// is sent as a regular response header.
func TestCov_HandleGRPCWeb_GrpcStatusHeader(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "ok")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response-data"))
	}))
	defer backendServer.Close()

	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	t.Logf("HandleGRPCWeb Grpc-Status header: %v", err)
}

// TestCov_HandleGRPCWeb_WriteError tests the path where writing response fails.
func TestCov_HandleGRPCWeb_WriteError(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer backendServer.Close()

	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	// Use a response writer that fails on Write
	w := &errorWriteResponseWriter{}

	err := gwh.HandleGRPCWeb(w, req, b)
	t.Logf("HandleGRPCWeb write error: %v", err)
}

type errorWriteResponseWriter struct {
	header http.Header
}

func (e *errorWriteResponseWriter) Header() http.Header {
	if e.header == nil {
		e.header = make(http.Header)
	}
	return e.header
}
func (e *errorWriteResponseWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("write failed")
}
func (e *errorWriteResponseWriter) WriteHeader(int) {}

// TestCov_prepareGRPCWebRequest_InvalidAddress tests the url.Parse error path.
func TestCov_prepareGRPCWebRequest_InvalidAddress(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	b := backend.NewBackend("b1", "invalid host:port")

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	_, err := gwh.prepareGRPCWebRequest(req, b, []byte("body"))
	if err == nil {
		t.Error("expected error for invalid backend address")
	}
}

// TestCov_prepareGRPCWebRequest_WithTLSAndPriorXFF tests the TLS and prior XFF paths.
func TestCov_prepareGRPCWebRequest_WithTLSAndPriorXFF(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	b := backend.NewBackend("b1", "127.0.0.1:8080")

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.TLS = &tls.ConnectionState{}

	outReq, err := gwh.prepareGRPCWebRequest(req, b, []byte("body"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outReq.Header.Get("X-Forwarded-Proto") != "https" {
		t.Error("expected X-Forwarded-Proto to be https")
	}
	xff := outReq.Header.Get("X-Forwarded-For")
	if !strings.Contains(xff, "10.0.0.1,") {
		t.Errorf("expected appended XFF, got: %s", xff)
	}
}

// ============================================================================
// sse.go coverage
// ============================================================================

// TestCov_HandleSSE_BackendAtMaxConns tests SSE handler when backend is at max connections.
func TestCov_HandleSSE_BackendAtMaxConns(t *testing.T) {
	sh := NewSSEHandler(nil)
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	b.SetMaxConns(1)
	b.AcquireConn()
	defer b.ReleaseConn()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	err := sh.HandleSSE(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "max connections") {
		t.Errorf("expected max connections error, got %v", err)
	}
}

// TestCov_HandleSSE_Disabled tests SSE handler when disabled.
func TestCov_HandleSSE_Disabled(t *testing.T) {
	sh := NewSSEHandler(&SSEConfig{EnableSSE: false})
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	err := sh.HandleSSE(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "sse disabled") {
		t.Errorf("expected sse disabled error, got %v", err)
	}
}

// TestCov_SSEProxy_NoBackendAvailable tests SSEProxy.ServeHTTP when no backend
// is available from the balancer.
func TestCov_SSEProxy_NoBackendAvailable(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()

	httpProxy := NewHTTPProxy(&Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		MiddlewareChain: middleware.NewChain(),
	})

	sseProxy := NewSSEProxy(httpProxy, nil)

	pool := backend.NewPool("sse-pool", "round_robin")
	pool.SetBalancer(&nilBalancer{})
	b := backend.NewBackend("b1", "127.0.0.1:9999")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "sse-route", Path: "/", BackendPool: "sse-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	sseProxy.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 when no backend available")
	}
}

// TestCov_readLineWithTimeout_Timeout tests readLineWithTimeout's timeout
// path with onCancel callback.
func TestCov_readLineWithTimeout_Timeout(t *testing.T) {
	sh := NewSSEHandler(&SSEConfig{
		EnableSSE:   true,
		IdleTimeout: 100 * time.Millisecond,
	})

	ctx := context.Background()
	reader := bufio.NewReader(&slowReader{})
	onCancelCalled := false
	onCancel := func() {
		onCancelCalled = true
	}

	line, err := sh.readLineWithTimeout(ctx, reader, 50*time.Millisecond, onCancel)
	if err == nil {
		t.Error("expected timeout error")
	}
	if line != nil {
		t.Errorf("expected nil line, got %q", string(line))
	}
	if !onCancelCalled {
		t.Error("expected onCancel to be called")
	}
}

// slowReader is an io.Reader that never returns data.
type slowReader struct{}

func (s *slowReader) Read([]byte) (int, error) {
	time.Sleep(5 * time.Second) // Block forever
	return 0, io.EOF
}

// TestCov_readLineWithTimeout_ZeroTimeout tests readLineWithTimeout with zero
// timeout, which should use direct ReadBytes.
func TestCov_readLineWithTimeout_ZeroTimeout(t *testing.T) {
	sh := NewSSEHandler(nil)
	ctx := context.Background()
	reader := bufio.NewReader(strings.NewReader("line1\nline2\n"))

	line, err := sh.readLineWithTimeout(ctx, reader, 0, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if string(line) != "line1\n" {
		t.Errorf("expected 'line1\\n', got %q", string(line))
	}
}

// ============================================================================
// http2.go coverage
// ============================================================================

// TestCov_HTTP2Listener_StartH2C tests the h2c handler wrapping path in Start.
func TestCov_HTTP2Listener_StartH2C(t *testing.T) {
	l, err := NewHTTP2Listener(&HTTP2ListenerOptions{
		Name:    "h2c-test",
		Address: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		Config: &HTTP2Config{
			EnableHTTP2: true,
			EnableH2C:   true,
		},
	})
	if err != nil {
		t.Fatalf("NewHTTP2Listener: %v", err)
	}

	err = l.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	if !l.IsRunning() {
		t.Error("expected listener to be running")
	}

	// Verify address is set
	addr := l.Address()
	if addr == "" {
		t.Error("expected non-empty address")
	}
}

// TestCov_HTTP2Listener_StopTwice tests the double-stop path where the second
// Stop finds the listener not running.
func TestCov_HTTP2Listener_StopTwice(t *testing.T) {
	l, err := NewHTTP2Listener(&HTTP2ListenerOptions{
		Name:    "stop-twice",
		Address: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		Config:  DefaultHTTP2Config(),
	})
	if err != nil {
		t.Fatalf("NewHTTP2Listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx := context.Background()
	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Second stop should return "not running" error
	err = l.Stop(ctx)
	if err == nil {
		t.Error("expected error on second stop")
	}
}

// TestCov_HTTP2Listener_StopWithDeadline tests the shutdown timeout path.
func TestCov_HTTP2Listener_StopWithDeadline(t *testing.T) {
	l, err := NewHTTP2Listener(&HTTP2ListenerOptions{
		Name:    "deadline-test",
		Address: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Slow handler that blocks forever
			select {}
		}),
		Config: DefaultHTTP2Config(),
	})
	if err != nil {
		t.Fatalf("NewHTTP2Listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Connect a client that will keep the connection busy
	addr := l.Address()
	go func() {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err != nil {
			return
		}
		defer conn.Close()
		// Send a request that will block
		fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
		time.Sleep(5 * time.Second)
	}()

	time.Sleep(50 * time.Millisecond) // let the connection establish

	// Use a very short deadline to trigger the timeout path
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err = l.Stop(ctx)
	t.Logf("Stop with deadline result: %v", err)
}

// TestCov_HTTP2Listener_StartAlreadyRunning tests starting a listener that's
// already running.
func TestCov_HTTP2Listener_StartAlreadyRunning(t *testing.T) {
	l, err := NewHTTP2Listener(&HTTP2ListenerOptions{
		Name:    "already-running",
		Address: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		Config:  DefaultHTTP2Config(),
	})
	if err != nil {
		t.Fatalf("NewHTTP2Listener: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	// Second start should fail
	err = l.Start()
	if err == nil {
		t.Error("expected error on second start")
	}
}

// ============================================================================
// proxy.go - additional coverage for buildCachedHandler with middleware chain
// ============================================================================

// TestCov_buildCachedHandler_WithMiddlewareAndValidState tests the middleware
// chain path where the requestState is properly set in context.
func TestCov_buildCachedHandler_WithMiddlewareAndValidState(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backendServer.Close()

	pool := backend.NewPool("test-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	addr := backendServer.Listener.Addr().String()
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "test-route", Path: "/", BackendPool: "test-pool"}
	routerInstance.AddRoute(route)

	config := &Config{
		Router:              routerInstance,
		PoolManager:         poolManager,
		ConnPoolManager:     connPoolManager,
		HealthChecker:       healthChecker,
		MiddlewareChain:     middleware.NewChain(),
		ProxyTimeout:        5 * time.Second,
		DialTimeout:         1 * time.Second,
		MaxRetries:          3,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
	}
	p := NewHTTPProxy(config)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ============================================================================
// proxy.go - passive checker on WSErr
// ============================================================================

// TestCov_proxyHandler_WSErrWithPassiveChecker tests WebSocket error with
// passive checker present.
func TestCov_proxyHandler_WSErrWithPassiveChecker(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	pc := health.NewPassiveChecker(nil)

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		PassiveChecker:  pc,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      1,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("ws-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", closedPortAddr(t))
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "ws-route", Path: "/", BackendPool: "ws-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	// Should get error (backend unreachable)
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for unreachable WS backend")
	}
}

// ============================================================================
// proxy.go - passive checker on GRPCWebErr
// ============================================================================

// TestCov_proxyHandler_GRPCWebErrWithPassiveChecker tests gRPC-Web error with
// passive checker present.
func TestCov_proxyHandler_GRPCWebErrWithPassiveChecker(t *testing.T) {
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	pc := health.NewPassiveChecker(nil)

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		PassiveChecker:  pc,
		MiddlewareChain: middleware.NewChain(),
		MaxRetries:      1,
	}
	p := NewHTTPProxy(config)

	pool := backend.NewPool("grpcweb-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", closedPortAddr(t))
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "grpcweb-route", Path: "/", BackendPool: "grpcweb-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for unreachable gRPC-Web backend")
	}
}

// TestCov_HandleGRPCWeb_BodyTooLarge tests the path where the non-text request
// body exceeds MaxMessageSize.
func TestCov_HandleGRPCWeb_BodyTooLarge(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(&GRPCConfig{
		EnableGRPC:     true,
		EnableGRPCWeb:  true,
		MaxMessageSize: 10,
	}))

	// Body larger than MaxMessageSize
	largeBody := make([]byte, 100)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil {
		t.Error("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected exceeds maximum error, got: %v", err)
	}
}

// TestCov_HandleGRPCWeb_TextModeBodyTooLarge tests text mode with body too large.
func TestCov_HandleGRPCWeb_TextModeBodyTooLarge(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(&GRPCConfig{
		EnableGRPC:     true,
		EnableGRPCWeb:  true,
		MaxMessageSize: 10,
	}))

	// Create a large base64-encoded body
	largeBody := make([]byte, 100)
	encoded := base64.StdEncoding.EncodeToString(largeBody)
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(encoded)))
	req.Header.Set("Content-Type", "application/grpc-web-text+proto")

	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil {
		t.Error("expected error for oversized text body")
	}
}

// TestCov_HandleGRPCWeb_TextModeInvalidBase64 tests text mode with invalid base64.
func TestCov_HandleGRPCWeb_TextModeInvalidBase64(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(&GRPCConfig{
		EnableGRPC:     true,
		EnableGRPCWeb:  true,
		MaxMessageSize: 1024,
	}))

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("!!!invalid-base64!!!")))
	req.Header.Set("Content-Type", "application/grpc-web-text+proto")

	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "decoding base64") {
		t.Errorf("expected base64 decode error, got: %v", err)
	}
}

// TestCov_HandleGRPCWeb_Disabled tests HandleGRPCWeb when disabled.
func TestCov_HandleGRPCWeb_Disabled(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(&GRPCConfig{
		EnableGRPC:    true,
		EnableGRPCWeb: false,
	}))

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected disabled error, got %v", err)
	}
}

// TestCov_HandleGRPCWeb_BackendAtMaxConns tests when backend is at max conns.
func TestCov_HandleGRPCWeb_BackendAtMaxConns(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetMaxConns(1)
	b.AcquireConn()
	defer b.ReleaseConn()

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "max connections") {
		t.Errorf("expected max connections error, got %v", err)
	}
}

// TestCov_HandleGRPC_Disabled tests HandleGRPC when disabled.
func TestCov_HandleGRPC_Disabled(t *testing.T) {
	gh := NewGRPCHandler(&GRPCConfig{EnableGRPC: false})
	b := backend.NewBackend("b1", "127.0.0.1:8080")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Content-Type", "application/grpc")

	err := gh.HandleGRPC(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected disabled error, got %v", err)
	}
}

// TestCov_HandleGRPC_InvalidBackendAddress tests HandleGRPC with an invalid backend address.
func TestCov_HandleGRPC_InvalidBackendAddress(t *testing.T) {
	gh := NewGRPCHandler(nil)
	b := backend.NewBackend("b1", "invalid host:12345")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc")

	err := gh.HandleGRPC(w, req, b)
	if err == nil {
		t.Error("expected error for invalid backend address")
	}
}

// TestCov_writeGRPCFrame_Compressed tests writeGRPCFrame with compressed flag.
func TestCov_writeGRPCFrame_Compressed(t *testing.T) {
	var buf bytes.Buffer
	frame := &gRPCFrame{
		Compressed: true,
		Length:     4,
		Data:       []byte("test"),
	}
	err := writeGRPCFrame(&buf, frame)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if buf.Len() != 9 { // 5 header + 4 data
		t.Errorf("expected 9 bytes, got %d", buf.Len())
	}
	if buf.Bytes()[0] != 1 { // compressed flag
		t.Errorf("expected compressed flag 1, got %d", buf.Bytes()[0])
	}
}

// TestCov_parseGRPCFrame_ReadFullError tests parseGRPCFrame when ReadFull fails.
func TestCov_parseGRPCFrame_ReadFullError(t *testing.T) {
	_, err := parseGRPCFrame(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty reader")
	}
}

// TestCov_parseGRPCFrame_DataReadError tests parseGRPCFrame when data read fails.
func TestCov_parseGRPCFrame_DataReadError(t *testing.T) {
	// Provide header but not enough data
	header := make([]byte, 5)
	header[0] = 0
	binary.BigEndian.PutUint32(header[1:5], 1000) // requests 1000 bytes
	reader := bytes.NewReader(header)

	_, err := parseGRPCFrame(reader)
	if err == nil {
		t.Error("expected error for truncated frame data")
	}
}

// TestCov_parseGRPCFrame_Success tests a successful parseGRPCFrame.
func TestCov_parseGRPCFrame_Success(t *testing.T) {
	data := []byte("hello grpc")
	header := make([]byte, 5)
	header[0] = 0 // not compressed
	binary.BigEndian.PutUint32(header[1:5], uint32(len(data)))
	fullFrame := append(header, data...)

	frame, err := parseGRPCFrame(bytes.NewReader(fullFrame))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Compressed {
		t.Error("expected not compressed")
	}
	if frame.Length != uint32(len(data)) {
		t.Errorf("expected length %d, got %d", len(data), frame.Length)
	}
	if string(frame.Data) != "hello grpc" {
		t.Errorf("expected 'hello grpc', got %q", string(frame.Data))
	}
}

// TestCov_encodeTrailersAsGRPCWebFrame_WithTrailers tests with actual trailers.
func TestCov_encodeTrailersAsGRPCWebFrame_WithTrailers(t *testing.T) {
	trailers := http.Header{}
	trailers.Set("Grpc-Status", "0")
	trailers.Set("Grpc-Message", "ok")

	frame := encodeTrailersAsGRPCWebFrame(trailers)
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}
	if frame[0] != 0x80 {
		t.Errorf("expected trailer flag 0x80, got 0x%02x", frame[0])
	}
	// Verify it contains the trailer data
	data := string(frame[5:])
	if !strings.Contains(data, "Grpc-Status: 0") {
		t.Error("expected Grpc-Status in trailer frame")
	}
	if !strings.Contains(data, "Grpc-Message: ok") {
		t.Error("expected Grpc-Message in trailer frame")
	}
}

// TestCov_HandleGRPCWeb_ReadReqBodyError tests the non-text request body read error.
func TestCov_HandleGRPCWeb_ReadReqBodyError(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(&GRPCConfig{
		EnableGRPC:     true,
		EnableGRPCWeb:  true,
		MaxMessageSize: 1024,
	}))

	req := httptest.NewRequest("POST", "/", &coverageErrorReader{})
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil {
		t.Error("expected error for read failure")
	}
	if !strings.Contains(err.Error(), "reading gRPC-Web request body") {
		t.Errorf("expected request body read error, got: %v", err)
	}
}

// coverageErrorReader always returns an error on Read.
type coverageErrorReader struct{}

func (e *coverageErrorReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("read error for test")
}

// TestCov_HandleGRPCWeb_TextModeReadError tests text mode request body read error.
func TestCov_HandleGRPCWeb_TextModeReadError(t *testing.T) {
	gwh := NewGRPCWebHandler(NewGRPCHandler(&GRPCConfig{
		EnableGRPC:     true,
		EnableGRPCWeb:  true,
		MaxMessageSize: 1024,
	}))

	req := httptest.NewRequest("POST", "/", &coverageErrorReader{})
	req.Header.Set("Content-Type", "application/grpc-web-text+proto")

	b := backend.NewBackend("b1", "127.0.0.1:8080")
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	if err == nil {
		t.Error("expected error for text mode read failure")
	}
	if !strings.Contains(err.Error(), "reading gRPC-Web request body") {
		t.Errorf("expected request body read error, got: %v", err)
	}
}

// TestCov_SSE_NonSSEResponse tests HandleSSE when backend returns a non-SSE response.
func TestCov_SSE_NonSSEResponse(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backendServer.Close()

	sh := NewSSEHandler(nil)
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	err := sh.HandleSSE(w, req, b)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestCov_SSE_NoFlusherFallback tests the SSE streaming fallback when the
// ResponseWriter does not implement Flusher.
func TestCov_SSE_NoFlusherFallback(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: test\n\n"))
	}))
	defer backendServer.Close()

	sh := NewSSEHandler(nil)
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	// ResponseWriter that does NOT implement Flusher
	w := &noFlusherResponseWriter{}
	req := httptest.NewRequest("GET", "/", nil)

	err := sh.HandleSSE(w, req, b)
	if err != nil {
		t.Logf("HandleSSE with no flusher: %v", err)
	}
}

type noFlusherResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (n *noFlusherResponseWriter) Header() http.Header {
	if n.header == nil {
		n.header = make(http.Header)
	}
	return n.header
}
func (n *noFlusherResponseWriter) Write(b []byte) (int, error) {
	return n.body.Write(b)
}
func (n *noFlusherResponseWriter) WriteHeader(code int) {
	n.code = code
}

// TestCov_SSE_TimeoutKeepalive tests the SSE idle timeout path that sends a keepalive.
func TestCov_SSE_TimeoutKeepalive(t *testing.T) {
	// Backend that sends one event then nothing (triggers idle timeout)
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: first\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Don't send more data to trigger idle timeout
		time.Sleep(5 * time.Second)
	}))
	defer backendServer.Close()

	sh := NewSSEHandler(&SSEConfig{
		EnableSSE:    true,
		IdleTimeout:  100 * time.Millisecond,
		MaxEventSize: 1024 * 1024,
	})
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	done := make(chan error, 1)
	go func() {
		done <- sh.HandleSSE(w, req, b)
	}()

	select {
	case err := <-done:
		t.Logf("HandleSSE timeout: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("HandleSSE timed out waiting for idle timeout")
	}
}

// TestCov_SSE_ContextCancellation tests SSE streaming when context is cancelled.
func TestCov_SSE_ContextCancellation(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: first\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Keep sending events slowly
		for i := 0; i < 100; i++ {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprintf(w, "data: event-%d\n\n", i)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer backendServer.Close()

	sh := NewSSEHandler(&SSEConfig{
		EnableSSE:    true,
		IdleTimeout:  60 * time.Second, // long timeout so context cancellation wins
		MaxEventSize: 1024 * 1024,
	})
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)

	w := httptest.NewRecorder()

	done := make(chan error, 1)
	go func() {
		done <- sh.HandleSSE(w, req, b)
	}()

	// Let some events stream through then cancel
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Logf("HandleSSE context cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HandleSSE didn't finish after context cancellation")
	}
}

// TestCov_HandleGRPCWeb_HopByHopSkip tests that hop-by-hop headers are skipped
// in the gRPC-Web response forwarding.
func TestCov_HandleGRPCWeb_HopByHopSkip(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		// Set a hop-by-hop header that should be skipped
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer backendServer.Close()

	gwh := NewGRPCWebHandler(NewGRPCHandler(nil))
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("test")))
	req.Header.Set("Content-Type", "application/grpc-web+proto")

	w := httptest.NewRecorder()
	err := gwh.HandleGRPCWeb(w, req, b)
	t.Logf("HandleGRPCWeb hop-by-hop: %v", err)

	// Connection header should not appear in the response
	if w.Header().Get("Connection") != "" {
		t.Error("Connection header should have been skipped")
	}
	// X-Custom should be present
	if w.Header().Get("X-Custom") != "value" {
		t.Error("X-Custom header should be present")
	}
}

// TestCov_SSE_RegularHTTPResponse tests copyRegularResponse path in HandleSSE.
func TestCov_SSE_RegularHTTPResponse(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>not sse</html>"))
	}))
	defer backendServer.Close()

	sh := NewSSEHandler(nil)
	addr := strings.TrimPrefix(backendServer.URL, "http://")
	b := backend.NewBackend("b1", addr)
	b.SetState(backend.StateUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	err := sh.HandleSSE(w, req, b)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "not sse") {
		t.Errorf("expected 'not sse' in response body, got: %s", body)
	}
}
