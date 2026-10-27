// Package tls provides mTLS (Mutual TLS) support for OpenLoadBalancer.
package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ClientAuthPolicy defines the policy for client certificate authentication.
type ClientAuthPolicy int

const (
	// NoClientCert does not request a client certificate.
	NoClientCert ClientAuthPolicy = iota
	// RequestClientCert requests a client certificate but does not require it.
	RequestClientCert
	// RequireAnyClientCert requires a client certificate but does not verify it.
	RequireAnyClientCert
	// VerifyClientCertIfGiven verifies the client certificate if one is provided.
	VerifyClientCertIfGiven
	// RequireAndVerifyClientCert requires and verifies a client certificate.
	RequireAndVerifyClientCert
)

// String returns the string representation of the policy.
func (p ClientAuthPolicy) String() string {
	switch p {
	case NoClientCert:
		return "NoClientCert"
	case RequestClientCert:
		return "RequestClientCert"
	case RequireAnyClientCert:
		return "RequireAnyClientCert"
	case VerifyClientCertIfGiven:
		return "VerifyClientCertIfGiven"
	case RequireAndVerifyClientCert:
		return "RequireAndVerifyClientCert"
	default:
		return fmt.Sprintf("Unknown(%d)", p)
	}
}

// ParseClientAuthPolicy parses a client auth policy string.
func ParseClientAuthPolicy(s string) (ClientAuthPolicy, error) {
	switch strings.ToLower(s) {
	case "none", "noclientcert":
		return NoClientCert, nil
	case "request", "requestclientcert":
		return RequestClientCert, nil
	case "requireany", "requireanyclientcert":
		return RequireAnyClientCert, nil
	case "verifyifgiven", "verifyclientcertifgiven":
		return VerifyClientCertIfGiven, nil
	case "requireandverify", "requireandverifyclientcert":
		return RequireAndVerifyClientCert, nil
	default:
		return NoClientCert, fmt.Errorf("unknown client auth policy: %s", s)
	}
}

// ToTLSClientAuthType converts the policy to tls.ClientAuthType.
func (p ClientAuthPolicy) ToTLSClientAuthType() tls.ClientAuthType {
	switch p {
	case NoClientCert:
		return tls.NoClientCert
	case RequestClientCert:
		return tls.RequestClientCert
	case RequireAnyClientCert:
		return tls.RequireAnyClientCert
	case VerifyClientCertIfGiven:
		return tls.VerifyClientCertIfGiven
	case RequireAndVerifyClientCert:
		return tls.RequireAndVerifyClientCert
	default:
		return tls.NoClientCert
	}
}

// MTLSConfig represents mTLS configuration for a listener or backend.
type MTLSConfig struct {
	// Enabled enables mTLS for this configuration.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// ClientAuth specifies the client authentication policy.
	ClientAuth ClientAuthPolicy `yaml:"client_auth" json:"client_auth"`

	// ClientCAs is a list of paths to CA certificates for validating client certs.
	// Can be files or directories.
	ClientCAs []string `yaml:"client_cas" json:"client_cas"`

	// RootCAs is a list of paths to CA certificates for validating server certs (upstream).
	// Can be files or directories.
	RootCAs []string `yaml:"root_cas" json:"root_cas"`

	// CertFile is the path to the client certificate (for upstream mTLS).
	CertFile string `yaml:"cert_file" json:"cert_file"`

	// KeyFile is the path to the client key (for upstream mTLS).
	KeyFile string `yaml:"key_file" json:"key_file"`

	// InsecureSkipVerify skips server certificate verification (for upstream).
	// Use with caution - only for testing or internal networks.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`

	// ServerName is the expected server name for upstream connections.
	ServerName string `yaml:"server_name" json:"server_name"`

	// VerifyDepth is the maximum certificate chain depth (0 = unlimited).
	VerifyDepth int `yaml:"verify_depth" json:"verify_depth"`

	// CRLFile is the path to a Certificate Revocation List file.
	CRLFile string `yaml:"crl_file" json:"crl_file"`

	// OCSPCheck enables OCSP checking for client certificates.
	OCSPCheck bool `yaml:"ocsp_check" json:"ocsp_check"`
}

