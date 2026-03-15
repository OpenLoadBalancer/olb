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

// buildServerHello builds a ServerHello (for testing non-ClientHello).
func buildServerHello() []byte {
	return []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x02, 0x00, 0x00, 0x00, 0x00}
}
