import { useState, useEffect, useRef } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { toast } from 'sonner'
import { cn, formatDate } from '@/lib/utils'
import {
  Terminal,
  Play,
  Square,
  Trash2,
  Download,
  Filter,
  Wifi,
  WifiOff,
  AlertCircle,
  Info,
  CheckCircle2,
  XCircle,
  Clock,
  Zap,
  Settings,
  ChevronRight,
  ChevronDown,
  Maximize2,
  Minimize2
} from 'lucide-react'

interface LogEntry {
  id: string
  timestamp: Date
  level: 'debug' | 'info' | 'warn' | 'error' | 'fatal'
  source: string
  message: string
  metadata?: Record<string, any>
}

interface WSMessage {
  type: 'log' | 'event' | 'metric' | 'alert'
  data: any
  timestamp: number
}

const mockLogs: LogEntry[] = [
  {
    id: '1',
    timestamp: new Date(),
    level: 'info',
    source: 'proxy/http',
    message: 'Request received: GET /api/users'
  },
  {
    id: '2',
    timestamp: new Date(Date.now() - 1000),
    level: 'debug',
    source: 'balancer/round_robin',
    message: 'Selected backend: backend-01 (weight: 100)'
  },
  {
    id: '3',
    timestamp: new Date(Date.now() - 2000),
    level: 'info',
    source: 'proxy/http',
    message: 'Response: 200 OK (45ms)'
  },
  {
    id: '4',
    timestamp: new Date(Date.now() - 3000),
    level: 'warn',
    source: 'health/monitor',
    message: 'Backend backend-03 health check failed',
    metadata: { error: 'connection timeout', duration: '5.2s' }
  },
  {
    id: '5',
    timestamp: new Date(Date.now() - 4000),
    level: 'error',
    source: 'waf/detection',
    message: 'Potential SQL injection detected',
    metadata: { ip: '192.168.1.100', pattern: 'UNION SELECT' }
  }
]

const levelColors = {
  debug: 'text-gray-500',
  info: 'text-blue-500',
  warn: 'text-yellow-500',
  error: 'text-red-500',
  fatal: 'text-red-700 bg-red-500/10'
}

const levelIcons = {
  debug: Info,
  info: Info,
  warn: AlertCircle,
  error: XCircle,
  fatal: XCircle
}

