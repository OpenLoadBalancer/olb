# OpenLoadBalancer Scripts

> Production automation scripts for OpenLoadBalancer deployment and operations

## Overview

This directory contains production-ready scripts for deploying, monitoring, and maintaining OpenLoadBalancer.

## Scripts

### `deploy.sh` - Deployment Automation

Automated deployment script with blue-green deployment strategy for production.

**Usage:**
```bash
# Deploy to staging
./deploy.sh staging

# Deploy specific version to production
./deploy.sh production v1.2.3

# Show help
./deploy.sh --help
```

**Features:**
- Pre-deployment checks (Docker, config)
- Build frontend and Go binary
- Run test suite
- Build Docker image
- Blue-green deployment for production
- Automatic rollback on failure
- Health check verification

### `health-check.sh` - Health Monitoring

Comprehensive health check script for monitoring OLB instances.

**Usage:**
```bash
# Check local instance
./health-check.sh

# Check remote instance
./health-check.sh http://olb-server:8081

# Show help
./health-check.sh --help
```

**Checks:**
- Process running status
- TCP port connectivity
- HTTP endpoints (health, metrics)
- JSON API responses
- Backend pool health
- Disk space
- Memory usage
- Recent error logs

**Exit Codes:**
- 0: All checks passed
- 1: One or more checks failed

### `backup.sh` - Backup and Restore

Complete backup and restore solution for OLB configuration and data.

**Usage:**
```bash
# Create backup
./backup.sh backup
./backup.sh backup pre-migration

# Restore from backup
./backup.sh restore backup-20250405-120000.tar.gz

# List backups
./backup.sh list

# Verify backup
./backup.sh verify backup-20250405-120000.tar.gz

# Cleanup old backups
./backup.sh cleanup
```

**Features:**
- Automatic backup with timestamp
- Leader detection (only leader backs up Raft data)
- Compressed archives
- Configurable retention policy
- Verify backup integrity
- Automatic restore with service restart
- Safe rollback (backs up current state first)

**Environment Variables:**
```bash
export BACKUP_DIR="/backup/olb"
export DATA_DIR="/var/lib/olb"
export CONFIG_DIR="/etc/olb"
export RETENTION_DAYS=30
```

## Quick Start

### Initial Setup

1. **Configure OLB**:
   ```bash
   sudo mkdir -p /etc/olb /var/lib/olb /var/log/olb
   sudo cp configs/olb.yaml.example /etc/olb/olb.yaml
   sudo editor /etc/olb/olb.yaml
   ```

2. **Set up backup directory**:
   ```bash
   sudo mkdir -p /backup/olb
   sudo chown olb:olb /backup/olb
   ```

3. **Install scripts**:
   ```bash
   sudo cp scripts/*.sh /usr/local/bin/
   sudo chmod +x /usr/local/bin/*.sh
   ```

### Daily Operations

```bash
# Check health
health-check.sh

# Create backup before changes
backup.sh backup before-config-change

# Deploy new version
deploy.sh production v1.2.3

# Monitor continuously
watch -n 30 health-check.sh
```

## Cron Jobs

Add to crontab for automated operations:

```bash
# Health check every 5 minutes
*/5 * * * * /usr/local/bin/health-check.sh > /var/log/olb/health.log 2>&1

# Daily backup at 2 AM
0 2 * * * /usr/local/bin/backup.sh backup daily

# Weekly cleanup of old backups
0 3 * * 0 /usr/local/bin/backup.sh cleanup

# Monthly full backup
0 4 1 * * /usr/local/bin/backup.sh backup monthly
```

## Integration with Monitoring

### Prometheus Alertmanager

Add to `alertmanager.yml`:

```yaml
receivers:
- name: 'health-check'
  webhook_configs:
  - url: 'http://localhost:9093/webhook'
    send_resolved: true
```

### Nagios/Icinga

```bash
# Check command
define command {
    command_name check_olb
    command_line /usr/local/bin/health-check.sh $ARG1$
}
```

## Troubleshooting

### Deployment Failures

```bash
# Check logs
docker-compose logs -f

# Rollback manually
docker-compose -f docker-compose.blue.yml up -d
docker-compose -f docker-compose.green.yml down

# Verify configuration
olb config validate /etc/olb/olb.yaml
```

### Health Check Failures

```bash
# Verbose check
curl -v http://localhost:8081/api/v1/system/health

# Check process
systemctl status olb

# Check logs
journalctl -u olb -f
```

### Backup Issues

```bash
# Verify disk space
df -h /backup

# Check permissions
ls -la /backup/olb

# Test restore in staging
backup.sh restore backup-20250405.tar.gz
```

## Security

- Scripts should be owned by root and readable only by administrators
- Backup files should be encrypted at rest
- Use secure transport for remote health checks (HTTPS)
- Store credentials in environment variables, not in scripts

## License

Same as OpenLoadBalancer project.
