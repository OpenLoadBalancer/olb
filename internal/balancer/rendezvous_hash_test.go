package balancer

import (
	"sync/atomic"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestRendezvousHash_Next(t *testing.T) {
	rh := NewRendezvousHash()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
		backend.NewBackend("be3", "10.0.0.3:8080"),
	}

	// Set backends to healthy
	for _, be := range backends {
		be.SetState(backend.StateUp)
		rh.Add(be)
	}

	// Test selection
	selections := make(map[string]int)
	for i := 0; i < 1000; i++ {
		be := rh.Next(nil, backends)
		if be != nil {
			selections[be.ID]++
		}
	}

	// All backends should be selected
	if len(selections) != 3 {
		t.Errorf("Expected 3 backends selected, got %d", len(selections))
	}

	// Distribution should be relatively uniform
	for id, count := range selections {
		// Each backend should get roughly 33% of traffic
		// Allow 10% tolerance
		ratio := float64(count) / 1000.0
		if ratio < 0.20 || ratio > 0.50 {
			t.Errorf("Backend %s has uneven distribution: %.2f%% (%d/1000)", id, ratio*100, count)
		}
	}
}

func TestRendezvousHash_Empty(t *testing.T) {
	rh := NewRendezvousHash()

	be := rh.Next(nil, []*backend.Backend{})
	if be != nil {
		t.Error("Expected nil for empty backends")
	}
}

func TestRendezvousHash_AddRemove(t *testing.T) {
	rh := NewRendezvousHash()

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)

	// Add backend
	rh.Add(be1)

	stats := rh.Stats()
	if stats["backend_count"].(int) != 1 {
		t.Errorf("Expected 1 backend, got %d", stats["backend_count"])
	}

	// Remove backend
	rh.Remove("be1")

	stats = rh.Stats()
	if stats["backend_count"].(int) != 0 {
		t.Errorf("Expected 0 backends, got %d", stats["backend_count"])
	}
}

func TestRendezvousHash_Unhealthy(t *testing.T) {
	rh := NewRendezvousHash()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
	}

	// Set be1 to down
	backends[0].SetState(backend.StateDown)
	backends[1].SetState(backend.StateUp)

	rh.Add(backends[0])
	rh.Add(backends[1])

	// Should always select be2
	for i := 0; i < 10; i++ {
		be := rh.Next(nil, backends)
		if be != nil && be.ID != "be2" {
			t.Errorf("Expected be2 (healthy), got %s", be.ID)
		}
	}
}

func TestRendezvousHash_Consistency(t *testing.T) {
	rh := NewRendezvousHash()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
	}

	for _, be := range backends {
		be.SetState(backend.StateUp)
		rh.Add(be)
	}

	// Reset key counter for consistent test
	keyCounter = 0

	// Get selections
	var selections []string
	for i := 0; i < 10; i++ {
		be := rh.Next(nil, backends)
		if be != nil {
			selections = append(selections, be.ID)
		}
	}

	// Reset and get again
	keyCounter = 0
	var selections2 []string
	for i := 0; i < 10; i++ {
		be := rh.Next(nil, backends)
		if be != nil {
			selections2 = append(selections2, be.ID)
		}
	}

	// Should be identical
	for i := range selections {
		if selections[i] != selections2[i] {
			t.Errorf("Inconsistent selection at index %d: %s vs %s", i, selections[i], selections2[i])
		}
	}
}

func TestRendezvousHash_MinimalDisruption(t *testing.T) {
	rh := NewRendezvousHash()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
		backend.NewBackend("be3", "10.0.0.3:8080"),
	}

	for _, be := range backends {
		be.SetState(backend.StateUp)
		rh.Add(be)
	}

	// Reset key counter
	keyCounter = 0

	// Get initial selections
	initialSelections := make(map[int]string)
	for i := 0; i < 100; i++ {
		be := rh.Next(nil, backends)
		if be != nil {
			initialSelections[i] = be.ID
		}
	}

	// Add new backend
	be4 := backend.NewBackend("be4", "10.0.0.4:8080")
	be4.SetState(backend.StateUp)
	rh.Add(be4)

	// Reset counter and check how many changed
	keyCounter = 0
	changed := 0
	for i := 0; i < 100; i++ {
		be := rh.Next(nil, backends)
		if be != nil {
			if initialSelections[i] != be.ID {
				changed++
			}
		}
	}

	// Should have minimal disruption (ideally ~25% for 4 backends)
	// Allow up to 50% disruption
	changeRate := float64(changed) / 100.0
	if changeRate > 0.50 {
		t.Errorf("Too much disruption: %.2f%% of requests reassigned", changeRate*100)
	}
}

