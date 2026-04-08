# Changelog

All notable changes to OpenLoadBalancer will be documented in this file.

The format is based on [Keep a Changelog](https://keepchangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-04-09

### Added
- L4/L7 proxy with HTTP/HTTPS, WebSocket, gRPC, gRPC-Web, SSE, TCP, UDP, SNI routing, PROXY protocol v1/v2 support
- 16 load balancing algorithms (round_robin, weighted_round_robin, least_connections, weighted_least_connections, least_response_time, weighted_least_response_time, ip_hash, consistent_hash, maglev, ring_hash, power_of_two, random, weighted_random, rendezvous_hash, peak_ewma, sticky_sessions)
- 6-layer WAF with SQLi, XSS, CMDi, path traversal, XXE, SSRF detection
- Bot detection with JA3 fingerprinting
- TLS/mTLS with ACME/Let's Encrypt and OCSP stapling support
- Clustering with Raft consensus and SWIM gossip
- MCP server for AI integration (17 tools)
- Admin REST API with Web UI dashboard
- CSRF protection for admin API
- Circuit breaker for admin API backend calls
- Hot config reload (YAML/JSON/TOML/HCL)
- Service discovery (Static/DNS/Consul/Docker/File)
- 30+ middleware components (rate_limit, cors, compression, auth, cache, circuit_breaker, timeout, ip_filter, trace, coalesce, rewrite, max_body_size, hmac, apikey, etc.)
- Prometheus metrics with sharded counters for high-concurrency performance
- Connection pooling with Prometheus gauges for idle/active/hits/misses/evictions
- Plugin system with event bus
- Geo-DNS routing with country/region-based traffic routing
- Intelligent request shadowing/mirroring (percentage-based, header-matched, time-windowed)
- Production deployment guide with Kubernetes, Docker, systemd examples
- Troubleshooting playbook with diagnostics and emergency procedures
- Enhanced Helm charts with HPA, PDB, ServiceMonitor, NetworkPolicy
- Grafana dashboard with 20+ monitoring panels and import guide
- Performance tuning guide for high RPS, high concurrency, and high bandwidth workloads
- SECURITY.md with vulnerability reporting policy
- OpenAPI 3.0 spec for Admin API
- Migration guide from NGINX, HAProxy, Traefik, Envoy, AWS ALB
- Getting started tutorial
- Terraform AWS module
- Prometheus alerting rules
- Docker Compose full stack
- Comprehensive config validation for all middleware and server settings
- Per-package benchmark profiling with CPU/memory profiles
- Configurable HTTP transport pool settings (idle timeout, max conns per host)
- WebSocket connection limit enforcement
- HTTP/2 strict mode with HPACK and settings frame flood protection
- Request body logging opt-in for security

### Changed
- Binary size is ~13 MB (statically linked, CGO_ENABLED=0)
- 3 Go dependencies: golang.org/x/crypto, golang.org/x/net, golang.org/x/text
- Code coverage: 87%+ across 67 packages
- 56 end-to-end tests
- CI/CD workflow fixed (binary and release jobs separated)

[1.0.0]: https://github.com/openloadbalancer/olb/releases/tag/v1.0.0
