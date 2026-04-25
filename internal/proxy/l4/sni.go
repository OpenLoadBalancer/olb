package l4

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// SNIRouterConfig configures SNI-based routing.
type SNIRouterConfig struct {
	// DefaultBackend is the backend to use when no SNI match is found.
	DefaultBackend string

	// Passthrough enables TLS passthrough mode (no termination).
	Passthrough bool

	// ReadTimeout is the timeout for reading the ClientHello.
	ReadTimeout time.Duration

	// MaxHandshakeSize is the maximum size of the ClientHello to read.
	MaxHandshakeSize int

	// MaxConnections limits concurrent connections. 0 = unlimited (default 10000).
	MaxConnections int
}

// DefaultSNIRouterConfig returns a default SNI router configuration.
func DefaultSNIRouterConfig() *SNIRouterConfig {
	return &SNIRouterConfig{
		Passthrough:      true,
		ReadTimeout:      5 * time.Second,
		MaxHandshakeSize: 16 * 1024, // 16KB
		MaxConnections:   10000,
	}
}

// SNIRouter routes TCP connections based on TLS SNI.
type SNIRouter struct {
	config  *SNIRouterConfig
	routes  map[string]*backend.Backend
	mu      sync.RWMutex
	running atomic.Bool
}

// NewSNIRouter creates a new SNI router.
func NewSNIRouter(config *SNIRouterConfig) *SNIRouter {
	if config == nil {
		config = DefaultSNIRouterConfig()
	}

	return &SNIRouter{
		config: config,
		routes: make(map[string]*backend.Backend),
	}
}

// AddRoute adds an SNI to backend mapping.
func (r *SNIRouter) AddRoute(sni string, backend *backend.Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[strings.ToLower(sni)] = backend
}

// RemoveRoute removes an SNI route.
func (r *SNIRouter) RemoveRoute(sni string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.routes, strings.ToLower(sni))
}

// GetRoute gets the backend for an SNI.
func (r *SNIRouter) GetRoute(sni string) *backend.Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try exact match
	if backend, ok := r.routes[strings.ToLower(sni)]; ok {
		return backend
	}

	// Try wildcard match
	parts := strings.Split(sni, ".")
	for i := 1; i < len(parts); i++ {
		wildcard := "*." + strings.Join(parts[i:], ".")
		if backend, ok := r.routes[wildcard]; ok {
			return backend
		}
	}

	return nil
}

// RouteConnection routes a connection based on SNI.
func (r *SNIRouter) RouteConnection(conn net.Conn) error {
	// Peek at the ClientHello to extract SNI
	sni, peekedConn, err := r.peekClientHello(conn)
	if err != nil {
		// Not a TLS connection or error, use default
		return r.routeToDefault(peekedConn)
	}

	// Get backend for SNI
	backend := r.GetRoute(sni)
	if backend == nil {
		if r.config.DefaultBackend != "" {
			return r.routeToDefault(peekedConn)
		}
		return olbErrors.Newf(olbErrors.CodeNotFound, "no route for SNI: %s", sni)
	}

	// Connect to backend
	if err := r.proxyToBackend(peekedConn, backend); err != nil {
		return err
	}

	return nil
}

// peekClientHello peeks at the TLS ClientHello and extracts SNI.
// It returns the SNI and a connection that includes the peeked data.
func (r *SNIRouter) peekClientHello(conn net.Conn) (string, net.Conn, error) {
	// Set read timeout
	if r.config.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(r.config.ReadTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	// Read TLS record header (5 bytes: type, version, length)
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", conn, err
	}

	// Check this is a ClientHello (type 0x16) and version is valid
	if header[0] != 0x16 {
		return "", &peekedConn{Conn: conn, peeked: header}, fmt.Errorf("not a TLS handshake record")
	}

	// Read the rest of the record based on declared length
	recordLen := int(header[3])<<8 | int(header[4])
	if recordLen > r.config.MaxHandshakeSize {
		return "", conn, fmt.Errorf("TLS ClientHello too large (%d bytes, max %d)", recordLen, r.config.MaxHandshakeSize)
	}
	record := make([]byte, recordLen)
	if _, err := io.ReadFull(conn, record); err != nil {
		return "", conn, err
	}
	buf := append(header, record...)

	// Parse ClientHello
	sni, err := ParseClientHelloSNI(buf)
	if err != nil {
		// Return connection with peeked data for non-TLS connections
		return "", &peekedConn{
			Conn:   conn,
			peeked: buf,
		}, err
	}

	// Return connection with peeked data
	return sni, &peekedConn{
		Conn:   conn,
		peeked: buf,
	}, nil
}

