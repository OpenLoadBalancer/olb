# WAF — Web Application Firewall

OpenLoadBalancer includes a built-in 6-layer Web Application Firewall that protects your backends from common web attacks. Zero external dependencies, sub-millisecond overhead, configurable per-layer.

## Architecture

Every HTTP request passes through 6 security layers in order:

```
Client → [IP ACL] → [Rate Limit] → [Sanitizer] → [Detection] → [Bot] → [Response] → Backend
```

| Layer | Purpose | Action |
|-------|---------|--------|
| 1. IP ACL | Whitelist/blacklist by IP/CIDR | Bypass or 403 |
| 2. Rate Limiter | Token bucket per IP/path/key | 429 + Retry-After |
| 3. Sanitizer | Validate headers, body, URL; normalize encoding | 400/413/414/431 |
| 4. Detection | Score-based attack detection (6 detectors) | 403 if score >= threshold |
| 5. Bot Detection | TLS fingerprint, UA analysis, behavioral | 403 |
| 6. Response | Security headers, data masking, error pages | Headers injected |

## Quick Start

Minimal WAF config:

```yaml
waf:
  enabled: true
  mode: enforce    # enforce, monitor, disabled
```

This enables all detectors with default thresholds. Attacks scoring >= 50 are blocked.

## Detection Engine

Six specialized detectors, each producing a threat score (0-100):

| Detector | Detects | Example |
|----------|---------|---------|
| **SQLi** | SQL injection (tokenizer-based) | `' OR 1=1 --`, `UNION SELECT` |
| **XSS** | Cross-site scripting | `<script>`, `javascript:`, `onerror=` |
| **Path Traversal** | Directory traversal | `../../etc/passwd`, `%c0%af` |
| **CMDi** | Command injection | `; cat /etc/passwd`, `$(whoami)` |
| **XXE** | XML external entity | `<!ENTITY SYSTEM "file:///">` |
| **SSRF** | Server-side request forgery | `http://169.254.169.254/` |

Scores accumulate per-request. Default thresholds:
- **Block**: total score >= 50
- **Log**: total score >= 25

## Full Configuration

```yaml
waf:
  enabled: true
  mode: enforce

  ip_acl:
    enabled: true
    whitelist:
      - cidr: "10.0.0.0/8"
        reason: "Internal network"
    blacklist:
      - cidr: "203.0.113.0/24"
        reason: "Known bad actor"
        expires: "2025-12-31T23:59:59Z"
    auto_ban:
      enabled: true
      default_ttl: "1h"
      max_ttl: "24h"

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
        auto_ban_after: 5

  sanitizer:
    enabled: true
    max_header_size: 8192
    max_body_size: 10485760    # 10MB
    max_url_length: 8192
    block_null_bytes: true
    normalize_encoding: true
    path_overrides:
      - path: "/upload/*"
        max_body_size: 104857600  # 100MB

  detection:
    enabled: true
    mode: enforce
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
    exclusions:
      - path: "/api/webhook/*"
        detectors: ["sqli"]
        reason: "Webhooks may contain SQL-like content"

  bot_detection:
    enabled: true
    mode: monitor
    user_agent:
      enabled: true
      block_empty: true
      block_known_scanners: true
    behavior:
      enabled: true
      window: "5m"
      rps_threshold: 10

  response:
    security_headers:
      enabled: true
      hsts:
        enabled: true
        max_age: 31536000
        include_subdomains: true
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
    log_blocked: true
```

## Modes

| Mode | Behavior |
|------|----------|
| `enforce` | Block attacks (403), log events |
| `monitor` | Log attacks but allow through |
| `disabled` | WAF completely bypassed |

Each layer can be independently enabled/disabled. Start with `monitor` mode and graduate to `enforce` after tuning.

## MCP Tools

Manage WAF via AI agents using the MCP server:

| Tool | Description |
|------|-------------|
| `waf_status` | Get WAF layers, mode, statistics |
| `waf_add_whitelist` | Add IP/CIDR to whitelist |
| `waf_add_blacklist` | Add IP/CIDR to blacklist |
| `waf_remove_whitelist` | Remove from whitelist |
| `waf_remove_blacklist` | Remove from blacklist |
| `waf_list_rules` | List all IP ACL rules |
| `waf_get_stats` | Request counts, detector hits |
| `waf_get_top_blocked_ips` | Most blocked IP addresses |
| `waf_get_attack_timeline` | Attacks per minute |

## Admin API

```
GET /api/v1/waf/status
```

Returns enabled layers, current mode, and real-time statistics.

## Performance

| Metric | Result |
|--------|--------|
| IP ACL lookup | 69ns |
| Full pipeline (clean request) | 35μs |
| Full pipeline (parallel) | 15μs |
| Target | < 1ms p99 |
