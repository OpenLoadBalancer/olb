package acme

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	challengePrefix = "/.well-known/acme-challenge/"

	// renewBefore is how long before expiry a certificate is renewed.
	renewBefore = 30 * 24 * time.Hour

	checkInterval = 12 * time.Hour
	retryInterval = time.Hour
)

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	DirectoryURL string   // ACME directory; Let's Encrypt production when empty
	Email        string   // account contact
	Domains      []string // names on the certificate
	StorageDir   string   // account key, certificate and private key live here

	// Install receives each new or reloaded certificate chain and key (PEM).
	Install func(certPEM, keyPEM []byte) error

	// Log reports progress and failures; optional.
	Log func(msg string, err error)

	// ChallengeTimeout bounds each authorization and order poll (default 2m).
	ChallengeTimeout time.Duration
}

// Manager obtains and renews a certificate through ACME HTTP-01.
//
// The HTTP-01 token responses are served by Wrap, which must sit in front of
// the listener on port 80 (the CA validates over plain HTTP on port 80).
type Manager struct {
	cfg ManagerConfig

	mu     sync.Mutex
	client *Client

	tokens sync.Map // token -> key authorization

	notAfter time.Time // of the installed certificate (guarded by mu)

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewManager validates cfg and returns a Manager. It does no network I/O;
// Start begins issuing.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if len(cfg.Domains) == 0 {
		return nil, errors.New("acme: at least one domain is required")
	}
	if cfg.StorageDir == "" {
		return nil, errors.New("acme: storage directory is required")
	}
	if cfg.Install == nil {
		return nil, errors.New("acme: install callback is required")
	}
	if cfg.ChallengeTimeout <= 0 {
		cfg.ChallengeTimeout = 2 * time.Minute
	}
	return &Manager{cfg: cfg, stop: make(chan struct{})}, nil
}

// Start loads a stored certificate if there is one, then issues and renews in
// the background. It returns immediately.
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.run()
}

// Stop ends the background loop and waits for it.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.wg.Wait()
}

// Wrap serves HTTP-01 challenge responses and passes everything else to next.
func (m *Manager) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, challengePrefix) {
			token := strings.TrimPrefix(r.URL.Path, challengePrefix)
			if v, ok := m.tokens.Load(token); ok {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(v.(string)))
				return
			}
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) logf(msg string, err error) {
	if m.cfg.Log != nil {
		m.cfg.Log(msg, err)
	}
}

func (m *Manager) run() {
	defer m.wg.Done()

	m.loadStored()

	for {
		wait := checkInterval
		if m.needsCertificate() {
			if err := m.obtain(); err != nil {
				m.logf("ACME certificate request failed; will retry", err)
				wait = retryInterval
			}
		}
		select {
		case <-m.stop:
			return
		case <-time.After(wait):
		}
	}
}

func (m *Manager) needsCertificate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.notAfter.IsZero() || time.Until(m.notAfter) < renewBefore
}

func (m *Manager) certPath() string { return filepath.Join(m.cfg.StorageDir, "cert.pem") }
func (m *Manager) keyPath() string  { return filepath.Join(m.cfg.StorageDir, "key.pem") }

// loadStored installs the certificate saved by an earlier run, if it still
// covers every configured domain and has not expired.
func (m *Manager) loadStored() {
	certPEM, err := os.ReadFile(m.certPath())
	if err != nil {
		return
	}
	keyPEM, err := os.ReadFile(m.keyPath())
	if err != nil {
		return
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		m.logf("stored ACME certificate is unusable", err)
		return
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || time.Now().After(leaf.NotAfter) {
		return
	}
	for _, d := range m.cfg.Domains {
		if err := leaf.VerifyHostname(d); err != nil {
			return // domain list changed since issuance
		}
	}
	if err := m.cfg.Install(certPEM, keyPEM); err != nil {
		m.logf("installing stored ACME certificate failed", err)
		return
	}
	m.mu.Lock()
	m.notAfter = leaf.NotAfter
	m.mu.Unlock()
	m.logf("loaded stored ACME certificate", nil)
}

// ensureClient creates the ACME client and registers the account once.
func (m *Manager) ensureClient() (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		return m.client, nil
	}

	if err := os.MkdirAll(m.cfg.StorageDir, 0o700); err != nil {
		return nil, err
	}
	key, err := loadOrCreateAccountKey(filepath.Join(m.cfg.StorageDir, "account.key"))
	if err != nil {
		return nil, err
	}

	acfg := DefaultConfig()
	if m.cfg.DirectoryURL != "" {
		acfg.DirectoryURL = m.cfg.DirectoryURL
	}
	if m.cfg.Email != "" {
		acfg.Contact = []string{"mailto:" + m.cfg.Email}
	}
	acfg.AccountKey = key
	client, err := New(acfg)
	if err != nil {
		return nil, err
	}
	// The operator configuring ACME for their domains is the agreement to the
	// CA's terms of service.
	if _, err := client.Register(true); err != nil {
		return nil, fmt.Errorf("register account: %w", err)
	}
	m.client = client
	return client, nil
}

