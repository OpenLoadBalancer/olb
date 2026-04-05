import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Progress } from '@/components/ui/progress'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { Separator } from '@/components/ui/separator'
import api from '@/lib/api'
import { cn } from '@/lib/utils'
import {
  Server,
  Users,
  Crown,
  Activity,
  Clock,
  Database,
  Network,
  AlertTriangle,
  CheckCircle,
  RefreshCw,
  LogOut,
  Settings,
  Zap,
  HardDrive,
  Cpu,
  MemoryStick
} from 'lucide-react'

interface ClusterNode {
  id: string
  address: string
  status: 'healthy' | 'unhealthy' | 'offline' | 'joining'
  role: 'leader' | 'follower' | 'candidate'
  lastSeen: Date
  latency: number
  raftTerm: number
  raftIndex: number
  metadata: {
    version: string
    uptime: number
    cpu: number
    memory: number
    disk: number
  }
}

interface RaftState {
  term: number
  leader: string
  commitIndex: number
  lastApplied: number
  state: 'leader' | 'follower' | 'candidate'
}

// Mock cluster data
function generateMockCluster(): ClusterNode[] {
  return [
    {
      id: 'node-1',
      address: '10.0.1.10:7946',
      status: 'healthy',
      role: 'leader',
      lastSeen: new Date(),
      latency: 2,
      raftTerm: 42,
      raftIndex: 15234,
      metadata: {
        version: '1.0.0',
        uptime: 86400 * 7,
        cpu: 45,
        memory: 62,
        disk: 78
      }
    },
    {
      id: 'node-2',
      address: '10.0.1.11:7946',
      status: 'healthy',
      role: 'follower',
      lastSeen: new Date(Date.now() - 5000),
      latency: 5,
      raftTerm: 42,
      raftIndex: 15234,
      metadata: {
        version: '1.0.0',
        uptime: 86400 * 5,
        cpu: 32,
        memory: 48,
        disk: 65
      }
    },
    {
      id: 'node-3',
      address: '10.0.1.12:7946',
      status: 'healthy',
      role: 'follower',
      lastSeen: new Date(Date.now() - 3000),
      latency: 4,
      raftTerm: 42,
      raftIndex: 15234,
      metadata: {
        version: '1.0.0',
        uptime: 86400 * 3,
        cpu: 28,
        memory: 41,
        disk: 52
      }
    }
  ]
}

function NodeCard({ node, isLeader }: { node: ClusterNode; isLeader: boolean }) {
  const statusColors = {
    healthy: 'bg-green-500',
    unhealthy: 'bg-amber-500',
    offline: 'bg-destructive',
    joining: 'bg-blue-500'
  }

  const formatUptime = (seconds: number) => {
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    return `${days}d ${hours}h`
  }

  return (
    <Card className={cn('relative overflow-hidden', isLeader && 'border-primary')}>
      <div className={cn('absolute top-0 left-0 w-1 h-full', statusColors[node.status])} />
      {isLeader && (
        <div className="absolute top-2 right-2">
          <Badge variant="default" className="gap-1">
            <Crown className="h-3 w-3" />
            Leader
          </Badge>
        </div>
      )}
      <CardHeader className="pb-2">
        <div className="flex items-center gap-2">
          <Server className="h-5 w-5 text-muted-foreground" />
          <CardTitle className="text-base">{node.id}</CardTitle>
        </div>
        <CardDescription>{node.address}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Status */}
        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">Status</span>
          <Badge variant={node.status === 'healthy' ? 'default' : 'destructive'}>
            {node.status}
          </Badge>
        </div>

        {/* Latency */}
        <div className="space-y-1">
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Latency</span>
            <span>{node.latency}ms</span>
          </div>
          <Progress value={Math.min((node.latency / 20) * 100, 100)} className="h-1" />
        </div>

        {/* Raft Info */}
        <div className="rounded-md bg-muted p-2 text-xs font-mono">
          <div>Term: {node.raftTerm}</div>
          <div>Index: {node.raftIndex}</div>
        </div>

        {/* Resource Usage */}
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-sm">
            <Cpu className="h-4 w-4 text-muted-foreground" />
            <span className="flex-1">CPU</span>
            <span>{node.metadata.cpu}%</span>
          </div>
          <Progress value={node.metadata.cpu} className="h-1" />

          <div className="flex items-center gap-2 text-sm">
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
            <span className="flex-1">Memory</span>
            <span>{node.metadata.memory}%</span>
          </div>
          <Progress value={node.metadata.memory} className="h-1" />

          <div className="flex items-center gap-2 text-sm">
            <HardDrive className="h-4 w-4 text-muted-foreground" />
            <span className="flex-1">Disk</span>
            <span>{node.metadata.disk}%</span>
          </div>
          <Progress value={node.metadata.disk} className="h-1" />
        </div>

        {/* Metadata */}
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>v{node.metadata.version}</span>
          <span>Uptime: {formatUptime(node.metadata.uptime)}</span>
        </div>
      </CardContent>
    </Card>
  )
}

