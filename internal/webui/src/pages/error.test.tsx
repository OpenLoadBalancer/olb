import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@/test/utils'
import { ErrorBoundary, NotFoundPage } from '@/pages/error'
import { useRouteError, isRouteErrorResponse } from 'react-router'

vi.mock('react-router', async () => {
  const actual = await vi.importActual('react-router')
  return {
    ...actual,
    useRouteError: vi.fn(),
    isRouteErrorResponse: vi.fn(),
  }
})

describe('NotFoundPage', () => {
  it('renders 404 page', () => {
    render(<NotFoundPage />)

    expect(screen.getByText('404')).toBeInTheDocument()
    expect(screen.getByText('Page Not Found')).toBeInTheDocument()
    expect(screen.getByText(/doesn't exist or has been moved/i)).toBeInTheDocument()
  })

  it('has Go Back and Go Home buttons', () => {
    render(<NotFoundPage />)

    expect(screen.getByText('Go Back')).toBeInTheDocument()
    expect(screen.getByText('Go Home')).toBeInTheDocument()
  })
})

describe('ErrorBoundary', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('shows NotFoundPage for 404 responses', () => {
    vi.mocked(useRouteError).mockReturnValue({ status: 404, statusText: 'Not Found' })
    vi.mocked(isRouteErrorResponse).mockReturnValue(true)
    render(<ErrorBoundary />)

    expect(screen.getByText('404')).toBeInTheDocument()
    expect(screen.getByText('Page Not Found')).toBeInTheDocument()
  })

  it('shows error details for HTTP error responses', () => {
    vi.mocked(useRouteError).mockReturnValue({
      status: 500,
      statusText: 'Internal Server Error',
      data: 'Something broke',
    })
    vi.mocked(isRouteErrorResponse).mockReturnValue(true)
    render(<ErrorBoundary />)

    expect(screen.getByText('Error 500')).toBeInTheDocument()
    expect(screen.getByText('Something broke')).toBeInTheDocument()
    expect(screen.getByText('Go Back')).toBeInTheDocument()
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('shows unexpected error for non-response errors', () => {
    vi.mocked(useRouteError).mockReturnValue(new Error('Something unexpected'))
    vi.mocked(isRouteErrorResponse).mockReturnValue(false)
    render(<ErrorBoundary />)

    expect(screen.getByText('Unexpected Error')).toBeInTheDocument()
    expect(screen.getByText('Something unexpected')).toBeInTheDocument()
    expect(screen.getByText('Retry')).toBeInTheDocument()
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
  })

  it('shows generic message for non-Error objects', () => {
    vi.mocked(useRouteError).mockReturnValue('string error')
    vi.mocked(isRouteErrorResponse).mockReturnValue(false)
    render(<ErrorBoundary />)

    expect(screen.getByText('Unexpected Error')).toBeInTheDocument()
    expect(screen.getByText('An unexpected error occurred')).toBeInTheDocument()
  })
})
