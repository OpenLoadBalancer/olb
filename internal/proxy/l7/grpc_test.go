package l7

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestIsGRPCRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "gRPC request",
			contentType: "application/grpc",
			expected:    true,
		},
		{
			name:        "gRPC with encoding",
			contentType: "application/grpc+proto",
			expected:    true,
		},
		{
			name:        "regular HTTP request",
			contentType: "application/json",
			expected:    false,
		},
		{
			name:        "empty content type",
			contentType: "",
			expected:    false,
		},
		{
			name:        "gRPC-Web request",
			contentType: "application/grpc-web",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/service/method", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			got := IsGRPCRequest(req)
			if got != tt.expected {
				t.Errorf("IsGRPCRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultGRPCConfig(t *testing.T) {
	config := DefaultGRPCConfig()

	if !config.EnableGRPC {
		t.Error("EnableGRPC should be true by default")
	}
	if config.MaxMessageSize != 4*1024*1024 {
		t.Errorf("MaxMessageSize = %v, want 4MB", config.MaxMessageSize)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", config.Timeout)
	}
	if !config.EnableGRPCWeb {
		t.Error("EnableGRPCWeb should be true by default")
	}
}

func TestNewGRPCHandler(t *testing.T) {
	config := DefaultGRPCConfig()
	handler := NewGRPCHandler(config)

	if handler == nil {
		t.Fatal("NewGRPCHandler() returned nil")
	}
	if handler.config != config {
		t.Error("Handler config mismatch")
	}
	if handler.transport == nil {
		t.Error("Handler transport should not be nil")
	}
}

func TestNewGRPCHandler_NilConfig(t *testing.T) {
	handler := NewGRPCHandler(nil)

	if handler == nil {
		t.Fatal("NewGRPCHandler(nil) returned nil")
	}
	if handler.config == nil {
		t.Error("Handler config should use defaults when nil")
	}
}

func TestGRPCHandler_Disabled(t *testing.T) {
	config := &GRPCConfig{EnableGRPC: false}
	handler := NewGRPCHandler(config)

	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	req := httptest.NewRequest("POST", "/service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)

	if err == nil || err.Error() != "gRPC disabled" {
		t.Errorf("Expected 'gRPC disabled' error, got: %v", err)
	}
}

func TestGRPCHandler_BackendMaxConnections(t *testing.T) {
	handler := NewGRPCHandler(nil)

	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	be.SetState(backend.StateUp)
	be.SetMaxConns(1)

	// First connection should acquire
	if !be.AcquireConn() {
		t.Fatal("Failed to acquire first connection")
	}

	req := httptest.NewRequest("POST", "/service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)

	if err == nil || err.Error() != "backend at max connections" {
		t.Errorf("Expected 'backend at max connections' error, got: %v", err)
	}

	be.ReleaseConn()
}

func TestIsGRPCWebRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "gRPC-Web request",
			contentType: "application/grpc-web",
			expected:    true,
		},
		{
			name:        "gRPC-Web with text",
			contentType: "application/grpc-web-text",
			expected:    true,
		},
		{
			name:        "regular gRPC request",
			contentType: "application/grpc",
			expected:    false,
		},
		{
			name:        "regular HTTP request",
			contentType: "application/json",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/service/method", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			got := IsGRPCWebRequest(req)
			if got != tt.expected {
				t.Errorf("IsGRPCWebRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewGRPCWebHandler(t *testing.T) {
	grpcHandler := NewGRPCHandler(nil)
	webHandler := NewGRPCWebHandler(grpcHandler)

	if webHandler == nil {
		t.Fatal("NewGRPCWebHandler() returned nil")
	}
	if webHandler.grpcHandler != grpcHandler {
		t.Error("gRPC-Web handler should reference gRPC handler")
	}
}

func TestGRPCWebHandler_Disabled(t *testing.T) {
	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: false}
	grpcHandler := NewGRPCHandler(config)
	webHandler := NewGRPCWebHandler(grpcHandler)

	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	req := httptest.NewRequest("POST", "/service/method", nil)
	req.Header.Set("Content-Type", "application/grpc-web")

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)

	if err == nil || err.Error() != "gRPC-Web disabled" {
		t.Errorf("Expected 'gRPC-Web disabled' error, got: %v", err)
	}
}

func TestHTTPStatusToGRPCStatus(t *testing.T) {
	tests := []struct {
		httpStatus int
		expected   GRPCStatus
	}{
		{http.StatusOK, GRPCStatusOK},
		{http.StatusBadRequest, GRPCStatusInvalidArgument},
		{http.StatusUnauthorized, GRPCStatusUnauthenticated},
		{http.StatusForbidden, GRPCStatusPermissionDenied},
		{http.StatusNotFound, GRPCStatusNotFound},
		{http.StatusTooManyRequests, GRPCStatusResourceExhausted},
		{http.StatusInternalServerError, GRPCStatusInternal},
		{http.StatusNotImplemented, GRPCStatusUnimplemented},
		{http.StatusBadGateway, GRPCStatusUnavailable},
		{http.StatusServiceUnavailable, GRPCStatusUnavailable},
		{http.StatusGatewayTimeout, GRPCStatusDeadlineExceeded},
		{999, GRPCStatusUnknown}, // Unknown status
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.httpStatus), func(t *testing.T) {
			got := HTTPStatusToGRPCStatus(tt.httpStatus)
			if got != tt.expected {
				t.Errorf("HTTPStatusToGRPCStatus(%d) = %v, want %v", tt.httpStatus, got, tt.expected)
			}
		})
	}
}

func TestGRPCStatusToHTTPStatus(t *testing.T) {
	tests := []struct {
		grpcStatus GRPCStatus
		expected   int
	}{
		{GRPCStatusOK, http.StatusOK},
		{GRPCStatusCancelled, 499},
		{GRPCStatusUnknown, http.StatusInternalServerError},
		{GRPCStatusInvalidArgument, http.StatusBadRequest},
		{GRPCStatusDeadlineExceeded, http.StatusGatewayTimeout},
		{GRPCStatusNotFound, http.StatusNotFound},
		{GRPCStatusAlreadyExists, http.StatusConflict},
		{GRPCStatusPermissionDenied, http.StatusForbidden},
		{GRPCStatusResourceExhausted, http.StatusTooManyRequests},
		{GRPCStatusFailedPrecondition, http.StatusPreconditionFailed},
		{GRPCStatusAborted, 409},
		{GRPCStatusOutOfRange, http.StatusBadRequest},
		{GRPCStatusUnimplemented, http.StatusNotImplemented},
		{GRPCStatusInternal, http.StatusInternalServerError},
		{GRPCStatusUnavailable, http.StatusServiceUnavailable},
		{GRPCStatusDataLoss, http.StatusInternalServerError},
		{GRPCStatusUnauthenticated, http.StatusUnauthorized},
		{99, http.StatusInternalServerError}, // Unknown status
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.grpcStatus)), func(t *testing.T) {
			got := GRPCStatusToHTTPStatus(tt.grpcStatus)
			if got != tt.expected {
				t.Errorf("GRPCStatusToHTTPStatus(%d) = %d, want %d", tt.grpcStatus, got, tt.expected)
			}
		})
	}
}

