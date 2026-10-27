package balancer

import (
	"hash"
	"hash/fnv"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/openloadbalancer/olb/internal/backend"
)

// RendezvousHash implements the Rendezvous Hashing (Highest Random Weight) algorithm.
// This is an alternative to consistent hashing that provides:
// - Better distribution uniformity
// - Lower memory overhead
// - Simpler implementation
// - Similar minimal disruption when backends change
//
// Reference: https://en.wikipedia.org/wiki/Rendezvous_hashing
type RendezvousHash struct {
	mu       sync.RWMutex
	seeds    []uint32       // Per-backend seeds for randomness
	backends map[string]int // Backend ID to index mapping
}

// NewRendezvousHash creates a new RendezvousHash balancer.
func NewRendezvousHash() *RendezvousHash {
	return &RendezvousHash{
		seeds:    make([]uint32, 0),
		backends: make(map[string]int),
	}
}

// Name returns the name of the balancer.
func (r *RendezvousHash) Name() string {
	return "rendezvous_hash"
}

// Next selects the backend with the highest random weight for the given key.
// Uses ctx.ClientIP as the hash key when available for request affinity.
func (r *RendezvousHash) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Use client IP from context as hash key, fall back to counter-based key
	key := ""
	if ctx != nil {
		key = ctx.ClientIP
	}
	if key == "" {
		key = generateKey()
	}

	var bestBackend *backend.Backend
	maxWeight := uint64(0)

	for _, be := range backends {
		if !be.IsHealthy() {
			continue
		}

		// Calculate weight for this backend
		weight := r.computeWeight(key, be.ID)
		if weight > maxWeight {
			maxWeight = weight
			bestBackend = be
		}
	}

	// Fall back to first backend if none healthy
	if bestBackend == nil && len(backends) > 0 {
		return backends[0]
	}

	return bestBackend
}

// computeWeight calculates the weight for a given key-backend pair.
// Uses a pooled FNV hash for efficiency.
func (r *RendezvousHash) computeWeight(key, backendID string) uint64 {
	h := fnvPool.Get().(hash.Hash64)
	h.Reset()
	h.Write([]byte(key))
	h.Write([]byte(backendID))
	v := h.Sum64()
	fnvPool.Put(h)
	// FNV-1a has no finalization step: inputs differing only in their trailing
	// bytes (backend IDs "a"/"b"/"c" appended after the key) yield weights that
	// differ by small multiples of the FNV prime, so the high bits — which the
	// max comparison sees — barely change and selection is badly biased.
	// Apply the murmur3 64-bit finalizer to avalanche the bits.
	v ^= v >> 33
	v *= 0xff51afd7ed558ccd
	v ^= v >> 33
	v *= 0xc4ceb9fe1a85ec53
	v ^= v >> 33
	return v
}

// fnvPool reuses FNV hash objects to reduce per-request allocations.
var fnvPool = sync.Pool{
	New: func() any { return fnv.New64a() },
}

// generateKey creates a key for the request.
// Uses atomic counter to avoid mutex contention.
var keyCounter uint64

func generateKey() string {
	v := atomic.AddUint64(&keyCounter, 1)
	// Decimal text, not string(rune(v)): rune conversion collapses counters in
	// the surrogate range (0xD800-0xDFFF) and beyond 0x10FFFF to U+FFFD, making
	// distinct requests share one key from request #55,296 onward.
	return strconv.FormatUint(v, 10)
}

// Add registers a new backend.
func (r *RendezvousHash) Add(be *backend.Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.backends[be.ID]; !exists {
		r.backends[be.ID] = len(r.seeds)
		// Generate random seed for this backend
		r.seeds = append(r.seeds, hashString(be.ID))
	}
}

// Remove unregisters a backend.
func (r *RendezvousHash) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if idx, exists := r.backends[id]; exists {
		// Remove from slice by swapping with last element
		lastIdx := len(r.seeds) - 1
		if idx != lastIdx {
			r.seeds[idx] = r.seeds[lastIdx]
			// Update index for the moved backend
			for movedID, movedIdx := range r.backends {
				if movedIdx == lastIdx {
					r.backends[movedID] = idx
					break
				}
			}
		}
		r.seeds = r.seeds[:lastIdx]
		delete(r.backends, id)
	}
}

// Update is called when a backend is updated.
func (r *RendezvousHash) Update(be *backend.Backend) {
	// Seed is based on ID which shouldn't change
}

// hashString generates a hash from a string.
func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// Stats returns balancer statistics.
func (r *RendezvousHash) Stats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"algorithm":     "rendezvous_hash",
		"backend_count": len(r.backends),
		"seed_count":    len(r.seeds),
	}
}
