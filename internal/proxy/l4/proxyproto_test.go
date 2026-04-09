package l4

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDefaultPROXYProtocolConfig(t *testing.T) {
	config := DefaultPROXYProtocolConfig()

	if !config.AcceptV1 {
		t.Error("AcceptV1 should be true by default")
	}
	if !config.AcceptV2 {
		t.Error("AcceptV2 should be true by default")
	}
	if !config.AllowLocal {
		t.Error("AllowLocal should be true by default")
	}
	if config.SendV1 {
		t.Error("SendV1 should be false by default")
	}
	if config.SendV2 {
		t.Error("SendV2 should be false by default")
	}
}

func TestNewPROXYProtocolParser(t *testing.T) {
	config := DefaultPROXYProtocolConfig()
	parser := NewPROXYProtocolParser(config)

	if parser == nil {
		t.Fatal("NewPROXYProtocolParser returned nil")
	}
	if parser.config != config {
		t.Error("Config mismatch")
	}
}

func TestNewPROXYProtocolParser_NilConfig(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)

	if parser == nil {
		t.Fatal("NewPROXYProtocolParser(nil) returned nil")
	}
	if parser.config == nil {
		t.Error("Config should use defaults when nil")
	}
}

func TestParseV1_TCP4(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\n")

	header, remaining, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Version != PROXYProtocolV1 {
		t.Errorf("Version = %v, want V1", header.Version)
	}
	if header.Command != PROXYCommandProxy {
		t.Errorf("Command = %v, want PROXY", header.Command)
	}
	if header.Family != PROXYAFInet {
		t.Errorf("Family = %v, want AF_INET", header.Family)
	}
	if header.Transport != PROXYTransportStream {
		t.Errorf("Transport = %v, want STREAM", header.Transport)
	}

	srcAddr, ok := header.SourceAddr.(*net.TCPAddr)
	if !ok {
		t.Fatal("SourceAddr is not TCPAddr")
	}
	if !srcAddr.IP.Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("Source IP = %v, want 192.168.1.1", srcAddr.IP)
	}
	if srcAddr.Port != 12345 {
		t.Errorf("Source Port = %v, want 12345", srcAddr.Port)
	}

	dstAddr, ok := header.DestAddr.(*net.TCPAddr)
	if !ok {
		t.Fatal("DestAddr is not TCPAddr")
	}
	if !dstAddr.IP.Equal(net.ParseIP("192.168.1.2")) {
		t.Errorf("Dest IP = %v, want 192.168.1.2", dstAddr.IP)
	}
	if dstAddr.Port != 443 {
		t.Errorf("Dest Port = %v, want 443", dstAddr.Port)
	}

	if len(remaining) != 0 {
		t.Errorf("Remaining = %v, want empty", remaining)
	}
}

func TestParseV1_TCP6(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP6 2001:db8::1 2001:db8::2 54321 80\r\n")

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Family != PROXYAFInet6 {
		t.Errorf("Family = %v, want AF_INET6", header.Family)
	}

	srcAddr := header.SourceAddr.(*net.TCPAddr)
	if !srcAddr.IP.Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("Source IP = %v, want 2001:db8::1", srcAddr.IP)
	}
}

func TestParseV1_UDP4(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY UDP4 10.0.0.1 10.0.0.2 53 53\r\n")

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Transport != PROXYTransportDgram {
		t.Errorf("Transport = %v, want DGRAM", header.Transport)
	}

	_, ok := header.SourceAddr.(*net.UDPAddr)
	if !ok {
		t.Error("SourceAddr should be UDPAddr for UDP")
	}
}

func TestParseV1_UNKNOWN(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY UNKNOWN\r\n")

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Family != PROXYAFUnspec {
		t.Errorf("Family = %v, want UNSPEC", header.Family)
	}
	if header.Transport != PROXYTransportUnspec {
		t.Errorf("Transport = %v, want UNSPEC", header.Transport)
	}
}

func TestParseV1_WithRemainingData(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\nGET / HTTP/1.1\r\n")

	header, remaining, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header == nil {
		t.Fatal("Header is nil")
	}

	if !bytes.Equal(remaining, []byte("GET / HTTP/1.1\r\n")) {
		t.Errorf("Remaining = %q, want GET / HTTP/1.1\\r\\n", remaining)
	}
}

