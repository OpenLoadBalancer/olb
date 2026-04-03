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
	"testing"
)

// --- Mock providers ---

type mockMetricsProvider struct {
	data map[string]interface{}
}

func (m *mockMetricsProvider) QueryMetrics(pattern string) map[string]interface{} {
	if pattern == "*" {
		return m.data
	}
	result := make(map[string]interface{})
	for k, v := range m.data {
		if strings.Contains(k, pattern) {
			result[k] = v
		}
	}
	return result
}

type mockBackendProvider struct {
	pools       []PoolInfo
	modifyError error
}

func (m *mockBackendProvider) ListPools() []PoolInfo {
	return m.pools
}

func (m *mockBackendProvider) ModifyBackend(action, pool, addr string) error {
	if m.modifyError != nil {
		return m.modifyError
	}
	return nil
}

type mockConfigProvider struct {
	config interface{}
}

func (m *mockConfigProvider) GetConfig() interface{} {
	return m.config
}

type mockLogProvider struct {
	logs []LogEntry
}

func (m *mockLogProvider) GetLogs(count int, level string) []LogEntry {
	result := m.logs
	if level != "" {
		filtered := make([]LogEntry, 0)
		for _, l := range result {
			if strings.EqualFold(l.Level, level) {
				filtered = append(filtered, l)
			}
		}
		result = filtered
	}
	if count > 0 && count < len(result) {
		result = result[:count]
	}
	return result
}

type mockClusterProvider struct {
	status interface{}
}

func (m *mockClusterProvider) GetStatus() interface{} {
	return m.status
}

type mockRouteProvider struct {
	modifyError error
}

func (m *mockRouteProvider) ModifyRoute(action, host, path, backend string) error {
	if m.modifyError != nil {
		return m.modifyError
	}
	return nil
}

// --- Helper functions ---

func newTestServer() *Server {
	return NewServer(ServerConfig{
		Metrics: &mockMetricsProvider{
			data: map[string]interface{}{
				"requests_total":     int64(1000),
				"active_connections": float64(42),
				"latency_p99":        float64(0.125),
			},
		},
		Backends: &mockBackendProvider{
			pools: []PoolInfo{
				{
					Name:      "web-pool",
					Algorithm: "round_robin",
					Backends: []BackendInfo{
						{ID: "web-1", Address: "10.0.0.1:8080", Status: "healthy", Weight: 1, Connections: 10},
						{ID: "web-2", Address: "10.0.0.2:8080", Status: "healthy", Weight: 1, Connections: 8},
						{ID: "web-3", Address: "10.0.0.3:8080", Status: "unhealthy", Weight: 1, Connections: 0},
					},
				},
				{
					Name:      "api-pool",
					Algorithm: "least_connections",
					Backends: []BackendInfo{
						{ID: "api-1", Address: "10.0.1.1:9090", Status: "healthy", Weight: 2, Connections: 5},
					},
				},
			},
		},
		Config: &mockConfigProvider{
			config: map[string]interface{}{
				"version": "1",
				"listeners": []map[string]interface{}{
					{"name": "http", "address": ":8080"},
				},
			},
		},
		Logs: &mockLogProvider{
			logs: []LogEntry{
				{Timestamp: "2026-03-15T10:00:00Z", Level: "INFO", Message: "Server started"},
				{Timestamp: "2026-03-15T10:01:00Z", Level: "ERROR", Message: "Backend connection failed"},
				{Timestamp: "2026-03-15T10:02:00Z", Level: "WARN", Message: "High latency detected"},
			},
		},
		Cluster: &mockClusterProvider{
			status: map[string]interface{}{
				"mode":   "cluster",
				"leader": "node-1",
				"nodes":  []string{"node-1", "node-2", "node-3"},
				"state":  "leader",
			},
		},
		Routes: &mockRouteProvider{},
	})
}

func makeRequest(method string, params interface{}) []byte {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		raw, _ := json.Marshal(params)
		req["params"] = json.RawMessage(raw)
	}
	data, _ := json.Marshal(req)
	return data
}

func parseResponse(t *testing.T, data []byte) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v\nResponse: %s", err, string(data))
	}
	return resp
}

// --- Tests ---

