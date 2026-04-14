package engine

import (
	"fmt"
	"time"

	"github.com/openloadbalancer/olb/internal/cluster"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/logging"
)

// initCluster initializes the Raft cluster and cluster manager.
func (e *Engine) initCluster(clusterCfg *config.ClusterConfig, logger *logging.Logger) error {
	raftCfg := &cluster.Config{
		NodeID:        clusterCfg.NodeID,
		BindAddr:      clusterCfg.BindAddr,
		BindPort:      clusterCfg.BindPort,
		Peers:         clusterCfg.Peers,
		DataDir:       clusterCfg.DataDir,
		ElectionTick:  parseDuration(clusterCfg.ElectionTick, 2*time.Second),
		HeartbeatTick: parseDuration(clusterCfg.HeartbeatTick, 500*time.Millisecond),
	}

	// Create a config state machine for Raft replication.
	// This handles applying config changes (set_config, update_backend,
	// update_route, update_listener, WAF commands) across cluster nodes.
	configSM := cluster.NewConfigStateMachine(e.config)
	configSM.OnConfigApplied(func(newCfg *config.Config) {
		if err := e.validateConfig(newCfg); err != nil {
			logger.Error("Raft config validation failed", logging.Error(err))
			return
		}
		if err := e.applyConfig(newCfg); err != nil {
			logger.Error("Failed to apply Raft config", logging.Error(err))
			return
		}
		logger.Info("Config applied via Raft consensus")
	})
	stateMachine := configSM

	raftCluster, err := cluster.New(raftCfg, stateMachine)
	if err != nil {
		return fmt.Errorf("failed to create Raft cluster: %w", err)
	}
	e.raftCluster = raftCluster

	// Initialize persistence for crash recovery if DataDir is configured
	if clusterCfg.DataDir != "" {
		persister, err := cluster.NewFilePersister(clusterCfg.DataDir)
		if err != nil {
			logger.Warn("Failed to create Raft persister", logging.Error(err))
		} else {
			e.recoverRaftState(raftCluster, configSM, persister, logger)
			e.persister = persister
		}
	}

	// Initialize TCP transport for Raft RPCs
	bindAddr := fmt.Sprintf("%s:%d", clusterCfg.BindAddr, clusterCfg.BindPort)
	transportCfg := &cluster.TCPTransportConfig{
		BindAddr:    bindAddr,
		MaxPoolSize: 5,
		Timeout:     5 * time.Second,
	}
	transport, err := cluster.NewTCPTransport(transportCfg, raftCluster)
	if err != nil {
		logger.Warn("Failed to create cluster transport, running in local mode",
			logging.Error(err),
		)
	} else {
		// Wrap transport listener with authentication if node_auth is configured
		if clusterCfg.NodeAuth != nil && clusterCfg.NodeAuth.SharedSecret != "" {
			if err := transport.Start(); err != nil {
				logger.Warn("Failed to start cluster transport", logging.Error(err))
			} else {
				authListener := cluster.NewNodeAuthMiddleware(
					transport.Listener(),
					[]byte(clusterCfg.NodeAuth.SharedSecret),
					clusterCfg.NodeAuth.AllowedNodeIDs,
				)
				transport.SetListener(authListener)
				logger.Info("Cluster transport authentication enabled",
					logging.Int("allowed_nodes", len(clusterCfg.NodeAuth.AllowedNodeIDs)),
				)
			}
		} else {
			if err := transport.Start(); err != nil {
				logger.Warn("Failed to start cluster transport", logging.Error(err))
			} else {
				logger.Warn("Cluster transport has no node_auth configured - connections are unauthenticated")
			}
		}

		raftCluster.SetTransport(transport)
		if transport.Listener() != nil {
			logger.Info("Cluster TCP transport started", logging.String("bind_addr", bindAddr))
		}
	}

	// Create distributed state
	distState := cluster.NewDistributedState(nil)

	// Create cluster manager
	mgrCfg := &cluster.ClusterManagerConfig{
		NodeID:   clusterCfg.NodeID,
		BindAddr: clusterCfg.BindAddr,
		BindPort: clusterCfg.BindPort,
	}
	e.clusterMgr = cluster.NewClusterManager(mgrCfg, raftCluster, distState)

	logger.Info("Cluster initialized",
		logging.String("node_id", clusterCfg.NodeID),
		logging.Int("peers", len(clusterCfg.Peers)),
	)

	return nil
}

// recoverRaftState attempts to recover Raft state from a previous session.
// It loads the last known term/vote, restores the state machine from the
// latest snapshot, and replays any log entries not included in the snapshot.
func (e *Engine) recoverRaftState(c *cluster.Cluster, sm *cluster.ConfigStateMachine, p *cluster.FilePersister, logger *logging.Logger) {
	// Load persisted node state
	state, err := p.LoadRaftState()
	if err != nil {
		logger.Warn("Failed to load Raft state, starting fresh", logging.Error(err))
		return
	}
	if state.Term == 0 && state.CommitIndex == 0 {
		// No previous state found — fresh start
		return
	}

	// Restore term and vote
	c.CurrentTerm(state.Term)
	c.VotedFor(state.VotedFor)

	logger.Info("Recovered Raft state",
		logging.Uint64("term", state.Term),
		logging.String("voted_for", state.VotedFor),
		logging.Uint64("commit_index", state.CommitIndex),
	)

	// Load and replay log entries
	entries, err := p.LoadLogEntries()
	if err != nil {
		logger.Warn("Failed to load Raft log entries", logging.Error(err))
		return
	}

	// Replay entries that were committed but may not have been applied
	for _, entry := range entries {
		if entry.Index <= state.LastApplied {
			continue // already applied
		}
		if _, err := sm.Apply(entry.Command); err != nil {
			logger.Warn("Failed to replay log entry",
				logging.Uint64("index", entry.Index),
				logging.Error(err),
			)
		}
	}

	logger.Info("Replayed Raft log entries",
		logging.Int("total", len(entries)),
		logging.Uint64("last_applied", state.LastApplied),
	)
}
