package health

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusUnknown, "unknown"},
		{StatusHealthy, "healthy"},
		{StatusUnhealthy, "unhealthy"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultCheck(t *testing.T) {
	check := DefaultCheck()

	if check.Type != "tcp" {
		t.Errorf("DefaultCheck().Type = %v, want %v", check.Type, "tcp")
	}
	if check.Interval != 10*time.Second {
		t.Errorf("DefaultCheck().Interval = %v, want %v", check.Interval, 10*time.Second)
	}
	if check.Timeout != 5*time.Second {
		t.Errorf("DefaultCheck().Timeout = %v, want %v", check.Timeout, 5*time.Second)
	}
	if check.Path != "/health" {
		t.Errorf("DefaultCheck().Path = %v, want %v", check.Path, "/health")
	}
	if check.Method != "GET" {
		t.Errorf("DefaultCheck().Method = %v, want %v", check.Method, "GET")
	}
	if check.ExpectedStatus != 200 {
		t.Errorf("DefaultCheck().ExpectedStatus = %v, want %v", check.ExpectedStatus, 200)
	}
	if check.HealthyThreshold != 2 {
		t.Errorf("DefaultCheck().HealthyThreshold = %v, want %v", check.HealthyThreshold, 2)
	}
	if check.UnhealthyThreshold != 3 {
		t.Errorf("DefaultCheck().UnhealthyThreshold = %v, want %v", check.UnhealthyThreshold, 3)
	}
}

func TestChecker_Register(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("test", "127.0.0.1:18080")
	check := &Check{
		Type:     "tcp",
		Interval: 100 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}

	err := checker.Register(b, check)
	if err != nil {
		t.Errorf("Register() error = %v", err)
	}

	// Registering duplicate should fail
	err = checker.Register(b, check)
	if err == nil {
		t.Error("Register() duplicate should return error")
	}

	// Registering nil backend should fail
	err = checker.Register(nil, check)
	if err == nil {
		t.Error("Register() nil backend should return error")
	}
}

func TestChecker_Unregister(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("test", "127.0.0.1:18081")
	check := &Check{
		Type:     "tcp",
		Interval: 100 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}

	checker.Register(b, check)
	checker.Unregister("test")

	// Should be able to register again after unregister
	err := checker.Register(b, check)
	if err != nil {
		t.Errorf("Register() after unregister error = %v", err)
	}
}

func TestChecker_GetStatus(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("test", "127.0.0.1:18082")
	check := &Check{
		Type:     "tcp",
		Interval: 100 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}

	// Status should be unknown before registration
	if status := checker.GetStatus("test"); status != StatusUnknown {
		t.Errorf("GetStatus() before register = %v, want %v", status, StatusUnknown)
	}

	checker.Register(b, check)

	// Status should be unknown initially (no check performed yet in this test)
	// In real scenario, the first check happens immediately
}

func TestChecker_ListStatuses(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	b1 := backend.NewBackend("test1", "127.0.0.1:18083")
	b2 := backend.NewBackend("test2", "127.0.0.1:18084")

	checker.Register(b1, DefaultCheck())
	checker.Register(b2, DefaultCheck())

	statuses := checker.ListStatuses()
	if len(statuses) != 2 {
		t.Errorf("ListStatuses() length = %v, want %v", len(statuses), 2)
	}
}

func TestChecker_checkTCP(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	// Test successful TCP check
	b := backend.NewBackend("test", listener.Addr().String())
	check := &Check{
		Timeout: 1 * time.Second,
	}

	result := checker.checkTCP(b, check)
	if !result.Healthy {
		t.Errorf("checkTCP() on available port = %v, want healthy", result.Healthy)
	}
	if result.Error != nil {
		t.Errorf("checkTCP() error = %v", result.Error)
	}

	// Test failed TCP check - use a port that's not listening
	failListener, _ := net.Listen("tcp", "127.0.0.1:0")
	failAddr := failListener.Addr().String()
	failListener.Close()
	b2 := backend.NewBackend("test2", failAddr)
	result2 := checker.checkTCP(b2, check)
	if result2.Healthy {
		t.Error("checkTCP() on unavailable port should be unhealthy")
	}
	if result2.Error == nil {
		t.Error("checkTCP() on unavailable port should return error")
	}
}

func TestChecker_checkHTTP(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a test HTTP server on 127.0.0.2 to avoid SSRF protection
	// (127.0.0.1 is blocked by isInternalAddress).
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Error"))
	})

	server := &http.Server{
		Addr:    "127.0.0.2:0",
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}

	go server.Serve(listener)
	defer server.Close()

	addr := listener.Addr().String()
	time.Sleep(10 * time.Millisecond) // Wait for server to start

	// Test successful HTTP check
	b := backend.NewBackend("test", addr)
	check := &Check{
		Type:           "http",
		Path:           "/health",
		Method:         "GET",
		ExpectedStatus: 200,
		Timeout:        1 * time.Second,
	}

	result := checker.checkHTTP(b, check)
	if !result.Healthy {
		t.Errorf("checkHTTP() on healthy endpoint = %v, want healthy", result.Healthy)
	}
	if result.Error != nil {
		t.Errorf("checkHTTP() error = %v", result.Error)
	}

	// Test failed HTTP check (wrong status)
	check2 := &Check{
		Type:           "http",
		Path:           "/error",
		Method:         "GET",
		ExpectedStatus: 200,
		Timeout:        1 * time.Second,
	}

	result2 := checker.checkHTTP(b, check2)
	if result2.Healthy {
		t.Error("checkHTTP() on error endpoint should be unhealthy")
	}

	// Test with any 2xx status
	check3 := &Check{
		Type:           "http",
		Path:           "/health",
		Method:         "GET",
		ExpectedStatus: 0, // Any 2xx
		Timeout:        1 * time.Second,
	}

	result3 := checker.checkHTTP(b, check3)
	if !result3.Healthy {
		t.Error("checkHTTP() with ExpectedStatus=0 should accept 200")
	}
}

