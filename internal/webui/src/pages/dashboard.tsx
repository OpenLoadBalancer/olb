import { useEffect, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Activity, Layers, Radio, Server, Clock, TrendingUp, AlertCircle, CheckCircle } from "lucide-react"

export function DashboardPage() {
  const [systemInfo] = useState<{version: string; uptime: string; go_version: string} | null>(null)
  const [health] = useState<{status: 'healthy' | 'unhealthy'; checks?: Record<string, {status: string; message: string}>} | null>(null)
  const [stats, setStats] = useState({
    pools: 0,
    listeners: 0,
    backends: 0,
    requestsPerSecond: 0,
    totalRequests: 0,
  })

  useEffect(() => {
    // Mock data - API calls would go here
    setStats({
      pools: 4,
      listeners: 3,
      backends: 8,
      requestsPerSecond: 1247,
      totalRequests: 1523456789,
    })
  }, [])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-muted-foreground">Overview of your OpenLoadBalancer instance</p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Pools</CardTitle>
            <Layers className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.pools}</div>
            <p className="text-xs text-muted-foreground">Backend pools configured</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Listeners</CardTitle>
            <Radio className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.listeners}</div>
            <p className="text-xs text-muted-foreground">Active listeners</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Backends</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.backends}</div>
            <p className="text-xs text-muted-foreground">
              <span className="text-green-500">6 healthy</span>, <span className="text-red-500">2 unhealthy</span>
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

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>System Status</CardTitle>
            <CardDescription>Current system health and information</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Status</span>
              <Badge variant={health?.status === 'healthy' ? 'default' : 'destructive'} className="flex items-center gap-1">
                {health?.status === 'healthy' ? <CheckCircle className="h-3 w-3" /> : <AlertCircle className="h-3 w-3" />}
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
                <Badge variant={check.status === 'healthy' ? 'outline' : 'destructive'} className="text-xs">
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
        <CardHeader>
          <CardTitle>Recent Activity</CardTitle>
          <CardDescription>Latest events and changes</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {[
              { event: 'Backend backend-1 marked healthy', time: '2 min ago', type: 'success' },
              { event: 'Configuration reloaded', time: '15 min ago', type: 'info' },
              { event: 'Pool api-pools updated', time: '1 hour ago', type: 'info' },
              { event: 'Listener http restarted', time: '3 hours ago', type: 'warning' },
            ].map((item, i) => (
              <div key={i} className="flex items-center justify-between text-sm">
                <span>{item.event}</span>
                <span className="text-muted-foreground">{item.time}</span>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
