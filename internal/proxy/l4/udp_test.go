package l4

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestDefaultUDPProxyConfig(t *testing.T) {
	config := DefaultUDPProxyConfig()

	if config.ListenAddr != ":0" {
		t.Errorf("ListenAddr = %q, want %q", config.ListenAddr, ":0")
	}
	if config.SessionTimeout != 30*time.Second {
		t.Errorf("SessionTimeout = %v, want 30s", config.SessionTimeout)
	}
	if config.MaxSessions != 10000 {
		t.Errorf("MaxSessions = %d, want 10000", config.MaxSessions)
	}
	if config.BufferSize != 65535 {
		t.Errorf("BufferSize = %d, want 65535", config.BufferSize)
	}
	if config.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", config.IdleTimeout)
	}
	if config.CleanupInterval != 30*time.Second {
		t.Errorf("CleanupInterval = %v, want 30s", config.CleanupInterval)
	}
}

func TestNewUDPProxy(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()

	proxy := NewUDPProxy(pool, bal, config)

	if proxy == nil {
		t.Fatal("NewUDPProxy returned nil")
	}
	if proxy.config != config {
		t.Error("Config mismatch")
	}
	if proxy.pool != pool {
		t.Error("Pool mismatch")
	}
	if proxy.balancer != bal {
		t.Error("Balancer mismatch")
	}
	if proxy.sessions == nil {
		t.Error("Sessions map should be initialized")
	}
	if proxy.running.Load() {
		t.Error("Proxy should not be running initially")
	}
}

func TestNewUDPProxy_NilConfig(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	bal := NewSimpleBalancer()

	proxy := NewUDPProxy(pool, bal, nil)

	if proxy == nil {
		t.Fatal("NewUDPProxy(nil config) returned nil")
	}
	if proxy.config == nil {
		t.Error("Config should use defaults when nil")
	}
	if proxy.config.MaxSessions != 10000 {
		t.Errorf("Default MaxSessions = %d, want 10000", proxy.config.MaxSessions)
	}
}

func TestUDPProxy_StartStop(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"

	proxy := NewUDPProxy(pool, bal, config)

	// Start
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	if !proxy.IsRunning() {
		t.Error("Proxy should be running after Start")
	}

	// Verify we got an address
	addr := proxy.ListenAddr()
	if addr == nil {
		t.Error("ListenAddr should not be nil after Start")
	}

	// Stop
	if err := proxy.Stop(); err != nil {
		t.Errorf("Stop error: %v", err)
	}

	if proxy.IsRunning() {
		t.Error("Proxy should not be running after Stop")
	}
}

func TestUDPProxy_DoubleStart(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"

	proxy := NewUDPProxy(pool, bal, config)

	if err := proxy.Start(); err != nil {
		t.Fatalf("First start error: %v", err)
	}
	defer proxy.Stop()

	// Second start should fail
	if err := proxy.Start(); err == nil {
		t.Error("Second Start should return error")
	}
}

func TestUDPProxy_StopNotRunning_Original(t *testing.T) {
	pool := backend.NewPool("test-pool", "round_robin")
	bal := NewSimpleBalancer()

	proxy := NewUDPProxy(pool, bal, nil)

	// Stop without start should fail
	if err := proxy.Stop(); err == nil {
		t.Error("Stop without Start should return error")
	}
}

// setupUDPBackend creates a UDP echo server and returns the proxy and backend address.
func setupUDPBackend(t *testing.T) (*net.UDPConn, string) {
	t.Helper()

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to resolve UDP addr: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("Failed to listen UDP: %v", err)
	}

	return conn, conn.LocalAddr().String()
}

// runEchoServer runs a simple UDP echo server until the connection is closed.
func runEchoServer(conn *net.UDPConn) {
	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		conn.WriteToUDP(buf[:n], remoteAddr)
	}
}

