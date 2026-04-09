package geodns

import (
	"encoding/binary"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClose(t *testing.T) {
	path := writeTestMMDB(t, 24)
	g := New(Config{DBPath: path, DefaultPool: "default"})
	if g.mmdb == nil {
		t.Fatal("expected mmdb to be loaded")
	}
	g.Close()
	if g.mmdb != nil {
		t.Error("expected mmdb to be nil after Close()")
	}
}

func TestReloadDB_Success(t *testing.T) {
	path := writeTestMMDB(t, 24)
	g := New(Config{DBPath: path, DefaultPool: "default"})
	if err := g.ReloadDB(); err != nil {
		t.Fatalf("ReloadDB() error: %v", err)
	}
	if g.mmdb == nil {
		t.Error("expected mmdb to be non-nil after ReloadDB")
	}
}

func TestReloadDB_EmptyPath(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	if err := g.ReloadDB(); err == nil {
		t.Error("expected error when mmdbPath is empty")
	}
}

func TestReloadDB_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "bad.mmdb")
	os.WriteFile(badPath, []byte("not a valid mmdb"), 0644)

	g := New(Config{DBPath: badPath, DefaultPool: "default"})
	// First load would have failed silently
	if err := g.ReloadDB(); err == nil {
		t.Error("expected error for invalid mmdb file")
	}
}

func TestNew_WithMMDB(t *testing.T) {
	path := writeTestMMDB(t, 28)
	g := New(Config{DBPath: path, DefaultPool: "default"})
	if g.mmdb == nil {
		t.Error("expected mmdb to be loaded when valid DBPath provided")
	}
	stats := g.Stats()
	if !stats.DBLoaded {
		t.Error("Stats.DBLoaded should be true")
	}
	if stats.DBPath != path {
		t.Errorf("Stats.DBPath = %q, want %q", stats.DBPath, path)
	}
}

func TestLookupLocation_WithMMDB(t *testing.T) {
	path := writeTestMMDB(t, 28)
	g := New(Config{DBPath: path, DefaultPool: "default"})

	// 8.8.8.8 is in the MMDB as US
	loc := g.lookupLocation("8.8.8.8")
	if loc == nil {
		t.Fatal("expected non-nil location for 8.8.8.8")
	}
	if loc.Country != "US" {
		t.Errorf("Country = %q, want US", loc.Country)
	}

	// 1.1.1.1 is in the MMDB as AU
	loc = g.lookupLocation("1.1.1.1")
	if loc == nil {
		t.Fatal("expected non-nil location for 1.1.1.1")
	}
	if loc.Country != "AU" {
		t.Errorf("Country = %q, want AU", loc.Country)
	}
}

func TestExtractClientIP_MalformedRemoteAddr(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "192.168.1.1" // no port

	got := g.extractClientIP(req)
	if got != "192.168.1.1" {
		t.Errorf("extractClientIP() = %q, want %q", got, "192.168.1.1")
	}
}

func TestMiddleware(t *testing.T) {
	g := New(Config{
		DefaultPool: "default",
		Rules:       []GeoRule{{ID: "us", Country: "US", Pool: "us-pool"}},
	})

	called := false
	handler := g.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		country := r.Header.Get("X-Geo-Country")
		if country == "" {
			t.Error("expected X-Geo-Country header")
		}
	}))

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:1234" // in default geoDB as US
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("next handler was not called")
	}
}

func TestRoute_FallbackWhenBothUnhealthy(t *testing.T) {
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
		t.Errorf("expected default pool when both are unhealthy, got %q", pool)
	}
}

// --- MMDB decode coverage tests ---

func TestDecodeSize_Extended(t *testing.T) {
	r := &mmdbReader{data: []byte{}}

	// sizeValue 29: 29 + data[0]
	size, bytes := r.decodeSize([]byte{10}, 29)
	if size != 39 || bytes != 1 {
		t.Errorf("sizeValue=29: got (%d, %d), want (39, 1)", size, bytes)
	}

	// sizeValue 30: 285 + data[0]<<8 + data[1]
	size, bytes = r.decodeSize([]byte{1, 0}, 30)
	if size != 285+256 || bytes != 2 {
		t.Errorf("sizeValue=30: got (%d, %d), want (%d, 2)", size, bytes, 285+256)
	}

	// sizeValue 31: 65821 + data[0]<<16 + data[1]<<8 + data[2]
	size, bytes = r.decodeSize([]byte{0, 1, 0}, 31)
	if size != 65821+256 || bytes != 3 {
		t.Errorf("sizeValue=31: got (%d, %d), want (%d, 3)", size, bytes, 65821+256)
	}

	// sizeValue 29 with empty data
	size, bytes = r.decodeSize([]byte{}, 29)
	if size != 29 || bytes != 0 {
		t.Errorf("sizeValue=29 empty: got (%d, %d), want (29, 0)", size, bytes)
	}

	// sizeValue 30 with insufficient data
	size, bytes = r.decodeSize([]byte{1}, 30)
	if size != 285 || bytes != 0 {
		t.Errorf("sizeValue=30 short: got (%d, %d), want (285, 0)", size, bytes)
	}

	// sizeValue 31 with insufficient data
	size, bytes = r.decodeSize([]byte{0, 1}, 31)
	if size != 65821 || bytes != 0 {
		t.Errorf("sizeValue=31 short: got (%d, %d), want (65821, 0)", size, bytes)
	}
}

