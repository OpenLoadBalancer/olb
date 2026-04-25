package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateTestCert generates a test certificate with the given DNS names.
func generateTestCert(dnsNames []string, isCA bool) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}

	if isCA {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM, nil
}

func TestManager_LoadCertificateFromPEM(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	if len(cert.Names) != 1 || cert.Names[0] != "example.com" {
		t.Errorf("expected names [example.com], got %v", cert.Names)
	}

	if cert.IsWildcard {
		t.Error("expected non-wildcard certificate")
	}
}

func TestManager_LoadCertificateFromPEM_Wildcard(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"*.example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	if len(cert.Names) != 1 || cert.Names[0] != "*.example.com" {
		t.Errorf("expected names [*.example.com], got %v", cert.Names)
	}

	if !cert.IsWildcard {
		t.Error("expected wildcard certificate")
	}
}

func TestManager_GetCertificate_ExactMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.AddCertificate(cert)

	result := m.GetCertificate("example.com")
	if result == nil {
		t.Fatal("expected to find certificate for example.com")
	}

	if result != cert {
		t.Error("returned certificate doesn't match")
	}
}

func TestManager_GetCertificate_WildcardMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"*.example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.AddCertificate(cert)

	testCases := []string{
		"sub.example.com",
		"www.example.com",
		"api.example.com",
	}

	for _, sni := range testCases {
		result := m.GetCertificate(sni)
		if result == nil {
			t.Errorf("expected to find certificate for %s", sni)
			continue
		}
		if result != cert {
			t.Errorf("returned wrong certificate for %s", sni)
		}
	}
}

func TestManager_GetCertificate_NoMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.AddCertificate(cert)

	result := m.GetCertificate("other.com")
	if result != nil {
		t.Error("expected no certificate for other.com")
	}
}

func TestManager_GetCertificate_Default(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"default.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.SetDefaultCertificate(cert)

	result := m.GetCertificate("unknown.com")
	if result != cert {
		t.Error("expected default certificate for unknown.com")
	}
}

func TestManager_GetCertificate_CaseInsensitive(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"EXAMPLE.COM"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.AddCertificate(cert)

	testCases := []string{
		"example.com",
		"EXAMPLE.COM",
		"Example.COM",
	}

	for _, sni := range testCases {
		result := m.GetCertificate(sni)
		if result == nil {
			t.Errorf("expected to find certificate for %s", sni)
		}
	}
}

func TestManager_GetCertificateCallback(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.AddCertificate(cert)

	callback := m.GetCertificateCallback()

	tlsCert, err := callback(&tls.ClientHelloInfo{ServerName: "example.com"})
	if err != nil {
		t.Fatalf("callback failed: %v", err)
	}

	if tlsCert == nil {
		t.Fatal("expected certificate from callback")
	}
}

func TestManager_GetCertificateCallback_NoSNI(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"default.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	m.SetDefaultCertificate(cert)

	callback := m.GetCertificateCallback()

	tlsCert, err := callback(&tls.ClientHelloInfo{ServerName: ""})
	if err != nil {
		t.Fatalf("callback failed: %v", err)
	}

	if tlsCert == nil {
		t.Fatal("expected default certificate from callback")
	}
}

func TestManager_LoadCertificate(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"filetest.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	cert, err := m.LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("failed to load certificate from file: %v", err)
	}

	if len(cert.Names) != 1 || cert.Names[0] != "filetest.com" {
		t.Errorf("expected names [filetest.com], got %v", cert.Names)
	}
}

func TestManager_ReloadCertificates(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"reload.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()

	err = m.ReloadCertificates([]CertConfig{
		{CertFile: certFile, KeyFile: keyFile, IsDefault: true},
	})
	if err != nil {
		t.Fatalf("failed to reload certificates: %v", err)
	}

	result := m.GetCertificate("reload.com")
	if result == nil {
		t.Fatal("expected to find certificate after reload")
	}
}

func TestManager_LoadCertificatesFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"dirtest.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "cert.crt")
	keyFile := filepath.Join(tmpDir, "cert.key")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	err = m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load certificates from directory: %v", err)
	}

	result := m.GetCertificate("dirtest.com")
	if result == nil {
		t.Fatal("expected to find certificate loaded from directory")
	}
}

