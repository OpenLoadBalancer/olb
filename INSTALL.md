# Installation Guide

OpenLoadBalancer can be installed in multiple ways depending on your environment and use case.

## Quick Start

### Binary (Linux/macOS/Windows)

```bash
# Download latest release
curl -L https://github.com/openloadbalancer/olb/releases/latest/download/olb_$(uname -s)_$(uname -m).tar.gz | tar xz

# Move to PATH
sudo mv olb /usr/local/bin/

# Create config directory
sudo mkdir -p /etc/olb
sudo cp configs/olb.yaml.example /etc/olb/olb.yaml

# Edit configuration
sudo nano /etc/olb/olb.yaml

# Run
olb --config /etc/olb/olb.yaml
```

### Docker

```bash
docker run -d \
  --name olb \
  -p 8080:8080 \
  -p 8081:8081 \
  -v $(pwd)/configs:/etc/olb \
  openloadbalancer/olb:latest
```

### Docker Compose

```bash
# Clone repo
git clone https://github.com/openloadbalancer/olb.git
cd olb

# Edit config
cp configs/olb.yaml.example configs/olb.yaml
nano configs/olb.yaml

# Run
docker-compose up -d
```

## Package Managers

### Homebrew (macOS/Linux)

```bash
brew tap openloadbalancer/tap
brew install olb
```

### APT (Debian/Ubuntu)

```bash
# Add repository
echo "deb [trusted=yes] https://apt.openloadbalancer.dev stable main" | sudo tee /etc/apt/sources.list.d/olb.list

# Install
sudo apt update
sudo apt install olb
```

### YUM/DNF (RHEL/CentOS/Fedora)

```bash
# Add repository
sudo tee /etc/yum.repos.d/olb.repo <<EOF
[olb]
name=OpenLoadBalancer
baseurl=https://yum.openloadbalancer.dev/stable/\$basearch
enabled=1
gpgcheck=0
EOF

# Install
sudo yum install olb
```

### APK (Alpine Linux)

```bash
# Add repository
echo "https://apk.openloadbalancer.dev/stable" | sudo tee -a /etc/apk/repositories

# Install
sudo apk add olb
```

## Kubernetes

### Helm

```bash
# Add Helm repo
helm repo add olb https://charts.openloadbalancer.dev
helm repo update

# Install
helm install my-olb olb/olb

# With custom values
helm install my-olb olb/olb -f my-values.yaml
```

### kubectl

```bash
kubectl apply -f https://raw.githubusercontent.com/openloadbalancer/olb/main/deploy/kubernetes/olb.yaml
```

## Systemd (Linux)

```bash
# Install binary
sudo cp olb /usr/local/bin/
sudo chmod +x /usr/local/bin/olb

# Install systemd service
sudo cp systemd/olb.service /etc/systemd/system/
sudo systemctl daemon-reload

# Create directories
sudo mkdir -p /etc/olb /var/lib/olb /var/log/olb
sudo cp configs/olb.yaml.example /etc/olb/olb.yaml

# Start service
sudo systemctl enable olb
sudo systemctl start olb

# Check status
sudo systemctl status olb
sudo journalctl -u olb -f
```

## Build from Source

### Requirements

- Go 1.25+
- Node.js 20+ (for WebUI)
- pnpm 9+

### Steps

```bash
# Clone repo
git clone https://github.com/openloadbalancer/olb.git
cd olb

# Build Go binary
go build -o olb ./cmd/olb

# Build WebUI (optional)
cd internal/webui
pnpm install
pnpm build
cd ../..

# Run tests
go test ./...

# Install
sudo cp olb /usr/local/bin/
```

## Configuration

After installation, edit `/etc/olb/olb.yaml`:

```yaml
listeners:
  - name: http
    address: ":8080"
    protocol: http
    routes:
      - path: /
        pool: default

pools:
  - name: default
    algorithm: round_robin
    backends:
      - address: "localhost:3001"
      - address: "localhost:3002"
```

## Verification

```bash
# Check version
olb --version

# Validate config
olb config validate /etc/olb/olb.yaml

# Health check
curl http://localhost:8081/api/v1/system/health
```

## Uninstallation

### Binary
```bash
sudo rm /usr/local/bin/olb
sudo rm -rf /etc/olb /var/lib/olb
```

### Systemd
```bash
sudo systemctl stop olb
sudo systemctl disable olb
sudo rm /etc/systemd/system/olb.service
sudo systemctl daemon-reload
```

### Docker
```bash
docker stop olb
docker rm olb
```

### Helm
```bash
helm uninstall my-olb
```

## Support

- Documentation: https://openloadbalancer.dev/docs
- Issues: https://github.com/openloadbalancer/olb/issues
- Discussions: https://github.com/openloadbalancer/olb/discussions
