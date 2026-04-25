// Package mcp implements a Model Context Protocol (MCP) server for OpenLoadBalancer.
// It exposes load balancer management capabilities as MCP tools, resources, and prompts,
// enabling AI agents to query metrics, modify backends/routes, and diagnose issues
// through a standardized JSON-RPC 2.0 interface.
//
// Supports two transports:
//   - StdioTransport: reads JSON-RPC from stdin, writes to stdout (line-delimited)
//   - HTTPTransport: HTTP POST endpoint for JSON-RPC requests
package mcp

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/pkg/version"
)

// JSON-RPC 2.0 protocol version.
const jsonRPCVersion = "2.0"

// MCP protocol version.
const mcpProtocolVersion = "2024-11-05"

// Server name and version.
const (
	serverName = "olb-mcp"
)

// JSON-RPC error codes.
const (
	errCodeParse          = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

// --- JSON-RPC types ---

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC 2.0 error.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// --- MCP types ---

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema represents the JSON Schema for a tool's input parameters.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property represents a JSON Schema property.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Resource represents an MCP resource definition.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// ResourceContent represents the content of a resource read.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// Prompt represents an MCP prompt template.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument represents an argument for a prompt template.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// PromptMessage represents a message in a prompt result.
type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

// PromptContent represents the content of a prompt message.
type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult represents the result of a tool call.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent represents a content item in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Provider interfaces ---

// PoolInfo holds information about a backend pool.
type PoolInfo struct {
	Name      string        `json:"name"`
	Algorithm string        `json:"algorithm"`
	Backends  []BackendInfo `json:"backends"`
}

// BackendInfo holds information about a single backend.
type BackendInfo struct {
	ID          string `json:"id"`
	Address     string `json:"address"`
	Status      string `json:"status"`
	Weight      int    `json:"weight"`
	Connections int64  `json:"connections"`
}

// LogEntry represents a single log entry.
type LogEntry struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// MetricsProvider provides access to load balancer metrics.
type MetricsProvider interface {
	QueryMetrics(pattern string) map[string]any
}

// BackendProvider provides access to backend pool management.
type BackendProvider interface {
	ListPools() []PoolInfo
	ModifyBackend(action, pool, addr string) error
}

// ConfigProvider provides access to the current configuration.
type ConfigProvider interface {
	GetConfig() any
}

// LogProvider provides access to recent log entries.
type LogProvider interface {
	GetLogs(count int, level string) []LogEntry
}

// ClusterProvider provides access to cluster status information.
type ClusterProvider interface {
	GetStatus() any
}

// RouteProvider provides access to route management.
type RouteProvider interface {
	ModifyRoute(action, host, path, backend string) error
}

// --- Tool permission levels ---

// ToolPermission represents the permission level required to invoke a tool.
type ToolPermission int

const (
	// PermissionRead allows read-only operations (querying metrics, listing backends, etc.).
	PermissionRead ToolPermission = iota
	// PermissionWrite allows destructive/mutating operations (modifying backends, routes, etc.).
	PermissionWrite
)

// --- Tool handler type ---

// ToolHandler is a function that handles a tool call.
type ToolHandler func(params map[string]any) (any, error)

// --- Server ---

// Server is the MCP server implementation.
type Server struct {
	mu sync.RWMutex

	tools     map[string]Tool
	resources map[string]Resource
	prompts   map[string]Prompt

	toolHandlers map[string]ToolHandler

	// toolMeta maps tool name to required permission level.
	toolMeta map[string]ToolPermission

	// tokenPermissions maps bearer token to its granted permission level.
	// If nil or empty, all tools are accessible (backward compatible).
	tokenPermissions map[string]ToolPermission

	metrics  MetricsProvider
	backends BackendProvider
	config   ConfigProvider
	logs     LogProvider
	cluster  ClusterProvider
	routes   RouteProvider
}

// ServerConfig holds configuration for the MCP server.
type ServerConfig struct {
	Metrics  MetricsProvider
	Backends BackendProvider
	Config   ConfigProvider
	Logs     LogProvider
	Cluster  ClusterProvider
	Routes   RouteProvider

	// TokenPermissions maps bearer token to its granted permission level.
	// If nil or empty, all tools are accessible by any authenticated token
	// (backward compatible behavior).
	TokenPermissions map[string]ToolPermission
}

// NewServer creates a new MCP server with the given providers.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		tools:            make(map[string]Tool),
		resources:        make(map[string]Resource),
		prompts:          make(map[string]Prompt),
		toolHandlers:     make(map[string]ToolHandler),
		toolMeta:         make(map[string]ToolPermission),
		tokenPermissions: cfg.TokenPermissions,
		metrics:          cfg.Metrics,
		backends:         cfg.Backends,
		config:           cfg.Config,
		logs:             cfg.Logs,
		cluster:          cfg.Cluster,
		routes:           cfg.Routes,
	}

	s.registerDefaultTools()
	s.registerDefaultResources()
	s.registerDefaultPrompts()

	return s
}

