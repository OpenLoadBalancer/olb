// Package acme provides ACME v2 client for Let's Encrypt certificate automation.
package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Directory represents the ACME server directory.
type Directory struct {
	NewNonce   string `json:"newNonce"`
	NewAccount string `json:"newAccount"`
	NewOrder   string `json:"newOrder"`
	NewAuthz   string `json:"newAuthz"`
	RevokeCert string `json:"revokeCert"`
	KeyChange  string `json:"keyChange"`
	Meta       struct {
		TermsOfService          string   `json:"termsOfService"`
		Website                 string   `json:"website"`
		CaaIdentities           []string `json:"caaIdentities"`
		ExternalAccountRequired bool     `json:"externalAccountRequired"`
	} `json:"meta"`
}

// Account represents an ACME account.
type Account struct {
	Status               string   `json:"status"`
	Contact              []string `json:"contact"`
	TermsOfServiceAgreed bool     `json:"termsOfServiceAgreed"`
	Orders               string   `json:"orders"`
	URL                  string   `json:"-"`
}

// Order represents a certificate order.
type Order struct {
	Status         string       `json:"status"`
	Expires        string       `json:"expires"`
	Identifiers    []Identifier `json:"identifiers"`
	Authorizations []string     `json:"authorizations"`
	Finalize       string       `json:"finalize"`
	Certificate    string       `json:"certificate,omitempty"`
	URL            string       `json:"-"`
}

// Identifier represents a certificate identifier.
type Identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Authorization represents a domain authorization.
type Authorization struct {
	Status     string      `json:"status"`
	Expires    string      `json:"expires"`
	Identifier Identifier  `json:"identifier"`
	Challenges []Challenge `json:"challenges"`
	URL        string      `json:"-"`
}

// Challenge represents an ACME challenge.
type Challenge struct {
	Type      string   `json:"type"`
	URL       string   `json:"url"`
	Status    string   `json:"status"`
	Validated string   `json:"validated,omitempty"`
	Token     string   `json:"token"`
	Error     *Problem `json:"error,omitempty"`
}

// Problem represents an ACME error.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func (p *Problem) Error() string {
	return fmt.Sprintf("%s: %s", p.Type, p.Detail)
}

