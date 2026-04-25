// Package cluster provides distributed clustering and consensus for OpenLoadBalancer.
//
// This file implements distributed state management (Phase 4.4), enabling
// shared state propagation across cluster nodes. It handles health status
// propagation, session affinity table synchronization, and state merging
// using a last-writer-wins strategy based on timestamps.
package cluster

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"
)

// ErrInvalidHMAC is returned when a state message fails HMAC verification.
var ErrInvalidHMAC = errors.New("cluster: state message HMAC verification failed")

// HealthStatus represents the health status of a backend as seen by the cluster.
type HealthStatus struct {
	// BackendAddr is the address of the backend (e.g., "10.0.0.1:8080").
	BackendAddr string `json:"backend_addr"`

	// Healthy indicates whether the backend is considered healthy.
	Healthy bool `json:"healthy"`

	// LastCheck is the time the health check was last performed.
	LastCheck time.Time `json:"last_check"`

	// Latency is the last observed health check latency.
	Latency time.Duration `json:"latency"`

	// CheckerNodeID is the ID of the node that performed the check.
	CheckerNodeID string `json:"checker_node_id"`

	// Timestamp is when this status was last updated (used for merge conflict resolution).
	Timestamp time.Time `json:"timestamp"`
}

// SessionEntry represents a session affinity mapping in the distributed state.
type SessionEntry struct {
	// Key is the session key (e.g., cookie value, client IP, header value).
	Key string `json:"key"`

	// BackendAddr is the backend address this session is pinned to.
	BackendAddr string `json:"backend_addr"`

	// Expires is when this session entry expires.
	Expires time.Time `json:"expires"`

	// Timestamp is when this entry was last updated (used for merge conflict resolution).
	Timestamp time.Time `json:"timestamp"`
}

// StateMessageType identifies the type of state message being exchanged.
type StateMessageType string

const (
	// StateMessageHealth is a health status update message.
	StateMessageHealth StateMessageType = "health"

	// StateMessageSession is a session affinity update message.
	StateMessageSession StateMessageType = "session"

	// StateMessageFull is a full state synchronization message.
	StateMessageFull StateMessageType = "full"
)

// StateMessage is the envelope for state messages exchanged between nodes.
type StateMessage struct {
	// Type identifies the kind of state update.
	Type StateMessageType `json:"type"`

	// SenderID is the node that originated this message.
	SenderID string `json:"sender_id"`

	// Health contains health status updates (used when Type is StateMessageHealth or StateMessageFull).
	Health map[string]*HealthStatus `json:"health,omitempty"`

	// Sessions contains session affinity updates (used when Type is StateMessageSession or StateMessageFull).
	Sessions map[string]*SessionEntry `json:"sessions,omitempty"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`

	// HMAC is the HMAC-SHA256 of the message payload for integrity verification.
	// When present, it is computed over the serialized message with this field set to nil.
	// If HMACKey is configured on the receiver, messages without a valid HMAC are dropped.
	HMAC []byte `json:"hmac,omitempty"`
}

// StateSync is the interface for broadcasting state updates to other nodes.
// Implementations may use gossip protocol piggyback, direct RPC, or other mechanisms.
type StateSync interface {
	// Broadcast sends data to all other nodes in the cluster.
	Broadcast(data []byte) error

	// OnReceive registers a callback for incoming state data from peers.
	OnReceive(fn func(data []byte))
}

// DistributedStateConfig configures the distributed state manager.
type DistributedStateConfig struct {
	// NodeID is the ID of the local node.
	NodeID string

	// SyncInterval is how often to broadcast full state. Zero disables periodic sync.
	SyncInterval time.Duration

	// SessionDefaultTTL is the default TTL for session entries if not specified.
	SessionDefaultTTL time.Duration

	// MaxSessionEntries limits the number of session entries stored. Zero means unlimited.
	MaxSessionEntries int

	// HMACKey is the shared secret key used for HMAC-SHA256 verification of state
	// messages. When set, all outgoing messages carry an HMAC and all incoming
	// messages are verified before processing. When empty, HMAC is skipped
	// (backward compatible).
	HMACKey []byte
}

// DefaultDistributedStateConfig returns a default configuration.
func DefaultDistributedStateConfig() *DistributedStateConfig {
	return &DistributedStateConfig{
		SyncInterval:      30 * time.Second,
		SessionDefaultTTL: 30 * time.Minute,
		MaxSessionEntries: 100000,
	}
}

