import { useState, useEffect, useRef, useCallback } from 'react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useWebSocket } from '@/hooks/websocket'
import { cn } from '@/lib/utils'
import {
  Play,
  Pause,
  Trash2,
  Download,
  Filter,
  Search,
  AlertCircle,
  Info,
  AlertTriangle,
  X,
  Terminal,
  RefreshCw
} from 'lucide-react'

interface LogEntry {
  id: string
  timestamp: Date
  level: 'debug' | 'info' | 'warn' | 'error'
  source: string
  message: string
  metadata?: Record<string, unknown>
}

interface LogViewerProps {
  className?: string
}

const levelColors = {
  debug: 'text-muted-foreground',
  info: 'text-blue-500',
  warn: 'text-amber-500',
  error: 'text-destructive'
}

const levelBgColors = {
  debug: 'bg-muted',
  info: 'bg-blue-500/10',
  warn: 'bg-amber-500/10',
  error: 'bg-destructive/10'
}

const levelIcons = {
  debug: Terminal,
  info: Info,
  warn: AlertTriangle,
  error: AlertCircle
}

// Generate mock logs
function generateMockLog(): LogEntry {
  const levels: LogEntry['level'][] = ['debug', 'info', 'warn', 'error']
  const sources = ['proxy', 'balancer', 'health', 'waf', 'tls', 'admin']
  const messages = [
    'Request processed successfully',
    'Backend health check failed',
    'Rate limit exceeded',
    'Certificate renewed',
    'Connection established',
    'Request timeout',
    'Config reloaded',
    'New backend registered',
    'Pool updated',
    'WAF rule triggered'
  ]

  const level = levels[Math.floor(Math.random() * levels.length)]
  const source = sources[Math.floor(Math.random() * sources.length)]
  const message = messages[Math.floor(Math.random() * messages.length)]

  return {
    id: Math.random().toString(36).substr(2, 9),
    timestamp: new Date(),
    level,
    source,
    message,
    metadata: { ip: '192.168.1.' + Math.floor(Math.random() * 255) }
  }
}

