package l7

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestIsSSERequest(t *testing.T) {
	tests := []struct {
		name      string
		accept    string
		wantIsSSE bool
	}{
		{
			name:      "SSE request",
			accept:    "text/event-stream",
			wantIsSSE: true,
		},
		{
			name:      "SSE with quality value",
			accept:    "text/event-stream;q=0.9",
			wantIsSSE: true,
		},
		{
			name:      "Multiple types with SSE",
			accept:    "text/html, text/event-stream, */*",
			wantIsSSE: true,
		},
		{
			name:      "Regular HTTP request",
			accept:    "text/html",
			wantIsSSE: false,
		},
		{
			name:      "JSON request",
			accept:    "application/json",
			wantIsSSE: false,
		},
		{
			name:      "Empty accept",
			accept:    "",
			wantIsSSE: false,
		},
		{
			name:      "Wildcard accept",
			accept:    "*/*",
			wantIsSSE: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/events", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			got := IsSSERequest(req)
			if got != tt.wantIsSSE {
				t.Errorf("IsSSERequest() = %v, want %v", got, tt.wantIsSSE)
			}
		})
	}
}

func TestIsSSEResponse(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantIsSSE   bool
	}{
		{
			name:        "SSE response",
			contentType: "text/event-stream",
			wantIsSSE:   true,
		},
		{
			name:        "SSE with charset",
			contentType: "text/event-stream;charset=utf-8",
			wantIsSSE:   true,
		},
		{
			name:        "HTML response",
			contentType: "text/html",
			wantIsSSE:   false,
		},
		{
			name:        "JSON response",
			contentType: "application/json",
			wantIsSSE:   false,
		},
		{
			name:        "Empty content type",
			contentType: "",
			wantIsSSE:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: make(http.Header),
			}
			if tt.contentType != "" {
				resp.Header.Set("Content-Type", tt.contentType)
			}

			got := IsSSEResponse(resp)
			if got != tt.wantIsSSE {
				t.Errorf("IsSSEResponse() = %v, want %v", got, tt.wantIsSSE)
			}
		})
	}
}

func TestDefaultSSEConfig(t *testing.T) {
	config := DefaultSSEConfig()

	if !config.EnableSSE {
		t.Error("EnableSSE should be true by default")
	}
	if config.MaxEventSize != 1024*1024 {
		t.Errorf("MaxEventSize = %v, want 1MB", config.MaxEventSize)
	}
	if config.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", config.IdleTimeout)
	}
}

func TestNewSSEHandler(t *testing.T) {
	config := DefaultSSEConfig()
	handler := NewSSEHandler(config)

	if handler == nil {
		t.Fatal("NewSSEHandler() returned nil")
	}
	if handler.config != config {
		t.Error("Handler config mismatch")
	}
}

func TestNewSSEHandler_NilConfig(t *testing.T) {
	handler := NewSSEHandler(nil)

	if handler == nil {
		t.Fatal("NewSSEHandler(nil) returned nil")
	}
	if handler.config == nil {
		t.Error("Handler config should use defaults when nil")
	}
}

func TestSSEHandler_Disabled(t *testing.T) {
	config := &SSEConfig{EnableSSE: false}
	handler := NewSSEHandler(config)

	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	rec := httptest.NewRecorder()
	err := handler.HandleSSE(rec, req, be)

	if err == nil || err.Error() != "sse disabled" {
		t.Errorf("Expected 'sse disabled' error, got: %v", err)
	}
}

func TestSSEHandler_BackendMaxConnections(t *testing.T) {
	handler := NewSSEHandler(nil)

	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	be.SetState(backend.StateUp)
	be.MaxConns = 1

	// First connection should acquire
	if !be.AcquireConn() {
		t.Fatal("Failed to acquire first connection")
	}

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	rec := httptest.NewRecorder()
	err := handler.HandleSSE(rec, req, be)

	if err == nil || err.Error() != "backend at max connections" {
		t.Errorf("Expected 'backend at max connections' error, got: %v", err)
	}

	be.ReleaseConn()
}

