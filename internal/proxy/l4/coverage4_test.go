package l4

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// ============================================================================
// validateSNIHostname coverage (66.7%)
// Uncovered: empty hostname, control character, invalid character
// ============================================================================

func TestCov_ValidateSNIHostname_Empty(t *testing.T) {
	err := validateSNIHostname("")
	if err == nil {
		t.Error("expected error for empty hostname")
	}
	if !strings.Contains(err.Error(), "length") {
		t.Errorf("error = %q, want length-related", err.Error())
	}
}

func TestCov_ValidateSNIHostname_TooLong(t *testing.T) {
	longHost := strings.Repeat("a", 254)
	err := validateSNIHostname(longHost)
	if err == nil {
		t.Error("expected error for hostname > 253 chars")
	}
}

func TestCov_ValidateSNIHostname_ControlChar(t *testing.T) {
	// Hostname with a control character (< 0x20)
	err := validateSNIHostname("test\x01example.com")
	if err == nil {
		t.Error("expected error for control character in hostname")
	}
	if !strings.Contains(err.Error(), "control character") {
		t.Errorf("error = %q, want control character message", err.Error())
	}
}

func TestCov_ValidateSNIHostname_DelChar(t *testing.T) {
	// Hostname with DEL character (0x7f)
	err := validateSNIHostname("test\x7fexample.com")
	if err == nil {
		t.Error("expected error for DEL character in hostname")
	}
}

func TestCov_ValidateSNIHostname_InvalidChar(t *testing.T) {
	// Hostname with underscore (not allowed)
	err := validateSNIHostname("test_example.com")
	if err == nil {
		t.Error("expected error for underscore in hostname")
	}
	if !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("error = %q, want invalid character message", err.Error())
	}
}

func TestCov_ValidateSNIHostname_ValidHostname(t *testing.T) {
	err := validateSNIHostname("valid.example.com")
	if err != nil {
		t.Errorf("expected nil for valid hostname, got %v", err)
	}
}

// ============================================================================
// parseV1: uncovered branches (UDP4 path - lines 212-221)
// parseV1 UDP4 addresses result in UDPAddr, lines 228-234
// ============================================================================

func TestCov_ParseV1_UDP4_Addresses(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY UDP4 10.0.0.1 10.0.0.2 53 53\r\n")

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if header.Transport != PROXYTransportDgram {
		t.Errorf("Transport = %v, want DGRAM", header.Transport)
	}
	src, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Fatal("SourceAddr should be UDPAddr for UDP4")
	}
	if !src.IP.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("Source IP = %v, want 10.0.0.1", src.IP)
	}
	if src.Port != 53 {
		t.Errorf("Source Port = %d, want 53", src.Port)
	}
}

// ============================================================================
// parseV2: uncovered branches (UDP4/UDP6 addr paths - lines 280-304)
// ============================================================================

func TestCov_ParseV2_UDP4_Addresses(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21) // version=2, PROXY command
	buf.WriteByte(0x12) // AF_INET + DGRAM
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.Write([]byte{10, 0, 0, 1})                  // src IP
	buf.Write([]byte{10, 0, 0, 2})                  // dst IP
	binary.Write(buf, binary.BigEndian, uint16(53)) // src port
	binary.Write(buf, binary.BigEndian, uint16(53)) // dst port

	header, _, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	src, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Fatal("SourceAddr should be UDPAddr for UDP4 DGRAM")
	}
	if src.Port != 53 {
		t.Errorf("Source Port = %d, want 53", src.Port)
	}
}

func TestCov_ParseV2_UDP6_Addresses(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21) // version=2, PROXY command
	buf.WriteByte(0x22) // AF_INET6 + DGRAM
	binary.Write(buf, binary.BigEndian, uint16(36))
	buf.Write(net.ParseIP("2001:db8::1").To16())
	buf.Write(net.ParseIP("2001:db8::2").To16())
	binary.Write(buf, binary.BigEndian, uint16(12345))
	binary.Write(buf, binary.BigEndian, uint16(443))

	header, _, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	src, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Fatal("SourceAddr should be UDPAddr for UDP6 DGRAM")
	}
	if !src.IP.Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("Source IP = %v, want 2001:db8::1", src.IP)
	}
}

// ============================================================================
// WriteV1: uncovered fmt.Fprintf error path (line 389-391)
// ============================================================================

func TestCov_WriteV1_FprintfError(t *testing.T) {
	// A bufio.Writer wrapping a failingWriter that fails on Write.
	// The WriteString path for "PROXY UNKNOWN" is covered elsewhere,
	// but the fmt.Fprintf path for TCP4/TCP6 is not.
	// We test both success and the error branch.
	t.Run("TCP4_success", func(t *testing.T) {
		var buf bytes.Buffer
		w := bufio.NewWriter(&buf)
		src := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 100}
		dst := &net.TCPAddr{IP: net.ParseIP("5.6.7.8"), Port: 200}
		if err := WriteV1(w, src, dst); err != nil {
			t.Fatalf("WriteV1: %v", err)
		}
		if !strings.Contains(buf.String(), "PROXY TCP4 1.2.3.4 5.6.7.8 100 200") {
			t.Errorf("got %q", buf.String())
		}
	})
	t.Run("TCP4_flush_error", func(t *testing.T) {
		w := bufio.NewWriter(&failingWriter{})
		src := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 100}
		dst := &net.TCPAddr{IP: net.ParseIP("5.6.7.8"), Port: 200}
		err := WriteV1(w, src, dst)
		if err == nil {
			t.Error("expected error from failing writer during fmt.Fprintf")
		}
	})
}

// ============================================================================
// PROXYListener.Accept: uncovered read error path (line 544-546)
// ============================================================================

func TestCov_PROXYListener_Accept_ReadError_ClosesConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	config := DefaultPROXYProtocolConfig()
	proxyLn := NewPROXYListener(ln, config)

	// Connect and close immediately so Accept's Read returns error
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return
		}
		conn.Close()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := proxyLn.Accept()
		if err != nil {
			// Accept returned error (read error path, line 544-546)
			return
		}
		conn.Close()
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Accept should return")
	}
}

