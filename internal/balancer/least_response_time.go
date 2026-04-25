package balancer

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// DefaultWindowSize is the default number of response time samples to keep per backend.
const DefaultResponseTimeWindowSize = 100

// backendState holds the state for a single backend including response time tracking.
type lrtBackendState struct {
	backend *backend.Backend
	// samples is a circular buffer of recent response times
	samples []atomic.Int64
	// writePos is the next position to write in the circular buffer
	writePos atomic.Uint32
	// count is the number of valid samples in the buffer
	count atomic.Uint32
	// total is the sum of all samples for fast average calculation
	total atomic.Int64
}

// newLRTBackendState creates a new backendState for the given backend.
func newLRTBackendState(backend *backend.Backend, windowSize int) *lrtBackendState {
	return &lrtBackendState{
		backend: backend,
		samples: make([]atomic.Int64, windowSize),
	}
}

// record records a new response time sample.
func (bs *lrtBackendState) record(d time.Duration) {
	windowSize := uint32(len(bs.samples))

	// Get current position and calculate new position
	pos := bs.writePos.Load()
	nextPos := (pos + 1) % windowSize

	// Try to CAS until successful
	for !bs.writePos.CompareAndSwap(pos, nextPos) {
		pos = bs.writePos.Load()
		nextPos = (pos + 1) % windowSize
	}

	// Now we own position 'pos'
	oldSample := bs.samples[pos].Swap(int64(d))

	// Update count (cap at windowSize)
	for {
		oldCount := bs.count.Load()
		newCount := oldCount
		if oldCount < windowSize {
			newCount = oldCount + 1
		}
		if bs.count.CompareAndSwap(oldCount, newCount) {
			break
		}
	}

	// Update total: add new sample, subtract old sample if we're overwriting
	if bs.count.Load() == windowSize {
		// We're overwriting, subtract the old value
		bs.total.Add(int64(d) - oldSample)
	} else {
		// New slot, just add
		bs.total.Add(int64(d))
	}
}

// average returns the average response time for this backend.
func (bs *lrtBackendState) average() time.Duration {
	count := bs.count.Load()
	if count == 0 {
		return 0
	}
	total := bs.total.Load()
	return time.Duration(total / int64(count))
}

// weightedAverage returns the effective response time considering weight.
// For weighted least response time: effective_time = avg_response_time / weight
func (bs *lrtBackendState) weightedAverage() time.Duration {
	avg := bs.average()
	weight := bs.backend.GetWeight()
	if weight <= 0 {
		weight = 1
	}
	return time.Duration(int64(avg) / int64(weight))
}

// LeastResponseTime implements the least response time load balancing algorithm.
// It tracks recent response times for each backend and selects the one with
// the lowest average response time.
type LeastResponseTime struct {
	mu         sync.RWMutex
	backends   map[string]*lrtBackendState
	windowSize int
}

// NewLeastResponseTime creates a new LeastResponseTime balancer.
func NewLeastResponseTime() *LeastResponseTime {
	return NewLeastResponseTimeWithWindow(DefaultResponseTimeWindowSize)
}

// NewLeastResponseTimeWithWindow creates a new LeastResponseTime balancer with a custom window size.
func NewLeastResponseTimeWithWindow(windowSize int) *LeastResponseTime {
	if windowSize <= 0 {
		windowSize = DefaultResponseTimeWindowSize
	}
	return &LeastResponseTime{
		backends:   make(map[string]*lrtBackendState),
		windowSize: windowSize,
	}
}

// Name returns the name of the balancer.
func (l *LeastResponseTime) Name() string {
	return "least_response_time"
}

// Next returns the backend with the lowest average response time.
func (l *LeastResponseTime) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(backends) == 0 {
		return nil
	}

	var selected *backend.Backend
	var minAvg time.Duration = -1 // Max duration

	for _, be := range backends {
		// Skip unhealthy backends
		if !be.State().IsAvailable() {
			continue
		}

		// Get or create state for this backend
		state, ok := l.backends[be.ID]
		if !ok {
			// Backend not tracked yet, treat as 0 (best choice)
			if minAvg == -1 || 0 < minAvg {
				minAvg = 0
				selected = be
			}
			continue
		}

		avg := state.average()
		// If no samples yet, treat as 0 (best choice)
		if state.count.Load() == 0 {
			avg = 0
		}

		if minAvg == -1 || avg < minAvg {
			minAvg = avg
			selected = be
		}
	}

	return selected
}

