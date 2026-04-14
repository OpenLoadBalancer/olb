// Package plugin provides a plugin system for extending OpenLoadBalancer.
// It supports dynamic plugin loading via Go's plugin package (.so files),
// direct registration for built-in plugins, and an event bus for inter-component
// communication. Plugins can register custom middleware, balancer algorithms,
// health check strategies, and service discovery providers.
package plugin

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"plugin"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/metrics"
)

// --------------------------------------------------------------------------
// Core interfaces that plugins can extend
// --------------------------------------------------------------------------

// Middleware is the interface for HTTP middleware components.
// Plugins register factories that produce Middleware implementations.
type Middleware interface {
	// Name returns the middleware name.
	Name() string
	// Handle wraps the given handler with middleware logic.
	Handle(next http.Handler) http.Handler
}

// Balancer is the interface for load-balancing algorithms.
// Plugins register factories that produce Balancer implementations.
type Balancer interface {
	// Name returns the algorithm name.
	Name() string
	// Next selects the next backend from the provided list.
	// Returns the selected backend index, or -1 if none available.
	Next(backends []string) int
}

// HealthChecker is the interface for health check strategies.
// Plugins register factories that produce HealthChecker implementations.
type HealthChecker interface {
	// Name returns the health check type name.
	Name() string
	// Check performs a health check against the given address.
	// Returns nil if healthy, or an error describing the failure.
	Check(address string) error
}

// DiscoveryProvider is the interface for service discovery backends.
// Plugins register factories that produce DiscoveryProvider implementations.
type DiscoveryProvider interface {
	// Name returns the provider name.
	Name() string
	// Discover returns the current set of backend addresses for a service.
	Discover(service string) ([]string, error)
	// Watch starts watching for changes and sends updates on the channel.
	Watch(service string) (<-chan []string, error)
	// Stop stops the discovery provider.
	Stop() error
}

// --------------------------------------------------------------------------
// Factory types
// --------------------------------------------------------------------------

// MiddlewareFactory creates a Middleware from configuration.
type MiddlewareFactory func(config map[string]any) (Middleware, error)

// BalancerFactory creates a Balancer from configuration.
type BalancerFactory func(config map[string]any) (Balancer, error)

// HealthCheckFactory creates a HealthChecker from configuration.
type HealthCheckFactory func(config map[string]any) (HealthChecker, error)

// DiscoveryFactory creates a DiscoveryProvider from configuration.
type DiscoveryFactory func(config map[string]any) (DiscoveryProvider, error)

// --------------------------------------------------------------------------
// Plugin interface & info
// --------------------------------------------------------------------------

// Plugin is the interface that all plugins must implement.
type Plugin interface {
	// Name returns the plugin name (unique identifier).
	Name() string
	// Version returns the plugin version string.
	Version() string
	// Init initializes the plugin with access to the host API.
	Init(api PluginAPI) error
	// Start begins plugin operation after initialization.
	Start() error
	// Stop gracefully shuts down the plugin.
	Stop() error
}

// PluginInfo describes a loaded plugin.
type PluginInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	License     string `json:"license"`
}

// --------------------------------------------------------------------------
// Event system
// --------------------------------------------------------------------------

// Built-in event topic constants.
const (
	EventConfigReload       = "config.reload"
	EventBackendAdded       = "backend.added"
	EventBackendRemoved     = "backend.removed"
	EventBackendStateChange = "backend.state_change"
	EventRouteAdded         = "route.added"
	EventRouteRemoved       = "route.removed"
	EventHealthCheckResult  = "health.check_result"
)

// Event represents a published event.
type Event struct {
	// Topic is the event topic name.
	Topic string
	// Data is the event payload.
	Data any
	// Timestamp is when the event was published.
	Timestamp time.Time
}

// EventHandler is a function that handles an event.
type EventHandler func(Event)

// subscription holds a subscriber's handler and ID.
type subscription struct {
	id      string
	handler EventHandler
}

// EventBus provides a publish/subscribe event system.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]subscription
	nextID      atomic.Uint64
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]subscription),
	}
}

// Subscribe registers a handler for the given topic and returns a subscription ID.
func (eb *EventBus) Subscribe(topic string, handler EventHandler) string {
	id := fmt.Sprintf("sub_%d", eb.nextID.Add(1))
	eb.mu.Lock()
	eb.subscribers[topic] = append(eb.subscribers[topic], subscription{
		id:      id,
		handler: handler,
	})
	eb.mu.Unlock()
	return id
}

