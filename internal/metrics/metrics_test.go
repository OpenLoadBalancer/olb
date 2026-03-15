package metrics

import (
	"math"
	"strings"
	"sync"
	"testing"
)

// ==================== Counter Tests ====================

func TestCounter_Basic(t *testing.T) {
	c := NewCounter("test_counter", "A test counter")

	if c.Name() != "test_counter" {
		t.Errorf("Name() = %q, want %q", c.Name(), "test_counter")
	}

	if c.Help() != "A test counter" {
		t.Errorf("Help() = %q, want %q", c.Help(), "A test counter")
	}

	// Initial value should be 0
	if c.Get() != 0 {
		t.Errorf("Get() = %d, want 0", c.Get())
	}

	// Increment
	c.Inc()
	if c.Get() != 1 {
		t.Errorf("Get() = %d, want 1", c.Get())
	}

	// Add
	c.Add(10)
	if c.Get() != 11 {
		t.Errorf("Get() = %d, want 11", c.Get())
	}

	// Reset
	c.Reset()
	if c.Get() != 0 {
		t.Errorf("Get() = %d, want 0", c.Get())
	}
}

func TestCounter_Concurrent(t *testing.T) {
	c := NewCounter("test", "test")

	// Run 100 goroutines each incrementing 1000 times
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				c.Inc()
			}
		}()
	}
	wg.Wait()

	if c.Get() != 100000 {
		t.Errorf("Get() = %d, want 100000", c.Get())
	}
}

// ==================== Gauge Tests ====================

func TestGauge_Basic(t *testing.T) {
	g := NewGauge("test_gauge", "A test gauge")

	if g.Name() != "test_gauge" {
		t.Errorf("Name() = %q, want %q", g.Name(), "test_gauge")
	}

	if g.Help() != "A test gauge" {
		t.Errorf("Help() = %q, want %q", g.Help(), "A test gauge")
	}

	// Initial value should be 0
	if g.Get() != 0 {
		t.Errorf("Get() = %f, want 0", g.Get())
	}

	// Set
	g.Set(42.5)
	if g.Get() != 42.5 {
		t.Errorf("Get() = %f, want 42.5", g.Get())
	}

	// Inc
	g.Inc()
	if g.Get() != 43.5 {
		t.Errorf("Get() = %f, want 43.5", g.Get())
	}

	// Dec
	g.Dec()
	if g.Get() != 42.5 {
		t.Errorf("Get() = %f, want 42.5", g.Get())
	}

	// Add
	g.Add(7.5)
	if g.Get() != 50 {
		t.Errorf("Get() = %f, want 50", g.Get())
	}

	// Sub
	g.Sub(10)
	if g.Get() != 40 {
		t.Errorf("Get() = %f, want 40", g.Get())
	}

	// Reset
	g.Reset()
	if g.Get() != 0 {
		t.Errorf("Get() = %f, want 0", g.Get())
	}
}

func TestGauge_Concurrent(t *testing.T) {
	g := NewGauge("test", "test")

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				g.Add(1)
			}
		}()
	}
	wg.Wait()

	if g.Get() != 100000 {
		t.Errorf("Get() = %f, want 100000", g.Get())
	}
}

// ==================== Histogram Tests ====================

func TestHistogram_Basic(t *testing.T) {
	h := NewHistogram("test_histogram", "A test histogram")

	if h.Name() != "test_histogram" {
		t.Errorf("Name() = %q, want %q", h.Name(), "test_histogram")
	}

	// Observe some values
	h.Observe(0.5)
	h.Observe(1.0)
	h.Observe(10.0)
	h.Observe(100.0)

	// Check count
	if h.GetCount() != 4 {
		t.Errorf("GetCount() = %d, want 4", h.GetCount())
	}

	// Check sum
	if h.GetSum() != 111.5 {
		t.Errorf("GetSum() = %f, want 111.5", h.GetSum())
	}

	// Reset
	h.Reset()
	if h.GetCount() != 0 {
		t.Errorf("GetCount() = %d, want 0", h.GetCount())
	}
}

func TestHistogram_Percentile(t *testing.T) {
	h := NewHistogram("test", "test")

	// Observe 100 values
	for i := 1; i <= 100; i++ {
		h.Observe(float64(i))
	}

	// Check percentiles - these are estimates based on buckets
	p50 := h.Percentile(50)
	if p50 < 50 || p50 > 100 {
		t.Errorf("Percentile(50) = %f, want around 50", p50)
	}

	p90 := h.Percentile(90)
	if p90 < 90 || p90 > 250 {
		t.Errorf("Percentile(90) = %f, want around 90", p90)
	}

	p99 := h.Percentile(99)
	if p99 < 100 {
		t.Errorf("Percentile(99) = %f, want at least 100", p99)
	}
}

func TestHistogram_PercentileEdgeCases(t *testing.T) {
	h := NewHistogram("test", "test")

	// Empty histogram
	if h.Percentile(50) != 0 {
		t.Errorf("Percentile(50) on empty histogram = %f, want 0", h.Percentile(50))
	}

	// Out of range percentiles
	h.Observe(100)
	if h.Percentile(-1) != 0 {
		t.Errorf("Percentile(-1) = %f, want 0", h.Percentile(-1))
	}
	if h.Percentile(101) != 0 {
		t.Errorf("Percentile(101) = %f, want 0", h.Percentile(101))
	}
}

func TestHistogram_CustomBuckets(t *testing.T) {
	buckets := []float64{1, 10, 100}
	h := NewHistogramWithBuckets("test", "test", buckets)

	if len(h.Buckets()) != 3 {
		t.Errorf("len(Buckets()) = %d, want 3", len(h.Buckets()))
	}

	h.Observe(5)
	h.Observe(50)
	h.Observe(500)

	if h.GetCount() != 3 {
		t.Errorf("GetCount() = %d, want 3", h.GetCount())
	}
}

func TestHistogram_Snapshot(t *testing.T) {
	h := NewHistogram("test", "test")
	h.Observe(1.0)
	h.Observe(2.0)
	h.Observe(3.0)

	snap := h.Snapshot()
	if snap.Count != 3 {
		t.Errorf("Snapshot.Count = %d, want 3", snap.Count)
	}
	if snap.Sum != 6.0 {
		t.Errorf("Snapshot.Sum = %f, want 6.0", snap.Sum)
	}
	if len(snap.Buckets) != len(snap.Bounds)+1 {
		t.Errorf("len(Snapshot.Buckets) = %d, want %d", len(snap.Buckets), len(snap.Bounds)+1)
	}
}

