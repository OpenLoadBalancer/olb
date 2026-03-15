// Package cluster implements the SWIM gossip protocol for decentralized cluster
// membership and failure detection. It provides eventually consistent membership
// views without requiring a central coordinator.
//
// The protocol uses a combination of direct probes (PING/ACK), indirect probes
// (PING_REQ), and piggybacked state broadcasts to detect node failures and
// disseminate membership changes efficiently.
package cluster

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// NodeState represents the state of a node in the cluster.
type NodeState int32

const (
	// StateAlive indicates the node is healthy and reachable.
	StateAlive NodeState = iota
	// StateSuspect indicates the node may have failed.
	StateSuspect
	// StateDead indicates the node has been confirmed dead.
	StateDead
	// StateLeft indicates the node has gracefully left the cluster.
	StateLeft
)

// String returns the string representation of the node state.
func (s NodeState) String() string {
	switch s {
	case StateAlive:
		return "alive"
	case StateSuspect:
		return "suspect"
	case StateDead:
		return "dead"
	case StateLeft:
		return "left"
	default:
		return "unknown"
	}
}

// MessageType identifies the type of gossip message.
type MessageType uint8

const (
	// MsgPing is a direct health probe.
	MsgPing MessageType = iota + 1
	// MsgAck is a response to a PING.
	MsgAck
	// MsgPingReq is an indirect probe request.
	MsgPingReq
	// MsgSuspect marks a node as suspected failed.
	MsgSuspect
	// MsgAlive marks a node as alive (can refute suspicion).
	MsgAlive
	// MsgDead marks a node as dead.
	MsgDead
	// MsgJoin announces a new node joining the cluster.
	MsgJoin
	// MsgLeave announces a node gracefully leaving.
	MsgLeave
	// MsgCompound is a compound message containing multiple messages.
	MsgCompound
)

// String returns the string representation of the message type.
func (m MessageType) String() string {
	switch m {
	case MsgPing:
		return "PING"
	case MsgAck:
		return "ACK"
	case MsgPingReq:
		return "PING_REQ"
	case MsgSuspect:
		return "SUSPECT"
	case MsgAlive:
		return "ALIVE"
	case MsgDead:
		return "DEAD"
	case MsgJoin:
		return "JOIN"
	case MsgLeave:
		return "LEAVE"
	case MsgCompound:
		return "COMPOUND"
	default:
		return "UNKNOWN"
	}
}

// GossipConfig holds configuration for the gossip protocol.
type GossipConfig struct {
	// BindAddr is the address to bind the UDP listener to.
	BindAddr string
	// BindPort is the port to bind the UDP listener to.
	BindPort int
	// ProbeInterval is the interval between failure detection probes.
	ProbeInterval time.Duration
	// ProbeTimeout is the timeout for a direct probe response.
	ProbeTimeout time.Duration
	// IndirectChecks is the number of indirect probe requests to send
	// when a direct probe fails.
	IndirectChecks int
	// SuspicionTimeout is how long a node stays in suspect state
	// before being declared dead.
	SuspicionTimeout time.Duration
	// GossipInterval is the interval between gossip rounds for
	// broadcasting state changes.
	GossipInterval time.Duration
	// GossipNodes is the number of random nodes to gossip to each round.
	GossipNodes int
	// RetransmitMult is the multiplier for the number of retransmissions.
	// Actual count = RetransmitMult * log(N+1).
	RetransmitMult int
	// MaxMessageSize is the maximum size of a UDP message in bytes.
	// Defaults to 1400 to stay within typical MTU.
	MaxMessageSize int
	// TCPTimeout is the timeout for TCP fallback connections.
	TCPTimeout time.Duration
	// DeadNodeReapInterval is the interval for reaping dead nodes from
	// the member list.
	DeadNodeReapInterval time.Duration
	// NodeID is the unique identifier for this node.
	NodeID string
}

// DefaultGossipConfig returns a GossipConfig with sane defaults.
func DefaultGossipConfig() *GossipConfig {
	return &GossipConfig{
		BindAddr:             "0.0.0.0",
		BindPort:             7946,
		ProbeInterval:        1 * time.Second,
		ProbeTimeout:         500 * time.Millisecond,
		IndirectChecks:       3,
		SuspicionTimeout:     5 * time.Second,
		GossipInterval:       200 * time.Millisecond,
		GossipNodes:          3,
		RetransmitMult:       4,
		MaxMessageSize:       1400,
		TCPTimeout:           10 * time.Second,
		DeadNodeReapInterval: 30 * time.Second,
	}
}

// Validate checks the configuration for errors.
func (c *GossipConfig) Validate() error {
	if c.BindPort <= 0 || c.BindPort > 65535 {
		return fmt.Errorf("gossip: invalid bind port: %d", c.BindPort)
	}
	if c.ProbeInterval <= 0 {
		return fmt.Errorf("gossip: probe interval must be positive")
	}
	if c.ProbeTimeout <= 0 {
		return fmt.Errorf("gossip: probe timeout must be positive")
	}
	if c.IndirectChecks < 0 {
		return fmt.Errorf("gossip: indirect checks must be non-negative")
	}
	if c.SuspicionTimeout <= 0 {
		return fmt.Errorf("gossip: suspicion timeout must be positive")
	}
	if c.MaxMessageSize <= 0 {
		return fmt.Errorf("gossip: max message size must be positive")
	}
	return nil
}

// GossipNode represents a member of the gossip cluster.
type GossipNode struct {
	// ID is the unique identifier for this node.
	ID string
	// Address is the IP address or hostname.
	Address string
	// Port is the gossip protocol port.
	Port int
	// State is the current membership state.
	State NodeState
	// Incarnation is the logical clock for state precedence.
	Incarnation uint32
	// LastSeen is the last time this node was confirmed alive.
	LastSeen time.Time
	// Metadata holds arbitrary key-value data about the node.
	Metadata map[string]string
}

// Addr returns the address in host:port format.
func (n *GossipNode) Addr() string {
	return fmt.Sprintf("%s:%d", n.Address, n.Port)
}

