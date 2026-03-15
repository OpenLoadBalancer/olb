#!/bin/sh
# OpenLoadBalancer (OLB) Install Script
#
# Usage:
#   curl -fsSL https://openloadbalancer.dev/install.sh | sh
#   curl -fsSL https://openloadbalancer.dev/install.sh | sh -s -- --version v0.2.0
#   curl -fsSL https://openloadbalancer.dev/install.sh | sh -s -- --prefix /opt/olb/bin
#
# Environment variables:
#   OLB_VERSION   - Version to install (default: latest)
#   OLB_PREFIX    - Installation directory (default: /usr/local/bin)
#
# This script is safe to pipe to sh. It:
#   - Does not modify PATH or shell profiles
#   - Does not install system packages
#   - Only writes the binary to the install directory
#   - Verifies checksums before installing

set -e

# ─── Constants ────────────────────────────────────────────────────────
GITHUB_REPO="openloadbalancer/olb"
BINARY_NAME="olb"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}"
GITHUB_RELEASES="https://github.com/${GITHUB_REPO}/releases"

# ─── Defaults ─────────────────────────────────────────────────────────
VERSION="${OLB_VERSION:-}"
INSTALL_DIR="${OLB_PREFIX:-/usr/local/bin}"
CHECKSUM_VERIFY=1

# ─── Color output (disabled if not a terminal) ───────────────────────
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    BOLD='\033[1m'
    RESET='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    BOLD=''
    RESET=''
fi

# ─── Logging helpers ─────────────────────────────────────────────────
info() {
    printf "${BLUE}==>${RESET} ${BOLD}%s${RESET}\n" "$1"
}

success() {
    printf "${GREEN}==>${RESET} ${BOLD}%s${RESET}\n" "$1"
}

warn() {
    printf "${YELLOW}Warning:${RESET} %s\n" "$1" >&2
}

error() {
    printf "${RED}Error:${RESET} %s\n" "$1" >&2
    exit 1
}

# ─── Parse arguments ─────────────────────────────────────────────────
while [ $# -gt 0 ]; do
    case "$1" in
        --version|-v)
            VERSION="$2"
            shift 2
            ;;
        --version=*)
            VERSION="${1#*=}"
            shift
            ;;
        --prefix|-p)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --prefix=*)
            INSTALL_DIR="${1#*=}"
            shift
            ;;
        --no-checksum)
            CHECKSUM_VERIFY=0
            shift
            ;;
        --help|-h)
            printf "OpenLoadBalancer Install Script\n\n"
            printf "Usage:\n"
            printf "  install.sh [options]\n\n"
            printf "Options:\n"
            printf "  --version, -v <version>   Install specific version (e.g., v0.1.0)\n"
            printf "  --prefix, -p <path>       Installation directory (default: /usr/local/bin)\n"
            printf "  --no-checksum             Skip SHA256 checksum verification\n"
            printf "  --help, -h                Show this help message\n\n"
            printf "Environment:\n"
            printf "  OLB_VERSION               Same as --version\n"
            printf "  OLB_PREFIX                Same as --prefix\n"
            exit 0
            ;;
        *)
            error "Unknown option: $1. Use --help for usage."
            ;;
    esac
done

# ─── Detect platform ─────────────────────────────────────────────────
detect_os() {
    local os
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        linux)   echo "linux" ;;
        darwin)  echo "darwin" ;;
        mingw*|msys*|cygwin*|windows*)
                 echo "windows" ;;
        freebsd) echo "freebsd" ;;
        *)       error "Unsupported operating system: $os" ;;
    esac
}

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)   echo "arm64" ;;
        armv7l|armv6l)   echo "arm" ;;
        *)               error "Unsupported architecture: $arch" ;;
    esac
}

# ─── Check for required tools ────────────────────────────────────────
check_dependencies() {
    local missing=""

    # Need either curl or wget
    if command -v curl >/dev/null 2>&1; then
        DOWNLOAD_CMD="curl"
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOAD_CMD="wget"
    else
        missing="${missing} curl/wget"
    fi

    # Need sha256sum or shasum for checksum verification
    if [ "$CHECKSUM_VERIFY" = "1" ]; then
        if command -v sha256sum >/dev/null 2>&1; then
            SHA_CMD="sha256sum"
        elif command -v shasum >/dev/null 2>&1; then
            SHA_CMD="shasum -a 256"
        else
            warn "Neither sha256sum nor shasum found; skipping checksum verification"
            CHECKSUM_VERIFY=0
        fi
    fi

    # Need tar for extraction (not needed if binary is direct download)
    if ! command -v tar >/dev/null 2>&1; then
        missing="${missing} tar"
    fi

    if [ -n "$missing" ]; then
        error "Missing required tools:${missing}"
    fi
}

# ─── HTTP helpers ─────────────────────────────────────────────────────
http_get() {
    local url="$1"
    if [ "$DOWNLOAD_CMD" = "curl" ]; then
        curl -fsSL "$url"
    else
        wget -qO- "$url"
    fi
}

http_download() {
    local url="$1"
    local output="$2"
    if [ "$DOWNLOAD_CMD" = "curl" ]; then
        curl -fsSL -o "$output" "$url"
    else
        wget -qO "$output" "$url"
    fi
}

