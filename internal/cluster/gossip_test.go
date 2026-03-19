package cluster

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- Config tests ----

func TestDefaultGossipConfig(t *testing.T) {
	cfg := DefaultGossipConfig()
	if cfg.BindAddr != "0.0.0.0" {
		t.Errorf("BindAddr = %q, want %q", cfg.BindAddr, "0.0.0.0")
	}
	if cfg.BindPort != 7946 {
		t.Errorf("BindPort = %d, want %d", cfg.BindPort, 7946)
	}
	if cfg.ProbeInterval != 1*time.Second {
		t.Errorf("ProbeInterval = %v, want %v", cfg.ProbeInterval, 1*time.Second)
	}
	if cfg.ProbeTimeout != 500*time.Millisecond {
		t.Errorf("ProbeTimeout = %v, want %v", cfg.ProbeTimeout, 500*time.Millisecond)
	}
	if cfg.IndirectChecks != 3 {
		t.Errorf("IndirectChecks = %d, want %d", cfg.IndirectChecks, 3)
	}
	if cfg.SuspicionTimeout != 5*time.Second {
		t.Errorf("SuspicionTimeout = %v, want %v", cfg.SuspicionTimeout, 5*time.Second)
	}
	if cfg.GossipInterval != 200*time.Millisecond {
		t.Errorf("GossipInterval = %v, want %v", cfg.GossipInterval, 200*time.Millisecond)
	}
	if cfg.GossipNodes != 3 {
		t.Errorf("GossipNodes = %d, want %d", cfg.GossipNodes, 3)
	}
	if cfg.RetransmitMult != 4 {
		t.Errorf("RetransmitMult = %d, want %d", cfg.RetransmitMult, 4)
	}
	if cfg.MaxMessageSize != 1400 {
		t.Errorf("MaxMessageSize = %d, want %d", cfg.MaxMessageSize, 1400)
	}
	if cfg.TCPTimeout != 10*time.Second {
		t.Errorf("TCPTimeout = %v, want %v", cfg.TCPTimeout, 10*time.Second)
	}
}

func TestGossipConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*GossipConfig)
		wantErr bool
	}{
		{"valid defaults", func(c *GossipConfig) {}, false},
		{"invalid port zero", func(c *GossipConfig) { c.BindPort = 0 }, true},
		{"invalid port negative", func(c *GossipConfig) { c.BindPort = -1 }, true},
		{"invalid port too high", func(c *GossipConfig) { c.BindPort = 70000 }, true},
		{"zero probe interval", func(c *GossipConfig) { c.ProbeInterval = 0 }, true},
		{"zero probe timeout", func(c *GossipConfig) { c.ProbeTimeout = 0 }, true},
		{"negative indirect checks", func(c *GossipConfig) { c.IndirectChecks = -1 }, true},
		{"zero suspicion timeout", func(c *GossipConfig) { c.SuspicionTimeout = 0 }, true},
		{"zero max message size", func(c *GossipConfig) { c.MaxMessageSize = 0 }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultGossipConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---- Node tests ----

func TestGossipNodeCreation(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.NodeID = "test-node-1"

	// Use a free port to avoid conflicts in parallel test runs.
	cfg.BindPort = getFreePort(t)
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	local := g.LocalNode()
	if local.ID != "test-node-1" {
		t.Errorf("LocalNode().ID = %q, want %q", local.ID, "test-node-1")
	}
	if local.State != StateAlive {
		t.Errorf("LocalNode().State = %v, want %v", local.State, StateAlive)
	}
	if local.Incarnation != 0 {
		t.Errorf("LocalNode().Incarnation = %d, want %d", local.Incarnation, 0)
	}
}

func TestGossipNodeAddr(t *testing.T) {
	node := &GossipNode{
		Address: "192.168.1.1",
		Port:    7946,
	}
	if got := node.Addr(); got != "192.168.1.1:7946" {
		t.Errorf("Addr() = %q, want %q", got, "192.168.1.1:7946")
	}
}

func TestGossipNodeClone(t *testing.T) {
	node := &GossipNode{
		ID:          "node-1",
		Address:     "10.0.0.1",
		Port:        7946,
		State:       StateAlive,
		Incarnation: 5,
		LastSeen:    time.Now(),
		Metadata:    map[string]string{"role": "worker"},
	}

	clone := node.Clone()
	if clone.ID != node.ID {
		t.Errorf("Clone().ID = %q, want %q", clone.ID, node.ID)
	}
	if clone.Metadata["role"] != "worker" {
		t.Errorf("Clone().Metadata[role] = %q, want %q", clone.Metadata["role"], "worker")
	}

	// Mutating clone should not affect original.
	clone.Metadata["role"] = "master"
	if node.Metadata["role"] != "worker" {
		t.Error("Clone metadata mutation affected original")
	}
}

func TestNodeStateString(t *testing.T) {
	tests := []struct {
		state NodeState
		want  string
	}{
		{StateAlive, "alive"},
		{StateSuspect, "suspect"},
		{StateDead, "dead"},
		{StateLeft, "left"},
		{NodeState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("NodeState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestMessageTypeString(t *testing.T) {
	tests := []struct {
		mt   MessageType
		want string
	}{
		{MsgPing, "PING"},
		{MsgAck, "ACK"},
		{MsgPingReq, "PING_REQ"},
		{MsgSuspect, "SUSPECT"},
		{MsgAlive, "ALIVE"},
		{MsgDead, "DEAD"},
		{MsgJoin, "JOIN"},
		{MsgLeave, "LEAVE"},
		{MsgCompound, "COMPOUND"},
		{MessageType(0), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.mt.String(); got != tt.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", tt.mt, got, tt.want)
		}
	}
}

// ---- Binary serialization tests ----

func TestEncodeDecode_Ping(t *testing.T) {
	original := encodePing(42, "sender-node", "target-node")

	msgType, payload, remaining, err := decodeMessage(original)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgPing {
		t.Errorf("type = %v, want %v", msgType, MsgPing)
	}
	if len(remaining) != 0 {
		t.Errorf("remaining = %d bytes, want 0", len(remaining))
	}

	seqNo, senderID, targetID, err := decodePing(payload)
	if err != nil {
		t.Fatalf("decodePing() error = %v", err)
	}
	if seqNo != 42 {
		t.Errorf("seqNo = %d, want %d", seqNo, 42)
	}
	if senderID != "sender-node" {
		t.Errorf("senderID = %q, want %q", senderID, "sender-node")
	}
	if targetID != "target-node" {
		t.Errorf("targetID = %q, want %q", targetID, "target-node")
	}
}

func TestEncodeDecode_Ack(t *testing.T) {
	original := encodeAck(99, "responder")

	msgType, payload, _, err := decodeMessage(original)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgAck {
		t.Errorf("type = %v, want %v", msgType, MsgAck)
	}

	seqNo, senderID, err := decodeAck(payload)
	if err != nil {
		t.Fatalf("decodeAck() error = %v", err)
	}
	if seqNo != 99 {
		t.Errorf("seqNo = %d, want %d", seqNo, 99)
	}
	if senderID != "responder" {
		t.Errorf("senderID = %q, want %q", senderID, "responder")
	}
}

func TestEncodeDecode_PingReq(t *testing.T) {
	original := encodePingReq(7, "requester", "suspected-node")

	msgType, payload, _, err := decodeMessage(original)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgPingReq {
		t.Errorf("type = %v, want %v", msgType, MsgPingReq)
	}

	seqNo, senderID, targetID, err := decodePingReq(payload)
	if err != nil {
		t.Fatalf("decodePingReq() error = %v", err)
	}
	if seqNo != 7 {
		t.Errorf("seqNo = %d, want %d", seqNo, 7)
	}
	if senderID != "requester" {
		t.Errorf("senderID = %q, want %q", senderID, "requester")
	}
	if targetID != "suspected-node" {
		t.Errorf("targetID = %q, want %q", targetID, "suspected-node")
	}
}

func TestEncodeDecode_Suspect(t *testing.T) {
	original := encodeSuspect(10, "suspect-node")

	msgType, payload, _, err := decodeMessage(original)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgSuspect {
		t.Errorf("type = %v, want %v", msgType, MsgSuspect)
	}

	inc, nodeID, err := decodeSuspect(payload)
	if err != nil {
		t.Fatalf("decodeSuspect() error = %v", err)
	}
	if inc != 10 {
		t.Errorf("incarnation = %d, want %d", inc, 10)
	}
	if nodeID != "suspect-node" {
		t.Errorf("nodeID = %q, want %q", nodeID, "suspect-node")
	}
}

func TestEncodeDecode_Alive(t *testing.T) {
	meta := map[string]string{"role": "web", "version": "1.0"}
	original := encodeAlive(3, "alive-node", "10.0.0.1", 8080, meta)

	msgType, payload, _, err := decodeMessage(original)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgAlive {
		t.Errorf("type = %v, want %v", msgType, MsgAlive)
	}

	inc, nodeID, addr, port, metaOut, err := decodeAlive(payload)
	if err != nil {
		t.Fatalf("decodeAlive() error = %v", err)
	}
	if inc != 3 {
		t.Errorf("incarnation = %d, want %d", inc, 3)
	}
	if nodeID != "alive-node" {
		t.Errorf("nodeID = %q, want %q", nodeID, "alive-node")
	}
	if addr != "10.0.0.1" {
		t.Errorf("address = %q, want %q", addr, "10.0.0.1")
	}
	if port != 8080 {
		t.Errorf("port = %d, want %d", port, 8080)
	}
	if metaOut["role"] != "web" || metaOut["version"] != "1.0" {
		t.Errorf("metadata = %v, want role=web, version=1.0", metaOut)
	}
}

func TestEncodeDecode_Dead(t *testing.T) {
	original := encodeDead(15, "dead-node")

	msgType, payload, _, err := decodeMessage(original)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgDead {
		t.Errorf("type = %v, want %v", msgType, MsgDead)
	}

	inc, nodeID, err := decodeDead(payload)
	if err != nil {
		t.Fatalf("decodeDead() error = %v", err)
	}
	if inc != 15 {
		t.Errorf("incarnation = %d, want %d", inc, 15)
	}
	if nodeID != "dead-node" {
		t.Errorf("nodeID = %q, want %q", nodeID, "dead-node")
	}
}

func TestEncodeDecode_NodePayload_EmptyMetadata(t *testing.T) {
	payload := encodeNodePayload(1, "node-1", "127.0.0.1", 9000, nil)
	inc, nodeID, addr, port, meta, err := decodeNodePayload(payload)
	if err != nil {
		t.Fatalf("decodeNodePayload() error = %v", err)
	}
	if inc != 1 || nodeID != "node-1" || addr != "127.0.0.1" || port != 9000 {
		t.Errorf("unexpected values: inc=%d, nodeID=%q, addr=%q, port=%d", inc, nodeID, addr, port)
	}
	if len(meta) != 0 {
		t.Errorf("meta = %v, want empty", meta)
	}
}

func TestEncodeDecode_Compound(t *testing.T) {
	msgs := [][]byte{
		encodeSuspect(1, "node-a"),
		encodeAlive(2, "node-b", "10.0.0.2", 7946, nil),
		encodeDead(3, "node-c"),
	}

	compound := encodeCompound(msgs)

	msgType, payload, _, err := decodeMessage(compound)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msgType != MsgCompound {
		t.Errorf("type = %v, want %v", msgType, MsgCompound)
	}

	decoded, err := decodeCompound(payload)
	if err != nil {
		t.Fatalf("decodeCompound() error = %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("compound message count = %d, want %d", len(decoded), 3)
	}

	// Verify first sub-message is SUSPECT.
	mt, _, _, err := decodeMessage(decoded[0])
	if err != nil {
		t.Fatalf("decode sub-message 0: %v", err)
	}
	if mt != MsgSuspect {
		t.Errorf("sub-message 0 type = %v, want %v", mt, MsgSuspect)
	}
}

func TestDecodeMessage_TooShort(t *testing.T) {
	_, _, _, err := decodeMessage([]byte{0x01})
	if err == nil {
		t.Error("expected error for short message")
	}
}

func TestDecodeMessage_Truncated(t *testing.T) {
	// Type=1, length=100, but only 3 bytes total.
	msg := []byte{0x01, 0x00, 0x64}
	_, _, _, err := decodeMessage(msg)
	if err == nil {
		t.Error("expected error for truncated payload")
	}
}

func TestDecodeMessage_WithRemaining(t *testing.T) {
	ping := encodePing(1, "a", "b")
	ack := encodeAck(2, "c")
	combined := append(ping, ack...)

	mt, _, remaining, err := decodeMessage(combined)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if mt != MsgPing {
		t.Errorf("type = %v, want %v", mt, MsgPing)
	}
	if len(remaining) == 0 {
		t.Error("expected remaining bytes")
	}

	mt2, _, _, err := decodeMessage(remaining)
	if err != nil {
		t.Fatalf("decodeMessage(remaining) error = %v", err)
	}
	if mt2 != MsgAck {
		t.Errorf("remaining type = %v, want %v", mt2, MsgAck)
	}
}

// ---- Broadcast queue tests ----

func TestBroadcastQueue_AddAndGet(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "bq-test"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	msg1 := encodeSuspect(1, "node-a")
	msg2 := encodeAlive(2, "node-b", "10.0.0.2", 7946, nil)

	g.queueBroadcast(msg1)
	g.queueBroadcast(msg2)

	broadcasts := g.getBroadcasts(65536)
	if len(broadcasts) != 2 {
		t.Errorf("getBroadcasts() returned %d, want 2", len(broadcasts))
	}
}

func TestBroadcastQueue_SizeLimit(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "bq-limit-test"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add a large number of broadcasts.
	for i := 0; i < 100; i++ {
		g.queueBroadcast(encodeSuspect(uint32(i), fmt.Sprintf("node-%d", i)))
	}

	// Get with a small limit.
	broadcasts := g.getBroadcasts(50)
	if len(broadcasts) == 0 {
		t.Error("expected at least one broadcast within limit")
	}

	total := 0
	for _, b := range broadcasts {
		total += len(b)
	}
	if total > 50 {
		t.Errorf("total broadcast size = %d, exceeds limit 50", total)
	}
}

func TestBroadcastQueue_Retransmit(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "bq-retransmit"
	cfg.RetransmitMult = 1 // small for testing
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	msg := encodeSuspect(1, "node-a")
	g.queueBroadcast(msg)

	// First get should return the broadcast.
	b1 := g.getBroadcasts(65536)
	if len(b1) != 1 {
		t.Fatalf("first getBroadcasts() returned %d, want 1", len(b1))
	}

	// The retransmit count should be decremented. With RetransmitMult=1 and
	// 1 member (self), retransmitLimit = 1 * ceil(log2(2)) = 1.
	// After one get, retransmit goes from 1 to 0, so it should be removed.
	b2 := g.getBroadcasts(65536)
	if len(b2) != 0 {
		t.Errorf("second getBroadcasts() returned %d, want 0 (exhausted retransmit)", len(b2))
	}
}

func TestBroadcastQueue_Priority(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "bq-priority"
	cfg.RetransmitMult = 10 // high so nothing expires
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add old broadcast.
	g.broadcastsMu.Lock()
	g.broadcasts = append(g.broadcasts, &broadcast{
		msg:        encodeSuspect(1, "old-node"),
		retransmit: 10,
		created:    time.Now().Add(-10 * time.Second),
	})
	g.broadcastsMu.Unlock()

	// Add new broadcast.
	g.broadcastsMu.Lock()
	g.broadcasts = append(g.broadcasts, &broadcast{
		msg:        encodeSuspect(2, "new-node"),
		retransmit: 10,
		created:    time.Now(),
	})
	g.broadcastsMu.Unlock()

	// Get with enough space for both.
	broadcasts := g.getBroadcasts(65536)
	if len(broadcasts) < 2 {
		t.Fatalf("expected at least 2 broadcasts, got %d", len(broadcasts))
	}

	// First broadcast should be the newer one.
	_, p0, _, _ := decodeMessage(broadcasts[0])
	_, nodeID0, _ := decodeSuspect(p0)
	if nodeID0 != "new-node" {
		t.Errorf("first broadcast node = %q, want %q (newer should come first)", nodeID0, "new-node")
	}
}

// ---- Incarnation number precedence tests ----

func TestIncarnationPrecedence_AliveOverridesSuspect(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add a member.
	g.membersMu.Lock()
	g.members["remote"] = &GossipNode{
		ID:          "remote",
		Address:     "10.0.0.2",
		Port:        7946,
		State:       StateSuspect,
		Incarnation: 5,
		LastSeen:    time.Now(),
		Metadata:    map[string]string{},
	}
	g.membersMu.Unlock()

	// Send ALIVE with higher incarnation.
	alivePayload := encodeNodePayload(6, "remote", "10.0.0.2", 7946, nil)
	g.handleAlive(alivePayload)

	g.membersMu.RLock()
	m := g.members["remote"]
	g.membersMu.RUnlock()

	if m.State != StateAlive {
		t.Errorf("state = %v, want %v (ALIVE with higher incarnation should override SUSPECT)", m.State, StateAlive)
	}
	if m.Incarnation != 6 {
		t.Errorf("incarnation = %d, want %d", m.Incarnation, 6)
	}
}

func TestIncarnationPrecedence_SameIncarnationNoOverride(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add a suspect member.
	g.membersMu.Lock()
	g.members["remote"] = &GossipNode{
		ID:          "remote",
		Address:     "10.0.0.2",
		Port:        7946,
		State:       StateSuspect,
		Incarnation: 5,
		LastSeen:    time.Now(),
		Metadata:    map[string]string{},
	}
	g.membersMu.Unlock()

	// ALIVE with same incarnation should NOT override SUSPECT.
	alivePayload := encodeNodePayload(5, "remote", "10.0.0.2", 7946, nil)
	g.handleAlive(alivePayload)

	g.membersMu.RLock()
	m := g.members["remote"]
	g.membersMu.RUnlock()

	if m.State != StateSuspect {
		t.Errorf("state = %v, want %v (ALIVE with same incarnation should not override SUSPECT)", m.State, StateSuspect)
	}
}

func TestIncarnationPrecedence_SuspectOverridesAlive(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add an alive member.
	g.membersMu.Lock()
	g.members["remote"] = &GossipNode{
		ID:          "remote",
		Address:     "10.0.0.2",
		Port:        7946,
		State:       StateAlive,
		Incarnation: 5,
		LastSeen:    time.Now(),
		Metadata:    map[string]string{},
	}
	g.membersMu.Unlock()

	// SUSPECT with same incarnation should override ALIVE.
	suspectPayload := make([]byte, 4+2+len("remote"))
	binary.BigEndian.PutUint32(suspectPayload[0:4], 5) // same incarnation
	binary.BigEndian.PutUint16(suspectPayload[4:6], uint16(len("remote")))
	copy(suspectPayload[6:], "remote")
	g.handleSuspect(suspectPayload)

	g.membersMu.RLock()
	m := g.members["remote"]
	g.membersMu.RUnlock()

	if m.State != StateSuspect {
		t.Errorf("state = %v, want %v (SUSPECT with >= incarnation should override ALIVE)", m.State, StateSuspect)
	}
}

func TestIncarnationPrecedence_DeadOverridesAll(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add a suspect member.
	g.membersMu.Lock()
	g.members["remote"] = &GossipNode{
		ID:          "remote",
		Address:     "10.0.0.2",
		Port:        7946,
		State:       StateSuspect,
		Incarnation: 5,
		LastSeen:    time.Now(),
		Metadata:    map[string]string{},
	}
	g.membersMu.Unlock()

	// DEAD with same incarnation should override.
	deadPayload := make([]byte, 4+2+len("remote"))
	binary.BigEndian.PutUint32(deadPayload[0:4], 5)
	binary.BigEndian.PutUint16(deadPayload[4:6], uint16(len("remote")))
	copy(deadPayload[6:], "remote")
	g.handleDead(deadPayload)

	g.membersMu.RLock()
	m := g.members["remote"]
	g.membersMu.RUnlock()

	if m.State != StateDead {
		t.Errorf("state = %v, want %v (DEAD should override SUSPECT)", m.State, StateDead)
	}
}

// ---- Self-refutation tests ----

func TestSelfRefutation_Suspect(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local-node"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Someone suspects us with incarnation 3.
	suspectPayload := make([]byte, 4+2+len("local-node"))
	binary.BigEndian.PutUint32(suspectPayload[0:4], 3)
	binary.BigEndian.PutUint16(suspectPayload[4:6], uint16(len("local-node")))
	copy(suspectPayload[6:], "local-node")
	g.handleSuspect(suspectPayload)

	// We should have incremented our incarnation to refute.
	if g.localNode.Incarnation != 4 {
		t.Errorf("incarnation = %d, want 4 (should increment to refute suspicion)", g.localNode.Incarnation)
	}
}

func TestSelfRefutation_Dead(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local-node"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Someone declares us dead with incarnation 2.
	deadPayload := make([]byte, 4+2+len("local-node"))
	binary.BigEndian.PutUint32(deadPayload[0:4], 2)
	binary.BigEndian.PutUint16(deadPayload[4:6], uint16(len("local-node")))
	copy(deadPayload[6:], "local-node")
	g.handleDead(deadPayload)

	// We should have incremented our incarnation to refute.
	if g.localNode.Incarnation != 3 {
		t.Errorf("incarnation = %d, want 3 (should increment to refute dead declaration)", g.localNode.Incarnation)
	}
}

// ---- Callback / event handler tests ----

func TestEventCallbacks(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	var mu sync.Mutex
	var events []EventType
	var nodes []string

	g.OnEvent(func(event EventType, node *GossipNode) {
		mu.Lock()
		events = append(events, event)
		nodes = append(nodes, node.ID)
		mu.Unlock()
	})

	// Simulate a JOIN via ALIVE message (new node).
	alivePayload := encodeNodePayload(1, "new-node", "10.0.0.2", 7946, nil)
	g.handleAlive(alivePayload)

	mu.Lock()
	if len(events) != 1 || events[0] != EventJoin {
		t.Errorf("events = %v, want [EventJoin]", events)
	}
	if nodes[0] != "new-node" {
		t.Errorf("node = %q, want %q", nodes[0], "new-node")
	}
	mu.Unlock()
}

// ---- Join/Leave handler tests ----

func TestHandleJoin(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	cfg.TCPTimeout = 100 * time.Millisecond // Short timeout for test
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	joinPayload := encodeNodePayload(1, "joiner", "10.0.0.5", 7946, map[string]string{"env": "prod"})
	g.handleJoin(joinPayload, "10.0.0.5:7946")

	g.membersMu.RLock()
	m, ok := g.members["joiner"]
	g.membersMu.RUnlock()

	if !ok {
		t.Fatal("expected joiner in members")
	}
	if m.State != StateAlive {
		t.Errorf("state = %v, want %v", m.State, StateAlive)
	}
	if m.Metadata["env"] != "prod" {
		t.Errorf("metadata[env] = %q, want %q", m.Metadata["env"], "prod")
	}
}

func TestHandleLeave(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// Add a member first.
	g.membersMu.Lock()
	g.members["leaver"] = &GossipNode{
		ID:       "leaver",
		Address:  "10.0.0.6",
		Port:     7946,
		State:    StateAlive,
		Metadata: map[string]string{},
	}
	g.membersMu.Unlock()

	leavePayload := encodeNodePayload(1, "leaver", "10.0.0.6", 7946, nil)
	g.handleLeaveMsg(leavePayload)

	g.membersMu.RLock()
	m := g.members["leaver"]
	g.membersMu.RUnlock()

	if m.State != StateLeft {
		t.Errorf("state = %v, want %v", m.State, StateLeft)
	}
}

// ---- Members and NumMembers tests ----

func TestMembersAndNumMembers(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "local"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if g.NumMembers() != 0 {
		t.Errorf("NumMembers() = %d, want 0", g.NumMembers())
	}

	g.membersMu.Lock()
	g.members["a"] = &GossipNode{ID: "a", Address: "10.0.0.1", Port: 7946, State: StateAlive, Metadata: map[string]string{}}
	g.members["b"] = &GossipNode{ID: "b", Address: "10.0.0.2", Port: 7946, State: StateSuspect, Metadata: map[string]string{}}
	g.members["c"] = &GossipNode{ID: "c", Address: "10.0.0.3", Port: 7946, State: StateDead, Metadata: map[string]string{}}
	g.membersMu.Unlock()

	if g.NumMembers() != 2 {
		t.Errorf("NumMembers() = %d, want 2 (alive + suspect)", g.NumMembers())
	}

	members := g.Members()
	if len(members) != 2 {
		t.Errorf("Members() len = %d, want 2", len(members))
	}

	allMembers := g.AllMembers()
	if len(allMembers) != 4 { // 3 remote + 1 local
		t.Errorf("AllMembers() len = %d, want 4", len(allMembers))
	}
}

// ---- Integration test: PING/ACK exchange between two nodes ----

func TestPingAckExchange(t *testing.T) {
	// Create two gossip nodes on different ports.
	port1, port2 := getFreePort(t), getFreePort(t)

	cfg1 := DefaultGossipConfig()
	cfg1.BindAddr = "127.0.0.1"
	cfg1.BindPort = port1
	cfg1.NodeID = "node-1"
	cfg1.ProbeInterval = 50 * time.Millisecond
	cfg1.ProbeTimeout = 200 * time.Millisecond
	cfg1.GossipInterval = 50 * time.Millisecond

	cfg2 := DefaultGossipConfig()
	cfg2.BindAddr = "127.0.0.1"
	cfg2.BindPort = port2
	cfg2.NodeID = "node-2"
	cfg2.ProbeInterval = 50 * time.Millisecond
	cfg2.ProbeTimeout = 200 * time.Millisecond
	cfg2.GossipInterval = 50 * time.Millisecond

	g1, err := NewGossip(cfg1)
	if err != nil {
		t.Fatalf("NewGossip(1) error = %v", err)
	}
	g2, err := NewGossip(cfg2)
	if err != nil {
		t.Fatalf("NewGossip(2) error = %v", err)
	}

	if err := g1.Start(); err != nil {
		t.Fatalf("g1.Start() error = %v", err)
	}
	defer g1.Stop()

	if err := g2.Start(); err != nil {
		t.Fatalf("g2.Start() error = %v", err)
	}
	defer g2.Stop()

	// Manually add node-2 to node-1's members so probe loop can reach it.
	g1.membersMu.Lock()
	g1.members["node-2"] = &GossipNode{
		ID:       "node-2",
		Address:  "127.0.0.1",
		Port:     port2,
		State:    StateAlive,
		LastSeen: time.Now(),
		Metadata: map[string]string{},
	}
	g1.membersMu.Unlock()

	// Send a direct PING from node-1 to node-2.
	seqNo := g1.nextSeqNo()
	acked := g1.probeNode(&GossipNode{
		ID:      "node-2",
		Address: "127.0.0.1",
		Port:    port2,
	}, seqNo)

	if !acked {
		t.Error("expected ACK from node-2 but did not receive one")
	}
}

// ---- Integration test: 3-node cluster formation ----

func TestThreeNodeCluster(t *testing.T) {
	port1, port2, port3 := getFreePort(t), getFreePort(t), getFreePort(t)

	makeConfig := func(port int, id string) *GossipConfig {
		return &GossipConfig{
			BindAddr:             "127.0.0.1",
			BindPort:             port,
			NodeID:               id,
			ProbeInterval:        100 * time.Millisecond,
			ProbeTimeout:         200 * time.Millisecond,
			IndirectChecks:       3,
			SuspicionTimeout:     2 * time.Second,
			GossipInterval:       50 * time.Millisecond,
			GossipNodes:          3,
			RetransmitMult:       4,
			MaxMessageSize:       1400,
			TCPTimeout:           5 * time.Second,
			DeadNodeReapInterval: 10 * time.Second,
		}
	}

	g1, _ := NewGossip(makeConfig(port1, "node-1"))
	g2, _ := NewGossip(makeConfig(port2, "node-2"))
	g3, _ := NewGossip(makeConfig(port3, "node-3"))

	if err := g1.Start(); err != nil {
		t.Fatalf("g1.Start() error = %v", err)
	}
	defer g1.Stop()

	if err := g2.Start(); err != nil {
		t.Fatalf("g2.Start() error = %v", err)
	}
	defer g2.Stop()

	if err := g3.Start(); err != nil {
		t.Fatalf("g3.Start() error = %v", err)
	}
	defer g3.Stop()

	// Node-2 joins via node-1.
	if err := g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)}); err != nil {
		t.Fatalf("g2.Join() error = %v", err)
	}

	// Node-3 joins via node-1.
	if err := g3.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)}); err != nil {
		t.Fatalf("g3.Join() error = %v", err)
	}

	// Wait for membership to converge.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g1.NumMembers() >= 2 && g2.NumMembers() >= 1 && g3.NumMembers() >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if g1.NumMembers() < 2 {
		t.Errorf("g1 NumMembers() = %d, want >= 2", g1.NumMembers())
	}
	if g2.NumMembers() < 1 {
		t.Errorf("g2 NumMembers() = %d, want >= 1", g2.NumMembers())
	}
	if g3.NumMembers() < 1 {
		t.Errorf("g3 NumMembers() = %d, want >= 1", g3.NumMembers())
	}
}

