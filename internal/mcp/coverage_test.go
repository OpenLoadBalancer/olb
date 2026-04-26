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
// checkPermission: comprehensive coverage (mcp.go:480-509)
// Currently 30.8% -- need to cover all branches.
// --------------------------------------------------------------------------

func TestCov_CheckPermission_NoTokenPermissions_AllowsAll(t *testing.T) {
	// When tokenPermissions is nil/empty, all access is allowed (backward compatible)
	s := NewServer(ServerConfig{}) // no TokenPermissions
	if !s.checkPermission("olb_query_metrics", "") {
		t.Error("expected access allowed when no token permissions configured")
	}
	if !s.checkPermission("olb_modify_backend", "some-token") {
		t.Error("expected access allowed when no token permissions configured")
	}
}

func TestCov_CheckPermission_PermissionsConfigured_NoToken(t *testing.T) {
	// When permissions are configured but no token is provided, access denied
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"read-token":  PermissionRead,
			"write-token": PermissionWrite,
		},
	})
	if s.checkPermission("olb_query_metrics", "") {
		t.Error("expected access denied when no token but permissions configured")
	}
}

func TestCov_CheckPermission_PermissionsConfigured_UnknownToken(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"read-token": PermissionRead,
		},
	})
	if s.checkPermission("olb_query_metrics", "unknown-token") {
		t.Error("expected access denied for unknown token")
	}
}

func TestCov_CheckPermission_ReadToken_ReadTool(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"read-token": PermissionRead,
		},
	})
	// olb_query_metrics has no explicit permission (defaults to PermissionRead)
	if !s.checkPermission("olb_query_metrics", "read-token") {
		t.Error("read token should have access to read tools")
	}
}

func TestCov_CheckPermission_ReadToken_WriteTool_Denied(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"read-token": PermissionRead,
		},
	})
	// olb_modify_backend requires PermissionWrite
	if s.checkPermission("olb_modify_backend", "read-token") {
		t.Error("read token should not have access to write tools")
	}
}

func TestCov_CheckPermission_WriteToken_ReadTool(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"write-token": PermissionWrite,
		},
	})
	// Write permission implies read
	if !s.checkPermission("olb_query_metrics", "write-token") {
		t.Error("write token should have access to read tools")
	}
}

func TestCov_CheckPermission_WriteToken_WriteTool(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"write-token": PermissionWrite,
		},
	})
	if !s.checkPermission("olb_modify_backend", "write-token") {
		t.Error("write token should have access to write tools")
	}
}

// --------------------------------------------------------------------------
// dispatch: currently 0% coverage (mcp.go:574)
// dispatch just delegates to dispatchWithToken with empty token.
// --------------------------------------------------------------------------

func TestCov_Dispatch_Methods(t *testing.T) {
	s := newTestServer()

	// Exercise dispatch directly for each method
	methods := []struct {
		method string
		params json.RawMessage
	}{
		{"initialize", nil},
		{"tools/list", nil},
		{"resources/list", nil},
		{"prompts/list", nil},
		{"nonexistent", nil},
	}

	for _, tc := range methods {
		t.Run(tc.method, func(t *testing.T) {
			result, rpcErr := s.dispatch(tc.method, tc.params)
			if tc.method == "nonexistent" {
				if rpcErr == nil {
					t.Error("expected error for unknown method")
				}
			} else {
				if rpcErr != nil {
					t.Errorf("unexpected error: %v", rpcErr)
				}
				if result == nil {
					t.Error("expected non-nil result")
				}
			}
		})
	}
}

func TestCov_Dispatch_ToolsCall(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{
		"name":      "olb_cluster_status",
		"arguments": map[string]any{},
	})
	result, rpcErr := s.dispatch("tools/call", params)
	if rpcErr != nil {
		t.Errorf("unexpected error: %v", rpcErr)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestCov_Dispatch_ResourcesRead(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{"uri": "olb://metrics"})
	result, rpcErr := s.dispatch("resources/read", params)
	if rpcErr != nil {
		t.Errorf("unexpected error: %v", rpcErr)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestCov_Dispatch_PromptsGet(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{
		"name":      "diagnose",
		"arguments": map[string]any{"target": "all"},
	})
	result, rpcErr := s.dispatch("prompts/get", params)
	if rpcErr != nil {
		t.Errorf("unexpected error: %v", rpcErr)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

// --------------------------------------------------------------------------
// sanitizeMCPError: currently 42.9% (mcp.go:624-651)
// Need to cover all error mapping branches.
// --------------------------------------------------------------------------

func TestCov_SanitizeMCPError_Nil(t *testing.T) {
	result := sanitizeMCPError(nil)
	if result != "internal error" {
		t.Errorf("nil error = %q, want %q", result, "internal error")
	}
}

func TestCov_SanitizeMCPError_ParameterRequired(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("action parameter is required"))
	if !strings.Contains(result, "parameter is required") {
		t.Errorf("parameter error should pass through, got: %q", result)
	}
}

func TestCov_SanitizeMCPError_NotConfigured(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("backend provider not configured"))
	if !strings.Contains(result, "not configured") {
		t.Errorf("'not configured' error should pass through, got: %q", result)
	}
}

func TestCov_SanitizeMCPError_AnalyticsNotAvailable(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("analytics not available"))
	if !strings.Contains(result, "analytics not available") {
		t.Errorf("'analytics not available' error should pass through, got: %q", result)
	}
}