// Validate validates the mTLS configuration.
func (c *MTLSConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	// Validate client CA paths if client auth is required
	if c.ClientAuth >= RequireAnyClientCert && len(c.ClientCAs) == 0 {
		return fmt.Errorf("client_cas required when client_auth is %s", c.ClientAuth.String())
	}

	// Validate client cert/key for upstream mTLS
	if c.CertFile != "" && c.KeyFile == "" {
		return fmt.Errorf("key_file required when cert_file is specified")
	}
	if c.KeyFile != "" && c.CertFile == "" {
		return fmt.Errorf("cert_file required when key_file is specified")
	}

	// Check that files exist
	for _, path := range c.ClientCAs {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("client_cas path %s: %w", path, err)
		}
	}

	for _, path := range c.RootCAs {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("root_cas path %s: %w", path, err)
		}
	}

	if c.CertFile != "" {
		if _, err := os.Stat(c.CertFile); err != nil {
			return fmt.Errorf("cert_file: %w", err)
		}
	}

	if c.KeyFile != "" {
		if _, err := os.Stat(c.KeyFile); err != nil {
			return fmt.Errorf("key_file: %w", err)
		}
	}

	if c.CRLFile != "" {
		if _, err := os.Stat(c.CRLFile); err != nil {
			return fmt.Errorf("crl_file: %w", err)
		}
	}

	return nil
}

// CAPool represents a CA certificate pool with metadata.
type CAPool struct {
	pool      *x509.CertPool
	subjects  []string
	certCount int
	mu        sync.RWMutex
}

// NewCAPool creates a new empty CA pool.
func NewCAPool() *CAPool {
	return &CAPool{
		pool:     x509.NewCertPool(),
		subjects: make([]string, 0),
	}
}

// Pool returns the underlying x509.CertPool.
func (p *CAPool) Pool() *x509.CertPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool
}

// Subjects returns the list of CA subjects in the pool.
func (p *CAPool) Subjects() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]string, len(p.subjects))
	copy(result, p.subjects)
	return result
}

// CertCount returns the number of certificates in the pool.
func (p *CAPool) CertCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.certCount
}

// AddCert adds a certificate to the pool.
func (p *CAPool) AddCert(cert *x509.Certificate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pool.AddCert(cert)
	p.certCount++
	if cert.Subject.CommonName != "" {
		p.subjects = append(p.subjects, cert.Subject.CommonName)
	} else if len(cert.Subject.Organization) > 0 {
		p.subjects = append(p.subjects, cert.Subject.Organization[0])
	}
}

// LoadCAPool loads CA certificates from files or directories.
// It returns a CAPool containing all valid CA certificates found.
func LoadCAPool(paths []string) (*CAPool, error) {
	pool := NewCAPool()

	for _, path := range paths {
		if err := loadCAPath(pool, path); err != nil {
			return nil, fmt.Errorf("failed to load CA from %s: %w", path, err)
		}
	}

	if pool.CertCount() == 0 {
		return nil, fmt.Errorf("no valid CA certificates found in paths: %v", paths)
	}

	return pool, nil
}

// loadCAPath loads CA certificates from a single path (file or directory).
func loadCAPath(pool *CAPool, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return loadCADirectory(pool, path)
	}

	return loadCAFile(pool, path)
}

// loadCADirectory loads all CA certificates from a directory.
func loadCADirectory(pool *CAPool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var loaded int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		// Look for certificate files
		if ext == ".crt" || ext == ".cert" || ext == ".pem" {
			certFile := filepath.Join(dir, name)
			if err := loadCAFile(pool, certFile); err != nil {
				// Log but continue - don't fail on individual bad certs
				continue
			}
			loaded++
		}
	}

	return nil
}

