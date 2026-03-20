package l4

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// PROXYProtocolVersion represents the PROXY protocol version.
type PROXYProtocolVersion int

const (
	// PROXYProtocolV1 is the human-readable text format.
	PROXYProtocolV1 PROXYProtocolVersion = 1
	// PROXYProtocolV2 is the binary format.
	PROXYProtocolV2 PROXYProtocolVersion = 2
)

// PROXYProtocolCommand represents the PROXY protocol command.
type PROXYProtocolCommand byte

const (
	// PROXYCommandProxy represents a proxied connection.
	PROXYCommandProxy PROXYProtocolCommand = 0x01
	// PROXYCommandLocal represents a local health check.
	PROXYCommandLocal PROXYProtocolCommand = 0x00
)

// PROXYProtocolFamily represents the address family.
type PROXYProtocolFamily byte

const (
	// PROXYAFUnspec is unspecified.
	PROXYAFUnspec PROXYProtocolFamily = 0x00
	// PROXYAFInet is IPv4.
	PROXYAFInet PROXYProtocolFamily = 0x10
	// PROXYAFInet6 is IPv6.
	PROXYAFInet6 PROXYProtocolFamily = 0x20
	// PROXYAFUnix is UNIX socket.
	PROXYAFUnix PROXYProtocolFamily = 0x30
)

// PROXYProtocolTransport represents the transport protocol.
type PROXYProtocolTransport byte

const (
	// PROXYTransportUnspec is unspecified.
	PROXYTransportUnspec PROXYProtocolTransport = 0x00
	// PROXYTransportStream is TCP.
	PROXYTransportStream PROXYProtocolTransport = 0x01
	// PROXYTransportDgram is UDP.
	PROXYTransportDgram PROXYProtocolTransport = 0x02
)

// PROXYHeader represents a PROXY protocol header.
type PROXYHeader struct {
	Version    PROXYProtocolVersion
	Command    PROXYProtocolCommand
	Family     PROXYProtocolFamily
	Transport  PROXYProtocolTransport
	SourceAddr net.Addr
	DestAddr   net.Addr
	TLVs       []PROXYTLV
}

// PROXYTLV represents a Type-Length-Value entry.
type PROXYTLV struct {
	Type  byte
	Value []byte
}

// PROXYProtocolConfig configures PROXY protocol handling.
type PROXYProtocolConfig struct {
	// AcceptV1 enables accepting PROXY protocol v1.
	AcceptV1 bool
	// AcceptV2 enables accepting PROXY protocol v2.
	AcceptV2 bool
	// SendV1 enables sending PROXY protocol v1.
	SendV1 bool
	// SendV2 enables sending PROXY protocol v2.
	SendV2 bool
	// AllowLocal allows LOCAL command (health checks).
	AllowLocal bool
	// OverrideTo allows overriding the destination address.
	OverrideTo string
}

// DefaultPROXYProtocolConfig returns a default configuration.
func DefaultPROXYProtocolConfig() *PROXYProtocolConfig {
	return &PROXYProtocolConfig{
		AcceptV1:   true,
		AcceptV2:   true,
		AllowLocal: true,
	}
}

// PROXYProtocolParser parses PROXY protocol headers.
type PROXYProtocolParser struct {
	config *PROXYProtocolConfig
}

// NewPROXYProtocolParser creates a new parser.
func NewPROXYProtocolParser(config *PROXYProtocolConfig) *PROXYProtocolParser {
	if config == nil {
		config = DefaultPROXYProtocolConfig()
	}
	return &PROXYProtocolParser{config: config}
}

// Parse parses a PROXY protocol header from a reader.
func (p *PROXYProtocolParser) Parse(data []byte) (*PROXYHeader, []byte, error) {
	if len(data) < 5 {
		return nil, data, errors.New("data too short")
	}

	// Check for v2 signature
	if isPROXYProtocolV2(data) {
		if !p.config.AcceptV2 {
			return nil, data, errors.New("proxy protocol v2 not accepted")
		}
		return p.parseV2(data)
	}

	// Check for v1 signature
	if isPROXYProtocolV1(data) {
		if !p.config.AcceptV1 {
			return nil, data, errors.New("proxy protocol v1 not accepted")
		}
		return p.parseV1(data)
	}

	return nil, data, errors.New("not a PROXY protocol header")
}

// isPROXYProtocolV1 checks if data is a PROXY protocol v1 header.
func isPROXYProtocolV1(data []byte) bool {
	return len(data) >= 5 && string(data[:5]) == "PROXY"
}

