import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { SettingsPage } from '@/pages/settings'
import { toast } from 'sonner'

const { mockUseConfig, mockApiReload } = vi.hoisted(() => ({
  mockUseConfig: vi.fn(),
  mockApiReload: vi.fn(() => Promise.resolve({ success: true })),
}))

vi.mock('@/hooks/use-query', () => ({
  useConfig: mockUseConfig,
}))

vi.mock('@/lib/api', () => ({
  api: { reload: mockApiReload },
  APIError: class APIError extends Error {
    status: number
    constructor(status: number, message: string) {
      super(message)
      this.status = status
    }
  },
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const mockConfig = {
  logging: { level: 'debug', format: 'json', output: 'file' },
  server: {
    max_connections: 50000,
    proxy_timeout: '30s',
    dial_timeout: '5s',
    max_retries: 5,
  },
  admin: {
    address: ':9090',
    mcp_audit: true,
    mcp_address: ':8080',
  },
  cluster: {
    enabled: true,
    node_id: 'node-1',
    bind_addr: '0.0.0.0',
    bind_port: 7946,
    peers: ['node-2', 'node-3'],
  },
  listeners: [
    { name: 'http', protocol: 'http', address: ':8080', routes: [{ path: '/' }, { path: '/api' }] },
  ],
  pools: [
    { name: 'web', algorithm: 'round_robin', backends: [{ address: '10.0.0.1:8080' }], health_check: { type: 'http', path: '/health', interval: '10s' } },
  ],
  tls: { cert_file: '/certs/cert.pem', key_file: '/certs/key.pem', acme: { enabled: true, email: 'admin@test.com' } },
  waf: { enabled: true, mode: 'block', ip_acl: { enabled: true }, rate_limit: { enabled: false }, sanitizer: { enabled: false }, detection: { enabled: true }, bot_detection: { enabled: true }, response: { security_headers: { enabled: true } } },
  middleware: { cors: { enabled: true, allowed_origins: ['*'], allowed_methods: ['GET', 'POST'] } },
}

function setupConfig() {
  mockUseConfig.mockReturnValue({ data: mockConfig, isLoading: false, error: null, refetch: vi.fn() })
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders page title and info alert', () => {
    setupConfig()
    render(<SettingsPage />)

    expect(screen.getByText('Settings')).toBeInTheDocument()
    expect(screen.getByText('View current configuration')).toBeInTheDocument()
    expect(screen.getByText(/edit the config file and click/i)).toBeInTheDocument()
  })

  it('shows logging config in general tab', () => {
    setupConfig()
    render(<SettingsPage />)

    expect(screen.getByText('Logging')).toBeInTheDocument()
    expect(screen.getByText('debug')).toBeInTheDocument()
    expect(screen.getByText('json')).toBeInTheDocument()
    expect(screen.getByText('file')).toBeInTheDocument()
  })

  it('shows server config in general tab', () => {
    setupConfig()
    render(<SettingsPage />)

    expect(screen.getByText('Server')).toBeInTheDocument()
    expect(screen.getByText('50000')).toBeInTheDocument()
    // Timeout values render via String()
    expect(screen.getByText('5s')).toBeInTheDocument()
  })

  it('shows admin config in admin tab', async () => {
    setupConfig()
    render(<SettingsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /admin/i }))

    await waitFor(() => {
      expect(screen.getByText('Admin API')).toBeInTheDocument()
    })
    expect(screen.getByText(':9090')).toBeInTheDocument()
    expect(screen.getByText(':8080')).toBeInTheDocument()
  })

  it('shows listeners in network tab', async () => {
    setupConfig()
    render(<SettingsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /network/i }))

    await waitFor(() => {
      expect(screen.getByText('Listeners')).toBeInTheDocument()
    })
    // "http" appears as both listener name and protocol badge
    expect(screen.getAllByText('http').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText(':8080')).toBeInTheDocument()
  })

  it('shows TLS config in security tab', async () => {
    setupConfig()
    render(<SettingsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /security/i }))

    await waitFor(() => {
      expect(screen.getByText('TLS')).toBeInTheDocument()
    })
    expect(screen.getByText('/certs/cert.pem')).toBeInTheDocument()
  })

  it('shows WAF config when enabled', async () => {
    setupConfig()
    render(<SettingsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /security/i }))

    await waitFor(() => {
      expect(screen.getByText('WAF')).toBeInTheDocument()
    })
    expect(screen.getByText('block')).toBeInTheDocument()
  })

  it('calls api.reload on reload button click', async () => {
    setupConfig()
    render(<SettingsPage />)

    const reloadBtn = screen.getByRole('button', { name: /reload configuration/i })
    fireEvent.click(reloadBtn)

    await waitFor(() => {
      expect(mockApiReload).toHaveBeenCalled()
    })
    expect(toast.success).toHaveBeenCalledWith('Configuration reloaded from disk')
  })

  it('shows error toast when reload fails', async () => {
    mockApiReload.mockRejectedValueOnce(new Error('Reload failed'))
    setupConfig()
    render(<SettingsPage />)

    const reloadBtn = screen.getByRole('button', { name: /reload configuration/i })
    fireEvent.click(reloadBtn)

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('Reload failed')
    })
  })
})
