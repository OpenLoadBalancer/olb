import { useEffect, useState, useRef } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import { Activity, Server, Zap } from "lucide-react"
import { useMetrics, usePools } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"

// Simple chart component using SVG
function SimpleLineChart({ data, color = "#3b82f6" }: { data: number[]; color?: string }) {
  if (data.length < 2) return <div className="h-16 flex items-center justify-center text-xs text-muted-foreground">Collecting data...</div>
  const max = Math.max(...data)
  const min = Math.min(...data)
  const range = max - min || 1
  const width = 100
  const height = 40

  const points = data.map((val, i) => {
    const x = (i / (data.length - 1)) * width
    const y = height - ((val - min) / range) * height
    return `${x},${y}`
  }).join(' ')

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full h-16 overflow-visible">
      <polyline
        fill="none"
        stroke={color}
        strokeWidth="2"
        points={points}
        vectorEffect="non-scaling-stroke"
      />
      <defs>
        <linearGradient id={`grad-${color.replace('#', '')}`} x1="0%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" stopColor={color} stopOpacity="0.3" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <polygon
        fill={`url(#grad-${color.replace('#', '')})`}
        points={`0,${height} ${points} ${width},${height}`}
      />
    </svg>
  )
}

function SimpleBarChart({ data }: { data: { label: string; value: number }[] }) {
  const max = Math.max(...data.map(d => d.value))
  if (max === 0) return <div className="h-32 flex items-center justify-center text-sm text-muted-foreground">No data</div>

  return (
    <div className="flex items-end gap-2 h-32">
      {data.map((item, i) => (
        <div key={i} className="flex-1 flex flex-col items-center gap-1">
          <div
            className="w-full bg-primary/80 rounded-t"
            style={{ height: `${(item.value / max) * 100}%` }}
          />
          <span className="text-xs text-muted-foreground">{item.label}</span>
        </div>
      ))}
    </div>
  )
}

// Extract a numeric value from metrics data
function extractMetric(metrics: Record<string, any>, name: string): number {
  if (!metrics) return 0
  const val = metrics[name]
  if (val === undefined) return 0
  if (typeof val === 'number') return val
  if (typeof val === 'object' && val !== null) {
    if (val.value !== undefined) return Number(val.value) || 0
    if (val.counter !== undefined) return Number(val.counter) || 0
  }
  return 0
}

const MAX_HISTORY = 30

