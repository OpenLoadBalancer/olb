import {
  useState,
  useMemo,
  useCallback,
  type ReactNode,
  type ChangeEvent
} from 'react'
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  type ColumnDef,
  type SortingState,
  type ColumnFiltersState,
  flexRender,
  type Table as TanstackTable
} from '@tanstack/react-table'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  ChevronDown,
  ChevronUp,
  ChevronsUpDown,
  MoreHorizontal,
  Download,
  Filter,
  Trash2,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Settings2,
  Search,
  X
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { LucideIcon } from 'lucide-react'

interface DataTableProps<TData> {
  data: TData[]
  columns: ColumnDef<TData>[]
  isLoading?: boolean
  enableSorting?: boolean
  enableFiltering?: boolean
  enablePagination?: boolean
  enableColumnVisibility?: boolean
  enableRowSelection?: boolean
  enableBulkActions?: boolean
  searchPlaceholder?: string
  searchColumn?: string
  pageSize?: number
  pageSizeOptions?: number[]
  emptyMessage?: string
  className?: string
  onRowClick?: (row: TData) => void
  bulkActions?: {
    onDelete?: (selected: TData[]) => void
    onExport?: (selected: TData[]) => void
  }
  toolbar?: ReactNode
  getRowId?: (row: TData) => string
  actions?: {
    label: string
    icon: LucideIcon
    variant?: 'default' | 'destructive'
    onClick: (row: TData) => void
  }[]
}

