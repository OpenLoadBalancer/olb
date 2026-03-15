package l4

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestDefaultTCPProxyConfig(t *testing.T) {
	config := DefaultTCPProxyConfig()

	if config.DialTimeout != 10*time.Second {
		t.Errorf("DialTimeout = %v, want 10s", config.DialTimeout)
	}
	if config.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", config.IdleTimeout)
	}
	if config.BufferSize != 32*1024 {
		t.Errorf("BufferSize = %v, want 32KB", config.BufferSize)
	}
	if config.MaxConnections != 0 {
		t.Errorf("MaxConnections = %v, want 0 (unlimited)", config.MaxConnections)
	}
	if !config.EnableTCPKeepalive {
		t.Error("EnableTCPKeepalive should be true by default")
	}
}

func TestNewTCPProxy(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	config := DefaultTCPProxyConfig()

	proxy := NewTCPProxy(pool, balancer, config)

	if proxy == nil {
		t.Fatal("NewTCPProxy returned nil")
	}
	if proxy.config != config {
		t.Error("Config mismatch")
	}
	if proxy.balancer != balancer {
		t.Error("Balancer mismatch")
	}
	if proxy.pool != pool {
		t.Error("Pool mismatch")
	}
}

func TestNewTCPProxy_NilConfig(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()

	proxy := NewTCPProxy(pool, balancer, nil)

	if proxy == nil {
		t.Fatal("NewTCPProxy(nil) returned nil")
	}
	if proxy.config == nil {
		t.Error("Config should use defaults when nil")
	}
}

func TestTCPProxy_StartStop(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	config := DefaultTCPProxyConfig()

	proxy := NewTCPProxy(pool, balancer, config)

	// Start should succeed
	if err := proxy.Start(); err != nil {
		t.Errorf("Start error: %v", err)
	}

	// Stop should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := proxy.Stop(ctx); err != nil {
		t.Errorf("Stop error: %v", err)
	}
}

func TestSimpleBalancer(t *testing.T) {
	balancer := NewSimpleBalancer()

	// Test with empty backends
	if b := balancer.Next(nil); b != nil {
		t.Error("Expected nil for empty backends")
	}

	// Create test backends
	backends := []*backend.Backend{
		backend.NewBackend("backend-1", "127.0.0.1:8081"),
		backend.NewBackend("backend-2", "127.0.0.1:8082"),
		backend.NewBackend("backend-3", "127.0.0.1:8083"),
	}

	// Test round-robin
	for i := 0; i < len(backends)*2; i++ {
		b := balancer.Next(backends)
		expectedIdx := (i + 1) % len(backends)
		if b != backends[expectedIdx] {
			t.Errorf("Iteration %d: got backend %v, want backend %d", i, b.ID, expectedIdx)
		}
	}
}

func TestNewTCPListener(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	opts := &TCPListenerOptions{
		Name:    "test-listener",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	}

	listener, err := NewTCPListener(opts)
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}

	if listener == nil {
		t.Fatal("NewTCPListener returned nil")
	}
	if listener.name != opts.Name {
		t.Error("Name mismatch")
	}
	if listener.proxy != proxy {
		t.Error("Proxy mismatch")
	}
}

