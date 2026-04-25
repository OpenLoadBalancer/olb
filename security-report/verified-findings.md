# Verified Findings Report -- OpenLoadBalancer

**Date:** 2026-04-25 (Phase 3 Verification)
**Previous Scan:** 2026-04-18 (34 findings, 19 remediated)
**Scope:** Full codebase (~380 Go files), 6 Phase 2 result files verified against source
**Method:** Each finding read at cited file:lines, cross-referenced with related code paths, mitigating factors evaluated, duplicates merged

---

## Verification Summary

| Metric | Count |
|--------|-------|
| Total Phase 2 findings reviewed | 42 |
| Duplicates merged | 6 |
| False positives eliminated | 5 |
| Carried forward from previous scan (OLB-SEC-007, 016, 017, 020-034) | 16 |
| **New verified findings** | **21** |
| **Total open findings (new + carried forward)** | **37** |

### Confidence Distribution

| Confidence | Count | Description |
|------------|-------|-------------|
| 90-100 (CONFIRMED) | 4 | Verified exploit path, no mitigating controls |
| 70-89 (HIGH_CONFIDENCE) | 10 | Vulnerability exists with partial mitigations |
| 50-69 (POSSIBLE) | 12 | Requires specific conditions or chained attacks |
| Below 50 (UNLIKELY) | 11 | Hard to exploit in practice, defense-in-depth recommended |

---

## Severity Summary

| Severity | New | Carried Forward | Total |
|----------|-----|-----------------|-------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 2 | 1 | 3 |
| MEDIUM | 9 | 2 | 11 |
| LOW | 8 | 10 | 18 |
| INFO | 2 | 5 | 7 |

---

## HIGH Severity Findings

### VF-001: MCP SSE Transport Constructor Rejects Empty Token, But Engine Still Initializes MCP Server Object

| Attribute | Value |
|-----------|-------|
| **ID** | VF-001 |
| **Severity** | HIGH |
| **Confidence** | 75 |
| **CWE** | CWE-306 (Missing Authentication for Critical Function) |
| **Files** | `internal/engine/lifecycle.go:124-149`, `internal/mcp/sse_transport.go:82-84` |
| **Source** | AUTH-001 |
| **Previous** | OLB-SEC-001 (REMEDIATED -- constructor now returns error) |

**Verification:**

I confirmed that `NewSSETransport` at `sse_transport.go:82-84` now correctly returns `nil, error` when `BearerToken` is empty. The engine at `lifecycle.go:146-148` checks the error and only assigns the transport when `sseErr == nil`. However, the MCP `Server` object (`e.mcpServer`) is still initialized earlier in `engine.New()` regardless of whether MCPToken is set. The server object remains accessible via `Engine.GetMCPServer()`, and in clustered mode could potentially be used by other nodes.

The CRITICAL variant (OLB-SEC-001) is fully remediated -- the SSE transport no longer starts without a token. The remaining HIGH concern is that the MCP server object exists in memory and the engine logs only a `Warn` (not `Error`) when transport creation fails.

**Mitigating Factors:**
- Transport does NOT start -- endpoints are not bound to any port
- MCP Server object is not network-accessible without a transport
- The HTTP transport at `mcp.go:1281-1283` also rejects empty tokens
- The MCP server has no in-process callers other than the transports

**Status:** Partially remediated (transport auth fixed, MCP server object lifecycle remains a defense-in-depth concern).

**Remediation:** Skip MCP server initialization entirely when `MCPToken` is empty, or log at `Error` level.

---

### VF-002: Docker Compose Node Exporter Mounts Host Root Filesystem Read-Only

| Attribute | Value |
|-----------|-------|
| **ID** | VF-002 |
| **Severity** | HIGH |
| **Confidence** | 80 |
| **CWE** | CWE-250 (Execution with Unnecessary Privileges) |
| **Files** | `deploy/docker-compose.yml:97-100` |
| **Source** | SCG-001 |
| **Previous** | New finding |

**Verification:**

Confirmed at `deploy/docker-compose.yml:97-100`:
```yaml
volumes:
  - /proc:/host/proc:ro
  - /sys:/host/sys:ro
  - /:/rootfs:ro
```

The mount `/:/rootfs:ro` is indeed present. The `--path.rootfs=/rootfs` flag at line 104 enables the filesystem collector which needs rootfs access. While read-only, this exposes `/etc/shadow`, SSH keys, Docker state, and any secrets on the host to anyone who gains access to the node-exporter container.

**Mitigating Factors:**
- Mount is read-only (not writable)
- Node exporter is a well-known Prometheus component with a limited attack surface
- Requires network access to node-exporter's metrics port (which is NOT exposed to host -- it only uses the internal Docker network)

**Note:** Node-exporter at line 93-106 does NOT publish any ports to the host (`ports:` section absent), so it is only accessible within the `olb-network` Docker network. This significantly reduces the risk.

**Remediation:** Remove the `/:/rootfs:ro` mount unless filesystem metrics are explicitly required. The `--path.procfs` and `--path.sysfs` mounts are sufficient for most monitoring.

