# Migration Guide

Guide for migrating from other load balancers to OpenLoadBalancer.

## Table of Contents

- [From NGINX](#from-nginx)
- [From HAProxy](#from-haproxy)
- [From Traefik](#from-traefik)
- [From Envoy](#from-envoy)
- [From AWS ALB](#from-aws-alb)

---

## From NGINX

### Basic HTTP Proxy

**NGINX:**
```nginx
upstream backend {
    server 10.0.1.10:8080;
    server 10.0.1.11:8080;
}

server {
    listen 80;
    location / {
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - path: /
        pool: backend

pools:
  - name: backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
```

### SSL/TLS Termination

**NGINX:**
```nginx
server {
    listen 443 ssl;
    ssl_certificate /etc/nginx/ssl/cert.pem;
    ssl_certificate_key /etc/nginx/ssl/key.pem;
    # ...
}
```

**OLB:**
```yaml
listeners:
  - name: https
    address: ":443"
    protocol: https
    tls:
      cert_file: /etc/olb/certs/cert.pem
      key_file: /etc/olb/certs/key.pem
    routes:
      - path: /
        pool: backend
```

### Rate Limiting

**NGINX:**
```nginx
limit_req_zone $binary_remote_addr zone=one:10m rate=10r/s;
limit_req zone=one burst=20 nodelay;
```

**OLB:**
```yaml
middleware:
  rate_limit:
    enabled: true
    requests_per_second: 10
    burst: 20
```

### Health Checks

**NGINX Plus:**
```nginx
upstream backend {
    server 10.0.1.10:8080;
    server 10.0.1.11:8080;
    health_check interval=5s fails=3 passes=2;
}
```

**OLB:**
```yaml
pools:
  - name: backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
    health_check:
      type: http
      path: /health
      interval: 5s
      timeout: 3s
      healthy_threshold: 2
      unhealthy_threshold: 3
```

### Weighted Backends

**NGINX:**
```nginx
upstream backend {
    server 10.0.1.10:8080 weight=3;
    server 10.0.1.11:8080 weight=2;
    server 10.0.1.12:8080 weight=1;
}
```

**OLB:**
```yaml
pools:
  - name: backend
    algorithm: weighted_round_robin
    backends:
      - address: "10.0.1.10:8080"
        weight: 3
      - address: "10.0.1.11:8080"
        weight: 2
      - address: "10.0.1.12:8080"
        weight: 1
```

> NGINX `weight` defaults to 1. OLB also defaults to 1. Use `weighted_round_robin` (or `wrr`) to activate weight-based distribution.

### Gzip Compression

**NGINX:**
```nginx
gzip on;
gzip_types text/plain application/json;
gzip_min_length 1024;
```

**OLB:**
```yaml
middleware:
  compression:
    enabled: true
    min_size: 1024
    level: default
```

### Basic Authentication

**NGINX:**
```nginx
location /admin {
    auth_basic "Restricted";
    auth_basic_user_file /etc/nginx/.htpasswd;
}
```

**OLB:**
```yaml
admin:
  enabled: true
  address: "127.0.0.1:8081"
  auth:
    type: basic
    username: admin
    password: "$2a$10$..."   # bcrypt hash
```

> OLB uses bcrypt-hashed passwords in config. Generate with `htpasswd -nbBC 10 admin password`.

### HTTP → HTTPS Redirect

**NGINX:**
```nginx
server {
    listen 80;
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl;
    # ...
}
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    redirect_https: true

  - name: https
    address: ":443"
    protocol: https
    tls:
      cert_file: /etc/olb/certs/cert.pem
      key_file: /etc/olb/certs/key.pem
    routes:
      - path: /
        pool: backend
```

### Multiple Server Blocks (Virtual Hosts)

**NGINX:**
```nginx
server {
    server_name api.example.com;
    location / { proxy_pass http://api_backend; }
}
server {
    server_name web.example.com;
    location / { proxy_pass http://web_backend; }
}
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - host: api.example.com
        path: /
        pool: api-backend
      - host: web.example.com
        path: /
        pool: web-backend

pools:
  - name: api-backend
    algorithm: least_connections
    backends:
      - address: "10.0.2.10:8080"
  - name: web-backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
```

### Timeout Settings

**NGINX:**
```nginx
proxy_connect_timeout 5s;
proxy_send_timeout 30s;
proxy_read_timeout 30s;
client_body_timeout 10s;
```

**OLB:**
```yaml
global:
  timeouts:
    read: 30s
    write: 30s
    idle: 120s
    header: 10s

pools:
  - name: backend
    connection:
      connect_timeout: 5s
```

---

## From HAProxy

### Basic TCP Load Balancing

**HAProxy:**
```haproxy
global
    maxconn 4096

defaults
    mode tcp
    timeout connect 5s
    timeout client 30s
    timeout server 30s

frontend tcp_frontend
    bind *:3306
    default_backend mysql_backend

backend mysql_backend
    balance roundrobin
    server mysql1 10.0.1.10:3306 check
    server mysql2 10.0.1.11:3306 check
```

**OLB:**
```yaml
listeners:
  - name: mysql
    address: ":3306"
    protocol: tcp
    pool: mysql_backend

pools:
  - name: mysql_backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:3306"
      - address: "10.0.1.11:3306"
    health_check:
      type: tcp
      interval: 5s
```

### Sticky Sessions

**HAProxy:**
```haproxy
backend web_backend
    balance roundrobin
    cookie SESSION insert indirect nocache
    server web1 10.0.1.10:8080 cookie web1
    server web2 10.0.1.11:8080 cookie web2
```

**OLB:**
```yaml
pools:
  - name: web
    algorithm: sticky_sessions
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
```

### SSL Pass-through

**HAProxy:**
```haproxy
frontend ssl_frontend
    bind *:443
    option tcplog
    default_backend ssl_backend

backend ssl_backend
    balance source
    server ssl1 10.0.1.10:443 check
```

**OLB:**
```yaml
listeners:
  - name: ssl
    address: ":443"
    protocol: tcp
    tls:
      passthrough: true
    pool: ssl_backend

pools:
  - name: ssl_backend
    algorithm: ip_hash
    backends:
      - address: "10.0.1.10:443"
```

### ACL-Based Routing

**HAProxy:**
```haproxy
frontend http_frontend
    bind *:80
    acl is_api path_beg /api
    acl is_static path_beg /static
    use_backend api_backend if is_api
    use_backend static_backend if is_static
    default_backend web_backend
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - name: api
        path: /api/
        pool: api-backend
      - name: static
        path: /static/
        pool: static-backend
      - name: default
        path: /
        pool: web-backend
```

> OLB routes are evaluated in order — the first matching route wins. No explicit ACL syntax needed; path/host/method matching is built into routes.

### HAProxy Map File → OLB Routes

**HAProxy:**
```haproxy
# /etc/haproxy/domains.map
api.example.com   api_backend
web.example.com   web_backend
```
```haproxy
frontend http_frontend
    bind *:80
    use_backend %[req.hdr(Host),lower,map(/etc/haproxy/domains.map)]
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - host: api.example.com
        path: /
        pool: api-backend
      - host: web.example.com
        path: /
        pool: web-backend
```

### Connection Limits and Timeouts

**HAProxy:**
```haproxy
defaults
    timeout connect 5s
    timeout client  30s
    timeout server  30s
    timeout tunnel  3600s
    maxconn 4096

backend web_backend
    server web1 10.0.1.10:8080 maxconn 200 check
```

**OLB:**
```yaml
global:
  limits:
    max_connections: 4096
  timeouts:
    read: 30s
    write: 30s
    idle: 120s

pools:
  - name: web-backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
        max_conns: 200
    connection:
      connect_timeout: 5s
      idle_timeout: 90s
```

### Circuit Breaker

**HAProxy:**
```haproxy
backend api_backend
    balance roundrobin
    server api1 10.0.1.10:8080 check inter 2s fall 3 rise 2
    server api2 10.0.1.11:8080 check inter 2s fall 3 rise 2
```

**OLB:**
```yaml
middleware:
  circuit_breaker:
    enabled: true
    error_threshold: 5
    error_window: 10s
    open_duration: 30s
    half_open_requests: 3
    failure_codes: [500, 502, 503, 504]

pools:
  - name: api-backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
    health_check:
      type: http
      interval: 2s
      unhealthy_threshold: 3
      healthy_threshold: 2
```

---

## From Traefik

### Docker Integration

**Traefik:**
```yaml
# docker-compose.yml
services:
  traefik:
    image: traefik:v2.10
    command:
      - "--api.insecure=true"
      - "--providers.docker=true"
  app:
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.app.rule=Host(`app.example.com`)"
```

**OLB:**
```yaml
discovery:
  - type: docker
    docker:
      endpoint: unix:///var/run/docker.sock
      filters:
        - label=olb.enable=true
```

### Let's Encrypt

**Traefik:**
```yaml
certificatesResolvers:
  letsencrypt:
    acme:
      email: admin@example.com
      storage: acme.json
      tlsChallenge: {}
```

**OLB:**
```yaml
acme:
  enabled: true
  email: admin@example.com
  directory: https://acme-v02.api.letsencrypt.org/directory
  storage: /var/lib/olb/acme.json
```

### Labels → YAML Translation

**Traefik (Docker Compose):**
```yaml
services:
  app:
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.app.rule=Host(`app.example.com`)"
      - "traefik.http.routers.app.entrypoints=web"
      - "traefik.http.services.app.loadbalancer.server.port=8080"
```

**OLB (static config):**
```yaml
listeners:
  - name: web
    address: ":80"
    routes:
      - host: app.example.com
        path: /
        pool: app-pool

pools:
  - name: app-pool
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
```

> Traefik discovers backends from Docker labels at runtime. With OLB, declare backends statically in YAML or use Docker service discovery (`discovery.type: docker`).

### Middleware Chains

**Traefik:**
```yaml
http:
  middlewares:
    ratelimit:
      rateLimit:
        average: 100
        burst: 50
    secure-headers:
      headers:
        frameDeny: true
        contentTypeNosniff: true
  routers:
    app:
      rule: "Host(`app.example.com`)"
      middlewares:
        - ratelimit
        - secure-headers
      service: app
```

**OLB:**
```yaml
middleware:
  rate_limit:
    enabled: true
    requests_per_second: 100
    burst_size: 50
  headers:
    enabled: true
    response_set:
      X-Frame-Options: DENY
      X-Content-Type-Options: nosniff

listeners:
  - name: http
    address: ":80"
    routes:
      - host: app.example.com
        path: /
        pool: app-pool
```

> OLB applies all enabled global middleware to every request. For per-route middleware, use the `middleware` field on individual routes.

### Path Prefix Routing

**Traefik:**
```yaml
http:
  routers:
    api:
      rule: "PathPrefix(`/api`)"
      service: api-svc
    web:
      rule: "PathPrefix(`/`)"
      service: web-svc
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - name: api
        path: /api/
        pool: api-svc
      - name: web
        path: /
        pool: web-svc
```

---

## From Envoy

### Basic Configuration

**Envoy:**
```yaml
static_resources:
  listeners:
    - name: listener_0
      address:
        socket_address: { address: 0.0.0.0, port_value: 80 }
      filter_chains:
        - filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                route_config:
                  virtual_hosts:
                    - routes:
                        - match: { prefix: "/" }
                          route: { cluster: service_backend }
  clusters:
    - name: service_backend
      connect_timeout: 5s
      lb_policy: ROUND_ROBIN
      load_assignment:
        endpoints:
          - lb_endpoints:
              - endpoint: { address: { socket_address: { address: 10.0.1.10, port_value: 8080 } } }
              - endpoint: { address: { socket_address: { address: 10.0.1.11, port_value: 8080 } } }
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - path: /
        pool: service_backend

pools:
  - name: service_backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
    health_check:
      type: http
      interval: 5s
```

### Retries and Timeouts

**Envoy:**
```yaml
static_resources:
  clusters:
    - name: service_backend
      connect_timeout: 5s
      per_connection_buffer_limit_bytes: 32768
      circuit_breakers:
        thresholds:
          - max_connections: 1000
            max_pending_requests: 100
            max_retries: 3
      outlier_detection:
        consecutive_5xx: 5
        interval: 10s
        base_ejection_time: 30s
```

**OLB:**
```yaml
pools:
  - name: service-backend
    algorithm: round_robin
    backends:
      - address: "10.0.1.10:8080"
      - address: "10.0.1.11:8080"
    connection:
      connect_timeout: 5s
      max_per_host: 1000

middleware:
  retry:
    enabled: true
    max_retries: 3
    retry_on: [502, 503, 504]
    backoff:
      initial: 100ms
      max: 10s
      multiplier: 2.0
  circuit_breaker:
    enabled: true
    error_threshold: 5
    error_window: 10s
    open_duration: 30s
```

### Weighted Cluster (Traffic Splitting)

**Envoy:**
```yaml
route_config:
  virtual_hosts:
    - name: backend
      routes:
        - match: { prefix: "/" }
          route:
            weighted_clusters:
              clusters:
                - name: web-v1
                  weight: 90
                - name: web-v2
                  weight: 10
```

**OLB:**
```yaml
listeners:
  - name: http
    address: ":80"
    routes:
      - path: /
        split:
          - pool: web-v1
            weight: 90
          - pool: web-v2
            weight: 10
```

---

## From AWS ALB

### Basic Configuration

**AWS ALB (Terraform):**
```hcl
resource "aws_lb" "main" {
  name               = "main-alb"
  load_balancer_type = "application"
  subnets            = var.subnet_ids
}

resource "aws_lb_target_group" "app" {
  name     = "app-tg"
  port     = 80
  protocol = "HTTP"
  vpc_id   = var.vpc_id
}

resource "aws_lb_target_group_attachment" "app" {
  target_group_arn = aws_lb_target_group.app.arn
  target_id        = "i-1234567890abcdef0"
  port             = 80
}
```

**OLB on AWS:**
See `deploy/terraform/examples/aws/` for complete AWS deployment with OLB.

---

## Migration Checklist

### Pre-Migration

- [ ] **Inventory current config** — Export full NGINX/HAProxy/Traefik/Envoy config files
- [ ] **Document custom logic** — Identify custom Lua scripts, external checks, or template logic that needs manual translation
- [ ] **Map algorithms** — Verify your load balancing algorithm maps correctly (see table below)
- [ ] **Export TLS certificates** — Copy `.crt`/`.key` files; note ACME-issued certificates for Let's Encrypt migration
- [ ] **List all backends** — Export backend addresses, weights, ports, health check parameters
- [ ] **Record current metrics** — Capture baseline RPS, latency percentiles, error rates for comparison

### Algorithm Mapping

| Source | Directive | OLB Algorithm |
|--------|-----------|---------------|
| NGINX | (default) | `round_robin` |
| NGINX | `ip_hash` | `ip_hash` |
| NGINX | `weight=N` | `weighted_round_robin` |
| HAProxy | `balance roundrobin` | `round_robin` |
| HAProxy | `balance leastconn` | `least_connections` |
| HAProxy | `balance source` | `ip_hash` |
| HAProxy | `balance uri` | `consistent_hash` |
| Traefik | `wrr` | `weighted_round_robin` |
| Envoy | `ROUND_ROBIN` | `round_robin` |
| Envoy | `LEAST_REQUEST` | `least_connections` |
| Envoy | `RING_HASH` | `ring_hash` |
| Envoy | `MAGLEV` | `maglev` |
| Envoy | `RANDOM` | `random` |

### Build OLB Config

- [ ] Create `olb.yaml` with mapped listeners, routes, and pools
- [ ] Configure health checks (type, interval, thresholds)
- [ ] Set up TLS certificates or ACME
- [ ] Enable middleware (rate limiting, CORS, compression, circuit breaker)
- [ ] Configure admin API address and authentication
- [ ] Validate: `olb config validate olb.yaml`

### Test

- [ ] **Unit test config** — `olb config validate olb.yaml` passes with no errors
- [ ] **Stage deployment** — Run OLB alongside existing LB in parallel
- [ ] **Traffic shadow** — Mirror production traffic to OLB backends and compare responses
- [ ] **Health check verification** — Confirm all backends report healthy via `olb health show`
- [ ] **Load test** — Verify RPS and latency match or exceed baseline
- [ ] **Failover test** — Stop a backend and confirm OLB detects and reroutes

### Cutover

- [ ] **DNS switch** — Update DNS to point to OLB listener addresses
- [ ] **Monitor error rates** — Watch `/api/v1/metrics` and Prometheus dashboard for 5xx spikes
- [ ] **Monitor latency** — Compare P50/P95/P99 against pre-migration baseline
- [ ] **Verify TLS** — Confirm certificate chain, OCSP stapling, and protocol version
- [ ] **Keep old config** — Preserve old LB config for rollback for 48 hours

### Post-Migration

- [ ] **Remove old LB** — Decommission previous load balancer after stable operation
- [ ] **Update monitoring** — Point Grafana dashboards to OLB Prometheus metrics
- [ ] **Update runbooks** — Document OLB-specific operational procedures
- [ ] **Team training** — Share CLI quick reference (`olb status`, `olb backend list`, `olb reload`)
- [ ] **Enable clustering** — If running multi-node, configure Raft + SWIM gossip