func TestCov_SanitizeMCPError_Conflict(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("address conflict detected"))
	if !strings.Contains(result, "conflict") {
		t.Errorf("'conflict' error should pass through, got: %q", result)
	}
}

func TestCov_SanitizeMCPError_NotFound(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("pool not found"))
	if result != "resource not found" {
		t.Errorf("'not found' = %q, want %q", result, "resource not found")
	}
}

func TestCov_SanitizeMCPError_Unauthorized(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("unauthorized access"))
	if result != "access denied" {
		t.Errorf("'unauthorized' = %q, want %q", result, "access denied")
	}
}

func TestCov_SanitizeMCPError_Forbidden(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("forbidden operation"))
	if result != "access denied" {
		t.Errorf("'forbidden' = %q, want %q", result, "access denied")
	}
}

func TestCov_SanitizeMCPError_Timeout(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("request timeout"))
	if result != "operation timed out" {
		t.Errorf("'timeout' = %q, want %q", result, "operation timed out")
	}
}

func TestCov_SanitizeMCPError_Deadline(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("deadline exceeded"))
	if result != "operation timed out" {
		t.Errorf("'deadline' = %q, want %q", result, "operation timed out")
	}
}

func TestCov_SanitizeMCPError_Unavailable(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("service unavailable"))
	if result != "service unavailable" {
		t.Errorf("'unavailable' = %q, want %q", result, "service unavailable")
	}
}

func TestCov_SanitizeMCPError_ConnectionRefused(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("connection refused to backend"))
	if result != "service unavailable" {
		t.Errorf("'connection refused' = %q, want %q", result, "service unavailable")
	}
}

func TestCov_SanitizeMCPError_GenericInternal(t *testing.T) {
	result := sanitizeMCPError(fmt.Errorf("some random internal error"))
	if result != "internal error" {
		t.Errorf("generic error = %q, want %q", result, "internal error")
	}
}

// --------------------------------------------------------------------------
// handleToolsCall: currently 0% (mcp.go:743)
// This is the non-token version of tools/call. It's used by dispatch()
// but dispatch currently delegates to dispatchWithToken.
// Exercise it via dispatch() since dispatch calls dispatchWithToken(""),
// which goes through handleToolsCallWithPerm instead.
// We need to call handleToolsCall directly.
// --------------------------------------------------------------------------

func TestCov_HandleToolsCall_NilParams(t *testing.T) {
	s := newTestServer()
	_, rpcErr := s.handleToolsCall(nil)
	if rpcErr == nil {
		t.Fatal("expected error for nil params")
	}
	if rpcErr.Code != errCodeInvalidParams {
		t.Errorf("error code = %d, want %d", rpcErr.Code, errCodeInvalidParams)
	}
}

func TestCov_HandleToolsCall_InvalidParams(t *testing.T) {
	s := newTestServer()
	result, rpcErr := s.handleToolsCall(json.RawMessage(`"not an object"`))
	if rpcErr == nil {
		t.Fatal("expected error for invalid params")
	}
	if rpcErr.Code != errCodeInvalidParams {
		t.Errorf("error code = %d, want %d", rpcErr.Code, errCodeInvalidParams)
	}
	_ = result
}

func TestCov_HandleToolsCall_MissingToolName(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{"arguments": map[string]any{}})
	result, rpcErr := s.handleToolsCall(params)
	if rpcErr == nil {
		t.Fatal("expected error for missing tool name")
	}
	if rpcErr.Code != errCodeInvalidParams {
		t.Errorf("error code = %d, want %d", rpcErr.Code, errCodeInvalidParams)
	}
	_ = result
}