// isPROXYProtocolV2 checks if data is a PROXY protocol v2 header.
func isPROXYProtocolV2(data []byte) bool {
	return len(data) >= 12 &&
		data[0] == 0x0D && data[1] == 0x0A && data[2] == 0x0D && data[3] == 0x0A &&
		data[4] == 0x00 && data[5] == 0x0D && data[6] == 0x0A && data[7] == 0x51 &&
		data[8] == 0x55 && data[9] == 0x49 && data[10] == 0x54 && data[11] == 0x0A
}

// parseV1 parses a PROXY protocol v1 header.
func (p *PROXYProtocolParser) parseV1(data []byte) (*PROXYHeader, []byte, error) {
	// Find end of header (CRLF)
	crlfIdx := bytes.Index(data, []byte("\r\n"))
	if crlfIdx == -1 {
		return nil, data, errors.New("incomplete PROXY v1 header")
	}

	headerLine := string(data[:crlfIdx])
	remaining := data[crlfIdx+2:]

	// Parse: PROXY TCP4 192.168.1.1 192.168.1.2 12345 443\r\n
	parts := strings.Split(headerLine, " ")
	if len(parts) < 2 {
		return nil, data, errors.New("invalid PROXY v1 header")
	}

	header := &PROXYHeader{
		Version: PROXYProtocolV1,
		Command: PROXYCommandProxy,
	}

	// Check for UNKNOWN
	if parts[1] == "UNKNOWN" {
		header.Family = PROXYAFUnspec
		header.Transport = PROXYTransportUnspec
		return header, remaining, nil
	}

	// Parse protocol
	switch parts[1] {
	case "TCP4":
		header.Family = PROXYAFInet
		header.Transport = PROXYTransportStream
	case "TCP6":
		header.Family = PROXYAFInet6
		header.Transport = PROXYTransportStream
	case "UDP4":
		header.Family = PROXYAFInet
		header.Transport = PROXYTransportDgram
	case "UDP6":
		header.Family = PROXYAFInet6
		header.Transport = PROXYTransportDgram
	default:
		return nil, data, fmt.Errorf("unknown protocol: %s", parts[1])
	}

	if len(parts) != 6 {
		return nil, data, errors.New("invalid PROXY v1 header format")
	}

	// Parse addresses
	srcIP := net.ParseIP(parts[2])
	dstIP := net.ParseIP(parts[3])
	srcPort, _ := strconv.Atoi(parts[4])
	dstPort, _ := strconv.Atoi(parts[5])

	if srcIP == nil || dstIP == nil {
		return nil, data, errors.New("invalid IP address")
	}

	// Create appropriate address type based on transport protocol
	if header.Transport == PROXYTransportDgram {
		header.SourceAddr = &net.UDPAddr{IP: srcIP, Port: srcPort}
		header.DestAddr = &net.UDPAddr{IP: dstIP, Port: dstPort}
	} else {
		header.SourceAddr = &net.TCPAddr{IP: srcIP, Port: srcPort}
		header.DestAddr = &net.TCPAddr{IP: dstIP, Port: dstPort}
	}

	return header, remaining, nil
}

// parseV2 parses a PROXY protocol v2 header.
func (p *PROXYProtocolParser) parseV2(data []byte) (*PROXYHeader, []byte, error) {
	if len(data) < 16 {
		return nil, data, errors.New("data too short for PROXY v2 header")
	}

	header := &PROXYHeader{
		Version: PROXYProtocolV2,
	}

	// Skip signature (12 bytes)
	// Version and command (1 byte)
	verCmd := data[12]
	header.Command = PROXYProtocolCommand(verCmd & 0x0F)

	if header.Command != PROXYCommandProxy && header.Command != PROXYCommandLocal {
		return nil, data, errors.New("invalid command")
	}

	if header.Command == PROXYCommandLocal && !p.config.AllowLocal {
		return nil, data, errors.New("local command not allowed")
	}

	// Family and transport (1 byte)
	famTrans := data[13]
	header.Family = PROXYProtocolFamily(famTrans & 0xF0)
	header.Transport = PROXYProtocolTransport(famTrans & 0x0F)

	// Length (2 bytes, big-endian)
	length := binary.BigEndian.Uint16(data[14:16])

	if len(data) < 16+int(length) {
		return nil, data, errors.New("incomplete PROXY v2 header")
	}

	addrData := data[16 : 16+length]
	remaining := data[16+length:]

	// Parse addresses based on family
	switch header.Family {
	case PROXYAFInet:
		if len(addrData) < 12 {
			return nil, data, errors.New("insufficient data for IPv4")
		}
		srcIP := net.IP(addrData[0:4])
		dstIP := net.IP(addrData[4:8])
		srcPort := binary.BigEndian.Uint16(addrData[8:10])
		dstPort := binary.BigEndian.Uint16(addrData[10:12])

		if header.Transport == PROXYTransportStream {
			header.SourceAddr = &net.TCPAddr{IP: srcIP, Port: int(srcPort)}
			header.DestAddr = &net.TCPAddr{IP: dstIP, Port: int(dstPort)}
		} else {
			header.SourceAddr = &net.UDPAddr{IP: srcIP, Port: int(srcPort)}
			header.DestAddr = &net.UDPAddr{IP: dstIP, Port: int(dstPort)}
		}

		// Parse TLVs if present
		if len(addrData) > 12 {
			header.TLVs = parseTLVs(addrData[12:])
		}

	case PROXYAFInet6:
		if len(addrData) < 36 {
			return nil, data, errors.New("insufficient data for IPv6")
		}
		srcIP := net.IP(addrData[0:16])
		dstIP := net.IP(addrData[16:32])
		srcPort := binary.BigEndian.Uint16(addrData[32:34])
		dstPort := binary.BigEndian.Uint16(addrData[34:36])

		if header.Transport == PROXYTransportStream {
			header.SourceAddr = &net.TCPAddr{IP: srcIP, Port: int(srcPort)}
			header.DestAddr = &net.TCPAddr{IP: dstIP, Port: int(dstPort)}
		} else {
			header.SourceAddr = &net.UDPAddr{IP: srcIP, Port: int(srcPort)}
			header.DestAddr = &net.UDPAddr{IP: dstIP, Port: int(dstPort)}
		}

		if len(addrData) > 36 {
			header.TLVs = parseTLVs(addrData[36:])
		}

	case PROXYAFUnix:
		if len(addrData) < 216 {
			return nil, data, errors.New("insufficient data for UNIX")
		}
		// UNIX sockets - just skip for now
	}

	return header, remaining, nil
}

