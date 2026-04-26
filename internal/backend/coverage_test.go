package backend

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// backend.go coverage
// ---------------------------------------------------------------------------

// TestCov_LastCheck_TypeAssertionFallback exercises the branch in LastCheck()
// where the atomic.Value holds a non-time.Time value, falling through to the
// final return time.Time{}.
func TestCov_LastCheck_TypeAssertionFallback(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	// Store a non-time.Time value to hit the type-assertion-fail branch.
	b.lastCheck.Store("not-a-time")

	got := b.LastCheck()
	if !got.IsZero() {
		t.Errorf("LastCheck() = %v, want zero time when value is wrong type", got)
	}
}

// TestCov_GetURL_EmptyScheme covers the branch where b.Scheme is empty,
// so the default "http" is used.
func TestCov_GetURL_EmptyScheme(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")
	b.Scheme = "" // override the "http" default set by NewBackend

	u := b.GetURL()
	if u == nil {
		t.Fatal("GetURL() returned nil")
	}
	if u.Scheme != "http" {
		t.Errorf("GetURL().Scheme = %q, want %q", u.Scheme, "http")
	}
}

// TestCov_GetURL_ParseErrorFallback covers the branch where url.Parse fails
// and the fallback URL is constructed manually.
func TestCov_GetURL_ParseErrorFallback(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")
	// Use a scheme with control characters that causes url.Parse to fail.
	b.Scheme = "h ttp"
	// Address with characters that make the full URL unparseable together
	// with the bad scheme.
	b.Address = "127.0.0.1:8080"

	u := b.GetURL()
	if u == nil {
		t.Fatal("GetURL() returned nil")
	}
	// Fallback path sets Scheme and Host directly.
	if u.Scheme != "h ttp" {
		t.Errorf("GetURL().Scheme = %q, want %q", u.Scheme, "h ttp")
	}
	if u.Host != "127.0.0.1:8080" {
		t.Errorf("GetURL().Host = %q, want %q", u.Host, "127.0.0.1:8080")
	}
}

// ---------------------------------------------------------------------------
// manager.go coverage
// ---------------------------------------------------------------------------

// TestCov_GetBackendByAddress_Found exercises GetBackendByAddress finding a
// backend by its address across multiple pools.
func TestCov_GetBackendByAddress_Found(t *testing.T) {
	pm := NewPoolManager()

	p1 := NewPool("pool1", "roundrobin")
	p1.AddBackend(NewBackend("b1", "10.0.0.1:8080"))
	p1.AddBackend(NewBackend("b2", "10.0.0.2:8080"))

	p2 := NewPool("pool2", "roundrobin")
	p2.AddBackend(NewBackend("b3", "10.0.0.3:8080"))

	pm.AddPool(p1)
	pm.AddPool(p2)

	got := pm.GetBackendByAddress("10.0.0.2:8080")
	if got == nil {
		t.Fatal("GetBackendByAddress() returned nil, expected backend b2")
	}
	if got.ID != "b2" {
		t.Errorf("GetBackendByAddress() ID = %q, want %q", got.ID, "b2")
	}
}

// TestCov_GetBackendByAddress_NotFound exercises GetBackendByAddress when no
// backend matches.
func TestCov_GetBackendByAddress_NotFound(t *testing.T) {
	pm := NewPoolManager()

	p := NewPool("pool1", "roundrobin")
	p.AddBackend(NewBackend("b1", "10.0.0.1:8080"))
	pm.AddPool(p)

	got := pm.GetBackendByAddress("10.0.0.99:8080")
	if got != nil {
		t.Errorf("GetBackendByAddress() = %v, want nil", got)
	}
}