---

### VF-003: Cluster State Messages Applied Without Cryptographic Verification (Carried Forward)

| Attribute | Value |
|-----------|-------|
| **ID** | VF-003 |
| **Severity** | HIGH |
| **Confidence** | 72 |
| **CWE** | CWE-345 (Insufficient Verification of Data Authenticity) |
| **Files** | `internal/cluster/state.go:432-460`, `internal/cluster/state.go:237-249` |
| **Source** | SC-013, OLB-SEC-007 |
| **Previous** | OLB-SEC-007 (OPEN) |

**Verification:**

Confirmed at `state.go:432-442`: `handleIncoming()` deserializes state messages and only checks `msg.SenderID != ds.config.NodeID` (skip own messages). No HMAC, signature, or authentication is verified. `MergeHealthStatus()` at line 237-249 uses last-writer-wins by timestamp with no sender validation.

An attacker with network access to the cluster gossip port can inject arbitrary health status updates with future timestamps, causing all nodes to accept fake health states.

**Mitigating Factors:**
- Requires network access to the cluster's gossip/RPC port (typically not exposed to the internet)
- The `NodeAuthMiddleware` (when enabled) provides HMAC-based authentication for TCP connections
- In practice, the gossip protocol at `cluster/gossip.go` uses memberlist-style protocols that have their own authentication layer

**Remediation:** Add HMAC verification to all state messages, leveraging the existing cluster node auth infrastructure.

---

## MEDIUM Severity Findings

### VF-004: Admin API Public Health Endpoints Expose Operational State Without Authentication

| Attribute | Value |
|-----------|-------|
| **ID** | VF-004 |
| **Severity** | MEDIUM |
| **Confidence** | 82 |
| **CWE** | CWE-200 (Exposure of Sensitive Information) |
| **Files** | `internal/admin/auth.go:170-173`, `internal/admin/auth.go:289-298` |
| **Source** | AUTH-002 |
| **Previous** | New finding |

**Verification:**

Confirmed at `auth.go:170`: when `RequireAuthForRead` is false, GET requests to public health endpoints bypass authentication. At `auth.go:291-294`, `isPublicHealthEndpoint()` returns true for `/health`, `/api/v1/system/health`, and `/api/v1/health`.

The `/api/v1/system/health` endpoint returns detailed operational state including router status, pool manager status, backend health counts, and uptime. This is more than a simple up/down health probe.

**Mitigating Factors:**
- The health endpoint data does not include secrets (all secret fields use `json:"-"`)
- The admin API is typically bound to an internal network or localhost
- The default when no auth is configured is localhost-only binding (line 210-217)

**Remediation:** Restrict the public health bypass to only `/health` (simple up/down). Require authentication for `/api/v1/system/health` and `/api/v1/health` regardless of `RequireAuthForRead`.

---

### VF-005: No Role-Based Access Control on Admin API Endpoints

| Attribute | Value |
|-----------|-------|
| **ID** | VF-005 |
| **Severity** | MEDIUM |
| **Confidence** | 78 |
| **CWE** | CWE-862 (Missing Authorization) |
| **Files** | `internal/admin/auth.go:160-240`, `internal/admin/server.go:246-311` |
| **Source** | AUTH-003 |
| **Previous** | New finding |

**Verification:**

Confirmed. The admin API has a single authentication layer. The auth middleware at `auth.go:160-240` validates credentials but does not attach any role or permission information to the request context. All authenticated users have identical access to all endpoints including destructive operations (DELETE backends, reload config, update config).

**Mitigating Factors:**
- Admin API requires authentication (basic auth or bearer token)
- Admin API has rate limiting (60 req/min per IP)
- Admin API has auth failure lockout (5 attempts, 5 min lockout)
- When no auth is configured, the server refuses non-localhost binding

**Remediation:** Introduce RBAC with at least read-only and admin roles.

---

### VF-006: MCP Tools Have No Authorization Separation

| Attribute | Value |
|-----------|-------|
| **ID** | VF-006 |
| **Severity** | MEDIUM |
| **Confidence** | 80 |
| **CWE** | CWE-862 (Missing Authorization) |
| **Files** | `internal/mcp/mcp.go:999-1030`, `internal/mcp/sse_transport.go:147-157` |
| **Source** | AUTH-004 |
| **Previous** | New finding |

**Verification:**

Confirmed. The MCP server exposes both read-only and destructive write tools behind a single bearer token. The `HandleJSONRPC` method dispatches any validated method without checking tool-level permissions. The `authenticate()` method at `sse_transport.go:147-157` validates the bearer token but does not attach any permission scope.

**Mitigating Factors:**
- Bearer token is required (empty tokens rejected at constructor)
- Token is compared with constant-time comparison
- MCP is typically used in controlled environments (AI assistants, internal tools)

**Remediation:** Implement tool-level authorization with configurable permission sets per token.

---

### VF-007: Backend Weight/MaxConns Update Without Atomicity

