# OpenLoadBalancer — Prioritized Roadmap

> **Date**: 2026-04-11
> **Source**: Analysis findings from `.project/ANALYSIS.md`
> **Methodology**: Risk-weighted prioritization — items ordered by impact × urgency × effort
> **Scope**: Work needed to move from current state to hardened production readiness

---

## Current State Summary

| Dimension | Status | Score |
|-----------|--------|-------|
| Go Backend | Production-ready | A- |
| Frontend (Web UI) | Tested, a11y-audited, E2E coverage | A- |
| Test Coverage (Go) | 95% average, 67/67 packages passing | A |
| Test Coverage (Frontend) | 177 Vitest + 4 Playwright tests | A- |
| Accessibility | Zero axe-core violations, WCAG 2.1 AA | A |
| Spec Compliance | ~97% | A |
| CI/CD | Multi-OS, coverage enforcement, lint gates | A |
| Performance | 15K RPS, optimized hot path | B+ |
| Documentation | Comprehensive + migration guides | A |
| Technical Debt | 1 item remaining (TD-16, acceptable) | A- |

---

## Phase 1: Critical Fixes (Week 1-2) — 6h

> **Goal**: Eliminate the highest-risk items with minimal effort.

### 1.1 Remove Committed Build Artifacts — DONE
- Added `internal/webui/assets/` to `.gitignore`
- Removed tracked `index.html` from git index (`git rm --cached`)
- CI builds frontend in separate job before Go embed step
- **ID**: TD-03

### 1.2 Remove Unused `next-themes` Dependency — DONE
- Already removed from `internal/webui/package.json` (0 matches)

### 1.3 Refactor Duplicated `applyConfig` Code — DONE
- Already refactored: `applyConfig()` and `rollbackConfig()` both delegate to `applyConfigInternal(cfg, noRollback bool)`
- No code duplication remains in `internal/engine/config.go`
- **ID**: TD-01

### 1.4 Fix `panic()` in `pkg/errors/errors.go` — DONE (no change needed)
- Single `panic("errors.As: target cannot be nil")` is a standard Go programmer-error guard (matches stdlib behavior)
- No actionable panics to fix — all other error handling uses proper error returns
- **ID**: TD-07
- **ID**: TD-07

### 1.5 Enforce golangci-lint in CI — DONE
- `golangci/golangci-lint-action@v6` already in CI workflow
- `.golangci.yml` config updated with correct Go version (1.26)
- **ID**: TD-10

---

## Phase 2: Frontend Hardening (Week 3-6) — 50h

> **Goal**: Bring the Web UI to production quality with tests and quality gates.

### 2.1 Add Frontend Testing Infrastructure — DONE
- Vitest + React Testing Library + jsdom installed and configured
- Path aliases set up in `vitest.config.ts`
- Test utilities (`renderWithProviders`, `mockApi`) created
- `npm test` script in `package.json`
- CI `build-frontend` job includes `npm test` step
- **ID**: TD-02 (partial)

### 2.2 Write Core Component Tests — DONE
- 50+ tests across 10 pages: dashboard, pools, backends, settings, cluster, metrics, WAF, middleware, certificates, logs, error
- **ID**: TD-02 (partial)

### 2.3 Add Frontend Linting — DONE
- ESLint 9 flat config with typescript-eslint + react-hooks + react-refresh + prettier
- `npm run lint` and `npm run format` scripts
- Lint gate in CI (`npm run lint` before `npm test`)
- **ID**: TD-05

### 2.4 API Integration Tests — DONE
- **Effort**: 12h → 4h (used existing mockFetch pattern instead of MSW)
- **Risk**: Low
- **Done**:
  - Expanded `api.test.ts` from 6 to 36 tests covering all 25 API endpoints
  - Error state tests: 401 Unauthorized, 404 Not Found, 500 Internal Server Error, 502/503 Bad Gateway, network failure (TypeError)
  - Request construction tests: Content-Type header, JSON body encoding, URL encoding for special characters
  - Created `use-query.test.ts` with 24 tests for `useQuery`, `useMutation`, `useToastMutation` hooks
  - Retry logic tests: transient errors (502, 503, 504, TypeError) retry with backoff; non-transient errors (401, 404, 500) fail immediately
  - Found and fixed bug in `useQuery` hook: non-transient errors were incorrectly retried (missing `break` in catch block)
  - Toast mutation tests: success/error messages, dynamic message functions, fallback to error.message
