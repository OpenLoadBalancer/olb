package balancer

import (
	"fmt"
	"hash/fnv"
	"sync"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestRingHash_Name(t *testing.T) {
	rh := NewRingHash()
	if got := rh.Name(); got != "ring_hash" {
		t.Errorf("Name() = %v, want %v", got, "ring_hash")
	}
}

func TestRingHash_Next_EmptyBackends(t *testing.T) {
	rh := NewRingHash()
	if got := rh.Next(nil, []*backend.Backend{}); got != nil {
		t.Errorf("Next() with empty backends = %v, want nil", got)
	}
}

func TestRingHash_DistributesAcrossBackends(t *testing.T) {
	rh := NewRingHash()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)
	be3 := backend.NewBackend("backend-3", "10.0.0.3:8080")
	be3.SetState(backend.StateUp)

	rh.Add(be1)
	rh.Add(be2)
	rh.Add(be3)

	backends := []*backend.Backend{be1, be2, be3}

	// Should distribute across all backends
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		got := rh.Next(nil, backends)
		if got == nil {
			t.Fatal("Next() returned nil for valid backends")
		}
		seen[got.ID] = true
	}

	// Should have seen all backends
	if len(seen) != 3 {
		t.Errorf("Expected to see all 3 backends, saw %d", len(seen))
	}
}

func TestRingHash_Distribution(t *testing.T) {
	rh := NewRingHash()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)
	be3 := backend.NewBackend("backend-3", "10.0.0.3:8080")
	be3.SetState(backend.StateUp)

	rh.Add(be1)
	rh.Add(be2)
	rh.Add(be3)

	backends := []*backend.Backend{be1, be2, be3}

	// Generate many requests and check distribution
	counts := make(map[string]int)
	numRequests := 10000

	for i := 0; i < numRequests; i++ {
		b := rh.Next(nil, backends)
		if b == nil {
			t.Fatal("Next() returned nil")
		}
		counts[b.ID]++
	}

	// Each backend should get some traffic (rough distribution check)
	for id, count := range counts {
		percentage := float64(count) / float64(numRequests) * 100
		if percentage < 10 || percentage > 60 {
			t.Errorf("Backend %s has %.1f%% distribution, expected roughly 33%%", id, percentage)
		}
	}
}

func TestRingHash_BackendAdded(t *testing.T) {
	rh := NewRingHash()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)
	be3 := backend.NewBackend("backend-3", "10.0.0.3:8080")
	be3.SetState(backend.StateUp)

	rh.Add(be1)
	rh.Add(be2)
	rh.Add(be3)

	backends := []*backend.Backend{be1, be2, be3}

	// Count requests to each backend
	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		b := rh.Next(nil, backends)
		counts[b.ID]++
	}

	// Add a new backend
	be4 := backend.NewBackend("backend-4", "10.0.0.4:8080")
	be4.SetState(backend.StateUp)
	rh.Add(be4)
	backends = append(backends, be4)

	// Verify new backend gets traffic
	newCounts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		b := rh.Next(nil, backends)
		newCounts[b.ID]++
	}

	// New backend should have received some traffic
	if newCounts["backend-4"] == 0 {
		t.Error("New backend should receive traffic after being added")
	}

	// All backends should have traffic
	if len(newCounts) != 4 {
		t.Errorf("Expected 4 backends to have traffic, got %d", len(newCounts))
	}
}

func TestRingHash_Add(t *testing.T) {
	rh := NewRingHash()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	rh.Add(be)

	if len(rh.backends) != 1 {
		t.Errorf("Expected 1 backend, got %d", len(rh.backends))
	}

	if len(rh.ring) == 0 {
		t.Error("Ring should have virtual nodes after Add")
	}
}

func TestRingHash_Remove(t *testing.T) {
	rh := NewRingHash()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	rh.Add(be1)
	rh.Add(be2)

	originalRingSize := len(rh.ring)
	rh.Remove("backend-1")

	if len(rh.backends) != 1 {
		t.Errorf("Expected 1 backend after removal, got %d", len(rh.backends))
	}

	if len(rh.ring) >= originalRingSize {
		t.Errorf("Ring size should decrease after removal: was %d, now %d", originalRingSize, len(rh.ring))
	}

	if _, exists := rh.backends["backend-1"]; exists {
		t.Error("backend-1 should not exist after removal")
	}
}

func TestRingHash_Update(t *testing.T) {
	rh := NewRingHash()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetWeight(1)
	rh.Add(be)

	originalRingSize := len(rh.ring)

	updated := backend.NewBackend("backend-1", "10.0.0.1:8080")
	updated.SetWeight(2)
	rh.Update(updated)

	// Weight change should trigger ring rebuild
	if len(rh.ring) == originalRingSize {
		t.Error("Ring should be rebuilt when weight changes")
	}
}

func TestRingHash_UpdateSameWeight(t *testing.T) {
	rh := NewRingHash()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetWeight(1)
	rh.Add(be)

	originalRingSize := len(rh.ring)

	updated := backend.NewBackend("backend-1", "10.0.0.1:8080")
	updated.SetWeight(1)
	rh.Update(updated)

	// Same weight should not trigger ring rebuild
	if len(rh.ring) != originalRingSize {
		t.Error("Ring should not be rebuilt when weight stays the same")
	}
}

