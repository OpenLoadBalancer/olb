import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router'
import { ThemeProvider } from '@/components/theme-provider'
import { Toaster } from '@/components/ui/sonner'
import { Layout } from '@/components/layout'
import { DashboardPage } from '@/pages/dashboard'
import { PoolsPage } from '@/pages/pools'
import { ListenersPage } from '@/pages/listeners'
import { MiddlewarePage } from '@/pages/middleware'
import { CertificatesPage } from '@/pages/certificates'
import { WAFPage } from '@/pages/waf'
import { MetricsPage } from '@/pages/metrics'
import { SettingsPage } from '@/pages/settings'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider defaultTheme="system" storageKey="olb-theme">
      <BrowserRouter>
        <Layout>
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/pools" element={<PoolsPage />} />
            <Route path="/listeners" element={<ListenersPage />} />
            <Route path="/middleware" element={<MiddlewarePage />} />
            <Route path="/certificates" element={<CertificatesPage />} />
            <Route path="/waf" element={<WAFPage />} />
            <Route path="/metrics" element={<MetricsPage />} />
            <Route path="/settings" element={<SettingsPage />} />
          </Routes>
        </Layout>
      </BrowserRouter>
      <Toaster position="bottom-right" />
    </ThemeProvider>
  </StrictMode>,
)
