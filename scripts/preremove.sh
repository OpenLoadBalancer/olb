#!/bin/bash
# Pre-removal script for OpenLoadBalancer packages

set -e

# Stop and disable systemd service
if command -v systemctl >/dev/null 2>&1; then
    systemctl stop olb 2>/dev/null || true
    systemctl disable olb.socket olb.service 2>/dev/null || true
fi

echo "OpenLoadBalancer removed."
echo "Config files remain at /etc/olb (remove manually if desired)"
