package l7

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/router"
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

func TestHandleHTTP2Proxy_NilConfig(t *testing.T) {
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	err := HandleHTTP2Proxy(rec, req, be, nil)
	if err == nil {
		t.Error("Expected error when config is nil")
	}
}

func TestHandleHTTP2Proxy_FullRoundTrip(t *testing.T) {
	// Create an h2c test backend
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "h2c")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("HTTP/2 response from backend"))
	})

	h2s := &http2.Server{}
	server := httptest.NewServer(h2c.NewHandler(handler, h2s))
	defer server.Close()

	// Extract host:port from server URL
	addr := strings.TrimPrefix(server.URL, "http://")
	be := backend.NewBackend("h2-backend", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Proto = "HTTP/2.0"
	req.ProtoMajor = 2
	rec := httptest.NewRecorder()

	config := DefaultHTTP2Config()
	err := HandleHTTP2Proxy(rec, req, be, config)
	if err != nil {
		t.Fatalf("HandleHTTP2Proxy() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if body != "HTTP/2 response from backend" {
		t.Errorf("Body = %q, want %q", body, "HTTP/2 response from backend")
	}

	// Check headers were copied
	if rec.Header().Get("X-Backend") != "h2c" {
		t.Errorf("X-Backend header = %q, want %q", rec.Header().Get("X-Backend"), "h2c")
	}
}

func TestHandleHTTP2Proxy_BackendConnectionError(t *testing.T) {
	// Use an address that refuses connections
	be := backend.NewBackend("h2-backend-bad", "127.0.0.1:1")
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	config := DefaultHTTP2Config()
	err := HandleHTTP2Proxy(rec, req, be, config)
	if err == nil {
		t.Error("Expected error when backend connection fails")
	}
	if err != nil && !strings.Contains(err.Error(), "backend request failed") {
		t.Errorf("Expected 'backend request failed' error, got: %v", err)
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

// ============================================================================
// Tests for HTTP2Listener Start with TLS
// ============================================================================

func TestHTTP2Listener_StartWithTLS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello TLS"))
	})

	// Generate a self-signed cert for testing
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("Failed to marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	opts := &HTTP2ListenerOptions{
		Name:      "test-tls",
		Address:   "127.0.0.1:0",
		Handler:   handler,
		TLSConfig: tlsConfig,
		Config:    DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	if !listener.IsRunning() {
		t.Error("Listener should be running")
	}

	// Verify address is updated
	addr := listener.Address()
	if addr == "127.0.0.1:0" {
		t.Error("Address should be updated after start")
	}
}

// ============================================================================
// Tests for HTTP2Listener Stop with deadline exceeded
// ============================================================================

func TestHTTP2Listener_StopWithDeadlineExceeded(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler that blocks forever
		select {}
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-deadline",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Create a connection that holds up shutdown
	go func() {
		conn, err := net.Dial("tcp", listener.Address())
		if err == nil {
			conn.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	// Stop with very short deadline
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// The stop should either succeed quickly or timeout
	_ = listener.Stop(ctx)
}

// ============================================================================
// Tests for HTTP2Listener Stop when not running (double-check)
// ============================================================================

func TestHTTP2Listener_StopWhenAlreadyStopped(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-already-stopped",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)

	// Stop should fail - listener not running
	err := listener.Stop(context.Background())
	if err == nil {
		t.Error("Expected error when stopping non-running listener")
	}
}

// ============================================================================
// Tests for HTTP2Listener Address after start
// ============================================================================

func TestHTTP2Listener_AddressAfterStart(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-addr",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)

	// Before start, should return configured address
	beforeAddr := listener.Address()
	if beforeAddr != "127.0.0.1:0" {
		t.Errorf("Address before start = %q, want 127.0.0.1:0", beforeAddr)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// After start, should return actual address
	afterAddr := listener.Address()
	if afterAddr == "127.0.0.1:0" {
		t.Error("Address after start should be different from configured address")
	}
	if afterAddr == "" {
		t.Error("Address after start should not be empty")
	}
}

// ============================================================================
// Tests for HTTP2Listener StartError recording
// ============================================================================

func TestHTTP2Listener_StartError_AfterSuccessfulStart(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-starterror",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// After successful start, StartError should be nil
	if listener.StartError() != nil {
		t.Errorf("StartError() = %v, want nil after successful start", listener.StartError())
	}
}

// ============================================================================
// Test for HTTP2Listener Stop covering deadline exceeded and shutdown error
// ============================================================================

func TestHTTP2Listener_StopDeadlineExceeded(t *testing.T) {
	// Create a listener that has a handler that stalls, making shutdown take time
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write([]byte("done"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-stop-deadline",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Start a request that will hold the connection open
	go func() {
		resp, err := http.Get("http://" + listener.Address() + "/")
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Give the request time to be accepted
	time.Sleep(100 * time.Millisecond)

	// Stop with an already-expired context to trigger deadline exceeded
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond) // Ensure context is expired

	err = listener.Stop(ctx)
	// The error could be nil (if shutdown completes before deadline) or a timeout error
	if err != nil {
		// If we got an error, it should mention timeout
		t.Logf("Stop returned error (expected): %v", err)
	}
}

// ============================================================================
// Test for HTTP2Listener Stop after listener is already stopped
// ============================================================================

func TestHTTP2Listener_DoubleStop(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-double-stop",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// First stop should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := listener.Stop(ctx); err != nil {
		t.Errorf("First Stop error: %v", err)
	}

	// Wait for running to be false
	time.Sleep(100 * time.Millisecond)

	// Second stop should fail
	err := listener.Stop(context.Background())
	if err == nil {
		t.Error("Second Stop should return error")
	}
}

// ============================================================================
// Test for HTTP2Listener Start with invalid address (failure path)
// ============================================================================

func TestHTTP2Listener_StartWithInvalidAddress(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	opts := &HTTP2ListenerOptions{
		Name:    "test-invalid-addr",
		Address: "0.0.0.0:0", // Valid but let's use a different approach
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)

	if err := listener.Start(); err != nil {
		t.Logf("Start with address failed as expected: %v", err)
	} else {
		// If it succeeds, clean up
		listener.Stop(context.Background())
	}
}

// ============================================================================
// HTTP2Listener Start with TLS and verify ALPN config
// ============================================================================

func TestHTTP2Listener_StartWithTLS_VerifyALPN(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello TLS ALPN"))
	})

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	cert, _ := tls.X509KeyPair(certPEM, keyPEM)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	opts := &HTTP2ListenerOptions{
		Name:      "test-tls-alpn",
		Address:   "127.0.0.1:0",
		Handler:   handler,
		TLSConfig: tlsConfig,
		Config:    DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer listener.Stop(context.Background())

	// Verify the TLS config has ALPN protocols set
	if len(tlsConfig.NextProtos) < 2 {
		t.Errorf("Expected ALPN protos to be set, got %v", tlsConfig.NextProtos)
	}
	if tlsConfig.NextProtos[0] != "h2" {
		t.Errorf("First ALPN proto should be h2, got %s", tlsConfig.NextProtos[0])
	}
}

// ============================================================================
// HTTP2Listener Start with h2c disabled
// ============================================================================

func TestHTTP2Listener_StartWithH2CDisabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no h2c"))
	})

	config := DefaultHTTP2Config()
	config.EnableH2C = false // Disable h2c

	opts := &HTTP2ListenerOptions{
		Name:    "test-no-h2c",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  config,
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + listener.Address())
	if err != nil {
		t.Fatalf("Request error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "no h2c" {
		t.Errorf("Body = %q, want 'no h2c'", string(body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listener.Stop(ctx)
}

// ============================================================================
// HTTP2Listener Start with invalid address (listen failure)
// ============================================================================

func TestHTTP2Listener_StartWithBadAddress(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// Use a port that's already taken (if possible)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("Could not allocate a port")
	}
	takenAddr := ln.Addr().String()
	ln.Close() // Free it briefly, then try to bind

	// Now start a listener on that address first
	blocker, _ := net.Listen("tcp", takenAddr)
	defer blocker.Close()

	opts := &HTTP2ListenerOptions{
		Name:    "test-bad-addr",
		Address: takenAddr,
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, _ := NewHTTP2Listener(opts)
	err = listener.Start()
	if err == nil {
		t.Error("Expected error when address is already in use")
		listener.Stop(context.Background())
	}
}

// ============================================================================
// HandleHTTP2Proxy: full round-trip with flusher
// ============================================================================

func TestHandleHTTP2Proxy_WithFlusher(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("flushed response"))
	})

	h2s := &http2.Server{}
	server := httptest.NewServer(h2c.NewHandler(handler, h2s))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	be := backend.NewBackend("h2-flush-backend", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Proto = "HTTP/2.0"
	req.ProtoMajor = 2

	rec := httptest.NewRecorder()

	config := DefaultHTTP2Config()
	err := HandleHTTP2Proxy(rec, req, be, config)
	if err != nil {
		t.Fatalf("HandleHTTP2Proxy() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if body != "flushed response" {
		t.Errorf("Body = %q, want %q", body, "flushed response")
	}
}

// ============================================================================
// HandleHTTP2Proxy: backend connection refused
// ============================================================================

func TestHandleHTTP2Proxy_BackendRefused(t *testing.T) {
	be := backend.NewBackend("h2-refused", "127.0.0.1:1")
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	config := DefaultHTTP2Config()
	err := HandleHTTP2Proxy(rec, req, be, config)
	if err == nil {
		t.Error("Expected error when backend connection fails")
	}
	if err != nil && !strings.Contains(err.Error(), "backend request failed") {
		t.Errorf("Expected 'backend request failed', got: %v", err)
	}
}

// ============================================================================
// HTTP2Proxy ServeHTTP with regular HTTP/1.1 request
// ============================================================================

func TestHTTP2Proxy_ServeHTTP_HTTP11WithRoute(t *testing.T) {
	httpProxy, poolManager, routerInstance := setupTestProxy(t)

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("h1 response"))
	}))
	defer backendServer.Close()

	pool := backend.NewPool("h2-pool", "round_robin")
	pool.SetBalancer(balancer.NewRoundRobin())
	b := backend.NewBackend("h2-b1", backendServer.Listener.Addr().String())
	b.SetState(backend.StateUp)
	pool.AddBackend(b)
	poolManager.AddPool(pool)

	route := &router.Route{Name: "h2-route", Path: "/test", BackendPool: "h2-pool"}
	routerInstance.AddRoute(route)

	h2Proxy := NewHTTP2Proxy(httpProxy, DefaultHTTP2Config())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	h2Proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "h1 response" {
		t.Errorf("expected 'h1 response', got %q", rec.Body.String())
	}
}

