// Package cluster provides distributed clustering and consensus using Raft.
// This file implements Raft snapshots, TCP transport, and membership changes.
package cluster

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// --------------------------------------------------------------------------
// Snapshot types
// --------------------------------------------------------------------------

// Snapshot represents a point-in-time snapshot of the state machine.
type Snapshot struct {
	LastIncludedIndex uint64            `json:"last_included_index"`
	LastIncludedTerm  uint64            `json:"last_included_term"`
	Data              []byte            `json:"data"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// SnapshotMeta contains metadata about a stored snapshot (without the data).
type SnapshotMeta struct {
	LastIncludedIndex uint64            `json:"last_included_index"`
	LastIncludedTerm  uint64            `json:"last_included_term"`
	Size              int64             `json:"size"`
	Timestamp         time.Time         `json:"timestamp"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// SnapshotStore defines the interface for persisting and loading snapshots.
type SnapshotStore interface {
	// Save persists a snapshot.
	Save(snapshot *Snapshot) error
	// Load returns the most recent snapshot.
	Load() (*Snapshot, error)
	// List returns metadata for all available snapshots, newest first.
	List() ([]*SnapshotMeta, error)
}

// --------------------------------------------------------------------------
// MemorySnapshotStore – in-memory (testing)
// --------------------------------------------------------------------------

// MemorySnapshotStore keeps snapshots in memory. It is primarily intended for
// unit tests. Access is safe for concurrent use.
type MemorySnapshotStore struct {
	mu        sync.RWMutex
	snapshots []*snapshotEntry
}

type snapshotEntry struct {
	snapshot  *Snapshot
	timestamp time.Time
}

// NewMemorySnapshotStore creates a new in-memory snapshot store.
func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{
		snapshots: make([]*snapshotEntry, 0),
	}
}

// Save stores the snapshot in memory.
func (m *MemorySnapshotStore) Save(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep-copy the data so the caller can mutate its buffer safely.
	dataCopy := make([]byte, len(snapshot.Data))
	copy(dataCopy, snapshot.Data)

	metaCopy := make(map[string]string, len(snapshot.Metadata))
	for k, v := range snapshot.Metadata {
		metaCopy[k] = v
	}

	m.snapshots = append(m.snapshots, &snapshotEntry{
		snapshot: &Snapshot{
			LastIncludedIndex: snapshot.LastIncludedIndex,
			LastIncludedTerm:  snapshot.LastIncludedTerm,
			Data:              dataCopy,
			Metadata:          metaCopy,
		},
		timestamp: time.Now(),
	})

	return nil
}

// Load returns the most recent snapshot or an error if none exists.
func (m *MemorySnapshotStore) Load() (*Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.snapshots) == 0 {
		return nil, errors.New("no snapshots available")
	}

	entry := m.snapshots[len(m.snapshots)-1]
	return entry.snapshot, nil
}

// List returns metadata for all stored snapshots, newest first.
func (m *MemorySnapshotStore) List() ([]*SnapshotMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metas := make([]*SnapshotMeta, len(m.snapshots))
	for i, entry := range m.snapshots {
		metas[len(m.snapshots)-1-i] = &SnapshotMeta{
			LastIncludedIndex: entry.snapshot.LastIncludedIndex,
			LastIncludedTerm:  entry.snapshot.LastIncludedTerm,
			Size:              int64(len(entry.snapshot.Data)),
			Timestamp:         entry.timestamp,
			Metadata:          entry.snapshot.Metadata,
		}
	}

	return metas, nil
}

// --------------------------------------------------------------------------
// FileSnapshotStore – disk-based
// --------------------------------------------------------------------------

// FileSnapshotStore persists snapshots to a directory on disk. Each snapshot
// is stored as a JSON file named by its index. Only the most recent retain
// snapshots are kept.
type FileSnapshotStore struct {
	dir    string
	retain int
	mu     sync.Mutex
}

// NewFileSnapshotStore creates a new disk-backed snapshot store rooted at dir.
// retain controls how many snapshots to keep (minimum 1).
func NewFileSnapshotStore(dir string, retain int) (*FileSnapshotStore, error) {
	if retain < 1 {
		retain = 1
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}

	return &FileSnapshotStore{
		dir:    dir,
		retain: retain,
	}, nil
}

