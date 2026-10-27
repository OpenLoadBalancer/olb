package tls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultOCSPConfig(t *testing.T) {
	config := DefaultOCSPConfig()

	if !config.Enabled {
		t.Error("Enabled should be true by default")
	}
	if config.UpdateInterval != 1*time.Hour {
		t.Errorf("UpdateInterval = %v, want 1h", config.UpdateInterval)
	}
	if config.CacheDuration != 24*time.Hour {
		t.Errorf("CacheDuration = %v, want 24h", config.CacheDuration)
	}
	if config.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", config.Timeout)
	}
}

func TestNewOCSPManager(t *testing.T) {
	config := DefaultOCSPConfig()
	manager := NewOCSPManager(config)

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}
	if manager.config != config {
		t.Error("Config mismatch")
	}
	if len(manager.cache) != 0 {
		t.Error("Cache should be empty initially")
	}
}

func TestNewOCSPManager_NilConfig(t *testing.T) {
	manager := NewOCSPManager(nil)

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}
	if manager.config == nil {
		t.Error("Should use default config when nil")
	}
}

func TestOCSPManager_StartStop(t *testing.T) {
	config := DefaultOCSPConfig()
	manager := NewOCSPManager(config)

	err := manager.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	err = manager.Stop()
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

func TestOCSPResponse_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		nextUpdate time.Time
		expected   bool
	}{
		{
			name:       "not expired",
			nextUpdate: now.Add(1 * time.Hour),
			expected:   false,
		},
		{
			name:       "expired",
			nextUpdate: now.Add(-1 * time.Hour),
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &OCSPResponse{
				NextUpdate: tt.nextUpdate,
				ThisUpdate: now.Add(-2 * time.Hour),
			}
			if got := resp.IsExpired(); got != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOCSPResponse_IsValid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		thisUpdate time.Time
		nextUpdate time.Time
		expected   bool
	}{
		{
			name:       "valid",
			thisUpdate: now.Add(-1 * time.Hour),
			nextUpdate: now.Add(1 * time.Hour),
			expected:   true,
		},
		{
			name:       "not yet valid",
			thisUpdate: now.Add(1 * time.Hour),
			nextUpdate: now.Add(2 * time.Hour),
			expected:   false,
		},
		{
			name:       "expired",
			thisUpdate: now.Add(-2 * time.Hour),
			nextUpdate: now.Add(-1 * time.Hour),
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &OCSPResponse{
				ThisUpdate: tt.thisUpdate,
				NextUpdate: tt.nextUpdate,
			}
			if got := resp.IsValid(); got != tt.expected {
				t.Errorf("IsValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOCSPResponse_RemainingValidity(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		nextUpdate time.Time
		expected   time.Duration
	}{
		{
			name:       "valid",
			nextUpdate: now.Add(1 * time.Hour),
			expected:   1 * time.Hour,
		},
		{
			name:       "expired",
			nextUpdate: now.Add(-1 * time.Hour),
			expected:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &OCSPResponse{
				ThisUpdate: now.Add(-2 * time.Hour),
				NextUpdate: tt.nextUpdate,
			}
			got := resp.RemainingValidity()
			// Allow some tolerance for execution time
			diff := got - tt.expected
			if diff < 0 {
				diff = -diff
			}
			if diff > 5*time.Second {
				t.Errorf("RemainingValidity() = %v, want approximately %v", got, tt.expected)
			}
		})
	}
}

func TestOCSPManager_GetResponse_Disabled(t *testing.T) {
	config := &OCSPConfig{Enabled: false}
	manager := NewOCSPManager(config)

	_, err := manager.GetResponse(nil, nil)
	if err == nil {
		t.Error("Should return error when disabled")
	}
}

func TestOCSPManager_GetResponse_NilCert(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	issuer := createTestCert(t, "Test CA", nil)
	_, err := manager.GetResponse(nil, issuer)
	if err == nil {
		t.Error("Should return error for nil certificate")
	}
}

func TestOCSPManager_GetResponse_NilIssuer(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	cert := createTestCert(t, "example.com", nil)
	_, err := manager.GetResponse(cert, nil)
	if err == nil {
		t.Error("Should return error for nil issuer")
	}
}

func TestOCSPManager_Cache(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	// Create a mock response
	now := time.Now()
	fp := "test-fingerprint"
	resp := &OCSPResponse{
		Raw:        []byte("test-response"),
		CachedAt:   now,
		ThisUpdate: now.Add(-1 * time.Hour),
		NextUpdate: now.Add(1 * time.Hour),
	}

	// Add to cache
	manager.cacheMu.Lock()
	manager.cache[fp] = resp
	manager.cacheMu.Unlock()

	// Check cache stats
	total, valid, expired := manager.GetCacheStats()
	if total != 1 {
		t.Errorf("Total = %d, want 1", total)
	}
	if valid != 1 {
		t.Errorf("Valid = %d, want 1", valid)
	}
	if expired != 0 {
		t.Errorf("Expired = %d, want 0", expired)
	}

	// Clear cache
	manager.ClearCache()
	total, _, _ = manager.GetCacheStats()
	if total != 0 {
		t.Errorf("Cache should be empty after clear, got %d", total)
	}
}

func TestHasMustStaple(t *testing.T) {
	// Certificate without must-staple
	certWithout := createTestCert(t, "example.com", nil)
	if HasMustStaple(certWithout) {
		t.Error("Certificate should not have must-staple")
	}

	// Note: Creating a certificate with must-staple requires
	// adding the TLS Feature extension which is complex to set up
	// in a test. We'll skip testing the positive case for now.
}

func TestFingerprint(t *testing.T) {
	cert := createTestCert(t, "example.com", nil)
	fp := fingerprint(cert)

	if fp == "" {
		t.Error("Fingerprint should not be empty")
	}

	// Same cert should produce same fingerprint
	fp2 := fingerprint(cert)
	if fp != fp2 {
		t.Error("Fingerprint should be consistent")
	}

	// Nil cert should return empty
	if fingerprint(nil) != "" {
		t.Error("Nil cert should return empty fingerprint")
	}
}

func TestEncodeOCSPRequest(t *testing.T) {
	request := []byte("test ocsp request")
	pem := EncodeOCSPRequest(request)

	if len(pem) == 0 {
		t.Error("PEM should not be empty")
	}

	if !contains(string(pem), "OCSP REQUEST") {
		t.Error("PEM should contain OCSP REQUEST header")
	}
}

func TestEncodeOCSPResponse(t *testing.T) {
	response := []byte("test ocsp response")
	pem := EncodeOCSPResponse(response)

	if len(pem) == 0 {
		t.Error("PEM should not be empty")
	}

	if !contains(string(pem), "OCSP RESPONSE") {
		t.Error("PEM should contain OCSP RESPONSE header")
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func createTestCert(t *testing.T, cn string, parent *x509.Certificate) *x509.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	var parentCert *x509.Certificate
	var parentKey any = priv

	if parent != nil {
		parentCert = parent
		parentKey = priv
	} else {
		parentCert = template
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, parentCert, &priv.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	return cert
}

func TestOCSPManager_StartStop_Disabled(t *testing.T) {
	config := &OCSPConfig{Enabled: false}
	manager := NewOCSPManager(config)

	err := manager.Start()
	if err != nil {
		t.Fatalf("Start error for disabled manager: %v", err)
	}

	err = manager.Stop()
	if err != nil {
		t.Fatalf("Stop error for disabled manager: %v", err)
	}
}

func TestOCSPManager_GetResponseBytes_Disabled(t *testing.T) {
	config := &OCSPConfig{Enabled: false}
	manager := NewOCSPManager(config)

	_, err := manager.GetResponseBytes(nil, nil)
	if err == nil {
		t.Error("Expected error when OCSP is disabled")
	}
}

func TestOCSPManager_GetResponseBytes_NilCert(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())
	issuer := createTestCert(t, "Test CA", nil)

	_, err := manager.GetResponseBytes(nil, issuer)
	if err == nil {
		t.Error("Expected error for nil certificate")
	}
}

func TestOCSPManager_GetResponse_NoOCSPServers(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	_, err := manager.GetResponse(cert, issuer)
	if err == nil {
		t.Error("Expected error for cert without OCSP servers")
	}
}

func TestOCSPManager_RefreshAll_EmptyCache(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())
	// Should not panic with empty cache
	manager.refreshAll()
}

func TestOCSPManager_RefreshAll_WithEntries(t *testing.T) {
	config := DefaultOCSPConfig()
	config.UpdateInterval = 1 * time.Hour
	manager := NewOCSPManager(config)

	now := time.Now()

	// Add a response that is expiring soon (remaining < 2*UpdateInterval)
	manager.cacheMu.Lock()
	manager.cache["expiring-cert"] = &OCSPResponse{
		Raw:        []byte("test"),
		CachedAt:   now,
		ThisUpdate: now.Add(-23 * time.Hour),
		NextUpdate: now.Add(30 * time.Minute), // Less than 2 hours
	}
	// Add a response that is not expiring soon
	manager.cache["valid-cert"] = &OCSPResponse{
		Raw:        []byte("test2"),
		CachedAt:   now,
		ThisUpdate: now.Add(-1 * time.Hour),
		NextUpdate: now.Add(23 * time.Hour), // More than 2 hours
	}
	manager.cacheMu.Unlock()

	manager.refreshAll()

	// Expiring cert should be removed
	manager.cacheMu.RLock()
	_, hasExpiring := manager.cache["expiring-cert"]
	_, hasValid := manager.cache["valid-cert"]
	manager.cacheMu.RUnlock()

	if hasExpiring {
		t.Error("Expected expiring cert to be removed from cache")
	}
	if !hasValid {
		t.Error("Expected valid cert to remain in cache")
	}
}

func TestOCSPManager_CacheStats_Mixed(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())
	now := time.Now()

	manager.cacheMu.Lock()
	manager.cache["valid"] = &OCSPResponse{
		ThisUpdate: now.Add(-1 * time.Hour),
		NextUpdate: now.Add(1 * time.Hour),
	}
	manager.cache["expired"] = &OCSPResponse{
		ThisUpdate: now.Add(-2 * time.Hour),
		NextUpdate: now.Add(-1 * time.Hour),
	}
	manager.cacheMu.Unlock()

	total, valid, expired := manager.GetCacheStats()
	if total != 2 {
		t.Errorf("Total = %d, want 2", total)
	}
	if valid != 1 {
		t.Errorf("Valid = %d, want 1", valid)
	}
	if expired != 1 {
		t.Errorf("Expired = %d, want 1", expired)
	}
}

func TestParseOCSPResponse_Invalid(t *testing.T) {
	_, err := ParseOCSPResponse([]byte("invalid ocsp data"))
	if err == nil {
		t.Error("Expected error for invalid OCSP response data")
	}
}

func TestCreateOCSPRequest_ValidCerts(t *testing.T) {
	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	// CreateOCSPRequest may succeed or fail depending on cert contents,
	// but should not panic
	_, _ = CreateOCSPRequest(cert, issuer)
}

func TestOCSPManager_GetResponse_CachedValid(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())
	now := time.Now()

	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	fp := fingerprint(cert)

	// Pre-populate cache with a valid response
	cachedResp := &OCSPResponse{
		Raw:        []byte("cached-response"),
		CachedAt:   now,
		ThisUpdate: now.Add(-1 * time.Hour),
		NextUpdate: now.Add(1 * time.Hour),
	}
	manager.cacheMu.Lock()
	manager.cache[fp] = cachedResp
	manager.cacheMu.Unlock()

	resp, err := manager.GetResponse(cert, issuer)
	if err != nil {
		t.Fatalf("GetResponse error: %v", err)
	}
	if string(resp.Raw) != "cached-response" {
		t.Error("Expected cached response to be returned")
	}
}

