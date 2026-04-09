# Verified Security Findings — OpenLoadBalancer

## Finding Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 4 |
| HIGH | 9 |
| MEDIUM | 12 |
| LOW | 4 |
| INFO | 4 |

---

## CRITICAL Findings

### C-01: OAuth2 Middleware Token Validation Is a Stub
- **File:** `internal/middleware/oauth2/oauth2.go:158-189`
- **CWE:** CWE-287 (Improper Authentication)
- **CVSS:** 9.8 (Critical)
- **Confidence:** HIGH (verified in source)
- **Description:** The `validateToken()` function unconditionally returns `Active: true` for any token. JWT-format tokens (3 dot-separated parts) receive a mock `TokenInfo` with `Subject: "user"` and `Permissions: ["read"]`. Non-JWT tokens are also accepted as active.
- **Impact:** Complete authentication bypass on any route using OAuth2 middleware.
- **Remediation:** Implement real token validation via JWKS endpoint introspection, or at minimum, call the configured introspection URL before accepting tokens.

### C-02: Admin API Can Run Without Authentication
- **File:** `internal/admin/server.go:195-196, 282-286`
- **CWE:** CWE-306 (Missing Authentication for Critical Function)
- **CVSS:** 9.1 (Critical)
- **Confidence:** HIGH (verified in source)
- **Description:** When no `AuthConfig` is provided, the admin server logs a warning but proceeds without any authentication. All endpoints — config reload, backend CRUD, system control — are publicly accessible.
- **Impact:** Full administrative control exposed to anyone who can reach the admin port.
- **Remediation:** Require auth configuration to start. If no auth is configured, bind admin API to localhost only, or refuse to start.

### C-03: Race Condition — Gossip localNode.Metadata Concurrent Map Access
- **File:** `internal/cluster/gossip.go:528-531, 770, 1116, 1200, 1316`
- **CWE:** CWE-362 (Race Condition)
- **CVSS:** 8.6 (High)
- **Confidence:** HIGH (verified: write under `membersMu` lock, reads without lock in multiple goroutines)
- **Description:** `SetMetadata()` writes to `localNode.Metadata` map under `membersMu` lock, but `encodeJoinMessage()`, `encodeAlive()`, and `encodeLeaveMessage()` read the same map from different goroutines (probeLoop, gossipLoop, udpReadLoop) without holding the lock.
- **Impact:** Runtime panic under concurrent cluster operation (Go map concurrent read/write).
- **Remediation:** Copy metadata under lock before encoding, or acquire `membersMu` in all encoding functions.

### C-04: Race Condition — Gossip localNode.Incarnation Written Without Lock
- **File:** `internal/cluster/gossip.go:1113-1114, 1198`
- **CWE:** CWE-362 (Race Condition)
- **CVSS:** 8.6 (High)
- **Confidence:** HIGH (verified: no synchronization on Incarnation field)
- **Description:** `localNode.Incarnation` is incremented in `handleSuspect()` and `handleDead()` (udpReadLoop goroutine) without any lock, while `encodeAlive()` and other encoding functions read it from different goroutines.
- **Impact:** Data race on fundamental cluster state, can cause incorrect cluster behavior or crashes.
- **Remediation:** Use `atomic.Uint64` for Incarnation, or protect all accesses with a mutex.

---

## HIGH Findings

### H-01: Hardcoded Default Password in Example Config
- **File:** `configs/olb.yaml:322`
- **CWE:** CWE-798 (Use of Hard-coded Credentials)
- **CVSS:** 7.5 (High)
- **Confidence:** HIGH
- **Description:** SHA256 hash of "password" with the plaintext revealed in a comment. Operators may use this as a template.
- **Remediation:** Remove the comment revealing the password. Use a clearly-fake hash like `REPLACE_WITH_YOUR_HASH`. Add a startup check that rejects known-weak passwords.

