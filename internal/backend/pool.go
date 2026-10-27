// Package backend provides backend pool management for OpenLoadBalancer.
package backend

import (
	"context"
	"net/http"
	"sync"
	"time"

	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// RequestContext carries per-request information that balancers can use
// for context-aware routing decisions (e.g., IP hash, cookie-based sticky).
type RequestContext struct {
	// ClientIP is the real client IP address.
	ClientIP string
	// Request is the original HTTP request (may be nil for non-HTTP traffic).
	Request *http.Request
}

// Balancer is the interface for load balancing algorithms.
// This is a minimal interface to avoid circular dependencies with the balancer package.
type Balancer interface {
	// Name returns the name of the balancer algorithm.
	Name() string

	// Next selects the next backend from the provided list.
	// The RequestContext carries request-level info (client IP, headers, etc.)
	// that context-aware algorithms can use. May be nil.
	// Returns nil if no backend is available.
	Next(ctx *RequestContext, backends []*Backend) *Backend

	// Add adds a backend to the balancer.
	Add(backend *Backend)

	// Remove removes a backend from the balancer by ID.
	Remove(id string)

	// Update updates a backend's state in the balancer.
	Update(backend *Backend)
}

// HealthCheckConfig defines health check settings for a pool.
type HealthCheckConfig struct {
	// Enabled indicates whether health checks are enabled.
	Enabled bool

	// Interval is the time between health checks.
	Interval time.Duration

	// Timeout is the maximum time to wait for a health check response.
	Timeout time.Duration

	// Path is the HTTP path for health checks (for HTTP backends).
	Path string

	// Port is the port to use for health checks (0 = use backend port).
	Port int
}

// Pool represents a group of backends with a load balancing algorithm.
type Pool struct {
	// Name is the unique identifier for this pool.
	Name string

	// Algorithm is the load balancing algorithm name.
	Algorithm string

	// Backends is a map of backend ID to Backend.
	Backends map[string]*Backend

	// balancer is the load balancing algorithm implementation.
	balancer Balancer

	// HealthCheck contains health check configuration.
	HealthCheck *HealthCheckConfig

	// mu protects the Backends map and balancer.
	mu sync.RWMutex
}

// NewPool creates a new Pool with the given name and algorithm.
func NewPool(name, algorithm string) *Pool {
	return &Pool{
		Name:        name,
		Algorithm:   algorithm,
		Backends:    make(map[string]*Backend),
		HealthCheck: &HealthCheckConfig{Enabled: false},
	}
}

// SetBalancer sets the balancer implementation for this pool.
func (p *Pool) SetBalancer(b Balancer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.balancer = b
}

// GetBalancer returns the balancer implementation for this pool.
// Returns nil if no balancer is configured.
func (p *Pool) GetBalancer() Balancer {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.balancer
}

// AddBackend adds a backend to the pool.
// Returns ErrAlreadyExist if a backend with the same ID already exists.
func (p *Pool) AddBackend(backend *Backend) error {
	if backend == nil {
		return olbErrors.ErrInvalidArg.WithContext("reason", "backend is nil")
	}
	if backend.ID == "" {
		return olbErrors.ErrInvalidArg.WithContext("reason", "backend ID is empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.Backends[backend.ID]; exists {
		return olbErrors.ErrAlreadyExist.WithContext("backend_id", backend.ID)
	}

	p.Backends[backend.ID] = backend
	if p.balancer != nil {
		p.balancer.Add(backend)
	}

	return nil
}

// RemoveBackend removes a backend from the pool by ID.
// Returns ErrBackendNotFound if the backend does not exist.
func (p *Pool) RemoveBackend(id string) error {
	if id == "" {
		return olbErrors.ErrInvalidArg.WithContext("reason", "backend ID is empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.Backends[id]; !exists {
		return olbErrors.ErrBackendNotFound.WithContext("backend_id", id)
	}

	delete(p.Backends, id)
	if p.balancer != nil {
		p.balancer.Remove(id)
	}

	return nil
}

// GetBackend retrieves a backend by ID.
// Returns nil if the backend does not exist.
func (p *Pool) GetBackend(id string) *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Backends[id]
}

// GetHealthyBackends returns a slice of backends that can receive new traffic.
// Only StateUp backends qualify: Draining and Maintenance backends must not be
// handed new requests (that is the point of draining), even though they may
// still hold in-flight connections.
// The returned slice is from a pool and should be returned via ReleaseHealthyBackends.
func (p *Pool) GetHealthyBackends() []*Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	healthy := healthySlicePool.Get().(*[]*Backend)
	*healthy = (*healthy)[:0]
	for _, backend := range p.Backends {
		if backend.IsAvailable() {
			*healthy = append(*healthy, backend)
		}
	}

	return *healthy
}

// ReleaseHealthyBackends returns a slice obtained from GetHealthyBackends to the pool.
func ReleaseHealthyBackends(s []*Backend) {
	healthySlicePool.Put(&s)
}

// healthySlicePool reuses slices returned by GetHealthyBackends.
var healthySlicePool = sync.Pool{
	New: func() any {
		s := make([]*Backend, 0, 8)
		return &s
	},
}

// NextBackend selects the next available backend using the configured balancer.
// Returns ErrPoolEmpty if no healthy backends are available.
// Returns ErrBackendNotFound if no balancer is configured.
func (p *Pool) NextBackend(ctx context.Context) (*Backend, error) {
	if ctx.Err() != nil {
		return nil, olbErrors.ErrCanceled
	}

	p.mu.RLock()
	balancer := p.balancer
	p.mu.RUnlock()

	if balancer == nil {
		return nil, olbErrors.ErrInternal.WithContext("reason", "no balancer configured")
	}

	healthy := p.GetHealthyBackends()
	if len(healthy) == 0 {
		return nil, olbErrors.ErrPoolEmpty.WithContext("pool", p.Name)
	}

	b := balancer.Next(nil, healthy)
	ReleaseHealthyBackends(healthy)
	if b == nil {
		return nil, olbErrors.ErrBackendUnavailable.WithContext("pool", p.Name)
	}

	return b, nil
}

// DrainBackend sets a backend's state to draining.
// Returns ErrBackendNotFound if the backend does not exist.
func (p *Pool) DrainBackend(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	backend, exists := p.Backends[id]
	if !exists {
		return olbErrors.ErrBackendNotFound.WithContext("backend_id", id)
	}

	backend.SetState(StateDraining)
	if p.balancer != nil {
		p.balancer.Update(backend)
	}

	return nil
}

// EnableBackend sets a backend's state to up.
// Returns ErrBackendNotFound if the backend does not exist.
func (p *Pool) EnableBackend(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	backend, exists := p.Backends[id]
	if !exists {
		return olbErrors.ErrBackendNotFound.WithContext("backend_id", id)
	}

	backend.SetState(StateUp)
	if p.balancer != nil {
		p.balancer.Update(backend)
	}

	return nil
}

// DisableBackend sets a backend's state to maintenance.
// Returns ErrBackendNotFound if the backend does not exist.
func (p *Pool) DisableBackend(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	backend, exists := p.Backends[id]
	if !exists {
		return olbErrors.ErrBackendNotFound.WithContext("backend_id", id)
	}

	backend.SetState(StateMaintenance)
	if p.balancer != nil {
		p.balancer.Update(backend)
	}

	return nil
}

// BackendCount returns the total number of backends in the pool.
func (p *Pool) BackendCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.Backends)
}

