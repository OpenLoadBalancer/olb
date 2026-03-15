package cluster

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// mockStateSync is a mock implementation of StateSync for testing.
type mockStateSync struct {
	mu         sync.Mutex
	broadcasts [][]byte
	receiver   func(data []byte)
}

func newMockStateSync() *mockStateSync {
	return &mockStateSync{
		broadcasts: make([][]byte, 0),
	}
}

func (m *mockStateSync) Broadcast(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, data)
	return nil
}

func (m *mockStateSync) OnReceive(fn func(data []byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receiver = fn
}

func (m *mockStateSync) simulateReceive(data []byte) {
	m.mu.Lock()
	fn := m.receiver
	m.mu.Unlock()
	if fn != nil {
		fn(data)
	}
}

func (m *mockStateSync) getBroadcastCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.broadcasts)
}

func TestNewDistributedState(t *testing.T) {
	ds := NewDistributedState(nil)
	if ds == nil {
		t.Fatal("NewDistributedState returned nil")
	}
	if ds.config.SessionDefaultTTL != 30*time.Minute {
		t.Errorf("SessionDefaultTTL = %v, want 30m", ds.config.SessionDefaultTTL)
	}
	if ds.config.MaxSessionEntries != 100000 {
		t.Errorf("MaxSessionEntries = %d, want 100000", ds.config.MaxSessionEntries)
	}
}

func TestNewDistributedState_WithConfig(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID:            "node1",
		SyncInterval:      10 * time.Second,
		SessionDefaultTTL: 5 * time.Minute,
		MaxSessionEntries: 500,
	}
	ds := NewDistributedState(config)

	if ds.config.NodeID != "node1" {
		t.Errorf("NodeID = %q, want node1", ds.config.NodeID)
	}
	if ds.config.SessionDefaultTTL != 5*time.Minute {
		t.Errorf("SessionDefaultTTL = %v, want 5m", ds.config.SessionDefaultTTL)
	}
}

func TestDistributedState_PropagateHealthStatus(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID: "node1",
	}
	ds := NewDistributedState(config)
	mockSync := newMockStateSync()
	ds.SetSync(mockSync)

	// Propagate a health status
	ds.PropagateHealthStatus("10.0.0.1:8080", true, 5*time.Millisecond)

	// Verify local state was updated
	status, ok := ds.GetHealthStatus("10.0.0.1:8080")
	if !ok {
		t.Fatal("Expected health status to exist")
	}
	if !status.Healthy {
		t.Error("Expected healthy = true")
	}
	if status.Latency != 5*time.Millisecond {
		t.Errorf("Latency = %v, want 5ms", status.Latency)
	}
	if status.CheckerNodeID != "node1" {
		t.Errorf("CheckerNodeID = %q, want node1", status.CheckerNodeID)
	}

	// Verify broadcast was sent
	if mockSync.getBroadcastCount() != 1 {
		t.Errorf("Expected 1 broadcast, got %d", mockSync.getBroadcastCount())
	}
}

func TestDistributedState_GetClusterHealthView(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	// Propagate multiple health statuses (without sync to avoid nil panic)
	ds.healthMu.Lock()
	ds.healthState["10.0.0.1:8080"] = &HealthStatus{
		BackendAddr: "10.0.0.1:8080",
		Healthy:     true,
		Timestamp:   time.Now(),
	}
	ds.healthState["10.0.0.2:8080"] = &HealthStatus{
		BackendAddr: "10.0.0.2:8080",
		Healthy:     false,
		Timestamp:   time.Now(),
	}
	ds.healthMu.Unlock()

	view := ds.GetClusterHealthView()
	if len(view) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(view))
	}
	if !view["10.0.0.1:8080"].Healthy {
		t.Error("Expected 10.0.0.1:8080 to be healthy")
	}
	if view["10.0.0.2:8080"].Healthy {
		t.Error("Expected 10.0.0.2:8080 to be unhealthy")
	}
}

