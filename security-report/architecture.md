# Architecture Security Map — OpenLoadBalancer (2026-04-25)

**Scan Date:** 2026-04-25
**Previous Scan:** 2026-04-18
**Codebase:** ~380 Go source files + React 19 WebUI

---

## 1. Technology Stack

| Attribute | Value |
|---|---|
| Primary Language | Go 1.26.2 |
| Go Source Files (non-test) | 244 |
| Go Test Files | 206 |
| Frontend Framework | React 19.2 + TypeScript + Vite 8 (internal/webui) |
| Build Tools | Make, GoReleaser, Docker |
| Go Dependencies | 3: `golang.org/x/crypto v0.50.0`, `golang.org/x/net v0.53.0`, `golang.org/x/text v0.36.0` |
| Frontend Dependencies | ~20 runtime (React, Radix UI, Tailwind, Zustand, Zod), ~20 dev (Vitest, Playwright, ESLint) |
| External Go Frameworks | None — pure stdlib net/http |

---

## 2. Entry Points

| Entry Point | File | Protocol | Auth Required | Notes |
|---|---|---|---|---|
| HTTP Listeners | `engine/listeners.go:18-44` | HTTP | Via middleware chain | Dynamically configured |
| HTTPS Listeners | `listener/https.go:39-59` | HTTPS (TLS) | Via middleware chain | TLS from tls.Manager; mTLS optional |
| TCP Listeners | `engine/listeners.go:57-96` | TCP | None | Raw passthrough, connection limits (10K) |
| UDP Listeners | `engine/listeners.go:99-132` | UDP | None | Raw datagrams, session limits (10K) |
| SNI Proxy | `proxy/l4/sni.go:442-571` | TCP/TLS | None | Routes by SNI without TLS termination |
| Admin API | `admin/server.go:246-351` | HTTP(S) | Basic/Bearer | Rate limited 60/min; localhost-only without auth |
| Admin SSE Stream | `admin/events.go:82-141` | SSE | Same as Admin API | Max 100 subscribers |
| MCP SSE Transport | `mcp/sse_transport.go:95-117` | HTTP/SSE | Bearer token (required) | 3 endpoints; max 100 clients |
| MCP HTTP Transport | `mcp/mcp.go:1333-1350` | HTTP | Bearer token (required) | POST /mcp |
| MCP Stdio Transport | `mcp/mcp.go:1235-1267` | stdin/stdout | None (local only) | Line-delimited JSON-RPC |
| pprof Server | `profiling/profiling.go:248-311` | HTTP | None | localhost:6060 default; warns if non-localhost |
| Cluster Raft TCP | `cluster/cluster.go:~100-121` | TCP | Optional HMAC | Configurable via cluster.node_auth |
| Cluster Gossip | `cluster/gossip.go:61-68` | UDP+TCP | Optional | Memberlist-style gossip |

---

## 3. Trust Boundaries

```
Internet → [L4/L7 Listeners] → [WAF Engine] → [Middleware Chain] → [Router] → [Pool] → [Backend]
                                       ↓
                                [Admin API] ← [Operator/MCP/AI]
                                       ↓
                                [Config File] ← [Filesystem]
                                       ↓
                                [Cluster] ← [Peer Nodes]
```

| Input Source | Entry File | Risk | Validation |
|---|---|---|---|
| HTTP requests | `listener/http.go:86` | HIGH | Full middleware chain + WAF |
| TCP connections | `engine/listeners.go:57-96` | MEDIUM | Connection limits only |
| UDP datagrams | `engine/listeners.go:99-132` | MEDIUM | Session limits + buffer size |
| SNI ClientHello | `proxy/l4/sni.go:133-177` | MEDIUM | Size limit (16KB), hostname validation, timeout (5s) |
| Config files | `config/config.go:1-1108` | HIGH | Config.Validate() at load; env var expansion |
| Environment variables | `config/config.go:824-837` | MEDIUM | Secrets sourced from env vars |
| Admin API requests | `admin/server.go:246-351` | HIGH | Auth + rate limit + CSRF + security headers |
| MCP JSON-RPC messages | `mcp/mcp.go:457-494` | HIGH | Bearer auth, 1MB body limit, error sanitization |
| Docker API | `discovery/docker.go` | MEDIUM | Container enumeration for service discovery |
| MaxMind DB file | `geodns/mmdb.go` | LOW | Local file read for GeoIP |

