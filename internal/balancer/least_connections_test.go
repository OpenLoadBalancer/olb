package balancer

import (
	"sync"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

// mockBackendWithConns creates a backend with a specific active connection count
func mockBackendWithConns(id string, conns int64) *backend.Backend {
	b := backend.NewBackend(id, "127.0.0.1:8080")
	// Simulate active connections by acquiring them
	for i := int64(0); i < conns; i++ {
		b.AcquireConn()
	}
	return b
}

// mockWeightedBackend creates a backend with specific weight and connection count
func mockWeightedBackend(id string, weight int32, conns int64) *backend.Backend {
	b := backend.NewBackend(id, "127.0.0.1:8080")
	b.SetWeight(weight)
	for i := int64(0); i < conns; i++ {
		b.AcquireConn()
	}
	return b
}

func TestLeastConnections_Name(t *testing.T) {
	lc := NewLeastConnections()
	if lc.Name() != "least_connections" {
		t.Errorf("expected name 'least_connections', got '%s'", lc.Name())
	}
}

func TestLeastConnections_Next_Empty(t *testing.T) {
	lc := NewLeastConnections()
	result := lc.Next(nil, nil)
	if result != nil {
		t.Error("expected nil for empty backend list")
	}

	result = lc.Next(nil, []*backend.Backend{})
	if result != nil {
		t.Error("expected nil for empty backend slice")
	}
}

func TestLeastConnections_Next_SingleBackend(t *testing.T) {
	lc := NewLeastConnections()
	b := mockBackendWithConns("backend1", 0)

	result := lc.Next(nil, []*backend.Backend{b})
	if result != b {
		t.Error("expected single backend to be selected")
	}
}

func TestLeastConnections_Next_SelectsLeastConns(t *testing.T) {
	lc := NewLeastConnections()

	b1 := mockBackendWithConns("backend1", 5)
	b2 := mockBackendWithConns("backend2", 2)
	b3 := mockBackendWithConns("backend3", 10)

	backends := []*backend.Backend{b1, b2, b3}

	// Should select backend2 with 2 connections
	result := lc.Next(nil, backends)
	if result != b2 {
		t.Errorf("expected backend2 (2 conns), got %s (%d conns)", result.ID, result.ActiveConns())
	}
}

func TestLeastConnections_Next_TieBreaking(t *testing.T) {
	lc := NewLeastConnections()

	// Create backends with same connection count
	b1 := mockBackendWithConns("backend1", 5)
	b2 := mockBackendWithConns("backend2", 5)
	b3 := mockBackendWithConns("backend3", 5)

	backends := []*backend.Backend{b1, b2, b3}

	// Make multiple selections and verify distribution
	selections := make(map[string]int)
	for i := 0; i < 9; i++ {
		result := lc.Next(nil, backends)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		selections[result.ID]++
	}

	// All backends should be selected (round-robin tie-breaking)
	if len(selections) != 3 {
		t.Errorf("expected all 3 backends to be selected, got %d", len(selections))
	}

	// Each should be selected 3 times
	for id, count := range selections {
		if count != 3 {
			t.Errorf("expected backend %s to be selected 3 times, got %d", id, count)
		}
	}
}

func TestLeastConnections_Next_ZeroConns(t *testing.T) {
	lc := NewLeastConnections()

	b1 := mockBackendWithConns("backend1", 5)
	b2 := mockBackendWithConns("backend2", 0)
	b3 := mockBackendWithConns("backend3", 3)

	backends := []*backend.Backend{b1, b2, b3}

	// Should select backend2 with 0 connections
	result := lc.Next(nil, backends)
	if result != b2 {
		t.Errorf("expected backend2 (0 conns), got %s (%d conns)", result.ID, result.ActiveConns())
	}
}

func TestLeastConnections_Add(t *testing.T) {
	lc := NewLeastConnections()

	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b2 := backend.NewBackend("backend2", "127.0.0.1:8081")

	lc.Add(b1)
	lc.Add(b2)

	// Verify internal state
	lc.mu.RLock()
	if len(lc.backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(lc.backends))
	}
	lc.mu.RUnlock()

	// Adding duplicate should not increase count
	lc.Add(b1)
	lc.mu.RLock()
	if len(lc.backends) != 2 {
		t.Errorf("expected 2 backends after duplicate add, got %d", len(lc.backends))
	}
	lc.mu.RUnlock()
}

func TestLeastConnections_Remove(t *testing.T) {
	lc := NewLeastConnections()

	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b2 := backend.NewBackend("backend2", "127.0.0.1:8081")
	b3 := backend.NewBackend("backend3", "127.0.0.1:8082")

	lc.Add(b1)
	lc.Add(b2)
	lc.Add(b3)

	lc.Remove("backend2")

	lc.mu.RLock()
	if len(lc.backends) != 2 {
		t.Errorf("expected 2 backends after remove, got %d", len(lc.backends))
	}

	// Verify correct backend was removed
	for _, b := range lc.backends {
		if b.ID == "backend2" {
			t.Error("backend2 should have been removed")
		}
	}
	lc.mu.RUnlock()

	// Removing non-existent should not panic
	lc.Remove("nonexistent")
}

