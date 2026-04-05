import { createBrowserRouter, Navigate } from 'react-router'
import { RootLayout } from './components/layout/root-layout'
import { ErrorBoundary } from './components/error-boundary'
import { DashboardPage } from './pages/dashboard'
import { BackendsPage } from './pages/backends'
import { PoolsPage } from './pages/pools'
import { RoutesPage } from './pages/routes'
import { ListenersPage } from './pages/listeners'
import { MiddlewarePage } from './pages/middleware'
import { WAFPage } from './pages/waf'
import { CertsPage } from './pages/certs'
import { LogsPage } from './pages/logs'
import { SettingsPage } from './pages/settings'
import { LoginPage } from './pages/login'
import { ClusterPage } from './pages/cluster'
import { MCPPage } from './pages/mcp'
import { DiscoveryPage } from './pages/discovery'
import { PluginsPage } from './pages/plugins'
import { AnalyticsPage } from './pages/analytics'
import { BackupPage } from './pages/backup'
import { ProfilerPage } from './pages/profiler'
import { AppearancePage } from './pages/appearance'
import { NotificationsPage } from './pages/notifications'
import { ImportExportPage } from './pages/import-export'
import { UsersPage } from './pages/users'
import { AuditPage } from './pages/audit'
import { DiagnosticsPage } from './pages/diagnostics'
import { RateLimitPage } from './pages/rate-limit'
import { HealthPage } from './pages/health'
import { MetricsPage } from './pages/metrics'
import { CachePage } from './pages/cache'
import { TasksPage } from './pages/tasks'
import { MaintenancePage } from './pages/maintenance'
import { ConsolePage } from './pages/console'
import { NotFoundPage } from './pages/not-found'

export const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginPage />,
    errorElement: <ErrorBoundary />
  },
  {
    path: '/',
    element: <RootLayout />,
    errorElement: <ErrorBoundary />,
    children: [
      {
        index: true,
        element: <Navigate to="/dashboard" replace />
      },
      {
        path: 'dashboard',
        element: <DashboardPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'backends',
        element: <BackendsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'pools',
        element: <PoolsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'routes',
        element: <RoutesPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'listeners',
        element: <ListenersPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'middleware',
        element: <MiddlewarePage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'waf',
        element: <WAFPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'certificates',
        element: <CertsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'cluster',
        element: <ClusterPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'discovery',
        element: <DiscoveryPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'plugins',
        element: <PluginsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'mcp',
        element: <MCPPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'logs',
        element: <LogsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'analytics',
        element: <AnalyticsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'backup',
        element: <BackupPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'profiler',
        element: <ProfilerPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'settings',
        element: <SettingsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'appearance',
        element: <AppearancePage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'import-export',
        element: <ImportExportPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'notifications',
        element: <NotificationsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'users',
        element: <UsersPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'audit',
        element: <AuditPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'diagnostics',
        element: <DiagnosticsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'rate-limit',
        element: <RateLimitPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'health',
        element: <HealthPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'metrics',
        element: <MetricsPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'cache',
        element: <CachePage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'tasks',
        element: <TasksPage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'maintenance',
        element: <MaintenancePage />,
        errorElement: <ErrorBoundary />
      },
      {
        path: 'console',
        element: <ConsolePage />,
        errorElement: <ErrorBoundary />
      }
    ]
  },
  {
    path: '*',
    element: <NotFoundPage />
  }
])
