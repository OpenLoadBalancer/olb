import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { DataTable } from '@/components/data-table'
import { toast } from 'sonner'
import { cn, formatBytes, formatDate } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Database,
  Download,
  Upload,
  Clock,
  Calendar,
  CheckCircle2,
  XCircle,
  RotateCcw,
  HardDrive,
  Cloud,
  Server,
  Trash2,
  Play,
  Pause,
  Settings,
  FileArchive,
  AlertTriangle
} from 'lucide-react'

interface BackupJob {
  id: string
  name: string
  type: 'manual' | 'scheduled'
  status: 'running' | 'completed' | 'failed' | 'paused'
  size: number
  createdAt: Date
  completedAt?: Date
  location: string
  retention: number
}

interface RestorePoint {
  id: string
  backupId: string
  name: string
  createdAt: Date
  size: number
  source: string
}

const mockBackups: BackupJob[] = [
  {
    id: '1',
    name: 'Daily Backup',
    type: 'scheduled',
    status: 'completed',
    size: 1024 * 1024 * 245,
    createdAt: new Date(Date.now() - 86400000),
    completedAt: new Date(Date.now() - 86400000 + 300000),
    location: 'local',
    retention: 30
  },
  {
    id: '2',
    name: 'Weekly Full Backup',
    type: 'scheduled',
    status: 'completed',
    size: 1024 * 1024 * 512,
    createdAt: new Date(Date.now() - 604800000),
    completedAt: new Date(Date.now() - 604800000 + 600000),
    location: 's3',
    retention: 90
  },
  {
    id: '3',
    name: 'Pre-Update Backup',
    type: 'manual',
    status: 'completed',
    size: 1024 * 1024 * 248,
    createdAt: new Date(Date.now() - 172800000),
    completedAt: new Date(Date.now() - 172800000 + 240000),
    location: 'local',
    retention: 7
  },
  {
    id: '4',
    name: 'Emergency Backup',
    type: 'manual',
    status: 'running',
    size: 0,
    createdAt: new Date(),
    location: 'local',
    retention: 7
  }
]

const backupColumns: ColumnDef<BackupJob>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <FileArchive className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{row.original.name}</span>
      </div>
    )
  },
  {
    accessorKey: 'type',
    header: 'Type',
    cell: ({ row }) => (
      <Badge variant={row.original.type === 'scheduled' ? 'secondary' : 'outline'}>
        {row.original.type === 'scheduled' ? 'Scheduled' : 'Manual'}
      </Badge>
    )
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => {
      const status = row.original.status
      const statusColors: Record<string, string> = {
        running: 'text-blue-500',
        completed: 'text-green-500',
        failed: 'text-red-500',
        paused: 'text-yellow-500'
      }
      return (
        <div className="flex items-center gap-1.5">
          {status === 'running' && (
            <div className="h-2 w-2 animate-pulse rounded-full bg-blue-500" />
          )}
          {status === 'completed' && <CheckCircle2 className="h-4 w-4 text-green-500" />}
          {status === 'failed' && <XCircle className="h-4 w-4 text-red-500" />}
          <span className={cn('text-sm', statusColors[status])}>
            {status.charAt(0).toUpperCase() + status.slice(1)}
          </span>
        </div>
      )
    }
  },
  {
    accessorKey: 'size',
    header: 'Size',
    cell: ({ row }) => (
      row.original.size > 0 ? formatBytes(row.original.size) : '-'
    )
  },
  {
    accessorKey: 'createdAt',
    header: 'Created',
    cell: ({ row }) => formatDate(row.original.createdAt)
  },
  {
    accessorKey: 'location',
    header: 'Location',
    cell: ({ row }) => (
      <div className="flex items-center gap-1">
        {row.original.location === 's3' ? (
          <Cloud className="h-3.5 w-3.5" />
        ) : (
          <HardDrive className="h-3.5 w-3.5" />
        )}
        <span className="capitalize">{row.original.location}</span>
      </div>
    )
  }
]

