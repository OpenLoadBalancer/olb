import { useState } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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
import { Plus, Globe, Shield, Trash2, Edit, Activity, Route } from "lucide-react"
import { toast } from "sonner"
import { useConfig, usePools } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"
import type { Listener } from "@/types"

const protocolIcons: Record<string, React.ReactNode> = {
  http: <Globe className="h-4 w-4" />,
  https: <Shield className="h-4 w-4" />,
  tcp: <Activity className="h-4 w-4" />,
  udp: <Activity className="h-4 w-4" />,
}

const protocolColors: Record<string, string> = {
  http: "bg-blue-500/10 text-blue-600",
  https: "bg-green-500/10 text-green-600",
  tcp: "bg-purple-500/10 text-purple-600",
  udp: "bg-orange-500/10 text-orange-600",
}

const httpMethods = ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"]

export function ListenersPage() {
  useDocumentTitle("Listeners")
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const { data: configRaw, isLoading: configLoading, error: configError } = useConfig() as { data: Record<string, any> | undefined; isLoading: boolean; error: Error | null }
  // Handle both API response shape and test mock shape
  const config = (configRaw?.data ?? configRaw) as Record<string, any> | undefined
  const { data: pools } = usePools()
  const poolNames = (pools ?? []).map(p => p.name)

  // Derive listeners from config
  const listeners: Listener[] = (config?.listeners ?? []).map((l: { name: string; address: string; protocol?: string; tls?: { enabled?: boolean }; routes?: Array<{ path: string }> }, i: number) => ({
    id: String(i),
    name: l.name,
    address: l.address,
    protocol: (l.protocol || (l.tls?.enabled ? 'https' : 'http')) as Listener['protocol'],
    routes: (l.routes ?? []).map((r: any, j: number) => ({
      id: `${i}-${j}`,
      path: r.path,
      pool: r.pool,
      methods: r.methods ?? [],
      strip_prefix: false,
      priority: j,
    })),
    enabled: true,
  }))

  const [selectedListener, setSelectedListener] = useState<Listener | null>(null)

  // Auto-select first listener when data loads
  if (listeners.length > 0 && !selectedListener) {
    setSelectedListener(listeners[0] ?? null)
  }

  // Create Listener Dialog State
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [newListener, setNewListener] = useState({
    name: "",
    address: "",
    protocol: "http",
  })

  // Add Route Dialog State
  const [routeDialogOpen, setRouteDialogOpen] = useState(false)
  const [newRoute, setNewRoute] = useState({
    path: "",
    pool: "",
    methods: ["GET"] as string[],
    strip_prefix: false,
    priority: 10,
  })

  const toggleListener = (_id: string) => {
    toast.info("Listener state changes require config file update and reload")
  }

  const handleCreateListener = () => {
    toast.info("Listener creation requires config file update and reload")
    setCreateDialogOpen(false)
    setNewListener({ name: "", address: "", protocol: "http" })
  }

  const handleAddRoute = () => {
    toast.info("Route creation requires config file update and reload")
    setRouteDialogOpen(false)
    setNewRoute({ path: "", pool: "", methods: ["GET"], strip_prefix: false, priority: 10 })
  }

  const handleDeleteListener = (_id: string) => {
    toast.info("Listener removal requires config file update and reload")
  }

  const handleDeleteRoute = (_routeId: string) => {
    toast.info("Route removal requires config file update and reload")
  }

  const toggleMethod = (method: string) => {
    setNewRoute(prev => ({
      ...prev,
      methods: prev.methods.includes(method)
        ? prev.methods.filter(m => m !== method)
        : [...prev.methods, method]
    }))
  }

  if (configLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Listeners</h1>
          <p className="text-muted-foreground">Configure entry points and routing rules</p>
        </div>
        <LoadingCard />
      </div>
    )
  }

  if (configError) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Listeners</h1>
          <p className="text-muted-foreground">Configure entry points and routing rules</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-destructive">Failed to load configuration: {configError.message}</p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Listeners</h1>
          <p className="text-muted-foreground">Configure entry points and routing rules</p>
        </div>
        <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Create Listener
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[500px]">
            <DialogHeader>
              <DialogTitle>Create New Listener</DialogTitle>
              <DialogDescription>
                Configure a new entry point for incoming traffic.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="grid gap-2">
                <Label htmlFor="listener-name">Listener Name</Label>
                <Input
                  id="listener-name"
                  placeholder="e.g., http-public"
                  value={newListener.name}
                  onChange={(e) => setNewListener({ ...newListener, name: e.target.value })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="listener-address">Address</Label>
                <Input
                  id="listener-address"
                  placeholder="e.g., :80 or 0.0.0.0:8080"
                  value={newListener.address}
                  onChange={(e) => setNewListener({ ...newListener, address: e.target.value })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="listener-protocol">Protocol</Label>
                <Select
                  value={newListener.protocol}
                  onValueChange={(value: string) => setNewListener({ ...newListener, protocol: value })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select protocol" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="http">HTTP</SelectItem>
                    <SelectItem value="https">HTTPS</SelectItem>
                    <SelectItem value="tcp">TCP</SelectItem>
                    <SelectItem value="udp">UDP</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleCreateListener} disabled={!newListener.name || !newListener.address}>
                Create Listener
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-6 lg:grid-cols-3 md:grid-cols-2">
        <div className="space-y-4">
          {listeners.map((listener) => (
            <Card
              key={listener.id}
              role="button"
              tabIndex={0}
              aria-label={`Select listener ${listener.name}`}
              aria-pressed={selectedListener?.id === listener.id}
              className={`cursor-pointer transition-colors hover:bg-accent ${selectedListener?.id === listener.id ? 'border-primary' : ''}`}
              onClick={() => setSelectedListener(listener)}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setSelectedListener(listener) } }}
            >
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div className={`p-2 rounded-lg ${protocolColors[listener.protocol]}`}>
                      {protocolIcons[listener.protocol]}
                    </div>
                    <div>
                      <CardTitle className="text-base">{listener.name}</CardTitle>
                      <CardDescription>{listener.address}</CardDescription>
                    </div>
                  </div>
                  <Switch
                    checked={listener.enabled}
                    onCheckedChange={() => toggleListener(listener.id)}
                    onClick={(e) => e.stopPropagation()}
                  />
                </div>
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between text-sm">
                  <Badge variant="outline" className={protocolColors[listener.protocol]}>
                    {listener.protocol.toUpperCase()}
                  </Badge>
                  <span className="text-muted-foreground">
                    {listener.routes.length} route{listener.routes.length !== 1 ? 's' : ''}
                  </span>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        <div className="lg:col-span-2">
          {selectedListener ? (
            <div className="space-y-4">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <h3 className="text-lg font-medium">{selectedListener.name}</h3>
                  <p className="text-sm text-muted-foreground">
                    {selectedListener.address} · {selectedListener.protocol.toUpperCase()}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => toast.info("Edit listener")}>
                    <Edit className="mr-2 h-4 w-4" />
                    Edit
                  </Button>
						<Button
                    variant="destructive"
                    size="sm"
                    onClick={() => handleDeleteListener(selectedListener.id)}
                  >
                    <Trash2 className="mr-2 h-4 w-4" />
                    Delete
                  </Button>
                </div>
              </div>

              <Card>
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle className="text-base flex items-center gap-2">
                      <Route className="h-4 w-4" />
                      Routes
                    </CardTitle>
                    <Dialog open={routeDialogOpen} onOpenChange={setRouteDialogOpen}>
                      <DialogTrigger asChild>
                        <Button size="sm">
                          <Plus className="mr-2 h-4 w-4" />
                          Add Route
                        </Button>
                      </DialogTrigger>
                      <DialogContent className="sm:max-w-[500px]">
                        <DialogHeader>
                          <DialogTitle>Add Route</DialogTitle>
                          <DialogDescription>
                            Configure a new routing rule.
                          </DialogDescription>
                        </DialogHeader>
                        <div className="grid gap-4 py-4">
                          <div className="grid gap-2">
                            <Label htmlFor="route-path">Path Pattern</Label>
                            <Input
                              id="route-path"
                              placeholder="e.g., /api/*"
                              value={newRoute.path}
                              onChange={(e) => setNewRoute({ ...newRoute, path: e.target.value })}
                            />
                          </div>
                          <div className="grid gap-2">
                            <Label htmlFor="route-pool">Target Pool</Label>
                            <Select
                              value={newRoute.pool}
                              onValueChange={(value: string) => setNewRoute({ ...newRoute, pool: value })}
                            >
                              <SelectTrigger>
                                <SelectValue placeholder="Select pool" />
                              </SelectTrigger>
                              <SelectContent>
                                {poolNames.map((pool) => (
                                  <SelectItem key={pool} value={pool}>
                                    {pool}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </div>
                          <div className="grid gap-2">
                            <Label>HTTP Methods</Label>
                            <div className="flex flex-wrap gap-2">
                              {httpMethods.map((method) => (
                                <Badge
                                  key={method}
                                  variant={newRoute.methods.includes(method) ? "default" : "outline"}
                                  className="cursor-pointer"
                                  onClick={() => toggleMethod(method)}
                                >
                                  {method}
                                </Badge>
                              ))}
                            </div>
                          </div>
                          <div className="grid gap-2">
                            <Label htmlFor="route-priority">Priority</Label>
                            <Input
                              id="route-priority"
                              type="number"
                              value={newRoute.priority}
                              onChange={(e) => setNewRoute({ ...newRoute, priority: parseInt(e.target.value) || 0 })}
                            />
                          </div>
                          <div className="flex items-center justify-between">
                            <Label htmlFor="strip-prefix">Strip Prefix</Label>
                            <Switch
                              id="strip-prefix"
                              checked={newRoute.strip_prefix}
                              onCheckedChange={(checked) => setNewRoute({ ...newRoute, strip_prefix: checked })}
                            />
                          </div>
                        </div>
                        <DialogFooter>
                          <Button variant="outline" onClick={() => setRouteDialogOpen(false)}>
                            Cancel
                          </Button>
                          <Button onClick={handleAddRoute} disabled={!newRoute.path || !newRoute.pool}>
                            Add Route
                          </Button>
                        </DialogFooter>
                      </DialogContent>
                    </Dialog>
                  </div>
                </CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    {[...selectedListener.routes]
                      .sort((a, b) => b.priority - a.priority)
                      .map((route) => (
                      <div
                        key={route.id}
                        className="flex flex-wrap items-center justify-between gap-2 p-3 rounded-lg border hover:bg-accent transition-colors"
                      >
                        <div className="flex items-center gap-3 min-w-0">
                          <div className="text-sm font-medium text-muted-foreground shrink-0 w-10">
                            P{route.priority}
                          </div>
                          <div className="min-w-0">
                            <div className="font-medium truncate">{route.path || '/'}</div>
                            <div className="text-sm text-muted-foreground">
                              → {route.pool}
                            </div>
                          </div>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          {route.methods.length > 0 && (
                            <div className="flex flex-wrap gap-1">
                              {route.methods.map(m => (
                                <Badge key={m} variant="secondary" className="text-xs">
                                  {m}
                                </Badge>
                              ))}
                            </div>
                          )}
                          {route.strip_prefix && (
                            <Badge variant="outline" className="text-xs">Strip Prefix</Badge>
                          )}
                          <Button variant="ghost" size="icon" className="h-9 w-9 shrink-0" aria-label="Edit route">
                            <Edit className="h-4 w-4" />
                          </Button>
									<Button
                            variant="ghost"
                            size="icon"
                            className="h-9 w-9 shrink-0 text-destructive" aria-label="Delete route"
                            onClick={() => handleDeleteRoute(route.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle className="text-base">TLS Configuration</CardTitle>
                </CardHeader>
                <CardContent>
                  {selectedListener.protocol === 'https' ? (
                    <div className="space-y-3 text-sm">
                      <div className="flex flex-wrap justify-between gap-2">
                        <span className="text-muted-foreground">Certificate</span>
                        <span className="font-medium break-all">*.openloadbalancer.dev</span>
                      </div>
                      <div className="flex flex-wrap justify-between gap-2">
                        <span className="text-muted-foreground">TLS Version</span>
                        <span className="font-medium">1.3</span>
                      </div>
                      <div className="flex flex-wrap justify-between gap-2">
                        <span className="text-muted-foreground">ALPN</span>
                        <span className="font-medium">h2, http/1.1</span>
                      </div>
                    </div>
                  ) : (
                    <p className="text-sm text-muted-foreground">
                      TLS is not configured for this listener
                    </p>
                  )}
                </CardContent>
              </Card>
            </div>
          ) : (
            <div className="flex h-64 items-center justify-center text-muted-foreground">
              Select a listener to view details
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
