package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ClusterState represents the operational state of a node in the cluster.
type ClusterState string

const (
	// ClusterStateJoining indicates the node is in the process of joining.
	ClusterStateJoining ClusterState = "joining"

	// ClusterStateActive indicates the node is actively participating in the cluster.
	ClusterStateActive ClusterState = "active"

	// ClusterStateDraining indicates the node is draining before leaving.
	ClusterStateDraining ClusterState = "draining"

	// ClusterStateLeaving indicates the node is leaving the cluster.
	ClusterStateLeaving ClusterState = "leaving"

	// ClusterStateStandalone indicates the node is not part of any cluster.
	ClusterStateStandalone ClusterState = "standalone"
)

// MemberInfo contains information about a cluster member.
type MemberInfo struct {
	// ID is the node's unique identifier.
	ID string `json:"id"`

	// Address is the node's cluster communication address.
	Address string `json:"address"`

	// State is the node's current Raft state (leader, follower, candidate).
	RaftState string `json:"raft_state"`

	// IsLeader indicates whether this node is the Raft leader.
	IsLeader bool `json:"is_leader"`

	// LastSeen is when this member was last seen.
	LastSeen time.Time `json:"last_seen"`

	// Healthy indicates whether the member is considered healthy.
	Healthy bool `json:"healthy"`
}

// ClusterStatus contains the full status of the cluster as seen by this node.
type ClusterStatus struct {
	// NodeID is the ID of the local node.
	NodeID string `json:"node_id"`

	// State is the cluster operational state of this node.
	State ClusterState `json:"state"`

	// RaftState is the Raft consensus state (follower, candidate, leader).
	RaftState string `json:"raft_state"`

	// Leader is the ID of the current Raft leader.
	Leader string `json:"leader"`

	// Term is the current Raft term.
	Term uint64 `json:"term"`

	// Members is the list of known cluster members.
	Members []MemberInfo `json:"members"`

	// Healthy indicates whether this node considers the cluster healthy.
	Healthy bool `json:"healthy"`

	// Uptime is how long this node has been running.
	Uptime string `json:"uptime"`
}

// ClusterManagerConfig configures the cluster manager.
type ClusterManagerConfig struct {
	// NodeID is this node's unique identifier.
	NodeID string

	// BindAddr is the address to bind the cluster listener on.
	BindAddr string

	// BindPort is the port for cluster communication.
	BindPort int

	// DrainTimeout is how long to wait for draining before forceful leave.
	DrainTimeout time.Duration

	// HealthCheckInterval is how often to check cluster health.
	HealthCheckInterval time.Duration
}

// DefaultClusterManagerConfig returns a default cluster manager configuration.
func DefaultClusterManagerConfig() *ClusterManagerConfig {
	return &ClusterManagerConfig{
		BindPort:            7946,
		DrainTimeout:        30 * time.Second,
		HealthCheckInterval: 10 * time.Second,
	}
}

// ConnectionDrainer is implemented by the engine or connection manager
// to provide active connection count for graceful drain.
type ConnectionDrainer interface {
	ActiveConnectionCount() int64
}

// ClusterManager wraps the Raft Cluster and DistributedState to provide
// a high-level cluster management API including join, leave, and status operations.
type ClusterManager struct {
	config *ClusterManagerConfig

	cluster *Cluster
	state   *DistributedState

	mu        sync.RWMutex
	clusterSt ClusterState
	startTime time.Time
	seedAddrs []string

	stopCh  chan struct{}
	wg      sync.WaitGroup
	drainer ConnectionDrainer
}

// SetDrainer sets the connection drainer for graceful drain during Leave.
func (cm *ClusterManager) SetDrainer(d ConnectionDrainer) {
	cm.drainer = d
}

// NewClusterManager creates a new cluster manager.
func NewClusterManager(config *ClusterManagerConfig, cluster *Cluster, distState *DistributedState) *ClusterManager {
	if config == nil {
		config = DefaultClusterManagerConfig()
	}
	if config.DrainTimeout <= 0 {
		config.DrainTimeout = 30 * time.Second
	}
	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = 10 * time.Second
	}

	return &ClusterManager{
		config:    config,
		cluster:   cluster,
		state:     distState,
		clusterSt: ClusterStateStandalone,
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
	}
}