func TestHistogram_GetBucketCount(t *testing.T) {
	h := NewHistogram("test", "test")
	h.Observe(0.5)

	// Valid index
	if h.GetBucketCount(0) < 0 {
		t.Error("GetBucketCount(0) should be >= 0")
	}

	// Invalid indices
	if h.GetBucketCount(-1) != 0 {
		t.Errorf("GetBucketCount(-1) = %d, want 0", h.GetBucketCount(-1))
	}
	if h.GetBucketCount(1000) != 0 {
		t.Errorf("GetBucketCount(1000) = %d, want 0", h.GetBucketCount(1000))
	}
}

func TestHistogram_Concurrent(t *testing.T) {
	h := NewHistogram("test", "test")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				h.Observe(float64(j))
			}
		}()
	}
	wg.Wait()

	if h.GetCount() != 100000 {
		t.Errorf("GetCount() = %d, want 100000", h.GetCount())
	}
}

// ==================== CounterVec Tests ====================

func TestCounterVec_Basic(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method", "status"})

	if cv.Name() != "requests_total" {
		t.Errorf("Name() = %q, want %q", cv.Name(), "requests_total")
	}

	if cv.Help() != "Total requests" {
		t.Errorf("Help() = %q, want %q", cv.Help(), "Total requests")
	}

	labels := cv.Labels()
	if len(labels) != 2 || labels[0] != "method" || labels[1] != "status" {
		t.Errorf("Labels() = %v, want [method status]", labels)
	}
}

func TestCounterVec_With(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method", "status"})

	// Get counter with label values
	c1 := cv.With("GET", "200")
	c1.Inc()
	c1.Inc()

	// Get same counter again
	c2 := cv.With("GET", "200")
	if c1 != c2 {
		t.Error("With() should return same counter for same labels")
	}
	if c2.Get() != 2 {
		t.Errorf("Get() = %d, want 2", c2.Get())
	}

	// Get different counter
	c3 := cv.With("POST", "500")
	c3.Inc()
	if c3.Get() != 1 {
		t.Errorf("Get() = %d, want 1", c3.Get())
	}
}

func TestCounterVec_WithLabels(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method", "status"})

	c := cv.WithLabels(map[string]string{
		"method": "GET",
		"status": "200",
	})
	c.Inc()

	if c.Get() != 1 {
		t.Errorf("Get() = %d, want 1", c.Get())
	}
}

func TestCounterVec_WithLabels_MissingLabel(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method", "status"})

	// Missing "status" label - should use empty string
	c := cv.WithLabels(map[string]string{
		"method": "GET",
	})
	c.Inc()

	// Should get same counter with empty status
	c2 := cv.With("GET", "")
	if c != c2 {
		t.Error("WithLabels with missing label should use empty string")
	}
}

func TestCounterVec_Delete(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method", "status"})

	c1 := cv.With("GET", "200")
	c1.Inc()

	// Delete the counter
	cv.Delete("GET", "200")

	// Get a new counter (should be fresh)
	c2 := cv.With("GET", "200")
	if c2.Get() != 0 {
		t.Errorf("Get() after delete = %d, want 0", c2.Get())
	}
}

func TestCounterVec_Reset(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method"})

	c1 := cv.With("GET")
	c1.Inc()
	c2 := cv.With("POST")
	c2.Inc()

	// Reset all counters
	cv.Reset()

	// Get new counters - should be fresh
	c3 := cv.With("GET")
	c4 := cv.With("POST")
	if c3.Get() != 0 || c4.Get() != 0 {
		t.Error("Reset() should clear all counters")
	}
}

func TestCounterVec_Collect(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"method"})

	cv.With("GET").Inc()
	cv.With("POST").Add(5)

	collected := make(map[string]int64)
	cv.Collect(func(labels map[string]string, c *Counter) {
		collected[labels["method"]] = c.Get()
	})

	if collected["GET"] != 1 {
		t.Errorf("GET count = %d, want 1", collected["GET"])
	}
	if collected["POST"] != 5 {
		t.Errorf("POST count = %d, want 5", collected["POST"])
	}
}

func TestCounterVec_Concurrent(t *testing.T) {
	cv := NewCounterVec("requests_total", "Total requests", []string{"id"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := cv.With(string(rune('a' + id%26)))
			for j := 0; j < 1000; j++ {
				c.Inc()
			}
		}(i)
	}
	wg.Wait()

	// Check total count
	total := int64(0)
	cv.Collect(func(labels map[string]string, c *Counter) {
		total += c.Get()
	})
	if total != 100000 {
		t.Errorf("Total count = %d, want 100000", total)
	}
}

// ==================== GaugeVec Tests ====================

func TestGaugeVec_Basic(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})

	if gv.Name() != "active_connections" {
		t.Errorf("Name() = %q, want %q", gv.Name(), "active_connections")
	}

	if gv.Help() != "Active connections" {
		t.Errorf("Help() = %q, want %q", gv.Help(), "Active connections")
	}

	labels := gv.Labels()
	if len(labels) != 1 || labels[0] != "pool" {
		t.Errorf("Labels() = %v, want [pool]", labels)
	}
}

func TestGaugeVec_With(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})

	g1 := gv.With("pool1")
	g1.Set(42)

	// Get same gauge again
	g2 := gv.With("pool1")
	if g1 != g2 {
		t.Error("With() should return same gauge for same labels")
	}
	if g2.Get() != 42 {
		t.Errorf("Get() = %f, want 42", g2.Get())
	}

	// Get different gauge
	g3 := gv.With("pool2")
	g3.Set(10)
	if g3.Get() != 10 {
		t.Errorf("Get() = %f, want 10", g3.Get())
	}
}

func TestGaugeVec_WithLabels(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool", "backend"})

	g := gv.WithLabels(map[string]string{
		"pool":    "pool1",
		"backend": "backend1",
	})
	g.Set(100)

	if g.Get() != 100 {
		t.Errorf("Get() = %f, want 100", g.Get())
	}
}

func TestGaugeVec_Delete(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})

	g1 := gv.With("pool1")
	g1.Set(42)

	// Delete the gauge
	gv.Delete("pool1")

	// Get a new gauge (should be fresh)
	g2 := gv.With("pool1")
	if g2.Get() != 0 {
		t.Errorf("Get() after delete = %f, want 0", g2.Get())
	}
}

func TestGaugeVec_Reset(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})

	gv.With("pool1").Set(10)
	gv.With("pool2").Set(20)

	// Reset all gauges
	gv.Reset()

	// Get new gauges - should be fresh
	if gv.With("pool1").Get() != 0 {
		t.Error("Reset() should clear all gauges")
	}
	if gv.With("pool2").Get() != 0 {
		t.Error("Reset() should clear all gauges")
	}
}