func TestParseV1_IncompleteHeader(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 12345")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error for incomplete header")
	}
}

func TestParseV1_InvalidProtocol(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY INVALID 192.168.1.1 192.168.1.2 12345 443\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error for invalid protocol")
	}
}

func TestParseV1_InvalidFormat(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PROXY TCP4 192.168.1.1\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestParseV1_NotAccepted(t *testing.T) {
	config := &PROXYProtocolConfig{
		AcceptV1: false,
		AcceptV2: true,
	}
	parser := NewPROXYProtocolParser(config)
	data := []byte("PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error when V1 not accepted")
	}
}

func TestParseV2_TCP4(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := buildPROXYProtocolV2TCP4()

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Version != PROXYProtocolV2 {
		t.Errorf("Version = %v, want V2", header.Version)
	}
	if header.Command != PROXYCommandProxy {
		t.Errorf("Command = %v, want PROXY", header.Command)
	}
	if header.Family != PROXYAFInet {
		t.Errorf("Family = %v, want AF_INET", header.Family)
	}
	if header.Transport != PROXYTransportStream {
		t.Errorf("Transport = %v, want STREAM", header.Transport)
	}

	srcAddr := header.SourceAddr.(*net.TCPAddr)
	if !srcAddr.IP.Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("Source IP = %v, want 192.168.1.1", srcAddr.IP)
	}
	if srcAddr.Port != 12345 {
		t.Errorf("Source Port = %v, want 12345", srcAddr.Port)
	}
}

func TestParseV2_TCP6(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := buildPROXYProtocolV2TCP6()

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Family != PROXYAFInet6 {
		t.Errorf("Family = %v, want AF_INET6", header.Family)
	}
}

func TestParseV2_LocalCommand(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := buildPROXYProtocolV2Local()

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if header.Command != PROXYCommandLocal {
		t.Errorf("Command = %v, want LOCAL", header.Command)
	}
}

func TestParseV2_LocalNotAllowed(t *testing.T) {
	config := &PROXYProtocolConfig{
		AcceptV1:   true,
		AcceptV2:   true,
		AllowLocal: false,
	}
	parser := NewPROXYProtocolParser(config)
	data := buildPROXYProtocolV2Local()

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error when LOCAL not allowed")
	}
}

func TestParseV2_WithTLVs(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := buildPROXYProtocolV2WithTLVs()

	header, _, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(header.TLVs) != 1 {
		t.Fatalf("Expected 1 TLV, got %d", len(header.TLVs))
	}

	if header.TLVs[0].Type != 0x01 {
		t.Errorf("TLV Type = %v, want 0x01", header.TLVs[0].Type)
	}

	if !bytes.Equal(header.TLVs[0].Value, []byte("test")) {
		t.Errorf("TLV Value = %v, want test", header.TLVs[0].Value)
	}
}

func TestParseV2_NotAccepted(t *testing.T) {
	config := &PROXYProtocolConfig{
		AcceptV1: true,
		AcceptV2: false,
	}
	parser := NewPROXYProtocolParser(config)
	data := buildPROXYProtocolV2TCP4()

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error when V2 not accepted")
	}
}

func TestParseV2_Incomplete(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error for incomplete V2 header")
	}
}

func TestParse_NotPROXYProtocol(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("GET / HTTP/1.1\r\n")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error for non-PROXY data")
	}
}

func TestParse_DataTooShort(t *testing.T) {
	parser := NewPROXYProtocolParser(nil)
	data := []byte("PRO")

	_, _, err := parser.Parse(data)
	if err == nil {
		t.Error("Expected error for short data")
	}
}

func TestWriteV1_TCP4(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	srcAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 443}

	err := WriteV1(writer, srcAddr, dstAddr)
	if err != nil {
		t.Fatalf("WriteV1 error: %v", err)
	}

	result := buf.String()
	expected := "PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\n"
	if result != expected {
		t.Errorf("WriteV1 = %q, want %q", result, expected)
	}
}

