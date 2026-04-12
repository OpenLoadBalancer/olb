# Configuration Reference

Complete reference for all OLB configuration options.

## File Formats

OLB supports four configuration formats, all parsed from scratch with zero external dependencies:

| Format | Extension | Notes |
|--------|-----------|-------|
| YAML | `.yaml`, `.yml` | Recommended. Full YAML 1.2 subset. |
| JSON | `.json` | Uses Go stdlib `encoding/json`. |
| TOML | `.toml` | TOML v1.0. Tables, arrays, inline tables. |
| HCL | `.hcl` | HashiCorp Configuration Language subset. |

Specify the config file at startup:

```bash
olb start --config /etc/olb/olb.yaml
```

OLB auto-detects the format from the file extension. If no config file is specified, OLB searches for `olb.yaml`, `olb.toml`, `olb.hcl`, or `olb.json` in the current directory and `/etc/olb/`.

## Environment Variable Overlay

Every configuration key can be overridden via environment variables using the `OLB_` prefix with `__` (double underscore) as a path separator:

```bash
# Override log level
export OLB_LOGGING__LEVEL=debug

# Override admin API address
export OLB_ADMIN__ADDRESS=0.0.0.0:9090

# Override max connections
export OLB_GLOBAL__LIMITS__MAX_CONNECTIONS=50000
```

Within config files, reference environment variables using `${VAR}` or `${VAR:-default}` syntax:

```yaml
admin:
  address: "${OLB_ADMIN_ADDR:-127.0.0.1:8081}"
  auth:
    password: "${OLB_ADMIN_PASSWORD}"
```

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
- Backends (add, remove, change weights)
- TLS certificates
- Middleware configuration
- Rate limit parameters
- Health check parameters
- Logging configuration

**Requires restart:**
- Listener addresses and ports
- Cluster configuration
- Admin API bind address
- Worker count and buffer sizes

---

## Global Settings

```yaml
version: 1

global:
  workers:
    count: auto              # "auto" = number of CPUs (GOMAXPROCS)

  limits:
    max_connections: 10000             # Total max concurrent connections
    max_connections_per_source: 100    # Per client IP
    max_connections_per_backend: 1000  # Per backend server

  timeouts:
    read: 30s        # Max time to read entire request
    write: 30s       # Max time to write entire response
    idle: 120s       # Max idle connection duration
    header: 10s      # Max time to read request headers
    drain: 30s       # Graceful shutdown drain timeout
```

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
  enabled: true
  address: "127.0.0.1:8081"       # Bind address (localhost-only by default)

  auth:
    type: basic                    # "basic", "token", or "none"
    username: admin
    password: "$2a$10$..."         # bcrypt-hashed password

    # Or use token auth:
    # type: token
    # token: "your-secret-token"

  tls:
    enabled: false
    cert_file: /etc/olb/admin.crt
    key_file: /etc/olb/admin.key

  webui:
    enabled: true
    path_prefix: /ui              # Web UI served at this path
```

---

## Metrics

```yaml
metrics:
  enabled: true
  path: /metrics                  # Prometheus endpoint on admin API

  prometheus:
    enabled: true
    path: /metrics

  retention: 1h                   # In-memory metrics retention
  resolution: 10s                 # Metrics aggregation interval
```

Prometheus metrics are exposed at `http://<admin-address>/metrics`. JSON metrics are available at `/api/v1/metrics`.

---

## Listeners

Listeners define frontend entry points where OLB accepts connections.

### HTTP Listener

```yaml
listeners:
  - name: http
    protocol: http
    address: ":80"
    redirect_https: false         # Redirect all traffic to HTTPS

    routes:
      - name: default
        path: /
        pool: backend
```

### HTTPS Listener

```yaml
  - name: https
    protocol: https
    address: ":443"

    tls:
      cert_file: /etc/olb/certs/server.crt
      key_file: /etc/olb/certs/server.key
      min_version: "1.2"          # Minimum TLS version

    routes:
      - name: secure-app
        path: /
        pool: backend
```

### TCP Listener (L4)

```yaml
  - name: mysql
    protocol: tcp
    address: ":3306"
    proxy_protocol: v2            # Send PROXY protocol to backends

    routes:
      - name: mysql-route
        pool: mysql-pool
```

### UDP Listener (L4)

```yaml
  - name: dns
    protocol: udp
    address: ":53"
    session_timeout: 30s          # UDP session tracking timeout

    routes:
      - name: dns-route
        pool: dns-pool
```

---

## Routes

Routes match incoming requests and direct them to backend pools.

