import { useState, useMemo } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ScrollArea } from '@/components/ui/scroll-area'
import { toast } from 'sonner'
import { cn, formatNumber } from '@/lib/utils'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  AreaChart,
  Area,
  BarChart,
  Bar,
  Legend
} from 'recharts'
import {
  Search,
  Download,
  Clock,
  Activity,
  TrendingUp,
  Globe,
  Server,
  Cpu,
  MemoryStick,
  Wifi,
  AlertTriangle,
  Filter,
  Star,
  History
} from 'lucide-react'

interface MetricSeries {
  name: string
  data: { timestamp: number; value: number }[]
  unit: string
  labels: Record<string, string>
}

interface Metric {
  name: string
  description: string
  type: 'counter' | 'gauge' | 'histogram'
  category: 'system' | 'network' | 'application' | 'custom'
  unit: string
  favorite: boolean
}

const availableMetrics: Metric[] = [
  { name: 'requests_total', description: 'Total number of requests', type: 'counter', category: 'application', unit: 'requests', favorite: true },
  { name: 'requests_duration', description: 'Request duration', type: 'histogram', category: 'application', unit: 'seconds', favorite: true },
  { name: 'active_connections', description: 'Active connections', type: 'gauge', category: 'network', unit: 'connections', favorite: true },
  { name: 'bytes_received', description: 'Bytes received', type: 'counter', category: 'network', unit: 'bytes', favorite: false },
  { name: 'bytes_sent', description: 'Bytes sent', type: 'counter', category: 'network', unit: 'bytes', favorite: false },
  { name: 'cpu_usage', description: 'CPU usage percentage', type: 'gauge', category: 'system', unit: 'percent', favorite: true },
  { name: 'memory_usage', description: 'Memory usage', type: 'gauge', category: 'system', unit: 'bytes', favorite: false },
  { name: 'goroutines', description: 'Number of goroutines', type: 'gauge', category: 'system', unit: 'goroutines', favorite: false },
  { name: 'gc_duration', description: 'GC pause duration', type: 'histogram', category: 'system', unit: 'seconds', favorite: false },
  { name: 'backend_health', description: 'Backend health status', type: 'gauge', category: 'application', unit: 'status', favorite: true },
  { name: 'cache_hits', description: 'Cache hit count', type: 'counter', category: 'application', unit: 'hits', favorite: false },
  { name: 'cache_misses', description: 'Cache miss count', type: 'counter', category: 'application', unit: 'misses', favorite: false },
  { name: 'waf_blocked', description: 'WAF blocked requests', type: 'counter', category: 'application', unit: 'requests', favorite: false },
  { name: 'rate_limited', description: 'Rate limited requests', type: 'counter', category: 'application', unit: 'requests', favorite: false },
  { name: 'tls_handshake_duration', description: 'TLS handshake duration', type: 'histogram', category: 'network', unit: 'seconds', favorite: false }
]

function generateMetricData(metricName: string, timeRange: string): MetricSeries[] {
  const points = timeRange === '1h' ? 60 : timeRange === '24h' ? 288 : timeRange === '7d' ? 168 : 720
  const now = Date.now()
  const interval = timeRange === '1h' ? 60000 : timeRange === '24h' ? 300000 : timeRange === '7d' ? 3600000 : 300000

  const series: MetricSeries[] = []

  if (metricName === 'requests_total') {
    series.push(
      {
        name: 'HTTP',
        data: Array.from({ length: points }, (_, i) => ({
          timestamp: now - (points - i) * interval,
          value: Math.floor(Math.random() * 500) + 800
        })),
        unit: 'req/min',
        labels: { method: 'http' }
      },
      {
        name: 'HTTPS',
        data: Array.from({ length: points }, (_, i) => ({
          timestamp: now - (points - i) * interval,
          value: Math.floor(Math.random() * 800) + 1200
        })),
        unit: 'req/min',
        labels: { method: 'https' }
      }
    )
  } else if (metricName === 'cpu_usage') {
    series.push({
      name: 'CPU Usage',
      data: Array.from({ length: points }, (_, i) => ({
        timestamp: now - (points - i) * interval,
        value: 30 + Math.random() * 40
      })),
      unit: '%',
      labels: {}
    })
  } else if (metricName === 'active_connections') {
    series.push({
      name: 'Connections',
      data: Array.from({ length: points }, (_, i) => ({
        timestamp: now - (points - i) * interval,
        value: Math.floor(Math.random() * 200) + 300
      })),
      unit: 'connections',
      labels: {}
    })
  } else {
    series.push({
      name: metricName,
      data: Array.from({ length: points }, (_, i) => ({
        timestamp: now - (points - i) * interval,
        value: Math.random() * 100
      })),
      unit: 'value',
      labels: {}
    })
  }

  return series
}