// parseTLVs parses TLV entries.
func parseTLVs(data []byte) []PROXYTLV {
	var tlvs []PROXYTLV
	offset := 0

	for offset < len(data) {
		if offset+3 > len(data) {
			break
		}

		tlvType := data[offset]
		tlvLen := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))

		if offset+3+tlvLen > len(data) {
			break
		}

		tlv := PROXYTLV{
			Type:  tlvType,
			Value: data[offset+3 : offset+3+tlvLen],
		}
		tlvs = append(tlvs, tlv)

		offset += 3 + tlvLen
	}

	return tlvs
}

// WriteV1 writes a PROXY protocol v1 header.
func WriteV1(w *bufio.Writer, srcAddr, dstAddr net.Addr) error {
	srcTCP, srcOK := srcAddr.(*net.TCPAddr)
	dstTCP, dstOK := dstAddr.(*net.TCPAddr)

	if !srcOK || !dstOK {
		// Write UNKNOWN for non-TCP
		_, err := w.WriteString("PROXY UNKNOWN\r\n")
		if err != nil {
			return err
		}
		return w.Flush()
	}

	var proto string
	if srcTCP.IP.To4() != nil {
		proto = "TCP4"
	} else {
		proto = "TCP6"
	}

	_, err := fmt.Fprintf(w, "PROXY %s %s %s %d %d\r\n",
		proto,
		srcTCP.IP.String(),
		dstTCP.IP.String(),
		srcTCP.Port,
		dstTCP.Port,
	)
	if err != nil {
		return err
	}

	return w.Flush()
}

// WriteV2 writes a PROXY protocol v2 header.
func WriteV2(w *bufio.Writer, srcAddr, dstAddr net.Addr) error {
	// Signature
	sig := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}
	w.Write(sig)

	srcTCP, srcOK := srcAddr.(*net.TCPAddr)
	dstTCP, dstOK := dstAddr.(*net.TCPAddr)

	if !srcOK || !dstOK {
		// Write header for UNKNOWN
		w.WriteByte(0x21) // Version 2, PROXY command
		w.WriteByte(0x00) // AF_UNSPEC, UNSPEC
		binary.Write(w, binary.BigEndian, uint16(0))
		return w.Flush()
	}

	var verCmd, famTrans byte
	verCmd = 0x21 // Version 2, PROXY command

	var addrData []byte

	if srcTCP.IP.To4() != nil {
		famTrans = 0x11 // AF_INET + STREAM

		addrData = make([]byte, 12)
		copy(addrData[0:4], srcTCP.IP.To4())
		copy(addrData[4:8], dstTCP.IP.To4())
		binary.BigEndian.PutUint16(addrData[8:10], uint16(srcTCP.Port))
		binary.BigEndian.PutUint16(addrData[10:12], uint16(dstTCP.Port))
	} else {
		famTrans = 0x21 // AF_INET6 + STREAM

		addrData = make([]byte, 36)
		copy(addrData[0:16], srcTCP.IP.To16())
		copy(addrData[16:32], dstTCP.IP.To16())
		binary.BigEndian.PutUint16(addrData[32:34], uint16(srcTCP.Port))
		binary.BigEndian.PutUint16(addrData[34:36], uint16(dstTCP.Port))
	}

	w.WriteByte(verCmd)
	w.WriteByte(famTrans)
	binary.Write(w, binary.BigEndian, uint16(len(addrData)))
	w.Write(addrData)

	return w.Flush()
}

