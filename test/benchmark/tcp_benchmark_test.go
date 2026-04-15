// Package benchmark provides TCP proxy performance benchmarks for OpenLoadBalancer.
//
// Run with: go test -bench=BenchmarkTCPProxy -benchmem -count=3 ./test/benchmark/
package benchmark

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/proxy/l4"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tcpEchoServer starts a TCP server that echoes everything it reads.
// Returns the listener address and a cleanup function.
func tcpEchoServer(b *testing.B) (string, func()) {
	b.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	cleanup := func() {
		close(done)
		ln.Close()
		wg.Wait()
	}

	return ln.Addr().String(), cleanup
}

// tcpSinkServer starts a TCP server that reads and discards all data,
// then closes the connection. Useful for one-way throughput benchmarks.
func tcpSinkServer(b *testing.B) (string, func()) {
	b.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	cleanup := func() {
		close(done)
		ln.Close()
		wg.Wait()
	}

	return ln.Addr().String(), cleanup
}

// setupTCPProxy creates a TCPProxy with a single backend pointed at addr.
func setupTCPProxy(b *testing.B, addr string) (*l4.TCPProxy, *backend.Pool) {
	b.Helper()

	pool := backend.NewPool("bench-pool", "round_robin")
	be := backend.NewBackend("bench-be", addr)
	be.SetState(backend.StateUp)
	pool.AddBackend(be)

	balancer := l4.NewSimpleBalancer()
	config := l4.DefaultTCPProxyConfig()
	config.IdleTimeout = 5 * time.Second
	config.DialTimeout = 2 * time.Second

	proxy := l4.NewTCPProxy(pool, balancer, config)
	return proxy, pool
}

// ---------------------------------------------------------------------------
// TCP Proxy Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkTCPProxy_SmallPayload measures TCP proxy throughput with 1KB payloads.
func BenchmarkTCPProxy_SmallPayload(b *testing.B) {
	b.ReportAllocs()

	const payloadSize = 1024 // 1 KB
	echoAddr, cleanup := tcpEchoServer(b)
	defer cleanup()

	payload := bytes.Repeat([]byte("A"), payloadSize)
	readBuf := make([]byte, payloadSize+256)

	proxy, _ := setupTCPProxy(b, echoAddr)

	b.SetBytes(int64(payloadSize * 2)) // write + read
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clientConn, proxyConn := net.Pipe()

		go proxy.HandleConnection(proxyConn)

		// Write payload
		_, err := clientConn.Write(payload)
		if err != nil {
			clientConn.Close()
			b.Fatalf("write error: %v", err)
		}

		// Read echoed response
		total := 0
		clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for total < payloadSize {
			n, err := clientConn.Read(readBuf[total:])
			if err != nil {
				break
			}
			total += n
		}

		clientConn.Close()

		if total < payloadSize {
			b.Fatalf("short read: got %d, want %d", total, payloadSize)
		}
	}
}

// BenchmarkTCPProxy_LargePayload measures TCP proxy throughput with 1MB payloads.
func BenchmarkTCPProxy_LargePayload(b *testing.B) {
	b.ReportAllocs()

	const payloadSize = 1 << 20 // 1 MB
	sinkAddr, cleanup := tcpSinkServer(b)
	defer cleanup()

	payload := bytes.Repeat([]byte("B"), payloadSize)

	proxy, _ := setupTCPProxy(b, sinkAddr)

	b.SetBytes(int64(payloadSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clientConn, proxyConn := net.Pipe()

		go proxy.HandleConnection(proxyConn)

		// Write the full payload then close to signal completion
		_, err := clientConn.Write(payload)
		clientConn.Close()

		if err != nil {
			b.Fatalf("write error: %v", err)
		}
	}
}

// BenchmarkTCPProxy_Throughput measures sustained MB/s throughput through the
// TCP proxy using real TCP connections on localhost.
func BenchmarkTCPProxy_Throughput(b *testing.B) {
	b.ReportAllocs()

	const chunkSize = 32 * 1024 // 32KB per write
	sinkAddr, sinkCleanup := tcpSinkServer(b)
	defer sinkCleanup()

	proxy, pool := setupTCPProxy(b, sinkAddr)

	// Start a proxy listener on a random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	// Accept and hand off to proxy
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go proxy.HandleConnection(conn)
		}
	}()

	_ = pool // keep reference
	proxyAddr := ln.Addr().String()
	chunk := bytes.Repeat([]byte("C"), chunkSize)

	b.SetBytes(int64(chunkSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
		if err != nil {
			b.Fatalf("dial error: %v", err)
		}

		_, err = conn.Write(chunk)
		conn.Close()

		if err != nil {
			b.Fatalf("write error: %v", err)
		}
	}
}

// BenchmarkTCPProxy_ConcurrentConnections measures TCP proxy performance
// under parallel load from multiple goroutines.
func BenchmarkTCPProxy_ConcurrentConnections(b *testing.B) {
	b.ReportAllocs()

	const payloadSize = 1024 // 1 KB per request
	echoAddr, cleanup := tcpEchoServer(b)
	defer cleanup()

	proxy, pool := setupTCPProxy(b, echoAddr)

	// Start a proxy listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go proxy.HandleConnection(conn)
		}
	}()

	_ = pool
	proxyAddr := ln.Addr().String()
	payload := bytes.Repeat([]byte("D"), payloadSize)

	b.SetBytes(int64(payloadSize * 2)) // write + read
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		readBuf := make([]byte, payloadSize+256)
		for pb.Next() {
			conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
			if err != nil {
				b.Errorf("dial error: %v", err)
				return
			}

			_, err = conn.Write(payload)
			if err != nil {
				conn.Close()
				b.Errorf("write error: %v", err)
				return
			}

			// Read echoed response
			total := 0
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			for total < payloadSize {
				n, readErr := conn.Read(readBuf[total:])
				if readErr != nil {
					break
				}
				total += n
			}

			conn.Close()
		}
	})
}