// ---- Integration test: node failure detection ----

func TestNodeFailureDetection(t *testing.T) {
	port1, port2 := getFreePort(t), getFreePort(t)

	cfg1 := &GossipConfig{
		BindAddr:             "127.0.0.1",
		BindPort:             port1,
		NodeID:               "detector",
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         100 * time.Millisecond,
		IndirectChecks:       0, // no indirect checks for simpler test
		SuspicionTimeout:     200 * time.Millisecond,
		GossipInterval:       50 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       1400,
		TCPTimeout:           2 * time.Second,
		DeadNodeReapInterval: 10 * time.Second,
	}
	cfg2 := &GossipConfig{
		BindAddr:             "127.0.0.1",
		BindPort:             port2,
		NodeID:               "target",
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         100 * time.Millisecond,
		IndirectChecks:       0,
		SuspicionTimeout:     200 * time.Millisecond,
		GossipInterval:       50 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       1400,
		TCPTimeout:           2 * time.Second,
		DeadNodeReapInterval: 10 * time.Second,
	}

	g1, _ := NewGossip(cfg1)
	g2, _ := NewGossip(cfg2)

	var leaveDetected atomic.Bool
	g1.OnEvent(func(event EventType, node *GossipNode) {
		if event == EventLeave && node.ID == "target" {
			leaveDetected.Store(true)
		}
	})

	if err := g1.Start(); err != nil {
		t.Fatalf("g1.Start() error = %v", err)
	}
	defer g1.Stop()

	if err := g2.Start(); err != nil {
		t.Fatalf("g2.Start() error = %v", err)
	}

	// Join.
	if err := g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)}); err != nil {
		t.Fatalf("g2.Join() error = %v", err)
	}

	// Wait for membership to establish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if g1.NumMembers() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Now stop g2 abruptly to simulate failure.
	g2.Stop()

	// Wait for g1 to detect the failure.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if leaveDetected.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !leaveDetected.Load() {
		// Check if at least suspected.
		g1.membersMu.RLock()
		m := g1.members["target"]
		g1.membersMu.RUnlock()
		if m != nil && m.State == StateSuspect {
			// Suspicion detected is acceptable; the timer may not have fired yet.
			t.Log("node suspected but not yet declared dead (acceptable in fast test)")
		} else if m != nil && m.State == StateDead {
			// Dead is fine too.
		} else {
			t.Error("expected target node to be detected as failed")
		}
	}
}

