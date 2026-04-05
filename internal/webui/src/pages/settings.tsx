import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Settings, Save, RotateCcw, Server, Globe, Shield, Bell } from "lucide-react"
import { toast } from "sonner"

export function SettingsPage() {
  const [generalSettings, setGeneralSettings] = useState({
    instanceName: 'prod-olb-01',
    logLevel: 'info',
    maxConnections: 10000,
    gracefulShutdown: true,
  })

  const [adminSettings, setAdminSettings] = useState({
    apiEnabled: true,
    webUIEnabled: true,
    metricsEnabled: true,
    apiKey: '',
  })

  const [notificationSettings, setNotificationSettings] = useState({
    emailEnabled: false,
    slackEnabled: false,
    webhookEnabled: false,
    onBackendDown: true,
    onHighLatency: true,
    onWAFBlock: false,
  })

  const handleSave = (section: string) => {
    toast.success(`${section} settings saved`)
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground">Configure OpenLoadBalancer instance</p>
      </div>

      <Tabs defaultValue="general" className="space-y-4">
        <TabsList>
          <TabsTrigger value="general" className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            General
          </TabsTrigger>
          <TabsTrigger value="admin" className="flex items-center gap-2">
            <Server className="h-4 w-4" />
            Admin
          </TabsTrigger>
          <TabsTrigger value="network" className="flex items-center gap-2">
            <Globe className="h-4 w-4" />
            Network
          </TabsTrigger>
          <TabsTrigger value="security" className="flex items-center gap-2">
            <Shield className="h-4 w-4" />
            Security
          </TabsTrigger>
          <TabsTrigger value="notifications" className="flex items-center gap-2">
            <Bell className="h-4 w-4" />
            Notifications
          </TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>General Settings</CardTitle>
              <CardDescription>Basic instance configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="instance-name">Instance Name</Label>
                  <Input
                    id="instance-name"
                    value={generalSettings.instanceName}
                    onChange={(e) => setGeneralSettings({ ...generalSettings, instanceName: e.target.value })}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="log-level">Log Level</Label>
                  <select
                    id="log-level"
                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background"
                    value={generalSettings.logLevel}
                    onChange={(e) => setGeneralSettings({ ...generalSettings, logLevel: e.target.value })}
                  >
                    <option value="debug">Debug</option>
                    <option value="info">Info</option>
                    <option value="warn">Warning</option>
                    <option value="error">Error</option>
                  </select>
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="max-connections">Max Connections</Label>
                <Input
                  id="max-connections"
                  type="number"
                  value={generalSettings.maxConnections}
                  onChange={(e) => setGeneralSettings({ ...generalSettings, maxConnections: parseInt(e.target.value) })}
                />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Graceful Shutdown</Label>
                  <p className="text-sm text-muted-foreground">Wait for active connections to close on shutdown</p>
                </div>
                <Switch
                  checked={generalSettings.gracefulShutdown}
                  onCheckedChange={(checked) => setGeneralSettings({ ...generalSettings, gracefulShutdown: checked })}
                />
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => toast.info("Settings reset")}>
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Reset
                </Button>
                <Button onClick={() => handleSave('General')}>
                  <Save className="mr-2 h-4 w-4" />
                  Save Changes
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="admin" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Admin Interface</CardTitle>
              <CardDescription>API and Web UI settings</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>API Enabled</Label>
                  <p className="text-sm text-muted-foreground">Enable REST API endpoints</p>
                </div>
                <Switch
                  checked={adminSettings.apiEnabled}
                  onCheckedChange={(checked) => setAdminSettings({ ...adminSettings, apiEnabled: checked })}
                />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Web UI Enabled</Label>
                  <p className="text-sm text-muted-foreground">Enable web interface</p>
                </div>
                <Switch
                  checked={adminSettings.webUIEnabled}
                  onCheckedChange={(checked) => setAdminSettings({ ...adminSettings, webUIEnabled: checked })}
                />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Metrics Enabled</Label>
                  <p className="text-sm text-muted-foreground">Enable Prometheus metrics endpoint</p>
                </div>
                <Switch
                  checked={adminSettings.metricsEnabled}
                  onCheckedChange={(checked) => setAdminSettings({ ...adminSettings, metricsEnabled: checked })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="api-key">API Key</Label>
                <Input
                  id="api-key"
                  type="password"
                  placeholder="Enter API key for authentication"
                  value={adminSettings.apiKey}
                  onChange={(e) => setAdminSettings({ ...adminSettings, apiKey: e.target.value })}
                />
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => toast.info("Settings reset")}>
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Reset
                </Button>
                <Button onClick={() => handleSave('Admin')}>
                  <Save className="mr-2 h-4 w-4" />
                  Save Changes
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="network" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Network Settings</CardTitle>
              <CardDescription>TCP and connection settings</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="tcp-keepalive">TCP Keepalive</Label>
                  <select
                    id="tcp-keepalive"
                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  >
                    <option value="30s">30 seconds</option>
                    <option value="1m">1 minute</option>
                    <option value="5m">5 minutes</option>
                  </select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="read-timeout">Read Timeout</Label>
                  <select
                    id="read-timeout"
                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  >
                    <option value="30s">30 seconds</option>
                    <option value="1m">1 minute</option>
                    <option value="5m">5 minutes</option>
                  </select>
                </div>
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>PROXY Protocol</Label>
                  <p className="text-sm text-muted-foreground">Accept PROXY protocol headers</p>
                </div>
                <Switch defaultChecked={false} />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>HTTP/2</Label>
                  <p className="text-sm text-muted-foreground">Enable HTTP/2 support</p>
                </div>
                <Switch defaultChecked={true} />
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => toast.info("Settings reset")}>
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Reset
                </Button>
                <Button onClick={() => handleSave('Network')}>
                  <Save className="mr-2 h-4 w-4" />
                  Save Changes
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="security" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Security Settings</CardTitle>
              <CardDescription>TLS and security configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Minimum TLS Version</Label>
                  <p className="text-sm text-muted-foreground">Minimum TLS version for HTTPS</p>
                </div>
                <select className="h-10 rounded-md border border-input bg-background px-3 py-2 text-sm">
                  <option value="1.2">TLS 1.2</option>
                  <option value="1.3">TLS 1.3</option>
                </select>
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>HSTS</Label>
                  <p className="text-sm text-muted-foreground">Enable HTTP Strict Transport Security</p>
                </div>
                <Switch defaultChecked={true} />
              </div>
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>OCSP Stapling</Label>
                  <p className="text-sm text-muted-foreground">Enable OCSP stapling for TLS</p>
                </div>
                <Switch defaultChecked={true} />
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => toast.info("Settings reset")}>
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Reset
                </Button>
                <Button onClick={() => handleSave('Security')}>
                  <Save className="mr-2 h-4 w-4" />
                  Save Changes
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="notifications" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Notifications</CardTitle>
              <CardDescription>Alert and notification settings</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-4">
                <h4 className="text-sm font-medium">Channels</h4>
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>Email Notifications</Label>
                  </div>
                  <Switch
                    checked={notificationSettings.emailEnabled}
                    onCheckedChange={(checked) => setNotificationSettings({ ...notificationSettings, emailEnabled: checked })}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>Slack Notifications</Label>
                  </div>
                  <Switch
                    checked={notificationSettings.slackEnabled}
                    onCheckedChange={(checked) => setNotificationSettings({ ...notificationSettings, slackEnabled: checked })}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>Webhook</Label>
                  </div>
                  <Switch
                    checked={notificationSettings.webhookEnabled}
                    onCheckedChange={(checked) => setNotificationSettings({ ...notificationSettings, webhookEnabled: checked })}
                  />
                </div>
              </div>

              <div className="space-y-4 pt-4 border-t">
                <h4 className="text-sm font-medium">Events</h4>
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>Backend Down</Label>
                    <p className="text-sm text-muted-foreground">Notify when a backend becomes unhealthy</p>
                  </div>
                  <Switch
                    checked={notificationSettings.onBackendDown}
                    onCheckedChange={(checked) => setNotificationSettings({ ...notificationSettings, onBackendDown: checked })}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>High Latency</Label>
                    <p className="text-sm text-muted-foreground">Notify when response time exceeds threshold</p>
                  </div>
                  <Switch
                    checked={notificationSettings.onHighLatency}
                    onCheckedChange={(checked) => setNotificationSettings({ ...notificationSettings, onHighLatency: checked })}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>WAF Block</Label>
                    <p className="text-sm text-muted-foreground">Notify on WAF rule triggers</p>
                  </div>
                  <Switch
                    checked={notificationSettings.onWAFBlock}
                    onCheckedChange={(checked) => setNotificationSettings({ ...notificationSettings, onWAFBlock: checked })}
                  />
                </div>
              </div>

              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => toast.info("Settings reset")}>
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Reset
                </Button>
                <Button onClick={() => handleSave('Notification')}>
                  <Save className="mr-2 h-4 w-4" />
                  Save Changes
                </Button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
