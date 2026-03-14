//go:build darwin
// +build darwin

package cli

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func newTerminal() (*Terminal, error) {
	fd := int(os.Stdin.Fd())
	var oldState syscall.Termios

	// TIOCGETA on Darwin
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0x40487413), uintptr(unsafe.Pointer(&oldState)))
	if errno != 0 {
		return nil, fmt.Errorf("failed to get terminal attributes: %v", errno)
	}

	t := &Terminal{originalState: oldState}

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
	oldState, ok := t.originalState.(syscall.Termios)
	if !ok {
		return fmt.Errorf("invalid terminal state")
	}
	fd := int(os.Stdin.Fd())
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0x80487414), uintptr(unsafe.Pointer(&oldState)))
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
