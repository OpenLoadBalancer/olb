# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability within OpenLoadBalancer, please follow these steps:

1. **Do not** open a public issue
2. Email security@openloadbalancer.dev with details
3. Include steps to reproduce, impact assessment, and suggested fix if possible
4. Allow up to 48 hours for initial response
5. We will coordinate disclosure timeline with you

## Security Features

### Build Security
- Reproducible builds with Go modules
- SBOM generation for every release
- Dependency scanning in CI/CD
- Static analysis with go vet, staticcheck

### Runtime Security
- Non-root container execution
- Seccomp and AppArmor profiles
- Capability dropping (CAP_NET_BIND_SERVICE only)
- Read-only root filesystem
- Privilege escalation disabled

### Network Security
- TLS 1.3 with secure cipher suites
- mTLS support with client certificate verification
- OCSP stapling
- PROXY protocol support

### WAF Security
- 6-layer protection pipeline
- SQLi, XSS, CMDi, Path Traversal, XXE, SSRF detection
- Bot detection with JA3 fingerprinting
- Rate limiting with auto-ban

## Security Hardening Checklist

- [ ] Use non-root user (UID 1000)
- [ ] Enable mTLS for admin API
- [ ] Configure WAF in enforce mode
- [ ] Enable request/response logging
- [ ] Use dedicated certificates
- [ ] Enable OCSP stapling
- [ ] Configure IP ACL whitelist
- [ ] Enable rate limiting
- [ ] Use secrets management for tokens
- [ ] Enable audit logging

## Known Limitations

- GeoIP data is simplified; production use requires MaxMind GeoIP2
- Distributed rate limiting requires Redis for cluster-wide coordination
- Request body logging may capture sensitive data (use data masking)

## CVE Reporting

Security advisories will be published at:
- GitHub Security Advisories
- openloadbalancer.dev/security
- security@openloadbalancer.dev mailing list
