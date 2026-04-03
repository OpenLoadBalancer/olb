// Package cluster provides distributed clustering and consensus using Raft.
package cluster

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the state of a node in the cluster.
type State string

const (
	// StateFollower is a follower node.
	StateFollower State = "follower"
	// StateCandidate is a candidate node (during election).
	StateCandidate State = "candidate"
	// StateLeader is the leader node.
	StateLeader State = "leader"
)

// Config contains cluster configuration.
type Config struct {
	NodeID        string        `json:"node_id" yaml:"node_id"`
	BindAddr      string        `json:"bind_addr" yaml:"bind_addr"`
	BindPort      int           `json:"bind_port" yaml:"bind_port"`
	Peers         []string      `json:"peers" yaml:"peers"` // List of peer addresses
	ElectionTick  time.Duration `json:"election_tick" yaml:"election_tick"`
	HeartbeatTick time.Duration `json:"heartbeat_tick" yaml:"heartbeat_tick"`
	DataDir       string        `json:"data_dir" yaml:"data_dir"`
}

// DefaultConfig returns a default cluster configuration.
func DefaultConfig() *Config {
	return &Config{
		BindPort:      7946,
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.NodeID == "" {
		return errors.New("node ID is required")
	}
	if c.BindAddr == "" {
		c.BindAddr = "0.0.0.0"
	}
	if c.BindPort <= 0 {
		c.BindPort = 7946
	}
	if c.ElectionTick <= 0 {
		c.ElectionTick = 2 * time.Second
	}
	if c.HeartbeatTick <= 0 {
		c.HeartbeatTick = 500 * time.Millisecond
	}
	return nil
}

// Node represents a node in the cluster.
type Node struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	LastSeen time.Time `json:"last_seen"`
	IsLeader bool      `json:"is_leader"`
}

// LogEntry represents a log entry in the Raft log.
type LogEntry struct {
	Index   uint64    `json:"index"`
	Term    uint64    `json:"term"`
	Command []byte    `json:"command"`
	Applied time.Time `json:"applied"`
}

// Cluster manages the cluster state and Raft consensus.
type Cluster struct {
	config      *Config
	state       atomic.Value // State
	currentTerm atomic.Uint64
	votedFor    atomic.Value // string
	leaderID    atomic.Value // string

	// Log management
	log         []*LogEntry
	logMu       sync.RWMutex
	commitIndex atomic.Uint64
	lastApplied atomic.Uint64

	// Cluster membership
	nodes   map[string]*Node
	nodesMu sync.RWMutex

	// Channels
	electionTimer  *time.Timer
	heartbeatTimer *time.Ticker
	stopCh         chan struct{}
	commandCh      chan *Command
	runDone        chan struct{} // closed when run() goroutine exits

	// State machine
	stateMachine StateMachine

	// Network transport for Raft RPCs (nil = local-only / test mode)
	transport *TCPTransport

	// Callbacks
	onStateChange   func(State, State)
	onLeaderElected func(string)

	// Membership change tracking (joint consensus)
	memberMu   sync.RWMutex
	membership membershipConfig
}

// StateMachine is the interface for the replicated state machine.
type StateMachine interface {
	Apply(command []byte) ([]byte, error)
	Snapshot() ([]byte, error)
	Restore(snapshot []byte) error
}

// Command represents a command to be applied to the state machine.
type Command struct {
	Command []byte
	Result  chan<- *CommandResult
}

// CommandResult represents the result of applying a command.
type CommandResult struct {
	Output []byte
	Error  error
	Index  uint64
	Term   uint64
}

// New creates a new cluster.
func New(config *Config, stateMachine StateMachine) (*Cluster, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	c := &Cluster{
		config:       config,
		log:          make([]*LogEntry, 0),
		nodes:        make(map[string]*Node),
		stopCh:       make(chan struct{}),
		commandCh:    make(chan *Command, 100),
		stateMachine: stateMachine,
	}

	c.state.Store(StateFollower)
	c.votedFor.Store("")
	c.leaderID.Store("")

	// Add self to nodes
	c.nodes[config.NodeID] = &Node{
		ID:       config.NodeID,
		Address:  net.JoinHostPort(config.BindAddr, fmt.Sprintf("%d", config.BindPort)),
		LastSeen: time.Now(),
		IsLeader: false,
	}

	// Add peers
	for _, peer := range config.Peers {
		c.nodes[peer] = &Node{
			ID:       peer,
			Address:  peer,
			LastSeen: time.Now(),
			IsLeader: false,
		}
	}

	return c, nil
}

