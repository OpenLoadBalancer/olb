import { useEffect, useCallback } from 'react'

type KeyCombo = string
type Handler = (e: KeyboardEvent) => void

interface ShortcutConfig {
  key: KeyCombo
  handler: Handler
  description?: string
  preventDefault?: boolean
}

// Parse key combo like "Ctrl+K" or "Cmd+Shift+P"
function matchesKeyCombo(event: KeyboardEvent, combo: string): boolean {
  const keys = combo.toLowerCase().split('+')
  const ctrlKey = keys.includes('ctrl') || keys.includes('cmd')
  const shiftKey = keys.includes('shift')
  const altKey = keys.includes('alt')
  const metaKey = keys.includes('meta') || keys.includes('cmd')

  const mainKey = keys.find(k => !['ctrl', 'cmd', 'shift', 'alt', 'meta'].includes(k))

  return (
    event.ctrlKey === ctrlKey &&
    event.shiftKey === shiftKey &&
    event.altKey === altKey &&
    event.metaKey === metaKey &&
    event.key.toLowerCase() === mainKey
  )
}

export function useKeyboardShortcuts(shortcuts: ShortcutConfig[]) {
  const handleKeyDown = useCallback((event: KeyboardEvent) => {
    // Don't trigger shortcuts when typing in inputs
    if (
      event.target instanceof HTMLInputElement ||
      event.target instanceof HTMLTextAreaElement ||
      (event.target as HTMLElement)?.isContentEditable
    ) {
      return
    }

    shortcuts.forEach(({ key, handler, preventDefault = true }) => {
      if (matchesKeyCombo(event, key)) {
        if (preventDefault) {
          event.preventDefault()
        }
        handler(event)
      }
    })
  }, [shortcuts])

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])
}

// Common shortcuts
export const commonShortcuts = {
  search: { key: 'Ctrl+K', description: 'Open search' },
  newItem: { key: 'Ctrl+N', description: 'Create new item' },
  refresh: { key: 'Ctrl+R', description: 'Refresh data' },
  darkMode: { key: 'Ctrl+Shift+D', description: 'Toggle dark mode' },
  help: { key: '?', description: 'Show keyboard shortcuts' },
  close: { key: 'Escape', description: 'Close modal/dropdown' },
  save: { key: 'Ctrl+S', description: 'Save form' },
  delete: { key: 'Delete', description: 'Delete selected' },
  selectAll: { key: 'Ctrl+A', description: 'Select all' },
  back: { key: 'Alt+Left', description: 'Go back' }
}

// Hook for specific page shortcuts
export function usePageShortcuts(pageName: string) {
  const shortcuts: Record<string, ShortcutConfig[]> = {
    dashboard: [
      { key: 'Ctrl+R', handler: () => window.location.reload(), description: 'Refresh dashboard' }
    ],
    backends: [
      { key: 'Ctrl+N', handler: () => {}, description: 'Add new backend' },
      { key: 'Ctrl+E', handler: () => {}, description: 'Export backends' }
    ],
    settings: [
      { key: 'Ctrl+S', handler: () => {}, description: 'Save settings' }
    ]
  }

  return shortcuts[pageName] || []
}

interface ShortcutHelpProps {
  shortcuts: { key: string; description: string }[]
}

export function ShortcutHelp({ shortcuts }: ShortcutHelpProps) {
  return (
    <div className="space-y-2">
      <h3 className="font-semibold">Keyboard Shortcuts</h3>
      <div className="grid gap-2">
        {shortcuts.map(({ key, description }) => (
          <div key={key} className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">{description}</span>
            <kbd className="rounded bg-muted px-2 py-1 font-mono text-xs">{key}</kbd>
          </div>
        ))}
      </div>
    </div>
  )
}
