package balancer

import (
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestLeastResponseTime_Name(t *testing.T) {
	l := NewLeastResponseTime()
	if got := l.Name(); got != "least_response_time" {
		t.Errorf("Name() = %v, want %v", got, "least_response_time")
	}
}

func TestWeightedLeastResponseTime_Name(t *testing.T) {
	w := NewWeightedLeastResponseTime()
	if got := w.Name(); got != "weighted_least_response_time" {
		t.Errorf("Name() = %v, want %v", got, "weighted_least_response_time")
	}
}

func TestLeastResponseTime_Next_EmptyBackends(t *testing.T) {
	l := NewLeastResponseTime()
	if got := l.Next(nil, []*backend.Backend{}); got != nil {
		t.Errorf("Next() with empty backends = %v, want nil", got)
	}
}

func TestWeightedLeastResponseTime_Next_EmptyBackends(t *testing.T) {
	w := NewWeightedLeastResponseTime()
	if got := w.Next(nil, []*backend.Backend{}); got != nil {
		t.Errorf("Next() with empty backends = %v, want nil", got)
	}
}

func TestLeastResponseTime_Next_SingleBackend(t *testing.T) {
	l := NewLeastResponseTime()
	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)
	l.Add(be)

	backends := []*backend.Backend{be}
	got := l.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil, want backend")
	}
	if got.ID != be.ID {
		t.Errorf("Next() = %v, want %v", got.ID, be.ID)
	}
}

func TestLeastResponseTime_Next_SelectsLowestResponseTime(t *testing.T) {
	l := NewLeastResponseTimeWithWindow(10)

	be1 := backend.NewBackend("fast", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be2 := backend.NewBackend("slow", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)

	l.Add(be1)
	l.Add(be2)

	// Record response times: backend1 is fast (10ms), backend2 is slow (100ms)
	for i := 0; i < 5; i++ {
		l.Record("fast", 10*time.Millisecond)
		l.Record("slow", 100*time.Millisecond)
	}

	// Should select the fast backend
	backends := []*backend.Backend{be1, be2}
	got := l.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil")
	}
	if got.ID != "fast" {
		t.Errorf("Next() selected %v, want fast (lowest response time)", got.ID)
	}
}

func TestLeastResponseTime_Next_SkipsUnhealthy(t *testing.T) {
	l := NewLeastResponseTime()

	healthy := backend.NewBackend("healthy", "10.0.0.1:8080")
	healthy.SetState(backend.StateUp)
	unhealthy := backend.NewBackend("unhealthy", "10.0.0.2:8080")
	unhealthy.SetState(backend.StateDown)

	l.Add(healthy)
	l.Add(unhealthy)

	// Should always select the healthy backend
	backends := []*backend.Backend{healthy, unhealthy}
	for i := 0; i < 10; i++ {
		got := l.Next(nil, backends)
		if got == nil {
			t.Fatal("Next() returned nil")
		}
		if got.ID != "healthy" {
			t.Errorf("Next() selected %v, want healthy", got.ID)
		}
	}
}

func TestLeastResponseTime_Next_AllUnhealthy(t *testing.T) {
	l := NewLeastResponseTime()

	be := backend.NewBackend("unhealthy", "10.0.0.1:8080")
	be.SetState(backend.StateDown)
	l.Add(be)

	backends := []*backend.Backend{be}
	if got := l.Next(nil, backends); got != nil {
		t.Errorf("Next() with all unhealthy = %v, want nil", got)
	}
}

func TestLeastResponseTime_ResponseTimeTracking(t *testing.T) {
	l := NewLeastResponseTimeWithWindow(5)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)
	l.Add(be)

	// Record some response times
	l.Record("backend-1", 10*time.Millisecond)
	l.Record("backend-1", 20*time.Millisecond)
	l.Record("backend-1", 30*time.Millisecond)

	// Check internal state
	bs := l.backends["backend-1"]
	if bs.count.Load() != 3 {
		t.Errorf("count = %d, want 3", bs.count.Load())
	}

	expectedAvg := (10 + 20 + 30) * time.Millisecond / 3
	if avg := bs.average(); avg != expectedAvg {
		t.Errorf("average = %v, want %v", avg, expectedAvg)
	}
}

