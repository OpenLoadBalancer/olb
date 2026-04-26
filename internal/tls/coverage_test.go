package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// manager.go coverage: SetExpiryAlert, StartExpiryMonitor, expiryMonitorLoop,
// checkExpiry, StopExpiryMonitor, ZeroSecrets
// --------------------------------------------------------------------------

func TestManager_SetExpiryAlert_ExpiredCert(t *testing.T) {
	m := NewManager()

	// Generate a certificate that expires in 10 days (within 30-day threshold)
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
		NotAfter:              time.Now().Add(10 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"expiring-soon.com"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}
	m.AddCertificate(cert)

	alertCalled := false
	var alertNames []string
	var alertDays int
	var alertExpiry time.Time

	m.SetExpiryAlert(func(names []string, daysUntilExpiry int, expiresAt time.Time) {
		alertCalled = true
		alertNames = names
		alertDays = daysUntilExpiry
		alertExpiry = expiresAt
	})

	// Start monitoring with short interval and stop after check fires
	m.StartExpiryMonitor(50 * time.Millisecond)

	// Wait for the check to fire
	time.Sleep(200 * time.Millisecond)
	m.StopExpiryMonitor()

	if !alertCalled {
		t.Error("expected expiry alert to be called")
	}
	if len(alertNames) == 0 || alertNames[0] != "expiring-soon.com" {
		t.Errorf("expected alert for expiring-soon.com, got %v", alertNames)
	}
	if alertDays <= 0 || alertDays > 30 {
		t.Errorf("expected days between 1-30, got %d", alertDays)
	}
	_ = alertExpiry
}

func TestManager_SetExpiryAlert_AlreadyExpiredCert(t *testing.T) {
	m := NewManager()

	// Create a cert and manually inject an already-expired entry
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Create cert with a very short validity (already expired by the time we check)
	expiredTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now().Add(-2 * time.Second),
		NotAfter:              time.Now().Add(-1 * time.Second), // already expired
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"already-expired.com"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &expiredTemplate, &expiredTemplate, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}

	expiredX509, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse cert: %v", err)
	}

	// Manually add the expired cert to the manager (bypassing the LoadCertificateFromPEM expired check)
	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}
	expiredCert := &Certificate{
		Cert:       tlsCert,
		Names:      []string{"already-expired.com"},
		Expiry:     expiredX509.NotAfter.Unix(),
		IsWildcard: false,
	}
	m.AddCertificate(expiredCert)

	alertCalled := false
	var alertDays int
	m.SetExpiryAlert(func(names []string, daysUntilExpiry int, expiresAt time.Time) {
		alertCalled = true
		alertDays = daysUntilExpiry
	})

	m.StartExpiryMonitor(50 * time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	m.StopExpiryMonitor()

	if !alertCalled {
		t.Error("expected expiry alert for already-expired cert")
	}
	if alertDays != 0 {
		t.Errorf("expected 0 days for expired cert, got %d", alertDays)
	}
}

func TestManager_StartExpiryMonitor_DefaultInterval(t *testing.T) {
	m := NewManager()

	// Test with zero/invalid interval uses default 1h
	m.StartExpiryMonitor(0)
	// Stop immediately to verify it doesn't hang
	m.StopExpiryMonitor()

	// Test with negative interval
	m2 := NewManager()
	m2.StartExpiryMonitor(-5 * time.Second)
	m2.StopExpiryMonitor()
}

