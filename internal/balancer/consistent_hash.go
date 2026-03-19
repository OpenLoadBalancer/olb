package balancer

import (
	"hash/crc32"
	"sort"
	"sync"

	"github.com/openloadbalancer/olb/internal/backend"
)

// DefaultVirtualNodes is the default number of virtual nodes per backend.
const DefaultVirtualNodes = 150

// ringNode represents a single node on the consistent hash ring.
type ringNode struct {
	hash    uint32
	backend string
}

// consistentRing is a sorted list of ring nodes.
type consistentRing struct {
	nodes []ringNode
}

// ConsistentHash implements the Ketama consistent hashing algorithm.
// It provides good distribution of keys across backends with minimal
// redistribution when backends are added or removed.
type ConsistentHash struct {
	ring     *consistentRing
	backends map[string]*backend.Backend
	vnodes   int // virtual nodes per backend
	mu       sync.RWMutex
}

// NewConsistentHash creates a new ConsistentHash balancer with the specified
// number of virtual nodes per backend. If vnodes is 0, DefaultVirtualNodes is used.
func NewConsistentHash(vnodes int) *ConsistentHash {
	if vnodes <= 0 {
		vnodes = DefaultVirtualNodes
	}
	return &ConsistentHash{
		ring: &consistentRing{
			nodes: make([]ringNode, 0),
		},
		backends: make(map[string]*backend.Backend),
		vnodes:   vnodes,
	}
}

// Name returns the name of the balancer.
func (ch *ConsistentHash) Name() string {
	return "consistent_hash"
}

// Next selects the next backend using consistent hashing.
// Without a key, it hashes the first available backend's address as a
// deterministic fallback. For proper consistent hashing with request
// affinity, use NextWithKey instead.
func (ch *ConsistentHash) Next(backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}
	// Deterministic fallback: hash the first available backend's address.
	// Callers should prefer NextWithKey for request-aware routing.
	key := backends[0].Address
	return ch.NextWithKey(backends, key)
}

// NextWithKey selects a backend using consistent hashing on the provided key.
// The key is typically derived from request context (e.g. client IP, URL path,
// or a combination). The same key will consistently map to the same backend
// as long as the backend set is stable.
func (ch *ConsistentHash) NextWithKey(backends []*backend.Backend, key string) *backend.Backend {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring.nodes) == 0 || len(backends) == 0 {
		return nil
	}

	hash := ch.hashKey(key)

	backendID := ch.getNode(hash)
	if backendID == "" {
		return nil
	}

	// Find the backend in the provided list
	for _, b := range backends {
		if b.ID == backendID && b.IsAvailable() {
			return b
		}
	}

	// Fallback: if the selected backend is not in the list or not available,
	// find the next available backend on the ring
	return ch.findNextAvailable(hash, backends)
}

// findNextAvailable finds the next available backend on the ring starting from the given hash.
func (ch *ConsistentHash) findNextAvailable(hash uint32, backends []*backend.Backend) *backend.Backend {
	// Create a set of available backend IDs for quick lookup
	available := make(map[string]*backend.Backend, len(backends))
	for _, b := range backends {
		if b.IsAvailable() {
			available[b.ID] = b
		}
	}

	if len(available) == 0 {
		return nil
	}

	// Search forward on the ring
	idx := ch.search(hash)
	for i := 0; i < len(ch.ring.nodes); i++ {
		node := ch.ring.nodes[(idx+i)%len(ch.ring.nodes)]
		if b, ok := available[node.backend]; ok {
			return b
		}
	}

	return nil
}

// Add adds a backend to the consistent hash ring with virtual nodes.
func (ch *ConsistentHash) Add(b *backend.Backend) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	// Remove existing backend if present to avoid duplicates
	ch.removeLocked(b.ID)

	ch.backends[b.ID] = b

	// Add virtual nodes for this backend
	for i := 0; i < ch.vnodes; i++ {
		vnodeKey := b.ID + ":" + intToString(i)
		hash := ch.hashKey(vnodeKey)
		ch.ring.nodes = append(ch.ring.nodes, ringNode{
			hash:    hash,
			backend: b.ID,
		})
	}

	// Sort the ring by hash
	ch.sortRing()
}