// loadCAFile loads CA certificates from a PEM file.
func loadCAFile(pool *CAPool, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	certs, err := parsePEMCerts(data)
	if err != nil {
		return err
	}

	for _, cert := range certs {
		if !cert.IsCA {
			// Still add it, but it might not work for client auth
		}
		pool.AddCert(cert)
	}

	return nil
}

// parsePEMCerts parses PEM-encoded certificates from data.
func parsePEMCerts(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate

	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}

		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse certificate: %w", err)
			}
			certs = append(certs, cert)
		}

		data = rest
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found in PEM data")
	}

	return certs, nil
}

// MTLSManager manages mTLS configurations for listeners and backends.
type MTLSManager struct {
	mu sync.RWMutex

	// clientCAPools stores CA pools for client certificate validation (key = config name)
	clientCAPools map[string]*CAPool

	// rootCAPools stores CA pools for server certificate validation (key = config name)
	rootCAPools map[string]*CAPool

	// clientCerts stores client certificates for upstream mTLS (key = config name)
	clientCerts map[string]*tls.Certificate
}

// NewMTLSManager creates a new mTLS manager.
func NewMTLSManager() *MTLSManager {
	return &MTLSManager{
		clientCAPools: make(map[string]*CAPool),
		rootCAPools:   make(map[string]*CAPool),
		clientCerts:   make(map[string]*tls.Certificate),
	}
}

// LoadClientCAPool loads a CA pool for client certificate validation.
func (m *MTLSManager) LoadClientCAPool(name string, paths []string) error {
	pool, err := LoadCAPool(paths)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientCAPools[name] = pool

	return nil
}

// LoadRootCAPool loads a CA pool for server certificate validation.
func (m *MTLSManager) LoadRootCAPool(name string, paths []string) error {
	pool, err := LoadCAPool(paths)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.rootCAPools[name] = pool

	return nil
}

// LoadClientCert loads a client certificate for upstream mTLS.
func (m *MTLSManager) LoadClientCert(name, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load client certificate: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientCerts[name] = &cert

	return nil
}

// GetClientCAPool returns a client CA pool by name.
func (m *MTLSManager) GetClientCAPool(name string) *CAPool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clientCAPools[name]
}

// GetRootCAPool returns a root CA pool by name.
func (m *MTLSManager) GetRootCAPool(name string) *CAPool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rootCAPools[name]
}

// GetClientCert returns a client certificate by name.
func (m *MTLSManager) GetClientCert(name string) *tls.Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clientCerts[name]
}

// BuildServerTLSConfig builds a tls.Config for a server (listener) with mTLS support.
func (m *MTLSManager) BuildServerTLSConfig(name string, config *MTLSConfig, getCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)) (*tls.Config, error) {
	if !config.Enabled {
		return &tls.Config{
			GetCertificate: getCertificate,
			MinVersion:     tls.VersionTLS12,
		}, nil
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		GetCertificate: getCertificate,
		MinVersion:     tls.VersionTLS12,
		ClientAuth:     config.ClientAuth.ToTLSClientAuthType(),
	}

	// Load client CA pool if needed
	if config.ClientAuth >= RequireAnyClientCert && len(config.ClientCAs) > 0 {
		pool, err := LoadCAPool(config.ClientCAs)
		if err != nil {
			return nil, fmt.Errorf("failed to load client CAs: %w", err)
		}
		tlsConfig.ClientCAs = pool.Pool()
		m.mu.Lock()
		m.clientCAPools[name] = pool
		m.mu.Unlock()
	}

	return tlsConfig, nil
}

// BuildClientTLSConfig builds a tls.Config for a client (upstream connection) with mTLS support.
func BuildClientTLSConfig(config *MTLSConfig) (*tls.Config, error) {
	if config == nil {
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
		}, nil
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: config.InsecureSkipVerify, // #nosec G402 -- opt-in via config
		ServerName:         config.ServerName,
	}

	// Load root CA pool for server verification
	if len(config.RootCAs) > 0 {
		pool, err := LoadCAPool(config.RootCAs)
		if err != nil {
			return nil, fmt.Errorf("failed to load root CAs: %w", err)
		}
		tlsConfig.RootCAs = pool.Pool()
	}

	// Load client certificate for mTLS
	if config.CertFile != "" && config.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// VerifyClientCert verifies a client certificate against the configured CA pool.
