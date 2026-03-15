#!/bin/bash
# OpenLoadBalancer (OLB) - DEB/RPM Package Builder
#
# Builds .deb and .rpm packages for distribution.
#
# Usage:
#   ./scripts/build-packages.sh                    # Build both DEB and RPM
#   ./scripts/build-packages.sh --deb              # Build DEB only
#   ./scripts/build-packages.sh --rpm              # Build RPM only
#   ./scripts/build-packages.sh --version 0.2.0    # Override version
#   ./scripts/build-packages.sh --arch arm64        # Override architecture
#
# Requirements:
#   DEB: dpkg-deb
#   RPM: rpmbuild
#   Both: The olb binary must be pre-built in bin/ (run `make build` first)

set -euo pipefail

# ─── Configuration ────────────────────────────────────────────────────
PROJECT_NAME="olb"
PROJECT_DESCRIPTION="OpenLoadBalancer - High-performance, zero-dependency load balancer"
PROJECT_URL="https://github.com/openloadbalancer/olb"
MAINTAINER="OpenLoadBalancer Team <hello@openloadbalancer.dev>"
LICENSE="Apache-2.0"
VENDOR="OpenLoadBalancer"

# Resolve directories
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Defaults
VERSION=""
ARCH=""
BUILD_DEB=1
BUILD_RPM=1
OUTPUT_DIR="${PROJECT_ROOT}/dist/packages"

# ─── Parse arguments ─────────────────────────────────────────────────
while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            VERSION="$2"
            shift 2
            ;;
        --version=*)
            VERSION="${1#*=}"
            shift
            ;;
        --arch)
            ARCH="$2"
            shift 2
            ;;
        --arch=*)
            ARCH="${1#*=}"
            shift
            ;;
        --deb)
            BUILD_DEB=1
            BUILD_RPM=0
            shift
            ;;
        --rpm)
            BUILD_DEB=0
            BUILD_RPM=1
            shift
            ;;
        --output|-o)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --help|-h)
            echo "OpenLoadBalancer Package Builder"
            echo ""
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --version <ver>   Package version (default: from Makefile or git tag)"
            echo "  --arch <arch>     Target architecture: amd64, arm64 (default: host arch)"
            echo "  --deb             Build DEB package only"
            echo "  --rpm             Build RPM package only"
            echo "  --output, -o      Output directory (default: dist/packages)"
            echo "  --help, -h        Show this help"
            exit 0
            ;;
        *)
            echo "Error: Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

# ─── Detect version ──────────────────────────────────────────────────
if [ -z "$VERSION" ]; then
    # Try git tag first
    VERSION="$(git -C "$PROJECT_ROOT" describe --tags --abbrev=0 2>/dev/null || true)"
    VERSION="${VERSION#v}"  # Strip leading 'v'

    # Fallback to Makefile
    if [ -z "$VERSION" ]; then
        VERSION="$(grep '^VERSION' "${PROJECT_ROOT}/Makefile" | head -1 | awk -F':=' '{print $2}' | tr -d ' ')"
    fi

    # Final fallback
    if [ -z "$VERSION" ]; then
        VERSION="0.1.0"
    fi
fi
VERSION="${VERSION#v}"  # Ensure no leading 'v'

# ─── Detect architecture ─────────────────────────────────────────────
if [ -z "$ARCH" ]; then
    case "$(uname -m)" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)   ARCH="arm64" ;;
        *)               ARCH="amd64" ;;
    esac
fi

# Map to DEB/RPM arch names
case "$ARCH" in
    amd64)
        DEB_ARCH="amd64"
        RPM_ARCH="x86_64"
        ;;
    arm64)
        DEB_ARCH="arm64"
        RPM_ARCH="aarch64"
        ;;
    *)
        echo "Error: Unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

# ─── Locate binary ───────────────────────────────────────────────────
BINARY="${PROJECT_ROOT}/bin/${PROJECT_NAME}-linux-${ARCH}"
if [ ! -f "$BINARY" ]; then
    BINARY="${PROJECT_ROOT}/bin/${PROJECT_NAME}"
