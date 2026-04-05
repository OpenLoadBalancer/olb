import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { toast } from 'sonner'
import { cn, formatBytes } from '@/lib/utils'
import {
  Database,
  Trash2,
  RefreshCw,
  Zap,
  Clock,
  Activity,
  HardDrive,
  MemoryStick,
  Search,
  Filter,
  CheckCircle2,
  XCircle,
  Settings,
  Play,
  Pause,
  BarChart3,
  Key,
  Layers
} from 'lucide-react'

interface CacheStats {
  name: string
  type: 'memory' | 'disk' | 'distributed'
  size: number
  maxSize: number
  entries: number
  hitRate: number
  hits: number
  misses: number
  evictions: number
  avgLoadTime: number
  enabled: boolean
}

interface CacheKey {
  key: string
  value: string
  size: number
  ttl?: number
  lastAccessed: Date
  hitCount: number
}

const mockCacheStats: CacheStats[] = [
  {
    name: 'Response Cache',
    type: 'memory',
    size: 1024 * 1024 * 256,
    maxSize: 1024 * 1024 * 512,
    entries: 15420,
    hitRate: 87.5,
    hits: 125000,
    misses: 17850,
    evictions: 2450,
    avgLoadTime: 2.3,
    enabled: true
  },
  {
    name: 'Session Store',
    type: 'memory',
    size: 1024 * 1024 * 64,
    maxSize: 1024 * 1024 * 128,
    entries: 3420,
    hitRate: 99.2,
    hits: 89000,
    misses: 720,
    evictions: 120,
    avgLoadTime: 0.1,
    enabled: true
  },
  {
    name: 'SSL Certificate Cache',
    type: 'memory',
    size: 1024 * 1024 * 16,
    maxSize: 1024 * 1024 * 32,
    entries: 45,
    hitRate: 98.5,
    hits: 45000,
    misses: 685,
    evictions: 0,
    avgLoadTime: 5.2,
    enabled: true
  },
  {
    name: 'Disk Cache',
    type: 'disk',
    size: 1024 * 1024 * 1024,
    maxSize: 1024 * 1024 * 2048,
    entries: 5234,
    hitRate: 72.3,
    hits: 32000,
    misses: 12260,
    evictions: 4500,
    avgLoadTime: 15.8,
    enabled: true
  }
]

const mockCacheKeys: CacheKey[] = [
  { key: 'response:/api/users', value: '{...}', size: 1024, ttl: 300, lastAccessed: new Date(Date.now() - 60000), hitCount: 45 },
  { key: 'session:user:12345', value: '{...}', size: 512, ttl: 3600, lastAccessed: new Date(Date.now() - 120000), hitCount: 12 },
  { key: 'cert:api.openloadbalancer.dev', value: '...', size: 2048, lastAccessed: new Date(Date.now() - 300000), hitCount: 890 },
  { key: 'response:/health', value: '{...}', size: 256, ttl: 5, lastAccessed: new Date(Date.now() - 10000), hitCount: 1500 },
  { key: 'config:rate-limits', value: '{...}', size: 1024, lastAccessed: new Date(Date.now() - 600000), hitCount: 2340 }
]