---

## 4. Authentication Architecture

| Mechanism | File | Enforced | Details |
|---|---|---|---|
| Basic Auth (Admin) | `admin/auth.go:160-240` | YES | bcrypt, constant-time compare, IP lockout (5/5min) |
| Bearer Token (Admin) | `admin/auth.go:198-207` | YES | Constant-time compare; multiple tokens |
| Bearer Token (MCP) | `mcp/sse_transport.go:147-157` | YES | Constructor rejects empty token |
| JWT (L7 Middleware) | `middleware/jwt/jwt.go:71+` | Configurable | HS256/384/512, EdDSA; exp/iss/aud validation |
| HMAC (L7 Middleware) | `middleware/hmac/hmac.go:57+` | Configurable | SHA-256/512; optional timestamp replay protection |
| OAuth2/OIDC (L7) | `middleware/oauth2/oauth2.go:25+` | Configurable | JWKS caching, token introspection |
| API Key (L7 Middleware) | `middleware/apikey/apikey.go:52+` | Configurable | SHA-256 or plain; multiple keys |
| mTLS (Listeners) | `config/config.go:669-673` | Configurable | Client CA verification per listener |
| CSRF (Admin) | `middleware/csrf/csrf.go:85-131` | YES (with WebUI) | Double-submit cookie; SameSite=Strict; 32-byte tokens |
| HMAC Node Auth (Cluster) | `cluster/security.go:206-336` | Configurable | HMAC token-based inter-node auth |
| Admin No-Auth Guard | `admin/server.go:210-217` | YES | Refuses non-localhost without auth |

---

## 5. Data Flow Map

```
External Client
     │
     ▼
[HTTP/HTTPS/TCP/UDP/SNI Listener]
     │
     ▼
[Connection Manager] (limits: 10K total, 100/src, 1K/backend)
     │
     ▼
[WAF Engine] (optional) — SQLi/XSS/PathTraversal/CMDi/XXE/SSRF + IP ACL + rate limiting + bot detection
     │
     ▼
[Middleware Chain]
  Recovery → RealIP → RequestID → Logging → Metrics → Rate Limit → IP Filter →
  JWT/OAuth2/HMAC/BasicAuth/APIKey → CSRF → CORS → CSP → SecureHeaders →
  ForceSSL → Compression → Cache → Coalesce → Circuit Breaker → Body Limit →
  Validator → Transformer → Rewrite → Trace
     │
     ▼
[Router] (host/path matching, radix trie)
     │
     ▼
[Pool Manager] → [Balancer] (16 algorithms) → [Backend Server]
     │
     ▼
[Shadow Manager] (parallel, strips credentials)
```

**Config Flow:** File Watcher (poll SHA-256) → Debounce (2 consecutive matches) → Reload → Validate → Apply → Rollback Grace Period (30s)

---

## 6. Security Controls

