import { useState, useEffect, useRef } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { useDebounce } from "@/hooks/use-debounce"
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
import { Search, Download, Pause, Play, AlertCircle, Info, AlertTriangle, CheckCircle, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { useEvents } from "@/hooks/use-query"
import { APIEventItem } from "@/types"
import { LoadingCard } from "@/components/ui/loading"

type LogLevel = 'debug' | 'info' | 'warn' | 'error'

// Convert event type to log level
function eventToLevel(type: string): LogLevel {
  switch (type) {
    case 'success': return 'info'
    case 'warning': return 'warn'
    case 'error': return 'error'
    default: return 'info'
  }
}

// Convert event to log entry
function eventToLog(event: APIEventItem): { id: string; timestamp: string; level: LogLevel; source: string; message: string } {
  return {
    id: event.id,
    timestamp: event.timestamp,
    level: eventToLevel(event.type),
    source: 'system',
    message: event.message,
  }
}

export function LogsPage() {
  useDocumentTitle("Logs")
  const { data: events, refetch, isLoading, error } = useEvents()
  const [search, setSearch] = useState("")
  const debouncedSearch = useDebounce(search)
  const [levelFilter, setLevelFilter] = useState<string>("all")
  const [isLive, setIsLive] = useState(true)
  const [autoScroll, setAutoScroll] = useState(true)
  const logsEndRef = useRef<HTMLDivElement>(null)

  // Convert events to log entries
  const logs = (events ?? []).map(eventToLog)

  // Auto-refresh in live mode
  useEffect(() => {
    if (!isLive) return
    const interval = setInterval(() => refetch(), 5000)
    return () => clearInterval(interval)
  }, [isLive, refetch])

  // Filter logs
  const filteredLogs = logs.filter(l => {
    if (debouncedSearch && !l.message.toLowerCase().includes(debouncedSearch.toLowerCase()) && !l.source.toLowerCase().includes(debouncedSearch.toLowerCase())) {
      return false
    }
    if (levelFilter !== "all" && l.level !== levelFilter) {
      return false
    }
    return true
  })

  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: "smooth" })
    }
  }, [filteredLogs, autoScroll])

  const handleExport = () => {
    const blob = new Blob([JSON.stringify(logs, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `olb-events-${new Date().toISOString().split('T')[0]}.json`
    a.click()
    URL.revokeObjectURL(url)
    toast.success("Events exported")
  }

  const getLevelIcon = (level: string) => {
    switch (level) {
      case 'debug': return <Info className="h-4 w-4 text-muted-foreground"  aria-hidden="true" />
      case 'info': return <CheckCircle className="h-4 w-4 text-blue-500"  aria-hidden="true" />
      case 'warn': return <AlertTriangle className="h-4 w-4 text-amber-500"  aria-hidden="true" />
      case 'error': return <AlertCircle className="h-4 w-4 text-red-500"  aria-hidden="true" />
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
    try {
      const date = new Date(ts)
      return date.toLocaleTimeString()
    } catch {
      return ts
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">System Events</h1>
          <p className="text-muted-foreground">View backend health events and system activity</p>
        </div>
        <LoadingCard />
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">System Events</h1>
          <p className="text-muted-foreground">View backend health events and system activity</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-destructive">Failed to load events: {error.message}</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4"  aria-hidden="true" /> Retry
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">System Events</h1>
          <p className="text-muted-foreground">View backend health events and system activity</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-2 mr-4" role="status" aria-live="polite" aria-label={isLive ? "Live streaming" : "Streaming paused"}>
            <div className={cn("h-2 w-2 rounded-full", isLive ? "bg-success animate-pulse" : "bg-muted-foreground")} aria-hidden="true" />
            <span className="text-sm text-muted-foreground">{isLive ? "Live" : "Paused"}</span>
          </div>
          <Button variant="outline" size="sm" onClick={() => setIsLive(!isLive)}>
            {isLive ? <Pause className="h-4 w-4 mr-2"  aria-hidden="true" /> : <Play className="h-4 w-4 mr-2"  aria-hidden="true" />}
            {isLive ? "Pause" : "Resume"}
          </Button>
          <Button variant="outline" size="sm" onClick={handleExport}>
            <Download className="h-4 w-4 mr-2"  aria-hidden="true" />
            Export
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
          <CardDescription>Filter events by level or search term</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-3">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"  aria-hidden="true" />
              <Input
                placeholder="Search events..." aria-label="Search events"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-10"
              />
            </div>
            <Select value={levelFilter} onValueChange={setLevelFilter}>
              <SelectTrigger aria-label="Event level filter">
                <SelectValue placeholder="Event Level" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Levels</SelectItem>
                <SelectItem value="info">Info</SelectItem>
                <SelectItem value="warn">Warning</SelectItem>
                <SelectItem value="error">Error</SelectItem>
              </SelectContent>
            </Select>
            <div className="flex items-center justify-between px-4 py-2 border rounded-md">
              <span className="text-sm">Auto-scroll</span>
              <Switch checked={autoScroll} onCheckedChange={setAutoScroll} aria-label="Auto-scroll" />
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div>
            <CardTitle>Events</CardTitle>
            <CardDescription>
              Showing {filteredLogs.length} of {logs.length} events
              {isLive && " (auto-refreshing every 5s)"}
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          <div className="border rounded-lg overflow-x-auto">
            <div className="max-h-[600px] overflow-y-auto font-mono text-sm">
              {filteredLogs.length === 0 ? (
                <div className="p-8 text-center text-muted-foreground">
                  {logs.length === 0
                    ? "No system events available. Events appear as backends change health status."
                    : "No events match the current filters"}
                </div>
              ) : (
                <table className="w-full">
                  <caption className="sr-only">Event log with time, level, source, and message columns</caption>
                  <thead className="bg-muted sticky top-0">
                    <tr>
                      <th className="text-left px-4 py-2 w-32">Time</th>
                      <th className="text-left px-4 py-2 w-20">Level</th>
                      <th className="text-left px-4 py-2 w-24">Source</th>
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
