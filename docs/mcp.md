# MCP / AI Integration Guide

OLB includes a built-in MCP (Model Context Protocol) server, allowing AI agents and tools like Claude Code to query metrics, manage backends, modify routes, and diagnose issues through natural language.

## What is MCP

The Model Context Protocol (MCP) is an open standard for connecting AI assistants to external tools and data sources. OLB's MCP server exposes load balancer operations as tools that AI agents can call programmatically.

This enables scenarios like:
- "What is the current error rate on the API backend?"
- "Drain backend web-3 and add a new one at 10.0.1.5:8080"
- "Diagnose why latency spiked in the last 10 minutes"
- "Set up a canary deployment sending 5% of traffic to the new version"

## Setup

### Enable the MCP Server

Add the MCP section to `olb.yaml`:

```yaml
mcp:
  enabled: true
  transport: stdio          # "stdio" for CLI tools, "http" for remote access

  # HTTP transport settings (only used when transport is "http")
  http:
    address: "127.0.0.1:9091"

  # Tools to expose (all enabled by default)
  tools:
    - query_metrics
    - list_backends
    - backend_status
    - modify_backend        # Requires write access
    - modify_route          # Requires write access
    - add_backend           # Requires write access
    - remove_backend        # Requires write access
    - drain_backend         # Requires write access
    - get_config
    - set_config            # Requires write access
    - diagnose
    - get_logs
```

### Transport Modes

**stdio** -- For local CLI tools (Claude Code, other MCP clients). OLB communicates via stdin/stdout using JSON-RPC.

```bash
# Start OLB as an MCP server via stdio
olb mcp --config olb.yaml
```

**http** -- For remote AI agents. OLB exposes an HTTP SSE endpoint for MCP communication.

```yaml
mcp:
  transport: http
  http:
    address: "127.0.0.1:9091"
```

## SSE Transport

The SSE (Server-Sent Events) transport implements the MCP specification's streaming protocol, enabling real-time bidirectional communication between AI agents and OLB.

**How it works:**

1. **Client opens SSE stream:** `GET /sse` opens a persistent HTTP connection. The server holds this connection open and pushes events as they occur.
2. **Server sends endpoint event:** Immediately after connection, the server sends an `event: endpoint` message containing the URL for the client to POST commands to (e.g., `/message?sessionId=1`).
3. **Client sends JSON-RPC messages:** The client sends MCP requests as JSON-RPC 2.0 messages via `POST /message?sessionId=N`. Each message includes a method name and parameters.
4. **Server pushes responses via SSE:** Responses to tool calls, resource reads, and other operations are pushed back through the SSE stream as `event: message` events with JSON-RPC response payloads.
5. **Keepalive:** The server sends a keepalive comment (`: keepalive`) every 30 seconds to prevent proxies and load balancers from closing idle connections.

**Example SSE event flow:**

```
← event: endpoint
← data: /message?sessionId=1

→ POST /message?sessionId=1
→ {"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"olb_query_metrics","arguments":{"metric":"requests_per_second"}}}

← event: message
← data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"metric\":\"requests_per_second\",\"value\":1247.5}"}]}}
```

## Authentication

MCP endpoints support Bearer token authentication to secure access to load balancer management tools.

**Configuration:**

```yaml
admin:
  mcp_address: ":8082"
  mcp_token: "your-secret-token"
```

**Behavior:**

- When `mcp_token` is set, all MCP endpoints (`/sse`, `/message`, `/mcp`) require an `Authorization: Bearer <token>` header.
- When `mcp_token` is empty or not set, authentication is disabled (suitable for local development only).
- Token comparison uses constant-time comparison (`crypto/subtle.ConstantTimeCompare`) to prevent timing attacks.
- Authentication failures return HTTP 401 with a JSON-RPC error response.

**Example with curl:**

```bash
# SSE stream with auth
curl -H "Authorization: Bearer your-secret-token" http://localhost:8082/sse

# Tool call with auth
curl -X POST -H "Authorization: Bearer your-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"olb_query_metrics","arguments":{"metric":"requests_per_second"}}}' \
  http://localhost:8082/message?sessionId=1
```

## Audit Logging

MCP audit logging records all tool invocations for security monitoring and compliance.

**Configuration:**

```yaml
admin:
  mcp_audit: true
```

**What is logged:**

- **Tool name:** The MCP tool that was called (e.g., `olb_modify_backend`, `waf_add_blacklist`).
- **Client address:** The remote IP and port of the client making the request.
- **Duration:** How long the tool call took to execute.
- **Timestamp:** When the call was made.

**Custom audit function:**

OLB supports registering a custom audit function for advanced use cases such as forwarding audit events to external SIEM systems, alerting on sensitive operations, or applying additional access control logic. The audit function is called synchronously before the tool result is returned.