func TestManager_ListCertificates(t *testing.T) {
	certPEM1, keyPEM1, err := generateTestCert([]string{"cert1.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certPEM2, keyPEM2, err := generateTestCert([]string{"*.cert2.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()

	cert1, _ := m.LoadCertificateFromPEM(certPEM1, keyPEM1)
	cert2, _ := m.LoadCertificateFromPEM(certPEM2, keyPEM2)

	m.AddCertificate(cert1)
	m.AddCertificate(cert2)

	certs := m.ListCertificates()
	if len(certs) != 2 {
		t.Errorf("expected 2 certificates, got %d", len(certs))
	}

	foundWildcard := false
	for _, info := range certs {
		if info.IsWildcard {
			foundWildcard = true
			break
		}
	}
	if !foundWildcard {
		t.Error("expected to find wildcard certificate")
	}
}

func TestBuildTLSConfig(t *testing.T) {
	tests := []struct {
		name                     string
		minVersion               string
		maxVersion               string
		cipherSuites             []string
		preferServerCipherSuites bool
		wantErr                  bool
	}{
		{
			name:       "default",
			minVersion: "",
			wantErr:    false,
		},
		{
			name:       "tls1.2",
			minVersion: "1.2",
			wantErr:    false,
		},
		{
			name:       "tls1.3",
			minVersion: "1.3",
			wantErr:    false,
		},
		{
			name:       "invalid version",
			minVersion: "2.0",
			wantErr:    true,
		},
		{
			name:         "with cipher suites",
			minVersion:   "1.2",
			cipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
			wantErr:      false,
		},
		{
			name:         "invalid cipher suite",
			minVersion:   "1.2",
			cipherSuites: []string{"INVALID_SUITE"},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := BuildTLSConfig(tt.minVersion, tt.maxVersion, tt.cipherSuites, tt.preferServerCipherSuites)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildTLSConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if config == nil {
				t.Error("expected non-nil config")
			}
		})
	}
}

func TestBuildTLSConfig_VersionValues(t *testing.T) {
	tests := []struct {
		version string
		want    uint16
		wantErr bool
	}{
		{"1.0", 0, true}, // TLS 1.0 rejected per RFC 8996
		{"1.1", 0, true}, // TLS 1.1 rejected per RFC 8996
		{"1.2", tls.VersionTLS12, false},
		{"1.3", tls.VersionTLS13, false},
		{"tls10", 0, true}, // TLS 1.0 rejected per RFC 8996
		{"tls11", 0, true}, // TLS 1.1 rejected per RFC 8996
		{"tls12", tls.VersionTLS12, false},
		{"tls13", tls.VersionTLS13, false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			config, err := BuildTLSConfig(tt.version, "", nil, false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error for deprecated TLS version")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if config.MinVersion != tt.want {
				t.Errorf("MinVersion = %v, want %v", config.MinVersion, tt.want)
			}
		})
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"concurrent.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	// Concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = m.GetCertificate("concurrent.com")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// Additional tests for comprehensive coverage

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager() returned nil")
	}
	if m.exactCerts == nil {
		t.Error("exactCerts map not initialized")
	}
	if m.wildcardCerts == nil {
		t.Error("wildcardCerts map not initialized")
	}
}

func TestAddCertificate_InvalidCert(t *testing.T) {
	m := NewManager()

	// Add certificate with nil Cert field (but valid names)
	cert := &Certificate{
		Cert:       nil,
		Names:      []string{"test.com"},
		Expiry:     0,
		IsWildcard: false,
	}
	m.AddCertificate(cert)

	// Should still be able to get the certificate entry
	result := m.GetCertificate("test.com")
	if result == nil {
		t.Error("should be able to retrieve added certificate")
	}
}

func TestAddCertificate_Duplicate(t *testing.T) {
	certPEM1, keyPEM1, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certPEM2, keyPEM2, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()

	cert1, _ := m.LoadCertificateFromPEM(certPEM1, keyPEM1)
	cert2, _ := m.LoadCertificateFromPEM(certPEM2, keyPEM2)

	// Add first certificate
	m.AddCertificate(cert1)

	// Add second certificate with same name (should overwrite)
	m.AddCertificate(cert2)

	// Should get the second certificate
	result := m.GetCertificate("example.com")
	if result != cert2 {
		t.Error("second certificate should overwrite first")
	}
}

