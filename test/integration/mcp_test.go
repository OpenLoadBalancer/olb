package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/mcp"
)

// ---------------------------------------------------------------------------
// Mock providers
// ---------------------------------------------------------------------------

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
	pools       []mcp.PoolInfo
	modifyError error
	lastAction  string
	lastPool    string
	lastAddr    string
}

func (m *mockBackendProvider) ListPools() []mcp.PoolInfo {
	return m.pools
}

func (m *mockBackendProvider) ModifyBackend(action, pool, addr string) error {
	m.lastAction = action
	m.lastPool = pool
	m.lastAddr = addr
	return m.modifyError
}

type mockConfigProvider struct {
	config interface{}
}

func (m *mockConfigProvider) GetConfig() interface{} {
	return m.config
}

type mockLogProvider struct {
	logs []mcp.LogEntry
}

func (m *mockLogProvider) GetLogs(count int, level string) []mcp.LogEntry {
	result := m.logs
	if level != "" {
		var filtered []mcp.LogEntry
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
	lastAction  string
	lastHost    string
	lastPath    string
	lastBackend string
}

func (m *mockRouteProvider) ModifyRoute(action, host, path, backend string) error {
	m.lastAction = action
	m.lastHost = host
	m.lastPath = path
	m.lastBackend = backend
	return m.modifyError
}

// newTestServer creates an MCP server with fully populated mock providers.
func newTestServer() *mcp.Server {
	return mcp.NewServer(mcp.ServerConfig{
		Metrics: &mockMetricsProvider{
			data: map[string]interface{}{
				"requests_total":      float64(42000),
				"requests_per_second": float64(150),
				"latency_p50_ms":      float64(12),
				"latency_p99_ms":      float64(85),
				"errors_total":        float64(7),
				"connections_active":  float64(320),
			},
		},
		Backends: &mockBackendProvider{
			pools: []mcp.PoolInfo{
				{
					Name:      "web",
					Algorithm: "round_robin",
					Backends: []mcp.BackendInfo{
						{ID: "web-1", Address: "10.0.0.1:8080", Status: "healthy", Weight: 100, Connections: 10},
						{ID: "web-2", Address: "10.0.0.2:8080", Status: "healthy", Weight: 100, Connections: 8},
						{ID: "web-3", Address: "10.0.0.3:8080", Status: "unhealthy", Weight: 100, Connections: 0},
					},
				},
				{
					Name:      "api",
					Algorithm: "least_connections",
					Backends: []mcp.BackendInfo{
						{ID: "api-1", Address: "10.0.1.1:9090", Status: "healthy", Weight: 50, Connections: 5},
					},
				},
			},
		},
		Config: &mockConfigProvider{
			config: map[string]interface{}{
				"version":   "1",
				"listeners": []interface{}{"http://0.0.0.0:80"},
			},
		},
		Logs: &mockLogProvider{
			logs: []mcp.LogEntry{
				{Timestamp: "2026-03-15T10:00:00Z", Level: "info", Message: "server started"},
				{Timestamp: "2026-03-15T10:01:00Z", Level: "error", Message: "backend web-3 health check failed"},
				{Timestamp: "2026-03-15T10:02:00Z", Level: "warn", Message: "high latency on route /api/v2"},
			},
		},
		Cluster: &mockClusterProvider{
			status: map[string]interface{}{
				"mode":    "cluster",
				"leader":  "node-1",
				"members": 3,
			},
		},
		Routes: &mockRouteProvider{},
	})
}

// ---------------------------------------------------------------------------
// JSON-RPC helpers
// ---------------------------------------------------------------------------

// rpcRequest builds a JSON-RPC 2.0 request as raw bytes.
func rpcRequest(id interface{}, method string, params interface{}) []byte {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		raw, _ := json.Marshal(params)
		req["params"] = json.RawMessage(raw)
	}
	data, _ := json.Marshal(req)
	return data
}

// rpcResponse is the generic JSON-RPC 2.0 response envelope.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcResponseErr `json:"error,omitempty"`
}

type rpcResponseErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func parseResponse(t *testing.T, data []byte) rpcResponse {
	t.Helper()
	var resp rpcResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, string(data))
	}
	return resp
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMCPStdioTransport exercises the stdio (line-delimited JSON) transport by
// sending initialize, tools/list, and tools/call through an in-memory pipe.
func TestMCPStdioTransport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()

	// Build a multi-line input containing several requests.
	var input bytes.Buffer
	input.Write(rpcRequest(1, "initialize", nil))
	input.WriteByte('\n')
	input.Write(rpcRequest(2, "tools/list", nil))
	input.WriteByte('\n')
	input.Write(rpcRequest(3, "tools/call", map[string]interface{}{
		"name":      "olb_query_metrics",
		"arguments": map[string]interface{}{"metric": "requests_total"},
	}))
	input.WriteByte('\n')

	var output bytes.Buffer
	transport := mcp.NewStdioTransport(srv, &input, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := transport.Run(ctx); err != nil {
		t.Fatalf("StdioTransport.Run: %v", err)
	}

	// Parse the line-delimited responses.
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 response lines, got %d:\n%s", len(lines), output.String())
	}

	// Response 1: initialize
	resp1 := parseResponse(t, []byte(lines[0]))
	if resp1.Error != nil {
		t.Fatalf("initialize error: %s", resp1.Error.Message)
	}
	var initResult map[string]interface{}
	if err := json.Unmarshal(resp1.Result, &initResult); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if _, ok := initResult["protocolVersion"]; !ok {
		t.Error("initialize result missing protocolVersion")
	}
	if _, ok := initResult["capabilities"]; !ok {
		t.Error("initialize result missing capabilities")
	}

	// Response 2: tools/list
	resp2 := parseResponse(t, []byte(lines[1]))
	if resp2.Error != nil {
		t.Fatalf("tools/list error: %s", resp2.Error.Message)
	}
	var toolsResult map[string]json.RawMessage
	if err := json.Unmarshal(resp2.Result, &toolsResult); err != nil {
		t.Fatalf("unmarshal tools/list result: %v", err)
	}
	if _, ok := toolsResult["tools"]; !ok {
		t.Error("tools/list result missing 'tools' key")
	}

	// Response 3: tools/call (olb_query_metrics)
	resp3 := parseResponse(t, []byte(lines[2]))
	if resp3.Error != nil {
		t.Fatalf("tools/call error: %s", resp3.Error.Message)
	}
}

// TestMCPHTTPTransport starts an HTTP MCP endpoint and sends requests over HTTP.
func TestMCPHTTPTransport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()
	transport := mcp.NewHTTPTransport(srv, "127.0.0.1:0", "")

	if err := transport.Start(); err != nil {
		t.Fatalf("HTTPTransport.Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = transport.Stop(ctx)
	})

	addr := transport.Addr()
	url := fmt.Sprintf("http://%s/mcp", addr)

	// Send initialize.
	body := rpcRequest(1, "initialize", nil)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST initialize: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want 200", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	rpcResp := parseResponse(t, respBody)
	if rpcResp.Error != nil {
		t.Fatalf("initialize error: %s", rpcResp.Error.Message)
	}

	// Send tools/list.
	body = rpcRequest(2, "tools/list", nil)
	resp2, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST tools/list: %v", err)
	}
	defer resp2.Body.Close()

	respBody2, _ := io.ReadAll(resp2.Body)
	rpcResp2 := parseResponse(t, respBody2)
	if rpcResp2.Error != nil {
		t.Fatalf("tools/list error: %s", rpcResp2.Error.Message)
	}

	// Verify GET is rejected (only POST allowed).
	getResp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", getResp.StatusCode)
	}
}

