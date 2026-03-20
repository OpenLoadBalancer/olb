# Load Balancing Algorithms

OLB includes 14 load balancing algorithms. This guide explains how each works and when to use it.

## Overview

| Algorithm | Config Name | Complexity | Session Affinity | Weight Support | Best For |
|-----------|-------------|------------|------------------|----------------|----------|
| Round Robin | `round_robin` | O(1) | No | No | General purpose, equal backends |
| Weighted Round Robin | `weighted_round_robin` | O(1) | No | Yes | Heterogeneous backend capacity |
| Least Connections | `least_connections` | O(n) | No | No | Varying request duration |
| Least Response Time | `least_response_time` | O(n) | No | No | Latency-sensitive workloads |
| IP Hash | `ip_hash` | O(1) | Yes (IP) | No | Simple session affinity |
| Consistent Hash | `consistent_hash` | O(log n) | Yes (key) | Yes | Caching layers, stateful services |
| Maglev | `maglev` | O(1) | Yes (key) | No | High-performance hashing |
| Power of Two | `power_of_two` | O(1) | No | No | Large backend pools |
| Random | `random` | O(1) | No | No | Simple, stateless |
| Weighted Random | `weighted_random` | O(n) | No | Yes | Probabilistic distribution |
| Ring Hash | `ring_hash` | O(log n) | Yes (key) | Yes | Cache-friendly distribution |
| Sticky Session | `sticky_session` | O(1) | Yes (cookie) | No | Stateful web applications |

---

## Round Robin

Distributes requests sequentially across all healthy backends in a fixed cycle.

```
Request 1 -> Backend A
Request 2 -> Backend B
Request 3 -> Backend C
Request 4 -> Backend A  (cycles back)
```

**When to use:** Default choice when all backends have equal capacity and requests have similar processing cost.

**Configuration:**

```yaml
pools:
  - name: web
    algorithm: round_robin
    backends:
      - id: web-1
        address: "10.0.1.1:8080"
      - id: web-2
        address: "10.0.1.2:8080"
      - id: web-3
        address: "10.0.1.3:8080"
```

---

## Weighted Round Robin

Distributes requests proportional to backend weights using smooth weighted round-robin (Nginx-style) to avoid burst patterns.

```
Weights: A=5, B=3, C=2
Traffic: A, A, B, A, C, A, B, A, B, C  (smooth, no bursts)
```

**When to use:** Backends have different capacities (e.g., a mix of large and small instances).

**Configuration:**

```yaml
pools:
  - name: web
    algorithm: weighted_round_robin
    backends:
      - id: large-1
        address: "10.0.1.1:8080"
        weight: 5                  # Gets 50% of traffic
      - id: medium-1
        address: "10.0.1.2:8080"
        weight: 3                  # Gets 30% of traffic
      - id: small-1
        address: "10.0.1.3:8080"
        weight: 2                  # Gets 20% of traffic
```

---

## Least Connections

Routes each request to the backend with the fewest active connections.

**When to use:** Requests have widely varying processing times (e.g., some requests take milliseconds, others take seconds). Naturally adapts to slower backends by sending fewer requests to them.

**Configuration:**

```yaml
pools:
  - name: api
    algorithm: least_connections
    backends:
      - id: api-1
        address: "10.0.2.1:8080"
      - id: api-2
        address: "10.0.2.2:8080"
```

---

## Least Response Time

Routes to the backend with the lowest average response time over a sliding window. Combines latency measurement with active connection count.

**When to use:** Latency-sensitive workloads where you want to favor the fastest backend. Adapts to transient performance differences between backends.

**Configuration:**

```yaml
pools:
  - name: api
    algorithm: least_response_time
    backends:
      - id: api-1
        address: "10.0.2.1:8080"
      - id: api-2
        address: "10.0.2.2:8080"
```

---

## IP Hash

Deterministically maps a client IP to a backend using `hash(client_ip) % len(backends)`. The same client always reaches the same backend (as long as the pool is stable).

**When to use:** Simple session affinity without cookies. Note that adding or removing backends causes most clients to be remapped.

**Configuration:**

```yaml
pools:
  - name: app
    algorithm: ip_hash
    backends:
      - id: app-1
        address: "10.0.3.1:8080"
      - id: app-2
        address: "10.0.3.2:8080"
      - id: app-3
        address: "10.0.3.3:8080"
```

---

## Consistent Hash

Uses a hash ring with virtual nodes (Ketama algorithm). When backends are added or removed, only ~K/n keys are redistributed (where K = total keys, n = number of backends).

Each backend is placed at 150 virtual node positions on the ring (configurable). Requests are hashed and routed to the nearest backend on the ring.

**When to use:** Caching layers (maximizes cache hit rate), stateful services, or any scenario where backend changes should cause minimal disruption.