# ─── Fetch latest version ────────────────────────────────────────────
get_latest_version() {
    local latest
    latest="$(http_get "${GITHUB_API}/releases/latest" 2>/dev/null | \
        grep '"tag_name"' | \
        sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')" || true

    if [ -z "$latest" ]; then
        # Fallback: parse redirect from latest release URL
        if [ "$DOWNLOAD_CMD" = "curl" ]; then
            latest="$(curl -fsSI -o /dev/null -w '%{url_effective}' "${GITHUB_RELEASES}/latest" 2>/dev/null | \
                sed 's|.*/||')" || true
        fi
    fi

    if [ -z "$latest" ]; then
        error "Could not determine latest version. Specify one with --version."
    fi

    echo "$latest"
}

# ─── Main install logic ──────────────────────────────────────────────
main() {
    info "OpenLoadBalancer Installer"

    check_dependencies

    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"
    info "Detected platform: ${os}/${arch}"

    # Resolve version
    if [ -z "$VERSION" ]; then
        info "Fetching latest release..."
        VERSION="$(get_latest_version)"
    fi
    # Ensure version starts with 'v'
    case "$VERSION" in
        v*) ;;
        *)  VERSION="v${VERSION}" ;;
    esac
    info "Installing OLB ${VERSION}"

    # Build artifact name
    local ext=""
    if [ "$os" = "windows" ]; then
        ext=".exe"
    fi
    local artifact_name="${BINARY_NAME}-${os}-${arch}${ext}"
    local checksum_name="${BINARY_NAME}-${VERSION}-checksums.txt"
    local download_url="${GITHUB_RELEASES}/download/${VERSION}/${artifact_name}"
    local checksum_url="${GITHUB_RELEASES}/download/${VERSION}/${checksum_name}"

    # Create temp directory
    local tmp_dir
    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "$tmp_dir"' EXIT

    # Download binary
    info "Downloading ${download_url}..."
    http_download "$download_url" "${tmp_dir}/${artifact_name}" || \
        error "Failed to download ${download_url}. Check that version ${VERSION} exists."

    # Verify checksum
    if [ "$CHECKSUM_VERIFY" = "1" ]; then
        info "Verifying SHA256 checksum..."
        http_download "$checksum_url" "${tmp_dir}/checksums.txt" || {
            warn "Could not download checksums file; skipping verification"
            CHECKSUM_VERIFY=0
        }

        if [ "$CHECKSUM_VERIFY" = "1" ]; then
            local expected_sum
            expected_sum="$(grep "${artifact_name}" "${tmp_dir}/checksums.txt" | awk '{print $1}')"
            if [ -z "$expected_sum" ]; then
                warn "No checksum found for ${artifact_name} in checksums file; skipping verification"
            else
                local actual_sum
                actual_sum="$(cd "${tmp_dir}" && ${SHA_CMD} "${artifact_name}" | awk '{print $1}')"
                if [ "$expected_sum" != "$actual_sum" ]; then
                    error "Checksum mismatch!\n  Expected: ${expected_sum}\n  Got:      ${actual_sum}\nThe downloaded file may be corrupted or tampered with."
                fi
                success "Checksum verified"
            fi
        fi
    fi

    # Make binary executable
    chmod +x "${tmp_dir}/${artifact_name}"

    # Install binary
    local install_path="${INSTALL_DIR}/${BINARY_NAME}${ext}"

    # Check if we need sudo
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmp_dir}/${artifact_name}" "$install_path"
    elif command -v sudo >/dev/null 2>&1; then
        info "Elevated permissions required to install to ${INSTALL_DIR}"
        sudo mkdir -p "$INSTALL_DIR"
        sudo mv "${tmp_dir}/${artifact_name}" "$install_path"
        sudo chmod +x "$install_path"
    elif command -v doas >/dev/null 2>&1; then
        info "Elevated permissions required to install to ${INSTALL_DIR}"
        doas mkdir -p "$INSTALL_DIR"
        doas mv "${tmp_dir}/${artifact_name}" "$install_path"
        doas chmod +x "$install_path"
    else
        error "Cannot write to ${INSTALL_DIR}. Run with sudo or use --prefix to set a writable path."
    fi

    # ─── Verify installation ──────────────────────────────────────────
    if [ -x "$install_path" ]; then
        success "OLB ${VERSION} installed to ${install_path}"
    else
        error "Installation failed: ${install_path} is not executable"
    fi

    # Print version info
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        "$BINARY_NAME" version 2>/dev/null || true
    fi

    # ─── Next steps ───────────────────────────────────────────────────
    printf "\n"
    success "Installation complete!"
    printf "\n"
    printf "  ${BOLD}Next steps:${RESET}\n"
    printf "\n"
    printf "  1. Generate a default configuration:\n"
    printf "     ${BLUE}\$ olb init${RESET}\n"
    printf "\n"
    printf "  2. Edit the configuration:\n"
    printf "     ${BLUE}\$ \$EDITOR olb.yaml${RESET}\n"
    printf "\n"
    printf "  3. Start the load balancer:\n"
    printf "     ${BLUE}\$ olb start --config olb.yaml${RESET}\n"
    printf "\n"
    printf "  4. Check status:\n"
    printf "     ${BLUE}\$ olb status${RESET}\n"
    printf "\n"
    printf "  Documentation: ${BLUE}https://openloadbalancer.dev${RESET}\n"
    printf "\n"

    # Warn if not in PATH
    case ":$PATH:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            warn "${INSTALL_DIR} is not in your PATH. Add it with:"
            printf "  export PATH=\"\$PATH:${INSTALL_DIR}\"\n" >&2
            ;;
    esac
}

main
