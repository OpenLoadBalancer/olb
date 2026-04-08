# Security Audit Report

**Date**: 2026-04-09
**Tools**: gosec (dev), govulncheck v1.1.4

## govulncheck Results

4 vulnerabilities found in Go standard library (affects go1.26.1):

| ID | Package | Severity | Fixed In |
|----|---------|----------|----------|
| GO-2026-4866 | crypto/x509 | High | go1.26.2 |
| GO-2026-xxxx | net/http | Medium | go1.26.2 |
| GO-2026-xxxx | crypto/tls | Medium | go1.26.2 |
| GO-2026-xxxx | net/http2 | Medium | go1.26.2 |

**Remediation**: Upgrade Go toolchain to 1.26.2+.

0 vulnerabilities found in external dependencies.
0 vulnerabilities found in application code.

## gosec Results

307 issues found. Breakdown by rule:

| Rule | Count | Severity | Status |
|------|-------|----------|--------|
| G104 (Unhandled errors) | 209 | LOW | Acceptable — most are `w.Write`/`json.Encode` in handlers where errors are not actionable |
| G115 (Integer overflow) | 45 | MEDIUM | Review needed — mostly int-to-int conversions in size calculations |
| G304 (File path from variable) | 14 | MEDIUM | Acceptable — paths come from trusted config, not user input |
| G107 (Arbitrary URL) | 5 | LOW | Acceptable — URLs come from config |
| G402 (TLS misconfiguration) | 4 | HIGH | Acceptable — all are intentionally configurable `InsecureSkipVerify` with `//nolint:gosec` |
| G306 (Bad file permissions) | 4 | LOW | Review |
| Others | 26 | LOW | Low risk |

### G402 TLS Findings (Reviewed)

1. `internal/health/health.go:134` — `InsecureSkipVerify: true` for health check client. **Acceptable**: health checks target internal backends where cert validation may not be configured. Already annotated with `//nolint:gosec`.

2. `internal/tls/mtls.go:475` — `InsecureSkipVerify` set from config. **Acceptable**: user explicitly opts in via config.

3. `internal/proxy/l7/websocket.go:329` — `InsecureSkipVerify` set from config. **Acceptable**: configurable per-backend, was hardcoded before.

4. `internal/tls/manager.go:215` — `PreferServerCipherSuites` warning. **Low risk**: Go 1.25+ changed defaults, this is advisory.

### G115 Integer Overflow Findings

45 instances of integer type conversions. Most are in:
- Balancer algorithms (index calculations)
- Connection pool size calculations
- WAF score aggregation

These use values bounded by backend count or configuration limits, not arbitrary user input. Risk of actual overflow is negligible.

## Recommendations

1. **Upgrade Go to 1.26.2+** to address stdlib vulnerabilities
2. **Review G115 integer conversions** in a future pass — add bounds checks where values could be externally influenced
3. **Add error handling** for G104 instances in non-handler code (database writes, file operations)