// TestMCPToolExecution calls each registered tool with appropriate parameters
// and verifies the results are not errors.
func TestMCPToolExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()

	tests := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{
			name: "query_metrics",
			tool: "olb_query_metrics",
			args: map[string]interface{}{"metric": "requests_total"},
		},
		{
			name: "list_backends",
			tool: "olb_list_backends",
			args: map[string]interface{}{},
		},
		{
			name: "list_backends_filtered",
			tool: "olb_list_backends",
			args: map[string]interface{}{"pool": "web", "status": "healthy"},
		},
		{
			name: "modify_backend",
			tool: "olb_modify_backend",
			args: map[string]interface{}{"action": "add", "pool": "web", "address": "10.0.0.4:8080"},
		},
		{
			name: "modify_route",
			tool: "olb_modify_route",
			args: map[string]interface{}{"action": "add", "host": "example.com", "path": "/v2", "backend": "api"},
		},
		{
			name: "diagnose_full",
			tool: "olb_diagnose",
			args: map[string]interface{}{"mode": "full"},
		},
		{
			name: "diagnose_errors",
			tool: "olb_diagnose",
			args: map[string]interface{}{"mode": "errors"},
		},
		{
			name: "diagnose_health",
			tool: "olb_diagnose",
			args: map[string]interface{}{"mode": "health"},
		},
		{
			name: "diagnose_capacity",
			tool: "olb_diagnose",
			args: map[string]interface{}{"mode": "capacity"},
		},
		{
			name: "diagnose_latency",
			tool: "olb_diagnose",
			args: map[string]interface{}{"mode": "latency"},
		},
		{
			name: "get_logs",
			tool: "olb_get_logs",
			args: map[string]interface{}{"count": float64(10), "level": "error"},
		},
		{
			name: "get_config",
			tool: "olb_get_config",
			args: map[string]interface{}{},
		},
		{
			name: "cluster_status",
			tool: "olb_cluster_status",
			args: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBytes := rpcRequest(1, "tools/call", map[string]interface{}{
				"name":      tt.tool,
				"arguments": tt.args,
			})

			respBytes, err := srv.HandleJSONRPC(reqBytes)
			if err != nil {
				t.Fatalf("HandleJSONRPC: %v", err)
			}

			resp := parseResponse(t, respBytes)
			if resp.Error != nil {
				t.Fatalf("JSON-RPC error: code=%d msg=%s", resp.Error.Code, resp.Error.Message)
			}

			// Unmarshal the tool result.
			var toolResult struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			}
			if err := json.Unmarshal(resp.Result, &toolResult); err != nil {
				t.Fatalf("unmarshal tool result: %v", err)
			}

			if toolResult.IsError {
				t.Errorf("tool returned isError=true: %s", toolResult.Content[0].Text)
			}
			if len(toolResult.Content) == 0 {
				t.Error("tool returned empty content")
			}
		})
	}
}

// TestMCPResourceRead reads each registered resource and verifies the content.
func TestMCPResourceRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()

	resources := []struct {
		uri      string
		contains string // substring expected in the text content
	}{
		{"olb://metrics", "requests_total"},
		{"olb://config", "version"},
		{"olb://health", "web"},
		{"olb://logs", "server started"},
	}

	for _, rc := range resources {
		t.Run(rc.uri, func(t *testing.T) {
			reqBytes := rpcRequest(1, "resources/read", map[string]interface{}{
				"uri": rc.uri,
			})

			respBytes, err := srv.HandleJSONRPC(reqBytes)
			if err != nil {
				t.Fatalf("HandleJSONRPC: %v", err)
			}

			resp := parseResponse(t, respBytes)
			if resp.Error != nil {
				t.Fatalf("JSON-RPC error: code=%d msg=%s", resp.Error.Code, resp.Error.Message)
			}

			var readResult struct {
				Contents []struct {
					URI      string `json:"uri"`
					MimeType string `json:"mimeType"`
					Text     string `json:"text"`
				} `json:"contents"`
			}
			if err := json.Unmarshal(resp.Result, &readResult); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if len(readResult.Contents) == 0 {
				t.Fatal("no contents returned")
			}

			content := readResult.Contents[0]
			if content.URI != rc.uri {
				t.Errorf("content URI = %q, want %q", content.URI, rc.uri)
			}
			if content.MimeType != "application/json" {
				t.Errorf("mimeType = %q, want application/json", content.MimeType)
			}
			if !strings.Contains(content.Text, rc.contains) {
				t.Errorf("content text does not contain %q:\n%s", rc.contains, content.Text)
			}
		})
	}

	// Verify unknown resource returns error.
	t.Run("unknown_resource", func(t *testing.T) {
		reqBytes := rpcRequest(1, "resources/read", map[string]interface{}{
			"uri": "olb://nonexistent",
		})
		respBytes, _ := srv.HandleJSONRPC(reqBytes)
		resp := parseResponse(t, respBytes)
		if resp.Error == nil {
			t.Error("expected error for unknown resource")
		}
	})
}