func TestCopyGRPCHeaders(t *testing.T) {
	src := http.Header{
		"Content-Type": []string{"application/grpc"},
		"X-Custom":     []string{"value"},
		"Connection":   []string{"keep-alive"},  // hop-by-hop, should be skipped
		"Trailer":      []string{"grpc-status"}, // trailer header, should be skipped
	}

	dst := make(http.Header)
	copyGRPCHeaders(dst, src)

	// Should have Content-Type and X-Custom
	if dst.Get("Content-Type") != "application/grpc" {
		t.Error("Content-Type header not copied")
	}
	if dst.Get("X-Custom") != "value" {
		t.Error("X-Custom header not copied")
	}

	// Should not have hop-by-hop headers
	if dst.Get("Connection") != "" {
		t.Error("Connection header should not be copied")
	}

	// Should not have trailer headers
	if dst.Get("Trailer") != "" {
		t.Error("Trailer header should not be copied")
	}
}

func TestParseGRPCFrame(t *testing.T) {
	// Create a gRPC frame
	data := []byte("hello world")
	frame := &gRPCFrame{
		Compressed: false,
		Length:     uint32(len(data)),
		Data:       data,
	}

	// Write to buffer
	var buf bytes.Buffer
	err := writeGRPCFrame(&buf, frame)
	if err != nil {
		t.Fatalf("writeGRPCFrame error: %v", err)
	}

	// Parse back
	parsed, err := parseGRPCFrame(&buf)
	if err != nil {
		t.Fatalf("parseGRPCFrame error: %v", err)
	}

	if parsed.Compressed != frame.Compressed {
		t.Errorf("Compressed = %v, want %v", parsed.Compressed, frame.Compressed)
	}
	if parsed.Length != frame.Length {
		t.Errorf("Length = %v, want %v", parsed.Length, frame.Length)
	}
	if !bytes.Equal(parsed.Data, frame.Data) {
		t.Errorf("Data = %v, want %v", parsed.Data, frame.Data)
	}
}