export function CacheManager() {
  const [caches, setCaches] = useState<CacheStats[]>(mockCacheStats)
  const [cacheKeys, setCacheKeys] = useState<CacheKey[]>(mockCacheKeys)
  const [selectedCache, setSelectedCache] = useState<string>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [activeTab, setActiveTab] = useState('overview')
  const [showClearDialog, setShowClearDialog] = useState(false)
  const [cacheToClear, setCacheToClear] = useState<string | null>(null)

  const clearCache = () => {
    if (cacheToClear === 'all') {
      toast.success('All caches cleared')
    } else {
      toast.success(`Cache "${cacheToClear}" cleared`)
    }
    setShowClearDialog(false)
  }

  const toggleCache = (name: string) => {
    setCaches(prev =>
      prev.map(c =>
        c.name === name ? { ...c, enabled: !c.enabled } : c
      )
    )
    toast.success(`Cache "${name}" ${caches.find(c => c.name === name)?.enabled ? 'disabled' : 'enabled'}`)
  }

  const deleteKey = (key: string) => {
    setCacheKeys(prev => prev.filter(k => k.key !== key))
    toast.success(`Key deleted: ${key}`)
  }

  const filteredKeys = cacheKeys.filter(key => {
    const matchesCache = selectedCache === 'all' || key.key.startsWith(selectedCache.toLowerCase())
    const matchesSearch = key.key.toLowerCase().includes(searchQuery.toLowerCase())
    return matchesCache && matchesSearch
  })

  const totalSize = caches.reduce((sum, c) => sum + c.size, 0)
  const totalMax = caches.reduce((sum, c) => sum + c.maxSize, 0)
  const avgHitRate = caches.reduce((sum, c) => sum + c.hitRate, 0) / caches.length

  const getTypeIcon = (type: CacheStats['type']) => {
    switch (type) {
      case 'memory':
        return <MemoryStick className="h-4 w-4" />
      case 'disk':
        return <HardDrive className="h-4 w-4" />
      case 'distributed':
        return <Layers className="h-4 w-4" />
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Cache Management</h1>
          <p className="text-muted-foreground">
            Manage response and session caches
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="destructive"
            onClick={() => { setCacheToClear('all'); setShowClearDialog(true) }}
          >
            <Trash2 className="mr-2 h-4 w-4" />
            Clear All
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Size</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatBytes(totalSize)}</div>
            <p className="text-xs text-muted-foreground">of {formatBytes(totalMax)}</p>
            <Progress value={(totalSize / totalMax) * 100} className="mt-2" />
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Hit Rate</CardTitle>
            <Activity className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{avgHitRate.toFixed(1)}%</div>
            <p className="text-xs text-muted-foreground">Average across caches</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Entries</CardTitle>
            <Key className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {caches.reduce((sum, c) => sum + c.entries, 0).toLocaleString()}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Caches</CardTitle>
            <Zap className="h-4 w-4 text-yellow-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {caches.filter(c => c.enabled).length}/{caches.length}
            </div>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="keys">Keys</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <div className="grid gap-4">
            {caches.map(cache => (
              <Card key={cache.name}>
                <CardContent className="p-4">
                  <div className="flex items-start justify-between">
                    <div className="flex items-start gap-4">
                      <div className="mt-1">{getTypeIcon(cache.type)}</div>
                      <div>
                        <div className="flex items-center gap-2">
                          <h3 className="font-medium">{cache.name}</h3>
                          <Badge variant="outline" className="capitalize">
                            {cache.type}
                          </Badge>
                          {cache.enabled ? (
                            <CheckCircle2 className="h-4 w-4 text-green-500" />
                          ) : (
                            <XCircle className="h-4 w-4 text-red-500" />
                          )}
                        </div>
                        <p className="text-sm text-muted-foreground mt-1">
                          {cache.entries.toLocaleString()} entries • {formatBytes(cache.size)} / {formatBytes(cache.maxSize)}
                        </p>
                        <div className="mt-2 flex items-center gap-4 text-sm">
                          <span className="flex items-center gap-1">
                            <Activity className="h-3.5 w-3.5 text-green-500" />
                            {cache.hitRate.toFixed(1)}% hit rate
                          </span>
                          <span className="text-muted-foreground">
                            {cache.hits.toLocaleString()} hits
                          </span>
                          <span className="text-muted-foreground">
                            {cache.misses.toLocaleString()} misses
                          </span>
                          <span className="text-muted-foreground">
                            {cache.evictions.toLocaleString()} evictions
                          </span>
                        </div>
                        <div className="mt-2">
                          <Progress value={(cache.size / cache.maxSize) * 100} />
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Switch
                        checked={cache.enabled}
                        onCheckedChange={() => toggleCache(cache.name)}
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => { setCacheToClear(cache.name); setShowClearDialog(true) }}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="keys" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Cache Keys</CardTitle>
              <CardDescription>Manage individual cache entries</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex items-center gap-2 mb-4">
                <Select value={selectedCache} onValueChange={setSelectedCache}>
                  <SelectTrigger className="w-[180px]">
                    <Filter className="mr-2 h-4 w-4" />
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">All Caches</SelectItem>
                    {caches.map(c => (
                      <SelectItem key={c.name} value={c.name}>{c.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className="relative flex-1">
                  <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                  <Input
                    placeholder="Search keys..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="pl-9"
                  />
                </div>
              </div>

              <ScrollArea className="h-[400px]">
                <div className="space-y-2">
                  {filteredKeys.map(key => (
                    <div
                      key={key.key}
                      className="flex items-center justify-between p-3 rounded-lg border hover:bg-muted/50"
                    >
                      <div className="flex-1 min-w-0">
                        <code className="text-sm font-mono">{key.key}</code>
                        <div className="flex items-center gap-4 mt-1 text-xs text-muted-foreground">
                          <span>{formatBytes(key.size)}</span>
                          {key.ttl && (
                            <span className="flex items-center gap-1">
                              <Clock className="h-3 w-3" />
                              TTL: {key.ttl}s
                            </span>
                          )}
                          <span>Accessed: {key.lastAccessed.toLocaleTimeString()}</span>
                          <span>Hits: {key.hitCount}</span>
                        </div>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => deleteKey(key.key)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Cache Settings</CardTitle>
              <CardDescription>Configure global cache behavior</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Auto-Eviction</Label>
                  <p className="text-sm text-muted-foreground">Automatically remove expired entries</p>
                </div>
                <Switch defaultChecked />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Compression</Label>
                  <p className="text-sm text-muted-foreground">Compress large cache values</p>
                </div>
                <Switch defaultChecked />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Statistics Collection</Label>
                  <p className="text-sm text-muted-foreground">Track hit/miss rates</p>
                </div>
                <Switch defaultChecked />
              </div>
              <div className="space-y-2">
                <Label>Default TTL (seconds)</Label>
                <Input type="number" defaultValue={300} />
              </div>
              <div className="space-y-2">
                <Label>Max Entry Size</Label>
                <Input defaultValue="1MB" />
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Clear Confirmation Dialog */}
      <Dialog open={showClearDialog} onOpenChange={setShowClearDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Clear Cache</DialogTitle>
            <DialogDescription>
              Are you sure you want to clear {cacheToClear === 'all' ? 'all caches' : `"${cacheToClear}"`}?
              This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowClearDialog(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={clearCache}>
              <Trash2 className="mr-2 h-4 w-4" />
              Clear
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
