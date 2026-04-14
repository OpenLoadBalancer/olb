# Dependency Audit — OpenLoadBalancer (2026-04-14 Rescan)

## Status: CLEAN

### Direct Dependencies

| Dependency | Version | Purpose | Known CVEs |
|-----------|---------|---------|------------|
| `golang.org/x/crypto` | v0.49.0 | bcrypt, ed25519, OCSP stapling | None |
| `golang.org/x/net` | v0.52.0 | HTTP/2, HPACK, advanced networking | None |
| `golang.org/x/text` | v0.35.0 | Text processing (indirect/transitive) | None |

### Supply Chain Assessment

- **No external HTTP clients for dependency fetching** — all deps are Go standard library extensions
- **No CGO dependencies** — pure Go codebase
- **No pre/post install scripts** — standard Go module system
- **No obfuscated or minified code** — all source is readable Go
- **Go module checksum database** provides integrity verification via `go.sum`
- **3 total external dependencies** — minimal attack surface

### Internal "Dependencies" (Self-Contained)

| Package | Purpose | Risk Assessment |
|---------|---------|-----------------|
| `internal/waf/` | Web Application Firewall | Self-contained detection engine, RE2-safe regex |
| `internal/acme/` | Let's Encrypt client | Uses standard ACME protocol, crypto/rand for keys |
| `internal/cluster/` | Raft + SWIM consensus | Custom implementation, **unauthenticated transport** (H-1) |
| `internal/plugin/` | Go plugin system (.so loading) | Inherent trust boundary, mitigated by allowlist |
| `internal/config/` | Custom YAML/TOML/HCL/JSON parsers | Custom parsers reduce supply-chain risk but carry parser-bug risk |

### Custom Parser Risk Assessment

The codebase implements custom parsers for YAML, TOML, HCL, and JSON-RPC instead of using third-party libraries. This is a deliberate tradeoff:

**Benefits:**
- Eliminates entire supply-chain attack surface
- No dependency version churn
- Full control over parsing behavior

**Risks:**
- Custom parsers may not handle all edge cases that mature libraries do
- Potential for parser differentials (differing interpretation of the same input)
- No community fuzz-testing of custom parser implementations

**Mitigation:** Config files are admin-controlled, not user-supplied input.

### Plugin System Risk

The plugin system uses Go's `plugin.Open()` to load `.so` shared objects at runtime:
- Plugins run with full process privileges
- No sandboxing or capability restriction
- Mitigated by `AllowedPlugins` allowlist (defaults to empty = no loading)
- **Finding L-12:** Plugin paths not resolved to absolute paths

**Recommendation:** Document plugin security model. Consider code signing verification for plugin `.so` files.

---

*Updated by security-check on 2026-04-14*