func TestGaugeVec_Collect(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})

	gv.With("pool1").Set(10)
	gv.With("pool2").Set(20)

	collected := make(map[string]float64)
	gv.Collect(func(labels map[string]string, g *Gauge) {
		collected[labels["pool"]] = g.Get()
	})

	if collected["pool1"] != 10 {
		t.Errorf("pool1 value = %f, want 10", collected["pool1"])
	}
	if collected["pool2"] != 20 {
		t.Errorf("pool2 value = %f, want 20", collected["pool2"])
	}
}

func TestGaugeVec_Concurrent(t *testing.T) {
	gv := NewGaugeVec("active_connections", "Active connections", []string{"id"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			g := gv.With(string(rune('a' + id%26)))
			for j := 0; j < 1000; j++ {
				g.Add(1)
			}
		}(i)
	}
	wg.Wait()

	// Check total
	total := 0.0
	gv.Collect(func(labels map[string]string, g *Gauge) {
		total += g.Get()
	})
	if total != 100000 {
		t.Errorf("Total = %f, want 100000", total)
	}
}

// ==================== HistogramVec Tests ====================

func TestHistogramVec_Basic(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})

	if hv.Name() != "request_duration" {
		t.Errorf("Name() = %q, want %q", hv.Name(), "request_duration")
	}

	if hv.Help() != "Request duration" {
		t.Errorf("Help() = %q, want %q", hv.Help(), "Request duration")
	}

	labels := hv.Labels()
	if len(labels) != 1 || labels[0] != "method" {
		t.Errorf("Labels() = %v, want [method]", labels)
	}
}

func TestHistogramVec_With(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})

	h1 := hv.With("GET")
	h1.Observe(0.5)
	h1.Observe(1.0)

	// Get same histogram again
	h2 := hv.With("GET")
	if h1 != h2 {
		t.Error("With() should return same histogram for same labels")
	}
	if h2.GetCount() != 2 {
		t.Errorf("GetCount() = %d, want 2", h2.GetCount())
	}

	// Get different histogram
	h3 := hv.With("POST")
	h3.Observe(2.0)
	if h3.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h3.GetCount())
	}
}

func TestHistogramVec_WithLabels(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method", "status"})

	h := hv.WithLabels(map[string]string{
		"method": "GET",
		"status": "200",
	})
	h.Observe(0.5)

	if h.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h.GetCount())
	}
}

func TestHistogramVec_CustomBuckets(t *testing.T) {
	buckets := []float64{1, 10, 100}
	hv := NewHistogramVecWithBuckets("request_duration", "Request duration", []string{"method"}, buckets)

	h := hv.With("GET")
	h.Observe(5)

	if h.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h.GetCount())
	}
}

func TestHistogramVec_Delete(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})

	h1 := hv.With("GET")
	h1.Observe(0.5)

	// Delete the histogram
	hv.Delete("GET")

	// Get a new histogram (should be fresh)
	h2 := hv.With("GET")
	if h2.GetCount() != 0 {
		t.Errorf("GetCount() after delete = %d, want 0", h2.GetCount())
	}
}

func TestHistogramVec_Reset(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})

	hv.With("GET").Observe(0.5)
	hv.With("POST").Observe(1.0)

	// Reset all histograms
	hv.Reset()

	// Get new histograms - should be fresh
	if hv.With("GET").GetCount() != 0 {
		t.Error("Reset() should clear all histograms")
	}
	if hv.With("POST").GetCount() != 0 {
		t.Error("Reset() should clear all histograms")
	}
}

func TestHistogramVec_Collect(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})

	hv.With("GET").Observe(0.5)
	hv.With("POST").Observe(1.0)
	hv.With("POST").Observe(2.0)

	collected := make(map[string]int64)
	hv.Collect(func(labels map[string]string, h *Histogram) {
		collected[labels["method"]] = h.GetCount()
	})

	if collected["GET"] != 1 {
		t.Errorf("GET count = %d, want 1", collected["GET"])
	}
	if collected["POST"] != 2 {
		t.Errorf("POST count = %d, want 2", collected["POST"])
	}
}

func TestHistogramVec_Concurrent(t *testing.T) {
	hv := NewHistogramVec("request_duration", "Request duration", []string{"id"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			h := hv.With(string(rune('a' + id%26)))
			for j := 0; j < 1000; j++ {
				h.Observe(float64(j))
			}
		}(i)
	}
	wg.Wait()

	// Check total count
	total := int64(0)
	hv.Collect(func(labels map[string]string, h *Histogram) {
		total += h.GetCount()
	})
	if total != 100000 {
		t.Errorf("Total count = %d, want 100000", total)
	}
}

// ==================== Registry Tests ====================

func TestRegistry_Basic(t *testing.T) {
	r := NewRegistry()

	// Register a counter
	c := NewCounter("requests_total", "Total requests")
	if err := r.RegisterCounter(c); err != nil {
		t.Fatalf("RegisterCounter failed: %v", err)
	}

	// Register a gauge
	g := NewGauge("active_connections", "Active connections")
	if err := r.RegisterGauge(g); err != nil {
		t.Fatalf("RegisterGauge failed: %v", err)
	}

	// Register a histogram
	h := NewHistogram("request_duration", "Request duration")
	if err := r.RegisterHistogram(h); err != nil {
		t.Fatalf("RegisterHistogram failed: %v", err)
	}

	// Get metrics
	if got := r.GetCounter("requests_total"); got != c {
		t.Error("GetCounter returned wrong counter")
	}

	if got := r.GetGauge("active_connections"); got != g {
		t.Error("GetGauge returned wrong gauge")
	}

	if got := r.GetHistogram("request_duration"); got != h {
		t.Error("GetHistogram returned wrong histogram")
	}
}

func TestRegistry_RegisterVecs(t *testing.T) {
	r := NewRegistry()

	// Register counter vec
	cv := NewCounterVec("requests_total", "Total requests", []string{"method"})
	if err := r.RegisterCounterVec(cv); err != nil {
		t.Fatalf("RegisterCounterVec failed: %v", err)
	}

	// Register gauge vec
	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})
	if err := r.RegisterGaugeVec(gv); err != nil {
		t.Fatalf("RegisterGaugeVec failed: %v", err)
	}

	// Register histogram vec
	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})
	if err := r.RegisterHistogramVec(hv); err != nil {
		t.Fatalf("RegisterHistogramVec failed: %v", err)
	}

	// Get metrics
	if got := r.GetCounterVec("requests_total"); got != cv {
		t.Error("GetCounterVec returned wrong counter vec")
	}

	if got := r.GetGaugeVec("active_connections"); got != gv {
		t.Error("GetGaugeVec returned wrong gauge vec")
	}

	if got := r.GetHistogramVec("request_duration"); got != hv {
		t.Error("GetHistogramVec returned wrong histogram vec")
	}
}

