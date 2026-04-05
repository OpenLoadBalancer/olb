import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Plus, Layers, Trash2 } from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import type { Pool } from '@/types'

const algorithms = [
  { value: 'round_robin', label: 'Round Robin' },
  { value: 'weighted_round_robin', label: 'Weighted Round Robin' },
  { value: 'least_connections', label: 'Least Connections' },
  { value: 'least_response_time', label: 'Least Response Time' },
  { value: 'ip_hash', label: 'IP Hash' },
  { value: 'consistent_hash', label: 'Consistent Hash' },
  { value: 'maglev', label: 'Maglev' },
  { value: 'power_of_two_choices', label: 'Power of Two Choices' },
  { value: 'random', label: 'Random' },
  { value: 'ring_hash', label: 'Ring Hash' }
]

export function PoolsPage() {
  const queryClient = useQueryClient()
  const [isAddOpen, setIsAddOpen] = useState(false)
  const [newPool, setNewPool] = useState({
    name: '',
    algorithm: 'round_robin'
  })

  const { data: pools = [], isLoading } = useQuery<Pool[]>({
    queryKey: ['pools'],
    queryFn: async () => {
      const response = await api.get('/api/v1/pools')
      return response.data
    }
  })

  const { data: backends = [] } = useQuery<Backend[]>({
    queryKey: ['backends'],
    queryFn: async () => {
      const response = await api.get('/api/v1/backends')
      return response.data
    }
  })

  const createMutation = useMutation({
    mutationFn: (pool: Partial<Pool>) => api.post('/api/v1/pools', pool),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pools'] })
      toast.success('Pool created successfully')
      setIsAddOpen(false)
      setNewPool({ name: '', algorithm: 'round_robin' })
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to create pool')
    }
  })

  const deleteMutation = useMutation({
    mutationFn: (name: string) => api.delete(`/api/v1/pools/${name}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pools'] })
      toast.success('Pool deleted successfully')
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to delete pool')
    }
  })

  const handleCreate = () => {
    if (!newPool.name) {
      toast.error('Pool name is required')
      return
    }
    createMutation.mutate(newPool)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Pools</h1>
          <p className="text-muted-foreground">
            Manage backend pools and load balancing algorithms
          </p>
        </div>
        <Dialog open={isAddOpen} onOpenChange={setIsAddOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Create Pool
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create New Pool</DialogTitle>
              <DialogDescription>
                Create a new backend pool with a load balancing algorithm
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">Pool Name</Label>
                <Input
                  id="name"
                  placeholder="web-servers"
                  value={newPool.name}
                  onChange={(e) => setNewPool({ ...newPool, name: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="algorithm">Algorithm</Label>
                <Select
                  value={newPool.algorithm}
                  onValueChange={(value) =>
                    setNewPool({ ...newPool, algorithm: value })
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {algorithms.map((alg) => (
                      <SelectItem key={alg.value} value={alg.value}>
                        {alg.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setIsAddOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleCreate} disabled={createMutation.isPending}>
                Create Pool
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {isLoading ? (
          <Card className="col-span-full">
            <CardContent className="flex h-32 items-center justify-center">
              <div className="animate-spin">Loading...</div>
            </CardContent>
          </Card>
        ) : pools.length === 0 ? (
          <Card className="col-span-full">
            <CardContent className="flex h-32 flex-col items-center justify-center text-center">
              <Layers className="h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-muted-foreground">No pools configured</p>
              <Button variant="outline" className="mt-4" onClick={() => setIsAddOpen(true)}>
                Create your first pool
              </Button>
            </CardContent>
          </Card>
        ) : (
          pools.map((pool) => (
            <Card key={pool.name}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle>{pool.name}</CardTitle>
                  <div className="flex gap-1">
                    <Button variant="ghost" size="icon">
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => deleteMutation.mutate(pool.name)}
                      disabled={deleteMutation.isPending}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
                <CardDescription>
                  <Badge variant="secondary">
                    {algorithms.find((a) => a.value === pool.algorithm)?.label ||
                      pool.algorithm}
                  </Badge>
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">Backends</span>
                    <span className="font-medium">
                      {pool.backends?.length || 0}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">Health Check</span>
                    <Badge
                      variant={pool.health_check?.enabled ? 'success' : 'secondary'}
                    >
                      {pool.health_check?.enabled ? 'Enabled' : 'Disabled'}
                    </Badge>
                  </div>
                  {pool.backends && pool.backends.length > 0 && (
                    <div className="mt-4 space-y-1">
                      {pool.backends.map((backendId) => {
                        const backend = backends.find((b) => b.id === backendId)
                        return (
                          <div
                            key={backendId}
                            className="flex items-center gap-2 text-sm"
                          >
                            <div
                              className={`h-2 w-2 rounded-full ${
                                backend?.status === 'up'
                                  ? 'bg-green-500'
                                  : backend?.status === 'down'
                                    ? 'bg-red-500'
                                    : 'bg-gray-400'
                              }`}
                            />
                            <span className="truncate">
                              {backend?.address || backendId}
                            </span>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          ))
        )}
      </div>
    </div>
  )
}
