package balancer

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestWeightedRoundRobin_Name(t *testing.T) {
	wrr := NewWeightedRoundRobin()
	if wrr.Name() != "weighted_round_robin" {
		t.Errorf("Name() = %v, want %v", wrr.Name(), "weighted_round_robin")
	}
}

func TestWeightedRoundRobin_Next_Empty(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	result := wrr.Next(nil, []*backend.Backend{})
	if result != nil {
		t.Error("Next() with empty backends should return nil")
	}
}

func TestWeightedRoundRobin_Next_Nil(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	result := wrr.Next(nil, nil)
	if result != nil {
		t.Error("Next() with nil backends should return nil")
	}
}

func TestWeightedRoundRobin_Next_SingleBackend(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	wrr.Add(b1)

	backends := []*backend.Backend{b1}

	// Should always return the same backend
	for i := 0; i < 10; i++ {
		result := wrr.Next(nil, backends)
		if result != b1 {
			t.Errorf("Next() = %v, want %v at iteration %d", result, b1, i)
		}
	}
}

func TestWeightedRoundRobin_WeightedDistribution(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	// Create backends with weights: 1, 2, 3
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)
	b1.SetState(backend.StateUp)

	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b2.SetWeight(2)
	b2.SetState(backend.StateUp)

	b3 := backend.NewBackend("b3", "127.0.0.1:8082")
	b3.SetWeight(3)
	b3.SetState(backend.StateUp)

	wrr.Add(b1)
	wrr.Add(b2)
	wrr.Add(b3)

	backends := []*backend.Backend{b1, b2, b3}

	// Collect distribution
	counts := map[string]int{
		"b1": 0,
		"b2": 0,
		"b3": 0,
	}

	numRequests := 6000
	for i := 0; i < numRequests; i++ {
		b := wrr.Next(nil, backends)
		if b != nil {
			counts[b.ID]++
		}
	}

	// Expected distribution: b1=1/6, b2=2/6, b3=3/6
	expected := map[string]int{
		"b1": numRequests / 6,
		"b2": numRequests * 2 / 6,
		"b3": numRequests * 3 / 6,
	}

	tolerance := numRequests / 100 // 1% tolerance

	for id, exp := range expected {
		got := counts[id]
		if got < exp-tolerance || got > exp+tolerance {
			t.Errorf("Backend %s count = %d, want ~%d", id, got, exp)
		}
	}

	t.Logf("Distribution: b1=%d (%.2f%%), b2=%d (%.2f%%), b3=%d (%.2f%%)",
		counts["b1"], float64(counts["b1"])*100/float64(numRequests),
		counts["b2"], float64(counts["b2"])*100/float64(numRequests),
		counts["b3"], float64(counts["b3"])*100/float64(numRequests))
}

func TestWeightedRoundRobin_SmoothDistribution(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	// Create backends with weights 5 and 1
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(5)
	b1.SetState(backend.StateUp)

	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b2.SetWeight(1)
	b2.SetState(backend.StateUp)

	wrr.Add(b1)
	wrr.Add(b2)

	backends := []*backend.Backend{b1, b2}

	// In smooth WRR, we should see b1, b1, b1, b1, b1, b2 pattern
	// rather than b1, b1, b1, b1, b1, b1, b2, b2, b2, b2, b2, b2

	sequence := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		b := wrr.Next(nil, backends)
		if b != nil {
			sequence = append(sequence, b.ID)
		}
	}

	// Count consecutive same backends
	maxConsecutive := 0
	currentConsecutive := 1
	for i := 1; i < len(sequence); i++ {
		if sequence[i] == sequence[i-1] {
			currentConsecutive++
		} else {
			if currentConsecutive > maxConsecutive {
				maxConsecutive = currentConsecutive
			}
			currentConsecutive = 1
		}
	}
	if currentConsecutive > maxConsecutive {
		maxConsecutive = currentConsecutive
	}

	// With smooth WRR, max consecutive should not be too high
	if maxConsecutive > 5 {
		t.Errorf("Max consecutive same backend = %d, want <= 5 (not smooth)", maxConsecutive)
	}

	t.Logf("Sequence: %v", sequence)
}

func TestWeightedRoundRobin_Add(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(10)

	wrr.Add(b1)

	backends := []*backend.Backend{b1}
	result := wrr.Next(nil, backends)

	if result != b1 {
		t.Error("Next() should return the added backend")
	}
}

func TestWeightedRoundRobin_Remove(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")

	wrr.Add(b1)
	wrr.Add(b2)

	wrr.Remove("b1")

	// After removing b1, Next should only see b2
	backends := []*backend.Backend{b2}
	result := wrr.Next(nil, backends)

	if result != b2 {
		t.Error("Next() should return b2 after b1 is removed")
	}
}

