package balancer

import (
	"sync"

	"github.com/openloadbalancer/olb/internal/backend"
)

// weightedBackend holds the state for a backend in the weighted round-robin algorithm.
// This implements the Nginx smooth weighted round-robin algorithm.
type weightedBackend struct {
	backend       *backend.Backend
	weight        int32
	currentWeight int32
}

// WeightedRoundRobin implements a smooth weighted round-robin load balancing algorithm.
// Based on the Nginx implementation for even distribution across weighted backends.
type WeightedRoundRobin struct {
	mu        sync.RWMutex
	backends  map[string]*weightedBackend
	callCount uint64
}

// NewWeightedRoundRobin creates a new WeightedRoundRobin balancer.
func NewWeightedRoundRobin() *WeightedRoundRobin {
	return &WeightedRoundRobin{
		backends: make(map[string]*weightedBackend),
	}
}

// Name returns the name of the balancer.
func (wrr *WeightedRoundRobin) Name() string {
	return "weighted_round_robin"
}

// Next selects the next backend using smooth weighted round-robin.
// Returns nil if no backends are available.
func (wrr *WeightedRoundRobin) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	// Build a list of weighted backends from the provided healthy backends
	weighted := make([]*weightedBackend, 0, len(backends))
	totalWeight := int32(0)

	for _, b := range backends {
		if wb, exists := wrr.backends[b.ID]; exists {
			// Update the backend reference in case it changed
			wb.backend = b
			weighted = append(weighted, wb)
			totalWeight += wb.weight
		} else {
			// Backend not in our state, add it with default weight
			wb = &weightedBackend{
				backend:       b,
				weight:        b.GetWeight(),
				currentWeight: 0,
			}
			wrr.backends[b.ID] = wb
			weighted = append(weighted, wb)
			totalWeight += wb.weight
		}
	}

	if len(weighted) == 0 {
		return nil
	}

	// Smooth weighted round-robin algorithm (Nginx style)
	// 1. Add weight to each backend's current weight
	// 2. Select backend with highest current weight
	// 3. Subtract total weight from selected backend's current weight

	var best *weightedBackend
	for _, wb := range weighted {
		wb.currentWeight += wb.weight
		if best == nil || wb.currentWeight > best.currentWeight {
			best = wb
		}
	}

	if best != nil {
		best.currentWeight -= totalWeight

		// Periodically normalize weights to prevent int32 overflow.
		// After 1M calls, reset all currentWeight values toward zero
		// while preserving relative ordering.
		wrr.callCount++
		if wrr.callCount >= 1_000_000 {
			for _, wb := range weighted {
				wb.currentWeight = wb.currentWeight / 2
			}
			wrr.callCount = 0
		}

		return best.backend
	}

	return nil
}

// Add adds a backend to the balancer.
func (wrr *WeightedRoundRobin) Add(b *backend.Backend) {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	if _, exists := wrr.backends[b.ID]; !exists {
		wrr.backends[b.ID] = &weightedBackend{
			backend:       b,
			weight:        b.GetWeight(),
			currentWeight: 0,
		}
	}
}

// Remove removes a backend from the balancer.
func (wrr *WeightedRoundRobin) Remove(id string) {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	delete(wrr.backends, id)
}

// Update updates a backend's weight in the balancer.
func (wrr *WeightedRoundRobin) Update(b *backend.Backend) {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	if wb, exists := wrr.backends[b.ID]; exists {
		wb.weight = b.GetWeight()
		wb.backend = b
	}
}

// Reset resets all current weights to zero.
// This can be useful for testing or when backends change significantly.
func (wrr *WeightedRoundRobin) Reset() {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	for _, wb := range wrr.backends {
		wb.currentWeight = 0
	}
}
