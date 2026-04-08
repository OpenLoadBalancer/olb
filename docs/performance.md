# Performance Tuning Guide

This guide covers tuning OpenLoadBalancer for different workload types.

## Configuration Overview

All tuning parameters live under the `server` section of `olb.yaml`:

```yaml
server:
  max_connections: 10000
  max_connections_per_source: 100
  max_connections_per_backend: 1000
  proxy_timeout: "60s"
  dial_timeout: "10s"
  drain_timeout: "30s"
  max_idle_conns: 100
  max_idle_conns_per_host: 10
  idle_conn_timeout: "90s"
```

## Workload Profiles

### High RPS (10K-100K requests/second)

Optimize for maximum throughput with small, fast requests.

```yaml
server:
  max_connections: 50000
  max_connections_per_source: 500
  max_connections_per_backend: 5000
  proxy_timeout: "10s"
  dial_timeout: "5s"
  max_idle_conns: 500
  max_idle_conns_per_host: 50
  idle_conn_timeout: "120s"

listeners:
  - name: http
    address: ":8080"
    protocol: http
    routes:
      - path: /
        pool: fast-pool

pools:
  - name: fast-pool
    algorithm: round_robin     # Lowest overhead algorithm
    backends:
      - address: "10.0.0.1:8080"
      - address: "10.0.0.2:8080"
    health_check:
      interval: 30s            # Less frequent checks reduce overhead

middleware:
  rate_limit:
    enabled: true
    requests_per_second: 10000
  cors:
    enabled: false             # Disable unused middleware layers
  compression:
    enabled: false             # Compression adds latency per-request
```

**Key tuning points:**
- Use `round_robin` or `least_connections` — lowest algorithm overhead (~3.5 ns/op)
- Increase `max_idle_conns` and `max_idle_conns_per_host` to reuse connections
- Disable unused middleware — each enabled layer adds measurable overhead
- Disable compression for already-compressed payloads (APIs, gRPC)
- Set aggressive `dial_timeout` — fast fail for unreachable backends

### High Concurrency (10K-50K simultaneous connections)

Optimize for many long-lived connections (WebSocket, SSE, streaming).

```yaml
server:
  max_connections: 50000
  max_connections_per_source: 1000
  max_connections_per_backend: 10000
  proxy_timeout: "300s"        # Long timeout for streaming
  dial_timeout: "10s"
  drain_timeout: "120s"        # Allow time for connections to close
  max_idle_conns: 200
  max_idle_conns_per_host: 20

pools:
  - name: ws-pool
    algorithm: least_connections  # Distribute evenly for long-lived conns
    backends:
      - address: "10.0.0.1:8080"
        weight: 100
      - address: "10.0.0.2:8080"
        weight: 100
    health_check:
      interval: 10s
      timeout: 5s
```

**Key tuning points:**
- Use `least_connections` — prevents any single backend from being overloaded
- Increase `max_connections_per_backend` significantly
- Set long `proxy_timeout` and `drain_timeout` for graceful handling
- Use `weighted_least_connections` if backends have different capacities

### High Bandwidth (large request/response bodies)

Optimize for large file transfers, media streaming, or bulk data pipelines.

```yaml
server:
  max_connections: 5000        # Fewer connections, but each carries more data
  proxy_timeout: "600s"        # 10 minutes for large transfers
  dial_timeout: "10s"
  max_idle_conns: 50
  max_idle_conns_per_host: 10
  idle_conn_timeout: "300s"

middleware:
  compression:
    enabled: true              # Compress to save bandwidth
    level: 3                   # Balance between speed and ratio
    min_size: 1024             # Only compress responses > 1KB
  cache:
    enabled: true              # Cache static assets
    ttl: "5m"
    max_size: "1GB"
```

**Key tuning points:**
- Enable compression — significant bandwidth savings for text-based payloads
- Use moderate compression level (3-4) for good CPU/bandwidth trade-off
- Set long `proxy_timeout` for large transfers
- Enable caching for repeated static content

## Balancer Algorithm Selection

| Workload | Recommended Algorithm | Why |
|----------|----------------------|-----|
| Stateless API | `round_robin` | Lowest overhead, even distribution |
| Varied request cost | `least_connections` | Balances actual load |
| Latency-sensitive | `peak_ewma` | Routes to fastest backend |
| Session-affinity needed | `sticky` | Ensures session consistency |
| Cache-friendly | `consistent_hash` | Minimizes cache invalidation |
| Heterogeneous backends | `weighted_round_robin` | Respects backend capacity |

## Connection Pool Tuning

The connection pool (`internal/conn`) manages reusable backend connections:

```yaml
# These are set at the server level
server:
  max_idle_conns: 100          # Total idle connections across all backends
  max_idle_conns_per_host: 10  # Idle connections per backend
  idle_conn_timeout: "90s"     # How long idle connections are kept
```

**Pool metrics** (available at `/metrics`):
- `olb_pool_idle_connections` — reusable connections waiting
- `olb_pool_active_connections` — connections in use
- `olb_pool_hits_total` — connections reused (higher is better)
- `olb_pool_misses_total` — new connections created
- `olb_pool_evictions_total` — connections evicted due to timeout

**Tuning tips:**
- If `misses` > `hits`, increase `max_idle_conns_per_host`
- If `evictions` is high but backends are active, increase `idle_conn_timeout`
- Monitor pool utilization via the Grafana dashboard

## WAF Performance

The 6-layer WAF adds ~35μs per request (~3% overhead at proxy scale):

```yaml
waf:
  enabled: true
  mode: monitor              # Monitor-only adds less overhead than enforce
  detection:
    threshold:
      block: 50
      log: 25
    detectors:
      sqli: {enabled: true}
      xss: {enabled: true}
```

**Tuning for performance:**
- Use `mode: monitor` during rollout to measure impact without blocking
- Disable unused detectors
- Set appropriate thresholds to reduce false positives
- Use IP ACL whitelist for trusted internal traffic to bypass detection

## Monitoring Performance

### Key Metrics

| Metric | What to Watch |
|--------|--------------|
| `olb_backends_healthy` | Should match `olb_backends_total` |
| `olb_pool_hits_total` / `olb_pool_misses_total` | Hit ratio should be >80% |
| Request duration P99 | Should be <2x backend response time |
| Error rate | Should be <0.1% |

### Profiling

OLB exposes Go runtime profiling at the configured pprof address:

```yaml
profiling:
  enabled: true
  pprof_addr: ":6060"
```

```bash
# CPU profile for 30 seconds
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Memory allocations
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine dump (check for leaks)
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

### Benchmarking

```bash
# Run built-in benchmarks
make bench

# Profiled benchmarks with CPU/memory output
make bench-profile

# Compare between two versions
make bench-compare BASE=v0.9.0
```

## OS-Level Tuning (Linux)

For production Linux deployments:

```bash
# Increase file descriptor limits
ulimit -n 65535

# TCP tuning for high connection counts
sysctl -w net.core.somaxconn=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535
sysctl -w net.ipv4.tcp_tw_reuse=1
sysctl -w net.ipv4.ip_local_port_range="1024 65535"

# For high bandwidth
sysctl -w net.core.rmem_max=16777216
sysctl -w net.core.wmem_max=16777216
```