// DistributedState manages shared state across cluster nodes.
// It provides health status propagation and session affinity table
// synchronization with last-writer-wins conflict resolution.
type DistributedState struct {
	config *DistributedStateConfig

	// Health state: backendAddr -> HealthStatus
	healthMu    sync.RWMutex
	healthState map[string]*HealthStatus

	// Session state: sessionKey -> SessionEntry
	sessionMu    sync.RWMutex
	sessionState map[string]*SessionEntry

	// State sync transport
	sync   StateSync
	syncMu sync.RWMutex

	// Lifecycle
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewDistributedState creates a new distributed state manager.
func NewDistributedState(config *DistributedStateConfig) *DistributedState {
	if config == nil {
		config = DefaultDistributedStateConfig()
	}
	if config.SessionDefaultTTL <= 0 {
		config.SessionDefaultTTL = 30 * time.Minute
	}

	return &DistributedState{
		config:       config,
		healthState:  make(map[string]*HealthStatus),
		sessionState: make(map[string]*SessionEntry),
		stopCh:       make(chan struct{}),
	}
}

// computeStateHMAC computes HMAC-SHA256 of the serialized message data using
// the provided key. The HMAC field of the message is zeroed before serialization
// so that the digest covers only the payload.
func computeStateHMAC(msg *StateMessage, key []byte) []byte {
	// Save and zero the HMAC field so it is excluded from the digest.
	saved := msg.HMAC
	msg.HMAC = nil
	data, err := json.Marshal(msg)
	msg.HMAC = saved
	if err != nil {
		return nil
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// attachHMAC computes and attaches an HMAC to the message if an HMACKey is
// configured. Returns the message unchanged (no HMAC) when the key is empty.
func (ds *DistributedState) attachHMAC(msg *StateMessage) {
	if len(ds.config.HMACKey) == 0 {
		return
	}
	msg.HMAC = computeStateHMAC(msg, ds.config.HMACKey)
}

// verifyHMAC checks that the message carries a valid HMAC when an HMACKey is
// configured. Returns true when the HMAC is valid or when no key is configured
// (backward compatible). Returns false if a key is configured but the message
// HMAC is missing or invalid.
func (ds *DistributedState) verifyHMAC(msg *StateMessage) bool {
	if len(ds.config.HMACKey) == 0 {
		return true // no key configured — skip verification
	}
	if len(msg.HMAC) == 0 {
		return false
	}
	expected := computeStateHMAC(msg, ds.config.HMACKey)
	return hmac.Equal(msg.HMAC, expected)
}

// SetSync sets the state synchronization transport.
func (ds *DistributedState) SetSync(s StateSync) {
	ds.syncMu.Lock()
	defer ds.syncMu.Unlock()

	ds.sync = s
	if s != nil {
		s.OnReceive(ds.handleIncoming)
	}
}

// Start starts periodic state synchronization and cleanup.
func (ds *DistributedState) Start() {
	// Start session cleanup goroutine
	ds.wg.Add(1)
	go ds.cleanupLoop()

	// Start periodic full sync if configured
	if ds.config.SyncInterval > 0 {
		ds.wg.Add(1)
		go ds.syncLoop()
	}
}

// Stop stops the distributed state manager.
func (ds *DistributedState) Stop() {
	close(ds.stopCh)
	ds.wg.Wait()
}

// PropagateHealthStatus updates and broadcasts a backend's health status.
func (ds *DistributedState) PropagateHealthStatus(backendAddr string, healthy bool, latency time.Duration) {
	now := time.Now()
	status := &HealthStatus{
		BackendAddr:   backendAddr,
		Healthy:       healthy,
		LastCheck:     now,
		Latency:       latency,
		CheckerNodeID: ds.config.NodeID,
		Timestamp:     now,
	}

	// Update local state
	ds.healthMu.Lock()
	ds.healthState[backendAddr] = status
	ds.healthMu.Unlock()

	// Broadcast to peers
	ds.broadcastHealth(map[string]*HealthStatus{backendAddr: status})
}

// GetHealthStatus returns the health status for a specific backend.
func (ds *DistributedState) GetHealthStatus(backendAddr string) (*HealthStatus, bool) {
	ds.healthMu.RLock()
	defer ds.healthMu.RUnlock()

	status, ok := ds.healthState[backendAddr]
	if !ok {
		return nil, false
	}
	// Return a copy to avoid data races
	cp := *status
	return &cp, true
}

// GetClusterHealthView returns a snapshot of health status for all known backends.
func (ds *DistributedState) GetClusterHealthView() map[string]*HealthStatus {
	ds.healthMu.RLock()
	defer ds.healthMu.RUnlock()

	view := make(map[string]*HealthStatus, len(ds.healthState))
	for addr, status := range ds.healthState {
		cp := *status
		view[addr] = &cp
	}
	return view
}

// MergeHealthStatus merges incoming health states with local state.
// The latest timestamp wins in case of conflict.
func (ds *DistributedState) MergeHealthStatus(incoming map[string]*HealthStatus) {
	ds.healthMu.Lock()
	defer ds.healthMu.Unlock()

	for addr, inStatus := range incoming {
		existing, ok := ds.healthState[addr]
		if !ok || inStatus.Timestamp.After(existing.Timestamp) {
			cp := *inStatus
			ds.healthState[addr] = &cp
		}
	}
}

// PropagateSession updates and broadcasts a session affinity entry.
func (ds *DistributedState) PropagateSession(key string, backendAddr string, expires time.Time) {
	now := time.Now()
	if expires.IsZero() {
		expires = now.Add(ds.config.SessionDefaultTTL)
	}

	entry := &SessionEntry{
		Key:         key,
		BackendAddr: backendAddr,
		Expires:     expires,
		Timestamp:   now,
	}

	// Update local state
	ds.sessionMu.Lock()
	ds.sessionState[key] = entry
	ds.enforceSessionLimit()
	ds.sessionMu.Unlock()

	// Broadcast to peers
	ds.broadcastSessions(map[string]*SessionEntry{key: entry})
}

// GetSession returns the backend address for a session key, if it exists and hasn't expired.
func (ds *DistributedState) GetSession(key string) (string, bool) {
	ds.sessionMu.RLock()
	defer ds.sessionMu.RUnlock()

	entry, ok := ds.sessionState[key]
	if !ok {
		return "", false
	}

	// Check expiry
	if time.Now().After(entry.Expires) {
		return "", false
	}

	return entry.BackendAddr, true
}

// GetAllSessions returns all non-expired session entries.
func (ds *DistributedState) GetAllSessions() map[string]*SessionEntry {
	ds.sessionMu.RLock()
	defer ds.sessionMu.RUnlock()

	now := time.Now()
	sessions := make(map[string]*SessionEntry)
	for key, entry := range ds.sessionState {
		if now.Before(entry.Expires) {
			cp := *entry
			sessions[key] = &cp
		}
	}
	return sessions
}

// MergeSessions merges incoming session entries with local state.
// The latest timestamp wins in case of conflict.
func (ds *DistributedState) MergeSessions(incoming map[string]*SessionEntry) {
	ds.sessionMu.Lock()
	defer ds.sessionMu.Unlock()

	for key, inEntry := range incoming {
		existing, ok := ds.sessionState[key]
		if !ok || inEntry.Timestamp.After(existing.Timestamp) {
			cp := *inEntry
			ds.sessionState[key] = &cp
		}
	}
	ds.enforceSessionLimit()
}

// enforceSessionLimit removes oldest entries if we exceed MaxSessionEntries.
// Must be called with sessionMu held.
func (ds *DistributedState) enforceSessionLimit() {
	if ds.config.MaxSessionEntries <= 0 {
		return
	}

	for len(ds.sessionState) > ds.config.MaxSessionEntries {
		// Find the oldest entry by timestamp
		var oldestKey string
		var oldestTime time.Time
		first := true
		for key, entry := range ds.sessionState {
			if first || entry.Timestamp.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.Timestamp
				first = false
			}
		}
		if oldestKey != "" {
			delete(ds.sessionState, oldestKey)
		}
	}
}

// broadcastHealth broadcasts health status updates to peers.
func (ds *DistributedState) broadcastHealth(health map[string]*HealthStatus) {
	ds.syncMu.RLock()
	s := ds.sync
	ds.syncMu.RUnlock()

	if s == nil {
		return
	}

	msg := &StateMessage{
		Type:      StateMessageHealth,
		SenderID:  ds.config.NodeID,
		Health:    health,
		Timestamp: time.Now(),
	}
	ds.attachHMAC(msg)

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	if err := s.Broadcast(data); err != nil {
		log.Printf("broadcast health state failed: %v", err)
	}
}

// broadcastSessions broadcasts session updates to peers.
func (ds *DistributedState) broadcastSessions(sessions map[string]*SessionEntry) {
	ds.syncMu.RLock()
	s := ds.sync
	ds.syncMu.RUnlock()

	if s == nil {
		return
	}

	msg := &StateMessage{
		Type:      StateMessageSession,
		SenderID:  ds.config.NodeID,
		Sessions:  sessions,
		Timestamp: time.Now(),
	}
	ds.attachHMAC(msg)

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	if err := s.Broadcast(data); err != nil {
		log.Printf("broadcast health state failed: %v", err)
	}
}

// broadcastFullState broadcasts the complete state to peers.
func (ds *DistributedState) broadcastFullState() {
	ds.syncMu.RLock()
	s := ds.sync
	ds.syncMu.RUnlock()

	if s == nil {
		return
	}

	msg := &StateMessage{
		Type:      StateMessageFull,
		SenderID:  ds.config.NodeID,
		Health:    ds.GetClusterHealthView(),
		Sessions:  ds.GetAllSessions(),
		Timestamp: time.Now(),
	}
	ds.attachHMAC(msg)

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	if err := s.Broadcast(data); err != nil {
		log.Printf("broadcast health state failed: %v", err)
	}
}

// handleIncoming processes incoming state messages from peers.
func (ds *DistributedState) handleIncoming(data []byte) {
	var msg StateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Ignore messages from ourselves
	if msg.SenderID == ds.config.NodeID {
		return
	}

	// Verify HMAC integrity when a key is configured.
	if !ds.verifyHMAC(&msg) {
		log.Printf("cluster: dropping state message from %q: HMAC verification failed", msg.SenderID)
		return
	}

	switch msg.Type {
	case StateMessageHealth:
		if msg.Health != nil {
			ds.MergeHealthStatus(msg.Health)
		}
	case StateMessageSession:
		if msg.Sessions != nil {
			ds.MergeSessions(msg.Sessions)
		}
	case StateMessageFull:
		if msg.Health != nil {
			ds.MergeHealthStatus(msg.Health)
		}
		if msg.Sessions != nil {
			ds.MergeSessions(msg.Sessions)
		}
	}
}

// cleanupLoop periodically removes expired session entries.
func (ds *DistributedState) cleanupLoop() {
	defer ds.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.cleanupExpiredSessions()
		}
	}
}