func TestParseSSEEvent(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantEvent *SSEEvent
	}{
		{
			name:  "Simple message",
			input: "data: Hello\n\n",
			wantEvent: &SSEEvent{
				Data: []byte("Hello"),
			},
		},
		{
			name:  "Message with event type",
			input: "event: update\ndata: Hello\n\n",
			wantEvent: &SSEEvent{
				Event: "update",
				Data:  []byte("Hello"),
			},
		},
		{
			name:  "Message with ID",
			input: "id: 123\ndata: Hello\n\n",
			wantEvent: &SSEEvent{
				ID:   "123",
				Data: []byte("Hello"),
			},
		},
		{
			name:  "Message with retry",
			input: "retry: 5000\ndata: Hello\n\n",
			wantEvent: &SSEEvent{
				Data:  []byte("Hello"),
				Retry: 5000,
			},
		},
		{
			name:  "Multi-line data",
			input: "data: Hello\ndata: World\n\n",
			wantEvent: &SSEEvent{
				Data: []byte("Hello\nWorld"),
			},
		},
		{
			name:  "Complete event",
			input: "id: 123\nevent: update\ndata: Hello World\nretry: 5000\n\n",
			wantEvent: &SSEEvent{
				ID:    "123",
				Event: "update",
				Data:  []byte("Hello World"),
				Retry: 5000,
			},
		},
		{
			name:  "With comment",
			input: ": This is a comment\ndata: Hello\n\n",
			wantEvent: &SSEEvent{
				Data: []byte("Hello"),
			},
		},
		{
			name:      "Empty data",
			input:     "\n",
			wantEvent: &SSEEvent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := ParseSSEEvent([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseSSEEvent error: %v", err)
			}

			if event.ID != tt.wantEvent.ID {
				t.Errorf("ID = %q, want %q", event.ID, tt.wantEvent.ID)
			}
			if event.Event != tt.wantEvent.Event {
				t.Errorf("Event = %q, want %q", event.Event, tt.wantEvent.Event)
			}
			if string(event.Data) != string(tt.wantEvent.Data) {
				t.Errorf("Data = %q, want %q", string(event.Data), string(tt.wantEvent.Data))
			}
			if event.Retry != tt.wantEvent.Retry {
				t.Errorf("Retry = %d, want %d", event.Retry, tt.wantEvent.Retry)
			}
		})
	}
}

