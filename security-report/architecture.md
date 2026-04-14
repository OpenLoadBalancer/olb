# Architecture Security Map - OpenLoadBalancer

## Entry Points

| Component | Protocol | Auth Required | File |
|-----------|----------|---------------|------|
| HTTP/HTTPS Listener | HTTP/HTTPS | Optional (middleware-gated) | internal/listener/http.go |
| TCP/UDP L4 Proxy | TCP/UDP | None (passthrough) | internal/proxy/l4/tcp.go |
| SNI Router | TLS/TCP | TLS-based | internal/proxy/l4/sni.go |
| Admin API | HTTP | Basic/Bearer (localhost-only if no auth) | internal/admin/server.go |
| WebUI | HTTP (SPA) | Proxied to Admin | internal/webui/webui.go |
| MCP Server (HTTP) | HTTP POST | Bearer Token (required) | internal/mcp/mcp.go |
| MCP Server (SSE) | HTTP SSE | Bearer Token (if set) | internal/mcp/sse_transport.go |
| MCP Server (Stdio) | Stdin/Stdout | None (local) | internal/mcp/mcp.go |
| Raft Transport | TCP | NONE (unauthenticated) | internal/cluster/transport.go |
| Gossip Transport | UDP | Shared secret | internal/cluster/gossip_transport.go |
| Profiling (pprof) | HTTP | None (localhost warning) | internal/profiling/profiling.go |

## Trust Boundaries

```
[Internet] → [Listeners] → [WAF] → [IP Filter] → [Auth MW] → [Router] → [Proxy] → [Backend]
[Admin User] → [Admin API (auth)] → [Backend/Route/Config State]
[MCP Client] → [MCP HTTP (bearer)] → [Backend/Route/Config State]
[Cluster Peer] → [Raft TCP (UNAUTHENTICATED ⚠)] → [State Replication]
[Cluster Peer] → [Gossip UDP (shared secret)] → [Membership]
[Config File] → [Config Loader (env var expansion)] → [Engine]
```

## Security Layers

```
Layer 1: Network    — Listeners (connection limits, timeouts)
Layer 2: Transport  — TLS 1.2+, ECDHE ciphers, mTLS option
Layer 3: Request    — WAF (6-layer detection pipeline)
Layer 4: Validation — Request smuggling, path traversal, header injection
Layer 5: Auth       — Basic, Bearer, JWT, OAuth2/OIDC, HMAC, API Key, CSRF
Layer 6: Proxy      — Hop-by-hop stripping, XFF trust model, body limits
Layer 7: Backend    — Health checking, circuit breaking, passive monitoring
```

## Dependencies

| Dependency | Version | Usage | Vulnerabilities |
|-----------|---------|-------|----------------|
| golang.org/x/crypto | v0.49.0 | bcrypt, ed25519, OCSP | None known |
| golang.org/x/net | v0.52.0 | HTTP/2, HPACK | None known |
| golang.org/x/text | v0.35.0 | Indirect (transitive) | None known |

Go: 1.26+ | CGO: Disabled | External deps: 3 | Supply chain risk: Minimal

## Key Security Files

| Area | File |
|------|------|
| Request validation | internal/security/security.go |
| TLS management | internal/tls/manager.go |
| Admin auth | internal/admin/auth.go |
| WAF engine | internal/waf/waf.go |
| JWT middleware | internal/middleware/jwt/jwt.go |
| OAuth2 middleware | internal/middleware/oauth2/oauth2.go |
| HMAC middleware | internal/middleware/hmac/hmac.go |
| CSRF protection | internal/middleware/csrf/csrf.go |
| Cluster security | internal/cluster/security.go |
| Request smuggling | internal/security/security.go (lines 130-239) |
| Path traversal | internal/security/security.go (lines 411-461) |
| WebSocket security | internal/proxy/l7/websocket.go |

---

*Updated by security-check on 2026-04-14*
