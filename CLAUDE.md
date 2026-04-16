# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project
High-performance zero-dependency L4/L7 load balancer written in Go.
- Website: https://openloadbalancer.dev
- Repo: github.com/openloadbalancer/olb
- Go 1.26+, single binary, ~380 Go files
- External deps: `golang.org/x/crypto` (bcrypt/OCSP), `golang.org/x/net` (http2), `golang.org/x/text` (indirect) — no other external deps allowed
- Architecture Decision Records: `docs/architecture-decisions.md` (ADR-001 through ADR-008)

## Build & Test
```bash
go build ./cmd/olb/                       # Build binary (output: bin/olb)
go test ./...                              # Run all tests
go test -race ./...                        # Race detector (needs CGO/GCC, Linux only)
go test -cover ./...                       # Coverage (must stay ≥85%)
go test ./internal/balancer/...            # Run single package tests
go test -run TestRoundRobin ./internal/balancer/  # Run single test
go test -bench=. -benchmem ./...          # Benchmarks
gofmt -w .                                 # Format all
go vet ./...                               # Vet
make build                                 # Build via Makefile (output: bin/olb)
make build-debug                           # Build with debug symbols (no -s -w)
make build-race                            # Build with race detector (CGO=1)
make test                                  # Test with coverage output
make test-short                            # Short tests only (-short flag)
make coverage                              # Test + generate HTML coverage report
make coverage-check                        # Verify total coverage ≥ 85%
make coverage-check-packages               # Warn on packages below 85%
make check                                 # fmt + vet + lint + test + webui
make ci                                    # build + test + lint + webui (CI equivalent)
make build-all                             # Cross-platform (linux/darwin/windows/freebsd)
make dev                                   # Build debug + run with --log-level debug
make run                                   # Build and run locally
make tidy                                  # go mod tidy + verify
make clean                                 # Remove build artifacts
make bench-compare BASE=main               # Compare benchmarks between branches
```
CI test flags: `-p 1 -short -count=1 -timeout=600s`. Binary must stay under 20MB (enforced in CI).
E2E tests: `test/e2e/` (e2e, chaos, load, realworld, TLS integration).
Example configs: `configs/` (YAML, TOML, HCL).

## CLI Commands
Binary is `olb`. Subcommands registered in `cmd/olb/main.go` via `internal/cli`:
- `olb start --config /path/to/config.yaml` — start the load balancer
- `olb stop` — graceful shutdown
- `olb reload` — hot reload config
- `olb status` — show runtime status
- `olb top` — live TUI dashboard
- `olb version` — print version (fields set via ldflags)
- `olb config` — config management
- `olb backend` — backend operations
- `olb health` — health check operations
- `olb setup` — interactive first-run setup

## WebUI (Embedded Admin Dashboard)
Located at `internal/webui/`. React 19 + TypeScript + Tailwind CSS v4 + Radix UI (ADR-004).
Built with Vite, embedded into the Go binary via `embed.FS`.
```bash
make webui-deps          # Install npm dependencies (npm ci; CI uses --legacy-peer-deps)
make webui-build         # Build frontend assets (tsc + vite build)
make webui-lint          # Lint frontend (eslint)
make webui-test          # Run unit tests (vitest)
cd internal/webui && npm run dev        # Dev server with HMR
cd internal/webui && npm run test:e2e   # Playwright E2E tests
```
CI builds frontend first, then Go tests depend on built assets.

## Website (openloadbalancer.dev)
Located at `website-new/`. React 19 + TypeScript + Tailwind CSS v4 + Vite.
```bash
cd website-new && npm install && npm run dev    # Dev server
cd website-new && npm run build                  # Production build
```
Deployed via GitHub Pages (`.github/workflows/pages.yml`).

## Architecture
```
cmd/olb/main.go          → Entry point, delegates to internal/cli
internal/engine/          → Central orchestrator (engine.go, config.go, adapters.go, lifecycle.go, pools_routes.go, listeners.go, middleware_registration.go, signals_{unix,windows}.go)
internal/proxy/l7/        → HTTP reverse proxy + WebSocket/gRPC/SSE detection + shadow manager
internal/proxy/l4/        → TCP/UDP proxy, SNI routing, PROXY protocol
internal/balancer/        → 16 load balancing algorithms
internal/middleware/      → Config-gated middleware (see list below)
internal/router/          → Radix trie router (router.go + radix.go)
internal/config/          → YAML/TOML/HCL/JSON config + hot reload + env var substitution
internal/admin/           → REST API + Web UI serving (30+ endpoints)
internal/cluster/         → Raft consensus + SWIM gossip
internal/mcp/             → MCP server for AI integration (SSE + HTTP + stdio, 17 tools)
internal/tls/             → TLS, mTLS, OCSP stapling
internal/acme/            → ACME/Let's Encrypt client
internal/health/          → Active + passive health checking (HTTP, TCP, gRPC)
internal/waf/             → WAF: 6-layer security pipeline (SQLi, XSS, CMDi, XXE, SSRF, path traversal)
internal/security/        → Request smuggling, header injection protection
internal/webui/           → Embedded SPA (React 19 + TypeScript + Tailwind CSS + Radix UI)
internal/plugin/          → Plugin system with event bus
internal/discovery/       → Service discovery (static/DNS/file/Docker/Consul)
internal/geodns/          → Geographic DNS routing
internal/conn/            → Connection management + pooling
internal/logging/         → Structured JSON logging with rotating file output
internal/metrics/         → Prometheus metrics registry (40+ metrics)
internal/profiling/       → Go runtime profiling (pprof)
pkg/version/              → Version info injected via ldflags at build time
pkg/utils/                → Buffer pool, LRU, ring buffer, CIDR matcher
pkg/errors/               → Sentinel errors with context
```