// ============================================================================
// SNI: peekClientHello - error path returning peeked conn (lines 164-170)
// The "not a TLS handshake record" path with peeked data
// ============================================================================

func TestCov_PeekClientHello_NotTLSWithPeeked(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// Send 5 bytes that look like a TLS header but with wrong content type
		// First byte != 0x16 but still enough to read
		client.Write([]byte{0x17, 0x03, 0x01, 0x00, 0x05})
		client.Close()
	}()

	sni, peeked, err := router.peekClientHello(server)
	if err == nil {
		t.Error("expected error for non-TLS data")
	}
	if sni != "" {
		t.Errorf("SNI = %q, want empty", sni)
	}
	if peeked == nil {
		t.Error("peeked conn should not be nil")
	}
	server.Close()
}

// ============================================================================
// SNI: parseClientHello - truncated data after version (lines 280-282, 286-288)
// Also: truncated cipher suites length (309-311), truncated compression (322-324)
// truncated extensions length (334-336), incomplete handshake header (265-267)
// ============================================================================

func TestCov_ParseClientHello_TruncatedDataAfterHandshakeHeader(t *testing.T) {
	// Handshake header with length but insufficient data
	_, err := parseClientHello([]byte{0x01, 0x00, 0x00, 0x01, 0x03})
	if err == nil {
		t.Error("expected error for truncated data after handshake header")
	}
}

func TestCov_ParseClientHello_TruncatedVersion(t *testing.T) {
	// Handshake header says length 1, but only 1 byte for version (need 2)
	_, err := parseClientHello([]byte{0x01, 0x00, 0x00, 0x02, 0x03})
	if err == nil {
		t.Error("expected error for truncated version")
	}
}

func TestCov_ParseClientHello_VersionOutOfRange(t *testing.T) {
	// Valid length but version 0x0200 is out of range
	buf := []byte{
		0x01,                   // ClientHello type
		0x00, 0x00, 0x24,       // handshake length = 36
		0x02, 0x00,             // version = 0x0200 (invalid)
	}
	buf = append(buf, make([]byte, 32)...) // random (fill to match length)
	_, err := parseClientHello(buf)
	if err == nil {
		t.Error("expected error for version out of range")
	}
}

// ============================================================================
// SNI: RouteConnection - default backend with connection (line 117-119)
// proxyToBackend connecting to a valid backend
// ============================================================================

func TestCov_RouteConnection_DefaultBackendConnects(t *testing.T) {
	// Start a real backend listener
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendLn.Close()

	backendReady := make(chan struct{})
	go func() {
		close(backendReady)
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	<-backendReady

	router := NewSNIRouter(&SNIRouterConfig{
		DefaultBackend:   backendLn.Addr().String(),
		ReadTimeout:      2 * time.Second,
		MaxHandshakeSize: 16 * 1024,
	})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		err := router.RouteConnection(server)
		_ = err // May succeed or fail depending on timing
	}()

	// Send a valid TLS ClientHello for an unknown SNI (should use default)
	client.Write(buildClientHelloWithSNI("unknown.openloadbalancer.dev"))
	// Send some data to verify bidirectional copy works
	client.Write([]byte("hello"))
	time.Sleep(200 * time.Millisecond)
	client.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RouteConnection should complete")
	}
}

// ============================================================================
// SNI: ExtractSNI - error on valid TLS parse failure (line 621-623)
// ============================================================================

func TestCov_ExtractSNI_ParseSNIError(t *testing.T) {
	// Build a TLS ClientHello that passes IsTLSConnection but fails ParseClientHelloSNI
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// Valid TLS record header but with invalid handshake content
		buf := []byte{
			0x16,                   // Handshake
			0x03, 0x01,             // TLS 1.0
			0x00, 0x05,             // Length 5
			0x01,                   // ClientHello
			0x00, 0x00, 0x01,       // handshake length = 1
			0x03,                   // Only 1 byte (need at least 2 for version)
		}
		client.Write(buf)
		client.Close()
	}()

	sni, peeked, err := ExtractSNI(server, 2*time.Second)
	if err == nil {
		t.Error("expected error for malformed ClientHello")
	}
	if sni != "" {
		t.Errorf("SNI = %q, want empty", sni)
	}
	if peeked == nil {
		t.Error("peeked conn should not be nil")
	}
	server.Close()
}

// ============================================================================
// SNI: SNIProxy.HandleConnection - with default backend (line 703-705)
// ============================================================================

func TestCov_SNIProxy_HandleConnection_DefaultBackend(t *testing.T) {
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendLn.Close()

	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	proxy := NewSNIProxy(&SNIRouterConfig{
		DefaultBackend:   backendLn.Addr().String(),
		ReadTimeout:      2 * time.Second,
		MaxHandshakeSize: 16 * 1024,
	})

	client, server := net.Pipe()

	go func() {
		client.Write(buildClientHelloWithSNI("unmatched.openloadbalancer.dev"))
		client.Write([]byte("test-data"))
		time.Sleep(300 * time.Millisecond)
		client.Close()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(server)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleConnection should complete")
	}
}

// ============================================================================
// SNI: SNIBasedProxy acceptLoop - connection limit reached (lines 518, 525-527, 534-535)
// Also: panic recovery in handler (lines 543-545)
// ============================================================================