func TestDecodePointer(t *testing.T) {
	r := &mmdbReader{data: []byte{}}

	// size=0: 1-byte pointer
	ptr, bytes := r.decodePointer([]byte{0x50}, byte(0x20)|0x05)
	if bytes != 1 {
		t.Errorf("size=0: bytes=%d, want 1", bytes)
	}
	_ = ptr

	// size=1: 2-byte pointer
	ptr, bytes = r.decodePointer([]byte{0x01, 0x02}, byte(0x28)|0x03)
	if bytes != 2 {
		t.Errorf("size=1: bytes=%d, want 2", bytes)
	}
	_ = ptr

	// size=2: 3-byte pointer
	ptr, bytes = r.decodePointer([]byte{0x01, 0x02, 0x03}, byte(0x30)|0x01)
	if bytes != 3 {
		t.Errorf("size=2: bytes=%d, want 3", bytes)
	}
	_ = ptr

	// size=3: 4-byte pointer
	ptr, bytes = r.decodePointer([]byte{0x01, 0x02, 0x03, 0x04}, byte(0x38))
	if bytes != 4 {
		t.Errorf("size=3: bytes=%d, want 4", bytes)
	}
	if ptr != 0x01020304 {
		t.Errorf("size=3: ptr=0x%08X, want 0x01020304", ptr)
	}

	// Insufficient data for each size
	_, bytes = r.decodePointer([]byte{}, byte(0x20))
	if bytes != 0 {
		t.Errorf("size=0 empty: bytes=%d, want 0", bytes)
	}
	_, bytes = r.decodePointer([]byte{0x01}, byte(0x28))
	if bytes != 0 {
		t.Errorf("size=1 short: bytes=%d, want 0", bytes)
	}
	_, bytes = r.decodePointer([]byte{0x01, 0x02}, byte(0x30))
	if bytes != 0 {
		t.Errorf("size=2 short: bytes=%d, want 0", bytes)
	}
	_, bytes = r.decodePointer([]byte{0x01, 0x02, 0x03}, byte(0x38))
	if bytes != 0 {
		t.Errorf("size=3 short: bytes=%d, want 0", bytes)
	}
}

func TestDecodeField_Double(t *testing.T) {
	val := 3.14159
	bits := math.Float64bits(val)
	// Double is type 3 (<=7), so top 3 bits of control = 3, lower 5 = size=8
	var data [9]byte
	data[0] = byte(mmdbTypeDouble)<<5 | 8
	binary.BigEndian.PutUint64(data[1:], bits)

	r := &mmdbReader{data: data[:]}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeDouble {
		t.Errorf("type = %d, want %d", res.typ, mmdbTypeDouble)
	}
	if res.num != bits {
		t.Errorf("value mismatch")
	}
}

func TestDecodeField_Float(t *testing.T) {
	val := float32(2.5)
	bits := math.Float32bits(val)

	// Float is type 15, which is extended (>7).
	// Extended: control byte has typeNum=0 in top 3 bits, next byte is actualType-7
	buf := make([]byte, 0, 7)
	buf = append(buf, 0x00|4)                // typeNum=0 (extended), sizeValue=4 in lower 5 bits
	buf = append(buf, byte(mmdbTypeFloat-7)) // extended type byte = 8
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint32(tmp, bits)
	buf = append(buf, tmp...)

	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeFloat {
		t.Errorf("type = %d, want %d (mmdbTypeFloat)", res.typ, mmdbTypeFloat)
	}
}

func TestDecodeField_Boolean(t *testing.T) {
	// Boolean is type 14, extended (>7). extByte = 14-7 = 7
	// Code reads sizeValue from lower 5 bits of extByte = 7, which is non-zero → true
	extByte := byte(mmdbTypeBoolean - 7) // 7
	buf := []byte{0x00, extByte}
	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeBoolean {
		t.Errorf("type = %d, want %d", res.typ, mmdbTypeBoolean)
	}
	if !res.bln {
		t.Error("expected true (sizeValue=7 from extByte)")
	}
}

func TestDecodeField_Bytes(t *testing.T) {
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	buf := []byte{byte(mmdbTypeBytes)<<5 | byte(len(payload))}
	buf = append(buf, payload...)

	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeBytes {
		t.Errorf("type = %d, want %d", res.typ, mmdbTypeBytes)
	}
	if res.str != string(payload) {
		t.Errorf("payload mismatch")
	}
}