export function DataTable<TData>({
  data,
  columns,
  isLoading = false,
  enableSorting = true,
  enableFiltering = true,
  enablePagination = true,
  enableColumnVisibility = true,
  enableRowSelection = false,
  enableBulkActions = false,
  searchPlaceholder = 'Search...',
  searchColumn,
  pageSize = 10,
  pageSizeOptions = [10, 20, 50, 100],
  emptyMessage = 'No results found',
  className,
  onRowClick,
  bulkActions,
  toolbar,
  getRowId,
  actions
}: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const [globalFilter, setGlobalFilter] = useState('')
  const [columnVisibility, setColumnVisibility] = useState<Record<string, boolean>>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting,
      columnFilters,
      globalFilter,
      columnVisibility,
      rowSelection
    },
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onGlobalFilterChange: setGlobalFilter,
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    enableSorting,
    enableMultiSort: true,
    getRowId: getRowId as ((row: TData) => string) | undefined,
    enableRowSelection
  })

  const selectedRows = useMemo(() => {
    return table.getSelectedRowModel().rows.map(row => row.original)
  }, [table])

  const selectedCount = selectedRows.length

  const handleDeleteSelected = useCallback(() => {
    if (bulkActions?.onDelete && selectedCount > 0) {
      bulkActions.onDelete(selectedRows)
      setRowSelection({})
    }
  }, [bulkActions, selectedRows, selectedCount])

  const handleExportSelected = useCallback(() => {
    if (bulkActions?.onExport && selectedCount > 0) {
      bulkActions.onExport(selectedRows)
    }
  }, [bulkActions, selectedRows, selectedCount])

  const hasBulkActions = enableBulkActions && (bulkActions?.onDelete || bulkActions?.onExport)

  return (
    <div className={cn('space-y-4', className)}>
      {/* Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-2">
          {/* Global Search */}
          {enableFiltering && (
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder={searchPlaceholder}
                value={globalFilter}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setGlobalFilter(e.target.value)}
                className="w-[250px] pl-9"
              />
              {globalFilter && (
                <Button
                  variant="ghost"
                  size="icon"
                  className="absolute right-0 top-0 h-9 w-9"
                  onClick={() => setGlobalFilter('')}
                >
                  <X className="h-4 w-4" />
                </Button>
              )}
            </div>
          )}

          {/* Column Filters Toggle */}
          {enableFiltering && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <Filter className="mr-2 h-4 w-4" />
                  Filters
                  {columnFilters.length > 0 && (
                    <Badge variant="secondary" className="ml-2">
                      {columnFilters.length}
                    </Badge>
                  )}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-[200px]">
                <DropdownMenuLabel>Filter by column</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {table.getAllColumns()
                  .filter(column => column.getCanFilter())
                  .map(column => (
                    <DropdownMenuItem key={column.id}>
                      <div className="flex items-center gap-2">
                        <span className="capitalize">{column.id}</span>
                        <Input
                          value={(column.getFilterValue() as string) ?? ''}
                          onChange={e => column.setFilterValue(e.target.value)}
                          placeholder={`Filter ${column.id}...`}
                          className="h-8"
                          onClick={e => e.stopPropagation()}
                        />
                      </div>
                    </DropdownMenuItem>
                  ))}
                {columnFilters.length > 0 && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onClick={() => setColumnFilters([])}>
                      Clear all filters
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          )}

          {/* Bulk Actions */}
          {hasBulkActions && selectedCount > 0 && (
            <div className="flex items-center gap-2">
              <Badge variant="secondary">{selectedCount} selected</Badge>
              {bulkActions?.onDelete && (
                <Button variant="destructive" size="sm" onClick={handleDeleteSelected}>
                  <Trash2 className="mr-2 h-4 w-4" />
                  Delete
                </Button>
              )}
              {bulkActions?.onExport && (
                <Button variant="outline" size="sm" onClick={handleExportSelected}>
                  <Download className="mr-2 h-4 w-4" />
                  Export
                </Button>
              )}
            </div>
          )}
        </div>

        <div className="flex items-center gap-2">
          {/* Column Visibility */}
          {enableColumnVisibility && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <Settings2 className="mr-2 h-4 w-4" />
                  Columns
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {table
                  .getAllColumns()
                  .filter(column => column.getCanHide())
                  .map(column => {
                    return (
                      <DropdownMenuCheckboxItem
                        key={column.id}
                        className="capitalize"
                        checked={column.getIsVisible()}
                        onCheckedChange={value => column.toggleVisibility(!!value)}
                      >
                        {column.id}
                      </DropdownMenuCheckboxItem>
                    )
                  })}
              </DropdownMenuContent>
            </DropdownMenu>
          )}

          {toolbar}
        </div>
      </div>

      {/* Table */}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map(headerGroup => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map(header => {
                  const isSortable = header.column.getCanSort()
                  const isSorted = header.column.getIsSorted()

                  return (
                    <TableHead key={header.id} className="whitespace-nowrap">
                      {flexRender(header.column.columnDef.header, {
                        ...header.getContext(),
                        column: header.column
                      })}
                      {isSortable && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="ml-1 h-6 w-6 p-0"
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          {isSorted === 'asc' && <ChevronUp className="h-3 w-3" />}
                          {isSorted === 'desc' && <ChevronDown className="h-3 w-3" />}
                          {!isSorted && <ChevronsUpDown className="h-3 w-3 opacity-30" />}
                        </Button>
                      )}
                    </TableHead>
                  )
                })}
                {actions && actions.length > 0 && (
                  <TableHead className="w-[50px]"></TableHead>
                )}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isLoading ? (
              // Loading skeletons
              Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  {columns.map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-4 w-full" />
                    </TableCell>
                  ))}
                  {actions && actions.length > 0 && (
                    <TableCell>
                      <Skeleton className="h-8 w-8" />
                    </TableCell>
                  )}
                </TableRow>
              ))
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map(row => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                  className={cn(
                    onRowClick && 'cursor-pointer',
                    row.getIsSelected() && 'bg-muted'
                  )}
                  onClick={() => onRowClick?.(row.original)}
                >
                  {row.getVisibleCells().map(cell => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                  {actions && actions.length > 0 && (
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          {actions.map((action, idx) => (
                            <DropdownMenuItem
                              key={idx}
                              onClick={(e) => {
                                e.stopPropagation()
                                action.onClick(row.original)
                              }}
                              className={cn(
                                action.variant === 'destructive' && 'text-destructive focus:text-destructive'
                              )}
                            >
                              <action.icon className="mr-2 h-4 w-4" />
                              {action.label}
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  )}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length + (actions?.length ? 1 : 0)}
                  className="h-32 text-center"
                >
                  <div className="flex flex-col items-center gap-2 text-muted-foreground">
                    <Search className="h-8 w-8 opacity-20" />
                    <p>{emptyMessage}</p>
                    {globalFilter && (
                      <Button variant="link" onClick={() => setGlobalFilter('')}>
                        Clear search
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
      {enablePagination && !isLoading && (
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <span>
              Showing {table.getRowModel().rows.length} of {table.getFilteredRowModel().rows.length}{' '}
              results
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {/* Page size selector */}
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">Rows per page:</span>
              <Select
                value={table.getState().pagination.pageSize.toString()}
                onValueChange={value => table.setPageSize(Number(value))}
              >
                <SelectTrigger className="h-8 w-[70px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {pageSizeOptions.map(size => (
                    <SelectItem key={size} value={size.toString()}>
                      {size}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Page navigation */}
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="icon"
                className="h-8 w-8"
                onClick={() => table.setPageIndex(0)}
                disabled={!table.getCanPreviousPage()}
              >
                <ChevronsLeft className="h-4 w-4" />
              </Button>
              <Button
                variant="outline"
                size="icon"
                className="h-8 w-8"
                onClick={() => table.previousPage()}
                disabled={!table.getCanPreviousPage()}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <span className="px-2 text-sm">
                Page {table.getState().pagination.pageIndex + 1} of {table.getPageCount()}
              </span>
              <Button
                variant="outline"
                size="icon"
                className="h-8 w-8"
                onClick={() => table.nextPage()}
                disabled={!table.getCanNextPage()}
              >
                <ChevronRight className="h-4 w-4" />
              </Button>
              <Button
                variant="outline"
                size="icon"
                className="h-8 w-8"
                onClick={() => table.setPageIndex(table.getPageCount() - 1)}
                disabled={!table.getCanNextPage()}
              >
                <ChevronsRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// Helper hook for table state
export function useDataTableState<T>({
  data,
  columns,
  pageSize = 10
}: {
  data: T[]
  columns: ColumnDef<T>[]
  pageSize?: number
}) {
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const [globalFilter, setGlobalFilter] = useState('')
  const [columnVisibility, setColumnVisibility] = useState<Record<string, boolean>>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})

  return {
    sorting,
    setSorting,
    columnFilters,
    setColumnFilters,
    globalFilter,
    setGlobalFilter,
    columnVisibility,
    setColumnVisibility,
    rowSelection,
    setRowSelection
  }
}

// Status badge component helper
interface StatusBadgeProps {
  status: 'up' | 'down' | 'healthy' | 'unhealthy' | 'active' | 'inactive' | string
  className?: string
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; label: string }> = {
    up: { variant: 'default', label: 'Up' },
    down: { variant: 'destructive', label: 'Down' },
    healthy: { variant: 'default', label: 'Healthy' },
    unhealthy: { variant: 'destructive', label: 'Unhealthy' },
    active: { variant: 'default', label: 'Active' },
    inactive: { variant: 'secondary', label: 'Inactive' },
    pending: { variant: 'outline', label: 'Pending' },
    error: { variant: 'destructive', label: 'Error' },
    warning: { variant: 'secondary', label: 'Warning' }
  }

  const config = variants[status] || { variant: 'secondary', label: status }

  return (
    <Badge variant={config.variant} className={className}>
      {config.label}
    </Badge>
  )
}
