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
  Shield,
  AlertTriangle,
  Ban,
  Activity,
  Bot,
  Eye,
  EyeOff,
  RefreshCw,
  Save,
  Settings2,
  Siren
} from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import { WAFRulesBuilder } from '@/components/waf-rules-builder'

interface WAFStats {
  total_requests: number
  blocked_requests: number
  flagged_requests: number
  rules_triggered: number
}

export function WAFPage() {
  const queryClient = useQueryClient()
  const [mode, setMode] = useState<'enforce' | 'monitor' | 'disabled'>('monitor')

  const { data: stats = { total_requests: 0, blocked_requests: 0, flagged_requests: 0, rules_triggered: 0 } } = useQuery<WAFStats>({
    queryKey: ['waf-stats'],
    queryFn: async () => {
      const response = await api.get('/api/v1/waf/stats')
      return response.data
    },
    refetchInterval: 10000
  })

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Web Application Firewall</h1>
          <p className="text-muted-foreground">
            Protect your applications from common web vulnerabilities
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge
            variant={mode === 'enforce' ? 'destructive' : mode === 'monitor' ? 'warning' : 'secondary'}
            className="text-sm px-3 py-1"
          >
            {mode === 'enforce' ? 'Enforce Mode' : mode === 'monitor' ? 'Monitor Mode' : 'Disabled'}
          </Badge>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.total_requests.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Blocked</CardTitle>
            <Ban className="h-4 w-4 text-destructive" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-destructive">{stats.blocked_requests.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Flagged</CardTitle>
            <AlertTriangle className="h-4 w-4 text-warning" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-amber-500">{stats.flagged_requests.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Rules Triggered</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.rules_triggered.toLocaleString()}</div>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="rules" className="space-y-4">
        <TabsList>
          <TabsTrigger value="rules">Rules Builder</TabsTrigger>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        <TabsContent value="rules">
          <WAFRulesBuilder />
        </TabsContent>

        <TabsContent value="overview" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>WAF Status</CardTitle>
              <CardDescription>Current protection status and modules</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid gap-4 md:grid-cols-3">
                <Card>
                  <CardHeader className="pb-2">
                    <div className="flex items-center gap-2">
                      <Siren className="h-5 w-5 text-green-500" />
                      <CardTitle className="text-base">Detection</CardTitle>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-muted-foreground">6 threat detectors active</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2">
                    <div className="flex items-center gap-2">
                      <Bot className="h-5 w-5 text-blue-500" />
                      <CardTitle className="text-base">Bot Detection</CardTitle>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-muted-foreground">Monitoring active</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2">
                    <div className="flex items-center gap-2">
                      <Settings2 className="h-5 w-5 text-amber-500" />
                      <CardTitle className="text-base">Rate Limiting</CardTitle>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-muted-foreground">3 rules configured</p>
                  </CardContent>
                </Card>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>WAF Mode</CardTitle>
              <CardDescription>Configure how the WAF handles detected threats</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-4">
                <div
                  className={`flex items-center justify-between rounded-lg border p-4 cursor-pointer ${
                    mode === 'enforce' ? 'border-primary bg-primary/5' : ''
                  }`}
                  onClick={() => setMode('enforce')}
                >
                  <div className="flex items-center gap-3">
                    <Ban className="h-5 w-5 text-destructive" />
                    <div>
                      <p className="font-medium">Enforce Mode</p>
                      <p className="text-sm text-muted-foreground">
                        Block all requests that trigger security rules
                      </p>
                    </div>
                  </div>
                  {mode === 'enforce' && <div className="h-2 w-2 rounded-full bg-primary" />}
                </div>

                <div
                  className={`flex items-center justify-between rounded-lg border p-4 cursor-pointer ${
                    mode === 'monitor' ? 'border-primary bg-primary/5' : ''
                  }`}
                  onClick={() => setMode('monitor')}
                >
                  <div className="flex items-center gap-3">
                    <Eye className="h-5 w-5 text-warning" />
                    <div>
                      <p className="font-medium">Monitor Mode</p>
                      <p className="text-sm text-muted-foreground">
                        Log threats but allow all requests through
                      </p>
                    </div>
                  </div>
                  {mode === 'monitor' && <div className="h-2 w-2 rounded-full bg-primary" />}
                </div>

                <div
                  className={`flex items-center justify-between rounded-lg border p-4 cursor-pointer ${
                    mode === 'disabled' ? 'border-primary bg-primary/5' : ''
                  }`}
                  onClick={() => setMode('disabled')}
                >
                  <div className="flex items-center gap-3">
                    <EyeOff className="h-5 w-5 text-muted-foreground" />
                    <div>
                      <p className="font-medium">Disabled</p>
                      <p className="text-sm text-muted-foreground">
                        WAF is completely disabled
                      </p>
                    </div>
                  </div>
                  {mode === 'disabled' && <div className="h-2 w-2 rounded-full bg-primary" />}
                </div>
              </div>

              <div className="flex justify-end gap-2">
                <Button variant="outline">
                  <RefreshCw className="mr-2 h-4 w-4" />
                  Reset
                </Button>
                <Button>
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

