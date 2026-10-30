package engine

import (
	"context"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/discovery"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/logging"
)

const (
	// discoveryIDPrefix marks backends that came from discovery, so the
	// syncer never touches statically configured ones.
	discoveryIDPrefix = "disc:"

	discoverySyncInterval = 2 * time.Second
)

// poolDiscovery keeps one pool's discovered backends in line with a provider.
//
// It reconciles against Services() on a timer instead of replaying events:
// providers drop events when their channel is full, and a reload replaces the
// pool, which the next sync simply refills.
type poolDiscovery struct {
	e        *Engine
	poolCfg  *config.Pool
	provider discovery.Provider
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// startPoolDiscovery creates and starts a provider for every pool that
// configures one. Providers that fail to start are logged and skipped: a
// pool keeps serving its static backends.
func (e *Engine) startPoolDiscovery(cfg *config.Config) {
	e.discMu.Lock()
	defer e.discMu.Unlock()
	for _, poolCfg := range cfg.Pools {
		if poolCfg.Discovery == nil {
			continue
		}
		pd, err := newPoolDiscovery(e, poolCfg)
		if err != nil {
			e.logger.Warn("Pool discovery disabled",
				logging.String("pool", poolCfg.Name), logging.Error(err))
			continue
		}
		e.discoveries = append(e.discoveries, pd)
		e.logger.Info("Pool discovery started",
			logging.String("pool", poolCfg.Name),
			logging.String("provider", poolCfg.Discovery.Provider))
	}
}

// stopPoolDiscovery stops all pool syncers and their providers.
func (e *Engine) stopPoolDiscovery() {
	e.discMu.Lock()
	defer e.discMu.Unlock()
	for _, pd := range e.discoveries {
		pd.stop()
	}
	e.discoveries = nil
}

func (e *Engine) restartPoolDiscovery(cfg *config.Config) {
	e.stopPoolDiscovery()
	e.startPoolDiscovery(cfg)
}

func newPoolDiscovery(e *Engine, poolCfg *config.Pool) (*poolDiscovery, error) {
	d := poolCfg.Discovery
	pc := discovery.DefaultProviderConfig()
	pc.Type = discovery.ProviderType(d.Provider)
	pc.Name = "pool:" + poolCfg.Name
	pc.Options = d.Options
	pc.Tags = d.Tags
	if d.Refresh != "" {
		if dur, err := time.ParseDuration(d.Refresh); err == nil {
			pc.Refresh = dur
		}
	}
	provider, err := discovery.CreateProvider(pc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := provider.Start(ctx); err != nil {
		cancel()
		return nil, err
	}
	pd := &poolDiscovery{e: e, poolCfg: poolCfg, provider: provider, cancel: cancel}
	pd.wg.Add(1)
	go pd.loop(ctx)
	return pd, nil
}

func (pd *poolDiscovery) stop() {
	pd.cancel()
	_ = pd.provider.Stop()
	pd.wg.Wait()
}

func (pd *poolDiscovery) loop(ctx context.Context) {
	defer pd.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			pd.e.logger.Error("Pool discovery panic recovered",
				logging.String("pool", pd.poolCfg.Name), logging.Any("panic", r))
		}
	}()

	t := time.NewTicker(discoverySyncInterval)
	defer t.Stop()
	for {
		pd.sync()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sync makes the pool's discovered backends match the provider's healthy
// services. The pool and health checker are read fresh each time because a
// config reload swaps both.
func (pd *poolDiscovery) sync() {
	e := pd.e
	e.mu.RLock()
	pm := e.poolManager
	hc := e.healthChecker
	e.mu.RUnlock()
	if pm == nil {
		return
	}
	pool := pm.GetPool(pd.poolCfg.Name)
	if pool == nil {
		return
	}

	want := make(map[string]*discovery.Service)
	for _, svc := range pd.provider.Services() {
		if svc.Healthy && svc.Port > 0 {
			want[discoveryIDPrefix+svc.ID] = svc
		}
	}

	have := make(map[string]*backend.Backend)
	for _, b := range pool.GetAllBackends() {
		if len(b.ID) > len(discoveryIDPrefix) && b.ID[:len(discoveryIDPrefix)] == discoveryIDPrefix {
			have[b.ID] = b
		}
	}

	for id, b := range have {
		svc, ok := want[id]
		if ok && svc.FullAddress() == b.Address {
			continue
		}
		// Gone, or the service moved: drop it (a moved one is re-added below).
		if hc != nil {
			hc.Unregister(id)
		}
		if err := pool.RemoveBackend(id); err != nil {
			e.logger.Warn("Discovery: removing backend failed",
				logging.String("pool", pd.poolCfg.Name), logging.String("backend", id), logging.Error(err))
			continue
		}
		delete(have, id)
		e.logger.Info("Discovery: backend removed",
			logging.String("pool", pd.poolCfg.Name), logging.String("address", b.Address))
	}

	for id, svc := range want {
		if _, ok := have[id]; ok {
			continue
		}
		b := backend.NewBackend(id, svc.FullAddress())
		weight := svc.Weight
		if weight <= 0 {
			weight = 100
		}
		b.SetWeight(int32(min(weight, 1<<30))) // #nosec G115 -- capped above
		b.SetState(backend.StateUp)
		if err := pool.AddBackend(b); err != nil {
			e.logger.Warn("Discovery: adding backend failed",
				logging.String("pool", pd.poolCfg.Name), logging.String("backend", id), logging.Error(err))
			continue
		}
		if hc != nil {
			if err := hc.Register(b, checkFromConfig(pd.poolCfg.HealthCheck)); err != nil { // nolint:contextcheck -- the checker owns its own lifetime
				e.logger.Warn("Discovery: registering health check failed",
					logging.String("backend", id), logging.Error(err))
			}
		}
		e.logger.Info("Discovery: backend added",
			logging.String("pool", pd.poolCfg.Name), logging.String("address", b.Address))
	}
}

// checkFromConfig builds the health check a pool's backends are registered
// with, applying the same defaults as the static backends.
func checkFromConfig(hcCfg *config.HealthCheck) *health.Check {
	if hcCfg == nil {
		hcCfg = config.DefaultHealthCheck()
	}
	healthy, unhealthy := 2, 3
	if hcCfg.HealthyThreshold > 0 {
		healthy = hcCfg.HealthyThreshold
	}
	if hcCfg.UnhealthyThreshold > 0 {
		unhealthy = hcCfg.UnhealthyThreshold
	}
	return &health.Check{
		Type:               hcCfg.Type,
		Path:               hcCfg.Path,
		Interval:           parseDuration(hcCfg.Interval, 10*time.Second),
		Timeout:            parseDuration(hcCfg.Timeout, 5*time.Second),
		Command:            hcCfg.Command,
		Args:               hcCfg.Args,
		HealthyThreshold:   healthy,
		UnhealthyThreshold: unhealthy,
	}
}