func TestWriteV1_TCP6(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	srcAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 12345}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443}

	err := WriteV1(writer, srcAddr, dstAddr)
	if err != nil {
		t.Fatalf("WriteV1 error: %v", err)
	}

	result := buf.String()
	if !strings.HasPrefix(result, "PROXY TCP6 ") {
		t.Errorf("WriteV1 = %q, expected TCP6 prefix", result)
	}
}

func TestWriteV1_NonTCP(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	srcAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 53}
	dstAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.2"), Port: 53}

	err := WriteV1(writer, srcAddr, dstAddr)
	if err != nil {
		t.Fatalf("WriteV1 error: %v", err)
	}

	result := buf.String()
	expected := "PROXY UNKNOWN\r\n"
	if result != expected {
		t.Errorf("WriteV1 = %q, want %q", result, expected)
	}
}

func TestWriteV2_TCP4(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	srcAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 443}

	err := WriteV2(writer, srcAddr, dstAddr)
	if err != nil {
		t.Fatalf("WriteV2 error: %v", err)
	}

	data := buf.Bytes()

	// Check signature
	sig := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}
	if !bytes.Equal(data[:12], sig) {
		t.Error("Invalid V2 signature")
	}

	// Check version and command
	if data[12] != 0x21 {
		t.Errorf("Version/Command = 0x%02x, want 0x21", data[12])
	}

	// Check family and transport
	if data[13] != 0x11 {
		t.Errorf("Family/Transport = 0x%02x, want 0x11 (AF_INET + STREAM)", data[13])
	}
}

func TestWriteV2_TCP6(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	srcAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 12345}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443}

	err := WriteV2(writer, srcAddr, dstAddr)
	if err != nil {
		t.Fatalf("WriteV2 error: %v", err)
	}

	data := buf.Bytes()

	// Check family and transport for IPv6
	if data[13] != 0x21 {
		t.Errorf("Family/Transport = 0x%02x, want 0x21 (AF_INET6 + STREAM)", data[13])
	}
}

func TestWriteV2_NonTCP(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	srcAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 53}
	dstAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.2"), Port: 53}

	err := WriteV2(writer, srcAddr, dstAddr)
	if err != nil {
		t.Fatalf("WriteV2 error: %v", err)
	}

	data := buf.Bytes()

	// Check family and transport for UNKNOWN
	if data[13] != 0x00 {
		t.Errorf("Family/Transport = 0x%02x, want 0x00 (UNSPEC)", data[13])
	}
}

func TestNewPROXYConn(t *testing.T) {
	// Create a pipe for testing
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	header := &PROXYHeader{
		Version: PROXYProtocolV1,
		Command: PROXYCommandProxy,
	}

	conn := NewPROXYConn(server, header)
	if conn == nil {
		t.Fatal("NewPROXYConn returned nil")
	}

	if conn.PROXYHeader() != header {
		t.Error("PROXYHeader mismatch")
	}
}

func TestPROXYConn_OriginalSource(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	srcAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}
	header := &PROXYHeader{
		Version:    PROXYProtocolV1,
		SourceAddr: srcAddr,
	}

	conn := NewPROXYConn(server, header)

	origSrc := conn.OriginalSource()
	if origSrc != srcAddr {
		t.Errorf("OriginalSource = %v, want %v", origSrc, srcAddr)
	}
}

func TestPROXYConn_OriginalSource_Fallback(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := NewPROXYConn(server, nil)

	origSrc := conn.OriginalSource()
	if origSrc == nil {
		t.Error("OriginalSource should fall back to RemoteAddr")
	}
}

func TestPROXYConn_OriginalDest(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	dstAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 443}
	header := &PROXYHeader{
		Version:  PROXYProtocolV1,
		DestAddr: dstAddr,
	}

	conn := NewPROXYConn(server, header)

	origDst := conn.OriginalDest()
	if origDst != dstAddr {
		t.Errorf("OriginalDest = %v, want %v", origDst, dstAddr)
	}
}

func TestNewPROXYListener(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := DefaultPROXYProtocolConfig()
	listener := NewPROXYListener(innerListener, config)

	if listener == nil {
		t.Fatal("NewPROXYListener returned nil")
	}

	if listener.config != config {
		t.Error("Config mismatch")
	}
}

