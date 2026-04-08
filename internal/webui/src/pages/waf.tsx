import { useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
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
import { AlertTriangle, Ban, Globe, Bot, Lock, Plus, Trash2, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { useWAFStatus } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"

interface WAFRule {
  id: string
  name: string
  pattern: string
  action: 'block' | 'log' | 'challenge'
  enabled: boolean
  hits: number
  severity: 'low' | 'medium' | 'high' | 'critical'
  description: string
}

interface IPBlock {
  id: string
  ip: string
  reason: string
  expires: string
  added: string
}

interface RateLimitRule {
  id: string
  name: string
  requests: number
  window: string
  action: 'block' | 'challenge'
  enabled: boolean
}

export function WAFPage() {
  const { data: wafStatus, isLoading, error, refetch } = useWAFStatus()
  const [wafEnabled, setWafEnabled] = useState(true)
  const [wafMode, setWafMode] = useState<'enforce' | 'monitor'>('enforce')
  const [rules, setRules] = useState<WAFRule[]>([
    { id: "1", name: "SQL Injection", pattern: "(?i)(union|select|insert|update|delete|drop|create|alter)", action: "block", enabled: true, hits: 1523, severity: "critical", description: "Detects SQL injection attempts" },
    { id: "2", name: "XSS Attack", pattern: "(?i)(<script|javascript:|onerror=|onload=)", action: "block", enabled: true, hits: 892, severity: "high", description: "Cross-site scripting detection" },
    { id: "3", name: "Path Traversal", pattern: "\.\./|\\.\\./", action: "block", enabled: true, hits: 445, severity: "high", description: "Directory traversal attempts" },
    { id: "4", name: "Bot Detection", pattern: "(?i)(bot|crawler|spider|scrape)", action: "challenge", enabled: true, hits: 5678, severity: "medium", description: "Challenge suspicious bots" },
  ])
  const [blockedIPs, setBlockedIPs] = useState<IPBlock[]>([
    { id: "1", ip: "192.168.1.100", reason: "Rate limit exceeded", expires: "1 hour", added: "2 hours ago" },
    { id: "2", ip: "10.0.0.50", reason: "WAF rule triggered", expires: "24 hours", added: "5 hours ago" },
  ])
  const [rateLimits, setRateLimits] = useState<RateLimitRule[]>([
    { id: "1", name: "General API", requests: 1000, window: "1m", action: "block", enabled: true },
    { id: "2", name: "Login Attempts", requests: 5, window: "5m", action: "challenge", enabled: true },
  ])

  // Dialog states
  const [ruleDialogOpen, setRuleDialogOpen] = useState(false)
  const [ipDialogOpen, setIpDialogOpen] = useState(false)
  const [rateLimitDialogOpen, setRateLimitDialogOpen] = useState(false)

  const [newRule, setNewRule] = useState({
    name: "",
    pattern: "",
    action: "block" as 'block' | 'challenge' | 'log',
    severity: "medium" as 'low' | 'medium' | 'high' | 'critical',
    description: "",
  })

  const [newBlock, setNewBlock] = useState({
    ip: "",
    reason: "",
    duration: "1h",
  })

  const [newRateLimit, setNewRateLimit] = useState({
    name: "",
    requests: 100,
    window: "1m",
    action: "block" as 'block' | 'challenge',
  })

  const toggleRule = (id: string) => {
    setRules(prev => prev.map(r =>
      r.id === id ? { ...r, enabled: !r.enabled } : r
    ))
    const rule = rules.find(r => r.id === id)
    toast.success(`${rule?.name} ${rule?.enabled ? 'disabled' : 'enabled'}`)
  }

  const toggleWAF = () => {
    setWafEnabled(!wafEnabled)
    toast.success(`WAF ${wafEnabled ? 'disabled' : 'enabled'}`)
  }

  // Sync with API status
  if (wafStatus && wafStatus.enabled !== wafEnabled) {
    setWafEnabled(wafStatus.enabled)
    if (wafStatus.mode) setWafMode(wafStatus.mode as 'enforce' | 'monitor')
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">Protect your applications from attacks</p>
        </div>
        <LoadingCard />
      </div>
    )
  }

  if (error || !wafStatus?.enabled) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">Protect your applications from attacks</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-muted-foreground">
              {error ? `Failed to load WAF status: ${error.message}` : "WAF is not enabled. Enable it in your configuration file."}
            </p>
            <Button variant="outline" size="sm" className="mt-2" onClick={() => refetch()}>
              <RefreshCw className="mr-2 h-4 w-4" /> Retry
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  const handleAddRule = () => {
    const rule: WAFRule = {
      id: Math.random().toString(36).substr(2, 9),
      name: newRule.name,
      pattern: newRule.pattern,
      action: newRule.action,
      enabled: true,
      hits: 0,
      severity: newRule.severity,
      description: newRule.description,
    }
    setRules([...rules, rule])
    setRuleDialogOpen(false)
    setNewRule({ name: "", pattern: "", action: "block", severity: "medium", description: "" })
    toast.success(`Rule "${rule.name}" created successfully`)
  }

  const handleDeleteRule = (id: string) => {
    setRules(rules.filter(r => r.id !== id))
    toast.success("Rule deleted successfully")
  }

  const handleAddBlock = () => {
    const block: IPBlock = {
      id: Math.random().toString(36).substr(2, 9),
      ip: newBlock.ip,
      reason: newBlock.reason,
      expires: newBlock.duration === "1h" ? "1 hour" : newBlock.duration === "24h" ? "24 hours" : "7 days",
      added: "Just now",
    }
    setBlockedIPs([...blockedIPs, block])
    setIpDialogOpen(false)
    setNewBlock({ ip: "", reason: "", duration: "1h" })
    toast.success(`IP "${block.ip}" blocked successfully`)
  }

  const handleRemoveBlock = (id: string) => {
    setBlockedIPs(blockedIPs.filter(b => b.id !== id))
    toast.success("IP unblocked successfully")
  }

  const handleAddRateLimit = () => {
    const rl: RateLimitRule = {
      id: Math.random().toString(36).substr(2, 9),
      name: newRateLimit.name,
      requests: newRateLimit.requests,
      window: newRateLimit.window,
      action: newRateLimit.action,
      enabled: true,
    }
    setRateLimits([...rateLimits, rl])
    setRateLimitDialogOpen(false)
    setNewRateLimit({ name: "", requests: 100, window: "1m", action: "block" })
    toast.success(`Rate limit "${rl.name}" created successfully`)
  }

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'critical': return 'bg-red-500'
      case 'high': return 'bg-orange-500'
      case 'medium': return 'bg-yellow-500'
      case 'low': return 'bg-blue-500'
      default: return 'bg-gray-500'
    }
  }

  const getActionBadge = (action: string) => {
    switch (action) {
      case 'block': return <Badge variant="destructive">Block</Badge>
      case 'challenge': return <Badge>Challenge</Badge>
      case 'log': return <Badge variant="secondary">Log</Badge>
      default: return <Badge variant="outline">{action}</Badge>
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">Protect your applications from attacks</p>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Mode:</span>
            <Button
              variant={wafMode === 'enforce' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setWafMode('enforce')}
            >
              Enforce
            </Button>
            <Button
              variant={wafMode === 'monitor' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setWafMode('monitor')}
            >
              Monitor
            </Button>
          </div>
          <Switch checked={wafEnabled} onCheckedChange={toggleWAF} />
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Globe className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">1.2M</div>
            <p className="text-xs text-muted-foreground">Last 24 hours</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Threats Blocked</CardTitle>
            <Ban className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600">8,542</div>
            <p className="text-xs text-muted-foreground">Last 24 hours</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Bot Challenges</CardTitle>
            <Bot className="h-4 w-4 text-amber-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-amber-600">3,291</div>
            <p className="text-xs text-muted-foreground">Last 24 hours</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Blocked IPs</CardTitle>
            <Lock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{blockedIPs.length}</div>
            <p className="text-xs text-muted-foreground">Currently blocked</p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="rules" className="space-y-4">
        <TabsList>
          <TabsTrigger value="rules">Rules</TabsTrigger>
          <TabsTrigger value="blocked">Blocked IPs</TabsTrigger>
          <TabsTrigger value="ratelimit">Rate Limiting</TabsTrigger>
          <TabsTrigger value="detection">Detection</TabsTrigger>
        </TabsList>

        <TabsContent value="rules" className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-medium">Security Rules</h3>
            <Dialog open={ruleDialogOpen} onOpenChange={setRuleDialogOpen}>
              <DialogTrigger asChild>
                <Button size="sm">
                  <Plus className="mr-2 h-4 w-4" />
                  Add Rule
                </Button>
              </DialogTrigger>
              <DialogContent className="sm:max-w-[550px]">
                <DialogHeader>
                  <DialogTitle>Add WAF Rule</DialogTitle>
                  <DialogDescription>
                    Create a new security rule to detect and block attacks.
                  </DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-4">
                  <div className="grid gap-2">
                    <Label htmlFor="rule-name">Rule Name</Label>
                    <Input
                      id="rule-name"
                      placeholder="e.g., SQL Injection Detection"
                      value={newRule.name}
                      onChange={(e) => setNewRule({ ...newRule, name: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="rule-description">Description</Label>
                    <Input
                      id="rule-description"
                      placeholder="Brief description of what this rule detects"
                      value={newRule.description}
                      onChange={(e) => setNewRule({ ...newRule, description: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="rule-pattern">Pattern (Regex)</Label>
                    <Textarea
                      id="rule-pattern"
                      placeholder="(?i)(union|select|insert|...)"
                      value={newRule.pattern}
                      onChange={(e) => setNewRule({ ...newRule, pattern: e.target.value })}
                    />
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="grid gap-2">
                      <Label htmlFor="rule-action">Action</Label>
                      <Select
                        value={newRule.action}
                        onValueChange={(value: 'block' | 'challenge' | 'log') => setNewRule({ ...newRule, action: value })}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="block">Block</SelectItem>
                          <SelectItem value="challenge">Challenge</SelectItem>
                          <SelectItem value="log">Log Only</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="grid gap-2">
                      <Label htmlFor="rule-severity">Severity</Label>
                      <Select
                        value={newRule.severity}
                        onValueChange={(value: 'low' | 'medium' | 'high' | 'critical') => setNewRule({ ...newRule, severity: value })}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="low">Low</SelectItem>
                          <SelectItem value="medium">Medium</SelectItem>
                          <SelectItem value="high">High</SelectItem>
                          <SelectItem value="critical">Critical</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  </div>
                </div>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setRuleDialogOpen(false)}>
                    Cancel
                  </Button>
                  <Button onClick={handleAddRule} disabled={!newRule.name || !newRule.pattern}>
                    Add Rule
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>

          <div className="space-y-3">
            {rules.map((rule) => (
              <Card key={rule.id} className={cn("transition-colors", rule.enabled ? 'border-primary/50' : 'opacity-60')}>
                <CardContent className="p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-4">
                      <div className={`w-2 h-8 rounded-full ${getSeverityColor(rule.severity)}`} />
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{rule.name}</span>
                          <Badge variant="outline" className="text-xs capitalize">{rule.severity}</Badge>
                        </div>
                        <div className="text-sm text-muted-foreground">{rule.description}</div>
                        <div className="text-xs font-mono text-muted-foreground mt-1">{rule.pattern}</div>
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      {getActionBadge(rule.action)}
                      <div className="text-sm text-muted-foreground w-20 text-right">
                        {rule.hits.toLocaleString()} hits
                      </div>
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={() => toggleRule(rule.id)}
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-destructive"
                        onClick={() => handleDeleteRule(rule.id)}
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

        <TabsContent value="blocked" className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-medium">Blocked IP Addresses</h3>
            <Dialog open={ipDialogOpen} onOpenChange={setIpDialogOpen}>
              <DialogTrigger asChild>
                <Button size="sm">
                  <Ban className="mr-2 h-4 w-4" />
                  Block IP
                </Button>
              </DialogTrigger>
              <DialogContent className="sm:max-w-[400px]">
                <DialogHeader>
                  <DialogTitle>Block IP Address</DialogTitle>
                  <DialogDescription>
                    Manually block an IP address.
                  </DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-4">
                  <div className="grid gap-2">
                    <Label htmlFor="block-ip">IP Address</Label>
                    <Input
                      id="block-ip"
                      placeholder="192.168.1.100"
                      value={newBlock.ip}
                      onChange={(e) => setNewBlock({ ...newBlock, ip: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="block-reason">Reason</Label>
                    <Input
                      id="block-reason"
                      placeholder="Suspicious activity"
                      value={newBlock.reason}
                      onChange={(e) => setNewBlock({ ...newBlock, reason: e.target.value })}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="block-duration">Duration</Label>
                    <Select
                      value={newBlock.duration}
                      onValueChange={(value: string) => setNewBlock({ ...newBlock, duration: value })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="1h">1 hour</SelectItem>
                        <SelectItem value="24h">24 hours</SelectItem>
                        <SelectItem value="7d">7 days</SelectItem>
                        <SelectItem value="permanent">Permanent</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setIpDialogOpen(false)}>
                    Cancel
                  </Button>
                  <Button onClick={handleAddBlock} disabled={!newBlock.ip}>
                    Block IP
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>

          <div className="space-y-3">
            {blockedIPs.map((block) => (
              <Card key={block.id}>
                <CardContent className="p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-4">
                      <Ban className="h-5 w-5 text-destructive" />
                      <div>
                        <div className="font-medium font-mono">{block.ip}</div>
                        <div className="text-sm text-muted-foreground">{block.reason}</div>
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      <div className="text-right text-sm">
                        <div className="text-muted-foreground">Expires: {block.expires}</div>
                        <div className="text-muted-foreground">Added: {block.added}</div>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-destructive"
                        onClick={() => handleRemoveBlock(block.id)}
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

        <TabsContent value="ratelimit" className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-medium">Rate Limiting Rules</h3>
            <Dialog open={rateLimitDialogOpen} onOpenChange={setRateLimitDialogOpen}>
              <DialogTrigger asChild>
                <Button size="sm">
                  <Plus className="mr-2 h-4 w-4" />
                  Add Rate Limit
                </Button>
              </DialogTrigger>
              <DialogContent className="sm:max-w-[450px]">
                <DialogHeader>
                  <DialogTitle>Add Rate Limit</DialogTitle>
                  <DialogDescription>
                    Configure request rate limiting for specific endpoints or globally.
                  </DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-4">
                  <div className="grid gap-2">
                    <Label htmlFor="rl-name">Rule Name</Label>
                    <Input
                      id="rl-name"
                      placeholder="e.g., API Rate Limit"
                      value={newRateLimit.name}
                      onChange={(e) => setNewRateLimit({ ...newRateLimit, name: e.target.value })}
                    />
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="grid gap-2">
                      <Label htmlFor="rl-requests">Requests</Label>
                      <Input
                        id="rl-requests"
                        type="number"
                        value={newRateLimit.requests}
                        onChange={(e) => setNewRateLimit({ ...newRateLimit, requests: parseInt(e.target.value) || 0 })}
                      />
                    </div>
                    <div className="grid gap-2">
                      <Label htmlFor="rl-window">Window</Label>
                      <Select
                        value={newRateLimit.window}
                        onValueChange={(value: string) => setNewRateLimit({ ...newRateLimit, window: value })}
                      >
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
                  <div className="grid gap-2">
                    <Label htmlFor="rl-action">Action</Label>
                    <Select
                      value={newRateLimit.action}
                      onValueChange={(value: 'block' | 'challenge') => setNewRateLimit({ ...newRateLimit, action: value })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="block">Block</SelectItem>
                        <SelectItem value="challenge">Challenge</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setRateLimitDialogOpen(false)}>
                    Cancel
                  </Button>
                  <Button onClick={handleAddRateLimit} disabled={!newRateLimit.name}>
                    Add Rate Limit
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>

          <div className="grid gap-4">
            {rateLimits.map((rl) => (
              <Card key={rl.id}>
                <CardContent className="p-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="font-medium">{rl.name}</div>
                      <div className="text-sm text-muted-foreground">
                        {rl.requests} requests per {rl.window}
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      {getActionBadge(rl.action)}
                      <Switch
                        checked={rl.enabled}
                        onCheckedChange={() => {
                          setRateLimits(rateLimits.map(r => r.id === rl.id ? { ...r, enabled: !r.enabled } : r))
                        }}
                      />
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="detection">
          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <AlertTriangle className="h-5 w-5 text-amber-500" />
                  Top Attack Types
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {[
                    { type: 'SQL Injection', count: 1543, percent: 45 },
                    { type: 'XSS', count: 892, percent: 26 },
                    { type: 'Bot Traffic', count: 567, percent: 17 },
                    { type: 'Path Traversal', count: 445, percent: 12 },
                  ].map((attack) => (
                    <div key={attack.type} className="space-y-1">
                      <div className="flex justify-between text-sm">
                        <span>{attack.type}</span>
                        <span className="text-muted-foreground">{attack.count.toLocaleString()}</span>
                      </div>
                      <div className="h-2 bg-muted rounded-full overflow-hidden">
                        <div
                          className="h-full bg-primary rounded-full"
                          style={{ width: `${attack.percent}%` }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Globe className="h-5 w-5 text-primary" />
                  Top Countries
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {[
                    { country: 'China', count: 2341, percent: 35 },
                    { country: 'United States', count: 1234, percent: 18 },
                    { country: 'Russia', count: 987, percent: 15 },
                    { country: 'Brazil', count: 654, percent: 10 },
                    { country: 'Germany', count: 432, percent: 6 },
                  ].map((country) => (
                    <div key={country.country} className="space-y-1">
                      <div className="flex justify-between text-sm">
                        <span>{country.country}</span>
                        <span className="text-muted-foreground">{country.count.toLocaleString()}</span>
                      </div>
                      <div className="h-2 bg-muted rounded-full overflow-hidden">
                        <div
                          className="h-full bg-primary rounded-full"
                          style={{ width: `${country.percent}%` }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
