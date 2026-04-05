package l4

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestDefaultSNIRouterConfig(t *testing.T) {
	config := DefaultSNIRouterConfig()

	if !config.Passthrough {
		t.Error("Passthrough should be true by default")
	}
	if config.ReadTimeout != 5*time.Second {
		t.Errorf("ReadTimeout = %v, want 5s", config.ReadTimeout)
	}
	if config.MaxHandshakeSize != 16*1024 {
		t.Errorf("MaxHandshakeSize = %v, want 16KB", config.MaxHandshakeSize)
	}
}

func TestNewSNIRouter(t *testing.T) {
	config := DefaultSNIRouterConfig()
	router := NewSNIRouter(config)

	if router == nil {
		t.Fatal("NewSNIRouter returned nil")
	}
	if router.config != config {
		t.Error("Config mismatch")
	}
	if router.routes == nil {
		t.Error("Routes map should not be nil")
	}
}

func TestNewSNIRouter_NilConfig(t *testing.T) {
	router := NewSNIRouter(nil)

	if router == nil {
		t.Fatal("NewSNIRouter(nil) returned nil")
	}
	if router.config == nil {
		t.Error("Config should use defaults when nil")
	}
}

func TestSNIRouter_AddRoute(t *testing.T) {
	router := NewSNIRouter(nil)
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")

	router.AddRoute("example.com", be)

	retrieved := router.GetRoute("example.com")
	if retrieved != be {
		t.Error("Failed to retrieve added route")
	}
}

func TestSNIRouter_RemoveRoute(t *testing.T) {
	router := NewSNIRouter(nil)
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")

	router.AddRoute("example.com", be)
	router.RemoveRoute("example.com")

	retrieved := router.GetRoute("example.com")
	if retrieved != nil {
		t.Error("Route should have been removed")
	}
}

func TestSNIRouter_GetRoute_ExactMatch(t *testing.T) {
	router := NewSNIRouter(nil)
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")

	router.AddRoute("example.com", be)

	tests := []struct {
		name    string
		sni     string
		wantNil bool
	}{
		{"exact match", "example.com", false},
		{"case insensitive", "EXAMPLE.COM", false},
		{"no match", "other.com", true},
		{"subdomain", "sub.example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.GetRoute(tt.sni)
			if (got == nil) != tt.wantNil {
				t.Errorf("GetRoute(%q) nil = %v, want %v", tt.sni, got == nil, tt.wantNil)
			}
		})
	}
}

func TestSNIRouter_GetRoute_Wildcard(t *testing.T) {
	router := NewSNIRouter(nil)
	be := backend.NewBackend("backend-1", "127.0.0.1:8080")

	router.AddRoute("*.example.com", be)

	tests := []struct {
		name    string
		sni     string
		wantNil bool
	}{
		{"subdomain match", "sub.example.com", false},
		{"deep subdomain", "deep.sub.example.com", false},
		{"wildcard itself", "*.example.com", false}, // The wildcard pattern matches itself
		{"base domain", "example.com", true},
		{"other domain", "other.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.GetRoute(tt.sni)
			if (got == nil) != tt.wantNil {
				t.Errorf("GetRoute(%q) nil = %v, want %v", tt.sni, got == nil, tt.wantNil)
			}
		})
	}
}

func TestParseClientHelloSNI(t *testing.T) {
	// Build a minimal ClientHello with SNI
	clientHello := buildClientHelloWithSNI("example.com")

	sni, err := ParseClientHelloSNI(clientHello)
	if err != nil {
		t.Fatalf("ParseClientHelloSNI error: %v", err)
	}

	if sni != "example.com" {
		t.Errorf("SNI = %q, want example.com", sni)
	}
}

func TestParseClientHelloSNI_NoSNI(t *testing.T) {
	// Build a ClientHello without SNI
	clientHello := buildClientHelloWithoutSNI()

	_, err := ParseClientHelloSNI(clientHello)
	if err == nil {
		t.Error("Expected error for ClientHello without SNI")
	}
}

func TestParseClientHelloSNI_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"too short", []byte{0x16, 0x03, 0x01}},
		{"not handshake", []byte{0x17, 0x03, 0x01, 0x00, 0x05}},
		{"not ClientHello", buildServerHello()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseClientHelloSNI(tt.data)
			if err == nil {
				t.Error("Expected error")
			}
		})
	}
}

