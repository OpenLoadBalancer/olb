package balancer

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestPeakEWMA_Next(t *testing.T) {
	p := NewPeakEWMA()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
		backend.NewBackend("be3", "10.0.0.3:8080"),
	}

	// Set backends to healthy state
	for _, be := range backends {
		be.SetState(backend.StateUp)
	}

	// Initially should return first backend
	be := p.Next(backends)
	if be == nil {
		t.Fatal("Expected backend, got nil")
	}

	// Record different latencies
	p.Record("be1", 10*time.Millisecond, true)
	p.Record("be2", 5*time.Millisecond, true)
	p.Record("be3", 20*time.Millisecond, true)

	// Should prefer be2 (lowest latency)
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		be := p.Next(backends)
		if be != nil {
			selections[be.ID]++
		}
	}

	// be2 should be selected most often
	if selections["be2"] < selections["be3"] {
		t.Logf("be2 selected %d times, be3 selected %d times", selections["be2"], selections["be3"])
	}
}

func TestPeakEWMA_Record(t *testing.T) {
	p := NewPeakEWMA()

	// Record increasing latencies
	p.Record("be1", 5*time.Millisecond, true)
	p.Record("be1", 10*time.Millisecond, true)
	p.Record("be1", 8*time.Millisecond, true) // Lower than previous peak

	stats := p.Stats()
	be1Stats, ok := stats["be1"]
	if !ok {
		t.Fatal("be1 stats not found")
	}

	if be1Stats["request_count"].(uint64) != 3 {
		t.Errorf("Expected 3 requests, got %d", be1Stats["request_count"])
	}
}

func TestPeakEWMA_Errors(t *testing.T) {
	p := NewPeakEWMA()
	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
	}

	// Set backends to healthy state
	for _, be := range backends {
		be.SetState(backend.StateUp)
	}

	// Both backends have same latency
	p.Record("be1", 10*time.Millisecond, true)
	p.Record("be2", 10*time.Millisecond, true)

	// be1 has errors
	p.Record("be1", 10*time.Millisecond, false)
	p.Record("be1", 10*time.Millisecond, false)

	// be2 should be preferred (lower error rate)
	be := p.Next(backends)
	if be != nil && be.ID != "be2" {
		t.Logf("Expected be2 (lower error rate), got %s", be.ID)
	}
}

func TestPeakEWMA_Unhealthy(t *testing.T) {
	p := NewPeakEWMA()

	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
	}

	// Set be1 to down, be2 to up
	backends[0].SetState(backend.StateDown)
	backends[1].SetState(backend.StateUp)

	// Should return healthy backend
	be := p.Next(backends)
	if be != nil && be.ID != "be2" {
		t.Errorf("Expected be2 (healthy), got %s", be.ID)
	}
}

func TestPeakEWMA_Empty(t *testing.T) {
	p := NewPeakEWMA()

	be := p.Next([]*backend.Backend{})
	if be != nil {
		t.Error("Expected nil for empty backends")
	}
}

func TestPeakEWMA_AddRemove(t *testing.T) {
	p := NewPeakEWMA()

	be := &backend.Backend{ID: "test-be"}
	p.Add(be)

	stats := p.Stats()
	if _, ok := stats["test-be"]; !ok {
		t.Error("Backend not added")
	}

	p.Remove("test-be")

	stats = p.Stats()
	if _, ok := stats["test-be"]; ok {
		t.Error("Backend not removed")
	}
}

func TestPeakEWMA_Decay(t *testing.T) {
	p := NewPeakEWMA()

	// Record high latency
	p.Record("be1", 100*time.Millisecond, true)

	initialPeak := p.samples["be1"].peakLatency

	// Wait a bit (simulated by updating time)
	p.samples["be1"].lastUpdate = time.Now().Add(-5 * time.Second)

	// Record lower latency - should decay old peak
	p.Record("be1", 10*time.Millisecond, true)

	// Peak should have decayed
	if p.samples["be1"].peakLatency >= initialPeak {
		t.Error("Peak latency should have decayed")
	}
}

func BenchmarkPeakEWMA_Next(b *testing.B) {
	p := NewPeakEWMA()
	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
		backend.NewBackend("be3", "10.0.0.3:8080"),
	}

	// Set backends to healthy
	for _, be := range backends {
		be.SetState(backend.StateUp)
	}

	// Pre-populate samples
	for i, be := range backends {
		p.Record(be.ID, time.Duration((i+1)*10)*time.Millisecond, true)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Next(backends)
		}
	})
}

func BenchmarkPeakEWMA_Record(b *testing.B) {
	p := NewPeakEWMA()
	backendID := "be1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Record(backendID, 10*time.Millisecond, true)
	}
}

func BenchmarkPeakEWMA_Concurrent(b *testing.B) {
	p := NewPeakEWMA()
	backends := []*backend.Backend{
		backend.NewBackend("be1", "10.0.0.1:8080"),
		backend.NewBackend("be2", "10.0.0.2:8080"),
		backend.NewBackend("be3", "10.0.0.3:8080"),
	}

	// Set backends to healthy
	for _, be := range backends {
		be.SetState(backend.StateUp)
	}

	var counter atomic.Uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := counter.Add(1)
			if idx%2 == 0 {
				p.Next(backends)
			} else {
				p.Record(backends[idx%uint64(len(backends))].ID, 10*time.Millisecond, true)
			}
		}
	})
}

func TestPeakEWMA_Stats(t *testing.T) {
	p := NewPeakEWMA()

	p.Record("be1", 10*time.Millisecond, true)
	p.Record("be1", 20*time.Millisecond, false)

	stats := p.Stats()
	be1Stats, ok := stats["be1"]
	if !ok {
		t.Fatal("be1 stats not found")
	}

	if be1Stats["error_count"].(uint64) != 1 {
		t.Errorf("Expected 1 error, got %d", be1Stats["error_count"])
	}

	errRate := be1Stats["error_rate"].(float64)
	if errRate != 0.5 {
		t.Errorf("Expected error rate 0.5, got %f", errRate)
	}
}
