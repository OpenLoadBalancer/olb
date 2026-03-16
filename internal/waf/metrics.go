package waf

import (
	"github.com/openloadbalancer/olb/internal/metrics"
)

// WAFMetrics holds Prometheus-compatible metrics for the WAF.
type WAFMetrics struct {
	RequestsTotal  *metrics.CounterVec
	BlockedTotal   *metrics.CounterVec
	DetectorHits   *metrics.CounterVec
	LatencySeconds *metrics.HistogramVec
}

// RegisterWAFMetrics creates and registers WAF metrics with the registry.
func RegisterWAFMetrics(registry *metrics.Registry) *WAFMetrics {
	if registry == nil {
		return nil
	}

	m := &WAFMetrics{}

	m.RequestsTotal = metrics.NewCounterVec(
		"waf_requests_total",
		"Total requests processed by WAF",
		[]string{"action"},
	)
	registry.RegisterCounterVec(m.RequestsTotal)

	m.BlockedTotal = metrics.NewCounterVec(
		"waf_blocked_total",
		"Total requests blocked by WAF by layer",
		[]string{"layer"},
	)
	registry.RegisterCounterVec(m.BlockedTotal)

	m.DetectorHits = metrics.NewCounterVec(
		"waf_detector_hits_total",
		"Detection hits by detector type",
		[]string{"detector"},
	)
	registry.RegisterCounterVec(m.DetectorHits)

	m.LatencySeconds = metrics.NewHistogramVec(
		"waf_latency_seconds",
		"WAF processing latency in seconds",
		[]string{"layer"},
	)
	registry.RegisterHistogramVec(m.LatencySeconds)

	return m
}

// RecordRequest records a WAF request metric.
func (m *WAFMetrics) RecordRequest(action string) {
	if m == nil || m.RequestsTotal == nil {
		return
	}
	m.RequestsTotal.WithLabels(map[string]string{"action": action}).Inc()
}

// RecordBlock records a WAF block metric.
func (m *WAFMetrics) RecordBlock(layer string) {
	if m == nil || m.BlockedTotal == nil {
		return
	}
	m.BlockedTotal.WithLabels(map[string]string{"layer": layer}).Inc()
}

// RecordDetectorHit records a detector hit.
func (m *WAFMetrics) RecordDetectorHit(detector string) {
	if m == nil || m.DetectorHits == nil {
		return
	}
	m.DetectorHits.WithLabels(map[string]string{"detector": detector}).Inc()
}

// RecordLatency records WAF processing latency.
func (m *WAFMetrics) RecordLatency(layer string, seconds float64) {
	if m == nil || m.LatencySeconds == nil {
		return
	}
	m.LatencySeconds.WithLabels(map[string]string{"layer": layer}).Observe(seconds)
}
