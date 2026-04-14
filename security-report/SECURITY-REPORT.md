# Security Audit Report — OpenLoadBalancer

**Date:** 2026-04-14 (Full Rescan)
**Previous Audit:** 2026-04-14 (HMAC-focused, 31 findings — all remediated)
**Scope:** Full codebase (~380 Go files), 48 security skill categories
**Pipeline:** Recon → Hunt → Verify → Report
**Scanner:** security-check skill v2

---

## Executive Summary

Full rescan of the OpenLoadBalancer codebase after remediation of the previous 31 findings. The codebase demonstrates **strong overall security posture** with defense-in-depth architecture, proper cryptographic defaults, comprehensive request smuggling protections, and well-implemented auth mechanisms.

**Previous audit (31 findings): ALL REMEDIATED.**

This fresh scan identified **1 HIGH, 13 MEDIUM, 15 LOW** new findings. No CRITICAL vulnerabilities found.

| Severity | Count | Key Areas |
|----------|-------|-----------|
| CRITICAL | 0 | — |
| HIGH | 1 | Cluster Raft transport unauthenticated |
| MEDIUM | 13 | SSRF, crypto timing, integer overflow, DoS |
| LOW | 15 | Config defaults, CLI timeouts, defense-in-depth |
| INFO | 8 | Positive observations, design decisions |

---

## Finding Summary Table

| # | Severity | Category | File | Finding |
|---|----------|----------|------|---------|
| **H-1** | HIGH | Auth | `internal/cluster/transport.go:136-148` | Raft TCP transport has no authentication |
| M-1 | MEDIUM | Injection | `internal/health/health.go:478` | Exec health check: no command allowlist |
| M-2 | MEDIUM | SSRF | `internal/health/health.go:575-586` | Weak health check address validation (no RFC1918/loopback blocking) |
| M-3 | MEDIUM | SSRF | `internal/proxy/l7/shadow.go:112` | `X-OLB-Shadow-Force` header bypasses percentage control |
| M-4 | MEDIUM | SSRF | `internal/discovery/consul.go:339-345` | Consul token leaked in URL query string |
| M-5 | MEDIUM | Integer Overflow | `internal/geodns/mmdb.go:92,236-267` | MMDB treeSize uint32 overflow + no bounds check in readNode |
| M-6 | MEDIUM | Integer Overflow | `internal/cluster/gossip_encoding.go:19,43,46` | Gossip encoding uint16 truncation |
| M-7 | MEDIUM | Crypto Timing | `internal/middleware/oauth2/oauth2.go:239,248` | Non-constant-time issuer/audience comparison |
| M-8 | MEDIUM | Auth | `internal/cluster/security.go:244-253` | Cluster node auth: partial TCP read vulnerability |
| M-9 | MEDIUM | Auth | `internal/cluster/security.go:159-203` | Cluster node token replay within 5-min validity window |
| M-10 | MEDIUM | Injection | `internal/middleware/apikey/apikey.go:183-184` | JSON response injection in API key unauthorized |
| M-11 | MEDIUM | DoS | `internal/admin/events.go:24-29` | Unbounded SSE subscriber growth |
| M-12 | MEDIUM | DoS | `internal/admin/handlers_system.go` | Config reload DoS via rapid successful reloads |
| M-13 | MEDIUM | Resource | `internal/middleware/coalesce/coalesce.go:166-200` | Unbounded map/goroutine growth per unique key |
| L-1 | LOW | Injection | `internal/proxy/l7/proxy.go:683-693` | Missing CRLF sanitization in copyHeaders |
| L-2 | LOW | SSRF | `internal/health/health.go:462-508` | Exec health check template resolution from config |
| L-3 | LOW | Redirect | `internal/middleware/forcessl/forcessl.go:127-148` | Open redirect via unvalidated r.Host |
| L-4 | LOW | Redirect | `internal/middleware/botdetection/botdetection.go:348` | Bot challenge embeds unvalidated path |
| L-5 | LOW | Validation | `internal/proxy/l4/sni.go:418-430` | Weak SNI hostname validation (no char-set check) |
| L-6 | LOW | Crypto | `internal/middleware/basic/basic.go:59-61` | Unsalted SHA-256 password option |
| L-7 | LOW | Crypto | `internal/middleware/apikey/apikey.go:63-65` | Unsalted SHA-256 API key hashing option |
| L-8 | LOW | Auth | `internal/mcp/sse_transport.go:141-144` | SSE transport auth bypass when token empty |
| L-9 | LOW | Auth | `internal/admin/server.go` | No RBAC — any authenticated user has full admin |
| L-10 | LOW | Info Disc | `internal/mcp/mcp.go:538-544` | MCP error messages leak internal details |
| L-11 | LOW | DoS | `internal/cluster/transport.go:383-408` | 16 MiB InstallSnapshot payload limit |
| L-12 | LOW | Plugin | `internal/plugin/plugin.go:416,463` | Plugin loading without absolute path resolution |
| L-13 | LOW | Infra | `Dockerfile:63-66` | No network segmentation guidance |
| L-14 | LOW | Goroutine | `internal/proxy/l7/sse.go:289-313` | SSE readLineWithTimeout goroutine leak |
| L-15 | LOW | Time | `internal/engine/config.go:214,223` | time.Sleep in engine reload goroutines |

