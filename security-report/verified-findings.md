# Verified Findings — OpenLoadBalancer Full Rescan (2026-04-14)

All 29 new findings verified against actual source code. Previous 31 findings (all remediated) documented separately.

## Verification Method

Each finding cross-referenced with:
1. Source code reads of affected files
2. Analysis of surrounding context (callers, data flow)
3. Assessment of exploitability in realistic deployment scenarios
4. Grep-based confirmation across the codebase

---

## Verified HIGH (1)

### H-1: Raft Transport Unauthenticated
- **Verified:** Read `internal/engine/cluster_init.go:58-77` — transport created and started directly
- **Verified:** Grep for `NodeAuthMiddleware` — only in `cluster/security.go` + test, never in `engine/`
- **Confidence:** 100% — `NodeAuthMiddleware` exists but is never wired into the engine
- **Impact:** Any network attacker reaching port 7947 can disrupt Raft consensus

---

## Verified MEDIUM (13)

### M-1: Exec Health Check No Allowlist
- **Verified:** Read `internal/health/health.go:463-508` — `exec.CommandContext(ctx, resolvedCmd, args...)`
- **Confidence:** 100% — no validation on command path or identity

### M-2: Weak Health Check SSRF
- **Verified:** Read `internal/health/health.go:575-586` — `isInternalAddress()` only blocks 5 cloud metadata IPs
- **Confidence:** 100% — no RFC1918/loopback/link-local blocking

### M-3: Shadow Force Header
- **Verified:** Read `internal/proxy/l7/shadow.go:107-114` — `X-OLB-Shadow-Force: true` bypass
- **Confidence:** 100% — any client can force shadowing

### M-4: Consul Token in URL
- **Verified:** Read `internal/discovery/consul.go:339-345` — token appended as query param
- **Confidence:** 100%

### M-5: MMDB Integer Overflow + No Bounds Check
- **Verified:** Read `internal/geodns/mmdb.go:92` — `uint32` multiplication for treeSize
- **Verified:** Read `internal/geodns/mmdb.go:236-267` — `readNode()` indexes `r.data[offset]` without bounds check
- **Confidence:** 100% — malformed MMDB causes out-of-bounds panic

### M-6: Gossip uint16 Truncation
- **Verified:** Read `internal/cluster/gossip_encoding.go:19,43,46` — `uint16(len(...))` without overflow check
- **Confidence:** 100% — payloads >65535 bytes silently truncated

### M-7: OAuth2 Non-Constant-Time Comparison
- **Verified:** Read `internal/middleware/oauth2/oauth2.go:239,248` — plain `==`/`!=`
- **Verified:** Read `internal/middleware/jwt/jwt.go` — JWT uses `subtle.ConstantTimeCompare` correctly
- **Confidence:** 100% — inconsistency confirmed

### M-8: Cluster Auth Partial Read
- **Verified:** Read `internal/cluster/security.go:244-253` — single `conn.Read(buf)` for auth message
- **Confidence:** 95% — TCP fragmentation can cause partial reads

### M-9: Cluster Token Replay
- **Verified:** Read `internal/cluster/security.go:159-203` — timestamp-based only, no nonce tracking
- **Confidence:** 95% — replayable within 5-minute window

### M-10: API Key JSON Injection
- **Verified:** Read `internal/middleware/apikey/apikey.go:183-184` — string concatenation into JSON
- **Confidence:** 90% — currently static strings only, structural weakness

### M-11: Unbounded SSE Subscribers
- **Verified:** Read `internal/admin/events.go:24-29` — no limit check in `subscribe()`
- **Verified:** Compared with `internal/mcp/sse_transport.go` which has `MaxClients`
- **Confidence:** 100%

### M-12: Reload DoS
- **Verified:** Read `internal/engine/lifecycle.go:457-494` — reload sets state correctly
- **Verified:** Read `internal/admin/circuit_breaker.go` — counts errors, not successes
- **Confidence:** 85% — requires auth + rate limit proximity

### M-13: Coalesce Unbounded Growth
- **Verified:** Read `internal/middleware/coalesce/coalesce.go:166-200` — goroutine per unique key
- **Confidence:** 95%

---

## Verified LOW (15)

All 15 low-severity findings confirmed by source code inspection. See SECURITY-REPORT.md for details.

---

## False Positives Eliminated

| Claim | Investigation | Verdict |
|-------|--------------|---------|
| Concurrent config reload race | Read `lifecycle.go:457-494` — state check blocks concurrent reloads | NOT A FINDING |
| ReDoS in WAF regex | Go `regexp` uses RE2 (linear-time matching) | NOT A FINDING |
| Shadow body not restored | `io.MultiReader` restores consumed bytes (fixed in prior audit) | NOT A FINDING |

---

*Verified by security-check on 2026-04-14 (Full Rescan)*
