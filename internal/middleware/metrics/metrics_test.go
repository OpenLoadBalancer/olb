package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMetrics_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw := New(config)

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}

	if mw.GetCollector().GetRequestsTotal() != 0 {
		t.Error("No metrics should be recorded when disabled")
	}
}

func TestMetrics_BasicRequest(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if mw.GetCollector().GetRequestsTotal() != 1 {
		t.Errorf("Expected 1 request, got %d", mw.GetCollector().GetRequestsTotal())
	}

	if mw.GetCollector().GetRequestsByStatus(200) != 1 {
		t.Errorf("Expected 1 status 200, got %d", mw.GetCollector().GetRequestsByStatus(200))
	}
}

func TestMetrics_StatusCodes(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	statuses := []int{200, 201, 400, 404, 500}
	for _, status := range statuses {
		handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if mw.GetCollector().GetRequestsTotal() != 5 {
		t.Errorf("Expected 5 requests, got %d", mw.GetCollector().GetRequestsTotal())
	}

	for _, status := range statuses {
		if mw.GetCollector().GetRequestsByStatus(status) != 1 {
			t.Errorf("Expected 1 status %d", status)
		}
	}
}

func TestMetrics_ResponseSize(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableSize = true

	mw := New(config)

	body := []byte("Hello, World!")
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	buckets, counts, sum, count := mw.GetCollector().GetResponseSizeSnapshot()
	if buckets == nil {
		t.Fatal("Response size histogram should be enabled")
	}

	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	if sum != float64(len(body)) {
		t.Errorf("Expected sum %d, got %f", len(body), sum)
	}

	// Check that something was recorded in a bucket
	totalCount := int64(0)
	for _, c := range counts {
		totalCount += c
	}
	if totalCount != 1 {
		t.Errorf("Expected total count 1 in buckets, got %d", totalCount)
	}
}

func TestMetrics_Latency(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableLatency = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	buckets, counts, sum, count := mw.GetCollector().GetRequestDurationSnapshot()
	if buckets == nil {
		t.Fatal("Duration histogram should be enabled")
	}

	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	if sum <= 0 {
		t.Error("Expected positive sum")
	}

	// Check that something was recorded
	totalCount := int64(0)
	for _, c := range counts {
		totalCount += c
	}
	if totalCount != 1 {
		t.Errorf("Expected total count 1 in buckets, got %d", totalCount)
	}
}

func TestMetrics_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludePaths = []string{"/health", "/metrics"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request to excluded path
	req := httptest.NewRequest("GET", "/health/live", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if mw.GetCollector().GetRequestsTotal() != 0 {
		t.Error("Metrics should not be recorded for excluded paths")
	}

	// Request to non-excluded path
	req2 := httptest.NewRequest("GET", "/api/users", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if mw.GetCollector().GetRequestsTotal() != 1 {
		t.Errorf("Expected 1 request, got %d", mw.GetCollector().GetRequestsTotal())
	}
}

func TestMetrics_ExcludeMethod(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.ExcludeMethods = []string{"OPTIONS", "HEAD"}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// OPTIONS request (excluded)
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if mw.GetCollector().GetRequestsTotal() != 0 {
		t.Error("Metrics should not be recorded for excluded methods")
	}

	// GET request (not excluded)
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if mw.GetCollector().GetRequestsTotal() != 1 {
		t.Errorf("Expected 1 request, got %d", mw.GetCollector().GetRequestsTotal())
	}
}

func TestMetrics_ActiveRequests(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableActive = true

	mw := New(config)

	// Check initial active count
	if mw.GetCollector().GetActiveRequests() != 0 {
		t.Errorf("Expected 0 active requests initially, got %d", mw.GetCollector().GetActiveRequests())
	}

	// Simulate a request that tracks active count
	var activeDuringRequest int64
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeDuringRequest = mw.GetCollector().GetActiveRequests()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if activeDuringRequest != 1 {
		t.Errorf("Expected 1 active request during handling, got %d", activeDuringRequest)
	}

	if mw.GetCollector().GetActiveRequests() != 0 {
		t.Errorf("Expected 0 active requests after completion, got %d", mw.GetCollector().GetActiveRequests())
	}
}

func TestMetrics_DisabledActive(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableActive = false

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if mw.GetCollector().GetActiveRequests() != 0 {
		t.Error("Active requests should be 0 when disabled")
	}
}

func TestMetrics_DisabledLatency(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableLatency = false

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	buckets, _, _, _ := mw.GetCollector().GetRequestDurationSnapshot()
	if buckets != nil {
		t.Error("Duration histogram should be nil when disabled")
	}
}

func TestMetrics_DisabledSize(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableSize = false

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("response"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	buckets, _, _, _ := mw.GetCollector().GetResponseSizeSnapshot()
	if buckets != nil {
		t.Error("Response size histogram should be nil when disabled")
	}
}

func TestMetrics_Reset(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Record some metrics
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if mw.GetCollector().GetRequestsTotal() != 5 {
		t.Errorf("Expected 5 requests before reset, got %d", mw.GetCollector().GetRequestsTotal())
	}

	// Reset
	mw.GetCollector().Reset()

	if mw.GetCollector().GetRequestsTotal() != 0 {
		t.Errorf("Expected 0 requests after reset, got %d", mw.GetCollector().GetRequestsTotal())
	}

	if mw.GetCollector().GetRequestsByStatus(200) != 0 {
		t.Errorf("Expected 0 status 200 after reset, got %d", mw.GetCollector().GetRequestsByStatus(200))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
	if config.Namespace != "olb" {
		t.Errorf("Default Namespace should be 'olb', got '%s'", config.Namespace)
	}
	if config.Subsystem != "http" {
		t.Errorf("Default Subsystem should be 'http', got '%s'", config.Subsystem)
	}
	if !config.EnableLatency {
		t.Error("Default EnableLatency should be true")
	}
	if !config.EnableSize {
		t.Error("Default EnableSize should be true")
	}
	if !config.EnableActive {
		t.Error("Default EnableActive should be true")
	}
	if len(config.LatencyBuckets) == 0 {
		t.Error("Default LatencyBuckets should not be empty")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Priority() != 85 {
		t.Errorf("Expected priority 85, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw := New(config)

	if mw.Name() != "metrics" {
		t.Errorf("Expected name 'metrics', got '%s'", mw.Name())
	}
}

func TestCounter(t *testing.T) {
	c := NewCounter()

	if c.Value() != 0 {
		t.Error("New counter should be 0")
	}

	c.Inc()
	if c.Value() != 1 {
		t.Errorf("Expected 1, got %d", c.Value())
	}

	c.Add(5)
	if c.Value() != 6 {
		t.Errorf("Expected 6, got %d", c.Value())
	}
}

func TestGauge(t *testing.T) {
	g := NewGauge()

	if g.Value() != 0 {
		t.Error("New gauge should be 0")
	}

	g.Inc()
	if g.Value() != 1 {
		t.Errorf("Expected 1, got %d", g.Value())
	}

	g.Dec()
	if g.Value() != 0 {
		t.Errorf("Expected 0, got %d", g.Value())
	}

	g.Set(100)
	if g.Value() != 100 {
		t.Errorf("Expected 100, got %d", g.Value())
	}
}

func TestHistogram(t *testing.T) {
	buckets := []float64{0.1, 0.5, 1.0, 2.0}
	h := NewHistogram(buckets)

	// Observe some values
	h.Observe(0.05) // bucket 0
	h.Observe(0.3)  // bucket 1
	h.Observe(0.7)  // bucket 2
	h.Observe(1.5)  // bucket 3
	h.Observe(5.0)  // +Inf bucket

	b, counts, sum, count := h.Snapshot()

	if count != 5 {
		t.Errorf("Expected count 5, got %d", count)
	}

	expectedSum := 0.05 + 0.3 + 0.7 + 1.5 + 5.0
	if sum != expectedSum {
		t.Errorf("Expected sum %f, got %f", expectedSum, sum)
	}

	if len(b) != len(buckets) {
		t.Errorf("Expected %d buckets, got %d", len(buckets), len(b))
	}

	if len(counts) != len(buckets)+1 {
		t.Errorf("Expected %d counts (including +Inf), got %d", len(buckets)+1, len(counts))
	}

	// Check bucket counts
	if counts[0] != 1 { // 0.05 in bucket 0.1
		t.Errorf("Expected bucket[0] = 1, got %d", counts[0])
	}
	if counts[1] != 1 { // 0.3 in bucket 0.5
		t.Errorf("Expected bucket[1] = 1, got %d", counts[1])
	}
	if counts[4] != 1 { // 5.0 in +Inf bucket
		t.Errorf("Expected bucket[+Inf] = 1, got %d", counts[4])
	}
}

func TestResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rw.statusCode)
	}

	// Test double WriteHeader
	rw.WriteHeader(http.StatusOK)
	if rw.statusCode != http.StatusCreated {
		t.Error("Second WriteHeader should not change status")
	}

	// Test Write
	data := []byte("test data")
	n, err := rw.Write(data)
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected %d bytes written, got %d", len(data), n)
	}
	if rw.bytesSent != len(data) {
		t.Errorf("Expected bytesSent %d, got %d", len(data), rw.bytesSent)
	}

	// Test Header
	h := rw.Header()
	if h == nil {
		t.Error("Header() should return non-nil map")
	}
}

func TestGetRequestsByStatus_NotFound(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	mw := New(config)

	// Status 404 was never recorded
	if mw.GetCollector().GetRequestsByStatus(404) != 0 {
		t.Error("Unrecorded status should return 0")
	}
}

func TestHistogramSnapshot(t *testing.T) {
	h := NewHistogram([]float64{1.0, 2.0, 3.0})

	// Get snapshot of empty histogram
	buckets, counts, sum, count := h.Snapshot()

	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}
	if sum != 0 {
		t.Errorf("Expected sum 0, got %f", sum)
	}
	if len(buckets) != 3 {
		t.Errorf("Expected 3 buckets, got %d", len(buckets))
	}
	if len(counts) != 4 { // 3 buckets + Inf
		t.Errorf("Expected 4 counts, got %d", len(counts))
	}
}

func TestCustomLatencyBuckets(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.LatencyBuckets = []float64{0.001, 0.01, 0.1, 1.0}

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	buckets, _, _, _ := mw.GetCollector().GetRequestDurationSnapshot()
	if len(buckets) != 4 {
		t.Errorf("Expected 4 custom buckets, got %d", len(buckets))
	}
}

func TestMultipleRequestsConcurrent(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.EnableActive = true

	mw := New(config)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	// Run multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if mw.GetCollector().GetRequestsTotal() != 10 {
		t.Errorf("Expected 10 requests, got %d", mw.GetCollector().GetRequestsTotal())
	}

	if mw.GetCollector().GetActiveRequests() != 0 {
		t.Errorf("Expected 0 active requests after completion, got %d", mw.GetCollector().GetActiveRequests())
	}
}
