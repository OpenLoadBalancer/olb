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
import { Plus, Route, Edit, Trash2 } from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import type { Route as RouteType, Pool } from '@/types'

export function RoutesPage() {
  const queryClient = useQueryClient()
  const [isAddOpen, setIsAddOpen] = useState(false)
  const [newRoute, setNewRoute] = useState({
    path: '',
    pool: '',
    priority: 0
  })

  const { data: routes = [], isLoading } = useQuery<RouteType[]>({
    queryKey: ['routes'],
    queryFn: async () => {
      const response = await api.get('/api/v1/routes')
      return response.data
    }
  })

  const { data: pools = [] } = useQuery<Pool[]>({
    queryKey: ['pools'],
    queryFn: async () => {
      const response = await api.get('/api/v1/pools')
      return response.data
    }
  })

  const createMutation = useMutation({
    mutationFn: (route: Partial<RouteType>) => api.post('/api/v1/routes', route),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      toast.success('Route created successfully')
      setIsAddOpen(false)
      setNewRoute({ path: '', pool: '', priority: 0 })
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to create route')
    }
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/routes/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['routes'] })
      toast.success('Route deleted successfully')
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to delete route')
    }
  })

  const handleCreate = () => {
    if (!newRoute.path || !newRoute.pool) {
      toast.error('Path and pool are required')
      return
    }
    createMutation.mutate(newRoute)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Routes</h1>
          <p className="text-muted-foreground">
            Configure routing rules for incoming requests
          </p>
        </div>
        <Dialog open={isAddOpen} onOpenChange={setIsAddOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Route
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add New Route</DialogTitle>
              <DialogDescription>
                Create a new routing rule to direct traffic to a pool
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="path">Path Pattern</Label>
                <Input
                  id="path"
                  placeholder="/api/*"
                  value={newRoute.path}
                  onChange={(e) => setNewRoute({ ...newRoute, path: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="pool">Target Pool</Label>
                <Select
                  value={newRoute.pool}
                  onValueChange={(value) => setNewRoute({ ...newRoute, pool: value })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select a pool" />
                  </SelectTrigger>
                  <SelectContent>
                    {pools.map((pool) => (
                      <SelectItem key={pool.name} value={pool.name}>
                        {pool.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="priority">Priority</Label>
                <Input
                  id="priority"
                  type="number"
                  value={newRoute.priority}
                  onChange={(e) =>
                    setNewRoute({ ...newRoute, priority: parseInt(e.target.value) })
                  }
                />
                <p className="text-xs text-muted-foreground">
                  Higher priority routes are matched first
                </p>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setIsAddOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleCreate} disabled={createMutation.isPending}>
                Create Route
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>All Routes</CardTitle>
          <CardDescription>
            Routes are evaluated in priority order (highest first)
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex h-32 items-center justify-center">
              <div className="animate-spin">Loading...</div>
            </div>
          ) : routes.length === 0 ? (
            <div className="flex h-32 flex-col items-center justify-center text-center">
              <Route className="h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-muted-foreground">No routes configured</p>
              <Button variant="outline" className="mt-4" onClick={() => setIsAddOpen(true)}>
                Add your first route
              </Button>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Priority</TableHead>
                  <TableHead>Path</TableHead>
                  <TableHead>Pool</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Requests</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {routes
                  .sort((a, b) => (b.priority || 0) - (a.priority || 0))
                  .map((route) => (
                    <TableRow key={route.id}>
                      <TableCell>
                        <Badge variant="outline">{route.priority || 0}</Badge>
                      </TableCell>
                      <TableCell className="font-medium">{route.path}</TableCell>
                      <TableCell>
                        <Badge variant="secondary">{route.pool}</Badge>
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={route.enabled !== false ? 'success' : 'secondary'}
                        >
                          {route.enabled !== false ? 'Active' : 'Disabled'}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {route.request_count?.toLocaleString() || 0}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <Button variant="ghost" size="icon">
                            <Edit className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => deleteMutation.mutate(route.id)}
                            disabled={deleteMutation.isPending}
                          >
                            <Trash2 className="h-4 w-4 text-destructive" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