// routeToDefault routes to the default backend.
func (r *SNIRouter) routeToDefault(conn net.Conn) error {
	_ = conn.Close() // Best-effort close on rejected connection
	return olbErrors.New(olbErrors.CodeNotFound, "no route found and no default backend")
}

// proxyToBackend proxies the connection to a backend.
func (r *SNIRouter) proxyToBackend(clientConn net.Conn, backend *backend.Backend) error {
	defer clientConn.Close()

	// Acquire connection slot
	if !backend.AcquireConn() {
		return olbErrors.New(olbErrors.CodeUnavailable, "backend at max connections")
	}
	defer backend.ReleaseConn()

	// Connect to backend
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	backendConn, err := dialer.Dial("tcp", backend.Address)
	if err != nil {
		backend.RecordError()
		return err
	}
	defer backendConn.Close()

	// Bidirectional copy
	_, _, err = CopyBidirectional(clientConn, backendConn, 5*time.Minute)
	return err
}

// peekedConn wraps a connection with peeked data.
type peekedConn struct {
	net.Conn
	peeked     []byte
	peekOffset int
}

// Read reads from the peeked data first, then the underlying connection.
func (c *peekedConn) Read(p []byte) (n int, err error) {
	// Serve from peeked buffer first
	if c.peekOffset < len(c.peeked) {
		n = copy(p, c.peeked[c.peekOffset:])
		c.peekOffset += n
		return n, nil
	}

	// Then read from underlying connection
	return c.Conn.Read(p)
}

// TLSRecordHeader represents a TLS record header.
type TLSRecordHeader struct {
	ContentType uint8
	Version     uint16
	Length      uint16
}

// ParseClientHelloSNI parses the SNI from a TLS ClientHello.
func ParseClientHelloSNI(data []byte) (string, error) {
	// Check if it's a valid TLS record
	if len(data) < 5 {
		return "", errors.New("data too short for TLS record")
	}

	// TLS record header
	contentType := data[0]
	if contentType != 0x16 { // Handshake
		return "", errors.New("not a TLS handshake record")
	}

	// Check TLS version (SSL 3.0, TLS 1.0-1.3)
	version := binary.BigEndian.Uint16(data[1:3])
	if version < 0x0300 || version > 0x0304 {
		return "", errors.New("invalid TLS version")
	}

	// Record length
	recordLen := binary.BigEndian.Uint16(data[3:5])
	if int(recordLen)+5 > len(data) {
		return "", errors.New("incomplete TLS record")
	}

	// Handshake header
	if len(data) < 9 {
		return "", errors.New("data too short for handshake header")
	}

	handshakeType := data[5]
	if handshakeType != 0x01 { // ClientHello
		return "", errors.New("not a ClientHello message")
	}

	// Parse ClientHello
	return parseClientHello(data[5:])
}

// parseClientHello parses the ClientHello message.
func parseClientHello(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("data too short")
	}

	// Handshake length (3 bytes)
	handshakeLen := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if handshakeLen+4 > len(data) {
		return "", errors.New("incomplete ClientHello")
	}

	data = data[4:] // Skip handshake header

	// ClientHello version
	if len(data) < 2 {
		return "", errors.New("data too short for version")
	}
	version := binary.BigEndian.Uint16(data[0:2])
	if version < 0x0300 || version > 0x0304 {
		return "", errors.New("invalid ClientHello version")
	}
	data = data[2:]

	// Random (32 bytes)
	if len(data) < 32 {
		return "", errors.New("data too short for random")
	}
	data = data[32:]

	// Session ID length
	if len(data) < 1 {
		return "", errors.New("data too short for session ID length")
	}
	sessionIDLen := int(data[0])
	data = data[1:]

	// Session ID
	if len(data) < sessionIDLen {
		return "", errors.New("data too short for session ID")
	}
	data = data[sessionIDLen:]

	// Cipher suites
	if len(data) < 2 {
		return "", errors.New("data too short for cipher suites length")
	}
	cipherSuitesLen := int(binary.BigEndian.Uint16(data[0:2]))
	data = data[2:]

	if len(data) < cipherSuitesLen {
		return "", errors.New("data too short for cipher suites")
	}
	data = data[cipherSuitesLen:]

	// Compression methods
	if len(data) < 1 {
		return "", errors.New("data too short for compression length")
	}
	compressionLen := int(data[0])
	data = data[1:]

	if len(data) < compressionLen {
		return "", errors.New("data too short for compression methods")
	}
	data = data[compressionLen:]

	// Extensions
	if len(data) < 2 {
		// No extensions, no SNI
		return "", errors.New("no extensions")
	}

	extensionsLen := int(binary.BigEndian.Uint16(data[0:2]))
	data = data[2:]

	if len(data) < extensionsLen {
		return "", errors.New("data too short for extensions")
	}
	extensions := data[:extensionsLen]

	// Parse extensions to find SNI
	return parseSNIExtension(extensions)
}