func TestLeastResponseTime_ResponseTimeWindow(t *testing.T) {
	l := NewLeastResponseTimeWithWindow(3)

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetState(backend.StateUp)
	l.Add(be)

	// Record more samples than window size
	l.Record("backend-1", 10*time.Millisecond)
	l.Record("backend-1", 20*time.Millisecond)
	l.Record("backend-1", 30*time.Millisecond)
	l.Record("backend-1", 40*time.Millisecond) // Should overwrite first

	// Wait a bit for atomic operations to settle
	time.Sleep(10 * time.Millisecond)

	bs := l.backends["backend-1"]
	if bs.count.Load() != 3 {
		t.Errorf("count = %d, want 3 (window size)", bs.count.Load())
	}

	// Average should be of last 3: 20, 30, 40
	expectedAvg := (20 + 30 + 40) * time.Millisecond / 3
	if avg := bs.average(); avg != expectedAvg {
		t.Errorf("average = %v, want %v", avg, expectedAvg)
	}
}

func TestWeightedLeastResponseTime_Next_WeightedSelection(t *testing.T) {
	w := NewWeightedLeastResponseTimeWithWindow(10)

	// Backend with same response time but higher weight should be preferred
	be1 := backend.NewBackend("light", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be1.SetWeight(1)
	be2 := backend.NewBackend("heavy", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)
	be2.SetWeight(5)

	w.Add(be1)
	w.Add(be2)

	// Record same response times for both
	for i := 0; i < 5; i++ {
		w.Record("light", 50*time.Millisecond)
		w.Record("heavy", 50*time.Millisecond)
	}

	// Heavy has weight 5, so effective time = 50/5 = 10ms
	// Light has weight 1, so effective time = 50/1 = 50ms
	// Should select heavy
	backends := []*backend.Backend{be1, be2}
	got := w.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil")
	}
	if got.ID != "heavy" {
		t.Errorf("Next() selected %v, want heavy (better weighted response time)", got.ID)
	}
}

func TestWeightedLeastResponseTime_ZeroWeight(t *testing.T) {
	w := NewWeightedLeastResponseTime()

	be := backend.NewBackend("zero-weight", "10.0.0.1:8080")
	be.SetState(backend.StateUp)
	be.SetWeight(0)
	w.Add(be)

	w.Record("zero-weight", 100*time.Millisecond)

	bs := w.backends["zero-weight"]
	// With weight 0, should treat as weight 1
	expected := 100 * time.Millisecond
	if got := bs.weightedAverage(); got != expected {
		t.Errorf("weightedAverage() with zero weight = %v, want %v", got, expected)
	}
}

func TestLeastResponseTime_Add(t *testing.T) {
	l := NewLeastResponseTime()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	l.Add(be)

	if _, ok := l.backends["backend-1"]; !ok {
		t.Error("Add() did not add backend to backends map")
	}
}

func TestLeastResponseTime_Remove(t *testing.T) {
	l := NewLeastResponseTime()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	l.Add(be)
	l.Remove("backend-1")

	if _, ok := l.backends["backend-1"]; ok {
		t.Error("Remove() did not remove backend from backends map")
	}
}

func TestLeastResponseTime_Update(t *testing.T) {
	l := NewLeastResponseTime()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetWeight(1)
	l.Add(be)

	updated := backend.NewBackend("backend-1", "10.0.0.1:8080")
	updated.SetWeight(5)
	l.Update(updated)

	bs := l.backends["backend-1"]
	if bs.backend.GetWeight() != 5 {
		t.Errorf("Update() did not update weight: got %d, want 5", bs.backend.GetWeight())
	}
}

