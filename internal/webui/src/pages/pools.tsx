import { useState } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Layers, Plus, Search, Trash2, Activity, Clock, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { usePools } from "@/hooks/use-query"
import { api } from "@/lib/api"
import { LoadingCard } from "@/components/ui/loading"

const algorithmLabels: Record<string, string> = {
  round_robin: "Round Robin",
  rr: "Round Robin",
  least_connections: "Least Connections",
  lc: "Least Connections",
  ip_hash: "IP Hash",
  iphash: "IP Hash",
  weighted_round_robin: "Weighted Round Robin",
  wrr: "Weighted Round Robin",
  weighted_least_connections: "Weighted Least Connections",
  wlc: "Weighted Least Connections",
  least_response_time: "Least Response Time",
  lrt: "Least Response Time",
  consistent_hash: "Consistent Hash",
  ch: "Consistent Hash",
  ketama: "Consistent Hash",
  maglev: "Maglev",
  power_of_two: "Power of Two",
  p2c: "Power of Two",
  random: "Random",
  weighted_random: "Weighted Random",
  wrandom: "Weighted Random",
  ring_hash: "Ring Hash",
  ringhash: "Ring Hash",
  rendezvous: "Rendezvous Hash",
  rendezvous_hash: "Rendezvous Hash",
  sticky: "Sticky Sessions",
  peak_ewma: "Peak EWMA",
  pewma: "Peak EWMA",
}

