import { useState, useEffect, useRef } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Search, Download, Trash2, Pause, Play, AlertCircle, Info, AlertTriangle, CheckCircle } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

interface LogEntry {
  id: string
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  source: string
  message: string
}

const mockLogs: LogEntry[] = [
  { id: "1", timestamp: "2025-04-05T20:15:30Z", level: "info", source: "proxy", message: "Request completed: GET /api/v1/users 200 12ms" },
  { id: "2", timestamp: "2025-04-05T20:15:28Z", level: "debug", source: "balancer", message: "Selected backend: 10.0.1.10:8080 (round_robin)" },
  { id: "3", timestamp: "2025-04-05T20:15:25Z", level: "warn", source: "health", message: "Backend 10.0.1.12:8080 health check failed (timeout)" },
  { id: "4", timestamp: "2025-04-05T20:15:20Z", level: "info", source: "waf", message: "Blocked request from 192.168.1.100 - SQL injection detected" },
  { id: "5", timestamp: "2025-04-05T20:15:15Z", level: "error", source: "tls", message: "Certificate validation failed for *.example.com" },
  { id: "6", timestamp: "2025-04-05T20:15:10Z", level: "info", source: "config", message: "Configuration reloaded successfully" },
  { id: "7", timestamp: "2025-04-05T20:15:05Z", level: "debug", source: "middleware", message: "Applied CORS headers for request" },
  { id: "8", timestamp: "2025-04-05T20:15:00Z", level: "info", source: "pool", message: "Backend 10.0.1.11:8080 marked healthy" },
]

