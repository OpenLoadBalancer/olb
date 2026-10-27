package utils

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewBloomFilter(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	if bf.Cap() == 0 {
		t.Error("Capacity should be > 0")
	}

	if bf.K() == 0 {
		t.Error("K should be > 0")
	}

	// Default values
	bf2 := NewBloomFilter(0, 0)
	if bf2.Cap() == 0 {
		t.Error("Should use default capacity when n=0")
	}
}

func TestBloomFilter_Basic(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	// Add some items
	items := []string{"apple", "banana", "cherry", "date", "elderberry"}
	for _, item := range items {
		bf.AddString(item)
	}

	// Check all items are present
	for _, item := range items {
		if !bf.ContainsString(item) {
			t.Errorf("ContainsString(%s) = false, expected true", item)
		}
	}

	// Check non-existent items (may have false positives)
	notInFilter := []string{"grape", "kiwi", "mango"}
	falsePositives := 0
	for _, item := range notInFilter {
		if bf.ContainsString(item) {
			falsePositives++
		}
	}

	// Allow for some false positives with 1% rate
	if falsePositives > 1 {
		t.Logf("False positives: %d/%d", falsePositives, len(notInFilter))
	}
}

func TestBloomFilter_ByteSlice(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	data := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		[]byte("test"),
	}

	for _, d := range data {
		bf.Add(d)
	}

	for _, d := range data {
		if !bf.Contains(d) {
			t.Errorf("Contains(%s) = false", string(d))
		}
	}
}

func TestBloomFilter_Clear(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	bf.AddString("test")
	if !bf.ContainsString("test") {
		t.Error("Item should be present before Clear")
	}

	bf.Clear()

	if bf.ContainsString("test") {
		t.Error("Item should not be present after Clear")
	}

	if bf.Len() != 0 {
		t.Errorf("Len() = %d, want 0", bf.Len())
	}
}

func TestBloomFilter_Len(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	if bf.Len() != 0 {
		t.Errorf("Initial Len() = %d, want 0", bf.Len())
	}

	bf.AddString("a")
	if bf.Len() != 1 {
		t.Errorf("Len() = %d, want 1", bf.Len())
	}

	bf.AddString("b")
	if bf.Len() != 2 {
		t.Errorf("Len() = %d, want 2", bf.Len())
	}

	// Adding same item again
	bf.AddString("a")
	if bf.Len() != 3 {
		// Bloom filter doesn't track duplicates
		t.Errorf("Len() = %d, want 3", bf.Len())
	}
}

func TestBloomFilter_FalsePositiveRate(t *testing.T) {
	// Test with high false positive rate
	bf := NewBloomFilter(100, 0.5) // 50% FP rate

	// Add items
	for i := 0; i < 50; i++ {
		bf.AddString(fmt.Sprintf("item%d", i))
	}

	// Check that items are present
	for i := 0; i < 50; i++ {
		if !bf.ContainsString(fmt.Sprintf("item%d", i)) {
			t.Errorf("Item %d should be present", i)
		}
	}

	// Check FP rate
	fpCount := 0
	for i := 100; i < 200; i++ {
		if bf.ContainsString(fmt.Sprintf("item%d", i)) {
			fpCount++
		}
	}

	t.Logf("False positive rate: %d%% (%d/100)", fpCount, fpCount)
}

func TestBloomFilter_Merge(t *testing.T) {
	bf1 := NewBloomFilter(100, 0.01)
	bf2 := NewBloomFilter(100, 0.01)

	bf1.AddString("a")
	bf2.AddString("b")

	err := bf1.Merge(bf2)
	if err != nil {
		t.Errorf("Merge failed: %v", err)
	}

	if !bf1.ContainsString("a") {
		t.Error("Should contain 'a' after merge")
	}
	if !bf1.ContainsString("b") {
		t.Error("Should contain 'b' after merge")
	}
}

func TestBloomFilter_MergeIncompatible(t *testing.T) {
	bf1 := NewBloomFilter(100, 0.01)
	bf2 := NewBloomFilter(200, 0.01)

	err := bf1.Merge(bf2)
	if err == nil {
		t.Error("Merge should fail with different capacities")
	}
}

