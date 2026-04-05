import { useState, useEffect, useCallback, useRef } from 'react'
import { toast } from 'sonner'

interface UseAutoSaveOptions<T> {
  data: T
  onSave: (data: T) => Promise<void>
  delay?: number
  enabled?: boolean
  key?: string
}

interface AutoSaveState {
  isSaving: boolean
  lastSaved: Date | null
  hasChanges: boolean
  error: Error | null
}

export function useAutoSave<T>({
  data,
  onSave,
  delay = 2000,
  enabled = true,
  key
}: UseAutoSaveOptions<T>) {
  const [state, setState] = useState<AutoSaveState>({
    isSaving: false,
    lastSaved: null,
    hasChanges: false,
    error: null
  })

  const timeoutRef = useRef<NodeJS.Timeout>()
  const previousDataRef = useRef<T>(data)

  // Save to localStorage as backup
  useEffect(() => {
    if (key && enabled) {
      localStorage.setItem(`autosave-${key}`, JSON.stringify(data))
    }
  }, [data, key, enabled])

  // Load from localStorage on mount
  useEffect(() => {
    if (key && enabled) {
      const saved = localStorage.getItem(`autosave-${key}`)
      if (saved) {
        try {
          const parsed = JSON.parse(saved)
          // You might want to restore this data somehow
          console.log('[AutoSave] Restored from localStorage:', parsed)
        } catch {
          localStorage.removeItem(`autosave-${key}`)
        }
      }
    }
  }, [key, enabled])

  const save = useCallback(async () => {
    if (!enabled) return

    setState(prev => ({ ...prev, isSaving: true, error: null }))

    try {
      await onSave(data)
      setState({
        isSaving: false,
        lastSaved: new Date(),
        hasChanges: false,
        error: null
      })
      previousDataRef.current = data
    } catch (error) {
      setState(prev => ({
        ...prev,
        isSaving: false,
        error: error as Error
      }))
      toast.error('Auto-save failed')
    }
  }, [data, onSave, enabled])

  const saveImmediately = useCallback(async () => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current)
    }
    await save()
  }, [save])

  // Trigger auto-save when data changes
  useEffect(() => {
    if (!enabled) return

    const hasChanges = JSON.stringify(data) !== JSON.stringify(previousDataRef.current)
    setState(prev => ({ ...prev, hasChanges }))

    if (hasChanges) {
      timeoutRef.current = setTimeout(() => {
        save()
      }, delay)
    }

    return () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current)
      }
    }
  }, [data, delay, enabled, save])

  // Clear localStorage on successful manual save
  const clearDraft = useCallback(() => {
    if (key) {
      localStorage.removeItem(`autosave-${key}`)
    }
  }, [key])

  return {
    ...state,
    save: saveImmediately,
    clearDraft
  }
}

// Component to show auto-save status
import { Cloud, Check, AlertCircle, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

interface AutoSaveIndicatorProps {
  isSaving: boolean
  lastSaved: Date | null
  hasChanges: boolean
  error: Error | null
  className?: string
}

export function AutoSaveIndicator({
  isSaving,
  lastSaved,
  hasChanges,
  error,
  className
}: AutoSaveIndicatorProps) {
  const getStatus = () => {
    if (error) {
      return {
        icon: AlertCircle,
        text: 'Save failed',
        className: 'text-destructive'
      }
    }
    if (isSaving) {
      return {
        icon: Loader2,
        text: 'Saving...',
        className: 'text-muted-foreground animate-spin'
      }
    }
    if (lastSaved) {
      return {
        icon: Check,
        text: `Saved ${formatTimeAgo(lastSaved)}`,
        className: 'text-green-500'
      }
    }
    if (hasChanges) {
      return {
        icon: Cloud,
        text: 'Unsaved changes',
        className: 'text-amber-500'
      }
    }
    return {
      icon: Check,
      text: 'All changes saved',
      className: 'text-muted-foreground'
    }
  }

  const status = getStatus()
  const Icon = status.icon

  return (
    <div className={cn('flex items-center gap-2 text-sm', className)}>
      <Icon className={cn('h-4 w-4', status.className)} />
      <span className={status.className}>{status.text}</span>
    </div>
  )
}

function formatTimeAgo(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000)
  if (seconds < 5) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return date.toLocaleDateString()
}