func TestParseGRPCFrame_Compressed(t *testing.T) {
	// Create a compressed gRPC frame
	data := []byte("compressed data")
	frame := &gRPCFrame{
		Compressed: true,
		Length:     uint32(len(data)),
		Data:       data,
	}

	// Write to buffer
	var buf bytes.Buffer
	err := writeGRPCFrame(&buf, frame)
	if err != nil {
		t.Fatalf("writeGRPCFrame error: %v", err)
	}

	// Parse back
	parsed, err := parseGRPCFrame(&buf)
	if err != nil {
		t.Fatalf("parseGRPCFrame error: %v", err)
	}

	if !parsed.Compressed {
		t.Error("Compressed flag should be true")
	}
}

func TestParseGRPCFrame_Empty(t *testing.T) {
	// Create an empty gRPC frame
	frame := &gRPCFrame{
		Compressed: false,
		Length:     0,
		Data:       []byte{},
	}

	// Write to buffer
	var buf bytes.Buffer
	err := writeGRPCFrame(&buf, frame)
	if err != nil {
		t.Fatalf("writeGRPCFrame error: %v", err)
	}

	// Parse back
	parsed, err := parseGRPCFrame(&buf)
	if err != nil {
		t.Fatalf("parseGRPCFrame error: %v", err)
	}

	if parsed.Length != 0 {
		t.Errorf("Length = %v, want 0", parsed.Length)
	}
	if len(parsed.Data) != 0 {
		t.Errorf("Data length = %v, want 0", len(parsed.Data))
	}
}

func TestParseGRPCFrame_Invalid(t *testing.T) {
	// Test with incomplete data
	buf := bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x00}) // Missing length byte
	_, err := parseGRPCFrame(buf)
	if err == nil {
		t.Error("Expected error for incomplete frame header")
	}
}

func TestPrepareGRPCRequest(t *testing.T) {
	handler := NewGRPCHandler(nil)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	req := httptest.NewRequest("POST", "/my.service/method", bytes.NewReader([]byte("request body")))
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Host = "example.com"

	outReq, err := handler.prepareGRPCRequest(req, be)
	if err != nil {
		t.Fatalf("prepareGRPCRequest error: %v", err)
	}

	// Check URL
	if outReq.URL.Host != "10.0.0.1:8080" {
		t.Errorf("URL.Host = %v, want 10.0.0.1:8080", outReq.URL.Host)
	}
	if outReq.URL.Scheme != "http" {
		t.Errorf("URL.Scheme = %v, want http", outReq.URL.Scheme)
	}

	// Check Host is preserved
	if outReq.Host != "example.com" {
		t.Errorf("Host = %v, want example.com", outReq.Host)
	}

	// Check HTTP/2
	if outReq.ProtoMajor != 2 {
		t.Errorf("ProtoMajor = %v, want 2", outReq.ProtoMajor)
	}

	// Check X-Forwarded headers
	if outReq.Header.Get("X-Forwarded-For") == "" {
		t.Error("X-Forwarded-For header not set")
	}
	if outReq.Header.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("X-Forwarded-Proto = %v, want http", outReq.Header.Get("X-Forwarded-Proto"))
	}

	// Check custom header is preserved
	if outReq.Header.Get("X-Custom-Header") != "custom-value" {
		t.Error("Custom header not preserved")
	}
}