func TestChecker_checkHTTP_WithHeaders(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	var receivedHeader string

	// Start a test HTTP server on 127.0.0.2 to avoid SSRF protection
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:    "127.0.0.2:0",
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}

	go server.Serve(listener)
	defer server.Close()

	addr := listener.Addr().String()
	time.Sleep(10 * time.Millisecond)

	b := backend.NewBackend("test", addr)
	check := &Check{
		Type:           "http",
		Path:           "/health",
		Method:         "GET",
		ExpectedStatus: 200,
		Timeout:        1 * time.Second,
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
	}

	checker.checkHTTP(b, check)

	if receivedHeader != "test-value" {
		t.Errorf("Header not received correctly: got %v, want %v", receivedHeader, "test-value")
	}
}

func TestChecker_StateTransitions(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	b := backend.NewBackend("test", listener.Addr().String())
	b.SetState(backend.StateStarting)

	check := &Check{
		Type:               "tcp",
		Interval:           50 * time.Millisecond,
		Timeout:            100 * time.Millisecond,
		HealthyThreshold:   2,
		UnhealthyThreshold: 2,
	}

	checker.Register(b, check)

	// Wait for health checks to run and transition to healthy
	time.Sleep(200 * time.Millisecond)

	status := checker.GetStatus("test")
	if status != StatusHealthy {
		t.Errorf("Status after successful checks = %v, want %v", status, StatusHealthy)
	}

	if b.State() != backend.StateUp {
		t.Errorf("Backend state = %v, want %v", b.State(), backend.StateUp)
	}
}