fi
if [ ! -f "$BINARY" ]; then
    echo "Error: Binary not found. Run 'make build' or 'make build-linux' first." >&2
    echo "  Expected: ${PROJECT_ROOT}/bin/${PROJECT_NAME}-linux-${ARCH}" >&2
    echo "        or: ${PROJECT_ROOT}/bin/${PROJECT_NAME}" >&2
    exit 1
fi

# ─── Prepare output directory ────────────────────────────────────────
mkdir -p "$OUTPUT_DIR"

echo "========================================"
echo " OLB Package Builder"
echo "========================================"
echo " Version:       ${VERSION}"
echo " Architecture:  ${ARCH}"
echo " Binary:        ${BINARY}"
echo " Output:        ${OUTPUT_DIR}"
echo "========================================"

# ─── Create default config ───────────────────────────────────────────
create_default_config() {
    local config_file="$1"
    cat > "$config_file" << 'YAML'
# OpenLoadBalancer Default Configuration
# See: https://github.com/openloadbalancer/olb

listeners:
  - address: ":8080"
    protocol: http

admin:
  address: ":9090"
  enabled: true

logging:
  level: info
  format: json
  output: /var/log/olb/olb.log

backends: []

routes: []
YAML
}

# ─── Create man page placeholder ─────────────────────────────────────
create_man_page() {
    local man_file="$1"
    cat > "$man_file" << MANPAGE
.TH OLB 1 "$(date +%Y-%m-%d)" "${VERSION}" "OpenLoadBalancer Manual"
.SH NAME
olb \- high-performance, zero-dependency load balancer
.SH SYNOPSIS
.B olb
[\fIcommand\fR] [\fIoptions\fR]
.SH DESCRIPTION
.B olb
is a high-performance, zero-external-dependency load balancer written in Go.
It supports HTTP/HTTPS, WebSocket, gRPC, TCP (L4), and more.
.PP
Single binary includes: reverse proxy, CLI, web dashboard, admin API,
cluster agent, and ACME certificate management.
.SH COMMANDS
.TP
.B start
Start the load balancer with the specified configuration.
.TP
.B stop
Stop a running load balancer instance.
.TP
.B reload
Reload configuration without downtime.
.TP
.B status
Show current status of the load balancer.
.TP
.B health
Run a health check against the load balancer.
.TP
.B version
Print version information.
.TP
.B top
Live TUI dashboard with real-time metrics.
.TP
.B init
Generate a default configuration file.
.SH OPTIONS
.TP
.BR \-\-config " " \fIpath\fR
Path to configuration file (default: /etc/olb/olb.yaml).
.TP
.BR \-\-log\-level " " \fIlevel\fR
Log level: debug, info, warn, error (default: info).
.SH FILES
.TP
.I /etc/olb/olb.yaml
Default configuration file.
.TP
.I /var/log/olb/
Log directory.
.TP
.I /var/lib/olb/
Data directory (certificates, state).
.SH ENVIRONMENT
.TP
.B OLB_CONFIG
Override default configuration file path.
.TP
.B GOMAXPROCS
Set the number of OS threads for the Go runtime.
.SH EXIT STATUS
.TP
.B 0
Successful completion.
.TP
.B 1
An error occurred.
.SH AUTHOR
OpenLoadBalancer Team
.SH SEE ALSO
.BR nginx (1),
.BR haproxy (1)
.PP
Full documentation: <${PROJECT_URL}>
MANPAGE
}

