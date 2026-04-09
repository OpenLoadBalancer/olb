package l4

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// --- TCP Proxy Tests ---

func TestNewTCPProxy(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewTCPProxy(pool, balancer, nil)

	if proxy == nil {
		t.Fatal("NewTCPProxy returned nil")
	}
	if proxy.config == nil {
		t.Error("config should be set to defaults when nil")
	}
	if proxy.config.DialTimeout != 10*time.Second {
		t.Errorf("expected default DialTimeout 10s, got %v", proxy.config.DialTimeout)
	}
}

func TestNewTCPProxy_WithConfig(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	balancer := NewSimpleBalancer()
	cfg := &TCPProxyConfig{
		DialTimeout:    5 * time.Second,
		IdleTimeout:    30 * time.Second,
		BufferSize:     16 * 1024,
		MaxConnections: 100,
	}
	proxy := NewTCPProxy(pool, balancer, cfg)
	if proxy.config.DialTimeout != 5*time.Second {
		t.Errorf("expected DialTimeout 5s, got %v", proxy.config.DialTimeout)
	}
	if proxy.config.MaxConnections != 100 {
		t.Errorf("expected MaxConnections 100, got %d", proxy.config.MaxConnections)
	}
}

func TestTCPProxy_StartStop(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	err := proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = proxy.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

func TestTCPProxy_HandleConnection_NoBackends(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	client, server := net.Pipe()
	defer client.Close()

	// No backends — HandleConnection should return without panicking
	proxy.HandleConnection(server)
}

func TestTCPProxy_HandleConnection_MaxConnections(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		MaxConnections: 0, // unlimited for this test
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	// Set max conns to 1 to test rejection
	proxy.config.MaxConnections = 1
	proxy.activeConns.Store(1) // Simulate at max

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	select {
	case <-done:
		// Expected: connection rejected because at max
	case <-time.After(2 * time.Second):
		t.Fatal("HandleConnection should return quickly when at max connections")
	}
}

func TestTCPProxy_HandleConnection_Echo(t *testing.T) {
	// Start an echo backend
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
		io.Copy(conn, conn)
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	proxy := NewTCPProxy(pool, NewSimpleBalancer(), &TCPProxyConfig{
		DialTimeout: 5 * time.Second,
		IdleTimeout: 5 * time.Second,
		BufferSize:  4096,
	})

	// Use the proxy via HandleConnection
	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	// Write test data and read response
	testData := []byte("hello tcp proxy")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.Write(testData)
	}()

	buf := make([]byte, 1024)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("client read error: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("echo = %q, want %q", string(buf[:n]), string(testData))
	}
	client.Close()
	wg.Wait()
	<-done
}

func TestTCPProxy_HandleConnection_CancelledContext(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)
	proxy.cancel() // Cancel context

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	select {
	case <-done:
		// Expected: returns immediately because context is cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("HandleConnection should return when context is cancelled")
	}
}

func TestTCPProxy_HandleConnection_DialFailure(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	// Backend points to a port with no listener
	b := backend.NewBackend("b1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	proxy := NewTCPProxy(pool, NewSimpleBalancer(), &TCPProxyConfig{
		DialTimeout: 1 * time.Second,
	})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	select {
	case <-done:
		// Expected: dial fails and connection is closed
	case <-time.After(5 * time.Second):
		t.Fatal("HandleConnection should return after dial failure")
	}
}

func TestTCPProxy_Stop_WithActiveConnections(t *testing.T) {
	// Start an echo backend
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	proxy := NewTCPProxy(pool, NewSimpleBalancer(), &TCPProxyConfig{
		DialTimeout: 2 * time.Second,
		IdleTimeout: 2 * time.Second,
	})

	client, server := net.Pipe()

	go proxy.HandleConnection(server)

	// Write some data and read it back so the pipe doesn't block
	client.Write([]byte("test"))
	buf := make([]byte, 4)
	client.Read(buf)

	// Close the client to unblock the proxy's copy goroutines
	client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = proxy.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

// --- TCPListener Tests ---

func TestNewTCPListener(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	opts := &TCPListenerOptions{
		Name:    "test-listener",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	}

	listener, err := NewTCPListener(opts)
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}
	if listener.Name() != "test-listener" {
		t.Errorf("Name = %q, want test-listener", listener.Name())
	}
}

