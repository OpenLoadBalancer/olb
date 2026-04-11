//go:build ignore

// Package main implements an example OpenLoadBalancer plugin that demonstrates
// the full plugin API. This plugin registers a custom middleware that logs
// rate-limited requests (HTTP 429 responses) and subscribes to system events
// such as backend state changes and configuration reloads.
//
// # Build Instructions
//
// Go plugins use -buildmode=plugin which produces a shared object (.so) file.
// This build mode is supported on Linux and macOS (not Windows).
//
//	cd examples/plugins/ratelimit-logger
//	go build -buildmode=plugin -o ratelimit-logger.so .
//
// # Install
//
// Copy the .so file into the OLB plugins directory (default: plugins/):
//
//	cp ratelimit-logger.so /etc/olb/plugins/
//
// OLB will automatically load all .so files from the plugin directory on startup.
// Alternatively, specify the plugin directory in your config:
//
//	plugins:
//	  directory: /etc/olb/plugins
//	  auto_load: true
//	  allowed:
//	    - ratelimit-logger
//
// # Architecture
//
// Every plugin must:
//  1. Implement the plugin.Plugin interface (Name, Version, Init, Start, Stop)
//  2. Export a package-level NewPlugin function: func NewPlugin() plugin.Plugin
//
// The NewPlugin function is the entry point that OLB's plugin manager calls
// after loading the .so file. It must return a fresh, uninitialized Plugin
// instance. The manager then calls Init(api) followed by Start().
package main

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
	"github.com/openloadbalancer/olb/internal/plugin"
)

// ---------------------------------------------------------------------------
// Plugin entry point
// ---------------------------------------------------------------------------

// NewPlugin is the required export for OLB plugin loading. The plugin manager
// looks up this symbol by name after opening the .so file. The signature must
// be exactly: func() plugin.Plugin
func NewPlugin() plugin.Plugin {
	return &RateLimitLoggerPlugin{}
}

// ---------------------------------------------------------------------------
// RateLimitLoggerPlugin
// ---------------------------------------------------------------------------

// RateLimitLoggerPlugin is an example plugin that demonstrates all major
// capabilities of the OLB plugin system:
//
//   - Registering custom middleware (rate-limit logging)
//   - Subscribing to system events (backend changes, config reloads)
//   - Using the plugin logger for structured output
//   - Registering and updating custom metrics
//   - Reading the live configuration
//
// It serves as both a functional plugin and a tutorial for plugin authors.
type RateLimitLoggerPlugin struct {
	// api is the host API provided during Init. It gives the plugin access
	// to the logger, metrics registry, config, and event system.
	api plugin.PluginAPI

	// logger is a named logger obtained from the API. All plugin log output
	// should go through this logger so it is tagged with the plugin name.
	logger *logging.Logger

	// subscriptionIDs tracks event subscriptions so they can be cleaned up
	// during Stop. Always unsubscribe to avoid leaking handlers.
	subscriptionIDs []string

	// metrics tracks rate-limited request counts.
	rateLimitedTotal *metrics.Counter
	// rateLimitedByPath tracks rate-limited requests broken down by path.
	rateLimitedByPath *metrics.CounterVec
	// activeBackends is a gauge tracking the number of healthy backends,
	// updated in response to backend.state_change events.
	activeBackends *metrics.Gauge

	// stopped signals the background goroutine to exit.
	stopped atomic.Bool
	// wg tracks background goroutines for clean shutdown.
	wg sync.WaitGroup
}

// Name returns the unique identifier for this plugin. The name is used for
// logging, the plugin whitelist, and the admin API.
func (p *RateLimitLoggerPlugin) Name() string {
	return "ratelimit-logger"
}

// Version returns the plugin version. Use semantic versioning so operators
// can track which version is deployed.
func (p *RateLimitLoggerPlugin) Version() string {
	return "0.1.0"
}

// ---------------------------------------------------------------------------
// Lifecycle: Init
// ---------------------------------------------------------------------------

