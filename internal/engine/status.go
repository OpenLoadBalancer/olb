package engine

import (
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/cluster"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/discovery"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/mcp"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/plugin"
	"github.com/openloadbalancer/olb/internal/router"
	"github.com/openloadbalancer/olb/pkg/version"
)

// IsRunning returns true if the engine is started.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state == StateRunning
}

// Done returns a channel that is closed when the engine shuts down.
// Callers can block on this to wait for the engine to stop.
func (e *Engine) Done() <-chan struct{} {
	return e.stopCh
}

// GetState returns the current engine state.
func (e *Engine) GetState() State {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// Uptime returns engine uptime.
func (e *Engine) Uptime() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state != StateRunning {
		return 0
	}
	return time.Since(e.startTime)
}

// GetStatus returns engine status information.
func (e *Engine) GetStatus() Status {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return Status{
		State:     string(e.state),
		Uptime:    e.Uptime().String(),
		Version:   version.Version,
		Listeners: len(e.listeners),
		Pools:     e.poolManager.PoolCount(),
		Routes:    e.router.RouteCount(),
	}
}

// GetConfig returns the current configuration.
func (e *Engine) GetConfig() *config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	// Return a shallow copy so callers cannot mutate the live config.
	cfg := *e.config
	return &cfg
}

// GetLogger returns the engine logger.
func (e *Engine) GetLogger() *logging.Logger {
	return e.logger
}

// GetMetrics returns the metrics registry.
func (e *Engine) GetMetrics() *metrics.Registry {
	return e.metrics
}

// GetPoolManager returns the pool manager.
func (e *Engine) GetPoolManager() *backend.PoolManager {
	return e.poolManager
}

// GetRouter returns the router.
func (e *Engine) GetRouter() *router.Router {
	return e.router
}

// GetHealthChecker returns the health checker.
func (e *Engine) GetHealthChecker() *health.Checker {
	return e.healthChecker
}

// GetMCPServer returns the MCP server.
func (e *Engine) GetMCPServer() *mcp.Server {
	return e.mcpServer
}

// GetPluginManager returns the plugin manager.
func (e *Engine) GetPluginManager() *plugin.PluginManager {
	return e.pluginMgr
}

// GetClusterManager returns the cluster manager (may be nil).
func (e *Engine) GetClusterManager() *cluster.ClusterManager {
	return e.clusterMgr
}

// GetDiscoveryManager returns the discovery manager.
func (e *Engine) GetDiscoveryManager() *discovery.Manager {
	return e.discoveryMgr
}

// GetRaftCluster returns the Raft cluster (may be nil if clustering is not enabled).
func (e *Engine) GetRaftCluster() *cluster.Cluster {
	return e.raftCluster
}
