package cluster

import (
	"testing"
	"time"
)

// TestPartition_LeaderStaysLeaderOnQuorumLoss covers handleCommand in
// isolation: when peers are unreachable the command times out without being
// applied. The heartbeat loop (not handleCommand) is what steps the leader
// down; see TestRaft_LeaderStepsDownWithoutQuorum.
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

	// handleCommand itself never changes the node's role
	if c.GetState() != StateLeader {
		t.Errorf("leader state = %v, want StateLeader (leader stays leader on quorum loss)", c.GetState())
	}

	// Command was NOT applied
	if sm.get("key") == "value" {
		t.Error("command should not have been applied without quorum")
	}
}
