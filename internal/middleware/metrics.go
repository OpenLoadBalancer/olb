// Package middleware provides HTTP middleware components for OpenLoadBalancer.
package middleware

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/openloadbalancer/olb/internal/metrics"
)

// MetricsMiddleware collects HTTP metrics.
type MetricsMiddleware struct {
	registry *metrics.Registry
	prefix   string

	// Pre-created metric vectors
	requestsTotal     *metrics.CounterVec
	durationSeconds   *metrics.HistogramVec
	requestSizeBytes  *metrics.HistogramVec
	responseSizeBytes *metrics.HistogramVec
	activeRequests    *metrics.GaugeVec
}

// NewMetricsMiddleware creates a new Metrics middleware.
func NewMetricsMiddleware(registry *metrics.Registry) *MetricsMiddleware {
	if registry == nil {
		registry = metrics.DefaultRegistry
	}

	m := &MetricsMiddleware{
		registry: registry,
		prefix:   "http",
	}

	// Create or get metric vectors
	m.requestsTotal = registry.CounterVecOrRegister(metrics.NewCounterVec(
		"http_requests_total",
		"Total number of HTTP requests",
		[]string{"method", "route", "status"},
	))

	m.durationSeconds = registry.HistogramVecOrRegister(metrics.NewHistogramVec(
		"http_request_duration_seconds",
		"HTTP request duration in seconds",
		[]string{"method", "route"},
	))

	m.requestSizeBytes = registry.HistogramVecOrRegister(metrics.NewHistogramVecWithBuckets(
		"http_request_size_bytes",
		"HTTP request size in bytes",
		[]string{"method", "route"},
		[]float64{100, 1000, 10000, 100000, 1000000, 10000000}, // 100B to 10MB
	))

	m.responseSizeBytes = registry.HistogramVecOrRegister(metrics.NewHistogramVecWithBuckets(
		"http_response_size_bytes",
		"HTTP response size in bytes",
		[]string{"method", "route"},
		[]float64{100, 1000, 10000, 100000, 1000000, 10000000}, // 100B to 10MB
	))

	m.activeRequests = registry.GaugeVecOrRegister(metrics.NewGaugeVec(
		"http_active_requests",
		"Number of active HTTP requests",
		[]string{"method"},
	))

	return m
}

// Name returns the middleware name.
func (m *MetricsMiddleware) Name() string {
	return "metrics"
}

// Priority returns the middleware priority.
func (m *MetricsMiddleware) Priority() int {
	return PriorityMetrics
}

// Wrap wraps the next handler with metrics collection.
func (m *MetricsMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create request context
		ctx := NewRequestContext(r, w)
		defer ctx.Release()

		// Get route name (will be empty if not matched yet)
		routeName := ""
		if ctx.Route != nil {
			routeName = ctx.Route.Name
		}

		// Increment active requests gauge
		m.activeRequests.With(r.Method).Inc()
		defer m.activeRequests.With(r.Method).Dec()

		// Call next handler
		next.ServeHTTP(ctx.Response, r)

		// Update context with response data
		ctx.StatusCode = ctx.Response.Status()
		ctx.BytesOut = ctx.Response.BytesWritten()

		// Get route name from context if it was set during request processing
		if ctx.Route != nil && routeName == "" {
			routeName = ctx.Route.Name
		}
		if routeName == "" {
			routeName = "unknown"
		}

		// Record metrics
		m.recordMetrics(ctx, routeName)
	})
}

// recordMetrics records all metrics for the request.
func (m *MetricsMiddleware) recordMetrics(ctx *RequestContext, routeName string) {
	method := ctx.Request.Method
	statusClass := statusCodeClass(ctx.StatusCode)

	// Increment request counter
	m.requestsTotal.With(method, routeName, statusClass).Inc()

	// Observe duration
	duration := ctx.Duration().Seconds()
	m.durationSeconds.With(method, routeName).Observe(duration)

	// Observe request size
	if ctx.BytesIn > 0 {
		m.requestSizeBytes.With(method, routeName).Observe(float64(ctx.BytesIn))
	}

	// Observe response size
	if ctx.BytesOut > 0 {
		m.responseSizeBytes.With(method, routeName).Observe(float64(ctx.BytesOut))
	}
}

// statusCodeClass returns the status code class (2xx, 3xx, 4xx, 5xx).
func statusCodeClass(code int) string {
	switch code / 100 {
	case 1:
		return "1xx"
	case 2:
		return "2xx"
	case 3:
		return "3xx"
	case 4:
		return "4xx"
	case 5:
		return "5xx"
	default:
		return strconv.Itoa(code)
	}
}

// GetRequestsTotal returns the requests total counter vector.
func (m *MetricsMiddleware) GetRequestsTotal() *metrics.CounterVec {
	return m.requestsTotal
}

// GetDurationSeconds returns the duration histogram vector.
func (m *MetricsMiddleware) GetDurationSeconds() *metrics.HistogramVec {
	return m.durationSeconds
}

// GetRequestSizeBytes returns the request size histogram vector.
func (m *MetricsMiddleware) GetRequestSizeBytes() *metrics.HistogramVec {
	return m.requestSizeBytes
}

// GetResponseSizeBytes returns the response size histogram vector.
func (m *MetricsMiddleware) GetResponseSizeBytes() *metrics.HistogramVec {
	return m.responseSizeBytes
}

// GetActiveRequests returns the active requests gauge vector.
func (m *MetricsMiddleware) GetActiveRequests() *metrics.GaugeVec {
	return m.activeRequests
}

// GetMetricValue returns the value of a specific metric for testing.
func (m *MetricsMiddleware) GetMetricValue(metricName string, labelValues ...string) (float64, error) {
	switch metricName {
	case "requests_total":
		c := m.requestsTotal.With(labelValues...)
		return float64(c.Get()), nil
	case "active_requests":
		g := m.activeRequests.With(labelValues...)
		return g.Get(), nil
	default:
		return 0, fmt.Errorf("unknown metric: %s", metricName)
	}
}
