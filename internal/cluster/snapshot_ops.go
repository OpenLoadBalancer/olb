package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// --------------------------------------------------------------------------
// Cluster snapshot / restore / log compaction
// --------------------------------------------------------------------------

// CreateSnapshot triggers a snapshot of the state machine and stores it. The
// log is then compacted up to the snapshot index.
func (c *Cluster) CreateSnapshot() (*Snapshot, error) {
	if c.stateMachine == nil {
		return nil, errors.New("no state machine configured")
	}

	data, err := c.stateMachine.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("state machine snapshot: %w", err)
	}

	c.logMu.RLock()
	lastIndex := c.getLastLogIndexLocked()
	lastTerm := c.getLastLogTermLocked()
	c.logMu.RUnlock()

	snapshot := &Snapshot{
		LastIncludedIndex: lastIndex,
		LastIncludedTerm:  lastTerm,
		Data:              data,
		Metadata: map[string]string{
			"node_id":    c.config.NodeID,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		},
	}

	// Compact the log.
	c.compactLog(lastIndex)

	return snapshot, nil
}

// RestoreSnapshot restores the state machine from a snapshot and resets the
// Raft log.
func (c *Cluster) RestoreSnapshot(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}
	if c.stateMachine == nil {
		return errors.New("no state machine configured")
	}

	if err := c.stateMachine.Restore(snapshot.Data); err != nil {
		return fmt.Errorf("restore state machine: %w", err)
	}

	// Reset log.
	c.logMu.Lock()
	c.log = make([]*LogEntry, 0)
	c.logMu.Unlock()

	// Update indices.
	c.commitIndex.Store(snapshot.LastIncludedIndex)
	c.lastApplied.Store(snapshot.LastIncludedIndex)

	// Update term if the snapshot is from a later term.
	if snapshot.LastIncludedTerm > c.GetTerm() {
		c.currentTerm.Store(snapshot.LastIncludedTerm)
	}

	return nil
}

// compactLog removes all log entries with index <= compactIndex.
func (c *Cluster) compactLog(compactIndex uint64) {
	c.logMu.Lock()
	defer c.logMu.Unlock()

	newLog := make([]*LogEntry, 0)
	for _, entry := range c.log {
		if entry.Index > compactIndex {
			newLog = append(newLog, entry)
		}
	}
	c.log = newLog
}

// getLastLogIndexLocked returns the last log index. Caller must hold logMu.
func (c *Cluster) getLastLogIndexLocked() uint64 {
	if len(c.log) == 0 {
		return 0
	}
	return c.log[len(c.log)-1].Index
}

// getLastLogTerm returns the term of the last log entry (thread-safe).
func (c *Cluster) getLastLogTerm() uint64 {
	c.logMu.RLock()
	defer c.logMu.RUnlock()
	return c.getLastLogTermLocked()
}

// getLastLogTermLocked returns the term of the last log entry. Caller must
// hold logMu.
func (c *Cluster) getLastLogTermLocked() uint64 {
	if len(c.log) == 0 {
		return 0
	}
	return c.log[len(c.log)-1].Term
}

// --------------------------------------------------------------------------
// InstallSnapshot RPC
// --------------------------------------------------------------------------

// InstallSnapshotRequest is the RPC sent by a leader to a lagging follower.
type InstallSnapshotRequest struct {
	Term              uint64 `json:"term"`
	LeaderID          string `json:"leader_id"`
	LastIncludedIndex uint64 `json:"last_included_index"`
	LastIncludedTerm  uint64 `json:"last_included_term"`
	Data              []byte `json:"data"`
}

// InstallSnapshotResponse is the follower's reply.
type InstallSnapshotResponse struct {
	Term    uint64 `json:"term"`
	Success bool   `json:"success"`
}

// HandleInstallSnapshot processes an InstallSnapshot RPC from the leader.
func (c *Cluster) HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse {
	if req.Term < c.GetTerm() {
		return &InstallSnapshotResponse{
			Term:    c.GetTerm(),
			Success: false,
		}
	}

	// Step down if we receive a higher term.
	if req.Term > c.GetTerm() {
		c.currentTerm.Store(req.Term)
		c.setState(StateFollower)
		c.votedFor.Store("")
	}

	c.leaderID.Store(req.LeaderID)
	c.resetElectionTimer()

	// Apply the snapshot.
	snapshot := &Snapshot{
		LastIncludedIndex: req.LastIncludedIndex,
		LastIncludedTerm:  req.LastIncludedTerm,
		Data:              req.Data,
	}

	if err := c.RestoreSnapshot(snapshot); err != nil {
		return &InstallSnapshotResponse{
			Term:    c.GetTerm(),
			Success: false,
		}
	}

	return &InstallSnapshotResponse{
		Term:    c.GetTerm(),
		Success: true,
	}
}