// parseSNIExtension parses the SNI from extensions.
func parseSNIExtension(extensions []byte) (string, error) {
	for len(extensions) >= 4 {
		extensionType := binary.BigEndian.Uint16(extensions[0:2])
		extensionLen := int(binary.BigEndian.Uint16(extensions[2:4]))

		if len(extensions) < 4+extensionLen {
			return "", errors.New("incomplete extension")
		}

		extensionData := extensions[4 : 4+extensionLen]

		if extensionType == 0x0000 { // SNI extension
			return parseSNIList(extensionData)
		}

		extensions = extensions[4+extensionLen:]
	}

	return "", errors.New("sni extension not found")
}

// parseSNIList parses the SNI list.
func parseSNIList(data []byte) (string, error) {
	if len(data) < 2 {
		return "", errors.New("data too short for SNI list length")
	}

	sniListLen := int(binary.BigEndian.Uint16(data[0:2]))
	if len(data) < 2+sniListLen {
		return "", errors.New("data too short for SNI list")
	}

	data = data[2 : 2+sniListLen]

	// Parse SNI entries
	for len(data) >= 3 {
		sniType := data[0]
		sniLen := int(binary.BigEndian.Uint16(data[1:3]))

		if len(data) < 3+sniLen {
			return "", errors.New("data too short for SNI entry")
		}

		sniData := data[3 : 3+sniLen]

		if sniType == 0x00 { // Host name
			host := string(sniData)
			if err := validateSNIHostname(host); err != nil {
				return "", err
			}
			return host, nil
		}

		data = data[3+sniLen:]
	}

	return "", errors.New("no host name SNI found")
}

// validateSNIHostname checks that an SNI hostname conforms to RFC 5280 rules.
func validateSNIHostname(host string) error {
	if len(host) == 0 || len(host) > 253 {
		return fmt.Errorf("invalid SNI hostname length: %d", len(host))
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("invalid SNI hostname: control character at position %d", i)
		}
		// Only allow alphanumeric, hyphen, dot
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && c != '-' && c != '.' {
			return fmt.Errorf("invalid SNI hostname: invalid character %q at position %d", c, i)
		}
	}
	return nil
}

// SNIBasedProxy is a TCP proxy that routes based on SNI.
type SNIBasedProxy struct {
	router   *SNIRouter
	listener net.Listener
	config   *SNIRouterConfig

	maxConns    int64
	activeConns atomic.Int64
	running     atomic.Bool
	mu          sync.RWMutex
	wg          sync.WaitGroup
}

// NewSNIBasedProxy creates a new SNI-based proxy.
func NewSNIBasedProxy(config *SNIRouterConfig) *SNIBasedProxy {
	if config == nil {
		config = DefaultSNIRouterConfig()
	}
	maxConns := int64(config.MaxConnections)
	if maxConns <= 0 {
		maxConns = 10000
	}
	return &SNIBasedProxy{
		router:   NewSNIRouter(config),
		config:   config,
		maxConns: maxConns,
	}
}

// AddRoute adds an SNI route.
func (p *SNIBasedProxy) AddRoute(sni string, backend *backend.Backend) {
	p.router.AddRoute(sni, backend)
}

// RemoveRoute removes an SNI route.
func (p *SNIBasedProxy) RemoveRoute(sni string) {
	p.router.RemoveRoute(sni)
}

// Listen starts listening on the given address.
func (p *SNIBasedProxy) Listen(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.listener = listener
	p.mu.Unlock()

	return nil
}

