// Package geodns provides Geo-location based DNS routing for OpenLoadBalancer.
//
// This file implements a minimal MaxMind DB (MMDB) reader for GeoLite2-Country
// databases. The format specification is documented at:
// https://maxmind.github.io/MaxMind-DB/
//
// The reader is immutable after construction and safe for concurrent use
// without synchronization.

package geodns

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
)

// mmdbMarker is the sentinel bytes that precede the metadata section.
var mmdbMarker = []byte("\xab\xcd\xefMaxMind.com")

// mmdbMetadata holds parsed database metadata.
type mmdbMetadata struct {
	nodeCount    uint32
	recordSize   uint16 // bits per record: 24, 28, or 32
	ipVersion    uint16 // 4 or 6
	databaseType string
	treeSize     uint32 // computed: (recordSize * 2 / 8) * nodeCount
}

// mmdbReader reads a MaxMind DB file.
// Immutable after construction — safe for concurrent use.
type mmdbReader struct {
	data     []byte // full file contents
	meta     mmdbMetadata
	dataBase uint32 // offset where data section starts (treeSize + 16)
}

// result represents a decoded MMDB data field (discriminated union).
type result struct {
	typ uint8
	str string
	num uint64
	bln bool
	mp  map[string]result
	arr []result
}

// MMDB data type constants (from the spec).
const (
	mmdbTypeExtended  = 0
	mmdbTypePointer   = 1
	mmdbTypeString    = 2
	mmdbTypeDouble    = 3
	mmdbTypeBytes     = 4
	mmdbTypeUint16    = 5
	mmdbTypeUint32    = 6
	mmdbTypeMap       = 7
	mmdbTypeInt32     = 8
	mmdbTypeUint64    = 9
	mmdbTypeUint128   = 10
	mmdbTypeArray     = 11
	mmdbTypeContainer = 12
	mmdbTypeEndMarker = 13
	mmdbTypeBoolean   = 14
	mmdbTypeFloat     = 15
)

// newMMDBReader opens and parses an MMDB file.
func newMMDBReader(path string) (*mmdbReader, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("geodns: read mmdb: %w", err)
	}

	metaOffset, err := findMetadata(data)
	if err != nil {
		return nil, err
	}

	meta, err := parseMetadata(data, metaOffset)
	if err != nil {
		return nil, err
	}

	if meta.recordSize != 24 && meta.recordSize != 28 && meta.recordSize != 32 {
		return nil, fmt.Errorf("geodns: unsupported record size %d", meta.recordSize)
	}

	treeSize := (uint64(meta.recordSize) * 2 / 8) * uint64(meta.nodeCount)
	if treeSize > uint64(len(data)) {
		return nil, fmt.Errorf("geodns: tree size %d exceeds file size %d", treeSize, len(data))
	}
	if treeSize > math.MaxUint32 {
		return nil, fmt.Errorf("geodns: tree size %d overflows uint32", treeSize)
	}
	meta.treeSize = uint32(treeSize)

	return &mmdbReader{
		data:     data,
		meta:     meta,
		dataBase: uint32(treeSize) + 16, // 16-byte separator between tree and data
	}, nil
}

// findMetadata locates the metadata section by searching backward for the marker.
func findMetadata(data []byte) (uint32, error) {
	// Search from the end, within last 128KB
	start := len(data) - 128*1024
	if start < 0 {
		start = 0
	}

	for i := len(data) - len(mmdbMarker); i >= start; i-- {
		found := true
		for j := 0; j < len(mmdbMarker); j++ {
			if data[i+j] != mmdbMarker[j] {
				found = false
				break
			}
		}
		if found {
			return uint32(i + len(mmdbMarker)), nil
		}
	}

	return 0, errors.New("geodns: mmdb metadata marker not found")
}

// parseMetadata decodes the metadata map at the given offset.
func parseMetadata(data []byte, offset uint32) (mmdbMetadata, error) {
	r := &mmdbReader{data: data}
	res, _ := r.decodeField(offset)

	if res.typ != mmdbTypeMap {
		return mmdbMetadata{}, fmt.Errorf("geodns: metadata is not a map (type %d)", res.typ)
	}

	mp := res.mp
	meta := mmdbMetadata{}

	if v, ok := mp["node_count"]; ok {
		meta.nodeCount = uint32(v.num)
	}
	if v, ok := mp["record_size"]; ok {
		meta.recordSize = uint16(v.num)
	}
	if v, ok := mp["ip_version"]; ok {
		meta.ipVersion = uint16(v.num)
	}
	if v, ok := mp["database_type"]; ok {
		meta.databaseType = v.str
	}

	if meta.nodeCount == 0 {
		return mmdbMetadata{}, errors.New("geodns: missing node_count in metadata")
	}
	if meta.recordSize == 0 {
		return mmdbMetadata{}, errors.New("geodns: missing record_size in metadata")
	}

	return meta, nil
}