### H-02: Hardcoded API Keys in Example Config
- **File:** `configs/olb.yaml:357-358`
- **CWE:** CWE-798 (Use of Hard-coded Credentials)
- **CVSS:** 7.5 (High)
- **Confidence:** HIGH
- **Description:** Trivially guessable API keys (`a1b2c3d4e5f6`, `f6e5d4c3b2a1`) in default config.
- **Remediation:** Replace with placeholder values. Add documentation on generating secure keys.

### H-03: CORS Wildcard with Credentials Allowed
- **File:** `internal/middleware/cors.go:110-115`
- **CWE:** CWE-942 (Overly Permissive CORS Policy)
- **CVSS:** 7.5 (High)
- **Confidence:** HIGH
- **Description:** When `AllowedOrigins: ["*"]` and `AllowCredentials: true`, the middleware reflects the requesting Origin verbatim with `Access-Control-Allow-Credentials: true`, effectively disabling same-origin policy.
- **Remediation:** When credentials are enabled, reject wildcard origins. Require explicit origin list.

### H-04: GeoDNS Trusts X-Forwarded-For Unconditionally
- **File:** `internal/geodns/geodns.go:222-243`
- **CWE:** CWE-346 (Origin Validation Error)
- **CVSS:** 7.4 (High)
- **Confidence:** HIGH
- **Description:** GeoDNS extracts client IP from `X-Forwarded-For` without validating the request came from a trusted proxy. Attackers can spoof geographic location.
- **Remediation:** Use the same trusted-proxy logic as `RealIPMiddleware`, or only use `RemoteAddr`.

### H-05: IP Filter XFF Bypass
- **File:** `internal/middleware/ip_filter.go:175-192`
- **CWE:** CWE-346 (Origin Validation Error)
- **CVSS:** 7.4 (High)
- **Confidence:** HIGH
- **Description:** When `TrustXForwardedFor: true`, the leftmost IP from XFF is trusted without verifying the request came from a configured proxy.
- **Remediation:** Add a trusted proxy list configuration. Only parse XFF when the direct peer is a trusted proxy.

### H-06: Race Condition — Gossip localNode.State Without Lock
- **File:** `internal/cluster/gossip.go:450`
- **CWE:** CWE-362
- **CVSS:** 7.0 (High)
- **Confidence:** HIGH
- **Description:** `Leave()` sets `localNode.State = StateLeft` without synchronization while other goroutines read it under `membersMu`.
- **Remediation:** Protect with mutex or use `atomic.Int32`.

### H-07: Goroutine Leak — SSE readLineWithTimeout
- **File:** `internal/proxy/l7/sse.go:264`
- **CWE:** CWE-400 (Uncontrolled Resource Consumption)
- **CVSS:** 7.0 (High)
- **Confidence:** HIGH
- **Description:** Each SSE line-read timeout spawns a drain goroutine that may never return if the backend hangs. Unbounded goroutine growth over long-lived SSE connections.
- **Remediation:** Use a bounded goroutine pool, or cancel the underlying reader via context.

### H-08: Unbounded io.ReadAll on gRPC-Web Request Bodies
- **File:** `internal/proxy/l7/grpc.go:302, 312`
- **CWE:** CWE-400 (Uncontrolled Resource Consumption)
- **CVSS:** 7.5 (High)
- **Confidence:** HIGH (verified: no size limit on request body reads)
- **Description:** `HandleGRPCWeb` reads entire request body with `io.ReadAll(r.Body)` without size limit. Response body correctly uses `io.LimitReader`.
- **Impact:** Memory exhaustion DoS via large request bodies.
- **Remediation:** Add `io.LimitReader(r.Body, maxGRPCWebSize)` consistent with the response body handling.

