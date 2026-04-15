package engine

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/mcp"
	"github.com/openloadbalancer/olb/pkg/version"
)

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

	// 3. Start health checker (stop any previous instance from a prior Start call)
	if e.healthChecker != nil {
		e.healthChecker.Stop()
	}
	e.healthChecker = health.NewChecker()
	e.adminServer.SetHealthChecker(e.healthChecker)

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
	adminAddr := getAdminAddress(e.config)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("Admin server panic recovered", logging.Any("panic", r))
			}
		}()
		addr := adminAddr
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
		ctx, cancel := context.WithCancel(context.Background())
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("Discovery cancel panic recovered", logging.Any("panic", r))
				}
			}()
			<-e.stopCh
			cancel()
		}()
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

	// 12a. Start system metrics refresh goroutine
	if e.sysMetrics != nil {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("System metrics panic recovered", logging.Any("panic", r))
				}
			}()
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			// Initial update (read pointers under RLock to avoid race with applyConfig)
			e.mu.RLock()
			pm, hc, cp := e.poolManager, e.healthChecker, e.connPoolMgr
			e.mu.RUnlock()
			e.sysMetrics.updateSystemMetrics(pm, hc, cp)
			for {
				select {
				case <-ticker.C:
					e.mu.RLock()
					pm, hc, cp := e.poolManager, e.healthChecker, e.connPoolMgr
					e.mu.RUnlock()
					e.sysMetrics.updateSystemMetrics(pm, hc, cp)
				case <-e.sysMetricsStop:
					return
				}
			}
		}()
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

	// 0. Stop rollback timer
	e.stopRollbackTimer()

	// 0a. Stop config file watcher
	if e.configWatcher != nil {
		e.configWatcher.Stop()
		e.logger.Info("Config file watcher stopped")
	}

	// 0a. Stop system metrics refresh
	if e.sysMetricsStop != nil {
		close(e.sysMetricsStop)
		e.logger.Info("System metrics updater stopped")
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

	// 0e. Stop GeoDNS
	if e.geoDNS != nil {
		e.geoDNS.Close()
		e.logger.Info("GeoDNS stopped")
	}

	// 0f. Stop OCSP manager
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

	// 3a. Wait for in-flight shadow requests
	if e.shadowMgr != nil {
		e.shadowMgr.Wait()
		e.logger.Info("Shadow requests drained")
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

	// 9. Zero secrets from middleware
	if e.middlewareChain != nil {
		e.middlewareChain.ZeroSecrets()
	}

	// Signal stop
	e.stopOnce.Do(func() {
		close(e.stopCh)
	})

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("WaitGroup drain panic recovered", logging.Any("panic", r))
			}
		}()
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