```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      # Match by host and path
      - name: api
        host: api.example.com
        path: /api/
        methods: [GET, POST, PUT, DELETE]
        pool: api-backend

      # Match by path prefix
      - name: static
        path: /static/
        pool: static-backend

      # Default catch-all
      - name: default
        path: /
        pool: web-backend

        # Per-route middleware
        middleware:
          - name: rate_limit
            config:
              requests_per_second: 100
              burst_size: 200
```

### Route Matching Fields

| Field | Description | Example |
|-------|-------------|---------|
| `host` | Match hostname (exact or wildcard) | `api.example.com`, `*.example.com` |
| `path` | Match URL path (prefix) | `/api/`, `/static/*` |
| `methods` | Match HTTP methods | `[GET, POST]` |
| `headers` | Match request headers | `Upgrade: websocket` |

Routes are evaluated in order. The first matching route wins.

### Traffic Splitting (Canary)

```yaml
      - name: canary
        path: /feature/
        split:
          - pool: web-app
            weight: 90
          - pool: web-app-canary
            weight: 10
```

---

## Backend Pools

Backend pools define groups of upstream servers and how traffic is distributed among them.

```yaml
pools:
  - name: web-backend
    algorithm: round_robin         # Load balancing algorithm

    backends:
      - id: web-1
        address: "10.0.1.10:8080"
        weight: 1                  # Relative weight (for weighted algorithms)
      - id: web-2
        address: "10.0.1.11:8080"
        weight: 1
      - id: web-3
        address: "10.0.1.12:8080"
        weight: 2                  # Gets 2x traffic with weighted algorithms
```

### Available Algorithms

`round_robin`, `weighted_round_robin`, `least_connections`, `least_response_time`, `ip_hash`, `consistent_hash`, `maglev`, `power_of_two`, `random`, `weighted_random`, `ring_hash`, `sticky_session`

See [algorithms.md](algorithms.md) for details on each algorithm.

### Health Checks

```yaml
    health_check:
      type: http                   # "http", "https", "tcp", "grpc", or "exec"
      path: /health                # HTTP health check path
      interval: 10s                # Check interval
      timeout: 5s                  # Check timeout
      healthy_threshold: 2         # Consecutive successes to mark healthy
      unhealthy_threshold: 3       # Consecutive failures to mark unhealthy
      expected_status: 200         # Expected HTTP status code
```

TCP health check (connection-only):

```yaml
    health_check:
      type: tcp
      interval: 10s
      timeout: 5s
```

gRPC health check (uses grpc.health.v1.Health/Check protocol):

```yaml
    health_check:
      type: grpc
      interval: 10s
      timeout: 5s
```

Exec health check (runs an external command; exit code 0 = healthy):

```yaml
    health_check:
      type: exec
      command: /usr/local/bin/check-backend
      args: ["{{.Host}}", "{{.Port}}"]
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

```yaml
    connection:
      max_idle: 10                 # Max idle connections per backend
      max_per_host: 100            # Max connections per backend
      idle_timeout: 90s            # Close idle connections after this
      connect_timeout: 5s          # Backend connection timeout
```

### Session Affinity

```yaml
    sticky:
      type: cookie                 # "cookie", "header", or "param"
      name: OLB_BACKEND            # Cookie/header/param name
      ttl: 1h                      # Session TTL
      http_only: true              # Cookie HttpOnly flag
      secure: true                 # Cookie Secure flag
```

---

## Middleware

Middleware is applied globally or per-route.

### Global Middleware

```yaml
middleware:
  - name: request_id
    enabled: true
    config:
      header_name: X-Request-ID
      trust_incoming: false

  - name: real_ip
    enabled: true
    config:
      trusted_proxies:
        - "10.0.0.0/8"
        - "172.16.0.0/12"
        - "192.168.0.0/16"
```

### Rate Limiting

```yaml
  - name: rate_limit
    enabled: true
    config:
      requests_per_second: 100
      burst_size: 200
      key: client_ip               # "client_ip", "header:X-API-Key", "path"
```

### CORS

```yaml
  - name: cors
    enabled: true
    config:
      allowed_origins: ["*"]
      allowed_methods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
      allowed_headers: ["Content-Type", "Authorization"]
      max_age: 3600
```

### Compression

```yaml
  - name: compression
    enabled: true
    config:
      min_size: 1024               # Minimum response size to compress (bytes)
      level: default               # "fastest", "default", "best"
```

### Security Headers

```yaml
  - name: headers
    enabled: true
    config:
      security_preset: strict
      response_set:
        X-Frame-Options: DENY
        X-Content-Type-Options: nosniff
        Strict-Transport-Security: "max-age=31536000; includeSubDomains"
