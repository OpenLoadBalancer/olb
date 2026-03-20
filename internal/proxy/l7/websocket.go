package l7

import (
	"bufio"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// WebSocket handshake magic GUID per RFC 6455
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocketConfig configures WebSocket proxy behavior.
type WebSocketConfig struct {
	// EnableWebSocket enables WebSocket proxying.
	EnableWebSocket bool

	// IdleTimeout is the maximum time to wait for data before closing.
	IdleTimeout time.Duration

	// PingInterval is the interval between ping frames (0 = disabled).
	PingInterval time.Duration

	// MaxMessageSize is the maximum message size in bytes.
	MaxMessageSize int64
}

// DefaultWebSocketConfig returns a default WebSocket configuration.
func DefaultWebSocketConfig() *WebSocketConfig {
	return &WebSocketConfig{
		EnableWebSocket: true,
		IdleTimeout:     60 * time.Second,
		PingInterval:    30 * time.Second,
		MaxMessageSize:  10 * 1024 * 1024, // 10MB
	}
}

// IsWebSocketUpgrade checks if the request is a WebSocket upgrade request.
func IsWebSocketUpgrade(r *http.Request) bool {
	connHeader := strings.ToLower(r.Header.Get("Connection"))
	if !strings.Contains(connHeader, "upgrade") {
		return false
	}
	upgradeHeader := strings.ToLower(r.Header.Get("Upgrade"))
	return upgradeHeader == "websocket"
}

// WebSocketHandler handles WebSocket proxying.
type WebSocketHandler struct {
	config *WebSocketConfig
	dialer *net.Dialer
}

// NewWebSocketHandler creates a new WebSocket handler.
func NewWebSocketHandler(config *WebSocketConfig) *WebSocketHandler {
	if config == nil {
		config = DefaultWebSocketConfig()
	}
	return &WebSocketHandler{
		config: config,
		dialer: &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
}

// HandleWebSocket handles a WebSocket upgrade request by:
// 1. Dialing the backend and forwarding the upgrade request
// 2. Reading the backend's 101 response
// 3. Forwarding the 101 to the client
// 4. Establishing a bidirectional tunnel
func (wh *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request, b *backend.Backend) error {
	if !wh.config.EnableWebSocket {
		return errors.New("WebSocket disabled")
	}

	wsKey := r.Header.Get("Sec-WebSocket-Key")
	if wsKey == "" {
		return errors.New("missing Sec-WebSocket-Key header")
	}

	if !b.AcquireConn() {
		return errors.New("backend at max connections")
	}
	defer b.ReleaseConn()

	// 1. Dial backend (raw TCP)
	backendConn, err := wh.dialBackend(r, b)
	if err != nil {
		return fmt.Errorf("failed to connect to backend: %w", err)
	}
	defer backendConn.Close()

	// 2. Forward the original upgrade request to backend
	if err := wh.writeUpgradeRequest(backendConn, r, b); err != nil {
		return fmt.Errorf("failed to send upgrade to backend: %w", err)
	}

	// 3. Read backend's 101 response
	backendBuf := bufio.NewReader(backendConn)
	backendResp, err := http.ReadResponse(backendBuf, r)
	if err != nil {
		return fmt.Errorf("failed to read backend upgrade response: %w", err)
	}
	defer backendResp.Body.Close()

	if backendResp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("backend rejected WebSocket upgrade: %d", backendResp.StatusCode)
	}

	// 4. Hijack client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("response writer does not support hijacking")
	}
	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		return fmt.Errorf("failed to hijack connection: %w", err)
	}
	defer clientConn.Close()

	// 5. Forward 101 Switching Protocols to client
	if err := wh.writeUpgradeResponse(clientConn, backendResp, wsKey); err != nil {
		return fmt.Errorf("failed to send 101 to client: %w", err)
	}

	// 6. Forward any buffered data from client
	if clientBuf != nil && clientBuf.Reader != nil && clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		n, _ := clientBuf.Reader.Read(buffered)
		if n > 0 {
			backendConn.Write(buffered[:n])
		}
	}

	// 7. Forward any buffered data from backend
	if backendBuf.Buffered() > 0 {
		buffered := make([]byte, backendBuf.Buffered())
		n, _ := backendBuf.Read(buffered)
		if n > 0 {
			clientConn.Write(buffered[:n])
		}
	}

	// 8. Bidirectional tunnel
	return wh.proxyWebSocket(clientConn, backendConn)
}

// writeUpgradeRequest writes the WebSocket upgrade HTTP request to the backend.
func (wh *WebSocketHandler) writeUpgradeRequest(conn net.Conn, r *http.Request, b *backend.Backend) error {
	path := r.URL.RequestURI()
	if path == "" {
		path = "/"
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("GET %s HTTP/1.1\r\n", path))
	buf.WriteString(fmt.Sprintf("Host: %s\r\n", r.Host))

	// Forward all original headers
	for key, vals := range r.Header {
		for _, val := range vals {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, val))
		}
	}

	// Add X-Forwarded headers
	clientIP := extractClientIP(r)
	if clientIP != "" {
		if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
			buf.WriteString(fmt.Sprintf("X-Forwarded-For: %s, %s\r\n", prior, clientIP))
		} else {
			buf.WriteString(fmt.Sprintf("X-Forwarded-For: %s\r\n", clientIP))
		}
	}

	buf.WriteString("\r\n")
	_, err := conn.Write([]byte(buf.String()))
	return err
}