// TestMCPPromptGet retrieves each registered prompt template and verifies it
// returns proper messages.
func TestMCPPromptGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()

	prompts := []struct {
		name     string
		args     map[string]interface{}
		contains string // substring expected in the first message text
	}{
		{
			name:     "diagnose",
			args:     map[string]interface{}{"target": "web"},
			contains: "Analyze the OpenLoadBalancer",
		},
		{
			name:     "capacity_planning",
			args:     map[string]interface{}{"pool": "api"},
			contains: "capacity",
		},
		{
			name: "canary_deploy",
			args: map[string]interface{}{
				"route":       "/api",
				"new_backend": "10.0.0.5:8080",
				"percentage":  "5",
			},
			contains: "canary",
		},
	}

	for _, pc := range prompts {
		t.Run(pc.name, func(t *testing.T) {
			reqBytes := rpcRequest(1, "prompts/get", map[string]interface{}{
				"name":      pc.name,
				"arguments": pc.args,
			})

			respBytes, err := srv.HandleJSONRPC(reqBytes)
			if err != nil {
				t.Fatalf("HandleJSONRPC: %v", err)
			}

			resp := parseResponse(t, respBytes)
			if resp.Error != nil {
				t.Fatalf("JSON-RPC error: code=%d msg=%s", resp.Error.Code, resp.Error.Message)
			}

			var promptResult struct {
				Description string `json:"description"`
				Messages    []struct {
					Role    string `json:"role"`
					Content struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(resp.Result, &promptResult); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if len(promptResult.Messages) == 0 {
				t.Fatal("no messages returned")
			}

			msg := promptResult.Messages[0]
			if msg.Role != "user" {
				t.Errorf("message role = %q, want user", msg.Role)
			}
			if msg.Content.Type != "text" {
				t.Errorf("content type = %q, want text", msg.Content.Type)
			}
			if !strings.Contains(strings.ToLower(msg.Content.Text), strings.ToLower(pc.contains)) {
				t.Errorf("message text does not contain %q:\n%s", pc.contains, msg.Content.Text)
			}
		})
	}

	// Unknown prompt.
	t.Run("unknown_prompt", func(t *testing.T) {
		reqBytes := rpcRequest(1, "prompts/get", map[string]interface{}{
			"name": "nonexistent",
		})
		respBytes, _ := srv.HandleJSONRPC(reqBytes)
		resp := parseResponse(t, respBytes)
		if resp.Error == nil {
			t.Error("expected error for unknown prompt")
		}
	})
}

// TestMCPFullWorkflow exercises a realistic workflow:
// initialize -> list tools -> call diagnose -> read metrics resource.
func TestMCPFullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()

	// Step 1: Initialize.
	resp := callRPC(t, srv, 1, "initialize", nil)
	var initResult map[string]interface{}
	mustUnmarshal(t, resp.Result, &initResult)
	if initResult["protocolVersion"] == nil {
		t.Fatal("initialize: missing protocolVersion")
	}
	t.Log("step 1: initialized")

	// Step 2: List tools.
	resp = callRPC(t, srv, 2, "tools/list", nil)
	var toolsResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	mustUnmarshal(t, resp.Result, &toolsResult)
	if len(toolsResult.Tools) == 0 {
		t.Fatal("tools/list returned 0 tools")
	}

	// Verify expected tool names are present.
	toolNames := make(map[string]bool)
	for _, tool := range toolsResult.Tools {
		toolNames[tool.Name] = true
	}
	expectedTools := []string{
		"olb_query_metrics", "olb_list_backends", "olb_modify_backend",
		"olb_modify_route", "olb_diagnose", "olb_get_logs",
		"olb_get_config", "olb_cluster_status",
	}
	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("missing expected tool: %s", name)
		}
	}
	t.Logf("step 2: listed %d tools", len(toolsResult.Tools))

	// Step 3: Call diagnose.
	resp = callRPC(t, srv, 3, "tools/call", map[string]interface{}{
		"name":      "olb_diagnose",
		"arguments": map[string]interface{}{"mode": "full"},
	})
	var diagnoseResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	mustUnmarshal(t, resp.Result, &diagnoseResult)
	if diagnoseResult.IsError {
		t.Fatal("diagnose returned isError=true")
	}
	if len(diagnoseResult.Content) == 0 || diagnoseResult.Content[0].Text == "" {
		t.Fatal("diagnose returned empty content")
	}
	t.Log("step 3: diagnose completed")

	// Step 4: Read metrics resource.
	resp = callRPC(t, srv, 4, "resources/read", map[string]interface{}{
		"uri": "olb://metrics",
	})
	var metricsRead struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	mustUnmarshal(t, resp.Result, &metricsRead)
	if len(metricsRead.Contents) == 0 || metricsRead.Contents[0].Text == "" {
		t.Fatal("metrics resource returned empty")
	}
	if !strings.Contains(metricsRead.Contents[0].Text, "requests_total") {
		t.Error("metrics resource missing requests_total")
	}
	t.Log("step 4: read metrics resource")

	// Step 5: List resources.
	resp = callRPC(t, srv, 5, "resources/list", nil)
	var resList struct {
		Resources []struct {
			URI  string `json:"uri"`
			Name string `json:"name"`
		} `json:"resources"`
	}
	mustUnmarshal(t, resp.Result, &resList)
	if len(resList.Resources) < 4 {
		t.Errorf("expected at least 4 resources, got %d", len(resList.Resources))
	}
	t.Logf("step 5: listed %d resources", len(resList.Resources))

	// Step 6: List prompts.
	resp = callRPC(t, srv, 6, "prompts/list", nil)
	var promptList struct {
		Prompts []struct {
			Name string `json:"name"`
		} `json:"prompts"`
	}
	mustUnmarshal(t, resp.Result, &promptList)
	if len(promptList.Prompts) < 3 {
		t.Errorf("expected at least 3 prompts, got %d", len(promptList.Prompts))
	}
	t.Logf("step 6: listed %d prompts", len(promptList.Prompts))
}