func TestRegistry_Duplicate(t *testing.T) {
	r := NewRegistry()

	c1 := NewCounter("test", "test")
	if err := r.RegisterCounter(c1); err != nil {
		t.Fatalf("First RegisterCounter failed: %v", err)
	}

	c2 := NewCounter("test", "test")
	if err := r.RegisterCounter(c2); err == nil {
		t.Error("Second RegisterCounter should fail")
	}
}

func TestRegistry_DuplicateGauge(t *testing.T) {
	r := NewRegistry()

	g1 := NewGauge("test", "test")
	if err := r.RegisterGauge(g1); err != nil {
		t.Fatalf("First RegisterGauge failed: %v", err)
	}

	g2 := NewGauge("test", "test")
	if err := r.RegisterGauge(g2); err == nil {
		t.Error("Second RegisterGauge should fail")
	}
}

func TestRegistry_DuplicateHistogram(t *testing.T) {
	r := NewRegistry()

	h1 := NewHistogram("test", "test")
	if err := r.RegisterHistogram(h1); err != nil {
		t.Fatalf("First RegisterHistogram failed: %v", err)
	}

	h2 := NewHistogram("test", "test")
	if err := r.RegisterHistogram(h2); err == nil {
		t.Error("Second RegisterHistogram should fail")
	}
}

