// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"sync"
)

// InputHandler handles keyboard input.
type InputHandler struct {
	reader  *bufio.Reader
	eventCh chan<- Event
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewInputHandler creates a new input handler.
func NewInputHandler(reader *os.File, eventCh chan<- Event) *InputHandler {
	return &InputHandler{
		reader:  bufio.NewReader(reader),
		eventCh: eventCh,
		stopCh:  make(chan struct{}),
	}
}

// Start starts the input handler goroutine.
func (h *InputHandler) Start() {
	h.wg.Add(1)
	go h.readLoop()
}

// Stop stops the input handler.
func (h *InputHandler) Stop() {
	close(h.stopCh)
	h.wg.Wait()
}

// readLoop reads input and sends events.
func (h *InputHandler) readLoop() {
	defer h.wg.Done()

	for {
		select {
		case <-h.stopCh:
			return
		default:
		}

		// Set non-blocking mode would be better, but for simplicity use timeout
		if b, err := h.reader.ReadByte(); err == nil {
			// Handle escape sequences
			if b == '\x1b' {
				// Try to read the rest of the escape sequence
				h.reader.UnreadByte()
				seq, err := h.reader.Peek(3)
				if err == nil && len(seq) == 3 && seq[0] == '\x1b' && seq[1] == '[' {
					// Consume the sequence
					h.reader.Discard(3)
					// Handle arrow keys if needed
					continue
				}
			}

			// Handle Ctrl+C and Ctrl+D
			if b == '\x03' || b == '\x04' {
				h.eventCh <- Event{Type: EventQuit}
				return
			}

			// Handle regular keys
			switch b {
			case 'q', 'Q':
				h.eventCh <- Event{Type: EventKey, Key: b}
			case 'b', 'B', 'r', 'R', 'm', 'M', 'o', 'O':
				h.eventCh <- Event{Type: EventKey, Key: b}
			}
		}
	}
}

// Layout manages widget positioning.
type Layout struct {
	widgets []Widget
}

// Widget is a UI component.
type Widget interface {
	Draw(s *Screen, x, y, width, height int)
}

// NewLayout creates a new layout manager.
func NewLayout() *Layout {
	return &Layout{
		widgets: make([]Widget, 0),
	}
}

// AddWidget adds a widget to the layout.
func (l *Layout) AddWidget(w Widget) {
	l.widgets = append(l.widgets, w)
}

// Draw draws all widgets.
func (l *Layout) Draw(s *Screen) {
	for _, w := range l.widgets {
		// Widgets know their own position
		_ = w
	}
}

// Terminal provides low-level terminal control.
type Terminal struct {
	originalState any // Platform-specific state
}

// NewTerminal initializes the terminal for TUI mode.
func NewTerminal() (*Terminal, error) {
	return newTerminal()
}

// Restore restores the terminal to its original state.
func (t *Terminal) Restore() error {
	return t.restore()
}

// Helper functions

// truncate truncates a string to the given length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// formatNumber formats a number with K/M/B suffixes.
func formatNumber(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

// getTerminalSize returns the terminal size.
func getTerminalSize() (int, int) {
	return getTerminalSizePlatform()
}

// Platform-specific implementations

func init() {
	// Register the top command
	Commands = append(Commands, &TopCommand{})
}
