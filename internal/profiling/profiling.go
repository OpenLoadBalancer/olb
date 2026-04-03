// Package profiling provides CPU/memory profiling utilities, pprof handler
// registration, startup-time measurement, and runtime memory statistics for
// OpenLoadBalancer.  All functionality uses the Go standard library only
// (runtime, runtime/pprof, net/http/pprof).
package profiling

import (
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	runtimepprof "runtime/pprof"
	"sync"
	"time"
)

// processStartTime is recorded once at package init so that
// MeasureStartupTime can compute elapsed wall-clock time from process start.
var processStartTime time.Time

func init() {
	processStartTime = time.Now()
}

// ---------------------------------------------------------------------------
// ProfileConfig
// ---------------------------------------------------------------------------

// ProfileConfig holds all knobs for profiling.
type ProfileConfig struct {
	// CPUProfilePath is the file path to write the CPU profile to.
	// Empty means CPU profiling is disabled.
	CPUProfilePath string

	// MemProfilePath is the file path to write the heap profile to.
	// Empty means memory profiling on shutdown is disabled.
	MemProfilePath string

	// BlockProfileRate is passed to runtime.SetBlockProfileRate.
	// 0 disables block profiling; 1 captures every event.
	BlockProfileRate int

	// MutexProfileFraction is passed to runtime.SetMutexProfileFraction.
	// 0 disables mutex profiling.
	MutexProfileFraction int

	// EnablePprof enables net/http/pprof handlers on PprofAddr.
	EnablePprof bool

	// PprofAddr is the listen address for the pprof HTTP server
	// (e.g. "localhost:6060"). Only used when EnablePprof is true.
	PprofAddr string
}

// DefaultConfig returns a ProfileConfig with sensible defaults.
// Profiling is disabled by default; only the pprof address is pre-populated.
func DefaultConfig() ProfileConfig {
	return ProfileConfig{
		PprofAddr: "localhost:6060",
	}
}

// ---------------------------------------------------------------------------
// CPU profiling
// ---------------------------------------------------------------------------

// StartCPUProfile begins CPU profiling and writes the profile data to the
// given path.  Call the returned stop function to flush and close the file.
func StartCPUProfile(path string) (stop func(), err error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("profiling: create cpu profile %s: %w", path, err)
	}

	if err := runtimepprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("profiling: start cpu profile: %w", err)
	}

	var once sync.Once
	stop = func() {
		once.Do(func() {
			runtimepprof.StopCPUProfile()
			f.Close()
		})
	}
	return stop, nil
}

// ---------------------------------------------------------------------------
// Memory / allocation profiling
// ---------------------------------------------------------------------------

// WriteMemProfile writes a heap profile snapshot to the given path.
func WriteMemProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("profiling: create mem profile %s: %w", path, err)
	}
	defer f.Close()

	// Ensure up-to-date statistics.
	runtime.GC()

	if err := runtimepprof.Lookup("heap").WriteTo(f, 0); err != nil {
		return fmt.Errorf("profiling: write heap profile: %w", err)
	}
	return nil
}

// WriteAllocProfile writes an allocs profile snapshot to the given path.
// Unlike the heap profile, the allocs profile records total allocations since
// program start, not just live objects.
func WriteAllocProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("profiling: create alloc profile %s: %w", path, err)
	}
	defer f.Close()

	runtime.GC()

	if err := runtimepprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		return fmt.Errorf("profiling: write alloc profile: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Block & mutex profiling
// ---------------------------------------------------------------------------

// EnableBlockProfile sets the block profile rate.  A rate of 1 captures every
// blocking event; 0 disables block profiling.
func EnableBlockProfile(rate int) {
	runtime.SetBlockProfileRate(rate)
}

// EnableMutexProfile sets the mutex profile fraction.  A fraction of 1
// captures every mutex contention event; 0 disables mutex profiling.
func EnableMutexProfile(fraction int) {
	runtime.SetMutexProfileFraction(fraction)
}

// ---------------------------------------------------------------------------
// pprof HTTP handlers
// ---------------------------------------------------------------------------

// RegisterPprofHandlers registers the standard net/http/pprof endpoints on
// the given ServeMux:
//
//	/debug/pprof/           - index page
//	/debug/pprof/profile    - CPU profile (duration via ?seconds=N)
//	/debug/pprof/heap       - heap profile
//	/debug/pprof/goroutine  - goroutine dump
//	/debug/pprof/allocs     - allocation profile
//	/debug/pprof/block      - block profile
//	/debug/pprof/mutex      - mutex profile
//	/debug/pprof/cmdline    - command-line invocation
//	/debug/pprof/symbol     - symbol lookup
//	/debug/pprof/trace      - execution trace
func RegisterPprofHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Named profiles served via Index already, but we register explicit
	// handlers so that "/debug/pprof/heap" (without trailing slash) works
	// directly and does not 404.
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
}