func TestRingHash_ConcurrentAccess(t *testing.T) {
	rh := NewRingHash()

	// Create backends
	backends := make([]*backend.Backend, 5)
	for i := 0; i < 5; i++ {
		be := backend.NewBackend(fmt.Sprintf("backend-%d", i), "10.0.0.1:8080")
		be.SetState(backend.StateUp)
		backends[i] = be
		rh.Add(be)
	}

	var wg sync.WaitGroup

	// Concurrent Next() calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rh.Next(nil, backends)
		}()
	}

	// Concurrent Add/Remove
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("backend-%d", i%5)
			rh.Remove(id)
			be := backend.NewBackend(id, "10.0.0.1:8080")
			be.SetState(backend.StateUp)
			rh.Add(be)
		}(i)
	}

	wg.Wait()
}

func TestRingHash_CustomHashFunc(t *testing.T) {
	customHash := func(data []byte) uint32 {
		h := fnv.New32a()
		h.Write(data)
		return h.Sum32()
	}

	rh := NewRingHashWithConfig(100, customHash)

	if rh.hashFunc == nil {
		t.Error("Custom hash function should be set")
	}

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)
	rh.Add(be)

	backends := []*backend.Backend{be}
	got := rh.Next(nil, backends)
	if got == nil {
		t.Error("Next() should work with custom hash function")
	}
}

func TestRingHash_NewRingHashWithConfig_ZeroVnodes(t *testing.T) {
	rh := NewRingHashWithConfig(0, nil)
	if rh.vnodes != DefaultRingVirtualNodes {
		t.Errorf("vnodes = %d, want %d", rh.vnodes, DefaultRingVirtualNodes)
	}
	if rh.hashFunc == nil {
		t.Error("hashFunc should not be nil (uses default)")
	}
}

func TestRingHash_NewRingHashWithConfig_NilHashFunc(t *testing.T) {
	rh := NewRingHashWithConfig(50, nil)
	if rh.hashFunc == nil {
		t.Error("hashFunc should be set to default when nil is passed")
	}
}

func TestRingHash_Update_Nonexistent(t *testing.T) {
	rh := NewRingHash()

	b := backend.NewBackend("nonexistent", "10.0.0.1:8080")
	b.SetWeight(5)

	// Should not panic when updating a backend not in the ring
	rh.Update(b)

	if len(rh.backends) != 0 {
		t.Errorf("Expected 0 backends, got %d", len(rh.backends))
	}
}

func TestRingHash_Add_Duplicate(t *testing.T) {
	rh := NewRingHash()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	rh.Add(be)

	originalRingSize := len(rh.ring)
	rh.Add(be) // duplicate

	if len(rh.backends) != 1 {
		t.Errorf("Expected 1 backend after duplicate add, got %d", len(rh.backends))
	}
	if len(rh.ring) != originalRingSize {
		t.Error("Ring should not change on duplicate add")
	}
}

func TestRingHash_Remove_Nonexistent(t *testing.T) {
	rh := NewRingHash()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	rh.Add(be)

	originalRingSize := len(rh.ring)
	rh.Remove("nonexistent")

	if len(rh.backends) != 1 {
		t.Errorf("Expected 1 backend, got %d", len(rh.backends))
	}
	if len(rh.ring) != originalRingSize {
		t.Error("Ring should not change when removing nonexistent")
	}
}

func TestRingHash_FindNextAvailable_EmptyRing(t *testing.T) {
	rh := NewRingHash()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	backends := []*backend.Backend{be}

	// Empty ring should return nil
	result := rh.findNextAvailable(0, backends)
	if result != nil {
		t.Errorf("findNextAvailable on empty ring = %v, want nil", result.ID)
	}
}

func TestRingHash_FindNextAvailable_AllUnavailable(t *testing.T) {
	rh := NewRingHash()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetState(backend.StateDown)
	rh.Add(be1)

	// All provided backends are down
	backends := []*backend.Backend{be1}
	result := rh.findNextAvailable(0, backends)
	if result != nil {
		t.Errorf("findNextAvailable with all unavailable = %v, want nil", result.ID)
	}
}

func TestIntToStr_Negative(t *testing.T) {
	got := intToStr(-42)
	if got != "-42" {
		t.Errorf("intToStr(-42) = %q, want %q", got, "-42")
	}
}

func TestIntToStr_Zero(t *testing.T) {
	got := intToStr(0)
	if got != "0" {
		t.Errorf("intToStr(0) = %q, want %q", got, "0")
	}
}

func TestRingHash_WeightedBackends(t *testing.T) {
	rh := NewRingHashWithConfig(10, nil)

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetWeight(3)
	be1.SetState(backend.StateUp)

	rh.Add(be1)

	// With weight=3 and 10 vnodes, should have 30 ring nodes
	if len(rh.ring) != 30 {
		t.Errorf("Expected 30 ring nodes (10*3), got %d", len(rh.ring))
	}
}

func TestRingHash_FindNextAvailable(t *testing.T) {
	rh := NewRingHash()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)

	rh.Add(be1)
	rh.Add(be2)

	// Test with all backends available
	backends := []*backend.Backend{be1, be2}
	got := rh.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil when backends available")
	}

	// Test with one unavailable backend
	be1.SetState(backend.StateDown)
	for i := 0; i < 10; i++ {
		got = rh.Next(nil, backends)
		if got == nil {
			t.Fatal("Next() returned nil, should find available backend")
		}
		if got.ID != "backend-2" {
			t.Errorf("Expected backend-2, got %s", got.ID)
		}
	}
}

func BenchmarkRingHash_Next(b *testing.B) {
	rh := NewRingHash()

	// Add backends
	backends := make([]*backend.Backend, 10)
	for i := 0; i < 10; i++ {
		be := backend.NewBackend(fmt.Sprintf("backend-%d", i), "10.0.0.1:8080")
		be.SetState(backend.StateUp)
		backends[i] = be
		rh.Add(be)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rh.Next(nil, backends)
	}
}
