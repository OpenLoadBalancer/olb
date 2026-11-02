package cluster

import (
	"fmt"
	"testing"
	"time"
)

type testNode struct {
	id string
	c  *Cluster
	tr *TCPTransport
	sm *kvStateMachine
}

// newTestRaftCluster starts n fully-connected nodes over loopback TCP
// transports on ephemeral ports.
func newTestRaftCluster(t *testing.T, n int) []*testNode {
	t.Helper()
	nodes := make([]*testNode, n)
	for i := range nodes {
		id := fmt.Sprintf("n%d", i+1)
		sm := newKVStateMachine()
		c, err := New(&Config{
			NodeID:        id,
			BindAddr:      "127.0.0.1",
			BindPort:      1,
			ElectionTick:  400 * time.Millisecond,
			HeartbeatTick: 80 * time.Millisecond,
		}, sm)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		tr, err := NewTCPTransport(&TCPTransportConfig{BindAddr: "127.0.0.1:0", MaxPoolSize: 3, Timeout: time.Second}, c)
		if err != nil {
			t.Fatalf("transport: %v", err)
		}
		if err := tr.Start(); err != nil {
			t.Fatalf("transport start: %v", err)
		}
		c.SetTransport(tr)
		nodes[i] = &testNode{id: id, c: c, tr: tr, sm: sm}
	}
	for _, a := range nodes {
		for _, b := range nodes {
			a.c.AddNode(b.id, b.tr.Addr())
		}
	}
	for _, nd := range nodes {
		if err := nd.c.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, nd := range nodes {
			nd.stop()
		}
	})
	return nodes
}

func (n *testNode) stop() {
	defer func() { _ = recover() }() // already stopped
	_ = n.c.Stop()
	_ = n.tr.Stop()
}

func waitLeader(t *testing.T, nodes []*testNode, exclude *testNode) *testNode {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var leaders []*testNode
		for _, nd := range nodes {
			if nd != exclude && nd.c.IsLeader() {
				leaders = append(leaders, nd)
			}
		}
		if len(leaders) == 1 {
			return leaders[0]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no single leader elected in time")
	return nil
}

func waitValue(t *testing.T, nd *testNode, key, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if nd.sm.get(key) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: %s=%q, want %q", nd.id, key, nd.sm.get(key), want)
}

func TestRaft_ElectsLeaderAndReplicates(t *testing.T) {
	nodes := newTestRaftCluster(t, 3)
	leader := waitLeader(t, nodes, nil)

	res, err := leader.c.Propose([]byte("k=v1"))
	if err != nil || res.Error != nil {
		t.Fatalf("propose: %v / %v", err, res)
	}
	for _, nd := range nodes {
		waitValue(t, nd, "k", "v1")
	}
}

func TestRaft_LeaderFailureElectsNewLeaderAndKeepsData(t *testing.T) {
	nodes := newTestRaftCluster(t, 3)
	old := waitLeader(t, nodes, nil)

	if res, err := old.c.Propose([]byte("a=1")); err != nil || res.Error != nil {
		t.Fatalf("propose: %v / %+v", err, res)
	}
	for _, nd := range nodes {
		waitValue(t, nd, "a", "1")
	}

	old.stop() // crash the leader

	next := waitLeader(t, nodes, old)
	if next == old {
		t.Fatal("old leader re-elected")
	}
	if res, err := next.c.Propose([]byte("b=2")); err != nil || res.Error != nil {
		t.Fatalf("propose on new leader: %v / %+v", err, res)
	}
	for _, nd := range nodes {
		if nd == old {
			continue
		}
		waitValue(t, nd, "a", "1") // committed data survived failover
		waitValue(t, nd, "b", "2")
	}
}

func TestRaft_FollowerRejectsProposal(t *testing.T) {
	nodes := newTestRaftCluster(t, 3)
	leader := waitLeader(t, nodes, nil)
	for _, nd := range nodes {
		if nd == leader {
			continue
		}
		// Propose reports a rejection through CommandResult.Error.
		if res, err := nd.c.Propose([]byte("x=1")); err == nil && res.Error == nil {
			t.Fatalf("%s accepted a proposal while not leader", nd.id)
		}
	}
}

func TestRaft_LeaderStepsDownWithoutQuorum(t *testing.T) {
	nodes := newTestRaftCluster(t, 3)
	leader := waitLeader(t, nodes, nil)

	// Cut the leader off from both followers.
	for _, nd := range nodes {
		if nd != leader {
			nd.stop()
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for leader.c.IsLeader() {
		if time.Now().After(deadline) {
			t.Fatal("leader kept leading without a quorum")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
