package engine

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

func writeSelfSignedPEM(t *testing.T, dir, cn string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, cn+".crt")
	keyFile = filepath.Join(dir, cn+".key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// protocol: https used to serve plain HTTP unless tls.enabled was also set,
// and a listener's own cert_file/key_file were validated but never loaded.
func TestHTTPSListener_ProtocolImpliesTLSWithListenerCert(t *testing.T) {
	certFile, keyFile := writeSelfSignedPEM(t, t.TempDir(), "listener.openloadbalancer.dev")

	cfg := createTestConfig()
	cfg.Listeners[0].Protocol = "https"
	cfg.Listeners[0].TLS = &config.ListenerTLS{CertFile: certFile, KeyFile: keyFile}

	eng, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := eng.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	addr := eng.listeners[0].Address()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr,
		&tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- test, self-signed
	if err != nil {
		t.Fatalf("TLS handshake with the https listener failed (served plain HTTP?): %v", err)
	}
	defer conn.Close()
	if got := conn.ConnectionState().PeerCertificates[0].Subject.CommonName; got != "listener.openloadbalancer.dev" {
		t.Errorf("served certificate CN = %q, want the listener's own certificate", got)
	}
}
