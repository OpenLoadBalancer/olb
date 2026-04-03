// Package engine provides the central orchestrator for OpenLoadBalancer.
// It coordinates all components including listeners, proxy, health checking,
// routing, and configuration management.
package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/acme"
	"github.com/openloadbalancer/olb/internal/admin"
	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
	"github.com/openloadbalancer/olb/internal/cluster"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/conn"
	"github.com/openloadbalancer/olb/internal/discovery"
	"github.com/openloadbalancer/olb/internal/geodns"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/listener"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/mcp"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/middleware"
	"github.com/openloadbalancer/olb/internal/plugin"
	"github.com/openloadbalancer/olb/internal/profiling"
	"github.com/openloadbalancer/olb/internal/proxy/l4"
	"github.com/openloadbalancer/olb/internal/proxy/l7"
	"github.com/openloadbalancer/olb/internal/router"
	"github.com/openloadbalancer/olb/internal/security"
	olbTLS "github.com/openloadbalancer/olb/internal/tls"
	"github.com/openloadbalancer/olb/internal/waf"
	wafmcp "github.com/openloadbalancer/olb/internal/waf/mcp"
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
	tlsManager      *olbTLS.Manager
	mtlsManager     *olbTLS.MTLSManager
	ocspManager     *olbTLS.OCSPManager
	poolManager     *backend.PoolManager
	healthChecker   *health.Checker
	passiveChecker  *health.PassiveChecker
	router          *router.Router
	proxy           *l7.HTTPProxy
	listeners       []listener.Listener
	adminServer     *admin.Server
	connManager     *conn.Manager
	connPoolMgr     *conn.PoolManager
	middlewareChain *middleware.Chain

	// L4 proxies (kept for lifecycle management)
	udpProxies []*l4.UDPProxy

	// Integrated subsystems
	mcpServer    *mcp.Server
	mcpTransport *mcp.SSETransport
	pluginMgr    *plugin.PluginManager
	clusterMgr   *cluster.ClusterManager // optional, nil if not configured
	raftCluster  *cluster.Cluster        // optional, nil if not configured
	discoveryMgr *discovery.Manager
	webUIHandler *webui.Handler
	geoDNS       *geodns.GeoDNS    // optional, nil if not configured
	shadowMgr    *l7.ShadowManager // optional, nil if not configured

	// ACME/Let's Encrypt client
	acmeClient *acme.Client

	// Profiling cleanup function (non-nil when profiling is active)
	profilingCleanup func()

	// Log file output for SIGUSR1 reopening (non-nil when logging to file)
	logFileOutput *logging.RotatingFileOutput

	// Config file watcher
	configWatcher *config.Watcher

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
	logger, logFileOutput := createLoggerWithOutput(cfg.Logging)

	// Initialize metrics registry
	metricsRegistry := metrics.NewRegistry()

	// Create TLS manager
	tlsMgr := olbTLS.NewManager()

	// Create mTLS manager
	mtlsMgr := olbTLS.NewMTLSManager()

	// Create OCSP manager
	ocspMgr := olbTLS.NewOCSPManager(olbTLS.DefaultOCSPConfig())

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

	// Create passive health checker for real-traffic based health detection
	passiveChecker := health.NewPassiveChecker(nil)
	passiveChecker.OnBackendUnhealthy = func(addr string) {
		logger.Warn("Passive health check: backend marked unhealthy",
			logging.String("backend", addr),
		)
	}
	passiveChecker.OnBackendRecovered = func(addr string) {
		logger.Info("Passive health check: backend recovered",
			logging.String("backend", addr),
		)
	}

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

	// Initialize GeoDNS if configured
	var geoDNSMgr *geodns.GeoDNS
	if cfg.GeoDNS != nil && cfg.GeoDNS.Enabled {
		geoDNSMgr = geodns.New(geodns.Config{
			Enabled:     cfg.GeoDNS.Enabled,
			DefaultPool: cfg.GeoDNS.DefaultPool,
			Rules:       convertGeoDNSRules(cfg.GeoDNS.Rules),
		})
		logger.Info("GeoDNS initialized",
			logging.Int("rules", len(cfg.GeoDNS.Rules)),
		)
	}

	// Initialize shadow manager if configured
	var shadowManager *l7.ShadowManager
	if cfg.Shadow != nil && cfg.Shadow.Enabled {
		timeout := 30 * time.Second
		if cfg.Shadow.Timeout != "" {
			if d, err := time.ParseDuration(cfg.Shadow.Timeout); err == nil {
				timeout = d
			}
		}
		shadowManager = l7.NewShadowManager(l7.ShadowConfig{
			Enabled:     cfg.Shadow.Enabled,
			Percentage:  cfg.Shadow.Percentage,
			CopyHeaders: cfg.Shadow.CopyHeaders,
			CopyBody:    cfg.Shadow.CopyBody,
			Timeout:     timeout,
		})
		logger.Info("Shadow manager initialized",
			logging.Float64("percentage", cfg.Shadow.Percentage),
		)
	}

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
		logFileOutput:   logFileOutput,
		metrics:         metricsRegistry,
		tlsManager:      tlsMgr,
		mtlsManager:     mtlsMgr,
		ocspManager:     ocspMgr,
		poolManager:     poolMgr,
		healthChecker:   healthChecker,
		passiveChecker:  passiveChecker,
		router:          rtr,
		proxy:           proxy,
		connManager:     connMgr,
		connPoolMgr:     connPoolMgr,
		middlewareChain: mwChain,
		pluginMgr:       pluginMgr,
		discoveryMgr:    discoveryMgr,
		webUIHandler:    webUIH,
		geoDNS:          geoDNSMgr,
		shadowMgr:       shadowManager,
		state:           StateStopped,
		stopCh:          make(chan struct{}),
		reloadCh:        make(chan struct{}),
	}

	// Wire config getter and cert lister for admin API
	adminCfg.ConfigGetter = &engineConfigGetter{engine: e}
	adminCfg.CertLister = &engineCertLister{tlsMgr: tlsMgr}

	// Wire WAF status provider for admin API
	if e.middlewareChain != nil {
		if wafMW, ok := e.middlewareChain.Get("waf").(*waf.WAFMiddleware); ok {
			adminCfg.WAFStatus = func() any { return wafMW.Status() }
		}
	}

	// Initialize MCP server with provider adapters
	mcpCfg := mcp.ServerConfig{
		Metrics:  &engineMetricsProvider{registry: metricsRegistry},
		Backends: &engineBackendProvider{poolMgr: poolMgr},
		Config:   &engineConfigProvider{engine: e},
		Routes:   &engineRouteProvider{rtr: rtr},
	}
	e.mcpServer = mcp.NewServer(mcpCfg)

	// Register WAF MCP tools if WAF is enabled
	if e.mcpServer != nil && e.middlewareChain != nil {
		if wafMW, ok := e.middlewareChain.Get("waf").(*waf.WAFMiddleware); ok {
			wafmcp.RegisterTools(e.mcpServer, wafMW)
		}
	}

	// Set up admin server reload callback before creating the server
	adminCfg.OnReload = func() error {
		return e.Reload()
	}

	adminServer, err := admin.NewServer(adminCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin server: %w", err)
	}
	e.adminServer = adminServer

	// Initialize cluster manager if configured
	if cfg.Cluster != nil && cfg.Cluster.Enabled {
		if err := e.initCluster(cfg.Cluster, logger); err != nil {
			logger.Warn("Failed to initialize cluster, running standalone",
				logging.Error(err),
			)
		}
	}

	// Initialize ACME/Let's Encrypt client if configured
	if cfg.TLS != nil && cfg.TLS.ACME != nil && cfg.TLS.ACME.Enabled {
		acmeCfg := acme.DefaultConfig()
		if cfg.TLS.ACME.Email != "" {
			acmeCfg.Contact = []string{"mailto:" + cfg.TLS.ACME.Email}
		}
		acmeClient, err := acme.New(acmeCfg)
		if err != nil {
			logger.Warn("Failed to initialize ACME client", logging.Error(err))
		} else {
			e.acmeClient = acmeClient
			logger.Info("ACME client initialized",
				logging.String("directory", acmeCfg.DirectoryURL),
			)
		}
	}

	// Initialize profiling if configured
	if cfg.Profiling != nil && cfg.Profiling.Enabled {
		profCfg := profiling.ProfileConfig{
			CPUProfilePath:       cfg.Profiling.CPUProfilePath,
			MemProfilePath:       cfg.Profiling.MemProfilePath,
			BlockProfileRate:     cfg.Profiling.BlockProfileRate,
			MutexProfileFraction: cfg.Profiling.MutexProfileFraction,
			EnablePprof:          true,
			PprofAddr:            cfg.Profiling.PprofAddr,
		}
		if profCfg.PprofAddr == "" {
			profCfg.PprofAddr = "localhost:6060"
		}
		cleanup, err := profiling.Apply(profCfg)
		if err != nil {
			logger.Warn("Failed to initialize profiling", logging.Error(err))
		} else {
			e.profilingCleanup = cleanup
			logger.Info("Profiling enabled",
				logging.String("pprof_addr", profCfg.PprofAddr),
			)
		}
	}

	logger.Info("Engine created",
		logging.String("version", version.Version),
		logging.String("config_path", configPath),
	)

	return e, nil
}

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

	// Create a simple state machine for the cluster
	stateMachine := &engineStateMachine{}

	raftCluster, err := cluster.New(raftCfg, stateMachine)
	if err != nil {
		return fmt.Errorf("failed to create Raft cluster: %w", err)
	}
	e.raftCluster = raftCluster

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
		raftCluster.SetTransport(transport)
		if err := transport.Start(); err != nil {
			logger.Warn("Failed to start cluster transport", logging.Error(err))
		} else {
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

// engineStateMachine implements cluster.StateMachine for the engine.
type engineStateMachine struct{}

func (sm *engineStateMachine) Apply(command []byte) ([]byte, error) {
	// Simple passthrough - in production this would apply config changes
	return command, nil
}

func (sm *engineStateMachine) Snapshot() ([]byte, error) {
	return []byte("{}"), nil
}

func (sm *engineStateMachine) Restore(snapshot []byte) error {
	return nil
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

	// 2. Start OCSP manager for certificate stapling
	if e.ocspManager != nil {
		if err := e.ocspManager.Start(); err != nil {
			e.logger.Warn("OCSP manager start failed", logging.Error(err))
		} else {
			e.logger.Info("OCSP manager started")
		}
	}

	// 3. Start health checker
	e.healthChecker = health.NewChecker()

	// 4. Initialize backend pools and register backends with health checker
	if err := e.initializePools(); err != nil {
		e.setState(StateStopped)
		return fmt.Errorf("failed to initialize pools: %w", err)
	}

	// 5. Add routes to router
	if err := e.initializeRoutes(); err != nil {
		e.setState(StateStopped)
		return fmt.Errorf("failed to initialize routes: %w", err)
	}

	// 6. Start listeners (HTTP, HTTPS with mTLS, TCP, UDP)
	if err := e.startListeners(); err != nil {
		e.setState(StateStopped)
		return fmt.Errorf("failed to start listeners: %w", err)
	}

	// 7. Start admin server
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

	// 8. Start plugin manager
	if e.pluginMgr != nil {
		if err := e.pluginMgr.StartAll(); err != nil {
			e.logger.Warn("Plugin manager start failed", logging.Error(err))
		} else {
			e.logger.Info("Plugin manager started")
		}
	}

	// 8a. Start passive health checker
	if e.passiveChecker != nil {
		e.passiveChecker.Start()
		e.logger.Info("Passive health checker started")
	}

	// 9. Start MCP SSE transport (spec-compliant with auth + audit)
	if e.mcpServer != nil {
		mcpAddr := getMCPAddress(e.config)
		if mcpAddr != "" {
			sseConfig := mcp.SSETransportConfig{
				Addr:     mcpAddr,
				AuditLog: e.config.Admin != nil && e.config.Admin.MCPAudit,
			}
			if e.config.Admin != nil && e.config.Admin.MCPToken != "" {
				sseConfig.BearerToken = e.config.Admin.MCPToken
			}
			if e.config.Middleware != nil && e.config.Middleware.CORS != nil {
				sseConfig.AllowedOrigins = e.config.Middleware.CORS.AllowedOrigins
			}
			if sseConfig.AuditLog {
				sseConfig.AuditFunc = func(tool, params, clientAddr string, dur time.Duration, err error) {
					e.logger.Info("MCP tool call",
						logging.String("tool", tool),
						logging.String("client", clientAddr),
						logging.String("duration", dur.String()),
					)
				}
			}
			e.mcpTransport = mcp.NewSSETransport(e.mcpServer, sseConfig)
			if err := e.mcpTransport.Start(); err != nil {
				e.logger.Warn("MCP SSE transport start failed", logging.Error(err))
				e.mcpTransport = nil
			} else {
				e.logger.Info("MCP SSE transport started",
					logging.String("address", e.mcpTransport.Addr()),
					logging.Bool("auth", sseConfig.BearerToken != ""),
					logging.Bool("audit", sseConfig.AuditLog),
				)
			}
		}
	}

	// 10. Start cluster manager if configured
	if e.clusterMgr != nil {
		if e.config.Cluster != nil && len(e.config.Cluster.Peers) > 0 {
			if err := e.clusterMgr.Join(e.config.Cluster.Peers); err != nil {
				e.logger.Warn("Failed to join cluster", logging.Error(err))
			} else {
				e.logger.Info("Cluster manager started, joined cluster")
			}
		} else {
			e.logger.Info("Cluster manager available (standalone mode)")
		}
	}

	// 11. Start discovery manager
	if e.discoveryMgr != nil {
		ctx := context.Background()
		if err := e.discoveryMgr.Start(ctx); err != nil {
			e.logger.Warn("Discovery manager start failed", logging.Error(err))
		} else {
			e.logger.Info("Discovery manager started")
		}
	}

	// 12. Start config file watcher for auto-reload
	if e.configPath != "" {
		e.startConfigWatcher()
	}

	// 13. Install signal handlers
	e.setupSignalHandlers()

	// 14. Set running state
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

// startConfigWatcher creates and starts a file watcher for the config file.
// On change, it triggers a debounced Reload() to coalesce rapid file changes
// from editors that perform multiple write operations.
func (e *Engine) startConfigWatcher() {
	var debounceTimer *time.Timer
	var debounceMu sync.Mutex

	debouncedReload := func(path string) {
		debounceMu.Lock()
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
			e.logger.Info("Config file changed, triggering reload",
				logging.String("path", path),
			)
			if err := e.Reload(); err != nil {
				e.logger.Error("Auto-reload failed", logging.Error(err))
			}
		})
		debounceMu.Unlock()
	}

	watcher, err := config.NewWatcher(
		e.configPath,
		2*time.Second,
		func(path string, data []byte) {
			debouncedReload(path)
		},
		func(path string, err error) {
			e.logger.Warn("Config watcher error",
				logging.String("path", path),
				logging.Error(err),
			)
		},
	)
	if err != nil {
		e.logger.Warn("Failed to create config file watcher", logging.Error(err))
		return
	}

	e.configWatcher = watcher
	watcher.Start(context.Background())
	e.logger.Info("Config file watcher started",
		logging.String("path", e.configPath),
	)
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

	// 0. Stop config file watcher
	if e.configWatcher != nil {
		e.configWatcher.Stop()
		e.logger.Info("Config file watcher stopped")
	}

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

	// 0e. Stop OCSP manager
	if e.ocspManager != nil {
		if err := e.ocspManager.Stop(); err != nil {
			e.logger.Warn("Failed to stop OCSP manager", logging.Error(err))
		} else {
			e.logger.Info("OCSP manager stopped")
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

	// 1a. Stop UDP proxies
	for _, udpProxy := range e.udpProxies {
		if err := udpProxy.Stop(); err != nil {
			e.logger.Warn("Failed to stop UDP proxy", logging.Error(err))
		}
	}
	e.udpProxies = nil

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

	// 4. Stop health checkers
	if e.passiveChecker != nil {
		e.passiveChecker.Stop()
		e.logger.Info("Passive health checker stopped")
	}
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

	// 8. Stop profiling and write memory profile if configured
	if e.profilingCleanup != nil {
		e.profilingCleanup()
		e.logger.Info("Profiling stopped")
	}

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
		case "round_robin", "rr":
			bal = balancer.NewRoundRobin()
		case "weighted_round_robin", "wrr":
			bal = balancer.NewWeightedRoundRobin()
		case "least_connections", "lc":
			bal = balancer.NewLeastConnections()
		case "weighted_least_connections", "wlc":
			bal = balancer.NewWeightedLeastConnections()
		case "least_response_time", "lrt":
			bal = balancer.NewLeastResponseTime()
		case "weighted_least_response_time", "wlrt":
			bal = balancer.NewWeightedLeastResponseTime()
		case "ip_hash", "iphash":
			bal = balancer.NewIPHash()
		case "consistent_hash", "ch", "ketama":
			bal = balancer.NewConsistentHash(balancer.DefaultVirtualNodes)
		case "maglev":
			bal = balancer.NewMaglev()
		case "power_of_two", "p2c":
			bal = balancer.NewPowerOfTwo()
		case "random":
			bal = balancer.NewRandom()
		case "weighted_random", "wrandom":
			bal = balancer.NewWeightedRandom()
		case "ring_hash", "ringhash":
			bal = balancer.NewRingHash()
		case "sticky":
			bal = balancer.NewSticky(balancer.NewRoundRobin(), nil)
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
// Supports HTTP, HTTPS (with optional mTLS), TCP (L4), and UDP (L4) protocols.
func (e *Engine) startListeners() error {
	for _, listenerCfg := range e.config.Listeners {
		switch listenerCfg.Protocol {
		case "tcp":
			// L4 TCP proxy listener
			if err := e.startTCPListener(listenerCfg); err != nil {
				return fmt.Errorf("failed to start TCP listener %s: %w", listenerCfg.Name, err)
			}

		case "udp":
			// L4 UDP proxy
			if err := e.startUDPListener(listenerCfg); err != nil {
				return fmt.Errorf("failed to start UDP listener %s: %w", listenerCfg.Name, err)
			}

		default:
			// HTTP/HTTPS (L7) listener
			if err := e.startHTTPListener(listenerCfg); err != nil {
				return fmt.Errorf("failed to start listener %s: %w", listenerCfg.Name, err)
			}
		}
	}

	return nil
}

// startTCPListener creates and starts a TCP (L4) proxy listener.
func (e *Engine) startTCPListener(listenerCfg *config.Listener) error {
	// Resolve the backend pool for this TCP listener
	poolName := listenerCfg.Pool
	if poolName == "" && len(listenerCfg.Routes) > 0 {
		poolName = listenerCfg.Routes[0].Pool
	}

	pool := e.poolManager.GetPool(poolName)
	if pool == nil {
		return fmt.Errorf("backend pool %q not found for TCP listener %s", poolName, listenerCfg.Name)
	}

	// Create TCP proxy with simple round-robin balancer
	tcpBalancer := l4.NewSimpleBalancer()
	tcpProxy := l4.NewTCPProxy(pool, tcpBalancer, l4.DefaultTCPProxyConfig())

	// Create TCP listener
	tcpListener, err := l4.NewTCPListener(&l4.TCPListenerOptions{
		Name:    listenerCfg.Name,
		Address: listenerCfg.Address,
		Proxy:   tcpProxy,
	})
	if err != nil {
		return err
	}

	if err := tcpListener.Start(); err != nil {
		return err
	}

	e.listeners = append(e.listeners, tcpListener)

	e.logger.Info("TCP listener started",
		logging.String("name", listenerCfg.Name),
		logging.String("address", tcpListener.Address()),
		logging.String("pool", poolName),
	)

	return nil
}

// startUDPListener creates and starts a UDP (L4) proxy.
func (e *Engine) startUDPListener(listenerCfg *config.Listener) error {
	// Resolve the backend pool for this UDP listener
	poolName := listenerCfg.Pool
	if poolName == "" && len(listenerCfg.Routes) > 0 {
		poolName = listenerCfg.Routes[0].Pool
	}

	pool := e.poolManager.GetPool(poolName)
	if pool == nil {
		return fmt.Errorf("backend pool %q not found for UDP listener %s", poolName, listenerCfg.Name)
	}

	// Create UDP proxy with simple round-robin balancer
	udpBalancer := l4.NewSimpleBalancer()
	udpConfig := l4.DefaultUDPProxyConfig()
	udpConfig.ListenAddr = listenerCfg.Address
	udpConfig.BackendPool = poolName

	udpProxy := l4.NewUDPProxy(pool, udpBalancer, udpConfig)

	if err := udpProxy.Start(); err != nil {
		return err
	}

	e.udpProxies = append(e.udpProxies, udpProxy)

	e.logger.Info("UDP listener started",
		logging.String("name", listenerCfg.Name),
		logging.String("address", listenerCfg.Address),
		logging.String("pool", poolName),
	)

	return nil
}

// startHTTPListener creates and starts an HTTP or HTTPS (L7) listener.
// If the listener has mTLS configured, it applies client certificate settings.
// Slow loris protection settings from the security package are applied.
func (e *Engine) startHTTPListener(listenerCfg *config.Listener) error {
	// Use security package slow loris defaults for hardened timeouts
	slp := security.DefaultSlowLorisProtection()

	opts := &listener.Options{
		Name:           listenerCfg.Name,
		Address:        listenerCfg.Address,
		Handler:        e.proxy,
		ReadTimeout:    slp.ReadTimeout,
		HeaderTimeout:  slp.ReadHeaderTimeout,
		WriteTimeout:   slp.WriteTimeout,
		IdleTimeout:    slp.IdleTimeout,
		MaxHeaderBytes: slp.MaxHeaderBytes,
	}

	var l listener.Listener
	var err error

	if listenerCfg.IsTLS() {
		// Check for mTLS configuration
		if listenerCfg.MTLS != nil && listenerCfg.MTLS.Enabled {
			l, err = e.createMTLSListener(opts, listenerCfg)
		} else {
			// Standard HTTPS listener
			l, err = listener.NewHTTPSListener(opts, e.tlsManager)
		}
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
		logging.Bool("tls", listenerCfg.IsTLS()),
		logging.Bool("mtls", listenerCfg.MTLS != nil && listenerCfg.MTLS.Enabled),
	)

	return nil
}

// createMTLSListener creates an HTTPS listener with mTLS (mutual TLS) support.
// It loads client CA certificates and configures client certificate verification.
func (e *Engine) createMTLSListener(opts *listener.Options, listenerCfg *config.Listener) (listener.Listener, error) {
	mtlsCfg := &olbTLS.MTLSConfig{
		Enabled:   true,
		ClientCAs: listenerCfg.MTLS.ClientCAs,
	}

	// Parse client auth policy
	if listenerCfg.MTLS.ClientAuth != "" {
		policy, err := olbTLS.ParseClientAuthPolicy(listenerCfg.MTLS.ClientAuth)
		if err != nil {
			return nil, fmt.Errorf("invalid client_auth policy: %w", err)
		}
		mtlsCfg.ClientAuth = policy
	} else {
		mtlsCfg.ClientAuth = olbTLS.RequireAndVerifyClientCert
	}

	// Build mTLS-enabled TLS config using the MTLSManager
	tlsConfig, err := e.mtlsManager.BuildServerTLSConfig(
		listenerCfg.Name,
		mtlsCfg,
		e.tlsManager.GetCertificateCallback(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build mTLS config: %w", err)
	}

	// Apply security-hardened TLS defaults (min version, cipher suites, curves)
	secureDefaults := security.DefaultTLSConfig()
	if tlsConfig.MinVersion < secureDefaults.MinVersion {
		tlsConfig.MinVersion = secureDefaults.MinVersion
	}
	if len(tlsConfig.CipherSuites) == 0 {
		tlsConfig.CipherSuites = secureDefaults.CipherSuites
	}
	if len(tlsConfig.CurvePreferences) == 0 {
		tlsConfig.CurvePreferences = secureDefaults.CurvePreferences
	}

	// Create a custom HTTPS listener that uses our mTLS-configured tls.Config
	return newMTLSHTTPSListener(opts, tlsConfig)
}

// mtlsHTTPSListener is an HTTPS listener with custom TLS configuration for mTLS.
type mtlsHTTPSListener struct {
	name       string
	address    string
	handler    http.Handler
	tlsConfig  *tls.Config
	server     *http.Server
	ln         interface{ Addr() fmt.Stringer }
	running    bool
	mu         sync.RWMutex
	actualAddr string
}

// newMTLSHTTPSListener creates a new HTTPS listener with mTLS support.
func newMTLSHTTPSListener(opts *listener.Options, tlsConfig *tls.Config) (*mtlsHTTPSListener, error) {
	return &mtlsHTTPSListener{
		name:      opts.Name,
		address:   opts.Address,
		handler:   opts.Handler,
		tlsConfig: tlsConfig,
	}, nil
}

// Start begins listening for mTLS connections.
func (l *mtlsHTTPSListener) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return fmt.Errorf("listener already running")
	}

	srv := &http.Server{
		Addr:           l.address,
		Handler:        l.handler,
		TLSConfig:      l.tlsConfig,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	l.server = srv
	l.running = true

	// Use ListenAndServeTLS with empty cert/key since tlsConfig has GetCertificate
	go func() {
		err := srv.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
		}
	}()

	// Store the address (may differ from configured if port 0 was used)
	l.actualAddr = l.address

	return nil
}

// Stop gracefully shuts down the mTLS HTTPS listener.
func (l *mtlsHTTPSListener) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running {
		return fmt.Errorf("listener not running")
	}

	l.running = false
	if l.server != nil {
		return l.server.Shutdown(ctx)
	}
	return nil
}

// Name returns the listener name.
func (l *mtlsHTTPSListener) Name() string { return l.name }

// Address returns the listener address.
func (l *mtlsHTTPSListener) Address() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.actualAddr
}

