import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { PoolsPage } from '@/pages/pools'

const { mockUsePools } = vi.hoisted(() => ({
  mockUsePools: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  usePools: mockUsePools,
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const mockPools = [
  {
    name: 'web-pool',
    algorithm: 'round_robin',
    backends: [
      { id: 'b1', address: '10.0.0.1:8080', weight: 1, state: 'up', healthy: true, requests: 1500, errors: 3 },
      { id: 'b2', address: '10.0.0.2:8080', weight: 1, state: 'down', healthy: false, requests: 200, errors: 45 },
    ],
    health_check: { type: 'http', path: '/health', interval: '10s', timeout: '5s' },
  },
  {
    name: 'api-pool',
    algorithm: 'least_connections',
    backends: [
      { id: 'b3', address: '10.0.1.1:3000', weight: 1, state: 'up', healthy: true, requests: 800, errors: 0 },
    ],
  },
]

function setupLoadedPools() {
  mockUsePools.mockReturnValue({ data: mockPools, isLoading: false, error: null, refetch: vi.fn() })
}

describe('PoolsPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading state', () => {
    mockUsePools.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    render(<PoolsPage />)

    expect(screen.getByText('Pools')).toBeInTheDocument()
    expect(screen.getByText('Manage backend pools and load balancing')).toBeInTheDocument()
  })

  it('shows error with retry button when fetch fails', () => {
    mockUsePools.mockReturnValue({ data: null, isLoading: false, error: new Error('fetch failed'), refetch: vi.fn() })
    render(<PoolsPage />)

    expect(screen.getByText(/failed to load pools/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
  })

  it('renders pool cards with names and algorithms', () => {
    setupLoadedPools()
    render(<PoolsPage />)

    expect(screen.getByText('web-pool')).toBeInTheDocument()
    expect(screen.getByText('api-pool')).toBeInTheDocument()
    expect(screen.getByText('Round Robin')).toBeInTheDocument()
    expect(screen.getByText('Least Connections')).toBeInTheDocument()
  })

  it('auto-selects first pool and shows its backends', () => {
    setupLoadedPools()
    render(<PoolsPage />)

    // First pool (web-pool) is auto-selected, its backends shown
    expect(screen.getByText('10.0.0.1:8080')).toBeInTheDocument()
    expect(screen.getByText('10.0.0.2:8080')).toBeInTheDocument()
  })

  it('displays backend health badges', () => {
    setupLoadedPools()
    render(<PoolsPage />)

    expect(screen.getByText('Healthy')).toBeInTheDocument()
    expect(screen.getByText('Unhealthy')).toBeInTheDocument()
  })

  it('filters pools by search input', () => {
    setupLoadedPools()
    render(<PoolsPage />)

    expect(screen.getByText('web-pool')).toBeInTheDocument()
    expect(screen.getByText('api-pool')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Search pools'), { target: { value: 'api' } })

    expect(screen.queryByText('web-pool')).not.toBeInTheDocument()
    expect(screen.getByText('api-pool')).toBeInTheDocument()
  })

  it('switches to different pool on click', () => {
    setupLoadedPools()
    render(<PoolsPage />)

    // Click api-pool card (web-pool is already selected)
    fireEvent.click(screen.getByRole('button', { name: /select pool api-pool/i }))

    expect(screen.getByText('10.0.1.1:3000')).toBeInTheDocument()
  })

  it('shows pool settings in settings tab', async () => {
    setupLoadedPools()
    render(<PoolsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /settings/i }))

    await waitFor(() => {
      expect(screen.getByText('Pool Settings')).toBeInTheDocument()
    })
    // Round Robin appears in both pool card and settings panel
    expect(screen.getAllByText('Round Robin').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('http')).toBeInTheDocument()
    expect(screen.getByText('/health')).toBeInTheDocument()
  })

  it('shows pool statistics in stats tab', async () => {
    setupLoadedPools()
    render(<PoolsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /statistics/i }))

    await waitFor(() => {
      expect(screen.getByText('Total Requests')).toBeInTheDocument()
    })
    expect(screen.getByText('Total Errors')).toBeInTheDocument()
  })

  it('shows no pool cards when search has no matches', () => {
    setupLoadedPools()
    render(<PoolsPage />)

    fireEvent.change(screen.getByLabelText('Search pools'), { target: { value: 'nonexistent' } })

    // Pool cards are filtered out but selected pool details remain
    expect(screen.queryByRole('button', { name: /select pool web-pool/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /select pool api-pool/i })).not.toBeInTheDocument()
  })
})
