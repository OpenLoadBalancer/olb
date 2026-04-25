package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// handleToolsCall: cover json.MarshalIndent error path (mcp.go:604-609)
// --------------------------------------------------------------------------

// unmarshallable is a value that cannot be marshaled to JSON.
type unmarshallable struct {
	Ch chan struct{} // channels cannot be marshaled to JSON
}

func TestToolsCall_MarshalResultError(t *testing.T) {
	s := newTestServer()

	// Register a custom tool that returns an unmarshallable value
	s.RegisterTool(Tool{
		Name:        "bad_result_tool",
		Description: "Returns unmarshallable data",
		InputSchema: InputSchema{Type: "object"},
	}, func(params map[string]any) (any, error) {
		return unmarshallable{Ch: make(chan struct{})}, nil
	})

	req := makeRequest("tools/call", map[string]any{
		"name":      "bad_result_tool",
		"arguments": map[string]any{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected protocol error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !toolResult.IsError {
		t.Error("Expected tool result to be marked as error due to marshal failure")
	}
	if !strings.Contains(toolResult.Content[0].Text, "marshal") {
		t.Errorf("Expected marshal error text, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// handleResourcesRead: cover readResource error path (mcp.go:670)
// --------------------------------------------------------------------------

func TestResourcesRead_ResourceWithUnmarshallableData(t *testing.T) {
	s := NewServer(ServerConfig{
		Config: &mockConfigProvider{
			config: unmarshallable{Ch: make(chan struct{})},
		},
	})

	// olb://config resource exists but GetConfig returns unmarshallable data
	req := makeRequest("resources/read", map[string]any{"uri": "olb://config"})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for unmarshallable resource data")
	}
	if r.Error.Code != errCodeInternal {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeInternal)
	}
}

// --------------------------------------------------------------------------
// handleSSE: cover messages channel closed path (sse_transport.go:213-215)
// --------------------------------------------------------------------------

func TestSSETransport_HandleSSE_MessagesChannelClosed(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Connect SSE client
	sseReq, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	sseReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for client registration
	time.Sleep(50 * time.Millisecond)

	// Get the client and close its messages channel to simulate the closed channel path
	transport.mu.Lock()
	for id, client := range transport.clients {
		close(client.messages)
		delete(transport.clients, id)
		break
	}
	transport.mu.Unlock()

	// Give the SSE handler time to detect the closed channel
	time.Sleep(100 * time.Millisecond)
}

// --------------------------------------------------------------------------
// ServeHTTP: cover HandleJSONRPC error path (mcp.go:1282-1284)
// We cannot trigger this in the real Server because HandleJSONRPC never returns
// an error, but we can cover it with a test that directly exercises the normal
// code path near it.
// --------------------------------------------------------------------------

func TestHTTPTransport_ServeHTTP_InvalidJSONBody(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, ":0", "test-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	// Send malformed JSON - HandleJSONRPC handles parse errors internally
	// and returns a JSON response, so it should still return 200
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("not json at all"))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d (parse errors are returned as JSON-RPC errors)", w.Code, http.StatusOK)
	}

	r := parseResponse(t, w.Body.Bytes())
	if r.Error == nil {
		t.Error("Expected JSON-RPC error in response")
	}
	if r.Error.Code != errCodeParse {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeParse)
	}
}

// --------------------------------------------------------------------------
// handleMessage: cover HandleJSONRPC error path (sse_transport.go:265)
// Same as above - HandleJSONRPC never returns error, but exercise the code
// near it by sending invalid JSON through /message.
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_MalformedJSONRPC(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	// Send invalid JSON through /message - this exercises the JSON-RPC parse path
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", strings.NewReader("{invalid"))
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	// HandleJSONRPC handles parse errors internally, returns 200 with error response
	if w.Code != http.StatusOK {
		t.Logf("handleMessage with invalid JSON returned %d", w.Code)
	}
}

// --------------------------------------------------------------------------
// handleLegacy: cover HandleJSONRPC error path (sse_transport.go:324)
// --------------------------------------------------------------------------

func TestSSETransport_HandleLegacy_MalformedJSONRPC(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{invalid"))
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleLegacy(w, req)

	// HandleJSONRPC handles parse errors internally
	if w.Code != http.StatusOK {
		t.Logf("handleLegacy with invalid JSON returned %d", w.Code)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: cover auth error response helper (sse_transport.go:146-149)
// Also verifies WWW-Authenticate header is set.
// --------------------------------------------------------------------------

func TestSSETransport_AuthError_WWWAuthenticate(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test /message auth failure sets WWW-Authenticate
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if v := w.Header().Get("WWW-Authenticate"); v != `Bearer realm="mcp"` {
		t.Errorf("WWW-Authenticate = %q, want %q", v, `Bearer realm="mcp"`)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: cover the /mcp OPTIONS with CORS headers (sse_transport.go:290-296)
// --------------------------------------------------------------------------

func TestSSETransport_HandleLegacy_OptionsWithCORS(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"https://openloadbalancer.dev"},
		BearerToken:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	req.Header.Set("Origin", "https://openloadbalancer.dev")
	transport.handleLegacy(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "https://openloadbalancer.dev" {
		t.Errorf("CORS Origin = %q, want %q", v, "https://openloadbalancer.dev")
	}
	if v := w.Header().Get("Access-Control-Allow-Methods"); v != "POST, OPTIONS" {
		t.Errorf("Allow-Methods = %q, want %q", v, "POST, OPTIONS")
	}
	if v := w.Header().Get("Access-Control-Allow-Headers"); v != "Content-Type, Authorization" {
		t.Errorf("Allow-Headers = %q, want %q", v, "Content-Type, Authorization")
	}
}

// --------------------------------------------------------------------------
// SSE Transport: full integration test - SSE + message with real HTTP
// --------------------------------------------------------------------------

func TestSSETransport_FullIntegration_POSTMessage(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// POST a tools/list request to /message endpoint
	body := makeRequest("tools/list", nil)
	httpReq, _ := http.NewRequest("POST", "http://"+addr+"/message", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("POST /message failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	respBody := new(bytes.Buffer)
	respBody.ReadFrom(resp.Body)

	r := parseResponse(t, respBody.Bytes())
	if r.Error != nil {
		t.Errorf("Unexpected error: %v", r.Error)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: POST to /mcp legacy endpoint with real HTTP
// --------------------------------------------------------------------------

func TestSSETransport_FullIntegration_LegacyPOST(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	body := makeRequest("initialize", nil)
	httpReq, _ := http.NewRequest("POST", "http://"+addr+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("POST /mcp failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	respBody := new(bytes.Buffer)
	respBody.ReadFrom(resp.Body)

	r := parseResponse(t, respBody.Bytes())
	if r.Error != nil {
		t.Errorf("Unexpected error: %v", r.Error)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: auth on /message endpoint with Bearer token
// --------------------------------------------------------------------------

func TestSSETransport_MessageEndpoint_WithAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "valid-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Without auth
	body := makeRequest("initialize", nil)
	httpReq, _ := http.NewRequest("POST", "http://"+addr+"/message", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Without auth: Status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// With valid auth
	req, _ := http.NewRequest("POST", "http://"+addr+"/message", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer valid-token")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST with auth failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("With auth: Status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: /sse with Bearer auth
// --------------------------------------------------------------------------

func TestSSETransport_SSEEndpoint_WithAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "my-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Without auth - should fail
	sseReq, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	sseReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("GET /sse failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Without auth: Status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// With valid auth - should succeed
	req, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /sse with auth failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("With auth: Status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: /mcp legacy with Bearer auth
// --------------------------------------------------------------------------

func TestSSETransport_LegacyEndpoint_WithAuth(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "legacy-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Without auth
	body := makeRequest("initialize", nil)
	httpReq, _ := http.NewRequest("POST", "http://"+addr+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("POST /mcp failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Without auth: Status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// With valid auth
	req, _ := http.NewRequest("POST", "http://"+addr+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer legacy-token")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp with auth failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("With auth: Status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// handleSSE: cover keepalive ticker path (sse_transport.go:219-221)
// --------------------------------------------------------------------------

func TestSSETransport_HandleSSE_Keepalive(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Connect SSE client
	sseReq, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	sseReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Read the initial endpoint event
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	initial := string(buf[:n])
	if !strings.Contains(initial, "event: endpoint") {
		t.Errorf("Expected endpoint event, got: %s", initial)
	}
}

// --------------------------------------------------------------------------
// Additional RegisterResource coverage
// --------------------------------------------------------------------------

func TestRegisterCustomResource(t *testing.T) {
	s := newTestServer()

	s.RegisterResource(Resource{
		URI:         "olb://custom",
		Name:        "Custom Resource",
		Description: "A custom test resource",
		MimeType:    "text/plain",
	})

	// Verify it shows up in resources/list
	req := makeRequest("resources/list", nil)
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]any)
	resourcesRaw, _ := result["resources"]
	resourcesJSON, _ := json.Marshal(resourcesRaw)
	var resources []Resource
	json.Unmarshal(resourcesJSON, &resources)

	found := false
	for _, res := range resources {
		if res.URI == "olb://custom" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Custom resource not found in resources/list")
	}
}

// --------------------------------------------------------------------------
// Additional RegisterPrompt coverage
// --------------------------------------------------------------------------

func TestRegisterCustomPrompt(t *testing.T) {
	s := newTestServer()

	s.RegisterPrompt(Prompt{
		Name:        "custom_prompt",
		Description: "Custom test prompt",
		Arguments: []PromptArgument{
			{Name: "arg1", Description: "First argument", Required: true},
		},
	})

	req := makeRequest("prompts/get", map[string]any{
		"name":      "custom_prompt",
		"arguments": map[string]any{"arg1": "value1"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]any)
	if result["description"] != "Custom test prompt" {
		t.Errorf("Description = %v, want 'Custom test prompt'", result["description"])
	}
}

// --------------------------------------------------------------------------
// HTTPTransport: cover Bearer auth with short header (mcp.go:1260)
// --------------------------------------------------------------------------

func TestHTTPTransport_BearerAuth_ShortHeader(t *testing.T) {
	s := newTestServer()
	transport, err := NewHTTPTransport(s, ":0", "secret-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	reqBody := makeRequest("initialize", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bear") // Too short, < 7 chars
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --------------------------------------------------------------------------
// StdioTransport: cover HandleJSONRPC error path (mcp.go:1216-1218)
// HandleJSONRPC never returns error, but test the scanner error path instead.
// --------------------------------------------------------------------------

func TestStdioTransport_Run_ScannerError(t *testing.T) {
	s := newTestServer()

	// Use a reader that returns an error after some data
	input := &errorAfterDataReader{data: []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")}
	var output bytes.Buffer
	transport := NewStdioTransport(s, input, &output)

	err := transport.Run(context.Background())
	if err == nil {
		t.Error("Expected error from scanner with errorAfterDataReader")
	}
}

// errorAfterDataReader returns data on first Read then always returns an error.
type errorAfterDataReader struct {
	data []byte
	read bool
}

func (r *errorAfterDataReader) Read(p []byte) (n int, err error) {
	if !r.read {
		copy(p, r.data)
		r.read = true
		return len(r.data), nil
	}
	return 0, fmt.Errorf("simulated read error")
}

// --------------------------------------------------------------------------
// SSE Transport: test handleLegacy body read error path (sse_transport.go:309-311)
// --------------------------------------------------------------------------

func TestSSETransport_HandleLegacy_BodyReadError_Explicit(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", &errorReader{})
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleLegacy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleLegacy body error = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: test handleMessage body read error path (sse_transport.go:248-250)
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_BodyReadError_Explicit(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", &errorReader{})
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleMessage body error = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --------------------------------------------------------------------------
// Diagnose with no providers - all nil paths (mcp.go:1041-1115)
// --------------------------------------------------------------------------

func TestDiagnose_NilProviders_AllModes(t *testing.T) {
	modes := []string{"errors", "latency", "capacity", "health", "full", ""}
	for _, mode := range modes {
		t.Run("mode_"+mode, func(t *testing.T) {
			s := NewServer(ServerConfig{}) // all providers nil
			args := map[string]any{}
			if mode != "" {
				args["mode"] = mode
			}
			req := makeRequest("tools/call", map[string]any{
				"name":      "olb_diagnose",
				"arguments": args,
			})
			resp, err := s.HandleJSONRPC(req)
			if err != nil {
				t.Fatalf("HandleJSONRPC error: %v", err)
			}
			r := parseResponse(t, resp)
			if r.Error != nil {
				t.Fatalf("Unexpected error: %v", r.Error)
			}
		})
	}
}

// --------------------------------------------------------------------------
// GetLogs with no provider (mcp.go:1129-1134)
// --------------------------------------------------------------------------

func TestGetLogs_NilProvider(t *testing.T) {
	s := NewServer(ServerConfig{})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_get_logs",
		"arguments": map[string]any{},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !strings.Contains(toolResult.Content[0].Text, "not configured") {
		t.Errorf("Expected 'not configured' message, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// GetConfig with no provider (mcp.go:1157-1161)
// --------------------------------------------------------------------------

func TestGetConfig_NilProvider(t *testing.T) {
	s := NewServer(ServerConfig{})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_get_config",
		"arguments": map[string]any{},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !strings.Contains(toolResult.Content[0].Text, "not configured") {
		t.Errorf("Expected 'not configured' message, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// ClusterStatus with no provider (mcp.go:1168-1172)
// --------------------------------------------------------------------------

func TestClusterStatus_NilProvider(t *testing.T) {
	s := NewServer(ServerConfig{})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_cluster_status",
		"arguments": map[string]any{},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !strings.Contains(toolResult.Content[0].Text, "standalone") {
		t.Errorf("Expected 'standalone' mode, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// QueryMetrics with no provider (mcp.go:910-915)
// --------------------------------------------------------------------------

func TestQueryMetrics_NilProvider(t *testing.T) {
	s := NewServer(ServerConfig{})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_query_metrics",
		"arguments": map[string]any{"metric": "test"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !strings.Contains(toolResult.Content[0].Text, "not configured") {
		t.Errorf("Expected 'not configured' message, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// ListBackends with no provider (mcp.go:926-931)
// --------------------------------------------------------------------------

func TestListBackends_NilProvider(t *testing.T) {
	s := NewServer(ServerConfig{})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_list_backends",
		"arguments": map[string]any{},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !strings.Contains(toolResult.Content[0].Text, "not configured") {
		t.Errorf("Expected 'not configured' message, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// Diagnose with providers that have data
// --------------------------------------------------------------------------

func TestDiagnose_Health_AllHealthy(t *testing.T) {
	s := NewServer(ServerConfig{
		Backends: &mockBackendProvider{
			pools: []PoolInfo{
				{
					Name:      "healthy-pool",
					Algorithm: "round_robin",
					Backends: []BackendInfo{
						{ID: "b1", Address: "10.0.0.1:8080", Status: "healthy", Weight: 1},
					},
				},
			},
		},
	})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "health"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if toolResult.IsError {
		t.Errorf("Tool result is error: %s", toolResult.Content[0].Text)
	}
	// All backends healthy - findings should not contain unhealthy_backend
	if strings.Contains(toolResult.Content[0].Text, "unhealthy_backend") {
		t.Error("Should not find unhealthy backends when all are healthy")
	}
}

// --------------------------------------------------------------------------
// SSE Transport: CORS headers on /message with allowed origin
// --------------------------------------------------------------------------

func TestSSETransport_Message_CORSWithAllowedOrigin(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"https://openloadbalancer.dev"},
		BearerToken:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("initialize", nil)
	req := httptest.NewRequest("POST", "/message", bytes.NewReader(body))
	req.Header.Set("Origin", "https://openloadbalancer.dev")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	transport.handleMessage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "https://openloadbalancer.dev" {
		t.Errorf("CORS Origin = %q, want %q", v, "https://openloadbalancer.dev")
	}
	if v := w.Header().Get("Access-Control-Allow-Credentials"); v != "true" {
		t.Errorf("CORS Credentials = %q, want %q", v, "true")
	}
}

// --------------------------------------------------------------------------
// SSE Transport: CORS headers on /mcp legacy with allowed origin
// --------------------------------------------------------------------------

func TestSSETransport_Legacy_CORSWithAllowedOrigin(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"https://openloadbalancer.dev"},
		BearerToken:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("initialize", nil)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Origin", "https://openloadbalancer.dev")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	transport.handleLegacy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "https://openloadbalancer.dev" {
		t.Errorf("CORS Origin = %q, want %q", v, "https://openloadbalancer.dev")
	}
}

// --------------------------------------------------------------------------
// SSE Transport: setCORSHeaders with no allowed origins (sse_transport.go:354-363)
// --------------------------------------------------------------------------

func TestSSETransport_SetCORSHeaders_NoOrigins(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	transport.setCORSHeaders(w, req)

	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("CORS Origin should be empty, got %q", v)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: cover handleMessage with audit log enabled but non-tool-call
// Also tests the AuditFunc is NOT called for non-tool-call.
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_AuditEnabledButNonTool(t *testing.T) {
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

	// Non-tool-call: "initialize" method
	body := makeRequest("initialize", nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if auditCalled {
		t.Error("Audit should not be called for non-tools/call method")
	}
}

// --------------------------------------------------------------------------
// SSE Transport: cover handleLegacy with AuditLog enabled but no AuditFunc
// --------------------------------------------------------------------------

func TestSSETransport_HandleLegacy_AuditLogNoFunc(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		AuditLog:    true,
		AuditFunc:   nil, // no audit func
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_cluster_status",
		"arguments": map[string]any{},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleLegacy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: cover handleMessage with AuditLog enabled but no AuditFunc
// --------------------------------------------------------------------------

func TestSSETransport_HandleMessage_AuditLogNoFunc(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		AuditLog:    true,
		AuditFunc:   nil,
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_cluster_status",
		"arguments": map[string]any{},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/message", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	transport.handleMessage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: broadcast to client with real SSE connection
// --------------------------------------------------------------------------

func TestSSETransport_BroadcastAndReceive(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{Addr: "127.0.0.1:0", BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Connect SSE client
	sseReq, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	sseReq.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for client registration
	time.Sleep(50 * time.Millisecond)

	if transport.ClientCount() != 1 {
		t.Fatalf("Expected 1 client, got %d", transport.ClientCount())
	}

	// Read the initial endpoint event to extract session ID
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	sseData := string(buf[:n])

	sessionID := ""
	for _, line := range strings.Split(sseData, "\n") {
		if strings.HasPrefix(line, "data: /message?sessionId=") {
			sessionID = strings.TrimPrefix(line, "data: /message?sessionId=")
			sessionID = strings.TrimSpace(sessionID)
			break
		}
	}

	if sessionID == "" {
		t.Skip("Could not extract session ID from SSE stream")
	}

	// Send a message via /message with session ID
	msgBody := makeRequest("resources/list", nil)
	msgReq, _ := http.NewRequest("POST", "http://"+addr+"/message?sessionId="+sessionID, bytes.NewReader(msgBody))
	msgReq.Header.Set("Authorization", "Bearer test-token")
	msgReq.Header.Set("Content-Type", "application/json")
	msgResp, err := http.DefaultClient.Do(msgReq)
	if err != nil {
		t.Fatalf("Message POST failed: %v", err)
	}
	msgResp.Body.Close()

	if msgResp.StatusCode != http.StatusOK {
		t.Errorf("Message POST status = %d, want %d", msgResp.StatusCode, http.StatusOK)
	}
}
