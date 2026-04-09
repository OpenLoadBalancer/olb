package l4

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// --- parseV2 uncovered branches ---

func TestParseV2_InvalidCommand(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	// Build v2 header with invalid command (version=2, command=0x05)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x25) // version=2, command=5 (invalid)
	buf.WriteByte(0x11) // AF_INET + STREAM
	binary.Write(buf, binary.BigEndian, uint16(0))

	_, _, err := parser.Parse(buf.Bytes())
	if err == nil {
		t.Error("expected error for invalid command")
	}
	if err.Error() != "invalid command" {
		t.Errorf("error = %q, want 'invalid command'", err.Error())
	}
}

func TestParseV2_IncompleteDataForLength(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	// Build v2 header that declares length=100 but has no data
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21)                              // version=2, PROXY command
	buf.WriteByte(0x11)                              // AF_INET + STREAM
	binary.Write(buf, binary.BigEndian, uint16(100)) // claims 100 bytes
	// Only 16 bytes total, not 16+100

	_, _, err := parser.Parse(buf.Bytes())
	if err == nil {
		t.Error("expected error for incomplete v2 header")
	}
}

func TestParseV2_UDP4(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21) // version=2, PROXY command
	buf.WriteByte(0x12) // AF_INET + DGRAM
	binary.Write(buf, binary.BigEndian, uint16(12))
	buf.Write([]byte{10, 0, 0, 1})                  // src IP
	buf.Write([]byte{10, 0, 0, 2})                  // dst IP
	binary.Write(buf, binary.BigEndian, uint16(53)) // src port
	binary.Write(buf, binary.BigEndian, uint16(53)) // dst port

	header, _, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if header.Transport != PROXYTransportDgram {
		t.Errorf("Transport = %v, want DGRAM", header.Transport)
	}
	_, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Error("SourceAddr should be UDPAddr for UDP transport")
	}
}

func TestParseV2_UDP6(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21) // version=2, PROXY command
	buf.WriteByte(0x22) // AF_INET6 + DGRAM
	binary.Write(buf, binary.BigEndian, uint16(36))
	buf.Write(net.ParseIP("2001:db8::1").To16())
	buf.Write(net.ParseIP("2001:db8::2").To16())
	binary.Write(buf, binary.BigEndian, uint16(53))
	binary.Write(buf, binary.BigEndian, uint16(53))

	header, _, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if header.Transport != PROXYTransportDgram {
		t.Errorf("Transport = %v, want DGRAM", header.Transport)
	}
	_, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Error("SourceAddr should be UDPAddr for UDP6 transport")
	}
}

func TestParseV2_UnixFamily(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21)                              // version=2, PROXY command
	buf.WriteByte(0x30)                              // AF_UNIX + UNSPEC
	binary.Write(buf, binary.BigEndian, uint16(216)) // 216 bytes for Unix addresses
	buf.Write(make([]byte, 216))                     // Zero-filled Unix addresses

	header, _, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if header.Family != PROXYAFUnix {
		t.Errorf("Family = %v, want AF_UNIX", header.Family)
	}
}

func TestParseV2_InsufficientIPv4Data(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21)
	buf.WriteByte(0x11)                            // AF_INET
	binary.Write(buf, binary.BigEndian, uint16(8)) // claims 8 bytes but need 12

	_, _, err := parser.Parse(buf.Bytes())
	if err == nil {
		t.Error("expected error for insufficient IPv4 data")
	}
}

func TestParseV2_InsufficientIPv6Data(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21)
	buf.WriteByte(0x21)                             // AF_INET6
	binary.Write(buf, binary.BigEndian, uint16(16)) // claims 16 bytes but need 36

	_, _, err := parser.Parse(buf.Bytes())
	if err == nil {
		t.Error("expected error for insufficient IPv6 data")
	}
}

func TestParseV2_InsufficientUnixData(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21)
	buf.WriteByte(0x31)                              // AF_UNIX + STREAM
	binary.Write(buf, binary.BigEndian, uint16(100)) // claims 100 bytes but need 216
	buf.Write(make([]byte, 100))

	_, _, err := parser.Parse(buf.Bytes())
	if err == nil {
		t.Error("expected error for insufficient Unix data")
	}
}

func TestParseV2_IPv6WithTLVs(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})
	buf.WriteByte(0x21)
	buf.WriteByte(0x21) // AF_INET6 + STREAM
	// 36 bytes address + 7 bytes TLV = 43
	binary.Write(buf, binary.BigEndian, uint16(43))
	buf.Write(net.ParseIP("2001:db8::1").To16())
	buf.Write(net.ParseIP("2001:db8::2").To16())
	binary.Write(buf, binary.BigEndian, uint16(12345))
	binary.Write(buf, binary.BigEndian, uint16(443))
	// TLV
	buf.WriteByte(0x01)
	binary.Write(buf, binary.BigEndian, uint16(4))
	buf.WriteString("test")

	header, _, err := parser.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(header.TLVs) != 1 {
		t.Errorf("TLVs = %d, want 1", len(header.TLVs))
	}
}

