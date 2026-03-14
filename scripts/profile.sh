#!/usr/bin/env bash
# profile.sh - Profiling helper for OpenLoadBalancer
#
# This script builds OLB with profiling enabled, runs a short load test,
# collects CPU and memory profiles, and generates analysis output.
#
# Usage:
#   ./scripts/profile.sh                # full profiling session
#   ./scripts/profile.sh -duration 30   # custom load-test duration (seconds)
#   ./scripts/profile.sh -addr :9090    # custom OLB listen address
#   ./scripts/profile.sh -skip-load     # skip load generation (attach your own)
#   ./scripts/profile.sh -pprof-only    # only capture pprof from running instance
#   ./scripts/profile.sh -h             # show help

set -euo pipefail

# ---- Configuration --------------------------------------------------------

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROFILE_DIR="${PROJECT_ROOT}/profiles"
BINARY="${PROJECT_ROOT}/olb-profiling"
CONFIG="${PROJECT_ROOT}/configs/example.yaml"

DURATION=10          # seconds of load testing
OLB_ADDR=":8080"     # OLB listener address
ADMIN_ADDR=":8081"   # OLB admin address
PPROF_ADDR="localhost:6060"
SKIP_LOAD=false
PPROF_ONLY=false
REQUESTS=1000        # number of requests for the load test

# Profile output files
CPU_PROFILE="${PROFILE_DIR}/cpu.prof"
MEM_PROFILE="${PROFILE_DIR}/mem.prof"
ALLOC_PROFILE="${PROFILE_DIR}/allocs.prof"
BLOCK_PROFILE="${PROFILE_DIR}/block.prof"
MUTEX_PROFILE="${PROFILE_DIR}/mutex.prof"
GOROUTINE_DUMP="${PROFILE_DIR}/goroutine.txt"
CPU_SVG="${PROFILE_DIR}/cpu-flamegraph.svg"
MEM_SVG="${PROFILE_DIR}/mem-flamegraph.svg"

# ---- Argument parsing ------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case "$1" in
        -duration)
            DURATION="$2"
            shift 2
            ;;
        -addr)
            OLB_ADDR="$2"
            shift 2
            ;;
        -admin-addr)
            ADMIN_ADDR="$2"
            shift 2
            ;;
        -pprof-addr)
            PPROF_ADDR="$2"
            shift 2
            ;;
        -config)
            CONFIG="$2"
            shift 2
            ;;
        -skip-load)
            SKIP_LOAD=true
            shift
            ;;
        -pprof-only)
            PPROF_ONLY=true
            shift
            ;;
        -requests)
            REQUESTS="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  -duration N       Load test duration in seconds (default: 10)"
            echo "  -addr ADDR        OLB listen address (default: :8080)"
            echo "  -admin-addr ADDR  OLB admin address (default: :8081)"
            echo "  -pprof-addr ADDR  pprof server address (default: localhost:6060)"
            echo "  -config FILE      OLB config file (default: configs/example.yaml)"
            echo "  -requests N       Number of requests for load test (default: 1000)"
            echo "  -skip-load        Skip load generation"
            echo "  -pprof-only       Only capture pprof from a running instance"
            echo "  -h, --help        Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# ---- Setup -----------------------------------------------------------------

mkdir -p "${PROFILE_DIR}"

echo "============================================================"
echo "  OpenLoadBalancer Profiling Session"
echo "============================================================"
echo ""
echo "  Project root:  ${PROJECT_ROOT}"
echo "  Profile dir:   ${PROFILE_DIR}"
echo "  Duration:      ${DURATION}s"
echo "  pprof addr:    ${PPROF_ADDR}"
echo ""

# ---- pprof-only mode: capture from running instance -----------------------