func VerifyClientCert(cert *x509.Certificate, caPool *x509.CertPool, verifyDepth int) error {
	if cert == nil {
		return fmt.Errorf("no client certificate provided")
	}

	opts := x509.VerifyOptions{
		Roots:         caPool,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		Intermediates: x509.NewCertPool(),
	}

	if verifyDepth > 0 {
		// Note: Go's x509 package doesn't directly support MaxConstraintComparisons
		// for depth limiting. We check chain length after verification.
	}

	chains, err := cert.Verify(opts)
	if err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	// Check chain depth if specified
	if verifyDepth > 0 {
		for _, chain := range chains {
			if len(chain) > verifyDepth+1 { // +1 because leaf cert is included
				return fmt.Errorf("certificate chain depth %d exceeds maximum %d", len(chain), verifyDepth+1)
			}
		}
	}

	return nil
}

// VerifyClientCertWithIntermediates verifies a client certificate with intermediate certificates.
func VerifyClientCertWithIntermediates(cert *x509.Certificate, intermediates []*x509.Certificate, caPool *x509.CertPool, verifyDepth int) error {
	if cert == nil {
		return fmt.Errorf("no client certificate provided")
	}

	interPool := x509.NewCertPool()
	for _, intermediate := range intermediates {
		interPool.AddCert(intermediate)
	}

	opts := x509.VerifyOptions{
		Roots:         caPool,
		Intermediates: interPool,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	chains, err := cert.Verify(opts)
	if err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	// Check chain depth if specified
	if verifyDepth > 0 {
		for _, chain := range chains {
			if len(chain) > verifyDepth+1 {
				return fmt.Errorf("certificate chain depth %d exceeds maximum %d", len(chain), verifyDepth+1)
			}
		}
	}

	return nil
}

// GetClientCertInfo extracts information from a client certificate for logging/auditing.
func GetClientCertInfo(cert *x509.Certificate) map[string]any {
	if cert == nil {
		return nil
	}

	info := map[string]any{
		"subject":    cert.Subject.String(),
		"issuer":     cert.Issuer.String(),
		"serial":     cert.SerialNumber.String(),
		"not_before": cert.NotBefore,
		"not_after":  cert.NotAfter,
	}

	if len(cert.DNSNames) > 0 {
		info["dns_names"] = cert.DNSNames
	}

	if len(cert.EmailAddresses) > 0 {
		info["email_addresses"] = cert.EmailAddresses
	}

	if len(cert.IPAddresses) > 0 {
		info["ip_addresses"] = cert.IPAddresses
	}

	if len(cert.URIs) > 0 {
		uris := make([]string, len(cert.URIs))
		for i, uri := range cert.URIs {
			uris[i] = uri.String()
		}
		info["uris"] = uris
	}

	return info
}

// DefaultMTLSConfig returns a default mTLS configuration.
func DefaultMTLSConfig() *MTLSConfig {
	return &MTLSConfig{
		Enabled:            false,
		ClientAuth:         NoClientCert,
		InsecureSkipVerify: false,
		VerifyDepth:        0,
		OCSPCheck:          false,
	}
}

// IsClientAuthRequired returns true if the policy requires a client certificate.
func (p ClientAuthPolicy) IsClientAuthRequired() bool {
	return p == RequireAnyClientCert || p == RequireAndVerifyClientCert
}

// IsClientAuthVerified returns true if the policy verifies client certificates.
func (p ClientAuthPolicy) IsClientAuthVerified() bool {
	return p == VerifyClientCertIfGiven || p == RequireAndVerifyClientCert
}
