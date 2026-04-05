import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DataTable } from '@/components/data-table'
import { toast } from 'sonner'
import { cn, formatDate } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  AlertTriangle,
  Clock,
  Play,
  Pause,
  Calendar,
  Globe,
  Shield,
  Users,
  FileText,
  Trash2,
  Edit,
  History,
  CheckCircle2,
  XCircle,
  AlertOctagon,
  Bell,
  Settings,
  Zap,
  Code
} from 'lucide-react'

interface MaintenanceWindow {
  id: string
  name: string
  description?: string
  startTime: Date
  endTime: Date
  status: 'scheduled' | 'active' | 'completed' | 'cancelled'
  createdBy: string
  affectedPools: string[]
  allowHealthChecks: boolean
  customPage?: {
    enabled: boolean
    title: string
    message: string
    statusCode: number
  }
  notifyBefore: number // minutes
}

const mockMaintenanceWindows: MaintenanceWindow[] = [
  {
    id: '1',
    name: 'Database Migration',
    description: 'Scheduled database migration to new cluster',
    startTime: new Date(Date.now() + 86400000),
    endTime: new Date(Date.now() + 90000000),
    status: 'scheduled',
    createdBy: 'admin',
    affectedPools: ['db-pool', 'api-pool'],
    allowHealthChecks: true,
    customPage: {
      enabled: true,
      title: 'Maintenance in Progress',
      message: 'We are currently performing scheduled maintenance. Please check back soon.',
      statusCode: 503
    },
    notifyBefore: 60
  },
  {
    id: '2',
    name: 'SSL Certificate Renewal',
    description: 'Renewal of SSL certificates',
    startTime: new Date(Date.now() - 172800000),
    endTime: new Date(Date.now() - 169200000),
    status: 'completed',
    createdBy: 'admin',
    affectedPools: ['web-pool'],
    allowHealthChecks: true,
    notifyBefore: 30
  },
  {
    id: '3',
    name: 'Emergency Patch',
    description: 'Security patch deployment',
    startTime: new Date(Date.now() - 3600000),
    endTime: new Date(Date.now() + 1800000),
    status: 'active',
    createdBy: 'admin',
    affectedPools: ['all'],
    allowHealthChecks: false,
    customPage: {
      enabled: true,
      title: 'Service Temporarily Unavailable',
      message: 'We are performing emergency maintenance.',
      statusCode: 503
    },
    notifyBefore: 0
  }
]

const maintenanceColumns: ColumnDef<MaintenanceWindow>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => (
      <div>
        <p className="font-medium">{row.original.name}</p>
        {row.original.description && (
          <p className="text-xs text-muted-foreground truncate max-w-[200px]">
            {row.original.description}
          </p>
        )}
      </div>
    )
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => {
      const statusColors: Record<string, string> = {
        scheduled: 'bg-blue-500',
        active: 'bg-yellow-500',
        completed: 'bg-green-500',
        cancelled: 'bg-gray-500'
      }
      return (
        <Badge className={statusColors[row.original.status]}>
          {row.original.status.charAt(0).toUpperCase() + row.original.status.slice(1)}
        </Badge>
      )
    }
  },
  {
    accessorKey: 'startTime',
    header: 'Start Time',
    cell: ({ row }) => formatDate(row.original.startTime)
  },
  {
    accessorKey: 'endTime',
    header: 'End Time',
    cell: ({ row }) => formatDate(row.original.endTime)
  },
  {
    accessorKey: 'affectedPools',
    header: 'Affected Pools',
    cell: ({ row }) => (
      <div className="flex gap-1 flex-wrap">
        {row.original.affectedPools.slice(0, 2).map(pool => (
          <Badge key={pool} variant="outline" className="text-xs">
            {pool}
          </Badge>
        ))}
        {row.original.affectedPools.length > 2 && (
          <Badge variant="outline" className="text-xs">
            +{row.original.affectedPools.length - 2}
          </Badge>
        )}
      </div>
    )
  }
]

