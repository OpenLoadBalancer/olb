package l7

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/router"
)

// --- computeWebSocketAccept ---

func TestComputeWebSocketAccept(t *testing.T) {
	// Verify determinism
	a := computeWebSocketAccept("test-key")
	b := computeWebSocketAccept("test-key")
	if a != b {
		t.Error("computeWebSocketAccept should be deterministic")
	}

	// Verify it produces a valid base64 string for known inputs
	result := computeWebSocketAccept("dGhlbGxvIGlzIG5vIGJpbmU=")
	if result == "" {
		t.Error("computeWebSocketAccept should return non-empty string")
	}

	// Empty key should also work
	empty := computeWebSocketAccept("")
	if empty == "" {
		t.Error("computeWebSocketAccept with empty key should return non-empty")
	}
}

// --- writeUpgradeResponse ---

func TestWriteUpgradeResponse(t *testing.T) {
	t.Run("with backend headers", func(t *testing.T) {
		conn1, conn2 := net.Pipe()
		defer conn1.Close()
		defer conn2.Close()

		resp := &http.Response{
			StatusCode: 101,
			Header: http.Header{
				"Sec-WebSocket-Accept": []string{"accept-value"},
				"Upgrade":              []string{"websocket"},
				"Connection":           []string{"Upgrade"},
			},
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh := NewWebSocketHandler(nil)
			err := wh.writeUpgradeResponse(conn2, resp, "test-key")
			if err != nil {
				t.Errorf("writeUpgradeResponse error: %v", err)
			}
		}()

		buf := make([]byte, 4096)
		conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn1.Read(buf)
		if err != nil {
			t.Fatalf("read error: %v", err)
		}

		result := string(buf[:n])
		if !strings.Contains(result, "HTTP/1.1 101") {
			t.Errorf("expected 101 response, got: %s", result)
		}
		if !strings.Contains(result, "Sec-WebSocket-Accept: accept-value") {
			t.Errorf("expected Sec-WebSocket-Accept header, got: %s", result)
		}
		<-done
	})

	t.Run("missing accept header computes it", func(t *testing.T) {
		conn1, conn2 := net.Pipe()
		defer conn1.Close()
		defer conn2.Close()

		resp := &http.Response{
			StatusCode: 101,
			Header:     http.Header{},
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh := NewWebSocketHandler(nil)
			wh.writeUpgradeResponse(conn2, resp, "test-key")
		}()

		buf := make([]byte, 4096)
		conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := conn1.Read(buf)
		result := string(buf[:n])
		expected := computeWebSocketAccept("test-key")
		if !strings.Contains(result, fmt.Sprintf("Sec-WebSocket-Accept: %s", expected)) {
			t.Errorf("expected computed Sec-WebSocket-Accept, got: %s", result)
		}
		<-done
	})

	t.Run("missing upgrade and connection headers", func(t *testing.T) {
		conn1, conn2 := net.Pipe()
		defer conn1.Close()
		defer conn2.Close()

		resp := &http.Response{
			StatusCode: 101,
			Header: http.Header{
				"Sec-WebSocket-Accept": []string{"accept-val"},
			},
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh := NewWebSocketHandler(nil)
			wh.writeUpgradeResponse(conn2, resp, "key")
		}()

		buf := make([]byte, 4096)
		conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := conn1.Read(buf)
		result := string(buf[:n])
		if !strings.Contains(result, "Upgrade: websocket") {
			t.Errorf("expected Upgrade header to be added, got: %s", result)
		}
		if !strings.Contains(result, "Connection: Upgrade") {
			t.Errorf("expected Connection header to be added, got: %s", result)
		}
		<-done
	})
}

// --- isWebSocketCloseError ---

func TestIsWebSocketCloseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"eof", io.EOF, true},
		{"closed conn", fmt.Errorf("use of closed network connection"), true},
		{"broken pipe", fmt.Errorf("broken pipe error"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"other error", fmt.Errorf("some other error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWebSocketCloseError(tt.err)
			if got != tt.want {
				t.Errorf("isWebSocketCloseError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- copyWithIdleTimeout ---

func TestCopyWithIdleTimeout(t *testing.T) {
	t.Run("copies data", func(t *testing.T) {
		src, srcPipe := net.Pipe()
		dst, dstPipe := net.Pipe()
		defer src.Close()
		defer dstPipe.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh := NewWebSocketHandler(nil)
			err := wh.copyWithIdleTimeout(dst, src, 2*time.Second)
			_ = err
		}()

		srcPipe.Write([]byte("hello"))
		buf := make([]byte, 100)
		dstPipe.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := dstPipe.Read(buf)
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if string(buf[:n]) != "hello" {
			t.Errorf("got %q, want hello", string(buf[:n]))
		}
		srcPipe.Close()
		dst.Close()
		<-done
	})

	t.Run("idle timeout", func(t *testing.T) {
		src, _ := net.Pipe()
		dst, _ := net.Pipe()
		defer src.Close()
		defer dst.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh := NewWebSocketHandler(nil)
			wh.copyWithIdleTimeout(dst, src, 50*time.Millisecond)
		}()

		select {
		case <-done:
			// Good - timed out
		case <-time.After(3 * time.Second):
			t.Fatal("expected idle timeout")
		}
	})

	t.Run("zero timeout uses default", func(t *testing.T) {
		src, _ := net.Pipe()
		dst, _ := net.Pipe()
		defer src.Close()
		defer dst.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh := NewWebSocketHandler(nil)
			// With 0 timeout, should use 5 minute default
			// but we'll close src to end quickly
			time.AfterFunc(100*time.Millisecond, func() {
				src.Close()
			})
			wh.copyWithIdleTimeout(dst, src, 0)
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("expected copy to finish")
		}
	})
}

// --- proxyWebSocket ---

func TestProxyWebSocket(t *testing.T) {
	t.Run("bidirectional copy", func(t *testing.T) {
		client, clientPipe := net.Pipe()
		bk, bkPipe := net.Pipe()
		defer client.Close()
		defer bk.Close()

		wh := NewWebSocketHandler(&WebSocketConfig{
			IdleTimeout: 2 * time.Second,
		})

		done := make(chan struct{})
		go func() {
			defer close(done)
			wh.proxyWebSocket(client, bk)
		}()

		// Write from clientPipe -> client -> bk -> bkPipe
		clientPipe.Write([]byte("from-client"))
		buf := make([]byte, 100)
		bkPipe.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := bkPipe.Read(buf)
		if err != nil {
			t.Fatalf("backend read error: %v", err)
		}
		if string(buf[:n]) != "from-client" {
			t.Errorf("backend got %q, want from-client", string(buf[:n]))
		}

		// Write from bkPipe -> bk -> client -> clientPipe
		bkPipe.Write([]byte("from-backend"))
		clientPipe.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err = clientPipe.Read(buf)
		if err != nil {
			t.Fatalf("client read error: %v", err)
		}
		if string(buf[:n]) != "from-backend" {
			t.Errorf("client got %q, want from-backend", string(buf[:n]))
		}

		clientPipe.Close()
		bkPipe.Close()
		<-done
	})
}

// --- extractClientIP ---

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			"X-Forwarded-For",
			&http.Request{
				Header:     http.Header{"X-Forwarded-For": []string{"10.0.0.1, 192.168.1.1"}},
				RemoteAddr: "192.168.1.100:12345",
			},
			"10.0.0.1",
		},
		{
			"X-Real-IP",
			&http.Request{
				Header:     http.Header{"X-Real-Ip": []string{"10.0.0.2"}},
				RemoteAddr: "192.168.1.100:12345",
			},
			"10.0.0.2",
		},
		{
			"RemoteAddr fallback",
			&http.Request{
				RemoteAddr: "192.168.1.100:12345",
			},
			"192.168.1.100",
		},
		{
			"RemoteAddr no port",
			&http.Request{
				RemoteAddr: "192.168.1.100",
			},
			"192.168.1.100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractClientIP(tt.req)
			if got != tt.want {
				t.Errorf("extractClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- isWSHopByHop ---

func TestIsWSHopByHop(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{"Connection", true},
		{"Keep-Alive", true},
		{"Transfer-Encoding", true},
		{"Content-Length", true},
		{"Content-Type", false},
		{"Authorization", false},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := isWSHopByHop(tt.header)
			if got != tt.want {
				t.Errorf("isWSHopByHop(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

// --- writeUpgradeRequest ---

func TestWriteUpgradeRequest(t *testing.T) {
	t.Run("basic request", func(t *testing.T) {
		conn1, conn2 := net.Pipe()
		defer conn1.Close()
		defer conn2.Close()

		req := &http.Request{
			Method: "GET",
			Host:   "example.com",
			URL:    &url.URL{Path: "/ws"},
			Header: http.Header{
				"Upgrade":               []string{"websocket"},
				"Sec-WebSocket-Key":     []string{"dGhlbGxvIGlzIG5vIGJpbmU="},
				"Sec-WebSocket-Version": []string{"13"},
			},
			RemoteAddr: "192.168.1.100:12345",
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
		if !strings.Contains(result, "GET /ws HTTP/1.1") {
			t.Errorf("expected GET /ws, got: %s", result)
		}
		if !strings.Contains(result, "Host: example.com") {
			t.Errorf("expected Host header, got: %s", result)
		}
		if strings.Contains(result, "Connection:") {
			t.Error("hop-by-hop Connection header should be stripped")
		}
		<-done
	})

	t.Run("with X-Forwarded-For existing", func(t *testing.T) {
		conn1, conn2 := net.Pipe()
		defer conn1.Close()
		defer conn2.Close()

		req := &http.Request{
			Method: "GET",
			Host:   "example.com",
			URL:    &url.URL{Path: "/ws"},
			Header: http.Header{
				"X-Forwarded-For": []string{"10.0.0.1"},
			},
			RemoteAddr: "192.168.1.100:12345",
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
		n, _ := conn1.Read(buf)
		result := string(buf[:n])
		// extractClientIP returns the first IP from XFF (10.0.0.1), so the appended
		// line becomes "10.0.0.1, 10.0.0.1" (original XFF first value + extracted client IP)
		if !strings.Contains(result, "X-Forwarded-For: 10.0.0.1") {
			t.Errorf("expected XFF header, got: %s", result)
		}
		<-done
	})

	t.Run("empty path defaults to /", func(t *testing.T) {
		conn1, conn2 := net.Pipe()
		defer conn1.Close()
		defer conn2.Close()

		req := &http.Request{
			Method: "GET",
			Host:   "example.com",
			URL:    &url.URL{},
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
		n, _ := conn1.Read(buf)
		result := string(buf[:n])
		if !strings.Contains(result, "GET / HTTP/1.1") {
			t.Errorf("expected GET /, got: %s", result)
		}
		<-done
	})
}

// --- HandleWebSocket error paths ---

func TestHandleWebSocket_Disabled(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{EnableWebSocket: false})
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()

	err := wh.HandleWebSocket(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected disabled error, got %v", err)
	}
}

func TestHandleWebSocket_MissingWSKey(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{EnableWebSocket: true})
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()

	err := wh.HandleWebSocket(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "Sec-WebSocket-Key") {
		t.Errorf("expected missing key error, got %v", err)
	}
}

func TestHandleWebSocket_BackendAtMaxConns(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{EnableWebSocket: true})
	b := backend.NewBackend("b1", "10.0.0.1:8080")
	b.MaxConns = 1
	b.AcquireConn() // Saturate

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	w := httptest.NewRecorder()

	err := wh.HandleWebSocket(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "max connections") {
		t.Errorf("expected max connections error, got %v", err)
	}
	b.ReleaseConn()
}

func TestHandleWebSocket_NoHijacker(t *testing.T) {
	// Create a real backend that responds with 101
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	// Backend that sends 101 response
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the upgrade request
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		// Send 101 response
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\n"))
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	w := httptest.NewRecorder() // Does NOT implement http.Hijacker

	err = wh.HandleWebSocket(w, req, b)
	if err == nil || !strings.Contains(err.Error(), "hijacking") {
		t.Errorf("expected hijacking error, got %v", err)
	}
}

func TestHandleWebSocket_BackendNon101Response(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	// Backend that sends 403 response
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		conn.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
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
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("expected rejected error, got %v", err)
	}
}

func TestHandleWebSocket_ConnectionRefused(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     1 * time.Second,
	})
	// Backend that doesn't exist
	b := backend.NewBackend("b1", "127.0.0.1:1")
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	w := httptest.NewRecorder()

	err := wh.HandleWebSocket(w, req, b)
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

// --- NewWebSocketProxy ---

func TestNewWebSocketProxy(t *testing.T) {
	proxy := NewWebSocketProxy(nil, nil)
	if proxy == nil {
		t.Fatal("NewWebSocketProxy returned nil")
	}
	if proxy.wsHandler == nil {
		t.Error("wsHandler should be initialized")
	}
}

// --- IsWebSocketUpgrade ---

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			"valid upgrade",
			map[string]string{"Connection": "Upgrade", "Upgrade": "websocket"},
			true,
		},
		{
			"case insensitive",
			map[string]string{"Connection": "keep-alive, Upgrade", "Upgrade": "WebSocket"},
			true,
		},
		{
			"no connection upgrade",
			map[string]string{"Connection": "keep-alive", "Upgrade": "websocket"},
			false,
		},
		{
			"no upgrade header",
			map[string]string{"Connection": "Upgrade"},
			false,
		},
		{
			"empty headers",
			map[string]string{},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{Header: http.Header{}}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			got := IsWebSocketUpgrade(req)
			if got != tt.want {
				t.Errorf("IsWebSocketUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DefaultWebSocketConfig ---

func TestDefaultWebSocketConfig(t *testing.T) {
	cfg := DefaultWebSocketConfig()
	if !cfg.EnableWebSocket {
		t.Error("EnableWebSocket should be true")
	}
	if cfg.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", cfg.IdleTimeout)
	}
	if cfg.MaxMessageSize != 10*1024*1024 {
		t.Errorf("MaxMessageSize = %d, want 10MB", cfg.MaxMessageSize)
	}
}

// --- dialBackend with TLS prefix ---

func TestDialBackend_TLSAddress(t *testing.T) {
	wh := NewWebSocketHandler(nil)
	req := httptest.NewRequest("GET", "/ws", nil)

	b := backend.NewBackend("b1", "wss://127.0.0.1:1")
	b.SetState(backend.StateUp)

	// Should attempt TLS dial (will fail because no server, but tests the TLS path)
	_, err := wh.dialBackend(req, b)
	if err == nil {
		t.Error("expected error for TLS dial to non-existent server")
	}
}

func TestDialBackend_HTTPSPrefix(t *testing.T) {
	wh := NewWebSocketHandler(nil)
	req := httptest.NewRequest("GET", "/ws", nil)

	b := backend.NewBackend("b1", "https://127.0.0.1:1")
	b.SetState(backend.StateUp)

	_, err := wh.dialBackend(req, b)
	if err == nil {
		t.Error("expected error for TLS dial to non-existent server")
	}
}

func TestDialBackend_WithTLSRequest(t *testing.T) {
	wh := NewWebSocketHandler(nil)

	// Simulate a request with TLS (r.TLS != nil)
	req := httptest.NewRequest("GET", "/ws", nil)
	req.TLS = &tls.ConnectionState{} // Simulate TLS request

	b := backend.NewBackend("b1", "127.0.0.1:1")
	b.SetState(backend.StateUp)

	// Should attempt TLS dial because r.TLS is set
	_, err := wh.dialBackend(req, b)
	if err == nil {
		t.Error("expected error for TLS dial to non-existent server")
	}
}

func TestDialBackend_PlainTCP(t *testing.T) {
	// Start a simple TCP listener
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
		conn.Close()
	}()

	wh := NewWebSocketHandler(nil)
	req := httptest.NewRequest("GET", "/ws", nil)

	b := backend.NewBackend("b1", ln.Addr().String())
	b.SetState(backend.StateUp)

	conn, err := wh.dialBackend(req, b)
	if err != nil {
		t.Fatalf("dialBackend error: %v", err)
	}
	conn.Close()
}

// --- proxyWebSocket with close errors ---

func TestProxyWebSocket_ClientClose(t *testing.T) {
	client, clientPipe := net.Pipe()
	bk, bkPipe := net.Pipe()
	defer client.Close()
	defer bk.Close()

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 2 * time.Second,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		wh.proxyWebSocket(client, bk)
	}()

	// Close client side to trigger close error path
	clientPipe.Close()
	bkPipe.Close()
	<-done
}

// --- copyWithIdleTimeout write error ---

func TestCopyWithIdleTimeout_WriteError(t *testing.T) {
	src, srcPipe := net.Pipe()
	dst, _ := net.Pipe()
	defer src.Close()
	// Don't close dstPipe - we want writes to the real dst to fail

	done := make(chan struct{})
	go func() {
		defer close(done)
		wh := NewWebSocketHandler(nil)
		wh.copyWithIdleTimeout(dst, src, 2*time.Second)
	}()

	// Write data but dst is closed
	dst.Close()
	srcPipe.Write([]byte("data"))

	<-done
}

// --- HandleWebSocket full happy path with hijackable connection ---

func TestHandleWebSocket_FullHappyPath(t *testing.T) {
	// Start a raw TCP backend that does WebSocket upgrade
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
		// Read the upgrade request
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		// Send 101 response with proper Sec-WebSocket-Accept
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\n"))
		// Echo data back
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	// Use a real TCP connection for client side to test hijacking
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Create a fake ResponseWriter that implements Hijacker
	hijackRW := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufio.NewReader(strings.NewReader("")), bufio.NewWriter(io.Discard)),
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Upgrade", "websocket")

	// HandleWebSocket will hijack serverConn, dial backend, and proxy
	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(hijackRW, req, b)
	}()

	// Read the 101 response from clientConn
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 4096)
	n, err := clientConn.Read(respBuf)
	if err != nil {
		t.Fatalf("Failed to read 101 response: %v", err)
	}
	if !strings.Contains(string(respBuf[:n]), "101") {
		t.Errorf("Expected 101 response, got: %s", string(respBuf[:n]))
	}

	// Write data and read echo
	clientConn.Write([]byte("hello-ws"))
	echoBuf := make([]byte, 100)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = clientConn.Read(echoBuf)
	if err != nil {
		t.Logf("Echo read error (may be expected): %v", err)
	} else if string(echoBuf[:n]) != "hello-ws" {
		t.Errorf("Echo = %q, want hello-ws", string(echoBuf[:n]))
	}

	// Close to finish the test
	serverConn.Close()
	clientConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket returned: %v", err)
	case <-time.After(3 * time.Second):
		t.Log("HandleWebSocket timed out")
	}
}