---

## Detailed Findings

### H-1: Raft TCP Transport Has No Authentication

**Severity:** HIGH | **Confidence:** VERIFIED
**File:** `internal/cluster/transport.go:136-148`, `internal/engine/cluster_init.go:58-77`

The Raft TCP transport accepts connections from any source without authentication, TLS, or peer verification. The `NodeAuthMiddleware` exists in `security.go` but is **not wired into the engine** during cluster initialization.

**Verification:** `initCluster()` creates a `TCPTransport` and calls `transport.Start()` directly without wrapping with `NodeAuthMiddleware`. Grep confirms `NodeAuthMiddleware` is only referenced in `cluster/security.go` and its test file — never in `engine/`.

**Attack vector:**
- Send `RequestVote` with a very high term → forces all nodes to step down → cluster outage
- Send `InstallSnapshot` with malicious data → replaces the configuration
- Send `AppendEntries` with false log entries → corrupts cluster state

**Remediation:** Wire `NodeAuthMiddleware` into engine's `initCluster()`:
```go
if clusterCfg.NodeAuth != nil {
    authListener := cluster.NewNodeAuthMiddleware(
        transport.Listener(),
        []byte(clusterCfg.NodeAuth.Secret),
        clusterCfg.NodeAuth.AllowedNodeIDs,
    )
    transport.SetListener(authListener)
}
```

---

### M-1: Exec Health Check — No Command Allowlist

**File:** `internal/health/health.go:463-508`

The `health_check.type: exec` config allows any arbitrary command with no restrictions on executables, paths, or argument counts. While `resolveExecTemplate` is simple string replacement (not Go templates), and args pass as `[]string` to `exec.Command` (no shell injection), a malicious config pushed via hot-reload or Raft consensus enables arbitrary code execution.

**Remediation:** Add command allowlist or require absolute paths with validation.

---

### M-2: Weak Health Check Address Validation (SSRF)

**File:** `internal/health/health.go:575-586`

`isInternalAddress()` only blocks 5 cloud metadata endpoints. Does NOT block `127.0.0.1`, RFC 1918, or link-local addresses. An attacker with admin API access can use health checks as an SSRF scanner to probe internal services.

**Remediation:** Add opt-in strict SSRF mode. Log warnings for private IP targets.

---

### M-3: Shadow Traffic `X-OLB-Shadow-Force` Header Bypass

**File:** `internal/proxy/l7/shadow.go:107-114`

The `X-OLB-Shadow-Force: true` header lets any client force request shadowing, bypassing the configured percentage. Shadow traffic forwards full request (path, headers, body) to internal shadow backends. No redirect protection on shadow client.

**Remediation:** Restrict force header to authenticated requests. Add `CheckRedirect` to shadow client.

---

### M-4: Consul Token Leaked in URL Query String

**File:** `internal/discovery/consul.go:339-345`

Consul ACL token appended as `?token=...` query parameter, appearing in server logs, proxy logs, error messages, and referrer headers.

**Remediation:** Send as `X-Consul-Token` HTTP header.

---

