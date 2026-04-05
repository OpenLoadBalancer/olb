import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import {
  ScrollText,
  Shield,
  Clock,
  Globe,
  Code,
  Zap,
  Server,
  Lock,
  AlertCircle,
  ChevronRight
} from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'

interface MiddlewareConfig {
  name: string
  enabled: boolean
  type: string
  description: string
  icon: React.ComponentType<{ className?: string }>
  settings?: Record<string, any>
}

const middlewareTypes: MiddlewareConfig[] = [
  {
    name: 'Rate Limit',
    enabled: false,
    type: 'rate_limit',
    description: 'Limit requests per second per IP',
    icon: Clock,
    settings: { requests_per_second: 100 }
  },
  {
    name: 'CORS',
    enabled: false,
    type: 'cors',
    description: 'Cross-Origin Resource Sharing headers',
    icon: Globe,
    settings: {
      allowed_origins: ['*'],
      allowed_methods: ['GET', 'POST', 'PUT', 'DELETE'],
      allowed_headers: ['*']
    }
  },
  {
    name: 'Compression',
    enabled: false,
    type: 'compression',
    description: 'Gzip/Brotli response compression',
    icon: Zap,
    settings: { level: 6 }
  },
  {
    name: 'Security Headers',
    enabled: false,
    type: 'security_headers',
    description: 'HSTS, CSP, X-Frame-Options, etc.',
    icon: Shield,
    settings: {
      hsts: true,
      csp: "default-src 'self'",
      x_frame_options: 'DENY'
    }
  },
  {
    name: 'Request ID',
    enabled: false,
    type: 'request_id',
    description: 'Add unique request IDs to headers',
    icon: Code,
    settings: { header_name: 'X-Request-ID' }
  },
  {
    name: 'Strip Prefix',
    enabled: false,
    type: 'strip_prefix',
    description: 'Remove path prefix before forwarding',
    icon: Server,
    settings: { prefix: '/api' }
  },
  {
    name: 'Basic Auth',
    enabled: false,
    type: 'basic_auth',
    description: 'HTTP Basic Authentication',
    icon: Lock,
    settings: {}
  },
  {
    name: 'IP Allowlist',
    enabled: false,
    type: 'ip_allowlist',
    description: 'Restrict access by IP address',
    icon: Shield,
    settings: { allowlist: [] }
  }
]

