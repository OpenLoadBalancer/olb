package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/engine"
	olbTLS "github.com/openloadbalancer/olb/internal/tls"
)

// generateCACert generates a self-signed CA certificate and key.
func generateCACert(t *testing.T, name string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{name},
			CommonName:   name,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	x509Cert, _ := x509.ParseCertificate(certDER)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return x509Cert, priv, certPEM, keyPEM
}

// generateClientCert generates a client certificate signed by the given CA.
// If ca or caKey is nil, the certificate is self-signed.
func generateClientCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, commonName string) ([]byte, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate client key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{"OLB Test Client"},
			CommonName:   commonName,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// If no CA provided, self-sign
	parent := ca
	signer := caKey
	if parent == nil {
		parent = &template
		signer = priv
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, parent, &priv.PublicKey, signer)
	if err != nil {
		t.Fatalf("Failed to create client certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM
}

// writeCertFiles writes PEM data to temp files and returns their paths.
func writeCertFiles(t *testing.T, tmpDir, certName, keyName string, certPEM, keyPEM []byte) (string, string) {
	t.Helper()
	certPath := filepath.Join(tmpDir, certName)
	keyPath := filepath.Join(tmpDir, keyName)
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		t.Fatalf("Failed to write cert %s: %v", certName, err)
	}
	if len(keyPEM) > 0 {
		if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
			t.Fatalf("Failed to write key %s: %v", keyName, err)
		}
	}
	return certPath, keyPath
}

// startEngineWithConfig is a helper that creates and starts an engine from config YAML.
func startEngineWithConfig(t *testing.T, yamlCfg string) *engine.Engine {
	t.Helper()
	cfgPath := writeYAML(t, yamlCfg)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	eng, err := engine.New(cfg, "")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})
	return eng
}

// TestE2E_MTLS_Handshake verifies mutual TLS: client cert is required and verified.
func TestE2E_MTLS_Handshake(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "mtls-backend", &hits)

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateSelfSignedCert(t, []string{"localhost"})

	// Create a CA for client certs
	caCert2, caKey2, caCertPEM2, _ := generateCACert(t, "OLB Client CA")

	// Generate client cert signed by the client CA
	clientCertPEM2, clientKeyPEM2 := generateClientCert(t, caCert2, caKey2, "test-client-2")

	tmpDir := t.TempDir()
	serverCertFile, serverKeyFile := writeCertFiles(t, tmpDir, "server-cert.pem", "server-key.pem", serverCertPEM, serverKeyPEM)
	writeCertFiles(t, tmpDir, "client-ca.pem", "", caCertPEM2, nil)
	clientCertFile, clientKeyFile := writeCertFiles(t, tmpDir, "client-cert.pem", "client-key.pem", clientCertPEM2, clientKeyPEM2)

	proxyPort := getFreePort(t)

	// Set up TLS manager with server cert
	mgr := olbTLS.NewManager()
	cert, err := mgr.LoadCertificate(serverCertFile, serverKeyFile)
	if err != nil {
		t.Fatalf("Failed to load server cert: %v", err)
	}
	mgr.AddCertificate(cert)
	mgr.SetDefaultCertificate(cert)

	// Build mTLS server config
	mtlsMgr := olbTLS.NewMTLSManager()
	mtlsConfig := &olbTLS.MTLSConfig{
		Enabled:    true,
		ClientAuth: olbTLS.RequireAndVerifyClientCert,
		ClientCAs:  []string{filepath.Join(tmpDir, "client-ca.pem")},
	}
	serverTLSConfig, err := mtlsMgr.BuildServerTLSConfig("test", mtlsConfig, mgr.GetCertificateCallback())
	if err != nil {
		t.Fatalf("Failed to build server TLS config: %v", err)
	}

	// Start a TLS listener with mTLS
	listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), serverTLSConfig)
	if err != nil {
		t.Fatalf("Failed to create TLS listener: %v", err)
	}

	// Simple proxy to backend
	backendHTTPClient := &http.Client{Timeout: 5 * time.Second}
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		url := fmt.Sprintf("http://%s%s", backendAddr, r.URL.Path)
		resp, err := backendHTTPClient.Get(url)
		if err != nil {
			http.Error(w, "Backend error", 502)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Write(body)
	})

	proxyServer := &http.Server{Handler: proxyHandler}
	go proxyServer.Serve(listener)
	t.Cleanup(func() { proxyServer.Close() })

	// Test 1: Connect with valid client cert - should succeed
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(serverCertPEM)
	clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		t.Fatalf("Failed to load client cert: %v", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      caPool,
				Certificates: []tls.Certificate{clientCert},
				ServerName:   "localhost",
			},
		},
	}

	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", proxyPort))
	if err != nil {
		t.Fatalf("mTLS request with valid client cert failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 with valid client cert, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "mtls-backend") {
		t.Errorf("Expected response from mtls-backend, got: %s", body)
	}

	// Test 2: Connect WITHOUT client cert - should fail
	noCertClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	_, err = noCertClient.Get(fmt.Sprintf("https://127.0.0.1:%d/", proxyPort))
	if err == nil {
		t.Error("Expected error when connecting without client cert, but succeeded")
	} else {
		t.Logf("Correctly rejected connection without client cert: %v", err)
	}

	// Test 3: Connect with self-signed (untrusted) client cert - should fail
	untrustedCertPEM, untrustedKeyPEM := generateClientCert(t, nil, nil, "untrusted")
	untrustedCertFile, untrustedKeyFile := writeCertFiles(t, tmpDir, "untrusted-cert.pem", "untrusted-key.pem", untrustedCertPEM, untrustedKeyPEM)
	untrustedCert, err := tls.LoadX509KeyPair(untrustedCertFile, untrustedKeyFile)
	if err != nil {
		t.Fatalf("Failed to load untrusted cert: %v", err)
	}

	untrustedClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates:       []tls.Certificate{untrustedCert},
				InsecureSkipVerify: true,
			},
		},
	}

	_, err = untrustedClient.Get(fmt.Sprintf("https://127.0.0.1:%d/", proxyPort))
	if err == nil {
		t.Error("Expected error when connecting with untrusted client cert, but succeeded")
	} else {
		t.Logf("Correctly rejected untrusted client cert: %v", err)
	}

	t.Logf("mTLS integration test passed: valid cert accepted, missing/untrusted certs rejected")
}

