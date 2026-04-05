import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { SimpleLineChart, SimplePieChart } from '@/components/charts'
import {
  Server,
  Layers,
  Route,
  Radio,
  Activity,
  TrendingUp,
  Users,
  AlertTriangle,
  RefreshCw,
  Wifi,
  WifiOff,
  PlayCircle,
  Keyboard,
  Settings2
} from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import { NetworkStatusBadge } from '@/components/network-status'
import { KeyboardShortcutsDialog, useKeyboardShortcutsDialog } from '@/components/keyboard-shortcuts-dialog'
import { TourGuide, dashboardTour, useTour } from '@/components/tour-guide'
import { WidgetCustomizer, useDashboardWidgets, defaultWidgets } from '@/components/widget-customizer'
import type { Backend, Pool, Listener, Metrics } from '@/types'

// Mock data for charts - replace with real data from API
const mockTrafficData = [
  { label: '00:00', value: 120 },
  { label: '04:00', value: 80 },
  { label: '08:00', value: 450 },
  { label: '12:00', value: 680 },
  { label: '16:00', value: 520 },
  { label: '20:00', value: 380 },
  { label: '23:59', value: 200 }
]

const mockStatusCodeData = [
  { label: '200 OK', value: 8500 },
  { label: '201 Created', value: 1200 },
  { label: '301 Redirect', value: 300 },
  { label: '404 Not Found', value: 150 },
  { label: '500 Error', value: 50 }
]