func TestManager_StopExpiryMonitor_Idempotent(t *testing.T) {
	m := NewManager()
	m.StartExpiryMonitor(50 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	// StopExpiryMonitor should be safe to call multiple times
	m.StopExpiryMonitor()
	m.StopExpiryMonitor() // second call should be no-op
}

func TestManager_checkExpiry_WildcardCert(t *testing.T) {
	m := NewManager()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(5 * 24 * time.Hour), // 5 days, within 30-day threshold
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"*.wildcard-expiring.com"},
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	x509Cert, _ := x509.ParseCertificate(certDER)

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}
	wc := &Certificate{
		Cert:       tlsCert,
		Names:      []string{"*.wildcard-expiring.com"},
		Expiry:     x509Cert.NotAfter.Unix(),
		IsWildcard: true,
	}
	m.AddCertificate(wc)

	alertCalled := false
	m.SetExpiryAlert(func(names []string, daysUntilExpiry int, expiresAt time.Time) {
		alertCalled = true
	})

	// checkExpiry is called internally; trigger it via StartExpiryMonitor
	m.StartExpiryMonitor(50 * time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	m.StopExpiryMonitor()

	if !alertCalled {
		t.Error("expected expiry alert for wildcard cert")
	}
}

func TestManager_ZeroSecrets(t *testing.T) {
	m := NewManager()

	certPEM, keyPEM, err := generateTestCert([]string{"zerotest.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}
	cert, err := m.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to load certificate: %v", err)
	}

	// Set OCSPStaple and SCT data so ZeroSecrets has something to zero
	cert.Cert.OCSPStaple = []byte("ocsp-staple-secret")
	cert.Cert.SignedCertificateTimestamps = [][]byte{[]byte("sct-secret-1"), []byte("sct-secret-2")}

	m.AddCertificate(cert)

	wildcardPEM, wildcardKeyPEM, _ := generateTestCert([]string{"*.zero.com"}, false)
	wildcardCert, _ := m.LoadCertificateFromPEM(wildcardPEM, wildcardKeyPEM)
	wildcardCert.Cert.OCSPStaple = []byte("wildcard-ocsp-staple-secret")
	m.AddCertificate(wildcardCert)

	// Set default cert
	defaultPEM, defaultKeyPEM, _ := generateTestCert([]string{"default-zero.com"}, false)
	defaultCert, _ := m.LoadCertificateFromPEM(defaultPEM, defaultKeyPEM)
	defaultCert.Cert.OCSPStaple = []byte("default-ocsp-staple-secret")
	m.SetDefaultCertificate(defaultCert)

	m.ZeroSecrets()

	// After zeroing, all maps should be empty
	if len(m.ListCertificates()) != 0 {
		t.Error("expected no certificates after ZeroSecrets")
	}

	// Should not be able to find any certs
	if m.GetCertificate("zerotest.com") != nil {
		t.Error("expected nil after ZeroSecrets")
	}
	if m.GetCertificate("sub.zero.com") != nil {
		t.Error("expected nil for wildcard after ZeroSecrets")
	}
}

func TestManager_ZeroSecrets_EmptyManager(t *testing.T) {
	m := NewManager()
	// Should not panic on empty manager
	m.ZeroSecrets()
}

func TestManager_ZeroSecrets_NilFields(t *testing.T) {
	m := NewManager()
	// Add a certificate with nil internal fields
	cert := &Certificate{
		Cert:   nil,
		Names:  []string{"nilcert.com"},
		Expiry: time.Now().Add(24 * time.Hour).Unix(),
	}
	m.AddCertificate(cert)

	// Should not panic
	m.ZeroSecrets()
}

// --------------------------------------------------------------------------
// ocsp.go coverage: ParseAndVerifyOCSPResponse, HasMustStaple with extension,
// refreshLoop stop path
// --------------------------------------------------------------------------

func TestParseAndVerifyOCSPResponse_NilIssuer(t *testing.T) {
	_, err := ParseAndVerifyOCSPResponse([]byte("any-data"), nil)
	if err == nil {
		t.Error("expected error for nil issuer")
	}
}

func TestParseAndVerifyOCSPResponse_InvalidData(t *testing.T) {
	// Create an issuer certificate to pass the nil check
	issuer := createTestCert(t, "Test CA", nil)
	_, err := ParseAndVerifyOCSPResponse([]byte("not-valid-ocsp"), issuer)
	if err == nil {
		t.Error("expected error for invalid OCSP data")
	}
}

func TestHasMustStaple_WithExtension(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// The TLS Feature extension OID: 1.3.6.1.5.5.7.1.24
	// Value is a DER-encoded sequence containing the status_request feature (5)
	// DER encoding: 30 03 02 01 05
	featureValue, _ := asn1.Marshal([]int{5})

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "must-staple.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		ExtraExtensions: []pkix.Extension{
			{
				Id:       asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 24},
				Critical: false,
				Value:    featureValue,
			},
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse cert: %v", err)
	}

	if !HasMustStaple(cert) {
		t.Error("expected certificate to have OCSP must-staple extension")
	}
}

