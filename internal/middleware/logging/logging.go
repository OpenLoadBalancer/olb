// Package logging provides HTTP request/response logging middleware.
package logging

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config configures logging middleware.
type Config struct {
	Enabled         bool     // Enable logging
	Format          string   // Format: "json", "combined", "common", "custom"
	CustomFormat    string   // Custom log format template
	Fields          []string // Fields to include: "timestamp", "method", "path", "status", "duration", "bytes", "ip", "user_agent", "referer", "request_id"
	ExcludePaths    []string // Paths to exclude from logging
	ExcludeStatus   []int    // Status codes to exclude
	MinDuration     string   // Only log requests slower than this (e.g., "100ms")
	RequestHeaders  []string // Request headers to log
	ResponseHeaders []string // Response headers to log
}

// DefaultConfig returns default logging configuration.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Format:  "combined",
		Fields: []string{
			"timestamp", "method", "path", "status", "duration", "bytes", "ip",
		},
	}
}

// Middleware provides request logging functionality.
type Middleware struct {
	config      Config
	minDuration time.Duration
}

// responseRecorder wraps http.ResponseWriter to capture status and size.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	bytesSent  int
	written    bool
}

// New creates a new logging middleware.
func New(config Config) *Middleware {
	var minDuration time.Duration
	if config.MinDuration != "" {
		d, err := time.ParseDuration(config.MinDuration)
		if err == nil {
			minDuration = d
		}
	}

	return &Middleware{
		config:      config,
		minDuration: minDuration,
	}
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "logging"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 80 // Very early, after Recovery
}

// Wrap wraps the handler with logging.
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

		start := time.Now()
		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)

		// Check min duration filter
		if m.minDuration > 0 && duration < m.minDuration {
			return
		}

		// Check excluded status codes
		for _, code := range m.config.ExcludeStatus {
			if rec.statusCode == code {
				return
			}
		}

		// Build log entry
		entry := m.buildLogEntry(r, rec, duration, start)

		// Output log (using fmt for now, could integrate with logging package)
		fmt.Println(entry)
	})
}

// buildLogEntry creates a log entry based on configured format.
func (m *Middleware) buildLogEntry(r *http.Request, rec *responseRecorder, duration time.Duration, start time.Time) string {
	switch m.config.Format {
	case "json":
		return m.buildJSONEntry(r, rec, duration, start)
	case "common":
		return m.buildCommonLogEntry(r, rec, duration, start)
	case "custom":
		return m.buildCustomEntry(r, rec, duration, start)
	default: // combined
		return m.buildCombinedLogEntry(r, rec, duration, start)
	}
}

// buildCombinedLogEntry creates Apache Combined Log Format entry.
func (m *Middleware) buildCombinedLogEntry(r *http.Request, rec *responseRecorder, duration time.Duration, start time.Time) string {
	// %h %l %u %t "%r" %>s %b "%{Referer}i" "%{User-agent}i"
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	if ip == "" {
		ip = "-"
	}

	username := "-"
	if r.URL.User != nil {
		username = r.URL.User.Username()
	}

	timestamp := start.Format("02/Jan/2006:15:04:05 -0700")
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.Path, r.Proto)

	size := rec.bytesSent
	if size == 0 {
		size = 0
	}

	referer := r.Referer()
	if referer == "" {
		referer = "-"
	}

	userAgent := r.UserAgent()
	if userAgent == "" {
		userAgent = "-"
	}

	return fmt.Sprintf("%s - %s [%s] \"%s\" %d %d \"%s\" \"%s\"",
		ip, username, timestamp, requestLine, rec.statusCode, size, referer, userAgent)
}

