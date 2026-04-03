package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// FileConfig contains configuration for the file-based discovery provider.
type FileConfig struct {
	// Path is the path to the JSON file containing backend definitions.
	Path string
	// PollInterval is how frequently to check the file for changes.
	// Defaults to 5 seconds if not set.
	PollInterval time.Duration
}

// DefaultFileConfig returns a FileConfig with sensible defaults.
func DefaultFileConfig() *FileConfig {
	return &FileConfig{
		PollInterval: 5 * time.Second,
	}
}

// FileBackend represents a single backend entry in the JSON file.
type FileBackend struct {
	Address  string            `json:"address"`
	Weight   int               `json:"weight,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// FileDocument represents the top-level JSON document structure.
type FileDocument struct {
	Backends []FileBackend `json:"backends"`
}

// FileProvider discovers services by watching a JSON file for backend definitions.
// It polls the file at a configurable interval and uses SHA-256 hashing to
// detect changes, emitting add/remove/update events as backends change.
type FileProvider struct {
	*baseProvider
	filePath     string
	pollInterval time.Duration
	lastHash     [sha256.Size]byte
	hasLastHash  bool
}

// NewFileProvider creates a new file-based discovery provider.
// The config.Options map must contain a "path" key pointing to the JSON file.
// An optional "poll_interval" key can specify the polling duration (e.g., "10s").
func NewFileProvider(config *ProviderConfig) (*FileProvider, error) {
	if config.Type != ProviderTypeFile {
		return nil, fmt.Errorf("invalid provider type: %q, expected %q", config.Type, ProviderTypeFile)
	}

	filePath, ok := config.Options["path"]
	if !ok || filePath == "" {
		return nil, fmt.Errorf("path option is required for file provider")
	}

	pollInterval := 5 * time.Second
	if intervalStr, ok := config.Options["poll_interval"]; ok && intervalStr != "" {
		parsed, err := time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid poll_interval %q: %w", intervalStr, err)
		}
		if parsed < time.Second {
			parsed = time.Second
		}
		pollInterval = parsed
	}

	return &FileProvider{
		baseProvider: newBaseProvider(config.Name, ProviderTypeFile, config),
		filePath:     filePath,
		pollInterval: pollInterval,
	}, nil
}

// Start begins watching the file for changes. It performs an initial load
// of the file and then starts a background goroutine that polls for changes.
func (p *FileProvider) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Perform initial load
	if err := p.loadFile(); err != nil {
		return fmt.Errorf("initial file load failed: %w", err)
	}

	// Start background polling
	p.wg.Add(1)
	go p.pollLoop()

	return nil
}

// loadFile reads and parses the JSON file, updating services accordingly.
// It uses SHA-256 hashing to skip processing if the file has not changed.
func (p *FileProvider) loadFile() error {
	data, err := os.ReadFile(p.filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", p.filePath, err)
	}

	// Compute SHA-256 hash to detect changes
	hash := sha256.Sum256(data)
	if p.hasLastHash && hash == p.lastHash {
		// File has not changed
		return nil
	}
	p.lastHash = hash
	p.hasLastHash = true

	// Parse JSON document
	var doc FileDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse JSON file %q: %w", p.filePath, err)
	}

	// Track IDs from the current file to detect removals
	currentIDs := make(map[string]bool, len(doc.Backends))

	// Add or update services
	for i, backend := range doc.Backends {
		if backend.Address == "" {
			continue // Skip entries without an address
		}

		host, port := splitHostPort(backend.Address)

		weight := backend.Weight
		if weight <= 0 {
			weight = 1
		}

		id := fmt.Sprintf("%s-file-%d", p.name, i)
		currentIDs[id] = true

		meta := make(map[string]string)
		for k, v := range backend.Metadata {
			meta[k] = v
		}

		service := &Service{
			ID:      id,
			Name:    p.name,
			Address: host,
			Port:    port,
			Weight:  weight,
			Tags:    p.config.Tags,
			Meta:    meta,
			Healthy: true,
		}

		p.addService(service)
	}

	// Remove services that are no longer in the file
	for _, svc := range p.Services() {
		if !currentIDs[svc.ID] {
			p.removeService(svc.ID)
		}
	}

	return nil
}

// pollLoop periodically checks the file for changes.
func (p *FileProvider) pollLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			// Log errors but keep the provider running. The last known
			// good state is preserved.
			if err := p.loadFile(); err != nil {
				log.Printf("file discovery: failed to reload %s: %v", p.filePath, err)
			}
		}
	}
}

func init() {
	// Register file provider factory
	RegisterProviderFactory(ProviderTypeFile, func(config *ProviderConfig) (Provider, error) {
		return NewFileProvider(config)
	})
}
