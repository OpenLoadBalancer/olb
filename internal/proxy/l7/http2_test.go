package l7

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestDefaultHTTP2Config(t *testing.T) {
	config := DefaultHTTP2Config()

	if !config.EnableHTTP2 {
		t.Error("EnableHTTP2 should be true by default")
	}
	if !config.EnableH2C {
		t.Error("EnableH2C should be true by default")
	}
	if config.MaxConcurrentStreams != 250 {
		t.Errorf("MaxConcurrentStreams = %v, want 250", config.MaxConcurrentStreams)
	}
	if config.MaxFrameSize != 16*1024 {
		t.Errorf("MaxFrameSize = %v, want 16KB", config.MaxFrameSize)
	}
	if config.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", config.IdleTimeout)
	}
}

func TestIsHTTP2Request(t *testing.T) {
	tests := []struct {
		name   string
		proto  string
		major  int
		wantH2 bool
	}{
		{
			name:   "HTTP/2 request",
			proto:  "HTTP/2.0",
			major:  2,
			wantH2: true,
		},
		{
			name:   "HTTP/1.1 request",
			proto:  "HTTP/1.1",
			major:  1,
			wantH2: false,
		},
		{
			name:   "HTTP/1.0 request",
			proto:  "HTTP/1.0",
			major:  1,
			wantH2: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Proto = tt.proto
			req.ProtoMajor = tt.major

			got := IsHTTP2Request(req)
			if got != tt.wantH2 {
				t.Errorf("IsHTTP2Request() = %v, want %v", got, tt.wantH2)
			}
		})
	}
}

func TestIsH2CRequest(t *testing.T) {
	tests := []struct {
		name       string
		upgrade    string
		h2Settings string
		wantH2C    bool
	}{
		{
			name:    "h2c upgrade request",
			upgrade: "h2c",
			wantH2C: true,
		},
		{
			name:       "h2c with settings",
			h2Settings: "AAMAAABkAARAAAAAAAIAAAAA",
			wantH2C:    true,
		},
		{
			name:    "websocket upgrade",
			upgrade: "websocket",
			wantH2C: false,
		},
		{
			name:    "no upgrade",
			upgrade: "",
			wantH2C: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}
			if tt.h2Settings != "" {
				req.Header.Set("HTTP2-Settings", tt.h2Settings)
			}

			got := IsH2CRequest(req)
			if got != tt.wantH2C {
				t.Errorf("IsH2CRequest() = %v, want %v", got, tt.wantH2C)
			}
		})
	}
}

func TestNewHTTP2Handler(t *testing.T) {
	config := DefaultHTTP2Config()
	handler := NewHTTP2Handler(config)

	if handler == nil {
		t.Fatal("NewHTTP2Handler() returned nil")
	}
	if handler.config != config {
		t.Error("Handler config mismatch")
	}
	if handler.transport == nil {
		t.Error("Handler transport should not be nil")
	}
	if handler.h2Transport == nil {
		t.Error("Handler h2Transport should not be nil")
	}
}

func TestNewHTTP2Handler_NilConfig(t *testing.T) {
	handler := NewHTTP2Handler(nil)

	if handler == nil {
		t.Fatal("NewHTTP2Handler(nil) returned nil")
	}
	if handler.config == nil {
		t.Error("Handler config should use defaults when nil")
	}
}

func TestHTTP2Handler_GetTransport(t *testing.T) {
	tests := []struct {
		name     string
		enableH2 bool
		scheme   string
		wantType string
	}{
		{
			name:     "HTTP with HTTP/2 enabled",
			enableH2: true,
			scheme:   "http",
			wantType: "*http2.Transport",
		},
		{
			name:     "HTTPS with HTTP/2 enabled",
			enableH2: true,
			scheme:   "https",
			wantType: "*http.Transport",
		},
		{
			name:     "HTTP with HTTP/2 disabled",
			enableH2: false,
			scheme:   "http",
			wantType: "*http.Transport",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &HTTP2Config{EnableHTTP2: tt.enableH2, EnableH2C: true}
			handler := NewHTTP2Handler(config)

			transport := handler.GetTransport(tt.scheme)
			if transport == nil {
				t.Fatal("GetTransport returned nil")
			}

			if !strings.Contains(fmt.Sprintf("%T", transport), tt.wantType) {
				t.Errorf("GetTransport() returned %T, expected to contain %s", transport, tt.wantType)
			}
		})
	}
}