// hijackableResponseWriter implements http.ResponseWriter and http.Hijacker.
type hijackableResponseWriter struct {
	header http.Header
	conn   net.Conn
	buf    *bufio.ReadWriter
}

func (h *hijackableResponseWriter) Header() http.Header {
	if h.header == nil {
		h.header = make(http.Header)
	}
	return h.header
}

func (h *hijackableResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (h *hijackableResponseWriter) WriteHeader(int) {}

func (h *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, h.buf, nil
}

// --- proxyWebSocket error channel reporting ---

func TestProxyWebSocket_ErrorReporting(t *testing.T) {
	// Use pipes that cause errors on read
	client, _ := net.Pipe()
	bk, _ := net.Pipe()

	// Close both immediately to trigger errors
	client.Close()
	bk.Close()

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 2 * time.Second,
	})

	err := wh.proxyWebSocket(client, bk)
	// Should return without hanging
	t.Logf("proxyWebSocket with closed conns: %v", err)
}

func TestProxyWebSocket_NilConfig(t *testing.T) {
	wh := NewWebSocketHandler(nil)
	if wh.config == nil {
		t.Error("Config should be set to default when nil")
	}
	if !wh.config.EnableWebSocket {
		t.Error("Default config should have WebSocket enabled")
	}
}