func TestOptimalBloomFilter(t *testing.T) {
	m, k := OptimalBloomFilter(1000, 0.01)

	if m == 0 {
		t.Error("m should be > 0")
	}
	if k == 0 {
		t.Error("k should be > 0")
	}

	// Verify m is multiple of 64
	if m%64 != 0 {
		t.Errorf("m should be multiple of 64, got %d", m)
	}

	// Estimate false positive rate
	fpRate := EstimateFalsePositiveRate(1000, m, k)
	t.Logf("Estimated FP rate: %f", fpRate)
}

func BenchmarkBloomFilter_Add(b *testing.B) {
	bf := NewBloomFilter(10000, 0.01)
	data := []byte("benchmark data")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(data)
	}
}

func BenchmarkBloomFilter_Contains(b *testing.B) {
	bf := NewBloomFilter(10000, 0.01)
	data := []byte("benchmark data")
	bf.Add(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Contains(data)
	}
}

// TestBloomFilter_AddContains tests basic Add and Contains operations
func TestBloomFilter_AddContains(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	// Add various items
	items := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		[]byte("test"),
		[]byte("bloom"),
		[]byte("filter"),
	}

	for _, item := range items {
		bf.Add(item)
	}

	// All added items should be present
	for _, item := range items {
		if !bf.Contains(item) {
			t.Errorf("Contains(%s) = false, expected true", string(item))
		}
	}

	// Check length
	if bf.Len() != uint32(len(items)) {
		t.Errorf("Len() = %d, want %d", bf.Len(), len(items))
	}
}

// TestBloomFilter_FalsePositiveRateDetailed tests actual false positive rate
func TestBloomFilter_FalsePositiveRateDetailed(t *testing.T) {
	// Create filter with 1% target FP rate
	n := uint32(1000)
	bf := NewBloomFilter(n, 0.01)

	// Add n items
	for i := uint32(0); i < n; i++ {
		bf.AddString(fmt.Sprintf("item%d", i))
	}

	// Check actual FP rate with items not in filter
	falsePositives := 0
	testCount := 1000
	for i := uint32(0); i < uint32(testCount); i++ {
		if bf.ContainsString(fmt.Sprintf("notitem%d", i)) {
			falsePositives++
		}
	}

	actualRate := float64(falsePositives) / float64(testCount)
	t.Logf("Actual false positive rate: %.4f (%d/%d)", actualRate, falsePositives, testCount)

	// Allow some tolerance - actual rate should be close to target
	if actualRate > 0.05 { // 5% upper bound
		t.Errorf("False positive rate too high: %.4f", actualRate)
	}

	// Check theoretical rate
	theoreticalRate := bf.FalsePositiveRate()
	t.Logf("Theoretical false positive rate: %.4f", theoreticalRate)
}

// TestBloomFilter_EstimateParameters tests parameter estimation functions
func TestBloomFilter_EstimateParameters(t *testing.T) {
	tests := []struct {
		n        uint32
		p        float64
		wantMMin uint32
		wantKMin uint32
	}{
		{1000, 0.01, 9000, 5},
		{10000, 0.001, 140000, 8},
		{100, 0.1, 400, 2},
	}

	for _, tt := range tests {
		m, k := OptimalBloomFilter(tt.n, tt.p)

		if m < tt.wantMMin {
			t.Errorf("OptimalBloomFilter(%d, %f) m = %d, want >= %d", tt.n, tt.p, m, tt.wantMMin)
		}
		if k < tt.wantKMin {
			t.Errorf("OptimalBloomFilter(%d, %f) k = %d, want >= %d", tt.n, tt.p, k, tt.wantKMin)
		}

		// m should be multiple of 64
		if m%64 != 0 {
			t.Errorf("m = %d should be multiple of 64", m)
		}

		// Estimate FP rate
		estimatedRate := EstimateFalsePositiveRate(tt.n, m, k)
		t.Logf("n=%d, p=%f -> m=%d, k=%d, estimated_rate=%.6f", tt.n, tt.p, m, k, estimatedRate)

		// Estimated rate should be close to target
		if estimatedRate > tt.p*2 {
			t.Errorf("Estimated FP rate %.6f too high for target %.6f", estimatedRate, tt.p)
		}
	}
}

