import { useState, useEffect } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { toast } from 'sonner'
import { cn, formatBytes } from '@/lib/utils'
import {
  Activity,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  RefreshCw,
  Server,
  Network,
  Database,
  Shield,
  Cpu,
  MemoryStick,
  HardDrive,
  Wifi,
  Globe,
  Clock,
  Play,
  Pause,
  Download,
  ChevronRight,
  ChevronDown,
  Settings,
  Zap,
  Bug
} from 'lucide-react'

interface DiagnosticTest {
  id: string
  name: string
  description: string
  category: 'network' | 'system' | 'config' | 'security'
  status: 'idle' | 'running' | 'passed' | 'failed' | 'warning'
  duration?: number
  message?: string
  details?: string[]
}

const defaultTests: DiagnosticTest[] = [
  {
    id: 'network-connectivity',
    name: 'Network Connectivity',
    description: 'Check network interfaces and connectivity',
    category: 'network',
    status: 'idle'
  },
  {
    id: 'dns-resolution',
    name: 'DNS Resolution',
    description: 'Test DNS resolution for configured domains',
    category: 'network',
    status: 'idle'
  },
  {
    id: 'backend-health',
    name: 'Backend Health',
    description: 'Check health of all configured backends',
    category: 'system',
    status: 'idle'
  },
  {
    id: 'certificate-expiry',
    name: 'Certificate Expiry',
    description: 'Check SSL certificate expiration dates',
    category: 'security',
    status: 'idle'
  },
  {
    id: 'config-syntax',
    name: 'Configuration Syntax',
    description: 'Validate configuration file syntax',
    category: 'config',
    status: 'idle'
  },
  {
    id: 'disk-space',
    name: 'Disk Space',
    description: 'Check available disk space',
    category: 'system',
    status: 'idle'
  },
  {
    id: 'memory-usage',
    name: 'Memory Usage',
    description: 'Check memory utilization',
    category: 'system',
    status: 'idle'
  },
  {
    id: 'port-conflicts',
    name: 'Port Conflicts',
    description: 'Check for port binding conflicts',
    category: 'network',
    status: 'idle'
  }
]

interface SystemMetric {
  name: string
  value: number
  max: number
  unit: string
  status: 'good' | 'warning' | 'critical'
}