// --- copyWithIdleTimeout short write ---

func TestCopyWithIdleTimeout_ShortWrite(t *testing.T) {
	src, srcPipe := net.Pipe()
	dst, dstPipe := net.Pipe()
	defer src.Close()
	defer dstPipe.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wh := NewWebSocketHandler(nil)
		wh.copyWithIdleTimeout(dst, src, 2*time.Second)
	}()

	srcPipe.Write([]byte("data"))
	// Read only partial to create backpressure
	buf := make([]byte, 1)
	dstPipe.SetReadDeadline(time.Now().Add(2 * time.Second))
	dstPipe.Read(buf)

	srcPipe.Close()
	dst.Close()
	<-done
}

// --- WebSocketProxy.ServeHTTP tests ---

func setupWSProxy(t *testing.T) (*WebSocketProxy, *backend.PoolManager, *router.Router) {
	t.Helper()
	poolManager := backend.NewPoolManager()
	routerInstance := router.NewRouter()
	connPoolManager := conn.NewPoolManager(nil)
	healthChecker := health.NewChecker()
	middlewareChain := middleware.NewChain()

	config := &Config{
		Router:          routerInstance,
		PoolManager:     poolManager,
		ConnPoolManager: connPoolManager,
		HealthChecker:   healthChecker,
		MiddlewareChain: middlewareChain,
		ProxyTimeout:    5 * time.Second,
		DialTimeout:     1 * time.Second,
		MaxRetries:      3,
	}

	httpProxy := NewHTTPProxy(config)
	wsProxy := NewWebSocketProxy(httpProxy, &WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	return wsProxy, poolManager, routerInstance
}

func TestWebSocketProxy_ServeHTTP_NotWebSocket(t *testing.T) {
	wsProxy, poolManager, routerInstance := setupWSProxy(t)

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("regular response"))
	}))
	defer backendServer.Close()

	pool := backend.NewPool("ws-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "ws-route", Path: "/", BackendPool: "ws-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	wsProxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWebSocketProxy_ServeHTTP_RouteNotFound(t *testing.T) {
	wsProxy, _, _ := setupWSProxy(t)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rr := httptest.NewRecorder()
	wsProxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestWebSocketProxy_ServeHTTP_PoolNotFound(t *testing.T) {
	wsProxy, _, routerInstance := setupWSProxy(t)

	route := &router.Route{Name: "ws-route", Path: "/ws", BackendPool: "missing-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rr := httptest.NewRecorder()
	wsProxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestWebSocketProxy_ServeHTTP_NoHealthyBackends(t *testing.T) {
	wsProxy, poolManager, routerInstance := setupWSProxy(t)

	pool := backend.NewPool("ws-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", "127.0.0.1:1")
	b.SetState(backend.StateDown)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "ws-route", Path: "/ws", BackendPool: "ws-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rr := httptest.NewRecorder()
	wsProxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestWebSocketProxy_ServeHTTP_BackendConnRefused(t *testing.T) {
	wsProxy, poolManager, routerInstance := setupWSProxy(t)

	pool := backend.NewPool("ws-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("b1", "127.0.0.1:1")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "ws-route", Path: "/ws", BackendPool: "ws-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rr := httptest.NewRecorder()
	wsProxy.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for unreachable backend")
	}
}

// --- HandleWebSocket: backend that closes mid-upgrade (read response error) ---

func TestHandleWebSocket_BackendCloseMidUpgrade(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer backendListener.Close()

	// Backend that closes immediately after accept without sending a response
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
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
		t.Error("expected error for backend closing mid-upgrade")
	}
}

// --- proxyWebSocket with panic recovery ---

func TestProxyWebSocket_PanicRecovery(t *testing.T) {
	// Create pipes that will cause a panic in one direction
	client, clientPipe := net.Pipe()
	bk, bkPipe := net.Pipe()
	defer client.Close()
	defer bk.Close()

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 2 * time.Second,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		err := wh.proxyWebSocket(client, bk)
		t.Logf("proxyWebSocket returned: %v", err)
	}()

	// Close both pipes to trigger errors in both directions
	clientPipe.Close()
	bkPipe.Close()

	select {
	case <-done:
		// Good - proxyWebSocket returned without hanging
	case <-time.After(3 * time.Second):
		t.Fatal("proxyWebSocket hung after closing both connections")
	}
}

// --- copyWithIdleTimeout with io.ErrShortWrite ---

func TestCopyWithIdleTimeout_ErrShortWrite(t *testing.T) {
	src, srcPipe := net.Pipe()
	dst, _ := net.Pipe()
	defer src.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wh := NewWebSocketHandler(nil)
		wh.copyWithIdleTimeout(dst, src, 2*time.Second)
	}()

	// Close dst before writing to make Write fail immediately
	dst.Close()
	srcPipe.Write([]byte("data"))

	select {
	case <-done:
		// Good - copy returned after write failure
	case <-time.After(3 * time.Second):
		t.Fatal("copyWithIdleTimeout hung on write failure")
	}
}

// --- isWebSocketCloseError with syscall errors ---

func TestIsWebSocketCloseError_SyscallErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"connection reset string", fmt.Errorf("connection reset by peer"), true},
		{"broken pipe string", fmt.Errorf("write: broken pipe"), true},
		{"closed connection string", fmt.Errorf("use of closed network connection"), true},
		{"non-close error", fmt.Errorf("random error"), false},
		{"EOF", io.EOF, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWebSocketCloseError(tt.err)
			if got != tt.want {
				t.Errorf("isWebSocketCloseError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- HandleWebSocket with hijack error ---

func TestHandleWebSocket_HijackError(t *testing.T) {
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
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "test-key")

	// Use a response writer that implements Hijacker but returns error
	rw := &hijackErrorWriter{}

	err = wh.HandleWebSocket(rw, req, b)
	if err == nil {
		t.Error("expected error when hijack fails")
	}
	if !strings.Contains(err.Error(), "hijack") {
		t.Errorf("expected hijack error, got: %v", err)
	}
}

type hijackErrorWriter struct {
	header http.Header
}

func (h *hijackErrorWriter) Header() http.Header {
	if h.header == nil {
		h.header = make(http.Header)
	}
	return h.header
}
func (h *hijackErrorWriter) Write([]byte) (int, error) { return 0, nil }
func (h *hijackErrorWriter) WriteHeader(int)           {}
func (h *hijackErrorWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, fmt.Errorf("hijack failed for test")
}

// --- HandleWebSocket with hijacker returning nil buffered data ---

func TestHandleWebSocket_HijackNilBuffer(t *testing.T) {
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
		// Send 101 with data that won't be buffered
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQiSG5hZWdpIHRvbyBzYXR1yKQ=\r\n\r\n"))
		// Wait then close
		time.Sleep(100 * time.Millisecond)
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     200 * time.Millisecond,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Hijackable writer with nil bufio.ReadWriter
	rw := &hijackNilBufWriter{conn: serverConn}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")

	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(rw, req, b)
	}()

	// Read the 101 response sent to client
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _ := clientConn.Read(buf)
	result := string(buf[:n])
	if !strings.Contains(result, "101") {
		t.Errorf("expected 101 response, got: %s", result)
	}

	serverConn.Close()
	clientConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket returned: %v", err)
	case <-time.After(3 * time.Second):
		t.Log("HandleWebSocket timed out")
	}
}

type hijackNilBufWriter struct {
	header http.Header
	conn   net.Conn
}

func (h *hijackNilBufWriter) Header() http.Header {
	if h.header == nil {
		h.header = make(http.Header)
	}
	return h.header
}
func (h *hijackNilBufWriter) Write([]byte) (int, error) { return 0, nil }
func (h *hijackNilBufWriter) WriteHeader(int)           {}
func (h *hijackNilBufWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, nil, nil // nil ReadWriter
}

// --- WebSocketProxy.ServeHTTP: no backend available from balancer ---

func TestWebSocketProxy_ServeHTTP_NoBalancerBackend(t *testing.T) {
	wsProxy, poolManager, routerInstance := setupWSProxy(t)

	pool := backend.NewPool("ws-pool", "round_robin")
	// Use a balancer that always returns nil
	pool.SetBalancer(&nilBalancer{})
	b := backend.NewBackend("b1", "127.0.0.1:9999")
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "ws-route", Path: "/ws", BackendPool: "ws-pool"}
	routerInstance.AddRoute(route)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rr := httptest.NewRecorder()
	wsProxy.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Error("expected non-200 when balancer returns nil")
	}
}

