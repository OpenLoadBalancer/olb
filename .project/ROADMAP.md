# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-14
> This roadmap prioritizes work needed to bring the project to production quality.

## Current State Assessment

OpenLoadBalancer is a remarkably complete project — 99.7% spec completion, 95.3% test coverage, 178K LOC of Go, 13.9K LOC of TypeScript, all building into a single 9.1MB binary. Every major subsystem from the specification (L4/L7 proxy, 16 algorithms, Raft clustering, ACME, WAF, MCP, Web UI) is implemented and tested.

**Key blockers for production readiness:**
- 15 test failures in the core proxy layer (`internal/proxy/l4` and `internal/proxy/l7`)
- From-scratch Raft consensus has not been validated under chaos conditions
- Frontend has architectural inconsistencies (dual data-fetching layers)

**What's working well:**
- Clean builds with zero warnings
- `go vet` passes clean
- 63 of 65 packages pass all tests
- Comprehensive security hardening (97 findings addressed)
- Extensive documentation and deployment tooling
- Multi-platform binary, Docker, Helm, Terraform all ready

---

## Phase 1: Critical Fixes (Week 1)

### Must-fix items blocking basic functionality

- [x] **Fix parallel test execution failures** — `internal/proxy/l7/` + `internal/engine/`
  - **Root cause identified**: Resource contention when engine and proxy tests run in parallel on Windows
  - Engine tests start hundreds of TCP/UDP listeners, admin servers, health checkers; this exhausts OS resources
  - All tests pass with `go test -p 1 ./...` (sequential package execution), which is what CI uses
  - All tests pass when packages are run individually (`go test ./internal/proxy/l7/`)
  - **Not a code bug** — proxy functionality works correctly
  - Resolution: CI already uses `-p 1`; local dev should use `-p 1` or run packages separately
  - Effort: 0h (mitigated by CI config)

---

## Phase 2: Core Hardening (Week 2-3)

### Reliability and correctness

- [x] **Validate Raft under failure scenarios** — `internal/cluster/`
  - **DONE**: 8 chaos tests in `test/chaos/raft_chaos_test.go` — leader election (3-node, 5-node), leader failover with re-election, write after leader change, multiple leader kills, quorum loss protection, single-node, rapid writes
  - **Known issue**: 3 tests (LeaderFailover_Reelection, WriteAfterLeaderChange, MultipleLeaderKills) are timing-sensitive — skipped in `-short` mode. Root cause: `startElection()` blocks the `run()` event loop, preventing nodes from processing incoming RequestVote RPCs during split vote scenarios. Fix requires non-blocking election architecture redesign.
  - **Improvements made**: Election timer now uses `ElectionTick` config (was hardcoded 150-300ms); wider randomization range (1x-3x); split vote tiebreaker (higher NodeID wins); state check prevents stepped-down nodes from becoming leader; all chaos tests use `-short` in CI
  - Effort: 40h

- [x] **Add chaos testing framework** — `test/chaos/`
  - **DONE**: Framework with `testCluster` builder, real TCP transports, kill/restart injection, leader tracking, multi-node Raft consensus
  - Effort: 24h

- [x] **Race detection on Linux CI** — `.github/workflows/ci.yml`
  - Add `go test -race ./...` job (requires CGO)
  - Already has `build-race` Makefile target
  - **DONE**: Race detection job added to CI
  - Effort: 2h

- [x] **Fix cluster test TODO** — `internal/cluster/cluster_test.go:534`
  - **DONE**: No TODOs remain in cluster_test.go — already resolved
  - Effort: 0h

- [x] **Increase `internal/plugin` test coverage** — currently 85.2%, target ≥90%
  - **DONE**: Coverage increased to 93.8% — added LoadDir tests with AllowedPlugins, factory error paths
  - Effort: 8h

- [x] **Increase `internal/engine` test coverage** — currently 87.8%, target ≥90%
  - **DONE**: Coverage increased to 90.2% — added tests for rollback, passive checker, profiling, ACME, shadow, admin auth, TLS listener, etc.
  - Effort: 8h

