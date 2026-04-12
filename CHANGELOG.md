# Changelog

All notable changes to OpenLoadBalancer will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Frontend test suite: 50+ tests across 10 pages (dashboard, pools, backends, settings, cluster, metrics, WAF, middleware, certificates, logs, error)
- Frontend testing infrastructure: Vitest + React Testing Library + jsdom with path aliases and mock utilities
- `npm test` step in CI `build-frontend` job
- Multi-OS CI matrix (Ubuntu, macOS, Windows) for Go tests
- Concurrent test for `cachedHandler` atomic.Value safety in `internal/proxy/l7`
- API integration tests: 36 tests covering all endpoints (system, pools, backends, routes, health, metrics, config, certificates, WAF, cluster, middleware, events) with error states (401, 404, 500, 502, 503, network failure)
- Query hook tests: 24 tests for `useQuery`, `useMutation`, `useToastMutation` covering loading states, error handling, retry logic, transient error detection, and toast notifications
- Accessibility audit: automated axe-core tests across all 9 pages, all passing with zero violations
- E2E smoke tests: 4 Playwright tests covering page loading, navigation, skip-to-content link, responsive sidebar

### Changed
- Split `internal/admin/handlers.go` (981 LOC) into 5 focused files: `handler_helpers.go`, `handlers_system.go`, `handlers_pools.go`, `handlers_backends.go`, `handlers_readonly.go`
- Split `internal/config/hcl/hcl.go` (1415 LOC) into `lexer.go`, `parser.go`, `decoder.go`
- Fixed `.golangci.yml` Go version: `1.25` → `1.26` to match `go.mod`

### Fixed
- Race condition in `HTTPProxy.cachedHandler` — converted from bare `http.Handler` field to `atomic.Value` for safe concurrent `ServeHTTP` + `RebuildHandler` access
- Bug in `useQuery` hook: non-transient errors (401, 404, 500) were incorrectly retried — added `break` to stop retry loop for non-transient errors
- Logs page auto-scroll switch missing `aria-label` for screen readers
- Logs page event level filter select missing `aria-label` for screen readers
- Middleware page category filter buttons missing `aria-pressed` attribute
- Logs page table missing `<caption>` for screen reader context
- Middleware page category filter buttons missing `aria-pressed` attribute

### Changed
- Optimized per-request allocations in L7 proxy hot path: merged two `context.WithValue` into single struct, stack-allocated `attemptedBackends` array, skip backend filtering on first attempt, canonical hop-by-hop header lookup to avoid `strings.ToLower` per header
- Optimized middleware per-request allocations: pre-computed `FormatFloat` and `strconv.Itoa` in rate limiter headers, pre-computed `strings.Join` for CORS static config slices, pooled `headerResponseWriter` struct in headers middleware
- Exec health checks now support template variables (`{{.Address}}`, `{{.Host}}`, `{{.Port}}`) in both command and args
- Exec health check `command` and `args` fields are now configurable from YAML/JSON/TOML/HCL configs
- Engine wiring passes `Command` and `Args` fields through to health checker on both startup and hot-reload
- Updated OpenAPI spec (`docs/api/openapi.yaml`): added SSE `/events/stream` endpoint, fixed Go version example
- Updated `docs/configuration.md` to document `grpc` and `exec` health check types with template variable reference
- Updated `docs/migration-guide.md` with expanded examples: NGINX (weighted backends, gzip, basic auth, HTTPS redirect, virtual hosts, timeouts), HAProxy (ACL routing, map files, connection limits, circuit breaker), Traefik (label translation, middleware chains, path prefix routing), Envoy (retries/timeouts, weighted cluster traffic splitting), and detailed migration checklist with algorithm mapping table
- Fixed README link to API reference (now points to OpenAPI spec)

## [0.1.0] - 2026-04-11

### Added
- L4/L7 proxy with HTTP/HTTPS, WebSocket, gRPC, gRPC-Web, SSE, TCP, UDP, SNI routing, PROXY protocol v1/v2 support
- 16 load balancing algorithms (round_robin, weighted_round_robin, least_connections, weighted_least_connections, least_response_time, weighted_least_response_time, ip_hash, consistent_hash, maglev, ring_hash, power_of_two, random, weighted_random, rendezvous_hash, peak_ewma, sticky_sessions)
- Request-context aware balancer interface with all 16 algorithm implementations
- 6-layer WAF with SQLi, XSS, CMDi, path traversal, XXE, SSRF detection
- Bot detection with JA3 fingerprinting
- TLS/mTLS with ACME/Let's Encrypt and OCSP stapling support
- Clustering with Raft consensus and SWIM gossip
- MCP server for AI integration (17 tools)
- Admin REST API with Web UI dashboard
- SSE real-time event streaming (`/api/v1/events/stream`) with auto-reconnect Web UI hook
- CSRF protection for admin API
- Circuit breaker for admin API backend calls
- Hot config reload (YAML/JSON/TOML/HCL) with automatic rollback grace period
- Service discovery (Static/DNS/Consul/Docker/File)
- Exec health checks (external command-based health checking)
- 30+ middleware components (rate_limit, cors, compression, auth, cache, circuit_breaker, timeout, ip_filter, trace, coalesce, rewrite, max_body_size, hmac, apikey, etc.)
- Prometheus metrics with sharded counters for high-concurrency performance
- Connection pooling with Prometheus gauges for idle/active/hits/misses/evictions
- Plugin system with event bus
- Geo-DNS routing with country/region-based traffic routing
- Intelligent request shadowing/mirroring (percentage-based, header-matched, time-windowed)
- Admin API security headers via secureheaders middleware
- Distributed tracing with W3C Trace Context, B3, and Jaeger propagation
- Production deployment guide with Kubernetes, Docker, systemd examples
- Troubleshooting playbook with diagnostics and emergency procedures
- Enhanced Helm charts with HPA, PDB, ServiceMonitor, NetworkPolicy
- Grafana dashboard with 20+ monitoring panels and import guide
- Performance tuning guide for high RPS, high concurrency, and high bandwidth workloads
- SECURITY.md with vulnerability reporting policy
- OpenAPI 3.0 spec for Admin API
- Architecture Decision Records (8 ADRs)
- Migration guide from NGINX, HAProxy, Traefik, Envoy, AWS ALB
- Getting started tutorial
- Terraform AWS module
- Prometheus alerting rules
- Docker Compose full stack
- GoReleaser with multi-arch Docker, Homebrew, nFPM, Helm, SBOM
- Benchstat performance regression tracking in CI
- Code of Conduct with enforcement ladder and appeals process
- CONTRIBUTING.md with examples for extending OLB
- Web UI: React 19 + TypeScript + Vite + Tailwind CSS
- Web UI: WCAG 2.1 AA accessibility, mobile responsiveness, lazy-loaded routes

### Changed
- Binary size is ~13 MB (statically linked, CGO_ENABLED=0)
- 3 Go dependencies: golang.org/x/crypto, golang.org/x/net, golang.org/x/text
- Test coverage: 95.3% across 67+ packages
- Code structure: engine.go, gossip.go, advanced_commands.go, toml.go split into focused files

### Fixed
- Health checker double-close panic (sync.Once guard on Stop)
- IPv6 host parsing — net.SplitHostPort replaces string splitting in 9 locations
- Backend.GetURL() scheme defaults — configurable http/https per backend
- Passive health checker wired to pool manager state updates
- Admin server health checker wired to engine lifecycle

[0.1.0]: https://github.com/openloadbalancer/olb/releases/tag/v0.1.0