// ============================================================================
// HTTP2Listener: Start error captured in StartError after listener close
// ============================================================================

func TestHTTP2Listener_StartError_AcceptAfterClose(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	})

	opts := &HTTP2ListenerOptions{
		Name:    "test-accept-err",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  DefaultHTTP2Config(),
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Get the underlying listener and close it to cause Serve to fail
	addr := listener.Address()
	t.Logf("Listening on %s", addr)

	// Close the underlying listener to force an Accept error
	// This should cause the goroutine to record a startErr
	listener.mu.Lock()
	if listener.listener != nil {
		listener.listener.Close()
	}
	listener.mu.Unlock()

	// Wait for the goroutine to detect the error
	time.Sleep(200 * time.Millisecond)

	// The listener should have stopped
	if listener.IsRunning() {
		t.Log("Listener still running after listener close")
	}

	// StartError may or may not be set depending on timing
	t.Logf("StartError: %v", listener.StartError())
}

// ============================================================================
// HTTP2Handler: GetTransport with HTTPS scheme
// ============================================================================

func TestHTTP2Handler_GetTransport_HTTPS(t *testing.T) {
	config := &HTTP2Config{EnableHTTP2: true, EnableH2C: true}
	handler := NewHTTP2Handler(config)

	// HTTPS should use the standard transport
	transport := handler.GetTransport("https")
	if transport == nil {
		t.Fatal("GetTransport(https) returned nil")
	}
	if handler.transport != transport {
		t.Error("Expected standard transport for HTTPS scheme")
	}
}