export function MiddlewarePage() {
  const queryClient = useQueryClient()
  const [selectedMiddleware, setSelectedMiddleware] = useState<MiddlewareConfig | null>(null)
  const [isConfigOpen, setIsConfigOpen] = useState(false)
  const [activeTab, setActiveTab] = useState('all')

  const { data: config = {} } = useQuery({
    queryKey: ['middleware-config'],
    queryFn: async () => {
      const response = await api.get('/api/v1/config')
      return response.data.middleware || {}
    }
  })

  const updateMutation = useMutation({
    mutationFn: (data: any) => api.put('/api/v1/config/middleware', data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['middleware-config'] })
      toast.success('Middleware configuration updated')
      setIsConfigOpen(false)
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to update configuration')
    }
  })

  const toggleMiddleware = (type: string, enabled: boolean) => {
    updateMutation.mutate({
      ...config,
      [type]: { ...config[type], enabled }
    })
  }

  const filteredMiddleware =
    activeTab === 'all'
      ? middlewareTypes
      : middlewareTypes.filter((m) => (config[m.type]?.enabled || m.enabled) === (activeTab === 'enabled'))

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Middleware</h1>
          <p className="text-muted-foreground">
            Configure middleware components for request processing
          </p>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="enabled">Enabled</TabsTrigger>
          <TabsTrigger value="disabled">Disabled</TabsTrigger>
        </TabsList>
      </Tabs>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {filteredMiddleware.map((middleware) => {
          const Icon = middleware.icon
          const isEnabled = config[middleware.type]?.enabled || middleware.enabled

          return (
            <Card
              key={middleware.type}
              className={`cursor-pointer transition-colors hover:border-primary ${
                isEnabled ? 'border-primary/50' : ''
              }`}
              onClick={() => {
                setSelectedMiddleware(middleware)
                setIsConfigOpen(true)
              }}
            >
              <CardHeader>
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <div
                      className={`rounded-lg p-2 ${
                        isEnabled ? 'bg-primary text-primary-foreground' : 'bg-muted'
                      }`}
                    >
                      <Icon className="h-5 w-5" />
                    </div>
                    <div>
                      <CardTitle className="text-base">{middleware.name}</CardTitle>
                      <Badge
                        variant={isEnabled ? 'success' : 'secondary'}
                        className="mt-1"
                      >
                        {isEnabled ? 'Enabled' : 'Disabled'}
                      </Badge>
                    </div>
                  </div>
                  <ChevronRight className="h-4 w-4 text-muted-foreground" />
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">
                  {middleware.description}
                </p>
                {isEnabled && config[middleware.type] && (
                  <div className="mt-4 border-t pt-4">
                    <p className="text-xs font-medium text-muted-foreground">
                      Configuration:
                    </p>
                    <pre className="mt-2 text-xs text-muted-foreground overflow-hidden">
                      {JSON.stringify(config[middleware.type], null, 2).slice(0, 100)}
                      {JSON.stringify(config[middleware.type], null, 2).length > 100
                        ? '...'
                        : ''}
                    </pre>
                  </div>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>

      <Dialog open={isConfigOpen} onOpenChange={setIsConfigOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{selectedMiddleware?.name} Configuration</DialogTitle>
            <DialogDescription>{selectedMiddleware?.description}</DialogDescription>
          </DialogHeader>
          <div className="space-y-6 py-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {selectedMiddleware && (
                  <selectedMiddleware.icon className="h-5 w-5" />
                )}
                <Label htmlFor="enabled">Enable Middleware</Label>
              </div>
              <Switch
                id="enabled"
                checked={
                  selectedMiddleware
                    ? config[selectedMiddleware.type]?.enabled || false
                    : false
                }
                onCheckedChange={(checked) =>
                  selectedMiddleware && toggleMiddleware(selectedMiddleware.type, checked)
                }
              />
            </div>

            {selectedMiddleware?.settings && (
              <div className="space-y-4">
                <h4 className="text-sm font-medium">Settings</h4>
                {Object.entries(selectedMiddleware.settings).map(([key, value]) => (
                  <div key={key} className="space-y-2">
                    <Label htmlFor={key}>
                      {key.replace(/_/g, ' ').replace(/\b\w/g, (l) => l.toUpperCase())}
                    </Label>
                    {Array.isArray(value) ? (
                      <Input
                        id={key}
                        value={value.join(', ')}
                        placeholder="Comma-separated values"
                        onChange={(e) => {
                          // Handle array input
                        }}
                      />
                    ) : typeof value === 'number' ? (
                      <Input
                        id={key}
                        type="number"
                        value={value}
                        onChange={(e) => {
                          // Handle number input
                        }}
                      />
                    ) : (
                      <Input
                        id={key}
                        value={value as string}
                        onChange={(e) => {
                          // Handle string input
                        }}
                      />
                    )}
                  </div>
                ))}
              </div>
            )}

            <div className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-3 text-sm text-amber-600 dark:text-amber-400">
              <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
              <p>
                Changes to middleware configuration will take effect immediately for all
                incoming requests.
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsConfigOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                if (selectedMiddleware) {
                  updateMutation.mutate({
                    ...config,
                    [selectedMiddleware.type]: {
                      ...config[selectedMiddleware.type],
                      ...selectedMiddleware.settings,
                      enabled: true
                    }
                  })
                }
              }}
              disabled={updateMutation.isPending}
            >
              Save Configuration
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
