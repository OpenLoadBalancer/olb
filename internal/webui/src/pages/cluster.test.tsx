import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@/test/utils'
import userEvent from '@testing-library/user-event'
import { ClusterPage } from '@/pages/cluster'
import { toast } from 'sonner'

const { mockUseClusterStatus, mockUseClusterMembers } = vi.hoisted(() => ({
  mockUseClusterStatus: vi.fn(),
  mockUseClusterMembers: vi.fn(),
}))

vi.mock('@/hooks/use-query', () => ({
  useClusterStatus: mockUseClusterStatus,
  useClusterMembers: mockUseClusterMembers,
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const mockClusterStatus = {
  node_id: 'node-1',
  state: 'leader',
  leader: 'node-1',
  peers: ['node-2', 'node-3'],
  applied_index: 1500,
  commit_index: 1500,
  term: 5,
  vote: 'node-1',
}

const mockMembers = [
  { id: 'node-1', address: '10.0.0.1:7946', state: 'alive' },
  { id: 'node-2', address: '10.0.0.2:7946', state: 'alive' },
  { id: 'node-3', address: '10.0.0.3:7946', state: 'suspect' },
]

function setupLoadedCluster() {
  mockUseClusterStatus.mockReturnValue({ data: mockClusterStatus, isLoading: false, error: null, refetch: vi.fn() })
  mockUseClusterMembers.mockReturnValue({ data: mockMembers, isLoading: false, error: null, refetch: vi.fn() })
}

describe('ClusterPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading state', () => {
    mockUseClusterStatus.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    mockUseClusterMembers.mockReturnValue({ data: null, isLoading: true, error: null, refetch: vi.fn() })
    render(<ClusterPage />)

    expect(screen.getByText('Cluster')).toBeInTheDocument()
    expect(screen.getByText('Raft consensus and cluster membership')).toBeInTheDocument()
  })

  it('shows standalone mode when cluster not configured', () => {
    mockUseClusterStatus.mockReturnValue({ data: null, isLoading: false, error: new Error('not configured'), refetch: vi.fn() })
    mockUseClusterMembers.mockReturnValue({ data: null, isLoading: false, error: null, refetch: vi.fn() })
    render(<ClusterPage />)

    expect(screen.getByText(/cluster is not configured/i)).toBeInTheDocument()
    expect(screen.getByText(/enable clustering in your configuration/i)).toBeInTheDocument()
  })

  it('renders cluster status cards', () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    expect(screen.getByText('Cluster Status')).toBeInTheDocument()
    expect(screen.getByText('Members')).toBeInTheDocument()
    expect(screen.getByText('Current Term')).toBeInTheDocument()
    expect(screen.getByText('Raft Index')).toBeInTheDocument()
  })

  it('displays node state and member count', () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    // State is lowercase in DOM (CSS capitalize only affects visual display)
    expect(screen.getByText('leader')).toBeInTheDocument()
    expect(screen.getByText('Node: node-1')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
  })

  it('shows cluster nodes in nodes tab', () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    expect(screen.getByText('Cluster Nodes')).toBeInTheDocument()
    expect(screen.getByText('10.0.0.1:7946')).toBeInTheDocument()
    expect(screen.getByText('10.0.0.2:7946')).toBeInTheDocument()
    expect(screen.getByText('10.0.0.3:7946')).toBeInTheDocument()
  })

  it('shows LEADER badge for leader node', () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    expect(screen.getByText('LEADER')).toBeInTheDocument()
  })

  it('shows log replication status', () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    expect(screen.getByText('Log Replication Status')).toBeInTheDocument()
  })

  it('refreshes on button click', () => {
    const refetchStatus = vi.fn()
    const refetchMembers = vi.fn()
    mockUseClusterStatus.mockReturnValue({ data: mockClusterStatus, isLoading: false, error: null, refetch: refetchStatus })
    mockUseClusterMembers.mockReturnValue({ data: mockMembers, isLoading: false, error: null, refetch: refetchMembers })
    render(<ClusterPage />)

    const refreshBtn = screen.getByRole('button', { name: /^refresh$/i })
    fireEvent.click(refreshBtn)

    expect(refetchStatus).toHaveBeenCalled()
    expect(refetchMembers).toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalledWith('Cluster status refreshed')
  })

  it('shows raft state in logs tab', async () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /raft logs/i }))

    await waitFor(() => {
      expect(screen.getByText('Raft State')).toBeInTheDocument()
    })
    expect(screen.getByText('Current Raft consensus state')).toBeInTheDocument()
  })

  it('shows gossip members in gossip tab', async () => {
    setupLoadedCluster()
    render(<ClusterPage />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: /swim gossip/i }))

    await waitFor(() => {
      expect(screen.getByText('SWIM gossip protocol membership')).toBeInTheDocument()
    })
  })
})
