import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { toast } from 'sonner'
import { cn, formatDuration, formatBytes } from '@/lib/utils'
import {
  Activity,
  Heart,
  HeartPulse,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Clock,
  TrendingUp,
  TrendingDown,
  Server,
  Globe,
  Zap,
  RefreshCw,
  ArrowUpRight,
  ArrowDownRight,
  Minus
} from 'lucide-react'

interface HealthCheck {
  id: string
  name: string
  target: string
  type: 'http' | 'tcp' | 'grpc'
  interval: number
  timeout: number
  healthyThreshold: number
  unhealthyThreshold: number
  lastCheck?: Date
  status: 'healthy' | 'unhealthy' | 'unknown'
  uptime: number
  responseTime: number
  history: { timestamp: Date; status: 'healthy' | 'unhealthy'; responseTime: number }[]
}

const mockHealthChecks: HealthCheck[] = [
  {
    id: '1',
    name: 'Web Backend Health',
    target: 'web-pool',
    type: 'http',
    interval: 5,
    timeout: 3,
    healthyThreshold: 2,
    unhealthyThreshold: 3,
    lastCheck: new Date(Date.now() - 30000),
    status: 'healthy',
    uptime: 99.98,
    responseTime: 23,
    history: Array.from({ length: 20 }, (_, i) => ({
      timestamp: new Date(Date.now() - (19 - i) * 60000),
      status: Math.random() > 0.9 ? 'unhealthy' : 'healthy',
      responseTime: 20 + Math.random() * 15
    }))
  },
  {
    id: '2',
    name: 'API Backend Health',
    target: 'api-pool',
    type: 'http',
    interval: 5,
    timeout: 5,
    healthyThreshold: 2,
    unhealthyThreshold: 3,
    lastCheck: new Date(Date.now() - 45000),
    status: 'healthy',
    uptime: 99.95,
    responseTime: 45,
    history: Array.from({ length: 20 }, (_, i) => ({
      timestamp: new Date(Date.now() - (19 - i) * 60000),
      status: Math.random() > 0.85 ? 'unhealthy' : 'healthy',
      responseTime: 40 + Math.random() * 25
    }))
  },
  {
    id: '3',
    name: 'Database Backend',
    target: 'db-pool',
    type: 'tcp',
    interval: 10,
    timeout: 2,
    healthyThreshold: 1,
    unhealthyThreshold: 2,
    lastCheck: new Date(Date.now() - 120000),
    status: 'unhealthy',
    uptime: 98.5,
    responseTime: 0,
    history: Array.from({ length: 20 }, (_, i) => ({
      timestamp: new Date(Date.now() - (19 - i) * 60000),
      status: i < 3 ? 'unhealthy' : 'healthy',
      responseTime: i < 3 ? 0 : 5 + Math.random() * 5
    }))
  }
]

