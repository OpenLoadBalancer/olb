import { useState, useEffect } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Network,
  Server,
  Activity,
  AlertCircle,
  Plus,
  Trash2,
  RefreshCw,
  Shield,
  Zap,
} from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

interface ClusterNode {
  id: string
  address: string
  role: 'leader' | 'follower' | 'candidate'
  status: 'healthy' | 'unhealthy' | 'suspect'
  lastHeartbeat: string
  logIndex: number
  term: number
}

interface RaftLog {
  index: number
  term: number
  command: string
  timestamp: string
}

const mockNodes: ClusterNode[] = [
  { id: "node-1", address: "10.0.1.10:12000", role: "leader", status: "healthy", lastHeartbeat: "2s ago", logIndex: 15234, term: 5 },
  { id: "node-2", address: "10.0.1.11:12000", role: "follower", status: "healthy", lastHeartbeat: "1s ago", logIndex: 15234, term: 5 },
  { id: "node-3", address: "10.0.1.12:12000", role: "follower", status: "healthy", lastHeartbeat: "3s ago", logIndex: 15234, term: 5 },
]

const mockLogs: RaftLog[] = [
  { index: 15234, term: 5, command: "UPDATE_BACKEND_STATE", timestamp: "2025-04-05T20:15:30Z" },
  { index: 15233, term: 5, command: "CONFIG_RELOAD", timestamp: "2025-04-05T20:14:15Z" },
  { index: 15232, term: 5, command: "POOL_UPDATE", timestamp: "2025-04-05T20:12:45Z" },
  { index: 15231, term: 5, command: "BACKEND_HEALTH_CHECK", timestamp: "2025-04-05T20:11:20Z" },
  { index: 15230, term: 5, command: "TLS_CERT_UPDATE", timestamp: "2025-04-05T20:10:00Z" },
]

