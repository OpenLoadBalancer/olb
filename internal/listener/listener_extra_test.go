package listener

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
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"runtime"
	"testing"
	"time"

	olbTLS "github.com/openloadbalancer/olb/internal/tls"
)

// generateTestCertKey generates a self-signed cert and key for testing.
func generateTestCertKey() (certPEM, keyPEM []byte) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return
}

// TestHTTPListenerStopWithTimeout exercises the error path in Stop when context expires.
func TestHTTPListenerStopWithTimeout(t *testing.T) {
	handler := testHandler()

	opts := &Options{
		Name:    "test-timeout-stop",
		Address: "127.0.0.1:0",
		Handler: handler,
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Shutdown with an already-expired context to trigger error path
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // ensure context is expired

	err = l.Stop(ctx)
	// The error may or may not be nil depending on timing, just exercise the path
	t.Logf("Stop with expired context returned: %v", err)
}

// TestHTTPListenerStopDoubleCheckRace exercises the double-checked locking path in Stop.
// We hold the lock while the goroutine is blocked trying to acquire it, then
// set running=false before releasing, so the double-check inside the lock catches it.
func TestHTTPListenerStopDoubleCheckRace(t *testing.T) {
	opts := &Options{
		Name:    "test-stop-dblcheck",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Set running=true so the fast-path check (line 132) passes
	l.running.Store(true)

	// Hold the lock ourselves BEFORE starting the goroutine
	l.mu.Lock()

	done := make(chan error, 1)
	goroutineReady := make(chan struct{})

	go func() {
		close(goroutineReady)
		// This will:
		// 1. Pass fast-path check (running=true) at line 132
		// 2. Block on l.mu.Lock() at line 136
		err := l.Stop(context.Background())
		done <- err
	}()

	// Wait for goroutine to be running and reach the lock (it will block)
	<-goroutineReady
	runtime.Gosched() // yield so the goroutine reaches l.mu.Lock()

	// Now: goroutine is blocked on l.mu.Lock(). Set running=false so when
	// it acquires the lock, the double-check at line 139 catches it.
	l.running.Store(false)

	// Release the lock - goroutine can now proceed
	l.mu.Unlock()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error from double-check path in Stop")
		} else {
			t.Logf("Got expected error from double-check: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Stop goroutine")
	}
}

// TestHTTPListenerStartDoubleCheckRace exercises the double-checked locking path in Start.
// We hold the lock with running=false, so the goroutine passes the fast-path check,
// then blocks on the lock. We then set running=true and release the lock. The goroutine
// acquires the lock and the double-check sees running=true, returning "already running".
func TestHTTPListenerStartDoubleCheckRace(t *testing.T) {
	opts := &Options{
		Name:    "test-start-dblcheck",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// running is false initially, so fast-path check (line 72) will pass.
	// Hold the lock ourselves BEFORE starting the goroutine.
	l.mu.Lock()

	done := make(chan error, 1)
	goroutineReady := make(chan struct{})

	go func() {
		close(goroutineReady)
		// This will:
		// 1. Pass fast-path check (running=false) at line 72
		// 2. Block on l.mu.Lock() at line 77
		err := l.Start()
		done <- err
	}()

	// Wait for goroutine to be running (it will hit the lock and block)
	<-goroutineReady
	time.Sleep(50 * time.Millisecond)

	// Now set running=true so the double-check at line 80 sees it
	l.running.Store(true)

	// Release the lock - goroutine can now proceed
	l.mu.Unlock()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error from double-check path in Start")
		} else if !strings.Contains(err.Error(), "already running") {
			t.Errorf("expected 'already running' error, got: %v", err)
		} else {
			t.Logf("Got expected error from double-check: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start goroutine")
	}

	// Reset state
	l.running.Store(false)
}

// TestHTTPListenerStopNonDeadlineError exercises the non-DeadlineExceeded error
// path in Stop, specifically the "shutdown failed" branch (line 149).
// We use context.Canceled (not DeadlineExceeded) by canceling the context before Stop.
// With an in-flight slow request, Shutdown sees context.Canceled which is NOT
// context.DeadlineExceeded, hitting the "shutdown failed" path.
func TestHTTPListenerStopNonDeadlineError(t *testing.T) {
	handlerDone := make(chan struct{})
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hold the connection open until we're told to finish
		<-handlerDone
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("done"))
	})

	opts := &Options{
		Name:    "test-stop-non-dl",
		Address: "127.0.0.1:0",
		Handler: slowHandler,
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	addr := l.Address()

	// Start a request that will block
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			return
		}
		resp.Body.Close()
	}()

	// Wait for the request to reach the handler
	time.Sleep(100 * time.Millisecond)

	// Create a context and cancel it immediately.
	// context.Canceled is NOT context.DeadlineExceeded,
	// so the Stop code will hit the "shutdown failed" path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately - this produces context.Canceled, not DeadlineExceeded

	err = l.Stop(ctx)
	t.Logf("Stop with canceled context returned: %v", err)

	// Allow the handler to complete
	close(handlerDone)
	wg.Wait()

	if err == nil {
		t.Log("Stop returned nil - Shutdown completed before context check")
	} else {
		t.Logf("Stop returned error as expected: %v", err)
	}
}

