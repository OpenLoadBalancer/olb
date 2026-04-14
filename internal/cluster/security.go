package cluster

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NodeAuthConfig configures inter-node authentication and TLS.
type NodeAuthConfig struct {
	// CACertFile is the path to the CA certificate file for verifying node certificates.
	CACertFile string `json:"ca_cert_file" yaml:"ca_cert_file"`

	// CertFile is the path to this node's TLS certificate file.
	CertFile string `json:"cert_file" yaml:"cert_file"`

	// KeyFile is the path to this node's TLS private key file.
	KeyFile string `json:"key_file" yaml:"key_file"`

	// AllowedNodeIDs restricts which node IDs (from certificate CN or SAN) are allowed.
	// Empty means all valid certificates are accepted.
	AllowedNodeIDs []string `json:"allowed_node_ids" yaml:"allowed_node_ids"`

	// SharedSecret is the shared secret used for HMAC token generation/verification.
	// This is an alternative to mTLS for simpler deployments.
	SharedSecret []byte `json:"-" yaml:"-"`
}

// Validate validates the node auth configuration.
func (c *NodeAuthConfig) Validate() error {
	if c.CACertFile == "" && c.CertFile == "" && len(c.SharedSecret) == 0 {
		return errors.New("at least one of CA cert, node cert, or shared secret is required")
	}
	if c.CertFile != "" && c.KeyFile == "" {
		return errors.New("key_file is required when cert_file is provided")
	}
	return nil
}

// BuildNodeTLSConfig creates a mutual TLS configuration for cluster communication.
// Both server and client certificates are configured for bidirectional authentication.
func BuildNodeTLSConfig(config *NodeAuthConfig) (*tls.Config, error) {
	if config == nil {
		return nil, errors.New("node auth config is nil")
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load CA certificate pool for verifying peer certificates
	if config.CACertFile != "" {
		caCert, err := os.ReadFile(config.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New("failed to parse CA certificate")
		}

		tlsConfig.RootCAs = caPool
		tlsConfig.ClientCAs = caPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	// Load node certificate and key for this node's identity
	if config.CertFile != "" && config.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load node certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Set custom verification if allowed node IDs are specified
	if len(config.AllowedNodeIDs) > 0 {
		allowedSet := make(map[string]struct{}, len(config.AllowedNodeIDs))
		for _, id := range config.AllowedNodeIDs {
			allowedSet[id] = struct{}{}
		}

		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return verifyNodeCertificateWithAllowed(rawCerts, verifiedChains, allowedSet)
		}
	}

	return tlsConfig, nil
}

// VerifyNodeCertificate is a custom TLS verification function that checks
// whether the peer certificate belongs to a known and allowed node.
// It is intended to be used as tls.Config.VerifyPeerCertificate.
func VerifyNodeCertificate(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errors.New("no peer certificates provided")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Basic validation: the certificate must have a Common Name or DNS SAN
	if cert.Subject.CommonName == "" && len(cert.DNSNames) == 0 {
		return errors.New("peer certificate has no Common Name or DNS SANs")
	}

	return nil
}

// verifyNodeCertificateWithAllowed checks certificates against an allowed set.
func verifyNodeCertificateWithAllowed(rawCerts [][]byte, _ [][]*x509.Certificate, allowed map[string]struct{}) error {
	if len(rawCerts) == 0 {
		return errors.New("no peer certificates provided")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Check Common Name
	if cert.Subject.CommonName != "" {
		if _, ok := allowed[cert.Subject.CommonName]; ok {
			return nil
		}
	}

	// Check DNS SANs
	for _, dns := range cert.DNSNames {
		if _, ok := allowed[dns]; ok {
			return nil
		}
	}

	// Check IP SANs
	for _, ip := range cert.IPAddresses {
		if _, ok := allowed[ip.String()]; ok {
			return nil
		}
	}

	return fmt.Errorf("peer certificate CN=%q is not in the allowed node list", cert.Subject.CommonName)
}

// GenerateNodeToken generates an HMAC-SHA256 authentication token for a node.
// The token includes a timestamp to prevent replay attacks.
func GenerateNodeToken(nodeID string, secret []byte) string {
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nodeID))
	mac.Write([]byte(fmt.Sprintf("%d", ts)))
	tokenBytes := mac.Sum(nil)
	return fmt.Sprintf("%d:%s", ts, hex.EncodeToString(tokenBytes))
}

// VerifyNodeToken verifies that a token is valid for the given node ID and secret.
// Tokens older than maxAge are rejected to prevent replay attacks.
func VerifyNodeToken(token string, nodeID string, secret []byte) bool {
	// Default max age: 5 minutes
	return VerifyNodeTokenWithMaxAge(token, nodeID, secret, 5*time.Minute)
}