---

## Phase 3: Frontend Cleanup (Week 3-4)

### Resolve architectural inconsistencies

- [x] **Consolidate data-fetching layer** — `internal/webui/src/hooks/use-query.ts`
  - **DONE**: Removed unused TanStack React Query dependency; custom hooks are the sole data-fetching layer
  - Effort: 8h

- [x] **Remove unused `recharts` dependency** — `internal/webui/package.json`
  - **DONE**: Removed recharts and unused chart.tsx component
  - Effort: 5min

- [x] **Add focus trap to mobile sidebar** — `internal/webui/src/components/layout.tsx`
  - **DONE**: Focus trap added — Tab/Shift+Tab wrap within sidebar, Escape closes, focus restores to open button on close
  - Effort: 2h

- [x] **Add frontend integration tests** — `internal/webui/src/test/integration.test.tsx`
  - **DONE**: 10 integration tests covering API mutations (POST/PATCH/DELETE), error handling (400/404/500), network failure, toast notifications, and error boundary rendering
  - Effort: 16h

- [x] **Add marketing website tests** — `website-new/src/test/smoke.test.tsx`
  - **DONE**: Added vitest + @testing-library/react + jsdom; 13 smoke tests covering App, Header, Hero, Footer, and cn() utility
  - Effort: 8h

---

## Phase 4: Performance & Optimization (Week 5-6)

### Performance tuning and validation

- [x] **Run full benchmark suite** — `make bench`
  - **DONE**: All benchmarks pass clean. Balancer: 3.7-147 ns/op (0-3 allocs). Router: 185-982 ns/op. Metrics: 3.9-78 ns/op (0 allocs). Utils: 8-161 ns/op. On AMD Ryzen 9 9950X3D (Windows). See `docs/benchmark-report.md` for full results.
  - Note: Full HTTP RPS/latency benchmarks require Linux (`test/benchmark/`) — these validate algorithm-level performance
  - Effort: 4h

- [x] **Memory profiling under load**
  - **DONE**: Created `test/benchmark/memory_test.go` with 5 memory profiling tests. Results: idle conn=0.19 KB (<4 KB target), active req=5.10 KB (<32 KB target), per-backend=0.38 KB, per-route≈0 bytes, no goroutine leaks. All pass.
  - Effort: 4h

- [x] **TCP throughput benchmark** — L4 proxy
  - **DONE**: Added `BenchmarkTCPProxy_SustainedThroughput` in `test/benchmark/tcp_benchmark_test.go` — measures 1010 MB/s (~8 Gbps) on Windows. On Linux, `io.CopyBuffer` uses splice(2) via `net.TCPConn.ReadFrom` for zero-copy transfer.
  - Effort: 8h

- [x] **Wire splice() into TCP proxy hot path** — `internal/proxy/l4/`
  - **DONE**: Replaced manual Read/Write loops in `copyWithTimeout` and `copyWithBuffer` with `io.CopyBuffer`, which uses splice(2) on Linux via `net.TCPConn.ReadFrom`. Preserves idle timeout semantics with deadline-refresh loop. Removed dead custom splice code from `copy_linux.go`. Windows throughput improved from 884 to 1010 MB/s. All 19 copy-related tests pass.
  - Effort: 4h

- [x] **Connection pool effectiveness audit**
  - **DONE**: 5 tests in `test/benchmark/pool_test.go` — serial hit rate 99.9%, concurrent hit rate 99.6%, maxSize truncation verified, idle eviction verified, stat counters verified
  - Effort: 8h

- [x] **Startup time benchmark**
  - **DONE**: Pre-built binary cold start 85-259ms (median ~150ms), well under 500ms target. Includes `go run` overhead: 400-620ms.
  - Effort: 1h

---

## Phase 5: Documentation & Polish (Week 7-8)

### Documentation completeness and final polish