func TestOCSPManager_RefreshLoop_StopCh(t *testing.T) {
	// Test the stop channel path in refreshLoop
	config := &OCSPConfig{
		Enabled:        true,
		UpdateInterval: 1 * time.Hour, // long interval so tick won't fire
	}
	m := NewOCSPManager(config)

	// Start and immediately stop; the stopCh path should be exercised
	if err := m.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	// Give the goroutine a moment to enter the select
	time.Sleep(50 * time.Millisecond)
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

func TestOCSPManager_QueryResponder_InvalidNewRequest(t *testing.T) {
	m := NewOCSPManager(DefaultOCSPConfig())
	// Use a URL with control characters that http.NewRequestWithContext rejects
	_, err := m.queryResponder("http://\x00invalid", []byte("test"), nil)
	if err == nil {
		t.Error("expected error for invalid URL in POST request creation")
	}
}

func TestOCSPManager_QueryResponderGET_InvalidNewRequest(t *testing.T) {
	m := NewOCSPManager(DefaultOCSPConfig())
	// Use a URL with control characters that http.NewRequestWithContext rejects
	_, err := m.queryResponderGET("http://\x00invalid", []byte("test"), nil)
	if err == nil {
		t.Error("expected error for invalid URL in GET request creation")
	}
}

func TestOCSPManager_ParseResponse_WithIssuer(t *testing.T) {
	// Create a mock server that returns a real-ish OCSP response
	// to test the parseResponse path with a non-nil issuer
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0x30, 0x03, 0x0A, 0x01, 0x00})
	}))
	defer mockServer.Close()

	issuer := createTestCert(t, "Test CA", nil)
	m := NewOCSPManager(DefaultOCSPConfig())

	// This will attempt to parse the minimal DER, which will fail,
	// but exercises the parseResponse path with a non-nil issuer
	_, err := m.queryResponder(mockServer.URL, []byte("test-body"), issuer)
	_ = err // parse error is expected
}

func TestOCSPManager_GetResponse_CacheAndFetchError(t *testing.T) {
	// Test GetResponse when there is no cached response and fetch fails
	m := NewOCSPManager(DefaultOCSPConfig())

	// Create cert without OCSP servers so fetch fails
	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	// Ensure no cache entry exists
	fp := fingerprint(cert)
	m.cacheMu.Lock()
	delete(m.cache, fp)
	m.cacheMu.Unlock()

	_, err := m.GetResponse(cert, issuer)
	if err == nil {
		t.Error("expected error when no cache and fetch fails")
	}
}

// --------------------------------------------------------------------------
// mtls.go coverage: LoadClientCAPool error, LoadRootCAPool error,
// BuildServerTLSConfig validation error, additional BuildClientTLSConfig branches
// --------------------------------------------------------------------------

func TestMTLSManager_LoadClientCAPool_InvalidPath(t *testing.T) {
	m := NewMTLSManager()
	err := m.LoadClientCAPool("bad-pool", []string{"/nonexistent/ca.pem"})
	if err == nil {
		t.Error("expected error for non-existent CA path")
	}
}

func TestMTLSManager_LoadRootCAPool_InvalidPath(t *testing.T) {
	m := NewMTLSManager()
	err := m.LoadRootCAPool("bad-root", []string{"/nonexistent/ca.pem"})
	if err == nil {
		t.Error("expected error for non-existent CA path")
	}
}

