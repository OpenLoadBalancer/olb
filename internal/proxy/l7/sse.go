package l7

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// SSEConfig configures Server-Sent Events proxy behavior.
type SSEConfig struct {
	// EnableSSE enables SSE proxying support.
	EnableSSE bool

	// MaxEventSize is the maximum size of a single SSE event.
	MaxEventSize int64

	// IdleTimeout is the maximum time to wait between events.
	IdleTimeout time.Duration

	// FlushInterval is how often to flush even if buffer not full (0 = disable).
	FlushInterval time.Duration
}

// DefaultSSEConfig returns a default SSE configuration.
func DefaultSSEConfig() *SSEConfig {
	return &SSEConfig{
		EnableSSE:     true,
		MaxEventSize:  1024 * 1024, // 1MB max per event
		IdleTimeout:   60 * time.Second,
		FlushInterval: 0, // No forced flush, rely on line-based flushing
	}
}

// IsSSERequest checks if the request is an SSE request.
func IsSSERequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/event-stream")
}

// IsSSEResponse checks if the response is an SSE response.
func IsSSEResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "text/event-stream")
}

// SSEHandler handles Server-Sent Events proxying.
type SSEHandler struct {
	config *SSEConfig
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(config *SSEConfig) *SSEHandler {
	if config == nil {
		config = DefaultSSEConfig()
	}
	return &SSEHandler{
		config: config,
	}
}

// HandleSSE handles an SSE request.
// For SSE, we need to:
// 1. Disable buffering and enable immediate flush
// 2. Preserve the connection for streaming
// 3. Handle Last-Event-ID for replay/resume
func (sh *SSEHandler) HandleSSE(w http.ResponseWriter, r *http.Request, b *backend.Backend) error {
	if !sh.config.EnableSSE {
		return errors.New("sse disabled")
	}

	// Acquire connection slot
	if !b.AcquireConn() {
		return errors.New("backend at max connections")
	}
	defer b.ReleaseConn()

	// Create transport with SSE-specific settings
	transport := sh.createSSETransport()

	// Prepare outbound request
	outReq, err := sh.prepareSSERequest(r, b)
	if err != nil {
		return err
	}

	// Execute request
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		return fmt.Errorf("backend request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check if response is actually SSE
	if !IsSSEResponse(resp) {
		// Not an SSE response, treat as regular HTTP response
		return sh.copyRegularResponse(w, resp)
	}

	// Handle SSE response with streaming
	return sh.streamSSEResponse(w, resp, b)
}

// createSSETransport creates an HTTP transport optimized for SSE.
func (sh *SSEHandler) createSSETransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		// Disable response compression for SSE (we need to read line by line)
		DisableCompression: true,
	}
}

// prepareSSERequest creates the outbound SSE request.
func (sh *SSEHandler) prepareSSERequest(r *http.Request, b *backend.Backend) (*http.Request, error) {
	// Clone the request
	outReq := r.Clone(r.Context())

	// Set the URL to point to the backend
	outReq.URL.Scheme = "http"
	outReq.URL.Host = b.Address
	outReq.Host = r.Host
	outReq.RequestURI = ""

	// Set X-Forwarded headers
	clientIP := getClientIP(r)
	if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
		outReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		outReq.Header.Set("X-Forwarded-For", clientIP)
	}
	outReq.Header.Set("X-Real-IP", clientIP)

	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	outReq.Header.Set("X-Forwarded-Proto", proto)

	// Ensure Accept header is set
	if outReq.Header.Get("Accept") == "" {
		outReq.Header.Set("Accept", "text/event-stream")
	}

	return outReq, nil
}

// streamSSEResponse streams an SSE response to the client.
func (sh *SSEHandler) streamSSEResponse(w http.ResponseWriter, resp *http.Response, b *backend.Backend) error {
	// Copy headers
	copySSEHeaders(w.Header(), resp.Header)

	// Set SSE-required cache and streaming headers per spec
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable proxy buffering (nginx compat)

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Get flusher (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		// If we can't flush, just copy the body normally
		_, err := io.Copy(w, resp.Body)
		return err
	}

	// Stream events line by line
	reader := bufio.NewReader(resp.Body)
	for {
		// Read line with timeout handling
		line, err := sh.readLineWithTimeout(reader, sh.config.IdleTimeout)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			// Check if it's a timeout (normal for SSE idle connections)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Send a keepalive comment and continue
				if _, writeErr := w.Write([]byte(":keepalive\n")); writeErr != nil {
					return writeErr
				}
				flusher.Flush()
				continue
			}
			return err
		}

		// Write the line
		if _, writeErr := w.Write(line); writeErr != nil {
			return writeErr
		}

		// Flush after each line (critical for SSE)
		flusher.Flush()
	}
}

