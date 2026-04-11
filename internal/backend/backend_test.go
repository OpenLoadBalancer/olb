package backend

import (
	"net"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestNewBackend(t *testing.T) {
	b := NewBackend("backend1", "127.0.0.1:8080")

	if b.ID != "backend1" {
		t.Errorf("Backend.ID = %v, want %v", b.ID, "backend1")
	}
	if b.Address != "127.0.0.1:8080" {
		t.Errorf("Backend.Address = %v, want %v", b.Address, "127.0.0.1:8080")
	}
	if b.Weight != 1 {
		t.Errorf("Backend.Weight = %v, want %v", b.Weight, 1)
	}
	if b.State() != StateStarting {
		t.Errorf("Backend.State() = %v, want %v", b.State(), StateStarting)
	}
}

func TestBackendStateTransitions(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	// Starting -> Up
	if !b.TransitionState(StateUp) {
		t.Error("Transition(Starting -> Up) should succeed")
	}
	if b.State() != StateUp {
		t.Errorf("State = %v, want %v", b.State(), StateUp)
	}

	// Up -> Draining
	if !b.TransitionState(StateDraining) {
		t.Error("Transition(Up -> Draining) should succeed")
	}
	if b.State() != StateDraining {
		t.Errorf("State = %v, want %v", b.State(), StateDraining)
	}

	// Draining -> Down
	if !b.TransitionState(StateDown) {
		t.Error("Transition(Draining -> Down) should succeed")
	}
	if b.State() != StateDown {
		t.Errorf("State = %v, want %v", b.State(), StateDown)
	}

	// Down -> Up
	if !b.TransitionState(StateUp) {
		t.Error("Transition(Down -> Up) should succeed")
	}
	if b.State() != StateUp {
		t.Errorf("State = %v, want %v", b.State(), StateUp)
	}
}

func TestBackendIsAvailable(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	if b.IsAvailable() {
		t.Error("Starting backend should not be available")
	}

	b.SetState(StateUp)
	if !b.IsAvailable() {
		t.Error("Up backend should be available")
	}

	b.SetState(StateDraining)
	if b.IsAvailable() {
		t.Error("Draining backend should not be available")
	}
}

func TestBackendIsHealthy(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	if b.IsHealthy() {
		t.Error("Starting backend should not be healthy")
	}

	b.SetState(StateUp)
	if !b.IsHealthy() {
		t.Error("Up backend should be healthy")
	}

	b.SetState(StateDraining)
	if !b.IsHealthy() {
		t.Error("Draining backend should be healthy (active)")
	}

	b.SetState(StateMaintenance)
	if !b.IsHealthy() {
		t.Error("Maintenance backend should be healthy (active)")
	}

	b.SetState(StateDown)
	if b.IsHealthy() {
		t.Error("Down backend should not be healthy")
	}
}

func TestBackendConnectionTracking(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	// Acquire connections
	if !b.AcquireConn() {
		t.Error("AcquireConn should succeed")
	}
	if !b.AcquireConn() {
		t.Error("AcquireConn should succeed")
	}

	if b.ActiveConns() != 2 {
		t.Errorf("ActiveConns = %v, want %v", b.ActiveConns(), 2)
	}
	if b.TotalConns() != 2 {
		t.Errorf("TotalConns = %v, want %v", b.TotalConns(), 2)
	}

	// Release connections
	b.ReleaseConn()
	if b.ActiveConns() != 1 {
		t.Errorf("ActiveConns after release = %v, want %v", b.ActiveConns(), 1)
	}
	// TotalConns should not decrease
	if b.TotalConns() != 2 {
		t.Errorf("TotalConns after release = %v, want %v", b.TotalConns(), 2)
	}
}

func TestBackendMaxConns(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")
	b.MaxConns = 2

	if !b.AcquireConn() {
		t.Error("First AcquireConn should succeed")
	}
	if !b.AcquireConn() {
		t.Error("Second AcquireConn should succeed")
	}
	if b.AcquireConn() {
		t.Error("Third AcquireConn should fail (max reached)")
	}

	if b.ActiveConns() != 2 {
		t.Errorf("ActiveConns = %v, want %v", b.ActiveConns(), 2)
	}
}

func TestBackendRecordRequest(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	latency := 100 * time.Millisecond
	b.RecordRequest(latency, 1024)

	if b.TotalRequests() != 1 {
		t.Errorf("TotalRequests = %v, want %v", b.TotalRequests(), 1)
	}
	if b.TotalBytes() != 1024 {
		t.Errorf("TotalBytes = %v, want %v", b.TotalBytes(), 1024)
	}
	if b.LastLatency() != latency {
		t.Errorf("LastLatency = %v, want %v", b.LastLatency(), latency)
	}
	if b.AvgLatency() != latency {
		t.Errorf("AvgLatency = %v, want %v", b.AvgLatency(), latency)
	}

	// Record another request with different latency
	latency2 := 200 * time.Millisecond
	b.RecordRequest(latency2, 2048)

	if b.TotalRequests() != 2 {
		t.Errorf("TotalRequests = %v, want %v", b.TotalRequests(), 2)
	}
	if b.TotalBytes() != 3072 {
		t.Errorf("TotalBytes = %v, want %v", b.TotalBytes(), 3072)
	}
	if b.LastLatency() != latency2 {
		t.Errorf("LastLatency = %v, want %v", b.LastLatency(), latency2)
	}
	// Avg latency should be between 100ms and 200ms (EMA)
	if b.AvgLatency() <= latency || b.AvgLatency() >= latency2 {
		t.Errorf("AvgLatency = %v, should be between %v and %v", b.AvgLatency(), latency, latency2)
	}
}

func TestBackendRecordError(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	b.RecordError()
	b.RecordError()

	if b.TotalErrors() != 2 {
		t.Errorf("TotalErrors = %v, want %v", b.TotalErrors(), 2)
	}
}

func TestBackendHealthCheck(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	// Record successful check
	b.RecordHealthCheck(true)
	if b.CheckFailCount() != 0 {
		t.Errorf("CheckFailCount after success = %v, want %v", b.CheckFailCount(), 0)
	}
	if b.LastCheck().IsZero() {
		t.Error("LastCheck should be set")
	}

	// Record failed checks
	b.RecordHealthCheck(false)
	if b.CheckFailCount() != 1 {
		t.Errorf("CheckFailCount after 1 failure = %v, want %v", b.CheckFailCount(), 1)
	}

	b.RecordHealthCheck(false)
	if b.CheckFailCount() != 2 {
		t.Errorf("CheckFailCount after 2 failures = %v, want %v", b.CheckFailCount(), 2)
	}

	// Success resets counter
	b.RecordHealthCheck(true)
	if b.CheckFailCount() != 0 {
		t.Errorf("CheckFailCount after reset = %v, want %v", b.CheckFailCount(), 0)
	}
}

func TestBackendMetadata(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")

	b.SetMetadata("region", "us-east-1")
	b.SetMetadata("zone", "a")

	if got := b.GetMetadata("region"); got != "us-east-1" {
		t.Errorf("GetMetadata(region) = %v, want %v", got, "us-east-1")
	}
	if got := b.GetMetadata("zone"); got != "a" {
		t.Errorf("GetMetadata(zone) = %v, want %v", got, "a")
	}
	if got := b.GetMetadata("unknown"); got != "" {
		t.Errorf("GetMetadata(unknown) = %v, want empty", got)
	}

	all := b.GetAllMetadata()
	if len(all) != 2 {
		t.Errorf("GetAllMetadata length = %v, want %v", len(all), 2)
	}
	if all["region"] != "us-east-1" {
		t.Errorf("all[region] = %v, want %v", all["region"], "us-east-1")
	}
}

func TestBackendStats(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")
	b.AcquireConn()
	b.RecordRequest(100*time.Millisecond, 1024)
	b.RecordError()

	stats := b.Stats()

	if stats.ActiveConns != 1 {
		t.Errorf("Stats.ActiveConns = %v, want %v", stats.ActiveConns, 1)
	}
	if stats.TotalRequests != 1 {
		t.Errorf("Stats.TotalRequests = %v, want %v", stats.TotalRequests, 1)
	}
	if stats.TotalErrors != 1 {
		t.Errorf("Stats.TotalErrors = %v, want %v", stats.TotalErrors, 1)
	}
	if stats.TotalBytes != 1024 {
		t.Errorf("Stats.TotalBytes = %v, want %v", stats.TotalBytes, 1024)
	}
	if stats.LastLatency != 100*time.Millisecond {
		t.Errorf("Stats.LastLatency = %v, want %v", stats.LastLatency, 100*time.Millisecond)
	}
}

func TestBackendString(t *testing.T) {
	b := NewBackend("backend1", "127.0.0.1:8080")
	expected := "backend1@127.0.0.1:8080"
	if got := b.String(); got != expected {
		t.Errorf("Backend.String() = %v, want %v", got, expected)
	}
}

func TestBackendDial(t *testing.T) {
	// Start a real TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	b := NewBackend("test-dial", addr)

	// Accept connections in background
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	// Dial should succeed
	conn, err := b.Dial(2 * time.Second)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	conn.Close()
}

func TestBackendDial_ConnectionRefused(t *testing.T) {
	// Listen on a random port, then close it to guarantee connection refused
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	b := NewBackend("test-dial-fail", addr)

	_, err = b.Dial(100 * time.Millisecond)
	if err == nil {
		t.Error("Dial() should return error for refused connection")
	}
}

func TestBackendConcurrentAccess(t *testing.T) {
	b := NewBackend("b1", "127.0.0.1:8080")
	b.SetState(StateUp)

	var wg sync.WaitGroup
	numGoroutines := 100
	numOps := 100

	// Concurrent connection acquire/release
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if b.AcquireConn() {
					b.ReleaseConn()
				}
			}
		}()
	}

	// Concurrent request recording
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				b.RecordRequest(time.Millisecond, 100)
			}
		}()
	}

	// Concurrent state transitions
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if id%2 == 0 {
					b.TransitionState(StateUp)
				} else {
					b.TransitionState(StateDraining)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is valid
	state := b.State()
	if state != StateUp && state != StateDraining {
		t.Errorf("Final state = %v, want Up or Draining", state)
	}

	// Verify stats are consistent
	if b.TotalRequests() != int64(numGoroutines*numOps) {
		t.Errorf("TotalRequests = %v, want %v", b.TotalRequests(), numGoroutines*numOps)
	}

	// Active connections should be 0 (all released)
	if b.ActiveConns() != 0 {
		t.Errorf("ActiveConns = %v, want 0", b.ActiveConns())
	}
}