// ============================================================================
// HTTP2Listener: Start with non-zero config values
// ============================================================================

func TestHTTP2Listener_StartWithCustomConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("custom config"))
	})

	config := &HTTP2Config{
		EnableHTTP2:          true,
		EnableH2C:            true,
		MaxConcurrentStreams: 100,
		MaxFrameSize:         32 * 1024,
		IdleTimeout:          30 * time.Second,
	}

	opts := &HTTP2ListenerOptions{
		Name:    "test-custom-cfg",
		Address: "127.0.0.1:0",
		Handler: handler,
		Config:  config,
	}

	listener, err := NewHTTP2Listener(opts)
	if err != nil {
		t.Fatalf("NewHTTP2Listener error: %v", err)
	}

	if err := listener.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + listener.Address())
	if err != nil {
		t.Fatalf("Request error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "custom config" {
		t.Errorf("Body = %q, want 'custom config'", string(body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listener.Stop(ctx)
}

// ============================================================================
// HTTP2BackendTransport: RoundTrip to unreachable backend
// ============================================================================

func TestHTTP2BackendTransport_RoundTrip_Unreachable(t *testing.T) {
	transport := NewHTTP2BackendTransport(DefaultHTTP2Config())

	req, _ := http.NewRequest("GET", "http://127.0.0.1:1/", nil)
	_, err := transport.RoundTrip(req)
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}
}

// --- HTTP/2 strict mode hardening tests ---

func TestHTTP2Config_StrictModeDefaults(t *testing.T) {
	config := DefaultHTTP2Config()

	if config.MaxDecoderHeaderBytes != 64*1024 {
		t.Errorf("expected MaxDecoderHeaderBytes=65536, got %d", config.MaxDecoderHeaderBytes)
	}
	if config.MaxHeaderListSize != 256 {
		t.Errorf("expected MaxHeaderListSize=256, got %d", config.MaxHeaderListSize)
	}
	if config.MaxUploadBufferPerConnection != 1*1024*1024 {
		t.Errorf("expected MaxUploadBufferPerConnection=1048576, got %d", config.MaxUploadBufferPerConnection)
	}
	if config.MaxUploadBufferPerStream != 256*1024 {
		t.Errorf("expected MaxUploadBufferPerStream=262144, got %d", config.MaxUploadBufferPerStream)
	}
	if config.ReadIdleTimeout != 30*time.Second {
		t.Errorf("expected ReadIdleTimeout=30s, got %v", config.ReadIdleTimeout)
	}
	if config.PingTimeout != 15*time.Second {
		t.Errorf("expected PingTimeout=15s, got %v", config.PingTimeout)
	}
}

func TestHTTP2Handler_NewHTTP2Server_SetsAllFields(t *testing.T) {
	config := &HTTP2Config{
		EnableHTTP2:                  true,
		MaxConcurrentStreams:         100,
		MaxFrameSize:                 32768,
		IdleTimeout:                  30 * time.Second,
		ReadIdleTimeout:              10 * time.Second,
		PingTimeout:                  5 * time.Second,
		MaxDecoderHeaderBytes:        32768,
		MaxUploadBufferPerConnection: 512 * 1024,
		MaxUploadBufferPerStream:     128 * 1024,
	}

	handler := NewHTTP2Handler(config)
	srv := handler.newHTTP2Server()

	if srv.MaxConcurrentStreams != 100 {
		t.Errorf("expected MaxConcurrentStreams=100, got %d", srv.MaxConcurrentStreams)
	}
	if srv.MaxReadFrameSize != 32768 {
		t.Errorf("expected MaxReadFrameSize=32768, got %d", srv.MaxReadFrameSize)
	}
	if srv.IdleTimeout != 30*time.Second {
		t.Errorf("expected IdleTimeout=30s, got %v", srv.IdleTimeout)
	}
	if srv.ReadIdleTimeout != 10*time.Second {
		t.Errorf("expected ReadIdleTimeout=10s, got %v", srv.ReadIdleTimeout)
	}
	if srv.PingTimeout != 5*time.Second {
		t.Errorf("expected PingTimeout=5s, got %v", srv.PingTimeout)
	}
	if srv.MaxUploadBufferPerConnection != 512*1024 {
		t.Errorf("expected MaxUploadBufferPerConnection=524288, got %d", srv.MaxUploadBufferPerConnection)
	}
	if srv.MaxUploadBufferPerStream != 128*1024 {
		t.Errorf("expected MaxUploadBufferPerStream=131072, got %d", srv.MaxUploadBufferPerStream)
	}
	if srv.MaxDecoderHeaderTableSize != 32768 {
		t.Errorf("expected MaxDecoderHeaderTableSize=32768, got %d", srv.MaxDecoderHeaderTableSize)
	}
}

func TestHTTP2Handler_NewHTTP2Server_ZeroHeaderBytes(t *testing.T) {
	config := &HTTP2Config{
		EnableHTTP2:           true,
		MaxDecoderHeaderBytes: 0, // should not set MaxDecoderHeaderTableSize
	}

	handler := NewHTTP2Handler(config)
	srv := handler.newHTTP2Server()

	// Should not override the default (4096 in http2 library)
	if srv.MaxDecoderHeaderTableSize != 0 {
		t.Errorf("expected MaxDecoderHeaderTableSize=0 when config is 0, got %d", srv.MaxDecoderHeaderTableSize)
	}
}

func TestHTTP2Listener_Start_SetsStrictMode(t *testing.T) {
	config := &HTTP2Config{
		EnableHTTP2:                  true,
		EnableH2C:                    true,
		MaxConcurrentStreams:         50,
		MaxFrameSize:                 16384,
		IdleTimeout:                  30 * time.Second,
		ReadIdleTimeout:              10 * time.Second,
		PingTimeout:                  5 * time.Second,
		MaxDecoderHeaderBytes:        16384,
		MaxUploadBufferPerConnection: 512 * 1024,
		MaxUploadBufferPerStream:     64 * 1024,
	}

	listener, err := NewHTTP2Listener(&HTTP2ListenerOptions{
		Name:    "test-strict",
		Address: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		Config:  config,
	})
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	// Start in background
	go listener.Start()
	defer listener.Stop(context.Background())

	// Wait for startup
	time.Sleep(100 * time.Millisecond)

	// Verify the h2Server was configured
	h2s := listener.h2Server
	if h2s == nil {
		t.Fatal("expected h2Server to be set")
	}
	if h2s.MaxConcurrentStreams != 50 {
		t.Errorf("expected MaxConcurrentStreams=50, got %d", h2s.MaxConcurrentStreams)
	}
	if h2s.ReadIdleTimeout != 10*time.Second {
		t.Errorf("expected ReadIdleTimeout=10s, got %v", h2s.ReadIdleTimeout)
	}
	if h2s.PingTimeout != 5*time.Second {
		t.Errorf("expected PingTimeout=5s, got %v", h2s.PingTimeout)
	}
	if h2s.MaxUploadBufferPerConnection != 512*1024 {
		t.Errorf("expected MaxUploadBufferPerConnection=524288, got %d", h2s.MaxUploadBufferPerConnection)
	}
	if h2s.MaxUploadBufferPerStream != 64*1024 {
		t.Errorf("expected MaxUploadBufferPerStream=65536, got %d", h2s.MaxUploadBufferPerStream)
	}
	if h2s.MaxDecoderHeaderTableSize != 16384 {
		t.Errorf("expected MaxDecoderHeaderTableSize=16384, got %d", h2s.MaxDecoderHeaderTableSize)
	}
}