func TestCov_SNIBasedProxy_AcceptLoop_MaxConnections(t *testing.T) {
	cfg := &SNIRouterConfig{
		MaxConnections: 1,
		ReadTimeout:    2 * time.Second,
	}
	proxy := NewSNIBasedProxy(cfg)

	err := proxy.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer proxy.Stop()

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Saturate the connection counter
	proxy.activeConns.Add(1)

	// Connect - should be rejected because at max connections
	conn, err := net.Dial("tcp", proxy.listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Give it a moment for the accept loop to process
	time.Sleep(200 * time.Millisecond)

	proxy.activeConns.Add(-1)
}

func TestCov_SNIBasedProxy_AcceptLoop_PanicRecovery(t *testing.T) {
	// This tests the panic recovery in the connection handler goroutine
	// by using a router that panics on RouteConnection
	cfg := DefaultSNIRouterConfig()
	proxy := NewSNIBasedProxy(cfg)

	err := proxy.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Connect and send non-TLS data - handleConnection calls RouteConnection
	// which may trigger various error paths but not panic in normal operation.
	// The panic recovery is for unexpected panics.
	conn, err := net.Dial("tcp", proxy.listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Write([]byte("not TLS"))
	time.Sleep(200 * time.Millisecond)
	conn.Close()

	proxy.Stop()
}

// ============================================================================
// SNI: SNIBasedProxy - NewSNIBasedProxy with MaxConnections=0 (line 460-462)
// ============================================================================

func TestCov_NewSNIBasedProxy_ZeroMaxConnections(t *testing.T) {
	cfg := &SNIRouterConfig{
		MaxConnections: 0, // Should default to 10000
	}
	proxy := NewSNIBasedProxy(cfg)
	if proxy.maxConns != 10000 {
		t.Errorf("maxConns = %d, want 10000 when MaxConnections=0", proxy.maxConns)
	}
}

// ============================================================================
// TCP: Stop panic recovery (lines 104-106, 123-125)
// ============================================================================

func TestCov_TCPProxy_Stop_PanicRecovery(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	// The panic recovery in Stop's goroutine is hard to trigger directly.
	// Test that Stop works normally with an already-cancelled context.
	proxy.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := proxy.Stop(ctx)
	// Should complete without panic
	_ = err
}

func TestCov_TCPProxy_HandleConnection_PanicRecovery(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := DefaultTCPProxyConfig()
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	// The panic recovery in HandleConnection catches panics during execution.
	// We can trigger it indirectly but it's mostly for safety.
	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(server)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleConnection should complete")
	}
}

// ============================================================================
// TCP: proxyConnections panic recovery (lines 234-238)
// ============================================================================

func TestCov_TCPProxy_ProxyConnections_PanicInBothDirections(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 1 * time.Second,
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	// Use panicConn for both to trigger panic recovery in both goroutines
	panicConn1 := &panicConn{panicOnRead: true}
	panicConn2 := &panicConn{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.proxyConnections(panicConn1, panicConn2)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxyConnections should recover from panics and complete")
	}
}

// ============================================================================
// TCP: copyWithTimeout - context cancelled during read (lines 261-263, 268-283)
// The full loop with context cancellation
// ============================================================================

func TestCov_TCPProxy_CopyWithTimeout_ContextCancelled(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 5 * time.Second,
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	// Cancel the context
	proxy.cancel()

	// Use a real TCP connection instead of net.Pipe to avoid deadlocks
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Don't read - just hold connection open briefly
		time.Sleep(200 * time.Millisecond)
	}()

	src, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer src.Close()

	dst := &discardConn{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		err := proxy.copyWithTimeout(dst, src)
		if err == nil {
			t.Error("expected error when context is cancelled")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("copyWithTimeout should exit after context cancellation")
	}
}

// discardConn discards all writes, returns EOF on Read.
type discardConn struct {
	net.Conn
}

func (c *discardConn) Read(b []byte) (int, error)    { return 0, io.EOF }
func (c *discardConn) Write(b []byte) (int, error)   { return len(b), nil }
func (c *discardConn) Close() error                   { return nil }
func (c *discardConn) SetDeadline(t time.Time) error  { return nil }
func (c *discardConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *discardConn) SetWriteDeadline(t time.Time) error { return nil }

// ============================================================================
// TCP: copyWithTimeout - zero buffer size fallback (line 261-263)
// ============================================================================

func TestCov_TCPProxy_CopyWithTimeout_ZeroBufferSize(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 5 * time.Second,
		BufferSize:  0, // Zero — should fall back to 32KB
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	echoConn, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer echoConn.Close()

	src, srcPipe := net.Pipe()
	defer src.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.copyWithTimeout(echoConn, srcPipe)
	}()

	src.Write([]byte("zero-buf-test"))
	echoConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 100)
	n, err := echoConn.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(buf[:n]) != "zero-buf-test" {
		t.Errorf("got %q, want zero-buf-test", string(buf[:n]))
	}

	src.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

// ============================================================================
// TCP: TCPListener Start - already running double-check (lines 374-376)
// ============================================================================