// Clone returns a deep copy of the node.
func (n *GossipNode) Clone() *GossipNode {
	clone := &GossipNode{
		ID:          n.ID,
		Address:     n.Address,
		Port:        n.Port,
		State:       n.State,
		Incarnation: n.Incarnation,
		LastSeen:    n.LastSeen,
	}
	if n.Metadata != nil {
		clone.Metadata = make(map[string]string, len(n.Metadata))
		for k, v := range n.Metadata {
			clone.Metadata[k] = v
		}
	}
	return clone
}

// EventType classifies membership events.
type EventType int

const (
	// EventJoin indicates a node joined the cluster.
	EventJoin EventType = iota
	// EventLeave indicates a node left the cluster.
	EventLeave
	// EventUpdate indicates a node's state changed.
	EventUpdate
)

// EventHandler is a callback for membership changes.
type EventHandler func(event EventType, node *GossipNode)

// message is the internal representation of a gossip message.
type message struct {
	Type        MessageType
	SeqNo       uint32
	SenderID    string
	TargetID    string
	Incarnation uint32
	NodeID      string
	Address     string
	Port        uint16
	Metadata    map[string]string
}

// broadcast represents a queued broadcast with retransmit tracking.
type broadcast struct {
	msg        []byte
	retransmit int
	created    time.Time
}

// ackHandler tracks a pending ACK response.
type ackHandler struct {
	seqNo   uint32
	timer   *time.Timer
	ackCh   chan struct{}
	created time.Time
}

// Gossip implements the SWIM gossip protocol for membership management.
type Gossip struct {
	config *GossipConfig

	// Local node information.
	localNode *GossipNode

	// Members tracked by this node, keyed by node ID.
	members   map[string]*GossipNode
	membersMu sync.RWMutex

	// UDP transport.
	udpConn *net.UDPConn

	// TCP listener for large messages.
	tcpListener net.Listener

	// Sequence number for PING/ACK correlation.
	seqNo atomic.Uint32

	// Pending ACK handlers, keyed by sequence number.
	ackHandlers   map[uint32]*ackHandler
	ackHandlersMu sync.Mutex

	// Broadcast queue for piggybacking state changes.
	broadcasts   []*broadcast
	broadcastsMu sync.Mutex

	// Event handlers for membership changes.
	eventHandlers   []EventHandler
	eventHandlersMu sync.RWMutex

	// Suspicion timers, keyed by node ID.
	suspicionTimers   map[string]*time.Timer
	suspicionTimersMu sync.Mutex

	// Lifecycle management.
	stopCh chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool

	// Random source — protected by rngMu for concurrent access.
	rng   *rand.Rand
	rngMu sync.Mutex

	// nowFn allows injecting a clock for testing.
	nowFn func() time.Time
}

// NewGossip creates a new Gossip instance with the given configuration.
// Call Start() to begin the protocol.
func NewGossip(config *GossipConfig) (*Gossip, error) {
	if config == nil {
		config = DefaultGossipConfig()
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	nodeID := config.NodeID
	if nodeID == "" {
		nodeID = fmt.Sprintf("%s:%d", config.BindAddr, config.BindPort)
	}

	g := &Gossip{
		config: config,
		localNode: &GossipNode{
			ID:       nodeID,
			Address:  config.BindAddr,
			Port:     config.BindPort,
			State:    StateAlive,
			LastSeen: time.Now(),
			Metadata: make(map[string]string),
		},
		members:         make(map[string]*GossipNode),
		ackHandlers:     make(map[uint32]*ackHandler),
		suspicionTimers: make(map[string]*time.Timer),
		stopCh:          make(chan struct{}),
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())),
		nowFn:           time.Now,
	}

	return g, nil
}

// Start begins the gossip protocol by binding to the network and starting
// background goroutines for probing, gossip, and TCP listening.
func (g *Gossip) Start() error {
	// Bind UDP socket.
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", g.config.BindAddr, g.config.BindPort))
	if err != nil {
		return fmt.Errorf("gossip: resolve UDP address: %w", err)
	}
	g.udpConn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("gossip: listen UDP: %w", err)
	}

	// Bind TCP socket on the same port for large message fallback.
	tcpAddr := fmt.Sprintf("%s:%d", g.config.BindAddr, g.config.BindPort)
	g.tcpListener, err = net.Listen("tcp", tcpAddr)
	if err != nil {
		g.udpConn.Close()
		return fmt.Errorf("gossip: listen TCP: %w", err)
	}

	// Start background goroutines.
	g.wg.Add(4)
	go g.udpReadLoop()
	go g.tcpAcceptLoop()
	go g.probeLoop()
	go g.gossipLoop()

	return nil
}

// Stop gracefully shuts down the gossip protocol.
func (g *Gossip) Stop() error {
	if !g.closed.CompareAndSwap(false, true) {
		return nil // already stopped
	}
	close(g.stopCh)

	if g.udpConn != nil {
		g.udpConn.Close()
	}
	if g.tcpListener != nil {
		g.tcpListener.Close()
	}

	// Cancel all pending ACK handlers.
	g.ackHandlersMu.Lock()
	for _, ah := range g.ackHandlers {
		ah.timer.Stop()
		close(ah.ackCh)
	}
	g.ackHandlers = make(map[uint32]*ackHandler)
	g.ackHandlersMu.Unlock()

	// Cancel all suspicion timers.
	g.suspicionTimersMu.Lock()
	for _, t := range g.suspicionTimers {
		t.Stop()
	}
	g.suspicionTimers = make(map[string]*time.Timer)
	g.suspicionTimersMu.Unlock()

	g.wg.Wait()
	return nil
}

// Join attempts to join an existing cluster by contacting the given addresses.
// Each address should be in "host:port" format.
func (g *Gossip) Join(existing []string) error {
	if len(existing) == 0 {
		return nil
	}

	var lastErr error
	joined := 0

	for _, addr := range existing {
		host, port, err := parseHostPort(addr, g.config.BindPort)
		if err != nil {
			lastErr = err
			continue
		}

		// Send a JOIN message via TCP for reliability.
		msg := g.encodeJoinMessage()
		if err := g.sendTCP(fmt.Sprintf("%s:%d", host, port), msg); err != nil {
			lastErr = err
			continue
		}
		joined++
	}

	if joined == 0 && lastErr != nil {
		return fmt.Errorf("gossip: failed to join cluster: %w", lastErr)
	}
	return nil
}