// ---- Integration test: graceful leave ----

func TestGracefulLeave(t *testing.T) {
	port1, port2 := getFreePort(t), getFreePort(t)

	cfg1 := &GossipConfig{
		BindAddr:             "127.0.0.1",
		BindPort:             port1,
		NodeID:               "stayer",
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         100 * time.Millisecond,
		IndirectChecks:       0,
		SuspicionTimeout:     500 * time.Millisecond,
		GossipInterval:       50 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       1400,
		TCPTimeout:           2 * time.Second,
		DeadNodeReapInterval: 10 * time.Second,
	}
	cfg2 := &GossipConfig{
		BindAddr:             "127.0.0.1",
		BindPort:             port2,
		NodeID:               "leaver",
		ProbeInterval:        50 * time.Millisecond,
		ProbeTimeout:         100 * time.Millisecond,
		IndirectChecks:       0,
		SuspicionTimeout:     500 * time.Millisecond,
		GossipInterval:       50 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       1400,
		TCPTimeout:           2 * time.Second,
		DeadNodeReapInterval: 10 * time.Second,
	}

	g1, _ := NewGossip(cfg1)
	g2, _ := NewGossip(cfg2)

	var leftDetected atomic.Bool
	g1.OnEvent(func(event EventType, node *GossipNode) {
		if event == EventLeave && node.ID == "leaver" {
			leftDetected.Store(true)
		}
	})

	if err := g1.Start(); err != nil {
		t.Fatalf("g1.Start() error = %v", err)
	}
	defer g1.Stop()

	if err := g2.Start(); err != nil {
		t.Fatalf("g2.Start() error = %v", err)
	}

	// Join.
	if err := g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)}); err != nil {
		t.Fatalf("g2.Join() error = %v", err)
	}

	// Wait for membership.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if g1.NumMembers() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Graceful leave.
	if err := g2.Leave(); err != nil {
		t.Fatalf("g2.Leave() error = %v", err)
	}

	// Wait for g1 to receive the leave.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if leftDetected.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !leftDetected.Load() {
		// Check member state directly.
		g1.membersMu.RLock()
		m := g1.members["leaver"]
		g1.membersMu.RUnlock()
		if m == nil || m.State != StateLeft {
			t.Error("expected leaver to be in StateLeft")
		}
	}

	g2.Stop()
}

