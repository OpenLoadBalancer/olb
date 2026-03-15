package backend

import (
	"context"
	"testing"

	"github.com/openloadbalancer/olb/pkg/errors"
)

func TestNewPool(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	if p.Name != "test-pool" {
		t.Errorf("Pool.Name = %v, want %v", p.Name, "test-pool")
	}
	if p.Algorithm != "roundrobin" {
		t.Errorf("Pool.Algorithm = %v, want %v", p.Algorithm, "roundrobin")
	}
	if p.BackendCount() != 0 {
		t.Errorf("Pool.BackendCount() = %v, want %v", p.BackendCount(), 0)
	}
}

func TestPoolAddBackend(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")

	if err := p.AddBackend(b); err != nil {
		t.Errorf("AddBackend() error = %v", err)
	}

	if p.BackendCount() != 1 {
		t.Errorf("BackendCount() = %v, want %v", p.BackendCount(), 1)
	}

	// Adding duplicate should fail
	if err := p.AddBackend(b); !errors.Is(err, errors.ErrAlreadyExist) {
		t.Errorf("AddBackend() duplicate error = %v, want ErrAlreadyExist", err)
	}
}

func TestPoolRemoveBackend(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")

	p.AddBackend(b)

	if err := p.RemoveBackend("b1"); err != nil {
		t.Errorf("RemoveBackend() error = %v", err)
	}

	if p.BackendCount() != 0 {
		t.Errorf("BackendCount() after remove = %v, want %v", p.BackendCount(), 0)
	}

	// Removing non-existent should fail
	if err := p.RemoveBackend("b1"); !errors.Is(err, errors.ErrBackendNotFound) {
		t.Errorf("RemoveBackend() error = %v, want ErrBackendNotFound", err)
	}
}

func TestPoolGetBackend(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")

	p.AddBackend(b)

	got := p.GetBackend("b1")
	if got == nil {
		t.Fatal("GetBackend() returned nil")
	}
	if got.ID != "b1" {
		t.Errorf("GetBackend().ID = %v, want %v", got.ID, "b1")
	}

	// Get non-existent
	if p.GetBackend("nonexistent") != nil {
		t.Error("GetBackend(nonexistent) should return nil")
	}
}

func TestPoolGetHealthyBackends(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	b1 := NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(StateUp)
	b2 := NewBackend("b2", "127.0.0.1:8081")
	b2.SetState(StateDown)
	b3 := NewBackend("b3", "127.0.0.1:8082")
	b3.SetState(StateDraining)

	p.AddBackend(b1)
	p.AddBackend(b2)
	p.AddBackend(b3)

	healthy := p.GetHealthyBackends()
	if len(healthy) != 2 {
		t.Errorf("GetHealthyBackends() = %v, want 2", len(healthy))
	}

	// b1 (Up) and b3 (Draining) should be healthy
	hasB1, hasB3 := false, false
	for _, b := range healthy {
		if b.ID == "b1" {
			hasB1 = true
		}
		if b.ID == "b3" {
			hasB3 = true
		}
	}
	if !hasB1 {
		t.Error("GetHealthyBackends() should include b1 (Up)")
	}
	if !hasB3 {
		t.Error("GetHealthyBackends() should include b3 (Draining)")
	}
}

func TestPoolDrainBackend(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateUp)

	p.AddBackend(b)

	if err := p.DrainBackend("b1"); err != nil {
		t.Errorf("DrainBackend() error = %v", err)
	}

	if b.State() != StateDraining {
		t.Errorf("State after drain = %v, want %v", b.State(), StateDraining)
	}

	// Draining non-existent should fail
	if err := p.DrainBackend("nonexistent"); !errors.Is(err, errors.ErrBackendNotFound) {
		t.Errorf("DrainBackend() error = %v, want ErrBackendNotFound", err)
	}
}

func TestPoolEnableBackend(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateDown)

	p.AddBackend(b)

	if err := p.EnableBackend("b1"); err != nil {
		t.Errorf("EnableBackend() error = %v", err)
	}

	if b.State() != StateUp {
		t.Errorf("State after enable = %v, want %v", b.State(), StateUp)
	}
}

func TestPoolDisableBackend(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateUp)

	p.AddBackend(b)

	if err := p.DisableBackend("b1"); err != nil {
		t.Errorf("DisableBackend() error = %v", err)
	}

	if b.State() != StateMaintenance {
		t.Errorf("State after disable = %v, want %v", b.State(), StateMaintenance)
	}
}