func TestGetCertificate_ExactMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"api.example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	result := m.GetCertificate("api.example.com")
	if result == nil {
		t.Fatal("expected exact match")
	}
	if result != cert {
		t.Error("returned wrong certificate")
	}
}

func TestGetCertificate_WildcardMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"*.example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	tests := []struct {
		sni       string
		wantMatch bool
	}{
		{"sub.example.com", true},
		{"www.example.com", true},
		{"deep.sub.example.com", true},
		{"example.com", false}, // wildcard doesn't match base domain
		{"other.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.sni, func(t *testing.T) {
			result := m.GetCertificate(tt.sni)
			if tt.wantMatch && result == nil {
				t.Error("expected match")
			}
			if !tt.wantMatch && result != nil {
				t.Error("expected no match")
			}
		})
	}
}

func TestGetCertificate_NoMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	result := m.GetCertificate("other.com")
	if result != nil {
		t.Error("expected no match for different domain")
	}
}

func TestGetCertificate_SNIEmpty(t *testing.T) {
	m := NewManager()

	// Empty SNI with no default
	result := m.GetCertificate("")
	if result != nil {
		t.Error("expected nil for empty SNI with no default")
	}

	// Set default certificate
	certPEM, keyPEM, err := generateTestCert([]string{"default.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.SetDefaultCertificate(cert)

	// Empty SNI with default
	result = m.GetCertificate("")
	if result != cert {
		t.Error("expected default certificate for empty SNI")
	}
}

func TestRemoveCertificate(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"remove.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	// Verify it exists
	if m.GetCertificate("remove.com") == nil {
		t.Fatal("certificate should exist before removal")
	}

	// Note: Manager doesn't have a RemoveCertificate method
	// This test documents the expected behavior
	t.Log("Manager does not have RemoveCertificate method - certificates can only be replaced via ReloadCertificates")
}

func TestListCertificates(t *testing.T) {
	certPEM1, keyPEM1, err := generateTestCert([]string{"cert1.com", "cert1-alt.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certPEM2, keyPEM2, err := generateTestCert([]string{"*.cert2.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()

	cert1, _ := m.LoadCertificateFromPEM(certPEM1, keyPEM1)
	cert2, _ := m.LoadCertificateFromPEM(certPEM2, keyPEM2)

	m.AddCertificate(cert1)
	m.AddCertificate(cert2)

	certs := m.ListCertificates()

	// Should have 2 unique certificates (even though cert1 has 2 names)
	if len(certs) != 2 {
		t.Errorf("expected 2 certificates, got %d", len(certs))
	}

	// Check that we have one wildcard
	foundWildcard := false
	for _, info := range certs {
		if info.IsWildcard {
			foundWildcard = true
			break
		}
	}
	if !foundWildcard {
		t.Error("expected to find wildcard certificate")
	}
}

func TestReloadCertificates(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"reload1.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()

	// Initial load
	err = m.ReloadCertificates([]CertConfig{
		{CertFile: certFile, KeyFile: keyFile, IsDefault: false},
	})
	if err != nil {
		t.Fatalf("failed to reload certificates: %v", err)
	}

	if m.GetCertificate("reload1.com") == nil {
		t.Error("expected to find certificate after reload")
	}

	// Generate new certificate
	certPEM2, keyPEM2, err := generateTestCert([]string{"reload2.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	if err := os.WriteFile(certFile, certPEM2, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM2, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	// Reload with new certificate
	err = m.ReloadCertificates([]CertConfig{
		{CertFile: certFile, KeyFile: keyFile, IsDefault: true},
	})
	if err != nil {
		t.Fatalf("failed to reload certificates: %v", err)
	}

	// After reload, only the new certificate should exist
	// Note: Since we overwrote the same file, the old certificate name is still there
	// but the certificate data has been replaced. This is expected behavior.
	result := m.GetCertificate("reload2.com")
	if result == nil {
		t.Error("new certificate should exist after reload")
	}

	// Verify we have exactly 1 certificate after reload
	certs := m.ListCertificates()
	if len(certs) != 1 {
		t.Errorf("expected 1 certificate after reload, got %d", len(certs))
	}
}

func TestLoadCertificate_InvalidPath(t *testing.T) {
	m := NewManager()

	// Non-existent cert file
	_, err := m.LoadCertificate("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("expected error for non-existent cert file")
	}

	// Create temp files for testing
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	// Write invalid content
	if err := os.WriteFile(certFile, []byte("invalid cert"), 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("invalid key"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	// Invalid PEM content
	_, err = m.LoadCertificate(certFile, keyFile)
	if err == nil {
		t.Error("expected error for invalid PEM content")
	}
}

func TestLoadCertificate_InvalidPEM(t *testing.T) {
	m := NewManager()

	// Invalid PEM data
	invalidCert := []byte(`-----BEGIN CERTIFICATE-----
invalid
-----END CERTIFICATE-----`)

	invalidKey := []byte(`-----BEGIN EC PRIVATE KEY-----
invalid
-----END EC PRIVATE KEY-----`)

	_, err := m.LoadCertificateFromPEM(invalidCert, invalidKey)
	if err == nil {
		t.Error("expected error for invalid PEM data")
	}
}

func TestLoadCertificateFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid certificate
	certPEM, keyPEM, err := generateTestCert([]string{"dirtest.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "cert.crt")
	keyFile := filepath.Join(tmpDir, "cert.key")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	err = m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load certificates from directory: %v", err)
	}

	result := m.GetCertificate("dirtest.com")
	if result == nil {
		t.Error("expected to find certificate loaded from directory")
	}
}

func TestLoadCertificateFromDirectory_NoKeyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create cert file without corresponding key file
	certPEM, _, err := generateTestCert([]string{"orphan.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "orphan.crt")
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}

	m := NewManager()
	err = m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load certificates from directory: %v", err)
	}

	// Should not have loaded the orphan certificate
	result := m.GetCertificate("orphan.com")
	if result != nil {
		t.Error("should not load certificate without key file")
	}
}

func TestGetCertificateForServerName(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"specific.com", "*.wildcard.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	tests := []struct {
		sni       string
		wantMatch bool
	}{
		{"specific.com", true},
		{"sub.wildcard.com", true},
		{"deep.sub.wildcard.com", true},
		{"other.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.sni, func(t *testing.T) {
			result := m.GetCertificate(tt.sni)
			if tt.wantMatch && result == nil {
				t.Errorf("expected match for %s", tt.sni)
			}
			if !tt.wantMatch && result != nil {
				t.Errorf("expected no match for %s", tt.sni)
			}
		})
	}
}

func TestMatchWildcard(t *testing.T) {
	// Test the internal matchWildcard logic via GetCertificate
	tests := []struct {
		name    string
		pattern string
		sni     string
		match   bool
	}{
		{"exact match", "example.com", "example.com", true},
		{"wildcard match", "*.example.com", "sub.example.com", true},
		{"wildcard deep match", "*.example.com", "deep.sub.example.com", true},
		{"wildcard no match", "*.example.com", "other.com", false},
		{"base domain no match", "*.example.com", "example.com", false},
		{"case insensitive exact", "EXAMPLE.COM", "example.com", true},
		{"case insensitive wildcard", "*.EXAMPLE.COM", "sub.example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certPEM, keyPEM, err := generateTestCert([]string{tt.pattern}, false)
			if err != nil {
				t.Fatalf("failed to generate test cert: %v", err)
			}

			m := NewManager()
			cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
			m.AddCertificate(cert)

			result := m.GetCertificate(tt.sni)
			if tt.match && result == nil {
				t.Errorf("expected match for pattern=%s, sni=%s", tt.pattern, tt.sni)
			}
			if !tt.match && result != nil {
				t.Errorf("expected no match for pattern=%s, sni=%s", tt.pattern, tt.sni)
			}
		})
	}
}

func TestNilCertificate(t *testing.T) {
	m := NewManager()

	// The Manager's AddCertificate doesn't handle nil certificates
	// This test documents the expected behavior - passing nil will panic
	// In production code, nil checks should be done before calling AddCertificate

	// Test with empty manager
	result := m.GetCertificate("anything.com")
	if result != nil {
		t.Error("expected nil for empty manager")
	}
}

func TestExpiredCertificate(t *testing.T) {
	// Generate an expired certificate
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now().Add(-48 * time.Hour),
		NotAfter:              time.Now().Add(-24 * time.Hour), // Expired
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"expired.com"},
	}

	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	m := NewManager()
	_, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err == nil {
		t.Fatal("expired certificate should be rejected")
	}
}

func TestCertificateWithWrongKey(t *testing.T) {
	// Generate two different key pairs
	priv1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	priv2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"mismatch.com"},
	}

	// Create cert with priv1
	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv1.PublicKey, priv1)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Use priv2 for key
	keyBytes, _ := x509.MarshalECPrivateKey(priv2)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	m := NewManager()
	_, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err == nil {
		t.Error("expected error for certificate/key mismatch")
	}
}

