# Security Audit Report - OpenLoadBalancer

**Date:** 2026-04-13
**Scope:** Full codebase audit (~380 Go files)
**Methodology:** 4-phase pipeline (Recon -> Hunt -> Verify -> Report) + 6 deep-dive scans

## Executive Summary

| Metric | Value |
|--------|-------|
| Total Findings | 97 |
| Critical | 1 |
| High | 12 |
| Medium | 35 |
| Low | 49 |

**Overall Risk Assessment:** MODERATE (after remediation of Critical and most High findings)

## Fixes Applied

### Round 1 — Initial Audit (commit `13562c0`)
Resolved 1 Critical, 6 High, 14 Medium findings.

### Round 2 — Deep-Dive Scans (commit `a20bf02`)
Resolved 6 additional High-severity findings from deep-dive scans.

### Round 3 — P1 Remediation (commit `df88781` + latest)
Resolved P1 race conditions, integer overflow, division-by-zero, and unbounded I/O:

| Finding | File | Fix | Status |
|---------|------|-----|--------|
| conn/pool.go mutex released during blocking dial | internal/conn/pool.go | Re-check closed state after re-acquiring lock | FIXED |
| Passive health callback race with background goroutine | internal/health/passive.go | Read callbacks under pc.mu.RLock | FIXED |
| ParseByteSize integer overflow on large units | pkg/utils/time.go | Check num*multiplier > MaxInt64 before conversion | FIXED |
| parseCustomDuration overflow on weeks/days | pkg/utils/time.go | Overflow check before time.Duration conversion | FIXED |
| TruncateDuration/RoundDuration division by zero | pkg/utils/time.go | Guard precision <= 0, return d unchanged | FIXED |
| Rate limiter division by zero with RequestsPerSecond=0 | internal/middleware/rate_limiter.go | Reject zero/negative values in constructor | FIXED |
| SSE unbounded io.Copy in fallback path | internal/proxy/l7/sse.go | io.LimitReader with 64MB cap | FIXED |
| SSE unbounded io.Copy in copyRegularResponse | internal/proxy/l7/sse.go | io.LimitReader with 64MB cap | FIXED |
| gRPC unbounded io.Copy in HandleGRPC | internal/proxy/l7/grpc.go | io.LimitReader with MaxMessageSize cap | FIXED |

### Round 4 — P1 Batch 2 (latest commit)
Resolved remaining P1 race conditions, goroutine leaks, and resource exhaustion:

| Finding | File | Fix | Status |
|---------|------|-----|--------|
| Cluster callback race with run() goroutine | internal/cluster/cluster.go | callbackMu RWMutex for onStateChange/onLeaderElected | FIXED |
| GetBackendByAddress iterates Backends without pool.mu | internal/backend/manager.go | Use GetAllBackends() with proper locking | FIXED |
| MCP SSE MaxClients defaults to 0 (unlimited) | internal/mcp/sse_transport.go | Default to 100 concurrent clients | FIXED |
| Timeout middleware goroutine leak | internal/middleware/timeout.go | Background drain of handler goroutine on timeout | FIXED |
| WebSocket missing write deadline | internal/proxy/l7/websocket.go | Add write deadline in copyWithIdleTimeout | FIXED |
| TCP proxy MaxConnections defaults to 0 (unlimited) | internal/proxy/l4/tcp.go | Default to 10000 connections | FIXED |
| TCP proxy error type assertion misses wrapped errors | internal/proxy/l4/tcp.go | Use errors.As instead of type assertion | FIXED |

### Round 5 — P2 Quick Wins (latest commit)
| Compression unbounded response buffer | internal/middleware/compression.go | MaxBufferSize (default 8MB) with passthrough | FIXED |
| PROXY protocol ignored Atoi error for ports | internal/proxy/l4/proxyproto.go | Validate parse + range (0-65535) | FIXED |

## Critical Findings

### CRIT-1: MCP Server Fully Open When BearerToken Is Empty
- **File:** internal/mcp/mcp.go:1258-1271
- **CVSS:** 9.8 (Critical)
- **Status:** FIXED — Empty bearerToken now rejected
- Empty bearerToken bypassed all MCP authentication

