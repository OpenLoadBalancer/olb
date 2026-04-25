// Package trace provides distributed tracing middleware for OpenLoadBalancer.
// Supports W3C Trace Context and B3 propagation formats.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures tracing middleware.
type Config struct {
	Enabled         bool     // Enable tracing
	ServiceName     string   // Service name for spans
	ServiceVersion  string   // Service version
	Propagators     []string // Propagation formats: "w3c", "b3", "b3multi", "jaeger"
	SampleRate      float64  // Sampling rate (0.0 to 1.0)
	BaggageHeaders  []string // Headers to propagate as baggage
	ExcludePaths    []string // Paths to exclude from tracing
	MaxBaggageItems int      // Max baggage items per request
	MaxBaggageSize  int      // Max total baggage size in bytes
}

// DefaultConfig returns default trace configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		ServiceName:     "openloadbalancer",
		ServiceVersion:  "0.1.0",
		Propagators:     []string{"w3c", "b3"},
		SampleRate:      1.0,
		BaggageHeaders:  []string{"X-Request-ID", "X-User-ID", "X-Tenant-ID"},
		MaxBaggageItems: 10,
		MaxBaggageSize:  8192, // 8KB
	}
}

// Span represents a trace span.
type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	Attributes map[string]string
	Baggage    map[string]string
	sampled    bool
}

// Duration returns the span duration.
func (s *Span) Duration() time.Duration {
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// IsSampled returns if the span is sampled.
func (s *Span) IsSampled() bool {
	return s.sampled
}

// SpanContext holds trace context for propagation.
type SpanContext struct {
	TraceID string
	SpanID  string
	Sampled bool
	Baggage map[string]string
}

// Middleware provides distributed tracing.
type Middleware struct {
	config Config
	spans  map[string]*Span
	mu     sync.RWMutex
	idGen  *idGenerator
}

// idGenerator generates unique IDs.
type idGenerator struct {
	mu sync.Mutex
}

// newIDGenerator creates a new ID generator.
func newIDGenerator() *idGenerator {
	return &idGenerator{}
}

// generateTraceID generates a 32-byte hex trace ID.
func (g *idGenerator) generateTraceID() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateSpanID generates a 16-byte hex span ID.
func (g *idGenerator) generateSpanID() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// New creates a new trace middleware.
func New(config Config) *Middleware {
	if config.ServiceName == "" {
		config.ServiceName = "openloadbalancer"
	}
	if config.SampleRate < 0 {
		config.SampleRate = 1.0
	}
	if config.MaxBaggageItems == 0 {
		config.MaxBaggageItems = 10
	}
	if config.MaxBaggageSize == 0 {
		config.MaxBaggageSize = 8192
	}
	if len(config.Propagators) == 0 {
		config.Propagators = []string{"w3c"}
	}

	return &Middleware{
		config: config,
		spans:  make(map[string]*Span),
		idGen:  newIDGenerator(),
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "trace"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 10 // Very early, after Recovery(1), before RealIP(15)
}

// Wrap wraps the handler with tracing.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) && (len(r.URL.Path) == len(path) || r.URL.Path[len(path)] == '/' || path[len(path)-1] == '/') {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract or create trace context
		spanCtx := m.extractContext(r)

		// Create new span
		span := m.createSpan(r, spanCtx)

		// Store span in context
		ctx := contextWithSpan(r.Context(), span)
		r = r.WithContext(ctx)

		// Inject context into response headers
		m.injectContext(w, span)

		// Wrap response writer to capture status code
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		// Execute request
		next.ServeHTTP(rec, r)

		// Finish span
		span.EndTime = time.Now()
		span.Attributes["http.status_code"] = strconv.Itoa(rec.statusCode)
		span.Attributes["http.response_size"] = strconv.FormatInt(rec.bytesWritten, 10)

		// Store span if sampled
		if span.IsSampled() {
			m.storeSpan(span)
		}
	})
}

// extractContext extracts trace context from incoming request.
func (m *Middleware) extractContext(r *http.Request) *SpanContext {
	ctx := &SpanContext{
		Baggage: make(map[string]string),
	}

	for _, propagator := range m.config.Propagators {
		switch propagator {
		case "w3c":
			m.extractW3C(r, ctx)
		case "b3":
			m.extractB3Single(r, ctx)
		case "b3multi":
			m.extractB3Multi(r, ctx)
		case "jaeger":
			m.extractJaeger(r, ctx)
		}
	}

	// Extract baggage
	m.extractBaggage(r, ctx)

	// Determine sampling
	if ctx.TraceID == "" {
		ctx.Sampled = m.shouldSample()
	}

	return ctx
}

