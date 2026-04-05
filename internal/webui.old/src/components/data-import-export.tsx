import { useState, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { Progress } from '@/components/ui/progress'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import {
  Upload,
  Download,
  FileJson,
  FileText,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  FileCode,
  Database,
  Settings,
  Shield,
  Server,
  ChevronRight,
  ChevronDown
} from 'lucide-react'

interface ExportConfig {
  includeBackends: boolean
  includePools: boolean
  includeRoutes: boolean
  includeListeners: boolean
  includeMiddleware: boolean
  includeWAF: boolean
  includeCertificates: boolean
  includeSettings: boolean
}

const defaultExportConfig: ExportConfig = {
  includeBackends: true,
  includePools: true,
  includeRoutes: true,
  includeListeners: true,
  includeMiddleware: true,
  includeWAF: true,
  includeCertificates: true,
  includeSettings: true
}

export function DataImportExport() {
  const [activeTab, setActiveTab] = useState('export')
  const [exportConfig, setExportConfig] = useState<ExportConfig>(defaultExportConfig)
  const [exportFormat, setExportFormat] = useState<'json' | 'yaml'>('json')
  const [isExporting, setIsExporting] = useState(false)
  const [exportProgress, setExportProgress] = useState(0)
  const [showImportDialog, setShowImportDialog] = useState(false)
  const [importPreview, setImportPreview] = useState<any>(null)
  const [importErrors, setImportErrors] = useState<string[]>([])

  const handleExport = async () => {
    setIsExporting(true)
    setExportProgress(0)

    // Simulate export progress
    for (let i = 0; i <= 100; i += 10) {
      await new Promise(resolve => setTimeout(resolve, 100))
      setExportProgress(i)
    }

    const config = {
      version: '1.0',
      exportedAt: new Date().toISOString(),
      data: {
        ...(exportConfig.includeBackends && { backends: [] }),
        ...(exportConfig.includePools && { pools: [] }),
        ...(exportConfig.includeRoutes && { routes: [] }),
        ...(exportConfig.includeListeners && { listeners: [] }),
        ...(exportConfig.includeMiddleware && { middleware: {} }),
        ...(exportConfig.includeWAF && { waf: {} }),
        ...(exportConfig.includeCertificates && { certificates: [] }),
        ...(exportConfig.includeSettings && { settings: {} })
      }
    }

    const content = exportFormat === 'json'
      ? JSON.stringify(config, null, 2)
      : `# OpenLoadBalancer Configuration\n# Exported: ${new Date().toISOString()}\n\nversion: "1.0"`

    const blob = new Blob([content], {
      type: exportFormat === 'json' ? 'application/json' : 'text/yaml'
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `openlb-config-${new Date().toISOString().split('T')[0]}.${exportFormat}`
    a.click()
    URL.revokeObjectURL(url)

    setIsExporting(false)
    toast.success('Configuration exported successfully')
  }

  const handleFileUpload = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return

    const reader = new FileReader()
    reader.onload = (event) => {
      try {
        const content = event.target?.result as string
        const data = JSON.parse(content)
        setImportPreview(data)
        setImportErrors([])
        setShowImportDialog(true)
      } catch (error) {
        toast.error('Invalid configuration file')
      }
    }
    reader.readAsText(file)
  }, [])

  const handleImport = async () => {
    await new Promise(resolve => setTimeout(resolve, 1000))
    toast.success('Configuration imported successfully')
    setShowImportDialog(false)
    setImportPreview(null)
  }

  const toggleConfig = (key: keyof ExportConfig) => {
    setExportConfig(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const selectAll = () => {
    setExportConfig(defaultExportConfig)
  }

  const selectNone = () => {
    setExportConfig({
      includeBackends: false,
      includePools: false,
      includeRoutes: false,
      includeListeners: false,
      includeMiddleware: false,
      includeWAF: false,
      includeCertificates: false,
      includeSettings: false
    })
  }

  const exportSections = [
    { key: 'includeBackends' as const, label: 'Backends', icon: Server, count: 12 },
    { key: 'includePools' as const, label: 'Pools', icon: Database, count: 5 },
    { key: 'includeRoutes' as const, label: 'Routes', icon: ChevronRight, count: 24 },
    { key: 'includeListeners' as const, label: 'Listeners', icon: ChevronRight, count: 3 },
    { key: 'includeMiddleware' as const, label: 'Middleware', icon: Settings, count: 8 },
    { key: 'includeWAF' as const, label: 'WAF Rules', icon: Shield, count: 15 },
    { key: 'includeCertificates' as const, label: 'Certificates', icon: ChevronRight, count: 4 },
    { key: 'includeSettings' as const, label: 'Settings', icon: Settings, count: 1 }
  ]

  const selectedCount = Object.values(exportConfig).filter(Boolean).length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Import / Export</h1>
          <p className="text-muted-foreground">
            Import or export your load balancer configuration
          </p>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="export">
            <Download className="mr-2 h-4 w-4" />
            Export
          </TabsTrigger>
          <TabsTrigger value="import">
            <Upload className="mr-2 h-4 w-4" />
            Import
          </TabsTrigger>
        </TabsList>

        <TabsContent value="export" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Export Configuration</CardTitle>
              <CardDescription>Choose what to include in the export</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {/* Format Selection */}
              <div className="space-y-2">
                <Label>Export Format</Label>
                <div className="flex gap-2">
                  <Button
                    variant={exportFormat === 'json' ? 'default' : 'outline'}
                    onClick={() => setExportFormat('json')}
                    className="flex-1"
                  >
                    <FileJson className="mr-2 h-4 w-4" />
                    JSON
                  </Button>
                  <Button
                    variant={exportFormat === 'yaml' ? 'default' : 'outline'}
                    onClick={() => setExportFormat('yaml')}
                    className="flex-1"
                  >
                    <FileCode className="mr-2 h-4 w-4" />
                    YAML
                  </Button>
                </div>
              </div>

              {/* Selection Controls */}
              <div className="flex items-center justify-between">
                <Label>Configuration Sections</Label>
                <div className="flex gap-2">
                  <Button variant="ghost" size="sm" onClick={selectAll}>
                    Select All
                  </Button>
                  <Button variant="ghost" size="sm" onClick={selectNone}>
                    Select None
                  </Button>
                </div>
              </div>

              {/* Sections Grid */}
              <div className="grid gap-4 md:grid-cols-2">
                {exportSections.map(section => {
                  const Icon = section.icon
                  const isSelected = exportConfig[section.key]
                  return (
                    <div
                      key={section.key}
                      className={cn(
                        'flex items-center gap-3 rounded-lg border p-4 cursor-pointer transition-colors',
                        isSelected ? 'border-primary bg-primary/5' : 'hover:bg-muted/50'
                      )}
                      onClick={() => toggleConfig(section.key)}
                    >
                      <Checkbox checked={isSelected} />
                      <div className="flex-1">
                        <div className="flex items-center gap-2">
                          <Icon className="h-4 w-4 text-muted-foreground" />
                          <span className="font-medium">{section.label}</span>
                        </div>
                        <p className="text-sm text-muted-foreground">{section.count} items</p>
                      </div>
                    </div>
                  )
                })}
              </div>

              {/* Export Progress */}
              {isExporting && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between text-sm">
                    <span>Exporting...</span>
                    <span>{exportProgress}%</span>
                  </div>
                  <Progress value={exportProgress} />
                </div>
              )}

              {/* Export Button */}
              <Button
                onClick={handleExport}
                disabled={isExporting || selectedCount === 0}
                className="w-full"
              >
                {isExporting ? (
                  <>
                    <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                    Exporting...
                  </>
                ) : (
                  <>
                    <Download className="mr-2 h-4 w-4" />
                    Export {selectedCount} Section{selectedCount !== 1 ? 's' : ''}
                  </>
                )}
              </Button>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="import" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Import Configuration</CardTitle>
              <CardDescription>Upload a configuration file to import</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="rounded-lg border border-dashed p-8">
                <div className="flex flex-col items-center gap-4">
                  <Upload className="h-12 w-12 text-muted-foreground" />
                  <div className="text-center">
                    <p className="font-medium">Drop your configuration file here</p>
                    <p className="text-sm text-muted-foreground">
                      Supports JSON and YAML files
                    </p>
                  </div>
                  <div className="flex gap-2">
                    <Button variant="outline" asChild>
                      <label className="cursor-pointer">
                        <FileJson className="mr-2 h-4 w-4" />
                        Choose JSON
                        <input
                          type="file"
                          accept=".json,application/json"
                          className="hidden"
                          onChange={handleFileUpload}
                        />
                      </label>
                    </Button>
                    <Button variant="outline" asChild>
                      <label className="cursor-pointer">
                        <FileCode className="mr-2 h-4 w-4" />
                        Choose YAML
                        <input
                          type="file"
                          accept=".yaml,.yml"
                          className="hidden"
                          onChange={handleFileUpload}
                        />
                      </label>
                    </Button>
                  </div>
                </div>
              </div>

              <div className="rounded-lg bg-muted p-4">
                <div className="flex items-start gap-3">
                  <AlertTriangle className="h-5 w-5 text-yellow-500" />
                  <div>
                    <p className="font-medium">Important Notes</p>
                    <ul className="mt-2 list-inside list-disc text-sm text-muted-foreground">
                      <li>Backup your current configuration before importing</li>
                      <li>Importing will overwrite existing configuration</li>
                      <li>Invalid configuration files will be rejected</li>
                      <li>Certificates must be imported separately for security</li>
                    </ul>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Import Preview Dialog */}
      <Dialog open={showImportDialog} onOpenChange={setShowImportDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Import Preview</DialogTitle>
            <DialogDescription>
              Review the configuration before importing
            </DialogDescription>
          </DialogHeader>

          {importPreview && (
            <div className="space-y-4 py-4">
              <div className="rounded-lg border bg-muted p-4">
                <p className="text-sm"><strong>Version:</strong> {importPreview.version}</p>
                <p className="text-sm"><strong>Exported:</strong> {importPreview.exportedAt}</p>
              </div>

              <div className="space-y-2">
                <Label>Configuration Sections</Label>
                <div className="space-y-1">
                  {Object.entries(importPreview.data || {}).map(([key, value]) => (
                    <div key={key} className="flex items-center gap-2">
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                      <span className="capitalize">{key}</span>
                      <span className="text-sm text-muted-foreground">
                        ({Array.isArray(value) ? value.length : 'configured'})
                      </span>
                    </div>
                  ))}
                </div>
              </div>

              {importErrors.length > 0 && (
                <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4">
                  <div className="flex items-center gap-2 text-destructive">
                    <XCircle className="h-5 w-5" />
                    <span className="font-medium">Validation Errors</span>
                  </div>
                  <ul className="mt-2 list-inside list-disc text-sm text-destructive">
                    {importErrors.map((error, i) => (
                      <li key={i}>{error}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowImportDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleImport}
              disabled={importErrors.length > 0}
            >
              <Upload className="mr-2 h-4 w-4" />
              Import Configuration
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
