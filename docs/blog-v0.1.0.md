# Announcing OpenLoadBalancer v0.1.0

**A high-performance, minimal-dependency load balancer written in Go**

Today we're releasing OpenLoadBalancer (OLB) v0.1.0 — a production-ready load balancer built with Go's standard library. Only 3 external dependencies (golang.org/x/{crypto,net,text}), single binary, batteries included.

## Why Another Load Balancer?

Existing solutions like Nginx, HAProxy, and Envoy are excellent, but they come with complexity: configuration languages to learn, modules to compile, dependencies to manage, and separate tools for monitoring.

OLB takes a different approach:

- **Minimal external dependencies** — stdlib + golang.org/x/{crypto,net,text} only
- **Single binary** — proxy, CLI, Web UI, admin API, MCP server, all in one ~13MB binary
- **Built-in observability** — Web dashboard, TUI (`olb top`), Prometheus metrics, structured logging
- **AI-native** — MCP server for integration with Claude and other AI assistants

## Key Features

### Layer 7 + Layer 4 Proxying
- HTTP/HTTPS reverse proxy with WebSocket, gRPC, gRPC-Web, SSE, HTTP/2 support
- TCP/UDP proxy with zero-copy splice on Linux
- SNI-based routing, TLS passthrough, PROXY protocol v1/v2
- Intelligent request shadowing/mirroring with percentage and header-based controls

### 16 Load Balancing Algorithms
Round Robin, Weighted Round Robin, Least Connections, Weighted Least Connections, Least Response Time, Weighted Least Response Time, IP Hash, Consistent Hash (Ketama), Maglev, Ring Hash, Power of Two Choices, Random, Weighted Random, Rendezvous Hash, Peak EWMA, and Sticky Sessions.

### Security
- Automatic TLS via ACME/Let's Encrypt
- mTLS for client and backend authentication
- OCSP stapling
- 6-layer WAF with SQL injection, XSS, path traversal, command injection, XXE, SSRF detection
- Bot detection with JA3 fingerprinting
- Rate limiting, IP filtering, circuit breaker
- Request smuggling and header injection protection
- CSRF protection for admin API
- Configurable admin rate limiting (default: 60 req/min per IP)
- Configurable WebSocket TLS verification
- HTTP/2 strict mode with flood protection

### Web UI Dashboard
SPA dashboard with live metrics, backend management, route configuration, log streaming, certificate management, and cluster status — all in vanilla JS/CSS embedded in the binary.

### Multi-Node Clustering
Raft consensus for configuration replication, SWIM gossip for failure detection, distributed rate limiting with CRDT counters, and session affinity table propagation.

### AI Integration (MCP)
Built-in MCP server with 17 tools (`olb_query_metrics`, `olb_diagnose`, `olb_list_backends`, etc.), 4 resources, and 3 prompt templates. Point Claude Code at your load balancer for AI-powered operations.

### Comprehensive Config Validation
All middleware and server settings are validated on startup — catch conflicting settings before they cause runtime issues.

## Performance

| Operation | Result | Allocations |
|-----------|--------|-------------|
| RoundRobin.Next | 3.5 ns/op | 0 allocs |
| Sharded Counter.Inc (parallel) | ~15 ns/op | 0 allocs |
| Router lookup (100 routes) | ~500 ns/op | 6 allocs |
| HTTP proxy (1KB) | ~70 us/op | 145 allocs |
| HTTP proxy (1MB) | ~2.4 ms/op | 442 MB/s |

Binary size: ~13MB. Startup: <100ms.

### Connection Pooling
Built-in connection pooling with configurable idle timeout, max lifetime, and eviction tracking. Prometheus gauges for idle/active connections, hits, misses, and evictions.

## Quick Start

```sh
# Install
curl -fsSL https://openloadbalancer.dev/install.sh | sh

# Or with Homebrew
brew tap openloadbalancer/olb && brew install olb

# Or with Docker
docker pull ghcr.io/openloadbalancer/olb:v0.1.0

# Create minimal config
cat > olb.yaml << 'YAML'
listeners:
  - name: http
    address: ":8080"
    protocol: http
    routes:
      - path: /
        pool: myapp

pools:
  - name: myapp
    algorithm: round_robin
    backends:
      - address: "localhost:3001"
      - address: "localhost:3002"
      - address: "localhost:3003"
    health_check:
      type: http
      path: /health
      interval: 10s
YAML

# Start
olb start --config olb.yaml
```

## Documentation

- [Getting Started](getting-started.md)
- [Configuration Reference](configuration.md)
- [API Reference](api.md)
- [Performance Tuning](performance.md)
- [Grafana Dashboard Setup](grafana.md)
- [Architecture Decisions](architecture-decisions.md)
- [Migration Guide](migration-guide.md) (from NGINX, HAProxy, Traefik, Envoy, AWS ALB)
- [Production Deployment](production-deployment.md)
- [Troubleshooting](troubleshooting.md)
- [Contributing](../CONTRIBUTING.md)

## Security Audit

Full security audit performed with gosec and govulncheck. 0 vulnerabilities in application code. 4 stdlib vulnerabilities addressed by Go 1.26.2 toolchain upgrade. [Full report](security-audit.md).

## Links

- **Website**: https://openloadbalancer.dev
- **GitHub**: https://github.com/openloadbalancer/olb
- **Release**: https://github.com/openloadbalancer/olb/releases/tag/v0.1.0
- **Documentation**: https://openloadbalancer.dev/docs

## License

Apache 2.0

---

Built with Go and Claude Code.
