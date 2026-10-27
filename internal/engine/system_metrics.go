package engine

import (
	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/metrics"
)

// systemMetrics holds gauges and counters that reflect system-level state.
// They are updated periodically by the engine's metrics refresh goroutine.
type systemMetrics struct {
	// Backend pool gauges
	backendTotal     *metrics.Gauge
	backendHealthy   *metrics.Gauge
	backendUnhealthy *metrics.Gauge

	// Connection gauges
	activeConnections *metrics.Gauge
	poolIdleConns     *metrics.Gauge
	poolActiveConns   *metrics.Gauge

	// Connection pool counters
	poolHitsTotal      *metrics.Counter
	poolMissesTotal    *metrics.Counter
	poolEvictionsTotal *metrics.Counter

	// Health check counters
	healthChecksTotal *metrics.Counter

	// Track last health check counter values to compute deltas
	lastHealthyCount   int
	lastUnhealthyCount int

	// Track last pool counter values to compute deltas
	lastPoolHits      int64
	lastPoolMisses    int64
	lastPoolEvictions int64
}

// registerSystemMetrics creates and registers system-level metrics in the
// shared registry so they appear in the Prometheus /metrics endpoint.
func registerSystemMetrics(registry *metrics.Registry) *systemMetrics {
	sm := &systemMetrics{
		backendTotal: metrics.NewGauge(
			"olb_backends_total",
			"Total number of configured backends",
		),
		backendHealthy: metrics.NewGauge(
			"olb_backends_healthy",
			"Number of healthy backends",
		),
		backendUnhealthy: metrics.NewGauge(
			"olb_backends_unhealthy",
			"Number of unhealthy backends",
		),
		activeConnections: metrics.NewGauge(
			"olb_active_connections",
			"Number of active connections across all backends",
		),
		poolIdleConns: metrics.NewGauge(
			"olb_pool_idle_connections",
			"Number of idle connections in connection pools",
		),
		poolActiveConns: metrics.NewGauge(
			"olb_pool_active_connections",
			"Number of active connections in connection pools",
		),
		poolHitsTotal: metrics.NewCounter(
			"olb_pool_hits_total",
			"Total number of connection pool hits (reused connections)",
		),
		poolMissesTotal: metrics.NewCounter(
			"olb_pool_misses_total",
			"Total number of connection pool misses (new connections)",
		),
		poolEvictionsTotal: metrics.NewCounter(
			"olb_pool_evictions_total",
			"Total number of evicted connections from pools",
		),
		healthChecksTotal: metrics.NewCounter(
			"olb_health_checks_total",
			"Total number of health check results",
		),
	}

	registry.RegisterGauge(sm.backendTotal)
	registry.RegisterGauge(sm.backendHealthy)
	registry.RegisterGauge(sm.backendUnhealthy)
	registry.RegisterGauge(sm.activeConnections)
	registry.RegisterGauge(sm.poolIdleConns)
	registry.RegisterGauge(sm.poolActiveConns)
	registry.RegisterCounter(sm.poolHitsTotal)
	registry.RegisterCounter(sm.poolMissesTotal)
	registry.RegisterCounter(sm.poolEvictionsTotal)
	registry.RegisterCounter(sm.healthChecksTotal)

	return sm
}

// updateSystemMetrics refreshes system gauges from the current state of
// backend pools and health checkers.
func (sm *systemMetrics) updateSystemMetrics(
	poolMgr *backend.PoolManager,
	healthChecker *health.Checker,
	connPoolMgr *conn.PoolManager,
) {
	if poolMgr == nil {
		return
	}

	var total, healthy, unhealthy float64
	for _, pool := range poolMgr.GetAllPools() {
		for _, b := range pool.GetAllBackends() {
			total++
			if b.State() == backend.StateUp {
				healthy++
			} else {
				unhealthy++
			}
		}
	}

	sm.backendTotal.Set(total)
	sm.backendHealthy.Set(healthy)
	sm.backendUnhealthy.Set(unhealthy)

	// Connection pool metrics — aggregate across all backend pools
	if connPoolMgr != nil {
		var idleTotal, activeTotal float64
		var hitsTotal, missesTotal, evictionsTotal int64
		for _, stats := range connPoolMgr.Stats() {
			idleTotal += float64(stats.Idle)
			activeTotal += float64(stats.Active)
			hitsTotal += stats.Hits
			missesTotal += stats.Misses
			evictionsTotal += stats.Evictions
		}
		sm.poolIdleConns.Set(idleTotal)
		sm.poolActiveConns.Set(activeTotal)

		// Increment counters by delta since last refresh
		if delta := hitsTotal - sm.lastPoolHits; delta > 0 {
			sm.poolHitsTotal.Add(delta)
		}
		if delta := missesTotal - sm.lastPoolMisses; delta > 0 {
			sm.poolMissesTotal.Add(delta)
		}
		if delta := evictionsTotal - sm.lastPoolEvictions; delta > 0 {
			sm.poolEvictionsTotal.Add(delta)
		}
		sm.lastPoolHits = hitsTotal
		sm.lastPoolMisses = missesTotal
		sm.lastPoolEvictions = evictionsTotal
	}

	// Health check counts — increment counter by delta since last refresh
	if healthChecker != nil {
		hc := healthChecker.CountHealthy()
		hu := healthChecker.CountUnhealthy()
		delta := (hc + hu) - (sm.lastHealthyCount + sm.lastUnhealthyCount)
		if delta > 0 {
			sm.healthChecksTotal.Add(int64(delta))
		}
		sm.lastHealthyCount = hc
		sm.lastUnhealthyCount = hu
	}
}