func TestHashString(t *testing.T) {
	// Test that hashString produces consistent results
	h1 := hashString("test")
	h2 := hashString("test")
	if h1 != h2 {
		t.Error("hashString should be deterministic")
	}

	// Different inputs should produce different outputs
	h3 := hashString("different")
	if h1 == h3 {
		t.Error("Different strings should have different hashes")
	}
}

func TestComputeWeight(t *testing.T) {
	rh := NewRendezvousHash()

	// Same key-backend pair should produce same weight
	w1 := rh.computeWeight("key1", "be1")
	w2 := rh.computeWeight("key1", "be1")
	if w1 != w2 {
		t.Error("computeWeight should be deterministic")
	}

	// Different backends should generally produce different weights
	w3 := rh.computeWeight("key1", "be2")
	if w1 == w3 {
		t.Error("Different backends should generally have different weights")
	}
}

func TestRendezvousHash_Name(t *testing.T) {
	rh := NewRendezvousHash()
	if name := rh.Name(); name != "rendezvous_hash" {
		t.Errorf("Name() = %q, want %q", name, "rendezvous_hash")
	}
}

func TestRendezvousHash_Update(t *testing.T) {
	rh := NewRendezvousHash()
	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)

	// Update is a no-op but should not panic
	rh.Update(be1)

	// Update after Add
	rh.Add(be1)
	rh.Update(be1)

	// Update with nil should not panic
	rh.Update(nil)

	// Verify balancer still works after Update
	backends := []*backend.Backend{be1}
	result := rh.Next(nil, backends)
	if result == nil {
		t.Error("Next() returned nil after Update")
	}
}

func TestRendezvousHash_Remove_Middle(t *testing.T) {
	rh := NewRendezvousHash()

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be2 := backend.NewBackend("be2", "10.0.0.2:8080")
	be3 := backend.NewBackend("be3", "10.0.0.3:8080")

	for _, be := range []*backend.Backend{be1, be2, be3} {
		be.SetState(backend.StateUp)
		rh.Add(be)
	}

	// Remove a backend in the middle to exercise the swap logic
	rh.Remove("be2")

	stats := rh.Stats()
	if stats["backend_count"].(int) != 2 {
		t.Errorf("Expected 2 backends after remove, got %d", stats["backend_count"])
	}
	if stats["seed_count"].(int) != 2 {
		t.Errorf("Expected 2 seeds after remove, got %d", stats["seed_count"])
	}

	// Remove last backend
	rh.Remove("be3")

	stats = rh.Stats()
	if stats["backend_count"].(int) != 1 {
		t.Errorf("Expected 1 backend after second remove, got %d", stats["backend_count"])
	}

	// Remove non-existent backend (should not panic)
	rh.Remove("nonexistent")

	// Verify balancer still works
	be1.SetState(backend.StateUp)
	result := rh.Next(nil, []*backend.Backend{be1})
	if result == nil {
		t.Error("Next() returned nil after removes")
	}
}

func TestRendezvousHash_Remove_LastElement(t *testing.T) {
	rh := NewRendezvousHash()

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be2 := backend.NewBackend("be2", "10.0.0.2:8080")

	rh.Add(be1)
	rh.Add(be2)

	// Remove the last element (idx == lastIdx case)
	rh.Remove("be2")

	stats := rh.Stats()
	if stats["backend_count"].(int) != 1 {
		t.Errorf("Expected 1 backend after remove, got %d", stats["backend_count"])
	}
}

func TestRendezvousHash_Remove_FirstOfThree(t *testing.T) {
	rh := NewRendezvousHash()

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be2 := backend.NewBackend("be2", "10.0.0.2:8080")
	be3 := backend.NewBackend("be3", "10.0.0.3:8080")

	rh.Add(be1)
	rh.Add(be2)
	rh.Add(be3)

	// Remove first backend to exercise swap-with-last logic
	rh.Remove("be1")

	stats := rh.Stats()
	if stats["backend_count"].(int) != 2 {
		t.Errorf("Expected 2 backends, got %d", stats["backend_count"])
	}
	if stats["seed_count"].(int) != 2 {
		t.Errorf("Expected 2 seeds, got %d", stats["seed_count"])
	}

	// Ensure the remaining backends are correct
	_, hasBe2 := rh.backends["be2"]
	_, hasBe3 := rh.backends["be3"]
	if !hasBe2 || !hasBe3 {
		t.Error("Expected be2 and be3 to remain after removing be1")
	}
}

