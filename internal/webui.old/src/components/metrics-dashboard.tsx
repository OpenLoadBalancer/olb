import { useState, useEffect, useMemo } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { useWebSocket } from '@/hooks/websocket'
import { cn, formatBytes, formatNumber } from '@/lib/utils'
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  BarChart,
  Bar,
  PieChart,
  Pie,
  Cell,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceLine
} from 'recharts'
import {
  Activity,
  TrendingUp,
  TrendingDown,
  Users,
  Clock,
  Globe,
  Server,
  AlertTriangle,
  RefreshCw,
  Download
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { toast } from 'sonner'

interface MetricsData {
  timestamp: Date
  requestsPerSecond: number
  activeConnections: number
  bytesTransferred: number
  errorRate: number
  responseTime: number
  statusCodes: { code: string; count: number }
  topPaths: { path: string; count: number }[]
  topIps: { ip: string; count: number }[]
}

// Generate mock metrics
function generateMockMetrics(minutes: number): MetricsData[] {
  return Array.from({ length: minutes }, (_, i) => {
    const baseRps = 100 + Math.sin(i / 10) * 50
    return {
      timestamp: new Date(Date.now() - (minutes - i) * 60000),
      requestsPerSecond: baseRps + Math.random() * 50,
      activeConnections: Math.floor(baseRps * 0.3 + Math.random() * 100),
      bytesTransferred: Math.floor((baseRps * 1000) + Math.random() * 100000),
      errorRate: Math.random() * 5,
      responseTime: 20 + Math.random() * 80,
      statusCodes: {
        code: '200',
        count: Math.floor(baseRps * 0.9)
      },
      topPaths: [
        { path: '/api/users', count: Math.floor(baseRps * 0.3) },
        { path: '/api/products', count: Math.floor(baseRps * 0.2) },
        { path: '/health', count: Math.floor(baseRps * 0.1) }
      ],
      topIps: [
        { ip: '192.168.1.100', count: Math.floor(Math.random() * 100) },
        { ip: '192.168.1.101', count: Math.floor(Math.random() * 80) },
        { ip: '192.168.1.102', count: Math.floor(Math.random() * 60) }
      ]
    }
  })
}

const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899']

function MetricCard({
  title,
  value,
  unit,
  change,
  icon: Icon,
  trend,
  loading
}: {
  title: string
  value: string | number
  unit?: string
  change?: number
  icon: typeof Activity
  trend?: 'up' | 'down' | 'neutral'
  loading?: boolean
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
        <Icon className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-8 w-24" />
        ) : (
          <div className="text-2xl font-bold">
            {value}
            {unit && <span className="ml-1 text-sm text-muted-foreground">{unit}</span>}
          </div>
        )}
        {change !== undefined && !loading && (
          <div className={cn(
            'flex items-center text-xs',
            trend === 'up' ? 'text-green-500' : trend === 'down' ? 'text-destructive' : 'text-muted-foreground'
          )}>
            {trend === 'up' && <TrendingUp className="mr-1 h-3 w-3" />}
            {trend === 'down' && <TrendingDown className="mr-1 h-3 w-3" />}
            {change > 0 ? '+' : ''}{change.toFixed(1)}%
            <span className="ml-1 text-muted-foreground">from last hour</span>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function TrafficChart({ data, isLoading }: { data: MetricsData[]; isLoading: boolean }) {
  const chartData = data.map(d => ({
    time: d.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    rps: Math.round(d.requestsPerSecond),
    connections: d.activeConnections
  }))

  if (isLoading) {
    return (
      <Card className="h-[300px]">
        <CardContent className="flex items-center justify-center h-full">
          <Skeleton className="h-full w-full" />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Traffic Overview</CardTitle>
        <CardDescription>Requests per second and active connections</CardDescription>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={250}>
          <AreaChart data={chartData}>
            <defs>
              <linearGradient id="colorRps" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
            <XAxis dataKey="time" tick={{ fontSize: 12 }} />
            <YAxis tick={{ fontSize: 12 }} />
            <Tooltip
              contentStyle={{ background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))' }}
            />
            <Legend />
            <Area
              type="monotone"
              dataKey="rps"
              name="Requests/sec"
              stroke="#3b82f6"
              fillOpacity={1}
              fill="url(#colorRps)"
            />
            <Line
              type="monotone"
              dataKey="connections"
              name="Connections"
              stroke="#10b981"
              strokeWidth={2}
              dot={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}

function ResponseTimeChart({ data, isLoading }: { data: MetricsData[]; isLoading: boolean }) {
  const chartData = data.map(d => ({
    time: d.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    p50: d.responseTime * 0.5,
    p95: d.responseTime * 1.5,
    p99: d.responseTime * 2.5
  }))

  if (isLoading) {
    return (
      <Card className="h-[300px]">
        <CardContent className="flex items-center justify-center h-full">
          <Skeleton className="h-full w-full" />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Response Times</CardTitle>
        <CardDescription>Percentile distribution (ms)</CardDescription>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={250}>
          <LineChart data={chartData}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
            <XAxis dataKey="time" tick={{ fontSize: 12 }} />
            <YAxis tick={{ fontSize: 12 }} />
            <Tooltip
              contentStyle={{ background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))' }}
            />
            <Legend />
            <Line type="monotone" dataKey="p50" name="P50" stroke="#3b82f6" strokeWidth={2} />
            <Line type="monotone" dataKey="p95" name="P95" stroke="#f59e0b" strokeWidth={2} />
            <Line type="monotone" dataKey="p99" name="P99" stroke="#ef4444" strokeWidth={2} />
          </LineChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}

function ErrorRateChart({ data, isLoading }: { data: MetricsData[]; isLoading: boolean }) {
  const chartData = data.map(d => ({
    time: d.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    rate: d.errorRate
  }))

  if (isLoading) {
    return (
      <Card className="h-[300px]">
        <CardContent className="flex items-center justify-center h-full">
          <Skeleton className="h-full w-full" />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Error Rate</CardTitle>
        <CardDescription>Percentage of failed requests</CardDescription>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={250}>
          <AreaChart data={chartData}>
            <defs>
              <linearGradient id="colorError" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#ef4444" stopOpacity={0.3} />
                <stop offset="95%" stopColor="#ef4444" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
            <XAxis dataKey="time" tick={{ fontSize: 12 }} />
            <YAxis tick={{ fontSize: 12 }} domain={[0, 10]} />
            <Tooltip
              contentStyle={{ background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))' }}
            />
            <ReferenceLine y={5} stroke="#ef4444" strokeDasharray="3 3" />
            <Area
              type="monotone"
              dataKey="rate"
              name="Error Rate %"
              stroke="#ef4444"
              fill="url(#colorError)"
            />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}

function TopPathsTable({ data }: { data: MetricsData[] }) {
  const paths = useMemo(() => {
    const pathCounts: Record<string, number> = {}
    data.forEach(d => {
      d.topPaths.forEach(p => {
        pathCounts[p.path] = (pathCounts[p.path] || 0) + p.count
      })
    })
    return Object.entries(pathCounts)
      .map(([path, count]) => ({ path, count }))
      .sort((a, b) => b.count - a.count)
      .slice(0, 5)
  }, [data])

  return (
    <Card>
      <CardHeader>
        <CardTitle>Top Paths</CardTitle>
        <CardDescription>Most requested endpoints</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-2">
          {paths.map((p, i) => (
            <div key={p.path} className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Badge variant="secondary">#{i + 1}</Badge>
                <span className="font-mono text-sm">{p.path}</span>
              </div>
              <span className="text-muted-foreground">{formatNumber(p.count)}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function TopIpsTable({ data }: { data: MetricsData[] }) {
  const ips = useMemo(() => {
    const ipCounts: Record<string, number> = {}
    data.forEach(d => {
      d.topIps.forEach(i => {
        ipCounts[i.ip] = (ipCounts[i.ip] || 0) + i.count
      })
    })
    return Object.entries(ipCounts)
      .map(([ip, count]) => ({ ip, count }))
      .sort((a, b) => b.count - a.count)
      .slice(0, 5)
  }, [data])

  return (
    <Card>
      <CardHeader>
        <CardTitle>Top IPs</CardTitle>
        <CardDescription>Most active client IPs</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-2">
          {ips.map((ip, i) => (
            <div key={ip.ip} className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Badge variant="secondary">#{i + 1}</Badge>
                <span className="font-mono text-sm">{ip.ip}</span>
              </div>
              <span className="text-muted-foreground">{formatNumber(ip.count)}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

export function EnhancedMetricsDashboard() {
  const [timeRange, setTimeRange] = useState('1h')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [data, setData] = useState<MetricsData[]>([])
  const [isLoading, setIsLoading] = useState(true)

  // Load initial data
  useEffect(() => {
    const minutes = timeRange === '1h' ? 60 : timeRange === '6h' ? 360 : 1440
    setData(generateMockMetrics(minutes))
    setIsLoading(false)
  }, [timeRange])

  // Auto-refresh
  useEffect(() => {
    if (!autoRefresh) return

    const interval = setInterval(() => {
      setData(prev => {
        const newMetric = generateMockMetrics(1)[0]
        return [...prev.slice(-59), newMetric]
      })
    }, 5000)

    return () => clearInterval(interval)
  }, [autoRefresh])

  // Calculate stats
  const stats = useMemo(() => {
    if (data.length === 0) return null
    const latest = data[data.length - 1]
    const prev = data[data.length - 2] || latest

    const avgRps = data.reduce((acc, d) => acc + d.requestsPerSecond, 0) / data.length
    const avgResponseTime = data.reduce((acc, d) => acc + d.responseTime, 0) / data.length
    const totalBytes = data.reduce((acc, d) => acc + d.bytesTransferred, 0)
    const avgErrorRate = data.reduce((acc, d) => acc + d.errorRate, 0) / data.length

    return {
      rps: {
        current: latest.requestsPerSecond,
        change: ((latest.requestsPerSecond - prev.requestsPerSecond) / prev.requestsPerSecond) * 100
      },
      connections: latest.activeConnections,
      responseTime: {
        current: latest.responseTime,
        change: ((latest.responseTime - prev.responseTime) / prev.responseTime) * 100
      },
      errorRate: {
        current: latest.errorRate,
        change: ((latest.errorRate - prev.errorRate) / (prev.errorRate || 1)) * 100
      },
      avgRps,
      avgResponseTime,
      totalBytes,
      avgErrorRate
    }
  }, [data])

  const handleExport = () => {
    const csv = [
      'timestamp,requests_per_second,active_connections,response_time,error_rate',
      ...data.map(d => `${d.timestamp.toISOString()},${d.requestsPerSecond},${d.activeConnections},${d.responseTime},${d.errorRate}`)
    ].join('\n')

    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `metrics-${new Date().toISOString()}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    toast.success('Metrics exported')
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Metrics</h1>
          <p className="text-muted-foreground">
            Real-time performance and traffic analytics
          </p>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <Switch
              id="auto-refresh"
              checked={autoRefresh}
              onCheckedChange={setAutoRefresh}
            />
            <Label htmlFor="auto-refresh">Auto-refresh</Label>
          </div>
          <Select value={timeRange} onValueChange={setTimeRange}>
            <SelectTrigger className="w-[120px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1h">Last 1 hour</SelectItem>
              <SelectItem value="6h">Last 6 hours</SelectItem>
              <SelectItem value="24h">Last 24 hours</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={handleExport}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
        </div>
      </div>

      {/* Stats */}
      {stats && (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <MetricCard
            title="Requests/sec"
            value={Math.round(stats.rps.current)}
            change={stats.rps.change}
            trend={stats.rps.change > 0 ? 'up' : 'down'}
            icon={Activity}
            loading={isLoading}
          />
          <MetricCard
            title="Active Connections"
            value={stats.connections}
            icon={Users}
            loading={isLoading}
          />
          <MetricCard
            title="Avg Response Time"
            value={Math.round(stats.responseTime.current)}
            unit="ms"
            change={stats.responseTime.change}
            trend={stats.responseTime.change > 0 ? 'down' : 'up'}
            icon={Clock}
            loading={isLoading}
          />
          <MetricCard
            title="Error Rate"
            value={stats.errorRate.current.toFixed(2)}
            unit="%"
            change={stats.errorRate.change}
            trend={stats.errorRate.change > 0 ? 'down' : 'up'}
            icon={AlertTriangle}
            loading={isLoading}
          />
        </div>
      )}

      {/* Charts */}
      <Tabs defaultValue="traffic" className="space-y-6">
        <TabsList>
          <TabsTrigger value="traffic">Traffic</TabsTrigger>
          <TabsTrigger value="performance">Performance</TabsTrigger>
          <TabsTrigger value="errors">Errors</TabsTrigger>
          <TabsTrigger value="insights">Insights</TabsTrigger>
        </TabsList>

        <TabsContent value="traffic" className="space-y-6">
          <TrafficChart data={data} isLoading={isLoading} />
          <div className="grid gap-4 md:grid-cols-2">
            <TopPathsTable data={data} />
            <TopIpsTable data={data} />
          </div>
        </TabsContent>

        <TabsContent value="performance" className="space-y-6">
          <ResponseTimeChart data={data} isLoading={isLoading} />
        </TabsContent>

        <TabsContent value="errors" className="space-y-6">
          <ErrorRateChart data={data} isLoading={isLoading} />
        </TabsContent>

        <TabsContent value="insights" className="space-y-6">
          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">Total Requests</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {formatNumber(data.reduce((acc, d) => acc + d.requestsPerSecond, 0) * 60)}
                </div>
                <p className="text-xs text-muted-foreground">In selected period</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">Total Bandwidth</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {formatBytes(data.reduce((acc, d) => acc + d.bytesTransferred, 0))}
                </div>
                <p className="text-xs text-muted-foreground">In selected period</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">Success Rate</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {(100 - (stats?.avgErrorRate || 0)).toFixed(2)}%
                </div>
                <p className="text-xs text-muted-foreground">Average</p>
              </CardContent>
            </Card>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