- **ID**: TD-02 (partial)

### 2.5 Accessibility Audit — DONE
- **Effort**: 8h → 3h (strong a11y foundation already in place)
- **Risk**: Low
- **Done**:
  - Installed `vitest-axe` with `axe-core` for automated accessibility testing
  - Created `src/test/a11y.test.tsx` with automated axe-core scan across all 9 pages
  - All 9 pages pass with zero a11y violations
  - Fixed Logs page: added `aria-label` to auto-scroll Switch and event level filter Select
  - Fixed Middleware page: added `aria-pressed` to category filter buttons
  - Fixed Logs page: added `<caption>` to event log table for screen readers
  - Existing a11y features verified: landmark roles, skip-to-content link, keyboard navigation (Enter/Space/Escape), decorative icons with `aria-hidden`, form labels with `htmlFor`, document title management, focus-visible styles
- **Continues**: Existing WCAG 2.1 AA work (commit 744d6ae)

### 2.6 E2E Browser Tests — DONE
- **Effort**: 4h → 2h
- **Risk**: Low
- **Done**:
  - Installed `@playwright/test` with Chromium browser
  - Created `playwright.config.ts` with Vite dev server integration
  - Created `e2e/smoke.spec.ts` with 4 smoke tests:
    - Loads dashboard page and verifies nav + heading
    - Navigates between Pools, Cluster, Settings pages via sidebar links
    - Skip-to-content link is present and visible on Tab
    - Responsive sidebar collapses on mobile viewport (640px)
  - All 4 E2E tests pass
  - `npm run test:e2e` script added to `package.json`
- **Note**: E2E tests require dev server running (handled automatically by Playwright)

---

## Phase 3: Missing Spec Features & Performance (Week 5-8) — 40h

> **Goal**: Close spec gaps and optimize performance.

### 3.1 Implement gRPC Health Check — DONE
- Already fully implemented in `internal/health/grpc_check.go` with unary check support
- Config support for `grpc` health check type already wired
- Tests pass in `internal/health/`

### 3.2 Implement Exec Health Check — DONE
- Implemented in `internal/health/` with template variable support (`{{.Address}}`, `{{.Host}}`, `{{.Port}}`)
- Config fields (`command`, `args`) wired through engine startup and hot-reload
- Tests in `internal/health/exec_test.go`

### 3.3 Performance Optimization Pass — DONE
- **Effort**: 16h → ~10h (remaining work is incremental profiling)
- **Risk**: Medium
- **Done**:
  - Merged two `context.WithValue` into single `requestState` struct (saves 1 context + 1 request allocation per request)
  - Stack-allocated `attemptedBackends` array (avoids heap allocation in 99% case)
  - Skip backend filtering on first retry attempt (avoids slice allocation when no retries needed)
  - Added `hopByHopCanonicalSet` for zero-allocation hop-by-hop header check in `copyHeaders`
  - Pre-computed `strconv.FormatFloat` and `strconv.Itoa` in rate limiter (eliminates constant-per-request allocs)
  - Pre-computed `strings.Join` for CORS AllowedMethods/AllowedHeaders/ExposedHeaders/MaxAge (eliminates 3-4 allocs per CORS request)
  - Pooled `headerResponseWriter` struct via `sync.Pool` (eliminates 1 alloc per request through headers middleware)
  - Existing benchmark infrastructure already comprehensive (36 benchmark functions)
  - CI already has benchstat regression detection in PR workflow
  - Benchmarked with middleware disabled vs enabled: middleware adds only 16 allocs/request (~3.7µs overhead), well within target
- **Remaining (incremental)**:
  - Profile CPU under sustained load with `go tool pprof` (requires real deployment)
  - Profile memory allocation hotspots (requires real deployment)
- **Spec Reference**: SPECIFICATION.md §20

### 3.4 Review `cachedHandler` Race Condition — DONE
- Converted from bare `http.Handler` field to `atomic.Value` for safe concurrent `ServeHTTP` + `RebuildHandler`
- Concurrent tests added in `internal/proxy/l7/proxy_test.go`
- **ID**: TD-06

---

## Phase 4: CI/CD & Infrastructure Improvements (Week 7-9) — 20h

> **Goal**: Strengthen the deployment pipeline.