// Start starts the cluster.
func (c *Cluster) Start() error {
	c.runDone = make(chan struct{})

	// Start election timer
	c.resetElectionTimer()

	// Start processing goroutines
	go c.run()

	return nil
}

// Stop stops the cluster and cleans up resources.
func (c *Cluster) Stop() error {
	close(c.stopCh)

	// Stop timers to prevent goroutine/resource leaks
	if c.electionTimer != nil {
		c.electionTimer.Stop()
	}
	if c.heartbeatTimer != nil {
		c.heartbeatTimer.Stop()
	}

	// Wait for the run() goroutine to exit
	if c.runDone != nil {
		<-c.runDone
	}

	return nil
}

// run is the main processing loop.
func (c *Cluster) run() {
	defer func() {
		if c.runDone != nil {
			close(c.runDone)
		}
	}()
	for {
		select {
		case <-c.stopCh:
			return
		case <-c.getElectionTimerChan():
			c.startElection()
		case <-c.getHeartbeatTimerChan():
			if c.GetState() == StateLeader {
				c.sendHeartbeats()
			}
		case cmd := <-c.commandCh:
			c.handleCommand(cmd)
		}
	}
}

// GetState returns the current state of the node.
func (c *Cluster) GetState() State {
	return c.state.Load().(State)
}

// setState sets the state of the node.
func (c *Cluster) setState(newState State) {
	oldState := c.GetState()
	if oldState != newState {
		c.state.Store(newState)
		if c.onStateChange != nil {
			c.onStateChange(oldState, newState)
		}
	}
}

// GetLeader returns the current leader ID.
func (c *Cluster) GetLeader() string {
	return c.leaderID.Load().(string)
}

// IsLeader returns true if this node is the leader.
func (c *Cluster) IsLeader() bool {
	return c.GetState() == StateLeader
}

// GetTerm returns the current term.
func (c *Cluster) GetTerm() uint64 {
	return c.currentTerm.Load()
}

// incrementTerm increments the current term.
func (c *Cluster) incrementTerm() uint64 {
	return c.currentTerm.Add(1)
}

// resetElectionTimer resets the election timer.
func (c *Cluster) resetElectionTimer() {
	if c.electionTimer != nil {
		c.electionTimer.Stop()
	}

	// Random timeout between 150ms and 300ms (to avoid split votes)
	timeout := 150*time.Millisecond + time.Duration(time.Now().UnixNano()%150)*time.Millisecond
	c.electionTimer = time.NewTimer(timeout)
}

// getElectionTimerChan returns the election timer channel.
func (c *Cluster) getElectionTimerChan() <-chan time.Time {
	if c.electionTimer == nil {
		return nil
	}
	return c.electionTimer.C
}

// getHeartbeatTimerChan returns the heartbeat timer channel.
func (c *Cluster) getHeartbeatTimerChan() <-chan time.Time {
	if c.heartbeatTimer == nil {
		return nil
	}
	return c.heartbeatTimer.C
}

// startElection starts a new election.
func (c *Cluster) startElection() {
	c.setState(StateCandidate)
	term := c.incrementTerm()
	c.votedFor.Store(c.config.NodeID)

	// Request votes from all peers
	votes := 1 // Vote for self
	votesMu := sync.Mutex{}

	lastLogIndex := c.getLastLogIndex()
	lastLogTerm := c.getLastLogTermLocked()

	var wg sync.WaitGroup
	c.nodesMu.RLock()
	peers := make(map[string]string, len(c.nodes))
	for nodeID, node := range c.nodes {
		if nodeID != c.config.NodeID {
			peers[nodeID] = node.Address
		}
	}
	c.nodesMu.RUnlock()

	for _, addr := range peers {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			if c.transport != nil {
				// Send real RPC via TCPTransport
				resp, err := c.transport.SendRequestVote(addr, &RequestVote{
					Term:         term,
					CandidateID:  c.config.NodeID,
					LastLogIndex: lastLogIndex,
					LastLogTerm:  lastLogTerm,
				})
				if err != nil {
					return
				}
				if resp.Term > term {
					c.currentTerm.Store(resp.Term)
					c.setState(StateFollower)
					return
				}
				if resp.VoteGranted {
					votesMu.Lock()
					votes++
					votesMu.Unlock()
				}
			} else {
				// Local/test mode: simulate a successful vote
				votesMu.Lock()
				votes++
				votesMu.Unlock()
			}
		}(addr)
	}

	// Wait for votes with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Check if we won
		if votes > len(c.nodes)/2 {
			c.becomeLeader()
		} else {
			c.setState(StateFollower)
			c.resetElectionTimer()
		}
	case <-time.After(c.config.ElectionTick):
		// Election timeout
		c.setState(StateFollower)
		c.resetElectionTimer()
	}
}

