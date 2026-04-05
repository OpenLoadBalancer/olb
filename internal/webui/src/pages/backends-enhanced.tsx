import { useState, useCallback, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Badge } from '@/components/ui/badge'
import { DataTable, StatusBadge } from '@/components/data-table'
import api from '@/lib/api'
import { toast } from 'sonner'
import {
  MoreHorizontal,
  Plus,
  RefreshCw,
  Trash2,
  Edit,
  Power,
  PowerOff,
  Download,
  Upload,
  Server
} from 'lucide-react'
import type { Backend } from '@/types'
import { formatDate } from '@/lib/utils'
import { useBulkOperations } from '@/hooks/bulk-operations'
import { ImportButton } from '@/hooks/bulk-operations'

// Columns definition for the data table
const columns: ColumnDef<Backend>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => {
      const backend = row.original
      return (
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-muted-foreground" />
          <div>
            <div className="font-medium">{backend.name}</div>
            <div className="text-xs text-muted-foreground">{backend.address}</div>
          </div>
        </div>
      )
    }
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => <StatusBadge status={row.original.status} />
  },
  {
    accessorKey: 'pool',
    header: 'Pool',
    cell: ({ row }) => (
      <Badge variant="outline">{row.original.pool || 'default'}</Badge>
    )
  },
  {
    accessorKey: 'weight',
    header: 'Weight',
    cell: ({ row }) => (
      <div className="font-mono">{row.original.weight || 1}</div>
    )
  },
  {
    accessorKey: 'health',
    header: 'Health',
    cell: ({ row }) => {
      const backend = row.original
      if (!backend.health_checks?.enabled) {
        return <span className="text-muted-foreground">Disabled</span>
      }
      const healthy = backend.health_checks.consecutive_successes || 0
      const threshold = backend.health_checks.healthy_threshold || 2
      return (
        <div className="flex items-center gap-2">
          <div className="h-2 w-16 rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-green-500 transition-all"
              style={{ width: `${Math.min((healthy / threshold) * 100, 100)}%` }}
            />
          </div>
          <span className="text-xs text-muted-foreground">{healthy}/{threshold}</span>
        </div>
      )
    }
  },
  {
    accessorKey: 'last_check',
    header: 'Last Check',
    cell: ({ row }) => (
      <span className="text-muted-foreground">
        {row.original.last_check
          ? formatDate(row.original.last_check)
          : 'Never'}
      </span>
    )
  },
  {
    id: 'actions',
    header: '',
    cell: ({ row }) => <BackendActions backend={row.original} />
  }
]

// Backend actions component
function BackendActions({ backend }: { backend: Backend }) {
  const queryClient = useQueryClient()

  const { mutate: toggleStatus } = useMutation({
    mutationFn: async () => {
      const newStatus = backend.status === 'up' ? 'down' : 'up'
      await api.patch(`/api/v1/backends/${backend.id}`, { status: newStatus })
      return newStatus
    },
    onSuccess: (newStatus) => {
      toast.success(`Backend ${backend.name} is now ${newStatus}`)
      queryClient.invalidateQueries({ queryKey: ['backends'] })
    },
    onError: () => {
      toast.error('Failed to update backend status')
    }
  })

  const { mutate: deleteBackend } = useMutation({
    mutationFn: async () => {
      await api.delete(`/api/v1/backends/${backend.id}`)
    },
    onSuccess: () => {
      toast.success('Backend deleted')
      queryClient.invalidateQueries({ queryKey: ['backends'] })
    },
    onError: () => {
      toast.error('Failed to delete backend')
    }
  })

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="h-8 w-8">
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem>
          <Edit className="mr-2 h-4 w-4" />
          Edit
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => toggleStatus()}>
          {backend.status === 'up' ? (
            <>
              <PowerOff className="mr-2 h-4 w-4" />
              Disable
            </>
          ) : (
            <>
              <Power className="mr-2 h-4 w-4" />
              Enable
            </>
          )}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="text-destructive"
          onClick={() => deleteBackend()}
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function BackendsPage() {
  const queryClient = useQueryClient()
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false)

  const { data: backends = [], isLoading } = useQuery<Backend[]>({
    queryKey: ['backends'],
    queryFn: async () => {
      const response = await api.get('/api/v1/backends')
      return response.data
    }
  })

  // Bulk operations handlers
  const handleDelete = useCallback(
    async (selectedBackends: Backend[]) => {
      try {
        await Promise.all(
          selectedBackends.map(b => api.delete(`/api/v1/backends/${b.id}`))
        )
        toast.success(`Deleted ${selectedBackends.length} backends`)
        queryClient.invalidateQueries({ queryKey: ['backends'] })
      } catch {
        toast.error('Failed to delete some backends')
      }
    },
    [queryClient]
  )

  const handleExport = useCallback((selectedBackends: Backend[]) => {
    const data = JSON.stringify(selectedBackends, null, 2)
    const blob = new Blob([data], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `backends-${new Date().toISOString().split('T')[0]}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    toast.success(`Exported ${selectedBackends.length} backends`)
  }, [])

  const handleImport = useCallback(
    async (data: Backend[]) => {
      try {
        await Promise.all(
          data.map(b =>
            api.post('/api/v1/backends', {
              name: b.name,
              address: b.address,
              weight: b.weight,
              pool: b.pool
            })
          )
        )
        toast.success(`Imported ${data.length} backends`)
        queryClient.invalidateQueries({ queryKey: ['backends'] })
      } catch {
        toast.error('Failed to import some backends')
      }
    },
    [queryClient]
  )

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Backends</h1>
          <p className="text-muted-foreground">
            Manage your backend servers and their configuration
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={() => queryClient.invalidateQueries({ queryKey: ['backends'] })}
          >
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <ImportButton onImport={handleImport} />
          <Button onClick={() => setIsCreateDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Add Backend
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <div className="rounded-lg border bg-card p-4">
          <div className="text-sm text-muted-foreground">Total Backends</div>
          <div className="mt-1 text-2xl font-bold">{backends.length}</div>
        </div>
        <div className="rounded-lg border bg-card p-4">
          <div className="text-sm text-muted-foreground">Healthy</div>
          <div className="mt-1 text-2xl font-bold text-green-500">
            {backends.filter(b => b.status === 'up').length}
          </div>
        </div>
        <div className="rounded-lg border bg-card p-4">
          <div className="text-sm text-muted-foreground">Unhealthy</div>
          <div className="mt-1 text-2xl font-bold text-destructive">
            {backends.filter(b => b.status === 'down').length}
          </div>
        </div>
        <div className="rounded-lg border bg-card p-4">
          <div className="text-sm text-muted-foreground">Avg Weight</div>
          <div className="mt-1 text-2xl font-bold">
            {backends.length > 0
              ? Math.round(
                  backends.reduce((acc, b) => acc + (b.weight || 1), 0) /
                    backends.length
                )
              : 0}
          </div>
        </div>
      </div>

      {/* Data Table */}
      <DataTable
        data={backends}
        columns={columns}
        isLoading={isLoading}
        enableSorting
        enableFiltering
        enablePagination
        enableColumnVisibility
        enableRowSelection
        enableBulkActions
        searchPlaceholder="Search backends..."
        emptyMessage="No backends found. Add your first backend to get started."
        pageSize={10}
        pageSizeOptions={[10, 20, 50, 100]}
        bulkActions={{
          onDelete: handleDelete,
          onExport: handleExport
        }}
        getRowId={(row: Backend) => row.id}
      />
    </div>
  )
}

export default BackendsPage
