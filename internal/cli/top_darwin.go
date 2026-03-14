//go:build darwin
// +build darwin

package cli

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Terminal manages raw terminal mode on macOS/Darwin.
type Terminal struct {
	fd       int
	oldState syscall.Termios
}

func newTerminal() (*Terminal, error) {
	fd := int(os.Stdin.Fd())
	var oldState syscall.Termios

	// TIOCGETA on Darwin
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0x40487413), uintptr(unsafe.Pointer(&oldState)))
	if errno != 0 {
		return nil, fmt.Errorf("failed to get terminal attributes: %v", errno)
	}

	t := &Terminal{fd: fd, oldState: oldState}

	// Set raw mode
	newState := oldState
	newState.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG
	newState.Iflag &^= syscall.ICRNL | syscall.IXON
	newState.Cc[syscall.VMIN] = 1
	newState.Cc[syscall.VTIME] = 0

	// TIOCSETA on Darwin
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0x80487414), uintptr(unsafe.Pointer(&newState)))
	if errno != 0 {
		return nil, fmt.Errorf("failed to set terminal attributes: %v", errno)
	}

	return t, nil
}

func (t *Terminal) restore() error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(t.fd), uintptr(0x80487414), uintptr(unsafe.Pointer(&t.oldState)))
	if errno != 0 {
		return fmt.Errorf("failed to restore terminal: %v", errno)
	}
	return nil
}

func getTerminalSizePlatform() (int, int) {
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(os.Stdout.Fd()), uintptr(0x40087468), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}