// ---------------------------------------------------------------------------
// Startup time measurement
// ---------------------------------------------------------------------------

// MeasureStartupTime returns the wall-clock duration elapsed since the
// process started (i.e. since this package's init function ran).
func MeasureStartupTime() time.Duration {
	return time.Since(processStartTime)
}

// GetProcessStartTime returns the timestamp recorded at package init.
// This can be used to compute custom deltas.
func GetProcessStartTime() time.Time {
	return processStartTime
}

// ---------------------------------------------------------------------------
// Memory statistics
// ---------------------------------------------------------------------------

// ReportMemStats returns a snapshot of selected runtime memory statistics as
// a string-keyed map suitable for JSON serialization.
func ReportMemStats() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]any{
		// Heap
		"heap_alloc_bytes":    m.HeapAlloc,
		"heap_sys_bytes":      m.HeapSys,
		"heap_idle_bytes":     m.HeapIdle,
		"heap_inuse_bytes":    m.HeapInuse,
		"heap_released_bytes": m.HeapReleased,
		"heap_objects":        m.HeapObjects,

		// Cumulative allocation
		"total_alloc_bytes": m.TotalAlloc,
		"mallocs":           m.Mallocs,
		"frees":             m.Frees,

		// System
		"sys_bytes":   m.Sys,
		"stack_inuse": m.StackInuse,
		"stack_sys":   m.StackSys,

		// GC
		"num_gc":            m.NumGC,
		"num_forced_gc":     m.NumForcedGC,
		"gc_cpu_fraction":   m.GCCPUFraction,
		"last_gc_unix_nano": m.LastGC,
		"pause_total_ns":    m.PauseTotalNs,

		// Goroutines (not in MemStats, but very useful)
		"goroutines": runtime.NumGoroutine(),
	}
}

// ---------------------------------------------------------------------------
// Convenience: apply a full ProfileConfig
// ---------------------------------------------------------------------------

// Apply configures the runtime according to cfg and optionally starts a
// pprof HTTP server in the background.  It returns a cleanup function that
// should be called on shutdown (it stops CPU profiling if active and writes
// a memory profile if configured).
func Apply(cfg ProfileConfig) (cleanup func(), err error) {
	var cleanups []func()

	// Block profiling
	if cfg.BlockProfileRate > 0 {
		EnableBlockProfile(cfg.BlockProfileRate)
	}

	// Mutex profiling
	if cfg.MutexProfileFraction > 0 {
		EnableMutexProfile(cfg.MutexProfileFraction)
	}

	// CPU profiling to file
	if cfg.CPUProfilePath != "" {
		stop, err := StartCPUProfile(cfg.CPUProfilePath)
		if err != nil {
			return nil, err
		}
		cleanups = append(cleanups, stop)
	}

	// Memory profile on shutdown
	if cfg.MemProfilePath != "" {
		cleanups = append(cleanups, func() {
			_ = WriteMemProfile(cfg.MemProfilePath)
		})
	}

	// pprof HTTP server
	if cfg.EnablePprof {
		addr := cfg.PprofAddr
		if addr == "" {
			addr = "localhost:6060"
		}
		mux := http.NewServeMux()
		RegisterPprofHandlers(mux)

		srv := &http.Server{
			Addr:    addr,
			Handler: mux,
		}
		go func() {
			// ListenAndServe returns ErrServerClosed on clean shutdown.
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("profiling: server error: %v", err)
			}
		}()
		cleanups = append(cleanups, func() {
			_ = srv.Close()
		})
	}

	cleanup = func() {
		// Run cleanups in reverse order.
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	return cleanup, nil
}
