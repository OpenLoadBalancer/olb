# WAF Implementation Tasks

> Ordered, numbered implementation tasks. Each task references specific files to create or modify.
> Tasks are grouped by phase. Execute sequentially within each phase.

---

## Phase 1: Foundation

### Task 1 — WAF Configuration Types
**Create:** `internal/waf/config.go`
**Modify:** `internal/config/config.go` (expand `WAFConfig`)

Expand the existing minimal `WAFConfig` struct with full sub-configurations for all 6 layers. Add sensible defaults for every field. Each sub-config has an `Enabled` field.

Sub-configs: `WAFIPACLConfig`, `WAFRateLimitConfig`, `WAFSanitizerConfig`, `WAFDetectionConfig`, `WAFBotConfig`, `WAFResponseConfig`, `WAFLoggingConfig`.

In `internal/waf/config.go`: create `WAFFullConfig` that holds parsed/validated config with computed values (parsed durations, compiled patterns, etc.).

### Task 2 — RequestContext
**Create:** `internal/waf/context.go`

Create `RequestContext` struct that carries normalized request data between layers. Include builder function `NewRequestContext(r *http.Request) *RequestContext` that extracts method, path, query, headers, cookies, body, remote IP. Store in `http.Request.Context()`.

### Task 3 — WAF Event Logging
**Create:** `internal/waf/event.go`

Create `WAFEvent` struct with timestamp, request ID, remote IP, method, path, user agent, layer, action, total score, findings, latency, node ID. Create `EventLogger` that writes JSON-structured events. Integrate with OLB's existing `logging.Logger`.

### Task 4 — Analytics
**Create:** `internal/waf/analytics.go`

Create `Analytics` struct with:
- Atomic counters: total requests, blocked, monitored
- Per-detector hit counts: `map[string]*atomic.Int64`
- Top blocked IPs: min-heap top-K tracker (embed in analytics, not separate package)
- Attack timeline: reuse `pkg/utils.RingBuffer` with per-minute buckets (1440 entries)
- Thread-safe read methods for MCP tools

### Task 5 — IP Access Control
**Create:** `internal/waf/ipacl/ipacl.go`
**Create:** `internal/waf/ipacl/ipacl_test.go`

Create `IPAccessList` that wraps `pkg/utils.CIDRMatcher` with metadata tracking. Methods:
- `Check(ip string) Action` — returns `Allow`, `Block`, or `Bypass` (whitelist hit)
- `AddWhitelist(cidr, reason string, expires time.Time) error`
- `AddBlacklist(cidr, reason string, expires time.Time) error`
- `RemoveWhitelist(cidr string) bool`
- `RemoveBlacklist(cidr string) bool`
- `Ban(ip string, ttl time.Duration, reason string)` — auto-ban
- `ListRules(listType string) []RuleMetadata`

Background goroutine for expiry cleanup (every 30s).

Tests: whitelist bypass, blacklist block, expiry, IPv4/IPv6, CIDR ranges, auto-ban.

### Task 6 — Request Sanitizer
**Create:** `internal/waf/sanitizer/sanitizer.go`
**Create:** `internal/waf/sanitizer/normalize.go`
**Create:** `internal/waf/sanitizer/validate.go`
**Create:** `internal/waf/sanitizer/sanitizer_test.go`

`sanitizer.go`: `Sanitizer` struct with `Process(r *http.Request) (*RequestContext, error)`. Runs validation then normalization. Returns enriched `RequestContext`.

`validate.go`: Check max header size/count, body size, URL length, cookie size/count, allowed methods, content-type, null bytes.

`normalize.go`: URL decode (multi-level), null byte removal, path canonicalization, hop-by-hop header stripping. Populate `DecodedPath`, `DecodedQuery`, `DecodedBody`, `BodyParams` in RequestContext.

Tests: double encoding, triple encoding, null bytes, path traversal normalization, oversized headers, invalid methods.

### Task 7 — WAF Middleware Orchestrator
**Create:** Refactor `internal/waf/waf.go`

