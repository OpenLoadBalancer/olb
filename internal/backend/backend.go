package backend

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/pkg/utils"
)

// Backend represents a single backend server.
type Backend struct {
	// ID is a unique identifier for this backend.
	ID string

	// Address is the network address (host:port) of the backend.
	Address string

	// Weight is the load balancing weight for this backend.
	Weight int32

	// MaxConns is the maximum number of concurrent connections.
	// 0 means unlimited.
	MaxConns int32

	// state is the current state of the backend.
	state *AtomicState

	// Connection statistics.
	activeConns   atomic.Int64
	totalConns    atomic.Int64
	totalRequests atomic.Int64
	totalErrors   atomic.Int64
	totalBytes    atomic.Int64

	// Latency tracking.
	avgLatency  *utils.AtomicDuration
	lastLatency *utils.AtomicDuration

	// Health check tracking.
	lastCheck      atomic.Value // time.Time
	checkFailCount atomic.Int32

	// Metadata for the backend.
	mu       sync.RWMutex
	metadata map[string]string
}

// NewBackend creates a new Backend with the given ID and address.
func NewBackend(id, address string) *Backend {
	b := &Backend{
		ID:       id,
		Address:  address,
		Weight:   1,
		state:    NewAtomicState(StateStarting),
		metadata: make(map[string]string),
	}
	b.avgLatency = utils.NewAtomicDuration(0)
	b.lastLatency = utils.NewAtomicDuration(0)
	return b
}

// State returns the current state of the backend.
func (b *Backend) State() State {
	return b.state.Load()
}

// SetState sets the backend state directly.
// Use with caution; prefer TransitionState for state machine compliance.
func (b *Backend) SetState(s State) {
	b.state.Store(s)
}

// TransitionState attempts to transition to a new state.
// Returns true if the transition was successful.
func (b *Backend) TransitionState(newState State) bool {
	return b.state.Transition(newState)
}

// IsAvailable returns true if the backend can accept new connections.
func (b *Backend) IsAvailable() bool {
	return b.state.Load().IsAvailable()
}

// IsHealthy returns true if the backend is in an active state.
func (b *Backend) IsHealthy() bool {
	return b.state.Load().IsActive()
}

// ActiveConns returns the number of active connections.
func (b *Backend) ActiveConns() int64 {
	return b.activeConns.Load()
}

// TotalConns returns the total number of connections.
func (b *Backend) TotalConns() int64 {
	return b.totalConns.Load()
}

// TotalRequests returns the total number of requests.
func (b *Backend) TotalRequests() int64 {
	return b.totalRequests.Load()
}

// TotalErrors returns the total number of errors.
func (b *Backend) TotalErrors() int64 {
	return b.totalErrors.Load()
}

// TotalBytes returns the total bytes transferred.
func (b *Backend) TotalBytes() int64 {
	return b.totalBytes.Load()
}

// AvgLatency returns the average latency.
func (b *Backend) AvgLatency() time.Duration {
	return b.avgLatency.Load()
}

// LastLatency returns the last recorded latency.
func (b *Backend) LastLatency() time.Duration {
	return b.lastLatency.Load()
}

// AcquireConn attempts to acquire a connection slot.
// Returns true if successful, false if at max connections.
// Uses atomic compare-and-swap to prevent race conditions.
func (b *Backend) AcquireConn() bool {
	if b.MaxConns > 0 {
		for {
			current := b.activeConns.Load()
			if current >= int64(b.MaxConns) {
				return false
			}
			if b.activeConns.CompareAndSwap(current, current+1) {
				b.totalConns.Add(1)
				return true
			}
			// CAS failed — another goroutine changed activeConns, retry
		}
	}
	b.activeConns.Add(1)
	b.totalConns.Add(1)
	return true
}

// ReleaseConn releases a connection slot.
func (b *Backend) ReleaseConn() {
	b.activeConns.Add(-1)
}

// RecordRequest records a completed request with latency.
func (b *Backend) RecordRequest(latency time.Duration, bytes int64) {
	b.totalRequests.Add(1)
	b.totalBytes.Add(bytes)
	b.lastLatency.Store(latency)

	// Update average latency using exponential moving average.
	currentAvg := b.avgLatency.Load()
	if currentAvg == 0 {
		b.avgLatency.Store(latency)
	} else {
		// EMA with alpha = 0.1
		newAvg := time.Duration(0.9*float64(currentAvg) + 0.1*float64(latency))
		b.avgLatency.Store(newAvg)
	}
}

// RecordError records an error.
func (b *Backend) RecordError() {
	b.totalErrors.Add(1)
}

// RecordHealthCheck records the result of a health check.
func (b *Backend) RecordHealthCheck(success bool) {
	b.lastCheck.Store(time.Now())
	if success {
		b.checkFailCount.Store(0)
	} else {
		b.checkFailCount.Add(1)
	}
}

// LastCheck returns the time of the last health check.
func (b *Backend) LastCheck() time.Time {
	v := b.lastCheck.Load()
	if v == nil {
		return time.Time{}
	}
	return v.(time.Time)
}

// CheckFailCount returns the number of consecutive failed checks.
func (b *Backend) CheckFailCount() int32 {
	return b.checkFailCount.Load()
}

// GetMetadata returns a metadata value.
func (b *Backend) GetMetadata(key string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.metadata[key]
}

// SetMetadata sets a metadata value.
func (b *Backend) SetMetadata(key, value string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.metadata[key] = value
}

// GetAllMetadata returns a copy of all metadata.
func (b *Backend) GetAllMetadata() map[string]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make(map[string]string, len(b.metadata))
	for k, v := range b.metadata {
		result[k] = v
	}
	return result
}

// Stats returns a snapshot of the backend statistics.
func (b *Backend) Stats() BackendStats {
	return BackendStats{
		ActiveConns:   b.activeConns.Load(),
		TotalRequests: b.totalRequests.Load(),
		TotalErrors:   b.totalErrors.Load(),
		TotalBytes:    b.totalBytes.Load(),
		AvgLatency:    b.avgLatency.Load(),
		LastLatency:   b.lastLatency.Load(),
	}
}

// Dial connects to the backend.
func (b *Backend) Dial(timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", b.Address, timeout)
}

// String returns a string representation of the backend.
func (b *Backend) String() string {
	return b.ID + "@" + b.Address
}