// TestE2E_SNI_MultiCertificate tests SNI-based certificate selection with multiple certs.
func TestE2E_SNI_MultiCertificate(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "sni-backend", &hits)

	// Generate certificates with different DNS names
	certAPEM, keyAPEM := generateSelfSignedCert(t, []string{"a.example.com", "www.a.example.com"})
	certBPEM, keyBPEM := generateSelfSignedCert(t, []string{"b.example.com"})
	wildcardPEM, wildcardKeyPEM := generateSelfSignedCert(t, []string{"*.wildcard.example.com"})

	tmpDir := t.TempDir()
	certAFile, keyAFile := writeCertFiles(t, tmpDir, "cert-a.pem", "key-a.pem", certAPEM, keyAPEM)
	certBFile, keyBFile := writeCertFiles(t, tmpDir, "cert-b.pem", "key-b.pem", certBPEM, keyBPEM)
	writeCertFiles(t, tmpDir, "wildcard.pem", "wildcard.key", wildcardPEM, wildcardKeyPEM)

	// Set up the TLS manager with multiple certs
	mgr := olbTLS.NewManager()

	certA, err := mgr.LoadCertificate(certAFile, keyAFile)
	if err != nil {
		t.Fatalf("Failed to load cert A: %v", err)
	}
	mgr.AddCertificate(certA)

	certB, err := mgr.LoadCertificate(certBFile, keyBFile)
	if err != nil {
		t.Fatalf("Failed to load cert B: %v", err)
	}
	mgr.AddCertificate(certB)

	wildcardCert, err := mgr.LoadCertificate(filepath.Join(tmpDir, "wildcard.pem"), filepath.Join(tmpDir, "wildcard.key"))
	if err != nil {
		t.Fatalf("Failed to load wildcard cert: %v", err)
	}
	mgr.AddCertificate(wildcardCert)

	// Verify manager has the certs
	certs := mgr.ListCertificates()
	if len(certs) != 3 {
		t.Fatalf("Expected 3 certificates, got %d", len(certs))
	}

	// Test SNI matching logic
	testCases := []struct {
		sni      string
		expectCN string
		expectOK bool
	}{
		{"a.example.com", "a.example.com", true},
		{"b.example.com", "b.example.com", true},
		{"www.a.example.com", "a.example.com", true},
		{"sub.wildcard.example.com", "*.wildcard.example.com", true},
		{"other.wildcard.example.com", "*.wildcard.example.com", true},
		{"unknown.example.com", "", false},
	}

	for _, tc := range testCases {
		cert := mgr.GetCertificate(tc.sni)
		if tc.expectOK {
			if cert == nil {
				t.Errorf("SNI %q: expected cert but got nil", tc.sni)
				continue
			}
			found := false
			for _, name := range cert.Names {
				if name == tc.expectCN {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("SNI %q: expected cert with name %q, got names %v", tc.sni, tc.expectCN, cert.Names)
			}
		} else {
			if cert != nil {
				t.Errorf("SNI %q: expected nil but got cert with names %v", tc.sni, cert.Names)
			}
		}
	}

	// Test via real TLS handshake
	proxyPort := getFreePort(t)
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(fmt.Sprintf("http://%s/", backendAddr))
		if err != nil {
			http.Error(w, "error", 502)
			return
		}
		defer resp.Body.Close()
		io.Copy(w, resp.Body)
	})

	tlsConfig := &tls.Config{
		GetCertificate: mgr.GetCertificateCallback(),
		MinVersion:     tls.VersionTLS12,
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), tlsConfig)
	if err != nil {
		t.Fatalf("Failed to create TLS listener: %v", err)
	}

	proxyServer := &http.Server{Handler: proxyHandler}
	go proxyServer.Serve(listener)
	t.Cleanup(func() { proxyServer.Close() })

	// Connect with SNI = a.example.com
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		ServerName:         "a.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Failed to connect with SNI a.example.com: %v", err)
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("No peer certificates")
	}
	if !containsName(state.PeerCertificates[0].DNSNames, "a.example.com") {
		t.Errorf("SNI a.example.com: expected cert with DNS name 'a.example.com', got %v", state.PeerCertificates[0].DNSNames)
	}
	conn.Close()

	// Connect with SNI = b.example.com
	conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		ServerName:         "b.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Failed to connect with SNI b.example.com: %v", err)
	}
	state = conn.ConnectionState()
	if !containsName(state.PeerCertificates[0].DNSNames, "b.example.com") {
		t.Errorf("SNI b.example.com: expected cert with DNS name 'b.example.com', got %v", state.PeerCertificates[0].DNSNames)
	}
	conn.Close()

	// Connect with wildcard SNI
	conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		ServerName:         "test.wildcard.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Failed to connect with SNI test.wildcard.example.com: %v", err)
	}
	state = conn.ConnectionState()
	if !containsName(state.PeerCertificates[0].DNSNames, "*.wildcard.example.com") {
		t.Errorf("SNI test.wildcard.example.com: expected wildcard cert, got %v", state.PeerCertificates[0].DNSNames)
	}
	conn.Close()

	// Connect with unknown SNI - should fail (no default cert)
	conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		ServerName:         "unknown.example.com",
		InsecureSkipVerify: true,
	})
	if err == nil {
		conn.Close()
		t.Error("Expected error for unknown SNI, but connection succeeded")
	} else {
		t.Logf("Correctly rejected unknown SNI: %v", err)
	}

	t.Logf("SNI multi-certificate test passed, hits=%d", hits.Load())
}