// IsRunning returns true if the listener is active.
func (l *mtlsHTTPSListener) IsRunning() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.running
}

// createLoggerWithOutput creates the logger and optionally returns a rotating file
// output reference for SIGUSR1 log reopening.
func createLoggerWithOutput(cfg *config.Logging) (*logging.Logger, *logging.RotatingFileOutput) {
	var output logging.Output
	var rotatingOut *logging.RotatingFileOutput

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
				rotatingOut = rotatingOutput
			}
		}
	}

	logger := logging.New(output)
	if cfg != nil {
		logger.SetLevel(logging.ParseLevel(cfg.Level))
	}
	return logger, rotatingOut
}

// createMiddlewareChain creates the middleware chain based on configuration.
func createMiddlewareChain(cfg *config.Config, logger *logging.Logger, registry *metrics.Registry) *middleware.Chain {
	chain := middleware.NewChain()

	// Panic Recovery (priority 1) — MUST be first to catch panics from all downstream middleware
	chain.Use(middleware.NewRecoveryMiddleware(middleware.RecoveryConfig{
		LogFunc: func(panicVal any, stack string) {
			logger.Error("panic recovered",
				logging.String("panic", fmt.Sprintf("%v", panicVal)),
				logging.String("stack", stack),
			)
		},
	}))

	// IP Filter (priority 100)
	if cfg.Middleware != nil && cfg.Middleware.IPFilter != nil && cfg.Middleware.IPFilter.Enabled {
		ipCfg := cfg.Middleware.IPFilter
		ipFilter, err := middleware.NewIPFilterMiddleware(middleware.IPFilterConfig{
			AllowList: ipCfg.AllowList, DenyList: ipCfg.DenyList, DefaultAction: ipCfg.DefaultAction,
		})
		if err == nil {
			chain.Use(ipFilter)
		}
	}

	// Max Body Size (priority 50) — MUST run before WAF to reject oversized
	// payloads before they enter the detection pipeline (prevents DoS).
	if cfg.Middleware != nil && cfg.Middleware.MaxBodySize != nil && cfg.Middleware.MaxBodySize.Enabled {
		maxSize := cfg.Middleware.MaxBodySize.MaxSize
		if maxSize <= 0 {
			maxSize = 10 * 1024 * 1024 // 10 MB default
		}
		chain.Use(middleware.NewBodyLimitMiddleware(middleware.BodyLimitConfig{MaxSize: maxSize}))
	}

	// WAF (priority 100) — 6-layer security pipeline
	if cfg.WAF != nil && cfg.WAF.Enabled {
		wafMW, err := waf.NewWAFMiddleware(waf.WAFMiddlewareConfig{
			Config:          cfg.WAF,
			MetricsRegistry: registry,
		})
		if err == nil {
			chain.Use(wafMW)
		}
	}

	// Real IP (priority 300)
	if realIP, err := middleware.NewRealIPMiddleware(middleware.RealIPConfig{}); err == nil {
		chain.Use(realIP)
	}

	// Request ID (priority 400)
	chain.Use(middleware.NewRequestIDMiddleware(middleware.RequestIDConfig{}))

	// Request Timeout (priority 450) — prevent hung backends from blocking clients
	if cfg.Middleware != nil && cfg.Middleware.Timeout != nil && cfg.Middleware.Timeout.Enabled {
		timeout := parseDuration(cfg.Middleware.Timeout.Timeout, 60*time.Second)
		chain.Use(middleware.NewTimeoutMiddleware(middleware.TimeoutConfig{Timeout: timeout}))
	}

	// Rate Limiter (priority 500)
	if cfg.Middleware != nil && cfg.Middleware.RateLimit != nil && cfg.Middleware.RateLimit.Enabled {
		rl := cfg.Middleware.RateLimit
		rps := rl.RequestsPerSecond
		if rps <= 0 {
			rps = 100
		}
		burst := rl.BurstSize
		if burst <= 0 {
			burst = 200
		}
		if rateLimiter, err := middleware.NewRateLimitMiddleware(middleware.RateLimitConfig{
			RequestsPerSecond: rps, BurstSize: burst,
		}); err == nil {
			chain.Use(rateLimiter)
		}
	}

	// Circuit Breaker (priority 550)
	if cfg.Middleware != nil && cfg.Middleware.CircuitBreaker != nil && cfg.Middleware.CircuitBreaker.Enabled {
		cbCfg := middleware.DefaultCircuitBreakerConfig()
		if cfg.Middleware.CircuitBreaker.ErrorThreshold > 0 {
			cbCfg.ErrorThreshold = cfg.Middleware.CircuitBreaker.ErrorThreshold
		}
		chain.Use(middleware.NewCircuitBreaker(cbCfg))
	}

	// CORS (priority 600)
	if cfg.Middleware != nil && cfg.Middleware.CORS != nil && cfg.Middleware.CORS.Enabled {
		c := cfg.Middleware.CORS
		origins := c.AllowedOrigins
		if len(origins) == 0 {
			origins = []string{"*"}
		}
		methods := c.AllowedMethods
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
		}
		chain.Use(middleware.NewCORSMiddleware(middleware.CORSConfig{
			AllowedOrigins: origins, AllowedMethods: methods, AllowedHeaders: c.AllowedHeaders,
			AllowCredentials: c.AllowCredentials, MaxAge: time.Duration(c.MaxAge) * time.Second,
		}))
	}

	// Headers (priority 700)
	if cfg.Middleware != nil && cfg.Middleware.Headers != nil && cfg.Middleware.Headers.Enabled {
		h := cfg.Middleware.Headers
		chain.Use(middleware.NewHeadersMiddleware(middleware.HeadersConfig{
			RequestAdd: h.RequestAdd, ResponseAdd: h.ResponseAdd,
		}))
	}

	// Compression (priority 800)
	if cfg.Middleware != nil && cfg.Middleware.Compression != nil && cfg.Middleware.Compression.Enabled {
		if comp, err := middleware.NewCompressionMiddleware(middleware.CompressionConfig{
			MinSize: cfg.Middleware.Compression.MinSize,
			Level:   cfg.Middleware.Compression.Level,
		}); err == nil {
			chain.Use(comp)
		}
	}

	// Retry (priority 750)
	if cfg.Middleware != nil && cfg.Middleware.Retry != nil && cfg.Middleware.Retry.Enabled {
		retryCfg := middleware.DefaultRetryConfig()
		if cfg.Middleware.Retry.MaxRetries > 0 {
			retryCfg.MaxRetries = cfg.Middleware.Retry.MaxRetries
		}
		chain.Use(middleware.NewRetryMiddleware(retryCfg))
	}

	// Cache (priority 750)
	if cfg.Middleware != nil && cfg.Middleware.Cache != nil && cfg.Middleware.Cache.Enabled {
		chain.Use(middleware.NewCacheMiddleware(middleware.DefaultCacheConfig()))
	}

	// Metrics (priority 900)
	chain.Use(middleware.NewMetricsMiddleware(registry))

	// Access Log (priority 1000)
	chain.Use(middleware.NewAccessLogMiddleware(middleware.AccessLogConfig{Logger: logger}))

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

// convertGeoDNSRules converts config.GeoDNSRule to geodns.GeoRule.
func convertGeoDNSRules(rules []config.GeoDNSRule) []geodns.GeoRule {
	result := make([]geodns.GeoRule, 0, len(rules))
	for _, r := range rules {
		result = append(result, geodns.GeoRule{
			ID:       r.ID,
			Country:  r.Country,
			Region:   r.Region,
			Pool:     r.Pool,
			Fallback: r.Fallback,
			Weight:   r.Weight,
			Headers:  r.Headers,
		})
	}
	return result
}
