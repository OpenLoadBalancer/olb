import React from 'react'
import { render, type RenderOptions } from '@testing-library/react'
import { BrowserRouter } from 'react-router'

// Wrapper that provides router context for component tests
function AllTheProviders({ children }: { children: React.ReactNode }) {
  return <BrowserRouter>{children}</BrowserRouter>
}

// Custom render that includes providers
function customRender(ui: React.ReactElement, options?: Omit<RenderOptions, 'wrapper'>) {
  return render(ui, { wrapper: AllTheProviders, ...options })
}

// Re-export everything from testing library
export * from '@testing-library/react'
export { customRender as render }

// Mock API fetch helper - creates a mock fetch that returns given data
export function mockFetchSuccess<T>(data: T) {
  const mock = vi.fn(() =>
    Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ success: true, data }),
      text: () => Promise.resolve(''),
    } as Response),
  )
  vi.stubGlobal('fetch', mock)
  return mock
}

// Mock API fetch that returns an error
export function mockFetchError(status: number, message: string) {
  const mock = vi.fn(() =>
    Promise.resolve({
      ok: false,
      status,
      json: () => Promise.resolve({ success: false, error: { code: 'ERROR', message } }),
      text: () => Promise.resolve(message),
    } as Response),
  )
  vi.stubGlobal('fetch', mock)
  return mock
}