export function DiagnosticsPanel() {
  const [tests, setTests] = useState<DiagnosticTest[]>(defaultTests)
  const [isRunningAll, setIsRunningAll] = useState(false)
  const [activeTab, setActiveTab] = useState('all')
  const [progress, setProgress] = useState(0)
  const [metrics, setMetrics] = useState<SystemMetric[]>([
    { name: 'CPU Usage', value: 45, max: 100, unit: '%', status: 'good' },
    { name: 'Memory', value: 6.2, max: 16, unit: 'GB', status: 'good' },
    { name: 'Disk', value: 78, max: 100, unit: '%', status: 'warning' },
    { name: 'Network', value: 125, max: 1000, unit: 'Mbps', status: 'good' }
  ])

  const runTest = async (testId: string) => {
    setTests(prev =>
      prev.map(t =>
        t.id === testId ? { ...t, status: 'running' } : t
      )
    )

    await new Promise(resolve => setTimeout(resolve, 1500))

    const results: Record<string, { status: 'passed' | 'failed' | 'warning'; message: string; details?: string[] }> = {
      'network-connectivity': { status: 'passed', message: 'All network interfaces operational', details: ['eth0: UP (1Gbps)', 'eth1: UP (1Gbps)', 'lo: UP'] },
      'dns-resolution': { status: 'passed', message: 'DNS resolution working', details: ['api.openloadbalancer.dev: OK', 'admin.openloadbalancer.dev: OK'] },
      'backend-health': { status: 'warning', message: '2 of 5 backends unhealthy', details: ['backend-01: Healthy', 'backend-02: Healthy', 'backend-03: Down', 'backend-04: Healthy', 'backend-05: Timeout'] },
      'certificate-expiry': { status: 'passed', message: 'All certificates valid', details: ['api.openloadbalancer.dev: 245 days remaining', 'admin.openloadbalancer.dev: 180 days remaining'] },
      'config-syntax': { status: 'passed', message: 'Configuration valid' },
      'disk-space': { status: 'warning', message: 'Disk usage above 75%', details: ['/var: 82% used', '/tmp: 45% used'] },
      'memory-usage': { status: 'passed', message: 'Memory usage normal' },
      'port-conflicts': { status: 'passed', message: 'No port conflicts detected' }
    }

    const result = results[testId] || { status: 'passed', message: 'Test completed' }

    setTests(prev =>
      prev.map(t =>
        t.id === testId
          ? { ...t, status: result.status, message: result.message, details: result.details, duration: 1500 }
          : t
      )
    )
  }

  const runAllTests = async () => {
    setIsRunningAll(true)
    setProgress(0)

    const testsToRun = activeTab === 'all'
      ? tests
      : tests.filter(t => t.category === activeTab)

    for (let i = 0; i < testsToRun.length; i++) {
      await runTest(testsToRun[i].id)
      setProgress(((i + 1) / testsToRun.length) * 100)
    }

    setIsRunningAll(false)
    toast.success('Diagnostics complete')
  }

  const filteredTests = activeTab === 'all'
    ? tests
    : tests.filter(t => t.category === activeTab)

  const stats = {
    passed: tests.filter(t => t.status === 'passed').length,
    failed: tests.filter(t => t.status === 'failed').length,
    warnings: tests.filter(t => t.status === 'warning').length,
    pending: tests.filter(t => t.status === 'idle' || t.status === 'running').length
  }

  const getStatusIcon = (status: DiagnosticTest['status']) => {
    switch (status) {
      case 'passed':
        return <CheckCircle2 className="h-5 w-5 text-green-500" />
      case 'failed':
        return <XCircle className="h-5 w-5 text-red-500" />
      case 'warning':
        return <AlertTriangle className="h-5 w-5 text-yellow-500" />
      case 'running':
        return <RefreshCw className="h-5 w-5 animate-spin text-blue-500" />
      default:
        return <div className="h-5 w-5 rounded-full border-2 border-muted" />
    }
  }

  const getStatusColor = (status: DiagnosticTest['status']) => {
    switch (status) {
      case 'passed':
        return 'border-green-500/20 bg-green-500/5'
      case 'failed':
        return 'border-red-500/20 bg-red-500/5'
      case 'warning':
        return 'border-yellow-500/20 bg-yellow-500/5'
      case 'running':
        return 'border-blue-500/20 bg-blue-500/5'
      default:
        return ''
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Diagnostics</h1>
          <p className="text-muted-foreground">
            System health checks and troubleshooting
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => setTests(defaultTests)}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Reset
          </Button>
          <Button onClick={runAllTests} disabled={isRunningAll}>
            {isRunningAll ? (
              <>
                <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                Running...
              </>
            ) : (
              <>
                <Play className="mr-2 h-4 w-4" />
                Run All Tests
              </>
            )}
          </Button>
        </div>
      </div>

      {/* System Metrics */}
      <div className="grid gap-4 md:grid-cols-4">
        {metrics.map(metric => (
          <Card key={metric.name}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{metric.name}</CardTitle>
              {metric.name === 'CPU Usage' && <Cpu className="h-4 w-4 text-muted-foreground" />}
              {metric.name === 'Memory' && <MemoryStick className="h-4 w-4 text-muted-foreground" />}
              {metric.name === 'Disk' && <HardDrive className="h-4 w-4 text-muted-foreground" />}
              {metric.name === 'Network' && <Wifi className="h-4 w-4 text-muted-foreground" />}
            </CardHeader>
            <CardContent>
              <div className="flex items-baseline gap-1">
                <span className={cn(
                  'text-2xl font-bold',
                  metric.status === 'critical' && 'text-red-500',
                  metric.status === 'warning' && 'text-yellow-500'
                )}>
                  {metric.value}
                </span>
                <span className="text-sm text-muted-foreground">{metric.unit}</span>
              </div>
              <div className="mt-2 h-2 rounded-full bg-muted">
                <div
                  className={cn(
                    'h-full rounded-full transition-all',
                    metric.status === 'good' && 'bg-green-500',
                    metric.status === 'warning' && 'bg-yellow-500',
                    metric.status === 'critical' && 'bg-red-500'
                  )}
                  style={{ width: `${(metric.value / metric.max) * 100}%` }}
                />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Passed</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{stats.passed}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Warnings</CardTitle>
            <AlertTriangle className="h-4 w-4 text-yellow-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-yellow-500">{stats.warnings}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Failed</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.failed}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Pending</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.pending}</div>
          </CardContent>
        </Card>
      </div>

      {isRunningAll && (
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between mb-2">
              <span className="font-medium">Running diagnostics...</span>
              <span className="text-sm text-muted-foreground">{Math.round(progress)}%</span>
            </div>
            <Progress value={progress} />
          </CardContent>
        </Card>
      )}

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="all">All Tests</TabsTrigger>
          <TabsTrigger value="network">Network</TabsTrigger>
          <TabsTrigger value="system">System</TabsTrigger>
          <TabsTrigger value="config">Config</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
        </TabsList>

        <TabsContent value={activeTab} className="space-y-4">
          <div className="grid gap-4">
            {filteredTests.map(test => (
              <Card
                key={test.id}
                className={cn(
                  'transition-colors',
                  getStatusColor(test.status)
                )}
              >
                <CardContent className="p-4">
                  <div className="flex items-start gap-4">
                    <div className="mt-0.5">
                      {getStatusIcon(test.status)}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center justify-between">
                        <div>
                          <h3 className="font-medium">{test.name}</h3>
                          <p className="text-sm text-muted-foreground">{test.description}</p>
                        </div>
                        <div className="flex items-center gap-2">
                          <Badge variant="outline" className="capitalize">
                            {test.category}
                          </Badge>
                          {test.status === 'idle' && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => runTest(test.id)}
                            >
                              <Play className="h-4 w-4" />
                            </Button>
                          )}
                        </div>
                      </div>
                      {test.message && (
                        <p className={cn(
                          'mt-2 text-sm',
                          test.status === 'passed' && 'text-green-600',
                          test.status === 'failed' && 'text-red-600',
                          test.status === 'warning' && 'text-yellow-600'
                        )}>
                          {test.message}
                        </p>
                      )}
                      {test.details && (
                        <div className="mt-2 space-y-1">
                          {test.details.map((detail, i) => (
                            <div key={i} className="flex items-center gap-2 text-sm text-muted-foreground">
                              <ChevronRight className="h-3 w-3" />
                              {detail}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>
      </Tabs>

      {/* Quick Actions */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Zap className="h-5 w-5" />
            Quick Actions
          </CardTitle>
          <CardDescription>Common troubleshooting actions</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-4">
            <Button variant="outline" className="justify-start">
              <RefreshCw className="mr-2 h-4 w-4" />
              Reload Config
            </Button>
            <Button variant="outline" className="justify-start">
              <Network className="mr-2 h-4 w-4" />
              Flush DNS
            </Button>
            <Button variant="outline" className="justify-start">
              <Server className="mr-2 h-4 w-4" />
              Restart Health Checks
            </Button>
            <Button variant="outline" className="justify-start">
              <Download className="mr-2 h-4 w-4" />
              Export Debug Info
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