func TestIsTLSConnection(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid TLS",
			data: buildClientHelloWithSNI("example.com"),
			want: true,
		},
		{
			name: "not handshake",
			data: []byte{0x17, 0x03, 0x01, 0x00, 0x05, 0x00},
			want: false,
		},
		{
			name: "invalid version",
			data: []byte{0x16, 0x02, 0x99, 0x00, 0x05, 0x00},
			want: false,
		},
		{
			name: "not ClientHello",
			data: []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x02}, // ServerHello
			want: false,
		},
		{
			name: "too short",
			data: []byte{0x16},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTLSConnection(tt.data)
			if got != tt.want {
				t.Errorf("IsTLSConnection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractSNI(t *testing.T) {
	// Create a test connection
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write ClientHello in goroutine
	go func() {
		client.Write(buildClientHelloWithSNI("test.example.com"))
		client.Close()
	}()

	// Extract SNI
	sni, _, err := ExtractSNI(server, time.Second)
	if err != nil {
		t.Fatalf("ExtractSNI error: %v", err)
	}

	if sni != "test.example.com" {
		t.Errorf("SNI = %q, want test.example.com", sni)
	}
}

func TestExtractSNI_NotTLS(t *testing.T) {
	// Create a test connection
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write non-TLS data
	go func() {
		client.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
		client.Close()
	}()

	// Extract SNI
	_, _, err := ExtractSNI(server, time.Second)
	if err == nil {
		t.Error("Expected error for non-TLS connection")
	}
}

func TestNewSNIProxy(t *testing.T) {
	config := DefaultSNIRouterConfig()
	proxy := NewSNIProxy(config)

	if proxy == nil {
		t.Fatal("NewSNIProxy returned nil")
	}
	if proxy.routes == nil {
		t.Error("Routes map should not be nil")
	}
	if proxy.dialer == nil {
		t.Error("Dialer should not be nil")
	}
}

func TestSNIProxy_AddRoute(t *testing.T) {
	proxy := NewSNIProxy(nil)

	proxy.AddRoute("example.com", "127.0.0.1:8080")

	addr := proxy.GetBackend("example.com")
	if addr != "127.0.0.1:8080" {
		t.Errorf("Backend = %q, want 127.0.0.1:8080", addr)
	}
}

func TestSNIProxy_GetBackend_Wildcard(t *testing.T) {
	proxy := NewSNIProxy(nil)

	proxy.AddRoute("*.example.com", "127.0.0.1:8080")

	tests := []struct {
		sni  string
		want string
	}{
		{"sub.example.com", "127.0.0.1:8080"},
		{"deep.sub.example.com", "127.0.0.1:8080"},
		{"example.com", ""},
		{"other.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.sni, func(t *testing.T) {
			got := proxy.GetBackend(tt.sni)
			if got != tt.want {
				t.Errorf("GetBackend(%q) = %q, want %q", tt.sni, got, tt.want)
			}
		})
	}
}

func TestSNIMatcher(t *testing.T) {
	matcher := NewSNIMatcher()
	matcher.Add("example.com")
	matcher.Add("*.example.com")

	tests := []struct {
		sni  string
		want bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"other.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.sni, func(t *testing.T) {
			got := matcher.Match(tt.sni)
			if got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.sni, got, tt.want)
			}
		})
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		version uint16
		want    string
	}{
		{0x0300, "SSL 3.0"},
		{0x0301, "TLS 1.0"},
		{0x0302, "TLS 1.1"},
		{0x0303, "TLS 1.2"},
		{0x0304, "TLS 1.3"},
		{0x9999, "Unknown (0x9999)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data := []byte{0x16, byte(tt.version >> 8), byte(tt.version)}
			got, err := ParseTLSVersion(data)
			if err != nil {
				t.Fatalf("ParseTLSVersion error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseTLSVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTLSRecordInfo(t *testing.T) {
	data := buildClientHelloWithSNI("example.com")

	info, err := ParseTLSRecordInfo(data)
	if err != nil {
		t.Fatalf("ParseTLSRecordInfo error: %v", err)
	}

	if info.ContentType != "Handshake" {
		t.Errorf("ContentType = %q, want Handshake", info.ContentType)
	}
	if info.Version != "TLS 1.0" {
		t.Errorf("Version = %q, want TLS 1.0", info.Version)
	}
	if info.Length <= 0 {
		t.Error("Length should be positive")
	}
	if !info.IsClientHello {
		t.Error("IsClientHello should be true")
	}
}

func TestBufferedConn(t *testing.T) {
	// Create a pipe
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write data
	go func() {
		client.Write([]byte("Hello, World!"))
		client.Close()
	}()

	// Create buffered conn with initial data
	initial := []byte("Initial ")
	bufConn := NewBufferedConn(server, initial)

	// Read should return initial data first
	buf := make([]byte, 100)
	n, err := bufConn.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	if !bytes.Contains(buf[:n], []byte("Initial")) {
		t.Error("Expected initial data in read")
	}
}

func TestCreateTLSConfigForSNI(t *testing.T) {
	called := false
	getCert := func(sni string) (*tls.Certificate, error) {
		called = true
		return nil, nil
	}

	config := CreateTLSConfigForSNI(getCert)

	// Simulate ClientHello
	_, _ = config.GetCertificate(&tls.ClientHelloInfo{
		ServerName: "example.com",
	})

	if !called {
		t.Error("GetCertificate callback was not called")
	}
}

func TestNewSNIBasedProxy(t *testing.T) {
	config := DefaultSNIRouterConfig()
	proxy := NewSNIBasedProxy(config)

	if proxy == nil {
		t.Fatal("NewSNIBasedProxy returned nil")
	}
	if proxy.router == nil {
		t.Error("Router should not be nil")
	}
}

func TestSNIBasedProxy_AddRemoveRoute(t *testing.T) {
	proxy := NewSNIBasedProxy(nil)
	be := backend.NewBackend("b1", "127.0.0.1:8080")

	proxy.AddRoute("example.com", be)
	got := proxy.router.GetRoute("example.com")
	if got != be {
		t.Error("Expected route to be added")
	}

	proxy.RemoveRoute("example.com")
	got = proxy.router.GetRoute("example.com")
	if got != nil {
		t.Error("Expected route to be removed")
	}
}

func TestSNIBasedProxy_StartWithoutListen(t *testing.T) {
	proxy := NewSNIBasedProxy(nil)
	err := proxy.Start()
	if err == nil {
		t.Error("Expected error when starting without Listen")
	}
}

func TestSNIBasedProxy_ListenStartStop(t *testing.T) {
	proxy := NewSNIBasedProxy(nil)

	err := proxy.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	if !proxy.running.Load() {
		t.Error("Proxy should be running")
	}

	// Double start should fail
	err = proxy.Start()
	if err == nil {
		t.Error("Double start should fail")
	}

	err = proxy.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}

	if proxy.running.Load() {
		t.Error("Proxy should not be running after stop")
	}
}

func TestSNIBasedProxy_StopWithoutStart(t *testing.T) {
	proxy := NewSNIBasedProxy(nil)
	err := proxy.Stop()
	if err != nil {
		t.Errorf("Stop without start should not error: %v", err)
	}
}

func TestPeekedConn_Read(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		client.Write([]byte("subsequent data"))
		client.Close()
	}()

	pc := &peekedConn{
		Conn:   server,
		peeked: []byte("peeked "),
	}

	// First read should return peeked data
	buf := make([]byte, 7)
	n, err := pc.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf[:n]) != "peeked " {
		t.Errorf("Expected 'peeked ', got %q", string(buf[:n]))
	}

	// Second read should return from underlying connection
	buf2 := make([]byte, 100)
	n, err = pc.Read(buf2)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf2[:n]) != "subsequent data" {
		t.Errorf("Expected 'subsequent data', got %q", string(buf2[:n]))
	}
}

