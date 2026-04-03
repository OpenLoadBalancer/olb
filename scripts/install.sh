#!/bin/bash
# OpenLoadBalancer Installation Script
# Usage: curl -sSL https://openloadbalancer.dev/install.sh | sh

set -e

REPO="openloadbalancer/olb"
INSTALL_DIR="/usr/local/bin"
VERSION="${VERSION:-latest}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux|darwin|freebsd) ;;  # Supported
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

echo "Installing OpenLoadBalancer for $OS/$ARCH..."

# Determine download URL
if [ "$VERSION" = "latest" ]; then
  DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/olb-$OS-$ARCH"
else
  DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/olb-$OS-$ARCH"
fi

# Download binary
echo "Downloading from $DOWNLOAD_URL..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$DOWNLOAD_URL" -o /tmp/olb
elif command -v wget >/dev/null 2>&1; then
  wget -q "$DOWNLOAD_URL" -O /tmp/olb
else
  echo "Error: curl or wget required"
  exit 1
fi

# Make executable and move to install directory
chmod +x /tmp/olb

# Check if we need sudo
if [ -w "$INSTALL_DIR" ]; then
  mv /tmp/olb "$INSTALL_DIR/olb"
else
  echo "Requires sudo to install to $INSTALL_DIR"
  sudo mv /tmp/olb "$INSTALL_DIR/olb"
fi

# Verify installation
if command -v olb >/dev/null 2>&1; then
  echo "OpenLoadBalancer installed successfully!"
  olb version
  echo ""
  echo "Get started:"
  echo "  olb --help"
  echo "  olb init --config olb.yaml"
  echo "  olb start --config olb.yaml"
else
  echo "Installation complete. Add $INSTALL_DIR to your PATH."
fi