Add `WAFMiddleware` struct alongside existing `WAF` struct. `WAFMiddleware` implements `middleware.Middleware` interface. It chains all 6 layers internally. Add `recover()` wrapper for fail-open behavior.

Keep existing `WAF`, `Rule`, `Match`, `Result` types for backwards compatibility. `WAFMiddleware` uses them internally.

### Task 8 — Wire into Engine
**Modify:** `internal/engine/engine.go`

Update `createMiddlewareChain()` to:
1. Parse full WAF config from `cfg.WAF`
2. Create `WAFMiddleware` with all layers
3. Replace existing `wafMiddlewareAdapter` usage with `WAFMiddleware`
4. Store `WAFMiddleware` reference on `Engine` for MCP access

---

## Phase 2: Detection Engine

### Task 9 — Detection Engine Core
**Create:** `internal/waf/detection/engine.go`
**Create:** `internal/waf/detection/detector.go`
**Create:** `internal/waf/detection/finding.go`

`detector.go`: `Detector` interface with `Name() string` and `Detect(ctx *RequestContext) []Finding`.

`finding.go`: `Finding` struct with detector name, score, location, evidence, rule ID.

`engine.go`: `Engine` struct that holds `[]Detector`, runs all detectors, accumulates scores, applies thresholds. Add `DetectionThreshold` (block: 50, log: 25). Add exclusion support (path + detector combinations).

### Task 10 — SQL Injection Detector
**Create:** `internal/waf/detection/sqli/sqli.go`
**Create:** `internal/waf/detection/sqli/tokenizer.go`
**Create:** `internal/waf/detection/sqli/patterns.go`
**Create:** `internal/waf/detection/sqli/sqli_test.go`

`tokenizer.go`: SQL tokenizer that classifies input into token types (String, Number, Keyword, Operator, Function, Comment, Paren, Semicolon, Wildcard, Other). Handle string delimiters (single/double/backtick), comments (`--`, `/* */`, `#`), case-insensitive keyword matching, hex encoding.

`patterns.go`: SQL keyword set (union of MySQL/PostgreSQL/MSSQL/SQLite), dangerous function names, scoring table.

`sqli.go`: `SQLiDetector` implementing `Detector`. Tokenize all request fields (query, body, headers, cookies), detect dangerous token sequences, score each finding.

Tests: classic SQLi, union-based, time-based blind, stacked queries, encoded, double-encoded, comment obfuscation, case mixing. Benign tests: names with apostrophes, English text with SQL keywords.

### Task 11 — XSS Detector
**Create:** `internal/waf/detection/xss/xss.go`
**Create:** `internal/waf/detection/xss/parser.go`
**Create:** `internal/waf/detection/xss/patterns.go`
**Create:** `internal/waf/detection/xss/xss_test.go`

`parser.go`: Simple state machine that detects `<tagname` patterns and extracts attributes. Handle case variations, entity encoding, unicode escapes.

`patterns.go`: Dangerous tag names, event handler names, protocol schemes, DOM properties.

`xss.go`: `XSSDetector` implementing `Detector`. Scan all request fields for XSS patterns.

Tests: `<script>`, event handlers, javascript: protocol, entity encoding, null byte insertion, expression(), data: URIs.

### Task 12 — Path Traversal Detector
**Create:** `internal/waf/detection/pathtraversal/pathtraversal.go`
**Create:** `internal/waf/detection/pathtraversal/sensitive_paths.go`
**Create:** `internal/waf/detection/pathtraversal/pathtraversal_test.go`

`sensitive_paths.go`: Known sensitive file paths per OS (/etc/passwd, /etc/shadow, /proc/self, win.ini, boot.ini, etc.).

`pathtraversal.go`: `PathTraversalDetector` implementing `Detector`. Compare normalized vs raw path — divergence with `..` increases score. Check for sensitive file paths.

Tests: single `../`, multiple levels, encoded, double-encoded, overlong UTF-8, sensitive paths, file:// protocol.

### Task 13 — Command Injection Detector
**Create:** `internal/waf/detection/cmdi/cmdi.go`
**Create:** `internal/waf/detection/cmdi/patterns.go`
**Create:** `internal/waf/detection/cmdi/cmdi_test.go`

