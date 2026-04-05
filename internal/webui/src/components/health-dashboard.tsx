import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { cn, formatDuration } from '@/lib/utils'
import api from '@/lib/api'
import { Activity, Heart, AlertCircle, Clock, TrendingUp, TrendingDown } from 'lucide-react'
import type { Backend } from '@/types'

interface HealthStatus {
  backendId: string
  backendName: string
  status: 'healthy' | 'unhealthy' | 'degraded' | 'unknown'
  lastCheck: Date
  responseTime: number
  uptime: number
  consecutiveSuccesses: number
  consecutiveFailures: number
  history: { timestamp: Date; success: boolean; responseTime: number }[]
}

// Generate mock health data
function generateMockHealthData(backends: Backend[]): HealthStatus[] {
  return backends.map(backend => {
    const history = Array.from({ length: 20 }, (_, i) => ({
      timestamp: new Date(Date.now() - (19 - i) * 60000),
      success: Math.random() > 0.2,
      responseTime: 10 + Math.random() * 100
    }))

    const recentSuccesses = history.slice(-5).filter(h => h.success).length
    let status: HealthStatus['status'] = 'unknown'
    if (recentSuccesses === 5) status = 'healthy'
    else if (recentSuccesses >= 3) status = 'degraded'
    else if (recentSuccesses > 0) status = 'unhealthy'

    return {
      backendId: backend.id,
      backendName: backend.name,
      status,
      lastCheck: new Date(),
      responseTime: history[history.length - 1].responseTime,
      uptime: (history.filter(h => h.success).length / history.length) * 100,
      consecutiveSuccesses: history.slice(-5).reduce((acc, h) => h.success ? acc + 1 : 0, 0),
      consecutiveFailures: history.slice(-5).reduce((acc, h) => !h.success ? acc + 1 : 0, 0),
      history
    }
  })
}

function HealthSparkline({ history, className }: { history: HealthStatus['history']; className?: string }) {
  const height = 30
  const width = 100
  const maxResponseTime = Math.max(...history.map(h => h.responseTime), 100)

  const points = history.map((h, i) => {
    const x = (i / (history.length - 1)) * width
    const y = height - (h.responseTime / maxResponseTime) * height
    return `${x},${y}`
  }).join(' ')

  return (
    <svg width={width} height={height} className={className}>
      <polyline
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        points={points}
        className="text-primary"
      />
      {history.map((h, i) => (
        <circle
          key={i}
          cx={(i / (history.length - 1)) * width}
          cy={height - (h.responseTime / maxResponseTime) * height}
          r="3"
          className={cn(
            h.success ? 'fill-green-500' : 'fill-destructive'
          )}
        />
      ))}
    </svg>
  )
}

function HealthStatusCard({ status }: { status: HealthStatus }) {
  const statusConfig = {
    healthy: { color: 'bg-green-500', icon: Heart, label: 'Healthy' },
    degraded: { color: 'bg-amber-500', icon: Activity, label: 'Degraded' },
    unhealthy: { color: 'bg-destructive', icon: AlertCircle, label: 'Unhealthy' },
    unknown: { color: 'bg-muted', icon: Clock, label: 'Unknown' }
  }

  const config = statusConfig[status.status]
  const Icon = config.icon

  return (
    <Card className="relative overflow-hidden">
      <div className={cn('absolute top-0 right-0 h-1 w-full', config.color)} />
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base">{status.backendName}</CardTitle>
          <Badge variant={status.status === 'healthy' ? 'default' : status.status === 'degraded' ? 'secondary' : 'destructive'}>
            <Icon className="mr-1 h-3 w-3" />
            {config.label}
          </Badge>
        </div>
        <CardDescription>Last check: {status.lastCheck.toLocaleTimeString()}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Response Time */}
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Response Time</span>
            <span className="font-mono">{Math.round(status.responseTime)}ms</span>
          </div>
          <Progress value={Math.min((status.responseTime / 200) * 100, 100)} className="h-2" />
        </div>

        {/* Uptime */}
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Uptime</span>
            <span className="font-mono">{status.uptime.toFixed(1)}%</span>
          </div>
          <Progress value={status.uptime} className="h-2" />
        </div>

        {/* Sparkline */}
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">Last 20 checks</span>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger>
                <HealthSparkline history={status.history} className="text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent>
                <div className="space-y-1">
                  <p>Success: {status.consecutiveSuccesses}/5</p>
                  <p>Failures: {status.consecutiveFailures}/5</p>
                </div>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>

        {/* Stats */}
        <div className="grid grid-cols-2 gap-2 pt-2 text-xs">
          <div className="flex items-center gap-1 text-green-500">
            <TrendingUp className="h-3 w-3" />
            <span>{status.consecutiveSuccesses} consecutive successes</span>
          </div>
          <div className="flex items-center gap-1 text-destructive">
            <TrendingDown className="h-3 w-3" />
            <span>{status.consecutiveFailures} consecutive failures</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

// Global health summary component
function HealthSummary({ healthData }: { healthData: HealthStatus[] }) {
  const stats = {
    total: healthData.length,
    healthy: healthData.filter(h => h.status === 'healthy').length,
    degraded: healthData.filter(h => h.status === 'degraded').length,
    unhealthy: healthData.filter(h => h.status === 'unhealthy').length,
    avgResponseTime: healthData.reduce((acc, h) => acc + h.responseTime, 0) / healthData.length || 0,
    avgUptime: healthData.reduce((acc, h) => acc + h.uptime, 0) / healthData.length || 0
  }

  return (
    <div className="grid gap-4 md:grid-cols-4">
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">Overall Health</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">
            {((stats.healthy / stats.total) * 100).toFixed(0)}%
          </div>
          <p className="text-xs text-muted-foreground">
            {stats.healthy} of {stats.total} healthy
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">Avg Response Time</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">
            {Math.round(stats.avgResponseTime)}ms
          </div>
          <p className="text-xs text-muted-foreground">
            Across all backends
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">Avg Uptime</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">
            {stats.avgUptime.toFixed(1)}%
          </div>
          <p className="text-xs text-muted-foreground">
            Last 20 checks
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">Issues</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-destructive">
            {stats.degraded + stats.unhealthy}
          </div>
          <p className="text-xs text-muted-foreground">
            {stats.degraded} degraded, {stats.unhealthy} unhealthy
          </p>
        </CardContent>
      </Card>
    </div>
  )
}

export function HealthDashboard() {
  const { data: backends = [] } = useQuery<Backend[]>({
    queryKey: ['backends'],
    queryFn: async () => {
      const response = await api.get('/api/v1/backends')
      return response.data
    },
    refetchInterval: 30000
  })

  const [healthData, setHealthData] = useState<HealthStatus[]>([])

  // Update health data periodically
  useEffect(() => {
    if (backends.length > 0) {
      setHealthData(generateMockHealthData(backends))
    }

    const interval = setInterval(() => {
      if (backends.length > 0) {
        setHealthData(generateMockHealthData(backends))
      }
    }, 10000)

    return () => clearInterval(interval)
  }, [backends])

  if (backends.length === 0) {
    return (
      <Card className="p-8 text-center">
        <Heart className="mx-auto mb-4 h-12 w-12 text-muted-foreground opacity-20" />
        <h3 className="text-lg font-medium">No Backends</h3>
        <p className="text-muted-foreground">Add backends to see health status</p>
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      <HealthSummary healthData={healthData} />
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {healthData.map(status => (
          <HealthStatusCard key={status.backendId} status={status} />
        ))}
      </div>
    </div>
  )
}