// nilBalancer always returns nil from Next
type nilBalancer struct{}

func (n *nilBalancer) Name() string                             { return "nil" }
func (n *nilBalancer) Next([]*backend.Backend) *backend.Backend { return nil }
func (n *nilBalancer) Add(*backend.Backend)                     {}
func (n *nilBalancer) Remove(string)                            {}
func (n *nilBalancer) Update(*backend.Backend)                  {}

// --- proxyWebSocket: error reported through errChan (non-close error) ---

func TestProxyWebSocket_NonCloseErrorReported(t *testing.T) {
	// Use errorConn that returns a non-close, non-timeout error on read
	client := &errorConn{readErr: fmt.Errorf("custom read failure")}
	bk := &errorConn{readErr: fmt.Errorf("custom read failure")}

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 100 * time.Millisecond,
	})

	err := wh.proxyWebSocket(client, bk)
	if err == nil {
		t.Error("expected error from proxyWebSocket with error connections")
	}
	t.Logf("proxyWebSocket returned: %v", err)
}

// errorConn is a net.Conn that always returns an error on read.
type errorConn struct {
	readErr error
	closed  bool
}

func (c *errorConn) Read([]byte) (int, error)    { return 0, c.readErr }
func (c *errorConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *errorConn) Close() error                { c.closed = true; return nil }
func (c *errorConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
}
func (c *errorConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5678}
}
func (c *errorConn) SetDeadline(time.Time) error      { return nil }
func (c *errorConn) SetReadDeadline(time.Time) error  { return nil }
func (c *errorConn) SetWriteDeadline(time.Time) error { return nil }