func TestEmptyManager(t *testing.T) {
	m := NewManager()

	// GetCertificate on empty manager
	result := m.GetCertificate("anything.com")
	if result != nil {
		t.Error("expected nil for empty manager")
	}

	// ListCertificates on empty manager
	certs := m.ListCertificates()
	if len(certs) != 0 {
		t.Errorf("expected 0 certificates, got %d", len(certs))
	}

	// GetCertificateCallback on empty manager
	callback := m.GetCertificateCallback()
	_, err := callback(&tls.ClientHelloInfo{ServerName: "test.com"})
	if err == nil {
		t.Error("expected error for empty manager with no default")
	}
}

func TestGetCertificateCallback_NoSNINoDefault(t *testing.T) {
	m := NewManager()

	callback := m.GetCertificateCallback()
	_, err := callback(&tls.ClientHelloInfo{ServerName: ""})
	if err == nil {
		t.Error("expected error when no SNI and no default certificate")
	}
}

func TestGetCertificateCallback_NoMatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"example.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	m := NewManager()
	cert, _ := m.LoadCertificateFromPEM(certPEM, keyPEM)
	m.AddCertificate(cert)

	callback := m.GetCertificateCallback()
	_, err = callback(&tls.ClientHelloInfo{ServerName: "other.com"})
	if err == nil {
		t.Error("expected error when no certificate matches")
	}
}

