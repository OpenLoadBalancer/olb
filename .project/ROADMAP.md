# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-08
> This roadmap prioritizes work needed to bring the project to production quality.

## Current State Assessment

OpenLoadBalancer is a remarkably complete load balancer implementation with 97% spec feature completion, 93.4% test coverage, and genuine zero-dependency Go code. The project self-reports as "Production Ready" and most core features are fully functional. However, several issues prevent true production readiness:

**Key blockers:**
1. ~~Duplicate middleware v1/v2 wired in engine~~ -- FIXED (v1 duplicates removed)
2. ~~Raft state machine is a stub~~ -- FIXED (Apply/Snapshot/Restore implemented, Raft-integrated config replication, persistence, admin Raft-aware CRUD)
3. WebUI pages use mock data -- dashboard cannot manage a real deployment

**What's working well:**
- L4/L7 proxying with all protocol handlers (HTTP, WebSocket, gRPC, SSE, HTTP/2, TCP, UDP, SNI)
- 16 balancer algorithms, all tested and functional
- 6-layer WAF with real detection engines
- Admin REST API with 20+ endpoints
- 93.4% test coverage with all 67 packages passing
- Comprehensive CI/CD pipeline
- Excellent documentation (7,000+ lines across 25+ docs)

---

## Phase 1: Critical Fixes (Week 1-2)

### Must-fix items blocking basic functionality

- [x] **Remove duplicate middleware registration** -- `internal/engine/engine.go` `createMiddlewareChain()` wires both v1 (root middleware/) and v2 (subdirectory middleware/cache/, middleware/metrics/, etc.) versions of Cache, Metrics, RealIP, RequestID. Remove v1 registrations. ~2h
- [x] **Implement Raft state machine Apply/Snapshot** -- ConfigStateMachine with full command handling (set_config, update_backend, delete_backend, update_route, update_listener, WAF commands), Snapshot/Restore, file-based persistence, crash recovery, Raft-aware admin API endpoints. ~40-80h
- [x] **Connect WebUI to real API** -- Replace mock data in 10/11 React pages (`internal/webui/src/pages/`) with actual API calls using existing `useQuery`/`useMutation` hooks. ~40h
- [x] **Remove webui.old/ directory** -- Delete `internal/webui.old/` from the repo. ~1h
- [x] **Clean up stray root files** -- Remove `simple.yaml`, `test_backend.go`, `cover.out.bin`, `cli.cov` from project root. ~15min
- [x] **Remove duplicate Helm chart** -- Keep `helm/olb/`, remove `deploy/helm/olb/`, update any references. ~1h

**Estimated effort:** 85-125 hours

---

## Phase 2: Core Completion (Week 3-6)

### Complete missing core features from specification

- [x] **Implement gRPC-Web support** -- `internal/proxy/l7/grpc.go` currently delegates gRPC-Web to the standard gRPC handler. Implement proper application/grpc-web protocol with base64 framing. ~20-40h
- [x] **Implement intelligent request shadowing** -- `internal/proxy/l7/shadow.go` `ShouldShadow()` always returns true. Add percentage-based shadowing, header-based matching, and time-window controls. ~8h
- [x] **Make hardcoded defaults configurable** -- Move `MaxConnections: 10000`, `MaxPerSource: 100`, `ProxyTimeout: 60s`, `DialTimeout: 10s`, `MaxIdleConns: 100`, `MaxIdleConnsPerHost: 10` from engine.go/proxy.go to config with sensible defaults. ~4h
- [x] **Add CSRF protection to admin API** -- State-changing endpoints (POST/PATCH/DELETE) lack CSRF tokens. Add CSRF middleware to admin server for browser-based access. ~4h
- [x] **Make WebSocket InsecureSkipVerify configurable** -- `internal/proxy/l7/websocket.go` hardcodes `InsecureSkipVerify: true` for backend TLS. Make it configurable per-backend. ~2h
- [ ] **Add distributed rate limiting support** -- The spec mentions Redis-backed distributed rate limiting. The current implementation is memory-based only. Add optional Redis backend. ~20-30h
- [x] **Implement GeoIP database loading** -- `internal/geodns/geodns.go` uses simplified IP-to-location. Add MaxMind GeoLite2 database loading support. ~16h

