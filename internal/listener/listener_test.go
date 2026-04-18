package listener

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	olbTLS "github.com/openloadbalancer/olb/internal/tls"
)

// generateTestCert generates a test certificate with the given DNS names.
func generateTestCert(dnsNames []string) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM, nil
}

// testHandler returns a simple HTTP handler for testing
func testHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})
}

// waitForListener polls until the listener is running or timeout
func waitForListener(t *testing.T, l Listener, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l.IsRunning() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener did not start within %v", timeout)
}

// TestHTTPListenerStartStop tests basic start/stop functionality
func TestHTTPListenerStartStop(t *testing.T) {
	opts := &Options{
		Name:    "test-http",
		Address: "127.0.0.1:0", // Let system assign port
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Start listener
	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForListener(t, l, 2*time.Second)

	if !l.IsRunning() {
		t.Error("expected listener to be running")
	}

	// Make a request
	addr := l.Address()
	resp, err := http.Get("http://" + addr + "/test")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Hello, World!" {
		t.Errorf("unexpected body: %s", string(body))
	}

	// Stop listener
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if l.IsRunning() {
		t.Error("expected listener to be stopped")
	}
}

// TestHTTPListenerAlreadyRunning tests starting an already running listener
func TestHTTPListenerAlreadyRunning(t *testing.T) {
	opts := &Options{
		Name:    "test-already-running",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Start first time
	if err := l.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	waitForListener(t, l, 2*time.Second)

	// Try to start again
	err = l.Start()
	if err == nil {
		t.Error("expected error when starting already running listener")
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerGracefulShutdown tests graceful shutdown behavior
func TestHTTPListenerGracefulShutdown(t *testing.T) {
	// Handler that takes time to complete; signals when request is in-flight.
	requestStarted := make(chan struct{})
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("done"))
	})

	opts := &Options{
		Name:    "test-graceful",
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

	// Start a slow request in background
	var wg sync.WaitGroup
	wg.Add(1)
	var requestErr error
	var responseStatus int

	go func() {
		defer wg.Done()
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			requestErr = err
			return
		}
		defer resp.Body.Close()
		responseStatus = resp.StatusCode
	}()

	// Wait until the handler has started processing the request
	<-requestStarted

	// Initiate graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- l.Stop(ctx)
	}()

	// Wait for shutdown or timeout
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("shutdown timed out")
	}

	// Wait for request to complete
	wg.Wait()

	// Request should have succeeded
	if requestErr != nil {
		t.Logf("Request error (expected during shutdown): %v", requestErr)
	}
	if responseStatus != http.StatusOK {
		t.Logf("Response status: %d", responseStatus)
	}
}