// buildCommonLogEntry creates Apache Common Log Format entry.
func (m *Middleware) buildCommonLogEntry(r *http.Request, rec *responseRecorder, duration time.Duration, start time.Time) string {
	// %h %l %u %t "%r" %>s %b
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	if ip == "" {
		ip = "-"
	}

	username := "-"
	if r.URL.User != nil {
		username = r.URL.User.Username()
	}

	timestamp := start.Format("02/Jan/2006:15:04:05 -0700")
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.Path, r.Proto)

	return fmt.Sprintf("%s - %s [%s] \"%s\" %d %d",
		ip, username, timestamp, requestLine, rec.statusCode, rec.bytesSent)
}

// buildJSONEntry creates JSON formatted log entry.
func (m *Middleware) buildJSONEntry(r *http.Request, rec *responseRecorder, duration time.Duration, start time.Time) string {
	fields := []string{}

	for _, field := range m.config.Fields {
		var value string
		switch field {
		case "timestamp":
			value = fmt.Sprintf("\"timestamp\":\"%s\"", start.Format(time.RFC3339Nano))
		case "method":
			value = fmt.Sprintf("\"method\":\"%s\"", r.Method)
		case "path":
			value = fmt.Sprintf("\"path\":\"%s\"", r.URL.Path)
		case "query":
			value = fmt.Sprintf("\"query\":\"%s\"", r.URL.RawQuery)
		case "status":
			value = fmt.Sprintf("\"status\":%d", rec.statusCode)
		case "duration":
			value = fmt.Sprintf("\"duration_ms\":%d", duration.Milliseconds())
		case "bytes":
			value = fmt.Sprintf("\"bytes_sent\":%d", rec.bytesSent)
		case "ip":
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
			value = fmt.Sprintf("\"client_ip\":\"%s\"", ip)
		case "user_agent":
			value = fmt.Sprintf("\"user_agent\":\"%s\"", escapeJSON(r.UserAgent()))
		case "referer":
			value = fmt.Sprintf("\"referer\":\"%s\"", escapeJSON(r.Referer()))
		case "request_id":
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = "-"
			}
			value = fmt.Sprintf("\"request_id\":\"%s\"", reqID)
		case "host":
			value = fmt.Sprintf("\"host\":\"%s\"", r.Host)
		case "proto":
			value = fmt.Sprintf("\"proto\":\"%s\"", r.Proto)
		}
		if value != "" {
			fields = append(fields, value)
		}
	}

	// Add headers if configured
	for _, header := range m.config.RequestHeaders {
		val := r.Header.Get(header)
		fields = append(fields, fmt.Sprintf("\"%s\":\"%s\"", strings.ToLower(header), escapeJSON(val)))
	}

	return "{" + strings.Join(fields, ",") + "}"
}

// buildCustomEntry creates custom formatted log entry.
func (m *Middleware) buildCustomEntry(r *http.Request, rec *responseRecorder, duration time.Duration, start time.Time) string {
	format := m.config.CustomFormat
	if format == "" {
		return m.buildCombinedLogEntry(r, rec, duration, start)
	}

	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}

	replacements := map[string]string{
		"$timestamp":   start.Format(time.RFC3339),
		"$method":      r.Method,
		"$path":        r.URL.Path,
		"$query":       r.URL.RawQuery,
		"$status":      fmt.Sprintf("%d", rec.statusCode),
		"$duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
		"$duration":    duration.String(),
		"$bytes":       fmt.Sprintf("%d", rec.bytesSent),
		"$ip":          ip,
		"$host":        r.Host,
		"$user_agent":  r.UserAgent(),
		"$referer":     r.Referer(),
		"$request_id":  r.Header.Get("X-Request-ID"),
	}

	result := format
	for key, val := range replacements {
		result = strings.ReplaceAll(result, key, val)
	}

	return result
}

// escapeJSON escapes a string for JSON.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
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

// Write captures the number of bytes written.
func (rec *responseRecorder) Write(p []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(p)
	rec.bytesSent += n
	rec.written = true
	return n, err
}

// Header returns the header map.
func (rec *responseRecorder) Header() http.Header {
	return rec.ResponseWriter.Header()
}