func TestPoolBackendCount(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	if p.BackendCount() != 0 {
		t.Errorf("BackendCount() initial = %v, want 0", p.BackendCount())
	}

	p.AddBackend(NewBackend("b1", "127.0.0.1:8080"))
	if p.BackendCount() != 1 {
		t.Errorf("BackendCount() after add = %v, want 1", p.BackendCount())
	}

	p.AddBackend(NewBackend("b2", "127.0.0.1:8081"))
	if p.BackendCount() != 2 {
		t.Errorf("BackendCount() after add = %v, want 2", p.BackendCount())
	}
}

func TestPoolHealthyCount(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	b1 := NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(StateUp)
	b2 := NewBackend("b2", "127.0.0.1:8081")
	b2.SetState(StateUp)
	b3 := NewBackend("b3", "127.0.0.1:8082")
	b3.SetState(StateDown)

	p.AddBackend(b1)
	p.AddBackend(b2)
	p.AddBackend(b3)

	if p.HealthyCount() != 2 {
		t.Errorf("HealthyCount() = %v, want 2", p.HealthyCount())
	}
}

func TestPoolGetAllBackends(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	p.AddBackend(NewBackend("b1", "127.0.0.1:8080"))
	p.AddBackend(NewBackend("b2", "127.0.0.1:8081"))

	all := p.GetAllBackends()
	if len(all) != 2 {
		t.Errorf("GetAllBackends() = %v, want 2", len(all))
	}
}

func TestPoolSetBalancer(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	// Create a mock balancer
	mb := &mockBalancer{name: "mock"}
	p.SetBalancer(mb)

	// The balancer should be set (we can't easily verify without exporting the field)
	// This test mainly ensures it doesn't panic
}

func TestPoolGetBalancer(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	// Initially no balancer is set
	if got := p.GetBalancer(); got != nil {
		t.Errorf("GetBalancer() on new pool = %v, want nil", got)
	}

	// Set a balancer and retrieve it
	mb := &mockBalancer{name: "test-balancer"}
	p.SetBalancer(mb)

	got := p.GetBalancer()
	if got == nil {
		t.Fatal("GetBalancer() returned nil after SetBalancer")
	}
	if got.Name() != "test-balancer" {
		t.Errorf("GetBalancer().Name() = %v, want test-balancer", got.Name())
	}
}

func TestPoolNextBackend_NoBalancer(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	ctx := context.Background()
	_, err := p.NextBackend(ctx)
	if err == nil {
		t.Error("NextBackend() should error when no balancer is set")
	}
}

func TestPoolNextBackend_NoHealthyBackends(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	// Add backends but all are down
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateDown)
	p.AddBackend(b)

	// Set a mock balancer
	mockBalancer := &mockBalancer{name: "mock"}
	p.SetBalancer(mockBalancer)

	ctx := context.Background()
	_, err := p.NextBackend(ctx)
	if err == nil {
		t.Error("NextBackend() should error when no healthy backends")
	}
}

func TestPoolStats(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")

	b1 := NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(StateUp)
	b2 := NewBackend("b2", "127.0.0.1:8081")
	b2.SetState(StateDown)

	p.AddBackend(b1)
	p.AddBackend(b2)

	// Record some stats
	b1.RecordRequest(10, 100)
	b1.RecordRequest(20, 200)
	b2.RecordRequest(5, 50)

	stats := p.Stats()

	if stats.Name != "test-pool" {
		t.Errorf("Stats.Name = %v, want test-pool", stats.Name)
	}
	if stats.TotalBackends != 2 {
		t.Errorf("Stats.TotalBackends = %v, want 2", stats.TotalBackends)
	}
	if stats.HealthyBackends != 1 {
		t.Errorf("Stats.HealthyBackends = %v, want 1", stats.HealthyBackends)
	}
	if len(stats.BackendStats) != 2 {
		t.Errorf("len(Stats.BackendStats) = %v, want 2", len(stats.BackendStats))
	}

	// Verify b1 stats
	if stats.BackendStats["b1"].TotalRequests != 2 {
		t.Errorf("b1 TotalRequests = %v, want 2", stats.BackendStats["b1"].TotalRequests)
	}
}

func TestPoolClone(t *testing.T) {
	p := NewPool("test-pool", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateUp)
	p.AddBackend(b)

	clone := p.Clone()

	if clone.Name != p.Name {
		t.Errorf("Clone.Name = %v, want %v", clone.Name, p.Name)
	}
	if clone.Algorithm != p.Algorithm {
		t.Errorf("Clone.Algorithm = %v, want %v", clone.Algorithm, p.Algorithm)
	}
	if clone.BackendCount() != p.BackendCount() {
		t.Errorf("Clone.BackendCount() = %v, want %v", clone.BackendCount(), p.BackendCount())
	}

	// Modifying clone should not affect original
	clone.Name = "modified"
	if p.Name == "modified" {
		t.Error("Clone modification affected original")
	}
}
