import { useState } from 'react'
import {
  Bell,
  Check,
  Info,
  AlertTriangle,
  AlertCircle,
  X,
  Settings,
  Trash2
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

export interface Notification {
  id: string
  title: string
  message: string
  type: 'info' | 'success' | 'warning' | 'error'
  timestamp: Date
  read: boolean
  action?: {
    label: string
    onClick: () => void
  }
}

interface NotificationCenterProps {
  notifications: Notification[]
  onMarkAsRead: (id: string) => void
  onMarkAllAsRead: () => void
  onDismiss: (id: string) => void
  onClearAll: () => void
  className?: string
}

const icons = {
  info: Info,
  success: Check,
  warning: AlertTriangle,
  error: AlertCircle
}

const iconColors = {
  info: 'text-blue-500 bg-blue-50 dark:bg-blue-900/20',
  success: 'text-green-500 bg-green-50 dark:bg-green-900/20',
  warning: 'text-amber-500 bg-amber-50 dark:bg-amber-900/20',
  error: 'text-red-500 bg-red-50 dark:bg-red-900/20'
}

export function NotificationCenter({
  notifications,
  onMarkAsRead,
  onMarkAllAsRead,
  onDismiss,
  onClearAll,
  className
}: NotificationCenterProps) {
  const [open, setOpen] = useState(false)
  const unreadCount = notifications.filter((n) => !n.read).length

  const handleMarkAsRead = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    onMarkAsRead(id)
  }

  const handleDismiss = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    onDismiss(id)
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className={cn('relative', className)}>
          <Bell className="h-5 w-5" />
          {unreadCount > 0 && (
            <span className="absolute right-1 top-1 flex h-4 w-4 items-center justify-center rounded-full bg-destructive text-[10px] font-medium text-destructive-foreground">
              {unreadCount > 99 ? '99+' : unreadCount}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[400px]" forceMount>
        <div className="flex items-center justify-between px-3 py-2">
          <DropdownMenuLabel className="font-semibold">Notifications</DropdownMenuLabel>
          <div className="flex items-center gap-1">
            {unreadCount > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="h-8 text-xs"
                onClick={onMarkAllAsRead}
              >
                Mark all read
              </Button>
            )}
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={onClearAll}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        </div>
        <DropdownMenuSeparator />

        {notifications.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <Bell className="h-8 w-8 text-muted-foreground/50" />
            <p className="mt-2 text-sm text-muted-foreground">No notifications</p>
          </div>
        ) : (
          <ScrollArea className="h-[400px]">
            <div className="divide-y">
              {notifications.map((notification) => {
                const Icon = icons[notification.type]
                const colorClass = iconColors[notification.type]

                return (
                  <div
                    key={notification.id}
                    className={cn(
                      'relative flex gap-3 p-3 transition-colors hover:bg-accent cursor-pointer',
                      !notification.read && 'bg-accent/50'
                    )}
                    onClick={() => {
                      if (!notification.read) {
                        onMarkAsRead(notification.id)
                      }
                      if (notification.action) {
                        notification.action.onClick()
                      }
                      setOpen(false)
                    }}
                  >
                    <div
                      className={cn(
                        'flex h-8 w-8 shrink-0 items-center justify-center rounded-full',
                        colorClass
                      )}
                    >
                      <Icon className="h-4 w-4" />
                    </div>
                    <div className="flex-1 min-w-0">
                      <p className={cn('text-sm font-medium', !notification.read && 'font-semibold')}>
                        {notification.title}
                      </p>
                      <p className="text-xs text-muted-foreground line-clamp-2">
                        {notification.message}
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {formatTimeAgo(notification.timestamp)}
                      </p>
                      {notification.action && (
                        <Button
                          variant="link"
                          size="sm"
                          className="h-auto p-0 mt-1"
                          onClick={(e) => {
                            e.stopPropagation()
                            notification.action?.onClick()
                            setOpen(false)
                          }}
                        >
                          {notification.action.label}
                        </Button>
                      )}
                    </div>
                    <div className="flex flex-col gap-1">
                      {!notification.read && (
                        <button
                          className="rounded-full p-1 hover:bg-accent"
                          onClick={(e) => handleMarkAsRead(notification.id, e)}
                          title="Mark as read"
                        >
                          <div className="h-2 w-2 rounded-full bg-primary" />
                        </button>
                      )}
                      <button
                        className="rounded-full p-1 hover:bg-accent text-muted-foreground"
                        onClick={(e) => handleDismiss(notification.id, e)}
                        title="Dismiss"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          </ScrollArea>
        )}

        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Button variant="ghost" className="w-full justify-center">
            <Settings className="mr-2 h-4 w-4" />
            Notification Settings
          </Button>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function formatTimeAgo(date: Date): string {
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 1) return 'Just now'
  if (minutes < 60) return `${minutes}m ago`
  if (hours < 24) return `${hours}h ago`
  if (days < 7) return `${days}d ago`
  return date.toLocaleDateString()
}

// Hook for managing notifications
import { useCallback, useState as useReactState } from 'react'

export function useNotifications() {
  const [notifications, setNotifications] = useReactState<Notification[]>([])

  const addNotification = useCallback((notification: Omit<Notification, 'id' | 'timestamp' | 'read'>) => {
    const newNotification: Notification = {
      ...notification,
      id: Math.random().toString(36).substring(7),
      timestamp: new Date(),
      read: false
    }
    setNotifications((prev) => [newNotification, ...prev])
    return newNotification.id
  }, [])

  const markAsRead = useCallback((id: string) => {
    setNotifications((prev) =>
      prev.map((n) => (n.id === id ? { ...n, read: true } : n))
    )
  }, [])

  const markAllAsRead = useCallback(() => {
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })))
  }, [])

  const dismiss = useCallback((id: string) => {
    setNotifications((prev) => prev.filter((n) => n.id !== id))
  }, [])

  const clearAll = useCallback(() => {
    setNotifications([])
  }, [])

  return {
    notifications,
    addNotification,
    markAsRead,
    markAllAsRead,
    dismiss,
    clearAll
  }
}
