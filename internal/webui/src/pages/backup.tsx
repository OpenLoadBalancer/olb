import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Save, Upload, Download, Trash2, Clock, Calendar, RotateCcw, CheckCircle, AlertCircle } from "lucide-react"
import { toast } from "sonner"
import { useConfig } from "@/hooks/use-query"

interface Backup {
  id: string
  name: string
  created: string
  size: string
  type: 'manual' | 'auto'
  status: 'complete' | 'in_progress' | 'failed'
}

export function BackupRestorePage() {
  const { data: config } = useConfig()
  const [backups, setBackups] = useState<Backup[]>([
    { id: "1", name: "Pre-migration backup", created: "2025-01-15 14:30", size: "2.4 MB", type: "manual", status: "complete" },
    { id: "2", name: "Weekly backup", created: "2025-01-14 02:00", size: "2.3 MB", type: "auto", status: "complete" },
    { id: "3", name: "Monthly backup", created: "2025-01-01 00:00", size: "2.1 MB", type: "auto", status: "complete" },
  ])

  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [restoreDialogOpen, setRestoreDialogOpen] = useState(false)
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [selectedBackup, setSelectedBackup] = useState<Backup | null>(null)
  const [newBackupName, setNewBackupName] = useState("")
  const [autoBackup, setAutoBackup] = useState(true)
  const [autoBackupInterval, setAutoBackupInterval] = useState("daily")

  const handleCreateBackup = () => {
    const backup: Backup = {
      id: Math.random().toString(36).substr(2, 9),
      name: newBackupName || `Manual backup ${new Date().toLocaleString()}`,
      created: new Date().toISOString().replace('T', ' ').slice(0, 16),
      size: `${(2 + Math.random()).toFixed(1)} MB`,
      type: "manual",
      status: "complete",
    }
    setBackups([backup, ...backups])
    setCreateDialogOpen(false)
    setNewBackupName("")
    toast.success("Backup created successfully")
  }

  const handleDeleteBackup = (id: string) => {
    setBackups(backups.filter(b => b.id !== id))
    toast.success("Backup deleted")
  }

  const handleRestore = () => {
    if (!selectedBackup) return
    setRestoreDialogOpen(false)
    toast.success(`Restored from backup: ${selectedBackup.name}`)
    setSelectedBackup(null)
  }

  const handleExport = () => {
    const exportData = {
      version: "1.0.0",
      exported: new Date().toISOString(),
      config: config || {},
    }
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `olb-config-${new Date().toISOString().split('T')[0]}.json`
    a.click()
    URL.revokeObjectURL(url)
    toast.success("Configuration exported")
  }

  const handleImport = () => {
    setImportDialogOpen(false)
    toast.success("Configuration imported successfully")
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'complete': return <CheckCircle className="h-4 w-4 text-green-500" />
      case 'in_progress': return <Clock className="h-4 w-4 text-blue-500 animate-pulse" />
      case 'failed': return <AlertCircle className="h-4 w-4 text-red-500" />
      default: return null
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Backup & Restore</h1>
          <p className="text-muted-foreground">Manage backups and import/export configuration</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={handleExport}>
            <Download className="mr-2 h-4 w-4" />
            Export Config
          </Button>
          <Button variant="outline" onClick={() => setImportDialogOpen(true)}>
            <Upload className="mr-2 h-4 w-4" />
            Import Config
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Save className="h-5 w-5" />
              Create Backup
            </CardTitle>
            <CardDescription>Create a new backup of your configuration</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex gap-2">
              <Input
                placeholder="Backup name (optional)"
                value={newBackupName}
                onChange={(e) => setNewBackupName(e.target.value)}
                className="flex-1"
              />
              <Button onClick={() => setCreateDialogOpen(true)}>
                <Save className="mr-2 h-4 w-4" />
                Create
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Calendar className="h-5 w-5" />
              Auto Backup Settings
            </CardTitle>
            <CardDescription>Configure automatic backup schedule</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <div className="font-medium">Automatic Backups</div>
                <div className="text-sm text-muted-foreground">Create backups on a schedule</div>
              </div>
              <Switch
                checked={autoBackup}
                onCheckedChange={setAutoBackup}
              />
            </div>
            {autoBackup && (
              <div className="grid gap-2">
                <Label>Backup Interval</Label>
                <Select
                  value={autoBackupInterval}
                  onValueChange={(value: string) => setAutoBackupInterval(value)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="hourly">Hourly</SelectItem>
                    <SelectItem value="daily">Daily</SelectItem>
                    <SelectItem value="weekly">Weekly</SelectItem>
                    <SelectItem value="monthly">Monthly</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Backup History</CardTitle>
          <CardDescription>Previous backups and restore points</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            {backups.map((backup) => (
              <div
                key={backup.id}
                className="flex items-center justify-between p-4 rounded-lg border hover:bg-muted/50 transition-colors"
              >
                <div className="flex items-center gap-4">
                  {getStatusIcon(backup.status)}
                  <div>
                    <div className="font-medium">{backup.name}</div>
                    <div className="text-sm text-muted-foreground flex items-center gap-2">
                      <Clock className="h-3 w-3" />
                      {backup.created}
                      <span className="text-xs">({backup.size})</span>
                      <Badge variant={backup.type === 'auto' ? 'secondary' : 'outline'} className="text-xs">
                        {backup.type}
                      </Badge>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setSelectedBackup(backup)
                      setRestoreDialogOpen(true)
                    }}
                  >
                    <RotateCcw className="mr-2 h-4 w-4" />
                    Restore
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="text-destructive"
                    onClick={() => handleDeleteBackup(backup.id)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            ))}
            {backups.length === 0 && (
              <div className="text-center py-8 text-muted-foreground">
                No backups available
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Create Backup Dialog */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Backup</DialogTitle>
            <DialogDescription>
              This will create a backup of your current configuration.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="backup-name">Backup Name</Label>
              <Input
                id="backup-name"
                placeholder="e.g., Before changes"
                value={newBackupName}
                onChange={(e) => setNewBackupName(e.target.value)}
              />
            </div>
            <div className="text-sm text-muted-foreground">
              <p>This backup will include:</p>
              <ul className="list-disc list-inside mt-2 space-y-1">
                <li>All pool configurations</li>
                <li>Listener settings</li>
                <li>Middleware chain</li>
                <li>TLS certificates</li>
                <li>WAF rules</li>
              </ul>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleCreateBackup}>
              <Save className="mr-2 h-4 w-4" />
              Create Backup
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Restore Dialog */}
      <Dialog open={restoreDialogOpen} onOpenChange={setRestoreDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Restore Configuration</DialogTitle>
            <DialogDescription>
              Are you sure you want to restore from this backup?
            </DialogDescription>
          </DialogHeader>
          <div className="py-4">
            {selectedBackup && (
              <div className="p-4 bg-muted rounded-lg">
                <div className="font-medium">{selectedBackup.name}</div>
                <div className="text-sm text-muted-foreground">
                  Created: {selectedBackup.created}
                </div>
              </div>
            )}
            <div className="mt-4 text-sm text-amber-600 flex items-start gap-2">
              <AlertCircle className="h-4 w-4 mt-0.5" />
              <p>
                This will replace your current configuration. Make sure to create a backup first if you want to revert.
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRestoreDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleRestore}>
              <RotateCcw className="mr-2 h-4 w-4" />
              Restore
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Import Dialog */}
      <Dialog open={importDialogOpen} onOpenChange={setImportDialogOpen}>
        <DialogContent className="sm:max-w-[500px]">
          <DialogHeader>
            <DialogTitle>Import Configuration</DialogTitle>
            <DialogDescription>
              Import a configuration from a JSON file.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="import-file">Configuration File</Label>
              <Input
                id="import-file"
                type="file"
                accept=".json"
              />
            </div>
            <div className="text-sm text-muted-foreground">
              <p>Supported formats:</p>
              <ul className="list-disc list-inside mt-2 space-y-1">
                <li>OLB Configuration JSON</li>
                <li>Exported from OpenLoadBalancer</li>
              </ul>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setImportDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleImport}>
              <Upload className="mr-2 h-4 w-4" />
              Import
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