// Init is called by the plugin manager after the plugin is loaded but before
// Start. This is where you should:
//   - Store the API reference for later use
//   - Register middleware, balancers, health checks, or discovery providers
//   - Subscribe to events
//   - Create and register metrics
//
// If Init returns an error the plugin will not be started and the error is
// reported to the operator.
func (p *RateLimitLoggerPlugin) Init(api plugin.PluginAPI) error {
	p.api = api
	p.logger = api.Logger()

	p.logger.Info("initializing ratelimit-logger plugin")

	// -----------------------------------------------------------------
	// 1. Register custom middleware
	// -----------------------------------------------------------------
	// RegisterMiddleware takes a name and a MiddlewareFactory. The factory
	// receives a config map (from the YAML config) and returns a Middleware
	// implementation. Operators enable the middleware by adding it to a
	// route's middleware list:
	//
	//   routes:
	//     - path: /api
	//       middleware:
	//         - name: ratelimit-logger
	//           config:
	//             log_headers: true
	//
	err := api.RegisterMiddleware("ratelimit-logger", p.middlewareFactory)
	if err != nil {
		return fmt.Errorf("failed to register middleware: %w", err)
	}
	p.logger.Info("registered ratelimit-logger middleware")

	// -----------------------------------------------------------------
	// 2. Subscribe to system events
	// -----------------------------------------------------------------
	// The event bus lets plugins react to changes in the system without
	// tight coupling. Subscribe returns a subscription ID that you should
	// store so you can Unsubscribe during Stop.

	// React to backend state changes (healthy <-> unhealthy transitions).
	subID := api.Subscribe(plugin.EventBackendStateChange, p.onBackendStateChange)
	p.subscriptionIDs = append(p.subscriptionIDs, subID)

	// React to configuration reloads (hot reload).
	subID = api.Subscribe(plugin.EventConfigReload, p.onConfigReload)
	p.subscriptionIDs = append(p.subscriptionIDs, subID)

	// React to new backends being added.
	subID = api.Subscribe(plugin.EventBackendAdded, p.onBackendAdded)
	p.subscriptionIDs = append(p.subscriptionIDs, subID)

	// React to backends being removed.
	subID = api.Subscribe(plugin.EventBackendRemoved, p.onBackendRemoved)
	p.subscriptionIDs = append(p.subscriptionIDs, subID)

	p.logger.Info("subscribed to system events",
		logging.Int("subscriptions", len(p.subscriptionIDs)),
	)

	// -----------------------------------------------------------------
	// 3. Register custom metrics
	// -----------------------------------------------------------------
	// Plugins can create and register their own metrics with the shared
	// metrics registry. These metrics appear alongside built-in metrics
	// in the Prometheus and JSON endpoints.

	p.rateLimitedTotal = metrics.NewCounter(
		"plugin_ratelimit_logger_total",
		"Total number of rate-limited requests observed",
	)
	if err := api.Metrics().RegisterCounter(p.rateLimitedTotal); err != nil {
		return fmt.Errorf("failed to register counter metric: %w", err)
	}

	p.rateLimitedByPath = metrics.NewCounterVec(
		"plugin_ratelimit_logger_by_path",
		"Rate-limited requests broken down by request path",
		[]string{"path"},
	)
	if err := api.Metrics().RegisterCounterVec(p.rateLimitedByPath); err != nil {
		return fmt.Errorf("failed to register counter vec metric: %w", err)
	}

	p.activeBackends = metrics.NewGauge(
		"plugin_ratelimit_logger_active_backends",
		"Number of healthy backends observed by the plugin",
	)
	if err := api.Metrics().RegisterGauge(p.activeBackends); err != nil {
		return fmt.Errorf("failed to register gauge metric: %w", err)
	}

	// -----------------------------------------------------------------
	// 4. Read configuration
	// -----------------------------------------------------------------
	// Plugins can read the current configuration via api.Config(). This is
	// useful for adapting behavior based on the operator's settings.
	cfg := api.Config()
	if cfg != nil {
		p.logger.Info("current config loaded successfully")
	}

	return nil
}

// ---------------------------------------------------------------------------
// Lifecycle: Start
// ---------------------------------------------------------------------------

