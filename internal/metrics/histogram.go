package metrics

import (
	"math"
	"sync/atomic"
)

// Histogram is a log-linear bucketed histogram.
type Histogram struct {
	buckets []atomic.Int64 // bucket counts
	bounds  []float64     // bucket upper bounds
	sum     atomic.Uint64 // bits of float64
	count   atomic.Int64
	name    string
	help    string
}

// Default buckets for histogram (log-linear scale).
// Covers 0.001ms to 10000ms (10 seconds).
var DefaultBuckets = []float64{
	0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
	0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000,
}

// NewHistogram creates a new histogram with default buckets.
func NewHistogram(name, help string) *Histogram {
	return NewHistogramWithBuckets(name, help, DefaultBuckets)
}

// NewHistogramWithBuckets creates a new histogram with custom buckets.
func NewHistogramWithBuckets(name, help string, buckets []float64) *Histogram {
	h := &Histogram{
		name:    name,
		help:    help,
		bounds:  buckets,
		buckets: make([]atomic.Int64, len(buckets)+1), // +1 for +Inf bucket
	}
	return h
}

// Name returns the histogram name.
func (h *Histogram) Name() string {
	return h.name
}

// Help returns the histogram help text.
func (h *Histogram) Help() string {
	return h.help
}

// Observe records a value in the histogram.
func (h *Histogram) Observe(v float64) {
	// Find the bucket
	for i, bound := range h.bounds {
		if v <= bound {
			h.buckets[i].Add(1)
			break
		}
		if i == len(h.bounds)-1 {
			// +Inf bucket
			h.buckets[len(h.bounds)].Add(1)
		}
	}

	// Update sum and count
	for {
		old := h.sum.Load()
		newSum := math.Float64frombits(old) + v
		if h.sum.CompareAndSwap(old, math.Float64bits(newSum)) {
			break
		}
	}
	h.count.Add(1)
}

// GetCount returns the total count of observations.
func (h *Histogram) GetCount() int64 {
	return h.count.Load()
}

// GetSum returns the sum of all observations.
func (h *Histogram) GetSum() float64 {
	return math.Float64frombits(h.sum.Load())
}

// GetBucketCount returns the count for a specific bucket.
func (h *Histogram) GetBucketCount(bucketIdx int) int64 {
	if bucketIdx < 0 || bucketIdx >= len(h.buckets) {
		return 0
	}
	return h.buckets[bucketIdx].Load()
}

// Percentile returns the estimated value at the given percentile (0-100).
func (h *Histogram) Percentile(p float64) float64 {
	if p < 0 || p > 100 {
		return 0
	}

	count := h.GetCount()
	if count == 0 {
		return 0
	}

	target := int64(math.Ceil(float64(count) * p / 100))
	cumulative := int64(0)

	for i, bucket := range h.buckets {
		cumulative += bucket.Load()
		if cumulative >= target {
			if i >= len(h.bounds) {
				return h.bounds[len(h.bounds)-1] // Return max bucket
			}
			return h.bounds[i]
		}
	}

	return h.bounds[len(h.bounds)-1]
}

// Reset resets all buckets and counters.
func (h *Histogram) Reset() {
	for i := range h.buckets {
		h.buckets[i].Store(0)
	}
	h.sum.Store(0)
	h.count.Store(0)
}

// Buckets returns the bucket upper bounds.
func (h *Histogram) Buckets() []float64 {
	return h.bounds
}

// Snapshot returns a snapshot of the histogram data.
func (h *Histogram) Snapshot() HistogramSnapshot {
	snap := HistogramSnapshot{
		Bounds:  h.bounds,
		Buckets: make([]int64, len(h.buckets)),
		Sum:     h.GetSum(),
		Count:   h.GetCount(),
	}
	for i, b := range h.buckets {
		snap.Buckets[i] = b.Load()
	}
	return snap
}

// HistogramSnapshot is a snapshot of histogram data.
type HistogramSnapshot struct {
	Bounds  []float64
	Buckets []int64
	Sum     float64
	Count   int64
}
