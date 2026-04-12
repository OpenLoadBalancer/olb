import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { LogsPage } from '@/pages/logs'
import { toast } from 'sonner'

const { mockUseEvents } = vi.hoisted(() => ({
  mockUseEvents: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useEvents: mockUseEvents,
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const mockEvents = [
  { id: '1', type: 'success', message: 'Backend healthy', timestamp: '2026-04-11T12:00:00Z' },
  { id: '2', type: 'warning', message: 'High latency on backend-2', timestamp: '2026-04-11T11:30:00Z' },
  { id: '3', type: 'error', message: 'Connection refused to 10.0.0.5', timestamp: '2026-04-11T11:00:00Z' },
]

function setupLoadedEvents() {
  mockUseEvents.mockReturnValue({ data: mockEvents, refetch: vi.fn() })
}

describe('LogsPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // jsdom doesn't implement scrollIntoView
    Element.prototype.scrollIntoView = vi.fn()
  })

  it('renders page title', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByText('System Events')).toBeInTheDocument()
    expect(screen.getByText('View backend health events and system activity')).toBeInTheDocument()
  })

  it('shows live indicator', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByText('Live')).toBeInTheDocument()
  })

  it('shows pause button when live', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByRole('button', { name: /pause/i })).toBeInTheDocument()
  })

  it('renders event messages in table', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByText('Backend healthy')).toBeInTheDocument()
    expect(screen.getByText('High latency on backend-2')).toBeInTheDocument()
    expect(screen.getByText('Connection refused to 10.0.0.5')).toBeInTheDocument()
  })

  it('shows source badges for events', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    // All events have source "system"
    const systemBadges = screen.getAllByText('system')
    expect(systemBadges.length).toBeGreaterThanOrEqual(3)
  })

  it('filters events by search', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByText('Backend healthy')).toBeInTheDocument()
    expect(screen.getByText('High latency on backend-2')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Search events'), { target: { value: 'latency' } })

    expect(screen.queryByText('Backend healthy')).not.toBeInTheDocument()
    expect(screen.getByText('High latency on backend-2')).toBeInTheDocument()
  })

  it('shows empty state when no events', () => {
    mockUseEvents.mockReturnValue({ data: [], refetch: vi.fn() })
    render(<LogsPage />)

    expect(screen.getByText(/No system events available/i)).toBeInTheDocument()
  })

  it('shows filters card', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByText('Filters')).toBeInTheDocument()
    expect(screen.getByText('Auto-scroll')).toBeInTheDocument()
  })

  it('toggles to paused state', async () => {
    setupLoadedEvents()
    render(<LogsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /pause/i }))

    expect(screen.getByText('Paused')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /resume/i })).toBeInTheDocument()
  })

  it('exports events on export click', async () => {
    vi.stubGlobal('URL', { ...URL, createObjectURL: vi.fn(() => 'blob:test'), revokeObjectURL: vi.fn() })
    setupLoadedEvents()
    render(<LogsPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /export/i }))

    expect(toast.success).toHaveBeenCalledWith('Events exported')
  })

  it('shows event count', () => {
    setupLoadedEvents()
    render(<LogsPage />)

    expect(screen.getByText(/Showing 3 of 3 events/i)).toBeInTheDocument()
  })
})