func TestNewServer(t *testing.T) {
	s := newTestServer()

	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	if len(s.tools) == 0 {
		t.Error("No tools registered")
	}

	if len(s.resources) == 0 {
		t.Error("No resources registered")
	}

	if len(s.prompts) == 0 {
		t.Error("No prompts registered")
	}
}

func TestHandleJSONRPC_ParseError(t *testing.T) {
	s := newTestServer()

	resp, err := s.HandleJSONRPC([]byte("not json"))
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error response")
	}
	if r.Error.Code != errCodeParse {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeParse)
	}
}

func TestHandleJSONRPC_InvalidVersion(t *testing.T) {
	s := newTestServer()

	req := `{"jsonrpc": "1.0", "id": 1, "method": "initialize"}`
	resp, err := s.HandleJSONRPC([]byte(req))
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error response")
	}
	if r.Error.Code != errCodeInvalidRequest {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeInvalidRequest)
	}
}

func TestHandleJSONRPC_MethodNotFound(t *testing.T) {
	s := newTestServer()

	req := makeRequest("nonexistent/method", nil)
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error response")
	}
	if r.Error.Code != errCodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeMethodNotFound)
	}
}

func TestInitialize(t *testing.T) {
	s := newTestServer()

	req := makeRequest("initialize", map[string]interface{}{
		"protocolVersion": mcpProtocolVersion,
		"clientInfo": map[string]interface{}{
			"name":    "test-client",
			"version": "1.0.0",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result is not a map")
	}

	if result["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], mcpProtocolVersion)
	}

	caps, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("capabilities is not a map")
	}
	if caps["tools"] == nil {
		t.Error("Missing tools capability")
	}
	if caps["resources"] == nil {
		t.Error("Missing resources capability")
	}
	if caps["prompts"] == nil {
		t.Error("Missing prompts capability")
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("serverInfo is not a map")
	}
	if serverInfo["name"] != serverName {
		t.Errorf("server name = %v, want %s", serverInfo["name"], serverName)
	}
}

func TestToolsList(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/list", nil)
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result is not a map")
	}

	toolsRaw, ok := result["tools"]
	if !ok {
		t.Fatal("Missing tools in result")
	}

	// Re-marshal/unmarshal to get tools as a slice
	toolsJSON, _ := json.Marshal(toolsRaw)
	var tools []Tool
	if err := json.Unmarshal(toolsJSON, &tools); err != nil {
		t.Fatalf("Failed to unmarshal tools: %v", err)
	}

	expectedTools := []string{
		"olb_query_metrics",
		"olb_list_backends",
		"olb_modify_backend",
		"olb_modify_route",
		"olb_diagnose",
		"olb_get_logs",
		"olb_get_config",
		"olb_cluster_status",
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("Got %d tools, want %d", len(tools), len(expectedTools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("Missing tool: %s", expected)
		}
	}
}

func TestToolsCall_QueryMetrics(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_query_metrics",
		"arguments": map[string]interface{}{"metric": "requests_total"},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	// Result should be a ToolResult with content
	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	if err := json.Unmarshal(resultJSON, &toolResult); err != nil {
		t.Fatalf("Failed to unmarshal tool result: %v", err)
	}

	if toolResult.IsError {
		t.Error("Tool result is marked as error")
	}

	if len(toolResult.Content) == 0 {
		t.Fatal("No content in tool result")
	}

	if !strings.Contains(toolResult.Content[0].Text, "requests_total") {
		t.Error("Result does not contain metric name")
	}
}

func TestToolsCall_QueryMetrics_MissingParam(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_query_metrics",
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected protocol error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !toolResult.IsError {
		t.Error("Expected tool result to be marked as error")
	}
}

func TestToolsCall_ListBackends(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_list_backends",
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if toolResult.IsError {
		t.Error("Tool result is marked as error")
	}

	if len(toolResult.Content) == 0 {
		t.Fatal("No content in tool result")
	}

	text := toolResult.Content[0].Text
	if !strings.Contains(text, "web-pool") {
		t.Error("Result does not contain web-pool")
	}
	if !strings.Contains(text, "api-pool") {
		t.Error("Result does not contain api-pool")
	}
}

func TestToolsCall_ListBackends_FilterByPool(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_list_backends",
		"arguments": map[string]interface{}{"pool": "web-pool"},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	text := toolResult.Content[0].Text
	if !strings.Contains(text, "web-pool") {
		t.Error("Result does not contain web-pool")
	}
	// api-pool should not be in a filtered result
	// The text is JSON, check the pools array
	var resultData map[string]interface{}
	json.Unmarshal([]byte(text), &resultData)
	if pools, ok := resultData["pools"].([]interface{}); ok {
		if len(pools) != 1 {
			t.Errorf("Expected 1 pool, got %d", len(pools))
		}
	}
}

func TestToolsCall_ModifyBackend(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name": "olb_modify_backend",
		"arguments": map[string]interface{}{
			"action":  "add",
			"pool":    "web-pool",
			"address": "10.0.0.4:8080",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
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

	if !strings.Contains(toolResult.Content[0].Text, "ok") {
		t.Error("Result does not contain success status")
	}
}

func TestToolsCall_ModifyBackend_Error(t *testing.T) {
	s := NewServer(ServerConfig{
		Backends: &mockBackendProvider{
			modifyError: fmt.Errorf("pool not found"),
		},
	})

	req := makeRequest("tools/call", map[string]interface{}{
		"name": "olb_modify_backend",
		"arguments": map[string]interface{}{
			"action":  "add",
			"pool":    "nonexistent",
			"address": "10.0.0.1:8080",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected protocol error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !toolResult.IsError {
		t.Error("Expected tool result to be error")
	}
}

func TestToolsCall_ModifyRoute(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name": "olb_modify_route",
		"arguments": map[string]interface{}{
			"action":  "add",
			"host":    "example.com",
			"path":    "/api",
			"backend": "api-pool",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
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
}

func TestToolsCall_Diagnose(t *testing.T) {
	s := newTestServer()

	modes := []string{"errors", "latency", "capacity", "health", "full"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			req := makeRequest("tools/call", map[string]interface{}{
				"name":      "olb_diagnose",
				"arguments": map[string]interface{}{"mode": mode},
			})

			resp, err := s.HandleJSONRPC(req)
			if err != nil {
				t.Fatalf("HandleJSONRPC returned error: %v", err)
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

			if len(toolResult.Content) == 0 {
				t.Fatal("No content in tool result")
			}

			if !strings.Contains(toolResult.Content[0].Text, "findings") {
				t.Error("Result does not contain findings")
			}
		})
	}
}

func TestToolsCall_GetLogs(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name": "olb_get_logs",
		"arguments": map[string]interface{}{
			"count": float64(10),
			"level": "error",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if toolResult.IsError {
		t.Error("Tool result is error")
	}

	text := toolResult.Content[0].Text
	if !strings.Contains(text, "entries") {
		t.Error("Result does not contain entries")
	}
}

func TestToolsCall_GetLogs_WithFilter(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name": "olb_get_logs",
		"arguments": map[string]interface{}{
			"filter": "Backend",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	// The filtered result should contain only the "Backend connection failed" entry
	var resultData map[string]interface{}
	json.Unmarshal([]byte(toolResult.Content[0].Text), &resultData)
	count, ok := resultData["count"].(float64)
	if !ok {
		t.Fatal("Missing count in result")
	}
	if int(count) != 1 {
		t.Errorf("Expected 1 filtered entry, got %d", int(count))
	}
}

func TestToolsCall_GetConfig(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_get_config",
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if toolResult.IsError {
		t.Error("Tool result is error")
	}

	text := toolResult.Content[0].Text
	if !strings.Contains(text, "version") {
		t.Error("Result does not contain version")
	}
}

func TestToolsCall_ClusterStatus(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_cluster_status",
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if toolResult.IsError {
		t.Error("Tool result is error")
	}

	text := toolResult.Content[0].Text
	if !strings.Contains(text, "leader") {
		t.Error("Result does not contain leader info")
	}
}

func TestToolsCall_UnknownTool(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "nonexistent_tool",
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for unknown tool")
	}
	if r.Error.Code != errCodeInvalidParams {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeInvalidParams)
	}
}

func TestToolsCall_MissingName(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for missing tool name")
	}
}

func TestToolsCall_MissingParams(t *testing.T) {
	s := newTestServer()

	// tools/call with nil params should return error
	req := makeRequest("tools/call", nil)
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for missing params")
	}
	if r.Error.Code != errCodeInvalidParams {
		t.Errorf("Error code = %d, want %d", r.Error.Code, errCodeInvalidParams)
	}
}

func TestResourcesList(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/list", nil)
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result is not a map")
	}

	resourcesRaw, ok := result["resources"]
	if !ok {
		t.Fatal("Missing resources in result")
	}

	resourcesJSON, _ := json.Marshal(resourcesRaw)
	var resources []Resource
	json.Unmarshal(resourcesJSON, &resources)

	expectedURIs := map[string]bool{
		"olb://metrics": true,
		"olb://config":  true,
		"olb://health":  true,
		"olb://logs":    true,
	}

	for _, res := range resources {
		if !expectedURIs[res.URI] {
			t.Errorf("Unexpected resource URI: %s", res.URI)
		}
		delete(expectedURIs, res.URI)
	}

	for uri := range expectedURIs {
		t.Errorf("Missing resource: %s", uri)
	}
}

func TestResourcesRead_Metrics(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]interface{}{"uri": "olb://metrics"})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result is not a map")
	}

	contents, ok := result["contents"]
	if !ok {
		t.Fatal("Missing contents in result")
	}

	contentsJSON, _ := json.Marshal(contents)
	var resourceContents []ResourceContent
	json.Unmarshal(contentsJSON, &resourceContents)

	if len(resourceContents) != 1 {
		t.Fatalf("Expected 1 content, got %d", len(resourceContents))
	}

	if resourceContents[0].URI != "olb://metrics" {
		t.Errorf("URI = %s, want olb://metrics", resourceContents[0].URI)
	}

	if !strings.Contains(resourceContents[0].Text, "requests_total") {
		t.Error("Resource content does not contain requests_total")
	}
}