// TestBloomFilter_Reset tests Clear/Reset functionality
func TestBloomFilter_Reset(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	// Add items
	items := []string{"a", "b", "c", "d", "e"}
	for _, item := range items {
		bf.AddString(item)
	}

	// Verify items present
	for _, item := range items {
		if !bf.ContainsString(item) {
			t.Errorf("'%s' should be present before Clear", item)
		}
	}

	// Clear
	bf.Clear()

	// Verify items not present
	for _, item := range items {
		if bf.ContainsString(item) {
			t.Errorf("'%s' should not be present after Clear", item)
		}
	}

	// Len should be 0
	if bf.Len() != 0 {
		t.Errorf("Len() = %d after Clear, want 0", bf.Len())
	}

	// Can add items again
	bf.AddString("new")
	if !bf.ContainsString("new") {
		t.Error("Should be able to add items after Clear")
	}
}

// TestBloomFilter_ConcurrentAccess tests concurrent read access
func TestBloomFilter_ConcurrentAccess(t *testing.T) {
	bf := NewBloomFilter(10000, 0.01)

	// Pre-populate the filter
	for i := 0; i < 5000; i++ {
		bf.AddString(fmt.Sprintf("item%d", i))
	}

	const numGoroutines = 50
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Only concurrent readers (Bloom filter is not thread-safe for concurrent writes)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				bf.ContainsString(fmt.Sprintf("item%d", j))
			}
		}(i)
	}

	wg.Wait()

	// Len should still be 5000
	if bf.Len() != 5000 {
		t.Errorf("Len() = %d, want 5000", bf.Len())
	}
}

// TestBloomFilter_DefaultValues tests default values when parameters are 0
func TestBloomFilter_DefaultValues(t *testing.T) {
	// n=0 should use default
	bf := NewBloomFilter(0, 0.01)
	if bf.Cap() == 0 {
		t.Error("Should use default capacity when n=0")
	}
	if bf.K() == 0 {
		t.Error("Should use default k when n=0")
	}

	// p=0 should use default
	bf2 := NewBloomFilter(1000, 0)
	if bf2.Cap() == 0 {
		t.Error("Should use default capacity when p=0")
	}

	// Both 0
	bf3 := NewBloomFilter(0, 0)
	if bf3.Cap() == 0 {
		t.Error("Should use default capacity when both are 0")
	}
	if bf3.K() == 0 {
		t.Error("Should use default k when both are 0")
	}
}

// TestBloomFilter_CapAndK tests Cap and K methods
func TestBloomFilter_CapAndK(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	cap := bf.Cap()
	k := bf.K()

	if cap == 0 {
		t.Error("Cap() should be > 0")
	}
	if k == 0 {
		t.Error("K() should be > 0")
	}

	// Cap should be multiple of 64
	if cap%64 != 0 {
		t.Errorf("Cap() = %d should be multiple of 64", cap)
	}

	// Add items and verify Cap and K don't change
	for i := 0; i < 100; i++ {
		bf.AddString(fmt.Sprintf("item%d", i))
	}

	if bf.Cap() != cap {
		t.Error("Cap() should not change after adding items")
	}
	if bf.K() != k {
		t.Error("K() should not change after adding items")
	}
}

func TestNewBloomFilter_EdgeCases(t *testing.T) {
	// Zero n should default to 1000
	bf := NewBloomFilter(0, 0.01)
	if bf == nil {
		t.Fatal("Expected non-nil bloom filter")
	}

	// p out of range should default to 0.01
	bf2 := NewBloomFilter(100, -0.5)
	if bf2 == nil {
		t.Fatal("Expected non-nil bloom filter for negative p")
	}

	bf3 := NewBloomFilter(100, 2.0)
	if bf3 == nil {
		t.Fatal("Expected non-nil bloom filter for p > 1")
	}
}

func TestOptimalBloomFilter_EdgeCases(t *testing.T) {
	// Small n
	m, k := OptimalBloomFilter(1, 0.01)
	if m == 0 {
		t.Error("m should be > 0")
	}
	if k == 0 {
		t.Error("k should be > 0")
	}

	// Very large k should be capped at 30
	_, k = OptimalBloomFilter(1000000, 0.00001)
	if k > 30 {
		t.Errorf("k = %d, should be capped at 30", k)
	}
}

func TestBloomFilter_FalsePositiveRateEmpty(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)
	fpr := bf.FalsePositiveRate()
	if fpr != 0 {
		t.Errorf("FPR of empty filter = %f, want 0", fpr)
	}
}
