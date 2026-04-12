import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { WAFPage } from '@/pages/waf'

const { mockUseWAFStatus, mockUseConfig } = vi.hoisted(() => ({
  mockUseWAFStatus: vi.fn(),
  mockUseConfig: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useWAFStatus: mockUseWAFStatus,
  useConfig: mockUseConfig,
}))

const mockWAFStatus = {
  enabled: true,
  mode: 'enforce',
  layers: {
    ip_acl: true,
    rate_limit: true,
    sanitizer: true,
    detection: true,
    bot_detect: false,
    response: true,
  },
  stats: {
    total_blocked: 42,
    total_challenges: 7,
    total_requests: 10000,
  },
}

const mockConfig = {
  waf: {
    enabled: true,
    mode: 'enforce',
    detection: {
      detectors: {
        sqli: { enabled: true },
        xss: { enabled: true },
        rce: { enabled: false },
      },
    },
    rate_limit: { rules: [] },
  },
}

function setupLoadedWAF() {
  mockUseWAFStatus.mockReturnValue({ data: mockWAFStatus, isLoading: false, error: null, refetch: vi.fn() })
  mockUseConfig.mockReturnValue({ data: mockConfig, isLoading: false, error: null, refetch: vi.fn() })
}

describe('WAFPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading state', () => {
    mockUseWAFStatus.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    mockUseConfig.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    render(<WAFPage />)

    expect(screen.getByText('Web Application Firewall')).toBeInTheDocument()
  })

  it('shows disabled message when WAF not enabled', () => {
    mockUseWAFStatus.mockReturnValue({ data: { enabled: false }, isLoading: false, error: null, refetch: vi.fn() })
    mockUseConfig.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    render(<WAFPage />)

    expect(screen.getByText(/WAF is not enabled/i)).toBeInTheDocument()
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('shows error message when fetch fails', () => {
    mockUseWAFStatus.mockReturnValue({ data: null, isLoading: false, error: new Error('fetch failed'), refetch: vi.fn() })
    mockUseConfig.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    render(<WAFPage />)

    expect(screen.getByText(/Failed to load WAF status/i)).toBeInTheDocument()
  })

  it('renders summary cards when loaded', () => {
    setupLoadedWAF()
    render(<WAFPage />)

    expect(screen.getByText('Active Layers')).toBeInTheDocument()
    expect(screen.getByText('Threats Blocked')).toBeInTheDocument()
    expect(screen.getByText('Bot Challenges')).toBeInTheDocument()
    expect(screen.getByText('Total Requests')).toBeInTheDocument()
  })

  it('displays mode badge', () => {
    setupLoadedWAF()
    render(<WAFPage />)

    expect(screen.getByText('Enforce Mode')).toBeInTheDocument()
  })

  it('shows protection layers tab by default', () => {
    setupLoadedWAF()
    render(<WAFPage />)

    expect(screen.getByText('6-Layer Security Pipeline')).toBeInTheDocument()
    expect(screen.getByText('IP ACL')).toBeInTheDocument()
    // "Rate Limiting" appears in both layer card and tab trigger
    expect(screen.getAllByText('Rate Limiting').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('Sanitizer')).toBeInTheDocument()
    expect(screen.getByText('Detection')).toBeInTheDocument()
    expect(screen.getByText('Bot Detection')).toBeInTheDocument()
    expect(screen.getByText('Response')).toBeInTheDocument()
  })

  it('shows active/inactive badges for layers', () => {
    setupLoadedWAF()
    render(<WAFPage />)

    // 5 active, 1 inactive (bot_detect)
    const activeBadges = screen.getAllByText('Active')
    const inactiveBadges = screen.getAllByText('Inactive')
    expect(activeBadges.length).toBeGreaterThanOrEqual(5)
    expect(inactiveBadges.length).toBeGreaterThanOrEqual(1)
  })

  it('shows detection engines in detection tab', async () => {
    setupLoadedWAF()
    render(<WAFPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /detection engines/i }))

    await waitFor(() => {
      // "Detection Engines" appears in both tab and card title
      expect(screen.getAllByText('Detection Engines').length).toBeGreaterThanOrEqual(2)
    })
    expect(screen.getByText(/sqli detection/i)).toBeInTheDocument()
  })

  it('shows WAF configuration in config tab', async () => {
    setupLoadedWAF()
    render(<WAFPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /configuration/i }))

    await waitFor(() => {
      expect(screen.getByText('WAF Configuration')).toBeInTheDocument()
    })
    expect(screen.getByText('Enforce Mode')).toBeInTheDocument()
  })
})