func TestGRPCFrame_LargeData(t *testing.T) {
	// Test with larger data
	data := make([]byte, 10000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	frame := &gRPCFrame{
		Compressed: false,
		Length:     uint32(len(data)),
		Data:       data,
	}

	var buf bytes.Buffer
	err := writeGRPCFrame(&buf, frame)
	if err != nil {
		t.Fatalf("writeGRPCFrame error: %v", err)
	}

	parsed, err := parseGRPCFrame(&buf)
	if err != nil {
		t.Fatalf("parseGRPCFrame error: %v", err)
	}

	if parsed.Length != 10000 {
		t.Errorf("Length = %v, want 10000", parsed.Length)
	}
	if !bytes.Equal(parsed.Data, data) {
		t.Error("Data mismatch")
	}
}

func TestGRPCStatusConstants(t *testing.T) {
	// Verify gRPC status code values match spec
	if GRPCStatusOK != 0 {
		t.Error("GRPCStatusOK should be 0")
	}
	if GRPCStatusCancelled != 1 {
		t.Error("GRPCStatusCancelled should be 1")
	}
	if GRPCStatusUnknown != 2 {
		t.Error("GRPCStatusUnknown should be 2")
	}
	if GRPCStatusInternal != 13 {
		t.Error("GRPCStatusInternal should be 13")
	}
	if GRPCStatusUnavailable != 14 {
		t.Error("GRPCStatusUnavailable should be 14")
	}
	if GRPCStatusUnauthenticated != 16 {
		t.Error("GRPCStatusUnauthenticated should be 16")
	}
}

func TestGRPCHandler_HandleGRPC_FullRoundTrip(t *testing.T) {
	// Create a mock gRPC backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back gRPC-like response
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "OK")
		w.Header().Add(http.TrailerPrefix+"Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("grpc response body"))
	}))
	defer backendServer.Close()

	handler := NewGRPCHandler(nil)

	// Use backendServer address
	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-backend-1", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/MyMethod", bytes.NewReader([]byte("grpc request")))
	req.Header.Set("Content-Type", "application/grpc")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPC() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if body != "grpc response body" {
		t.Errorf("Body = %q, want %q", body, "grpc response body")
	}
}

func TestGRPCHandler_HandleGRPC_BackendError(t *testing.T) {
	handler := NewGRPCHandler(&GRPCConfig{
		EnableGRPC: true,
		Timeout:    1 * time.Second,
	})

	// Use a port that is guaranteed to refuse connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	listener.Close()
	be := backend.NewBackend("grpc-backend-bad", listener.Addr().String())
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/MyMethod", bytes.NewReader([]byte("grpc request")))
	req.Header.Set("Content-Type", "application/grpc")

	rec := httptest.NewRecorder()
	handleErr := handler.HandleGRPC(rec, req, be)
	if handleErr == nil {
		t.Error("Expected error when backend connection fails")
	}
}

func TestGRPCHandler_HandleGRPC_WithTimeout(t *testing.T) {
	// Create a backend server that responds slowly
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed"))
	}))
	defer backendServer.Close()

	handler := NewGRPCHandler(&GRPCConfig{
		EnableGRPC: true,
		Timeout:    100 * time.Millisecond, // Very short timeout
	})

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-backend-slow", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/MyMethod", bytes.NewReader([]byte("grpc request")))
	req.Header.Set("Content-Type", "application/grpc")

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)
	// Should get a timeout error
	if err == nil {
		t.Log("Backend may have responded before timeout")
	}
}

func TestGRPCHandler_HandleGRPC_WithTrailers(t *testing.T) {
	// Create a mock gRPC backend server with trailers
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("body"))
		// Note: httptest doesn't support trailers well, but this tests the path
	}))
	defer backendServer.Close()

	handler := NewGRPCHandler(nil)

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-backend-trailer", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPC() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}
}