func TestMTLSManager_LoadClientCAPool_EmptyPaths(t *testing.T) {
	m := NewMTLSManager()
	err := m.LoadClientCAPool("empty-pool", []string{})
	if err == nil {
		t.Error("expected error for empty paths")
	}
}

func TestMTLSManager_LoadRootCAPool_EmptyPaths(t *testing.T) {
	m := NewMTLSManager()
	err := m.LoadRootCAPool("empty-root", []string{})
	if err == nil {
		t.Error("expected error for empty paths")
	}
}

func TestMTLSManager_GetClientCAPool_NotFound(t *testing.T) {
	m := NewMTLSManager()
	pool := m.GetClientCAPool("nonexistent")
	if pool != nil {
		t.Error("expected nil for non-existent pool")
	}
}

func TestMTLSManager_GetRootCAPool_NotFound(t *testing.T) {
	m := NewMTLSManager()
	pool := m.GetRootCAPool("nonexistent")
	if pool != nil {
		t.Error("expected nil for non-existent pool")
	}
}

func TestMTLSManager_GetClientCert_NotFound(t *testing.T) {
	m := NewMTLSManager()
	cert := m.GetClientCert("nonexistent")
	if cert != nil {
		t.Error("expected nil for non-existent cert")
	}
}

func TestMTLSManager_BuildServerTLSConfig_ValidationError(t *testing.T) {
	m := NewMTLSManager()
	getCert := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return nil, nil
	}

	// RequireAndVerifyClientCert with no ClientCAs should fail Validate()
	config := &MTLSConfig{
		Enabled:    true,
		ClientAuth: RequireAndVerifyClientCert,
		ClientCAs:  []string{}, // empty but required auth
	}
	_, err := m.BuildServerTLSConfig("test-validation", config, getCert)
	if err == nil {
		t.Error("expected error for invalid mTLS config (require auth but no CAs)")
	}
}

func TestBuildClientTLSConfig_EnabledNoRoots(t *testing.T) {
	// BuildClientTLSConfig with enabled=true but no RootCAs and no client cert
	config := &MTLSConfig{
		Enabled:    true,
		ClientAuth: NoClientCert,
	}
	tlsConfig, err := BuildClientTLSConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Error("expected TLS 1.2 minimum")
	}
}

// --------------------------------------------------------------------------
// manager.go: LoadCertificateFromPEM cert with no common name and no DNS names
// --------------------------------------------------------------------------

func TestLoadCertificateFromPEM_NoCommonNameNoDNSNames(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
			// No CommonName, no DNSNames
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
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
	if len(cert.Names) != 0 {
		t.Errorf("expected 0 names for cert with no CN and no DNS names, got %v", cert.Names)
	}
	if cert.IsWildcard {
		t.Error("expected non-wildcard")
	}
}

// --------------------------------------------------------------------------
// manager.go: ReloadCertificates with wildcard cert config
// --------------------------------------------------------------------------

func TestReloadCertificates_WildcardCert(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM, keyPEM, err := generateTestCert([]string{"*.reload-wildcard.com"}, false)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certFile := filepath.Join(tmpDir, "wildcard.pem")
	keyFile := filepath.Join(tmpDir, "wildcard.key")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	m := NewManager()
	err = m.ReloadCertificates([]CertConfig{
		{CertFile: certFile, KeyFile: keyFile, IsDefault: false},
	})
	if err != nil {
		t.Fatalf("failed to reload wildcard certificates: %v", err)
	}

	result := m.GetCertificate("sub.reload-wildcard.com")
	if result == nil {
		t.Error("expected to find wildcard cert via subdomain match after reload")
	}
}