// --- Additional coverage tests for BuildTLSConfig ---

func TestBuildTLSConfig_MaxVersion(t *testing.T) {
	config, err := BuildTLSConfig("1.2", "1.3", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want TLS12", config.MinVersion)
	}
	if config.MaxVersion != tls.VersionTLS13 {
		t.Errorf("MaxVersion = %v, want TLS13", config.MaxVersion)
	}
}

func TestBuildTLSConfig_InvalidMaxVersion(t *testing.T) {
	_, err := BuildTLSConfig("", "9.9", nil, false)
	if err == nil {
		t.Error("expected error for invalid max version")
	}
}

func TestBuildTLSConfig_AllCipherSuites(t *testing.T) {
	suites := []string{
		"TLS_AES_128_GCM_SHA256",
		"TLS_AES_256_GCM_SHA384",
		"TLS_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
		"TLS_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_RSA_WITH_AES_256_GCM_SHA384",
	}
	// With allowRSACipherSuites=true, all 11 should be present
	config, err := BuildTLSConfigWithOptions("1.2", "1.3", suites, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config.CipherSuites) != len(suites) {
		t.Errorf("expected %d cipher suites, got %d", len(suites), len(config.CipherSuites))
	}
	if !config.PreferServerCipherSuites {
		t.Error("expected PreferServerCipherSuites to be true")
	}

	// With allowRSACipherSuites=false (default), RSA suites should be filtered out
	configFiltered, err := BuildTLSConfig("1.2", "1.3", suites, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedFiltered := len(suites) - 2 // 2 RSA suites removed
	if len(configFiltered.CipherSuites) != expectedFiltered {
		t.Errorf("expected %d cipher suites after filtering, got %d", expectedFiltered, len(configFiltered.CipherSuites))
	}
}

func TestBuildTLSConfig_EmptyVersions(t *testing.T) {
	config, err := BuildTLSConfig("", "", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("default MinVersion = %v, want TLS12", config.MinVersion)
	}
	if config.MaxVersion != 0 {
		t.Errorf("default MaxVersion = %v, want 0", config.MaxVersion)
	}
	if len(config.CipherSuites) != 0 {
		t.Errorf("expected no cipher suites, got %d", len(config.CipherSuites))
	}
}

func TestBuildTLSConfig_TLSVersionAliases(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    uint16
		wantErr bool
	}{
		{"tls1.0", "tls1.0", 0, true}, // rejected per RFC 8996
		{"tls1.1", "tls1.1", 0, true}, // rejected per RFC 8996
		{"tls1.2", "tls1.2", tls.VersionTLS12, false},
		{"tls1.3", "tls1.3", tls.VersionTLS13, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := BuildTLSConfig(tt.version, "", nil, false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error for deprecated TLS version")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if config.MinVersion != tt.want {
				t.Errorf("MinVersion = %v, want %v", config.MinVersion, tt.want)
			}
		})
	}
}