// TestMCPErrorCases covers various error paths in the MCP protocol handling.
func TestMCPErrorCases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()

	t.Run("invalid_json", func(t *testing.T) {
		respBytes, err := srv.HandleJSONRPC([]byte(`{not valid json`))
		if err != nil {
			t.Fatalf("HandleJSONRPC: %v", err)
		}
		resp := parseResponse(t, respBytes)
		if resp.Error == nil {
			t.Error("expected parse error")
		}
		if resp.Error != nil && resp.Error.Code != -32700 {
			t.Errorf("error code = %d, want -32700", resp.Error.Code)
		}
	})

	t.Run("unknown_method", func(t *testing.T) {
		resp := callRPCExpectError(t, srv, 1, "nonexistent/method", nil)
		if resp.Error.Code != -32601 {
			t.Errorf("error code = %d, want -32601", resp.Error.Code)
		}
	})

	t.Run("wrong_jsonrpc_version", func(t *testing.T) {
		req := map[string]interface{}{
			"jsonrpc": "1.0",
			"id":      1,
			"method":  "initialize",
		}
		reqBytes, _ := json.Marshal(req)
		respBytes, err := srv.HandleJSONRPC(reqBytes)
		if err != nil {
			t.Fatalf("HandleJSONRPC: %v", err)
		}
		resp := parseResponse(t, respBytes)
		if resp.Error == nil {
			t.Error("expected error for wrong jsonrpc version")
		}
	})

	t.Run("tools_call_missing_name", func(t *testing.T) {
		resp := callRPCExpectError(t, srv, 1, "tools/call", map[string]interface{}{
			"arguments": map[string]interface{}{},
		})
		if resp.Error == nil {
			t.Error("expected error for missing tool name")
		}
	})

	t.Run("tools_call_unknown_tool", func(t *testing.T) {
		resp := callRPCExpectError(t, srv, 1, "tools/call", map[string]interface{}{
			"name":      "nonexistent_tool",
			"arguments": map[string]interface{}{},
		})
		if resp.Error == nil {
			t.Error("expected error for unknown tool")
		}
	})

	t.Run("resources_read_missing_uri", func(t *testing.T) {
		resp := callRPCExpectError(t, srv, 1, "resources/read", map[string]interface{}{})
		if resp.Error == nil {
			t.Error("expected error for missing URI")
		}
	})

	t.Run("prompts_get_missing_name", func(t *testing.T) {
		resp := callRPCExpectError(t, srv, 1, "prompts/get", map[string]interface{}{})
		if resp.Error == nil {
			t.Error("expected error for missing prompt name")
		}
	})
}