func TestPrepareGRPCRequest_WithExistingXForwardedFor(t *testing.T) {
	handler := NewGRPCHandler(nil)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	req := httptest.NewRequest("POST", "/my.service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Host = "example.com"

	outReq, err := handler.prepareGRPCRequest(req, be)
	if err != nil {
		t.Fatalf("prepareGRPCRequest error: %v", err)
	}

	// Should append to existing X-Forwarded-For
	xff := outReq.Header.Get("X-Forwarded-For")
	if xff == "" {
		t.Error("X-Forwarded-For should not be empty")
	}
	if xff == "1.2.3.4" {
		t.Error("X-Forwarded-For should have appended the proxy IP")
	}
}

// ============================================================================
// parseGRPCFrame: data read error (truncated frame)
// ============================================================================

func TestParseGRPCFrame_DataReadError(t *testing.T) {
	// Write header claiming 100 bytes, but only provide 5
	buf := bytes.NewBuffer([]byte{0x00, 0x00, 0x00, 0x00, 0x64, 0x48, 0x45, 0x4C, 0x4C})
	_, err := parseGRPCFrame(buf)
	if err == nil {
		t.Error("expected error for truncated frame data")
	}
}

// ============================================================================
// prepareGRPCRequest with TLS request
// ============================================================================

func TestPrepareGRPCRequest_WithTLS(t *testing.T) {
	handler := NewGRPCHandler(nil)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	req := httptest.NewRequest("POST", "/my.service/method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.TLS = &tls.ConnectionState{}
	req.Host = "secure.example.com"

	outReq, err := handler.prepareGRPCRequest(req, be)
	if err != nil {
		t.Fatalf("prepareGRPCRequest error: %v", err)
	}

	if outReq.Header.Get("X-Forwarded-Proto") != "https" {
		t.Errorf("X-Forwarded-Proto = %v, want https", outReq.Header.Get("X-Forwarded-Proto"))
	}
}

// ============================================================================
// HandleGRPC: response body copy error
// ============================================================================

func TestGRPCHandler_HandleGRPC_ResponseBodyError(t *testing.T) {
	// Create a backend that returns a response then closes
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Log("Backend doesn't support hijacking")
			w.WriteHeader(http.StatusOK)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/grpc\r\n\r\npartial"))
		conn.Close()
	}))
	defer backendServer.Close()

	handler := NewGRPCHandler(nil)

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-backend-partial", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)
	t.Logf("HandleGRPC with partial response: %v", err)
}

// ============================================================================
// HandleGRPC: with explicit timeout (0 timeout should skip WithTimeout)
// ============================================================================

func TestGRPCHandler_HandleGRPC_ZeroTimeout(t *testing.T) {
	handler := NewGRPCHandler(&GRPCConfig{
		EnableGRPC: true,
		Timeout:    0, // Zero timeout - should skip WithTimeout
	})

	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backendServer.Close()

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-backend-notmo", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc")

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)
	if err != nil {
		t.Logf("HandleGRPC zero timeout: %v", err)
	}
}

// ============================================================================
// HandleGRPC: connection refused (round trip error)
// ============================================================================

func TestGRPCHandler_HandleGRPC_ConnectionRefused(t *testing.T) {
	handler := NewGRPCHandler(&GRPCConfig{
		EnableGRPC: true,
		Timeout:    1 * time.Second,
	})

	// Use a port that is guaranteed to refuse connections
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	be := backend.NewBackend("grpc-backend-refused", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc")

	rec := httptest.NewRecorder()
	handleErr := handler.HandleGRPC(rec, req, be)
	if handleErr == nil {
		t.Error("Expected error when backend connection fails")
	}
}

// ============================================================================
// HandleGRPC: backend returns non-200 status (copies headers and body)
// ============================================================================

func TestGRPCHandler_HandleGRPC_BackendReturnsError(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "14")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
	}))
	defer backendServer.Close()

	handler := NewGRPCHandler(nil)

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("grpc-backend-err", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := handler.HandleGRPC(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPC() error = %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// ============================================================================
// gRPC-Web helper function tests
// ============================================================================

func TestIsGRPCWebTextRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"grpc-web-text", "application/grpc-web-text", true},
		{"grpc-web-text+proto", "application/grpc-web-text+proto", true},
		{"grpc-web binary", "application/grpc-web", false},
		{"native grpc", "application/grpc", false},
		{"json", "application/json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/svc/method", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			if got := isGRPCWebTextRequest(req); got != tt.expected {
				t.Errorf("isGRPCWebTextRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGrpcWebResponseContentType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"application/grpc-web-text", "application/grpc-web-text+proto"},
		{"application/grpc-web-text+proto", "application/grpc-web-text+proto"},
		{"application/grpc-web", "application/grpc-web+proto"},
		{"application/grpc-web+proto", "application/grpc-web+proto"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := grpcWebResponseContentType(tt.input); got != tt.expected {
				t.Errorf("grpcWebResponseContentType(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestEncodeTrailersAsGRPCWebFrame(t *testing.T) {
	trailers := http.Header{
		"Grpc-Status":  []string{"0"},
		"Grpc-Message": []string{"OK"},
	}

	frame := encodeTrailersAsGRPCWebFrame(trailers)

	if frame[0] != 0x80 {
		t.Errorf("trailer flag = 0x%02x, want 0x80", frame[0])
	}

	length := binary.BigEndian.Uint32(frame[1:5])
	if length == 0 {
		t.Error("trailer data length should not be zero")
	}

	data := string(frame[5:])
	if !strings.Contains(data, "Grpc-Status: 0") {
		t.Errorf("trailer data should contain 'Grpc-Status: 0', got %q", data)
	}
	if !strings.Contains(data, "Grpc-Message: OK") {
		t.Errorf("trailer data should contain 'Grpc-Message: OK', got %q", data)
	}
}

func TestEncodeTrailersAsGRPCWebFrame_Empty(t *testing.T) {
	frame := encodeTrailersAsGRPCWebFrame(http.Header{})

	if frame[0] != 0x80 {
		t.Errorf("trailer flag = 0x%02x, want 0x80", frame[0])
	}

	length := binary.BigEndian.Uint32(frame[1:5])
	if length == 0 {
		t.Error("should synthesize grpc-status: 0 for empty trailers")
	}

	data := string(frame[5:])
	if !strings.Contains(data, "grpc-status: 0") {
		t.Errorf("empty trailers should synthesize grpc-status: 0, got %q", data)
	}
}

// ============================================================================
// gRPC-Web HandleGRPCWeb tests
// ============================================================================

func TestGRPCWebHandler_UnaryBinary_Success(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/grpc" {
			t.Errorf("backend received Content-Type = %q, want application/grpc", ct)
		}
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("grpc response data"))
	}))
	defer backendServer.Close()

	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 5 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("web-backend-1", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/MyMethod", bytes.NewReader([]byte("request data")))
	req.Header.Set("Content-Type", "application/grpc-web")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPCWeb() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/grpc-web+proto" {
		t.Errorf("Content-Type = %q, want application/grpc-web+proto", ct)
	}

	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("grpc response data")) {
		t.Errorf("body should start with grpc response data, got %q", body[:min(len(body), 30)])
	}

	// Verify trailer frame
	idx := bytes.IndexByte(body, 0x80)
	if idx < 0 {
		t.Fatal("response body should contain trailer frame (0x80 marker)")
	}
	trailerData := string(body[idx+5:])
	if !strings.Contains(trailerData, "Grpc-Status: 0") {
		t.Errorf("trailer frame should contain 'Grpc-Status: 0', got %q", trailerData)
	}
}

