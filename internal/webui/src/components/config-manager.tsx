import { useState, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import api from '@/lib/api'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import {
  Upload,
  Download,
  Copy,
  Check,
  AlertTriangle,
  FileJson,
  FileCode,
  RefreshCw,
  History,
  Undo,
  Play,
  CheckCircle,
  XCircle,
  FileText,
  Settings,
  Save
} from 'lucide-react'

// Mock configuration
const mockConfig = {
  version: '1.0.0',
  backends: [
    { name: 'web-01', address: '10.0.1.10:8080', weight: 1 },
    { name: 'web-02', address: '10.0.1.11:8080', weight: 1 }
  ],
  pools: [
    {
      name: 'web-pool',
      algorithm: 'round_robin',
      backends: ['web-01', 'web-02']
    }
  ],
  listeners: [
    {
      name: 'http',
      address: ':8080',
      protocol: 'http',
      routes: [
        { path: '/', pool: 'web-pool' }
      ]
    }
  ]
}

interface ConfigVersion {
  id: string
  timestamp: Date
  author: string
  message: string
  config: string
}

// Mock version history
const mockVersions: ConfigVersion[] = [
  {
    id: 'v1.0.3',
    timestamp: new Date(Date.now() - 3600000),
    author: 'admin',
    message: 'Added new backend web-03',
    config: JSON.stringify({ ...mockConfig, version: '1.0.3' }, null, 2)
  },
  {
    id: 'v1.0.2',
    timestamp: new Date(Date.now() - 86400000),
    author: 'admin',
    message: 'Updated pool algorithm',
    config: JSON.stringify({ ...mockConfig, version: '1.0.2' }, null, 2)
  },
  {
    id: 'v1.0.1',
    timestamp: new Date(Date.now() - 172800000),
    author: 'admin',
    message: 'Initial configuration',
    config: JSON.stringify({ ...mockConfig, version: '1.0.1' }, null, 2)
  }
]

export function ConfigManager() {
  const [config, setConfig] = useState(JSON.stringify(mockConfig, null, 2))
  const [validationErrors, setValidationErrors] = useState<string[]>([])
  const [isValid, setIsValid] = useState(true)
  const [activeTab, setActiveTab] = useState('editor')
  const [showDiff, setShowDiff] = useState(false)
  const [compareVersion, setCompareVersion] = useState<string>('')
  const [isDeploying, setIsDeploying] = useState(false)

  // Validate config
  const validateConfig = useCallback((configStr: string) => {
    try {
      JSON.parse(configStr)
      setIsValid(true)
      setValidationErrors([])
      return true
    } catch (e: any) {
      setIsValid(false)
      setValidationErrors([e.message])
      return false
    }
  }, [])

  const handleConfigChange = (value: string) => {
    setConfig(value)
    validateConfig(value)
  }

  const handleImport = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return

    const reader = new FileReader()
    reader.onload = (event) => {
      const content = event.target?.result as string
      setConfig(content)
      validateConfig(content)
      toast.success('Configuration imported')
    }
    reader.readAsText(file)
  }

  const handleExport = () => {
    const blob = new Blob([config], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `openlb-config-${new Date().toISOString().split('T')[0]}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    toast.success('Configuration exported')
  }

  const handleCopy = () => {
    navigator.clipboard.writeText(config)
    toast.success('Copied to clipboard')
  }

  const handleDeploy = async () => {
    if (!validateConfig(config)) {
      toast.error('Invalid configuration')
      return
    }

    setIsDeploying(true)
    try {
      // Simulate API call
      await new Promise(resolve => setTimeout(resolve, 2000))
      // await api.post('/api/v1/config/reload', JSON.parse(config))
      toast.success('Configuration deployed successfully')
    } catch {
      toast.error('Failed to deploy configuration')
    } finally {
      setIsDeploying(false)
    }
  }

  const handleRestore = (version: ConfigVersion) => {
    setConfig(version.config)
    validateConfig(version.config)
    toast.success(`Restored version ${version.id}`)
  }

  const formatDate = (date: Date) => {
    return date.toLocaleString()
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Configuration</h1>
          <p className="text-muted-foreground">
            Import, export, and manage load balancer configuration
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={handleExport}>
            <Download className="mr-2 h-4 w-4" />
            Export
          </Button>
          <Button variant="outline" onClick={() => document.getElementById('config-import')?.click()}>
            <Upload className="mr-2 h-4 w-4" />
            Import
            <input
              id="config-import"
              type="file"
              accept=".json,.yaml,.yml"
              className="hidden"
              onChange={handleImport}
            />
          </Button>
          <Button
            onClick={handleDeploy}
            disabled={!isValid || isDeploying}
            variant={isValid ? 'default' : 'destructive'}
          >
            {isDeploying ? (
              <>
                <div className="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                Deploying...
              </>
            ) : (
              <>
                <Play className="mr-2 h-4 w-4" />
                Deploy
              </>
            )}
          </Button>
        </div>
      </div>

      {/* Validation Status */}
      {!isValid && validationErrors.length > 0 && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>
            <div className="font-semibold">Validation Error</div>
            {validationErrors.map((err, i) => (
              <div key={i} className="text-sm">{err}</div>
            ))}
          </AlertDescription>
        </Alert>
      )}

      {/* Main Content */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="editor">
            <FileCode className="mr-2 h-4 w-4" />
            Editor
          </TabsTrigger>
          <TabsTrigger value="history">
            <History className="mr-2 h-4 w-4" />
            History
          </TabsTrigger>
          <TabsTrigger value="validate">
            <CheckCircle className="mr-2 h-4 w-4" />
            Validate
          </TabsTrigger>
        </TabsList>

        <TabsContent value="editor" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle>Configuration Editor</CardTitle>
                <CardDescription>Edit your load balancer configuration in JSON format</CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={handleCopy}>
                  <Copy className="mr-2 h-4 w-4" />
                  Copy
                </Button>
                <Badge variant={isValid ? 'default' : 'destructive'}>
                  {isValid ? 'Valid JSON' : 'Invalid'}
                </Badge>
              </div>
            </CardHeader>
            <CardContent>
              <Textarea
                value={config}
                onChange={(e) => handleConfigChange(e.target.value)}
                className="min-h-[500px] font-mono text-sm"
                placeholder="Paste your configuration here..."
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="history" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Version History</CardTitle>
              <CardDescription>Previous configuration versions</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {mockVersions.map((version, index) => (
                  <div
                    key={version.id}
                    className="flex items-center justify-between rounded-lg border p-4 hover:bg-muted"
                  >
                    <div className="flex items-center gap-4">
                      <div className="flex h-10 w-10 items-center justify-center rounded-full bg-muted">
                        <FileText className="h-5 w-5" />
                      </div>
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-semibold">{version.id}</span>
                          {index === 0 && (
                            <Badge variant="default">Current</Badge>
                          )}
                        </div>
                        <p className="text-sm text-muted-foreground">{version.message}</p>
                        <p className="text-xs text-muted-foreground">
                          by {version.author} on {formatDate(version.timestamp)}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleRestore(version)}
                      >
                        <Undo className="mr-2 h-4 w-4" />
                        Restore
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="validate" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Validation Results</CardTitle>
              <CardDescription>Configuration validation status</CardDescription>
            </CardHeader>
            <CardContent>
              {isValid ? (
                <div className="space-y-4">
                  <div className="flex items-center gap-2 text-green-500">
                    <CheckCircle className="h-5 w-5" />
                    <span className="font-semibold">Configuration is valid</span>
                  </div>
                  <Separator />
                  <div className="space-y-2">
                    <h4 className="font-semibold">Configuration Summary</h4>
                    <div className="grid gap-2 text-sm">
                      {(() => {
                        try {
                          const parsed = JSON.parse(config)
                          return (
                            <>
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">Version</span>
                                <span>{parsed.version || 'N/A'}</span>
                              </div>
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">Backends</span>
                                <span>{parsed.backends?.length || 0}</span>
                              </div>
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">Pools</span>
                                <span>{parsed.pools?.length || 0}</span>
                              </div>
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">Listeners</span>
                                <span>{parsed.listeners?.length || 0}</span>
                              </div>
                            </>
                          )
                        } catch {
                          return <p>Unable to parse configuration</p>
                        }
                      })()}
                    </div>
                  </div>
                </div>
              ) : (
                <div className="space-y-4">
                  <div className="flex items-center gap-2 text-destructive">
                    <XCircle className="h-5 w-5" />
                    <span className="font-semibold">Configuration has errors</span>
                  </div>
                  <div className="rounded-md bg-destructive/10 p-4 text-destructive">
                    {validationErrors.map((err, i) => (
                      <div key={i}>{err}</div>
                    ))}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