// PROXYConn wraps a connection with PROXY protocol support.
type PROXYConn struct {
	net.Conn
	header *PROXYHeader
}

// NewPROXYConn creates a new PROXY protocol connection.
func NewPROXYConn(conn net.Conn, header *PROXYHeader) *PROXYConn {
	return &PROXYConn{
		Conn:   conn,
		header: header,
	}
}

// PROXYHeader returns the parsed PROXY header.
func (c *PROXYConn) PROXYHeader() *PROXYHeader {
	return c.header
}

// OriginalSource returns the original source address.
func (c *PROXYConn) OriginalSource() net.Addr {
	if c.header != nil {
		return c.header.SourceAddr
	}
	return c.Conn.RemoteAddr()
}

// OriginalDest returns the original destination address.
func (c *PROXYConn) OriginalDest() net.Addr {
	if c.header != nil {
		return c.header.DestAddr
	}
	return c.Conn.LocalAddr()
}

// PROXYListener wraps a listener with PROXY protocol support.
type PROXYListener struct {
	net.Listener
	config *PROXYProtocolConfig
}

// NewPROXYListener creates a new PROXY protocol listener.
func NewPROXYListener(listener net.Listener, config *PROXYProtocolConfig) *PROXYListener {
	if config == nil {
		config = DefaultPROXYProtocolConfig()
	}
	return &PROXYListener{
		Listener: listener,
		config:   config,
	}
}

// Accept accepts a connection and parses the PROXY header.
func (l *PROXYListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	// Set read timeout for header parsing
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	// Peek at the data
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, err
	}
	buf = buf[:n]

	// Check if it's PROXY protocol
	parser := NewPROXYProtocolParser(l.config)
	header, remaining, err := parser.Parse(buf)
	if err != nil {
		// Not PROXY protocol, wrap with original data
		return &bufferedConn{
			Conn:   conn,
			buffer: buf,
		}, nil
	}

	// Wrap with remaining data
	return &PROXYConn{
		Conn: &bufferedConn{
			Conn:   conn,
			buffer: remaining,
		},
		header: header,
	}, nil
}

// bufferedConn wraps a connection with buffered data.
type bufferedConn struct {
	net.Conn
	buffer []byte
	offset int
}

// Read reads from the buffer first, then the connection.
func (c *bufferedConn) Read(p []byte) (n int, err error) {
	if c.offset < len(c.buffer) {
		n = copy(p, c.buffer[c.offset:])
		c.offset += n
		return n, nil
	}
	return c.Conn.Read(p)
}

// IsPROXYProtocol checks if data starts with a PROXY protocol signature.
func IsPROXYProtocol(data []byte) bool {
	return isPROXYProtocolV1(data) || isPROXYProtocolV2(data)
}

// FormatPROXYHeaderV1 formats a PROXY protocol v1 header.
func FormatPROXYHeaderV1(srcIP, dstIP string, srcPort, dstPort int) string {
	return fmt.Sprintf("PROXY TCP4 %s %s %d %d\r\n", srcIP, dstIP, srcPort, dstPort)
}

// PROXYProtocolInfo contains information about a PROXY protocol header.
type PROXYProtocolInfo struct {
	Version  string
	Command  string
	Protocol string
	Source   string
	Dest     string
}

// GetInfo returns information about the header.
func (h *PROXYHeader) GetInfo() *PROXYProtocolInfo {
	info := &PROXYProtocolInfo{
		Version: "Unknown",
	}

	if h.Version == PROXYProtocolV1 {
		info.Version = "1"
	} else if h.Version == PROXYProtocolV2 {
		info.Version = "2"
	}

	switch h.Command {
	case PROXYCommandProxy:
		info.Command = "PROXY"
	case PROXYCommandLocal:
		info.Command = "LOCAL"
	}

	switch h.Family {
	case PROXYAFInet:
		if h.Transport == PROXYTransportStream {
			info.Protocol = "TCP4"
		} else {
			info.Protocol = "UDP4"
		}
	case PROXYAFInet6:
		if h.Transport == PROXYTransportStream {
			info.Protocol = "TCP6"
		} else {
			info.Protocol = "UDP6"
		}
	}

	if h.SourceAddr != nil {
		info.Source = h.SourceAddr.String()
	}
	if h.DestAddr != nil {
		info.Dest = h.DestAddr.String()
	}

	return info
}
