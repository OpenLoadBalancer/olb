package geodns

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// writeTestMMDB creates a minimal MMDB file that maps:
//   - 8.0.0.0/8 -> US (United States)
//
// The tree is a simple 8-level trie for the first octet.
func writeTestMMDB(t *testing.T, recordSize uint16) string {
	t.Helper()

	nodeCount := uint32(256)

	// Build data section
	dataUS := buildCountryData("US", "United States")
	dataAU := buildCountryData("AU", "Australia")

	// Data record values: nodeCount + 16 + dataOffset
	// In MMDB: record value > nodeCount means data record
	// data offset = (record - nodeCount) - 16
	dataRecordUS := nodeCount + uint32(len(dataUS)) + 16 + uint32(len(dataAU))
	dataRecordAU := nodeCount + uint32(len(dataUS)) + 16

	// Actually, let me reconsider. The MMDB spec says:
	// If record > nodeCount: dataOffset = (record - nodeCount - 16)
	// Our reader code: dataOffset = record - nodeCount - 16
	// But wait, our reader uses dataBase = treeSize + 16
	// and dataOffset = record - nodeCount - 16 means:
	// actual byte offset in data section = dataOffset
	// Hmm, let me re-read our reader code...
	//
	// In our reader.lookup():
	//   dataOffset := record - r.meta.nodeCount - 16
	//   r.decodeField(dataOffset)
	// decodeField uses the raw byte offset in r.data.
	// But the data section starts at dataBase = treeSize + 16.
	// So we need: dataOffset = treeSize + 16 + (record - nodeCount - 16)
	// That doesn't make sense. Let me re-read...
	//
	// Actually looking at the code more carefully:
	// dataOffset := record - r.meta.nodeCount - 16
	// This is the offset from the START of the file to the data.
	// No wait, it's the offset passed directly to decodeField which
	// indexes into r.data (the full file).
	//
	// But the data section starts at treeSize + 16.
	// The record value in MMDB is: nodeCount + (offset_in_data_section) + 16
	// So: offset_in_data_section = record - nodeCount - 16
	// And we pass this to decodeField which indexes r.data.
	// That's WRONG because offset_in_data_section is relative to data section,
	// not the file start.
	//
	// Fix: dataOffset should be (treeSize + 16) + (record - nodeCount - 16)
	// = treeSize + record - nodeCount
	// Or simpler: dataBase + (record - nodeCount - 16)

	// Let me just use the treeSize + 16 as data section start.
	// dataBase = treeSize + 16
	// For a record value R > nodeCount:
	//   offset_in_data = R - nodeCount - 16  (MMDB spec)
	//   absolute_offset = dataBase + offset_in_data = treeSize + 16 + R - nodeCount - 16
	//                   = treeSize + R - nodeCount

	// So for US data at data section offset 0:
	// record = nodeCount + 16 + 0 = 272
	// For AU data at data section offset len(dataUS):
	// record = nodeCount + 16 + len(dataUS)

	// But our reader code does:
	//   dataOffset := record - r.meta.nodeCount - 16
	// and then r.decodeField(dataOffset)
	// This treats dataOffset as an absolute file offset, but it's actually
	// relative to the data section. This is a bug in the reader!

	// I need to fix the reader to add dataBase to the offset.

	dataRecordUS = nodeCount + 16 + 0                   // US data at section offset 0
	dataRecordAU = nodeCount + 16 + uint32(len(dataUS)) // AU data after US

	// Build the trie: 8 levels for first octet
	// Node layout: node 0 is root, nodes 1-255 are children
	// For each level i (0-7), if bit is 0 go left, if 1 go right
	nodes := make([][2]uint32, nodeCount)
	for i := range nodes {
		nodes[i] = [2]uint32{nodeCount, nodeCount} // default: empty
	}

	// Level 0-7: node i has children at (2*i+1) and (2*i+2) -- binary tree
	// But that uses too many nodes. Instead, use a sequential layout:
	// Node i has children at 2*i+1 (left) and 2*i+2 (right)
	// For 8 levels: need nodes 0-510 (2^9-1)
	// That's too many. Let's use just enough.

	// Better approach: use node positions as a simple flat array
	// Node 0 = root
	// Node i: left child = 2*i+1, right child = 2*i+2
	// For 8 bits, max node = 2^8*2-2 = 510
	// We have 256 nodes, which is not enough for full 8-level binary tree.

	// Alternative: use a simpler 1-node-per-bit approach where:
	// Following bit 0 from node N goes to node N+1
	// Following bit 1 from node N goes to... we need another node

	// Simplest correct approach: allocate nodes on demand
	// Start with node 0 (root), allocate new nodes as needed

	allocatedNodes := uint32(1) // node 0 is root
	allocateNode := func() uint32 {
		n := allocatedNodes
		allocatedNodes++
		if n >= nodeCount {
			// Need more nodes
			extra := n - uint32(len(nodes)) + 1
			for i := uint32(0); i < extra; i++ {
				nodes = append(nodes, [2]uint32{nodeCount, nodeCount})
			}
		}
		return n
	}

	// Insert 8.0.0.0/8 -> US
	insertPrefix(t, nodes, &allocatedNodes, nodeCount, allocateNode, net.ParseIP("8.0.0.0").To4(), 8, dataRecordUS)
	// Insert 1.0.0.0/8 -> AU
	insertPrefix(t, nodes, &allocatedNodes, nodeCount, allocateNode, net.ParseIP("1.0.0.0").To4(), 8, dataRecordAU)

	// Update nodeCount to actual size
	actualNodeCount := allocatedNodes
	// Adjust empty record marker and data records
	emptyRecord := actualNodeCount
	dataRecordUS = actualNodeCount + 16 + 0
	dataRecordAU = actualNodeCount + 16 + uint32(len(dataUS))

	// Re-insert with correct node count
	nodes = make([][2]uint32, actualNodeCount+1)
	for i := range nodes {
		nodes[i] = [2]uint32{emptyRecord, emptyRecord}
	}
	allocatedNodes = uint32(1)

	insertPrefix(t, nodes, &allocatedNodes, emptyRecord, allocateNode, net.ParseIP("8.0.0.0").To4(), 8, dataRecordUS)
	insertPrefix(t, nodes, &allocatedNodes, emptyRecord, allocateNode, net.ParseIP("1.0.0.0").To4(), 8, dataRecordAU)

	// Write tree section
	var tree bytes.Buffer
	for i := uint32(0); i < actualNodeCount; i++ {
		left := nodes[i][0]
		right := nodes[i][1]
		writeNodeRecord(&tree, left, right, recordSize)
	}

	// 16-byte separator
	separator := make([]byte, 16)

	// Build metadata
	var metaData bytes.Buffer
	metaData.Write(mmdbMarker)
	metaData.Write(encodeMapHeader(5))
	metaData.Write(encodeString("node_count"))
	metaData.Write(encodeUint32(actualNodeCount))
	metaData.Write(encodeString("record_size"))
	metaData.Write(encodeUint16(uint32(recordSize)))
	metaData.Write(encodeString("ip_version"))
	metaData.Write(encodeUint16(4))
	metaData.Write(encodeString("database_type"))
	metaData.Write(encodeString("GeoLite2-Country"))
	metaData.Write(encodeString("binary_format_major_version"))
	metaData.Write(encodeUint16(2))

	// Assemble file
	var file bytes.Buffer
	file.Write(tree.Bytes())
	file.Write(separator)
	file.Write(dataUS)
	file.Write(dataAU)
	file.Write(metaData.Bytes())

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.mmdb")
	if err := os.WriteFile(path, file.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to write test MMDB: %v", err)
	}

	t.Logf("Created test MMDB: %s (%d bytes), record_size=%d, node_count=%d, allocated=%d",
		path, file.Len(), recordSize, actualNodeCount, allocatedNodes)
	t.Logf("Data record US: %d, Data record AU: %d", dataRecordUS, dataRecordAU)
	t.Logf("Tree: %d bytes, Data: %d bytes, Meta: %d bytes",
		tree.Len(), len(dataUS)+len(dataAU), metaData.Len())

	if testing.Verbose() {
		maxDump := min(300, file.Len())
		t.Logf("Hex (first %d): %s", maxDump, hex.EncodeToString(file.Bytes()[:maxDump]))
	}

	return path
}

