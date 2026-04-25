package balancer

import (
	"sync"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/pkg/utils"
)

// Random implements a uniform random load balancing algorithm.
// It selects backends randomly with equal probability.
type Random struct {
	backends []*backend.Backend
	rnd      *utils.FastRand
	mu       sync.RWMutex
}

// NewRandom creates a new Random balancer.
func NewRandom() *Random {
	return &Random{
		backends: make([]*backend.Backend, 0),
		rnd:      utils.NewFastRand(),
	}
}

// Name returns the name of the balancer.
func (r *Random) Name() string {
	return "random"
}

// Next selects a random backend from the provided list.
// Returns nil if no backends are available.
func (r *Random) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	// Get random int64 and compute index
	// backendIndex = random % len(backends)
	random := r.rnd.Int63()
	index := int(random % int64(len(backends)))

	return backends[index]
}

// Add adds a backend to the balancer's internal tracking.
func (r *Random) Add(b *backend.Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already exists
	for _, existing := range r.backends {
		if existing.ID == b.ID {
			return
		}
	}
	r.backends = append(r.backends, b)
}

// Remove removes a backend from the balancer's internal tracking.
func (r *Random) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, b := range r.backends {
		if b.ID == id {
			// Remove by swapping with last element
			r.backends[i] = r.backends[len(r.backends)-1]
			r.backends = r.backends[:len(r.backends)-1]
			return
		}
	}
}

// Update is a no-op for Random (stateless algorithm).
func (r *Random) Update(b *backend.Backend) {
	// No-op: random doesn't maintain per-backend state
}

// wrWeightedBackend holds the state for a backend in the weighted random algorithm.
type wrWeightedBackend struct {
	backend *backend.Backend
	weight  int32
}

// WeightedRandom implements a weighted random load balancing algorithm.
// Backends are selected with probability proportional to their weight.
type WeightedRandom struct {
	backends []*wrWeightedBackend
	total    int64
	rnd      *utils.FastRand
	mu       sync.RWMutex
}

// NewWeightedRandom creates a new WeightedRandom balancer.
func NewWeightedRandom() *WeightedRandom {
	return &WeightedRandom{
		backends: make([]*wrWeightedBackend, 0),
		rnd:      utils.NewFastRand(),
	}
}

// Name returns the name of the balancer.
func (wr *WeightedRandom) Name() string {
	return "weighted_random"
}

// Next selects a backend using weighted random selection.
// Returns nil if no backends are available.
func (wr *WeightedRandom) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	wr.mu.RLock()
	defer wr.mu.RUnlock()

	// Calculate total weight and select in a single pass
	var total int64
	for _, b := range backends {
		w := b.GetWeight()
		if w <= 0 {
			w = 1
		}
		total += int64(w)
	}

	if total == 0 {
		return nil
	}

	// Get random value in range [0, total)
	random := wr.rnd.Int63n(total)

	// Walk through backends, subtracting weight until random < 0
	for _, b := range backends {
		w := b.GetWeight()
		if w <= 0 {
			w = 1
		}
		random -= int64(w)
		if random < 0 {
			return b
		}
	}

	// Fallback to last backend
	return backends[len(backends)-1]
}

// Add adds a backend to the balancer.
func (wr *WeightedRandom) Add(b *backend.Backend) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	// Check if already exists
	for _, existing := range wr.backends {
		if existing.backend.ID == b.ID {
			return
		}
	}

	weight := b.GetWeight()
	if weight <= 0 {
		weight = 1
	}

	wr.backends = append(wr.backends, &wrWeightedBackend{
		backend: b,
		weight:  weight,
	})
	wr.total += int64(weight)
}

// Remove removes a backend from the balancer.
func (wr *WeightedRandom) Remove(id string) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	for i, wb := range wr.backends {
		if wb.backend.ID == id {
			wr.total -= int64(wb.weight)
			// Remove by swapping with last element
			wr.backends[i] = wr.backends[len(wr.backends)-1]
			wr.backends = wr.backends[:len(wr.backends)-1]
			return
		}
	}
}

// Update updates a backend's weight in the balancer.
func (wr *WeightedRandom) Update(b *backend.Backend) {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	for _, wb := range wr.backends {
		if wb.backend.ID == b.ID {
			// Adjust total
			wr.total -= int64(wb.weight)
			// Update weight
			newWeight := b.GetWeight()
			if newWeight <= 0 {
				newWeight = 1
			}
			wb.weight = newWeight
			wr.total += int64(newWeight)
			return
		}
	}
}
