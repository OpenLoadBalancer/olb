# WAF Technical Specification

> Full technical specification for the Web Application Firewall layer in OpenLoadBalancer.
> Based on `docs/OLB-WAF-SECURITY-LAYER.md`, adapted to OLB's existing architecture.

---

## 1. Overview

The WAF layer transforms OLB from a pure load balancer into a security-aware application delivery controller. It consists of 6 defense-in-depth layers, each implemented as a composable middleware in OLB's existing `middleware.Chain`.

**Constraints:**
- Zero external dependencies (Go stdlib only)
- < 1ms p99 latency for clean requests through all 6 layers
- All state replicated via OLB's existing Raft consensus
- All management exposed via OLB's existing MCP server
- Every layer independently supports `monitor` / `enforce` / `disabled` modes

---

## 2. Request Pipeline

```
Client Request
    │
    ▼
┌─────────────────────────────────┐
│  Layer 1: IP Access Control     │  Priority 90
│  Radix tree CIDR matching       │
│  Whitelist → bypass layers 2-6  │
│  Blacklist → immediate 403      │
├─────────────────────────────────┤
│  Layer 2: Rate Limiter          │  Priority 95
│  Token bucket per-IP/path/key   │
│  Raft-synced counters           │
│  429 + Retry-After              │
├─────────────────────────────────┤
│  Layer 3: Request Sanitizer     │  Priority 98
│  URL/encoding normalization     │
│  Header/body validation         │
│  Null byte removal              │
├─────────────────────────────────┤
│  Layer 4: WAF Detection Engine  │  Priority 100
│  SQLi, XSS, path traversal,    │
│  CMDi, XXE, SSRF detectors     │
│  Scoring-based decision         │
├─────────────────────────────────┤
│  Layer 5: Bot Detection         │  Priority 105
│  JA3 TLS fingerprinting         │
│  User-Agent analysis            │
│  Behavioral analysis            │
├─────────────────────────────────┤
│  Layer 6: Response Protection   │  Priority 1100
│  Security headers injection     │
│  Sensitive data masking         │
│  Error page standardization     │
└─────────────────────────────────┘
    │
    ▼
OLB Load Balancer → Backend Pool
```

Priorities are chosen to integrate with OLB's existing chain:
- Layers 1-5 run before existing security middleware (priority 100)
- Layer 6 wraps the response, so it runs after access log (priority 1100)

---

## 3. Layer 1: IP Access Control

**Package:** `internal/waf/ipacl/`

### 3.1 Data Structure

Uses `pkg/utils.CIDRMatcher` (radix trie already implemented) as the foundation, extended with metadata tracking:

```go
type IPAccessList struct {
    mu        sync.RWMutex
    whitelist *MetadataRadixTree
    blacklist *MetadataRadixTree
}

type MetadataRadixTree struct {
    matcher  *utils.CIDRMatcher  // existing radix trie for fast lookup
    metadata map[string]*RuleMetadata // CIDR string → metadata
}

type RuleMetadata struct {
    ID        string
    CIDR      string
    CreatedAt time.Time
    ExpiresAt time.Time    // zero = never expires
    Reason    string
    Source    string        // "manual", "auto-ban", "raft-sync"
}
```

### 3.2 Behavior

1. **Whitelist first.** If client IP matches whitelist → set context flag `waf_whitelisted=true` → skip layers 2-6.
2. **Blacklist check.** If IP matches blacklist → 403 Forbidden immediately.
3. **Expiry cleanup.** Background goroutine runs every 30s, removes expired entries.
4. **Auto-ban.** Other layers can call `ipacl.Ban(ip, ttl, reason)` to dynamically blacklist IPs.

### 3.3 Configuration

```yaml
waf:
  ip_acl:
    enabled: true
    whitelist:
      - cidr: "10.0.0.0/8"
        reason: "Internal network"
      - cidr: "192.168.1.100/32"
        reason: "Monitoring server"
    blacklist:
      - cidr: "203.0.113.0/24"
        reason: "Known bad actor"
        expires: "2025-12-31T23:59:59Z"
    auto_ban:
      enabled: true
      default_ttl: "1h"
      max_ttl: "24h"
```

---

## 4. Layer 2: Distributed Rate Limiter

**Package:** `internal/waf/ratelimit/`

### 4.1 Algorithm

Token bucket (same algorithm as existing `middleware.RateLimitMiddleware`) but with:
- Multiple scope types (per-IP, per-path, per-header, global)
- Multiple concurrent rules
- Raft-synchronized counters for cluster-wide accuracy
- Auto-ban integration

### 4.2 Distributed Sync