func insertPrefix(t *testing.T, nodes [][2]uint32, allocatedNodes *uint32, emptyRecord uint32, allocateNode func() uint32, ip net.IP, prefixLen int, dataRecord uint32) {
	t.Helper()
	cur := uint32(0)

	for i := 0; i < prefixLen; i++ {
		bit := (ip[i/8] >> (7 - i%8)) & 1

		if i == prefixLen-1 {
			// Last bit — set the data record
			if bit == 0 {
				nodes[cur][0] = dataRecord
			} else {
				nodes[cur][1] = dataRecord
			}
			return
		}

		// Need to follow to next node
		var next uint32
		if bit == 0 {
			next = nodes[cur][0]
			if next == emptyRecord {
				next = allocateNode()
				nodes[cur][0] = next
			}
		} else {
			next = nodes[cur][1]
			if next == emptyRecord {
				next = allocateNode()
				nodes[cur][1] = next
			}
		}
		cur = next
	}
}

func allocateNodeHelper(nodes *[][2]uint32, allocatedNodes *uint32, emptyRecord uint32) uint32 {
	n := *allocatedNodes
	*allocatedNodes++
	if n >= uint32(len(*nodes)) {
		*nodes = append(*nodes, [2]uint32{emptyRecord, emptyRecord})
	}
	return n
}

