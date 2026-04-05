import { NavLink, useLocation } from 'react-router'
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
  Menu,
  X,
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
  Terminal,
  Clock,
  Wrench,
  Trash2
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from '@/components/ui/tooltip'
import { useState } from 'react'

const navItems = [
  { path: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { path: '/backends', label: 'Backends', icon: Server },
  { path: '/pools', label: 'Pools', icon: Layers },
  { path: '/routes', label: 'Routes', icon: Route },
  { path: '/listeners', label: 'Listeners', icon: Radio },
  { path: '/middleware', label: 'Middleware', icon: ScrollText },
  { path: '/waf', label: 'WAF', icon: Shield },
  { path: '/rate-limit', label: 'Rate Limit', icon: Gauge },
  { path: '/certificates', label: 'Certificates', icon: Lock },
  { path: '/cluster', label: 'Cluster', icon: Users },
  { path: '/discovery', label: 'Discovery', icon: Search },
  { path: '/plugins', label: 'Plugins', icon: Puzzle },
  { path: '/mcp', label: 'MCP', icon: Bot },
  { path: '/analytics', label: 'Analytics', icon: BarChart3 },
  { path: '/metrics', label: 'Metrics', icon: LineChart },
  { path: '/health', label: 'Health', icon: HeartPulse },
  { path: '/cache', label: 'Cache', icon: Database },
  { path: '/tasks', label: 'Tasks', icon: Clock },
  { path: '/backup', label: 'Backup', icon: Database },
  { path: '/maintenance', label: 'Maintenance', icon: Wrench },
  { path: '/profiler', label: 'Profiler', icon: Activity },
  { path: '/logs', label: 'Logs', icon: FileText },
  { path: '/console', label: 'Console', icon: Terminal },
  { path: '/audit', label: 'Audit Logs', icon: FileSearch },
  { path: '/diagnostics', label: 'Diagnostics', icon: Zap },
  { path: '/users', label: 'Users', icon: UserCog },
  { path: '/notifications', label: 'Notifications', icon: Bell },
  { path: '/appearance', label: 'Appearance', icon: Palette },
  { path: '/import-export', label: 'Import/Export', icon: Upload },
  { path: '/settings', label: 'Settings', icon: Settings }
]

export function Sidebar() {
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <TooltipProvider delayDuration={0}>
      {/* Mobile Menu Button */}
      <Button
        variant="ghost"
        size="icon"
        className="fixed left-4 top-4 z-50 lg:hidden"
        onClick={() => setMobileOpen(!mobileOpen)}
      >
        {mobileOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
      </Button>

      {/* Mobile Overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 lg:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 w-64 transform border-r bg-card transition-transform duration-200 ease-in-out lg:static lg:transform-none',
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex h-full flex-col">
          {/* Logo */}
          <div className="flex h-16 items-center border-b px-6">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary">
                <Layers className="h-5 w-5 text-primary-foreground" />
              </div>
              <span className="text-lg font-bold">OpenLB</span>
            </div>
          </div>

          {/* Navigation */}
          <nav className="flex-1 overflow-auto py-4 px-3" data-tour="sidebar">
            <ul className="space-y-1">
              {navItems.map((item) => {
                const Icon = item.icon
                const isActive = location.pathname === item.path

                return (
                  <li key={item.path}>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <NavLink
                          to={item.path}
                          onClick={() => setMobileOpen(false)}
                          className={cn(
                            'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors',
                            isActive
                              ? 'bg-primary text-primary-foreground'
                              : 'text-muted-foreground hover:bg-accent hover:text-foreground'
                          )}
                        >
                          <Icon className="h-5 w-5" />
                          {item.label}
                        </NavLink>
                      </TooltipTrigger>
                      <TooltipContent side="right" className="lg:hidden">
                        {item.label}
                      </TooltipContent>
                    </Tooltip>
                  </li>
                )
              })}
            </ul>
          </nav>

          {/* Version */}
          <div className="border-t p-4">
            <p className="text-xs text-muted-foreground">OpenLoadBalancer v1.0</p>
          </div>
        </div>
      </aside>
    </TooltipProvider>
  )
}
