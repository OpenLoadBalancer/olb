# REST API Reference

OLB provides a REST API for runtime management, monitoring, and integration.

## Base URL

```
http://localhost:8081/api/v1
```

The admin API address is configured in `olb.yaml`:

```yaml
admin:
  enabled: true
  address: "127.0.0.1:8081"
```

## Authentication

All API requests require authentication (unless `auth.type` is set to `none`).

### Basic Auth

```bash
curl -u admin:password http://localhost:8081/api/v1/system/info
```

### Bearer Token

```bash
curl -H "Authorization: Bearer your-secret-token" \
  http://localhost:8081/api/v1/system/info
```

### WebSocket Authentication

For WebSocket endpoints, pass the token as a query parameter:

```
ws://localhost:8081/api/v1/ws/metrics?token=your-secret-token
```

---

## Rate Limiting

The admin API has built-in per-IP rate limiting to prevent brute-force attacks on authentication endpoints.

### Default Limits

| Parameter | Default |
|-----------|---------|
| Max requests per window | 60 |
| Window duration | 1 minute |

### Behavior

- Rate limiting is applied **before** authentication — unauthenticated requests also count toward the limit
- Each source IP gets an independent counter
- When the limit is exceeded, the API returns **429 Too Many Requests** with a `Retry-After` header
- Stale entries are cleaned up automatically by a background goroutine

### Response When Rate Limited

```json
{
  "error": "rate limit exceeded"
}
```

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 60
Content-Type: application/json
```

### Configuration

Configure via the admin section in `olb.yaml`:

```yaml
admin:
  enabled: true
  address: "127.0.0.1:8081"
  rate_limit_max_requests: 100   # default: 60
  rate_limit_window: "2m"        # default: "1m"
```

### Additional Protection: Circuit Breaker

The admin API also has an internal circuit breaker that protects against cascading failures:

| Parameter | Default |
|-----------|---------|
| Error threshold | 5 consecutive errors |
| Open duration | 30 seconds |
| Half-open timeout | 10 seconds |
| Successes to close | 3 consecutive |

When the circuit breaker is open, the API returns **503 Service Unavailable** for state-changing operations.

---

## System

### GET /api/v1/system/info

Returns version, uptime, and system status.

```bash
curl http://localhost:8081/api/v1/system/info
```

```json
{
  "version": "1.0.0",
  "go_version": "go1.23.0",
  "uptime_seconds": 86400,
  "state": "running",
  "start_time": "2026-03-14T10:00:00Z",
  "reload_count": 3,
  "goroutines": 42,
  "listeners": [
    {"name": "http", "address": ":80", "protocol": "http"},
    {"name": "https", "address": ":443", "protocol": "https"}
  ]
}
```

### GET /api/v1/system/health

Health check endpoint. Returns 200 if OLB is healthy. Use this for health checking OLB itself (load-balancer-of-load-balancers).

```bash
curl http://localhost:8081/api/v1/system/health
```

```json
{
  "status": "healthy",
  "checks": {
    "listeners": "ok",
    "backends": "ok",
    "cluster": "ok"
  }
}
```

Returns HTTP 503 if OLB is unhealthy or draining.

### POST /api/v1/system/reload

Trigger a configuration hot-reload.

```bash
curl -X POST http://localhost:8081/api/v1/system/reload
```

```json
{
  "status": "ok",
  "reload_count": 4,
  "changes": [
    "+ backends.web-pool.backends[2]: 10.0.1.3:8080",
    "~ routes.api.middleware.rate_limit.rate: 50 -> 100"
  ]
}
```

### POST /api/v1/system/drain

Start graceful shutdown (drain connections).

```bash
curl -X POST http://localhost:8081/api/v1/system/drain
```

```json
{
  "status": "draining",
  "active_connections": 42,
  "drain_timeout": "30s"
}
```

---

## Backends

### GET /api/v1/backends

List all backend pools.

```bash
curl http://localhost:8081/api/v1/backends
```

```json
{
  "pools": [
    {
      "name": "web-backend",
      "algorithm": "round_robin",
      "backends": [
        {
          "id": "web-1",
          "address": "10.0.1.10:8080",
          "state": "healthy",
          "weight": 1,
          "active_connections": 14,
          "total_requests": 52341,
          "error_rate": 0.001,
          "avg_response_time_ms": 12.5
        },
        {
          "id": "web-2",
          "address": "10.0.1.11:8080",
          "state": "healthy",
          "weight": 1,
          "active_connections": 12,
          "total_requests": 51987,
          "error_rate": 0.002,
          "avg_response_time_ms": 11.8
        }
      ]
    }
  ]
}
```

### GET /api/v1/backends/:pool

Get details for a specific pool.

```bash
curl http://localhost:8081/api/v1/backends/web-backend
```

### GET /api/v1/backends/:pool/:backend

Get details for a specific backend.

```bash
curl http://localhost:8081/api/v1/backends/web-backend/web-1
```

### POST /api/v1/backends/:pool

Add a new backend to a pool.

```bash
curl -X POST http://localhost:8081/api/v1/backends/web-backend \
  -H "Content-Type: application/json" \
  -d '{
    "id": "web-3",
    "address": "10.0.1.12:8080",
    "weight": 1
  }'