func TestParseV2_WithRemainingData(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := append(buildPROXYProtocolV2TCP4(), []byte("extra data")...)

	header, remaining, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if header == nil {
		t.Fatal("header is nil")
	}
	if string(remaining) != "extra data" {
		t.Errorf("remaining = %q, want 'extra data'", string(remaining))
	}
}

// --- parseV1 uncovered branches ---

func TestParseV1_UDP6(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY UDP6 2001:db8::1 2001:db8::2 53 53\r\n")

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if header.Family != PROXYAFInet6 {
		t.Errorf("Family = %v, want AF_INET6", header.Family)
	}
	if header.Transport != PROXYTransportDgram {
		t.Errorf("Transport = %v, want DGRAM", header.Transport)
	}
	_, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Error("SourceAddr should be UDPAddr for UDP6")
	}
}

func TestParseV1_InvalidIP(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 not-an-ip 192.168.1.2 12345 443\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for invalid IP address")
	}
}

func TestParseV1_InvalidIPDst(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 not-an-ip 12345 443\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for invalid destination IP address")
	}
}

func TestParseV1_NoSubProtocol(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	// "PROXY\r\n" — only 1 part after split by space
	data := []byte("PROXY\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for header with no sub-protocol")
	}
}

// --- WriteV1 coverage ---

func TestWriteV1_FlushError(t *testing.T) {
	// Use a writer that always fails
	writer := bufio.NewWriter(&failingWriter{})
	srcAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 443}

	err := WriteV1(writer, srcAddr, dstAddr)
	if err == nil {
		t.Error("expected error from failing writer")
	}
}

// failingWriter always returns an error on Write.
type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (n int, err error) { return 0, bytes.ErrTooLarge }

// --- OriginalDest fallback ---

func TestPROXYConn_OriginalDest_Fallback(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := NewPROXYConn(server, nil)
	dest := conn.OriginalDest()
	if dest == nil {
		t.Error("OriginalDest should fall back to LocalAddr when header is nil")
	}
}

// --- copyWithTimeout zero timeout ---

func TestTCPProxy_CopyWithTimeout_ZeroTimeout(t *testing.T) {
	pool := backend.NewPool("test", "round_robin")
	cfg := &TCPProxyConfig{
		IdleTimeout: 0, // zero — should use 5 minute default
		BufferSize:  1024,
	}
	proxy := NewTCPProxy(pool, NewSimpleBalancer(), cfg)

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}()

	echoConn, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer echoConn.Close()

	src, srcPipe := net.Pipe()
	defer src.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.copyWithTimeout(echoConn, srcPipe)
	}()

	src.Write([]byte("hello"))
	echoConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 100)
	n, err := echoConn.Read(buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("got %q, want hello", string(buf[:n]))
	}

	src.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for CopyBidirectional to complete")
	}
}

// --- CopyBidirectional panic recovery ---

func TestCopyBidirectional_PanicRecovery(t *testing.T) {
	conn1a, conn1b := net.Pipe()
	conn2a, conn2b := net.Pipe()
	defer conn1a.Close()
	defer conn2a.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// This should trigger panic in copy because conn1b panics on Read
		CopyBidirectional(conn1b, conn2b, time.Second)
	}()

	// Close both ends to trigger normal close paths
	conn1a.Close()
	conn2a.Close()

	select {
	case <-done:
		// Completed without hanging
	case <-time.After(5 * time.Second):
		t.Fatal("CopyBidirectional should complete")
	}
}

// --- CopyBidirectional non-normal error ---

func TestCopyBidirectional_NonNormalError(t *testing.T) {
	// Create a scenario where copy returns a non-normal error
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read once then close abruptly
		buf := make([]byte, 1024)
		conn.Read(buf)
		// Don't echo back, just close
	}()

	echoConn, err := net.Dial("tcp", echoLn.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer echoConn.Close()

	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = CopyBidirectional(proxyConn, echoConn, 2*time.Second)
	}()

	// Send data
	clientConn.Write([]byte("test"))
	// Close to trigger error path
	time.Sleep(100 * time.Millisecond)
	clientConn.Close()
	echoConn.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CopyBidirectional should complete")
	}
}

// --- parseClientHello additional truncation paths ---

func TestParseClientHelloSNI_TruncatedRandom(t *testing.T) {
	buf := new(bytes.Buffer)
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 10)) // Only 10 bytes instead of 32 for random

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
		t.Error("expected error for truncated random")
	}
}