// Leave gracefully leaves the cluster by broadcasting a LEAVE message.
func (g *Gossip) Leave() error {
	g.localNode.State = StateLeft

	// Broadcast LEAVE to all known members.
	msg := g.encodeLeaveMessage(g.localNode)
	g.queueBroadcast(msg)

	// Send directly to all members for faster propagation.
	g.membersMu.RLock()
	members := make([]*GossipNode, 0, len(g.members))
	for _, m := range g.members {
		if m.State == StateAlive || m.State == StateSuspect {
			members = append(members, m)
		}
	}
	g.membersMu.RUnlock()

	for _, m := range members {
		_ = g.sendUDP(m.Addr(), msg)
	}

	return nil
}

// Members returns a snapshot of the current membership list.
// Only alive and suspect nodes are included.
func (g *Gossip) Members() []*GossipNode {
	g.membersMu.RLock()
	defer g.membersMu.RUnlock()

	result := make([]*GossipNode, 0, len(g.members))
	for _, m := range g.members {
		if m.State == StateAlive || m.State == StateSuspect {
			result = append(result, m.Clone())
		}
	}
	return result
}

// AllMembers returns all members including dead/left nodes.
func (g *Gossip) AllMembers() []*GossipNode {
	g.membersMu.RLock()
	defer g.membersMu.RUnlock()

	result := make([]*GossipNode, 0, len(g.members)+1)
	result = append(result, g.localNode.Clone())
	for _, m := range g.members {
		result = append(result, m.Clone())
	}
	return result
}

// NumMembers returns the number of alive/suspect members (excluding self).
func (g *Gossip) NumMembers() int {
	g.membersMu.RLock()
	defer g.membersMu.RUnlock()

	count := 0
	for _, m := range g.members {
		if m.State == StateAlive || m.State == StateSuspect {
			count++
		}
	}
	return count
}

// LocalNode returns a copy of the local node.
func (g *Gossip) LocalNode() *GossipNode {
	return g.localNode.Clone()
}

// OnEvent registers a callback for membership events.
func (g *Gossip) OnEvent(handler EventHandler) {
	g.eventHandlersMu.Lock()
	defer g.eventHandlersMu.Unlock()
	g.eventHandlers = append(g.eventHandlers, handler)
}

// SetMetadata sets metadata on the local node.
func (g *Gossip) SetMetadata(key, value string) {
	g.membersMu.Lock()
	g.localNode.Metadata[key] = value
	g.membersMu.Unlock()
}

// ---- Binary message encoding/decoding ----

// Message wire format:
//   [type: 1 byte][length: 2 bytes][payload: variable]
//
// Payload varies by message type. All multi-byte integers are big-endian.

// encodeMessage creates a wire-format message from type and payload.
func encodeMessage(msgType MessageType, payload []byte) []byte {
	buf := make([]byte, 3+len(payload))
	buf[0] = byte(msgType)
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(payload)))
	copy(buf[3:], payload)
	return buf
}

// decodeMessage splits a wire-format message into type, payload, and remaining bytes.
func decodeMessage(data []byte) (MessageType, []byte, []byte, error) {
	if len(data) < 3 {
		return 0, nil, nil, fmt.Errorf("gossip: message too short: %d bytes", len(data))
	}
	msgType := MessageType(data[0])
	length := binary.BigEndian.Uint16(data[1:3])
	if int(length) > len(data)-3 {
		return 0, nil, nil, fmt.Errorf("gossip: message payload truncated: want %d, have %d", length, len(data)-3)
	}
	payload := data[3 : 3+length]
	remaining := data[3+length:]
	return msgType, payload, remaining, nil
}

// PING payload: [seqNo: 4][senderIDLen: 2][senderID: var][targetIDLen: 2][targetID: var]
func encodePing(seqNo uint32, senderID, targetID string) []byte {
	payload := make([]byte, 4+2+len(senderID)+2+len(targetID))
	binary.BigEndian.PutUint32(payload[0:4], seqNo)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(senderID)))
	copy(payload[6:6+len(senderID)], senderID)
	off := 6 + len(senderID)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(targetID)))
	copy(payload[off+2:], targetID)
	return encodeMessage(MsgPing, payload)
}

// decodePing parses a PING payload.
func decodePing(payload []byte) (seqNo uint32, senderID, targetID string, err error) {
	if len(payload) < 8 {
		return 0, "", "", fmt.Errorf("gossip: ping payload too short")
	}
	seqNo = binary.BigEndian.Uint32(payload[0:4])
	senderLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(senderLen)+2 {
		return 0, "", "", fmt.Errorf("gossip: ping payload truncated at sender")
	}
	senderID = string(payload[6 : 6+senderLen])
	off := 6 + int(senderLen)
	targetLen := binary.BigEndian.Uint16(payload[off : off+2])
	if len(payload) < off+2+int(targetLen) {
		return 0, "", "", fmt.Errorf("gossip: ping payload truncated at target")
	}
	targetID = string(payload[off+2 : off+2+int(targetLen)])
	return seqNo, senderID, targetID, nil
}

// ACK payload: [seqNo: 4][senderIDLen: 2][senderID: var]
func encodeAck(seqNo uint32, senderID string) []byte {
	payload := make([]byte, 4+2+len(senderID))
	binary.BigEndian.PutUint32(payload[0:4], seqNo)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(senderID)))
	copy(payload[6:], senderID)
	return encodeMessage(MsgAck, payload)
}

// decodeAck parses an ACK payload.
func decodeAck(payload []byte) (seqNo uint32, senderID string, err error) {
	if len(payload) < 6 {
		return 0, "", fmt.Errorf("gossip: ack payload too short")
	}
	seqNo = binary.BigEndian.Uint32(payload[0:4])
	senderLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(senderLen) {
		return 0, "", fmt.Errorf("gossip: ack payload truncated")
	}
	senderID = string(payload[6 : 6+senderLen])
	return seqNo, senderID, nil
}

// PING_REQ payload: [seqNo: 4][senderIDLen: 2][senderID: var][targetIDLen: 2][targetID: var]
func encodePingReq(seqNo uint32, senderID, targetID string) []byte {
	payload := make([]byte, 4+2+len(senderID)+2+len(targetID))
	binary.BigEndian.PutUint32(payload[0:4], seqNo)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(senderID)))
	copy(payload[6:6+len(senderID)], senderID)
	off := 6 + len(senderID)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(targetID)))
	copy(payload[off+2:], targetID)
	return encodeMessage(MsgPingReq, payload)
}

