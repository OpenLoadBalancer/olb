import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator
} from '@/components/ui/command'
import { DialogTitle } from '@/components/ui/dialog'
import { VisuallyHidden } from '@radix-ui/react-visually-hidden'
import {
  LayoutDashboard,
  Server,
  Layers,
  Route,
  Radio,
  Shield,
  FileText,
  Settings,
  ScrollText,
  Lock,
  Users,
  Search,
  Puzzle,
  Bot,
  BarChart3,
  Database,
  Activity,
  Bell,
  Palette,
  Upload,
  UserCog,
  FileSearch,
  Zap,
  Gauge,
  HeartPulse,
  LineChart,
  LogOut,
  Moon,
  Sun,
  Keyboard,
  HelpCircle,
  BookOpen,
  Terminal,
  Play,
  Pause,
  RefreshCw,
  Globe,
  AlertCircle,
  CheckCircle2
} from 'lucide-react'
import { toast } from 'sonner'

interface SearchResult {
  id: string
  type: 'page' | 'backend' | 'pool' | 'route' | 'action' | 'setting'
  title: string
  subtitle?: string
  icon: React.ElementType
  action: () => void
  keywords?: string[]
}

export function GlobalSearch() {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()

  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setOpen((open) => !open)
      }
    }
    document.addEventListener('keydown', down)
    return () => document.removeEventListener('keydown', down)
  }, [])

  const runAction = (action: () => void) => {
    setOpen(false)
    action()
  }

  const toggleTheme = () => {
    const html = document.documentElement
    if (html.classList.contains('dark')) {
      html.classList.remove('dark')
      toast.success('Switched to light mode')
    } else {
      html.classList.add('dark')
      toast.success('Switched to dark mode')
    }
  }

  const quickActions: SearchResult[] = [
    {
      id: 'reload',
      type: 'action',
      title: 'Reload Configuration',
      subtitle: 'Apply latest configuration changes',
      icon: RefreshCw,
      action: () => toast.success('Configuration reloaded')
    },
    {
      id: 'maintenance-on',
      type: 'action',
      title: 'Enable Maintenance Mode',
      subtitle: 'Put system into maintenance',
      icon: Pause,
      action: () => toast.success('Maintenance mode enabled')
    },
    {
      id: 'maintenance-off',
      type: 'action',
      title: 'Disable Maintenance Mode',
      subtitle: 'Resume normal operation',
      icon: Play,
      action: () => toast.success('Maintenance mode disabled')
    },
    {
      id: 'clear-cache',
      type: 'action',
      title: 'Clear Cache',
      subtitle: 'Clear all cached data',
      icon: Database,
      action: () => toast.success('Cache cleared')
    },
    {
      id: 'toggle-theme',
      type: 'action',
      title: 'Toggle Theme',
      subtitle: 'Switch between light and dark mode',
      icon: Sun,
      action: toggleTheme
    }
  ]

  const pages: SearchResult[] = [
    { id: 'dashboard', type: 'page', title: 'Dashboard', icon: LayoutDashboard, action: () => navigate('/dashboard'), keywords: ['home', 'overview'] },
    { id: 'backends', type: 'page', title: 'Backends', icon: Server, action: () => navigate('/backends'), keywords: ['server', 'target'] },
    { id: 'pools', type: 'page', title: 'Pools', icon: Layers, action: () => navigate('/pools'), keywords: ['group', 'collection'] },
    { id: 'routes', type: 'page', title: 'Routes', icon: Route, action: () => navigate('/routes'), keywords: ['path', 'url'] },
    { id: 'listeners', type: 'page', title: 'Listeners', icon: Radio, action: () => navigate('/listeners'), keywords: ['port', 'address'] },
    { id: 'middleware', type: 'page', title: 'Middleware', icon: ScrollText, action: () => navigate('/middleware'), keywords: ['chain', 'filter'] },
    { id: 'waf', type: 'page', title: 'WAF', icon: Shield, action: () => navigate('/waf'), keywords: ['firewall', 'security'] },
    { id: 'rate-limit', type: 'page', title: 'Rate Limit', icon: Gauge, action: () => navigate('/rate-limit'), keywords: ['throttle', 'limit'] },
    { id: 'certificates', type: 'page', title: 'Certificates', icon: Lock, action: () => navigate('/certificates'), keywords: ['tls', 'ssl'] },
    { id: 'cluster', type: 'page', title: 'Cluster', icon: Users, action: () => navigate('/cluster'), keywords: ['nodes', 'raft'] },
    { id: 'discovery', type: 'page', title: 'Discovery', icon: Search, action: () => navigate('/discovery'), keywords: ['dns', 'consul'] },
    { id: 'plugins', type: 'page', title: 'Plugins', icon: Puzzle, action: () => navigate('/plugins'), keywords: ['extension', 'addon'] },
    { id: 'mcp', type: 'page', title: 'MCP', icon: Bot, action: () => navigate('/mcp'), keywords: ['ai', 'claude'] },
    { id: 'analytics', type: 'page', title: 'Analytics', icon: BarChart3, action: () => navigate('/analytics'), keywords: ['reports', 'stats'] },
    { id: 'metrics', type: 'page', title: 'Metrics', icon: LineChart, action: () => navigate('/metrics'), keywords: ['monitoring', 'prometheus'] },
    { id: 'health', type: 'page', title: 'Health', icon: HeartPulse, action: () => navigate('/health'), keywords: ['checks', 'status'] },
    { id: 'backup', type: 'page', title: 'Backup', icon: Database, action: () => navigate('/backup'), keywords: ['restore', 'snapshot'] },
    { id: 'profiler', type: 'page', title: 'Profiler', icon: Activity, action: () => navigate('/profiler'), keywords: ['performance', 'pprof'] },
    { id: 'logs', type: 'page', title: 'Logs', icon: FileText, action: () => navigate('/logs'), keywords: ['logging', 'traces'] },
    { id: 'audit', type: 'page', title: 'Audit Logs', icon: FileSearch, action: () => navigate('/audit'), keywords: ['history', 'events'] },
    { id: 'diagnostics', type: 'page', title: 'Diagnostics', icon: Zap, action: () => navigate('/diagnostics'), keywords: ['troubleshoot', 'repair'] },
    { id: 'users', type: 'page', title: 'Users', icon: UserCog, action: () => navigate('/users'), keywords: ['accounts', 'permissions'] },
    { id: 'notifications', type: 'page', title: 'Notifications', icon: Bell, action: () => navigate('/notifications'), keywords: ['alerts', 'messages'] },
    { id: 'appearance', type: 'page', title: 'Appearance', icon: Palette, action: () => navigate('/appearance'), keywords: ['theme', 'style'] },
    { id: 'import-export', type: 'page', title: 'Import/Export', icon: Upload, action: () => navigate('/import-export'), keywords: ['backup', 'config'] },
    { id: 'settings', type: 'page', title: 'Settings', icon: Settings, action: () => navigate('/settings'), keywords: ['config', 'preferences'] }
  ]

  const dataItems: SearchResult[] = [
    { id: 'backend-1', type: 'backend', title: 'web-server-01', subtitle: '10.0.0.10:8080', icon: Server, action: () => navigate('/backends') },
    { id: 'backend-2', type: 'backend', title: 'web-server-02', subtitle: '10.0.0.11:8080', icon: Server, action: () => navigate('/backends') },
    { id: 'backend-3', type: 'backend', title: 'api-server-01', subtitle: '10.0.0.20:3000', icon: Server, action: () => navigate('/backends') },
    { id: 'pool-1', type: 'pool', title: 'web-pool', subtitle: '5 backends', icon: Layers, action: () => navigate('/pools') },
    { id: 'pool-2', type: 'pool', title: 'api-pool', subtitle: '3 backends', icon: Layers, action: () => navigate('/pools') },
    { id: 'route-1', type: 'route', title: '/api/*', subtitle: '→ api-pool', icon: Route, action: () => navigate('/routes') },
    { id: 'route-2', type: 'route', title: '/*', subtitle: '→ web-pool', icon: Route, action: () => navigate('/routes') }
  ]

  const settings: SearchResult[] = [
    { id: 'settings-general', type: 'setting', title: 'General Settings', icon: Settings, action: () => navigate('/settings') },
    { id: 'settings-security', type: 'setting', title: 'Security Settings', icon: Shield, action: () => navigate('/settings') },
    { id: 'settings-network', type: 'setting', title: 'Network Settings', icon: Globe, action: () => navigate('/settings') },
    { id: 'settings-logs', type: 'setting', title: 'Log Configuration', icon: FileText, action: () => navigate('/settings') }
  ]

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <VisuallyHidden>
        <DialogTitle>Search</DialogTitle>
      </VisuallyHidden>
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>

        <CommandGroup heading="Quick Actions">
          {quickActions.map((item) => (
            <CommandItem
              key={item.id}
              onSelect={() => runAction(item.action)}
              keywords={item.keywords}
            >
              <item.icon className="mr-2 h-4 w-4" />
              <div className="flex flex-col">
                <span>{item.title}</span>
                {item.subtitle && (
                  <span className="text-xs text-muted-foreground">{item.subtitle}</span>
                )}
              </div>
            </CommandItem>
          ))}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Pages">
          {pages.map((item) => (
            <CommandItem
              key={item.id}
              onSelect={() => runAction(item.action)}
              keywords={item.keywords}
            >
              <item.icon className="mr-2 h-4 w-4" />
              <span>{item.title}</span>
            </CommandItem>
          ))}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Resources">
          {dataItems.map((item) => (
            <CommandItem
              key={item.id}
              onSelect={() => runAction(item.action)}
            >
              <item.icon className="mr-2 h-4 w-4" />
              <div className="flex flex-col">
                <span>{item.title}</span>
                {item.subtitle && (
                  <span className="text-xs text-muted-foreground">{item.subtitle}</span>
                )}
              </div>
              <Badge variant="secondary" className="ml-auto text-xs capitalize">
                {item.type}
              </Badge>
            </CommandItem>
          ))}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Settings">
          {settings.map((item) => (
            <CommandItem
              key={item.id}
              onSelect={() => runAction(item.action)}
            >
              <item.icon className="mr-2 h-4 w-4" />
              <span>{item.title}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}

import { Badge } from '@/components/ui/badge'
