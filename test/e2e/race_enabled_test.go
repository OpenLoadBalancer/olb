//go:build race

package e2e

// raceEnabled reports whether the race detector is on; it slows request
// handling several-fold, so latency thresholds are not meaningful.
const raceEnabled = true