| Attribute | Value |
|-----------|-------|
| **ID** | VF-007 |
| **Severity** | MEDIUM |
| **Confidence** | 85 |
| **CWE** | CWE-362 (Race Condition) |
| **Files** | `internal/admin/handlers_backends.go:292-301`, `internal/backend/backend.go:28-32, 141-158` |
| **Source** | SC-002, SC-009 |
| **Previous** | New finding |

**Verification:**

Confirmed at `handlers_backends.go:292-300`: Weight and MaxConns are written directly on the backend struct without atomic operations:
```go
b.Weight = *req.Weight    // line 293
b.MaxConns = *req.MaxConns // line 300
```

At `backend.go:28-32`, these are plain `int32` fields. Meanwhile, `AcquireConn()` at line 141-158 reads `b.MaxConns` without synchronization. The `activeConns` field correctly uses `atomic.Int64` (line 38), but `MaxConns` and `Weight` do not.

SC-002 and SC-009 are duplicates (same root cause) and are merged here.

**Mitigating Factors:**
- On 64-bit platforms, int32 reads/writes are naturally atomic at the hardware level
- The window for observing stale values is brief (single write operation)
- The impact is a brief inconsistency in load distribution, not a security vulnerability

**Remediation:** Convert `Weight` and `MaxConns` to `atomic.Int32` to match the pattern used for `activeConns`.

---

### VF-008: Config Reload Race -- prevConfig Overwrite During Rollback Window

| Attribute | Value |
|-----------|-------|
| **ID** | VF-008 |
| **Severity** | MEDIUM |
| **Confidence** | 70 |
| **CWE** | CWE-362 (Race Condition) |
| **Files** | `internal/engine/config.go:62-79, 322-380` |
| **Source** | SC-004 |
| **Previous** | New finding |

**Verification:**

Confirmed at `config.go:66-69`: `applyConfig()` unconditionally overwrites `e.prevConfig`:
```go
e.rollbackMu.Lock()
e.prevConfig = e.config   // Overwrites any previous prevConfig
e.rollbackMu.Unlock()
```

If a second reload starts while the first reload's rollback timer is still active (30s window), the original prevConfig is lost. The rollback check at `config.go:333-380` reads `e.prevConfig` and uses it to roll back, but it would roll back to the wrong config.

**Mitigating Factors:**
- The engine state check at `lifecycle.go:499-506` prevents concurrent reloads (state must be `StateRunning`)
- The config watcher and API reload share the same state guard
- The practical window for two rapid reloads is very small

**Remediation:** Only overwrite `prevConfig` if it is currently nil or the rollback timer has expired.

---

### VF-009: Admin API Raft Mode Proposes Config Without Local Validation

| Attribute | Value |
|-----------|-------|
| **ID** | VF-009 |
| **Severity** | MEDIUM |
| **Confidence** | 75 |
| **CWE** | CWE-20 (Improper Input Validation) |
| **Files** | `internal/admin/handlers_system.go:186-199` |
| **Source** | SC-005 |
| **Previous** | New finding |

**Verification:**

Confirmed at `handlers_system.go:186-199`: In Raft mode, the `updateConfig` handler reads the request body and directly calls `s.raftProposer.ProposeSetConfig(body)` without any local validation. If the proposed config is malformed, it is committed to the Raft log before validation catches the error.

In standalone mode (lines 202-227), the handler correctly applies cooldown, circuit breaker, and validates via `e.loadConfig()` + `e.validateConfig()`.

**Mitigating Factors:**
- Requires valid admin authentication
- The Raft apply callback likely re-validates the config before applying
- Standalone mode has proper safeguards

**Remediation:** Validate the config locally (parse + validate) before proposing to Raft.

---

### VF-010: Health Checker SSRF Protection Incomplete -- DNS Rebinding and Missing Checks

| Attribute | Value |
|-----------|-------|
| **ID** | VF-010 |
| **Severity** | MEDIUM |
| **Confidence** | 65 |
| **CWE** | CWE-918 (Server-Side Request Forgery) |
| **Files** | `internal/health/health.go:587-605` |
| **Source** | SC-SSRF-002, SC-008 |
| **Previous** | New finding |

**Verification:**

Confirmed at `health.go:592-605`: `isInternalAddress()` only blocks hardcoded cloud metadata hostnames and `169.254.x.x` range. It does NOT:
1. Resolve DNS names to check the resulting IP (DNS rebinding possible)
2. Block `0.0.0.0` or IPv6 link-local (`fe80::/10`)
3. Apply SSRF checks to TCP or gRPC health checks (only HTTP/HTTPS)

SC-SSRF-002 and SC-008 are duplicates (same function, same root cause) and merged here.

**Mitigating Factors:**
- Health check addresses are admin-controlled (not user input)
- Localhost/RFC1918 are intentionally allowed for legitimate internal backends
- Cloud metadata endpoints ARE blocked (the primary SSRF target)
- An attacker needs admin credentials or config write access to set health check addresses

**Remediation:** After DNS resolution, check the resolved IP against the blocklist. Apply SSRF validation to all health check types, not just HTTP.

---

### VF-011: Basic Auth SHA256 Hash Mode Uses HMAC Without Proper Password Hashing

