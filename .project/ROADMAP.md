# OpenLoadBalancer — Development Roadmap

**Date:** 2026-04-16
**Based on:** Comprehensive codebase audit (4 parallel deep-dive agents)
**Target:** Production-grade reliability for v1.1.0

---

## Current State

- **Version:** v1.0.0 (released 2026-04-11)
- **Spec completion:** ~99%
- **Test coverage:** ~93% (on passing packages)
- **Known issues:** 4 HIGH, 28 MEDIUM, 30 LOW
- **Bus factor:** 1

**Note:** An initial deep audit by Agent 4 reported 6 "CRITICAL" cluster/admin findings. Upon verification, ALL were false positives — the agent read stub files instead of actual implementation. The real codebase has proper Raft replication, binary-framed transport, mutex-protected elections, and fully implemented admin endpoints.

---

## Phase 1: Security Hardening (Week 1-2)

**Priority: CRITICAL** — These affect any deployment exposed to untrusted traffic.

### ~~1.1 WAF SSRF Detection~~ (FIXED)
- **Status:** Fixed IPv6 host extraction bug in `extractHost` (brackets prevented `net.ParseIP` from matching). Azure/DigitalOcean metadata already covered by `169.254.169.254`. Added edge case tests: IPv6 loopback, IPv6 ULA, AWS IPv6 metadata, mixed-case hostname, short IP forms, Alibaba metadata, GCP metadata, multiple URLs, 172.x boundary values, link-local, credential bypass to external, URL with port.

### ~~1.2 MCP SSE Transport CORS~~ (FALSE POSITIVE)
- **Status:** ALREADY IMPLEMENTED — `sse_transport.go` has configurable `AllowedOrigins` list with `Vary: Origin` and `Access-Control-Allow-Credentials: true`. No wildcard CORS.

### ~~1.3 MCP Body Size Limits~~ (FIXED)
- **Status:** Fixed — upgraded from `io.LimitReader` to `http.MaxBytesReader` on all 3 MCP endpoints. Now returns proper 413 error when limit exceeded instead of silently truncating.

### ~~1.4 PROXY Protocol Trusted Upstreams~~ (FALSE POSITIVE)
- **Status:** ALREADY IMPLEMENTED — `PROXYProtocolConfig.TrustedNetworks` CIDR list with `isTrustedSource()` method. Default: trust no one (empty list = reject all PROXY headers).

### ~~1.5 Path Traversal Verification~~ (FIXED)
- **Status:** Added edge case tests for mixed encoding (literal + encoded slash, encoded dots + literal, case-insensitive `%2E%2E%2F`), repeated encoded traversal, encoded dots in query params, null byte with traversal, backslash traversal, encoded backslash dots. All existing and new tests pass.

---

## Phase 2: Reliability Fixes (Week 2-3)

**Priority: HIGH** — These affect uptime and correctness under load.

### ~~2.1 UDP Connection Limits~~ (FALSE POSITIVE)
- **Status:** ALREADY IMPLEMENTED — `MaxSessions` field with default 10,000 in `UDPProxyConfig`, enforced at `createSession()` line 356.

### ~~2.2 Hot Reload Atomicity~~ (FALSE POSITIVE)
- **Status:** ALREADY IMPLEMENTED — `applyConfigInternal()` builds new router/pools/health checker OUTSIDE the lock, then takes `e.mu.Lock()` only for the atomic pointer swap (lines 177-208).

### ~~2.3 MCP Resource Implementation~~ (FALSE POSITIVE)
- **Status:** ALREADY IMPLEMENTED — All 4 resources (metrics, config, health, logs) are fully implemented in `internal/mcp/mcp.go:720-768` with proper engine integration.

### ~~2.4 MCP Version Tool~~ (FIXED)
- **Status:** Fixed — replaced hardcoded `serverVersion = "0.1.0"` with `version.Version` from `pkg/version`.

### ~~2.5 Cache Key Fix~~ (FALSE POSITIVE)
- **Status:** NOT A BUG — `DefaultKeyFunc` uses `r.URL.String()` which in Go stdlib already includes query parameters. Key format: `r.Method + ":" + r.URL.String()`.

### ~~2.6 Engine Shutdown Safety~~ (FALSE POSITIVE)
- **Status:** NOT A BUG — No `shutdownMu` exists in the codebase. `Shutdown()` uses `e.mu` with correct Lock/Unlock pairs.

