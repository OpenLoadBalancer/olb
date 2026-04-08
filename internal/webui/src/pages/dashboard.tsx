import { useEffect, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Activity, Layers, Radio, Server, Clock, TrendingUp, AlertCircle, CheckCircle, Download, RefreshCw } from "lucide-react"
import { useHealth, useSystemInfo, usePools, useRoutes } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

interface RealtimeStats {
  requestsPerSecond: number
  activeConnections: number
  totalRequests: number
  bytesTransferred: number
}

interface ActivityEvent {
  id: string
  event: string
  time: string
  type: 'success' | 'info' | 'warning' | 'error'
}

export function DashboardPage() {
  const { data: health, isLoading: healthLoading, error: healthError } = useHealth()
  const { data: systemInfo, isLoading: infoLoading, error: infoError } = useSystemInfo()
  const { data: pools } = usePools()
  const { data: routes } = useRoutes()
  const [stats, setStats] = useState<RealtimeStats>({
    requestsPerSecond: 1247,
    activeConnections: 83,
    totalRequests: 1523456789,
    bytesTransferred: 0,
  })
  const [events, setEvents] = useState<ActivityEvent[]>([
    { id: "1", event: 'Backend backend-1 marked healthy', time: '2 min ago', type: 'success' },
    { id: "2", event: 'Configuration reloaded', time: '15 min ago', type: 'info' },
    { id: "3", event: 'Pool api-pools updated', time: '1 hour ago', type: 'info' },
    { id: "4", event: 'Listener http restarted', time: '3 hours ago', type: 'warning' },
  ])
  const [isConnected, setIsConnected] = useState(false)

  // Simulate real-time updates
  useEffect(() => {
    const interval = setInterval(() => {
      setStats(prev => ({
        requestsPerSecond: Math.floor(1000 + Math.random() * 500),
        activeConnections: Math.floor(80 + Math.random() * 20),
        totalRequests: prev.totalRequests + Math.floor(Math.random() * 100),
        bytesTransferred: prev.bytesTransferred + Math.floor(Math.random() * 1000000),
      }))
    }, 3000)
    return () => clearInterval(interval)
  }, [])

  // WebSocket connection (commented out - would connect to real WS endpoint)
  useEffect(() => {
    // This would connect to actual WebSocket endpoint
    // const ws = new WebSocket(`ws://${window.location.host}/api/v1/ws`)
    // ws.onopen = () => setIsConnected(true)
    // ws.onmessage = (event) => {
    //   const data = JSON.parse(event.data)
    //   if (data.type === 'stats') setStats(data.payload)
    //   if (data.type === 'event') setEvents(prev => [data.payload, ...prev.slice(0, 49)])
    // }
    // ws.onclose = () => setIsConnected(false)
    //
    // Simulate connection for demo
    setTimeout(() => setIsConnected(true), 1000)
    // return () => ws.close()
  }, [])

  const handleRefresh = () => {
    toast.success("Dashboard refreshed")
    window.location.reload()
  }

  const handleExport = () => {
    const data = {
      timestamp: new Date().toISOString(),
      stats,
      systemInfo,
      health,
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
            <div className={cn("h-2 w-2 rounded-full", isConnected ? "bg-green-500" : "bg-red-500")} />
            <span className="text-sm text-muted-foreground">
              {isConnected ? "Live" : "Disconnected"}
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
              <span className="text-red-500">{pools?.reduce((sum, p) => sum + p.backends.filter(b => !b.healthy).length, 0) ?? 0} unhealthy</span>
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Requests/sec</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.requestsPerSecond.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground flex items-center gap-1">
              <TrendingUp className="h-3 w-3 text-green-500" />
              <span className="text-green-500">+12%</span> from last hour
            </p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Active Connections</CardTitle>
            <Activity className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.activeConnections}</div>
            <p className="text-xs text-muted-foreground">Current connections</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Activity className="h-4 w-4 text-purple-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{(stats.totalRequests / 1000000).toFixed(1)}M</div>
            <p className="text-xs text-muted-foreground">All time requests</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Data Transferred</CardTitle>
            <Activity className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{(stats.bytesTransferred / 1024 / 1024 / 1024).toFixed(2)} GB</div>
            <p className="text-xs text-muted-foreground">Since last restart</p>
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
                  variant={check.status === 'healthy' ? 'outline' : 'destructive'}
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
            <CardDescription>Latest events and changes</CardDescription>
          </div>
          <Button variant="ghost" size="sm" onClick={() => setEvents([])}>
            Clear
          </Button>
        </CardHeader>
        <CardContent>
          <div className="space-y-3 max-h-64 overflow-y-auto">
            {events.map((item) => (
              <div key={item.id} className="flex items-center justify-between text-sm p-2 rounded-lg hover:bg-muted/50">
                <div className="flex items-center gap-3">
                  <div className={cn("h-2 w-2 rounded-full", getEventIcon(item.type))} />
                  <span>{item.event}</span>
                </div>
                <span className="text-muted-foreground text-xs">{item.time}</span>
              </div>
            ))}
            {events.length === 0 && (
              <p className="text-sm text-muted-foreground text-center py-4">No recent activity</p>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
