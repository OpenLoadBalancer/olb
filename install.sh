#!/usr/bin/env sh
# OpenLoadBalancer install script
# Usage:
#   Binary:  curl -sSL https://raw.githubusercontent.com/openloadbalancer/olb/main/install.sh | sh
#   Docker: curl -sSL https://raw.githubusercontent.com/openloadbalancer/olb/main/install.sh | sh -s -- --method docker
set -eu

REPO="openloadbalancer/olb"
BINARY="olb"
IMAGE="ghcr.io/${REPO}:latest"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
METHOD="${METHOD:-binary}"

# --- Helpers ---
info()  { printf '\033[1;34m[info]\033[0m  %s\n' "$*"; }
ok()    { printf '\033[1;32m[ok]\033[0m    %s\n' "$*"; }
err()   { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

# --- Parse args ---
while [ $# -gt 0 ]; do
    case "$1" in
        -m|--method) METHOD="$2"; shift 2 ;;
        -m|--method=*) METHOD="${1#*=}"; shift ;;
        -h|--help) echo "Usage: $0 [-m|--method binary|docker]"; exit 0 ;;
        *) shift ;;
    esac
done

# --- Detect platform ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    freebsd) OS="freebsd" ;;
    *)      err "Unsupported OS: $OS" ;;
esac

case "$ARCH" in
    x86_64|amd64)        ARCH="amd64" ;;
    aarch64|arm64)       ARCH="arm64" ;;
    *)                   err "Unsupported architecture: $ARCH" ;;
esac

# --- Determine version ---
if [ -n "${VERSION:-}" ]; then
    TAG="$VERSION"
else
    TAG=$(curl -sfL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    if [ -z "$TAG" ]; then
        err "Could not determine latest version. Set VERSION env var manually."
    fi
fi

# --- Docker method ---
install_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        err "Docker is not installed. Install Docker Engine from https://docker.com"
    fi

    info "Pulling ${IMAGE} ..."
    docker pull "${IMAGE}" || err "Failed to pull image"
    ok "Image pulled: ${IMAGE}"
    printf '\nRun with Docker:\n'
    printf '  docker run -it --rm -p 8080:8080 -p 9090:9090 \\\n'
    printf '    -v $(pwd)/olb.yaml:/etc/olb/olb.yaml:ro \\\n'
    printf '    %s start\n' "${IMAGE}"
    printf '\nOr use docker-compose from:\n'
    printf '  https://github.com/%s/tree/main/deploy\n' "${REPO}"
}

# --- Binary method ---
install_binary() {
    info "Installing OpenLoadBalancer ${TAG} for ${OS}-${ARCH}"

    FILENAME="${BINARY}-${OS}-${ARCH}"
    if [ "$OS" = "windows" ]; then
        FILENAME="${FILENAME}.exe"
    fi

    URL="https://github.com/${REPO}/releases/download/${TAG}/${FILENAME}"

    TMPDIR="$(mktemp -d)"
    TARGET="${TMPDIR}/${BINARY}"

    info "Downloading ${URL}..."
    curl -sfL "$URL" -o "$TARGET" || err "Download failed. Check that the release exists at ${URL}"

    chmod +x "$TARGET"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$TARGET" "${INSTALL_DIR}/${BINARY}"
    else
        info "Requires sudo to install to ${INSTALL_DIR}"
        sudo mv "$TARGET" "${INSTALL_DIR}/${BINARY}"
    fi

    INSTALLED="$(${INSTALL_DIR}/${BINARY} version 2>/dev/null | head -1 || echo "${TAG}")"
    ok "Installed: ${INSTALLED}"

    rm -rf "$TMPDIR"

    printf '\nRun `olb setup` to create an initial configuration, or:\n'
    printf '  olb start --config olb.yaml\n\n'
}

# --- Dispatch ---
case "$METHOD" in
    docker) install_docker ;;
    binary) install_binary ;;
    *)      err "Unknown method: $METHOD. Use 'binary' or 'docker'." ;;
esac
