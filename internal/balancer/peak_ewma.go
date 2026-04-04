package balancer

import (
	"math"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// PeakEWMA implements the Peak Exponentially Weighted Moving Average algorithm.
// It tracks peak response times using an exponentially weighted moving average
// and routes to the backend with the lowest peak latency.
// This is particularly effective for handling variable load and backend performance.
type PeakEWMA struct {
	mu       sync.RWMutex
	decay    float64        // Decay factor for EWMA calculation
	samples  map[string]*peakEWMASample // Per-backend samples
	lastTick time.Time
}

// peakEWMASample tracks EWMA statistics for a single backend
type peakEWMASample struct {
	peakLatency    float64   // Current peak EWMA latency in nanoseconds
	requestCount   uint64    // Total requests
	errorCount     uint64    // Failed requests
	lastUpdate     time.Time // Last update time
}

// NewPeakEWMA creates a new Peak EWMA balancer.
// The decay parameter controls how quickly old samples lose influence.
// A typical value is 10 seconds (half-life for peak latency tracking).
func NewPeakEWMA() *PeakEWMA {
	return &PeakEWMA{
		decay:    10.0, // 10 second half-life
		samples:  make(map[string]*peakEWMASample),
		lastTick: time.Now(),
	}
}

// Name returns the name of the balancer.
func (p *PeakEWMA) Name() string {
	return "peak_ewma"
}

// Next selects the backend with the lowest peak EWMA latency.
// Backends with errors are penalized with higher effective latency.
func (p *PeakEWMA) Next(backends []*backend.Backend) *backend.Backend {
	if len(backends) == 0 {
		return nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var best *backend.Backend
	bestScore := math.MaxFloat64

	now := time.Now()

	for _, be := range backends {
		if be.State() != backend.StateUp {
			continue
		}

		score := p.score(be.ID, now)
		if score < bestScore {
			bestScore = score
			best = be
		}
	}

	// If no healthy backends, fall back to first backend
	if best == nil && len(backends) > 0 {
		return backends[0]
	}

	return best
}

// score calculates the effective score for a backend.
// Lower score = better backend.
func (p *PeakEWMA) score(backendID string, now time.Time) float64 {
	sample, exists := p.samples[backendID]
	if !exists {
		// New backend - give it a moderate initial score
		return 1e6 // 1ms in nanoseconds
	}

	// Calculate decay factor based on time elapsed
	elapsed := now.Sub(sample.lastUpdate).Seconds()
	decayMultiplier := math.Exp(-elapsed / p.decay)

	// Decay the peak latency
	peakLatency := sample.peakLatency * decayMultiplier

	// Penalize backends with errors
	errorRate := float64(0)
	if sample.requestCount > 0 {
		errorRate = float64(sample.errorCount) / float64(sample.requestCount)
	}

	// Effective score = peak latency * (1 + error penalty)
	// Error rate of 10% adds 100% penalty
	errorPenalty := 1.0 + (errorRate * 10.0)
	effectiveScore := peakLatency * errorPenalty

	return effectiveScore
}

// Record records request latency for a backend.
// This should be called after each request to update the statistics.
func (p *PeakEWMA) Record(backendID string, latency time.Duration, success bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sample, exists := p.samples[backendID]
	if !exists {
		sample = &peakEWMASample{
			peakLatency: float64(latency),
			lastUpdate:  time.Now(),
		}
		p.samples[backendID] = sample
	}

	now := time.Now()
	elapsed := now.Sub(sample.lastUpdate).Seconds()
	decayMultiplier := math.Exp(-elapsed / p.decay)

	// Decay old peak
	oldPeak := sample.peakLatency * decayMultiplier

	// Update peak: if new latency is higher, use it directly
	// Otherwise, decay the old peak slightly
	newLatency := float64(latency)
	if newLatency > oldPeak {
		sample.peakLatency = newLatency
	} else {
		// Slowly decay the peak even if no new peak
		sample.peakLatency = oldPeak * 0.95
	}

	sample.requestCount++
	if !success {
		sample.errorCount++
	}
	sample.lastUpdate = now
}

// Add registers a new backend.
func (p *PeakEWMA) Add(be *backend.Backend) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.samples[be.ID]; !exists {
		p.samples[be.ID] = &peakEWMASample{
			peakLatency: 1e6, // 1ms initial
			lastUpdate:  time.Now(),
		}
	}
}

// Remove unregisters a backend.
func (p *PeakEWMA) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.samples, id)
}

// Update is called when a backend is updated.
func (p *PeakEWMA) Update(be *backend.Backend) {
	// No specific action needed for Peak EWMA
}

// Stats returns current statistics for all backends.
func (p *PeakEWMA) Stats() map[string]map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]map[string]interface{})
	now := time.Now()

	for id, sample := range p.samples {
		elapsed := now.Sub(sample.lastUpdate).Seconds()
		decayMultiplier := math.Exp(-elapsed / p.decay)

		errRate := float64(0)
		if sample.requestCount > 0 {
			errRate = float64(sample.errorCount) / float64(sample.requestCount)
		}

		stats[id] = map[string]interface{}{
			"peak_latency_ms": sample.peakLatency * decayMultiplier / 1e6,
			"request_count":   sample.requestCount,
			"error_count":     sample.errorCount,
			"error_rate":      errRate,
		}
	}

	return stats
}
