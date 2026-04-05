import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DataTable } from '@/components/data-table'
import { toast } from 'sonner'
import { cn, formatDate } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Clock,
  Calendar,
  Play,
  Pause,
  RotateCw,
  Trash2,
  Edit,
  CheckCircle2,
  XCircle,
  AlertCircle,
  History,
  Plus,
  Settings,
  Terminal,
  Globe,
  Database,
  FileText,
  Zap
} from 'lucide-react'

interface ScheduledTask {
  id: string
  name: string
  description?: string
  schedule: string // cron expression
  command: string
  enabled: boolean
  lastRun?: Date
  lastStatus?: 'success' | 'failed' | 'running'
  nextRun?: Date
  runCount: number
  failCount: number
  timeout: number // seconds
  notifyOnFailure: boolean
}

interface TaskExecution {
  id: string
  taskId: string
  taskName: string
  startedAt: Date
  completedAt?: Date
  status: 'running' | 'success' | 'failed'
  output?: string
  error?: string
  duration?: number // milliseconds
}

const mockTasks: ScheduledTask[] = [
  {
    id: '1',
    name: 'Config Backup',
    description: 'Daily configuration backup',
    schedule: '0 2 * * *',
    command: 'backup create --full',
    enabled: true,
    lastRun: new Date(Date.now() - 86400000),
    lastStatus: 'success',
    nextRun: new Date(Date.now() + 86400000),
    runCount: 245,
    failCount: 2,
    timeout: 300,
    notifyOnFailure: true
  },
  {
    id: '2',
    name: 'Log Rotation',
    description: 'Rotate and compress old logs',
    schedule: '0 0 * * *',
    command: 'logs rotate --compress',
    enabled: true,
    lastRun: new Date(Date.now() - 43200000),
    lastStatus: 'success',
    nextRun: new Date(Date.now() + 43200000),
    runCount: 890,
    failCount: 0,
    timeout: 60,
    notifyOnFailure: false
  },
  {
    id: '3',
    name: 'Health Report',
    description: 'Generate and email health report',
    schedule: '0 9 * * 1',
    command: 'report generate --type health --email',
    enabled: true,
    lastRun: new Date(Date.now() - 604800000),
    lastStatus: 'failed',
    nextRun: new Date(Date.now() + 604800000),
    runCount: 52,
    failCount: 3,
    timeout: 120,
    notifyOnFailure: true
  },
  {
    id: '4',
    name: 'Certificate Check',
    description: 'Check SSL certificate expiry',
    schedule: '0 0 * * *',
    command: 'certs check --alert-threshold 30',
    enabled: false,
    lastRun: new Date(Date.now() - 172800000),
    lastStatus: 'success',
    runCount: 365,
    failCount: 0,
    timeout: 30,
    notifyOnFailure: true
  }
]

const mockExecutions: TaskExecution[] = [
  {
    id: '1',
    taskId: '1',
    taskName: 'Config Backup',
    startedAt: new Date(Date.now() - 86400000),
    completedAt: new Date(Date.now() - 86395000),
    status: 'success',
    duration: 5000,
    output: 'Backup completed successfully. Size: 245MB'
  },
  {
    id: '2',
    taskId: '2',
    taskName: 'Log Rotation',
    startedAt: new Date(Date.now() - 43200000),
    completedAt: new Date(Date.now() - 43199000),
    status: 'success',
    duration: 1000,
    output: 'Rotated 12 log files. Compressed: 450MB'
  },
  {
    id: '3',
    taskId: '3',
    taskName: 'Health Report',
    startedAt: new Date(Date.now() - 604800000),
    completedAt: new Date(Date.now() - 604795000),
    status: 'failed',
    duration: 5000,
    error: 'Failed to connect to SMTP server'
  }
]