`patterns.go`: Shell metacharacters, known dangerous commands, scoring table.

`cmdi.go`: `CMDiDetector` implementing `Detector`. Detect shell metacharacters followed by command-like tokens.

Tests: semicolon chaining, pipe, backtick, subshell, reverse shell patterns, base64 pipes.

### Task 14 — XXE Detector
**Create:** `internal/waf/detection/xxe/xxe.go`
**Create:** `internal/waf/detection/xxe/xxe_test.go`

`xxe.go`: `XXEDetector` implementing `Detector`. Only active when Content-Type contains "xml". Detect DOCTYPE, ENTITY, SYSTEM with file/http/expect protocols.

Tests: basic XXE, parameter entities, SSRF via XXE, SSI injection.

### Task 15 — SSRF Detector
**Create:** `internal/waf/detection/ssrf/ssrf.go`
**Create:** `internal/waf/detection/ssrf/ipcheck.go`
**Create:** `internal/waf/detection/ssrf/ssrf_test.go`

`ipcheck.go`: Detect private IP ranges (reuse `pkg/utils.IsPrivateIP`), cloud metadata IPs (169.254.169.254), decimal/octal IP encoding, URL with @ bypass.

`ssrf.go`: `SSRFDetector` implementing `Detector`. Scan URL parameters, JSON body values for internal URLs.

Tests: localhost, 127.0.0.1, cloud metadata, private ranges, decimal IP, octal IP, @ bypass.

### Task 16 — Legacy Regex Detector Bridge
**Modify:** `internal/waf/waf.go`

Create `LegacyDetector` that wraps existing `WAF.Process()` as a `Detector` interface implementation. This preserves existing regex rules as an additional detection source alongside the new tokenizer-based detectors.

### Task 17 — Detection Integration Tests
**Create:** `internal/waf/detection/engine_test.go`

Test the full detection engine with all detectors:
- Each detector independently scores correctly
- Scores accumulate across detectors
- Threshold triggers block/log decisions
- Exclusions work per-path per-detector
- Score multipliers adjust sensitivity

---

## Phase 3: Rate Limiting & Bot Detection

### Task 18 — Token Bucket Rate Limiter
**Create:** `internal/waf/ratelimit/ratelimit.go`
**Create:** `internal/waf/ratelimit/bucket.go`
**Create:** `internal/waf/ratelimit/ratelimit_test.go`

`bucket.go`: `TokenBucket` with `Allow() bool`, `Consume(n int) bool`, `Refill()`. Thread-safe.

`ratelimit.go`: `RateLimiter` struct with multiple rules, each with scope (ip, path, ip+path, header, global). `Allow(r *http.Request) bool` checks all rules. Returns 429 with `Retry-After` header. Tracks violation counts for auto-ban.

Background cleanup goroutine for expired buckets.

Tests: basic rate limiting, burst, multiple rules, different scopes, cleanup.

### Task 19 — Distributed Rate Limit Sync
**Create:** `internal/waf/ratelimit/sync.go`

Raft sync logic:
- Periodic sync (every `sync_interval`) sends local counter deltas via `cluster.Propose()`
- On apply, merge remote deltas into local state
- `RateLimiter.SetCluster(cluster *cluster.Cluster)` to enable distributed mode

### Task 20 — JA3 TLS Fingerprinting
**Create:** `internal/waf/botdetect/ja3.go`

Compute JA3 hash from `*tls.ClientHelloInfo`:
- Extract SSL version, cipher suites, extensions, elliptic curves, EC point formats
- Build JA3 string: `SSLVersion,Ciphers,Extensions,EllipticCurves,ECPointFormats`
- Compute MD5 hash
- Store in connection context

### Task 21 — Known Fingerprint Database
**Create:** `internal/waf/botdetect/fingerprints.go`

Embedded database of known JA3 hashes:
- Known good: Chrome, Firefox, Safari, Edge (major versions)
- Known bad: Python requests, Go default, curl, sqlmap, nikto
- Known suspicious: Headless Chrome, Selenium, Puppeteer

Lookup function: `Classify(ja3Hash string) (category string, confidence int)`