// registerDefaultTools registers the built-in MCP tools.
func (s *Server) registerDefaultTools() {
	// olb_query_metrics
	s.RegisterTool(Tool{
		Name:        "olb_query_metrics",
		Description: "Query load balancer metrics. Get RPS, latency, error rates, connection counts for routes, backends, or globally.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"metric": {Type: "string", Description: "Metric name or pattern (e.g., 'requests_total', 'latency_*')"},
				"scope":  {Type: "string", Description: "Scope of the query", Enum: []string{"global", "route", "backend", "listener"}},
				"target": {Type: "string", Description: "Route name or backend pool:address"},
				"range":  {Type: "string", Description: "Time range (e.g., '5m', '1h', '24h')"},
			},
			Required: []string{"metric"},
		},
	}, s.handleQueryMetrics)

	// olb_list_backends
	s.RegisterTool(Tool{
		Name:        "olb_list_backends",
		Description: "List all backend pools and their backends with current status, health, connections, and performance.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"pool":   {Type: "string", Description: "Filter by pool name"},
				"status": {Type: "string", Description: "Filter by status", Enum: []string{"all", "healthy", "unhealthy", "draining"}},
			},
		},
	}, s.handleListBackends)

	// olb_modify_backend (requires write permission)
	s.RegisterToolWithPermission(Tool{
		Name:        "olb_modify_backend",
		Description: "Add, remove, drain, enable, or disable a backend in a pool.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"action":  {Type: "string", Description: "Action to perform", Enum: []string{"add", "remove", "drain", "enable", "disable"}},
				"pool":    {Type: "string", Description: "Backend pool name"},
				"address": {Type: "string", Description: "Backend address (host:port)"},
			},
			Required: []string{"action", "pool", "address"},
		},
	}, s.handleModifyBackend, PermissionWrite)

	// olb_modify_route (requires write permission)
	s.RegisterToolWithPermission(Tool{
		Name:        "olb_modify_route",
		Description: "Add, update, or remove a route. Supports traffic splitting for canary deployments.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"action":  {Type: "string", Description: "Action to perform", Enum: []string{"add", "update", "remove"}},
				"host":    {Type: "string", Description: "Host to match"},
				"path":    {Type: "string", Description: "Path to match"},
				"backend": {Type: "string", Description: "Backend pool name"},
			},
			Required: []string{"action"},
		},
	}, s.handleModifyRoute, PermissionWrite)

	// olb_diagnose
	s.RegisterTool(Tool{
		Name:        "olb_diagnose",
		Description: "Diagnose issues. Analyze error patterns, detect anomalies, check configuration for problems, and suggest fixes.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"mode":   {Type: "string", Description: "Diagnostic mode", Enum: []string{"errors", "latency", "capacity", "health", "config", "full"}},
				"target": {Type: "string", Description: "Scope: route name, backend pool, or 'all'"},
			},
		},
	}, s.handleDiagnose)

	// olb_get_logs
	s.RegisterTool(Tool{
		Name:        "olb_get_logs",
		Description: "Search and retrieve access logs and error logs.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"count":  {Type: "integer", Description: "Number of log entries to return"},
				"level":  {Type: "string", Description: "Minimum log level", Enum: []string{"trace", "debug", "info", "warn", "error"}},
				"filter": {Type: "string", Description: "Search filter string"},
			},
		},
	}, s.handleGetLogs)

	// olb_get_config
	s.RegisterTool(Tool{
		Name:        "olb_get_config",
		Description: "Get current configuration as JSON.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"section": {Type: "string", Description: "Config section: global, listeners, backends, routes, cluster, or 'all'"},
			},
		},
	}, s.handleGetConfig)

	// olb_cluster_status
	s.RegisterTool(Tool{
		Name:        "olb_cluster_status",
		Description: "Get cluster status including node list, Raft state, leader info, and replication lag.",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}, s.handleClusterStatus)
}