// TestE2E_CertificateHotReload tests that certificates can be hot-reloaded without disruption.
func TestE2E_CertificateHotReload(t *testing.T) {
	// Generate two different certificates with unique serials
	cert1PEM, key1PEM := generateSelfSignedCertWithSerial(t, []string{"reload.example.com"}, 1001)
	cert2PEM, key2PEM := generateSelfSignedCertWithSerial(t, []string{"reload.example.com"}, 2002)

	tmpDir := t.TempDir()
	cert1File, key1File := writeCertFiles(t, tmpDir, "cert-v1.pem", "key-v1.pem", cert1PEM, key1PEM)
	cert2File, key2File := writeCertFiles(t, tmpDir, "cert-v2.pem", "key-v2.pem", cert2PEM, key2PEM)

	// Set up manager with first cert
	mgr := olbTLS.NewManager()
	cert1, err := mgr.LoadCertificate(cert1File, key1File)
	if err != nil {
		t.Fatalf("Failed to load cert v1: %v", err)
	}
	mgr.AddCertificate(cert1)
	mgr.SetDefaultCertificate(cert1)

	cert1Serial := getCertSerial(t, cert1PEM)

	// Start TLS server
	proxyPort := getFreePort(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	})

	tlsConfig := &tls.Config{
		GetCertificate: mgr.GetCertificateCallback(),
		MinVersion:     tls.VersionTLS12,
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), tlsConfig)
	if err != nil {
		t.Fatalf("Failed to create TLS listener: %v", err)
	}

	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	// Verify we get cert v1
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		ServerName:         "reload.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Initial TLS connection failed: %v", err)
	}
	initialSerial := conn.ConnectionState().PeerCertificates[0].SerialNumber
	conn.Close()

	if initialSerial.String() != cert1Serial {
		t.Fatalf("Initial cert serial mismatch: got %s, expected %s", initialSerial, cert1Serial)
	}

	// Hot-reload with cert v2
	err = mgr.ReloadCertificates([]olbTLS.CertConfig{
		{CertFile: cert2File, KeyFile: key2File, IsDefault: true},
	})
	if err != nil {
		t.Fatalf("Failed to reload certificates: %v", err)
	}

	// Verify new connections get cert v2
	conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		ServerName:         "reload.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Post-reload TLS connection failed: %v", err)
	}
	reloadedSerial := conn.ConnectionState().PeerCertificates[0].SerialNumber
	conn.Close()

	cert2Serial := getCertSerial(t, cert2PEM)
	if reloadedSerial.String() != cert2Serial {
		t.Errorf("After reload: expected serial %s, got %s", cert2Serial, reloadedSerial)
	}

	if reloadedSerial.String() == initialSerial.String() {
		t.Error("Certificate was not actually reloaded - same serial number")
	}

	t.Logf("Certificate hot-reload test passed: serial changed from %s to %s", initialSerial, reloadedSerial)
}