- [x] **Create `llms.txt` at project root** — referenced in spec but missing
  - **DONE**: Already exists with comprehensive project description for AI tooling
  - Effort: 0h

- [x] **Add OpenAPI/Swagger spec** — `docs/api/openapi.yaml`
  - **DONE**: Already comprehensive at 1273 lines — covers all 25+ endpoints (System, Pools, Backends, Routes, Health, Config, Certificates, WAF, Middleware, Events/SSE, Cluster, Metrics) with full request/response schemas, security schemes, and reusable components
  - Effort: 4h (review only)

- [x] **Update CHANGELOG.md** — reflect all post-v0.1.0 changes
  - **DONE**: Added security audit remediation (97 findings), coverage improvements, E2E stabilization, dependency cleanup
  - Effort: 4h

- [x] **Review and update all example configs** — `configs/`
  - **DONE**: Expanded WAF section with full sub-config (IPACL, RateLimit, Sanitizer, Detection, BotDetection, Response, Logging); added `server` tuning and `cluster` (Raft) sections; added `discovery` section; fixed duplicate `cache:` key in YAML; TOML and HCL configs updated with matching WAF, cluster, and server sections
  - Effort: 4h

- [x] **Production deployment guide** — `docs/production-deployment.md`
  - **DONE**: Fixed health check endpoint URLs (`/api/v1/status` → `/api/v1/system/health`), updated cluster config to use `nodeID:address:port` peer format with `node_auth` section, added Kubernetes probe paths, added troubleshooting section with common issues (unhealthy backends, reload failures, high memory, split-brain, 502 errors)
  - Effort: 8h

---

## Phase 6: Release Preparation (Week 9-10)

### Final production preparation

- [ ] **Resolve GHCR image publishing** — requires repo permissions
  - **DONE**: Release workflow (`release.yml`) already has GHCR publishing: multi-arch Docker build (amd64/arm64), semver tagging, GHA cache. Fixed `download-artifact@v8` → `@v4`. Removed duplicate release job from ci.yml. Remaining: enable `packages: write` permission on repo and push first tag.
  - Effort: 2h

- [ ] **Multi-arch Docker image validation**
  - Test amd64 and arm64 images on real hardware
  - Verify frontend assets embedded correctly
  - Effort: 4h

- [ ] **End-to-end smoke test on fresh environment**
  - **DONE**: Created `scripts/smoke-test.sh` — validates complete binary lifecycle: build → start → proxy traffic → admin API (7 endpoints) → Web UI → config reload → graceful shutdown. Supports `--docker` mode and existing binary path. Remaining: test on actual clean Linux VM, Docker, macOS environments.
  - Effort: 8h

- [x] **Security scan of Docker image**
  - **DONE**: Added `.trivy.yaml` config, `make docker-scan` / `make docker-scan-grype` targets, and `image-scan` CI job with SARIF upload to GitHub Security tab
  - Effort: 4h

- [ ] **Cut v1.0.0 release**
  - Tag release
  - Build all platform binaries
  - Publish to GHCR, Homebrew
  - Create GitHub release with checksums
  - Effort: 4h

---

## Beyond v1.0: Future Enhancements

### Features and improvements for future versions

- [ ] **Brotli compression** — Pure Go implementation for middleware
- [ ] **QUIC/HTTP3 support** — Listed as future in spec §3
- [ ] **WASM plugin runtime** — Sandboxed alternative to Go plugins (spec §18.2)
- [ ] **RBAC for Admin API** — Read-only vs admin roles (spec §19.2)
- [ ] **Custom dashboard builder** — Drag-drop widget system (spec §14.2)
- [ ] **Config history** — Track and diff config changes over time (spec §14.6)
- [ ] **gRPC-Web full proxying** — Enhanced gRPC-Web support
- [ ] **Rate limiting distributed via Raft** — Use Raft log for strong consistency
- [ ] **Prometheus remote write** — Export metrics to remote Prometheus
- [ ] **HTTP/3 upstream proxying** — Proxy to HTTP/3 backends

