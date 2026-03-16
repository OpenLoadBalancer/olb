# WAF Implementation Decisions

> Architecture decisions, algorithm choices, data structure selections, and integration points
> for the WAF layer in OpenLoadBalancer.

---

## 1. Strategy: Extend, Don't Replace

### Decision
Keep the existing `internal/waf/waf.go` (582 lines) as a compatibility layer but refactor it to delegate to the new detection engine. The existing `Rule`, `Match`, `Config`, and scoring types remain public API. New specialized detectors plug into the existing scoring model.

### Rationale
- The existing WAF has 792 lines of tests and is wired into `engine.go`
- Its `Rule.Match()` regex-based approach works for basic detection
- The new detection engine adds tokenizer-based detection alongside regex rules
- Existing tests continue to pass throughout migration

### Approach
1. New code goes in sub-packages: `ipacl/`, `ratelimit/`, `sanitizer/`, `detection/`, `botdetect/`, `response/`
2. `waf.go` gets a new `WAFMiddleware` struct that orchestrates all 6 layers
3. The old `WAF.Process()` method is called by Layer 4's detection engine as one of many detectors (the "legacy regex detector")
4. `engine.go`'s `wafMiddlewareAdapter` is updated to use the new `WAFMiddleware`

---

## 2. Middleware Integration

### Decision: 6 Layers as Single Middleware, Not 6 Separate Middlewares

Despite the spec describing 6 "layers", they are implemented as a **single WAF middleware** that internally chains the 6 layers. This avoids:
- Polluting the middleware chain with 6 entries
- Complexity of managing inter-layer communication (whitelist bypass flag)
- Priority conflicts with existing middleware

### Implementation

```go
// internal/waf/waf.go — new WAFMiddleware

type WAFMiddleware struct {
    config     *WAFConfig
    ipACL      *ipacl.IPAccessList
    rateLimiter *ratelimit.RateLimiter
    sanitizer  *sanitizer.Sanitizer
    detection  *detection.Engine
    botDetect  *botdetect.BotDetector
    response   *response.Protection
    events     *EventLogger
    analytics  *Analytics
}

func (w *WAFMiddleware) Wrap(next http.Handler) http.Handler {
    return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
        // Layer 1: IP ACL
        if action := w.ipACL.Check(r); action == ActionBlock {
            w.block(rw, r, "ip_acl"); return
        } else if action == ActionBypass {
            next.ServeHTTP(rw, r); return  // whitelist bypass
        }

        // Layer 2: Rate Limit
        if !w.rateLimiter.Allow(r) {
            w.rateLimit(rw, r); return
        }

        // Layer 3: Sanitize + build context
        ctx, err := w.sanitizer.Process(r)
        if err != nil {
            w.block(rw, r, "sanitizer"); return
        }

        // Layer 4: Detection
        findings := w.detection.Detect(ctx)
        if w.shouldBlock(findings) {
            w.block(rw, r, "detection"); return
        }

        // Layer 5: Bot Detection
        botFindings := w.botDetect.Analyze(r, ctx)
        if w.shouldBlock(botFindings) {
            w.block(rw, r, "bot"); return
        }

        // Layer 6: Response Protection (wraps ResponseWriter)
        wrappedRW := w.response.Wrap(rw)
        next.ServeHTTP(wrappedRW, r)
    })
}
```

### engine.go Changes

Minimal change — update `wafMiddlewareAdapter` to use new `WAFMiddleware`:

```go
// Before:
chain.Use(&wafMiddlewareAdapter{waf: w})

// After:
chain.Use(wafMiddleware)  // WAFMiddleware implements middleware.Middleware
```

---

## 3. Algorithm & Data Structure Choices

### 3.1 IP ACL: Reuse Existing Radix Trie

**Decision:** Use `pkg/utils.CIDRMatcher` (already implements radix trie with IPv4/IPv6 support) instead of building a new one.

**Extension needed:** The existing `CIDRMatcher` only returns `bool` from `Contains()`. We need metadata (expiry, reason, source). Solution: wrap `CIDRMatcher` with a metadata map:

```go
type MetadataRadixTree struct {
    matcher  *utils.CIDRMatcher    // fast path: O(k) lookup
    entries  map[string]*RuleMetadata  // keyed by canonical CIDR string
}
```

The `matcher.Contains()` gives O(k) performance for the hot path. Metadata lookup only happens when there's a match (cold path).

### 3.2 Rate Limiter: Token Bucket

**Decision:** Token bucket (same as existing `middleware.RateLimitMiddleware`).

**Why not sliding window or leaky bucket:**
- Token bucket naturally handles bursts (important for legitimate traffic spikes)
- Simple to implement and reason about
- Easy to sync via Raft (only need to sync consumed token counts)

**Distributed sync:** Periodic delta sync via Raft, not per-request. Each node tracks `localConsumed` since last sync. Every `sync_interval`:
1. Leader collects deltas from all nodes
2. Leader applies combined delta to canonical state
3. Followers receive updated canonical state

### 3.3 SQL Injection: Tokenizer, Not Pure Regex

