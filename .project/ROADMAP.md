# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-11
> Updated on 2026-04-11 to reflect completed work.
> This roadmap prioritizes work needed to bring the project to production quality.

## Current State Assessment

**Where the project stands**: OpenLoadBalancer is feature-complete at v1.0. All 305 tasks from the specification are implemented. The codebase has ~87% test coverage, comprehensive CI/CD, 16 load balancing algorithms, a 6-layer WAF, Raft clustering, MCP AI integration, and an embedded Web UI. The project is production-viable for most use cases.

**Key blockers for production**: None critical. The main risks are operational (single author, no external audit) rather than technical.

**What's working well**: Dependency discipline, test coverage, CI/CD pipeline, documentation, performance benchmarks, security features.

---

## Phase 1: Code Structure Improvements (Week 1-2)

### Refactor oversized files for maintainability
- [x] **Split engine.go** (1,843 → 507 LOC) into 8 focused files: `engine.go`, `lifecycle.go`, `listeners.go`, `mtls.go`, `cluster_init.go`, `pools_routes.go`, `status.go`, `helpers.go`
- [x] **Split gossip.go** (1,739 → 272 LOC) into 7 files: `gossip.go`, `gossip_types.go`, `gossip_encoding.go`, `gossip_transport.go`, `gossip_handler.go`, `gossip_probe.go`, `gossip_broadcast.go`
- [x] **Split advanced_commands.go** (1,458 → 38 LOC) into 7 files: `advanced_commands.go`, `backend_commands.go`, `route_commands.go`, `cert_commands.go`, `metrics_commands.go`, `config_commands.go`, `completion_scripts.go`
- [x] **Split config parsers** — toml.go (1,595 LOC) split into 5 files: toml.go (50), types.go (34), lexer.go (629), parser.go (564), decode.go (318)

## Phase 2: Security Hardening (Week 3-4)

### Strengthen default security posture
- [x] **Admin API auth wiring** — Wire `cfg.Admin.Username/Password/BearerToken` → `admin.AuthConfig`. Admin already enforces localhost-only when no auth configured.
- [x] **Admin API rate limiting** — Per-IP sliding window rate limiter already exists in admin server (`internal/admin/server.go`)
- [x] **Fix IPv6 host parsing** — Replaced `strings.LastIndex(host, ":")` with `net.SplitHostPort()` in all 9 locations across router, engine, middleware, SSRF detection
- [x] **Fix Backend.GetURL() scheme** — Added `Scheme` field to backend config, defaults to `http://` but supports `https://`
- [x] **Wire passive health checker** — Connected callbacks to pool manager state updates + wired proxy to passive checker for recording outcomes
- [x] **Default body size limit** — Already implemented with 10MB default. Opt-in by design (upload streams may need larger bodies) — Effort: 0h
- [x] **Security headers on admin API** — Wired `secureheaders.RecommendedConfig()` middleware into admin server handler chain
- [x] **Shutdown guard** — Added `sync.Once` for `close(e.stopCh)` to prevent double-close panic
- [ ] **External security audit** — Commission a third-party security review of the WAF, TLS, and authentication code — Effort: External

## Phase 3: Missing Spec Features (Week 5-6)

### Complete spec compliance
- [x] **Exec health checks** — Implemented external command health check type (`internal/health/health.go` checkExec method)
  - Supports command + args, timeout via existing `config.Timeout` field
  - Exit code 0 = healthy, non-zero = unhealthy
  - Stderr captured for error messages
  - Tests: `internal/health/exec_test.go`
- [x] **Static discovery YAML** — Replaced stub with actual YAML parsing using internal parser
- [ ] **Request-context aware balancer** — Extend `Balancer.Next()` to accept request context per spec §8.1 — Effort: 16h
  - Interface change: `Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend`
  - Update all 16 algorithm implementations
  - Enables content-based routing in future
- [ ] **Brotli compression** — Pure Go brotli implementation per spec §10.6 — Effort: 40h+
  - This is a significant undertaking. Consider deferring to v1.1.

## Phase 4: Observability Improvements (Week 7-8)