func TestParseTLSVersion_TooShort(t *testing.T) {
	_, err := ParseTLSVersion([]byte{0x16})
	if err == nil {
		t.Error("Expected error for too-short data")
	}
}

func TestParseTLSRecordInfo_AllContentTypes(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantType string
	}{
		{"ChangeCipherSpec", []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x00}, "ChangeCipherSpec"},
		{"Alert", []byte{0x15, 0x03, 0x03, 0x00, 0x01, 0x00}, "Alert"},
		{"Application", []byte{0x17, 0x03, 0x03, 0x00, 0x01, 0x00}, "Application"},
		{"Unknown", []byte{0xFF, 0x03, 0x03, 0x00, 0x01, 0x00}, "Unknown (255)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseTLSRecordInfo(tt.data)
			if err != nil {
				t.Fatalf("ParseTLSRecordInfo error: %v", err)
			}
			if info.ContentType != tt.wantType {
				t.Errorf("ContentType = %q, want %q", info.ContentType, tt.wantType)
			}
		})
	}
}

func TestParseTLSRecordInfo_TooShort(t *testing.T) {
	_, err := ParseTLSRecordInfo([]byte{0x16, 0x03})
	if err == nil {
		t.Error("Expected error for too-short data")
	}
}

func TestSNIProxy_NilConfig(t *testing.T) {
	proxy := NewSNIProxy(nil)
	if proxy == nil {
		t.Fatal("NewSNIProxy(nil) returned nil")
	}
	if proxy.config == nil {
		t.Error("config should use defaults")
	}
}