# ═══════════════════════════════════════════════════════════════════════
# DEB Package Build
# ═══════════════════════════════════════════════════════════════════════
build_deb() {
    echo ""
    echo "--- Building DEB package ---"

    if ! command -v dpkg-deb >/dev/null 2>&1; then
        echo "Warning: dpkg-deb not found, skipping DEB build" >&2
        return 1
    fi

    local pkg_name="${PROJECT_NAME}_${VERSION}_${DEB_ARCH}"
    local build_dir="${OUTPUT_DIR}/deb-build/${pkg_name}"

    # Clean previous build
    rm -rf "$build_dir"

    # Create directory structure
    mkdir -p "${build_dir}/DEBIAN"
    mkdir -p "${build_dir}/usr/local/bin"
    mkdir -p "${build_dir}/etc/olb"
    mkdir -p "${build_dir}/var/log/olb"
    mkdir -p "${build_dir}/var/lib/olb"
    mkdir -p "${build_dir}/lib/systemd/system"
    mkdir -p "${build_dir}/usr/share/man/man1"
    mkdir -p "${build_dir}/usr/share/doc/${PROJECT_NAME}"

    # ── DEBIAN/control ────────────────────────────────────────────────
    cat > "${build_dir}/DEBIAN/control" << EOF
Package: ${PROJECT_NAME}
Version: ${VERSION}
Section: net
Priority: optional
Architecture: ${DEB_ARCH}
Maintainer: ${MAINTAINER}
Homepage: ${PROJECT_URL}
Description: ${PROJECT_DESCRIPTION}
 OpenLoadBalancer is a high-performance, zero-external-dependency
 load balancer written in Go. It features HTTP/HTTPS reverse proxy,
 WebSocket and gRPC support, TCP (L4) proxying, automatic TLS with
 ACME/Let's Encrypt, clustering with Raft consensus, a built-in
 web dashboard, and much more -- all in a single binary.
Depends: adduser
Recommends: ca-certificates
EOF

    # ── DEBIAN/conffiles ──────────────────────────────────────────────
    cat > "${build_dir}/DEBIAN/conffiles" << EOF
/etc/olb/olb.yaml
EOF

    # ── DEBIAN/preinst ────────────────────────────────────────────────
    cat > "${build_dir}/DEBIAN/preinst" << 'EOF'
#!/bin/sh
set -e

# Create olb user and group if they don't exist
if ! getent group olb >/dev/null 2>&1; then
    addgroup --system olb
fi
if ! getent passwd olb >/dev/null 2>&1; then
    adduser --system --ingroup olb --home /var/lib/olb \
        --no-create-home --shell /usr/sbin/nologin \
        --gecos "OpenLoadBalancer" olb
fi

exit 0
EOF
    chmod 0755 "${build_dir}/DEBIAN/preinst"

    # ── DEBIAN/postinst ───────────────────────────────────────────────
    cat > "${build_dir}/DEBIAN/postinst" << 'EOF'
#!/bin/sh
set -e

# Set ownership on directories
chown -R olb:olb /var/log/olb
chown -R olb:olb /var/lib/olb
chmod 750 /var/log/olb
chmod 750 /var/lib/olb

# Reload systemd
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi

echo ""
echo "OpenLoadBalancer has been installed."
echo ""
echo "  Enable and start:  systemctl enable --now olb"
echo "  Configuration:     /etc/olb/olb.yaml"
echo "  Logs:              journalctl -u olb -f"
echo ""

exit 0
EOF
    chmod 0755 "${build_dir}/DEBIAN/postinst"

    # ── DEBIAN/prerm ──────────────────────────────────────────────────
    cat > "${build_dir}/DEBIAN/prerm" << 'EOF'
#!/bin/sh
set -e

# Stop service before removal
if command -v systemctl >/dev/null 2>&1; then
    if systemctl is-active --quiet olb 2>/dev/null; then
        systemctl stop olb
    fi
    if systemctl is-enabled --quiet olb 2>/dev/null; then
        systemctl disable olb
    fi
fi

exit 0
EOF
    chmod 0755 "${build_dir}/DEBIAN/prerm"

    # ── DEBIAN/postrm ─────────────────────────────────────────────────
    cat > "${build_dir}/DEBIAN/postrm" << 'EOF'
#!/bin/sh
set -e

if [ "$1" = "purge" ]; then
    # Remove config, logs, and data on purge
    rm -rf /etc/olb /var/log/olb /var/lib/olb

    # Remove user and group
    if getent passwd olb >/dev/null 2>&1; then
        deluser --system olb >/dev/null 2>&1 || true
    fi
    if getent group olb >/dev/null 2>&1; then
        delgroup --system olb >/dev/null 2>&1 || true
    fi
fi

# Reload systemd
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi

exit 0
EOF
    chmod 0755 "${build_dir}/DEBIAN/postrm"

    # ── Install files ─────────────────────────────────────────────────
    # Binary
    cp "$BINARY" "${build_dir}/usr/local/bin/${PROJECT_NAME}"
    chmod 0755 "${build_dir}/usr/local/bin/${PROJECT_NAME}"

    # Default config
    create_default_config "${build_dir}/etc/olb/olb.yaml"
    chmod 0644 "${build_dir}/etc/olb/olb.yaml"

    # Systemd service
    cp "${PROJECT_ROOT}/init/olb.service" "${build_dir}/lib/systemd/system/olb.service"
    chmod 0644 "${build_dir}/lib/systemd/system/olb.service"

    # Man page
    create_man_page "${build_dir}/usr/share/man/man1/olb.1"
    gzip -9 "${build_dir}/usr/share/man/man1/olb.1"

    # Copyright / license
    cp "${PROJECT_ROOT}/LICENSE" "${build_dir}/usr/share/doc/${PROJECT_NAME}/copyright" 2>/dev/null || \
        echo "Apache-2.0" > "${build_dir}/usr/share/doc/${PROJECT_NAME}/copyright"

    # ── Build the package ─────────────────────────────────────────────
    local deb_file="${OUTPUT_DIR}/${pkg_name}.deb"
    dpkg-deb --build --root-owner-group "$build_dir" "$deb_file"

    # Verify
    echo "DEB package contents:"
    dpkg-deb --contents "$deb_file" | head -20
    echo ""
    echo "DEB package info:"
    dpkg-deb --info "$deb_file"

    # Clean up build dir
    rm -rf "${OUTPUT_DIR}/deb-build"

    echo ""
    echo "DEB package created: ${deb_file}"
    echo "  Install: sudo dpkg -i ${deb_file}"
}

