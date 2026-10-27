package utils

import (
	"hash/fnv"
	"math"
)

// BloomFilter is a probabilistic data structure for set membership testing.
// It may return false positives but never false negatives.
//
// Uses FNV-1a hash with k different hash functions derived from double hashing.
type BloomFilter struct {
	bits []uint64
	k    uint32 // number of hash functions
	n    uint32 // number of elements added
	m    uint32 // number of bits
}

// NewBloomFilter creates a new Bloom filter with expected n elements and false positive rate p.
// The optimal size and number of hash functions are calculated automatically.
func NewBloomFilter(n uint32, p float64) *BloomFilter {
	if n == 0 {
		n = 1000
	}
	if p <= 0 || p >= 1 {
		p = 0.01 // default 1% false positive rate
	}

	// Calculate optimal size: m = -n*ln(p) / (ln(2)^2)
	m := uint32(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2))
	// Round up to multiple of 64 for efficient bit manipulation
	m = ((m + 63) / 64) * 64
	if m == 0 {
		m = 64
	}

	// Calculate optimal k: k = m/n * ln(2)
	k := uint32(float64(m) / float64(n) * math.Ln2)
	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	return &BloomFilter{
		bits: make([]uint64, m/64),
		k:    k,
		n:    0,
		m:    m,
	}
}

// Add adds an element to the filter.
// The element is hashed and k bits are set.
func (bf *BloomFilter) Add(data []byte) {
	h1, h2 := bf.hash(data)

	for i := uint32(0); i < bf.k; i++ {
		idx := (h1 + i*h2) % bf.m
		bf.bits[idx/64] |= 1 << (idx % 64)
	}

	bf.n++
}

// AddString adds a string element to the filter.
func (bf *BloomFilter) AddString(s string) {
	bf.Add([]byte(s))
}

// Contains checks if an element might be in the filter.
// Returns true if the element might be in the set (could be false positive).
// Returns false if the element is definitely not in the set.
func (bf *BloomFilter) Contains(data []byte) bool {
	h1, h2 := bf.hash(data)

	for i := uint32(0); i < bf.k; i++ {
		idx := (h1 + i*h2) % bf.m
		if bf.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}

	return true
}

// ContainsString checks if a string element might be in the filter.
func (bf *BloomFilter) ContainsString(s string) bool {
	return bf.Contains([]byte(s))
}

// Clear resets the filter, removing all elements.
func (bf *BloomFilter) Clear() {
	for i := range bf.bits {
		bf.bits[i] = 0
	}
	bf.n = 0
}

// Len returns the number of elements added to the filter.
func (bf *BloomFilter) Len() uint32 {
	return bf.n
}

// Cap returns the capacity (number of bits) of the filter.
func (bf *BloomFilter) Cap() uint32 {
	return bf.m
}

// K returns the number of hash functions used.
func (bf *BloomFilter) K() uint32 {
	return bf.k
}

// FalsePositiveRate returns the theoretical false positive rate given the current fill.
func (bf *BloomFilter) FalsePositiveRate() float64 {
	// (1 - e^(-kn/m))^k
	m := float64(bf.m)
	n := float64(bf.n)
	k := float64(bf.k)

	return math.Pow(1-math.Exp(-k*n/m), k)
}

// hash computes FNV-1a hash and returns two 32-bit hashes for double hashing.
func (bf *BloomFilter) hash(data []byte) (uint32, uint32) {
	h := fnv.New64a()
	h.Write(data)
	sum := h.Sum64()

	// Split 64-bit hash into two 32-bit hashes
	h1 := uint32(sum)
	h2 := uint32(sum >> 32)

	if h2 == 0 {
		h2 = 1
	}

	return h1, h2
}

// EstimateFalsePositiveRate estimates the false positive rate for given n and p.
func EstimateFalsePositiveRate(n uint32, m uint32, k uint32) float64 {
	fn := float64(n)
	fm := float64(m)
	fk := float64(k)

	return math.Pow(1-math.Exp(-fk*fn/fm), fk)
}

// OptimalBloomFilter calculates the optimal Bloom filter parameters.
func OptimalBloomFilter(n uint32, p float64) (m uint32, k uint32) {
	// m = -n*ln(p) / (ln(2)^2)
	m = uint32(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2))
	m = ((m + 63) / 64) * 64
	if m == 0 {
		m = 64
	}

	// k = m/n * ln(2)
	k = uint32(float64(m) / float64(n) * math.Ln2)
	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	return m, k
}

// Merge merges another Bloom filter into this one.
// Both filters must have the same size and number of hash functions.
func (bf *BloomFilter) Merge(other *BloomFilter) error {
	if bf.m != other.m || bf.k != other.k {
		return ErrIncompatibleFilter
	}

	for i := range bf.bits {
		bf.bits[i] |= other.bits[i]
	}

	bf.n += other.n
	return nil
}