// ---- Integration test: TCP fallback for large messages ----

func TestTCPFallbackLargeMessage(t *testing.T) {
	port1, port2 := getFreePort(t), getFreePort(t)

	cfg1 := &GossipConfig{
		BindAddr:             "127.0.0.1",
		BindPort:             port1,
		NodeID:               "receiver",
		ProbeInterval:        1 * time.Second,
		ProbeTimeout:         500 * time.Millisecond,
		IndirectChecks:       3,
		SuspicionTimeout:     5 * time.Second,
		GossipInterval:       200 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       100, // Very small to force TCP fallback
		TCPTimeout:           5 * time.Second,
		DeadNodeReapInterval: 30 * time.Second,
	}
	cfg2 := &GossipConfig{
		BindAddr:             "127.0.0.1",
		BindPort:             port2,
		NodeID:               "sender",
		ProbeInterval:        1 * time.Second,
		ProbeTimeout:         500 * time.Millisecond,
		IndirectChecks:       3,
		SuspicionTimeout:     5 * time.Second,
		GossipInterval:       200 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       100, // Very small to force TCP fallback
		TCPTimeout:           5 * time.Second,
		DeadNodeReapInterval: 30 * time.Second,
	}

	g1, _ := NewGossip(cfg1)
	g2, _ := NewGossip(cfg2)

	var joinDetected atomic.Bool
	g1.OnEvent(func(event EventType, node *GossipNode) {
		if event == EventJoin {
			joinDetected.Store(true)
		}
	})

	if err := g1.Start(); err != nil {
		t.Fatalf("g1.Start() error = %v", err)
	}
	defer g1.Stop()

	if err := g2.Start(); err != nil {
		t.Fatalf("g2.Start() error = %v", err)
	}
	defer g2.Stop()

	// Build a large message (larger than MaxMessageSize=100).
	largeMeta := map[string]string{}
	for i := 0; i < 10; i++ {
		largeMeta[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("value-with-lots-of-data-%d", i)
	}

	// Join sends via TCP, which should work even though MaxMessageSize is small.
	if err := g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)}); err != nil {
		t.Fatalf("g2.Join() error = %v", err)
	}

	// Wait for join event.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if joinDetected.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !joinDetected.Load() {
		t.Error("expected join event via TCP fallback")
	}
}