func TestUDPProxy_SessionCreationOnFirstPacket(t *testing.T) {
	// Create a UDP echo backend
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	// Create pool with one backend
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 5 * time.Second
	config.SessionTimeout = 10 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// No sessions before any packets
	if got := proxy.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions before any packet = %d, want 0", got)
	}

	// Send a packet to the proxy
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	_, err = clientConn.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Wait for session to be created
	time.Sleep(100 * time.Millisecond)

	if got := proxy.ActiveSessions(); got != 1 {
		t.Errorf("ActiveSessions after first packet = %d, want 1", got)
	}

	stats := proxy.Stats()
	if stats.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", stats.TotalSessions)
	}
}

func TestUDPProxy_SessionReuseForSameClient(t *testing.T) {
	// Create a UDP echo backend
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 5 * time.Second
	config.SessionTimeout = 10 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Send multiple packets from the same client
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	for i := 0; i < 5; i++ {
		_, err = clientConn.Write([]byte("packet"))
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	// Should still be only 1 session
	if got := proxy.ActiveSessions(); got != 1 {
		t.Errorf("ActiveSessions after 5 packets = %d, want 1", got)
	}

	stats := proxy.Stats()
	if stats.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1 (session should be reused)", stats.TotalSessions)
	}
}

func TestUDPProxy_ForwardAndReply(t *testing.T) {
	// Create a UDP echo backend
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 5 * time.Second
	config.SessionTimeout = 10 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Send a packet and read the echoed reply
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	msg := []byte("hello world")
	_, err = clientConn.Write(msg)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read reply with timeout
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 65535)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Read reply failed: %v", err)
	}

	reply := string(buf[:n])
	if reply != "hello world" {
		t.Errorf("Reply = %q, want %q", reply, "hello world")
	}
}

func TestUDPProxy_SessionIdleTimeout(t *testing.T) {
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 500 * time.Millisecond
	config.SessionTimeout = 10 * time.Second
	config.CleanupInterval = 200 * time.Millisecond

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Send a packet to create session
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	_, err = clientConn.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if got := proxy.ActiveSessions(); got != 1 {
		t.Errorf("ActiveSessions = %d, want 1", got)
	}

	// Wait for idle timeout + cleanup
	time.Sleep(1 * time.Second)

	if got := proxy.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions after idle timeout = %d, want 0", got)
	}
}

func TestUDPProxy_MaxSessionsLimit(t *testing.T) {
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.MaxSessions = 2
	config.IdleTimeout = 30 * time.Second
	config.SessionTimeout = 30 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)

	// Create MaxSessions clients
	var clients []*net.UDPConn
	for i := 0; i < 3; i++ {
		c, err := net.DialUDP("udp", nil, proxyAddr)
		if err != nil {
			t.Fatalf("Failed to dial proxy %d: %v", i, err)
		}
		defer c.Close()
		clients = append(clients, c)

		_, err = c.Write([]byte("hello"))
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
		// Give time for session creation
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	// Should have at most MaxSessions active sessions
	active := proxy.ActiveSessions()
	if active > int64(config.MaxSessions) {
		t.Errorf("ActiveSessions = %d, should not exceed MaxSessions=%d", active, config.MaxSessions)
	}

	// With LRU eviction, no packets should be dropped — the oldest session
	// is evicted to make room for the new one.
	stats := proxy.Stats()
	if stats.ActiveSessions > int64(config.MaxSessions) {
		t.Errorf("ActiveSessions = %d, should not exceed MaxSessions=%d", stats.ActiveSessions, config.MaxSessions)
	}
	if stats.DroppedPackets > 0 {
		t.Errorf("DroppedPackets = %d, expected 0 with LRU eviction", stats.DroppedPackets)
	}
	// Total sessions should be 3 (all three clients created a session, even
	// though the oldest was evicted to make room for the third).
	if stats.TotalSessions != 3 {
		t.Errorf("TotalSessions = %d, want 3", stats.TotalSessions)
	}
}

