package admin

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// adminCircuitBreaker protects admin API handlers from cascading failures
// when engine internals are slow or unresponsive. Unlike the HTTP-layer
// circuit breaker in middleware, this wraps Go function calls with timeout
// and error counting.
type adminCircuitBreaker struct {
	mu sync.Mutex

	state     cbState
	openSince time.Time
	errors    int
	successes int
	lastError time.Time

	// Configuration
	errorThreshold int           // errors before opening (default: 5)
	openDuration   time.Duration // how long to stay open (default: 30s)
	timeout        time.Duration // per-call timeout (default: 10s)
	windowSize     time.Duration // error counting window (default: 60s)
}

type cbState int

const (
	cbClosed cbState = iota
	cbOpen
	cbHalfOpen
)

// newAdminCircuitBreaker creates a new circuit breaker for admin API protection.
func newAdminCircuitBreaker() *adminCircuitBreaker {
	return &adminCircuitBreaker{
		state:          cbClosed,
		errorThreshold: 5,
		openDuration:   30 * time.Second,
		timeout:        10 * time.Second,
		windowSize:     60 * time.Second,
	}
}

// State returns the current circuit breaker state as a string.
func (acb *adminCircuitBreaker) State() string {
	acb.mu.Lock()
	defer acb.mu.Unlock()

	now := time.Now()

	switch acb.state {
	case cbOpen:
		if now.Sub(acb.openSince) >= acb.openDuration {
			return "half-open"
		}
		return "open"
	case cbHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Execute runs the given function with circuit breaker protection.
// The caller's context deadline is combined with the circuit breaker's timeout.
// Returns an error if the circuit is open or if the function fails/times out.
func (acb *adminCircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	acb.mu.Lock()
	now := time.Now()

	switch acb.state {
	case cbOpen:
		if now.Sub(acb.openSince) >= acb.openDuration {
			acb.state = cbHalfOpen
			acb.errors = 0
			acb.successes = 0
		} else {
			acb.mu.Unlock()
			return fmt.Errorf("admin circuit breaker open (retry after %v)", acb.openDuration-time.Since(acb.openSince))
		}
	case cbClosed:
		// Evict old errors outside the window
		if acb.lastError.IsZero() || now.Sub(acb.lastError) > acb.windowSize {
			acb.errors = 0
			acb.successes = 0
		}
	case cbHalfOpen:
		// Allow through
	}
	acb.mu.Unlock()

	// Combine caller context with circuit breaker timeout
	callCtx, cancel := context.WithTimeout(ctx, acb.timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic in admin handler: %v", r)
			}
		}()
		done <- fn(callCtx)
	}()

	var err error
	select {
	case err = <-done:
		// Function completed
	case <-callCtx.Done():
		err = fmt.Errorf("admin call timed out after %v", acb.timeout)
		// Drain the goroutine so it doesn't leak. Bound the wait
		// to prevent permanent leaks if fn ignores context cancellation.
		go func() {
			timer := time.NewTimer(acb.timeout)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
			}
		}()
	}

	acb.recordOutcome(err)
	return err
}

// recordOutcome records the result of a function call.
func (acb *adminCircuitBreaker) recordOutcome(err error) {
	acb.mu.Lock()
	defer acb.mu.Unlock()

	if err == nil {
		acb.successes++
		if acb.state == cbHalfOpen && acb.successes >= 3 {
			acb.state = cbClosed
			acb.errors = 0
			acb.successes = 0
		}
		return
	}

	acb.errors++
	acb.lastError = time.Now()

	switch acb.state {
	case cbClosed:
		if acb.errors >= acb.errorThreshold {
			acb.state = cbOpen
			acb.openSince = time.Now()
		}
	case cbHalfOpen:
		acb.state = cbOpen
		acb.openSince = time.Now()
	}
}

// Reset resets the circuit breaker to closed state.
func (acb *adminCircuitBreaker) Reset() {
	acb.mu.Lock()
	defer acb.mu.Unlock()
	acb.state = cbClosed
	acb.errors = 0
	acb.successes = 0
	acb.openSince = time.Time{}
}
