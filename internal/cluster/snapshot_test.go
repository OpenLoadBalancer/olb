package cluster

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// Helper: enriched mock state machine with key-value store
// --------------------------------------------------------------------------

type kvStateMachine struct {
	mu   sync.Mutex
	data map[string]string
}

func newKVStateMachine() *kvStateMachine {
	return &kvStateMachine{data: make(map[string]string)}
}

func (kv *kvStateMachine) Apply(command []byte) ([]byte, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	parts := split(string(command), '=')
	if len(parts) == 2 {
		kv.data[parts[0]] = parts[1]
	}
	return []byte("ok"), nil
}

func (kv *kvStateMachine) Snapshot() ([]byte, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	return json.Marshal(kv.data)
}

func (kv *kvStateMachine) Restore(snapshot []byte) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.data = make(map[string]string)
	return json.Unmarshal(snapshot, &kv.data)
}

func (kv *kvStateMachine) get(key string) string {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	return kv.data[key]
}

func (kv *kvStateMachine) size() int {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	return len(kv.data)
}

// --------------------------------------------------------------------------
// Helper: create a test cluster
// --------------------------------------------------------------------------

func newTestCluster(t *testing.T, sm StateMachine) *Cluster {
	t.Helper()
	config := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      7946,
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}
	c, err := New(config, sm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// --------------------------------------------------------------------------
// MemorySnapshotStore tests
// --------------------------------------------------------------------------

func TestMemorySnapshotStore_SaveAndLoad(t *testing.T) {
	store := NewMemorySnapshotStore()

	snap := &Snapshot{
		LastIncludedIndex: 10,
		LastIncludedTerm:  2,
		Data:              []byte(`{"key":"value"}`),
		Metadata:          map[string]string{"source": "test"},
	}

	if err := store.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.LastIncludedIndex != 10 {
		t.Errorf("LastIncludedIndex = %d, want 10", loaded.LastIncludedIndex)
	}
	if loaded.LastIncludedTerm != 2 {
		t.Errorf("LastIncludedTerm = %d, want 2", loaded.LastIncludedTerm)
	}
	if string(loaded.Data) != `{"key":"value"}` {
		t.Errorf("Data = %s, want {\"key\":\"value\"}", loaded.Data)
	}
	if loaded.Metadata["source"] != "test" {
		t.Errorf("Metadata[source] = %q, want test", loaded.Metadata["source"])
	}
}

func TestMemorySnapshotStore_LoadEmpty(t *testing.T) {
	store := NewMemorySnapshotStore()

	_, err := store.Load()
	if err == nil {
		t.Error("Load from empty store should error")
	}
}

func TestMemorySnapshotStore_List(t *testing.T) {
	store := NewMemorySnapshotStore()

	for i := uint64(1); i <= 3; i++ {
		if err := store.Save(&Snapshot{
			LastIncludedIndex: i * 10,
			LastIncludedTerm:  i,
			Data:              []byte(fmt.Sprintf("snapshot-%d", i)),
		}); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("List length = %d, want 3", len(metas))
	}

	// Newest first.
	if metas[0].LastIncludedIndex != 30 {
		t.Errorf("metas[0].LastIncludedIndex = %d, want 30", metas[0].LastIncludedIndex)
	}
	if metas[2].LastIncludedIndex != 10 {
		t.Errorf("metas[2].LastIncludedIndex = %d, want 10", metas[2].LastIncludedIndex)
	}
}

func TestMemorySnapshotStore_SaveNil(t *testing.T) {
	store := NewMemorySnapshotStore()
	if err := store.Save(nil); err == nil {
		t.Error("Save nil snapshot should error")
	}
}

// --------------------------------------------------------------------------
// FileSnapshotStore tests
// --------------------------------------------------------------------------

func TestFileSnapshotStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileSnapshotStore(dir, 3)
	if err != nil {
		t.Fatalf("NewFileSnapshotStore: %v", err)
	}

	snap := &Snapshot{
		LastIncludedIndex: 5,
		LastIncludedTerm:  1,
		Data:              []byte(`{"hello":"world"}`),
		Metadata:          map[string]string{"env": "test"},
	}

	if err := store.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.LastIncludedIndex != 5 {
		t.Errorf("LastIncludedIndex = %d, want 5", loaded.LastIncludedIndex)
	}
	if string(loaded.Data) != `{"hello":"world"}` {
		t.Errorf("Data mismatch")
	}
}

func TestFileSnapshotStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileSnapshotStore(dir, 5)
	if err != nil {
		t.Fatalf("NewFileSnapshotStore: %v", err)
	}

	for i := uint64(1); i <= 4; i++ {
		if err := store.Save(&Snapshot{
			LastIncludedIndex: i * 100,
			LastIncludedTerm:  i,
			Data:              []byte("data"),
		}); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 4 {
		t.Fatalf("List length = %d, want 4", len(metas))
	}
	// Newest first.
	if metas[0].LastIncludedIndex != 400 {
		t.Errorf("metas[0].LastIncludedIndex = %d, want 400", metas[0].LastIncludedIndex)
	}
}

func TestFileSnapshotStore_Retention(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileSnapshotStore(dir, 2) // keep only 2
	if err != nil {
		t.Fatalf("NewFileSnapshotStore: %v", err)
	}

	for i := uint64(1); i <= 5; i++ {
		if err := store.Save(&Snapshot{
			LastIncludedIndex: i,
			LastIncludedTerm:  1,
			Data:              []byte("x"),
		}); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	// Only 2 snapshot files should remain.
	matches, _ := filepath.Glob(filepath.Join(dir, "snapshot-*.json"))
	if len(matches) != 2 {
		t.Errorf("snapshot files = %d, want 2", len(matches))
	}

	// The latest should be index 5.
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LastIncludedIndex != 5 {
		t.Errorf("loaded.LastIncludedIndex = %d, want 5", loaded.LastIncludedIndex)
	}
}

func TestFileSnapshotStore_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileSnapshotStore(dir, 3)
	if err != nil {
		t.Fatalf("NewFileSnapshotStore: %v", err)
	}

	_, err = store.Load()
	if err == nil {
		t.Error("Load from empty store should error")
	}
}

// --------------------------------------------------------------------------
// Cluster snapshot creation and restore
// --------------------------------------------------------------------------

func TestCluster_CreateSnapshot(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Apply some data.
	sm.Apply([]byte("foo=bar"))
	sm.Apply([]byte("baz=qux"))

	// Simulate log entries.
	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("foo=bar")},
		{Index: 2, Term: 1, Command: []byte("baz=qux")},
		{Index: 3, Term: 2, Command: []byte("x=y")},
	}
	c.logMu.Unlock()

	snap, err := c.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	if snap.LastIncludedIndex != 3 {
		t.Errorf("LastIncludedIndex = %d, want 3", snap.LastIncludedIndex)
	}
	if snap.LastIncludedTerm != 2 {
		t.Errorf("LastIncludedTerm = %d, want 2", snap.LastIncludedTerm)
	}
	if snap.Metadata["node_id"] != "node1" {
		t.Errorf("Metadata[node_id] = %q, want node1", snap.Metadata["node_id"])
	}

	// Verify data was serialised.
	var data map[string]string
	if err := json.Unmarshal(snap.Data, &data); err != nil {
		t.Fatalf("unmarshal snapshot data: %v", err)
	}
	if data["foo"] != "bar" {
		t.Errorf("data[foo] = %q, want bar", data["foo"])
	}
}