// decodePingReq parses a PING_REQ payload.
func decodePingReq(payload []byte) (seqNo uint32, senderID, targetID string, err error) {
	return decodePing(payload) // same format
}

// encodeNodePayload encodes a node's identity into a byte slice.
// Format: [incarnation: 4][nodeIDLen: 2][nodeID: var][addrLen: 2][addr: var][port: 2][metaCount: 2][{keyLen: 2, key, valLen: 2, val}...]
func encodeNodePayload(incarnation uint32, nodeID, address string, port int, metadata map[string]string) []byte {
	size := 4 + 2 + len(nodeID) + 2 + len(address) + 2 + 2
	for k, v := range metadata {
		size += 2 + len(k) + 2 + len(v)
	}
	payload := make([]byte, size)
	binary.BigEndian.PutUint32(payload[0:4], incarnation)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(nodeID)))
	copy(payload[6:6+len(nodeID)], nodeID)
	off := 6 + len(nodeID)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(address)))
	copy(payload[off+2:off+2+len(address)], address)
	off += 2 + len(address)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(port))
	off += 2
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(metadata)))
	off += 2
	for k, v := range metadata {
		binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(k)))
		copy(payload[off+2:off+2+len(k)], k)
		off += 2 + len(k)
		binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(v)))
		copy(payload[off+2:off+2+len(v)], v)
		off += 2 + len(v)
	}
	return payload
}

// decodeNodePayload decodes a node identity payload.
func decodeNodePayload(payload []byte) (incarnation uint32, nodeID, address string, port int, metadata map[string]string, err error) {
	if len(payload) < 12 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload too short")
	}
	incarnation = binary.BigEndian.Uint32(payload[0:4])
	nodeIDLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(nodeIDLen)+2 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at nodeID")
	}
	nodeID = string(payload[6 : 6+nodeIDLen])
	off := 6 + int(nodeIDLen)
	addrLen := binary.BigEndian.Uint16(payload[off : off+2])
	if len(payload) < off+2+int(addrLen)+2+2 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at address")
	}
	address = string(payload[off+2 : off+2+int(addrLen)])
	off += 2 + int(addrLen)
	port = int(binary.BigEndian.Uint16(payload[off : off+2]))
	off += 2
	if len(payload) < off+2 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta count")
	}
	metaCount := binary.BigEndian.Uint16(payload[off : off+2])
	off += 2
	metadata = make(map[string]string, metaCount)
	for i := 0; i < int(metaCount); i++ {
		if len(payload) < off+2 {
			return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta key len")
		}
		kLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(kLen)+2 {
			return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta key")
		}
		k := string(payload[off : off+int(kLen)])
		off += int(kLen)
		vLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(vLen) {
			return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta value")
		}
		v := string(payload[off : off+int(vLen)])
		off += int(vLen)
		metadata[k] = v
	}
	return incarnation, nodeID, address, port, metadata, nil
}

// encodeSuspect encodes a SUSPECT message.
func encodeSuspect(incarnation uint32, nodeID string) []byte {
	payload := make([]byte, 4+2+len(nodeID))
	binary.BigEndian.PutUint32(payload[0:4], incarnation)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(nodeID)))
	copy(payload[6:], nodeID)
	return encodeMessage(MsgSuspect, payload)
}

// decodeSuspect parses a SUSPECT payload.
func decodeSuspect(payload []byte) (incarnation uint32, nodeID string, err error) {
	if len(payload) < 6 {
		return 0, "", fmt.Errorf("gossip: suspect payload too short")
	}
	incarnation = binary.BigEndian.Uint32(payload[0:4])
	nodeIDLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(nodeIDLen) {
		return 0, "", fmt.Errorf("gossip: suspect payload truncated")
	}
	nodeID = string(payload[6 : 6+nodeIDLen])
	return incarnation, nodeID, nil
}

// encodeAlive encodes an ALIVE message with full node info.
func encodeAlive(incarnation uint32, nodeID, address string, port int, metadata map[string]string) []byte {
	return encodeMessage(MsgAlive, encodeNodePayload(incarnation, nodeID, address, port, metadata))
}

// decodeAlive parses an ALIVE payload.
func decodeAlive(payload []byte) (incarnation uint32, nodeID, address string, port int, metadata map[string]string, err error) {
	return decodeNodePayload(payload)
}

// encodeDead encodes a DEAD message.
func encodeDead(incarnation uint32, nodeID string) []byte {
	payload := make([]byte, 4+2+len(nodeID))
	binary.BigEndian.PutUint32(payload[0:4], incarnation)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(nodeID)))
	copy(payload[6:], nodeID)
	return encodeMessage(MsgDead, payload)
}

// decodeDead parses a DEAD payload.
func decodeDead(payload []byte) (incarnation uint32, nodeID string, err error) {
	return decodeSuspect(payload) // same format
}

// encodeJoinMessage encodes a JOIN message for the local node.
func (g *Gossip) encodeJoinMessage() []byte {
	return encodeMessage(MsgJoin, encodeNodePayload(
		g.localNode.Incarnation,
		g.localNode.ID,
		g.localNode.Address,
		g.localNode.Port,
		g.localNode.Metadata,
	))
}

// encodeLeaveMessage encodes a LEAVE message for a node.
func (g *Gossip) encodeLeaveMessage(node *GossipNode) []byte {
	return encodeMessage(MsgLeave, encodeNodePayload(
		node.Incarnation,
		node.ID,
		node.Address,
		node.Port,
		node.Metadata,
	))
}

// encodeCompound wraps multiple messages into a single compound message.
func encodeCompound(messages [][]byte) []byte {
	// Compound payload: [count: 2][{msgLen: 2, msg}...]
	total := 2
	for _, m := range messages {
		total += 2 + len(m)
	}
	payload := make([]byte, total)
	binary.BigEndian.PutUint16(payload[0:2], uint16(len(messages)))
	off := 2
	for _, m := range messages {
		binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(m)))
		copy(payload[off+2:off+2+len(m)], m)
		off += 2 + len(m)
	}
	return encodeMessage(MsgCompound, payload)
}

