# Configuration Reference

Complete reference for all OLB configuration options.

Every example below is a complete document rooted at the top of the config
file, and every key in it is a real config field. Unknown keys are **silently
ignored** by the decoders, so a misspelled or invented key does nothing; this
document is checked against the schema by
`internal/config/docs_reference_test.go`.

## File Formats

OLB supports four configuration formats, all parsed from scratch with zero external dependencies:

| Format | Extension | Notes |
|--------|-----------|-------|
| YAML | `.yaml`, `.yml` | Recommended. Full YAML 1.2 subset. |
| JSON | `.json` | Parsed by the built-in YAML parser (JSON is a YAML subset). |
| TOML | `.toml` | TOML v1.0. Tables, arrays, inline tables. |
| HCL | `.hcl` | HashiCorp Configuration Language subset. |

Specify the config file at startup:

```bash
olb start --config /etc/olb/olb.yaml
```

OLB detects the format from the file extension (unknown extensions are parsed as YAML). If `--config` is not given, OLB loads `olb.yaml` from the current directory.

## Environment Variables

Within config files, reference environment variables using `${VAR}` or `${VAR:-default}` syntax. Substitution happens on the raw file text before parsing:

```yaml
admin:
  address: "${OLB_ADMIN_ADDR:-127.0.0.1:8081}"
  username: admin
  password: "${OLB_ADMIN_PASSWORD_HASH}"
```

There is no `OLB_SECTION__KEY` style environment overlay; to override a value from the environment, reference the variable in the file as shown above.

## Hot Reload

Configuration can be reloaded without downtime using any of these methods:

```bash
# CLI command
olb reload

# SIGHUP signal
kill -HUP $(pidof olb)

# Admin API
curl -X POST http://localhost:8081/api/v1/system/reload
```

**Hot-reloadable settings:**
- Routes (add, remove, modify)
- Pools and backends (add, remove, change weights)
- Health check parameters
- Middleware and WAF configuration
- The certificate in `tls.cert_file` / `tls.key_file`

**Requires restart:**
- Listener addresses, protocols and TLS on/off
- Admin API settings
- Cluster configuration
- Logging configuration
- `server` settings

After a reload OLB watches the new config for `server.rollback_monitor_duration` (default 30s) and reverts to the previous config if every backend becomes unhealthy.

---

## Server Settings

Process-wide limits and timeouts live under `server`. All keys are optional; the defaults are shown.

```yaml
version: "1"

server:
  max_connections: 10000             # Total max concurrent connections
  max_connections_per_source: 100    # Per client IP
  max_connections_per_backend: 1000  # Per backend server
  proxy_timeout: 60s                 # Max time for a proxied request
  dial_timeout: 10s                  # Backend connect timeout
  max_retries: 3                     # Proxy retries on another backend
  max_idle_conns: 100                # Idle upstream connections (total)
  max_idle_conns_per_host: 10        # Idle upstream connections per backend
  idle_conn_timeout: 90s             # Close idle upstream connections after this
  drain_timeout: 30s                 # Connection drain time on shutdown
  shutdown_timeout: 30s              # Overall graceful shutdown deadline
  listener_stop_timeout: 5s          # Per-listener stop deadline
  proxy_drain_window: 5s             # How long an old proxy lives after a reload
  rollback_check_interval: 15s       # Health check cadence after a reload
  rollback_monitor_duration: 30s     # How long to watch for auto-rollback
```

HTTP listener read/write/header/idle timeouts are fixed hardened defaults (slow-loris protection) and are not configurable.

### Duration Format

All timeout/interval values accept Go-style duration strings:

| Format | Meaning |
|--------|---------|
| `5s` | 5 seconds |
| `100ms` | 100 milliseconds |
| `1m30s` | 1 minute 30 seconds |
| `1h` | 1 hour |
| `24h` | 24 hours |

---

## Admin API

```yaml
admin:
  address: "127.0.0.1:8081"       # Bind address (default ":8080")

  # Basic auth: the password must be a bcrypt hash
  username: admin
  password: "$2a$10$replace.with.a.real.bcrypt.hash"

  # And/or a static bearer token
  bearer_token: "${OLB_ADMIN_TOKEN}"

  trust_proxy_headers: false      # Use X-Forwarded-For for auth rate limiting
  rate_limit_max_requests: 60     # Requests per window per client (default 60)
  rate_limit_window: 1m           # Default 1m

  # MCP server (AI integration, SSE + HTTP transports)
  mcp_address: "127.0.0.1:8082"   # Default: admin port + 1
  mcp_token: "${OLB_MCP_TOKEN}"   # Required: the MCP server only starts when set
  mcp_audit: true                 # Log every MCP tool call
```