// registerDefaultResources registers the built-in MCP resources.
func (s *Server) registerDefaultResources() {
	s.RegisterResource(Resource{
		URI:         "olb://metrics",
		Name:        "Live Dashboard Metrics",
		Description: "Real-time metrics suitable for a dashboard view",
		MimeType:    "application/json",
	})

	s.RegisterResource(Resource{
		URI:         "olb://config",
		Name:        "Current Configuration",
		Description: "Full current configuration in JSON",
		MimeType:    "application/json",
	})

	s.RegisterResource(Resource{
		URI:         "olb://health",
		Name:        "Health Summary",
		Description: "All backends health status",
		MimeType:    "application/json",
	})

	s.RegisterResource(Resource{
		URI:         "olb://logs",
		Name:        "Recent Logs",
		Description: "Last 100 log entries",
		MimeType:    "application/json",
	})
}

// registerDefaultPrompts registers the built-in MCP prompt templates.
func (s *Server) registerDefaultPrompts() {
	s.RegisterPrompt(Prompt{
		Name:        "diagnose",
		Description: "Analyze the load balancer and identify issues",
		Arguments: []PromptArgument{
			{Name: "target", Description: "Route or backend pool to diagnose, or 'all'", Required: false},
		},
	})

	s.RegisterPrompt(Prompt{
		Name:        "capacity_planning",
		Description: "Review current capacity and recommend scaling",
		Arguments: []PromptArgument{
			{Name: "pool", Description: "Backend pool to analyze", Required: true},
		},
	})

	s.RegisterPrompt(Prompt{
		Name:        "canary_deploy",
		Description: "Guide a canary deployment for a service",
		Arguments: []PromptArgument{
			{Name: "route", Description: "Route to deploy canary on", Required: true},
			{Name: "new_backend", Description: "New backend address", Required: true},
			{Name: "percentage", Description: "Initial traffic percentage for canary", Required: true},
		},
	})
}

// RegisterTool registers a tool with its handler.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
	s.toolHandlers[tool.Name] = handler
}

// RegisterToolWithPermission registers a tool with its handler and required permission level.
func (s *Server) RegisterToolWithPermission(tool Tool, handler ToolHandler, perm ToolPermission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
	s.toolHandlers[tool.Name] = handler
	s.toolMeta[tool.Name] = perm
}

// checkPermission verifies that the given token has the required permission for a tool.
// If tokenPermissions is nil/empty (no auth config), all tools are accessible (backward compatible).
// Returns true if access is allowed, false otherwise.
func (s *Server) checkPermission(toolName, token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// No permission config set: allow all access (backward compatible)
	if len(s.tokenPermissions) == 0 {
		return true
	}

	// No token provided but permissions are configured
	if token == "" {
		return false
	}

	// Look up the token's permission level
	tokenPerm, ok := s.tokenPermissions[token]
	if !ok {
		return false
	}

	// Look up the tool's required permission
	requiredPerm, ok := s.toolMeta[toolName]
	if !ok {
		// Tool has no explicit permission requirement: default to read
		requiredPerm = PermissionRead
	}

	// Write permission implies read permission
	return tokenPerm >= requiredPerm
}

// RegisterResource registers a resource.
func (s *Server) RegisterResource(resource Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource.URI] = resource
}

// RegisterPrompt registers a prompt template.
func (s *Server) RegisterPrompt(prompt Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = prompt
}

// HandleJSONRPC processes a JSON-RPC 2.0 request and returns the response.
// No token is provided, so permission checks use the default (full access
// when tokenPermissions is not configured, denied when it is configured).
func (s *Server) HandleJSONRPC(request []byte) ([]byte, error) {
	return s.HandleJSONRPCWithToken(request, "")
}

