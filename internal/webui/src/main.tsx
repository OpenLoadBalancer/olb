import { StrictMode, lazy, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router'
import { ThemeProvider } from '@/components/theme-provider'
import { QueryProvider } from '@/lib/query-provider'
import { Toaster } from '@/components/ui/sonner'
import { Layout } from '@/components/layout'
import { ErrorBoundary } from '@/pages/error'
import './index.css'

// Lazy-load page components to reduce initial bundle size.
// Each page is loaded on first navigation.
const DashboardPage = lazy(() => import('@/pages/dashboard').then(m => ({ default: m.DashboardPage })))
const PoolsPage = lazy(() => import('@/pages/pools').then(m => ({ default: m.PoolsPage })))
const ListenersPage = lazy(() => import('@/pages/listeners').then(m => ({ default: m.ListenersPage })))
const MiddlewarePage = lazy(() => import('@/pages/middleware').then(m => ({ default: m.MiddlewarePage })))
const CertificatesPage = lazy(() => import('@/pages/certificates').then(m => ({ default: m.CertificatesPage })))
const WAFPage = lazy(() => import('@/pages/waf').then(m => ({ default: m.WAFPage })))
const MetricsPage = lazy(() => import('@/pages/metrics').then(m => ({ default: m.MetricsPage })))
const LogsPage = lazy(() => import('@/pages/logs').then(m => ({ default: m.LogsPage })))
const ClusterPage = lazy(() => import('@/pages/cluster').then(m => ({ default: m.ClusterPage })))
const SettingsPage = lazy(() => import('@/pages/settings').then(m => ({ default: m.SettingsPage })))
const BackupRestorePage = lazy(() => import('@/pages/backup').then(m => ({ default: m.BackupRestorePage })))

function PageLoader() {
	return (
		<div className="flex min-h-screen" role="status" aria-label="Loading page">
			<div className="hidden lg:block w-64 border-r bg-muted/30 flex-shrink-0 p-4 space-y-3">
				<div className="h-8 bg-muted rounded animate-pulse" />
				<div className="h-6 bg-muted rounded animate-pulse w-3/4" />
				<div className="h-6 bg-muted rounded animate-pulse w-5/6" />
				<div className="h-6 bg-muted rounded animate-pulse w-2/3" />
				<div className="h-6 bg-muted rounded animate-pulse w-4/5" />
			</div>
			<div className="flex-1 p-6 space-y-6">
				<div className="h-8 bg-muted rounded animate-pulse w-1/3" />
				<div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
					{Array.from({ length: 4 }).map((_, i) => (
						<div key={i} className="h-28 bg-muted rounded animate-pulse" />
					))}
				</div>
				<div className="h-64 bg-muted rounded animate-pulse" />
			</div>
			<span className="sr-only">Loading page...</span>
		</div>
	)
}

createRoot(document.getElementById('root')!).render(
	<StrictMode>
		<QueryProvider>
			<ThemeProvider defaultTheme="system" storageKey="olb-theme">
				<BrowserRouter>
					<Layout>
						<Suspense fallback={<PageLoader />}>
							<Routes>
								<Route path="/" element={<DashboardPage />} />
								<Route path="/pools" element={<PoolsPage />} />
								<Route path="/listeners" element={<ListenersPage />} />
								<Route path="/middleware" element={<MiddlewarePage />} />
								<Route path="/certificates" element={<CertificatesPage />} />
								<Route path="/waf" element={<WAFPage />} />
								<Route path="/metrics" element={<MetricsPage />} />
								<Route path="/logs" element={<LogsPage />} />
								<Route path="/cluster" element={<ClusterPage />} />
								<Route path="/settings" element={<SettingsPage />} />
								<Route path="/backup" element={<BackupRestorePage />} />
								<Route path="*" element={<ErrorBoundary />} />
							</Routes>
						</Suspense>
					</Layout>
					<Toaster position="bottom-right" />
				</BrowserRouter>
			</ThemeProvider>
		</QueryProvider>
	</StrictMode>,
)