func TestDistributedState_MergeHealthStatus_LatestWins(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	oldTime := time.Now().Add(-10 * time.Second)
	newTime := time.Now()

	// Set initial state with a recent timestamp
	ds.healthMu.Lock()
	ds.healthState["10.0.0.1:8080"] = &HealthStatus{
		BackendAddr: "10.0.0.1:8080",
		Healthy:     true,
		Timestamp:   newTime,
	}
	ds.healthMu.Unlock()

	// Try to merge with an older timestamp (should NOT override)
	incoming := map[string]*HealthStatus{
		"10.0.0.1:8080": {
			BackendAddr: "10.0.0.1:8080",
			Healthy:     false,
			Timestamp:   oldTime,
		},
	}
	ds.MergeHealthStatus(incoming)

	status, _ := ds.GetHealthStatus("10.0.0.1:8080")
	if !status.Healthy {
		t.Error("Older timestamp should not override newer; expected healthy = true")
	}

	// Merge with a newer timestamp (should override)
	newerTime := time.Now().Add(10 * time.Second)
	incoming2 := map[string]*HealthStatus{
		"10.0.0.1:8080": {
			BackendAddr: "10.0.0.1:8080",
			Healthy:     false,
			Timestamp:   newerTime,
		},
	}
	ds.MergeHealthStatus(incoming2)

	status2, _ := ds.GetHealthStatus("10.0.0.1:8080")
	if status2.Healthy {
		t.Error("Newer timestamp should override; expected healthy = false")
	}
}

func TestDistributedState_MergeHealthStatus_NewBackend(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	incoming := map[string]*HealthStatus{
		"10.0.0.5:8080": {
			BackendAddr: "10.0.0.5:8080",
			Healthy:     true,
			Timestamp:   time.Now(),
		},
	}
	ds.MergeHealthStatus(incoming)

	status, ok := ds.GetHealthStatus("10.0.0.5:8080")
	if !ok {
		t.Fatal("Expected new backend to be added via merge")
	}
	if !status.Healthy {
		t.Error("Expected merged backend to be healthy")
	}
}

func TestDistributedState_PropagateSession(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID:            "node1",
		SessionDefaultTTL: 10 * time.Minute,
	}
	ds := NewDistributedState(config)
	mockSync := newMockStateSync()
	ds.SetSync(mockSync)

	expires := time.Now().Add(5 * time.Minute)
	ds.PropagateSession("sess-abc", "10.0.0.1:8080", expires)

	// Verify local state
	addr, ok := ds.GetSession("sess-abc")
	if !ok {
		t.Fatal("Expected session to exist")
	}
	if addr != "10.0.0.1:8080" {
		t.Errorf("BackendAddr = %q, want 10.0.0.1:8080", addr)
	}

	// Verify broadcast
	if mockSync.getBroadcastCount() != 1 {
		t.Errorf("Expected 1 broadcast, got %d", mockSync.getBroadcastCount())
	}
}

func TestDistributedState_PropagateSession_DefaultExpiry(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID:            "node1",
		SessionDefaultTTL: 15 * time.Minute,
	}
	ds := NewDistributedState(config)

	// Pass zero time for expires to use default TTL
	ds.PropagateSession("sess-def", "10.0.0.2:8080", time.Time{})

	// The session should exist and not be expired
	addr, ok := ds.GetSession("sess-def")
	if !ok {
		t.Fatal("Expected session to exist with default TTL")
	}
	if addr != "10.0.0.2:8080" {
		t.Errorf("BackendAddr = %q, want 10.0.0.2:8080", addr)
	}
}

func TestDistributedState_SessionExpiry(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID:            "node1",
		SessionDefaultTTL: 10 * time.Minute,
	}
	ds := NewDistributedState(config)

	// Add a session that has already expired
	ds.sessionMu.Lock()
	ds.sessionState["expired-sess"] = &SessionEntry{
		Key:         "expired-sess",
		BackendAddr: "10.0.0.1:8080",
		Expires:     time.Now().Add(-1 * time.Second), // Already expired
		Timestamp:   time.Now(),
	}
	ds.sessionMu.Unlock()

	// GetSession should return false for expired entry
	_, ok := ds.GetSession("expired-sess")
	if ok {
		t.Error("Expected expired session to not be returned")
	}
}