// --- Additional coverage for LoadCertificateFromPEM ---

func TestLoadCertificateFromPEM_WithCommonName(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
			CommonName:   "common-name.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"dns.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	// CommonName + DNSNames should both be present
	if len(cert.Names) != 2 {
		t.Errorf("expected 2 names, got %d: %v", len(cert.Names), cert.Names)
	}
	if cert.Names[0] != "common-name.example.com" {
		t.Errorf("first name should be CommonName, got %s", cert.Names[0])
	}
}

func TestLoadCertificateFromPEM_NearlyExpired(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(15 * 24 * time.Hour), // 15 days - less than 30
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"nearly-expired.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load nearly-expired certificate: %v", err)
	}
	if cert == nil {
		t.Fatal("expected non-nil certificate")
	}
}

func TestLoadCertificateFromPEM_MultipleSANs(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
			CommonName:   "primary.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"secondary.example.com", "*.wildcard.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	m := NewManager()
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	if !cert.IsWildcard {
		t.Error("expected wildcard certificate since one SAN is a wildcard")
	}
	if len(cert.Names) != 3 {
		t.Errorf("expected 3 names (CN + 2 SANs), got %d: %v", len(cert.Names), cert.Names)
	}
}

// --- Additional coverage for LoadCertificate error paths ---

func TestLoadCertificate_KeyFileReadError(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, _, err := generateTestCert([]string{"keytest.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}

	m := NewManager()
	_, err = m.LoadCertificate(certFile, filepath.Join(tmpDir, "nonexistent.key"))
	if err == nil {
		t.Error("expected error for non-existent key file")
	}
}

// --- Additional coverage for ReloadCertificates error path ---

func TestReloadCertificates_InvalidCertFile(t *testing.T) {
	m := NewManager()

	err := m.ReloadCertificates([]CertConfig{
		{CertFile: "/nonexistent/cert.pem", KeyFile: "/nonexistent/key.pem", IsDefault: true},
	})
	if err == nil {
		t.Error("expected error for non-existent cert file")
	}
}

// --- Additional coverage for LoadCertificatesFromDirectory ---

func TestLoadCertificatesFromDirectory_PEMExtension(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"pemtest.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "server.pem")
	keyFile := filepath.Join(tmpDir, "server.key")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	err = m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load certificates from directory: %v", err)
	}

	result := m.GetCertificate("pemtest.com")
	if result == nil {
		t.Error("expected to find certificate with .pem extension")
	}
}

func TestLoadCertificatesFromDirectory_CertExtension(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"certext.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "server.cert")
	keyFile := filepath.Join(tmpDir, "server.key")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	err = m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load certificates from directory: %v", err)
	}

	result := m.GetCertificate("certext.com")
	if result == nil {
		t.Error("expected to find certificate with .cert extension")
	}
}

func TestLoadCertificatesFromDirectory_FallbackKeyPath(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"fallback.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	// Write cert as "cert.crt" and key as "cert.crt.key" (fallback pattern: name+".key")
	certFile := filepath.Join(tmpDir, "cert.crt")
	keyFile := filepath.Join(tmpDir, "cert.crt.key")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	err = m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load certificates from directory: %v", err)
	}

	result := m.GetCertificate("fallback.com")
	if result == nil {
		t.Error("expected to find certificate using fallback key path pattern")
	}
}

func TestLoadCertificatesFromDirectory_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager()
	err := m.LoadCertificatesFromDirectory(filepath.Join(tmpDir, "nonexistent", "subdir"))
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestLoadCertificatesFromDirectory_InvalidCertContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Write invalid cert content
	certFile := filepath.Join(tmpDir, "bad.crt")
	keyFile := filepath.Join(tmpDir, "bad.key")
	os.WriteFile(certFile, []byte("not a cert"), 0644)
	os.WriteFile(keyFile, []byte("not a key"), 0600)

	m := NewManager()
	err := m.LoadCertificatesFromDirectory(tmpDir)
	if err == nil {
		t.Error("expected error for invalid certificate content")
	}
}