func TestRegistry_DuplicateVecs(t *testing.T) {
	r := NewRegistry()

	// CounterVec
	cv1 := NewCounterVec("test", "test", []string{"a"})
	if err := r.RegisterCounterVec(cv1); err != nil {
		t.Fatalf("First RegisterCounterVec failed: %v", err)
	}
	cv2 := NewCounterVec("test", "test", []string{"a"})
	if err := r.RegisterCounterVec(cv2); err == nil {
		t.Error("Second RegisterCounterVec should fail")
	}

	// GaugeVec
	gv1 := NewGaugeVec("gauge_test", "test", []string{"a"})
	if err := r.RegisterGaugeVec(gv1); err != nil {
		t.Fatalf("First RegisterGaugeVec failed: %v", err)
	}
	gv2 := NewGaugeVec("gauge_test", "test", []string{"a"})
	if err := r.RegisterGaugeVec(gv2); err == nil {
		t.Error("Second RegisterGaugeVec should fail")
	}

	// HistogramVec
	hv1 := NewHistogramVec("hist_test", "test", []string{"a"})
	if err := r.RegisterHistogramVec(hv1); err != nil {
		t.Fatalf("First RegisterHistogramVec failed: %v", err)
	}
	hv2 := NewHistogramVec("hist_test", "test", []string{"a"})
	if err := r.RegisterHistogramVec(hv2); err == nil {
		t.Error("Second RegisterHistogramVec should fail")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()

	if r.GetCounter("nonexistent") != nil {
		t.Error("GetCounter should return nil for nonexistent counter")
	}

	if r.GetGauge("nonexistent") != nil {
		t.Error("GetGauge should return nil for nonexistent gauge")
	}

	if r.GetHistogram("nonexistent") != nil {
		t.Error("GetHistogram should return nil for nonexistent histogram")
	}

	if r.GetCounterVec("nonexistent") != nil {
		t.Error("GetCounterVec should return nil for nonexistent counter vec")
	}

	if r.GetGaugeVec("nonexistent") != nil {
		t.Error("GetGaugeVec should return nil for nonexistent gauge vec")
	}

	if r.GetHistogramVec("nonexistent") != nil {
		t.Error("GetHistogramVec should return nil for nonexistent histogram vec")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()

	// Register metrics
	r.RegisterCounter(NewCounter("counter", "test"))
	r.RegisterGauge(NewGauge("gauge", "test"))
	r.RegisterHistogram(NewHistogram("histogram", "test"))
	r.RegisterCounterVec(NewCounterVec("countervec", "test", []string{"a"}))
	r.RegisterGaugeVec(NewGaugeVec("gaugevec", "test", []string{"a"}))
	r.RegisterHistogramVec(NewHistogramVec("histogramvec", "test", []string{"a"}))

	// Unregister each
	r.Unregister("counter")
	if r.GetCounter("counter") != nil {
		t.Error("Unregister should remove counter")
	}

	r.Unregister("gauge")
	if r.GetGauge("gauge") != nil {
		t.Error("Unregister should remove gauge")
	}

	r.Unregister("histogram")
	if r.GetHistogram("histogram") != nil {
		t.Error("Unregister should remove histogram")
	}

	r.Unregister("countervec")
	if r.GetCounterVec("countervec") != nil {
		t.Error("Unregister should remove counter vec")
	}

	r.Unregister("gaugevec")
	if r.GetGaugeVec("gaugevec") != nil {
		t.Error("Unregister should remove gauge vec")
	}

	r.Unregister("histogramvec")
	if r.GetHistogramVec("histogramvec") != nil {
		t.Error("Unregister should remove histogram vec")
	}
}

func TestRegistry_Collect(t *testing.T) {
	r := NewRegistry()

	// Register metrics
	c := NewCounter("counter", "test")
	c.Inc()
	r.RegisterCounter(c)

	g := NewGauge("gauge", "test")
	g.Set(42)
	r.RegisterGauge(g)

	h := NewHistogram("histogram", "test")
	h.Observe(1.0)
	r.RegisterHistogram(h)

	cv := NewCounterVec("countervec", "test", []string{"a"})
	cv.With("x").Inc()
	r.RegisterCounterVec(cv)

	gv := NewGaugeVec("gaugevec", "test", []string{"a"})
	gv.With("x").Set(10)
	r.RegisterGaugeVec(gv)

	hv := NewHistogramVec("histogramvec", "test", []string{"a"})
	hv.With("x").Observe(1.0)
	r.RegisterHistogramVec(hv)

	// Collect all metrics
	var (
		counterFound      bool
		gaugeFound        bool
		histogramFound    bool
		counterVecFound   bool
		gaugeVecFound     bool
		histogramVecFound bool
	)

	r.Collect(
		func(name string, c *Counter) {
			if name == "counter" && c.Get() == 1 {
				counterFound = true
			}
		},
		func(name string, g *Gauge) {
			if name == "gauge" && g.Get() == 42 {
				gaugeFound = true
			}
		},
		func(name string, h *Histogram) {
			if name == "histogram" && h.GetCount() == 1 {
				histogramFound = true
			}
		},
		func(name string, cv *CounterVec) {
			if name == "countervec" {
				counterVecFound = true
			}
		},
		func(name string, gv *GaugeVec) {
			if name == "gaugevec" {
				gaugeVecFound = true
			}
		},
		func(name string, hv *HistogramVec) {
			if name == "histogramvec" {
				histogramVecFound = true
			}
		},
	)

	if !counterFound {
		t.Error("Collect should find counter")
	}
	if !gaugeFound {
		t.Error("Collect should find gauge")
	}
	if !histogramFound {
		t.Error("Collect should find histogram")
	}
	if !counterVecFound {
		t.Error("Collect should find counter vec")
	}
	if !gaugeVecFound {
		t.Error("Collect should find gauge vec")
	}
	if !histogramVecFound {
		t.Error("Collect should find histogram vec")
	}
}

func TestRegistry_Reset(t *testing.T) {
	r := NewRegistry()

	// Register metrics
	r.RegisterCounter(NewCounter("counter", "test"))
	r.RegisterGauge(NewGauge("gauge", "test"))
	r.RegisterHistogram(NewHistogram("histogram", "test"))
	r.RegisterCounterVec(NewCounterVec("countervec", "test", []string{"a"}))
	r.RegisterGaugeVec(NewGaugeVec("gaugevec", "test", []string{"a"}))
	r.RegisterHistogramVec(NewHistogramVec("histogramvec", "test", []string{"a"}))

	// Reset
	r.Reset()

	// All should be nil
	if r.GetCounter("counter") != nil {
		t.Error("Reset should clear counters")
	}
	if r.GetGauge("gauge") != nil {
		t.Error("Reset should clear gauges")
	}
	if r.GetHistogram("histogram") != nil {
		t.Error("Reset should clear histograms")
	}
	if r.GetCounterVec("countervec") != nil {
		t.Error("Reset should clear counter vecs")
	}
	if r.GetGaugeVec("gaugevec") != nil {
		t.Error("Reset should clear gauge vecs")
	}
	if r.GetHistogramVec("histogramvec") != nil {
		t.Error("Reset should clear histogram vecs")
	}
}

func TestRegistry_Empty(t *testing.T) {
	r := NewRegistry()

	// Collect on empty registry should not panic
	r.Collect(
		func(name string, c *Counter) { t.Error("Should not find counters in empty registry") },
		func(name string, g *Gauge) { t.Error("Should not find gauges in empty registry") },
		func(name string, h *Histogram) { t.Error("Should not find histograms in empty registry") },
		func(name string, cv *CounterVec) { t.Error("Should not find counter vecs in empty registry") },
		func(name string, gv *GaugeVec) { t.Error("Should not find gauge vecs in empty registry") },
		func(name string, hv *HistogramVec) { t.Error("Should not find histogram vecs in empty registry") },
	)
}

// ==================== DefaultRegistry Tests ====================

func TestDefaultRegistry(t *testing.T) {
	// Reset default registry for clean test
	DefaultRegistry.Reset()

	// Test global registration functions
	c := NewCounter("test_counter", "test")
	if err := RegisterCounter(c); err != nil {
		t.Errorf("RegisterCounter failed: %v", err)
	}

	g := NewGauge("test_gauge", "test")
	if err := RegisterGauge(g); err != nil {
		t.Errorf("RegisterGauge failed: %v", err)
	}

	h := NewHistogram("test_histogram", "test")
	if err := RegisterHistogram(h); err != nil {
		t.Errorf("RegisterHistogram failed: %v", err)
	}

	cv := NewCounterVec("test_countervec", "test", []string{"a"})
	if err := RegisterCounterVec(cv); err != nil {
		t.Errorf("RegisterCounterVec failed: %v", err)
	}

	gv := NewGaugeVec("test_gaugevec", "test", []string{"a"})
	if err := RegisterGaugeVec(gv); err != nil {
		t.Errorf("RegisterGaugeVec failed: %v", err)
	}

	hv := NewHistogramVec("test_histogramvec", "test", []string{"a"})
	if err := RegisterHistogramVec(hv); err != nil {
		t.Errorf("RegisterHistogramVec failed: %v", err)
	}

	// Verify they were registered
	if DefaultRegistry.GetCounter("test_counter") != c {
		t.Error("DefaultRegistry should have counter")
	}
	if DefaultRegistry.GetGauge("test_gauge") != g {
		t.Error("DefaultRegistry should have gauge")
	}
	if DefaultRegistry.GetHistogram("test_histogram") != h {
		t.Error("DefaultRegistry should have histogram")
	}
	if DefaultRegistry.GetCounterVec("test_countervec") != cv {
		t.Error("DefaultRegistry should have counter vec")
	}
	if DefaultRegistry.GetGaugeVec("test_gaugevec") != gv {
		t.Error("DefaultRegistry should have gauge vec")
	}
	if DefaultRegistry.GetHistogramVec("test_histogramvec") != hv {
		t.Error("DefaultRegistry should have histogram vec")
	}
}

// ==================== PrometheusHandler Tests ====================

func TestPrometheusHandler(t *testing.T) {
	r := NewRegistry()

	// Register metrics
	c := NewCounter("requests_total", "Total requests")
	c.Inc()
	r.RegisterCounter(c)

	g := NewGauge("active_connections", "Active connections")
	g.Set(42)
	r.RegisterGauge(g)

	// Write Prometheus format
	h := NewPrometheusHandler(r)
	var buf strings.Builder
	err := h.WriteMetrics(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check output contains expected content
	if !strings.Contains(output, "# HELP requests_total Total requests") {
		t.Error("Output missing counter help")
	}

	if !strings.Contains(output, "# TYPE requests_total counter") {
		t.Error("Output missing counter type")
	}

	if !strings.Contains(output, "requests_total 1") {
		t.Error("Output missing counter value")
	}

	if !strings.Contains(output, "active_connections 42") {
		t.Error("Output missing gauge value")
	}
}

func TestPrometheusHandler_Histogram(t *testing.T) {
	r := NewRegistry()

	h := NewHistogram("request_duration", "Request duration")
	h.Observe(0.5)
	h.Observe(1.0)
	h.Observe(10.0)
	r.RegisterHistogram(h)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check histogram output
	if !strings.Contains(output, "# TYPE request_duration histogram") {
		t.Error("Output missing histogram type")
	}
	if !strings.Contains(output, "request_duration_sum") {
		t.Error("Output missing histogram sum")
	}
	if !strings.Contains(output, "request_duration_count 3") {
		t.Error("Output missing histogram count")
	}
	if !strings.Contains(output, "request_duration_bucket{le=") {
		t.Error("Output missing histogram buckets")
	}
	if !strings.Contains(output, "request_duration_bucket{le=\"+Inf\"}") {
		t.Error("Output missing +Inf bucket")
	}
}

func TestPrometheusHandler_CounterVec(t *testing.T) {
	r := NewRegistry()

	cv := NewCounterVec("requests_total", "Total requests", []string{"method", "status"})
	cv.With("GET", "200").Inc()
	cv.With("GET", "200").Inc()
	cv.With("POST", "500").Inc()
	r.RegisterCounterVec(cv)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check counter vec output
	if !strings.Contains(output, "# TYPE requests_total counter") {
		t.Error("Output missing counter vec type")
	}
	if !strings.Contains(output, `requests_total{method="GET",status="200"} 2`) {
		t.Error("Output missing GET 200 counter value")
	}
	if !strings.Contains(output, `requests_total{method="POST",status="500"} 1`) {
		t.Error("Output missing POST 500 counter value")
	}
}

func TestPrometheusHandler_GaugeVec(t *testing.T) {
	r := NewRegistry()

	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})
	gv.With("pool1").Set(10)
	gv.With("pool2").Set(20)
	r.RegisterGaugeVec(gv)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check gauge vec output
	if !strings.Contains(output, "# TYPE active_connections gauge") {
		t.Error("Output missing gauge vec type")
	}
	if !strings.Contains(output, `active_connections{pool="pool1"} 10`) {
		t.Error("Output missing pool1 gauge value")
	}
	if !strings.Contains(output, `active_connections{pool="pool2"} 20`) {
		t.Error("Output missing pool2 gauge value")
	}
}

func TestPrometheusHandler_HistogramVec(t *testing.T) {
	r := NewRegistry()

	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})
	hv.With("GET").Observe(0.5)
	hv.With("GET").Observe(1.0)
	hv.With("POST").Observe(2.0)
	r.RegisterHistogramVec(hv)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check histogram vec output
	if !strings.Contains(output, "# TYPE request_duration histogram") {
		t.Error("Output missing histogram vec type")
	}
	// The format is: request_duration_bucket{method="GET"}{le="0.5"} (two brace pairs)
	if !strings.Contains(output, `request_duration_bucket{method="GET"}{le=`) {
		t.Error("Output missing GET histogram buckets")
	}
	// The format is: request_duration_count{method="GET"} 2 (no le label for count)
	if !strings.Contains(output, `request_duration_count{method="GET"} 2`) {
		t.Error("Output missing GET histogram count")
	}
	if !strings.Contains(output, `request_duration_count{method="POST"} 1`) {
		t.Error("Output missing POST histogram count")
	}
}