**Estimated effort:** 74-104 hours

---

## Phase 3: Hardening (Week 7-8)

### Security, error handling, edge cases

- [x] **Admin auth rate limit hardening** -- Increase from 30/min to configurable limit, add progressive backoff, add account lockout after N failures. ~4h
- [x] **Add request body logging opt-in** -- SECURITY.md notes "request body logging may capture sensitive data". Make body logging explicitly opt-in with per-route control. ~4h
- [x] **Refactor engine.go** -- Break `createMiddlewareChain()` (~800 LOC) into per-middleware registration functions. Break `gossip.go` (1,715 LOC) into logical files. ~8h
- [x] **Add pprof memory profiling to benchmarks** -- Include heap and CPU profiles in benchmark runs to identify allocation hotspots. ~4h
- [x] **Validate config on startup comprehensively** -- Add validation for all middleware config types, catch conflicting settings early. ~8h
- [x] **Add circuit breaker to admin API backend calls** -- Admin API currently has no protection against cascading failures when calling engine internals. ~4h
- [x] **WebSocket connection limit enforcement** -- Add configurable max concurrent WebSocket connections per listener. ~2h
- [x] **Add HTTP/2 strict mode** -- Enforce HTTP/2 connection preface limits, HPACK decoder size limits, and settings frame flood protection. ~8h

**Estimated effort:** 42 hours

---

## Phase 4: Testing (Week 9-10)

### Comprehensive test coverage

- [ ] **Add React component tests** -- Set up Vitest + React Testing Library for WebUI. Write tests for all 11 pages and shared components. ~20h
- [x] **Add load test suite** -- Create automated load tests using vegeta or custom Go HTTP clients. Test at 10K, 50K, 100K concurrent connections. ~16h
- [x] **Add chaos testing** -- Test behavior under: backend failures during request, config reload during traffic, cluster leader election during traffic, OOM conditions. ~16h
- [x] **Add fuzzing tests** -- Add Go native fuzz tests (`func FuzzXxx`) for: HTTP request parsing, SNI ClientHello parsing, WAF detection engines, config parsing. ~16h
- [ ] **Add end-to-end cluster tests** -- Test 3-node Raft cluster: leader failover, config replication, join/leave, split-brain recovery. ~16h
- [x] **Add TLS integration tests** -- Test mTLS handshake, certificate rotation, OCSP stapling, SNI routing with real TLS. ~8h
- [x] **Test coverage enforcement per-package** -- Add CI check for minimum 85% per-package (not just average). ~2h

**Estimated effort:** 94 hours

---

## Phase 5: Performance & Optimization (Week 11-12)

### Performance tuning and optimization

- [x] **Profile and optimize WAF pipeline** -- Benchmark the 6-layer WAF pipeline end-to-end. Target <1ms p99 as specified. Optimize regex compilation and matching. ~16h
- [x] **Optimize HTTP transport pool settings** -- Tune `MaxIdleConns`, `MaxIdleConnsPerHost`, idle timeout for production workloads. Make all configurable. ~4h
- [x] **Add connection pooling metrics** -- Expose pool utilization, wait time, eviction rate to Prometheus. ~4h
- [x] **Benchmark memory allocation hotspots** -- Use pprof to identify and reduce allocations in hot paths (proxy request, balancer selection, middleware chain). ~8h
- [x] **Optimize gRPC frame parsing** -- Reduce allocations in `internal/proxy/l7/grpc.go` frame read/write. ~4h
- [x] **Add write-combining for metrics** -- Batch metric increments to reduce atomic operation contention under high load. ~4h
- [x] **Optimize WebUI bundle** -- Audit React chunk splitting, lazy-load pages, reduce initial bundle size. ~4h

**Estimated effort:** 44 hours

---

## Phase 6: Documentation & DX (Week 13-14)

### Documentation and developer experience