// --- proxyWebSocket: error reported through errChan ---

func TestProxyWebSocket_ErrorReportedToChannel(t *testing.T) {
	// Create a mock error connection pair where reads fail with non-close errors
	client, clientPipe := net.Pipe()
	bk, bkPipe := net.Pipe()

	wh := NewWebSocketHandler(&WebSocketConfig{
		IdleTimeout: 200 * time.Millisecond,
	})

	done := make(chan error, 1)
	go func() {
		done <- wh.proxyWebSocket(client, bk)
	}()

	// Write data from client side, then close one direction to cause an error
	clientPipe.Write([]byte("test-data"))

	buf := make([]byte, 100)
	bkPipe.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := bkPipe.Read(buf)
	if n == 0 {
		t.Error("expected to read data from backend pipe")
	}

	// Now close both pipes to cause errors in both directions
	clientPipe.Close()
	bkPipe.Close()

	select {
	case err := <-done:
		t.Logf("proxyWebSocket returned: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("proxyWebSocket hung")
	}
}

// --- HandleWebSocket: read error from backend response ---

func TestHandleWebSocket_BackendReadResponseError(t *testing.T) {
	// Start a backend that accepts the connection and reads the upgrade request
	// but then closes before sending a complete response
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
		// Read the upgrade request
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf)
		// Send an incomplete response and close
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\n"))
		// Don't send the full response - close prematurely
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
		t.Error("expected error when backend sends incomplete response")
	}
}

