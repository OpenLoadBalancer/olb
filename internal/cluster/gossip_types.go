// Package cluster implements the SWIM gossip protocol for decentralized cluster
// membership and failure detection.
package cluster

import (
	"fmt"
	"math/rand"
	"net"
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
	localMu   sync.RWMutex // protects localNode fields (Incarnation, State, Metadata)

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