if [[ "${PPROF_ONLY}" == "true" ]]; then
    echo ">> Capturing profiles from running instance at ${PPROF_ADDR} ..."
    echo ""

    # CPU profile (30s by default, we use DURATION)
    echo "   [1/5] CPU profile (${DURATION}s) ..."
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/profile?seconds=${DURATION}" -o "${CPU_PROFILE}" 2>/dev/null; then
        echo "         -> ${CPU_PROFILE}"
    else
        echo "         FAILED (is OLB running with pprof enabled?)"
    fi

    # Heap profile
    echo "   [2/5] Heap profile ..."
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/heap" -o "${MEM_PROFILE}" 2>/dev/null; then
        echo "         -> ${MEM_PROFILE}"
    else
        echo "         FAILED"
    fi

    # Allocs profile
    echo "   [3/5] Allocs profile ..."
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/allocs" -o "${ALLOC_PROFILE}" 2>/dev/null; then
        echo "         -> ${ALLOC_PROFILE}"
    else
        echo "         FAILED"
    fi

    # Block profile
    echo "   [4/5] Block profile ..."
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/block" -o "${BLOCK_PROFILE}" 2>/dev/null; then
        echo "         -> ${BLOCK_PROFILE}"
    else
        echo "         FAILED"
    fi

    # Goroutine dump
    echo "   [5/5] Goroutine dump ..."
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/goroutine?debug=2" -o "${GOROUTINE_DUMP}" 2>/dev/null; then
        echo "         -> ${GOROUTINE_DUMP}"
    else
        echo "         FAILED"
    fi

    echo ""
    # Jump to analysis
    SKIP_LOAD=true  # no need to stop OLB
fi

# ---- Build & run OLB with profiling ---------------------------------------

OLB_PID=""

cleanup() {
    if [[ -n "${OLB_PID}" ]]; then
        echo ""
        echo ">> Stopping OLB (PID ${OLB_PID}) ..."
        kill "${OLB_PID}" 2>/dev/null || true
        wait "${OLB_PID}" 2>/dev/null || true
    fi
}
trap cleanup EXIT

if [[ "${PPROF_ONLY}" != "true" ]]; then
    # Build
    echo ">> Building OLB with profiling support ..."
    cd "${PROJECT_ROOT}"
    go build -o "${BINARY}" ./cmd/olb 2>/dev/null || {
        echo "   Build failed or no cmd/olb entry point yet."
        echo "   Running unit-level profiling instead."
        echo ""

        # ---- Fallback: profile via go test -bench ---------------------------------
        echo ">> Running benchmark-based profiling ..."
        echo ""

        go test -bench=. -benchmem -benchtime=1s \
            -cpuprofile="${CPU_PROFILE}" \
            -memprofile="${MEM_PROFILE}" \
            -run='^$' \
            -timeout=120s \
            ./internal/... 2>&1 | tee "${PROFILE_DIR}/bench-output.txt" || true

        echo ""
        echo ">> Benchmark profiling complete."
        echo ""

        # Jump to analysis
        SKIP_LOAD=true
        PPROF_ONLY=true  # skip the "stop OLB" cleanup
        OLB_PID=""
    }

    if [[ "${PPROF_ONLY}" != "true" ]]; then
        echo "   Built: ${BINARY}"
        echo ""

        # Ensure config exists
        if [[ ! -f "${CONFIG}" ]]; then
            echo "   WARN: Config file not found at ${CONFIG}"
            echo "         Creating minimal config ..."
            mkdir -p "$(dirname "${CONFIG}")"
            cat > "${CONFIG}" <<'YAML'
version: "1"
admin:
  address: ":8081"
listeners:
  - name: http
    address: ":8080"
    routes:
      - path: /
        pool: default
pools:
  - name: default
    algorithm: round_robin
    backends:
      - id: local
        address: "127.0.0.1:9999"
        weight: 1
    health_check:
      type: tcp
      interval: 10s
      timeout: 5s
YAML
        fi

        # Start OLB with profiling flags
        echo ">> Starting OLB with profiling enabled ..."
        "${BINARY}" start \
            --config "${CONFIG}" \
            --cpu-profile "${CPU_PROFILE}" \
            --mem-profile "${MEM_PROFILE}" \
            --pprof-addr "${PPROF_ADDR}" \
            &
        OLB_PID=$!
        echo "   PID: ${OLB_PID}"

        # Wait for OLB to start
        echo "   Waiting for OLB to become ready ..."
        for i in $(seq 1 30); do
            if curl -sS -o /dev/null "http://localhost${ADMIN_ADDR}/api/v1/system/health" 2>/dev/null; then
                echo "   OLB is ready."
                break
            fi
            if [[ $i -eq 30 ]]; then
                echo "   WARN: OLB did not become ready in 30s."
            fi
            sleep 1
        done
        echo ""
    fi
fi

# ---- Load generation -------------------------------------------------------