| Control | File | Details |
|---|---|---|
| WAF Engine | `waf/waf.go` | SQLi, XSS, path traversal, CMDi, XXE, SSRF; scoring + configurable thresholds |
| Request Smuggling | `security/security.go:130-239` | CL/TE conflict detection; duplicate CL; malformed CL |
| Path Traversal | `security/security.go:411-461` | Multi-pass percent-decode; path.Clean; leading slash enforcement |
| Header Injection | `security/security.go:247-294` | CR/LF/NUL stripping; RFC 7230 header name validation |
| Host Header Validation | `security/security.go:300-378` | Whitespace/control char rejection; hostname char validation |
| TLS Hardening | `security/security.go:31-54` | Min TLS 1.2; AEAD ciphers only; X25519/P-256/P-384 |
| Slow Loris Protection | `security/security.go:73-124` | ReadHeaderTimeout 10s, ReadTimeout 30s, MaxHeaderBytes 1MB |
| Shadow Credential Stripping | `proxy/l7/shadow.go:57-63` | Removes Authorization, Cookie, Set-Cookie, Proxy-Authorization, X-Api-Key |
| MCP Error Sanitization | `mcp/mcp.go:542-569` | Internal errors mapped to generic messages |
| SNI Hostname Validation | `proxy/l4/sni.go:424-438` | Length + character validation (alphanumeric, hyphen, dot) |
| Config Rollback | `engine/config.go` | Auto-rollback if errors spike within 30s of reload |
| CORS (Admin) | `admin/server.go:600-636` | Origin allowlist; no wildcard+credentials; Vary: Origin |
| CORS (MCP SSE) | `mcp/sse_transport.go:369-390` | Origin allowlist from config; same-origin default |

---

## 7. External Integrations

| Integration | File | Protocol | Auth | Risk |
|---|---|---|---|---|
| Let's Encrypt (ACME) | `acme/acme.go` | HTTPS outbound | ECDSA JWS signed requests | LOW |
| Docker Daemon | `discovery/docker.go` | Unix socket / TCP | Optional TLS | MEDIUM |
| Consul | `discovery/consul.go` | HTTP(S) | Consul token | MEDIUM |
| DNS | `discovery/dns.go` | UDP/TCP | None | LOW |
| MaxMind GeoIP DB | `geodns/mmdb.go` | File read | N/A | LOW |
| Prometheus Scrapers | `admin/server.go:274` | HTTP GET | Via admin auth | LOW |

---

## 8. Modified Files Since Last Scan (2026-04-18)

All unstaged modifications reviewed. Key attack surface changes:

| File | Security Impact |
|---|---|
| `acme/acme.go` | Outbound HTTPS to Let's Encrypt; 1MB response cap |
| `admin/auth.go` | Per-IP lockout (5 failures/5min); 100K entry cap |
| `admin/handlers_system.go` | Health probes may be unauthenticated when RequireAuthForRead=false |
| `admin/server.go` | CSRF auto-enabled with WebUI; CORS origin allowlist; localhost enforcement |
| `config/watcher.go` | SHA-256 hash polling with 2-check debounce |
| `engine/config.go` | Config rollback grace period (30s); error spike detection |
| `geodns/geodns.go` | Trusted proxy CIDR parsing |
| `geodns/mmdb.go` | GeoIP lookups from MMDB file |
| `health/health.go` | gRPC TLS skip verify option (configurable) |
| `mcp/sse_transport.go` | Bearer token required; 1MB body limit; error sanitization |
| `middleware/jwt/jwt.go` | HS256/384/512, EdDSA; exp/iss/aud validation |
| `proxy/l4/sni.go` | SNI hostname validation (alphanumeric/hyphen/dot) |
| `proxy/l7/shadow.go` | Strips sensitive headers; 1MB body cap |
| `tls/manager.go` | Certificate expiry monitoring with alert callbacks |

---

## 9. Key Observations

1. **Minimal supply chain surface**: Only 3 Go dependencies (x/crypto, x/net, x/text)
2. **Admin auth enforcement is strong**: Refuses non-localhost without auth; IP lockout with cap
3. **MCP endpoints require auth**: Constructors reject empty bearer tokens
4. **pprof defaults to localhost**: No auth but localhost-only
5. **L4 proxies have no app-layer auth**: By design; connection limits provide basic protection
6. **Cluster inter-node auth is optional**: Requires explicit configuration
7. **Config watcher uses SHA-256 polling**: 2-consecutive-check debounce prevents reload storms
8. **Shadow traffic strips credentials**: Explicit header denylist
9. **Config rollback mechanism**: Auto-rollback on error spike within 30s
10. **Zero secrets in JSON output**: All secret fields use `json:"-"` tags

*Architecture map generated 2026-04-25 by security-check pipeline*
