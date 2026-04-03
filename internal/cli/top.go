// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/admin"
)

// TopCommand implements the "olb top" TUI dashboard command.
type TopCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *TopCommand) Name() string {
	return "top"
}

// Description returns the command description.
func (c *TopCommand) Description() string {
	return "Interactive TUI dashboard for real-time monitoring"
}

// Run executes the top command.
func (c *TopCommand) Run(args []string) error {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create metrics fetcher
	fetcher := NewMetricsFetcher(c.apiAddr)

	// Create and run TUI
	tui := NewTUI(fetcher)
	return tui.Run()
}

// MetricsFetcher fetches metrics from the admin API.
type MetricsFetcher struct {
	apiAddr string
	client  *http.Client
}

// NewMetricsFetcher creates a new metrics fetcher.
func NewMetricsFetcher(apiAddr string) *MetricsFetcher {
	return &MetricsFetcher{
		apiAddr: apiAddr,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// FetchSystemInfo fetches system information.
func (f *MetricsFetcher) FetchSystemInfo() (*admin.SystemInfo, error) {
	url := fmt.Sprintf("http://%s/api/v1/system/info", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var info admin.SystemInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchBackends fetches backend information.
func (f *MetricsFetcher) FetchBackends() ([]admin.BackendPool, error) {
	url := fmt.Sprintf("http://%s/api/v1/backends", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var pools []admin.BackendPool
	if err := json.Unmarshal(data, &pools); err != nil {
		return nil, err
	}
	return pools, nil
}

// FetchRoutes fetches route information.
func (f *MetricsFetcher) FetchRoutes() ([]admin.Route, error) {
	url := fmt.Sprintf("http://%s/api/v1/routes", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var routes []admin.Route
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}

// FetchHealth fetches health status.
func (f *MetricsFetcher) FetchHealth() (*admin.HealthStatus, error) {
	url := fmt.Sprintf("http://%s/api/v1/system/health", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var health admin.HealthStatus
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, err
	}
	return &health, nil
}

// View represents the current view mode.
type View int

const (
	ViewOverview View = iota
	ViewBackends
	ViewRoutes
	ViewMetrics
)

// TUI is the terminal user interface for the top command.
type TUI struct {
	fetcher     *MetricsFetcher
	screen      *Screen
	input       *InputHandler
	layout      *Layout
	eventCh     chan Event
	stopCh      chan struct{}
	refreshCh   chan struct{}
	currentView View
	data        *DashboardData
	dataMu      sync.RWMutex
	running     atomic.Bool
	lastError   string
}

// DashboardData holds all the data displayed in the dashboard.
type DashboardData struct {
	SystemInfo *admin.SystemInfo
	Pools      []admin.BackendPool
	Routes     []admin.Route
	Health     *admin.HealthStatus
	Timestamp  time.Time
}

// Event represents a user input event.
type Event struct {
	Type EventType
	Key  byte
}

// EventType represents the type of event.
type EventType int

const (
	EventKey EventType = iota
	EventResize
	EventQuit
)

// NewTUI creates a new TUI instance.
func NewTUI(fetcher *MetricsFetcher) *TUI {
	return &TUI{
		fetcher:     fetcher,
		eventCh:     make(chan Event, 10),
		stopCh:      make(chan struct{}),
		refreshCh:   make(chan struct{}, 1),
		currentView: ViewOverview,
		data:        &DashboardData{},
	}
}

// Run starts the TUI event loop.
func (t *TUI) Run() error {
	if !t.running.CompareAndSwap(false, true) {
		return fmt.Errorf("TUI already running")
	}
	defer t.running.Store(false)

	// Initialize terminal
	term, err := NewTerminal()
	if err != nil {
		return fmt.Errorf("failed to initialize terminal: %w", err)
	}
	defer term.Restore()

	// Initialize screen
	t.screen = NewScreen()

	// Initialize input handler
	t.input = NewInputHandler(os.Stdin, t.eventCh)
	t.input.Start()
	defer t.input.Stop()

	// Initialize layout
	t.layout = NewLayout()

	// Initial data fetch
	t.fetchData()

	// Start refresh ticker
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Clear screen and hide cursor
	t.screen.Clear()
	t.screen.HideCursor()
	defer t.screen.ShowCursor()

	// Main event loop
	for {
		select {
		case <-t.stopCh:
			return nil

		case event := <-t.eventCh:
			if t.handleEvent(event) {
				return nil
			}

		case <-ticker.C:
			t.fetchData()
			t.render()

		case <-t.refreshCh:
			t.render()
		}
	}
}

// handleEvent handles user input events.
// Returns true if the TUI should exit.
func (t *TUI) handleEvent(e Event) bool {
	switch e.Type {
	case EventQuit:
		return true
	case EventKey:
		switch e.Key {
		case 'q', 'Q':
			return true
		case 'b', 'B':
			t.currentView = ViewBackends
			t.render()
		case 'r', 'R':
			t.currentView = ViewRoutes
			t.render()
		case 'm', 'M':
			t.currentView = ViewMetrics
			t.render()
		case 'o', 'O':
			t.currentView = ViewOverview
			t.render()
		}
	}
	return false
}

// fetchData fetches all data from the API.
func (t *TUI) fetchData() {
	data := &DashboardData{
		Timestamp: time.Now(),
	}

	// Fetch in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(4)

	go func() {
		defer wg.Done()
		if info, err := t.fetcher.FetchSystemInfo(); err == nil {
			mu.Lock()
			data.SystemInfo = info
			mu.Unlock()
		} else {
			t.lastError = err.Error()
		}
	}()

	go func() {
		defer wg.Done()
		if pools, err := t.fetcher.FetchBackends(); err == nil {
			mu.Lock()
			data.Pools = pools
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		if routes, err := t.fetcher.FetchRoutes(); err == nil {
			mu.Lock()
			data.Routes = routes
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		if health, err := t.fetcher.FetchHealth(); err == nil {
			mu.Lock()
			data.Health = health
			mu.Unlock()
		}
	}()

	wg.Wait()

	t.dataMu.Lock()
	t.data = data
	t.dataMu.Unlock()
}

// render draws the current view.
func (t *TUI) render() {
	// Skip rendering if screen is not initialized (for testing)
	if t.screen == nil {
		return
	}

	t.dataMu.RLock()
	data := t.data
	t.dataMu.RUnlock()

	// Get terminal size
	width, height := getTerminalSize()
	if width < 40 || height < 10 {
		return // Terminal too small
	}

	// Reset screen buffer
	t.screen.Reset(width, height)

	// Render based on current view
	switch t.currentView {
	case ViewOverview:
		t.renderOverview(data, width, height)
	case ViewBackends:
		t.renderBackends(data, width, height)
	case ViewRoutes:
		t.renderRoutes(data, width, height)
	case ViewMetrics:
		t.renderMetrics(data, width, height)
	}

	// Render help bar at bottom
	t.renderHelpBar(width, height)

	// Flush to terminal
	t.screen.Flush()
}

// renderOverview renders the overview view.
func (t *TUI) renderOverview(data *DashboardData, width, height int) {
	// Title bar
	t.screen.DrawBox(0, 0, width, 3, "OpenLoadBalancer Dashboard", true)

	// System info box
	infoLines := []string{
		fmt.Sprintf("Version: %s", t.getString(data.SystemInfo.Version, "unknown")),
		fmt.Sprintf("Uptime:  %s", t.getString(data.SystemInfo.Uptime, "unknown")),
		fmt.Sprintf("State:   %s", t.getString(data.SystemInfo.State, "unknown")),
	}
	t.screen.DrawBox(0, 3, width/2, 6, "System Info", false)
	for i, line := range infoLines {
		t.screen.DrawText(2, 5+i, line)
	}

	// Health status box
	healthStatus := "unknown"
	if data.Health != nil {
		healthStatus = data.Health.Status
	}
	statusColor := ColorGreen
	if healthStatus != "healthy" {
		statusColor = ColorRed
	}
	t.screen.DrawBox(width/2, 3, width-width/2, 6, "Health", false)
	t.screen.DrawTextColored(width/2+2, 5, fmt.Sprintf("Status: %s", healthStatus), statusColor)

	// Summary box
	totalBackends := 0
	healthyBackends := 0
	for _, pool := range data.Pools {
		totalBackends += len(pool.Backends)
		for _, b := range pool.Backends {
			if b.Healthy {
				healthyBackends++
			}
		}
	}

	t.screen.DrawBox(0, 9, width, 6, "Summary", false)
	t.screen.DrawText(2, 11, fmt.Sprintf("Pools:    %d", len(data.Pools)))
	t.screen.DrawText(2, 12, fmt.Sprintf("Backends: %d total, %d healthy", totalBackends, healthyBackends))
	t.screen.DrawText(2, 13, fmt.Sprintf("Routes:   %d", len(data.Routes)))

	// Last update
	t.screen.DrawText(2, height-2, fmt.Sprintf("Last update: %s", data.Timestamp.Format("15:04:05")))

	if t.lastError != "" {
		t.screen.DrawTextColored(2, height-3, fmt.Sprintf("Error: %s", t.lastError), ColorRed)
	}
}

// renderBackends renders the backends view.
func (t *TUI) renderBackends(data *DashboardData, width, height int) {
	// Title bar
	t.screen.DrawBox(0, 0, width, 3, "Backends", true)

	// Calculate table dimensions
	tableY := 4
	tableHeight := height - tableY - 3

	// Table headers
	colWidths := []int{20, 30, 10, 10, 15, 15}
	headers := []string{"ID", "Address", "Weight", "Status", "Requests", "Errors"}

	// Draw table header
	t.screen.DrawBox(0, 3, width, tableHeight+2, "", false)
	x := 2
	for i, h := range headers {
		t.screen.DrawTextColored(x, tableY, truncate(h, colWidths[i]), ColorCyan)
		x += colWidths[i] + 2
	}

	// Draw separator
	t.screen.DrawHLine(1, tableY+1, width-2)

	// Draw backend rows
	row := tableY + 2
	for _, pool := range data.Pools {
		if row >= tableY+tableHeight {
			break
		}
		// Pool name
		t.screen.DrawTextColored(2, row, fmt.Sprintf("Pool: %s (%s)", pool.Name, pool.Algorithm), ColorYellow)
		row++

		for _, b := range pool.Backends {
			if row >= tableY+tableHeight {
				break
			}

			status := "healthy"
			statusColor := ColorGreen
			if !b.Healthy {
				status = "unhealthy"
				statusColor = ColorRed
			}

			x := 2
			t.screen.DrawText(x, row, truncate(b.ID, colWidths[0]))
			x += colWidths[0] + 2
			t.screen.DrawText(x, row, truncate(b.Address, colWidths[1]))
			x += colWidths[1] + 2
			t.screen.DrawText(x, row, fmt.Sprintf("%d", b.Weight))
			x += colWidths[2] + 2
			t.screen.DrawTextColored(x, row, truncate(status, colWidths[3]), statusColor)
			x += colWidths[3] + 2
			t.screen.DrawText(x, row, formatNumber(b.Requests))
			x += colWidths[4] + 2
			t.screen.DrawText(x, row, formatNumber(b.Errors))

			row++
		}
		row++ // Space between pools
	}
}

// renderRoutes renders the routes view.
func (t *TUI) renderRoutes(data *DashboardData, width, height int) {
	// Title bar
	t.screen.DrawBox(0, 0, width, 3, "Routes", true)

	// Calculate table dimensions
	tableY := 4
	tableHeight := height - tableY - 3

	// Table headers
	colWidths := []int{20, 20, 25, 20, 10}
	headers := []string{"Name", "Host", "Path", "Backend Pool", "Priority"}

	// Draw table header
	t.screen.DrawBox(0, 3, width, tableHeight+2, "", false)
	x := 2
	for i, h := range headers {
		t.screen.DrawTextColored(x, tableY, truncate(h, colWidths[i]), ColorCyan)
		x += colWidths[i] + 2
	}

	// Draw separator
	t.screen.DrawHLine(1, tableY+1, width-2)

	// Draw route rows
	row := tableY + 2
	for _, r := range data.Routes {
		if row >= tableY+tableHeight {
			break
		}

		host := r.Host
		if host == "" {
			host = "*"
		}

		x := 2
		t.screen.DrawText(x, row, truncate(r.Name, colWidths[0]))
		x += colWidths[0] + 2
		t.screen.DrawText(x, row, truncate(host, colWidths[1]))
		x += colWidths[1] + 2
		t.screen.DrawText(x, row, truncate(r.Path, colWidths[2]))
		x += colWidths[2] + 2
		t.screen.DrawText(x, row, truncate(r.BackendPool, colWidths[3]))
		x += colWidths[3] + 2
		t.screen.DrawText(x, row, fmt.Sprintf("%d", r.Priority))

		row++
	}
}

// renderMetrics renders the metrics view.
func (t *TUI) renderMetrics(data *DashboardData, width, height int) {
	// Title bar
	t.screen.DrawBox(0, 0, width, 3, "Metrics", true)

	// Calculate totals
	var totalRequests, totalErrors int64
	for _, pool := range data.Pools {
		for _, b := range pool.Backends {
			totalRequests += b.Requests
			totalErrors += b.Errors
		}
	}

	errorRate := float64(0)
	if totalRequests > 0 {
		errorRate = float64(totalErrors) / float64(totalRequests) * 100
	}

	// Metrics boxes
	t.screen.DrawBox(0, 3, width/2, 5, "Request Metrics", false)
	t.screen.DrawText(2, 5, fmt.Sprintf("Total Requests: %s", formatNumber(totalRequests)))
	t.screen.DrawText(2, 6, fmt.Sprintf("Total Errors:   %s", formatNumber(totalErrors)))
	t.screen.DrawText(2, 7, fmt.Sprintf("Error Rate:     %.2f%%", errorRate))

	// Draw error rate gauge
	gaugeWidth := width/2 - 4
	filled := int(errorRate / 100 * float64(gaugeWidth))
	if filled > gaugeWidth {
		filled = gaugeWidth
	}
	gaugeColor := ColorGreen
	if errorRate > 1 {
		gaugeColor = ColorYellow
	}
	if errorRate > 5 {
		gaugeColor = ColorRed
	}
	t.screen.DrawGauge(2, 8, gaugeWidth, filled, gaugeColor)
}

// renderHelpBar renders the help bar at the bottom.
func (t *TUI) renderHelpBar(width, height int) {
	help := "q:Quit  o:Overview  b:Backends  r:Routes  m:Metrics"
	t.screen.DrawHLine(0, height-2, width)
	t.screen.DrawTextColored(2, height-1, help, ColorWhite)

	// View indicator
	viewName := "Overview"
	switch t.currentView {
	case ViewBackends:
		viewName = "Backends"
	case ViewRoutes:
		viewName = "Routes"
	case ViewMetrics:
		viewName = "Metrics"
	}
	t.screen.DrawTextColored(width-len(viewName)-4, height-1, "["+viewName+"]", ColorCyan)
}

// getString returns the string value or default if empty.
func (t *TUI) getString(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

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
	for i := 0; i < length; i++ {
		s.SetCell(x+i, y, BoxDrawingsLightHorizontal, ColorDefault)
	}
}

// DrawVLine draws a vertical line.
func (s *Screen) DrawVLine(x, y, length int) {
	for i := 0; i < length; i++ {
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
	for i := 0; i < fillWidth; i++ {
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
