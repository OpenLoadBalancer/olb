# Architecture Decision Records

Key design decisions that shaped OpenLoadBalancer.

## ADR-001: Minimal External Dependencies

**Status**: Accepted

**Context**: Go has a rich ecosystem of third-party libraries. Using them speeds up development but introduces supply-chain risk, version conflicts, and bloat.

**Decision**: Use only Go standard library plus `golang.org/x/crypto` (bcrypt, OCSP), `golang.org/x/net` (HTTP/2), and `golang.org/x/text` (text processing). No other external dependencies.

**Consequences**:
- Single statically-linked binary with no CGO dependencies
- Smaller attack surface — only 3 vetted Go team libraries
- No dependency version conflicts
- More implementation code to maintain (e.g., custom load balancer algorithms, radix trie router)
- Faster builds and reproducible binaries

## ADR-002: Raft + SWIM for Clustering

**Status**: Accepted

**Context**: Clustering requires both strong consistency for configuration state and efficient failure detection for node membership.

**Decision**: Use Raft consensus for configuration replication (strong consistency, linearizable reads) and SWIM gossip protocol for node membership and failure detection (eventual consistency, low overhead).

**Consequences**:
- Configuration changes are strongly consistent — all nodes see the same config
- Node membership is eventually consistent but converges fast (gossip period ~1s)
- Failure detection tolerates network partitions (SWIM uses indirect probing)
- Raft requires a leader — write unavailable during leader election (typically <1s)
- More complex than a simple primary-replica setup, but correct

## ADR-003: Radix Trie Router

**Status**: Accepted

**Context**: HTTP routing needs O(k) path matching where k is path length, not the number of routes. Regex-based routers are flexible but slow for large route tables.

**Decision**: Use a compressed radix trie (Patricia trie) for HTTP path matching. Routes are stored as a tree where common prefixes are shared.

**Consequences**:
- O(k) lookup time regardless of route count (k = path length)
- Path parameters (`/api/:id`) handled natively by the trie structure
- Wildcard routes (`/static/*filepath`) are first-class
- Static memory layout — good cache locality
- More complex to implement than a simple map, but the implementation is under 500 LOC

## ADR-004: Vanilla JS/CSS for WebUI

**Status**: Accepted

**Context**: The admin dashboard needs to be embedded in the Go binary. React/Vue/Svelte bundles add complexity to the build pipeline.

**Decision**: Use vanilla JavaScript and CSS with no framework. The WebUI is embedded via `embed.FS` at compile time.

**Consequences**:
- Zero build tooling — no node_modules, no webpack, no transpilation
- Embedded in the binary — no separate static file server needed
- Smaller binary contribution (~200KB vs ~2MB for React)
- More verbose DOM manipulation code
- No hot module replacement during development

## ADR-005: Config-Gated Middleware

**Status**: Accepted

**Context**: A load balancer needs to support many middleware components, but not all deployments need all of them. Unused middleware should not add latency.

**Decision**: Each middleware has an `enabled: true/false` flag in the config. The engine only creates and wires enabled middleware into the chain.

**Consequences**:
- Zero overhead for disabled middleware — it's never instantiated
- Each middleware can be toggled independently at runtime via config reload
- Config validation catches invalid combinations early
- Adds boilerplate per-middleware registration code (mitigated by `middleware_registration.go`)

## ADR-006: Sharded Atomic Counters

**Status**: Accepted

**Context**: Under high concurrency (100K+ goroutines), a single `atomic.Int64` counter becomes a cache-line contention hotspot. All CPUs serialize on the same memory location.

**Decision**: Use sharded counters with one shard per CPU core. Each increment targets a different shard based on goroutine ID. Reads sum all shards.

**Consequences**:
- ~15ns/op under parallel load on 16-core machines (vs 50+ ns with single atomic)
- Reads are slower (must sum all shards) but reads are infrequent (metrics scrape every 10s)
- Slightly more memory per counter (numCPU × 8 bytes)
- Same API surface — drop-in replacement for the previous single-atomic counter

## ADR-007: Protocol Detection in L7 Proxy

**Status**: Accepted

**Context**: A single HTTP listener may receive regular HTTP, WebSocket, gRPC, gRPC-Web, or SSE requests. Each needs different proxy handling.

**Decision**: Detect protocol via Content-Type and Upgrade headers before routing. The proxy handler switches to the appropriate protocol handler (WebSocket, gRPC, SSE) based on the detected type.

**Consequences**:
- Single port handles all L7 protocols — simpler deployment
- Detection is cheap (header inspection, no body parsing)
- Each protocol handler is independent and testable in isolation
- gRPC-Web requests are detected before gRPC requests (more specific prefix match first)

## ADR-008: Connection Pooling with Eviction Tracking

**Status**: Accepted

**Context**: Creating new TCP connections for every request is expensive. Connection reuse is critical for high-RPS workloads.

**Decision**: Maintain a connection pool per backend with configurable max size, idle timeout, and max lifetime. Evict stale connections in a background goroutine. Track hits, misses, and evictions as Prometheus metrics.

**Consequences**:
- Connection reuse reduces latency by ~1ms per request (no TCP handshake)
- Pool metrics enable capacity planning (hit ratio, eviction rate)
- Eviction goroutine prevents resource leaks
- Pool is optional — the transport falls back to direct dial if no pool manager is configured
