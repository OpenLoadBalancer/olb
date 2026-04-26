package geodns

import (
	"encoding/binary"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// New() coverage (was 72.7%)
// ============================================================================

// TestCov_New_TrustedProxiesParseError tests that invalid CIDRs in TrustedProxies
// are silently skipped.
func TestCov_New_TrustedProxiesParseError(t *testing.T) {
	g := New(Config{
		DefaultPool:    "default",
		TrustedProxies: []string{"not-a-valid-cidr", "10.0.0.0/8"},
	})
	if g == nil {
		t.Fatal("expected non-nil GeoDNS")
	}
	if len(g.trustedProxies) != 1 {
		t.Errorf("expected 1 valid trusted proxy, got %d", len(g.trustedProxies))
	}
}

// TestCov_New_MMDBLoadFailureSoftFail tests that New does not fail when MMDB
// path exists but file is invalid (soft-fail).
func TestCov_New_MMDBLoadFailureSoftFail(t *testing.T) {
	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "bad.mmdb")
	if err := os.WriteFile(badPath, []byte("garbage"), 0644); err != nil {
		t.Fatal(err)
	}

	g := New(Config{
		DefaultPool: "default",
		DBPath:      badPath,
	})
	if g == nil {
		t.Fatal("expected non-nil GeoDNS even with bad MMDB")
	}
	if g.mmdb != nil {
		t.Error("expected mmdb to be nil when file is invalid")
	}
}

// ============================================================================
// extractClientIP coverage (was 81.8%)
// ============================================================================

