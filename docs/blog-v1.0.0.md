# Announcing OpenLoadBalancer v1.0.0

**A high-performance, zero-dependency load balancer written in Go**

Today we're releasing OpenLoadBalancer (OLB) v1.0.0 — a production-ready load balancer built entirely with Go's standard library. No external dependencies, single binary, batteries included.

## Why Another Load Balancer?

Existing solutions like Nginx, HAProxy, and Envoy are excellent, but they come with complexity: configuration languages to learn, modules to compile, dependencies to manage, and separate tools for monitoring.

OLB takes a different approach:

- **Zero external dependencies** — only Go stdlib (plus `x/crypto` for bcrypt/OCSP)
- **Single binary** — proxy, CLI, Web UI, admin API, MCP server, all in one 9MB binary
- **Built-in observability** — Web dashboard, TUI (`olb top`), Prometheus metrics, structured logging
- **AI-native** — MCP server for integration with Claude and other AI assistants

## Key Features

### Layer 7 + Layer 4 Proxying
- HTTP/HTTPS reverse proxy with WebSocket, gRPC, SSE, HTTP/2 support
- TCP/UDP proxy with zero-copy splice on Linux
- SNI-based routing, TLS passthrough, PROXY protocol v1/v2

### 12 Load Balancing Algorithms
Round Robin, Weighted Round Robin, Least Connections, Least Response Time, IP Hash, Consistent Hash (Ketama), Maglev, Power of Two Choices, Random, Weighted Random, Ring Hash, and Sticky Sessions.

### Security
- Automatic TLS via ACME/Let's Encrypt
- mTLS for client and backend authentication
- OCSP stapling
- WAF with SQL injection, XSS, path traversal, command injection detection
- Rate limiting, IP filtering, circuit breaker
- Request smuggling and header injection protection

### Web UI Dashboard
8-page SPA dashboard with live metrics, backend management, route configuration, log streaming, certificate management, and cluster status — all in 441KB of vanilla JS/CSS.

### Multi-Node Clustering
Raft consensus for configuration replication, SWIM gossip for failure detection, distributed rate limiting with CRDT counters, and session affinity table propagation.

### AI Integration (MCP)
Built-in MCP server with 8 tools (`olb_query_metrics`, `olb_diagnose`, `olb_list_backends`, etc.), 4 resources, and 3 prompt templates. Point Claude Code at your load balancer for AI-powered operations.

## Performance

| Operation | Result | Allocations |
|-----------|--------|-------------|
| RoundRobin.Next | 3.5 ns/op | 0 allocs |
| Router lookup (100 routes) | ~500 ns/op | 6 allocs |
| HTTP proxy (1KB) | ~70 us/op | 145 allocs |
| HTTP proxy (1MB) | ~2.4 ms/op | 442 MB/s |

Binary size: 9.1MB. Web UI: 441KB. Startup: <100ms.

## Quick Start

```sh
# Install
curl -fsSL https://openloadbalancer.dev/install.sh | sh

# Or with Homebrew
brew tap openloadbalancer/olb && brew install olb

# Or with Docker
docker pull ghcr.io/openloadbalancer/olb:v1.0.0

# Create minimal config
cat > olb.yaml << 'YAML'
listeners:
  - name: http
    address: ":8080"
    routes:
      - path: /
        backend: myapp

pools:
  - name: myapp
    backends:
      - address: "localhost:3001"
      - address: "localhost:3002"
      - address: "localhost:3003"
YAML

# Start
olb start --config olb.yaml
```

## What's Inside

- **307 files**, **233 Go files**, **104 test files**
- **~160K+ lines** of code
- Full test suite with race detector
- Cross-platform builds (Linux, macOS, Windows, FreeBSD)
- Comprehensive documentation (6 guides + API reference)

## Links

- **Website**: https://openloadbalancer.dev
- **GitHub**: https://github.com/openloadbalancer/olb
- **Release**: https://github.com/openloadbalancer/olb/releases/tag/v1.0.0
- **Documentation**: https://openloadbalancer.dev/docs
- **Homebrew**: `brew tap openloadbalancer/olb && brew install olb`

## License

Apache 2.0

---

Built with Go and Claude Code.