func TestWeightedRoundRobin_Update(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)

	wrr.Add(b1)

	// Update weight
	b1.SetWeight(10)
	wrr.Update(b1)

	// The weight should be updated
	backends := []*backend.Backend{b1}
	result := wrr.Next(nil, backends)

	if result != b1 {
		t.Error("Next() should return the updated backend")
	}
}

func TestWeightedRoundRobin_BackendNotInState(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)
	// Don't add to wrr state explicitly

	backends := []*backend.Backend{b1}

	// Should still work - backend will be added dynamically
	result := wrr.Next(nil, backends)
	if result != b1 {
		t.Error("Next() should return backend even if not pre-added")
	}
}

func TestWeightedRoundRobin_Concurrent(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b2.SetWeight(1)

	wrr.Add(b1)
	wrr.Add(b2)

	backends := []*backend.Backend{b1, b2}

	// Concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				wrr.Next(nil, backends)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestWeightedRoundRobin_Reset(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(5)

	wrr.Add(b1)

	backends := []*backend.Backend{b1}

	// Use the balancer
	wrr.Next(nil, backends)
	wrr.Next(nil, backends)

	// Reset
	wrr.Reset()

	// Should continue to work
	result := wrr.Next(nil, backends)
	if result != b1 {
		t.Error("Next() should work after Reset()")
	}
}

func BenchmarkWeightedRoundRobin_Next(b *testing.B) {
	wrr := NewWeightedRoundRobin()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	be1.SetWeight(1)
	be2 := backend.NewBackend("b2", "127.0.0.1:8081")
	be2.SetWeight(2)
	be3 := backend.NewBackend("b3", "127.0.0.1:8082")
	be3.SetWeight(3)

	wrr.Add(be1)
	wrr.Add(be2)
	wrr.Add(be3)

	backends := []*backend.Backend{be1, be2, be3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrr.Next(nil, backends)
	}
}

func TestWeightedRoundRobin_Next_DynamicBackend(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)

	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b2.SetWeight(2)

	// Do NOT call wrr.Add - backends will be added dynamically in Next
	backends := []*backend.Backend{b1, b2}

	counts := map[string]int{"b1": 0, "b2": 0}
	for i := 0; i < 90; i++ {
		result := wrr.Next(nil, backends)
		if result != nil {
			counts[result.ID]++
		}
	}

	// b2 should get roughly 2x the traffic of b1
	if counts["b2"] < counts["b1"] {
		t.Errorf("b2 (weight=2) should get more traffic than b1 (weight=1), got b1=%d b2=%d", counts["b1"], counts["b2"])
	}
	t.Logf("Dynamic backend distribution: b1=%d, b2=%d", counts["b1"], counts["b2"])
}

func TestWeightedRoundRobin_Next_ZeroWeight(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(0)

	backends := []*backend.Backend{b1}

	// Zero weight should still work (uses 0 weight from backend)
	result := wrr.Next(nil, backends)
	if result == nil {
		t.Error("Next() with zero weight should return a backend")
	}
}

func TestWeightedRoundRobin_Update_Nonexistent(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b := backend.NewBackend("nonexistent", "127.0.0.1:8080")
	b.SetWeight(5)

	// Update on nonexistent backend should not panic
	wrr.Update(b)
}

func TestWeightedRoundRobin_Remove_Nonexistent(t *testing.T) {
	wrr := NewWeightedRoundRobin()
	// Remove on empty should not panic
	wrr.Remove("nonexistent")
}

func TestWeightedRoundRobin_Add_Duplicate(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(5)

	wrr.Add(b1)
	wrr.Add(b1) // duplicate

	wrr.mu.RLock()
	count := len(wrr.backends)
	wrr.mu.RUnlock()
	if count != 1 {
		t.Errorf("Expected 1 backend after duplicate add, got %d", count)
	}
}

func TestWeightedRoundRobin_Next_NormalizesWeights(t *testing.T) {
	wrr := NewWeightedRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)
	wrr.Add(b1)

	backends := []*backend.Backend{b1}

	// Force the call count close to 1M to trigger normalization
	wrr.mu.Lock()
	wrr.callCount = 999_999
	wrr.mu.Unlock()

	// This call should trigger normalization
	result := wrr.Next(nil, backends)
	if result == nil {
		t.Fatal("Next() returned nil")
	}
	if result.ID != "b1" {
		t.Errorf("Next() = %s, want b1", result.ID)
	}

	// Verify callCount was reset
	wrr.mu.RLock()
	cc := wrr.callCount
	wrr.mu.RUnlock()
	if cc >= 1_000_000 {
		t.Errorf("callCount should be reset, got %d", cc)
	}
}

func BenchmarkWeightedRoundRobin_Next_SingleBackend(b *testing.B) {
	wrr := NewWeightedRoundRobin()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	be1.SetWeight(1)

	wrr.Add(be1)

	backends := []*backend.Backend{be1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrr.Next(nil, backends)
	}
}