### 4.1 Multi-OS CI Testing — DONE
- GitHub Actions matrix: `os: [ubuntu-latest, macos-latest, windows-latest]`
- `shell: bash` on all platforms, race detector scoped to Linux
- **ID**: TD-09

### 4.2 Coverage Diff Enforcement on PRs — DONE
- `codecov.yml` configured with 85% target, 0.5% threshold for project and patch
- Codecov already uploading in CI
- **Note**: Codecov is already uploading, just needs enforcement

### 4.3 Generate OpenAPI Spec — DONE
- `docs/api/openapi.yaml` already comprehensive (1273 lines, 20+ endpoints, full schemas)
- Updated with SSE `/events/stream` endpoint and fixed Go version
- **ID**: TD-08

### 4.4 Split Large Files — DONE
- `internal/admin/handlers.go` (981 LOC) split into 5 focused files
- `internal/config/hcl/hcl.go` (1415 LOC) split into `lexer.go`, `parser.go`, `decoder.go`
- **ID**: TD-11, TD-12

---

## Phase 5: Polish & Documentation (Week 9-10) — 12h

> **Goal**: Final cleanup for production confidence.

### 5.1 Update Stale Documentation — DONE
- CHANGELOG.md expanded with comprehensive `[Unreleased]` section covering all recent changes
- README links verified (API reference points to OpenAPI spec)
- **ID**: TD-14, TD-15

### 5.2 Add Deployment Guides — DONE
- **Effort**: 6h → 0h (already complete)
- `docs/production-deployment.md` covers: single node, HA cluster (3-node + Keepalived VIP), Kubernetes (Helm + manual Deployment/Service/ConfigMap), Docker, systemd, security hardening, Prometheus + Alertmanager, backup/recovery, performance tuning, upgrade procedure

### 5.3 Add Migration Examples — DONE
- **Effort**: 4h → done
- Expanded `docs/migration-guide.md` from 370 → 901 lines
- NGINX: weighted backends, gzip, basic auth, HTTPS redirect, virtual hosts, timeouts
- HAProxy: ACL routing, map files, connection limits, circuit breaker
- Traefik: label→YAML translation, middleware chains, path prefix routing
- Envoy: retries/timeouts, weighted cluster traffic splitting
- Added detailed migration checklist with algorithm mapping table

---

## Roadmap Summary

| Phase | Focus | Duration | Effort | Impact |
|-------|-------|----------|--------|--------|
| **Phase 1** | Critical fixes | Week 1-2 | 6h | Removes high-risk debt items |
| **Phase 2** | Frontend hardening | Week 3-6 | 50h | Enables trusted Web UI |
| **Phase 3** | Spec gaps + perf | Week 5-8 | 40h | 100% spec compliance, better performance |
| **Phase 4** | CI/CD improvements | Week 7-9 | 20h | Multi-OS confidence, API documentation |
| **Phase 5** | Polish + docs | Week 9-10 | 12h | Production deployment readiness |
| **Total** | | **10 weeks** | **~128h** | |

### Risk Matrix

| Phase | Risk | Mitigation |
|-------|------|-----------|
| Phase 1 | Low | All changes have clear rollback paths |
| Phase 2 | Low | Testing infrastructure is additive |
| Phase 3 | Medium | Performance changes require careful benchmarking; spec features are new code |
| Phase 4 | Low | CI changes are infrastructure-only |
| Phase 5 | Low | Documentation-only |

### Priority Justification

1. **Phase 1 first**: The code duplication in `applyConfig` is a correctness risk — if someone fixes a bug in one path but not the other, config reload could silently fail. Build artifacts in git will cause merge conflicts as the frontend evolves.

2. **Phase 2 second**: The Web UI is the primary management interface. Without tests, any frontend change risks regressions. The 40h estimate for full testing is significant but necessary for production trust.

3. **Phase 3 third**: The missing spec features (gRPC health check, exec health check) are unlikely to be showstoppers — most deployments use HTTP or TCP health checks. Performance optimization is important but the current 15K RPS is sufficient for many use cases.

4. **Phase 4 fourth**: Multi-OS CI is a nice-to-have. The project builds cross-platform but testing on other OSes catches edge cases in signal handling, file paths, and network behavior.

5. **Phase 5 last**: Documentation polish is important for adoption but doesn't affect runtime correctness.