### Task 22 — User-Agent Analysis
**Create:** `internal/waf/botdetect/useragent.go`

`AnalyzeUA(ua string) UAResult` — returns scores for:
- Empty UA
- Known scanner strings
- Missing version
- Outdated browser version
- UA/JA3 mismatch detection

### Task 23 — Behavioral Analysis
**Create:** `internal/waf/botdetect/behavior.go`

Per-IP sliding window tracker:
- Requests per second
- Unique paths per minute
- Error rate percentage
- Timing standard deviation
- Session/encoding/referer presence

`Analyze(ip string, r *http.Request) BehaviorResult` — returns composite score.

Background goroutine to clean expired windows.

### Task 24 — Bot Detection Middleware
**Create:** `internal/waf/botdetect/botdetect.go`
**Create:** `internal/waf/botdetect/botdetect_test.go`

`BotDetector` struct that combines JA3, UA, and behavioral analysis. Implements detection logic with configurable thresholds.

Tests: known scanner, empty UA, behavioral anomalies, JA3/UA mismatch.

---

## Phase 4: Response Protection & Analytics

### Task 25 — Security Headers
**Create:** `internal/waf/response/headers.go`

`InjectHeaders(w http.ResponseWriter, config *HeadersConfig)` — injects HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Permissions-Policy, X-XSS-Protection. Configurable per header.

### Task 26 — Sensitive Data Masking
**Create:** `internal/waf/response/masking.go`

`MaskSensitiveData(body []byte, contentType string, config *MaskingConfig) []byte`

Regex patterns for: credit card numbers, SSN, API keys (sk_live_, ghp_, etc.), stack traces. Only scan text-based content types. Use `regexp` stdlib.

### Task 27 — Error Page Standardization
**Create:** `internal/waf/response/errorpage.go`

Replace backend 5xx responses with generic HTML error pages in production mode. Pass through in development mode. Embedded HTML templates (no external files).

### Task 28 — Response Protection Middleware
**Create:** `internal/waf/response/response.go`
**Create:** `internal/waf/response/response_test.go`

`Protection` struct with `Wrap(w http.ResponseWriter) http.ResponseWriter`. Returns a `protectedResponseWriter` that:
1. Injects security headers on `WriteHeader()`
2. Scans and masks response body on `Write()`
3. Replaces error pages on 5xx status

Tests: header injection, credit card masking, error page replacement.

### Task 29 — Wire Analytics into Raft
**Modify:** `internal/waf/analytics.go`

Add `SetCluster(cluster *cluster.Cluster)` method. Periodic Raft sync of analytics counters for cluster-wide aggregation. Each node maintains local counters + last-known remote counters.

---

## Phase 5: MCP & Raft Integration

### Task 30 — WAF Raft Command Types
**Modify:** `internal/cluster/config_sm.go`

Add WAF command types:
- `CmdWAFAddWhitelist`, `CmdWAFRemoveWhitelist`
- `CmdWAFAddBlacklist`, `CmdWAFRemoveBlacklist`
- `CmdWAFAddRateRule`, `CmdWAFRemoveRateRule`
- `CmdWAFSetMode`
- `CmdWAFSyncCounters`

Add corresponding payload structs. Extend `ConfigStateMachine.Apply()` switch statement. Add callback hook for WAF state application.

### Task 31 — MCP Tool Definitions
**Create:** `internal/waf/mcp/tools.go`

Define all ~15 MCP tools with proper `InputSchema` (JSON Schema). Group by category: IP ACL, rate limit, detection, control, analytics.

### Task 32 — MCP Tool Handlers
**Create:** `internal/waf/mcp/handlers.go`

Implement handler functions for each MCP tool:
- Read operations: query WAF state directly
- Write operations: create `ConfigCommand`, call `cluster.Propose()`
- Return structured `ToolResult` with JSON content

### Task 33 — Wire MCP Tools into Engine
**Modify:** `internal/engine/engine.go`

After MCP server creation, call `wafmcp.RegisterTools(mcpServer, wafMiddleware, cluster)` to register all WAF tools.