**Example log output:**

```
{"level":"info","msg":"mcp tool call","tool":"olb_modify_backend","client":"10.0.1.5:43210","duration_ms":12,"timestamp":"2026-03-15T10:00:00Z"}
```

## Available Tools

### olb_query_metrics

Query load balancer metrics including request rates, latency percentiles, error rates, and connection counts.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `metric` | string | Yes | Metric name: `requests_per_second`, `latency_p99`, `latency_p95`, `latency_p50`, `error_rate`, `active_connections`, `bytes_in`, `bytes_out` |
| `scope` | string | No | `global`, `route`, `backend`, `listener` |
| `target` | string | No | Route name, pool name, or `pool:backend` |
| `range` | string | No | Time range: `5m`, `1h`, `24h` |

**Example interaction:**

```
User: What's the current request rate?

AI calls: olb_query_metrics(metric="requests_per_second", scope="global")

Response: {
  "metric": "requests_per_second",
  "value": 1247.5,
  "scope": "global",
  "timestamp": "2026-03-15T10:00:00Z"
}

AI: The global request rate is currently 1,247 requests per second.
```

### olb_list_backends

List all backend pools and their servers with current status, health, connections, and performance data.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pool` | string | No | Filter by pool name |
| `status` | string | No | Filter: `all`, `healthy`, `unhealthy`, `draining` |

**Example interaction:**

```
User: Show me unhealthy backends.

AI calls: olb_list_backends(status="unhealthy")

Response: {
  "pools": [{
    "name": "web-backend",
    "backends": [{
      "id": "web-3",
      "address": "10.0.1.12:8080",
      "state": "unhealthy",
      "consecutive_failures": 5,
      "last_error": "connection refused",
      "last_check": "2026-03-15T09:59:50Z"
    }]
  }]
}

AI: Backend web-3 (10.0.1.12:8080) in pool web-backend is unhealthy.
    It has failed 5 consecutive health checks with "connection refused".
```

### olb_modify_backend

Add, remove, drain, enable, or disable a backend.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | Yes | `add`, `remove`, `drain`, `enable`, `disable` |
| `pool` | string | Yes | Pool name |
| `address` | string | Yes | Backend address (`host:port`) |
| `weight` | integer | No | Backend weight (for `add` action) |
| `drain_timeout` | string | No | Drain timeout (for `drain` action) |

**Example interaction:**

```
User: Remove the broken backend and add a replacement.

AI calls: olb_modify_backend(action="remove", pool="web-backend", address="10.0.1.12:8080")
AI calls: olb_modify_backend(action="add", pool="web-backend", address="10.0.1.15:8080", weight=1)

AI: Done. Removed 10.0.1.12:8080 and added 10.0.1.15:8080 to pool web-backend.
```

### olb_modify_route

Add, update, or remove routes. Supports traffic splitting for canary deployments.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | Yes | `add`, `update`, `remove` |
| `name` | string | Yes | Route name |
| `match` | object | No | Match criteria (`host`, `path`, `methods`) |
| `backend` | string | No | Target backend pool |
| `split` | array | No | Traffic split for canary deployments |

**Example interaction:**

```
User: Set up a canary sending 10% of traffic to the new version.

AI calls: olb_modify_route(
  action="update",
  name="default",
  split=[
    {"backend": "web-backend", "weight": 90},
    {"backend": "web-canary", "weight": 10}
  ]
)

AI: Updated route "default" to split traffic: 90% to web-backend, 10% to web-canary.
```

### olb_diagnose

Analyze error patterns, detect anomalies, check configuration for problems, and suggest fixes.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | No | `errors`, `latency`, `config`, `health`, `capacity`, `full` |
| `target` | string | No | Route name, pool name, or `all` |
| `range` | string | No | Time range to analyze |

**Example interaction:**

```
User: Why is latency high on the API route?

AI calls: olb_diagnose(type="latency", target="api", range="30m")

Response: {
  "diagnosis": {
    "type": "latency",
    "target": "api",
    "findings": [
      {
        "severity": "high",
        "description": "Backend api-2 (10.0.2.11:8080) p99 latency is 450ms vs pool average of 25ms",
        "evidence": {
          "api-1_p99": 22,
          "api-2_p99": 450
        },
        "suggestion": "Backend api-2 may be overloaded. Consider draining it or checking its health."
      },
      {
        "severity": "medium",
        "description": "Connection pool utilization at 85% for api-2",
        "suggestion": "Increase max_per_host or add more backends."
      }
    ]
  }
}

