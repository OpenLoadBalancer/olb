// Package middleware provides HTTP middleware components for OpenLoadBalancer.
package middleware

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/logging"
)

// AccessLogFormat represents the format for access logging.
type AccessLogFormat string

const (
	// AccessLogFormatJSON logs in JSON format.
	AccessLogFormatJSON AccessLogFormat = "json"
	// AccessLogFormatCLF logs in Common Log Format.
	AccessLogFormatCLF AccessLogFormat = "clf"
)

// AccessLogConfig configures the AccessLog middleware.
type AccessLogConfig struct {
	Format    AccessLogFormat // json or clf
	Output    io.Writer       // log destination (default: os.Stdout)
	Logger    *logging.Logger // optional structured logger to use
	SkipPaths []string        // paths to skip logging (e.g., /health)
}

// AccessLogMiddleware logs HTTP requests.
type AccessLogMiddleware struct {
	config AccessLogConfig
}

// NewAccessLogMiddleware creates a new AccessLog middleware.
func NewAccessLogMiddleware(config AccessLogConfig) *AccessLogMiddleware {
	// Apply defaults
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.Format == "" {
		config.Format = AccessLogFormatCLF
	}

	return &AccessLogMiddleware{
		config: config,
	}
}

// Name returns the middleware name.
func (m *AccessLogMiddleware) Name() string {
	return "access-log"
}

// Priority returns the middleware priority.
func (m *AccessLogMiddleware) Priority() int {
	return PriorityAccessLog
}

// Wrap wraps the next handler with access logging functionality.
func (m *AccessLogMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path should be skipped
		if m.shouldSkip(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Create request context
		ctx := NewRequestContext(r, w)

		// Call next handler
		next.ServeHTTP(ctx.Response, r)

		// Update context with response data
		ctx.StatusCode = ctx.Response.Status()
		ctx.BytesOut = ctx.Response.BytesWritten()

		// Log the request synchronously (logging is fast, no need for goroutine)
		m.log(ctx)

		// Release context after logging
		ctx.Release()
	})
}

// shouldSkip checks if the path should be skipped from logging.
func (m *AccessLogMiddleware) shouldSkip(path string) bool {
	for _, skipPath := range m.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath+"/") {
			return true
		}
	}
	return false
}

// log writes the access log entry.
func (m *AccessLogMiddleware) log(ctx *RequestContext) {
	switch m.config.Format {
	case AccessLogFormatJSON:
		m.logJSON(ctx)
	case AccessLogFormatCLF:
		m.logCLF(ctx)
	default:
		m.logCLF(ctx)
	}
}