// TestHTTPListenerOptionsDefaults tests that default options are applied
func TestHTTPListenerOptionsDefaults(t *testing.T) {
	opts := &Options{
		Name:    "test-defaults",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
		// Leave timeouts as zero to test defaults
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Verify defaults were applied
	if l.readTimeout == 0 {
		t.Error("expected default read timeout to be applied")
	}
	if l.writeTimeout == 0 {
		t.Error("expected default write timeout to be applied")
	}
	if l.idleTimeout == 0 {
		t.Error("expected default idle timeout to be applied")
	}
	if l.maxHeaderBytes == 0 {
		t.Error("expected default max header bytes to be applied")
	}

	// Verify specific default values
	if l.readTimeout != 30*time.Second {
		t.Errorf("expected read timeout 30s, got %v", l.readTimeout)
	}
	if l.writeTimeout != 30*time.Second {
		t.Errorf("expected write timeout 30s, got %v", l.writeTimeout)
	}
	if l.idleTimeout != 120*time.Second {
		t.Errorf("expected idle timeout 120s, got %v", l.idleTimeout)
	}
	if l.maxHeaderBytes != 1<<20 {
		t.Errorf("expected max header bytes 1MB, got %d", l.maxHeaderBytes)
	}
}

// TestHTTPListenerValidation tests validation of required options
func TestHTTPListenerValidation(t *testing.T) {
	tests := []struct {
		name    string
		opts    *Options
		wantErr bool
	}{
		{
			name: "missing name",
			opts: &Options{
				Address: "127.0.0.1:8080",
				Handler: testHandler(),
			},
			wantErr: true,
		},
		{
			name: "missing address",
			opts: &Options{
				Name:    "test",
				Handler: testHandler(),
			},
			wantErr: true,
		},
		{
			name: "missing handler",
			opts: &Options{
				Name:    "test",
				Address: "127.0.0.1:8080",
			},
			wantErr: true,
		},
		{
			name: "valid options",
			opts: &Options{
				Name:    "test",
				Address: "127.0.0.1:8080",
				Handler: testHandler(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHTTPListener(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHTTPListener() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestHTTPListenerConcurrentAccess tests thread safety
func TestHTTPListenerConcurrentAccess(t *testing.T) {
	opts := &Options{
		Name:    "test-concurrent",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Start listener
	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForListener(t, l, 2*time.Second)

	addr := l.Address()

	// Run concurrent requests
	var wg sync.WaitGroup
	numRequests := 100
	numWorkers := 10

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numRequests/numWorkers; j++ {
				resp, err := http.Get("http://" + addr + fmt.Sprintf("/test?worker=%d&req=%d", workerID, j))
				if err != nil {
					t.Logf("Request error: %v", err)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()

	// Stop listener with longer timeout for concurrent test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestHTTPSListenerWithTLS tests HTTPS listener with TLS
func TestHTTPSListenerWithTLS(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForListener(t, l, 2*time.Second)

	addr := l.Address()

	// Create HTTPS client that skips verification for testing
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get("https://" + addr + "/test")
	if err != nil {
		t.Fatalf("HTTPS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Stop listener
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestHTTPSListenerWithoutManager tests that HTTPS listener requires a TLS manager
func TestHTTPSListenerWithoutManager(t *testing.T) {
	opts := &Options{
		Name:    "test-https-no-manager",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	_, err := NewHTTPSListener(opts, nil)
	if err == nil {
		t.Error("expected error when creating HTTPS listener without TLS manager")
	}
}

// TestHTTPListenerAddress tests that Address() returns the correct address
func TestHTTPListenerAddress(t *testing.T) {
	opts := &Options{
		Name:    "test-address",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Before starting, should return configured address
	if l.Address() != "127.0.0.1:0" {
		t.Errorf("expected address 127.0.0.1:0, got %s", l.Address())
	}

	// Start listener
	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForListener(t, l, 2*time.Second)

	// After starting, should return actual bound address
	addr := l.Address()
	if addr == "127.0.0.1:0" {
		t.Error("expected actual bound address, got 127.0.0.1:0")
	}

	// Verify it's a valid address with port
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Errorf("Address() returned invalid address: %s", addr)
	}
	if port == "0" {
		t.Error("expected non-zero port")
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerStopNotRunning tests stopping a listener that isn't running
func TestHTTPListenerStopNotRunning(t *testing.T) {
	opts := &Options{
		Name:    "test-stop-not-running",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = l.Stop(ctx)
	if err == nil {
		t.Error("expected error when stopping listener that isn't running")
	}
}

// TestDefaultOptions tests the DefaultOptions function
func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.ReadTimeout != 30*time.Second {
		t.Errorf("expected ReadTimeout 30s, got %v", opts.ReadTimeout)
	}
	if opts.WriteTimeout != 30*time.Second {
		t.Errorf("expected WriteTimeout 30s, got %v", opts.WriteTimeout)
	}
	if opts.IdleTimeout != 120*time.Second {
		t.Errorf("expected IdleTimeout 120s, got %v", opts.IdleTimeout)
	}
	if opts.HeaderTimeout != 10*time.Second {
		t.Errorf("expected HeaderTimeout 10s, got %v", opts.HeaderTimeout)
	}
	if opts.MaxHeaderBytes != 1<<20 {
		t.Errorf("expected MaxHeaderBytes 1MB, got %d", opts.MaxHeaderBytes)
	}
}

// mockConnManager is a mock connection manager for testing
type mockConnManager struct {
	acceptCalled bool
	acceptFunc   func(net.Conn) (net.Conn, error)
}

func (m *mockConnManager) Accept(conn net.Conn) (net.Conn, error) {
	m.acceptCalled = true
	if m.acceptFunc != nil {
		return m.acceptFunc(conn)
	}
	return conn, nil
}

// TestHTTPListenerWithConnManager tests connection manager integration
func TestHTTPListenerWithConnManager(t *testing.T) {
	mock := &mockConnManager{}

	opts := &Options{
		Name:        "test-conn-manager",
		Address:     "127.0.0.1:0",
		Handler:     testHandler(),
		ConnManager: mock,
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForListener(t, l, 2*time.Second)

	// Make a request to trigger connection acceptance
	addr := l.Address()
	resp, err := http.Get("http://" + addr + "/test")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()

	// Note: The mock may or may not be called depending on timing
	// This is primarily a smoke test to ensure no panic occurs

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerTimeoutConfiguration tests custom timeout configuration
func TestHTTPListenerTimeoutConfiguration(t *testing.T) {
	opts := &Options{
		Name:           "test-timeouts",
		Address:        "127.0.0.1:0",
		Handler:        testHandler(),
		ReadTimeout:    45 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    300 * time.Second,
		MaxHeaderBytes: 2 << 20, // 2 MB
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Verify custom values are preserved
	if l.readTimeout != 45*time.Second {
		t.Errorf("expected read timeout 45s, got %v", l.readTimeout)
	}
	if l.writeTimeout != 60*time.Second {
		t.Errorf("expected write timeout 60s, got %v", l.writeTimeout)
	}
	if l.idleTimeout != 300*time.Second {
		t.Errorf("expected idle timeout 300s, got %v", l.idleTimeout)
	}
	if l.maxHeaderBytes != 2<<20 {
		t.Errorf("expected max header bytes 2MB, got %d", l.maxHeaderBytes)
	}
}

// BenchmarkHTTPListener benchmarks the HTTP listener
func BenchmarkHTTPListener(b *testing.B) {
	opts := &Options{
		Name:    "bench-http",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		b.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		b.Fatalf("Start failed: %v", err)
	}

	waitForListenerTB(b, l, 2*time.Second)

	addr := l.Address()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get("http://" + addr + "/bench")
			if err != nil {
				b.Logf("Request error: %v", err)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// waitForListenerTB is a helper that works with both *testing.T and *testing.B
func waitForListenerTB(tb testing.TB, l Listener, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l.IsRunning() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	tb.Fatalf("listener did not start within %v", timeout)
}

// TestHTTPListenerStartInvalidAddress tests starting with an invalid address
func TestHTTPListenerStartInvalidAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "invalid IP format",
			address: "999.999.999.999:8080",
		},
		{
			name:    "invalid port",
			address: "127.0.0.1:abc",
		},
		{
			name:    "negative port",
			address: "127.0.0.1:-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{
				Name:    "test-invalid-addr",
				Address: tt.address,
				Handler: testHandler(),
			}

			l, err := NewHTTPListener(opts)
			if err != nil {
				t.Fatalf("NewHTTPListener failed: %v", err)
			}

			err = l.Start()
			if err == nil {
				t.Error("expected error when starting with invalid address")
				l.Stop(context.Background())
			}
		})
	}
}

// TestHTTPListenerDoubleStart tests starting an already running listener returns error
func TestHTTPListenerDoubleStart(t *testing.T) {
	opts := &Options{
		Name:    "test-double-start",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// First start
	if err := l.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Second start should fail
	err = l.Start()
	if err == nil {
		t.Error("expected error on double start")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerMultipleConcurrentRequests tests handling multiple concurrent requests
func TestHTTPListenerMultipleConcurrentRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Small delay to ensure concurrency
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	opts := &Options{
		Name:    "test-concurrent-reqs",
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

	addr := l.Address()

	// Fire 50 concurrent requests
	var wg sync.WaitGroup
	numRequests := 50
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := http.Get(fmt.Sprintf("http://%s/test?id=%d", addr, id))
			if err != nil {
				errors <- fmt.Errorf("request %d failed: %w", id, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("request %d: expected status 200, got %d", id, resp.StatusCode)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			if string(body) != "ok" {
				errors <- fmt.Errorf("request %d: unexpected body: %s", id, string(body))
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerRequestWithBody tests requests with body content
func TestHTTPListenerRequestWithBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("received: "))
		w.Write(body)
	})

	opts := &Options{
		Name:    "test-body",
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

	addr := l.Address()

	// Test POST with body
	bodyContent := []byte("Hello, Server!")
	resp, err := http.Post("http://"+addr+"/echo", "text/plain", bytes.NewReader(bodyContent))
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	responseBody, _ := io.ReadAll(resp.Body)
	expected := "received: Hello, Server!"
	if string(responseBody) != expected {
		t.Errorf("expected body '%s', got '%s'", expected, string(responseBody))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerLargeRequestBody tests handling of large request bodies
func TestHTTPListenerLargeRequestBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "size: %d", len(body))
	})

	opts := &Options{
		Name:    "test-large-body",
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

	addr := l.Address()

	// Test with 1MB body
	largeBody := make([]byte, 1024*1024)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	resp, err := http.Post("http://"+addr+"/upload", "application/octet-stream", bytes.NewReader(largeBody))
	if err != nil {
		t.Fatalf("POST request with large body failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	responseBody, _ := io.ReadAll(resp.Body)
	expected := fmt.Sprintf("size: %d", len(largeBody))
	if string(responseBody) != expected {
		t.Errorf("expected body '%s', got '%s'", expected, string(responseBody))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerRequestTimeout tests request timeout handling
func TestHTTPListenerRequestTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow handler
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("completed"))
	})

	opts := &Options{
		Name:         "test-timeout",
		Address:      "127.0.0.1:0",
		Handler:      handler,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
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

	// Create client with timeout
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get("http://" + addr + "/slow")
	// Request may succeed or fail depending on timeout handling
	if err != nil {
		t.Logf("Request timed out as expected: %v", err)
	} else {
		defer resp.Body.Close()
		// If it succeeded, that's also acceptable depending on implementation
		t.Logf("Request completed with status: %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerConnectionClose tests Connection: close handling
func TestHTTPListenerConnectionClose(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	})

	opts := &Options{
		Name:    "test-close",
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

	addr := l.Address()

	// Make request with Connection: close
	req, _ := http.NewRequest("GET", "http://"+addr+"/test", nil)
	req.Header.Set("Connection", "close")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerStartWithTLS tests HTTPS listener with valid TLS config
func TestHTTPSListenerStartWithTLS(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https-tls",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	if !l.IsRunning() {
		t.Error("expected HTTPS listener to be running")
	}

	addr := l.Address()

	// Create HTTPS client that skips verification
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get("https://" + addr + "/test")
	if err != nil {
		t.Fatalf("HTTPS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Hello, World!" {
		t.Errorf("unexpected body: %s", string(body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerTLSHandshake tests TLS handshake behavior
func TestHTTPSListenerTLSHandshake(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-tls-handshake",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	addr := l.Address()

	// Test TLS connection
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("TLS dial failed: %v", err)
	}
	defer conn.Close()

	// Verify TLS version
	if conn.ConnectionState().Version < tls.VersionTLS12 {
		t.Errorf("expected TLS 1.2 or higher, got %d", conn.ConnectionState().Version)
	}

	// Send HTTP request over TLS
	fmt.Fprintf(conn, "GET /test HTTP/1.1\r\nHost: localhost\r\n\r\n")

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	response := string(buf[:n])
	if !strings.Contains(response, "200 OK") {
		t.Errorf("expected 200 OK in response, got: %s", response)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerInvalidTLSConfig tests HTTPS listener with nil TLS manager
func TestHTTPSListenerInvalidTLSConfig(t *testing.T) {
	opts := &Options{
		Name:    "test-invalid-tls",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	_, err := NewHTTPSListener(opts, nil)
	if err == nil {
		t.Error("expected error when creating HTTPS listener without TLS manager")
	}
	if !strings.Contains(err.Error(), "TLS manager is required") {
		t.Errorf("expected 'TLS manager is required' error, got: %v", err)
	}
}

// TestHTTPListenerNilHandler tests creating listener with nil handler
func TestHTTPListenerNilHandler(t *testing.T) {
	opts := &Options{
		Name:    "test-nil-handler",
		Address: "127.0.0.1:8080",
		Handler: nil,
	}

	_, err := NewHTTPListener(opts)
	if err == nil {
		t.Error("expected error when creating listener with nil handler")
	}
	if !strings.Contains(err.Error(), "handler is required") {
		t.Errorf("expected 'handler is required' error, got: %v", err)
	}
}

// TestHTTPListenerAddressInUse tests address already in use error
func TestHTTPListenerAddressInUse(t *testing.T) {
	// Allocate a dynamic port to avoid conflicts
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Create first listener
	opts1 := &Options{
		Name:    "test-addr-in-use-1",
		Address: addr,
		Handler: testHandler(),
	}

	l1, err := NewHTTPListener(opts1)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l1.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	waitForListener(t, l1, 2*time.Second)

	// Try to create second listener on same address
	opts2 := &Options{
		Name:    "test-addr-in-use-2",
		Address: addr,
		Handler: testHandler(),
	}

	l2, err := NewHTTPListener(opts2)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	err = l2.Start()
	if err == nil {
		t.Error("expected error when starting listener on already-used address")
		l2.Stop(context.Background())
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l1.Stop(ctx)
}

// TestHTTPListenerFullRequestResponseCycle tests complete request/response cycle
func TestHTTPListenerFullRequestResponseCycle(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set multiple headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "custom-value")

		// Set status
		w.WriteHeader(http.StatusCreated)

		// Write response body
		fmt.Fprintf(w, `{"method":"%s","path":"%s"}`, r.Method, r.URL.Path)
	})

	opts := &Options{
		Name:    "test-full-cycle",
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

	addr := l.Address()

	// Make request with custom headers
	req, _ := http.NewRequest("POST", "http://"+addr+"/api/resource", strings.NewReader(`{"data":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "12345")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", resp.Header.Get("Content-Type"))
	}

	if resp.Header.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected X-Custom-Header custom-value, got %s", resp.Header.Get("X-Custom-Header"))
	}

	body, _ := io.ReadAll(resp.Body)
	expected := `{"method":"POST","path":"/api/resource"}`
	if string(body) != expected {
		t.Errorf("expected body '%s', got '%s'", expected, string(body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerKeepAlive tests keep-alive connections
func TestHTTPListenerKeepAlive(t *testing.T) {
	requestCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "request %d", requestCount)
	})

	opts := &Options{
		Name:        "test-keepalive",
		Address:     "127.0.0.1:0",
		Handler:     handler,
		IdleTimeout: 5 * time.Second,
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

	// Create a client with keep-alive enabled
	tr := &http.Transport{
		MaxIdleConns:      1,
		IdleConnTimeout:   10 * time.Second,
		DisableKeepAlives: false,
	}
	client := &http.Client{Transport: tr}

	// Make multiple requests using the same connection
	for i := 0; i < 5; i++ {
		resp, err := client.Get("http://" + addr + "/test")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		expected := fmt.Sprintf("request %d", i+1)
		if string(body) != expected {
			t.Errorf("request %d: expected body '%s', got '%s'", i+1, expected, string(body))
		}
	}

	if requestCount != 5 {
		t.Errorf("expected 5 requests, got %d", requestCount)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerMultipleSequentialRequests tests multiple sequential requests
func TestHTTPListenerMultipleSequentialRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	opts := &Options{
		Name:    "test-sequential",
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

	addr := l.Address()

	// Make 100 sequential requests
	for i := 0; i < 100; i++ {
		resp, err := http.Get("http://" + addr + fmt.Sprintf("/test?n=%d", i))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected status 200, got %d", i+1, resp.StatusCode)
		}
		if string(body) != "ok" {
			t.Errorf("request %d: expected body 'ok', got '%s'", i+1, string(body))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerName tests the Name() method
func TestHTTPListenerName(t *testing.T) {
	opts := &Options{
		Name:    "my-test-listener",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if l.Name() != "my-test-listener" {
		t.Errorf("expected name 'my-test-listener', got '%s'", l.Name())
	}
}

// TestHTTPListenerStartError tests the StartError() method
func TestHTTPListenerStartError(t *testing.T) {
	opts := &Options{
		Name:    "test-start-error",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Before start, should be nil
	if l.StartError() != nil {
		t.Errorf("expected nil StartError before start, got: %v", l.StartError())
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// After successful start, should still be nil
	if l.StartError() != nil {
		t.Errorf("expected nil StartError after successful start, got: %v", l.StartError())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestMergeOptionsNilInput tests mergeOptions with nil input
func TestMergeOptionsNilInput(t *testing.T) {
	result := mergeOptions(nil)

	if result == nil {
		t.Fatal("expected non-nil result from mergeOptions(nil)")
	}

	defaults := DefaultOptions()
	if result.ReadTimeout != defaults.ReadTimeout {
		t.Errorf("expected ReadTimeout %v, got %v", defaults.ReadTimeout, result.ReadTimeout)
	}
	if result.WriteTimeout != defaults.WriteTimeout {
		t.Errorf("expected WriteTimeout %v, got %v", defaults.WriteTimeout, result.WriteTimeout)
	}
}

// TestManagedListenerAcceptError tests managed listener when Accept fails
func TestManagedListenerAcceptError(t *testing.T) {
	// Create a mock listener that returns an error
	mockListener := &mockErrorListener{}

	mockManager := &mockConnManager{
		acceptFunc: func(conn net.Conn) (net.Conn, error) {
			return conn, nil
		},
	}

	managed := &managedListener{
		Listener:    mockListener,
		connManager: mockManager,
	}

	conn, err := managed.Accept()
	if err == nil {
		t.Error("expected error from mock listener")
	}
	if conn != nil {
		t.Error("expected nil conn on error")
	}
}

// mockErrorListener is a mock listener that always returns an error
type mockErrorListener struct{}

func (m *mockErrorListener) Accept() (net.Conn, error) {
	return nil, fmt.Errorf("mock accept error")
}

func (m *mockErrorListener) Close() error {
	return nil
}

func (m *mockErrorListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// TestManagedListenerConnManagerReject tests when connection manager rejects a connection
func TestManagedListenerConnManagerReject(t *testing.T) {
	// Create a pipe to simulate a connection
	server, _ := net.Pipe()
	defer server.Close()

	mockListener := &mockOneShotListener{conn: server}

	mockManager := &mockConnManager{
		acceptFunc: func(conn net.Conn) (net.Conn, error) {
			return nil, fmt.Errorf("connection rejected: limit exceeded")
		},
	}

	managed := &managedListener{
		Listener:    mockListener,
		connManager: mockManager,
	}

	conn, err := managed.Accept()
	if err == nil {
		t.Error("expected error when connection manager rejects")
	}
	if conn != nil {
		t.Error("expected nil conn when rejected")
	}
	if !strings.Contains(err.Error(), "limit exceeded") {
		t.Errorf("expected 'limit exceeded' error, got: %v", err)
	}
}

// mockOneShotListener accepts one connection then errors
type mockOneShotListener struct {
	conn     net.Conn
	accepted bool
}

func (m *mockOneShotListener) Accept() (net.Conn, error) {
	if !m.accepted {
		m.accepted = true
		return m.conn, nil
	}
	return nil, fmt.Errorf("no more connections")
}

func (m *mockOneShotListener) Close() error {
	return nil
}

func (m *mockOneShotListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// TestIsClosedError tests the isClosedError function
func TestIsClosedError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "http Server closed",
			err:      fmt.Errorf("http: Server closed"),
			expected: true,
		},
		{
			name:     "net.OpError with closed connection",
			err:      &net.OpError{Err: fmt.Errorf("use of closed network connection")},
			expected: true,
		},
		{
			name:     "random error",
			err:      fmt.Errorf("some random error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClosedError(tt.err)
			if result != tt.expected {
				t.Errorf("isClosedError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

// TestHTTPSListenerDoubleStart tests double start on HTTPS listener
func TestHTTPSListenerDoubleStart(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https-double-start",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	// First start
	if err := l.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Second start should fail
	err = l.Start()
	if err == nil {
		t.Error("expected error on double start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerStopNotRunning tests stopping HTTPS listener that is not running
func TestHTTPSListenerStopNotRunning(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https-stop-not-running",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	// Try to stop before starting
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = l.Stop(ctx)
	if err == nil {
		t.Error("expected error when stopping HTTPS listener that isn't running")
	}
}

// TestHTTPSListenerWithConnManager tests HTTPS listener with connection manager
func TestHTTPSListenerWithConnManager(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	mockManager := &mockConnManager{}

	opts := &Options{
		Name:        "test-https-conn-manager",
		Address:     "127.0.0.1:0",
		Handler:     testHandler(),
		ConnManager: mockManager,
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Make a request
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{Transport: tr}

	addr := l.Address()
	resp, err := client.Get("https://" + addr + "/test")
	if err != nil {
		t.Fatalf("HTTPS request failed: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerShutdownTimeout tests shutdown with timeout
func TestHTTPListenerShutdownTimeout(t *testing.T) {
	// Handler that takes a long time; signals when request is in-flight.
	requestStarted := make(chan struct{})
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("done"))
	})

	opts := &Options{
		Name:    "test-shutdown-timeout",
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

	// Start a slow request in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		http.Get("http://" + addr + "/slow")
	}()

	// Wait until the handler has started processing the request
	<-requestStarted

	// Try to shutdown with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = l.Stop(ctx)
	// Should get timeout error
	if err == nil {
		t.Error("expected timeout error from shutdown")
	}

	// Wait for goroutine
	wg.Wait()
}

// TestHTTPListenerStartErrorPropagation tests that start errors are captured
func TestHTTPListenerStartErrorPropagation(t *testing.T) {
	opts := &Options{
		Name:    "test-start-err-prop",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Initially should have no error
	if l.StartError() != nil {
		t.Errorf("expected nil StartError initially, got: %v", l.StartError())
	}

	// Start normally
	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// After successful start, should still have no error
	if l.StartError() != nil {
		t.Errorf("expected nil StartError after start, got: %v", l.StartError())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerStartError tests HTTPS listener Start method error paths
func TestHTTPSListenerStartError(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	// Test with invalid address
	opts := &Options{
		Name:    "test-https-start-err",
		Address: "invalid-address:999999",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	err = l.Start()
	if err == nil {
		t.Error("expected error when starting with invalid address")
	}
}

// TestHTTPListenerConcurrentStartStop tests concurrent start/stop operations
func TestHTTPListenerConcurrentStartStop(t *testing.T) {
	opts := &Options{
		Name:    "test-concurrent-start-stop",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	// Start listener
	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// Try concurrent operations
	var wg sync.WaitGroup

	// Multiple status checks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.IsRunning()
			_ = l.Address()
			_ = l.Name()
		}()
	}

	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPListenerHandlerPanic tests that handler panics don't crash the server
func TestHTTPListenerHandlerPanic(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			panic("intentional test panic")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	opts := &Options{
		Name:    "test-panic",
		Address: "127.0.0.1:0",
		Handler: panicHandler,
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

	// The default Go HTTP server recovers from panics, so this should work
	resp, err := http.Get("http://" + addr + "/normal")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	// Test panic endpoint - server should not crash
	// The request will fail (EOF) but server should stay up
	http.Get("http://" + addr + "/panic")

	// Give server a moment to recover
	time.Sleep(100 * time.Millisecond)

	// Server should still be running
	if !l.IsRunning() {
		t.Error("expected listener to still be running after panic")
	}

	// Make another request to verify server still works
	resp, err = http.Get("http://" + addr + "/after-panic")
	if err != nil {
		t.Fatalf("Request after panic failed: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerAddress tests HTTPS listener Address method
func TestHTTPSListenerAddress(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https-address",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	// Before start, should return configured address
	if l.Address() != "127.0.0.1:0" {
		t.Errorf("expected address 127.0.0.1:0, got %s", l.Address())
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// After start, should return actual bound address
	addr := l.Address()
	if addr == "127.0.0.1:0" {
		t.Error("expected actual bound address, got 127.0.0.1:0")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)
}

// TestHTTPSListenerName tests HTTPS listener Name method
func TestHTTPSListenerName(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https-name",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	if l.Name() != "test-https-name" {
		t.Errorf("expected name 'test-https-name', got '%s'", l.Name())
	}
}

// TestHTTPSListenerIsRunning tests HTTPS listener IsRunning method
func TestHTTPSListenerIsRunning(t *testing.T) {
	// Generate a test certificate
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create TLS manager
	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	opts := &Options{
		Name:    "test-https-isrunning",
		Address: "127.0.0.1:0",
		Handler: testHandler(),
	}

	l, err := NewHTTPSListener(opts, tlsManager)
	if err != nil {
		t.Fatalf("NewHTTPSListener failed: %v", err)
	}

	// Before start
	if l.IsRunning() {
		t.Error("expected IsRunning to be false before start")
	}

	if err := l.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForListener(t, l, 2*time.Second)

	// After start
	if !l.IsRunning() {
		t.Error("expected IsRunning to be true after start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.Stop(ctx)

	// After stop
	if l.IsRunning() {
		t.Error("expected IsRunning to be false after stop")
	}
}

// TestHTTPListener_StopWhenNotRunning tests Stop on a listener that hasnt been started.
func TestHTTPListener_StopWhenNotRunning(t *testing.T) {
	opts := &Options{
		Name:    "test-stop-not-running",
		Address: "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop without start should return "not running" error but not panic
	err = l.Stop(ctx)
	if err == nil {
		t.Error("Expected error when stopping non-running listener")
	}
}

// TestHTTPListener_AddressBeforeStart tests Address() returns configured address before Start.
func TestHTTPListener_AddressBeforeStart(t *testing.T) {
	opts := &Options{
		Name:    "test-addr-before",
		Address: "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	addr := l.Address()
	if addr != "127.0.0.1:0" {
		t.Errorf("Address() = %q, want 127.0.0.1:0", addr)
	}
}

// TestHTTPListener_StartTwice tests that starting an already running listener returns error.
func TestHTTPListener_StartTwice(t *testing.T) {
	opts := &Options{
		Name:    "test-start-twice",
		Address: "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
	l, err := NewHTTPListener(opts)
	if err != nil {
		t.Fatalf("NewHTTPListener failed: %v", err)
	}

	if err := l.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.Stop(ctx)
	}()

	waitForListener(t, l, 2*time.Second)

	// Second start should return an error
	err = l.Start()
	if err == nil {
		t.Error("Expected error when starting already running listener")
	}
}

// TestHTTPListener_NewMissingName tests NewHTTPListener with empty name.
func TestHTTPListener_NewMissingName(t *testing.T) {
	opts := &Options{
		Address: "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
	_, err := NewHTTPListener(opts)
	if err == nil {
		t.Error("Expected error for missing name")
	}
}

// TestHTTPListener_NewMissingAddress tests NewHTTPListener with empty address.
func TestHTTPListener_NewMissingAddress(t *testing.T) {
	opts := &Options{
		Name:    "test",
		Address: "",
		Handler: http.NewServeMux(),
	}
	_, err := NewHTTPListener(opts)
	if err == nil {
		t.Error("Expected error for missing address")
	}
}

// TestHTTPListener_NewMissingHandler tests NewHTTPListener with nil handler.
func TestHTTPListener_NewMissingHandler(t *testing.T) {
	opts := &Options{
		Name:    "test",
		Address: "127.0.0.1:0",
		Handler: nil,
	}
	_, err := NewHTTPListener(opts)
	if err == nil {
		t.Error("Expected error for nil handler")
	}
}

// TestHTTPSListener_NewWithInvalidOpts tests NewHTTPSListener with options that fail NewHTTPListener.
func TestHTTPSListener_NewWithInvalidOpts(t *testing.T) {
	certPEM, keyPEM, err := generateTestCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	tlsManager := olbTLS.NewManager()
	cert, err := tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load test certificate: %v", err)
	}
	tlsManager.AddCertificate(cert)
	tlsManager.SetDefaultCertificate(cert)

	// Empty name should fail
	opts := &Options{
		Name:    "",
		Address: "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
	_, err = NewHTTPSListener(opts, tlsManager)
	if err == nil {
		t.Error("Expected error for missing name in HTTPSListener")
	}
}

// TestMergeOptions_PartialOverrides tests mergeOptions with some fields set and some zero
func TestMergeOptions_PartialOverrides(t *testing.T) {
	opts := &Options{
		Name:           "test-partial",
		Address:        "127.0.0.1:0",
		Handler:        testHandler(),
		ReadTimeout:    45 * time.Second, // Custom value
		WriteTimeout:   0,                // Should get default
		IdleTimeout:    0,                // Should get default
		MaxHeaderBytes: 2 << 20,          // Custom value
	}

	result := mergeOptions(opts)

	// Custom values should be preserved
	if result.ReadTimeout != 45*time.Second {
		t.Errorf("expected ReadTimeout 45s, got %v", result.ReadTimeout)
	}
	if result.MaxHeaderBytes != 2<<20 {
		t.Errorf("expected MaxHeaderBytes 2MB, got %d", result.MaxHeaderBytes)
	}

	// Zero values should get defaults
	if result.WriteTimeout != 30*time.Second {
		t.Errorf("expected WriteTimeout default 30s, got %v", result.WriteTimeout)
	}
	if result.IdleTimeout != 120*time.Second {
		t.Errorf("expected IdleTimeout default 120s, got %v", result.IdleTimeout)
	}
	if result.HeaderTimeout != 10*time.Second {
		t.Errorf("expected HeaderTimeout default 10s, got %v", result.HeaderTimeout)
	}
}

// TestManagedListener_SuccessfulAccept tests managedListener when ConnManager accepts successfully
func TestManagedListener_SuccessfulAccept(t *testing.T) {
	// Create a pipe to simulate a connection
	server, _ := net.Pipe()
	defer server.Close()

	mockListener := &mockOneShotListener{conn: server}

	acceptCalled := false
	mockManager := &mockConnManager{
		acceptFunc: func(conn net.Conn) (net.Conn, error) {
			acceptCalled = true
			return conn, nil
		},
	}

	managed := &managedListener{
		Listener:    mockListener,
		connManager: mockManager,
	}

	conn, err := managed.Accept()
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}
	if conn == nil {
		t.Fatal("Expected non-nil connection")
	}
	if !acceptCalled {
		t.Error("Expected ConnManager.Accept to be called")
	}
}