```
Strategy: Sliding window + periodic Raft sync

1. Each node maintains local counters (in-memory, fast path)
2. Every sync_interval (default 5s), nodes sync counter deltas via Raft
3. On sync, each node adds received deltas to local counters
4. Rate limit check uses: local_count + last_known_remote_counts
5. Accuracy: ±1 sync_interval worth of requests
```

### 4.3 Rule Types

| Scope | Key | Example |
|-------|-----|---------|
| `ip` | Client IP | 1000 req/min per IP |
| `ip+path` | IP + path pattern | 10 req/min to /login per IP |
| `header:X-API-Key` | Header value | 100 req/min per API key |
| `global` | None (cluster-wide) | 50000 req/min total |

### 4.4 Graduated Response

1. First violation → 429 with `Retry-After` header
2. Repeated violations (configurable count) → auto-ban via Layer 1

### 4.5 Configuration

```yaml
waf:
  rate_limit:
    enabled: true
    sync_interval: "5s"
    rules:
      - id: "global-per-ip"
        scope: "ip"
        limit: 1000
        window: "1m"
        burst: 50
        action: "block"
      - id: "login-bruteforce"
        scope: "ip+path"
        paths: ["/login", "/auth/*"]
        limit: 10
        window: "1m"
        burst: 3
        action: "block"
        auto_ban_after: 5
```

---

## 5. Layer 3: Request Sanitizer

**Package:** `internal/waf/sanitizer/`

### 5.1 Validation Checks

| Check | Default | Action |
|-------|---------|--------|
| Max header size | 8KB per header | 431 Request Header Fields Too Large |
| Max header count | 100 | 431 |
| Max body size | 10MB | 413 Payload Too Large |
| Max URL length | 8192 bytes | 414 URI Too Long |
| Max cookie size | 4096 bytes | 400 Bad Request |
| Max cookie count | 50 | 400 |
| Allowed methods | GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS | 405 Method Not Allowed |
| Null bytes | Block | 400 |
| Content-Type | Validated list | 415 Unsupported Media Type |

### 5.2 Normalization Pipeline

Applied in order, BEFORE the detection engine sees the request:

1. **URL decode** — handle double/triple encoding (`%2527` → `%27` → `'`)
2. **Null byte removal** — `%00`, `\0`
3. **Path canonicalization** — remove `/../`, `/./`, `//`, trailing dots
4. **Unicode normalization** — NFC form
5. **Case normalization** — for path comparison (configurable)
6. **Hop-by-hop header stripping** — remove `Connection`, `Keep-Alive`, `Proxy-*`, etc.
7. **Content-Type validation**
8. **Body size enforcement** — including chunked transfer

The normalized request is stored in a `RequestContext` that all subsequent layers use.

### 5.3 Configuration

```yaml
waf:
  sanitizer:
    enabled: true
    max_header_size: 8192
    max_header_count: 100
    max_body_size: 10485760
    max_url_length: 8192
    block_null_bytes: true
    normalize_encoding: true
    strip_hop_by_hop: true
    path_overrides:
      - path: "/upload/*"
        max_body_size: 104857600
```

---

## 6. Layer 4: WAF Detection Engine

**Package:** `internal/waf/detection/`

### 6.1 Architecture

The existing `internal/waf/waf.go` handles basic regex-based detection. The new detection engine replaces this with specialized tokenizer/parser-based detectors while maintaining the existing scoring model.

```go
type DetectionEngine struct {
    detectors []Detector
    threshold DetectionThreshold
}

type Detector interface {
    Name() string
    Detect(ctx *RequestContext) []Finding
}

type Finding struct {
    Detector  string   // "sqli", "xss", "path_traversal", etc.
    Score     int      // 0-100
    Location  string   // "url", "body", "header:X-Custom", "cookie:session"
    Evidence  string   // matched pattern (truncated)
    Rule      string   // rule ID
}

type DetectionThreshold struct {
    Block int  // default: 50
    Log   int  // default: 25
}
```

### 6.2 Detectors

#### SQL Injection (`detection/sqli/`)

**Approach:** Tokenize input into SQL token types, identify dangerous keyword sequences, score by pattern.

Token types: `String`, `Number`, `Keyword`, `Operator`, `Function`, `Comment`, `Paren`, `Comma`, `Semicolon`, `Wildcard`, `Other`

| Pattern | Score |
|---------|-------|
| `UNION SELECT` | 90 |
| `OR/AND` + tautology (`1=1`, `'a'='a'`) | 85 |
| `SLEEP()`, `BENCHMARK()` | 95 |
| `DROP TABLE/DATABASE` | 100 |
| `;` + SQL keyword (stacked queries) | 85 |
| `INTO OUTFILE/DUMPFILE` | 100 |
| Single quote + `OR/AND` | 50 |
| SQL comments (`--`, `#`) | 40 |
| `CHAR()`, `CONCAT()` functions | 55 |
| SQL keywords in isolation | 10 |

