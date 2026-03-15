// Package engine provides the central orchestrator for OpenLoadBalancer.
// It coordinates all components including listeners, proxy, health checking,
// routing, and configuration management.
package engine

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/admin"
	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/cluster"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/discovery"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/listener"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/mcp"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/plugin"
	"github.com/openloadbalancer/olb/internal/proxy/l7"
	"github.com/openloadbalancer/olb/internal/router"
	"github.com/openloadbalancer/olb/internal/tls"
	"github.com/openloadbalancer/olb/internal/webui"
	"github.com/openloadbalancer/olb/pkg/version"
)

// State represents the engine runtime state.
type State string

const (
	// StateStopped indicates the engine is not running.
	StateStopped State = "stopped"
	// StateStarting indicates the engine is initializing.
	StateStarting State = "starting"
	// StateRunning indicates the engine is active.
	StateRunning State = "running"
	// StateReloading indicates the engine is reloading configuration.
	StateReloading State = "reloading"
	// StateStopping indicates the engine is shutting down.
	StateStopping State = "stopping"
)

// Engine is the central orchestrator for OpenLoadBalancer.
// It manages all components and coordinates their lifecycle.
type Engine struct {
	// Configuration
	config     *config.Config
	configPath string

	// Components
	logger          *logging.Logger
	metrics         *metrics.Registry
	tlsManager      *tls.Manager
	poolManager     *backend.PoolManager
	healthChecker   *health.Checker
	router          *router.Router
	proxy           *l7.HTTPProxy
	listeners       []listener.Listener
	adminServer     *admin.Server
	connManager     *conn.Manager
	connPoolMgr     *conn.PoolManager
	middlewareChain *middleware.Chain

	// Integrated subsystems
	mcpServer    *mcp.Server
	mcpTransport *mcp.HTTPTransport
	pluginMgr    *plugin.PluginManager
	clusterMgr   *cluster.ClusterManager // optional, nil if not configured
	discoveryMgr *discovery.Manager
	webUIHandler *webui.Handler

	// Runtime state
	state     State
	startTime time.Time
	mu        sync.RWMutex

	// Control channels
	stopCh   chan struct{}
	reloadCh chan struct{}
	wg       sync.WaitGroup
}