## High Findings

| ID | Finding | File | CVSS | Status |
|----|---------|------|------|--------|
| HIGH-1 | CSP nonce hardcoded placeholder | internal/middleware/csp/csp.go:217 | 7.5 | FIXED |
| HIGH-2 | HMAC replay protection not implemented | internal/middleware/hmac/hmac.go:29 | 7.5 | FIXED |
| HIGH-3 | CSRF disabled by default | internal/middleware/csrf/csrf.go:32 | 8.0 | FIXED |
| HIGH-4 | MCP tools no authorization granularity | internal/mcp/mcp.go:554 | 7.5 | FIXED |
| HIGH-5 | SSE unbounded line buffering (DoS) | internal/proxy/l7/sse.go:190 | 7.5 | FIXED |
| HIGH-6 | H2C enabled by default | internal/proxy/l7/http2.go:74 | 7.4 | FIXED |
| HIGH-7 | Shadow proxy req.Body race condition | internal/proxy/l7/shadow.go | 7.0 | FIXED |
| HIGH-8 | gRPC parseGRPCFrame unbounded allocation | internal/proxy/l7/grpc.go | 7.5 | FIXED |
| HIGH-9 | Bot detection unbounded IP tracker growth | internal/middleware/botdetection/ | 7.0 | FIXED |
| HIGH-10 | CSRF init error silently swallowed | internal/admin/server.go | 7.0 | FIXED |
| HIGH-11 | Circuit breaker goroutine leak on timeout | internal/admin/circuit_breaker.go | 6.5 | FIXED |
| HIGH-12 | gosec@master unpinned in CI | .github/workflows/ci.yml | 7.0 | FIXED |

## Deep-Dive Scan Results

### Race Conditions (10 findings)
1. **HIGH-7** Shadow proxy req.Body race — multiple goroutines read req.Body concurrently → FIXED
2. conn/pool.go: mutex released during blocking dial
3. cluster/cluster.go: callback function pointers unsynchronized
4. backend/manager.go: Pool.Backends map iterated without pool.mu
5. engine/lifecycle.go: listeners/udpProxies modified without lock
6. health/passive.go: callbacks set after construction, read by background goroutine
7. middleware/cache.go: int64-to-int truncation
8. middleware/rate_limiter.go: sync.Map concurrent access patterns
9. middleware/compression.go: write flush ordering
10. config/watcher.go: untracked goroutine not in engine WaitGroup

### Resource Exhaustion (14 findings)
1. **HIGH-8** gRPC parseGRPCFrame unbounded allocation → FIXED
2. **HIGH-9** Bot detection IP tracker unbounded map growth → FIXED
3. proxy/l7/sse.go: unbounded io.Copy in SSE streaming
4. proxy/l7/grpc.go: unbounded io.Copy in HandleGRPC
5. middleware/coalesce/coalesce.go: unbounded goroutine/map growth from per-request TTL cleanup
6. middleware/rate_limiter.go: unbounded sync.Map growth
7. middleware/cache/cache.go: O(n) eviction; background revalidation with context.Background()
8. middleware/compression.go: unbounded compression buffer
9. middleware/retry.go: full response body buffering on retry
10. middleware/timeout.go: handler goroutine continues after timeout
11. proxy/l7/sse.go: unbounded goroutine creation per SSE stream
12. mcp/sse_transport.go: MaxClients defaults to 0 (unlimited)
13. proxy/l7/websocket.go: missing write deadline
14. proxy/l4/tcp.go: missing connection limits