func TestResourcesRead_Config(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]interface{}{"uri": "olb://config"})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]interface{})
	contents, _ := result["contents"]
	contentsJSON, _ := json.Marshal(contents)
	var rc []ResourceContent
	json.Unmarshal(contentsJSON, &rc)

	if len(rc) == 0 || !strings.Contains(rc[0].Text, "version") {
		t.Error("Config resource does not contain version")
	}
}

func TestResourcesRead_Health(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]interface{}{"uri": "olb://health"})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]interface{})
	contents, _ := result["contents"]
	contentsJSON, _ := json.Marshal(contents)
	var rc []ResourceContent
	json.Unmarshal(contentsJSON, &rc)

	if len(rc) == 0 || !strings.Contains(rc[0].Text, "web-pool") {
		t.Error("Health resource does not contain pool info")
	}
}

func TestResourcesRead_Logs(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]interface{}{"uri": "olb://logs"})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]interface{})
	contents, _ := result["contents"]
	contentsJSON, _ := json.Marshal(contents)
	var rc []ResourceContent
	json.Unmarshal(contentsJSON, &rc)

	if len(rc) == 0 || !strings.Contains(rc[0].Text, "Server started") {
		t.Error("Logs resource does not contain log entries")
	}
}

func TestResourcesRead_UnknownURI(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]interface{}{"uri": "olb://unknown"})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for unknown resource URI")
	}
}