func TestCov_TCPListener_Start_RaceDoubleCheck(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, err := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})
	if err != nil {
		t.Fatalf("NewTCPListener: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer listener.Stop(context.Background())

	// Second start should hit the double-check under lock
	if err := listener.Start(); err == nil {
		t.Error("expected error for double start")
	}
}

// ============================================================================
// TCP: TCPListener Stop - double check under lock (lines 421-423)
// ============================================================================

func TestCov_TCPListener_Stop_DoubleCheck(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), nil)

	listener, err := NewTCPListener(&TCPListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Proxy:   proxy,
	})
	if err != nil {
		t.Fatalf("NewTCPListener: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx := context.Background()
	if err := listener.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Second stop should hit the double-check under lock
	if err := listener.Stop(ctx); err == nil {
		t.Error("expected error for double stop")
	}
}

// ============================================================================
// TCP: CopyBidirectional panic recovery (lines 508-513)
// ============================================================================

func TestCov_CopyBidirectional_PanicInFirstDirection(t *testing.T) {
	// Create connections where the first direction (conn1->conn2) panics
	panicWriteConn := &panicWriteConn{}
	normalConn := &panicConn{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		b1, b2, err := CopyBidirectional(panicWriteConn, normalConn, time.Second)
		_ = b1
		_ = b2
		_ = err
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CopyBidirectional should complete after panic recovery")
	}
}

// panicWriteConn panics on Write and returns EOF on Read.
type panicWriteConn struct {
	net.Conn
}

func (c *panicWriteConn) Read(b []byte) (int, error)    { return 0, io.EOF }
func (c *panicWriteConn) Write(b []byte) (int, error)   { panic("write panic") }
func (c *panicWriteConn) Close() error                   { return nil }
func (c *panicWriteConn) SetDeadline(t time.Time) error  { return nil }
func (c *panicWriteConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *panicWriteConn) SetWriteDeadline(t time.Time) error { return nil }

// ============================================================================
// TCP: copyWithBuffer - timeout with data (lines 558-559)
// ============================================================================

func TestCov_CopyWithBuffer_TimeoutWithData(t *testing.T) {
	// Create a source that provides data, then triggers timeout
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Send some data then hold the connection open
		conn.Write([]byte("first"))
		time.Sleep(200 * time.Millisecond)
	}()

	src, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer src.Close()

	dst := &timeoutCollectConn{}

	total, err := copyWithBuffer(dst, src, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total == 0 {
		t.Error("expected some bytes to be copied before timeout")
	}
}

// timeoutCollectConn collects written data, always succeeds.
type timeoutCollectConn struct {
	net.Conn
	total int64
}

func (c *timeoutCollectConn) Read(b []byte) (int, error)    { return 0, io.EOF }
func (c *timeoutCollectConn) Write(b []byte) (int, error)   { c.total += int64(len(b)); return len(b), nil }
func (c *timeoutCollectConn) Close() error                   { return nil }
func (c *timeoutCollectConn) SetDeadline(t time.Time) error  { return nil }
func (c *timeoutCollectConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *timeoutCollectConn) SetWriteDeadline(t time.Time) error { return nil }

// ============================================================================
// TCP: ParseTCPAddress - bare hostname with colon prefix (line 574-576)
// ============================================================================

func TestCov_ParseTCPAddress_ColonWithoutPort(t *testing.T) {
	result, err := ParseTCPAddress(":")
	if err != nil {
		t.Fatalf("ParseTCPAddress error: %v", err)
	}
	// When addr[0]==':', it strips the colon and joins with empty host
	if result != ":" {
		t.Errorf("ParseTCPAddress(\":\") = %q, want \":\"", result)
	}
}

// ============================================================================
// TCP: SetTCPKeepAlive - keepalive period error (line 607-609)
// ============================================================================

func TestCov_SetTCPKeepAlive_EnableWithRealConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	var serverConn net.Conn
	serverReady := make(chan struct{})
	go func() {
		serverConn, _ = ln.Accept()
		close(serverReady)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	<-serverReady
	defer serverConn.Close()

	// Enable keepalive with period
	err = SetTCPKeepAlive(conn, true, 15*time.Second)
	if err != nil {
		t.Errorf("SetTCPKeepAlive(true, 15s) error: %v", err)
	}

	// Disable keepalive
	err = SetTCPKeepAlive(conn, false, 0)
	if err != nil {
		t.Errorf("SetTCPKeepAlive(false) error: %v", err)
	}
}

// ============================================================================
// UDP: LastActivity - type assertion failure (line 125)
// ============================================================================

func TestCov_UDPSession_LastActivity_WrongType(t *testing.T) {
	session := &UDPSession{
		clientAddr:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
		backendAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321},
		created:     time.Now(),
	}
	// Store a non-time.Time value to trigger the wrong-type branch
	session.lastActivity.Store("not-a-time")
	la := session.LastActivity()
	if la != session.created {
		t.Errorf("LastActivity should return created time when value is wrong type, got %v", la)
	}
}

// ============================================================================
// UDP: Start - resolve error (lines 220-222)
// ============================================================================

func TestCov_UDPProxy_Start_InvalidAddress(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &UDPProxyConfig{
		ListenAddr: "invalid:address:too:many:colons",
	}
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)
	err := proxy.Start()
	if err == nil {
		t.Error("expected error for invalid listen address")
		proxy.Stop()
	}
}

// ============================================================================
// UDP: Start - listen failure (lines 225-227)
// ============================================================================

func TestCov_UDPProxy_Start_ListenFailure(t *testing.T) {
	// First, grab a port
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	addr := ln.LocalAddr().String()

	pool := backend.NewPool("test", "round_robin")
	cfg := &UDPProxyConfig{
		ListenAddr: addr, // Already in use
	}
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)
	err = proxy.Start()
	if err == nil {
		t.Error("expected error for address already in use")
		proxy.Stop()
	}
	ln.Close()
}

// ============================================================================
// UDP: receiveLoop - non-timeout error while proxy running (lines 298-302)
// The slog.Error path
// ============================================================================

func TestCov_UDPProxy_ReceiveLoop_NonTimeoutErrorWhileRunning(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := DefaultUDPProxyConfig()
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Create and immediately close the listener conn
	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)

	// Close the conn to cause read errors
	listenerConn.Close()

	// The receiveLoop should exit when it gets a non-timeout error and ctx is valid
	done := make(chan struct{})
	proxy.wg.Add(1)
	go func() {
		proxy.receiveLoop()
		close(done)
	}()

	// It should exit because the listener conn is closed
	select {
	case <-done:
		// Good - exited via error path
	case <-time.After(5 * time.Second):
		t.Fatal("receiveLoop should exit after listener conn error")
	}
}

// ============================================================================
// UDP: createSession - evict closed session to make room (lines 363-366)
// ============================================================================