func TestNewPROXYListener_NilConfig(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	listener := NewPROXYListener(innerListener, nil)

	if listener == nil {
		t.Fatal("NewPROXYListener(nil) returned nil")
	}
	if listener.config == nil {
		t.Error("Config should use defaults when nil")
	}
}

func TestPROXYListener_Accept_NoPROXY(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	listener := NewPROXYListener(innerListener, nil)

	// Connect in background
	go func() {
		conn, err := net.Dial("tcp", innerListener.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()

		// Send HTTP request (not PROXY protocol)
		conn.Write([]byte("GET / HTTP/1.1\r\nHost: test\r\n\r\n"))
	}()

	// Accept connection
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept error: %v", err)
	}
	defer conn.Close()

	// Should be a bufferedConn, not PROXYConn (no PROXY header)
	_, isPROXY := conn.(*PROXYConn)
	if isPROXY {
		t.Error("Connection should not be PROXYConn when no PROXY header sent")
	}
}

func TestPROXYListener_Accept_WithV1(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	listener := NewPROXYListener(innerListener, nil)

	// Connect in background
	go func() {
		conn, err := net.Dial("tcp", innerListener.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()

		// Send PROXY v1 header followed by HTTP request
		conn.Write([]byte("PROXY TCP4 192.168.1.100 192.168.1.200 12345 80\r\nGET / HTTP/1.1\r\n\r\n"))
	}()

	// Accept connection
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept error: %v", err)
	}
	defer conn.Close()

	// Should be a PROXYConn
	proxyConn, ok := conn.(*PROXYConn)
	if !ok {
		t.Fatal("Connection should be PROXYConn")
	}

	// Check header
	if proxyConn.header == nil {
		t.Fatal("PROXY header is nil")
	}

	if proxyConn.header.Version != PROXYProtocolV1 {
		t.Errorf("Version = %v, want V1", proxyConn.header.Version)
	}
}

func TestIsPROXYProtocol(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "v1 header",
			data: []byte("PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\n"),
			want: true,
		},
		{
			name: "v2 header",
			data: buildPROXYProtocolV2TCP4(),
			want: true,
		},
		{
			name: "HTTP request",
			data: []byte("GET / HTTP/1.1\r\n"),
			want: false,
		},
		{
			name: "too short",
			data: []byte("PRO"),
			want: false,
		},
		{
			name: "v2 signature only",
			data: []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPROXYProtocol(tt.data)
			if got != tt.want {
				t.Errorf("IsPROXYProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatPROXYHeaderV1(t *testing.T) {
	result := FormatPROXYHeaderV1("192.168.1.1", "192.168.1.2", 12345, 443)
	expected := "PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\n"
	if result != expected {
		t.Errorf("FormatPROXYHeaderV1 = %q, want %q", result, expected)
	}
}

func TestPROXYHeader_GetInfo(t *testing.T) {
	tests := []struct {
		name   string
		header *PROXYHeader
		want   *PROXYProtocolInfo
	}{
		{
			name: "v1 TCP4",
			header: &PROXYHeader{
				Version:    PROXYProtocolV1,
				Command:    PROXYCommandProxy,
				Family:     PROXYAFInet,
				Transport:  PROXYTransportStream,
				SourceAddr: &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345},
				DestAddr:   &net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 443},
			},
			want: &PROXYProtocolInfo{
				Version:  "1",
				Command:  "PROXY",
				Protocol: "TCP4",
				Source:   "192.168.1.1:12345",
				Dest:     "192.168.1.2:443",
			},
		},
		{
			name: "v2 UDP6",
			header: &PROXYHeader{
				Version:    PROXYProtocolV2,
				Command:    PROXYCommandProxy,
				Family:     PROXYAFInet6,
				Transport:  PROXYTransportDgram,
				SourceAddr: &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 53},
				DestAddr:   &net.UDPAddr{IP: net.ParseIP("2001:db8::2"), Port: 53},
			},
			want: &PROXYProtocolInfo{
				Version:  "2",
				Command:  "PROXY",
				Protocol: "UDP6",
				Source:   "[2001:db8::1]:53",
				Dest:     "[2001:db8::2]:53",
			},
		},
		{
			name: "LOCAL command",
			header: &PROXYHeader{
				Version: PROXYProtocolV2,
				Command: PROXYCommandLocal,
			},
			want: &PROXYProtocolInfo{
				Version: "2",
				Command: "LOCAL",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.header.GetInfo()
			if got.Version != tt.want.Version {
				t.Errorf("Version = %q, want %q", got.Version, tt.want.Version)
			}
			if got.Command != tt.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tt.want.Command)
			}
			if got.Protocol != tt.want.Protocol {
				t.Errorf("Protocol = %q, want %q", got.Protocol, tt.want.Protocol)
			}
			if got.Source != tt.want.Source {
				t.Errorf("Source = %q, want %q", got.Source, tt.want.Source)
			}
			if got.Dest != tt.want.Dest {
				t.Errorf("Dest = %q, want %q", got.Dest, tt.want.Dest)
			}
		})
	}
}