### Enhance production monitoring
- [x] **Structured error responses** — `ErrorResponse(code, message)` in `internal/admin/types.go`, used across all handlers
- [ ] **Distributed tracing** — Add OpenTelemetry-compatible trace propagation (W3C Trace Context headers) — Effort: 16h
- [x] **Health check aggregation** — `/api/v1/system/health` endpoint already exists in admin server
- [x] **Metrics histograms** — `HistogramVec` with `request_duration`, configurable buckets, latency tracking already implemented
- [ ] **Grafana dashboard improvements** — Update provided Grafana dashboards with all new metrics — Effort: 4h

## Phase 5: Community & Sustainability (Week 9-10)

### Prepare for community contributions
- [x] **CONTRIBUTING.md** — Comprehensive guide (508 lines) with examples for adding balancers, middleware, WAF detectors
- [ ] **Code of Conduct enforcement** — Set up reporting mechanism beyond CoC text — Effort: 2h
- [x] **Release automation** — `.goreleaser.yml` exists with builds, Docker multi-arch, Homebrew, nFPM, Helm, SBOM, changelog grouping. Fixed typo, added ShortCommit ldflag, removed unnecessary CGO override.
- [ ] **Issue/PR templates** — Enhance existing templates with bug report checklists — Effort: 2h
- [x] **Performance regression tracking** — Benchstat comparison in CI: baseline vs PR branch comparison with PR comment
- [ ] **Architecture Decision Records** — Formalize ADRs for key decisions (balancer registry, middleware pattern, etc.) — Effort: 8h

## Phase 6: Web UI Modernization (Week 11-14)

### Improve Web UI maintainability and accessibility
- [ ] **TypeScript migration** — Add TypeScript to WebUI build pipeline — Effort: 20h
- [ ] **Accessibility audit** — Full WCAG 2.1 AA compliance check — Effort: 8h
- [ ] **ARIA labels audit** — Ensure all interactive elements have proper labels — Effort: 4h
- [ ] **Keyboard navigation** — Verify tab order, focus management, skip links — Effort: 4h
- [ ] **Mobile responsiveness** — Test and fix on mobile viewports — Effort: 4h
- [ ] **Bundle size optimization** — Code splitting, lazy loading of pages — Effort: 8h

## Beyond v1.0: Future Enhancements

### Features and improvements for future versions
- [ ] **HTTP/3 (QUIC) support** — Spec mentioned `quic.go` as future work
- [ ] **GeoIP database** — Spec mentioned embedded GeoIP DB for geo-based routing
- [ ] **WebAssembly plugins** — Extend plugin system with WASM runtime
- [ ] **API gateway features** — Request transformation, API versioning, request/response validation
- [ ] **Service mesh integration** — xDS API support for Envoy-compatible service mesh
- [ ] **Hot config rollback** — Automatic rollback if new config causes errors within grace period
- [ ] **Multi-process mode** — Shared-nothing multi-process for zero-downtime upgrades
- [ ] **WebUI real-time updates** — Server-Sent Events or WebSocket push for live dashboard updates

---

## Effort Summary

| Phase | Estimated Hours | Status |
|---|---|---|
| Phase 1: Code Structure | 22h (22h done) | Complete |
| Phase 2: Security Hardening | 19h + external (19h done) | Complete (external audit pending) |
| Phase 3: Missing Spec Features | 60h (4h done) | In progress |
| Phase 4: Observability | 36h (24h done) | Mostly complete |
| Phase 5: Community | 24h (12h done) | In progress |
| Phase 6: Web UI | 48h | Not started |

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Single author burnout | Medium | Critical | Phase 5 (community) is the mitigation |
| Security vulnerability in WAF | Medium | High | External audit (Phase 2) |
| Performance regression in future changes | Low | High | Benchmark CI + benchstat (done) |
| Admin API exposure in production | Medium | High | Default auth enforcement (done) |
| Breaking balancer interface change | Medium | Medium | Phase 3 careful migration |
| Web UI tech debt accumulation | Medium | Low | TypeScript migration (Phase 6) |
