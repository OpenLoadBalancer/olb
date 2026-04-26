# OpenLoadBalancer Deployment

## Quick Start with Docker

### 1. Pull the image
```bash
docker pull ghcr.io/openloadbalancer/olb:latest
```

### 2. Create your configuration
```bash
mkdir -p configs certs
# Create olb.yaml in configs/ directory
```

### 3. Run
```bash
docker compose up -d
```

## Docker Compose Files

### Single Node (`deploy/docker-compose.yml`)
Full stack with Prometheus, Grafana, and Alertmanager.

```yaml
services:
  olb:
    image: ghcr.io/openloadbalancer/olb:latest
    ports:
      - "80:80"      # HTTP
      - "443:443"    # HTTPS
      - "8081:8081"  # Admin API
      - "8082:8082"  # MCP Server
    volumes:
      - ./configs/olb.yaml:/etc/olb/configs/olb.yaml:ro
      - ./certs:/etc/olb/certs:ro
      - olb-logs:/var/log/olb
    environment:
      - OLB_CONFIG=/etc/olb/configs/olb.yaml
      - OLB_LOG_LEVEL=info
```

### 3-Node Cluster (`docker-compose.yml` in repo root)
See cluster deployment in the repo root.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OLB_CONFIG` | `/etc/olb/olb.yaml` | Config file path |
| `OLB_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `OLB_ADMIN_ADDR` | `:8081` | Admin API address |
| `GRAFANA_ADMIN_PASSWORD` | `changeme` | Grafana admin password |

## Ports

| Port | Service | Description |
|------|---------|-------------|
| 80 | HTTP | Load balancer HTTP listener |
| 443 | HTTPS | Load balancer HTTPS listener |
| 8081 | Admin API | REST API for management |
| 8082 | MCP Server | Model Context Protocol |
| 9090 | Prometheus | Metrics collection |
| 9093 | Alertmanager | Alert routing |
| 3000 | Grafana | Dashboards |

## Example Config

```yaml
listeners:
  - name: http
    address: ":80"
    protocol: http

pools:
  - name: web
    algorithm: round_robin
    backends:
      - address: "localhost:8080"
      - address: "localhost:8081"

admin:
  address: ":8081"
```

## Health Check

```bash
docker exec olb olb health
```

## Observability Stack

The full stack includes:
- **Prometheus** — Metrics collection (port 9090)
- **Grafana** — Visualization (port 3000)
- **Alertmanager** — Alert routing (port 9093)
- **Node Exporter** — System metrics

Start with observability:
```bash
docker compose up -d prometheus grafana alertmanager
```

## TLS Certificates

Place certificates in `./certs/`:
```
certs/
├── server.crt   # TLS certificate
└── server.key   # TLS private key
```