# ═══════════════════════════════════════════════════════════════════════
# RPM Package Build
# ═══════════════════════════════════════════════════════════════════════
build_rpm() {
    echo ""
    echo "--- Building RPM package ---"

    if ! command -v rpmbuild >/dev/null 2>&1; then
        echo "Warning: rpmbuild not found, skipping RPM build" >&2
        return 1
    fi

    local rpm_version="${VERSION}"
    local rpm_release="1"
    local rpmbuild_dir="${OUTPUT_DIR}/rpmbuild"

    # Clean previous build
    rm -rf "$rpmbuild_dir"

    # Create RPM build directory structure
    mkdir -p "${rpmbuild_dir}"/{BUILD,RPMS,SOURCES,SPECS,SRPMS}

    # ── Create tarball for SOURCES ────────────────────────────────────
    local tarball_name="${PROJECT_NAME}-${rpm_version}"
    local tarball_dir="${rpmbuild_dir}/SOURCES/${tarball_name}"
    mkdir -p "$tarball_dir"

    # Binary
    cp "$BINARY" "${tarball_dir}/${PROJECT_NAME}"
    chmod 0755 "${tarball_dir}/${PROJECT_NAME}"

    # Config
    create_default_config "${tarball_dir}/olb.yaml"

    # Systemd service
    cp "${PROJECT_ROOT}/init/olb.service" "${tarball_dir}/olb.service"

    # Man page
    create_man_page "${tarball_dir}/olb.1"

    # Create tarball
    tar czf "${rpmbuild_dir}/SOURCES/${tarball_name}.tar.gz" \
        -C "${rpmbuild_dir}/SOURCES" "${tarball_name}"
    rm -rf "$tarball_dir"

    # ── Create spec file ──────────────────────────────────────────────
    cat > "${rpmbuild_dir}/SPECS/${PROJECT_NAME}.spec" << EOF
Name:           ${PROJECT_NAME}
Version:        ${rpm_version}
Release:        ${rpm_release}%{?dist}
Summary:        ${PROJECT_DESCRIPTION}
License:        ${LICENSE}
URL:            ${PROJECT_URL}
Source0:        %{name}-%{version}.tar.gz
BuildArch:      ${RPM_ARCH}

# No build dependencies (pre-built binary)
# No auto-requires (statically linked)
AutoReqProv:    no

%description
OpenLoadBalancer is a high-performance, zero-external-dependency
load balancer written in Go. It features HTTP/HTTPS reverse proxy,
WebSocket and gRPC support, TCP (L4) proxying, automatic TLS with
ACME/Let's Encrypt, clustering with Raft consensus, a built-in
web dashboard, and much more -- all in a single binary.

%prep
%setup -q

%install
rm -rf %{buildroot}

# Binary
install -D -m 0755 %{name} %{buildroot}/usr/local/bin/%{name}

# Configuration
install -D -m 0644 olb.yaml %{buildroot}/etc/olb/olb.yaml

# Systemd service
install -D -m 0644 olb.service %{buildroot}/usr/lib/systemd/system/olb.service

# Man page
install -D -m 0644 olb.1 %{buildroot}/usr/share/man/man1/olb.1
gzip %{buildroot}/usr/share/man/man1/olb.1

# Create directories
install -d -m 0750 %{buildroot}/var/log/olb
install -d -m 0750 %{buildroot}/var/lib/olb

%pre
# Create olb user and group
getent group olb >/dev/null 2>&1 || groupadd -r olb
getent passwd olb >/dev/null 2>&1 || \
    useradd -r -g olb -d /var/lib/olb -s /sbin/nologin \
    -c "OpenLoadBalancer" olb
exit 0

%post
# Set ownership
chown -R olb:olb /var/log/olb
chown -R olb:olb /var/lib/olb

# Reload systemd
systemctl daemon-reload 2>/dev/null || true

echo ""
echo "OpenLoadBalancer has been installed."
echo ""
echo "  Enable and start:  systemctl enable --now olb"
echo "  Configuration:     /etc/olb/olb.yaml"
echo "  Logs:              journalctl -u olb -f"
echo ""

%preun
# Stop and disable service on uninstall (not upgrade)
if [ \$1 -eq 0 ]; then
    systemctl stop olb 2>/dev/null || true
    systemctl disable olb 2>/dev/null || true
fi

%postun
systemctl daemon-reload 2>/dev/null || true

# Remove user/group on full uninstall
if [ \$1 -eq 0 ]; then
    userdel olb 2>/dev/null || true
    groupdel olb 2>/dev/null || true
fi

%files
%defattr(-,root,root,-)
/usr/local/bin/%{name}
%config(noreplace) /etc/olb/olb.yaml
/usr/lib/systemd/system/olb.service
/usr/share/man/man1/olb.1.gz
%dir %attr(0750,olb,olb) /var/log/olb
%dir %attr(0750,olb,olb) /var/lib/olb

%changelog
* $(date "+%a %b %d %Y") OpenLoadBalancer Team <hello@openloadbalancer.dev> - ${rpm_version}-${rpm_release}
- Release ${rpm_version}
EOF

    # ── Build the RPM ─────────────────────────────────────────────────
    rpmbuild \
        --define "_topdir ${rpmbuild_dir}" \
        --define "_rpmdir ${OUTPUT_DIR}" \
        -bb "${rpmbuild_dir}/SPECS/${PROJECT_NAME}.spec"

    # Find the built RPM
    local rpm_file
    rpm_file="$(find "${OUTPUT_DIR}" -name "*.rpm" -type f | head -1)"

    if [ -n "$rpm_file" ]; then
        echo ""
        echo "RPM package created: ${rpm_file}"
        echo "  Install: sudo rpm -i ${rpm_file}"
        echo "      or:  sudo dnf install ${rpm_file}"
    fi

    # Clean up build dir
    rm -rf "$rpmbuild_dir"
}

# ═══════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════

if [ "$BUILD_DEB" -eq 1 ]; then
    build_deb || echo "DEB build skipped (missing dpkg-deb)"
fi

if [ "$BUILD_RPM" -eq 1 ]; then
    build_rpm || echo "RPM build skipped (missing rpmbuild)"
fi

echo ""
echo "========================================"
echo " Build complete"
echo "========================================"
ls -lh "${OUTPUT_DIR}"/*.deb "${OUTPUT_DIR}"/*.rpm 2>/dev/null || \
    echo " No packages in ${OUTPUT_DIR}"
echo "========================================"