func TestSNIRouter_RouteConnection_WithSNI(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	// Create a backend listener
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	be := backend.NewBackend("backend-1", backendListener.Addr().String())
	be.SetState(backend.StateUp)
	router.AddRoute("example.com", be)

	// Accept connections on backend (just accept and close)
	go func() {
		conn, _ := backendListener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	// Create client connection with a TLS ClientHello
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	clientHello := buildClientHelloWithSNI("example.com")

	go func() {
		clientConn.Write(clientHello)
		clientConn.Close()
	}()

	// RouteConnection should process the SNI and route
	err = router.RouteConnection(serverConn)
	// May return error (e.g. connection closed by backend) but should not panic
	_ = err
}

func TestSNIRouter_RouteConnection_NoSNI(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Send non-TLS data
	go func() {
		clientConn.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
		clientConn.Close()
	}()

	// Should route to default (which closes the connection)
	err := router.RouteConnection(serverConn)
	if err == nil {
		t.Error("Expected error when no route and no default backend")
	}
}

func TestSNIRouter_PeekClientHello_ValidData(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	clientHello := buildClientHelloWithSNI("test.example.com")

	go func() {
		clientConn.Write(clientHello)
		clientConn.Close()
	}()

	sni, peeked, err := router.peekClientHello(serverConn)
	if err != nil {
		t.Fatalf("peekClientHello error: %v", err)
	}
	if sni != "test.example.com" {
		t.Errorf("SNI = %q, want test.example.com", sni)
	}
	if peeked == nil {
		t.Error("peeked connection should not be nil")
	}
}

func TestSNIRouter_PeekClientHello_InvalidData(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	go func() {
		clientConn.Write([]byte("not TLS data"))
		clientConn.Close()
	}()

	sni, peeked, err := router.peekClientHello(serverConn)
	if err == nil {
		t.Error("Expected error for non-TLS data")
	}
	if sni != "" {
		t.Errorf("SNI should be empty, got %q", sni)
	}
	if peeked == nil {
		t.Error("peeked connection should not be nil even on error")
	}
}

func TestSNIBasedProxy_FullLifecycle(t *testing.T) {
	config := DefaultSNIRouterConfig()
	proxy := NewSNIBasedProxy(config)

	// Create a backend listener
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	be := backend.NewBackend("b1", backendListener.Addr().String())
	be.SetState(backend.StateUp)
	proxy.AddRoute("test.com", be)

	// Listen and start
	err = proxy.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	if !proxy.running.Load() {
		t.Error("Proxy should be running")
	}

	// Remove and re-add route to test RemoveRoute
	proxy.RemoveRoute("test.com")
	if proxy.router.GetRoute("test.com") != nil {
		t.Error("Route should be removed")
	}

	proxy.AddRoute("test.com", be)
	if proxy.router.GetRoute("test.com") == nil {
		t.Error("Route should be re-added")
	}

	// Stop
	err = proxy.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}
}

// buildClientHelloWithSNI builds a minimal TLS ClientHello with SNI.
func buildClientHelloWithSNI(sni string) []byte {
	buf := new(bytes.Buffer)

	// Handshake header will be added after we know the length
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)                 // ClientHello
	buf.Write([]byte{0x00, 0x00, 0x00}) // Placeholder for length

	// ClientHello
	binary.Write(buf, binary.BigEndian, uint16(0x0303)) // TLS 1.2
	buf.Write(make([]byte, 32))                         // Random

	// Session ID length
	buf.WriteByte(0x00)

	// Cipher suites
	binary.Write(buf, binary.BigEndian, uint16(2))
	binary.Write(buf, binary.BigEndian, uint16(0x002f)) // TLS_RSA_WITH_AES_128_CBC_SHA

	// Compression methods
	buf.WriteByte(0x01)
	buf.WriteByte(0x00)

	// Extensions
	extensionsStart := buf.Len()
	binary.Write(buf, binary.BigEndian, uint16(0)) // Placeholder for extensions length

	// SNI extension
	sniExtension := buildSNIExtension(sni)
	buf.Write(sniExtension)

	// Update extensions length
	extensionsLen := buf.Len() - extensionsStart - 2
	buf.Bytes()[extensionsStart] = byte(extensionsLen >> 8)
	buf.Bytes()[extensionsStart+1] = byte(extensionsLen)

	// Update handshake length
	handshakeLen := buf.Len() - handshakeStart - 4
	buf.Bytes()[handshakeStart+1] = byte(handshakeLen >> 16)
	buf.Bytes()[handshakeStart+2] = byte(handshakeLen >> 8)
	buf.Bytes()[handshakeStart+3] = byte(handshakeLen)

	// Add record header
	record := make([]byte, 5+buf.Len())
	record[0] = 0x16                                // Handshake
	binary.BigEndian.PutUint16(record[1:3], 0x0301) // TLS 1.0
	binary.BigEndian.PutUint16(record[3:5], uint16(buf.Len()))
	copy(record[5:], buf.Bytes())

	return record
}

// buildSNIExtension builds the SNI extension.
func buildSNIExtension(sni string) []byte {
	buf := new(bytes.Buffer)

	// Extension type: SNI
	binary.Write(buf, binary.BigEndian, uint16(0x0000))

	// Extension length (will be filled in)
	extLenPos := buf.Len()
	binary.Write(buf, binary.BigEndian, uint16(0))

	// SNI list length (will be filled in)
	sniListLenPos := buf.Len()
	binary.Write(buf, binary.BigEndian, uint16(0))

	// Host name entry
	buf.WriteByte(0x00) // Name type: host_name
	binary.Write(buf, binary.BigEndian, uint16(len(sni)))
	buf.WriteString(sni)

	// Update lengths
	sniListLen := buf.Len() - sniListLenPos - 2
	buf.Bytes()[sniListLenPos] = byte(sniListLen >> 8)
	buf.Bytes()[sniListLenPos+1] = byte(sniListLen)

	extLen := buf.Len() - extLenPos - 2
	buf.Bytes()[extLenPos] = byte(extLen >> 8)
	buf.Bytes()[extLenPos+1] = byte(extLen)

	return buf.Bytes()
}

