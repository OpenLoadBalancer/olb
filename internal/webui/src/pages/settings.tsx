import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Settings, Server, Globe, Shield, RotateCcw, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { useConfig } from "@/hooks/use-query"
import { api } from "@/lib/api"
import { LoadingCard } from "@/components/ui/loading"

interface ListenerConfig {
  name: string
  address: string
  protocol?: string
  routes?: Array<{ path: string }>
}

interface PoolConfig {
  name: string
  algorithm: string
  backends?: Array<Record<string, unknown>>
  health_check?: { type: string; path: string; interval: string }
}

import { type OLBConfig } from "@/types"

export function SettingsPage() {
  useDocumentTitle("Settings")
  const { data: config, isLoading, error, refetch } = useConfig()
  const c = (config?.data ?? config) as OLBConfig | undefined

  const handleReload = async () => {
    try {
      await api.reload()
      toast.success("Configuration reloaded from disk")
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to reload configuration"
      toast.error(message)
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
          <p className="text-muted-foreground">View current configuration</p>
        </div>
        <LoadingCard />
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
          <p className="text-muted-foreground">View current configuration</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-destructive">Failed to load configuration: {error.message}</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4"  aria-hidden="true" /> Retry
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground">View current configuration</p>
      </div>

      <Alert>
        <AlertDescription>
          Settings are read from the configuration file. Edit the config file and click "Reload" to apply changes.
        </AlertDescription>
      </Alert>

      <Tabs defaultValue="general" className="space-y-4">
        <TabsList>
          <TabsTrigger value="general" className="flex items-center gap-2">
            <Settings className="h-4 w-4"  aria-hidden="true" />
            General
          </TabsTrigger>
          <TabsTrigger value="admin" className="flex items-center gap-2">
            <Server className="h-4 w-4"  aria-hidden="true" />
            Admin
          </TabsTrigger>
          <TabsTrigger value="network" className="flex items-center gap-2">
            <Globe className="h-4 w-4"  aria-hidden="true" />
            Network
          </TabsTrigger>
          <TabsTrigger value="security" className="flex items-center gap-2">
            <Shield className="h-4 w-4"  aria-hidden="true" />
            Security
          </TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Logging</CardTitle>
              <CardDescription>Log output configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Log Level" value={c?.logging?.level || 'info'} />
              <ConfigRow label="Log Format" value={c?.logging?.format || 'json'} />
              <ConfigRow label="Log Output" value={c?.logging?.output || 'stdout'} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Server</CardTitle>
              <CardDescription>Connection and timeout settings</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Max Connections" value={c?.server?.max_connections ?? 10000} />
              <ConfigRow label="Max Connections Per Source" value={c?.server?.max_connections_per_source ?? 100} />
              <ConfigRow label="Max Connections Per Backend" value={c?.server?.max_connections_per_backend ?? 1000} />
              <ConfigRow label="Proxy Timeout" value={c?.server?.proxy_timeout || '60s'} />
              <ConfigRow label="Dial Timeout" value={c?.server?.dial_timeout || '10s'} />
              <ConfigRow label="Max Retries" value={c?.server?.max_retries ?? 3} />
              <ConfigRow label="Max Idle Connections" value={c?.server?.max_idle_conns ?? 100} />
              <ConfigRow label="Max Idle Conns Per Host" value={c?.server?.max_idle_conns_per_host ?? 10} />
              <ConfigRow label="Drain Timeout" value={c?.server?.drain_timeout || '30s'} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="admin" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Admin API</CardTitle>
              <CardDescription>Admin server configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Listen Address" value={c?.admin?.address || ':9090'} />
              <ConfigRow label="Rate Limit Max Requests" value={c?.admin?.rate_limit_max_requests ?? 'default'} />
              <ConfigRow label="Rate Limit Window" value={c?.admin?.rate_limit_window || '1m'} />
              <ConfigRow label="MCP Audit Logging" value={c?.admin?.mcp_audit ? 'Enabled' : 'Disabled'} />
              <ConfigRow label="MCP Address" value={c?.admin?.mcp_address || '(auto)'} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Cluster</CardTitle>
              <CardDescription>Clustering configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Enabled" value={c?.cluster?.enabled ? 'Yes' : 'No'} />
              {c?.cluster?.enabled && (
                <>
                  <ConfigRow label="Node ID" value={c?.cluster?.node_id || '-'} />
                  <ConfigRow label="Bind Address" value={`${c?.cluster?.bind_addr || '0.0.0.0'}:${c?.cluster?.bind_port || 7946}`} />
                  <ConfigRow label="Peers" value={c?.cluster?.peers?.length ?? 0} />
                  <ConfigRow label="Data Directory" value={c?.cluster?.data_dir || '(none)'} />
                  <ConfigRow label="Election Tick" value={c?.cluster?.election_tick || '2s'} />
                  <ConfigRow label="Heartbeat Tick" value={c?.cluster?.heartbeat_tick || '500ms'} />
                </>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="network" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Listeners</CardTitle>
              <CardDescription>Configured listener endpoints</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {((c?.listeners ?? []) as ListenerConfig[]).map((l, i) => (
                <div key={i} className="p-3 rounded-lg border space-y-1">
                  <div className="flex items-center justify-between">
                    <span className="font-medium">{l.name}</span>
                    <div className="flex gap-2">
                      <Badge variant="outline">{l.protocol || 'http'}</Badge>
                      <Badge variant="secondary">{l.address}</Badge>
                    </div>
                  </div>
                  {l.routes && l.routes.length > 0 && (
                    <div className="text-xs text-muted-foreground">
                      {l.routes.length} route(s): {l.routes.map((r) => r.path).join(', ')}
                    </div>
                  )}
                </div>
              )) || (
                <p className="text-sm text-muted-foreground">No listeners configured</p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Pools</CardTitle>
              <CardDescription>Backend pool configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {((c?.pools ?? []) as PoolConfig[]).map((p, i) => (
                <div key={i} className="p-3 rounded-lg border space-y-1">
                  <div className="flex items-center justify-between">
                    <span className="font-medium">{p.name}</span>
                    <div className="flex gap-2">
                      <Badge variant="outline">{p.algorithm}</Badge>
                      <Badge variant="secondary">{p.backends?.length ?? 0} backends</Badge>
                    </div>
                  </div>
                  {p.health_check && (
                    <div className="text-xs text-muted-foreground">
                      Health: {p.health_check.type} {p.health_check.path} every {p.health_check.interval}
                    </div>
                  )}
                </div>
              )) || (
                <p className="text-sm text-muted-foreground">No pools configured</p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="security" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>TLS</CardTitle>
              <CardDescription>TLS configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Enabled" value={c?.tls ? 'Yes' : 'No'} />
              {c?.tls && (
                <>
                  <ConfigRow label="Certificate File" value={c?.tls?.cert_file || '(none)'} />
                  <ConfigRow label="Key File" value={c?.tls?.key_file ? '(configured)' : '(none)'} />
                  <ConfigRow label="ACME Enabled" value={c?.tls?.acme?.enabled ? 'Yes' : 'No'} />
                  {c?.tls?.acme?.enabled && (
                    <ConfigRow label="ACME Email" value={c?.tls?.acme?.email || '(none)'} />
                  )}
                </>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>WAF</CardTitle>
              <CardDescription>Web Application Firewall</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Enabled" value={c?.waf?.enabled ? 'Yes' : 'No'} />
              {c?.waf?.enabled && (
                <>
                  <ConfigRow label="Mode" value={c?.waf?.mode || 'unknown'} />
                  <ConfigRow label="IP ACL" value={c?.waf?.ip_acl?.enabled ? 'Enabled' : 'Disabled'} />
                  <ConfigRow label="Rate Limiting" value={c?.waf?.rate_limit?.enabled ? 'Enabled' : 'Disabled'} />
                  <ConfigRow label="Sanitizer" value={c?.waf?.sanitizer?.enabled ? 'Enabled' : 'Disabled'} />
                  <ConfigRow label="Detection" value={c?.waf?.detection?.enabled ? 'Enabled' : 'Disabled'} />
                  <ConfigRow label="Bot Detection" value={c?.waf?.bot_detection?.enabled ? 'Enabled' : 'Disabled'} />
                  <ConfigRow label="Response Headers" value={c?.waf?.response?.security_headers?.enabled ? 'Enabled' : 'Disabled'} />
                </>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>CORS</CardTitle>
              <CardDescription>Cross-Origin Resource Sharing</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <ConfigRow label="Enabled" value={c?.middleware?.cors?.enabled ? 'Yes' : 'No'} />
              {c?.middleware?.cors?.enabled && (
                <>
                  <ConfigRow label="Allowed Origins" value={c?.middleware?.cors?.allowed_origins?.join(', ') || '*'} />
                  <ConfigRow label="Allowed Methods" value={c?.middleware?.cors?.allowed_methods?.join(', ') || 'GET,POST,PUT,DELETE'} />
                </>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <div className="flex justify-end">
        <Button onClick={handleReload}>
          <RotateCcw className="mr-2 h-4 w-4"  aria-hidden="true" />
          Reload Configuration
        </Button>
      </div>
    </div>
  )
}

function ConfigRow({ label, value }: { label: string; value: string | number | boolean }) {
  return (
    <div className="flex items-center justify-between py-1">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-sm font-medium font-mono">{String(value)}</span>
    </div>
  )
}