// TestE2E_TLS13_Enforcement verifies TLS 1.3-only configuration.
func TestE2E_TLS13_Enforcement(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t, []string{"localhost"})

	tmpDir := t.TempDir()
	certFile, keyFile := writeCertFiles(t, tmpDir, "cert.pem", "key.pem", certPEM, keyPEM)

	// Build a TLS config that only allows TLS 1.3
	tlsConfig, err := olbTLS.BuildTLSConfig("1.3", "1.3", nil, true)
	if err != nil {
		t.Fatalf("Failed to build TLS config: %v", err)
	}

	mgr := olbTLS.NewManager()
	cert, err := mgr.LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load cert: %v", err)
	}
	mgr.AddCertificate(cert)
	mgr.SetDefaultCertificate(cert)

	tlsConfig.GetCertificate = mgr.GetCertificateCallback()

	proxyPort := getFreePort(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "TLS 1.3 OK")
	})

	listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), tlsConfig)
	if err != nil {
		t.Fatalf("Failed to create TLS 1.3 listener: %v", err)
	}

	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	// Connect with TLS 1.3 - should succeed
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err != nil {
		t.Fatalf("TLS 1.3 connection failed: %v", err)
	}
	state := conn.ConnectionState()
	if state.Version != tls.VersionTLS13 {
		t.Errorf("Expected TLS 1.3 (0x%04x), got 0x%04x", tls.VersionTLS13, state.Version)
	}
	conn.Close()

	// Connect with max TLS 1.2 - should fail
	conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), &tls.Config{
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err == nil {
		state = conn.ConnectionState()
		conn.Close()
		t.Errorf("TLS 1.2 connection should have been rejected, but got version 0x%04x", state.Version)
	} else {
		t.Logf("TLS 1.2 correctly rejected: %v", err)
	}

	t.Logf("TLS 1.3 enforcement test passed")
}