// Start starts accepting connections.
func (p *SNIBasedProxy) Start() error {
	if p.listener == nil {
		return errors.New("not listening")
	}

	if !p.running.CompareAndSwap(false, true) {
		return errors.New("already running")
	}

	p.wg.Add(1)
	go p.acceptLoop()
	return nil
}

// acceptLoop accepts incoming connections.
func (p *SNIBasedProxy) acceptLoop() {
	defer p.wg.Done()
	for p.running.Load() {
		conn, err := p.listener.Accept()
		if err != nil {
			if !p.running.Load() {
				return
			}
			continue
		}

		// Enforce connection limit with CAS to prevent TOCTOU races.
		accepted := false
		for {
			current := p.activeConns.Load()
			if current >= p.maxConns {
				conn.Close()
				break
			}
			if p.activeConns.CompareAndSwap(current, current+1) {
				accepted = true
				break
			}
		}
		if !accepted {
			continue
		}

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer p.activeConns.Add(-1)
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[sni-proxy] panic recovered in connection handler: %v", r)
				}
			}()
			p.handleConnection(conn)
		}()
	}
}

// handleConnection handles a single connection.
func (p *SNIBasedProxy) handleConnection(conn net.Conn) {
	if err := p.router.RouteConnection(conn); err != nil {
		// Log error
	}
}

// Stop stops the proxy.
func (p *SNIBasedProxy) Stop() error {
	p.running.Store(false)

	p.mu.Lock()
	if p.listener != nil {
		p.listener.Close()
	}
	p.mu.Unlock()

	p.wg.Wait()
	return nil
}

// IsTLSConnection checks if the data is a TLS connection.
func IsTLSConnection(data []byte) bool {
	if len(data) < 6 {
		return false
	}

	// Check for TLS handshake record
	if data[0] != 0x16 { // Content type: Handshake
		return false
	}

	// Check version (SSL 3.0 or TLS 1.0-1.3)
	version := binary.BigEndian.Uint16(data[1:3])
	if version < 0x0300 || version > 0x0304 {
		return false
	}

	// Check handshake type (ClientHello = 1)
	if data[5] != 0x01 {
		return false
	}

	return true
}

// ExtractSNI extracts SNI from a connection without consuming it.
func ExtractSNI(conn net.Conn, timeout time.Duration) (string, net.Conn, error) {
	// Set timeout
	if timeout > 0 {
		conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	// Read initial data
	buf := make([]byte, 16*1024)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return "", conn, err
	}
	buf = buf[:n]

	// Check if TLS
	if !IsTLSConnection(buf) {
		return "", &peekedConn{Conn: conn, peeked: buf}, errors.New("not a TLS connection")
	}

	// Parse SNI
	sni, err := ParseClientHelloSNI(buf)
	if err != nil {
		return "", &peekedConn{Conn: conn, peeked: buf}, err
	}

	return sni, &peekedConn{Conn: conn, peeked: buf}, nil
}

// SNIProxy handles SNI-based routing with TLS passthrough.
type SNIProxy struct {
	routes map[string]string // SNI -> backend address
	mu     sync.RWMutex
	config *SNIRouterConfig
	dialer *net.Dialer
}

// NewSNIProxy creates a new SNI proxy.
func NewSNIProxy(config *SNIRouterConfig) *SNIProxy {
	if config == nil {
		config = DefaultSNIRouterConfig()
	}

	return &SNIProxy{
		routes: make(map[string]string),
		config: config,
		dialer: &net.Dialer{
			Timeout: 10 * time.Second,
		},
	}
}

// AddRoute adds an SNI to backend address mapping.
func (p *SNIProxy) AddRoute(sni, backendAddr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes[strings.ToLower(sni)] = backendAddr
}

// GetBackend gets the backend address for an SNI.
func (p *SNIProxy) GetBackend(sni string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Try exact match
	if addr, ok := p.routes[strings.ToLower(sni)]; ok {
		return addr
	}

	// Try wildcard match
	parts := strings.Split(sni, ".")
	for i := 1; i < len(parts); i++ {
		wildcard := "*." + strings.Join(parts[i:], ".")
		if addr, ok := p.routes[wildcard]; ok {
			return addr
		}
	}

	return ""
}

