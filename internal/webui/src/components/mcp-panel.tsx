import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'
import api from '@/lib/api'
import { toast } from 'sonner'
import {
  Bot,
  Play,
  Pause,
  Terminal,
  CheckCircle,
  AlertCircle,
  Settings,
  Code,
  MessageSquare,
  Zap,
  Shield,
  Activity,
  Globe,
  Server,
  Plus,
  Trash2,
  RefreshCw
} from 'lucide-react'

interface MCPTool {
  name: string
  description: string
  enabled: boolean
  parameters: Record<string, unknown>
  lastUsed?: Date
  usageCount: number
}

interface MCPLog {
  timestamp: Date
  level: 'info' | 'warn' | 'error'
  message: string
  tool?: string
}

const availableTools: MCPTool[] = [
  {
    name: 'get_backends',
    description: 'Retrieve list of configured backends',
    enabled: true,
    parameters: { format: 'json' },
    usageCount: 145
  },
  {
    name: 'update_backend',
    description: 'Update backend configuration',
    enabled: true,
    parameters: { id: 'string', weight: 'number' },
    usageCount: 23
  },
  {
    name: 'get_metrics',
    description: 'Get real-time metrics data',
    enabled: true,
    parameters: { timeframe: 'string' },
    usageCount: 89
  },
  {
    name: 'reload_config',
    description: 'Reload load balancer configuration',
    enabled: false,
    parameters: {},
    usageCount: 5
  },
  {
    name: 'get_waf_stats',
    description: 'Retrieve WAF security statistics',
    enabled: true,
    parameters: {},
    usageCount: 67
  },
  {
    name: 'block_ip',
    description: 'Block an IP address',
    enabled: true,
    parameters: { ip: 'string', duration: 'string' },
    usageCount: 12
  }
]

const mockLogs: MCPLog[] = [
  { timestamp: new Date(Date.now() - 5000), level: 'info', message: 'Tool get_backends executed successfully', tool: 'get_backends' },
  { timestamp: new Date(Date.now() - 30000), level: 'info', message: 'Tool get_metrics executed successfully', tool: 'get_metrics' },
  { timestamp: new Date(Date.now() - 60000), level: 'warn', message: 'Rate limit approaching for tool block_ip', tool: 'block_ip' }
]