func TestDistributedState_SessionMerge_LatestWins(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	oldTime := time.Now().Add(-10 * time.Second)
	newTime := time.Now()
	expires := time.Now().Add(10 * time.Minute)

	// Set initial session with recent timestamp
	ds.sessionMu.Lock()
	ds.sessionState["sess-merge"] = &SessionEntry{
		Key:         "sess-merge",
		BackendAddr: "10.0.0.1:8080",
		Expires:     expires,
		Timestamp:   newTime,
	}
	ds.sessionMu.Unlock()

	// Try to merge older entry (should NOT override)
	ds.MergeSessions(map[string]*SessionEntry{
		"sess-merge": {
			Key:         "sess-merge",
			BackendAddr: "10.0.0.2:8080",
			Expires:     expires,
			Timestamp:   oldTime,
		},
	})

	addr, _ := ds.GetSession("sess-merge")
	if addr != "10.0.0.1:8080" {
		t.Errorf("Older merge should not override; got %q, want 10.0.0.1:8080", addr)
	}

	// Merge newer entry (should override)
	newerTime := time.Now().Add(10 * time.Second)
	ds.MergeSessions(map[string]*SessionEntry{
		"sess-merge": {
			Key:         "sess-merge",
			BackendAddr: "10.0.0.3:8080",
			Expires:     expires,
			Timestamp:   newerTime,
		},
	})

	addr2, _ := ds.GetSession("sess-merge")
	if addr2 != "10.0.0.3:8080" {
		t.Errorf("Newer merge should override; got %q, want 10.0.0.3:8080", addr2)
	}
}

func TestDistributedState_Serialization(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	// Add health data
	ds.healthMu.Lock()
	ds.healthState["10.0.0.1:8080"] = &HealthStatus{
		BackendAddr: "10.0.0.1:8080",
		Healthy:     true,
		Timestamp:   time.Now(),
	}
	ds.healthMu.Unlock()

	// Add session data
	ds.sessionMu.Lock()
	ds.sessionState["sess-ser"] = &SessionEntry{
		Key:         "sess-ser",
		BackendAddr: "10.0.0.2:8080",
		Expires:     time.Now().Add(10 * time.Minute),
		Timestamp:   time.Now(),
	}
	ds.sessionMu.Unlock()

	// Serialize
	data, err := ds.Serialize()
	if err != nil {
		t.Fatalf("Serialize error: %v", err)
	}

	// Verify it's valid JSON
	var msg StateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if msg.Type != StateMessageFull {
		t.Errorf("Type = %q, want %q", msg.Type, StateMessageFull)
	}
	if len(msg.Health) != 1 {
		t.Errorf("Health entries = %d, want 1", len(msg.Health))
	}
	if len(msg.Sessions) != 1 {
		t.Errorf("Session entries = %d, want 1", len(msg.Sessions))
	}
}

