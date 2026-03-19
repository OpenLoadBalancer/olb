// Package discovery provides service discovery mechanisms for automatic backend registration.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ProviderType represents the type of service discovery provider.
type ProviderType string

const (
	// ProviderTypeStatic is a static configuration provider.
	ProviderTypeStatic ProviderType = "static"
	// ProviderTypeConsul discovers services from HashiCorp Consul.
	ProviderTypeConsul ProviderType = "consul"
	// ProviderTypeDNS discovers services via DNS SRV records.
	ProviderTypeDNS ProviderType = "dns"
	// ProviderTypeFile watches a file for backend changes.
	ProviderTypeFile ProviderType = "file"
	// ProviderTypeDocker discovers services from Docker containers.
	ProviderTypeDocker ProviderType = "docker"
)

// Service represents a discovered backend service.
type Service struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Address  string            `json:"address"`
	Port     int               `json:"port"`
	Tags     []string          `json:"tags,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
	Weight   int               `json:"weight,omitempty"`
	Priority int               `json:"priority,omitempty"`
	Healthy  bool              `json:"healthy"`
}

// FullAddress returns the full address (host:port) of the service.
func (s *Service) FullAddress() string {
	return fmt.Sprintf("%s:%d", s.Address, s.Port)
}

// EventType represents the type of discovery event.
type EventType string

const (
	// EventTypeAdd indicates a service was added.
	EventTypeAdd EventType = "add"
	// EventTypeRemove indicates a service was removed.
	EventTypeRemove EventType = "remove"
	// EventTypeUpdate indicates a service was updated.
	EventTypeUpdate EventType = "update"
)

// Event represents a service discovery event.
type Event struct {
	Type      EventType `json:"type"`
	Service   *Service  `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

// ProviderConfig contains configuration for a discovery provider.
type ProviderConfig struct {
	Type        ProviderType      `json:"type" yaml:"type"`
	Name        string            `json:"name" yaml:"name"`
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Refresh     time.Duration     `json:"refresh" yaml:"refresh"`
	Options     map[string]string `json:"options" yaml:"options"`
	Tags        []string          `json:"tags" yaml:"tags"`
	HealthCheck bool              `json:"health_check" yaml:"health_check"`
}

// DefaultProviderConfig returns a default provider configuration.
func DefaultProviderConfig() *ProviderConfig {
	return &ProviderConfig{
		Enabled:     true,
		Refresh:     30 * time.Second,
		HealthCheck: true,
	}
}

// Validate validates the provider configuration.
func (c *ProviderConfig) Validate() error {
	if c.Type == "" {
		return errors.New("provider type is required")
	}
	if c.Name == "" {
		return errors.New("provider name is required")
	}
	if c.Refresh < time.Second {
		c.Refresh = time.Second
	}
	return nil
}

// Provider is the interface for service discovery providers.
type Provider interface {
	// Name returns the provider name.
	Name() string
	// Type returns the provider type.
	Type() ProviderType
	// Start begins watching for service changes.
	Start(ctx context.Context) error
	// Stop stops the provider.
	Stop() error
	// Services returns the current list of discovered services.
	Services() []*Service
	// Events returns the channel of discovery events.
	Events() <-chan *Event
}

// baseProvider provides common functionality for all providers.
type baseProvider struct {
	name         string
	providerType ProviderType
	config       *ProviderConfig
	services     map[string]*Service
	events       chan *Event
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// newBaseProvider creates a new base provider.
func newBaseProvider(name string, providerType ProviderType, config *ProviderConfig) *baseProvider {
	return &baseProvider{
		name:         name,
		providerType: providerType,
		config:       config,
		services:     make(map[string]*Service),
		events:       make(chan *Event, 100),
	}
}

// Name returns the provider name.
func (p *baseProvider) Name() string {
	return p.name
}

// Type returns the provider type.
func (p *baseProvider) Type() ProviderType {
	return p.providerType
}

// Services returns the current list of discovered services.
func (p *baseProvider) Services() []*Service {
	p.mu.RLock()
	defer p.mu.RUnlock()

	services := make([]*Service, 0, len(p.services))
	for _, s := range p.services {
		services = append(services, s)
	}
	return services
}

// Events returns the channel of discovery events.
func (p *baseProvider) Events() <-chan *Event {
	return p.events
}

// addService adds or updates a service and emits an event.
func (p *baseProvider) addService(service *Service) {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing, ok := p.services[service.ID]
	p.services[service.ID] = service

	eventType := EventTypeAdd
	if ok {
		eventType = EventTypeUpdate
		// Don't emit if nothing changed
		if existing.Address == service.Address &&
			existing.Port == service.Port &&
			existing.Healthy == service.Healthy {
			return
		}
	}

	select {
	case p.events <- &Event{
		Type:      eventType,
		Service:   service,
		Timestamp: time.Now(),
	}:
	default:
		// Channel full, drop event
	}
}

// removeService removes a service and emits an event.
func (p *baseProvider) removeService(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	service, ok := p.services[id]
	if !ok {
		return
	}

	delete(p.services, id)

	select {
	case p.events <- &Event{
		Type:      EventTypeRemove,
		Service:   service,
		Timestamp: time.Now(),
	}:
	default:
		// Channel full, drop event
	}
}

// Stop stops the provider.
func (p *baseProvider) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	close(p.events)
	return nil
}

