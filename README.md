# OpenLoadBalancer

<p align="center">
  <img src="olb.png" alt="OpenLoadBalancer" width="100%">
</p>

<p align="center">
  <a href="https://openloadbalancer.dev"><img src="https://img.shields.io/badge/web-openloadbalancer.dev-blue" alt="Website"></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
  <a href="https://github.com/openloadbalancer/olb/releases"><img src="https://img.shields.io/github/v/release/openloadbalancer/olb" alt="Release"></a>
  <a href="./"><img src="https://img.shields.io/badge/tests-56_E2E_%2B_49_unit-brightgreen" alt="Tests"></a>
  <a href="./"><img src="https://img.shields.io/badge/coverage-90%25-brightgreen" alt="Coverage"></a>
  <a href="./"><img src="https://img.shields.io/badge/deps-zero-orange" alt="Zero Deps"></a>
</p>

## Quick Start

```bash
curl -sSL https://openloadbalancer.dev/install.sh | sh
```

Create `olb.yaml`:

```yaml
admin:
  address: "127.0.0.1:8081"

listeners:
  - name: http
    address: ":80"
    routes:
      - path: /
        pool: web

pools:
  - name: web
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
    health_check:
      type: http
      path: /health
      interval: 10s
```

```bash
olb start --config olb.yaml
```

That's it. HTTP proxy on `:80`, admin API on `:8081`, health checks every 10s, round-robin across two backends.

## Install

```bash
# Binary
curl -sSL https://openloadbalancer.dev/install.sh | sh

# Docker
docker pull ghcr.io/openloadbalancer/olb:latest
docker run -d -p 80:80 -p 8081:8081 -v ./olb.yaml:/etc/olb/configs/olb.yaml ghcr.io/openloadbalancer/olb:latest

# Homebrew
brew tap openloadbalancer/olb && brew install olb

# Build from source
git clone https://github.com/openloadbalancer/olb.git && cd olb && make build
```

Requires Go 1.25+. No other dependencies.

## Features

**Proxy:** HTTP/HTTPS, WebSocket, gRPC, SSE, TCP (L4), UDP (L4), SNI routing, PROXY protocol v1/v2

**Load Balancing:** 12 algorithms — Round Robin, Weighted RR, Least Connections, Least Response Time, IP Hash, Consistent Hash (Ketama), Maglev, Ring Hash, Power of Two, Random, Weighted Random, Sticky Sessions

**Security:** TLS termination + SNI, ACME/Let's Encrypt, mTLS, OCSP stapling, 6-layer WAF (IP ACL, rate limiting, request sanitizer, detection engine with SQLi/XSS/path traversal/CMDi/XXE/SSRF, bot detection with JA3 fingerprinting, response protection with security headers + data masking), circuit breaker

**Middleware:** 19 components — WAF (6-layer security pipeline), rate limit, CORS, compression (gzip), IP filter, circuit breaker, retry, response cache, headers, request ID, real IP, access log, metrics

**Observability:** Web UI dashboard (8 pages), TUI (`olb top`), Prometheus metrics, structured JSON logging, admin REST API (15+ endpoints)

**Operations:** Hot config reload (SIGHUP or API), Raft clustering + SWIM gossip, service discovery (Static/DNS/Consul/Docker/File), MCP server for AI integration, plugin system, 30+ CLI commands

## Performance

Benchmarked on AMD Ryzen 9 9950X3D:

| Metric | Result |
|--------|--------|
| Peak RPS | **15,480** (10 concurrent, round_robin) |
| Proxy overhead | **137µs** (direct: 87µs → proxied: 223µs) |
| RoundRobin.Next | **3.5 ns/op**, 0 allocs |
| Middleware overhead | **< 3%** (full stack vs none) |
| WAF overhead (6-layer) | **~35μs** per request, **< 3%** at proxy scale |
| Binary size | **9 MB** |
| P99 latency (50 conc.) | **22ms** |
| Success rate | **100%** across all tests |

<details>
<summary>Algorithm comparison (1000 req, 50 concurrent)</summary>

| Algorithm | RPS | Avg Latency | Distribution |
|-----------|-----|-------------|-------------|
| random | 12,913 | 3.5ms | 32/34/34% |
| maglev | 11,597 | 3.8ms | 68/2/30% |
| ip_hash | 11,062 | 4.0ms | 75/12/13% |
| power_of_two | 10,708 | 4.0ms | 34/33/33% |
| least_connections | 10,119 | 4.4ms | 33/33/34% |
| consistent_hash | 8,897 | 4.6ms | 0/0/100% |
| weighted_rr | 8,042 | 5.6ms | 33/33/34% |
| round_robin | 7,320 | 6.3ms | 35/33/32% |

</details>

<details>
<summary>Full benchmark report</summary>

