import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { CertificatesPage } from '@/pages/certificates'
import { toast } from 'sonner'

const { mockUseCertificates } = vi.hoisted(() => ({
  mockUseCertificates: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useCertificates: mockUseCertificates,
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const mockCerts = [
  { names: ['example.com', 'www.example.com'], expiry: '2099-12-31T00:00:00Z', is_wildcard: false },
  { names: ['*.example.com'], expiry: '2099-06-15T00:00:00Z', is_wildcard: true },
]

function setupLoadedCerts() {
  mockUseCertificates.mockReturnValue({ data: mockCerts, isLoading: false, error: null, refetch: vi.fn() })
}

describe('CertificatesPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders page title and summary cards', () => {
    setupLoadedCerts()
    render(<CertificatesPage />)

    expect(screen.getByText('TLS Certificates')).toBeInTheDocument()
    expect(screen.getByText('Manage SSL/TLS certificates')).toBeInTheDocument()
    expect(screen.getByText('Total Certificates')).toBeInTheDocument()
    expect(screen.getByText('Wildcards')).toBeInTheDocument()
    expect(screen.getByText('Expiring Soon')).toBeInTheDocument()
  })

  it('shows certificate domains in list', () => {
    setupLoadedCerts()
    render(<CertificatesPage />)

    // Domain appears in both card title and SANs field
    expect(screen.getAllByText('example.com, www.example.com').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('*.example.com').length).toBeGreaterThanOrEqual(2)
  })

  it('shows wildcard badge for wildcard certs', () => {
    setupLoadedCerts()
    render(<CertificatesPage />)

    expect(screen.getByText('Wildcard Certificate')).toBeInTheDocument()
    expect(screen.getByText('Standard Certificate')).toBeInTheDocument()
  })

  it('shows empty state when no certificates', () => {
    mockUseCertificates.mockReturnValue({ data: [], isLoading: false, error: null, refetch: vi.fn() })
    render(<CertificatesPage />)

    expect(screen.getByText(/No TLS certificates configured/i)).toBeInTheDocument()
  })

  it('shows error with retry when fetch fails', () => {
    mockUseCertificates.mockReturnValue({ data: null, isLoading: false, error: new Error('fetch failed'), refetch: vi.fn() })
    render(<CertificatesPage />)

    expect(screen.getByText(/Failed to load certificates/i)).toBeInTheDocument()
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('opens add certificate dialog', async () => {
    setupLoadedCerts()
    render(<CertificatesPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add certificate/i }))

    await waitFor(() => {
      // Dialog has a description that is unique
      expect(screen.getByText(/Add a new TLS certificate/i)).toBeInTheDocument()
    }, { timeout: 3000 })
    expect(screen.getByText("Let's Encrypt")).toBeInTheDocument()
    expect(screen.getByText('Manual Upload')).toBeInTheDocument()
  })

  it('shows toast on renew click', async () => {
    setupLoadedCerts()
    render(<CertificatesPage />)

    const renewBtns = screen.getAllByRole('button', { name: /renew certificate/i })
    fireEvent.click(renewBtns[0])

    expect(toast.success).toHaveBeenCalledWith('Certificate renewal initiated')
  })

  it('shows toast on add certificate placeholder', async () => {
    setupLoadedCerts()
    render(<CertificatesPage />)

    const user = userEvent.setup()
    // Open dialog via the header button
    await user.click(screen.getByRole('button', { name: /add certificate/i }))

    await waitFor(() => {
      expect(screen.getByText(/Add a new TLS certificate/i)).toBeInTheDocument()
    }, { timeout: 3000 })

    // Fill required ACME fields to enable submit button using role-based queries
    const domainInput = screen.getByRole('textbox', { name: /domain/i })
    await user.type(domainInput, 'test.com')

    const emailInput = screen.getByRole('textbox', { name: /email/i })
    await user.type(emailInput, 'admin@test.com')

    // Click the last "Add Certificate" button (the dialog submit)
    const addBtns = screen.getAllByRole('button').filter(b => b.textContent?.trim() === 'Add Certificate')
    await user.click(addBtns[addBtns.length - 1])

    expect(toast.info).toHaveBeenCalledWith('Certificate management is done via configuration or ACME')
  })
})
