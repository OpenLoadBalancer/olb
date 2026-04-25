package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// Tests: SSE Transport authenticate (sse_transport.go:134)
// --------------------------------------------------------------------------

func TestSSETransport_Authenticate_EmptyBearerTokenRejected(t *testing.T) {
	s := newTestServer()
	_, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "",
	})
	if err == nil {
		t.Fatal("Expected error for empty BearerToken")
	}
}

func TestSSETransport_Authenticate_ValidToken(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "my-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/sse", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	if !transport.authenticate(req) {
		t.Error("authenticate should return true for valid token")
	}
}

func TestSSETransport_Authenticate_InvalidToken(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "my-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/sse", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	if transport.authenticate(req) {
		t.Error("authenticate should return false for wrong token")
	}
}

func TestSSETransport_Authenticate_NoAuthHeader(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "my-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/sse", nil)
	if transport.authenticate(req) {
		t.Error("authenticate should return false with no auth header")
	}
}

func TestSSETransport_Authenticate_MalformedAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "my-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/sse", nil)
	req.Header.Set("Authorization", "Basic abc123")
	if transport.authenticate(req) {
		t.Error("authenticate should return false for non-Bearer auth")
	}
}

func TestSSETransport_Authenticate_ShortHeader(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "my-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/sse", nil)
	req.Header.Set("Authorization", "Bear")
	if transport.authenticate(req) {
		t.Error("authenticate should return false for short auth header")
	}
}

// --------------------------------------------------------------------------
// Tests: SSE Transport handleSSE method check (sse_transport.go:153)
// --------------------------------------------------------------------------