func TestPrometheusHandler_NilRegistry(t *testing.T) {
	// Should use DefaultRegistry when nil
	ph := NewPrometheusHandler(nil)
	if ph.registry != DefaultRegistry {
		t.Error("NewPrometheusHandler should use DefaultRegistry when nil")
	}
}

func TestPrometheusHandler_EmptyRegistry(t *testing.T) {
	r := NewRegistry()
	ph := NewPrometheusHandler(r)

	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	// Empty output is fine
	_ = buf.String()
}

func TestPrometheusHandler_EscapeHelp(t *testing.T) {
	r := NewRegistry()

	c := NewCounter("test", "Help with \\ backslash and \n newline")
	r.RegisterCounter(c)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "\\\\") {
		t.Error("Backslash should be escaped")
	}
	if !strings.Contains(output, "\\n") {
		t.Error("Newline should be escaped")
	}
}

func TestPrometheusHandler_EscapeLabel(t *testing.T) {
	r := NewRegistry()

	cv := NewCounterVec("test", "test", []string{"label"})
	cv.With(`value with \ and "quotes" and
newline`).Inc()
	r.RegisterCounterVec(cv)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()
	// Check that special characters are escaped
	if strings.Contains(output, `"label"="value with \ and "quotes"`) {
		t.Error("Quotes should be escaped in labels")
	}
}

// ==================== JSONHandler Tests ====================

func TestJSONHandler(t *testing.T) {
	r := NewRegistry()

	// Register metrics
	c := NewCounter("requests_total", "Total requests")
	c.Inc()
	r.RegisterCounter(c)

	// Write JSON format
	h := NewJSONHandler(r)
	var buf strings.Builder
	err := h.WriteMetrics(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check output contains expected content
	if !strings.Contains(output, `"type": "counter"`) {
		t.Error("Output missing counter type")
	}

	if !strings.Contains(output, `"value": 1`) {
		t.Error("Output missing counter value")
	}
}

func TestJSONHandler_Gauge(t *testing.T) {
	r := NewRegistry()

	g := NewGauge("active_connections", "Active connections")
	g.Set(42.5)
	r.RegisterGauge(g)

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"type": "gauge"`) {
		t.Error("Output missing gauge type")
	}
	if !strings.Contains(output, `"value": 42.5`) {
		t.Error("Output missing gauge value")
	}
}

func TestJSONHandler_Histogram(t *testing.T) {
	r := NewRegistry()

	h := NewHistogram("request_duration", "Request duration")
	h.Observe(0.5)
	h.Observe(1.0)
	r.RegisterHistogram(h)

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"type": "histogram"`) {
		t.Error("Output missing histogram type")
	}
	if !strings.Contains(output, `"count": 2`) {
		t.Error("Output missing histogram count")
	}
	if !strings.Contains(output, `"buckets"`) {
		t.Error("Output missing histogram buckets")
	}
}

func TestJSONHandler_CounterVec(t *testing.T) {
	r := NewRegistry()

	cv := NewCounterVec("requests_total", "Total requests", []string{"method"})
	cv.With("GET").Inc()
	cv.With("POST").Add(5)
	r.RegisterCounterVec(cv)

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"type": "counter_vec"`) {
		t.Error("Output missing counter_vec type")
	}
	if !strings.Contains(output, `"labels"`) {
		t.Error("Output missing labels")
	}
	if !strings.Contains(output, `"values"`) {
		t.Error("Output missing values")
	}
}

func TestJSONHandler_GaugeVec(t *testing.T) {
	r := NewRegistry()

	gv := NewGaugeVec("active_connections", "Active connections", []string{"pool"})
	gv.With("pool1").Set(10)
	gv.With("pool2").Set(20)
	r.RegisterGaugeVec(gv)

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"type": "gauge_vec"`) {
		t.Error("Output missing gauge_vec type")
	}
}

