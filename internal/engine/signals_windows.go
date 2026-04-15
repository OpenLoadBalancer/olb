//go:build windows
// +build windows

package engine

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openloadbalancer/olb/internal/logging"
)

// setupSignalHandlers installs signal handlers for Windows systems.
// Windows only supports SIGINT (Ctrl+C) and SIGTERM.
func (e *Engine) setupSignalHandlers() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh,
		syscall.SIGTERM, // Graceful shutdown
		syscall.SIGINT,  // Graceful shutdown (Ctrl+C, Ctrl+Break)
	)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("Signal handler panic recovered", logging.Any("panic", r))
			}
		}()
		for {
			select {
			case sig := <-sigCh:
				switch sig {
				case syscall.SIGTERM, syscall.SIGINT:
					e.logger.Info("Received shutdown signal", logging.String("signal", sig.String()))
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					err := e.Shutdown(ctx)
					cancel()
					if err != nil {
						e.logger.Error("Shutdown failed", logging.Error(err))
					}
					return
				}

			case <-e.stopCh:
				return
			}
		}
	}()
}