// decodeCompound splits a compound payload into individual messages.
func decodeCompound(payload []byte) ([][]byte, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("gossip: compound payload too short")
	}
	count := binary.BigEndian.Uint16(payload[0:2])
	off := 2
	messages := make([][]byte, 0, count)
	for i := 0; i < int(count); i++ {
		if len(payload) < off+2 {
			return nil, fmt.Errorf("gossip: compound truncated at message %d length", i)
		}
		msgLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(msgLen) {
			return nil, fmt.Errorf("gossip: compound truncated at message %d data", i)
		}
		messages = append(messages, payload[off:off+int(msgLen)])
		off += int(msgLen)
	}
	return messages, nil
}

// ---- Transport ----

// sendUDP sends raw bytes via UDP.
func (g *Gossip) sendUDP(addr string, msg []byte) error {
	if g.udpConn == nil {
		return fmt.Errorf("gossip: UDP connection not initialized")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	_, err = g.udpConn.WriteToUDP(msg, udpAddr)
	return err
}

// sendTCP sends raw bytes via TCP with the configured timeout.
func (g *Gossip) sendTCP(addr string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, g.config.TCPTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(g.nowFn().Add(g.config.TCPTimeout))
	// Write length-prefixed message: [totalLen: 4][msg]
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(msg)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err = conn.Write(msg)
	return err
}

// sendMessage sends a message via UDP, falling back to TCP for oversized messages.
func (g *Gossip) sendMessage(addr string, msg []byte) error {
	if len(msg) > g.config.MaxMessageSize {
		return g.sendTCP(addr, msg)
	}
	return g.sendUDP(addr, msg)
}

// ---- Background loops ----

// udpReadLoop reads messages from the UDP socket.
func (g *Gossip) udpReadLoop() {
	defer g.wg.Done()
	buf := make([]byte, 65536)
	for {
		select {
		case <-g.stopCh:
			return
		default:
		}
		g.udpConn.SetReadDeadline(g.nowFn().Add(1 * time.Second))
		n, from, err := g.udpConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-g.stopCh:
				return
			default:
				continue
			}
		}
		// Make a copy so we don't race on the buffer.
		data := make([]byte, n)
		copy(data, buf[:n])
		g.handleMessage(data, from.String())
	}
}

// tcpAcceptLoop accepts TCP connections for large messages.
func (g *Gossip) tcpAcceptLoop() {
	defer g.wg.Done()
	for {
		select {
		case <-g.stopCh:
			return
		default:
		}
		conn, err := g.tcpListener.Accept()
		if err != nil {
			select {
			case <-g.stopCh:
				return
			default:
				continue
			}
		}
		go g.handleTCPConn(conn)
	}
}

// handleTCPConn reads a length-prefixed message from a TCP connection.
func (g *Gossip) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(g.nowFn().Add(g.config.TCPTimeout))

	// Read length prefix.
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	length := binary.BigEndian.Uint32(header)
	if length > 10*1024*1024 { // 10MB safety limit
		return
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return
	}
	g.handleMessage(data, conn.RemoteAddr().String())
}

// handleMessage dispatches a received message by type.
func (g *Gossip) handleMessage(data []byte, from string) {
	msgType, payload, remaining, err := decodeMessage(data)
	if err != nil {
		return
	}

	switch msgType {
	case MsgPing:
		g.handlePing(payload, from)
	case MsgAck:
		g.handleAck(payload)
	case MsgPingReq:
		g.handlePingReq(payload, from)
	case MsgSuspect:
		g.handleSuspect(payload)
	case MsgAlive:
		g.handleAlive(payload)
	case MsgDead:
		g.handleDead(payload)
	case MsgJoin:
		g.handleJoin(payload, from)
	case MsgLeave:
		g.handleLeaveMsg(payload)
	case MsgCompound:
		g.handleCompound(payload, from)
	}

	// Process piggybacked messages in remaining bytes.
	for len(remaining) >= 3 {
		msgType, payload, remaining, err = decodeMessage(remaining)
		if err != nil {
			break
		}
		switch msgType {
		case MsgSuspect:
			g.handleSuspect(payload)
		case MsgAlive:
			g.handleAlive(payload)
		case MsgDead:
			g.handleDead(payload)
		case MsgJoin:
			g.handleJoin(payload, from)
		case MsgLeave:
			g.handleLeaveMsg(payload)
		}
	}
}

// handlePing responds to a PING with an ACK, attaching piggybacked broadcasts.
func (g *Gossip) handlePing(payload []byte, from string) {
	seqNo, senderID, _, err := decodePing(payload)
	if err != nil {
		return
	}

	// Mark sender as alive.
	g.markNodeAlive(senderID, from)

	// Build response: ACK + piggybacked broadcasts.
	ack := encodeAck(seqNo, g.localNode.ID)
	broadcasts := g.getBroadcasts(g.config.MaxMessageSize - len(ack))
	response := ack
	for _, b := range broadcasts {
		response = append(response, b...)
	}

	// Send ACK back to sender.
	g.sendUDP(from, response)
}

// handleAck processes an ACK response.
func (g *Gossip) handleAck(payload []byte) {
	seqNo, senderID, err := decodeAck(payload)
	if err != nil {
		return
	}

	// Mark sender as alive.
	g.membersMu.RLock()
	if m, ok := g.members[senderID]; ok {
		_ = m // will be updated below
	}
	g.membersMu.RUnlock()

	// Notify the pending ack handler.
	g.ackHandlersMu.Lock()
	ah, ok := g.ackHandlers[seqNo]
	if ok {
		delete(g.ackHandlers, seqNo)
	}
	g.ackHandlersMu.Unlock()

	if ok {
		ah.timer.Stop()
		select {
		case ah.ackCh <- struct{}{}:
		default:
		}
	}

	// Refresh the sender's alive status.
	g.membersMu.Lock()
	if m, ok := g.members[senderID]; ok {
		if m.State == StateSuspect {
			m.State = StateAlive
			g.cancelSuspicion(senderID)
			g.emitEvent(EventUpdate, m)
		}
		m.LastSeen = g.nowFn()
	}
	g.membersMu.Unlock()
}