func TestJSONHandler_HistogramVec(t *testing.T) {
	r := NewRegistry()

	hv := NewHistogramVec("request_duration", "Request duration", []string{"method"})
	hv.With("GET").Observe(0.5)
	hv.With("POST").Observe(2.0)
	r.RegisterHistogramVec(hv)

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"type": "histogram_vec"`) {
		t.Error("Output missing histogram_vec type")
	}
}

func TestJSONHandler_NilRegistry(t *testing.T) {
	// Should use DefaultRegistry when nil
	jh := NewJSONHandler(nil)
	if jh.registry != DefaultRegistry {
		t.Error("NewJSONHandler should use DefaultRegistry when nil")
	}
}

func TestJSONHandler_EmptyRegistry(t *testing.T) {
	r := NewRegistry()
	jh := NewJSONHandler(r)

	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "{}") && len(output) < 10 {
		t.Error("Empty registry should produce valid JSON")
	}
}

func TestJSONHandler_AllTypes(t *testing.T) {
	r := NewRegistry()

	// Register all metric types
	r.RegisterCounter(NewCounter("counter", "test"))
	r.RegisterGauge(NewGauge("gauge", "test"))
	r.RegisterHistogram(NewHistogram("histogram", "test"))
	r.RegisterCounterVec(NewCounterVec("countervec", "test", []string{"a"}))
	r.RegisterGaugeVec(NewGaugeVec("gaugevec", "test", []string{"a"}))
	r.RegisterHistogramVec(NewHistogramVec("histogramvec", "test", []string{"a"}))

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check all types are present
	if !strings.Contains(output, `"type": "counter"`) {
		t.Error("Output missing counter")
	}
	if !strings.Contains(output, `"type": "gauge"`) {
		t.Error("Output missing gauge")
	}
	if !strings.Contains(output, `"type": "histogram"`) {
		t.Error("Output missing histogram")
	}
	if !strings.Contains(output, `"type": "counter_vec"`) {
		t.Error("Output missing counter_vec")
	}
	if !strings.Contains(output, `"type": "gauge_vec"`) {
		t.Error("Output missing gauge_vec")
	}
	if !strings.Contains(output, `"type": "histogram_vec"`) {
		t.Error("Output missing histogram_vec")
	}
}

// ==================== Integration Tests ====================

func TestMetricsIntegration(t *testing.T) {
	// Create a registry and register various metrics
	r := NewRegistry()

	// Counter
	requests := NewCounter("requests_total", "Total HTTP requests")
	requests.Add(100)
	r.RegisterCounter(requests)

	// Gauge
	connections := NewGauge("active_connections", "Number of active connections")
	connections.Set(42)
	r.RegisterGauge(connections)

	// Histogram
	duration := NewHistogram("request_duration_ms", "Request duration in milliseconds")
	for i := 1; i <= 100; i++ {
		duration.Observe(float64(i))
	}
	r.RegisterHistogram(duration)

	// CounterVec
	requestsByMethod := NewCounterVec("requests_by_method", "Requests by HTTP method", []string{"method", "status"})
	requestsByMethod.With("GET", "200").Add(50)
	requestsByMethod.With("POST", "201").Add(30)
	requestsByMethod.With("GET", "404").Add(20)
	r.RegisterCounterVec(requestsByMethod)

	// GaugeVec
	connectionsByPool := NewGaugeVec("connections_by_pool", "Connections by pool", []string{"pool"})
	connectionsByPool.With("pool1").Set(10)
	connectionsByPool.With("pool2").Set(20)
	r.RegisterGaugeVec(connectionsByPool)

	// HistogramVec
	durationByMethod := NewHistogramVec("duration_by_method", "Duration by HTTP method", []string{"method"})
	durationByMethod.With("GET").Observe(10)
	durationByMethod.With("GET").Observe(20)
	durationByMethod.With("POST").Observe(30)
	r.RegisterHistogramVec(durationByMethod)

	// Test Prometheus output
	ph := NewPrometheusHandler(r)
	var promBuf strings.Builder
	if err := ph.WriteMetrics(&promBuf); err != nil {
		t.Fatalf("Prometheus WriteTo failed: %v", err)
	}
	promOutput := promBuf.String()

	// Verify Prometheus output
	if !strings.Contains(promOutput, "requests_total 100") {
		t.Error("Prometheus output missing requests_total")
	}
	if !strings.Contains(promOutput, "active_connections 42") {
		t.Error("Prometheus output missing active_connections")
	}
	if !strings.Contains(promOutput, `requests_by_method{method="GET",status="200"} 50`) {
		t.Error("Prometheus output missing requests_by_method GET 200")
	}

	// Test JSON output
	jh := NewJSONHandler(r)
	var jsonBuf strings.Builder
	if err := jh.WriteMetrics(&jsonBuf); err != nil {
		t.Fatalf("JSON WriteTo failed: %v", err)
	}
	jsonOutput := jsonBuf.String()

	// Verify JSON output
	if !strings.Contains(jsonOutput, `"requests_total"`) {
		t.Error("JSON output missing requests_total")
	}
	if !strings.Contains(jsonOutput, `"active_connections"`) {
		t.Error("JSON output missing active_connections")
	}
}

// ==================== Edge Cases ====================

func TestCounterVec_EmptyLabels(t *testing.T) {
	cv := NewCounterVec("test", "test", []string{})
	c := cv.With()
	c.Inc()
	if c.Get() != 1 {
		t.Errorf("Get() = %d, want 1", c.Get())
	}
}

func TestGaugeVec_EmptyLabels(t *testing.T) {
	gv := NewGaugeVec("test", "test", []string{})
	g := gv.With()
	g.Set(42)
	if g.Get() != 42 {
		t.Errorf("Get() = %f, want 42", g.Get())
	}
}

func TestHistogramVec_EmptyLabels(t *testing.T) {
	hv := NewHistogramVec("test", "test", []string{})
	h := hv.With()
	h.Observe(1.0)
	if h.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h.GetCount())
	}
}

func TestCounterVec_MoreValuesThanLabels(t *testing.T) {
	cv := NewCounterVec("test", "test", []string{"a", "b"})
	// Extra values are still used in the key
	c := cv.With("x", "y", "z")
	c.Inc()

	// Same extra values should return same counter
	c2 := cv.With("x", "y", "z")
	if c != c2 {
		t.Error("Should return same counter for same values including extras")
	}
}

func TestCounterVec_FewerValuesThanLabels(t *testing.T) {
	cv := NewCounterVec("test", "test", []string{"a", "b", "c"})
	// Fewer values create a shorter key
	c := cv.With("x", "y")
	c.Inc()

	// Should still work
	if c.Get() != 1 {
		t.Errorf("Get() = %d, want 1", c.Get())
	}
}

func TestPrometheusHandler_EmptyLabels(t *testing.T) {
	r := NewRegistry()

	cv := NewCounterVec("test", "test", []string{})
	cv.With().Inc()
	r.RegisterCounterVec(cv)

	ph := NewPrometheusHandler(r)
	var buf strings.Builder
	if err := ph.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test 1") {
		t.Error("Output missing counter value")
	}
}

func TestJSONHandler_EmptyLabels(t *testing.T) {
	r := NewRegistry()

	cv := NewCounterVec("test", "test", []string{})
	cv.With().Inc()
	r.RegisterCounterVec(cv)

	jh := NewJSONHandler(r)
	var buf strings.Builder
	if err := jh.WriteMetrics(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"type": "counter_vec"`) {
		t.Error("Output missing counter_vec type")
	}
}