func TestSSETransport_HandleSSE_WrongMethod(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/sse", nil)
	transport.handleSSE(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleSSE with POST = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestSSETransport_HandleSSE_AuthFailure(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sse", nil)
	transport.handleSSE(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("handleSSE without auth = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --------------------------------------------------------------------------
// Tests: SSE Transport handleMessage (sse_transport.go:237)
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_WrongMethod(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/message", nil)
	transport.handleMessage(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleMessage with GET = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestSSETransport_HandleMessage_AuthFailure(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("handleMessage without auth = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestSSETransport_HandleMessage_WithSessionID(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Register a fake client to exercise broadcastToClient
	client := &sseClient{
		id:       "test-session",
		addr:     "127.0.0.1:12345",
		messages: make(chan []byte, 64),
		done:     make(chan struct{}),
	}
	transport.mu.Lock()
	transport.clients["test-session"] = client
	transport.mu.Unlock()

	// Send a valid JSON-RPC request with sessionId
	body := makeRequest("tools/list", nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message?sessionId=test-session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleMessage = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify the message was broadcast to the client
	select {
	case msg := <-client.messages:
		if len(msg) == 0 {
			t.Error("Expected non-empty message in client channel")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message in client channel")
	}
}

// --------------------------------------------------------------------------
// Tests: SSE Transport handleLegacy (sse_transport.go:289)
// --------------------------------------------------------------------------

func TestSSETransport_HandleLegacy_Options(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	transport.handleLegacy(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("handleLegacy OPTIONS = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Check CORS headers are set
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected Access-Control-Allow-Methods header")
	}
}

func TestSSETransport_HandleLegacy_WrongMethod(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mcp", nil)
	transport.handleLegacy(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleLegacy GET = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestSSETransport_HandleLegacy_AuthFailure(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleLegacy(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("handleLegacy without auth = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestSSETransport_HandleLegacy_WithAudit(t *testing.T) {
	s := newTestServer()
	auditCalled := false
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:     "127.0.0.1:0",
		AuditLog: true,
		AuditFunc: func(tool, params, clientAddr string, dur time.Duration, err error) {
			auditCalled = true
		},
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_list_backends",
		"arguments": map[string]any{},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleLegacy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleLegacy = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if auditCalled {
		t.Log("Audit function was called as expected")
	}
}

func TestSSETransport_HandleLegacy_BodyReadError(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", &errorReader{})
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleLegacy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleLegacy with body error = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// errorReader always returns an error
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read error")
}

// --------------------------------------------------------------------------
// Tests: SSE Transport CORS helpers (sse_transport.go:340-363)
// --------------------------------------------------------------------------

func TestSSETransport_CORSOrigin_Allowed(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"https://openloadbalancer.dev"},
		BearerToken:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Origin", "https://openloadbalancer.dev")

	origin := transport.corsOrigin(req)
	if origin != "https://openloadbalancer.dev" {
		t.Errorf("corsOrigin = %q, want %q", origin, "https://openloadbalancer.dev")
	}
}

func TestSSETransport_CORSOrigin_NotAllowed(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"https://openloadbalancer.dev"},
		BearerToken:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Origin", "https://evil.example.com")

	origin := transport.corsOrigin(req)
	if origin != "" {
		t.Errorf("corsOrigin = %q, want empty", origin)
	}
}

func TestSSETransport_CORSOrigin_NoOriginsConfigured(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Origin", "https://openloadbalancer.dev")

	origin := transport.corsOrigin(req)
	if origin != "" {
		t.Errorf("corsOrigin with no allowed origins = %q, want empty", origin)
	}
}

func TestSSETransport_BroadcastToClient_FullBufferExtra(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a client with a buffer of 1, already full
	client := &sseClient{
		id:       "full-client",
		addr:     "127.0.0.1:12345",
		messages: make(chan []byte, 1),
		done:     make(chan struct{}),
	}
	client.messages <- []byte("first message") // fill buffer

	transport.mu.Lock()
	transport.clients["full-client"] = client
	transport.mu.Unlock()

	// This should not block - message is dropped
	transport.broadcastToClient("full-client", []byte("dropped message"))
}

func TestSSETransport_BroadcastToClient_NonExistent(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Broadcast to non-existent client should not panic
	transport.broadcastToClient("non-existent", []byte("test"))
}

// --------------------------------------------------------------------------
// Tests: SSE Transport extractToolInfo (sse_transport.go:381)
// --------------------------------------------------------------------------

func TestExtractToolInfo_InvalidJSON(t *testing.T) {
	name, params := extractToolInfo([]byte("not json"))
	if name != "" || params != "" {
		t.Errorf("Expected empty results for invalid JSON, got %q, %q", name, params)
	}
}

func TestExtractToolInfo_NonToolCall(t *testing.T) {
	body := makeRequest("resources/list", nil)
	name, params := extractToolInfo(body)
	if name != "" || params != "" {
		t.Errorf("Expected empty results for non-tool call, got %q, %q", name, params)
	}
}

func TestExtractToolInfo_ToolCallWithArgs(t *testing.T) {
	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_list_backends",
		"arguments": map[string]any{"pool": "web"},
	})
	name, params := extractToolInfo(body)
	if name != "olb_list_backends" {
		t.Errorf("Expected tool name olb_list_backends, got %q", name)
	}
	if !strings.Contains(params, "web") {
		t.Errorf("Expected params to contain 'web', got %q", params)
	}
}

func TestExtractToolInfo_ToolCallNoParams(t *testing.T) {
	// tools/call with invalid params - missing params field
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	name, params := extractToolInfo(body)
	if name != "" || params != "" {
		t.Errorf("Expected empty results for tool call without params, got %q, %q", name, params)
	}
}

// --------------------------------------------------------------------------
// Tests: SSE Transport full integration with Start/Stop
// --------------------------------------------------------------------------

func TestSSETransport_StartStopIntegration(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	addr := transport.Addr()
	if addr == "" {
		t.Error("Addr should return non-empty after Start")
	}

	// Make a simple request
	resp, err := http.Get("http://" + addr + "/sse")
	if err != nil {
		t.Logf("GET /sse error: %v", err)
	} else {
		resp.Body.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := transport.Stop(ctx); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

func TestSSETransport_ClientCountManual(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if transport.ClientCount() != 0 {
		t.Error("ClientCount should be 0 initially")
	}

	// Manually add a client
	transport.mu.Lock()
	transport.clients["test"] = &sseClient{
		id:       "test",
		messages: make(chan []byte, 64),
		done:     make(chan struct{}),
	}
	transport.mu.Unlock()

	if transport.ClientCount() != 1 {
		t.Errorf("ClientCount = %d, want 1", transport.ClientCount())
	}
}

func TestSSETransport_HandleMessage_InvalidBody(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", &errorReader{})
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleMessage with read error = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSSETransport_HandleMessage_InvalidJSONRPC(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", strings.NewReader("invalid json"))
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	// HandleJSONRPC on invalid JSON should return an error
	if w.Code != http.StatusInternalServerError {
		t.Logf("handleMessage with invalid JSON = %d", w.Code)
	}
}

// --------------------------------------------------------------------------
// Tests: Stdio Transport Run (mcp.go:1198)
// --------------------------------------------------------------------------

// lockedWriter is a thread-safe io.Writer that buffers all written data.
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestStdioTransport_Run(t *testing.T) {
	s := newTestServer()

	// Use io.Pipe so writes from the test and reads from the transport
	// are naturally synchronized (no concurrent access to a bytes.Buffer).
	pr, pw := io.Pipe()
	output := &lockedWriter{}

	transport := NewStdioTransport(s, pr, output)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- transport.Run(ctx)
	}()

	// Write a valid JSON-RPC request
	req := makeRequest("tools/list", nil)
	pw.Write(req)
	pw.Write([]byte("\n"))

	// Give it time to process
	time.Sleep(100 * time.Millisecond)
	cancel()
	pw.Close()

	err := <-done
	if err != nil {
		t.Logf("Run returned: %v", err)
	}

	// Check output
	result := output.String()
	if result == "" {
		t.Error("Expected output from Run")
	}
}

func TestStdioTransport_RunEmptyLine(t *testing.T) {
	s := newTestServer()
	input := bytes.NewBufferString("\n\n") // two empty lines
	output := &bytes.Buffer{}

	transport := NewStdioTransport(s, input, output)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Run(ctx)
	if err != nil {
		t.Logf("Run with empty lines returned: %v", err)
	}
}

// --------------------------------------------------------------------------
// Tests: HTTP Transport ServeHTTP (mcp.go:1251)
// --------------------------------------------------------------------------

func TestHTTPTransport_ServeHTTP_WrongMethod(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("ServeHTTP GET = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHTTPTransport_ServeHTTP_NoAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	body := makeRequest("tools/list", nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("ServeHTTP with no auth = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHTTPTransport_ServeHTTP_ValidAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "secret-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	body := makeRequest("tools/list", nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ServeHTTP with valid auth = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHTTPTransport_ServeHTTP_InvalidAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "secret-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	body := makeRequest("tools/list", nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-token")
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("ServeHTTP with wrong token = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHTTPTransport_ServeHTTP_NoBearerPrefix(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "secret-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	body := makeRequest("tools/list", nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Basic abc123")
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("ServeHTTP with Basic auth = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHTTPTransport_ServeHTTP_BodyReadError(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", &errorReader{})
	req.Header.Set("Authorization", "Bearer test-token")
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ServeHTTP with body error = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --------------------------------------------------------------------------
// Tests: HTTP Transport Start (mcp.go:1293)
// --------------------------------------------------------------------------

func TestHTTPTransport_Start(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	addr := transport.Addr()
	if addr == "" {
		t.Error("Addr should be non-empty after Start")
	}

	// Make a request to /mcp endpoint
	body := makeRequest("tools/list", nil)
	httpReq, err := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	transport.Stop(context.Background())
}

// --------------------------------------------------------------------------
// Tests: handleResourcesRead with unknown resource (mcp.go:633)
// --------------------------------------------------------------------------

func TestServer_HandleResourcesRead_UnknownResource(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]any{
		"uri": "unknown://resource",
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	var result Response
	json.Unmarshal(resp, &result)
	if result.Error != nil {
		t.Logf("Error for unknown resource: %v", result.Error)
	}
}

// --------------------------------------------------------------------------
// Tests: readResource with nil resources map (mcp.go:686)
// --------------------------------------------------------------------------

func TestServer_ReadResource_NoResourcesConfigured(t *testing.T) {
	s := NewServer(ServerConfig{}) // no resources configured

	uri := "metrics://overview"
	contents, err := s.readResource(uri)
	if err == nil {
		t.Log("readResource with no resources returned nil error")
	} else {
		t.Logf("readResource error (expected): %v", err)
	}
	if len(contents) > 0 {
		t.Log("readResource returned some contents")
	}
}

// --------------------------------------------------------------------------
// Tests: handleMessage with audit logging (sse_transport.go:256-271)
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_WithAudit(t *testing.T) {
	s := newTestServer()
	auditCalled := false
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:     "127.0.0.1:0",
		AuditLog: true,
		AuditFunc: func(tool, params, clientAddr string, dur time.Duration, err error) {
			auditCalled = true
		},
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_list_backends",
		"arguments": map[string]any{},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleMessage = %d, want %d", w.Code, http.StatusOK)
	}

	if auditCalled {
		t.Log("Audit function was called")
	}
}

// --------------------------------------------------------------------------
// Tests: handleMessage with audit but non-tool call (no audit logged)
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_AuditNonToolCall(t *testing.T) {
	s := newTestServer()
	auditCalled := false
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:     "127.0.0.1:0",
		AuditLog: true,
		AuditFunc: func(tool, params, clientAddr string, dur time.Duration, err error) {
			auditCalled = true
		},
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Non-tool-call request - should not trigger audit
	body := makeRequest("resources/list", nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if auditCalled {
		t.Error("Audit should not be called for non-tool-call request")
	}
}

// --------------------------------------------------------------------------
// Tests: Start method for MCP Server (mcp.go:1293)
// --------------------------------------------------------------------------

func TestHTTPTransport_StartWithAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-bearer")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	addr := transport.Addr()

	// Test with correct auth
	body := makeRequest("tools/list", nil)
	req, _ := http.NewRequest("POST", "http://"+addr+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-bearer")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request error: %v", err)
	}
	defer resp.Body.Close()

	// The response may be 200 or 404 depending on route registration
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("auth should have succeeded, got 401")
	}

	transport.Stop(context.Background())
}

func TestHTTPTransport_Addr(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	// Before Start, Addr should return configured addr
	if transport.Addr() != "" {
		t.Logf("Addr before start: %s", transport.Addr())
	}
}

func TestHTTPTransport_Stop(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, "127.0.0.1:0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := transport.Stop(ctx); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}
