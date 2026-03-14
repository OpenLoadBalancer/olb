package metrics

import (
	"strings"
	"sync"
	"testing"
)

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
	err := h.WriteTo(&buf)
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

func TestJSONHandler(t *testing.T) {
	r := NewRegistry()

	// Register metrics
	c := NewCounter("requests_total", "Total requests")
	c.Inc()
	r.RegisterCounter(c)

	// Write JSON format
	h := NewJSONHandler(r)
	var buf strings.Builder
	err := h.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Check output contains expected content
	if !strings.Contains(output, "\"type\": \"counter\"") {
		t.Error("Output missing counter type")
	}

	if !strings.Contains(output, "\"value\": 1") {
		t.Error("Output missing counter value")
	}
}
