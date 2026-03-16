package engine

import (
	"fmt"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/proxy/l7"
	"github.com/openloadbalancer/olb/internal/router"
	olbTLS "github.com/openloadbalancer/olb/internal/tls"
)

// loadConfig reloads configuration from disk.
func (e *Engine) loadConfig() (*config.Config, error) {
	loader := config.NewLoader()
	return loader.Load(e.configPath)
}

// validateConfig validates configuration.
func (e *Engine) validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("configuration is nil")
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	// Validate pools have valid algorithms
	for _, pool := range cfg.Pools {
		switch pool.Algorithm {
		case "round_robin", "rr", "weighted_round_robin", "wrr", "":
			// Valid algorithms
		default:
			return fmt.Errorf("pool %s: unknown algorithm %s", pool.Name, pool.Algorithm)
		}
	}

	// Validate routes reference existing pools
	poolNames := make(map[string]bool)
	for _, pool := range cfg.Pools {
		poolNames[pool.Name] = true
	}

	for _, listener := range cfg.Listeners {
		for _, route := range listener.Routes {
			if !poolNames[route.Pool] {
				return fmt.Errorf("route references non-existent pool: %s", route.Pool)
			}
		}
	}

	return nil
}

// applyConfig applies new configuration atomically.
// This performs a hot-reload without dropping connections.
func (e *Engine) applyConfig(newCfg *config.Config) error {
	e.logger.Info("Applying new configuration...")

	// 1. Create new router with new routes
	newRouter := router.NewRouter()
	for _, listenerCfg := range newCfg.Listeners {
		for _, routeCfg := range listenerCfg.Routes {
			route := &router.Route{
				Name:        fmt.Sprintf("%s-%s", listenerCfg.Name, routeCfg.Path),
				Host:        routeCfg.Host,
				Path:        routeCfg.Path,
				Methods:     routeCfg.Methods,
				BackendPool: routeCfg.Pool,
			}
			if err := newRouter.AddRoute(route); err != nil {
				return fmt.Errorf("failed to add route %s: %w", route.Name, err)
			}
		}
	}

	// 2. Create new pool manager
	newPoolManager := backend.NewPoolManager()

	// 3. Create new health checker
	newHealthChecker := health.NewChecker()

	// 4. Initialize pools and register backends
	for _, poolCfg := range newCfg.Pools {
		pool := backend.NewPool(poolCfg.Name, poolCfg.Algorithm)

		// Create balancer
		var bal backend.Balancer
		switch poolCfg.Algorithm {
		case "weighted_round_robin", "wrr":
			bal = balancer.NewWeightedRoundRobin()
		default:
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
			b.Weight = int32(backendCfg.Weight)
			if err := pool.AddBackend(b); err != nil {
				return fmt.Errorf("failed to add backend %s to pool %s: %w",
					id, poolCfg.Name, err)
			}

			// Register with health checker
			checkConfig := &health.Check{
				Type:               poolCfg.HealthCheck.Type,
				Path:               poolCfg.HealthCheck.Path,
				Interval:           parseDuration(poolCfg.HealthCheck.Interval, 10*time.Second),
				Timeout:            parseDuration(poolCfg.HealthCheck.Timeout, 5*time.Second),
				HealthyThreshold:   2,
				UnhealthyThreshold: 3,
			}
			if err := newHealthChecker.Register(b, checkConfig); err != nil {
				e.logger.Warn("Failed to register backend with health checker",
					logging.String("backend_id", b.ID),
					logging.Error(err),
				)
			}
		}

		if err := newPoolManager.AddPool(pool); err != nil {
			return fmt.Errorf("failed to add pool %s: %w", poolCfg.Name, err)
		}
	}

	// 5. Atomic swap - replace router and pools
	// We need to update the proxy with new components
	e.mu.Lock()
	oldRouter := e.router
	oldPoolManager := e.poolManager
	oldHealthChecker := e.healthChecker

	e.router = newRouter
	e.poolManager = newPoolManager
	e.healthChecker = newHealthChecker
	e.config = newCfg

	// Update proxy components
	// Note: The proxy references the router and pool manager directly,
	// so we need to create a new proxy or update its references.
	// For now, we'll create a new proxy configuration.
	newProxyConfig := &l7.Config{
		Router:          newRouter,
		PoolManager:     newPoolManager,
		ConnPoolManager: e.connPoolMgr,
		HealthChecker:   newHealthChecker,
		MiddlewareChain: e.middlewareChain,
		ProxyTimeout:    60 * time.Second,
		DialTimeout:     10 * time.Second,
		MaxRetries:      3,
	}
	newProxy := l7.NewHTTPProxy(newProxyConfig)

	// Close old proxy and swap
	if e.proxy != nil {
		go func() {
			// Give some time for in-flight requests to complete
			time.Sleep(5 * time.Second)
			e.proxy.Close()
		}()
	}
	e.proxy = newProxy
	e.mu.Unlock()

	// 6. Stop old health checker
	go func() {
		time.Sleep(10 * time.Second)
		oldHealthChecker.Stop()
	}()

	// 7. Reload TLS certificates if changed
	if newCfg.TLS != nil && newCfg.TLS.CertFile != "" && newCfg.TLS.KeyFile != "" {
		if err := e.tlsManager.ReloadCertificates([]olbTLS.CertConfig{
			{CertFile: newCfg.TLS.CertFile, KeyFile: newCfg.TLS.KeyFile},
		}); err != nil {
			e.logger.Warn("Failed to reload TLS certificates", logging.Error(err))
		}
	}

	e.logger.Info("Configuration applied successfully",
		logging.Int("pools", newPoolManager.PoolCount()),
		logging.Int("routes", newRouter.RouteCount()),
	)

	// Suppress unused variable warnings (old components are kept for graceful transition)
	_ = oldRouter
	_ = oldPoolManager

	return nil
}

// reloadListeners checks if listener configuration changed and updates accordingly.
// This is called during config reload if listener addresses changed.
func (e *Engine) reloadListeners(newCfg *config.Config) error {
	// Check if listener config changed
	if len(newCfg.Listeners) != len(e.listeners) {
		e.logger.Warn("Listener count changed - requires restart for full effect")
		return nil
	}

	// For now, we don't support changing listener addresses dynamically
	// as it requires stopping and restarting listeners which drops connections
	for i, newListener := range newCfg.Listeners {
		oldListener := e.config.Listeners[i]
		if newListener.Address != oldListener.Address ||
			newListener.IsTLS() != oldListener.IsTLS() {
			e.logger.Warn("Listener configuration changed - requires restart for full effect",
				logging.String("listener", newListener.Name),
			)
		}
	}

	return nil
}

// Helper to check if configuration requires listener restart
func listenersChanged(oldCfg, newCfg *config.Config) bool {
	if len(oldCfg.Listeners) != len(newCfg.Listeners) {
		return true
	}

	for i, oldL := range oldCfg.Listeners {
		newL := newCfg.Listeners[i]
		if oldL.Name != newL.Name ||
			oldL.Address != newL.Address ||
			oldL.IsTLS() != newL.IsTLS() ||
			oldL.Protocol != newL.Protocol {
			return true
		}
	}

	return false
}