func TestChecker_CountHealthyUnhealthy(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Register some backends
	checker.Register(backend.NewBackend("b1", "127.0.0.1:18085"), DefaultCheck())
	checker.Register(backend.NewBackend("b2", "127.0.0.1:18086"), DefaultCheck())
	checker.Register(backend.NewBackend("b3", "127.0.0.1:18087"), DefaultCheck())

	// Initially all unknown
	if healthy := checker.CountHealthy(); healthy != 0 {
		t.Errorf("CountHealthy() initial = %v, want 0", healthy)
	}
	if unhealthy := checker.CountUnhealthy(); unhealthy != 0 {
		t.Errorf("CountUnhealthy() initial = %v, want 0", unhealthy)
	}
}

func TestChecker_GetResult(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Non-existent backend
	if result := checker.GetResult("nonexistent"); result != nil {
		t.Error("GetResult() for non-existent backend should return nil")
	}

	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	b := backend.NewBackend("test", listener.Addr().String())
	check := &Check{
		Type:     "tcp",
		Interval: 50 * time.Millisecond,
		Timeout:  100 * time.Millisecond,
	}

	checker.Register(b, check)

	// Wait for a check to complete
	time.Sleep(100 * time.Millisecond)

	result := checker.GetResult("test")
	if result == nil {
		t.Fatal("GetResult() should return a result after check")
	}

	if result.Timestamp.IsZero() {
		t.Error("Result.Timestamp should be set")
	}
}

func BenchmarkChecker_checkTCP(b *testing.B) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Failed to create test server: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	be := backend.NewBackend("test", listener.Addr().String())
	check := &Check{
		Timeout: 1 * time.Second,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.checkTCP(be, check)
	}
}

func BenchmarkChecker_checkHTTP(b *testing.B) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a test HTTP server on 127.0.0.2 to avoid SSRF protection
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:    "127.0.0.2:0",
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		b.Fatalf("Failed to create test server: %v", err)
	}

	go server.Serve(listener)
	defer server.Close()

	addr := listener.Addr().String()
	time.Sleep(10 * time.Millisecond)

	be := backend.NewBackend("test", addr)
	check := &Check{
		Type:           "http",
		Path:           "/health",
		Method:         "GET",
		ExpectedStatus: 200,
		Timeout:        1 * time.Second,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.checkHTTP(be, check)
	}
}

// mockBalancer is a simple mock balancer for integration tests
type mockBalancer struct {
	name string
}

func (m *mockBalancer) Name() string {
	return m.name
}

func (m *mockBalancer) Next(_ *backend.RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) > 0 {
		return backends[0]
	}
	return nil
}

func (m *mockBalancer) Add(b *backend.Backend) {}

func (m *mockBalancer) Remove(id string) {}

func (m *mockBalancer) Update(b *backend.Backend) {}

// --- New tests to improve coverage ---

func TestNewChecker_CheckRedirect(t *testing.T) {
	// Verify that the shared httpClient follows no redirects by exercising
	// its CheckRedirect function directly.
	checker := NewChecker()
	defer checker.Stop()

	req := httptest.NewRequest("GET", "/redirect", nil)
	err := checker.httpClient.CheckRedirect(req, []*http.Request{req})
	if err != http.ErrUseLastResponse {
		t.Errorf("CheckRedirect = %v, want http.ErrUseLastResponse", err)
	}
}

func TestChecker_Register_NilConfig(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("nil-cfg", "127.0.0.1:19999")
	// Pass nil config — Register should use DefaultCheck.
	err := checker.Register(b, nil)
	if err != nil {
		t.Fatalf("Register with nil config: %v", err)
	}

	// Verify a check state was created with default check type.
	state, ok := checker.checks["nil-cfg"]
	if !ok {
		t.Fatal("expected check state for nil-cfg")
	}
	if state.config.Type != "tcp" {
		t.Errorf("default config type = %q, want %q", state.config.Type, "tcp")
	}
}

func TestChecker_Unregister_NonExistent(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Unregistering a backend that was never registered should not panic.
	checker.Unregister("nonexistent")

	// Verify nothing was added.
	if len(checker.checks) != 0 {
		t.Errorf("checks map should be empty, got %d entries", len(checker.checks))
	}
}