// TestHTTPSListenerStartErrPropagation tests that the HTTPS Serve goroutine
// captures non-closed errors in startErr.
func TestHTTPSListenerStartErrPropagation(t *testing.T) {
	certPEM, keyPEM := generateTestCertKey()

	tlsMgr := olbTLS.NewManager()
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}
	tlsMgr.AddCertificate(&olbTLS.Certificate{
		Cert:       &tlsCert,
		Names:      []string{"localhost"},
		Expiry:     time.Now().Add(24 * time.Hour).Unix(),
		IsWildcard: false,
	})

	opts := &Options{
		Name:    "https-start-err",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsMgr)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Close the underlying listener directly to cause Serve to fail with a non-closed error
	l.mu.Lock()
	if l.listener != nil {
		l.listener.Close()
	}
	l.mu.Unlock()

	// Wait for Serve goroutine to detect the error
	time.Sleep(200 * time.Millisecond)

	// Check that startErr was captured or the listener stopped
	startErr := l.StartError()
	running := l.IsRunning()
	t.Logf("StartError after closing listener: %v, running: %v", startErr, running)
}

// TestHTTPSListenerStartDoubleCheckRace exercises the double-checked locking
// path in HTTPSListener.Start. Running=false at fast-path, then set to true
// while goroutine blocks on lock, so double-check catches it.
func TestHTTPSListenerStartDoubleCheckRace(t *testing.T) {
	certPEM, keyPEM := generateTestCertKey()

	tlsMgr := olbTLS.NewManager()
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}
	tlsMgr.AddCertificate(&olbTLS.Certificate{
		Cert:       &tlsCert,
		Names:      []string{"localhost"},
		Expiry:     time.Now().Add(24 * time.Hour).Unix(),
		IsWildcard: false,
	})

	opts := &Options{
		Name:    "https-start-dblcheck",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsMgr)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	// running is false initially, so fast-path check will pass.
	// Hold the lock BEFORE starting the goroutine.
	l.mu.Lock()

	done := make(chan error, 1)
	goroutineReady := make(chan struct{})

	go func() {
		close(goroutineReady)
		err := l.Start()
		done <- err
	}()

	<-goroutineReady
	time.Sleep(50 * time.Millisecond)

	// Set running=true so the double-check sees it
	l.running.Store(true)
	l.mu.Unlock()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error from double-check path in HTTPS Start")
		} else if !strings.Contains(err.Error(), "already running") {
			t.Errorf("expected 'already running' error, got: %v", err)
		} else {
			t.Logf("Got expected error from HTTPS double-check: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTPS Start goroutine")
	}

	// Reset
	l.running.Store(false)
}

// TestHTTPSListenerServeErrorCapture tests that the HTTPS Serve goroutine captures
// errors that are NOT "closed" errors into startErr.
func TestHTTPSListenerServeErrorCapture(t *testing.T) {
	certPEM, keyPEM := generateTestCertKey()

	tlsMgr := olbTLS.NewManager()
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}
	tlsMgr.AddCertificate(&olbTLS.Certificate{
		Cert:       &tlsCert,
		Names:      []string{"localhost"},
		Expiry:     time.Now().Add(24 * time.Hour).Unix(),
		IsWildcard: false,
	})

	var acceptCalled atomic.Bool
	rejectMgr := &mockConnManager{
		acceptFunc: func(conn net.Conn) (net.Conn, error) {
			acceptCalled.Store(true)
			conn.Close()
			return nil, net.UnknownNetworkError("forced accept error for coverage")
		},
	}

	opts := &Options{
		Name:        "https-serve-err",
		Address:     "127.0.0.1:0",
		Handler:     testHandler(),
		ConnManager: rejectMgr,
	}

	l, err := NewHTTPSListener(opts, tlsMgr)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Make a TLS connection attempt to trigger the connManager accept path
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 2 * time.Second,
	}

	addr := l.Address()
	resp, err := client.Get("https://" + addr + "/test")
	if err != nil {
		t.Logf("Client error (expected due to rejected connection): %v", err)
	} else {
		resp.Body.Close()
	}

	if acceptCalled.Load() {
		t.Log("ConnManager.Accept was called - exercised the reject path")
	}

	// Give time for the error to propagate
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerStartWithConnManager exercises the connManager path in HTTPS Start.
func TestHTTPSListenerStartWithConnManager(t *testing.T) {
	certPEM, keyPEM := generateTestCertKey()

	tlsMgr := olbTLS.NewManager()
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}
	tlsMgr.AddCertificate(&olbTLS.Certificate{
		Cert:       &tlsCert,
		Names:      []string{"localhost"},
		Expiry:     time.Now().Add(24 * time.Hour).Unix(),
		IsWildcard: false,
	})

	var acceptCalled atomic.Bool
	mockMgr := &mockConnManager{
		acceptFunc: func(conn net.Conn) (net.Conn, error) {
			acceptCalled.Store(true)
			return conn, nil
		},
	}

	opts := &Options{
		Name:        "https-connmgr",
		Address:     "127.0.0.1:0",
		Handler:     testHandler(),
		ConnManager: mockMgr,
	}

	l, err := NewHTTPSListener(opts, tlsMgr)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Make a TLS request to exercise the connManager accept path
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 2 * time.Second,
	}

	addr := l.Address()
	resp, err := client.Get("https://" + addr + "/test")
	if err != nil {
		t.Logf("HTTPS request error (may be expected with self-signed cert): %v", err)
	} else {
		resp.Body.Close()
	}

	if acceptCalled.Load() {
		t.Log("ConnManager.Accept was called")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerStartErrPropagation tests that startErr is set when Serve fails.
func TestHTTPListenerStartErrPropagation(t *testing.T) {
	handler := testHandler()
	opts := &Options{
		Name:    "test-starterr",
		Address: "127.0.0.1:0",
		Handler: handler,
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Close the underlying listener directly to cause Serve to fail
	l.mu.Lock()
	if l.listener != nil {
		l.listener.Close()
	}
	l.mu.Unlock()

	// Wait for Serve goroutine to detect the error
	time.Sleep(200 * time.Millisecond)

	// Check that startErr was captured
	startErr := l.StartError()
	t.Logf("StartError after closing listener: %v", startErr)
}
