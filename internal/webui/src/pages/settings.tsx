import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Settings,
  Save,
  RotateCcw,
  Database,
  Bell,
  Shield,
  Globe,
  Server,
  AlertCircle,
  Code,
  FileJson,
  Terminal
} from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import { ConfigManager } from '@/components/config-manager'
import { ApiPlayground } from '@/components/api-playground'

export function SettingsPage() {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState('general')
  const [config, setConfig] = useState({
    log_level: 'info',
    log_format: 'json',
    admin_address: ':9090',
    graceful_shutdown_timeout: '30s',
    max_connections: 10000,
    enable_pprof: false,
    enable_metrics: true,
    metrics_interval: 10
  })

  const { data: currentConfig, isLoading } = useQuery({
    queryKey: ['config'],
    queryFn: async () => {
      const response = await api.get('/api/v1/config')
      return response.data
    }
  })

  const updateMutation = useMutation({
    mutationFn: (data: any) => api.put('/api/v1/config', data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      toast.success('Settings saved successfully')
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to save settings')
    }
  })

  const reloadMutation = useMutation({
    mutationFn: () => api.post('/api/v1/config/reload'),
    onSuccess: () => {
      toast.success('Configuration reloaded')
    }
  })

  const handleSave = () => {
    updateMutation.mutate({ ...currentConfig, ...config })
  }

  const handleReset = () => {
    if (currentConfig) {
      setConfig({
        log_level: currentConfig.log_level || 'info',
        log_format: currentConfig.log_format || 'json',
        admin_address: currentConfig.admin?.address || ':9090',
        graceful_shutdown_timeout: currentConfig.graceful_shutdown_timeout || '30s',
        max_connections: currentConfig.max_connections || 10000,
        enable_pprof: currentConfig.enable_pprof || false,
        enable_metrics: currentConfig.enable_metrics !== false,
        metrics_interval: currentConfig.metrics_interval || 10
      })
    }
    toast.info('Settings reset to saved values')
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
          <p className="text-muted-foreground">
            Configure global load balancer settings
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={handleReset}>
            <RotateCcw className="mr-2 h-4 w-4" />
            Reset
          </Button>
          <Button onClick={handleSave} disabled={updateMutation.isPending}>
            <Save className="mr-2 h-4 w-4" />
            Save Changes
          </Button>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-4">
        <TabsList>
          <TabsTrigger value="general">
            <Settings className="mr-2 h-4 w-4" />
            General
          </TabsTrigger>
          <TabsTrigger value="config">
            <FileJson className="mr-2 h-4 w-4" />
            Configuration
          </TabsTrigger>
          <TabsTrigger value="api">
            <Code className="mr-2 h-4 w-4" />
            API Playground
          </TabsTrigger>
          <TabsTrigger value="logging">
            <Database className="mr-2 h-4 w-4" />
            Logging
          </TabsTrigger>
          <TabsTrigger value="notifications">
            <Bell className="mr-2 h-4 w-4" />
            Notifications
          </TabsTrigger>
          <TabsTrigger value="security">
            <Shield className="mr-2 h-4 w-4" />
            Security
          </TabsTrigger>
          <TabsTrigger value="advanced">
            <Server className="mr-2 h-4 w-4" />
            Advanced
          </TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>General Settings</CardTitle>
              <CardDescription>Basic configuration options</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="admin-address">Admin API Address</Label>
                  <Input
                    id="admin-address"
                    value={config.admin_address}
                    onChange={(e) =>
                      setConfig({ ...config, admin_address: e.target.value })
                    }
                    placeholder=":9090"
                  />
                  <p className="text-xs text-muted-foreground">
                    Address for the admin API and web UI
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="shutdown-timeout">Graceful Shutdown Timeout</Label>
                  <Input
                    id="shutdown-timeout"
                    value={config.graceful_shutdown_timeout}
                    onChange={(e) =>
                      setConfig({ ...config, graceful_shutdown_timeout: e.target.value })
                    }
                    placeholder="30s"
                  />
                  <p className="text-xs text-muted-foreground">
                    Time to wait for connections to close
                  </p>
                </div>
              </div>

              <div className="flex items-center justify-between rounded-lg border p-4">
                <div className="flex items-center gap-3">
                  <Globe className="h-5 w-5 text-muted-foreground" />
                  <div>
                    <p className="font-medium">Enable Metrics</p>
                    <p className="text-sm text-muted-foreground">
                      Export Prometheus-compatible metrics
                    </p>
                  </div>
                </div>
                <Switch
                  checked={config.enable_metrics}
                  onCheckedChange={(checked) =>
                    setConfig({ ...config, enable_metrics: checked })
                  }
                />
              </div>
            </CardContent>
          </Card>

          <div className="flex items-start gap-2 rounded-lg bg-amber-500/10 p-4 text-sm text-amber-600 dark:text-amber-400">
            <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
            <p>
              Some changes may require a restart of the load balancer to take effect.
              Use the Reload button to apply changes without restart where supported.
            </p>
          </div>
        </TabsContent>

        <TabsContent value="config">
          <ConfigManager />
        </TabsContent>

        <TabsContent value="api">
          <ApiPlayground />
        </TabsContent>

        <TabsContent value="logging" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Logging Configuration</CardTitle>
              <CardDescription>Control log output and verbosity</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="log-level">Log Level</Label>
                  <Select
                    value={config.log_level}
                    onValueChange={(value) =>
                      setConfig({ ...config, log_level: value })
                    }
                  >
                    <SelectTrigger id="log-level">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="debug">Debug</SelectItem>
                      <SelectItem value="info">Info</SelectItem>
                      <SelectItem value="warn">Warning</SelectItem>
                      <SelectItem value="error">Error</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="log-format">Log Format</Label>
                  <Select
                    value={config.log_format}
                    onValueChange={(value) =>
                      setConfig({ ...config, log_format: value })
                    }
                  >
                    <SelectTrigger id="log-format">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="json">JSON</SelectItem>
                      <SelectItem value="text">Text</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="notifications" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Notifications</CardTitle>
              <CardDescription>Configure alerts and notifications</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-center py-8 text-muted-foreground">
                <Bell className="h-8 w-8 mx-auto mb-2" />
                <p>Notification settings coming soon</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="security" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Security Settings</CardTitle>
              <CardDescription>Security-related configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between rounded-lg border p-4">
                <div className="flex items-center gap-3">
                  <Shield className="h-5 w-5 text-muted-foreground" />
                  <div>
                    <p className="font-medium">Enable Profiling</p>
                    <p className="text-sm text-muted-foreground">
                      Expose pprof endpoints for debugging (security risk)
                    </p>
                  </div>
                </div>
                <Switch
                  checked={config.enable_pprof}
                  onCheckedChange={(checked) =>
                    setConfig({ ...config, enable_pprof: checked })
                  }
                />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="advanced" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Advanced Settings</CardTitle>
              <CardDescription>Performance and tuning options</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-2">
                <Label htmlFor="max-connections">Max Connections</Label>
                <Input
                  id="max-connections"
                  type="number"
                  value={config.max_connections}
                  onChange={(e) =>
                    setConfig({ ...config, max_connections: parseInt(e.target.value) })
                  }
                />
                <p className="text-xs text-muted-foreground">
                  Maximum concurrent connections (0 = unlimited)
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="metrics-interval">Metrics Collection Interval (seconds)</Label>
                <Input
                  id="metrics-interval"
                  type="number"
                  value={config.metrics_interval}
                  onChange={(e) =>
                    setConfig({ ...config, metrics_interval: parseInt(e.target.value) })
                  }
                />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Configuration Management</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Reload Configuration</p>
                  <p className="text-sm text-muted-foreground">
                    Reload config from file without restart
                  </p>
                </div>
                <Button
                  variant="outline"
                  onClick={() => reloadMutation.mutate()}
                  disabled={reloadMutation.isPending}
                >
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Reload
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