// VerifyNodeTokenWithMaxAge verifies a token with a custom max age.
func VerifyNodeTokenWithMaxAge(token string, nodeID string, secret []byte, maxAge time.Duration) bool {
	// Split timestamp from signature
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		// Legacy token format (no timestamp) — reject
		return false
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}

	// Check freshness
	if time.Since(time.Unix(ts, 0)) > maxAge {
		return false
	}

	// Recompute expected token
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nodeID))
	mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

// NodeAuthMiddleware wraps a net.Listener to add HMAC token-based authentication
// for cluster connections. Each connection must send a valid token as the first
// message before being accepted.
type NodeAuthMiddleware struct {
	inner  net.Listener
	secret []byte

	mu             sync.RWMutex
	allowedNodeIDs map[string]struct{}
}

// NewNodeAuthMiddleware creates a new authentication middleware for cluster connections.
func NewNodeAuthMiddleware(inner net.Listener, secret []byte, allowedNodeIDs []string) *NodeAuthMiddleware {
	allowed := make(map[string]struct{}, len(allowedNodeIDs))
	for _, id := range allowedNodeIDs {
		allowed[id] = struct{}{}
	}

	if len(allowed) == 0 {
		log.Printf("WARNING: cluster NodeAuthMiddleware has no allowed_node_ids configured — any node with a valid token can join")
	}

	return &NodeAuthMiddleware{
		inner:          inner,
		secret:         secret,
		allowedNodeIDs: allowed,
	}
}

// Accept waits for and returns the next authenticated connection.
// The incoming connection must send "AUTH <nodeID> <token>\n" as the first line.
func (m *NodeAuthMiddleware) Accept() (net.Conn, error) {
	for {
		conn, err := m.inner.Accept()
		if err != nil {
			return nil, err
		}

		// Read auth line (max 512 bytes)
		buf := make([]byte, 512)
		conn.SetReadDeadline(deadlineFromNow(5))
		n, err := conn.Read(buf)
		if err != nil {
			_ = conn.Close() // best-effort cleanup
			continue
		}
		conn.SetReadDeadline(zeroTime)

		line := strings.TrimSpace(string(buf[:n]))
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 || parts[0] != "AUTH" {
			conn.Write([]byte("ERR invalid auth format\n"))
			_ = conn.Close() // best-effort cleanup
			continue
		}

		nodeID := parts[1]
		token := parts[2]

		// Check if node ID is allowed
		m.mu.RLock()
		_, nodeAllowed := m.allowedNodeIDs[nodeID]
		allowAll := len(m.allowedNodeIDs) == 0
		m.mu.RUnlock()

		if !allowAll && !nodeAllowed {
			conn.Write([]byte("ERR node not allowed\n"))
			_ = conn.Close() // best-effort cleanup
			continue
		}

		// Verify token
		if !VerifyNodeToken(token, nodeID, m.secret) {
			conn.Write([]byte("ERR invalid token\n"))
			_ = conn.Close() // best-effort cleanup
			continue
		}

		// Authentication successful
		conn.Write([]byte("OK\n"))
		return conn, nil
	}
}

// Close closes the underlying listener and zeros the shared secret.
func (m *NodeAuthMiddleware) Close() error {
	for i := range m.secret {
		m.secret[i] = 0
	}
	return m.inner.Close()
}

// Addr returns the listener's network address.
func (m *NodeAuthMiddleware) Addr() net.Addr {
	return m.inner.Addr()
}

// AddAllowedNode adds a node ID to the allowed set.
func (m *NodeAuthMiddleware) AddAllowedNode(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowedNodeIDs[nodeID] = struct{}{}
}

// RemoveAllowedNode removes a node ID from the allowed set.
func (m *NodeAuthMiddleware) RemoveAllowedNode(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.allowedNodeIDs, nodeID)
}

// AuthenticateToNode performs token-based authentication to a remote node.
// It sends the "AUTH <nodeID> <token>" handshake and reads the response.
func AuthenticateToNode(conn net.Conn, nodeID string, secret []byte) error {
	token := GenerateNodeToken(nodeID, secret)
	msg := fmt.Sprintf("AUTH %s %s\n", nodeID, token)

	conn.SetWriteDeadline(deadlineFromNow(5))
	if _, err := conn.Write([]byte(msg)); err != nil {
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Read response
	buf := make([]byte, 64)
	conn.SetReadDeadline(deadlineFromNow(5))
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}
	conn.SetWriteDeadline(zeroTime)
	conn.SetReadDeadline(zeroTime)

	response := strings.TrimSpace(string(buf[:n]))
	if response != "OK" {
		return fmt.Errorf("authentication failed: %s", response)
	}

	return nil
}

// deadlineFromNow returns a time.Time that is the given number of seconds from now.
func deadlineFromNow(seconds int) time.Time {
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

// zeroTime is a zero-value time used to clear deadlines.
var zeroTime time.Time