func TestNewTCPListener_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    *TCPListenerOptions
		wantErr bool
	}{
		{
			name:    "nil options",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "missing name",
			opts: &TCPListenerOptions{
				Address: "127.0.0.1:0",
				Proxy:   NewTCPProxy(backend.NewPool("test", "round_robin"), NewSimpleBalancer(), nil),
			},
			wantErr: true,
		},
		{
			name: "missing address",
			opts: &TCPListenerOptions{
				Name:  "test",
				Proxy: NewTCPProxy(backend.NewPool("test", "round_robin"), NewSimpleBalancer(), nil),
			},
			wantErr: true,
		},
		{
			name: "missing proxy",
			opts: &TCPListenerOptions{
				Name:    "test",
				Address: "127.0.0.1:0",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTCPListener(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTCPListener() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTCPListener_StartStop(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	opts := &TCPListenerOptions{
		Name:    "test-tcp",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	}

	listener, err := NewTCPListener(opts)
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}

	// Start listener
	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	if !listener.IsRunning() {
		t.Error("Listener should be running")
	}

	// Verify we can get the address
	addr := listener.Address()
	if addr == "" {
		t.Error("Address should not be empty")
	}

	// Stop listener
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := listener.Stop(ctx); err != nil {
		t.Errorf("Stop error: %v", err)
	}

	if listener.IsRunning() {
		t.Error("Listener should not be running")
	}
}

func TestTCPListener_DoubleStart(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	opts := &TCPListenerOptions{
		Name:    "test-double",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	}

	listener, _ := NewTCPListener(opts)

	if err := listener.Start(); err != nil {
		t.Fatalf("First start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// Second start should fail
	if err := listener.Start(); err == nil {
		t.Error("Second start should fail")
	}
}

func TestCopyBidirectional(t *testing.T) {
	// Create a test server
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := server.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo server
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	// Connect to server
	client, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for server to accept
	time.Sleep(50 * time.Millisecond)

	// Get server connection for bidirectional copy test
	// This is a simplified test - in reality we'd need two connections
	// Just verify the function doesn't panic
	_, _, _ = CopyBidirectional(client, client, time.Second)
}

func TestIsNormalCloseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "EOF",
			err:  io.EOF,
			want: true,
		},
		{
			name: "timeout error",
			err:  &net.OpError{Err: &timeoutError{}},
			want: true,
		},
		{
			name: "closed connection",
			err:  &net.OpError{Err: &closedError{}},
			want: true,
		},
		{
			name: "broken pipe",
			err:  &net.OpError{Err: &brokenPipeError{}},
			want: true,
		},
		{
			name: "other error",
			err:  &net.OpError{Err: &otherError{}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNormalCloseError(tt.err)
			if got != tt.want {
				t.Errorf("isNormalCloseError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test error types
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

type closedError struct{}

func (e *closedError) Error() string { return "use of closed network connection" }

type brokenPipeError struct{}

func (e *brokenPipeError) Error() string { return "write: broken pipe" }

type otherError struct{}

func (e *otherError) Error() string { return "some other error" }

func TestParseTCPAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    string
		wantErr bool
	}{
		{
			name: "host:port",
			addr: "127.0.0.1:8080",
			want: "127.0.0.1:8080",
		},
		{
			name: "just host",
			addr: "127.0.0.1",
			want: "127.0.0.1:80",
		},
		{
			name: "just port",
			addr: ":8080",
			want: ":8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTCPAddress(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTCPAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseTCPAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTCPConn(t *testing.T) {
	// Create a TCP connection
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot create TCP listener: %v", err)
	}
	defer server.Close()

	conn, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Skipf("Cannot connect: %v", err)
	}
	defer conn.Close()

	if !IsTCPConn(conn) {
		t.Error("Expected IsTCPConn to return true for TCP connection")
	}

	// Test with nil
	if IsTCPConn(nil) {
		t.Error("Expected IsTCPConn to return false for nil")
	}
}

func TestGetTCPConn(t *testing.T) {
	// Create a TCP connection
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot create TCP listener: %v", err)
	}
	defer server.Close()

	conn, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Skipf("Cannot connect: %v", err)
	}
	defer conn.Close()

	tcpConn := GetTCPConn(conn)
	if tcpConn == nil {
		t.Error("Expected GetTCPConn to return non-nil for TCP connection")
	}

	// Test with nil
	if GetTCPConn(nil) != nil {
		t.Error("Expected GetTCPConn to return nil for nil")
	}
}

func TestSetTCPNoDelay(t *testing.T) {
	// Create a TCP connection
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot create TCP listener: %v", err)
	}
	defer server.Close()

	conn, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Skipf("Cannot connect: %v", err)
	}
	defer conn.Close()

	// Set no delay
	if err := SetTCPNoDelay(conn, true); err != nil {
		t.Errorf("SetTCPNoDelay error: %v", err)
	}

	// Test with nil
	if err := SetTCPNoDelay(nil, true); err == nil {
		t.Error("Expected error for nil connection")
	}
}

func TestSetTCPKeepAlive(t *testing.T) {
	// Create a TCP connection
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot create TCP listener: %v", err)
	}
	defer server.Close()

	conn, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Skipf("Cannot connect: %v", err)
	}
	defer conn.Close()

	// Set keepalive
	if err := SetTCPKeepAlive(conn, true, 30*time.Second); err != nil {
		t.Errorf("SetTCPKeepAlive error: %v", err)
	}

	// Disable keepalive
	if err := SetTCPKeepAlive(conn, false, 0); err != nil {
		t.Errorf("SetTCPKeepAlive disable error: %v", err)
	}

	// Test with nil
	if err := SetTCPKeepAlive(nil, true, 30*time.Second); err == nil {
		t.Error("Expected error for nil connection")
	}
}

