#!/bin/bash
#
# OpenLoadBalancer Health Check Script
# Usage: ./health-check.sh [admin-url]
#

set -e

# Configuration
ADMIN_URL=${1:-http://localhost:8081}
TIMEOUT=5

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Status tracking
OVERALL_STATUS="HEALTHY"
FAILED_CHECKS=()

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
    OVERALL_STATUS="UNHEALTHY"
    FAILED_CHECKS+=("$1")
}

check_http_endpoint() {
    local name=$1
    local url=$2
    local expected_code=${3:-200}

    log_info "Checking ${name}..."

    response=$(curl -s -o /dev/null -w "%{http_code}" --max-time ${TIMEOUT} "${url}" 2>&1) || true

    if [ "$response" = "$expected_code" ]; then
        log_success "${name} - HTTP ${response}"
        return 0
    else
        log_error "${name} - Expected ${expected_code}, got ${response}"
        return 1
    fi
}

check_json_endpoint() {
    local name=$1
    local url=$2
    local jq_filter=$3

    log_info "Checking ${name}..."

    response=$(curl -s --max-time ${TIMEOUT} "${url}" 2>&1) || true

    if [ -z "$response" ]; then
        log_error "${name} - No response"
        return 1
    fi

    if echo "$response" | jq -e "$jq_filter" > /dev/null 2>&1; then
        log_success "${name} - Valid response"
        return 0
    else
        log_error "${name} - Invalid response"
        return 1
    fi
}

check_tcp_port() {
    local name=$1
    local host=$2
    local port=$3

    log_info "Checking ${name}..."

    if timeout ${TIMEOUT} bash -c "cat < /dev/null > /dev/tcp/${host}/${port}" 2>/dev/null; then
        log_success "${name} - Port ${port} open"
        return 0
    else
        log_error "${name} - Port ${port} not reachable"
        return 1
    fi
}

check_process() {
    local name=$1
    local process=$2

    log_info "Checking ${name} process..."

    if pgrep -x "$process" > /dev/null 2>&1; then
        log_success "${name} - Process running"
        return 0
    else
        log_error "${name} - Process not running"
        return 1
    fi
}

check_disk_space() {
    local threshold=${1:-90}

    log_info "Checking disk space..."

    usage=$(df / | tail -1 | awk '{print $5}' | sed 's/%//')

    if [ "$usage" -lt "$threshold" ]; then
        log_success "Disk usage: ${usage}%"
        return 0
    else
        log_error "Disk usage critical: ${usage}%"
        return 1
    fi
}

check_memory() {
    local threshold=${1:-90}

    log_info "Checking memory..."

    # Get memory usage percentage
    if command -v free &> /dev/null; then
        usage=$(free | grep Mem | awk '{printf("%.0f", $3/$2 * 100.0)}')
    else
        # macOS
        usage=$(vm_stat | awk '/Pages active/ {print $3}' | sed 's/[^0-9]//g')
        usage=$((usage / 1024 / 1024))
    fi

    if [ "$usage" -lt "$threshold" ]; then
        log_success "Memory usage: ${usage}%"
        return 0
    else
        log_error "Memory usage critical: ${usage}%"
        return 1
    fi
}

check_logs() {
    local log_file=${1:-/var/log/olb/olb.log}

    log_info "Checking recent errors in logs..."

    if [ ! -f "$log_file" ]; then
        log_warn "Log file not found: ${log_file}"
        return 0
    fi

    # Check for recent errors (last 5 minutes)
    error_count=$(find "$log_file" -mmin -5 -exec grep -c '"level":"error"' {} \; 2>/dev/null || echo "0")

    if [ "$error_count" -eq 0 ]; then
        log_success "No recent errors in logs"
        return 0
    else
        log_warn "${error_count} errors in last 5 minutes"
        return 0
    fi
}

# Main health checks
main() {
    echo "======================================"
    echo "OpenLoadBalancer Health Check"
    echo "Admin URL: ${ADMIN_URL}"
    echo "Time: $(date)"
    echo "======================================"
    echo ""

    # Check process
    check_process "OLB" "olb" || true

    # Check TCP ports
    check_tcp_port "Admin API" "localhost" "8081" || true

    # Check HTTP endpoints
    check_http_endpoint "System Health" "${ADMIN_URL}/api/v1/system/health" || true
    check_http_endpoint "Metrics" "${ADMIN_URL}/metrics" || true

    # Check JSON endpoints
    check_json_endpoint "System Info" "${ADMIN_URL}/api/v1/system/info" '.version' || true

    # Check system resources
    check_disk_space || true
    check_memory || true
    check_logs || true

    # Check backends if configured
    log_info "Checking backends..."
    backends=$(curl -s "${ADMIN_URL}/api/v1/backends" 2>/dev/null | jq -r '.pools[]?.name' 2>/dev/null || true)
    if [ -n "$backends" ]; then
        for pool in $backends; do
            check_json_endpoint "Pool: ${pool}" "${ADMIN_URL}/api/v1/backends/${pool}" '.healthy' || true
        done
    fi

    echo ""
    echo "======================================"
    if [ "$OVERALL_STATUS" = "HEALTHY" ]; then
        echo -e "${GREEN}Status: HEALTHY${NC}"
        echo "All checks passed"
        exit 0
    else
        echo -e "${RED}Status: UNHEALTHY${NC}"
        echo "Failed checks:"
        for check in "${FAILED_CHECKS[@]}"; do
            echo "  - $check"
        done
        exit 1
    fi
}

# Help
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    cat << EOF
OpenLoadBalancer Health Check Script

Usage: ./health-check.sh [OPTIONS] [ADMIN_URL]

Arguments:
  ADMIN_URL      Admin API URL (default: http://localhost:8081)

Options:
  -h, --help     Show this help message

Examples:
  ./health-check.sh                    # Check local instance
  ./health-check.sh http://olb:8081    # Check remote instance

EOF
    exit 0
fi

main