func TestDistributedState_Deserialization(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node2"}
	ds := NewDistributedState(config)

	// Create a serialized state from another node
	msg := &StateMessage{
		Type:     StateMessageFull,
		SenderID: "node1",
		Health: map[string]*HealthStatus{
			"10.0.0.1:8080": {
				BackendAddr: "10.0.0.1:8080",
				Healthy:     true,
				Timestamp:   time.Now(),
			},
		},
		Sessions: map[string]*SessionEntry{
			"sess-deser": {
				Key:         "sess-deser",
				BackendAddr: "10.0.0.3:8080",
				Expires:     time.Now().Add(10 * time.Minute),
				Timestamp:   time.Now(),
			},
		},
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Deserialize into the new state
	if err := ds.Deserialize(data); err != nil {
		t.Fatalf("Deserialize error: %v", err)
	}

	// Verify health data was loaded
	status, ok := ds.GetHealthStatus("10.0.0.1:8080")
	if !ok {
		t.Fatal("Expected health status to exist after deserialize")
	}
	if !status.Healthy {
		t.Error("Expected healthy = true")
	}

	// Verify session data was loaded
	addr, ok := ds.GetSession("sess-deser")
	if !ok {
		t.Fatal("Expected session to exist after deserialize")
	}
	if addr != "10.0.0.3:8080" {
		t.Errorf("Session addr = %q, want 10.0.0.3:8080", addr)
	}
}

func TestDistributedState_HandleIncoming(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)
	mockSync := newMockStateSync()
	ds.SetSync(mockSync)

	// Simulate receiving a health message from another node
	msg := &StateMessage{
		Type:     StateMessageHealth,
		SenderID: "node2",
		Health: map[string]*HealthStatus{
			"10.0.0.9:8080": {
				BackendAddr:   "10.0.0.9:8080",
				Healthy:       true,
				CheckerNodeID: "node2",
				Timestamp:     time.Now(),
			},
		},
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	mockSync.simulateReceive(data)

	// Verify the health status was merged
	status, ok := ds.GetHealthStatus("10.0.0.9:8080")
	if !ok {
		t.Fatal("Expected health status from node2 to be merged")
	}
	if status.CheckerNodeID != "node2" {
		t.Errorf("CheckerNodeID = %q, want node2", status.CheckerNodeID)
	}
}

func TestDistributedState_HandleIncoming_IgnoresSelf(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)
	mockSync := newMockStateSync()
	ds.SetSync(mockSync)

	// Simulate receiving a message from ourselves (should be ignored)
	msg := &StateMessage{
		Type:     StateMessageHealth,
		SenderID: "node1", // Same as our node ID
		Health: map[string]*HealthStatus{
			"10.0.0.99:8080": {
				BackendAddr: "10.0.0.99:8080",
				Healthy:     true,
				Timestamp:   time.Now(),
			},
		},
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	mockSync.simulateReceive(data)

	// Since we ignore our own messages in handleIncoming,
	// this should not appear in state
	_, ok := ds.GetHealthStatus("10.0.0.99:8080")
	if ok {
		t.Error("Should have ignored message from self")
	}
}

func TestDistributedState_CleanupExpiredSessions(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	// Add expired and non-expired sessions
	ds.sessionMu.Lock()
	ds.sessionState["expired1"] = &SessionEntry{
		Key:     "expired1",
		Expires: time.Now().Add(-1 * time.Minute),
	}
	ds.sessionState["expired2"] = &SessionEntry{
		Key:     "expired2",
		Expires: time.Now().Add(-5 * time.Minute),
	}
	ds.sessionState["active"] = &SessionEntry{
		Key:         "active",
		BackendAddr: "10.0.0.1:8080",
		Expires:     time.Now().Add(10 * time.Minute),
	}
	ds.sessionMu.Unlock()

	// Run cleanup
	ds.cleanupExpiredSessions()

	if ds.SessionCount() != 1 {
		t.Errorf("Expected 1 session after cleanup, got %d", ds.SessionCount())
	}

	addr, ok := ds.GetSession("active")
	if !ok {
		t.Error("Active session should still exist")
	}
	if addr != "10.0.0.1:8080" {
		t.Errorf("Active session addr = %q, want 10.0.0.1:8080", addr)
	}
}

func TestDistributedState_GetAllSessions_ExcludesExpired(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	ds.sessionMu.Lock()
	ds.sessionState["expired"] = &SessionEntry{
		Key:     "expired",
		Expires: time.Now().Add(-1 * time.Minute),
	}
	ds.sessionState["active"] = &SessionEntry{
		Key:         "active",
		BackendAddr: "10.0.0.1:8080",
		Expires:     time.Now().Add(10 * time.Minute),
	}
	ds.sessionMu.Unlock()

	sessions := ds.GetAllSessions()
	if len(sessions) != 1 {
		t.Errorf("Expected 1 active session, got %d", len(sessions))
	}
	if _, ok := sessions["active"]; !ok {
		t.Error("Expected 'active' session in results")
	}
}

func TestDistributedState_SessionLimitEnforcement(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID:            "node1",
		MaxSessionEntries: 3,
		SessionDefaultTTL: 10 * time.Minute,
	}
	ds := NewDistributedState(config)

	// Add more sessions than the limit
	for i := 0; i < 5; i++ {
		key := "sess-" + string(rune('a'+i))
		ds.sessionMu.Lock()
		ds.sessionState[key] = &SessionEntry{
			Key:         key,
			BackendAddr: "10.0.0.1:8080",
			Expires:     time.Now().Add(10 * time.Minute),
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
		}
		ds.enforceSessionLimit()
		ds.sessionMu.Unlock()
	}

	if ds.SessionCount() > 3 {
		t.Errorf("Expected at most 3 sessions, got %d", ds.SessionCount())
	}
}