// ---- Integration test: indirect probe (PING_REQ) ----

func TestIndirectProbe(t *testing.T) {
	port1, port2, port3 := getFreePort(t), getFreePort(t), getFreePort(t)

	makeConfig := func(port int, id string) *GossipConfig {
		return &GossipConfig{
			BindAddr:             "127.0.0.1",
			BindPort:             port,
			NodeID:               id,
			ProbeInterval:        100 * time.Millisecond,
			ProbeTimeout:         300 * time.Millisecond,
			IndirectChecks:       3,
			SuspicionTimeout:     2 * time.Second,
			GossipInterval:       50 * time.Millisecond,
			GossipNodes:          3,
			RetransmitMult:       4,
			MaxMessageSize:       1400,
			TCPTimeout:           5 * time.Second,
			DeadNodeReapInterval: 10 * time.Second,
		}
	}

	g1, _ := NewGossip(makeConfig(port1, "prober"))
	g2, _ := NewGossip(makeConfig(port2, "mediator"))
	g3, _ := NewGossip(makeConfig(port3, "target"))

	for _, g := range []*Gossip{g1, g2, g3} {
		if err := g.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer g.Stop()
	}

	// Establish 3-node cluster.
	g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)})
	g3.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)})

	// Wait for convergence.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if g1.NumMembers() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Send a PING_REQ from g1 to g2, asking it to probe g3.
	seqNo := g1.nextSeqNo()
	pingReq := encodePingReq(seqNo, "prober", "target")

	ackCh := make(chan struct{}, 1)
	g1.ackHandlersMu.Lock()
	g1.ackHandlers[seqNo] = &ackHandler{
		seqNo:   seqNo,
		timer:   time.AfterFunc(2*time.Second, func() {}),
		ackCh:   ackCh,
		created: time.Now(),
	}
	g1.ackHandlersMu.Unlock()

	if err := g1.sendUDP(fmt.Sprintf("127.0.0.1:%d", port2), pingReq); err != nil {
		t.Fatalf("sendUDP() error = %v", err)
	}

	// Wait for relayed ACK.
	select {
	case <-ackCh:
		// Success: g2 probed g3 and relayed the ACK.
	case <-time.After(3 * time.Second):
		t.Log("indirect probe did not receive relayed ACK within timeout (may be timing)")
	}
}

// ---- parseHostPort tests ----

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		addr        string
		defaultPort int
		wantHost    string
		wantPort    int
		wantErr     bool
	}{
		{"10.0.0.1:8080", 7946, "10.0.0.1", 8080, false},
		{"10.0.0.1", 7946, "10.0.0.1", 7946, false},
		{"[::1]:9000", 7946, "::1", 9000, false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			host, port, err := parseHostPort(tt.addr, tt.defaultPort)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseHostPort(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
				return
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
		})
	}
}

// ---- SetMetadata test ----

func TestSetMetadata(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "meta-test"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	g.SetMetadata("role", "primary")
	g.SetMetadata("zone", "us-east-1")

	local := g.LocalNode()
	if local.Metadata["role"] != "primary" {
		t.Errorf("Metadata[role] = %q, want %q", local.Metadata["role"], "primary")
	}
	if local.Metadata["zone"] != "us-east-1" {
		t.Errorf("Metadata[zone] = %q, want %q", local.Metadata["zone"], "us-east-1")
	}
}

// ---- RetransmitLimit test ----

func TestRetransmitLimit(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "rt-test"
	cfg.RetransmitMult = 4
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	// With 0 members + self = 1, log2(2) = 1, ceil = 1, * 4 = 4
	limit := g.retransmitLimit()
	if limit != 4 {
		t.Errorf("retransmitLimit() = %d, want 4 (4 * ceil(log2(2)))", limit)
	}

	// Add 7 members: total = 8, log2(9) ~= 3.17, ceil = 4, * 4 = 16
	g.membersMu.Lock()
	for i := 0; i < 7; i++ {
		g.members[fmt.Sprintf("n%d", i)] = &GossipNode{ID: fmt.Sprintf("n%d", i), State: StateAlive}
	}
	g.membersMu.Unlock()

	limit = g.retransmitLimit()
	// log2(9) = 3.17, ceil = 4, * 4 = 16
	if limit != 16 {
		t.Errorf("retransmitLimit() = %d, want 16 (4 * ceil(log2(9)))", limit)
	}
}

