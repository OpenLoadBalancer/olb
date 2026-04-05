import { useEffect, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import { Activity, TrendingUp, Clock, Server, Zap } from "lucide-react"

// Simple chart component using SVG
function SimpleLineChart({ data, color = "#3b82f6" }: { data: number[]; color?: string }) {
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

export function MetricsPage() {
  const [timeRange, setTimeRange] = useState<'1h' | '24h' | '7d' | '30d'>('24h')

  // Generate mock time series data
  const generateData = (base: number, variance: number, count: number) => {
    return Array.from({ length: count }, () =>
      Math.max(0, base + (Math.random() - 0.5) * variance)
    )
  }

  const [requestData, setRequestData] = useState<number[]>([])
  const [latencyData, setLatencyData] = useState<number[]>([])
  const [errorData, setErrorData] = useState<number[]>([])

  useEffect(() => {
    const count = timeRange === '1h' ? 60 : timeRange === '24h' ? 24 : timeRange === '7d' ? 7 : 30
    setRequestData(generateData(1000, 400, count))
    setLatencyData(generateData(25, 15, count))
    setErrorData(generateData(5, 8, count))
  }, [timeRange])

  const timeRangeLabels: Record<string, string> = {
    '1h': 'Last Hour',
    '24h': 'Last 24 Hours',
    '7d': 'Last 7 Days',
    '30d': 'Last 30 Days',
  }

  const topEndpoints = [
    { path: '/api/v1/users', requests: 45234, latency: 12, errors: 0.1 },
    { path: '/api/v1/auth', requests: 38921, latency: 45, errors: 0.5 },
    { path: '/api/v1/products', requests: 32145, latency: 18, errors: 0.2 },
    { path: '/api/v1/orders', requests: 28765, latency: 25, errors: 0.3 },
    { path: '/health', requests: 15678, latency: 2, errors: 0 },
  ]

  const statusCodes = [
    { code: '2xx', count: 145234, percent: 94.2 },
    { code: '3xx', count: 5234, percent: 3.4 },
    { code: '4xx', count: 3123, percent: 2.0 },
    { code: '5xx', count: 567, percent: 0.4 },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Metrics</h1>
          <p className="text-muted-foreground">Performance and traffic analytics</p>
        </div>
        <div className="flex gap-2">
          {(['1h', '24h', '7d', '30d'] as const).map((range) => (
            <Badge
              key={range}
              variant={timeRange === range ? 'default' : 'outline'}
              className="cursor-pointer"
              onClick={() => setTimeRange(range)}
            >
              {range}
            </Badge>
          ))}
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">154.2K</div>
            <div className="text-xs text-green-600 flex items-center gap-1 mt-1">
              <TrendingUp className="h-3 w-3" />
              +12.5% vs previous {timeRange}
            </div>
            {requestData.length > 0 && <SimpleLineChart data={requestData} color="#3b82f6" />}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Avg Latency</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">24.5ms</div>
            <div className="text-xs text-green-600 flex items-center gap-1 mt-1">
              <TrendingUp className="h-3 w-3" />
              -8.2% vs previous {timeRange}
            </div>
            {latencyData.length > 0 && <SimpleLineChart data={latencyData} color="#10b981" />}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Error Rate</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">0.42%</div>
            <div className="text-xs text-red-600 flex items-center gap-1 mt-1">
              <TrendingUp className="h-3 w-3" />
              +0.1% vs previous {timeRange}
            </div>
            {errorData.length > 0 && <SimpleLineChart data={errorData} color="#ef4444" />}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Throughput</CardTitle>
            <Zap className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">64.3 MB/s</div>
            <div className="text-xs text-green-600 flex items-center gap-1 mt-1">
              <TrendingUp className="h-3 w-3" />
              +5.8% vs previous {timeRange}
            </div>
            <SimpleLineChart data={generateData(60, 20, 24)} color="#8b5cf6" />
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="traffic" className="space-y-4">
        <TabsList>
          <TabsTrigger value="traffic">Traffic</TabsTrigger>
          <TabsTrigger value="endpoints">Endpoints</TabsTrigger>
          <TabsTrigger value="status">Status Codes</TabsTrigger>
        </TabsList>

        <TabsContent value="traffic" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Requests by Hour</CardTitle>
                <CardDescription>Traffic distribution over {timeRangeLabels[timeRange]}</CardDescription>
              </CardHeader>
              <CardContent>
                <SimpleBarChart data={[
                  { label: '00', value: 450 },
                  { label: '04', value: 320 },
                  { label: '08', value: 890 },
                  { label: '12', value: 1200 },
                  { label: '16', value: 1100 },
                  { label: '20', value: 950 },
                ]} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Response Time Distribution</CardTitle>
                <CardDescription>Latency percentiles</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  {[
                    { label: 'p50 (Median)', value: 18, color: 'bg-green-500' },
                    { label: 'p90', value: 35, color: 'bg-blue-500' },
                    { label: 'p95', value: 55, color: 'bg-amber-500' },
                    { label: 'p99', value: 120, color: 'bg-red-500' },
                  ].map((item) => (
                    <div key={item.label} className="space-y-1">
                      <div className="flex justify-between text-sm">
                        <span>{item.label}</span>
                        <span className="text-muted-foreground">{item.value}ms</span>
                      </div>
                      <div className="h-2 bg-muted rounded-full overflow-hidden">
                        <div
                          className={`h-full ${item.color} rounded-full`}
                          style={{ width: `${Math.min(100, (item.value / 150) * 100)}%` }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="endpoints">
          <Card>
            <CardHeader>
              <CardTitle>Top Endpoints</CardTitle>
              <CardDescription>Most requested API endpoints</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {topEndpoints.map((endpoint, i) => (
                  <div key={endpoint.path} className="flex items-center justify-between p-3 rounded-lg border">
                    <div className="flex items-center gap-4">
                      <span className="text-sm text-muted-foreground w-6">#{i + 1}</span>
                      <div>
                        <div className="font-medium font-mono text-sm">{endpoint.path}</div>
                        <div className="text-xs text-muted-foreground">
                          {endpoint.requests.toLocaleString()} requests
                        </div>
                      </div>
                    </div>
                    <div className="text-right text-sm">
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3 text-muted-foreground" />
                        {endpoint.latency}ms
                      </div>
                      <div className={endpoint.errors > 0.5 ? 'text-red-600' : 'text-muted-foreground'}>
                        {endpoint.errors}% errors
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="status">
          <Card>
            <CardHeader>
              <CardTitle>HTTP Status Codes</CardTitle>
              <CardDescription>Response status distribution</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {statusCodes.map((status) => (
                  <div key={status.code} className="space-y-1">
                    <div className="flex justify-between text-sm">
                      <Badge variant={status.code.startsWith('2') ? 'default' : status.code.startsWith('3') ? 'secondary' : status.code.startsWith('4') ? 'outline' : 'destructive'}>
                        {status.code}
                      </Badge>
                      <div className="flex items-center gap-4">
                        <span className="text-muted-foreground">{status.count.toLocaleString()}</span>
                        <span className="w-12 text-right">{status.percent}%</span>
                      </div>
                    </div>
                    <div className="h-2 bg-muted rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full ${
                          status.code.startsWith('2') ? 'bg-green-500' :
                          status.code.startsWith('3') ? 'bg-blue-500' :
                          status.code.startsWith('4') ? 'bg-amber-500' : 'bg-red-500'
                        }`}
                        style={{ width: `${status.percent}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