AI: The latency spike is caused by backend api-2 (10.0.2.11:8080), which has a p99
    latency of 450ms compared to the pool average of 25ms. Its connection pool is also
    at 85% utilization. I recommend draining api-2 to investigate, or adding another
    backend to reduce load.
```

### olb_get_config

Get the current running configuration.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `section` | string | No | `global`, `listeners`, `backends`, `routes`, `cluster`, `all` |

### olb_get_logs

Search and retrieve access logs and error logs.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | No | `access`, `error`, `all` |
| `filter` | string | No | Search query |
| `limit` | integer | No | Max entries to return |
| `since` | string | No | Start time |
| `level` | string | No | `trace`, `debug`, `info`, `warn`, `error` |

### olb_cluster_status

Get cluster status including node list, Raft state, leader info, and replication lag. No parameters required.

## Available Resources

MCP resources provide read-only access to OLB state:

| URI | Name | Description |
|-----|------|-------------|
| `olb://metrics/dashboard` | Live Dashboard Metrics | Real-time metrics for a dashboard view |
| `olb://config/current` | Current Configuration | Full running config in YAML |
| `olb://health/summary` | Health Summary | All backend health status |
| `olb://logs/recent` | Recent Logs | Last 100 log entries |

## Prompt Templates

Pre-built prompt templates for common operations:

### diagnose_high_latency

Investigate high latency on a route or backend.

```
Arguments: target (required) -- route name or backend pool
```

Gathers metrics, health status, connection pool stats, and recent errors to produce a diagnosis.

### capacity_planning

Analyze current capacity and recommend scaling decisions.

```
Arguments: pool (required) -- backend pool name
```

Reviews request rates, connection utilization, response times, and trends to suggest scaling actions.

### deploy_canary

Set up a canary deployment with traffic splitting.

```
Arguments:
  route (required) -- route name
  new_backend (required) -- new backend pool for canary
  percentage (required) -- percentage of traffic to canary (e.g., "5")
```

Configures traffic splitting on the specified route.

## Using with Claude Code

### Configuration

Add OLB to your Claude Code MCP server config (`.claude/mcp.json`):

```json
{
  "mcpServers": {
    "olb": {
      "command": "olb",
      "args": ["mcp", "--config", "/etc/olb/olb.yaml"]
    }
  }
}
```

### Usage

Once configured, interact with OLB naturally:

```
You: What's the health of our backend pools?

Claude: Let me check. [calls olb_list_backends]

All pools are healthy:
- web-backend: 3/3 backends healthy, 1,247 RPS total
- api-backend: 2/2 backends healthy, 523 RPS total

You: The api-2 backend seems slow. Can you investigate?

Claude: [calls olb_diagnose(type="latency", target="api-backend")]

Backend api-2 has elevated latency (p99 = 180ms vs api-1's 12ms). The connection
pool is at 78% capacity. Recommendations:
1. Drain api-2 for investigation
2. Add a new backend to maintain capacity

Should I drain api-2?

You: Yes, drain it and add 10.0.2.13:8080 as a replacement.

Claude: [calls olb_modify_backend(action="drain", pool="api-backend", address="10.0.2.11:8080")]
        [calls olb_modify_backend(action="add", pool="api-backend", address="10.0.2.13:8080")]

Done. Backend api-2 is draining (14 active connections finishing up) and
10.0.2.13:8080 has been added to the api-backend pool.
```

## Example: Automated Diagnostics

A scheduled AI agent can monitor OLB and take action:

```python
# Pseudocode for automated OLB monitoring via MCP
while True:
    # Check error rate
    result = mcp.call("olb_query_metrics", metric="error_rate", scope="global")
    if result["value"] > 0.05:  # 5% error rate threshold
        # Diagnose the issue
        diagnosis = mcp.call("olb_diagnose", type="errors", range="5m")

        for finding in diagnosis["findings"]:
            if finding["severity"] == "critical":
                # Auto-drain unhealthy backends
                mcp.call("olb_modify_backend",
                    action="drain",
                    pool=finding["pool"],
                    address=finding["backend"]
                )
                alert(f"Auto-drained {finding['backend']}: {finding['description']}")

    # Check capacity
    result = mcp.call("olb_diagnose", type="capacity")
    for finding in result["findings"]:
        if "utilization" in finding and finding["utilization"] > 80:
            alert(f"High utilization on {finding['pool']}: {finding['utilization']}%")

    sleep(60)
```

## Security Considerations

- MCP tools that modify state (`modify_backend`, `modify_route`, `set_config`) should be restricted in production environments. Use the `tools` config to expose only read-only tools when appropriate.
- The stdio transport is inherently local (runs as a subprocess). The HTTP transport should be bound to localhost or secured with authentication.
- All MCP actions are logged in the OLB audit log.