// Add adds a backend to the balancer.
func (l *LeastResponseTime) Add(be *backend.Backend) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.backends[be.ID] = newLRTBackendState(be, l.windowSize)
}

// Remove removes a backend from the balancer.
func (l *LeastResponseTime) Remove(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.backends, id)
}

// Update updates a backend's properties.
func (l *LeastResponseTime) Update(be *backend.Backend) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if state, ok := l.backends[be.ID]; ok {
		state.backend = be
	}
}

// Record records a response time for the given backend.
// This should be called after each request completes.
func (l *LeastResponseTime) Record(backendID string, d time.Duration) {
	l.mu.RLock()
	state, ok := l.backends[backendID]
	l.mu.RUnlock()

	if ok {
		state.record(d)
	}
}

// WeightedLeastResponseTime implements the weighted least response time
// load balancing algorithm. Backends with higher weights are preferred,
// and among backends with equal effective response times, higher weight wins.
type WeightedLeastResponseTime struct {
	mu         sync.RWMutex
	backends   map[string]*lrtBackendState
	windowSize int
}

// NewWeightedLeastResponseTime creates a new WeightedLeastResponseTime balancer.
func NewWeightedLeastResponseTime() *WeightedLeastResponseTime {
	return NewWeightedLeastResponseTimeWithWindow(DefaultResponseTimeWindowSize)
}

// NewWeightedLeastResponseTimeWithWindow creates a new WeightedLeastResponseTime balancer with a custom window size.
func NewWeightedLeastResponseTimeWithWindow(windowSize int) *WeightedLeastResponseTime {
	if windowSize <= 0 {
		windowSize = DefaultResponseTimeWindowSize
	}
	return &WeightedLeastResponseTime{
		backends:   make(map[string]*lrtBackendState),
		windowSize: windowSize,
	}
}

// Name returns the name of the balancer.
func (w *WeightedLeastResponseTime) Name() string {
	return "weighted_least_response_time"
}

// Next returns the backend with the lowest weighted average response time.
func (w *WeightedLeastResponseTime) Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(backends) == 0 {
		return nil
	}

	var selected *backend.Backend
	var minWeighted time.Duration = -1 // Max duration

	for _, be := range backends {
		// Skip unhealthy backends
		if !be.State().IsAvailable() {
			continue
		}

		state, ok := w.backends[be.ID]
		if !ok {
			// Backend not tracked yet, treat as 0 (best choice)
			if minWeighted == -1 || 0 < minWeighted {
				minWeighted = 0
				selected = be
			}
			continue
		}

		weighted := state.weightedAverage()
		// If no samples yet, treat as 0 (best choice)
		if state.count.Load() == 0 {
			weighted = 0
		}

		if minWeighted == -1 || weighted < minWeighted {
			minWeighted = weighted
			selected = be
		}
	}

	return selected
}

// Add adds a backend to the balancer.
func (w *WeightedLeastResponseTime) Add(be *backend.Backend) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.backends[be.ID] = newLRTBackendState(be, w.windowSize)
}

// Remove removes a backend from the balancer.
func (w *WeightedLeastResponseTime) Remove(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.backends, id)
}

// Update updates a backend's properties.
func (w *WeightedLeastResponseTime) Update(be *backend.Backend) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if state, ok := w.backends[be.ID]; ok {
		state.backend = be
	}
}

// Record records a response time for the given backend.
// This should be called after each request completes.
func (w *WeightedLeastResponseTime) Record(backendID string, d time.Duration) {
	w.mu.RLock()
	state, ok := w.backends[backendID]
	w.mu.RUnlock()

	if ok {
		state.record(d)
	}
}
