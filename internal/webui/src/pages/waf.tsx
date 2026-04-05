import { useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Shield, AlertTriangle, Ban, Globe, Bot, Lock, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

interface WAFRule {
  id: string
  name: string
  pattern: string
  action: 'block' | 'log' | 'challenge'
  enabled: boolean
  hits: number
}

interface IPBlock {
  id: string
  ip: string
  reason: string
  expires: string
  added: string
}

export function WAFPage() {
  const [wafEnabled, setWafEnabled] = useState(true)
  const [wafMode, setWafMode] = useState<'enforce' | 'monitor'>('enforce')
  const [rules, setRules] = useState<WAFRule[]>([
    { id: "1", name: "SQL Injection", pattern: "(?i)(union|select|insert|update|delete|drop|create|alter)", action: "block", enabled: true, hits: 1523 },
    { id: "2", name: "XSS Attack", pattern: "(?i)(<script|javascript:|onerror=|onload=)", action: "block", enabled: true, hits: 892 },
    { id: "3", name: "Path Traversal", pattern: "\.\./|\\.\\./", action: "block", enabled: true, hits: 445 },
    { id: "4", name: "Bot Detection", pattern: "(?i)(bot|crawler|spider|scrape)", action: "challenge", enabled: true, hits: 5678 },
  ])
  const [blockedIPs] = useState<IPBlock[]>([
    { id: "1", ip: "192.168.1.100", reason: "Rate limit exceeded", expires: "1 hour", added: "2 hours ago" },
    { id: "2", ip: "10.0.0.50", reason: "WAF rule triggered", expires: "24 hours", added: "5 hours ago" },
  ])

  const toggleRule = (id: string) => {
    setRules(prev => prev.map(r =>
      r.id === id ? { ...r, enabled: !r.enabled } : r
    ))
  }

  const toggleWAF = () => {
    setWafEnabled(!wafEnabled)
    toast.success(`WAF ${wafEnabled ? 'disabled' : 'enabled'}`)
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
          <TabsTrigger value="detection">Detection</TabsTrigger>
        </TabsList>

        <TabsContent value="rules" className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-medium">Security Rules</h3>
            <Button size="sm" onClick={() => toast.info("Add rule dialog would open")}>
              <Plus className="mr-2 h-4 w-4" />
              Add Rule
            </Button>
          </div>

          <div className="space-y-3">
            {rules.map((rule) => (
              <Card key={rule.id} className={rule.enabled ? 'border-primary/50' : ''}>
                <CardContent className="p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-4">
                      <Shield className={`h-5 w-5 ${rule.enabled ? 'text-primary' : 'text-muted-foreground'}`} />
                      <div>
                        <div className="font-medium">{rule.name}</div>
                        <div className="text-sm text-muted-foreground font-mono">{rule.pattern}</div>
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      <Badge variant={rule.action === 'block' ? 'destructive' : rule.action === 'challenge' ? 'default' : 'secondary'}>
                        {rule.action}
                      </Badge>
                      <div className="text-sm text-muted-foreground w-20 text-right">
                        {rule.hits.toLocaleString()} hits
                      </div>
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={() => toggleRule(rule.id)}
                      />
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
            <div className="flex gap-2">
              <Input placeholder="Search IP..." className="w-48" />
              <Button size="sm" onClick={() => toast.info("Block IP dialog would open")}>
                <Ban className="mr-2 h-4 w-4" />
                Block IP
              </Button>
            </div>
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
                      <Button variant="ghost" size="icon" className="text-destructive">
                        <Trash2 className="h-4 w-4" />
                      </Button>
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