func TestUDPProxy_MultipleConcurrentClients(t *testing.T) {
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 5 * time.Second
	config.SessionTimeout = 10 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)

	numClients := 5
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			c, err := net.DialUDP("udp", nil, proxyAddr)
			if err != nil {
				errors <- err
				return
			}
			defer c.Close()

			msg := []byte("hello from client")
			_, err = c.Write(msg)
			if err != nil {
				errors <- err
				return
			}

			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 65535)
			n, err := c.Read(buf)
			if err != nil {
				errors <- err
				return
			}

			reply := string(buf[:n])
			if reply != string(msg) {
				errors <- &net.OpError{Op: "read", Err: &echoMismatchError{got: reply, want: string(msg)}}
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Client error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if got := proxy.ActiveSessions(); got != int64(numClients) {
		t.Errorf("ActiveSessions = %d, want %d", got, numClients)
	}
}

type echoMismatchError struct {
	got, want string
}

func (e *echoMismatchError) Error() string {
	return "echo mismatch: got " + e.got + ", want " + e.want
}

func TestUDPProxy_GracefulShutdownClosesSessions(t *testing.T) {
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 30 * time.Second
	config.SessionTimeout = 30 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Create some sessions
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	for i := 0; i < 3; i++ {
		c, err := net.DialUDP("udp", nil, proxyAddr)
		if err != nil {
			t.Fatalf("Dial %d error: %v", i, err)
		}
		defer c.Close()
		c.Write([]byte("hello"))
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	if got := proxy.ActiveSessions(); got == 0 {
		t.Error("Expected active sessions before stop")
	}

	// Stop should close all sessions
	if err := proxy.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	if got := proxy.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions after Stop = %d, want 0", got)
	}

	if proxy.IsRunning() {
		t.Error("Proxy should not be running after Stop")
	}
}

func TestUDPProxy_DNSSimulation(t *testing.T) {
	// Simulate a DNS-like request/response flow.
	// The backend receives a "query" and responds with a "response".

	// Create a simple DNS-like server
	backendAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to resolve: %v", err)
	}
	backendConn, err := net.ListenUDP("udp", backendAddr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer backendConn.Close()

	// DNS-like server: receives query, responds with a prefixed response
	go func() {
		buf := make([]byte, 65535)
		for {
			n, remoteAddr, err := backendConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// Simulate DNS response: prefix with "RESPONSE:"
			response := append([]byte("RESPONSE:"), buf[:n]...)
			backendConn.WriteToUDP(response, remoteAddr)
		}
	}()

	pool := backend.NewPool("dns-pool", "round_robin")
	b := backend.NewBackend("dns-backend", backendConn.LocalAddr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 5 * time.Second
	config.SessionTimeout = 10 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Send a DNS-like query
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	query := []byte("example.com A")
	_, err = clientConn.Write(query)
	if err != nil {
		t.Fatalf("Write query failed: %v", err)
	}

	// Read response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 65535)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Read response failed: %v", err)
	}

	response := string(buf[:n])
	expected := "RESPONSE:example.com A"
	if response != expected {
		t.Errorf("Response = %q, want %q", response, expected)
	}
}

func TestUDPProxy_StatsTracking(t *testing.T) {
	backendConn, backendAddr := setupUDPBackend(t)
	defer backendConn.Close()
	go runEchoServer(backendConn)

	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", backendAddr)
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"
	config.IdleTimeout = 5 * time.Second
	config.SessionTimeout = 10 * time.Second

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Initial stats should be zero
	stats := proxy.Stats()
	if stats.PacketsForwarded != 0 {
		t.Errorf("Initial PacketsForwarded = %d, want 0", stats.PacketsForwarded)
	}
	if stats.BytesForwarded != 0 {
		t.Errorf("Initial BytesForwarded = %d, want 0", stats.BytesForwarded)
	}
	if stats.TotalSessions != 0 {
		t.Errorf("Initial TotalSessions = %d, want 0", stats.TotalSessions)
	}

	// Send some packets and get echoes
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	msg := []byte("stats test")
	numPackets := 3

	for i := 0; i < numPackets; i++ {
		_, err = clientConn.Write(msg)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}

		// Read echo reply
		clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 65535)
		_, err = clientConn.Read(buf)
		if err != nil {
			t.Fatalf("Read %d failed: %v", i, err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	stats = proxy.Stats()

	// Each send + reply = 2 packets forwarded per iteration
	expectedPackets := int64(numPackets * 2)
	if stats.PacketsForwarded != expectedPackets {
		t.Errorf("PacketsForwarded = %d, want %d", stats.PacketsForwarded, expectedPackets)
	}

	// Each direction should count the message bytes
	expectedBytes := int64(len(msg)) * int64(numPackets) * 2
	if stats.BytesForwarded != expectedBytes {
		t.Errorf("BytesForwarded = %d, want %d", stats.BytesForwarded, expectedBytes)
	}

	if stats.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", stats.TotalSessions)
	}

	if stats.ActiveSessions != 1 {
		t.Errorf("ActiveSessions = %d, want 1", stats.ActiveSessions)
	}
}