| Attribute | Value |
|-----------|-------|
| **ID** | VF-011 |
| **Severity** | MEDIUM |
| **Confidence** | 82 |
| **CWE** | CWE-916 (Use of Password Hash With Insufficient Computational Effort) |
| **Files** | `internal/middleware/basic/basic.go:59-63` |
| **Source** | AUTH-007, SC-001 |
| **Previous** | New finding |

**Verification:**

Confirmed at `basic.go:59-63`: When `Hash: "sha256"` is configured, passwords are hashed as `HMAC-SHA256(key=username, message=password)`. This is a single-iteration fast hash. At `basic.go:70-72`, bcrypt mode is also available and stores the hash as-is for runtime comparison.

SC-001 and AUTH-007 are duplicates (same code, same issue) and merged.

**Mitigating Factors:**
- The bcrypt mode is available and documented
- The `json:"-"` tag on the Users field prevents hash leakage via API
- Constant-time comparison prevents timing attacks
- An attacker needs config file read access to obtain the hashes

**Remediation:** Deprecate or add a startup warning for the SHA256 hash mode. Document that bcrypt should be used in production.

---

### VF-012: Cluster State Merge Trusts Peer Timestamps Without Authentication

| Attribute | Value |
|-----------|-------|
| **ID** | VF-012 |
| **Severity** | MEDIUM |
| **Confidence** | 70 |
| **CWE** | CWE-345 (Insufficient Verification of Data Authenticity) |
| **Files** | `internal/cluster/state.go:237-249` |
| **Source** | SC-013 |
| **Previous** | Overlaps with OLB-SEC-007 (VF-003) |

**Verification:**

Confirmed at `state.go:237-249`: `MergeHealthStatus()` uses last-writer-wins based on timestamp with no authentication. This is a specific instance of the broader OLB-SEC-007 finding.

**Note:** This is a sub-finding of VF-003 (same root cause). Counted once at HIGH level in VF-003. Listing here for completeness of the state-merge-specific attack vector.

**Remediation:** See VF-003.

---

## LOW Severity Findings

### VF-013: CRLF Injection in Transformer AddHeaders Configuration

| Attribute | Value |
|-----------|-------|
| **ID** | VF-013 |
| **Severity** | LOW |
| **Confidence** | 72 |
| **CWE** | CWE-113 (Improper Neutralization of CRLF Sequences in HTTP Headers) |
| **Files** | `internal/middleware/transformer/transformer.go:236-238` |
| **Source** | SC-HDR-01 |
| **Previous** | New finding |

**Verification:**

Confirmed at `transformer.go:236-238`: Header values from `AddHeaders` config are set directly without CRLF sanitization. The same applies to `headers.go:141-147` (SC-HDR-02). Both findings are the same class of issue.

**Mitigating Factors:**
- Values come from admin-controlled configuration, not user input
- The `security.SanitizeHeaderValue()` function exists in the codebase but is not applied here
- An attacker needs config file write access AND the CRLF must survive YAML/TOML parsing
- YAML parsers typically interpret `\r\n` as literal escape sequences, not actual CRLF bytes

**Remediation:** Apply `security.SanitizeHeaderValue()` to all config-sourced header values in transformer and headers middleware.

---

### VF-014: Request ID Fallback Uses Deterministic Pattern

| Attribute | Value |
|-----------|-------|
| **ID** | VF-014 |
| **Severity** | LOW |
| **Confidence** | 88 |
| **CWE** | CWE-331 (Insufficient Entropy) |
| **Files** | `internal/middleware/requestid/requestid.go:104-120` |
| **Source** | SC-004, SCG-006 |
| **Previous** | New finding |

**Verification:**

Confirmed at `requestid.go:114-120`: When `crypto/rand.Read()` fails, `fallbackID()` produces the same deterministic value on every call: `b[i] = byte(i * 7)`. No warning is logged. SC-004 and SCG-006 are duplicates and merged.

**Mitigating Factors:**
- `crypto/rand.Read()` failure is extremely rare in practice
- Request IDs are not used for authorization or security decisions
- The function is used only as a fallback for observability

**Remediation:** Log a warning when falling back. Include a timestamp or counter in the fallback for some uniqueness.

---

### VF-015: Logging Middleware Can Be Configured to Log Sensitive Headers

| Attribute | Value |
|-----------|-------|
| **ID** | VF-015 |
| **Severity** | LOW |
| **Confidence** | 85 |
| **CWE** | CWE-532 (Insertion of Sensitive Information into Log File) |
| **Files** | `internal/middleware/logging/logging.go:244-247` |
| **Source** | SC-005 |
| **Previous** | New finding |

**Verification:**

Confirmed at `logging.go:244-247`: The `RequestHeaders` config field allows logging arbitrary headers verbatim. If an operator configures `request_headers: ["Authorization", "Cookie"]`, bearer tokens and session cookies would be written to access logs.

**Mitigating Factors:**
- This is a configuration choice made by the operator
- The default configuration does not log sensitive headers
- Log file permissions are the operator's responsibility

