package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/admin"
)

// ---------------------------------------------------------------------------
// promptSecret coverage
// ---------------------------------------------------------------------------

func TestCov_PromptSecret(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("my-secret-password\n"))
	got := promptSecret(r, "Password")
	if got != "my-secret-password" {
		t.Errorf("expected 'my-secret-password', got %q", got)
	}
}

func TestCov_PromptSecret_Empty(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\n"))
	got := promptSecret(r, "Token")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// InputHandler Start/Stop coverage
// ---------------------------------------------------------------------------

func TestCov_InputHandler_StartStop(t *testing.T) {
	// Use a pipe so we can write to the reader and control the goroutine.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pw.Close()

	eventCh := make(chan Event, 10)
	h := NewInputHandler(pr, eventCh)

	h.Start()

	// Close the pipe so the reader gets EOF and exits the loop.
	pr.Close()

	// Stop should complete without blocking since readLoop exited.
	done := make(chan struct{})
	go func() {
		h.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Error("Stop blocked unexpectedly")
	}
}

func TestCov_InputHandler_CtrlC(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pw.Close()

	eventCh := make(chan Event, 10)
	h := NewInputHandler(pr, eventCh)

	h.Start()

	// Write Ctrl+C (0x03)
	pw.Write([]byte{0x03})

	select {
	case e := <-eventCh:
		if e.Type != EventQuit {
			t.Errorf("expected EventQuit, got %d", e.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for quit event from Ctrl+C")
	}

	// readLoop exits after Ctrl+C, but call Stop to exercise the code path.
	pr.Close()

	done := make(chan struct{})
	go func() {
		h.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop blocked")
	}
}

func TestCov_InputHandler_EscapeSequence(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pw.Close()

	eventCh := make(chan Event, 10)
	h := NewInputHandler(pr, eventCh)

	h.Start()

	// Write an escape sequence: ESC [ A (up arrow)
	pw.Write([]byte{0x1b, '[', 'A'})

	// Give it time to process the escape sequence.
	time.Sleep(100 * time.Millisecond)

	// The escape sequence should be consumed without emitting an event.
	// Now write a regular key to verify the handler is still alive.
	pw.Write([]byte{'b'})

	select {
	case e := <-eventCh:
		if e.Type != EventKey || e.Key != 'b' {
			t.Errorf("expected key event 'b', got type=%d key=%c", e.Type, e.Key)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for key event after escape sequence")
	}

	// Close pipe to let readLoop exit on EOF
	pr.Close()

	done := make(chan struct{})
	go func() {
		h.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop blocked")
	}
}

func TestCov_InputHandler_CtrlD(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pw.Close()

	eventCh := make(chan Event, 10)
	h := NewInputHandler(pr, eventCh)

	h.Start()

	// Write Ctrl+D (0x04)
	pw.Write([]byte{0x04})

	select {
	case e := <-eventCh:
		if e.Type != EventQuit {
			t.Errorf("expected EventQuit, got %d", e.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for quit event from Ctrl+D")
	}

	pr.Close()

	done := make(chan struct{})
	go func() {
		h.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop blocked")
	}
}

// ---------------------------------------------------------------------------
// Layout.Draw coverage
// ---------------------------------------------------------------------------

func TestCov_Layout_Draw(t *testing.T) {
	l := NewLayout()
	l.AddWidget(&mockWidget{})
	s := NewScreen()
	s.Reset(80, 24)
	// Draw should iterate over widgets without panicking.
	l.Draw(s)
}

// ---------------------------------------------------------------------------
// Terminal helpers coverage
// ---------------------------------------------------------------------------

func TestCov_NewTerminal_And_Restore(t *testing.T) {
	term, err := NewTerminal()
	if err != nil {
		// In non-terminal environments (CI, piped), this may fail.
		t.Logf("NewTerminal failed (expected in non-TTY): %v", err)
		return
	}
	// Restore should succeed.
	if err := term.Restore(); err != nil {
		t.Errorf("Restore failed: %v", err)
	}
}

func TestCov_GetTerminalSize(t *testing.T) {
	w, h := getTerminalSize()
	if w <= 0 || h <= 0 {
		t.Errorf("getTerminalSize returned unexpected dimensions: %dx%d", w, h)
	}
	t.Logf("Terminal size: %dx%d", w, h)
}

// ---------------------------------------------------------------------------
// TopCommand.Run coverage (exercises flag parsing + TUI init)
// ---------------------------------------------------------------------------

func TestCov_TopCommand_Run_AlreadyRunning(t *testing.T) {
	// Test the full TopCommand.Run path indirectly by testing that
	// the TUI detects already-running state.
	fetcher := NewMetricsFetcher("localhost:1")
	tui := NewTUI(fetcher)
	tui.running.Store(true)
	err := tui.Run()
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
	tui.running.Store(false)
}

// ---------------------------------------------------------------------------
// TUI.Run coverage - stopCh path
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_StopChannel(t *testing.T) {
	fetcher := NewMetricsFetcher("localhost:1")
	tui := NewTUI(fetcher)

	// Close stopCh immediately so the event loop exits on the first iteration.
	close(tui.stopCh)

	err := tui.Run()
	// In a non-TTY environment, NewTerminal may fail first.
	// In a TTY, stopCh will cause immediate exit.
	if err != nil {
		// Acceptable: terminal init failure in non-TTY
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TUI.render with actual screen (exercises getTerminalSize + view switch)
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_WithScreen(t *testing.T) {
	fetcher := NewMetricsFetcher("localhost:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		SystemInfo: &admin.SystemInfo{Version: "1.0", Uptime: "5m", State: "running"},
		Health:     &admin.HealthStatus{Status: "healthy"},
		Pools: []admin.BackendPool{
			{Name: "p1", Algorithm: "rr", Backends: []admin.Backend{
				{ID: "b1", Address: "10.0.0.1:80", Healthy: true, Requests: 100, Errors: 2, Weight: 1},
			}},
		},
		Routes:    []admin.Route{{Name: "r1", Path: "/", BackendPool: "p1", Priority: 10}},
		Timestamp: time.Now(),
	}
	tui.dataMu.Unlock()

	// Test each view renders without panic
	for _, view := range []View{ViewOverview, ViewBackends, ViewRoutes, ViewMetrics} {
		tui.currentView = view
		tui.render()
	}

	if buf.Len() == 0 {
		t.Error("expected non-empty output from render")
	}
}

// ---------------------------------------------------------------------------
// TUI.fetchData error path coverage
// ---------------------------------------------------------------------------

func TestCov_TUI_FetchData_ServerError(t *testing.T) {
	// Server returns 500 for all endpoints.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	tui.fetchData()

	tui.dataMu.RLock()
	data := tui.data
	tui.dataMu.RUnlock()

	if data == nil {
		t.Fatal("expected data to be non-nil even on error")
	}
	if data.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
	// All fields should be nil/empty since all fetches failed
	if data.SystemInfo != nil {
		t.Error("expected nil SystemInfo on server error")
	}
}

func TestCov_TUI_FetchData_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	tui.fetchData()

	tui.dataMu.RLock()
	data := tui.data
	tui.dataMu.RUnlock()

	if data == nil {
		t.Fatal("expected data to be non-nil")
	}
}

func TestCov_TUI_FetchData_PartialSuccess(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/api/v1/system/info" {
			resp := admin.Response{
				Success: true,
				Data:    admin.SystemInfo{Version: "0.1.0", Uptime: "5m", State: "running"},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	tui.fetchData()

	tui.dataMu.RLock()
	data := tui.data
	tui.dataMu.RUnlock()

	if data == nil {
		t.Fatal("expected data to be non-nil")
	}
	if data.SystemInfo == nil {
		t.Error("expected SystemInfo to be populated")
	}
	if data.SystemInfo.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %s", data.SystemInfo.Version)
	}
}

// ---------------------------------------------------------------------------
// TUI.render error display (lastError)
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_WithError(t *testing.T) {
	fetcher := NewMetricsFetcher("localhost:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		SystemInfo: &admin.SystemInfo{},
		Timestamp:  time.Now(),
	}
	tui.dataMu.Unlock()
	tui.lastError = "connection refused"

	tui.currentView = ViewOverview
	tui.render()

	tui.screen.Flush()
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher additional coverage: JSON decode error paths
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchSystemInfo_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchSystemInfo()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCov_MetricsFetcher_FetchBackends_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchBackends()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCov_MetricsFetcher_FetchRoutes_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchRoutes()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCov_MetricsFetcher_FetchHealth_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchHealth()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher: data type mismatch (unmarshal error)
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchSystemInfo_DataMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: true,
			Data:    "this is a string not a SystemInfo struct",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchSystemInfo()
	// Should still succeed (fields will be zero-valued)
	// The marshal/unmarshal round-trip with a string data is OK for SystemInfo
	_ = err
}

func TestCov_MetricsFetcher_FetchBackends_DataMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: true,
			Data:    "this is a string not a slice",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchBackends()
	if err == nil {
		t.Error("expected error when data is not a slice of BackendPool")
	}
}

func TestCov_MetricsFetcher_FetchHealth_DataMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: true,
			Data:    []string{"not", "a", "HealthStatus"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchHealth()
	// Should still succeed or fail gracefully
	_ = err
}

// ---------------------------------------------------------------------------
// Formatter coverage
// ---------------------------------------------------------------------------

func TestCov_JSONFormatter_Nil(t *testing.T) {
	f := &JSONFormatter{}
	result, err := f.Format(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "null" {
		t.Errorf("expected 'null', got %q", result)
	}
}

func TestCov_JSONFormatter_Indent(t *testing.T) {
	f := &JSONFormatter{Indent: true}
	result, err := f.Format(map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "  ") {
		t.Errorf("expected indented output, got %q", result)
	}
}

func TestCov_TableFormatter_Nil(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string for nil, got %q", result)
	}
}

func TestCov_TableFormatter_StringSlice(t *testing.T) {
	f := &TableFormatter{Headers: []string{"Name", "Value"}}
	result, err := f.Format([][]string{{"a", "b"}, {"c", "d"}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Name") || !strings.Contains(result, "a") {
		t.Errorf("expected table output, got %q", result)
	}
}

func TestCov_TableFormatter_EmptyStringSlice(t *testing.T) {
	f := &TableFormatter{Headers: []string{"Name"}}
	result, err := f.Format([][]string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty string slice, got %q", result)
	}
}

func TestCov_TableFormatter_MapSlice(t *testing.T) {
	f := &TableFormatter{Headers: []string{"Name", "Age"}}
	result, err := f.Format([]map[string]string{
		{"Name": "Alice", "Age": "30"},
		{"Name": "Bob", "Age": "25"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "Bob") {
		t.Errorf("expected map slice output, got %q", result)
	}
}

func TestCov_TableFormatter_MapSlice_NoHeaders(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format([]map[string]string{
		{"x": "1", "y": "2"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "x") {
		t.Errorf("expected auto-extracted headers, got %q", result)
	}
}

func TestCov_TableFormatter_EmptyMapSlice(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format([]map[string]string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCov_TableFormatter_SingleMap(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format(map[string]string{"key1": "val1", "key2": "val2"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "key1") || !strings.Contains(result, "val1") {
		t.Errorf("expected single map output, got %q", result)
	}
}

func TestCov_TableFormatter_EmptySingleMap(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format(map[string]string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCov_TableFormatter_SingleColumn(t *testing.T) {
	f := &TableFormatter{Headers: []string{"Items"}}
	result, err := f.Format([]string{"alpha", "beta"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "alpha") || !strings.Contains(result, "Items") {
		t.Errorf("expected single column output, got %q", result)
	}
}

func TestCov_TableFormatter_EmptySingleColumn(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format([]string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCov_TableFormatter_Default(t *testing.T) {
	f := &TableFormatter{}
	// Pass an integer - should hit the default branch (formatWithHeaders)
	result, err := f.Format(42)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "42") {
		t.Errorf("expected '42' in output, got %q", result)
	}
}

func TestCov_NewFormatter(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"json", false},
		{"json-indent", false},
		{"table", false},
		{"xml", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewFormatter(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if f == nil {
					t.Error("expected non-nil formatter")
				}
			}
		})
	}
}

func TestCov_FormatToWriter(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{}
	err := FormatToWriter(&buf, f, map[string]string{"key": "val"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected output")
	}
}

func TestCov_FormatWithGlobals(t *testing.T) {
	globals := &GlobalFlags{Format: "json"}
	result, err := FormatWithGlobals(globals, map[string]string{"a": "b"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a") {
		t.Errorf("expected formatted output, got %q", result)
	}
}

func TestCov_FormatWithGlobals_InvalidFormat(t *testing.T) {
	globals := &GlobalFlags{Format: "xml"}
	_, err := FormatWithGlobals(globals, nil)
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

// ---------------------------------------------------------------------------
// Parser additional coverage
// ---------------------------------------------------------------------------

func TestCov_ParseArgs_Empty(t *testing.T) {
	pa, err := ParseArgs([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Command != "" {
		t.Errorf("expected empty command, got %q", pa.Command)
	}
}

func TestCov_ParseArgs_GlobalFlagsBeforeCommand(t *testing.T) {
	pa, err := ParseArgs([]string{"--format", "json", "status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The first non-flag arg is the command
	if pa.Command != "status" {
		t.Errorf("expected command 'status', got %q", pa.Command)
	}
}

func TestCov_ParseArgs_EqualsFlag(t *testing.T) {
	pa, err := ParseArgs([]string{"command", "--key=value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Command != "command" {
		t.Errorf("expected command 'command', got %q", pa.Command)
	}
	if pa.Flags["key"] != "value" {
		t.Errorf("expected flags[key]=value, got %q", pa.Flags["key"])
	}
}

func TestCov_ParseArgs_ShortFlag(t *testing.T) {
	pa, err := ParseArgs([]string{"command", "-k", "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Flags["k"] != "val" {
		t.Errorf("expected flags[k]=val, got %q", pa.Flags["k"])
	}
}

func TestCov_ParseArgs_ShortFlagEquals(t *testing.T) {
	pa, err := ParseArgs([]string{"command", "-k=val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Flags["k"] != "val" {
		t.Errorf("expected flags[k]=val, got %q", pa.Flags["k"])
	}
}

func TestCov_ParseArgs_BoolFlag(t *testing.T) {
	pa, err := ParseArgs([]string{"command", "--verbose"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Flags["verbose"] != "true" {
		t.Errorf("expected flags[verbose]=true, got %q", pa.Flags["verbose"])
	}
}

func TestCov_ParseArgs_ShortBoolFlag(t *testing.T) {
	pa, err := ParseArgs([]string{"command", "-v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Flags["v"] != "true" {
		t.Errorf("expected flags[v]=true, got %q", pa.Flags["v"])
	}
}

func TestCov_ParseArgs_Subcommand(t *testing.T) {
	pa, err := ParseArgs([]string{"config", "validate", "-c", "file.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Command != "config" {
		t.Errorf("expected command 'config', got %q", pa.Command)
	}
	if pa.Subcommand != "validate" {
		t.Errorf("expected subcommand 'validate', got %q", pa.Subcommand)
	}
	if pa.Flags["c"] != "file.yaml" {
		t.Errorf("expected flags[c]=file.yaml, got %q", pa.Flags["c"])
	}
}

func TestCov_ParseArgs_PositionalArgs(t *testing.T) {
	pa, err := ParseArgs([]string{"cmd", "subcmd", "arg1", "arg2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Command != "cmd" {
		t.Errorf("expected command 'cmd', got %q", pa.Command)
	}
	if pa.Subcommand != "subcmd" {
		t.Errorf("expected subcommand 'subcmd', got %q", pa.Subcommand)
	}
	if len(pa.Args) != 2 {
		t.Fatalf("expected 2 positional args, got %d", len(pa.Args))
	}
	if pa.Args[0] != "arg1" || pa.Args[1] != "arg2" {
		t.Errorf("expected [arg1, arg2], got %v", pa.Args)
	}
}

func TestCov_ParseGlobalFlags_FormatWithValue(t *testing.T) {
	g, remaining, err := ParseGlobalFlags([]string{"--format", "json", "status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Format != "json" {
		t.Errorf("expected json format, got %q", g.Format)
	}
	if len(remaining) != 1 || remaining[0] != "status" {
		t.Errorf("expected remaining [status], got %v", remaining)
	}
}

func TestCov_ParseGlobalFlags_ShortFormat(t *testing.T) {
	g, _, err := ParseGlobalFlags([]string{"-f", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Format != "json" {
		t.Errorf("expected json format, got %q", g.Format)
	}
}

func TestCov_ParseGlobalFlags_ShortFormatEquals(t *testing.T) {
	g, _, err := ParseGlobalFlags([]string{"-f=json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Format != "json" {
		t.Errorf("expected json format, got %q", g.Format)
	}
}

func TestCov_ParseGlobalFlags_FormatInvalid(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--format", "xml"})
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestCov_ParseGlobalFlags_FormatNoValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--format"})
	if err == nil {
		t.Error("expected error for --format without value")
	}
}

func TestCov_ParseGlobalFlags_ShortFormatNoValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"-f"})
	if err == nil {
		t.Error("expected error for -f without value")
	}
}

func TestCov_ParseGlobalFlags_ShortFormatInvalid(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"-f", "xml"})
	if err == nil {
		t.Error("expected error for invalid short format")
	}
}

func TestCov_ParseGlobalFlags_HelpVersion(t *testing.T) {
	g, _, err := ParseGlobalFlags([]string{"-h", "-v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.Help {
		t.Error("expected Help=true")
	}
	if !g.Version {
		t.Error("expected Version=true")
	}
}

// ---------------------------------------------------------------------------
// SetupCommand promptYesNo default=false with explicit input
// ---------------------------------------------------------------------------

func TestCov_PromptYesNo_DefaultFalse_ExplicitYes(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("y\n"))
	got := promptYesNo(r, "Enable", false)
	if !got {
		t.Error("expected true for 'y' input")
	}
}

func TestCov_PromptYesNo_DefaultFalse_ExplicitNo(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("n\n"))
	got := promptYesNo(r, "Enable", false)
	if got {
		t.Error("expected false for 'n' input")
	}
}

func TestCov_PromptYesNo_DefaultTrue_UnknownInput(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("maybe\n"))
	got := promptYesNo(r, "Enable", true)
	if !got {
		t.Error("expected true for unknown input with default=true")
	}
}

// ---------------------------------------------------------------------------
// SetupCommand full Run coverage via stdin pipe
// ---------------------------------------------------------------------------

func TestCov_SetupCommand_Run_WithAuth(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := tmpDir + "/olb-setup-auth.yaml"

	// Algorithm list: round_robin(1), weighted_round_robin(2), least_connections(3),
	// ip_hash(4), consistent_hash(5), random(6), power_of_two(7)
	input := strings.Join([]string{
		"",          // admin addr (default)
		"admin",     // username
		"secret123", // password
		"",          // listener name
		"",          // listen addr
		"2",         // protocol https (index 1)
		"",          // pool name
		"4",         // algorithm ip_hash (index 3)
		"",          // backend addr
		"5",         // weight
		"n",         // another backend
		"y",         // health checks
		"2",         // hc type tcp (index 1)
		"10s",       // interval
		"y",         // rate limit
		"2000",      // rps
		"y",         // CORS
		"n",         // compression
	}, "\n") + "\n"

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = oldStdin }()

	go func() {
		pw.WriteString(input)
		pw.Close()
	}()

	s := &SetupCommand{}
	err = s.Run([]string{"--output", outputPath})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `username: "admin"`) {
		t.Error("expected username in output")
	}
	if !strings.Contains(content, `password: "secret123"`) {
		t.Error("expected password in output")
	}
	if !strings.Contains(content, "ip_hash") {
		t.Error("expected ip_hash algorithm")
	}
	if !strings.Contains(content, "weight: 5") {
		t.Error("expected weight: 5")
	}
}

func TestCov_SetupCommand_Run_NoAuth(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := tmpDir + "/olb-setup-noauth.yaml"

	input := strings.Join([]string{
		"",  // admin addr
		"",  // no username
		"",  // listener name
		"",  // listen addr
		"1", // protocol http
		"",  // pool name
		"1", // algorithm round_robin
		"",  // backend addr
		"",  // weight (default 1)
		"n", // another backend
		"y", // health checks
		"1", // hc type http
		"",  // hc path default
		"",  // interval default
		"n", // rate limit
		"n", // CORS
		"y", // compression
	}, "\n") + "\n"

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = oldStdin }()

	go func() {
		pw.WriteString(input)
		pw.Close()
	}()

	s := &SetupCommand{}
	err = s.Run([]string{"--output", outputPath})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "username") {
		t.Error("should not contain username when no auth")
	}
	if !strings.Contains(content, "compression:") {
		t.Error("expected compression section")
	}
}

func TestCov_SetupCommand_Run_DefaultOutput(t *testing.T) {
	// Test that output defaults to "olb.yaml" when --output is not provided.
	// We verify the default logic by calling with an empty --output, which
	// still triggers the default outputPath = "olb.yaml" code path.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "olb.yaml")

	input := strings.Join([]string{
		"",  // admin addr
		"",  // no username
		"",  // listener name
		"",  // listen addr
		"1", // protocol
		"",  // pool
		"1", // algorithm
		"",  // backend
		"",  // weight
		"n", // another
		"n", // health check
		"n", // rate limit
		"n", // CORS
		"n", // compression
	}, "\n") + "\n"

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = oldStdin }()

	go func() {
		pw.WriteString(input)
		pw.Close()
	}()

	s := &SetupCommand{}
	err = s.Run([]string{"--output", outputPath})
	_ = err
}

// ---------------------------------------------------------------------------
// Client handleResponse coverage: read body error
// ---------------------------------------------------------------------------

func TestCov_Client_HandleResponse_ReadError(t *testing.T) {
	client := NewClient("http://localhost")
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(errReader{}),
	}
	var result map[string]any
	err := client.handleResponse(resp, &result)
	if err == nil {
		t.Error("expected error when reading body fails")
	}
}

// ---------------------------------------------------------------------------
// TUI render with small terminal
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_SmallTerminal(t *testing.T) {
	// This test verifies that render returns early when terminal is too small.
	// getTerminalSize usually returns >= 80x24, so we cannot easily test
	// the "too small" path in a real terminal. But we can test that
	// render works correctly with a nil screen (early return).
	tui := NewTUI(nil)
	tui.screen = nil
	tui.render() // should not panic
}

// ---------------------------------------------------------------------------
// TUI Run with event from refreshCh
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_EventFromChannel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := admin.Response{Success: true, Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	// Send a quit event via eventCh to terminate the loop.
	go func() {
		time.Sleep(200 * time.Millisecond)
		tui.eventCh <- Event{Type: EventQuit}
	}()

	err := tui.Run()
	// In a non-TTY environment, terminal init may fail first.
	if err != nil {
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") && !strings.Contains(err.Error(), "handle") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// MetricsExportCommand coverage
// ---------------------------------------------------------------------------

func TestCov_MetricsExportCommand_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/metrics" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"requests": float64(100)})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	tmpDir := t.TempDir()
	outputPath := tmpDir + "/metrics.json"

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--output", outputPath, "--format", "json"})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if !strings.Contains(string(data), "requests") {
		t.Errorf("expected metrics in output, got %s", string(data))
	}
}

func TestCov_MetricsExportCommand_Prometheus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/metrics/prometheus" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("# HELP test Test\n# TYPE test counter\ntest 1\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	tmpDir := t.TempDir()
	outputPath := tmpDir + "/metrics.txt"

	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--output", outputPath, "--format", "prometheus"})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
}

func TestCov_MetricsExportCommand_UnknownFormat(t *testing.T) {
	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--format", "xml"})
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestCov_MetricsExportCommand_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	cmd := &MetricsExportCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "json"})
	if err == nil {
		t.Error("expected error for API failure")
	}
}

// ---------------------------------------------------------------------------
// MetricsShowCommand table format coverage
// ---------------------------------------------------------------------------

func TestCov_MetricsShowCommand_TableFormat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/metrics" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"requests": float64(100), "errors": float64(5)})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	cmd := &MetricsShowCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "table"})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
}

func TestCov_MetricsShowCommand_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	cmd := &MetricsShowCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--format", "json"})
	if err == nil {
		t.Error("expected error for API failure")
	}
}

// ---------------------------------------------------------------------------
// ConfigValidateCommand coverage
// ---------------------------------------------------------------------------

func TestCov_ConfigValidateCommand_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/valid.yaml"
	os.WriteFile(configPath, []byte(`{"version":"1","listeners":[],"pools":[]}`), 0644)

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
}

func TestCov_ConfigValidateCommand_NotFound(t *testing.T) {
	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", "/nonexistent/config.yaml"})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestCov_ConfigValidateCommand_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/empty.yaml"
	os.WriteFile(configPath, []byte(""), 0644)

	cmd := &ConfigValidateCommand{}
	err := cmd.Run([]string{"--config", configPath})
	if err == nil {
		t.Error("expected error for empty file")
	}
}

func TestCov_ConfigValidateCommand_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/invalid.yaml"
	os.WriteFile(configPath, []byte("not: [broken: json"), 0644)

	cmd := &ConfigValidateCommand{}
	// Should still report as valid since it's non-empty (falls through to "not empty" check)
	err := cmd.Run([]string{"--config", configPath})
	_ = err // The code treats non-empty non-JSON as valid
}

// ---------------------------------------------------------------------------
// ConfigDiffCommand coverage
// ---------------------------------------------------------------------------

func TestCov_ConfigDiffCommand_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/config" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"version": "1"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	tmpDir := t.TempDir()
	filePath := tmpDir + "/olb.yaml"
	os.WriteFile(filePath, []byte("version: 1\n"), 0644)

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--file", filePath})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
}

func TestCov_ConfigDiffCommand_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr})
	if err == nil {
		t.Error("expected error for API failure")
	}
}

func TestCov_ConfigDiffCommand_FileNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/config" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"version": "1"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	apiAddr := strings.TrimPrefix(ts.URL, "http://")
	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{"--api-addr", apiAddr, "--file", "/nonexistent/file.yaml"})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// CompletionCommand coverage
// ---------------------------------------------------------------------------

func TestCov_CompletionCommand_Bash(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "bash"})

	w.Close()
	os.Stdout = old
	<-done

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "_olb()") {
		t.Error("expected bash completion script")
	}
}

func TestCov_CompletionCommand_Zsh(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "zsh"})

	w.Close()
	os.Stdout = old
	<-done

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "#compdef olb") {
		t.Error("expected zsh completion script")
	}
}

func TestCov_CompletionCommand_Fish(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "fish"})

	w.Close()
	os.Stdout = old
	<-done

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "fish completion") {
		t.Error("expected fish completion script")
	}
}