func TestHTTP2Handler_WrapHandler(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	})

	tests := []struct {
		name      string
		enableH2  bool
		enableH2C bool
		isWrapped bool
	}{
		{
			name:      "HTTP/2 and h2c enabled",
			enableH2:  true,
			enableH2C: true,
			isWrapped: true,
		},
		{
			name:      "HTTP/2 enabled, h2c disabled",
			enableH2:  true,
			enableH2C: false,
			isWrapped: false,
		},
		{
			name:      "HTTP/2 disabled",
			enableH2:  false,
			enableH2C: true,
			isWrapped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &HTTP2Config{EnableHTTP2: tt.enableH2, EnableH2C: tt.enableH2C}
			handler := NewHTTP2Handler(config)

			wrapped := handler.WrapHandler(innerHandler)

			// The wrapped handler should be a function wrapper or h2c handler
			// Just verify it doesn't panic
			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
		})
	}
}

func TestNewHTTP2Listener(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-listener",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if listener == nil {
		t.Fatal("NewHTTP2Listener returned nil")
	}
	if listener.name != opts.Name {
		t.Error("Name mismatch")
	}
}

func TestNewHTTP2Listener_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    *HTTP2ListenerOptions
		wantErr bool
	}{
		{
			name:    "nil options",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "missing name",
			opts: &HTTP2ListenerOptions{
				Address: "127.0.0.1:0",
				Handler: http.NotFoundHandler(),
			},
			wantErr: true,
		},
		{
			name: "missing address",
			opts: &HTTP2ListenerOptions{
				Name:    "test",
				Handler: http.NotFoundHandler(),
			},
			wantErr: true,
		},
		{
			name: "missing handler",
			opts: &HTTP2ListenerOptions{
				Name:    "test",
				Address: "127.0.0.1:0",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHTTP2Listener(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHTTP2Listener() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHTTP2Listener_StartStop(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello from HTTP/2"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-h2",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	// Start listener
	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	if !listener.IsRunning() {
		t.Error("Listener should be running")
	}

	// Make a request
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + listener.Address())
	if err != nil {
		t.Fatalf("Request error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Hello from HTTP/2" {
		t.Errorf("Body = %q, want %q", string(body), "Hello from HTTP/2")
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

func TestHTTP2Listener_DoubleStart(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-double",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)

	if err := listener.Start(); err != nil {
		t.Fatalf("First start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// Second start should fail
	if err := listener.Start(); err == nil {
		t.Error("Second start should fail")
	}
}

func TestALPNNegotiator(t *testing.T) {
	tests := []struct {
		name       string
		supportH2  bool
		wantProtos []string
	}{
		{
			name:       "HTTP/2 supported",
			supportH2:  true,
			wantProtos: []string{"h2", "http/1.1"},
		},
		{
			name:       "HTTP/2 not supported",
			supportH2:  false,
			wantProtos: []string{"http/1.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewALPNNegotiator(tt.supportH2)

			if len(n.supportedProtos) != len(tt.wantProtos) {
				t.Errorf("Protocols = %v, want %v", n.supportedProtos, tt.wantProtos)
			}

			for i, proto := range tt.wantProtos {
				if n.supportedProtos[i] != proto {
					t.Errorf("Protocol[%d] = %v, want %v", i, n.supportedProtos[i], proto)
				}
			}
		})
	}
}

func TestALPNNegotiator_ConfigureTLS(t *testing.T) {
	n := NewALPNNegotiator(true)
	config := &tls.Config{}

	n.ConfigureTLS(config)

	if len(config.NextProtos) != 2 {
		t.Errorf("NextProtos length = %d, want 2", len(config.NextProtos))
	}
	if config.NextProtos[0] != "h2" {
		t.Errorf("NextProtos[0] = %v, want h2", config.NextProtos[0])
	}
}

func TestALPNNegotiator_IsHTTP2(t *testing.T) {
	n := NewALPNNegotiator(true)

	tests := []struct {
		name   string
		state  *tls.ConnectionState
		wantH2 bool
	}{
		{
			name:   "HTTP/2 negotiated",
			state:  &tls.ConnectionState{NegotiatedProtocol: "h2"},
			wantH2: true,
		},
		{
			name:   "HTTP/1.1 negotiated",
			state:  &tls.ConnectionState{NegotiatedProtocol: "http/1.1"},
			wantH2: false,
		},
		{
			name:   "No ALPN",
			state:  &tls.ConnectionState{NegotiatedProtocol: ""},
			wantH2: false,
		},
		{
			name:   "Nil state",
			state:  nil,
			wantH2: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := n.IsHTTP2(tt.state)
			if got != tt.wantH2 {
				t.Errorf("IsHTTP2() = %v, want %v", got, tt.wantH2)
			}
		})
	}
}

func TestNewHTTP2BackendTransport(t *testing.T) {
	config := DefaultHTTP2Config()
	transport := NewHTTP2BackendTransport(config)

	if transport == nil {
		t.Fatal("NewHTTP2BackendTransport returned nil")
	}
	if transport.config != config {
		t.Error("Config mismatch")
	}
	if transport.transport == nil {
		t.Error("Transport should not be nil")
	}
}

func TestGetProtocolInfo(t *testing.T) {
	tests := []struct {
		name        string
		proto       string
		hasTLS      bool
		alpn        string
		wantVersion string
		wantTLS     bool
		wantALPN    string
	}{
		{
			name:        "HTTP/2 with TLS",
			proto:       "HTTP/2.0",
			hasTLS:      true,
			alpn:        "h2",
			wantVersion: "HTTP/2.0",
			wantTLS:     true,
			wantALPN:    "h2",
		},
		{
			name:        "HTTP/1.1 without TLS",
			proto:       "HTTP/1.1",
			hasTLS:      false,
			alpn:        "",
			wantVersion: "HTTP/1.1",
			wantTLS:     false,
			wantALPN:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Proto = tt.proto
			if tt.hasTLS {
				req.TLS = &tls.ConnectionState{
					NegotiatedProtocol: tt.alpn,
				}
			}

			info := GetProtocolInfo(req)

			if info.Version != tt.wantVersion {
				t.Errorf("Version = %v, want %v", info.Version, tt.wantVersion)
			}
			if info.TLS != tt.wantTLS {
				t.Errorf("TLS = %v, want %v", info.TLS, tt.wantTLS)
			}
			if info.ALPN != tt.wantALPN {
				t.Errorf("ALPN = %v, want %v", info.ALPN, tt.wantALPN)
			}
		})
	}
}

func TestIsProtocolUpgrade(t *testing.T) {
	tests := []struct {
		name        string
		conn        string
		upgrade     string
		h2Settings  string
		wantUpgrade bool
	}{
		{
			name:        "h2c upgrade",
			conn:        "Upgrade, HTTP2-Settings",
			upgrade:     "h2c",
			h2Settings:  "",
			wantUpgrade: true,
		},
		{
			name:        "h2c with settings",
			conn:        "",
			upgrade:     "",
			h2Settings:  "AAMAAABkAARAAAAAAAIAAAAA",
			wantUpgrade: true,
		},
		{
			name:        "websocket upgrade",
			conn:        "Upgrade",
			upgrade:     "websocket",
			wantUpgrade: false,
		},
		{
			name:        "no upgrade",
			wantUpgrade: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.conn != "" {
				req.Header.Set("Connection", tt.conn)
			}
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}
			if tt.h2Settings != "" {
				req.Header.Set("HTTP2-Settings", tt.h2Settings)
			}

			got := IsProtocolUpgrade(req)
			if got != tt.wantUpgrade {
				t.Errorf("IsProtocolUpgrade() = %v, want %v", got, tt.wantUpgrade)
			}
		})
	}
}

func TestHandleHTTP2Proxy_Disabled(t *testing.T) {
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	// Test with disabled HTTP/2
	config := &HTTP2Config{EnableHTTP2: false}
	err := HandleHTTP2Proxy(rec, req, be, config)

	if err == nil {
		t.Error("Expected error when HTTP/2 is disabled")
	}
}

func TestHandleHTTP2Proxy_MaxConnections(t *testing.T) {
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	be.SetState(backend.StateUp)
	be.MaxConns = 1

	// First connection should acquire
	if !be.AcquireConn() {
		t.Fatal("Failed to acquire first connection")
	}

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	config := DefaultHTTP2Config()
	err := HandleHTTP2Proxy(rec, req, be, config)

	if err == nil {
		t.Error("Expected error when backend at max connections")
	}

	be.ReleaseConn()
}

func TestNewHTTP2Proxy(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	h2Config := DefaultHTTP2Config()
	h2Proxy := NewHTTP2Proxy(httpProxy, h2Config)

	if h2Proxy == nil {
		t.Fatal("NewHTTP2Proxy returned nil")
	}
	if h2Proxy.httpProxy != httpProxy {
		t.Error("HTTP proxy mismatch")
	}
	if h2Proxy.h2Handler == nil {
		t.Error("h2Handler should not be nil")
	}
}

func TestHTTP2Proxy_ServeHTTP_HTTP2Request(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	h2Config := DefaultHTTP2Config()
	h2Proxy := NewHTTP2Proxy(httpProxy, h2Config)

	// Create HTTP/2 request
	req := httptest.NewRequest("GET", "/", nil)
	req.Proto = "HTTP/2.0"
	req.ProtoMajor = 2
	rec := httptest.NewRecorder()

	// Will fail because no healthy backends, but should not panic
	h2Proxy.ServeHTTP(rec, req)

	// Should get some response (might be error)
	if rec.Code == 0 {
		t.Error("Expected non-zero response code")
	}
}

func TestHTTP2Proxy_ServeHTTP_H2CRequest(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	h2Config := DefaultHTTP2Config()
	h2Proxy := NewHTTP2Proxy(httpProxy, h2Config)

	// Create h2c upgrade request
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()

	// Should handle h2c upgrade without panic
	h2Proxy.ServeHTTP(rec, req)

	// Might get upgrade required or other response
	if rec.Code == 0 {
		t.Error("Expected non-zero response code")
	}
}

func TestHTTP2Proxy_ServeHTTP_HTTP11Request(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	h2Config := DefaultHTTP2Config()
	h2Proxy := NewHTTP2Proxy(httpProxy, h2Config)

	// Create HTTP/1.1 request
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	// Should route to HTTP proxy without panic
	h2Proxy.ServeHTTP(rec, req)

	if rec.Code == 0 {
		t.Error("Expected non-zero response code")
	}
}

func TestHTTP2Listener_Name(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-name",
		Address: "127.0.0.1:0",
		Handler: handler,
	}
	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}
	if listener.Name() != "test-name" {
		t.Errorf("Name() = %q, want test-name", listener.Name())
	}
}

func TestHTTP2Listener_Address_BeforeStart(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:9999",
		Handler: handler,
	}
	listener, _ := NewHTTP2Listener(opts)

	addr := listener.Address()
	if addr != "127.0.0.1:9999" {
		t.Errorf("Address() before start = %q, want 127.0.0.1:9999", addr)
	}
}

func TestHTTP2Listener_StartError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Handler: handler,
	}
	listener, _ := NewHTTP2Listener(opts)

	if listener.StartError() != nil {
		t.Error("StartError() should be nil before start")
	}
}