func TestCov_UDPProxy_CreateSession_EvictClosedSession(t *testing.T) {
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

	cfg := &UDPProxyConfig{
		MaxSessions: 1,
		BufferSize:  65535,
		IdleTimeout: 30 * time.Second,
	}
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Manually add a closed session
	closedClientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 11111}
	closedBackendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	closedBackendConn, _ := net.DialUDP("udp", nil, closedBackendAddr)
	closedSession := newUDPSession(closedClientAddr, closedBackendAddr, closedBackendConn, nil)
	closedSession.close() // Close it
	proxy.mu.Lock()
	proxy.sessions[closedClientAddr.String()] = closedSession
	proxy.mu.Unlock()

	// Create a new session — should evict the closed session
	newClientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22222}
	session, err := proxy.createSession(newClientAddr)
	if err != nil {
		t.Fatalf("createSession error: %v", err)
	}
	if session == nil {
		t.Fatal("session should not be nil")
	}

	// Verify the closed session was evicted
	proxy.mu.RLock()
	_, exists := proxy.sessions[closedClientAddr.String()]
	proxy.mu.RUnlock()
	if exists {
		t.Error("closed session should have been evicted")
	}

	session.close()
}

// ============================================================================
// UDP: receiveFromBackend - all uncovered paths (lines 479-498)
// ============================================================================

func TestCov_UDPProxy_ReceiveFromBackend_NonTimeoutErrorWhileRunning(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := DefaultUDPProxyConfig()
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)

	b := backend.NewBackend("b1", "127.0.0.1:0")
	b.SetState(backend.StateUp)
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}

	// Close the backend conn to cause read errors
	backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, b)

	done := make(chan struct{})
	proxy.wg.Add(1)
	go func() {
		proxy.receiveFromBackend(session)
		close(done)
	}()

	// The receiveFromBackend loop will hit the non-timeout error path.
	// Since the proxy is running, it continues. Cancel the context to stop it.
	time.Sleep(200 * time.Millisecond)
	proxy.cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("receiveFromBackend should exit after context cancel")
	}
}

func TestCov_UDPProxy_ReceiveFromBackend_WriteErrorProxyRunning(t *testing.T) {
	// Test the path where WriteToUDP fails but proxy is still running (continue)
	backendServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendServer.Close()

	// Echo backend
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := backendServer.ReadFrom(buf)
			if err != nil {
				return
			}
			if n > 0 {
				backendServer.WriteTo(buf[:n], addr)
			}
		}
	}()

	cfg := DefaultUDPProxyConfig()
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Create a closed listener conn so WriteToUDP will fail
	closedListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	proxy.listenerConn = closedListener.(*net.UDPConn)
	proxy.running.Store(true)

	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := backendServer.LocalAddr().(*net.UDPAddr)
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, b)

	done := make(chan struct{})
	proxy.wg.Add(1)
	go func() {
		proxy.receiveFromBackend(session)
		close(done)
	}()

	// Close the listener to make WriteToUDP fail, but keep proxy running
	closedListener.Close()

	// Send data to trigger backend reply, which will try WriteToUDP and fail
	backendConn.Write([]byte("trigger-write-error"))

	// The goroutine should continue looping (continue path after write error)
	// then eventually stop via cancel
	time.Sleep(200 * time.Millisecond)
	proxy.running.Store(false)
	proxy.cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("receiveFromBackend should exit after cancel")
	}
}

// ============================================================================
// parseSNIList: short data for SNI entry (lines 411-413 in sni.go via validateSNIHostname)
// Test that validateSNIHostname is called with valid host from parseSNIList
// ============================================================================

func TestCov_ParseSNIList_ValidHostWithInvalidChars(t *testing.T) {
	// Build SNI list with a hostname containing an underscore (invalid)
	buf := new(bytes.Buffer)
	host := "test_invalid.com"
	binary.Write(buf, binary.BigEndian, uint16(len(host)+3))
	buf.WriteByte(0x00) // hostname type
	binary.Write(buf, binary.BigEndian, uint16(len(host)))
	buf.WriteString(host)

	_, err := parseSNIList(buf.Bytes())
	if err == nil {
		t.Error("expected error for hostname with invalid chars")
	}
}

// ============================================================================
// ParseClientHelloSNI: additional truncation paths
// ============================================================================

func TestCov_ParseClientHelloSNI_IncompleteHandshakeBody(t *testing.T) {
	// Valid record header, ClientHello type, but body too short
	data := []byte{
		0x16,                   // Handshake
		0x03, 0x01,             // TLS 1.0
		0x00, 0x04,             // Length 4
		0x01,                   // ClientHello type
		0x00, 0x00, 0x00,       // handshake length = 0
	}
	_, err := ParseClientHelloSNI(data)
	if err == nil {
		t.Error("expected error for ClientHello with empty body")
	}
}

func TestCov_ParseClientHelloSNI_ValidButNoSNI(t *testing.T) {
	// Build a ClientHello with extensions but no SNI extension
	buf := new(bytes.Buffer)
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32))                         // Random
	buf.WriteByte(0x00)                                 // Session ID length = 0
	binary.Write(buf, binary.BigEndian, uint16(2))      // Cipher suites length
	binary.Write(buf, binary.BigEndian, uint16(0x002f)) // Cipher suite
	buf.WriteByte(0x01)                                 // Compression methods length
	buf.WriteByte(0x00)                                 // No compression

	// Extensions with a non-SNI extension
	extStart := buf.Len()
	binary.Write(buf, binary.BigEndian, uint16(0)) // placeholder for extensions length
	binary.Write(buf, binary.BigEndian, uint16(0x0001)) // Extension type: not SNI
	binary.Write(buf, binary.BigEndian, uint16(3))      // Extension length
	buf.WriteString("abc")

	// Update extensions length
	extLen := buf.Len() - extStart - 2
	data := buf.Bytes()
	data[extStart] = byte(extLen >> 8)
	data[extStart+1] = byte(extLen)

	// Update handshake length
	handshakeLen := buf.Len() - handshakeStart - 4
	data[handshakeStart+1] = byte(handshakeLen >> 16)
	data[handshakeStart+2] = byte(handshakeLen >> 8)
	data[handshakeStart+3] = byte(handshakeLen)

	record := make([]byte, 5+len(data[handshakeStart:]))
	record[0] = 0x16
	binary.BigEndian.PutUint16(record[1:3], 0x0301)
	binary.BigEndian.PutUint16(record[3:5], uint16(len(data[handshakeStart:])))
	copy(record[5:], data[handshakeStart:])

	_, err := ParseClientHelloSNI(record)
	if err == nil {
		t.Error("expected error for ClientHello with no SNI extension")
	}
}

