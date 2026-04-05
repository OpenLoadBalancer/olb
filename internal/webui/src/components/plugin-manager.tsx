import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import {
  Puzzle,
  Play,
  Pause,
  Settings,
  Code,
  Trash2,
  Upload,
  Download,
  CheckCircle,
  AlertCircle,
  Zap,
  Shield,
  Globe,
  Database,
  FileJson,
  MoreHorizontal,
  RefreshCw,
  Plus,
  Terminal
} from 'lucide-react'

interface Plugin {
  id: string
  name: string
  description: string
  version: string
  author: string
  enabled: boolean
  status: 'active' | 'inactive' | 'error' | 'loading'
  hooks: string[]
  config: Record<string, unknown>
  size: string
  lastUpdated: Date
}

interface PluginEvent {
  timestamp: Date
  plugin: string
  event: string
  level: 'info' | 'warn' | 'error'
  message: string
}

const mockPlugins: Plugin[] = [
  {
    id: 'plugin-1',
    name: 'Custom Metrics',
    description: 'Export custom metrics to Prometheus',
    version: '1.2.0',
    author: 'OpenLoadBalancer',
    enabled: true,
    status: 'active',
    hooks: ['metrics', 'response'],
    config: { endpoint: '/metrics/custom', format: 'prometheus' },
    size: '24 KB',
    lastUpdated: new Date(Date.now() - 86400000 * 30)
  },
  {
    id: 'plugin-2',
    name: 'GeoIP Redirect',
    description: 'Redirect users based on geolocation',
    version: '0.9.5',
    author: 'Community',
    enabled: true,
    status: 'active',
    hooks: ['request', 'routing'],
    config: { database: '/data/GeoLite2.mmdb' },
    size: '156 KB',
    lastUpdated: new Date(Date.now() - 86400000 * 15)
  },
  {
    id: 'plugin-3',
    name: 'JWT Validation',
    description: 'Validate JWT tokens on requests',
    version: '2.0.1',
    author: 'OpenLoadBalancer',
    enabled: false,
    status: 'inactive',
    hooks: ['request', 'auth'],
    config: { secret: '', algorithm: 'RS256' },
    size: '45 KB',
    lastUpdated: new Date(Date.now() - 86400000 * 7)
  }
]

const mockEvents: PluginEvent[] = [
  { timestamp: new Date(Date.now() - 5000), plugin: 'Custom Metrics', event: 'hook_executed', level: 'info', message: 'Metrics exported successfully' },
  { timestamp: new Date(Date.now() - 30000), plugin: 'GeoIP Redirect', event: 'redirect', level: 'info', message: 'Redirected 192.168.1.100 to US region' },
  { timestamp: new Date(Date.now() - 60000), plugin: 'JWT Validation', event: 'error', level: 'error', message: 'Failed to validate token: expired' }
]

const hookTypes = [
  { value: 'request', label: 'Request', description: 'Process incoming requests' },
  { value: 'response', label: 'Response', description: 'Modify responses' },
  { value: 'routing', label: 'Routing', description: 'Custom routing logic' },
  { value: 'metrics', label: 'Metrics', description: 'Custom metrics collection' },
  { value: 'auth', label: 'Authentication', description: 'Custom auth handlers' },
  { value: 'health', label: 'Health Check', description: 'Custom health checks' }
]