func TestOCSPManager_GetResponseBytes_CachedValid(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())
	now := time.Now()

	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	fp := fingerprint(cert)

	// Pre-populate cache with a valid response
	cachedResp := &OCSPResponse{
		Raw:        []byte("cached-raw-bytes"),
		CachedAt:   now,
		ThisUpdate: now.Add(-1 * time.Hour),
		NextUpdate: now.Add(1 * time.Hour),
	}
	manager.cacheMu.Lock()
	manager.cache[fp] = cachedResp
	manager.cacheMu.Unlock()

	rawBytes, err := manager.GetResponseBytes(cert, issuer)
	if err != nil {
		t.Fatalf("GetResponseBytes error: %v", err)
	}
	if string(rawBytes) != "cached-raw-bytes" {
		t.Errorf("Got %q, want 'cached-raw-bytes'", string(rawBytes))
	}
}

func TestOCSPManager_FetchResponse_NoOCSPServers(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	_, err := manager.fetchResponse(cert, issuer)
	if err == nil {
		t.Error("Expected error for cert without OCSP servers")
	}
}

func TestOCSPManager_QueryResponder_MockServer(t *testing.T) {
	// Create a mock OCSP responder that returns a valid OCSP response
	// We'll return 200 with a basic OCSP response structure
	mockOCSPServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			// Return a basic OCSP response - the actual parsing will fail
			// but we're testing the HTTP layer
			w.Header().Set("Content-Type", "application/ocsp-response")
			w.WriteHeader(http.StatusOK)
			// Write minimal bytes that look like an OCSP response
			w.Write([]byte{0x30, 0x03, 0x0A, 0x01, 0x00})
		}
	}))
	defer mockOCSPServer.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())

	// The response will fail to parse as valid OCSP, but we verify the HTTP flow works
	_, err := manager.queryResponder(mockOCSPServer.URL, []byte("test-request"), nil)
	// We expect a parse error since our mock response isn't a real OCSP response
	if err == nil {
		t.Log("queryResponder succeeded (mock response happened to parse)")
	}
}

