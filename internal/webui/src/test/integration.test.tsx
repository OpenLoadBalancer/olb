import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { render, screen, waitFor as waitForDom } from '@/test/utils'
import { useMutation, useToastMutation, useQuery } from '@/hooks/use-query'
import { api, APIError } from '@/lib/api'

// Mock sonner toast
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
  },
}))

import { toast } from 'sonner'

// ---------------------------------------------------------------------------
// API Mutation Integration Tests
// ---------------------------------------------------------------------------

describe('API Mutation Integration', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  it('useMutation handles successful POST mutation', async () => {
    const mockResponse = { success: true, data: { id: 'be-new', address: '10.0.0.5:8080' } }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(mockResponse),
        text: () => Promise.resolve(''),
      } as Response)
    ))

    const onSuccess = vi.fn()
    const { result } = renderHook(() =>
      useMutation(
        (req: { address: string }) => api.addBackend('web-pool', { id: 'be-new', address: req.address }),
        { onSuccess }
      )
    )

    await act(async () => {
      await result.current.mutate({ address: '10.0.0.5:8080' })
    })

    expect(result.current.data).toEqual(mockResponse)
    expect(result.current.error).toBeNull()
    expect(result.current.isLoading).toBe(false)
    expect(onSuccess).toHaveBeenCalledWith(mockResponse, { address: '10.0.0.5:8080' })
  })

  it('useMutation handles failed mutation (400 Bad Request)', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: false,
        status: 400,
        json: () => Promise.resolve({ success: false, error: { code: 'INVALID', message: 'Bad address' } }),
        text: () => Promise.resolve('Bad address'),
      } as Response)
    ))

    const onError = vi.fn()
    const { result } = renderHook(() =>
      useMutation(
        () => api.addBackend('bad-pool', { id: 'x', address: '' }),
        { onError }
      )
    )

    await act(async () => {
      try {
        await result.current.mutate(undefined as any)
      } catch {
        // Expected to throw
      }
    })

    expect(result.current.error).toBeInstanceOf(APIError)
    expect((result.current.error as APIError).status).toBe(400)
    expect(onError).toHaveBeenCalled()
  })

  it('useMutation handles network failure', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))))

    const { result } = renderHook(() =>
      useMutation(() => api.removeBackend('pool', 'backend-1'))
    )

    await act(async () => {
      try {
        await result.current.mutate(undefined as any)
      } catch {
        // Expected
      }
    })

    expect(result.current.error).toBeInstanceOf(TypeError)
    expect(result.current.error?.message).toBe('Failed to fetch')
  })

  it('useToastMutation shows success toast on success', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ success: true, data: { message: 'draining' } }),
        text: () => Promise.resolve(''),
      } as Response)
    ))

    const { result } = renderHook(() =>
      useToastMutation(
        () => api.drainBackend('pool', 'be-1'),
        {
          successMessage: 'Backend drained',
          errorMessage: 'Failed to drain',
        }
      )
    )

    await act(async () => {
      await result.current.mutate(undefined as any)
    })

    expect(toast.success).toHaveBeenCalledWith('Backend drained')
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('useToastMutation shows error toast on failure', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: false,
        status: 404,
        json: () => Promise.resolve({ success: false, error: { code: 'NOT_FOUND', message: 'Backend not found' } }),
        text: () => Promise.resolve('Backend not found'),
      } as Response)
    ))

    const { result } = renderHook(() =>
      useToastMutation(
        () => api.removeBackend('pool', 'nonexistent'),
        {
          successMessage: 'Removed',
          errorMessage: 'Failed to remove',
        }
      )
    )

    await act(async () => {
      try {
        await result.current.mutate(undefined as any)
      } catch {
        // Expected
      }
    })

    expect(toast.error).toHaveBeenCalledWith('Failed to remove')
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('useMutation handles DELETE with 200 response', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ success: true, data: { message: 'Backend removed' } }),
        text: () => Promise.resolve(''),
      } as Response)
    ))

    const { result } = renderHook(() =>
      useMutation(() => api.removeBackend('web-pool', 'be-old'))
    )

    await act(async () => {
      await result.current.mutate(undefined as any)
    })

    expect(result.current.data).toEqual({ success: true, data: { message: 'Backend removed' } })
    expect(result.current.isLoading).toBe(false)
  })

  it('useMutation handles PATCH with 200 response', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ success: true, data: { id: 'be-1', weight: 50 } }),
        text: () => Promise.resolve(''),
      } as Response)
    ))

    const { result } = renderHook(() =>
      useMutation(() => api.updateBackend('web-pool', 'be-1', { weight: 50 }))
    )

    await act(async () => {
      await result.current.mutate(undefined as any)
    })

    expect(result.current.data).toEqual({ success: true, data: { id: 'be-1', weight: 50 } })
  })
})

// ---------------------------------------------------------------------------
// Query Error Handling Integration
// ---------------------------------------------------------------------------

describe('Query Error Handling Integration', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  it('useQuery does NOT retry on 404 (non-transient)', async () => {
    const queryFn = vi.fn(() => {
      throw new APIError(404, 'Not Found')
    })

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 5 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(queryFn).toHaveBeenCalledTimes(1) // No retries for 404
    expect(result.current.error).toBeInstanceOf(APIError)
    expect((result.current.error as APIError).status).toBe(404)
  })

  it('useQuery does NOT retry on 500 (non-transient)', async () => {
    const queryFn = vi.fn(() => {
      throw new APIError(500, 'Internal Server Error')
    })

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 5 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(queryFn).toHaveBeenCalledTimes(1) // No retries for 500
  })
})

// ---------------------------------------------------------------------------
// Error Boundary Behavior
// ---------------------------------------------------------------------------

describe('Error Boundary Behavior', () => {
  it('renders error state when fetch fails', async () => {
    // Create a simple component that shows error state
    function TestComponent() {
      const { data, error, isLoading, refetch } = useQuery(
        (signal) => api.getHealth(signal),
        { retryCount: 0 }
      )

      if (isLoading) return <div>Loading...</div>
      if (error) return (
        <div>
          <span role="alert">Error: {error.message}</span>
          <button onClick={() => refetch()}>Retry</button>
        </div>
      )
      return <div>Data: {JSON.stringify(data)}</div>
    }

    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ success: false, error: { code: 'ERROR', message: 'Internal Server Error' } }),
        text: () => Promise.resolve('Internal Server Error'),
      } as Response)
    ))

    render(<TestComponent />)

    await waitForDom(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })

    expect(screen.getByText(/Error: Internal Server Error/)).toBeInTheDocument()
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })
})