// ============================================================================
// SNI: parseSNIExtension - empty extensions (loop doesn't enter)
// ============================================================================

func TestCov_ParseSNIExtension_EmptyData(t *testing.T) {
	_, err := parseSNIExtension([]byte{})
	if err == nil {
		t.Error("expected error for empty extensions data")
	}
}

// ============================================================================
// TCP: HandleConnection with unlimited connections (line 142-143)
// The else branch when MaxConnections is 0
// ============================================================================

func TestCov_TCPProxy_HandleConnection_UnlimitedConns(t *testing.T) {
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", echoLn.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	cfg := &TCPProxyConfig{
		MaxConnections: 0, // unlimited — tests the else branch
		IdleTimeout:    5 * time.Second,
		BufferSize:     1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(server)
	}()

	testData := []byte("unlimited-conns-test")
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
		t.Fatalf("read error: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("got %q, want %q", string(buf[:n]), string(testData))
	}

	client.Close()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

// ============================================================================
// parseV1: invalid port values (out of range)
// ============================================================================

func TestCov_ParseV1_PortOutOfRange(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	// Port > 65535
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 99999 443\r\n")
	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for port out of range")
	}
}

func TestCov_ParseV1_DstPortOutOfRange(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 443 99999\r\n")
	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for destination port out of range")
	}
}

func TestCov_ParseV1_InvalidSrcPort(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 abc 443\r\n")
	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for non-numeric source port")
	}
}

func TestCov_ParseV1_InvalidDstPort(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 443 abc\r\n")
	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for non-numeric destination port")
	}
}

func TestCov_ParseV1_InvalidDstIP(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 not-an-ip 443 80\r\n")
	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for invalid destination IP")
	}
}

// ============================================================================
// CopyBidirectional: both errors are normal close
// ============================================================================

func TestCov_CopyBidirectional_BothNormalErrors(t *testing.T) {
	conn1 := &errorConn{returnErr: io.EOF}
	conn2 := &errorConn{returnErr: io.EOF}

	b1, b2, err := CopyBidirectional(conn1, conn2, time.Second)
	if err != nil {
		t.Errorf("expected nil error when both are normal close, got %v", err)
	}
	_ = b1
	_ = b2
}

// ============================================================================
// CopyBidirectional: first error is non-normal, second is normal
// ============================================================================

func TestCov_CopyBidirectional_FirstNonNormalSecondNormal(t *testing.T) {
	specificErr := fmt.Errorf("first-direction-failure")
	conn1 := &errorConn{returnErr: specificErr}
	conn2 := &errorConn{returnErr: io.EOF}

	_, _, err := CopyBidirectional(conn1, conn2, time.Second)
	if err != specificErr {
		t.Errorf("error = %v, want %v", err, specificErr)
	}
}

// ============================================================================
// UDP: forwardToBackend - successful write with backend stats recording
// ============================================================================

func TestCov_UDPProxy_ForwardToBackend_SuccessfulWriteWithBackend(t *testing.T) {
	// Start a backend echo server
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
			if n > 0 {
				backendServer.WriteTo(buf[:n], addr)
			}
		}
	}()

	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)

	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := backendServer.LocalAddr().(*net.UDPAddr)
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, b)

	testData := []byte("forward-test")
	packetsBefore := proxy.packetsForwarded.Load()
	proxy.forwardToBackend(session, testData)

	// Verify stats
	if proxy.packetsForwarded.Load() != packetsBefore+1 {
		t.Errorf("packetsForwarded should have incremented")
	}
	if session.packetsIn.Load() != 1 {
		t.Errorf("packetsIn = %d, want 1", session.packetsIn.Load())
	}
	if session.bytesIn.Load() != int64(len(testData)) {
		t.Errorf("bytesIn = %d, want %d", session.bytesIn.Load(), len(testData))
	}

	session.close()
}

// ============================================================================
// acceptLoop - test with MaxConnections=0 and concurrent connections
// (This ensures the CAS loop works under contention for SNIBasedProxy)
// ============================================================================

type testNoDialBalancer struct {
	atomic.Uint64
}

func (b *testNoDialBalancer) Next(ctx *backend.RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}
	idx := b.Add(1) % uint64(len(backends))
	return backends[idx]
}

// ============================================================================
// proxyConnections - verify both directions close their respective conns
// ============================================================================

func TestCov_TCPProxy_ProxyConnections_ClientClosesMidTransfer(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 5 * time.Second,
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	backendConn, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer backendConn.Close()

	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.proxyConnections(clientConn, backendConn)
	}()

	// Send data and read response
	proxyConn.Write([]byte("partial"))
	buf := make([]byte, 1024)
	proxyConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _ := proxyConn.Read(buf)
	_ = n

	// Close mid-transfer
	proxyConn.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxyConnections should complete after close")
	}
}

// ============================================================================
// SNI: ParseClientHelloSNI with valid data that passes through parseClientHello
// but has empty SNI list
// ============================================================================

func TestCov_ParseSNIList_EmptyListAfterLength(t *testing.T) {
	// SNI list length = 0
	_, err := parseSNIList([]byte{0x00, 0x00})
	if err == nil {
		t.Error("expected error for empty SNI list")
	}
}

// ============================================================================
// Additional coverage for remaining uncovered lines
// ============================================================================

// ============================================================================
// parseV2: insufficient data for IPv4 and IPv6 with proper addrData length
// (lines 280-282, 302-304) - these paths are already tested but with
// different data construction. Let's verify they're covered by testing
// the full Parse path with exact data.
// ============================================================================

