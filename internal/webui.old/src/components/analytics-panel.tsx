import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { DatePicker } from '@/components/ui/date-picker'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { DataTable } from '@/components/data-table'
import { SimpleLineChart, SimpleBarChart, SimplePieChart } from '@/components/charts'
import api from '@/lib/api'
import { toast } from 'sonner'
import { cn, formatBytes, formatNumber } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  BarChart3,
  Download,
  Calendar,
  TrendingUp,
  TrendingDown,
  Activity,
  Users,
  Globe,
  Shield,
  AlertTriangle,
  FileText,
  Clock,
  Filter,
  RefreshCw,
  ChevronRight,
  PieChart,
  LineChart,
  BarChart
} from 'lucide-react'

interface ReportData {
  period: string
  totalRequests: number
  avgResponseTime: number
  errorRate: number
  uniqueVisitors: number
  bandwidth: number
  blockedRequests: number
  topCountries: { name: string; count: number }[]
  topPaths: { path: string; count: number }[]
  hourlyDistribution: { hour: number; requests: number }[]
  statusCodes: { code: string; count: number }[]
}

interface SavedReport {
  id: string
  name: string
  type: string
  createdAt: Date
  schedule?: string
}

const mockReportData: ReportData = {
  period: 'Last 7 Days',
  totalRequests: 15234567,
  avgResponseTime: 23.5,
  errorRate: 0.12,
  uniqueVisitors: 89234,
  bandwidth: 1024 * 1024 * 1024 * 45, // 45GB
  blockedRequests: 1234,
  topCountries: [
    { name: 'United States', count: 45234 },
    { name: 'Germany', count: 23412 },
    { name: 'United Kingdom', count: 18765 },
    { name: 'France', count: 15678 },
    { name: 'Japan', count: 12345 }
  ],
  topPaths: [
    { path: '/api/users', count: 234567 },
    { path: '/api/products', count: 189234 },
    { path: '/health', count: 145678 },
    { path: '/metrics', count: 98765 },
    { path: '/api/orders', count: 87654 }
  ],
  hourlyDistribution: Array.from({ length: 24 }, (_, i) => ({
    hour: i,
    requests: Math.floor(Math.random() * 100000) + 50000
  })),
  statusCodes: [
    { code: '200', count: 14567890 },
    { code: '201', count: 234567 },
    { code: '301', count: 123456 },
    { code: '404', count: 23456 },
    { code: '500', count: 1234 }
  ]
}

const mockSavedReports: SavedReport[] = [
  { id: '1', name: 'Weekly Traffic Report', type: 'traffic', createdAt: new Date(Date.now() - 86400000), schedule: 'Weekly' },
  { id: '2', name: 'Security Analysis', type: 'security', createdAt: new Date(Date.now() - 172800000) },
  { id: '3', name: 'Performance Summary', type: 'performance', createdAt: new Date(Date.now() - 259200000), schedule: 'Daily' }
]

const reportColumns: ColumnDef<SavedReport>[] = [
  {
    accessorKey: 'name',
    header: 'Report Name',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <FileText className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{row.original.name}</span>
      </div>
    )
  },
  {
    accessorKey: 'type',
    header: 'Type',
    cell: ({ row }) => (
      <Badge variant="outline" className="capitalize">{row.original.type}</Badge>
    )
  },
  {
    accessorKey: 'schedule',
    header: 'Schedule',
    cell: ({ row }) => row.original.schedule ? (
      <Badge variant="secondary">{row.original.schedule}</Badge>
    ) : (
      <span className="text-muted-foreground">-</span>
    )
  },
  {
    accessorKey: 'createdAt',
    header: 'Created',
    cell: ({ row }) => row.original.createdAt.toLocaleDateString()
  }
]