// HandleJSONRPCWithToken processes a JSON-RPC 2.0 request with an optional
// bearer token for tool-level permission checking.
func (s *Server) HandleJSONRPCWithToken(request []byte, token string) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(request, &req); err != nil {
		resp := Response{
			JSONRPC: jsonRPCVersion,
			Error: &ResponseError{
				Code:    errCodeParse,
				Message: "Parse error",
			},
		}
		return json.Marshal(resp)
	}

	if req.JSONRPC != jsonRPCVersion {
		resp := Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error: &ResponseError{
				Code:    errCodeInvalidRequest,
				Message: "Invalid JSON-RPC version",
			},
		}
		return json.Marshal(resp)
	}

	result, rpcErr := s.dispatchWithToken(req.Method, req.Params, token)
	resp := Response{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	return json.Marshal(resp)
}

// dispatch routes a method call to the appropriate handler.
func (s *Server) dispatch(method string, params json.RawMessage) (any, *ResponseError) {
	return s.dispatchWithToken(method, params, "")
}

// dispatchWithToken routes a method call to the appropriate handler with token-based permission checking.
func (s *Server) dispatchWithToken(method string, params json.RawMessage, token string) (any, *ResponseError) {
	switch method {
	case "initialize":
		return s.handleInitialize(params)
	case "tools/list":
		return s.handleToolsList(params)
	case "tools/call":
		return s.handleToolsCallWithPerm(params, token)
	case "resources/list":
		return s.handleResourcesList(params)
	case "resources/read":
		return s.handleResourcesRead(params)
	case "prompts/list":
		return s.handlePromptsList(params)
	case "prompts/get":
		return s.handlePromptsGet(params)
	default:
		return nil, &ResponseError{
			Code:    errCodeMethodNotFound,
			Message: "Method not found",
		}
	}
}

// --- Protocol method handlers ---

// handleInitialize handles the "initialize" method.
func (s *Server) handleInitialize(_ json.RawMessage) (any, *ResponseError) {
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
			"prompts":   map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": version.Version,
		},
	}, nil
}

// sanitizeMCPError maps internal errors to safe messages for MCP clients.
// Validation/parameter errors are passed through since they are user-facing by design.
// Internal infrastructure errors are mapped to generic messages to prevent info leakage.
func sanitizeMCPError(err error) string {
	if err == nil {
		return "internal error"
	}
	msg := err.Error()
	// Pass through user-facing validation errors (parameter names are safe to expose)
	if strings.Contains(msg, "parameter is required") ||
		strings.Contains(msg, "not configured") ||
		strings.Contains(msg, "analytics not available") ||
		strings.Contains(msg, "conflict") {
		return msg
	}
	// Map common internal errors to generic messages
	if strings.Contains(msg, "not found") {
		return "resource not found"
	}
	if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") {
		return "access denied"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return "operation timed out"
	}
	if strings.Contains(msg, "unavailable") || strings.Contains(msg, "connection refused") {
		return "service unavailable"
	}
	// Generic fallback — do not leak internal details
	return "internal error"
}

// handleToolsList handles the "tools/list" method.
func (s *Server) handleToolsList(_ json.RawMessage) (any, *ResponseError) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}

	return map[string]any{
		"tools": tools,
	}, nil
}

// handleToolsCallWithPerm handles the "tools/call" method with permission checking.
func (s *Server) handleToolsCallWithPerm(params json.RawMessage, token string) (any, *ResponseError) {
	if params == nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing params",
		}
	}

	var callParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Invalid params",
		}
	}

	if callParams.Name == "" {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing tool name",
		}
	}

	// Check tool-level permission
	if !s.checkPermission(callParams.Name, token) {
		return nil, &ResponseError{
			Code:    errCodeInternal,
			Message: "permission denied: token does not have required permission for this tool",
		}
	}

	s.mu.RLock()
	handler, ok := s.toolHandlers[callParams.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Unknown tool",
		}
	}

	if callParams.Arguments == nil {
		callParams.Arguments = make(map[string]any)
	}

	result, err := handler(callParams.Arguments)
	if err != nil {
		// Map internal errors to generic messages for MCP clients
		errMsg := sanitizeMCPError(err)
		return ToolResult{
			Content: []ToolContent{{Type: "text", Text: "error: " + errMsg}},
			IsError: true,
		}, nil
	}

	// Marshal result to JSON text for the tool content
	resultJSON, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return ToolResult{
			Content: []ToolContent{{Type: "text", Text: "Failed to marshal response"}},
			IsError: true,
		}, nil
	}

	return ToolResult{
		Content: []ToolContent{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// handleToolsCall handles the "tools/call" method (no token, used internally).
func (s *Server) handleToolsCall(params json.RawMessage) (any, *ResponseError) {
	if params == nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing params",
		}
	}

	var callParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Invalid params",
		}
	}

	if callParams.Name == "" {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing tool name",
		}
	}

	s.mu.RLock()
	handler, ok := s.toolHandlers[callParams.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Unknown tool",
		}
	}

	if callParams.Arguments == nil {
		callParams.Arguments = make(map[string]any)
	}

	result, err := handler(callParams.Arguments)
	if err != nil {
		// Map internal errors to generic messages for MCP clients
		errMsg := sanitizeMCPError(err)
		return ToolResult{
			Content: []ToolContent{{Type: "text", Text: "error: " + errMsg}},
			IsError: true,
		}, nil
	}

	// Marshal result to JSON text for the tool content
	resultJSON, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return ToolResult{
			Content: []ToolContent{{Type: "text", Text: "Failed to marshal response"}},
			IsError: true,
		}, nil
	}

	return ToolResult{
		Content: []ToolContent{{Type: "text", Text: string(resultJSON)}},
	}, nil
}