export function BackupRestorePanel() {
  const [activeTab, setActiveTab] = useState('backups')
  const [isCreatingBackup, setIsCreatingBackup] = useState(false)
  const [isRestoring, setIsRestoring] = useState(false)
  const [showRestoreDialog, setShowRestoreDialog] = useState(false)
  const [selectedBackup, setSelectedBackup] = useState<BackupJob | null>(null)
  const [backupProgress, setBackupProgress] = useState(0)
  const [autoBackup, setAutoBackup] = useState(true)
  const [backupSchedule, setBackupSchedule] = useState('daily')
  const [retentionDays, setRetentionDays] = useState('30')
  const [remoteStorage, setRemoteStorage] = useState(false)

  const createBackup = async () => {
    setIsCreatingBackup(true)
    setBackupProgress(0)

    const interval = setInterval(() => {
      setBackupProgress(prev => {
        if (prev >= 100) {
          clearInterval(interval)
          return 100
        }
        return prev + 10
      })
    }, 500)

    await new Promise(resolve => setTimeout(resolve, 5500))
    setIsCreatingBackup(false)
    toast.success('Backup created successfully')
  }

  const restoreBackup = async () => {
    if (!selectedBackup) return
    setIsRestoring(true)
    await new Promise(resolve => setTimeout(resolve, 3000))
    setIsRestoring(false)
    setShowRestoreDialog(false)
    toast.success(`Restored from backup: ${selectedBackup.name}`)
  }

  const deleteBackup = (backup: BackupJob) => {
    toast.success(`Deleted backup: ${backup.name}`)
  }

  const downloadBackup = (backup: BackupJob) => {
    toast.success(`Downloading backup: ${backup.name}`)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Backup & Restore</h1>
          <p className="text-muted-foreground">
            Manage configuration backups and restore points
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => setShowRestoreDialog(true)}>
            <RotateCcw className="mr-2 h-4 w-4" />
            Restore
          </Button>
          <Button onClick={createBackup} disabled={isCreatingBackup}>
            {isCreatingBackup ? (
              <>
                <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                Backing up...
              </>
            ) : (
              <>
                <Database className="mr-2 h-4 w-4" />
                Create Backup
              </>
            )}
          </Button>
        </div>
      </div>

      {isCreatingBackup && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Backup Progress</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <Progress value={backupProgress} />
            <p className="text-sm text-muted-foreground">{backupProgress}% complete</p>
          </CardContent>
        </Card>
      )}

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="backups">Backups</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
          <TabsTrigger value="storage">Storage</TabsTrigger>
        </TabsList>

        <TabsContent value="backups" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Backup History</CardTitle>
              <CardDescription>View and manage your backup history</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={mockBackups}
                columns={backupColumns}
                emptyMessage="No backups found"
                actions={[
                  {
                    label: 'Download',
                    icon: Download,
                    onClick: downloadBackup
                  },
                  {
                    label: 'Restore',
                    icon: RotateCcw,
                    onClick: (backup) => {
                      setSelectedBackup(backup)
                      setShowRestoreDialog(true)
                    }
                  },
                  {
                    label: 'Delete',
                    icon: Trash2,
                    variant: 'destructive',
                    onClick: deleteBackup
                  }
                ]}
              />
            </CardContent>
          </Card>

          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Storage Used</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{formatBytes(1024 * 1024 * 1024)}</div>
                <p className="text-xs text-muted-foreground">of 10 GB allocated</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Last Backup</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">24h ago</div>
                <p className="text-xs text-muted-foreground">Daily Backup completed</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Backup Count</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">12</div>
                <p className="text-xs text-muted-foreground">7 automatic, 5 manual</p>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Automatic Backup Settings</CardTitle>
              <CardDescription>Configure scheduled backup behavior</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Enable Automatic Backups</Label>
                  <p className="text-sm text-muted-foreground">
                    Automatically create backups on a schedule
                  </p>
                </div>
                <Switch checked={autoBackup} onCheckedChange={setAutoBackup} />
              </div>

              <Separator />

              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label>Backup Schedule</Label>
                  <Select value={backupSchedule} onValueChange={setBackupSchedule}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="hourly">Every Hour</SelectItem>
                      <SelectItem value="daily">Daily</SelectItem>
                      <SelectItem value="weekly">Weekly</SelectItem>
                      <SelectItem value="monthly">Monthly</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Retention Period (days)</Label>
                  <Input
                    type="number"
                    value={retentionDays}
                    onChange={(e) => setRetentionDays(e.target.value)}
                    min="1"
                    max="365"
                  />
                </div>
              </div>

              <div className="space-y-2">
                <Label>Backup Contents</Label>
                <div className="grid gap-2 md:grid-cols-2">
                  {[
                    { id: 'config', label: 'Configuration Files', checked: true },
                    { id: 'certs', label: 'SSL Certificates', checked: true },
                    { id: 'logs', label: 'Application Logs', checked: false },
                    { id: 'metrics', label: 'Metrics Data', checked: false }
                  ].map(item => (
                    <div key={item.id} className="flex items-center gap-2">
                      <Switch defaultChecked={item.checked} />
                      <Label className="text-sm">{item.label}</Label>
                    </div>
                  ))}
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="storage" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Storage Locations</CardTitle>
              <CardDescription>Configure backup storage destinations</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-4">
                <div className="flex items-center justify-between rounded-lg border p-4">
                  <div className="flex items-center gap-3">
                    <HardDrive className="h-8 w-8 text-muted-foreground" />
                    <div>
                      <p className="font-medium">Local Storage</p>
                      <p className="text-sm text-muted-foreground">/var/lib/openlb/backups</p>
                    </div>
                  </div>
                  <Badge>Default</Badge>
                </div>

                <div className="flex items-center justify-between rounded-lg border p-4">
                  <div className="flex items-center gap-3">
                    <Cloud className="h-8 w-8 text-muted-foreground" />
                    <div>
                      <p className="font-medium">Amazon S3</p>
                      <p className="text-sm text-muted-foreground">s3://openlb-backups</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch checked={remoteStorage} onCheckedChange={setRemoteStorage} />
                    <Button variant="ghost" size="icon">
                      <Settings className="h-4 w-4" />
                    </Button>
                  </div>
                </div>

                <div className="flex items-center justify-between rounded-lg border p-4">
                  <div className="flex items-center gap-3">
                    <Server className="h-8 w-8 text-muted-foreground" />
                    <div>
                      <p className="font-medium">SFTP Server</p>
                      <p className="text-sm text-muted-foreground">Not configured</p>
                    </div>
                  </div>
                  <Button variant="outline" size="sm">Configure</Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <Dialog open={showRestoreDialog} onOpenChange={setShowRestoreDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Restore from Backup</DialogTitle>
            <DialogDescription>
              Select a backup to restore your configuration
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="rounded-lg border bg-muted/50 p-4">
              <div className="flex items-start gap-3">
                <AlertTriangle className="h-5 w-5 text-yellow-500" />
                <div>
                  <p className="font-medium">Warning</p>
                  <p className="text-sm text-muted-foreground">
                    Restoring will overwrite your current configuration.
                    Consider creating a backup first.
                  </p>
                </div>
              </div>
            </div>

            <Select
              value={selectedBackup?.id}
              onValueChange={(value) => {
                const backup = mockBackups.find(b => b.id === value)
                setSelectedBackup(backup || null)
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select backup to restore" />
              </SelectTrigger>
              <SelectContent>
                {mockBackups.filter(b => b.status === 'completed').map(backup => (
                  <SelectItem key={backup.id} value={backup.id}>
                    {backup.name} - {formatDate(backup.createdAt)} ({formatBytes(backup.size)})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {selectedBackup && (
              <div className="rounded-lg border p-4 space-y-2">
                <p className="text-sm"><strong>Name:</strong> {selectedBackup.name}</p>
                <p className="text-sm"><strong>Created:</strong> {formatDate(selectedBackup.createdAt)}</p>
                <p className="text-sm"><strong>Size:</strong> {formatBytes(selectedBackup.size)}</p>
                <p className="text-sm"><strong>Location:</strong> {selectedBackup.location}</p>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRestoreDialog(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={restoreBackup}
              disabled={!selectedBackup || isRestoring}
            >
              {isRestoring ? (
                <>
                  <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                  Restoring...
                </>
              ) : (
                <>
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Restore Backup
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
