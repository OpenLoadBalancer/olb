package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/router"
)

// initializePools creates pools and registers backends with health checker.
func (e *Engine) initializePools() error {
	for _, poolCfg := range e.config.Pools {
		pool := backend.NewPool(poolCfg.Name, poolCfg.Algorithm)

		// Create balancer for the pool using the registry
		bal := balancer.New(poolCfg.Algorithm)
		if bal == nil {
			bal = balancer.NewRoundRobin()
		}
		pool.SetBalancer(bal)

		// Add backends
		for i, backendCfg := range poolCfg.Backends {
			id := backendCfg.ID
			if id == "" {
				id = fmt.Sprintf("%s-%d", backendCfg.Address, i)
			}
			b := backend.NewBackend(id, backendCfg.Address)
			if backendCfg.Weight > math.MaxInt32 {
				return fmt.Errorf("backend %s weight %d exceeds maximum %d", backendCfg.Address, backendCfg.Weight, math.MaxInt32)
			}
			b.SetWeight(int32(backendCfg.Weight))
			if backendCfg.Scheme != "" {
				b.Scheme = backendCfg.Scheme
			}
			b.SetState(backend.StateUp) // Start as Up, health checker will update
			if err := pool.AddBackend(b); err != nil {
				return fmt.Errorf("failed to add backend %s to pool %s: %w",
					backendCfg.ID, poolCfg.Name, err)
			}

			// Register with health checker
			healthyThreshold := 2
			if poolCfg.HealthCheck.HealthyThreshold > 0 {
				healthyThreshold = poolCfg.HealthCheck.HealthyThreshold
			}
			unhealthyThreshold := 3
			if poolCfg.HealthCheck.UnhealthyThreshold > 0 {
				unhealthyThreshold = poolCfg.HealthCheck.UnhealthyThreshold
			}
			checkConfig := &health.Check{
				Type:               poolCfg.HealthCheck.Type,
				Path:               poolCfg.HealthCheck.Path,
				Interval:           parseDuration(poolCfg.HealthCheck.Interval, 10*time.Second),
				Timeout:            parseDuration(poolCfg.HealthCheck.Timeout, 5*time.Second),
				Command:            poolCfg.HealthCheck.Command,
				Args:               poolCfg.HealthCheck.Args,
				HealthyThreshold:   healthyThreshold,
				UnhealthyThreshold: unhealthyThreshold,
			}
			if err := e.healthChecker.Register(b, checkConfig); err != nil {
				e.logger.Warn("Failed to register backend with health checker",
					logging.String("backend_id", b.ID),
					logging.Error(err),
				)
			}
		}

		if err := e.poolManager.AddPool(pool); err != nil {
			return fmt.Errorf("failed to add pool %s: %w", poolCfg.Name, err)
		}

		e.logger.Info("Pool initialized",
			logging.String("name", poolCfg.Name),
			logging.String("algorithm", poolCfg.Algorithm),
			logging.Int("backends", len(poolCfg.Backends)),
		)
	}

	return nil
}

// initializeRoutes adds routes to the router.
func (e *Engine) initializeRoutes() error {
	for _, listenerCfg := range e.config.Listeners {
		for _, routeCfg := range listenerCfg.Routes {
			route := &router.Route{
				Name:        fmt.Sprintf("%s-%s", listenerCfg.Name, routeCfg.Path),
				Host:        routeCfg.Host,
				Path:        routeCfg.Path,
				Methods:     routeCfg.Methods,
				BackendPool: routeCfg.Pool,
			}
			if err := e.router.AddRoute(route); err != nil {
				return fmt.Errorf("failed to add route %s: %w", route.Name, err)
			}
		}
	}

	e.logger.Info("Routes initialized",
		logging.Int("count", e.router.RouteCount()),
	)

	return nil
}