// TestE2E_MTLS_PolicyVariations tests different client auth policies.
func TestE2E_MTLS_PolicyVariations(t *testing.T) {
	// Generate CA and certs
	caCert, caKey, caCertPEM, _ := generateCACert(t, "Policy Test CA")
	serverCertPEM, serverKeyPEM := generateSelfSignedCert(t, []string{"localhost"})
	validClientPEM, validClientKeyPEM := generateClientCert(t, caCert, caKey, "valid-client")

	tmpDir := t.TempDir()
	serverCertFile, serverKeyFile := writeCertFiles(t, tmpDir, "server-cert.pem", "server-key.pem", serverCertPEM, serverKeyPEM)
	writeCertFiles(t, tmpDir, "ca.pem", "", caCertPEM, nil)
	validClientCertFile, validClientKeyFile := writeCertFiles(t, tmpDir, "client-cert.pem", "client-key.pem", validClientPEM, validClientKeyPEM)

	tests := []struct {
		name     string
		policy   olbTLS.ClientAuthPolicy
		withCert bool
		expectOK bool
	}{
		{"RequestCert_no_cert", olbTLS.RequestClientCert, false, true},
		{"RequestCert_with_cert", olbTLS.RequestClientCert, true, true},
		{"RequireAny_no_cert", olbTLS.RequireAnyClientCert, false, false},
		{"RequireAny_with_cert", olbTLS.RequireAnyClientCert, true, true},
		{"VerifyIfGiven_no_cert", olbTLS.VerifyClientCertIfGiven, false, true},
		{"VerifyIfGiven_valid_cert", olbTLS.VerifyClientCertIfGiven, true, true},
		{"RequireAndVerify_no_cert", olbTLS.RequireAndVerifyClientCert, false, false},
		{"RequireAndVerify_valid_cert", olbTLS.RequireAndVerifyClientCert, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr := olbTLS.NewManager()
			cert, err := mgr.LoadCertificate(serverCertFile, serverKeyFile)
			if err != nil {
				t.Fatalf("Load cert: %v", err)
			}
			mgr.AddCertificate(cert)
			mgr.SetDefaultCertificate(cert)

			mtlsMgr := olbTLS.NewMTLSManager()
			clientCAs := []string{}
			if tc.policy.IsClientAuthVerified() || tc.policy == olbTLS.RequireAnyClientCert {
				clientCAs = []string{filepath.Join(tmpDir, "ca.pem")}
			}

			mtlsConfig := &olbTLS.MTLSConfig{
				Enabled:    true,
				ClientAuth: tc.policy,
				ClientCAs:  clientCAs,
			}

			serverTLS, err := mtlsMgr.BuildServerTLSConfig(tc.name, mtlsConfig, mgr.GetCertificateCallback())
			if err != nil {
				t.Fatalf("BuildServerTLSConfig: %v", err)
			}

			port := getFreePort(t)
			listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port), serverTLS)
			if err != nil {
				t.Fatalf("Listen: %v", err)
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "OK")
			})
			server := &http.Server{Handler: handler}
			go server.Serve(listener)
			t.Cleanup(func() { server.Close() })

			// Build client config
			clientTLSConfig := &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         "localhost",
			}
			if tc.withCert {
				clientCert, err := tls.LoadX509KeyPair(validClientCertFile, validClientKeyFile)
				if err != nil {
					t.Fatalf("Load client cert: %v", err)
				}
				clientTLSConfig.Certificates = []tls.Certificate{clientCert}
			}

			client := &http.Client{
				Timeout:   3 * time.Second,
				Transport: &http.Transport{TLSClientConfig: clientTLSConfig},
			}

			resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
			if tc.expectOK {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				} else {
					resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Errorf("Expected 200, got %d", resp.StatusCode)
					}
				}
			} else {
				if err == nil {
					resp.Body.Close()
					t.Errorf("Expected rejection but request succeeded with status %d", resp.StatusCode)
				}
			}
		})
	}
}

// TestE2E_TLS_CipherSuiteRestriction tests that restricted cipher suites are enforced.
func TestE2E_TLS_CipherSuiteRestriction(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t, []string{"localhost"})

	tmpDir := t.TempDir()
	certFile, keyFile := writeCertFiles(t, tmpDir, "cert.pem", "key.pem", certPEM, keyPEM)

	// Build TLS config with specific cipher suites (TLS 1.2 for cipher suite control)
	tlsConfig, err := olbTLS.BuildTLSConfig("1.2", "1.2", []string{
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
	}, true)
	if err != nil {
		t.Fatalf("Failed to build TLS config: %v", err)
	}

	mgr := olbTLS.NewManager()
	cert, err := mgr.LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load cert: %v", err)
	}
	mgr.AddCertificate(cert)
	mgr.SetDefaultCertificate(cert)

	tlsConfig.GetCertificate = mgr.GetCertificateCallback()

	port := getFreePort(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	})

	listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port), tlsConfig)
	if err != nil {
		t.Fatalf("Failed to create TLS listener: %v", err)
	}

	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	// Connect and verify the negotiated cipher suite
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err != nil {
		t.Fatalf("TLS connection failed: %v", err)
	}
	state := conn.ConnectionState()
	conn.Close()

	cipherName := tls.CipherSuiteName(state.CipherSuite)
	allowed := map[string]bool{
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": true,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": true,
	}
	if !allowed[cipherName] {
		t.Errorf("Negotiated cipher suite %s is not in the allowed list", cipherName)
	}

	t.Logf("Cipher suite restriction test passed: negotiated %s", cipherName)
}

