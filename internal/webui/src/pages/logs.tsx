import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table'
import {
  FileText,
  Download,
  RefreshCw,
  AlertCircle,
  CheckCircle,
  Info,
  AlertTriangle,
  Search,
  Filter,
  Shield,
  Terminal
} from 'lucide-react'
import api from '@/lib/api'
import { RealtimeLogViewer, LogAnalyzer } from '@/components/log-viewer'

interface LogEntry {
  id: string
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  component: string
  message: string
  metadata?: Record<string, any>
}

const logLevels = [
  { value: 'all', label: 'All Levels' },
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warning' },
  { value: 'error', label: 'Error' }
]

const components = [
  { value: 'all', label: 'All Components' },
  { value: 'proxy', label: 'Proxy' },
  { value: 'balancer', label: 'Balancer' },
  { value: 'health', label: 'Health Check' },
  { value: 'waf', label: 'WAF' },
  { value: 'tls', label: 'TLS' }
]

export function LogsPage() {
  const [activeTab, setActiveTab] = useState('realtime')
  const [logLevel, setLogLevel] = useState('all')
  const [component, setComponent] = useState('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [lines, setLines] = useState(100)

  const { data: logs = [], isLoading, refetch } = useQuery<LogEntry[]>({
    queryKey: ['logs', activeTab, logLevel, component, lines],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (logLevel !== 'all') params.append('level', logLevel)
      if (component !== 'all') params.append('component', component)
      params.append('lines', lines.toString())

      const response = await api.get(`/api/v1/logs/${activeTab}?${params}`)
      return response.data
    },
    refetchInterval: activeTab === 'realtime' ? 1000 : 5000,
    enabled: activeTab !== 'realtime'
  })

  const filteredLogs = logs.filter(
    (log) =>
      log.message.toLowerCase().includes(searchQuery.toLowerCase()) ||
      log.component.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const getLevelIcon = (level: string) => {
    switch (level) {
      case 'error':
        return <AlertCircle className="h-4 w-4 text-destructive" />
      case 'warn':
        return <AlertTriangle className="h-4 w-4 text-amber-500" />
      case 'info':
        return <Info className="h-4 w-4 text-blue-500" />
      case 'debug':
        return <CheckCircle className="h-4 w-4 text-green-500" />
      default:
        return <Info className="h-4 w-4" />
    }
  }

  const getLevelBadge = (level: string) => {
    switch (level) {
      case 'error':
        return <Badge variant="destructive">ERROR</Badge>
      case 'warn':
        return <Badge className="bg-amber-500">WARN</Badge>
      case 'info':
        return <Badge>INFO</Badge>
      case 'debug':
        return <Badge variant="secondary">DEBUG</Badge>
      default:
        return <Badge>{level.toUpperCase()}</Badge>
    }
  }

  const formatTimestamp = (timestamp: string) => {
    return new Date(timestamp).toLocaleString()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Logs</h1>
          <p className="text-muted-foreground">
            View and analyze system and access logs
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => refetch()}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Button variant="outline">
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="realtime">
            <Terminal className="mr-2 h-4 w-4" />
            Real-time
          </TabsTrigger>
          <TabsTrigger value="system">System Logs</TabsTrigger>
          <TabsTrigger value="access">Access Logs</TabsTrigger>
          <TabsTrigger value="waf">WAF Logs</TabsTrigger>
          <TabsTrigger value="audit">Audit Logs</TabsTrigger>
        </TabsList>

        <TabsContent value="realtime" className="space-y-4">
          <RealtimeLogViewer />
        </TabsContent>

        <TabsContent value="system" className="space-y-4">
          {/* Filters */}
          <Card>
            <CardContent className="pt-6">
              <div className="flex flex-wrap gap-4">
                <div className="flex-1 min-w-[200px]">
                  <Label htmlFor="search" className="mb-2 block">
                    Search
                  </Label>
                  <div className="relative">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                    <Input
                      id="search"
                      placeholder="Search logs..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="pl-9"
                    />
                  </div>
                </div>
                <div className="w-[180px]">
                  <Label htmlFor="level" className="mb-2 block">
                    Log Level
                  </Label>
                  <Select value={logLevel} onValueChange={setLogLevel}>
                    <SelectTrigger id="level">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {logLevels.map((l) => (
                        <SelectItem key={l.value} value={l.value}>
                          {l.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="w-[180px]">
                  <Label htmlFor="component" className="mb-2 block">
                    Component
                  </Label>
                  <Select value={component} onValueChange={setComponent}>
                    <SelectTrigger id="component">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {components.map((c) => (
                        <SelectItem key={c.value} value={c.value}>
                          {c.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="w-[120px]">
                  <Label htmlFor="lines" className="mb-2 block">
                    Lines
                  </Label>
                  <Select value={lines.toString()} onValueChange={(v) => setLines(parseInt(v))}>
                    <SelectTrigger id="lines">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="100">100</SelectItem>
                      <SelectItem value="500">500</SelectItem>
                      <SelectItem value="1000">1000</SelectItem>
                      <SelectItem value="5000">5000</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Logs Table */}
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle>Log Entries</CardTitle>
                  <CardDescription>
                    Showing {filteredLogs.length} entries
                  </CardDescription>
                </div>
                <Filter className="h-4 w-4 text-muted-foreground" />
              </div>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <div className="flex h-32 items-center justify-center">
                  <RefreshCw className="h-6 w-6 animate-spin" />
                </div>
              ) : filteredLogs.length === 0 ? (
                <div className="flex h-32 flex-col items-center justify-center text-center">
                  <FileText className="h-8 w-8 text-muted-foreground" />
                  <p className="mt-2 text-muted-foreground">No logs found</p>
                </div>
              ) : (
                <div className="border rounded-lg">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-[140px]">Timestamp</TableHead>
                        <TableHead className="w-[80px]">Level</TableHead>
                        <TableHead className="w-[120px]">Component</TableHead>
                        <TableHead>Message</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredLogs.map((log) => (
                        <TableRow key={log.id}>
                          <TableCell className="font-mono text-xs">
                            {formatTimestamp(log.timestamp)}
                          </TableCell>
                          <TableCell>
                            <div className="flex items-center gap-1">
                              {getLevelIcon(log.level)}
                              {getLevelBadge(log.level)}
                            </div>
                          </TableCell>
                          <TableCell>
                            <code className="text-xs bg-muted px-1.5 py-0.5 rounded">
                              {log.component}
                            </code>
                          </TableCell>
                          <TableCell className="max-w-md">
                            <p className="text-sm truncate" title={log.message}>
                              {log.message}
                            </p>
                            {log.metadata && (
                              <pre className="mt-1 text-xs text-muted-foreground overflow-hidden">
                                {JSON.stringify(log.metadata, null, 2).slice(0, 100)}
                              </pre>
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="access" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Access Logs</CardTitle>
              <CardDescription>HTTP request and response logs</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-center py-8 text-muted-foreground">
                <FileText className="h-8 w-8 mx-auto mb-2" />
                <p>Access logs will be displayed here</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="waf" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>WAF Logs</CardTitle>
              <CardDescription>Web Application Firewall security events</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-center py-8 text-muted-foreground">
                <Shield className="h-8 w-8 mx-auto mb-2" />
                <p>WAF security events will be displayed here</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="audit" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Audit Logs</CardTitle>
              <CardDescription>Administrative actions and configuration changes</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-center py-8 text-muted-foreground">
                <FileText className="h-8 w-8 mx-auto mb-2" />
                <p>Audit logs will be displayed here</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
