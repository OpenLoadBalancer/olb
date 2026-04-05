import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { Toaster } from 'sonner'
import { RouterProvider } from 'react-router'
import { router } from './router'
import { ThemeProvider } from './providers/theme-provider'
import './globals.css'

// Enable mock API in development
if (import.meta.env.DEV && import.meta.env.VITE_ENABLE_MOCK_API === 'true') {
  import('./lib/mock-api').then(({ setupMockAPI }) => {
    setupMockAPI()
    console.log('[MockAPI] Mock API enabled')
  })
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 5 * 60 * 1000
    }
  }
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="system" storageKey="olb-theme">
        <RouterProvider router={router} />
        <Toaster position="bottom-right" richColors />
      </ThemeProvider>
      <ReactQueryDevtools initialIsOpen={false} />
    </QueryClientProvider>
  </StrictMode>
)