// TestCov_GetBackendByAddress_EmptyManager exercises GetBackendByAddress on an
// empty PoolManager.
func TestCov_GetBackendByAddress_EmptyManager(t *testing.T) {
	pm := NewPoolManager()
	got := pm.GetBackendByAddress("10.0.0.1:8080")
	if got != nil {
		t.Errorf("GetBackendByAddress() on empty manager = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// pool.go coverage
// ---------------------------------------------------------------------------

// TestCov_AddBackend_NilBackend covers the nil backend validation branch.
func TestCov_AddBackend_NilBackend(t *testing.T) {
	p := NewPool("test", "roundrobin")
	err := p.AddBackend(nil)
	if err == nil {
		t.Error("AddBackend(nil) should return an error")
	}
}

// TestCov_AddBackend_EmptyID covers the empty ID validation branch.
func TestCov_AddBackend_EmptyID(t *testing.T) {
	p := NewPool("test", "roundrobin")
	b := &Backend{ID: "", Address: "127.0.0.1:8080"}
	err := p.AddBackend(b)
	if err == nil {
		t.Error("AddBackend with empty ID should return an error")
	}
}

// TestCov_RemoveBackend_EmptyID covers the empty ID validation branch in
// RemoveBackend.
func TestCov_RemoveBackend_EmptyID(t *testing.T) {
	p := NewPool("test", "roundrobin")
	err := p.RemoveBackend("")
	if err == nil {
		t.Error("RemoveBackend with empty ID should return an error")
	}
}

// TestCov_NextBackend_CanceledContext exercises the ctx.Err() != nil early
// return in NextBackend.
func TestCov_NextBackend_CanceledContext(t *testing.T) {
	p := NewPool("test", "roundrobin")
	p.SetBalancer(&mockBalancer{name: "mock"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.NextBackend(ctx)
	if err == nil {
		t.Error("NextBackend with canceled context should return an error")
	}
}

// TestCov_NextBackend_BalancerReturnsNil exercises the branch where the
// balancer's Next() returns nil even though healthy backends exist.
func TestCov_NextBackend_BalancerReturnsNil(t *testing.T) {
	p := NewPool("test", "roundrobin")

	b1 := NewBackend("b1", "127.0.0.1:8080")
	b1.SetState(StateUp)
	p.AddBackend(b1)

	// Use a balancer that always returns nil.
	p.SetBalancer(&nilBalancer{})

	ctx := context.Background()
	_, err := p.NextBackend(ctx)
	if err == nil {
		t.Error("NextBackend should return error when balancer returns nil")
	}
}

// nilBalancer is a mock Balancer that always returns nil from Next().
type nilBalancer struct{}

func (n *nilBalancer) Name() string                              { return "nil-balancer" }
func (n *nilBalancer) Next(_ *RequestContext, _ []*Backend) *Backend { return nil }
func (n *nilBalancer) Add(_ *Backend)                            {}
func (n *nilBalancer) Remove(_ string)                           {}
func (n *nilBalancer) Update(_ *Backend)                         {}

// ---------------------------------------------------------------------------
// state.go coverage
// ---------------------------------------------------------------------------

// TestCov_CanTransitionTo_UnknownState covers the default case in
// CanTransitionTo where the state value is outside the known enum range.
func TestCov_CanTransitionTo_UnknownState(t *testing.T) {
	unknown := State(99)
	if unknown.CanTransitionTo(StateUp) {
		t.Error("Unknown state should not be able to transition to any state")
	}
	if unknown.CanTransitionTo(StateDown) {
		t.Error("Unknown state should not be able to transition to any state")
	}
}

// ---------------------------------------------------------------------------
// Concurrent coverage for GetBackendByAddress
// ---------------------------------------------------------------------------

// TestCov_GetBackendByAddress_Concurrent exercises GetBackendByAddress under
// concurrent read pressure.
func TestCov_GetBackendByAddress_Concurrent(t *testing.T) {
	pm := NewPoolManager()

	for i := 0; i < 5; i++ {
		p := NewPool(string(rune('a'+i)), "roundrobin")
		for j := 0; j < 5; j++ {
			addr := "10.0." + string(rune('0'+i)) + "." + string(rune('0'+j)) + ":8080"
			p.AddBackend(NewBackend("b"+string(rune('0'+j)), addr))
		}
		pm.AddPool(p)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = pm.GetBackendByAddress("10.0.0.1:8080")
				_ = pm.GetBackendByAddress("nonexistent:8080")
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Extra edge cases to improve confidence
// ---------------------------------------------------------------------------

// TestCov_GetURL_ConcurrentMixedSchemes verifies concurrent GetURL calls on
// backends with different schemes.
func TestCov_GetURL_ConcurrentMixedSchemes(t *testing.T) {
	backends := []*Backend{
		NewBackend("b1", "127.0.0.1:8080"),
		NewBackend("b2", "127.0.0.1:8443"),
	}
	backends[1].Scheme = "https"

	var wg sync.WaitGroup
	for _, b := range backends {
		b := b
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				u := b.GetURL()
				if u == nil {
					t.Error("GetURL() returned nil")
				}
			}
		}()
	}
	wg.Wait()
}

// TestCov_ReleaseHealthyBackends_DoubleRelease verifies that
// ReleaseHealthyBackends does not panic when called (the sync.Pool handles it).
func TestCov_ReleaseHealthyBackends_DoubleRelease(t *testing.T) {
	p := NewPool("test", "roundrobin")
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateUp)
	p.AddBackend(b)

	healthy := p.GetHealthyBackends()
	if len(healthy) != 1 {
		t.Fatalf("GetHealthyBackends() = %d, want 1", len(healthy))
	}
	// Return to pool twice -- sync.Pool allows this without panicking.
	ReleaseHealthyBackends(healthy)
	ReleaseHealthyBackends(healthy)
}

// TestCov_DrainBackend_WithBalancer covers DrainBackend when a balancer is
// configured, exercising the balancer.Update path.
func TestCov_DrainBackend_WithBalancer(t *testing.T) {
	p := NewPool("test", "roundrobin")
	mb := &mockBalancer{name: "mock"}
	p.SetBalancer(mb)

	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateUp)
	p.AddBackend(b)

	if err := p.DrainBackend("b1"); err != nil {
		t.Fatalf("DrainBackend() error = %v", err)
	}
	if b.State() != StateDraining {
		t.Errorf("State after drain = %v, want %v", b.State(), StateDraining)
	}
}

// TestCov_RemoveBackend_WithBalancer covers RemoveBackend when a balancer is
// configured, exercising the balancer.Remove path.
func TestCov_RemoveBackend_WithBalancer(t *testing.T) {
	p := NewPool("test", "roundrobin")
	mb := &mockBalancer{name: "mock"}
	p.SetBalancer(mb)

	b := NewBackend("b1", "127.0.0.1:8080")
	p.AddBackend(b)

	if err := p.RemoveBackend("b1"); err != nil {
		t.Fatalf("RemoveBackend() error = %v", err)
	}
	if p.BackendCount() != 0 {
		t.Errorf("BackendCount() = %d, want 0", p.BackendCount())
	}
}

// TestCov_Stats_CoversAllFields ensures all fields in BackendStats are
// populated correctly via Stats().
func TestCov_Stats_CoversAllFields(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")
	b.AcquireConn()
	b.RecordRequest(50*time.Millisecond, 2048)
	b.RecordRequest(75*time.Millisecond, 4096)
	b.RecordError()

	s := b.Stats()
	if s.ActiveConns != 1 {
		t.Errorf("Stats.ActiveConns = %d, want 1", s.ActiveConns)
	}
	if s.TotalRequests != 2 {
		t.Errorf("Stats.TotalRequests = %d, want 2", s.TotalRequests)
	}
	if s.TotalErrors != 1 {
		t.Errorf("Stats.TotalErrors = %d, want 1", s.TotalErrors)
	}
	if s.TotalBytes != 6144 {
		t.Errorf("Stats.TotalBytes = %d, want 6144", s.TotalBytes)
	}
	if s.LastLatency != 75*time.Millisecond {
		t.Errorf("Stats.LastLatency = %v, want %v", s.LastLatency, 75*time.Millisecond)
	}
	if s.AvgLatency == 0 {
		t.Error("Stats.AvgLatency should not be zero after recording requests")
	}
}