// handlePingReq handles an indirect probe request. We ping the target on
// behalf of the requester and relay the ACK back.
func (g *Gossip) handlePingReq(payload []byte, from string) {
	seqNo, senderID, targetID, err := decodePingReq(payload)
	if err != nil {
		return
	}
	_ = senderID

	// Look up the target node.
	g.membersMu.RLock()
	target, ok := g.members[targetID]
	g.membersMu.RUnlock()
	if !ok {
		return
	}

	// Probe the target directly.
	probeSeq := g.nextSeqNo()
	ping := encodePing(probeSeq, g.localNode.ID, targetID)

	ackCh := make(chan struct{}, 1)
	timer := time.AfterFunc(g.config.ProbeTimeout, func() {
		g.ackHandlersMu.Lock()
		delete(g.ackHandlers, probeSeq)
		g.ackHandlersMu.Unlock()
	})

	g.ackHandlersMu.Lock()
	g.ackHandlers[probeSeq] = &ackHandler{
		seqNo:   probeSeq,
		timer:   timer,
		ackCh:   ackCh,
		created: g.nowFn(),
	}
	g.ackHandlersMu.Unlock()

	if err := g.sendUDP(target.Addr(), ping); err != nil {
		return
	}

	// Wait for ACK from target, then relay to original requester.
	go func() {
		select {
		case <-ackCh:
			// Target responded; relay ACK to requester.
			ack := encodeAck(seqNo, targetID)
			g.sendUDP(from, ack)
		case <-time.After(g.config.ProbeTimeout):
			// No response; do nothing. The requester will handle suspicion.
		case <-g.stopCh:
		}
	}()
}

// handleSuspect processes a SUSPECT message.
func (g *Gossip) handleSuspect(payload []byte) {
	incarnation, nodeID, err := decodeSuspect(payload)
	if err != nil {
		return
	}

	// If we are being suspected, refute by incrementing incarnation.
	if nodeID == g.localNode.ID {
		if incarnation >= g.localNode.Incarnation {
			g.localNode.Incarnation = incarnation + 1
			alive := encodeAlive(g.localNode.Incarnation, g.localNode.ID,
				g.localNode.Address, g.localNode.Port, g.localNode.Metadata)
			g.queueBroadcast(alive)
		}
		return
	}

	g.membersMu.Lock()
	defer g.membersMu.Unlock()

	m, ok := g.members[nodeID]
	if !ok {
		return
	}

	// SUSPECT with >= incarnation overrides ALIVE.
	if m.State == StateAlive && incarnation >= m.Incarnation {
		m.State = StateSuspect
		m.Incarnation = incarnation
		g.startSuspicion(nodeID)
		g.emitEvent(EventUpdate, m)
	}
}

// handleAlive processes an ALIVE message.
func (g *Gossip) handleAlive(payload []byte) {
	incarnation, nodeID, address, port, metadata, err := decodeAlive(payload)
	if err != nil {
		return
	}

	// Ignore messages about ourselves.
	if nodeID == g.localNode.ID {
		return
	}

	g.membersMu.Lock()
	defer g.membersMu.Unlock()

	m, ok := g.members[nodeID]
	if !ok {
		// New node.
		m = &GossipNode{
			ID:          nodeID,
			Address:     address,
			Port:        port,
			State:       StateAlive,
			Incarnation: incarnation,
			LastSeen:    g.nowFn(),
			Metadata:    metadata,
		}
		g.members[nodeID] = m
		g.emitEvent(EventJoin, m)
		return
	}

	// ALIVE with higher incarnation overrides SUSPECT.
	if incarnation > m.Incarnation {
		oldState := m.State
		m.State = StateAlive
		m.Incarnation = incarnation
		m.Address = address
		m.Port = port
		m.LastSeen = g.nowFn()
		if metadata != nil {
			m.Metadata = metadata
		}
		if oldState == StateSuspect || oldState == StateDead {
			g.cancelSuspicion(nodeID)
			g.emitEvent(EventUpdate, m)
		}
	}
}

// handleDead processes a DEAD message.
func (g *Gossip) handleDead(payload []byte) {
	incarnation, nodeID, err := decodeDead(payload)
	if err != nil {
		return
	}

	// If we are declared dead, refute.
	if nodeID == g.localNode.ID {
		g.localNode.Incarnation = incarnation + 1
		alive := encodeAlive(g.localNode.Incarnation, g.localNode.ID,
			g.localNode.Address, g.localNode.Port, g.localNode.Metadata)
		g.queueBroadcast(alive)
		return
	}

	g.membersMu.Lock()
	defer g.membersMu.Unlock()

	m, ok := g.members[nodeID]
	if !ok {
		return
	}

	// DEAD overrides everything for same or higher incarnation.
	if incarnation >= m.Incarnation && m.State != StateDead && m.State != StateLeft {
		m.State = StateDead
		m.Incarnation = incarnation
		g.cancelSuspicion(nodeID)
		g.emitEvent(EventLeave, m)
	}
}

// handleJoin processes a JOIN message from a new node.
func (g *Gossip) handleJoin(payload []byte, from string) {
	incarnation, nodeID, address, port, metadata, err := decodeNodePayload(payload)
	if err != nil {
		return
	}

	if nodeID == g.localNode.ID {
		return
	}

	g.membersMu.Lock()
	m, exists := g.members[nodeID]
	if !exists {
		m = &GossipNode{
			ID:          nodeID,
			Address:     address,
			Port:        port,
			State:       StateAlive,
			Incarnation: incarnation,
			LastSeen:    g.nowFn(),
			Metadata:    metadata,
		}
		g.members[nodeID] = m
		g.membersMu.Unlock()
		g.emitEvent(EventJoin, m)
	} else {
		if incarnation > m.Incarnation || m.State == StateDead || m.State == StateLeft {
			m.State = StateAlive
			m.Incarnation = incarnation
			m.Address = address
			m.Port = port
			m.LastSeen = g.nowFn()
			if metadata != nil {
				m.Metadata = metadata
			}
			g.cancelSuspicion(nodeID)
			g.membersMu.Unlock()
			g.emitEvent(EventUpdate, m)
		} else {
			g.membersMu.Unlock()
		}
	}

	// Respond with our membership list so the joiner gets the full picture.
	g.sendMemberList(from)

	// Broadcast the join to existing members.
	alive := encodeAlive(incarnation, nodeID, address, port, metadata)
	g.queueBroadcast(alive)
}