func TestCov_HandleToolsCall_UnknownTool(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})
	result, rpcErr := s.handleToolsCall(params)
	if rpcErr == nil {
		t.Fatal("expected error for unknown tool")
	}
	if rpcErr.Code != errCodeInvalidParams {
		t.Errorf("error code = %d, want %d", rpcErr.Code, errCodeInvalidParams)
	}
	_ = result
}

func TestCov_HandleToolsCall_NilArguments(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{
		"name":      "olb_cluster_status",
		"arguments": nil,
	})
	result, rpcErr := s.handleToolsCall(params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestCov_HandleToolsCall_Success(t *testing.T) {
	s := newTestServer()
	params, _ := json.Marshal(map[string]any{
		"name":      "olb_query_metrics",
		"arguments": map[string]any{"metric": "requests_total"},
	})
	result, rpcErr := s.handleToolsCall(params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestCov_HandleToolsCall_HandlerError(t *testing.T) {
	s := NewServer(ServerConfig{}) // all providers nil
	params, _ := json.Marshal(map[string]any{
		"name": "olb_modify_backend",
		"arguments": map[string]any{
			"action":  "add",
			"pool":    "test",
			"address": "10.0.0.1:80",
		},
	})
	result, rpcErr := s.handleToolsCall(params)
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	// Should be a ToolResult with IsError (nil backend provider)
	toolResult, ok := result.(ToolResult)
	if !ok {
		t.Fatal("expected ToolResult")
	}
	if !toolResult.IsError {
		t.Error("expected error tool result for nil backend provider")
	}
}

func TestCov_HandleToolsCall_MarshalError(t *testing.T) {
	s := newTestServer()
	s.RegisterTool(Tool{
		Name:        "bad_marshal_tool",
		Description: "Returns unmarshallable data",
		InputSchema: InputSchema{Type: "object"},
	}, func(params map[string]any) (any, error) {
		return unmarshallable{Ch: make(chan struct{})}, nil
	})
	params, _ := json.Marshal(map[string]any{
		"name":      "bad_marshal_tool",
		"arguments": map[string]any{},
	})
	result, rpcErr := s.handleToolsCall(params)
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	toolResult, ok := result.(ToolResult)
	if !ok {
		t.Fatal("expected ToolResult")
	}
	if !toolResult.IsError {
		t.Error("expected error tool result for marshal failure")
	}
	if !strings.Contains(toolResult.Content[0].Text, "marshal") {
		t.Errorf("expected marshal error text, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// NewHTTPTransport: currently 66.7% (mcp.go:1436)
// Missing the empty token error path.
// --------------------------------------------------------------------------

func TestCov_NewHTTPTransport_EmptyToken(t *testing.T) {
	s := newTestServer()
	_, err := NewHTTPTransport(s, ":0", "")
	if err == nil {
		t.Fatal("expected error for empty bearer token")
	}
	if !strings.Contains(err.Error(), "non-empty bearer token") {
		t.Errorf("error = %q, want substring 'non-empty bearer token'", err.Error())
	}
}

// --------------------------------------------------------------------------
// SSE authenticate: currently 85.7% (sse_transport.go:147)
// Missing empty BearerToken (auth disabled) path.
// --------------------------------------------------------------------------

func TestCov_SSETransport_Authenticate_EmptyToken_AuthDisabled(t *testing.T) {
	s := newTestServer()
	// Bypass NewSSETransport's validation by creating manually
	transport := &SSETransport{
		server:  s,
		config:  SSETransportConfig{BearerToken: ""}, // no token = auth disabled
		clients: make(map[string]*sseClient),
		done:    make(chan struct{}),
	}
	req := httptest.NewRequest("GET", "/sse", nil)
	// With empty BearerToken, authenticate should return true (auth disabled)
	if !transport.authenticate(req) {
		t.Error("authenticate should return true when BearerToken is empty (auth disabled)")
	}
}

// --------------------------------------------------------------------------
// handleToolsCallWithPerm: cover permission denied path via JSON-RPC
// (mcp.go:669-740)
// --------------------------------------------------------------------------

func TestCov_HandleToolsCallWithPerm_PermissionDenied(t *testing.T) {
	s := NewServer(ServerConfig{
		Backends: &mockBackendProvider{},
		TokenPermissions: map[string]ToolPermission{
			"read-only-token": PermissionRead,
		},
	})

	// Try to call a write tool with a read-only token
	params, _ := json.Marshal(map[string]any{
		"name": "olb_modify_backend",
		"arguments": map[string]any{
			"action":  "add",
			"pool":    "test",
			"address": "10.0.0.1:80",
		},
	})
	result, rpcErr := s.dispatchWithToken("tools/call", params, "read-only-token")
	if rpcErr == nil {
		t.Fatal("expected error for permission denied")
	}
	if rpcErr.Code != errCodeInternal {
		t.Errorf("error code = %d, want %d", rpcErr.Code, errCodeInternal)
	}
	if !strings.Contains(rpcErr.Message, "permission denied") {
		t.Errorf("error message = %q, want 'permission denied'", rpcErr.Message)
	}
	_ = result
}

// --------------------------------------------------------------------------
// HandleJSONRPCWithToken: full flow with token
// (mcp.go:534)
// --------------------------------------------------------------------------

func TestCov_HandleJSONRPCWithToken_Success(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"valid-token": PermissionWrite,
		},
		Metrics: &mockMetricsProvider{data: map[string]any{"requests_total": 100}},
	})

	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_query_metrics",
		"arguments": map[string]any{"metric": "requests_total"},
	})

	resp, err := s.HandleJSONRPCWithToken(req, "valid-token")
	if err != nil {
		t.Fatalf("HandleJSONRPCWithToken error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestCov_HandleJSONRPCWithToken_PermissionDenied(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"read-token": PermissionRead,
		},
	})

	req := makeRequest("tools/call", map[string]any{
		"name": "olb_modify_backend",
		"arguments": map[string]any{
			"action":  "add",
			"pool":    "test",
			"address": "10.0.0.1:80",
		},
	})

	resp, err := s.HandleJSONRPCWithToken(req, "read-token")
	if err != nil {
		t.Fatalf("HandleJSONRPCWithToken error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected permission denied error")
	}
	if !strings.Contains(r.Error.Message, "permission denied") {
		t.Errorf("error = %v, want 'permission denied'", r.Error)
	}
}