// extractW3C extracts W3C Trace Context headers.
func (m *Middleware) extractW3C(r *http.Request, ctx *SpanContext) {
	// traceparent: version-traceid-parentid-flags
	traceparent := r.Header.Get("traceparent")
	if traceparent == "" {
		return
	}

	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		return
	}

	ctx.TraceID = parts[1]
	ctx.SpanID = parts[2]
	// flags: bit 0 = sampled
	if len(parts[3]) >= 2 {
		flags, _ := hex.DecodeString(parts[3][:2])
		if len(flags) > 0 {
			ctx.Sampled = (flags[0] & 0x01) == 0x01
		}
	}

	// tracestate: vendor-specific data
	tracestate := r.Header.Get("tracestate")
	if tracestate != "" {
		// Store tracestate for propagation
		ctx.Baggage["tracestate"] = tracestate
	}
}

// extractB3Single extracts B3 single header format.
func (m *Middleware) extractB3Single(r *http.Request, ctx *SpanContext) {
	// b3: traceid-spanid-sampled-parentspanid
	b3 := r.Header.Get("b3")
	if b3 == "" {
		return
	}

	parts := strings.Split(b3, "-")
	if len(parts) >= 2 {
		ctx.TraceID = parts[0]
		ctx.SpanID = parts[1]
	}
	if len(parts) >= 3 {
		switch parts[2] {
		case "1", "d": // debug implies sampled
			ctx.Sampled = true
		case "0":
			ctx.Sampled = false
		}
	}
}

// extractB3Multi extracts B3 multi-header format.
func (m *Middleware) extractB3Multi(r *http.Request, ctx *SpanContext) {
	traceID := r.Header.Get("X-B3-TraceId")
	spanID := r.Header.Get("X-B3-SpanId")

	if traceID != "" {
		ctx.TraceID = traceID
	}
	if spanID != "" {
		ctx.SpanID = spanID
	}

	sampled := r.Header.Get("X-B3-Sampled")
	switch sampled {
	case "1":
		ctx.Sampled = true
	case "0":
		ctx.Sampled = false
	}

	flags := r.Header.Get("X-B3-Flags")
	if flags == "1" {
		ctx.Sampled = true // debug flag
	}
}

// extractJaeger extracts Jaeger trace context.
func (m *Middleware) extractJaeger(r *http.Request, ctx *SpanContext) {
	// uber-trace-id: traceid:spanid:parentid:flags
	uberTraceID := r.Header.Get("uber-trace-id")
	if uberTraceID == "" {
		return
	}

	parts := strings.Split(uberTraceID, ":")
	if len(parts) >= 2 {
		ctx.TraceID = parts[0]
		ctx.SpanID = parts[1]
	}
	if len(parts) >= 4 {
		// flags can be "0" or "1" or hex encoded
		flagsStr := parts[3]
		if flagsStr == "1" || flagsStr == "d" {
			ctx.Sampled = true
		} else if flagsStr == "0" {
			ctx.Sampled = false
		} else {
			// Try hex decode
			flags, _ := hex.DecodeString(flagsStr)
			if len(flags) > 0 {
				ctx.Sampled = (flags[0] & 0x01) == 0x01
			}
		}
	}
}

// extractBaggage extracts baggage from headers.
func (m *Middleware) extractBaggage(r *http.Request, ctx *SpanContext) {
	totalSize := 0
	itemCount := 0

	for _, header := range m.config.BaggageHeaders {
		if itemCount >= m.config.MaxBaggageItems {
			break
		}

		value := r.Header.Get(header)
		if value == "" {
			continue
		}

		// Check size limit
		if totalSize+len(value) > m.config.MaxBaggageSize {
			continue
		}

		key := strings.TrimPrefix(header, "X-")
		key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		ctx.Baggage[key] = value
		totalSize += len(value)
		itemCount++
	}
}

// createSpan creates a new span from the request.
func (m *Middleware) createSpan(r *http.Request, parentCtx *SpanContext) *Span {
	traceID := parentCtx.TraceID
	if traceID == "" {
		traceID = m.idGen.generateTraceID()
	}

	parentSpanID := parentCtx.SpanID
	spanID := m.idGen.generateSpanID()

	span := &Span{
		TraceID:   traceID,
		SpanID:    spanID,
		ParentID:  parentSpanID,
		Name:      fmt.Sprintf("%s %s", r.Method, r.URL.Path),
		StartTime: time.Now(),
		Attributes: map[string]string{
			"http.method":     r.Method,
			"http.url":        r.URL.String(),
			"http.path":       r.URL.Path,
			"http.host":       r.Host,
			"http.scheme":     r.URL.Scheme,
			"http.user_agent": r.UserAgent(),
			"service.name":    m.config.ServiceName,
			"service.version": m.config.ServiceVersion,
		},
		Baggage: make(map[string]string),
		sampled: parentCtx.Sampled,
	}

	// Copy baggage
	for k, v := range parentCtx.Baggage {
		span.Baggage[k] = v
	}

	return span
}