const taskColumns: ColumnDef<ScheduledTask>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => (
      <div>
        <p className="font-medium">{row.original.name}</p>
        {row.original.description && (
          <p className="text-xs text-muted-foreground">{row.original.description}</p>
        )}
      </div>
    )
  },
  {
    accessorKey: 'schedule',
    header: 'Schedule',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Clock className="h-4 w-4 text-muted-foreground" />
        <code className="text-xs bg-muted px-2 py-1 rounded">{row.original.schedule}</code>
      </div>
    )
  },
  {
    accessorKey: 'enabled',
    header: 'Status',
    cell: ({ row }) => (
      <Badge variant={row.original.enabled ? 'default' : 'secondary'}>
        {row.original.enabled ? 'Active' : 'Paused'}
      </Badge>
    )
  },
  {
    accessorKey: 'lastStatus',
    header: 'Last Run',
    cell: ({ row }) => {
      if (!row.original.lastStatus) return <span className="text-muted-foreground">Never</span>
      const colors = {
        success: 'text-green-500',
        failed: 'text-red-500',
        running: 'text-blue-500'
      }
      return (
        <div className="flex items-center gap-2">
          <span className={colors[row.original.lastStatus]}>
            {row.original.lastStatus.charAt(0).toUpperCase() + row.original.lastStatus.slice(1)}
          </span>
          {row.original.lastRun && (
            <span className="text-xs text-muted-foreground">
              {formatDate(row.original.lastRun)}
            </span>
          )}
        </div>
      )
    }
  },
  {
    accessorKey: 'nextRun',
    header: 'Next Run',
    cell: ({ row }) => (
      row.original.nextRun && row.original.enabled
        ? formatDate(row.original.nextRun)
        : <span className="text-muted-foreground">-</span>
    )
  }
]

const executionColumns: ColumnDef<TaskExecution>[] = [
  {
    accessorKey: 'taskName',
    header: 'Task',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Terminal className="h-4 w-4 text-muted-foreground" />
        <span>{row.original.taskName}</span>
      </div>
    )
  },
  {
    accessorKey: 'startedAt',
    header: 'Started',
    cell: ({ row }) => formatDate(row.original.startedAt)
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => {
      const icons = {
        running: RotateCw,
        success: CheckCircle2,
        failed: XCircle
      }
      const colors = {
        running: 'text-blue-500',
        success: 'text-green-500',
        failed: 'text-red-500'
      }
      const Icon = icons[row.original.status]
      return (
        <div className={cn('flex items-center gap-1', colors[row.original.status])}>
          <Icon className={cn('h-4 w-4', row.original.status === 'running' && 'animate-spin')} />
          <span className="capitalize">{row.original.status}</span>
        </div>
      )
    }
  },
  {
    accessorKey: 'duration',
    header: 'Duration',
    cell: ({ row }) => (
      row.original.duration
        ? `${(row.original.duration / 1000).toFixed(1)}s`
        : '-'
    )
  }
]

