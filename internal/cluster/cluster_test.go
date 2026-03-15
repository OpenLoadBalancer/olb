package cluster

import (
	"testing"
	"time"
)

// mockStateMachine is a mock state machine for testing.
type mockStateMachine struct {
	data map[string]string
}

func newMockStateMachine() *mockStateMachine {
	return &mockStateMachine{
		data: make(map[string]string),
	}
}

func (m *mockStateMachine) Apply(command []byte) ([]byte, error) {
	// Simple key=value command parsing
	cmd := string(command)
	if len(cmd) > 0 {
		parts := split(cmd, '=')
		if len(parts) == 2 {
			m.data[parts[0]] = parts[1]
		}
	}
	return []byte("ok"), nil
}

func (m *mockStateMachine) Snapshot() ([]byte, error) {
	return []byte{}, nil
}

func (m *mockStateMachine) Restore(snapshot []byte) error {
	return nil
}

// split splits a string by delimiter (simple implementation)
func split(s string, delim byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == delim {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.BindPort != 7946 {
		t.Errorf("BindPort = %d, want 7946", config.BindPort)
	}
	if config.ElectionTick != 2*time.Second {
		t.Errorf("ElectionTick = %v, want 2s", config.ElectionTick)
	}
	if config.HeartbeatTick != 500*time.Millisecond {
		t.Errorf("HeartbeatTick = %v, want 500ms", config.HeartbeatTick)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				NodeID:   "node1",
				BindAddr: "127.0.0.1",
				BindPort: 7946,
			},
			wantErr: false,
		},
		{
			name: "missing node ID",
			config: &Config{
				BindAddr: "127.0.0.1",
				BindPort: 7946,
			},
			wantErr: true,
		},
		{
			name: "empty bind addr defaults to 0.0.0.0",
			config: &Config{
				NodeID:   "node1",
				BindPort: 7946,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNew(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
		Peers:    []string{"127.0.0.1:7947", "127.0.0.1:7948"},
	}
	sm := newMockStateMachine()

	cluster, err := New(config, sm)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if cluster.config != config {
		t.Error("Config mismatch")
	}
	if cluster.stateMachine != sm {
		t.Error("State machine mismatch")
	}
	if len(cluster.nodes) != 3 { // self + 2 peers
		t.Errorf("Expected 3 nodes, got %d", len(cluster.nodes))
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	config := &Config{
		// Missing NodeID
		BindAddr: "127.0.0.1",
	}
	sm := newMockStateMachine()

	_, err := New(config, sm)
	if err == nil {
		t.Error("Expected error for invalid config")
	}
}

func TestCluster_GetState(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	state := cluster.GetState()
	if state != StateFollower {
		t.Errorf("Initial state = %v, want StateFollower", state)
	}
}

func TestCluster_GetTerm(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	term := cluster.GetTerm()
	if term != 0 {
		t.Errorf("Initial term = %d, want 0", term)
	}

	// Increment term
	cluster.incrementTerm()
	if cluster.GetTerm() != 1 {
		t.Errorf("Term after increment = %d, want 1", cluster.GetTerm())
	}
}

func TestCluster_IsLeader(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	if cluster.IsLeader() {
		t.Error("Should not be leader initially")
	}

	// Manually become leader
	cluster.setState(StateLeader)
	if !cluster.IsLeader() {
		t.Error("Should be leader after setState")
	}
}

func TestCluster_AddRemoveNode(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	// Add node
	cluster.AddNode("node2", "127.0.0.1:7947")
	nodes := cluster.GetNodes()
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodes))
	}

	// Remove node
	cluster.RemoveNode("node2")
	nodes = cluster.GetNodes()
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node after removal, got %d", len(nodes))
	}
}

func TestCluster_GetLogEntries(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	// Add some entries
	cluster.log = []*LogEntry{
		{Index: 1, Term: 1, Command: []byte("cmd1")},
		{Index: 2, Term: 1, Command: []byte("cmd2")},
		{Index: 3, Term: 2, Command: []byte("cmd3")},
	}

	entries := cluster.GetLogEntries(2)
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	if entries[0].Index != 2 {
		t.Errorf("First entry index = %d, want 2", entries[0].Index)
	}
}

func TestCluster_HandleRequestVote(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	// Set term to 1 for testing lower term case
	cluster.currentTerm.Store(1)

	tests := []struct {
		name     string
		req      *RequestVote
		expected bool
	}{
		{
			name: "lower term",
			req: &RequestVote{
				Term:         0,
				CandidateID:  "node2",
				LastLogIndex: 0,
				LastLogTerm:  0,
			},
			expected: false,
		},
		{
			name: "higher term",
			req: &RequestVote{
				Term:         1,
				CandidateID:  "node2",
				LastLogIndex: 0,
				LastLogTerm:  0,
			},
			expected: true,
		},
		{
			name: "already voted",
			req: &RequestVote{
				Term:         1,
				CandidateID:  "node3",
				LastLogIndex: 0,
				LastLogTerm:  0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := cluster.HandleRequestVote(tt.req)
			if resp.VoteGranted != tt.expected {
				t.Errorf("VoteGranted = %v, want %v", resp.VoteGranted, tt.expected)
			}
		})
	}
}