func TestDistributedState_HealthCount(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	if ds.HealthCount() != 0 {
		t.Errorf("Expected 0 health entries initially, got %d", ds.HealthCount())
	}

	ds.healthMu.Lock()
	ds.healthState["a"] = &HealthStatus{BackendAddr: "a", Timestamp: time.Now()}
	ds.healthState["b"] = &HealthStatus{BackendAddr: "b", Timestamp: time.Now()}
	ds.healthMu.Unlock()

	if ds.HealthCount() != 2 {
		t.Errorf("Expected 2 health entries, got %d", ds.HealthCount())
	}
}

func TestDistributedState_BroadcastFullState(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)
	mockSync := newMockStateSync()
	ds.SetSync(mockSync)

	// Add some health data
	ds.healthMu.Lock()
	ds.healthState["10.0.0.1:8080"] = &HealthStatus{
		BackendAddr: "10.0.0.1:8080",
		Healthy:     true,
		Timestamp:   time.Now(),
	}
	ds.healthMu.Unlock()

	// Add some session data
	ds.sessionMu.Lock()
	ds.sessionState["sess-full"] = &SessionEntry{
		Key:         "sess-full",
		BackendAddr: "10.0.0.2:8080",
		Expires:     time.Now().Add(10 * time.Minute),
		Timestamp:   time.Now(),
	}
	ds.sessionMu.Unlock()

	// Call broadcastFullState directly
	ds.broadcastFullState()

	// Verify broadcast was sent
	if mockSync.getBroadcastCount() != 1 {
		t.Errorf("Expected 1 broadcast, got %d", mockSync.getBroadcastCount())
	}

	// Verify the broadcast is a full state message
	mockSync.mu.Lock()
	data := mockSync.broadcasts[0]
	mockSync.mu.Unlock()

	var msg StateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Failed to unmarshal broadcast: %v", err)
	}

	if msg.Type != StateMessageFull {
		t.Errorf("Type = %q, want %q", msg.Type, StateMessageFull)
	}
	if msg.SenderID != "node1" {
		t.Errorf("SenderID = %q, want node1", msg.SenderID)
	}
	if len(msg.Health) != 1 {
		t.Errorf("Health entries = %d, want 1", len(msg.Health))
	}
	if len(msg.Sessions) != 1 {
		t.Errorf("Session entries = %d, want 1", len(msg.Sessions))
	}
}

func TestDistributedState_BroadcastFullState_NoSync(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	// broadcastFullState without sync should not panic
	ds.broadcastFullState()
}

func TestDistributedState_SyncLoop(t *testing.T) {
	config := &DistributedStateConfig{
		NodeID:       "node1",
		SyncInterval: 50 * time.Millisecond, // Short interval for test
	}
	ds := NewDistributedState(config)
	mockSync := newMockStateSync()
	ds.SetSync(mockSync)

	// Add some health data so broadcasts are non-empty
	ds.healthMu.Lock()
	ds.healthState["10.0.0.1:8080"] = &HealthStatus{
		BackendAddr: "10.0.0.1:8080",
		Healthy:     true,
		Timestamp:   time.Now(),
	}
	ds.healthMu.Unlock()

	// Start the distributed state (which starts syncLoop and cleanupLoop)
	ds.Start()

	// Wait for at least two sync intervals
	time.Sleep(150 * time.Millisecond)

	// Stop the distributed state
	ds.Stop()

	// Verify that broadcastFullState was called at least once via syncLoop
	count := mockSync.getBroadcastCount()
	if count < 1 {
		t.Errorf("Expected at least 1 broadcast from syncLoop, got %d", count)
	}
}

func TestDistributedState_NoBroadcastWithoutSync(t *testing.T) {
	config := &DistributedStateConfig{NodeID: "node1"}
	ds := NewDistributedState(config)

	// Should not panic without sync set
	ds.PropagateHealthStatus("10.0.0.1:8080", true, time.Millisecond)
	ds.PropagateSession("key", "10.0.0.1:8080", time.Now().Add(time.Minute))

	// Verify state was still updated locally
	if ds.HealthCount() != 1 {
		t.Errorf("Expected 1 health entry, got %d", ds.HealthCount())
	}
	_, ok := ds.GetSession("key")
	if !ok {
		t.Error("Expected session to exist even without sync")
	}
}