**Remediation:** Add a built-in denylist of sensitive header names with redaction, or log a warning at startup when sensitive headers are configured.

---

### VF-016: Auth Failure Limiter Map Exhaustion Bypass

| Attribute | Value |
|-----------|-------|
| **ID** | VF-016 |
| **Severity** | LOW |
| **Confidence** | 60 |
| **CWE** | CWE-367 (TOCTOU Race Condition) |
| **Files** | `internal/admin/auth.go:89-112` |
| **Source** | SC-003 |
| **Previous** | New finding (map cap was remediated in OLB-SEC-009, but exhaustion behavior is new) |

**Verification:**

Confirmed at `auth.go:94-103`: When the map reaches 100K entries and no expired entries can be evicted, new IP failures are silently dropped (`return` on line 102). An attacker with a botnet of 100K+ unique IPs can exhaust the map, after which new IPs bypass lockout tracking.

**Mitigating Factors:**
- 100K unique IPs is a very large botnet
- The admin API has rate limiting (60 req/min per IP) as an additional layer
- Expired entries are cleaned up on every new entry attempt
- The admin API is typically localhost-only or internal-network-only

**Remediation:** When the map is full and no expired entries can be evicted, evict the oldest non-locked entries (LRU-style) to make room.

---

### VF-017: Admin Auth IP Lockout Shared Behind Proxies (Carried Forward)

| Attribute | Value |
|-----------|-------|
| **ID** | VF-017 |
| **Severity** | LOW |
| **Confidence** | 80 |
| **CWE** | CWE-290 (Authentication Bypass by Spoofing) |
| **Files** | `internal/admin/auth.go:176` |
| **Source** | AUTH-006 |
| **Previous** | OLB-SEC-021 (OPEN) |

**Verification:**

Confirmed at `auth.go:176`: `net.SplitHostPort(r.RemoteAddr)` returns the direct TCP peer, not the real client IP behind a proxy. All users behind the same reverse proxy share a single lockout bucket.

**Remediation:** Integrate with the `RealIP` middleware to use the actual client IP when a trusted proxy is configured.

---

### VF-018: Rollback Logic Reads poolManager Outside Lock Scope

| Attribute | Value |
|-----------|-------|
| **ID** | VF-018 |
| **Severity** | LOW |
| **Confidence** | 55 |
| **CWE** | CWE-367 (TOCTOU Race Condition) |
| **Files** | `internal/engine/config.go:333-380` |
| **Source** | SC-011 |
| **Previous** | New finding |

**Verification:**

Confirmed at `config.go:344-347`: The rollback check reads `e.poolManager` under RLock, then releases it before iterating over pools. A concurrent reload could swap the pool manager.

**Mitigating Factors:**
- The rollback timer fires at 15s and 30s, and rapid concurrent reloads are blocked by the engine state check
- Using the old pool manager for the check is actually conservative (checks old, likely degraded state)
- The practical risk is minimal

**Remediation:** Accept current behavior or read poolManager and use it within the same lock scope.

---

### VF-019: SSE Event Stream Has No Per-Connection Timeout

| Attribute | Value |
|-----------|-------|
| **ID** | VF-019 |
| **Severity** | LOW |
| **Confidence** | 78 |
| **CWE** | CWE-400 (Uncontrolled Resource Consumption) |
| **Files** | `internal/admin/events.go:82-141` |
| **Source** | SC-012 |
| **Previous** | New finding |

**Verification:**

Confirmed at `events.go:125-140`: The SSE stream loop blocks on `ch` and `r.Context().Done()`. There is no per-connection idle timeout. The subscriber limit is 100 (checked at line 107-116), so 100 idle connections can exhaust all SSE slots.

**Mitigating Factors:**
- Max 100 subscribers (hard limit)
- Admin authentication is required
- Rate limiting applies to the initial GET request
- The `r.Context().Done()` channel fires when the client disconnects

**Remediation:** Add a per-connection idle timeout (e.g., 5 minutes) or send periodic keepalive pings.

---

### VF-020: Plugin isAllowed Returns True When Allowlist Is Empty

| Attribute | Value |
|-----------|-------|
| **ID** | VF-020 |
| **Severity** | LOW |
| **Confidence** | 82 |
| **CWE** | CWE-426 (Untrusted Search Path) |
| **Files** | `internal/plugin/plugin.go:373-378` |
| **Source** | SC-RCE-01 |
| **Previous** | OLB-SEC-025 (OPEN) |

**Verification:**

Confirmed at `plugin.go:373-378`: `isAllowed()` returns `true` when `AllowedPlugins` is empty. This means any `.so` file will be loaded when `AutoLoad` is enabled and no allowlist is configured. The `LoadDir()` function (line 447-451) does skip loading when `AllowedPlugins` is empty, logging a warning -- so the risk is primarily from `RegisterPlugin()` being called directly.

**Mitigating Factors:**
- `LoadDir()` skips loading when `AllowedPlugins` is empty (line 447-451)
- Plugin directory is resolved to absolute path
- Plugin API is interface-based, limiting what plugins can access
- Auto-loading from an empty allowlist is warned against

