package balancer

import (
	"math"
	"sync"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

// TestRandomDistribution tests that Random balancer distributes requests uniformly.
func TestRandomDistribution(t *testing.T) {
	r := NewRandom()

	// Create 4 backends
	backends := []*backend.Backend{
		backend.NewBackend("backend-1", "10.0.0.1:8080"),
		backend.NewBackend("backend-2", "10.0.0.2:8080"),
		backend.NewBackend("backend-3", "10.0.0.3:8080"),
		backend.NewBackend("backend-4", "10.0.0.4:8080"),
	}

	// Run many iterations
	iterations := 10000
	counts := make(map[string]int)

	for i := 0; i < iterations; i++ {
		b := r.Next(nil, backends)
		if b == nil {
			t.Fatal("expected backend, got nil")
		}
		counts[b.ID]++
	}

	// Check that all backends were selected
	if len(counts) != 4 {
		t.Errorf("expected 4 backends to be selected, got %d", len(counts))
	}

	// Check distribution is roughly uniform (within 20% of expected)
	expected := iterations / 4
	tolerance := float64(expected) * 0.20

	for _, b := range backends {
		count := counts[b.ID]
		deviation := math.Abs(float64(count-expected)) / float64(expected) * 100
		t.Logf("Backend %s: %d selections (%.2f%% deviation from expected %d)",
			b.ID, count, deviation, expected)

		if float64(count) < float64(expected)-tolerance || float64(count) > float64(expected)+tolerance {
			t.Errorf("backend %s distribution uneven: got %d, expected ~%d (tolerance %.0f)",
				b.ID, count, expected, tolerance)
		}
	}
}

// TestRandomEmptyBackends tests that Random returns nil for empty backends.
func TestRandomEmptyBackends(t *testing.T) {
	r := NewRandom()

	result := r.Next(nil, []*backend.Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends, got %v", result)
	}

	result = r.Next(nil, nil)
	if result != nil {
		t.Errorf("expected nil for nil backends, got %v", result)
	}
}

// TestRandomName tests the Name method.
func TestRandomName(t *testing.T) {
	r := NewRandom()
	if r.Name() != "random" {
		t.Errorf("expected name 'random', got '%s'", r.Name())
	}
}

// TestRandomAdd tests adding backends.
func TestRandomAdd(t *testing.T) {
	r := NewRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	r.Add(b1)

	// Add same backend again (should not duplicate)
	r.Add(b1)

	backends := []*backend.Backend{b1}
	result := r.Next(nil, backends)
	if result == nil {
		t.Error("expected backend, got nil")
	}
}

// TestRandomRemove tests removing backends.
func TestRandomRemove(t *testing.T) {
	r := NewRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")

	r.Add(b1)
	r.Add(b2)

	r.Remove("backend-1")

	// Remove non-existent backend (should not panic)
	r.Remove("non-existent")
}

// TestRandomUpdate tests updating backends.
func TestRandomUpdate(t *testing.T) {
	r := NewRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)

	// Update is a no-op for Random, but should not panic
	r.Update(b1)

	// Also test with a backend that was previously added
	r.Add(b1)
	b1.SetWeight(10)
	r.Update(b1)

	// Balancer should still function after Update
	backends := []*backend.Backend{b1}
	result := r.Next(nil, backends)
	if result == nil {
		t.Fatal("Next() returned nil after Update")
	}
	if result.ID != "backend-1" {
		t.Errorf("Next() = %s, want backend-1", result.ID)
	}
}

// TestRandom_Update_NilBackend tests Update with nil backend.
func TestRandom_Update_NilBackend(t *testing.T) {
	r := &Random{}
	r.Update(nil) // no-op, should not panic
}

// TestRandomConcurrent tests concurrent access.
func TestRandomConcurrent(t *testing.T) {
	r := NewRandom()

	backends := []*backend.Backend{
		backend.NewBackend("backend-1", "10.0.0.1:8080"),
		backend.NewBackend("backend-2", "10.0.0.2:8080"),
		backend.NewBackend("backend-3", "10.0.0.3:8080"),
	}

	var wg sync.WaitGroup
	numGoroutines := 10
	iterationsPerGoroutine := 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				b := r.Next(nil, backends)
				if b == nil {
					t.Error("expected backend, got nil")
				}
			}
		}()
	}

	wg.Wait()
}