export function ClusterPage() {
  const [nodes, setNodes] = useState<ClusterNode[]>(mockNodes)
  const [logs] = useState<RaftLog[]>(mockLogs)
  const [selectedNode, setSelectedNode] = useState<ClusterNode | null>(null)
  const [addNodeDialogOpen, setAddNodeDialogOpen] = useState(false)
  const [removeNodeDialogOpen, setRemoveNodeDialogOpen] = useState(false)
  const [newNodeAddress, setNewNodeAddress] = useState("")
  const [isRefreshing, setIsRefreshing] = useState(false)

  // Simulate real-time updates
  useEffect(() => {
    const interval = setInterval(() => {
      setNodes(prev => prev.map(node => ({
        ...node,
        lastHeartbeat: `${Math.floor(Math.random() * 5) + 1}s ago`,
      })))
    }, 3000)
    return () => clearInterval(interval)
  }, [])

  const handleRefresh = () => {
    setIsRefreshing(true)
    setTimeout(() => {
      setIsRefreshing(false)
      toast.success("Cluster status refreshed")
    }, 1000)
  }

  const handleAddNode = () => {
    if (!newNodeAddress) {
      toast.error("Please enter a node address")
      return
    }
    const newNode: ClusterNode = {
      id: `node-${nodes.length + 1}`,
      address: newNodeAddress,
      role: "follower",
      status: "healthy",
      lastHeartbeat: "just now",
      logIndex: nodes[0]?.logIndex || 0,
      term: nodes[0]?.term || 1,
    }
    setNodes([...nodes, newNode])
    setAddNodeDialogOpen(false)
    setNewNodeAddress("")
    toast.success(`Node ${newNodeAddress} added to cluster`)
  }

  const handleRemoveNode = () => {
    if (!selectedNode) return
    setNodes(nodes.filter(n => n.id !== selectedNode.id))
    setRemoveNodeDialogOpen(false)
    setSelectedNode(null)
    toast.success("Node removed from cluster")
  }

  const getRoleIcon = (role: string) => {
    switch (role) {
      case 'leader': return <Zap className="h-4 w-4 text-amber-500" />
      case 'follower': return <Server className="h-4 w-4 text-blue-500" />
      case 'candidate': return <Activity className="h-4 w-4 text-purple-500 animate-pulse" />
      default: return null
    }
  }

  const getRoleBadge = (role: string) => {
    switch (role) {
      case 'leader':
        return <Badge className="bg-amber-500/10 text-amber-500 border-amber-500/20">LEADER</Badge>
      case 'follower':
        return <Badge className="bg-blue-500/10 text-blue-500 border-blue-500/20">FOLLOWER</Badge>
      case 'candidate':
        return <Badge className="bg-purple-500/10 text-purple-500 border-purple-500/20">CANDIDATE</Badge>
      default:
        return null
    }
  }

  const leader = nodes.find(n => n.role === 'leader')
  const healthyNodes = nodes.filter(n => n.status === 'healthy').length
  const quorumSize = Math.floor(nodes.length / 2) + 1

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Cluster</h1>
          <p className="text-muted-foreground">Raft consensus and cluster membership</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={isRefreshing}>
            <RefreshCw className={cn("mr-2 h-4 w-4", isRefreshing && "animate-spin")} />
            Refresh
          </Button>
          <Button size="sm" onClick={() => setAddNodeDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Add Node
          </Button>
        </div>
      </div>

      {/* Cluster Overview */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Cluster Status</CardTitle>
            <Network className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <div className="h-2 w-2 rounded-full bg-green-500" />
              <span className="text-2xl font-bold">Healthy</span>
            </div>
            <p className="text-xs text-muted-foreground">Quorum maintained</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Nodes</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{nodes.length}</div>
            <p className="text-xs text-muted-foreground">
              <span className="text-green-500">{healthyNodes} healthy</span>, {nodes.length - healthyNodes} unhealthy
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Current Term</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{nodes[0]?.term || 0}</div>
            <p className="text-xs text-muted-foreground">Leader: {leader?.id || "None"}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Log Index</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{nodes[0]?.logIndex.toLocaleString() || 0}</div>
            <p className="text-xs text-muted-foreground">Committed entries</p>
          </CardContent>
        </Card>
      </div>

      {/* Quorum Warning */}
      {healthyNodes < quorumSize && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Quorum Lost</AlertTitle>
          <AlertDescription>
            Only {healthyNodes} of {nodes.length} nodes are healthy. Minimum {quorumSize} nodes required for consensus.
          </AlertDescription>
        </Alert>
      )}

      <Tabs defaultValue="nodes" className="space-y-4">
        <TabsList>
          <TabsTrigger value="nodes">Nodes</TabsTrigger>
          <TabsTrigger value="logs">Raft Logs</TabsTrigger>
          <TabsTrigger value="gossip">SWIM Gossip</TabsTrigger>
        </TabsList>

        <TabsContent value="nodes" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Cluster Nodes</CardTitle>
              <CardDescription>Raft consensus group members</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {nodes.map((node) => (
                  <div
                    key={node.id}
                    className="flex items-center justify-between p-4 rounded-lg border hover:bg-muted/50 transition-colors"
                  >
                    <div className="flex items-center gap-4">
                      <div className="h-10 w-10 rounded-full bg-muted flex items-center justify-center">
                        {getRoleIcon(node.role)}
                      </div>
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{node.id}</span>
                          {getRoleBadge(node.role)}
                          <Badge variant={node.status === 'healthy' ? 'default' : 'destructive'} className="text-xs">
                            {node.status}
                          </Badge>
                        </div>
                        <div className="text-sm text-muted-foreground flex items-center gap-2">
                          <span>{node.address}</span>
                          <span>•</span>
                          <span>Last heartbeat: {node.lastHeartbeat}</span>
                          <span>•</span>
                          <span>Log: {node.logIndex}</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-destructive"
                        onClick={() => {
                          setSelectedNode(node)
                          setRemoveNodeDialogOpen(true)
                        }}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* Replication Status */}
          <Card>
            <CardHeader>
              <CardTitle>Log Replication Status</CardTitle>
              <CardDescription>Raft log consistency across nodes</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {nodes.map((node) => (
                  <div key={node.id} className="space-y-2">
                    <div className="flex items-center justify-between text-sm">
                      <span className="font-medium">{node.id}</span>
                      <span className="text-muted-foreground">{node.logIndex} / {nodes[0]?.logIndex}</span>
                    </div>
                    <div className="h-2 bg-muted rounded-full overflow-hidden">
                      <div
                        className={cn(
                          "h-full rounded-full transition-all",
                          node.logIndex === nodes[0]?.logIndex ? "bg-green-500" : "bg-amber-500"
                        )}
                        style={{ width: `${(node.logIndex / (nodes[0]?.logIndex || 1)) * 100}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Recent Raft Log Entries</CardTitle>
              <CardDescription>Committed entries in the distributed log</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="border rounded-lg overflow-hidden">
                <table className="w-full">
                  <thead className="bg-muted">
                    <tr>
                      <th className="text-left px-4 py-2 w-24">Index</th>
                      <th className="text-left px-4 py-2 w-20">Term</th>
                      <th className="text-left px-4 py-2">Command</th>
                      <th className="text-left px-4 py-2 w-40">Timestamp</th>
                    </tr>
                  </thead>
                  <tbody>
                    {logs.map((log) => (
                      <tr key={log.index} className="border-t">
                        <td className="px-4 py-2 font-mono text-sm">{log.index}</td>
                        <td className="px-4 py-2">
                          <Badge variant="outline" className="text-xs">{log.term}</Badge>
                        </td>
                        <td className="px-4 py-2 font-mono text-sm">{log.command}</td>
                        <td className="px-4 py-2 text-sm text-muted-foreground">
                          {new Date(log.timestamp).toLocaleTimeString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="gossip" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>SWIM Protocol Status</CardTitle>
              <CardDescription>Scalable Weakly-consistent Infection-style Process Group Membership</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 md:grid-cols-3">
                <div className="p-4 border rounded-lg">
                  <div className="text-sm text-muted-foreground">Protocol</div>
                  <div className="text-lg font-medium">SWIM + Lifeguard</div>
                  <div className="text-xs text-muted-foreground">With suspicion mechanism</div>
                </div>
                <div className="p-4 border rounded-lg">
                  <div className="text-sm text-muted-foreground">Probe Interval</div>
                  <div className="text-lg font-medium">1s</div>
                  <div className="text-xs text-muted-foreground">Configurable</div>
                </div>
                <div className="p-4 border rounded-lg">
                  <div className="text-sm text-muted-foreground">Suspect Timeout</div>
                  <div className="text-lg font-medium">5s</div>
                  <div className="text-xs text-muted-foreground">Before declaring failed</div>
                </div>
              </div>

              <div className="border rounded-lg p-4">
                <h4 className="font-medium mb-4">Member Status</h4>
                <div className="space-y-2">
                  {nodes.map((node) => (
                    <div key={node.id} className="flex items-center justify-between p-2 rounded hover:bg-muted/50">
                      <div className="flex items-center gap-3">
                        <div className={cn(
                          "h-2 w-2 rounded-full",
                          node.status === 'healthy' ? "bg-green-500" : "bg-red-500"
                        )} />
                        <span className="font-medium">{node.id}</span>
                        <span className="text-sm text-muted-foreground">{node.address}</span>
                      </div>
                      <Badge variant={node.status === 'healthy' ? 'default' : 'secondary'}>
                        {node.status === 'healthy' ? 'ALIVE' : 'SUSPECT'}
                      </Badge>
                    </div>
                  ))}
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Add Node Dialog */}
      <Dialog open={addNodeDialogOpen} onOpenChange={setAddNodeDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Cluster Node</DialogTitle>
            <DialogDescription>
              Add a new node to the Raft consensus group.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="node-address">Node Address</Label>
              <Input
                id="node-address"
                placeholder="e.g., 10.0.1.13:12000"
                value={newNodeAddress}
                onChange={(e) => setNewNodeAddress(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Enter the IP address and Raft port of the new node.
              </p>
            </div>
            <div className="bg-muted p-3 rounded-lg text-sm">
              <p className="font-medium mb-1">Note:</p>
              <p className="text-muted-foreground">
                The new node must be running and reachable. It will join as a follower.
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddNodeDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleAddNode}>
              <Plus className="mr-2 h-4 w-4" />
              Add Node
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Remove Node Dialog */}
      <Dialog open={removeNodeDialogOpen} onOpenChange={setRemoveNodeDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove Node</DialogTitle>
            <DialogDescription>
              Are you sure you want to remove this node from the cluster?
            </DialogDescription>
          </DialogHeader>
          <div className="py-4">
            {selectedNode && (
              <div className="p-4 bg-muted rounded-lg">
                <div className="font-medium">{selectedNode.id}</div>
                <div className="text-sm text-muted-foreground">{selectedNode.address}</div>
                <div className="text-sm text-muted-foreground">Role: {selectedNode.role}</div>
              </div>
            )}
            {selectedNode?.role === 'leader' && (
              <div className="mt-4 text-sm text-amber-600 flex items-start gap-2">
                <AlertCircle className="h-4 w-4 mt-0.5" />
                <p>
                  This is the current leader. Removing it will trigger a leader election.
                </p>
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoveNodeDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleRemoveNode}>
              <Trash2 className="mr-2 h-4 w-4" />
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
