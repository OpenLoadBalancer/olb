// Package health provides health checking for OpenLoadBalancer backends.
package health

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// Status represents the health status of a backend.
type Status int

const (
	// StatusUnknown indicates the health status is not yet determined.
	StatusUnknown Status = iota
	// StatusHealthy indicates the backend is healthy.
	StatusHealthy
	// StatusUnhealthy indicates the backend is unhealthy.
	StatusUnhealthy
)

// String returns the string representation of the health status.
func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// Check represents a health check for a single backend.
type Check struct {
	// Type is the health check type ("http", "tcp", "grpc", "exec").
	Type string

	// Interval is the time between health checks.
	Interval time.Duration

	// Timeout is the maximum time to wait for a health check response.
	Timeout time.Duration

	// Path is the HTTP path for health checks (for HTTP type).
	Path string

	// Method is the HTTP method for health checks (for HTTP type).
	Method string

	// ExpectedStatus is the expected HTTP status code (for HTTP type).
	// 0 means any 2xx status is acceptable.
	ExpectedStatus int

	// Headers are additional HTTP headers to send (for HTTP type).
	Headers map[string]string

	// Command is the command to execute for exec health checks.
	// The command is run with the backend address as an argument.
	Command string

	// Args are additional arguments for the exec health check command.
	Args []string

	// HealthyThreshold is the number of consecutive successes required
	// to transition from unhealthy to healthy.
	HealthyThreshold int

	// UnhealthyThreshold is the number of consecutive failures required
	// to transition from healthy to unhealthy.
	UnhealthyThreshold int
}

// DefaultCheck returns a default health check configuration.
func DefaultCheck() *Check {
	return &Check{
		Type:               "tcp",
		Interval:           10 * time.Second,
		Timeout:            5 * time.Second,
		Path:               "/health",
		Method:             "GET",
		ExpectedStatus:     200,
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
	}
}

// Result represents the result of a single health check.
type Result struct {
	Healthy   bool
	Latency   time.Duration
	Error     error
	Timestamp time.Time
}

// Checker performs health checks for backends.
type Checker struct {
	mu              sync.RWMutex
	checks          map[string]*checkState // backend ID -> check state
	stopCh          chan struct{}
	stopOnce        sync.Once
	wg              sync.WaitGroup
	httpClient      *http.Client // shared HTTP client for HTTP/HTTPS health checks
	grpcClient      *http.Client // shared HTTP client for gRPC health checks (TLS + H2)
	grpcPlainClient *http.Client // shared HTTP client for gRPC fallback (plain H2C)
}

// checkState holds the state for a single backend's health check.
type checkState struct {
	backend         *backend.Backend
	config          *Check
	status          Status
	consecutiveOK   int
	consecutiveFail int
	lastResult      *Result
	mu              sync.RWMutex
	stopCh          chan struct{} // closed by Unregister to signal goroutine exit
}

// NewChecker creates a new health checker.
func NewChecker() *Checker {
	// Shared HTTP client for HTTP/HTTPS health checks.
	// 10s overall timeout, no redirect following, connection reuse enabled.
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Shared HTTP client for gRPC health checks (TLS with InsecureSkipVerify + HTTP/2).
	grpcClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec — health check to internal backends
			ForceAttemptHTTP2: true,
		},
	}

	// Shared HTTP client for gRPC fallback (plain HTTP/2 without TLS).
	grpcPlainClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
		},
	}

	return &Checker{
		checks:          make(map[string]*checkState),
		stopCh:          make(chan struct{}),
		httpClient:      httpClient,
		grpcClient:      grpcClient,
		grpcPlainClient: grpcPlainClient,
	}
}

// SetGRPCTLSSkipVerify configures whether the gRPC health check client
// skips TLS certificate verification. Default is true for backward compatibility
// with self-signed internal certificates. Set to false in production environments
// where backends use trusted certificates.
func (c *Checker) SetGRPCTLSSkipVerify(skip bool) {
	c.grpcClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: skip}, //nolint:gosec
			ForceAttemptHTTP2: true,
		},
	}
}

// Register registers a backend for health checking.
func (c *Checker) Register(b *backend.Backend, config *Check) error {
	if b == nil {
		return errors.New("backend cannot be nil")
	}
	if config == nil {
		config = DefaultCheck()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.checks[b.ID]; exists {
		return fmt.Errorf("backend %s is already registered", b.ID)
	}

	state := &checkState{
		backend: b,
		config:  config,
		status:  StatusUnknown,
		stopCh:  make(chan struct{}),
	}
	c.checks[b.ID] = state

	// Start the health check goroutine
	c.wg.Add(1)
	go c.runCheck(state)

	return nil
}

// Unregister removes a backend from health checking and stops its goroutine.
func (c *Checker) Unregister(backendID string) {
	c.mu.Lock()

	state, exists := c.checks[backendID]
	if !exists {
		c.mu.Unlock()
		return
	}
	delete(c.checks, backendID)
	c.mu.Unlock()

	// Signal the goroutine to exit by closing its per-backend stop channel.
	close(state.stopCh)
}

// GetStatus returns the current health status of a backend.
func (c *Checker) GetStatus(backendID string) Status {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if state, exists := c.checks[backendID]; exists {
		state.mu.RLock()
		defer state.mu.RUnlock()
		return state.status
	}
	return StatusUnknown
}

// GetResult returns the last health check result for a backend.
func (c *Checker) GetResult(backendID string) *Result {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if state, exists := c.checks[backendID]; exists {
		state.mu.RLock()
		defer state.mu.RUnlock()
		return state.lastResult
	}
	return nil
}

// Stop stops all health check goroutines.
func (c *Checker) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.wg.Wait()
}

