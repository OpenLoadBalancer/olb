#!/bin/bash
# OpenLoadBalancer End-to-End Smoke Test
#
# Validates the complete binary lifecycle:
#   build → start → proxy traffic → admin API → config reload → graceful shutdown
#
# Usage:
#   ./scripts/smoke-test.sh              # Build from source and test
#   ./scripts/smoke-test.sh ./bin/olb    # Test an existing binary
#   ./scripts/smoke-test.sh --docker     # Build Docker image and test
#
# Exit codes:
#   0  All checks passed
#   1  One or more checks failed
#   2  Prerequisites missing

set -euo pipefail

# --- Configuration ---
PROXY_PORT=18080
ADMIN_PORT=19090
BACKEND_PORT=17091
TIMEOUT=30
VERBOSE=0

# --- Colors (when terminal supports it) ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

log()  { echo -e "${BOLD}[SMOKE]${NC} $*"; }
pass() { echo -e "${GREEN}[PASS]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; FAILURES=$((FAILURES + 1)); }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }

FAILURES=0
OLB_PID=""
BACKEND_PID=""
TMPDIR=""
DOCKER_MODE=0
BINARY=""

cleanup() {
    if [ -n "$OLB_PID" ]; then
        kill "$OLB_PID" 2>/dev/null || true
        wait "$OLB_PID" 2>/dev/null || true
    fi
    if [ -n "$BACKEND_PID" ]; then
        kill "$BACKEND_PID" 2>/dev/null || true
        wait "$BACKEND_PID" 2>/dev/null || true
    fi
    if [ -n "$TMPDIR" ]; then
        rm -rf "$TMPDIR"
    fi
}
trap cleanup EXIT

# --- Argument parsing ---
while [[ $# -gt 0 ]]; do
    case "$1" in
        --docker)  DOCKER_MODE=1; shift ;;
        --verbose) VERBOSE=1; shift ;;
        -h|--help)
            echo "Usage: $0 [--docker] [--verbose] [path-to-binary]"
            exit 0
            ;;
        *)
            if [ -z "$BINARY" ]; then
                BINARY="$1"
            else
                fail "Unknown argument: $1"
                exit 2
            fi
            shift
            ;;
    esac
done

# --- Prerequisites check ---
check_prereqs() {
    local missing=0

    if [ "$DOCKER_MODE" -eq 1 ]; then
        if ! command -v docker &>/dev/null; then
            fail "docker is required for --docker mode"
            missing=1
        fi
    else
        if [ -z "$BINARY" ]; then
            if ! command -v go &>/dev/null; then
                fail "go is required to build from source"
                missing=1
            fi
        elif [ ! -x "$BINARY" ]; then
            fail "Binary not found or not executable: $BINARY"
            missing=1
        fi
    fi

    if ! command -v curl &>/dev/null; then
        fail "curl is required"
        missing=1
    fi

    if [ "$missing" -eq 1 ]; then
        exit 2
    fi
}

# --- Build or locate binary ---
prepare_binary() {
    if [ "$DOCKER_MODE" -eq 1 ]; then
        log "Building Docker image..."
        docker build -t olb-smoke-test:latest .
        return
    fi

    if [ -z "$BINARY" ]; then
        log "Building from source..."
        BINARY=$(mktemp -u olb-smoke-XXXXXX)
        CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$BINARY" ./cmd/olb
        chmod +x "$BINARY"
    fi

    log "Binary: $BINARY"
    log "Version: $($BINARY version 2>&1 || echo 'unknown')"
}

# --- Start a simple backend server ---
start_backend() {
    log "Starting test backend on :$BACKEND_PORT ..."

    # Python one-liner HTTP server that echoes request info
    cat > "$TMPDIR/backend.py" << 'PYEOF'
import http.server
import json

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("X-Backend", "smoke-backend")
        self.end_headers()
        resp = {"backend": "smoke", "path": self.path, "method": "GET"}
        self.wfile.write(json.dumps(resp).encode())

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length > 0 else b""
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        resp = {"backend": "smoke", "path": self.path, "method": "POST", "body_len": len(body)}
        self.wfile.write(json.dumps(resp).encode())

    def log_message(self, format, *args):
        pass  # Suppress stderr output

http.server.HTTPServer(("127.0.0.1", BACKEND_PORT), Handler).serve_forever()
PYEOF

    BACKEND_PORT=$BACKEND_PORT python3 "$TMPDIR/backend.py" &
    BACKEND_PID=$!

    # Wait for backend to be ready
    local waited=0
    while [ $waited -lt $TIMEOUT ]; do
        if curl -sf "http://127.0.0.1:$BACKEND_PORT/" -o /dev/null 2>/dev/null; then
            return
        fi
        sleep 0.2
        waited=$((waited + 1))
    done
    fail "Backend did not start within ${TIMEOUT}s"
    exit 1
}