// Join attempts to join an existing cluster using the provided seed addresses.
func (cm *ClusterManager) Join(seedAddrs []string) error {
	cm.mu.Lock()
	if cm.clusterSt == ClusterStateActive {
		cm.mu.Unlock()
		return errors.New("already part of a cluster")
	}
	cm.clusterSt = ClusterStateJoining
	cm.seedAddrs = seedAddrs
	cm.mu.Unlock()

	if cm.cluster == nil {
		cm.mu.Lock()
		cm.clusterSt = ClusterStateStandalone
		cm.mu.Unlock()
		return errors.New("cluster not initialized")
	}

	// Add seed addresses as peers
	for _, addr := range seedAddrs {
		// Use the address as the node ID if no separate ID is available
		cm.cluster.AddNode(addr, addr)
	}

	// Start the cluster
	if err := cm.cluster.Start(); err != nil {
		cm.mu.Lock()
		cm.clusterSt = ClusterStateStandalone
		cm.mu.Unlock()
		return fmt.Errorf("failed to start cluster: %w", err)
	}

	// Start distributed state sync if available
	if cm.state != nil {
		cm.state.Start()
	}

	cm.mu.Lock()
	cm.clusterSt = ClusterStateActive
	cm.mu.Unlock()

	return nil
}

// Leave gracefully leaves the cluster, draining connections first.
func (cm *ClusterManager) Leave() error {
	cm.mu.Lock()
	if cm.clusterSt != ClusterStateActive {
		cm.mu.Unlock()
		return fmt.Errorf("not currently part of a cluster (state: %s)", cm.clusterSt)
	}
	cm.clusterSt = ClusterStateDraining
	cm.mu.Unlock()

	// Drain phase: wait for in-flight connections to complete
	drainDone := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[cluster] panic recovered in drain: %v", r)
			}
		}()
		defer close(drainDone)

		// If a connection drainer is available, poll until connections reach zero
		// or the drain timeout expires.
		if cm.drainer != nil {
			deadline := time.Now().Add(cm.config.DrainTimeout)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				if cm.drainer.ActiveConnectionCount() == 0 {
					return
				}
				if time.Now().After(deadline) {
					return
				}
				select {
				case <-ticker.C:
				case <-cm.stopCh:
					return
				}
			}
		}

		// Fallback: wait for drain timeout
		timer := time.NewTimer(cm.config.DrainTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-cm.stopCh:
		}
	}()

	select {
	case <-drainDone:
	case <-cm.stopCh:
	}

	cm.mu.Lock()
	cm.clusterSt = ClusterStateLeaving
	cm.mu.Unlock()

	// Stop distributed state
	if cm.state != nil {
		cm.state.Stop()
	}

	// Stop the cluster
	if cm.cluster != nil {
		if err := cm.cluster.Stop(); err != nil {
			cm.mu.Lock()
			cm.clusterSt = ClusterStateStandalone
			cm.mu.Unlock()
			return fmt.Errorf("failed to stop cluster: %w", err)
		}
	}

	cm.mu.Lock()
	cm.clusterSt = ClusterStateStandalone
	cm.mu.Unlock()

	return nil
}

// Status returns the current cluster status as seen by this node.
func (cm *ClusterManager) Status() *ClusterStatus {
	cm.mu.RLock()
	clState := cm.clusterSt
	start := cm.startTime
	cm.mu.RUnlock()

	status := &ClusterStatus{
		NodeID:  cm.config.NodeID,
		State:   clState,
		Uptime:  time.Since(start).Truncate(time.Second).String(),
		Healthy: clState == ClusterStateActive,
	}

	if cm.cluster != nil {
		status.RaftState = string(cm.cluster.GetState())
		status.Leader = cm.cluster.GetLeader()
		status.Term = cm.cluster.GetTerm()

		// Collect member info
		nodes := cm.cluster.GetNodes()
		status.Members = make([]MemberInfo, 0, len(nodes))
		for _, node := range nodes {
			member := MemberInfo{
				ID:        node.ID,
				Address:   node.Address,
				IsLeader:  node.IsLeader,
				LastSeen:  node.LastSeen,
				Healthy:   time.Since(node.LastSeen) < 30*time.Second,
				RaftState: "follower",
			}
			if node.IsLeader {
				member.RaftState = "leader"
			}
			status.Members = append(status.Members, member)
		}
	}

	return status
}

