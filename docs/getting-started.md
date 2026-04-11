# Getting Started with OpenLoadBalancer

Get OLB running in under 5 minutes.

## Prerequisites

- **Go 1.23+** (for building from source)
- **Linux, macOS, or Windows**
- Backend services to load balance (or use the examples below)

## Installation

### Binary Download

Download a pre-built binary from the [releases page](https://github.com/openloadbalancer/olb/releases):

```bash
# Linux (amd64)
curl -L https://github.com/openloadbalancer/olb/releases/latest/download/olb-linux-amd64 -o olb
chmod +x olb
sudo mv olb /usr/local/bin/

# macOS (Apple Silicon)
curl -L https://github.com/openloadbalancer/olb/releases/latest/download/olb-darwin-arm64 -o olb
chmod +x olb
sudo mv olb /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/openloadbalancer/olb/releases/latest/download/olb-darwin-amd64 -o olb
chmod +x olb
sudo mv olb /usr/local/bin/
```

### Go Install

```bash
go install github.com/openloadbalancer/olb/cmd/olb@latest
```

### Build from Source

```bash
git clone https://github.com/openloadbalancer/olb.git
cd olb
make build
# Binary at ./bin/olb
```

### Docker

```bash
docker run -d \
  --name olb \
  -p 80:80 -p 443:443 -p 8081:8081 \
  -v $(pwd)/olb.yaml:/etc/olb/configs/olb.yaml \
  openloadbalancer/olb:latest
```

## Minimal Configuration

Create a file named `olb.yaml`:

```yaml
version: 1

listeners:
  - name: http
    protocol: http
    address: ":8080"
    routes:
      - name: default
        path: /
        pool: backend

pools:
  - name: backend
    algorithm: round_robin
    backends:
      - id: app-1
        address: "127.0.0.1:3001"
      - id: app-2
        address: "127.0.0.1:3002"
      - id: app-3
        address: "127.0.0.1:3003"

logging:
  level: info
  output: stdout
```

This configures OLB to listen on port 8080 and distribute requests across three backends using round-robin.

## Start the Load Balancer

```bash
olb start --config olb.yaml
```

You should see output like:

```
{"ts":"2026-03-15T10:00:00Z","level":"info","msg":"OpenLoadBalancer v0.1.0 starting"}
{"ts":"2026-03-15T10:00:00Z","level":"info","msg":"Listener started","name":"http","address":":8080"}
```

## Verify It Works

Start some test backend servers (in separate terminals):

```bash
# Terminal 1 - Backend on port 3001
python3 -c "
from http.server import HTTPServer, BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'Hello from backend 3001')
HTTPServer(('', 3001), H).serve_forever()
"

# Terminal 2 - Backend on port 3002
python3 -c "
from http.server import HTTPServer, BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'Hello from backend 3002')
HTTPServer(('', 3002), H).serve_forever()
"

# Terminal 3 - Backend on port 3003
python3 -c "
from http.server import HTTPServer, BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'Hello from backend 3003')
HTTPServer(('', 3003), H).serve_forever()
"
```

Send requests through the load balancer:

```bash
# Each request goes to a different backend (round-robin)
curl http://localhost:8080/
# Hello from backend 3001

curl http://localhost:8080/
# Hello from backend 3002

curl http://localhost:8080/
# Hello from backend 3003

curl http://localhost:8080/
# Hello from backend 3001 (cycles back)
```

## Add Health Checks

Update `olb.yaml` to include health checks:

```yaml
pools:
  - name: backend
    algorithm: round_robin
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
      healthy_threshold: 2
      unhealthy_threshold: 3
    backends:
      - id: app-1
        address: "127.0.0.1:3001"
      - id: app-2
        address: "127.0.0.1:3002"
      - id: app-3
        address: "127.0.0.1:3003"
```

Reload the configuration without downtime:

```bash
olb reload
```

OLB will now probe each backend's `/health` endpoint every 10 seconds. Backends that fail 3 consecutive checks are removed from rotation. They are re-added after 2 consecutive successes.

## View Metrics

### Enable the Admin API

Add the admin section to `olb.yaml`:

```yaml
admin:
  enabled: true
  address: "127.0.0.1:8081"

metrics:
  enabled: true
  path: /metrics
```

### JSON Metrics

```bash
curl http://localhost:8081/api/v1/metrics | jq
```

```json
{
  "uptime_seconds": 3600,
  "total_requests": 15234,
  "active_connections": 42,
  "backends": {
    "backend": {
      "app-1": {"status": "healthy", "connections": 14, "rps": 120},
      "app-2": {"status": "healthy", "connections": 12, "rps": 115},
      "app-3": {"status": "healthy", "connections": 16, "rps": 118}
    }
  }
}
```

### Prometheus Metrics

```bash
curl http://localhost:8081/metrics
```

```
# HELP olb_backend_up Whether backend is healthy
# TYPE olb_backend_up gauge
olb_backend_up{pool="backend",backend="app-1"} 1
olb_backend_up{pool="backend",backend="app-2"} 1
olb_backend_up{pool="backend",backend="app-3"} 1

# HELP olb_route_requests_total Total requests per route
# TYPE olb_route_requests_total counter
olb_route_requests_total{route="default",method="GET",status="200"} 15234
```

Point Prometheus at `http://olb-host:8081/metrics` to scrape these metrics.

### Live TUI Dashboard

```bash
olb top
```

This opens an htop-style terminal UI showing real-time request rates, backend health, latency percentiles, and connection counts.

## CLI Commands

```bash
# Check system status
olb status

# List backends and their health
olb backend list

# View metrics summary
olb metrics show

# Validate a config file
olb config validate --config olb.yaml

# Hot-reload configuration
olb reload
```

## Next Steps

- [Configuration Reference](configuration.md) -- All config options explained
- [Load Balancing Algorithms](algorithms.md) -- Choose the right algorithm
- [REST API Reference](api.md) -- Manage OLB programmatically
- [Clustering Guide](clustering.md) -- Set up multi-node deployments
- [MCP / AI Integration](mcp.md) -- Use OLB with AI agents