// TestWeightedRandomDistribution tests that WeightedRandom respects weights.
func TestWeightedRandomDistribution(t *testing.T) {
	wr := NewWeightedRandom()

	// Create backends with different weights
	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(1)

	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	b2.SetWeight(2)

	b3 := backend.NewBackend("backend-3", "10.0.0.3:8080")
	b3.SetWeight(3)

	backends := []*backend.Backend{b1, b2, b3}

	// Run many iterations
	iterations := 10000
	counts := make(map[string]int)

	for i := 0; i < iterations; i++ {
		b := wr.Next(nil, backends)
		if b == nil {
			t.Fatal("expected backend, got nil")
		}
		counts[b.ID]++
	}

	// Check that all backends were selected
	if len(counts) != 3 {
		t.Errorf("expected 3 backends to be selected, got %d", len(counts))
	}

	// Total weight = 6
	// backend-1: 1/6 = 16.67%
	// backend-2: 2/6 = 33.33%
	// backend-3: 3/6 = 50%

	totalWeight := int32(6)
	tolerance := 0.05 // 5% tolerance

	for _, b := range backends {
		count := counts[b.ID]
		expectedRatio := float64(b.GetWeight()) / float64(totalWeight)
		actualRatio := float64(count) / float64(iterations)
		deviation := math.Abs(actualRatio - expectedRatio)

		t.Logf("Backend %s (weight %d): %d selections (%.2f%% vs expected %.2f%%)",
			b.ID, b.GetWeight(), count, actualRatio*100, expectedRatio*100)

		if deviation > tolerance {
			t.Errorf("backend %s distribution off: got %.2f%%, expected %.2f%% (deviation %.2f%%)",
				b.ID, actualRatio*100, expectedRatio*100, deviation*100)
		}
	}
}

// TestWeightedRandomEmptyBackends tests that WeightedRandom returns nil for empty backends.
func TestWeightedRandomEmptyBackends(t *testing.T) {
	wr := NewWeightedRandom()

	result := wr.Next(nil, []*backend.Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends, got %v", result)
	}

	result = wr.Next(nil, nil)
	if result != nil {
		t.Errorf("expected nil for nil backends, got %v", result)
	}
}

// TestWeightedRandomName tests the Name method.
func TestWeightedRandomName(t *testing.T) {
	wr := NewWeightedRandom()
	if wr.Name() != "weighted_random" {
		t.Errorf("expected name 'weighted_random', got '%s'", wr.Name())
	}
}

// TestWeightedRandomAdd tests adding backends.
func TestWeightedRandomAdd(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)

	wr.Add(b1)

	// Add same backend again (should not duplicate)
	wr.Add(b1)

	backends := []*backend.Backend{b1}
	result := wr.Next(nil, backends)
	if result == nil {
		t.Error("expected backend, got nil")
	}
}

// TestWeightedRandomRemove tests removing backends.
func TestWeightedRandomRemove(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)

	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	b2.SetWeight(3)

	wr.Add(b1)
	wr.Add(b2)

	wr.Remove("backend-1")

	// Remove non-existent backend (should not panic)
	wr.Remove("non-existent")
}

// TestWeightedRandomUpdate tests updating backend weights.
func TestWeightedRandomUpdate(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)

	wr.Add(b1)

	// Update weight
	b1.SetWeight(10)
	wr.Update(b1)

	// Update non-existent backend (should not panic)
	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	wr.Update(b2)
}

// TestWeightedRandomZeroWeight tests handling of zero/negative weights.
func TestWeightedRandomZeroWeight(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(0) // Zero weight

	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	b2.SetWeight(-1) // Negative weight

	backends := []*backend.Backend{b1, b2}

	// Should still work (treats as weight 1)
	for i := 0; i < 100; i++ {
		result := wr.Next(nil, backends)
		if result == nil {
			t.Error("expected backend, got nil")
		}
	}
}

// TestWeightedRandomConcurrent tests concurrent access.
func TestWeightedRandomConcurrent(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(1)
	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	b2.SetWeight(2)
	b3 := backend.NewBackend("backend-3", "10.0.0.3:8080")
	b3.SetWeight(3)

	backends := []*backend.Backend{b1, b2, b3}

	var wg sync.WaitGroup
	numGoroutines := 10
	iterationsPerGoroutine := 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				b := wr.Next(nil, backends)
				if b == nil {
					t.Error("expected backend, got nil")
				}
			}
		}()
	}

	wg.Wait()
}

// TestWeightedRandomSingleBackend tests with a single backend.
func TestWeightedRandomSingleBackend(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)

	backends := []*backend.Backend{b1}

	for i := 0; i < 100; i++ {
		result := wr.Next(nil, backends)
		if result == nil {
			t.Fatal("expected backend, got nil")
		}
		if result.ID != "backend-1" {
			t.Errorf("expected backend-1, got %s", result.ID)
		}
	}
}