func TestHistogram_ObserveZero(t *testing.T) {
	h := NewHistogram("test", "test")
	h.Observe(0)
	if h.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h.GetCount())
	}
	if h.GetSum() != 0 {
		t.Errorf("GetSum() = %f, want 0", h.GetSum())
	}
}

func TestHistogram_ObserveNegative(t *testing.T) {
	h := NewHistogram("test", "test")
	h.Observe(-5)
	if h.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h.GetCount())
	}
	if h.GetSum() != -5 {
		t.Errorf("GetSum() = %f, want -5", h.GetSum())
	}
}

func TestHistogram_ObserveVeryLarge(t *testing.T) {
	h := NewHistogram("test", "test")
	h.Observe(1000000) // Larger than any default bucket
	if h.GetCount() != 1 {
		t.Errorf("GetCount() = %d, want 1", h.GetCount())
	}
	// Should go into +Inf bucket
	lastBucket := h.GetBucketCount(len(DefaultBuckets))
	if lastBucket != 1 {
		t.Errorf("Last bucket count = %d, want 1", lastBucket)
	}
}

func TestGauge_NaN(t *testing.T) {
	g := NewGauge("test", "test")
	g.Set(math.NaN())
	if !math.IsNaN(g.Get()) {
		t.Error("Should be able to set NaN")
	}
}

func TestGauge_Infinity(t *testing.T) {
	g := NewGauge("test", "test")
	g.Set(math.Inf(1))
	if !math.IsInf(g.Get(), 1) {
		t.Error("Should be able to set +Inf")
	}

	g.Set(math.Inf(-1))
	if !math.IsInf(g.Get(), -1) {
		t.Error("Should be able to set -Inf")
	}
}

// ==================== validateValues Tests ====================

func TestCounterVec_ValidateValues(t *testing.T) {
	cv := NewCounterVec("test", "test", []string{"method", "status"})

	// Correct number of values
	if err := cv.validateValues([]string{"GET", "200"}); err != nil {
		t.Errorf("validateValues with correct count returned error: %v", err)
	}

	// Too few values
	if err := cv.validateValues([]string{"GET"}); err == nil {
		t.Error("validateValues with too few values should return error")
	}

	// Too many values
	if err := cv.validateValues([]string{"GET", "200", "extra"}); err == nil {
		t.Error("validateValues with too many values should return error")
	}

	// Empty values when labels exist
	if err := cv.validateValues([]string{}); err == nil {
		t.Error("validateValues with empty values and non-empty labels should return error")
	}

	// No labels, no values
	cvEmpty := NewCounterVec("empty", "test", []string{})
	if err := cvEmpty.validateValues([]string{}); err != nil {
		t.Errorf("validateValues with no labels and no values returned error: %v", err)
	}

	// No labels, but values provided
	if err := cvEmpty.validateValues([]string{"extra"}); err == nil {
		t.Error("validateValues with no labels but values provided should return error")
	}
}

func TestGaugeVec_ValidateValues(t *testing.T) {
	gv := NewGaugeVec("test", "test", []string{"pool", "backend"})

	// Correct number of values
	if err := gv.validateValues([]string{"web", "b1"}); err != nil {
		t.Errorf("validateValues with correct count returned error: %v", err)
	}

	// Too few values
	if err := gv.validateValues([]string{"web"}); err == nil {
		t.Error("validateValues with too few values should return error")
	}

	// Too many values
	if err := gv.validateValues([]string{"web", "b1", "extra"}); err == nil {
		t.Error("validateValues with too many values should return error")
	}

	// Empty values when labels exist
	if err := gv.validateValues([]string{}); err == nil {
		t.Error("validateValues with empty values and non-empty labels should return error")
	}

	// No labels, no values
	gvEmpty := NewGaugeVec("empty", "test", []string{})
	if err := gvEmpty.validateValues([]string{}); err != nil {
		t.Errorf("validateValues with no labels and no values returned error: %v", err)
	}

	// No labels, but values provided
	if err := gvEmpty.validateValues([]string{"extra"}); err == nil {
		t.Error("validateValues with no labels but values provided should return error")
	}
}

func TestHistogramVec_ValidateValues(t *testing.T) {
	hv := NewHistogramVec("test", "test", []string{"method", "path"})

	// Correct number of values
	if err := hv.validateValues([]string{"GET", "/api"}); err != nil {
		t.Errorf("validateValues with correct count returned error: %v", err)
	}

	// Too few values
	if err := hv.validateValues([]string{"GET"}); err == nil {
		t.Error("validateValues with too few values should return error")
	}

	// Too many values
	if err := hv.validateValues([]string{"GET", "/api", "extra"}); err == nil {
		t.Error("validateValues with too many values should return error")
	}

	// Empty values when labels exist
	if err := hv.validateValues([]string{}); err == nil {
		t.Error("validateValues with empty values and non-empty labels should return error")
	}

	// No labels, no values
	hvEmpty := NewHistogramVec("empty", "test", []string{})
	if err := hvEmpty.validateValues([]string{}); err != nil {
		t.Errorf("validateValues with no labels and no values returned error: %v", err)
	}

	// No labels, but values provided
	if err := hvEmpty.validateValues([]string{"extra"}); err == nil {
		t.Error("validateValues with no labels but values provided should return error")
	}
}