// logJSON logs in JSON format.
func (m *AccessLogMiddleware) logJSON(ctx *RequestContext) {
	req := ctx.Request

	// Build route name
	routeName := ""
	if ctx.Route != nil {
		routeName = ctx.Route.Name
	}

	// Build backend name
	backendName := ""
	if ctx.Backend != nil {
		backendName = ctx.Backend.ID
	}

	// Extract client IP
	clientIP := ctx.ClientIP
	if clientIP == "" {
		clientIP = extractIP(req.RemoteAddr)
	}

	// Get request ID from context or header
	requestID := ctx.RequestID
	if requestID == "" {
		requestID = req.Header.Get("X-Request-Id")
	}

	// Build JSON manually for performance
	var sb strings.Builder
	sb.Grow(512)

	sb.WriteString(`{`)

	// Timestamp
	sb.WriteString(`"timestamp":"`)
	sb.WriteString(ctx.StartTime.UTC().Format(time.RFC3339))
	sb.WriteString(`",`)

	// Request ID
	if requestID != "" {
		sb.WriteString(`"request_id":"`)
		sb.WriteString(escapeJSON(requestID))
		sb.WriteString(`",`)
	}

	// Method
	sb.WriteString(`"method":"`)
	sb.WriteString(req.Method)
	sb.WriteString(`",`)

	// Path
	sb.WriteString(`"path":"`)
	sb.WriteString(escapeJSON(req.URL.Path))
	sb.WriteString(`",`)

	// Query
	if req.URL.RawQuery != "" {
		sb.WriteString(`"query":"`)
		sb.WriteString(escapeJSON(req.URL.RawQuery))
		sb.WriteString(`",`)
	}

	// Client IP
	sb.WriteString(`"client_ip":"`)
	sb.WriteString(clientIP)
	sb.WriteString(`",`)

	// Host
	sb.WriteString(`"host":"`)
	sb.WriteString(escapeJSON(req.Host))
	sb.WriteString(`",`)

	// User Agent
	if ua := req.UserAgent(); ua != "" {
		sb.WriteString(`"user_agent":"`)
		sb.WriteString(escapeJSON(ua))
		sb.WriteString(`",`)
	}

	// Referer
	if referer := req.Referer(); referer != "" {
		sb.WriteString(`"referer":"`)
		sb.WriteString(escapeJSON(referer))
		sb.WriteString(`",`)
	}

	// Status
	sb.WriteString(`"status":`)
	sb.WriteString(fmt.Sprintf("%d", ctx.StatusCode))
	sb.WriteString(`,"bytes_in":`)
	sb.WriteString(fmt.Sprintf("%d", ctx.BytesIn))
	sb.WriteString(`,"bytes_out":`)
	sb.WriteString(fmt.Sprintf("%d", ctx.BytesOut))
	sb.WriteString(`,"duration_ms":`)
	sb.WriteString(fmt.Sprintf("%.3f", float64(ctx.Duration().Nanoseconds())/1e6))

	// Backend
	if backendName != "" {
		sb.WriteString(`,"backend":"`)
		sb.WriteString(escapeJSON(backendName))
		sb.WriteString(`"`)
	}

	// Route
	if routeName != "" {
		sb.WriteString(`,"route":"`)
		sb.WriteString(escapeJSON(routeName))
		sb.WriteString(`"`)
	}

	sb.WriteString(`}`)
	sb.WriteByte('\n')

	// Write to output
	if m.config.Logger != nil {
		m.config.Logger.Info("access", logging.String("log", sb.String()))
	} else {
		m.config.Output.Write([]byte(sb.String()))
	}
}

// logCLF logs in Common Log Format.
// Format: host ident authuser date request status bytes
func (m *AccessLogMiddleware) logCLF(ctx *RequestContext) {
	req := ctx.Request

	// Extract client IP
	clientIP := ctx.ClientIP
	if clientIP == "" {
		clientIP = extractIP(req.RemoteAddr)
	}

	// Build CLF log line
	var sb strings.Builder
	sb.Grow(256)

	// Remote host
	sb.WriteString(clientIP)
	sb.WriteString(" - - ") // ident and authuser are always "-"

	// Date in CLF format: [14/Mar/2026:10:30:00 +0000]
	sb.WriteByte('[')
	sb.WriteString(ctx.StartTime.UTC().Format("02/Jan/2006:15:04:05 -0700"))
	sb.WriteString(`] "`)

	// Request line: METHOD PATH PROTOCOL
	sb.WriteString(req.Method)
	sb.WriteByte(' ')
	sb.WriteString(req.URL.RequestURI())
	sb.WriteByte(' ')
	sb.WriteString(req.Proto)
	sb.WriteString(`" `)

	// Status code
	sb.WriteString(fmt.Sprintf("%d", ctx.StatusCode))
	sb.WriteByte(' ')

	// Response size (or - if not known)
	if ctx.BytesOut > 0 {
		sb.WriteString(fmt.Sprintf("%d", ctx.BytesOut))
	} else {
		sb.WriteByte('-')
	}

	sb.WriteByte('\n')

	// Write to output
	if m.config.Logger != nil {
		m.config.Logger.Info("access", logging.String("log", sb.String()))
	} else {
		m.config.Output.Write([]byte(sb.String()))
	}
}

// escapeJSON escapes a string for JSON output.
func escapeJSON(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

// extractIP extracts the IP address from a host:port string.
func extractIP(addr string) string {
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}

// logBufferPool is a pool for log entry buffers.
var logBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 0, 512)
	},
}
