package profiling

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test: DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.CPUProfilePath != "" {
		t.Errorf("expected empty CPUProfilePath, got %q", cfg.CPUProfilePath)
	}
	if cfg.MemProfilePath != "" {
		t.Errorf("expected empty MemProfilePath, got %q", cfg.MemProfilePath)
	}
	if cfg.BlockProfileRate != 0 {
		t.Errorf("expected BlockProfileRate 0, got %d", cfg.BlockProfileRate)
	}
	if cfg.MutexProfileFraction != 0 {
		t.Errorf("expected MutexProfileFraction 0, got %d", cfg.MutexProfileFraction)
	}
	if cfg.EnablePprof {
		t.Error("expected EnablePprof false")
	}
	if cfg.PprofAddr != "localhost:6060" {
		t.Errorf("expected PprofAddr localhost:6060, got %q", cfg.PprofAddr)
	}
}

// ---------------------------------------------------------------------------
// Test: StartCPUProfile / stop
// ---------------------------------------------------------------------------

func TestStartCPUProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cpu.prof")

	stop, err := StartCPUProfile(path)
	if err != nil {
		t.Fatalf("StartCPUProfile: %v", err)
	}

	// Do some work so the profile is non-empty.
	for i := 0; i < 100000; i++ {
		_ = i * i
	}

	stop()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cpu profile file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("cpu profile file is empty")
	}
}

func TestStartCPUProfile_InvalidPath(t *testing.T) {
	// A path that cannot be created.
	_, err := StartCPUProfile(filepath.Join(t.TempDir(), "no", "such", "dir", "cpu.prof"))
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestStartCPUProfile_StopIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cpu.prof")

	stop, err := StartCPUProfile(path)
	if err != nil {
		t.Fatalf("StartCPUProfile: %v", err)
	}

	// Calling stop multiple times must not panic.
	stop()
	stop()
}

// ---------------------------------------------------------------------------
// Test: WriteMemProfile
// ---------------------------------------------------------------------------

func TestWriteMemProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.prof")

	if err := WriteMemProfile(path); err != nil {
		t.Fatalf("WriteMemProfile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("mem profile file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("mem profile file is empty")
	}
}

