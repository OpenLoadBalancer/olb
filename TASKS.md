# OpenLoadBalancer — TASKS v1.0

> **Reference**: SPECIFICATION.md + IMPLEMENTATION.md
> **Methodology**: Each task is atomic, testable, and completable in 1-4 hours
> **Convention**: `[ ]` = todo, `[x]` = done, `[~]` = in progress, `[-]` = blocked

---

## Phase 1: Core L7 Proxy (v0.1.0) — MVP

### 1.1 Project Bootstrap
- [x] Initialize Go module (`go mod init github.com/openloadbalancer/olb`)
- [x] Create full directory structure (cmd, internal, pkg, configs, docs, test, scripts)
- [x] Create `pkg/version/version.go` with build-time injection (Version, Commit, Date)
- [x] Create `Makefile` with build, test, lint, bench, build-all targets
- [x] Create `Dockerfile` (multi-stage, alpine-based)
- [x] Create `.github/workflows/ci.yml` (test, build, lint)
- [x] Create `llms.txt` (LLM-friendly project description)
- [x] Create `README.md` (project overview, quick start, architecture)
- [x] Create `.gitignore` (Go standard + bin/ + coverage files)
- [x] Create `LICENSE` (Apache 2.0)
- [x] Create `cmd/olb/main.go` (entry point stub)

### 1.2 Core Utilities (pkg/utils)
- [x] Implement `BufferPool` — `sync.Pool` based buffer management with multiple size tiers
- [x] Write unit tests for BufferPool (get, put, size matching, oversized allocation)
- [x] Implement `RingBuffer[T]` — generic lock-free circular buffer (SPSC)
- [x] Write unit tests for RingBuffer (push, pop, full, empty, snapshot, concurrent)
- [x] Implement `LRU[K,V]` — thread-safe LRU cache with TTL support
- [x] Write unit tests for LRU (get, put, evict, TTL expiry, concurrent access)
- [x] Implement `AtomicFloat64` — atomic operations on float64
- [x] Implement `AtomicDuration` — atomic operations on time.Duration
- [x] Write unit tests for atomic helpers
- [x] Implement `FastRand` — SplitMix64 pseudo-random number generator
- [x] Write unit tests for FastRand (distribution, uniqueness)
- [x] Implement `CIDRMatcher` — radix trie based IP/CIDR matching
- [x] Write unit tests for CIDRMatcher (IPv4, IPv6, CIDR ranges, edge cases)
- [x] Implement `BloomFilter` — probabilistic set membership
- [x] Write unit tests for BloomFilter (add, contains, false positive rate)
- [x] Implement IP utility functions (extractIP, isPrivate, parsePort)
- [x] Implement time/duration utility functions (parseDuration, parseByteSize)

### 1.3 Error Types (pkg/errors)
- [x] Define sentinel errors (ErrBackendNotFound, ErrPoolNotFound, ErrRouteNotFound, etc.)
- [x] Implement error wrapping helpers with context
- [x] Implement error code system (for API responses)
- [x] Write unit tests for error types and wrapping

### 1.4 Structured Logger (internal/logging)
- [x] Define `Level` type with Trace through Fatal
- [x] Implement `Logger` struct with atomic level, inherited fields, child loggers
- [x] Implement zero-alloc fast path (level check before allocation)
- [x] Implement `Field` type with string, int, float, bool, error, duration variants
- [x] Implement `JSONOutput` — manual JSON encoding (no encoding/json on hot path)
- [x] Implement `TextOutput` — human-readable format for development
- [x] Implement `MultiOutput` — fan-out to multiple outputs
- [x] Implement `RotatingFileOutput` — file rotation by size, max backups, compression
- [x] Implement SIGUSR1 handler for log file reopening
- [x] Implement `Logger.With()` for child loggers with inherited fields
- [x] Write unit tests for all log levels, outputs, rotation
- [x] Write benchmark tests comparing to stdlib log

### 1.5 Config System (internal/config)

