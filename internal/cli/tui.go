// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"fmt"
	"github.com/openloadbalancer/olb/internal/admin"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

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
func (t *TUI) renderMetrics(data *DashboardData, width, _ int) {
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
	filled := min(int(errorRate/100*float64(gaugeWidth)), gaugeWidth)
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