func TestChecker_Unregister_StopsGoroutine(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a TCP server.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	b := backend.NewBackend("stop-test", listener.Addr().String())
	check := &Check{
		Type:               "tcp",
		Interval:           50 * time.Millisecond,
		Timeout:            100 * time.Millisecond,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
	}

	if err := checker.Register(b, check); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Wait for the health check goroutine to start running.
	time.Sleep(150 * time.Millisecond)

	// Unregister should close the per-backend stopCh and the goroutine should exit.
	checker.Unregister("stop-test")

	// Verify the backend is removed.
	if _, exists := checker.checks["stop-test"]; exists {
		t.Error("backend should be removed after unregister")
	}
}

func TestChecker_checkHTTP_HTTPS(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Start a plain TCP listener on 127.0.0.2 (no TLS). The HTTPS check will
	// fail to complete a TLS handshake, but we still exercise the url-building
	// branch that produces an "https://" scheme.
	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	b := backend.NewBackend("https-test", listener.Addr().String())
	check := &Check{
		Type:           "https",
		Path:           "/health",
		Method:         "GET",
		ExpectedStatus: 200,
		Timeout:        500 * time.Millisecond,
	}

	result := checker.checkHTTP(b, check)
	// The connection will fail because we don't have TLS, but the important
	// thing is that the https URL branch is exercised.
	if result.Healthy {
		t.Error("HTTPS check against plain TCP should not be healthy")
	}
}

func TestChecker_checkHTTP_Non2xxStatus(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	server := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	go server.Serve(listener)
	defer server.Close()

	time.Sleep(10 * time.Millisecond)

	b := backend.NewBackend("non2xx", listener.Addr().String())
	// ExpectedStatus == 0 means any 2xx is acceptable; a 404 should fail.
	check := &Check{
		Type:           "http",
		Path:           "/bad",
		Method:         "GET",
		ExpectedStatus: 0,
		Timeout:        1 * time.Second,
	}

	result := checker.checkHTTP(b, check)
	if result.Healthy {
		t.Error("checkHTTP with 404 and ExpectedStatus=0 should be unhealthy")
	}
	if result.Error == nil {
		t.Error("expected non-nil error for non-2xx response")
	}
}

func TestChecker_checkGRPC_PlainFallback(t *testing.T) {
	// Start a plain HTTP server on a random port.
	// The checkGRPC function will first try HTTPS (fail), then
	// fall back to plain HTTP. It will get an HTTP response with
	// Grpc-Status header set to "0", indicating healthy.
	mux := http.NewServeMux()
	mux.HandleFunc("/grpc.health.v1.Health/Check", func(w http.ResponseWriter, r *http.Request) {
		// Verify gRPC headers are present.
		if ct := r.Header.Get("Content-Type"); ct != "application/grpc" {
			t.Errorf("expected Content-Type application/grpc, got %q", ct)
		}

		// Read and discard the gRPC payload.
		buf := make([]byte, 5)
		r.Body.Read(buf)

		// Set Grpc-Status as a regular response header (the checker
		// falls back to resp.Header.Get when trailers are empty).
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0, 0, 0, 0, 0})
	})

	server := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	go server.Serve(listener)
	defer server.Close()

	time.Sleep(20 * time.Millisecond)

	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("grpc-plain", listener.Addr().String())
	check := &Check{
		Type:    "grpc",
		Timeout: 2 * time.Second,
	}

	result := checker.checkGRPC(b, check)
	if !result.Healthy {
		t.Errorf("checkGRPC (plain fallback) should be healthy, got error: %v", result.Error)
	}
}