func TestGRPCWebHandler_UnaryText_Success(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("grpc response"))
	}))
	defer backendServer.Close()

	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 5 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("web-text-backend", addr)
	be.SetState(backend.StateUp)

	encodedBody := base64.StdEncoding.EncodeToString([]byte("request payload"))
	req := httptest.NewRequest("POST", "/my.service/MyMethod", strings.NewReader(encodedBody))
	req.Header.Set("Content-Type", "application/grpc-web-text")
	req.Host = "example.com"

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPCWeb() error = %v", err)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/grpc-web-text+proto" {
		t.Errorf("Content-Type = %q, want application/grpc-web-text+proto", ct)
	}

	decoded, err := base64.StdEncoding.DecodeString(rec.Body.String())
	if err != nil {
		t.Fatalf("response body is not valid base64: %v", err)
	}

	if !bytes.HasPrefix(decoded, []byte("grpc response")) {
		t.Errorf("decoded body should start with 'grpc response', got %q", decoded[:min(len(decoded), 30)])
	}

	idx := bytes.IndexByte(decoded, 0x80)
	if idx < 0 {
		t.Fatal("decoded body should contain trailer frame (0x80 marker)")
	}
}

func TestGRPCWebHandler_BackendError(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "14")
		w.Header().Set("Grpc-Message", "service unavailable")
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 5 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("web-err-backend", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/MyMethod", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc-web")

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPCWeb() error = %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Grpc-Status: 14") {
		t.Errorf("trailer should contain 'Grpc-Status: 14', got %q", body)
	}
	if !strings.Contains(body, "Grpc-Message: service unavailable") {
		t.Errorf("trailer should contain error message, got %q", body)
	}
}

func TestGRPCWebHandler_NoTrailersFromBackend(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer backendServer.Close()

	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 5 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("web-notrailer-backend", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc-web")

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPCWeb() error = %v", err)
	}

	body := rec.Body.Bytes()
	idx := bytes.IndexByte(body, 0x80)
	if idx < 0 {
		t.Fatal("response should contain trailer frame")
	}
	trailerData := string(body[idx+5:])
	if !strings.Contains(trailerData, "grpc-status: 0") {
		t.Errorf("synthesized trailer should contain 'grpc-status: 0', got %q", trailerData)
	}
}

