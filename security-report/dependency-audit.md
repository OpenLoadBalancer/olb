# Dependency Audit — OpenLoadBalancer (2026-04-25)

**Date:** 2026-04-25
**Project:** github.com/openloadbalancer/olb
**Go Version:** 1.26.2

---

## 1. Go Module Dependencies

| Dependency | Version | Type | Allowed by Policy? |
|---|---|---|---|
| `golang.org/x/crypto` | v0.50.0 | Direct | YES |
| `golang.org/x/net` | v0.53.0 | Direct | YES |
| `golang.org/x/text` | v0.36.0 | Indirect | YES |

**Policy compliance: PASS.** No external third-party packages. Zero `replace` directives.

### Usage Map

| Package | Sub-package | Used In |
|---|---|---|
| `golang.org/x/crypto` | `bcrypt` | `admin/auth.go`, `middleware/basic/basic.go` |
| `golang.org/x/crypto` | `ocsp` | `tls/ocsp.go` |
| `golang.org/x/crypto` | `ed25519` | `middleware/jwt/jwt.go` |
| `golang.org/x/net` | `http2` | `proxy/l7/http2.go` |
| `golang.org/x/net` | `http2/h2c` | `proxy/l7/http2.go` |
| `golang.org/x/text` | (indirect) | Transitive dep of x/net; not directly imported |

---

## 2. CVE Assessment — Go Dependencies

| Dependency | Version | Known CVEs | Status |
|---|---|---|---|
| `golang.org/x/crypto` | v0.50.0 | CVE-2024-45337/45336 fixed in v0.35.0 | CLEAN |
| `golang.org/x/net` | v0.53.0 | CVE-2023-45288 (HTTP/2 CONTINUATION) fixed in v0.23.0 | CLEAN |
| `golang.org/x/text` | v0.36.0 | No known CVEs | CLEAN |

---

## 3. WebUI Dependencies (internal/webui)

### Runtime Dependencies

| Package | Version | License | Risk |
|---|---|---|---|
| `react` / `react-dom` | 19.2.5 | MIT | LOW — Meta, heavily audited |
| `react-router` | 7.14.0 | MIT | LOW — Remix Software |
| `radix-ui` components | Various | MIT | LOW — WorkOS/commercial |
| `zustand` | 5.0.12 | MIT | LOW-MEDIUM — small org |
| `zod` | 3.25.76 | MIT | MEDIUM — single maintainer |
| `cmdk` | 1.1.1 | MIT | MEDIUM — single maintainer |
| `sonner` | 1.7.4 | MIT | MEDIUM — single maintainer |
| `tailwindcss` | 4.2.2 | MIT | LOW — Tailwind Labs |
| `vite` | 8.0.8 | MIT | LOW — large community |

### Dev Dependencies (20 packages)

All MIT/Apache-2.0 licensed. Includes: Vitest, Playwright, ESLint, TypeScript, Testing Library, Prettier.

---

## 4. Website Dependencies (website-new)

6 runtime deps (React, Lucide, Tailwind), 12 dev deps. All MIT licensed.

---

## 5. Hidden Dependency Scan

| Check | Result |
|---|---|
| External `github.com/` imports | NONE — only internal `github.com/openloadbalancer/olb/...` |
| `go.uber.org/`, `google.golang.org/`, `k8s.io/`, `gopkg.in/` | NONE |
| CGo files | NONE |
| `vendor/` directory | NONE |
| Third-party code in source | NONE |
| Alternative npm registries | NONE |

**Result: No hidden or undeclared dependencies.**

---

## 6. Supply Chain Assessment

| Category | Risk | Notes |
|---|---|---|
| Go dep policy compliance | PASS | All 3 deps on allowed list |
| Go CVE status | CLEAN | All versions past known fixes |
| Hidden dependencies | NONE | Full scan completed |
| Dependency pinning | MEDIUM | npm uses caret ranges (lockfiles provide reproducibility) |
| Single-maintainer packages | MEDIUM | `zod`, `cmdk`, `sonner` individually maintained |
| Dependency confusion | LOW | No private registry vectors |
| **Dependabot coverage gap** | **MEDIUM** | `website-new/` NOT monitored by Dependabot |

---

## 7. Recommendations

1. **Add `website-new/` to Dependabot config** — only `/internal/webui` is currently monitored for npm
2. **Run `govulncheck ./...`** to verify against latest Go vulnerability database
3. **Run `npm audit`** in both `internal/webui/` and `website-new/`
4. **Consider exact version pinning** in npm `package.json` to prevent drift
5. **Monitor single-maintainer packages** for ownership changes

*Dependency audit generated 2026-04-25 by security-check pipeline*