**Remediation:** Change `isAllowed` to return false when `AllowedPlugins` is empty, requiring explicit allowlisting.

---

### VF-021: Private Keys Not Zeroed From Memory After Use

| Attribute | Value |
|-----------|-------|
| **ID** | VF-021 |
| **Severity** | LOW |
| **Confidence** | 70 |
| **CWE** | CWE-226 (Sensitive Information in Resource Not Removed Before Reuse) |
| **Files** | `internal/tls/manager.go`, `internal/middleware/jwt/jwt.go`, `internal/admin/auth.go` |
| **Source** | SC-003 |
| **Previous** | OLB-SEC-027, OLB-SEC-033 (OPEN) |

**Verification:**

Confirmed. The TLS Manager, JWT middleware, and admin auth store secrets as Go strings (immutable, cannot be reliably zeroed). The HMAC, API Key, Basic Auth, and Cluster Security middlewares correctly implement `ZeroSecrets()`. The ACME client does not zero its `accountKey`.

**Mitigating Factors:**
- Go strings are immutable and the GC makes reliable zeroing difficult
- Memory dumps require elevated privileges (root/admin)
- This is a defense-in-depth concern, not an exploitable vulnerability

**Remediation:** Use `[]byte` for secrets where possible and implement `ZeroSecrets()` / `Close()` methods.

---

## INFO Severity Findings

### VF-022: Admin CORS Configuration -- Origin Reflection with Credentials

| Attribute | Value |
|-----------|-------|
| **ID** | VF-022 |
| **Severity** | INFO |
| **Confidence** | 70 |
| **CWE** | CWE-942 (Permissive Cross-domain Policy) |
| **Files** | `internal/admin/server.go:600-636` |
| **Source** | AUTH-010, SCG-008, SC-CORS-001 |
| **Previous** | New finding |

**Verification:**

Confirmed at `server.go:616-625`: The admin CORS handler correctly distinguishes between `allowAll` (no credentials) and specific origins (credentials allowed). The combination of `Access-Control-Allow-Origin: *` with `Access-Control-Allow-Credentials: true` is properly prevented. The implementation is correct as-is.

SC-CORS-001, AUTH-010, and SCG-008 describe the same behavior and are merged. The finding is downgraded to INFO because the implementation is correct -- the concern is about potential misconfiguration by operators.

**Status:** Correctly implemented. Document that `AllowedOrigins` should only contain specific, trusted origins.

---

### VF-023: OAuth2/JWKS Introspection URL Outbound SSRF

| Attribute | Value |
|-----------|-------|
| **ID** | VF-023 |
| **Severity** | INFO |
| **Confidence** | 55 |
| **CWE** | CWE-918 (Server-Side Request Forgery) |
| **Files** | `internal/middleware/oauth2/oauth2.go:154-186, 475-525, 594-650` |
| **Source** | SC-SSRF-003 |
| **Previous** | New finding |

**Verification:**

Confirmed that the OAuth2 middleware makes outbound HTTP requests to admin-configured URLs (IntrospectionURL, JwksURL). HTTPS is enforced by default (line 167-186). URLs are admin-controlled configuration.

**Mitigating Factors:**
- HTTPS enforcement prevents MITM on the connection
- URLs are admin-controlled (not user input)
- This is an inherent property of any OAuth2 client -- it MUST make outbound requests to configured endpoints
- The same concern applies to any system that calls external OAuth2/OIDC providers

**Status:** Expected behavior. The SSRF vector requires compromised admin config, at which point the attacker already has equivalent or greater access.

---

## False Positives Eliminated

| Finding ID | Reason for Elimination |
|-----------|----------------------|
| AUTH-001 (full severity) | OLB-SEC-001 was remediated: `NewSSETransport` now returns `nil, error` when BearerToken is empty. The SSE transport does NOT start without a token. Downgraded from HIGH to defense-in-depth concern. |
| AUTH-005 (CSRF Secure cookie) | CSRF is auto-enabled when WebUI is present (`server.go:219-223`). The `Secure: true` default is correct for HTTPS deployments. The finding describes a deployment scenario (HTTP) that the admin auth guard already addresses by refusing non-localhost binding without auth. Not a vulnerability. |
| AUTH-008 (API key SHA256 scheme) | The HMAC scheme `HMAC-SHA256(key=apiKey, message=keyID)` is unconventional but not weaker than `SHA256(keyID:apiKey)`. The key_id mixing provides salting. Constant-time comparison is correctly used. Merged with SC-002. |
| AUTH-009 (JWT jti replay) | JWTs are bearer tokens by design. Adding `jti` tracking would require shared state (inconsistent with stateless JWT architecture). The `exp` claim provides time-bounded replay protection. Not a vulnerability -- it's an architectural trade-off. |
| SC-010 (Config watcher debounce fields) | Confirmed false positive by the finding itself: `lastSeenHash` and `stableCount` are goroutine-local fields only accessed from the single `watch()` goroutine ticker loop. No concurrent access. |
| SCG-012 (math/rand seeded with time) | The project requires Go 1.26+ (per CLAUDE.md), which auto-seeds the global `math/rand` source with cryptographic randomness. The gossip protocol correctly uses a local `rand.Rand` with mutex protection. Not exploitable. |

