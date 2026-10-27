package waf

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/metrics"
)

func TestRegisterWAFMetrics_NilRegistry(t *testing.T) {
	result := RegisterWAFMetrics(nil)
	if result != nil {
		t.Error("RegisterWAFMetrics(nil) should return nil")
	}
}

func TestRegisterWAFMetrics(t *testing.T) {
	registry := metrics.NewRegistry()
	m := RegisterWAFMetrics(registry)
	if m == nil {
		t.Fatal("RegisterWAFMetrics should return non-nil WAFMetrics")
	}

	// Verify all four metric vectors were registered
	if m.RequestsTotal == nil {
		t.Error("RequestsTotal should not be nil")
	}
	if m.BlockedTotal == nil {
		t.Error("BlockedTotal should not be nil")
	}
	if m.DetectorHits == nil {
		t.Error("DetectorHits should not be nil")
	}
	if m.LatencySeconds == nil {
		t.Error("LatencySeconds should not be nil")
	}

	// Verify they are accessible from the registry
	if registry.GetCounterVec("waf_requests_total") == nil {
		t.Error("waf_requests_total should be registered in registry")
	}
	if registry.GetCounterVec("waf_blocked_total") == nil {
		t.Error("waf_blocked_total should be registered in registry")
	}
	if registry.GetCounterVec("waf_detector_hits_total") == nil {
		t.Error("waf_detector_hits_total should be registered in registry")
	}
	if registry.GetHistogramVec("waf_latency_seconds") == nil {
		t.Error("waf_latency_seconds should be registered in registry")
	}
}

func TestWAFMetrics_RecordRequest(t *testing.T) {
	registry := metrics.NewRegistry()
	m := RegisterWAFMetrics(registry)

	// Record some requests
	m.RecordRequest("allow")
	m.RecordRequest("allow")
	m.RecordRequest("block")

	// Verify counter vec exists
	cv := registry.GetCounterVec("waf_requests_total")
	if cv == nil {
		t.Fatal("waf_requests_total not found in registry")
	}

	// Verify counters were created and incremented
	allowCounter := cv.WithLabels(map[string]string{"action": "allow"})
	if allowCounter == nil {
		t.Fatal("allow counter should not be nil")
	}
	if allowCounter.Get() != 2 {
		t.Errorf("allow counter = %d, want 2", allowCounter.Get())
	}

	blockCounter := cv.WithLabels(map[string]string{"action": "block"})
	if blockCounter == nil {
		t.Fatal("block counter should not be nil")
	}
	if blockCounter.Get() != 1 {
		t.Errorf("block counter = %d, want 1", blockCounter.Get())
	}
}

func TestWAFMetrics_RecordBlock(t *testing.T) {
	registry := metrics.NewRegistry()
	m := RegisterWAFMetrics(registry)

	m.RecordBlock("ip_acl")
	m.RecordBlock("ip_acl")
	m.RecordBlock("rate_limit")

	cv := registry.GetCounterVec("waf_blocked_total")
	if cv == nil {
		t.Fatal("waf_blocked_total not found in registry")
	}

	ipACLCounter := cv.WithLabels(map[string]string{"layer": "ip_acl"})
	if ipACLCounter == nil {
		t.Fatal("ip_acl counter should not be nil")
	}
	if ipACLCounter.Get() != 2 {
		t.Errorf("ip_acl counter = %d, want 2", ipACLCounter.Get())
	}

	rateLimitCounter := cv.WithLabels(map[string]string{"layer": "rate_limit"})
	if rateLimitCounter == nil {
		t.Fatal("rate_limit counter should not be nil")
	}
	if rateLimitCounter.Get() != 1 {
		t.Errorf("rate_limit counter = %d, want 1", rateLimitCounter.Get())
	}
}

func TestWAFMetrics_RecordDetectorHit(t *testing.T) {
	registry := metrics.NewRegistry()
	m := RegisterWAFMetrics(registry)

	m.RecordDetectorHit("sqli")
	m.RecordDetectorHit("xss")
	m.RecordDetectorHit("sqli")

	cv := registry.GetCounterVec("waf_detector_hits_total")
	if cv == nil {
		t.Fatal("waf_detector_hits_total not found in registry")
	}

	sqliCounter := cv.WithLabels(map[string]string{"detector": "sqli"})
	if sqliCounter == nil {
		t.Fatal("sqli counter should not be nil")
	}
	if sqliCounter.Get() != 2 {
		t.Errorf("sqli counter = %d, want 2", sqliCounter.Get())
	}

	xssCounter := cv.WithLabels(map[string]string{"detector": "xss"})
	if xssCounter == nil {
		t.Fatal("xss counter should not be nil")
	}
	if xssCounter.Get() != 1 {
		t.Errorf("xss counter = %d, want 1", xssCounter.Get())
	}
}

func TestWAFMetrics_RecordLatency(t *testing.T) {
	registry := metrics.NewRegistry()
	m := RegisterWAFMetrics(registry)

	m.RecordLatency("sanitizer", 0.005)
	m.RecordLatency("detection", 0.010)

	hv := registry.GetHistogramVec("waf_latency_seconds")
	if hv == nil {
		t.Fatal("waf_latency_seconds not found in registry")
	}

	// Verify histograms were created
	sanitizer := hv.WithLabels(map[string]string{"layer": "sanitizer"})
	if sanitizer == nil {
		t.Error("sanitizer histogram should not be nil")
	}
	detection := hv.WithLabels(map[string]string{"layer": "detection"})
	if detection == nil {
		t.Error("detection histogram should not be nil")
	}
}

func TestWAFMetrics_RecordMethods_NilReceiver(t *testing.T) {
	// All record methods should be no-ops on nil receiver
	var m *WAFMetrics
	m.RecordRequest("allow")      // should not panic
	m.RecordBlock("layer")        // should not panic
	m.RecordDetectorHit("xss")    // should not panic
	m.RecordLatency("layer", 1.0) // should not panic
}

func TestWAFMetrics_RecordMethods_NilFields(t *testing.T) {
	// Test with a WAFMetrics that has nil fields
	m := &WAFMetrics{}
	m.RecordRequest("allow")      // should not panic (RequestsTotal is nil)
	m.RecordBlock("layer")        // should not panic (BlockedTotal is nil)
	m.RecordDetectorHit("xss")    // should not panic (DetectorHits is nil)
	m.RecordLatency("layer", 1.0) // should not panic (LatencySeconds is nil)
}

// The WAF middleware is rebuilt on config reload; its metrics must keep
// feeding the vectors already registered for /metrics.
func TestRegisterWAFMetrics_ReloadReusesRegisteredVectors(t *testing.T) {
	registry := metrics.NewRegistry()
	_ = RegisterWAFMetrics(registry)
	reloaded := RegisterWAFMetrics(registry)

	reloaded.RecordRequest("blocked")

	got := registry.GetCounterVec("waf_requests_total").WithLabels(map[string]string{"action": "blocked"}).Get()
	if got != 1 {
		t.Errorf("registered waf_requests_total{action=blocked} = %d after reload, want 1", got)
	}
}