// Unsubscribe removes a subscription by ID.
func (eb *EventBus) Unsubscribe(id string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for topic, subs := range eb.subscribers {
		for i, s := range subs {
			if s.id == id {
				eb.subscribers[topic] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

// Publish sends an event to all subscribers of the given topic.
// Handlers are called synchronously in the order they were registered.
func (eb *EventBus) Publish(topic string, data any) {
	event := Event{
		Topic:     topic,
		Data:      data,
		Timestamp: time.Now(),
	}

	eb.mu.RLock()
	subs := make([]subscription, len(eb.subscribers[topic]))
	copy(subs, eb.subscribers[topic])
	eb.mu.RUnlock()

	for _, s := range subs {
		s.handler(event)
	}
}

// --------------------------------------------------------------------------
// PluginAPI — what plugins can access
// --------------------------------------------------------------------------

// PluginAPI provides the host API that plugins use to interact with the system.
type PluginAPI interface {
	// RegisterMiddleware registers a middleware factory under the given name.
	RegisterMiddleware(name string, factory MiddlewareFactory) error
	// RegisterBalancer registers a balancer factory under the given name.
	RegisterBalancer(name string, factory BalancerFactory) error
	// RegisterHealthCheck registers a health check factory under the given name.
	RegisterHealthCheck(name string, factory HealthCheckFactory) error
	// RegisterDiscovery registers a discovery factory under the given name.
	RegisterDiscovery(name string, factory DiscoveryFactory) error
	// Logger returns a logger instance for the plugin.
	Logger() *logging.Logger
	// Metrics returns the metrics registry.
	Metrics() *metrics.Registry
	// Config returns the current configuration.
	Config() *config.Config
	// Subscribe subscribes to an event topic.
	Subscribe(event string, handler EventHandler) string
	// Publish publishes an event to a topic.
	Publish(event string, data any)
}

// pluginAPI is the concrete implementation of PluginAPI provided to plugins.
type pluginAPI struct {
	manager *PluginManager
	logger  *logging.Logger
}

func (a *pluginAPI) RegisterMiddleware(name string, factory MiddlewareFactory) error {
	return a.manager.registerMiddleware(name, factory)
}

func (a *pluginAPI) RegisterBalancer(name string, factory BalancerFactory) error {
	return a.manager.registerBalancer(name, factory)
}

func (a *pluginAPI) RegisterHealthCheck(name string, factory HealthCheckFactory) error {
	return a.manager.registerHealthCheck(name, factory)
}

func (a *pluginAPI) RegisterDiscovery(name string, factory DiscoveryFactory) error {
	return a.manager.registerDiscovery(name, factory)
}

func (a *pluginAPI) Logger() *logging.Logger {
	return a.logger
}

func (a *pluginAPI) Metrics() *metrics.Registry {
	return a.manager.metrics
}

func (a *pluginAPI) Config() *config.Config {
	return a.manager.config
}

func (a *pluginAPI) Subscribe(event string, handler EventHandler) string {
	return a.manager.eventBus.Subscribe(event, handler)
}

func (a *pluginAPI) Publish(event string, data any) {
	a.manager.eventBus.Publish(event, data)
}

// --------------------------------------------------------------------------
// PluginManagerConfig
// --------------------------------------------------------------------------

// PluginManagerConfig configures the PluginManager.
type PluginManagerConfig struct {
	// PluginDir is the directory to scan for plugin .so files.
	PluginDir string
	// AutoLoad controls whether plugins are automatically loaded from PluginDir.
	AutoLoad bool
	// AllowedPlugins is a whitelist of plugin names. When AutoLoad is true,
	// AllowedPlugins must be non-empty; otherwise auto-loading is skipped with a warning.
	AllowedPlugins []string
}

// DefaultPluginManagerConfig returns a PluginManagerConfig with default values.
func DefaultPluginManagerConfig() PluginManagerConfig {
	return PluginManagerConfig{
		PluginDir: "plugins/",
		AutoLoad:  false,
	}
}

// --------------------------------------------------------------------------
// PluginManager
// --------------------------------------------------------------------------

// pluginEntry holds a loaded plugin and its metadata.
type pluginEntry struct {
	plugin Plugin
	info   PluginInfo
	order  int // load order for deterministic start/stop
}

// PluginManager manages plugin lifecycle, registration, and the event bus.
type PluginManager struct {
	mu sync.RWMutex

	config   *config.Config
	logger   *logging.Logger
	metrics  *metrics.Registry
	eventBus *EventBus

	managerConfig PluginManagerConfig
	plugins       map[string]*pluginEntry
	loadOrder     int

	// Extension registries
	middlewareFactories  map[string]MiddlewareFactory
	balancerFactories    map[string]BalancerFactory
	healthCheckFactories map[string]HealthCheckFactory
	discoveryFactories   map[string]DiscoveryFactory
}

// NewPluginManager creates a new PluginManager with the given configuration.
func NewPluginManager(cfg PluginManagerConfig) *PluginManager {
	return &PluginManager{
		managerConfig:        cfg,
		plugins:              make(map[string]*pluginEntry),
		eventBus:             NewEventBus(),
		logger:               logging.NewWithDefaults(),
		metrics:              metrics.NewRegistry(),
		config:               &config.Config{},
		middlewareFactories:  make(map[string]MiddlewareFactory),
		balancerFactories:    make(map[string]BalancerFactory),
		healthCheckFactories: make(map[string]HealthCheckFactory),
		discoveryFactories:   make(map[string]DiscoveryFactory),
	}
}

// SetLogger sets the logger used by the plugin manager and provided to plugins.
func (pm *PluginManager) SetLogger(logger *logging.Logger) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.logger = logger
}

// SetMetrics sets the metrics registry provided to plugins.
func (pm *PluginManager) SetMetrics(registry *metrics.Registry) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.metrics = registry
}

// SetConfig sets the configuration provided to plugins.
func (pm *PluginManager) SetConfig(cfg *config.Config) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.config = cfg
}

// EventBus returns the event bus for publishing/subscribing to events.
func (pm *PluginManager) EventBus() *EventBus {
	return pm.eventBus
}

// isAllowed checks whether a plugin name is permitted by the whitelist.
func (pm *PluginManager) isAllowed(name string) bool {
	if len(pm.managerConfig.AllowedPlugins) == 0 {
		return true
	}
	return slices.Contains(pm.managerConfig.AllowedPlugins, name)
}

// RegisterPlugin registers a plugin directly (for built-in plugins).
// The plugin is not initialized or started; call StartAll for that.
func (pm *PluginManager) RegisterPlugin(p Plugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	name := p.Name()
	if !pm.isAllowed(name) {
		return fmt.Errorf("plugin %q is not in the allowed list", name)
	}

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %q is already registered", name)
	}

	pm.loadOrder++
	pm.plugins[name] = &pluginEntry{
		plugin: p,
		info: PluginInfo{
			Name:    name,
			Version: p.Version(),
		},
		order: pm.loadOrder,
	}

	pm.logger.Info("plugin registered",
		logging.String("plugin", name),
		logging.String("version", p.Version()),
	)

	return nil
}