### Error Handling (27 findings)
1. **HIGH-10** CSRF init error silently swallowed → FIXED
2. cluster/cluster.go: Raft state machine Apply errors discarded
3. engine/engine.go: silent cluster init failure
4. engine/pools_routes.go: health check registration failure only warns
5. engine/cluster_init.go: cluster transport failure degrades silently
6. cluster/config_sm.go: silent recover in callback; Raft Apply errors discarded on follower
7. proxy/l4/tcp.go: silent connection drops; error type assertions miss wrapped errors
8. proxy/l4/udp.go: silent packet drops
9. proxy/l4/proxyproto.go: ignored Atoi error for ports
10. proxy/l7/http2.go: no dial timeout
11. proxy/l7/websocket.go: WebSocket buffered data read errors ignored
12. mcp/mcp.go: internal error details leaked to clients
13. admin/server.go: Prometheus write error ignored
14. Multiple files: silently swallowed errors in defer close, response writes
15-27. Various: missing error checks in non-critical paths (logging, metrics, cleanup)

### Integer Overflow / Unsafe (16 findings)
1. pkg/utils/time.go: ParseByteSize overflow with large units
2. pkg/utils/time.go: parseCustomDuration overflow
3. pkg/utils/time.go: division by zero in TruncateDuration/RoundDuration
4. middleware/rate_limiter.go: division by zero with RequestsPerSecond=0
5. config/hcl/decoder.go: reflect type confusion without bounds checks
6. config/toml/decode.go: reflect type confusion without bounds checks
7. config/yaml/decoder.go: reflect type confusion without bounds checks
8-16. Various: unchecked type assertions, integer truncation, missing bounds validation

### Supply Chain / Build (10 findings)
1. **HIGH-12** gosec@master unpinned in CI → FIXED (pinned to v2.22.3)
2. nancy@latest unpinned in CI → FIXED (pinned to v1.0.106)
3. Missing go mod verify in CI → FIXED
4. Dockerfile base images not pinned by digest
5. golangci-lint@latest unpinned
6. staticcheck@latest unpinned
7. benchstat@latest unpinned
8. No dependency pinning verification in CI
9. No SBOM generation in release pipeline
10. No cosign/signature verification for Docker images

### Goroutine Leaks (14 findings)
1. **HIGH-11** Circuit breaker goroutine leak on timeout → FIXED
2. proxy/l7/sse.go: per-line-read goroutines can block forever
3. config/watcher.go: untracked goroutine not in engine WaitGroup
4. middleware/coalesce/coalesce.go: unbounded goroutine creation
5. middleware/cache/cache.go: background revalidation goroutine with context.Background()
6. proxy/l7/sse.go: drain goroutine lifetime not bounded
7-14. Various: fire-and-forget goroutines without context cancellation

## Remediation Priority

### P0 (Immediate) — All Fixed
CRIT-1, HIGH-1 through HIGH-12

### P1 (Next Sprint) — Mostly Complete
- Race conditions in conn/pool.go (FIXED), cluster/cluster.go (FIXED), backend/manager.go (FIXED)
- SSE/gRPC unbounded io.Copy (FIXED)
- Integer overflow fixes in pkg/utils/time.go (FIXED)
- Rate limiter division by zero (FIXED)
- Error type assertions (tcp.go FIXED; remaining use errors.As where needed)
- MCP SSE unbounded clients (FIXED)
- Timeout goroutine leak (FIXED)
- WebSocket missing write deadline (FIXED)
- TCP proxy missing connection limits (FIXED)

### P2 (Next Quarter)
- MED-7: Add RBAC to admin API (Large effort)
- MED-9: Use []byte for secrets with zeroing (Medium effort)
- MED-11: Full mTLS client cert revocation (Large effort)
- Dockerfile image pinning by digest
- SBOM generation in CI/CD

## Scan Categories

| Category | Tool | Findings |
|----------|------|----------|
| Injection (SQL, XSS, SSTI, etc.) | Pattern matching | 0 exploitable |
| Authentication & Authorization | Code review | 6 (all fixed) |
| Race Conditions | Concurrency analysis | 10 (1 fixed) |
| Resource Exhaustion | DoS analysis | 14 (2 fixed) |
| Error Handling | Anti-pattern scan | 27 (1 fixed) |
| Integer Overflow | Bounds analysis | 16 (0 fixed) |
| Supply Chain | CI/CD audit | 10 (3 fixed) |
| Goroutine Leaks | Lifecycle analysis | 14 (1 fixed) |