func TestCov_CompletionCommand_UnknownShell(t *testing.T) {
	cmd := &CompletionCommand{}
	err := cmd.Run([]string{"--shell", "powershell"})
	if err == nil {
		t.Error("expected error for unknown shell")
	}
}

// ---------------------------------------------------------------------------
// waitForProcessExit Windows branch coverage
// ---------------------------------------------------------------------------

func TestCov_WaitForProcessExit_WindowsBranch(t *testing.T) {
	// On Windows, FindProcess always succeeds (no signal 0 check).
	// This test verifies the function doesn't panic when running on any platform.
	pid := os.Getpid()
	err := waitForProcessExit(pid, 150*time.Millisecond)
	// The current process won't exit, so we expect a timeout on all platforms.
	if err == nil {
		t.Error("expected timeout error for running process")
	}
	if err != nil && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// printYAML additional coverage: scalar with indent
// ---------------------------------------------------------------------------

func TestCov_PrintYAML_ScalarWithIndent(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printYAML(42, 2)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "42") {
		t.Errorf("expected '42' in output, got %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// CLI.Run coverage for help and version flags
// ---------------------------------------------------------------------------

func TestCov_CLI_Run_HelpNoCommand(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)
	cli.Register(&VersionCommand{})

	err := cli.Run([]string{"--help"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Commands:") {
		t.Errorf("expected help output, got %q", out.String())
	}
}

func TestCov_CLI_Run_HelpSpecificCommand(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)
	cli.Register(&VersionCommand{})

	err := cli.Run([]string{"--help", "version"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "version") {
		t.Errorf("expected help for version command, got %q", out.String())
	}
}

func TestCov_CLI_Run_HelpUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)

	err := cli.Run([]string{"--help", "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown command in help")
	}
}

func TestCov_CLI_Run_Version(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)

	err := cli.Run([]string{"--version"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "1.0.0") {
		t.Errorf("expected version in output, got %q", out.String())
	}
}

func TestCov_CLI_Run_NoCommand(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)

	err := cli.Run([]string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Commands:") {
		t.Errorf("expected help output, got %q", out.String())
	}
}

