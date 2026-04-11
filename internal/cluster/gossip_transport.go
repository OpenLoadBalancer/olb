package cluster

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// ---- Transport ----

// sendUDP sends raw bytes via UDP.
func (g *Gossip) sendUDP(addr string, msg []byte) error {
	if g.udpConn == nil {
		return fmt.Errorf("gossip: UDP connection not initialized")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	_, err = g.udpConn.WriteToUDP(msg, udpAddr)
	return err
}

// sendTCP sends raw bytes via TCP with the configured timeout.
func (g *Gossip) sendTCP(addr string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, g.config.TCPTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(g.nowFn().Add(g.config.TCPTimeout))
	// Write length-prefixed message: [totalLen: 4][msg]
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(msg)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err = conn.Write(msg)
	return err
}

// sendMessage sends a message via UDP, falling back to TCP for oversized messages.
func (g *Gossip) sendMessage(addr string, msg []byte) error {
	if len(msg) > g.config.MaxMessageSize {
		return g.sendTCP(addr, msg)
	}
	return g.sendUDP(addr, msg)
}

// ---- Background loops ----

// udpReadLoop reads messages from the UDP socket.
func (g *Gossip) udpReadLoop() {
	defer g.wg.Done()
	buf := make([]byte, 65536)
	for {
		select {
		case <-g.stopCh:
			return
		default:
		}
		g.udpConn.SetReadDeadline(g.nowFn().Add(1 * time.Second))
		n, from, err := g.udpConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-g.stopCh:
				return
			default:
				continue
			}
		}
		// Make a copy so we don't race on the buffer.
		data := make([]byte, n)
		copy(data, buf[:n])
		g.handleMessage(data, from.String())
	}
}

// tcpAcceptLoop accepts TCP connections for large messages.
func (g *Gossip) tcpAcceptLoop() {
	defer g.wg.Done()
	for {
		select {
		case <-g.stopCh:
			return
		default:
		}
		conn, err := g.tcpListener.Accept()
		if err != nil {
			select {
			case <-g.stopCh:
				return
			default:
				continue
			}
		}
		go g.handleTCPConn(conn)
	}
}

// handleTCPConn reads a length-prefixed message from a TCP connection.
func (g *Gossip) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(g.nowFn().Add(g.config.TCPTimeout))

	// Read length prefix.
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	length := binary.BigEndian.Uint32(header)
	if length > 10*1024*1024 { // 10MB safety limit
		return
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return
	}
	g.handleMessage(data, conn.RemoteAddr().String())
}