func TestWriteMemProfile_InvalidPath(t *testing.T) {
	err := WriteMemProfile(filepath.Join(t.TempDir(), "no", "such", "dir", "mem.prof"))
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// ---------------------------------------------------------------------------
// Test: WriteAllocProfile
// ---------------------------------------------------------------------------

func TestWriteAllocProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allocs.prof")

	if err := WriteAllocProfile(path); err != nil {
		t.Fatalf("WriteAllocProfile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("alloc profile file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("alloc profile file is empty")
	}
}

// ---------------------------------------------------------------------------
// Test: EnableBlockProfile / EnableMutexProfile
// ---------------------------------------------------------------------------

func TestEnableBlockProfile(t *testing.T) {
	// Ensure no panic for various rates.
	EnableBlockProfile(0)
	EnableBlockProfile(1)
	EnableBlockProfile(1000)

	// Reset to 0 so as not to affect other tests.
	EnableBlockProfile(0)
}

func TestEnableMutexProfile(t *testing.T) {
	EnableMutexProfile(0)
	EnableMutexProfile(1)
	EnableMutexProfile(5)

	// Reset.
	EnableMutexProfile(0)
}

// ---------------------------------------------------------------------------
// Test: RegisterPprofHandlers
// ---------------------------------------------------------------------------

func TestRegisterPprofHandlers(t *testing.T) {
	mux := http.NewServeMux()
	RegisterPprofHandlers(mux)

	// Verify a subset of the expected endpoints respond.
	paths := []string{
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/allocs",
		"/debug/pprof/block",
		"/debug/pprof/mutex",
		"/debug/pprof/cmdline",
		"/debug/pprof/symbol",
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, p := range paths {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Errorf("GET %s: %v", p, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", p, resp.StatusCode)
		}
	}
}

func TestRegisterPprofHandlers_Index(t *testing.T) {
	mux := http.NewServeMux()
	RegisterPprofHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("pprof index: status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "heap") {
		t.Error("pprof index page does not mention 'heap'")
	}
}

// ---------------------------------------------------------------------------
// Test: MeasureStartupTime
// ---------------------------------------------------------------------------

func TestMeasureStartupTime(t *testing.T) {
	elapsed := MeasureStartupTime()

	if elapsed <= 0 {
		t.Errorf("expected positive startup time, got %v", elapsed)
	}
	// It should be at least a microsecond since init ran before this test.
	if elapsed < time.Microsecond {
		t.Errorf("startup time suspiciously small: %v", elapsed)
	}
}

func TestGetProcessStartTime(t *testing.T) {
	start := GetProcessStartTime()
	if start.IsZero() {
		t.Error("process start time is zero")
	}
	if start.After(time.Now()) {
		t.Error("process start time is in the future")
	}
}

// ---------------------------------------------------------------------------
// Test: ReportMemStats
// ---------------------------------------------------------------------------

func TestReportMemStats(t *testing.T) {
	stats := ReportMemStats()

	requiredKeys := []string{
		"heap_alloc_bytes",
		"heap_sys_bytes",
		"heap_idle_bytes",
		"heap_inuse_bytes",
		"heap_released_bytes",
		"heap_objects",
		"total_alloc_bytes",
		"mallocs",
		"frees",
		"sys_bytes",
		"stack_inuse",
		"stack_sys",
		"num_gc",
		"num_forced_gc",
		"gc_cpu_fraction",
		"last_gc_unix_nano",
		"pause_total_ns",
		"goroutines",
	}

	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("missing key %q in ReportMemStats", key)
		}
	}

	// Sanity: HeapAlloc and Sys should be non-zero in any Go program.
	if stats["heap_alloc_bytes"].(uint64) == 0 {
		t.Error("heap_alloc_bytes is 0")
	}
	if stats["sys_bytes"].(uint64) == 0 {
		t.Error("sys_bytes is 0")
	}

	// goroutines must be at least 1 (the test goroutine itself).
	g := stats["goroutines"].(int)
	if g < 1 {
		t.Errorf("goroutines = %d, want >= 1", g)
	}
}

func TestReportMemStats_GoroutineCount(t *testing.T) {
	before := ReportMemStats()["goroutines"].(int)

	done := make(chan struct{})
	go func() {
		<-done
	}()

	// Give scheduler a moment.
	runtime.Gosched()

	after := ReportMemStats()["goroutines"].(int)
	close(done)

	if after < before {
		t.Errorf("goroutine count decreased after spawning: before=%d after=%d", before, after)
	}
}

// ---------------------------------------------------------------------------
// Test: Apply
// ---------------------------------------------------------------------------

func TestApply_EmptyConfig(t *testing.T) {
	cfg := ProfileConfig{}
	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply empty config: %v", err)
	}
	cleanup()
}

func TestApply_WithCPUAndMemProfile(t *testing.T) {
	dir := t.TempDir()
	cfg := ProfileConfig{
		CPUProfilePath: filepath.Join(dir, "cpu.prof"),
		MemProfilePath: filepath.Join(dir, "mem.prof"),
	}

	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Do some work.
	for i := 0; i < 10000; i++ {
		_ = make([]byte, 1024)
	}

	cleanup()

	// Both files should exist and be non-empty.
	for _, path := range []string{cfg.CPUProfilePath, cfg.MemProfilePath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("profile file %s not created: %v", path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("profile file %s is empty", path)
		}
	}
}

func TestApply_WithBlockAndMutex(t *testing.T) {
	cfg := ProfileConfig{
		BlockProfileRate:     1,
		MutexProfileFraction: 1,
	}
	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	cleanup()

	// Reset so we don't leak state to other tests.
	runtime.SetBlockProfileRate(0)
	runtime.SetMutexProfileFraction(0)
}

func TestApply_InvalidCPUPath(t *testing.T) {
	cfg := ProfileConfig{
		CPUProfilePath: filepath.Join(t.TempDir(), "no", "such", "dir", "cpu.prof"),
	}
	_, err := Apply(cfg)
	if err == nil {
		t.Fatal("expected error for invalid CPU profile path")
	}
}
