import { useState } from "react"
import { useDocumentTitle } from "@/hooks/use-document-title"
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
import { useClusterStatus, useClusterMembers } from "@/hooks/use-query"
import { LoadingCard } from "@/components/ui/loading"

export function ClusterPage() {
  useDocumentTitle("Cluster")
  const { data: clusterStatus, isLoading: statusLoading, error: statusError, refetch: refetchStatus } = useClusterStatus()
  const { data: members, isLoading: membersLoading, refetch: refetchMembers } = useClusterMembers()

  const [addNodeDialogOpen, setAddNodeDialogOpen] = useState(false)
  const [newNodeAddress, setNewNodeAddress] = useState("")
  const [selectedMember, setSelectedMember] = useState<{ id: string; address: string; state: string } | null>(null)
  const [removeNodeDialogOpen, setRemoveNodeDialogOpen] = useState(false)

  const handleRefresh = () => {
    refetchStatus()
    refetchMembers()
    toast.success("Cluster status refreshed")
  }

  const handleAddNode = () => {
    if (!newNodeAddress) {
      toast.error("Please enter a node address")
      return
    }
    toast.info(`Node addition requested for ${newNodeAddress}`)
    setAddNodeDialogOpen(false)
    setNewNodeAddress("")
  }

  const handleRemoveNode = () => {
    if (!selectedMember) return
    toast.info(`Node removal requested for ${selectedMember.id}`)
    setRemoveNodeDialogOpen(false)
    setSelectedMember(null)
  }

  const getRoleIcon = (isLeader: boolean) => {
    if (isLeader) return <Zap className="h-4 w-4 text-amber-500" />
    return <Server className="h-4 w-4 text-blue-500" />
  }

  const getRoleBadge = (isLeader: boolean) => {
    if (isLeader) return <Badge className="bg-amber-500/10 text-amber-500 border-amber-500/20">LEADER</Badge>
    return <Badge className="bg-blue-500/10 text-blue-500 border-blue-500/20">MEMBER</Badge>
  }

  const isLoading = statusLoading || membersLoading

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Cluster</h1>
          <p className="text-muted-foreground">Raft consensus and cluster membership</p>
        </div>
        <div className="grid gap-4 md:grid-cols-4">
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
          <LoadingCard />
        </div>
      </div>
    )
  }

  if (statusError || !clusterStatus) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Cluster</h1>
          <p className="text-muted-foreground">Raft consensus and cluster membership</p>
        </div>
        <Card>
          <CardContent className="p-6">
            <p className="text-muted-foreground mb-4">
              Cluster is not configured or this node is running in standalone mode.
            </p>
            <p className="text-sm text-muted-foreground">
              Enable clustering in your configuration file to see cluster status here.
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Cluster</h1>
          <p className="text-muted-foreground">Raft consensus and cluster membership</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleRefresh}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Button size="sm" onClick={() => setAddNodeDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Add Node
          </Button>
        </div>
      </div>

      {/* Cluster Overview */}
      <div className="grid gap-4 grid-cols-2 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Cluster Status</CardTitle>
            <Network className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <div className={cn("h-2 w-2 rounded-full", clusterStatus.state === "leader" || clusterStatus.state === "follower" ? "bg-green-500" : "bg-amber-500")} />
              <span className="text-2xl font-bold capitalize">{clusterStatus.state}</span>
            </div>
            <p className="text-xs text-muted-foreground">Node: {clusterStatus.node_id}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Members</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{members?.length ?? 0}</div>
            <p className="text-xs text-muted-foreground truncate">
              {members?.map(m => m.id).join(", ") || "No members"}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Current Term</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{clusterStatus.term}</div>
            <p className="text-xs text-muted-foreground">Leader: {clusterStatus.leader || "None"}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Raft Index</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{clusterStatus.commit_index.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground">Applied: {clusterStatus.applied_index.toLocaleString()}</p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="nodes" className="space-y-4">
        <TabsList className="flex-wrap h-auto">
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
                {(members ?? []).map((member) => {
                  const memberIsLeader = member.id === clusterStatus.leader
                  return (
                    <div
                      key={member.id}
                      className="flex flex-wrap items-center justify-between gap-2 p-4 rounded-lg border hover:bg-muted/50 transition-colors"
                    >
                      <div className="flex items-center gap-4">
                        <div className="h-10 w-10 rounded-full bg-muted flex items-center justify-center">
                          {getRoleIcon(memberIsLeader)}
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="font-medium">{member.id}</span>
                            {getRoleBadge(memberIsLeader)}
                            <Badge variant={member.id === clusterStatus.node_id ? "default" : "outline"} className="text-xs">
                              {member.state}
                            </Badge>
                          </div>
                          <div className="text-sm text-muted-foreground">
                            {member.address}
                          </div>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="ghost" aria-label="Remove node"
                          size="icon"
                          className="text-destructive"
                          onClick={() => {
                            setSelectedMember(member)
                            setRemoveNodeDialogOpen(true)
                          }}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                  )
                })}
                {(!members || members.length === 0) && (
                  <p className="text-sm text-muted-foreground text-center py-4">No cluster members found</p>
                )}
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
                <div className="space-y-2">
                  <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
                    <span className="font-medium">{clusterStatus.node_id}</span>
                    <span className="text-muted-foreground">Applied: {clusterStatus.applied_index} / Committed: {clusterStatus.commit_index}</span>
                  </div>
                  <div className="h-2 bg-muted rounded-full overflow-hidden">
                    <div
                      className={cn(
                        "h-full rounded-full transition-all",
                        clusterStatus.applied_index === clusterStatus.commit_index ? "bg-green-500" : "bg-amber-500"
                      )}
                      style={{ width: `${clusterStatus.commit_index > 0 ? (clusterStatus.applied_index / clusterStatus.commit_index) * 100 : 100}%` }}
                    />
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Raft State</CardTitle>
              <CardDescription>Current Raft consensus state</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Node ID</span>
                    <span className="font-mono">{clusterStatus.node_id}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">State</span>
                    <span className="capitalize">{clusterStatus.state}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Term</span>
                    <span>{clusterStatus.term}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Vote</span>
                    <span className="font-mono">{clusterStatus.vote || "none"}</span>
                  </div>
                </div>
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Leader</span>
                    <span className="font-mono">{clusterStatus.leader || "none"}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Commit Index</span>
                    <span>{clusterStatus.commit_index.toLocaleString()}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Applied Index</span>
                    <span>{clusterStatus.applied_index.toLocaleString()}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Peers</span>
                    <span>{clusterStatus.peers?.length ?? 0}</span>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="gossip" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Cluster Members</CardTitle>
              <CardDescription>SWIM gossip protocol membership</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="border rounded-lg p-4">
                <div className="space-y-2">
                  {(members ?? []).map((member) => (
                    <div key={member.id} className="flex flex-wrap items-center justify-between gap-2 p-2 rounded hover:bg-muted/50">
                      <div className="flex items-center gap-3">
                        <div className={cn(
                          "h-2 w-2 rounded-full",
                          member.state === "alive" ? "bg-green-500" : "bg-amber-500"
                        )} />
                        <span className="font-medium">{member.id}</span>
                        <span className="text-sm text-muted-foreground">{member.address}</span>
                      </div>
                      <Badge variant={member.id === clusterStatus.leader ? "default" : "secondary"}>
                        {member.id === clusterStatus.leader ? "LEADER" : member.state.toUpperCase()}
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
            {selectedMember && (
              <div className="p-4 bg-muted rounded-lg">
                <div className="font-medium">{selectedMember.id}</div>
                <div className="text-sm text-muted-foreground">{selectedMember.address}</div>
                <div className="text-sm text-muted-foreground">State: {selectedMember.state}</div>
              </div>
            )}
            {selectedMember?.id === clusterStatus?.leader && (
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