**Decision:** Build a simple SQL tokenizer that classifies input into token types, then score based on token sequences.

**Why not pure regex (like current implementation):**
- Regex can't handle context: `"John O'Brien"` triggers `'` + `OR` false positive
- Regex can't detect obfuscation: `UN/**/ION SE/**/LECT` bypasses `UNION SELECT` pattern
- Tokenizer handles string literals properly: content inside quotes is not checked for keywords

**Tokenizer approach:**
1. Input → token stream (handle quotes, comments, whitespace)
2. Token stream → pattern matching (dangerous sequences like `KEYWORD_UNION KEYWORD_SELECT`)
3. Pattern → score (weighted by pattern dangerousness)

The tokenizer is ~200 lines. It doesn't need to be a full SQL parser — just enough to distinguish keywords from string literals and detect dangerous sequences.

### 3.4 XSS: Tag Parser, Not Full HTML Parser

**Decision:** Simple state machine that detects `<tagname` patterns and extracts attribute names.

**Implementation:** Scan for `<`, check if followed by known dangerous tag names (`script`, `img`, `svg`, `iframe`, etc.) or `on`-event attributes. Handle case variations and encoding.

### 3.5 Analytics: Ring Buffer + Min-Heap

**Decision:**
- Attack timeline → `pkg/utils.RingBuffer` (already exists), 1440 entries for 24h of per-minute buckets
- Top blocked IPs → min-heap with fixed size (top-K tracker)
- Per-detector counters → simple `map[string]*atomic.Int64`

### 3.6 Bot Detection: JA3 via GetConfigForClient

**Decision:** Hook into TLS handshake via `tls.Config.GetConfigForClient` callback.

**Implementation:** This callback receives `*tls.ClientHelloInfo` containing cipher suites, supported versions, server name. Compute JA3 string and MD5 hash. Store in connection context for later retrieval by bot detection middleware.

**Integration point:** `internal/tls/manager.go` — add `GetConfigForClient` callback that computes JA3 and stores it.

---

## 4. Configuration Integration

### Decision: Extend Existing WAFConfig

The existing `config.WAFConfig` is minimal (`Enabled` + `Mode`). Extend it with full WAF configuration:

```go
// internal/config/config.go

type WAFConfig struct {
    Enabled      bool                `yaml:"enabled" json:"enabled"`
    Mode         string              `yaml:"mode" json:"mode"`
    IPACL        *WAFIPACLConfig     `yaml:"ip_acl" json:"ip_acl"`
    RateLimit    *WAFRateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
    Sanitizer    *WAFSanitizerConfig `yaml:"sanitizer" json:"sanitizer"`
    Detection    *WAFDetectionConfig `yaml:"detection" json:"detection"`
    BotDetection *WAFBotConfig       `yaml:"bot_detection" json:"bot_detection"`
    Response     *WAFResponseConfig  `yaml:"response" json:"response"`
    Logging      *WAFLoggingConfig   `yaml:"logging" json:"logging"`
}
```

Each sub-config has sensible defaults. If a sub-config is nil, the layer uses defaults. If `enabled: false`, the layer is skipped entirely.

---

## 5. Raft Integration

### Decision: Extend ConfigStateMachine, Not Create New State Machine

The existing `cluster.ConfigStateMachine` handles config changes via Raft. WAF state changes are added as new command types in the same state machine.

**Why not a separate WAF state machine:**
- OLB's Raft implementation supports one state machine per cluster
- WAF state is logically part of cluster configuration
- Single state machine simplifies snapshotting and recovery

### New Command Types

Add to `cluster/config_sm.go`:

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

### Apply Flow

```
MCP Tool Call (e.g., waf_add_blacklist)
    → Validate parameters
    → Create ConfigCommand{Type: CmdWAFAddBlacklist, Payload: ...}
    → cluster.Propose(command)
    → Raft replicates to all nodes
    → ConfigStateMachine.Apply() on each node
    → WAF state updated on all nodes
    → Return result to MCP caller
```

### Callback Pattern

WAF registers a callback with the state machine:

```go
stateMachine.OnWAFCommand = func(cmdType ConfigCommandType, payload json.RawMessage) error {
    return wafMiddleware.ApplyRaftCommand(cmdType, payload)
}
```

---

## 6. MCP Integration

### Decision: Register WAF Tools in engine.go During MCP Server Setup

The existing MCP server in `engine.go` registers tools during `New()`. WAF tools are added alongside existing tools.

### Implementation Pattern

```go
// internal/waf/mcp/tools.go

func RegisterTools(server *mcp.Server, waf *WAFMiddleware, cluster *cluster.Cluster) {
    server.RegisterTool(mcp.Tool{
        Name:        "waf_add_blacklist",
        Description: "Add IP/CIDR to WAF blacklist",
        InputSchema: mcp.InputSchema{...},
    }, func(params json.RawMessage) (*mcp.ToolResult, error) {
        // Parse params, validate, propose via Raft
        return handleAddBlacklist(params, waf, cluster)
    })
    // ... register other tools
}
```

Read-only tools (stats, list rules) query WAF state directly. Write tools go through Raft.

---