// buildClientHelloWithoutSNI builds a ClientHello without SNI.
func buildClientHelloWithoutSNI() []byte {
	buf := new(bytes.Buffer)

	// Handshake header
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	// ClientHello
	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32))
	buf.WriteByte(0x00)
	binary.Write(buf, binary.BigEndian, uint16(2))
	binary.Write(buf, binary.BigEndian, uint16(0x002f))
	buf.WriteByte(0x01)
	buf.WriteByte(0x00)

	// No extensions
	binary.Write(buf, binary.BigEndian, uint16(0))

	// Update handshake length
	handshakeLen := buf.Len() - handshakeStart - 4
	buf.Bytes()[handshakeStart+1] = byte(handshakeLen >> 16)
	buf.Bytes()[handshakeStart+2] = byte(handshakeLen >> 8)
	buf.Bytes()[handshakeStart+3] = byte(handshakeLen)

	record := make([]byte, 5+buf.Len())
	record[0] = 0x16
	binary.BigEndian.PutUint16(record[1:3], 0x0301)
	binary.BigEndian.PutUint16(record[3:5], uint16(buf.Len()))
	copy(record[5:], buf.Bytes())

	return record
}

// TestSNIBasedProxy_handleConnection tests the unexported handleConnection method.
func TestSNIBasedProxy_handleConnection(t *testing.T) {
	proxy := NewSNIBasedProxy(DefaultSNIRouterConfig())

	// Create a pipe; handleConnection will try to route via the router
	// which has no routes, so it will close the connection.
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		client.Write([]byte("not TLS data"))
		client.Close()
	}()

	// Should not panic; the error is swallowed inside handleConnection.
	proxy.handleConnection(server)
}

// TestSNIProxy_HandleConnection tests the exported HandleConnection method.
func TestSNIProxy_HandleConnection(t *testing.T) {
	proxy := NewSNIProxy(DefaultSNIRouterConfig())

	// Create a pipe; HandleConnection will try to extract SNI from non-TLS
	// data, fail, and return gracefully.
	client, server := net.Pipe()

	go func() {
		client.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		client.Close()
	}()

	// Should not panic; returns gracefully when SNI extraction fails.
	proxy.HandleConnection(server)
}

// TestSNIProxy_HandleConnection_WithSNI tests HandleConnection with a TLS
// ClientHello that has an SNI but no matching backend.
func TestSNIProxy_HandleConnection_WithSNI(t *testing.T) {
	config := DefaultSNIRouterConfig()
	proxy := NewSNIProxy(config)

	client, server := net.Pipe()

	clientHello := buildClientHelloWithSNI("unknown.example.com")
	go func() {
		client.Write(clientHello)
		client.Close()
	}()

	// No route and no default backend, so HandleConnection returns gracefully.
	proxy.HandleConnection(server)
}

// buildServerHello builds a ServerHello (for testing non-ClientHello).
func buildServerHello() []byte {
	return []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x02, 0x00, 0x00, 0x00, 0x00}
}

// --- Additional RouteConnection coverage ---

func TestSNIRouter_RouteConnection_NoRouteNoDefault(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())
	// No DefaultBackend, no routes

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	clientHello := buildClientHelloWithSNI("unknown.example.com")
	go func() {
		clientConn.Write(clientHello)
		clientConn.Close()
	}()

	err := router.RouteConnection(serverConn)
	if err == nil {
		t.Error("Expected error for SNI with no route and no default backend")
	}
}

func TestSNIRouter_RouteConnection_WithDefaultBackend(t *testing.T) {
	router := NewSNIRouter(&SNIRouterConfig{
		Passthrough:      true,
		ReadTimeout:      5 * time.Second,
		MaxHandshakeSize: 16 * 1024,
	})

	// Start a backend listener
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	be := backend.NewBackend("default-backend", backendListener.Addr().String())
	be.SetState(backend.StateUp)
	router.AddRoute("example.com", be)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	clientHello := buildClientHelloWithSNI("example.com")
	go func() {
		clientConn.Write(clientHello)
		clientConn.Close()
	}()

	err = router.RouteConnection(serverConn)
	// May succeed or fail depending on timing (backend closes), but should not panic
	_ = err
}

func TestSNIRouter_RouteConnection_ReadError(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())

	// Already-closed connection causes immediate read error
	clientConn, serverConn := net.Pipe()
	clientConn.Close()

	err := router.RouteConnection(serverConn)
	if err == nil {
		t.Error("Expected error for read on closed connection")
	}
}

// --- acceptLoop coverage ---

func TestSNIBasedProxy_AcceptLoop_StopWhileRunning(t *testing.T) {
	proxy := NewSNIBasedProxy(DefaultSNIRouterConfig())

	err := proxy.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Stop while acceptLoop is running — should trigger the !running check
	err = proxy.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}

	if proxy.running.Load() {
		t.Error("Proxy should not be running after stop")
	}
}

