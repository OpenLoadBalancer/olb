package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DockerConfig contains configuration for the Docker discovery provider.
type DockerConfig struct {
	// SocketPath is the path to the Docker daemon Unix socket.
	// Defaults to "/var/run/docker.sock".
	SocketPath string

	// PollInterval is how frequently to poll for container changes.
	// Defaults to 10 seconds.
	PollInterval time.Duration

	// LabelPrefix is the prefix for Docker labels used for OLB configuration.
	// Defaults to "olb.".
	LabelPrefix string

	// Network filters containers by Docker network name.
	// If empty, the first network IP is used.
	Network string

	// Host is the Docker daemon host for remote connections (e.g., "tcp://192.168.1.10:2376").
	// If set, SocketPath is ignored and HTTP over TCP is used instead of Unix socket.
	Host string

	// TLSEnabled enables TLS for remote Docker daemon connections.
	TLSEnabled bool
}

// DefaultDockerConfig returns a DockerConfig with sensible defaults.
func DefaultDockerConfig() *DockerConfig {
	return &DockerConfig{
		SocketPath:   "/var/run/docker.sock",
		PollInterval: 10 * time.Second,
		LabelPrefix:  "olb.",
	}
}

// dockerContainer represents the JSON response from the Docker containers API.
type dockerContainer struct {
	ID              string            `json:"Id"`
	Names           []string          `json:"Names"`
	Image           string            `json:"Image"`
	State           string            `json:"State"`
	Status          string            `json:"Status"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings *dockerNetworks   `json:"NetworkSettings"`
}

// dockerNetworks holds the network settings from a container.
type dockerNetworks struct {
	Networks map[string]*dockerNetwork `json:"Networks"`
}

// dockerNetwork holds a single network configuration from a container.
type dockerNetwork struct {
	IPAddress string `json:"IPAddress"`
	Gateway   string `json:"Gateway"`
	NetworkID string `json:"NetworkID"`
}

// dockerInspect represents the JSON response from the Docker container inspect API.
type dockerInspect struct {
	ID              string            `json:"Id"`
	Name            string            `json:"Name"`
	State           dockerState       `json:"State"`
	Config          dockerConfig      `json:"Config"`
	NetworkSettings *dockerNetworks   `json:"NetworkSettings"`
	Labels          map[string]string `json:"-"` // populated from Config
}

// dockerState represents the container state from the inspect API.
type dockerState struct {
	Status  string `json:"Status"`
	Running bool   `json:"Running"`
}

// dockerConfig represents the container configuration from the inspect API.
type dockerConfig struct {
	Labels map[string]string `json:"Labels"`
}

// dockerEvent represents a Docker daemon event from the events stream.
type dockerEvent struct {
	Type   string           `json:"Type"`
	Action string           `json:"Action"`
	Actor  dockerEventActor `json:"Actor"`
	Time   int64            `json:"time"`
}

// dockerEventActor holds actor details for a Docker event.
type dockerEventActor struct {
	ID         string            `json:"ID"`
	Attributes map[string]string `json:"Attributes"`
}

// DockerProvider discovers services from Docker containers by connecting to the
// Docker daemon via its HTTP API over a Unix socket. It watches for containers
// with OLB labels and registers them as backend services.
//
// Containers are discovered using two mechanisms:
//  1. Polling: periodically lists running containers and detects changes.
//  2. Events: watches Docker events for container start/stop/die actions.
//
// Container labels control backend configuration:
//   - olb.enable=true   — include this container as a backend
//   - olb.port=8080     — backend port (required)
//   - olb.weight=1      — backend weight for load balancing
//   - olb.pool=web      — pool name assignment
//   - olb.tags=t1,t2    — comma-separated metadata tags
type DockerProvider struct {
	*baseProvider
	dockerConfig *DockerConfig
	client       *http.Client
	transport    *http.Transport
}

// NewDockerProvider creates a new Docker discovery provider.
func NewDockerProvider(config *ProviderConfig) (*DockerProvider, error) {
	if config.Type != ProviderTypeDocker {
		return nil, fmt.Errorf("invalid provider type: %q, expected %q", config.Type, ProviderTypeDocker)
	}

	dc := DefaultDockerConfig()

	// Parse options from ProviderConfig
	if socketPath, ok := config.Options["socket_path"]; ok && socketPath != "" {
		dc.SocketPath = socketPath
	}
	if host, ok := config.Options["host"]; ok && host != "" {
		dc.Host = host
	}
	if prefix, ok := config.Options["label_prefix"]; ok && prefix != "" {
		dc.LabelPrefix = prefix
	}
	if network, ok := config.Options["network"]; ok {
		dc.Network = network
	}
	if intervalStr, ok := config.Options["poll_interval"]; ok && intervalStr != "" {
		parsed, err := time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid poll_interval %q: %w", intervalStr, err)
		}
		if parsed < time.Second {
			parsed = time.Second
		}
		dc.PollInterval = parsed
	}
	if tlsStr, ok := config.Options["tls"]; ok && tlsStr == "true" {
		dc.TLSEnabled = true
	}

	return NewDockerProviderWithConfig(config, dc)
}

// NewDockerProviderWithConfig creates a new Docker provider with explicit Docker config.
func NewDockerProviderWithConfig(config *ProviderConfig, dc *DockerConfig) (*DockerProvider, error) {
	transport := &http.Transport{}

	if dc.Host == "" {
		// Unix socket mode
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", dc.SocketPath)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &DockerProvider{
		baseProvider: newBaseProvider(config.Name, ProviderTypeDocker, config),
		dockerConfig: dc,
		client:       client,
		transport:    transport,
	}, nil
}

// SetHTTPClient overrides the HTTP client used to communicate with Docker.
// This is primarily useful for testing.
func (p *DockerProvider) SetHTTPClient(client *http.Client) {
	p.client = client
}

// Start begins watching for Docker container changes.
func (p *DockerProvider) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Attempt initial container discovery; if Docker is not available,
	// start anyway and rely on polling to pick up containers later.
	_ = p.pollContainers()

	// Start background polling
	p.wg.Add(1)
	go p.pollLoop()

	// Start event watcher
	p.wg.Add(1)
	go p.watchEvents()

	return nil
}

// baseURL returns the base URL for Docker API requests.
func (p *DockerProvider) baseURL() string {
	if p.dockerConfig.Host != "" {
		scheme := "http"
		if p.dockerConfig.TLSEnabled {
			scheme = "https"
		}
		host := p.dockerConfig.Host
		// Strip protocol prefix if present
		for _, prefix := range []string{"tcp://", "http://", "https://"} {
			if strings.HasPrefix(host, prefix) {
				host = host[len(prefix):]
				break
			}
		}
		return scheme + "://" + host
	}
	// For Unix socket, the host portion is ignored by the transport dialer
	return "http://localhost"
}

// listContainers fetches running containers from the Docker API.
func (p *DockerProvider) listContainers() ([]dockerContainer, error) {
	url := p.baseURL() + "/containers/json?filters=" + urlEncode(`{"status":["running"]}`)

	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker API returned status %d", resp.StatusCode)
	}

	var containers []dockerContainer
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, fmt.Errorf("failed to decode container list: %w", err)
	}

	return containers, nil
}

// inspectContainer fetches detailed information about a specific container.
func (p *DockerProvider) inspectContainer(id string) (*dockerInspect, error) {
	url := p.baseURL() + "/containers/" + id + "/json"

	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker API returned status %d for container %s", resp.StatusCode, id)
	}

	var inspect dockerInspect
	if err := json.NewDecoder(resp.Body).Decode(&inspect); err != nil {
		return nil, fmt.Errorf("failed to decode container inspect: %w", err)
	}

	// Populate labels from Config
	inspect.Labels = inspect.Config.Labels

	return &inspect, nil
}

// isEnabled checks whether a container has the olb.enable=true label.
func (p *DockerProvider) isEnabled(labels map[string]string) bool {
	key := p.dockerConfig.LabelPrefix + "enable"
	val, ok := labels[key]
	return ok && val == "true"
}

// parseContainerLabels extracts OLB configuration from container labels.
func (p *DockerProvider) parseContainerLabels(labels map[string]string) (port int, weight int, pool string, tags []string) {
	prefix := p.dockerConfig.LabelPrefix

	// Parse port
	if portStr, ok := labels[prefix+"port"]; ok {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	// Parse weight (default 1)
	weight = 1
	if weightStr, ok := labels[prefix+"weight"]; ok {
		if w, err := strconv.Atoi(weightStr); err == nil && w > 0 {
			weight = w
		}
	}

	// Parse pool
	pool = labels[prefix+"pool"]

	// Parse tags
	if tagStr, ok := labels[prefix+"tags"]; ok && tagStr != "" {
		tags = splitTags(tagStr)
	}

	return port, weight, pool, tags
}

// splitTags splits a comma-separated tag string into individual tags.
func splitTags(s string) []string {
	var tags []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			tag := trimSpace(s[start:i])
			if tag != "" {
				tags = append(tags, tag)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		tag := trimSpace(s[start:])
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// extractContainerIP extracts the IP address of a container from its network settings.
// If a specific network is configured, the IP from that network is used.
// Otherwise, the first available network IP is returned.
func (p *DockerProvider) extractContainerIP(networks *dockerNetworks) string {
	if networks == nil || networks.Networks == nil {
		return ""
	}

	// If a specific network is configured, use that
	if p.dockerConfig.Network != "" {
		if net, ok := networks.Networks[p.dockerConfig.Network]; ok {
			return net.IPAddress
		}
		return ""
	}

	// Use the first available network
	for _, net := range networks.Networks {
		if net.IPAddress != "" {
			return net.IPAddress
		}
	}

	return ""
}

// containerName returns a clean container name (without leading slash).
func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	name := names[0]
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	return name
}

// containerToService converts a Docker container into a discovery Service.
// Returns nil if the container does not have the required labels or no IP.
func (p *DockerProvider) containerToService(container dockerContainer) *Service {
	if !p.isEnabled(container.Labels) {
		return nil
	}

	port, weight, pool, tags := p.parseContainerLabels(container.Labels)
	if port == 0 {
		return nil // Port is required
	}

	ip := p.extractContainerIP(container.NetworkSettings)
	if ip == "" {
		return nil
	}

	name := containerName(container.Names)
	if name == "" {
		name = container.ID[:12]
	}

	meta := map[string]string{
		"container_id": container.ID,
		"image":        container.Image,
	}
	if pool != "" {
		meta["pool"] = pool
	}

	// Merge provider tags with container tags
	allTags := make([]string, 0, len(p.config.Tags)+len(tags))
	allTags = append(allTags, p.config.Tags...)
	allTags = append(allTags, tags...)

	return &Service{
		ID:      fmt.Sprintf("%s-docker-%s", p.name, container.ID[:12]),
		Name:    name,
		Address: ip,
		Port:    port,
		Weight:  weight,
		Tags:    allTags,
		Meta:    meta,
		Healthy: true,
	}
}

// pollContainers fetches the list of running containers and reconciles with
// the current set of known services: adding new ones, removing stale ones.
func (p *DockerProvider) pollContainers() error {
	containers, err := p.listContainers()
	if err != nil {
		return err
	}

	currentIDs := make(map[string]bool)

	for _, container := range containers {
		svc := p.containerToService(container)
		if svc == nil {
			continue
		}
		currentIDs[svc.ID] = true
		p.addService(svc)
	}

	// Remove services that no longer exist
	for _, svc := range p.Services() {
		if !currentIDs[svc.ID] {
			p.removeService(svc.ID)
		}
	}

	return nil
}

// pollLoop periodically polls Docker for container changes.
func (p *DockerProvider) pollLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.dockerConfig.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			_ = p.pollContainers()
		}
	}
}

// watchEvents connects to the Docker events stream and processes container
// start/stop/die events in real time.
func (p *DockerProvider) watchEvents() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		err := p.streamEvents()
		if err != nil {
			// If context cancelled, exit
			select {
			case <-p.ctx.Done():
				return
			default:
			}

			// Backoff before reconnecting
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// streamEvents opens a connection to the Docker events API and processes
// container events until the connection is closed or context is cancelled.
func (p *DockerProvider) streamEvents() error {
	url := p.baseURL() + "/events?filters=" + urlEncode(`{"type":["container"],"event":["start","stop","die"]}`)

	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create events request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker events API returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-p.ctx.Done():
			return nil
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var event dockerEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		p.handleEvent(&event)
	}

	return scanner.Err()
}

// handleEvent processes a single Docker event.
func (p *DockerProvider) handleEvent(event *dockerEvent) {
	if event.Type != "container" {
		return
	}

	containerID := event.Actor.ID
	if containerID == "" {
		return
	}

	shortID := containerID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	serviceID := fmt.Sprintf("%s-docker-%s", p.name, shortID)

	switch event.Action {
	case "start":
		// A container started — refresh all containers to pick it up.
		// This is simpler and more reliable than trying to reconstruct
		// service info from the event alone.
		_ = p.pollContainers()

	case "stop", "die":
		// Remove the service if we were tracking it
		p.removeService(serviceID)
	}
}

// urlEncode performs minimal percent-encoding for URL query values.
// This avoids importing net/url for a simple encoding need.
func urlEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexUpper(c >> 4))
			b.WriteByte(hexUpper(c & 0x0f))
		}
	}
	return b.String()
}

// hexUpper returns the uppercase hex character for a nibble value.
func hexUpper(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'A' + (b - 10)
}

// Stop stops the Docker provider and cleans up resources.
func (p *DockerProvider) Stop() error {
	err := p.baseProvider.Stop()
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
	return err
}

// DockerConfig returns the Docker-specific configuration.
func (p *DockerProvider) DockerConfig() *DockerConfig {
	return p.dockerConfig
}

// readEventStream reads Docker events from a reader line by line and invokes
// handleEvent for each parsed event. This is exposed for testing.
func (p *DockerProvider) readEventStream(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event dockerEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		p.handleEvent(&event)
	}
	return scanner.Err()
}

func init() {
	// Register Docker provider factory
	RegisterProviderFactory(ProviderTypeDocker, func(config *ProviderConfig) (Provider, error) {
		return NewDockerProvider(config)
	})
}