### ~~2.5 Cache Key Fix~~ (FALSE POSITIVE)
- **Status:** NOT A BUG — `DefaultKeyFunc` uses `r.URL.String()` which in Go stdlib already includes query parameters. Key format: `r.Method + ":" + r.URL.String()`.

### ~~2.6 Engine Shutdown Safety~~ (FALSE POSITIVE)
- **Status:** NOT A BUG — No `shutdownMu` exists in the codebase. `Shutdown()` uses `e.mu` with correct Lock/Unlock pairs. The audit finding referenced a non-existent mutex.

---

## Phase 3: Production Observability (Week 3-4)

**Priority: MEDIUM** — Required for operating in production with confidence.

### ~~3.1 Structured Security Event Logging~~ (FIXED)
- **Status:** Added request ID correlation to all 8 WAF event paths. Request ID middleware now sets ID on request header so downstream consumers can read it. Added `security_events_total` Prometheus counter labeled by `rule`, wired into `LogEvent()`.

### 3.2 ~~Shadow Traffic Metrics~~ (FIXED)
- **Status:** Added `shadow_requests_total`, `shadow_errors_total`, `shadow_dropped_total` counters to `ShadowManager`. Integrated with engine lifecycle.

### 3.3 ~~ACME Renewal Alerting~~ (FIXED)
- **Status:** Added certificate expiry monitor to `internal/tls/manager.go`. Runs hourly, logs warnings for certs expiring within 30 days. Configurable `ExpiryAlertFunc` callback for webhook integration. Wired into engine lifecycle (start/stop).

### ~~3.4 ACME Rate Limit Tracking~~ (FIXED)
- **Status:** Added `RateTracker` in `internal/acme/ratelimit.go` with sliding time windows. Tracks orders per domain (50/week), orders per account (300/3h), failed validations (5/h). Warns at 80% of limits. Integrated into `Client.CreateOrder` (pre-check blocks over-limit) and `Client.PollAuthorization` (failure tracking). 9 tests.

---

## Phase 4: WAF Depth Improvements (Week 4-5)

**Priority: MEDIUM** — WAF is marketed as 6-layer but has detection gaps.

### ~~4.1 Double Encoding Protection~~ (FALSE POSITIVE)
- **Status:** Already implemented — `DecodeMultiLevel` in `internal/waf/sanitizer/normalize.go` iteratively decodes up to 5 levels.

### ~~4.2 SQLi Comment Splitting~~ (FALSE POSITIVE)
- **Status:** Already implemented — `rawPatternScan` in `internal/waf/detection/sqli/sqli.go` strips `/**/` before pattern matching.

### ~~4.3 CMDi Windows Patterns~~ (FIXED)
- **Status:** Added 30+ Windows commands (certutil, bitsadmin, mshta, powershell, wmic, etc.) and Windows shell paths to `internal/waf/detection/cmdi/patterns.go`.

### ~~4.4 Host Header Validation Default~~ (FALSE POSITIVE)
- **Status:** Already enforced — `security.ValidateRequest()` is called on every proxy request, which includes `ValidateHostHeader()`.
- **Effort:** 2 hours

### ~~4.5 XXE UTF-7 Bypass~~ (FIXED)
- **Status:** Added UTF-7 decoding to XXE detector (`internal/waf/detection/xxe/xxe.go`). Decodes `+ADw-`, `+AD4-`, `+AFs-`, `+AF0-` and other common XXE bypass sequences before pattern matching. Findings tagged with `utf7_` prefix.

### ~~4.6 Path Traversal Encoded Dots~~ (FIXED)
- **Status:** Added detection for `%2e%2e/`, `%2e%2e%2f`, `%2e%2e%5c` patterns in path traversal detector (`internal/waf/detection/pathtraversal/pathtraversal.go`). Previously only detected `..%2f` and `..%5c`.

---

## Phase 5: Frontend Improvements (Week 5-6)

**Priority: MEDIUM** — WebUI is the primary management interface.

### ~~5.1 Accessibility Remediation~~ (FIXED)
- **Status:** Added `aria-hidden="true"` to 99 decorative SVG icons across 15 files. Added `aria-label` to listener enable/disable Switch. Added `role="button"`, `tabIndex`, `aria-pressed`, and `onKeyDown` to HTTP method Badge toggles. Added `role="status"` and `aria-live="polite"` to logs live/paused indicator.

