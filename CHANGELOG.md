# Changelog

All notable changes to OpenLoadBalancer will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- An enabled middleware whose config is invalid now stops the engine from starting (and a reload is rejected) instead of being dropped from the chain. Previously a misconfigured auth, IP filter, WAF, rate limit, CSRF, CORS or CSP block silently disabled that protection — e.g. `basic_auth` without `hash` served the backend unauthenticated (#71)
- Documented defaults are now applied: `basic_auth`/`api_key` `hash: sha256`, JWT `algorithm: HS256`, `header: Authorization`, `prefix: "Bearer "` — configs relying on them had authentication disabled (#71)
- Rewrite middleware no longer produces protocol-relative `Location` headers (`//evil.example`): path-capturing redirect rules, including the built-in `remove_trailing_slash`, were open redirects (#68)
- `olb setup` writes the generated config (which contains the admin password) as 0600; Raft state, log entries and snapshots are 0600 in 0700 directories (#68)
- Cluster TLS checks the allowed node list in `VerifyConnection`, so it also applies to resumed sessions (#68)
- MCP SSE logs the client session ID with `%q` (log injection) (#68)
- MCP and profiling servers set `ReadHeaderTimeout` (Slowloris) (#62)
- Shared cache never stores or replays `Set-Cookie` and keys on `Authorization`; request coalescing no longer shares in-flight responses across users; real IP honours forwarded headers only from trusted proxies; admin auth lockout keys on the peer address by default (#61)
- Cluster transport authenticates before accepting a connection (#61)
- Dependencies: x/net v0.61.0, x/text v0.43.0, x/crypto v0.58.0 (govulncheck: 0 reachable); all WebUI and website npm packages updated, zod 4 (GHSA-mrhx-c7xc-m3w5), `npm audit` clean (#61, #64)

### Added
- `pools[].discovery` feeds a pool from a discovery provider (static, file, dns, consul, docker); backends are added/removed as services appear and disappear, also across config reloads
- `tls.acme` now obtains and renews certificates (ACME HTTP-01 on port 80, account/certificate storage in `tls.acme.storage_dir`, `directory_url` for staging). Previously it only created a client and never requested a certificate
- `admin.enabled: false`, `metrics.enabled`/`metrics.path`, `waf.detection.enabled`, per-layer `waf.detection.mode`/`waf.bot_detection.mode`, `waf.detection.exclusions[].condition`, `behavior.error_rate_threshold`, `permissions_policy` and `ip_acl.auto_ban.default_ttl` are now honored

### Changed
- Config validation rejects `middleware.bot_detection` action `throttle` (global, `user_agent_rules`, `custom_headers`); it only tagged requests and never limited them.
- `routes[].middleware` is rejected at load time instead of being silently ignored

### Fixed
- Data race between accepting TCP connections and shutting the L4 proxy down
- Round robin, IP hash and least-connections tie-breaking picked backends from a randomly ordered slice: round robin was effectively random and IP hash did not keep a client on one backend (#67)
- `/metrics` stopped updating `http_*` and `waf_*` series after a config reload (#70)
- Distributed rate limiter banned on the first violation, ignoring `AutoBanAfter`; violation maps are now bounded (#69)
- `ParsePort` accepted out-of-range values by wrapping (`"70000"` → 4464) (#69)
- Raft log compaction watermark and snapshot metadata after compaction (#61)
- SSE and gRPC streams were truncated, and responses already sent to the client could be retried (#61)
- L4 TCP accept loop stopped after one transient error such as EMFILE (#61)
- WebSocket proxy sent the forwarded handshake without `Connection: Upgrade` (#61)
- Router accepted conflicting parameter names and routes below a wildcard (#61)
- Rendezvous hash key collapse and weight bias (#61)
- Admin-drained backends were put back into rotation by the health checker (#61)
- A panicking plugin event handler crashed `EventBus.Publish` (#61)
- CSRF consumed the request body when reading the form token (#61)
- SSE `MaxEventSize` was not enforced on lines shorter than the read buffer (#61)
- Request ID fallback IDs could collide within one clock tick (#61)
- Durations such as `1d500ms` failed to parse (#62)
- `.dockerignore` did not exclude `node_modules`, so host-installed packages leaked into the image build (#66)

### Changed
- CI no longer ignores failures: lint (golangci-lint v2, which actually runs on Go 1.26+), frontend build and tests, and all macOS packages are enforced (#62)
- Go toolchain 1.27.3 (minimum `go 1.26.2`), read by CI from `go.mod`; GitHub Actions, gosec, govulncheck, Node 24 and Docker base images on their latest releases; Dependabot also tracks the website and Docker images (#63, #66)
- golangci-lint findings reduced from 569 to ~470 (#65, #69, #70)

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
- Test coverage for `internal/engine` increased to 90.2% (from 87.8%)
- Test coverage for `internal/plugin` increased to 93.8% (from 85.2%)
- Frontend integration tests: 10 tests covering API mutations (POST/PATCH/DELETE), error handling (400/404/500), network failure, toast notifications, error boundary rendering
- Marketing website tests: 13 smoke tests covering App, Header, Hero, Footer components with vitest + @testing-library/react
- Memory profiling tests: 5 benchmarks in `test/benchmark/memory_test.go` — idle conn <4KB, active req <32KB, no goroutine leaks
- Connection pool effectiveness tests: 5 tests in `test/benchmark/pool_test.go` — serial hit rate 99.9%, concurrent hit rate 99.6%
- Full benchmark suite validated: balancer 3.7-147 ns/op, router 185-982 ns/op, metrics 0 allocs
- TCP sustained throughput benchmark: 1010 MB/s (~8 Gbps) on Windows; splice(2) zero-copy on Linux via `io.CopyBuffer`/`ReadFrom`
- Raft chaos testing framework: 8 tests in `test/chaos/raft_chaos_test.go` — leader election, failover, quorum loss, rapid writes
- Raft election timer: now uses `ElectionTick` config with 1x-3x randomization range (was hardcoded 150-300ms); split vote tiebreaker for deterministic leader selection; state check prevents stepped-down candidates from becoming leader
- CI test runs use `-short` flag to skip timing-sensitive Raft failover tests
- Docker image security scan: trivy config, Makefile targets, CI job with SARIF upload
- Startup time benchmark: 85-259ms cold start (well under 500ms target)
- Focus trap for mobile sidebar in Web UI
- E2E smoke test script (`scripts/smoke-test.sh`): validates complete binary lifecycle — proxy traffic, admin API, Web UI, config reload, graceful shutdown
- Release workflow fixes: corrected `download-artifact@v8` to `@v4`, removed duplicate release job from CI (release.yml handles full publishing including GHCR)

### Changed
- CORS middleware: `NewCORSMiddleware` returns `(*CORSMiddleware, error)` instead of panicking on invalid config
- Logger: `os.Exit(1)` replaced with configurable `ExitFunc` field for graceful shutdown and testability
- Logging middleware: `fmt.Println` replaced with configurable `LogFunc` field, engine wires structured logger
- Bot detection and OAuth2 middleware: manual JSON string concatenation replaced with `json.Marshal` for safety
- WAF middleware, WAF rate limiter, admin rate limiter, bot detection: all HTTP response JSON now uses `json.Marshal` instead of hardcoded string literals
- All 20 background goroutines now have `recover()` crash protection to prevent single-connection panics from crashing the process:
  - Externally-facing: SNI proxy, TCP proxy, timeout middleware, shadow requests, admin circuit breaker, cache revalidation, SSE stream handlers
  - Internal: WAF cleanup, auth cleanup, admin rate limiter cleanup, DNS resolver, engine config reload, cluster election/replication/compaction, gossip relay, discovery providers, config watcher, coalesce cleanup, HTTP/HTTPS/mTLS listeners, profiling server
  - All `recover()` guards now log the panic value with component prefix (e.g. `[shadow]`, `[sse]`, `[raft]`) for observability
- Removed duplicate signal handler registration between CLI and engine — prevents race condition on Ctrl+C where two concurrent `Shutdown()` calls competed
- Engine `Shutdown()` now waits for in-flight shadow requests to complete via `shadowMgr.Wait()`
- Admin API and cluster management JSON response writers now log `json.Encoder` errors instead of silently discarding them
- Flaky `TestUDPProxy_ReceiveFromBackend_ForwardsData` test: replaced `time.Sleep(10ms)` synchronization with polling loop for reliability under load
- Removed unsupported `query_param` field from example `configs/olb.yaml` API key middleware section
- Split `internal/admin/handlers.go` (981 LOC) into 5 focused files: `handler_helpers.go`, `handlers_system.go`, `handlers_pools.go`, `handlers_backends.go`, `handlers_readonly.go`
- Split `internal/config/hcl/hcl.go` (1415 LOC) into `lexer.go`, `parser.go`, `decoder.go`
- Fixed `.golangci.yml` Go version: `1.25` → `1.26` to match `go.mod`
- Optimized per-request allocations in L7 proxy hot path: merged two `context.WithValue` into single struct, stack-allocated `attemptedBackends` array, skip backend filtering on first attempt, canonical hop-by-hop header lookup to avoid `strings.ToLower` per header
- Wired splice(2) zero-copy into L4 TCP proxy: `copyWithTimeout` and `copyWithBuffer` now use `io.CopyBuffer` which auto-splices on Linux via `net.TCPConn.ReadFrom`; removed dead custom splice code; idle timeout semantics preserved with deadline-refresh loop
- Optimized middleware per-request allocations: pre-computed `FormatFloat` and `strconv.Itoa` in rate limiter headers, pre-computed `strings.Join` for CORS static config slices, pooled `headerResponseWriter` struct in headers middleware
- Exec health checks now support template variables (`{{.Address}}`, `{{.Host}}`, `{{.Port}}`) in both command and args
- Exec health check `command` and `args` fields are now configurable from YAML/JSON/TOML/HCL configs
- Engine wiring passes `Command` and `Args` fields through to health checker on both startup and hot-reload
- Updated OpenAPI spec (`docs/api/openapi.yaml`): added SSE `/events/stream` endpoint, fixed Go version example
- Updated `docs/configuration.md` to document `grpc` and `exec` health check types with template variable reference
- Updated `docs/migration-guide.md` with expanded examples: NGINX (weighted backends, gzip, basic auth, HTTPS redirect, virtual hosts, timeouts), HAProxy (ACL routing, map files, connection limits, circuit breaker), Traefik (label translation, middleware chains, path prefix routing), Envoy (retries/timeouts, weighted cluster traffic splitting), and detailed migration checklist with algorithm mapping table
- Removed unused frontend dependencies (`recharts`, `@tanstack/react-query`)
- Eliminated all `any` types from WebUI source code for type safety

### Fixed
- **Security audit remediation (97 findings across multiple passes)**:
  - Race conditions in connection pooling, health checking, and cluster state management
  - Resource exhaustion: unbounded I/O, buffer limits, goroutine leaks, connection limits
  - XFF trust model hardened with explicit trusted proxy configuration
  - Shadow body restore for proper request body replay
  - Cluster replay protection for Raft consensus
  - bcrypt as default password hashing algorithm
  - Plugin allowlist for security
  - Connection pool goroutine lifecycle management
  - JWT expiration default enforcement
  - Raft payload size limits
  - MCP error sanitization to prevent information leakage
  - PROXY protocol port validation
  - Dial timeouts and buffer limits on all network operations
  - Compression buffer limits to prevent memory exhaustion
- Race condition in `HTTPProxy.cachedHandler` — converted from bare `http.Handler` field to `atomic.Value` for safe concurrent `ServeHTTP` + `RebuildHandler` access
- Bug in `useQuery` hook: non-transient errors (401, 404, 500) were incorrectly retried — added `break` to stop retry loop for non-transient errors
- Accessibility: logs page auto-scroll switch and level filter missing `aria-label`, middleware filter buttons missing `aria-pressed`, logs table missing `<caption>`
- Fixed README link to API reference (now points to OpenAPI spec)
- E2E test stabilization: port TOCTOU elimination, polling helpers, CI-compatible timeouts
- Admin API rate limiter responses now include `Content-Type: application/json` header
- JWT middleware `unauthorized()` response now uses correct `Content-Type: application/json` instead of `text/plain`
- Admin API backend creation now handles `json.Marshal` errors instead of proposing nil to Raft
- Raft RPC goroutines (RequestVote, AppendEntries heartbeat, replication) now have `recover()` crash protection
- Engine lifecycle goroutines (admin server, system metrics, discovery cancel, WaitGroup drain) now have `recover()` crash protection
- Signal handler goroutines (Unix and Windows) now have `recover()` crash protection
- HTTP/2 server goroutine now has `recover()` crash protection
- TCP proxy drain goroutine now has `recover()` crash protection
- Access log JSON fields (path, query, host, request_id, client_ip) now use `escapeJSON()` to prevent log injection from attacker-controlled input
- WAF middleware recovery path now sets `Content-Type: application/json` header
- Basic auth middleware unauthorized response now uses correct `Content-Type: application/json`
- Middleware rate limiter response now uses consistent JSON format with `Content-Type: application/json`
- Discovery event forwarder goroutines now have `recover()` crash protection with panic logging
- SSE readLineWithTimeout goroutine now has `recover()` crash protection
- TUI fetcher goroutines (FetchSystemInfo, FetchBackends, FetchRoutes, FetchHealth) now have `recover()` crash protection

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