// HandleConnection handles a connection with SNI-based routing.
func (p *SNIProxy) HandleConnection(conn net.Conn) {
	defer conn.Close()

	// Extract SNI
	sni, peekedConn, err := ExtractSNI(conn, p.config.ReadTimeout)
	if err != nil {
		// Not TLS or error
		return
	}

	// Get backend
	backendAddr := p.GetBackend(sni)
	if backendAddr == "" {
		backendAddr = p.config.DefaultBackend
	}

	if backendAddr == "" {
		return
	}

	// Connect to backend
	backendConn, err := p.dialer.Dial("tcp", backendAddr)
	if err != nil {
		return
	}
	defer backendConn.Close()

	// Copy data (5 minute idle timeout to prevent hung connections)
	CopyBidirectional(peekedConn, backendConn, 5*time.Minute)
}

// CreateTLSConfigForSNI creates a TLS config for SNI-based routing.
// This is used when terminating TLS at the proxy.
func CreateTLSConfigForSNI(getCertificate func(string) (*tls.Certificate, error)) *tls.Config {
	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return getCertificate(hello.ServerName)
		},
	}
}

// SNIMatcher matches SNI patterns including wildcards.
type SNIMatcher struct {
	exact     map[string]bool
	wildcards []string
}

// NewSNIMatcher creates a new SNI matcher.
func NewSNIMatcher() *SNIMatcher {
	return &SNIMatcher{
		exact: make(map[string]bool),
	}
}

// Add adds a pattern to match.
func (m *SNIMatcher) Add(pattern string) {
	if strings.HasPrefix(pattern, "*.") {
		m.wildcards = append(m.wildcards, pattern[2:])
	} else {
		m.exact[pattern] = true
	}
}

// Match checks if an SNI matches any pattern.
func (m *SNIMatcher) Match(sni string) bool {
	// Check exact match
	if m.exact[sni] {
		return true
	}

	// Check wildcard matches
	for _, wildcard := range m.wildcards {
		if strings.HasSuffix(sni, wildcard) && sni != wildcard {
			return true
		}
	}

	return false
}

// ParseTLSVersion parses the TLS version from the record.
func ParseTLSVersion(data []byte) (string, error) {
	if len(data) < 3 {
		return "", errors.New("data too short")
	}

	version := binary.BigEndian.Uint16(data[1:3])
	switch version {
	case 0x0300:
		return "SSL 3.0", nil
	case 0x0301:
		return "TLS 1.0", nil
	case 0x0302:
		return "TLS 1.1", nil
	case 0x0303:
		return "TLS 1.2", nil
	case 0x0304:
		return "TLS 1.3", nil
	default:
		return fmt.Sprintf("Unknown (0x%04x)", version), nil
	}
}

// TLSRecordInfo contains information about a TLS record.
type TLSRecordInfo struct {
	ContentType   string
	Version       string
	Length        int
	IsClientHello bool
}

// ParseTLSRecordInfo parses information from a TLS record.
func ParseTLSRecordInfo(data []byte) (*TLSRecordInfo, error) {
	if len(data) < 5 {
		return nil, errors.New("data too short for TLS record")
	}

	info := &TLSRecordInfo{}

	// Content type
	switch data[0] {
	case 0x16:
		info.ContentType = "Handshake"
	case 0x14:
		info.ContentType = "ChangeCipherSpec"
	case 0x15:
		info.ContentType = "Alert"
	case 0x17:
		info.ContentType = "Application"
	default:
		info.ContentType = fmt.Sprintf("Unknown (%d)", data[0])
	}

	// Version
	version, _ := ParseTLSVersion(data)
	info.Version = version

	// Length
	info.Length = int(binary.BigEndian.Uint16(data[3:5]))

	// Check if ClientHello
	info.IsClientHello = len(data) > 5 && data[5] == 0x01

	return info, nil
}

// BufferedConn wraps a connection with a buffer for peeking.
type BufferedConn struct {
	net.Conn
	reader *bytes.Reader
	buf    []byte
}

// NewBufferedConn creates a buffered connection.
func NewBufferedConn(conn net.Conn, initial []byte) *BufferedConn {
	return &BufferedConn{
		Conn:   conn,
		buf:    initial,
		reader: bytes.NewReader(initial),
	}
}

// Read reads from the buffer first, then the connection.
func (c *BufferedConn) Read(p []byte) (n int, err error) {
	if c.reader.Len() > 0 {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}