# --- Write config file ---
write_config() {
    cat > "$TMPDIR/olb.yaml" << EOF
version: 1

admin:
  address: "127.0.0.1:$ADMIN_PORT"

listeners:
  - name: http
    protocol: http
    address: "127.0.0.1:$PROXY_PORT"
    routes:
      - name: default
        path: /
        pool: backend

pools:
  - name: backend
    algorithm: round_robin
    health_check:
      type: http
      path: /
      interval: 2s
      timeout: 1s
    backends:
      - id: smoke-backend
        address: "127.0.0.1:$BACKEND_PORT"

logging:
  level: warn
  output: stdout
EOF
}

# --- Start OLB binary ---
start_olb() {
    if [ "$DOCKER_MODE" -eq 1 ]; then
        log "Starting OLB in Docker..."
        docker run -d --name olb-smoke \
            -p "$PROXY_PORT:$PROXY_PORT" \
            -p "$ADMIN_PORT:$ADMIN_PORT" \
            -v "$TMPDIR/olb.yaml:/etc/olb/config.yaml:ro" \
            olb-smoke-test:latest \
            --config /etc/olb/config.yaml
        return
    fi

    log "Starting OLB binary..."
    "$BINARY" --config "$TMPDIR/olb.yaml" &
    OLB_PID=$!
}

# --- Wait for proxy/admin to be ready ---
wait_ready() {
    local waited=0
    log "Waiting for proxy to be ready on :$PROXY_PORT ..."
    while [ $waited -lt $TIMEOUT ]; do
        if curl -sf "http://127.0.0.1:$PROXY_PORT/" -o /dev/null 2>/dev/null; then
            return
        fi
        sleep 0.3
        waited=$((waited + 1))
    done
    fail "Proxy did not become ready within ${TIMEOUT}s"

    # Show diagnostics
    if [ -n "$OLB_PID" ]; then
        log "OLB process status:"
        kill -0 "$OLB_PID" 2>/dev/null && log "  Process $OLB_PID is running" || log "  Process $OLB_PID is NOT running"
    fi
    exit 1
}

# --- Test functions ---
test_proxy_get() {
    log "Test: Proxy GET request"
    local resp
    resp=$(curl -sf "http://127.0.0.1:$PROXY_PORT/" 2>/dev/null)
    if echo "$resp" | grep -q "smoke"; then
        pass "Proxy GET: request proxied to backend"
    else
        fail "Proxy GET: unexpected response: $resp"
    fi
}

test_proxy_post() {
    log "Test: Proxy POST request"
    local resp
    resp=$(curl -sf -X POST -d '{"test":true}' "http://127.0.0.1:$PROXY_PORT/api" 2>/dev/null)
    if echo "$resp" | grep -q "POST"; then
        pass "Proxy POST: request proxied to backend"
    else
        fail "Proxy POST: unexpected response: $resp"
    fi
}

test_proxy_round_robin() {
    log "Test: Multiple requests succeed"
    local ok=0
    for i in $(seq 1 10); do
        if curl -sf "http://127.0.0.1:$PROXY_PORT/" -o /dev/null 2>/dev/null; then
            ok=$((ok + 1))
        fi
    done
    if [ "$ok" -ge 8 ]; then
        pass "Round-robin: $ok/10 requests succeeded"
    else
        fail "Round-robin: only $ok/10 requests succeeded"
    fi
}

test_admin_info() {
    log "Test: Admin API /api/v1/system/info"
    local resp
    resp=$(curl -sf "http://127.0.0.1:$ADMIN_PORT/api/v1/system/info" 2>/dev/null)
    if echo "$resp" | grep -q "version"; then
        pass "Admin /system/info: responds with version info"
    else
        fail "Admin /system/info: unexpected response: ${resp:0:200}"
    fi
}

test_admin_health() {
    log "Test: Admin API /api/v1/system/health"
    local code
    code=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:$ADMIN_PORT/api/v1/system/health" 2>/dev/null)
    if [ "$code" = "200" ]; then
        pass "Admin /system/health: HTTP $code"
    else
        fail "Admin /system/health: HTTP $code (expected 200)"
    fi
}

test_admin_pools() {
    log "Test: Admin API /api/v1/pools"
    local resp
    resp=$(curl -sf "http://127.0.0.1:$ADMIN_PORT/api/v1/pools" 2>/dev/null)
    if echo "$resp" | grep -q "backend"; then
        pass "Admin /pools: returns pool info"
    else
        fail "Admin /pools: unexpected response: ${resp:0:200}"
    fi
}

