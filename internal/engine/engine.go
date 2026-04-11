// Package engine provides the central orchestrator for OpenLoadBalancer.
// It coordinates all components including listeners, proxy, health checking,
// routing, and configuration management.
package engine

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/acme"
	"github.com/openloadbalancer/olb/internal/admin"
	"github.com/openloadbalancer/olb/internal/backend"
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
	persister    *cluster.FilePersister  // optional, nil if no DataDir
	discoveryMgr *discovery.Manager
	webUIHandler *webui.Handler
	geoDNS       *geodns.GeoDNS    // optional, nil if not configured
	shadowMgr    *l7.ShadowManager // optional, nil if not configured

	// ACME/Let's Encrypt client
	acmeClient *acme.Client

	// Profiling cleanup function (non-nil when profiling is active)
	profilingCleanup func()

	// System metrics updater
	sysMetrics     *systemMetrics
	sysMetricsStop chan struct{}

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
	stopOnce sync.Once
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

	// Register system-level metrics (backends, connections, health)
	sysMetrics := registerSystemMetrics(metricsRegistry)

	// Create TLS manager
	tlsMgr := olbTLS.NewManager()

	// Create mTLS manager
	mtlsMgr := olbTLS.NewMTLSManager()

	// Create OCSP manager
	ocspMgr := olbTLS.NewOCSPManager(olbTLS.DefaultOCSPConfig())

	// Create connection manager with limits
	maxConns := 10000
	maxPerSource := 100
	maxPerBackend := 1000
	drainTimeout := 30 * time.Second
	if cfg.Server != nil {
		if cfg.Server.MaxConnections > 0 {
			maxConns = cfg.Server.MaxConnections
		}
		if cfg.Server.MaxConnectionsPerSource > 0 {
			maxPerSource = cfg.Server.MaxConnectionsPerSource
		}
		if cfg.Server.MaxConnectionsPerBackend > 0 {
			maxPerBackend = cfg.Server.MaxConnectionsPerBackend
		}
		if cfg.Server.DrainTimeout != "" {
			if d, err := time.ParseDuration(cfg.Server.DrainTimeout); err == nil {
				drainTimeout = d
			}
		}
	}
	connMgr := conn.NewManager(&conn.Config{
		MaxConnections: maxConns,
		MaxPerSource:   maxPerSource,
		MaxPerBackend:  maxPerBackend,
		DrainTimeout:   drainTimeout,
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
	proxyTimeout := 60 * time.Second
	dialTimeout := 10 * time.Second
	maxRetries := 3
	maxIdleConns := 100
	maxIdleConnsPerHost := 10
	if cfg.Server != nil {
		if cfg.Server.ProxyTimeout != "" {
			if d, err := time.ParseDuration(cfg.Server.ProxyTimeout); err == nil {
				proxyTimeout = d
			}
		}
		if cfg.Server.DialTimeout != "" {
			if d, err := time.ParseDuration(cfg.Server.DialTimeout); err == nil {
				dialTimeout = d
			}
		}
		if cfg.Server.MaxRetries > 0 {
			maxRetries = cfg.Server.MaxRetries
		}
		if cfg.Server.MaxIdleConns > 0 {
			maxIdleConns = cfg.Server.MaxIdleConns
		}
		if cfg.Server.MaxIdleConnsPerHost > 0 {
			maxIdleConnsPerHost = cfg.Server.MaxIdleConnsPerHost
		}
	}
	var idleConnTimeout time.Duration
	if cfg.Server != nil && cfg.Server.IdleConnTimeout != "" {
		if d, err := time.ParseDuration(cfg.Server.IdleConnTimeout); err == nil {
			idleConnTimeout = d
		}
	}
	proxyConfig := &l7.Config{
		Router:              rtr,
		PoolManager:         poolMgr,
		ConnPoolManager:     connPoolMgr,
		HealthChecker:       healthChecker,
		MiddlewareChain:     mwChain,
		ProxyTimeout:        proxyTimeout,
		DialTimeout:         dialTimeout,
		MaxRetries:          maxRetries,
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		IdleConnTimeout:     idleConnTimeout,
		PassiveChecker:      passiveChecker,
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
			DBPath:      cfg.GeoDNS.DBPath,
		})
		stats := geoDNSMgr.Stats()
		logger.Info("GeoDNS initialized",
			logging.Int("rules", len(cfg.GeoDNS.Rules)),
			logging.Bool("mmdb_loaded", stats.DBLoaded),
			logging.String("db_path", stats.DBPath),
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
	if cfg.Admin != nil {
		adminCfg.RateLimitMaxRequests = cfg.Admin.RateLimitMaxRequests
		adminCfg.RateLimitWindow = cfg.Admin.RateLimitWindow
		if cfg.Admin.Username != "" || cfg.Admin.BearerToken != "" {
			authCfg := &admin.AuthConfig{
				Username:     cfg.Admin.Username,
				Password:     cfg.Admin.Password,
				BearerTokens: []string{},
			}
			if cfg.Admin.BearerToken != "" {
				authCfg.BearerTokens = append(authCfg.BearerTokens, cfg.Admin.BearerToken)
			}
			adminCfg.Auth = authCfg
		}
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
		sysMetrics:      sysMetrics,
		sysMetricsStop:  make(chan struct{}),
		state:           StateStopped,
		stopCh:          make(chan struct{}),
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

	// Wire middleware status provider for admin API
	adminCfg.MiddlewareStatus = func() any { return e.buildMiddlewareStatus() }

	// Wire passive health checker callbacks to update backend state
	e.passiveChecker.OnBackendUnhealthy = func(addr string) {
		e.mu.RLock()
		pm := e.poolManager
		e.mu.RUnlock()
		if b := pm.GetBackendByAddress(addr); b != nil {
			b.SetState(backend.StateDown)
		}
		e.logger.Warn("Passive health check: backend marked unhealthy",
			logging.String("backend", addr),
		)
	}
	e.passiveChecker.OnBackendRecovered = func(addr string) {
		e.mu.RLock()
		pm := e.poolManager
		e.mu.RUnlock()
		if b := pm.GetBackendByAddress(addr); b != nil {
			b.SetState(backend.StateUp)
		}
		e.logger.Info("Passive health check: backend recovered",
			logging.String("backend", addr),
		)
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
		// Wire Raft proposer into admin server for clustered config changes
		if e.raftCluster != nil && e.adminServer != nil {
			e.adminServer.SetRaftProposer(&engineRaftProposer{raftCluster: e.raftCluster})
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
		} else if !strings.HasPrefix(profCfg.PprofAddr, "localhost:") &&
			!strings.HasPrefix(profCfg.PprofAddr, "127.0.0.1:") &&
			!strings.HasPrefix(profCfg.PprofAddr, "[::1]:") {
			logger.Warn("pprof endpoint is bound to a non-localhost address — runtime profiles may be exposed to the network",
				logging.String("pprof_addr", profCfg.PprofAddr),
			)
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