// BenchmarkRandom benchmarks the Random balancer.
func BenchmarkRandom(b *testing.B) {
	r := NewRandom()

	backends := []*backend.Backend{
		backend.NewBackend("backend-1", "10.0.0.1:8080"),
		backend.NewBackend("backend-2", "10.0.0.2:8080"),
		backend.NewBackend("backend-3", "10.0.0.3:8080"),
		backend.NewBackend("backend-4", "10.0.0.4:8080"),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.Next(nil, backends)
		}
	})
}

// BenchmarkWeightedRandom benchmarks the WeightedRandom balancer.
func BenchmarkWeightedRandom(b *testing.B) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(1)
	b2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	b2.SetWeight(2)
	b3 := backend.NewBackend("backend-3", "10.0.0.3:8080")
	b3.SetWeight(3)
	b4 := backend.NewBackend("backend-4", "10.0.0.4:8080")
	b4.SetWeight(4)

	backends := []*backend.Backend{b1, b2, b3, b4}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wr.Next(nil, backends)
		}
	})
}

// BenchmarkRandomManyBackends benchmarks Random with many backends.
func BenchmarkRandomManyBackends(b *testing.B) {
	r := NewRandom()

	backends := make([]*backend.Backend, 100)
	for i := 0; i < 100; i++ {
		backends[i] = backend.NewBackend(
			string(rune('a'+i%26))+string(rune('0'+i/26)),
			"10.0.0.1:8080",
		)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.Next(nil, backends)
		}
	})
}

func TestRandom_Add_Duplicate(t *testing.T) {
	r := NewRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	r.Add(b1)
	r.Add(b1) // duplicate

	r.mu.RLock()
	count := len(r.backends)
	r.mu.RUnlock()
	if count != 1 {
		t.Errorf("Expected 1 backend after duplicate add, got %d", count)
	}
}

func TestRandom_Remove_NotFound(t *testing.T) {
	r := NewRandom()
	r.Remove("nonexistent") // should not panic
}

func TestRandom_Remove_SingleBackend(t *testing.T) {
	r := NewRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	r.Add(b1)
	r.Remove("backend-1")

	r.mu.RLock()
	count := len(r.backends)
	r.mu.RUnlock()
	if count != 0 {
		t.Errorf("Expected 0 backends after remove, got %d", count)
	}
}

func TestWeightedRandom_Update_Nonexistent(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)

	// Update should not panic on unknown backend
	wr.Update(b1)
}

func TestWeightedRandom_Add_ZeroWeight(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(0)

	wr.Add(b1)

	// Backend should be added with effective weight of 1
	wr.mu.RLock()
	if len(wr.backends) != 1 {
		t.Errorf("Expected 1 backend, got %d", len(wr.backends))
	}
	if wr.backends[0].weight != 1 {
		t.Errorf("Expected weight 1 for zero-weight backend, got %d", wr.backends[0].weight)
	}
	wr.mu.RUnlock()
}

func TestWeightedRandom_Remove_SingleBackend(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)
	wr.Add(b1)
	wr.Remove("backend-1")

	wr.mu.RLock()
	if len(wr.backends) != 0 {
		t.Errorf("Expected 0 backends after remove, got %d", len(wr.backends))
	}
	wr.mu.RUnlock()
}

func TestWeightedRandom_Add_Duplicate(t *testing.T) {
	wr := NewWeightedRandom()

	b1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	b1.SetWeight(5)
	wr.Add(b1)
	wr.Add(b1) // duplicate

	wr.mu.RLock()
	count := len(wr.backends)
	total := wr.total
	wr.mu.RUnlock()
	if count != 1 {
		t.Errorf("Expected 1 backend after duplicate add, got %d", count)
	}
	if total != 5 {
		t.Errorf("Expected total 5 after duplicate add, got %d", total)
	}
}

// BenchmarkWeightedRandomManyBackends benchmarks WeightedRandom with many backends.
func BenchmarkWeightedRandomManyBackends(b *testing.B) {
	wr := NewWeightedRandom()

	backends := make([]*backend.Backend, 100)
	for i := 0; i < 100; i++ {
		backends[i] = backend.NewBackend(
			string(rune('a'+i%26))+string(rune('0'+i/26)),
			"10.0.0.1:8080",
		)
		backends[i].SetWeight(int32(i%10 + 1))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wr.Next(nil, backends)
		}
	})
}
