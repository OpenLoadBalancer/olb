//go:build !windows
// +build !windows

package engine

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openloadbalancer/olb/internal/logging"
)

// setupSignalHandlers installs signal handlers for Unix systems.
func (e *Engine) setupSignalHandlers() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh,
		syscall.SIGHUP,  // Reload configuration
		syscall.SIGTERM, // Graceful shutdown
		syscall.SIGINT,  // Graceful shutdown (Ctrl+C)
		syscall.SIGUSR1, // Reopen log files
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
				case syscall.SIGHUP:
					e.logger.Info("Received SIGHUP, reloading configuration...")
					if err := e.Reload(); err != nil {
						e.logger.Error("Reload failed", logging.Error(err))
					}

				case syscall.SIGTERM, syscall.SIGINT:
					e.logger.Info("Received shutdown signal", logging.String("signal", sig.String()))
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					err := e.Shutdown(ctx)
					cancel()
					if err != nil {
						e.logger.Error("Shutdown failed", logging.Error(err))
					}
					return

				case syscall.SIGUSR1:
					e.logger.Info("Received SIGUSR1, reopening log files...")
					if e.logFileOutput != nil {
						if err := e.logFileOutput.Reopen(); err != nil {
							e.logger.Error("Failed to reopen log file", logging.Error(err))
						} else {
							e.logger.Info("Log file reopened successfully")
						}
					}
				}

			case <-e.stopCh:
				return
			}
		}
	}()
}