func TestChecker_checkGRPC_ConnectionRefused(t *testing.T) {
	// Use a port that is not listening.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("grpc-fail", addr)
	check := &Check{
		Type:    "grpc",
		Timeout: 500 * time.Millisecond,
	}

	result := checker.checkGRPC(b, check)
	if result.Healthy {
		t.Error("checkGRPC on closed port should be unhealthy")
	}
	if result.Error == nil {
		t.Error("expected non-nil error for connection refused")
	}
}

func TestChecker_checkGRPC_BadGRPCStatus(t *testing.T) {
	// Start an HTTP server that returns HTTP 200 but with gRPC status != 0.
	mux := http.NewServeMux()
	mux.HandleFunc("/grpc.health.v1.Health/Check", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Grpc-Status", "14") // UNAVAILABLE
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0, 0, 0, 0, 0})
	})

	server := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	go server.Serve(listener)
	defer server.Close()

	time.Sleep(20 * time.Millisecond)

	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("grpc-bad-status", listener.Addr().String())
	check := &Check{
		Type:    "grpc",
		Timeout: 2 * time.Second,
	}

	result := checker.checkGRPC(b, check)
	if result.Healthy {
		t.Error("checkGRPC with non-zero gRPC status should be unhealthy")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "grpc health check returned status") {
		t.Errorf("expected gRPC status error, got: %v", result.Error)
	}
}

