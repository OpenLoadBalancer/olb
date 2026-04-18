// Package tls provides TLS certificate management for OpenLoadBalancer.
package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Certificate represents a loaded TLS certificate with metadata.
type Certificate struct {
	Cert       *tls.Certificate
	Names      []string // All DNS names from the certificate
	Expiry     int64    // Unix timestamp
	IsWildcard bool
}

// ExpiryAlertFunc is called when a certificate is approaching expiry.
// The function receives the domain names, days until expiry, and the expiry time.
type ExpiryAlertFunc func(names []string, daysUntilExpiry int, expiresAt time.Time)

// Manager manages TLS certificates with support for exact and wildcard matching.
type Manager struct {
	mu sync.RWMutex

	// exactCerts maps exact domain names to certificates
	exactCerts map[string]*Certificate

	// wildcardCerts maps wildcard patterns (e.g., "*.example.com") to certificates
	wildcardCerts map[string]*Certificate

	// defaultCert is returned when no match is found (optional)
	defaultCert *Certificate

	// expiry monitoring
	expiryStop     chan struct{}
	expiryStopOnce sync.Once
	expiryWg       sync.WaitGroup
	expiryAlert    ExpiryAlertFunc
}

// NewManager creates a new TLS certificate manager.
func NewManager() *Manager {
	return &Manager{
		exactCerts:    make(map[string]*Certificate),
		wildcardCerts: make(map[string]*Certificate),
		expiryStop:    make(chan struct{}),
	}
}

// SetExpiryAlert registers a callback for certificate expiry warnings.
// The callback is invoked during periodic checks when a certificate
// is within 30 days of expiry.
func (m *Manager) SetExpiryAlert(fn ExpiryAlertFunc) {
	m.expiryAlert = fn
}

// StartExpiryMonitor begins periodic certificate expiry checking.
// Checks run at the given interval. A reasonable default is 1 hour.
func (m *Manager) StartExpiryMonitor(interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	m.expiryWg.Add(1)
	go m.expiryMonitorLoop(interval)
}

func (m *Manager) expiryMonitorLoop(interval time.Duration) {
	defer m.expiryWg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Check immediately on start
	m.checkExpiry()

	for {
		select {
		case <-m.expiryStop:
			return
		case <-ticker.C:
			m.checkExpiry()
		}
	}
}

// checkExpiry examines all loaded certificates and logs/alerts on approaching expiry.
func (m *Manager) checkExpiry() {
	m.mu.RLock()
	allCerts := make(map[string]*Certificate, len(m.exactCerts)+len(m.wildcardCerts))
	for k, v := range m.exactCerts {
		allCerts[k] = v
	}
	for k, v := range m.wildcardCerts {
		allCerts[k] = v
	}
	m.mu.RUnlock()

	warnThreshold := 30 * 24 * time.Hour // 30 days

	for _, cert := range allCerts {
		expiresAt := time.Unix(cert.Expiry, 0)
		remaining := time.Until(expiresAt)

		if remaining < 0 {
			log.Printf("ALERT: TLS certificate for %v has EXPIRED (%s)", cert.Names, expiresAt.Format(time.RFC3339))
			if m.expiryAlert != nil {
				m.expiryAlert(cert.Names, 0, expiresAt)
			}
		} else if remaining < warnThreshold {
			days := int(remaining.Hours() / 24)
			log.Printf("WARNING: TLS certificate for %v expires in %d days (%s)", cert.Names, days, expiresAt.Format(time.RFC3339))
			if m.expiryAlert != nil {
				m.expiryAlert(cert.Names, days, expiresAt)
			}
		}
	}
}

// StopExpiryMonitor stops the certificate expiry monitoring goroutine.
func (m *Manager) StopExpiryMonitor() {
	m.expiryStopOnce.Do(func() {
		close(m.expiryStop)
	})
	m.expiryWg.Wait()
}

// LoadCertificate loads a certificate from PEM files.
func (m *Manager) LoadCertificate(certFile, keyFile string) (*Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	return m.LoadCertificateFromPEM(certPEM, keyPEM)
}