test_admin_routes() {
    log "Test: Admin API /api/v1/routes"
    local resp
    resp=$(curl -sf "http://127.0.0.1:$ADMIN_PORT/api/v1/routes" 2>/dev/null)
    if echo "$resp" | grep -q "backend"; then
        pass "Admin /routes: returns route info"
    else
        fail "Admin /routes: unexpected response: ${resp:0:200}"
    fi
}

test_admin_backends() {
    log "Test: Admin API /api/v1/backends"
    local resp
    resp=$(curl -sf "http://127.0.0.1:$ADMIN_PORT/api/v1/backends" 2>/dev/null)
    if echo "$resp" | grep -q "smoke-backend"; then
        pass "Admin /backends: returns backend info"
    else
        fail "Admin /backends: unexpected response: ${resp:0:200}"
    fi
}

test_admin_metrics() {
    log "Test: Admin API /api/v1/metrics"
    local code
    code=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:$ADMIN_PORT/api/v1/metrics" 2>/dev/null)
    if [ "$code" = "200" ]; then
        pass "Admin /metrics: HTTP $code"
    else
        fail "Admin /metrics: HTTP $code (expected 200)"
    fi
}

test_webui() {
    log "Test: Web UI served at admin root"
    local resp code
    code=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:$ADMIN_PORT/" 2>/dev/null)
    if [ "$code" = "200" ]; then
        pass "Web UI: HTTP $code at /"
    else
        fail "Web UI: HTTP $code at / (expected 200)"
    fi
}

test_config_reload() {
    log "Test: Config reload via admin API"
    local resp
    resp=$(curl -sf -X POST "http://127.0.0.1:$ADMIN_PORT/api/v1/system/reload" 2>/dev/null)
    if echo "$resp" | grep -qi "success\|ok\|reload"; then
        pass "Config reload: accepted"

        # Verify proxy still works after reload
        sleep 1
        if curl -sf "http://127.0.0.1:$PROXY_PORT/" -o /dev/null 2>/dev/null; then
            pass "Config reload: proxy still functional"
        else
            fail "Config reload: proxy not responding after reload"
        fi
    else
        fail "Config reload: unexpected response: ${resp:0:200}"
    fi
}

test_graceful_shutdown() {
    log "Test: Graceful shutdown via SIGTERM"
    if [ "$DOCKER_MODE" -eq 1 ]; then
        docker stop olb-smoke --time 10 2>/dev/null || true
        pass "Docker container stopped"
        return
    fi

    if [ -z "$OLB_PID" ]; then
        warn "No OLB process to shut down"
        return
    fi

    # Send SIGTERM
    kill "$OLB_PID" 2>/dev/null

    # Wait up to 15s for graceful shutdown
    local waited=0
    while [ $waited -lt 50 ]; do
        if ! kill -0 "$OLB_PID" 2>/dev/null; then
            pass "Graceful shutdown: process exited cleanly"
            wait "$OLB_PID" 2>/dev/null && true
            OLB_PID=""
            return
        fi
        sleep 0.3
        waited=$((waited + 1))
    done

    fail "Graceful shutdown: process did not exit within 15s"
    kill -9 "$OLB_PID" 2>/dev/null || true
    wait "$OLB_PID" 2>/dev/null || true
    OLB_PID=""
}

# --- Summary ---
print_summary() {
    echo ""
    echo "========================================="
    if [ "$FAILURES" -eq 0 ]; then
        echo -e "${GREEN}${BOLD}ALL CHECKS PASSED${NC}"
    else
        echo -e "${RED}${BOLD}$FAILURES CHECK(S) FAILED${NC}"
    fi
    echo "========================================="
    echo ""
}

# --- Main ---
main() {
    echo ""
    log "OpenLoadBalancer E2E Smoke Test"
    log "================================"
    echo ""

    check_prereqs

    TMPDIR=$(mktemp -d)
    trap cleanup EXIT

    prepare_binary
    start_backend
    write_config
    start_olb
    wait_ready

    echo ""
    log "Running checks..."
    echo ""

    # Proxy tests
    test_proxy_get
    test_proxy_post
    test_proxy_round_robin

    # Admin API tests
    test_admin_info
    test_admin_health
    test_admin_pools
    test_admin_routes
    test_admin_backends
    test_admin_metrics
    test_webui

    # Config reload
    test_config_reload

    # Graceful shutdown
    test_graceful_shutdown

    print_summary

    if [ "$FAILURES" -gt 0 ]; then
        exit 1
    fi
    exit 0
}

main "$@"
