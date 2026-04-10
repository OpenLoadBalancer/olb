// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"bufio"
	"fmt"
	"os"
)

// Color represents an ANSI color code.
type Color int

const (
	ColorDefault Color = iota
	ColorBlack
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorWhite
	ColorBrightBlack
	ColorBrightRed
	ColorBrightGreen
	ColorBrightYellow
	ColorBrightBlue
	ColorBrightMagenta
	ColorBrightCyan
	ColorBrightWhite
)

// ANSI color codes
var colorCodes = map[Color]string{
	ColorDefault:       "\x1b[0m",
	ColorBlack:         "\x1b[30m",
	ColorRed:           "\x1b[31m",
	ColorGreen:         "\x1b[32m",
	ColorYellow:        "\x1b[33m",
	ColorBlue:          "\x1b[34m",
	ColorMagenta:       "\x1b[35m",
	ColorCyan:          "\x1b[36m",
	ColorWhite:         "\x1b[37m",
	ColorBrightBlack:   "\x1b[90m",
	ColorBrightRed:     "\x1b[91m",
	ColorBrightGreen:   "\x1b[92m",
	ColorBrightYellow:  "\x1b[93m",
	ColorBrightBlue:    "\x1b[94m",
	ColorBrightMagenta: "\x1b[95m",
	ColorBrightCyan:    "\x1b[96m",
	ColorBrightWhite:   "\x1b[97m",
}

// ResetCode is the ANSI reset code.
const ResetCode = "\x1b[0m"

// Screen represents the terminal screen buffer with double buffering.
type Screen struct {
	front  []Cell
	back   []Cell
	width  int
	height int
	writer *bufio.Writer
}

// Cell represents a single character cell on the screen.
type Cell struct {
	Ch    rune
	Color Color
	Dirty bool
}

// NewScreen creates a new screen buffer.
func NewScreen() *Screen {
	return &Screen{
		writer: bufio.NewWriter(os.Stdout),
	}
}

// Reset clears the back buffer and resizes if needed.
func (s *Screen) Reset(width, height int) {
	if width != s.width || height != s.height {
		s.width = width
		s.height = height
		s.front = make([]Cell, width*height)
		s.back = make([]Cell, width*height)
	} else {
		// Mark all cells as dirty to force redraw
		for i := range s.back {
			s.back[i].Dirty = true
		}
	}
}

// Clear clears both buffers and the terminal.
func (s *Screen) Clear() {
	// Clear terminal
	fmt.Fprint(s.writer, "\x1b[2J\x1b[H")

	// Clear buffers
	for i := range s.front {
		s.front[i] = Cell{}
	}
	for i := range s.back {
		s.back[i] = Cell{Ch: ' ', Dirty: true}
	}
	s.writer.Flush()
}

// SetCell sets a cell in the back buffer.
func (s *Screen) SetCell(x, y int, ch rune, color Color) {
	if x < 0 || x >= s.width || y < 0 || y >= s.height {
		return
	}
	idx := y*s.width + x
	if s.back[idx].Ch != ch || s.back[idx].Color != color {
		s.back[idx].Ch = ch
		s.back[idx].Color = color
		s.back[idx].Dirty = true
	}
}

// DrawText draws text at the given position.
func (s *Screen) DrawText(x, y int, text string) {
	s.DrawTextColored(x, y, text, ColorDefault)
}

// DrawTextColored draws colored text at the given position.
func (s *Screen) DrawTextColored(x, y int, text string, color Color) {
	for i, ch := range text {
		s.SetCell(x+i, y, ch, color)
	}
}