func TestLeastResponseTime_UpdateNonExistent(t *testing.T) {
	l := NewLeastResponseTime()

	// Should not panic
	l.Update(backend.NewBackend("non-existent", "10.0.0.1:8080"))
}

func TestLeastResponseTime_RecordNonExistent(t *testing.T) {
	l := NewLeastResponseTime()

	// Should not panic
	l.Record("non-existent", 10*time.Millisecond)
}

func TestLeastResponseTime_ConcurrentAccess(t *testing.T) {
	l := NewLeastResponseTimeWithWindow(100)

	// Create backends
	backends := make([]*backend.Backend, 5)
	for i := 0; i < 5; i++ {
		be := backend.NewBackend(string(rune('a'+i)), "10.0.0.1:8080")
		be.SetState(backend.StateUp)
		backends[i] = be
		l.Add(be)
	}

	var wg sync.WaitGroup

	// Concurrent Next() calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Next(nil, backends)
		}()
	}

	// Concurrent Record() calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			backendID := string(rune('a' + (i % 5)))
			l.Record(backendID, time.Duration(i)*time.Millisecond)
		}(i)
	}

	// Concurrent Add/Remove
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%5))
			l.Remove(id)
			be := backend.NewBackend(id, "10.0.0.1:8080")
			be.SetState(backend.StateUp)
			l.Add(be)
		}(i)
	}

	wg.Wait()
}

func TestWeightedLeastResponseTime_ConcurrentAccess(t *testing.T) {
	w := NewWeightedLeastResponseTimeWithWindow(100)

	// Create backends
	backends := make([]*backend.Backend, 5)
	for i := 0; i < 5; i++ {
		be := backend.NewBackend(string(rune('a'+i)), "10.0.0.1:8080")
		be.SetState(backend.StateUp)
		be.SetWeight(int32(i + 1))
		backends[i] = be
		w.Add(be)
	}

	var wg sync.WaitGroup

	// Concurrent Next() calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Next(nil, backends)
		}()
	}

	// Concurrent Record() calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			backendID := string(rune('a' + (i % 5)))
			w.Record(backendID, time.Duration(i)*time.Millisecond)
		}(i)
	}

	wg.Wait()
}

func TestLeastResponseTime_NoSamples_SelectsFirst(t *testing.T) {
	l := NewLeastResponseTime()

	be1 := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be2 := backend.NewBackend("backend-2", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)

	l.Add(be1)
	l.Add(be2)

	// No response times recorded yet, both have 0 average
	// Should select one (first encountered with 0 average)
	backends := []*backend.Backend{be1, be2}
	got := l.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil when backends exist")
	}
	if got.ID != "backend-1" && got.ID != "backend-2" {
		t.Errorf("Next() returned unexpected backend: %v", got.ID)
	}
}

func TestLeastResponseTime_BackendState_average_Empty(t *testing.T) {
	be := backend.NewBackend("test", "10.0.0.1:8080")
	bs := newLRTBackendState(be, 10)

	// No samples recorded yet
	if avg := bs.average(); avg != 0 {
		t.Errorf("average() with no samples = %v, want 0", avg)
	}
}

func TestLeastResponseTime_DefaultWindowSize(t *testing.T) {
	l := NewLeastResponseTime()
	if l.windowSize != DefaultResponseTimeWindowSize {
		t.Errorf("default window size = %d, want %d", l.windowSize, DefaultResponseTimeWindowSize)
	}
}

func TestWeightedLeastResponseTime_Remove(t *testing.T) {
	w := NewWeightedLeastResponseTime()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	w.Add(be)

	// Verify backend was added
	if _, ok := w.backends["backend-1"]; !ok {
		t.Fatal("Add() did not add backend to backends map")
	}

	w.Remove("backend-1")

	if _, ok := w.backends["backend-1"]; ok {
		t.Error("Remove() did not remove backend from backends map")
	}

	// Remove non-existent should not panic
	w.Remove("nonexistent")
}

