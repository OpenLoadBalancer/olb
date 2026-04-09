# OpenLoadBalancer — Security Architecture Map

## System Overview

High-performance L4/L7 load balancer written in Go. Single binary, zero external dependencies beyond `golang.org/x/{crypto,net,text}`. ~380 Go files across 25 internal packages.

## Tech Stack

- **Language:** Go 1.26+
- **External deps:** `golang.org/x/crypto` (bcrypt/OCSP/Ed25519), `golang.org/x/net` (HTTP/2), `golang.org/x/text` (indirect)
- **Build:** Pure Go, CGO disabled, static binary
- **WebUI:** React 19 + Vite 8, embedded via `//go:embed`
- **CI:** GitHub Actions (gofmt, vet, staticcheck, gosec, nancy, CodeQL)

## Entry Points

| Entry Point | Protocol | File | Notes |
|---|---|---|---|
| HTTP/HTTPS Listeners | HTTP/1.1, HTTP/2, H2C | `internal/engine/engine.go:1288-1459` | Slow-loris hardened timeouts |
| TCP Listeners (L4) | TCP | `internal/proxy/l4/tcp.go:372` | Per-connection goroutines |
| UDP Listeners (L4) | UDP | `internal/engine/engine.go:1372-1405` | Packet proxy |
| Admin API | HTTP | `internal/admin/server.go:310` | Auth optional — WARNED but not enforced |
| MCP SSE Transport | HTTP/SSE | `internal/mcp/sse_transport.go:82` | Bearer token auth |
| pprof Debug | HTTP | `internal/profiling/profiling.go:277` | Localhost binding check (warning only) |
| Gossip Cluster | UDP + TCP | `internal/cluster/gossip.go:352` | HMAC/mTLS optional |
| Raft Consensus | TCP | `internal/engine/engine.go:527` | Leader election + log replication |

## Trust Boundaries

```
┌─────────────────────────────────────────────────────────┐
│                    UNTRUSTED ZONE                        │
│  Client HTTP/HTTPS requests, TCP/UDP connections,       │
│  WebSocket upgrades, gRPC calls, SSE streams             │
│                    PROXY protocol headers                 │
└─────────────┬──────────────────────┬────────────────────┘
              │                      │
     ┌────────▼────────┐    ┌────────▼────────┐
     │  L7 HTTP Proxy  │    │  L4 TCP/UDP     │
     │  + Middleware    │    │  + SNI Routing   │
     │  + WAF (6-layer)│    │  + PROXY Proto   │
     └────────┬────────┘    └────────┬────────┘
              │                      │
     ┌────────▼──────────────────────▼────────┐
     │           BACKEND POOLS                │
     │  Health checks, service discovery,     │
     │  load balancing algorithms             │
     └───────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                  MANAGEMENT ZONE                         │
│  Admin API (REST), WebUI (SPA), MCP Server, pprof       │
│  Config file (YAML/TOML/HCL), CLI args                  │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                  CLUSTER ZONE                            │
│  Gossip (SWIM), Raft consensus, Config state machine    │
└─────────────────────────────────────────────────────────┘
```

## Security Controls

### Strong Controls Present
- Request smuggling validation (`security.ValidateRequest`)
- WAF with 6 detection layers (SQLi, XSS, SSRF, path traversal, CMDI, XXE)
- WebSocket CRLF injection prevention
- Path traversal normalization (multi-level URL decoding)
- Hop-by-hop header stripping in main proxy path
- Constant-time comparisons for all secrets/tokens
- bcrypt for admin auth passwords
- JWT algorithm confusion prevention (algorithm mismatch rejected)
- TLS 1.2 minimum by default, secure cipher suites
- PROXY protocol trusted networks CIDR validation
- Admin API rate limiting (60 req/min per IP, 100K visitor cap)
- Config secret redaction in JSON output (`json:"-"` tags)

### Areas Needing Attention
- Admin API auth enforcement (optional, not required)
- OAuth2 middleware token validation (stub implementation)
- Cluster gossip data race conditions
- Unbounded request body reads in gRPC-Web and shadow traffic
- CORS wildcard + credentials configuration allowed