function RaftVisualizer({ nodes, leader }: { nodes: ClusterNode[]; leader: string }) {
  const [animationStep, setAnimationStep] = useState(0)

  useEffect(() => {
    const interval = setInterval(() => {
      setAnimationStep((prev) => (prev + 1) % 4)
    }, 500)
    return () => clearInterval(interval)
  }, [])

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Network className="h-5 w-5" />
          Raft Consensus
        </CardTitle>
        <CardDescription>Visual representation of the Raft consensus algorithm</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="relative h-[300px] flex items-center justify-center">
          {/* Leader in center */}
          <div className="absolute z-10">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <div className={cn(
                    'flex h-20 w-20 items-center justify-center rounded-full border-4 bg-card transition-all',
                    'border-primary shadow-lg shadow-primary/20'
                  )}>
                    <Crown className="h-8 w-8 text-primary" />
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Leader: {leader}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>

          {/* Followers around leader */}
          {nodes.filter(n => n.id !== leader).map((node, index) => {
            const angle = (index / (nodes.length - 1)) * 2 * Math.PI
            const radius = 100
            const x = Math.cos(angle) * radius
            const y = Math.sin(angle) * radius

            return (
              <TooltipProvider key={node.id}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div
                      className="absolute"
                      style={{
                        transform: `translate(${x}px, ${y}px)`
                      }}
                    >
                      <div className={cn(
                        'flex h-14 w-14 items-center justify-center rounded-full border-2 bg-card transition-all',
                        node.status === 'healthy' ? 'border-green-500' : 'border-destructive'
                      )}>
                        <Server className={cn(
                          'h-6 w-6',
                          node.status === 'healthy' ? 'text-green-500' : 'text-destructive'
                        )} />
                      </div>
                      {/* Connection line to leader */}
                      <svg
                        className="absolute top-1/2 left-1/2 pointer-events-none"
                        style={{
                          width: `${radius * 2}px`,
                          height: `${radius * 2}px`,
                          transform: `translate(-50%, -50%) rotate(${angle + Math.PI}rad)`
                        }}
                      >
                        <line
                          x1="50%"
                          y1="50%"
                          x2={`${50 + (radius / 2)}%`}
                          y2="50%"
                          stroke="currentColor"
                          strokeWidth="2"
                          className={cn(
                            'text-primary transition-opacity',
                            animationStep === index ? 'opacity-100' : 'opacity-20'
                          )}
                          strokeDasharray="4 4"
                        >
                          <animate
                            attributeName="stroke-dashoffset"
                            from="8"
                            to="0"
                            dur="1s"
                            repeatCount="indefinite"
                          />
                        </line>
                      </svg>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{node.id}</p>
                    <p className="text-xs text-muted-foreground">{node.address}</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )
          })}
        </div>

        {/* Legend */}
        <div className="mt-4 flex justify-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full border-2 border-primary bg-primary/20" />
            <span>Leader</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full border-2 border-green-500 bg-green-500/20" />
            <span>Healthy Follower</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full border-2 border-destructive bg-destructive/20" />
            <span>Unhealthy</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

export function ClusterManager() {
  const [clusterNodes, setClusterNodes] = useState<ClusterNode[]>([])
  const [isRefreshing, setIsRefreshing] = useState(false)

  // Load mock data
  useEffect(() => {
    setClusterNodes(generateMockCluster())
  }, [])

  const leader = clusterNodes.find(n => n.role === 'leader')
  const healthyNodes = clusterNodes.filter(n => n.status === 'healthy').length
  const quorum = Math.floor(clusterNodes.length / 2) + 1
  const hasQuorum = healthyNodes >= quorum

  const handleRefresh = () => {
    setIsRefreshing(true)
    setTimeout(() => {
      setClusterNodes(generateMockCluster())
      setIsRefreshing(false)
    }, 1000)
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Cluster</h1>
          <p className="text-muted-foreground">
            Manage Raft consensus cluster and node health
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={handleRefresh} disabled={isRefreshing}>
            <RefreshCw className={cn('mr-2 h-4 w-4', isRefreshing && 'animate-spin')} />
            Refresh
          </Button>
          <Button>
            <Zap className="mr-2 h-4 w-4" />
            Join Node
          </Button>
        </div>
      </div>

      {/* Quorum Alert */}
      {!hasQuorum && (
        <div className="rounded-lg border border-destructive bg-destructive/10 p-4 text-destructive">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5" />
            <span className="font-semibold">Quorum Lost</span>
          </div>
          <p className="mt-1 text-sm">
            Only {healthyNodes} of {clusterNodes.length} nodes are healthy.
            Minimum {quorum} nodes required for quorum.
          </p>
        </div>
      )}

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Total Nodes</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{clusterNodes.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Healthy</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{healthyNodes}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Leader</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <div className="text-2xl font-bold">{leader?.id || 'None'}</div>
              {leader && <CheckCircle className="h-5 w-5 text-green-500" />}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Raft Term</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{leader?.raftTerm || '-'}</div>
          </CardContent>
        </Card>
      </div>

      {/* Main Content */}
      <Tabs defaultValue="nodes" className="space-y-6">
        <TabsList>
          <TabsTrigger value="nodes">Nodes</TabsTrigger>
          <TabsTrigger value="raft">Raft Visualizer</TabsTrigger>
          <TabsTrigger value="events">Events</TabsTrigger>
        </TabsList>

        <TabsContent value="nodes" className="space-y-6">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {clusterNodes.map(node => (
              <NodeCard
                key={node.id}
                node={node}
                isLeader={node.id === leader?.id}
              />
            ))}
          </div>
        </TabsContent>

        <TabsContent value="raft">
          <RaftVisualizer
            nodes={clusterNodes}
            leader={leader?.id || ''}
          />
        </TabsContent>

        <TabsContent value="events">
          <Card>
            <CardHeader>
              <CardTitle>Cluster Events</CardTitle>
              <CardDescription>Recent cluster events and state changes</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                <div className="flex items-start gap-3">
                  <div className="mt-0.5 h-2 w-2 rounded-full bg-green-500" />
                  <div>
                    <p className="text-sm font-medium">Node Joined</p>
                    <p className="text-xs text-muted-foreground">node-3 joined the cluster</p>
                    <p className="text-xs text-muted-foreground">2 minutes ago</p>
                  </div>
                </div>
                <Separator />
                <div className="flex items-start gap-3">
                  <div className="mt-0.5 h-2 w-2 rounded-full bg-blue-500" />
                  <div>
                    <p className="text-sm font-medium">Leader Elected</p>
                    <p className="text-xs text-muted-foreground">node-1 elected as leader for term 42</p>
                    <p className="text-xs text-muted-foreground">1 hour ago</p>
                  </div>
                </div>
                <Separator />
                <div className="flex items-start gap-3">
                  <div className="mt-0.5 h-2 w-2 rounded-full bg-green-500" />
                  <div>
                    <p className="text-sm font-medium">Config Reloaded</p>
                    <p className="text-xs text-muted-foreground">Configuration successfully reloaded on all nodes</p>
                    <p className="text-xs text-muted-foreground">3 hours ago</p>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
