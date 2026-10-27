package balancer

import (
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"github.com/openloadbalancer/olb/internal/backend"
)

const (
	// MaglevTableSize is the size of the Maglev lookup table.
	// Using a prime number as recommended in the Maglev paper.
	MaglevTableSize = 65537
)

// Maglev implements the Google Maglev consistent hashing algorithm.
//
// Maglev provides:
//   - O(1) lookup time after table construction
//   - Minimal disruption when backends are added/removed
//   - Perfect load balancing when the table size is large compared to the number of backends
//
// Reference: "Maglev: A Fast and Reliable Software Network Load Balancer"
// https://static.googleusercontent.com/media/research.google.com/en//pubs/archive/44824.pdf
type Maglev struct {
	mu           sync.RWMutex
	backends     []*backend.Backend
	backendMap   map[string]int // id -> index in backends slice
	lookupTable  []int          // lookup table: position -> backend index
	permutations [][]uint64     // permutation table for each backend
	needRebuild  bool           // flag to indicate lookup table needs rebuild
	counter      uint64         // for distributing requests
}

// NewMaglev creates a new Maglev consistent hash balancer.
func NewMaglev() *Maglev {
	return &Maglev{
		backends:     make([]*backend.Backend, 0),
		backendMap:   make(map[string]int),
		lookupTable:  make([]int, MaglevTableSize),
		permutations: make([][]uint64, 0),
		needRebuild:  false,
	}
}

// Name returns the algorithm name.
func (m *Maglev) Name() string {
	return "maglev"
}

// Next selects the next backend using consistent hashing.
// Uses ctx.ClientIP as the lookup key when available.
// Returns nil if no backend is available.
func (m *Maglev) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.backends) == 0 || len(backends) == 0 {
		return nil
	}

	// Rebuild lookup table if needed. Upgrade to write lock with double-check
	// to avoid the lock release/reacquire race condition.
	if m.needRebuild {
		m.mu.RUnlock()
		m.mu.Lock()
		if m.needRebuild {
			m.rebuildLocked()
		}
		m.mu.Unlock()
		m.mu.RLock()
	}

	// Use client IP from context as lookup key, fall back to counter-based key
	key := ""
	if ctx != nil {
		key = ctx.ClientIP
	}
	if key == "" {
		key = m.generateKey(backends)
	}

	// Hash the key to get position in lookup table
	pos := m.hashKey(key) % MaglevTableSize

	// Get backend index from lookup table
	backendIdx := m.lookupTable[pos]
	if backendIdx < 0 || backendIdx >= len(m.backends) {
		return nil
	}

	backendID := m.backends[backendIdx].ID

	// Find the backend in the provided list
	for _, b := range backends {
		if b.ID == backendID && b.IsAvailable() {
			return b
		}
	}

	// If the hashed backend is not available, find the next available one
	return m.findNextAvailable(pos, backends)
}

// generateKey creates a hash key for distributing requests.
func (m *Maglev) generateKey(_ []*backend.Backend) string {
	// Use counter to distribute requests across backends
	counter := atomic.AddUint64(&m.counter, 1)
	return fmt.Sprintf("request-%d", counter)
}

// findNextAvailable finds the next available backend starting from pos.
func (m *Maglev) findNextAvailable(startPos uint64, backends []*backend.Backend) *backend.Backend {
	if len(m.backends) == 0 {
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

	// Search forward from startPos
	for i := uint64(0); i < MaglevTableSize; i++ {
		pos := (startPos + i) % MaglevTableSize
		backendIdx := m.lookupTable[pos]
		if backendIdx >= 0 && backendIdx < len(m.backends) {
			backendID := m.backends[backendIdx].ID
			if available[backendID] {
				for _, b := range backends {
					if b.ID == backendID {
						return b
					}
				}
			}
		}
	}

	return nil
}

// Add registers a backend.
func (m *Maglev) Add(b *backend.Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.backendMap[b.ID]; exists {
		// Update existing backend
		idx := m.backendMap[b.ID]
		m.backends[idx] = b
	} else {
		// Add new backend
		idx := len(m.backends)
		m.backendMap[b.ID] = idx
		m.backends = append(m.backends, b)

		// Generate permutation for this backend
		perm := m.generatePermutation(b)
		m.permutations = append(m.permutations, perm)
	}

	m.needRebuild = true
}

// Remove deregisters a backend by ID.
func (m *Maglev) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, exists := m.backendMap[id]
	if !exists {
		return
	}

	// Remove from backends slice
	m.backends = append(m.backends[:idx], m.backends[idx+1:]...)

	// Remove from permutations
	m.permutations = append(m.permutations[:idx], m.permutations[idx+1:]...)

	// Rebuild backend map and update indices
	delete(m.backendMap, id)
	for i, backend := range m.backends {
		m.backendMap[backend.ID] = i
	}

	m.needRebuild = true
}

// Update updates a backend's state in the balancer.
func (m *Maglev) Update(b *backend.Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx, exists := m.backendMap[b.ID]; exists {
		m.backends[idx] = b
	}
}

// hashKey hashes a string key to a uint64.
func (m *Maglev) hashKey(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}

// generatePermutation generates the permutation table for a backend.
func (m *Maglev) generatePermutation(b *backend.Backend) []uint64 {
	perm := make([]uint64, MaglevTableSize)

	// Compute offset and skip from backend ID
	offset, skip := m.computeHashPair(b.ID)

	// Generate permutation
	for i := range MaglevTableSize {
		perm[i] = (offset + uint64(i)*skip) % MaglevTableSize
	}

	return perm
}

// computeHashPair computes offset and skip values from a backend ID.
func (m *Maglev) computeHashPair(backendID string) (offset, skip uint64) {
	// Use two different hash functions for offset and skip
	h1 := fnv.New64a()
	h1.Write([]byte(backendID))
	h1.Write([]byte("offset"))
	offset = h1.Sum64() % MaglevTableSize

	h2 := fnv.New64a()
	h2.Write([]byte(backendID))
	h2.Write([]byte("skip"))
	skip = h2.Sum64() % (MaglevTableSize - 1)
	if skip == 0 {
		skip = 1
	}

	return offset, skip
}

// rebuildLocked rebuilds the lookup table assuming the caller holds m.mu.
func (m *Maglev) rebuildLocked() {
	if !m.needRebuild {
		return
	}

	n := len(m.backends)
	if n == 0 {
		for i := range m.lookupTable {
			m.lookupTable[i] = -1
		}
		m.needRebuild = false
		return
	}

	m.permutations = make([][]uint64, n)
	for i, backend := range m.backends {
		m.permutations[i] = m.generatePermutation(backend)
	}

	for i := range m.lookupTable {
		m.lookupTable[i] = -1
	}

	next := make([]int, n)
	for range MaglevTableSize {
		minPos := ^uint64(0)
		minBackend := -1

		for j := range n {
			for next[j] < MaglevTableSize {
				pos := m.permutations[j][next[j]]
				if m.lookupTable[pos] == -1 {
					if pos < minPos {
						minPos = pos
						minBackend = j
					}
					break
				}
				next[j]++
			}
		}

		if minBackend >= 0 {
			m.lookupTable[minPos] = minBackend
			next[minBackend]++
		}
	}

	m.needRebuild = false
}