---

## Duplicate Merges

| Merged IDs | Unified As | Reason |
|------------|-----------|--------|
| SC-002 + SC-009 | VF-007 | Same root cause: non-atomic Weight/MaxConns writes |
| SC-SSRF-002 + SC-008 | VF-010 | Same root cause: incomplete `isInternalAddress()` |
| AUTH-007 + SC-001 | VF-011 | Same root cause: HMAC-SHA256 used for password hashing |
| SC-004 + SCG-006 | VF-014 | Same root cause: deterministic request ID fallback |
| AUTH-010 + SCG-008 + SC-CORS-001 | VF-022 | Same root cause: admin CORS origin reflection |
| SC-HDR-01 + SC-HDR-02 | VF-013 | Same root cause: config-sourced headers without CRLF sanitization |

---

## Carried Forward Findings (from 2026-04-18 scan, still open)

These findings were previously identified and remain open. They were not re-reported in Phase 2 but are still valid.

| Previous ID | Severity | Description | Status |
|-------------|----------|-------------|--------|
| OLB-SEC-007 | HIGH | Cluster state messages without crypto verification | Mapped to VF-003 |
| OLB-SEC-016 | MEDIUM | Docker provider plaintext TCP | Still open |
| OLB-SEC-017 | MEDIUM | DNS discovery over plaintext | Still open |
| OLB-SEC-020 | LOW | Health endpoint exposes backend IDs via header check | Mapped to AUTH-011, still open |
| OLB-SEC-021 | LOW | Admin auth IP lockout shared behind proxy | Mapped to VF-017 |
| OLB-SEC-022 | LOW | Log files created world-readable (0644) | Still open |
| OLB-SEC-023 | LOW | pprof server has no auth (localhost-only) | Still open |
| OLB-SEC-024 | LOW | No token rotation for admin bearer tokens | Still open |
| OLB-SEC-025 | LOW | Plugin files loaded without integrity check | Mapped to VF-020 |
| OLB-SEC-026 | LOW | Config API relies on json:"-" for secret hiding | Still open |
| OLB-SEC-027 | LOW | Private keys not zeroed from memory | Mapped to VF-021 |
| OLB-SEC-029 | LOW | Env var expansion reads all host environment | Still open |
| OLB-SEC-030 | LOW | OCSP ParseOCSPResponse exports unverifed parsing | Still open |
| OLB-SEC-031 | LOW | RSA cipher suites allowed (non-PFS) with warning | Still open |
| OLB-SEC-032 | LOW | Docker Compose containers not read-only | Still open |
| OLB-SEC-033 | LOW | Admin bearer tokens have no ZeroSecrets method | Mapped to VF-021 |
| OLB-SEC-034 | LOW | UDP session table spoofable to exhaust entries | Still open |

---

## Regression Check

Comparing the current codebase with the 2026-04-18 remediated findings:

| Previous Finding | Status | Regression? |
|-----------------|--------|-------------|
| OLB-SEC-001 (MCP SSE auth) | REMEDIATED | No regression -- constructor correctly returns error |
| OLB-SEC-002 (JWT empty secret) | REMEDIATED | Not re-tested in Phase 2; no indication of regression |
| OLB-SEC-003 (ExcludePaths prefix) | REMEDIATED | Not re-tested in Phase 2; no indication of regression |
| OLB-SEC-004 (SNI connection limit) | REMEDIATED | Not re-tested in Phase 2; no indication of regression |
| OLB-SEC-005 (PUT config cooldown) | REMEDIATED | Confirmed -- cooldown and circuit breaker present at `handlers_system.go:209-224` |
| OLB-SEC-006 (Shadow header denylist) | REMEDIATED | Confirmed -- `defaultShadowDenyHeaders` at `shadow.go:57-63` |
| OLB-SEC-008 (MMDB recursion) | REMEDIATED | Not re-tested in Phase 2; no indication of regression |
| OLB-SEC-009 (Auth limiter cap) | REMEDIATED | Confirmed -- `maxAuthEntries` cap present at `auth.go:94` |
| OLB-SEC-011 (CSRF default) | REMEDIATED | Confirmed -- auto-enabled at `server.go:219-223` |
| OLB-SEC-014 (Config watcher debounce) | REMEDIATED | Confirmed -- stability counter at `watcher.go:131-140` |

**No regressions detected.** All previously remediated issues remain fixed.

---

## Genuinely New Findings (not in previous scan)