### Request Flow
```
Client → Listener → Middleware Chain → Router (radix trie path match) → Pool (balancer selects backend) → Backend
                                                                                                      ↓
                                                                                               Health checker updates backend state
```

### Engine Lifecycle
`engine.New()` creates all components. `Start()` starts listeners, health checks, admin, cluster, config watcher. `Shutdown()` stops everything gracefully. `Reload()` re-reads config and reinitializes pools/routes/listeners.

### Middleware Components
Wired in `internal/engine/middleware_registration.go`. Each has `enabled: true/false` in config (ADR-005):
requestid, realip, logging, metrics, basic auth, JWT, API key, HMAC, OAuth2, rate_limit, CORS (via secureheaders), cache, request coalesce, bot detection, CSRF, CSP, force SSL, request rewrite, transformer, validator, trace, WAF.

## Key Rules
1. **No new external deps** — only stdlib + existing x/crypto, x/net, x/text
2. **All features must be wired in engine.go** — `New()` creates the component, `Start()` runs it. Middleware goes in `middleware_registration.go`
3. **Config-gated middleware** — each middleware has `enabled: true/false`
4. **Port 0 in tests** — never hardcode ports, use `net.Listen(":0")`
5. **gofmt clean** — CI enforces formatting
6. **Coverage ≥ 85%** — enforced by CI and `make coverage-check`
7. **Binary size ≤ 20MB** — enforced in CI
8. **Only openloadbalancer.dev** — no other domains in any file
9. **Conventional commits** — `feat:`, `fix:`, `test:`, `docs:`, `refactor:`, `perf:`, `ci:`
10. **Version via ldflags** — `pkg/version` fields set by Makefile at build time

## Balancer Algorithms
16 algorithms, registered via `Register()` + `init()` in `internal/balancer/balancer.go`. Engine calls `balancer.New(algorithmName)` which looks up the registry. To add a new algorithm: implement the `Balancer` interface, register it in `init()`, and add tests. The `Next()` method receives a `RequestContext` (client IP, headers) for context-aware routing. Config accepts full names and aliases:
| Algorithm | Config Names |
|-----------|-------------|
| Round Robin | `round_robin`, `rr` |
| Weighted RR | `weighted_round_robin`, `wrr` |
| Least Connections | `least_connections`, `lc` |
| Weighted LC | `weighted_least_connections`, `wlc` |
| Least Response Time | `least_response_time`, `lrt` |
| Weighted LRT | `weighted_least_response_time`, `wlrt` |
| IP Hash | `ip_hash`, `iphash` |
| Consistent Hash | `consistent_hash`, `ch`, `ketama` |
| Maglev | `maglev` |
| Ring Hash | `ring_hash`, `ringhash` |
| Power of Two | `power_of_two`, `p2c` |
| Random | `random` |
| Weighted Random | `weighted_random`, `wrandom` |
| Rendezvous Hash | `rendezvous`, `rendezvous_hash` |
| Peak EWMA | `peak_ewma`, `pewma` |
| Sticky Sessions | `sticky` |

## Config Format
Supports YAML, TOML, HCL, JSON with `${ENV_VAR}` substitution. Full reference: `docs/configuration.md`. Minimal example:
```yaml
listeners:
  - name: http
    address: ":8080"
    protocol: http          # http, https, tcp, udp
    routes:
      - path: /
        pool: my-pool

pools:
  - name: my-pool
    algorithm: round_robin
    backends:
      - address: "localhost:3001"
    health_check:
      type: http
      path: /health
      interval: 10s

middleware:
  rate_limit:
    enabled: true
    requests_per_second: 100
admin:
  address: ":9090"
```

## CI Pipeline
`.github/workflows/ci.yml` — 11 jobs: lint → build-frontend → test (3 OS matrix) → test-race → build → integration → benchmark (PR only) → docker → security (gosec + govulncheck + nancy) → binary-analysis → image-scan (Trivy).
Release workflow: `.github/workflows/release.yml` (binaries, GitHub Release, SBOM, GHCR Docker publish).