// handleResourcesList handles the "resources/list" method.
func (s *Server) handleResourcesList(_ json.RawMessage) (any, *ResponseError) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resources := make([]Resource, 0, len(s.resources))
	for _, resource := range s.resources {
		resources = append(resources, resource)
	}

	return map[string]any{
		"resources": resources,
	}, nil
}

// handleResourcesRead handles the "resources/read" method.
func (s *Server) handleResourcesRead(params json.RawMessage) (any, *ResponseError) {
	if params == nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing params",
		}
	}

	var readParams struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &readParams); err != nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Invalid params",
		}
	}

	if readParams.URI == "" {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing resource URI",
		}
	}

	s.mu.RLock()
	_, ok := s.resources[readParams.URI]
	s.mu.RUnlock()

	if !ok {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Unknown resource",
		}
	}

	content, rpcErr := s.readResource(readParams.URI)
	if rpcErr != nil {
		return nil, rpcErr
	}

	return map[string]any{
		"contents": []ResourceContent{
			{
				URI:      readParams.URI,
				MimeType: "application/json",
				Text:     content,
			},
		},
	}, nil
}

// readResource reads the content of a resource by URI.
func (s *Server) readResource(uri string) (string, *ResponseError) {
	var data any

	switch uri {
	case "olb://metrics":
		if s.metrics != nil {
			data = s.metrics.QueryMetrics("*")
		} else {
			data = map[string]any{"message": "metrics provider not configured"}
		}

	case "olb://config":
		if s.config != nil {
			data = s.config.GetConfig()
		} else {
			data = map[string]any{"message": "config provider not configured"}
		}

	case "olb://health":
		if s.backends != nil {
			data = s.backends.ListPools()
		} else {
			data = map[string]any{"message": "backend provider not configured"}
		}

	case "olb://logs":
		if s.logs != nil {
			data = s.logs.GetLogs(100, "")
		} else {
			data = map[string]any{"message": "log provider not configured"}
		}

	default:
		return "", &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Unknown resource",
		}
	}

	result, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", &ResponseError{
			Code:    errCodeInternal,
			Message: "Failed to read resource",
		}
	}

	return string(result), nil
}

// handlePromptsList handles the "prompts/list" method.
func (s *Server) handlePromptsList(_ json.RawMessage) (any, *ResponseError) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prompts := make([]Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, prompt)
	}

	return map[string]any{
		"prompts": prompts,
	}, nil
}

// handlePromptsGet handles the "prompts/get" method.
func (s *Server) handlePromptsGet(params json.RawMessage) (any, *ResponseError) {
	if params == nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing params",
		}
	}

	var getParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &getParams); err != nil {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Invalid params",
		}
	}

	if getParams.Name == "" {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Missing prompt name",
		}
	}

	s.mu.RLock()
	prompt, ok := s.prompts[getParams.Name]
	s.mu.RUnlock()

	if !ok {
		return nil, &ResponseError{
			Code:    errCodeInvalidParams,
			Message: "Unknown prompt",
		}
	}

	messages := s.generatePromptMessages(prompt, getParams.Arguments)

	return map[string]any{
		"description": prompt.Description,
		"messages":    messages,
	}, nil
}