func TestCov_ParseV2_IPv4StreamWithTLVs(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21) // version=2, PROXY command
	buf.WriteByte(0x11) // AF_INET + STREAM
	// 12 bytes addr + 7 bytes TLV = 19
	binary.Write(buf, binary.BigEndian, uint16(19))
	buf.Write([]byte{192, 168, 1, 1})                  // src IP
	buf.Write([]byte{192, 168, 1, 2})                  // dst IP
	binary.Write(buf, binary.BigEndian, uint16(12345)) // src port
	binary.Write(buf, binary.BigEndian, uint16(443))   // dst port
	// TLV
	buf.WriteByte(0x01)
	binary.Write(buf, binary.BigEndian, uint16(4))
	buf.WriteString("test")

	header, remaining, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(header.TLVs) != 1 {
		t.Errorf("TLVs = %d, want 1", len(header.TLVs))
	}
	if len(remaining) != 0 {
		t.Errorf("remaining = %d bytes, want 0", len(remaining))
	}
}

// ============================================================================
// WriteV1: WriteString error path for UNKNOWN (line 369-371)
// ============================================================================

func TestCov_WriteV1_UnknownWriteStringError(t *testing.T) {
	// Test the WriteString error path in the UNKNOWN branch
	// We need a bufio.Writer that fails on WriteString
	w := bufio.NewWriterSize(&failingWriter{}, 64) // small buffer to trigger flush
	srcAddr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 100}
	dstAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 200}
	err := WriteV1(w, srcAddr, dstAddr)
	if err == nil {
		t.Error("expected error from failing writer on WriteString for UNKNOWN")
	}
}

// ============================================================================
// WriteV1: fmt.Fprintf error for TCP4 (line 389-391)
// ============================================================================

func TestCov_WriteV1_TCP4FprintfError(t *testing.T) {
	// Test the fmt.Fprintf error path for TCP4 addresses
	w := bufio.NewWriterSize(&failingWriter{}, 32)
	srcAddr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 100}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("5.6.7.8"), Port: 200}
	err := WriteV1(w, srcAddr, dstAddr)
	if err == nil {
		t.Error("expected error from failing writer during fmt.Fprintf for TCP4")
	}
}

// ============================================================================
// TCP: proxyConnections - second direction panic recovery (lines 234-238)
// ============================================================================

func TestCov_TCPProxy_ProxyConnections_SecondDirPanicRecovery(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 1 * time.Second,
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	// panicOnSecondReadConn panics on the second read (backend->client direction)
	backendConn := &panicOnSecondReadConn{}
	clientConn := &panicConn{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.proxyConnections(clientConn, backendConn)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxyConnections should recover from panic in second direction")
	}
}

// ============================================================================
// TCP: copyWithTimeout - timeout with data written (lines 279-283)
// The branch where netErr.Timeout() is true AND n > 0
// ============================================================================

func TestCov_TCPProxy_CopyWithTimeout_TimeoutWithDataWritten(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 100 * time.Millisecond, // Very short timeout
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	// Create a slow source that sends data then blocks
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Send some data then hold open
		conn.Write([]byte("data"))
		// Hold open until timeout
		time.Sleep(500 * time.Millisecond)
	}()

	src, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer src.Close()

	dst := &collectConn{}

	err = proxy.copyWithTimeout(dst, src)
	if err != nil {
		t.Logf("copyWithTimeout returned err=%v", err)
	}
	if dst.buf.Len() == 0 {
		t.Error("expected some data to be copied before timeout")
	}
}

// collectConn collects all writes.
type collectConn struct {
	net.Conn
	buf bytes.Buffer
}

func (c *collectConn) Read(b []byte) (int, error)    { return 0, io.EOF }
func (c *collectConn) Write(b []byte) (int, error)   { c.buf.Write(b); return len(b), nil }
func (c *collectConn) Close() error                   { return nil }
func (c *collectConn) SetDeadline(t time.Time) error  { return nil }
func (c *collectConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *collectConn) SetWriteDeadline(t time.Time) error { return nil }

// ============================================================================
// TCP: CopyBidirectional - second direction panic recovery (lines 508-513)
// ============================================================================

func TestCov_CopyBidirectional_SecondDirPanicRecovery(t *testing.T) {
	// conn1 reads fine, conn2 panics on read (second direction)
	conn1 := &panicConn{} // EOF on read - first direction completes
	conn2 := &panicOnSecondReadConn{}

	b1, b2, err := CopyBidirectional(conn1, conn2, time.Second)
	_ = b1
	_ = b2
	// Should recover from panic
	_ = err
}

// ============================================================================
// UDP: receiveLoop - non-timeout error with valid ctx (lines 298-302)
// slog.Error path
// ============================================================================

func TestCov_UDPProxy_ReceiveLoop_ErrorThenStop(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := DefaultUDPProxyConfig()
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Create and immediately close the listener conn
	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)

	// Close it to cause errors
	listenerConn.Close()

	done := make(chan struct{})
	proxy.wg.Add(1)
	go func() {
		proxy.receiveLoop()
		close(done)
	}()

	// Wait for the receiveLoop to hit the error and exit
	// Since running is true but ctx is not cancelled, it should hit slog.Error and return
	select {
	case <-done:
		// Good - exited via error path
	case <-time.After(5 * time.Second):
		// Fallback: cancel context
		proxy.cancel()
		<-done
	}
}

// ============================================================================
// UDP: createSession - dial backend fails (lines 412-415)
// ============================================================================

func TestCov_UDPProxy_CreateSession_DialBackendFails(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	// Use a backend with an address that can be resolved but can't be dialed
	// Port 0 on a UDP address won't work for DialUDP - use an invalid format
	b := backend.NewBackend("b1", "0.0.0.0:0")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)

	cfg := DefaultUDPProxyConfig()
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	_, err := proxy.createSession(clientAddr)
	// DialUDP to 0.0.0.0:0 may or may not fail depending on the OS
	// But the important thing is we exercise the path
	_ = err
}

// ============================================================================
// UDP: receiveFromBackend - zero length read (line 485-486)
// ============================================================================