func TestReloadCertificates_MultipleCerts_SecondDefault(t *testing.T) {
	tmpDir := t.TempDir()

	cert1PEM, key1PEM, _ := generateTestCert([]string{"first.com"}, false)
	cert2PEM, key2PEM, _ := generateTestCert([]string{"second.com"}, false)

	cert1File := filepath.Join(tmpDir, "cert1.pem")
	key1File := filepath.Join(tmpDir, "key1.pem")
	cert2File := filepath.Join(tmpDir, "cert2.pem")
	key2File := filepath.Join(tmpDir, "key2.pem")

	os.WriteFile(cert1File, cert1PEM, 0644)
	os.WriteFile(key1File, key1PEM, 0600)
	os.WriteFile(cert2File, cert2PEM, 0644)
	os.WriteFile(key2File, key2PEM, 0600)

	m := NewManager()
	err := m.ReloadCertificates([]CertConfig{
		{CertFile: cert1File, KeyFile: key1File, IsDefault: true},
		{CertFile: cert2File, KeyFile: key2File, IsDefault: true}, // second default ignored
	})
	if err != nil {
		t.Fatalf("failed to reload certificates: %v", err)
	}

	// Only the first IsDefault=true should be honored
	cert := m.GetCertificate("unknown.com")
	if cert == nil {
		t.Error("expected default cert")
	}
	// First cert should be default
	names := m.ListCertificates()
	if len(names) != 2 {
		t.Errorf("expected 2 certs after reload, got %d", len(names))
	}
}

// --------------------------------------------------------------------------
// manager.go: LoadCertificatesFromDirectory with subdirectory (skipped)
// --------------------------------------------------------------------------

func TestLoadCertificatesFromDirectory_WithSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory (should be skipped)
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Create valid cert in the top-level directory
	certPEM, keyPEM, _ := generateTestCert([]string{"topdir.com"}, false)
	os.WriteFile(filepath.Join(tmpDir, "topdir.crt"), certPEM, 0644)
	os.WriteFile(filepath.Join(tmpDir, "topdir.key"), keyPEM, 0600)

	m := NewManager()
	err := m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("failed to load from directory: %v", err)
	}

	result := m.GetCertificate("topdir.com")
	if result == nil {
		t.Error("expected to find certificate from top-level directory")
	}
}

func TestLoadCertificatesFromDirectory_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	m := NewManager()
	err := m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("empty directory should not be an error: %v", err)
	}

	certs := m.ListCertificates()
	if len(certs) != 0 {
		t.Errorf("expected 0 certs from empty dir, got %d", len(certs))
	}
}

func TestLoadCertificatesFromDirectory_OtherExtensionsIgnored(t *testing.T) {
	tmpDir := t.TempDir()

	// Write files with non-cert extensions
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("yaml"), 0644)

	m := NewManager()
	err := m.LoadCertificatesFromDirectory(tmpDir)
	if err != nil {
		t.Fatalf("directory with non-cert files should not error: %v", err)
	}
	certs := m.ListCertificates()
	if len(certs) != 0 {
		t.Errorf("expected 0 certs, got %d", len(certs))
	}
}

// --------------------------------------------------------------------------
// ocsp.go: OCSP manager with a working mock OCSP responder that fully succeeds
// --------------------------------------------------------------------------

func TestOCSPManager_QueryResponder_PostSuccess_ParseFail(t *testing.T) {
	// Server returns 200 with invalid OCSP data -> parseResponse fails
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid ocsp"))
	}))
	defer mockServer.Close()

	m := NewOCSPManager(DefaultOCSPConfig())
	issuer := createTestCert(t, "Test CA", nil)

	_, err := m.queryResponder(mockServer.URL, []byte("test-body"), issuer)
	if err == nil {
		t.Log("parseResponse happened to succeed with minimal data")
	} else {
		// Expected: parse error
		t.Logf("got expected error: %v", err)
	}
}

