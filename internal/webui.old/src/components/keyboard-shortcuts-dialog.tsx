import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Keyboard, Command, Navigation, FileText, Settings } from 'lucide-react'

interface Shortcut {
  key: string
  description: string
  category: 'global' | 'navigation' | 'actions' | 'forms'
}

const shortcuts: Shortcut[] = [
  // Global
  { key: 'Ctrl+K', description: 'Open command palette', category: 'global' },
  { key: '?', description: 'Show keyboard shortcuts', category: 'global' },
  { key: 'Ctrl+Shift+D', description: 'Toggle dark mode', category: 'global' },
  { key: 'Ctrl+Shift+N', description: 'Open notifications', category: 'global' },
  { key: 'Escape', description: 'Close modal/dropdown', category: 'global' },

  // Navigation
  { key: 'G D', description: 'Go to Dashboard', category: 'navigation' },
  { key: 'G B', description: 'Go to Backends', category: 'navigation' },
  { key: 'G P', description: 'Go to Pools', category: 'navigation' },
  { key: 'G R', description: 'Go to Routes', category: 'navigation' },
  { key: 'G L', description: 'Go to Listeners', category: 'navigation' },
  { key: 'G S', description: 'Go to Settings', category: 'navigation' },
  { key: 'Alt+Left', description: 'Go back', category: 'navigation' },
  { key: 'Alt+Right', description: 'Go forward', category: 'navigation' },

  // Actions
  { key: 'Ctrl+R', description: 'Refresh data', category: 'actions' },
  { key: 'Ctrl+N', description: 'Create new item', category: 'actions' },
  { key: 'Ctrl+F', description: 'Search/Filter', category: 'actions' },
  { key: 'Ctrl+A', description: 'Select all', category: 'actions' },
  { key: 'Delete', description: 'Delete selected', category: 'actions' },
  { key: 'Ctrl+E', description: 'Export', category: 'actions' },

  // Forms
  { key: 'Ctrl+S', description: 'Save form', category: 'forms' },
  { key: 'Ctrl+Enter', description: 'Submit form', category: 'forms' },
  { key: 'Tab', description: 'Next field', category: 'forms' },
  { key: 'Shift+Tab', description: 'Previous field', category: 'forms' }
]

const categories = {
  global: { label: 'Global', icon: Command, color: 'text-blue-500' },
  navigation: { label: 'Navigation', icon: Navigation, color: 'text-green-500' },
  actions: { label: 'Actions', icon: FileText, color: 'text-purple-500' },
  forms: { label: 'Forms', icon: Settings, color: 'text-orange-500' }
}

interface KeyboardShortcutsDialogProps {
  open?: boolean
  onOpenChange?: (open: boolean) => void
  trigger?: React.ReactNode
}

export function KeyboardShortcutsDialog({
  open,
  onOpenChange,
  trigger
}: KeyboardShortcutsDialogProps) {
  const shortcutsByCategory = shortcuts.reduce((acc, shortcut) => {
    if (!acc[shortcut.category]) acc[shortcut.category] = []
    acc[shortcut.category].push(shortcut)
    return acc
  }, {} as Record<string, Shortcut[]>)

  const content = (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          <Keyboard className="h-5 w-5" />
          Keyboard Shortcuts
        </DialogTitle>
        <DialogDescription>
          Use these keyboard shortcuts to navigate and control the application faster.
        </DialogDescription>
      </DialogHeader>
      <ScrollArea className="h-[400px] pr-4">
        <div className="space-y-6">
          {Object.entries(shortcutsByCategory).map(([category, categoryShortcuts]) => {
            const config = categories[category as keyof typeof categories]
            const Icon = config.icon
            return (
              <div key={category}>
                <div className="mb-3 flex items-center gap-2">
                  <Icon className={`h-4 w-4 ${config.color}`} />
                  <h3 className="font-semibold capitalize">{config.label}</h3>
                </div>
                <div className="space-y-2">
                  {categoryShortcuts.map((shortcut, index) => (
                    <div
                      key={index}
                      className="flex items-center justify-between rounded-lg border p-3"
                    >
                      <span className="text-sm text-muted-foreground">
                        {shortcut.description}
                      </span>
                      <div className="flex items-center gap-1">
                        {shortcut.key.split(' ').map((key, i) => (
                          <kbd
                            key={i}
                            className="rounded border bg-muted px-2 py-1 font-mono text-xs"
                          >
                            {key}
                          </kbd>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      </ScrollArea>
    </>
  )

  if (trigger) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogTrigger asChild>{trigger}</DialogTrigger>
        <DialogContent className="max-w-2xl">{content}</DialogContent>
      </Dialog>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">{content}</DialogContent>
    </Dialog>
  )
}

// Hook for keyboard shortcuts dialog
export function useKeyboardShortcutsDialog() {
  const [open, setOpen] = useState(false)
  return { open, setOpen }
}

import { useState } from 'react'
