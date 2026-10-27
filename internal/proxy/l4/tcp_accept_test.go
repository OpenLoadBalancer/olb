package l4

import (
	"errors"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// scriptedListener returns the given Accept errors in order.
type scriptedListener struct {
	errs  []error
	calls atomic.Int32
}

func (s *scriptedListener) Accept() (net.Conn, error) {
	i := int(s.calls.Add(1)) - 1
	if i < len(s.errs) {
		return nil, s.errs[i]
	}
	return nil, net.ErrClosed
}

func (s *scriptedListener) Close() error   { return nil }
func (s *scriptedListener) Addr() net.Addr { return &net.TCPAddr{} }

func runAcceptLoop(t *testing.T, ln net.Listener) *TCPListener {
	t.Helper()
	l := &TCPListener{name: "test", listener: ln}
	l.running.Store(true)

	done := make(chan struct{})
	go func() {
		l.acceptLoop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("acceptLoop did not return")
	}
	return l
}

// TestAcceptLoop_RetriesTransientErrors pins F026: one EMFILE must not stop
// the data plane, and transient errors must not be reported as StartError.
func TestAcceptLoop_RetriesTransientErrors(t *testing.T) {
	ln := &scriptedListener{errs: []error{
		syscall.EMFILE,
		syscall.ENFILE,
		syscall.ECONNABORTED,
		syscall.EINTR,
	}}
	l := runAcceptLoop(t, ln)

	if got := ln.calls.Load(); got != 5 {
		t.Errorf("Accept calls = %d, want 5 (4 transient retries + close)", got)
	}
	if err := l.StartError(); err != nil {
		t.Errorf("StartError = %v, want nil after transient errors", err)
	}
}

func TestAcceptLoop_TerminalErrorRecorded(t *testing.T) {
	terminal := errors.New("terminal accept failure")
	ln := &scriptedListener{errs: []error{syscall.EMFILE, terminal}}
	l := runAcceptLoop(t, ln)

	if got := ln.calls.Load(); got != 2 {
		t.Errorf("Accept calls = %d, want 2", got)
	}
	if err := l.StartError(); !errors.Is(err, terminal) {
		t.Errorf("StartError = %v, want %v", err, terminal)
	}
}