func TestRendezvousHash_Remove_Nonexistent(t *testing.T) {
	rh := NewRendezvousHash()

	// Remove on empty should not panic
	rh.Remove("nonexistent")

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	rh.Add(be1)

	// Remove nonexistent when there are backends
	rh.Remove("nonexistent")

	stats := rh.Stats()
	if stats["backend_count"].(int) != 1 {
		t.Errorf("Expected 1 backend after removing nonexistent, got %d", stats["backend_count"])
	}
}

func TestRendezvousHash_Add_Duplicate(t *testing.T) {
	rh := NewRendezvousHash()

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	rh.Add(be1)
	rh.Add(be1) // duplicate

	stats := rh.Stats()
	if stats["backend_count"].(int) != 1 {
		t.Errorf("Expected 1 backend after duplicate add, got %d", stats["backend_count"])
	}
	if stats["seed_count"].(int) != 1 {
		t.Errorf("Expected 1 seed after duplicate add, got %d", stats["seed_count"])
	}
}

func TestRendezvousHash_Next_AllUnhealthy(t *testing.T) {
	rh := NewRendezvousHash()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
	}

	// Both down
	backends[0].SetState(backend.StateDown)
	backends[1].SetState(backend.StateDown)

	rh.Add(backends[0])
	rh.Add(backends[1])

	// Should fall back to first backend
	result := rh.Next(nil, backends)
	if result == nil {
		t.Fatal("Next() with all unhealthy should fall back to first")
	}
	if result.ID != "be1" {
		t.Errorf("Next() with all unhealthy = %s, want be1 (fallback)", result.ID)
	}
}

func BenchmarkRendezvousHash_Next(b *testing.B) {
	rh := NewRendezvousHash()
	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
		backend.NewBackend("be3", "10.0.0.3:8080"),
	}

	for _, be := range backends {
		be.SetState(backend.StateUp)
		rh.Add(be)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rh.Next(nil, backends)
		}
	})
}

func BenchmarkRendezvousHash_ComputeWeight(b *testing.B) {
	rh := NewRendezvousHash()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rh.computeWeight("key", "backend")
	}
}

func BenchmarkRendezvousHash_WithRealisticBackends(b *testing.B) {
	rh := NewRendezvousHash()

	// Simulate a more realistic scenario with 10 backends
	backends := make([]*backend.Backend, 10)
	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		backends[i] = backend.NewBackend(id, "10.0.0."+string(rune('0'+i))+":8080")
		backends[i].SetState(backend.StateUp)
		rh.Add(backends[i])
	}

	var counter atomic.Uint32

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Vary the key for each request
			counter.Add(1)
			rh.Next(nil, backends)
		}
	})
}

// TestRendezvousHash_NoKeyCollapse pins F017: context-less requests (nil ctx,
// as used by SSE/WS/TCP/UDP paths) must keep distributing evenly across
// backends — the counter key must never collapse and the score hash must
// avalanche (raw FNV-1a barely changes high bits when only trailing bytes
// differ, biasing selection toward one backend).
func TestRendezvousHash_NoKeyCollapse(t *testing.T) {
	rh := NewRendezvousHash()
	ids := []string{"a", "b", "c"}
	var backends []*backend.Backend
	for _, id := range ids {
		be := backend.NewBackend(id, "127.0.0.1:900"+id)
		be.SetState(backend.StateUp) // NewBackend starts in StateStarting
		rh.Add(be)
		backends = append(backends, be)
	}

	const total = 60000
	counts := make(map[string]int, len(ids))
	for i := 0; i < total; i++ {
		be := rh.Next(nil, backends)
		if be == nil {
			t.Fatal("Next returned nil with healthy backends")
		}
		counts[be.ID]++
	}
	for _, id := range ids {
		share := float64(counts[id]) / float64(total)
		if share > 0.40 {
			t.Errorf("backend %q received %.1f%% of context-less requests; distribution collapsed", id, share*100)
		}
		if share < 0.10 {
			t.Errorf("backend %q received only %.1f%% of context-less requests", id, share*100)
		}
	}

	// Key uniqueness across the surrogate/out-of-range rune range: decimal
	// counter keys must never repeat (rune conversion would collapse
	// 0xD800-0xDFFF and >=0x110000 onto U+FFFD).
	seen := make(map[string]struct{}, 70001)
	for i := 0; i <= 70000; i++ {
		k := generateKey()
		if _, dup := seen[k]; dup {
			t.Fatalf("generateKey produced duplicate key %q within %d calls", k, len(seen))
		}
		seen[k] = struct{}{}
	}
}