func TestSNIBasedProxy_AcceptLoop_HandlesConnections(t *testing.T) {
	// Create a backend
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	proxy := NewSNIBasedProxy(DefaultSNIRouterConfig())
	be := backend.NewBackend("b1", backendListener.Addr().String())
	be.SetState(backend.StateUp)
	proxy.AddRoute("test.com", be)

	err = proxy.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer proxy.Stop()

	// Connect as a client with TLS ClientHello
	conn, err := net.Dial("tcp", proxy.listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send a TLS ClientHello with SNI
	clientHello := buildClientHelloWithSNI("test.com")
	conn.Write(clientHello)

	// Give the accept loop time to process
	time.Sleep(200 * time.Millisecond)
}

// --- SNIProxy HandleConnection coverage ---

func TestSNIProxy_HandleConnection_WithDefaultBackend(t *testing.T) {
	// Start a backend echo server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the peeked data that gets forwarded
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		// Echo it back
		conn.Write(buf[:n])
	}()

	config := &SNIRouterConfig{
		Passthrough:      true,
		ReadTimeout:      2 * time.Second,
		MaxHandshakeSize: 16 * 1024,
		DefaultBackend:   backendListener.Addr().String(),
	}
	proxy := NewSNIProxy(config)

	client, server := net.Pipe()

	clientHello := buildClientHelloWithSNI("unmatched.example.com")
	go func() {
		client.Write(clientHello)
		client.Write([]byte("test-data"))
		time.Sleep(500 * time.Millisecond)
		client.Close()
	}()

	// HandleConnection should route to default backend
	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	select {
	case <-done:
		// Good — HandleConnection completed
	case <-time.After(5 * time.Second):
		t.Error("HandleConnection should complete within timeout")
	}
}

func TestSNIProxy_HandleConnection_ReadError(t *testing.T) {
	config := DefaultSNIRouterConfig()
	proxy := NewSNIProxy(config)

	// Use a closed pipe — ExtractSNI will fail immediately
	client, server := net.Pipe()
	client.Close()

	// Should return gracefully without panic
	proxy.HandleConnection(server)
}

// --- parseClientHello additional coverage ---

func TestParseClientHelloSNI_InvalidVersion(t *testing.T) {
	// Build a ClientHello with invalid version in the ClientHello body
	buf := new(bytes.Buffer)

	// TLS record header
	buf.WriteByte(0x16)
	binary.Write(buf, binary.BigEndian, uint16(0x0301)) // TLS 1.0

	// Handshake header placeholder
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)                 // ClientHello type
	buf.Write([]byte{0x00, 0x00, 0x00}) // length placeholder

	// Invalid ClientHello version (0x0200 = SSL 2.0, out of range)
	binary.Write(buf, binary.BigEndian, uint16(0x0200))
	buf.Write(make([]byte, 32)) // Random
	buf.WriteByte(0x00)         // Session ID length
	binary.Write(buf, binary.BigEndian, uint16(2))
	binary.Write(buf, binary.BigEndian, uint16(0x002f))
	buf.WriteByte(0x01) // Compression methods length
	buf.WriteByte(0x00) // No compression

	// Update lengths
	handshakeLen := buf.Len() - handshakeStart - 4
	data := buf.Bytes()
	data[handshakeStart+1] = byte(handshakeLen >> 16)
	data[handshakeStart+2] = byte(handshakeLen >> 8)
	data[handshakeStart+3] = byte(handshakeLen)

	// Update record length
	record := make([]byte, 5+len(data[5:]))
	copy(record, data[:5])
	binary.BigEndian.PutUint16(record[3:5], uint16(len(data[5:])))
	copy(record[5:], data[5:])

	_, err := ParseClientHelloSNI(record)
	if err == nil {
		t.Error("Expected error for invalid ClientHello version")
	}
}

func TestParseClientHelloSNI_DataTooShort(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"1 byte", []byte{0x16}},
		{"2 bytes", []byte{0x16, 0x03}},
		{"3 bytes", []byte{0x16, 0x03, 0x01}},
		{"4 bytes", []byte{0x16, 0x03, 0x01, 0x00}},
		{"5 bytes wrong type", []byte{0x17, 0x03, 0x01, 0x00, 0x05}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseClientHelloSNI(tt.data)
			if err == nil {
				t.Error("Expected error for short/invalid data")
			}
		})
	}
}

func TestParseClientHelloSNI_IncompleteRecord(t *testing.T) {
	// Build a header that claims more data than provided
	buf := new(bytes.Buffer)
	buf.WriteByte(0x16)                                 // Handshake
	binary.Write(buf, binary.BigEndian, uint16(0x0301)) // TLS 1.0
	binary.Write(buf, binary.BigEndian, uint16(0xFF))   // Claim 255 bytes
	buf.WriteByte(0x01)                                 // ClientHello

	// Only provide a few bytes — not enough
	buf.Write(make([]byte, 10))

	_, err := ParseClientHelloSNI(buf.Bytes())
	if err == nil {
		t.Error("Expected error for incomplete record")
	}
}