// handleLeaveMsg processes a LEAVE message.
func (g *Gossip) handleLeaveMsg(payload []byte) {
	_, nodeID, _, _, _, err := decodeNodePayload(payload)
	if err != nil {
		return
	}

	if nodeID == g.localNode.ID {
		return
	}

	g.membersMu.Lock()
	m, ok := g.members[nodeID]
	if ok && m.State != StateLeft {
		m.State = StateLeft
		g.cancelSuspicion(nodeID)
		g.membersMu.Unlock()
		g.emitEvent(EventLeave, m)
	} else {
		g.membersMu.Unlock()
	}
}

// handleCompound processes a compound message by dispatching each sub-message.
func (g *Gossip) handleCompound(payload []byte, from string) {
	messages, err := decodeCompound(payload)
	if err != nil {
		return
	}
	for _, msg := range messages {
		g.handleMessage(msg, from)
	}
}

// sendMemberList sends our full membership to the given address via TCP.
func (g *Gossip) sendMemberList(addr string) {
	g.membersMu.RLock()
	var messages [][]byte

	// Include ourselves.
	messages = append(messages, encodeAlive(
		g.localNode.Incarnation, g.localNode.ID,
		g.localNode.Address, g.localNode.Port, g.localNode.Metadata,
	))

	for _, m := range g.members {
		if m.State == StateAlive || m.State == StateSuspect {
			messages = append(messages, encodeAlive(
				m.Incarnation, m.ID, m.Address, m.Port, m.Metadata,
			))
		}
	}
	g.membersMu.RUnlock()

	if len(messages) > 0 {
		compound := encodeCompound(messages)
		_ = g.sendTCP(addr, compound)
	}
}

// ---- Probe (failure detection) ----

// probeLoop is the main failure detection loop.
func (g *Gossip) probeLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(g.config.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.probe()
		}
	}
}

// probe selects a random member and probes it.
func (g *Gossip) probe() {
	target := g.randomMember()
	if target == nil {
		return
	}

	seqNo := g.nextSeqNo()
	acked := g.probeNode(target, seqNo)

	if acked {
		return
	}

	// Direct probe failed; try indirect probes.
	indirectTargets := g.randomMembers(g.config.IndirectChecks, target.ID)
	if len(indirectTargets) == 0 {
		g.suspectNode(target)
		return
	}

	ackCh := make(chan struct{}, len(indirectTargets))
	for _, peer := range indirectTargets {
		pingReq := encodePingReq(seqNo, g.localNode.ID, target.ID)
		if err := g.sendUDP(peer.Addr(), pingReq); err != nil {
			continue
		}
	}

	// Re-use the seqNo; any ACK with this seqNo from the target means it is alive.
	g.ackHandlersMu.Lock()
	ah := &ackHandler{
		seqNo:   seqNo,
		ackCh:   ackCh,
		created: g.nowFn(),
	}
	ah.timer = time.AfterFunc(g.config.ProbeTimeout, func() {
		g.ackHandlersMu.Lock()
		delete(g.ackHandlers, seqNo)
		g.ackHandlersMu.Unlock()
	})
	g.ackHandlers[seqNo] = ah
	g.ackHandlersMu.Unlock()

	// Wait for indirect ACK.
	select {
	case <-ackCh:
		// Got indirect ACK; node is alive.
		return
	case <-time.After(g.config.ProbeTimeout):
		// No indirect ACK; suspect the node.
		g.suspectNode(target)
	case <-g.stopCh:
	}
}

// probeNode sends a direct PING to a node and waits for an ACK.
func (g *Gossip) probeNode(target *GossipNode, seqNo uint32) bool {
	ackCh := make(chan struct{}, 1)
	timer := time.AfterFunc(g.config.ProbeTimeout, func() {
		g.ackHandlersMu.Lock()
		delete(g.ackHandlers, seqNo)
		g.ackHandlersMu.Unlock()
	})

	g.ackHandlersMu.Lock()
	g.ackHandlers[seqNo] = &ackHandler{
		seqNo:   seqNo,
		timer:   timer,
		ackCh:   ackCh,
		created: g.nowFn(),
	}
	g.ackHandlersMu.Unlock()

	// Build PING with piggybacked broadcasts.
	ping := encodePing(seqNo, g.localNode.ID, target.ID)
	broadcasts := g.getBroadcasts(g.config.MaxMessageSize - len(ping))
	msg := ping
	for _, b := range broadcasts {
		msg = append(msg, b...)
	}

	if err := g.sendUDP(target.Addr(), msg); err != nil {
		return false
	}

	select {
	case <-ackCh:
		return true
	case <-time.After(g.config.ProbeTimeout):
		return false
	case <-g.stopCh:
		return false
	}
}

// suspectNode transitions a node to the suspect state.
func (g *Gossip) suspectNode(node *GossipNode) {
	g.membersMu.Lock()
	m, ok := g.members[node.ID]
	if !ok || m.State != StateAlive {
		g.membersMu.Unlock()
		return
	}
	m.State = StateSuspect
	g.startSuspicion(node.ID)
	g.membersMu.Unlock()

	// Broadcast suspicion.
	suspect := encodeSuspect(m.Incarnation, m.ID)
	g.queueBroadcast(suspect)
	g.emitEvent(EventUpdate, m)
}

// markNodeAlive updates a node's last seen time when we hear from it.
func (g *Gossip) markNodeAlive(nodeID, addr string) {
	if nodeID == g.localNode.ID {
		return
	}
	g.membersMu.Lock()
	if m, ok := g.members[nodeID]; ok {
		m.LastSeen = g.nowFn()
	}
	g.membersMu.Unlock()
}

