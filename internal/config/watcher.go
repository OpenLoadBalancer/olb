package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"
)

// ChangeCallback is called when the config file changes.
type ChangeCallback func(path string, data []byte)

// ErrorCallback is called when there's an error watching the file.
type ErrorCallback func(path string, err error)

// Watcher watches a config file for changes.
type Watcher struct {
	path     string
	interval time.Duration
	callback ChangeCallback
	errback  ErrorCallback

	// internal state
	mu      sync.RWMutex
	hash    string
	data    []byte
	stopCh  chan struct{}
	stopped bool
	wg      sync.WaitGroup // tracks the watch goroutine for graceful shutdown
}

// NewWatcher creates a new file watcher.
// interval is the polling interval (minimum 100ms).
func NewWatcher(path string, interval time.Duration, callback ChangeCallback, errback ErrorCallback) (*Watcher, error) {
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}

	// Read initial file content
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read initial config: %w", err)
	}

	return &Watcher{
		path:     path,
		interval: interval,
		callback: callback,
		errback:  errback,
		hash:     hashBytes(data),
		data:     data,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start starts watching the file for changes.
func (w *Watcher) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.watch(ctx)
	}()
}

// Stop stops watching the file and waits for the watch goroutine to exit.
func (w *Watcher) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.mu.Unlock()

	close(w.stopCh)
	w.wg.Wait()
}

// GetData returns the current config data.
func (w *Watcher) GetData() []byte {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.data
}

// watch polls the file for changes.
func (w *Watcher) watch(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.check()
		}
	}
}

// check reads the file and compares hashes.
func (w *Watcher) check() {
	data, err := os.ReadFile(w.path)
	if err != nil {
		if w.errback != nil {
			w.errback(w.path, fmt.Errorf("failed to read file: %w", err))
		}
		return
	}

	newHash := hashBytes(data)

	w.mu.Lock()
	if newHash != w.hash {
		w.hash = newHash
		w.data = data
		w.mu.Unlock()

		if w.callback != nil {
			w.callback(w.path, data)
		}
	} else {
		w.mu.Unlock()
	}
}

// hashBytes returns SHA-256 hash of data as hex string.
func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
