import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Slider } from '@/components/ui/slider'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DataTable } from '@/components/data-table'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import type { ColumnDef } from '@tanstack/react-table'
import {
  Shield,
  Plus,
  Globe,
  Clock,
  Zap,
  AlertTriangle,
  Ban,
  Activity,
  User,
  Server,
  Trash2,
  Edit,
  Play,
  Pause
} from 'lucide-react'

interface RateLimitRule {
  id: string
  name: string
  enabled: boolean
  scope: 'ip' | 'user' | 'global' | 'path'
  limit: number
  window: string
  burst: number
  action: 'block' | 'throttle' | 'log'
  path?: string
  description: string
}

const mockRules: RateLimitRule[] = [
  {
    id: '1',
    name: 'General API Rate Limit',
    enabled: true,
    scope: 'ip',
    limit: 1000,
    window: '1m',
    burst: 50,
    action: 'throttle',
    description: 'Default rate limit per IP address'
  },
  {
    id: '2',
    name: 'Login Attempt Limit',
    enabled: true,
    scope: 'ip',
    limit: 5,
    window: '5m',
    burst: 0,
    action: 'block',
    description: 'Prevent brute force login attempts'
  },
  {
    id: '3',
    name: 'Admin API Protection',
    enabled: true,
    scope: 'user',
    limit: 100,
    window: '1m',
    burst: 10,
    action: 'block',
    path: '/admin/*',
    description: 'Stricter limits for admin endpoints'
  },
  {
    id: '4',
    name: 'Global Burst Protection',
    enabled: false,
    scope: 'global',
    limit: 10000,
    window: '1s',
    burst: 1000,
    action: 'throttle',
    description: 'Global rate limiting across all requests'
  }
]

const ruleColumns: ColumnDef<RateLimitRule>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Shield className="h-4 w-4 text-muted-foreground" />
        <div>
          <p className="font-medium">{row.original.name}</p>
          <p className="text-xs text-muted-foreground">{row.original.description}</p>
        </div>
      </div>
    )
  },
  {
    accessorKey: 'enabled',
    header: 'Status',
    cell: ({ row }) => (
      <Badge variant={row.original.enabled ? 'default' : 'secondary'}>
        {row.original.enabled ? 'Active' : 'Disabled'}
      </Badge>
    )
  },
  {
    accessorKey: 'scope',
    header: 'Scope',
    cell: ({ row }) => {
      const scopeIcons = {
        ip: Globe,
        user: User,
        global: Server,
        path: Activity
      }
      const Icon = scopeIcons[row.original.scope]
      return (
        <div className="flex items-center gap-1">
          <Icon className="h-3.5 w-3.5" />
          <span className="capitalize">{row.original.scope}</span>
        </div>
      )
    }
  },
  {
    accessorKey: 'limit',
    header: 'Limit',
    cell: ({ row }) => (
      <span className="font-mono">{row.original.limit}/{row.original.window}</span>
    )
  },
  {
    accessorKey: 'action',
    header: 'Action',
    cell: ({ row }) => {
      const actionColors = {
        block: 'text-red-500 bg-red-500/10',
        throttle: 'text-yellow-500 bg-yellow-500/10',
        log: 'text-blue-500 bg-blue-500/10'
      }
      return (
        <Badge variant="outline" className={actionColors[row.original.action]}>
          {row.original.action}
        </Badge>
      )
    }
  }
]

