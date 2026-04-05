import { useState, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DragDropContext, Droppable, Draggable } from '@hello-pangea/dnd'
import { cn } from '@/lib/utils'
import {
  GripVertical,
  Eye,
  EyeOff,
  LayoutGrid,
  Settings2,
  RotateCcw,
  Save,
  X
} from 'lucide-react'
import { toast } from 'sonner'

export interface DashboardWidget {
  id: string
  title: string
  description?: string
  visible: boolean
  size: 'small' | 'medium' | 'large' | 'full'
}

const defaultWidgets: DashboardWidget[] = [
  { id: 'stats', title: 'Statistics Cards', description: 'Backend, pool, listener counts', visible: true, size: 'full' },
  { id: 'traffic', title: 'Traffic Overview', description: 'Requests per minute chart', visible: true, size: 'large' },
  { id: 'status', title: 'Response Codes', description: 'HTTP status distribution', visible: true, size: 'medium' },
  { id: 'load', title: 'Current Load', description: 'Real-time metrics', visible: true, size: 'small' },
  { id: 'health', title: 'Health Status', description: 'Backend health overview', visible: true, size: 'medium' },
  { id: 'top-routes', title: 'Top Routes', description: 'Most accessed routes', visible: false, size: 'medium' },
  { id: 'recent-alerts', title: 'Recent Alerts', description: 'Latest system alerts', visible: false, size: 'small' },
  { id: 'geo-map', title: 'Geographic Map', description: 'Traffic by location', visible: false, size: 'large' }
]

interface WidgetCustomizerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  widgets: DashboardWidget[]
  onWidgetsChange: (widgets: DashboardWidget[]) => void
}

export function WidgetCustomizer({
  open,
  onOpenChange,
  widgets,
  onWidgetsChange
}: WidgetCustomizerProps) {
  const [localWidgets, setLocalWidgets] = useState<DashboardWidget[]>(widgets)
  const [hasChanges, setHasChanges] = useState(false)

  const handleDragEnd = useCallback((result: { source: { index: number }; destination?: { index: number } }) => {
    if (!result.destination) return

    const items = Array.from(localWidgets)
    const [reorderedItem] = items.splice(result.source.index, 1)
    items.splice(result.destination.index, 0, reorderedItem)

    setLocalWidgets(items)
    setHasChanges(true)
  }, [localWidgets])

  const toggleVisibility = useCallback((id: string) => {
    setLocalWidgets(prev =>
      prev.map(w =>
        w.id === id ? { ...w, visible: !w.visible } : w
      )
    )
    setHasChanges(true)
  }, [])

  const handleSave = () => {
    onWidgetsChange(localWidgets)
    setHasChanges(false)
    onOpenChange(false)
    toast.success('Dashboard layout saved')
  }

  const handleReset = () => {
    setLocalWidgets(defaultWidgets)
    setHasChanges(true)
    toast.info('Layout reset to defaults')
  }

  const visibleCount = localWidgets.filter(w => w.visible).length

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <LayoutGrid className="h-5 w-5" />
            Customize Dashboard
          </DialogTitle>
          <DialogDescription>
            Show, hide, and reorder dashboard widgets
          </DialogDescription>
        </DialogHeader>

        <div className="py-4">
          <div className="mb-4 flex items-center justify-between">
            <span className="text-sm text-muted-foreground">
              {visibleCount} of {localWidgets.length} widgets visible
            </span>
            <Button variant="ghost" size="sm" onClick={handleReset}>
              <RotateCcw className="mr-2 h-4 w-4" />
              Reset
            </Button>
          </div>

          <DragDropContext onDragEnd={handleDragEnd}>
            <Droppable droppableId="widgets">
              {(provided) => (
                <div
                  {...provided.droppableProps}
                  ref={provided.innerRef}
                  className="space-y-2"
                >
                  {localWidgets.map((widget, index) => (
                    <Draggable key={widget.id} draggableId={widget.id} index={index}>
                      {(provided, snapshot) => (
                        <div
                          ref={provided.innerRef}
                          {...provided.draggableProps}
                          className={cn(
                            'flex items-center gap-3 rounded-lg border bg-card p-3 transition-colors',
                            snapshot.isDragging && 'border-primary shadow-lg',
                            !widget.visible && 'opacity-60'
                          )}
                        >
                          <div {...provided.dragHandleProps} className="cursor-grab active:cursor-grabbing">
                            <GripVertical className="h-5 w-5 text-muted-foreground" />
                          </div>

                          <div className="flex-1 min-w-0">
                            <p className="font-medium">{widget.title}</p>
                            <p className="text-sm text-muted-foreground truncate">
                              {widget.description}
                            </p>
                          </div>

                          <div className="flex items-center gap-2">
                            <Badge variant="outline" className="text-xs">
                              {widget.size}
                            </Badge>
                            <Switch
                              checked={widget.visible}
                              onCheckedChange={() => toggleVisibility(widget.id)}
                            />
                          </div>
                        </div>
                      )}
                    </Draggable>
                  ))}
                  {provided.placeholder}
                </div>
              )}
            </Droppable>
          </DragDropContext>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!hasChanges}>
            <Save className="mr-2 h-4 w-4" />
            Save Changes
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

import { Badge } from '@/components/ui/badge'

interface WidgetGridProps {
  widgets: DashboardWidget[]
  children: React.ReactNode[]
}

export function WidgetGrid({ widgets, children }: WidgetGridProps) {
  const visibleWidgets = widgets.filter(w => w.visible)

  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {visibleWidgets.map((widget, index) => {
        const sizeClasses = {
          small: 'md:col-span-1',
          medium: 'md:col-span-2',
          large: 'md:col-span-3',
          full: 'md:col-span-4'
        }

        return (
          <div
            key={widget.id}
            className={cn(sizeClasses[widget.size])}
          >
            {children[index]}
          </div>
        )
      })}
    </div>
  )
}

export function useDashboardWidgets(storageKey = 'dashboard-widgets') {
  const [widgets, setWidgets] = useState<DashboardWidget[]>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem(storageKey)
      if (saved) {
        try {
          return JSON.parse(saved)
        } catch {
          return defaultWidgets
        }
      }
    }
    return defaultWidgets
  })

  const [customizerOpen, setCustomizerOpen] = useState(false)

  const updateWidgets = useCallback((newWidgets: DashboardWidget[]) => {
    setWidgets(newWidgets)
    localStorage.setItem(storageKey, JSON.stringify(newWidgets))
  }, [storageKey])

  const openCustomizer = useCallback(() => setCustomizerOpen(true), [])
  const closeCustomizer = useCallback(() => setCustomizerOpen(false), [])

  return {
    widgets,
    setWidgets: updateWidgets,
    customizerOpen,
    openCustomizer,
    closeCustomizer
  }
}

export { defaultWidgets }
