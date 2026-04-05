import { useState, useEffect, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { toast } from 'sonner'
import { cn, formatDistanceToNow } from '@/lib/utils'
import {
  Bell,
  CheckCircle2,
  AlertTriangle,
  Info,
  X,
  Settings,
  Trash2,
  Mail,
  MessageSquare,
  Smartphone,
  Filter,
  CheckCheck,
  AlertOctagon
} from 'lucide-react'

interface Notification {
  id: string
  title: string
  message: string
  type: 'info' | 'warning' | 'error' | 'success'
  category: 'system' | 'security' | 'performance' | 'health'
  read: boolean
  timestamp: Date
  actions?: { label: string; action: string }[]
}

interface NotificationSettings {
  email: boolean
  push: boolean
  slack: boolean
  categories: {
    system: boolean
    security: boolean
    performance: boolean
    health: boolean
  }
}

const mockNotifications: Notification[] = [
  {
    id: '1',
    title: 'Backend Health Alert',
    message: 'Backend "web-server-03" is down and has been removed from rotation',
    type: 'error',
    category: 'health',
    read: false,
    timestamp: new Date(Date.now() - 300000),
    actions: [{ label: 'View Details', action: 'view-backend' }]
  },
  {
    id: '2',
    title: 'High CPU Usage',
    message: 'Pool "api-pool" is experiencing high CPU usage (89%)',
    type: 'warning',
    category: 'performance',
    read: false,
    timestamp: new Date(Date.now() - 600000),
    actions: [{ label: 'View Metrics', action: 'view-metrics' }]
  },
  {
    id: '3',
    title: 'Certificate Expiring',
    message: 'SSL Certificate for "api.openloadbalancer.dev" expires in 7 days',
    type: 'warning',
    category: 'security',
    read: true,
    timestamp: new Date(Date.now() - 3600000),
    actions: [{ label: 'Renew', action: 'renew-cert' }]
  },
  {
    id: '4',
    title: 'WAF Rule Triggered',
    message: 'Rate limiting rule triggered by IP 192.168.1.100',
    type: 'info',
    category: 'security',
    read: true,
    timestamp: new Date(Date.now() - 7200000)
  },
  {
    id: '5',
    title: 'Configuration Backup Complete',
    message: 'Automatic backup completed successfully. Size: 245MB',
    type: 'success',
    category: 'system',
    read: true,
    timestamp: new Date(Date.now() - 86400000)
  },
  {
    id: '6',
    title: 'New Version Available',
    message: 'OpenLoadBalancer v1.2.0 is available for upgrade',
    type: 'info',
    category: 'system',
    read: true,
    timestamp: new Date(Date.now() - 172800000),
    actions: [{ label: 'View Release Notes', action: 'view-release' }]
  }
]

const typeIcons = {
  info: Info,
  warning: AlertTriangle,
  error: AlertOctagon,
  success: CheckCircle2
}

const typeColors = {
  info: 'text-blue-500 bg-blue-500/10',
  warning: 'text-yellow-500 bg-yellow-500/10',
  error: 'text-red-500 bg-red-500/10',
  success: 'text-green-500 bg-green-500/10'
}

const categoryColors = {
  system: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300',
  security: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
  performance: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300',
  health: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300'
}

export function NotificationCenter() {
  const [notifications, setNotifications] = useState<Notification[]>(mockNotifications)
  const [activeTab, setActiveTab] = useState('all')
  const [settings, setSettings] = useState<NotificationSettings>({
    email: true,
    push: false,
    slack: true,
    categories: {
      system: true,
      security: true,
      performance: true,
      health: true
    }
  })

  const unreadCount = notifications.filter(n => !n.read).length

  const filteredNotifications = notifications.filter(n => {
    if (activeTab === 'unread') return !n.read
    if (activeTab === 'read') return n.read
    return true
  })

  const markAsRead = useCallback((id: string) => {
    setNotifications(prev =>
      prev.map(n => (n.id === id ? { ...n, read: true } : n))
    )
  }, [])

  const markAllAsRead = useCallback(() => {
    setNotifications(prev => prev.map(n => ({ ...n, read: true })))
    toast.success('All notifications marked as read')
  }, [])

  const deleteNotification = useCallback((id: string) => {
    setNotifications(prev => prev.filter(n => n.id !== id))
    toast.success('Notification deleted')
  }, [])

  const clearAll = useCallback(() => {
    setNotifications([])
    toast.success('All notifications cleared')
  }, [])

  const handleAction = useCallback((action: string) => {
    toast.info(`Action: ${action}`)
  }, [])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Notifications</h1>
          <p className="text-muted-foreground">
            Manage your system notifications and alerts
          </p>
        </div>
        <div className="flex items-center gap-2">
          {unreadCount > 0 && (
            <Button variant="outline" onClick={markAllAsRead}>
              <CheckCheck className="mr-2 h-4 w-4" />
              Mark All Read
            </Button>
          )}
          <Button variant="outline" onClick={clearAll}>
            <Trash2 className="mr-2 h-4 w-4" />
            Clear All
          </Button>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Notifications List */}
        <div className="lg:col-span-2">
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="flex items-center gap-2">
                    <Bell className="h-5 w-5" />
                    Notifications
                    {unreadCount > 0 && (
                      <Badge variant="secondary">{unreadCount} unread</Badge>
                    )}
                  </CardTitle>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <Tabs value={activeTab} onValueChange={setActiveTab}>
                <TabsList className="mb-4">
                  <TabsTrigger value="all">All</TabsTrigger>
                  <TabsTrigger value="unread">Unread</TabsTrigger>
                  <TabsTrigger value="read">Read</TabsTrigger>
                </TabsList>

                <TabsContent value={activeTab} className="mt-0">
                  <ScrollArea className="h-[500px]">
                    {filteredNotifications.length === 0 ? (
                      <div className="flex h-32 flex-col items-center justify-center text-muted-foreground">
                        <Bell className="mb-2 h-8 w-8 opacity-20" />
                        <p>No notifications</p>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {filteredNotifications.map(notification => {
                          const Icon = typeIcons[notification.type]
                          return (
                            <div
                              key={notification.id}
                              className={cn(
                                'group relative rounded-lg border p-4 transition-colors hover:bg-muted/50',
                                !notification.read && 'bg-muted/30 border-primary/20'
                              )}
                            >
                              <div className="flex items-start gap-3">
                                <div className={cn('rounded-full p-2', typeColors[notification.type])}>
                                  <Icon className="h-4 w-4" />
                                </div>
                                <div className="flex-1 min-w-0">
                                  <div className="flex items-start justify-between gap-2">
                                    <div>
                                      <p className="font-medium">{notification.title}</p>
                                      <p className="text-sm text-muted-foreground mt-0.5">
                                        {notification.message}
                                      </p>
                                    </div>
                                    <div className="flex items-center gap-1">
                                      {!notification.read && (
                                        <Button
                                          variant="ghost"
                                          size="icon"
                                          className="h-6 w-6 opacity-0 group-hover:opacity-100"
                                          onClick={() => markAsRead(notification.id)}
                                        >
                                          <CheckCircle2 className="h-3 w-3" />
                                        </Button>
                                      )}
                                      <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-6 w-6 opacity-0 group-hover:opacity-100"
                                        onClick={() => deleteNotification(notification.id)}
                                      >
                                        <X className="h-3 w-3" />
                                      </Button>
                                    </div>
                                  </div>
                                  <div className="mt-2 flex items-center gap-2">
                                    <Badge
                                      variant="outline"
                                      className={cn('text-xs', categoryColors[notification.category])}
                                    >
                                      {notification.category}
                                    </Badge>
                                    <span className="text-xs text-muted-foreground">
                                      {formatDistanceToNow(notification.timestamp)}
                                    </span>
                                  </div>
                                  {notification.actions && (
                                    <div className="mt-2 flex gap-2">
                                      {notification.actions.map((action, idx) => (
                                        <Button
                                          key={idx}
                                          variant="outline"
                                          size="sm"
                                          onClick={() => handleAction(action.action)}
                                        >
                                          {action.label}
                                        </Button>
                                      ))}
                                    </div>
                                  )}
                                </div>
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </ScrollArea>
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>
        </div>

        {/* Settings */}
        <div>
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Settings className="h-5 w-5" />
                Notification Settings
              </CardTitle>
              <CardDescription>Configure how you receive notifications</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-4">
                <h4 className="text-sm font-medium">Channels</h4>
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Mail className="h-4 w-4 text-muted-foreground" />
                      <Label>Email</Label>
                    </div>
                    <Switch
                      checked={settings.email}
                      onCheckedChange={(v) => setSettings(s => ({ ...s, email: v }))}
                    />
                  </div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Smartphone className="h-4 w-4 text-muted-foreground" />
                      <Label>Push</Label>
                    </div>
                    <Switch
                      checked={settings.push}
                      onCheckedChange={(v) => setSettings(s => ({ ...s, push: v }))}
                    />
                  </div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <MessageSquare className="h-4 w-4 text-muted-foreground" />
                      <Label>Slack</Label>
                    </div>
                    <Switch
                      checked={settings.slack}
                      onCheckedChange={(v) => setSettings(s => ({ ...s, slack: v }))}
                    />
                  </div>
                </div>
              </div>

              <div className="space-y-4">
                <h4 className="text-sm font-medium">Categories</h4>
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline" className={categoryColors.system}>System</Badge>
                    </div>
                    <Switch
                      checked={settings.categories.system}
                      onCheckedChange={(v) =>
                        setSettings(s => ({ ...s, categories: { ...s.categories, system: v } }))
                      }
                    />
                  </div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline" className={categoryColors.security}>Security</Badge>
                    </div>
                    <Switch
                      checked={settings.categories.security}
                      onCheckedChange={(v) =>
                        setSettings(s => ({ ...s, categories: { ...s.categories, security: v } }))
                      }
                    />
                  </div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline" className={categoryColors.performance}>Performance</Badge>
                    </div>
                    <Switch
                      checked={settings.categories.performance}
                      onCheckedChange={(v) =>
                        setSettings(s => ({ ...s, categories: { ...s.categories, performance: v } }))
                      }
                    />
                  </div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline" className={categoryColors.health}>Health</Badge>
                    </div>
                    <Switch
                      checked={settings.categories.health}
                      onCheckedChange={(v) =>
                        setSettings(s => ({ ...s, categories: { ...s.categories, health: v } }))
                      }
                    />
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
