package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// DNSProvider discovers services via DNS SRV records.
type DNSProvider struct {
	*baseProvider
	domain     string
	nameserver string
	resolver   *net.Resolver
}

// NewDNSProvider creates a new DNS provider.
func NewDNSProvider(config *ProviderConfig) (*DNSProvider, error) {
	domain, ok := config.Options["domain"]
	if !ok {
		return nil, fmt.Errorf("domain option is required for DNS provider")
	}

	nameserver := config.Options["nameserver"]

	resolver := &net.Resolver{
		PreferGo: true,
	}

	// If nameserver specified, create custom dialer
	if nameserver != "" {
		resolver.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, network, nameserver)
		}
	}

	return &DNSProvider{
		baseProvider: newBaseProvider(config.Name, ProviderTypeDNS, config),
		domain:       domain,
		nameserver:   nameserver,
		resolver:     resolver,
	}, nil
}

// Start begins watching for service changes.
func (p *DNSProvider) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Do initial lookup
	if err := p.refresh(); err != nil {
		// Log error but continue
	}

	// Start refresh loop
	p.wg.Add(1)
	go p.refreshLoop()

	return nil
}

// refresh performs a DNS lookup and updates services.
func (p *DNSProvider) refresh() error {
	// Query SRV records
	_, srvs, err := p.resolver.LookupSRV(p.ctx, "", "", p.domain)
	if err != nil {
		return fmt.Errorf("SRV lookup failed for %q: %w", p.domain, err)
	}

	// Track current services
	currentIDs := make(map[string]bool)
	var mu sync.Mutex

	// Resolve each SRV target
	var wg sync.WaitGroup
	for _, srv := range srvs {
		wg.Add(1)
		go func(srv *net.SRV) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[discovery] panic recovered in DNS SRV resolver: %v", r)
				}
			}()

			// Resolve target to IP
			addrs, err := p.resolver.LookupHost(p.ctx, srv.Target)
			if err != nil {
				return
			}

			if len(addrs) == 0 {
				return
			}

			// Use first resolved address
			service := &Service{
				ID:       fmt.Sprintf("%s-%s-%d", p.name, srv.Target, srv.Port),
				Name:     p.name,
				Address:  addrs[0],
				Port:     int(srv.Port),
				Weight:   int(srv.Weight),
				Priority: int(srv.Priority),
				Tags:     p.config.Tags,
				Meta:     map[string]string{"target": srv.Target},
				Healthy:  true,
			}

			p.addService(service)
			mu.Lock()
			currentIDs[service.ID] = true
			mu.Unlock()
		}(srv)
	}

	wg.Wait()

	// Remove services no longer in DNS
	for _, svc := range p.Services() {
		if !currentIDs[svc.ID] {
			p.removeService(svc.ID)
		}
	}

	return nil
}

// refreshLoop periodically refreshes DNS records.
func (p *DNSProvider) refreshLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.Refresh)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.refresh(); err != nil {
				// Log error but continue
			}
		}
	}
}

func init() {
	// Register DNS provider factory
	RegisterProviderFactory(ProviderTypeDNS, func(config *ProviderConfig) (Provider, error) {
		return NewDNSProvider(config)
	})
}