func TestChecker_checkGRPC_Non200HTTPStatus(t *testing.T) {
	// Start an HTTP server that returns 503 without gRPC status.
	mux := http.NewServeMux()
	mux.HandleFunc("/grpc.health.v1.Health/Check", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	server := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	go server.Serve(listener)
	defer server.Close()

	time.Sleep(20 * time.Millisecond)

	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("grpc-503", listener.Addr().String())
	check := &Check{
		Type:    "grpc",
		Timeout: 2 * time.Second,
	}

	result := checker.checkGRPC(b, check)
	if result.Healthy {
		t.Error("checkGRPC with HTTP 503 should be unhealthy")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "grpc health check HTTP status") {
		t.Errorf("expected HTTP status error, got: %v", result.Error)
	}
}

func TestChecker_UnknownCheckType(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	b := backend.NewBackend("unknown-type", listener.Addr().String())
	check := &Check{
		Type:               "unknown",
		Interval:           50 * time.Millisecond,
		Timeout:            100 * time.Millisecond,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
	}

	if err := checker.Register(b, check); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Wait for the check loop to run and produce an unhealthy result.
	time.Sleep(200 * time.Millisecond)

	status := checker.GetStatus("unknown-type")
	if status != StatusUnhealthy {
		t.Errorf("unknown check type should result in unhealthy, got %v", status)
	}

	result := checker.GetResult("unknown-type")
	if result == nil {
		t.Fatal("expected result for unknown-type backend")
	}
	if result.Healthy {
		t.Error("unknown check type result should not be healthy")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "unknown health check type") {
		t.Errorf("expected 'unknown health check type' error, got: %v", result.Error)
	}
}

func TestChecker_GRPCViaPerformCheck(t *testing.T) {
	// Exercise the "grpc" branch in performCheck by registering a backend
	// with type "grpc" against a non-listening address.
	checker := NewChecker()
	defer checker.Stop()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	b := backend.NewBackend("grpc-perform", addr)
	check := &Check{
		Type:               "grpc",
		Interval:           50 * time.Millisecond,
		Timeout:            200 * time.Millisecond,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
	}

	if err := checker.Register(b, check); err != nil {
		t.Fatalf("Register: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	status := checker.GetStatus("grpc-perform")
	if status != StatusUnhealthy {
		t.Errorf("gRPC check against closed port should be unhealthy, got %v", status)
	}
}

func TestChecker_CountHealthyUnhealthy_AfterChecks(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	// Start two TCP listeners: one healthy, one will be closed to become unhealthy.
	healthyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create healthy listener: %v", err)
	}
	defer healthyLn.Close()

	unhealthyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create unhealthy listener: %v", err)
	}

	go func() {
		for {
			conn, err := healthyLn.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	go func() {
		for {
			conn, err := unhealthyLn.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	check := &Check{
		Type:               "tcp",
		Interval:           50 * time.Millisecond,
		Timeout:            100 * time.Millisecond,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
	}

	b1 := backend.NewBackend("healthy1", healthyLn.Addr().String())
	b2 := backend.NewBackend("healthy2", healthyLn.Addr().String())
	b3 := backend.NewBackend("unhealthy1", unhealthyLn.Addr().String())

	if err := checker.Register(b1, check); err != nil {
		t.Fatalf("Register b1: %v", err)
	}
	if err := checker.Register(b2, check); err != nil {
		t.Fatalf("Register b2: %v", err)
	}
	if err := checker.Register(b3, check); err != nil {
		t.Fatalf("Register b3: %v", err)
	}

	// Wait for checks to run.
	time.Sleep(200 * time.Millisecond)

	// All three should be healthy now.
	healthyCount := checker.CountHealthy()
	if healthyCount != 3 {
		t.Errorf("CountHealthy = %d, want 3", healthyCount)
	}

	// Close one listener to make b3 unhealthy.
	unhealthyLn.Close()
	// Wait for b3 to transition to unhealthy (needs UnhealthyThreshold=1 failures).
	time.Sleep(300 * time.Millisecond)

	healthyCount = checker.CountHealthy()
	if healthyCount != 2 {
		t.Errorf("CountHealthy after closure = %d, want 2", healthyCount)
	}
	unhealthyCount := checker.CountUnhealthy()
	if unhealthyCount != 1 {
		t.Errorf("CountUnhealthy after closure = %d, want 1", unhealthyCount)
	}
}

func TestChecker_checkGRPC_EmptyPayload(t *testing.T) {
	// Verify the function correctly constructs the gRPC request payload.
	// We start a server that echoes back the content-type and checks the payload.
	mux := http.NewServeMux()
	mux.HandleFunc("/grpc.health.v1.Health/Check", func(w http.ResponseWriter, r *http.Request) {
		// Read the body to verify payload shape.
		body := make([]byte, 5)
		n, _ := r.Body.Read(body)
		body = body[:n]

		expected := []byte{0, 0, 0, 0, 0}
		if !bytes.Equal(body, expected) {
			t.Errorf("gRPC payload = %v, want %v", body, expected)
		}

		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	go server.Serve(listener)
	defer server.Close()

	time.Sleep(20 * time.Millisecond)

	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("grpc-payload", listener.Addr().String())
	check := &Check{
		Type:    "grpc",
		Timeout: 2 * time.Second,
	}

	result := checker.checkGRPC(b, check)
	if !result.Healthy {
		t.Errorf("checkGRPC with status 0 should be healthy, got error: %v", result.Error)
	}
}

func TestChecker_checkHTTP_RequestCreationError(t *testing.T) {
	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("bad-url", "10.0.0.1:0")
	// Use a path with a control character that will cause http.NewRequest to fail.
	check := &Check{
		Type:    "http",
		Path:    string([]byte{0x00}), // null byte invalid in URL
		Method:  "GET",
		Timeout: 1 * time.Second,
	}

	result := checker.checkHTTP(b, check)
	if result.Healthy {
		t.Error("checkHTTP with invalid URL should not be healthy")
	}
	if result.Error == nil {
		t.Error("expected error for invalid URL")
	}
}

func ExampleChecker_Register() {
	checker := NewChecker()
	defer checker.Stop()

	b := backend.NewBackend("web1", "10.0.0.1:8080")
	check := &Check{
		Type:     "http",
		Path:     "/health",
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
	}

	err := checker.Register(b, check)
	if err != nil {
		fmt.Printf("Failed to register: %v\n", err)
		return
	}

	fmt.Println("Backend registered for health checks")
	// Output: Backend registered for health checks
}