See [docs/benchmark-report.md](docs/benchmark-report.md) for the complete report including concurrency scaling, backend latency impact, and middleware overhead measurements.

</details>

## E2E Verified

56 end-to-end tests prove every feature works in a real proxy scenario:

| Category | Verified |
|----------|----------|
| **Proxy** | HTTP, HTTPS/TLS, WebSocket, SSE, TCP, UDP |
| **Algorithms** | RR, WRR, LC, IPHash, CH, Maglev, P2C, Random, RingHash |
| **Middleware** | Rate limit (429), CORS, gzip (98% reduction), WAF 6-layer (SQLi/XSS/CMDi/path traversal → 403, rate limit → 429, monitor mode, security headers, bot detection, IP ACL, data masking), IP filter, circuit breaker, cache (HIT/MISS), headers, retry |
| **Operations** | Health check (down/recovery), config reload, weighted distribution, session affinity, graceful failover (0 downtime) |
| **Infra** | Admin API, Web UI, Prometheus, MCP server, multiple listeners |
| **Performance** | 15K RPS, 137µs proxy overhead, 100% success rate |

## Algorithms

| Algorithm | Config Name | Use Case |
|-----------|------------|----------|
| Round Robin | `round_robin` | Default, equal backends |
| Weighted Round Robin | `weighted_round_robin` | Unequal backend capacity |
| Least Connections | `least_connections` | Long-lived connections |
| Least Response Time | `least_response_time` | Latency-sensitive |
| IP Hash | `ip_hash` | Session affinity by IP |
| Consistent Hash | `consistent_hash` | Cache locality |
| Maglev | `maglev` | Google-style hashing |
| Ring Hash | `ring_hash` | Consistent with vnodes |
| Power of Two | `power_of_two` | Balanced random |
| Random | `random` | Simple, no state |

## Configuration

Supports **YAML**, **JSON**, **TOML**, and **HCL** with `${ENV_VAR}` substitution.

```yaml
admin:
  address: "127.0.0.1:8081"

middleware:
  rate_limit:
    enabled: true
    requests_per_second: 1000
  cors:
    enabled: true
    allowed_origins: ["*"]
  compression:
    enabled: true

waf:
  enabled: true
  mode: enforce
  detection:
    enabled: true
    threshold: {block: 50, log: 25}
  bot_detection: {enabled: true, mode: monitor}
  response:
    security_headers: {enabled: true}

listeners:
  - name: http
    address: ":8080"
    routes:
      - path: /api
        pool: api-pool
      - path: /
        pool: web-pool

pools:
  - name: web-pool
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
    health_check:
      type: http
      path: /health
      interval: 5s

  - name: api-pool
    algorithm: least_connections
    backends:
      - address: "10.0.2.10:8080"
        weight: 3
      - address: "10.0.2.11:8080"
        weight: 2
```

See [docs/configuration.md](docs/configuration.md) for all options.

## CLI

```bash
olb start --config olb.yaml         # Start proxy
olb stop                             # Graceful shutdown
olb reload                           # Hot-reload config
olb status                           # Server status
olb top                              # Live TUI dashboard
olb backend list                     # List backends
olb backend drain web-pool 10.0.1.10:8080
olb health show                      # Health check status
olb config validate olb.yaml         # Validate config
olb cluster status                   # Cluster info
```

## Architecture

```
                    ┌─────────────────────────────────────────────────┐
                    │              OpenLoadBalancer                    │
  Clients ─────────┤                                                  │
  HTTP/S, WS,      │  Listeners → Middleware → Router → Balancer → Backends
  gRPC, TCP, UDP   │  (L4/L7)     (19 types)   (trie)   (12 algos)  │
                    │                                                  │
                    │  WAF (6 layers) │ TLS │ Cluster │ MCP │ Web UI  │
                    └─────────────────────────────────────────────────┘
```

## Documentation

| Guide | Description |
|-------|-------------|
| [Getting Started](docs/getting-started.md) | 5-minute quick start |
| [Configuration](docs/configuration.md) | All config options |
| [Algorithms](docs/algorithms.md) | Algorithm details |
| [API Reference](docs/api.md) | Admin REST API |
| [Clustering](docs/clustering.md) | Multi-node setup |
| [WAF](docs/waf.md) | Web Application Firewall (6-layer defense) |
| [MCP / AI](docs/mcp.md) | AI integration |
| [Benchmarks](docs/benchmark-report.md) | Performance data |
| [Specification](docs/SPECIFICATION.md) | Technical spec |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Key rules:

1. **Zero external deps** — stdlib only
2. **Tests required** — 90% coverage, don't lower it
3. **All features wired** — no dead code in engine.go
4. **gofmt + go vet** — CI enforced

## License

Apache 2.0 — [LICENSE](LICENSE)