func TestFormatSSEEvent(t *testing.T) {
	tests := []struct {
		name  string
		event *SSEEvent
		want  string
	}{
		{
			name: "Simple message",
			event: &SSEEvent{
				Data: []byte("Hello"),
			},
			want: "data: Hello\n\n",
		},
		{
			name: "Message with event type",
			event: &SSEEvent{
				Event: "update",
				Data:  []byte("Hello"),
			},
			want: "event: update\ndata: Hello\n\n",
		},
		{
			name: "Message with ID",
			event: &SSEEvent{
				ID:   "123",
				Data: []byte("Hello"),
			},
			want: "id: 123\ndata: Hello\n\n",
		},
		{
			name: "Message with retry",
			event: &SSEEvent{
				Data:  []byte("Hello"),
				Retry: 5000,
			},
			want: "retry: 5000\ndata: Hello\n\n",
		},
		{
			name: "Complete event",
			event: &SSEEvent{
				ID:    "123",
				Event: "update",
				Data:  []byte("Hello"),
				Retry: 5000,
			},
			want: "id: 123\nevent: update\nretry: 5000\ndata: Hello\n\n",
		},
		{
			name: "Multi-line data",
			event: &SSEEvent{
				Data: []byte("Hello\nWorld"),
			},
			want: "data: Hello\ndata: World\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSSEEvent(tt.event)
			if string(got) != tt.want {
				t.Errorf("FormatSSEEvent() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestParseAndFormatRoundTrip(t *testing.T) {
	original := &SSEEvent{
		ID:    "msg-123",
		Event: "notification",
		Data:  []byte("Hello, World!"),
		Retry: 3000,
	}

	formatted := FormatSSEEvent(original)
	parsed, err := ParseSSEEvent(formatted)
	if err != nil {
		t.Fatalf("ParseSSEEvent error: %v", err)
	}

	if parsed.ID != original.ID {
		t.Errorf("ID mismatch: got %q, want %q", parsed.ID, original.ID)
	}
	if parsed.Event != original.Event {
		t.Errorf("Event mismatch: got %q, want %q", parsed.Event, original.Event)
	}
	if string(parsed.Data) != string(original.Data) {
		t.Errorf("Data mismatch: got %q, want %q", string(parsed.Data), string(original.Data))
	}
	if parsed.Retry != original.Retry {
		t.Errorf("Retry mismatch: got %d, want %d", parsed.Retry, original.Retry)
	}
}

func TestCopySSEHeaders(t *testing.T) {
	src := http.Header{
		"Content-Type":  []string{"text/event-stream"},
		"Cache-Control": []string{"no-cache"},
		"X-Custom":      []string{"value"},
		"Connection":    []string{"keep-alive"}, // hop-by-hop, should be skipped
	}

	dst := make(http.Header)
	copySSEHeaders(dst, src)

	// Should have Content-Type and Cache-Control
	if dst.Get("Content-Type") != "text/event-stream" {
		t.Error("Content-Type header not copied")
	}
	if dst.Get("Cache-Control") != "no-cache" {
		t.Error("Cache-Control header not copied")
	}
	if dst.Get("X-Custom") != "value" {
		t.Error("X-Custom header not copied")
	}

	// Should not have hop-by-hop headers
	if dst.Get("Connection") != "" {
		t.Error("Connection header should not be copied")
	}
}

func TestNewSSEProxy(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	sseConfig := DefaultSSEConfig()
	sseProxy := NewSSEProxy(httpProxy, sseConfig)

	if sseProxy == nil {
		t.Fatal("NewSSEProxy() returned nil")
	}
	if sseProxy.httpProxy != httpProxy {
		t.Error("HTTP proxy mismatch")
	}
	if sseProxy.sseHandler == nil {
		t.Error("SSE handler should not be nil")
	}
}

func TestSSEProxy_ServeHTTP_NonSSE(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	sseConfig := DefaultSSEConfig()
	sseProxy := NewSSEProxy(httpProxy, sseConfig)

	// Regular HTTP request (not SSE)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	// Should route to HTTP proxy
	sseProxy.ServeHTTP(rec, req)

	// Response code will depend on proxy setup, but should not panic
}

func TestSSEProxy_ServeHTTP_SSERequest(t *testing.T) {
	httpProxy, _, _ := setupTestProxy(t)
	sseConfig := DefaultSSEConfig()
	sseProxy := NewSSEProxy(httpProxy, sseConfig)

	// SSE request
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	// Will fail because no healthy backends, but should not panic
	sseProxy.ServeHTTP(rec, req)

	// Should get an error response
	if rec.Code == 200 {
		t.Error("Expected non-200 response for failed SSE request")
	}
}

func TestSSEHandler_prepareSSERequest(t *testing.T) {
	handler := NewSSEHandler(nil)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "123")
	req.Host = "example.com"

	outReq, err := handler.prepareSSERequest(req, be)
	if err != nil {
		t.Fatalf("prepareSSERequest error: %v", err)
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

	// Check X-Forwarded headers
	if outReq.Header.Get("X-Forwarded-For") == "" {
		t.Error("X-Forwarded-For header not set")
	}
	if outReq.Header.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("X-Forwarded-Proto = %v, want http", outReq.Header.Get("X-Forwarded-Proto"))
	}

	// Check Last-Event-ID is preserved
	if outReq.Header.Get("Last-Event-ID") != "123" {
		t.Error("Last-Event-ID header not preserved")
	}
}

func TestSSEHandler_prepareSSERequest_NoAccept(t *testing.T) {
	handler := NewSSEHandler(nil)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	req := httptest.NewRequest("GET", "/events", nil)
	// No Accept header set

	outReq, err := handler.prepareSSERequest(req, be)
	if err != nil {
		t.Fatalf("prepareSSERequest error: %v", err)
	}

	// Should set Accept header to text/event-stream
	if outReq.Header.Get("Accept") != "text/event-stream" {
		t.Errorf("Accept = %v, want text/event-stream", outReq.Header.Get("Accept"))
	}
}

// mockFlusher is a ResponseRecorder that implements http.Flusher
type mockFlusher struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (m *mockFlusher) Flush() {
	m.flushed = true
}

func TestSSEHandler_streamSSEResponse(t *testing.T) {
	handler := NewSSEHandler(nil)

	// Create a mock SSE response
	sseData := "data: message 1\n\ndata: message 2\n\n"
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(sseData))),
	}

	rec := &mockFlusher{
		ResponseRecorder: httptest.NewRecorder(),
	}

	be := backend.NewBackend("backend-1", "127.0.0.1:8080")
	err := handler.streamSSEResponse(rec, resp, be)

	// Should complete without error (EOF is expected)
	if err != nil {
		t.Errorf("streamSSEResponse error: %v", err)
	}

	// Check Content-Type was copied
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Error("Content-Type header not set")
	}

	// Check body was written
	body := rec.Body.String()
	if body != sseData {
		t.Errorf("Body = %q, want %q", body, sseData)
	}
}

