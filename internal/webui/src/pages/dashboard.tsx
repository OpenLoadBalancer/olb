import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Activity, Layers, Radio, Server, Clock, AlertCircle, CheckCircle, Download, RefreshCw } from "lucide-react"
import { useHealth, useSystemInfo, usePools, useRoutes, useEvents } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

export function DashboardPage() {
  const { data: health, isLoading: healthLoading, error: healthError, refetch: refetchHealth } = useHealth()
  const { data: systemInfo, isLoading: infoLoading, error: infoError } = useSystemInfo()
  const { data: pools } = usePools()
  const { data: routes } = useRoutes()
  const { data: events } = useEvents()

  // Extract stats from real metrics data
  const totalRequests = pools?.reduce((sum, p) =>
    sum + p.backends.reduce((s, b) => s + (b.requests || 0), 0), 0) ?? 0

  // Unhealthy backend count
  const unhealthyBackends = pools?.reduce((sum, p) =>
    sum + p.backends.filter(b => !b.healthy).length, 0) ?? 0

  const handleRefresh = () => {
    refetchHealth()
    toast.success("Dashboard refreshed")
  }

  const handleExport = () => {
    const data = {
      timestamp: new Date().toISOString(),
      systemInfo,
      health,
      pools: pools?.map(p => ({ name: p.name, algorithm: p.algorithm, backends: p.backends.length })),
    }
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `olb-dashboard-${new Date().toISOString().split('T')[0]}.json`
    a.click()
    URL.revokeObjectURL(url)
    toast.success("Dashboard data exported")
  }

  const isLoading = healthLoading || infoLoading
  const hasError = healthError || infoError

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
            <p className="text-muted-foreground">Overview of your OpenLoadBalancer instance</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled>
              <RefreshCw className="mr-2 h-4 w-4" />
              Refresh
            </Button>
            <Button variant="outline" size="sm" disabled>
              <Download className="mr-2 h-4 w-4" />
              Export
            </Button>
          </div>
        </div>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <LoadingCard className="h-64" />
          <LoadingCard className="h-64" />
        </div>
      </div>
    )
  }

  if (hasError) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">Overview of your OpenLoadBalancer instance</p>
        </div>
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>
            Failed to load dashboard data. Please check your connection and try again.
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  const getEventIcon = (type: string) => {
    switch (type) {
      case 'success': return 'text-green-500'
      case 'warning': return 'text-amber-500'
      case 'error': return 'text-red-500'
      default: return 'text-blue-500'
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">Overview of your OpenLoadBalancer instance</p>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <div className={cn("h-2 w-2 rounded-full", health?.status === 'healthy' ? "bg-green-500" : "bg-red-500")} />
            <span className="text-sm text-muted-foreground">
              {health?.status === 'healthy' ? "Live" : "Degraded"}
            </span>
          </div>
          <Button variant="outline" size="sm" onClick={handleRefresh}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Button variant="outline" size="sm" onClick={handleExport}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Pools</CardTitle>
            <Layers className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{pools?.length ?? 0}</div>
            <p className="text-xs text-muted-foreground">Backend pools configured</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Routes</CardTitle>
            <Radio className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{routes?.length ?? 0}</div>
            <p className="text-xs text-muted-foreground">Active routes</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Backends</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{pools?.reduce((sum, p) => sum + p.backends.length, 0) ?? 0}</div>
            <p className="text-xs text-muted-foreground">
              <span className="text-green-500">{pools?.reduce((sum, p) => sum + p.backends.filter(b => b.healthy).length, 0) ?? 0} healthy</span>{", "}
              <span className={unhealthyBackends > 0 ? "text-red-500" : "text-muted-foreground"}>{unhealthyBackends} unhealthy</span>
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalRequests > 0 ? (totalRequests / 1000000).toFixed(1) + 'M' : '0'}</div>
            <p className="text-xs text-muted-foreground">Since last restart</p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Health Status</CardTitle>
            <Activity className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              <Badge variant={health?.status === 'healthy' ? 'default' : 'destructive'}>
                {health?.status || 'Unknown'}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              {health?.checks ? Object.keys(health.checks).length : 0} components checked
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Uptime</CardTitle>
            <Clock className="h-4 w-4 text-purple-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold flex items-center gap-1">
              <Clock className="h-5 w-5" />
              {systemInfo?.uptime || 'unknown'}
            </div>
            <p className="text-xs text-muted-foreground">v{systemInfo?.version || 'unknown'}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Go Version</CardTitle>
            <Activity className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{systemInfo?.go_version || 'unknown'}</div>
            <p className="text-xs text-muted-foreground">Runtime</p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>System Status</CardTitle>
            <CardDescription>Current system health and information</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Status</span>
              <Badge
                variant={health?.status === 'healthy' ? 'default' : 'destructive'}
                className="flex items-center gap-1"
              >
                {health?.status === 'healthy' ? (
                  <CheckCircle className="h-3 w-3" />
                ) : (
                  <AlertCircle className="h-3 w-3" />
                )}
                {health?.status || 'Unknown'}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Version</span>
              <span className="font-medium">{systemInfo?.version || 'unknown'}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Uptime</span>
              <span className="font-medium flex items-center gap-1">
                <Clock className="h-3 w-3" />
                {systemInfo?.uptime || 'unknown'}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Go Version</span>
              <span className="font-medium">{systemInfo?.go_version || 'unknown'}</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Health Checks</CardTitle>
            <CardDescription>Component health status</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {health?.checks && Object.entries(health.checks).map(([name, check]) => (
              <div key={name} className="flex items-center justify-between">
                <span className="text-sm capitalize">{name.replace(/_/g, ' ')}</span>
                <Badge
                  variant={check.status === 'healthy' || check.status === 'ok' ? 'outline' : 'destructive'}
                  className="text-xs"
                >
                  {check.status}
                </Badge>
              </div>
            ))}
            {!health?.checks && (
              <p className="text-sm text-muted-foreground">No health check data available</p>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Recent Activity</CardTitle>
            <CardDescription>Latest events and health changes</CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-3 max-h-64 overflow-y-auto">
            {events && events.length > 0 ? events.map((item) => (
              <div key={item.id} className="flex items-center justify-between text-sm p-2 rounded-lg hover:bg-muted/50">
                <div className="flex items-center gap-3">
                  <div className={cn("h-2 w-2 rounded-full", getEventIcon(item.type))} />
                  <span>{item.message}</span>
                </div>
                <span className="text-muted-foreground text-xs">{item.timestamp}</span>
              </div>
            )) : (
              <p className="text-sm text-muted-foreground text-center py-4">No recent activity</p>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
