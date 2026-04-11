import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AlertTriangle, Ban, Globe, Bot, RefreshCw, Shield, ShieldCheck, ShieldAlert, Eye } from "lucide-react"
import { cn } from "@/lib/utils"
import { useWAFStatus, useConfig } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"

interface WAFLayer {
  id: string
  name: string
  icon: React.ComponentType<{ className?: string }>
  active: boolean
}

export function WAFPage() {
  useDocumentTitle("WAF")
  const { data: wafStatus, isLoading, error, refetch } = useWAFStatus()
  const { data: config } = useConfig()

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">Protect your applications from attacks</p>
        </div>
        <LoadingCard />
      </div>
    )
  }

  if (error || !wafStatus?.enabled) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">Protect your applications from attacks</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-muted-foreground">
              {error ? `Failed to load WAF status: ${error.message}` : "WAF is not enabled. Enable it in your configuration file."}
            </p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4" /> Retry
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  // Extract WAF layers from status
  const layers = (wafStatus.layers || {}) as Record<string, boolean>
  const layerList: WAFLayer[] = [
    { id: 'ip_acl', name: 'IP ACL', icon: Ban, active: !!layers.ip_acl },
    { id: 'rate_limit', name: 'Rate Limiting', icon: Shield, active: !!layers.rate_limit },
    { id: 'sanitizer', name: 'Sanitizer', icon: ShieldCheck, active: !!layers.sanitizer },
    { id: 'detection', name: 'Detection', icon: Eye, active: !!layers.detection },
    { id: 'bot_detect', name: 'Bot Detection', icon: Bot, active: !!layers.bot_detect },
    { id: 'response', name: 'Response', icon: ShieldAlert, active: !!layers.response },
  ]

  const activeLayers = layerList.filter(l => l.active).length

  // Extract stats from WAF analytics if available
  const stats = wafStatus.stats as Record<string, any> | undefined
  const totalBlocked = stats?.total_blocked ?? stats?.blocked ?? 0
  const totalChallenges = stats?.total_challenges ?? stats?.challenges ?? 0
  const totalRequests = stats?.total_requests ?? 0

  // Get WAF config for detailed view
  const wafConfig = (config as any)?.waf as Record<string, any> | undefined

  // Extract rate limit rules from config
  const rateLimitRules = wafConfig?.rate_limit?.rules as Array<Record<string, any>> ?? []

  // Extract detection config
  const detectionConfig = wafConfig?.detection as Record<string, any> | undefined
  const detectors = detectionConfig?.detectors as Record<string, Record<string, any>> | undefined

  const mode = wafStatus.mode || 'unknown'

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">Protect your applications from attacks</p>
        </div>
        <div className="flex items-center gap-3">
          <Badge variant={mode === 'enforce' ? 'default' : 'secondary'}>
            {mode === 'enforce' ? 'Enforce Mode' : mode === 'monitor' ? 'Monitor Mode' : mode}
          </Badge>
          <Badge variant="outline">6-Layer Pipeline</Badge>
        </div>
      </div>

      <div className="grid gap-4 grid-cols-2 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Active Layers</CardTitle>
            <Shield className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{activeLayers}/{layerList.length}</div>
            <p className="text-xs text-muted-foreground">Protection layers active</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Threats Blocked</CardTitle>
            <Ban className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600">
              {typeof totalBlocked === 'number' ? totalBlocked.toLocaleString() : 'N/A'}
            </div>
            <p className="text-xs text-muted-foreground">Since last restart</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Bot Challenges</CardTitle>
            <Bot className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-amber-600">
              {typeof totalChallenges === 'number' ? totalChallenges.toLocaleString() : 'N/A'}
            </div>
            <p className="text-xs text-muted-foreground">Since last restart</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Globe className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {typeof totalRequests === 'number' ? totalRequests.toLocaleString() : 'N/A'}
            </div>
            <p className="text-xs text-muted-foreground">Since last restart</p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="layers" className="space-y-4">
        <TabsList className="flex-wrap h-auto">
          <TabsTrigger value="layers">Protection Layers</TabsTrigger>
          <TabsTrigger value="detection">Detection Engines</TabsTrigger>
          <TabsTrigger value="ratelimit">Rate Limiting</TabsTrigger>
          <TabsTrigger value="config">Configuration</TabsTrigger>
        </TabsList>

        <TabsContent value="layers" className="space-y-4">
          <h3 className="text-lg font-medium">6-Layer Security Pipeline</h3>
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {layerList.map((layer) => (
              <Card key={layer.id} className={cn("transition-colors", layer.active && "border-primary/50")}>
                <CardContent className="p-4">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-3">
                      <div className={cn(
                        "p-2 rounded-lg",
                        layer.active ? "bg-primary/10" : "bg-muted"
                      )}>
                        <layer.icon className={cn(
                          "h-5 w-5",
                          layer.active ? "text-primary" : "text-muted-foreground"
                        )} />
                      </div>
                      <div>
                        <div className="font-medium">{layer.name}</div>
                        <div className="text-xs text-muted-foreground">
                          Layer: {layer.id.replace(/_/g, ' ')}
                        </div>
                      </div>
                    </div>
                    <Badge variant={layer.active ? 'default' : 'secondary'}>
                      {layer.active ? 'Active' : 'Inactive'}
                    </Badge>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="detection">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <AlertTriangle className="h-5 w-5 text-amber-500" />
                Detection Engines
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {detectors ? Object.entries(detectors).map(([name, cfg]) => (
                  <div key={name} className="flex flex-wrap items-center justify-between gap-2 p-3 rounded-lg border">
                    <div className="flex items-center gap-3">
                      <ShieldCheck className={cn("h-5 w-5", cfg.enabled ? "text-green-500" : "text-muted-foreground")} />
                      <div>
                        <div className="font-medium capitalize">{name.replace(/_/g, ' ')} Detection</div>
                        <div className="text-xs text-muted-foreground">
                          Detects {name === 'sqli' ? 'SQL injection' : name === 'xss' ? 'cross-site scripting' : name} attacks
                        </div>
                      </div>
                    </div>
                    <Badge variant={cfg.enabled ? 'default' : 'secondary'}>
                      {cfg.enabled ? 'Active' : 'Disabled'}
                    </Badge>
                  </div>
                )) : (
                  <p className="text-sm text-muted-foreground text-center py-4">
                    Detection engine configuration not available. Configure in the WAF section of your config file.
                  </p>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="ratelimit" className="space-y-4">
          <h3 className="text-lg font-medium">WAF Rate Limiting Rules</h3>
          {rateLimitRules.length > 0 ? (
            <div className="grid gap-4">
              {rateLimitRules.map((rl, i) => (
                <Card key={i}>
                  <CardContent className="p-4">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div>
                        <div className="font-medium">{rl.id || `Rule ${i + 1}`}</div>
                        <div className="text-sm text-muted-foreground">
                          {rl.limit || rl.requests || '?'} requests per {rl.window || '?'}
                          {rl.scope && ` (scope: ${rl.scope})`}
                        </div>
                      </div>
                      <div className="flex items-center gap-4">
                        <Badge variant={rl.action === 'block' ? 'destructive' : 'default'}>
                          {rl.action || 'block'}
                        </Badge>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          ) : (
            <Card>
              <CardContent className="p-6">
                <p className="text-sm text-muted-foreground text-center">
                  {layers.rate_limit
                    ? "Rate limiting is active but no custom rules are configured. Using default limits."
                    : "Rate limiting is not enabled in the WAF configuration."}
                </p>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="config">
          <Card>
            <CardHeader>
              <CardTitle>WAF Configuration</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-sm">Enabled</span>
                  <Badge variant={wafStatus.enabled ? 'default' : 'secondary'}>
                    {wafStatus.enabled ? 'Yes' : 'No'}
                  </Badge>
                </div>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-sm">Mode</span>
                  <Badge variant="outline">{mode}</Badge>
                </div>
                {wafConfig?.ip_acl && (
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-sm">IP ACL Auto-Ban</span>
                    <Badge variant={wafConfig.ip_acl.auto_ban?.enabled ? 'default' : 'secondary'}>
                      {wafConfig.ip_acl.auto_ban?.enabled ? 'Enabled' : 'Disabled'}
                    </Badge>
                  </div>
                )}
                {wafConfig?.sanitizer && (
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-sm">Sanitizer</span>
                    <Badge variant={wafConfig.sanitizer.enabled ? 'default' : 'secondary'}>
                      {wafConfig.sanitizer.enabled ? 'Enabled' : 'Disabled'}
                    </Badge>
                  </div>
                )}
                {wafConfig?.bot_detection && (
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-sm">Bot Detection Mode</span>
                    <Badge variant="outline">{wafConfig.bot_detection.mode || 'unknown'}</Badge>
                  </div>
                )}
              </div>
              <p className="text-xs text-muted-foreground mt-4">
                WAF is configured via the config file. Edit the config file and reload to make changes.
              </p>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
