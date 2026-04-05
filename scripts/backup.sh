#!/bin/bash
#
# OpenLoadBalancer Backup and Restore Script
# Usage: ./backup.sh [backup|restore] [options]
#

set -e

# Configuration
BACKUP_DIR=${BACKUP_DIR:-"/backup/olb"}
DATA_DIR=${DATA_DIR:-"/var/lib/olb"}
CONFIG_DIR=${CONFIG_DIR:-"/etc/olb"}
RETENTION_DAYS=${RETENTION_DAYS:-30}

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Create backup
backup() {
    local backup_name=${1:-"backup-$(date +%Y%m%d-%H%M%S)"}
    local backup_path="${BACKUP_DIR}/${backup_name}"

    log_info "Creating backup: ${backup_name}"

    # Create backup directory
    mkdir -p "${backup_path}"

    # Backup configuration
    log_info "Backing up configuration..."
    if [ -d "${CONFIG_DIR}" ]; then
        cp -r "${CONFIG_DIR}" "${backup_path}/config"
    else
        log_warn "Config directory not found: ${CONFIG_DIR}"
    fi

    # Backup data (Raft state)
    log_info "Backing up data..."
    if [ -d "${DATA_DIR}" ]; then
        # Check if running as leader (only backup from leader)
        if command -v curl &> /dev/null; then
            cluster_status=$(curl -s http://localhost:8081/api/v1/cluster/status 2>/dev/null || echo "{}")
            is_leader=$(echo "$cluster_status" | grep -q '"state":"leader"' && echo "yes" || echo "no")

            if [ "$is_leader" = "yes" ] || [ "$cluster_status" = "{}" ]; then
                cp -r "${DATA_DIR}" "${backup_path}/data"
            else
                log_warn "Not cluster leader, skipping data backup"
            fi
        else
            cp -r "${DATA_DIR}" "${backup_path}/data"
        fi
    else
        log_warn "Data directory not found: ${DATA_DIR}"
    fi

    # Backup certificates
    log_info "Backing up certificates..."
    if [ -d "${CONFIG_DIR}/certs" ]; then
        cp -r "${CONFIG_DIR}/certs" "${backup_path}/certs"
    fi

    # Create manifest
    cat > "${backup_path}/MANIFEST.txt" << EOF
OpenLoadBalancer Backup
Name: ${backup_name}
Created: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
Hostname: $(hostname)
Version: $(olb version 2>/dev/null || echo "unknown")

Contents:
- config/: Configuration files
- data/: Raft data and state
- certs/: TLS certificates
EOF

    # Compress backup
    log_info "Compressing backup..."
    tar czf "${backup_path}.tar.gz" -C "${BACKUP_DIR}" "${backup_name}"
    rm -rf "${backup_path}"

    # Verify backup
    if [ -f "${backup_path}.tar.gz" ]; then
        size=$(du -h "${backup_path}.tar.gz" | cut -f1)
        log_success "Backup created: ${backup_path}.tar.gz (${size})"
    else
        log_error "Backup creation failed"
        exit 1
    fi

    # Cleanup old backups
    cleanup_old_backups

    echo ""
    log_success "Backup completed: ${backup_name}.tar.gz"
}

# Restore from backup
restore() {
    local backup_file=$1

    if [ -z "$backup_file" ]; then
        log_error "No backup file specified"
        log_info "Usage: ./backup.sh restore <backup-file.tar.gz>"
        list_backups
        exit 1
    fi

    if [ ! -f "$backup_file" ]; then
        # Try with backup directory prefix
        backup_file="${BACKUP_DIR}/${backup_file}"
        if [ ! -f "$backup_file" ]; then
            log_error "Backup file not found: $1"
            list_backups
            exit 1
        fi
    fi

    log_warn "About to restore from: ${backup_file}"
    log_warn "This will overwrite current configuration and data!"
    read -p "Are you sure? (yes/no): " confirm

    if [ "$confirm" != "yes" ]; then
        log_info "Restore cancelled"
        exit 0
    fi

    # Stop OLB
    log_info "Stopping OpenLoadBalancer..."
    systemctl stop olb 2>/dev/null || true

    # Create restore directory
    restore_dir="/tmp/olb-restore-$(date +%s)"
    mkdir -p "$restore_dir"

    # Extract backup
    log_info "Extracting backup..."
    tar xzf "$backup_file" -C "$restore_dir"

    # Find extracted directory
    extracted_dir=$(find "$restore_dir" -maxdepth 1 -type d | grep -v "^${restore_dir}$" | head -1)

    if [ -z "$extracted_dir" ]; then
        log_error "Failed to find extracted backup contents"
        exit 1
    fi

    # Restore configuration
    if [ -d "$extracted_dir/config" ]; then
        log_info "Restoring configuration..."
        rm -rf "${CONFIG_DIR}.bak"
        mv "${CONFIG_DIR}" "${CONFIG_DIR}.bak" 2>/dev/null || true
        cp -r "$extracted_dir/config" "$CONFIG_DIR"
    fi

    # Restore data
    if [ -d "$extracted_dir/data" ]; then
        log_info "Restoring data..."
        rm -rf "${DATA_DIR}.bak"
        mv "${DATA_DIR}" "${DATA_DIR}.bak" 2>/dev/null || true
        cp -r "$extracted_dir/data" "$DATA_DIR"
        chown -R olb:olb "$DATA_DIR" 2>/dev/null || true
    fi

    # Restore certificates
    if [ -d "$extracted_dir/certs" ]; then
        log_info "Restoring certificates..."
        cp -r "$extracted_dir/certs" "${CONFIG_DIR}/"
    fi

    # Cleanup
    rm -rf "$restore_dir"

    # Start OLB
    log_info "Starting OpenLoadBalancer..."
    systemctl start olb 2>/dev/null || true

    # Verify
    sleep 2
    if systemctl is-active --quiet olb 2>/dev/null || curl -s http://localhost:8080/health > /dev/null; then
        log_success "Restore completed and service is running"
    else
        log_warn "Service may not be running, please check"
    fi

    echo ""
    log_success "Restore completed from: ${backup_file}"
}

# List available backups
list_backups() {
    log_info "Available backups in ${BACKUP_DIR}:"

    if [ ! -d "$BACKUP_DIR" ] || [ -z "$(ls -A "$BACKUP_DIR" 2>/dev/null)" ]; then
        log_warn "No backups found"
        return
    fi

    printf "%-30s %-15s %-20s\n" "NAME" "SIZE" "DATE"
    echo "------------------------------------------------------"

    for backup in "$BACKUP_DIR"/*.tar.gz; do
        if [ -f "$backup" ]; then
            name=$(basename "$backup")
            size=$(du -h "$backup" | cut -f1)
            date=$(stat -c %y "$backup" 2>/dev/null | cut -d' ' -f1,2 | cut -d'.' -f1 || stat -f %Sm "$backup")
            printf "%-30s %-15s %-20s\n" "$name" "$size" "$date"
        fi
    done
}

# Cleanup old backups
cleanup_old_backups() {
    log_info "Cleaning up backups older than ${RETENTION_DAYS} days..."

    if [ ! -d "$BACKUP_DIR" ]; then
        return
    fi

    deleted=0
    while IFS= read -r file; do
        rm -f "$file"
        log_info "Deleted old backup: $(basename "$file")"
        ((deleted++))
    done < <(find "$BACKUP_DIR" -name "*.tar.gz" -mtime +${RETENTION_DAYS} 2>/dev/null)

    if [ $deleted -eq 0 ]; then
        log_info "No old backups to clean up"
    else
        log_success "Cleaned up ${deleted} old backups"
    fi
}

# Verify backup integrity
verify() {
    local backup_file=$1

    if [ -z "$backup_file" ]; then
        log_error "No backup file specified"
        exit 1
    fi

    if [ ! -f "$backup_file" ]; then
        backup_file="${BACKUP_DIR}/${backup_file}"
        if [ ! -f "$backup_file" ]; then
            log_error "Backup file not found: $1"
            exit 1
        fi
    fi

    log_info "Verifying backup: ${backup_file}"

    # Test archive integrity
    if tar tzf "$backup_file" > /dev/null 2>&1; then
        log_success "Archive integrity check passed"
    else
        log_error "Archive is corrupted"
        exit 1
    fi

    # List contents
    log_info "Backup contents:"
    tar tzf "$backup_file" | head -20

    echo ""
    log_success "Backup verification completed"
}

# Show help
show_help() {
    cat << EOF
OpenLoadBalancer Backup and Restore Script

Usage: ./backup.sh [COMMAND] [OPTIONS]

Commands:
  backup [name]       Create a new backup
  restore <file>      Restore from backup file
  list                List available backups
  verify <file>       Verify backup integrity
  cleanup             Remove old backups (respects RETENTION_DAYS)

Environment Variables:
  BACKUP_DIR          Backup storage directory (default: /backup/olb)
  DATA_DIR            Data directory (default: /var/lib/olb)
  CONFIG_DIR          Config directory (default: /etc/olb)
  RETENTION_DAYS      Days to keep backups (default: 30)

Examples:
  ./backup.sh backup                          # Create backup with auto name
  ./backup.sh backup pre-upgrade              # Create named backup
  ./backup.sh restore backup-20250405.tar.gz  # Restore from backup
  ./backup.sh list                            # List all backups
  ./backup.sh verify backup-20250405.tar.gz   # Verify backup integrity

EOF
}

# Main
main() {
    case "${1:-}" in
        backup)
            mkdir -p "$BACKUP_DIR"
            backup "$2"
            ;;
        restore)
            restore "$2"
            ;;
        list)
            list_backups
            ;;
        verify)
            verify "$2"
            ;;
        cleanup)
            cleanup_old_backups
            ;;
        --help|-h|help)
            show_help
            ;;
        *)
            log_error "Unknown command: ${1:-}"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