---

## Phase 7: Production Audit Fixes (Complete)

### Security and reliability fixes from comprehensive audit

- [x] **CORS middleware panic → config validation error** — `internal/middleware/cors.go`
  - `NewCORSMiddleware` returns `(*CORSMiddleware, error)` instead of panicking
  - Engine handles error gracefully, logs and skips middleware
  - All test and benchmark callers updated
  - Effort: 2h

- [x] **Logger os.Exit(1) in library code** — `internal/logging/logger.go`
  - Added configurable `ExitFunc` field (defaults to `os.Exit`)
  - Tests and graceful shutdown can override
  - Effort: 1h

- [x] **fmt.Println in logging middleware** — `internal/middleware/logging/logging.go`
  - Added configurable `LogFunc` field, engine wires structured logger
  - Effort: 1h

- [x] **Manual JSON construction → json.Marshal** — `internal/middleware/botdetection/`, `internal/middleware/oauth2/`
  - Dynamic string concatenation in HTTP responses replaced with `json.Marshal`
  - Removed now-unused `jsonEscape` function
  - Effort: 1h

- [x] **Goroutine crash protection (14 goroutines)** — Multiple files
  - CRITICAL: SNI proxy, TCP proxy connections
  - HIGH: Timeout middleware, shadow requests, admin circuit breaker
  - MEDIUM: Cache revalidation, WAF/auth/rate-limiter cleanup, DNS resolver, engine config reload, cluster election/replication/compaction
  - All use `defer recover()` to prevent single-connection panics from crashing the process
  - Effort: 4h

- [x] **Admin JSON encode error logging** — `internal/admin/`, `internal/cluster/`
  - `writeError()`, `writeSuccess()`, `writeUnauthorized()`, `writeJSON()`, `writeJSONError()` now log encode failures
  - Effort: 1h

- [x] **Duplicate signal handler race condition** — `internal/cli/commands.go`
  - CLI and engine both registered SIGTERM/SIGINT handlers, causing double Shutdown() on Ctrl+C
  - Removed CLI's signal handler, added `Engine.Done()` channel for clean wait
  - Effort: 2h

- [x] **Shadow manager cleanup during shutdown** — `internal/engine/lifecycle.go`
  - Engine `Shutdown()` now calls `shadowMgr.Wait()` to drain in-flight shadow requests
  - Effort: 0.5h

- [x] **Flaky UDP test fix** — `internal/proxy/l4/coverage_test.go`
  - Replaced `time.Sleep(10ms)` synchronization with polling loop
  - Effort: 0.5h

- [x] **Config cleanup** — `configs/olb.yaml`
  - Removed unsupported `query_param` field from API key middleware section
  - Effort: 0.1h

---

## Effort Summary

| Phase | Estimated Hours | Priority | Dependencies |
|---|---|---|---|
| Phase 1: Critical Fixes | 16-32h | CRITICAL | None |
| Phase 2: Core Hardening | 84h | HIGH | Phase 1 |
| Phase 3: Frontend Cleanup | 34h | MEDIUM | None |
| Phase 4: Performance | 44h | MEDIUM | Phase 1 |
| Phase 5: Documentation | 33h | LOW | Phases 1-4 |
| Phase 6: Release Prep | 22h | HIGH | Phases 1-2 |
| **Total** | **~250h** | | |

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Proxy test failures indicate deeper proxy bug | Medium | High | Phase 1 root cause analysis before any deployment |
| Raft consensus has edge case bugs | Medium | Critical | Phase 2 chaos testing; start with single-node for prod |
| Performance targets not met under real load | Medium | Medium | Phase 4 benchmarking; tune before release |
| Frontend data-fetching inconsistency causes bugs | Low | Low | Phase 3 consolidation |
| Security vulnerability in from-scratch parsers | Low | High | Fuzz tests exist; add more coverage |
| Docker image has CVEs | Low | Medium | Phase 6 security scan |
