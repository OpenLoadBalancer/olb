# Dependency Audit — OpenLoadBalancer

## Summary

| Metric | Value |
|---|---|
| Direct Go dependencies | 2 |
| Indirect Go dependencies | 1 |
| Total external Go modules | 3 |
| Third-party GitHub deps | 0 |
| CGO usage | Disabled (CGO_ENABLED=0) |
| Supply chain risk | **Very Low** |

## Direct Dependencies

### golang.org/x/crypto v0.49.0
- **Used in:**
  - `internal/admin/auth.go` — bcrypt password hashing for admin API
  - `internal/tls/ocsp.go` — OCSP stapling for TLS certificates
  - `internal/middleware/jwt/jwt.go` — Ed25519 JWT signature verification
- **Risk:** Low. Maintained by Go team. Well-audited.

### golang.org/x/net v0.52.0
- **Used in:**
  - `internal/proxy/l7/http2.go` — HTTP/2 and H2C proxy support
- **Risk:** Low. Maintained by Go team. Standard HTTP/2 implementation.

## Indirect Dependencies

### golang.org/x/text v0.35.0
- **Pulled by:** `golang.org/x/net`
- **Not directly imported** in any application source file
- **Risk:** Minimal.

## Build-Time Dependencies (Not in Binary)

### WebUI (internal/webui/)
- React 19, React Router 7, Tailwind CSS 4, Radix UI, Vite 8, TypeScript
- Compiled to static assets, embedded via `//go:embed`
- Not exposed to runtime network traffic
- **Risk:** Build-time only. Managed by Dependabot.

## Supply Chain Protections

| Protection | Status | Details |
|---|---|---|
| Dependabot | Enabled | Monitors gomod, npm, github-actions (weekly) |
| CodeQL | Enabled | Weekly + on push to main |
| Gosec | Enabled | Runs in CI pipeline |
| Nancy | Enabled | Dependency vulnerability scanning in CI |
| SBOM | Generated | SPDX format via anchore/sbom-action in release |
| SHA256 checksums | Generated | For all release binaries |
| Docker signing | N/A | No cosign/notation configured |

## Recommendations

1. **No action needed** on dependency footprint — it is already minimal.
2. Consider adding `govulncheck` to CI for real-time vulnerability scanning of standard library.
3. Consider pinning Docker base image digests in Dockerfile (currently uses `alpine:3.20` tag).
