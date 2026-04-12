import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { MetricsPage } from '@/pages/metrics'

const { mockUseMetrics, mockUsePools } = vi.hoisted(() => ({
  mockUseMetrics: vi.fn(),
  mockUsePools: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useMetrics: mockUseMetrics,
  usePools: mockUsePools,
}))

const mockPools = [
  {
    name: 'web-pool',
    algorithm: 'round_robin',
    backends: [
      { id: 'b1', address: '10.0.0.1:8080', weight: 1, state: 'up', healthy: true, requests: 1500, errors: 3 },
      { id: 'b2', address: '10.0.0.2:8080', weight: 1, state: 'down', healthy: false, requests: 200, errors: 45 },
    ],
  },
  {
    name: 'api-pool',
    algorithm: 'least_connections',
    backends: [
      { id: 'b3', address: '10.0.1.1:3000', weight: 1, state: 'up', healthy: true, requests: 800, errors: 0 },
    ],
  },
]

function setupLoadedMetrics() {
  mockUseMetrics.mockReturnValue({ data: { http_requests_total: 2500 }, isLoading: false })
  mockUsePools.mockReturnValue({ data: mockPools, isLoading: false })
}

describe('MetricsPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading state', () => {
    mockUseMetrics.mockReturnValue({ data: null, isLoading: true })
    mockUsePools.mockReturnValue({ data: null, isLoading: false })
    render(<MetricsPage />)

    expect(screen.getByText('Metrics')).toBeInTheDocument()
    expect(screen.getByText('Performance and traffic analytics')).toBeInTheDocument()
  })

  it('renders summary cards after data loads', () => {
    setupLoadedMetrics()
    render(<MetricsPage />)

    expect(screen.getByText('Total Requests')).toBeInTheDocument()
    expect(screen.getByText('Backends')).toBeInTheDocument()
    expect(screen.getByText('Error Rate')).toBeInTheDocument()
    // "Pools" appears in both card title and tab trigger
    expect(screen.getAllByText('Pools').length).toBeGreaterThanOrEqual(2)
  })

  it('displays correct pool count', () => {
    setupLoadedMetrics()
    render(<MetricsPage />)

    // Pools card shows count
    expect(screen.getByText('Backend pools active')).toBeInTheDocument()
  })

  it('displays backend health summary', () => {
    setupLoadedMetrics()
    render(<MetricsPage />)

    // 2 healthy out of 3 total
    expect(screen.getByText(/2 healthy/)).toBeInTheDocument()
  })

  it('shows pool overview in pools tab', async () => {
    setupLoadedMetrics()
    render(<MetricsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /pools/i }))

    // Pool names appear in overview rows
    expect(screen.getByText('web-pool')).toBeInTheDocument()
    expect(screen.getByText('api-pool')).toBeInTheDocument()
  })

  it('shows backend health in backends tab', async () => {
    setupLoadedMetrics()
    render(<MetricsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /backend health/i }))

    // Backend addresses shown
    expect(screen.getByText('10.0.0.1:8080')).toBeInTheDocument()
    expect(screen.getByText('10.0.0.2:8080')).toBeInTheDocument()
    expect(screen.getByText('10.0.1.1:3000')).toBeInTheDocument()
  })

  it('shows traffic tab by default', () => {
    setupLoadedMetrics()
    render(<MetricsPage />)

    expect(screen.getByText('Requests by Pool')).toBeInTheDocument()
    expect(screen.getByText('Request Trend')).toBeInTheDocument()
  })

  it('handles empty pools gracefully', () => {
    mockUseMetrics.mockReturnValue({ data: null, isLoading: false })
    mockUsePools.mockReturnValue({ data: [], isLoading: false })
    render(<MetricsPage />)

    expect(screen.getByText('Total Requests')).toBeInTheDocument()
  })
})