### M-5: MMDB treeSize uint32 Overflow + No Bounds Check

**File:** `internal/geodns/mmdb.go:92,236-267`

`treeSize` computation uses `uint32` multiplication which overflows for large MMDB files. `readNode()` computes `offset := nodeNum * 6/7/8` and indexes `r.data[offset]` without bounds checking. Malformed MMDB file causes out-of-bounds panic.

**Remediation:**
```go
treeSize := uint64(meta.recordSize) * 2 / 8 * uint64(meta.nodeCount)
if treeSize > uint64(len(data)) { return nil, fmt.Errorf("tree size exceeds file") }
// Add bounds check in readNode():
if offset+6 > uint32(len(r.data)) { return 0 }
```

---

### M-6: Gossip Encoding uint16 Truncation

**File:** `internal/cluster/gossip_encoding.go:19,43,46,120+`

Multiple locations cast `len(string)` to `uint16` without checking if value exceeds 65535. Silently truncates, corrupting cluster gossip protocol and causing desynchronization.

**Remediation:** Validate before encoding:
```go
if len(payload) > 65535 { return nil, fmt.Errorf("gossip: payload too large: %d", len(payload)) }
```

---

### M-7: OAuth2 Non-Constant-Time Issuer/Audience Comparison

**File:** `internal/middleware/oauth2/oauth2.go:239,248`

Issuer (`!=`) and audience (`==`) comparisons use plain string operators, enabling timing-based information leakage. JWT middleware correctly uses `subtle.ConstantTimeCompare` for the same checks.

**Remediation:** Use `subtle.ConstantTimeCompare` for both checks.

---

### M-8: Cluster Node Auth Partial Read Vulnerability

**File:** `internal/cluster/security.go:244-253`

Single `conn.Read(buf)` may return partial data due to TCP fragmentation. The `AUTH nodeID token` message may be incomplete, causing auth failures or parsing errors. No newline delimiter check.

**Remediation:** Use `bufio.NewReader` with `ReadString('\n')`.

---

### M-9: Cluster Node Token Replay Within Validity Window

**File:** `internal/cluster/security.go:159-203`

Within the 5-minute validity window, a captured token can be replayed without detection. No nonce or session tracking.

**Remediation:** Track used tokens per node within the validity window.

---

### M-10: JSON Response Injection in API Key Unauthorized

**File:** `internal/middleware/apikey/apikey.go:183-184`

`unauthorized()` concatenates message directly into JSON: `"{"error":"unauthorized","message":"` + message + `"}`. Currently only static strings passed, but structural weakness for future code.

**Remediation:** Use `json.NewEncoder(w).Encode()`.

---

### M-11: Unbounded SSE Subscriber Growth (Admin Events)

**File:** `internal/admin/events.go:24-29`

Admin event bus has no maximum subscriber limit. Each SSE client allocates a 16-entry channel. MCP SSE transport has `MaxClients` (100), but admin events do not.

**Remediation:** Add `maxSubscribers` to eventBus.

---

### M-12: Config Reload DoS via Rapid Successful Reloads

**File:** `internal/admin/handlers_system.go`

Rate limiter (60 req/min) and circuit breaker don't throttle successful reloads. Each reload creates new components and spawns cleanup goroutines. Rapid reloads cause goroutine buildup and health check storms.

**Remediation:** Add minimum cooldown period (e.g., 30 seconds) between successful reloads.

---

### M-13: Coalesce Middleware Unbounded Map/Goroutine Growth

**File:** `internal/middleware/coalesce/coalesce.go:166-200`

Every unique request key spawns a goroutine with `time.After(TTL)`. Under high load with diverse paths, creates unbounded map entries, goroutines, and timers.

**Remediation:** Add max-inflight limit; pass through without coalescing when exceeded.

---

## Low Severity Findings (Summary)