func TestLeastConnections_Update(t *testing.T) {
	lc := NewLeastConnections()

	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b1.SetWeight(1)

	lc.Add(b1)

	// Update with new backend instance with same ID but different properties
	b1Updated := backend.NewBackend("backend1", "127.0.0.1:9090")
	b1Updated.SetWeight(5)

	lc.Update(b1Updated)

	lc.mu.RLock()
	found := false
	for _, b := range lc.backends {
		if b.ID == "backend1" {
			found = true
			if b.Address != "127.0.0.1:9090" {
				t.Errorf("expected address update, got %s", b.Address)
			}
		}
	}
	lc.mu.RUnlock()

	if !found {
		t.Error("backend1 should still exist after update")
	}
}

func TestLeastConnections_Concurrent(t *testing.T) {
	lc := NewLeastConnections()

	b1 := mockBackendWithConns("backend1", 0)
	b2 := mockBackendWithConns("backend2", 0)
	b3 := mockBackendWithConns("backend3", 0)

	backends := []*backend.Backend{b1, b2, b3}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				lc.Next(nil, backends)
			}
		}()
	}
	wg.Wait()
}

// Weighted Least Connections Tests

func TestWeightedLeastConnections_Name(t *testing.T) {
	wlc := NewWeightedLeastConnections()
	if wlc.Name() != "weighted_least_connections" {
		t.Errorf("expected name 'weighted_least_connections', got '%s'", wlc.Name())
	}
}

func TestWeightedLeastConnections_Next_Empty(t *testing.T) {
	wlc := NewWeightedLeastConnections()
	result := wlc.Next(nil, nil)
	if result != nil {
		t.Error("expected nil for empty backend list")
	}
}

func TestWeightedLeastConnections_Next_SingleBackend(t *testing.T) {
	wlc := NewWeightedLeastConnections()
	b := mockWeightedBackend("backend1", 5, 10)

	result := wlc.Next(nil, []*backend.Backend{b})
	if result != b {
		t.Error("expected single backend to be selected")
	}
}

func TestWeightedLeastConnections_Next_ByRatio(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	// backend1: 10 conns / weight 5 = ratio 2.0
	// backend2: 5 conns / weight 5 = ratio 1.0 (lowest)
	// backend3: 15 conns / weight 5 = ratio 3.0
	b1 := mockWeightedBackend("backend1", 5, 10)
	b2 := mockWeightedBackend("backend2", 5, 5)
	b3 := mockWeightedBackend("backend3", 5, 15)

	backends := []*backend.Backend{b1, b2, b3}

	// Should select backend2 with lowest ratio
	result := wlc.Next(nil, backends)
	if result != b2 {
		t.Errorf("expected backend2 (ratio 1.0), got %s (ratio %.2f)",
			result.ID, float64(result.ActiveConns())/float64(result.GetWeight()))
	}
}

func TestWeightedLeastConnections_Next_WeightedRatio(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	// backend1: 10 conns / weight 2 = ratio 5.0
	// backend2: 20 conns / weight 5 = ratio 4.0 (lowest)
	// backend3: 30 conns / weight 10 = ratio 3.0 (lowest)
	b1 := mockWeightedBackend("backend1", 2, 10)
	b2 := mockWeightedBackend("backend2", 5, 20)
	b3 := mockWeightedBackend("backend3", 10, 30)

	backends := []*backend.Backend{b1, b2, b3}

	// backend3 has the lowest ratio (3.0)
	result := wlc.Next(nil, backends)
	if result != b3 {
		t.Errorf("expected backend3 (ratio 3.0), got %s (ratio %.2f)",
			result.ID, float64(result.ActiveConns())/float64(result.GetWeight()))
	}
}

func TestWeightedLeastConnections_Next_TieBreaking(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	// All backends have the same ratio: 2.0
	// backend1: 10 conns / weight 5 = 2.0
	// backend2: 20 conns / weight 10 = 2.0
	// backend3: 30 conns / weight 15 = 2.0
	b1 := mockWeightedBackend("backend1", 5, 10)
	b2 := mockWeightedBackend("backend2", 10, 20)
	b3 := mockWeightedBackend("backend3", 15, 30)

	backends := []*backend.Backend{b1, b2, b3}

	// Make multiple selections and verify distribution
	selections := make(map[string]int)
	for i := 0; i < 9; i++ {
		result := wlc.Next(nil, backends)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		selections[result.ID]++
	}

	// All backends should be selected (round-robin tie-breaking)
	if len(selections) != 3 {
		t.Errorf("expected all 3 backends to be selected, got %d", len(selections))
	}

	// Each should be selected 3 times
	for id, count := range selections {
		if count != 3 {
			t.Errorf("expected backend %s to be selected 3 times, got %d", id, count)
		}
	}
}

