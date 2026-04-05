import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ScrollArea } from '@/components/ui/scroll-area'
import { DataTable } from '@/components/data-table'
import { toast } from 'sonner'
import { cn, formatDate } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  FileText,
  Download,
  Filter,
  Search,
  User,
  Settings,
  Shield,
  Database,
  Activity,
  AlertTriangle,
  Info,
  CheckCircle2,
  AlertOctagon,
  Clock,
  Calendar,
  ChevronRight,
  RotateCcw,
  Trash2
} from 'lucide-react'

interface AuditLog {
  id: string
  timestamp: Date
  user: string
  action: 'create' | 'update' | 'delete' | 'login' | 'logout' | 'export' | 'import'
  resource: string
  resourceType: 'backend' | 'pool' | 'route' | 'listener' | 'config' | 'certificate' | 'user' | 'system'
  details: string
  ipAddress: string
  success: boolean
  metadata?: Record<string, any>
}

const mockAuditLogs: AuditLog[] = [
  {
    id: '1',
    timestamp: new Date(Date.now() - 300000),
    user: 'admin',
    action: 'update',
    resource: 'web-pool',
    resourceType: 'pool',
    details: 'Updated health check interval from 5s to 10s',
    ipAddress: '192.168.1.100',
    success: true
  },
  {
    id: '2',
    timestamp: new Date(Date.now() - 900000),
    user: 'operator1',
    action: 'create',
    resource: 'backend-05',
    resourceType: 'backend',
    details: 'Added new backend server at 10.0.0.15:8080',
    ipAddress: '192.168.1.105',
    success: true
  },
  {
    id: '3',
    timestamp: new Date(Date.now() - 1800000),
    user: 'admin',
    action: 'delete',
    resource: 'old-route',
    resourceType: 'route',
    details: 'Deleted deprecated API route',
    ipAddress: '192.168.1.100',
    success: true
  },
  {
    id: '4',
    timestamp: new Date(Date.now() - 3600000),
    user: 'system',
    action: 'login',
    resource: 'Authentication',
    resourceType: 'system',
    details: 'Failed login attempt from unknown location',
    ipAddress: '203.0.113.50',
    success: false
  },
  {
    id: '5',
    timestamp: new Date(Date.now() - 7200000),
    user: 'admin',
    action: 'export',
    resource: 'Configuration',
    resourceType: 'config',
    details: 'Exported full configuration backup',
    ipAddress: '192.168.1.100',
    success: true
  },
  {
    id: '6',
    timestamp: new Date(Date.now() - 10800000),
    user: 'operator1',
    action: 'update',
    resource: 'api.openloadbalancer.dev',
    resourceType: 'certificate',
    details: 'Renewed SSL certificate',
    ipAddress: '192.168.1.105',
    success: true,
    metadata: { certExpiry: '2025-12-31' }
  },
  {
    id: '7',
    timestamp: new Date(Date.now() - 14400000),
    user: 'system',
    action: 'create',
    resource: 'Auto-backup',
    resourceType: 'system',
    details: 'Automatic daily backup completed',
    ipAddress: '127.0.0.1',
    success: true
  },
  {
    id: '8',
    timestamp: new Date(Date.now() - 86400000),
    user: 'admin',
    action: 'import',
    resource: 'waf-rules',
    resourceType: 'config',
    details: 'Imported WAF rules from file',
    ipAddress: '192.168.1.100',
    success: true
  }
]

const actionIcons = {
  create: Database,
  update: Settings,
  delete: Trash2,
  login: User,
  logout: User,
  export: Download,
  import: Download
}

const actionColors = {
  create: 'text-green-500 bg-green-500/10',
  update: 'text-blue-500 bg-blue-500/10',
  delete: 'text-red-500 bg-red-500/10',
  login: 'text-purple-500 bg-purple-500/10',
  logout: 'text-gray-500 bg-gray-500/10',
  export: 'text-yellow-500 bg-yellow-500/10',
  import: 'text-yellow-500 bg-yellow-500/10'
}

const resourceTypeIcons = {
  backend: Database,
  pool: Database,
  route: Activity,
  listener: Activity,
  config: Settings,
  certificate: Shield,
  user: User,
  system: Activity
}

const logColumns: ColumnDef<AuditLog>[] = [
  {
    accessorKey: 'timestamp',
    header: 'Time',
    cell: ({ row }) => (
      <div className="flex flex-col">
        <span className="text-sm">{formatDate(row.original.timestamp)}</span>
        <span className="text-xs text-muted-foreground">
          {new Date(row.original.timestamp).toLocaleTimeString()}
        </span>
      </div>
    )
  },
  {
    accessorKey: 'user',
    header: 'User',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <div className="h-6 w-6 rounded-full bg-primary/10 flex items-center justify-center">
          <User className="h-3 w-3 text-primary" />
        </div>
        <span className="font-medium">{row.original.user}</span>
      </div>
    )
  },
  {
    accessorKey: 'action',
    header: 'Action',
    cell: ({ row }) => {
      const Icon = actionIcons[row.original.action]
      return (
        <div className={cn('inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium', actionColors[row.original.action])}>
          <Icon className="h-3 w-3" />
          <span className="capitalize">{row.original.action}</span>
        </div>
      )
    }
  },
  {
    accessorKey: 'resource',
    header: 'Resource',
    cell: ({ row }) => {
      const Icon = resourceTypeIcons[row.original.resourceType]
      return (
        <div className="flex items-center gap-2">
          <Icon className="h-4 w-4 text-muted-foreground" />
          <div>
            <p className="font-medium">{row.original.resource}</p>
            <p className="text-xs text-muted-foreground capitalize">{row.original.resourceType}</p>
          </div>
        </div>
      )
    }
  },
  {
    accessorKey: 'details',
    header: 'Details'
  },
  {
    accessorKey: 'ipAddress',
    header: 'IP Address',
    cell: ({ row }) => (
      <code className="text-xs bg-muted px-2 py-1 rounded">
        {row.original.ipAddress}
      </code>
    )
  },
  {
    accessorKey: 'success',
    header: 'Status',
    cell: ({ row }) => (
      row.original.success ? (
        <CheckCircle2 className="h-4 w-4 text-green-500" />
      ) : (
        <AlertOctagon className="h-4 w-4 text-red-500" />
      )
    )
  }
]

