package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ConsulProvider discovers services from HashiCorp Consul.
type ConsulProvider struct {
	*baseProvider
	address    string
	datacenter string
	token      string
	scheme     string
	client     *http.Client
	watches    map[string]context.CancelFunc
	watchMu    sync.Mutex
}

// NewConsulProvider creates a new Consul provider.
func NewConsulProvider(config *ProviderConfig) (*ConsulProvider, error) {
	if config.Type != ProviderTypeConsul {
		return nil, fmt.Errorf("invalid provider type: %q", config.Type)
	}

	address := config.Options["address"]
	if address == "" {
		address = "127.0.0.1:8500"
	}

	scheme := config.Options["scheme"]
	if scheme == "" {
		scheme = "http"
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	return &ConsulProvider{
		baseProvider: newBaseProvider(config.Name, ProviderTypeConsul, config),
		address:      address,
		datacenter:   config.Options["datacenter"],
		token:        config.Options["token"],
		scheme:       scheme,
		client:       client,
		watches:      make(map[string]context.CancelFunc),
	}, nil
}

// Start begins watching for service changes.
func (p *ConsulProvider) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Get list of services to watch
	services, err := p.getServices()
	if err != nil {
		return fmt.Errorf("failed to get services: %w", err)
	}

	// Filter services by tags if specified
	for serviceName := range services {
		if p.shouldWatchService(serviceName) {
			p.wg.Add(1)
			go p.watchService(serviceName)
		}
	}

	// Also watch for new services
	p.wg.Add(1)
	go p.watchCatalog()

	return nil
}

// shouldWatchService returns true if the service should be watched.
func (p *ConsulProvider) shouldWatchService(name string) bool {
	// If specific service name configured, only watch that
	if svcName := p.config.Options["service"]; svcName != "" {
		return name == svcName
	}
	return true
}

// Stop stops the provider.
func (p *ConsulProvider) Stop() error {
	// Cancel all watches
	p.watchMu.Lock()
	for _, cancel := range p.watches {
		cancel()
	}
	p.watches = make(map[string]context.CancelFunc)
	p.watchMu.Unlock()

	p.client.CloseIdleConnections()
	return p.baseProvider.Stop()
}

// consulService represents a Consul service response.
type consulService struct {
	ID                string
	Service           string
	Tags              []string
	Address           string
	Port              int
	Meta              map[string]string
	Datacenter        string
	Checks            []consulHealthCheck
	EnableTagOverride bool
}

// consulHealthCheck represents a Consul health check.
type consulHealthCheck struct {
	Node        string
	CheckID     string
	Name        string
	Status      string
	ServiceID   string
	ServiceName string
	Output      string
}

// consulServiceEntry represents a service returned by the catalog API.
type consulServiceEntry struct {
	ServiceAddress string
	ServicePort    int
	ServiceTags    []string
	ServiceMeta    map[string]string
	ServiceName    string
	ServiceID      string
	Node           string
	Datacenter     string
}

// getServices returns all services from Consul.
func (p *ConsulProvider) getServices() (map[string][]string, error) {
	u := p.buildURL("/v1/catalog/services", nil)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	p.setAuthHeader(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("consul returned status %d", resp.StatusCode)
	}

	var services map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, err
	}

	return services, nil
}

// watchService watches a specific service for changes.
func (p *ConsulProvider) watchService(serviceName string) {
	defer p.wg.Done()

	ctx, cancel := context.WithCancel(p.ctx)
	p.watchMu.Lock()
	p.watches[serviceName] = cancel
	p.watchMu.Unlock()

	defer cancel()

	ticker := time.NewTicker(p.config.Refresh)
	defer ticker.Stop()

	// Initial load
	p.refreshService(serviceName)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refreshService(serviceName)
		}
	}
}

