// Package metrics provides HTTP request metrics collection middleware.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures metrics middleware.
type Config struct {
	Enabled        bool      // Enable metrics collection
	Namespace      string    // Metrics namespace prefix
	Subsystem      string    // Metrics subsystem
	ExcludePaths   []string  // Paths to exclude from metrics
	ExcludeMethods []string  // HTTP methods to exclude
	EnableLatency  bool      // Enable latency histograms
	EnableSize     bool      // Enable request/response size metrics
	EnableActive   bool      // Enable active requests gauge
	LatencyBuckets []float64 // Latency histogram buckets (in seconds)
}

// DefaultConfig returns default metrics configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:        false,
		Namespace:      "olb",
		Subsystem:      "http",
		EnableLatency:  true,
		EnableSize:     true,
		EnableActive:   true,
		LatencyBuckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}
}

// MetricsCollector collects HTTP request metrics.
type MetricsCollector struct {
	config Config

	// Counters
	requestsTotal    *Counter
	requestsByStatus map[string]*Counter

	// Gauges
	activeRequests *Gauge

	// Histograms
	requestDuration *Histogram
	requestSize     *Histogram
	responseSize    *Histogram

	mu sync.RWMutex
}

// Counter is a simple counter metric.
type Counter struct {
	value int64
	mu    sync.RWMutex
}

// Gauge is a gauge metric that can go up and down.
type Gauge struct {
	value int64
	mu    sync.RWMutex
}

// Histogram is a histogram metric.
type Histogram struct {
	buckets []float64
	counts  []int64
	sum     float64
	count   int64
	mu      sync.RWMutex
}

// NewCounter creates a new counter.
func NewCounter() *Counter {
	return &Counter{}
}

// Inc increments the counter.
func (c *Counter) Inc() {
	c.mu.Lock()
	c.value++
	c.mu.Unlock()
}

// Add adds n to the counter.
func (c *Counter) Add(n int64) {
	c.mu.Lock()
	c.value += n
	c.mu.Unlock()
}

// Value returns the counter value.
func (c *Counter) Value() int64 {
	c.mu.RLock()
	v := c.value
	c.mu.RUnlock()
	return v
}

// NewGauge creates a new gauge.
func NewGauge() *Gauge {
	return &Gauge{}
}

// Inc increments the gauge.
func (g *Gauge) Inc() {
	g.mu.Lock()
	g.value++
	g.mu.Unlock()
}

// Dec decrements the gauge.
func (g *Gauge) Dec() {
	g.mu.Lock()
	g.value--
	g.mu.Unlock()
}

// Set sets the gauge value.
func (g *Gauge) Set(v int64) {
	g.mu.Lock()
	g.value = v
	g.mu.Unlock()
}

// Value returns the gauge value.
func (g *Gauge) Value() int64 {
	g.mu.RLock()
	v := g.value
	g.mu.RUnlock()
	return v
}

// NewHistogram creates a new histogram with buckets.
func NewHistogram(buckets []float64) *Histogram {
	return &Histogram{
		buckets: buckets,
		counts:  make([]int64, len(buckets)+1), // +1 for +Inf bucket
	}
}

// Observe adds a value to the histogram.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	h.sum += v
	h.count++

	// Find bucket
	for i, b := range h.buckets {
		if v <= b {
			h.counts[i]++
			h.mu.Unlock()
			return
		}
	}
	// +Inf bucket
	h.counts[len(h.buckets)]++
	h.mu.Unlock()
}

// Snapshot returns a snapshot of the histogram.
func (h *Histogram) Snapshot() (buckets []float64, counts []int64, sum float64, count int64) {
	h.mu.RLock()
	buckets = make([]float64, len(h.buckets))
	copy(buckets, h.buckets)
	counts = make([]int64, len(h.counts))
	copy(counts, h.counts)
	sum = h.sum
	count = h.count
	h.mu.RUnlock()
	return
}

// Middleware provides metrics collection functionality.
type Middleware struct {
	config    Config
	collector *MetricsCollector
}

// responseWriter wraps http.ResponseWriter to capture size.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bytesSent  int
	written    bool
}

