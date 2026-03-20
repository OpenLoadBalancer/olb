package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// SSETransportConfig configures the SSE MCP transport.
type SSETransportConfig struct {
	// Addr is the listen address (e.g. ":8082").
	Addr string

	// BearerToken is the required auth token. If empty, auth is disabled.
	BearerToken string

	// AuditLog enables logging of all MCP tool calls.
	AuditLog bool

	// AuditFunc is called for each tool invocation when AuditLog is true.
	// Arguments: tool name, parameters JSON, client address, duration, error.
	AuditFunc func(tool string, params string, clientAddr string, duration time.Duration, err error)
}

// SSETransport implements the MCP Streamable HTTP transport.
//
// Protocol (per MCP specification):
//   - GET  /sse           → Server-Sent Events stream for server→client messages
//   - POST /message       → Client→server JSON-RPC messages
//   - POST /mcp           → Legacy HTTP POST (backwards compatible)
//
// Authentication: Bearer token in Authorization header (configurable).
// Audit: All tool calls are logged when AuditLog is enabled.
type SSETransport struct {
	server *Server
	config SSETransportConfig

	listener net.Listener
	httpSrv  *http.Server

	// SSE client management
	mu      sync.RWMutex
	clients map[uint64]*sseClient
	nextID  atomic.Uint64

	// Shutdown
	done chan struct{}
}

// sseClient represents a connected SSE client.
type sseClient struct {
	id       uint64
	addr     string
	messages chan []byte
	done     chan struct{}
}

// NewSSETransport creates a new SSE-capable MCP transport.
func NewSSETransport(server *Server, config SSETransportConfig) *SSETransport {
	return &SSETransport{
		server:  server,
		config:  config,
		clients: make(map[uint64]*sseClient),
		done:    make(chan struct{}),
	}
}

// Start starts the SSE transport.
func (t *SSETransport) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", t.handleSSE)
	mux.HandleFunc("/message", t.handleMessage)
	mux.HandleFunc("/mcp", t.handleLegacy) // backwards compat

	t.httpSrv = &http.Server{
		Addr:         t.config.Addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE needs unlimited write timeout
		IdleTimeout:  120 * time.Second,
	}

	ln, err := net.Listen("tcp", t.config.Addr)
	if err != nil {
		return fmt.Errorf("mcp sse: listen %s: %w", t.config.Addr, err)
	}
	t.listener = ln

	go t.httpSrv.Serve(ln)
	return nil
}

// Stop gracefully stops the transport.
func (t *SSETransport) Stop(ctx context.Context) error {
	close(t.done)

	// Close all SSE clients
	t.mu.Lock()
	for _, c := range t.clients {
		close(c.done)
	}
	t.clients = make(map[uint64]*sseClient)
	t.mu.Unlock()

	if t.httpSrv != nil {
		return t.httpSrv.Shutdown(ctx)
	}
	return nil
}

// Addr returns the actual listen address.
func (t *SSETransport) Addr() string {
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.config.Addr
}

// --- Authentication ---

func (t *SSETransport) authenticate(r *http.Request) bool {
	if t.config.BearerToken == "" {
		return true // auth disabled
	}
	auth := r.Header.Get("Authorization")
	if len(auth) < 7 || auth[:7] != "Bearer " {
		return false
	}
	token := auth[7:]
	return subtle.ConstantTimeCompare([]byte(token), []byte(t.config.BearerToken)) == 1
}

func (t *SSETransport) authError(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// --- SSE Handler (GET /sse) ---

func (t *SSETransport) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !t.authenticate(r) {
		t.authError(w)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Register SSE client
	clientID := t.nextID.Add(1)
	client := &sseClient{
		id:       clientID,
		addr:     r.RemoteAddr,
		messages: make(chan []byte, 64),
		done:     make(chan struct{}),
	}

	t.mu.Lock()
	t.clients[clientID] = client
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.clients, clientID)
		t.mu.Unlock()
	}()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// Send endpoint event — tells client where to POST messages
	fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%d\n\n", clientID)
	flusher.Flush()

	// Keep-alive ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-client.messages:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()

		case <-client.done:
			return

		case <-t.done:
			return

		case <-r.Context().Done():
			return
		}
	}
}

// --- Message Handler (POST /message) ---

func (t *SSETransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !t.authenticate(r) {
		t.authError(w)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Audit logging
	start := time.Now()
	var toolName, paramsJSON string
	if t.config.AuditLog && t.config.AuditFunc != nil {
		toolName, paramsJSON = extractToolInfo(body)
	}

	// Process JSON-RPC
	resp, err := t.server.HandleJSONRPC(body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Audit log
	if t.config.AuditLog && t.config.AuditFunc != nil && toolName != "" {
		t.config.AuditFunc(toolName, paramsJSON, r.RemoteAddr, time.Since(start), nil)
	}

	// Send response directly via HTTP
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)

	// Also push to SSE stream if sessionId provided
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID != "" {
		t.broadcastToClient(sessionID, resp)
	}
}

// --- Legacy Handler (POST /mcp) — backwards compatible ---

func (t *SSETransport) handleLegacy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !t.authenticate(r) {
		t.authError(w)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Audit logging
	start := time.Now()
	var toolName, paramsJSON string
	if t.config.AuditLog && t.config.AuditFunc != nil {
		toolName, paramsJSON = extractToolInfo(body)
	}

	resp, err := t.server.HandleJSONRPC(body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if t.config.AuditLog && t.config.AuditFunc != nil && toolName != "" {
		t.config.AuditFunc(toolName, paramsJSON, r.RemoteAddr, time.Since(start), nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// --- Helpers ---

func (t *SSETransport) broadcastToClient(sessionID string, data []byte) {
	// Parse sessionID to uint64
	var id uint64
	fmt.Sscanf(sessionID, "%d", &id)

	t.mu.RLock()
	client, exists := t.clients[id]
	t.mu.RUnlock()

	if exists {
		select {
		case client.messages <- data:
		default:
			// Client buffer full — drop message to prevent blocking
			log.Printf("mcp: SSE client %d buffer full, dropping message", id)
		}
	}
}

// extractToolInfo extracts tool name and params from a JSON-RPC request.
func extractToolInfo(body []byte) (string, string) {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(body, &req) != nil {
		return "", ""
	}
	if req.Method != "tools/call" {
		return "", ""
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
		return "", ""
	}
	return params.Name, string(params.Arguments)
}

// ClientCount returns the number of connected SSE clients.
func (t *SSETransport) ClientCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.clients)
}
