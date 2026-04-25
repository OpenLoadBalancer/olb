package balancer

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/openloadbalancer/olb/internal/backend"
)

// DefaultRingVirtualNodes is the default number of virtual nodes per backend.
const DefaultRingVirtualNodes = 150

// ringHashNode represents a single node on the hash ring.
type ringHashNode struct {
	hash      uint32
	backendID string
}

// RingHash implements consistent hashing using a sorted slice of ring positions.
// It is similar to ConsistentHash but uses FNV-1a and a different ring structure.
type RingHash struct {
	mu       sync.RWMutex
	ring     []ringHashNode
	backends map[string]*backend.Backend
	vnodes   int
	hashFunc func(data []byte) uint32
	counter  uint64 // for distributing requests when no key provided
}

// NewRingHash creates a new RingHash balancer with default settings.
func NewRingHash() *RingHash {
	return NewRingHashWithConfig(DefaultRingVirtualNodes, nil)
}

// NewRingHashWithConfig creates a new RingHash balancer with custom configuration.
func NewRingHashWithConfig(vnodes int, hashFunc func(data []byte) uint32) *RingHash {
	if vnodes <= 0 {
		vnodes = DefaultRingVirtualNodes
	}
	if hashFunc == nil {
		hashFunc = defaultRingHashFunc
	}
	return &RingHash{
		ring:     make([]ringHashNode, 0),
		backends: make(map[string]*backend.Backend),
		vnodes:   vnodes,
		hashFunc: hashFunc,
	}
}

// defaultRingHashFunc uses FNV-1a for consistent hashing.
func defaultRingHashFunc(data []byte) uint32 {
	h := fnv.New32a()
	h.Write(data)
	return h.Sum32()
}

// Name returns the algorithm name.
func (rh *RingHash) Name() string {
	return "ring_hash"
}

// Next selects the next backend using consistent hashing.
// Uses ctx.ClientIP as the hash key when available for request affinity.
// Returns nil if no backends are available.
func (rh *RingHash) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	if len(rh.ring) == 0 || len(backends) == 0 {
		return nil
	}

	// Use client IP from context as hash key, fall back to counter-based key
	key := ""
	if ctx != nil {
		key = ctx.ClientIP
	}
	if key == "" {
		key = rh.generateKey(backends)
	}
	hash := rh.hashFunc([]byte(key))

	// Binary search for the first ring position >= hash
	idx := sort.Search(len(rh.ring), func(i int) bool {
		return rh.ring[i].hash >= hash
	})

	// If we went past the end, wrap around to the beginning
	if idx >= len(rh.ring) {
		idx = 0
	}

	backendID := rh.ring[idx].backendID

	// Find the backend in the provided list
	for _, b := range backends {
		if b.ID == backendID && b.IsAvailable() {
			return b
		}
	}

	// If the hashed backend is not available, find the next available one
	return rh.findNextAvailable(idx, backends)
}

// generateKey creates a hash key from the backends list.
// Uses a combination of available backend IDs for distribution.
func (rh *RingHash) generateKey(_ []*backend.Backend) string {
	// Use counter to distribute requests across backends
	// This ensures different requests go to different backends
	counter := atomic.AddUint64(&rh.counter, 1)
	return fmt.Sprintf("request-%d", counter)
}

// findNextAvailable finds the next available backend starting from idx.
func (rh *RingHash) findNextAvailable(startIdx int, backends []*backend.Backend) *backend.Backend {
	if len(rh.ring) == 0 {
		return nil
	}

	// Create a set of available backend IDs
	available := make(map[string]bool, len(backends))
	for _, b := range backends {
		if b.IsAvailable() {
			available[b.ID] = true
		}
	}

	if len(available) == 0 {
		return nil
	}

	// Search forward from startIdx
	for i := range len(rh.ring) {
		idx := (startIdx + i) % len(rh.ring)
		if available[rh.ring[idx].backendID] {
			for _, b := range backends {
				if b.ID == rh.ring[idx].backendID {
					return b
				}
			}
		}
	}

	return nil
}

// Add registers a backend to the hash ring.
func (rh *RingHash) Add(b *backend.Backend) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	// Check if backend already exists
	if _, exists := rh.backends[b.ID]; exists {
		return
	}

	rh.backends[b.ID] = b
	rh.rebuildRing()
}

// Remove deregisters a backend from the hash ring.
func (rh *RingHash) Remove(id string) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	if _, exists := rh.backends[id]; !exists {
		return
	}

	delete(rh.backends, id)
	rh.rebuildRing()
}

// Update updates a backend's state in the balancer.
func (rh *RingHash) Update(b *backend.Backend) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	if _, exists := rh.backends[b.ID]; !exists {
		return
	}

	// Check if weight changed
	oldBackend := rh.backends[b.ID]
	if oldBackend.GetWeight() != b.GetWeight() {
		rh.backends[b.ID] = b
		rh.rebuildRing()
	} else {
		rh.backends[b.ID] = b
	}
}

// rebuildRing rebuilds the entire ring from scratch.
// Must be called with lock held.
func (rh *RingHash) rebuildRing() {
	rh.ring = make([]ringHashNode, 0, len(rh.backends)*rh.vnodes)

	for _, b := range rh.backends {
		weight := int(b.GetWeight())
		if weight <= 0 {
			weight = 1
		}
		numVNodes := rh.vnodes * weight
		// Cap virtual nodes to prevent memory exhaustion with large weights
		numVNodes = min(numVNodes, 10000)

		for i := range numVNodes {
			key := b.ID + "#" + intToStr(i)
			node := ringHashNode{
				hash:      rh.hashFunc([]byte(key)),
				backendID: b.ID,
			}
			rh.ring = append(rh.ring, node)
		}
	}

	sort.Slice(rh.ring, func(i, j int) bool {
		return rh.ring[i].hash < rh.ring[j].hash
	})
}

// intToStr converts an integer to string without allocation.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)
	isNegative := n < 0
	if isNegative {
		n = -n
	}

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if isNegative {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}