export function MetricsExplorer() {
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedMetric, setSelectedMetric] = useState<Metric>(availableMetrics[0])
  const [timeRange, setTimeRange] = useState('1h')
  const [chartType, setChartType] = useState<'line' | 'area' | 'bar'>('line')
  const [favorites, setFavorites] = useState<string[]>(
    availableMetrics.filter(m => m.favorite).map(m => m.name)
  )
  const [activeTab, setActiveTab] = useState('all')

  const metricData = useMemo(() =>
    generateMetricData(selectedMetric.name, timeRange),
    [selectedMetric, timeRange]
  )

  const filteredMetrics = availableMetrics.filter(m => {
    const matchesSearch = m.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
                         m.description.toLowerCase().includes(searchQuery.toLowerCase())
    const matchesTab = activeTab === 'all' ||
                      (activeTab === 'favorites' && favorites.includes(m.name)) ||
                      (activeTab === 'category' && m.category === 'application')
    return matchesSearch && matchesTab
  })

  const toggleFavorite = (metricName: string) => {
    setFavorites(prev =>
      prev.includes(metricName)
        ? prev.filter(n => n !== metricName)
        : [...prev, metricName]
    )
  }

  const exportData = () => {
    toast.success('Metrics exported as CSV')
  }

  const formatTimestamp = (timestamp: number) => {
    const date = new Date(timestamp)
    if (timeRange === '1h') return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
    if (timeRange === '24h') return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit' })
  }

  const chartData = metricData[0]?.data.map((d, i) => {
    const point: any = { timestamp: d.timestamp, label: formatTimestamp(d.timestamp) }
    metricData.forEach(series => {
      point[series.name] = series.data[i]?.value ?? 0
    })
    return point
  }) || []

  const categoryIcons = {
    system: Cpu,
    network: Wifi,
    application: Activity,
    custom: Globe
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Metrics Explorer</h1>
          <p className="text-muted-foreground">
            Explore and visualize system metrics
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={exportData}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Metrics List */}
        <Card className="lg:col-span-1">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Activity className="h-5 w-5" />
              Metrics
            </CardTitle>
            <CardDescription>Select a metric to visualize</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="relative">
                <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search metrics..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-9"
                />
              </div>

              <Tabs value={activeTab} onValueChange={setActiveTab}>
                <TabsList className="w-full">
                  <TabsTrigger value="all" className="flex-1">All</TabsTrigger>
                  <TabsTrigger value="favorites" className="flex-1">
                    <Star className="h-3.5 w-3.5 mr-1" />
                    Favs
                  </TabsTrigger>
                </TabsList>
              </Tabs>

              <ScrollArea className="h-[400px]">
                <div className="space-y-1">
                  {filteredMetrics.map(metric => {
                    const Icon = categoryIcons[metric.category]
                    const isFavorite = favorites.includes(metric.name)
                    const isSelected = selectedMetric.name === metric.name
                    return (
                      <div
                        key={metric.name}
                        className={cn(
                          'flex items-center gap-2 p-2 rounded-lg cursor-pointer transition-colors',
                          isSelected ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
                        )}
                        onClick={() => setSelectedMetric(metric)}
                      >
                        <Icon className={cn('h-4 w-4', isSelected ? 'text-primary-foreground' : 'text-muted-foreground')} />
                        <div className="flex-1 min-w-0">
                          <p className="font-medium text-sm truncate">{metric.name}</p>
                          <p className={cn('text-xs truncate', isSelected ? 'text-primary-foreground/70' : 'text-muted-foreground')}>
                            {metric.description}
                          </p>
                        </div>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6"
                          onClick={(e) => {
                            e.stopPropagation()
                            toggleFavorite(metric.name)
                          }}
                        >
                          <Star className={cn('h-3.5 w-3.5', isFavorite && 'fill-current text-yellow-500')} />
                        </Button>
                      </div>
                    )
                  })}
                </div>
              </ScrollArea>
            </div>
          </CardContent>
        </Card>

        {/* Chart Area */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className="flex items-center gap-2">
                  {selectedMetric.name}
                  <Badge variant="outline" className="capitalize">
                    {selectedMetric.type}
                  </Badge>
                </CardTitle>
                <CardDescription>{selectedMetric.description}</CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <Select value={timeRange} onValueChange={setTimeRange}>
                  <SelectTrigger className="w-[100px]">
                    <Clock className="mr-2 h-4 w-4" />
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1h">1 Hour</SelectItem>
                    <SelectItem value="24h">24 Hours</SelectItem>
                    <SelectItem value="7d">7 Days</SelectItem>
                    <SelectItem value="30d">30 Days</SelectItem>
                  </SelectContent>
                </Select>
                <Select value={chartType} onValueChange={(v) => setChartType(v as 'line' | 'area' | 'bar')}>
                  <SelectTrigger className="w-[100px]">
                    <TrendingUp className="mr-2 h-4 w-4" />
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="line">Line</SelectItem>
                    <SelectItem value="area">Area</SelectItem>
                    <SelectItem value="bar">Bar</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <div className="h-[350px]">
              <ResponsiveContainer width="100%" height="100%">
                {chartType === 'line' ? (
                  <LineChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis dataKey="label" tick={{ fontSize: 12 }} />
                    <YAxis tick={{ fontSize: 12 }} />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: 'hsl(var(--card))',
                        border: '1px solid hsl(var(--border))',
                        borderRadius: '6px'
                      }}
                    />
                    <Legend />
                    {metricData.map((series, i) => (
                      <Line
                        key={series.name}
                        type="monotone"
                        dataKey={series.name}
                        stroke={['#3b82f6', '#22c55e', '#f59e0b', '#ef4444'][i]}
                        strokeWidth={2}
                        dot={false}
                      />
                    ))}
                  </LineChart>
                ) : chartType === 'area' ? (
                  <AreaChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis dataKey="label" tick={{ fontSize: 12 }} />
                    <YAxis tick={{ fontSize: 12 }} />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: 'hsl(var(--card))',
                        border: '1px solid hsl(var(--border))',
                        borderRadius: '6px'
                      }}
                    />
                    <Legend />
                    {metricData.map((series, i) => (
                      <Area
                        key={series.name}
                        type="monotone"
                        dataKey={series.name}
                        stroke={['#3b82f6', '#22c55e', '#f59e0b', '#ef4444'][i]}
                        fill={['#3b82f6', '#22c55e', '#f59e0b', '#ef4444'][i]}
                        fillOpacity={0.3}
                      />
                    ))}
                  </AreaChart>
                ) : (
                  <BarChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis dataKey="label" tick={{ fontSize: 12 }} />
                    <YAxis tick={{ fontSize: 12 }} />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: 'hsl(var(--card))',
                        border: '1px solid hsl(var(--border))',
                        borderRadius: '6px'
                      }}
                    />
                    <Legend />
                    {metricData.map((series, i) => (
                      <Bar
                        key={series.name}
                        dataKey={series.name}
                        fill={['#3b82f6', '#22c55e', '#f59e0b', '#ef4444'][i]}
                      />
                    ))}
                  </BarChart>
                )}
              </ResponsiveContainer>
            </div>

            {/* Stats Summary */}
            <div className="mt-4 grid grid-cols-4 gap-4 pt-4 border-t">
              {metricData.map(series => {
                const values = series.data.map(d => d.value)
                const avg = values.reduce((a, b) => a + b, 0) / values.length
                const max = Math.max(...values)
                const min = Math.min(...values)
                return (
                  <div key={series.name} className="space-y-1">
                    <p className="text-sm font-medium">{series.name}</p>
                    <div className="grid grid-cols-3 gap-2 text-xs text-muted-foreground">
                      <div>
                        <span className="block">Avg</span>
                        <span className="font-mono">{avg.toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="block">Max</span>
                        <span className="font-mono">{max.toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="block">Min</span>
                        <span className="font-mono">{min.toFixed(2)}</span>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Metric Details */}
      <Card>
        <CardHeader>
          <CardTitle>Metric Details</CardTitle>
          <CardDescription>Information about the selected metric</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-4">
            <div className="space-y-1">
              <Label className="text-muted-foreground">Name</Label>
              <p className="font-mono text-sm">{selectedMetric.name}</p>
            </div>
            <div className="space-y-1">
              <Label className="text-muted-foreground">Type</Label>
              <Badge variant="outline" className="capitalize">
                {selectedMetric.type}
              </Badge>
            </div>
            <div className="space-y-1">
              <Label className="text-muted-foreground">Category</Label>
              <p className="capitalize">{selectedMetric.category}</p>
            </div>
            <div className="space-y-1">
              <Label className="text-muted-foreground">Unit</Label>
              <p>{selectedMetric.unit}</p>
            </div>
          </div>
          <div className="mt-4">
            <Label className="text-muted-foreground">Description</Label>
            <p className="mt-1">{selectedMetric.description}</p>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

import { Label } from '@/components/ui/label'