### Task 34 — Integration Tests
**Create:** `internal/waf/mcp/mcp_test.go`

Test: MCP tool call → Raft propose → WAF state change → request affected.
Test: Read tools return correct analytics data.

---

## Phase 6: Hardening

### Task 35 — Performance Benchmarks
**Create:** `internal/waf/benchmark_test.go`

```go
BenchmarkIPACLLookup          // target: < 100ns
BenchmarkRateLimitCheck       // target: < 500ns
BenchmarkSanitizerProcess     // target: < 50μs
BenchmarkDetectionEngine      // target: < 500μs p95
BenchmarkSQLiDetector         // target: < 200μs
BenchmarkXSSDetector          // target: < 200μs
BenchmarkBotDetection         // target: < 100μs
BenchmarkResponseHeaders      // target: < 10μs
BenchmarkFullPipeline         // target: < 1ms p99
BenchmarkFullPipelineParallel // concurrent requests
```

### Task 36 — Hot Path Optimization
**Modify:** Various files

- Pre-allocate buffers in sanitizer (use `sync.Pool`)
- Pre-compile all regex patterns at init time
- Use `strings.Builder` instead of concatenation in tokenizers
- Minimize allocations in detection hot path (reuse `Finding` slices)
- Profile with `pprof`, fix allocation hotspots

### Task 37 — Fuzz Testing
**Create:** `internal/waf/detection/sqli/fuzz_test.go`
**Create:** `internal/waf/detection/xss/fuzz_test.go`
**Create:** `internal/waf/sanitizer/fuzz_test.go`

Go native fuzzing (`func FuzzXXX(f *testing.F)`). Feed random/malformed inputs to:
- SQL tokenizer
- XSS parser
- URL normalizer
- All detectors

Ensure no panics, no infinite loops, no excessive memory allocation.

### Task 38 — False Positive Testing
**Create:** `internal/waf/detection/falsepositive_test.go`

Feed legitimate traffic and verify no false blocks:
- Blog posts with SQL-like content ("SELECT your adventure")
- Names with apostrophes ("O'Brien", "D'Angelo")
- Math expressions ("if x=1 then y=2")
- URLs in request bodies
- JSON/XML payloads with angle brackets
- API payloads with code snippets

### Task 39 — Full Integration Tests
**Create:** `internal/waf/integration_test.go`

End-to-end tests:
- Clean request passes all 6 layers, reaches backend
- SQLi attack blocked at Layer 4
- Whitelisted IP bypasses all layers
- Rate limited IP gets 429
- Blacklisted IP gets 403
- Bot detected and blocked
- Response headers injected
- Sensitive data masked in response
- WAF latency < 1ms for clean requests

### Task 40 — Update Documentation
**Modify:** `CLAUDE.md` (add WAF architecture entry)

Update architecture section to include WAF sub-packages. Add WAF config example to config format section.

---

## Dependency Graph

```
Task 1 (config) ←── Task 2 (context)
    │                    │
    ├── Task 5 (ipacl) ─┤
    ├── Task 6 (sanitizer) ──── Task 9 (detection engine)
    │                               │
    │                    ┌──────────┼──────────┐
    │                    │          │          │
    │              Task 10 (sqli) Task 11 (xss) Task 12-15 (others)
    │                    │          │          │
    │                    └──────────┼──────────┘
    │                               │
    ├── Task 3 (events) ───── Task 4 (analytics)
    │                               │
    ├── Task 18-19 (rate limit) ────┤
    ├── Task 20-24 (bot detect) ────┤
    ├── Task 25-28 (response) ──────┤
    │                               │
    ├── Task 7 (orchestrator) ◄─────┘
    │         │
    └── Task 8 (engine wiring) ──── Task 30-33 (raft + mcp)
                                         │
                                    Task 34-39 (hardening)
                                         │
                                    Task 40 (docs)
```

Tasks 1-4 are foundation and can be done first.
Tasks 5, 6, 10-15, 18-24, 25-28 can be parallelized after foundation.
Tasks 7-8 wire everything together.
Tasks 30-33 add distributed features.
Tasks 34-40 harden the implementation.
