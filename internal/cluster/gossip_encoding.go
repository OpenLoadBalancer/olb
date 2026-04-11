package cluster

import (
	"encoding/binary"
	"fmt"
)

// ---- Binary message encoding/decoding ----

// Message wire format:
//   [type: 1 byte][length: 2 bytes][payload: variable]
//
// Payload varies by message type. All multi-byte integers are big-endian.

// encodeMessage creates a wire-format message from type and payload.
func encodeMessage(msgType MessageType, payload []byte) []byte {
	buf := make([]byte, 3+len(payload))
	buf[0] = byte(msgType)
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(payload)))
	copy(buf[3:], payload)
	return buf
}

// decodeMessage splits a wire-format message into type, payload, and remaining bytes.
func decodeMessage(data []byte) (MessageType, []byte, []byte, error) {
	if len(data) < 3 {
		return 0, nil, nil, fmt.Errorf("gossip: message too short: %d bytes", len(data))
	}
	msgType := MessageType(data[0])
	length := binary.BigEndian.Uint16(data[1:3])
	if int(length) > len(data)-3 {
		return 0, nil, nil, fmt.Errorf("gossip: message payload truncated: want %d, have %d", length, len(data)-3)
	}
	payload := data[3 : 3+length]
	remaining := data[3+length:]
	return msgType, payload, remaining, nil
}

// PING payload: [seqNo: 4][senderIDLen: 2][senderID: var][targetIDLen: 2][targetID: var]
func encodePing(seqNo uint32, senderID, targetID string) []byte {
	payload := make([]byte, 4+2+len(senderID)+2+len(targetID))
	binary.BigEndian.PutUint32(payload[0:4], seqNo)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(senderID)))
	copy(payload[6:6+len(senderID)], senderID)
	off := 6 + len(senderID)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(targetID)))
	copy(payload[off+2:], targetID)
	return encodeMessage(MsgPing, payload)
}

// decodePing parses a PING payload.
func decodePing(payload []byte) (seqNo uint32, senderID, targetID string, err error) {
	if len(payload) < 8 {
		return 0, "", "", fmt.Errorf("gossip: ping payload too short")
	}
	seqNo = binary.BigEndian.Uint32(payload[0:4])
	senderLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(senderLen)+2 {
		return 0, "", "", fmt.Errorf("gossip: ping payload truncated at sender")
	}
	senderID = string(payload[6 : 6+senderLen])
	off := 6 + int(senderLen)
	targetLen := binary.BigEndian.Uint16(payload[off : off+2])
	if len(payload) < off+2+int(targetLen) {
		return 0, "", "", fmt.Errorf("gossip: ping payload truncated at target")
	}
	targetID = string(payload[off+2 : off+2+int(targetLen)])
	return seqNo, senderID, targetID, nil
}

// ACK payload: [seqNo: 4][senderIDLen: 2][senderID: var]
func encodeAck(seqNo uint32, senderID string) []byte {
	payload := make([]byte, 4+2+len(senderID))
	binary.BigEndian.PutUint32(payload[0:4], seqNo)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(senderID)))
	copy(payload[6:], senderID)
	return encodeMessage(MsgAck, payload)
}

// decodeAck parses an ACK payload.
func decodeAck(payload []byte) (seqNo uint32, senderID string, err error) {
	if len(payload) < 6 {
		return 0, "", fmt.Errorf("gossip: ack payload too short")
	}
	seqNo = binary.BigEndian.Uint32(payload[0:4])
	senderLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(senderLen) {
		return 0, "", fmt.Errorf("gossip: ack payload truncated")
	}
	senderID = string(payload[6 : 6+senderLen])
	return seqNo, senderID, nil
}

// PING_REQ payload: [seqNo: 4][senderIDLen: 2][senderID: var][targetIDLen: 2][targetID: var]
func encodePingReq(seqNo uint32, senderID, targetID string) []byte {
	payload := make([]byte, 4+2+len(senderID)+2+len(targetID))
	binary.BigEndian.PutUint32(payload[0:4], seqNo)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(senderID)))
	copy(payload[6:6+len(senderID)], senderID)
	off := 6 + len(senderID)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(targetID)))
	copy(payload[off+2:], targetID)
	return encodeMessage(MsgPingReq, payload)
}

// decodePingReq parses a PING_REQ payload.
func decodePingReq(payload []byte) (seqNo uint32, senderID, targetID string, err error) {
	return decodePing(payload) // same format
}

// encodeNodePayload encodes a node's identity into a byte slice.
// Format: [incarnation: 4][nodeIDLen: 2][nodeID: var][addrLen: 2][addr: var][port: 2][metaCount: 2][{keyLen: 2, key, valLen: 2, val}...]
func encodeNodePayload(incarnation uint32, nodeID, address string, port int, metadata map[string]string) []byte {
	size := 4 + 2 + len(nodeID) + 2 + len(address) + 2 + 2
	for k, v := range metadata {
		size += 2 + len(k) + 2 + len(v)
	}
	payload := make([]byte, size)
	binary.BigEndian.PutUint32(payload[0:4], incarnation)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(nodeID)))
	copy(payload[6:6+len(nodeID)], nodeID)
	off := 6 + len(nodeID)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(address)))
	copy(payload[off+2:off+2+len(address)], address)
	off += 2 + len(address)
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(port))
	off += 2
	binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(metadata)))
	off += 2
	for k, v := range metadata {
		binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(k)))
		copy(payload[off+2:off+2+len(k)], k)
		off += 2 + len(k)
		binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(v)))
		copy(payload[off+2:off+2+len(v)], v)
		off += 2 + len(v)
	}
	return payload
}