Must handle: case variations, comment obfuscation (`UNION/**/SELECT`), whitespace tricks, hex encoding, backtick delimiters, multi-dialect keywords (MySQL/PostgreSQL/MSSQL/SQLite).

#### XSS (`detection/xss/`)

| Pattern | Score |
|---------|-------|
| `<script>` | 90 |
| `<img onerror=` | 85 |
| `<svg onload=` | 85 |
| `javascript:` | 80 |
| `data:text/html` | 75 |
| `on[event]=` (any HTML event) | 70 |
| `document.cookie` | 60 |
| `<iframe` | 50 |
| `<object`, `<embed` | 45 |
| `expression()` (IE CSS) | 45 |

Must handle: case variations, entity encoding (`&#x3C;script&#x3E;`), unicode escapes (`\u003C`), null byte insertion.

#### Path Traversal (`detection/pathtraversal/`)

| Pattern | Score |
|---------|-------|
| `../` (single) | 40 |
| `../../..` (3+ levels) | 70 |
| `/etc/passwd` | 90 |
| `/etc/shadow` | 95 |
| `/proc/self` | 90 |
| `..%2f` (encoded) | 80 |
| `..%252f` (double-encoded) | 90 |
| `%c0%af` (overlong UTF-8) | 95 |
| `file://` | 70 |

Compares normalized vs raw path — divergence with `..` increases score.

#### Command Injection (`detection/cmdi/`)

| Pattern | Score |
|---------|-------|
| `;` + command | 80 |
| `|` (pipe) | 60 |
| `` ` `` (backtick) | 85 |
| `$()` (subshell) | 85 |
| `&&`, `||` chaining | 65 |
| `id`, `whoami`, `uname` | 70 |
| `nc`, `netcat` | 90 |
| `/bin/sh`, `/bin/bash` | 95 |
| `python -c`, `perl -e` | 85 |
| `base64` decode pipe | 90 |

#### XXE (`detection/xxe/`)

Only active when `Content-Type` contains `xml`.

| Pattern | Score |
|---------|-------|
| `<!DOCTYPE` | 30 |
| `<!ENTITY` | 70 |
| `SYSTEM "file://` | 95 |
| `SYSTEM "http://` | 80 |
| `<!ENTITY %` (parameter entity) | 85 |

#### SSRF (`detection/ssrf/`)

Scans URL parameters, JSON body values, XML content.

| Pattern | Score |
|---------|-------|
| `http://127.0.0.1` | 80 |
| `http://localhost` | 80 |
| `http://169.254.169.254` (cloud metadata) | 95 |
| Private ranges (10.x, 172.16.x, 192.168.x) | 70 |
| Decimal IP (`http://2130706433`) | 90 |
| Octal IP (`http://0177.0.0.1`) | 90 |
| URL with `@` bypass | 75 |

Uses `pkg/utils.IsPrivateIP` for private range detection.

### 6.3 Exclusions

Per-path, per-detector exclusions to reduce false positives:

```yaml
waf:
  detection:
    exclusions:
      - path: "/api/webhook/*"
        detectors: ["sqli"]
        reason: "Webhook payloads may contain SQL"
      - path: "/admin/query"
        detectors: ["sqli", "cmdi"]
        condition: "whitelist"
```

### 6.4 Configuration

```yaml
waf:
  detection:
    enabled: true
    mode: "enforce"
    threshold:
      block: 50
      log: 25
    detectors:
      sqli: { enabled: true, score_multiplier: 1.0 }
      xss: { enabled: true, score_multiplier: 1.0 }
      path_traversal: { enabled: true, score_multiplier: 1.0 }
      cmdi: { enabled: true, score_multiplier: 1.0 }
      xxe: { enabled: true, score_multiplier: 1.0 }
      ssrf: { enabled: true, score_multiplier: 1.0 }
```

---

## 7. Layer 5: Bot Detection

**Package:** `internal/waf/botdetect/`

### 7.1 TLS Fingerprinting (JA3)

Hook into `tls.Config.GetConfigForClient` to extract `*tls.ClientHelloInfo`. Compute JA3 hash from: SSL version, cipher suites, extensions, elliptic curves, EC point formats.

Maintain embedded known-fingerprint database:
- **Known good:** Chrome, Firefox, Safari, Edge (major versions)
- **Known bad:** Python requests, Go default client, curl, sqlmap, nikto
- **Suspicious:** Headless Chrome, Selenium, Puppeteer