// readLineWithTimeout reads a line with a timeout.
func (sh *SSEHandler) readLineWithTimeout(reader *bufio.Reader, timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		// Use a channel for timeout
		type result struct {
			line []byte
			err  error
		}
		ch := make(chan result, 1)

		go func() {
			line, err := reader.ReadBytes('\n')
			ch <- result{line, err}
		}()

		select {
		case res := <-ch:
			return res.line, res.err
		case <-time.After(timeout):
			return nil, &timeoutError{}
		}
	}

	return reader.ReadBytes('\n')
}

// timeoutError represents a timeout error.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// copySSEHeaders copies headers from source to destination, excluding hop-by-hop.
func copySSEHeaders(dst, src http.Header) {
	for key, values := range src {
		// Skip hop-by-hop headers
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// copyRegularResponse copies a non-SSE response.
func (sh *SSEHandler) copyRegularResponse(w http.ResponseWriter, resp *http.Response) error {
	copySSEHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

// SSEEvent represents a parsed Server-Sent Event.
type SSEEvent struct {
	ID    string
	Event string
	Data  []byte
	Retry int
}

// ParseSSEEvent parses a single SSE event from bytes.
func ParseSSEEvent(data []byte) (*SSEEvent, error) {
	event := &SSEEvent{}
	lines := bytes.Split(data, []byte("\n"))

	var dataLines [][]byte

	for _, line := range lines {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}

		// Check for comment
		if line[0] == ':' {
			continue
		}

		// Parse field
		colonIdx := bytes.Index(line, []byte(":"))
		if colonIdx == -1 {
			// Field with no value
			field := string(line)
			switch field {
			case "event":
				event.Event = ""
			case "data":
				dataLines = append(dataLines, []byte{})
			case "id":
				event.ID = ""
			case "retry":
				event.Retry = 0
			}
			continue
		}

		field := string(line[:colonIdx])
		value := line[colonIdx+1:]

		// Strip leading space if present
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch field {
		case "event":
			event.Event = string(value)
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			event.ID = string(value)
		case "retry":
			// Parse retry as integer
			fmt.Sscanf(string(value), "%d", &event.Retry)
		}
	}

	// Join data lines with newlines
	if len(dataLines) > 0 {
		event.Data = bytes.Join(dataLines, []byte("\n"))
	}

	return event, nil
}

// FormatSSEEvent formats an SSE event for transmission.
func FormatSSEEvent(event *SSEEvent) []byte {
	var buf bytes.Buffer

	if event.ID != "" {
		fmt.Fprintf(&buf, "id: %s\n", event.ID)
	}

	if event.Event != "" {
		fmt.Fprintf(&buf, "event: %s\n", event.Event)
	}

	if event.Retry > 0 {
		fmt.Fprintf(&buf, "retry: %d\n", event.Retry)
	}

	// Write data lines
	if len(event.Data) > 0 {
		lines := bytes.Split(event.Data, []byte("\n"))
		for _, line := range lines {
			fmt.Fprintf(&buf, "data: %s\n", line)
		}
	}

	// Empty line to terminate event
	buf.WriteByte('\n')

	return buf.Bytes()
}

// SSEProxy wraps an HTTPProxy with SSE support.
type SSEProxy struct {
	httpProxy  *HTTPProxy
	sseHandler *SSEHandler
}

// NewSSEProxy creates a new proxy with SSE support.
func NewSSEProxy(httpProxy *HTTPProxy, sseConfig *SSEConfig) *SSEProxy {
	return &SSEProxy{
		httpProxy:  httpProxy,
		sseHandler: NewSSEHandler(sseConfig),
	}
}

// ServeHTTP implements http.Handler with SSE support.
func (sp *SSEProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is an SSE request
	if IsSSERequest(r) {
		// Get route match
		routeMatch, ok := sp.httpProxy.router.Match(r)
		if !ok {
			sp.httpProxy.errorHandler(w, r, errors.New("route not found"))
			return
		}

		// Get backend pool
		pool := sp.httpProxy.poolManager.GetPool(routeMatch.Route.BackendPool)
		if pool == nil {
			sp.httpProxy.errorHandler(w, r, errors.New("pool not found"))
			return
		}

		// Select backend
		backends := pool.GetHealthyBackends()
		if len(backends) == 0 {
			sp.httpProxy.errorHandler(w, r, errors.New("no healthy backends"))
			return
		}

		selected := pool.GetBalancer().Next(backends)
		if selected == nil {
			sp.httpProxy.errorHandler(w, r, errors.New("no backend available"))
			return
		}

		// Handle SSE
		if err := sp.sseHandler.HandleSSE(w, r, selected); err != nil {
			sp.httpProxy.errorHandler(w, r, err)
		}
		return
	}

	// Not an SSE request, use regular HTTP proxy
	sp.httpProxy.ServeHTTP(w, r)
}
