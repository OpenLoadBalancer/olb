package tls

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/ocsp"
)

// OCSPConfig contains OCSP stapling configuration.
type OCSPConfig struct {
	Enabled        bool
	UpdateInterval time.Duration
	CacheDuration  time.Duration
	Timeout        time.Duration
	MustStaple     bool // Enforce OCSP must-staple
}

// DefaultOCSPConfig returns default OCSP configuration.
func DefaultOCSPConfig() *OCSPConfig {
	return &OCSPConfig{
		Enabled:        true,
		UpdateInterval: 1 * time.Hour,
		CacheDuration:  24 * time.Hour,
		Timeout:        10 * time.Second,
		MustStaple:     false,
	}
}

// OCSPResponse represents a cached OCSP response.
type OCSPResponse struct {
	Raw        []byte
	Parsed     *ocsp.Response
	CachedAt   time.Time
	NextUpdate time.Time
	ThisUpdate time.Time
}

// IsExpired returns true if the OCSP response has expired.
func (r *OCSPResponse) IsExpired() bool {
	return time.Now().After(r.NextUpdate)
}

// IsValid returns true if the OCSP response is still valid.
func (r *OCSPResponse) IsValid() bool {
	now := time.Now()
	return now.After(r.ThisUpdate) && now.Before(r.NextUpdate)
}

// RemainingValidity returns the remaining validity period.
func (r *OCSPResponse) RemainingValidity() time.Duration {
	if r.IsExpired() {
		return 0
	}
	return r.NextUpdate.Sub(time.Now())
}

// OCSPManager manages OCSP stapling for certificates.
type OCSPManager struct {
	config *OCSPConfig

	// Cache: key = certificate fingerprint
	cache   map[string]*OCSPResponse
	cacheMu sync.RWMutex

	httpClient *http.Client
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewOCSPManager creates a new OCSP manager.
func NewOCSPManager(config *OCSPConfig) *OCSPManager {
	if config == nil {
		config = DefaultOCSPConfig()
	}

	return &OCSPManager{
		config: config,
		cache:  make(map[string]*OCSPResponse),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		stopCh: make(chan struct{}),
	}
}

// Start starts the OCSP manager background refresh.
func (m *OCSPManager) Start() error {
	if !m.config.Enabled {
		return nil
	}

	m.wg.Add(1)
	go m.refreshLoop()

	return nil
}

// Stop stops the OCSP manager.
func (m *OCSPManager) Stop() error {
	close(m.stopCh)
	m.wg.Wait()
	return nil
}

// refreshLoop periodically refreshes OCSP responses.
func (m *OCSPManager) refreshLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.refreshAll()
		}
	}
}

// refreshAll refreshes all cached OCSP responses.
func (m *OCSPManager) refreshAll() {
	m.cacheMu.RLock()
	certFingerprints := make([]string, 0, len(m.cache))
	for fp := range m.cache {
		certFingerprints = append(certFingerprints, fp)
	}
	m.cacheMu.RUnlock()

	for _, fp := range certFingerprints {
		// Get the certificate from cache entry to refresh
		m.cacheMu.RLock()
		resp := m.cache[fp]
		m.cacheMu.RUnlock()

		if resp != nil {
			// Mark for refresh if expiring soon
			if resp.RemainingValidity() < m.config.UpdateInterval*2 {
				// We don't have the original cert here, so we just remove
				// The next GetResponse call will fetch fresh
				m.cacheMu.Lock()
				delete(m.cache, fp)
				m.cacheMu.Unlock()
			}
		}
	}
}