- [x] **Update README metrics** -- Fix binary size (9MB -> 10.9MB), algorithm count (14 vs 12 vs 16), dependency count (zero vs 2 vs 3). ~1h
- [x] **Update CHANGELOG** -- Set v1.0.0 release date, move unreleased items to proper version. ~2h
- [x] **Add CONTRIBUTING.md code examples** -- Add examples for adding a new middleware, adding a new balancer algorithm, extending the WAF. ~4h
- [x] **Add architecture decision records** -- Document key decisions: why zero-dep, why Raft+SWIM, why radix trie, why React for WebUI. ~4h
- [x] **Create Grafana dashboard import guide** -- Document how to import the provided dashboard.json. ~1h
- [x] **Add API rate limiting documentation** -- Document the admin auth rate limit behavior and how to configure it. ~2h
- [x] **Create performance tuning guide** -- Document how to tune for different workload types (high RPS, high concurrency, high bandwidth). ~4h

**Estimated effort:** 18 hours

---

## Phase 7: Release Preparation (Week 15-16)

### Final production preparation

- [ ] **Set up GHCR Docker image publishing** -- Configure GitHub Actions to push multi-platform Docker images to ghcr.io/openloadbalancer/olb. ~4h
- [ ] **Set up Homebrew tap** -- Create openloadbalancer/homebrew-tap repository, configure GoReleaser to push formulae. ~4h
- [ ] **Set up package repositories** -- Configure APT, YUM, APK repositories (or document manual installation as the primary method). ~8-40h
- [ ] **Create release signing key** -- Set up GPG key for binary signing and verification. ~2h
- [x] **Final security audit** -- Run gosec, nancy, govulncheck against final codebase. Address any findings. ~4h
- [ ] **Tag v1.0.0 release** -- Update CHANGELOG, tag release, trigger GoReleaser. ~1h
- [x] **Write v1.0.0 release blog post** -- Update docs/blog-v1.0.0.md with accurate metrics and release date. ~4h
- [ ] **Set up monitoring for production** -- Deploy Prometheus alerting rules, Grafana dashboard, verify alerting pipeline. ~4h

**Estimated effort:** 31-63 hours

---

## Beyond v1.0: Future Enhancements

### Features and improvements for future versions

- [ ] HTTP/3 (QUIC) support
- [ ] WebSocket compression (permessage-deflate)
- [ ] Service mesh integration (Envoy xDS API compatibility)
- [ ] Additional cloud provider Terraform modules (GCP, Azure)
- [ ] Enhanced GeoIP with automatic database updates
- [ ] WebUI internationalization (i18n)
- [ ] Configuration dry-run mode (validate without applying)
- [ ] Rolling backend updates (canary deployments)
- [ ] Request/response body transformation (protocol translation)
- [ ] Additional WAF detectors (RFI, LFI, CRLF injection)
- [ ] Plugin marketplace / registry
- [ ] OpenTelemetry native tracing support
- [ ] Kubernetes Gateway API implementation
- [ ] macOS and Windows service management
- [x] Configuration import/export in WebUI (backup page uses real config export/import)

---

## Effort Summary

| Phase | Estimated Hours | Priority | Dependencies |
|-------|----------------|----------|--------------|
| Phase 1: Critical Fixes | 85-125h | CRITICAL | None |
| Phase 2: Core Completion | 74-104h | HIGH | Phase 1 |
| Phase 3: Hardening | 42h | HIGH | Phase 1 |
| Phase 4: Testing | 94h | MEDIUM | Phases 1-2 |
| Phase 5: Performance | 44h | MEDIUM | Phase 2 |
| Phase 6: Documentation | 18h | LOW | Phases 1-2 |
| Phase 7: Release Prep | 31-63h | HIGH | Phases 1-3 |
| **Total** | **388-490h** | | |

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Raft state machine implementation reveals design flaws | Medium | High | Prototype with simple config types first; extensive cluster testing |
| WebUI mock-to-real migration reveals API gaps | Medium | Medium | Verify all API endpoints return expected data before starting migration |
| Removing duplicate middleware breaks existing tests | Low | Medium | Run full test suite after each middleware removal; fix tests before proceeding |
| gRPC-Web implementation complexity exceeds estimate | Medium | Low | Can ship without gRPC-Web initially; document as known limitation |
| Performance regression from middleware fix | Low | Medium | Benchmark before/after; middleware double-execution was actually adding overhead, so removal should improve performance |
| Package repository setup requires infrastructure | High | Low | Start with manual binary downloads and Docker; add package repos incrementally |
| Binary size increases with WebUI changes | Low | Low | Monitor bundle size in CI; existing 20MB limit check in CI workflow |