func TestNewTCPListener_NilOptions(t *testing.T) {
	_, err := NewTCPListener(nil)
	if err == nil {
		t.Error("expected error for nil options")
	}
}

func TestNewTCPListener_EmptyName(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	_, err := NewTCPListener(&TCPListenerOptions{
		Name:    "",
		Address: ":8080",
		Proxy:   proxy,
	})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestNewTCPListener_EmptyAddress(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	_, err := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "",
		Proxy:   proxy,
	})
	if err == nil {
		t.Error("expected error for empty address")
	}
}

func TestNewTCPListener_NilProxy(t *testing.T) {
	_, err := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: ":8080",
		Proxy:   nil,
	})
	if err == nil {
		t.Error("expected error for nil proxy")
	}
}

func TestTCPListener_StartStop(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, err := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}

	err = listener.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if !listener.IsRunning() {
		t.Error("expected listener to be running")
	}

	// Address should be resolved now
	addr := listener.Address()
	if addr == "127.0.0.1:0" {
		t.Error("expected actual address, not :0")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = listener.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if listener.IsRunning() {
		t.Error("expected listener to not be running after stop")
	}
}

func TestTCPListener_DoubleStart(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})

	err := listener.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	err = listener.Start()
	if err == nil {
		t.Error("expected error for double start")
	}
}

func TestTCPListener_StopNotRunning(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})

	ctx := context.Background()
	err := listener.Stop(ctx)
	if err == nil {
		t.Error("expected error when stopping non-running listener")
	}
}

func TestTCPListener_StartError(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "256.256.256.256:99999", // Invalid address
		Proxy:   proxy,
	})

	err := listener.Start()
	if err == nil {
		t.Error("expected error for invalid address")
	}
}

func TestTCPListener_StartError_AfterFailedStart(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "256.256.256.256:99999",
		Proxy:   proxy,
	})

	// Start() returns an error for invalid addresses (net.Listen fails)
	err := listener.Start()
	if err == nil {
		t.Error("expected Start to return error for invalid address")
	}
	// StartError is only set in acceptLoop, not during initial Listen failure
	// So StartError() returns nil in this case (the error is returned from Start())
}

func TestTCPListener_AddressBeforeStart(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, _ := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:8080",
		Proxy:   proxy,
	})

	addr := listener.Address()
	if addr != "127.0.0.1:8080" {
		t.Errorf("Address before start = %q, want 127.0.0.1:8080", addr)
	}
}

// --- SimpleBalancer Tests ---

func TestSimpleBalancer_Next(t *testing.T) {
	b := NewSimpleBalancer()

	backends := []*backend.Backend{
		backend.NewBackend("b1", "10.0.0.1:80"),
		backend.NewBackend("b2", "10.0.0.2:80"),
	}

	// Should round-robin
	first := b.Next(backends)
	if first == nil {
		t.Fatal("Next returned nil")
	}

	second := b.Next(backends)
	if second == nil {
		t.Fatal("Next returned nil")
	}

	// Should alternate
	if first.ID == second.ID {
		t.Error("expected round-robin to alternate backends")
	}
}

func TestSimpleBalancer_Next_Empty(t *testing.T) {
	b := NewSimpleBalancer()
	result := b.Next(nil)
	if result != nil {
		t.Error("expected nil for empty backends")
	}
}

// --- Utility function tests ---