```

```json
{
  "status": "ok",
  "backend": {
    "id": "web-3",
    "address": "10.0.1.12:8080",
    "state": "healthy",
    "weight": 1
  }
}
```

### DELETE /api/v1/backends/:pool/:backend

Remove a backend from a pool.

```bash
curl -X DELETE http://localhost:8081/api/v1/backends/web-backend/web-3
```

```json
{
  "status": "ok",
  "message": "backend web-3 removed from pool web-backend"
}
```

### PATCH /api/v1/backends/:pool/:backend

Update a backend (weight, state).

```bash
curl -X PATCH http://localhost:8081/api/v1/backends/web-backend/web-1 \
  -H "Content-Type: application/json" \
  -d '{"weight": 3}'
```

```json
{
  "status": "ok",
  "backend": {
    "id": "web-1",
    "weight": 3
  }
}
```

### POST /api/v1/backends/:pool/:backend/drain

Drain a backend (stop sending new requests, wait for active connections to finish).

```bash
curl -X POST http://localhost:8081/api/v1/backends/web-backend/web-1/drain
```

```json
{
  "status": "ok",
  "backend": "web-1",
  "state": "draining",
  "active_connections": 14
}
```

### POST /api/v1/backends/:pool/:backend/enable

Re-enable a disabled or drained backend.

```bash
curl -X POST http://localhost:8081/api/v1/backends/web-backend/web-1/enable
```

### POST /api/v1/backends/:pool/:backend/disable

Disable a backend (immediately stop all traffic).

```bash
curl -X POST http://localhost:8081/api/v1/backends/web-backend/web-1/disable
```

---

## Routes

### GET /api/v1/routes

List all routes.

```bash
curl http://localhost:8081/api/v1/routes
```

```json
{
  "routes": [
    {
      "name": "api",
      "host": "api.example.com",
      "path": "/api/",
      "methods": ["GET", "POST", "PUT", "DELETE"],
      "pool": "api-backend",
      "requests_total": 123456,
      "avg_latency_ms": 15.2
    },
    {
      "name": "default",
      "path": "/",
      "pool": "web-backend",
      "requests_total": 654321,
      "avg_latency_ms": 8.7
    }
  ]
}
```

### GET /api/v1/routes/:name

Get details for a specific route.

```bash
curl http://localhost:8081/api/v1/routes/api
```

### POST /api/v1/routes

Add a new route.

```bash
curl -X POST http://localhost:8081/api/v1/routes \
  -H "Content-Type: application/json" \
  -d '{
    "name": "new-api",
    "host": "api.example.com",
    "path": "/v2/",
    "methods": ["GET", "POST"],
    "pool": "api-v2-backend"
  }'
```

### PUT /api/v1/routes/:name

Update an existing route.

```bash
curl -X PUT http://localhost:8081/api/v1/routes/api \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/api/v1/",
    "pool": "api-backend-v2"
  }'
```

### DELETE /api/v1/routes/:name

Remove a route.

```bash
curl -X DELETE http://localhost:8081/api/v1/routes/api
```

### POST /api/v1/routes/test

Test which route matches a given URL.

```bash
curl -X POST http://localhost:8081/api/v1/routes/test \
  -H "Content-Type: application/json" \
  -d '{
    "method": "GET",
    "host": "api.example.com",
    "path": "/api/users/123"
  }'