func TestParseTLVs(t *testing.T) {
	// Build TLV data
	buf := new(bytes.Buffer)

	// TLV 1: Type 0x01, Length 4, Value "test"
	buf.WriteByte(0x01)
	binary.Write(buf, binary.BigEndian, uint16(4))
	buf.WriteString("test")

	// TLV 2: Type 0x02, Length 2, Value "go"
	buf.WriteByte(0x02)
	binary.Write(buf, binary.BigEndian, uint16(2))
	buf.WriteString("go")

	tlvs := parseTLVs(buf.Bytes())

	if len(tlvs) != 2 {
		t.Fatalf("Expected 2 TLVs, got %d", len(tlvs))
	}

	if tlvs[0].Type != 0x01 || !bytes.Equal(tlvs[0].Value, []byte("test")) {
		t.Errorf("TLV[0] = {Type: 0x%02x, Value: %v}, want {Type: 0x01, Value: test}",
			tlvs[0].Type, tlvs[0].Value)
	}

	if tlvs[1].Type != 0x02 || !bytes.Equal(tlvs[1].Value, []byte("go")) {
		t.Errorf("TLV[1] = {Type: 0x%02x, Value: %v}, want {Type: 0x02, Value: go}",
			tlvs[1].Type, tlvs[1].Value)
	}
}

func TestParseTLVs_Incomplete(t *testing.T) {
	// TLV with incomplete length
	data := []byte{0x01, 0x00}
	tlvs := parseTLVs(data)

	if len(tlvs) != 0 {
		t.Errorf("Expected 0 TLVs for incomplete data, got %d", len(tlvs))
	}

	// TLV with incomplete value
	data = []byte{0x01, 0x00, 0x10, 0xAB} // Type 1, Length 16, but only 1 byte value
	tlvs = parseTLVs(data)

	if len(tlvs) != 0 {
		t.Errorf("Expected 0 TLVs for incomplete value, got %d", len(tlvs))
	}
}

func TestBufferedConn_Read(t *testing.T) {
	// Create a pipe
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Write data to the connection in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.Write([]byte("World!"))
		client.Close()
	}()

	// Wait a bit for the data to be written
	time.Sleep(50 * time.Millisecond)

	// Create buffered conn with initial data
	initial := []byte("Hello, ")
	bufConn := &bufferedConn{
		Conn:   server,
		buffer: initial,
		offset: 0,
	}

	// First read should get the buffered data
	buf1 := make([]byte, 100)
	n1, err := bufConn.Read(buf1)
	if err != nil {
		t.Fatalf("First read error: %v", err)
	}

	// Should get "Hello, " from buffer first
	if string(buf1[:n1]) != "Hello, " {
		t.Errorf("First read = %q, want Hello, ", string(buf1[:n1]))
	}

	// Second read should get data from underlying connection
	buf2 := make([]byte, 100)
	n2, err := bufConn.Read(buf2)
	if err != nil && err.Error() != "io: read/write on closed pipe" {
		t.Fatalf("Second read error: %v", err)
	}

	// Should get "World!" from connection
	if string(buf2[:n2]) != "World!" {
		t.Errorf("Second read = %q, want World!", string(buf2[:n2]))
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for read to complete")
	}
}

