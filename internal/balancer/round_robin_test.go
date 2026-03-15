package balancer

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestRoundRobin_Name(t *testing.T) {
	rr := NewRoundRobin()
	if rr.Name() != "round_robin" {
		t.Errorf("Name() = %v, want %v", rr.Name(), "round_robin")
	}
}

func TestRoundRobin_Next(t *testing.T) {
	rr := NewRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Test rotation
	results := make([]*backend.Backend, 0, 6)
	for i := 0; i < 6; i++ {
		b := rr.Next(backends)
		if b == nil {
			t.Fatalf("Next() returned nil at iteration %d", i)
		}
		results = append(results, b)
	}

	// Should rotate: b1, b2, b3, b1, b2, b3
	expected := []*backend.Backend{b1, b2, b3, b1, b2, b3}
	for i, exp := range expected {
		if results[i] != exp {
			t.Errorf("Next() at %d = %v, want %v", i, results[i].ID, exp.ID)
		}
	}
}

func TestRoundRobin_Next_Empty(t *testing.T) {
	rr := NewRoundRobin()

	result := rr.Next([]*backend.Backend{})
	if result != nil {
		t.Error("Next() with empty backends should return nil")
	}
}

func TestRoundRobin_Next_Nil(t *testing.T) {
	rr := NewRoundRobin()

	result := rr.Next(nil)
	if result != nil {
		t.Error("Next() with nil backends should return nil")
	}
}

func TestRoundRobin_Next_SingleBackend(t *testing.T) {
	rr := NewRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	backends := []*backend.Backend{b1}

	// Should always return the same backend
	for i := 0; i < 10; i++ {
		result := rr.Next(backends)
		if result != b1 {
			t.Errorf("Next() = %v, want %v at iteration %d", result, b1, i)
		}
	}
}

func TestRoundRobin_Distribution(t *testing.T) {
	rr := NewRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Collect distribution
	counts := map[string]int{
		"b1": 0,
		"b2": 0,
		"b3": 0,
	}

	numRequests := 3000
	for i := 0; i < numRequests; i++ {
		b := rr.Next(backends)
		if b != nil {
			counts[b.ID]++
		}
	}

	// Each backend should get exactly 1/3
	expected := numRequests / 3
	tolerance := 10

	for id, count := range counts {
		if count < expected-tolerance || count > expected+tolerance {
			t.Errorf("Backend %s count = %d, want ~%d", id, count, expected)
		}
	}
}

func TestRoundRobin_Concurrent(t *testing.T) {
	rr := NewRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				rr.Next(backends)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestRoundRobin_AddRemoveUpdate(t *testing.T) {
	rr := NewRoundRobin()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")

	// These should be no-ops and not panic
	rr.Add(b1)
	rr.Remove("b1")
	rr.Update(b1)
}

func BenchmarkRoundRobin_Next(b *testing.B) {
	rr := NewRoundRobin()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	be2 := backend.NewBackend("b2", "127.0.0.1:8081")
	be3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{be1, be2, be3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr.Next(backends)
	}
}

func TestRoundRobin_Add_NoOp(t *testing.T) {
	rr := NewRoundRobin()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")

	// Add should be a no-op and not panic
	rr.Add(b1)

	// Verify balancer still works after Add
	backends := []*backend.Backend{b1}
	result := rr.Next(backends)
	if result != b1 {
		t.Errorf("Next() after Add = %v, want %v", result, b1)
	}
}

func TestRoundRobin_Remove_NoOp(t *testing.T) {
	rr := NewRoundRobin()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")

	// Remove should be a no-op and not panic
	rr.Remove("b1")
	rr.Remove("nonexistent")

	// Verify balancer still works after Remove
	backends := []*backend.Backend{b1}
	result := rr.Next(backends)
	if result != b1 {
		t.Errorf("Next() after Remove = %v, want %v", result, b1)
	}
}

func TestRoundRobin_Update_NoOp(t *testing.T) {
	rr := NewRoundRobin()
	b1 := backend.NewBackend("b1", "127.0.0.1:8080")

	// Update should be a no-op and not panic
	rr.Update(b1)

	// Verify balancer still works after Update
	backends := []*backend.Backend{b1}
	result := rr.Next(backends)
	if result != b1 {
		t.Errorf("Next() after Update = %v, want %v", result, b1)
	}
}

func BenchmarkRoundRobin_Next_SingleBackend(b *testing.B) {
	rr := NewRoundRobin()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	backends := []*backend.Backend{be1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr.Next(backends)
	}
}