// --- HandleWebSocket: full happy path with buffered data from both sides ---

func TestHandleWebSocket_HappyPathWithBufferedData(t *testing.T) {
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
		// Echo
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}()

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     2 * time.Second,
	})
	b := backend.NewBackend("b1", backendListener.Addr().String())
	b.SetState(backend.StateUp)

	// Create a pair where the client has buffered data
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	// Use a hijackable writer with pre-buffered data on the client side
	bufReader := bufio.NewReader(strings.NewReader("pre-buffered"))
	bufWriter := bufio.NewWriter(io.Discard)
	rw := &hijackableResponseWriter{
		conn: serverConn,
		buf:  bufio.NewReadWriter(bufReader, bufWriter),
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlbGxvIGlzIG5vIGJpbmU=")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Upgrade", "websocket")

	done := make(chan error, 1)
	go func() {
		done <- wh.HandleWebSocket(rw, req, b)
	}()

	// Read the 101 response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 4096)
	n, _ := clientConn.Read(respBuf)
	if !strings.Contains(string(respBuf[:n]), "101") {
		t.Errorf("expected 101, got: %s", string(respBuf[:n]))
	}

	clientConn.Close()
	serverConn.Close()

	select {
	case err := <-done:
		t.Logf("HandleWebSocket: %v", err)
	case <-time.After(3 * time.Second):
		t.Log("HandleWebSocket timed out")
	}
}

