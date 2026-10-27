package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/metrics"
)

func TestMetricsMiddleware_Name(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)
	if m.Name() != "metrics" {
		t.Errorf("expected name 'metrics', got %s", m.Name())
	}
}

func TestMetricsMiddleware_Priority(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)
	if m.Priority() != PriorityMetrics {
		t.Errorf("expected priority %d, got %d", PriorityMetrics, m.Priority())
	}
}

func TestMetricsMiddleware_RequestsTotal(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make multiple requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Check counter value
	counter := m.requestsTotal.With("GET", "unknown", "2xx")
	if counter.Get() != 5 {
		t.Errorf("expected requests_total to be 5, got %d", counter.Get())
	}
}

func TestMetricsMiddleware_StatusCodeClasses(t *testing.T) {
	testCases := []struct {
		status      int
		statusClass string
	}{
		{200, "2xx"},
		{201, "2xx"},
		{301, "3xx"},
		{400, "4xx"},
		{404, "4xx"},
		{500, "5xx"},
	}

	for _, tc := range testCases {
		// Create a new registry and middleware for each test case
		// to avoid counter accumulation
		registry := metrics.NewRegistry()
		m := NewMetricsMiddleware(registry)

		handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))

		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		counter := m.requestsTotal.With("POST", "unknown", tc.statusClass)
		if counter.Get() != 1 {
			t.Errorf("status %d: expected counter for class %s to be 1, got %d",
				tc.status, tc.statusClass, counter.Get())
		}
	}
}

func TestMetricsMiddleware_DurationHistogram(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Ensure some duration
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Check histogram has observations
	histogram := m.durationSeconds.With("GET", "unknown")
	if histogram.GetCount() != 1 {
		t.Errorf("expected duration histogram count to be 1, got %d", histogram.GetCount())
	}

	// Check sum is positive
	if histogram.GetSum() <= 0 {
		t.Errorf("expected duration sum to be positive, got %f", histogram.GetSum())
	}
}

func TestMetricsMiddleware_ResponseSizeHistogram(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	responseBody := "Hello, World! This is a test."
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Check histogram has observations
	histogram := m.responseSizeBytes.With("GET", "unknown")
	if histogram.GetCount() != 1 {
		t.Errorf("expected response size histogram count to be 1, got %d", histogram.GetCount())
	}

	// Check sum matches response size
	expectedSize := float64(len(responseBody))
	if histogram.GetSum() != expectedSize {
		t.Errorf("expected response size sum to be %f, got %f", expectedSize, histogram.GetSum())
	}
}

func TestMetricsMiddleware_RequestSizeHistogram(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	requestBody := "This is a test request body with some content."
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(requestBody))
	req.ContentLength = int64(len(requestBody))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Note: Request size tracking depends on the request body being read
	// In this test, we're just verifying the histogram exists
	histogram := m.requestSizeBytes.With("POST", "unknown")
	if histogram == nil {
		t.Error("expected request size histogram to exist")
	}
}

func TestMetricsMiddleware_ActiveRequestsGauge(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	// Use a channel to control request timing
	startCh := make(chan struct{})
	doneCh := make(chan struct{})

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(startCh)
		<-doneCh // Wait for signal to complete
		w.WriteHeader(http.StatusOK)
	}))

	// Start request in goroutine
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}()

	// Wait for request to start
	<-startCh
	time.Sleep(10 * time.Millisecond) // Give time for gauge to increment

	// Check active requests gauge
	gauge := m.activeRequests.With("GET")
	if gauge.Get() != 1 {
		t.Errorf("expected active requests to be 1, got %f", gauge.Get())
	}

	// Signal request to complete
	close(doneCh)
	time.Sleep(10 * time.Millisecond) // Give time for gauge to decrement

	// Check active requests gauge after completion
	if gauge.Get() != 0 {
		t.Errorf("expected active requests to be 0 after completion, got %f", gauge.Get())
	}
}

func TestMetricsMiddleware_DifferentMethods(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		counter := m.requestsTotal.With(method, "unknown", "2xx")
		if counter.Get() != 1 {
			t.Errorf("expected %s requests_total to be 1, got %d", method, counter.Get())
		}
	}
}

func TestMetricsMiddleware_MultipleRequests(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests with same method/route/status
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	counter := m.requestsTotal.With("GET", "unknown", "2xx")
	if counter.Get() != 10 {
		t.Errorf("expected requests_total to be 10, got %d", counter.Get())
	}

	// Check histogram has 10 observations
	histogram := m.durationSeconds.With("GET", "unknown")
	if histogram.GetCount() != 10 {
		t.Errorf("expected duration histogram count to be 10, got %d", histogram.GetCount())
	}
}

func TestMetricsMiddleware_DefaultRegistry(t *testing.T) {
	// Test with nil registry (should use default)
	m := NewMetricsMiddleware(nil)
	if m.registry != metrics.DefaultRegistry {
		t.Error("expected middleware to use default registry when nil is passed")
	}
}

func TestMetricsMiddleware_Getters(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	if m.GetRequestsTotal() == nil {
		t.Error("GetRequestsTotal should not return nil")
	}
	if m.GetDurationSeconds() == nil {
		t.Error("GetDurationSeconds should not return nil")
	}
	if m.GetRequestSizeBytes() == nil {
		t.Error("GetRequestSizeBytes should not return nil")
	}
	if m.GetResponseSizeBytes() == nil {
		t.Error("GetResponseSizeBytes should not return nil")
	}
	if m.GetActiveRequests() == nil {
		t.Error("GetActiveRequests should not return nil")
	}
}

func TestStatusCodeClass(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{100, "1xx"},
		{150, "1xx"},
		{199, "1xx"},
		{200, "2xx"},
		{204, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{99, "99"},   // Edge case
		{600, "600"}, // Edge case
	}

	for _, tt := range tests {
		result := statusCodeClass(tt.code)
		if result != tt.expected {
			t.Errorf("statusCodeClass(%d) = %s, expected %s", tt.code, result, tt.expected)
		}
	}
}

func TestMetricsMiddleware_GetMetricValue(t *testing.T) {
	registry := metrics.NewRegistry()
	m := NewMetricsMiddleware(registry)

	// Make a request to populate metrics
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Test GetMetricValue for requests_total
	val, err := m.GetMetricValue("requests_total", "GET", "unknown", "2xx")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != 1 {
		t.Errorf("expected requests_total to be 1, got %f", val)
	}

	// Test GetMetricValue for active_requests
	val, err = m.GetMetricValue("active_requests", "GET")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != 0 { // Should be 0 since request completed
		t.Errorf("expected active_requests to be 0, got %f", val)
	}

	// Test GetMetricValue for unknown metric
	_, err = m.GetMetricValue("unknown_metric")
	if err == nil {
		t.Error("expected error for unknown metric")
	}
}

// Config reload rebuilds the middleware chain against the same registry.
// The second NewMetricsMiddleware must feed the registered vectors, or
// /metrics freezes at its pre-reload values.
func TestMetricsMiddleware_ReusesRegisteredVectorsAfterReload(t *testing.T) {
	registry := metrics.NewRegistry()
	_ = NewMetricsMiddleware(registry)
	reloaded := NewMetricsMiddleware(registry)

	handler := reloaded.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 3; i++ {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))
	}

	registered := registry.GetCounterVec("http_requests_total")
	if registered == nil {
		t.Fatal("http_requests_total not registered")
	}
	if got := registered.With("GET", "unknown", "2xx").Get(); got != 3 {
		t.Errorf("registered http_requests_total = %d after reload, want 3", got)
	}
}