// TestE2E_TLS_EngineIntegration tests TLS termination through the full engine stack.
func TestE2E_TLS_EngineIntegration(t *testing.T) {
	var hits atomic.Int64
	backendAddr := startBackend(t, "engine-tls-backend", &hits)

	certPEM, keyPEM := generateSelfSignedCert(t, []string{"localhost"})

	tmpDir := t.TempDir()
	certFile, keyFile := writeCertFiles(t, tmpDir, "cert.pem", "key.pem", certPEM, keyPEM)

	proxyPort := getFreePort(t)
	adminPort := getFreePort(t)

	yamlCfg := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
tls:
  cert_file: "%s"
  key_file: "%s"
listeners:
  - name: https
    address: "127.0.0.1:%d"
    protocol: http
    tls:
      enabled: true
    routes:
      - path: /
        pool: tls-engine-pool
pools:
  - name: tls-engine-pool
    algorithm: round_robin
    backends:
      - address: "%s"
    health_check:
      type: http
      interval: 1s
      timeout: 1s
      path: /health
`, adminPort, toForwardSlash(certFile), toForwardSlash(keyFile), proxyPort, backendAddr)

	startEngineWithConfig(t, yamlCfg)

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	waitForReady(t, proxyAddr, 5*time.Second)
	time.Sleep(2 * time.Second)

	// Make HTTPS request
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         "localhost",
			},
		},
	}

	resp, err := client.Get(fmt.Sprintf("https://%s/", proxyAddr))
	if err != nil {
		t.Fatalf("HTTPS request through engine failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "engine-tls-backend") {
		t.Errorf("Expected response from engine-tls-backend, got: %s", body)
	}

	// Verify TLS version
	conn, err := tls.Dial("tcp", proxyAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err != nil {
		t.Fatalf("TLS dial for version check failed: %v", err)
	}
	state := conn.ConnectionState()
	conn.Close()

	if state.Version < tls.VersionTLS12 {
		t.Errorf("Expected TLS 1.2+, got version 0x%04x", state.Version)
	}

	t.Logf("Engine TLS integration test passed: TLS version=0x%04x, hits=%d", state.Version, hits.Load())
}

// TestE2E_TLS_CertificateManagerConcurrency tests concurrent access to the TLS manager.
func TestE2E_TLS_CertificateManagerConcurrency(t *testing.T) {
	mgr := olbTLS.NewManager()

	// Generate multiple certs
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("concurrent-%d.example.com", i)
		certPEM, keyPEM := generateSelfSignedCert(t, []string{name})

		tmpDir := t.TempDir()
		certFile, keyFile := writeCertFiles(t, tmpDir, "cert.pem", "key.pem", certPEM, keyPEM)

		cert, err := mgr.LoadCertificate(certFile, keyFile)
		if err != nil {
			t.Fatalf("Failed to load cert %s: %v", name, err)
		}
		mgr.AddCertificate(cert)
	}

	// Concurrent reads
	done := make(chan bool, 20)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			name := fmt.Sprintf("concurrent-%d.example.com", idx%5)
			cert := mgr.GetCertificate(name)
			if cert == nil {
				t.Errorf("Concurrent read %d: expected cert for %s, got nil", idx, name)
			}
			done <- true
		}(i)
	}

	// Concurrent list operations
	for i := 0; i < 10; i++ {
		go func() {
			certs := mgr.ListCertificates()
			if len(certs) != 5 {
				t.Errorf("Expected 5 certs, got %d", len(certs))
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	t.Log("Concurrent certificate manager test passed")
}

// TestE2E_TLS_DefaultCertificate tests default certificate fallback behavior.
func TestE2E_TLS_DefaultCertificate(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t, []string{"default.example.com"})

	tmpDir := t.TempDir()
	certFile, keyFile := writeCertFiles(t, tmpDir, "cert.pem", "key.pem", certPEM, keyPEM)

	mgr := olbTLS.NewManager()
	cert, err := mgr.LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load cert: %v", err)
	}
	mgr.AddCertificate(cert)
	mgr.SetDefaultCertificate(cert)

	// With default set, unknown SNI returns the default cert
	result := mgr.GetCertificate("unknown.example.com")
	if result == nil {
		t.Fatal("Expected default cert for unknown SNI, got nil")
	}
	if result.Names[0] != "default.example.com" {
		t.Errorf("Expected default cert name 'default.example.com', got %v", result.Names)
	}

	// Start server with default cert
	port := getFreePort(t)
	tlsConfig := &tls.Config{
		GetCertificate: mgr.GetCertificateCallback(),
		MinVersion:     tls.VersionTLS12,
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port), tlsConfig)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	})
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	// Connect with no SNI at all - should get default cert
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Connection without SNI failed: %v", err)
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("No peer certificates")
	}
	if len(state.PeerCertificates[0].DNSNames) == 0 || state.PeerCertificates[0].DNSNames[0] != "default.example.com" {
		t.Errorf("Expected default cert, got DNS names: %v", state.PeerCertificates[0].DNSNames)
	}
	conn.Close()

	// Connect with random SNI - should also get default cert
	conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{
		ServerName:         "random.test.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Connection with random SNI failed: %v", err)
	}
	state = conn.ConnectionState()
	if len(state.PeerCertificates) == 0 || len(state.PeerCertificates[0].DNSNames) == 0 {
		t.Fatal("No peer certificates")
	}
	conn.Close()

	t.Log("Default certificate fallback test passed")
}

// TestE2E_TLS_ClientCertInfo tests extracting info from client certificates.
func TestE2E_TLS_ClientCertInfo(t *testing.T) {
	caCert, caKey, _, _ := generateCACert(t, "Cert Info CA")
	clientCertPEM, _ := generateClientCert(t, caCert, caKey, "info-client")

	// Parse the client cert to verify info extraction
	block, _ := pem.Decode(clientCertPEM)
	if block == nil {
		t.Fatal("Failed to decode client cert PEM")
	}
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse client cert: %v", err)
	}

	info := olbTLS.GetClientCertInfo(x509Cert)
	if info == nil {
		t.Fatal("GetClientCertInfo returned nil")
	}

	if info["subject"] == nil {
		t.Error("Expected subject in cert info")
	}
	if info["serial"] == nil {
		t.Error("Expected serial in cert info")
	}
	if info["issuer"] == nil {
		t.Error("Expected issuer in cert info")
	}

	// Test nil cert
	nilInfo := olbTLS.GetClientCertInfo(nil)
	if nilInfo != nil {
		t.Errorf("Expected nil for nil cert, got %v", nilInfo)
	}

	t.Log("Client cert info extraction test passed")
}

// TestE2E_TLS_VerifyClientCert tests certificate verification against a CA pool.
func TestE2E_TLS_VerifyClientCert(t *testing.T) {
	caCert, caKey, _, _ := generateCACert(t, "Verify CA")

	// Generate a valid client cert
	validCertPEM, _ := generateClientCert(t, caCert, caKey, "valid-client")
	validBlock, _ := pem.Decode(validCertPEM)
	validCert, _ := x509.ParseCertificate(validBlock.Bytes)

	// Build CA pool
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	// Test valid cert verification
	err := olbTLS.VerifyClientCert(validCert, caPool, 0)
	if err != nil {
		t.Errorf("Valid client cert verification failed: %v", err)
	}

	// Test nil cert
	err = olbTLS.VerifyClientCert(nil, caPool, 0)
	if err == nil {
		t.Error("Expected error for nil cert")
	}

	// Test cert with depth limit
	err = olbTLS.VerifyClientCert(validCert, caPool, 1)
	if err != nil {
		t.Logf("Depth limit check: %v (may be expected)", err)
	}

	t.Log("Client cert verification test passed")
}

// containsName checks if a slice contains a specific string.
func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

// generateSelfSignedCertWithSerial generates a self-signed cert with a specific serial number.
func generateSelfSignedCertWithSerial(t *testing.T, dnsNames []string, serial int64) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			Organization: []string{"OLB Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("Failed to marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM
}

// getCertSerial extracts the serial number from a PEM certificate.
func getCertSerial(t *testing.T, certPEM []byte) string {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("Failed to decode PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse cert: %v", err)
	}
	return cert.SerialNumber.String()
}

// toForwardSlash converts backslashes to forward slashes for YAML config paths.
func toForwardSlash(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}