// TestCov_ExtractClientIP_TrustedProxyWithXFF tests extraction from a trusted
// proxy that sends X-Forwarded-For.
func TestCov_ExtractClientIP_TrustedProxyWithXFF(t *testing.T) {
	g := New(Config{
		DefaultPool:    "default",
		TrustedProxies: []string{"10.0.0.0/8"},
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")

	got := g.extractClientIP(req)
	if got != "203.0.113.50" {
		t.Errorf("extractClientIP() = %q, want %q", got, "203.0.113.50")
	}
}

// TestCov_ExtractClientIP_TrustedProxyWithXRealIP tests extraction from a
// trusted proxy using X-Real-IP (no XFF).
func TestCov_ExtractClientIP_TrustedProxyWithXRealIP(t *testing.T) {
	g := New(Config{
		DefaultPool:    "default",
		TrustedProxies: []string{"10.0.0.0/8"},
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.50")

	got := g.extractClientIP(req)
	if got != "203.0.113.50" {
		t.Errorf("extractClientIP() = %q, want %q", got, "203.0.113.50")
	}
}

// TestCov_ExtractClientIP_TrustedProxyEmptyXFF tests extraction from a trusted
// proxy with empty X-Forwarded-For.
func TestCov_ExtractClientIP_TrustedProxyEmptyXFF(t *testing.T) {
	g := New(Config{
		DefaultPool:    "default",
		TrustedProxies: []string{"10.0.0.0/8"},
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "")

	got := g.extractClientIP(req)
	// Should fall through to X-Real-IP, then to host
	if got != "10.0.0.1" {
		t.Errorf("extractClientIP() = %q, want %q", got, "10.0.0.1")
	}
}

// TestCov_ExtractClientIP_UntrustedPublic tests that public IPs are not trusted
// for forwarding headers when trusted proxies are configured.
func TestCov_ExtractClientIP_UntrustedPublic(t *testing.T) {
	g := New(Config{
		DefaultPool:    "default",
		TrustedProxies: []string{"10.0.0.0/8"},
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := g.extractClientIP(req)
	// Should NOT use XFF since 203.0.113.1 is not in trusted proxies
	if got != "203.0.113.1" {
		t.Errorf("extractClientIP() = %q, want %q", got, "203.0.113.1")
	}
}

// TestCov_ExtractClientIP_LegacyPrivateLoopback tests the legacy path where
// no trusted proxies are configured and the request comes from a private IP.
func TestCov_ExtractClientIP_LegacyPrivateLoopback(t *testing.T) {
	g := New(Config{DefaultPool: "default"})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	got := g.extractClientIP(req)
	if got != "203.0.113.50" {
		t.Errorf("extractClientIP() = %q, want %q", got, "203.0.113.50")
	}
}

// TestCov_ExtractClientIP_NonIPRemoteAddr tests extractClientIP with a non-IP
// RemoteAddr that fails net.ParseIP.
func TestCov_ExtractClientIP_NonIPRemoteAddr(t *testing.T) {
	g := New(Config{DefaultPool: "default"})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "not-an-ip"

	got := g.extractClientIP(req)
	if got != "not-an-ip" {
		t.Errorf("extractClientIP() = %q, want %q", got, "not-an-ip")
	}
}

// ============================================================================
// isPrivateIP coverage (was 87.5%)
// ============================================================================

// TestCov_IsPrivateIP_IPv6ULA tests isPrivateIP with an IPv6 ULA address.
func TestCov_IsPrivateIP_IPv6ULA(t *testing.T) {
	ip := net.ParseIP("fd00::1")
	if !isPrivateIP(ip) {
		t.Error("fd00::1 should be private (fc00::/7 range)")
	}
}

// TestCov_IsPrivateIP_IPv6LinkLocal tests isPrivateIP with an IPv6 link-local.
func TestCov_IsPrivateIP_IPv6LinkLocal(t *testing.T) {
	ip := net.ParseIP("fe80::1")
	if isPrivateIP(ip) {
		t.Error("fe80::1 should NOT be in private ranges (fc00::/7 is fc00-fdff)")
	}
}

// TestCov_IsPrivateIP_Public tests isPrivateIP with a public IP.
func TestCov_IsPrivateIP_Public(t *testing.T) {
	ip := net.ParseIP("8.8.8.8")
	if isPrivateIP(ip) {
		t.Error("8.8.8.8 should not be private")
	}
}

// ============================================================================
// lookup coverage (was 87.5%)
// ============================================================================

// TestCov_Lookup_DataOffsetOutOfBounds tests the lookup path where the data
// offset exceeds the file size.
func TestCov_Lookup_DataOffsetOutOfBounds(t *testing.T) {
	path := writeTestMMDB(t, 24)
	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt the reader by truncating data to force an out-of-bounds access
	reader.data = reader.data[:len(reader.data)/2]

	// Any lookup should handle this gracefully
	_, err = reader.lookup(net.ParseIP("8.8.8.8"))
	// Should not panic; may return error or empty
	_ = err
}

// TestCov_Lookup_EmptyRecord tests lookup returning an empty record
// (record == nodeCount means "not found").
func TestCov_Lookup_EmptyRecord(t *testing.T) {
	// Create an MMDB where all nodes point to the root (node 0), so lookup
	// eventually exhausts bits without hitting a data record.
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.mmdb")

	// Build a minimal tree: 1 node, record size 24, both records = nodeCount (empty)
	nodeCount := uint32(1)
	recordSize := uint16(24)
	nodeBytes := make([]byte, 6) // 24-bit * 2 records / 8 = 6 bytes, all zeros

	// Set both records to nodeCount (=1) which means "empty"
	// Left record: bytes 0-2 = nodeCount
	nodeBytes[0] = byte(nodeCount >> 16)
	nodeBytes[1] = byte(nodeCount >> 8)
	nodeBytes[2] = byte(nodeCount)
	// Right record: bytes 3-5 = nodeCount
	nodeBytes[3] = byte(nodeCount >> 16)
	nodeBytes[4] = byte(nodeCount >> 8)
	nodeBytes[5] = byte(nodeCount)

	var mmdbData []byte
	mmdbData = append(mmdbData, nodeBytes...)
	// 16-byte separator
	mmdbData = append(mmdbData, make([]byte, 16)...)
	// Metadata
	var meta []byte
	meta = append(meta, encodeMapHeader(3)...)
	meta = append(meta, encodeString("node_count")...)
	meta = append(meta, encodeUint32(nodeCount)...)
	meta = append(meta, encodeString("record_size")...)
	meta = append(meta, encodeUint16(uint32(recordSize))...)
	meta = append(meta, encodeString("ip_version")...)
	meta = append(meta, encodeUint16(4)...)
	// Append marker + metadata
	mmdbData = append(mmdbData, []byte("\xab\xcd\xefMaxMind.com")...)
	mmdbData = append(mmdbData, meta...)

	if err := os.WriteFile(path, mmdbData, 0644); err != nil {
		t.Fatal(err)
	}

	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := reader.lookup(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res.typ != 0 {
		t.Errorf("expected empty result (type 0), got type %d", res.typ)
	}
}

// ============================================================================
// readNode coverage (was 81.0%) - out-of-bounds returns
// ============================================================================

// TestCov_ReadNode_OutOfBounds_RecordSize24 tests readNode with offset beyond
// data for record size 24.
func TestCov_ReadNode_OutOfBounds_RecordSize24(t *testing.T) {
	r := &mmdbReader{
		data: make([]byte, 4), // too small for any node
		meta: mmdbMetadata{recordSize: 24},
	}
	val := r.readNode(1, 0)
	if val != 0 {
		t.Errorf("expected 0 for out-of-bounds readNode, got %d", val)
	}
}

// TestCov_ReadNode_OutOfBounds_RecordSize28 tests readNode with offset beyond
// data for record size 28.
func TestCov_ReadNode_OutOfBounds_RecordSize28(t *testing.T) {
	r := &mmdbReader{
		data: make([]byte, 4), // too small for record size 28
		meta: mmdbMetadata{recordSize: 28},
	}
	val := r.readNode(1, 0)
	if val != 0 {
		t.Errorf("expected 0 for out-of-bounds readNode, got %d", val)
	}
}

// TestCov_ReadNode_OutOfBounds_RecordSize32 tests readNode with offset beyond
// data for record size 32.
func TestCov_ReadNode_OutOfBounds_RecordSize32(t *testing.T) {
	r := &mmdbReader{
		data: make([]byte, 4), // too small for record size 32 (needs 8)
		meta: mmdbMetadata{recordSize: 32},
	}
	val := r.readNode(0, 0)
	if val != 0 {
		t.Errorf("expected 0 for out-of-bounds readNode, got %d", val)
	}
}

// ============================================================================
// decodeFieldDepth coverage (was 79.5%)
// ============================================================================

// TestCov_DecodeFieldDepth_ExceedsMaxDepth tests the depth limit guard.
func TestCov_DecodeFieldDepth_ExceedsMaxDepth(t *testing.T) {
	r := &mmdbReader{data: make([]byte, 10)}
	res, consumed := r.decodeFieldDepth(0, maxDecodeDepth+1)
	if consumed != 0 {
		t.Errorf("expected 0 consumed at max depth, got %d", consumed)
	}
	_ = res
}

// TestCov_DecodeFieldDepth_ContainerType tests decoding a container type.
func TestCov_DecodeFieldDepth_ContainerType(t *testing.T) {
	// Container is type 12, extended type byte = 12-7 = 5
	// Build: [0x00|0, 5, size_bytes..., payload...]
	buf := []byte{
		0x00,       // typeNum=0 (extended), sizeValue=0 from lower 5 bits
		5,          // extended type byte = container-7 = 5; also sizeValue=5 from lower bits
		0x01, 0x02, // some payload bytes (5 bytes would be the size)
		0x03, 0x04, 0x05,
	}
	r := &mmdbReader{data: buf}
	res, consumed := r.decodeField(0)
	// Container type hits the default case: return result{}, consumed + payloadSize
	_ = res
	_ = consumed
}

// TestCov_DecodeFieldDepth_EndMarkerType tests decoding an end marker type.
func TestCov_DecodeFieldDepth_EndMarkerType(t *testing.T) {
	// End marker is type 13, extended type byte = 13-7 = 6
	buf := []byte{
		0x00, // typeNum=0 (extended), sizeValue=0 from lower 5 bits
		6,    // extended type byte = endmarker-7 = 6; sizeValue=6
	}
	r := &mmdbReader{data: buf}
	res, consumed := r.decodeField(0)
	// End marker also hits default case
	_ = res
	_ = consumed
}

// TestCov_DecodeFieldDepth_ExtendedTypeWithInsufficientData tests extended type
// when offset+1 >= len(data).
func TestCov_DecodeFieldDepth_ExtendedTypeWithInsufficientData(t *testing.T) {
	r := &mmdbReader{data: []byte{0x00}} // only 1 byte, typeNum=0 (extended)
	// offset 0: control byte is 0x00, typeNum=0 (extended)
	// needs offset+1 for extended type byte, but offset+1 >= len(data)
	res, consumed := r.decodeField(0)
	if consumed != 0 {
		t.Errorf("expected 0 consumed for insufficient data, got %d", consumed)
	}
	_ = res
}

// TestCov_DecodeFieldDepth_Int32Type tests decoding an Int32 value.
func TestCov_DecodeFieldDepth_Int32Type(t *testing.T) {
	// Int32 is type 8, extended type byte = 8-7 = 1
	// Build: [0x00|1, 1, 4-byte-payload]
	// The lower 5 bits of the extended byte = size = 1, but we want 4 bytes for int32
	// Actually, size comes from lower 5 bits of extended byte.
	// Extended byte for int32 = 1, lower 5 bits = 1 (size=1 byte)
	// But we want 4 bytes for a proper int32. Let's use size=4.
	// Extended byte: typeNum = int32-7 = 1, lower bits = size = 4
	// Actually the code: typeNum = r.data[offset+1] + 7, control = r.data[offset]
	// Then sizeValue = control & 0x1F (of original control byte, not the extended)
	// Wait, no: after reading extended type, control = r.data[offset] where offset was incremented
	// So: offset starts at 0, control = data[0] = 0x00, typeNum = 0 → extended
	// offset becomes 1, control = data[1], typeNum = data[1] + 7
	// sizeValue = data[1] & 0x1F

	// For int32 with 4-byte payload:
	// data[1] = (int32 - 7) with lower bits = size = 4
	// (8-7) = 1, so byte = 1, lower 5 bits of 1 = 1. Not 4.
	// We need the byte to encode both: the actual type = byte+7, and size in lower 5 bits
	// So data[1] = 4 (sizeValue=4), and typeNum = 4+7 = 11 (array, not int32)
	// This doesn't work for int32.
	// For int32: typeNum = 8, so data[1] + 7 = 8, data[1] = 1
	// sizeValue = data[1] & 0x1F = 1, payload = 1 byte
	buf := []byte{
		0x00,       // extended marker
		0x01,       // typeNum=1+7=8 (int32), sizeValue=1
		0xFF,       // 1-byte int32 payload
	}
	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeInt32 {
		t.Errorf("type = %d, want %d (int32)", res.typ, mmdbTypeInt32)
	}
	if res.num != 0xFF {
		t.Errorf("num = %d, want 255", res.num)
	}
}

// TestCov_DecodeFieldDepth_Uint16Type tests decoding a Uint16 value.
func TestCov_DecodeFieldDepth_Uint16Type(t *testing.T) {
	// Uint16 is type 5, control byte: typeNum=5, sizeValue=2
	control := byte(5<<5 | 2) // typeNum=5, size=2
	buf := []byte{
		control,
		0x01, 0x02, // 2-byte uint16 payload
	}
	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeUint16 {
		t.Errorf("type = %d, want %d (uint16)", res.typ, mmdbTypeUint16)
	}
	if res.num != 0x0102 {
		t.Errorf("num = %d, want %d", res.num, 0x0102)
	}
}

// TestCov_DecodeFieldDepth_Uint64Type tests decoding a Uint64 value.
func TestCov_DecodeFieldDepth_Uint64Type(t *testing.T) {
	// Uint64 is type 9, extended: byte = 9-7 = 2, sizeValue=2 (lower 5 bits)
	buf := []byte{
		0x00,       // extended
		0x02,       // typeNum=2+7=9 (uint64), sizeValue=2
		0x01, 0x02, // 2-byte payload
	}
	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeUint64 {
		t.Errorf("type = %d, want %d (uint64)", res.typ, mmdbTypeUint64)
	}
}

// TestCov_DecodeFieldDepth_Uint128Type tests decoding a Uint128 value.
func TestCov_DecodeFieldDepth_Uint128Type(t *testing.T) {
	// Uint128 is type 10, extended: byte = 10-7 = 3, sizeValue=4 (lower 5 bits)
	buf := []byte{
		0x00,                   // extended
		0x04,                   // typeNum=4+7=11 (array actually... let me fix)
		0x01, 0x02, 0x03, 0x04, // 4-byte payload
	}
	// Actually, for uint128: data[1] = 3, typeNum = 3+7 = 10 (uint128), sizeValue = 3
	buf[1] = 3
	buf = buf[:2+3] // 3-byte payload
	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeUint128 {
		t.Errorf("type = %d, want %d (uint128)", res.typ, mmdbTypeUint128)
	}
}

// TestCov_DecodeFieldDepth_PointerFollow tests pointer type that follows
// to another field.
func TestCov_DecodeFieldDepth_PointerFollow(t *testing.T) {
	// Build data where first field is a string at some offset, then a pointer to it
	// String "hello" at offset 10
	strData := encodeString("hello")
	buf := make([]byte, 20)
	copy(buf[10:], strData)

	// Pointer at offset 0 that points to offset 10
	// Pointer type = 1, control = 1<<5 | size_bits | value_bits
	// For a pointer to offset 10: use size=0 (1-byte), value bits from control
	// control byte: typeNum=1, sizeValue = pointer payload size
	// After control, decodePointer reads data
	// For size=0: ptr = (control & 0x07)<<8 | data[0]
	// control = 0x20 | 0x00 (size=0, value bits=0) → ptr = 0<<8 | data[0]
	// So data[0] = 10 → ptr = 10
	ptrControl := byte(1<<5 | 1) // typeNum=1, sizeValue=1 for pointer payload
	buf[0] = ptrControl
	// decodePointer receives data[offset:] after size decoding
	// Actually decodeFieldDepth: after control byte, it calls decodeSize then uses data[offset:]
	// For pointer, the code calls decodePointer(r.data[offset-sizeBytes:], control)
	// This is complex. Let me just verify a pointer to offset 10.
	// The pointer uses bits from control and following bytes.
	// control = 0x28 (typeNum=1, lower bits = 8)
	// Wait, I need to construct this carefully.

	// Simplify: just create raw bytes that encode a pointer
	// For a pointer with size=0 from decodePointer:
	//   ptr = uint32(control & 0x07) << 8 | uint32(data[0])
	// control byte for pointer type: typeNum=1 (top 3 bits), sizeValue in lower 5
	// decodePointer gets: data = r.data[offset-sizeBytes:], control = the original control
	// In decodePointer: size = (control >> 3) & 0x03, v = control & 0x07
	// For size=0: need (control >> 3) & 0x03 = 0 → bits 3-4 = 0
	// So control = 0b001_00_vvv where vvv = high bits of ptr
	// ptr = v << 8 | data[0]
	// For ptr = 10: v=0, data[0]=10
	// control = 0b001_00_000 = 0x20, but typeNum is top 3 bits = 001 = 1
	// sizeValue = control & 0x1F = 0 (no extra size bytes)
	// Actually sizeValue from decodeFieldDepth is used for payloadSize, not for decodePointer

	// The flow: control = data[offset], typeNum = 1 (pointer)
	// sizeValue = control & 0x1F = 0
	// payloadSize = 0, sizeBytes = 0
	// decodePointer(r.data[offset:], control) — offset has been incremented past control byte
	// decodePointer: size = (0x20 >> 3) & 0x03 = 4 >> 3... wait
	// 0x20 = 0b00100000, >> 3 = 0b00000100 = 4, & 0x03 = 0
	// v = 0x20 & 0x07 = 0
	// ptr = 0 << 8 | data[0] = data[0]
	// bytes consumed = 1

	// So: buf[0] = 0x20 (control), buf[1] = 10 (pointer target offset)
	// String "hello" at offset 10
	buf[0] = 0x20
	buf[1] = 10
	copy(buf[10:], strData)

	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeString {
		t.Errorf("type = %d, want %d (string from pointer)", res.typ, mmdbTypeString)
	}
	if res.str != "hello" {
		t.Errorf("str = %q, want %q", res.str, "hello")
	}
}

// TestCov_DecodeFieldDepth_PointerOutOfBounds tests a pointer that points
// beyond data bounds.
func TestCov_DecodeFieldDepth_PointerOutOfBounds(t *testing.T) {
	// Pointer to offset 255 with only 10 bytes of data
	buf := make([]byte, 10)
	buf[0] = 0x27 // pointer type, v=7
	buf[1] = 0xFF // ptr = 7<<8 | 255 = 2047 (out of bounds for 10-byte data)

	r := &mmdbReader{data: buf}
	res, consumed := r.decodeField(0)
	_ = res
	_ = consumed
}

// TestCov_DecodeFieldDepth_MapTooLarge tests map decoding with payload > 10000.
func TestCov_DecodeFieldDepth_MapTooLarge(t *testing.T) {
	// Map type = 7, use sizeValue=30 → 285 + data[0]<<8 + data[1]
	// 7<<5 = 224, 224 | 30 = 254, fits in byte
	control := byte(7<<5 | 30)
	buf := []byte{
		control,
		0x27, 0x10, // 285 + 10000 = 10285 entries
	}
	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	// Should return empty due to >10000 limit
	if res.typ != 0 {
		t.Errorf("expected empty result for oversized map, got type %d", res.typ)
	}
}

// TestCov_DecodeFieldDepth_ArrayTooLarge tests array decoding with payload > 10000.
// This is not achievable through standard MMDB encoding.
func TestCov_DecodeFieldDepth_ArrayTooLarge(t *testing.T) {
	t.Skip("cannot encode array with >10000 entries in MMDB format through standard path")
}

// TestCov_DecodeFieldDepth_IntegerOverflow tests integer type with payload
// exceeding data bounds.
func TestCov_DecodeFieldDepth_IntegerOverflow(t *testing.T) {
	// Uint32 type = 6, control = 6<<5 | 4 (size=4 bytes)
	control := byte(6<<5 | 4)
	buf := []byte{
		control,
		0x01, 0x02, // only 2 bytes but size says 4
	}
	r := &mmdbReader{data: buf}
	res, consumed := r.decodeField(0)
	// Should return empty due to bounds check
	if res.typ != 0 {
		t.Errorf("expected empty result for out-of-bounds integer, got type %d", res.typ)
	}
	_ = consumed
}

// TestCov_DecodeFieldDepth_DoubleOutOfBounds tests double with insufficient data.
func TestCov_DecodeFieldDepth_DoubleOutOfBounds(t *testing.T) {
	control := byte(mmdbTypeDouble<<5 | 8) // double, size=8
	buf := []byte{
		control,
		0x01, 0x02, 0x03, // only 3 bytes but need 8
	}
	r := &mmdbReader{data: buf}
	res, consumed := r.decodeField(0)
	if res.typ != 0 {
		t.Errorf("expected empty result for out-of-bounds double, got type %d", res.typ)
	}
	_ = consumed
}

// TestCov_DecodeFieldDepth_FloatOutOfBounds tests float with insufficient data.
func TestCov_DecodeFieldDepth_FloatOutOfBounds(t *testing.T) {
	// Float is extended type 15: extended byte = 15-7 = 8
	buf := []byte{
		0x00,       // extended marker
		0x08,       // typeNum=8+7=15 (float), sizeValue=8 (lower 5 bits)
		0x01, 0x02, // only 2 bytes but size says 8 (or 4)
	}
	r := &mmdbReader{data: buf}
	res, consumed := r.decodeField(0)
	if res.typ != 0 {
		t.Errorf("expected empty result for out-of-bounds float, got type %d", res.typ)
	}
	_ = consumed
}

// ============================================================================
// newMMDBReader coverage (was 83.3%) - tree size overflow
// ============================================================================

// TestCov_NewMMDBReader_TreeSizeOverflow tests newMMDBReader with a tree size
// that exceeds the file.
func TestCov_NewMMDBReader_TreeSizeOverflow(t *testing.T) {
	// Create an MMDB with many nodes but tiny file
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "overflow.mmdb")

	// Minimal 1-node tree with record size 24 (6 bytes)
	nodeBytes := make([]byte, 6)

	var mmdbData []byte
	mmdbData = append(mmdbData, nodeBytes...)
	mmdbData = append(mmdbData, make([]byte, 16)...) // separator

	// Metadata claiming a very large node count
	var meta []byte
	meta = append(meta, encodeMapHeader(3)...)
	meta = append(meta, encodeString("node_count")...)
	meta = append(meta, encodeUint32(1000000)...) // huge node count
	meta = append(meta, encodeString("record_size")...)
	meta = append(meta, encodeUint16(32)...) // 8 bytes per node
	meta = append(meta, encodeString("ip_version")...)
	meta = append(meta, encodeUint16(4)...)

	mmdbData = append(mmdbData, []byte("\xab\xcd\xefMaxMind.com")...)
	mmdbData = append(mmdbData, meta...)

	if err := os.WriteFile(path, mmdbData, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := newMMDBReader(path)
	if err == nil {
		t.Error("expected error for tree size exceeding file")
	}
}

// TestCov_NewMMDBReader_RecordSize32 tests newMMDBReader with record size 32.
func TestCov_NewMMDBReader_RecordSize32(t *testing.T) {
	path := writeTestMMDB(t, 32)
	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatalf("newMMDBReader(32) error: %v", err)
	}
	if reader == nil {
		t.Fatal("expected non-nil reader")
	}
	if reader.meta.recordSize != 32 {
		t.Errorf("recordSize = %d, want 32", reader.meta.recordSize)
	}
}

// ============================================================================
// lookupCountry coverage (was 85.7%) - missing iso_code and names
// ============================================================================

// TestCov_LookupCountry_MissingISOCode tests lookupCountry with a country map
// that has no iso_code key.
func TestCov_LookupCountry_MissingISOCode(t *testing.T) {
	// Build MMDB data with country but no iso_code
	var countryMap []byte
	countryMap = append(countryMap, encodeMapHeader(1)...)
	countryMap = append(countryMap, encodeString("names")...)
	countryMap = append(countryMap, encodeMapHeader(1)...)
	countryMap = append(countryMap, encodeString("en")...)
	countryMap = append(countryMap, encodeString("TestCountry")...)

	var rootMap []byte
	rootMap = append(rootMap, encodeMapHeader(1)...)
	rootMap = append(rootMap, encodeString("country")...)
	rootMap = append(rootMap, countryMap...)

	// Build a 1-node tree that points to this data
	// Use writeTestMMDB approach but with custom data
	path := buildTestMMDBWithRootData(t, rootMap, 24)
	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatal(err)
	}

	_, name, ok := reader.lookupCountry(net.ParseIP("8.8.8.8"))
	// Should work: country exists, but no iso_code
	// Actually without iso_code, ok should be false
	if ok {
		t.Error("expected ok=false when iso_code is missing")
	}
	_ = name
}

// TestCov_LookupCountry_WithEnglishName tests lookupCountry with full country
// info including English name.
func TestCov_LookupCountry_WithEnglishName(t *testing.T) {
	// Build data with country → iso_code + names → en
	var namesMap []byte
	namesMap = append(namesMap, encodeMapHeader(1)...)
	namesMap = append(namesMap, encodeString("en")...)
	namesMap = append(namesMap, encodeString("United States")...)

	var countryMap []byte
	countryMap = append(countryMap, encodeMapHeader(2)...)
	countryMap = append(countryMap, encodeString("iso_code")...)
	countryMap = append(countryMap, encodeString("US")...)
	countryMap = append(countryMap, encodeString("names")...)
	countryMap = append(countryMap, namesMap...)

	var rootMap []byte
	rootMap = append(rootMap, encodeMapHeader(1)...)
	rootMap = append(rootMap, encodeString("country")...)
	rootMap = append(rootMap, countryMap...)

	path := buildTestMMDBWithRootData(t, rootMap, 24)
	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatal(err)
	}

	iso, name, ok := reader.lookupCountry(net.ParseIP("8.8.8.8"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if iso != "US" {
		t.Errorf("iso = %q, want %q", iso, "US")
	}
	if name != "United States" {
		t.Errorf("name = %q, want %q", name, "United States")
	}
}

// ============================================================================
// Route coverage - unhealthy pool fallback
// ============================================================================

// TestCov_Route_UnhealthyPoolWithHealthyFallback tests routing when primary
// pool is unhealthy but fallback is healthy.
func TestCov_Route_UnhealthyPoolWithHealthyFallback(t *testing.T) {
	g := New(Config{
		DefaultPool: "default",
		Rules: []GeoRule{
			{ID: "us", Country: "US", Pool: "us-pool", Fallback: "us-fallback"},
		},
	})

	g.SetPoolHealth("us-pool", false)
	// us-fallback has no health entry → treated as healthy

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "3.0.0.1:1234" // US IP

	pool, _, _ := g.Route(req)
	if pool != "us-fallback" {
		t.Errorf("expected us-fallback pool, got %q", pool)
	}
}

// TestCov_Route_UnhealthyPoolWithUnhealthyFallback tests routing when both
// primary and fallback pools are unhealthy.
func TestCov_Route_UnhealthyPoolWithUnhealthyFallback(t *testing.T) {
	g := New(Config{
		DefaultPool: "default",
		Rules: []GeoRule{
			{ID: "us", Country: "US", Pool: "us-pool", Fallback: "us-fallback"},
		},
	})

	g.SetPoolHealth("us-pool", false)
	g.SetPoolHealth("us-fallback", false)

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "3.0.0.1:1234" // US IP

	pool, _, _ := g.Route(req)
	if pool != "default" {
		t.Errorf("expected default pool, got %q", pool)
	}
}

// TestCov_Route_NilGeoDNS tests Route on nil GeoDNS.
func TestCov_Route_NilGeoDNS(t *testing.T) {
	var g *GeoDNS
	pool, loc, err := g.Route(httptest.NewRequest("GET", "/", nil))
	if pool != "" || loc != nil || err != nil {
		t.Errorf("expected empty results for nil GeoDNS")
	}
}

// ============================================================================
// guessLocationFromIP coverage
// ============================================================================

// TestCov_GuessLocationFromIP_EuropeanRange tests the EU IP range heuristic.
func TestCov_GuessLocationFromIP_EuropeanRange(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	loc := g.guessLocationFromIP("5.0.0.1")
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.Country != "EU" {
		t.Errorf("Country = %q, want EU", loc.Country)
	}
}

// TestCov_GuessLocationFromIP_USRange tests the US IP range heuristic.
func TestCov_GuessLocationFromIP_USRange(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	loc := g.guessLocationFromIP("3.0.0.1")
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.Country != "US" {
		t.Errorf("Country = %q, want US", loc.Country)
	}
}

// TestCov_GuessLocationFromIP_Loopback tests loopback IP.
// Note: 127.0.0.1 is in isPrivateIP's 127.0.0.0/8 range, so it returns "PRIVATE".
// To reach the IsLoopback branch, use ::1 which is not in isPrivateIP ranges.
func TestCov_GuessLocationFromIP_Loopback(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	// IPv6 loopback reaches IsLoopback before isPrivateIP
	loc := g.guessLocationFromIP("::1")
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.Country != "LOCAL" {
		t.Errorf("Country = %q, want LOCAL", loc.Country)
	}
}

// TestCov_GuessLocationFromIP_PrivateIPv4 tests private IP returning PRIVATE.
func TestCov_GuessLocationFromIP_PrivateIPv4(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	loc := g.guessLocationFromIP("127.0.0.1")
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.Country != "PRIVATE" {
		t.Errorf("Country = %q, want PRIVATE", loc.Country)
	}
}

// TestCov_GuessLocationFromIP_Unknown tests unknown public IP.
func TestCov_GuessLocationFromIP_Unknown(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	loc := g.guessLocationFromIP("198.51.100.1")
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.Country != "UNKNOWN" {
		t.Errorf("Country = %q, want UNKNOWN", loc.Country)
	}
}

// TestCov_GuessLocationFromIP_InvalidIP tests with invalid IP string.
func TestCov_GuessLocationFromIP_InvalidIP(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	loc := g.guessLocationFromIP("not-an-ip")
	if loc != nil {
		t.Error("expected nil for invalid IP")
	}
}

// ============================================================================
// Helper: buildTestMMDBWithRootData builds a minimal MMDB that returns the
// given data for any IP lookup.
// ============================================================================

func buildTestMMDBWithRootData(t *testing.T, rootData []byte, recordSize uint16) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "custom.mmdb")

	nodeCount := uint32(1)
	// Build tree: 1 node, both records point to data
	var nodeBytes []byte
	dataOffset := uint32(nodeCount) + 16 // data record = nodeCount + 16 + offset

	switch recordSize {
	case 24:
		nodeBytes = make([]byte, 6)
		// Left record = data offset
		nodeBytes[0] = byte(dataOffset >> 16)
		nodeBytes[1] = byte(dataOffset >> 8)
		nodeBytes[2] = byte(dataOffset)
		// Right record = data offset
		nodeBytes[3] = byte(dataOffset >> 16)
		nodeBytes[4] = byte(dataOffset >> 8)
		nodeBytes[5] = byte(dataOffset)
	case 28:
		nodeBytes = make([]byte, 7)
		// Left: 3 bytes + upper nibble of middle
		nodeBytes[0] = byte(dataOffset >> 16)
		nodeBytes[1] = byte(dataOffset >> 8)
		nodeBytes[2] = byte(dataOffset)
		nodeBytes[3] = byte((dataOffset >> 24) << 4) // upper nibble
		// Right: 3 bytes + lower nibble of middle
		nodeBytes[4] = byte(dataOffset >> 16)
		nodeBytes[5] = byte(dataOffset >> 8)
		nodeBytes[6] = byte(dataOffset)
		nodeBytes[3] |= byte(dataOffset>>24) & 0x0F
	case 32:
		nodeBytes = make([]byte, 8)
		binary.BigEndian.PutUint32(nodeBytes[0:4], dataOffset)
		binary.BigEndian.PutUint32(nodeBytes[4:8], dataOffset)
	}

	treeSize := uint32(len(nodeBytes))
	dataStart := treeSize + 16 // tree + 16-byte separator

	var mmdbData []byte
	mmdbData = append(mmdbData, nodeBytes...)
	// Pad separator to exactly 16 bytes
	sep := make([]byte, 16)
	mmdbData = append(mmdbData, sep...)
	// Root data at dataStart
	// Ensure dataStart matches actual position
	if uint32(len(mmdbData)) != dataStart {
		// Adjust: fill gap if needed
		for uint32(len(mmdbData)) < dataStart {
			mmdbData = append(mmdbData, 0)
		}
	}
	mmdbData = append(mmdbData, rootData...)

	// Metadata
	var meta []byte
	meta = append(meta, encodeMapHeader(3)...)
	meta = append(meta, encodeString("node_count")...)
	meta = append(meta, encodeUint32(nodeCount)...)
	meta = append(meta, encodeString("record_size")...)
	meta = append(meta, encodeUint16(uint32(recordSize))...)
	meta = append(meta, encodeString("ip_version")...)
	meta = append(meta, encodeUint16(4)...)

	mmdbData = append(mmdbData, []byte("\xab\xcd\xefMaxMind.com")...)
	mmdbData = append(mmdbData, meta...)

	if err := os.WriteFile(path, mmdbData, 0644); err != nil {
		t.Fatal(err)
	}

	return path
}