// generatePromptMessages generates the messages for a prompt template.
func (s *Server) generatePromptMessages(prompt Prompt, args map[string]any) []PromptMessage {
	switch prompt.Name {
	case "diagnose":
		target := "all"
		if t, ok := args["target"]; ok {
			target = fmt.Sprintf("%v", t)
		}
		return []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(
						"Analyze the OpenLoadBalancer system and identify any issues.\n\n"+
							"Target: %s\n\n"+
							"Please use the following tools to gather data:\n"+
							"1. olb_query_metrics - Check error rates, latency, and throughput\n"+
							"2. olb_list_backends - Check backend health status\n"+
							"3. olb_get_logs - Check recent error logs\n"+
							"4. olb_diagnose - Run automated diagnostics\n\n"+
							"Provide a summary of findings and recommended actions.",
						target,
					),
				},
			},
		}

	case "capacity_planning":
		pool := ""
		if p, ok := args["pool"]; ok {
			pool = fmt.Sprintf("%v", p)
		}
		return []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(
						"Review the current capacity of the OpenLoadBalancer and recommend scaling actions.\n\n"+
							"Pool: %s\n\n"+
							"Please use the following tools to gather data:\n"+
							"1. olb_query_metrics - Check current RPS, connection counts, and resource usage\n"+
							"2. olb_list_backends - Check current backend pool sizes and health\n"+
							"3. olb_diagnose with mode 'capacity' - Run capacity analysis\n\n"+
							"Provide recommendations for scaling, including:\n"+
							"- Whether to add/remove backends\n"+
							"- Suggested backend count for current and projected load\n"+
							"- Any bottlenecks identified",
						pool,
					),
				},
			},
		}

	case "canary_deploy":
		route := ""
		newBackend := ""
		percentage := "10"
		if r, ok := args["route"]; ok {
			route = fmt.Sprintf("%v", r)
		}
		if nb, ok := args["new_backend"]; ok {
			newBackend = fmt.Sprintf("%v", nb)
		}
		if pct, ok := args["percentage"]; ok {
			percentage = fmt.Sprintf("%v", pct)
		}
		return []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(
						"Guide a canary deployment for the following service:\n\n"+
							"Route: %s\n"+
							"New Backend: %s\n"+
							"Initial Traffic Percentage: %s%%\n\n"+
							"Steps to perform:\n"+
							"1. Use olb_list_backends to check current backend status\n"+
							"2. Use olb_modify_backend to add the new backend\n"+
							"3. Use olb_modify_route to set up traffic splitting\n"+
							"4. Use olb_query_metrics to monitor error rates on the canary\n"+
							"5. Gradually increase traffic if metrics look healthy\n\n"+
							"Monitor for errors and roll back if issues are detected.",
						route, newBackend, percentage,
					),
				},
			},
		}

	default:
		return []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf("Execute prompt: %s", prompt.Name),
				},
			},
		}
	}
}

// --- Tool handlers ---

// handleQueryMetrics handles the olb_query_metrics tool.
func (s *Server) handleQueryMetrics(params map[string]any) (any, error) {
	metric, _ := params["metric"].(string)
	if metric == "" {
		return nil, fmt.Errorf("metric parameter is required")
	}

	if s.metrics == nil {
		return map[string]any{
			"metric":  metric,
			"message": "metrics provider not configured",
		}, nil
	}

	result := s.metrics.QueryMetrics(metric)
	return map[string]any{
		"metric":  metric,
		"results": result,
	}, nil
}

// handleListBackends handles the olb_list_backends tool.
func (s *Server) handleListBackends(params map[string]any) (any, error) {
	if s.backends == nil {
		return map[string]any{
			"message": "backend provider not configured",
			"pools":   []PoolInfo{},
		}, nil
	}

	pools := s.backends.ListPools()

	// Apply pool filter if specified
	if poolFilter, ok := params["pool"].(string); ok && poolFilter != "" {
		filtered := make([]PoolInfo, 0)
		for _, p := range pools {
			if p.Name == poolFilter {
				filtered = append(filtered, p)
			}
		}
		pools = filtered
	}

	// Apply status filter if specified
	if statusFilter, ok := params["status"].(string); ok && statusFilter != "" && statusFilter != "all" {
		for i := range pools {
			filtered := make([]BackendInfo, 0)
			for _, b := range pools[i].Backends {
				if strings.EqualFold(b.Status, statusFilter) {
					filtered = append(filtered, b)
				}
			}
			pools[i].Backends = filtered
		}
	}

	return map[string]any{
		"pools": pools,
	}, nil
}