function PluginCard({
  plugin,
  onToggle,
  onDelete,
  onConfigure
}: {
  plugin: Plugin
  onToggle: () => void
  onDelete: () => void
  onConfigure: () => void
}) {
  const statusColors = {
    active: 'bg-green-500',
    inactive: 'bg-muted',
    error: 'bg-destructive',
    loading: 'bg-amber-500'
  }

  return (
    <Card className={cn(!plugin.enabled && 'opacity-60')}>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-muted">
              <Puzzle className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <CardTitle className="text-base">{plugin.name}</CardTitle>
                <div className={cn('h-2 w-2 rounded-full', statusColors[plugin.status])} />
              </div>
              <CardDescription className="flex items-center gap-2">
                v{plugin.version}
                <span className="text-muted-foreground">by {plugin.author}</span>
              </CardDescription>
            </div>
          </div>
          <Switch checked={plugin.enabled} onCheckedChange={onToggle} />
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">{plugin.description}</p>

        <div className="flex flex-wrap gap-1">
          {plugin.hooks.map(hook => (
            <Badge key={hook} variant="outline" className="text-xs capitalize">
              {hook}
            </Badge>
          ))}
        </div>

        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>{plugin.size}</span>
          <span>Updated {plugin.lastUpdated.toLocaleDateString()}</span>
        </div>

        <div className="flex gap-2">
          <Button variant="outline" size="sm" className="flex-1" onClick={onConfigure}>
            <Settings className="mr-2 h-4 w-4" />
            Configure
          </Button>
          <Button variant="outline" size="sm" className="text-destructive" onClick={onDelete}>
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

export function PluginManager() {
  const [plugins, setPlugins] = useState<Plugin[]>(mockPlugins)
  const [events] = useState<PluginEvent[]>(mockEvents)
  const [showAddDialog, setShowAddDialog] = useState(false)
  const [selectedPlugin, setSelectedPlugin] = useState<Plugin | null>(null)
  const [showConfigDialog, setShowConfigDialog] = useState(false)
  const [newPlugin, setNewPlugin] = useState({
    name: '',
    code: ''
  })

  const handleToggle = (id: string) => {
    setPlugins(plugins.map(p =>
      p.id === id ? { ...p, enabled: !p.enabled, status: !p.enabled ? 'active' : 'inactive' } : p
    ))
  }

  const handleDelete = (id: string) => {
    setPlugins(plugins.filter(p => p.id !== id))
    toast.success('Plugin removed')
  }

  const handleUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) {
      toast.success(`Plugin ${file.name} uploaded`)
    }
  }

  const activePlugins = plugins.filter(p => p.enabled).length
  const totalHooks = plugins.reduce((acc, p) => acc + p.hooks.length, 0)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Plugins</h1>
          <p className="text-muted-foreground">
            Manage custom plugins and extensions
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => document.getElementById('plugin-upload')?.click()}>
            <Upload className="mr-2 h-4 w-4" />
            Upload
            <input
              id="plugin-upload"
              type="file"
              accept=".js,.wasm,.zip"
              className="hidden"
              onChange={handleUpload}
            />
          </Button>
          <Button onClick={() => setShowAddDialog(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Create
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Total Plugins</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{plugins.length}</div>
            <p className="text-xs text-muted-foreground">{activePlugins} active</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Active Hooks</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalHooks}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">API Version</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">v2.0</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Events (24h)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{events.length}</div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="installed" className="space-y-6">
        <TabsList>
          <TabsTrigger value="installed">Installed</TabsTrigger>
          <TabsTrigger value="marketplace">Marketplace</TabsTrigger>
          <TabsTrigger value="events">Events</TabsTrigger>
          <TabsTrigger value="docs">Documentation</TabsTrigger>
        </TabsList>

        <TabsContent value="installed" className="space-y-6">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {plugins.map(plugin => (
              <PluginCard
                key={plugin.id}
                plugin={plugin}
                onToggle={() => handleToggle(plugin.id)}
                onDelete={() => handleDelete(plugin.id)}
                onConfigure={() => {
                  setSelectedPlugin(plugin)
                  setShowConfigDialog(true)
                }}
              />
            ))}
          </div>
        </TabsContent>

        <TabsContent value="marketplace" className="space-y-6">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {[
              { name: 'OAuth2 Handler', description: 'OAuth2 authentication support', author: 'OpenLoadBalancer', installs: 1250 },
              { name: 'Rate Limiter', description: 'Advanced rate limiting strategies', author: 'Community', installs: 890 },
              { name: 'Request Logger', description: 'Detailed request logging', author: 'OpenLoadBalancer', installs: 2100 }
            ].map((item, i) => (
              <Card key={i}>
                <CardHeader>
                  <CardTitle className="text-base">{item.name}</CardTitle>
                  <CardDescription>{item.description}</CardDescription>
                </CardHeader>
                <CardContent>
                  <p className="text-sm text-muted-foreground">by {item.author}</p>
                  <p className="text-sm text-muted-foreground">{item.installs} installs</p>
                  <Button className="w-full mt-4">
                    <Download className="mr-2 h-4 w-4" />
                    Install
                  </Button>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="events" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Plugin Events</CardTitle>
              <CardDescription>Recent plugin activity and logs</CardDescription>
            </CardHeader>
            <CardContent>
              <ScrollArea className="h-[400px]">
                <div className="space-y-2">
                  {events.map((event, index) => (
                    <div
                      key={index}
                      className="flex items-start gap-3 rounded-lg border p-3"
                    >
                      {event.level === 'info' && <CheckCircle className="h-4 w-4 text-green-500" />}
                      {event.level === 'warn' && <AlertCircle className="h-4 w-4 text-amber-500" />}
                      {event.level === 'error' && <AlertCircle className="h-4 w-4 text-destructive" />}
                      <div className="flex-1">
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{event.plugin}</span>
                          <Badge variant="outline" className="text-xs">{event.event}</Badge>
                        </div>
                        <p className="text-sm text-muted-foreground">{event.message}</p>
                        <p className="text-xs text-muted-foreground">
                          {event.timestamp.toLocaleTimeString()}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="docs" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Plugin Development Guide</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="rounded-lg bg-muted p-4">
                <p className="font-medium mb-2">Plugin Structure</p>
                <code className="block text-sm">
                  {`// Plugin entry point
export default {
  name: 'my-plugin',
  version: '1.0.0',
  hooks: {
    request: async (ctx) => {
      // Process request
    },
    response: async (ctx) => {
      // Modify response
    }
  }
}`}
                </code>
              </div>
              <div className="space-y-2">
                <h4 className="font-medium">Available Hooks</h4>
                <ul className="list-disc list-inside text-sm text-muted-foreground">
                  <li><code>request</code> - Process incoming requests</li>
                  <li><code>response</code> - Modify outgoing responses</li>
                  <li><code>routing</code> - Custom routing decisions</li>
                  <li><code>metrics</code> - Custom metrics collection</li>
                </ul>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Create Plugin Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Create Plugin</DialogTitle>
            <DialogDescription>Write a new plugin using JavaScript</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>Name</Label>
              <Input
                value={newPlugin.name}
                onChange={(e) => setNewPlugin({ ...newPlugin, name: e.target.value })}
                placeholder="my-custom-plugin"
              />
            </div>
            <div className="space-y-2">
              <Label>Code</Label>
              <Textarea
                value={newPlugin.code}
                onChange={(e) => setNewPlugin({ ...newPlugin, code: e.target.value })}
                className="min-h-[300px] font-mono text-sm"
                placeholder={`export default {
  name: 'my-plugin',
  hooks: {
    request: async (ctx) => {
      // Your code here
    }
  }
}`}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddDialog(false)}>
              Cancel
            </Button>
            <Button onClick={() => {
              toast.success('Plugin created')
              setShowAddDialog(false)
            }}>
              Create Plugin
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Configure Plugin Dialog */}
      <Dialog open={showConfigDialog} onOpenChange={setShowConfigDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Configure {selectedPlugin?.name}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            {selectedPlugin && Object.entries(selectedPlugin.config).map(([key, value]) => (
              <div key={key} className="space-y-2">
                <Label className="capitalize">{key}</Label>
                <Input
                  value={String(value)}
                  onChange={() => {}}
                  type={key.includes('secret') || key.includes('password') ? 'password' : 'text'}
                />
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowConfigDialog(false)}>
              Cancel
            </Button>
            <Button onClick={() => {
              toast.success('Configuration saved')
              setShowConfigDialog(false)
            }}>
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