export function AuditLogs() {
  const [activeTab, setActiveTab] = useState('all')
  const [dateRange, setDateRange] = useState('24h')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null)

  const filteredLogs = mockAuditLogs.filter(log => {
    if (activeTab === 'security') return log.resourceType === 'system' || log.action === 'login'
    if (activeTab === 'changes') return ['create', 'update', 'delete'].includes(log.action)
    if (activeTab === 'failed') return !log.success
    return true
  })

  const handleExport = () => {
    toast.success('Audit logs exported as CSV')
  }

  const handlePurge = () => {
    toast.success('Audit logs purged successfully')
  }

  const stats = {
    total: mockAuditLogs.length,
    failed: mockAuditLogs.filter(l => !l.success).length,
    security: mockAuditLogs.filter(l => l.resourceType === 'system').length,
    changes: mockAuditLogs.filter(l => ['create', 'update', 'delete'].includes(l.action)).length
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Audit Logs</h1>
          <p className="text-muted-foreground">
            Track all system activities and changes
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={dateRange} onValueChange={setDateRange}>
            <SelectTrigger className="w-[140px]">
              <Calendar className="mr-2 h-4 w-4" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1h">Last Hour</SelectItem>
              <SelectItem value="24h">Last 24 Hours</SelectItem>
              <SelectItem value="7d">Last 7 Days</SelectItem>
              <SelectItem value="30d">Last 30 Days</SelectItem>
              <SelectItem value="all">All Time</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={handleExport}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
          <Button variant="outline" onClick={handlePurge}>
            <Trash2 className="mr-2 h-4 w-4" />
            Purge
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Events</CardTitle>
            <FileText className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.total}</div>
            <p className="text-xs text-muted-foreground">In selected period</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Failed Actions</CardTitle>
            <AlertOctagon className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.failed}</div>
            <p className="text-xs text-muted-foreground">Require attention</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Security Events</CardTitle>
            <Shield className="h-4 w-4 text-purple-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-purple-500">{stats.security}</div>
            <p className="text-xs text-muted-foreground">Logins & system</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Changes</CardTitle>
            <RotateCcw className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-blue-500">{stats.changes}</div>
            <p className="text-xs text-muted-foreground">CRUD operations</p>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="all">All Events</TabsTrigger>
          <TabsTrigger value="changes">Changes</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="failed">Failed</TabsTrigger>
        </TabsList>

        <TabsContent value={activeTab} className="space-y-4">
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle>Activity Log</CardTitle>
                  <CardDescription>View and filter system activities</CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  <div className="relative">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                      placeholder="Search logs..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="w-[250px] pl-9"
                    />
                  </div>
                  <Button variant="outline" size="icon">
                    <Filter className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <DataTable
                data={filteredLogs}
                columns={logColumns}
                onRowClick={(log) => setSelectedLog(log)}
              />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Log Detail Dialog */}
      {selectedLog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setSelectedLog(null)}>
          <Card className="w-full max-w-2xl m-4" onClick={(e) => e.stopPropagation()}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle className="flex items-center gap-2">
                  <FileText className="h-5 w-5" />
                  Log Details
                </CardTitle>
                <Button variant="ghost" size="icon" onClick={() => setSelectedLog(null)}>
                  <span className="sr-only">Close</span>
                  ×
                </Button>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <Label className="text-muted-foreground">Timestamp</Label>
                  <p className="font-medium">{formatDate(selectedLog.timestamp)}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">User</Label>
                  <p className="font-medium">{selectedLog.user}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">Action</Label>
                  <div className={cn('inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium', actionColors[selectedLog.action])}>
                    {(() => {
                      const Icon = actionIcons[selectedLog.action]
                      return <Icon className="h-3 w-3" />
                    })()}
                    <span className="capitalize">{selectedLog.action}</span>
                  </div>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">Resource Type</Label>
                  <Badge variant="outline" className="capitalize">
                    {selectedLog.resourceType}
                  </Badge>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">Resource</Label>
                  <p className="font-medium">{selectedLog.resource}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">IP Address</Label>
                  <code className="text-sm bg-muted px-2 py-1 rounded">
                    {selectedLog.ipAddress}
                  </code>
                </div>
              </div>
              <div className="space-y-1">
                <Label className="text-muted-foreground">Details</Label>
                <p className="text-sm">{selectedLog.details}</p>
              </div>
              {selectedLog.metadata && (
                <div className="space-y-1">
                  <Label className="text-muted-foreground">Metadata</Label>
                  <pre className="text-xs bg-muted p-3 rounded-lg overflow-auto">
                    {JSON.stringify(selectedLog.metadata, null, 2)}
                  </pre>
                </div>
              )}
              <div className="flex items-center gap-2 pt-4 border-t">
                {selectedLog.success ? (
                  <>
                    <CheckCircle2 className="h-5 w-5 text-green-500" />
                    <span className="text-green-600 font-medium">Action completed successfully</span>
                  </>
                ) : (
                  <>
                    <AlertOctagon className="h-5 w-5 text-red-500" />
                    <span className="text-red-600 font-medium">Action failed</span>
                  </>
                )}
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}

import { Label } from '@/components/ui/label'