| Condition | Score |
|-----------|-------|
| Known scanner fingerprint | 80 |
| JA3/UA mismatch (JA3=Chrome, UA=Firefox) | 70 |
| Unknown fingerprint | 20 |
| No TLS (plain HTTP) | 10 |

### 7.2 User-Agent Analysis

| Condition | Score |
|-----------|-------|
| Empty UA | 40 |
| Known scanner (sqlmap, nikto, nmap) | 90 |
| UA/TLS mismatch | 60 |
| Missing version | 30 |
| Severely outdated browser | 30 |

### 7.3 Behavioral Analysis

Per-IP sliding window metrics (default 5 minute window):

| Metric | Threshold | Score |
|--------|-----------|-------|
| RPS > 5 AND unique paths > 50/min | — | 70 |
| Error rate > 30% | — | 50 |
| Timing stddev < 10ms | — | 60 |
| No session + no encoding + no referer | — | 55 |

### 7.4 Configuration

```yaml
waf:
  bot_detection:
    enabled: true
    mode: "monitor"
    tls_fingerprint:
      enabled: true
      known_bots_action: "block"
      unknown_action: "log"
    user_agent:
      enabled: true
      block_empty: true
      block_known_scanners: true
    behavior:
      enabled: true
      window: "5m"
      rps_threshold: 10
```

---

## 8. Layer 6: Response Protection

**Package:** `internal/waf/response/`

### 8.1 Security Headers

Injected on all responses:

| Header | Default Value |
|--------|--------------|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` |
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `SAMEORIGIN` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` |
| `X-XSS-Protection` | `0` (CSP preferred) |
| `Content-Security-Policy` | Disabled by default (app-specific) |

### 8.2 Sensitive Data Masking

Scan response bodies (text/html, application/json, text/plain only):

| Pattern | Mask |
|---------|------|
| Credit card numbers | `4532****0123` |
| SSN (`XXX-XX-XXXX`) | `***-**-1234` |
| API keys (`sk_live_*`, `ghp_*`, etc.) | `sk_live_****abcd` |
| Stack traces (production mode) | Generic error message |

### 8.3 Error Page Standardization

Replace backend 5xx error pages with generic OLB error pages in production mode. Pass through in development mode.

### 8.4 Configuration

```yaml
waf:
  response:
    security_headers:
      enabled: true
      hsts: { enabled: true, max_age: 31536000, include_subdomains: true }
      x_content_type_options: true
      x_frame_options: "SAMEORIGIN"
      referrer_policy: "strict-origin-when-cross-origin"
    data_masking:
      enabled: true
      mask_credit_cards: true
      mask_ssn: true
      mask_api_keys: true
      strip_stack_traces: true
    error_pages:
      enabled: true
      mode: "production"
```

---

## 9. WAF Event Logging & Analytics

**Package:** `internal/waf/` (event.go, analytics.go)

### 9.1 Structured Event

```go
type WAFEvent struct {
    Timestamp  time.Time  `json:"timestamp"`
    RequestID  string     `json:"request_id"`
    RemoteIP   string     `json:"remote_ip"`
    Method     string     `json:"method"`
    Path       string     `json:"path"`
    UserAgent  string     `json:"user_agent"`
    Layer      string     `json:"layer"`
    Action     string     `json:"action"`
    TotalScore int        `json:"total_score"`
    Findings   []Finding  `json:"findings"`
    LatencyNS  int64      `json:"latency_ns"`
    NodeID     string     `json:"node_id"`
}
```

### 9.2 Analytics

In-memory rolling counters (24h sliding window):
- Total / blocked / monitored request counts
- Per-detector hit counts
- Top blocked IPs (top-K with min-heap, uses `pkg/utils.RingBuffer`)
- Attack timeline (per-minute buckets, 1440 entries for 24h)

Raft-synced periodically for cluster-wide aggregation.

---

## 10. MCP Integration

Add ~15 tools to OLB's existing MCP server via `Server.RegisterTool()`:

### IP ACL Tools
- `waf_add_whitelist` — Add IP/CIDR to whitelist
- `waf_add_blacklist` — Add IP/CIDR to blacklist
- `waf_remove_whitelist` — Remove from whitelist
- `waf_remove_blacklist` — Remove from blacklist
- `waf_list_rules` — List all IP ACL rules

### Rate Limit Tools
- `waf_add_rate_rule` — Add rate limiting rule
- `waf_remove_rate_rule` — Remove rate limiting rule

