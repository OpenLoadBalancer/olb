package balancer

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/openloadbalancer/olb/internal/backend"
)

// LeastConnections implements a least connections load balancing algorithm.
// It selects the backend with the fewest active connections.
type LeastConnections struct {
	backends []*backend.Backend
	mu       sync.RWMutex
	// counter is used for tie-breaking
	counter atomic.Uint64
}

// NewLeastConnections creates a new LeastConnections balancer.
func NewLeastConnections() *LeastConnections {
	return &LeastConnections{
		backends: make([]*backend.Backend, 0),
	}
}

// Name returns the name of the balancer.
func (lc *LeastConnections) Name() string {
	return "least_connections"
}

// Next selects the backend with the least active connections.
// Returns nil if no backends are available.
func (lc *LeastConnections) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	// Track the best backend(s) for tie-breaking
	var best *backend.Backend
	minConns := int64(math.MaxInt64)
	tieCount := 0

	// First pass: find minimum connection count
	for _, b := range backends {
		conns := b.ActiveConns()
		if conns < minConns {
			minConns = conns
			best = b
			tieCount = 1
		} else if conns == minConns {
			tieCount++
		}
	}

	// If there's a tie, use round-robin to break it
	if tieCount > 1 && best != nil {
		// Collect all backends with minimum connections
		tied := make([]*backend.Backend, 0, tieCount)
		for _, b := range backends {
			if b.ActiveConns() == minConns {
				tied = append(tied, b)
			}
		}

		// Use atomic counter for round-robin selection among tied backends
		count := lc.counter.Add(1)
		index := int((count - 1) % uint64(len(tied)))
		best = tied[index]
	}

	return best
}

// Add adds a backend to the balancer.
func (lc *LeastConnections) Add(b *backend.Backend) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Check if backend already exists
	for _, existing := range lc.backends {
		if existing.ID == b.ID {
			return
		}
	}

	lc.backends = append(lc.backends, b)
}

// Remove removes a backend from the balancer.
func (lc *LeastConnections) Remove(id string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for i, b := range lc.backends {
		if b.ID == id {
			// Remove by swapping with last element and truncating
			lc.backends[i] = lc.backends[len(lc.backends)-1]
			lc.backends = lc.backends[:len(lc.backends)-1]
			return
		}
	}
}

// Update updates a backend in the balancer.
func (lc *LeastConnections) Update(b *backend.Backend) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for i, existing := range lc.backends {
		if existing.ID == b.ID {
			lc.backends[i] = b
			return
		}
	}
}

// lcWeightedBackend holds the state for a backend in the weighted least connections algorithm.
type lcWeightedBackend struct {
	backend *backend.Backend
	weight  int32
}

// WeightedLeastConnections implements a weighted least connections load balancing algorithm.
// It selects the backend with the minimum (active_connections / weight) ratio.
type WeightedLeastConnections struct {
	backends map[string]*lcWeightedBackend
	mu       sync.RWMutex
	// counter is used for tie-breaking
	counter atomic.Uint64
}

// NewWeightedLeastConnections creates a new WeightedLeastConnections balancer.
func NewWeightedLeastConnections() *WeightedLeastConnections {
	return &WeightedLeastConnections{
		backends: make(map[string]*lcWeightedBackend),
	}
}

// Name returns the name of the balancer.
func (wlc *WeightedLeastConnections) Name() string {
	return "weighted_least_connections"
}

// Next selects the backend with the minimum (connections / weight) ratio.
// Returns nil if no backends are available.
func (wlc *WeightedLeastConnections) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	wlc.mu.Lock()
	defer wlc.mu.Unlock()

	// Ensure all backends are in our state
	for _, b := range backends {
		if _, exists := wlc.backends[b.ID]; !exists {
			wlc.backends[b.ID] = &lcWeightedBackend{
				backend: b,
				weight:  b.GetWeight(),
			}
		}
	}

	// Find the backend with minimum (conns / weight) ratio
	var best *backend.Backend
	minRatio := float64(math.MaxFloat64)
	tieCount := 0

	for _, b := range backends {
		wb := wlc.backends[b.ID]
		if wb == nil {
			continue
		}

		conns := b.ActiveConns()
		weight := wb.weight
		if weight <= 0 {
			weight = 1 // Default to 1 to avoid division by zero
		}

		ratio := float64(conns) / float64(weight)

		if ratio < minRatio {
			minRatio = ratio
			best = b
			tieCount = 1
		} else if ratio == minRatio {
			tieCount++
		}
	}

	// If there's a tie, use round-robin to break it
	if tieCount > 1 && best != nil {
		// Collect all backends with minimum ratio
		tied := make([]*backend.Backend, 0, tieCount)
		for _, b := range backends {
			wb := wlc.backends[b.ID]
			if wb == nil {
				continue
			}
			weight := wb.weight
			if weight <= 0 {
				weight = 1
			}
			ratio := float64(b.ActiveConns()) / float64(weight)
			if ratio == minRatio {
				tied = append(tied, b)
			}
		}

		// Use atomic counter for round-robin selection among tied backends
		count := wlc.counter.Add(1)
		index := int((count - 1) % uint64(len(tied)))
		best = tied[index]
	}

	return best
}

// Add adds a backend to the balancer.
func (wlc *WeightedLeastConnections) Add(b *backend.Backend) {
	wlc.mu.Lock()
	defer wlc.mu.Unlock()

	if _, exists := wlc.backends[b.ID]; !exists {
		wlc.backends[b.ID] = &lcWeightedBackend{
			backend: b,
			weight:  b.GetWeight(),
		}
	}
}

// Remove removes a backend from the balancer.
func (wlc *WeightedLeastConnections) Remove(id string) {
	wlc.mu.Lock()
	defer wlc.mu.Unlock()

	delete(wlc.backends, id)
}

// Update updates a backend's weight in the balancer.
func (wlc *WeightedLeastConnections) Update(b *backend.Backend) {
	wlc.mu.Lock()
	defer wlc.mu.Unlock()

	if wb, exists := wlc.backends[b.ID]; exists {
		wb.weight = b.GetWeight()
		wb.backend = b
	}
}