func (f *FileSnapshotStore) snapshotPath(index uint64) string {
	return filepath.Join(f.dir, fmt.Sprintf("snapshot-%020d.json", index))
}

func (f *FileSnapshotStore) metaPath(index uint64) string {
	return filepath.Join(f.dir, fmt.Sprintf("snapshot-%020d.meta", index))
}

// Save writes the snapshot to disk and trims old snapshots.
func (f *FileSnapshotStore) Save(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Write snapshot data.
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	path := f.snapshotPath(snapshot.LastIncludedIndex)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	// Write metadata.
	meta := &SnapshotMeta{
		LastIncludedIndex: snapshot.LastIncludedIndex,
		LastIncludedTerm:  snapshot.LastIncludedTerm,
		Size:              int64(len(snapshot.Data)),
		Timestamp:         time.Now(),
		Metadata:          snapshot.Metadata,
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	metaPath := f.metaPath(snapshot.LastIncludedIndex)
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	// Trim old snapshots.
	return f.trimSnapshots()
}

// Load returns the most recent snapshot from disk.
func (f *FileSnapshotStore) Load() (*Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	files, err := f.listSnapshotFiles()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errors.New("no snapshots available")
	}

	// files are sorted ascending; pick the last (highest index).
	path := files[len(files)-1]
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// List returns metadata for all snapshots on disk, newest first.
func (f *FileSnapshotStore) List() ([]*SnapshotMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	files, err := f.listMetaFiles()
	if err != nil {
		return nil, err
	}

	metas := make([]*SnapshotMeta, 0, len(files))
	for i := len(files) - 1; i >= 0; i-- {
		data, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		var meta SnapshotMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		metas = append(metas, &meta)
	}

	return metas, nil
}

// listSnapshotFiles returns snapshot file paths sorted by name (ascending
// index).
func (f *FileSnapshotStore) listSnapshotFiles() ([]string, error) {
	pattern := filepath.Join(f.dir, "snapshot-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// listMetaFiles returns meta file paths sorted by name (ascending index).
func (f *FileSnapshotStore) listMetaFiles() ([]string, error) {
	pattern := filepath.Join(f.dir, "snapshot-*.meta")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// trimSnapshots removes old snapshots, keeping only the latest retain count.
func (f *FileSnapshotStore) trimSnapshots() error {
	snapFiles, err := f.listSnapshotFiles()
	if err != nil {
		return err
	}
	metaFiles, err := f.listMetaFiles()
	if err != nil {
		return err
	}

	// Remove excess snapshot files.
	for len(snapFiles) > f.retain {
		if err := os.Remove(snapFiles[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		snapFiles = snapFiles[1:]
	}

	// Remove excess meta files.
	for len(metaFiles) > f.retain {
		if err := os.Remove(metaFiles[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		metaFiles = metaFiles[1:]
	}

	return nil
}

// --------------------------------------------------------------------------
// Cluster snapshot / restore / log compaction
// --------------------------------------------------------------------------

// CreateSnapshot triggers a snapshot of the state machine and stores it. The
// log is then compacted up to the snapshot index.
func (c *Cluster) CreateSnapshot() (*Snapshot, error) {
	if c.stateMachine == nil {
		return nil, errors.New("no state machine configured")
	}

	data, err := c.stateMachine.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("state machine snapshot: %w", err)
	}

	c.logMu.RLock()
	lastIndex := c.getLastLogIndexLocked()
	lastTerm := c.getLastLogTermLocked()
	c.logMu.RUnlock()

	snapshot := &Snapshot{
		LastIncludedIndex: lastIndex,
		LastIncludedTerm:  lastTerm,
		Data:              data,
		Metadata: map[string]string{
			"node_id":    c.config.NodeID,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		},
	}

	// Compact the log.
	c.compactLog(lastIndex)

	return snapshot, nil
}

// RestoreSnapshot restores the state machine from a snapshot and resets the
// Raft log.
func (c *Cluster) RestoreSnapshot(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}
	if c.stateMachine == nil {
		return errors.New("no state machine configured")
	}

	if err := c.stateMachine.Restore(snapshot.Data); err != nil {
		return fmt.Errorf("restore state machine: %w", err)
	}

	// Reset log.
	c.logMu.Lock()
	c.log = make([]*LogEntry, 0)
	c.logMu.Unlock()

	// Update indices.
	c.commitIndex.Store(snapshot.LastIncludedIndex)
	c.lastApplied.Store(snapshot.LastIncludedIndex)

	// Update term if the snapshot is from a later term.
	if snapshot.LastIncludedTerm > c.GetTerm() {
		c.currentTerm.Store(snapshot.LastIncludedTerm)
	}

	return nil
}

// compactLog removes all log entries with index <= compactIndex.
func (c *Cluster) compactLog(compactIndex uint64) {
	c.logMu.Lock()
	defer c.logMu.Unlock()

	newLog := make([]*LogEntry, 0)
	for _, entry := range c.log {
		if entry.Index > compactIndex {
			newLog = append(newLog, entry)
		}
	}
	c.log = newLog
}

// getLastLogIndexLocked returns the last log index. Caller must hold logMu.
func (c *Cluster) getLastLogIndexLocked() uint64 {
	if len(c.log) == 0 {
		return 0
	}
	return c.log[len(c.log)-1].Index
}

// getLastLogTermLocked returns the term of the last log entry. Caller must
// hold logMu.
func (c *Cluster) getLastLogTermLocked() uint64 {
	if len(c.log) == 0 {
		return 0
	}
	return c.log[len(c.log)-1].Term
}

// --------------------------------------------------------------------------
// InstallSnapshot RPC
// --------------------------------------------------------------------------

// InstallSnapshotRequest is the RPC sent by a leader to a lagging follower.
type InstallSnapshotRequest struct {
	Term              uint64 `json:"term"`
	LeaderID          string `json:"leader_id"`
	LastIncludedIndex uint64 `json:"last_included_index"`
	LastIncludedTerm  uint64 `json:"last_included_term"`
	Data              []byte `json:"data"`
}

// InstallSnapshotResponse is the follower's reply.
type InstallSnapshotResponse struct {
	Term    uint64 `json:"term"`
	Success bool   `json:"success"`
}

// HandleInstallSnapshot processes an InstallSnapshot RPC from the leader.
func (c *Cluster) HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse {
	if req.Term < c.GetTerm() {
		return &InstallSnapshotResponse{
			Term:    c.GetTerm(),
			Success: false,
		}
	}

	// Step down if we receive a higher term.
	if req.Term > c.GetTerm() {
		c.currentTerm.Store(req.Term)
		c.setState(StateFollower)
		c.votedFor.Store("")
	}

	c.leaderID.Store(req.LeaderID)
	c.resetElectionTimer()

	// Apply the snapshot.
	snapshot := &Snapshot{
		LastIncludedIndex: req.LastIncludedIndex,
		LastIncludedTerm:  req.LastIncludedTerm,
		Data:              req.Data,
	}

	if err := c.RestoreSnapshot(snapshot); err != nil {
		return &InstallSnapshotResponse{
			Term:    c.GetTerm(),
			Success: false,
		}
	}

	return &InstallSnapshotResponse{
		Term:    c.GetTerm(),
		Success: true,
	}
}

// ShouldSendSnapshot determines whether the leader should send a snapshot
// instead of log entries. This returns true when the follower's nextIndex is
// behind our earliest available log entry (i.e., it was compacted away).
func (c *Cluster) ShouldSendSnapshot(followerNextIndex uint64) bool {
	c.logMu.RLock()
	defer c.logMu.RUnlock()

	if len(c.log) == 0 {
		return false
	}

	earliestIndex := c.log[0].Index
	return followerNextIndex < earliestIndex
}

// BuildInstallSnapshotRequest builds an InstallSnapshotRequest from the
// current state. It is used by the leader when a follower is too far behind.
func (c *Cluster) BuildInstallSnapshotRequest() (*InstallSnapshotRequest, error) {
	snapshot, err := c.CreateSnapshot()
	if err != nil {
		return nil, err
	}

	return &InstallSnapshotRequest{
		Term:              c.GetTerm(),
		LeaderID:          c.config.NodeID,
		LastIncludedIndex: snapshot.LastIncludedIndex,
		LastIncludedTerm:  snapshot.LastIncludedTerm,
		Data:              snapshot.Data,
	}, nil
}

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

	// Guard against absurdly large payloads (256 MiB).
	const maxPayload = 256 << 20
	if length > maxPayload {
		return 0, nil, fmt.Errorf("payload too large: %d bytes", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}

	return msgType, payload, nil
}

// --------------------------------------------------------------------------
// Membership changes (joint consensus)
// --------------------------------------------------------------------------

// ChangeType describes the kind of membership change.
type ChangeType int

const (
	// AddNode adds a new node to the cluster.
	AddNode ChangeType = iota
	// RemoveNode removes a node from the cluster.
	RemoveNode
)

// String returns a human-readable representation.
func (ct ChangeType) String() string {
	switch ct {
	case AddNode:
		return "AddNode"
	case RemoveNode:
		return "RemoveNode"
	default:
		return "Unknown"
	}
}

// MembershipChange describes a proposed membership change.
type MembershipChange struct {
	Type    ChangeType `json:"type"`
	NodeID  string     `json:"node_id"`
	Address string     `json:"address"`
}

// MembershipChangeEntry is a log entry that encodes a membership change. It
// is serialised as the Command field of a LogEntry.
type MembershipChangeEntry struct {
	Phase  string           `json:"phase"` // "joint" or "final"
	Change MembershipChange `json:"change"`
}

// membershipConfig tracks the membership state during joint consensus.
type membershipConfig struct {
	mu            sync.RWMutex
	pending       *MembershipChange
	inTransition  bool
	jointCommitID uint64 // log index of the C_old,new entry
}

// ProposeMembershipChange proposes adding or removing a node. The change goes
// through two phases (joint consensus):
//
//  1. C_old,new — the joint configuration is written to the log.
//  2. C_new     — once committed, the final configuration is written.
//
// Only one membership change may be in progress at a time.
func (c *Cluster) ProposeMembershipChange(change MembershipChange) error {
	if c.GetState() != StateLeader {
		return fmt.Errorf("not leader, current leader is %s", c.GetLeader())
	}

	c.memberMu.Lock()
	if c.membership.inTransition {
		c.memberMu.Unlock()
		return errors.New("membership change already in progress")
	}
	c.membership.inTransition = true
	c.membership.pending = &change
	c.memberMu.Unlock()

	// Phase 1: propose the joint configuration.
	entry := MembershipChangeEntry{
		Phase:  "joint",
		Change: change,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("marshal membership change: %w", err)
	}

	result, err := c.Propose(data)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("propose joint config: %w", err)
	}
	if result.Error != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("apply joint config: %w", result.Error)
	}

	c.memberMu.Lock()
	c.membership.jointCommitID = result.Index
	c.memberMu.Unlock()

	// Apply the membership change immediately (Phase 1 effect).
	c.applyMembershipChange(&change)

	// Phase 2: commit the final configuration.
	finalEntry := MembershipChangeEntry{
		Phase:  "final",
		Change: change,
	}

	finalData, err := json.Marshal(finalEntry)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("marshal final config: %w", err)
	}

	finalResult, err := c.Propose(finalData)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("propose final config: %w", err)
	}
	if finalResult.Error != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("apply final config: %w", finalResult.Error)
	}

	c.clearMembershipTransition()
	return nil
}

// applyMembershipChange actually adds or removes the node.
func (c *Cluster) applyMembershipChange(change *MembershipChange) {
	switch change.Type {
	case AddNode:
		c.AddNode(change.NodeID, change.Address)
	case RemoveNode:
		c.RemoveNode(change.NodeID)
	}
}

// clearMembershipTransition resets the in-progress flag.
func (c *Cluster) clearMembershipTransition() {
	c.memberMu.Lock()
	c.membership.inTransition = false
	c.membership.pending = nil
	c.membership.jointCommitID = 0
	c.memberMu.Unlock()
}

// IsMembershipChangeInProgress reports whether a membership change is pending.
func (c *Cluster) IsMembershipChangeInProgress() bool {
	c.memberMu.RLock()
	defer c.memberMu.RUnlock()
	return c.membership.inTransition
}