// ShouldSendSnapshot determines whether the leader should send a snapshot
// instead of log entries. This returns true when the follower's nextIndex is
// behind our earliest available log entry (i.e., it was compacted away).
func (c *Cluster) ShouldSendSnapshot(followerNextIndex uint64) bool {
	c.logMu.RLock()
	defer c.logMu.RUnlock()

	if len(c.log) == 0 {
		return false
	}

	earliestIndex := c.log[0].Index
	return followerNextIndex < earliestIndex
}

// BuildInstallSnapshotRequest builds an InstallSnapshotRequest from the
// current state. It is used by the leader when a follower is too far behind.
func (c *Cluster) BuildInstallSnapshotRequest() (*InstallSnapshotRequest, error) {
	snapshot, err := c.CreateSnapshot()
	if err != nil {
		return nil, err
	}

	return &InstallSnapshotRequest{
		Term:              c.GetTerm(),
		LeaderID:          c.config.NodeID,
		LastIncludedIndex: snapshot.LastIncludedIndex,
		LastIncludedTerm:  snapshot.LastIncludedTerm,
		Data:              snapshot.Data,
	}, nil
}

// --------------------------------------------------------------------------
// Membership changes (joint consensus)
// --------------------------------------------------------------------------

// ChangeType describes the kind of membership change.
type ChangeType int

const (
	// AddNode adds a new node to the cluster.
	AddNode ChangeType = iota
	// RemoveNode removes a node from the cluster.
	RemoveNode
)

// String returns a human-readable representation.
func (ct ChangeType) String() string {
	switch ct {
	case AddNode:
		return "AddNode"
	case RemoveNode:
		return "RemoveNode"
	default:
		return "Unknown"
	}
}

// MembershipChange describes a proposed membership change.
type MembershipChange struct {
	Type    ChangeType `json:"type"`
	NodeID  string     `json:"node_id"`
	Address string     `json:"address"`
}

// MembershipChangeEntry is a log entry that encodes a membership change. It
// is serialised as the Command field of a LogEntry.
type MembershipChangeEntry struct {
	Phase  string           `json:"phase"` // "joint" or "final"
	Change MembershipChange `json:"change"`
}

// membershipConfig tracks the membership state during joint consensus.
type membershipConfig struct {
	mu            sync.RWMutex
	pending       *MembershipChange
	inTransition  bool
	jointCommitID uint64 // log index of the C_old,new entry
}

// ProposeMembershipChange proposes adding or removing a node. The change goes
// through two phases (joint consensus):
//
//  1. C_old,new — the joint configuration is written to the log.
//  2. C_new     — once committed, the final configuration is written.
//
// Only one membership change may be in progress at a time.
func (c *Cluster) ProposeMembershipChange(change MembershipChange) error {
	if c.GetState() != StateLeader {
		return fmt.Errorf("not leader, current leader is %s", c.GetLeader())
	}

	c.memberMu.Lock()
	if c.membership.inTransition {
		c.memberMu.Unlock()
		return errors.New("membership change already in progress")
	}
	c.membership.inTransition = true
	c.membership.pending = &change
	c.memberMu.Unlock()

	// Phase 1: propose the joint configuration.
	entry := MembershipChangeEntry{
		Phase:  "joint",
		Change: change,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("marshal membership change: %w", err)
	}

	result, err := c.Propose(data)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("propose joint config: %w", err)
	}
	if result.Error != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("apply joint config: %w", result.Error)
	}

	c.memberMu.Lock()
	c.membership.jointCommitID = result.Index
	c.memberMu.Unlock()

	// Apply the membership change immediately (Phase 1 effect).
	c.applyMembershipChange(&change)

	// Phase 2: commit the final configuration.
	finalEntry := MembershipChangeEntry{
		Phase:  "final",
		Change: change,
	}

	finalData, err := json.Marshal(finalEntry)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("marshal final config: %w", err)
	}

	finalResult, err := c.Propose(finalData)
	if err != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("propose final config: %w", err)
	}
	if finalResult.Error != nil {
		c.clearMembershipTransition()
		return fmt.Errorf("apply final config: %w", finalResult.Error)
	}

	c.clearMembershipTransition()
	return nil
}

// applyMembershipChange actually adds or removes the node.
func (c *Cluster) applyMembershipChange(change *MembershipChange) {
	switch change.Type {
	case AddNode:
		c.AddNode(change.NodeID, change.Address)
	case RemoveNode:
		c.RemoveNode(change.NodeID)
	}
}

// clearMembershipTransition resets the in-progress flag.
func (c *Cluster) clearMembershipTransition() {
	c.memberMu.Lock()
	c.membership.inTransition = false
	c.membership.pending = nil
	c.membership.jointCommitID = 0
	c.memberMu.Unlock()
}

// IsMembershipChangeInProgress reports whether a membership change is pending.
func (c *Cluster) IsMembershipChangeInProgress() bool {
	c.memberMu.RLock()
	defer c.memberMu.RUnlock()
	return c.membership.inTransition
}