func writeNodeRecord(buf *bytes.Buffer, left, right uint32, recordSize uint16) {
	switch recordSize {
	case 24:
		buf.WriteByte(byte(left >> 16))
		buf.WriteByte(byte(left >> 8))
		buf.WriteByte(byte(left))
		buf.WriteByte(byte(right >> 16))
		buf.WriteByte(byte(right >> 8))
		buf.WriteByte(byte(right))
	case 28:
		buf.WriteByte(byte(left >> 16))
		buf.WriteByte(byte(left >> 8))
		buf.WriteByte(byte(left))
		middle := byte((left>>24)&0x0F)<<4 | byte((right>>24)&0x0F)
		buf.WriteByte(middle)
		buf.WriteByte(byte(right >> 16))
		buf.WriteByte(byte(right >> 8))
		buf.WriteByte(byte(right))
	case 32:
		var tmp [8]byte
		binary.BigEndian.PutUint32(tmp[:4], left)
		binary.BigEndian.PutUint32(tmp[4:8], right)
		buf.Write(tmp[:])
	}
}

func buildCountryData(iso, name string) []byte {
	var buf bytes.Buffer
	buf.Write(encodeMapHeader(1))
	buf.Write(encodeString("country"))
	buf.Write(encodeMapHeader(2))
	buf.Write(encodeString("iso_code"))
	buf.Write(encodeString(iso))
	buf.Write(encodeString("names"))
	buf.Write(encodeMapHeader(1))
	buf.Write(encodeString("en"))
	buf.Write(encodeString(name))
	return buf.Bytes()
}

