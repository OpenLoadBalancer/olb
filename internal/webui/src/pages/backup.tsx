import { useState, useRef } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Download, Upload, RotateCcw, AlertCircle, FileJson, CheckCircle, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { useConfig } from "@/hooks/use-query"
import { api } from "@/lib/api"
import { LoadingCard } from "@/components/ui/loading"
import { type OLBConfig } from "@/types"

export function BackupRestorePage() {
  useDocumentTitle("Backup & Restore")
  const { data: config, refetch, isLoading, error } = useConfig()
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [importPreview, setImportPreview] = useState<Record<string, unknown> | null>(null)
  const [importing, setImporting] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const handleExport = () => {
    const exportData = {
      _source: "openloadbalancer",
      _exported: new Date().toISOString(),
      config: config || {},
    }
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: "application/json" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `olb-config-${new Date().toISOString().split("T")[0]}.json`
    a.click()
    URL.revokeObjectURL(url)
    toast.success("Configuration exported")
  }

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return

    const reader = new FileReader()
    reader.onload = (ev) => {
      try {
        const parsed = JSON.parse(ev.target?.result as string)
        setImportPreview(parsed)
      } catch {
        toast.error("Invalid JSON file")
        setImportPreview(null)
      }
    }
    reader.readAsText(file)
  }

  const handleImport = async () => {
    if (!importPreview) return
    setImporting(true)
    try {
      toast.info("Configuration loaded. Reload to apply changes.")
      // Download imported config so user can save it
      const blob = new Blob([JSON.stringify(importPreview.config || importPreview, null, 2)], {
        type: "application/json",
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = "olb-imported-config.json"
      a.click()
      URL.revokeObjectURL(url)
      setImportDialogOpen(false)
      setImportPreview(null)
      if (fileInputRef.current) fileInputRef.current.value = ""
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Import failed"
      toast.error(message)
    } finally {
      setImporting(false)
    }
  }

  const handleReload = async () => {
    try {
      await api.reload()
      toast.success("Configuration reloaded from disk")
      refetch()
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to reload configuration"
      toast.error(message)
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Backup & Restore</h1>
          <p className="text-muted-foreground">Export and import configuration</p>
        </div>
        <LoadingCard />
        <LoadingCard />
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Backup & Restore</h1>
          <p className="text-muted-foreground">Export and import configuration</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-destructive">Failed to load configuration: {error.message}</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4"  aria-hidden="true" /> Retry
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  const c = (config?.data ?? config) as OLBConfig | undefined
  const configSections = [
    { label: "Listeners", count: c?.listeners?.length ?? 0 },
    { label: "Pools", count: c?.pools?.length ?? 0 },
    { label: "Middleware", count: c?.middleware ? Object.keys(c.middleware).length : 0 },
    { label: "WAF", enabled: c?.waf?.enabled },
  ]

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Backup & Restore</h1>
          <p className="text-muted-foreground">Export and import configuration</p>
        </div>
        <Button variant="outline" onClick={handleReload}>
          <RotateCcw className="mr-2 h-4 w-4"  aria-hidden="true" />
          Reload Configuration
        </Button>
      </div>

      <Alert>
        <AlertDescription>
          Configuration is managed via the config file on disk. Use Export to download the current
          running config as JSON. Edit the config file and click Reload to apply changes.
        </AlertDescription>
      </Alert>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FileJson className="h-5 w-5"  aria-hidden="true" />
              Current Configuration
            </CardTitle>
            <CardDescription>Overview of the running configuration</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {configSections.map((s) => (
              <div key={s.label} className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">{s.label}</span>
                {"count" in s ? (
                  <Badge variant="secondary">{s.count}</Badge>
                ) : (
                  <Badge variant={s.enabled ? "default" : "secondary"}>
                    {s.enabled ? "Enabled" : "Disabled"}
                  </Badge>
                )}
              </div>
            ))}
            <div className="pt-2">
              <Button onClick={handleExport} className="w-full">
                <Download className="mr-2 h-4 w-4"  aria-hidden="true" />
                Export Configuration as JSON
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Upload className="h-5 w-5"  aria-hidden="true" />
              Import Configuration
            </CardTitle>
            <CardDescription>Load a configuration from a JSON file</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="text-sm text-muted-foreground">
              Import a previously exported OLB configuration file. The imported file can be used as
              a reference for editing the config file on disk.
            </div>
            <Button variant="outline" className="w-full" onClick={() => setImportDialogOpen(true)}>
              <Upload className="mr-2 h-4 w-4"  aria-hidden="true" />
              Import Configuration File
            </Button>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Configuration Sections</CardTitle>
          <CardDescription>Detailed view of each config section</CardDescription>
        </CardHeader>
        <CardContent>
          {config ? (
            <div className="space-y-2">
              {Object.entries(config as Record<string, unknown>).map(([key, value]) => (
                <div key={key} className="flex items-center justify-between p-3 rounded-lg border">
                  <span className="font-medium text-sm capitalize">{key.replace(/_/g, " ")}</span>
                  <Badge variant="outline" className="text-xs font-mono">
                    {typeof value === "object" && value !== null
                      ? Array.isArray(value)
                        ? `${value.length} items`
                        : "configured"
                      : String(value)}
                  </Badge>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground text-center py-4">
              No configuration loaded
            </p>
          )}
        </CardContent>
      </Card>

      {/* Import Dialog */}
      <Dialog open={importDialogOpen} onOpenChange={setImportDialogOpen}>
        <DialogContent className="sm:max-w-[500px]">
          <DialogHeader>
            <DialogTitle>Import Configuration</DialogTitle>
            <DialogDescription>Select a JSON configuration file to import.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="import-file">Configuration File</Label>
              <Input
                id="import-file"
                type="file"
                accept=".json"
                ref={fileInputRef}
                onChange={handleFileSelect}
              />
            </div>
            {importPreview && (
              <div className="p-3 bg-muted rounded-lg space-y-2">
                <div className="flex items-center gap-2 text-green-600">
                  <CheckCircle className="h-4 w-4"  aria-hidden="true" />
                  <span className="text-sm font-medium">File parsed successfully</span>
                </div>
                <div className="text-xs text-muted-foreground">
                  {importPreview._source === "openloadbalancer"
                    ? `OLB export from ${importPreview._exported || "unknown date"}`
                    : "Generic JSON configuration file"}
                </div>
                {typeof importPreview.config === 'object' && importPreview.config !== null && (
                  <div className="text-xs text-muted-foreground">
                    Sections: {Object.keys(importPreview.config).join(", ")}
                  </div>
                )}
              </div>
            )}
            <div className="text-sm text-amber-600 flex items-start gap-2">
              <AlertCircle className="h-4 w-4 mt-0.5 shrink-0"  aria-hidden="true" />
              <p>
                The imported configuration will be downloaded as a JSON file. Copy it to the config
                file path and reload to apply.
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setImportDialogOpen(false); setImportPreview(null) }}>
              Cancel
            </Button>
            <Button onClick={handleImport} disabled={!importPreview || importing}>
              <Download className="mr-2 h-4 w-4"  aria-hidden="true" />
              {importing ? "Importing..." : "Download Imported Config"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
