#!/bin/bash
# Post-installation script for OpenLoadBalancer packages

set -e

# Create directories
mkdir -p /etc/olb /var/lib/olb /var/log/olb

# Copy example config if not exists
if [ ! -f /etc/olb/olb.yaml ]; then
    cp /etc/olb/olb.yaml.example /etc/olb/olb.yaml
fi

# Set permissions
chown -R olb:olb /var/lib/olb /var/log/olb 2>/dev/null || true
chmod 755 /etc/olb /var/lib/olb /var/log/olb

# Enable systemd service if systemctl exists
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
    systemctl enable olb.socket olb.service 2>/dev/null || true
fi

echo "OpenLoadBalancer installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Edit /etc/olb/olb.yaml"
echo "  2. Run: systemctl start olb"
echo "  3. Check: systemctl status olb"
echo ""
echo "Documentation: https://openloadbalancer.dev/docs"