// refreshService fetches and updates services for a specific service name.
func (p *ConsulProvider) refreshService(serviceName string) {
	entries, err := p.getServiceEntries(serviceName)
	if err != nil {
		log.Printf("[discovery] Consul service entries failed for %q: %v", serviceName, err)
		return
	}

	// Track current service IDs for this Consul service
	currentIDs := make(map[string]bool)

	for _, entry := range entries {
		service := p.convertService(entry)

		// Skip if health check filtering is enabled and service is unhealthy
		if p.config.HealthCheck && !service.Healthy {
			continue
		}

		currentIDs[service.ID] = true
		p.addService(service)
	}

	// Remove services that no longer exist
	for _, svc := range p.Services() {
		// Only remove services from this Consul service
		if strings.HasPrefix(svc.ID, p.name+"-"+serviceName+"-") {
			if !currentIDs[svc.ID] {
				p.removeService(svc.ID)
			}
		}
	}
}

// getServiceEntries returns service entries from Consul.
func (p *ConsulProvider) getServiceEntries(serviceName string) ([]consulServiceEntry, error) {
	params := url.Values{}
	params.Set("healthy", "true")
	if p.datacenter != "" {
		params.Set("dc", p.datacenter)
	}
	if len(p.config.Tags) > 0 {
		params.Set("tag", p.config.Tags[0])
	}

	u := p.buildURL(fmt.Sprintf("/v1/catalog/service/%s", serviceName), params)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	p.setAuthHeader(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("consul returned status %d", resp.StatusCode)
	}

	var entries []consulServiceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}

	return entries, nil
}

// convertService converts a Consul service entry to a Service.
func (p *ConsulProvider) convertService(entry consulServiceEntry) *Service {
	address := entry.ServiceAddress
	if address == "" {
		address = entry.Node
	}

	serviceID := fmt.Sprintf("%s-%s-%s-%s-%d", p.name, entry.ServiceName, entry.Node, address, entry.ServicePort)

	// Determine health from tags/meta if available
	healthy := true

	// Build metadata
	meta := make(map[string]string)
	for k, v := range entry.ServiceMeta {
		meta[k] = v
	}
	meta["node"] = entry.Node
	meta["datacenter"] = entry.Datacenter

	return &Service{
		ID:       serviceID,
		Name:     entry.ServiceName,
		Address:  address,
		Port:     entry.ServicePort,
		Tags:     entry.ServiceTags,
		Meta:     meta,
		Weight:   1,
		Priority: 1,
		Healthy:  healthy,
	}
}

// watchCatalog watches the catalog for new services.
func (p *ConsulProvider) watchCatalog() {
	defer p.wg.Done()

	// If specific service configured, don't watch catalog
	if p.config.Options["service"] != "" {
		return
	}

	ticker := time.NewTicker(p.config.Refresh * 5) // Less frequent for catalog
	defer ticker.Stop()

	knownServices := make(map[string]bool)

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			services, err := p.getServices()
			if err != nil {
				log.Printf("[discovery] Consul catalog fetch failed: %v", err); continue
			}

			for serviceName := range services {
				if !knownServices[serviceName] {
					knownServices[serviceName] = true
					p.wg.Add(1)
					go p.watchService(serviceName)
				}
			}
		}
	}
}

// buildURL builds a Consul API URL.
func (p *ConsulProvider) buildURL(path string, params url.Values) string {
	u := url.URL{
		Scheme: p.scheme,
		Host:   p.address,
		Path:   path,
	}

	if params != nil {
		u.RawQuery = params.Encode()
	}

	return u.String()
}

// setAuthHeader sets the X-Consul-Token header on the request if a token is configured.
func (p *ConsulProvider) setAuthHeader(req *http.Request) {
	if p.token != "" {
		req.Header.Set("X-Consul-Token", p.token)
	}
}

// parseWeight parses weight from metadata.
func parseWeight(meta map[string]string) int {
	if w, ok := meta["weight"]; ok {
		if weight, err := strconv.Atoi(w); err == nil {
			return weight
		}
	}
	return 1
}

func init() {
	// Register Consul provider factory
	RegisterProviderFactory(ProviderTypeConsul, func(config *ProviderConfig) (Provider, error) {
		return NewConsulProvider(config)
	})
}