#### 1.5.1 YAML Parser
- [x] Implement YAML lexer/scanner (tokenization)
  - [x] Handle indentation tracking (indent stack, INDENT/DEDENT tokens)
  - [x] Handle scalar values (strings, numbers, booleans, null)
  - [x] Handle quoted strings (single, double, with escape sequences)
  - [x] Handle multi-line strings (literal `|`, folded `>`)
  - [x] Handle comments (#)
  - [x] Handle flow collections (`{}`, `[]`)
  - [x] Handle anchors (`&`) and aliases (`*`)
- [x] Implement YAML parser (recursive descent, token → AST)
  - [x] Parse mappings (key: value)
  - [x] Parse sequences (- item)
  - [x] Parse nested structures (indentation-based)
  - [x] Parse flow mappings and sequences
  - [x] Resolve anchors and aliases
- [x] Implement YAML decoder (AST → Go struct via reflection)
  - [x] Handle struct field matching (case-insensitive + yaml tags)
  - [x] Handle type conversions (string→int, string→bool, string→duration, string→byteSize)
  - [x] Handle maps, slices, nested structs
  - [x] Handle pointer types and interfaces
  - [x] Handle `${ENV_VAR}` and `${ENV_VAR:-default}` substitution
- [x] Write comprehensive unit tests for YAML parser
  - [x] Test all scalar types (int, float, bool, null, string)
  - [x] Test nested mappings and sequences
  - [x] Test multi-line strings
  - [x] Test anchors/aliases
  - [x] Test flow collections
  - [x] Test edge cases (empty values, special characters, unicode)
- [x] Write fuzz tests for YAML parser
- [x] Test against the full olb.yaml example config

#### 1.5.2 JSON Parser
- [x] Use stdlib `encoding/json` (this IS stdlib)
- [x] Implement JSON → generic map adapter for config loader
- [x] Write unit tests for JSON config loading

#### 1.5.3 Config Structure
- [x] Define `Config` struct with all sections (Global, Admin, Metrics, Listeners, Backends, Routes, Cluster, MCP)
- [x] Define `DefaultConfig()` with sensible defaults for all fields
- [x] Implement `Config.Validate()` — comprehensive validation
  - [x] Validate addresses (host:port format)
  - [x] Validate durations (parseable)
  - [x] Validate algorithm names (known algorithms)
  - [x] Validate backend references in routes (exist)
  - [x] Validate TLS cert/key file accessibility
  - [x] Detect port conflicts between listeners
  - [x] Detect circular references
- [x] Write unit tests for validation (valid configs, each error case)

#### 1.5.4 Config Loader
- [x] Implement `Loader` — detect format from extension, parse, decode, validate
- [x] Implement environment variable overlay (`OLB_` prefix, `__` separator)
- [x] Implement config merge (file + env + CLI flags)
- [x] Write unit tests for loader (YAML, JSON, env overlay, merge)

#### 1.5.5 Config Watcher
- [x] Implement `FileWatcher` — polling-based file change detection
- [x] Use SHA-256 content hash to avoid false positives
- [x] Write unit tests for watcher (detect change, ignore no-change)

### 1.6 Metrics Engine (internal/metrics)
- [x] Implement `Counter` — atomic int64 counter
- [x] Implement `Gauge` — atomic float64 gauge
- [x] Implement `Histogram` — log-linear bucketed histogram with percentiles
- [x] Implement `TimeSeries` — ring buffer based time-bucketed data
- [x] Implement `Engine` — metrics registry with pre-registered fast-path metrics
- [x] Implement `CounterVec` / `GaugeVec` / `HistogramVec` — labeled metric families
- [x] Implement Prometheus exposition format handler
- [x] Implement JSON metrics API handler
- [x] Write unit tests for all metric types (correctness, concurrency safety)
- [x] Write benchmark tests (Counter.Inc, Histogram.Observe, Gauge.Set)

### 1.7 TLS Manager (internal/tls) — Basic
- [x] Implement `Manager` — certificate storage with exact + wildcard matching
- [x] Implement `GetCertificate` callback for `tls.Config`
- [x] Implement `BuildTLSConfig` — create tls.Config from config
- [x] Implement `ReloadCertificates` — hot-reload certs
- [x] Implement `LoadCertificate` — load PEM cert + key from files
- [x] Write unit tests (cert loading, SNI matching, wildcard matching, reload)

### 1.8 Backend Pool (internal/backend)
- [x] Implement `Backend` struct with atomic stats (conns, requests, errors, latency)
- [x] Implement `BackendState` state machine (Up, Down, Draining, Maintenance, Starting)
- [x] Implement `Pool` — named collection of backends with balancer
- [x] Implement `PoolManager` — manage multiple pools, lookup by name
- [x] Implement `Pool.AddBackend`, `Pool.RemoveBackend`, `Pool.DrainBackend`
- [x] Write unit tests for state transitions, stats tracking, pool operations

### 1.9 Load Balancer Algorithms (internal/balancer)
- [x] Define `Balancer` interface (Name, Next, Add, Remove, Update, Stats)
- [x] Define `RequestContext` struct (ClientIP, Method, Host, Path, Headers, SessionKey)
- [x] Implement `RoundRobin` — simple atomic counter rotation
- [x] Write unit tests for RoundRobin (distribution, empty pool, single backend)
- [x] Implement `WeightedRoundRobin` — Nginx smooth weighted algorithm
- [x] Write unit tests for WRR (weighted distribution, smooth spread, weight changes)
- [x] Implement balancer registry — name → factory lookup
- [x] Write benchmark tests for both algorithms

### 1.10 Health Checker (internal/health) — Basic
- [x] Define health check config structs (HTTP, TCP)
- [x] Implement `Checker` — orchestrator with per-backend check goroutines
- [x] Implement HTTP health check (configurable path, method, expected status, timeout)
- [x] Implement TCP health check (connection test with timeout)
- [x] Implement state transition logic (consecutive OK/fail thresholds)
- [x] Write unit tests (HTTP check mock, TCP check, state transitions)

### 1.11 Connection Manager (internal/conn)
- [x] Implement `Manager` — global connection tracking, limits, per-source limits
- [x] Implement `TrackedConn` — wrapped net.Conn with metadata
- [x] Implement `Accept` — wrap conn with tracking and limit checks
- [x] Implement `Release` — remove from tracking
- [x] Implement `Drain` — wait for connections with timeout
- [x] Implement `ActiveConnections` — snapshot of all connections
- [x] Implement backend `Pool` — channel-based idle connection pool
- [x] Implement `Pool.Get`, `Pool.Put`, `Pool.Close`
- [x] Implement `PoolManager` — per-backend pool management
- [x] Write unit tests for limits, tracking, pooling, drain

### 1.12 Router (internal/router)
- [x] Implement `RadixTrie` — insert, match, with parameter and wildcard support
- [x] Write unit tests for trie (exact, prefix, param, wildcard, priority)
- [x] Implement `Router` — host-based trie selection + path matching + header/method checks
- [x] Implement route hot-reload (atomic swap)
- [x] Write unit tests for router (host matching, wildcard hosts, method filter, header filter)
- [x] Write benchmark tests for route matching

### 1.13 Middleware Pipeline (internal/middleware) — Basic Set
- [x] Implement `Chain` — middleware chain builder with priority ordering
- [x] Implement `RequestContext` — per-request state (request, response, route, backend, metrics)
- [x] Implement `ResponseWriter` wrapper — capture status code, byte count

#### Middleware: Request ID
- [x] Generate UUID v4 (from crypto/rand)
- [x] Inject X-Request-Id header (or use existing if present)
- [x] Write unit tests

#### Middleware: Real IP
- [x] Extract client IP from X-Forwarded-For, X-Real-IP, or RemoteAddr
- [x] Handle trusted proxy list
- [x] Write unit tests

#### Middleware: Rate Limiter
- [x] Implement token bucket algorithm
- [x] Implement per-key bucket management (client IP, header value, etc.)
- [x] Implement rate limit response headers (X-RateLimit-Limit, Remaining, Reset, Retry-After)
- [x] Implement cleanup goroutine for expired buckets
- [x] Write unit tests (allow, deny, burst, refill, multiple keys)

#### Middleware: CORS
- [x] Handle preflight OPTIONS requests
- [x] Set Access-Control-Allow-* headers
- [x] Configurable allowed origins, methods, headers, max-age
- [x] Write unit tests

#### Middleware: Headers
- [x] Add/remove/set request headers
- [x] Add/remove/set response headers
- [x] Security headers preset (HSTS, X-Content-Type-Options, X-Frame-Options, etc.)
- [x] Write unit tests

#### Middleware: Compression
- [x] Detect Accept-Encoding (gzip, deflate)
- [x] Compress using stdlib compress/gzip, compress/flate
- [x] Skip small responses (< configurable min size)
- [x] Skip already-compressed content types
- [x] Set Vary: Accept-Encoding
- [x] Write unit tests

#### Middleware: Access Log
- [x] Record request start time, method, path, client IP
- [x] Record response status, body size, duration
- [x] Support JSON and CLF format
- [x] Write to logger (non-blocking)
- [x] Write unit tests

#### Middleware: Metrics
- [x] Record per-request metrics (duration, status, bytes)
- [x] Record per-route and per-backend metrics
- [x] Write unit tests

### 1.14 L7 HTTP Proxy (internal/proxy/l7)
- [x] Implement `HTTPProxy` — main reverse proxy handler
- [x] Implement `ServeHTTP` — create context, run middleware chain
- [x] Implement `proxyRequest` — forward request to backend, stream response
- [x] Implement `prepareOutboundRequest` — add proxy headers, rewrite path
- [x] Implement hop-by-hop header stripping
- [x] Implement X-Forwarded-For, X-Real-IP, X-Forwarded-Proto, X-Forwarded-Host
- [x] Implement request/response body streaming (chunked transfer)
- [x] Implement error handling (backend down, timeout, connection refused)
- [x] Write unit tests with httptest server as backend
- [x] Write integration test (full proxy chain)
- [x] Write benchmark tests (small payload, large payload)

### 1.15 HTTP Listener (internal/listener)
- [x] Implement `Listener` interface (Start, Stop, Name, Address)
- [x] Implement `HTTPListener` — wraps net/http.Server
- [x] Implement `HTTPSListener` — HTTPListener with TLS config
- [x] Implement graceful shutdown (http.Server.Shutdown)
- [x] Implement ACME challenge handler integration point
- [x] Write unit tests

### 1.16 Admin API (internal/admin) — Basic
- [x] Implement admin HTTP server with auth middleware
- [x] Implement `GET /api/v1/system/info` (version, uptime, state)
- [x] Implement `GET /api/v1/system/health` (self health check)
- [x] Implement `POST /api/v1/system/reload` (trigger config reload)
- [x] Implement `GET /api/v1/backends` (list all pools)
- [x] Implement `GET /api/v1/backends/:pool` (pool detail)
- [x] Implement `POST /api/v1/backends/:pool` (add backend)
- [x] Implement `DELETE /api/v1/backends/:pool/:backend` (remove backend)
- [x] Implement `POST /api/v1/backends/:pool/:backend/drain` (drain)
- [x] Implement `GET /api/v1/routes` (list routes)
- [x] Implement `GET /api/v1/health` (all health status)
- [x] Implement `GET /api/v1/metrics` (JSON metrics)
- [x] Implement `GET /metrics` (Prometheus format)
- [x] Implement basic auth middleware (username + bcrypt password)
- [x] Implement bearer token auth middleware
- [x] Write unit tests for each endpoint
- [x] Write integration test for API flows

### 1.17 CLI (internal/cli) — Basic Commands
- [x] Implement argument parser (commands, subcommands, flags)
- [x] Implement output formatters (table, JSON)
- [x] Implement `olb start` — load config, start engine
  - [x] `--config` flag (default: olb.yaml)
  - [x] `--daemon` flag (background)
  - [x] `--pid-file` flag
- [x] Implement `olb stop` — send SIGTERM to running process
- [x] Implement `olb reload` — send SIGHUP or call admin API
- [x] Implement `olb status` — query admin API for system info
- [x] Implement `olb version` — print version info
- [x] Implement `olb config validate` — validate config without starting
- [x] Implement `olb backend list` — query admin API
- [x] Implement `olb health show` — query admin API
- [x] Write unit tests for parser, formatters

### 1.18 Engine Orchestrator (internal/engine)
- [x] Implement `Engine` struct — central coordinator
- [x] Implement `New(cfg)` — initialize all components in correct order
- [x] Implement `Start()` — start all components, install signal handlers
- [x] Implement `Shutdown(ctx)` — graceful drain + stop
- [x] Implement `Reload()` — hot reload config
- [x] Implement signal handler (SIGHUP→reload, SIGTERM/SIGINT→shutdown, SIGUSR1→reopen logs)
- [x] Write integration test (start, send request, shutdown)

### 1.19 cmd/olb Entry Point
- [x] Parse CLI args
- [x] Route to appropriate command
- [x] Handle `start` command: load config → create engine → start → wait
- [x] Write end-to-end test (binary starts, accepts HTTP, proxies to backend)

### 1.20 Phase 1 Polish
- [x] Write example configs (olb.yaml, olb.minimal.yaml)
- [x] Write getting-started.md
- [x] Run full test suite with race detector (CI job on Linux)
- [x] Run benchmarks, document baseline numbers
- [x] Binary size check (9.1MB — target <20MB)
- [x] Startup time check
- [x] Tag v0.1.0

---

## Phase 2: Advanced L7 + L4 (v0.2.0)

### 2.1 Additional Balancer Algorithms
- [x] Implement `LeastConnections` with weighted variant
- [x] Implement `LeastResponseTime` with sliding window
- [x] Implement `IPHash` (FNV-1a based)
- [x] Implement `ConsistentHash` (Ketama with virtual nodes)
- [x] Implement `Maglev` (Google Maglev hashing, prime table 65537)
- [x] Implement `PowerOfTwo` (P2C random choices)
- [x] Implement `Random` and `WeightedRandom`
- [x] Implement `RingHash` with configurable virtual nodes
- [x] Write unit tests for each (distribution, backend add/remove stability)
- [x] Write benchmark tests for all algorithms

### 2.2 Session Affinity
- [x] Implement cookie-based sticky sessions (set/read OLB_SRV cookie)
- [x] Implement header-based session affinity
- [x] Implement URL parameter-based session affinity
- [x] Configurable per-route
- [x] Write unit tests

### 2.3 WebSocket Proxying
- [x] Implement WebSocket upgrade detection
- [x] Implement connection hijacking (client side)
- [x] Implement upgrade forwarding to backend
- [x] Implement bidirectional frame copy
- [x] Implement ping/pong keepalive
- [x] Implement idle timeout
- [x] Write unit tests with gorilla/websocket test client (test only dep)

### 2.4 gRPC Proxying
- [x] Implement HTTP/2 h2c (prior knowledge) support
- [x] Implement trailer header propagation
- [x] Implement streaming body forwarding
- [x] Write unit tests

### 2.5 SSE Proxying
- [x] Detect Accept: text/event-stream
- [x] Disable response buffering for SSE routes
- [x] Implement flush-after-each-event
- [x] Write unit tests

### 2.6 HTTP/2 Support
- [x] Integrate `golang.org/x/net/http2` (or implement minimal h2)
- [x] HTTP/2 connection multiplexing to backends
- [x] ALPN negotiation (h2, http/1.1)
- [x] Write unit tests

### 2.7 TCP Proxy (L4)
- [x] Implement `TCPProxy` — accept, select backend, bidirectional copy
- [x] Implement `TCPListener` — raw TCP listener
- [x] Implement zero-copy splice on Linux (+build linux)
- [x] Implement fallback io.CopyBuffer on non-Linux
- [x] Write unit tests (proxy MySQL-like protocol, large data transfer)
- [x] Write benchmark tests (throughput, latency)

### 2.8 SNI-Based Routing
- [x] Implement TLS ClientHello peek (extract SNI without consuming)
- [x] Implement SNI → backend mapping
- [x] Implement TLS passthrough mode (no termination)
- [x] Write unit tests (SNI extraction, routing)

### 2.9 PROXY Protocol
- [x] Implement PROXY protocol v1 parser (text format)
- [x] Implement PROXY protocol v2 parser (binary format)
- [x] Implement PROXY protocol v1 writer
- [x] Implement PROXY protocol v2 writer
- [x] Configurable per-listener (send/receive)
- [x] Write unit tests

### 2.10 UDP Proxy
- [x] Implement `UDPProxy` — session tracking by source addr
- [x] Implement `UDPListener`
- [x] Implement session timeout and cleanup
- [x] Write unit tests (DNS proxy simulation)

### 2.11 Circuit Breaker Middleware
- [x] Implement state machine (Closed → Open → Half-Open)
- [x] Configurable error threshold, open duration, half-open requests
- [x] Per-backend circuit breakers
- [x] Write unit tests (state transitions, recovery)

### 2.12 Retry Middleware
- [x] Implement retry with configurable max retries
- [x] Implement exponential backoff with jitter
- [x] Configurable retry-on status codes
- [x] Only retry idempotent methods by default
- [x] Write unit tests

### 2.13 Response Cache Middleware
- [x] Implement LRU-based HTTP response cache
- [x] Cache key generation (method + host + path + query)
- [x] Respect Cache-Control headers
- [x] Configurable max size, TTL
- [x] Stale-while-revalidate support
- [x] Write unit tests

### 2.14 Basic WAF Middleware
- [x] Implement SQL injection pattern detection
- [x] Implement XSS pattern detection
- [x] Implement path traversal detection
- [x] Implement command injection detection
- [x] Configurable block vs log-only mode
- [x] Write unit tests

### 2.15 IP Filter Middleware
- [x] Implement allow/deny lists using CIDRMatcher
- [x] Configurable per-route
- [x] Write unit tests

### 2.16 Passive Health Checking
- [x] Track error rates per backend from real traffic
- [x] Sliding window counter
- [x] Configurable error rate threshold
- [x] Auto-disable backend on high error rate
- [x] Auto-recovery after cooldown
- [x] Write unit tests

### 2.17 TOML Parser
- [x] Implement TOML v1.0 lexer
- [x] Implement TOML parser (tables, arrays of tables, inline tables)
- [x] Implement all value types (string, int, float, bool, datetime, array)
- [x] Write unit tests + fuzz tests
- [x] Test against olb.toml example config

### 2.18 HCL Parser
- [x] Implement HCL lexer
- [x] Implement HCL parser (blocks, attributes, expressions)
- [x] Implement string interpolation (${...})
- [x] Implement here-doc strings
- [x] Write unit tests + fuzz tests
- [x] Test against olb.hcl example config

### 2.19 Config Hot Reload
- [x] Implement atomic config swap
- [x] Implement config diff computation and logging
- [x] Test hot reload of routes, backends, TLS certs, middleware
- [x] Write integration test (change config, verify new behavior)

### 2.20 Phase 2 Polish
- [x] Write example configs in TOML and HCL
- [x] Update documentation
- [x] Full test suite + benchmarks
- [x] Tag v0.2.0

---

## Phase 3: Web UI + Advanced Features (v0.3.0)

### 3.1 Web UI — Foundation
- [x] Create vanilla JS SPA framework (router, state, rendering)
- [x] Create CSS design system (variables, dark/light theme, grid, components)
- [x] Create reusable UI components (table, card, badge, button, form, modal)
- [x] Create navigation (sidebar, breadcrumbs)
- [x] Implement WebSocket client with auto-reconnect
- [x] Implement `go:embed` for static assets
- [x] Bundle and minify (or keep simple, no build step)

### 3.2 Web UI — Dashboard Page
- [x] Live request rate sparkline
- [x] Active connections gauge
- [x] Error rate display
- [x] Backend health grid
- [x] Top routes by traffic
- [x] Latency histogram
- [x] System resources (CPU, memory, goroutines)
- [x] Recent errors list
- [x] Uptime, version display

### 3.3 Web UI — Backends Page
- [x] Backend pool table with status, connections, RPS, latency
- [x] Per-backend detail view
- [x] Actions: drain, enable, disable, remove
- [x] Add backend form
- [x] Real-time health check results

### 3.4 Web UI — Routes Page
- [x] Route table with match criteria
- [x] Per-route metrics (RPS, latency p50/p95/p99, error rate)
- [x] Route testing tool

### 3.5 Web UI — Metrics Page
- [x] Interactive time-range charts
- [x] Metric explorer with search/filter
- [x] Export to JSON/CSV

### 3.6 Web UI — Logs Page
- [x] Real-time log stream via WebSocket
- [x] Full-text search
- [x] Filter by level, route, backend, status
- [x] Log entry detail view

### 3.7 Web UI — Config Page
- [x] Current config viewer (syntax highlighted)
- [x] Config diff view
- [x] Reload button with confirmation

### 3.8 Web UI — Certificates Page
- [x] Certificate inventory table
- [x] Expiry warnings
- [x] ACME status
- [x] Force renewal button

### 3.9 Custom Chart Library
- [x] Implement line chart with smooth curves
- [x] Implement sparkline (minimal line, no axes)
- [x] Implement bar chart (vertical, stacked)
- [x] Implement gauge (semicircle)
- [x] Implement histogram visualization
- [x] Tooltip on hover
- [x] Responsive canvas sizing

### 3.10 ACME Client (Let's Encrypt)
- [x] Implement ACME v2 directory discovery
- [x] Implement account registration (ECDSA P-256)
- [x] Implement JWS signing for ACME requests
- [x] Implement nonce management
- [x] Implement order creation
- [x] Implement HTTP-01 challenge solver
- [x] Implement TLS-ALPN-01 challenge solver
- [x] Implement CSR generation and order finalization
- [x] Implement certificate download and parsing
- [x] Implement certificate storage (PEM files)
- [x] Implement auto-renewal (background goroutine, 30 days before expiry)
- [x] Write unit tests (against Pebble or mock)
- [x] Write integration test (full issuance flow)

### 3.11 OCSP Stapling
- [x] Implement OCSP response fetching from CA
- [x] Implement OCSP response caching
- [x] Implement stapling into TLS handshake
- [x] Implement background refresh
- [x] Write unit tests

### 3.12 mTLS Support
- [x] Implement client certificate validation
- [x] Implement CA cert pool loading
- [x] Implement upstream mTLS (OLB → backend)
- [x] Configurable per-listener and per-backend
- [x] Write unit tests

### 3.13 `olb top` TUI Dashboard
- [x] Implement TUI engine (raw terminal, ANSI escape codes)
- [x] Implement box drawing, progress bars, tables
- [x] Implement color support
- [x] Implement keyboard input handling
- [x] Implement live metrics display
- [x] Implement backend status view
- [x] Implement route metrics view
- [x] Implement key shortcuts ([q] quit, [b] backends, [r] routes, etc.)

### 3.14 Service Discovery
- [x] Implement Discovery interface
- [x] Implement static (config-based) provider
- [x] Implement DNS SRV provider
- [x] Implement DNS A/AAAA provider
- [x] Implement file-based provider (watch JSON/YAML file)
- [x] Implement Docker provider (unix socket, label-based)
- [x] Write unit tests for each provider

### 3.15 Advanced CLI Commands
- [x] Implement `olb backend add/remove/drain/enable/disable/stats`
- [x] Implement `olb route add/remove/test`
- [x] Implement `olb cert list/add/remove/renew/info`
- [x] Implement `olb metrics show/export`
- [x] Implement `olb log tail/search`
- [x] Implement `olb config show/diff/generate`
- [x] Implement shell completions (bash, zsh, fish)

### 3.16 Phase 3 Polish
- [x] Responsive Web UI test (mobile, tablet)
- [x] Web UI accessibility (ARIA labels, keyboard nav)
- [x] Web UI bundle size check (441KB — target <2MB)
- [x] Full test suite
- [x] Tag v0.3.0

---

## Phase 4: Multi-Node Clustering (v0.4.0)

### 4.1 Gossip Protocol (SWIM)
- [x] Implement UDP message serialization (binary format)
- [x] Implement PING / ACK / PING-REQ message handlers
- [x] Implement probe loop with random member selection
- [x] Implement indirect probe (PING-REQ via random members)
- [x] Implement SUSPECT / ALIVE / DEAD state transitions
- [x] Implement incarnation numbers for state precedence
- [x] Implement piggybacked broadcast queue
- [x] Implement member join / leave handling
- [x] Implement TCP fallback for large messages
- [x] Write unit tests (membership, failure detection, state propagation)
- [x] Write integration test (3-node cluster, kill one, detect failure)

### 4.2 Raft Consensus
- [x] Implement Raft log (append, get, truncate, compact)
- [x] Implement persistent state storage (term, votedFor, log)
- [x] Implement RequestVote RPC (handler + sender)
- [x] Implement AppendEntries RPC (handler + sender)
- [x] Implement leader election with randomized timeout
- [x] Implement log replication
- [x] Implement commit index advancement
- [x] Implement state machine application (apply committed entries)
- [x] Implement snapshots (create, send, restore)
- [x] Implement membership changes (joint consensus)
- [x] Implement TCP transport for Raft RPCs
- [x] Write unit tests (election, replication, commit, snapshot)
- [x] Write integration test (3-node cluster, leader failure, re-election)

### 4.3 Config State Machine
- [x] Implement config store as Raft state machine
- [x] Implement config change proposal (leader only)
- [x] Implement follower → leader forwarding
- [x] Implement config apply on commit
- [x] Write unit tests

### 4.4 Distributed State
- [x] Implement health status propagation via gossip
- [x] Implement distributed rate limiting (CRDT counters)
- [x] Implement session affinity table propagation
- [x] Write unit tests

### 4.5 Inter-Node Security
- [x] Implement mTLS between cluster nodes
- [x] Implement node authentication
- [x] Write unit tests

### 4.6 Cluster Management
- [x] Implement cluster join flow
- [x] Implement cluster leave flow (graceful)
- [x] Implement `olb cluster status/join/leave/members` CLI commands
- [x] Implement cluster admin API endpoints
- [x] Add cluster page to Web UI

### 4.7 Phase 4 Polish
- [x] 3-node integration test
- [x] 5-node integration test
- [x] Network partition simulation
- [x] Split-brain protection verification
- [x] Tag v0.4.0

---

## Phase 5: AI Integration + Polish (v1.0.0)

### 5.1 MCP Server
- [x] Implement MCP JSON-RPC protocol handler
- [x] Implement stdio transport (stdin/stdout)
- [x] Implement HTTP/SSE transport
- [x] Implement `olb_query_metrics` tool
- [x] Implement `olb_list_backends` tool
- [x] Implement `olb_modify_backend` tool
- [x] Implement `olb_modify_route` tool
- [x] Implement `olb_diagnose` tool (error analysis, latency analysis, capacity)
- [x] Implement `olb_get_logs` tool
- [x] Implement `olb_get_config` tool
- [x] Implement `olb_cluster_status` tool
- [x] Implement MCP resources (metrics, config, health, logs)
- [x] Implement MCP prompt templates (diagnose, capacity planning, canary deploy)
- [x] Write unit tests for each tool
- [x] Write integration test (Claude Code ↔ MCP Server)

### 5.2 Plugin System
- [x] Implement Plugin interface
- [x] Implement PluginAPI (register middleware, balancer, health check, discovery)
- [x] Implement Go plugin loader (.so files)
- [x] Implement plugin directory scanning
- [x] Implement event system (subscribe/publish)
- [x] Write example plugin (custom middleware)
- [x] Write unit tests

### 5.3 Documentation
- [x] Write comprehensive README.md
- [x] Write getting-started.md (5-minute quick start)
- [x] Write configuration.md (all options documented)
- [x] Write algorithms.md (explain each algorithm with diagrams)
- [x] Write clustering.md (setup, operation, troubleshooting)
- [x] Write mcp.md (AI integration guide)
- [x] Write api.md (REST API reference)
- [x] Write llms.txt (LLM-friendly project summary)
- [x] Write CHANGELOG.md

### 5.4 Performance Optimization Pass
- [x] Profile CPU under load (go tool pprof)
- [x] Profile memory under load
- [x] Optimize hot path allocations (escape analysis)
- [x] Verify buffer pool effectiveness
- [x] Verify connection pool effectiveness
- [x] Benchmark: HTTP RPS (target: >50K single core, >300K 8-core)
- [x] Benchmark: TCP throughput (target: >10Gbps with splice)
- [x] Benchmark: Latency overhead (target: <1ms p99 L7, <0.1ms p99 L4)
- [x] Benchmark: Memory per connection (target: <4KB idle)
- [x] Benchmark: Startup time (target: <500ms)
- [x] Binary size check (9.1MB — target <20MB)

### 5.5 Security Audit
- [x] Review TLS configuration defaults
- [x] Review admin API authentication
- [x] Review input validation (config, API, headers)
- [x] Review WAF rule coverage
- [x] Test slow loris protection
- [x] Test request smuggling prevention
- [x] Test header injection prevention
- [x] Review privilege dropping implementation

### 5.6 Packaging & Distribution
- [x] Docker image (multi-arch: amd64, arm64)
- [x] Docker Compose example
- [x] Homebrew formula
- [x] systemd service file
- [x] DEB package
- [x] RPM package
- [x] Install script (curl | sh)
- [x] GitHub Actions release workflow

### 5.7 v1.0.0 Release
- [x] All tests pass with -race (CI job on Linux)
- [x] All benchmarks meet targets
- [x] Documentation complete
- [x] Example configs for all formats
- [~] Docker images published (GHCR push needs repo permissions)
- [x] Homebrew formula published
- [x] GitHub release with binaries
- [x] Blog post / announcement
- [x] Tag v1.0.0

---

## Task Statistics

| Phase | Tasks | Done | Remaining |
|-------|-------|------|-----------|
| Phase 1 (MVP) | ~120 | ~120 | tag only |
| Phase 2 (Advanced) | ~60 | ~60 | tag only |
| Phase 3 (Web UI) | ~55 | ~55 | tag only |
| Phase 4 (Cluster) | ~30 | ~30 | tag only |
| Phase 5 (AI+Polish) | ~40 | ~40 | release ops only |
| **Total** | **~305** | **~305** | **tags + publish** |

---

*Track progress by checking off tasks. Each phase should be tagged as a release before starting the next phase.*