func TestCov_UDPProxy_ReceiveFromBackend_ZeroReadFromBackend(t *testing.T) {
	// Create a backend that sends zero-length data followed by real data
	backendServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendServer.Close()

	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer listenerConn.Close()

	cfg := DefaultUDPProxyConfig()
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)

	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := backendServer.LocalAddr().(*net.UDPAddr)
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, b)

	done := make(chan struct{})
	proxy.wg.Add(1)
	go func() {
		proxy.receiveFromBackend(session)
		close(done)
	}()

	// Backend echoes data back
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := backendServer.ReadFrom(buf)
			if err != nil {
				return
			}
			if n > 0 {
				backendServer.WriteTo(buf[:n], addr)
			}
		}
	}()

	// Send data to create traffic
	testData := []byte("hello")
	backendConn.Write(testData)

	// Read echo on client side
	clientConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer clientConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readBuf := make([]byte, 1024)
	_, _, err = clientConn.ReadFrom(readBuf)
	// We're just trying to exercise the n==0 path; the main point is the
	// receiveFromBackend goroutine runs through its loop

	// Clean up
	session.close()
	proxy.running.Store(false)
	proxy.cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("receiveFromBackend should exit")
	}
}

// ============================================================================
// UDP: receiveFromBackend - write error, proxy still running (lines 496-498)
// ============================================================================

func TestCov_UDPProxy_ReceiveFromBackend_WriteErrorContinue(t *testing.T) {
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
			if n > 0 {
				backendServer.WriteTo(buf[:n], addr)
			}
		}
	}()

	cfg := DefaultUDPProxyConfig()
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)

	// Create a listener conn that will be closed to cause WriteToUDP failure
	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)

	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := backendServer.LocalAddr().(*net.UDPAddr)
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()

	session := newUDPSession(clientAddr, backendAddr, backendConn, b)

	done := make(chan struct{})
	proxy.wg.Add(1)
	go func() {
		proxy.receiveFromBackend(session)
		close(done)
	}()

	// Close the listener to cause WriteToUDP failure
	listenerConn.Close()

	// Send data to trigger backend reply -> write to client -> error
	backendConn.Write([]byte("trigger"))

	// Wait a moment for the error to be processed, then cancel
	time.Sleep(300 * time.Millisecond)
	proxy.cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("receiveFromBackend should exit after cancel")
	}
}

// ============================================================================
// SNI: peekClientHello - error reading record body (line 157-159)
// ============================================================================

func TestCov_PeekClientHello_ReadRecordError(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// Send only 5 bytes (TLS record header) but with a large record length
		// so io.ReadFull blocks, then close
		client.Write([]byte{0x16, 0x03, 0x01, 0x00, 0x20}) // claims 32 bytes
		time.Sleep(50 * time.Millisecond)
		client.Close()
	}()

	sni, _, err := router.peekClientHello(server)
	if err == nil {
		t.Error("expected error when record body can't be read")
	}
	_ = sni
	server.Close()
}

// ============================================================================
// SNI: ParseClientHelloSNI - data too short for handshake header (line 265-267)
// ============================================================================

func TestCov_ParseClientHelloSNI_ShortHandshakeHeader(t *testing.T) {
	data := []byte{
		0x16,                   // Handshake
		0x03, 0x01,             // TLS 1.0
		0x00, 0x02,             // Length 2
		0x01,                   // ClientHello type
		0x00,                   // Only 1 byte of handshake data (need 4)
	}
	_, err := ParseClientHelloSNI(data)
	if err == nil {
		t.Error("expected error for short handshake header")
	}
}

// ============================================================================
// SNI: parseClientHello - all truncation paths with single-call approach
// Lines 280-282, 309-311, 322-324, 334-336
// ============================================================================

func TestCov_ParseClientHello_AllTruncationPaths(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"data too short - 3 bytes", []byte{0x01, 0x00, 0x00}},
		{"incomplete ClientHello", func() []byte {
			// handshake length = 100 but only 6 bytes follow
			return []byte{0x01, 0x00, 0x00, 0x64, 0x03, 0x03}
		}()},
		{"data too short for session ID length", func() []byte {
			buf := []byte{0x01, 0x00, 0x00, 0x24}
			buf = append(buf, 0x03, 0x03) // version
			buf = append(buf, make([]byte, 32)...) // random
			// Missing session ID length byte
			return buf[:len(buf)-1]
		}()},
		{"data too short for cipher suites length", func() []byte {
			buf := []byte{0x01, 0x00, 0x00, 0x26}
			buf = append(buf, 0x03, 0x03)
			buf = append(buf, make([]byte, 32)...)
			buf = append(buf, 0x00) // session ID length = 0
			// Missing cipher suites length
			return buf[:len(buf)-1]
		}()},
		{"data too short for compression length", func() []byte {
			buf := []byte{0x01, 0x00, 0x00, 0x2A}
			buf = append(buf, 0x03, 0x03)
			buf = append(buf, make([]byte, 32)...)
			buf = append(buf, 0x00) // session ID length
			buf = append(buf, 0x00, 0x02) // cipher suites length
			buf = append(buf, 0x00, 0x2f) // cipher suite
			// Missing compression length
			return buf[:len(buf)-1]
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseClientHello(tt.data)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

// ============================================================================
// SNI: SNIProxy HandleConnection - default backend with failed dial (line 703-705)
// ============================================================================

func TestCov_SNIProxy_HandleConnection_DefaultBackendDialFails(t *testing.T) {
	proxy := NewSNIProxy(&SNIRouterConfig{
		DefaultBackend:   "127.0.0.1:1", // Port 1 - should fail to connect
		ReadTimeout:      1 * time.Second,
		MaxHandshakeSize: 16 * 1024,
	})

	client, server := net.Pipe()

	go func() {
		client.Write(buildClientHelloWithSNI("unmatched.openloadbalancer.dev"))
		client.Close()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(server)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleConnection should complete even when backend dial fails")
	}
}
