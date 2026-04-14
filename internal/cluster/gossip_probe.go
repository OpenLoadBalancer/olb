package cluster

import (
	"time"
)

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
		pingReq, _ := encodePingReq(seqNo, g.localNode.ID, target.ID)
		if pingReq == nil {
			continue
		}
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
	ping, _ := encodePing(seqNo, g.localNode.ID, target.ID)
	if ping == nil {
		return false
	}
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
	suspect, _ := encodeSuspect(m.Incarnation, m.ID)
	if suspect != nil {
		g.queueBroadcast(suspect)
	}
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

			dead, _ := encodeDead(m.Incarnation, m.ID)
			if dead != nil {
				g.queueBroadcast(dead)
			}
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
