package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"github.com/openloadbalancer/olb/internal/config/yaml"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// StaticProvider provides service discovery from static configuration.
type StaticProvider struct {
	*baseProvider
	addresses []string
}

// NewStaticProvider creates a new static provider.
func NewStaticProvider(config *ProviderConfig) (*StaticProvider, error) {
	if config.Type != ProviderTypeStatic {
		return nil, fmt.Errorf("invalid provider type: %q", config.Type)
	}

	p := &StaticProvider{
		baseProvider: newBaseProvider(config.Name, ProviderTypeStatic, config),
	}

	// Parse addresses from options
	if addrStr, ok := config.Options["addresses"]; ok {
		p.addresses = parseAddresses(addrStr)
	}

	return p, nil
}

// parseAddresses parses a comma-separated list of addresses.
func parseAddresses(s string) []string {
	var addresses []string
	start := 0
	for i := range len(s) {
		if s[i] == ',' {
			addr := trimSpace(s[start:i])
			if addr != "" {
				addresses = append(addresses, addr)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		addr := trimSpace(s[start:])
		if addr != "" {
			addresses = append(addresses, addr)
		}
	}
	return addresses
}

// trimSpace removes leading/trailing spaces from a string.
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// Start begins watching for service changes.
func (p *StaticProvider) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Register static services
	for i, addr := range p.addresses {
		host, port := splitHostPort(addr)
		service := &Service{
			ID:      fmt.Sprintf("%s-%d", p.name, i),
			Name:    p.name,
			Address: host,
			Port:    port,
			Tags:    p.config.Tags,
			Meta:    p.config.Options,
			Weight:  1,
			Healthy: true,
		}
		p.addService(service)
	}

	return nil
}

// parsePort extracts port from address (defaults to 80).
func parsePort(addr string) int {
	_, port := splitHostPort(addr)
	return port
}

// splitHostPort splits an address into host and port.
// If no port is found, returns the original string and port 80.
func splitHostPort(addr string) (string, int) {
	// Handle IPv6 addresses in brackets
	if len(addr) > 0 && addr[0] == '[' {
		// Find closing bracket
		for i := 1; i < len(addr); i++ {
			if addr[i] == ']' {
				if i+1 < len(addr) && addr[i+1] == ':' {
					// Port after bracket
					port := 0
					for j := i + 2; j < len(addr); j++ {
						if addr[j] < '0' || addr[j] > '9' {
							return addr, 80
						}
						port = port*10 + int(addr[j]-'0')
					}
					if port > 0 && port <= 65535 {
						return addr[1:i], port
					}
				}
				return addr[1:i], 80
			}
		}
		return addr, 80
	}

	// Find last colon
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host := addr[:i]
			port := 0
			for j := i + 1; j < len(addr); j++ {
				if addr[j] < '0' || addr[j] > '9' {
					return addr, 80
				}
				port = port*10 + int(addr[j]-'0')
			}
			if port > 0 && port <= 65535 {
				return host, port
			}
			return addr, 80
		}
	}
	return addr, 80
}

// StaticConfig represents a static configuration file format.
type StaticConfig struct {
	Services []StaticService `json:"services" yaml:"services"`
}

// StaticService represents a service in static configuration.
type StaticService struct {
	ID      string            `json:"id" yaml:"id"`
	Name    string            `json:"name" yaml:"name"`
	Address string            `json:"address" yaml:"address"`
	Port    int               `json:"port" yaml:"port"`
	Weight  int               `json:"weight" yaml:"weight"`
	Tags    []string          `json:"tags" yaml:"tags"`
	Meta    map[string]string `json:"meta" yaml:"meta"`
}

// StaticFileProvider loads services from a static JSON/YAML file.
type StaticFileProvider struct {
	*baseProvider
	filePath string
	format   string
}

// NewStaticFileProvider creates a new static file provider.
func NewStaticFileProvider(config *ProviderConfig) (*StaticFileProvider, error) {
	filePath, ok := config.Options["file"]
	if !ok {
		return nil, fmt.Errorf("file option is required for static file provider")
	}

	format := config.Options["format"]
	if format == "" {
		format = detectFormat(filePath)
	}

	return &StaticFileProvider{
		baseProvider: newBaseProvider(config.Name, ProviderTypeStatic, config),
		filePath:     filePath,
		format:       format,
	}, nil
}

// detectFormat detects file format from extension.
func detectFormat(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "json"
	}
}

// Start begins watching for service changes.
func (p *StaticFileProvider) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Load initial services
	if err := p.loadServices(); err != nil {
		return err
	}

	// Start file watcher if refresh interval is set
	if p.config.Refresh > 0 {
		p.wg.Add(1)
		go p.watchFile()
	}

	return nil
}

// loadServices loads services from the file.
func (p *StaticFileProvider) loadServices() error {
	data, err := os.ReadFile(p.filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", p.filePath, err)
	}

	var config StaticConfig
	if p.format == "json" {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else {
		// YAML format
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	}

	// Track current service IDs
	currentIDs := make(map[string]bool)

	// Add/update services
	for _, svc := range config.Services {
		if svc.ID == "" {
			svc.ID = fmt.Sprintf("%s-%s-%d", p.name, svc.Address, svc.Port)
		}

		currentIDs[svc.ID] = true

		service := &Service{
			ID:      svc.ID,
			Name:    svc.Name,
			Address: svc.Address,
			Port:    svc.Port,
			Weight:  svc.Weight,
			Tags:    append([]string{}, svc.Tags...),
			Meta:    make(map[string]string),
			Healthy: true,
		}

		maps.Copy(service.Meta, svc.Meta)

		p.addService(service)
	}

	// Remove services not in file
	for _, svc := range p.Services() {
		if !currentIDs[svc.ID] {
			p.removeService(svc.ID)
		}
	}

	return nil
}

// watchFile watches the file for changes.
func (p *StaticFileProvider) watchFile() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.Refresh)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.loadServices(); err != nil {
				log.Printf("[discovery] static file reload failed for %q: %v", p.filePath, err)
			}
		}
	}
}

func init() {
	// Register static provider factory
	RegisterProviderFactory(ProviderTypeStatic, func(config *ProviderConfig) (Provider, error) {
		// Check if file option is present
		if _, ok := config.Options["file"]; ok {
			return NewStaticFileProvider(config)
		}
		return NewStaticProvider(config)
	})
}
