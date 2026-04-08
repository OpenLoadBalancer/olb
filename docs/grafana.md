# Grafana Dashboard Setup

OpenLoadBalancer ships with a pre-built Grafana dashboard for monitoring request throughput, latency, backend health, connection pools, and WAF activity.

## Prerequisites

- Grafana 9.0+ (open source or Cloud)
- Prometheus data source configured and scraping OLB's `/metrics` endpoint

## Import the Dashboard

### Option 1: File Upload

1. Open Grafana → **Dashboards** → **Import**
2. Click **Upload dashboard JSON file**
3. Select `deploy/grafana/dashboard.json` from this repository
4. Select your Prometheus data source
5. Click **Import**

### Option 2: Paste JSON

1. Open Grafana → **Dashboards** → **Import**
2. Copy the contents of `deploy/grafana/dashboard.json`
3. Paste into the **Import via dashboard JSON model** text area
4. Select your Prometheus data source
5. Click **Import**

### Option 3: Grafana Provisioning

Add this to your Grafana provisioning config (`/etc/grafana/provisioning/dashboards/dashboards.yml`):

```yaml
apiVersion: 1
providers:
  - name: 'OLB'
    orgId: 1
    folder: ''
    type: file
    disableDeletion: false
    editable: true
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
```

Then copy `deploy/grafana/dashboard.json` to `/var/lib/grafana/dashboards/olb.json` and restart Grafana.

If using Docker Compose, mount the dashboard file:

```yaml
volumes:
  - ./deploy/grafana/dashboard.json:/var/lib/grafana/dashboards/olb.json:ro
```

## Dashboard Panels

The dashboard includes the following sections:

| Section | Metrics |
|---------|---------|
| **Overview** | Total requests, active connections, uptime |
| **Request Rate** | Requests/sec by pool, method, status code |
| **Latency** | P50, P90, P99 request duration histograms |
| **Backend Health** | Healthy/unhealthy backends, health check results |
| **Connection Pools** | Idle/active connections, hits, misses, evictions |
| **WAF** | Blocked requests, detection rates by attack type |
| **Errors** | Error rate by type, 5xx responses |

## Prometheus Scrape Config

Make sure Prometheus is configured to scrape OLB:

```yaml
scrape_configs:
  - job_name: 'olb'
    static_configs:
      - targets: ['olb:9090']  # admin address from config
    metrics_path: '/metrics'
    scrape_interval: 15s
```

## Template Variables

The dashboard uses these variables:

- `$datasource` — Prometheus data source (selected at import time)
- `$pool` — Backend pool name (filters all panels)
- `$interval` — Time aggregation interval (defaults to scrape interval)

## Customizing

The dashboard is fully editable. Common customizations:

- **Add alerts**: Panel menu → Edit → Alert tab → set threshold rules
- **Change refresh**: Top bar → refresh interval dropdown (5s recommended)
- **Add annotations**: Dashboard settings → Annotations → add deployment markers