// lookupCountry returns ISO country code and name for an IP.
func (r *mmdbReader) lookupCountry(ip net.IP) (isoCode, countryName string, ok bool) {
	res, err := r.lookup(ip)
	if err != nil || res.typ != mmdbTypeMap {
		return "", "", false
	}

	// Navigate: result.country.iso_code, result.country.names.en
	country, ok := res.mp["country"]
	if !ok {
		return "", "", false
	}

	iso, ok := country.mp["iso_code"]
	if !ok {
		return "", "", false
	}

	// Try to get English name
	name := ""
	if names, ok := country.mp["names"]; ok {
		if en, ok := names.mp["en"]; ok {
			name = en.str
		}
	}

	return iso.str, name, true
}

// lookup performs a tree search for the given IP and returns the data record.
func (r *mmdbReader) lookup(ip net.IP) (result, error) {
	// Normalize to 16-byte form for consistent bit traversal
	ip = ip.To16()
	if ip == nil {
		return result{}, errors.New("invalid IP")
	}

	// For IPv4 addresses in an IPv6 database, the tree handles this naturally
	// since the upper 96 bits are zero.
	bitCount := 128
	if r.meta.ipVersion == 4 {
		ip4 := ip.To4()
		if ip4 == nil {
			return result{}, nil // IPv6 addr in IPv4-only DB
		}
		ip = ip4
		bitCount = 32
	}

	node := uint32(0)
	for i := 0; i < bitCount; i++ {
		bit := (ip[i/8] >> (7 - i%8)) & 1
		record := r.readNode(node, int(bit))

		if record == r.meta.nodeCount {
			// Empty record — IP not found
			return result{}, nil
		}
		if record > r.meta.nodeCount {
			// Data record: offset from data section start
			dataOffset := r.dataBase + (record - r.meta.nodeCount - 16)
			if dataOffset >= uint32(len(r.data)) {
				return result{}, errors.New("data offset out of bounds")
			}
			res, _ := r.decodeField(dataOffset)
			return res, nil
		}

		node = record
	}

	return result{}, nil
}

// readNode returns the left (bit=0) or right (bit=1) record value for a node.
func (r *mmdbReader) readNode(nodeNum uint32, bit int) uint32 {
	switch r.meta.recordSize {
	case 24:
		offset := nodeNum * 6
		if offset+6 > uint32(len(r.data)) {
			return 0
		}
		if bit == 0 {
			return uint32(r.data[offset])<<16 | uint32(r.data[offset+1])<<8 | uint32(r.data[offset+2])
		}
		return uint32(r.data[offset+3])<<16 | uint32(r.data[offset+4])<<8 | uint32(r.data[offset+5])

	case 28:
		offset := nodeNum * 7
		if offset+7 > uint32(len(r.data)) {
			return 0
		}
		middle := r.data[offset+3]
		if bit == 0 {
			return uint32(r.data[offset])<<16 |
				uint32(r.data[offset+1])<<8 |
				uint32(r.data[offset+2]) |
				(uint32(middle)>>4)<<24
		}
		return uint32(r.data[offset+4])<<16 |
			uint32(r.data[offset+5])<<8 |
			uint32(r.data[offset+6]) |
			(uint32(middle)&0x0F)<<24

	case 32:
		offset := nodeNum * 8
		if offset+8 > uint32(len(r.data)) {
			return 0
		}
		if bit == 0 {
			return binary.BigEndian.Uint32(r.data[offset:])
		}
		return binary.BigEndian.Uint32(r.data[offset+4:])
	}

	return 0
}

