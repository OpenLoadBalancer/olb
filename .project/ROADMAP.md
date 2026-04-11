# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-11
> This roadmap prioritizes work needed to bring the project to production quality.

## Current State Assessment

**Where the project stands**: OpenLoadBalancer is feature-complete at v1.0. All 305 tasks from the specification are implemented. The codebase has ~87% test coverage, comprehensive CI/CD, 16 load balancing algorithms, a 6-layer WAF, Raft clustering, MCP AI integration, and an embedded Web UI. The project is production-viable for most use cases.

**Key blockers for production**: None critical. The main risks are operational (single author, no external audit) rather than technical.

**What's working well**: Dependency discipline, test coverage, CI/CD pipeline, documentation, performance benchmarks, security features.

---

## Phase 1: Code Structure Improvements (Week 1-2)

### Refactor oversized files for maintainability
- [ ] **Split engine.go** (1,803 LOC) into focused files: `engine.go` (struct + lifecycle), `pool_setup.go`, `route_setup.go`, `component_wiring.go`, `tls_setup.go` — Effort: 8h
- [ ] **Split gossip.go** (1,739 LOC) into: `gossip.go` (core), `membership.go`, `probe.go`, `broadcast.go` — Effort: 6h
- [ ] **Split advanced_commands.go** (1,458 LOC) into per-command-group files — Effort: 4h
- [ ] **Split config parsers** — toml.go (1,595 LOC) into lexer/parser/decoder — Effort: 4h each

## Phase 2: Security Hardening (Week 3-4)

### Strengthen default security posture
- [ ] **Admin API auth by default** — Add startup warning when admin auth is not configured, consider requiring explicit `auth: none` for unsecured deployments — Effort: 2h
- [ ] **Admin API rate limiting** — Add built-in rate limit to admin endpoints (especially POST /reload, config mutations) — Effort: 4h
- [ ] **Fix IPv6 host parsing** — Replace `strings.LastIndex(host, ":")` with `net.SplitHostPort()` in 7+ locations across router, engine, middleware, SSRF detection — Effort: 2h
- [ ] **Fix Backend.GetURL() scheme** — Add `scheme` field to backend config, default to `http://` but allow `https://` — Effort: 2h
- [ ] **Wire passive health checker** — Connect `OnBackendUnhealthy`/`OnBackendRecovered` callbacks to pool manager state updates — Effort: 4h
- [ ] **Default body size limit** — Enable body_limit middleware with a sensible default (e.g., 10MB) even without explicit config — Effort: 2h
- [ ] **Security headers by default** — Enable basic security headers (X-Content-Type-Options, X-Frame-Options) on admin API — Effort: 2h
- [ ] **Shutdown guard** — Use `sync.Once` for `close(e.stopCh)` to prevent double-close panic — Effort: 1h
- [ ] **External security audit** — Commission a third-party security review of the WAF, TLS, and authentication code — Effort: External

## Phase 3: Missing Spec Features (Week 5-6)

### Complete spec compliance
- [ ] **Exec health checks** — Implement external command health check type per spec §9.1 — Effort: 4h
  - File: `internal/health/exec_check.go`
  - Support command templates with `{{.Address}}`, `{{.Port}}`
  - Exit code 0 = healthy
- [ ] **Request-context aware balancer** — Extend `Balancer.Next()` to accept request context per spec §8.1 — Effort: 16h
  - Interface change: `Next(ctx *RequestContext, backends []*backend.Backend) *backend.Backend`
  - Update all 16 algorithm implementations
  - Enables content-based routing in future
- [ ] **Brotli compression** — Pure Go brotli implementation per spec §10.6 — Effort: 40h+
  - This is a significant undertaking. Consider deferring to v1.1.

## Phase 4: Observability Improvements (Week 7-8)

### Enhance production monitoring
- [ ] **Structured error responses** — Standardize API error format: `{"error": "code", "message": "details", "request_id": "..."}` — Effort: 4h
- [ ] **Distributed tracing** — Add OpenTelemetry-compatible trace propagation (W3C Trace Context headers) — Effort: 16h
- [ ] **Health check aggregation** — Add `/api/v1/system/health` that checks all subsystems (listeners, backends, TLS, cluster) — Effort: 4h
- [ ] **Metrics histograms** — Add latency percentile histograms (p50, p90, p95, p99) to Prometheus output — Effort: 8h
- [ ] **Grafana dashboard improvements** — Update provided Grafana dashboards with all new metrics — Effort: 4h

## Phase 5: Community & Sustainability (Week 9-10)

### Prepare for community contributions
- [ ] **Add CONTRIBUTING.md examples** — Expand with examples for adding discovery providers, admin API endpoints — Effort: 4h
- [ ] **Code of Conduct enforcement** — Set up reporting mechanism beyond CoC text — Effort: 2h
- [ ] **Release automation** — Add `.goreleaser.yml` for automated multi-platform releases — Effort: 4h
- [ ] **Issue/PR templates** — Enhance existing templates with bug report checklists — Effort: 2h
- [ ] **Performance regression tracking** — Add `benchstat` comparison in CI for PRs — Effort: 4h
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

| Phase | Estimated Hours | Priority | Dependencies |
|---|---|---|---|
| Phase 1: Code Structure | 22h | MEDIUM | None |
| Phase 2: Security Hardening | 19h + external | HIGH | None |
| Phase 3: Missing Spec Features | 60h | LOW | Phase 1 |
| Phase 4: Observability | 36h | MEDIUM | None |
| Phase 5: Community | 24h | MEDIUM | None |
| Phase 6: Web UI | 48h | LOW | None |
| **Total** | **~210h** | | |

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Single author burnout | Medium | Critical | Phase 5 (community) is the mitigation |
| Security vulnerability in WAF | Medium | High | External audit (Phase 2) |
| Performance regression in future changes | Low | High | Benchmark CI + benchstat |
| Admin API exposure in production | Medium | High | Default auth enforcement (Phase 2) |
| Breaking balancer interface change | Medium | Medium | Phase 3 careful migration |
| Web UI tech debt accumulation | Medium | Low | TypeScript migration (Phase 6) |
