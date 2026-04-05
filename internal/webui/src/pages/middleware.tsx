import { useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Clock, Globe, Lock, Zap, Server, ScrollText } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

interface MiddlewareItem {
  id: string
  name: string
  description: string
  enabled: boolean
  icon: React.ComponentType<{ className?: string }>
  category: "security" | "performance" | "traffic" | "observability"
}

const middlewareList: MiddlewareItem[] = [
  {
    id: "rate_limit",
    name: "Rate Limiting",
    description: "Limit requests per IP or user",
    enabled: true,
    icon: Clock,
    category: "traffic"
  },
  {
    id: "cors",
    name: "CORS",
    description: "Cross-Origin Resource Sharing",
    enabled: true,
    icon: Globe,
    category: "security"
  },
  {
    id: "jwt",
    name: "JWT Auth",
    description: "JSON Web Token authentication",
    enabled: false,
    icon: Lock,
    category: "security"
  },
  {
    id: "compression",
    name: "Compression",
    description: "Gzip/Brotli response compression",
    enabled: true,
    icon: Zap,
    category: "performance"
  },
  {
    id: "cache",
    name: "HTTP Cache",
    description: "Response caching with TTL",
    enabled: false,
    icon: Server,
    category: "performance"
  },
  {
    id: "logging",
    name: "Access Logging",
    description: "Request/response logging",
    enabled: true,
    icon: ScrollText,
    category: "observability"
  },
]

export function MiddlewarePage() {
  const [middlewares, setMiddlewares] = useState<MiddlewareItem[]>(middlewareList)
  const [selectedCategory, setSelectedCategory] = useState<string>("all")

  const toggleMiddleware = (id: string) => {
    setMiddlewares(prev => prev.map(m =>
      m.id === id ? { ...m, enabled: !m.enabled } : m
    ))
    const mw = middlewares.find(m => m.id === id)
    toast.success(`${mw?.name} ${mw?.enabled ? 'disabled' : 'enabled'}`)
  }

  const filteredMiddleware = selectedCategory === "all"
    ? middlewares
    : middlewares.filter(m => m.category === selectedCategory)

  const categories = [
    { id: "all", label: "All" },
    { id: "security", label: "Security" },
    { id: "performance", label: "Performance" },
    { id: "traffic", label: "Traffic" },
    { id: "observability", label: "Observability" },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Middleware</h1>
          <p className="text-muted-foreground">Configure request/response middleware chain</p>
        </div>
        <Button onClick={() => toast.success("Configuration saved")}>
          Save Changes
        </Button>
      </div>

      <div className="flex gap-2">
        {categories.map(cat => (
          <Button
            key={cat.id}
            variant={selectedCategory === cat.id ? "default" : "outline"}
            size="sm"
            onClick={() => setSelectedCategory(cat.id)}
          >
            {cat.label}
          </Button>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {filteredMiddleware.map((middleware) => (
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
                    <middleware.icon className={cn(
                      "h-5 w-5",
                      middleware.enabled ? "text-primary" : "text-muted-foreground"
                    )} />
                  </div>
                  <div>
                    <CardTitle className="text-base">{middleware.name}</CardTitle>
                    <Badge variant={middleware.enabled ? "default" : "secondary"} className="mt-1">
                      {middleware.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </div>
                </div>
                <Switch
                  checked={middleware.enabled}
                  onCheckedChange={() => toggleMiddleware(middleware.id)}
                />
              </div>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">{middleware.description}</p>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