export function AnalyticsPanel() {
  const [activeTab, setActiveTab] = useState('overview')
  const [dateRange, setDateRange] = useState('7d')
  const [reportType, setReportType] = useState('traffic')
  const [isGenerating, setIsGenerating] = useState(false)
  const [reportData, setReportData] = useState<ReportData>(mockReportData)

  const generateReport = async () => {
    setIsGenerating(true)
    // Simulate report generation
    await new Promise(resolve => setTimeout(resolve, 2000))
    setReportData(mockReportData)
    setIsGenerating(false)
    toast.success('Report generated successfully')
  }

  const exportReport = (format: 'pdf' | 'csv' | 'json') => {
    toast.success(`Report exported as ${format.toUpperCase()}`)
  }

  const stats = [
    {
      title: 'Total Requests',
      value: formatNumber(reportData.totalRequests),
      change: 12.5,
      trend: 'up',
      icon: Activity
    },
    {
      title: 'Avg Response Time',
      value: `${reportData.avgResponseTime}ms`,
      change: -8.2,
      trend: 'down',
      icon: Clock
    },
    {
      title: 'Error Rate',
      value: `${reportData.errorRate}%`,
      change: -15.3,
      trend: 'down',
      icon: AlertTriangle
    },
    {
      title: 'Unique Visitors',
      value: formatNumber(reportData.uniqueVisitors),
      change: 23.1,
      trend: 'up',
      icon: Users
    }
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Analytics & Reports</h1>
          <p className="text-muted-foreground">
            Generate insights and reports about your load balancer
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={dateRange} onValueChange={setDateRange}>
            <SelectTrigger className="w-[140px]">
              <Calendar className="mr-2 h-4 w-4" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="24h">Last 24 Hours</SelectItem>
              <SelectItem value="7d">Last 7 Days</SelectItem>
              <SelectItem value="30d">Last 30 Days</SelectItem>
              <SelectItem value="90d">Last 90 Days</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={() => exportReport('csv')}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
          <Button onClick={generateReport} disabled={isGenerating}>
            {isGenerating ? (
              <>
                <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                Generating...
              </>
            ) : (
              <>
                <BarChart3 className="mr-2 h-4 w-4" />
                Generate
              </>
            )}
          </Button>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="traffic">Traffic</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="performance">Performance</TabsTrigger>
          <TabsTrigger value="saved">Saved Reports</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          {/* Stats */}
          <div className="grid gap-4 md:grid-cols-4">
            {stats.map((stat, index) => {
              const Icon = stat.icon
              return (
                <Card key={index}>
                  <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                    <CardTitle className="text-sm font-medium">{stat.title}</CardTitle>
                    <Icon className="h-4 w-4 text-muted-foreground" />
                  </CardHeader>
                  <CardContent>
                    <div className="text-2xl font-bold">{stat.value}</div>
                    <div className={cn(
                      'flex items-center text-xs',
                      stat.trend === 'up' ? 'text-green-500' : 'text-destructive'
                    )}>
                      {stat.trend === 'up' ? <TrendingUp className="mr-1 h-3 w-3" /> : <TrendingDown className="mr-1 h-3 w-3" />}
                      {stat.change > 0 ? '+' : ''}{stat.change}%
                      <span className="ml-1 text-muted-foreground">vs last period</span>
                    </div>
                  </CardContent>
                </Card>
              )
            })}
          </div>

          {/* Charts */}
          <div className="grid gap-4 md:grid-cols-2">
            <SimpleLineChart
              title="Traffic Overview"
              description="Requests over time"
              data={reportData.hourlyDistribution.map(d => ({ label: `${d.hour}:00`, value: d.requests }))}
              color="#3b82f6"
              showArea
            />
            <SimplePieChart
              title="Status Codes"
              description="Response code distribution"
              data={reportData.statusCodes.map(s => ({ label: s.code, value: s.count }))}
            />
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Globe className="h-5 w-5" />
                  Top Countries
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {reportData.topCountries.map((country, index) => (
                    <div key={country.name} className="flex items-center gap-3">
                      <span className="text-muted-foreground w-6">#{index + 1}</span>
                      <div className="flex-1">
                        <div className="flex items-center justify-between mb-1">
                          <span className="font-medium">{country.name}</span>
                          <span className="text-sm text-muted-foreground">
                            {formatNumber(country.count)}
                          </span>
                        </div>
                        <div className="h-2 rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-primary"
                            style={{
                              width: `${(country.count / reportData.topCountries[0].count) * 100}%`
                            }}
                          />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Activity className="h-5 w-5" />
                  Top Paths
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {reportData.topPaths.map((path, index) => (
                    <div key={path.path} className="flex items-center gap-3">
                      <span className="text-muted-foreground w-6">#{index + 1}</span>
                      <div className="flex-1">
                        <div className="flex items-center justify-between mb-1">
                          <code className="text-sm">{path.path}</code>
                          <span className="text-sm text-muted-foreground">
                            {formatNumber(path.count)}
                          </span>
                        </div>
                        <div className="h-2 rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-primary"
                            style={{
                              width: `${(path.count / reportData.topPaths[0].count) * 100}%`
                            }}
                          />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="traffic" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Traffic Analysis</CardTitle>
              <CardDescription>Detailed traffic metrics and trends</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-6">
                <div className="grid gap-4 md:grid-cols-3">
                  <div className="space-y-2">
                    <Label className="text-muted-foreground">Total Bandwidth</Label>
                    <p className="text-2xl font-bold">{formatBytes(reportData.bandwidth)}</p>
                  </div>
                  <div className="space-y-2">
                    <Label className="text-muted-foreground">Requests/sec (Peak)</Label>
                    <p className="text-2xl font-bold">{formatNumber(1250)}</p>
                  </div>
                  <div className="space-y-2">
                    <Label className="text-muted-foreground">Cache Hit Rate</Label>
                    <p className="text-2xl font-bold">87.3%</p>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="security" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Shield className="h-5 w-5" />
                Security Summary
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid gap-4 md:grid-cols-4">
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Blocked Requests</Label>
                  <p className="text-2xl font-bold text-destructive">
                    {formatNumber(reportData.blockedRequests)}
                  </p>
                </div>
                <div className="space-y-2">
                  <Label className="text-muted-foreground">WAF Triggers</Label>
                  <p className="text-2xl font-bold">{formatNumber(456)}</p>
                </div>
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Rate Limited</Label>
                  <p className="text-2xl font-bold">{formatNumber(234)}</p>
                </div>
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Banned IPs</Label>
                  <p className="text-2xl font-bold">{formatNumber(12)}</p>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="performance" className="space-y-6">
          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">P50 Latency</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold">18ms</div>
                <p className="text-xs text-muted-foreground">50th percentile</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">P95 Latency</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold">45ms</div>
                <p className="text-xs text-muted-foreground">95th percentile</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">P99 Latency</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold">89ms</div>
                <p className="text-xs text-muted-foreground">99th percentile</p>
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <SimpleLineChart
              title="Response Time Distribution"
              description="Latency percentiles over time"
              data={Array.from({ length: 24 }, (_, i) => ({
                label: `${i}:00`,
                value: 15 + Math.random() * 30
              }))}
              color="#8b5cf6"
            />
            <SimpleBarChart
              title="Backend Performance"
              description="Avg response time by backend"
              data={[
                { label: 'backend-1', value: 22 },
                { label: 'backend-2', value: 28 },
                { label: 'backend-3', value: 19 },
                { label: 'backend-4', value: 35 },
                { label: 'backend-5', value: 24 }
              ]}
              color="#10b981"
            />
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Pool Performance Metrics</CardTitle>
              <CardDescription>Detailed performance by backend pool</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-6">
                {[
                  { name: 'web-pool', requests: 5234567, avgTime: 21, errorRate: 0.08, healthy: 5, total: 5 },
                  { name: 'api-pool', requests: 8912345, avgTime: 18, errorRate: 0.05, healthy: 3, total: 4 },
                  { name: 'static-pool', requests: 2345678, avgTime: 12, errorRate: 0.02, healthy: 2, total: 2 }
                ].map(pool => (
                  <div key={pool.name} className="space-y-2">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{pool.name}</span>
                        <Badge variant={pool.healthy === pool.total ? 'default' : 'destructive'}>
                          {pool.healthy}/{pool.total} healthy
                        </Badge>
                      </div>
                      <div className="flex gap-4 text-sm text-muted-foreground">
                        <span>{formatNumber(pool.requests)} req</span>
                        <span>{pool.avgTime}ms avg</span>
                        <span>{pool.errorRate}% errors</span>
                      </div>
                    </div>
                    <div className="h-2 rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-primary transition-all"
                        style={{ width: `${(pool.healthy / pool.total) * 100}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="saved" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Saved Reports</CardTitle>
              <CardDescription>Your scheduled and saved reports</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={mockSavedReports}
                columns={reportColumns}
                emptyMessage="No saved reports"
              />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
