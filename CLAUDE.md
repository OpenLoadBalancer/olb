# CLAUDE.md — OpenLoadBalancer

## Project
High-performance zero-dependency L4/L7 load balancer written in Go (stdlib only).
- Website: https://openloadbalancer.dev
- Repo: github.com/openloadbalancer/olb
- Go 1.25+, single binary, 310 files, ~170K lines

## Build & Test
```bash
go build ./cmd/olb/           # Build binary
go test ./...                  # Run all tests (36 packages)
go test -race ./...            # Race detector (needs GCC/Linux)
go test -cover ./...           # Coverage (89%)
gofmt -w .                     # Format all
go vet ./...                   # Lint
make build                     # Build via Makefile
make test                      # Test via Makefile
make build-all                 # Cross-platform (linux/darwin/windows/freebsd)
```

## Architecture
```
cmd/olb/main.go          → Entry point, CLI routing
internal/engine/          → Central orchestrator (engine.go wires everything)
internal/proxy/l7/        → HTTP reverse proxy + WebSocket/gRPC/SSE detection
internal/proxy/l4/        → TCP/UDP proxy, SNI routing, PROXY protocol
internal/balancer/        → 14 load balancing algorithms
internal/middleware/      → 16 middleware components (config-gated)
internal/config/          → YAML/TOML/HCL/JSON config + hot reload
internal/admin/           → REST API + Web UI serving
internal/cluster/         → Raft consensus + SWIM gossip
internal/mcp/             → MCP server for AI integration
internal/tls/             → TLS, mTLS, OCSP stapling
internal/acme/            → ACME/Let's Encrypt client
internal/health/          → Active + passive health checking (HTTP, TCP, gRPC)
internal/waf/             → WAF: 6-layer security pipeline (IP ACL, rate limit, sanitizer, detection, bot, response)
  waf/ipacl/              → Radix tree whitelist/blacklist with auto-ban
  waf/ratelimit/          → Token bucket rate limiter with distributed sync
  waf/sanitizer/          → Request validation + URL/encoding normalization
  waf/detection/          → Scoring engine + 6 detectors (sqli, xss, pathtraversal, cmdi, xxe, ssrf)
  waf/botdetect/          → JA3 fingerprinting, UA analysis, behavioral analysis
  waf/response/           → Security headers, data masking, error pages
  waf/mcp/                → 8 MCP tools for WAF management
internal/security/        → Request smuggling, header injection protection
internal/webui/           → Embedded SPA (vanilla JS/CSS)
internal/plugin/          → Plugin system with event bus
internal/discovery/       → Service discovery (static/DNS/file/Docker/Consul)
pkg/utils/                → Buffer pool, LRU, ring buffer, CIDR matcher
pkg/errors/               → Sentinel errors with context
```

## Key Rules
1. **Zero external deps** — only Go stdlib (x/crypto for bcrypt/OCSP is the only exception)
2. **All features must be wired in engine.go** — no dead code allowed
3. **Config-gated middleware** — each middleware has `enabled: true/false`
4. **Port 0 in tests** — never hardcode ports, use `net.Listen(":0")`
5. **gofmt clean** — CI enforces formatting
6. **Only openloadbalancer.dev** — no other domains in any file

## Config Format
```yaml
listeners:
  - name: http
    address: ":8080"
    protocol: http          # http, https, tcp, udp
    routes:
      - path: /
        pool: my-pool

pools:
  - name: my-pool
    algorithm: round_robin  # rr, wrr, lc, lrt, ip_hash, ch, maglev, p2c, random, ring_hash
    backends:
      - address: "localhost:3001"
      - address: "localhost:3002"

middleware:
  rate_limit:
    enabled: true
    requests_per_second: 100
  cors:
    enabled: true
admin:
  address: ":9090"

waf:
  enabled: true
  mode: enforce               # enforce, monitor, disabled
  ip_acl:
    enabled: true
    whitelist: [{cidr: "10.0.0.0/8", reason: "internal"}]
    auto_ban: {enabled: true, default_ttl: "1h"}
  rate_limit:
    enabled: true
    rules:
      - {id: "per-ip", scope: "ip", limit: 1000, window: "1m"}
  sanitizer: {enabled: true}
  detection:
    enabled: true
    threshold: {block: 50, log: 25}
    detectors:
      sqli: {enabled: true}
      xss: {enabled: true}
  bot_detection: {enabled: true, mode: monitor}
  response:
    security_headers: {enabled: true}
    data_masking: {enabled: true, mask_credit_cards: true}
```

## Common Patterns
- Engine wiring: `internal/engine/engine.go` → `New()`, `Start()`, `Shutdown()`
- Balancer switch: `initializePools()` — all 14 algorithms
- Middleware chain: `createMiddlewareChain()` — all 16 middleware
- Protocol detection: `proxy.go` → `proxyHandler()` checks WebSocket/gRPC/SSE headers
- Backend state: `StateStarting` → set `StateUp` after creation
- Backend ID: auto-generated from address if not specified