func TestOCSPManager_QueryResponderGET_MockServer(t *testing.T) {
	mockOCSPServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "application/ocsp-response")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte{0x30, 0x03, 0x0A, 0x01, 0x00})
		}
	}))
	defer mockOCSPServer.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())

	_, err := manager.queryResponderGET(mockOCSPServer.URL, []byte("test-request"), nil)
	// Parse error is expected since it's not a real OCSP response
	if err == nil {
		t.Log("queryResponderGET succeeded (mock response happened to parse)")
	}
}

func TestOCSPManager_QueryResponderGET_ServerError(t *testing.T) {
	mockOCSPServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockOCSPServer.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())

	_, err := manager.queryResponderGET(mockOCSPServer.URL, []byte("test-request"), nil)
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

func TestOCSPManager_ParseResponse_ValidStructure(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	// Use a bytes.Reader to test parseResponse
	// An invalid OCSP response body should return a parse error
	_, err := manager.parseResponse(bytes.NewReader([]byte("invalid ocsp data")), nil)
	if err == nil {
		t.Error("Expected error for invalid OCSP response data")
	}
}

func TestParseOCSPResponse_Wrapper(t *testing.T) {
	// ParseOCSPResponse is a thin wrapper around ocsp.ParseResponse
	_, err := ParseOCSPResponse([]byte{0x30, 0x03, 0x0A, 0x01, 0x00})
	// This will likely error unless it's a valid OCSP response
	_ = err // Just testing it doesn't panic
}