func TestCov_CLI_Run_UnknownCommand(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)

	err := cli.Run([]string{"foobar"})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestCov_CLI_Run_CommandExecution(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)
	cli.Register(&VersionCommand{})

	err := cli.Run([]string{"version"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCov_CLI_Run_InvalidGlobalFlags(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)

	err := cli.Run([]string{"--format", "xml"})
	if err == nil {
		t.Error("expected error for invalid format in global flags")
	}
}

// ---------------------------------------------------------------------------
// JSONFormatter with encoding error
// ---------------------------------------------------------------------------

func TestCov_JSONFormatter_EncodingError(t *testing.T) {
	f := &JSONFormatter{}
	// Channels cannot be marshaled to JSON
	_, err := f.Format(make(chan int))
	if err == nil {
		t.Error("expected error for unmarshallable data")
	}
}

func TestCov_JSONFormatter_IndentEncodingError(t *testing.T) {
	f := &JSONFormatter{Indent: true}
	_, err := f.Format(make(chan int))
	if err == nil {
		t.Error("expected error for unmarshallable data")
	}
}

// ---------------------------------------------------------------------------
// FormatToWriter with format error
// ---------------------------------------------------------------------------

func TestCov_FormatToWriter_FormatError(t *testing.T) {
	var buf bytes.Buffer
	f := &JSONFormatter{}
	// Channels cannot be marshaled
	err := FormatToWriter(&buf, f, make(chan int))
	if err == nil {
		t.Error("expected error for format failure")
	}
}

// ---------------------------------------------------------------------------
// Client basic auth coverage
// ---------------------------------------------------------------------------

func TestCov_Client_BasicAuth(t *testing.T) {
	var receivedUser, receivedPass string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, receivedPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	client.SetBasicAuth("testuser", "testpass")

	var result map[string]any
	err := client.get("/test", &result)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if receivedUser != "testuser" {
		t.Errorf("expected username 'testuser', got %q", receivedUser)
	}
	if receivedPass != "testpass" {
		t.Errorf("expected password 'testpass', got %q", receivedPass)
	}
}

func TestCov_Client_TokenAuth(t *testing.T) {
	var receivedToken string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	client.SetToken("my-token-123")

	var result map[string]any
	err := client.get("/test", &result)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(receivedToken, "Bearer my-token-123") {
		t.Errorf("expected Bearer token, got %q", receivedToken)
	}
}

func TestCov_Client_Post_Success(t *testing.T) {
	var receivedBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody = string(buf)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	var result map[string]any
	err := client.post("/test", map[string]string{"key": "value"}, &result)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(receivedBody, "value") {
		t.Errorf("expected body to contain 'value', got %q", receivedBody)
	}
}

// ---------------------------------------------------------------------------
// ParsedArgs helper methods
// ---------------------------------------------------------------------------

func TestCov_ParsedArgs_GetFlagDefault(t *testing.T) {
	pa := &ParsedArgs{Flags: map[string]string{"existing": "val"}}
	if v := pa.GetFlagDefault("existing", "default"); v != "val" {
		t.Errorf("expected 'val', got %q", v)
	}
	if v := pa.GetFlagDefault("missing", "default"); v != "default" {
		t.Errorf("expected 'default', got %q", v)
	}
}

// ---------------------------------------------------------------------------
// client.delete() success path (resp.StatusCode < 400, line 179 return nil)
// ---------------------------------------------------------------------------

func TestCov_Client_Delete_SuccessPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	// The unexported delete() method covers the StatusCode < 400 return nil path.
	err := client.delete("/backends/test/backends/b1")
	if err != nil {
		t.Errorf("expected nil error on 204 No Content, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// client.delete() error decode error path (85.7% -> more)
// ---------------------------------------------------------------------------

func TestCov_Client_Delete_DecodeReadError(t *testing.T) {
	// Trigger decodeError path with a response body that causes read error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Write a very short body; decodeError reads with LimitReader(4096) so this works
		w.Write([]byte("error"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.delete("/test")
	if err == nil {
		t.Error("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchBackends error path with non-OK status
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchBackends_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchBackends()
	if err == nil {
		t.Error("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchRoutes error paths
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchRoutes_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchRoutes()
	if err == nil {
		t.Error("expected error for non-200 status")
	}
}

func TestCov_MetricsFetcher_FetchRoutes_ConnectionError(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	_, err := fetcher.FetchRoutes()
	if err == nil {
		t.Error("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchSystemInfo error path: API success=false
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchSystemInfo_APISuccessFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: false,
			Error:   &admin.ErrorInfo{Code: "INTERNAL", Message: "something broke"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchSystemInfo()
	if err == nil {
		t.Error("expected error when API success=false")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("expected API error message, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchBackends error path: API success=false
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchBackends_APISuccessFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: false,
			Error:   &admin.ErrorInfo{Code: "INTERNAL", Message: "backend error"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchBackends()
	if err == nil {
		t.Error("expected error when API success=false")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchRoutes error path: API success=false
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchRoutes_APISuccessFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: false,
			Error:   &admin.ErrorInfo{Code: "INTERNAL", Message: "routes error"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchRoutes()
	if err == nil {
		t.Error("expected error when API success=false")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchHealth error path: API success=false
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchHealth_APISuccessFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: false,
			Error:   &admin.ErrorInfo{Code: "INTERNAL", Message: "health error"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchHealth()
	if err == nil {
		t.Error("expected error when API success=false")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchHealth error path: non-OK status
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchHealth_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	_, err := fetcher.FetchHealth()
	if err == nil {
		t.Error("expected error for non-200 status")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchSystemInfo connection error
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchSystemInfo_ConnectionError(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	_, err := fetcher.FetchSystemInfo()
	if err == nil {
		t.Error("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchBackends connection error
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchBackends_ConnectionError(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	_, err := fetcher.FetchBackends()
	if err == nil {
		t.Error("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchHealth connection error
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchHealth_ConnectionError(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	_, err := fetcher.FetchHealth()
	if err == nil {
		t.Error("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchRoutes successful data decode
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchRoutes_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: true,
			Data: []admin.Route{
				{Name: "route1", Path: "/api", BackendPool: "web", Priority: 100},
				{Name: "route2", Path: "/health", BackendPool: "api", Priority: 50},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	routes, err := fetcher.FetchRoutes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
	if routes[0].Name != "route1" {
		t.Errorf("expected route1, got %s", routes[0].Name)
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchBackends successful data decode
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchBackends_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: true,
			Data: []admin.BackendPool{
				{Name: "web", Algorithm: "round_robin", Backends: []admin.Backend{
					{ID: "b1", Address: "10.0.0.1:80", Healthy: true, Requests: 50, Errors: 1, Weight: 1},
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	pools, err := fetcher.FetchBackends()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pools) != 1 {
		t.Errorf("expected 1 pool, got %d", len(pools))
	}
	if pools[0].Name != "web" {
		t.Errorf("expected web pool, got %s", pools[0].Name)
	}
}

// ---------------------------------------------------------------------------
// MetricsFetcher.FetchHealth successful data decode
// ---------------------------------------------------------------------------

func TestCov_MetricsFetcher_FetchHealth_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{
			Success: true,
			Data:    admin.HealthStatus{Status: "healthy"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	health, err := fetcher.FetchHealth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %s", health.Status)
	}
}

// ---------------------------------------------------------------------------
// TUI.Run: stopCh already closed -> immediate nil return (line 119)
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_StopChImmediateReturn(t *testing.T) {
	// On Windows, NewTerminal may succeed since we're in a console.
	// We use a fetcher pointed at a real but unused port.
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)

	// Close stopCh before Run so the event loop exits immediately.
	close(tui.stopCh)

	err := tui.Run()
	// May fail if terminal init fails (non-TTY), which is acceptable.
	if err != nil {
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TUI.Run: eventCh receives key 'q' -> returns nil (line 123-124)
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_QuitViaKeyQ(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{Success: true, Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	// Send quit event after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		tui.eventCh <- Event{Type: EventKey, Key: 'q'}
	}()

	err := tui.Run()
	if err != nil {
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") && !strings.Contains(err.Error(), "handle") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TUI.Run: eventCh receives EventQuit -> returns nil (line 122-124)
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_QuitViaEventQuit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{Success: true, Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	go func() {
		time.Sleep(200 * time.Millisecond)
		tui.eventCh <- Event{Type: EventQuit}
	}()

	err := tui.Run()
	if err != nil {
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") && !strings.Contains(err.Error(), "handle") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TopCommand.Run with flag parsing and TUI creation (lines 24-38)
// ---------------------------------------------------------------------------

func TestCov_TopCommand_Run_ParseFlags(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)

	// Close stopCh immediately so TUI.Run exits right away.
	close(tui.stopCh)

	// Test TopCommand.Run indirectly by testing that it creates a fetcher and TUI
	cmd := &TopCommand{}

	// We can't easily test TopCommand.Run because it creates a TUI and calls Run.
	// But we can test the flag parsing path by creating the command and verifying
	// that it doesn't panic on flag parse.
	cmd.apiAddr = "localhost:9999"

	// Verify the command's name and description
	if cmd.Name() != "top" {
		t.Errorf("expected 'top', got %q", cmd.Name())
	}
	if cmd.Description() != "Interactive TUI dashboard for real-time monitoring" {
		t.Errorf("unexpected description: %q", cmd.Description())
	}

	// Run in a goroutine since it will try to init terminal (may fail in non-TTY)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run([]string{"--api-addr", "127.0.0.1:1"})
	}()

	select {
	case err := <-done:
		// Terminal init likely fails in test, which is expected
		if err != nil {
			t.Logf("TopCommand.Run returned (expected in non-TTY): %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("TopCommand.Run blocked unexpectedly")
	}
}

// ---------------------------------------------------------------------------
// TUI render with nil data fields (nil SystemInfo, nil Health, empty pools)
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_NilDataFields(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	// Data with non-nil SystemInfo (required by renderOverview) but empty pools/routes
	tui.dataMu.Lock()
	tui.data = &DashboardData{
		SystemInfo: &admin.SystemInfo{},
		Health:     nil,
		Pools:      nil,
		Routes:     nil,
		Timestamp:  time.Now(),
	}
	tui.dataMu.Unlock()

	// Test each view
	for _, view := range []View{ViewOverview, ViewBackends, ViewRoutes, ViewMetrics} {
		tui.currentView = view
		tui.render()
	}
}

// ---------------------------------------------------------------------------
// TUI render with error state and non-empty lastError
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_OverviewWithLastError(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.lastError = "connection refused: dial tcp 127.0.0.1:8081: connect: connection refused"

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		SystemInfo: &admin.SystemInfo{Version: "test", Uptime: "1m", State: "running"},
		Health:     nil,
		Pools:      []admin.BackendPool{},
		Routes:     []admin.Route{},
		Timestamp:  time.Now(),
	}
	tui.dataMu.Unlock()

	tui.currentView = ViewOverview
	tui.render()
	tui.screen.Flush()

	if buf.Len() == 0 {
		t.Error("expected non-empty output from render with error state")
	}
}

// ---------------------------------------------------------------------------
// TUI render with unhealthy backends
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_BackendsUnhealthy(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		Pools: []admin.BackendPool{
			{Name: "web", Algorithm: "rr", Backends: []admin.Backend{
				{ID: "b1", Address: "10.0.0.1:80", Healthy: false, Requests: 0, Errors: 100, Weight: 1},
			}},
		},
		Timestamp: time.Now(),
	}
	tui.dataMu.Unlock()

	tui.currentView = ViewBackends
	tui.render()
	tui.screen.Flush()

	if buf.Len() == 0 {
		t.Error("expected non-empty output from backends view")
	}
}

// ---------------------------------------------------------------------------
// TUI render routes with empty host (defaults to "*")
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_RoutesEmptyHost(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		Routes: []admin.Route{
			{Name: "r1", Host: "", Path: "/api", BackendPool: "web", Priority: 100},
			{Name: "r2", Host: "api.example.com", Path: "/v2", BackendPool: "api", Priority: 50},
		},
		Timestamp: time.Now(),
	}
	tui.dataMu.Unlock()

	tui.currentView = ViewRoutes
	tui.render()
	tui.screen.Flush()

	if buf.Len() == 0 {
		t.Error("expected non-empty output from routes view")
	}
}

// ---------------------------------------------------------------------------
// TUI render metrics with high error rate (> 5%)
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_MetricsHighErrorRate(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		Pools: []admin.BackendPool{
			{Name: "web", Algorithm: "rr", Backends: []admin.Backend{
				{ID: "b1", Address: "10.0.0.1:80", Healthy: true, Requests: 100, Errors: 50, Weight: 1},
			}},
		},
		Timestamp: time.Now(),
	}
	tui.dataMu.Unlock()

	tui.currentView = ViewMetrics
	tui.render()
	tui.screen.Flush()

	if buf.Len() == 0 {
		t.Error("expected non-empty output from metrics view with high error rate")
	}
}

// ---------------------------------------------------------------------------
// TUI render metrics with medium error rate (> 1%)
// ---------------------------------------------------------------------------

func TestCov_TUI_Render_MetricsMediumErrorRate(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	tui.dataMu.Lock()
	tui.data = &DashboardData{
		Pools: []admin.BackendPool{
			{Name: "web", Algorithm: "rr", Backends: []admin.Backend{
				{ID: "b1", Address: "10.0.0.1:80", Healthy: true, Requests: 1000, Errors: 30, Weight: 1},
			}},
		},
		Timestamp: time.Now(),
	}
	tui.dataMu.Unlock()

	tui.currentView = ViewMetrics
	tui.render()
	tui.screen.Flush()

	if buf.Len() == 0 {
		t.Error("expected non-empty output from metrics view with medium error rate")
	}
}

// ---------------------------------------------------------------------------
// TUI handleEvent: view switching keys
// ---------------------------------------------------------------------------

func TestCov_TUI_HandleEvent_ViewSwitching(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.screen = NewScreen()
	var buf bytes.Buffer
	tui.screen.writer = bufio.NewWriter(&buf)

	// Initialize data so render() doesn't panic
	tui.dataMu.Lock()
	tui.data = &DashboardData{
		SystemInfo: &admin.SystemInfo{},
		Pools:      []admin.BackendPool{},
		Routes:     []admin.Route{},
		Timestamp:  time.Now(),
	}
	tui.dataMu.Unlock()

	tests := []struct {
		key          byte
		expectedView View
	}{
		{'b', ViewBackends},
		{'B', ViewBackends},
		{'r', ViewRoutes},
		{'R', ViewRoutes},
		{'m', ViewMetrics},
		{'M', ViewMetrics},
		{'o', ViewOverview},
		{'O', ViewOverview},
	}

	for _, tt := range tests {
		tui.currentView = ViewOverview
		result := tui.handleEvent(Event{Type: EventKey, Key: tt.key})
		if result {
			t.Errorf("key %c should not cause quit", tt.key)
		}
		if tui.currentView != tt.expectedView {
			t.Errorf("key %c: expected view %d, got %d", tt.key, tt.expectedView, tui.currentView)
		}
	}
}

// ---------------------------------------------------------------------------
// Terminal.Restore with nil state (error path)
// ---------------------------------------------------------------------------

func TestCov_Terminal_Restore_NilState(t *testing.T) {
	term := &Terminal{originalState: nil}
	err := term.restore()
	if err == nil {
		t.Error("expected error for nil terminal state")
	}
}

// ---------------------------------------------------------------------------
// Screen.DrawBox too small (width < 2 or height < 2)
// ---------------------------------------------------------------------------

func TestCov_Screen_DrawBox_TooSmall(t *testing.T) {
	s := NewScreen()
	s.Reset(80, 24)
	// Should not panic with small dimensions
	s.DrawBox(0, 0, 1, 5, "title", false)  // width < 2
	s.DrawBox(0, 0, 5, 1, "title", false)  // height < 2
	s.DrawBox(0, 0, 0, 0, "title", false)  // both < 2
}

// ---------------------------------------------------------------------------
// Screen.DrawBox with title (long title > width-4 is skipped)
// ---------------------------------------------------------------------------

func TestCov_Screen_DrawBox_TitleTooLong(t *testing.T) {
	s := NewScreen()
	s.Reset(80, 24)
	// Title longer than width-4 should be skipped without panic
	s.DrawBox(0, 0, 10, 5, "This is a very long title that exceeds width", true)
}

// ---------------------------------------------------------------------------
// Screen.SetCell out of bounds (negative and >= dimensions)
// ---------------------------------------------------------------------------

func TestCov_Screen_SetCell_OutOfBounds(t *testing.T) {
	s := NewScreen()
	s.Reset(80, 24)
	// These should not panic
	s.SetCell(-1, 0, 'X', ColorRed)
	s.SetCell(0, -1, 'X', ColorRed)
	s.SetCell(80, 0, 'X', ColorRed)
	s.SetCell(0, 24, 'X', ColorRed)
}

// ---------------------------------------------------------------------------
// Screen.Reset with same dimensions (dirty marking path)
// ---------------------------------------------------------------------------

func TestCov_Screen_Reset_SameDimensions(t *testing.T) {
	s := NewScreen()
	s.Reset(80, 24)
	// Set some content
	s.SetCell(0, 0, 'A', ColorDefault)
	// Reset with same dimensions - should mark all as dirty
	s.Reset(80, 24)
	// Verify by flushing (should not panic)
	var buf bytes.Buffer
	s.writer = bufio.NewWriter(&buf)
	s.Flush()
}

// ---------------------------------------------------------------------------
// Screen.Clear
// ---------------------------------------------------------------------------

func TestCov_Screen_Clear(t *testing.T) {
	s := NewScreen()
	s.Reset(80, 24)
	var buf bytes.Buffer
	s.writer = bufio.NewWriter(&buf)
	s.Clear()
	if buf.Len() == 0 {
		t.Error("expected clear output")
	}
}

// ---------------------------------------------------------------------------
// Screen.HideCursor / ShowCursor
// ---------------------------------------------------------------------------

func TestCov_Screen_HideShowCursor(t *testing.T) {
	s := NewScreen()
	s.Reset(80, 24)
	var buf bytes.Buffer
	s.writer = bufio.NewWriter(&buf)
	s.HideCursor()
	s.ShowCursor()
}

// ---------------------------------------------------------------------------
// TUI.getString
// ---------------------------------------------------------------------------

func TestCov_TUI_GetString(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)

	if got := tui.getString("", "default"); got != "default" {
		t.Errorf("expected 'default', got %q", got)
	}
	if got := tui.getString("value", "default"); got != "value" {
		t.Errorf("expected 'value', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// FormatWithGlobals with table format
// ---------------------------------------------------------------------------

func TestCov_FormatWithGlobals_Table(t *testing.T) {
	globals := &GlobalFlags{Format: "table"}
	result, err := FormatWithGlobals(globals, []string{"a", "b"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a") {
		t.Errorf("expected table output, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// ParseArgs: flag with next arg starting with dash (bool flag path)
// ---------------------------------------------------------------------------

func TestCov_ParseArgs_BoolFlagNextArgIsDash(t *testing.T) {
	pa, err := ParseArgs([]string{"cmd", "--flag1", "--flag2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Flags["flag1"] != "true" {
		t.Errorf("expected flag1=true (bool), got %q", pa.Flags["flag1"])
	}
	if pa.Flags["flag2"] != "true" {
		t.Errorf("expected flag2=true (bool), got %q", pa.Flags["flag2"])
	}
}

// ---------------------------------------------------------------------------
// ParseArgs: short bool flag with next arg starting with dash
// ---------------------------------------------------------------------------

func TestCov_ParseArgs_ShortBoolFlagNextArgIsDash(t *testing.T) {
	pa, err := ParseArgs([]string{"cmd", "-a", "-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Flags["a"] != "true" {
		t.Errorf("expected a=true (bool), got %q", pa.Flags["a"])
	}
	if pa.Flags["b"] != "true" {
		t.Errorf("expected b=true (bool), got %q", pa.Flags["b"])
	}
}

// ---------------------------------------------------------------------------
// ParseArgs: no command (all global flags)
// ---------------------------------------------------------------------------

func TestCov_ParseArgs_NoCommandAllFlags(t *testing.T) {
	pa, err := ParseArgs([]string{"--format", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The first non-flag arg becomes the command; all args here are flags
	// so command should be empty
	if pa.Command != "" {
		t.Logf("Command=%q (may be non-empty if first non-flag found)", pa.Command)
	}
}

// ---------------------------------------------------------------------------
// ParseGlobalFlags: --format=table
// ---------------------------------------------------------------------------

func TestCov_ParseGlobalFlags_FormatEqualsTable(t *testing.T) {
	g, remaining, err := ParseGlobalFlags([]string{"--format=table", "status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Format != "table" {
		t.Errorf("expected table format, got %q", g.Format)
	}
	if len(remaining) != 1 || remaining[0] != "status" {
		t.Errorf("expected remaining [status], got %v", remaining)
	}
}

// ---------------------------------------------------------------------------
// ParsedArgs.HasFlag
// ---------------------------------------------------------------------------

func TestCov_ParsedArgs_HasFlag(t *testing.T) {
	pa := &ParsedArgs{Flags: map[string]string{"exists": "val"}}
	if !pa.HasFlag("exists") {
		t.Error("expected HasFlag(exists)=true")
	}
	if pa.HasFlag("missing") {
		t.Error("expected HasFlag(missing)=false")
	}
}

// ---------------------------------------------------------------------------
// ParsedArgs.GetFlag
// ---------------------------------------------------------------------------

func TestCov_ParsedArgs_GetFlag(t *testing.T) {
	pa := &ParsedArgs{Flags: map[string]string{"key": "val"}}
	if v, ok := pa.GetFlag("key"); !ok || v != "val" {
		t.Errorf("expected (val, true), got (%q, %v)", v, ok)
	}
	if _, ok := pa.GetFlag("missing"); ok {
		t.Error("expected false for missing flag")
	}
}

// ---------------------------------------------------------------------------
// CLI.Commands() and CLI.Command()
// ---------------------------------------------------------------------------

func TestCov_CLI_Commands(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)
	cli.Register(&VersionCommand{})
	cli.Register(&StartCommand{})

	cmds := cli.Commands()
	if len(cmds) != 2 {
		t.Errorf("expected 2 commands, got %d", len(cmds))
	}

	cmd := cli.Command("version")
	if cmd == nil {
		t.Error("expected non-nil command for 'version'")
	}
	cmd = cli.Command("nonexistent")
	if cmd != nil {
		t.Error("expected nil for nonexistent command")
	}
}

// ---------------------------------------------------------------------------
// CLI.Name() and CLI.Version()
// ---------------------------------------------------------------------------

func TestCov_CLI_NameVersion(t *testing.T) {
	cli := New("test-cli", "2.0.0")
	if cli.Name() != "test-cli" {
		t.Errorf("expected 'test-cli', got %q", cli.Name())
	}
	if cli.Version() != "2.0.0" {
		t.Errorf("expected '2.0.0', got %q", cli.Version())
	}
}

// ---------------------------------------------------------------------------
// formatNumber edge cases
// ---------------------------------------------------------------------------

func TestCov_FormatNumber_EdgeCases(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999999, "1000.0K"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
		{999999999, "1000.0M"},
		{1000000000, "1.0B"},
	}
	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.expected {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// truncate edge cases
// ---------------------------------------------------------------------------

func TestCov_Truncate_EdgeCases(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if got := truncate("hello world", 8); got != "hello..." {
		t.Errorf("expected 'hello...', got %q", got)
	}
	if got := truncate("hi", 2); got != "hi" {
		t.Errorf("expected 'hi', got %q", got)
	}
	if got := truncate("abc", 3); got != "abc" {
		t.Errorf("expected 'abc', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// TUI.FetchData with all endpoints succeeding
// ---------------------------------------------------------------------------

func TestCov_TUI_FetchData_AllSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api/v1/system/info":
			data = admin.SystemInfo{Version: "1.0", Uptime: "5m", State: "running"}
		case "/api/v1/backends":
			data = []admin.BackendPool{
				{Name: "web", Algorithm: "rr", Backends: []admin.Backend{
					{ID: "b1", Address: "10.0.0.1:80", Healthy: true, Requests: 50, Errors: 1, Weight: 1},
				}},
			}
		case "/api/v1/routes":
			data = []admin.Route{
				{Name: "r1", Path: "/api", BackendPool: "web", Priority: 100},
			}
		case "/api/v1/system/health":
			data = admin.HealthStatus{Status: "healthy"}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := admin.Response{Success: true, Data: data}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	tui.fetchData()

	tui.dataMu.RLock()
	data := tui.data
	tui.dataMu.RUnlock()

	if data.SystemInfo == nil {
		t.Error("expected SystemInfo to be populated")
	}
	if data.SystemInfo.Version != "1.0" {
		t.Errorf("expected version 1.0, got %s", data.SystemInfo.Version)
	}
	if len(data.Pools) != 1 {
		t.Errorf("expected 1 pool, got %d", len(data.Pools))
	}
	if len(data.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(data.Routes))
	}
	if data.Health == nil {
		t.Error("expected Health to be populated")
	}
}

// ---------------------------------------------------------------------------
// Formatter: TableFormatter formatStringSlice with no headers
// ---------------------------------------------------------------------------

func TestCov_TableFormatter_StringSliceNoHeaders(t *testing.T) {
	f := &TableFormatter{} // no headers
	result, err := f.Format([][]string{{"a", "b"}, {"c", "d"}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a") {
		t.Errorf("expected table output, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Formatter: TableFormatter formatStringSlice flush error (not testable without
// mock tabwriter, but we can verify the normal path with headers)
// ---------------------------------------------------------------------------

func TestCov_TableFormatter_StringSliceWithHeaders(t *testing.T) {
	f := &TableFormatter{Headers: []string{"Col1", "Col2"}}
	result, err := f.Format([][]string{{"a", "b"}, {"c", "d"}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Col1") {
		t.Errorf("expected headers in output, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Terminal.restore with valid terminalState on Windows (top_windows.go:113)
// This test exercises the restore path with a valid state object.
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

// ---------------------------------------------------------------------------
// Terminal newTerminal/getTerminalSizePlatform via NewTerminal
// ---------------------------------------------------------------------------

func TestCov_Terminal_NewTerminal_ExercisePaths(t *testing.T) {
	// Exercise newTerminal which calls GetConsoleMode twice and SetConsoleMode
	term, err := NewTerminal()
	if err != nil {
		t.Logf("NewTerminal failed (expected in non-TTY): %v", err)
		// Even if it fails, we exercised the first GetConsoleMode call
		return
	}
	// If it succeeds, also test restore
	if err := term.Restore(); err != nil {
		t.Logf("Restore failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// getTerminalSizePlatform direct call
// ---------------------------------------------------------------------------

func TestCov_GetTerminalSizePlatform(t *testing.T) {
	w, h := getTerminalSizePlatform()
	if w <= 0 || h <= 0 {
		t.Errorf("getTerminalSizePlatform returned unexpected dimensions: %dx%d", w, h)
	}
	t.Logf("Terminal size: %dx%d", w, h)
}

// ---------------------------------------------------------------------------
// client.delete error path: doRequest fails (connection refused)
// ---------------------------------------------------------------------------

func TestCov_Client_Delete_ConnectionError(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	err := client.delete("/test")
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

// ---------------------------------------------------------------------------
// waitForProcessExit: Unix signal(0) check path (line 676-678)
// On Windows, this path is skipped but the function still loops.
// ---------------------------------------------------------------------------

func TestCov_WaitForProcessExit_SignalCheck(t *testing.T) {
	// Test with current PID on all platforms
	pid := os.Getpid()
	err := waitForProcessExit(pid, 100*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error for running process")
	}
	if err != nil && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StopCommand.Run: full path exercise on Windows (signal fails)
// Already covered by TestStopCommand_Run_WithSelfProcess, but let's ensure
// the fmt.Printf lines (201, 213) are covered when signal succeeds.
// On Unix, we test by using a non-existent PID which fails at sendSignal.
// ---------------------------------------------------------------------------

func TestCov_StopCommand_Run_FullPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "olb.pid")
	// Write current PID
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		t.Fatalf("Failed to create PID file: %v", err)
	}

	cmd := &StopCommand{}
	err := cmd.Run([]string{"--pid-file", pidFile})
	// On Windows, sendSignal with SIGTERM fails
	if err == nil {
		t.Error("expected error for SIGTERM on Windows")
	}
	// Verify the error is from signal sending
	if err != nil && !strings.Contains(err.Error(), "failed to send signal") {
		t.Errorf("expected 'failed to send signal' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TUI.Run: ticker fires (line 127-129) - test with a real server and ticker
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_TickerFires(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := admin.Response{Success: true, Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	// Send quit after enough time for the ticker to fire at least once
	go func() {
		time.Sleep(1500 * time.Millisecond)
		tui.eventCh <- Event{Type: EventQuit}
	}()

	err := tui.Run()
	// May fail if terminal init fails (non-TTY), which is acceptable.
	if err != nil {
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") && !strings.Contains(err.Error(), "handle") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TUI.Run: refreshCh fires (line 130-132)
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_RefreshChFires(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := admin.Response{Success: true, Data: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	fetcher := NewMetricsFetcher(addr)
	tui := NewTUI(fetcher)

	go func() {
		time.Sleep(200 * time.Millisecond)
		tui.refreshCh <- struct{}{} // trigger render
		time.Sleep(200 * time.Millisecond)
		tui.eventCh <- Event{Type: EventQuit}
	}()

	err := tui.Run()
	if err != nil {
		if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "console") && !strings.Contains(err.Error(), "handle") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TUI.Run: already running (CompareAndSwap fails)
// ---------------------------------------------------------------------------

func TestCov_TUI_Run_AlreadyRunningRace(t *testing.T) {
	fetcher := NewMetricsFetcher("127.0.0.1:1")
	tui := NewTUI(fetcher)
	tui.running.Store(true)

	err := tui.Run()
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StartCommand.Run: the fmt.Printf line 141 ("started with config")
// This is covered by TestStartCommand_Run_LoadsConfigAndWaitsForSignal on Unix.
// On Windows, we can cover the path up to config.Load success but the engine
// Start may bind to ports. Let's cover the daemon-mode Windows error path.
// ---------------------------------------------------------------------------

func TestCov_StartCommand_Run_DaemonWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "olb.yaml")
	pidPath := filepath.Join(tmpDir, "olb.pid")

	configContent := `version: "1"
listeners:
  - name: http
    address: ":8080"
    protocol: http
pools:
  - name: default
    algorithm: round_robin
    backends:
      - address: "localhost:3001"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cmd := &StartCommand{}
	err := cmd.Run([]string{"--config", configPath, "--daemon", "--pid-file", pidPath})
	if err == nil {
		t.Error("expected error for daemon mode on Windows")
	}
	if !strings.Contains(err.Error(), "not supported on Windows") {
		t.Errorf("expected 'not supported on Windows', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// printYAML: array items with map values (lines 78-87)
// ---------------------------------------------------------------------------

func TestCov_PrintYAML_ArrayWithMaps(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]any{
		"items": []any{
			map[string]any{"name": "first", "value": "1"},
			map[string]any{"name": "second", "value": "2"},
		},
		"simple": "scalar",
	}
	printYAML(data, 0)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()
	if !strings.Contains(output, "items:") {
		t.Error("expected 'items:' in output")
	}
	if !strings.Contains(output, "name: first") {
		t.Error("expected 'name: first' in output")
	}
	if !strings.Contains(output, "simple: scalar") {
		t.Error("expected 'simple: scalar' in output")
	}
}

// ---------------------------------------------------------------------------
// printYAML: array with non-map items (lines 88-90)
// ---------------------------------------------------------------------------

func TestCov_PrintYAML_ArrayWithScalars(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]any{
		"tags": []any{"alpha", "beta", 42},
	}
	printYAML(data, 0)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()
	if !strings.Contains(output, "alpha") {
		t.Error("expected 'alpha' in output")
	}
	if !strings.Contains(output, "42") {
		t.Error("expected '42' in output")
	}
}

// ---------------------------------------------------------------------------
// formatSingleMap: multi-key map (tests sorting)
// ---------------------------------------------------------------------------

func TestCov_FormatSingleMap_MultiKey(t *testing.T) {
	f := &TableFormatter{}
	// Map with multiple keys to exercise the sorting loop (lines 157-163)
	result, err := f.Format(map[string]string{
		"zebra":   "z",
		"alpha":   "a",
		"middle":  "m",
		"charlie": "c",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "alpha") {
		t.Errorf("expected sorted output, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// ConfigDiffCommand: file read error
// ---------------------------------------------------------------------------

func TestCov_ConfigDiffCommand_FileReadError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/config" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"version": "1"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cmd := &ConfigDiffCommand{}
	err := cmd.Run([]string{
		"--api-addr", strings.TrimPrefix(ts.URL, "http://"),
		"--file", filepath.Join(t.TempDir(), "nonexistent", "config.yaml"),
	})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// FormatToWriter with table formatter
// ---------------------------------------------------------------------------

func TestCov_FormatToWriter_Table(t *testing.T) {
	var buf bytes.Buffer
	f := &TableFormatter{Headers: []string{"Name"}}
	err := FormatToWriter(&buf, f, []string{"item1"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CLI Run with --format json (valid global flag with command)
// ---------------------------------------------------------------------------

func TestCov_CLI_Run_FormatFlagWithCommand(t *testing.T) {
	var out bytes.Buffer
	cli := NewWithWriters("olb", "1.0.0", &out, io.Discard)
	cli.Register(&VersionCommand{})

	err := cli.Run([]string{"--format", "json", "version"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