// injectContext injects trace context into response headers.
func (m *Middleware) injectContext(w http.ResponseWriter, span *Span) {
	for _, propagator := range m.config.Propagators {
		switch propagator {
		case "w3c":
			m.injectW3C(w, span)
		case "b3":
			m.injectB3Single(w, span)
		case "b3multi":
			m.injectB3Multi(w, span)
		case "jaeger":
			m.injectJaeger(w, span)
		}
	}
}

// injectW3C injects W3C Trace Context headers.
func (m *Middleware) injectW3C(w http.ResponseWriter, span *Span) {
	// traceparent: 00-traceid-spanid-flags
	flags := "00"
	if span.IsSampled() {
		flags = "01"
	}
	traceparent := fmt.Sprintf("00-%s-%s-%s", span.TraceID, span.SpanID, flags)
	w.Header().Set("traceparent", traceparent)

	// tracestate
	if tracestate, ok := span.Baggage["tracestate"]; ok {
		w.Header().Set("tracestate", tracestate)
	}
}

// injectB3Single injects B3 single header format.
func (m *Middleware) injectB3Single(w http.ResponseWriter, span *Span) {
	sampled := "0"
	if span.IsSampled() {
		sampled = "1"
	}
	b3 := fmt.Sprintf("%s-%s-%s", span.TraceID, span.SpanID, sampled)
	w.Header().Set("b3", b3)
}

// injectB3Multi injects B3 multi-header format.
func (m *Middleware) injectB3Multi(w http.ResponseWriter, span *Span) {
	w.Header().Set("X-B3-TraceId", span.TraceID)
	w.Header().Set("X-B3-SpanId", span.SpanID)
	if span.IsSampled() {
		w.Header().Set("X-B3-Sampled", "1")
	} else {
		w.Header().Set("X-B3-Sampled", "0")
	}
}

// injectJaeger injects Jaeger trace context.
func (m *Middleware) injectJaeger(w http.ResponseWriter, span *Span) {
	flags := "0"
	if span.IsSampled() {
		flags = "1"
	}
	uberTraceID := fmt.Sprintf("%s:%s:%s:%s", span.TraceID, span.SpanID, span.ParentID, flags)
	w.Header().Set("uber-trace-id", uberTraceID)
}

// shouldSample determines if a new trace should be sampled.
func (m *Middleware) shouldSample() bool {
	if m.config.SampleRate >= 1.0 {
		return true
	}
	if m.config.SampleRate <= 0.0 {
		return false
	}

	b := make([]byte, 4)
	rand.Read(b)
	val := float64(b[0]) / 255.0
	return val < m.config.SampleRate
}

// storeSpan stores a span for later retrieval.
func (m *Middleware) storeSpan(span *Span) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := span.TraceID + ":" + span.SpanID
	m.spans[key] = span
}

// GetSpan retrieves a span by trace and span ID.
func (m *Middleware) GetSpan(traceID, spanID string) *Span {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := traceID + ":" + spanID
	return m.spans[key]
}

// GetSpansByTrace retrieves all spans for a trace.
func (m *Middleware) GetSpansByTrace(traceID string) []*Span {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var spans []*Span
	for _, span := range m.spans {
		if span.TraceID == traceID {
			spans = append(spans, span)
		}
	}
	return spans
}

// Stats returns tracing statistics.
func (m *Middleware) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sampled := 0
	for _, span := range m.spans {
		if span.IsSampled() {
			sampled++
		}
	}

	return map[string]any{
		"spans":       len(m.spans),
		"sampled":     sampled,
		"sample_rate": m.config.SampleRate,
		"propagators": m.config.Propagators,
	}
}

// context key for span
type spanKey struct{}

// contextWithSpan adds a span to context.
func contextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

// GetSpanFromContext retrieves the span from context.
func GetSpanFromContext(ctx context.Context) *Span {
	if span, ok := ctx.Value(spanKey{}).(*Span); ok {
		return span
	}
	return nil
}

// responseRecorder wraps http.ResponseWriter to capture status code and bytes.
type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	written      bool
}

// WriteHeader captures the status code.
func (rec *responseRecorder) WriteHeader(code int) {
	if rec.written {
		return
	}
	rec.statusCode = code
	rec.written = true
	rec.ResponseWriter.WriteHeader(code)
}

// Write captures the bytes written.
func (rec *responseRecorder) Write(p []byte) (int, error) {
	if !rec.written {
		rec.WriteHeader(http.StatusOK)
	}
	n, err := rec.ResponseWriter.Write(p)
	rec.bytesWritten += int64(n)
	return n, err
}

// Header returns the header map.
func (rec *responseRecorder) Header() http.Header {
	return rec.ResponseWriter.Header()
}