func encodeUint32(v uint32) []byte {
	switch {
	case v <= 0xFF:
		return []byte{byte(mmdbTypeUint32)<<5 | 1, byte(v)}
	case v <= 0xFFFF:
		return []byte{byte(mmdbTypeUint32)<<5 | 2, byte(v >> 8), byte(v)}
	default:
		return []byte{byte(mmdbTypeUint32)<<5 | 4, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	}
}

func encodeUint16(v uint32) []byte {
	if v <= 0xFF {
		return []byte{byte(mmdbTypeUint16)<<5 | 1, byte(v)}
	}
	return []byte{byte(mmdbTypeUint16)<<5 | 2, byte(v >> 8), byte(v)}
}

func encodeMapHeader(count int) []byte {
	return []byte{byte(mmdbTypeMap)<<5 | byte(count)}
}

func encodeString(s string) []byte {
	return append([]byte{byte(mmdbTypeString)<<5 | byte(len(s))}, []byte(s)...)
}

func TestMMDBReader_RecordSize28(t *testing.T) {
	path := writeTestMMDB(t, 28)
	testMMDBLookups(t, path)
}

func TestMMDBReader_RecordSize24(t *testing.T) {
	path := writeTestMMDB(t, 24)
	testMMDBLookups(t, path)
}

func TestMMDBReader_RecordSize32(t *testing.T) {
	path := writeTestMMDB(t, 32)
	testMMDBLookups(t, path)
}

func testMMDBLookups(t *testing.T, path string) {
	t.Helper()

	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatalf("Failed to open MMDB: %v", err)
	}

	t.Logf("Reader: nodeCount=%d, recordSize=%d, ipVersion=%d, treeSize=%d, dataBase=%d",
		reader.meta.nodeCount, reader.meta.recordSize, reader.meta.ipVersion, reader.meta.treeSize, reader.dataBase)

	// Lookup 8.8.8.8 -> should be US
	iso, name, ok := reader.lookupCountry(net.ParseIP("8.8.8.8"))
	if !ok {
		t.Error("Expected to find 8.8.8.8")
	} else {
		t.Logf("8.8.8.8 -> %s (%s)", iso, name)
		if iso != "US" {
			t.Errorf("Expected US, got %s", iso)
		}
	}

	// Lookup 8.0.0.1 -> should be US (same /8)
	iso, name, ok = reader.lookupCountry(net.ParseIP("8.0.0.1"))
	if !ok {
		t.Error("Expected to find 8.0.0.1")
	} else if iso != "US" {
		t.Errorf("Expected US for 8.0.0.1, got %s", iso)
	}

	// Lookup 1.1.1.1 -> should be AU
	iso, name, ok = reader.lookupCountry(net.ParseIP("1.1.1.1"))
	if !ok {
		t.Error("Expected to find 1.1.1.1")
	} else {
		t.Logf("1.1.1.1 -> %s (%s)", iso, name)
		if iso != "AU" {
			t.Errorf("Expected AU, got %s", iso)
		}
	}

	// Lookup 192.168.1.1 -> should NOT be found (no entry)
	_, _, ok = reader.lookupCountry(net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("Expected 192.168.1.1 to not be found")
	}
}

func TestMMDBReader_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "invalid.mmdb")
	os.WriteFile(path, []byte("not an mmdb file"), 0644)

	_, err := newMMDBReader(path)
	if err == nil {
		t.Error("Expected error for invalid MMDB file")
	}
}

func TestMMDBReader_NonexistentFile(t *testing.T) {
	_, err := newMMDBReader("/nonexistent/path/to/file.mmdb")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestMMDBReader_ConcurrentLookups(t *testing.T) {
	path := writeTestMMDB(t, 28)

	reader, err := newMMDBReader(path)
	if err != nil {
		t.Fatalf("Failed to open MMDB: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				iso, _, ok := reader.lookupCountry(net.ParseIP("8.8.8.8"))
				if !ok {
					errCh <- fmt.Errorf("goroutine %d: expected to find 8.8.8.8", id)
					return
				}
				if iso != "US" {
					errCh <- fmt.Errorf("goroutine %d: expected US, got %s", id, iso)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

func TestMMDBReader_DataDecoding(t *testing.T) {
	data := buildCountryData("DE", "Germany")
	r := &mmdbReader{data: data}

	res, consumed := r.decodeField(0)
	if res.typ != mmdbTypeMap {
		t.Fatalf("Expected map type, got %d", res.typ)
	}
	t.Logf("Consumed %d bytes, map has %d keys", consumed, len(res.mp))

	country, ok := res.mp["country"]
	if !ok {
		t.Fatal("Missing 'country' key")
	}
	if country.mp == nil {
		t.Fatal("country value has nil map")
	}

	isoCode, ok := country.mp["iso_code"]
	if !ok {
		t.Fatal("Missing 'iso_code' in country")
	}
	if isoCode.str != "DE" {
		t.Errorf("Expected DE, got %s", isoCode.str)
	}

	names, ok := country.mp["names"]
	if !ok {
		t.Fatal("Missing 'names' in country")
	}
	en, ok := names.mp["en"]
	if !ok {
		t.Fatal("Missing 'en' in names")
	}
	if en.str != "Germany" {
		t.Errorf("Expected Germany, got %s", en.str)
	}
}