// cleanupExpiredSessions removes expired session entries from the local state.
func (ds *DistributedState) cleanupExpiredSessions() {
	ds.sessionMu.Lock()
	defer ds.sessionMu.Unlock()

	now := time.Now()
	for key, entry := range ds.sessionState {
		if now.After(entry.Expires) {
			delete(ds.sessionState, key)
		}
	}
}

// syncLoop periodically broadcasts full state to peers.
func (ds *DistributedState) syncLoop() {
	defer ds.wg.Done()

	ticker := time.NewTicker(ds.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.broadcastFullState()
		}
	}
}

// Serialize serializes the full distributed state to JSON bytes.
func (ds *DistributedState) Serialize() ([]byte, error) {
	msg := &StateMessage{
		Type:      StateMessageFull,
		SenderID:  ds.config.NodeID,
		Health:    ds.GetClusterHealthView(),
		Sessions:  ds.GetAllSessions(),
		Timestamp: time.Now(),
	}
	ds.attachHMAC(msg)
	return json.Marshal(msg)
}

// Deserialize loads state from JSON bytes, merging with existing state.
func (ds *DistributedState) Deserialize(data []byte) error {
	var msg StateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	// Verify HMAC integrity when a key is configured.
	if !ds.verifyHMAC(&msg) {
		return ErrInvalidHMAC
	}

	if msg.Health != nil {
		ds.MergeHealthStatus(msg.Health)
	}
	if msg.Sessions != nil {
		ds.MergeSessions(msg.Sessions)
	}
	return nil
}

// HealthCount returns the number of tracked health entries.
func (ds *DistributedState) HealthCount() int {
	ds.healthMu.RLock()
	defer ds.healthMu.RUnlock()
	return len(ds.healthState)
}

// SessionCount returns the number of active session entries (including expired).
func (ds *DistributedState) SessionCount() int {
	ds.sessionMu.RLock()
	defer ds.sessionMu.RUnlock()
	return len(ds.sessionState)
}
