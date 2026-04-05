import { useState } from 'react'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuCheckboxItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Search, Filter, X } from 'lucide-react'

interface FilterOption {
  id: string
  label: string
  checked: boolean
}

interface FilterGroup {
  label: string
  options: FilterOption[]
}

interface SearchFilterProps {
  placeholder?: string
  onSearch?: (query: string) => void
  onFilterChange?: (filters: FilterGroup[]) => void
  filters?: FilterGroup[]
  className?: string
  debounceMs?: number
}

export function SearchFilter({
  placeholder = 'Search...',
  onSearch,
  onFilterChange,
  filters = [],
  className
}: SearchFilterProps) {
  const [searchQuery, setSearchQuery] = useState('')
  const [localFilters, setLocalFilters] = useState<FilterGroup[]>(filters)

  const handleSearch = (value: string) => {
    setSearchQuery(value)
    onSearch?.(value)
  }

  const handleFilterToggle = (groupIndex: number, optionId: string) => {
    const newFilters = localFilters.map((group, idx) => {
      if (idx !== groupIndex) return group
      return {
        ...group,
        options: group.options.map((opt) =>
          opt.id === optionId ? { ...opt, checked: !opt.checked } : opt
        )
      }
    })
    setLocalFilters(newFilters)
    onFilterChange?.(newFilters)
  }

  const hasActiveFilters = localFilters.some((g) => g.options.some((o) => o.checked))

  const clearFilters = () => {
    const cleared = localFilters.map((group) => ({
      ...group,
      options: group.options.map((opt) => ({ ...opt, checked: false }))
    }))
    setLocalFilters(cleared)
    onFilterChange?.(cleared)
  }

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <div className="relative flex-1">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder={placeholder}
          value={searchQuery}
          onChange={(e) => handleSearch(e.target.value)}
          className="pl-9"
        />
        {searchQuery && (
          <Button
            variant="ghost"
            size="icon"
            className="absolute right-1 top-1/2 h-6 w-6 -translate-y-1/2"
            onClick={() => handleSearch('')}
          >
            <X className="h-3 w-3" />
          </Button>
        )}
      </div>
      {filters.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon" className="relative">
              <Filter className="h-4 w-4" />
              {hasActiveFilters && (
                <span className="absolute -right-1 -top-1 h-2 w-2 rounded-full bg-primary" />
              )}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            {localFilters.map((group, groupIndex) => (
              <div key={group.label}>
                <DropdownMenuLabel>{group.label}</DropdownMenuLabel>
                {group.options.map((option) => (
                  <DropdownMenuCheckboxItem
                    key={option.id}
                    checked={option.checked}
                    onCheckedChange={() => handleFilterToggle(groupIndex, option.id)}
                  >
                    {option.label}
                  </DropdownMenuCheckboxItem>
                ))}
                {groupIndex < localFilters.length - 1 && <DropdownMenuSeparator />}
              </div>
            ))}
            {hasActiveFilters && (
              <>
                <DropdownMenuSeparator />
                <Button
                  variant="ghost"
                  size="sm"
                  className="w-full justify-center"
                  onClick={clearFilters}
                >
                  Clear Filters
                </Button>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}

interface ColumnVisibilityProps<T extends string> {
  columns: { id: T; label: string; visible: boolean }[]
  onChange: (columns: { id: T; label: string; visible: boolean }[]) => void
}

export function ColumnVisibility<T extends string>({
  columns,
  onChange
}: ColumnVisibilityProps<T>) {
  const toggleColumn = (id: T) => {
    const newColumns = columns.map((col) =>
      col.id === id ? { ...col, visible: !col.visible } : col
    )
    onChange(newColumns)
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm">
          Columns
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Toggle Columns</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {columns.map((column) => (
          <DropdownMenuCheckboxItem
            key={column.id}
            checked={column.visible}
            onCheckedChange={() => toggleColumn(column.id)}
          >
            {column.label}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