func TestWeightedLeastConnections_Next_ZeroWeight(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	// backend1: 10 conns / weight 0 (defaults to 1) = 10.0
	// backend2: 5 conns / weight 1 = 5.0 (lowest)
	b1 := mockWeightedBackend("backend1", 0, 10)
	b2 := mockWeightedBackend("backend2", 1, 5)

	backends := []*backend.Backend{b1, b2}

	result := wlc.Next(nil, backends)
	if result != b2 {
		t.Errorf("expected backend2 (ratio 5.0), got %s", result.ID)
	}
}

func TestWeightedLeastConnections_Add(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b1.SetWeight(5)
	b2 := backend.NewBackend("backend2", "127.0.0.1:8081")
	b2.SetWeight(10)

	wlc.Add(b1)
	wlc.Add(b2)

	wlc.mu.RLock()
	if len(wlc.backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(wlc.backends))
	}
	wlc.mu.RUnlock()

	// Adding duplicate should not increase count
	wlc.Add(b1)
	wlc.mu.RLock()
	if len(wlc.backends) != 2 {
		t.Errorf("expected 2 backends after duplicate add, got %d", len(wlc.backends))
	}
	wlc.mu.RUnlock()
}

func TestWeightedLeastConnections_Remove(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b2 := backend.NewBackend("backend2", "127.0.0.1:8081")
	b3 := backend.NewBackend("backend3", "127.0.0.1:8082")

	wlc.Add(b1)
	wlc.Add(b2)
	wlc.Add(b3)

	wlc.Remove("backend2")

	wlc.mu.RLock()
	if len(wlc.backends) != 2 {
		t.Errorf("expected 2 backends after remove, got %d", len(wlc.backends))
	}

	if _, exists := wlc.backends["backend2"]; exists {
		t.Error("backend2 should have been removed")
	}
	wlc.mu.RUnlock()
}

func TestWeightedLeastConnections_Update(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	b1 := backend.NewBackend("backend1", "127.0.0.1:8080")
	b1.SetWeight(5)

	wlc.Add(b1)

	// Update with new weight
	b1Updated := backend.NewBackend("backend1", "127.0.0.1:8080")
	b1Updated.SetWeight(10)

	wlc.Update(b1Updated)

	wlc.mu.RLock()
	wb := wlc.backends["backend1"]
	if wb == nil {
		t.Fatal("backend1 should exist")
	}
	if wb.weight != 10 {
		t.Errorf("expected weight 10, got %d", wb.weight)
	}
	wlc.mu.RUnlock()
}

func TestWeightedLeastConnections_Concurrent(t *testing.T) {
	wlc := NewWeightedLeastConnections()

	b1 := mockWeightedBackend("backend1", 5, 0)
	b2 := mockWeightedBackend("backend2", 5, 0)
	b3 := mockWeightedBackend("backend3", 5, 0)

	backends := []*backend.Backend{b1, b2, b3}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				wlc.Next(nil, backends)
			}
		}()
	}
	wg.Wait()
}

// Benchmarks

func BenchmarkLeastConnections_Next(b *testing.B) {
	lc := NewLeastConnections()

	backends := []*backend.Backend{
		mockBackendWithConns("backend1", 5),
		mockBackendWithConns("backend2", 2),
		mockBackendWithConns("backend3", 10),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lc.Next(nil, backends)
	}
}

func BenchmarkLeastConnections_Next_Tie(b *testing.B) {
	lc := NewLeastConnections()

	// All same connection count - forces tie-breaking
	backends := []*backend.Backend{
		mockBackendWithConns("backend1", 5),
		mockBackendWithConns("backend2", 5),
		mockBackendWithConns("backend3", 5),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lc.Next(nil, backends)
	}
}

func BenchmarkWeightedLeastConnections_Next(b *testing.B) {
	wlc := NewWeightedLeastConnections()

	backends := []*backend.Backend{
		mockWeightedBackend("backend1", 5, 10),
		mockWeightedBackend("backend2", 5, 5),
		mockWeightedBackend("backend3", 5, 15),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wlc.Next(nil, backends)
	}
}

func BenchmarkWeightedLeastConnections_Next_Tie(b *testing.B) {
	wlc := NewWeightedLeastConnections()

	// All same ratio - forces tie-breaking
	backends := []*backend.Backend{
		mockWeightedBackend("backend1", 5, 10),
		mockWeightedBackend("backend2", 10, 20),
		mockWeightedBackend("backend3", 15, 30),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wlc.Next(nil, backends)
	}
}

func BenchmarkLeastConnections_Concurrent(b *testing.B) {
	lc := NewLeastConnections()

	backends := []*backend.Backend{
		mockBackendWithConns("backend1", 5),
		mockBackendWithConns("backend2", 2),
		mockBackendWithConns("backend3", 10),
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lc.Next(nil, backends)
		}
	})
}

func BenchmarkWeightedLeastConnections_Concurrent(b *testing.B) {
	wlc := NewWeightedLeastConnections()

	backends := []*backend.Backend{
		mockWeightedBackend("backend1", 5, 10),
		mockWeightedBackend("backend2", 5, 5),
		mockWeightedBackend("backend3", 5, 15),
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wlc.Next(nil, backends)
		}
	})
}
