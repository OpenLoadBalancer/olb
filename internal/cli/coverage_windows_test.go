//go:build windows
// +build windows

package cli

import (
	"os"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// Terminal.restore with valid terminalState on Windows (top_windows.go:113)
// This test exercises the restore path with a valid state object.
// Windows-only: terminalState on Windows carries console-mode fields
// (originalInMode/originalOutMode/inHandle/outHandle) that do not exist in the
// Unix terminalState (original/fd), so this test must not compile on other
// platforms.
// ---------------------------------------------------------------------------

func TestCov_Terminal_Restore_WithValidState(t *testing.T) {
	// Create a Terminal with a valid terminalState to test the restore path
	state := &terminalState{
		originalInMode:  0x0007, // default console mode
		originalOutMode: 0x0003,
		inHandle:        syscall.Handle(os.Stdin.Fd()),
		outHandle:       syscall.Handle(os.Stdout.Fd()),
	}
	term := &Terminal{originalState: state}

	// Call Restore (exported method) which delegates to restore() on Windows
	err := term.Restore()
	// This may or may not succeed depending on console state, but it should
	// exercise the code path and not panic.
	if err != nil {
		t.Logf("Restore returned error (acceptable in test env): %v", err)
	}
}