// runCheck runs the health check loop for a single backend.
func (c *Checker) runCheck(state *checkState) {
	defer c.wg.Done()

	ticker := time.NewTicker(state.config.Interval)
	defer ticker.Stop()

	// Run initial check immediately
	c.performCheck(state)

	for {
		select {
		case <-ticker.C:
			c.performCheck(state)
		case <-c.stopCh:
			return
		case <-state.stopCh:
			return
		}
	}
}

// performCheck performs a single health check.
func (c *Checker) performCheck(state *checkState) {
	config := state.config

	var result Result
	start := time.Now()

	switch config.Type {
	case "http", "https":
		result = c.checkHTTP(state.backend, config)
	case "tcp":
		result = c.checkTCP(state.backend, config)
	case "grpc":
		result = c.checkGRPC(state.backend, config)
	case "exec":
		result = c.checkExec(state.backend, config)
	default:
		result = Result{
			Healthy: false,
			Error:   fmt.Errorf("unknown health check type: %s", config.Type),
		}
	}

	result.Latency = time.Since(start)
	result.Timestamp = time.Now()

	// Update state
	state.mu.Lock()
	state.lastResult = &result

	if result.Healthy {
		state.consecutiveOK++
		state.consecutiveFail = 0

		// Check if we should transition to healthy
		if state.status != StatusHealthy && state.consecutiveOK >= config.HealthyThreshold {
			state.status = StatusHealthy
			state.backend.SetState(backend.StateUp)
		}
	} else {
		state.consecutiveFail++
		state.consecutiveOK = 0

		// Check if we should transition to unhealthy
		if state.status != StatusUnhealthy && state.consecutiveFail >= config.UnhealthyThreshold {
			state.status = StatusUnhealthy
			state.backend.SetState(backend.StateDown)
		}
	}
	state.mu.Unlock()

	// Record the health check result on the backend
	state.backend.RecordHealthCheck(result.Healthy)
}

// checkHTTP performs an HTTP health check.
func (c *Checker) checkHTTP(b *backend.Backend, config *Check) Result {
	// SSRF protection: reject health checks targeting internal/private addresses
	host, _, splitErr := net.SplitHostPort(b.Address)
	if splitErr != nil {
		host = b.Address
	}
	if isInternalAddress(host) {
		return Result{Healthy: false, Error: fmt.Errorf("health check target %q is an internal/private address (SSRF protection)", host)}
	}

	url := "http://" + b.Address + config.Path
	if config.Type == "https" {
		url = "https://" + b.Address + config.Path
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, config.Method, url, nil)
	if err != nil {
		return Result{Healthy: false, Error: err}
	}

	// Add headers
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{Healthy: false, Error: err}
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// Check status code
	if config.ExpectedStatus != 0 {
		if resp.StatusCode != config.ExpectedStatus {
			return Result{
				Healthy: false,
				Error:   fmt.Errorf("unexpected status code: %d, expected: %d", resp.StatusCode, config.ExpectedStatus),
			}
		}
	} else {
		// Any 2xx status is acceptable
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Result{
				Healthy: false,
				Error:   fmt.Errorf("unexpected status code: %d", resp.StatusCode),
			}
		}
	}

	return Result{Healthy: true}
}