## 7. RequestContext — Shared State Between Layers

### Decision: Single RequestContext Struct Built by Sanitizer, Used by All Subsequent Layers

```go
// internal/waf/context.go

type RequestContext struct {
    // Original request
    Method      string
    Path        string
    RawPath     string
    Query       string
    Headers     map[string][]string
    Cookies     map[string]string
    Body        []byte
    ContentType string
    RemoteIP    string

    // Normalized by sanitizer
    DecodedPath  string
    DecodedQuery string
    DecodedBody  string
    BodyParams   map[string]string

    // Set by ip_acl layer
    IsWhitelisted bool

    // Set by bot detection
    JA3Hash string
}
```

Stored in `http.Request.Context()` via `context.WithValue`. Each layer reads/writes to this shared context.

---

## 8. Error Handling & Recovery

### Decision: `recover()` Wrapper in WAF Middleware

If any WAF layer panics, the middleware catches it via `recover()`, logs the panic, and **allows the request through** (fail-open). This prevents a WAF bug from causing a complete outage.

```go
func (w *WAFMiddleware) Wrap(next http.Handler) http.Handler {
    return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
        defer func() {
            if err := recover(); err != nil {
                w.logPanic(r, err)
                next.ServeHTTP(rw, r)  // fail-open
            }
        }()
        // ... layer processing
    })
}
```

---

## 9. File Structure

```
internal/waf/
    waf.go              ← Existing (refactored to WAFMiddleware)
    waf_test.go         ← Existing (preserved)
    config.go           ← WAF config types (NEW)
    context.go          ← RequestContext (NEW)
    event.go            ← WAFEvent logging (NEW)
    analytics.go        ← Rolling counters, TopK (NEW)

    ipacl/
        ipacl.go        ← IP ACL middleware logic (NEW)
        ipacl_test.go   (NEW)

    ratelimit/
        ratelimit.go    ← Rate limiter middleware (NEW)
        bucket.go       ← Token bucket (NEW)
        sync.go         ← Raft sync logic (NEW)
        ratelimit_test.go (NEW)

    sanitizer/
        sanitizer.go    ← Sanitizer middleware (NEW)
        normalize.go    ← URL/encoding normalization (NEW)
        validate.go     ← Header/body validation (NEW)
        sanitizer_test.go (NEW)

    detection/
        engine.go       ← Detection orchestrator (NEW)
        detector.go     ← Detector interface (NEW)
        finding.go      ← Finding type (NEW)

        sqli/
            sqli.go     ← SQL injection detector (NEW)
            tokenizer.go ← SQL tokenizer (NEW)
            patterns.go  ← SQL keyword database (NEW)
            sqli_test.go (NEW)

        xss/
            xss.go      ← XSS detector (NEW)
            parser.go   ← HTML tag parser (NEW)
            patterns.go ← XSS patterns (NEW)
            xss_test.go (NEW)

        pathtraversal/
            pathtraversal.go (NEW)
            sensitive_paths.go (NEW)
            pathtraversal_test.go (NEW)

        cmdi/
            cmdi.go     (NEW)
            patterns.go (NEW)
            cmdi_test.go (NEW)

        xxe/
            xxe.go      (NEW)
            xxe_test.go (NEW)

        ssrf/
            ssrf.go     (NEW)
            ipcheck.go  (NEW)
            ssrf_test.go (NEW)

    botdetect/
        botdetect.go    ← Bot detection middleware (NEW)
        ja3.go          ← JA3 fingerprint computation (NEW)
        fingerprints.go ← Known fingerprint database (NEW)
        useragent.go    ← UA analysis (NEW)
        behavior.go     ← Behavioral analysis (NEW)
        botdetect_test.go (NEW)

    response/
        response.go     ← Response protection middleware (NEW)
        headers.go      ← Security headers (NEW)
        masking.go      ← Data masking (NEW)
        errorpage.go    ← Error page standardization (NEW)
        response_test.go (NEW)

    mcp/
        tools.go        ← MCP tool definitions (NEW)
        handlers.go     ← MCP tool handlers (NEW)
```

**Total new files:** ~38
**Estimated new code:** ~12,000–15,000 lines (including tests)
**Modified existing files:** 3 (engine.go, config.go, config_sm.go)

---

## 10. Reusable Existing Code

| Existing Code | Used By |
|--------------|---------|
| `pkg/utils.CIDRMatcher` | Layer 1 (IP ACL) |
| `pkg/utils.IsPrivateIP` | Layer 4 (SSRF detector) |
| `pkg/utils.ExtractIP` | All layers (client IP extraction) |
| `pkg/utils.RingBuffer` | Analytics (timeline) |
| `pkg/utils.BloomFilter` | Bot detection (fingerprint DB, optional) |
| `middleware.RateLimitMiddleware` (pattern) | Layer 2 (token bucket algorithm reference) |
| `cluster.Propose()` | Raft write operations |
| `cluster.ConfigStateMachine` | WAF state replication |
| `mcp.Server.RegisterTool()` | MCP integration |
| `waf.WAF.Process()` | Layer 4 (legacy regex detector) |