// New creates a new metrics middleware.
func New(config Config) *Middleware {
	collector := &MetricsCollector{
		config:           config,
		requestsTotal:    NewCounter(),
		requestsByStatus: make(map[string]*Counter),
		activeRequests:   NewGauge(),
	}

	if config.EnableLatency {
		collector.requestDuration = NewHistogram(config.LatencyBuckets)
	}
	if config.EnableSize {
		collector.requestSize = NewHistogram([]float64{100, 1000, 10000, 100000, 1000000, 10000000})
		collector.responseSize = NewHistogram([]float64{100, 1000, 10000, 100000, 1000000, 10000000})
	}

	return &Middleware{
		config:    config,
		collector: collector,
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "metrics"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 85 // After Logging (80), before RequestID (90)
}

// Wrap wraps the handler with metrics collection.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check excluded methods
		for _, method := range m.config.ExcludeMethods {
			if r.Method == method {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Track active requests
		if m.config.EnableActive {
			m.collector.activeRequests.Inc()
			defer m.collector.activeRequests.Dec()
		}

		start := time.Now()

		// Wrap response writer to capture status and size
		rec := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		duration := time.Since(start).Seconds()

		// Record metrics
		m.recordMetrics(r, rec, duration)
	})
}

// recordMetrics records the request metrics.
func (m *Middleware) recordMetrics(r *http.Request, rec *responseWriter, duration float64) {
	// Total requests
	m.collector.requestsTotal.Inc()

	// Requests by status code
	statusKey := strconv.Itoa(rec.statusCode)
	m.collector.mu.Lock()
	if m.collector.requestsByStatus[statusKey] == nil {
		m.collector.requestsByStatus[statusKey] = NewCounter()
	}
	m.collector.requestsByStatus[statusKey].Inc()
	m.collector.mu.Unlock()

	// Latency
	if m.config.EnableLatency && m.collector.requestDuration != nil {
		m.collector.requestDuration.Observe(duration)
	}

	// Response size
	if m.config.EnableSize && m.collector.responseSize != nil {
		m.collector.responseSize.Observe(float64(rec.bytesSent))
	}

	// Request size (from Content-Length header)
	if m.config.EnableSize && m.collector.requestSize != nil {
		if r.ContentLength > 0 {
			m.collector.requestSize.Observe(float64(r.ContentLength))
		}
	}
}

// GetCollector returns the metrics collector.
func (m *Middleware) GetCollector() *MetricsCollector {
	return m.collector
}

// GetRequestsTotal returns total requests count.
func (mc *MetricsCollector) GetRequestsTotal() int64 {
	return mc.requestsTotal.Value()
}

// GetRequestsByStatus returns requests count for a specific status code.
func (mc *MetricsCollector) GetRequestsByStatus(status int) int64 {
	mc.mu.RLock()
	counter := mc.requestsByStatus[strconv.Itoa(status)]
	mc.mu.RUnlock()
	if counter == nil {
		return 0
	}
	return counter.Value()
}

// GetActiveRequests returns current active requests count.
func (mc *MetricsCollector) GetActiveRequests() int64 {
	return mc.activeRequests.Value()
}

// GetRequestDurationSnapshot returns latency histogram snapshot.
func (mc *MetricsCollector) GetRequestDurationSnapshot() (buckets []float64, counts []int64, sum float64, count int64) {
	if mc.requestDuration == nil {
		return nil, nil, 0, 0
	}
	return mc.requestDuration.Snapshot()
}

// GetResponseSizeSnapshot returns response size histogram snapshot.
func (mc *MetricsCollector) GetResponseSizeSnapshot() (buckets []float64, counts []int64, sum float64, count int64) {
	if mc.responseSize == nil {
		return nil, nil, 0, 0
	}
	return mc.responseSize.Snapshot()
}

// GetRequestSizeSnapshot returns request size histogram snapshot.
func (mc *MetricsCollector) GetRequestSizeSnapshot() (buckets []float64, counts []int64, sum float64, count int64) {
	if mc.requestSize == nil {
		return nil, nil, 0, 0
	}
	return mc.requestSize.Snapshot()
}

// Reset resets all metrics.
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	mc.requestsTotal = NewCounter()
	mc.requestsByStatus = make(map[string]*Counter)
	mc.activeRequests.Set(0)
	if mc.requestDuration != nil {
		mc.requestDuration = NewHistogram(mc.config.LatencyBuckets)
	}
	if mc.requestSize != nil {
		mc.requestSize = NewHistogram([]float64{100, 1000, 10000, 100000, 1000000, 10000000})
	}
	if mc.responseSize != nil {
		mc.responseSize = NewHistogram([]float64{100, 1000, 10000, 100000, 1000000, 10000000})
	}
	mc.mu.Unlock()
}

// WriteHeader captures the status code.
func (rec *responseWriter) WriteHeader(code int) {
	if rec.written {
		return
	}
	rec.statusCode = code
	rec.written = true
	rec.ResponseWriter.WriteHeader(code)
}

// Write captures the number of bytes written.
func (rec *responseWriter) Write(p []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(p)
	rec.bytesSent += n
	rec.written = true
	return n, err
}

// Header returns the header map.
func (rec *responseWriter) Header() http.Header {
	return rec.ResponseWriter.Header()
}
