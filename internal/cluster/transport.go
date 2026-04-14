package cluster

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"
	"time"
)

// --------------------------------------------------------------------------
// TCP transport
// --------------------------------------------------------------------------

// Message types for the binary framing protocol.
const (
	msgRequestVote      byte = 1
	msgRequestVoteResp  byte = 2
	msgAppendEntries    byte = 3
	msgAppendEntriesRes byte = 4
	msgInstallSnapshot  byte = 5
	msgInstallSnapResp  byte = 6
)

const maxPayload = 16 << 20        // 16 MiB general RPC limit
const maxSnapshotPayload = 4 << 20 // 4 MiB InstallSnapshot limit

// TCPTransport implements Raft RPC over TCP with a connection pool.
type TCPTransport struct {
	bindAddr string
	listener net.Listener
	stopCh   chan struct{}

	// Connection pool: target address -> pooled connections.
	poolMu sync.Mutex
	pool   map[string][]net.Conn

	// Maximum connections to keep per peer.
	maxPoolSize int

	// Timeout for individual RPC calls.
	timeout time.Duration

	// Handler for incoming RPCs.
	handler RPCHandler
}

// RPCHandler processes incoming Raft RPCs.
type RPCHandler interface {
	HandleRequestVote(req *RequestVote) *RequestVoteResponse
	HandleAppendEntries(req *AppendEntries) *AppendEntriesResponse
	HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse
}

// TCPTransportConfig configures the TCP transport.
type TCPTransportConfig struct {
	BindAddr    string        `json:"bind_addr"`
	MaxPoolSize int           `json:"max_pool_size"`
	Timeout     time.Duration `json:"timeout"`
}

// DefaultTCPTransportConfig returns sensible defaults.
func DefaultTCPTransportConfig() *TCPTransportConfig {
	return &TCPTransportConfig{
		BindAddr:    "0.0.0.0:7947",
		MaxPoolSize: 5,
		Timeout:     5 * time.Second,
	}
}

// NewTCPTransport creates a new TCP transport.
func NewTCPTransport(config *TCPTransportConfig, handler RPCHandler) (*TCPTransport, error) {
	if config == nil {
		config = DefaultTCPTransportConfig()
	}
	if config.MaxPoolSize <= 0 {
		config.MaxPoolSize = 5
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}

	return &TCPTransport{
		bindAddr:    config.BindAddr,
		stopCh:      make(chan struct{}),
		pool:        make(map[string][]net.Conn),
		maxPoolSize: config.MaxPoolSize,
		timeout:     config.Timeout,
		handler:     handler,
	}, nil
}

// Start begins listening for incoming RPCs.
func (t *TCPTransport) Start() error {
	ln, err := net.Listen("tcp", t.bindAddr)
	if err != nil {
		return fmt.Errorf("tcp listen: %w", err)
	}
	t.listener = ln

	go t.acceptLoop()
	return nil
}

// Stop shuts down the transport and closes all pooled connections.
func (t *TCPTransport) Stop() error {
	close(t.stopCh)

	if t.listener != nil {
		t.listener.Close()
	}

	t.poolMu.Lock()
	defer t.poolMu.Unlock()

	for addr, conns := range t.pool {
		for _, conn := range conns {
			_ = conn.Close() // best-effort cleanup
		}
		delete(t.pool, addr)
	}

	return nil
}

// Addr returns the listener's address (useful when bound to :0).
func (t *TCPTransport) Addr() string {
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.bindAddr
}

// Listener returns the underlying net.Listener.
// This allows wrapping the listener with middleware (e.g., authentication).
func (t *TCPTransport) Listener() net.Listener {
	return t.listener
}

// SetListener replaces the transport's listener with a wrapped version.
// Must be called before Start(). The caller is responsible for ensuring
// the wrapped listener is properly closed via the transport's Stop() method.
func (t *TCPTransport) SetListener(ln net.Listener) {
	t.listener = ln
}

// acceptLoop accepts incoming connections until stopped.
func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				continue
			}
		}
		go t.handleConn(conn)
	}
}

// handleConn reads a single RPC from the connection, dispatches it to the
// handler, and writes the response.
func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()

	// We handle one RPC per connection for simplicity. The connection may
	// be pooled on the sender side for reuse, but each RPC is a separate
	// exchange (request + response).
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		msgType, payload, err := readFrame(conn)
		if err != nil {
			return
		}

		var respType byte
		var respPayload []byte

		switch msgType {
		case msgRequestVote:
			var req RequestVote
			if err := json.Unmarshal(payload, &req); err != nil {
				return
			}
			resp := t.handler.HandleRequestVote(&req)
			respPayload, err = json.Marshal(resp)
			if err != nil {
				log.Printf("raft: failed to marshal RequestVote response: %v", err)
				return
			}
			respType = msgRequestVoteResp

		case msgAppendEntries:
			var req AppendEntries
			if err := json.Unmarshal(payload, &req); err != nil {
				return
			}
			resp := t.handler.HandleAppendEntries(&req)
			respPayload, err = json.Marshal(resp)
			if err != nil {
				log.Printf("raft: failed to marshal AppendEntries response: %v", err)
				return
			}
			respType = msgAppendEntriesRes

		case msgInstallSnapshot:
			if len(payload) > maxSnapshotPayload {
				writeFrame(conn, msgInstallSnapResp, []byte(fmt.Sprintf("snapshot payload too large: %d bytes (max %d)", len(payload), maxSnapshotPayload)))
				return
			}
			var req InstallSnapshotRequest
			if err := json.Unmarshal(payload, &req); err != nil {
				return
			}
			resp := t.handler.HandleInstallSnapshot(&req)
			respPayload, err = json.Marshal(resp)
			if err != nil {
				log.Printf("raft: failed to marshal InstallSnapshot response: %v", err)
				return
			}
			respType = msgInstallSnapResp

		default:
			return
		}

		conn.SetWriteDeadline(time.Now().Add(t.timeout))
		if err := writeFrame(conn, respType, respPayload); err != nil {
			return
		}
	}
}