func TestWeightedLeastResponseTime_Update(t *testing.T) {
	w := NewWeightedLeastResponseTime()

	be := backend.NewBackend("backend-1", "10.0.0.1:8080")
	be.SetWeight(1)
	w.Add(be)

	updated := backend.NewBackend("backend-1", "10.0.0.1:8080")
	updated.SetWeight(10)
	w.Update(updated)

	bs := w.backends["backend-1"]
	if bs.backend.GetWeight() != 10 {
		t.Errorf("Update() did not update weight: got %d, want 10", bs.backend.GetWeight())
	}

	// Update non-existent should not panic
	w.Update(backend.NewBackend("nonexistent", "10.0.0.1:8080"))
}

func TestLeastResponseTime_InvalidWindowSize(t *testing.T) {
	l := NewLeastResponseTimeWithWindow(0)
	if l.windowSize != DefaultResponseTimeWindowSize {
		t.Errorf("window size with 0 input = %d, want %d", l.windowSize, DefaultResponseTimeWindowSize)
	}

	l = NewLeastResponseTimeWithWindow(-5)
	if l.windowSize != DefaultResponseTimeWindowSize {
		t.Errorf("window size with negative input = %d, want %d", l.windowSize, DefaultResponseTimeWindowSize)
	}
}

func TestWeightedLeastResponseTime_InvalidWindowSize(t *testing.T) {
	w := NewWeightedLeastResponseTimeWithWindow(0)
	if w.windowSize != DefaultResponseTimeWindowSize {
		t.Errorf("window size with 0 input = %d, want %d", w.windowSize, DefaultResponseTimeWindowSize)
	}

	w = NewWeightedLeastResponseTimeWithWindow(-5)
	if w.windowSize != DefaultResponseTimeWindowSize {
		t.Errorf("window size with negative input = %d, want %d", w.windowSize, DefaultResponseTimeWindowSize)
	}
}

// TestLeastResponseTime_Next_MixedTrackedUntracked tests the path where
// tracked backends with higher avg are compared against untracked backends.
func TestLeastResponseTime_Next_MixedTrackedUntracked(t *testing.T) {
	l := NewLeastResponseTime()

	// be1 is tracked with recorded response times
	be1 := backend.NewBackend("tracked", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	l.Add(be1)
	l.Record("tracked", 200*time.Millisecond)
	l.Record("tracked", 300*time.Millisecond)

	// be2 is NOT tracked (no Add/Record) - should be treated as 0 (best choice)
	be2 := backend.NewBackend("untracked", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)

	backends := []*backend.Backend{be1, be2}
	got := l.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil")
	}
	if got.ID != "untracked" {
		t.Errorf("Next() = %v, want untracked (0 avg vs high avg)", got.ID)
	}
}

// TestWeightedLeastResponseTime_Next_MixedTrackedUntracked tests the weighted version
// with a mix of tracked and untracked backends.
func TestWeightedLeastResponseTime_Next_MixedTrackedUntracked(t *testing.T) {
	w := NewWeightedLeastResponseTime()

	be1 := backend.NewBackend("tracked", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be1.SetWeight(1)
	w.Add(be1)
	w.Record("tracked", 200*time.Millisecond)
	w.Record("tracked", 300*time.Millisecond)

	be2 := backend.NewBackend("untracked", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)
	be2.SetWeight(1)

	backends := []*backend.Backend{be1, be2}
	got := w.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil")
	}
	if got.ID != "untracked" {
		t.Errorf("Next() = %v, want untracked (0 weighted avg vs high)", got.ID)
	}
}

// TestLeastResponseTime_Next_TrackedBetterThanUntracked tests when tracked
// backend has lower avg than the untracked one's effective 0.
func TestLeastResponseTime_Next_TrackedWithZeroSamples(t *testing.T) {
	l := NewLeastResponseTime()

	// be1 is tracked but has 0 samples (count == 0)
	be1 := backend.NewBackend("tracked-empty", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	l.Add(be1)
	// Don't record anything - count stays 0

	// be2 is NOT tracked at all
	be2 := backend.NewBackend("untracked", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)

	backends := []*backend.Backend{be1, be2}
	got := l.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil")
	}
	// Both should have 0 avg, first one wins
	if got.ID != "tracked-empty" {
		t.Errorf("Next() = %v, want tracked-empty (both 0, first wins)", got.ID)
	}
}