func TestCluster_RestoreSnapshot(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Pre-populate some state that should be overwritten.
	sm.Apply([]byte("old=data"))

	// Populate log.
	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("old=data")},
	}
	c.logMu.Unlock()
	c.commitIndex.Store(1)
	c.lastApplied.Store(1)

	// Restore from a snapshot with different data.
	snapData, _ := json.Marshal(map[string]string{"restored": "yes"})
	snap := &Snapshot{
		LastIncludedIndex: 10,
		LastIncludedTerm:  3,
		Data:              snapData,
	}

	if err := c.RestoreSnapshot(snap); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	// State machine should be restored.
	if sm.get("restored") != "yes" {
		t.Errorf("state machine not restored: got %q", sm.get("restored"))
	}
	// Old data should be gone (Restore does a full reset in our mock).
	if sm.get("old") != "" {
		t.Errorf("old data should be cleared, got %q", sm.get("old"))
	}

	// Log should be empty.
	c.logMu.RLock()
	logLen := len(c.log)
	c.logMu.RUnlock()
	if logLen != 0 {
		t.Errorf("log length = %d, want 0 after restore", logLen)
	}

	// Indices should match the snapshot.
	if c.commitIndex.Load() != 10 {
		t.Errorf("commitIndex = %d, want 10", c.commitIndex.Load())
	}
	if c.lastApplied.Load() != 10 {
		t.Errorf("lastApplied = %d, want 10", c.lastApplied.Load())
	}
	if c.GetTerm() != 3 {
		t.Errorf("currentTerm = %d, want 3", c.GetTerm())
	}
}

func TestCluster_RestoreSnapshotNil(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	if err := c.RestoreSnapshot(nil); err == nil {
		t.Error("RestoreSnapshot(nil) should error")
	}
}

// --------------------------------------------------------------------------
// Log compaction
// --------------------------------------------------------------------------

func TestCluster_CompactLog(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 2},
		{Index: 4, Term: 2},
		{Index: 5, Term: 3},
	}
	c.logMu.Unlock()

	c.compactLog(3)

	c.logMu.RLock()
	remaining := len(c.log)
	c.logMu.RUnlock()

	if remaining != 2 {
		t.Errorf("log entries after compaction = %d, want 2", remaining)
	}

	entries := c.GetLogEntries(1)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Index != 4 {
		t.Errorf("first entry index = %d, want 4", entries[0].Index)
	}
	if entries[1].Index != 5 {
		t.Errorf("second entry index = %d, want 5", entries[1].Index)
	}
}

func TestCluster_CreateSnapshotCompactsLog(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("a=1")},
		{Index: 2, Term: 1, Command: []byte("b=2")},
	}
	c.logMu.Unlock()

	_, err := c.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// After snapshot at index 2, all entries should be compacted.
	c.logMu.RLock()
	logLen := len(c.log)
	c.logMu.RUnlock()

	if logLen != 0 {
		t.Errorf("log length after snapshot = %d, want 0", logLen)
	}
}

// --------------------------------------------------------------------------
// InstallSnapshot RPC
// --------------------------------------------------------------------------

func TestCluster_HandleInstallSnapshot(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)
	c.currentTerm.Store(1)

	// Add some log entries.
	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("x=1")},
	}
	c.logMu.Unlock()

	snapData, _ := json.Marshal(map[string]string{"snap": "data"})
	req := &InstallSnapshotRequest{
		Term:              2,
		LeaderID:          "leader1",
		LastIncludedIndex: 10,
		LastIncludedTerm:  2,
		Data:              snapData,
	}

	resp := c.HandleInstallSnapshot(req)

	if !resp.Success {
		t.Error("HandleInstallSnapshot should succeed")
	}
	if resp.Term != 2 {
		t.Errorf("response term = %d, want 2", resp.Term)
	}

	// Verify state was restored.
	if sm.get("snap") != "data" {
		t.Errorf("state machine data[snap] = %q, want data", sm.get("snap"))
	}

	// Verify term was updated.
	if c.GetTerm() != 2 {
		t.Errorf("term = %d, want 2", c.GetTerm())
	}

	// Verify leader was updated.
	if c.GetLeader() != "leader1" {
		t.Errorf("leader = %q, want leader1", c.GetLeader())
	}
}

