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

### Zero External Dependencies

**CRITICAL**: OpenLoadBalancer uses only Go standard library. The only exception is `golang.org/x/crypto` for bcrypt and OCSP.

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