export function TaskScheduler() {
  const [tasks, setTasks] = useState<ScheduledTask[]>(mockTasks)
  const [executions] = useState<TaskExecution[]>(mockExecutions)
  const [showTaskDialog, setShowTaskDialog] = useState(false)
  const [editingTask, setEditingTask] = useState<ScheduledTask | null>(null)
  const [activeTab, setActiveTab] = useState('tasks')

  const toggleTask = (id: string) => {
    setTasks(prev =>
      prev.map(t =>
        t.id === id ? { ...t, enabled: !t.enabled } : t
      )
    )
  }

  const runTask = (id: string) => {
    toast.success('Task started manually')
  }

  const deleteTask = (id: string) => {
    toast.success('Task deleted')
  }

  const saveTask = () => {
    toast.success(editingTask ? 'Task updated' : 'Task created')
    setShowTaskDialog(false)
    setEditingTask(null)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Task Scheduler</h1>
          <p className="text-muted-foreground">
            Schedule and manage automated tasks
          </p>
        </div>
        <Button onClick={() => { setEditingTask(null); setShowTaskDialog(true) }}>
          <Plus className="mr-2 h-4 w-4" />
          New Task
        </Button>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Tasks</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{tasks.filter(t => t.enabled).length}</div>
            <p className="text-xs text-muted-foreground">of {tasks.length} total</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Runs</CardTitle>
            <History className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {tasks.reduce((sum, t) => sum + t.runCount, 0).toLocaleString()}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Success Rate</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">
              {(
                (tasks.reduce((sum, t) => sum + t.runCount - t.failCount, 0) /
                  tasks.reduce((sum, t) => sum + t.runCount, 0)) * 100
              ).toFixed(1)}%
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Failed (24h)</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">
              {tasks.reduce((sum, t) => sum + t.failCount, 0)}
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="tasks">
            <Clock className="mr-2 h-4 w-4" />
            Tasks
          </TabsTrigger>
          <TabsTrigger value="history">
            <History className="mr-2 h-4 w-4" />
            History
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings className="mr-2 h-4 w-4" />
            Settings
          </TabsTrigger>
        </TabsList>

        <TabsContent value="tasks" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Scheduled Tasks</CardTitle>
              <CardDescription>Manage cron jobs and scheduled operations</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={tasks}
                columns={taskColumns}
                actions={[
                  {
                    label: t => t.enabled ? 'Pause' : 'Resume',
                    icon: t => t.enabled ? Pause : Play,
                    onClick: (t) => toggleTask(t.id)
                  },
                  {
                    label: 'Run Now',
                    icon: Play,
                    onClick: (t) => runTask(t.id)
                  },
                  {
                    label: 'Edit',
                    icon: Edit,
                    onClick: (t) => { setEditingTask(t); setShowTaskDialog(true) }
                  },
                  {
                    label: 'Delete',
                    icon: Trash2,
                    variant: 'destructive',
                    onClick: (t) => deleteTask(t.id)
                  }
                ]}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="history" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Execution History</CardTitle>
              <CardDescription>Recent task executions</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={executions}
                columns={executionColumns}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Scheduler Settings</CardTitle>
              <CardDescription>Global task scheduler configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Enable Scheduler</Label>
                  <p className="text-sm text-muted-foreground">Run scheduled tasks automatically</p>
                </div>
                <Switch defaultChecked />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Overlap Protection</Label>
                  <p className="text-sm text-muted-foreground">Prevent same task from running concurrently</p>
                </div>
                <Switch defaultChecked />
              </div>
              <div className="space-y-2">
                <Label>Max Concurrent Tasks</Label>
                <Input type="number" defaultValue={5} />
              </div>
              <div className="space-y-2">
                <Label>Default Timeout (seconds)</Label>
                <Input type="number" defaultValue={300} />
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Task Dialog */}
      <Dialog open={showTaskDialog} onOpenChange={setShowTaskDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingTask ? 'Edit Task' : 'New Task'}</DialogTitle>
            <DialogDescription>Configure scheduled task</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Name</Label>
              <Input defaultValue={editingTask?.name} placeholder="Task name" />
            </div>
            <div className="space-y-2">
              <Label>Description</Label>
              <Input defaultValue={editingTask?.description} placeholder="Optional description" />
            </div>
            <div className="space-y-2">
              <Label>Schedule (Cron)</Label>
              <div className="flex gap-2">
                <Input defaultValue={editingTask?.schedule} placeholder="0 2 * * *" />
                <Select defaultValue="daily">
                  <SelectTrigger className="w-[140px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="hourly">Hourly</SelectItem>
                    <SelectItem value="daily">Daily</SelectItem>
                    <SelectItem value="weekly">Weekly</SelectItem>
                    <SelectItem value="monthly">Monthly</SelectItem>
                    <SelectItem value="custom">Custom</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-2">
              <Label>Command</Label>
              <Input defaultValue={editingTask?.command} placeholder="backup create --full" />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Timeout (seconds)</Label>
                <Input type="number" defaultValue={editingTask?.timeout || 300} />
              </div>
              <div className="flex items-center justify-between pt-6">
                <Label>Notify on Failure</Label>
                <Switch defaultChecked={editingTask?.notifyOnFailure} />
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowTaskDialog(false)}>
              Cancel
            </Button>
            <Button onClick={saveTask}>
              {editingTask ? 'Update Task' : 'Create Task'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