Auth is enabled when `username` or `bearer_token` is set. Without auth, the admin server refuses to bind to a non-loopback host; note that an address without a host (such as the default `:8080`) is not rejected and listens on all interfaces, so always set an explicit host or configure auth. The Web UI is served by the admin server and has no separate settings; admin TLS is not configurable. Set `admin.enabled: false` to not start the admin server at all (default: enabled); the CLI commands and Web UI then have nothing to talk to.

---

## Metrics

Prometheus metrics are exposed by the admin server at `metrics.path` (default `/metrics`); `metrics.enabled: false` turns that endpoint off. JSON metrics are always at `/api/v1/metrics`. (`olb` CLI commands read `/metrics`, so keep the default path if you use them.) metric retention and resolution are not configurable.

```yaml
metrics:
  enabled: true
  path: /metrics
```

For per-request HTTP metrics with custom names and buckets, see the `middleware.metrics` middleware.

---

## Listeners

Listeners define frontend entry points where OLB accepts connections.

### HTTP Listener

```yaml
listeners:
  - name: http
    protocol: http                # http (default), https, tcp, udp
    address: ":80"
    routes:
      - path: /
        pool: backend

pools:
  - name: backend
    backends:
      - address: "10.0.1.10:8080"
```

To redirect HTTP to HTTPS, enable the `middleware.forcessl` middleware; there is no per-listener redirect option.

### HTTPS Listener

