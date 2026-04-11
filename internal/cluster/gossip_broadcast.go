package cluster

import (
	"math"
	"sort"
	"time"
)

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
	for i := range n {
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