func TestCluster_HandleAppendEntries(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	tests := []struct {
		name     string
		req      *AppendEntries
		expected bool
	}{
		{
			name: "lower term",
			req: &AppendEntries{
				Term:     0,
				LeaderID: "node2",
			},
			expected: false,
		},
		{
			name: "higher term",
			req: &AppendEntries{
				Term:     1,
				LeaderID: "node2",
			},
			expected: true,
		},
		{
			name: "same term",
			req: &AppendEntries{
				Term:     1,
				LeaderID: "node2",
			},
			expected: true,
		},
	}

	// Set initial term
	cluster.currentTerm.Store(1)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := cluster.HandleAppendEntries(tt.req)
			if resp.Success != tt.expected {
				t.Errorf("Success = %v, want %v", resp.Success, tt.expected)
			}
		})
	}
}

func TestCluster_isLogUpToDate(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	// Set up log
	cluster.log = []*LogEntry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 2},
	}

	tests := []struct {
		name         string
		lastLogIndex uint64
		lastLogTerm  uint64
		expected     bool
	}{
		{
			name:         "higher term",
			lastLogIndex: 1,
			lastLogTerm:  3,
			expected:     true,
		},
		{
			name:         "same term, higher index",
			lastLogIndex: 4,
			lastLogTerm:  2,
			expected:     true,
		},
		{
			name:         "same term, same index",
			lastLogIndex: 3,
			lastLogTerm:  2,
			expected:     true,
		},
		{
			name:         "lower term",
			lastLogIndex: 5,
			lastLogTerm:  1,
			expected:     false,
		},
		{
			name:         "same term, lower index",
			lastLogIndex: 2,
			lastLogTerm:  2,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cluster.isLogUpToDate(tt.lastLogIndex, tt.lastLogTerm)
			if got != tt.expected {
				t.Errorf("isLogUpToDate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCluster_OnStateChange(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	stateChanges := []struct {
		old State
		new State
	}{}

	cluster.OnStateChange(func(oldState, newState State) {
		stateChanges = append(stateChanges, struct {
			old State
			new State
		}{oldState, newState})
	})

	// Change state
	cluster.setState(StateCandidate)
	cluster.setState(StateLeader)

	if len(stateChanges) != 2 {
		t.Errorf("Expected 2 state changes, got %d", len(stateChanges))
	}

	if stateChanges[0].old != StateFollower || stateChanges[0].new != StateCandidate {
		t.Errorf("First change = %v -> %v, want Follower -> Candidate", stateChanges[0].old, stateChanges[0].new)
	}
}

func TestCluster_OnLeaderElected(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	var electedLeader string
	cluster.OnLeaderElected(func(leaderID string) {
		electedLeader = leaderID
	})

	// Become leader
	cluster.becomeLeader()

	if electedLeader != "node1" {
		t.Errorf("Elected leader = %q, want node1", electedLeader)
	}
}

func TestCluster_getLastLogIndex(t *testing.T) {
	config := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}
	sm := newMockStateMachine()
	cluster, _ := New(config, sm)

	// Empty log
	if cluster.getLastLogIndex() != 0 {
		t.Errorf("Empty log index = %d, want 0", cluster.getLastLogIndex())
	}

	// Add entries
	cluster.log = []*LogEntry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
	}

	if cluster.getLastLogIndex() != 2 {
		t.Errorf("Last log index = %d, want 2", cluster.getLastLogIndex())
	}
}

func TestCluster_sendHeartbeats(t *testing.T) {
	// Test sendHeartbeats indirectly by becoming a leader with peers.
	config := &Config{
		NodeID:        "leader1",
		BindAddr:      "127.0.0.1",
		BindPort:      7946,
		Peers:         []string{"127.0.0.1:7947", "127.0.0.1:7948"},
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}
	sm := newMockStateMachine()
	cluster, err := New(config, sm)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Set term so heartbeats have a valid term
	cluster.currentTerm.Store(3)

	// Become leader to set up the necessary state
	cluster.setState(StateLeader)

	// Call sendHeartbeats directly - this exercises the function even though
	// the actual RPC is a TODO/no-op. It should not panic.
	cluster.sendHeartbeats()

	// Verify we're still leader and term is correct
	if cluster.GetState() != StateLeader {
		t.Error("Should still be leader after sending heartbeats")
	}
	if cluster.GetTerm() != 3 {
		t.Errorf("Term = %d, want 3", cluster.GetTerm())
	}
}