// DrawBox draws a box with optional title.
func (s *Screen) DrawBox(x, y, width, height int, title string, fill bool) {
	if width < 2 || height < 2 {
		return
	}

	// Corners
	s.SetCell(x, y, BoxDrawingsLightDownAndRight, ColorDefault)
	s.SetCell(x+width-1, y, BoxDrawingsLightDownAndLeft, ColorDefault)
	s.SetCell(x, y+height-1, BoxDrawingsLightUpAndRight, ColorDefault)
	s.SetCell(x+width-1, y+height-1, BoxDrawingsLightUpAndLeft, ColorDefault)

	// Horizontal lines
	for i := 1; i < width-1; i++ {
		s.SetCell(x+i, y, BoxDrawingsLightHorizontal, ColorDefault)
		s.SetCell(x+i, y+height-1, BoxDrawingsLightHorizontal, ColorDefault)
	}

	// Vertical lines
	for i := 1; i < height-1; i++ {
		s.SetCell(x, y+i, BoxDrawingsLightVertical, ColorDefault)
		s.SetCell(x+width-1, y+i, BoxDrawingsLightVertical, ColorDefault)
	}

	// Title
	if title != "" && len(title) <= width-4 {
		titleX := x + (width-len(title))/2
		s.DrawTextColored(titleX, y, title, ColorCyan)
	}

	// Fill background
	if fill {
		for row := y + 1; row < y+height-1; row++ {
			for col := x + 1; col < x+width-1; col++ {
				s.SetCell(col, row, ' ', ColorDefault)
			}
		}
	}
}

// DrawHLine draws a horizontal line.
func (s *Screen) DrawHLine(x, y, length int) {
	for i := range length {
		s.SetCell(x+i, y, BoxDrawingsLightHorizontal, ColorDefault)
	}
}

// DrawVLine draws a vertical line.
func (s *Screen) DrawVLine(x, y, length int) {
	for i := range length {
		s.SetCell(x, y+i, BoxDrawingsLightVertical, ColorDefault)
	}
}

// DrawGauge draws a progress gauge.
func (s *Screen) DrawGauge(x, y, width, filled int, color Color) {
	// Draw border
	s.SetCell(x, y, '[', ColorDefault)
	s.SetCell(x+width-1, y, ']', ColorDefault)

	// Draw filled portion
	fillWidth := width - 2
	for i := range fillWidth {
		if i < filled {
			s.SetCell(x+1+i, y, '=', color)
		} else {
			s.SetCell(x+1+i, y, '-', ColorDefault)
		}
	}
}

// HideCursor hides the terminal cursor.
func (s *Screen) HideCursor() {
	fmt.Fprint(s.writer, "\x1b[?25l")
	s.writer.Flush()
}

// ShowCursor shows the terminal cursor.
func (s *Screen) ShowCursor() {
	fmt.Fprint(s.writer, "\x1b[?25h")
	s.writer.Flush()
}

// Flush writes dirty cells from back buffer to front buffer and terminal.
func (s *Screen) Flush() {
	var lastColor Color = -1

	for i := 0; i < len(s.back); i++ {
		if !s.back[i].Dirty {
			continue
		}

		y := i / s.width
		x := i % s.width

		// Move cursor
		fmt.Fprintf(s.writer, "\x1b[%d;%dH", y+1, x+1)

		// Set color if changed
		if s.back[i].Color != lastColor {
			if code, ok := colorCodes[s.back[i].Color]; ok {
				fmt.Fprint(s.writer, code)
			}
			lastColor = s.back[i].Color
		}

		// Write character
		fmt.Fprint(s.writer, string(s.back[i].Ch))

		// Update front buffer
		s.front[i] = s.back[i]
		s.back[i].Dirty = false
	}

	// Reset color
	if lastColor != ColorDefault {
		fmt.Fprint(s.writer, ResetCode)
	}

	s.writer.Flush()
}

// Box drawing characters
const (
	BoxDrawingsLightHorizontal            = '\u2500'
	BoxDrawingsLightVertical              = '\u2502'
	BoxDrawingsLightDownAndRight          = '\u250c'
	BoxDrawingsLightDownAndLeft           = '\u2510'
	BoxDrawingsLightUpAndRight            = '\u2514'
	BoxDrawingsLightUpAndLeft             = '\u2518'
	BoxDrawingsLightVerticalAndRight      = '\u251c'
	BoxDrawingsLightVerticalAndLeft       = '\u2524'
	BoxDrawingsLightDownAndHorizontal     = '\u252c'
	BoxDrawingsLightUpAndHorizontal       = '\u2534'
	BoxDrawingsLightVerticalAndHorizontal = '\u253c'
)