func TestParseClientHelloSNI_VersionOutOfRange(t *testing.T) {
	// TLS record with version outside valid range
	buf := new(bytes.Buffer)
	buf.WriteByte(0x16)
	binary.Write(buf, binary.BigEndian, uint16(0x0500)) // Invalid TLS version
	binary.Write(buf, binary.BigEndian, uint16(0x05))
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00, 0x00})

	_, err := ParseClientHelloSNI(buf.Bytes())
	if err == nil {
		t.Error("Expected error for invalid TLS version in record header")
	}
}

func TestParseClientHelloSNI_IncompleteHandshake(t *testing.T) {
	// Valid record header but handshake is too short
	buf := new(bytes.Buffer)
	buf.WriteByte(0x16)
	binary.Write(buf, binary.BigEndian, uint16(0x0301)) // TLS 1.0
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.WriteByte(0x01)                 // ClientHello
	buf.Write([]byte{0x00, 0x00, 0x01}) // handshake length = 1
	buf.WriteByte(0x03)                 // Only 1 byte — not enough for version

	_, err := ParseClientHelloSNI(buf.Bytes())
	if err == nil {
		t.Error("Expected error for incomplete handshake data")
	}
}

func TestParseClientHelloSNI_NoExtensions(t *testing.T) {
	// ClientHello without extensions section
	buf := new(bytes.Buffer)

	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32)) // Random
	buf.WriteByte(0x00)         // Session ID length
	binary.Write(buf, binary.BigEndian, uint16(2))
	binary.Write(buf, binary.BigEndian, uint16(0x002f))
	buf.WriteByte(0x01) // Compression length
	buf.WriteByte(0x00) // No compression

	// No extensions — the data ends here after compression

	handshakeLen := buf.Len() - handshakeStart - 4
	data := buf.Bytes()
	data[handshakeStart+1] = byte(handshakeLen >> 16)
	data[handshakeStart+2] = byte(handshakeLen >> 8)
	data[handshakeStart+3] = byte(handshakeLen)

	record := make([]byte, 5+len(data[handshakeStart:]))
	record[0] = 0x16
	binary.BigEndian.PutUint16(record[1:3], 0x0301)
	binary.BigEndian.PutUint16(record[3:5], uint16(len(data[handshakeStart:])))
	copy(record[5:], data[handshakeStart:])

	_, err := ParseClientHelloSNI(record)
	if err == nil {
		t.Error("Expected error for ClientHello without extensions")
	}
}

// --- parseSNIList additional coverage ---

func TestParseSNIList_TooShort(t *testing.T) {
	_, err := parseSNIList([]byte{0x00})
	if err == nil {
		t.Error("Expected error for too-short SNI list data")
	}
}

func TestParseSNIList_Empty(t *testing.T) {
	_, err := parseSNIList([]byte{})
	if err == nil {
		t.Error("Expected error for empty SNI list data")
	}
}

func TestParseSNIList_ListLengthExceedsData(t *testing.T) {
	// Claim list length of 100 but only provide 2 bytes
	data := []byte{0x00, 0x64} // list length = 100
	_, err := parseSNIList(data)
	if err == nil {
		t.Error("Expected error when SNI list length exceeds data")
	}
}

func TestParseSNIList_EntryLengthExceedsData(t *testing.T) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, uint16(5))   // list length
	buf.WriteByte(0x00)                              // host name type
	binary.Write(buf, binary.BigEndian, uint16(100)) // entry claims 100 bytes
	buf.Write([]byte("hi"))                          // but only 2

	_, err := parseSNIList(buf.Bytes())
	if err == nil {
		t.Error("Expected error when SNI entry length exceeds data")
	}
}

func TestParseSNIList_NonHostNameType(t *testing.T) {
	buf := new(bytes.Buffer)

	// SNI list length
	sniData := []byte{0x05, 'h', 'e', 'l', 'l', 'o'}
	binary.Write(buf, binary.BigEndian, uint16(len(sniData)+3))

	// Entry with non-hostname type (0x01)
	buf.WriteByte(0x01) // Not hostname type
	binary.Write(buf, binary.BigEndian, uint16(5))
	buf.Write([]byte("hello"))

	_, err := parseSNIList(buf.Bytes())
	if err == nil {
		t.Error("Expected error when no host name SNI type found")
	}
}

func TestParseSNIList_ValidHostName(t *testing.T) {
	buf := new(bytes.Buffer)

	sni := "test.example.com"
	sniData := []byte(sni)
	binary.Write(buf, binary.BigEndian, uint16(len(sniData)+3))

	buf.WriteByte(0x00) // hostname type
	binary.Write(buf, binary.BigEndian, uint16(len(sniData)))
	buf.Write(sniData)

	result, err := parseSNIList(buf.Bytes())
	if err != nil {
		t.Fatalf("parseSNIList error: %v", err)
	}
	if result != sni {
		t.Errorf("SNI = %q, want %q", result, sni)
	}
}

// --- proxyToBackend coverage ---