// startSuspicion starts a timer that will declare the node dead after SuspicionTimeout.
// Must be called without holding suspicionTimersMu.
func (g *Gossip) startSuspicion(nodeID string) {
	g.suspicionTimersMu.Lock()
	defer g.suspicionTimersMu.Unlock()

	// Cancel existing timer if any.
	if t, ok := g.suspicionTimers[nodeID]; ok {
		t.Stop()
	}

	g.suspicionTimers[nodeID] = time.AfterFunc(g.config.SuspicionTimeout, func() {
		g.membersMu.Lock()
		m, ok := g.members[nodeID]
		if ok && m.State == StateSuspect {
			m.State = StateDead
			g.membersMu.Unlock()

			dead := encodeDead(m.Incarnation, m.ID)
			g.queueBroadcast(dead)
			g.emitEvent(EventLeave, m)
		} else {
			g.membersMu.Unlock()
		}

		g.suspicionTimersMu.Lock()
		delete(g.suspicionTimers, nodeID)
		g.suspicionTimersMu.Unlock()
	})
}

// cancelSuspicion cancels the suspicion timer for a node.
// Must NOT hold suspicionTimersMu.
func (g *Gossip) cancelSuspicion(nodeID string) {
	g.suspicionTimersMu.Lock()
	defer g.suspicionTimersMu.Unlock()
	if t, ok := g.suspicionTimers[nodeID]; ok {
		t.Stop()
		delete(g.suspicionTimers, nodeID)
	}
}

// ---- Gossip (broadcast dissemination) ----

// gossipLoop periodically disseminates queued broadcasts.
func (g *Gossip) gossipLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(g.config.GossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.gossip()
		}
	}
}

// gossip selects random members and sends them queued broadcasts.
func (g *Gossip) gossip() {
	targets := g.randomMembers(g.config.GossipNodes, "")
	if len(targets) == 0 {
		return
	}

	broadcasts := g.getBroadcasts(g.config.MaxMessageSize)
	if len(broadcasts) == 0 {
		return
	}

	// Build a compound message from the broadcasts.
	var msg []byte
	for _, b := range broadcasts {
		msg = append(msg, b...)
	}

	for _, target := range targets {
		g.sendUDP(target.Addr(), msg)
	}
}

// queueBroadcast adds a message to the broadcast queue.
func (g *Gossip) queueBroadcast(msg []byte) {
	g.broadcastsMu.Lock()
	defer g.broadcastsMu.Unlock()

	retransmit := g.retransmitLimit()
	g.broadcasts = append(g.broadcasts, &broadcast{
		msg:        msg,
		retransmit: retransmit,
		created:    g.nowFn(),
	})
}

// getBroadcasts returns queued broadcasts that fit within the size limit.
// It decrements retransmit counters and removes exhausted entries.
func (g *Gossip) getBroadcasts(limit int) [][]byte {
	g.broadcastsMu.Lock()
	defer g.broadcastsMu.Unlock()

	if len(g.broadcasts) == 0 {
		return nil
	}

	// Sort by newest first (higher priority).
	sort.Slice(g.broadcasts, func(i, j int) bool {
		return g.broadcasts[i].created.After(g.broadcasts[j].created)
	})

	var result [][]byte
	used := 0
	remaining := make([]*broadcast, 0, len(g.broadcasts))

	for _, b := range g.broadcasts {
		if used+len(b.msg) <= limit {
			result = append(result, b.msg)
			used += len(b.msg)
			b.retransmit--
			if b.retransmit > 0 {
				remaining = append(remaining, b)
			}
		} else {
			remaining = append(remaining, b)
		}
	}

	g.broadcasts = remaining
	return result
}

// retransmitLimit calculates the number of times a broadcast should be retransmitted.
func (g *Gossip) retransmitLimit() int {
	g.membersMu.RLock()
	n := len(g.members) + 1
	g.membersMu.RUnlock()
	return g.config.RetransmitMult * int(math.Ceil(math.Log2(float64(n)+1)))
}

// ---- Member selection ----

// randomMember returns a random alive or suspect member, or nil.
func (g *Gossip) randomMember() *GossipNode {
	g.membersMu.RLock()
	defer g.membersMu.RUnlock()

	eligible := make([]*GossipNode, 0, len(g.members))
	for _, m := range g.members {
		if m.State == StateAlive || m.State == StateSuspect {
			eligible = append(eligible, m)
		}
	}

	if len(eligible) == 0 {
		return nil
	}
	g.rngMu.Lock()
	idx := g.rng.Intn(len(eligible))
	g.rngMu.Unlock()
	return eligible[idx].Clone()
}

// randomMembers returns up to n random alive/suspect members, excluding excludeID.
func (g *Gossip) randomMembers(n int, excludeID string) []*GossipNode {
	g.membersMu.RLock()
	defer g.membersMu.RUnlock()

	eligible := make([]*GossipNode, 0, len(g.members))
	for _, m := range g.members {
		if (m.State == StateAlive || m.State == StateSuspect) && m.ID != excludeID {
			eligible = append(eligible, m)
		}
	}

	if len(eligible) == 0 {
		return nil
	}

	// Fisher-Yates shuffle and take first n.
	g.rngMu.Lock()
	g.rng.Shuffle(len(eligible), func(i, j int) {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	})
	g.rngMu.Unlock()

	if n > len(eligible) {
		n = len(eligible)
	}

	result := make([]*GossipNode, n)
	for i := 0; i < n; i++ {
		result[i] = eligible[i].Clone()
	}
	return result
}

// ---- Event emission ----

// emitEvent calls all registered event handlers.
func (g *Gossip) emitEvent(eventType EventType, node *GossipNode) {
	g.eventHandlersMu.RLock()
	handlers := make([]EventHandler, len(g.eventHandlers))
	copy(handlers, g.eventHandlers)
	g.eventHandlersMu.RUnlock()

	clone := node.Clone()
	for _, h := range handlers {
		h(eventType, clone)
	}
}

// ---- Helpers ----

// nextSeqNo returns the next sequence number.
func (g *Gossip) nextSeqNo() uint32 {
	return g.seqNo.Add(1)
}

// parseHostPort parses a host:port string, using defaultPort if no port is specified.
func parseHostPort(addr string, defaultPort int) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// No port specified; use default.
		return addr, defaultPort, nil
	}
	port := 0
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return "", 0, fmt.Errorf("gossip: invalid port in address %q", addr)
		}
		port = port*10 + int(c-'0')
	}
	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("gossip: port out of range in address %q", addr)
	}
	return host, port, nil
}