// LoadPlugin loads a Go plugin from a shared object (.so) file.
// The file must export a symbol named "NewPlugin" of type func() Plugin.
func (pm *PluginManager) LoadPlugin(path string) error {
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open plugin %s: %w", path, err)
	}

	sym, err := p.Lookup("NewPlugin")
	if err != nil {
		return fmt.Errorf("plugin %s missing NewPlugin symbol: %w", path, err)
	}

	factory, ok := sym.(func() Plugin)
	if !ok {
		return fmt.Errorf("plugin %s: NewPlugin has wrong signature, expected func() Plugin", path)
	}

	plug := factory()
	return pm.RegisterPlugin(plug)
}

// LoadDir scans a directory for .so plugin files and loads each one.
// Non-.so files are silently skipped. If the directory does not exist,
// no error is returned. If AllowedPlugins is empty, no plugins are loaded
// and a warning is logged — use SetAllowedPlugins or configure the allowlist
// before calling LoadDir.
func (pm *PluginManager) LoadDir(dir string) error {
	if len(pm.managerConfig.AllowedPlugins) == 0 {
		pm.logger.Warn("plugin auto-load skipped: AllowedPlugins is empty — configure an explicit allowlist before loading plugins",
			logging.String("dir", dir),
		)
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read plugin directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".so" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := pm.LoadPlugin(path); err != nil {
			pm.logger.Error("failed to load plugin",
				logging.String("path", path),
				logging.Error(err),
			)
			// Continue loading other plugins; don't fail the batch.
		}
	}

	return nil
}