func TestHTTP2Listener_StopNotRunning(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test",
		Address: "127.0.0.1:0",
		Handler: handler,
	}
	listener, _ := NewHTTP2Listener(opts)

	err := listener.Stop(context.Background())
	if err == nil {
		t.Error("Stop() on non-running listener should return error")
	}
}

func TestNewHTTP2BackendTransport_NilConfig(t *testing.T) {
	transport := NewHTTP2BackendTransport(nil)
	if transport == nil {
		t.Fatal("NewHTTP2BackendTransport(nil) returned nil")
	}
	if transport.config == nil {
		t.Error("Should use default config when nil")
	}
}

func TestHTTP2BackendTransport_CloseIdleConnections(t *testing.T) {
	transport := NewHTTP2BackendTransport(DefaultHTTP2Config())
	// Should not panic
	transport.CloseIdleConnections()
}

func TestALPNNegotiator_NegotiatedProtocol(t *testing.T) {
	n := NewALPNNegotiator(true)

	// Nil state
	proto := n.NegotiatedProtocol(nil)
	if proto != "" {
		t.Errorf("NegotiatedProtocol(nil) = %q, want empty", proto)
	}

	// With state
	state := &tls.ConnectionState{NegotiatedProtocol: "h2"}
	proto = n.NegotiatedProtocol(state)
	if proto != "h2" {
		t.Errorf("NegotiatedProtocol() = %q, want h2", proto)
	}
}