// Status represents the engine status for API responses.
type Status struct {
	State     string `json:"state"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
	Listeners int    `json:"listeners"`
	Pools     int    `json:"pools"`
	Routes    int    `json:"routes"`
}

// New creates a new engine from configuration.
// It initializes all components but does not start them.
func New(cfg *config.Config, configPath string) (*Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize logger
	logger := createLogger(cfg.Logging)

	// Initialize metrics registry
	metricsRegistry := metrics.NewRegistry()

	// Create TLS manager
	tlsMgr := tls.NewManager()

	// Create connection manager with limits
	connMgr := conn.NewManager(&conn.Config{
		MaxConnections: 10000,
		MaxPerSource:   100,
		MaxPerBackend:  1000,
		DrainTimeout:   30 * time.Second,
	})

	// Create connection pool manager
	connPoolMgr := conn.NewPoolManager(nil)

	// Create pool manager
	poolMgr := backend.NewPoolManager()

	// Create health checker
	healthChecker := health.NewChecker()

	// Create router
	rtr := router.NewRouter()

	// Create middleware chain
	mwChain := createMiddlewareChain(cfg, logger, metricsRegistry)

	// Create proxy
	proxyConfig := &l7.Config{
		Router:          rtr,
		PoolManager:     poolMgr,
		ConnPoolManager: connPoolMgr,
		HealthChecker:   healthChecker,
		MiddlewareChain: mwChain,
		ProxyTimeout:    60 * time.Second,
		DialTimeout:     10 * time.Second,
		MaxRetries:      3,
	}
	proxy := l7.NewHTTPProxy(proxyConfig)

	// Initialize Web UI handler
	webUIH, err := webui.NewHandler()
	if err != nil {
		logger.Warn("Failed to create Web UI handler, dashboard disabled",
			logging.Error(err),
		)
	}

	// Initialize plugin manager
	pluginMgr := plugin.NewPluginManager(plugin.DefaultPluginManagerConfig())
	pluginMgr.SetLogger(logger)
	pluginMgr.SetMetrics(metricsRegistry)
	pluginMgr.SetConfig(cfg)

	// Initialize discovery manager
	discoveryMgr := discovery.NewManager()

	// Create admin server
	adminCfg := &admin.Config{
		Address:       getAdminAddress(cfg),
		PoolManager:   poolMgr,
		Router:        rtr,
		HealthChecker: healthChecker,
		Metrics:       admin.NewDefaultMetrics(metricsRegistry),
	}

	// Wire optional admin components
	if webUIH != nil {
		adminCfg.WebUI = webUIH
	}

	e := &Engine{
		config:          cfg,
		configPath:      configPath,
		logger:          logger,
		metrics:         metricsRegistry,
		tlsManager:      tlsMgr,
		poolManager:     poolMgr,
		healthChecker:   healthChecker,
		router:          rtr,
		proxy:           proxy,
		connManager:     connMgr,
		connPoolMgr:     connPoolMgr,
		middlewareChain: mwChain,
		pluginMgr:       pluginMgr,
		discoveryMgr:    discoveryMgr,
		webUIHandler:    webUIH,
		state:           StateStopped,
		stopCh:          make(chan struct{}),
		reloadCh:        make(chan struct{}),
	}

	// Wire config getter and cert lister for admin API
	adminCfg.ConfigGetter = &engineConfigGetter{engine: e}
	adminCfg.CertLister = &engineCertLister{tlsMgr: tlsMgr}

	// Initialize MCP server with provider adapters
	mcpCfg := mcp.ServerConfig{
		Metrics:  &engineMetricsProvider{registry: metricsRegistry},
		Backends: &engineBackendProvider{poolMgr: poolMgr},
		Config:   &engineConfigProvider{engine: e},
		Routes:   &engineRouteProvider{rtr: rtr},
	}
	e.mcpServer = mcp.NewServer(mcpCfg)

	adminServer, err := admin.NewServer(adminCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin server: %w", err)
	}
	e.adminServer = adminServer

	// Set up admin server reload callback
	adminCfg.OnReload = func() error {
		return e.Reload()
	}

	logger.Info("Engine created",
		logging.String("version", version.Version),
		logging.String("config_path", configPath),
	)

	return e, nil
}

// Start initializes and starts all components in the correct order.
func (e *Engine) Start() error {
	e.mu.Lock()
	if e.state != StateStopped {
		e.mu.Unlock()
		return fmt.Errorf("engine is not stopped (current state: %s)", e.state)
	}
	e.state = StateStarting
	e.mu.Unlock()

	e.logger.Info("Starting engine",
		logging.String("version", version.Version),
		logging.String("commit", version.Commit),
	)

	// 1. Initialize TLS manager with certificates
	if e.config.TLS != nil {
		if e.config.TLS.CertFile != "" && e.config.TLS.KeyFile != "" {
			cert, err := e.tlsManager.LoadCertificate(e.config.TLS.CertFile, e.config.TLS.KeyFile)
			if err != nil {
				e.setState(StateStopped)
				return fmt.Errorf("failed to load TLS certificate: %w", err)
			}
			e.tlsManager.AddCertificate(cert)
			e.logger.Info("TLS certificate loaded",
				logging.String("cert_file", e.config.TLS.CertFile),
			)
		}
	}

	// 2. Start health checker
	e.healthChecker = health.NewChecker()

	// 3. Initialize backend pools and register backends with health checker
	if err := e.initializePools(); err != nil {
		e.setState(StateStopped)
		return fmt.Errorf("failed to initialize pools: %w", err)
	}

	// 4. Add routes to router
	if err := e.initializeRoutes(); err != nil {
		e.setState(StateStopped)
		return fmt.Errorf("failed to initialize routes: %w", err)
	}

	// 5. Start listeners
	if err := e.startListeners(); err != nil {
		e.setState(StateStopped)
		return fmt.Errorf("failed to start listeners: %w", err)
	}

	// 6. Start admin server
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		addr := getAdminAddress(e.config)
		e.logger.Info("Admin server starting",
			logging.String("address", addr),
		)
		if err := e.adminServer.Start(); err != nil && err != http.ErrServerClosed {
			e.logger.Error("Admin server error", logging.Error(err))
		}
	}()

	// 7. Start plugin manager
	if e.pluginMgr != nil {
		if err := e.pluginMgr.StartAll(); err != nil {
			e.logger.Warn("Plugin manager start failed", logging.Error(err))
		} else {
			e.logger.Info("Plugin manager started")
		}
	}

	// 8. Start MCP HTTP transport if admin address is available
	if e.mcpServer != nil {
		mcpAddr := getMCPAddress(e.config)
		if mcpAddr != "" {
			e.mcpTransport = mcp.NewHTTPTransport(e.mcpServer, mcpAddr)
			if err := e.mcpTransport.Start(); err != nil {
				e.logger.Warn("MCP HTTP transport start failed", logging.Error(err))
				e.mcpTransport = nil
			} else {
				e.logger.Info("MCP HTTP transport started",
					logging.String("address", e.mcpTransport.Addr()),
				)
			}
		}
	}

	// 9. Start cluster manager if configured
	if e.clusterMgr != nil {
		e.logger.Info("Cluster manager available")
	}

	// 10. Start discovery manager
	if e.discoveryMgr != nil {
		ctx := context.Background()
		if err := e.discoveryMgr.Start(ctx); err != nil {
			e.logger.Warn("Discovery manager start failed", logging.Error(err))
		} else {
			e.logger.Info("Discovery manager started")
		}
	}

	// 11. Install signal handlers
	e.setupSignalHandlers()

	// 12. Set running state
	e.mu.Lock()
	e.state = StateRunning
	e.startTime = time.Now()
	e.mu.Unlock()

	e.logger.Info("Engine started successfully",
		logging.Int("listeners", len(e.listeners)),
		logging.Int("pools", e.poolManager.PoolCount()),
		logging.Int("routes", e.router.RouteCount()),
	)

	return nil
}

// Shutdown gracefully stops all components in reverse order.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	if e.state != StateRunning && e.state != StateReloading {
		e.mu.Unlock()
		return fmt.Errorf("engine is not running (current state: %s)", e.state)
	}
	e.state = StateStopping
	e.mu.Unlock()

	e.logger.Info("Shutting down engine...")

	// 0a. Stop MCP transport
	if e.mcpTransport != nil {
		if err := e.mcpTransport.Stop(ctx); err != nil {
			e.logger.Warn("Failed to stop MCP transport", logging.Error(err))
		} else {
			e.logger.Info("MCP transport stopped")
		}
	}

	// 0b. Stop plugin manager
	if e.pluginMgr != nil {
		if err := e.pluginMgr.StopAll(); err != nil {
			e.logger.Warn("Failed to stop plugins", logging.Error(err))
		} else {
			e.logger.Info("Plugin manager stopped")
		}
	}

	// 0c. Stop cluster manager
	if e.clusterMgr != nil {
		e.clusterMgr.Stop()
		e.logger.Info("Cluster manager stopped")
	}

	// 0d. Stop discovery manager
	if e.discoveryMgr != nil {
		if err := e.discoveryMgr.Stop(); err != nil {
			e.logger.Warn("Failed to stop discovery manager", logging.Error(err))
		} else {
			e.logger.Info("Discovery manager stopped")
		}
	}

	// 1. Stop accepting new connections (close listeners)
	for _, l := range e.listeners {
		if err := l.Stop(ctx); err != nil {
			e.logger.Warn("Failed to stop listener",
				logging.String("name", l.Name()),
				logging.Error(err),
			)
		} else {
			e.logger.Info("Listener stopped",
				logging.String("name", l.Name()),
			)
		}
	}
	e.listeners = nil

	// 2. Drain active connections
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	if err := e.connManager.Drain(ctx); err != nil {
		e.logger.Warn("Connection drain incomplete", logging.Error(err))
	} else {
		e.logger.Info("All connections drained")
	}

	// 3. Stop proxy
	if e.proxy != nil {
		if err := e.proxy.Close(); err != nil {
			e.logger.Warn("Failed to close proxy", logging.Error(err))
		}
	}

	// 4. Stop health checker
	if e.healthChecker != nil {
		e.healthChecker.Stop()
		e.logger.Info("Health checker stopped")
	}

	// 5. Stop admin server
	if e.adminServer != nil {
		if err := e.adminServer.Stop(ctx); err != nil {
			e.logger.Warn("Failed to stop admin server", logging.Error(err))
		} else {
			e.logger.Info("Admin server stopped")
		}
	}

	// 6. Close connection pools
	if e.connPoolMgr != nil {
		e.connPoolMgr.Close()
	}

	// 7. Close connection manager
	e.connManager.CloseAll()

	// Signal stop
	close(e.stopCh)

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.logger.Info("All goroutines stopped")
	case <-ctx.Done():
		e.logger.Warn("Shutdown timeout waiting for goroutines")
	}

	e.mu.Lock()
	e.state = StateStopped
	e.mu.Unlock()

	e.logger.Info("Engine shutdown complete")

	return nil
}

// Reload hot-reloads configuration from disk.
// It loads new config, validates it, and applies changes atomically.
func (e *Engine) Reload() error {
	e.mu.Lock()
	if e.state != StateRunning {
		e.mu.Unlock()
		return fmt.Errorf("engine is not running (current state: %s)", e.state)
	}
	e.state = StateReloading
	e.mu.Unlock()

	e.logger.Info("Reloading configuration...")

	// Load new config
	newCfg, err := e.loadConfig()
	if err != nil {
		e.setState(StateRunning)
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate new config
	if err := e.validateConfig(newCfg); err != nil {
		e.setState(StateRunning)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Apply new configuration
	if err := e.applyConfig(newCfg); err != nil {
		e.setState(StateRunning)
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	e.mu.Lock()
	e.state = StateRunning
	e.mu.Unlock()

	e.logger.Info("Configuration reloaded successfully")

	return nil
}

// IsRunning returns true if the engine is started.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state == StateRunning
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
	return e.config
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

// setState sets the engine state (internal use only).
func (e *Engine) setState(state State) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = state
}

// initializePools creates pools and registers backends with health checker.
func (e *Engine) initializePools() error {
	for _, poolCfg := range e.config.Pools {
		pool := backend.NewPool(poolCfg.Name, poolCfg.Algorithm)

		// Create balancer for the pool
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
			b.SetState(backend.StateUp) // Start as Up, health checker will update
			if err := pool.AddBackend(b); err != nil {
				return fmt.Errorf("failed to add backend %s to pool %s: %w",
					backendCfg.ID, poolCfg.Name, err)
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

// startListeners creates and starts all listeners.
func (e *Engine) startListeners() error {
	for _, listenerCfg := range e.config.Listeners {
		opts := &listener.Options{
			Name:           listenerCfg.Name,
			Address:        listenerCfg.Address,
			Handler:        e.proxy,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			IdleTimeout:    120 * time.Second,
			MaxHeaderBytes: 1 << 20, // 1 MB
		}

		var l listener.Listener
		var err error

		if listenerCfg.TLS {
			// HTTPS listener
			l, err = listener.NewHTTPSListener(opts, e.tlsManager)
		} else {
			// HTTP listener
			l, err = listener.NewHTTPListener(opts)
		}

		if err != nil {
			return fmt.Errorf("failed to create listener %s: %w", listenerCfg.Name, err)
		}

		if err := l.Start(); err != nil {
			return fmt.Errorf("failed to start listener %s: %w", listenerCfg.Name, err)
		}

		e.listeners = append(e.listeners, l)

		e.logger.Info("Listener started",
			logging.String("name", listenerCfg.Name),
			logging.String("address", l.Address()),
			logging.Bool("tls", listenerCfg.TLS),
		)
	}

	return nil
}

// createLogger creates the logger based on configuration.
func createLogger(cfg *config.Logging) *logging.Logger {
	var output logging.Output

	if cfg == nil {
		// Default to stdout JSON
		output = logging.NewJSONOutput(os.Stdout)
	} else {
		switch cfg.Output {
		case "stdout":
			if cfg.Format == "text" {
				output = logging.NewTextOutput(os.Stdout)
			} else {
				output = logging.NewJSONOutput(os.Stdout)
			}
		case "stderr":
			if cfg.Format == "text" {
				output = logging.NewTextOutput(os.Stderr)
			} else {
				output = logging.NewJSONOutput(os.Stderr)
			}
		default:
			// File output - use rotating file output
			rotatingOutput, err := logging.NewRotatingFileOutput(logging.RotatingFileOptions{
				Filename:   cfg.Output,
				MaxSize:    100 * 1024 * 1024, // 100MB
				MaxBackups: 10,
				Compress:   true,
			})
			if err != nil {
				// Fallback to stdout
				output = logging.NewJSONOutput(os.Stdout)
			} else {
				output = rotatingOutput
			}
		}
	}

	logger := logging.New(output)
	if cfg != nil {
		logger.SetLevel(logging.ParseLevel(cfg.Level))
	}
	return logger
}

// createMiddlewareChain creates the middleware chain based on configuration.
func createMiddlewareChain(cfg *config.Config, logger *logging.Logger, registry *metrics.Registry) *middleware.Chain {
	chain := middleware.NewChain()

	// Request ID middleware (first)
	chain.Use(middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{}))

	// Real IP middleware
	if realIP, err := middleware.NewRealIPMiddleware(middleware.RealIPConfig{}); err == nil {
		chain.Use(realIP)
	}

	// CORS middleware (if configured)
	// TODO: Add CORS configuration from config

	// Rate limiter middleware (if configured)
	// TODO: Add rate limiter configuration from config

	// Metrics middleware
	chain.Use(middleware.NewMetricsMiddleware(registry))

	// Access log middleware (last)
	chain.Use(middleware.NewAccessLogMiddleware(middleware.AccessLogConfig{
		Logger: logger,
	}))

	return chain
}

// getAdminAddress returns the admin server address from config.
func getAdminAddress(cfg *config.Config) string {
	if cfg.Admin != nil && cfg.Admin.Address != "" {
		return cfg.Admin.Address
	}
	return ":8080"
}

// parseDuration parses a duration string with a default value.
func parseDuration(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}