// decodeNodePayload decodes a node identity payload.
func decodeNodePayload(payload []byte) (incarnation uint32, nodeID, address string, port int, metadata map[string]string, err error) {
	if len(payload) < 12 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload too short")
	}
	incarnation = binary.BigEndian.Uint32(payload[0:4])
	nodeIDLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(nodeIDLen)+2 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at nodeID")
	}
	nodeID = string(payload[6 : 6+nodeIDLen])
	off := 6 + int(nodeIDLen)
	addrLen := binary.BigEndian.Uint16(payload[off : off+2])
	if len(payload) < off+2+int(addrLen)+2+2 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at address")
	}
	address = string(payload[off+2 : off+2+int(addrLen)])
	off += 2 + int(addrLen)
	port = int(binary.BigEndian.Uint16(payload[off : off+2]))
	off += 2
	if len(payload) < off+2 {
		return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta count")
	}
	metaCount := binary.BigEndian.Uint16(payload[off : off+2])
	off += 2
	metadata = make(map[string]string, metaCount)
	for range int(metaCount) {
		if len(payload) < off+2 {
			return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta key len")
		}
		kLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(kLen)+2 {
			return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta key")
		}
		k := string(payload[off : off+int(kLen)])
		off += int(kLen)
		vLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(vLen) {
			return 0, "", "", 0, nil, fmt.Errorf("gossip: node payload truncated at meta value")
		}
		v := string(payload[off : off+int(vLen)])
		off += int(vLen)
		metadata[k] = v
	}
	return incarnation, nodeID, address, port, metadata, nil
}

// encodeSuspect encodes a SUSPECT message.
func encodeSuspect(incarnation uint32, nodeID string) []byte {
	payload := make([]byte, 4+2+len(nodeID))
	binary.BigEndian.PutUint32(payload[0:4], incarnation)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(nodeID)))
	copy(payload[6:], nodeID)
	return encodeMessage(MsgSuspect, payload)
}

// decodeSuspect parses a SUSPECT payload.
func decodeSuspect(payload []byte) (incarnation uint32, nodeID string, err error) {
	if len(payload) < 6 {
		return 0, "", fmt.Errorf("gossip: suspect payload too short")
	}
	incarnation = binary.BigEndian.Uint32(payload[0:4])
	nodeIDLen := binary.BigEndian.Uint16(payload[4:6])
	if len(payload) < 6+int(nodeIDLen) {
		return 0, "", fmt.Errorf("gossip: suspect payload truncated")
	}
	nodeID = string(payload[6 : 6+nodeIDLen])
	return incarnation, nodeID, nil
}

// encodeAlive encodes an ALIVE message with full node info.
func encodeAlive(incarnation uint32, nodeID, address string, port int, metadata map[string]string) []byte {
	return encodeMessage(MsgAlive, encodeNodePayload(incarnation, nodeID, address, port, metadata))
}

// decodeAlive parses an ALIVE payload.
func decodeAlive(payload []byte) (incarnation uint32, nodeID, address string, port int, metadata map[string]string, err error) {
	return decodeNodePayload(payload)
}

// encodeDead encodes a DEAD message.
func encodeDead(incarnation uint32, nodeID string) []byte {
	payload := make([]byte, 4+2+len(nodeID))
	binary.BigEndian.PutUint32(payload[0:4], incarnation)
	binary.BigEndian.PutUint16(payload[4:6], uint16(len(nodeID)))
	copy(payload[6:], nodeID)
	return encodeMessage(MsgDead, payload)
}

// decodeDead parses a DEAD payload.
func decodeDead(payload []byte) (incarnation uint32, nodeID string, err error) {
	return decodeSuspect(payload) // same format
}

// encodeJoinMessage encodes a JOIN message for the local node.
func (g *Gossip) encodeJoinMessage() []byte {
	g.localMu.RLock()
	inc := g.localNode.Incarnation
	id := g.localNode.ID
	addr := g.localNode.Address
	port := g.localNode.Port
	meta := copyMetadata(g.localNode.Metadata)
	g.localMu.RUnlock()
	return encodeMessage(MsgJoin, encodeNodePayload(inc, id, addr, port, meta))
}

// encodeLeaveMessage encodes a LEAVE message for a node.
func (g *Gossip) encodeLeaveMessage(node *GossipNode) []byte {
	return encodeMessage(MsgLeave, encodeNodePayload(
		node.Incarnation,
		node.ID,
		node.Address,
		node.Port,
		copyMetadata(node.Metadata),
	))
}

// encodeCompound wraps multiple messages into a single compound message.
func encodeCompound(messages [][]byte) []byte {
	// Compound payload: [count: 2][{msgLen: 2, msg}...]
	total := 2
	for _, m := range messages {
		total += 2 + len(m)
	}
	payload := make([]byte, total)
	binary.BigEndian.PutUint16(payload[0:2], uint16(len(messages)))
	off := 2
	for _, m := range messages {
		binary.BigEndian.PutUint16(payload[off:off+2], uint16(len(m)))
		copy(payload[off+2:off+2+len(m)], m)
		off += 2 + len(m)
	}
	return encodeMessage(MsgCompound, payload)
}

// decodeCompound splits a compound payload into individual messages.
func decodeCompound(payload []byte) ([][]byte, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("gossip: compound payload too short")
	}
	count := binary.BigEndian.Uint16(payload[0:2])
	off := 2
	messages := make([][]byte, 0, count)
	for i := range int(count) {
		if len(payload) < off+2 {
			return nil, fmt.Errorf("gossip: compound truncated at message %d length", i)
		}
		msgLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(msgLen) {
			return nil, fmt.Errorf("gossip: compound truncated at message %d data", i)
		}
		messages = append(messages, payload[off:off+int(msgLen)])
		off += int(msgLen)
	}
	return messages, nil
}