// TestMCPHTTPConcurrent sends concurrent requests to the HTTP transport to
// verify thread safety.
func TestMCPHTTPConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	srv := newTestServer()
	transport := mcp.NewHTTPTransport(srv, "127.0.0.1:0", "")

	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = transport.Stop(ctx)
	})

	url := fmt.Sprintf("http://%s/mcp", transport.Addr())

	const goroutines = 10
	const requestsPerGoroutine = 5

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*requestsPerGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for r := 0; r < requestsPerGoroutine; r++ {
				id := gID*requestsPerGoroutine + r
				body := rpcRequest(id, "tools/list", nil)
				resp, err := http.Post(url, "application/json", bytes.NewReader(body))
				if err != nil {
					errCh <- fmt.Errorf("goroutine %d request %d: %w", gID, r, err)
					continue
				}
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("goroutine %d request %d: status %d", gID, r, resp.StatusCode)
					continue
				}

				rpcResp := parseResponse(t, respBody)
				if rpcResp.Error != nil {
					errCh <- fmt.Errorf("goroutine %d request %d: rpc error: %s", gID, r, rpcResp.Error.Message)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// callRPC sends a JSON-RPC request and returns the parsed response, failing
// the test on any transport or protocol error.
func callRPC(t *testing.T, srv *mcp.Server, id interface{}, method string, params interface{}) rpcResponse {
	t.Helper()
	reqBytes := rpcRequest(id, method, params)
	respBytes, err := srv.HandleJSONRPC(reqBytes)
	if err != nil {
		t.Fatalf("HandleJSONRPC(%s): %v", method, err)
	}
	resp := parseResponse(t, respBytes)
	if resp.Error != nil {
		t.Fatalf("%s: JSON-RPC error: code=%d msg=%s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp
}

// callRPCExpectError sends a JSON-RPC request and returns the response,
// expecting it to contain an error.
func callRPCExpectError(t *testing.T, srv *mcp.Server, id interface{}, method string, params interface{}) rpcResponse {
	t.Helper()
	reqBytes := rpcRequest(id, method, params)
	respBytes, err := srv.HandleJSONRPC(reqBytes)
	if err != nil {
		t.Fatalf("HandleJSONRPC(%s): %v", method, err)
	}
	return parseResponse(t, respBytes)
}

// mustUnmarshal unmarshals JSON or fails the test.
func mustUnmarshal(t *testing.T, data json.RawMessage, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, string(data))
	}
}