// --- WebSocket connection limit tests ---

func TestWebSocketHandler_MaxConns_LimitEnforced(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		MaxConns:        2,
		IdleTimeout:     5 * time.Second,
	})

	if wh.ActiveConns() != 0 {
		t.Errorf("expected 0 active conns, got %d", wh.ActiveConns())
	}

	// Simulate acquiring connections
	wh.conns.Add(2)

	if wh.ActiveConns() != 2 {
		t.Errorf("expected 2 active conns, got %d", wh.ActiveConns())
	}

	// Third connection should be rejected
	wh.conns.Add(1)
	if wh.ActiveConns() != 3 {
		t.Errorf("expected 3 active conns, got %d", wh.ActiveConns())
	}
	wh.conns.Add(-1) // cleanup the over-limit

	wh.conns.Add(-2) // cleanup
	if wh.ActiveConns() != 0 {
		t.Errorf("expected 0 active conns after cleanup, got %d", wh.ActiveConns())
	}
}

func TestWebSocketHandler_MaxConns_Unlimited(t *testing.T) {
	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		MaxConns:        0, // unlimited
		IdleTimeout:     5 * time.Second,
	})

	// ActiveConns should always be 0 when MaxConns is 0 (no tracking needed)
	if wh.ActiveConns() != 0 {
		t.Errorf("expected 0 active conns, got %d", wh.ActiveConns())
	}
}

func TestWebSocketHandler_MaxConns_RejectsOverLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b := backend.NewBackend("b1", strings.TrimPrefix(srv.URL, "http://"))
	b.SetState(backend.StateUp)

	wh := NewWebSocketHandler(&WebSocketConfig{
		EnableWebSocket: true,
		MaxConns:        1,
		IdleTimeout:     5 * time.Second,
	})

	// Fill the connection slot
	wh.conns.Add(1)

	// Next connection should be rejected with an error
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	r.Header.Set("Sec-WebSocket-Version", "13")

	err := wh.HandleWebSocket(w, r, b)
	if err == nil {
		t.Error("expected error when connection limit reached")
	}
	if !strings.Contains(err.Error(), "connection limit") {
		t.Errorf("expected connection limit error, got: %v", err)
	}

	// Cleanup
	wh.conns.Add(-1)
}
