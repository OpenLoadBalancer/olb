import { useState, useCallback } from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'
import { Trash2, Download, Upload } from 'lucide-react'
import { toast } from 'sonner'

interface UseBulkOperationsOptions<T> {
  items: T[]
  getId: (item: T) => string
  onDelete?: (ids: string[]) => Promise<void>
  onExport?: (ids: string[]) => void
}

export function useBulkOperations<T>({
  items,
  getId,
  onDelete,
  onExport
}: UseBulkOperationsOptions<T>) {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  const selectAll = useCallback(() => {
    if (selectedIds.size === items.length) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(items.map(getId)))
    }
  }, [items, getId, selectedIds.size])

  const selectOne = useCallback((id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }, [])

  const isSelected = useCallback((id: string) => selectedIds.has(id), [selectedIds])

  const isAllSelected = items.length > 0 && selectedIds.size === items.length

  const isIndeterminate = selectedIds.size > 0 && selectedIds.size < items.length

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set())
  }, [])

  const handleDelete = useCallback(async () => {
    if (onDelete && selectedIds.size > 0) {
      await onDelete(Array.from(selectedIds))
      clearSelection()
    }
  }, [onDelete, selectedIds, clearSelection])

  const handleExport = useCallback(() => {
    if (onExport && selectedIds.size > 0) {
      onExport(Array.from(selectedIds))
    }
  }, [onExport, selectedIds])

  const selectedItems = items.filter(item => selectedIds.has(getId(item)))

  return {
    selectedIds,
    selectedItems,
    selectedCount: selectedIds.size,
    selectAll,
    selectOne,
    isSelected,
    isAllSelected,
    isIndeterminate,
    clearSelection,
    handleDelete,
    handleExport
  }
}

interface BulkActionToolbarProps {
  selectedCount: number
  totalCount: number
  onDelete?: () => void
  onExport?: () => void
  onClear: () => void
  deleteLabel?: string
  exportLabel?: string
}

export function BulkActionToolbar({
  selectedCount,
  totalCount,
  onDelete,
  onExport,
  onClear,
  deleteLabel = 'Delete Selected',
  exportLabel = 'Export Selected'
}: BulkActionToolbarProps) {
  if (selectedCount === 0) return null

  return (
    <div className="flex items-center justify-between rounded-lg border bg-card p-3 shadow-sm animate-in slide-in-from-bottom-2">
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium">
          {selectedCount} of {totalCount} selected
        </span>
        <Button variant="ghost" size="sm" onClick={onClear}>
          Clear
        </Button>
      </div>
      <div className="flex items-center gap-2">
        {onExport && (
          <Button variant="outline" size="sm" onClick={onExport}>
            <Download className="mr-2 h-4 w-4" />
            {exportLabel}
          </Button>
        )}
        {onDelete && (
          <Button variant="destructive" size="sm" onClick={onDelete}>
            <Trash2 className="mr-2 h-4 w-4" />
            {deleteLabel}
          </Button>
        )}
      </div>
    </div>
  )
}

interface BulkCheckboxProps {
  checked: boolean
  indeterminate?: boolean
  onChange: () => void
  label?: string
}

export function BulkCheckbox({ checked, indeterminate, onChange, label }: BulkCheckboxProps) {
  return (
    <div className="flex items-center gap-2">
      <Checkbox
        checked={checked}
        data-state={indeterminate ? 'indeterminate' : checked ? 'checked' : 'unchecked'}
        onCheckedChange={onChange}
      />
      {label && <span className="text-sm text-muted-foreground">{label}</span>}
    </div>
  )
}

// Import functionality
interface ImportOptions {
  accept?: string
  onImport: (data: any[]) => Promise<void> | void
}

export function useImport({ accept = '.json,.csv', onImport }: ImportOptions) {
  const [isImporting, setIsImporting] = useState(false)

  const handleFileSelect = useCallback(async (file: File) => {
    setIsImporting(true)
    try {
      const content = await file.text()
      let data: any[]

      if (file.name.endsWith('.json')) {
        data = JSON.parse(content)
      } else if (file.name.endsWith('.csv')) {
        data = parseCSV(content)
      } else {
        throw new Error('Unsupported file format')
      }

      await onImport(data)
      toast.success(`Imported ${data.length} items`)
    } catch (error) {
      toast.error('Import failed', {
        description: error instanceof Error ? error.message : 'Unknown error'
      })
    } finally {
      setIsImporting(false)
    }
  }, [onImport])

  const openFileDialog = useCallback(() => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = accept
    input.onchange = (e) => {
      const file = (e.target as HTMLInputElement).files?.[0]
      if (file) handleFileSelect(file)
    }
    input.click()
  }, [accept, handleFileSelect])

  return { isImporting, openFileDialog }
}

function parseCSV(content: string): any[] {
  const lines = content.split('\n').filter(line => line.trim())
  if (lines.length < 2) return []

  const headers = lines[0].split(',').map(h => h.trim())
  return lines.slice(1).map(line => {
    const values = line.split(',').map(v => v.trim())
    const obj: any = {}
    headers.forEach((header, i) => {
      let value: any = values[i] || ''
      // Try to parse numbers and booleans
      if (value === 'true') value = true
      else if (value === 'false') value = false
      else if (!isNaN(Number(value)) && value !== '') value = Number(value)
      obj[header] = value
    })
    return obj
  })
}

export function ImportButton({ onImport }: { onImport: (data: any[]) => Promise<void> }) {
  const { isImporting, openFileDialog } = useImport({ onImport })

  return (
    <Button variant="outline" size="sm" onClick={openFileDialog} disabled={isImporting}>
      <Upload className="mr-2 h-4 w-4" />
      {isImporting ? 'Importing...' : 'Import'}
    </Button>
  )
}