export function MaintenanceMode() {
  const [windows, setWindows] = useState<MaintenanceWindow[]>(mockMaintenanceWindows)
  const [isMaintenanceMode, setIsMaintenanceMode] = useState(false)
  const [showScheduleDialog, setShowScheduleDialog] = useState(false)
  const [activeTab, setActiveTab] = useState('active')
  const [customPage, setCustomPage] = useState({
    enabled: true,
    title: 'Maintenance in Progress',
    message: 'We are currently performing scheduled maintenance. Please check back soon.',
    statusCode: 503
  })

  const toggleMaintenance = () => {
    setIsMaintenanceMode(!isMaintenanceMode)
    toast.success(isMaintenanceMode ? 'Maintenance mode disabled' : 'Maintenance mode enabled')
  }

  const scheduleMaintenance = () => {
    setShowScheduleDialog(false)
    toast.success('Maintenance window scheduled')
  }

  const cancelMaintenance = (id: string) => {
    toast.success('Maintenance window cancelled')
  }

  const deleteMaintenance = (id: string) => {
    toast.success('Maintenance window deleted')
  }

  const filteredWindows = windows.filter(w => {
    if (activeTab === 'active') return w.status === 'scheduled' || w.status === 'active'
    if (activeTab === 'past') return w.status === 'completed' || w.status === 'cancelled'
    return true
  })

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Maintenance Mode</h1>
          <p className="text-muted-foreground">
            Manage maintenance windows and service availability
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => setShowScheduleDialog(true)}>
            <Calendar className="mr-2 h-4 w-4" />
            Schedule
          </Button>
          <Button
            variant={isMaintenanceMode ? 'destructive' : 'default'}
            onClick={toggleMaintenance}
          >
            {isMaintenanceMode ? (
              <>
                <Play className="mr-2 h-4 w-4" />
                Disable Maintenance
              </>
            ) : (
              <>
                <Pause className="mr-2 h-4 w-4" />
                Enable Maintenance
              </>
            )}
          </Button>
        </div>
      </div>

      {/* Status Card */}
      <Card className={cn(isMaintenanceMode && 'border-yellow-500/50 bg-yellow-500/5')}>
        <CardContent className="pt-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className={cn(
                'h-12 w-12 rounded-full flex items-center justify-center',
                isMaintenanceMode ? 'bg-yellow-500/20' : 'bg-green-500/20'
              )}>
                {isMaintenanceMode ? (
                  <AlertTriangle className="h-6 w-6 text-yellow-500" />
                ) : (
                  <CheckCircle2 className="h-6 w-6 text-green-500" />
                )}
              </div>
              <div>
                <h3 className="text-lg font-semibold">
                  {isMaintenanceMode ? 'Maintenance Mode Active' : 'System Operational'}
                </h3>
                <p className="text-muted-foreground">
                  {isMaintenanceMode
                    ? 'Service is currently unavailable to users'
                    : 'All services are running normally'}
                </p>
              </div>
            </div>
            <Badge variant={isMaintenanceMode ? 'destructive' : 'default'} className="text-sm">
              {isMaintenanceMode ? 'MAINTENANCE' : 'OPERATIONAL'}
            </Badge>
          </div>
        </CardContent>
      </Card>

      {/* Maintenance Windows */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="active">
            <Clock className="mr-2 h-4 w-4" />
            Active & Scheduled
          </TabsTrigger>
          <TabsTrigger value="past">
            <History className="mr-2 h-4 w-4" />
            Past
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings className="mr-2 h-4 w-4" />
            Settings
          </TabsTrigger>
        </TabsList>

        <TabsContent value="active" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Maintenance Windows</CardTitle>
              <CardDescription>Scheduled and active maintenance periods</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={filteredWindows}
                columns={maintenanceColumns}
                actions={[
                  {
                    label: 'Edit',
                    icon: Edit,
                    onClick: () => setShowScheduleDialog(true)
                  },
                  {
                    label: 'Cancel',
                    icon: XCircle,
                    variant: 'destructive',
                    onClick: (w) => cancelMaintenance(w.id)
                  },
                  {
                    label: 'Delete',
                    icon: Trash2,
                    variant: 'destructive',
                    onClick: (w) => deleteMaintenance(w.id)
                  }
                ]}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="past" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>History</CardTitle>
              <CardDescription>Past maintenance windows</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={filteredWindows}
                columns={maintenanceColumns}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Custom Maintenance Page</CardTitle>
              <CardDescription>Configure the page shown during maintenance</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Enable Custom Page</Label>
                  <p className="text-sm text-muted-foreground">
                    Show a custom maintenance page instead of default
                  </p>
                </div>
                <Switch
                  checked={customPage.enabled}
                  onCheckedChange={(v) => setCustomPage(p => ({ ...p, enabled: v }))}
                />
              </div>

              {customPage.enabled && (
                <>
                  <div className="space-y-2">
                    <Label>Page Title</Label>
                    <Input
                      value={customPage.title}
                      onChange={(e) => setCustomPage(p => ({ ...p, title: e.target.value }))}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label>Message</Label>
                    <Textarea
                      value={customPage.message}
                      onChange={(e) => setCustomPage(p => ({ ...p, message: e.target.value }))}
                      rows={4}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label>HTTP Status Code</Label>
                    <Select
                      value={String(customPage.statusCode)}
                      onValueChange={(v) => setCustomPage(p => ({ ...p, statusCode: parseInt(v) }))}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="503">503 - Service Unavailable</SelectItem>
                        <SelectItem value="502">502 - Bad Gateway</SelectItem>
                        <SelectItem value="504">504 - Gateway Timeout</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="rounded-lg border p-4">
                    <Label className="mb-2 block">Preview</Label>
                    <div className="bg-muted rounded-lg p-8 text-center">
                      <h2 className="text-2xl font-bold mb-2">{customPage.title}</h2>
                      <p className="text-muted-foreground">{customPage.message}</p>
                      <p className="text-xs text-muted-foreground mt-4">
                        HTTP {customPage.statusCode}
                      </p>
                    </div>
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Schedule Dialog */}
      <Dialog open={showScheduleDialog} onOpenChange={setShowScheduleDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Schedule Maintenance</DialogTitle>
            <DialogDescription>Plan a maintenance window</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Name</Label>
              <Input placeholder="e.g., Database Migration" />
            </div>
            <div className="space-y-2">
              <Label>Description</Label>
              <Textarea placeholder="Describe the maintenance..." />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Start Time</Label>
                <Input type="datetime-local" />
              </div>
              <div className="space-y-2">
                <Label>End Time</Label>
                <Input type="datetime-local" />
              </div>
            </div>
            <div className="space-y-2">
              <Label>Affected Pools</Label>
              <div className="flex flex-wrap gap-2">
                {['web-pool', 'api-pool', 'db-pool'].map(pool => (
                  <Badge key={pool} variant="outline" className="cursor-pointer hover:bg-muted">
                    {pool}
                  </Badge>
                ))}
              </div>
            </div>
            <div className="flex items-center justify-between">
              <Label>Allow Health Checks</Label>
              <Switch defaultChecked />
            </div>
            <div className="flex items-center justify-between">
              <Label>Notify Before (minutes)</Label>
              <Input type="number" defaultValue={60} className="w-24" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowScheduleDialog(false)}>
              Cancel
            </Button>
            <Button onClick={scheduleMaintenance}>
              <Calendar className="mr-2 h-4 w-4" />
              Schedule
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
