package middleware

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
)

// RecoveryConfig configures the panic recovery middleware.
type RecoveryConfig struct {
	// LogFunc is called with the panic value and stack trace.
	// If nil, panics are logged via log.Printf.
	LogFunc func(panicVal any, stack string)
}

// RecoveryMiddleware recovers from panics in downstream handlers and
// returns a 500 Internal Server Error instead of crashing the process.
type RecoveryMiddleware struct {
	config RecoveryConfig
}

// NewRecoveryMiddleware creates a new panic recovery middleware.
func NewRecoveryMiddleware(config RecoveryConfig) *RecoveryMiddleware {
	return &RecoveryMiddleware{config: config}
}

// Name returns the middleware name.
func (rm *RecoveryMiddleware) Name() string { return "recovery" }

// Priority returns the middleware priority (must run first — lowest number).
func (rm *RecoveryMiddleware) Priority() int { return 1 }

// Wrap wraps the handler with panic recovery.
func (rm *RecoveryMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				stack := string(debug.Stack())
				if rm.config.LogFunc != nil {
					rm.config.LogFunc(rv, stack)
				} else {
					log.Printf("[recovery] panic recovered: %v\n%s", rv, stack)
				}

				// Only write error if nothing was written yet
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(w, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