func TestTimeoutError(t *testing.T) {
	err := &timeoutError{}

	if err.Error() != "timeout" {
		t.Errorf("Error() = %q, want 'timeout'", err.Error())
	}
	if !err.Timeout() {
		t.Error("Timeout() should return true")
	}
	if !err.Temporary() {
		t.Error("Temporary() should return true")
	}
}

func TestSSEHandler_createSSETransport(t *testing.T) {
	handler := NewSSEHandler(nil)
	transport := handler.createSSETransport()
	if transport == nil {
		t.Fatal("createSSETransport() returned nil")
	}
	if !transport.DisableCompression {
		t.Error("DisableCompression should be true for SSE transport")
	}
}

func TestSSEHandler_copyRegularResponse(t *testing.T) {
	handler := NewSSEHandler(nil)

	// Create a mock regular HTTP response
	bodyData := `{"message": "Hello"}`
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(bodyData))),
	}

	rec := httptest.NewRecorder()

	err := handler.copyRegularResponse(rec, resp)
	if err != nil {
		t.Errorf("copyRegularResponse error: %v", err)
	}

	// Check Content-Type was copied
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type header not set")
	}

	// Check body was written
	body := rec.Body.String()
	if body != bodyData {
		t.Errorf("Body = %q, want %q", body, bodyData)
	}
}

func TestSSEHandler_HandleSSE_FullRoundTrip(t *testing.T) {
	// Create a mock SSE backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		// Send a few events then close
		for i := 0; i < 3; i++ {
			w.Write([]byte("data: message " + string(rune('1'+i)) + "\n\n"))
			flusher.Flush()
		}
	}))
	defer backendServer.Close()

	handler := NewSSEHandler(nil)

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("sse-backend-1", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	rec := &mockFlusher{
		ResponseRecorder: httptest.NewRecorder(),
	}

	err := handler.HandleSSE(rec, req, be)
	if err != nil {
		t.Fatalf("HandleSSE() error = %v", err)
	}

	// Check Content-Type was set
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}

	// Check body contains SSE data
	body := rec.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}
}

func TestSSEHandler_HandleSSE_NonSSEBackendResponse(t *testing.T) {
	// Create a backend that returns regular HTTP (not SSE)
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"error": "not an SSE endpoint"}`))
	}))
	defer backendServer.Close()

	handler := NewSSEHandler(nil)

	addr := backendServer.Listener.Addr().String()
	be := backend.NewBackend("sse-backend-json", addr)
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	rec := httptest.NewRecorder()

	err := handler.HandleSSE(rec, req, be)
	if err != nil {
		t.Fatalf("HandleSSE() error = %v", err)
	}

	// Should fall back to regular response copy
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
}

func TestSSEHandler_HandleSSE_BackendError(t *testing.T) {
	handler := NewSSEHandler(nil)

	// Use an address that refuses connections
	be := backend.NewBackend("sse-backend-bad", "127.0.0.1:1")
	be.SetState(backend.StateUp)

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	rec := httptest.NewRecorder()
	err := handler.HandleSSE(rec, req, be)
	if err == nil {
		t.Error("Expected error when backend connection fails")
	}
	if err != nil && !bytes.Contains([]byte(err.Error()), []byte("backend request failed")) {
		t.Errorf("Expected 'backend request failed' error, got: %v", err)
	}
}

func TestSSEHandler_streamSSEResponse_NoFlusher(t *testing.T) {
	handler := NewSSEHandler(nil)

	sseData := "data: hello\n\n"
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(sseData))),
	}

	// Use a regular ResponseRecorder which does NOT implement http.Flusher
	rec := httptest.NewRecorder()

	be := backend.NewBackend("sse-backend-nf", "127.0.0.1:8080")
	err := handler.streamSSEResponse(rec, resp, be)

	// Should still work but just copy the body without flushing
	if err != nil {
		t.Errorf("streamSSEResponse error: %v", err)
	}

	body := rec.Body.String()
	if body != sseData {
		t.Errorf("Body = %q, want %q", body, sseData)
	}
}