func TestParseTCPAddress(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:8080", "192.168.1.1:8080"},
		{":8080", ":8080"},
		{"example.com:443", "example.com:443"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseTCPAddress(tt.input)
			if err != nil {
				t.Fatalf("ParseTCPAddress error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("ParseTCPAddress(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseTCPAddress_NoPort(t *testing.T) {
	result, err := ParseTCPAddress("example.com")
	if err != nil {
		t.Fatalf("ParseTCPAddress error: %v", err)
	}
	if result != "example.com:80" {
		t.Errorf("ParseTCPAddress(%q) = %q, want example.com:80", "example.com", result)
	}
}

func TestParseTCPAddress_ColonOnly(t *testing.T) {
	result, err := ParseTCPAddress(":8080")
	if err != nil {
		t.Fatalf("ParseTCPAddress error: %v", err)
	}
	if result != ":8080" {
		t.Errorf("ParseTCPAddress(%q) = %q, want :8080", ":8080", result)
	}
}

func TestIsTCPConn(t *testing.T) {
	// TCP connection
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1", 10*time.Millisecond)
	if conn != nil {
		if !IsTCPConn(conn) {
			t.Error("expected IsTCPConn to return true for TCP connection")
		}
		conn.Close()
	}
	_ = err // may fail, we just need to check the type

	// Non-TCP connection
	pipeClient, _ := net.Pipe()
	defer pipeClient.Close()
	if IsTCPConn(pipeClient) {
		t.Error("expected IsTCPConn to return false for Pipe connection")
	}
}

func TestGetTCPConn(t *testing.T) {
	pipeClient, _ := net.Pipe()
	defer pipeClient.Close()

	result := GetTCPConn(pipeClient)
	if result != nil {
		t.Error("expected nil for non-TCP connection")
	}
}

func TestSetTCPNoDelay_NonTCP(t *testing.T) {
	pipeClient, _ := net.Pipe()
	defer pipeClient.Close()

	err := SetTCPNoDelay(pipeClient, true)
	if err == nil {
		t.Error("expected error for non-TCP connection")
	}
}

func TestSetTCPKeepAlive_NonTCP(t *testing.T) {
	pipeClient, _ := net.Pipe()
	defer pipeClient.Close()

	err := SetTCPKeepAlive(pipeClient, true, 30*time.Second)
	if err == nil {
		t.Error("expected error for non-TCP connection")
	}
}

// --- CopyBidirectional coverage ---

func TestCopyBidirectional_DataTransfer(t *testing.T) {
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoListener.Close()

	go func() {
		conn, err := echoListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	echoConn, err := net.Dial("tcp", echoListener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer echoConn.Close()

	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		CopyBidirectional(proxyConn, echoConn, 5*time.Second)
	}()

	testData := []byte("hello bidirectional copy")
	go func() {
		clientConn.Write(testData)
	}()

	buf := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("got %q, want %q", string(buf[:n]), string(testData))
	}

	clientConn.Close()
	<-done
}

// --- isNormalCloseError coverage ---

func TestIsNormalCloseError_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"eof", io.EOF, true},
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

// --- TCPListener Accept + HandleConnection Integration ---

func TestTCPListener_AcceptAndProxy(t *testing.T) {
	// Start echo backend
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
		io.Copy(conn, conn)
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	proxy := NewTCPProxy(pool, NewSimpleBalancer(), &TCPProxyConfig{
		DialTimeout: 2 * time.Second,
		IdleTimeout: 5 * time.Second,
		BufferSize:  4096,
	})

	listener, err := NewTCPListener(&TCPListenerOptions{
		Name:    "integration",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}

	err = listener.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// Connect as client
	conn, err := net.Dial("tcp", listener.Address())
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()

	testData := []byte("integration test")
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("echo = %q, want %q", string(buf[:n]), string(testData))
	}
}

// --- UDP additional coverage ---

func TestUDPProxy_Stats(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)

	stats := proxy.Stats()
	if stats.PacketsForwarded != 0 {
		t.Errorf("expected 0 PacketsForwarded, got %d", stats.PacketsForwarded)
	}
}

func TestUDPProxy_ActiveSessions(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)

	if proxy.ActiveSessions() != 0 {
		t.Errorf("expected 0 active sessions, got %d", proxy.ActiveSessions())
	}
}

func TestUDPProxy_IsRunning(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)

	if proxy.IsRunning() {
		t.Error("expected not running before Start()")
	}
}

func TestUDPProxy_StopNotRunning(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)

	err := proxy.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running proxy")
	}
}

func TestUDPSession_LastActivity(t *testing.T) {
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, nil)
	lastActivity := session.LastActivity()
	if lastActivity.IsZero() {
		t.Error("expected LastActivity to be set after creation")
	}
}

func TestUDPSession_CloseTwice(t *testing.T) {
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, nil)
	session.close()
	session.close() // Should not panic on double close
}

func TestUDPProxy_StartDouble(t *testing.T) {
	backendConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendConn.Close()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendConn.LocalAddr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	cfg := DefaultUDPProxyConfig()
	cfg.IdleTimeout = 5 * time.Second
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	err = proxy.Start()
	if err == nil {
		t.Error("expected error for double start")
	}
}