// --------------------------------------------------------------------------
// Send RPCs (client side)
// --------------------------------------------------------------------------

// SendRequestVote sends a RequestVote RPC to the target address and returns
// the response.
func (t *TCPTransport) SendRequestVote(target string, req *RequestVote) (*RequestVoteResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respPayload, err := t.sendRPC(target, msgRequestVote, payload)
	if err != nil {
		return nil, err
	}

	var resp RequestVoteResponse
	if err := json.Unmarshal(respPayload, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SendAppendEntries sends an AppendEntries RPC to the target address.
func (t *TCPTransport) SendAppendEntries(target string, req *AppendEntries) (*AppendEntriesResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respPayload, err := t.sendRPC(target, msgAppendEntries, payload)
	if err != nil {
		return nil, err
	}

	var resp AppendEntriesResponse
	if err := json.Unmarshal(respPayload, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// SendInstallSnapshot sends an InstallSnapshot RPC to the target address.
func (t *TCPTransport) SendInstallSnapshot(target string, req *InstallSnapshotRequest) (*InstallSnapshotResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respPayload, err := t.sendRPC(target, msgInstallSnapshot, payload)
	if err != nil {
		return nil, err
	}

	var resp InstallSnapshotResponse
	if err := json.Unmarshal(respPayload, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// sendRPC performs a single RPC exchange: write frame, read frame.
func (t *TCPTransport) sendRPC(target string, msgType byte, payload []byte) ([]byte, error) {
	conn, err := t.getConn(target)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", target, err)
	}

	conn.SetWriteDeadline(time.Now().Add(t.timeout))
	if err := writeFrame(conn, msgType, payload); err != nil {
		_ = conn.Close() // best-effort cleanup
		return nil, fmt.Errorf("write frame: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(t.timeout))
	_, respPayload, err := readFrame(conn)
	if err != nil {
		_ = conn.Close() // best-effort cleanup
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Return connection to pool.
	t.returnConn(target, conn)

	return respPayload, nil
}

// --------------------------------------------------------------------------
// Connection pool
// --------------------------------------------------------------------------

// getConn retrieves a pooled connection or dials a new one.
func (t *TCPTransport) getConn(target string) (net.Conn, error) {
	t.poolMu.Lock()
	conns := t.pool[target]
	if len(conns) > 0 {
		conn := conns[len(conns)-1]
		t.pool[target] = conns[:len(conns)-1]
		t.poolMu.Unlock()
		return conn, nil
	}
	t.poolMu.Unlock()

	return net.DialTimeout("tcp", target, t.timeout)
}

// returnConn returns a connection to the pool. If the pool is full the
// connection is closed.
func (t *TCPTransport) returnConn(target string, conn net.Conn) {
	t.poolMu.Lock()
	defer t.poolMu.Unlock()

	conns := t.pool[target]
	if len(conns) >= t.maxPoolSize {
		_ = conn.Close() // best-effort cleanup
		return
	}
	t.pool[target] = append(conns, conn)
}

// PoolSize returns the number of pooled connections for a target.
func (t *TCPTransport) PoolSize(target string) int {
	t.poolMu.Lock()
	defer t.poolMu.Unlock()
	return len(t.pool[target])
}

// --------------------------------------------------------------------------
// Binary framing: [msgType(1)][length(4)][payload(N)]
// --------------------------------------------------------------------------

// writeFrame writes a framed message to the writer.
func writeFrame(w io.Writer, msgType byte, payload []byte) error {
	if len(payload) > math.MaxUint32 {
		return fmt.Errorf("payload too large for framing: %d bytes (max %d)", len(payload), math.MaxUint32)
	}
	header := make([]byte, 5)
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:5], uint32(len(payload)))

	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// readFrame reads a framed message from the reader.
func readFrame(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}

	msgType := header[0]
	length := binary.BigEndian.Uint32(header[1:5])

	if length == 0 {
		return msgType, nil, nil
	}

	// Guard against large payloads.
	if length > maxPayload {
		return 0, nil, fmt.Errorf("payload too large: %d bytes", length)
	}
	// Enforce lower limit for InstallSnapshot messages.
	if msgType == msgInstallSnapshot && length > maxSnapshotPayload {
		return 0, nil, fmt.Errorf("snapshot payload too large: %d bytes (max %d)", length, maxSnapshotPayload)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}

	return msgType, payload, nil
}