func TestCluster_HandleInstallSnapshot_OlderTerm(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)
	c.currentTerm.Store(5)

	req := &InstallSnapshotRequest{
		Term:              3, // older term
		LeaderID:          "old_leader",
		LastIncludedIndex: 10,
		LastIncludedTerm:  3,
		Data:              []byte("{}"),
	}

	resp := c.HandleInstallSnapshot(req)

	if resp.Success {
		t.Error("should reject snapshot from older term")
	}
	if resp.Term != 5 {
		t.Errorf("response term = %d, want 5", resp.Term)
	}
}

func TestCluster_BuildInstallSnapshotRequest(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Apply some data via the state machine.
	sm.Apply([]byte("host=server1"))
	sm.Apply([]byte("port=8080"))

	// Populate log entries.
	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("host=server1")},
		{Index: 2, Term: 1, Command: []byte("port=8080")},
		{Index: 3, Term: 2, Command: []byte("mode=active")},
	}
	c.logMu.Unlock()

	// Set a term for the leader
	c.currentTerm.Store(2)

	req, err := c.BuildInstallSnapshotRequest()
	if err != nil {
		t.Fatalf("BuildInstallSnapshotRequest: %v", err)
	}

	if req == nil {
		t.Fatal("Expected non-nil request")
	}

	if req.Term != 2 {
		t.Errorf("Term = %d, want 2", req.Term)
	}
	if req.LeaderID != "node1" {
		t.Errorf("LeaderID = %q, want node1", req.LeaderID)
	}
	if req.LastIncludedIndex != 3 {
		t.Errorf("LastIncludedIndex = %d, want 3", req.LastIncludedIndex)
	}
	if req.LastIncludedTerm != 2 {
		t.Errorf("LastIncludedTerm = %d, want 2", req.LastIncludedTerm)
	}
	if len(req.Data) == 0 {
		t.Error("Expected non-empty snapshot data")
	}

	// Verify the snapshot data contains the state machine data.
	var data map[string]string
	if err := json.Unmarshal(req.Data, &data); err != nil {
		t.Fatalf("Failed to unmarshal snapshot data: %v", err)
	}
	if data["host"] != "server1" {
		t.Errorf("data[host] = %q, want server1", data["host"])
	}
	if data["port"] != "8080" {
		t.Errorf("data[port] = %q, want 8080", data["port"])
	}
}

func TestCluster_BuildInstallSnapshotRequest_EmptyLog(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// With an empty log, CreateSnapshot should still work (index/term = 0)
	req, err := c.BuildInstallSnapshotRequest()
	if err != nil {
		t.Fatalf("BuildInstallSnapshotRequest: %v", err)
	}

	if req.LastIncludedIndex != 0 {
		t.Errorf("LastIncludedIndex = %d, want 0", req.LastIncludedIndex)
	}
	if req.LastIncludedTerm != 0 {
		t.Errorf("LastIncludedTerm = %d, want 0", req.LastIncludedTerm)
	}
}

func TestCluster_ShouldSendSnapshot(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Empty log: should never need snapshot.
	if c.ShouldSendSnapshot(1) {
		t.Error("empty log should not trigger snapshot")
	}

	// Add log entries starting at index 5 (earlier ones compacted).
	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 5, Term: 2},
		{Index: 6, Term: 2},
		{Index: 7, Term: 3},
	}
	c.logMu.Unlock()

	// Follower at index 3 is behind the earliest entry.
	if !c.ShouldSendSnapshot(3) {
		t.Error("follower at index 3 should need snapshot (earliest is 5)")
	}

	// Follower at index 5 is fine.
	if c.ShouldSendSnapshot(5) {
		t.Error("follower at index 5 should not need snapshot")
	}

	// Follower at index 7 is also fine.
	if c.ShouldSendSnapshot(7) {
		t.Error("follower at index 7 should not need snapshot")
	}
}