// Start is called after Init succeeds. This is where you launch background
// goroutines, open connections, or perform any work that should happen after
// all plugins have been initialized.
func (p *RateLimitLoggerPlugin) Start() error {
	p.logger.Info("ratelimit-logger plugin started")

	// Example: start a background goroutine that periodically logs stats.
	// Always track goroutines with a WaitGroup so Stop can wait for them.
	p.wg.Add(1)
	go p.statsReporter()

	return nil
}

// ---------------------------------------------------------------------------
// Lifecycle: Stop
// ---------------------------------------------------------------------------

// Stop gracefully shuts down the plugin. The plugin manager calls Stop in
// reverse load order so that dependencies are respected. You should:
//   - Signal background goroutines to exit
//   - Wait for them to finish
//   - Clean up resources (close files, connections, etc.)
//   - Unsubscribe from events (prevents handler calls after Stop)
func (p *RateLimitLoggerPlugin) Stop() error {
	p.logger.Info("stopping ratelimit-logger plugin")

	// Signal the background goroutine to stop.
	p.stopped.Store(true)

	// Wait for all background goroutines to finish.
	p.wg.Wait()

	// Unsubscribe from all events. This is important: if you skip this,
	// the event bus may call your handlers after Stop returns, which can
	// cause panics if your plugin has released resources.
	// Note: we don't have direct access to the EventBus.Unsubscribe here
	// because PluginAPI does not expose it. In a production plugin, you
	// would either track this internally or the plugin manager handles
	// cleanup. For this example, we document the intent.
	p.logger.Info("ratelimit-logger plugin stopped",
		logging.Int("subscriptions_to_cleanup", len(p.subscriptionIDs)),
	)

	return nil
}

// ---------------------------------------------------------------------------
// Middleware factory
// ---------------------------------------------------------------------------

// middlewareFactory is the MiddlewareFactory function registered with the
// plugin system. It receives configuration from the YAML config and returns
// a Middleware implementation.
//
// Configuration example:
//
//	middleware:
//	  - name: ratelimit-logger
//	    config:
//	      log_headers: true
//	      log_body: false
func (p *RateLimitLoggerPlugin) middlewareFactory(config map[string]any) (plugin.Middleware, error) {
	// Parse configuration with defaults.
	logHeaders := false
	if v, ok := config["log_headers"]; ok {
		if b, ok := v.(bool); ok {
			logHeaders = b
		}
	}

	logBody := false
	if v, ok := config["log_body"]; ok {
		if b, ok := v.(bool); ok {
			logBody = b
		}
	}

	return &rateLimitLoggerMiddleware{
		plugin:     p,
		logHeaders: logHeaders,
		logBody:    logBody,
	}, nil
}

// ---------------------------------------------------------------------------
// Middleware implementation
// ---------------------------------------------------------------------------

// rateLimitLoggerMiddleware is the middleware that wraps HTTP handlers to
// detect and log rate-limited responses (HTTP 429).
type rateLimitLoggerMiddleware struct {
	plugin     *RateLimitLoggerPlugin
	logHeaders bool
	logBody    bool
}

// Name returns the middleware name. This is used for logging and metrics.
func (m *rateLimitLoggerMiddleware) Name() string {
	return "ratelimit-logger"
}

