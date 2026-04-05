import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Plus, RefreshCw, Trash2, Edit, Server, Search } from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { HealthStatusBadge } from '@/components/health'
import { SearchFilter } from '@/components/search-filter'
import { TableLoading } from '@/components/loading'
import { useConfirmDialog } from '@/components/confirm-dialog'
import { useForm, FormField, FormError } from '@/components/form'
import { DataExport } from '@/components/data-export'
import type { Backend } from '@/types'

export function BackendsPage() {
  const queryClient = useQueryClient()
  const { confirm, dialog } = useConfirmDialog()
  const [isAddOpen, setIsAddOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')

  const { data: backends = [], isLoading } = useQuery<Backend[]>({
    queryKey: ['backends'],
    queryFn: async () => {
      const response = await api.get('/api/v1/backends')
      return response.data
    }
  })

  const filteredBackends = backends.filter(
    (backend) =>
      backend.address.toLowerCase().includes(searchQuery.toLowerCase()) ||
      backend.id.toLowerCase().includes(searchQuery.toLowerCase())
  )

  // Form with validation
  const form = useForm(
    { address: '', weight: 1, max_connections: 1000 },
    {
      address: { required: true, minLength: 3 },
      weight: { required: true, min: 1, max: 100 },
      max_connections: { required: true, min: 1, max: 100000 }
    }
  )

  const createMutation = useMutation({
    mutationFn: (backend: Partial<Backend>) => api.post('/api/v1/backends', backend),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      toast.success('Backend created successfully')
      setIsAddOpen(false)
      form.reset()
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to create backend')
    }
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/backends/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      toast.success('Backend deleted successfully')
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to delete backend')
    }
  })

  const healthCheckMutation = useMutation({
    mutationFn: (id: string) => api.post(`/api/v1/backends/${id}/healthcheck`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backends'] })
      toast.success('Health check triggered')
    }
  })

  const handleCreate = () => {
    form.handleSubmit(async (values) => {
      createMutation.mutate(values)
    })
  }

  const handleDelete = async (backend: Backend) => {
    await confirm(
      {
        title: 'Delete Backend',
        description: `Are you sure you want to delete "${backend.address}"? This action cannot be undone.`,
        confirmText: 'Delete',
        variant: 'destructive'
      },
      async () => {
        deleteMutation.mutate(backend.id)
      }
    )
  }

  return (
    <div className="space-y-6">
      {dialog}

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Backends</h1>
          <p className="text-muted-foreground">
            Manage your backend servers and their health status
          </p>
        </div>
        <div className="flex items-center gap-2">
          <DataExport data={backends} filename="backends" />
          <Dialog open={isAddOpen} onOpenChange={setIsAddOpen}>
            <DialogTrigger asChild>
              <Button>
                <Plus className="mr-2 h-4 w-4" />
                Add Backend
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Add New Backend</DialogTitle>
                <DialogDescription>
                  Add a new backend server to your load balancer
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <FormField
                  label="Address"
                  name="address"
                  error={form.errors.address}
                  touched={form.touched.address}
                  required
                >
                  <Input
                    id="address"
                    placeholder="localhost:3001"
                    value={form.values.address}
                    onChange={(e) => form.handleChange('address', e.target.value)}
                    onBlur={() => form.handleBlur('address')}
                  />
                </FormField>

                <FormField
                  label="Weight"
                  name="weight"
                  error={form.errors.weight}
                  touched={form.touched.weight}
                  required
                  helpText="Higher weight receives more traffic"
                >
                  <Input
                    id="weight"
                    type="number"
                    min={1}
                    max={100}
                    value={form.values.weight}
                    onChange={(e) => form.handleChange('weight', parseInt(e.target.value) || 0)}
                    onBlur={() => form.handleBlur('weight')}
                  />
                </FormField>

                <FormField
                  label="Max Connections"
                  name="max_connections"
                  error={form.errors.max_connections}
                  touched={form.touched.max_connections}
                  required
                >
                  <Input
                    id="max-connections"
                    type="number"
                    min={1}
                    value={form.values.max_connections}
                    onChange={(e) =>
                      form.handleChange('max_connections', parseInt(e.target.value) || 0)
                    }
                    onBlur={() => form.handleBlur('max_connections')}
                  />
                </FormField>
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setIsAddOpen(false)}>
                  Cancel
                </Button>
                <Button
                  onClick={handleCreate}
                  disabled={createMutation.isPending || form.isSubmitting}
                >
                  {createMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                  Create Backend
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>All Backends</CardTitle>
              <CardDescription>
                A list of all backend servers in your infrastructure
              </CardDescription>
            </div>
            <SearchFilter
              placeholder="Search backends..."
              onSearch={setSearchQuery}
              className="w-[300px]"
            />
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableLoading rows={5} />
          ) : backends.length === 0 ? (
            <EmptyState
              icon={Server}
              title="No backends configured"
              description="Get started by adding your first backend server to the load balancer."
              action={{
                label: 'Add your first backend',
                onClick: () => setIsAddOpen(true)
              }}
            />
          ) : filteredBackends.length === 0 ? (
            <EmptyState
              icon={Search}
              title="No backends found"
              description="No backends match your search query. Try adjusting your filters."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Weight</TableHead>
                  <TableHead>Connections</TableHead>
                  <TableHead>Response Time</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredBackends.map((backend) => (
                  <TableRow key={backend.id}>
                    <TableCell className="font-medium">{backend.id}</TableCell>
                    <TableCell>{backend.address}</TableCell>
                    <TableCell>
                      <HealthStatusBadge status={backend.status} size="sm" />
                    </TableCell>
                    <TableCell>{backend.weight}</TableCell>
                    <TableCell>
                      {backend.active_connections || 0} / {backend.max_connections}
                    </TableCell>
                    <TableCell>
                      {backend.response_time ? `${backend.response_time}ms` : '-'}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => healthCheckMutation.mutate(backend.id)}
                          disabled={healthCheckMutation.isPending}
                        >
                          <RefreshCw className="h-4 w-4" />
                        </Button>
                        <Button variant="ghost" size="icon">
                          <Edit className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleDelete(backend)}
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