// --------------------------------------------------------------------------
// HTTPTransport ServeHTTP with token pass-through
// (mcp.go:1448-1487)
// --------------------------------------------------------------------------

func TestCov_HTTPTransport_ServeHTTP_TokenPropagation(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"write-token": PermissionWrite,
		},
		Metrics: &mockMetricsProvider{data: map[string]any{"requests_total": 100}},
	})
	transport, err := NewHTTPTransport(s, ":0", "write-token")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	// Valid token + read tool should succeed
	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_query_metrics",
		"arguments": map[string]any{"metric": "requests_total"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer write-token")
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: MaxClients limit
// (sse_transport.go:198-201)
// --------------------------------------------------------------------------

func TestCov_SSETransport_HandleSSE_MaxClientsExceeded(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
		MaxClients:  1, // only allow 1 client
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Stop(context.Background())

	addr := transport.Addr()

	// Connect first client - should succeed
	req1, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	req1.Header.Set("Authorization", "Bearer test-token")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("First SSE connect failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("First SSE status = %d, want %d", resp1.StatusCode, http.StatusOK)
	}

	// Wait for client registration
	time.Sleep(50 * time.Millisecond)

	// Connect second client - should be rejected with 503
	req2, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	req2.Header.Set("Authorization", "Bearer test-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("Second SSE connect failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Second SSE status = %d, want %d", resp2.StatusCode, http.StatusServiceUnavailable)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: handleSSE transport done path
// (sse_transport.go:254)
// --------------------------------------------------------------------------

func TestCov_SSETransport_HandleSSE_TransportDone(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	addr := transport.Addr()

	// Connect SSE client
	req, _ := http.NewRequest("GET", "http://"+addr+"/sse", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}

	// Wait for client registration
	time.Sleep(50 * time.Millisecond)

	// Stop transport - triggers t.done path in handleSSE
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	transport.Stop(ctx)

	// Read from response body - should end cleanly
	buf := make([]byte, 1024)
	resp.Body.Read(buf)
	resp.Body.Close()
}

// --------------------------------------------------------------------------
// HTTPTransport ServeHTTP: cover token extraction and pass to HandleJSONRPCWithToken
// when token matches (the "happy path" through ServeHTTP lines 1463-1486)
// --------------------------------------------------------------------------

func TestCov_HTTPTransport_ServeHTTP_ValidToken(t *testing.T) {
	s := NewServer(ServerConfig{
		TokenPermissions: map[string]ToolPermission{
			"my-secret": PermissionWrite,
		},
		Metrics: &mockMetricsProvider{data: map[string]any{"latency_p99": 0.1}},
	})
	transport, err := NewHTTPTransport(s, ":0", "my-secret")
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}

	body := makeRequest("tools/call", map[string]any{
		"name":      "olb_query_metrics",
		"arguments": map[string]any{"metric": "latency_p99"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer my-secret")
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
	r := parseResponse(t, w.Body.Bytes())
	if r.Error != nil {
		t.Errorf("unexpected error: %v", r.Error)
	}
}

// --------------------------------------------------------------------------
// Diagnose: cover the "config" mode specifically
// (mcp.go:1232-1306)
// --------------------------------------------------------------------------

func TestCov_Diagnose_ConfigMode(t *testing.T) {
	s := newTestServer()
	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "config"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)
	if toolResult.IsError {
		t.Errorf("tool result is error: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// ListBackends: cover status filter "all" explicitly
// (mcp.go:1138-1148)
// --------------------------------------------------------------------------

func TestCov_ListBackends_StatusAll(t *testing.T) {
	s := newTestServer()
	req := makeRequest("tools/call", map[string]any{
		"name": "olb_list_backends",
		"arguments": map[string]any{
			"status": "all",
		},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	var data map[string]any
	json.Unmarshal([]byte(toolResult.Content[0].Text), &data)
	pools, _ := data["pools"].([]any)
	// "all" should return all backends (3 in web-pool + 1 in api-pool = 4 total)
	totalBackends := 0
	for _, p := range pools {
		pm := p.(map[string]any)
		bs, _ := pm["backends"].([]any)
		totalBackends += len(bs)
	}
	if totalBackends != 4 {
		t.Errorf("expected 4 backends with status=all, got %d", totalBackends)
	}
}

// --------------------------------------------------------------------------
// GetLogs: cover edge cases with count validation
// (mcp.go:1313-1345)
// --------------------------------------------------------------------------

func TestCov_GetLogs_CountOutOfRange(t *testing.T) {
	s := newTestServer()
	// count is negative - should use default 50
	req := makeRequest("tools/call", map[string]any{
		"name": "olb_get_logs",
		"arguments": map[string]any{
			"count": float64(-5),
		},
	})
	resp, _ := s.HandleJSONRPC(req)
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestCov_GetLogs_CountOverLimit(t *testing.T) {
	s := newTestServer()
	// count > 1000 - should use default 50
	req := makeRequest("tools/call", map[string]any{
		"name": "olb_get_logs",
		"arguments": map[string]any{
			"count": float64(5000),
		},
	})
	resp, _ := s.HandleJSONRPC(req)
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestCov_GetLogs_CountNonInteger(t *testing.T) {
	s := newTestServer()
	// count is a non-integer float - should use default 50
	req := makeRequest("tools/call", map[string]any{
		"name": "olb_get_logs",
		"arguments": map[string]any{
			"count": float64(10.5),
		},
	})
	resp, _ := s.HandleJSONRPC(req)
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestCov_GetLogs_CountZero(t *testing.T) {
	s := newTestServer()
	// count is 0 - should use default 50
	req := makeRequest("tools/call", map[string]any{
		"name": "olb_get_logs",
		"arguments": map[string]any{
			"count": float64(0),
		},
	})
	resp, _ := s.HandleJSONRPC(req)
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

// --------------------------------------------------------------------------
// SSE handleLegacy: cover auth failure with correct token format but wrong value
// (sse_transport.go:316-362)
// --------------------------------------------------------------------------

func TestCov_SSETransport_HandleLegacy_CorrectFormatWrongToken(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("initialize", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	transport.handleLegacy(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --------------------------------------------------------------------------
// Diagnose with "errors" mode and logs provider returning data
// (mcp.go:1234-1241)
// --------------------------------------------------------------------------

func TestCov_Diagnose_ErrorsMode_WithLogs(t *testing.T) {
	s := newTestServer()
	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "errors"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)
	if !strings.Contains(toolResult.Content[0].Text, "error_analysis") {
		t.Errorf("expected 'error_analysis' in diagnose errors mode, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// Diagnose with "latency" mode and metrics provider
// (mcp.go:1242-1248)
// --------------------------------------------------------------------------

func TestCov_Diagnose_LatencyMode_WithMetrics(t *testing.T) {
	s := newTestServer()
	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "latency"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)
	if !strings.Contains(toolResult.Content[0].Text, "latency_analysis") {
		t.Errorf("expected 'latency_analysis' in diagnose latency mode, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// Diagnose with "health" mode - unhealthy backends detected
// (mcp.go:1270-1285)
// --------------------------------------------------------------------------

func TestCov_Diagnose_HealthMode_UnhealthyBackends(t *testing.T) {
	s := newTestServer()
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
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)
	if !strings.Contains(toolResult.Content[0].Text, "unhealthy_backend") {
		t.Errorf("expected 'unhealthy_backend' in diagnose health mode, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// Diagnose with "capacity" mode
// (mcp.go:1249-1268)
// --------------------------------------------------------------------------

func TestCov_Diagnose_CapacityMode_WithBackends(t *testing.T) {
	s := newTestServer()
	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "capacity"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)
	if !strings.Contains(toolResult.Content[0].Text, "capacity_analysis") {
		t.Errorf("expected 'capacity_analysis' in diagnose capacity mode, got: %s", toolResult.Content[0].Text)
	}
}

// --------------------------------------------------------------------------
// Diagnose with "full" mode with all providers
// (mcp.go:1286-1306)
// --------------------------------------------------------------------------

func TestCov_Diagnose_FullMode_AllProviders(t *testing.T) {
	s := newTestServer()
	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "full"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)
	text := toolResult.Content[0].Text
	if !strings.Contains(text, "metrics_snapshot") {
		t.Errorf("expected 'metrics_snapshot' in full mode, got: %s", text)
	}
	if !strings.Contains(text, "backend_status") {
		t.Errorf("expected 'backend_status' in full mode, got: %s", text)
	}
	if !strings.Contains(text, "recent_errors") {
		t.Errorf("expected 'recent_errors' in full mode, got: %s", text)
	}
}

// --------------------------------------------------------------------------
// Capacity diagnose: pool with zero backends (edge case for max(total, 1))
// (mcp.go:1266)
// --------------------------------------------------------------------------

func TestCov_Diagnose_CapacityMode_EmptyPool(t *testing.T) {
	s := NewServer(ServerConfig{
		Backends: &mockBackendProvider{
			pools: []PoolInfo{
				{
					Name:      "empty-pool",
					Algorithm: "round_robin",
					Backends:  []BackendInfo{}, // no backends
				},
			},
		},
	})
	req := makeRequest("tools/call", map[string]any{
		"name":      "olb_diagnose",
		"arguments": map[string]any{"mode": "capacity"},
	})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC error: %v", err)
	}
	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: handleMessage with no session ID (no broadcast)
// (sse_transport.go:308-311)
// --------------------------------------------------------------------------

func TestCov_SSETransport_HandleMessage_NoSessionID(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := makeRequest("initialize", nil)
	req := httptest.NewRequest(http.MethodPost, "/message", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	transport.handleMessage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// SSE Transport: MaxClients default to 100 when MaxClients <= 0
// (sse_transport.go:78-80)
// --------------------------------------------------------------------------

func TestCov_SSETransport_DefaultMaxClients(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
		MaxClients:  0, // should default to 100
	})
	if err != nil {
		t.Fatal(err)
	}
	if transport.config.MaxClients != 100 {
		t.Errorf("MaxClients = %d, want 100", transport.config.MaxClients)
	}
}

func TestCov_SSETransport_NegativeMaxClients(t *testing.T) {
	s := newTestServer()
	transport, err := NewSSETransport(s, SSETransportConfig{
		Addr:        "127.0.0.1:0",
		BearerToken: "test-token",
		MaxClients:  -5, // should default to 100
	})
	if err != nil {
		t.Fatal(err)
	}
	if transport.config.MaxClients != 100 {
		t.Errorf("MaxClients = %d, want 100", transport.config.MaxClients)
	}
}

// --------------------------------------------------------------------------
// HTTPTransport: cover bearer token empty check (already covered by NewHTTPTransport_EmptyToken
// above, but also ensure the token extraction in ServeHTTP works for the full flow)
// --------------------------------------------------------------------------

func TestCov_HTTPTransport_ServeHTTP_EmptyBearerToken(t *testing.T) {
	// Create transport with empty bearer token via manual construction to test
	// the ServeHTTP path where bearerToken is empty (auth disabled)
	s := newTestServer()
	transport := &HTTPTransport{
		server:      s,
		addr:        ":0",
		bearerToken: "", // empty = auth disabled
	}

	body := makeRequest("initialize", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d (no auth required)", w.Code, http.StatusOK)
	}
}
