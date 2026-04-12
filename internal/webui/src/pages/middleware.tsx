import { useState } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { LoadingCard } from "@/components/ui/loading"
import { cn } from "@/lib/utils"
import { useMiddlewareStatus, useConfig } from "@/hooks/use-query"
import { APIMiddlewareStatusItem } from "@/types"
import { Clock, Globe, Lock, Zap, Server, ScrollText, Shield, Code, Settings2, ShieldCheck, Key, Fingerprint, Repeat, Ban, Timer, Maximize, Activity, BarChart3, ArrowRightLeft, FileCheck, Scissors, Eye, Bug, MapPin, GitBranch } from "lucide-react"

// Map middleware IDs to icons
const mwIcons: Record<string, React.ComponentType<{ className?: string }>> = {
  rate_limit: Clock,
  cors: Globe,
  csp: ShieldCheck,
  compression: Zap,
  circuit_breaker: Activity,
  retry: Repeat,
  cache: Server,
  ip_filter: Ban,
  headers: FileCheck,
  timeout: Timer,
  max_body_size: Maximize,
  jwt: Lock,
  oauth2: Key,
  basic_auth: Shield,
  api_key: Fingerprint,
  hmac: ShieldCheck,
  transformer: Code,
  request_id: GitBranch,
  logging: ScrollText,
  metrics: BarChart3,
  rewrite: ArrowRightLeft,
  forcessl: ShieldCheck,
  csrf: Shield,
  secure_headers: ShieldCheck,
  coalesce: Zap,
  bot_detection: Bug,
  real_ip: MapPin,
  trace: Eye,
  validator: FileCheck,
  strip_prefix: Scissors,
}

const categoryColors: Record<string, string> = {
  security: "bg-red-500/10 text-red-600",
  performance: "bg-green-500/10 text-green-600",
  traffic: "bg-blue-500/10 text-blue-600",
  observability: "bg-purple-500/10 text-purple-600",
}

export function MiddlewarePage() {
  useDocumentTitle("Middleware")
  const { data: middlewareStatus, isLoading } = useMiddlewareStatus()
  const { data: config } = useConfig()
  const [selectedCategory, setSelectedCategory] = useState<string>("all")
  const [configDialogOpen, setConfigDialogOpen] = useState(false)
  const [selectedMiddleware, setSelectedMiddleware] = useState<APIMiddlewareStatusItem | null>(null)

  const middlewares = middlewareStatus ?? []

  const filteredMiddleware = selectedCategory === "all"
    ? middlewares
    : middlewares.filter(m => m.category === selectedCategory)

  // Get the current config values for a middleware
  const getMWConfig = (id: string): Record<string, any> => {
    if (!config || typeof config !== 'object') return {}
    const c = config as any
    if (!c.middleware || !c.middleware[id]) return {}
    return c.middleware[id]
  }

  const openConfigDialog = (middleware: APIMiddlewareStatusItem) => {
    setSelectedMiddleware(middleware)
    setConfigDialogOpen(true)
  }

  const categories = [
    { id: "all", label: "All" },
    { id: "security", label: "Security" },
    { id: "performance", label: "Performance" },
    { id: "traffic", label: "Traffic" },
    { id: "observability", label: "Observability" },
  ]

  const renderConfigView = () => {
    if (!selectedMiddleware) return null
    const cfg = getMWConfig(selectedMiddleware.id)
    if (!cfg || Object.keys(cfg).length === 0) {
      return <p className="text-sm text-muted-foreground">No configuration available (not configured in config file).</p>
    }
    // Show config as readable key-value pairs
    return (
      <div className="space-y-3">
        {Object.entries(cfg).map(([key, value]) => (
          <div key={key} className="grid gap-1">
            <Label className="text-xs text-muted-foreground capitalize">{key.replace(/_/g, ' ')}</Label>
            <div className="text-sm font-mono bg-muted p-2 rounded">
              {typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value)}
            </div>
          </div>
        ))}
      </div>
    )
  }

  const enabledCount = middlewares.filter(m => m.enabled).length

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Middleware</h1>
          <p className="text-muted-foreground">Configure request/response middleware chain</p>
        </div>
        <LoadingCard />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Middleware</h1>
          <p className="text-muted-foreground">
            {enabledCount} of {middlewares.length} middleware components enabled
          </p>
        </div>
      </div>

      <div className="flex gap-2">
        {categories.map(cat => (
          <Button
            key={cat.id}
            variant={selectedCategory === cat.id ? "default" : "outline"}
            size="sm"
            onClick={() => setSelectedCategory(cat.id)}
            aria-pressed={selectedCategory === cat.id}
          >
            {cat.label}
          </Button>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {filteredMiddleware.map((middleware) => {
          const Icon = mwIcons[middleware.id] || Settings2
          return (
            <Card key={middleware.id} className={cn(
              "transition-colors",
              middleware.enabled ? "border-primary/50" : ""
            )}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <div className={cn(
                      "p-2 rounded-lg",
                      middleware.enabled ? "bg-primary/10" : "bg-muted"
                    )}>
                      <Icon className={cn(
                        "h-5 w-5",
                        middleware.enabled ? "text-primary" : "text-muted-foreground"
                      )} />
                    </div>
                    <div>
                      <CardTitle className="text-base">{middleware.name}</CardTitle>
                      <Badge variant="outline" className={cn("text-xs capitalize mt-1", categoryColors[middleware.category])}>
                        {middleware.category}
                      </Badge>
                    </div>
                  </div>
                  <Badge variant={middleware.enabled ? 'default' : 'secondary'}>
                    {middleware.enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground mb-4">{middleware.description}</p>
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={() => openConfigDialog(middleware)}
                >
                  <Settings2 className="mr-2 h-4 w-4" />
                  View Configuration
                </Button>
              </CardContent>
            </Card>
          )
        })}
      </div>

      <Dialog open={configDialogOpen} onOpenChange={setConfigDialogOpen}>
        <DialogContent className="sm:max-w-[500px]">
          <DialogHeader>
            <DialogTitle>
              {selectedMiddleware?.name} Configuration
            </DialogTitle>
            <DialogDescription>
              {selectedMiddleware?.description}
              {!selectedMiddleware?.enabled && (
                <span className="block mt-1 text-amber-500">This middleware is currently disabled.</span>
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="py-4">
            {renderConfigView()}
            <p className="text-xs text-muted-foreground mt-4">
              Middleware is configured via the config file. Edit the config file and reload to make changes.
            </p>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfigDialogOpen(false)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
