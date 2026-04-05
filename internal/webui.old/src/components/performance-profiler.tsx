import { useState, useEffect, useRef } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { toast } from 'sonner'
import { cn, formatBytes, formatNumber, formatDuration } from '@/lib/utils'
import {
  Activity,
  Play,
  Square,
  RotateCcw,
  Download,
  Flame,
  Timer,
  Gauge,
  Cpu,
  MemoryStick,
  HardDrive,
  Network,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  BarChart3,
  Search,
  Filter
} from 'lucide-react'

interface ProfileSession {
  id: string
  name: string
  status: 'running' | 'completed' | 'failed'
  duration: number
  startTime: Date
  endTime?: Date
  samples: number
  hotspots: Hotspot[]
}

interface Hotspot {
  id: string
  function: string
  file: string
  line: number
  totalTime: number
  selfTime: number
  calls: number
  cpuPercent: number
}

interface MetricData {
  timestamp: number
  cpu: number
  memory: number
  goroutines: number
  gcTime: number
}

const mockHotspots: Hotspot[] = [
  { id: '1', function: 'handleRequest', file: 'proxy/http.go', line: 245, totalTime: 450, selfTime: 120, calls: 125000, cpuPercent: 15.2 },
  { id: '2', function: 'parseHeaders', file: 'http/parser.go', line: 89, totalTime: 380, selfTime: 95, calls: 250000, cpuPercent: 12.8 },
  { id: '3', function: 'matchRoute', file: 'router/tree.go', line: 156, totalTime: 320, selfTime: 80, calls: 125000, cpuPercent: 10.5 },
  { id: '4', function: 'loadBalance', file: 'balancer/round_robin.go', line: 78, totalTime: 280, selfTime: 65, calls: 125000, cpuPercent: 8.9 },
  { id: '5', function: 'checkHealth', file: 'health/monitor.go', line: 203, totalTime: 240, selfTime: 45, calls: 3600, cpuPercent: 7.2 },
  { id: '6', function: 'logRequest', file: 'middleware/logger.go', line: 112, totalTime: 200, selfTime: 40, calls: 125000, cpuPercent: 6.1 },
  { id: '7', function: 'validateToken', file: 'auth/jwt.go', line: 67, totalTime: 180, selfTime: 35, calls: 87500, cpuPercent: 5.4 },
  { id: '8', function: 'cacheLookup', file: 'cache/lru.go', line: 145, totalTime: 150, selfTime: 30, calls: 100000, cpuPercent: 4.8 }
]

function generateMetrics(duration: number): MetricData[] {
  const metrics: MetricData[] = []
  const now = Date.now()
  for (let i = 0; i < duration; i++) {
    metrics.push({
      timestamp: now - (duration - i) * 1000,
      cpu: 20 + Math.random() * 30,
      memory: 45 + Math.random() * 20,
      goroutines: 150 + Math.floor(Math.random() * 50),
      gcTime: Math.random() * 5
    })
  }
  return metrics
}