export function MetricsPage() {
  const { data: metrics, isLoading } = useMetrics()
  const { data: pools } = usePools()

  // Keep sliding window of metric samples for time-series charts
  const [requestHistory, setRequestHistory] = useState<number[]>([])
  const [errorHistory, setErrorHistory] = useState<number[]>([])
  const prevMetricsRef = useRef<Record<string, any> | null>(null)

  useEffect(() => {
    if (!metrics || typeof metrics !== 'object') return
    const m = metrics as Record<string, any>

    // Derive values from metrics or backend health
    const totalReqs = extractMetric(m, 'http_requests_total') ||
      (pools?.reduce((sum, p) => sum + p.backends.reduce((s, b) => s + (b.requests || 0), 0), 0) ?? 0)
    const errorCount = extractMetric(m, 'http_errors_total') ||
      extractMetric(m, 'errors_total') ||
      (pools?.reduce((sum, p) => sum + p.backends.reduce((s, b) => s + (b.errors || 0), 0), 0) ?? 0)

    // Compute deltas for requests/sec if we have previous snapshot
    const prev = prevMetricsRef.current
    const rps = prev ? Math.max(0, totalReqs - extractMetric(prev, 'http_requests_total')) : 0
    prevMetricsRef.current = m

    setRequestHistory(h => [...h.slice(-(MAX_HISTORY - 1)), rps || totalReqs])
    setErrorHistory(h => [...h.slice(-(MAX_HISTORY - 1)), errorCount])
  }, [metrics, pools])

  // Derive summary stats
  const totalRequests = pools?.reduce((sum, p) =>
    sum + p.backends.reduce((s, b) => s + (b.requests || 0), 0), 0) ?? 0

  const totalErrors = pools?.reduce((sum, p) =>
    sum + p.backends.reduce((s, b) => s + (b.errors || 0), 0), 0) ?? 0

  const errorRate = totalRequests > 0 ? ((totalErrors / totalRequests) * 100).toFixed(2) : '0.00'

  // Derive per-pool data for bar chart
  const poolData = pools?.map(p => ({
    label: p.name.substring(0, 8),
    value: p.backends.reduce((s, b) => s + (b.requests || 0), 0)
  })) ?? []

  // Derive backend health distribution
  const healthyBackends = pools?.reduce((sum, p) =>
    sum + p.backends.filter(b => b.healthy).length, 0) ?? 0
  const totalBackends = pools?.reduce((sum, p) => sum + p.backends.length, 0) ?? 0
  const unhealthyBackends = totalBackends - healthyBackends

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Metrics</h1>
          <p className="text-muted-foreground">Performance and traffic analytics</p>
        </div>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Metrics</h1>
          <p className="text-muted-foreground">Performance and traffic analytics</p>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {totalRequests > 1000000
                ? (totalRequests / 1000000).toFixed(1) + 'M'
                : totalRequests > 1000
                ? (totalRequests / 1000).toFixed(1) + 'K'
                : totalRequests}
            </div>
            <p className="text-xs text-muted-foreground">Since last restart</p>
            <SimpleLineChart data={requestHistory} color="#3b82f6" />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Backends</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalBackends}</div>
            <p className="text-xs text-muted-foreground">
              <span className="text-green-500">{healthyBackends} healthy</span>
              {unhealthyBackends > 0 && <span className="text-red-500">, {unhealthyBackends} unhealthy</span>}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Error Rate</CardTitle>
            <Activity className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{errorRate}%</div>
            <p className="text-xs text-muted-foreground">{totalErrors} total errors</p>
            <SimpleLineChart data={errorHistory} color="#ef4444" />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Pools</CardTitle>
            <Zap className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{pools?.length ?? 0}</div>
            <p className="text-xs text-muted-foreground">Backend pools active</p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="traffic" className="space-y-4">
        <TabsList>
          <TabsTrigger value="traffic">Traffic</TabsTrigger>
          <TabsTrigger value="pools">Pools</TabsTrigger>
          <TabsTrigger value="backends">Backend Health</TabsTrigger>
        </TabsList>

        <TabsContent value="traffic" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Requests by Pool</CardTitle>
                <CardDescription>Distribution across backend pools</CardDescription>
              </CardHeader>
              <CardContent>
                <SimpleBarChart data={poolData.length > 0 ? poolData : [{ label: 'No data', value: 0 }]} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Request Trend</CardTitle>
                <CardDescription>Recent request volume</CardDescription>
              </CardHeader>
              <CardContent>
                <SimpleLineChart data={requestHistory} color="#3b82f6" />
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="pools">
          <Card>
            <CardHeader>
              <CardTitle>Pool Overview</CardTitle>
              <CardDescription>Backend pool metrics</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {pools?.map((pool, i) => {
                  const poolReqs = pool.backends.reduce((s, b) => s + (b.requests || 0), 0)
                  const poolErrors = pool.backends.reduce((s, b) => s + (b.errors || 0), 0)
                  const healthy = pool.backends.filter(b => b.healthy).length
                  return (
                    <div key={pool.name} className="flex items-center justify-between p-3 rounded-lg border">
                      <div className="flex items-center gap-4">
                        <span className="text-sm text-muted-foreground w-6">#{i + 1}</span>
                        <div>
                          <div className="font-medium text-sm">{pool.name}</div>
                          <div className="text-xs text-muted-foreground">
                            {pool.algorithm} &middot; {pool.backends.length} backends
                          </div>
                        </div>
                      </div>
                      <div className="text-right text-sm">
                        <div>{poolReqs.toLocaleString()} requests</div>
                        <div className="flex items-center gap-1">
                          <Badge variant={poolErrors > 0 ? 'destructive' : 'secondary'} className="text-xs">
                            {poolErrors} errors
                          </Badge>
                          <Badge variant={healthy === pool.backends.length ? 'default' : 'outline'} className="text-xs">
                            {healthy}/{pool.backends.length} healthy
                          </Badge>
                        </div>
                      </div>
                    </div>
                  )
                })}
                {(!pools || pools.length === 0) && (
                  <p className="text-sm text-muted-foreground text-center py-4">No pools configured</p>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="backends">
          <Card>
            <CardHeader>
              <CardTitle>Backend Health Status</CardTitle>
              <CardDescription>Individual backend health and metrics</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {pools?.flatMap(pool =>
                  pool.backends.map(backend => (
                    <div key={backend.id} className="flex items-center justify-between p-3 rounded-lg border">
                      <div className="flex items-center gap-3">
                        <div className={cn("h-2 w-2 rounded-full", backend.healthy ? "bg-green-500" : "bg-red-500")} />
                        <div>
                          <div className="font-medium text-sm">{backend.id || backend.address}</div>
                          <div className="text-xs text-muted-foreground">{backend.address}</div>
                        </div>
                      </div>
                      <div className="flex items-center gap-3 text-sm">
                        <Badge variant={backend.healthy ? 'default' : 'destructive'} className="text-xs">
                          {backend.state}
                        </Badge>
                        <span className="text-muted-foreground">{backend.requests} reqs</span>
                        <span className={backend.errors > 0 ? 'text-red-600' : 'text-muted-foreground'}>
                          {backend.errors} err
                        </span>
                      </div>
                    </div>
                  ))
                )}
                {(!pools || pools.every(p => p.backends.length === 0)) && (
                  <p className="text-sm text-muted-foreground text-center py-4">No backends configured</p>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}

function cn(...classes: (string | boolean | undefined)[]) {
  return classes.filter(Boolean).join(' ')
}