// --------------------------------------------------------------------------
// TCPTransport tests
// --------------------------------------------------------------------------

// stubHandler is a minimal RPCHandler for transport tests.
type stubHandler struct {
	voteResp    *RequestVoteResponse
	appendResp  *AppendEntriesResponse
	installResp *InstallSnapshotResponse
}

func (s *stubHandler) HandleRequestVote(req *RequestVote) *RequestVoteResponse {
	if s.voteResp != nil {
		return s.voteResp
	}
	return &RequestVoteResponse{Term: req.Term, VoteGranted: true}
}

func (s *stubHandler) HandleAppendEntries(req *AppendEntries) *AppendEntriesResponse {
	if s.appendResp != nil {
		return s.appendResp
	}
	return &AppendEntriesResponse{Term: req.Term, Success: true}
}

func (s *stubHandler) HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse {
	if s.installResp != nil {
		return s.installResp
	}
	return &InstallSnapshotResponse{Term: req.Term, Success: true}
}

func TestTCPTransport_SendReceiveRequestVote(t *testing.T) {
	handler := &stubHandler{}

	config := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}

	transport, err := NewTCPTransport(config, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop()

	addr := transport.Addr()

	// Create a client transport (no handler needed for sending).
	clientConfig := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}
	client, err := NewTCPTransport(clientConfig, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport client: %v", err)
	}

	req := &RequestVote{
		Term:         5,
		CandidateID:  "node2",
		LastLogIndex: 10,
		LastLogTerm:  4,
	}

	resp, err := client.SendRequestVote(addr, req)
	if err != nil {
		t.Fatalf("SendRequestVote: %v", err)
	}

	if !resp.VoteGranted {
		t.Error("expected VoteGranted = true")
	}
	if resp.Term != 5 {
		t.Errorf("resp.Term = %d, want 5", resp.Term)
	}
}

func TestTCPTransport_SendReceiveAppendEntries(t *testing.T) {
	handler := &stubHandler{}

	config := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}

	transport, err := NewTCPTransport(config, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop()

	addr := transport.Addr()

	clientConfig := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}
	client, err := NewTCPTransport(clientConfig, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport client: %v", err)
	}

	req := &AppendEntries{
		Term:         3,
		LeaderID:     "leader1",
		PrevLogIndex: 5,
		PrevLogTerm:  2,
		Entries: []*LogEntry{
			{Index: 6, Term: 3, Command: []byte("cmd1")},
		},
		LeaderCommit: 5,
	}

	resp, err := client.SendAppendEntries(addr, req)
	if err != nil {
		t.Fatalf("SendAppendEntries: %v", err)
	}

	if !resp.Success {
		t.Error("expected Success = true")
	}
	if resp.Term != 3 {
		t.Errorf("resp.Term = %d, want 3", resp.Term)
	}
}

func TestTCPTransport_SendInstallSnapshot(t *testing.T) {
	handler := &stubHandler{}

	config := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}

	transport, err := NewTCPTransport(config, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop()

	addr := transport.Addr()

	clientConfig := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}
	client, err := NewTCPTransport(clientConfig, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport client: %v", err)
	}

	req := &InstallSnapshotRequest{
		Term:              7,
		LeaderID:          "leader1",
		LastIncludedIndex: 100,
		LastIncludedTerm:  6,
		Data:              []byte(`{"key":"value"}`),
	}

	resp, err := client.SendInstallSnapshot(addr, req)
	if err != nil {
		t.Fatalf("SendInstallSnapshot: %v", err)
	}

	if !resp.Success {
		t.Error("expected Success = true")
	}
	if resp.Term != 7 {
		t.Errorf("resp.Term = %d, want 7", resp.Term)
	}
}