export function DashboardPage() {
  const { open: shortcutsOpen, setOpen: setShortcutsOpen } = useKeyboardShortcutsDialog()
  const { isOpen: tourOpen, setIsOpen: setTourOpen, startTour, hasCompletedTour } = useTour('dashboard')
  const { widgets, setWidgets, customizerOpen, openCustomizer, closeCustomizer } = useDashboardWidgets()

  const visibleWidgets = widgets.filter(w => w.visible)
  const isStatsVisible = visibleWidgets.find(w => w.id === 'stats')?.visible ?? true
  const { data: backends = [], isLoading: backendsLoading } = useQuery<Backend[]>({
    queryKey: ['backends'],
    queryFn: async () => {
      const response = await api.get('/api/v1/backends')
      return response.data
    }
  })

  const { data: pools = [], isLoading: poolsLoading } = useQuery<Pool[]>({
    queryKey: ['pools'],
    queryFn: async () => {
      const response = await api.get('/api/v1/pools')
      return response.data
    }
  })

  const { data: listeners = [], isLoading: listenersLoading } = useQuery<Listener[]>({
    queryKey: ['listeners'],
    queryFn: async () => {
      const response = await api.get('/api/v1/listeners')
      return response.data
    }
  })

  const { data: metrics, isLoading: metricsLoading } = useQuery<Metrics>({
    queryKey: ['metrics'],
    queryFn: async () => {
      const response = await api.get('/api/v1/metrics')
      return response.data
    },
    refetchInterval: 5000
  })

  const isLoading = backendsLoading || poolsLoading || listenersLoading || metricsLoading

  const healthyBackends = backends.filter((b) => b.status === 'up').length
  const unhealthyBackends = backends.filter((b) => b.status === 'down').length

  const stats = [
    {
      title: 'Backends',
      value: backends.length,
      healthy: healthyBackends,
      unhealthy: unhealthyBackends,
      icon: Server,
      color: 'text-blue-500',
      bgColor: 'bg-blue-500/10',
      tourTarget: 'stats'
    },
    {
      title: 'Pools',
      value: pools.length,
      icon: Layers,
      color: 'text-green-500',
      bgColor: 'bg-green-500/10'
    },
    {
      title: 'Listeners',
      value: listeners.length,
      icon: Radio,
      color: 'text-purple-500',
      bgColor: 'bg-purple-500/10'
    },
    {
      title: 'Routes',
      value: listeners.reduce((acc, l) => acc + (l.routes?.length || 0), 0),
      icon: Route,
      color: 'text-orange-500',
      bgColor: 'bg-orange-500/10',
      tourTarget: 'charts'
    }
  ]

  const handleRefresh = () => {
    toast.info('Refreshing dashboard...')
    window.location.reload()
  }

  return (
    <div className="space-y-6">
      {/* Tour Guide */}
      <TourGuide
        steps={dashboardTour}
        isOpen={tourOpen}
        onClose={() => setTourOpen(false)}
        onComplete={() => toast.success('Tour completed! You\'re ready to use OpenLoadBalancer.')}
      />

      {/* Keyboard Shortcuts Dialog */}
      <KeyboardShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />

      {/* Widget Customizer */}
      <WidgetCustomizer
        open={customizerOpen}
        onOpenChange={closeCustomizer}
        widgets={widgets}
        onWidgetsChange={setWidgets}
      />

      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">
            Overview of your load balancer configuration and metrics
          </p>
        </div>
        <div className="flex items-center gap-2">
          {!hasCompletedTour && (
            <Button variant="outline" onClick={startTour}>
              <PlayCircle className="mr-2 h-4 w-4" />
              Take Tour
            </Button>
          )}
          <Button variant="outline" size="icon" onClick={openCustomizer} title="Customize widgets">
            <Settings2 className="h-4 w-4" />
          </Button>
          <Button variant="outline" size="icon" onClick={() => setShortcutsOpen(true)} title="Keyboard shortcuts (?)" >
            <Keyboard className="h-4 w-4" />
          </Button>
          <NetworkStatusBadge showLabel />
          <Button onClick={handleRefresh} variant="outline">
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Stats Grid */}
      {isStatsVisible && (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4" data-tour="stats">
          {isLoading
            ? Array.from({ length: 4 }).map((_, i) => (
                <Card key={i}>
                  <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                    <Skeleton className="h-4 w-[100px]" />
                    <Skeleton className="h-8 w-8 rounded-lg" />
                  </CardHeader>
                  <CardContent>
                    <Skeleton className="h-8 w-[60px]" />
                  </CardContent>
                </Card>
              ))
            : stats.map((stat) => {
                const Icon = stat.icon
                return (
                  <Card key={stat.title}>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                      <CardTitle className="text-sm font-medium">{stat.title}</CardTitle>
                      <div className={`${stat.bgColor} rounded-lg p-2`}>
                        <Icon className={`h-4 w-4 ${stat.color}`} />
                      </div>
                    </CardHeader>
                    <CardContent>
                      <div className="text-2xl font-bold">{stat.value}</div>
                      {stat.healthy !== undefined && stat.unhealthy !== undefined && (
                        <div className="mt-2 flex items-center gap-2 text-xs">
                          <Badge variant="success" className="text-xs">
                            {stat.healthy} healthy
                          </Badge>
                          {stat.unhealthy > 0 && (
                            <Badge variant="destructive" className="text-xs">
                              {stat.unhealthy} down
                            </Badge>
                          )}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                )
              })}
        </div>
      )}

      {/* Charts Row */}
      {(visibleWidgets.find(w => w.id === 'traffic')?.visible ||
        visibleWidgets.find(w => w.id === 'status')?.visible ||
        visibleWidgets.find(w => w.id === 'load')?.visible) && (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3" data-tour="charts">
          {visibleWidgets.find(w => w.id === 'traffic')?.visible && (
            <SimpleLineChart
              title="Traffic Overview"
              description="Requests per minute over the last 24 hours"
              data={mockTrafficData}
              isLoading={isLoading}
              color="#3b82f6"
              showArea
            />
          )}

          {visibleWidgets.find(w => w.id === 'status')?.visible && (
            <SimplePieChart
              title="Response Codes"
              description="Distribution of HTTP status codes"
              data={mockStatusCodeData}
              isLoading={isLoading}
            />
          )}

          {visibleWidgets.find(w => w.id === 'load')?.visible && (
            <Card>
              <CardHeader>
                <CardTitle>Current Load</CardTitle>
                <CardDescription>Real-time metrics</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                {metricsLoading ? (
                  <>
                    <Skeleton className="h-6 w-full" />
                    <Skeleton className="h-6 w-full" />
                    <Skeleton className="h-6 w-full" />
                  </>
                ) : (
                  <>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <Activity className="h-4 w-4 text-muted-foreground" />
                        <span className="text-sm">Requests/sec</span>
                      </div>
                      <span className="font-mono font-medium">
                        {metrics?.requests_per_second?.toFixed(1) || '0.0'}
                      </span>
                    </div>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <Users className="h-4 w-4 text-muted-foreground" />
                        <span className="text-sm">Active Connections</span>
                      </div>
                      <span className="font-mono font-medium">
                        {metrics?.active_connections || 0}
                      </span>
                    </div>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <TrendingUp className="h-4 w-4 text-muted-foreground" />
                        <span className="text-sm">Throughput</span>
                      </div>
                      <span className="font-mono font-medium">
                        {formatBytes(metrics?.bytes_per_second || 0)}/s
                      </span>
                    </div>
                  </>
                )}
              </CardContent>
            </Card>
          )}
        </div>
      )}

      {/* Alerts */}
      {!isLoading && unhealthyBackends > 0 && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>Health Alerts</AlertTitle>
          <AlertDescription>
            {unhealthyBackends} backend{unhealthyBackends > 1 ? 's are' : ' is'} currently down.
            Check the Backends page for details.
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}
