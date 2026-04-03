#!/bin/bash
# build-optimized.sh - Optimized build script for production

set -e

VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo "dev")}
COMMIT=${COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "unknown")}
DATE=${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}

LDFLAGS="-s -w \
    -X github.com/openloadbalancer/olb/pkg/version.Version=$VERSION \
    -X github.com/openloadbalancer/olb/pkg/version.Commit=$COMMIT \
    -X github.com/openloadbalancer/olb/pkg/version.Date=$DATE"

# Build flags for optimization
GCFLAGS=""
if [ "${DEBUG:-}" = "" ]; then
    GCFLAGS="-trimpath"
fi

echo "Building OpenLoadBalancer..."
echo "Version: $VERSION"
echo "Commit: $COMMIT"
echo "Date: $DATE"

# Build with optimizations
CGO_ENABLED=0 go build $GCFLAGS \
    -ldflags "$LDFLAGS" \
    -o bin/olb \
    ./cmd/olb

# Show binary size
BINARY="bin/olb"
SIZE=$(stat -c%s "$BINARY" 2>/dev/null || stat -f%z "$BINARY" 2>/dev/null || echo "0")
SIZE_MB=$((SIZE / 1024 / 1024))

echo ""
echo "Build complete!"
echo "Binary: $BINARY"
echo "Size: $SIZE bytes (${SIZE_MB} MB)"

# Check if binary is too large
if [ $SIZE -gt 20971520 ]; then
    echo "WARNING: Binary size exceeds 20MB limit"
    exit 1
fi

# Verify binary works
if [ -x "$BINARY" ]; then
    echo "Binary verification: OK"
    $BINARY version
else
    echo "ERROR: Binary not executable"
    exit 1
fi
