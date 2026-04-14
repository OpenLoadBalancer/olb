package cluster

import (
	"time"
)

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
	ack, _ := encodeAck(seqNo, g.localNode.ID)
	if ack == nil {
		return
	}
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

	// Notify the pending ack handler.
	g.ackHandlersMu.Lock()
	ah, ok := g.ackHandlers[seqNo]
	if ok {
		delete(g.ackHandlers, seqNo)
		ah.timer.Stop()
		select {
		case ah.ackCh <- struct{}{}:
		default:
		}
	}
	g.ackHandlersMu.Unlock()

	if ok {

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
	ping, _ := encodePing(probeSeq, g.localNode.ID, targetID)
	if ping == nil {
		return
	}

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
			ack, _ := encodeAck(seqNo, targetID)
			if ack != nil {
				g.sendUDP(from, ack)
			}
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
		g.localMu.Lock()
		if incarnation >= g.localNode.Incarnation {
			g.localNode.Incarnation = incarnation + 1
			alive, _ := encodeAlive(g.localNode.Incarnation, g.localNode.ID,
				g.localNode.Address, g.localNode.Port, copyMetadata(g.localNode.Metadata))
			g.localMu.Unlock()
			if alive != nil {
				g.queueBroadcast(alive)
			}
		} else {
			g.localMu.Unlock()
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
		g.localMu.Lock()
		g.localNode.Incarnation = incarnation + 1
		alive, _ := encodeAlive(g.localNode.Incarnation, g.localNode.ID,
			g.localNode.Address, g.localNode.Port, copyMetadata(g.localNode.Metadata))
		g.localMu.Unlock()
		if alive != nil {
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
	alive, _ := encodeAlive(incarnation, nodeID, address, port, metadata)
	if alive != nil {
		g.queueBroadcast(alive)
	}
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
	g.localMu.RLock()
	if alive, err := encodeAlive(
		g.localNode.Incarnation, g.localNode.ID,
		g.localNode.Address, g.localNode.Port, copyMetadata(g.localNode.Metadata),
	); err == nil {
		messages = append(messages, alive)
	}
	g.localMu.RUnlock()

	for _, m := range g.members {
		if m.State == StateAlive || m.State == StateSuspect {
			if alive, err := encodeAlive(
				m.Incarnation, m.ID, m.Address, m.Port, m.Metadata,
			); err == nil {
				messages = append(messages, alive)
			}
		}
	}
	g.membersMu.RUnlock()

	if len(messages) > 0 {
		compound, _ := encodeCompound(messages)
		if compound != nil {
			_ = g.sendTCP(addr, compound)
		}
	}
}
