# Changelog

All notable changes to OpenLoadBalancer will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Geo-DNS routing with country/region-based traffic routing
- Request shadowing/mirroring for traffic replication to staging
- Distributed rate limiting with Redis backend support
- Production deployment guide with Kubernetes, Docker, systemd examples
- Troubleshooting playbook with diagnostics and emergency procedures
- Enhanced Helm charts with HPA, PDB, ServiceMonitor, NetworkPolicy
- Grafana dashboard with 20+ monitoring panels
- SECURITY.md with vulnerability reporting policy
- Integration tests for GeoDNS package (90.7% coverage)

### Changed
- CI/CD workflow fixed (binary and release jobs separated)
- Code coverage improved to 87.7%

## [1.0.0] - 2025-XX-XX

### Added
- Initial release of OpenLoadBalancer
- L4/L7 proxy with HTTP/HTTPS, WebSocket, gRPC, SSE, TCP, UDP support
- 14 load balancing algorithms (round_robin, least_connections, maglev, etc.)
- 6-layer WAF with SQLi, XSS, CMDi, path traversal, XXE, SSRF detection
- Bot detection with JA3 fingerprinting
- TLS/mTLS with ACME/Let's Encrypt support
- Clustering with Raft consensus and SWIM gossip
- MCP server for AI integration (17 tools)
- Admin REST API with Web UI dashboard
- Hot config reload (YAML/JSON/TOML/HCL)
- Service discovery (Static/DNS/Consul/Docker/File)
- 16 middleware components
- Prometheus metrics
- Plugin system with event bus
- 56 end-to-end tests
- 87%+ test coverage

[Unreleased]: https://github.com/openloadbalancer/olb/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/openloadbalancer/olb/releases/tag/v1.0.0