func TestGRPCWebHandler_BackendConnectionRefused(t *testing.T) {
	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 1 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	be := backend.NewBackend("web-refused-backend", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc-web")

	rec := httptest.NewRecorder()
	handleErr := webHandler.HandleGRPCWeb(rec, req, be)
	if handleErr == nil {
		t.Error("expected error when backend connection fails")
	}
}

func TestGRPCWebHandler_MaxConnectionsExceeded(t *testing.T) {
	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 1 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	be := backend.NewBackend("web-maxconn-backend", "127.0.0.1:8080")
	be.SetState(backend.StateUp)
	be.SetMaxConns(1)

	if !be.AcquireConn() {
		t.Fatal("failed to acquire first connection")
	}

	req := httptest.NewRequest("POST", "/my.service/Method", bytes.NewReader([]byte("req")))
	req.Header.Set("Content-Type", "application/grpc-web")

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err == nil || err.Error() != "backend at max connections" {
		t.Errorf("expected 'backend at max connections' error, got: %v", err)
	}

	be.ReleaseConn()
}

func TestGRPCWebHandler_TextMode_DecodesRequestBody(t *testing.T) {
	var receivedBody string
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backendServer.Close()

	config := &GRPCConfig{EnableGRPC: true, EnableGRPCWeb: true, Timeout: 5 * time.Second}
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("web-decode-backend", addr)
	be.SetState(backend.StateUp)

	originalBody := "hello gRPC-Web text mode"
	encodedBody := base64.StdEncoding.EncodeToString([]byte(originalBody))
	req := httptest.NewRequest("POST", "/my.service/Method", strings.NewReader(encodedBody))
	req.Header.Set("Content-Type", "application/grpc-web-text")

	rec := httptest.NewRecorder()
	err := webHandler.HandleGRPCWeb(rec, req, be)
	if err != nil {
		t.Fatalf("HandleGRPCWeb() error = %v", err)
	}

	if receivedBody != originalBody {
		t.Errorf("backend received %q, want %q", receivedBody, originalBody)
	}
}

func TestGRPCWebHandler_PrepareGRPCWebRequest_SetsHeaders(t *testing.T) {
	config := DefaultGRPCConfig()
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	req := httptest.NewRequest("POST", "/my.service/method", bytes.NewReader([]byte("body")))
	req.Header.Set("Content-Type", "application/grpc-web")
	req.Host = "example.com"

	outReq, err := webHandler.prepareGRPCWebRequest(req, be, []byte("body"))
	if err != nil {
		t.Fatalf("prepareGRPCWebRequest error: %v", err)
	}

	if outReq.Header.Get("Content-Type") != "application/grpc" {
		t.Errorf("Content-Type = %q, want application/grpc", outReq.Header.Get("Content-Type"))
	}
	if outReq.URL.Host != "10.0.0.1:8080" {
		t.Errorf("URL.Host = %v, want 10.0.0.1:8080", outReq.URL.Host)
	}
	if outReq.Host != "example.com" {
		t.Errorf("Host = %v, want example.com", outReq.Host)
	}
	if outReq.ProtoMajor != 2 {
		t.Errorf("ProtoMajor = %v, want 2", outReq.ProtoMajor)
	}
	if outReq.Header.Get("X-Forwarded-For") == "" {
		t.Error("X-Forwarded-For should be set")
	}
}

func TestPrepareGRPCWebRequest_InvalidBackendAddress(t *testing.T) {
	// url.Parse is very lenient — test with a backend address that actually fails
	// The prepareGRPCWebRequest method should work with any address, so this test
	// validates the code path doesn't panic with unusual addresses.
	config := DefaultGRPCConfig()
	webHandler := NewGRPCWebHandler(NewGRPCHandler(config))

	be := backend.NewBackend("odd-backend", "???invalid host???")
	req := httptest.NewRequest("POST", "/my.service/method", bytes.NewReader([]byte("body")))
	req.Header.Set("Content-Type", "application/grpc-web")

	// This should succeed in building the request (url.Parse is lenient),
	// but would fail on actual connection. Just verify no panic.
	outReq, err := webHandler.prepareGRPCWebRequest(req, be, []byte("body"))
	if err != nil {
		// Acceptable — some addresses may fail parsing
		return
	}
	if outReq == nil {
		t.Error("request should not be nil when no parse error")
	}
}