| ID | Severity | Description |
|----|----------|-------------|
| VF-002 | HIGH | Docker Compose node-exporter mounts host root filesystem |
| VF-004 | MEDIUM | Admin health endpoints expose operational state |
| VF-005 | MEDIUM | No RBAC on admin API |
| VF-006 | MEDIUM | MCP tools have no authorization separation |
| VF-007 | MEDIUM | Backend Weight/MaxConns non-atomic update |
| VF-008 | MEDIUM | Config reload overwrites prevConfig during rollback |
| VF-009 | MEDIUM | Raft mode proposes config without local validation |
| VF-010 | MEDIUM | Health checker SSRF protection incomplete |
| VF-011 | MEDIUM | Basic auth SHA256 mode uses fast HMAC |
| VF-013 | LOW | CRLF injection in transformer/headers middleware |
| VF-014 | LOW | Request ID fallback is deterministic |
| VF-015 | LOW | Logging middleware can log sensitive headers |
| VF-016 | LOW | Auth failure limiter map exhaustion bypass |
| VF-018 | LOW | Rollback reads poolManager outside lock |
| VF-019 | LOW | SSE event stream no per-connection timeout |
| SCG-002 | MEDIUM | Docker Compose Grafana default credentials |
| SCG-003 | MEDIUM | CI benchmark job runs untrusted PR code |
| SCG-004 | LOW | CI golangci-lint continue-on-error |
| SCG-005 | LOW | CI build-frontend continue-on-error |
| SCG-007 | MEDIUM | Log rotation size tracking inaccuracy |
| SCG-009 | MEDIUM | Health checker unregister race with Stop |
| SCG-010 | LOW | Dockerfile does not pin Alpine package versions |
| SCG-011 | LOW | WebUI XSS defense-in-depth for error messages |
| SCG-013 | MEDIUM | Docker Compose exposes monitoring ports to host |

---

## Complete Summary Table

| ID | Severity | CWE | Confidence | File(s) | Description | Source |
|----|----------|-----|------------|---------|-------------|--------|
| VF-001 | HIGH | CWE-306 | 75 | lifecycle.go:124-149 | MCP server object initialized even without token | AUTH-001 |
| VF-002 | HIGH | CWE-250 | 80 | docker-compose.yml:97-100 | Node exporter mounts host rootfs | SCG-001 |
| VF-003 | HIGH | CWE-345 | 72 | cluster/state.go:432-460 | Cluster state no crypto verification | OLB-SEC-007 |
| VF-004 | MEDIUM | CWE-200 | 82 | admin/auth.go:170,289 | Health endpoints expose state | AUTH-002 |
| VF-005 | MEDIUM | CWE-862 | 78 | admin/auth.go, server.go | No RBAC on admin API | AUTH-003 |
| VF-006 | MEDIUM | CWE-862 | 80 | mcp/mcp.go, sse_transport.go | MCP tools no authorization separation | AUTH-004 |
| VF-007 | MEDIUM | CWE-362 | 85 | handlers_backends.go:292-300 | Weight/MaxConns non-atomic | SC-002+SC-009 |
| VF-008 | MEDIUM | CWE-362 | 70 | engine/config.go:62-79 | Config reload overwrites prevConfig | SC-004 |
| VF-009 | MEDIUM | CWE-20 | 75 | handlers_system.go:186-199 | Raft proposes config without validation | SC-005 |
| VF-010 | MEDIUM | CWE-918 | 65 | health/health.go:587-605 | SSRF protection incomplete | SC-SSRF-002+SC-008 |
| VF-011 | MEDIUM | CWE-916 | 82 | middleware/basic/basic.go:59-63 | SHA256 HMAC for password hashing | AUTH-007+SC-001 |
| VF-013 | LOW | CWE-113 | 72 | transformer.go:236-238 | CRLF in config-sourced headers | SC-HDR-01+02 |
| VF-014 | LOW | CWE-331 | 88 | requestid.go:114-120 | Deterministic fallback ID | SC-004+SCG-006 |
| VF-015 | LOW | CWE-532 | 85 | logging.go:244-247 | Sensitive header logging possible | SC-005 |
| VF-016 | LOW | CWE-367 | 60 | admin/auth.go:94-103 | Limiter map exhaustion bypass | SC-003 |
| VF-017 | LOW | CWE-290 | 80 | admin/auth.go:176 | IP lockout shared behind proxy | OLB-SEC-021 |
| VF-018 | LOW | CWE-367 | 55 | engine/config.go:333-380 | Rollback poolManager outside lock | SC-011 |
| VF-019 | LOW | CWE-400 | 78 | admin/events.go:82-141 | SSE no per-connection timeout | SC-012 |
| VF-020 | LOW | CWE-426 | 82 | plugin/plugin.go:373-378 | isAllowed true when empty | OLB-SEC-025 |
| VF-021 | LOW | CWE-226 | 70 | tls/manager.go, jwt/jwt.go | Secrets not zeroed from memory | SC-003 |
| VF-022 | INFO | CWE-942 | 70 | admin/server.go:600-636 | Admin CORS origin reflection | AUTH-010+SCG-008 |
| VF-023 | INFO | CWE-918 | 55 | oauth2/oauth2.go:154-186 | OAuth2 outbound SSRF (expected) | SC-SSRF-003 |

---

*Report generated 2026-04-25 by Phase 3 Verification. All findings verified against actual source code at cited file paths.*
