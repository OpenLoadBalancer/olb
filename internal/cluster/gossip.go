// Package cluster implements the SWIM gossip protocol for decentralized cluster
// membership and failure detection. It provides eventually consistent membership
// views without requiring a central coordinator.
//
// The protocol uses a combination of direct probes (PING/ACK), indirect probes
// (PING_REQ), and piggybacked state broadcasts to detect node failures and
// disseminate membership changes efficiently.
package cluster

import (
	"fmt"
	"math/rand"
	"net"
	"time"
)

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
	g.localMu.Lock()
	g.localNode.State = StateLeft
	g.localMu.Unlock()

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
	g.localMu.Lock()
	g.localNode.Metadata[key] = value
	g.localMu.Unlock()
}

// copyMetadata returns a safe copy of a metadata map for use outside locks.
func copyMetadata(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func (g *Gossip) nextSeqNo() uint32 {
	return g.seqNo.Add(1)
}

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