// HealthyCount returns the number of healthy backends in the pool.
func (p *Pool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, backend := range p.Backends {
		if backend.IsHealthy() {
			count++
		}
	}

	return count
}

// GetAllBackends returns a slice of all backends in the pool.
func (p *Pool) GetAllBackends() []*Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	backends := make([]*Backend, 0, len(p.Backends))
	for _, backend := range p.Backends {
		backends = append(backends, backend)
	}

	return backends
}

// Stats returns aggregated statistics for the pool.
func (p *Pool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		Name:            p.Name,
		TotalBackends:   len(p.Backends),
		HealthyBackends: 0,
		BackendStats:    make(map[string]BackendStats),
	}

	for id, backend := range p.Backends {
		if backend.IsHealthy() {
			stats.HealthyBackends++
		}
		backendStats := backend.Stats()
		stats.BackendStats[id] = backendStats
	}

	return stats
}

// Clone creates a deep copy of the pool.
// The balancer is NOT copied and must be set separately.
func (p *Pool) Clone() *Pool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	clone := &Pool{
		Name:        p.Name,
		Algorithm:   p.Algorithm,
		Backends:    make(map[string]*Backend, len(p.Backends)),
		HealthCheck: p.HealthCheck,
		balancer:    nil, // Balancer must be set separately
	}

	for id, backend := range p.Backends {
		clone.Backends[id] = backend
	}

	return clone
}