// checkGRPC performs a gRPC health check using a minimal HTTP/2 approach.
// It sends a gRPC request to /grpc.health.v1.Health/Check and checks the
// response status. This works without external protobuf dependencies by
// sending a pre-encoded empty Check request and validating the gRPC status.
func (c *Checker) checkGRPC(b *backend.Backend, config *Check) Result {
	// gRPC uses HTTP/2 POST with specific content type
	url := "https://" + b.Address + "/grpc.health.v1.Health/Check"

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// Pre-encoded protobuf: empty HealthCheckRequest = no fields = empty message
	// gRPC wire format: [compressed(1 byte) + length(4 bytes) + message(0 bytes)]
	grpcPayload := []byte{0, 0, 0, 0, 0} // uncompressed, 0-length message

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(grpcPayload))
	if err != nil {
		return Result{Healthy: false, Error: err}
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")

	resp, err := c.grpcClient.Do(req)
	if err != nil {
		// Fallback: if HTTPS fails (self-signed or no TLS), try plain HTTP/2
		url = "http://" + b.Address + "/grpc.health.v1.Health/Check"
		req, err = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(grpcPayload))
		if err != nil {
			return Result{Healthy: false, Error: fmt.Errorf("grpc fallback request creation failed: %w", err)}
		}
		req.Header.Set("Content-Type", "application/grpc")
		req.Header.Set("TE", "trailers")

		resp, err = c.grpcPlainClient.Do(req)
		if err != nil {
			return Result{Healthy: false, Error: fmt.Errorf("grpc health check failed: %w", err)}
		}
	}
	defer resp.Body.Close()

	// Check gRPC status from trailers (grpc-status: 0 = OK)
	grpcStatus := resp.Trailer.Get("Grpc-Status")
	if grpcStatus == "" {
		grpcStatus = resp.Header.Get("Grpc-Status")
	}
	if grpcStatus != "" && grpcStatus != "0" {
		return Result{
			Healthy: false,
			Error:   fmt.Errorf("grpc health check returned status: %s", grpcStatus),
		}
	}

	// HTTP 200 with gRPC status 0 (or absent) = healthy
	if resp.StatusCode == http.StatusOK {
		return Result{Healthy: true}
	}

	return Result{
		Healthy: false,
		Error:   fmt.Errorf("grpc health check HTTP status: %d", resp.StatusCode),
	}
}

// checkExec performs an exec-based health check by running an external command.
// The backend address is appended to the command arguments.
// A successful health check is indicated by a zero exit code.
func (c *Checker) checkExec(b *backend.Backend, config *Check) Result {
	if config.Command == "" {
		return Result{Healthy: false, Error: fmt.Errorf("exec health check: no command specified")}
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	host, port, _ := net.SplitHostPort(b.Address)
	args := make([]string, len(config.Args))
	for i, arg := range config.Args {
		args[i] = resolveExecTemplate(arg, b.Address, host, port)
	}
	resolvedCmd := resolveExecTemplate(config.Command, b.Address, host, port)

	// Validate command path — must be absolute to prevent path injection
	if !filepath.IsAbs(resolvedCmd) {
		return Result{Healthy: false, Error: fmt.Errorf("exec health check: command must be an absolute path: %s", resolvedCmd)}
	}

	cmd := exec.CommandContext(ctx, resolvedCmd, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Result{Healthy: false, Error: fmt.Errorf("exec health check timed out after %s", config.Timeout)}
		}
		errMsg := stderr.String()
		if errMsg != "" {
			return Result{Healthy: false, Error: fmt.Errorf("exec health check failed: %w: stderr: %s", err, truncateString(errMsg, 200))}
		}
		return Result{Healthy: false, Error: fmt.Errorf("exec health check failed: %w", err)}
	}

	return Result{Healthy: true}
}

// resolveExecTemplate replaces {{.Address}}, {{.Host}}, and {{.Port}} placeholders
// in an exec health check template string. If no templates are present, the
// original string is returned unchanged.
func resolveExecTemplate(s, address, host, port string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	s = strings.ReplaceAll(s, "{{.Address}}", address)
	s = strings.ReplaceAll(s, "{{.Host}}", host)
	s = strings.ReplaceAll(s, "{{.Port}}", port)
	return s
}

// truncateString truncates a string to maxLen characters, appending "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// checkTCP performs a TCP health check.
func (c *Checker) checkTCP(b *backend.Backend, config *Check) Result {
	conn, err := net.DialTimeout("tcp", b.Address, config.Timeout)
	if err != nil {
		return Result{Healthy: false, Error: err}
	}
	defer conn.Close()

	return Result{Healthy: true}
}

// ListStatuses returns the health status of all registered backends.
func (c *Checker) ListStatuses() map[string]Status {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statuses := make(map[string]Status, len(c.checks))
	for id, state := range c.checks {
		state.mu.RLock()
		statuses[id] = state.status
		state.mu.RUnlock()
	}
	return statuses
}

// CountHealthy returns the number of healthy backends.
func (c *Checker) CountHealthy() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	for _, state := range c.checks {
		state.mu.RLock()
		if state.status == StatusHealthy {
			count++
		}
		state.mu.RUnlock()
	}
	return count
}

// CountUnhealthy returns the number of unhealthy backends.
func (c *Checker) CountUnhealthy() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	for _, state := range c.checks {
		state.mu.RLock()
		if state.status == StatusUnhealthy {
			count++
		}
		state.mu.RUnlock()
	}
	return count
}

// isInternalAddress checks if a host is a cloud metadata endpoint or
// link-local address. These are high-value SSRF targets that should never
// be health check destinations.
// Note: localhost/RFC1918 are NOT blocked since legitimate backends commonly
// run on internal addresses. Health check addresses are admin-controlled.
func isInternalAddress(host string) bool {
	switch host {
	case "169.254.169.254", "metadata.google.internal", "metadata.google",
		"100.100.100.200", "fd00:ec2::254":
		return true
	}

	// Block all 169.254.x.x link-local addresses
	if strings.HasPrefix(host, "169.254.") {
		return true
	}

	return false
}