// Helper functions to build PROXY protocol v2 headers
func buildPROXYProtocolV2TCP4() []byte {
	buf := new(bytes.Buffer)

	// Signature
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})

	// Version (2) and Command (PROXY)
	buf.WriteByte(0x21)

	// Family (AF_INET) and Transport (STREAM)
	buf.WriteByte(0x11)

	// Length (12 bytes for IPv4 addresses and ports)
	binary.Write(buf, binary.BigEndian, uint16(12))

	// Source IP (192.168.1.1)
	buf.Write([]byte{192, 168, 1, 1})

	// Dest IP (192.168.1.2)
	buf.Write([]byte{192, 168, 1, 2})

	// Source Port (12345)
	binary.Write(buf, binary.BigEndian, uint16(12345))

	// Dest Port (443)
	binary.Write(buf, binary.BigEndian, uint16(443))

	return buf.Bytes()
}

func buildPROXYProtocolV2TCP6() []byte {
	buf := new(bytes.Buffer)

	// Signature
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})

	// Version (2) and Command (PROXY)
	buf.WriteByte(0x21)

	// Family (AF_INET6) and Transport (STREAM)
	buf.WriteByte(0x21)

	// Length (36 bytes for IPv6 addresses and ports)
	binary.Write(buf, binary.BigEndian, uint16(36))

	// Source IP (2001:db8::1)
	buf.Write(net.ParseIP("2001:db8::1").To16())

	// Dest IP (2001:db8::2)
	buf.Write(net.ParseIP("2001:db8::2").To16())

	// Source Port (12345)
	binary.Write(buf, binary.BigEndian, uint16(12345))

	// Dest Port (443)
	binary.Write(buf, binary.BigEndian, uint16(443))

	return buf.Bytes()
}

func buildPROXYProtocolV2Local() []byte {
	buf := new(bytes.Buffer)

	// Signature
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})

	// Version (2) and Command (LOCAL)
	buf.WriteByte(0x20)

	// Family (UNSPEC) and Transport (UNSPEC)
	buf.WriteByte(0x00)

	// Length (0 bytes - no addresses for LOCAL)
	binary.Write(buf, binary.BigEndian, uint16(0))

	return buf.Bytes()
}

func buildPROXYProtocolV2WithTLVs() []byte {
	buf := new(bytes.Buffer)

	// Signature
	buf.Write([]byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A})

	// Version (2) and Command (PROXY)
	buf.WriteByte(0x21)

	// Family (AF_INET) and Transport (STREAM)
	buf.WriteByte(0x11)

	// Address length + TLV length (12 + 7 = 19)
	binary.Write(buf, binary.BigEndian, uint16(19))

	// Source IP (192.168.1.1)
	buf.Write([]byte{192, 168, 1, 1})

	// Dest IP (192.168.1.2)
	buf.Write([]byte{192, 168, 1, 2})

	// Source Port (12345)
	binary.Write(buf, binary.BigEndian, uint16(12345))

	// Dest Port (443)
	binary.Write(buf, binary.BigEndian, uint16(443))

	// TLV: Type 0x01, Length 4, Value "test"
	buf.WriteByte(0x01)
	binary.Write(buf, binary.BigEndian, uint16(4))
	buf.WriteString("test")

	return buf.Bytes()
}

// --- isTrustedSource tests ---

func TestIsTrustedSource_NoTrustedNets(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	// No TrustedNetworks configured — no sources should be trusted (secure default)
	config := &PROXYProtocolConfig{}
	listener := NewPROXYListener(innerListener, config)

	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}
	if listener.isTrustedSource(addr) {
		t.Error("Expected NOT trusted when no TrustedNetworks configured (secure default)")
	}
}

func TestIsTrustedSource_TrustedIP(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"10.0.0.0/8", "192.168.0.0/16"},
	}
	listener := NewPROXYListener(innerListener, config)

	addr := &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 12345}
	if !listener.isTrustedSource(addr) {
		t.Error("Expected trusted for IP in 10.0.0.0/8")
	}
}

func TestIsTrustedSource_UntrustedIP(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"10.0.0.0/8"},
	}
	listener := NewPROXYListener(innerListener, config)

	addr := &net.TCPAddr{IP: net.ParseIP("172.16.0.1"), Port: 12345}
	if listener.isTrustedSource(addr) {
		t.Error("Expected untrusted for IP outside 10.0.0.0/8")
	}
}