### Detection Tools
- `waf_add_exclusion` — Add detector exclusion for path
- `waf_remove_exclusion` — Remove exclusion

### Control Tools
- `waf_set_mode` — Set mode per-layer or globally
- `waf_get_config` — Get current WAF configuration

### Analytics Tools
- `waf_get_stats` — Get WAF statistics for last N hours
- `waf_get_top_blocked_ips` — Top N blocked IPs
- `waf_get_attack_timeline` — Attacks per minute
- `waf_search_events` — Search events by filter

Write operations go through Raft (`cluster.Propose`). Read operations are local.

---

## 11. Raft Integration

### 11.1 WAF Command Types

Extend `cluster.ConfigCommandType` with WAF-specific commands:

```go
const (
    CmdWAFAddWhitelist    ConfigCommandType = "waf_add_whitelist"
    CmdWAFRemoveWhitelist ConfigCommandType = "waf_remove_whitelist"
    CmdWAFAddBlacklist    ConfigCommandType = "waf_add_blacklist"
    CmdWAFRemoveBlacklist ConfigCommandType = "waf_remove_blacklist"
    CmdWAFAddRateRule     ConfigCommandType = "waf_add_rate_rule"
    CmdWAFRemoveRateRule  ConfigCommandType = "waf_remove_rate_rule"
    CmdWAFSetMode         ConfigCommandType = "waf_set_mode"
    CmdWAFSyncCounters    ConfigCommandType = "waf_sync_counters"
)
```

### 11.2 State Machine Extension

WAF state is applied via the existing `ConfigStateMachine.Apply()` method by adding new command type handlers. WAF state is part of the cluster's replicated state.

---

## 12. Configuration (Full Reference)

```yaml
waf:
  enabled: true
  mode: "enforce"          # global: "enforce", "monitor", "disabled"

  ip_acl:
    enabled: true
    whitelist: []
    blacklist: []
    auto_ban:
      enabled: true
      default_ttl: "1h"
      max_ttl: "24h"

  rate_limit:
    enabled: true
    sync_interval: "5s"
    rules: []

  sanitizer:
    enabled: true
    max_header_size: 8192
    max_header_count: 100
    max_body_size: 10485760
    max_url_length: 8192
    block_null_bytes: true
    normalize_encoding: true

  detection:
    enabled: true
    mode: "enforce"
    threshold:
      block: 50
      log: 25
    detectors:
      sqli: { enabled: true, score_multiplier: 1.0 }
      xss: { enabled: true, score_multiplier: 1.0 }
      path_traversal: { enabled: true, score_multiplier: 1.0 }
      cmdi: { enabled: true, score_multiplier: 1.0 }
      xxe: { enabled: true, score_multiplier: 1.0 }
      ssrf: { enabled: true, score_multiplier: 1.0 }
    exclusions: []

  bot_detection:
    enabled: true
    mode: "monitor"
    tls_fingerprint: { enabled: true }
    user_agent: { enabled: true, block_empty: true }
    behavior: { enabled: true, window: "5m" }

  response:
    security_headers:
      enabled: true
      hsts: { enabled: true, max_age: 31536000 }
      x_content_type_options: true
      x_frame_options: "SAMEORIGIN"
      referrer_policy: "strict-origin-when-cross-origin"
    data_masking:
      enabled: true
      mask_credit_cards: true
      mask_api_keys: true
      strip_stack_traces: true
    error_pages:
      enabled: true
      mode: "production"

  logging:
    level: "info"
    format: "json"
    log_allowed: false
    log_blocked: true
```

---

## 13. Performance Requirements

| Metric | Target |
|--------|--------|
| IP ACL lookup | < 100ns (radix tree) |
| Rate limit check | < 500ns (local bucket) |
| Request sanitization | < 50μs |
| WAF detection (all detectors) | < 500μs p95 |
| Bot detection (without behavioral) | < 100μs |
| Response header injection | < 10μs |
| **Total WAF pipeline** | **< 1ms p99 for clean requests** |
| Memory per 10K active IPs | < 10MB |
| Raft sync overhead | < 1KB per sync cycle |

---

## 14. Testing Strategy

### Detection Accuracy
- True positive tests: known attack payloads that MUST be detected
- True negative tests: legitimate inputs that MUST NOT be blocked
- Edge case tests: tricky inputs at boundary conditions

### Integration Tests
- Full request lifecycle through all 6 layers
- Whitelist bypass verification
- Rate limit distributed sync across 3-node cluster
- MCP tool → Raft → WAF state → request handling

### Performance Tests
- Benchmark each layer individually
- Benchmark full pipeline with clean requests
- Memory allocation profiling
- Fuzz testing all detectors