// Manager manages multiple discovery providers.
type Manager struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewManager creates a new discovery manager.
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]Provider),
	}
}

// AddProvider adds a provider to the manager.
func (m *Manager) AddProvider(provider Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.providers[provider.Name()]; ok {
		return fmt.Errorf("provider %q already exists", provider.Name())
	}

	m.providers[provider.Name()] = provider
	return nil
}

// RemoveProvider removes a provider from the manager.
func (m *Manager) RemoveProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	provider, ok := m.providers[name]
	if !ok {
		return fmt.Errorf("provider %q not found", name)
	}

	provider.Stop()
	delete(m.providers, name)
	return nil
}

// GetProvider returns a provider by name.
func (m *Manager) GetProvider(name string) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, ok := m.providers[name]
	return provider, ok
}

// Providers returns all registered providers.
func (m *Manager) Providers() []Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	return providers
}

// Start starts all providers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, provider := range m.providers {
		if err := provider.Start(ctx); err != nil {
			return fmt.Errorf("failed to start provider %q: %w", provider.Name(), err)
		}
	}

	return nil
}

// Stop stops all providers.
func (m *Manager) Stop() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for _, provider := range m.providers {
		if err := provider.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop provider %q: %w", provider.Name(), err))
		}
	}

	if len(errs) > 0 {
		return errors.New("one or more providers failed to stop")
	}

	return nil
}

// AllServices returns all services from all providers.
func (m *Manager) AllServices() []*Service {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var services []*Service
	for _, provider := range m.providers {
		services = append(services, provider.Services()...)
	}
	return services
}

// AggregateEvents returns an aggregated channel of events from all providers.
func (m *Manager) AggregateEvents() <-chan *Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create aggregated channel
	agg := make(chan *Event, 1000)

	// Start goroutine for each provider
	for _, provider := range m.providers {
		go func(p Provider) {
			for event := range p.Events() {
				select {
				case agg <- event:
				default:
					// Channel full, drop event
				}
			}
		}(provider)
	}

	return agg
}

// ProviderFactory creates providers from configuration.
type ProviderFactory func(config *ProviderConfig) (Provider, error)

var providerFactories = make(map[ProviderType]ProviderFactory)

// RegisterProviderFactory registers a provider factory for a type.
func RegisterProviderFactory(providerType ProviderType, factory ProviderFactory) {
	providerFactories[providerType] = factory
}

// CreateProvider creates a provider from configuration.
func CreateProvider(config *ProviderConfig) (Provider, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	factory, ok := providerFactories[config.Type]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %q", config.Type)
	}

	return factory(config)
}

// ServiceFilter filters services based on criteria.
type ServiceFilter struct {
	Tags    []string
	Healthy *bool
	Meta    map[string]string
}

// Matches returns true if the service matches the filter.
func (f *ServiceFilter) Matches(service *Service) bool {
	if f == nil {
		return true
	}
	if f.Healthy != nil && service.Healthy != *f.Healthy {
		return false
	}

	if len(f.Tags) > 0 {
		tagSet := make(map[string]bool)
		for _, t := range service.Tags {
			tagSet[t] = true
		}
		for _, t := range f.Tags {
			if !tagSet[t] {
				return false
			}
		}
	}

	if len(f.Meta) > 0 {
		for k, v := range f.Meta {
			if service.Meta[k] != v {
				return false
			}
		}
	}

	return true
}

// FilterServices filters a list of services.
func FilterServices(services []*Service, filter *ServiceFilter) []*Service {
	if filter == nil {
		return services
	}

	var result []*Service
	for _, s := range services {
		if filter.Matches(s) {
			result = append(result, s)
		}
	}
	return result
}