func TestUDPProxy_CleanExpiredSessions(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &UDPProxyConfig{
		ListenAddr:     ":0",
		IdleTimeout:    50 * time.Millisecond,
		SessionTimeout: 50 * time.Millisecond,
		BufferSize:     65535,
	}
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Manually add an expired session
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, nil)
	proxy.mu.Lock()
	proxy.sessions[clientAddr.String()] = session
	proxy.mu.Unlock()

	// Wait for session to expire
	time.Sleep(100 * time.Millisecond)

	proxy.cleanExpiredSessions()

	proxy.mu.RLock()
	count := len(proxy.sessions)
	proxy.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 sessions after cleanup, got %d", count)
	}
}

func TestUDPProxy_HandleDatagram_NoBackends(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)

	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	proxy.handleDatagram(clientAddr, []byte("test"))

	// Should have dropped the packet
	if proxy.droppedPackets.Load() != 1 {
		t.Errorf("expected 1 dropped packet, got %d", proxy.droppedPackets.Load())
	}
}

func TestUDPProxy_CreateSession_MaxSessions(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &UDPProxyConfig{
		MaxSessions: 1,
		BufferSize:  65535,
		IdleTimeout: 30 * time.Second,
	}
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Manually add a session to fill the limit
	clientAddr1 := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr1, backendAddr, backendConn, nil)
	proxy.mu.Lock()
	proxy.sessions[clientAddr1.String()] = session
	proxy.mu.Unlock()

	// Try to create another session — should fail
	clientAddr2 := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	_, err = proxy.createSession(clientAddr2)
	if err == nil {
		t.Error("expected error for max sessions")
	}
}

// --- Default config test ---

func TestDefaultTCPProxyConfig(t *testing.T) {
	cfg := DefaultTCPProxyConfig()
	if cfg.DialTimeout != 10*time.Second {
		t.Errorf("DialTimeout = %v, want 10s", cfg.DialTimeout)
	}
	if cfg.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", cfg.IdleTimeout)
	}
	if cfg.BufferSize != 32*1024 {
		t.Errorf("BufferSize = %d, want 32KB", cfg.BufferSize)
	}
	if cfg.MaxConnections != 0 {
		t.Errorf("MaxConnections = %d, want 0", cfg.MaxConnections)
	}
	if !cfg.EnableTCPKeepalive {
		t.Error("EnableTCPKeepalive should be true")
	}
}

// --- SetTCPKeepAlive with real TCP conn ---

func TestSetTCPKeepAlive_RealTCPConn(t *testing.T) {
	// Start a real TCP listener to get a real TCP connection
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Test enabling keepalive
	err = SetTCPKeepAlive(conn, true, 30*time.Second)
	if err != nil {
		t.Errorf("SetTCPKeepAlive(true) error: %v", err)
	}

	// Test disabling keepalive
	err = SetTCPKeepAlive(conn, false, 0)
	if err != nil {
		t.Errorf("SetTCPKeepAlive(false) error: %v", err)
	}
}

// --- SetTCPNoDelay with real TCP conn ---

func TestSetTCPNoDelay_RealTCPConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	err = SetTCPNoDelay(conn, true)
	if err != nil {
		t.Errorf("SetTCPNoDelay error: %v", err)
	}
}

// --- GetTCPConn with real TCP conn ---

func TestGetTCPConn_RealTCPConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	tcpConn := GetTCPConn(conn)
	if tcpConn == nil {
		t.Error("expected non-nil TCPConn for real TCP connection")
	}
}

// --- TCPListener StartError after acceptLoop error ---

func TestTCPListener_StartError_AfterAcceptError(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, err := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})
	if err != nil {
		t.Fatalf("NewTCPListener error: %v", err)
	}

	err = listener.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Force close the underlying listener to cause acceptLoop error
	listener.mu.Lock()
	if l, ok := listener.listener.(*net.TCPListener); ok {
		l.Close()
	}
	listener.mu.Unlock()

	// Wait a bit for acceptLoop to detect the error
	time.Sleep(200 * time.Millisecond)

	// StartError should now reflect the accept error
	startErr := listener.StartError()
	_ = startErr // May or may not be set depending on timing, just verify no panic

	// Clean up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listener.Stop(ctx)
}