// Remove removes a backend and all its virtual nodes from the ring.
func (ch *ConsistentHash) Remove(id string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.removeLocked(id)
}

// removeLocked removes a backend without acquiring the lock.
// Must be called with lock held.
func (ch *ConsistentHash) removeLocked(id string) {
	delete(ch.backends, id)

	// Remove all virtual nodes for this backend
	newNodes := make([]ringNode, 0, len(ch.ring.nodes))
	for _, node := range ch.ring.nodes {
		if node.backend != id {
			newNodes = append(newNodes, node)
		}
	}
	ch.ring.nodes = newNodes
}

// Update updates a backend's state in the balancer.
// For consistent hash, this just updates the backend reference.
func (ch *ConsistentHash) Update(b *backend.Backend) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if _, exists := ch.backends[b.ID]; exists {
		ch.backends[b.ID] = b
	}
}

// hashKey computes a hash for the given key using CRC32.
func (ch *ConsistentHash) hashKey(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// getNode finds the backend for a given hash using binary search.
// Returns the first node with hash >= key hash (wrap around if needed).
func (ch *ConsistentHash) getNode(hash uint32) string {
	if len(ch.ring.nodes) == 0 {
		return ""
	}

	idx := ch.search(hash)
	return ch.ring.nodes[idx].backend
}

// search performs binary search to find the first node with hash >= target hash.
// Returns the index of the node. If all nodes have hash < target, returns 0 (wrap around).
func (ch *ConsistentHash) search(hash uint32) int {
	nodes := ch.ring.nodes
	if len(nodes) == 0 {
		return 0
	}

	// Binary search for the first node with hash >= target
	idx := sort.Search(len(nodes), func(i int) bool {
		return nodes[i].hash >= hash
	})

	// If no node found with hash >= target, wrap around to first node
	if idx >= len(nodes) {
		idx = 0
	}

	return idx
}

// sortRing sorts the ring nodes by hash.
func (ch *ConsistentHash) sortRing() {
	sort.Slice(ch.ring.nodes, func(i, j int) bool {
		return ch.ring.nodes[i].hash < ch.ring.nodes[j].hash
	})
}

// intToString converts an integer to string without allocation.
// This is a simple implementation; for production, use a faster method.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	// Handle negative numbers
	negative := n < 0
	if negative {
		n = -n
	}

	// Convert to string
	var buf [20]byte // enough for 64-bit int
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}

// GetVirtualNodes returns the number of virtual nodes per backend.
func (ch *ConsistentHash) GetVirtualNodes() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return ch.vnodes
}

// SetVirtualNodes sets the number of virtual nodes per backend.
// This rebuilds the entire ring. Use with caution.
func (ch *ConsistentHash) SetVirtualNodes(vnodes int) {
	if vnodes <= 0 {
		vnodes = DefaultVirtualNodes
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.vnodes = vnodes

	// Rebuild the ring
	ch.ring.nodes = make([]ringNode, 0, len(ch.backends)*ch.vnodes)
	for _, b := range ch.backends {
		for i := 0; i < ch.vnodes; i++ {
			vnodeKey := b.ID + ":" + intToString(i)
			hash := ch.hashKey(vnodeKey)
			ch.ring.nodes = append(ch.ring.nodes, ringNode{
				hash:    hash,
				backend: b.ID,
			})
		}
	}

	ch.sortRing()
}

// RingSize returns the total number of virtual nodes on the ring.
func (ch *ConsistentHash) RingSize() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.ring.nodes)
}

// BackendCount returns the number of backends.
func (ch *ConsistentHash) BackendCount() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.backends)
}