export function PoolsPage() {
  useDocumentTitle("Pools")
  const { data: pools, isLoading, error, refetch } = usePools()
  const [search, setSearch] = useState("")
  const [selectedPoolName, setSelectedPoolName] = useState<string | null>(null)

  // Create Pool Dialog State (local UI only — pools created via config)
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [newPool, setNewPool] = useState({
    name: "",
    algorithm: "round_robin",
    healthCheckEnabled: true,
    healthCheckType: "http",
    healthCheckPath: "/health",
    healthCheckInterval: "10s",
  })

  // Add Backend Dialog State
  const [backendDialogOpen, setBackendDialogOpen] = useState(false)
  const [newBackend, setNewBackend] = useState({
    address: "",
    weight: 1,
  })

  const selectedPool = pools?.find(p => p.name === selectedPoolName) ?? null

  // Auto-select first pool when data loads
  const firstPool = pools?.[0]
  if (pools && pools.length > 0 && !selectedPoolName && !selectedPool && firstPool) {
    setSelectedPoolName(firstPool.name)
  }

  const filteredPools = (pools ?? []).filter(p =>
    p.name.toLowerCase().includes(search.toLowerCase())
  )

  const getStatusColor = (state: string, healthy: boolean) => {
    if (!healthy) return 'bg-red-500'
    switch (state) {
      case 'up': return 'bg-green-500'
      case 'down': return 'bg-red-500'
      case 'draining': return 'bg-amber-500'
      default: return 'bg-green-500'
    }
  }

  const getHealthBadge = (healthy: boolean) => {
    return healthy
      ? <Badge variant="outline" className="text-green-600 border-green-600">Healthy</Badge>
      : <Badge variant="destructive">Unhealthy</Badge>
  }

  const handleAddBackend = async () => {
    if (!selectedPool) return
    try {
      await api.addBackend(selectedPool.name, {
        id: `${newBackend.address}`,
        address: newBackend.address,
        weight: newBackend.weight,
      })
      setBackendDialogOpen(false)
      setNewBackend({ address: "", weight: 1 })
      toast.success(`Backend "${newBackend.address}" added successfully`)
      refetch()
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to add backend"
      toast.error(message)
    }
  }

  const handleDeleteBackend = async (backendId: string) => {
    if (!selectedPool) return
    try {
      await api.removeBackend(selectedPool.name, backendId)
      toast.success("Backend removed successfully")
      refetch()
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to remove backend"
      toast.error(message)
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Pools</h1>
            <p className="text-muted-foreground">Manage backend pools and load balancing</p>
          </div>
        </div>
        <div className="grid gap-4 md:grid-cols-3">
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Pools</h1>
          <p className="text-muted-foreground">Manage backend pools and load balancing</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-destructive">Failed to load pools: {error.message}</p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4" />
              Retry
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Pools</h1>
          <p className="text-muted-foreground">Manage backend pools and load balancing</p>
        </div>
        <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Create Pool
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[500px]">
            <DialogHeader>
              <DialogTitle>Create New Pool</DialogTitle>
              <DialogDescription>
                Configure a new backend pool with load balancing settings.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="grid gap-2">
                <Label htmlFor="name">Pool Name</Label>
                <Input 
                  id="name"
                  placeholder="e.g., api-pool"
                  value={newPool.name}
                  onChange={(e) => setNewPool({ ...newPool, name: e.target.value })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="algorithm">Algorithm</Label>
                <Select
                  value={newPool.algorithm}
                  onValueChange={(value: string) => setNewPool({ ...newPool, algorithm: value })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select algorithm" />
                  </SelectTrigger>
                  <SelectContent>
                    {Object.entries(algorithmLabels).filter((_, i) => i % 2 === 0).map(([value, label]) => (
                      <SelectItem key={value} value={value}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex items-center justify-between">
                <Label htmlFor="health-check">Enable Health Checks</Label>
                <Switch
                  id="health-check"
                  checked={newPool.healthCheckEnabled}
                  onCheckedChange={(checked) => setNewPool({ ...newPool, healthCheckEnabled: checked })}
                />
              </div>
              {newPool.healthCheckEnabled && (
                <>
                  <div className="grid gap-2">
                    <Label htmlFor="hc-type">Health Check Type</Label>
                    <Select
                      value={newPool.healthCheckType}
                      onValueChange={(value: string) => setNewPool({ ...newPool, healthCheckType: value })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="http">HTTP</SelectItem>
                        <SelectItem value="tcp">TCP</SelectItem>
                        <SelectItem value="grpc">gRPC</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="hc-path">Health Check Path</Label>
                    <Input 
                      id="hc-path"
                      placeholder="/health"
                      value={newPool.healthCheckPath}
                      onChange={(e) => setNewPool({ ...newPool, healthCheckPath: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="hc-interval">Interval</Label>
                    <Select
                      value={newPool.healthCheckInterval}
                      onValueChange={(value: string) => setNewPool({ ...newPool, healthCheckInterval: value })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="5s">5 seconds</SelectItem>
                        <SelectItem value="10s">10 seconds</SelectItem>
                        <SelectItem value="30s">30 seconds</SelectItem>
                        <SelectItem value="1m">1 minute</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
                Cancel
              </Button>
              <Button onClick={() => { toast.info("Pool creation requires config file update and reload"); setCreateDialogOpen(false) }} disabled={!newPool.name}>
                Create Pool
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="flex gap-4">
        <div className="relative flex-1 sm:max-w-sm">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input 
            placeholder="Search pools..." aria-label="Search pools"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-10"
          />
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-3 md:grid-cols-2">
        <div className="space-y-4">
          {filteredPools.map((pool) => (
            <Card
              key={pool.name}
              role="button"
              tabIndex={0}
              aria-label={`Select pool ${pool.name}`}
              aria-pressed={selectedPool?.name === pool.name}
              className={`cursor-pointer transition-colors hover:bg-accent ${selectedPool?.name === pool.name ? 'border-primary' : ''}`}
              onClick={() => setSelectedPoolName(pool.name)}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setSelectedPoolName(pool.name) } }}
            >
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div className="p-2 rounded-lg bg-primary/10">
                      <Layers className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                      <CardTitle className="text-base">{pool.name}</CardTitle>
                      <CardDescription>{algorithmLabels[pool.algorithm] || pool.algorithm}</CardDescription>
                    </div>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-2 gap-2 text-sm sm:gap-4">
                  <div>
                    <span className="text-muted-foreground">Backends:</span>
                    <span className="ml-2 font-medium">{pool.backends.length}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Healthy:</span>
                    <span className="ml-2 font-medium text-green-600">
                      {pool.backends.filter(b => b.healthy).length}
                    </span>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        <div className="lg:col-span-2">
          {selectedPool ? (
            <Tabs defaultValue="backends" className="space-y-4">
              <TabsList>
                <TabsTrigger value="backends">Backends</TabsTrigger>
                <TabsTrigger value="settings">Settings</TabsTrigger>
                <TabsTrigger value="stats">Statistics</TabsTrigger>
              </TabsList>

              <TabsContent value="backends" className="space-y-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-lg font-medium">Backends</h3>
                  <Dialog open={backendDialogOpen} onOpenChange={setBackendDialogOpen}>
                    <DialogTrigger asChild>
                      <Button size="sm">
                        <Plus className="mr-2 h-4 w-4" />
                        Add Backend
                      </Button>
                    </DialogTrigger>
                    <DialogContent className="sm:max-w-[400px]">
                      <DialogHeader>
                        <DialogTitle>Add Backend</DialogTitle>
                        <DialogDescription>
                          Add a new backend server to this pool.
                        </DialogDescription>
                      </DialogHeader>
                      <div className="grid gap-4 py-4">
                        <div className="grid gap-2">
                          <Label htmlFor="address">Backend Address</Label>
                          <Input 
                            id="address"
                            placeholder="e.g., 10.0.1.10:8080"
                            value={newBackend.address}
                            onChange={(e) => setNewBackend({ ...newBackend, address: e.target.value })}
                          />
                        </div>
                        <div className="grid gap-2">
                          <Label htmlFor="weight">Weight</Label>
                          <Input 
                            id="weight"
                            type="number"
                            min={1}
                            value={newBackend.weight}
                            onChange={(e) => setNewBackend({ ...newBackend, weight: parseInt(e.target.value) || 1 })}
                          />
                        </div>
                      </div>
                      <DialogFooter>
                        <Button variant="outline" onClick={() => setBackendDialogOpen(false)}>
                          Cancel
                        </Button>
                        <Button onClick={handleAddBackend} disabled={!newBackend.address}>
                          Add Backend
                        </Button>
                      </DialogFooter>
                    </DialogContent>
                  </Dialog>
                </div>

                <div className="grid gap-4">
                  {selectedPool.backends.map((backend) => (
                    <Card key={backend.id}>
                      <CardContent className="p-4">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div className="flex items-center gap-3 min-w-0">
                            <div className={`h-3 w-3 shrink-0 rounded-full ${getStatusColor(backend.state, backend.healthy)}`} />
                            <div className="min-w-0">
                              <div className="font-medium truncate">{backend.address}</div>
                              <div className="text-sm text-muted-foreground">
                                Weight: {backend.weight}
                              </div>
                            </div>
                          </div>
                          <div className="flex flex-wrap items-center gap-3">
                            {getHealthBadge(backend.healthy)}
                            <div className="text-right text-sm">
                              <div className="flex items-center gap-1 text-muted-foreground">
                                <Activity className="h-3 w-3" />
                                {backend.requests.toLocaleString()} req
                              </div>
                              <div className="flex items-center gap-1 text-muted-foreground">
                                <Clock className="h-3 w-3" />
                                {backend.errors} err
                              </div>
                            </div>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-9 w-9 shrink-0 text-destructive" aria-label="Delete backend"
                              onClick={() => handleDeleteBackend(backend.id)}
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

              <TabsContent value="settings">
                <Card>
                  <CardHeader>
                    <CardTitle>Pool Settings</CardTitle>
                    <CardDescription>Load balancing algorithm and health check configuration</CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div className="grid gap-4 md:grid-cols-2">
                      <div>
                        <label className="text-sm font-medium">Algorithm</label>
                        <div className="mt-1 text-sm text-muted-foreground">
                          {algorithmLabels[selectedPool.algorithm] || selectedPool.algorithm}
                        </div>
                      </div>
                      <div>
                        <label className="text-sm font-medium">Backends</label>
                        <div className="mt-1 text-sm text-muted-foreground">
                          {selectedPool.backends.length} total, {selectedPool.backends.filter(b => b.healthy).length} healthy
                        </div>
                      </div>
                      {selectedPool.health_check && (
                        <>
                          <div>
                            <label className="text-sm font-medium">Check Type</label>
                            <div className="mt-1 text-sm text-muted-foreground uppercase">
                              {selectedPool.health_check.type}
                            </div>
                          </div>
                          <div>
                            <label className="text-sm font-medium">Interval</label>
                            <div className="mt-1 text-sm text-muted-foreground">
                              {selectedPool.health_check.interval}
                            </div>
                          </div>
                          {selectedPool.health_check.path && (
                            <div>
                              <label className="text-sm font-medium">Path</label>
                              <div className="mt-1 text-sm text-muted-foreground">
                                {selectedPool.health_check.path}
                              </div>
                            </div>
                          )}
                        </>
                      )}
                    </div>
                  </CardContent>
                </Card>
              </TabsContent>

              <TabsContent value="stats">
                <div className="grid gap-4 md:grid-cols-2">
                  <Card>
                    <CardHeader className="pb-2">
                      <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <div className="text-2xl font-bold">
                        {selectedPool.backends.reduce((sum, b) => sum + b.requests, 0).toLocaleString()}
                      </div>
                    </CardContent>
                  </Card>
                  <Card>
                    <CardHeader className="pb-2">
                      <CardTitle className="text-sm font-medium">Total Errors</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <div className="text-2xl font-bold">
                        {selectedPool.backends.reduce((sum, b) => sum + b.errors, 0).toLocaleString()}
                      </div>
                    </CardContent>
                  </Card>
                </div>
              </TabsContent>
            </Tabs>
          ) : (
            <div className="flex h-64 items-center justify-center text-muted-foreground">
              Select a pool to view details
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