func TestCreateOCSPRequest_Wrapper(t *testing.T) {
	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	// CreateOCSPRequest is a thin wrapper around ocsp.CreateRequest
	requestBytes, err := CreateOCSPRequest(cert, issuer)
	// May succeed or fail depending on cert contents, but should not panic
	if err == nil && len(requestBytes) == 0 {
		t.Error("Expected non-empty request bytes when no error")
	}
}

func TestOCSPManager_FetchResponse_WithOCSPServers(t *testing.T) {
	// Create a mock OCSP responder
	mockOCSPServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return non-200 to force GET fallback, then return non-200 again
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockOCSPServer.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())

	// Create a cert with the mock server as OCSP server
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject: pkix.Name{
			CommonName: "example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		OCSPServer:            []string{mockOCSPServer.URL},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	issuer := createTestCert(t, "Test CA", nil)

	// fetchResponse should attempt the server and fail since our mock
	// doesn't return a valid OCSP response
	_, err = manager.fetchResponse(cert, issuer)
	if err == nil {
		t.Error("Expected error from fetchResponse with mock server that returns invalid OCSP data")
	}
}

func TestOCSPManager_FetchResponse_MultipleResponders(t *testing.T) {
	// First server fails, second server also fails
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server2.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(43),
		Subject: pkix.Name{
			CommonName: "multi-ocsp.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		OCSPServer:            []string{server1.URL, server2.URL},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	issuer := createTestCert(t, "Test CA", nil)

	_, err = manager.fetchResponse(cert, issuer)
	if err == nil {
		t.Error("Expected error when all OCSP responders fail")
	}
}

func TestOCSPManager_GetResponse_ExpiredCacheFallback(t *testing.T) {
	// Test that GetResponse returns an expired cached response when fetch fails
	manager := NewOCSPManager(DefaultOCSPConfig())
	now := time.Now()

	cert := createTestCert(t, "example.com", nil)
	issuer := createTestCert(t, "Test CA", nil)

	fp := fingerprint(cert)

	// Pre-populate cache with an expired response
	expiredResp := &OCSPResponse{
		Raw:        []byte("expired-response"),
		CachedAt:   now.Add(-2 * time.Hour),
		ThisUpdate: now.Add(-2 * time.Hour),
		NextUpdate: now.Add(-1 * time.Hour), // expired
	}
	manager.cacheMu.Lock()
	manager.cache[fp] = expiredResp
	manager.cacheMu.Unlock()

	// GetResponse should try to fetch a new one, fail (no OCSP servers),
	// and return the expired cached response as fallback
	resp, err := manager.GetResponse(cert, issuer)
	if err != nil {
		t.Fatalf("GetResponse error: %v", err)
	}
	if string(resp.Raw) != "expired-response" {
		t.Error("Expected expired cached response to be returned as fallback")
	}
}

func TestOCSPManager_QueryResponder_PostSuccess(t *testing.T) {
	// Test the POST path that returns 200 but with invalid OCSP data
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/ocsp-request" {
			t.Errorf("Expected Content-Type application/ocsp-request, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Accept") != "application/ocsp-response" {
			t.Errorf("Expected Accept application/ocsp-response, got %s", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-ocsp"))
	}))
	defer mockServer.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())
	issuer := createTestCert(t, "Test CA", nil)

	// The request body doesn't matter for this test since we're testing HTTP flow
	_, err := manager.queryResponder(mockServer.URL, []byte("test-request-body"), issuer)
	// Should get a parse error since the response isn't valid OCSP
	if err == nil {
		t.Log("queryResponder succeeded (unexpected but ok)")
	}
}

