package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCA is a minimal in-process ACME server: it accepts any JWS, checks the
// HTTP-01 response by fetching it from the challenge server, and signs the CSR.
type fakeCA struct {
	t         *testing.T
	srv       *httptest.Server
	caKey     *ecdsa.PrivateKey
	caCert    *x509.Certificate
	challenge string // base URL that serves /.well-known/acme-challenge/

	mu       sync.Mutex
	valid    bool
	certPEM  []byte
	finished bool
	orders   int
}

func newFakeCA(t *testing.T) *fakeCA {
	t.Helper()
	f := &fakeCA{t: t}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	f.caKey = key
	f.caCert, _ = x509.ParseCertificate(der)

	mux := http.NewServeMux()
	nonce := func(w http.ResponseWriter) { w.Header().Set("Replay-Nonce", "n"+time.Now().String()) }
	mux.HandleFunc("/dir", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"newNonce": f.srv.URL + "/nonce", "newAccount": f.srv.URL + "/acct", "newOrder": f.srv.URL + "/order",
		})
	})
	mux.HandleFunc("/nonce", func(w http.ResponseWriter, r *http.Request) { nonce(w) })
	mux.HandleFunc("/acct", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		w.Header().Set("Location", f.srv.URL+"/acct/1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"valid"}`))
	})
	mux.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		f.mu.Lock()
		f.orders++
		f.mu.Unlock()
		w.Header().Set("Location", f.srv.URL+"/order/1")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.order())
	})
	mux.HandleFunc("/order/1", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		_ = json.NewEncoder(w).Encode(f.order())
	})
	mux.HandleFunc("/authz/1", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		f.mu.Lock()
		status := "pending"
		if f.valid {
			status = "valid"
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status, "identifier": map[string]string{"type": "dns", "value": "example.test"},
			"challenges": []map[string]string{{"type": "http-01", "url": f.srv.URL + "/chal/1", "token": "tok123", "status": "pending"}},
		})
	})
	mux.HandleFunc("/chal/1", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		resp, err := http.Get(f.challenge + "/.well-known/acme-challenge/tok123")
		ok := false
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			ok = resp.StatusCode == 200 && strings.HasPrefix(string(body), "tok123.") && len(body) > len("tok123.")
		}
		f.mu.Lock()
		f.valid = ok
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"type": "http-01", "url": f.srv.URL + "/chal/1", "token": "tok123", "status": "pending"})
	})
	mux.HandleFunc("/fin", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		var jws struct{ Payload string }
		_ = json.NewDecoder(r.Body).Decode(&jws)
		raw, _ := base64.RawURLEncoding.DecodeString(jws.Payload)
		var p struct{ CSR string }
		_ = json.Unmarshal(raw, &p)
		csrDER, _ := base64.RawURLEncoding.DecodeString(p.CSR)
		csr, err := x509.ParseCertificateRequest(csrDER)
		if err != nil {
			f.t.Errorf("bad CSR: %v", err)
			http.Error(w, "bad csr", 400)
			return
		}
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(2), Subject: csr.Subject, DNSNames: csr.DNSNames,
			NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(90 * 24 * time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, _ := x509.CreateCertificate(rand.Reader, leaf, f.caCert, csr.PublicKey, f.caKey)
		f.mu.Lock()
		f.certPEM = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.caCert.Raw})...)
		f.finished = true
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(f.order())
	})
	mux.HandleFunc("/cert/1", func(w http.ResponseWriter, r *http.Request) {
		nonce(w)
		f.mu.Lock()
		defer f.mu.Unlock()
		_, _ = w.Write(f.certPEM)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCA) order() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := map[string]any{
		"status": "pending", "identifiers": []map[string]string{{"type": "dns", "value": "example.test"}},
		"authorizations": []string{f.srv.URL + "/authz/1"}, "finalize": f.srv.URL + "/fin",
	}
	if f.finished {
		o["status"] = "valid"
		o["certificate"] = f.srv.URL + "/cert/1"
	}
	return o
}

func TestManager_ObtainsStoresAndReloadsCertificate(t *testing.T) {
	ca := newFakeCA(t)
	dir := t.TempDir()

	var mu sync.Mutex
	var installed [][]byte
	newMgr := func() *Manager {
		m, err := NewManager(ManagerConfig{
			DirectoryURL: ca.srv.URL + "/dir", Email: "ops@example.test",
			Domains: []string{"example.test"}, StorageDir: dir,
			Install: func(certPEM, keyPEM []byte) error {
				mu.Lock()
				installed = append(installed, certPEM)
				mu.Unlock()
				return nil
			},
			ChallengeTimeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	m := newMgr()
	chal := httptest.NewServer(m.Wrap(http.NotFoundHandler()))
	defer chal.Close()
	ca.challenge = chal.URL

	if err := m.obtain(); err != nil {
		t.Fatalf("obtain: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("installed %d certs, want 1", len(installed))
	}
	for _, f := range []string{"cert.pem", "key.pem", "account.key"} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("%s not stored: %v", f, err)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 { // Windows has no Unix modes
			t.Errorf("%s mode %v, want 0600", f, st.Mode().Perm())
		}
	}
	if m.needsCertificate() {
		t.Error("fresh certificate should not need renewal")
	}
	if _, ok := m.tokens.Load("tok123"); ok {
		t.Error("challenge token must be removed after validation")
	}

	// A second manager reuses the stored certificate instead of ordering.
	m2 := newMgr()
	m2.loadStored()
	if len(installed) != 2 || m2.needsCertificate() {
		t.Errorf("stored certificate not reused (installed=%d)", len(installed))
	}
	if ca.orders != 1 {
		t.Errorf("CA saw %d orders, want 1", ca.orders)
	}
}

func TestManager_WrapOnlyServesKnownTokens(t *testing.T) {
	m, _ := NewManager(ManagerConfig{Domains: []string{"a.test"}, StorageDir: t.TempDir(), Install: func(_, _ []byte) error { return nil }})
	m.tokens.Store("abc", "abc.thumb")
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("app")) }))

	for path, want := range map[string]string{
		challengePrefix + "abc":  "abc.thumb",
		challengePrefix + "nope": "404 page not found\n",
		"/":                      "app",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Body.String() != want {
			t.Errorf("%s: got %q, want %q", path, rec.Body.String(), want)
		}
	}
}

func TestNewManager_Validation(t *testing.T) {
	ok := func(_, _ []byte) error { return nil }
	for name, cfg := range map[string]ManagerConfig{
		"no domains": {StorageDir: "x", Install: ok},
		"no storage": {Domains: []string{"a"}, Install: ok},
		"no install": {Domains: []string{"a"}, StorageDir: "x"},
	} {
		if _, err := NewManager(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