export function LogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>(mockLogs)
  const [filteredLogs, setFilteredLogs] = useState<LogEntry[]>(mockLogs)
  const [search, setSearch] = useState("")
  const [levelFilter, setLevelFilter] = useState<string>("all")
  const [sourceFilter, setSourceFilter] = useState<string>("all")
  const [isLive, setIsLive] = useState(true)
  const [autoScroll, setAutoScroll] = useState(true)
  const logsEndRef = useRef<HTMLDivElement>(null)

  const sources = Array.from(new Set(logs.map(l => l.source)))

  useEffect(() => {
    let filtered = logs

    if (search) {
      filtered = filtered.filter(l =>
        l.message.toLowerCase().includes(search.toLowerCase()) ||
        l.source.toLowerCase().includes(search.toLowerCase())
      )
    }

    if (levelFilter !== "all") {
      filtered = filtered.filter(l => l.level === levelFilter)
    }

    if (sourceFilter !== "all") {
      filtered = filtered.filter(l => l.source === sourceFilter)
    }

    setFilteredLogs(filtered)
  }, [logs, search, levelFilter, sourceFilter])

  useEffect(() => {
    if (!isLive) return

    const interval = setInterval(() => {
      const newLog: LogEntry = {
        id: Math.random().toString(36).substr(2, 9),
        timestamp: new Date().toISOString(),
        level: ['debug', 'info', 'warn', 'error'][Math.floor(Math.random() * 4)] as any,
        source: sources[Math.floor(Math.random() * sources.length)],
        message: `Generated log entry at ${new Date().toLocaleTimeString()}`,
      }
      setLogs(prev => [newLog, ...prev].slice(0, 500))
    }, 2000)

    return () => clearInterval(interval)
  }, [isLive, sources])

  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: "smooth" })
    }
  }, [filteredLogs, autoScroll])

  const handleClear = () => {
    setLogs([])
    toast.success("Logs cleared")
  }

  const handleExport = () => {
    const blob = new Blob([JSON.stringify(logs, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `olb-logs-${new Date().toISOString().split('T')[0]}.json`
    a.click()
    URL.revokeObjectURL(url)
    toast.success("Logs exported")
  }

  const getLevelIcon = (level: string) => {
    switch (level) {
      case 'debug': return <Info className="h-4 w-4 text-gray-500" />
      case 'info': return <CheckCircle className="h-4 w-4 text-blue-500" />
      case 'warn': return <AlertTriangle className="h-4 w-4 text-amber-500" />
      case 'error': return <AlertCircle className="h-4 w-4 text-red-500" />
      default: return null
    }
  }

  const getLevelBadge = (level: string) => {
    switch (level) {
      case 'debug': return <Badge variant="outline" className="text-xs">DEBUG</Badge>
      case 'info': return <Badge variant="default" className="text-xs">INFO</Badge>
      case 'warn': return <Badge variant="secondary" className="text-xs">WARN</Badge>
      case 'error': return <Badge variant="destructive" className="text-xs">ERROR</Badge>
      default: return null
    }
  }

  const formatTimestamp = (ts: string) => {
    const date = new Date(ts)
    return date.toLocaleTimeString()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Logs</h1>
          <p className="text-muted-foreground">View and search system logs</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-2 mr-4">
            <div className={cn("h-2 w-2 rounded-full", isLive ? "bg-green-500 animate-pulse" : "bg-gray-400")} />
            <span className="text-sm text-muted-foreground">{isLive ? "Live" : "Paused"}</span>
          </div>
          <Button variant="outline" size="sm" onClick={() => setIsLive(!isLive)}>
            {isLive ? <Pause className="h-4 w-4 mr-2" /> : <Play className="h-4 w-4 mr-2" />}
            {isLive ? "Pause" : "Resume"}
          </Button>
          <Button variant="outline" size="sm" onClick={handleExport}>
            <Download className="h-4 w-4 mr-2" />
            Export
          </Button>
          <Button variant="outline" size="sm" onClick={handleClear}>
            <Trash2 className="h-4 w-4 mr-2" />
            Clear
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Log Filters</CardTitle>
          <CardDescription>Filter logs by level, source, or search term</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-4">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                placeholder="Search logs..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-10"
              />
            </div>
            <Select value={levelFilter} onValueChange={setLevelFilter}>
              <SelectTrigger>
                <SelectValue placeholder="Log Level" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Levels</SelectItem>
                <SelectItem value="debug">Debug</SelectItem>
                <SelectItem value="info">Info</SelectItem>
                <SelectItem value="warn">Warning</SelectItem>
                <SelectItem value="error">Error</SelectItem>
              </SelectContent>
            </Select>
            <Select value={sourceFilter} onValueChange={setSourceFilter}>
              <SelectTrigger>
                <SelectValue placeholder="Source" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Sources</SelectItem>
                {sources.map(source => (
                  <SelectItem key={source} value={source}>{source}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="flex items-center justify-between px-4 py-2 border rounded-md">
              <span className="text-sm">Auto-scroll</span>
              <Switch checked={autoScroll} onCheckedChange={setAutoScroll} />
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Log Entries</CardTitle>
            <CardDescription>Showing {filteredLogs.length} of {logs.length} logs</CardDescription>
          </div>
          <div className="flex gap-2 text-xs">
            <div className="flex items-center gap-1">
              <div className="h-2 w-2 rounded-full bg-blue-500" />
              <span>Debug</span>
            </div>
            <div className="flex items-center gap-1">
              <div className="h-2 w-2 rounded-full bg-green-500" />
              <span>Info</span>
            </div>
            <div className="flex items-center gap-1">
              <div className="h-2 w-2 rounded-full bg-amber-500" />
              <span>Warn</span>
            </div>
            <div className="flex items-center gap-1">
              <div className="h-2 w-2 rounded-full bg-red-500" />
              <span>Error</span>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="border rounded-lg overflow-hidden">
            <div className="max-h-[600px] overflow-y-auto font-mono text-sm">
              {filteredLogs.length === 0 ? (
                <div className="p-8 text-center text-muted-foreground">
                  No logs match the current filters
                </div>
              ) : (
                <table className="w-full">
                  <thead className="bg-muted sticky top-0">
                    <tr>
                      <th className="text-left px-4 py-2 w-32">Time</th>
                      <th className="text-left px-4 py-2 w-20">Level</th>
                      <th className="text-left px-4 py-2 w-32">Source</th>
                      <th className="text-left px-4 py-2">Message</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredLogs.map((log) => (
                      <tr key={log.id} className="border-t hover:bg-muted/50">
                        <td className="px-4 py-2 text-muted-foreground">{formatTimestamp(log.timestamp)}</td>
                        <td className="px-4 py-2">
                          <div className="flex items-center gap-1">
                            {getLevelIcon(log.level)}
                            {getLevelBadge(log.level)}
                          </div>
                        </td>
                        <td className="px-4 py-2">
                          <Badge variant="outline" className="text-xs capitalize">{log.source}</Badge>
                        </td>
                        <td className="px-4 py-2">{log.message}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              <div ref={logsEndRef} />
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
