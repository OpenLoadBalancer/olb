package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_DetectsChange(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	// Write initial content
	initialContent := []byte("version: \"1\"\n")
	if err := os.WriteFile(configFile, initialContent, 0644); err != nil {
		t.Fatalf("Failed to write initial config: %v", err)
	}

	var changed atomic.Bool
	var mu sync.Mutex
	var receivedData []byte

	callback := func(path string, data []byte) {
		mu.Lock()
		receivedData = data
		mu.Unlock()
		changed.Store(true)
	}

	watcher, err := NewWatcher(configFile, 50*time.Millisecond, callback, nil)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.Start(ctx)

	// Wait a bit and then modify the file
	time.Sleep(100 * time.Millisecond)

	newContent := []byte("version: \"2\"\n")
	if err := os.WriteFile(configFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	// Wait for the change to be detected
	time.Sleep(200 * time.Millisecond)

	if !changed.Load() {
		t.Error("Expected callback to be called on file change")
	}

	mu.Lock()
	gotData := string(receivedData)
	mu.Unlock()
	if gotData != string(newContent) {
		t.Errorf("Received data = %q, want %q", gotData, string(newContent))
	}
}

func TestWatcher_IgnoresNoChange(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	content := []byte("version: \"1\"\n")
	if err := os.WriteFile(configFile, content, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	var callCount atomic.Int32

	callback := func(path string, data []byte) {
		callCount.Add(1)
	}

	watcher, err := NewWatcher(configFile, 50*time.Millisecond, callback, nil)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.Start(ctx)

	// Wait for multiple polling cycles
	time.Sleep(300 * time.Millisecond)

	// Should not have triggered since file hasn't changed
	if callCount.Load() != 0 {
		t.Errorf("Expected 0 calls, got %d", callCount.Load())
	}
}

func TestWatcher_GetData(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	content := []byte("version: \"1\"\n")
	if err := os.WriteFile(configFile, content, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	watcher, err := NewWatcher(configFile, 100*time.Millisecond, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	data := watcher.GetData()
	if string(data) != string(content) {
		t.Errorf("GetData() = %q, want %q", string(data), string(content))
	}
}

func TestWatcher_ErrorCallback(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	content := []byte("version: \"1\"\n")
	if err := os.WriteFile(configFile, content, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	var errCalled atomic.Bool

	errback := func(path string, err error) {
		errCalled.Store(true)
	}

	watcher, err := NewWatcher(configFile, 50*time.Millisecond, nil, errback)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.Start(ctx)

	// Delete the file to cause an error
	if err := os.Remove(configFile); err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}

	// Wait for error to be detected
	time.Sleep(200 * time.Millisecond)

	if !errCalled.Load() {
		t.Error("Expected error callback to be called")
	}
}

func TestWatcher_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	content := []byte("version: \"1\"\n")
	if err := os.WriteFile(configFile, content, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	var callCount atomic.Int32

	callback := func(path string, data []byte) {
		callCount.Add(1)
	}

	watcher, err := NewWatcher(configFile, 50*time.Millisecond, callback, nil)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.Start(ctx)

	// Stop the watcher
	watcher.Stop()

	// Modify the file after stopping
	time.Sleep(100 * time.Millisecond)
	newContent := []byte("version: \"2\"\n")
	if err := os.WriteFile(configFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}

	// Wait and verify no callback was called after stop
	time.Sleep(200 * time.Millisecond)

	if callCount.Load() != 0 {
		t.Errorf("Expected 0 calls after stop, got %d", callCount.Load())
	}
}

func TestWatcher_MinimumInterval(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	content := []byte("version: \"1\"\n")
	if err := os.WriteFile(configFile, content, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Try to create with very small interval
	watcher, err := NewWatcher(configFile, 1*time.Millisecond, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Should have been set to minimum 100ms
	if watcher.interval < 100*time.Millisecond {
		t.Errorf("interval = %v, want at least 100ms", watcher.interval)
	}
}

func TestWatcher_InitialFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "nonexistent.yaml")

	_, err := NewWatcher(configFile, 100*time.Millisecond, nil, nil)
	if err == nil {
		t.Error("Expected error when initial file doesn't exist")
	}
}