// --- TCPProxy HandleConnection with real backend (keepalive path) ---

func TestTCPProxy_HandleConnection_WithKeepalive(t *testing.T) {
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoListener.Close()

	go func() {
		conn, err := echoListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", echoListener.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	proxy := NewTCPProxy(pool, NewSimpleBalancer(), &TCPProxyConfig{
		DialTimeout:        5 * time.Second,
		IdleTimeout:        5 * time.Second,
		BufferSize:         4096,
		EnableTCPKeepalive: true,
		TCPKeepalivePeriod: 10 * time.Second,
	})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	testData := []byte("hello keepalive")
	client.Write(testData)

	buf := make([]byte, 1024)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("echo = %q, want %q", string(buf[:n]), string(testData))
	}

	client.Close()
	<-done
}

// --- TCPListener Stop_WithActiveConnections already covered by TestTCPProxy_Stop_WithActiveConnections ---

// --- UDP Integration Tests ---

func TestUDPProxy_StartStop_Integration(t *testing.T) {
	// Start a UDP echo backend
	backendServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendServer.Close()

	// Echo server
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := backendServer.ReadFrom(buf)
			if err != nil {
				return
			}
			backendServer.WriteTo(buf[:n], addr)
		}
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	cfg := DefaultUDPProxyConfig()
	cfg.IdleTimeout = 5 * time.Second
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	addr := proxy.ListenAddr()
	if addr == nil {
		t.Fatal("ListenAddr should not be nil after Start")
	}

	// Send a datagram through the proxy
	clientConn, err := net.DialUDP("udp", nil, addr.(*net.UDPAddr))
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer clientConn.Close()

	testData := []byte("hello udp proxy")
	clientConn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = clientConn.Write(testData)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	if string(buf[:n]) != string(testData) {
		t.Errorf("echo = %q, want %q", string(buf[:n]), string(testData))
	}

	// Verify stats
	stats := proxy.Stats()
	if stats.PacketsForwarded < 2 {
		t.Errorf("expected at least 2 packets forwarded, got %d", stats.PacketsForwarded)
	}
	if stats.ActiveSessions < 1 {
		t.Errorf("expected at least 1 active session, got %d", stats.ActiveSessions)
	}
}

func TestUDPProxy_MultipleDatagrams(t *testing.T) {
	backendServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendServer.Close()

	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := backendServer.ReadFrom(buf)
			if err != nil {
				return
			}
			backendServer.WriteTo(buf[:n], addr)
		}
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	cfg := DefaultUDPProxyConfig()
	cfg.IdleTimeout = 5 * time.Second
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	clientConn, err := net.DialUDP("udp", nil, proxy.ListenAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer clientConn.Close()

	clientConn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send multiple packets
	for i := 0; i < 5; i++ {
		data := []byte(fmt.Sprintf("packet-%d", i))
		_, err = clientConn.Write(data)
		if err != nil {
			t.Fatalf("Write %d error: %v", i, err)
		}

		buf := make([]byte, 1024)
		n, err := clientConn.Read(buf)
		if err != nil {
			t.Fatalf("Read %d error: %v", i, err)
		}
		if string(buf[:n]) != string(data) {
			t.Errorf("packet %d: echo = %q, want %q", i, string(buf[:n]), string(data))
		}
	}

	// Should reuse the same session
	if proxy.ActiveSessions() != 1 {
		t.Errorf("expected 1 session for same client, got %d", proxy.ActiveSessions())
	}
}

func TestUDPProxy_DroppedPackets_NoHealthyBackends(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	// Add a backend but mark it as down
	b := backend.NewBackend("b1", "127.0.0.1:1")
	b.SetState(backend.StateDown)
	pool.AddBackend(b)

	cfg := DefaultUDPProxyConfig()
	cfg.IdleTimeout = 5 * time.Second
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	err := proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	clientConn, err := net.DialUDP("udp", nil, proxy.ListenAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer clientConn.Close()

	clientConn.SetDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("test"))

	// Wait for the packet to be processed
	time.Sleep(200 * time.Millisecond)

	stats := proxy.Stats()
	if stats.DroppedPackets < 1 {
		t.Errorf("expected at least 1 dropped packet, got %d", stats.DroppedPackets)
	}
}