// GetState returns the current cluster operational state.
func (cm *ClusterManager) GetState() ClusterState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.clusterSt
}

// Stop stops the cluster manager and releases resources.
func (cm *ClusterManager) Stop() {
	select {
	case <-cm.stopCh:
		// Already stopped
	default:
		close(cm.stopCh)
	}
	cm.wg.Wait()
}

// RegisterAdminEndpoints registers cluster management API endpoints on the given mux.
// Endpoints:
//   - GET  /api/v1/cluster/status  — cluster status
//   - POST /api/v1/cluster/join    — join cluster
//   - POST /api/v1/cluster/leave   — leave cluster
//   - GET  /api/v1/cluster/members — list members
func (cm *ClusterManager) RegisterAdminEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/cluster/status", cm.handleClusterStatus)
	mux.HandleFunc("/api/v1/cluster/join", cm.handleClusterJoin)
	mux.HandleFunc("/api/v1/cluster/leave", cm.handleClusterLeave)
	mux.HandleFunc("/api/v1/cluster/members", cm.handleClusterMembers)
}

// handleClusterStatus handles GET /api/v1/cluster/status.
func (cm *ClusterManager) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	status := cm.Status()
	writeJSON(w, http.StatusOK, status)
}

// handleClusterJoin handles POST /api/v1/cluster/join.
func (cm *ClusterManager) handleClusterJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	var req joinRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON")
		return
	}

	if len(req.SeedAddrs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELD", "seed_addrs is required")
		return
	}

	if err := cm.Join(req.SeedAddrs); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "JOIN_FAILED", "failed to join cluster")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "successfully joined cluster",
		"state":   string(cm.GetState()),
	})
}

// handleClusterLeave handles POST /api/v1/cluster/leave.
func (cm *ClusterManager) handleClusterLeave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	if err := cm.Leave(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "LEAVE_FAILED", "failed to leave cluster")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "successfully left cluster",
		"state":   string(cm.GetState()),
	})
}

// handleClusterMembers handles GET /api/v1/cluster/members.
func (cm *ClusterManager) handleClusterMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	status := cm.Status()
	writeJSON(w, http.StatusOK, status.Members)
}

// joinRequest is the request body for the join endpoint.
type joinRequest struct {
	SeedAddrs []string `json:"seed_addrs"`
}

// apiResponse is the standard API response envelope.
type apiResponse struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *apiError `json:"error,omitempty"`
}

// apiError represents a structured error in the API.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := &apiResponse{
		Success: statusCode >= 200 && statusCode < 300,
		Data:    data,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("cluster: failed to encode JSON response: %v", err)
	}
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := &apiResponse{
		Success: false,
		Error: &apiError{
			Code:    code,
			Message: message,
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("cluster: failed to encode JSON error response: %v", err)
	}
}

// FormatMembersTable formats the members list as a text table.
func FormatMembersTable(members []MemberInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-20s %-30s %-12s %-10s %-10s\n", "ID", "Address", "Raft State", "Leader", "Healthy")
	sb.WriteString(strings.Repeat("-", 82) + "\n")

	for _, m := range members {
		leader := "no"
		if m.IsLeader {
			leader = "yes"
		}
		healthy := "no"
		if m.Healthy {
			healthy = "yes"
		}
		fmt.Fprintf(&sb, "%-20s %-30s %-12s %-10s %-10s\n",
			m.ID, m.Address, m.RaftState, leader, healthy)
	}
	return sb.String()
}