func TestUDPProxy_NoHealthyBackends(t *testing.T) {
	// Pool with no healthy backends
	pool := backend.NewPool("test-pool", "round_robin")
	b := backend.NewBackend("backend-1", "127.0.0.1:19999")
	b.SetState(backend.StateDown)
	pool.AddBackend(b)

	bal := NewSimpleBalancer()
	config := DefaultUDPProxyConfig()
	config.ListenAddr = "127.0.0.1:0"

	proxy := NewUDPProxy(pool, bal, config)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Send a packet - should be dropped since no healthy backends
	proxyAddr := proxy.ListenAddr().(*net.UDPAddr)
	clientConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer clientConn.Close()

	_, err = clientConn.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Should have no sessions created
	if got := proxy.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions = %d, want 0 (no healthy backends)", got)
	}

	stats := proxy.Stats()
	if stats.DroppedPackets == 0 {
		t.Error("Expected dropped packets when no healthy backends")
	}
}

func TestUDPSession_Touch(t *testing.T) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:1234")
	session := &UDPSession{
		clientAddr: addr,
		created:    time.Now().Add(-1 * time.Minute),
	}
	session.lastActivity.Store(time.Now().Add(-1 * time.Minute))

	before := session.LastActivity()
	time.Sleep(10 * time.Millisecond)
	session.touch()
	after := session.LastActivity()

	if !after.After(before) {
		t.Error("touch() should update lastActivity to a later time")
	}
}

func TestUDPSession_Close(t *testing.T) {
	// Create a real UDP connection to close
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("Failed to create UDP conn: %v", err)
	}

	b := backend.NewBackend("test", "127.0.0.1:9999")
	b.AcquireConn()

	session := newUDPSession(addr, addr, conn, b)

	if session.closed.Load() {
		t.Error("Session should not be closed initially")
	}

	session.close()

	if !session.closed.Load() {
		t.Error("Session should be closed after close()")
	}

	// Double close should not panic
	session.close()
}

func TestUDPProxy_CleanupInterval(t *testing.T) {
	// Test that cleanup interval defaults to half the idle timeout
	config := &UDPProxyConfig{
		ListenAddr:     "127.0.0.1:0",
		IdleTimeout:    10 * time.Second,
		SessionTimeout: 30 * time.Second,
		MaxSessions:    10000,
		BufferSize:     65535,
	}

	pool := backend.NewPool("test", "round_robin")
	bal := NewSimpleBalancer()
	proxy := NewUDPProxy(pool, bal, config)

	if proxy.config.CleanupInterval != 5*time.Second {
		t.Errorf("CleanupInterval = %v, want 5s (half of IdleTimeout)", proxy.config.CleanupInterval)
	}
}

func TestUDPProxy_CleanupIntervalMinimum(t *testing.T) {
	// Test that cleanup interval has a minimum of 1 second
	config := &UDPProxyConfig{
		ListenAddr:     "127.0.0.1:0",
		IdleTimeout:    500 * time.Millisecond,
		SessionTimeout: 1 * time.Second,
		MaxSessions:    10000,
		BufferSize:     65535,
	}

	pool := backend.NewPool("test", "round_robin")
	bal := NewSimpleBalancer()
	proxy := NewUDPProxy(pool, bal, config)

	if proxy.config.CleanupInterval < time.Second {
		t.Errorf("CleanupInterval = %v, should be at least 1s", proxy.config.CleanupInterval)
	}
}