| # | File | Finding |
|---|------|---------|
| L-1 | `proxy/l7/proxy.go:683` | `copyHeaders` doesn't sanitize CRLF (Go stdlib mitigates at wire level) |
| L-2 | `health/health.go:462-508` | Exec health check template resolution from config |
| L-3 | `middleware/forcessl/forcessl.go:127` | Open redirect via unvalidated `r.Host` |
| L-4 | `middleware/botdetection/botdetection.go:348` | Bot challenge embeds unvalidated path in return param |
| L-5 | `proxy/l4/sni.go:418-430` | SNI hostname: no character-set validation |
| L-6 | `middleware/basic/basic.go:59-61` | Unsalted SHA-256 password option (bcrypt is default) |
| L-7 | `middleware/apikey/apikey.go:63-65` | Unsalted SHA-256 API key hashing option |
| L-8 | `mcp/sse_transport.go:141-144` | SSE transport silently allows unauthenticated access when token empty |
| L-9 | `admin/server.go` | No RBAC — any authenticated user has full admin access |
| L-10 | `mcp/mcp.go:538-544` | `sanitizeMCPError` truncates but doesn't redact internal details |
| L-11 | `cluster/transport.go:383-408` | 16 MiB InstallSnapshot allows memory pressure |
| L-12 | `plugin/plugin.go:416,463` | Plugin loading without absolute path resolution |
| L-13 | `Dockerfile:63-66` | No network segmentation guidance for public/admin/cluster |
| L-14 | `proxy/l7/sse.go:289-313` | readLineWithTimeout goroutine leak on timeout+cancelled context |
| L-15 | `engine/config.go:214,223` | `time.Sleep` instead of context-based cancellation in reload |

---

## Clean Categories (No Findings)

| Category | Status |
|----------|--------|
| SQL Injection | No SQL usage in codebase |
| NoSQL Injection | No NoSQL client usage |
| XXE | No XML parsing |
| SSTI | No template engine execution |
| XSS | No server-side rendering of user input |
| Deserialization | No `gob.Decode`, all JSON targets concrete structs |
| CGO | Zero CGO usage |
| Unsafe Package | Only in test code |
| File Upload | No upload endpoints |
| Path Traversal | Comprehensive multi-layer protection |
| Request Smuggling | Comprehensive CL/TE validation |
| Response Body Leaks | All bodies closed with `defer` |
| Signal Handling | Buffered channels with proper cleanup |

---

## Positive Security Observations

1. **Constant-time comparisons** throughout (bcrypt, bearer tokens, JWT, CSRF)
2. **bcrypt** default password hashing with proper cost factor
3. **TLS 1.2+ enforced**, ECDHE-only cipher suites, TLS 1.0/1.1 rejected
4. **Request smuggling protection** — comprehensive CL/TE conflict detection
5. **Admin localhost-only binding** when auth is not configured
6. **Rate limiting with memory caps** (100K visitor cap prevents state explosion)
7. **MCP HTTP transport** requires non-empty bearer token at construction
8. **CSRF double-submit cookie** with SameSite=Strict, token rotation, crypto-random
9. **WebSocket security** — hop-by-hop stripping, CRLF sanitization, path validation
10. **Config secrets redacted** from JSON via `json:"-"` tags
11. **Auth failure lockout** — per-IP lockout after 5 failures in 5 minutes
12. **Admin API body limits** — 1MB via `io.LimitReader` + `DisallowUnknownFields()`
13. **ACME client** uses `crypto/rand` for key generation
14. **X-Forwarded-For** only trusted from configured `TrustedProxies` CIDRs
15. **No hardcoded production secrets** in source code
16. **WAF module** with detection for SQLi, XSS, XXE, path traversal, CMDi, SSRF
17. **Panic recovery** in all bidirectional copy loops
18. **Config rollback** if >50% of pools have no healthy backends after reload

---

## Verified False Positives Eliminated

| Claim | Why Not a Finding |
|-------|-------------------|
| Concurrent config reload race | `Reload()` sets state to `StateReloading` under lock — concurrent calls rejected |
| ReDoS in WAF regex patterns | Go `regexp` uses RE2 (linear-time, no backtracking) |
| JSON unmarshaling into `interface{}` | All JSON targets concrete structs |
| Shadow body not restored | `io.MultiReader` restores consumed bytes (fixed in prior audit) |

---

## Previous Audit Status (2026-04-14, 31 findings)

All 31 findings from the previous HMAC-focused audit have been **remediated**:

| Priority | Finding | Status |
|----------|---------|--------|
| P0 | CRITICAL-01: Include timestamp in HMAC signature | FIXED |
| P0 | HIGH-01: Detect and reject body truncation | FIXED |
| P1 | MED-01..03: Hardcoded creds, empty secret, zero secrets | FIXED |
| P2 | MED-04..09: gRPC TLS, sticky sessions, SSE, errors, rate limiter, XFF | FIXED |
| P2 | LOW-01..14: All 14 LOW findings | FIXED |

---

## Remediation Status

### All HIGH + MEDIUM Findings: FIXED

| Finding | Description | Status |
|---------|-------------|--------|
| **H-1** | Wire `NodeAuthMiddleware` into engine `initCluster()` | **FIXED** |
| M-1 | Exec health check: require absolute path | **FIXED** |
| M-2 | Health check SSRF: block localhost/link-local | **FIXED** |
| M-3 | Remove `X-OLB-Shadow-Force` header bypass | **FIXED** |
| M-4 | Consul token moved to `X-Consul-Token` header | **FIXED** |
| M-5 | MMDB bounds check + uint64 treeSize | **FIXED** |
| M-6 | Gossip encoding uint16 overflow validation | **FIXED** |
| M-7 | OAuth2 constant-time issuer/audience comparison | **FIXED** |
| M-8 | Cluster auth: bufio.Reader for line reading | **FIXED** |
| M-9 | Cluster token replay protection cache | **FIXED** |
| M-10 | API key JSON response via json.Encoder | **FIXED** |
| M-11 | SSE subscriber limit (max 100) | **FIXED** |
| M-12 | Config reload cooldown (30s) | **FIXED** |
| M-13 | Coalesce max-inflight limit (10000) | **FIXED** |

### Build & Test Verification
- `go build ./cmd/olb/` — Success
- `go vet ./...` — No issues
- Tests — 3232 passed across modified packages
| M-13 | Add max-inflight limit to coalesce middleware |
| L-1 | Apply CRLF sanitization in `copyHeaders` |
| L-9 | Add RBAC to admin API |

### Remaining LOW Findings (Hardening)
| Finding | Action | Priority |
|---------|--------|----------|
| L-1 | Apply CRLF sanitization in `copyHeaders` | P3 |
| L-2 | Document exec health check template resolution risk | P3 |
| L-3 | Validate `r.Host` in ForceSSL redirect | P4 |
| L-4 | Validate path in bot challenge return param | P4 |
| L-5 | Add char-set check to SNI hostname validation | P4 |
| L-6, L-7 | Deprecate or salt SHA-256 in basic auth/apikey | P4 |
| L-8 | Warn when MCP SSE transport has no token | P3 |
| L-9 | Add RBAC to admin API | P3 |
| L-10 | Map MCP errors to generic messages | P3 |
| L-11 | Lower InstallSnapshot payload limit | P4 |
| L-12 | Plugin loading with absolute path resolution | P4 |
| L-13 | Document Docker network segmentation | P4 |
| L-14 | Fix SSE goroutine leak | P3 |
| L-15 | Use context in engine reload goroutines | P4 |

---

## Methodology

- **Phase 1 (Recon):** Full architecture mapping, 380 Go files analyzed, entry points identified
- **Phase 2 (Hunt):** 48 security skill categories via parallel specialized agents:
  - Injection & Code Execution (SQLi, NoSQLi, CMDi, XSS, SSTI, XXE, Header, RCE, Deserialization)
  - Access Control (Auth, AuthZ, Privilege Escalation, Session, Secrets, Data Exposure, Crypto)
  - Server-Side (SSRF, Path Traversal, File Upload, Open Redirect, CORS, WebSocket, Request Smuggling)
  - Logic & API (Race Conditions, Business Logic, API Security, Rate Limiting, JWT, DoS)
  - Go Language Deep Scan (unsafe, CGO, goroutine leaks, integer overflow, ReDoS, reflection, etc.)
- **Phase 3 (Verify):** Each HIGH/MEDIUM finding verified against actual source code
- **Phase 4 (Report):** Consolidated report with severity, attack vectors, and remediation

---

*Report generated by security-check skill — OpenLoadBalancer Full Rescan 2026-04-14*