// LoadCertificateFromPEM loads a certificate from PEM-encoded data.
func (m *Manager) LoadCertificateFromPEM(certPEM, keyPEM []byte) (*Certificate, error) {
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key pair: %w", err)
	}

	// Parse the certificate to extract metadata
	x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Collect all DNS names
	names := make([]string, 0, len(x509Cert.DNSNames)+1)
	if x509Cert.Subject.CommonName != "" {
		names = append(names, x509Cert.Subject.CommonName)
	}
	names = append(names, x509Cert.DNSNames...)

	// Check if any name is a wildcard
	isWildcard := false
	for _, name := range names {
		if strings.HasPrefix(name, "*.") {
			isWildcard = true
			break
		}
	}

	// Warn if certificate is expired or about to expire
	if time.Now().After(x509Cert.NotAfter) {
		log.Printf("WARNING: TLS certificate for %v has expired (expired %s)", names, x509Cert.NotAfter.Format(time.RFC3339))
	} else if time.Until(x509Cert.NotAfter) < 30*24*time.Hour {
		log.Printf("WARNING: TLS certificate for %v expires in %d days (%s)", names, int(time.Until(x509Cert.NotAfter).Hours()/24), x509Cert.NotAfter.Format(time.RFC3339))
	}

	return &Certificate{
		Cert:       &tlsCert,
		Names:      names,
		Expiry:     x509Cert.NotAfter.Unix(),
		IsWildcard: isWildcard,
	}, nil
}

// AddCertificate adds a certificate to the manager.
func (m *Manager) AddCertificate(cert *Certificate) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, name := range cert.Names {
		// Normalize to lowercase for case-insensitive matching
		name = strings.ToLower(name)
		if strings.HasPrefix(name, "*.") {
			m.wildcardCerts[name] = cert
		} else {
			m.exactCerts[name] = cert
		}
	}
}

// SetDefaultCertificate sets the default certificate to return when no match is found.
func (m *Manager) SetDefaultCertificate(cert *Certificate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultCert = cert
}

// GetCertificate returns the best matching certificate for the given SNI.
func (m *Manager) GetCertificate(sni string) *Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Normalize SNI to lowercase
	sni = strings.ToLower(sni)

	// First, try exact match
	if cert, ok := m.exactCerts[sni]; ok {
		return cert
	}

	// Then, try wildcard matches
	// For "sub.example.com", try "*.example.com", then "*.com"
	parts := strings.Split(sni, ".")
	for i := 1; i < len(parts); i++ {
		wildcard := "*." + strings.Join(parts[i:], ".")
		if cert, ok := m.wildcardCerts[wildcard]; ok {
			return cert
		}
	}

	return m.defaultCert
}

// GetCertificateCallback returns a function suitable for tls.Config.GetCertificate.
func (m *Manager) GetCertificateCallback() func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if hello.ServerName == "" {
			// No SNI, return default certificate
			if m.defaultCert != nil {
				return m.defaultCert.Cert, nil
			}
			return nil, fmt.Errorf("no SNI provided and no default certificate configured")
		}

		cert := m.GetCertificate(hello.ServerName)
		if cert == nil {
			return nil, fmt.Errorf("no certificate found for %s", hello.ServerName)
		}

		return cert.Cert, nil
	}
}

// ReloadCertificates reloads all certificates from the given configuration.
// This is used for hot-reloading certificates.
func (m *Manager) ReloadCertificates(certsConfig []CertConfig) error {
	newExactCerts := make(map[string]*Certificate)
	newWildcardCerts := make(map[string]*Certificate)
	var newDefaultCert *Certificate

	for _, cfg := range certsConfig {
		cert, err := m.LoadCertificate(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return fmt.Errorf("failed to load certificate %s: %w", cfg.CertFile, err)
		}

		// Add to appropriate maps
		for _, name := range cert.Names {
			if strings.HasPrefix(name, "*.") {
				newWildcardCerts[name] = cert
			} else {
				newExactCerts[name] = cert
			}
		}

		// Mark as default if specified
		if cfg.IsDefault && newDefaultCert == nil {
			newDefaultCert = cert
		}
	}

	// Atomic swap
	m.mu.Lock()
	m.exactCerts = newExactCerts
	m.wildcardCerts = newWildcardCerts
	m.defaultCert = newDefaultCert
	m.mu.Unlock()

	return nil
}

// BuildTLSConfig creates a tls.Config from the given configuration.
func BuildTLSConfig(minVersion, maxVersion string, cipherSuites []string, preferServerCipherSuites bool) (*tls.Config, error) {
	config := &tls.Config{
		PreferServerCipherSuites: preferServerCipherSuites,
	}

	// Parse min version
	if minVersion != "" {
		v, err := parseTLSVersion(minVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid min_version: %w", err)
		}
		config.MinVersion = v
	} else {
		config.MinVersion = tls.VersionTLS12 // Secure default
	}

	// Parse max version
	if maxVersion != "" {
		v, err := parseTLSVersion(maxVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid max_version: %w", err)
		}
		config.MaxVersion = v
	}

	// Parse cipher suites
	if len(cipherSuites) > 0 {
		suites, err := parseCipherSuites(cipherSuites)
		if err != nil {
			return nil, fmt.Errorf("invalid cipher_suites: %w", err)
		}
		config.CipherSuites = suites
	}

	return config, nil
}