// handleModifyBackend handles the olb_modify_backend tool.
func (s *Server) handleModifyBackend(params map[string]any) (any, error) {
	action, _ := params["action"].(string)
	pool, _ := params["pool"].(string)
	address, _ := params["address"].(string)

	if action == "" {
		return nil, fmt.Errorf("action parameter is required")
	}
	if pool == "" {
		return nil, fmt.Errorf("pool parameter is required")
	}
	if address == "" {
		return nil, fmt.Errorf("address parameter is required")
	}

	if s.backends == nil {
		return nil, fmt.Errorf("backend provider not configured")
	}

	err := s.backends.ModifyBackend(action, pool, address)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":  "ok",
		"action":  action,
		"pool":    pool,
		"address": address,
	}, nil
}

// handleModifyRoute handles the olb_modify_route tool.
func (s *Server) handleModifyRoute(params map[string]any) (any, error) {
	action, _ := params["action"].(string)
	host, _ := params["host"].(string)
	path, _ := params["path"].(string)
	backend, _ := params["backend"].(string)

	if action == "" {
		return nil, fmt.Errorf("action parameter is required")
	}

	if s.routes == nil {
		return nil, fmt.Errorf("route provider not configured")
	}

	err := s.routes.ModifyRoute(action, host, path, backend)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":  "ok",
		"action":  action,
		"host":    host,
		"path":    path,
		"backend": backend,
	}, nil
}

// handleDiagnose handles the olb_diagnose tool.
func (s *Server) handleDiagnose(params map[string]any) (any, error) {
	mode, _ := params["mode"].(string)
	if mode == "" {
		mode = "full"
	}

	diagnosis := map[string]any{
		"mode":      mode,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"findings":  []any{},
	}

	findings := make([]any, 0)

	switch mode {
	case "errors":
		if s.logs != nil {
			logs := s.logs.GetLogs(50, "error")
			findings = append(findings, map[string]any{
				"type":    "error_analysis",
				"count":   len(logs),
				"entries": logs,
			})
		}
	case "latency":
		if s.metrics != nil {
			latencyMetrics := s.metrics.QueryMetrics("latency")
			findings = append(findings, map[string]any{
				"type":    "latency_analysis",
				"metrics": latencyMetrics,
			})
		}
	case "capacity":
		if s.backends != nil {
			pools := s.backends.ListPools()
			for _, pool := range pools {
				healthy := 0
				total := len(pool.Backends)
				for _, b := range pool.Backends {
					if b.Status == "healthy" {
						healthy++
					}
				}
				findings = append(findings, map[string]any{
					"type":        "capacity_analysis",
					"pool":        pool.Name,
					"total":       total,
					"healthy":     healthy,
					"utilization": fmt.Sprintf("%.0f%%", float64(healthy)/float64(max(total, 1))*100),
				})
			}
		}
	case "health":
		if s.backends != nil {
			pools := s.backends.ListPools()
			for _, pool := range pools {
				for _, b := range pool.Backends {
					if b.Status != "healthy" {
						findings = append(findings, map[string]any{
							"type":    "unhealthy_backend",
							"pool":    pool.Name,
							"backend": b.Address,
							"status":  b.Status,
						})
					}
				}
			}
		}
	case "full":
		// Collect all diagnostics
		if s.metrics != nil {
			findings = append(findings, map[string]any{
				"type":    "metrics_snapshot",
				"metrics": s.metrics.QueryMetrics("*"),
			})
		}
		if s.backends != nil {
			findings = append(findings, map[string]any{
				"type":  "backend_status",
				"pools": s.backends.ListPools(),
			})
		}
		if s.logs != nil {
			findings = append(findings, map[string]any{
				"type": "recent_errors",
				"logs": s.logs.GetLogs(20, "error"),
			})
		}
	}

	diagnosis["findings"] = findings
	return diagnosis, nil
}