// becomeLeader transitions to leader state.
func (c *Cluster) becomeLeader() {
	c.setState(StateLeader)
	c.leaderID.Store(c.config.NodeID)

	// Stop election timer, start heartbeat timer
	if c.electionTimer != nil {
		c.electionTimer.Stop()
	}
	c.heartbeatTimer = time.NewTicker(c.config.HeartbeatTick)

	// Update node info
	c.nodesMu.Lock()
	for _, node := range c.nodes {
		node.IsLeader = (node.ID == c.config.NodeID)
	}
	c.nodesMu.Unlock()

	if c.onLeaderElected != nil {
		c.onLeaderElected(c.config.NodeID)
	}
}

// sendHeartbeats sends heartbeat messages to all peers.
func (c *Cluster) sendHeartbeats() {
	term := c.GetTerm()
	commitIndex := c.commitIndex.Load()

	// Snapshot peers under lock to avoid race on c.nodes map
	c.nodesMu.RLock()
	peerAddrs := make([]string, 0, len(c.nodes))
	for nodeID, node := range c.nodes {
		if nodeID != c.config.NodeID {
			peerAddrs = append(peerAddrs, node.Address)
		}
	}
	c.nodesMu.RUnlock()

	for _, addr := range peerAddrs {
		go func(addr string) {
			if c.transport != nil {
				resp, err := c.transport.SendAppendEntries(addr, &AppendEntries{
					Term:         term,
					LeaderID:     c.config.NodeID,
					PrevLogIndex: c.getLastLogIndex(),
					PrevLogTerm:  c.getLastLogTermLocked(),
					Entries:      nil, // Empty = heartbeat
					LeaderCommit: commitIndex,
				})
				if err != nil {
					return
				}
				if resp.Term > term {
					c.currentTerm.Store(resp.Term)
					c.setState(StateFollower)
					c.resetElectionTimer()
				}
			}
		}(addr)
	}
}

// handleCommand handles a command to be applied.
// The leader appends the entry to its log, replicates to followers via
// AppendEntries RPCs, waits for majority acknowledgment, then commits and applies.
func (c *Cluster) handleCommand(cmd *Command) {
	if c.GetState() != StateLeader {
		// Forward to leader
		cmd.Result <- &CommandResult{
			Error: fmt.Errorf("not leader, forward to %s", c.GetLeader()),
		}
		return
	}

	// Append to local log
	entry := &LogEntry{
		Index:   c.getLastLogIndex() + 1,
		Term:    c.GetTerm(),
		Command: cmd.Command,
	}

	c.logMu.Lock()
	c.log = append(c.log, entry)
	c.logMu.Unlock()

	// Self-acknowledgment counts as 1
	successCount := 1
	var successMu sync.Mutex

	// Collect peer addresses
	c.nodesMu.RLock()
	type peerInfo struct {
		id      string
		address string
	}
	var peers []peerInfo
	for nodeID, node := range c.nodes {
		if nodeID != c.config.NodeID {
			peers = append(peers, peerInfo{id: nodeID, address: node.Address})
		}
	}
	c.nodesMu.RUnlock()

	// Calculate quorum size (majority of all nodes including self)
	totalNodes := len(peers) + 1
	quorum := totalNodes/2 + 1

	// If single-node cluster, commit immediately
	if len(peers) == 0 {
		output, err := c.stateMachine.Apply(cmd.Command)
		c.commitIndex.Store(entry.Index)
		c.lastApplied.Store(entry.Index)
		cmd.Result <- &CommandResult{
			Output: output,
			Error:  err,
			Index:  entry.Index,
			Term:   entry.Term,
		}
		return
	}

	// Replicate to followers and wait for majority
	replicateDone := make(chan struct{})
	var once sync.Once

	replicatePeer := func(p peerInfo) {
		if c.transport == nil {
			// Local/test mode: simulate successful replication
			successMu.Lock()
			successCount++
			sc := successCount
			successMu.Unlock()
			if sc >= quorum {
				once.Do(func() { close(replicateDone) })
			}
			return
		}

		resp, err := c.transport.SendAppendEntries(p.address, &AppendEntries{
			Term:         c.GetTerm(),
			LeaderID:     c.config.NodeID,
			PrevLogIndex: entry.Index - 1,
			PrevLogTerm:  c.getLastLogTermForIndex(entry.Index - 1),
			Entries:      []*LogEntry{entry},
			LeaderCommit: c.commitIndex.Load(),
		})
		if err != nil {
			return
		}
		if resp.Term > c.GetTerm() {
			c.currentTerm.Store(resp.Term)
			c.setState(StateFollower)
			c.resetElectionTimer()
			return
		}
		if resp.Success {
			successMu.Lock()
			successCount++
			sc := successCount
			successMu.Unlock()
			if sc >= quorum {
				once.Do(func() { close(replicateDone) })
			}
		}
	}

	for _, p := range peers {
		go replicatePeer(p)
	}

	// Wait for quorum with timeout
	select {
	case <-replicateDone:
		// Majority replicated — commit and apply
		output, err := c.stateMachine.Apply(cmd.Command)
		c.commitIndex.Store(entry.Index)
		c.lastApplied.Store(entry.Index)
		cmd.Result <- &CommandResult{
			Output: output,
			Error:  err,
			Index:  entry.Index,
			Term:   entry.Term,
		}
	case <-time.After(5 * time.Second):
		cmd.Result <- &CommandResult{
			Error: errors.New("replication timeout: failed to reach quorum"),
			Index: entry.Index,
			Term:  entry.Term,
		}
	}
}