// Handle wraps the given handler. The middleware calls the next handler and
// then inspects the response status code. If it is 429 (Too Many Requests),
// the middleware logs the event with request details and increments metrics.
func (m *rateLimitLoggerMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap the ResponseWriter so we can capture the status code.
		recorder := &statusRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // default if WriteHeader is never called
		}

		// Call the next handler in the chain.
		next.ServeHTTP(recorder, r)

		// Check if the response was a rate limit (429 Too Many Requests).
		if recorder.statusCode == http.StatusTooManyRequests {
			// Increment metrics.
			m.plugin.rateLimitedTotal.Inc()
			m.plugin.rateLimitedByPath.With(r.URL.Path).Inc()

			// Build log fields.
			fields := []logging.Field{
				logging.String("client_ip", r.RemoteAddr),
				logging.String("method", r.Method),
				logging.String("path", r.URL.Path),
				logging.String("query", r.URL.RawQuery),
				logging.String("user_agent", r.UserAgent()),
			}

			// Optionally log request headers.
			if m.logHeaders {
				for name, values := range r.Header {
					for _, value := range values {
						fields = append(fields, logging.String("header_"+name, value))
					}
				}
			}

			// Log the rate-limited request.
			m.plugin.logger.Warn("request rate-limited", fields...)

			// Publish a custom event so other plugins can react to rate
			// limiting. This demonstrates plugin-to-plugin communication
			// via the event bus.
			m.plugin.api.Publish("plugin.ratelimit_logger.rate_limited", map[string]string{
				"client_ip": r.RemoteAddr,
				"method":    r.Method,
				"path":      r.URL.Path,
			})
		}
	})
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
// This is a common pattern for middleware that needs to inspect the response.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code before delegating to the wrapped writer.
func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.statusCode = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write delegates to the wrapped writer, recording a 200 status if
// WriteHeader was not called explicitly.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.statusCode = http.StatusOK
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

// ---------------------------------------------------------------------------
// Event handlers
// ---------------------------------------------------------------------------

// onBackendStateChange handles backend.state_change events. These are fired
// when a backend transitions between healthy and unhealthy states.
func (p *RateLimitLoggerPlugin) onBackendStateChange(event plugin.Event) {
	p.logger.Info("backend state changed",
		logging.String("event", event.Topic),
	)

	// The event data typically contains information about which backend
	// changed state. The exact type depends on the OLB core implementation.
	// Here we demonstrate safe type assertion with a fallback.
	switch data := event.Data.(type) {
	case map[string]any:
		if addr, ok := data["address"].(string); ok {
			p.logger.Info("backend state details",
				logging.String("address", addr),
			)
		}
		if state, ok := data["state"].(string); ok {
			p.logger.Info("backend new state",
				logging.String("state", state),
			)
			// Update our active backends gauge based on state transitions.
			if state == "healthy" {
				p.activeBackends.Inc()
			} else if state == "unhealthy" {
				p.activeBackends.Dec()
			}
		}
	case string:
		p.logger.Info("backend state change",
			logging.String("details", data),
		)
	default:
		p.logger.Debug("backend state change received (unrecognized data type)")
	}
}

// onConfigReload handles config.reload events. These are fired whenever
// the configuration file is reloaded (via SIGHUP, file watcher, or API).
func (p *RateLimitLoggerPlugin) onConfigReload(event plugin.Event) {
	p.logger.Info("configuration reloaded",
		logging.String("event", event.Topic),
	)

	// After a config reload, you may want to re-read settings that affect
	// your plugin's behavior. Use api.Config() to get the updated config.
	cfg := p.api.Config()
	if cfg != nil {
		p.logger.Info("plugin acknowledged config reload")
	}
}

// onBackendAdded handles backend.added events.
func (p *RateLimitLoggerPlugin) onBackendAdded(event plugin.Event) {
	p.logger.Info("new backend added",
		logging.String("event", event.Topic),
	)
	p.activeBackends.Inc()
}

// onBackendRemoved handles backend.removed events.
func (p *RateLimitLoggerPlugin) onBackendRemoved(event plugin.Event) {
	p.logger.Info("backend removed",
		logging.String("event", event.Topic),
	)
	p.activeBackends.Dec()
}

// ---------------------------------------------------------------------------
// Background goroutine
// ---------------------------------------------------------------------------

// statsReporter is a background goroutine that periodically logs a summary
// of rate-limiting activity. This demonstrates how to run background work
// in a plugin while respecting the Stop lifecycle.
func (p *RateLimitLoggerPlugin) statsReporter() {
	defer p.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if p.stopped.Load() {
				return
			}
			p.logger.Info("rate-limit stats report (periodic)")
		default:
			if p.stopped.Load() {
				return
			}
			// Sleep briefly to avoid busy-waiting. In a real plugin you
			// would use a proper channel or context for cancellation.
			time.Sleep(1 * time.Second)
		}
	}
}
