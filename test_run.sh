#!/bin/bash
# Quick smoke test: start backends, start OLB, verify proxying works.
set -e

echo "=== Starting test backends ==="
go run test_backend.go 8081 backend-1 &
BP1=$!
go run test_backend.go 8082 backend-2 &
BP2=$!
sleep 1

echo "=== Starting OpenLoadBalancer ==="
./olb start --config configs/olb.minimal.yaml &
OLBP=$!
sleep 2

echo ""
echo "=== Testing proxy requests ==="
echo "--- Request 1 ---"
curl -s http://127.0.0.1:8080/
echo ""
echo "--- Request 2 ---"
curl -s http://127.0.0.1:8080/
echo ""
echo "--- Request 3 (health) ---"
curl -s http://127.0.0.1:9090/api/v1/health
echo ""
echo "--- Request 4 (version) ---"
curl -s http://127.0.0.1:9090/api/v1/version
echo ""

echo ""
echo "=== Cleanup ==="
kill $OLBP $BP1 $BP2 2>/dev/null
echo "All processes stopped."
echo "=== SMOKE TEST PASSED ==="