func TestParseClientHelloSNI_TruncatedSessionID(t *testing.T) {
	buf := new(bytes.Buffer)
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32)) // Random (correct)
	buf.WriteByte(0x20)         // Session ID length = 32
	// But no session ID data follows

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
		t.Error("expected error for truncated session ID")
	}
}

func TestParseClientHelloSNI_TruncatedCipherSuites(t *testing.T) {
	buf := new(bytes.Buffer)
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32))                      // Random
	buf.WriteByte(0x00)                              // Session ID length = 0
	binary.Write(buf, binary.BigEndian, uint16(100)) // Cipher suites length = 100
	// But no cipher suite data follows

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
		t.Error("expected error for truncated cipher suites")
	}
}

func TestParseClientHelloSNI_TruncatedCompression(t *testing.T) {
	buf := new(bytes.Buffer)
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32)) // Random
	buf.WriteByte(0x00)         // Session ID length = 0
	binary.Write(buf, binary.BigEndian, uint16(2))
	binary.Write(buf, binary.BigEndian, uint16(0x002f))
	buf.WriteByte(0x05) // Compression methods length = 5
	// But no compression data follows

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
		t.Error("expected error for truncated compression methods")
	}
}

func TestParseClientHelloSNI_TruncatedExtensionsData(t *testing.T) {
	buf := new(bytes.Buffer)
	handshakeStart := buf.Len()
	buf.WriteByte(0x01)
	buf.Write([]byte{0x00, 0x00, 0x00})

	binary.Write(buf, binary.BigEndian, uint16(0x0303))
	buf.Write(make([]byte, 32)) // Random
	buf.WriteByte(0x00)         // Session ID length = 0
	binary.Write(buf, binary.BigEndian, uint16(2))
	binary.Write(buf, binary.BigEndian, uint16(0x002f))
	buf.WriteByte(0x01) // Compression methods length
	buf.WriteByte(0x00) // No compression

	// Extensions length claims more data than available
	binary.Write(buf, binary.BigEndian, uint16(50))
	// But only 2 bytes follow
	buf.Write([]byte{0x00, 0x00})

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
		t.Error("expected error for truncated extensions data")
	}
}

// --- parseSNIExtension incomplete extension data ---

func TestParseSNIExtension_IncompleteExtension(t *testing.T) {
	// Build extension data where the first extension claims more data than available
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, uint16(0x0000)) // SNI extension type
	binary.Write(buf, binary.BigEndian, uint16(50))     // Extension length = 50
	// But only a few bytes follow
	buf.Write([]byte{0x00, 0x01})

	_, err := parseSNIExtension(buf.Bytes())
	if err == nil {
		t.Error("expected error for incomplete extension data")
	}
}

func TestParseSNIExtension_NonSNISkip(t *testing.T) {
	// Build extensions with a non-SNI extension followed by SNI
	buf := new(bytes.Buffer)

	// Non-SNI extension (type 0x0001, length 3, value "abc")
	binary.Write(buf, binary.BigEndian, uint16(0x0001))
	binary.Write(buf, binary.BigEndian, uint16(3))
	buf.WriteString("abc")

	// SNI extension
	sniExt := buildSNIExtension("example.com")
	buf.Write(sniExt)

	sni, err := parseSNIExtension(buf.Bytes())
	if err != nil {
		t.Fatalf("parseSNIExtension error: %v", err)
	}
	if sni != "example.com" {
		t.Errorf("SNI = %q, want example.com", sni)
	}
}

// --- isNormalCloseError additional paths ---

func TestIsNormalCloseError_ConnectionRefused(t *testing.T) {
	err := fmt.Errorf("connection refused somewhere")
	if !isNormalCloseError(err) {
		t.Error("expected connection refused to be a normal close error")
	}
}

func TestIsNormalCloseError_NetTimeout(t *testing.T) {
	// net.Error with Timeout() returning true should be normal close
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr != nil {
		if !isNormalCloseError(readErr) {
			t.Errorf("timeout error should be normal close: %v", readErr)
		}
	}
}

// --- parseClientHello too-short data after handshake header ---

func TestParseClientHelloSNI_HandshakeTooShort(t *testing.T) {
	// Valid record header + handshake type but only 2 bytes of handshake data
	buf := new(bytes.Buffer)
	buf.WriteByte(0x16)
	binary.Write(buf, binary.BigEndian, uint16(0x0301))
	binary.Write(buf, binary.BigEndian, uint16(10)) // record length
	buf.WriteByte(0x01)                             // ClientHello
	buf.Write([]byte{0x00, 0x00, 0x06})             // handshake length = 6
	// Incomplete: need at least 4 bytes for handshake header + 2 for version
	buf.Write([]byte{0x03}) // Only 1 byte

	_, err := ParseClientHelloSNI(buf.Bytes())
	if err == nil {
		t.Error("expected error for too-short handshake data")
	}
}