### ~~5.2 AbortController for API Calls~~ (FIXED)
- **Status:** Added `AbortController` to `useQuery` and `useMutation` hooks. Signal passed through all 13 domain hooks and all `api.*` methods. In-flight requests cancelled on unmount. Retry sleep is abort-aware. `useMutation` aborts previous request on re-mutate.

### ~~5.3 Remove Unused Dependencies~~ (VERIFIED — NOT NEEDED)
- **Status:** Both `react-hook-form` and `zod` are actively used in pools, listeners, certificates, and cluster pages.

### ~~5.4 Error Boundaries~~ (FIXED)
- **Status:** Added `PageErrorBoundary` class component wrapping all 11 routes in `main.tsx`. Shows error message, retry button, logs to console.

---

## Phase 6: Performance Optimizations (Week 6-7)

**Priority: LOW** — Current performance is good (15K RPS) but specific areas can improve.

### ~~6.1 Router Param Map Pooling~~ (FIXED)
- **Status:** Added `sync.Pool` for param maps in `internal/router/radix.go`. `PutRouteMatch()` returns maps to pool. Wired into HTTP proxy, WebSocket, and SSE handlers.

### ~~6.2 Reload Lock-Free Swap~~ (ALREADY DONE)
- **Status:** Covered in Phase 2.2. `applyConfigInternal()` builds new state outside lock, then atomic swap under brief write-lock.

### ~~6.3 Shadow Transport Reuse~~ (FALSE POSITIVE)
- **Status:** Already implemented — `ShadowManager` uses a single shared `http.Transport` per manager, not per-request. No change needed.

### ~~6.4 UDP Goroutine Pool~~ (NOT NEEDED)
- **Status:** Per-session goroutine pattern is standard Go for UDP proxies. Go goroutines are lightweight (~8KB stack), the runtime scheduler handles blocked-on-I/O goroutines efficiently, and `MaxSessions` (default 10K) already bounds growth. A worker pool would be worse: either polling with short timeouts (CPU waste) or reimplementing Go's netpoller.

---

## Phase 7: Spec Compliance & Polish (Week 7-8)

**Priority: LOW** — Marketing claims should match reality.

### ~~7.1 MCP README Alignment~~ (FIXED)
- **Status:** Fixed phantom tool names in `docs/mcp.md`, `docs/configuration.md`, `docs/SPECIFICATION.md`. Aligned `olb_diagnose` params (`mode` not `type`), `olb_modify_route` params (`host`/`path` not `name`/`match`), `olb_get_logs` params (`count` not `limit`), resource URIs, and prompt template names with implementation.

### ~~7.2 JWT Algorithm Restriction~~ (FIXED)
- **Status:** Added allowlist (HS256, HS384, HS512, EdDSA) at config time. Reject "none"/empty at validation time.

### ~~7.3 OAuth2 HTTPS Validation~~ (FIXED)
- **Status:** Added HTTPS validation for IntrospectionURL, JwksURL, IssuerURL at config time. Bypass via `AllowInsecureHTTP` for development.

### ~~7.4 Add CONTRIBUTORS File~~ (FIXED)
- **Status:** Created `CONTRIBUTORS.md`.

---

## Timeline Summary

| Phase | Week | Focus | Items |
|-------|------|-------|-------|
| 1 | 1-2 | Security hardening | 5 items |
| 2 | 2-3 | Reliability fixes | 6 items |
| 3 | 3-4 | Observability | 4 items |
| 4 | 4-5 | WAF depth | 4 items |
| 5 | 5-6 | Frontend | 4 items |
| 6 | 6-7 | Performance | 4 items |
| 7 | 7-8 | Spec compliance | 4 items |

**Total estimated effort:** ~10 person-days (reduced from ~31 due to 12 false positives, 8 initial fixes, and 5 additional fixes)

---

## Version Milestones

- **v1.0.1** — Phase 1 + Phase 2 (security + reliability) — **Target: 2 weeks**
- **v1.0.2** — Phase 3 + Phase 4 (observability + WAF) — **Target: 4 weeks**
- **v1.1.0** — Phase 5 + Phase 6 + Phase 7 (frontend + perf + polish) — **Target: 8 weeks**