// BenchmarkTCPProxy_LatencyOverhead measures the added latency of the TCP proxy
// compared to a direct connection by timing round-trip echo requests.
func BenchmarkTCPProxy_LatencyOverhead(b *testing.B) {
	const payloadSize = 64 // small payload to isolate latency

	echoAddr, echoCleanup := tcpEchoServer(b)
	defer echoCleanup()

	payload := bytes.Repeat([]byte("E"), payloadSize)

	// Sub-benchmark: direct connection latency
	b.Run("Direct", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(payloadSize * 2))
		readBuf := make([]byte, payloadSize+64)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			conn, err := net.DialTimeout("tcp", echoAddr, 2*time.Second)
			if err != nil {
				b.Fatalf("dial error: %v", err)
			}

			_, err = conn.Write(payload)
			if err != nil {
				conn.Close()
				b.Fatalf("write error: %v", err)
			}

			total := 0
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			for total < payloadSize {
				n, readErr := conn.Read(readBuf[total:])
				if readErr != nil {
					break
				}
				total += n
			}

			conn.Close()
		}
	})

	// Sub-benchmark: proxied connection latency
	b.Run("Proxied", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(payloadSize * 2))

		proxy, pool := setupTCPProxy(b, echoAddr)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			b.Fatalf("failed to listen: %v", err)
		}
		defer ln.Close()

		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go proxy.HandleConnection(conn)
			}
		}()

		_ = pool
		proxyAddr := ln.Addr().String()
		readBuf := make([]byte, payloadSize+64)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
			if err != nil {
				b.Fatalf("dial error: %v", err)
			}

			_, err = conn.Write(payload)
			if err != nil {
				conn.Close()
				b.Fatalf("write error: %v", err)
			}

			total := 0
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			for total < payloadSize {
				n, readErr := conn.Read(readBuf[total:])
				if readErr != nil {
					break
				}
				total += n
			}

			conn.Close()
		}
	})
}

// ---------------------------------------------------------------------------
// CopyBidirectional Benchmark
// ---------------------------------------------------------------------------

// BenchmarkCopyBidirectional measures the throughput of the bidirectional
// copy function using net.Pipe pairs.
func BenchmarkCopyBidirectional(b *testing.B) {
	b.ReportAllocs()

	const chunkSize = 32 * 1024 // 32 KB
	chunk := bytes.Repeat([]byte("F"), chunkSize)

	b.SetBytes(int64(chunkSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn1Client, conn1Server := net.Pipe()
		conn2Client, conn2Server := net.Pipe()

		// Simulate: conn1Client -> conn1Server -- bidirectional -- conn2Server -> conn2Client
		done := make(chan struct{})

		go func() {
			defer close(done)
			l4.CopyBidirectional(conn1Server, conn2Server, time.Second)
		}()

		// Write on conn1Client, read on conn2Client
		go func() {
			conn1Client.Write(chunk)
			conn1Client.Close()
		}()

		buf := make([]byte, chunkSize+256)
		total := 0
		conn2Client.SetReadDeadline(time.Now().Add(2 * time.Second))
		for total < chunkSize {
			n, err := conn2Client.Read(buf[total:])
			if err != nil {
				break
			}
			total += n
		}

		conn2Client.Close()
		<-done
	}
}

// ---------------------------------------------------------------------------
// Sustained throughput benchmark
// ---------------------------------------------------------------------------

// BenchmarkTCPProxy_SustainedThroughput measures throughput over a single
// long-lived TCP connection through the proxy. Reports bytes/second.
// On Linux with splice(), this would measure kernel-level zero-copy throughput.
func BenchmarkTCPProxy_SustainedThroughput(b *testing.B) {
	b.ReportAllocs()

	backendAddr, backendCleanup := tcpGenerateServer(b, 10*time.Second)
	defer backendCleanup()

	proxy, _ := setupTCPProxy(b, backendAddr)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go proxy.HandleConnection(conn)
		}
	}()

	proxyAddr := ln.Addr().String()
	const chunkSize = 64 * 1024

	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, chunkSize)
	totalBytes := int64(0)

	b.SetBytes(int64(chunkSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		totalBytes += int64(n)
	}

	b.ReportMetric(float64(totalBytes)/b.Elapsed().Seconds()/1024/1024, "MB/s")
}

// tcpGenerateServer creates a TCP server that continuously sends data
// until the connection is closed or duration expires.
func tcpGenerateServer(b *testing.B, duration time.Duration) (string, func()) {
	b.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen: %v", err)
	}

	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}

			go func(c net.Conn) {
				defer c.Close()
				data := make([]byte, 64*1024)
				deadline := time.Now().Add(duration)
				c.SetWriteDeadline(deadline)
				for {
					_, err := c.Write(data)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	cleanup := func() {
		ln.Close()
		close(done)
	}

	return ln.Addr().String(), cleanup
}