// obtain runs one full order: authorize every domain over HTTP-01, finalize
// with a fresh key, store the result and install it.
func (m *Manager) obtain() error {
	client, err := m.ensureClient()
	if err != nil {
		return err
	}

	order, err := client.CreateOrder(m.cfg.Domains)
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}

	var tokens []string
	defer func() {
		for _, t := range tokens {
			m.tokens.Delete(t)
		}
	}()

	for _, authzURL := range order.Authorizations {
		authz, err := client.GetAuthorization(authzURL)
		if err != nil {
			return fmt.Errorf("get authorization: %w", err)
		}
		if authz.Status == "valid" {
			continue
		}
		ch := authz.GetChallenge("http-01")
		if ch == nil {
			return fmt.Errorf("no http-01 challenge offered for %s", authz.Identifier.Value)
		}
		// RFC 8555 §8.3: the CA fetches /.well-known/acme-challenge/<token>
		// and expects the key authorization "<token>.<account key thumbprint>".
		thumb, err := client.keyThumbprint()
		if err != nil {
			return err
		}
		m.tokens.Store(ch.Token, ch.Token+"."+thumb)
		tokens = append(tokens, ch.Token)

		if err := client.ValidateChallenge(ch); err != nil {
			return fmt.Errorf("validate %s: %w", authz.Identifier.Value, err)
		}
		if _, err := client.PollAuthorization(authzURL, m.cfg.ChallengeTimeout); err != nil {
			return fmt.Errorf("authorization for %s: %w", authz.Identifier.Value, err)
		}
	}

	certKey, err := GeneratePrivateKey()
	if err != nil {
		return err
	}
	csr, err := GenerateCSR(m.cfg.Domains, certKey)
	if err != nil {
		return err
	}
	if err := client.FinalizeOrder(order, csr); err != nil {
		return fmt.Errorf("finalize order: %w", err)
	}
	if order.Status != "valid" {
		if err := client.PollOrder(order, m.cfg.ChallengeTimeout); err != nil {
			return err
		}
	}
	chain, err := client.FetchCertificate(order.Certificate)
	if err != nil {
		return fmt.Errorf("fetch certificate: %w", err)
	}

	var certPEM []byte
	for _, der := range chain {
		certPEM = append(certPEM, EncodeCertificate(der)...)
	}
	keyPEM, err := EncodePrivateKey(certKey)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return err
	}

	// Persist first: a crash after issuance must not lose a rate-limited cert.
	if err := writeFile(m.keyPath(), keyPEM); err != nil {
		return err
	}
	if err := writeFile(m.certPath(), certPEM); err != nil {
		return err
	}
	if err := m.cfg.Install(certPEM, keyPEM); err != nil {
		return fmt.Errorf("install certificate: %w", err)
	}
	m.mu.Lock()
	m.notAfter = leaf.NotAfter
	m.mu.Unlock()
	m.logf("ACME certificate installed, expires "+leaf.NotAfter.Format(time.RFC3339), nil)
	return nil
}

func loadOrCreateAccountKey(path string) (*ecdsa.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("%s: no PEM data", path)
		}
		return x509.ParseECPrivateKey(block.Bytes)
	}
	key, err := GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	pemBytes, err := EncodePrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writeFile(path, pemBytes); err != nil {
		return nil, err
	}
	return key, nil
}

// writeFile writes atomically with owner-only permissions.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