func TestResourcesRead_MissingURI(t *testing.T) {
	s := newTestServer()

	req := makeRequest("resources/read", map[string]interface{}{})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for missing URI")
	}
}

func TestPromptsList(t *testing.T) {
	s := newTestServer()

	req := makeRequest("prompts/list", nil)
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result is not a map")
	}

	promptsRaw, ok := result["prompts"]
	if !ok {
		t.Fatal("Missing prompts in result")
	}

	promptsJSON, _ := json.Marshal(promptsRaw)
	var prompts []Prompt
	json.Unmarshal(promptsJSON, &prompts)

	expectedPrompts := map[string]bool{
		"diagnose":          true,
		"capacity_planning": true,
		"canary_deploy":     true,
	}

	for _, p := range prompts {
		if !expectedPrompts[p.Name] {
			t.Errorf("Unexpected prompt: %s", p.Name)
		}
		delete(expectedPrompts, p.Name)
	}

	for name := range expectedPrompts {
		t.Errorf("Missing prompt: %s", name)
	}
}

func TestPromptsGet_Diagnose(t *testing.T) {
	s := newTestServer()

	req := makeRequest("prompts/get", map[string]interface{}{
		"name":      "diagnose",
		"arguments": map[string]interface{}{"target": "web-pool"},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result is not a map")
	}

	if result["description"] == nil {
		t.Error("Missing description")
	}

	messagesRaw, ok := result["messages"]
	if !ok {
		t.Fatal("Missing messages")
	}

	messagesJSON, _ := json.Marshal(messagesRaw)
	var messages []PromptMessage
	json.Unmarshal(messagesJSON, &messages)

	if len(messages) == 0 {
		t.Fatal("No messages in prompt")
	}

	if messages[0].Role != "user" {
		t.Errorf("Message role = %s, want user", messages[0].Role)
	}

	if !strings.Contains(messages[0].Content.Text, "web-pool") {
		t.Error("Prompt message does not contain target")
	}
}

func TestPromptsGet_CapacityPlanning(t *testing.T) {
	s := newTestServer()

	req := makeRequest("prompts/get", map[string]interface{}{
		"name":      "capacity_planning",
		"arguments": map[string]interface{}{"pool": "api-pool"},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]interface{})
	messagesRaw, _ := result["messages"]
	messagesJSON, _ := json.Marshal(messagesRaw)
	var messages []PromptMessage
	json.Unmarshal(messagesJSON, &messages)

	if len(messages) == 0 || !strings.Contains(messages[0].Content.Text, "api-pool") {
		t.Error("Prompt does not contain pool name")
	}
}

func TestPromptsGet_CanaryDeploy(t *testing.T) {
	s := newTestServer()

	req := makeRequest("prompts/get", map[string]interface{}{
		"name": "canary_deploy",
		"arguments": map[string]interface{}{
			"route":       "/api",
			"new_backend": "10.0.0.5:8080",
			"percentage":  "5",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	result, _ := r.Result.(map[string]interface{})
	messagesRaw, _ := result["messages"]
	messagesJSON, _ := json.Marshal(messagesRaw)
	var messages []PromptMessage
	json.Unmarshal(messagesJSON, &messages)

	if len(messages) == 0 {
		t.Fatal("No messages")
	}

	text := messages[0].Content.Text
	if !strings.Contains(text, "/api") {
		t.Error("Prompt does not contain route")
	}
	if !strings.Contains(text, "10.0.0.5:8080") {
		t.Error("Prompt does not contain new backend")
	}
	if !strings.Contains(text, "5%") {
		t.Error("Prompt does not contain percentage")
	}
}

func TestPromptsGet_UnknownPrompt(t *testing.T) {
	s := newTestServer()

	req := makeRequest("prompts/get", map[string]interface{}{
		"name": "nonexistent_prompt",
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for unknown prompt")
	}
}

func TestPromptsGet_MissingName(t *testing.T) {
	s := newTestServer()

	req := makeRequest("prompts/get", map[string]interface{}{})
	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error == nil {
		t.Fatal("Expected error for missing prompt name")
	}
}

func TestNilProviders(t *testing.T) {
	s := NewServer(ServerConfig{}) // All providers nil

	// tools/call should still work but return appropriate messages
	tools := []struct {
		name string
		args map[string]interface{}
	}{
		{"olb_query_metrics", map[string]interface{}{"metric": "test"}},
		{"olb_list_backends", map[string]interface{}{}},
		{"olb_get_logs", map[string]interface{}{}},
		{"olb_get_config", map[string]interface{}{}},
		{"olb_cluster_status", map[string]interface{}{}},
	}

	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			req := makeRequest("tools/call", map[string]interface{}{
				"name":      tc.name,
				"arguments": tc.args,
			})

			resp, err := s.HandleJSONRPC(req)
			if err != nil {
				t.Fatalf("HandleJSONRPC returned error: %v", err)
			}

			r := parseResponse(t, resp)
			if r.Error != nil {
				t.Fatalf("Unexpected protocol error: %v", r.Error)
			}

			// Should get a result (not a protocol error)
			resultJSON, _ := json.Marshal(r.Result)
			var toolResult ToolResult
			json.Unmarshal(resultJSON, &toolResult)

			if len(toolResult.Content) == 0 {
				t.Error("No content in tool result")
			}
		})
	}
}

func TestNilProviders_ModifyRequiresProvider(t *testing.T) {
	s := NewServer(ServerConfig{}) // All providers nil

	// Modify operations should return errors when provider is nil
	t.Run("modify_backend", func(t *testing.T) {
		req := makeRequest("tools/call", map[string]interface{}{
			"name": "olb_modify_backend",
			"arguments": map[string]interface{}{
				"action":  "add",
				"pool":    "test",
				"address": "10.0.0.1:80",
			},
		})

		resp, _ := s.HandleJSONRPC(req)
		r := parseResponse(t, resp)

		resultJSON, _ := json.Marshal(r.Result)
		var toolResult ToolResult
		json.Unmarshal(resultJSON, &toolResult)

		if !toolResult.IsError {
			t.Error("Expected error when backend provider is nil")
		}
	})

	t.Run("modify_route", func(t *testing.T) {
		req := makeRequest("tools/call", map[string]interface{}{
			"name": "olb_modify_route",
			"arguments": map[string]interface{}{
				"action": "add",
				"host":   "test.com",
			},
		})

		resp, _ := s.HandleJSONRPC(req)
		r := parseResponse(t, resp)

		resultJSON, _ := json.Marshal(r.Result)
		var toolResult ToolResult
		json.Unmarshal(resultJSON, &toolResult)

		if !toolResult.IsError {
			t.Error("Expected error when route provider is nil")
		}
	})
}

func TestStdioTransport(t *testing.T) {
	s := newTestServer()

	// Prepare input: two JSON-RPC requests, one per line
	var input bytes.Buffer
	initReq := makeRequest("initialize", map[string]interface{}{})
	input.Write(initReq)
	input.WriteByte('\n')

	toolsReq := makeRequest("tools/list", nil)
	input.Write(toolsReq)
	input.WriteByte('\n')

	var output bytes.Buffer
	transport := NewStdioTransport(s, &input, &output)

	ctx := context.Background()
	err := transport.Run(ctx)
	if err != nil {
		t.Fatalf("StdioTransport.Run returned error: %v", err)
	}

	// Parse output lines
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 response lines, got %d: %q", len(lines), output.String())
	}

	// Verify first response (initialize)
	r1 := parseResponse(t, []byte(lines[0]))
	if r1.Error != nil {
		t.Errorf("Initialize response has error: %v", r1.Error)
	}

	// Verify second response (tools/list)
	r2 := parseResponse(t, []byte(lines[1]))
	if r2.Error != nil {
		t.Errorf("Tools/list response has error: %v", r2.Error)
	}
}

func TestStdioTransport_EmptyLines(t *testing.T) {
	s := newTestServer()

	var input bytes.Buffer
	input.WriteString("\n\n")
	initReq := makeRequest("initialize", nil)
	input.Write(initReq)
	input.WriteByte('\n')
	input.WriteString("\n")

	var output bytes.Buffer
	transport := NewStdioTransport(s, &input, &output)

	err := transport.Run(context.Background())
	if err != nil {
		t.Fatalf("StdioTransport.Run returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("Expected 1 response line, got %d", len(lines))
	}
}

func TestStdioTransport_ContextCancel(t *testing.T) {
	s := newTestServer()

	// Use a pipe so the reader blocks
	pr, pw := io.Pipe()
	var output bytes.Buffer
	transport := NewStdioTransport(s, pr, &output)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- transport.Run(ctx)
	}()

	// Send one request
	initReq := makeRequest("initialize", nil)
	pw.Write(initReq)
	pw.Write([]byte("\n"))

	// Close the pipe to let the scanner finish
	pw.Close()
	cancel()

	err := <-done
	// Should not get an error when the pipe is closed
	if err != nil && err != context.Canceled {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHTTPTransport_Handler(t *testing.T) {
	s := newTestServer()
	transport := NewHTTPTransport(s, ":0", "")

	// Test POST request
	reqBody := makeRequest("initialize", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", contentType)
	}

	r := parseResponse(t, w.Body.Bytes())
	if r.Error != nil {
		t.Errorf("Unexpected error: %v", r.Error)
	}
}

func TestHTTPTransport_MethodNotAllowed(t *testing.T) {
	s := newTestServer()
	transport := NewHTTPTransport(s, ":0", "")

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	transport.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHTTPTransport_StartStop(t *testing.T) {
	s := newTestServer()
	transport := NewHTTPTransport(s, "127.0.0.1:0", "")

	if err := transport.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	addr := transport.Addr()
	if addr == "" {
		t.Fatal("Addr is empty")
	}

	// Make a real HTTP request
	url := fmt.Sprintf("http://%s/mcp", addr)
	reqBody := makeRequest("initialize", nil)
	httpResp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", httpResp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(httpResp.Body)
	r := parseResponse(t, body)
	if r.Error != nil {
		t.Errorf("Unexpected error: %v", r.Error)
	}

	// Stop the server
	if err := transport.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestResponseIDPreservation(t *testing.T) {
	s := newTestServer()

	// Test with numeric ID
	req := `{"jsonrpc": "2.0", "id": 42, "method": "initialize"}`
	resp, err := s.HandleJSONRPC([]byte(req))
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.ID == nil {
		t.Fatal("Response ID is nil")
	}
	// JSON numbers unmarshal as float64
	if id, ok := r.ID.(float64); !ok || id != 42 {
		t.Errorf("Response ID = %v, want 42", r.ID)
	}

	// Test with string ID
	req2 := `{"jsonrpc": "2.0", "id": "abc-123", "method": "initialize"}`
	resp2, err := s.HandleJSONRPC([]byte(req2))
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r2 := parseResponse(t, resp2)
	if r2.ID != "abc-123" {
		t.Errorf("Response ID = %v, want abc-123", r2.ID)
	}
}

func TestRegisterCustomTool(t *testing.T) {
	s := newTestServer()

	// Register a custom tool
	s.RegisterTool(Tool{
		Name:        "custom_tool",
		Description: "A custom tool for testing",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"input": {Type: "string", Description: "Input value"},
			},
		},
	}, func(params map[string]interface{}) (interface{}, error) {
		input, _ := params["input"].(string)
		return map[string]interface{}{
			"echo": input,
		}, nil
	})

	// Call the custom tool
	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "custom_tool",
		"arguments": map[string]interface{}{"input": "hello"},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	if !strings.Contains(toolResult.Content[0].Text, "hello") {
		t.Error("Custom tool did not echo input")
	}
}

func TestListBackends_FilterByStatus(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name": "olb_list_backends",
		"arguments": map[string]interface{}{
			"pool":   "web-pool",
			"status": "healthy",
		},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	// Parse the result data
	var resultData map[string]interface{}
	json.Unmarshal([]byte(toolResult.Content[0].Text), &resultData)

	pools, ok := resultData["pools"].([]interface{})
	if !ok || len(pools) == 0 {
		t.Fatal("No pools in result")
	}

	pool := pools[0].(map[string]interface{})
	backends, ok := pool["backends"].([]interface{})
	if !ok {
		t.Fatal("No backends in pool")
	}

	// Should only have healthy backends (web-1 and web-2, not web-3)
	if len(backends) != 2 {
		t.Errorf("Expected 2 healthy backends, got %d", len(backends))
	}
}

func TestDiagnose_DefaultMode(t *testing.T) {
	s := newTestServer()

	req := makeRequest("tools/call", map[string]interface{}{
		"name":      "olb_diagnose",
		"arguments": map[string]interface{}{},
	})

	resp, err := s.HandleJSONRPC(req)
	if err != nil {
		t.Fatalf("HandleJSONRPC returned error: %v", err)
	}

	r := parseResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("Unexpected error: %v", r.Error)
	}

	resultJSON, _ := json.Marshal(r.Result)
	var toolResult ToolResult
	json.Unmarshal(resultJSON, &toolResult)

	// Default mode should be "full"
	if !strings.Contains(toolResult.Content[0].Text, "full") {
		t.Error("Default mode should be 'full'")
	}
}
