import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Plus, Globe, Shield, Trash2, Edit, Activity, Route } from "lucide-react"
import { toast } from "sonner"
import type { Listener } from "@/types"

const mockListeners: Listener[] = [
  {
    id: "1",
    name: "http-public",
    address: ":80",
    protocol: "http",
    routes: [
      { id: "r1", path: "/api/*", pool: "api-pool", methods: ["GET", "POST", "PUT", "DELETE"], strip_prefix: false, priority: 100 },
      { id: "r2", path: "/", pool: "web-pool", methods: ["GET"], strip_prefix: false, priority: 10 },
    ],
    enabled: true,
  },
  {
    id: "2",
    name: "https-public",
    address: ":443",
    protocol: "https",
    routes: [
      { id: "r3", path: "/api/*", pool: "api-pool", methods: ["GET", "POST", "PUT", "DELETE"], strip_prefix: false, priority: 100 },
      { id: "r4", path: "/grpc/*", pool: "grpc-pool", methods: ["POST"], strip_prefix: true, priority: 90 },
      { id: "r5", path: "/", pool: "web-pool", methods: ["GET"], strip_prefix: false, priority: 10 },
    ],
    enabled: true,
  },
  {
    id: "3",
    name: "tcp-internal",
    address: ":5432",
    protocol: "tcp",
    routes: [
      { id: "r6", path: "", pool: "db-pool", methods: [], strip_prefix: false, priority: 1 },
    ],
    enabled: false,
  },
]

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

export function ListenersPage() {
  const [listeners, setListeners] = useState<Listener[]>(mockListeners)
  const [selectedListener, setSelectedListener] = useState<Listener | null>(mockListeners[0])

  const toggleListener = (id: string) => {
    setListeners(prev => prev.map(l =>
      l.id === id ? { ...l, enabled: !l.enabled } : l
    ))
    const listener = listeners.find(l => l.id === id)
    toast.success(`${listener?.name} ${listener?.enabled ? 'disabled' : 'enabled'}`)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Listeners</h1>
          <p className="text-muted-foreground">Configure entry points and routing rules</p>
        </div>
        <Button onClick={() => toast.info("Create listener dialog would open")}>
          <Plus className="mr-2 h-4 w-4" />
          Create Listener
        </Button>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="space-y-4">
          {listeners.map((listener) => (
            <Card
              key={listener.id}
              className={`cursor-pointer transition-colors hover:bg-accent ${selectedListener?.id === listener.id ? 'border-primary' : ''}`}
              onClick={() => setSelectedListener(listener)}
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
              <div className="flex items-center justify-between">
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
                  <Button variant="destructive" size="sm" onClick={() => toast.info("Delete listener")}>
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
                    <Button size="sm" onClick={() => toast.info("Add route dialog would open")}>
                      <Plus className="mr-2 h-4 w-4" />
                      Add Route
                    </Button>
                  </div>
                </CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    {[...selectedListener.routes]
                      .sort((a, b) => b.priority - a.priority)
                      .map((route) => (
                      <div
                        key={route.id}
                        className="flex items-center justify-between p-3 rounded-lg border hover:bg-accent transition-colors"
                      >
                        <div className="flex items-center gap-4">
                          <div className="text-sm font-medium text-muted-foreground w-12">
                            P{route.priority}
                          </div>
                          <div>
                            <div className="font-medium">{route.path || '/'}</div>
                            <div className="text-sm text-muted-foreground">
                              → {route.pool}
                            </div>
                          </div>
                        </div>
                        <div className="flex items-center gap-3">
                          {route.methods.length > 0 && (
                            <div className="flex gap-1">
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
                          <div className="flex gap-1">
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <Edit className="h-4 w-4" />
                            </Button>
                            <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive">
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
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
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Certificate</span>
                        <span>*.openloadbalancer.dev</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">TLS Version</span>
                        <span>1.3</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">ALPN</span>
                        <span>h2, http/1.1</span>
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
