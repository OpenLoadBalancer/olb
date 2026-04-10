package cluster

import (
	"testing"
	"time"
)

// TestPartition_LeaderStaysLeaderOnQuorumLoss documents the current behavior:
// when a leader loses connectivity to all peers, it remains in StateLeader
// but cannot commit new commands (handleCommand times out after 5s).
//
// This is intentional for the current version — the leader does not
// proactively step down on sustained heartbeat failures. A future
// improvement could add a quorum-health tracker in the heartbeat loop
// that triggers StateFollower after consecutive failures exceed a threshold.
func TestPartition_LeaderStaysLeaderOnQuorumLoss(t *testing.T) {
	sm := newKVStateMachine()
	config := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      0,
		Peers:         []string{"node2", "node3"},
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}
	c, err := New(config, sm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.setState(StateLeader)
	c.currentTerm.Store(1)

	// Point all peers at an unreachable address to simulate partition
	clientTransport := newStartedTransport(t, &stubHandler{})
	defer clientTransport.Stop()
	c.SetTransport(clientTransport)

	c.nodesMu.Lock()
	for _, id := range []string{"node2", "node3"} {
		if n, ok := c.nodes[id]; ok {
			n.Address = "127.0.0.1:1" // unreachable
		}
	}
	c.nodesMu.Unlock()

	// Attempt to commit — should time out since we can't reach quorum
	resultCh := make(chan *CommandResult, 1)
	c.handleCommand(&Command{
		Command: []byte("key=value"),
		Result:  resultCh,
	})

	select {
	case result := <-resultCh:
		if result.Error == nil {
			t.Fatal("expected quorum timeout error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handleCommand took too long")
	}

	// Key behavior: leader does NOT step down
	if c.GetState() != StateLeader {
		t.Errorf("leader state = %v, want StateLeader (leader stays leader on quorum loss)", c.GetState())
	}

	// Command was NOT applied
	if sm.get("key") == "value" {
		t.Error("command should not have been applied without quorum")
	}
}