export function MCPPanel() {
  const [activeTab, setActiveTab] = useState('tools')
  const [selectedTool, setSelectedTool] = useState<MCPTool | null>(null)
  const [testParams, setTestParams] = useState('{}')
  const [serverEnabled, setServerEnabled] = useState(true)
  const [logs, setLogs] = useState<MCPLog[]>(mockLogs)
  const [showTestDialog, setShowTestDialog] = useState(false)

  const toggleTool = (toolName: string) => {
    toast.success(`Tool ${toolName} ${availableTools.find(t => t.name === toolName)?.enabled ? 'disabled' : 'enabled'}`)
  }

  const executeTest = async () => {
    try {
      const params = JSON.parse(testParams)
      // Simulate API call
      await new Promise(resolve => setTimeout(resolve, 1000))
      toast.success('Tool executed successfully')
      setLogs(prev => [{
        timestamp: new Date(),
        level: 'info',
        message: `Tool ${selectedTool?.name} executed with params: ${JSON.stringify(params)}`,
        tool: selectedTool?.name
      }, ...prev])
    } catch {
      toast.error('Invalid JSON parameters')
    }
  }

  const totalUsage = availableTools.reduce((acc, t) => acc + t.usageCount, 0)
  const enabledTools = availableTools.filter(t => t.enabled).length

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">MCP Server</h1>
          <p className="text-muted-foreground">
            Model Context Protocol integration for AI assistants
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-2 mr-4">
            <Switch
              id="mcp-enabled"
              checked={serverEnabled}
              onCheckedChange={setServerEnabled}
            />
            <Label htmlFor="mcp-enabled">{serverEnabled ? 'Running' : 'Stopped'}</Label>
          </div>
          <Badge variant={serverEnabled ? 'default' : 'secondary'} className="gap-1">
            {serverEnabled ? <Activity className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
            {serverEnabled ? 'Active' : 'Inactive'}
          </Badge>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Total Tools</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{availableTools.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Enabled</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{enabledTools}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Total Calls</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalUsage}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Endpoint</CardTitle>
          </CardHeader>
          <CardContent>
            <code className="text-sm bg-muted px-2 py-1 rounded">/mcp/v1</code>
          </CardContent>
        </Card>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="tools">
            <Bot className="mr-2 h-4 w-4" />
            Tools
          </TabsTrigger>
          <TabsTrigger value="logs">
            <Terminal className="mr-2 h-4 w-4" />
            Logs
          </TabsTrigger>
          <TabsTrigger value="config">
            <Settings className="mr-2 h-4 w-4" />
            Configuration
          </TabsTrigger>
          <TabsTrigger value="docs">
            <Code className="mr-2 h-4 w-4" />
            Documentation
          </TabsTrigger>
        </TabsList>

        <TabsContent value="tools" className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {availableTools.map((tool) => (
              <Card key={tool.name} className={cn(!tool.enabled && 'opacity-60')}>
                <CardHeader className="pb-3">
                  <div className="flex items-start justify-between">
                    <div className="flex items-center gap-2">
                      <Zap className="h-5 w-5 text-primary" />
                      <CardTitle className="text-base">{tool.name}</CardTitle>
                    </div>
                    <Switch
                      checked={tool.enabled}
                      onCheckedChange={() => toggleTool(tool.name)}
                    />
                  </div>
                  <CardDescription>{tool.description}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Activity className="h-4 w-4" />
                    <span>{tool.usageCount} calls</span>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1"
                      onClick={() => {
                        setSelectedTool(tool)
                        setShowTestDialog(true)
                      }}
                    >
                      <Play className="mr-2 h-4 w-4" />
                      Test
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>MCP Server Logs</CardTitle>
              <CardDescription>Recent tool executions and events</CardDescription>
            </CardHeader>
            <CardContent>
              <ScrollArea className="h-[400px]">
                <div className="space-y-2">
                  {logs.map((log, index) => (
                    <div
                      key={index}
                      className="flex items-start gap-3 rounded-lg border p-3"
                    >
                      {log.level === 'info' && <CheckCircle className="h-4 w-4 text-green-500" />}
                      {log.level === 'warn' && <AlertCircle className="h-4 w-4 text-amber-500" />}
                      {log.level === 'error' && <AlertCircle className="h-4 w-4 text-destructive" />}
                      <div className="flex-1">
                        <p className="text-sm">{log.message}</p>
                        <div className="flex items-center gap-2 mt-1">
                          {log.tool && (
                            <Badge variant="outline" className="text-xs">{log.tool}</Badge>
                          )}
                          <span className="text-xs text-muted-foreground">
                            {log.timestamp.toLocaleTimeString()}
                          </span>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="config" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Server Configuration</CardTitle>
              <CardDescription>MCP server settings</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label>API Endpoint</Label>
                <Input value="/mcp/v1" readOnly />
              </div>
              <div className="space-y-2">
                <Label>Authentication</Label>
                <Select defaultValue="token">
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">None</SelectItem>
                    <SelectItem value="token">API Token</SelectItem>
                    <SelectItem value="oauth">OAuth 2.0</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex items-center justify-between rounded-lg border p-4">
                <div>
                  <p className="font-medium">Rate Limiting</p>
                  <p className="text-sm text-muted-foreground">Limit requests per minute</p>
                </div>
                <Switch defaultChecked />
              </div>
              <div className="flex items-center justify-between rounded-lg border p-4">
                <div>
                  <p className="font-medium">Request Logging</p>
                  <p className="text-sm text-muted-foreground">Log all MCP requests</p>
                </div>
                <Switch defaultChecked />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="docs" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Integration Guide</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="rounded-lg bg-muted p-4">
                <p className="font-medium mb-2">Connect Claude to OpenLoadBalancer</p>
                <code className="block text-sm bg-background p-2 rounded">
                  {`{
  "mcpServers": {
    "openloadbalancer": {
      "command": "http://localhost:9090/mcp/v1",
      "args": ["--api-key", "your-api-key"]
    }
  }
}`}
                </code>
              </div>
              <div className="space-y-2">
                <h4 className="font-medium">Available Tools</h4>
                <ul className="list-disc list-inside text-sm text-muted-foreground space-y-1">
                  <li>Query backend status and health</li>
                  <li>Update backend weights dynamically</li>
                  <li>Retrieve real-time metrics</li>
                  <li>Block/unblock IP addresses</li>
                  <li>Reload configuration</li>
                </ul>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Test Dialog */}
      <Dialog open={showTestDialog} onOpenChange={setShowTestDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Test Tool: {selectedTool?.name}</DialogTitle>
            <DialogDescription>{selectedTool?.description}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>Parameters (JSON)</Label>
              <Textarea
                value={testParams}
                onChange={(e) => setTestParams(e.target.value)}
                className="min-h-[150px] font-mono text-sm"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowTestDialog(false)}>
              Cancel
            </Button>
            <Button onClick={executeTest}>
              <Play className="mr-2 h-4 w-4" />
              Execute
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