// getLastLogTermForIndex returns the term of the log entry at the given index.
// Returns 0 if index is 0 or out of range.
func (c *Cluster) getLastLogTermForIndex(index uint64) uint64 {
	if index == 0 {
		return 0
	}
	c.logMu.RLock()
	defer c.logMu.RUnlock()
	if index > uint64(len(c.log)) {
		return 0
	}
	return c.log[index-1].Term
}

// Propose proposes a command to be applied to the state machine.
func (c *Cluster) Propose(command []byte) (*CommandResult, error) {
	resultCh := make(chan *CommandResult, 1)

	select {
	case c.commandCh <- &Command{
		Command: command,
		Result:  resultCh,
	}:
		result := <-resultCh
		return result, nil
	case <-time.After(5 * time.Second):
		return nil, errors.New("command timeout")
	}
}

// getLastLogIndex returns the index of the last log entry.
func (c *Cluster) getLastLogIndex() uint64 {
	c.logMu.RLock()
	defer c.logMu.RUnlock()

	if len(c.log) == 0 {
		return 0
	}
	return c.log[len(c.log)-1].Index
}

// GetLogEntries returns log entries starting from the given index.
func (c *Cluster) GetLogEntries(startIndex uint64) []*LogEntry {
	c.logMu.RLock()
	defer c.logMu.RUnlock()

	var entries []*LogEntry
	for _, entry := range c.log {
		if entry.Index >= startIndex {
			entries = append(entries, entry)
		}
	}
	return entries
}