// parseTLSVersion parses a TLS version string.
func parseTLSVersion(version string) (uint16, error) {
	switch strings.ToLower(version) {
	case "1.0", "tls1.0", "tls10":
		return 0, fmt.Errorf("TLS 1.0 is deprecated (RFC 8996) and is not supported")
	case "1.1", "tls1.1", "tls11":
		return 0, fmt.Errorf("TLS 1.1 is deprecated (RFC 8996) and is not supported")
	case "1.2", "tls1.2", "tls12":
		return tls.VersionTLS12, nil
	case "1.3", "tls1.3", "tls13":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unknown TLS version: %s", version)
	}
}

// parseCipherSuites parses cipher suite names.
func parseCipherSuites(names []string) ([]uint16, error) {
	// Map of cipher suite names to their IDs
	cipherSuiteMap := map[string]uint16{
		// TLS 1.3 cipher suites (these are always enabled in Go)
		"TLS_AES_128_GCM_SHA256":       tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":       tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256": tls.TLS_CHACHA20_POLY1305_SHA256,

		// TLS 1.2 cipher suites
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305":    tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305":  tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,

		// Additional TLS 1.2 cipher suites (no forward secrecy — use ECDHE suites instead)
		"TLS_RSA_WITH_AES_128_GCM_SHA256": tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_RSA_WITH_AES_256_GCM_SHA384": tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	}

	noPFS := map[string]bool{
		"TLS_RSA_WITH_AES_128_GCM_SHA256": true,
		"TLS_RSA_WITH_AES_256_GCM_SHA384": true,
	}

	suites := make([]uint16, 0, len(names))
	for _, name := range names {
		id, ok := cipherSuiteMap[name]
		if !ok {
			return nil, fmt.Errorf("unknown cipher suite: %s", name)
		}
		if noPFS[name] {
			log.Printf("WARNING: Cipher suite %s does not provide forward secrecy — prefer ECDHE suites", name)
		}
		suites = append(suites, id)
	}

	return suites, nil
}

// CertConfig represents certificate configuration.
type CertConfig struct {
	CertFile  string
	KeyFile   string
	IsDefault bool
}

// LoadCertificatesFromDirectory loads all certificates from a directory.
// Files named *.crt, *.cert, or *.pem are treated as certificates.
// Key files should have matching names with .key extension.
func (m *Manager) LoadCertificatesFromDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		// Look for certificate files
		if ext == ".crt" || ext == ".cert" || ext == ".pem" {
			certFile := filepath.Join(dir, name)

			// Try to find matching key file
			base := strings.TrimSuffix(name, ext)
			keyFile := filepath.Join(dir, base+".key")

			if _, err := os.Stat(keyFile); err != nil {
				// Try other common extensions
				keyFile = filepath.Join(dir, name+".key")
				if _, err := os.Stat(keyFile); err != nil {
					continue // Skip if no key file found
				}
			}

			cert, err := m.LoadCertificate(certFile, keyFile)
			if err != nil {
				return fmt.Errorf("failed to load %s: %w", certFile, err)
			}

			m.AddCertificate(cert)
		}
	}

	return nil
}

// ListCertificates returns a list of all loaded certificates with their names.
func (m *Manager) ListCertificates() []CertInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[*Certificate]bool)
	var certs []CertInfo

	for _, cert := range m.exactCerts {
		if !seen[cert] {
			seen[cert] = true
			certs = append(certs, CertInfo{
				Names:      cert.Names,
				Expiry:     cert.Expiry,
				IsWildcard: cert.IsWildcard,
			})
		}
	}

	for name, cert := range m.wildcardCerts {
		if !seen[cert] {
			seen[cert] = true
			certs = append(certs, CertInfo{
				Names:      cert.Names,
				Expiry:     cert.Expiry,
				IsWildcard: cert.IsWildcard,
			})
		}
		_ = name // avoid unused warning
	}

	return certs
}

// CertInfo provides information about a loaded certificate.
type CertInfo struct {
	Names      []string
	Expiry     int64
	IsWildcard bool
}
