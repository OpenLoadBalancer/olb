# Contributing to OpenLoadBalancer

## Quick Start

```bash
git clone https://github.com/openloadbalancer/olb.git
cd olb
go build ./cmd/olb/
go test ./...
```

## Rules

1. **Zero external dependencies** — only Go stdlib. The only exception is `golang.org/x/crypto` for bcrypt and OCSP.
2. **All code must be gofmt formatted** — CI enforces this.
3. **All features must be wired end-to-end** — no dead code. If you add a feature, wire it in `internal/engine/engine.go`.
4. **Tests required** — new code must include tests. Current coverage: 89%.
5. **No hardcoded ports in tests** — use `net.Listen(":0")` for dynamic port allocation.

## Development

```bash
make build          # Build binary
make test           # Run tests
make lint           # Format + vet
make bench          # Run benchmarks
make build-all      # Cross-platform build
```

## Pull Requests

- Keep PRs focused on a single change
- Include tests for new functionality
- Run `gofmt -w .` and `go vet ./...` before submitting
- Ensure all existing tests pass: `go test ./...`

## Architecture

See [CLAUDE.md](CLAUDE.md) for the full architecture guide.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