func TestOCSPManager_QueryResponder_PostFail_GetFallback(t *testing.T) {
	// POST returns non-200, triggering GET fallback
	postAttempts := 0
	getAttempts := 0

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			postAttempts++
			w.WriteHeader(http.StatusInternalServerError)
		} else if r.Method == "GET" {
			getAttempts++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte{0x30, 0x03, 0x0A, 0x01, 0x00})
		}
	}))
	defer mockServer.Close()

	manager := NewOCSPManager(DefaultOCSPConfig())
	issuer := createTestCert(t, "Test CA", nil)

	manager.queryResponder(mockServer.URL, []byte("test-request-body"), issuer)

	if postAttempts != 1 {
		t.Errorf("Expected 1 POST attempt, got %d", postAttempts)
	}
	if getAttempts != 1 {
		t.Errorf("Expected 1 GET attempt, got %d", getAttempts)
	}
}

func TestOCSPManager_QueryResponderGET_InvalidURL(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	_, err := manager.queryResponderGET("http://invalid-host-that-does-not-exist.local:1", []byte("test"), nil)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestOCSPManager_QueryResponder_InvalidURL(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	_, err := manager.queryResponder("http://invalid-host-that-does-not-exist.local:1", []byte("test"), nil)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestOCSPManager_ParseResponse_ReadError(t *testing.T) {
	manager := NewOCSPManager(DefaultOCSPConfig())

	// Use a reader that always returns an error
	_, err := manager.parseResponse(&errorReader{}, nil)
	if err == nil {
		t.Error("Expected error from error reader")
	}
}

// errorReader is a helper io.Reader that always returns an error.
type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, bytes.ErrTooLarge
}

// TestOCSPManager_StopIdempotent ensures Stop can be called multiple times
// without panicking on a double channel close (parity with StopExpiryMonitor).
func TestOCSPManager_StopIdempotent(t *testing.T) {
	m := NewOCSPManager(nil)
	if err := m.Stop(); err != nil {
		t.Fatalf("first Stop returned error: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = m.Stop()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("second Stop did not return (possible deadlock)")
	}
}
