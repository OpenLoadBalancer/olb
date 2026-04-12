import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@/test/utils'
import { DashboardPage } from '@/pages/dashboard'
import { toast } from 'sonner'

// Create controllable mock functions for each hook
const { mockUseHealth, mockUseSystemInfo, mockUsePools, mockUseRoutes, mockUseEvents } = vi.hoisted(() => ({
  mockUseHealth: vi.fn(),
  mockUseSystemInfo: vi.fn(),
  mockUsePools: vi.fn(),
  mockUseRoutes: vi.fn(),
  mockUseEvents: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useHealth: mockUseHealth,
  useSystemInfo: mockUseSystemInfo,
  usePools: mockUsePools,
  useRoutes: mockUseRoutes,
  useEvents: mockUseEvents,
}))

vi.mock('@/hooks/use-event-stream', () => ({
  useEventStream: vi.fn(() => ({ lastEvent: null, connected: false, reconnect: vi.fn() })),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

// Test data
const mockHealth = {
  status: 'healthy',
  checks: {
    database: { status: 'healthy', message: 'OK' },
    api: { status: 'healthy', message: 'OK' },
  },
  timestamp: '2026-04-11T00:00:00Z',
}

const mockSystemInfo = {
  version: '0.1.0',
  commit: 'abc123',
  build_date: '2026-01-01',
  uptime: '2h30m',
  state: 'running',
  go_version: 'go1.26.0',
}

const mockPools = [
  {
    name: 'web',
    algorithm: 'round_robin',
    backends: [
      { id: 'b1', address: '10.0.0.1:8080', weight: 1, state: 'up', healthy: true, requests: 1000, errors: 5 },
      { id: 'b2', address: '10.0.0.2:8080', weight: 1, state: 'up', healthy: true, requests: 800, errors: 2 },
    ],
  },
  {
    name: 'api',
    algorithm: 'least_connections',
    backends: [
      { id: 'b3', address: '10.0.1.1:3000', weight: 1, state: 'up', healthy: true, requests: 500, errors: 0 },
      { id: 'b4', address: '10.0.1.2:3000', weight: 1, state: 'down', healthy: false, requests: 200, errors: 50 },
    ],
  },
]

const mockRoutes = [
  { name: 'web-route', host: '', path: '/', methods: ['GET'], headers: {}, backend_pool: 'web', priority: 0 },
]

const mockEvents = [
  { id: '1', type: 'success', message: 'Backend added', timestamp: '2026-04-11T00:01:00Z' },
  { id: '2', type: 'warning', message: 'High latency detected', timestamp: '2026-04-11T00:00:00Z' },
]

function setupLoadedMocks() {
  mockUseHealth.mockReturnValue({ data: mockHealth, isLoading: false, error: null, refetch: vi.fn() })
  mockUseSystemInfo.mockReturnValue({ data: mockSystemInfo, isLoading: false, error: null, refetch: vi.fn() })
  mockUsePools.mockReturnValue({ data: mockPools, isLoading: false, error: null, refetch: vi.fn() })
  mockUseRoutes.mockReturnValue({ data: mockRoutes, isLoading: false, error: null, refetch: vi.fn() })
  mockUseEvents.mockReturnValue({ data: mockEvents, isLoading: false, error: null, refetch: vi.fn() })
}

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading state with disabled action buttons', () => {
    mockUseHealth.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    mockUseSystemInfo.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    mockUsePools.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    mockUseRoutes.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    mockUseEvents.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })

    render(<DashboardPage />)

    expect(screen.getByText('Dashboard')).toBeInTheDocument()
    expect(screen.getByText('Overview of your OpenLoadBalancer instance')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /refresh/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /export/i })).toBeDisabled()
  })

  it('shows error alert when health fetch fails', () => {
    mockUseHealth.mockReturnValue({ data: null, isLoading: false, error: new Error('Server error'), refetch: vi.fn() })
    mockUseSystemInfo.mockReturnValue({ data: mockSystemInfo, isLoading: false, error: null, refetch: vi.fn() })
    mockUsePools.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseRoutes.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseEvents.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })

    render(<DashboardPage />)

    expect(screen.getByText('Error')).toBeInTheDocument()
    expect(screen.getByText(/failed to load dashboard data/i)).toBeInTheDocument()
  })

  it('renders metric cards after data loads', () => {
    setupLoadedMocks()
    render(<DashboardPage />)

    expect(screen.getByRole('region', { name: 'Key metrics' })).toBeInTheDocument()
    expect(screen.getByText('Backend pools configured')).toBeInTheDocument()
    expect(screen.getByText('Active routes')).toBeInTheDocument()
    expect(screen.getByText('Since last restart')).toBeInTheDocument()
  })

  it('displays system status information', () => {
    setupLoadedMocks()
    render(<DashboardPage />)

    expect(screen.getByText('System Status')).toBeInTheDocument()
    expect(screen.getByText('Health Checks')).toBeInTheDocument()
    // Uptime and Go version appear in both summary cards and detail card
    expect(screen.getAllByText('2h30m').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('go1.26.0').length).toBeGreaterThanOrEqual(2)
  })

  it('displays health check components', () => {
    setupLoadedMocks()
    render(<DashboardPage />)

    expect(screen.getByText('Health Checks')).toBeInTheDocument()
    expect(screen.getByText('database')).toBeInTheDocument()
    expect(screen.getByText('api')).toBeInTheDocument()
  })

  it('shows recent activity events', () => {
    setupLoadedMocks()
    render(<DashboardPage />)

    expect(screen.getByText('Recent Activity')).toBeInTheDocument()
    expect(screen.getByText('Backend added')).toBeInTheDocument()
    expect(screen.getByText('High latency detected')).toBeInTheDocument()
  })

  it('shows live status indicator when healthy', () => {
    setupLoadedMocks()
    render(<DashboardPage />)

    expect(screen.getByRole('status', { name: /system health/i })).toBeInTheDocument()
    expect(screen.getByText('Live')).toBeInTheDocument()
  })

  it('shows degraded status when unhealthy', () => {
    mockUseHealth.mockReturnValue({
      data: { ...mockHealth, status: 'unhealthy' },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    })
    mockUseSystemInfo.mockReturnValue({ data: mockSystemInfo, isLoading: false, error: null, refetch: vi.fn() })
    mockUsePools.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseRoutes.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseEvents.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })

    render(<DashboardPage />)

    expect(screen.getByText('Degraded')).toBeInTheDocument()
  })

  it('calls refetch and toast on refresh button click', () => {
    const refetchHealth = vi.fn()
    mockUseHealth.mockReturnValue({ data: mockHealth, isLoading: false, error: null, refetch: refetchHealth })
    mockUseSystemInfo.mockReturnValue({ data: mockSystemInfo, isLoading: false, error: null, refetch: vi.fn() })
    mockUsePools.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseRoutes.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    mockUseEvents.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })

    render(<DashboardPage />)
    fireEvent.click(screen.getByRole('button', { name: /refresh dashboard/i }))

    expect(refetchHealth).toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalledWith('Dashboard refreshed')
  })

  it('triggers export on export button click', () => {
    // jsdom doesn't implement URL.createObjectURL
    vi.stubGlobal('URL', { ...URL, createObjectURL: vi.fn(() => 'blob:test'), revokeObjectURL: vi.fn() })

    setupLoadedMocks()
    render(<DashboardPage />)

    fireEvent.click(screen.getByRole('button', { name: /export dashboard/i }))

    expect(toast.success).toHaveBeenCalledWith('Dashboard data exported')
  })
})