```

```json
{
  "matched_route": "api",
  "pool": "api-backend",
  "match_details": {
    "host": "api.example.com",
    "path_pattern": "/api/",
    "method": "GET"
  }
}
```

---

## Health

### GET /api/v1/health

Get health status for all backends.

```bash
curl http://localhost:8081/api/v1/health
```

```json
{
  "pools": {
    "web-backend": {
      "healthy": 2,
      "unhealthy": 1,
      "total": 3,
      "backends": {
        "web-1": {"state": "healthy", "last_check": "2026-03-15T10:00:00Z", "consecutive_successes": 5},
        "web-2": {"state": "healthy", "last_check": "2026-03-15T10:00:00Z", "consecutive_successes": 12},
        "web-3": {"state": "unhealthy", "last_check": "2026-03-15T10:00:00Z", "consecutive_failures": 3, "last_error": "connection refused"}
      }
    }
  }
}
```

### GET /api/v1/health/:pool

Get health status for a specific pool.

```bash
curl http://localhost:8081/api/v1/health/web-backend
```

### GET /api/v1/health/:pool/:backend

Get health status for a specific backend.

```bash
curl http://localhost:8081/api/v1/health/web-backend/web-1
```

### POST /api/v1/health/:pool/:backend/check

Trigger an immediate health check.

```bash
curl -X POST http://localhost:8081/api/v1/health/web-backend/web-3/check
```

```json
{
  "backend": "web-3",
  "result": "unhealthy",
  "latency_ms": 5012,
  "error": "connection refused"
}
```

---

## Metrics

### GET /api/v1/metrics

Get all metrics in JSON format.

```bash
curl http://localhost:8081/api/v1/metrics
```

```json
{
  "uptime_seconds": 86400,
  "total_requests": 1234567,
  "active_connections": 342,
  "requests_per_second": 1247.5,
  "error_rate": 0.003,
  "bytes_received": 5368709120,
  "bytes_sent": 10737418240
}
```

### GET /api/v1/metrics/summary

Dashboard summary with key metrics.

```bash
curl http://localhost:8081/api/v1/metrics/summary
```

### GET /api/v1/metrics/backends

Backend-specific metrics.

```bash
curl http://localhost:8081/api/v1/metrics/backends
```

### GET /api/v1/metrics/routes

Route-specific metrics.

```bash
curl http://localhost:8081/api/v1/metrics/routes
```

### GET /api/v1/metrics/timeseries

Time series data for charting.

```bash
curl "http://localhost:8081/api/v1/metrics/timeseries?metric=olb_route_requests_total&range=1h&step=10s"
```

```json
{
  "metric": "olb_route_requests_total",
  "range": "1h",
  "step": "10s",
  "data": [
    {"timestamp": "2026-03-15T09:00:00Z", "value": 1200},
    {"timestamp": "2026-03-15T09:00:10Z", "value": 1215},
    {"timestamp": "2026-03-15T09:00:20Z", "value": 1198}
  ]
}
```

### GET /metrics

Prometheus exposition format. Use this endpoint as a Prometheus scrape target.

```bash
curl http://localhost:8081/metrics
```

```
# HELP olb_uptime_seconds Uptime in seconds
# TYPE olb_uptime_seconds gauge
olb_uptime_seconds 86400

# HELP olb_route_requests_total Total requests per route
# TYPE olb_route_requests_total counter
olb_route_requests_total{route="default",method="GET",status="200"} 1234567

# HELP olb_backend_up Whether backend is healthy
# TYPE olb_backend_up gauge
olb_backend_up{pool="web-backend",backend="web-1"} 1
olb_backend_up{pool="web-backend",backend="web-2"} 1
olb_backend_up{pool="web-backend",backend="web-3"} 0

# HELP olb_backend_request_duration_seconds Backend request duration
# TYPE olb_backend_request_duration_seconds histogram
olb_backend_request_duration_seconds_bucket{pool="web-backend",backend="web-1",le="0.005"} 4523
olb_backend_request_duration_seconds_bucket{pool="web-backend",backend="web-1",le="0.01"} 12345
olb_backend_request_duration_seconds_bucket{pool="web-backend",backend="web-1",le="0.025"} 45678
olb_backend_request_duration_seconds_bucket{pool="web-backend",backend="web-1",le="+Inf"} 52341
olb_backend_request_duration_seconds_sum{pool="web-backend",backend="web-1"} 623.45
olb_backend_request_duration_seconds_count{pool="web-backend",backend="web-1"} 52341
```

---

## Config

### GET /api/v1/config

Get the current running configuration.

```bash
curl http://localhost:8081/api/v1/config
```

### PUT /api/v1/config

Replace the entire configuration. Triggers a hot-reload.

```bash
curl -X PUT http://localhost:8081/api/v1/config \
  -H "Content-Type: application/yaml" \
  --data-binary @olb.yaml
```

### PATCH /api/v1/config

Partial configuration update.

```bash
curl -X PATCH http://localhost:8081/api/v1/config \
  -H "Content-Type: application/json" \
  -d '{"logging": {"level": "debug"}}'
```

### POST /api/v1/config/validate

Validate a configuration without applying it.

```bash
curl -X POST http://localhost:8081/api/v1/config/validate \
  -H "Content-Type: application/yaml" \
  --data-binary @new-config.yaml