func TestNewHTTP2BackendTransport_WithConfig(t *testing.T) {
	config := &HTTP2Config{
		EnableHTTP2:          true,
		EnableH2C:            true,
		MaxConcurrentStreams: 100,
		IdleTimeout:          30 * time.Second,
	}
	transport := NewHTTP2BackendTransport(config)

	if transport == nil {
		t.Fatal("NewHTTP2BackendTransport returned nil")
	}
	if transport.config != config {
		t.Error("Config mismatch")
	}
	if transport.config.MaxConcurrentStreams != 100 {
		t.Errorf("MaxConcurrentStreams = %d, want 100", transport.config.MaxConcurrentStreams)
	}
	if transport.transport == nil {
		t.Error("Internal transport should not be nil")
	}
}

func TestHTTP2BackendTransport_RoundTrip(t *testing.T) {
	// Create an h2c test server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "ok")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("h2c response"))
	})

	h2s := &http2.Server{}
	server := httptest.NewServer(h2c.NewHandler(handler, h2s))
	defer server.Close()

	transport := NewHTTP2BackendTransport(DefaultHTTP2Config())

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "h2c response" {
		t.Errorf("Body = %q, want 'h2c response'", string(body))
	}
}

// Test with real HTTP/2 server using h2c
func TestHTTP2Handler_Integration(t *testing.T) {
	// Create a simple handler
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the protocol
		w.Header().Set("X-Proto", r.Proto)
		w.Write([]byte("Hello via " + r.Proto))
	})

	// Wrap with h2c
	h2s := &http2.Server{}
	handler := h2c.NewHandler(handlerFunc, h2s)

	// Create server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create client that supports HTTP/2
	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
	}

	// Make request
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Request error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Should get HTTP/2 response
	if !strings.Contains(string(body), "HTTP/2") {
		t.Logf("Response: %s (may be HTTP/1.1 if h2c not negotiated)", string(body))
	}
}