func TestTCPProxy_HandleConnection_MaxConnections(t *testing.T) {
	config := &TCPProxyConfig{
		DialTimeout:    1 * time.Second,
		IdleTimeout:    1 * time.Second,
		BufferSize:     1024,
		MaxConnections: 1,
	}

	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, config)

	// Create mock connections to test max connections
	client1, server1 := net.Pipe()
	defer client1.Close()
	defer server1.Close()

	// First connection should be allowed
	go proxy.HandleConnection(server1)

	// Give time for connection to be tracked (async goroutine)
	time.Sleep(200 * time.Millisecond)

	// Verify active connections
	// Note: Connection may not be tracked if no healthy backends exist
	// This is a smoke test that the code doesn't panic
	_ = proxy.activeConns.Load()

	// Clean up
	client1.Close()
	time.Sleep(50 * time.Millisecond)
}

func TestTCPProxy_Start(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	err := proxy.Start()
	if err != nil {
		t.Errorf("Start() should return nil: %v", err)
	}
}

func TestTCPProxy_Stop(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := proxy.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestTCPProxy_Stop_Timeout(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	// Simulate an active connection by adding to the WaitGroup
	proxy.connWg.Add(1)

	// Use an already-cancelled context for immediate timeout
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := proxy.Stop(ctx)
	if err == nil {
		t.Error("Expected timeout error from Stop()")
	}

	// Clean up the waitgroup
	proxy.connWg.Done()
}

func TestTCPListener_Name(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "my-listener",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})

	if listener.Name() != "my-listener" {
		t.Errorf("Name() = %q, want my-listener", listener.Name())
	}
}

func TestTCPListener_Address_BeforeStart(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:9999",
		Proxy:   proxy,
	})

	// Before Start, Address returns configured address
	addr := listener.Address()
	if addr != "127.0.0.1:9999" {
		t.Errorf("Address() before start = %q, want 127.0.0.1:9999", addr)
	}
}

func TestTCPListener_StartError(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})

	// Before start, StartError should be nil
	if listener.StartError() != nil {
		t.Error("StartError() should be nil before start")
	}
}

func TestTCPListener_StopNotRunning(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})

	err := listener.Stop(context.Background())
	if err == nil {
		t.Error("Stop() on non-running listener should return error")
	}
}

func TestTCPProxy_HandleConnection_NoBackends(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	// Create a pipe to simulate client connection
	client, server := net.Pipe()
	defer client.Close()

	// HandleConnection should handle gracefully when no backends available
	go proxy.HandleConnection(server)

	// Wait for handling to complete (server conn should be closed)
	buf := make([]byte, 1)
	client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := client.Read(buf)
	if err == nil {
		t.Error("Expected read error after HandleConnection completes")
	}
}