// JWS represents a JSON Web Signature.
type JWS struct {
	Protected string `json:"protected"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// Client is an ACME v2 client.
type Client struct {
	directoryURL string
	directory    *Directory
	accountKey   crypto.Signer
	account      *Account

	httpClient *http.Client
	nonce      string
	nonceMu    sync.Mutex
}

// Config contains ACME client configuration.
type Config struct {
	DirectoryURL string
	Contact      []string
	AccountKey   crypto.Signer
}

// DefaultConfig returns a default ACME configuration.
func DefaultConfig() *Config {
	return &Config{
		DirectoryURL: "https://acme-v02.api.letsencrypt.org/directory",
		Contact:      []string{},
	}
}

// New creates a new ACME client.
func New(config *Config) (*Client, error) {
	if config.DirectoryURL == "" {
		config.DirectoryURL = DefaultConfig().DirectoryURL
	}

	client := &Client{
		directoryURL: config.DirectoryURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}

	// Generate account key if not provided
	if config.AccountKey != nil {
		client.accountKey = config.AccountKey
	} else {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to generate account key: %w", err)
		}
		client.accountKey = key
	}

	// Fetch directory
	if err := client.fetchDirectory(); err != nil {
		return nil, fmt.Errorf("failed to fetch directory: %w", err)
	}

	return client, nil
}

// fetchDirectory fetches the ACME directory.
func (c *Client) fetchDirectory() error {
	resp, err := c.httpClient.Get(c.directoryURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var dir Directory
	if err := json.NewDecoder(resp.Body).Decode(&dir); err != nil {
		return err
	}

	c.directory = &dir
	return nil
}

// Register registers a new account with the ACME server.
func (c *Client) Register(termsAgreed bool) (*Account, error) {
	if c.directory == nil {
		return nil, errors.New("directory not fetched")
	}

	payload := struct {
		TermsOfServiceAgreed bool     `json:"termsOfServiceAgreed"`
		Contact              []string `json:"contact,omitempty"`
	}{
		TermsOfServiceAgreed: termsAgreed,
		Contact:              []string{},
	}

	resp, err := c.postJWS(c.directory.NewAccount, payload, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var account Account
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, err
	}

	account.URL = resp.Header.Get("Location")
	c.account = &account

	return &account, nil
}

// CreateOrder creates a new certificate order.
func (c *Client) CreateOrder(domains []string) (*Order, error) {
	if c.account == nil {
		return nil, errors.New("account not registered")
	}

	identifiers := make([]Identifier, len(domains))
	for i, domain := range domains {
		identifiers[i] = Identifier{Type: "dns", Value: domain}
	}

	payload := struct {
		Identifiers []Identifier `json:"identifiers"`
	}{
		Identifiers: identifiers,
	}

	resp, err := c.postJWS(c.directory.NewOrder, payload, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, c.parseError(resp)
	}

	var order Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return nil, err
	}

	order.URL = resp.Header.Get("Location")
	return &order, nil
}

// GetAuthorization fetches an authorization.
func (c *Client) GetAuthorization(url string) (*Authorization, error) {
	resp, err := c.postJWS(url, "", false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var authz Authorization
	if err := json.NewDecoder(resp.Body).Decode(&authz); err != nil {
		return nil, err
	}

	authz.URL = url
	return &authz, nil
}

// GetChallenge gets a challenge by type from authorization.
func (a *Authorization) GetChallenge(challengeType string) *Challenge {
	for i := range a.Challenges {
		if a.Challenges[i].Type == challengeType {
			return &a.Challenges[i]
		}
	}
	return nil
}

// ValidateChallenge validates a challenge.
func (c *Client) ValidateChallenge(challenge *Challenge) error {
	resp, err := c.postJWS(challenge.URL, struct{}{}, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return json.NewDecoder(resp.Body).Decode(challenge)
}

// PollAuthorization polls an authorization until it's finalized.
func (c *Client) PollAuthorization(authzURL string, timeout time.Duration) (*Authorization, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		authz, err := c.GetAuthorization(authzURL)
		if err != nil {
			return nil, err
		}

		switch authz.Status {
		case "valid":
			return authz, nil
		case "invalid":
			return nil, errors.New("authorization failed")
		case "expired":
			return nil, errors.New("authorization expired")
		}

		time.Sleep(2 * time.Second)
	}

	return nil, errors.New("authorization polling timeout")
}

// FinalizeOrder finalizes an order with a CSR.
func (c *Client) FinalizeOrder(order *Order, csr []byte) error {
	payload := struct {
		CSR string `json:"csr"`
	}{
		CSR: base64.RawURLEncoding.EncodeToString(csr),
	}

	resp, err := c.postJWS(order.Finalize, payload, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return json.NewDecoder(resp.Body).Decode(order)
}

// PollOrder polls an order until it's finalized.
func (c *Client) PollOrder(order *Order, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := c.postJWS(order.URL, "", false)
		if err != nil {
			return err
		}

		err = json.NewDecoder(resp.Body).Decode(order)
		resp.Body.Close()
		if err != nil {
			return err
		}

		switch order.Status {
		case "valid":
			return nil
		case "invalid":
			return errors.New("order failed")
		}

		time.Sleep(2 * time.Second)
	}

	return errors.New("order polling timeout")
}

// FetchCertificate fetches the certificate chain.
func (c *Client) FetchCertificate(certURL string) ([][]byte, error) {
	// Use postJWS for consistent signing — empty string payload per RFC 8555 POST-as-GET
	resp, err := c.postJWS(certURL, "", false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	// Parse PEM certificates
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var certs [][]byte
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		certs = append(certs, block.Bytes)
		data = rest
	}

	return certs, nil
}

// GenerateCSR generates a certificate signing request.
func GenerateCSR(domains []string, key crypto.Signer) ([]byte, error) {
	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		DNSNames: domains,
	}

	return x509.CreateCertificateRequest(rand.Reader, template, key)
}

// GeneratePrivateKey generates a new ECDSA private key.
func GeneratePrivateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// EncodePrivateKey encodes a private key to PEM.
func EncodePrivateKey(key *ecdsa.PrivateKey) ([]byte, error) {
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}), nil
}

// EncodeCertificate encodes a certificate to PEM.
func EncodeCertificate(cert []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	})
}

// GetHTTP01ChallengeResponse computes the key authorization for HTTP-01.
func (c *Client) GetHTTP01ChallengeResponse(token string) (string, error) {
	thumbprint, err := c.keyThumbprint()
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256([]byte(token + "." + thumbprint))
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// keyThumbprint computes the JWK thumbprint of the account key.
func (c *Client) keyThumbprint() (string, error) {
	pub, ok := c.accountKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return "", errors.New("unsupported key type")
	}

	jwk := map[string]any{
		"crv": "P-256",
		"kty": "EC",
		"x":   base64.RawURLEncoding.EncodeToString(pub.X.Bytes()),
		"y":   base64.RawURLEncoding.EncodeToString(pub.Y.Bytes()),
	}

	jwkJSON, err := json.Marshal(jwk)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JWK: %w", err)
	}
	hash := sha256.Sum256(jwkJSON)
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// postJWS makes a POST request with JWS signing.
func (c *Client) postJWS(url string, payload any, newAccount bool) (*http.Response, error) {
	var payloadB64 string
	if str, ok := payload.(string); ok && str == "" {
		// Empty string is a POST-as-GET (RFC 8555 §6.3) — payload is empty b64
		payloadB64 = ""
	} else {
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		payloadB64 = base64.RawURLEncoding.EncodeToString(payloadJSON)
	}

	nonce, err := c.getNonce()
	if err != nil {
		return nil, err
	}

	protected := map[string]any{
		"alg":   "ES256",
		"nonce": nonce,
		"url":   url,
	}

	if newAccount {
		// Use jwk for new account
		pub := c.accountKey.Public().(*ecdsa.PublicKey)
		protected["jwk"] = map[string]any{
			"crv": "P-256",
			"kty": "EC",
			"x":   base64.RawURLEncoding.EncodeToString(pub.X.Bytes()),
			"y":   base64.RawURLEncoding.EncodeToString(pub.Y.Bytes()),
		}
	} else if c.account != nil {
		protected["kid"] = c.account.URL
	}

	protectedJSON, err := json.Marshal(protected)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protected header: %w", err)
	}
	protectedB64 := base64.RawURLEncoding.EncodeToString(protectedJSON)

	signature, err := c.sign([]byte(protectedB64), []byte(payloadB64))
	if err != nil {
		return nil, err
	}

	jws := JWS{
		Protected: protectedB64,
		Payload:   payloadB64,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}

	body, err := json.Marshal(jws)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JWS: %w", err)
	}
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/jose+json")

	return c.httpClient.Do(req)
}

// sign creates a signature for the protected header and payload.
// Per RFC 8555, the signature input is SHA-256(protectedB64 || "." || payloadB64).
func (c *Client) sign(protected, payload []byte) ([]byte, error) {
	// Construct the signature input: protected || "." || payload
	signInput := append(protected, '.')
	signInput = append(signInput, payload...)
	hash := sha256.Sum256(signInput)

	r, s, err := ecdsa.Sign(rand.Reader, c.accountKey.(*ecdsa.PrivateKey), hash[:])
	if err != nil {
		return nil, err
	}

	// Encode signature as ASN.1 DER
	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()

	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)

	return sig, nil
}

// getNonce fetches a new nonce from the ACME server.
func (c *Client) getNonce() (string, error) {
	c.nonceMu.Lock()
	defer c.nonceMu.Unlock()

	if c.nonce != "" {
		nonce := c.nonce
		c.nonce = ""
		return nonce, nil
	}

	if c.directory == nil {
		return "", errors.New("directory not fetched")
	}

	resp, err := c.httpClient.Head(c.directory.NewNonce)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return resp.Header.Get("Replay-Nonce"), nil
}

// parseError parses an ACME error response.
func (c *Client) parseError(resp *http.Response) error {
	// Store nonce if present
	if nonce := resp.Header.Get("Replay-Nonce"); nonce != "" {
		c.nonceMu.Lock()
		c.nonce = nonce
		c.nonceMu.Unlock()
	}

	var problem Problem
	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return &problem
}

// GetTermsOfService returns the terms of service URL.
func (c *Client) GetTermsOfService() string {
	if c.directory == nil {
		return ""
	}
	return c.directory.Meta.TermsOfService
}

// GetAccount returns the current account.
func (c *Client) GetAccount() *Account {
	return c.account
}

// SetAccount sets the account (for loading existing accounts).
func (c *Client) SetAccount(account *Account) {
	c.account = account
}

// GetDirectory returns the ACME directory.
func (c *Client) GetDirectory() *Directory {
	return c.directory
}

// IsStaging returns true if using Let's Encrypt staging.
func (c *Client) IsStaging() bool {
	return strings.Contains(c.directoryURL, "staging")
}
