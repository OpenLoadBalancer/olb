# Troubleshooting Playbook

Quick reference for diagnosing and resolving OpenLoadBalancer issues in production.

## Table of Contents

- [Quick Diagnostics](#quick-diagnostics)
- [Startup Issues](#startup-issues)
- [Connection Issues](#connection-issues)
- [Backend Issues](#backend-issues)
- [Performance Issues](#performance-issues)
- [TLS/SSL Issues](#tlsssl-issues)
- [WAF Issues](#waf-issues)
- [Cluster Issues](#cluster-issues)
- [Memory Issues](#memory-issues)
- [Log Analysis](#log-analysis)

---

## Quick Diagnostics

### System Status Check

```bash
# 1. Check if OLB is running
systemctl status olb

# 2. Get process info
ps aux | grep olb

# 3. Check listening ports
ss -tlnp | grep olb
netstat -tlnp | grep olb

# 4. Check file descriptors
cat /proc/$(pgrep olb)/limits | grep "Max open files"
ls /proc/$(pgrep olb)/fd | wc -l

# 5. Network connections
ss -s
conntrack -L | wc -l

# 6. Resource usage
top -p $(pgrep olb)
htop -p $(pgrep olb)
```

### Health Check Script

```bash
#!/bin/bash
# olb-health-check.sh

echo "=== OpenLoadBalancer Health Check ==="

# Process check
if pgrep -x "olb" > /dev/null; then
    echo "✓ Process running"
else
    echo "✗ Process NOT running"
    exit 1
fi

# Port check
if ss -tln | grep -q ":80\|:443\|:8080"; then
    echo "✓ Listening on expected ports"
else
    echo "✗ Not listening on expected ports"
fi

# Admin API check
if curl -sf http://localhost:8081/api/v1/status > /dev/null; then
    echo "✓ Admin API responding"
    curl -s http://localhost:8081/api/v1/status | jq
else
    echo "✗ Admin API not responding"
fi

# Backend health
BACKENDS=$(curl -sf http://localhost:8081/api/v1/backends | jq -r '.[].health' | grep -c "healthy" || echo "0")
TOTAL=$(curl -sf http://localhost:8081/api/v1/backends | jq 'length' || echo "0")
echo "✓ Backends: $BACKENDS/$TOTAL healthy"

# Connection count
CONN_COUNT=$(ss -tan | grep -c ":80\|:443" || echo "0")
echo "✓ Active connections: $CONN_COUNT"

# File descriptors
FD_COUNT=$(ls /proc/$(pgrep olb)/fd 2>/dev/null | wc -l)
FD_LIMIT=$(cat /proc/$(pgrep olb)/limits 2>/dev/null | grep "Max open files" | awk '{print $5}')
echo "✓ File descriptors: $FD_COUNT / $FD_LIMIT"

echo "=== Check Complete ==="
```

---

## Startup Issues

### OLB Won't Start

#### Symptom
```
systemctl start olb
Job for olb.service failed.
```

#### Diagnosis
```bash
# Check logs
journalctl -u olb -n 100

# Validate config
olb config validate --config /etc/olb/olb.yaml

# Check permissions
ls -la /etc/olb/
ls -la /var/log/olb/
ls -la /var/lib/olb/

# Test as root
sudo -u olb /usr/local/bin/olb start --config /etc/olb/olb.yaml
```

#### Common Causes

**1. Config File Error**
```
Error: yaml: line 15: did not find expected key
```
Fix:
```bash
# Validate YAML syntax
yamllint /etc/olb/olb.yaml

# Check indentation (spaces, not tabs)
cat -A /etc/olb/olb.yaml | grep "^I"
```

**2. Permission Denied**
```
Error: open /var/log/olb/olb.log: permission denied
```
Fix:
```bash
chown -R olb:olb /var/log/olb
chmod 755 /var/log/olb
```

**3. Port Already in Use**
```
Error: listen tcp :80: bind: address already in use
```
Fix:
```bash
# Find process using port
ss -tlnp | grep ":80"
fuser 80/tcp

# Stop conflicting service
systemctl stop apache2 nginx

# Or change OLB port
```

**4. Certificate Not Found**
```
Error: open /etc/olb/certs/cert.pem: no such file
```
Fix:
```bash
# Verify paths
ls -la /etc/olb/certs/

# Check certificate validity
openssl x509 -in /etc/olb/certs/cert.pem -text -noout
```

### OLB Crashes on Start

#### Get Stack Trace
```bash
# Enable debug logging
olb start --config /etc/olb/olb.yaml --log-level debug

# Run with strace (last resort)
strace -f -o /tmp/olb.strace /usr/local/bin/olb start --config /etc/olb/olb.yaml
```

#### Check for Panics
```bash
# Search logs for panics
grep -i "panic" /var/log/olb/olb.log
grep -i "fatal" /var/log/olb/olb.log
```

---

## Connection Issues

### Clients Can't Connect

#### Diagnosis
```bash
# Test from localhost
curl -v http://localhost:8080/

# Test from remote
curl -v http://olb-server:80/

# Test with different source IPs
curl -v --interface eth1 http://olb-server:80/

# Check firewall
iptables -L -n | grep 80
iptables -L -n | grep 443
ufw status

# Check SELinux
getenforce
sealert -a /var/log/audit/audit.log
```

#### Common Causes

**1. Firewall Blocking**
```bash
# Allow traffic
ufw allow 80/tcp
ufw allow 443/tcp

# Or iptables
iptables -A INPUT -p tcp --dport 80 -j ACCEPT
iptables -A INPUT -p tcp --dport 443 -j ACCEPT
```

**2. SELinux Denying**
```bash
# Check for denials
ausearch -m avc -ts recent

# Temporarily disable (testing only)
setenforce 0

# Create policy module
audit2allow -a -M olb_fix
semodule -i olb_fix.pp
```

**3. Wrong Bind Address**
```yaml
# Wrong: binds only to localhost
listeners:
  - address: "127.0.0.1:8080"

# Correct: binds to all interfaces
listeners:
  - address: ":8080"
```

### Connection Drops

#### Diagnosis
```bash
# Monitor connections in real-time
watch -n 1 'ss -tan | grep -c ESTABLISHED'

# Check for TIME_WAIT
ss -tan | awk '{print $1}' | sort | uniq -c

# Check conntrack table
conntrack -L | wc -l
cat /proc/sys/net/netfilter/nf_conntrack_max

# Check for SYN flooding
netstat -s | grep -i syn
cat /proc/sys/net/ipv4/tcp_syncookies
```

#### Fixes

**1. Increase Connection Tracking**
```bash
# /etc/sysctl.conf
net.netfilter.nf_conntrack_max = 2000000
net.ipv4.tcp_max_tw_buckets = 2000000
net.ipv4.tcp_tw_reuse = 1
```

**2. Enable SYN Cookies**
```bash
echo 1 > /proc/sys/net/ipv4/tcp_syncookies
```

**3. Tune TCP Keepalive**
```bash
net.ipv4.tcp_keepalive_time = 1200
net.ipv4.tcp_keepalive_probes = 5
net.ipv4.tcp_keepalive_intvl = 15
```

### Slow Connection Establishment

#### Diagnosis
```bash
# Time connection setup
time curl -o /dev/null -s http://olb-server/

# Check DNS resolution time
dig +stats olb-server

# Check backend connection time
time curl -o /dev/null -s http://backend-server:8080/
```

#### Common Causes
- DNS resolution delays
- Backend slow to accept connections
- TLS handshake overhead
- Proxy protocol overhead

---

## Backend Issues

### Backend Marked Unhealthy

#### Diagnosis
```bash
# Check backend health endpoint manually
curl -v http://backend:8080/health

# Check from OLB node
curl -v --resolve backend:8080:BACKEND_IP http://backend:8080/health

# Check logs
grep "backend" /var/log/olb/olb.log | tail -50

# Verify health check config
grep -A 10 "health_check" /etc/olb/olb.yaml
```

#### Common Causes

**1. Wrong Health Check Path**
```yaml
# Check if backend returns 200 on this path
health_check:
  type: http
  path: /health  # Verify this exists!
  interval: 10s
  timeout: 5s
```

**2. Backend Overloaded**
```bash
# Check backend resources
ssh backend "top -bn1 | head -20"
ssh backend "df -h"
ssh backend "free -m"
```

**3. Network Issues**
```bash
# Test connectivity
ping -c 10 backend
mtr -n backend

# Test port
telnet backend 8080
nc -zv backend 8080
```

### All Backends Down

#### Emergency Response
```bash
# 1. Check if backends are actually up
for backend in backend1 backend2 backend3; do
    curl -s -o /dev/null -w "%{http_code}" http://$backend:8080/health
done

# 2. Disable health checks temporarily (emergency)
# Edit config to set all backends to 'up' manually

# 3. Restart OLB
systemctl restart olb

# 4. Check for network partition
ip addr show
ip route show
```

### Uneven Load Distribution

#### Diagnosis
```bash
# Check backend metrics
curl http://localhost:8081/api/v1/metrics | jq '.backends'

# Compare connection counts
ss -tan | awk '{print $5}' | grep -E "(backend1|backend2|backend3)" | sort | uniq -c

# Check algorithm settings
grep "algorithm" /etc/olb/olb.yaml
```

#### Fixes

**1. Switch Algorithm**
```yaml
# For long-lived connections
algorithm: least_connections

# For latency-sensitive
algorithm: least_response_time

# For session affinity
algorithm: ip_hash
```

**2. Check Backend Weights**
```yaml
backends:
  - id: backend-1
    address: "10.0.1.10:8080"
    weight: 3
  - id: backend-2
    address: "10.0.1.11:8080"
    weight: 3  # Equal weight for even distribution
```

---

## Performance Issues

### High Latency

#### Diagnosis
```bash
# Measure latency at each hop
# 1. Direct to backend
time curl http://backend:8080/

# 2. Through OLB
time curl http://olb/

# 3. Compare percentiles
curl http://olb:8081/metrics | grep request_duration_seconds

# Check system resources
vmstat 1 10
iostat -x 1 10
sar -u 1 10
```

#### Common Causes

**1. Backend Slow**
```bash
# Check backend response time
for i in {1..100}; do
    curl -s -o /dev/null -w "%{time_total}\n" http://backend:8080/
done | sort -n | awk '{all[NR] = $0} END{print "50%:", all[int(NR*0.5)], "99%:", all[int(NR*0.99)]}'
```

**2. Middleware Overhead**
```yaml
# Temporarily disable middleware to test
middleware:
  waf:
    enabled: false  # Test without WAF
  compression:
    enabled: false  # Test without compression
```

**3. Connection Pool Exhaustion**
```bash
# Check active connections
ss -tan | grep ESTABLISHED | wc -l

# Check pool settings
grep -A 5 "connection_pool" /etc/olb/olb.yaml
```

### High CPU Usage

#### Diagnosis
```bash
# Profile OLB
perf top -p $(pgrep olb)

# Check Go runtime stats
curl http://localhost:8081/api/v1/metrics | grep go_

# Check GOMAXPROCS
cat /proc/$(pgrep olb)/environ | tr '\0' '\n' | grep GOMAXPROCS
```

#### Fixes

**1. Adjust GOMAXPROCS**
```bash
# Set to number of CPU cores
export GOMAXPROCS=$(nproc)

# Or in systemd
[Service]
Environment="GOMAXPROCS=4"
```

**2. Optimize WAF Rules**
```yaml
waf:
  detection:
    enabled: true
    threshold:
      block: 100  # Increase threshold
      log: 50
```

### High Memory Usage

#### Diagnosis
```bash
# Memory usage over time
watch -n 5 'pmap $(pgrep olb) | tail -1'

# Go memory stats
curl -s http://localhost:8081/debug/pprof/heap > heap.out
go tool pprof heap.out

# Check for memory leaks
ps aux | grep olb | awk '{print $6}'  # RSS column
```

#### Fixes

**1. Enable GC Tuning**
```bash
# /etc/systemd/system/olb.service
[Service]
Environment="GOGC=50"  # Aggressive GC
Environment="GOMEMLIMIT=4GiB"
```

**2. Limit Connection Pool**
```yaml
connection_pool:
  enabled: true
  max_connections: 500  # Reduce from 1000
  max_idle: 50
  idle_timeout: "5m"
```

---

## TLS/SSL Issues

### Certificate Errors

#### Diagnosis
```bash
# Check certificate
openssl x509 -in /etc/olb/certs/cert.pem -text -noout

# Verify certificate chain
openssl verify -CAfile /etc/olb/certs/ca.pem /etc/olb/certs/cert.pem

# Check certificate dates
openssl x509 -in /etc/olb/certs/cert.pem -noout -dates

# Test TLS connection
openssl s_client -connect olb:443 -servername olb
```

#### Common Errors

**1. Certificate Expired**
```bash
# Check expiry
echo | openssl s_client -servername olb -connect olb:443 2>/dev/null | openssl x509 -noout -dates

# Auto-renewal with ACME
tls:
  acme:
    enabled: true
    email: admin@example.com
    domains:
      - example.com
```

**2. Wrong Certificate Chain**
```bash
# Combine certificates
cat cert.pem intermediate.pem > fullchain.pem

# Update config
tls:
  cert_file: "/etc/olb/certs/fullchain.pem"
  key_file: "/etc/olb/certs/key.pem"
```

### mTLS Issues

#### Diagnosis
```bash
# Test with client cert
curl --cert client.crt --key client.key https://olb/

# Check client CA
curl -v https://olb/ 2>&1 | grep "verify error"

# Verify cipher suites
nmap --script ssl-enum-ciphers -p 443 olb
```

---

## WAF Issues

### False Positives

#### Diagnosis
```bash
# Check WAF logs
grep "waf" /var/log/olb/olb.log | grep "blocked"

# Check detection scores
grep "waf_score" /var/log/olb/olb.log

# Review triggered rules
curl http://localhost:8081/api/v1/waf/blocks | jq
```

#### Fixes

**1. Switch to Monitor Mode**
```yaml
waf:
  enabled: true
  mode: monitor  # Don't block, just log
```

**2. Whitelist IP**
```yaml
waf:
  ip_acl:
    enabled: true
    whitelist:
      - cidr: "10.0.0.0/8"
        reason: "internal_trusted"
```

**3. Adjust Thresholds**
```yaml
waf:
  detection:
    threshold:
      block: 100  # Increase from 50
      log: 50
```

### WAF Bypass

#### Security Check
```bash
# Test common bypasses
curl "http://olb/<script>alert(1)</script>"
curl "http://olb/?id=1' OR '1'='1"
curl "http://olb/..%2f..%2fetc%2fpasswd"

# Check response headers
curl -I http://olb/ | grep -i security
```

#### Hardening
```yaml
waf:
  mode: enforce
  sanitizer:
    enabled: true
    strict: true
  response:
    security_headers:
      enabled: true
      x_frame_options: "DENY"
      x_content_type_options: "nosniff"
```

---

## Cluster Issues

### Node Can't Join Cluster

#### Diagnosis
```bash
# Check cluster status
olb cluster status

# Check gossip port
ss -tlnp | grep 7946

# Check firewall between nodes
nc -zv node-2 7946
nc -zv node-3 7946

# Check logs
grep "cluster" /var/log/olb/olb.log
```

#### Fixes

**1. Open Cluster Ports**
```bash
# Firewall rules
ufw allow from 10.0.0.0/8 to any port 7946
ufw allow from 10.0.0.0/8 to any port 7947

# Or iptables
iptables -A INPUT -p tcp --dport 7946 -s 10.0.0.0/8 -j ACCEPT
iptables -A INPUT -p udp --dport 7946 -s 10.0.0.0/8 -j ACCEPT
```

**2. Verify Cluster Config**
```yaml
cluster:
  enabled: true
  node_id: "olb-node-1"  # Must be unique per node!
  bind_addr: "10.0.0.10"  # Must be reachable IP
  bind_port: 7946
  peers:
    - "10.0.0.11:7946"
    - "10.0.0.12:7946"
```

### Split Brain

#### Diagnosis
```bash
# Check each node thinks it's leader
curl http://node-1:8081/api/v1/cluster/leader
curl http://node-2:8081/api/v1/cluster/leader
curl http://node-3:8081/api/v1/cluster/leader

# Should all return same leader
```

#### Recovery
```bash
# 1. Stop all nodes
systemctl stop olb

# 2. Clear Raft state on all nodes
rm -rf /var/lib/olb/cluster/raft/

# 3. Start nodes one by one
systemctl start olb
sleep 10

# 4. Verify cluster forms
curl http://node-1:8081/api/v1/cluster/members
```

---

## Memory Issues

### OOM Killed

#### Diagnosis
```bash
# Check OOM events
dmesg | grep -i "killed process"
journalctl -k | grep -i "oom"

# Check memory limit
cat /proc/$(pgrep olb)/status | grep -E "VmRSS|VmSize"
systemctl show olb | grep Memory
```

#### Fixes

**1. Increase Memory Limit**
```bash
# /etc/systemd/system/olb.service
[Service]
MemoryLimit=8G
```

**2. Set Go Memory Limit**
```bash
Environment="GOMEMLIMIT=6GiB"
```

**3. Reduce Cache Size**
```yaml
middleware:
  cache:
    enabled: true
    max_size: 100  # Reduce from 1000
```

---

## Log Analysis

### Common Log Patterns

#### Startup Success
```json
{"ts":"2026-01-15T10:00:00Z","level":"info","msg":"OpenLoadBalancer v1.0.0 starting"}
{"ts":"2026-01-15T10:00:00Z","level":"info","msg":"Listener started","name":"https","address":":443"}
{"ts":"2026-01-15T10:00:00Z","level":"info","msg":"Engine started successfully"}
```

#### Backend Down
```json
{"ts":"2026-01-15T10:05:00Z","level":"warn","msg":"Health check failed","backend":"app-1","error":"connection refused"}
{"ts":"2026-01-15T10:05:00Z","level":"warn","msg":"Backend marked unhealthy","backend":"app-1"}
```

#### WAF Block
```json
{"ts":"2026-01-15T10:10:00Z","level":"warn","msg":"WAF blocked request","ip":"192.168.1.100","rule":"sqli","score":75}
```

#### Rate Limit Hit
```json
{"ts":"2026-01-15T10:15:00Z","level":"warn","msg":"Rate limit exceeded","ip":"10.0.0.50","limit":100}
```

### Log Query Examples

```bash
# Find slow requests
grep "duration" /var/log/olb/olb.log | jq 'select(.duration > 1)'

# Find errors by backend
grep "error" /var/log/olb/olb.log | jq -s 'group_by(.backend) | map({backend: .[0].backend, count: length})'

# Find top blocked IPs
grep "WAF blocked" /var/log/olb/olb.log | jq -r '.ip' | sort | uniq -c | sort -rn | head -10

# Request rate over time
grep "request" /var/log/olb/olb.log | jq -r '.ts[0:16]' | sort | uniq -c
```

---

## Emergency Procedures

### Complete Outage Recovery

```bash
#!/bin/bash
# emergency-recovery.sh

echo "=== Emergency OLB Recovery ==="

# 1. Check if process exists
if ! pgrep olb; then
    echo "OLB not running, attempting restart..."
    systemctl restart olb
    sleep 5
fi

# 2. Check if listening
if ! ss -tln | grep -q ":80"; then
    echo "Not listening on port 80!"
    echo "Checking for port conflicts..."
    ss -tlnp | grep ":80"
fi

# 3. Test local health
if ! curl -sf http://localhost:8081/api/v1/status; then
    echo "Admin API not responding!"
    echo "Last 50 log lines:"
    tail -50 /var/log/olb/olb.log
    exit 1
fi

# 4. Check backends
BACKEND_COUNT=$(curl -sf http://localhost:8081/api/v1/backends | jq 'length')
HEALTHY_COUNT=$(curl -sf http://localhost:8081/api/v1/backends | jq '[.[] | select(.health == "healthy")] | length')

echo "Backends: $HEALTHY_COUNT / $BACKEND_COUNT healthy"

if [ "$HEALTHY_COUNT" -eq 0 ]; then
    echo "WARNING: No healthy backends!"
    echo "Checking backend connectivity..."
fi

echo "=== Recovery Complete ==="
```

### Graceful Shutdown

```bash
# Stop accepting new connections
systemctl stop olb

# Or send TERM signal
kill -TERM $(pgrep olb)

# Wait for graceful shutdown (30s default)
timeout 35 tail --pid=$(pgrep olb) -f /dev/null

# Force kill if still running
if pgrep olb; then
    kill -9 $(pgrep olb)
fi
```

---

## Getting Help

### Information to Collect

When reporting issues, include:

1. **OLB Version**: `olb version`
2. **Config** (sanitized): `/etc/olb/olb.yaml`
3. **Last 100 log lines**: `tail -100 /var/log/olb/olb.log`
4. **System info**: `uname -a`, `go version`
5. **Resource usage**: `free -m`, `df -h`
6. **Network**: `ss -tan`, `iptables -L -n`

### Debug Mode

```bash
# Enable maximum logging
olb start --config /etc/olb/olb.yaml --log-level debug

# Or in config
logging:
  level: debug
  output: stdout
```

### Support Channels

- Documentation: https://openloadbalancer.dev/docs
- GitHub Issues: https://github.com/openloadbalancer/olb/issues
- MCP Chat: Connect via MCP server for AI-assisted debugging