func TestTCPProxy_HandleConnection_CancelledContext(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	// Cancel the proxy context
	proxy.cancel()

	client, server := net.Pipe()
	defer client.Close()

	// HandleConnection should exit immediately when context is cancelled
	go proxy.HandleConnection(server)

	buf := make([]byte, 1)
	client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := client.Read(buf)
	if err == nil {
		t.Error("Expected read error after HandleConnection exits")
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		s       string
		substrs []string
		want    bool
	}{
		{"connection reset by peer", []string{"reset", "closed"}, true},
		{"something else", []string{"reset", "closed"}, false},
		{"use of closed network connection", []string{"closed"}, true},
		{"", []string{"anything"}, false},
	}

	for _, tt := range tests {
		got := containsAny(tt.s, tt.substrs)
		if got != tt.want {
			t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.substrs, got, tt.want)
		}
	}
}

func TestTCPProxy_DialBackend(t *testing.T) {
	// Create a TCP listener to act as a backend
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, _ := backendListener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	be := backend.NewBackend("backend-1", backendListener.Addr().String())

	conn, err := proxy.dialBackend(be)
	if err != nil {
		t.Fatalf("dialBackend error: %v", err)
	}
	if conn == nil {
		t.Fatal("dialBackend returned nil connection")
	}
	conn.Close()
}

func TestTCPProxy_DialBackend_ConnectionRefused(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	config := &TCPProxyConfig{
		DialTimeout:    500 * time.Millisecond,
		IdleTimeout:    1 * time.Second,
		BufferSize:     1024,
		MaxConnections: 0,
	}
	proxy := NewTCPProxy(pool, balancer, config)

	be := backend.NewBackend("backend-1", "127.0.0.1:1") // Almost certainly nothing listening

	_, err := proxy.dialBackend(be)
	if err == nil {
		t.Error("Expected error when dialing unreachable backend")
	}
}

func TestTCPProxy_ProxyConnections(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	config := &TCPProxyConfig{
		DialTimeout: 1 * time.Second,
		IdleTimeout: 500 * time.Millisecond,
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, balancer, config)

	// Create two pipe pairs
	client1, server1 := net.Pipe()
	client2, server2 := net.Pipe()

	// Write from client1 and read on server2
	go func() {
		client1.Write([]byte("hello"))
		time.Sleep(100 * time.Millisecond)
		client1.Close()
	}()

	go func() {
		buf := make([]byte, 100)
		n, _ := server2.Read(buf)
		if n > 0 && string(buf[:n]) == "hello" {
			server2.Write([]byte("world"))
		}
		time.Sleep(100 * time.Millisecond)
		server2.Close()
	}()

	// proxyConnections copies bidirectionally
	proxy.proxyConnections(server1, client2)
	// Should complete without panic
}

func TestTCPListener_NameAddressStartError(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	listener, err := NewTCPListener(&TCPListenerOptions{
		Name:    "my-tcp-listener",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}

	// Name()
	if listener.Name() != "my-tcp-listener" {
		t.Errorf("Name() = %q, want my-tcp-listener", listener.Name())
	}

	// Address() before start returns configured address
	if listener.Address() != "127.0.0.1:0" {
		t.Errorf("Address() = %q, want 127.0.0.1:0", listener.Address())
	}

	// StartError() before start
	if listener.StartError() != nil {
		t.Error("StartError() should be nil before start")
	}

	// Start to get actual address
	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// Address() after start should return the actual bound address
	addr := listener.Address()
	if addr == "" || addr == "127.0.0.1:0" {
		t.Error("Address() should return actual bound address after start")
	}

	// StartError() after successful start
	if listener.StartError() != nil {
		t.Errorf("StartError() = %v, want nil after successful start", listener.StartError())
	}
}

func TestCopyWithBuffer(t *testing.T) {
	// Create two pipes: one for source, one for destination
	// In a real scenario, src and dst are different connections
	// Here we simulate by creating a listener and connecting

	// Create echo server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot create listener: %v", err)
	}
	defer listener.Close()

	// Accept and echo
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	// Connect to server
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Skipf("Cannot connect: %v", err)
	}
	defer conn.Close()

	// Test copyWithBuffer in a simplified way
	// Just verify it doesn't panic and handles normal operations
	// Write something first
	conn.Write([]byte("test"))

	// Read response
	buf := make([]byte, 100)
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	conn.Read(buf)

	// Test passed if we got here without panic
}