**Configuration:**

```yaml
pools:
  - name: cache
    algorithm: consistent_hash
    backends:
      - id: cache-1
        address: "10.0.4.1:6379"
        weight: 1                  # Controls number of virtual nodes
      - id: cache-2
        address: "10.0.4.2:6379"
        weight: 1
      - id: cache-3
        address: "10.0.4.3:6379"
        weight: 1
```

---

## Maglev

Google's Maglev hashing algorithm. Builds a lookup table of size M (a prime number, default 65537) for O(1) lookups after initialization. Provides more uniform distribution than consistent hashing and minimal disruption when backends change.

**When to use:** High-performance environments requiring consistent hashing with better load distribution. Ideal for large-scale deployments.

**Configuration:**

```yaml
pools:
  - name: service
    algorithm: maglev
    backends:
      - id: svc-1
        address: "10.0.5.1:8080"
      - id: svc-2
        address: "10.0.5.2:8080"
      - id: svc-3
        address: "10.0.5.3:8080"
```

---

## Power of Two Random Choices (P2C)

Picks two random backends and sends the request to the one with fewer active connections. Achieves near-optimal load distribution with O(1) selection -- significantly better than pure random, nearly as good as least-connections.

**When to use:** Large backend pools where O(n) least-connections is too expensive, or when you want a good balance between simplicity and optimal distribution.

**Configuration:**

```yaml
pools:
  - name: large-pool
    algorithm: power_of_two
    backends:
      - id: node-1
        address: "10.0.6.1:8080"
      - id: node-2
        address: "10.0.6.2:8080"
      # ... many backends
      - id: node-50
        address: "10.0.6.50:8080"
```

---

## Random

Selects a backend uniformly at random. Simple and stateless.

**When to use:** When simplicity is the priority and rough distribution is acceptable. No coordination or state needed.

**Configuration:**

```yaml
pools:
  - name: stateless
    algorithm: random
    backends:
      - id: worker-1
        address: "10.0.7.1:8080"
      - id: worker-2
        address: "10.0.7.2:8080"
```

---

## Weighted Random

Selects a backend at random, with probability proportional to weights.

**When to use:** Probabilistic traffic splitting (e.g., canary deployments) or backends with different capacities where deterministic ordering is not needed.

**Configuration:**

```yaml
pools:
  - name: canary
    algorithm: weighted_random
    backends:
      - id: stable
        address: "10.0.8.1:8080"
        weight: 90                 # ~90% of traffic
      - id: canary
        address: "10.0.8.2:8080"
        weight: 10                 # ~10% of traffic
```

---

## Ring Hash

Hash ring with configurable virtual nodes per backend. Similar to consistent hash but with a different ring construction that allows fine-tuned virtual node counts per backend.

**When to use:** When you need consistent hashing with per-backend control over the number of virtual nodes, or when working with systems that expect ring-hash semantics (e.g., Envoy-compatible setups).

**Configuration:**

```yaml
pools:
  - name: distributed-cache
    algorithm: ring_hash
    backends:
      - id: cache-1
        address: "10.0.9.1:6379"
        weight: 3                  # 3x virtual nodes
      - id: cache-2
        address: "10.0.9.2:6379"
        weight: 1
```

---

## Sticky Session

Cookie-based session affinity. On the first request, a backend is selected (via round-robin) and a cookie is set. Subsequent requests with that cookie go to the same backend.

**When to use:** Stateful web applications that store session data in-process (not in a shared store). Works with any HTTP client that supports cookies.

**Configuration:**

```yaml
pools:
  - name: webapp
    algorithm: round_robin
    sticky:
      type: cookie
      name: OLB_BACKEND            # Cookie name
      ttl: 1h                      # Cookie/session TTL
      http_only: true
      secure: true

    backends:
      - id: app-1
        address: "10.0.10.1:8080"
      - id: app-2
        address: "10.0.10.2:8080"
```

Alternative affinity modes:

```yaml
    # Header-based affinity
    sticky:
      type: header
      name: X-Session-ID

    # URL parameter-based affinity
    sticky:
      type: param
      name: session_id
```

---

## Choosing an Algorithm

Use this decision tree:

1. **All backends equal capacity, simple workload?** Use `round_robin`.
2. **Backends with different capacities?** Use `weighted_round_robin`.
3. **Requests with varying processing time?** Use `least_connections`.
4. **Need lowest latency?** Use `least_response_time`.
5. **Need session affinity with cookies?** Use `sticky_session` (or any algorithm + `sticky` config).
6. **Need session affinity without cookies?** Use `ip_hash`.
7. **Caching layer or stateful sharding?** Use `consistent_hash` or `maglev`.
8. **Very large backend pool (50+ nodes)?** Use `power_of_two`.
9. **Traffic splitting for canary?** Use `weighted_random`.
