package l4

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestUDPProxy_ListenAddr_BeforeStart(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	balancer := NewSimpleBalancer()
	proxy := NewUDPProxy(pool, balancer, nil)
	if addr := proxy.ListenAddr(); addr != nil {
		t.Errorf("ListenAddr() = %v, want nil before Start()", addr)
	}
}

func TestUDPProxy_ListenAddr_AfterStart(t *testing.T) {
	backendConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for backend: %v", err)
	}
	defer backendConn.Close()
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := backendConn.ReadFrom(buf)
			if err != nil {
				return
			}
			backendConn.WriteTo(buf[:n], addr)
		}
	}()
	pool := backend.NewPool("test", "round_robin")
	b := backend.NewBackend("b1", backendConn.LocalAddr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	cfg := DefaultUDPProxyConfig()
	cfg.IdleTimeout = 5 * time.Second
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)
	if err := proxy.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer proxy.Stop()
	addr := proxy.ListenAddr()
	if addr == nil {
		t.Fatal("ListenAddr() = nil, want non-nil after Start()")
	}
	if addr.String() == "" {
		t.Error("ListenAddr().String() is empty")
	}
}

func TestUDPProxy_ForwardToBackend_ClosedSession(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), nil)
	b := backend.NewBackend("b1", "127.0.0.1:0")
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	backendConn, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer backendConn.Close()
	session := newUDPSession(clientAddr, backendAddr, backendConn, b)
	session.close()
	packetsBefore := proxy.packetsForwarded.Load()
	proxy.forwardToBackend(session, []byte("test"))
	packetsAfter := proxy.packetsForwarded.Load()
	if packetsAfter != packetsBefore {
		t.Errorf("packetsForwarded changed from %d to %d, expected no change",
			packetsBefore, packetsAfter)
	}
}

func TestUDPProxy_ReceiveFromBackend_ExitsOnSessionClose(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	backendListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendListener.Close()
	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket for proxy: %v", err)
	}
	defer listenerConn.Close()
	cfg := DefaultUDPProxyConfig()
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)
	b := backend.NewBackend("b1", backendListener.LocalAddr().String())
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	backendAddr, _ := net.ResolveUDPAddr("udp", backendListener.LocalAddr().String())
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
	session.close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("receiveFromBackend did not exit within timeout after session close")
	}
}

func TestUDPProxy_ReceiveFromBackend_ForwardsData(t *testing.T) {
	backendServer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer backendServer.Close()
	listenerConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket for proxy: %v", err)
	}
	defer listenerConn.Close()
	clientConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket for client: %v", err)
	}
	defer clientConn.Close()
	cfg := DefaultUDPProxyConfig()
	pool := backend.NewPool("test", "round_robin")
	proxy := NewUDPProxy(pool, NewSimpleBalancer(), cfg)
	proxy.listenerConn = listenerConn.(*net.UDPConn)
	proxy.running.Store(true)
	b := backend.NewBackend("b1", backendServer.LocalAddr().String())
	b.SetState(backend.StateUp)
	clientAddr := clientConn.LocalAddr().(*net.UDPAddr)
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
	go func() {
		buf := make([]byte, 65535)
		n, addr, err := backendServer.ReadFrom(buf)
		if err != nil {
			return
		}
		backendServer.WriteTo(buf[:n], addr)
	}()
	testData := []byte("hello from client")
	backendConn.Write(testData)
	clientBuf := make([]byte, 65535)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := clientConn.ReadFrom(clientBuf)
	if err != nil {
		t.Fatalf("client read error: %v", err)
	}
	if string(clientBuf[:n]) != string(testData) {
		t.Errorf("client received %q, want %q", string(clientBuf[:n]), string(testData))
	}
	if session.packetsOut.Load() < 1 {
		t.Error("expected packetsOut >= 1")
	}
	if session.bytesOut.Load() < int64(len(testData)) {
		t.Errorf("expected bytesOut >= %d, got %d", len(testData), session.bytesOut.Load())
	}
	session.close()
	proxy.running.Store(false)
	proxy.cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for receiveFromBackend to exit")
	}
}

func TestCopyBidirectional_BidirectionalEcho(t *testing.T) {
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		CopyBidirectional(proxyConn, echoConn, 5*time.Second)
	}()
	testData := []byte("hello bidirectional")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		clientConn.Write(testData)
	}()
	buf := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("client read error: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("received %q, want %q", string(buf[:n]), string(testData))
	}
	clientConn.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for CopyBidirectional to complete")
	}
	wg.Wait()
}

func TestCopyBidirectional_ErrorPaths(t *testing.T) {
	conn1a, conn1b := net.Pipe()
	conn2a, conn2b := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		CopyBidirectional(conn1b, conn2b, 100*time.Millisecond)
	}()
	conn1a.Close()
	conn2a.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("CopyBidirectional did not complete within timeout")
	}
}

func TestCopyBidirectional_IdleTimeout(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()
	echoConn1, echoConn2 := net.Pipe()
	defer echoConn1.Close()
	defer echoConn2.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		CopyBidirectional(conn2, echoConn2, 50*time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("CopyBidirectional did not exit within timeout")
	}
}

type closedConnError struct{}

func (e *closedConnError) Error() string   { return "use of closed network connection" }
func (e *closedConnError) Timeout() bool   { return false }
func (e *closedConnError) Temporary() bool { return false }