```

### Circuit Breaker

```yaml
  - name: circuit_breaker
    enabled: true
    config:
      error_threshold: 5           # Errors to open circuit
      error_window: 10s            # Window for counting errors
      open_duration: 30s           # How long to stay open
      half_open_requests: 3        # Test requests in half-open state
      failure_codes: [500, 502, 503, 504]
```

### Retry

```yaml
  - name: retry
    enabled: true
    config:
      max_retries: 3
      retry_on: [502, 503, 504]
      retry_methods: [GET, HEAD, OPTIONS]
      backoff:
        initial: 100ms
        max: 10s
        multiplier: 2.0
        jitter: 0.1
```

### Response Cache

```yaml
  - name: cache
    enabled: true
    config:
      max_size: 104857600          # 100MB
      max_age: 5m
      methods: [GET, HEAD]
      status_codes: [200, 301, 302]
```

### IP Filter

```yaml
  - name: ip_filter
    enabled: true
    config:
      mode: allow                  # "allow" or "deny"
      rules:
        - "10.0.0.0/8"
        - "192.168.1.0/24"
```

### WAF (Web Application Firewall)

```yaml
  - name: waf
    enabled: true
    config:
      mode: block                  # "block" or "detect" (log only)
      rules:
        sqli: true                 # SQL injection detection
        xss: true                  # Cross-site scripting detection
        path_traversal: true       # Path traversal detection
        command_injection: true    # Command injection detection
      anomaly_threshold: 5         # Block if score exceeds this
```

### Access Logging

```yaml
  - name: access_log
    enabled: true
    config:
      format: json                 # "json", "clf", "combined"
      output: /var/log/olb/access.log
```

---

## TLS

### Static Certificates

```yaml
tls:
  cert_file: /etc/olb/certs/server.crt
  key_file: /etc/olb/certs/server.key
```

### Multiple Certificates (SNI)

```yaml
listeners:
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
        - cert: /etc/olb/certs/other.com.crt
          key: /etc/olb/certs/other.com.key
          domains:
            - other.com
```

### ACME / Let's Encrypt

```yaml
      acme:
        enabled: true
        email: admin@example.com
        domains:
          - example.com
          - www.example.com
        storage: /etc/olb/acme/    # Certificate storage directory
        # provider: letsencrypt    # "letsencrypt", "letsencrypt-staging", "zerossl"
        # challenge: http-01       # "http-01" or "tls-alpn-01"
        # renew_before: 720h       # Renew 30 days before expiry
```

### Mutual TLS (mTLS)

```yaml
    tls:
      client_auth: require         # "none", "request", "require", "verify"
      client_ca:
        - /etc/olb/ca/client-ca.crt
```

### OCSP Stapling

OCSP stapling is enabled by default for all TLS listeners. It fetches and caches OCSP responses, stapling them to TLS handshakes for faster client verification.

```yaml
    tls:
      ocsp_stapling: true          # Default: true
```

---

## Cluster

```yaml
cluster:
  enabled: true
  node_name: node-1
  bind_address: "0.0.0.0:7946"    # Gossip protocol
  raft_address: "0.0.0.0:7947"    # Raft consensus

  peers:
    - "10.0.0.1:7946"
    - "10.0.0.2:7946"
    - "10.0.0.3:7946"

  tls:
    enabled: true
    cert: /etc/olb/cluster.crt
    key: /etc/olb/cluster.key
    ca: /etc/olb/cluster-ca.crt
```

See [clustering.md](clustering.md) for setup instructions.

---

## Logging

```yaml
logging:
  level: info                      # trace, debug, info, warn, error, fatal
  format: json                     # "json" or "text"
  output: stdout                   # "stdout", "stderr", or file path

  # File output with rotation
  file:
    path: /var/log/olb/olb.log
    max_size: 100MB                # Rotate after this size
    max_backups: 5                 # Keep this many old files
    max_age: 30                    # Delete files older than N days
    compress: true                 # Compress rotated files
```

Log files can be reopened for external rotation tools by sending `SIGUSR1`:

```bash
kill -USR1 $(pidof olb)
```

---

## MCP Server

```yaml
mcp:
  enabled: true
  transport: stdio                 # "stdio" or "http"

  http:
    address: "127.0.0.1:9091"     # Only used with http transport

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

See [mcp.md](mcp.md) for integration details.

---

## Complete Example

See [configs/olb.yaml](../configs/olb.yaml) for a full annotated configuration example.

See [configs/olb.minimal.yaml](../configs/olb.minimal.yaml) for a minimal working configuration.
