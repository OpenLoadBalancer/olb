package balancer

import (
	"sync"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestPowerOfTwo_Name(t *testing.T) {
	p2c := NewPowerOfTwo()
	if name := p2c.Name(); name != "power_of_two" {
		t.Errorf("expected name 'power_of_two', got '%s'", name)
	}
}

func TestPowerOfTwo_Next_EmptyBackends(t *testing.T) {
	p2c := NewPowerOfTwo()
	backends := []*backend.Backend{}

	result := p2c.Next(backends)
	if result != nil {
		t.Errorf("expected nil for empty backends, got %v", result)
	}
}

func TestPowerOfTwo_Next_SingleBackend(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	backends := []*backend.Backend{b1}

	result := p2c.Next(backends)
	if result != b1 {
		t.Errorf("expected b1, got %v", result)
	}
}

func TestPowerOfTwo_Next_TwoBackends_PicksFewerConns(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")

	// Simulate b1 having more connections
	b1.AcquireConn()
	b1.AcquireConn()
	b1.AcquireConn()

	// b2 has fewer connections (0)

	backends := []*backend.Backend{b1, b2}

	// Run multiple times to account for randomness
	// With P2C, we should almost always pick b2 since it has fewer conns
	b2Count := 0
	iterations := 100
	for i := 0; i < iterations; i++ {
		result := p2c.Next(backends)
		if result == b2 {
			b2Count++
		}
	}

	// b2 should be picked significantly more often since it has 0 vs 3 connections
	// Even with random selection of 2, b2 should be favored
	if b2Count < iterations/2 {
		t.Errorf("expected b2 (fewer conns) to be picked more often, got %d/%d", b2Count, iterations)
	}
}

func TestPowerOfTwo_Next_PicksBetweenTwoRandomChoices(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Run many iterations to verify both backends get selected
	b1Count := 0
	b2Count := 0
	b3Count := 0
	iterations := 1000

	for i := 0; i < iterations; i++ {
		result := p2c.Next(backends)
		switch result {
		case b1:
			b1Count++
		case b2:
			b2Count++
		case b3:
			b3Count++
		}
	}

	// All backends should have been selected at least once
	if b1Count == 0 {
		t.Error("b1 was never selected")
	}
	if b2Count == 0 {
		t.Error("b2 was never selected")
	}
	if b3Count == 0 {
		t.Error("b3 was never selected")
	}

	// With equal connections, distribution should be roughly even
	// Allow for some variance due to randomness
	t.Logf("Distribution: b1=%d, b2=%d, b3=%d", b1Count, b2Count, b3Count)
}

func TestPowerOfTwo_Next_EqualConns_RandomPick(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")

	// Both have same number of connections
	b1.AcquireConn()
	b2.AcquireConn()

	backends := []*backend.Backend{b1, b2}

	// Run multiple times - both should be picked roughly equally
	b1Count := 0
	b2Count := 0
	iterations := 100

	for i := 0; i < iterations; i++ {
		result := p2c.Next(backends)
		if result == b1 {
			b1Count++
		} else if result == b2 {
			b2Count++
		}
	}

	// Both should have been picked at least once
	if b1Count == 0 {
		t.Error("b1 was never selected with equal connections")
	}
	if b2Count == 0 {
		t.Error("b2 was never selected with equal connections")
	}
}

func TestPowerOfTwo_Add(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")

	p2c.Add(b1)
	p2c.Add(b2)

	// Verify backends are tracked
	p2c.mu.RLock()
	if len(p2c.backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(p2c.backends))
	}
	p2c.mu.RUnlock()

	// Adding duplicate should not increase count
	p2c.Add(b1)
	p2c.mu.RLock()
	if len(p2c.backends) != 2 {
		t.Errorf("expected 2 backends after duplicate add, got %d", len(p2c.backends))
	}
	p2c.mu.RUnlock()
}

func TestPowerOfTwo_Remove(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	p2c.Add(b1)
	p2c.Add(b2)
	p2c.Add(b3)

	// Remove middle backend
	p2c.Remove("b2")

	p2c.mu.RLock()
	if len(p2c.backends) != 2 {
		t.Errorf("expected 2 backends after remove, got %d", len(p2c.backends))
	}

	// Verify correct backend was removed
	for _, b := range p2c.backends {
		if b.ID == "b2" {
			t.Error("b2 should have been removed")
		}
	}
	p2c.mu.RUnlock()

	// Remove non-existent should not panic
	p2c.Remove("nonexistent")
}

func TestPowerOfTwo_Update_NoOp(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")

	p2c.Add(b1)
	p2c.Add(b2)

	// Update is a no-op for PowerOfTwo, but should not panic
	p2c.Update(b1)

	// Verify backends are still tracked after Update
	p2c.mu.RLock()
	if len(p2c.backends) != 2 {
		t.Errorf("expected 2 backends after Update, got %d", len(p2c.backends))
	}
	p2c.mu.RUnlock()

	// Balancer should still function after Update
	backends := []*backend.Backend{b1, b2}
	result := p2c.Next(backends)
	if result == nil {
		t.Error("Next() returned nil after Update")
	}
}

func TestPowerOfTwo_Update_NilBackend(t *testing.T) {
	p := &PowerOfTwo{}
	p.Update(nil) // no-op, should not panic
}

func TestPowerOfTwo_ConcurrentAccess(t *testing.T) {
	p2c := NewPowerOfTwo()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Add backends
	p2c.Add(b1)
	p2c.Add(b2)
	p2c.Add(b3)

	var wg sync.WaitGroup
	numGoroutines := 10
	iterations := 100

	// Concurrent Next calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = p2c.Next(backends)
			}
		}()
	}

	// Concurrent Add/Remove calls
	b4 := backend.NewBackend("b4", "127.0.0.1:8083")
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					p2c.Add(b4)
				} else {
					p2c.Remove("b4")
				}
			}
		}(i)
	}

	wg.Wait()

	// Test passed if no panic or race condition detected
}

func TestPowerOfTwo_Next_ManyBackends(t *testing.T) {
	p2c := NewPowerOfTwo()
	backends := make([]*backend.Backend, 10)

	for i := 0; i < 10; i++ {
		backends[i] = backend.NewBackend(
			string(rune('a'+i)),
			"127.0.0.1:8080",
		)
		// Vary connection counts
		for j := 0; j < i; j++ {
			backends[i].AcquireConn()
		}
	}

	// Backend 'a' (index 0) has 0 connections - should be favored
	// Backend 'j' (index 9) has 9 connections - should be rarely picked

	aCount := 0
	jCount := 0
	iterations := 1000

	for i := 0; i < iterations; i++ {
		result := p2c.Next(backends)
		if result == backends[0] {
			aCount++
		} else if result == backends[9] {
			jCount++
		}
	}

	// Backend with 0 conns should be picked more than backend with 9 conns
	if aCount <= jCount {
		t.Errorf("expected backend with 0 conns (a) to be picked more than backend with 9 conns (j), got a=%d, j=%d", aCount, jCount)
	}

	t.Logf("Distribution with varying conns: a(0 conns)=%d, j(9 conns)=%d", aCount, jCount)
}