export function PerformanceProfiler() {
  const [activeTab, setActiveTab] = useState('cpu')
  const [isProfiling, setIsProfiling] = useState(false)
  const [profileDuration, setProfileDuration] = useState(30)
  const [elapsedTime, setElapsedTime] = useState(0)
  const [metrics, setMetrics] = useState<MetricData[]>([])
  const [selectedFunction, setSelectedFunction] = useState<Hotspot | null>(null)
  const [showFlameGraph, setShowFlameGraph] = useState(false)
  const intervalRef = useRef<NodeJS.Timeout | null>(null)

  useEffect(() => {
    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
      }
    }
  }, [])

  const startProfiling = () => {
    setIsProfiling(true)
    setElapsedTime(0)
    setMetrics([])
    toast.success('Performance profiling started')

    intervalRef.current = setInterval(() => {
      setElapsedTime(prev => {
        const next = prev + 1
        if (next >= profileDuration) {
          stopProfiling()
        }
        return next
      })
      setMetrics(prev => [...prev.slice(-60), {
        timestamp: Date.now(),
        cpu: 20 + Math.random() * 30,
        memory: 45 + Math.random() * 20,
        goroutines: 150 + Math.floor(Math.random() * 50),
        gcTime: Math.random() * 5
      }])
    }, 1000)
  }

  const stopProfiling = () => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current)
    }
    setIsProfiling(false)
    toast.success('Performance profiling completed')
  }

  const exportProfile = () => {
    toast.success('Profile exported as JSON')
  }

  const avgCpu = metrics.length > 0
    ? metrics.reduce((sum, m) => sum + m.cpu, 0) / metrics.length
    : 0

  const avgMemory = metrics.length > 0
    ? metrics.reduce((sum, m) => sum + m.memory, 0) / metrics.length
    : 0

  const maxGoroutines = metrics.length > 0
    ? Math.max(...metrics.map(m => m.goroutines))
    : 0

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Performance Profiler</h1>
          <p className="text-muted-foreground">
            Analyze runtime performance and identify bottlenecks
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={String(profileDuration)} onValueChange={(v) => setProfileDuration(Number(v))} disabled={isProfiling}>
            <SelectTrigger className="w-[140px]">
              <Timer className="mr-2 h-4 w-4" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="10">10 seconds</SelectItem>
              <SelectItem value="30">30 seconds</SelectItem>
              <SelectItem value="60">1 minute</SelectItem>
              <SelectItem value="300">5 minutes</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={exportProfile} disabled={metrics.length === 0}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
          {isProfiling ? (
            <Button variant="destructive" onClick={stopProfiling}>
              <Square className="mr-2 h-4 w-4" />
              Stop
            </Button>
          ) : (
            <Button onClick={startProfiling}>
              <Play className="mr-2 h-4 w-4" />
              Start Profiling
            </Button>
          )}
        </div>
      </div>

      {isProfiling && (
        <Card className="border-primary/50">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between mb-4">
              <div className="flex items-center gap-2">
                <div className="h-3 w-3 animate-pulse rounded-full bg-red-500" />
                <span className="font-medium">Profiling in progress</span>
              </div>
              <span className="text-sm text-muted-foreground">
                {elapsedTime}s / {profileDuration}s
              </span>
            </div>
            <Progress value={(elapsedTime / profileDuration) * 100} />
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
            <Cpu className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{avgCpu.toFixed(1)}%</div>
            <p className="text-xs text-muted-foreground">Average over session</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Memory</CardTitle>
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{avgMemory.toFixed(1)}%</div>
            <p className="text-xs text-muted-foreground">Heap utilization</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Goroutines</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatNumber(maxGoroutines)}</div>
            <p className="text-xs text-muted-foreground">Peak concurrent</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">GC Time</CardTitle>
            <Timer className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">0.8%</div>
            <p className="text-xs text-muted-foreground">Garbage collection</p>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="cpu">CPU Profile</TabsTrigger>
          <TabsTrigger value="memory">Memory</TabsTrigger>
          <TabsTrigger value="goroutines">Goroutines</TabsTrigger>
          <TabsTrigger value="hotspots">Hotspots</TabsTrigger>
          <TabsTrigger value="timeline">Timeline</TabsTrigger>
        </TabsList>

        <TabsContent value="cpu" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>CPU Usage Over Time</CardTitle>
              <CardDescription>Real-time CPU utilization</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="h-[300px] relative">
                {metrics.length > 0 ? (
                  <div className="absolute inset-0 flex items-end gap-px">
                    {metrics.map((m, i) => (
                      <div
                        key={i}
                        className="flex-1 bg-primary/80 rounded-t-sm transition-all"
                        style={{ height: `${m.cpu}%` }}
                      />
                    ))}
                  </div>
                ) : (
                  <div className="flex h-full items-center justify-center text-muted-foreground">
                    <Activity className="mr-2 h-5 w-5" />
                    Start profiling to see CPU data
                  </div>
                )}
                <div className="absolute left-0 top-0 bottom-0 w-px bg-border" />
                <div className="absolute left-0 right-0 bottom-0 h-px bg-border" />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="memory" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Memory Allocation</CardTitle>
              <CardDescription>Heap memory usage over time</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="h-[300px] relative">
                {metrics.length > 0 ? (
                  <div className="absolute inset-0 flex items-end gap-px">
                    {metrics.map((m, i) => (
                      <div
                        key={i}
                        className="flex-1 bg-emerald-500/80 rounded-t-sm transition-all"
                        style={{ height: `${m.memory}%` }}
                      />
                    ))}
                  </div>
                ) : (
                  <div className="flex h-full items-center justify-center text-muted-foreground">
                    <MemoryStick className="mr-2 h-5 w-5" />
                    Start profiling to see memory data
                  </div>
                )}
              </div>
            </CardContent>
          </Card>

          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Memory Breakdown</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  {[
                    { name: 'Heap In Use', value: 45.2, color: 'bg-emerald-500' },
                    { name: 'Stack', value: 8.5, color: 'bg-blue-500' },
                    { name: 'GC Metadata', value: 12.3, color: 'bg-yellow-500' },
                    { name: 'Other', value: 34.0, color: 'bg-gray-400' }
                  ].map(item => (
                    <div key={item.name} className="space-y-1">
                      <div className="flex items-center justify-between text-sm">
                        <span>{item.name}</span>
                        <span>{item.value}%</span>
                      </div>
                      <div className="h-2 rounded-full bg-muted">
                        <div className={cn('h-full rounded-full transition-all', item.color)} style={{ width: `${item.value}%` }} />
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Allocation Sites</CardTitle>
              </CardHeader>
              <CardContent>
                <ScrollArea className="h-[200px]">
                  <div className="space-y-2">
                    {mockHotspots.slice(0, 5).map(hotspot => (
                      <div key={hotspot.id} className="flex items-center justify-between p-2 rounded-lg hover:bg-muted">
                        <div>
                          <p className="font-medium text-sm">{hotspot.function}</p>
                          <p className="text-xs text-muted-foreground">{hotspot.file}:{hotspot.line}</p>
                        </div>
                        <span className="text-sm font-medium">{formatBytes(hotspot.totalTime * 1024)}</span>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="goroutines" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Goroutine Count</CardTitle>
              <CardDescription>Active goroutines over time</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="h-[300px] relative">
                {metrics.length > 0 ? (
                  <svg className="w-full h-full">
                    <polyline
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      points={metrics.map((m, i) => {
                        const x = (i / (metrics.length - 1)) * 100
                        const y = 100 - ((m.goroutines - 100) / 100) * 100
                        return `${x},${y}`
                      }).join(' ')}
                      className="text-primary"
                    />
                  </svg>
                ) : (
                  <div className="flex h-full items-center justify-center text-muted-foreground">
                    Start profiling to see goroutine data
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="hotspots" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle>Performance Hotspots</CardTitle>
                <CardDescription>Functions consuming the most CPU time</CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={() => setShowFlameGraph(!showFlameGraph)}>
                  <Flame className="mr-2 h-4 w-4" />
                  {showFlameGraph ? 'List View' : 'Flame Graph'}
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {showFlameGraph ? (
                <div className="h-[400px] flex flex-col gap-1">
                  {mockHotspots.map((hotspot, i) => (
                    <div
                      key={hotspot.id}
                      className="flex items-center gap-2 rounded px-2 py-1 text-sm cursor-pointer hover:opacity-80 transition-opacity"
                      style={{
                        backgroundColor: `hsl(${240 - i * 30}, 70%, 50%)`,
                        width: `${(hotspot.cpuPercent / mockHotspots[0].cpuPercent) * 100}%`
                      }}
                      onClick={() => setSelectedFunction(hotspot)}
                    >
                      <span className="truncate text-white font-medium">{hotspot.function}</span>
                      <span className="text-white/80 ml-auto">{hotspot.cpuPercent}%</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="space-y-2">
                  {mockHotspots.map((hotspot, i) => (
                    <div
                      key={hotspot.id}
                      className={cn(
                        'flex items-center gap-4 p-3 rounded-lg border cursor-pointer transition-colors',
                        selectedFunction?.id === hotspot.id ? 'bg-muted border-primary' : 'hover:bg-muted/50'
                      )}
                      onClick={() => setSelectedFunction(hotspot)}
                    >
                      <span className="text-muted-foreground w-6">#{i + 1}</span>
                      <div className="flex-1 min-w-0">
                        <p className="font-medium truncate">{hotspot.function}</p>
                        <p className="text-sm text-muted-foreground truncate">{hotspot.file}:{hotspot.line}</p>
                      </div>
                      <div className="text-right">
                        <p className="font-medium">{hotspot.cpuPercent}%</p>
                        <p className="text-sm text-muted-foreground">{formatNumber(hotspot.calls)} calls</p>
                      </div>
                      <div className="w-24">
                        <div className="h-2 rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-primary transition-all"
                            style={{ width: `${(hotspot.cpuPercent / mockHotspots[0].cpuPercent) * 100}%` }}
                          />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {selectedFunction && (
            <Card>
              <CardHeader>
                <CardTitle>Function Details</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">Function</p>
                    <p className="font-medium">{selectedFunction.function}</p>
                  </div>
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">Location</p>
                    <p className="font-medium">{selectedFunction.file}:{selectedFunction.line}</p>
                  </div>
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">Total Time</p>
                    <p className="font-medium">{formatDuration(selectedFunction.totalTime)}</p>
                  </div>
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">Self Time</p>
                    <p className="font-medium">{formatDuration(selectedFunction.selfTime)}</p>
                  </div>
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">Calls</p>
                    <p className="font-medium">{formatNumber(selectedFunction.calls)}</p>
                  </div>
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">CPU Usage</p>
                    <p className="font-medium">{selectedFunction.cpuPercent}%</p>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="timeline" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Execution Timeline</CardTitle>
              <CardDescription>Trace execution flow and timing</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {[
                  { time: '0ms', event: 'Request received', duration: 0, type: 'info' },
                  { time: '0.1ms', event: 'Parse headers', duration: 0.2, type: 'success' },
                  { time: '0.3ms', event: 'Route match', duration: 0.1, type: 'success' },
                  { time: '0.4ms', event: 'Apply middleware', duration: 0.5, type: 'info' },
                  { time: '0.9ms', event: 'Rate limit check', duration: 0.1, type: 'success' },
                  { time: '1.0ms', event: 'Auth validation', duration: 0.8, type: 'warning' },
                  { time: '1.8ms', event: 'Load balance', duration: 0.2, type: 'success' },
                  { time: '2.0ms', event: 'Backend request', duration: 18.5, type: 'info' },
                  { time: '20.5ms', event: 'Response processing', duration: 0.5, type: 'success' },
                  { time: '21.0ms', event: 'Request complete', duration: 0, type: 'success' }
                ].map((item, i) => (
                  <div key={i} className="flex items-center gap-4">
                    <span className="text-sm text-muted-foreground w-16">{item.time}</span>
                    <div className={cn(
                      'w-3 h-3 rounded-full',
                      item.type === 'info' && 'bg-blue-500',
                      item.type === 'success' && 'bg-green-500',
                      item.type === 'warning' && 'bg-yellow-500'
                    )} />
                    <div className="flex-1">
                      <span className="font-medium">{item.event}</span>
                    </div>
                    {item.duration > 0 && (
                      <span className="text-sm text-muted-foreground">+{item.duration}ms</span>
                    )}
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