// GetNodes returns all nodes in the cluster.
func (c *Cluster) GetNodes() []*Node {
	c.nodesMu.RLock()
	defer c.nodesMu.RUnlock()

	nodes := make([]*Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// AddNode adds a node to the cluster.
func (c *Cluster) AddNode(nodeID, address string) {
	c.nodesMu.Lock()
	defer c.nodesMu.Unlock()

	c.nodes[nodeID] = &Node{
		ID:       nodeID,
		Address:  address,
		LastSeen: time.Now(),
		IsLeader: false,
	}
}

// RemoveNode removes a node from the cluster.
func (c *Cluster) RemoveNode(nodeID string) {
	c.nodesMu.Lock()
	defer c.nodesMu.Unlock()

	delete(c.nodes, nodeID)
}

// OnStateChange sets the callback for state changes.
func (c *Cluster) OnStateChange(fn func(oldState, newState State)) {
	c.onStateChange = fn
}

// OnLeaderElected sets the callback for leader election.
func (c *Cluster) OnLeaderElected(fn func(leaderID string)) {
	c.onLeaderElected = fn
}

// SetTransport sets the TCP transport for Raft RPCs.
// When set, RPCs are sent over the network; when nil, the cluster
// operates in local/test mode with simulated responses.
func (c *Cluster) SetTransport(t *TCPTransport) {
	c.transport = t
}

// RequestVote represents a request for a vote.
type RequestVote struct {
	Term         uint64
	CandidateID  string
	LastLogIndex uint64
	LastLogTerm  uint64
}

// RequestVoteResponse represents a response to a vote request.
type RequestVoteResponse struct {
	Term        uint64
	VoteGranted bool
}

// AppendEntries represents a request to append entries.
type AppendEntries struct {
	Term         uint64
	LeaderID     string
	PrevLogIndex uint64
	PrevLogTerm  uint64
	Entries      []*LogEntry
	LeaderCommit uint64
}

// AppendEntriesResponse represents a response to an append entries request.
type AppendEntriesResponse struct {
	Term    uint64
	Success bool
	// ConflictIndex and ConflictTerm help with log consistency
	ConflictIndex uint64
	ConflictTerm  uint64
}

// HandleRequestVote handles a request for a vote.
func (c *Cluster) HandleRequestVote(req *RequestVote) *RequestVoteResponse {
	if req.Term < c.GetTerm() {
		return &RequestVoteResponse{
			Term:        c.GetTerm(),
			VoteGranted: false,
		}
	}

	if req.Term > c.GetTerm() {
		c.currentTerm.Store(req.Term)
		c.setState(StateFollower)
		c.votedFor.Store("")
	}

	votedFor := c.votedFor.Load().(string)
	if (votedFor == "" || votedFor == req.CandidateID) && c.isLogUpToDate(req.LastLogIndex, req.LastLogTerm) {
		c.votedFor.Store(req.CandidateID)
		c.resetElectionTimer()
		return &RequestVoteResponse{
			Term:        c.GetTerm(),
			VoteGranted: true,
		}
	}

	return &RequestVoteResponse{
		Term:        c.GetTerm(),
		VoteGranted: false,
	}
}

// isLogUpToDate checks if the candidate's log is at least as up-to-date as ours.
func (c *Cluster) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastLogIndex := c.getLastLogIndex()
	myLastLogTerm := uint64(0)

	c.logMu.RLock()
	if len(c.log) > 0 {
		myLastLogTerm = c.log[len(c.log)-1].Term
	}
	c.logMu.RUnlock()

	if lastLogTerm != myLastLogTerm {
		return lastLogTerm > myLastLogTerm
	}
	return lastLogIndex >= myLastLogIndex
}

// HandleAppendEntries handles an append entries request.
func (c *Cluster) HandleAppendEntries(req *AppendEntries) *AppendEntriesResponse {
	if req.Term < c.GetTerm() {
		return &AppendEntriesResponse{
			Term:    c.GetTerm(),
			Success: false,
		}
	}

	c.resetElectionTimer()

	if req.Term > c.GetTerm() {
		c.currentTerm.Store(req.Term)
		c.setState(StateFollower)
		c.votedFor.Store("")
	}

	c.leaderID.Store(req.LeaderID)

	// Log consistency check: verify PrevLogIndex/PrevLogTerm match
	if req.PrevLogIndex > 0 {
		c.logMu.RLock()
		if req.PrevLogIndex > uint64(len(c.log)) {
			// Our log is too short — report conflict
			c.logMu.RUnlock()
			return &AppendEntriesResponse{
				Term:          c.GetTerm(),
				Success:       false,
				ConflictIndex: uint64(len(c.log)),
			}
		}
		if req.PrevLogIndex <= uint64(len(c.log)) {
			prevEntry := c.log[req.PrevLogIndex-1]
			if prevEntry.Term != req.PrevLogTerm {
				// Term mismatch at PrevLogIndex — report conflict
				conflictTerm := prevEntry.Term
				c.logMu.RUnlock()
				return &AppendEntriesResponse{
					Term:          c.GetTerm(),
					Success:       false,
					ConflictIndex: req.PrevLogIndex,
					ConflictTerm:  conflictTerm,
				}
			}
		}
		c.logMu.RUnlock()
	}

	// Append new entries (overwrite conflicting entries if any)
	if len(req.Entries) > 0 {
		c.logMu.Lock()
		for _, entry := range req.Entries {
			idx := entry.Index
			if idx <= uint64(len(c.log)) {
				// Overwrite existing entry if term differs
				if c.log[idx-1].Term != entry.Term {
					c.log = c.log[:idx-1] // Truncate from conflict point
					c.log = append(c.log, entry)
				}
			} else {
				c.log = append(c.log, entry)
			}
		}
		c.logMu.Unlock()
	}

	// Advance commitIndex if leader's commit is ahead
	if req.LeaderCommit > c.commitIndex.Load() {
		lastNewIndex := c.getLastLogIndex()
		newCommit := req.LeaderCommit
		if lastNewIndex < newCommit {
			newCommit = lastNewIndex
		}
		c.commitIndex.Store(newCommit)

		// Apply committed but unapplied entries to state machine
		for c.lastApplied.Load() < c.commitIndex.Load() {
			nextApply := c.lastApplied.Load() + 1
			c.logMu.RLock()
			if nextApply <= uint64(len(c.log)) {
				entry := c.log[nextApply-1]
				c.logMu.RUnlock()
				c.stateMachine.Apply(entry.Command)
				c.lastApplied.Add(1)
			} else {
				c.logMu.RUnlock()
				break
			}
		}
	}

	return &AppendEntriesResponse{
		Term:    c.GetTerm(),
		Success: true,
	}
}