// handleGetLogs handles the olb_get_logs tool.
func (s *Server) handleGetLogs(params map[string]any) (any, error) {
	count := 50
	if c, ok := params["count"].(float64); ok {
		if c > 0 && c == float64(int(c)) && c <= 1000 {
			count = int(c)
		}
	}
	level, _ := params["level"].(string)

	if s.logs == nil {
		return map[string]any{
			"message": "log provider not configured",
			"entries": []LogEntry{},
		}, nil
	}

	entries := s.logs.GetLogs(count, level)

	// Apply filter if specified
	if filter, ok := params["filter"].(string); ok && filter != "" {
		filtered := make([]LogEntry, 0)
		for _, e := range entries {
			if strings.Contains(e.Message, filter) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	return map[string]any{
		"count":   len(entries),
		"entries": entries,
	}, nil
}

// handleGetConfig handles the olb_get_config tool.
func (s *Server) handleGetConfig(_ map[string]any) (any, error) {
	if s.config == nil {
		return map[string]any{
			"message": "config provider not configured",
		}, nil
	}

	return s.config.GetConfig(), nil
}

// handleClusterStatus handles the olb_cluster_status tool.
func (s *Server) handleClusterStatus(_ map[string]any) (any, error) {
	if s.cluster == nil {
		return map[string]any{
			"mode":    "standalone",
			"message": "cluster not configured",
		}, nil
	}

	return s.cluster.GetStatus(), nil
}

// --- Transports ---

// StdioTransport implements MCP transport over stdin/stdout using line-delimited JSON.
type StdioTransport struct {
	server *Server
	reader io.Reader
	writer io.Writer
}

// NewStdioTransport creates a new stdio transport.
func NewStdioTransport(server *Server, reader io.Reader, writer io.Writer) *StdioTransport {
	return &StdioTransport{
		server: server,
		reader: reader,
		writer: writer,
	}
}

// Run starts the stdio transport, reading requests from reader and writing responses to writer.
// It blocks until the reader is exhausted or the context is cancelled.
func (t *StdioTransport) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(t.reader)
	// Allow large messages (up to 1MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		resp, err := t.server.HandleJSONRPC(line)
		if err != nil {
			return fmt.Errorf("handling JSON-RPC: %w", err)
		}

		// Write response followed by newline
		if _, err := t.writer.Write(resp); err != nil {
			return fmt.Errorf("writing response: %w", err)
		}
		if _, err := t.writer.Write([]byte("\n")); err != nil {
			return fmt.Errorf("writing newline: %w", err)
		}
	}

	return scanner.Err()
}

// HTTPTransport implements MCP transport over HTTP.
type HTTPTransport struct {
	server      *Server
	addr        string
	listener    net.Listener
	httpSrv     *http.Server
	bearerToken string
}

// NewHTTPTransport creates a new HTTP transport.
// The token must be non-empty; otherwise, the MCP endpoint would be unauthenticated.
func NewHTTPTransport(server *Server, addr string, token string) (*HTTPTransport, error) {
	if token == "" {
		return nil, fmt.Errorf("MCP HTTP transport requires a non-empty bearer token")
	}
	return &HTTPTransport{
		server:      server,
		addr:        addr,
		bearerToken: token,
	}, nil
}

// ServeHTTP implements http.Handler for the MCP HTTP transport.
func (t *HTTPTransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate
	token := ""
	if t.bearerToken != "" {
		auth := r.Header.Get("Authorization")
		if len(auth) < 7 || auth[:7] != "Bearer " {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token = auth[7:]
		if subtle.ConstantTimeCompare([]byte(token), []byte(t.bearerToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Limit request body to 1MB
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	resp, err := t.server.HandleJSONRPCWithToken(body, token)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

// Start starts the HTTP transport and listens for requests.
func (t *HTTPTransport) Start() error {
	mux := http.NewServeMux()
	mux.Handle("/mcp", t)

	t.httpSrv = &http.Server{
		Addr:    t.addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", t.addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", t.addr, err)
	}
	t.listener = ln

	go t.httpSrv.Serve(ln)
	return nil
}

// Stop gracefully stops the HTTP transport.
func (t *HTTPTransport) Stop(ctx context.Context) error {
	if t.httpSrv == nil {
		return nil
	}
	return t.httpSrv.Shutdown(ctx)
}

// Addr returns the listener address, useful when using port 0 for testing.
func (t *HTTPTransport) Addr() string {
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.addr
}