// TestWeightedLeastResponseTime_Next_AllUnhealthy tests WLRT with all unhealthy.
func TestWeightedLeastResponseTime_Next_AllUnhealthy(t *testing.T) {
	w := NewWeightedLeastResponseTime()

	be := backend.NewBackend("unhealthy", "10.0.0.1:8080")
	be.SetState(backend.StateDown)
	w.Add(be)

	backends := []*backend.Backend{be}
	if got := w.Next(nil, backends); got != nil {
		t.Errorf("Next() with all unhealthy = %v, want nil", got)
	}
}

// TestLeastResponseTime_record_NewSlot tests record path where count < windowSize
func TestLeastResponseTime_record_NewSlot(t *testing.T) {
	be := backend.NewBackend("test", "10.0.0.1:8080")
	bs := newLRTBackendState(be, 10)

	// First record goes to a new slot (count goes from 0 to 1)
	bs.record(50 * time.Millisecond)
	if bs.count.Load() != 1 {
		t.Errorf("count = %d, want 1", bs.count.Load())
	}
	if bs.total.Load() != int64(50*time.Millisecond) {
		t.Errorf("total = %d, want %d", bs.total.Load(), int64(50*time.Millisecond))
	}

	// Second record also goes to a new slot
	bs.record(100 * time.Millisecond)
	if bs.count.Load() != 2 {
		t.Errorf("count = %d, want 2", bs.count.Load())
	}
}

// TestLeastResponseTime_record_HighContention exercises the CAS retry loops in record.
func TestLeastResponseTime_record_HighContention(t *testing.T) {
	be := backend.NewBackend("test", "10.0.0.1:8080")
	bs := newLRTBackendState(be, 100)

	var wg sync.WaitGroup
	numGoroutines := 50
	recordsPerGoroutine := 200

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < recordsPerGoroutine; j++ {
				bs.record(time.Duration(i*1000+j) * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()

	// Count should be capped at windowSize
	count := bs.count.Load()
	if count != 100 {
		t.Errorf("count = %d, want 100 (window size)", count)
	}
}

// TestWeightedLeastResponseTime_Next_SelectsLowestWeighted tests weighted selection.
func TestWeightedLeastResponseTime_Next_SelectsLowestWeighted(t *testing.T) {
	w := NewWeightedLeastResponseTimeWithWindow(10)

	be1 := backend.NewBackend("light", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	be1.SetWeight(1)

	be2 := backend.NewBackend("heavy", "10.0.0.2:8080")
	be2.SetState(backend.StateUp)
	be2.SetWeight(10)

	w.Add(be1)
	w.Add(be2)

	// Record different response times
	for i := 0; i < 5; i++ {
		w.Record("light", 50*time.Millisecond)
		w.Record("heavy", 200*time.Millisecond)
	}

	backends := []*backend.Backend{be1, be2}
	got := w.Next(nil, backends)
	if got == nil {
		t.Fatal("Next() returned nil")
	}

	// heavy effective = 200/10 = 20ms, light effective = 50/1 = 50ms
	// heavy should be selected (lower effective)
	if got.ID != "heavy" {
		t.Errorf("Next() = %v, want heavy (lower weighted avg)", got.ID)
	}
}

// TestLeastResponseTime_Record_HighContention tests concurrent Record calls.
func TestLeastResponseTime_Record_HighContention(t *testing.T) {
	l := NewLeastResponseTimeWithWindow(50)

	be1 := backend.NewBackend("be1", "10.0.0.1:8080")
	be1.SetState(backend.StateUp)
	l.Add(be1)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				l.Record("be1", time.Duration(i*j)*time.Microsecond)
			}
		}(i)
	}
	wg.Wait()
}
