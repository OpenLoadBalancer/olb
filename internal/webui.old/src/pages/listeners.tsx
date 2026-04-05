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
import { Plus, Radio, Edit, Trash2, Power, PowerOff } from 'lucide-react'
import api from '@/lib/api'
import { toast } from 'sonner'
import type { Listener } from '@/types'

const protocols = [
  { value: 'http', label: 'HTTP' },
  { value: 'https', label: 'HTTPS' },
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' }
]

export function ListenersPage() {
  const queryClient = useQueryClient()
  const [isAddOpen, setIsAddOpen] = useState(false)
  const [newListener, setNewListener] = useState({
    name: '',
    address: ':8080',
    protocol: 'http'
  })

  const { data: listeners = [], isLoading } = useQuery<Listener[]>({
    queryKey: ['listeners'],
    queryFn: async () => {
      const response = await api.get('/api/v1/listeners')
      return response.data
    }
  })

  const createMutation = useMutation({
    mutationFn: (listener: Partial<Listener>) => api.post('/api/v1/listeners', listener),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['listeners'] })
      toast.success('Listener created successfully')
      setIsAddOpen(false)
      setNewListener({ name: '', address: ':8080', protocol: 'http' })
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to create listener')
    }
  })

  const deleteMutation = useMutation({
    mutationFn: (name: string) => api.delete(`/api/v1/listeners/${name}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['listeners'] })
      toast.success('Listener deleted successfully')
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to delete listener')
    }
  })

  const toggleMutation = useMutation({
    mutationFn: ({ name, enabled }: { name: string; enabled: boolean }) =>
      api.patch(`/api/v1/listeners/${name}`, { enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['listeners'] })
      toast.success('Listener updated')
    }
  })

  const handleCreate = () => {
    if (!newListener.name || !newListener.address) {
      toast.error('Name and address are required')
      return
    }
    createMutation.mutate(newListener)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Listeners</h1>
          <p className="text-muted-foreground">
            Configure network listeners and their protocols
          </p>
        </div>
        <Dialog open={isAddOpen} onOpenChange={setIsAddOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Listener
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add New Listener</DialogTitle>
              <DialogDescription>
                Create a new listener to accept incoming traffic
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="name">Listener Name</Label>
                <Input
                  id="name"
                  placeholder="http-public"
                  value={newListener.name}
                  onChange={(e) =>
                    setNewListener({ ...newListener, name: e.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="address">Address</Label>
                <Input
                  id="address"
                  placeholder=":8080"
                  value={newListener.address}
                  onChange={(e) =>
                    setNewListener({ ...newListener, address: e.target.value })
                  }
                />
                <p className="text-xs text-muted-foreground">
                  Use :port to bind to all interfaces or ip:port for specific IP
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="protocol">Protocol</Label>
                <Select
                  value={newListener.protocol}
                  onValueChange={(value) =>
                    setNewListener({ ...newListener, protocol: value })
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {protocols.map((p) => (
                      <SelectItem key={p.value} value={p.value}>
                        {p.label}
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
                Create Listener
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
        ) : listeners.length === 0 ? (
          <Card className="col-span-full">
            <CardContent className="flex h-32 flex-col items-center justify-center text-center">
              <Radio className="h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-muted-foreground">No listeners configured</p>
              <Button variant="outline" className="mt-4" onClick={() => setIsAddOpen(true)}>
                Add your first listener
              </Button>
            </CardContent>
          </Card>
        ) : (
          listeners.map((listener) => (
            <Card key={listener.name}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <CardTitle>{listener.name}</CardTitle>
                    <Badge variant={listener.enabled ? 'success' : 'secondary'}>
                      {listener.enabled ? 'Active' : 'Disabled'}
                    </Badge>
                  </div>
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() =>
                        toggleMutation.mutate({
                          name: listener.name,
                          enabled: !listener.enabled
                        })
                      }
                    >
                      {listener.enabled ? (
                        <Power className="h-4 w-4 text-green-500" />
                      ) : (
                        <PowerOff className="h-4 w-4 text-gray-400" />
                      )}
                    </Button>
                    <Button variant="ghost" size="icon">
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => deleteMutation.mutate(listener.name)}
                      disabled={deleteMutation.isPending}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
                <CardDescription>
                  {protocols.find((p) => p.value === listener.protocol)?.label ||
                    listener.protocol}{' '}
                  on {listener.address}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">Routes</span>
                    <span className="font-medium">
                      {listener.routes?.length || 0}
                    </span>
                  </div>
                  {listener.tls && (
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-muted-foreground">TLS</span>
                      <Badge variant="success">Enabled</Badge>
                    </div>
                  )}
                  {listener.routes && listener.routes.length > 0 && (
                    <div className="mt-4 border-t pt-4">
                      <p className="text-xs text-muted-foreground mb-2">Routes:</p>
                      <div className="space-y-1">
                        {listener.routes.slice(0, 3).map((route) => (
                          <div
                            key={route.id}
                            className="flex items-center justify-between text-sm"
                          >
                            <code className="text-xs bg-muted px-1 py-0.5 rounded">
                              {route.path}
                            </code>
                            <span className="text-xs text-muted-foreground">
                              → {route.pool}
                            </span>
                          </div>
                        ))}
                        {listener.routes.length > 3 && (
                          <p className="text-xs text-muted-foreground">
                            +{listener.routes.length - 3} more routes
                          </p>
                        )}
                      </div>
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