export function RateLimitConfig() {
  const [rules, setRules] = useState<RateLimitRule[]>(mockRules)
  const [activeTab, setActiveTab] = useState('rules')
  const [showRuleDialog, setShowRuleDialog] = useState(false)
  const [editingRule, setEditingRule] = useState<RateLimitRule | null>(null)
  const [globalEnabled, setGlobalEnabled] = useState(true)
  const [globalRate, setGlobalRate] = useState(10000)

  const toggleRule = (id: string) => {
    setRules(prev =>
      prev.map(r =>
        r.id === id ? { ...r, enabled: !r.enabled } : r
      )
    )
  }

  const deleteRule = (rule: RateLimitRule) => {
    toast.success(`Deleted rule: ${rule.name}`)
  }

  const saveRule = () => {
    toast.success(editingRule ? 'Rule updated' : 'Rule created')
    setShowRuleDialog(false)
    setEditingRule(null)
  }

  const stats = {
    active: rules.filter(r => r.enabled).length,
    total: rules.length,
    blocked: 234,
    throttled: 1892
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Rate Limiting</h1>
          <p className="text-muted-foreground">
            Configure request rate limits and throttling
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button onClick={() => { setEditingRule(null); setShowRuleDialog(true) }}>
            <Plus className="mr-2 h-4 w-4" />
            Add Rule
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Rules</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.active}</div>
            <p className="text-xs text-muted-foreground">of {stats.total} total</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Blocked Today</CardTitle>
            <Ban className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.blocked}</div>
            <p className="text-xs text-muted-foreground">Requests rejected</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Throttled</CardTitle>
            <Zap className="h-4 w-4 text-yellow-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-yellow-500">{stats.throttled}</div>
            <p className="text-xs text-muted-foreground">Requests slowed</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Global Rate</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{globalRate.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground">req/min capacity</p>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="rules">Rules</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
          <TabsTrigger value="blocked">Blocked IPs</TabsTrigger>
        </TabsList>

        <TabsContent value="rules" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Rate Limit Rules</CardTitle>
              <CardDescription>Manage rate limiting rules</CardDescription>
            </CardHeader>
            <CardContent>
              <DataTable
                data={rules}
                columns={ruleColumns}
                actions={[
                  {
                    label: rule => rule.enabled ? 'Disable' : 'Enable',
                    icon: rule => rule.enabled ? Pause : Play,
                    onClick: (rule) => toggleRule(rule.id)
                  },
                  {
                    label: 'Edit',
                    icon: Edit,
                    onClick: (rule) => {
                      setEditingRule(rule)
                      setShowRuleDialog(true)
                    }
                  },
                  {
                    label: 'Delete',
                    icon: Trash2,
                    variant: 'destructive',
                    onClick: deleteRule
                  }
                ]}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Global Settings</CardTitle>
              <CardDescription>Configure default rate limiting behavior</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Enable Rate Limiting</Label>
                  <p className="text-sm text-muted-foreground">Globally enable/disable rate limiting</p>
                </div>
                <Switch checked={globalEnabled} onCheckedChange={setGlobalEnabled} />
              </div>

              <div className="space-y-2">
                <Label>Default Rate Limit</Label>
                <div className="flex items-center gap-4">
                  <Slider
                    value={[globalRate]}
                    onValueChange={([v]) => setGlobalRate(v)}
                    min={100}
                    max={50000}
                    step={100}
                    className="flex-1"
                  />
                  <span className="w-20 text-right font-mono">{globalRate}</span>
                </div>
                <p className="text-xs text-muted-foreground">Requests per minute</p>
              </div>

              <div className="space-y-2">
                <Label>Default Action</Label>
                <Select defaultValue="throttle">
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="block">Block</SelectItem>
                    <SelectItem value="throttle">Throttle</SelectItem>
                    <SelectItem value="log">Log Only</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label>Whitelist IPs</Label>
                <Input placeholder="10.0.0.0/8, 192.168.1.1" />
                <p className="text-xs text-muted-foreground">Comma-separated list of IP addresses or CIDR ranges</p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="blocked" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Blocked IP Addresses</CardTitle>
              <CardDescription>Currently blocked IPs due to rate limit violations</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {[
                  { ip: '203.0.113.45', reason: 'Exceeded login rate limit', blockedAt: '2 min ago', expires: '3 min' },
                  { ip: '198.51.100.12', reason: 'Global rate limit exceeded', blockedAt: '5 min ago', expires: '55 min' },
                  { ip: '192.0.2.78', reason: 'API abuse detected', blockedAt: '12 min ago', expires: '48 min' }
                ].map((block, i) => (
                  <div key={i} className="flex items-center justify-between p-3 rounded-lg border">
                    <div className="flex items-center gap-3">
                      <Ban className="h-4 w-4 text-red-500" />
                      <div>
                        <code className="text-sm font-mono">{block.ip}</code>
                        <p className="text-xs text-muted-foreground">{block.reason}</p>
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      <div className="text-right">
                        <p className="text-xs text-muted-foreground">Blocked {block.blockedAt}</p>
                        <p className="text-xs text-muted-foreground">Expires in {block.expires}</p>
                      </div>
                      <Button variant="ghost" size="sm">
                        Unblock
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Create/Edit Rule Dialog */}
      <Dialog open={showRuleDialog} onOpenChange={setShowRuleDialog}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingRule ? 'Edit Rule' : 'Create Rule'}</DialogTitle>
            <DialogDescription>Configure rate limiting rule</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Name</Label>
              <Input defaultValue={editingRule?.name} placeholder="Rule name" />
            </div>
            <div className="space-y-2">
              <Label>Description</Label>
              <Input defaultValue={editingRule?.description} placeholder="Rule description" />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Scope</Label>
                <Select defaultValue={editingRule?.scope || 'ip'}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ip">IP Address</SelectItem>
                    <SelectItem value="user">User</SelectItem>
                    <SelectItem value="global">Global</SelectItem>
                    <SelectItem value="path">Path</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Action</Label>
                <Select defaultValue={editingRule?.action || 'throttle'}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="block">Block</SelectItem>
                    <SelectItem value="throttle">Throttle</SelectItem>
                    <SelectItem value="log">Log Only</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Limit</Label>
                <Input type="number" defaultValue={editingRule?.limit || 100} />
              </div>
              <div className="space-y-2">
                <Label>Window</Label>
                <Select defaultValue={editingRule?.window || '1m'}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1s">1 second</SelectItem>
                    <SelectItem value="10s">10 seconds</SelectItem>
                    <SelectItem value="1m">1 minute</SelectItem>
                    <SelectItem value="5m">5 minutes</SelectItem>
                    <SelectItem value="1h">1 hour</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-2">
              <Label>Burst Size</Label>
              <Input type="number" defaultValue={editingRule?.burst || 10} />
              <p className="text-xs text-muted-foreground">Allow short bursts above the rate limit</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRuleDialog(false)}>
              Cancel
            </Button>
            <Button onClick={saveRule}>
              {editingRule ? 'Update Rule' : 'Create Rule'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