### H-09: Shadow Request Body Consumed Without Size Limit
- **File:** `internal/proxy/l7/shadow.go:152`
- **CWE:** CWE-400, CWE-20
- **CVSS:** 7.5 (High)
- **Confidence:** HIGH
- **Description:** `sendShadow` reads the entire original request body with `io.ReadAll(req.Body)` when `CopyBody` is enabled, with no size cap. Additionally, consuming the body means the actual proxied request may receive an empty body.
- **Remediation:** Tee the body to a buffer with size limit, or read into a bounded buffer before the main proxy uses it.

---

## MEDIUM Findings

### M-01: PROXY Protocol Trusts All Sources by Default
- **File:** `internal/proxy/l4/proxyproto.go:507-525`
- **CWE:** CWE-346
- **Confidence:** HIGH
- **Description:** Unless `TrustedNetworks` is explicitly configured, PROXY protocol headers are accepted from any source, allowing IP spoofing.
- **Remediation:** Default to trusting no sources. Require explicit trusted network configuration.

### M-02: InsecureSkipVerify in gRPC Health Checks
- **File:** `internal/health/health.go:134`
- **CWE:** CWE-295 (Improper Certificate Validation)
- **Confidence:** HIGH
- **Description:** gRPC health check client permanently disables TLS certificate verification. Not configurable.
- **Remediation:** Make TLS verification configurable per-backend.

### M-03: Basic Auth Supports Plaintext Password Storage
- **File:** `internal/middleware/basic/basic.go:57-58, 158-159`
- **CWE:** CWE-256 (Plaintext Storage of a Password)
- **Confidence:** HIGH
- **Description:** The `Hash: "plain"` option stores and compares passwords in plaintext. Default hash is SHA-256 (unsalted, fast).
- **Remediation:** Deprecate `plain` mode. Default to bcrypt. Add salt to SHA-256 mode.

### M-04: TLS 1.0 and 1.1 Accepted as Configurable Minimum
- **File:** `internal/tls/manager.go:253-258`
- **CWE:** CWE-326 (Inadequate Encryption Strength)
- **Confidence:** HIGH
- **Description:** TLS 1.0/1.1 can be configured despite logging a deprecation warning. Per RFC 8996, these should be rejected outright.
- **Remediation:** Remove TLS 1.0/1.1 parsing. Return an error instead of a warning.

### M-05: Error Messages Leak Internal State in Admin API
- **File:** `internal/admin/handlers.go:289, 407, 464`; `internal/webui/webui.go:120`
- **CWE:** CWE-209 (Information Exposure Through Error Message)
- **Confidence:** HIGH
- **Description:** Raw error messages from reload, Raft, and filesystem operations are returned to clients, potentially exposing internal paths and state.
- **Remediation:** Return generic error messages to clients. Log detailed errors server-side.

### M-06: JSON Injection in WAF Block Response
- **File:** `internal/waf/middleware.go:359`
- **CWE:** CWE-79 (Injection)
- **Confidence:** HIGH
- **Description:** Sanitizer error messages (containing user-controlled header names) are concatenated into a JSON string without escaping. Header named `X"inject` produces malformed JSON.
- **Remediation:** Use `json.NewEncoder` instead of string concatenation.

### M-07: conn.Pool Get() Lock-Unlock-Lock Race
- **File:** `internal/conn/pool.go:133-189`
- **CWE:** CWE-362
- **Confidence:** MEDIUM
- **Description:** Between releasing the mutex to dial and re-acquiring it, the pool could be closed. Multiple goroutines can exceed logical connection limits.
- **Remediation:** Use a semaphore or check limits atomically before dialing.

### M-08: ConfigStateMachine Overlapping Config Applications
- **File:** `internal/cluster/config_sm.go:179`
- **CWE:** CWE-362
- **Confidence:** MEDIUM
- **Description:** `Apply()` fires `onConfigApplied` in a new goroutine without waiting, allowing overlapping `applyConfig` calls from rapid Raft log applies.
- **Remediation:** Serialize config applications or use a channel-based queue.