func TestTCPTransport_ConnectionPooling(t *testing.T) {
	handler := &stubHandler{}

	config := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}

	transport, err := NewTCPTransport(config, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}

	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop()

	addr := transport.Addr()

	clientConfig := &TCPTransportConfig{
		BindAddr:    "127.0.0.1:0",
		MaxPoolSize: 3,
		Timeout:     5 * time.Second,
	}
	client, err := NewTCPTransport(clientConfig, handler)
	if err != nil {
		t.Fatalf("NewTCPTransport client: %v", err)
	}

	// Send multiple requests to build up the pool.
	for i := 0; i < 3; i++ {
		req := &RequestVote{
			Term:        uint64(i + 1),
			CandidateID: "node2",
		}
		_, err := client.SendRequestVote(addr, req)
		if err != nil {
			t.Fatalf("SendRequestVote %d: %v", i, err)
		}
	}

	// After 3 successful RPCs, we should have at most maxPoolSize connections.
	poolSize := client.PoolSize(addr)
	if poolSize > 3 {
		t.Errorf("pool size = %d, want <= 3", poolSize)
	}
	if poolSize == 0 {
		t.Error("pool should have at least one connection after successful RPCs")
	}
}

func TestTCPTransport_DefaultConfig(t *testing.T) {
	cfg := DefaultTCPTransportConfig()

	if cfg.BindAddr != "0.0.0.0:7947" {
		t.Errorf("BindAddr = %q, want 0.0.0.0:7947", cfg.BindAddr)
	}
	if cfg.MaxPoolSize != 5 {
		t.Errorf("MaxPoolSize = %d, want 5", cfg.MaxPoolSize)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
}

// --------------------------------------------------------------------------
// Binary framing tests
// --------------------------------------------------------------------------

func TestFrameWriteRead(t *testing.T) {
	// Use a pipe to test read/write.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	payload := []byte(`{"term":1,"candidate_id":"node1"}`)

	done := make(chan struct{})
	go func() {
		defer close(done)
		msgType, data, err := readFrame(server)
		if err != nil {
			t.Errorf("readFrame: %v", err)
			return
		}
		if msgType != msgRequestVote {
			t.Errorf("msgType = %d, want %d", msgType, msgRequestVote)
		}
		if string(data) != string(payload) {
			t.Errorf("data mismatch: got %q", data)
		}
	}()

	if err := writeFrame(client, msgRequestVote, payload); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	<-done
}

func TestFrameEmptyPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		msgType, data, err := readFrame(server)
		if err != nil {
			t.Errorf("readFrame: %v", err)
			return
		}
		if msgType != msgAppendEntries {
			t.Errorf("msgType = %d, want %d", msgType, msgAppendEntries)
		}
		if len(data) != 0 {
			t.Errorf("expected empty payload, got %d bytes", len(data))
		}
	}()

	if err := writeFrame(client, msgAppendEntries, nil); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	<-done
}

// --------------------------------------------------------------------------
// Membership change tests
// --------------------------------------------------------------------------

func TestMembershipChange_ProposalAndApply(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Make the cluster a leader so it can accept proposals.
	c.setState(StateLeader)
	c.leaderID.Store("node1")
	c.heartbeatTimer = time.NewTicker(500 * time.Millisecond)
	defer c.heartbeatTimer.Stop()

	// Start the run loop so commands get processed.
	go c.run()
	defer func() { close(c.stopCh) }()

	// Propose adding a node.
	change := MembershipChange{
		Type:    AddNode,
		NodeID:  "node2",
		Address: "127.0.0.1:7947",
	}

	err := c.ProposeMembershipChange(change)
	if err != nil {
		t.Fatalf("ProposeMembershipChange (add): %v", err)
	}

	// Verify the node was added.
	nodes := c.GetNodes()
	found := false
	for _, n := range nodes {
		if n.ID == "node2" {
			found = true
			break
		}
	}
	if !found {
		t.Error("node2 should be in the cluster after add")
	}

	// Verify transition is complete.
	if c.IsMembershipChangeInProgress() {
		t.Error("membership change should not be in progress after completion")
	}
}

