//go:build linux
// +build linux

package cli

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Terminal implementation for Unix systems using termios.

type termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Cc     [20]byte
	Ispeed uint32
	Ospeed uint32
}

const (
	// Local modes
	ECHO   = 0x00000008
	ICANON = 0x00000002
	ISIG   = 0x00000001

	// Input modes
	ICRNL = 0x00000100
	INLCR = 0x00000040
	IGNCR = 0x00000080

	// Control characters
	VMIN  = 6
	VTIME = 5
)

// terminalState holds the original terminal state.
type terminalState struct {
	original termios
	fd       int
}

// newTerminal initializes the terminal for raw mode on Unix.
func newTerminal() (*Terminal, error) {
	fd := int(os.Stdin.Fd())

	// Get current terminal state
	var original termios
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&original)),
	)
	if errno != 0 {
		return nil, fmt.Errorf("tcgetattr failed: %v", errno)
	}

	// Save original state
	state := &terminalState{
		original: original,
		fd:       fd,
	}

	// Create raw mode settings
	raw := original
	raw.Iflag &^= ICRNL | INLCR | IGNCR
	raw.Lflag &^= ECHO | ICANON | ISIG
	raw.Cc[VMIN] = 0
	raw.Cc[VTIME] = 0

	// Apply raw mode
	_, _, errno = syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(syscall.TCSETS),
		uintptr(unsafe.Pointer(&raw)),
	)
	if errno != 0 {
		return nil, fmt.Errorf("tcsetattr failed: %v", errno)
	}

	return &Terminal{
		originalState: state,
	}, nil
}

// restore restores the terminal to its original state on Unix.
func (t *Terminal) restore() error {
	state, ok := t.originalState.(*terminalState)
	if !ok {
		return fmt.Errorf("invalid terminal state")
	}

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(state.fd),
		uintptr(syscall.TCSETS),
		uintptr(unsafe.Pointer(&state.original)),
	)
	if errno != 0 {
		return fmt.Errorf("tcsetattr failed: %v", errno)
	}

	// Clear screen and show cursor
	fmt.Print("\x1b[2J\x1b[H\x1b[?25h")
	return nil
}

// getTerminalSizePlatform returns the terminal size on Unix.
func getTerminalSizePlatform() (int, int) {
	ws := &struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{}

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(os.Stdout.Fd()),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)),
	)
	if errno != 0 {
		return 80, 24 // Default size
	}

	return int(ws.Col), int(ws.Row)
}
