import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { ScrollArea } from '@/components/ui/scroll-area'
import api from '@/lib/api'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import {
  Search,
  Plus,
  Trash2,
  RefreshCw,
  Server,
  Database,
  Globe,
  FileText,
  Box,
  CheckCircle,
  AlertCircle,
  Clock,
  Settings,
  MoreHorizontal,
  Play,
  Pause
} from 'lucide-react'

interface DiscoveryProvider {
  id: string
  name: string
  type: 'static' | 'dns' | 'docker' | 'consul' | 'kubernetes' | 'file'
  enabled: boolean
  config: Record<string, string>
  lastSync?: Date
  status: 'connected' | 'disconnected' | 'error' | 'syncing'
  backendCount: number
}

const providerTypes = [
  { value: 'static', label: 'Static List', icon: Server, description: 'Manually configured backends' },
  { value: 'dns', label: 'DNS SRV', icon: Globe, description: 'DNS SRV record discovery' },
  { value: 'docker', label: 'Docker', icon: Box, description: 'Docker container discovery' },
  { value: 'consul', label: 'Consul', icon: Database, description: 'HashiCorp Consul integration' },
  { value: 'kubernetes', label: 'Kubernetes', icon: Server, description: 'K8s service discovery' },
  { value: 'file', label: 'File', icon: FileText, description: 'JSON/YAML file watcher' }
]

const mockProviders: DiscoveryProvider[] = [
  {
    id: '1',
    name: 'Web Backends',
    type: 'static',
    enabled: true,
    config: {},
    lastSync: new Date(),
    status: 'connected',
    backendCount: 3
  },
  {
    id: '2',
    name: 'API Services',
    type: 'dns',
    enabled: true,
    config: { domain: '_api._tcp.example.com' },
    lastSync: new Date(Date.now() - 300000),
    status: 'connected',
    backendCount: 2
  },
  {
    id: '3',
    name: 'Docker Services',
    type: 'docker',
    enabled: false,
    config: { socket: '/var/run/docker.sock', label: 'lb.enabled=true' },
    status: 'disconnected',
    backendCount: 0
  }
]

function ProviderCard({
  provider,
  onToggle,
  onDelete,
  onSync
}: {
  provider: DiscoveryProvider
  onToggle: () => void
  onDelete: () => void
  onSync: () => void
}) {
  const typeConfig = providerTypes.find(t => t.value === provider.type)
  const Icon = typeConfig?.icon || Server

  const statusColors = {
    connected: 'bg-green-500',
    disconnected: 'bg-muted',
    error: 'bg-destructive',
    syncing: 'bg-amber-500'
  }

  return (
    <Card className={cn(!provider.enabled && 'opacity-60')}>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-muted">
              <Icon className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <CardTitle className="text-base">{provider.name}</CardTitle>
                <div className={cn('h-2 w-2 rounded-full', statusColors[provider.status])} />
              </div>
              <CardDescription>{typeConfig?.label}</CardDescription>
            </div>
          </div>
          <Switch checked={provider.enabled} onCheckedChange={onToggle} />
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">Backends</span>
          <Badge variant="outline">{provider.backendCount}</Badge>
        </div>
        {provider.lastSync && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Clock className="h-3 w-3" />
            <span>Last sync: {provider.lastSync.toLocaleTimeString()}</span>
          </div>
        )}
        <div className="flex gap-2">
          <Button variant="outline" size="sm" className="flex-1" onClick={onSync}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Sync
          </Button>
          <Button variant="outline" size="sm" className="text-destructive" onClick={onDelete}>
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

export function ServiceDiscoveryPanel() {
  const [providers, setProviders] = useState<DiscoveryProvider[]>(mockProviders)
  const [showAddDialog, setShowAddDialog] = useState(false)
  const [newProvider, setNewProvider] = useState({
    name: '',
    type: 'static',
    config: {}
  })

  const handleAdd = () => {
    const provider: DiscoveryProvider = {
      id: Math.random().toString(36).substr(2, 9),
      name: newProvider.name,
      type: newProvider.type as DiscoveryProvider['type'],
      enabled: true,
      config: newProvider.config,
      status: 'connected',
      backendCount: 0
    }
    setProviders([...providers, provider])
    setShowAddDialog(false)
    setNewProvider({ name: '', type: 'static', config: {} })
    toast.success('Provider added')
  }

  const handleToggle = (id: string) => {
    setProviders(providers.map(p =>
      p.id === id ? { ...p, enabled: !p.enabled } : p
    ))
  }

  const handleDelete = (id: string) => {
    setProviders(providers.filter(p => p.id !== id))
    toast.success('Provider removed')
  }

  const handleSync = (id: string) => {
    toast.success('Sync initiated')
  }

  const enabledCount = providers.filter(p => p.enabled).length
  const totalBackends = providers.reduce((acc, p) => acc + p.backendCount, 0)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Service Discovery</h1>
          <p className="text-muted-foreground">
            Configure automatic backend discovery from various sources
          </p>
        </div>
        <Button onClick={() => setShowAddDialog(true)}>
          <Plus className="mr-2 h-4 w-4" />
          Add Provider
        </Button>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Providers</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{providers.length}</div>
            <p className="text-xs text-muted-foreground">{enabledCount} enabled</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Discovered Backends</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalBackends}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Active Sources</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">
              {providers.filter(p => p.status === 'connected').length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Types</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-1">
              {Array.from(new Set(providers.map(p => p.type))).map(type => (
                <Badge key={type} variant="outline" className="text-xs">{type}</Badge>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Providers Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {providers.map(provider => (
          <ProviderCard
            key={provider.id}
            provider={provider}
            onToggle={() => handleToggle(provider.id)}
            onDelete={() => handleDelete(provider.id)}
            onSync={() => handleSync(provider.id)}
          />
        ))}
      </div>

      {/* Add Provider Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Add Discovery Provider</DialogTitle>
            <DialogDescription>Configure a new service discovery source</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>Name</Label>
              <Input
                value={newProvider.name}
                onChange={(e) => setNewProvider({ ...newProvider, name: e.target.value })}
                placeholder="e.g., Production API"
              />
            </div>
            <div className="space-y-2">
              <Label>Type</Label>
              <Select
                value={newProvider.type}
                onValueChange={(v) => setNewProvider({ ...newProvider, type: v })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {providerTypes.map(type => (
                    <SelectItem key={type.value} value={type.value}>
                      <div className="flex items-center gap-2">
                        <type.icon className="h-4 w-4" />
                        {type.label}
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Type-specific config */}
            {newProvider.type === 'dns' && (
              <div className="space-y-2">
                <Label>SRV Domain</Label>
                <Input placeholder="_api._tcp.example.com" />
              </div>
            )}
            {newProvider.type === 'consul' && (
              <div className="space-y-2">
                <Label>Consul Address</Label>
                <Input placeholder="localhost:8500" />
              </div>
            )}
            {newProvider.type === 'docker' && (
              <div className="space-y-2">
                <Label>Label Selector</Label>
                <Input placeholder="lb.enabled=true" />
              </div>
            )}
            {newProvider.type === 'file' && (
              <div className="space-y-2">
                <Label>File Path</Label>
                <Input placeholder="/etc/openlb/backends.json" />
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleAdd} disabled={!newProvider.name}>
              Add Provider
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