export function HealthMonitor() {
  const [checks, setChecks] = useState<HealthCheck[]>(mockHealthChecks)
  const [activeTab, setActiveTab] = useState('all')
  const [refreshing, setRefreshing] = useState(false)
  const [selectedCheck, setSelectedCheck] = useState<HealthCheck | null>(null)

  const refreshHealth = async () => {
    setRefreshing(true)
    await new Promise(resolve => setTimeout(resolve, 1000))
    setRefreshing(false)
    toast.success('Health status refreshed')
  }

  const filteredChecks = checks.filter(c => {
    if (activeTab === 'healthy') return c.status === 'healthy'
    if (activeTab === 'unhealthy') return c.status === 'unhealthy'
    return true
  })

  const stats = {
    total: checks.length,
    healthy: checks.filter(c => c.status === 'healthy').length,
    unhealthy: checks.filter(c => c.status === 'unhealthy').length,
    avgUptime: checks.reduce((sum, c) => sum + c.uptime, 0) / checks.length
  }

  const getStatusColor = (status: HealthCheck['status']) => {
    switch (status) {
      case 'healthy':
        return 'text-green-500 bg-green-500/10 border-green-500/20'
      case 'unhealthy':
        return 'text-red-500 bg-red-500/10 border-red-500/20'
      default:
        return 'text-gray-500 bg-gray-500/10 border-gray-500/20'
    }
  }

  const getStatusIcon = (status: HealthCheck['status']) => {
    switch (status) {
      case 'healthy':
        return <Heart className="h-5 w-5 text-green-500" />
      case 'unhealthy':
        return <HeartPulse className="h-5 w-5 text-red-500" />
      default:
        return <Activity className="h-5 w-5 text-gray-500" />
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Health Monitor</h1>
          <p className="text-muted-foreground">
            Monitor backend health and check status
          </p>
        </div>
        <Button onClick={refreshHealth} disabled={refreshing}>
          <RefreshCw className={cn('mr-2 h-4 w-4', refreshing && 'animate-spin')} />
          Refresh
        </Button>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Health Checks</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.total}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Healthy</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{stats.healthy}</div>
            <p className="text-xs text-muted-foreground">{((stats.healthy / stats.total) * 100).toFixed(0)}% passing</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Unhealthy</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.unhealthy}</div>
            <p className="text-xs text-muted-foreground">Require attention</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Avg Uptime</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.avgUptime.toFixed(2)}%</div>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="all">All Checks</TabsTrigger>
          <TabsTrigger value="healthy">Healthy</TabsTrigger>
          <TabsTrigger value="unhealthy">Unhealthy</TabsTrigger>
        </TabsList>

        <TabsContent value={activeTab} className="space-y-4">
          <div className="grid gap-4">
            {filteredChecks.map(check => (
              <Card
                key={check.id}
                className={cn('cursor-pointer transition-colors hover:bg-muted/50', getStatusColor(check.status))}
                onClick={() => setSelectedCheck(check)}
              >
                <CardContent className="p-4">
                  <div className="flex items-start justify-between">
                    <div className="flex items-start gap-4">
                      <div className="mt-1">{getStatusIcon(check.status)}</div>
                      <div>
                        <h3 className="font-medium">{check.name}</h3>
                        <p className="text-sm text-muted-foreground">{check.target} • {check.type.toUpperCase()}</p>
                        <div className="mt-2 flex items-center gap-4 text-sm">
                          <span className="flex items-center gap-1">
                            <Clock className="h-3.5 w-3.5" />
                            {check.interval}s interval
                          </span>
                          <span className="flex items-center gap-1">
                            <Zap className="h-3.5 w-3.5" />
                            {check.timeout}s timeout
                          </span>
                          {check.lastCheck && (
                            <span className="flex items-center gap-1">
                              <Activity className="h-3.5 w-3.5" />
                              Last check {formatDuration(Date.now() - check.lastCheck.getTime())} ago
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="text-right">
                      <div className="flex items-center gap-2 justify-end">
                        <span className="text-2xl font-bold">{check.uptime.toFixed(2)}%</span>
                        <span className="text-xs text-muted-foreground">uptime</span>
                      </div>
                      {check.responseTime > 0 && (
                        <p className="text-sm text-muted-foreground">
                          {check.responseTime}ms response
                        </p>
                      )}
                    </div>
                  </div>

                  {/* Mini history chart */}
                  <div className="mt-4 flex items-center gap-1">
                    <span className="text-xs text-muted-foreground mr-2">Last 20 checks:</span>
                    {check.history.slice(-20).map((h, i) => (
                      <div
                        key={i}
                        className={cn(
                          'w-3 h-6 rounded-sm',
                          h.status === 'healthy' ? 'bg-green-500' : 'bg-red-500'
                        )}
                        title={`${h.status} - ${h.responseTime > 0 ? h.responseTime + 'ms' : 'timeout'}`}
                      />
                    ))}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>
      </Tabs>

      {/* Detail Modal */}
      {selectedCheck && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setSelectedCheck(null)}>
          <Card className="w-full max-w-3xl m-4" onClick={(e) => e.stopPropagation()}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  {getStatusIcon(selectedCheck.status)}
                  <div>
                    <CardTitle>{selectedCheck.name}</CardTitle>
                    <CardDescription>{selectedCheck.target}</CardDescription>
                  </div>
                </div>
                <Badge variant={selectedCheck.status === 'healthy' ? 'default' : 'destructive'} className="capitalize">
                  {selectedCheck.status}
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Check Type</Label>
                  <p className="font-medium uppercase">{selectedCheck.type}</p>
                </div>
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Interval</Label>
                  <p className="font-medium">{selectedCheck.interval} seconds</p>
                </div>
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Timeout</Label>
                  <p className="font-medium">{selectedCheck.timeout} seconds</p>
                </div>
                <div className="space-y-2">
                  <Label className="text-muted-foreground">Uptime</Label>
                  <p className="font-medium">{selectedCheck.uptime.toFixed(3)}%</p>
                </div>
              </div>

              <div>
                <Label className="text-muted-foreground mb-2 block">Response History</Label>
                <div className="h-40 flex items-end gap-1">
                  {selectedCheck.history.slice(-50).map((h, i) => (
                    <div
                      key={i}
                      className="flex-1 flex flex-col justify-end"
                    >
                      <div
                        className={cn(
                          'w-full rounded-sm transition-all',
                          h.status === 'healthy' ? 'bg-green-500' : 'bg-red-500'
                        )}
                        style={{ height: `${(h.responseTime / 100) * 100}%`, minHeight: h.status === 'healthy' ? '4px' : '100%' }}
                      />
                    </div>
                  ))}
                </div>
              </div>

              <div>
                <Label className="text-muted-foreground mb-2 block">Check Log</Label>
                <ScrollArea className="h-40 rounded-lg border bg-muted/50 p-2">
                  <div className="space-y-1 font-mono text-sm">
                    {selectedCheck.history.slice(-10).reverse().map((h, i) => (
                      <div key={i} className="flex items-center gap-2">
                        <span className="text-muted-foreground">
                          {h.timestamp.toLocaleTimeString()}
                        </span>
                        <Badge variant={h.status === 'healthy' ? 'outline' : 'destructive'} className="text-xs">
                          {h.status}
                        </Badge>
                        {h.responseTime > 0 && (
                          <span className="text-muted-foreground">{h.responseTime}ms</span>
                        )}
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}

import { Label } from '@/components/ui/label'