A listener serves TLS when `tls.enabled` is true. The certificate comes from the top-level `tls` section (see [TLS](#tls)).

```yaml
tls:
  cert_file: /etc/olb/certs/server.crt
  key_file: /etc/olb/certs/server.key

listeners:
  - name: https
    protocol: https
    address: ":443"
    tls:
      enabled: true
    routes:
      - path: /
        pool: backend

pools:
  - name: backend
    backends:
      - address: "10.0.1.10:8080"
```

`listeners[].tls.cert_file` and `key_file` are accepted and validated but not used; the certificate is always the one from the top-level `tls` section. The minimum TLS version, cipher suites and curves are fixed secure defaults (TLS 1.2+). Note that `protocol: https` alone does not turn TLS on; `tls.enabled: true` does.

### TCP Listener (L4)

TCP and UDP listeners forward to a single pool, named by `pool` (or, if unset, the first route's pool).

```yaml
listeners:
  - name: mysql
    protocol: tcp
    address: ":3306"
    pool: mysql-pool

pools:
  - name: mysql-pool
    health_check:
      type: tcp
      interval: 10s
    backends:
      - address: "10.0.2.10:3306"
```

PROXY protocol and SNI-based routing exist in the L4 proxy code but are not configurable yet.

### UDP Listener (L4)

```yaml
listeners:
  - name: dns
    protocol: udp
    address: ":53"
    pool: dns-pool

pools:
  - name: dns-pool
    health_check:
      type: tcp
      interval: 10s
    backends:
      - address: "10.0.3.10:53"
```

The UDP session timeout is not configurable.

---

## Routes

Routes match incoming HTTP requests and direct them to backend pools.

```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      # Match by host and path
      - name: api
        host: api.openloadbalancer.dev
        path: /api
        methods:
          - GET
          - POST
          - PUT
          - DELETE
        pool: api-backend

      # Match by path prefix
      - name: static
        path: /static
        pool: static-backend

      # Default catch-all
      - name: default
        path: /
        pool: web-backend

pools:
  - name: api-backend
    backends:
      - address: "10.0.1.20:8080"
  - name: static-backend
    backends:
      - address: "10.0.1.30:8080"
  - name: web-backend
    backends:
      - address: "10.0.1.10:8080"
```

`routes[].middleware` is not supported: a config that sets it is rejected at load time (silently dropping an auth or rate-limit entry would fail open). All middleware is global (see [Middleware](#middleware)).

### Route Matching Fields

| Field | Description | Example |
|-------|-------------|---------|
| `host` | Match hostname (exact or `*.` wildcard) | `api.openloadbalancer.dev`, `*.openloadbalancer.dev` |
| `path` | Match URL path by segment prefix; `:name` and `*name` parameters are supported | `/api`, `/users/:id`, `/files/*path` |
| `methods` | Match HTTP methods | `[GET, POST]` |

Routes are matched by specificity, not by order: an exact `host` beats a wildcard host, which beats routes without a host; within a host, the longest matching path prefix wins. Header matching is not supported.

### Traffic Splitting (Canary)

Weighted traffic splitting between pools is not configurable. To mirror traffic to a canary instead, see [Shadow Traffic](#shadow-traffic); to shift load inside a pool, use backend `weight` with a weighted algorithm.

---

## Backend Pools

Backend pools define groups of upstream servers and how traffic is distributed among them.

```yaml
pools:
  - name: web-backend
    algorithm: round_robin         # Default: round_robin

    backends:
      - id: web-1
        address: "10.0.1.10:8080"
        weight: 100                # Relative weight (default 100)
      - id: web-2
        address: "10.0.1.11:8080"
        weight: 100
      - id: web-3
        address: "10.0.1.12:8443"
        weight: 200                # Gets 2x traffic with weighted algorithms
        scheme: https              # "http" (default) or "https" to the backend
```

### Available Algorithms

`round_robin` (`rr`), `weighted_round_robin` (`wrr`), `least_connections` (`lc`), `weighted_least_connections` (`wlc`), `least_response_time` (`lrt`), `weighted_least_response_time` (`wlrt`), `ip_hash` (`iphash`), `consistent_hash` (`ch`, `ketama`), `maglev`, `ring_hash` (`ringhash`), `power_of_two` (`p2c`), `random`, `weighted_random` (`wrandom`), `rendezvous` (`rendezvous_hash`), `peak_ewma` (`pewma`), `sticky`

An unknown algorithm name is rejected on reload, but at startup the pool silently falls back to round robin, so check the spelling. See [algorithms.md](algorithms.md) for details on each algorithm.

### Health Checks

If a pool has no `health_check`, OLB uses an HTTP check of `/health` every 10s with a 5s timeout.

```yaml
pools:
  - name: web
    backends:
      - address: "10.0.1.10:8080"
    health_check:
      type: http                   # "http", "tcp", "grpc", or "exec"
      path: /health                # Required for http
      interval: 10s                # Check interval (default 10s)
      timeout: 5s                  # Check timeout (default 5s, must be < interval)
      healthy_threshold: 2         # Consecutive successes to mark healthy (default 2)
      unhealthy_threshold: 3       # Consecutive failures to mark unhealthy (default 3)
```

HTTP checks treat any 2xx response as healthy; the expected status code is not configurable.

TCP health check (connection-only):

```yaml
pools:
  - name: tcp-pool
    backends:
      - address: "10.0.2.10:5432"
    health_check:
      type: tcp
      interval: 10s
      timeout: 5s
```

gRPC health check (uses grpc.health.v1.Health/Check protocol):

```yaml
pools:
  - name: grpc-pool
    backends:
      - address: "10.0.4.10:50051"
    health_check:
      type: grpc
      interval: 10s
      timeout: 5s
      grpc_tls_skip_verify: false  # Skip TLS verification for gRPC checks
```

Exec health check (runs an external command; exit code 0 = healthy):

```yaml
pools:
  - name: custom-pool
    backends:
      - address: "10.0.5.10:9000"
    health_check:
      type: exec
      command: /usr/local/bin/check-backend
      args:
        - "{{.Host}}"
        - "{{.Port}}"
      interval: 10s
      timeout: 5s
```

Template variables available in `command` and `args`:

| Variable | Description |
|----------|-------------|
| `{{.Address}}` | Full backend address (host:port) |
| `{{.Host}}` | Host portion of the address |
| `{{.Port}}` | Port portion of the address |

### Connection Pool Settings

There are no per-pool connection settings. Upstream connection pooling and timeouts are set globally under `server` (`max_idle_conns`, `max_idle_conns_per_host`, `idle_conn_timeout`, `dial_timeout`, `max_connections_per_backend`); see [Server Settings](#server-settings).

### Session Affinity

Session affinity is enabled with `algorithm: sticky`. It wraps round robin and pins a client to a backend with a cookie named `OLB_SRV` (Secure, HttpOnly, SameSite=Lax; sessions expire after 1h). The cookie name, mode and TTL are not configurable.

```yaml
pools:
  - name: app
    algorithm: sticky
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
```

---

## Middleware

Middleware is configured globally under `middleware`, one block per middleware, each with its own `enabled` switch (all are off by default). Order of execution is fixed by the engine, not by the order in the file. If an enabled middleware's config is invalid, the engine refuses to start (or rejects the reload) rather than running without it.

### Global Middleware

```yaml
middleware:
  request_id:
    enabled: true
    header: X-Request-ID           # Default X-Request-ID
    generate: true                 # Generate an ID when the request has none
    response: true                 # Echo the ID in the response
    length: 16                     # Random bytes (default 16)

  real_ip:
    enabled: true
    headers:                       # Headers to read the client IP from (default: the common proxy headers)
      - X-Forwarded-For
      - X-Real-IP
    trusted_proxies:               # Default: private ranges + loopback
      - "10.0.0.0/8"
      - "172.16.0.0/12"
      - "192.168.0.0/16"
    reject_untrusted: false
```

`generate` and `response` default to `true`; set one to `false` to turn it off.

### Rate Limiting

Token bucket per client IP (the connection's remote address).

```yaml
middleware:
  rate_limit:
    enabled: true
    requests_per_second: 100       # Default 100
    burst_size: 200                # Default 200
```

The rate-limit key is not configurable. For per-path, per-header or global limits, use WAF rate-limit rules (`waf.rate_limit.rules[].scope`).

### CORS

```yaml
middleware:
  cors:
    enabled: true
    allowed_origins:               # Default ["*"]
      - "https://openloadbalancer.dev"
    allowed_methods:               # Default GET, POST, PUT, DELETE, OPTIONS
      - GET
      - POST
      - PUT
      - DELETE
      - OPTIONS
    allowed_headers:
      - Content-Type
      - Authorization
    allow_credentials: false
    max_age: 3600                  # Seconds
```

### Compression

```yaml
middleware:
  compression:
    enabled: true
    min_size: 1024                 # Minimum response size to compress (bytes)
    level: 6                       # gzip level, -1..9 (0 = library default)
```

### Security Headers

```yaml
middleware:
  secure_headers:
    enabled: true
    x_frame_options: DENY
    x_content_type_options: true
    referrer_policy: strict-origin-when-cross-origin
    content_security_policy: "default-src 'self'"
    strict_transport_policy:
      enabled: true                # Set to false to turn HSTS off
      max_age: 31536000
      include_subdomains: true
      preload: false
```

Other `secure_headers` keys: `x_xss_protection`, `x_permitted_cross_domain_policies`, `x_download_options`, `x_dns_prefetch_control`, `permissions_policy`, `cross_origin_embedder_policy`, `cross_origin_opener_policy`, `cross_origin_resource_policy`, `cache_control`, `exclude_paths`. For a full Content-Security-Policy builder, use `middleware.csp`. To add arbitrary headers, use `middleware.headers` (`request_add`, `response_add`).

### Circuit Breaker

```yaml
middleware:
  circuit_breaker:
    enabled: true
    error_threshold: 5             # Errors to open the circuit (default 5)
    error_rate_threshold: 0.5      # Error ratio 0.0-1.0 that opens it (default 0.5)
    open_duration: 30s             # How long it stays open before probing (default 30s)
```

The window (60s) and the number of half-open probes (3) are fixed. An out-of-range `error_rate_threshold` or an unparsable `open_duration` makes the engine refuse to start.

### Retry

```yaml
middleware:
  retry:
    enabled: true
    max_retries: 3
    retry_on: [502, 503, 504]      # Status codes that trigger a retry (default 502/503/504)
```

Retries apply to GET, HEAD and OPTIONS with exponential backoff (100ms to 5s, with jitter).

### Response Cache

```yaml
middleware:
  cache:
    enabled: true
    max_size: 104857600            # 100MB
    max_entries: 10000
    default_ttl: 5m                # Default 5m
    methods:                       # Default GET, HEAD
      - GET
      - HEAD
    status_codes:
      - 200
      - 301
      - 302
    vary_headers:
      - Accept-Encoding
    exclude_paths:
      - /api/private
    cache_private: false
    cache_cookies: false
```

### IP Filter

Deny list is checked first, then the allow list, then `default_action`.

```yaml
middleware:
  ip_filter:
    enabled: true
    allow_list:
      - "10.0.0.0/8"
      - "192.168.1.0/24"
    deny_list:
      - "10.0.66.0/24"
    default_action: deny           # "allow" (default) or "deny"
```

At least one of `allow_list` or `deny_list` is required when enabled.

### Authentication

```yaml
middleware:
  basic_auth:
    enabled: true
    realm: "OLB"
    hash: bcrypt                   # "sha256" (default) or "bcrypt"
    users:
      admin: "$2a$10$replace.with.a.real.bcrypt.hash"
    exclude_paths:
      - /health

  api_key:
    enabled: true
    header: X-API-Key              # Default X-API-Key
    hash: sha256                   # "sha256" (default) or "plain"
    keys:
      ci-runner: "${OLB_CI_API_KEY}"

  jwt:
    enabled: true
    secret: "${OLB_JWT_SECRET}"    # Or public_key (EdDSA: file path or base64)
    algorithm: HS256               # HS256 (default), HS384, HS512, EdDSA
    header: Authorization          # Default Authorization
    prefix: "Bearer "              # Default "Bearer "
    required: true
    claims_validation:
      issuer: "https://auth.openloadbalancer.dev"
      audience: "olb"
    exclude_paths:
      - /health
```

With `hash: sha256` (the default for both `basic_auth` and `api_key`), the configured value is the plaintext password or key; OLB keeps only an HMAC-SHA256 of it in memory. With `hash: bcrypt`, `basic_auth` user values must be bcrypt hashes. `hash: plain` stores passwords as written in the config file and is refused unless `allow_plaintext: true` is also set; prefer `sha256` or `bcrypt`.

Other auth middleware: `oauth2` (`issuer_url` or `jwks_url`, `client_id`, `client_secret`, `audience`, `scopes`, `introspection_url`, `cache_duration`, `header`, `prefix`, `exclude_paths`) and `hmac` (`secret`, `algorithm` sha256/sha512, `header`, `prefix`, `encoding`, `use_body`, `timestamp_header`, `max_age`, `exclude_paths`).

### Other Middleware

| Key | Purpose | Main fields |
|-----|---------|-------------|
| `timeout` | Per-request timeout | `timeout` (default 60s) |
| `max_body_size` | Request body limit | `max_size` bytes (default 10MB) |
| `strip_prefix` | Remove a path prefix | `prefix`, `redirect_slash` |
| `rewrite` | Regex URL rewrite / redirect | `rules[]` (`pattern`, `replacement`, `flag`), `exclude_paths` |
| `forcessl` | Redirect HTTP to HTTPS | `permanent`, `port`, `exclude_paths`, `exclude_hosts`, `header_key`, `header_value` |
| `csrf` | CSRF token check | `cookie_name`, `header_name`, `field_name`, `cookie_*`, `token_length`, `exclude_*` |
| `csp` | Content-Security-Policy builder | `default_src`, `script_src`, ..., `nonce_script`, `report_uri` |
| `headers` | Add request/response headers | `request_add`, `response_add` |
| `coalesce` | Collapse identical concurrent requests | `ttl` (default 100ms), `max_requests`, `exclude_paths` |
| `bot_detection` | User-agent / header / rate bot rules | `action`, `block_known_bots`, `user_agent_rules`, `custom_headers`, ... |
| `validator` | Request/response validation | `content_types`, `required_headers`, `query_rules`, `path_patterns`, ... |
| `transformer` | Response header/body transform | `add_headers`, `remove_headers`, `rewrite_body`, `compress`, ... |
| `trace` | Trace context propagation | `service_name`, `propagators` (w3c, b3, b3multi, jaeger), `sample_rate` |
| `metrics` | Per-request Prometheus metrics | `namespace`, `subsystem`, `latency_buckets`, ... |
| `logging` | Request logging | see [Access Logging](#access-logging) |

See [configs/olb.yaml](../configs/olb.yaml) for every field of each middleware.

### WAF (Web Application Firewall)

The WAF is configured at the top level under `waf`, not under `middleware`; see [WAF](#waf-web-application-firewall-1).

```yaml
waf:
  enabled: true
  mode: enforce                    # "enforce" (block) or "monitor" (log only)
```

### Access Logging

A basic access log line is always written to the main log. For a configurable request log, enable `middleware.logging`; its lines are written to the main log output (see [Logging](#logging)).

```yaml
middleware:
  logging:
    enabled: true
    format: json                   # "combined" (default), "common", "json", "custom"
    fields:                        # For format: json
      - timestamp
      - method
      - path
      - status
      - duration
      - bytes
      - ip
    exclude_paths:
      - /health
    min_duration: ""               # Only log requests slower than this, e.g. "100ms"
```

---

## TLS

### Static Certificates

```yaml
tls:
  cert_file: /etc/olb/certs/server.crt
  key_file: /etc/olb/certs/server.key
```

This certificate is used by every listener with `tls.enabled: true` and is reloaded on hot reload.

### Multiple Certificates (SNI)

Serving several certificates by SNI is not configurable yet; only one certificate (`tls.cert_file` / `tls.key_file`) can be configured.

### ACME / Let's Encrypt

```yaml
tls:
  acme:
    enabled: true
    email: admin@openloadbalancer.dev   # Required when enabled
    domains:                            # At least one required when enabled
      - openloadbalancer.dev
      - www.openloadbalancer.dev
    directory_url: ""                   # Default: Let's Encrypt production; use the staging directory while testing
    storage_dir: ""                     # Default: "acme" next to the config file
```

With `tls.acme` enabled, OLB obtains one certificate covering all `domains` in the background after start and installs it as the default certificate. It uses the HTTP-01 challenge only: the CA validates over plain HTTP on **port 80**, so a plain `http` listener on port 80 must be reachable from the internet for every domain. Challenge requests are answered ahead of the middleware chain (WAF, auth and redirects do not see them). The account key, certificate and private key are stored in `storage_dir` (mode 0600) and reused on restart; the certificate is renewed 30 days before expiry (checked every 12 hours, retried hourly on failure). Until the first certificate is installed, HTTPS handshakes use the static certificate if one is configured and otherwise fail. Terms of service are accepted on your behalf. Wildcard names (DNS-01) are not supported. Renewed certificates are picked up without a restart, but changing `tls.acme` itself needs a restart.

### Mutual TLS (mTLS)

mTLS is configured per listener under `mtls`, next to `tls`:

```yaml
tls:
  cert_file: /etc/olb/certs/server.crt
  key_file: /etc/olb/certs/server.key

listeners:
  - name: https
    protocol: https
    address: ":443"
    tls:
      enabled: true
    mtls:
      enabled: true
      client_auth: requireandverify   # none, request, requireany, verifyifgiven, requireandverify (default)
      client_cas:                     # Required when enabled
        - /etc/olb/ca/client-ca.crt
    routes:
      - path: /
        pool: backend

pools:
  - name: backend
    backends:
      - address: "10.0.1.10:8080"
```

### OCSP Stapling

The OCSP stapling manager starts automatically; it has no configuration keys.

---

## Cluster

```yaml
cluster:
  enabled: true
  node_id: node-1
  bind_addr: "0.0.0.0"             # Raft transport bind address
  bind_port: 7946                  # Raft transport port
  data_dir: /var/lib/olb/raft      # Raft state for crash recovery (optional)
  election_tick: 2s                # Default 2s
  heartbeat_tick: 500ms            # Default 500ms

  peers:
    - "10.0.0.1:7946"
    - "10.0.0.2:7946"
    - "10.0.0.3:7946"

  node_auth:                       # Strongly recommended: otherwise the transport is unauthenticated
    shared_secret: "${OLB_CLUSTER_SECRET}"
    allowed_node_ids:
      - node-1
      - node-2
      - node-3
```

Cluster transport TLS is not configurable; use `node_auth` and a private network. See [clustering.md](clustering.md) for setup instructions.

---

## Logging

```yaml
logging:
  level: info                      # debug, info, warn, error
  format: json                     # "json" or "text"
  output: stdout                   # "stdout", "stderr", or a file path
```

When `output` is a file path, the file is rotated at 100MB, keeping 10 compressed backups; these rotation settings are not configurable. `format` applies to all three: `json` (the default) writes one JSON object per line, `text` writes `timestamp LEVEL message key=value ...`.

Log files can be reopened for external rotation tools by sending `SIGUSR1`:

```bash
kill -USR1 $(pidof olb)
```

---

## MCP Server

There is no top-level `mcp` section. The MCP server is configured under `admin`, and it starts only when `admin.mcp_token` is set:

```yaml
admin:
  address: "127.0.0.1:8081"
  mcp_address: "127.0.0.1:8082"    # Default: admin port + 1
  mcp_token: "${OLB_MCP_TOKEN}"    # Bearer token clients must send
  mcp_audit: true                  # Log every tool call
```

All tools (including the WAF tools when the WAF is enabled) are registered; the tool list is not configurable. Allowed browser origins for the SSE transport follow `middleware.cors.allowed_origins`. See [mcp.md](mcp.md) for integration details.

---

## Profiling

Go runtime profiling via pprof HTTP endpoint.

```yaml
profiling:
  enabled: true
  pprof_addr: "localhost:6060"     # pprof HTTP endpoint (localhost only recommended)
  cpu_profile_path: ""              # Write CPU profile to file on shutdown
  mem_profile_path: ""              # Write heap profile to file on shutdown
  block_profile_rate: 0             # Fraction of goroutine blocking events to report (0 = off)
  mutex_profile_fraction: 0         # Fraction of mutex contention events to report (0 = off)
  token: ""                         # Bearer token for pprof auth (empty = no auth, localhost-only recommended)
```

When `token` is set, all pprof endpoints require `Authorization: Bearer <token>`.

---

## WAF (Web Application Firewall)

Six-layer security pipeline: IP ACL, rate limiting, request sanitizer, attack detection (SQLi, XSS, CMDi, XXE, SSRF, path traversal), bot detection and response protection. See [waf.md](waf.md) for details.

```yaml
waf:
  enabled: true
  mode: enforce                    # "enforce" (block), "monitor" (log only), "disabled"

  ip_acl:
    enabled: true
    whitelist:                     # Always allowed, bypasses all checks
      - cidr: "10.0.0.0/8"
        reason: "internal"
    blacklist:                     # Always blocked
      - cidr: "192.168.1.100/32"
        reason: "abuse"
        expires: "2027-01-01T00:00:00Z"   # Optional, RFC 3339
    auto_ban:
      enabled: true
      default_ttl: 1h
      max_ttl: 24h

  rate_limit:
    enabled: true
    rules:
      - id: api-per-ip
        scope: ip                  # ip, path, ip+path, global, header:<Name>
        paths:
          - /api
        limit: 100                 # Requests per window
        window: 1m                 # Default 1m
        burst: 20
        auto_ban_after: 5          # Ban the IP after this many violations

  sanitizer:
    enabled: true
    max_header_size: 8192
    max_header_count: 100
    max_body_size: 10485760
    max_url_length: 8192
    max_cookie_size: 4096
    max_cookie_count: 50
    block_null_bytes: true
    normalize_encoding: true
    strip_hop_by_hop: true
    allowed_methods:
      - GET
      - POST
      - PUT
      - PATCH
      - DELETE
      - HEAD
      - OPTIONS
    path_overrides:
      - path: /upload
        max_body_size: 104857600

  detection:
    enabled: true
    threshold:
      block: 50                    # Default 50
      log: 25                      # Default 25
    detectors:                     # Omitted detectors stay on; set enabled: false to turn one off
      sqli:
        enabled: true
        score_multiplier: 1.0
      xss:
        enabled: true
      cmdi:
        enabled: true
      xxe:
        enabled: true
      ssrf:
        enabled: true
      path_traversal:
        enabled: true
    exclusions:
      - path: /api/search
        detectors:
          - sqli
        reason: "free-text search"

  bot_detection:
    enabled: true
    user_agent:
      enabled: true
      block_empty: true
      block_known_scanners: true
    tls_fingerprint:
      enabled: true
    behavior:
      enabled: true
      window: 1m
      rps_threshold: 20

  response:
    security_headers:
      enabled: true
      x_content_type_options: true
      x_frame_options: DENY
      referrer_policy: strict-origin-when-cross-origin
      hsts:
        enabled: true
        max_age: 31536000
        include_subdomains: true
    data_masking:
      enabled: true
      mask_credit_cards: true
      mask_ssn: true
      mask_api_keys: true
      strip_stack_traces: true
    error_pages:
      enabled: true
      mode: production             # "production" hides backend error details

  logging:
    log_blocked: true
    log_allowed: false
```

`block_null_bytes`, `normalize_encoding` and `strip_hop_by_hop` default to `true`; omitting them keeps that default, and only an explicit `false` turns one off.

`waf.detection.exclusions[].condition` is `always` (default) or `whitelist` (the exclusion applies only to whitelisted clients, which normally bypass the WAF entirely, so it rarely matters). `waf.detection.enabled: false` skips the detection layer (omitted = on). `waf.detection.mode` and `waf.bot_detection.mode` (`enforce` or `monitor`) set a layer to log-only; a layer never enforces more strictly than `waf.mode`, so `waf.mode: monitor` keeps everything log-only.

Keys that exist in the schema but currently have no effect:
- `waf.logging.log_body`, `waf.logging.level`, `waf.logging.format`
- `waf.rate_limit.sync_interval`, `waf.rate_limit.store`, and and `rules[].action` (rate-limited requests are always rejected with 429; `throttle` is not implemented)
- `tls_fingerprint` (the whole block: JA3 hashes are never computed, so `enabled` and the `*_action` keys do nothing)

---

## GeoDNS

Geographic DNS routing based on client IP location.

```yaml
geodns:
  enabled: true
  default_pool: "us-east-pool"     # Fallback pool when no rule matches
  db_path: "/etc/olb/GeoLite2-City.mmdb"  # MaxMind GeoLite2 database (required)

  rules:
    - id: "eu-rule"
      country: "DE"                # ISO 3166-1 alpha-2 country code
      pool: "eu-west-pool"         # Route to this pool
      weight: 100                  # Routing weight

    - id: "asia-rule"
      country: "JP"
      pool: "ap-northeast-pool"
      weight: 100

    - id: "fallback-rule"
      country: "*"                 # Wildcard matches all
      pool: "us-east-pool"
      weight: 50
```

Rules also accept `region`, `fallback` and `headers`. Requires a MaxMind GeoLite2 City database (MMDB format), available from MaxMind.

The `geodns` section is validated and the GeoDNS component is created at startup, but it is not yet consulted when routing requests.

---

## Shadow Traffic

Mirror production traffic to a shadow backend for testing and analysis. Shadow responses are discarded — never returned to the client.

```yaml
shadow:
  enabled: true
  percentage: 10.0                 # Percentage of traffic to mirror (0-100)
  copy_headers: true               # Copy request headers to shadow
  copy_body: true                  # Copy request body to shadow
  timeout: "5s"                    # Shadow request timeout (default 30s)

  targets:
    - pool: "shadow-pool"          # Target pool for mirrored traffic
      percentage: 100.0            # Percentage of shadowed traffic to this target
```

Sensitive headers (Authorization, Cookie, Set-Cookie, Proxy-Authorization, X-Api-Key) are stripped from shadow requests.

The `shadow` section is validated and a shadow manager is created at startup, but it is not yet attached to the proxy and `targets` are not registered, so no traffic is mirrored yet.

---

## Service Discovery

A pool can take backends from a discovery provider in addition to its static `backends`:

```yaml
pools:
  - name: api
    algorithm: round_robin
    discovery:
      provider: file            # static | file | dns | consul | docker
      refresh: 30s              # minimum 1s
      tags: []                  # only services carrying all of these tags
      options:
        path: /etc/olb/api-backends.json
        poll_interval: 5s
```

Discovered backends get the pool's `health_check` and the pool's balancer, are added when the provider reports a healthy service and removed when it disappears; statically listed backends are never touched. The pool is reconciled with the provider every 2 seconds, and again after a config reload. A provider that fails to start is logged and the pool keeps serving its static backends. Backends can also be added and removed at runtime through the admin API (these are not managed by discovery).

Provider `options` (all values are strings):

| Provider | Options |
|----------|---------|
| `static` | `addresses`: comma-separated `host:port` list; or `file` (+ `format`) to read a list from a file |
| `file` | `path` (required): JSON file `{"backends":[{"address":"host:port","weight":100}]}`; `poll_interval` (default 5s) |
| `dns` | `domain` (required): SRV domain to resolve; `nameserver`. Queries are sent over plaintext DNS |
| `consul` | `address`, `scheme`, `datacenter`, `token`, `service` (service name to watch) |
| `docker` | `socket_path`, `host`, `label_prefix`, `network`, `poll_interval`, `tls` (`"true"`) |

---

## Complete Example

See [configs/olb.yaml](../configs/olb.yaml) for a full annotated configuration example.

See [configs/olb.minimal.yaml](../configs/olb.minimal.yaml) for a minimal working configuration.