// decodeField decodes a data field at the given offset.
// Returns the result and the total bytes consumed.
func (r *mmdbReader) decodeField(offset uint32) (result, uint32) {
	if offset >= uint32(len(r.data)) {
		return result{}, 0
	}

	control := r.data[offset]
	typeNum := (control >> 5) & 0x07

	// Extended type
	if typeNum == 0 {
		if offset+1 >= uint32(len(r.data)) {
			return result{}, 0
		}
		typeNum = r.data[offset+1] + 7
		offset++
		control = r.data[offset]
	}

	// Size from remaining 5 bits
	sizeValue := control & 0x1F
	offset++
	consumed := uint32(1)

	payloadSize, sizeBytes := r.decodeSize(r.data[offset:], sizeValue)
	offset += sizeBytes
	consumed += sizeBytes

	switch typeNum {
	case mmdbTypePointer:
		ptr, ptrBytes := r.decodePointer(r.data[offset-sizeBytes:], control)
		// Follow pointer: decode at the target offset
		if ptr < uint32(len(r.data)) {
			res, _ := r.decodeField(ptr)
			return res, consumed + ptrBytes
		}
		return result{}, consumed

	case mmdbTypeString:
		end := offset + payloadSize
		if end > uint32(len(r.data)) {
			end = uint32(len(r.data))
		}
		return result{typ: mmdbTypeString, str: string(r.data[offset:end])}, consumed + payloadSize

	case mmdbTypeDouble:
		if offset+8 <= uint32(len(r.data)) {
			bits := binary.BigEndian.Uint64(r.data[offset:])
			return result{typ: mmdbTypeDouble, num: bits}, consumed + 8
		}
		return result{}, consumed

	case mmdbTypeBytes:
		end := offset + payloadSize
		if end > uint32(len(r.data)) {
			end = uint32(len(r.data))
		}
		return result{typ: mmdbTypeBytes, str: string(r.data[offset:end])}, consumed + payloadSize

	case mmdbTypeUint16, mmdbTypeUint32, mmdbTypeUint64, mmdbTypeUint128, mmdbTypeInt32:
		var val uint64
		for i := uint32(0); i < payloadSize; i++ {
			val = (val << 8) | uint64(r.data[offset+i])
		}
		return result{typ: typeNum, num: val}, consumed + payloadSize

	case mmdbTypeMap:
		mp := make(map[string]result, payloadSize)
		totalConsumed := consumed
		fieldOffset := offset
		for i := uint32(0); i < payloadSize; i++ {
			key, keyBytes := r.decodeField(fieldOffset)
			fieldOffset += keyBytes
			val, valBytes := r.decodeField(fieldOffset)
			fieldOffset += valBytes
			totalConsumed += keyBytes + valBytes
			mp[key.str] = val
		}
		return result{typ: mmdbTypeMap, mp: mp}, totalConsumed

	case mmdbTypeArray:
		arr := make([]result, 0, payloadSize)
		totalConsumed := consumed
		fieldOffset := offset
		for i := uint32(0); i < payloadSize; i++ {
			val, valBytes := r.decodeField(fieldOffset)
			fieldOffset += valBytes
			totalConsumed += valBytes
			arr = append(arr, val)
		}
		return result{typ: mmdbTypeArray, arr: arr}, totalConsumed

	case mmdbTypeBoolean:
		return result{typ: mmdbTypeBoolean, bln: payloadSize != 0}, consumed

	case mmdbTypeFloat:
		if offset+4 <= uint32(len(r.data)) {
			bits := binary.BigEndian.Uint32(r.data[offset:])
			val := math.Float32frombits(bits)
			// Store float as uint64 bits for deterministic handling
			return result{typ: mmdbTypeFloat, num: uint64(math.Float64bits(float64(val)))}, consumed + 4
		}
		return result{}, consumed
	}

	return result{}, consumed + payloadSize
}

// decodeSize decodes the payload size from the 5-bit value and optional trailing bytes.
func (r *mmdbReader) decodeSize(data []byte, sizeValue byte) (uint32, uint32) {
	switch {
	case sizeValue <= 28:
		return uint32(sizeValue), 0
	case sizeValue == 29:
		if len(data) == 0 {
			return 29, 0
		}
		return 29 + uint32(data[0]), 1
	case sizeValue == 30:
		if len(data) < 2 {
			return 285, 0
		}
		return 285 + uint32(data[0])<<8 + uint32(data[1]), 2
	case sizeValue == 31:
		if len(data) < 3 {
			return 65821, 0
		}
		return 65821 + uint32(data[0])<<16 + uint32(data[1])<<8 + uint32(data[2]), 3
	default:
		return uint32(sizeValue), 0
	}
}

// decodePointer decodes a pointer from the data section.
// The pointer encoding uses bits from the control byte and following bytes.
func (r *mmdbReader) decodePointer(data []byte, control byte) (uint32, uint32) {
	size := (control >> 3) & 0x03
	v := control & 0x07

	switch size {
	case 0:
		if len(data) < 1 {
			return 0, 0
		}
		return uint32(v)<<8 | uint32(data[0]), 1
	case 1:
		if len(data) < 2 {
			return 0, 0
		}
		return uint32(v)<<16 | uint32(data[0])<<8 | uint32(data[1]), 2
	case 2:
		if len(data) < 3 {
			return 0, 0
		}
		return uint32(v)<<24 | uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2]), 3
	case 3:
		if len(data) < 4 {
			return 0, 0
		}
		return uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]), 4
	}
	return 0, 0
}