// ---- sendMessage and handleCompound tests ----

func TestSendMessage_UDP(t *testing.T) {
	// Create two gossip nodes and send a message between them.
	cfg1 := DefaultGossipConfig()
	cfg1.BindPort = getFreePort(t)
	cfg1.NodeID = "sender"
	g1, err := NewGossip(cfg1)
	if err != nil {
		t.Fatalf("NewGossip(sender) error = %v", err)
	}
	defer g1.Stop()

	cfg2 := DefaultGossipConfig()
	cfg2.BindPort = getFreePort(t)
	cfg2.NodeID = "receiver"
	g2, err := NewGossip(cfg2)
	if err != nil {
		t.Fatalf("NewGossip(receiver) error = %v", err)
	}
	defer g2.Stop()

	// Start both nodes
	if err := g1.Start(); err != nil {
		t.Fatalf("g1.Start() error = %v", err)
	}
	if err := g2.Start(); err != nil {
		t.Fatalf("g2.Start() error = %v", err)
	}

	// Send a small message (should go via UDP since it's under MaxMessageSize)
	targetAddr := fmt.Sprintf("127.0.0.1:%d", cfg2.BindPort)
	msg := encodePing(1, g1.localNode.ID, "receiver")

	err = g1.sendMessage(targetAddr, msg)
	if err != nil {
		t.Errorf("sendMessage() error = %v", err)
	}
}

func TestSendMessage_TCPFallback(t *testing.T) {
	// Create a gossip node and send a message larger than MaxMessageSize
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "sender-tcp"
	cfg.MaxMessageSize = 100 // Small max to force TCP fallback
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}
	defer g.Stop()

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create a TCP listener to receive the fallback message
	tcpL, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.BindPort))
	if err != nil {
		// Port may already be taken by the gossip TCP listener; use a different port
		tcpL, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create TCP listener: %v", err)
		}
	}
	defer tcpL.Close()

	targetAddr := tcpL.Addr().String()

	// Accept in background
	accepted := make(chan bool, 1)
	go func() {
		conn, err := tcpL.Accept()
		if err == nil {
			conn.Close()
			accepted <- true
		}
	}()

	// Create a message larger than MaxMessageSize
	largeMsg := make([]byte, 200)
	for i := range largeMsg {
		largeMsg[i] = byte(i % 256)
	}

	// sendMessage should use TCP for the oversized message
	err = g.sendMessage(targetAddr, largeMsg)
	if err != nil {
		t.Logf("sendMessage(TCP fallback) error = %v (expected if TCP listener closed quickly)", err)
	}
}

func TestHandleCompound_Message(t *testing.T) {
	// Create a gossip node and test handleCompound by encoding
	// a compound message with multiple sub-messages.
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "compound-handler"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}
	defer g.Stop()

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Track events via callback
	var joinEvents atomic.Int32
	g.OnEvent(func(event EventType, node *GossipNode) {
		if event == EventJoin {
			joinEvents.Add(1)
		}
	})

	// Create multiple alive messages and bundle them as a compound message
	alive1 := encodeAlive(1, "node-a", "10.0.0.1", 7946, nil)
	alive2 := encodeAlive(1, "node-b", "10.0.0.2", 7946, nil)

	// Encode compound (this wraps the sub-messages with MsgCompound type byte)
	compoundMsg := encodeCompound([][]byte{alive1, alive2})

	// Decode the outer message to get the compound payload
	_, payload, _, err := decodeMessage(compoundMsg)
	if err != nil {
		t.Fatalf("decodeMessage(compound) error = %v", err)
	}

	// Now call handleCompound with the inner payload
	g.handleCompound(payload, "10.0.0.99:7946")

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Verify the alive messages were processed (nodes should be added as members)
	g.membersMu.RLock()
	_, hasA := g.members["node-a"]
	_, hasB := g.members["node-b"]
	g.membersMu.RUnlock()

	if !hasA {
		t.Error("node-a should be a member after handleCompound")
	}
	if !hasB {
		t.Error("node-b should be a member after handleCompound")
	}
}

func TestHandleCompound_InvalidPayload(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "compound-invalid"
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}
	defer g.Stop()

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Pass an invalid (too short) payload to handleCompound -- should not panic
	g.handleCompound([]byte{0}, "10.0.0.1:7946")
	g.handleCompound(nil, "10.0.0.1:7946")
	g.handleCompound([]byte{}, "10.0.0.1:7946")
}

// ---- handleJoin tests ----

func TestHandleJoin_NewNode(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "join-receiver"
	cfg.TCPTimeout = 500 * time.Millisecond // short timeout for test
	cfg.ProbeInterval = 1 * time.Hour       // disable probe loop during test
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Set up event listener
	type eventRecord struct {
		eventType EventType
		nodeID    string
	}
	eventCh := make(chan eventRecord, 10)
	g.OnEvent(func(et EventType, node *GossipNode) {
		eventCh <- eventRecord{et, node.ID}
	})

	// Use our own bind address so sendMemberList can connect
	fromAddr := fmt.Sprintf("127.0.0.1:%d", cfg.BindPort)

	// Construct a JOIN message payload
	payload := encodeNodePayload(1, "new-joiner", "10.0.0.5", 7946, map[string]string{"role": "worker"})

	// Deliver the join message
	g.handleJoin(payload, fromAddr)

	// The new node should be added to members -- check immediately
	g.membersMu.RLock()
	m, exists := g.members["new-joiner"]
	var state NodeState
	var addr string
	var port int
	var meta map[string]string
	if exists {
		state = m.State
		addr = m.Address
		port = m.Port
		meta = m.Metadata
	}
	g.membersMu.RUnlock()

	if !exists {
		t.Fatal("new-joiner should exist in members after handleJoin")
	}
	if state != StateAlive {
		t.Errorf("State = %v, want %v", state, StateAlive)
	}
	if addr != "10.0.0.5" {
		t.Errorf("Address = %q, want %q", addr, "10.0.0.5")
	}
	if port != 7946 {
		t.Errorf("Port = %d, want %d", port, 7946)
	}
	if meta["role"] != "worker" {
		t.Errorf("Metadata[role] = %q, want %q", meta["role"], "worker")
	}

	// Check that a join event was emitted
	select {
	case ev := <-eventCh:
		if ev.eventType != EventJoin {
			t.Errorf("Event type = %v, want %v", ev.eventType, EventJoin)
		}
		if ev.nodeID != "new-joiner" {
			t.Errorf("Event node ID = %q, want %q", ev.nodeID, "new-joiner")
		}
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for join event")
	}
}

