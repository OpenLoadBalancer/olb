import { StrictMode, lazy, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router'
import { ThemeProvider } from '@/components/theme-provider'
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
		<div role="status" className="flex items-center justify-center h-64">
			<div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" aria-hidden="true" />
		<span className="sr-only">Loading page...</span>
		</div>
	)
}

createRoot(document.getElementById('root')!).render(
	<StrictMode>
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
	</StrictMode>,
)
