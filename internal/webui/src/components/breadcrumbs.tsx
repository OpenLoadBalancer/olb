import { Link, useLocation } from 'react-router'
import { ChevronRight, Home } from 'lucide-react'
import { cn } from '@/lib/utils'

interface BreadcrumbItem {
  label: string
  path?: string
}

interface BreadcrumbsProps {
  items?: BreadcrumbItem[]
  className?: string
}

const routeLabels: Record<string, string> = {
  dashboard: 'Dashboard',
  backends: 'Backends',
  pools: 'Pools',
  routes: 'Routes',
  listeners: 'Listeners',
  middleware: 'Middleware',
  waf: 'WAF',
  certificates: 'Certificates',
  logs: 'Logs',
  settings: 'Settings'
}

export function Breadcrumbs({ items, className }: BreadcrumbsProps) {
  const location = useLocation()
  const pathSegments = location.pathname.split('/').filter(Boolean)

  // Auto-generate breadcrumbs from path if not provided
  const breadcrumbItems = items || [
    { label: 'Home', path: '/dashboard' },
    ...pathSegments.map((segment, index) => ({
      label: routeLabels[segment] || segment.charAt(0).toUpperCase() + segment.slice(1),
      path: index === pathSegments.length - 1 ? undefined : `/${pathSegments.slice(0, index + 1).join('/')}`
    }))
  ]

  return (
    <nav
      aria-label="Breadcrumb"
      className={cn(
        'flex items-center space-x-1 text-sm text-muted-foreground',
        className
      )}
    >
      <Link
        to="/dashboard"
        className="flex items-center hover:text-foreground transition-colors"
      >
        <Home className="h-4 w-4" />
      </Link>
      {breadcrumbItems.map((item, index) => (
        <div key={index} className="flex items-center">
          <ChevronRight className="h-4 w-4 mx-1" />
          {item.path ? (
            <Link
              to={item.path}
              className="hover:text-foreground transition-colors"
            >
              {item.label}
            </Link>
          ) : (
            <span className="font-medium text-foreground">{item.label}</span>
          )}
        </div>
      ))}
    </nav>
  )
}