func TestMembershipChange_RemoveNode(t *testing.T) {
	sm := newKVStateMachine()
	config := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      7946,
		Peers:         []string{"127.0.0.1:7947"},
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}
	c, err := New(config, sm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Make leader.
	c.setState(StateLeader)
	c.leaderID.Store("node1")
	c.heartbeatTimer = time.NewTicker(500 * time.Millisecond)
	defer c.heartbeatTimer.Stop()

	go c.run()
	defer func() { close(c.stopCh) }()

	// Remove the peer.
	change := MembershipChange{
		Type:   RemoveNode,
		NodeID: "127.0.0.1:7947",
	}

	err = c.ProposeMembershipChange(change)
	if err != nil {
		t.Fatalf("ProposeMembershipChange (remove): %v", err)
	}

	// Verify the node was removed.
	nodes := c.GetNodes()
	for _, n := range nodes {
		if n.ID == "127.0.0.1:7947" {
			t.Error("peer should be removed from cluster")
		}
	}
}

func TestMembershipChange_NotLeader(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Node is a follower by default.
	change := MembershipChange{
		Type:    AddNode,
		NodeID:  "node2",
		Address: "127.0.0.1:7947",
	}

	err := c.ProposeMembershipChange(change)
	if err == nil {
		t.Error("should reject membership change when not leader")
	}
}

func TestMembershipChange_ConcurrentRejection(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Make leader.
	c.setState(StateLeader)
	c.leaderID.Store("node1")
	c.heartbeatTimer = time.NewTicker(500 * time.Millisecond)
	defer c.heartbeatTimer.Stop()

	go c.run()
	defer func() { close(c.stopCh) }()

	// Simulate an in-progress transition.
	c.memberMu.Lock()
	c.membership.inTransition = true
	c.memberMu.Unlock()

	change := MembershipChange{
		Type:    AddNode,
		NodeID:  "node3",
		Address: "127.0.0.1:7948",
	}

	err := c.ProposeMembershipChange(change)
	if err == nil {
		t.Error("should reject concurrent membership change")
	}

	// Clean up.
	c.memberMu.Lock()
	c.membership.inTransition = false
	c.memberMu.Unlock()
}

func TestChangeType_String(t *testing.T) {
	if AddNode.String() != "AddNode" {
		t.Errorf("AddNode.String() = %q", AddNode.String())
	}
	if RemoveNode.String() != "RemoveNode" {
		t.Errorf("RemoveNode.String() = %q", RemoveNode.String())
	}
	if ChangeType(99).String() != "Unknown" {
		t.Errorf("Unknown.String() = %q", ChangeType(99).String())
	}
}

// --------------------------------------------------------------------------
// Integration-style: snapshot round-trip through store and restore
// --------------------------------------------------------------------------

func TestSnapshotRoundTrip(t *testing.T) {
	sm := newKVStateMachine()
	c := newTestCluster(t, sm)

	// Apply operations.
	sm.Apply([]byte("user=alice"))
	sm.Apply([]byte("role=admin"))

	c.logMu.Lock()
	c.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("user=alice")},
		{Index: 2, Term: 1, Command: []byte("role=admin")},
	}
	c.logMu.Unlock()

	// Create and save snapshot.
	snap, err := c.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	dir := t.TempDir()
	store, err := NewFileSnapshotStore(dir, 3)
	if err != nil {
		t.Fatalf("NewFileSnapshotStore: %v", err)
	}

	if err := store.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Create a new cluster and restore.
	sm2 := newKVStateMachine()
	c2 := newTestCluster(t, sm2)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := c2.RestoreSnapshot(loaded); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	if sm2.get("user") != "alice" {
		t.Errorf("restored user = %q, want alice", sm2.get("user"))
	}
	if sm2.get("role") != "admin" {
		t.Errorf("restored role = %q, want admin", sm2.get("role"))
	}

	// Use temporary file to ensure cleanup (Windows-safe).
	_ = os.RemoveAll(dir)
}
