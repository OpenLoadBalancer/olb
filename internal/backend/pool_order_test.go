package backend_test

import (
	"fmt"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
)

func newOrderedPool(t *testing.T, algorithm string, n int) *backend.Pool {
	t.Helper()
	p := backend.NewPool("p", algorithm)
	bal := balancer.New(algorithm)
	if bal == nil {
		t.Fatalf("balancer.New(%q) = nil", algorithm)
	}
	p.SetBalancer(bal)
	for i := 0; i < n; i++ {
		b := backend.NewBackend(fmt.Sprintf("b%d", i), fmt.Sprintf("127.0.0.1:%d", 9000+i))
		b.SetState(backend.StateUp)
		if err := p.AddBackend(b); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func pick(p *backend.Pool, ctx *backend.RequestContext) string {
	healthy := p.GetHealthyBackends()
	defer backend.ReleaseHealthyBackends(healthy)
	return p.GetBalancer().Next(ctx, healthy).ID
}

// Index-based balancers depend on a stable backend order. Pool.Backends is a
// map, so building the slice from map iteration turned round robin into a
// random pick (e2e saw 16/6/8 over 30 sequential requests).
func TestGetHealthyBackends_StableOrderForRoundRobin(t *testing.T) {
	p := newOrderedPool(t, "round_robin", 3)
	hits := map[string]int{}
	for i := 0; i < 300; i++ {
		hits[pick(p, nil)]++
	}
	for id, n := range hits {
		if n != 100 {
			t.Errorf("round robin: %s got %d of 300, want exactly 100 (hits=%v)", id, n, hits)
		}
	}
}

func TestGetHealthyBackends_StableOrderForIPHash(t *testing.T) {
	p := newOrderedPool(t, "ip_hash", 5)
	ctx := &backend.RequestContext{ClientIP: "203.0.113.7"}
	first := pick(p, ctx)
	for i := 0; i < 200; i++ {
		if got := pick(p, ctx); got != first {
			t.Fatalf("ip_hash: same client mapped to %s and %s", first, got)
		}
	}
}

func TestGetAllBackends_InsertionOrder(t *testing.T) {
	p := newOrderedPool(t, "round_robin", 6)
	if err := p.RemoveBackend("b2"); err != nil {
		t.Fatal(err)
	}
	want := []string{"b0", "b1", "b3", "b4", "b5"}
	for round := 0; round < 20; round++ {
		all := p.GetAllBackends()
		for i, b := range all {
			if b.ID != want[i] {
				t.Fatalf("GetAllBackends order = %v, want %v", ids(all), want)
			}
		}
	}
}

func ids(bs []*backend.Backend) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ID
	}
	return out
}