// writeUpgradeResponse writes the 101 Switching Protocols response to the client.
func (wh *WebSocketHandler) writeUpgradeResponse(conn net.Conn, resp *http.Response, wsKey string) error {
	var buf strings.Builder
	buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n")

	// Forward backend's response headers
	hasAccept := false
	for key, vals := range resp.Header {
		for _, val := range vals {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, val))
		}
		if strings.EqualFold(key, "Sec-WebSocket-Accept") {
			hasAccept = true
		}
	}

	// If backend didn't send Sec-WebSocket-Accept, compute it ourselves
	if !hasAccept && wsKey != "" {
		accept := computeWebSocketAccept(wsKey)
		buf.WriteString(fmt.Sprintf("Sec-WebSocket-Accept: %s\r\n", accept))
	}

	// Ensure required headers
	if resp.Header.Get("Upgrade") == "" {
		buf.WriteString("Upgrade: websocket\r\n")
	}
	if resp.Header.Get("Connection") == "" {
		buf.WriteString("Connection: Upgrade\r\n")
	}

	buf.WriteString("\r\n")
	_, err := conn.Write([]byte(buf.String()))
	return err
}

// computeWebSocketAccept computes the Sec-WebSocket-Accept value per RFC 6455 Section 4.2.2.
func computeWebSocketAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// extractClientIP extracts the client IP from a request.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// dialBackend dials the backend server for WebSocket connection.
func (wh *WebSocketHandler) dialBackend(r *http.Request, b *backend.Backend) (net.Conn, error) {
	isTLS := strings.HasPrefix(b.Address, "https://") || strings.HasPrefix(b.Address, "wss://")
	address := b.Address
	address = strings.TrimPrefix(address, "https://")
	address = strings.TrimPrefix(address, "http://")
	address = strings.TrimPrefix(address, "wss://")
	address = strings.TrimPrefix(address, "ws://")

	if isTLS || r.TLS != nil {
		tlsConfig := &tls.Config{
			// Backend TLS verification is skipped by default for internal
			// backends using self-signed certificates. For public backends,
			// configure proper CA certificates via the TLS manager.
			InsecureSkipVerify: true, //nolint:gosec — backend-to-backend internal TLS
		}
		return tls.DialWithDialer(wh.dialer, "tcp", address, tlsConfig)
	}

	return wh.dialer.Dial("tcp", address)
}

// proxyWebSocket performs bidirectional data copying between client and backend.
func (wh *WebSocketHandler) proxyWebSocket(clientConn, backendConn net.Conn) error {
	errChan := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend
	go func() {
		defer wg.Done()
		err := wh.copyWithIdleTimeout(backendConn, clientConn, wh.config.IdleTimeout)
		if err != nil && !isWebSocketCloseError(err) {
			errChan <- fmt.Errorf("client to backend: %w", err)
		}
		backendConn.Close()
	}()

	// Backend -> Client
	go func() {
		defer wg.Done()
		err := wh.copyWithIdleTimeout(clientConn, backendConn, wh.config.IdleTimeout)
		if err != nil && !isWebSocketCloseError(err) {
			errChan <- fmt.Errorf("backend to client: %w", err)
		}
		clientConn.Close()
	}()

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

// copyWithIdleTimeout copies data with an idle timeout.
func (wh *WebSocketHandler) copyWithIdleTimeout(dst, src net.Conn, timeout time.Duration) error {
	buf := make([]byte, 32*1024)

	for {
		if timeout > 0 {
			src.SetReadDeadline(time.Now().Add(timeout))
		}

		nr, err := src.Read(buf)
		if nr > 0 {
			if timeout > 0 {
				src.SetReadDeadline(time.Time{})
			}
			nw, writeErr := dst.Write(buf[:nr])
			if writeErr != nil {
				return writeErr
			}
			if nw != nr {
				return io.ErrShortWrite
			}
		}

		if err != nil {
			if isWebSocketCloseError(err) {
				return nil
			}
			return err
		}
	}
}

// isWebSocketCloseError checks if an error is a normal WebSocket close condition.
func isWebSocketCloseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}

	errStr := strings.ToLower(err.Error())
	for _, cond := range []string{"use of closed network connection", "broken pipe", "connection reset", "eof"} {
		if strings.Contains(errStr, cond) {
			return true
		}
	}
	return false
}

// WebSocketProxy wraps an HTTPProxy with WebSocket support.
type WebSocketProxy struct {
	httpProxy *HTTPProxy
	wsHandler *WebSocketHandler
}

// NewWebSocketProxy creates a new proxy with WebSocket support.
func NewWebSocketProxy(httpProxy *HTTPProxy, wsConfig *WebSocketConfig) *WebSocketProxy {
	return &WebSocketProxy{
		httpProxy: httpProxy,
		wsHandler: NewWebSocketHandler(wsConfig),
	}
}

// ServeHTTP implements http.Handler with WebSocket support.
func (wp *WebSocketProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if IsWebSocketUpgrade(r) {
		routeMatch, ok := wp.httpProxy.router.Match(r)
		if !ok {
			wp.httpProxy.errorHandler(w, r, errors.New("route not found"))
			return
		}

		pool := wp.httpProxy.poolManager.GetPool(routeMatch.Route.BackendPool)
		if pool == nil {
			wp.httpProxy.errorHandler(w, r, errors.New("pool not found"))
			return
		}

		backends := pool.GetHealthyBackends()
		if len(backends) == 0 {
			wp.httpProxy.errorHandler(w, r, errors.New("no healthy backends"))
			return
		}

		selected := pool.GetBalancer().Next(backends)
		if selected == nil {
			wp.httpProxy.errorHandler(w, r, errors.New("no backend available"))
			return
		}

		if err := wp.wsHandler.HandleWebSocket(w, r, selected); err != nil {
			wp.httpProxy.errorHandler(w, r, err)
		}
		return
	}

	wp.httpProxy.ServeHTTP(w, r)
}
