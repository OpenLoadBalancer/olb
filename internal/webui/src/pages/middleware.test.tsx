import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { MiddlewarePage } from '@/pages/middleware'

const { mockUseMiddlewareStatus, mockUseConfig } = vi.hoisted(() => ({
  mockUseMiddlewareStatus: vi.fn(),
  mockUseConfig: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useMiddlewareStatus: mockUseMiddlewareStatus,
  useConfig: mockUseConfig,
}))

const mockMiddlewareStatus = [
  { id: 'rate_limit', name: 'Rate Limiting', description: 'Limit request rates', enabled: true, category: 'security' },
  { id: 'cors', name: 'CORS', description: 'Cross-origin resource sharing', enabled: true, category: 'traffic' },
  { id: 'compression', name: 'Compression', description: 'Compress responses', enabled: false, category: 'performance' },
  { id: 'logging', name: 'Request Logging', description: 'Log all requests', enabled: true, category: 'observability' },
]

const mockConfig = {
  middleware: {
    rate_limit: { enabled: true, requests_per_second: 100 },
    cors: { enabled: true, allowed_origins: ['*'] },
  },
}

function setupLoadedMiddleware() {
  mockUseMiddlewareStatus.mockReturnValue({ data: mockMiddlewareStatus, isLoading: false, error: null, refetch: vi.fn() })
  mockUseConfig.mockReturnValue({ data: mockConfig, isLoading: false, error: null, refetch: vi.fn() })
}

describe('MiddlewarePage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading state', () => {
    mockUseMiddlewareStatus.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    mockUseConfig.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    render(<MiddlewarePage />)

    expect(screen.getByText('Middleware')).toBeInTheDocument()
    expect(screen.getByText('Configure request/response middleware chain')).toBeInTheDocument()
  })

  it('renders middleware cards with names and status', () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    expect(screen.getByText('Rate Limiting')).toBeInTheDocument()
    expect(screen.getByText('CORS')).toBeInTheDocument()
    expect(screen.getByText('Compression')).toBeInTheDocument()
    expect(screen.getByText('Request Logging')).toBeInTheDocument()
  })

  it('displays enabled count in header', () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    // 3 of 4 middleware components enabled
    expect(screen.getByText(/3 of 4 middleware components enabled/i)).toBeInTheDocument()
  })

  it('shows Enabled/Disabled badges', () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    const enabledBadges = screen.getAllByText('Enabled')
    const disabledBadges = screen.getAllByText('Disabled')
    expect(enabledBadges.length).toBeGreaterThanOrEqual(3)
    expect(disabledBadges.length).toBeGreaterThanOrEqual(1)
  })

  it('filters by category', () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    // Click Security filter
    fireEvent.click(screen.getByRole('button', { name: 'Security' }))

    expect(screen.getByText('Rate Limiting')).toBeInTheDocument()
    expect(screen.queryByText('CORS')).not.toBeInTheDocument()
    expect(screen.queryByText('Request Logging')).not.toBeInTheDocument()
  })

  it('shows all when All filter clicked', () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    // Click Security first, then All
    fireEvent.click(screen.getByRole('button', { name: 'Security' }))
    fireEvent.click(screen.getByRole('button', { name: 'All' }))

    expect(screen.getByText('Rate Limiting')).toBeInTheDocument()
    expect(screen.getByText('CORS')).toBeInTheDocument()
    expect(screen.getByText('Compression')).toBeInTheDocument()
  })

  it('shows category badges on middleware cards', () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    // category badges use capitalize CSS, DOM text is lowercase
    expect(screen.getByText('security')).toBeInTheDocument()
    expect(screen.getByText('traffic')).toBeInTheDocument()
  })

  it('opens config dialog on View Configuration click', async () => {
    setupLoadedMiddleware()
    render(<MiddlewarePage />)

    const user = userEvent.setup()
    const configBtns = screen.getAllByRole('button', { name: /view configuration/i })
    await user.click(configBtns[0])

    await waitFor(() => {
      // Dialog title includes the middleware name
      expect(screen.getByText('Rate Limiting Configuration')).toBeInTheDocument()
    }, { timeout: 3000 })
  })

  it('shows empty middleware list gracefully', () => {
    mockUseMiddlewareStatus.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseConfig.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    render(<MiddlewarePage />)

    expect(screen.getByText(/0 of 0 middleware components enabled/i)).toBeInTheDocument()
  })
})