### M-09: Goroutine Leak — TCP CopyBidirectional Without Timeout
- **File:** `internal/proxy/l4/tcp.go:484-533`; `internal/proxy/l4/sni.go:642`
- **CWE:** CWE-400
- **Confidence:** HIGH
- **Description:** SNI proxy uses `CopyBidirectional` with timeout 0 (no deadline). Goroutines depend entirely on TCP connection closing.
- **Remediation:** Apply a reasonable idle timeout for all bidirectional copies.

### M-10: SNI TLS Record Truncation Protocol Error
- **File:** `internal/proxy/l4/sni.go:147-152`
- **CWE:** CWE-20 (Improper Input Validation)
- **Confidence:** HIGH
- **Description:** When TLS record exceeds `MaxHandshakeSize`, the code caps the read but leaves remaining bytes in the TCP buffer, corrupting the forwarded stream.
- **Remediation:** Reject oversized ClientHello messages instead of truncating.

### M-11: Broadcast Errors Silently Ignored in Cluster State
- **File:** `internal/cluster/state.go:371, 396, 422`
- **CWE:** CWE-390 (Detection of Error Condition Without Action)
- **Confidence:** HIGH
- **Description:** All `_ = s.Broadcast(data)` calls suppress errors. Persistent broadcast failures cause silent cluster divergence.
- **Remediation:** Log broadcast errors. Add metrics for failed broadcasts.

### M-12: Health Checker Goroutine Leak on Restart
- **File:** `internal/engine/engine.go:656-657`
- **CWE:** CWE-400
- **Confidence:** MEDIUM
- **Description:** `Start()` creates a new health checker, replacing the one from `New()`, without stopping the old one's goroutines.
- **Remediation:** Stop the old health checker before creating a new one.

---

## LOW Findings

### L-01: Nil Pointer Risk from atomic.Value Type Assertions
- **File:** `internal/proxy/l7/proxy.go:218`; `internal/cluster/cluster.go:247, 263, 788`
- **Confidence:** LOW (currently safe but fragile)
- **Description:** Bare type assertions on `atomic.Value.Load()` without ok check. Currently initialized correctly but fragile.
- **Remediation:** Use typed atomic wrappers or add ok-check assertions.

### L-02: Context Background for Discovery Manager
- **File:** `internal/engine/engine.go:756-757`
- **Confidence:** HIGH
- **Description:** Discovery manager started with `context.Background()` instead of engine lifecycle context.
- **Remediation:** Derive context from engine's stop channel.

### L-03: Raft Log Unbounded Growth on Followers
- **File:** `internal/cluster/cluster.go:500-502, 616`
- **Confidence:** MEDIUM
- **Description:** Log compaction only triggered on leader after command commit. Follower logs grow without bound.
- **Remediation:** Add follower-side compaction trigger.

### L-04: HandleHTTP2Proxy Missing Hop-by-Hop Header Filtering
- **File:** `internal/proxy/l7/http2.go:554-558`
- **Confidence:** HIGH
- **Description:** Copies all backend response headers without filtering hop-by-hop headers, unlike the main proxy path.
- **Remediation:** Apply the same `copyHeaders()` function used in the main proxy path.

---

## INFO Findings

### I-01: MD5 Used for JA3 Fingerprinting — By Design
- **File:** `internal/waf/botdetect/ja3.go:85`
- **Note:** Standard JA3 algorithm definition. Not security-sensitive.

### I-02: SHA1 Used for WebSocket Handshake — By Design
- **File:** `internal/proxy/l7/websocket.go:305`
- **Note:** Required by RFC 6455 Section 4.2.2.

### I-03: math/rand Used for Retry Jitter — Acceptable
- **File:** `internal/middleware/retry.go:112`
- **Note:** Not used for security purposes.

### I-04: unsafe.Pointer in CLI TTY Handling — Acceptable
- **File:** `internal/cli/top_*.go`
- **Note:** Platform-specific terminal control. Not in network/data path.
