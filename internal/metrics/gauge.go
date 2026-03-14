package metrics

import (
	"math"
	"sync/atomic"
)

// Gauge is an atomic float64 gauge.
type Gauge struct {
	value atomic.Uint64 // bits of float64
	name  string
	help  string
}

// NewGauge creates a new gauge.
func NewGauge(name, help string) *Gauge {
	return &Gauge{
		name: name,
		help: help,
	}
}

// Name returns the gauge name.
func (g *Gauge) Name() string {
	return g.name
}

// Help returns the gauge help text.
func (g *Gauge) Help() string {
	return g.help
}

// Set sets the gauge to v.
func (g *Gauge) Set(v float64) {
	g.value.Store(math.Float64bits(v))
}

// Get returns the current gauge value.
func (g *Gauge) Get() float64 {
	return math.Float64frombits(g.value.Load())
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.Add(1)
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	g.Add(-1)
}

// Add adds n to the gauge.
func (g *Gauge) Add(n float64) {
	for {
		old := g.value.Load()
		newVal := math.Float64frombits(old) + n
		if g.value.CompareAndSwap(old, math.Float64bits(newVal)) {
			return
		}
	}
}

// Sub subtracts n from the gauge.
func (g *Gauge) Sub(n float64) {
	g.Add(-n)
}

// Reset resets the gauge to 0.
func (g *Gauge) Reset() {
	g.Set(0)
}