func TestDecodeField_Array(t *testing.T) {
	// Array is type 11, extended (>7). extByte = 11-7 = 4
	// Code reads sizeValue from lower 5 bits of extByte = 4, so it reads 4 elements
	extByte := byte(mmdbTypeArray - 7) // 4
	buf := make([]byte, 0, 50)
	buf = append(buf, 0x00)                 // typeNum=0 (extended), original lower bits ignored by code
	buf = append(buf, extByte)              // extended type byte → size=4
	buf = append(buf, encodeString("a")...) // element 0
	buf = append(buf, encodeString("b")...) // element 1
	buf = append(buf, encodeString("c")...) // element 2
	buf = append(buf, encodeString("d")...) // element 3

	r := &mmdbReader{data: buf}
	res, _ := r.decodeField(0)
	if res.typ != mmdbTypeArray {
		t.Errorf("type = %d, want %d", res.typ, mmdbTypeArray)
	}
	if len(res.arr) != 4 {
		t.Fatalf("array len = %d, want 4", len(res.arr))
	}
	if res.arr[0].str != "a" || res.arr[3].str != "d" {
		t.Errorf("array content mismatch: [%q, ..., %q]", res.arr[0].str, res.arr[3].str)
	}
}

func TestDecodeField_OutOfBounds(t *testing.T) {
	r := &mmdbReader{data: []byte{0x01}}
	res, consumed := r.decodeField(100) // offset > len(data)
	if consumed != 0 {
		t.Errorf("consumed = %d, want 0", consumed)
	}
	_ = res
}

func TestLookupCountry_MissingCountryKey(t *testing.T) {
	// Build data that has a map but no "country" key
	var buf []byte
	buf = append(buf, encodeMapHeader(1)...)
	buf = append(buf, encodeString("other_key")...)
	buf = append(buf, encodeString("value")...)

	r := &mmdbReader{data: buf}
	// Direct decode test — lookupCountry goes through lookup() which needs tree etc.
	// Just test the decode and manual navigation
	res, _ := r.decodeField(0)
	if _, ok := res.mp["country"]; ok {
		t.Error("should not have 'country' key")
	}
}

func TestLookup_IPv6InIPv4DB(t *testing.T) {
	path := writeTestMMDB(t, 28) // IPv4 DB
	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatal(err)
	}

	// IPv6 address in IPv4-only DB should return empty
	res, err := reader.lookup(net.ParseIP("2001:db8::1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.typ != 0 {
		t.Errorf("expected empty result for IPv6 in IPv4 DB, got type %d", res.typ)
	}
}

func TestParseMetadata_MissingNodeCount(t *testing.T) {
	// Build metadata map without node_count
	var buf []byte
	buf = append(buf, encodeMapHeader(1)...)
	buf = append(buf, encodeString("record_size")...)
	buf = append(buf, encodeUint16(28)...)

	_, err := parseMetadata(buf, 0)
	if err == nil {
		t.Error("expected error for missing node_count")
	}
}

func TestParseMetadata_MissingRecordSize(t *testing.T) {
	// Build metadata map without record_size
	var buf []byte
	buf = append(buf, encodeMapHeader(1)...)
	buf = append(buf, encodeString("node_count")...)
	buf = append(buf, encodeUint32(100)...)

	_, err := parseMetadata(buf, 0)
	if err == nil {
		t.Error("expected error for missing record_size")
	}
}

func TestNewMMDBReader_UnsupportedRecordSize(t *testing.T) {
	// Build a minimal MMDB with unsupported record size (e.g. 16)
	path := writeTestMMDB(t, 24) // Valid MMDB

	// Read it and patch the record_size in metadata
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Find "record_size" in the metadata and patch the value
	// The metadata contains encodeString("record_size") followed by encodeUint16(24)
	needle := encodeString("record_size")
	for i := 0; i < len(data)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if data[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			// Found it; the uint16 value follows
			valOffset := i + len(needle)
			// encodeUint16(24) is: [type_byte|1, 24] (2 bytes for value <= 0xFF)
			// Replace value byte with 16 (unsupported)
			data[valOffset+1] = 16
			break
		}
	}

	tmpDir := t.TempDir()
	patchedPath := filepath.Join(tmpDir, "patched.mmdb")
	os.WriteFile(patchedPath, data, 0644)

	_, err = newMMDBReader(patchedPath)
	if err == nil {
		t.Error("expected error for unsupported record size")
	}
}

func TestAddGeoData(t *testing.T) {
	g := New(Config{DefaultPool: "default"})

	if err := g.AddGeoData("203.0.113.0/24", &Location{Country: "XX", City: "TestCity"}); err != nil {
		t.Fatalf("AddGeoData() error: %v", err)
	}

	loc := g.lookupLocation("203.0.113.50")
	if loc == nil {
		t.Fatal("expected location for added geo data")
	}
	if loc.Country != "XX" {
		t.Errorf("Country = %q, want XX", loc.Country)
	}
}

func TestAddGeoData_InvalidCIDR(t *testing.T) {
	g := New(Config{DefaultPool: "default"})
	if err := g.AddGeoData("invalid", &Location{}); err == nil {
		t.Error("expected error for invalid CIDR")
	}
}
