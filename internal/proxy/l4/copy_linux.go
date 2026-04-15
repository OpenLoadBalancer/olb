//go:build linux
// +build linux

package l4

// On Linux, io.CopyBuffer (used by copyWithBuffer and copyWithTimeout in tcp.go)
// automatically uses splice(2) for zero-copy transfer when both connections are
// *net.TCPConn, via the net.TCPConn.ReadFrom interface. No platform-specific
// code is needed here — Go's standard library handles it.
