//go:build !windows

package logging

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// ReopenHandler handles SIGUSR1 for log file reopening.
// This is useful for log rotation tools like logrotate.
type ReopenHandler struct {
	outputs   []*RotatingFileOutput
	sigCh     chan os.Signal
	stopCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewReopenHandler creates a new signal handler for log reopening.
func NewReopenHandler() *ReopenHandler {
	return &ReopenHandler{
		outputs: make([]*RotatingFileOutput, 0),
		sigCh:   make(chan os.Signal, 1),
		stopCh:  make(chan struct{}),
	}
}

// AddOutput adds a rotating file output to be reopened on signal.
func (h *ReopenHandler) AddOutput(out *RotatingFileOutput) {
	h.outputs = append(h.outputs, out)
}

// Start starts listening for SIGUSR1. Safe to call multiple times.
func (h *ReopenHandler) Start() {
	h.startOnce.Do(func() {
		signal.Notify(h.sigCh, syscall.SIGUSR1)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[logging] panic recovered in SIGUSR1 handler: %v", r)
				}
			}()
			for {
				select {
				case <-h.sigCh:
					h.reopen()
				case <-h.stopCh:
					return
				}
			}
		}()
	})
}

// Stop stops the signal handler. Safe to call multiple times.
func (h *ReopenHandler) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
		signal.Stop(h.sigCh)
	})
}

// reopen reopens all registered outputs.
func (h *ReopenHandler) reopen() {
	for _, out := range h.outputs {
		if out != nil {
			_ = out.Reopen()
		}
	}
}

// Global reopen handler
var defaultReopenHandler *ReopenHandler
var defaultReopenOnce sync.Once

// EnableLogReopen enables SIGUSR1 handling for the given outputs.
func EnableLogReopen(outputs ...*RotatingFileOutput) {
	defaultReopenOnce.Do(func() {
		defaultReopenHandler = NewReopenHandler()
		defaultReopenHandler.Start()
	})

	for _, out := range outputs {
		defaultReopenHandler.AddOutput(out)
	}
}

// StopLogReopen stops the default reopen handler.
func StopLogReopen() {
	if defaultReopenHandler != nil {
		defaultReopenHandler.Stop()
	}
}