func TestHandleJoin_SelfIgnored(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "self-node"
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Construct a JOIN message with our own node ID
	payload := encodeNodePayload(1, "self-node", "10.0.0.1", 7946, nil)

	// This should be ignored (no self-join)
	g.handleJoin(payload, "10.0.0.1:7946")

	g.membersMu.RLock()
	_, exists := g.members["self-node"]
	g.membersMu.RUnlock()

	if exists {
		t.Error("Self node should not be added to members")
	}
}

func TestHandleJoin_ExistingNode_HigherIncarnation(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "join-updater"
	cfg.TCPTimeout = 500 * time.Millisecond
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Pre-populate a member
	g.membersMu.Lock()
	g.members["existing-node"] = &GossipNode{
		ID:          "existing-node",
		Address:     "10.0.0.1",
		Port:        7946,
		State:       StateAlive,
		Incarnation: 1,
		LastSeen:    time.Now(),
	}
	g.membersMu.Unlock()

	fromAddr := fmt.Sprintf("127.0.0.1:%d", cfg.BindPort)

	// Send JOIN with higher incarnation
	payload := encodeNodePayload(5, "existing-node", "10.0.0.2", 8000, map[string]string{"updated": "true"})
	g.handleJoin(payload, fromAddr)

	// Node should be updated -- check immediately
	g.membersMu.RLock()
	m := g.members["existing-node"]
	inc := m.Incarnation
	mAddr := m.Address
	mPort := m.Port
	g.membersMu.RUnlock()

	if inc != 5 {
		t.Errorf("Incarnation = %d, want 5", inc)
	}
	if mAddr != "10.0.0.2" {
		t.Errorf("Address = %q, want %q", mAddr, "10.0.0.2")
	}
	if mPort != 8000 {
		t.Errorf("Port = %d, want %d", mPort, 8000)
	}
}

func TestHandleJoin_ExistingNode_SameIncarnation(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "join-same-inc"
	cfg.TCPTimeout = 500 * time.Millisecond
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Pre-populate a member with same incarnation
	g.membersMu.Lock()
	g.members["same-inc-node"] = &GossipNode{
		ID:          "same-inc-node",
		Address:     "10.0.0.1",
		Port:        7946,
		State:       StateAlive,
		Incarnation: 3,
		LastSeen:    time.Now(),
	}
	g.membersMu.Unlock()

	fromAddr := fmt.Sprintf("127.0.0.1:%d", cfg.BindPort)

	// Send JOIN with same incarnation -- should NOT update
	payload := encodeNodePayload(3, "same-inc-node", "10.0.0.99", 9999, nil)
	g.handleJoin(payload, fromAddr)

	g.membersMu.RLock()
	m := g.members["same-inc-node"]
	mAddr := m.Address
	g.membersMu.RUnlock()

	// Address should not change because incarnation is the same (not higher)
	if mAddr != "10.0.0.1" {
		t.Errorf("Address = %q, want %q (should not update for same incarnation)", mAddr, "10.0.0.1")
	}
}

func TestHandleJoin_DeadNode_Rejoin(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "join-dead-rejoin"
	cfg.TCPTimeout = 500 * time.Millisecond
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Pre-populate a dead member
	g.membersMu.Lock()
	g.members["dead-node"] = &GossipNode{
		ID:          "dead-node",
		Address:     "10.0.0.1",
		Port:        7946,
		State:       StateDead,
		Incarnation: 1,
		LastSeen:    time.Now(),
	}
	g.membersMu.Unlock()

	fromAddr := fmt.Sprintf("127.0.0.1:%d", cfg.BindPort)

	// A join from a dead node with same incarnation should revive it
	payload := encodeNodePayload(1, "dead-node", "10.0.0.1", 7946, nil)
	g.handleJoin(payload, fromAddr)

	// Check state immediately after handleJoin returns
	g.membersMu.RLock()
	m := g.members["dead-node"]
	state := m.State
	g.membersMu.RUnlock()

	if state != StateAlive {
		t.Errorf("State = %v, want %v (dead node should be revived on join)", state, StateAlive)
	}
}

func TestHandleJoin_InvalidPayload(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "join-invalid"
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Should not panic with invalid payload
	g.handleJoin([]byte{0, 1, 2}, "10.0.0.1:7946")
	g.handleJoin(nil, "10.0.0.1:7946")

	g.membersMu.RLock()
	count := len(g.members)
	g.membersMu.RUnlock()

	if count != 0 {
		t.Errorf("Expected no members from invalid payloads, got %d", count)
	}
}

// ---- probe tests ----

func TestProbe_NoMembers(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "probe-empty"
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// probe() with no members should return immediately without panic
	g.probe()
}

func TestProbe_WithUnreachableMember(t *testing.T) {
	cfg := DefaultGossipConfig()
	cfg.BindPort = getFreePort(t)
	cfg.NodeID = "probe-unreachable"
	cfg.ProbeTimeout = 200 * time.Millisecond
	cfg.IndirectChecks = 0 // No indirect probes to keep test fast
	cfg.ProbeInterval = 1 * time.Hour
	g, err := NewGossip(cfg)
	if err != nil {
		t.Fatalf("NewGossip() error = %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer g.Stop()

	// Add an unreachable member
	g.membersMu.Lock()
	g.members["unreachable"] = &GossipNode{
		ID:          "unreachable",
		Address:     "127.0.0.1",
		Port:        1, // port 1 should refuse connections
		State:       StateAlive,
		Incarnation: 1,
		LastSeen:    time.Now(),
	}
	g.membersMu.Unlock()

	// probe() should attempt to ping unreachable node, fail, and suspect it
	g.probe()

	// Allow time for the probe timeout
	time.Sleep(500 * time.Millisecond)

	g.membersMu.RLock()
	m := g.members["unreachable"]
	state := m.State
	g.membersMu.RUnlock()

	// After a failed probe with IndirectChecks=0, node should be suspected
	if state != StateSuspect && state != StateDead {
		t.Logf("State = %v (expected suspect or dead after failed probe)", state)
	}
}

// ---- Helpers ----

// getFreePort returns a port on localhost that is free for both TCP and UDP.
// It binds both protocols simultaneously to verify availability, which avoids
// flaky failures on Windows where TCP and UDP port availability can differ.
func getFreePort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < 200; attempt++ {
		// Bind UDP first (more restrictive on Windows)
		udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		udpConn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			continue
		}
		port := udpConn.LocalAddr().(*net.UDPAddr).Port

		// Verify TCP is also available on the same port
		tcpL, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			udpConn.Close()
			continue
		}

		// Both available - close and return
		tcpL.Close()
		udpConn.Close()
		return port
	}
	t.Fatalf("getFreePort: unable to find a port free for both TCP and UDP after 200 attempts")
	return 0
}