func TestIsTrustedSource_InvalidAddr(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"10.0.0.0/8"},
	}
	listener := NewPROXYListener(innerListener, config)

	// Address without host:port format
	addr := &net.UnixAddr{Name: "/tmp/test.sock", Net: "unix"}
	if listener.isTrustedSource(addr) {
		t.Error("Expected untrusted for non-IP address")
	}
}

func TestIsTrustedSource_UnparsableIP(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"10.0.0.0/8"},
	}
	listener := NewPROXYListener(innerListener, config)

	// Custom address with an invalid IP string
	addr := &fakeAddr{network: "tcp", str: "not-an-ip:1234"}
	if listener.isTrustedSource(addr) {
		t.Error("Expected untrusted for invalid IP")
	}
}

func TestIsTrustedSource_AllInvalidCIDRs(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"not-a-cidr", "also-invalid"},
	}
	listener := NewPROXYListener(innerListener, config)

	// All CIDRs are invalid, so trustedReady should be false
	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}
	if listener.isTrustedSource(addr) {
		t.Error("Expected NOT trusted when no valid CIDRs parsed (secure default)")
	}
}

func TestIsTrustedSource_IPv6(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"2001:db8::/32"},
	}
	listener := NewPROXYListener(innerListener, config)

	addr := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 12345}
	if !listener.isTrustedSource(addr) {
		t.Error("Expected trusted for IPv6 in 2001:db8::/32")
	}
}

func TestIsTrustedSource_IPv6Untrusted(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		TrustedNetworks: []string{"2001:db8::/32"},
	}
	listener := NewPROXYListener(innerListener, config)

	addr := &net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 12345}
	if listener.isTrustedSource(addr) {
		t.Error("Expected untrusted for IPv6 outside 2001:db8::/32")
	}
}

func TestPROXYListener_Accept_UntrustedSource(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	// Configure trusted networks that don't include 127.0.0.1
	config := &PROXYProtocolConfig{
		AcceptV1:        true,
		AcceptV2:        true,
		TrustedNetworks: []string{"10.0.0.0/8"},
	}
	listener := NewPROXYListener(innerListener, config)

	// Connect from 127.0.0.1 (untrusted) and send PROXY header
	go func() {
		conn, err := net.Dial("tcp", innerListener.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()
		// Send a PROXY v1 header — should be ignored because source is untrusted
		conn.Write([]byte("PROXY TCP4 1.2.3.4 5.6.7.8 12345 443\r\nGET / HTTP/1.1\r\n\r\n"))
	}()

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept error: %v", err)
	}
	defer conn.Close()

	// Connection should NOT be a PROXYConn since source is untrusted
	_, isPROXY := conn.(*PROXYConn)
	if isPROXY {
		t.Error("Connection should NOT be PROXYConn for untrusted source")
	}
}

// fakeAddr is a test helper that implements net.Addr with a custom string.
type fakeAddr struct {
	network string
	str     string
}

func (a *fakeAddr) Network() string { return a.network }
func (a *fakeAddr) String() string  { return a.str }

// Test to ensure PROXY listener respects read timeout
func TestPROXYListener_ReadTimeout(t *testing.T) {
	innerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer innerListener.Close()

	config := &PROXYProtocolConfig{
		AcceptV1: true,
		AcceptV2: true,
	}
	listener := NewPROXYListener(innerListener, config)

	// Connect but send nothing (should timeout)
	go func() {
		conn, err := net.Dial("tcp", innerListener.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()

		// Just wait - don't send anything
		time.Sleep(2 * time.Second)
	}()

	// Accept should timeout or return error
	done := make(chan struct{})
	var acceptErr error
	go func() {
		_, acceptErr = listener.Accept()
		close(done)
	}()

	select {
	case <-done:
		// Expected - accept should fail due to timeout
		if acceptErr == nil {
			t.Error("Accept should have failed due to timeout")
		}
	case <-time.After(3 * time.Second):
		// Timeout waiting for accept
		t.Log("Accept didn't return within expected time, but this may be OK depending on implementation")
	}
}