export function RealtimeLogViewer({ className }: LogViewerProps) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [isPlaying, setIsPlaying] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedLevel, setSelectedLevel] = useState<LogEntry['level'] | 'all'>('all')
  const [selectedSource, setSelectedSource] = useState<string>('all')
  const [autoScroll, setAutoScroll] = useState(true)
  const [showMetadata, setShowMetadata] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)

  // Generate mock logs periodically
  useEffect(() => {
    if (!isPlaying) return

    const interval = setInterval(() => {
      const newLog = generateMockLog()
      setLogs(prev => [...prev.slice(-999), newLog])
    }, 500 + Math.random() * 1000)

    return () => clearInterval(interval)
  }, [isPlaying])

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, autoScroll])

  // Get unique sources
  const sources = useMemo(() => {
    const unique = new Set(logs.map(l => l.source))
    return Array.from(unique).sort()
  }, [logs])

  // Filter logs
  const filteredLogs = useMemo(() => {
    return logs.filter(log => {
      const matchesLevel = selectedLevel === 'all' || log.level === selectedLevel
      const matchesSource = selectedSource === 'all' || log.source === selectedSource
      const matchesSearch = searchQuery === '' ||
        log.message.toLowerCase().includes(searchQuery.toLowerCase()) ||
        log.source.toLowerCase().includes(searchQuery.toLowerCase())
      return matchesLevel && matchesSource && matchesSearch
    })
  }, [logs, selectedLevel, selectedSource, searchQuery])

  // Stats
  const stats = useMemo(() => {
    return {
      total: logs.length,
      error: logs.filter(l => l.level === 'error').length,
      warn: logs.filter(l => l.level === 'warn').length,
      info: logs.filter(l => l.level === 'info').length,
      debug: logs.filter(l => l.level === 'debug').length
    }
  }, [logs])

  const handleClear = () => {
    setLogs([])
  }

  const handleExport = () => {
    const data = JSON.stringify(filteredLogs, null, 2)
    const blob = new Blob([data], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `logs-${new Date().toISOString()}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  const togglePlay = () => {
    setIsPlaying(!isPlaying)
  }

  return (
    <Card className={cn('h-[600px]', className)}>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Terminal className="h-5 w-5" />
              Real-time Logs
            </CardTitle>
            <CardDescription>
              Live log stream from all services
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="icon" onClick={togglePlay}>
              {isPlaying ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
            </Button>
            <Button variant="outline" size="icon" onClick={handleClear}>
              <Trash2 className="h-4 w-4" />
            </Button>
            <Button variant="outline" size="icon" onClick={handleExport}>
              <Download className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Stats */}
        <div className="flex gap-2">
          <Badge variant="secondary">Total: {stats.total}</Badge>
          <Badge variant="destructive">Errors: {stats.error}</Badge>
          <Badge className="bg-amber-500/10 text-amber-500">Warnings: {stats.warn}</Badge>
          <Badge className="bg-blue-500/10 text-blue-500">Info: {stats.info}</Badge>
        </div>

        {/* Filters */}
        <div className="flex flex-wrap gap-2">
          <div className="relative flex-1 min-w-[200px]">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search logs..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9"
            />
            {searchQuery && (
              <Button
                variant="ghost"
                size="icon"
                className="absolute right-0 top-0 h-9 w-9"
                onClick={() => setSearchQuery('')}
              >
                <X className="h-4 w-4" />
              </Button>
            )}
          </div>
          <Select value={selectedLevel} onValueChange={(v) => setSelectedLevel(v as LogEntry['level'] | 'all')}>
            <SelectTrigger className="w-[130px]">
              <Filter className="mr-2 h-4 w-4" />
              <SelectValue placeholder="Level" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Levels</SelectItem>
              <SelectItem value="error">Error</SelectItem>
              <SelectItem value="warn">Warning</SelectItem>
              <SelectItem value="info">Info</SelectItem>
              <SelectItem value="debug">Debug</SelectItem>
            </SelectContent>
          </Select>
          <Select value={selectedSource} onValueChange={setSelectedSource}>
            <SelectTrigger className="w-[130px]">
              <SelectValue placeholder="Source" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Sources</SelectItem>
              {sources.map(source => (
                <SelectItem key={source} value={source}>{source}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="flex items-center gap-2">
            <Switch checked={autoScroll} onCheckedChange={setAutoScroll} id="autoscroll" />
            <label htmlFor="autoscroll" className="text-sm">Auto-scroll</label>
          </div>
          <div className="flex items-center gap-2">
            <Switch checked={showMetadata} onCheckedChange={setShowMetadata} id="metadata" />
            <label htmlFor="metadata" className="text-sm">Metadata</label>
          </div>
        </div>

        {/* Log Stream */}
        <ScrollArea className="h-[380px] rounded-md border bg-muted/50 p-2 font-mono text-sm">
          <div className="space-y-1">
            {filteredLogs.length === 0 ? (
              <div className="flex h-32 items-center justify-center text-muted-foreground">
                <div className="text-center">
                  <Terminal className="mx-auto mb-2 h-8 w-8 opacity-20" />
                  <p>No logs to display</p>
                  <p className="text-xs">Start the stream to see logs</p>
                </div>
              </div>
            ) : (
              filteredLogs.map((log) => {
                const Icon = levelIcons[log.level]
                return (
                  <div
                    key={log.id}
                    className={cn(
                      'flex items-start gap-2 rounded p-1.5 transition-colors hover:bg-muted',
                      levelBgColors[log.level]
                    )}
                  >
                    <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', levelColors[log.level])} />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-muted-foreground">
                          {log.timestamp.toLocaleTimeString()}
                        </span>
                        <Badge variant="outline" className="text-[10px] h-4 px-1">
                          {log.source}
                        </Badge>
                        <Badge
                          variant={log.level === 'error' ? 'destructive' : 'secondary'}
                          className={cn('text-[10px] h-4 px-1', levelColors[log.level])}
                        >
                          {log.level}
                        </Badge>
                      </div>
                      <div className={cn('mt-0.5', levelColors[log.level])}>
                        {log.message}
                      </div>
                      {showMetadata && log.metadata && (
                        <div className="mt-1 text-xs text-muted-foreground">
                          {JSON.stringify(log.metadata)}
                        </div>
                      )}
                    </div>
                  </div>
                )
              })
            )}
            <div ref={logsEndRef} />
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  )
}

import { useMemo } from 'react'

// Log analyzer component
interface LogAnalyzerProps {
  logs: LogEntry[]
}

export function LogAnalyzer({ logs }: LogAnalyzerProps) {
  const analysis = useMemo(() => {
    const byLevel = logs.reduce((acc, log) => {
      acc[log.level] = (acc[log.level] || 0) + 1
      return acc
    }, {} as Record<string, number>)

    const bySource = logs.reduce((acc, log) => {
      acc[log.source] = (acc[log.source] || 0) + 1
      return acc
    }, {} as Record<string, number>)

    const byHour = logs.reduce((acc, log) => {
      const hour = log.timestamp.getHours()
      acc[hour] = (acc[hour] || 0) + 1
      return acc
    }, {} as Record<number, number>)

    return { byLevel, bySource, byHour }
  }, [logs])

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">By Level</CardTitle>
          </CardHeader>
          <CardContent>
            {Object.entries(analysis.byLevel).map(([level, count]) => (
              <div key={level} className="flex items-center justify-between py-1">
                <span className={cn('capitalize', levelColors[level as LogEntry['level']])}>{level}</span>
                <span className="font-mono">{count}</span>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">By Source</CardTitle>
          </CardHeader>
          <CardContent>
            {Object.entries(analysis.bySource).map(([source, count]) => (
              <div key={source} className="flex items-center justify-between py-1">
                <span className="capitalize">{source}</span>
                <span className="font-mono">{count}</span>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Hourly Distribution</CardTitle>
          </CardHeader>
          <CardContent>
            {Object.entries(analysis.byHour).slice(-6).map(([hour, count]) => (
              <div key={hour} className="flex items-center justify-between py-1">
                <span>{hour}:00</span>
                <span className="font-mono">{count}</span>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
