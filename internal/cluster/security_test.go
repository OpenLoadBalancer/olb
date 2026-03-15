package cluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNodeAuthConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *NodeAuthConfig
		wantErr bool
	}{
		{
			name: "valid with CA cert",
			config: &NodeAuthConfig{
				CACertFile: "/path/to/ca.pem",
			},
			wantErr: false,
		},
		{
			name: "valid with cert and key",
			config: &NodeAuthConfig{
				CertFile: "/path/to/cert.pem",
				KeyFile:  "/path/to/key.pem",
			},
			wantErr: false,
		},
		{
			name: "valid with shared secret",
			config: &NodeAuthConfig{
				SharedSecret: []byte("secret123"),
			},
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  &NodeAuthConfig{},
			wantErr: true,
		},
		{
			name: "cert without key",
			config: &NodeAuthConfig{
				CertFile: "/path/to/cert.pem",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildNodeTLSConfig_NilConfig(t *testing.T) {
	_, err := BuildNodeTLSConfig(nil)
	if err == nil {
		t.Error("Expected error for nil config")
	}
}

func TestBuildNodeTLSConfig_WithCerts(t *testing.T) {
	// Generate a temporary CA cert, node cert, and key for testing
	tmpDir := t.TempDir()

	caKey, caCert, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	nodeKey, nodeCert, err := generateTestNodeCert(caKey, caCert, "node1")
	if err != nil {
		t.Fatalf("Failed to generate test node cert: %v", err)
	}

	// Write files
	caCertPath := filepath.Join(tmpDir, "ca.pem")
	certPath := filepath.Join(tmpDir, "node.pem")
	keyPath := filepath.Join(tmpDir, "node-key.pem")

	if err := writePEM(caCertPath, "CERTIFICATE", caCert); err != nil {
		t.Fatalf("Failed to write CA cert: %v", err)
	}
	if err := writePEM(certPath, "CERTIFICATE", nodeCert); err != nil {
		t.Fatalf("Failed to write node cert: %v", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(nodeKey)
	if err != nil {
		t.Fatalf("Failed to marshal key: %v", err)
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyBytes); err != nil {
		t.Fatalf("Failed to write node key: %v", err)
	}

	config := &NodeAuthConfig{
		CACertFile: caCertPath,
		CertFile:   certPath,
		KeyFile:    keyPath,
	}

	tlsConfig, err := BuildNodeTLSConfig(config)
	if err != nil {
		t.Fatalf("BuildNodeTLSConfig error: %v", err)
	}

	if tlsConfig.RootCAs == nil {
		t.Error("Expected RootCAs to be set")
	}
	if tlsConfig.ClientCAs == nil {
		t.Error("Expected ClientCAs to be set")
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(tlsConfig.Certificates))
	}
}

func TestBuildNodeTLSConfig_WithAllowedNodeIDs(t *testing.T) {
	tmpDir := t.TempDir()

	caKey, caCert, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	nodeKey, nodeCert, err := generateTestNodeCert(caKey, caCert, "node1")
	if err != nil {
		t.Fatalf("Failed to generate test node cert: %v", err)
	}

	caCertPath := filepath.Join(tmpDir, "ca.pem")
	certPath := filepath.Join(tmpDir, "node.pem")
	keyPath := filepath.Join(tmpDir, "node-key.pem")

	writePEM(caCertPath, "CERTIFICATE", caCert)
	writePEM(certPath, "CERTIFICATE", nodeCert)
	keyBytes, _ := x509.MarshalECPrivateKey(nodeKey)
	writePEM(keyPath, "EC PRIVATE KEY", keyBytes)

	config := &NodeAuthConfig{
		CACertFile:     caCertPath,
		CertFile:       certPath,
		KeyFile:        keyPath,
		AllowedNodeIDs: []string{"node1", "node2"},
	}

	tlsConfig, err := BuildNodeTLSConfig(config)
	if err != nil {
		t.Fatalf("BuildNodeTLSConfig error: %v", err)
	}

	if tlsConfig.VerifyPeerCertificate == nil {
		t.Error("Expected VerifyPeerCertificate to be set when AllowedNodeIDs is provided")
	}
}

func TestBuildNodeTLSConfig_InvalidCACert(t *testing.T) {
	tmpDir := t.TempDir()
	caCertPath := filepath.Join(tmpDir, "bad-ca.pem")
	os.WriteFile(caCertPath, []byte("not a cert"), 0644)

	config := &NodeAuthConfig{
		CACertFile: caCertPath,
	}

	_, err := BuildNodeTLSConfig(config)
	if err == nil {
		t.Error("Expected error for invalid CA cert")
	}
}

func TestBuildNodeTLSConfig_MissingCACertFile(t *testing.T) {
	config := &NodeAuthConfig{
		CACertFile: "/nonexistent/path/ca.pem",
	}

	_, err := BuildNodeTLSConfig(config)
	if err == nil {
		t.Error("Expected error for missing CA cert file")
	}
}

func TestGenerateNodeToken(t *testing.T) {
	secret := []byte("test-secret-key")
	nodeID := "node1"

	token := GenerateNodeToken(nodeID, secret)

	// Token should be a non-empty hex string
	if token == "" {
		t.Error("Token should not be empty")
	}

	// Token should be 64 chars (SHA-256 hex encoded)
	if len(token) != 64 {
		t.Errorf("Token length = %d, want 64", len(token))
	}

	// Same input should produce same token (deterministic)
	token2 := GenerateNodeToken(nodeID, secret)
	if token != token2 {
		t.Error("Same input should produce the same token")
	}

	// Different node ID should produce different token
	token3 := GenerateNodeToken("node2", secret)
	if token == token3 {
		t.Error("Different node IDs should produce different tokens")
	}

	// Different secret should produce different token
	token4 := GenerateNodeToken(nodeID, []byte("other-secret"))
	if token == token4 {
		t.Error("Different secrets should produce different tokens")
	}
}

func TestVerifyNodeToken(t *testing.T) {
	secret := []byte("test-secret-key")
	nodeID := "node1"

	token := GenerateNodeToken(nodeID, secret)

	// Valid token
	if !VerifyNodeToken(token, nodeID, secret) {
		t.Error("Valid token should verify successfully")
	}

	// Wrong node ID
	if VerifyNodeToken(token, "node2", secret) {
		t.Error("Token should not verify for wrong node ID")
	}

	// Wrong secret
	if VerifyNodeToken(token, nodeID, []byte("wrong-secret")) {
		t.Error("Token should not verify with wrong secret")
	}

	// Invalid token
	if VerifyNodeToken("invalid-hex-token", nodeID, secret) {
		t.Error("Invalid token should not verify")
	}

	// Empty token
	if VerifyNodeToken("", nodeID, secret) {
		t.Error("Empty token should not verify")
	}
}

func TestVerifyNodeCertificate(t *testing.T) {
	// Test with valid certificate
	caKey, caCert, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	_, nodeCertDER, err := generateTestNodeCert(caKey, caCert, "test-node")
	if err != nil {
		t.Fatalf("Failed to generate test node cert: %v", err)
	}

	// Valid certificate with CN
	err = VerifyNodeCertificate([][]byte{nodeCertDER}, nil)
	if err != nil {
		t.Errorf("Expected no error for valid cert, got: %v", err)
	}

	// No certificates
	err = VerifyNodeCertificate([][]byte{}, nil)
	if err == nil {
		t.Error("Expected error for empty certificates")
	}

	// Invalid certificate DER
	err = VerifyNodeCertificate([][]byte{[]byte("not a cert")}, nil)
	if err == nil {
		t.Error("Expected error for invalid certificate DER")
	}
}

func TestVerifyNodeCertificateWithAllowed(t *testing.T) {
	caKey, caCert, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	_, nodeCertDER, err := generateTestNodeCert(caKey, caCert, "allowed-node")
	if err != nil {
		t.Fatalf("Failed to generate test node cert: %v", err)
	}

	// Allowed node
	allowed := map[string]struct{}{"allowed-node": {}}
	err = verifyNodeCertificateWithAllowed([][]byte{nodeCertDER}, nil, allowed)
	if err != nil {
		t.Errorf("Expected allowed node to pass, got: %v", err)
	}

	// Disallowed node
	notAllowed := map[string]struct{}{"other-node": {}}
	err = verifyNodeCertificateWithAllowed([][]byte{nodeCertDER}, nil, notAllowed)
	if err == nil {
		t.Error("Expected disallowed node to fail")
	}
}

func TestNodeAuthMiddleware_AcceptAndAuthenticate(t *testing.T) {
	secret := []byte("cluster-secret")
	nodeID := "node1"

	// Create a TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Wrap with auth middleware (allow all nodes)
	authListener := NewNodeAuthMiddleware(listener, secret, nil)

	// Start accepting in goroutine
	accepted := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := authListener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		accepted <- conn
	}()

	// Connect as a client and authenticate
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	err = AuthenticateToNode(conn, nodeID, secret)
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	// Wait for accepted connection
	select {
	case <-accepted:
		// Success
	case err := <-errCh:
		t.Fatalf("Accept error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for accepted connection")
	}
}

func TestNodeAuthMiddleware_RejectsInvalidToken(t *testing.T) {
	secret := []byte("cluster-secret")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	authListener := NewNodeAuthMiddleware(listener, secret, nil)

	// Start accepting (will reject and loop, so we need to close listener to stop)
	go func() {
		authListener.Accept()
	}()

	// Connect and send wrong token
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Authenticate with wrong secret
	err = AuthenticateToNode(conn, "node1", []byte("wrong-secret"))
	if err == nil {
		t.Error("Expected authentication to fail with wrong secret")
	}
}

func TestNodeAuthMiddleware_AllowedNodes(t *testing.T) {
	secret := []byte("cluster-secret")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Only allow node2
	authListener := NewNodeAuthMiddleware(listener, secret, []string{"node2"})

	go func() {
		authListener.Accept()
	}()

	// Try to authenticate as node1 (not allowed)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	err = AuthenticateToNode(conn, "node1", secret)
	if err == nil {
		t.Error("Expected authentication to fail for disallowed node")
	}
}

func TestNodeAuthMiddleware_Close(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	secret := []byte("test-secret")
	authListener := NewNodeAuthMiddleware(listener, secret, nil)

	// Close the auth listener
	err = authListener.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// After close, the underlying listener should be closed too.
	// Trying to accept should fail.
	_, err = listener.Accept()
	if err == nil {
		t.Error("Accept() should fail after Close()")
	}
}

func TestNodeAuthMiddleware_Addr(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	secret := []byte("test-secret")
	authListener := NewNodeAuthMiddleware(listener, secret, nil)

	// Addr() should return the same address as the underlying listener
	addr := authListener.Addr()
	if addr == nil {
		t.Fatal("Addr() returned nil")
	}

	if addr.String() != listener.Addr().String() {
		t.Errorf("Addr() = %q, want %q", addr.String(), listener.Addr().String())
	}

	if addr.Network() != "tcp" {
		t.Errorf("Addr().Network() = %q, want tcp", addr.Network())
	}
}

func TestNodeAuthMiddleware_AddRemoveAllowedNode(t *testing.T) {
	secret := []byte("test-secret")
	middleware := NewNodeAuthMiddleware(nil, secret, []string{"node1"})

	// Check initial state
	middleware.mu.RLock()
	_, ok1 := middleware.allowedNodeIDs["node1"]
	_, ok2 := middleware.allowedNodeIDs["node2"]
	middleware.mu.RUnlock()

	if !ok1 {
		t.Error("Expected node1 to be allowed initially")
	}
	if ok2 {
		t.Error("Expected node2 to not be allowed initially")
	}

	// Add node2
	middleware.AddAllowedNode("node2")
	middleware.mu.RLock()
	_, ok2 = middleware.allowedNodeIDs["node2"]
	middleware.mu.RUnlock()
	if !ok2 {
		t.Error("Expected node2 to be allowed after add")
	}

	// Remove node1
	middleware.RemoveAllowedNode("node1")
	middleware.mu.RLock()
	_, ok1 = middleware.allowedNodeIDs["node1"]
	middleware.mu.RUnlock()
	if ok1 {
		t.Error("Expected node1 to not be allowed after remove")
	}
}

// Test helper functions

func generateTestCA() (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	return key, certDER, nil
}

func generateTestNodeCert(caKey *ecdsa.PrivateKey, caCertDER []byte, cn string) (*ecdsa.PrivateKey, []byte, error) {
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	return key, certDER, nil
}

func writePEM(path, blockType string, data []byte) error {
	block := &pem.Block{
		Type:  blockType,
		Bytes: data,
	}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0644)
}