export function WebSocketConsole() {
  const [logs, setLogs] = useState<LogEntry[]>(mockLogs)
  const [isConnected, setIsConnected] = useState(false)
  const [isPaused, setIsPaused] = useState(false)
  const [filterLevel, setFilterLevel] = useState<string>('all')
  const [filterSource, setFilterSource] = useState<string>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [showMetadata, setShowMetadata] = useState(true)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const logCounter = useRef(mockLogs.length + 1)

  useEffect(() => {
    if (!isConnected || isPaused) return

    const interval = setInterval(() => {
      const sources = ['proxy/http', 'balancer/round_robin', 'health/monitor', 'waf/detection', 'cache/lru']
      const levels: LogEntry['level'][] = ['debug', 'info', 'info', 'warn', 'error']
      const messages = [
        'Request processed successfully',
        'Backend selected',
        'Health check completed',
        'Cache hit',
        'Request completed in 23ms',
        'Rate limit check passed'
      ]

      const newLog: LogEntry = {
        id: String(logCounter.current++),
        timestamp: new Date(),
        level: levels[Math.floor(Math.random() * levels.length)],
        source: sources[Math.floor(Math.random() * sources.length)],
        message: messages[Math.floor(Math.random() * messages.length)]
      }

      setLogs(prev => [newLog, ...prev].slice(0, 1000))
    }, 1000)

    return () => clearInterval(interval)
  }, [isConnected, isPaused])

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = 0
    }
  }, [logs, autoScroll])

  const toggleConnection = () => {
    setIsConnected(!isConnected)
    toast.success(isConnected ? 'Disconnected' : 'Connected to WebSocket')
  }

  const clearLogs = () => {
    setLogs([])
    toast.success('Console cleared')
  }

  const exportLogs = () => {
    const content = JSON.stringify(logs, null, 2)
    const blob = new Blob([content], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `websocket-logs-${new Date().toISOString()}.json`
    a.click()
    URL.revokeObjectURL(url)
    toast.success('Logs exported')
  }

  const filteredLogs = logs.filter(log => {
    const matchesLevel = filterLevel === 'all' || log.level === filterLevel
    const matchesSource = filterSource === 'all' || log.source === filterSource
    const matchesSearch = log.message.toLowerCase().includes(searchQuery.toLowerCase()) ||
                         log.source.toLowerCase().includes(searchQuery.toLowerCase())
    return matchesLevel && matchesSource && matchesSearch
  })

  const uniqueSources = Array.from(new Set(logs.map(l => l.source)))

  const stats = {
    total: logs.length,
    error: logs.filter(l => l.level === 'error' || l.level === 'fatal').length,
    warn: logs.filter(l => l.level === 'warn').length,
    rate: isConnected ? '1,000/min' : '0/min'
  }

  return (
    <div className={cn('space-y-4', isFullscreen && 'fixed inset-0 z-50 bg-background p-4')}>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">WebSocket Console</h1>
          <p className="text-muted-foreground">
            Real-time log streaming via WebSocket
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant={isConnected ? 'default' : 'secondary'} className="gap-1">
            {isConnected ? <Wifi className="h-3 w-3" /> : <WifiOff className="h-3 w-3" />}
            {isConnected ? 'Connected' : 'Disconnected'}
          </Badge>
          <Button variant="outline" size="icon" onClick={() => setIsFullscreen(!isFullscreen)}>
            {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Logs</CardTitle>
            <Terminal className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.total.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Errors</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.error}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Warnings</CardTitle>
            <AlertCircle className="h-4 w-4 text-yellow-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-yellow-500">{stats.warn}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Rate</CardTitle>
            <Zap className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.rate}</div>
          </CardContent>
        </Card>
      </div>

      {/* Controls */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex flex-wrap items-center gap-4">
            <Button
              variant={isConnected ? 'destructive' : 'default'}
              onClick={toggleConnection}
            >
              {isConnected ? (
                <>
                  <Square className="mr-2 h-4 w-4" />
                  Disconnect
                </>
              ) : (
                <>
                  <Play className="mr-2 h-4 w-4" />
                  Connect
                </>
              )}
            </Button>

            <Button
              variant="outline"
              onClick={() => setIsPaused(!isPaused)}
              disabled={!isConnected}
            >
              {isPaused ? (
                <>
                  <Play className="mr-2 h-4 w-4" />
                  Resume
                </>
              ) : (
                <>
                  <Square className="mr-2 h-4 w-4" />
                  Pause
                </>
              )}
            </Button>

            <Button variant="outline" onClick={clearLogs}>
              <Trash2 className="mr-2 h-4 w-4" />
              Clear
            </Button>

            <Button variant="outline" onClick={exportLogs}>
              <Download className="mr-2 h-4 w-4" />
              Export
            </Button>

            <Separator orientation="vertical" className="h-8" />

            <div className="flex items-center gap-2">
              <Label>Level:</Label>
              <Select value={filterLevel} onValueChange={setFilterLevel}>
                <SelectTrigger className="w-[120px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All</SelectItem>
                  <SelectItem value="debug">Debug</SelectItem>
                  <SelectItem value="info">Info</SelectItem>
                  <SelectItem value="warn">Warn</SelectItem>
                  <SelectItem value="error">Error</SelectItem>
                  <SelectItem value="fatal">Fatal</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-2">
              <Label>Source:</Label>
              <Select value={filterSource} onValueChange={setFilterSource}>
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All</SelectItem>
                  {uniqueSources.map(source => (
                    <SelectItem key={source} value={source}>{source}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-2">
              <Switch
                checked={autoScroll}
                onCheckedChange={setAutoScroll}
                id="autoscroll"
              />
              <Label htmlFor="autoscroll">Auto-scroll</Label>
            </div>

            <div className="flex items-center gap-2">
              <Switch
                checked={showMetadata}
                onCheckedChange={setShowMetadata}
                id="metadata"
              />
              <Label htmlFor="metadata">Show metadata</Label>
            </div>

            <Input
              placeholder="Search logs..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-[200px]"
            />
          </div>
        </CardContent>
      </Card>

      {/* Console Output */}
      <Card className={cn(isFullscreen && 'flex-1')}>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Terminal className="h-5 w-5" />
            Console Output
            {isPaused && (
              <Badge variant="secondary">PAUSED</Badge>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ScrollArea
            ref={scrollRef}
            className={cn(
              'rounded-lg border bg-muted/50 font-mono text-sm',
              isFullscreen ? 'h-[calc(100vh-300px)]' : 'h-[500px]'
            )}
          >
            <div className="p-4 space-y-1">
              {filteredLogs.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-32 text-muted-foreground">
                  <Terminal className="h-8 w-8 mb-2 opacity-20" />
                  <p>No logs to display</p>
                  {!isConnected && <p className="text-xs">Connect to start streaming</p>}
                </div>
              ) : (
                filteredLogs.map((log, index) => {
                  const Icon = levelIcons[log.level]
                  return (
                    <div
                      key={log.id}
                      className={cn(
                        'flex gap-2 py-1 border-b border-border/50 last:border-0',
                        index % 2 === 0 && 'bg-muted/30'
                      )}
                    >
                      <span className="text-muted-foreground whitespace-nowrap">
                        {log.timestamp.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3 })}
                      </span>
                      <span className={cn('font-bold uppercase w-16', levelColors[log.level])}>
                        {log.level}
                      </span>
                      <span className="text-muted-foreground whitespace-nowrap">
                        [{log.source}]
                      </span>
                      <span className="flex-1">{log.message}</span>
                      {showMetadata && log.metadata && (
                        <span className="text-xs text-muted-foreground">
                          {JSON.stringify(log.metadata)}
                        </span>
                      )}
                    </div>
                  )
                })
              )}
            </div>
          </ScrollArea>
        </CardContent>
      </Card>
    </div>
  )
}
