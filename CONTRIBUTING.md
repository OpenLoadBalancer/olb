# Contributing to OpenLoadBalancer

Thank you for your interest in contributing to OpenLoadBalancer! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Testing](#testing)
- [Pull Request Process](#pull-request-process)
- [Architecture](#architecture)
- [Release Process](#release-process)

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## Getting Started

### Prerequisites

- Go 1.25 or later
- Make (optional, for using Makefile)
- Git

### Fork and Clone

```bash
# Fork the repository on GitHub, then clone your fork
git clone https://github.com/YOUR_USERNAME/olb.git
cd olb

# Add upstream remote
git remote add upstream https://github.com/openloadbalancer/olb.git
```

### Build and Test

```bash
# Build the binary
go build ./cmd/olb/

# Run tests
go test ./...

# Run with race detector
go test -race ./...

# Check coverage
go test -cover ./...
```

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/my-feature
# or
git checkout -b fix/my-bugfix
```

### 2. Make Changes

Follow the coding standards below.

### 3. Test Your Changes

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/balancer/...

# Run benchmarks
go test -bench=. ./...

# Format and vet
gofmt -w .
go vet ./...
```

### 4. Commit

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new load balancing algorithm
fix: resolve race condition in health checker
docs: update API documentation
test: add tests for rate limiter
refactor: simplify config parsing
perf: improve memory allocation in proxy
```

### 5. Push and Create PR

```bash
git push origin feature/my-feature
```

Then create a Pull Request on GitHub.

## Coding Standards

### Minimal External Dependencies

**CRITICAL**: OpenLoadBalancer uses only Go standard library plus `golang.org/x/crypto`, `golang.org/x/net`, and `golang.org/x/text`. No other external dependencies are allowed.

```go
// DON'T: Add external dependencies
import "github.com/some/external/lib"

// DO: Use stdlib only
import "net/http"
```

### Code Formatting

All code must be formatted with `gofmt`:

```bash
gofmt -w .
```

### Feature Wiring

All features must be wired in `internal/engine/engine.go`. No dead code allowed.

```go
// In engine.go New() function:
e := &Engine{
    // ... existing fields ...
    geoDNS:    geodns.New(config.GeoDNS),  // Wire new feature
}
```

### Config-Gated Middleware

Each middleware must have an `enabled` flag:

```yaml
middleware:
  my_middleware:
    enabled: true
    option1: value1
```

```go
// In code:
if config.Middleware.MyMiddleware.Enabled {
    // Apply middleware
}
```

### Port Usage in Tests

Never hardcode ports in tests. Use dynamic allocation:

```go
// DON'T:
listener, _ := net.Listen("tcp", ":8080")

// DO:
listener, _ := net.Listen("tcp", ":0")
port := listener.Addr().(*net.TCPAddr).Port
```

## Testing

### Test Coverage

Current coverage: ~87%. New code should maintain or improve coverage.

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Test Types

1. **Unit Tests**: Test individual functions/packages
2. **Integration Tests**: Test component interactions
3. **E2E Tests**: Located in `test/e2e/`

### Writing Tests

```go
func TestMyFeature(t *testing.T) {
    // Arrange
    config := &Config{Enabled: true}
    
    // Act
    result, err := MyFeature(config)
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

## Pull Request Process

1. **Update Documentation**: If your change affects user-facing behavior, update relevant documentation.

2. **Update CHANGELOG.md**: Add your change under the `[Unreleased]` section.

3. **Ensure CI Passes**: All checks must pass:
   - Tests
   - Coverage threshold (85%)
   - `gofmt` formatting
   - `go vet` static analysis

4. **Request Review**: At least one maintainer approval required.

5. **Squash and Merge**: Maintainers will squash commits on merge.

### PR Checklist

- [ ] Tests added/updated
- [ ] All tests pass
- [ ] Coverage maintained
- [ ] Code formatted with `gofmt`
- [ ] `go vet` passes
- [ ] Zero external dependencies added
- [ ] Feature wired in engine.go
- [ ] Documentation updated
- [ ] CHANGELOG.md updated

## Architecture

See [CLAUDE.md](CLAUDE.md) for the full architecture guide.

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/engine` | Central orchestrator |
| `internal/proxy` | L4/L7 proxy implementation |
| `internal/balancer` | Load balancing algorithms |
| `internal/middleware` | HTTP middleware chain |
| `internal/waf` | Web Application Firewall |
| `internal/config` | Configuration parsing |

---

## Code Examples

### Adding a New Balancer Algorithm

All balancers implement the `Balancer` interface in `internal/balancer/`:

```go
type Balancer interface {
    // Name returns the algorithm name for logging and config matching.
    Name() string

    // Next selects a backend from the list. Returns nil if empty.
    Next(backends []*backend.Backend) *backend.Backend

    // Add registers a backend with the balancer.
    Add(backend *backend.Backend)

    // Remove removes a backend by ID.
    Remove(id string)

    // Update notifies the balancer that a backend's state changed.
    Update(backend *backend.Backend)
}
```

**Example: Adding a "Random Two Choices" algorithm**

1. Create `internal/balancer/random_two.go`:

```go
package balancer

import (
    "math/rand"
    "github.com/openloadbalancer/olb/internal/backend"
)

// RandomTwoChoices picks two random backends and selects the one
// with fewer active connections (power-of-two-choices variant).
type RandomTwoChoices struct{}

func NewRandomTwoChoices() *RandomTwoChoices {
    return &RandomTwoChoices{}
}

func (r *RandomTwoChoices) Name() string { return "random_two_choices" }

func (r *RandomTwoChoices) Next(backends []*backend.Backend) *backend.Backend {
    if len(backends) == 0 {
        return nil
    }
    if len(backends) == 1 {
        return backends[0]
    }
    i := rand.Intn(len(backends))
    j := rand.Intn(len(backends) - 1)
    if j >= i {
        j++
    }
    // Pick the one with fewer connections
    if backends[j].ActiveConns() < backends[i].ActiveConns() {
        return backends[j]
    }
    return backends[i]
}

func (r *RandomTwoChoices) Add(*backend.Backend)    {}
func (r *RandomTwoChoices) Remove(string)           {}
func (r *RandomTwoChoices) Update(*backend.Backend)  {}
```

2. Wire in `internal/engine/engine.go` in the `initializePools` method:

```go
case "random_two_choices", "r2c":
    bal = balancer.NewRandomTwoChoices()
```

3. Write tests in `internal/balancer/random_two_test.go`:

```go
func TestRandomTwoChoices_Empty(t *testing.T) {
    r := NewRandomTwoChoices()
    if got := r.Next(nil); got != nil {
        t.Error("expected nil for empty backends")
    }
}

func TestRandomTwoChoices_Single(t *testing.T) {
    r := NewRandomTwoChoices()
    b := backend.NewBackend("b1", "localhost:8080")
    if got := r.Next([]*backend.Backend{b}); got != b {
        t.Error("expected the single backend")
    }
}
```

### Adding a New Middleware

Middlewares are HTTP handlers that wrap the request chain. Each lives in its own subdirectory under `internal/middleware/` with config gating.

**Example: Adding a "Request Logger" middleware**

1. Create `internal/middleware/reqlog/reqlog.go`:

```go
package reqlog

import (
    "log"
    "net/http"
)

// Config configures the request logger middleware.
type Config struct {
    Enabled bool   `yaml:"enabled" json:"enabled"`
    Prefix  string `yaml:"prefix" json:"prefix"`
}

// Middleware returns an HTTP middleware that logs each request.
func Middleware(cfg Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !cfg.Enabled {
                next.ServeHTTP(w, r)
                return
            }
            prefix := cfg.Prefix
            if prefix == "" {
                prefix = "[REQ]"
            }
            log.Printf("%s %s %s", prefix, r.Method, r.URL.Path)
            next.ServeHTTP(w, r)
        })
    }
}
```

2. Add config struct in `internal/config/config.go`:

```go
type ReqLogConfig struct {
    Enabled bool   `yaml:"enabled" json:"enabled"`
    Prefix  string `yaml:"prefix" json:"prefix"`
}
```

Add it to the `MiddlewareConfig` struct and YAML tags.

3. Register in `internal/engine/middleware_registration.go`:

```go
func (ctx *middlewareRegistrationContext) registerReqLogMiddleware() {
    cfg := ctx.cfg.Middleware.ReqLog
    if !cfg.Enabled {
        return
    }
    ctx.chain.Use(reqlog.Middleware(reqlog.Config{
        Enabled: cfg.Enabled,
        Prefix:  cfg.Prefix,
    }))
}
```

4. Add the call in `createMiddlewareChain()` and write tests.

### Extending the WAF

The WAF detection pipeline lives in `internal/waf/`. Each detector is a struct implementing a `Detect` method.

**Example: Adding a CRLF injection detector**

1. Create `internal/waf/detectors/crlf.go`:

```go
package detectors

import "strings"

// CRLFPattern matches common CRLF injection payloads.
var crlfPatterns = []string{
    "%0d", "%0a", "\r", "\n",
}

// DetectCRLF checks input for CRLF injection attempts.
// Returns a score (0-100) indicating threat level.
func DetectCRLF(input string) int {
    lower := strings.ToLower(input)
    score := 0
    for _, pattern := range crlfPatterns {
        if strings.Contains(lower, pattern) {
            score += 30
        }
    }
    if score > 100 {
        score = 100
    }
    return score
}
```

2. Wire into the detection engine in `internal/waf/detection.go`:

```go
// In the detect function, add alongside sqli, xss:
if config.CRLF.Enabled {
    if score := detectors.DetectCRLF(input); score > 0 {
        totalScore += score
    }
}
```

3. Add config in the WAF detector config:

```go
CRLF struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
}
```

4. Write tests in `internal/waf/detectors/crlf_test.go`:

```go
func TestDetectCRLF_Clean(t *testing.T) {
    if got := DetectCRLF("hello world"); got != 0 {
        t.Errorf("expected 0, got %d", got)
    }
}

func TestDetectCRLF_Injection(t *testing.T) {
    if got := DetectCRLF("test%0d%0aHeader:evil"); got == 0 {
        t.Error("expected non-zero score for CRLF injection")
    }
}
```

## Release Process

1. Update version in relevant files
2. Update CHANGELOG.md with release date
3. Create git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
4. Push tag: `git push origin v1.0.0`
5. CI will build and create release automatically

## Getting Help

- [Documentation](https://openloadbalancer.dev/docs)
- [GitHub Discussions](https://github.com/openloadbalancer/olb/discussions)
- [Issue Tracker](https://github.com/openloadbalancer/olb/issues)

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