```

```json
{
  "valid": true,
  "warnings": []
}
```

---

## Certificates

### GET /api/v1/certs

List all certificates.

```bash
curl http://localhost:8081/api/v1/certs
```

```json
{
  "certificates": [
    {
      "domain": "example.com",
      "sans": ["example.com", "*.example.com"],
      "issuer": "Let's Encrypt",
      "not_before": "2026-01-15T00:00:00Z",
      "not_after": "2026-04-15T00:00:00Z",
      "days_remaining": 31,
      "auto_renew": true,
      "source": "acme"
    }
  ]
}
```

### GET /api/v1/certs/:domain

Get certificate details.

```bash
curl http://localhost:8081/api/v1/certs/example.com
```

### POST /api/v1/certs

Add a certificate.

```bash
curl -X POST http://localhost:8081/api/v1/certs \
  -H "Content-Type: application/json" \
  -d '{
    "domain": "new.example.com",
    "cert": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
    "key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
  }'
```

### DELETE /api/v1/certs/:domain

Remove a certificate.

```bash
curl -X DELETE http://localhost:8081/api/v1/certs/old.example.com
```

### POST /api/v1/certs/:domain/renew

Force ACME certificate renewal.

```bash
curl -X POST http://localhost:8081/api/v1/certs/example.com/renew
```

```json
{
  "status": "ok",
  "domain": "example.com",
  "new_expiry": "2026-06-15T00:00:00Z"
}
```

---

## Cluster

### GET /api/v1/cluster/status

Get cluster status.

```bash
curl http://localhost:8081/api/v1/cluster/status
```

```json
{
  "enabled": true,
  "state": "healthy",
  "node_name": "node-1",
  "leader": "node-1",
  "raft_term": 3,
  "nodes_total": 3,
  "nodes_healthy": 3
}
```

### GET /api/v1/cluster/members

List cluster members.

```bash
curl http://localhost:8081/api/v1/cluster/members
```

```json
{
  "members": [
    {
      "name": "node-1",
      "address": "10.0.0.1:7946",
      "state": "alive",
      "raft_role": "leader",
      "uptime_seconds": 86400
    },
    {
      "name": "node-2",
      "address": "10.0.0.2:7946",
      "state": "alive",
      "raft_role": "follower",
      "uptime_seconds": 86300
    },
    {
      "name": "node-3",
      "address": "10.0.0.3:7946",
      "state": "alive",
      "raft_role": "follower",
      "uptime_seconds": 86300
    }
  ]
}
```

### POST /api/v1/cluster/join

Join a node to the cluster.

```bash
curl -X POST http://localhost:8081/api/v1/cluster/join \
  -H "Content-Type: application/json" \
  -d '{"address": "10.0.0.4:7946"}'
```

### POST /api/v1/cluster/leave

Remove the current node from the cluster.

```bash
curl -X POST http://localhost:8081/api/v1/cluster/leave
```

### GET /api/v1/cluster/raft

Get Raft consensus state.

```bash
curl http://localhost:8081/api/v1/cluster/raft
```

```json
{
  "state": "leader",
  "term": 3,
  "commit_index": 1542,
  "last_applied": 1542,
  "log_length": 1542,
  "last_snapshot_index": 1000,
  "peers": [
    {"name": "node-2", "match_index": 1542, "next_index": 1543},
    {"name": "node-3", "match_index": 1540, "next_index": 1543}
  ]
}
```

---

## WebSocket Streams

Real-time event streams via WebSocket.

### WS /api/v1/ws/metrics

Real-time metrics stream (pushes every second).

```javascript
const ws = new WebSocket("ws://localhost:8081/api/v1/ws/metrics?token=your-token");
ws.onmessage = (event) => {
  const metrics = JSON.parse(event.data);
  console.log("RPS:", metrics.requests_per_second);
};
```

### WS /api/v1/ws/logs

Real-time log stream.

```javascript
const ws = new WebSocket("ws://localhost:8081/api/v1/ws/logs?token=your-token");
ws.onmessage = (event) => {
  const logEntry = JSON.parse(event.data);
  console.log(logEntry.level, logEntry.msg);
};
```

### WS /api/v1/ws/events

System events stream (config reloads, backend state changes, cluster events).

### WS /api/v1/ws/health

Health status change stream.

---

## Error Responses

All error responses follow a consistent format:

```json
{
  "error": "not_found",
  "message": "backend web-99 not found in pool web-backend",
  "status": 404
}
```

Common error codes:

| Status | Error | Description |
|--------|-------|-------------|
| 400 | `bad_request` | Invalid request body or parameters |
| 401 | `unauthorized` | Missing or invalid authentication |
| 404 | `not_found` | Resource does not exist |
| 409 | `conflict` | Resource already exists |
| 422 | `validation_error` | Request body fails validation |
| 500 | `internal_error` | Internal server error |
| 503 | `unavailable` | OLB is draining or shutting down |