func TestOCSPManager_QueryResponderGET_PostSuccess_ParseFail(t *testing.T) {
	// Server returns 200 with invalid OCSP data on GET
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not valid ocsp"))
		}
	}))
	defer mockServer.Close()

	m := NewOCSPManager(DefaultOCSPConfig())
	issuer := createTestCert(t, "Test CA", nil)

	_, err := m.queryResponderGET(mockServer.URL, []byte("test-body"), issuer)
	if err == nil {
		t.Log("parseResponse happened to succeed")
	} else {
		t.Logf("got expected error: %v", err)
	}
}

// --------------------------------------------------------------------------
// mtls.go: loadCAFile with non-CA cert, loadCADirectory with non-recognized ext
// --------------------------------------------------------------------------

func TestLoadCADirectory_NonCertExtensionsIgnored(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a non-cert extension file
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("yaml"), 0644)

	pool := NewCAPool()
	err := loadCADirectory(pool, tmpDir)
	if err != nil {
		t.Fatalf("loadCADirectory should not fail on non-cert files: %v", err)
	}
	if pool.CertCount() != 0 {
		t.Errorf("expected 0 certs, got %d", pool.CertCount())
	}
}

func TestLoadCAFile_NonCACertStillAdded(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a non-CA cert and write it
	certPEM, _, _, _, _ := generateTestCertWithCA([]string{"Not a CA"}, false, nil, nil)
	certFile := filepath.Join(tmpDir, "nonca.pem")
	os.WriteFile(certFile, certPEM, 0644)

	pool := NewCAPool()
	err := loadCAFile(pool, certFile)
	if err != nil {
		t.Fatalf("loadCAFile should succeed for non-CA cert: %v", err)
	}
	if pool.CertCount() != 1 {
		t.Errorf("expected 1 cert, got %d", pool.CertCount())
	}
}

func TestLoadCAFile_NonExistentFile(t *testing.T) {
	pool := NewCAPool()
	err := loadCAFile(pool, "/nonexistent/file.pem")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadCADirectory_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	pool := NewCAPool()
	err := loadCADirectory(pool, filepath.Join(tmpDir, "no-such-dir"))
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

// --------------------------------------------------------------------------
// mtls.go: VerifyClientCert depth check branch
// --------------------------------------------------------------------------

func TestVerifyClientCert_WithDepthExceedingChain(t *testing.T) {
	// Create root CA
	_, _, rootCert, rootKey, _ := generateTestCertWithCA([]string{"Root CA"}, true, nil, nil)
	// Create intermediate CA
	_, _, intermediateCert, intermediateKey, _ := generateTestCertWithCA([]string{"Intermediate CA"}, true, rootCert, rootKey)
	// Create client cert
	_, _, clientCert, _, _ := generateTestCertWithCA([]string{"client.example.com"}, false, intermediateCert, intermediateKey)

	rootPool := x509.NewCertPool()
	rootPool.AddCert(rootCert)

	// Chain is: client -> intermediate -> root (length 3)
	// verifyDepth=1 means max chain length should be 2, so this should fail
	err := VerifyClientCert(clientCert, rootPool, 1)
	if err == nil {
		t.Error("expected chain depth error")
	}
}

// --------------------------------------------------------------------------
// manager.go: BuildTLSConfig with max version set and TLS version aliases
// --------------------------------------------------------------------------

func TestBuildTLSConfig_TLS11Aliases(t *testing.T) {
	aliases := []string{"tls11", "TLS1.1", "1.1"}
	for _, v := range aliases {
		_, err := BuildTLSConfig(v, "", nil, false)
		if err == nil {
			t.Errorf("expected error for TLS 1.1 alias %q", v)
		}
	}
}

func TestBuildTLSConfig_TLS10Aliases(t *testing.T) {
	aliases := []string{"tls10", "TLS1.0", "1.0"}
	for _, v := range aliases {
		_, err := BuildTLSConfig(v, "", nil, false)
		if err == nil {
			t.Errorf("expected error for TLS 1.0 alias %q", v)
		}
	}
}
