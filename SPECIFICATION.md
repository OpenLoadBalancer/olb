# OpenLoadBalancer — SPECIFICATION v1.0

> **Project**: OpenLoadBalancer (OLB)
> **Repository**: github.com/openloadbalancer/olb
> **Language**: Go 1.23+ (zero external dependencies — stdlib only)
> **License**: Apache 2.0
> **Author**: Ersin Koç / ECOSTACK TECHNOLOGY OÜ
> **Date**: 2026-03-13

---

## Table of Contents

1. [Vision & Philosophy](#1-vision--philosophy)
2. [Architecture Overview](#2-architecture-overview)
3. [Project Structure](#3-project-structure)
4. [Core Engine](#4-core-engine)
5. [L7 HTTP/HTTPS Proxy](#5-l7-httphttps-proxy)
6. [L4 TCP/UDP Proxy](#6-l4-tcpudp-proxy)
7. [TLS Engine](#7-tls-engine)
8. [Load Balancing Algorithms](#8-load-balancing-algorithms)
9. [Health Checking](#9-health-checking)
10. [Middleware Pipeline](#10-middleware-pipeline)
11. [Configuration System](#11-configuration-system)
12. [Service Discovery](#12-service-discovery)
13. [Observability & Metrics](#13-observability--metrics)
14. [Web UI Dashboard](#14-web-ui-dashboard)
15. [CLI Interface](#15-cli-interface)
16. [Multi-Node Clustering](#16-multi-node-clustering)
17. [MCP Server (AI Integration)](#17-mcp-server-ai-integration)
18. [Plugin System](#18-plugin-system)
19. [Security](#19-security)
20. [Performance Targets](#20-performance-targets)
21. [Testing Strategy](#21-testing-strategy)
22. [Release Phases](#22-release-phases)
23. [API Reference](#23-api-reference)
24. [Configuration Reference](#24-configuration-reference)

---

## 1. Vision & Philosophy

### 1.1 What is OpenLoadBalancer?

OpenLoadBalancer (OLB) is a **zero-dependency**, **production-grade** load balancer and reverse proxy written entirely in Go using only the standard library. It operates at both Layer 4 (TCP/UDP) and Layer 7 (HTTP/HTTPS/gRPC/WebSocket), with built-in clustering, observability, a real-time Web UI dashboard, a powerful CLI, and first-class AI integration via MCP (Model Context Protocol).

### 1.2 Design Principles

1. **Zero External Dependencies**: Only Go stdlib. No third-party packages. Every component is built from scratch — HTTP parsing, config parsing (YAML/TOML/HCL), Raft consensus, metrics engine, Web UI, everything.
2. **Single Binary**: One `olb` binary that contains the proxy, CLI, Web UI (embedded via `embed`), admin API, and cluster agent. No sidecars, no companion processes.
3. **Convention Over Configuration**: Sane defaults that work out of the box. A minimal 5-line config should get you running.
4. **Hot Reload Everything**: Config changes, TLS certificates, upstream additions/removals, middleware — all hot-reloadable with zero downtime. SIGHUP or API call triggers reload.
5. **Observable by Default**: Built-in metrics, access logs, error tracking, and real-time dashboard. No external Prometheus/Grafana needed (but export is supported).
6. **Security First**: mTLS between nodes, automatic ACME/Let's Encrypt, rate limiting, IP filtering, WAF basics — all built-in.
7. **AI-Native**: MCP server built-in, allowing AI agents to query metrics, modify routing, scale backends, and diagnose issues through natural language.
8. **LLM-Friendly Codebase**: Predictable naming, comprehensive JSDoc-style GoDoc, `llms.txt` at root, example-driven API documentation.

### 1.3 Positioning

| Feature | Nginx | HAProxy | Caddy | Traefik | **OLB** |
|---|---|---|---|---|---|
| Zero deps | ✗ | ✗ | ✗ | ✗ | ✓ |
| Single binary | ~ | ~ | ✓ | ✓ | ✓ |
| L4 + L7 | ✓ | ✓ | ✓ | ✓ | ✓ |
| Built-in Web UI | ✗ | ~ | ✗ | ✓ | ✓ |
| Built-in clustering | ✗ | ✗ | ✗ | ~ | ✓ |
| Auto HTTPS | ✗ | ✗ | ✓ | ✓ | ✓ |
| MCP/AI support | ✗ | ✗ | ✗ | ✗ | ✓ |
| YAML+TOML+HCL | ✗ | ✗ | ✗ | ~ | ✓ |
| Hot reload | ~ | ✓ | ✓ | ✓ | ✓ |
| Go stdlib only | - | - | ✗ | ✗ | ✓ |

### 1.4 Target Users

- DevOps engineers needing a lightweight, self-contained LB
- Developers wanting a dev-friendly proxy with great DX
- Teams wanting built-in observability without a metrics stack
- AI/MCP enthusiasts wanting LB management through AI agents
- Anyone tired of configuring Nginx/HAProxy

---

## 2. Architecture Overview

### 2.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        OpenLoadBalancer                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │  L7 HTTP  │  │  L4 TCP  │  │  L4 UDP  │  │  Admin   │       │
│  │ Listener  │  │ Listener │  │ Listener │  │   API    │       │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘       │
│       │              │              │              │             │
│  ┌────▼──────────────▼──────────────▼──────────────▼─────┐      │
│  │              Connection Manager                        │      │
│  │   (accept, track, limit, timeout, drain)              │      │
│  └────────────────────┬──────────────────────────────────┘      │
│                       │                                         │
│  ┌────────────────────▼──────────────────────────────────┐      │
│  │              Middleware Pipeline                        │      │
│  │  ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐   │      │
│  │  │Rate│→│Auth│→│CORS│→│Comp│→│Log │→│Metr│→│Rety│   │      │
│  │  │Lim │ │    │ │    │ │ress│ │    │ │ics │ │    │   │      │
│  │  └────┘ └────┘ └────┘ └────┘ └────┘ └────┘ └────┘   │      │
│  └────────────────────┬──────────────────────────────────┘      │
│                       │                                         │
│  ┌────────────────────▼──────────────────────────────────┐      │
│  │              Router / Matcher                          │      │
│  │   (host, path, header, method, query, SNI)            │      │
│  └────────────────────┬──────────────────────────────────┘      │
│                       │                                         │
│  ┌────────────────────▼──────────────────────────────────┐      │
│  │              Load Balancer                              │      │
│  │  ┌─────┐ ┌──────┐ ┌───────┐ ┌──────┐ ┌───────────┐   │      │
│  │  │ RR  │ │ WRR  │ │ LConn │ │IPHash│ │ ConsHash  │   │      │
│  │  └─────┘ └──────┘ └───────┘ └──────┘ └───────────┘   │      │
│  └────────────────────┬──────────────────────────────────┘      │
│                       │                                         │
│  ┌────────────────────▼──────────────────────────────────┐      │
│  │              Backend Pool                               │      │
│  │   ┌─────────┐  ┌─────────┐  ┌─────────┐               │      │
│  │   │Backend 1│  │Backend 2│  │Backend 3│  ...           │      │
│  │   │ :8001   │  │ :8002   │  │ :8003   │               │      │
│  │   └─────────┘  └─────────┘  └─────────┘               │      │
│  └────────────────────────────────────────────────────────┘      │
│                                                                 │
│  ┌───────────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│  │ Health Checker │ │ Metrics  │ │ Cluster  │ │ MCP Srv  │      │
│  │ (active+pass.) │ │ Engine   │ │ (Raft)   │ │ (AI API) │      │
│  └───────────────┘ └──────────┘ └──────────┘ └──────────┘      │
│                                                                 │
│  ┌───────────────────────────────────────────────────────┐      │
│  │            Embedded Web UI (SPA)                       │      │
│  │   Real-time dashboard, config editor, log viewer       │      │
│  └───────────────────────────────────────────────────────┘      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Dependency Graph

```
config_parser ──→ core_engine ──→ listeners (L4/L7)
                      │
                      ├──→ connection_manager
                      ├──→ middleware_pipeline
                      ├──→ router
                      ├──→ balancer
                      ├──→ backend_pool
                      ├──→ health_checker
                      ├──→ tls_engine
                      ├──→ metrics_engine
                      ├──→ admin_api ──→ web_ui
                      ├──→ cli
                      ├──→ cluster_manager (Raft + Gossip)
                      └──→ mcp_server
```

### 2.3 Concurrency Model

- **Per-listener goroutine**: Each listener (HTTP :80, HTTPS :443, TCP :3306, etc.) runs in its own goroutine
- **Per-connection goroutine**: Each accepted connection spawns a handler goroutine (bounded by connection limits)
- **Connection pool per backend**: Pre-established, reusable connections to backends
- **Lock-free where possible**: Atomic operations for counters, lock-free ring buffers for metrics
- **Context propagation**: Every request carries a `context.Context` with deadline, trace ID, and cancellation

---

## 3. Project Structure

```
openloadbalancer/
├── cmd/
│   └── olb/
│       └── main.go                    # Entry point
│
├── internal/
│   ├── engine/
│   │   ├── engine.go                  # Core engine orchestrator
│   │   ├── lifecycle.go               # Start, stop, reload, drain
│   │   └── signals.go                 # OS signal handling
│   │
│   ├── listener/
│   │   ├── listener.go                # Listener interface
│   │   ├── tcp.go                     # TCP listener
│   │   ├── udp.go                     # UDP listener
│   │   ├── http.go                    # HTTP listener (L7)
│   │   ├── https.go                   # HTTPS/TLS listener
│   │   └── quic.go                    # QUIC/HTTP3 (future)
│   │
│   ├── conn/
│   │   ├── manager.go                 # Connection manager
│   │   ├── tracker.go                 # Active connection tracking
│   │   ├── pool.go                    # Backend connection pooling
│   │   ├── limiter.go                 # Connection limits per source
│   │   └── drain.go                   # Graceful connection draining
│   │
│   ├── proxy/
│   │   ├── l7/
│   │   │   ├── proxy.go               # HTTP reverse proxy
│   │   │   ├── request.go             # Request manipulation
│   │   │   ├── response.go            # Response manipulation
│   │   │   ├── websocket.go           # WebSocket proxying
│   │   │   ├── grpc.go                # gRPC proxying
│   │   │   ├── sse.go                 # Server-Sent Events
│   │   │   ├── http2.go               # HTTP/2 handling
│   │   │   └── buffering.go           # Request/response buffering
│   │   │
│   │   └── l4/
│   │       ├── proxy.go               # TCP proxy (bidirectional copy)
│   │       ├── udp.go                 # UDP proxy
│   │       ├── splice.go              # Zero-copy splice (Linux)
│   │       └── sni.go                 # SNI-based routing for TLS passthrough
│   │
│   ├── router/
│   │   ├── router.go                  # Route matching engine
│   │   ├── host.go                    # Host-based routing
│   │   ├── path.go                    # Path-based routing (prefix, exact, regex)
│   │   ├── header.go                  # Header-based routing
│   │   ├── method.go                  # HTTP method routing
│   │   ├── query.go                   # Query parameter routing
│   │   ├── sni.go                     # SNI-based routing
│   │   ├── weighted.go                # Weighted routing (canary/blue-green)
│   │   └── trie.go                    # Radix trie for fast path matching
│   │
│   ├── balancer/
│   │   ├── balancer.go                # Balancer interface + registry
│   │   ├── roundrobin.go              # Round Robin
│   │   ├── weighted_roundrobin.go     # Weighted Round Robin (smooth)
│   │   ├── least_conn.go              # Least Connections
│   │   ├── least_time.go              # Least Response Time
│   │   ├── ip_hash.go                 # IP Hash (sticky sessions)
│   │   ├── consistent_hash.go         # Consistent Hashing (Ketama)
│   │   ├── random.go                  # Random
│   │   ├── weighted_random.go         # Weighted Random
│   │   ├── power_of_two.go            # Power of Two Random Choices (P2C)
│   │   ├── maglev.go                  # Maglev Hashing (Google)
│   │   └── ring_hash.go              # Ring Hash with virtual nodes
│   │
│   ├── backend/
│   │   ├── pool.go                    # Backend pool management
│   │   ├── backend.go                 # Single backend definition
│   │   ├── state.go                   # Backend state machine (up/down/draining/maintenance)
│   │   ├── stats.go                   # Per-backend statistics
│   │   └── dynamic.go                 # Dynamic backend add/remove
│   │
│   ├── health/
│   │   ├── checker.go                 # Health check orchestrator
│   │   ├── http_check.go              # HTTP/HTTPS health check
│   │   ├── tcp_check.go               # TCP connection health check
│   │   ├── grpc_check.go              # gRPC health check (protocol)
│   │   ├── exec_check.go              # External command health check
│   │   ├── passive.go                 # Passive health check (error rate)
│   │   ├── composite.go               # Combined active + passive
│   │   └── state.go                   # Health state transitions
│   │
│   ├── middleware/
│   │   ├── chain.go                   # Middleware chain builder
│   │   ├── ratelimit/
│   │   │   ├── ratelimit.go           # Rate limiter middleware
│   │   │   ├── token_bucket.go        # Token bucket algorithm
│   │   │   ├── sliding_window.go      # Sliding window counter
│   │   │   ├── leaky_bucket.go        # Leaky bucket algorithm
│   │   │   └── distributed.go         # Cluster-wide rate limiting
│   │   │
│   │   ├── circuit/
│   │   │   ├── breaker.go             # Circuit breaker middleware
│   │   │   └── state.go               # Open/Half-Open/Closed states
│   │   │
│   │   ├── retry/
│   │   │   ├── retry.go               # Retry middleware
│   │   │   ├── backoff.go             # Exponential backoff with jitter
│   │   │   └── policy.go              # Retry policies (idempotent, etc.)
│   │   │
│   │   ├── timeout/
│   │   │   └── timeout.go             # Request/response timeout
│   │   │
│   │   ├── compress/
│   │   │   ├── compress.go            # Compression middleware
│   │   │   ├── gzip.go                # gzip compression
│   │   │   ├── deflate.go             # deflate compression
│   │   │   └── brotli.go              # brotli (pure Go impl)
│   │   │
│   │   ├── cors/
│   │   │   └── cors.go                # CORS handling
│   │   │
│   │   ├── auth/
│   │   │   ├── basic.go               # Basic auth
│   │   │   ├── jwt.go                 # JWT validation (pure Go)
│   │   │   ├── apikey.go              # API key auth
│   │   │   └── oauth2.go              # OAuth2 token introspection
│   │   │
│   │   ├── headers/
│   │   │   ├── headers.go             # Header manipulation
│   │   │   ├── request_id.go          # X-Request-ID injection
│   │   │   ├── realip.go              # X-Real-IP / X-Forwarded-For
│   │   │   └── security.go            # Security headers (HSTS, CSP, etc.)
│   │   │
│   │   ├── rewrite/
│   │   │   ├── path.go                # URL path rewriting
│   │   │   └── redirect.go            # HTTP redirects (301/302/307/308)
│   │   │
│   │   ├── cache/
│   │   │   ├── cache.go               # HTTP response cache
│   │   │   ├── store.go               # In-memory LRU cache store
│   │   │   └── key.go                 # Cache key generation
│   │   │
│   │   ├── waf/
│   │   │   ├── waf.go                 # Basic WAF rules
│   │   │   ├── sqli.go                # SQL injection detection
│   │   │   ├── xss.go                 # XSS detection
│   │   │   └── scanner.go             # Request scanner
│   │   │
│   │   ├── ipfilter/
│   │   │   ├── filter.go              # IP allow/deny lists
│   │   │   ├── cidr.go                # CIDR range matching
│   │   │   └── geoip.go               # GeoIP (embedded DB, future)
│   │   │
│   │   ├── logging/
│   │   │   ├── access.go              # Access log middleware
│   │   │   └── format.go              # Log format (JSON, CLF, custom)
│   │   │
│   │   └── metrics/
│   │       └── middleware.go           # Per-request metrics collection
│   │
│   ├── tls/
│   │   ├── manager.go                 # TLS certificate manager
│   │   ├── config.go                  # TLS config builder
│   │   ├── acme/
│   │   │   ├── client.go              # ACME client (Let's Encrypt)
│   │   │   ├── challenge.go           # HTTP-01/TLS-ALPN-01 solvers
│   │   │   ├── account.go             # ACME account management
│   │   │   └── store.go               # Certificate storage
│   │   ├── sni.go                     # SNI multiplexer
│   │   ├── ocsp.go                    # OCSP stapling
│   │   └── mtls.go                    # Mutual TLS
│   │
│   ├── config/
│   │   ├── config.go                  # Config structs & defaults
│   │   ├── loader.go                  # Multi-format loader
│   │   ├── watcher.go                 # File watcher for hot reload
│   │   ├── validator.go               # Config validation
│   │   ├── merger.go                  # Config merge (file + env + flags)
│   │   ├── parser/
│   │   │   ├── yaml.go                # YAML parser (from scratch)
│   │   │   ├── toml.go                # TOML parser (from scratch)
│   │   │   ├── hcl.go                 # HCL parser (from scratch)
│   │   │   └── json.go                # JSON (stdlib encoding/json)
│   │   └── env.go                     # Environment variable overlay
│   │
│   ├── discovery/
│   │   ├── discovery.go               # Service discovery interface
│   │   ├── static.go                  # Static config-based
│   │   ├── dns.go                     # DNS SRV record discovery
│   │   ├── docker.go                  # Docker API discovery
│   │   └── file.go                    # File-based discovery (watch)
│   │
│   ├── metrics/
│   │   ├── engine.go                  # Metrics collection engine
│   │   ├── counter.go                 # Atomic counter
│   │   ├── gauge.go                   # Atomic gauge
│   │   ├── histogram.go               # HDR Histogram (lock-free)
│   │   ├── timeseries.go              # Time-series ring buffer
│   │   ├── aggregator.go              # Metrics aggregation
│   │   ├── prometheus.go              # Prometheus exposition format
│   │   └── export.go                  # JSON/StatsD export
│   │
│   ├── logging/
│   │   ├── logger.go                  # Structured logger
│   │   ├── level.go                   # Log levels
│   │   ├── output.go                  # Multi-output (file, stdout, syslog)
│   │   ├── rotation.go                # Log rotation
│   │   └── fields.go                  # Structured fields
│   │
│   ├── admin/
│   │   ├── server.go                  # Admin API server
│   │   ├── router.go                  # Admin API routes
│   │   ├── handlers/
│   │   │   ├── config.go              # Config CRUD endpoints
│   │   │   ├── backends.go            # Backend management
│   │   │   ├── routes.go              # Route management
│   │   │   ├── health.go              # Health status endpoints
│   │   │   ├── metrics.go             # Metrics endpoints
│   │   │   ├── cluster.go             # Cluster management
│   │   │   ├── certs.go               # Certificate management
│   │   │   └── system.go              # System info (version, uptime)
│   │   ├── auth.go                    # Admin API authentication
│   │   └── websocket.go              # WebSocket for real-time updates
│   │
│   ├── webui/
│   │   ├── embed.go                   # go:embed for SPA assets
│   │   ├── handler.go                 # Static file serving
│   │   └── assets/                    # Built SPA (HTML/CSS/JS)
│   │       ├── index.html
│   │       ├── app.js                 # Vanilla JS SPA (no framework)
│   │       ├── style.css
│   │       ├── components/
│   │       │   ├── dashboard.js       # Main dashboard
│   │       │   ├── backends.js        # Backend management
│   │       │   ├── routes.js          # Route management
│   │       │   ├── metrics.js         # Metrics & charts
│   │       │   ├── logs.js            # Real-time log viewer
│   │       │   ├── config.js          # Config editor
│   │       │   ├── cluster.js         # Cluster view
│   │       │   └── certs.js           # Certificate management
│   │       └── lib/
│   │           ├── chart.js           # Minimal chart library (from scratch)
│   │           ├── ws.js              # WebSocket client
│   │           └── router.js          # Client-side router
│   │
│   ├── cli/
│   │   ├── cli.go                     # CLI entry point
│   │   ├── commands/
│   │   │   ├── start.go               # olb start
│   │   │   ├── stop.go                # olb stop
│   │   │   ├── reload.go              # olb reload
│   │   │   ├── status.go              # olb status
│   │   │   ├── config.go              # olb config validate/show/diff
│   │   │   ├── backend.go             # olb backend add/remove/list/drain
│   │   │   ├── route.go               # olb route add/remove/list
│   │   │   ├── health.go              # olb health show
│   │   │   ├── metrics.go             # olb metrics show/export
│   │   │   ├── cert.go                # olb cert add/remove/renew/list
│   │   │   ├── cluster.go             # olb cluster join/leave/status
│   │   │   ├── log.go                 # olb log tail/search
│   │   │   ├── top.go                 # olb top (htop-style live view)
│   │   │   ├── version.go             # olb version
│   │   │   └── completion.go          # Shell completions
│   │   ├── parser.go                  # Arg/flag parser (from scratch)
│   │   ├── output.go                  # Table/JSON/YAML output formatters
│   │   └── tui/
│   │       ├── tui.go                 # Terminal UI engine
│   │       ├── widgets.go             # TUI widgets (tables, charts, gauges)
│   │       └── colors.go              # ANSI color support
│   │
│   ├── cluster/
│   │   ├── manager.go                 # Cluster manager
│   │   ├── raft/
│   │   │   ├── raft.go                # Raft consensus (from scratch)
│   │   │   ├── log.go                 # Raft log
│   │   │   ├── state.go               # Follower/Candidate/Leader
│   │   │   ├── rpc.go                 # Raft RPC (AppendEntries, RequestVote)
│   │   │   ├── snapshot.go            # Raft snapshots
│   │   │   └── transport.go           # TCP transport for Raft
│   │   ├── gossip/
│   │   │   ├── gossip.go              # Gossip protocol (SWIM)
│   │   │   ├── member.go              # Membership management
│   │   │   ├── broadcast.go           # Message broadcasting
│   │   │   └── suspect.go             # Failure detection
│   │   ├── state/
│   │   │   ├── store.go               # Distributed state store
│   │   │   ├── sync.go                # State synchronization
│   │   │   └── conflict.go            # Conflict resolution (CRDT-inspired)
│   │   └── election.go                # Leader election
│   │
│   ├── mcp/
│   │   ├── server.go                  # MCP server implementation
│   │   ├── tools.go                   # MCP tools (query, modify, diagnose)
│   │   ├── resources.go               # MCP resources (metrics, config, logs)
│   │   ├── prompts.go                 # MCP prompt templates
│   │   └── transport.go               # MCP transport (stdio, HTTP SSE)
│   │
│   └── plugin/
│       ├── plugin.go                  # Plugin interface
│       ├── registry.go                # Plugin registry
│       ├── loader.go                  # Plugin loader (Go plugin or WASM)
│       └── api.go                     # Plugin API surface
│
├── pkg/
│   ├── types/
│   │   └── types.go                   # Shared types
│   ├── errors/
│   │   └── errors.go                  # Error types with codes
│   ├── version/
│   │   └── version.go                 # Version info (build-time)
│   └── utils/
│       ├── atomic.go                  # Atomic helpers
│       ├── buffer.go                  # Buffer pool
│       ├── ring.go                    # Lock-free ring buffer
│       ├── lru.go                     # LRU cache
│       ├── bloom.go                   # Bloom filter
│       ├── ip.go                      # IP/CIDR utils
│       ├── time.go                    # Time/duration helpers
│       └── rand.go                    # Fast random (splitmix64)
│
├── configs/
│   ├── olb.yaml                       # Example YAML config
│   ├── olb.toml                       # Example TOML config
│   ├── olb.hcl                        # Example HCL config
│   └── olb.minimal.yaml               # Minimal config example
│
├── web/                               # Web UI source (vanilla JS)
│   ├── index.html
│   ├── src/
│   └── build.go                       # Build script to embed
│
├── docs/
│   ├── getting-started.md
│   ├── configuration.md
│   ├── algorithms.md
│   ├── clustering.md
│   ├── mcp.md
│   └── api.md
│
├── test/
│   ├── integration/
│   ├── e2e/
│   ├── benchmark/
│   └── fixtures/
│
├── scripts/
│   ├── build.sh
│   ├── release.sh
│   └── install.sh
│
├── llms.txt                           # LLM-friendly project description
├── go.mod                             # Zero dependencies
├── go.sum                             # Empty (no deps)
├── Makefile
├── Dockerfile
├── SPECIFICATION.md
├── IMPLEMENTATION.md
├── TASKS.md
├── CHANGELOG.md
├── LICENSE
└── README.md
```

---

## 4. Core Engine

### 4.1 Engine Orchestrator

The engine is the central coordinator. It owns the lifecycle of all components.

```go
// internal/engine/engine.go

type Engine struct {
    config       *config.Config
    listeners    []listener.Listener
    connManager  *conn.Manager
    router       *router.Router
    middleware   *middleware.Chain
    backends     *backend.PoolManager
    healthCheck  *health.Checker
    tlsManager   *tls.Manager
    metrics      *metrics.Engine
    adminServer  *admin.Server
    cluster      *cluster.Manager     // nil if standalone
    mcpServer    *mcp.Server          // nil if disabled
    logger       *logging.Logger
    
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
    
    state        atomic.Int32         // 0=stopped, 1=starting, 2=running, 3=draining, 4=stopping
    startTime    time.Time
    reloadCount  atomic.Int64
}

type EngineState int32

const (
    StateStopped  EngineState = 0
    StateStarting EngineState = 1
    StateRunning  EngineState = 2
    StateDraining EngineState = 3
    StateStopping EngineState = 4
)
```

### 4.2 Lifecycle

```
[Init] → [Load Config] → [Validate] → [Start Components] → [Running]
                                                                 │
                              [Hot Reload] ←── SIGHUP ───────────┤
                                                                 │
                              [Drain] ←── SIGTERM ───────────────┤
                                   │                             │
                                   └──→ [Stop] → [Cleanup] → [Exit]
```

**Startup sequence** (order matters):
1. Parse CLI flags
2. Load and validate config (YAML/TOML/HCL + env overlay)
3. Initialize logger
4. Initialize metrics engine
5. Initialize TLS manager (load certs, start ACME if needed)
6. Initialize backend pools with health checkers
7. Build middleware chains per route
8. Build router with all route definitions
9. Initialize connection manager
10. Start listeners (HTTP, HTTPS, TCP, UDP)
11. Start admin API server
12. Start Web UI (embedded)
13. Start cluster agent (if multi-node)
14. Start MCP server (if enabled)
15. Install signal handlers
16. Log "OpenLoadBalancer vX.Y.Z ready" with listener addresses

**Graceful shutdown**:
1. Stop accepting new connections
2. Set state to `Draining`
3. Wait for in-flight requests (with configurable timeout, default 30s)
4. Close all backend connections
5. Stop health checkers
6. Stop cluster agent
7. Flush metrics
8. Flush logs
9. Exit

### 4.3 Hot Reload

Triggered by: SIGHUP signal, Admin API `POST /api/v1/reload`, CLI `olb reload`

**What can be hot-reloaded**:
- Routes (add/remove/modify)
- Backends (add/remove/modify weights)
- TLS certificates
- Middleware configuration
- Rate limit settings
- Health check parameters
- Logging configuration
- Access control lists

**What requires restart**:
- Listener addresses/ports
- Cluster configuration
- Admin API bind address
- Core engine settings (worker count, buffer sizes)

**Reload algorithm**:
1. Load new config
2. Validate new config
3. Compute diff against current config
4. Apply changes atomically (swap pointers, no locks on hot path)
5. Log all changes
6. Increment reload counter
7. Emit reload event to WebSocket subscribers

### 4.4 Signal Handling

| Signal | Action |
|--------|--------|
| `SIGHUP` | Hot reload configuration |
| `SIGTERM` | Graceful shutdown (drain + stop) |
| `SIGINT` | Graceful shutdown (drain + stop) |
| `SIGQUIT` | Immediate shutdown (dump goroutines first) |
| `SIGUSR1` | Reopen log files (for log rotation) |
| `SIGUSR2` | Print internal state to stderr |

---

## 5. L7 HTTP/HTTPS Proxy

### 5.1 HTTP Processing Pipeline

```
Client Request
       │
       ▼
[TLS Termination] (if HTTPS)
       │
       ▼
[HTTP Parsing] (method, path, headers, body)
       │
       ▼
[Request ID Injection] (X-Request-Id: uuid)
       │
       ▼
[Access Logging Start] (record start time)
       │
       ▼
[Middleware Chain - Inbound]
  ├── IP Filter
  ├── Rate Limiter
  ├── WAF Scanner
  ├── Auth (Basic/JWT/APIKey)
  ├── CORS
  ├── Request Size Limit
  └── Custom Headers
       │
       ▼
[Router] (match host + path + headers → route)
       │
       ▼
[Load Balancer] (select backend from pool)
       │
       ▼
[Circuit Breaker Check]
       │
       ▼
[Backend Connection] (from pool or new)
       │
       ▼
[Proxy Request]
  ├── Add X-Forwarded-For, X-Real-IP
  ├── Add X-Forwarded-Proto
  ├── Add X-Forwarded-Host
  ├── Rewrite path (if configured)
  ├── Strip/add headers
  └── Forward body (streaming)
       │
       ▼
[Backend Response]
       │
       ▼
[Middleware Chain - Outbound]
  ├── Compression (gzip/deflate/brotli)
  ├── Response Cache
  ├── Security Headers
  └── Custom Response Headers
       │
       ▼
[Metrics Collection] (latency, status code, bytes)
       │
       ▼
[Access Log Write]
       │
       ▼
Client Response
```

### 5.2 HTTP Parser

We use Go's `net/http` from stdlib for HTTP/1.1 parsing (it's part of stdlib, not an external dependency). For HTTP/2 we use `golang.org/x/net/http2` — **exception**: since `x/` packages are maintained by the Go team and are quasi-stdlib, we allow `golang.org/x/net` and `golang.org/x/crypto` as the ONLY external packages. If strict zero-dep is enforced, we implement HTTP/2 framing ourselves.

**Decision**: Allow `golang.org/x/net` and `golang.org/x/crypto` only, or implement from scratch.

### 5.3 WebSocket Proxying

```go
// WebSocket upgrade detection
func isWebSocketUpgrade(r *http.Request) bool {
    return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
           strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
```

WebSocket proxy strategy:
1. Detect upgrade request
2. Forward upgrade to backend
3. If backend responds with 101 Switching Protocols, hijack both connections
4. Bidirectional copy with configurable buffer size
5. Track active WebSocket connections separately
6. Support ping/pong frames for keepalive
7. Configurable idle timeout for WebSocket connections

### 5.4 gRPC Proxying

gRPC runs over HTTP/2. Requirements:
- HTTP/2 with prior knowledge (h2c) support
- Trailer header propagation
- Streaming support (unary, server-streaming, client-streaming, bidirectional)
- gRPC health check protocol support
- Max message size configuration

### 5.5 Server-Sent Events (SSE)

- Detect `Accept: text/event-stream` header
- Disable response buffering for SSE endpoints
- Keep connection open, proxy events as they arrive
- Configurable SSE connection timeout

### 5.6 Request/Response Buffering

Two modes:
1. **Buffered** (default for requests < 1MB): Buffer entire request body before forwarding. Allows retry on backend failure.
2. **Streaming** (for large bodies or SSE/WebSocket): Stream body directly. No retry possible.

Configurable per-route:
```yaml
routes:
  - match:
      path: /upload/*
    proxy:
      buffer_request: false    # Stream large uploads
      buffer_response: false
```

### 5.7 Connection Pooling

Per-backend connection pool:
- **Min idle connections**: Keep warm connections ready
- **Max connections per backend**: Prevent overwhelming a single backend
- **Max idle time**: Close idle connections after timeout
- **Health check on reuse**: Optional quick check before reuse
- **HTTP/2 multiplexing**: Single connection for multiple streams

```go
type PoolConfig struct {
    MaxConnsPerHost    int           // Default: 100
    MaxIdleConns       int           // Default: 10
    MaxIdleTime        time.Duration // Default: 90s
    ConnectTimeout     time.Duration // Default: 5s
    TLSHandshakeTimeout time.Duration // Default: 5s
    DialKeepAlive      time.Duration // Default: 30s
}
```

---

## 6. L4 TCP/UDP Proxy

### 6.1 TCP Proxy

Pure TCP proxying (Layer 4):
- No protocol awareness (opaque byte stream)
- Bidirectional copy between client and backend
- **Zero-copy on Linux**: Use `splice()` syscall for kernel-space data transfer
- Connection tracking (source IP, port, backend, bytes transferred, duration)
- Source IP preservation via PROXY protocol v1/v2

```go
// Simplified TCP proxy flow
func (p *TCPProxy) handleConn(clientConn net.Conn) {
    backend := p.balancer.Next(clientConn.RemoteAddr())
    backendConn, err := p.pool.Get(backend)
    if err != nil {
        p.metrics.BackendError(backend)
        return
    }
    
    // Bidirectional copy (or splice on Linux)
    go p.copy(backendConn, clientConn) // client → backend
    p.copy(clientConn, backendConn)    // backend → client
}
```

### 6.2 SNI-Based TLS Routing

For TCP mode with TLS passthrough:
1. Read ClientHello to extract SNI (Server Name Indication)
2. Route to appropriate backend based on SNI
3. Forward raw TLS bytes (no termination)
4. Backend handles TLS termination

```go
// Peek at TLS ClientHello without consuming the connection
func extractSNI(conn net.Conn) (string, []byte, error) {
    // Read up to 16KB for ClientHello
    buf := make([]byte, 16384)
    n, err := conn.Read(buf)
    // Parse TLS record → HandshakeHello → SNI extension
    sni := parseSNI(buf[:n])
    return sni, buf[:n], err
}
```

### 6.3 PROXY Protocol

Support both sending and receiving PROXY protocol:
- **v1** (text): `PROXY TCP4 192.168.1.1 10.0.0.1 56789 8080\r\n`
- **v2** (binary): Structured binary header with TLV extensions

Use cases:
- Preserve original client IP when behind another LB
- Send client IP to backends that support PROXY protocol

### 6.4 UDP Proxy

UDP proxying (for DNS, game servers, etc.):
- Session tracking by source IP:port (configurable timeout)
- No connection concept — use a mapping table
- Support for PROXY protocol header in UDP (custom extension)

```go
type UDPSession struct {
    ClientAddr  *net.UDPAddr
    BackendAddr *net.UDPAddr
    BackendConn *net.UDPConn
    LastActive  time.Time
}
```

### 6.5 Use Cases for L4

| Protocol | Port | Notes |
|----------|------|-------|
| MySQL | 3306 | TCP proxy with read/write splitting possible |
| PostgreSQL | 5432 | TCP proxy |
| Redis | 6379 | TCP proxy |
| SMTP | 25/587 | TCP proxy |
| SSH | 22 | TCP proxy (SNI not available) |
| DNS | 53 | UDP proxy |
| Game servers | various | UDP proxy |
| TLS passthrough | 443 | SNI-based routing |

---

## 7. TLS Engine

### 7.1 TLS Configuration

```go
type TLSConfig struct {
    // Per-listener
    MinVersion       string   // "1.2" (default), "1.3"
    MaxVersion       string   // "1.3" (default)
    CipherSuites     []string // If empty, use secure defaults
    CurvePreferences []string // P256, P384, X25519
    
    // Certificate sources
    Certificates     []CertificateSource
    
    // ACME
    ACME             *ACMEConfig
    
    // Client auth
    ClientAuth       string   // "none", "request", "require", "verify"
    ClientCAs        []string // CA cert files for mTLS
    
    // OCSP
    OCSPStapling     bool     // Default: true
}

type CertificateSource struct {
    CertFile string // PEM cert file
    KeyFile  string // PEM key file
    Domains  []string // SNI matching
}

type ACMEConfig struct {
    Email      string
    Provider   string   // "letsencrypt" (default), "letsencrypt-staging", "zerossl"
    Domains    []string
    Challenge  string   // "http-01" (default), "tls-alpn-01"
    Storage    string   // Directory to store certs
    RenewBefore time.Duration // Default: 30 days before expiry
}
```

### 7.2 ACME Client (Let's Encrypt)

Full ACME v2 (RFC 8555) implementation from scratch:
1. **Account creation**: Generate ECDSA P-256 key, register with ACME server
2. **Order creation**: Submit CSR for domains
3. **Challenge solving**:
   - HTTP-01: Serve token at `/.well-known/acme-challenge/{token}`
   - TLS-ALPN-01: Self-signed cert with ACME extension on port 443
4. **Certificate retrieval**: Download signed cert chain
5. **Auto-renewal**: Background goroutine checks expiry, renews 30 days before
6. **Certificate storage**: Local filesystem (PEM files)

### 7.3 SNI Multiplexer

Multiple certificates per listener:
```go
func (m *TLSManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
    // 1. Exact match
    if cert, ok := m.certs[hello.ServerName]; ok {
        return cert, nil
    }
    // 2. Wildcard match
    if cert := m.findWildcard(hello.ServerName); cert != nil {
        return cert, nil
    }
    // 3. ACME on-demand (if enabled)
    if m.acme != nil && m.acme.OnDemand {
        return m.acme.GetOrCreate(hello.ServerName)
    }
    // 4. Default cert
    return m.defaultCert, nil
}
```

### 7.4 OCSP Stapling

- Fetch OCSP response from CA's OCSP responder
- Cache response (respect nextUpdate)
- Staple to TLS handshake
- Background refresh before expiry

### 7.5 mTLS (Mutual TLS)

- Between OLB and backends (upstream mTLS)
- Between OLB and clients (downstream mTLS)
- Between cluster nodes (inter-node mTLS)
- Client certificate validation with CRL/OCSP checking

---

## 8. Load Balancing Algorithms

### 8.1 Algorithm Interface

```go
type Balancer interface {
    // Name returns the algorithm name
    Name() string
    
    // Next selects the next backend for the given request context
    Next(ctx *RequestContext) *backend.Backend
    
    // Add registers a backend
    Add(b *backend.Backend)
    
    // Remove deregisters a backend
    Remove(id string)
    
    // Update modifies backend weight or metadata
    Update(id string, opts ...UpdateOption)
    
    // Stats returns algorithm-specific statistics
    Stats() map[string]interface{}
}

type RequestContext struct {
    ClientIP    net.IP
    Method      string
    Host        string
    Path        string
    Headers     http.Header
    SessionKey  string    // For session affinity
    RetryCount  int       // Current retry attempt
}
```

### 8.2 Algorithms

#### Round Robin
Simple rotation through backends. O(1) selection.
```
Backend A → Backend B → Backend C → Backend A → ...
```

#### Weighted Round Robin (Smooth)
Backends get traffic proportional to weight. Uses Nginx-style smooth weighted round-robin to avoid bursts.
```
Weights: A=5, B=3, C=2
Traffic: A, A, B, A, C, A, B, A, B, C (smooth distribution)
```

#### Least Connections
Route to backend with fewest active connections. O(n) selection, but n is typically small.

#### Least Response Time
Route to backend with lowest average response time over a sliding window. Combines latency + active connections.

#### IP Hash
Deterministic mapping: `hash(client_ip) % len(backends)`. Provides session affinity without cookies.

#### Consistent Hashing (Ketama)
Uses a hash ring with virtual nodes. When backends are added/removed, only K/n keys need redistribution.
- 150 virtual nodes per backend (configurable)
- Jump hash as alternative (zero memory, but doesn't support weighted)

#### Maglev Hashing (Google)
Google's Maglev hashing algorithm:
- Lookup table of size M (prime, e.g., 65537)
- O(1) lookup after O(M * n) initialization
- Minimal disruption on backend changes
- Better load distribution than ring hash

#### Power of Two Random Choices (P2C)
Pick 2 random backends, choose the one with fewer active connections. Near-optimal with O(1) selection.

#### Random / Weighted Random
Simple random selection with optional weights.

### 8.3 Session Affinity

Support sticky sessions via:
1. **Cookie**: Insert/read a cookie (`olb-session-id`) mapping to backend
2. **Header**: Use a custom header value as session key
3. **IP**: Use client IP (via IP Hash)
4. **URL parameter**: Use a query parameter as session key

Affinity config per route:
```yaml
routes:
  - match:
      path: /app/*
    balancer:
      algorithm: round_robin
      sticky:
        type: cookie
        name: OLB_BACKEND
        ttl: 1h
        http_only: true
        secure: true
```

---

## 9. Health Checking

### 9.1 Active Health Checks

Proactively check backends at regular intervals.

#### HTTP Health Check
```go
type HTTPHealthCheck struct {
    Path               string        // Default: "/"
    Method             string        // Default: "GET"
    ExpectedStatus     []int         // Default: [200]
    ExpectedBody       string        // Substring match
    Headers            map[string]string
    Timeout            time.Duration // Default: 5s
    Interval           time.Duration // Default: 10s
    SuccessThreshold   int           // Default: 2 (consecutive successes to mark UP)
    FailureThreshold   int           // Default: 3 (consecutive failures to mark DOWN)
}
```

#### TCP Health Check
```go
type TCPHealthCheck struct {
    Timeout          time.Duration // Default: 5s
    Interval         time.Duration // Default: 10s
    SendData         []byte        // Optional data to send
    ExpectData       []byte        // Optional expected response prefix
    SuccessThreshold int           // Default: 2
    FailureThreshold int           // Default: 3
}
```

#### gRPC Health Check
Implements the gRPC Health Checking Protocol:
```
rpc Check(HealthCheckRequest) returns (HealthCheckResponse)
```

#### External Command Check
Run an external command. Exit code 0 = healthy.
```yaml
health:
  type: exec
  command: "/scripts/check-db.sh"
  args: ["--host", "{{.Address}}", "--port", "{{.Port}}"]
  timeout: 10s
```

### 9.2 Passive Health Checks

Monitor real traffic for errors:
```go
type PassiveHealthCheck struct {
    // Error detection
    ErrorRateThreshold  float64       // Mark DOWN if error rate > X% (default: 50%)
    ErrorWindow         time.Duration // Sliding window (default: 30s)
    MinRequests         int           // Min requests in window before evaluating (default: 10)
    
    // What counts as error
    HTTPErrorCodes      []int         // Default: [500, 502, 503, 504]
    TimeoutAsError      bool          // Default: true
    ConnectionRefusedAsError bool     // Default: true
    
    // Recovery
    RecoveryTime        time.Duration // Time before re-enabling (default: 30s)
}
```

### 9.3 Health State Machine

```
                    ┌─────────────────────┐
                    │                     │
         ┌─────────▼────────┐    ┌───────┴───────┐
         │      HEALTHY      │◄──│   STARTING    │
         │  (accepts traffic)│    │ (no traffic)  │
         └────────┬──────────┘    └───────────────┘
                  │
                  │ failures >= threshold
                  ▼
         ┌────────────────────┐
         │     UNHEALTHY      │
         │  (no new traffic)  │──────────────┐
         └────────┬───────────┘              │
                  │                          │ admin action
                  │ successes >= threshold   │
                  ▼                          ▼
         ┌────────────────────┐    ┌────────────────┐
         │      HEALTHY       │    │  MAINTENANCE   │
         │  (accepts traffic) │    │ (no traffic)   │
         └────────────────────┘    └────────────────┘
                                            │
                                            │ admin action
                                            ▼
                                   ┌────────────────┐
                                   │    DRAINING     │
                                   │ (finish active, │
                                   │  no new traffic)│
                                   └────────────────┘
```

---

## 10. Middleware Pipeline

### 10.1 Middleware Interface

```go
type Middleware interface {
    // Name returns unique middleware identifier
    Name() string
    
    // Process handles the request/response
    // Call next.Process() to continue the chain
    // Return without calling next to short-circuit
    Process(ctx *RequestContext, next Handler) error
    
    // Priority returns execution order (lower = earlier)
    Priority() int
}

type Handler interface {
    Process(ctx *RequestContext) error
}

type RequestContext struct {
    Request     *http.Request
    Response    *ResponseWriter
    Route       *Route
    Backend     *Backend
    StartTime   time.Time
    RequestID   string
    ClientIP    net.IP
    Metadata    map[string]interface{}
    Metrics     *RequestMetrics
}
```

### 10.2 Default Middleware Order

| Priority | Middleware | Description |
|----------|-----------|-------------|
| 10 | Request ID | Inject X-Request-Id |
| 20 | Real IP | Extract real IP from X-Forwarded-For |
| 30 | Access Log (start) | Record request start |
| 40 | Metrics (start) | Start request timer |
| 50 | IP Filter | Allow/deny by IP/CIDR |
| 60 | Rate Limiter | Rate limit by IP/key |
| 70 | WAF | Basic attack detection |
| 80 | Auth | Authentication check |
| 90 | CORS | CORS header handling |
| 100 | Request Size | Body size limit |
| 110 | Headers (request) | Modify request headers |
| 120 | Rewrite | URL rewriting |
| 130 | Cache (lookup) | Check response cache |
| 140 | **→ Proxy →** | Forward to backend |
| 150 | Cache (store) | Store cacheable response |
| 160 | Compression | Compress response |
| 170 | Headers (response) | Modify response headers |
| 180 | Security Headers | Add security headers |
| 190 | Metrics (end) | Record response metrics |
| 200 | Access Log (end) | Write access log entry |

### 10.3 Rate Limiter

Three algorithms, configurable per route:

**Token Bucket** (default):
- Smooth rate limiting with burst allowance
- `rate`: tokens per second
- `burst`: max tokens (bucket size)

**Sliding Window**:
- Fixed rate over sliding time window
- More predictable than token bucket
- `limit`: max requests
- `window`: time window

**Leaky Bucket**:
- Constant output rate, smooths bursts
- `rate`: requests per second
- `queue_size`: max queued requests

Rate limit keys:
- `client_ip`: Per client IP
- `header:X-API-Key`: Per header value
- `path`: Per request path
- `composite`: Combination of multiple keys

Response when rate limited:
```http
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1609459261
Retry-After: 30
Content-Type: application/json

{"error": "rate_limit_exceeded", "retry_after": 30}
```

### 10.4 Circuit Breaker

States: **Closed** → **Open** → **Half-Open** → Closed

```go
type CircuitBreakerConfig struct {
    // When to open
    ErrorThreshold   int           // Errors to open (default: 5)
    ErrorWindow      time.Duration // Window for counting errors (default: 10s)
    
    // When open
    OpenDuration     time.Duration // How long to stay open (default: 30s)
    
    // Half-open
    HalfOpenRequests int           // Requests to test in half-open (default: 3)
    
    // What counts as error
    FailureCodes     []int         // HTTP codes (default: [500, 502, 503, 504])
    TimeoutAsFailure bool          // Default: true
}
```

### 10.5 Retry

```go
type RetryConfig struct {
    MaxRetries    int           // Default: 3
    RetryOn       []int         // HTTP codes to retry (default: [502, 503, 504])
    RetryMethods  []string      // Methods safe to retry (default: [GET, HEAD, OPTIONS])
    Backoff       BackoffConfig
    RetryTimeout  time.Duration // Total timeout for all retries
}

type BackoffConfig struct {
    Initial    time.Duration // Default: 100ms
    Max        time.Duration // Default: 10s
    Multiplier float64       // Default: 2.0
    Jitter     float64       // Default: 0.1 (10% jitter)
}
```

### 10.6 Compression

Automatic compression based on `Accept-Encoding`:
- **gzip**: stdlib `compress/gzip`
- **deflate**: stdlib `compress/flate`
- **brotli**: Pure Go implementation (from scratch)

Rules:
- Only compress if response > min size (default: 1KB)
- Skip already-compressed content types (images, video, archives)
- Respect `Cache-Control: no-transform`
- Set `Vary: Accept-Encoding`

### 10.7 Response Cache

In-memory LRU cache for HTTP responses:
```go
type CacheConfig struct {
    Enabled     bool
    MaxSize     int64         // Max cache size in bytes (default: 100MB)
    MaxAge      time.Duration // Default max-age (default: 5m)
    Methods     []string      // Cacheable methods (default: [GET, HEAD])
    StatusCodes []int         // Cacheable status codes (default: [200, 301, 302])
    KeyTemplate string        // Cache key template (default: "{{.Method}}:{{.Host}}{{.Path}}?{{.Query}}")
    
    // Cache-Control header respect
    RespectCacheControl bool  // Default: true
    RespectExpires      bool  // Default: true
    
    // Stale serving
    StaleWhileRevalidate time.Duration
    StaleIfError         time.Duration
}
```

### 10.8 Basic WAF

Rule-based Web Application Firewall:
- SQL injection pattern detection (UNION, SELECT, DROP, --, etc.)
- XSS pattern detection (script tags, event handlers, javascript: URIs)
- Path traversal detection (../, %2e%2e)
- Command injection detection (;, |, &&, backticks)
- Request size limits
- Configurable rule sets (enable/disable per rule)
- Block or log-only mode

---

## 11. Configuration System

### 11.1 Config Formats

All three formats are parsed from scratch (no external libraries).

#### YAML Parser
Full YAML 1.2 subset implementation:
- Mappings (key: value)
- Sequences (- item)
- Scalars (strings, numbers, booleans, null)
- Multi-line strings (literal `|`, folded `>`)
- Anchors and aliases (`&anchor`, `*alias`)
- Comments (#)
- Nested structures
- Flow style for compact notation

#### TOML Parser
TOML v1.0 implementation:
- Key/value pairs
- Tables `[table]`
- Array of tables `[[array]]`
- Inline tables
- All value types (string, int, float, bool, datetime, array)
- Multi-line strings (""")
- Dotted keys

#### HCL Parser
HashiCorp Configuration Language subset:
- Blocks with labels
- Attributes
- String interpolation `${var}`
- References
- Comments (// and /* */)
- Here-doc strings
- Terraform-style syntax

### 11.2 Config Structure

```yaml
# olb.yaml - Full configuration reference

# Global settings
global:
  # Worker settings
  workers: auto                      # "auto" = GOMAXPROCS = NumCPU
  max_connections: 65536             # Global max connections
  connection_timeout: 60s            # Idle connection timeout
  graceful_timeout: 30s              # Shutdown drain timeout
  
  # Logging
  log:
    level: info                      # trace, debug, info, warn, error, fatal
    format: json                     # json, text, clf (Common Log Format)
    output: stdout                   # stdout, stderr, file, syslog
    file:
      path: /var/log/olb/olb.log
      max_size: 100MB
      max_backups: 5
      max_age: 30d
      compress: true
    access_log:
      enabled: true
      path: /var/log/olb/access.log
      format: json                   # json, clf, custom
      fields:                        # Extra fields to include
        - request_id
        - upstream_addr
        - upstream_latency
        - tls_version
  
  # DNS resolver
  dns:
    servers:
      - 8.8.8.8:53
      - 1.1.1.1:53
    timeout: 5s
    cache_ttl: 60s

# Admin API
admin:
  listen: 127.0.0.1:9090           # Admin API address
  tls:
    enabled: false
    cert: /etc/olb/admin.crt
    key: /etc/olb/admin.key
  auth:
    type: basic                     # basic, token, none
    users:
      - username: admin
        password_hash: "$2a$..."    # bcrypt hash
    token: ""                       # Bearer token
  webui:
    enabled: true
    path_prefix: /ui               # Web UI at /ui
  cors:
    allowed_origins:
      - "*"

# Metrics
metrics:
  enabled: true
  prometheus:
    enabled: true
    path: /metrics                  # On admin API
  retention: 1h                     # In-memory metrics retention
  resolution: 10s                   # Metrics aggregation interval

# Listeners (frontends)
listeners:
  # HTTP listener
  - name: http
    protocol: http
    address: ":80"
    
    # Optional: redirect all to HTTPS
    redirect_https: true
    
  # HTTPS listener  
  - name: https
    protocol: https
    address: ":443"
    
    tls:
      min_version: "1.2"
      certificates:
        - cert: /etc/olb/certs/example.com.crt
          key: /etc/olb/certs/example.com.key
          domains:
            - example.com
            - "*.example.com"
      acme:
        enabled: true
        email: admin@example.com
        domains:
          - example.com
          - www.example.com
        storage: /etc/olb/acme/
  
  # TCP listener (e.g., MySQL)
  - name: mysql
    protocol: tcp
    address: ":3306"
    
    # TCP-specific
    proxy_protocol: v2             # Send PROXY protocol to backends
    
  # UDP listener (e.g., DNS)
  - name: dns
    protocol: udp
    address: ":53"
    session_timeout: 30s

# Backend pools (upstreams)
backends:
  # Web application pool
  - name: web-app
    backends:
      - address: 10.0.1.1:8080
        weight: 5
        max_connections: 200
      - address: 10.0.1.2:8080
        weight: 3
        max_connections: 200
      - address: 10.0.1.3:8080
        weight: 2
        max_connections: 200
    
    balancer:
      algorithm: weighted_round_robin
      sticky:
        type: cookie
        name: OLB_SRV
        ttl: 1h
    
    health:
      active:
        type: http
        path: /health
        interval: 10s
        timeout: 5s
        success_threshold: 2
        failure_threshold: 3
      passive:
        enabled: true
        error_rate: 50
        window: 30s
    
    connection:
      max_idle: 10
      max_per_host: 100
      idle_timeout: 90s
      connect_timeout: 5s
    
    circuit_breaker:
      enabled: true
      error_threshold: 5
      open_duration: 30s
  
  # API pool
  - name: api-pool
    backends:
      - address: 10.0.2.1:3000
      - address: 10.0.2.2:3000
    balancer:
      algorithm: least_connections
    health:
      active:
        type: http
        path: /api/health
        interval: 5s
  
  # Database pool (TCP)
  - name: mysql-pool
    backends:
      - address: 10.0.3.1:3306
        metadata:
          role: primary
      - address: 10.0.3.2:3306
        metadata:
          role: replica
      - address: 10.0.3.3:3306
        metadata:
          role: replica
    balancer:
      algorithm: round_robin
    health:
      active:
        type: tcp
        interval: 5s

# Routes
routes:
  # Main website
  - name: website
    match:
      hosts:
        - example.com
        - www.example.com
      path: /*
    action:
      backend: web-app
    middleware:
      - rate_limit:
          rate: 100
          burst: 200
          key: client_ip
      - compress:
          types:
            - text/html
            - text/css
            - application/javascript
      - security_headers:
          hsts: true
          hsts_max_age: 31536000
          content_type_nosniff: true
          frame_options: DENY
          xss_protection: true
      - cache:
          max_age: 5m
          methods: [GET]
  
  # API routes
  - name: api
    match:
      hosts:
        - api.example.com
      path: /v1/*
    action:
      backend: api-pool
      rewrite: /v1/(.*) → /$1
    middleware:
      - rate_limit:
          rate: 50
          burst: 100
          key: "header:X-API-Key"
      - auth:
          type: jwt
          jwks_url: https://auth.example.com/.well-known/jwks.json
      - cors:
          allowed_origins: ["*"]
          allowed_methods: [GET, POST, PUT, DELETE]
          allowed_headers: [Authorization, Content-Type]
          max_age: 86400
      - retry:
          max_retries: 2
          retry_on: [502, 503]
  
  # WebSocket route
  - name: websocket
    match:
      path: /ws/*
      headers:
        Upgrade: websocket
    action:
      backend: web-app
    proxy:
      websocket: true
      timeout: 0                    # No timeout for WS
  
  # MySQL TCP route
  - name: mysql-route
    listener: mysql
    match:
      protocol: tcp
    action:
      backend: mysql-pool
  
  # Canary deployment
  - name: canary
    match:
      path: /feature/*
    action:
      split:
        - backend: web-app
          weight: 90
        - backend: web-app-canary
          weight: 10

# Cluster configuration
cluster:
  enabled: false
  node_name: node-1
  bind_address: 0.0.0.0:7946        # Gossip
  raft_address: 0.0.0.0:7947         # Raft consensus
  peers:
    - 10.0.0.1:7946
    - 10.0.0.2:7946
    - 10.0.0.3:7946
  tls:
    enabled: true
    cert: /etc/olb/cluster.crt
    key: /etc/olb/cluster.key
    ca: /etc/olb/cluster-ca.crt

# MCP Server
mcp:
  enabled: false
  transport: stdio                   # stdio, http
  http:
    address: 127.0.0.1:9091
  tools:
    - query_metrics
    - list_backends
    - backend_status
    - modify_route
    - add_backend
    - remove_backend
    - drain_backend
    - get_config
    - set_config
    - diagnose
    - get_logs
```

### 11.3 Environment Variable Overlay

Every config key can be overridden via env vars:
- Format: `OLB_` prefix + path with `__` separator
- Example: `OLB_GLOBAL__LOG__LEVEL=debug`
- Example: `OLB_ADMIN__LISTEN=0.0.0.0:9090`
- Arrays via comma-separated: `OLB_LISTENERS__0__ADDRESS=:80`

### 11.4 Config Validation

Strict validation at load time:
- All addresses must be valid host:port
- All durations must parse correctly
- Algorithm names must be valid
- Backend references in routes must exist
- TLS cert/key files must be readable
- Circular references detection
- Port conflict detection
- Sensible defaults for all optional fields

### 11.5 Config Diff

On hot reload, compute a diff and log changes:
```
[RELOAD] Config changes detected:
  + backends.web-app.backends[2]: 10.0.1.4:8080 (weight=3)
  ~ routes.api.middleware.rate_limit.rate: 50 → 100
  - backends.old-pool: removed
```

---

## 12. Service Discovery

### 12.1 Discovery Interface

```go
type Discovery interface {
    // Name returns the discovery provider name
    Name() string
    
    // Watch starts watching for backend changes
    Watch(ctx context.Context, service string) (<-chan []Backend, error)
    
    // Resolve returns current backends for a service
    Resolve(ctx context.Context, service string) ([]Backend, error)
}
```

### 12.2 Providers

#### Static (Config-based)
Backends defined in config file. Changes via hot-reload or API.

#### DNS SRV
```yaml
backends:
  - name: web-app
    discovery:
      type: dns_srv
      record: _http._tcp.web-app.service.consul
      interval: 30s
```

Resolves SRV records, updates backends on DNS changes.

#### DNS A/AAAA
```yaml
backends:
  - name: web-app
    discovery:
      type: dns_a
      hostname: web-app.internal
      port: 8080
      interval: 30s
```

#### File-based
Watch a JSON/YAML file for backend definitions. Useful for integration with external systems.
```yaml
backends:
  - name: web-app
    discovery:
      type: file
      path: /etc/olb/backends/web-app.json
```

#### Docker (Unix Socket)
Connect to Docker daemon via unix socket, discover containers by label:
```yaml
backends:
  - name: web-app
    discovery:
      type: docker
      socket: /var/run/docker.sock
      label: olb.service=web-app
      port_label: olb.port
      network: bridge
```

---

## 13. Observability & Metrics

### 13.1 Metrics Engine

Custom metrics engine built from scratch:
- **Counter**: Monotonically increasing (requests, bytes, errors)
- **Gauge**: Current value (connections, backends up)
- **Histogram**: Distribution of values (latency, response size)
- **Time Series**: Rolling window of data points

All metrics are lock-free using atomic operations.

### 13.2 Built-in Metrics

```
# Global
olb_uptime_seconds                          gauge
olb_config_reload_total                     counter
olb_config_reload_errors_total              counter

# Listeners
olb_listener_connections_total{listener}    counter
olb_listener_connections_active{listener}   gauge
olb_listener_bytes_received_total{listener} counter
olb_listener_bytes_sent_total{listener}     counter

# Routes
olb_route_requests_total{route,method,status}     counter
olb_route_request_duration_seconds{route}         histogram
olb_route_request_size_bytes{route}               histogram
olb_route_response_size_bytes{route}              histogram

# Backends
olb_backend_up{pool,backend}                      gauge (0/1)
olb_backend_connections_active{pool,backend}       gauge
olb_backend_connections_total{pool,backend}        counter
olb_backend_request_duration_seconds{pool,backend} histogram
olb_backend_requests_total{pool,backend,status}    counter
olb_backend_health_checks_total{pool,backend,result} counter
olb_backend_bytes_sent_total{pool,backend}         counter
olb_backend_bytes_received_total{pool,backend}     counter

# Middleware
olb_ratelimit_rejected_total{route,key}            counter
olb_circuit_breaker_state{pool}{state}             gauge
olb_circuit_breaker_trips_total{pool}              counter
olb_cache_hits_total{route}                        counter
olb_cache_misses_total{route}                      counter
olb_waf_blocked_total{route,rule}                  counter
olb_retry_total{route,attempt}                     counter

# TLS
olb_tls_handshake_duration_seconds                 histogram
olb_tls_cert_expiry_timestamp_seconds{domain}      gauge
olb_acme_renewal_total{domain,result}              counter

# Cluster
olb_cluster_nodes{state}                           gauge
olb_cluster_raft_term                              gauge
olb_cluster_raft_leader                            gauge (0/1)

# System
olb_go_goroutines                                  gauge
olb_go_gc_pause_seconds                            histogram
olb_process_open_fds                               gauge
olb_process_resident_memory_bytes                  gauge
olb_process_cpu_seconds_total                      counter
```

### 13.3 Prometheus Export

Expose metrics in Prometheus exposition format on admin API:
```
GET /metrics
```

### 13.4 JSON Metrics API

For the Web UI and CLI:
```
GET /api/v1/metrics                    # All metrics
GET /api/v1/metrics/backends           # Backend-specific
GET /api/v1/metrics/routes             # Route-specific
GET /api/v1/metrics/timeseries?metric=olb_route_requests_total&range=1h&step=10s
```

### 13.5 Structured Logging

```go
// Every log entry includes:
type LogEntry struct {
    Timestamp  time.Time   `json:"ts"`
    Level      string      `json:"level"`
    Message    string      `json:"msg"`
    Component  string      `json:"component"`
    RequestID  string      `json:"request_id,omitempty"`
    ClientIP   string      `json:"client_ip,omitempty"`
    Method     string      `json:"method,omitempty"`
    Path       string      `json:"path,omitempty"`
    Status     int         `json:"status,omitempty"`
    Duration   float64     `json:"duration_ms,omitempty"`
    Backend    string      `json:"backend,omitempty"`
    Error      string      `json:"error,omitempty"`
    Fields     map[string]interface{} `json:"fields,omitempty"`
}
```

### 13.6 Access Log Format

Support multiple formats:
- **JSON**: Full structured log
- **CLF** (Common Log Format): `127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] "GET /page HTTP/1.1" 200 2326`
- **Combined**: CLF + referrer + user-agent
- **Custom**: Template-based format string

---

## 14. Web UI Dashboard

### 14.1 Technology

- **No framework**: Vanilla JavaScript (ES2020+)
- **No build step**: Directly embedded via `go:embed`
- **CSS**: Custom CSS with CSS variables for theming
- **Charts**: Custom chart library (from scratch, Canvas-based)
- **Real-time**: WebSocket connection to admin API
- **SPA**: Client-side routing (hash-based)

### 14.2 Pages

#### Dashboard (Home)
- Live request rate (requests/sec sparkline)
- Active connections gauge
- Error rate percentage
- Backend health overview (up/down/draining grid)
- Top routes by traffic (bar chart)
- Latency distribution (histogram)
- System resources (CPU, memory, goroutines)
- Recent errors (scrolling list)
- Uptime, version, reload count

#### Backends
- Table of all backend pools
- Per-backend: status, connections, RPS, latency, error rate
- Actions: drain, enable, disable, remove
- Add new backend (form)
- Real-time health check results
- Connection pool stats

#### Routes
- Table of all routes with match criteria
- Per-route: RPS, latency (p50/p95/p99), error rate, cache hit rate
- Route editor (YAML/visual)
- Route testing tool (send test request)

#### Metrics
- Interactive charts (time range selector)
- Metric explorer (search, filter, aggregate)
- Custom dashboard builder (drag-drop widgets)
- Export to JSON/CSV

#### Logs
- Real-time access log stream (WebSocket)
- Full-text search
- Filter by: level, route, backend, status code, client IP
- Log entry detail view
- Download logs

#### Config
- Current config viewer (syntax highlighted)
- Config editor with validation
- Config diff (current vs saved)
- Reload button with confirmation
- Config history (last N reloads)

#### Certificates
- Certificate inventory
- Expiry dates with warnings
- ACME status
- Add/remove certificates
- Force renewal

#### Cluster
- Node list with status (leader, follower, candidate)
- Raft state visualization
- Log replication status
- Network topology diagram

### 14.3 Design Language

- Dark mode by default, light mode toggle
- Color palette: dark grays, accent green (healthy), red (error), yellow (warning), blue (info)
- Minimal, information-dense layout
- Responsive (works on mobile for emergency access)
- Accessible (ARIA labels, keyboard navigation)
- <2MB total bundle size

---

## 15. CLI Interface

### 15.1 CLI Architecture

Custom argument parser (no cobra/urfave dependency):
```
olb <command> [subcommand] [flags] [args]
```

### 15.2 Commands

```
olb start [--config path] [--daemon] [--pid-file path]
olb stop [--graceful] [--timeout duration]
olb reload
olb status [--json] [--wide]
olb version [--short]

# Config
olb config validate [--config path]
olb config show [--format yaml|toml|json]
olb config diff <old> <new>
olb config generate [--format yaml|toml|hcl]     # Interactive config generator

# Backend management
olb backend list [--pool name] [--format table|json]
olb backend add <pool> <address> [--weight N] [--max-conn N]
olb backend remove <pool> <address>
olb backend drain <pool> <address> [--timeout duration]
olb backend enable <pool> <address>
olb backend disable <pool> <address>
olb backend stats <pool> [--backend address]

# Route management
olb route list [--format table|json]
olb route add <name> --match-host <host> --match-path <path> --backend <pool>
olb route remove <name>
olb route test <url>                                # Test which route matches

# Health
olb health show [--pool name] [--format table|json]
olb health check <pool> <backend>                   # Trigger immediate check

# Metrics
olb metrics show [--filter pattern] [--format table|json|prometheus]
olb metrics export [--format prometheus|json] [--output file]

# Certificates
olb cert list [--format table|json]
olb cert add <domain> --cert <file> --key <file>
olb cert remove <domain>
olb cert renew <domain>                             # Force ACME renewal
olb cert info <domain>                              # Show cert details

# Cluster
olb cluster status
olb cluster join <address>
olb cluster leave
olb cluster members

# Logs
olb log tail [--lines N] [--follow] [--filter expr]
olb log search <query> [--since time] [--until time]

# Live monitoring
olb top                                             # htop-style live dashboard

# Completions
olb completion bash
olb completion zsh
olb completion fish
```

### 15.3 `olb top` — Live TUI Dashboard

Terminal UI showing real-time stats:
```
┌─ OpenLoadBalancer v1.0.0 ─── node-1 ─── uptime: 3d 14h 22m ──────────┐
│                                                                        │
│  Requests/s: ████████████████████░░░░░  1,247/s  (peak: 3,891)       │
│  Active Conn: ██████░░░░░░░░░░░░░░░░░   342/4096                     │
│  Error Rate:  █░░░░░░░░░░░░░░░░░░░░░░   0.3%                        │
│  Bandwidth:   ↓ 45 MB/s  ↑ 128 MB/s                                 │
│                                                                        │
│  Backend Pool: web-app                                                │
│  ┌──────────────┬────────┬───────┬────────┬─────────┬────────────┐    │
│  │ Backend      │ Status │ Conn  │ RPS    │ p99 (ms)│ Error Rate │    │
│  ├──────────────┼────────┼───────┼────────┼─────────┼────────────┤    │
│  │ 10.0.1.1:80  │ ● UP   │ 120   │ 445    │ 23      │ 0.1%       │    │
│  │ 10.0.1.2:80  │ ● UP   │ 98    │ 312    │ 18      │ 0.2%       │    │
│  │ 10.0.1.3:80  │ ○ DOWN │ 0     │ 0      │ -       │ 100%       │    │
│  │ 10.0.1.4:80  │ ◐ DRAIN│ 45    │ 0      │ 31      │ 0.0%       │    │
│  └──────────────┴────────┴───────┴────────┴─────────┴────────────┘    │
│                                                                        │
│  Top Routes (by RPS):                                                 │
│  1. GET /api/v1/users     521/s  p99=12ms                            │
│  2. GET /static/*          312/s  p99=2ms                             │
│  3. POST /api/v1/events   198/s  p99=45ms                            │
│                                                                        │
│  [q] Quit  [b] Backends  [r] Routes  [m] Metrics  [l] Logs          │
└────────────────────────────────────────────────────────────────────────┘
```

### 15.4 Output Formats

All commands support multiple output formats:
- `--format table` (default, human-readable)
- `--format json` (machine-readable)
- `--format yaml`
- `--format wide` (extra columns in table)

---

## 16. Multi-Node Clustering

### 16.1 Cluster Architecture

```
                    ┌─────────────────┐
                    │   Raft Leader    │
                    │   (config state) │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
      ┌───────▼──────┐ ┌────▼────────┐ ┌──▼──────────┐
      │  Node 1       │ │  Node 2     │ │  Node 3     │
      │  (Leader)     │ │  (Follower) │ │  (Follower) │
      │  ┌──────────┐ │ │  ┌────────┐ │ │  ┌────────┐ │
      │  │ Gossip   │◄├─┤►│ Gossip │◄├─┤►│ Gossip │ │
      │  └──────────┘ │ │  └────────┘ │ │  └────────┘ │
      │  Proxy :80/443│ │  Proxy :80  │ │  Proxy :80  │
      │  Admin :9090  │ │  Admin :9090│ │  Admin :9090│
      └───────────────┘ └─────────────┘ └─────────────┘
```

### 16.2 What Gets Clustered

| Data | Mechanism | Consistency |
|------|-----------|-------------|
| Config (routes, backends, TLS) | Raft | Strong (linearizable) |
| Health status | Gossip | Eventual |
| Metrics | Local (aggregated on query) | Eventual |
| Rate limit counters | Gossip (CRDT counters) | Eventual |
| Session affinity table | Gossip | Eventual |

### 16.3 Raft Consensus (From Scratch)

Full Raft implementation per the Raft paper:
- Leader election with randomized timeouts
- Log replication (AppendEntries RPC)
- Safety (commitment rules)
- Log compaction (snapshots)
- Membership changes (joint consensus)

State machine: The config store is the Raft state machine. Config changes are Raft log entries.

```go
type RaftConfig struct {
    ElectionTimeout    time.Duration // Default: 150-300ms (randomized)
    HeartbeatInterval  time.Duration // Default: 50ms
    SnapshotThreshold  int           // Default: 10000 log entries
    MaxLogEntries      int           // Default: 50000
    Transport          TransportConfig
}
```

### 16.4 Gossip Protocol (SWIM)

For eventually-consistent state propagation:
- Membership: Who's alive, who's dead
- Health status: Which backends are up/down
- Metrics: Distributed counter/gauge values
- Session tables: Sticky session mappings

Based on SWIM (Scalable Weakly-consistent Infection-style Process Group Membership):
- Ping, Ping-Req, Suspect, Dead states
- Compound messages for efficiency
- Configurable probe interval and timeout

### 16.5 Distributed Rate Limiting

Use CRDT (Conflict-free Replicated Data Type) counters:
- Each node tracks local counts
- Gossip propagates counts
- Sum across all nodes for global rate
- Slightly over-limit is acceptable (eventual consistency)

### 16.6 Leader Responsibilities

Only the Raft leader processes config changes. Any node can:
- Accept proxy traffic (all nodes are active)
- Forward config changes to leader
- Serve read-only admin API
- Generate local metrics

---

## 17. MCP Server (AI Integration)

### 17.1 MCP Tools

```json
{
  "tools": [
    {
      "name": "olb_query_metrics",
      "description": "Query load balancer metrics. Get RPS, latency, error rates, connection counts for routes, backends, or globally.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "metric": {"type": "string", "description": "Metric name (e.g., 'requests_per_second', 'latency_p99', 'error_rate', 'active_connections')"},
          "scope": {"type": "string", "enum": ["global", "route", "backend", "listener"]},
          "target": {"type": "string", "description": "Route name or backend pool:address"},
          "range": {"type": "string", "description": "Time range (e.g., '5m', '1h', '24h')"}
        },
        "required": ["metric"]
      }
    },
    {
      "name": "olb_list_backends",
      "description": "List all backend pools and their backends with current status, health, connections, and performance.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "pool": {"type": "string", "description": "Filter by pool name"},
          "status": {"type": "string", "enum": ["all", "healthy", "unhealthy", "draining"]}
        }
      }
    },
    {
      "name": "olb_modify_backend",
      "description": "Add, remove, drain, enable, or disable a backend in a pool.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "action": {"type": "string", "enum": ["add", "remove", "drain", "enable", "disable"]},
          "pool": {"type": "string"},
          "address": {"type": "string"},
          "weight": {"type": "integer"},
          "drain_timeout": {"type": "string"}
        },
        "required": ["action", "pool", "address"]
      }
    },
    {
      "name": "olb_modify_route",
      "description": "Add, update, or remove a route. Supports traffic splitting for canary deployments.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "action": {"type": "string", "enum": ["add", "update", "remove"]},
          "name": {"type": "string"},
          "match": {"type": "object"},
          "backend": {"type": "string"},
          "split": {"type": "array"}
        },
        "required": ["action", "name"]
      }
    },
    {
      "name": "olb_diagnose",
      "description": "Diagnose issues. Analyze error patterns, detect anomalies, check configuration for problems, and suggest fixes.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "type": {"type": "string", "enum": ["errors", "latency", "config", "health", "capacity", "full"]},
          "target": {"type": "string", "description": "Scope: route name, backend pool, or 'all'"},
          "range": {"type": "string", "description": "Time range to analyze"}
        }
      }
    },
    {
      "name": "olb_get_logs",
      "description": "Search and retrieve access logs and error logs.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "type": {"type": "string", "enum": ["access", "error", "all"]},
          "filter": {"type": "string", "description": "Search query"},
          "limit": {"type": "integer"},
          "since": {"type": "string"},
          "level": {"type": "string", "enum": ["trace", "debug", "info", "warn", "error"]}
        }
      }
    },
    {
      "name": "olb_get_config",
      "description": "Get current configuration in YAML format.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "section": {"type": "string", "description": "Config section: global, listeners, backends, routes, cluster, or 'all'"}
        }
      }
    },
    {
      "name": "olb_cluster_status",
      "description": "Get cluster status including node list, Raft state, leader info, and replication lag.",
      "inputSchema": {
        "type": "object",
        "properties": {}
      }
    }
  ]
}
```

### 17.2 MCP Resources

```json
{
  "resources": [
    {
      "uri": "olb://metrics/dashboard",
      "name": "Live Dashboard Metrics",
      "description": "Real-time metrics suitable for a dashboard view"
    },
    {
      "uri": "olb://config/current",
      "name": "Current Configuration",
      "description": "Full current configuration in YAML"
    },
    {
      "uri": "olb://health/summary",
      "name": "Health Summary",
      "description": "All backends health status"
    },
    {
      "uri": "olb://logs/recent",
      "name": "Recent Logs",
      "description": "Last 100 log entries"
    }
  ]
}
```

### 17.3 MCP Prompt Templates

```json
{
  "prompts": [
    {
      "name": "diagnose_high_latency",
      "description": "Investigate high latency on a route or backend",
      "arguments": [{"name": "target", "required": true}]
    },
    {
      "name": "capacity_planning",
      "description": "Analyze current capacity and recommend scaling",
      "arguments": [{"name": "pool", "required": true}]
    },
    {
      "name": "deploy_canary",
      "description": "Set up canary deployment with traffic splitting",
      "arguments": [
        {"name": "route", "required": true},
        {"name": "new_backend", "required": true},
        {"name": "percentage", "required": true}
      ]
    }
  ]
}
```

---

## 18. Plugin System

### 18.1 Plugin Interface

```go
type Plugin interface {
    // Metadata
    Name() string
    Version() string
    Description() string
    
    // Lifecycle
    Init(api PluginAPI) error
    Start() error
    Stop() error
}

type PluginAPI interface {
    // Register middleware
    RegisterMiddleware(name string, factory MiddlewareFactory) error
    
    // Register balancer algorithm
    RegisterBalancer(name string, factory BalancerFactory) error
    
    // Register health check type
    RegisterHealthCheck(name string, factory HealthCheckFactory) error
    
    // Register service discovery provider
    RegisterDiscovery(name string, factory DiscoveryFactory) error
    
    // Access metrics
    Metrics() *metrics.Engine
    
    // Access logger
    Logger() *logging.Logger
    
    // Access config
    Config() *config.Config
    
    // Register admin API routes
    RegisterAdminRoute(method, path string, handler http.Handler) error
    
    // Subscribe to events
    OnEvent(event string, handler EventHandler) error
}
```

### 18.2 Plugin Loading

Two mechanisms:
1. **Go plugins** (`plugin.Open`): Shared objects (.so) loaded at runtime
2. **WASM plugins** (future): WebAssembly modules for sandboxed execution

### 18.3 Events

Plugins can subscribe to:
- `backend.up`, `backend.down`, `backend.drain`
- `route.add`, `route.remove`, `route.update`
- `config.reload`
- `cluster.leader_change`, `cluster.node_join`, `cluster.node_leave`
- `request.complete` (high-frequency, opt-in)
- `error` (any error event)

---

## 19. Security

### 19.1 Transport Security

- TLS 1.2+ by default (1.3 preferred)
- Strong cipher suites only
- HSTS headers (configurable)
- OCSP stapling
- Certificate pinning support
- mTLS for client authentication
- mTLS between cluster nodes

### 19.2 Admin API Security

- Bind to localhost only by default
- Authentication required (basic, token, or mTLS)
- RBAC (future): Read-only vs admin roles
- Rate limiting on admin API
- Audit log for all admin actions

### 19.3 Operational Security

- No shell exec (except health check exec mode)
- File permissions checks on config and certs
- Secrets can be loaded from env vars (not in config file)
- PID file with permission checks
- Privilege dropping (start as root for port 80/443, drop to olb user)

### 19.4 Request Security (Middleware)

- IP allow/deny lists with CIDR support
- Rate limiting (multiple algorithms)
- Basic WAF (SQL injection, XSS, path traversal)
- Request size limits
- Header size limits
- Slow loris protection (configurable timeouts)
- HTTP request smuggling prevention

---

## 20. Performance Targets

### 20.1 Benchmarks

| Metric | Target | Context |
|--------|--------|---------|
| HTTP RPS (L7, single core) | > 50,000 | Small request/response |
| HTTP RPS (L7, 8 cores) | > 300,000 | Small request/response |
| TCP throughput (L4) | > 10 Gbps | With splice() |
| Latency overhead (L7 proxy) | < 1ms p99 | Excludes backend |
| Latency overhead (L4 proxy) | < 0.1ms p99 | Excludes backend |
| Memory per connection | < 4KB | Idle connection |
| Memory per active HTTP req | < 32KB | Including buffers |
| Max concurrent connections | > 100,000 | On 8GB RAM |
| Config reload time | < 50ms | Full config swap |
| Startup time | < 500ms | Cold start to ready |
| Binary size | < 20MB | Linux amd64 |
| Goroutines at idle | < 50 | No traffic |

### 20.2 Optimization Strategies

- **Buffer pooling**: `sync.Pool` for all temporary buffers
- **Zero-copy I/O**: Linux `splice()` for L4, `sendfile()` for static files
- **Lock-free metrics**: Atomic operations, no mutexes on hot path
- **Connection reuse**: Aggressive HTTP keep-alive and connection pooling
- **Memory-mapped config**: Fast config access without parsing
- **Radix trie routing**: O(k) path matching where k = path length
- **Object pooling**: Reuse request/response objects
- **Batch operations**: Batch metric writes, batch log writes
- **SIMD-friendly data structures**: Where applicable

---

## 21. Testing Strategy

### 21.1 Unit Tests

Every package has comprehensive unit tests:
- Config parsers: All three formats + edge cases
- Balancer algorithms: Distribution verification
- Middleware: Each middleware in isolation
- Router: Pattern matching correctness
- Health checker: State transitions
- Metrics: Counter/gauge/histogram accuracy
- Cluster: Raft log, state machine, election

### 21.2 Integration Tests

- Full proxy chain: listener → middleware → router → balancer → backend
- TLS termination end-to-end
- Health check lifecycle (healthy → unhealthy → recovery)
- Hot reload correctness
- Admin API CRUD operations
- WebSocket proxying
- gRPC proxying

### 21.3 End-to-End Tests

- Multi-node cluster formation and failover
- ACME certificate issuance (against pebble/step-ca)
- Real HTTP client → OLB → backend
- Load test with concurrent connections
- Graceful shutdown with in-flight requests

### 21.4 Benchmark Tests

- `go test -bench` for all hot-path functions
- `wrk` / `hey` load tests for HTTP proxy
- `iperf3` for TCP proxy throughput
- Memory profiling under load
- CPU profiling under load
- Latency percentiles under varying load

### 21.5 Fuzz Tests

- Config parser fuzzing (YAML, TOML, HCL)
- HTTP request parsing
- TLS ClientHello parsing
- Route pattern matching

---

## 22. Release Phases

### Phase 1: Core L7 Proxy (v0.1.0) — MVP
- [x] Project structure setup
- [ ] Config system (YAML + JSON + env overlay)
- [ ] Structured logger
- [ ] HTTP/HTTPS listener
- [ ] Basic reverse proxy (net/http based)
- [ ] Backend pool with health checks (HTTP, TCP)
- [ ] Round Robin + Weighted Round Robin balancers
- [ ] Basic middleware: rate limiter, CORS, headers, compression
- [ ] TLS termination with static certificates
- [ ] Admin API (REST)
- [ ] CLI: start, stop, reload, status, backend list
- [ ] Prometheus metrics export
- [ ] Unit + integration tests

### Phase 2: Advanced L7 + L4 (v0.2.0)
- [ ] All balancer algorithms (Least Conn, IP Hash, P2C, Consistent Hash, Maglev)
- [ ] WebSocket proxying
- [ ] gRPC proxying
- [ ] SSE proxying
- [ ] HTTP/2 support
- [ ] TCP proxy (L4)
- [ ] SNI-based routing
- [ ] PROXY protocol v1/v2
- [ ] UDP proxy
- [ ] Circuit breaker middleware
- [ ] Retry middleware with backoff
- [ ] Response caching
- [ ] Basic WAF
- [ ] Passive health checking
- [ ] Session affinity (cookie, header)
- [ ] TOML + HCL parser
- [ ] Config hot reload

### Phase 3: Web UI + Advanced Features (v0.3.0)
- [ ] Web UI dashboard (embedded SPA)
- [ ] Real-time metrics (WebSocket)
- [ ] Log viewer
- [ ] Config editor
- [ ] ACME / Let's Encrypt client
- [ ] OCSP stapling
- [ ] mTLS support
- [ ] `olb top` TUI dashboard
- [ ] Service discovery (DNS, file, Docker)
- [ ] Advanced CLI commands
- [ ] Shell completions

### Phase 4: Multi-Node Clustering (v0.4.0)
- [ ] Gossip protocol (SWIM)
- [ ] Raft consensus (from scratch)
- [ ] Distributed config store
- [ ] Distributed rate limiting (CRDT)
- [ ] Cluster health monitoring
- [ ] Leader election
- [ ] Inter-node mTLS
- [ ] Cluster Web UI page

### Phase 5: AI Integration + Polish (v1.0.0)
- [ ] MCP server (stdio + HTTP transport)
- [ ] MCP tools (query, modify, diagnose)
- [ ] MCP resources + prompt templates
- [ ] Plugin system (Go plugins)
- [ ] Full documentation
- [ ] Performance optimization pass
- [ ] Security audit
- [ ] Comprehensive benchmark suite
- [ ] Docker images
- [ ] Homebrew formula
- [ ] systemd service file
- [ ] llms.txt

---

## 23. API Reference

### 23.1 Admin REST API

Base URL: `http://localhost:9090/api/v1`

#### System
```
GET    /api/v1/system/info           # Version, uptime, status
GET    /api/v1/system/health         # Health check (for LB-of-LBs)
POST   /api/v1/system/reload         # Trigger config reload
POST   /api/v1/system/drain          # Start graceful drain
```

#### Config
```
GET    /api/v1/config                # Get current config
PUT    /api/v1/config                # Update full config
PATCH  /api/v1/config                # Partial config update
POST   /api/v1/config/validate       # Validate config without applying
GET    /api/v1/config/diff           # Diff current vs file
```

#### Backends
```
GET    /api/v1/backends                         # List all pools
GET    /api/v1/backends/:pool                   # Get pool details
GET    /api/v1/backends/:pool/:backend          # Get backend details
POST   /api/v1/backends/:pool                   # Add backend to pool
DELETE /api/v1/backends/:pool/:backend          # Remove backend
PATCH  /api/v1/backends/:pool/:backend          # Update backend (weight, state)
POST   /api/v1/backends/:pool/:backend/drain    # Drain backend
POST   /api/v1/backends/:pool/:backend/enable   # Enable backend
POST   /api/v1/backends/:pool/:backend/disable  # Disable backend
```

#### Routes
```
GET    /api/v1/routes                # List all routes
GET    /api/v1/routes/:name          # Get route details
POST   /api/v1/routes                # Add route
PUT    /api/v1/routes/:name          # Update route
DELETE /api/v1/routes/:name          # Remove route
POST   /api/v1/routes/test           # Test route matching
```

#### Health
```
GET    /api/v1/health                             # All health status
GET    /api/v1/health/:pool                       # Pool health
GET    /api/v1/health/:pool/:backend              # Backend health
POST   /api/v1/health/:pool/:backend/check        # Trigger check
```

#### Metrics
```
GET    /api/v1/metrics                             # All metrics
GET    /api/v1/metrics/summary                     # Dashboard summary
GET    /api/v1/metrics/timeseries                  # Time series data
GET    /api/v1/metrics/backends                    # Backend metrics
GET    /api/v1/metrics/routes                      # Route metrics
GET    /metrics                                    # Prometheus format
```

#### Certificates
```
GET    /api/v1/certs                               # List certificates
GET    /api/v1/certs/:domain                       # Certificate details
POST   /api/v1/certs                               # Add certificate
DELETE /api/v1/certs/:domain                       # Remove certificate
POST   /api/v1/certs/:domain/renew                 # Force renewal
```

#### Cluster
```
GET    /api/v1/cluster/status                      # Cluster status
GET    /api/v1/cluster/members                     # Member list
POST   /api/v1/cluster/join                        # Join cluster
POST   /api/v1/cluster/leave                       # Leave cluster
GET    /api/v1/cluster/raft                        # Raft state
```

#### WebSocket
```
WS     /api/v1/ws/metrics              # Real-time metrics stream
WS     /api/v1/ws/logs                 # Real-time log stream
WS     /api/v1/ws/events               # System events stream
WS     /api/v1/ws/health               # Health status stream
```

### 23.2 API Authentication

All admin API requests require authentication:
```http
# Basic auth
Authorization: Basic base64(username:password)

# Token auth
Authorization: Bearer <token>

# Query parameter (for WebSocket)
?token=<token>
```

---

## 24. Configuration Reference

### 24.1 All Configuration Options

See section 11.2 for the complete configuration structure.

### 24.2 Minimal Configuration

```yaml
# Minimum viable config — 5 lines
listeners:
  - address: ":80"
    protocol: http

backends:
  - name: app
    backends:
      - address: localhost:8080

routes:
  - name: default
    action:
      backend: app
```

### 24.3 TOML Equivalent

```toml
[[listeners]]
address = ":80"
protocol = "http"

[[backends]]
name = "app"

  [[backends.backends]]
  address = "localhost:8080"

[[routes]]
name = "default"

  [routes.action]
  backend = "app"
```

### 24.4 HCL Equivalent

```hcl
listener "http" {
  address  = ":80"
  protocol = "http"
}

backend "app" {
  server {
    address = "localhost:8080"
  }
}

route "default" {
  action {
    backend = "app"
  }
}
```

---

## Appendix A: CLI Binary Name

The binary is named `olb` (OpenLoadBalancer):
```bash
# Install
go install github.com/openloadbalancer/olb/cmd/olb@latest

# Or download binary
curl -fsSL https://openloadbalancer.dev/install.sh | sh

# Run
olb start --config olb.yaml
```

## Appendix B: Build Tags

```bash
# Full build (default)
go build -o olb ./cmd/olb

# Minimal build (no Web UI, no WASM plugins)
go build -tags minimal -o olb ./cmd/olb

# With debug symbols
go build -gcflags "all=-N -l" -o olb ./cmd/olb
```

## Appendix C: Docker

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /olb ./cmd/olb

FROM alpine:3.19
COPY --from=builder /olb /usr/local/bin/olb
EXPOSE 80 443 9090
ENTRYPOINT ["olb", "start"]
```

## Appendix D: systemd Service

```ini
[Unit]
Description=OpenLoadBalancer
After=network.target

[Service]
Type=simple
User=olb
Group=olb
ExecStart=/usr/local/bin/olb start --config /etc/olb/olb.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

---

*This specification is the single source of truth for OpenLoadBalancer. All implementation decisions should reference this document. Changes to this spec require a version bump and changelog entry.*