func TestBackendGetURL(t *testing.T) {
	b := NewBackend("backend1", "127.0.0.1:8080")

	u := b.GetURL()
	if u == nil {
		t.Fatal("GetURL() returned nil")
	}
	if u.Scheme != "http" {
		t.Errorf("GetURL().Scheme = %v, want http", u.Scheme)
	}
	if u.Host != "127.0.0.1:8080" {
		t.Errorf("GetURL().Host = %v, want 127.0.0.1:8080", u.Host)
	}
}

func TestBackendGetURL_Cached(t *testing.T) {
	b := NewBackend("backend1", "127.0.0.1:8080")

	// First call computes and caches the URL
	u1 := b.GetURL()

	// Second call should return the same cached pointer
	u2 := b.GetURL()
	if u1 != u2 {
		t.Error("GetURL() should return the same cached URL on subsequent calls")
	}
}

func TestBackendGetURL_Concurrent(t *testing.T) {
	b := NewBackend("backend1", "10.0.0.5:3000")

	var wg sync.WaitGroup
	results := make([]*url.URL, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = b.GetURL()
		}(i)
	}
	wg.Wait()

	// All results must be non-nil and have the same value
	// (may be different pointers due to concurrent Store in sync.Value)
	first := results[0]
	if first == nil {
		t.Fatal("first GetURL() result is nil")
	}
	for i, u := range results {
		if u == nil {
			t.Errorf("results[%d] is nil", i)
		}
		if u.String() != first.String() {
			t.Errorf("results[%d] = %q, want %q", i, u.String(), first.String())
		}
	}
}

func TestBackendGetURL_HTTPSScheme(t *testing.T) {
	b := NewBackend("backend1", "127.0.0.1:8443")
	b.Scheme = "https"

	u := b.GetURL()
	if u.Scheme != "https" {
		t.Errorf("GetURL().Scheme = %v, want https", u.Scheme)
	}
	if u.Host != "127.0.0.1:8443" {
		t.Errorf("GetURL().Host = %v, want 127.0.0.1:8443", u.Host)
	}
}

func TestBackendGetURL_DefaultScheme(t *testing.T) {
	b := NewBackend("backend1", "10.0.0.1:3000")
	// Scheme defaults to "http" from NewBackend

	u := b.GetURL()
	if u.Scheme != "http" {
		t.Errorf("GetURL().Scheme = %v, want http (default)", u.Scheme)
	}
}