// GetResponse gets an OCSP response for a certificate.
// It returns cached response if valid, or fetches a new one.
func (m *OCSPManager) GetResponse(cert *x509.Certificate, issuer *x509.Certificate) (*OCSPResponse, error) {
	if !m.config.Enabled {
		return nil, errors.New("ocsp is disabled")
	}

	if cert == nil {
		return nil, errors.New("certificate is nil")
	}

	if issuer == nil {
		return nil, errors.New("issuer certificate is nil")
	}

	// Generate fingerprint for cache key
	fp := fingerprint(cert)

	// Check cache
	m.cacheMu.RLock()
	cached := m.cache[fp]
	m.cacheMu.RUnlock()

	if cached != nil && cached.IsValid() {
		return cached, nil
	}

	// Fetch new response
	resp, err := m.fetchResponse(cert, issuer)
	if err != nil {
		// Return cached response even if expired (better than nothing)
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	// Cache the response
	m.cacheMu.Lock()
	m.cache[fp] = resp
	m.cacheMu.Unlock()

	return resp, nil
}

// GetResponseBytes gets raw OCSP response bytes for TLS stapling.
func (m *OCSPManager) GetResponseBytes(cert *x509.Certificate, issuer *x509.Certificate) ([]byte, error) {
	resp, err := m.GetResponse(cert, issuer)
	if err != nil {
		return nil, err
	}
	return resp.Raw, nil
}

// fetchResponse fetches a fresh OCSP response from the responder.
func (m *OCSPManager) fetchResponse(cert *x509.Certificate, issuer *x509.Certificate) (*OCSPResponse, error) {
	if len(cert.OCSPServer) == 0 {
		return nil, errors.New("certificate has no OCSP responders")
	}

	// Build OCSP request
	opts := &ocsp.RequestOptions{
		Hash: crypto.SHA256,
	}

	requestBody, err := ocsp.CreateRequest(cert, issuer, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCSP request: %w", err)
	}

	// Try each responder
	var lastErr error
	for _, responderURL := range cert.OCSPServer {
		resp, err := m.queryResponder(responderURL, requestBody)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("all OCSP responders failed: %w", lastErr)
}

// queryResponder queries a single OCSP responder.
func (m *OCSPManager) queryResponder(responderURL string, requestBody []byte) (*OCSPResponse, error) {
	// Try POST first
	req, err := http.NewRequest("POST", responderURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/ocsp-request")
	req.Header.Set("Accept", "application/ocsp-response")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try GET fallback
		return m.queryResponderGET(responderURL, requestBody)
	}

	return m.parseResponse(resp.Body)
}

// queryResponderGET queries using GET method (RFC 6960 Appendix A.1).
func (m *OCSPManager) queryResponderGET(responderURL string, requestBody []byte) (*OCSPResponse, error) {
	// Base64 encode the request
	encoded := base64.StdEncoding.EncodeToString(requestBody)
	urlWithParam := fmt.Sprintf("%s/%s", responderURL, encoded)

	resp, err := m.httpClient.Get(urlWithParam)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OCSP responder returned %d", resp.StatusCode)
	}

	return m.parseResponse(resp.Body)
}

// parseResponse parses the OCSP response.
func (m *OCSPManager) parseResponse(body io.Reader) (*OCSPResponse, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	parsed, err := ocsp.ParseResponse(data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OCSP response: %w", err)
	}

	return &OCSPResponse{
		Raw:        data,
		Parsed:     parsed,
		CachedAt:   time.Now(),
		NextUpdate: parsed.NextUpdate,
		ThisUpdate: parsed.ThisUpdate,
	}, nil
}

// HasMustStaple checks if a certificate has the OCSP Must-Staple extension.
func HasMustStaple(cert *x509.Certificate) bool {
	// TLS Feature extension (OID: 1.3.6.1.5.5.7.1.24)
	// Contains the "status_request" feature (value 5)
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidTLSFeature) {
			// Check if it contains status_request (5)
			return bytes.Contains(ext.Value, []byte{0x05})
		}
	}
	return false
}

// oidTLSFeature is the OID for the TLS Feature extension.
var oidTLSFeature = []int{1, 3, 6, 1, 5, 5, 7, 1, 24}

// fingerprint generates a SHA-256 fingerprint of a certificate.
func fingerprint(cert *x509.Certificate) string {
	if cert == nil || len(cert.Raw) == 0 {
		return ""
	}
	h := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("%x", h[:])
}

// GetCacheStats returns cache statistics.
func (m *OCSPManager) GetCacheStats() (total int, valid int, expired int) {
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()

	total = len(m.cache)
	for _, resp := range m.cache {
		if resp.IsValid() {
			valid++
		} else {
			expired++
		}
	}
	return
}

// ClearCache clears the OCSP cache.
func (m *OCSPManager) ClearCache() {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	m.cache = make(map[string]*OCSPResponse)
}

// ParseOCSPResponse parses an OCSP response without verification.
func ParseOCSPResponse(data []byte) (*ocsp.Response, error) {
	return ocsp.ParseResponse(data, nil)
}

// CreateOCSPRequest creates an OCSP request for a certificate.
func CreateOCSPRequest(cert *x509.Certificate, issuer *x509.Certificate) ([]byte, error) {
	opts := &ocsp.RequestOptions{
		Hash: crypto.SHA256,
	}
	return ocsp.CreateRequest(cert, issuer, opts)
}

// EncodeOCSPRequest encodes an OCSP request to PEM.
func EncodeOCSPRequest(request []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "OCSP REQUEST",
		Bytes: request,
	})
}

// EncodeOCSPResponse encodes an OCSP response to PEM.
func EncodeOCSPResponse(response []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "OCSP RESPONSE",
		Bytes: response,
	})
}