func TestSNIRouter_ProxyToBackend_Success(t *testing.T) {
	// Create a backend echo server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}()

	router := NewSNIRouter(DefaultSNIRouterConfig())
	be := backend.NewBackend("backend-1", backendListener.Addr().String())
	be.SetState(backend.StateUp)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	clientHello := buildClientHelloWithSNI("example.com")

	go func() {
		clientConn.Write(clientHello)
		clientConn.Write([]byte("test-data"))
		time.Sleep(300 * time.Millisecond)
		clientConn.Close()
	}()

	// peekClientHello to get a peekedConn, then proxyToBackend
	sni, peeked, err := router.peekClientHello(serverConn)
	if err != nil {
		t.Fatalf("peekClientHello error: %v", err)
	}
	if sni != "example.com" {
		t.Errorf("SNI = %q, want example.com", sni)
	}

	err = router.proxyToBackend(peeked, be)
	// Should complete without panic; may return nil or an error depending on timing
	_ = err
}

func TestSNIRouter_ProxyToBackend_ConnectionRefused(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())
	// Use a port that is reliably unreachable (high random port with no listener)
	be := backend.NewBackend("backend-1", "127.0.0.1:1")
	be.SetState(backend.StateUp)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	go func() {
		clientConn.Write([]byte("data"))
		clientConn.Close()
	}()

	err := router.proxyToBackend(serverConn, be)
	if err == nil {
		t.Error("Expected error when backend is unreachable")
	}

	if be.TotalErrors() == 0 {
		t.Error("Expected error to be recorded on backend")
	}
}

func TestSNIRouter_ProxyToBackend_MaxConns(t *testing.T) {
	router := NewSNIRouter(DefaultSNIRouterConfig())
	be := backend.NewBackend("backend-1", "127.0.0.1:0")
	be.SetState(backend.StateUp)
	be.MaxConns = 1
	// Saturate the conn slot
	be.AcquireConn()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	err := router.proxyToBackend(serverConn, be)
	if err == nil {
		t.Error("Expected error when backend at max connections")
	}

	be.ReleaseConn()
}

// --- BufferedConn.Read additional coverage ---

func TestBufferedConn_Read_UnderlyingConn(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write data on the client side
	go func() {
		client.Write([]byte("underlying data"))
		client.Close()
	}()

	// Create BufferedConn with no initial buffer
	bufConn := NewBufferedConn(server, nil)

	buf := make([]byte, 100)
	n, err := bufConn.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf[:n]) != "underlying data" {
		t.Errorf("Read = %q, want underlying data", string(buf[:n]))
	}
}

func TestBufferedConn_Read_BufferThenConn(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		client.Write([]byte("from-conn"))
		client.Close()
	}()

	bufConn := NewBufferedConn(server, []byte("from-buf"))

	// First read should come from buffer
	buf1 := make([]byte, 9)
	n1, err := bufConn.Read(buf1)
	if err != nil {
		t.Fatalf("First read error: %v", err)
	}
	if string(buf1[:n1]) != "from-buf" {
		t.Errorf("First read = %q, want from-buf", string(buf1[:n1]))
	}

	// Second read should come from underlying connection
	buf2 := make([]byte, 100)
	n2, err := bufConn.Read(buf2)
	if err != nil {
		t.Fatalf("Second read error: %v", err)
	}
	if string(buf2[:n2]) != "from-conn" {
		t.Errorf("Second read = %q, want from-conn", string(buf2[:n2]))
	}
}

func TestBufferedConn_Read_EmptyBuffer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		client.Write([]byte("hello"))
		client.Close()
	}()

	bufConn := NewBufferedConn(server, []byte{})

	buf := make([]byte, 100)
	n, err := bufConn.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("Read = %q, want hello", string(buf[:n]))
	}
}

// --- SNIMatcher wildcard edge case ---

func TestSNIMatcher_WildcardNotExactMatch(t *testing.T) {
	matcher := NewSNIMatcher()
	matcher.Add("*.example.com")

	// Wildcard pattern "example.com" itself should not match
	// The suffix is ".example.com", and sni == wildcard check prevents exact base match
	if matcher.Match("example.com") {
		t.Error("example.com should not match *.example.com")
	}
}

// --- SNIProxy_HandleConnection_WithMatchingRoute ---

func TestSNIProxy_HandleConnection_WithMatchingRoute(t *testing.T) {
	// Create a backend listener that echoes data
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer backendListener.Close()

	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}()

	config := DefaultSNIRouterConfig()
	proxy := NewSNIProxy(config)
	proxy.AddRoute("test.com", backendListener.Addr().String())

	client, server := net.Pipe()

	clientHello := buildClientHelloWithSNI("test.com")
	go func() {
		client.Write(clientHello)
		client.Write([]byte("payload"))
		time.Sleep(500 * time.Millisecond)
		client.Close()
	}()

	done := make(chan struct{})
	go func() {
		proxy.HandleConnection(server)
		close(done)
	}()

	select {
	case <-done:
		// Completed successfully
	case <-time.After(5 * time.Second):
		t.Error("HandleConnection should complete")
	}
}