// StartAll initializes and starts all registered plugins in load order.
func (pm *PluginManager) StartAll() error {
	pm.mu.RLock()
	entries := pm.sortedEntries()
	pm.mu.RUnlock()

	for _, entry := range entries {
		api := &pluginAPI{
			manager: pm,
			logger:  pm.logger.WithName("plugin." + entry.plugin.Name()),
		}

		if err := entry.plugin.Init(api); err != nil {
			return fmt.Errorf("failed to initialize plugin %q: %w", entry.plugin.Name(), err)
		}

		if err := entry.plugin.Start(); err != nil {
			return fmt.Errorf("failed to start plugin %q: %w", entry.plugin.Name(), err)
		}

		pm.logger.Info("plugin started",
			logging.String("plugin", entry.plugin.Name()),
		)
	}

	return nil
}

// StopAll stops all plugins in reverse load order.
func (pm *PluginManager) StopAll() error {
	pm.mu.RLock()
	entries := pm.sortedEntries()
	pm.mu.RUnlock()

	// Reverse the slice so we stop in reverse order.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	var firstErr error
	for _, entry := range entries {
		if err := entry.plugin.Stop(); err != nil {
			pm.logger.Error("failed to stop plugin",
				logging.String("plugin", entry.plugin.Name()),
				logging.Error(err),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to stop plugin %q: %w", entry.plugin.Name(), err)
			}
		} else {
			pm.logger.Info("plugin stopped",
				logging.String("plugin", entry.plugin.Name()),
			)
		}
	}

	return firstErr
}

// ListPlugins returns information about all registered plugins.
func (pm *PluginManager) ListPlugins() []PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entries := pm.sortedEntries()
	infos := make([]PluginInfo, len(entries))
	for i, entry := range entries {
		infos[i] = entry.info
	}
	return infos
}

// GetPlugin returns a registered plugin by name.
func (pm *PluginManager) GetPlugin(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entry, ok := pm.plugins[name]
	if !ok {
		return nil, false
	}
	return entry.plugin, true
}

// GetMiddlewareFactory returns a registered middleware factory.
func (pm *PluginManager) GetMiddlewareFactory(name string) (MiddlewareFactory, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	f, ok := pm.middlewareFactories[name]
	return f, ok
}

// GetBalancerFactory returns a registered balancer factory.
func (pm *PluginManager) GetBalancerFactory(name string) (BalancerFactory, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	f, ok := pm.balancerFactories[name]
	return f, ok
}

// GetHealthCheckFactory returns a registered health check factory.
func (pm *PluginManager) GetHealthCheckFactory(name string) (HealthCheckFactory, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	f, ok := pm.healthCheckFactories[name]
	return f, ok
}

// GetDiscoveryFactory returns a registered discovery factory.
func (pm *PluginManager) GetDiscoveryFactory(name string) (DiscoveryFactory, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	f, ok := pm.discoveryFactories[name]
	return f, ok
}

// --------------------------------------------------------------------------
// Internal helpers
// --------------------------------------------------------------------------

// registerMiddleware registers a middleware factory (called via PluginAPI).
func (pm *PluginManager) registerMiddleware(name string, factory MiddlewareFactory) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.middlewareFactories[name]; exists {
		return fmt.Errorf("middleware %q is already registered", name)
	}
	pm.middlewareFactories[name] = factory
	pm.logger.Debug("middleware registered", logging.String("name", name))
	return nil
}

// registerBalancer registers a balancer factory (called via PluginAPI).
func (pm *PluginManager) registerBalancer(name string, factory BalancerFactory) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.balancerFactories[name]; exists {
		return fmt.Errorf("balancer %q is already registered", name)
	}
	pm.balancerFactories[name] = factory
	pm.logger.Debug("balancer registered", logging.String("name", name))
	return nil
}

// registerHealthCheck registers a health check factory (called via PluginAPI).
func (pm *PluginManager) registerHealthCheck(name string, factory HealthCheckFactory) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.healthCheckFactories[name]; exists {
		return fmt.Errorf("health check %q is already registered", name)
	}
	pm.healthCheckFactories[name] = factory
	pm.logger.Debug("health check registered", logging.String("name", name))
	return nil
}

// registerDiscovery registers a discovery factory (called via PluginAPI).
func (pm *PluginManager) registerDiscovery(name string, factory DiscoveryFactory) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.discoveryFactories[name]; exists {
		return fmt.Errorf("discovery provider %q is already registered", name)
	}
	pm.discoveryFactories[name] = factory
	pm.logger.Debug("discovery provider registered", logging.String("name", name))
	return nil
}

// sortedEntries returns plugin entries sorted by load order.
// Caller must hold pm.mu (at least RLock).
func (pm *PluginManager) sortedEntries() []*pluginEntry {
	entries := make([]*pluginEntry, 0, len(pm.plugins))
	for _, entry := range pm.plugins {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].order < entries[j].order
	})
	return entries
}