if [[ "${SKIP_LOAD}" != "true" ]]; then
    echo ">> Running load test (${DURATION}s, ${REQUESTS} requests) ..."
    echo ""

    # Use go test -bench if available, otherwise fall back to curl loop
    if command -v go >/dev/null 2>&1; then
        # Quick synthetic load with Go's HTTP client
        END=$((SECONDS + DURATION))
        COUNT=0
        ERRORS=0
        while [[ $SECONDS -lt $END && $COUNT -lt $REQUESTS ]]; do
            if curl -sS -o /dev/null "http://localhost${OLB_ADDR}/" 2>/dev/null; then
                COUNT=$((COUNT + 1))
            else
                ERRORS=$((ERRORS + 1))
                COUNT=$((COUNT + 1))
            fi
        done
        echo "   Completed: ${COUNT} requests (${ERRORS} errors) in ${DURATION}s"
    fi

    echo ""

    # Capture profiles from pprof endpoint after load
    echo ">> Capturing post-load profiles from pprof ..."

    if curl -sS "http://${PPROF_ADDR}/debug/pprof/heap" -o "${MEM_PROFILE}" 2>/dev/null; then
        echo "   Heap profile  -> ${MEM_PROFILE}"
    fi
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/allocs" -o "${ALLOC_PROFILE}" 2>/dev/null; then
        echo "   Alloc profile -> ${ALLOC_PROFILE}"
    fi
    if curl -sS "http://${PPROF_ADDR}/debug/pprof/goroutine?debug=2" -o "${GOROUTINE_DUMP}" 2>/dev/null; then
        echo "   Goroutines    -> ${GOROUTINE_DUMP}"
    fi

    echo ""
fi

# ---- Analysis --------------------------------------------------------------

echo "============================================================"
echo "  Profile Analysis"
echo "============================================================"
echo ""

# CPU profile analysis
if [[ -f "${CPU_PROFILE}" && -s "${CPU_PROFILE}" ]]; then
    echo ">> CPU Profile: ${CPU_PROFILE}"
    echo ""

    # Top CPU consumers
    if go tool pprof -top -nodecount=15 "${CPU_PROFILE}" 2>/dev/null; then
        echo ""
    else
        echo "   (go tool pprof not available or profile empty)"
    fi

    # Generate SVG flamegraph
    if go tool pprof -svg -output="${CPU_SVG}" "${CPU_PROFILE}" 2>/dev/null; then
        echo "   Flamegraph SVG -> ${CPU_SVG}"
    else
        echo "   (SVG generation skipped - graphviz may not be installed)"
    fi
    echo ""
else
    echo ">> CPU profile not found or empty, skipping."
    echo ""
fi

# Memory profile analysis
if [[ -f "${MEM_PROFILE}" && -s "${MEM_PROFILE}" ]]; then
    echo ">> Memory Profile: ${MEM_PROFILE}"
    echo ""

    if go tool pprof -top -nodecount=15 "${MEM_PROFILE}" 2>/dev/null; then
        echo ""
    else
        echo "   (go tool pprof not available or profile empty)"
    fi

    if go tool pprof -svg -output="${MEM_SVG}" "${MEM_PROFILE}" 2>/dev/null; then
        echo "   Flamegraph SVG -> ${MEM_SVG}"
    else
        echo "   (SVG generation skipped)"
    fi
    echo ""
else
    echo ">> Memory profile not found or empty, skipping."
    echo ""
fi

# Goroutine dump summary
if [[ -f "${GOROUTINE_DUMP}" && -s "${GOROUTINE_DUMP}" ]]; then
    GOROUTINE_COUNT=$(grep -c "^goroutine " "${GOROUTINE_DUMP}" 2>/dev/null || echo "?")
    echo ">> Goroutine dump: ${GOROUTINE_DUMP} (${GOROUTINE_COUNT} goroutines)"
    echo ""
fi

# ---- Summary ---------------------------------------------------------------

echo "============================================================"
echo "  Output Files"
echo "============================================================"
echo ""

for f in "${CPU_PROFILE}" "${MEM_PROFILE}" "${ALLOC_PROFILE}" "${BLOCK_PROFILE}" \
         "${MUTEX_PROFILE}" "${GOROUTINE_DUMP}" "${CPU_SVG}" "${MEM_SVG}"; do
    if [[ -f "$f" && -s "$f" ]]; then
        SIZE=$(du -h "$f" 2>/dev/null | cut -f1)
        printf "  %-50s %s\n" "$f" "${SIZE}"
    fi
done

echo ""
echo ">> Interactive analysis:"
echo "   go tool pprof ${CPU_PROFILE}"
echo "   go tool pprof ${MEM_PROFILE}"
echo "   go tool pprof -http=:8888 ${CPU_PROFILE}"
echo ""
echo ">> Done."
