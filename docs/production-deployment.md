# Production Deployment Guide

Deploy OpenLoadBalancer in production environments with confidence.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Single Node Deployment](#single-node-deployment)
- [High Availability Deployment](#high-availability-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Docker Deployment](#docker-deployment)
- [Systemd Service](#systemd-service)
- [Security Hardening](#security-hardening)
- [Monitoring Setup](#monitoring-setup)
- [Backup and Recovery](#backup-and-recovery)
- [Performance Tuning](#performance-tuning)

---

## Architecture Overview

```
                    ┌─────────────────────────────────────┐
                    │           Load Balancer Tier         │
                    │         (OLB Cluster Nodes)          │
    Clients ────────┤    ┌─────┐    ┌─────┐    ┌─────┐    │
    HTTP/HTTPS      │    │OLB-1│◄──►│OLB-2│◄──►│OLB-3│    │
    WebSocket       │    └──┬──┘    └──┬──┘    └──┬──┘    │
    gRPC            │       │          │          │       │
                    │    ┌──┴──────────┴──────────┴──┐    │
                    │    │      Raft Consensus        │    │
                    │    │    (Cluster State Sync)    │    │
                    │    └────────────────────────────┘    │
                    └─────────────────────────────────────┘
                                    │
                    ┌───────────────┼───────────────┐
                    │               │               │
              ┌─────┴─────┐   ┌─────┴─────┐   ┌─────┴─────┐
              │  Backend  │   │  Backend  │   │  Backend  │
              │   Pool 1  │   │   Pool 2  │   │   Pool 3  │
              │ (App Svc) │   │  (API Svc)│   │(Admin Svc)│
              └───────────┘   └───────────┘   └───────────┘
```

---

## Single Node Deployment

### Hardware Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4+ cores |
| RAM | 2 GB | 4+ GB |
| Disk | 10 GB SSD | 50+ GB SSD |
| Network | 1 Gbps | 10+ Gbps |

### OS Configuration

```bash
# Increase file descriptor limits
cat >> /etc/security/limits.conf << 'EOF'
*    soft    nofile    65535
*    hard    nofile    65535
olb  soft    nofile    1048576
olb  hard    nofile    1048576
EOF

# Kernel tuning for high performance
cat >> /etc/sysctl.conf << 'EOF'
# Network performance
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 65536
net.ipv4.tcp_max_syn_backlog = 65536
net.ipv4.tcp_fin_timeout = 10
net.ipv4.tcp_keepalive_time = 1200
net.ipv4.tcp_max_tw_buckets = 5000
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_tw_recycle = 1
net.ipv4.ip_local_port_range = 1024 65535

# Connection tracking
net.netfilter.nf_conntrack_max = 1000000
net.ipv4.netfilter.ip_conntrack_tcp_timeout_established = 600

# Memory settings
vm.swappiness = 10
vm.dirty_ratio = 40
vm.dirty_background_ratio = 10
EOF

sysctl -p
```

### Production Config

```yaml
# /etc/olb/olb.yaml
version: 1

# Admin API (bind to localhost for security)
admin:
  address: "127.0.0.1:8081"
  mcp_address: "127.0.0.1:8082"
  mcp_audit: true

# Logging (structured JSON to file)
logging:
  level: info
  output: "/var/log/olb/olb.log"
  format: json
  rotation:
    max_size: 100  # MB
    max_backups: 10
    max_age: 30    # days
    compress: true

# Metrics for Prometheus
metrics:
  enabled: true
  path: /metrics

# TLS Configuration
tls:
  cert_file: "/etc/olb/certs/cert.pem"
  key_file: "/etc/olb/certs/key.pem"
  
# WAF (Web Application Firewall)
waf:
  enabled: true
  mode: enforce  # enforce, monitor, disabled
  ip_acl:
    enabled: true
    whitelist:
      - cidr: "10.0.0.0/8"
        reason: "internal"
    auto_ban:
      enabled: true
      default_ttl: "1h"
  rate_limit:
    enabled: true
    rules:
      - id: "per-ip"
        scope: "ip"
        limit: 1000
        window: "1m"
  sanitizer:
    enabled: true
  detection:
    enabled: true
    threshold:
      block: 50
      log: 25
    detectors:
      sqli: {enabled: true}
      xss: {enabled: true}
      pathtraversal: {enabled: true}
      cmdi: {enabled: true}
  bot_detection:
    enabled: true
    mode: monitor
  response:
    security_headers:
      enabled: true
    data_masking:
      enabled: true
      mask_credit_cards: true
      mask_ssn: true

# Middleware chain
middleware:
  rate_limit:
    enabled: true
    requests_per_second: 2000
    burst_size: 4000
  cors:
    enabled: true
    allowed_origins: ["https://example.com"]
    allowed_methods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    allow_credentials: true
  compression:
    enabled: true
    min_size: 1024
    level: 6
  circuit_breaker:
    enabled: true
    error_threshold: 50
    timeout: "30s"
  retry:
    enabled: true
    max_retries: 3
    retryable_statuses: [502, 503, 504]

# Listeners
listeners:
  # HTTP redirect to HTTPS
  - name: http-redirect
    protocol: http
    address: ":80"
    redirect_https: true
    
  # HTTPS with mTLS support
  - name: https
    protocol: https
    address: ":443"
    tls:
      cert_file: "/etc/olb/certs/cert.pem"
      key_file: "/etc/olb/certs/key.pem"
    routes:
      - name: api
        path: /api/
        pool: api-pool
        methods: [GET, POST, PUT, DELETE]
      - name: web
        path: /
        pool: web-pool

  # TCP for database
  - name: postgres-tcp
    protocol: tcp
    address: ":5432"
    pool: postgres-pool

# Backend pools
pools:
  - name: web-pool
    algorithm: least_connections
    health_check:
      type: http
      path: /health
      interval: 10s
      timeout: 5s
      healthy_threshold: 2
      unhealthy_threshold: 3
    backends:
      - id: web-1
        address: "10.0.1.10:8080"
        weight: 3
      - id: web-2
        address: "10.0.1.11:8080"
        weight: 3
      - id: web-3
        address: "10.0.1.12:8080"
        weight: 2

  - name: api-pool
    algorithm: round_robin
    health_check:
      type: http
      path: /api/health
      interval: 5s
      timeout: 3s
    backends:
      - id: api-1
        address: "10.0.2.10:8080"
      - id: api-2
        address: "10.0.2.11:8080"
      - id: api-3
        address: "10.0.2.12:8080"

  - name: postgres-pool
    algorithm: least_response_time
    health_check:
      type: tcp
      interval: 5s
      timeout: 3s
    backends:
      - id: db-primary
        address: "10.0.3.10:5432"
        weight: 5
      - id: db-replica-1
        address: "10.0.3.11:5432"
        weight: 3
      - id: db-replica-2
        address: "10.0.3.12:5432"
        weight: 3
```

---

## High Availability Deployment

### 3-Node Cluster Setup

```yaml
# Node 1: /etc/olb/olb.yaml
cluster:
  enabled: true
  node_id: "olb-node-1"
  bind_addr: "10.0.0.10"
  bind_port: 7946
  data_dir: "/var/lib/olb/cluster"
  peers:
    - "10.0.0.11:7946"
    - "10.0.0.12:7946"
  election_tick: "2s"
  heartbeat_tick: "500ms"
```

```yaml
# Node 2: /etc/olb/olb.yaml
cluster:
  enabled: true
  node_id: "olb-node-2"
  bind_addr: "10.0.0.11"
  bind_port: 7946
  data_dir: "/var/lib/olb/cluster"
  peers:
    - "10.0.0.10:7946"
    - "10.0.0.12:7946"
```

```yaml
# Node 3: /etc/olb/olb.yaml
cluster:
  enabled: true
  node_id: "olb-node-3"
  bind_addr: "10.0.0.12"
  bind_port: 7946
  data_dir: "/var/lib/olb/cluster"
  peers:
    - "10.0.0.10:7946"
    - "10.0.0.11:7946"
```

### VIP with Keepalived

```bash
# /etc/keepalived/keepalived.conf (on all nodes)
global_defs {
    router_id OLB_HA
}

vrrp_script check_olb {
    script "/usr/local/bin/check_olb.sh"
    interval 2
    weight 2
}

vrrp_instance VI_OLB {
    state MASTER  # BACKUP on node-2, node-3
    interface eth0
    virtual_router_id 51
    priority 101  # 100 on node-2, 99 on node-3
    advert_int 1
    
    authentication {
        auth_type PASS
        auth_pass secret123
    }
    
    virtual_ipaddress {
        10.0.0.100/24
    }
    
    track_script {
        check_olb
    }
}
```

```bash
# /usr/local/bin/check_olb.sh
#!/bin/bash
curl -sf http://localhost:8081/api/v1/status >/dev/null || exit 1
```

---

## Kubernetes Deployment

### Using Helm Chart

```bash
# Add repo (when published)
helm repo add olb https://openloadbalancer.github.io/helm-charts
helm repo update

# Install
helm install olb olb/olb \
  --namespace olb --create-namespace \
  --set replicaCount=3 \
  --set service.type=LoadBalancer \
  --set persistence.enabled=true \
  --set persistence.size=10Gi
```

### Manual Kubernetes Deploy

```yaml
# olb-namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: olb
---
# olb-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: olb-config
  namespace: olb
data:
  olb.yaml: |
    version: 1
    admin:
      address: "0.0.0.0:8081"
    listeners:
      - name: http
        protocol: http
        address: ":8080"
        routes:
          - path: /
            pool: web
    pools:
      - name: web
        algorithm: round_robin
        backends:
          - id: app-1
            address: "app-service:8080"
---
# olb-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: olb
  namespace: olb
spec:
  replicas: 3
  selector:
    matchLabels:
      app: olb
  template:
    metadata:
      labels:
        app: olb
    spec:
      containers:
        - name: olb
          image: openloadbalancer/olb:latest
          ports:
            - containerPort: 8080
            - containerPort: 8081
          volumeMounts:
            - name: config
              mountPath: /etc/olb
          resources:
            requests:
              memory: "512Mi"
              cpu: "500m"
            limits:
              memory: "2Gi"
              cpu: "2000m"
          livenessProbe:
            httpGet:
              path: /api/v1/status
              port: 8081
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /api/v1/status
              port: 8081
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: config
          configMap:
            name: olb-config
---
# olb-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: olb
  namespace: olb
spec:
  type: LoadBalancer
  selector:
    app: olb
  ports:
    - name: http
      port: 80
      targetPort: 8080
    - name: admin
      port: 8081
      targetPort: 8081
```

---

## Docker Deployment

```bash
# Create config directory
mkdir -p /opt/olb/{configs,certs,logs}

# Write production config
cat > /opt/olb/configs/olb.yaml << 'EOF'
version: 1
admin:
  address: "0.0.0.0:8081"
listeners:
  - name: http
    protocol: http
    address: ":8080"
    routes:
      - path: /
        pool: web
pools:
  - name: web
    algorithm: round_robin
    backends:
      - id: app-1
        address: "host.docker.internal:3001"
      - id: app-2
        address: "host.docker.internal:3002"
EOF

# Run container
docker run -d \
  --name olb \
  --restart unless-stopped \
  -p 80:8080 \
  -p 8081:8081 \
  -v /opt/olb/configs:/etc/olb/configs \
  -v /opt/olb/certs:/etc/olb/certs \
  -v /opt/olb/logs:/var/log/olb \
  openloadbalancer/olb:latest
```

---

## Systemd Service

```bash
# /etc/systemd/system/olb.service
[Unit]
Description=OpenLoadBalancer
Documentation=https://openloadbalancer.dev
Wants=network-online.target
After=network-online.target

[Service]
Type=notify
ExecStart=/usr/local/bin/olb start --config /etc/olb/olb.yaml
ExecReload=/bin/kill -HUP $MAINPID
ExecStop=/bin/kill -TERM $MAINPID
Restart=always
RestartSec=5

User=olb
Group=olb

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/olb /var/lib/olb
ReadOnlyPaths=/etc/olb

# Resource limits
LimitNOFILE=1048576
LimitNPROC=4096

# Environment
Environment="GOMAXPROCS=4"

[Install]
WantedBy=multi-user.target
```

```bash
# Setup
useradd -r -s /bin/false olb
mkdir -p /etc/olb /var/log/olb /var/lib/olb
chown -R olb:olb /etc/olb /var/log/olb /var/lib/olb

systemctl daemon-reload
systemctl enable olb
systemctl start olb
```

---

## Security Hardening

### File Permissions

```bash
# Certificates
chmod 600 /etc/olb/certs/*.pem
chown olb:olb /etc/olb/certs/*.pem

# Config
chmod 640 /etc/olb/olb.yaml
chown olb:olb /etc/olb/olb.yaml

# Log directory
chmod 755 /var/log/olb
chown olb:olb /var/log/olb
```

### Firewall Rules

```bash
# UFW
ufw default deny incoming
ufw allow 22/tcp    # SSH
ufw allow 80/tcp    # HTTP
ufw allow 443/tcp   # HTTPS
ufw allow from 10.0.0.0/8 to any port 8081  # Admin API internal only
ufw enable

# iptables
iptables -A INPUT -p tcp --dport 80 -j ACCEPT
iptables -A INPUT -p tcp --dport 443 -j ACCEPT
iptables -A INPUT -p tcp --dport 8081 -s 10.0.0.0/8 -j ACCEPT
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

### SELinux/AppArmor

```bash
# SELinux policy (olb.te)
module olb 1.0;

require {
    type http_port_t;
    type var_log_t;
    class tcp_socket { bind listen accept };
    class file { read write append };
}

# Allow binding to HTTP/HTTPS ports
allow olb_t http_port_t:tcp_socket { bind listen accept };

# Allow log writes
allow olb_t var_log_t:file { write append };
```

---

## Monitoring Setup

### Prometheus Configuration

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'olb'
    static_configs:
      - targets: ['olb-node-1:8081', 'olb-node-2:8081', 'olb-node-3:8081']
    metrics_path: /metrics
    scrape_interval: 15s
```

### Alertmanager Rules

```yaml
# olb-alerts.yml
groups:
  - name: olb
    rules:
      - alert: OLBDown
        expr: up{job="olb"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "OpenLoadBalancer is down"
          
      - alert: OLBBackendDown
        expr: olb_backend_up == 0
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Backend {{ $labels.backend }} is unhealthy"
          
      - alert: OLBHighLatency
        expr: olb_request_duration_seconds{quantile="0.99"} > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "99th percentile latency is high"
          
      - alert: OLBHighErrorRate
        expr: rate(olb_requests_total{status=~"5.."}[5m]) > 0.1
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "High error rate detected"
```

---

## Backup and Recovery

### Configuration Backup

```bash
#!/bin/bash
# /usr/local/bin/olb-backup.sh

BACKUP_DIR="/backup/olb/$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

# Backup config
cp /etc/olb/olb.yaml "$BACKUP_DIR/"

# Backup certificates
tar czf "$BACKUP_DIR/certs.tar.gz" /etc/olb/certs/

# Backup cluster state (if clustered)
if [ -d /var/lib/olb/cluster ]; then
    tar czf "$BACKUP_DIR/cluster.tar.gz" /var/lib/olb/cluster/
fi

# Upload to S3 (optional)
# aws s3 sync "$BACKUP_DIR" s3://my-backup-bucket/olb/$(date +%Y%m%d)/

# Keep only last 7 days
find /backup/olb -type d -mtime +7 -exec rm -rf {} \;
```

### Recovery Procedure

```bash
# 1. Stop OLB
systemctl stop olb

# 2. Restore config
cp /backup/olb/20260115/olb.yaml /etc/olb/

# 3. Restore certificates
tar xzf /backup/olb/20260115/certs.tar.gz -C /

# 4. Restore cluster state (if needed)
tar xzf /backup/olb/20260115/cluster.tar.gz -C /

# 5. Start OLB
systemctl start olb

# 6. Verify
olb status
```

---

## Performance Tuning

### Connection Tuning

```yaml
# High performance config
listeners:
  - name: https
    protocol: https
    address: ":443"
    # Performance settings
    read_timeout: "30s"
    write_timeout: "30s"
    idle_timeout: "120s"
    max_header_bytes: 1048576  # 1MB
    
pools:
  - name: web
    algorithm: least_connections
    connection_pool:
      enabled: true
      max_connections: 1000
      max_idle: 100
      idle_timeout: "10m"
    health_check:
      type: http
      path: /health
      interval: "5s"
      timeout: "2s"
```

### Worker Pool Sizing

```bash
# Set GOMAXPROCS to match CPU cores
export GOMAXPROCS=$(nproc)

# For containerized deployments
export GOMAXPROCS=$(grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)
```

### Benchmark Your Setup

```bash
# Install vegeta
go install github.com/tsenart/vegeta@latest

# Run benchmark
echo "GET https://your-domain.com/" | vegeta attack \
  -duration=60s \
  -rate=10000 \
  -connections=10000 | vegeta report
```

---

## Health Checks

### Load Balancer Health

```bash
# Systemd health check
curl -sf http://localhost:8081/api/v1/status | jq

# Expected output:
{
  "state": "running",
  "uptime": "72h15m30s",
  "version": "0.1.0",
  "listeners": 2,
  "pools": 3,
  "routes": 5
}
```

### Backend Health

```bash
# Check all backends
olb backend list

# Check specific pool
curl http://localhost:8081/api/v1/backends/web-pool | jq
```

---

## Upgrade Procedure

### Zero-Downtime Upgrade

```bash
# 1. Verify backup exists
ls -la /backup/olb/$(date +%Y%m%d)/

# 2. Download new version
curl -L -o olb-new https://github.com/openloadbalancer/olb/releases/latest/download/olb-linux-amd64
chmod +x olb-new

# 3. Test config with new binary
./olb-new config validate --config /etc/olb/olb.yaml

# 4. Hot reload (if supported)
# OR graceful restart:
systemctl reload olb

# 5. Verify
olb version
curl http://localhost:8081/api/v1/status | jq .version
```

---

## Troubleshooting

See [Troubleshooting Playbook](troubleshooting.md) for detailed debugging procedures.
