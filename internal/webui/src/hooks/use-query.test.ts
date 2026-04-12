import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useQuery, useMutation, useToastMutation } from '@/hooks/use-query'
import { APIError } from '@/lib/api'

// Mock sonner toast
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

import { toast } from 'sonner'

// Helper to create a successful query function
function successQueryFn<T>(data: T) {
  return vi.fn(() => Promise.resolve({ success: true, data }))
}

// Helper to create a failing query function
function failingQueryFn(status: number, message: string) {
  return vi.fn(() => Promise.reject(new APIError(status, message)))
}

describe('useQuery', () => {
  // --- Tests using real timers (waitFor-compatible) ---

  it('fetches data successfully', async () => {
    const queryFn = successQueryFn({ status: 'healthy' })

    const { result } = renderHook(() => useQuery(queryFn))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(result.current.data).toEqual({ status: 'healthy' })
    expect(result.current.error).toBeNull()
    expect(queryFn).toHaveBeenCalledTimes(1)
  })

  it('starts with loading state', () => {
    const queryFn = successQueryFn({ status: 'ok' })
    const { result } = renderHook(() => useQuery(queryFn))
    expect(result.current.isLoading).toBe(true)
    expect(result.current.data).toBeNull()
  })

  it('sets error on non-transient failure (no retry)', async () => {
    const queryFn = failingQueryFn(500, 'Internal Server Error')

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(result.current.error).toBeInstanceOf(APIError)
    expect((result.current.error as APIError).status).toBe(500)
    // Non-transient: should only call once (no retries)
    expect(queryFn).toHaveBeenCalledTimes(1)
  })

  it('does not retry on 401 Unauthorized', async () => {
    const queryFn = failingQueryFn(401, 'Unauthorized')

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(queryFn).toHaveBeenCalledTimes(1)
    expect((result.current.error as APIError).status).toBe(401)
  })

  it('does not retry on 404 Not Found', async () => {
    const queryFn = failingQueryFn(404, 'Not Found')

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(queryFn).toHaveBeenCalledTimes(1)
    expect((result.current.error as APIError).status).toBe(404)
  })

  it('does not fetch when enabled is false', () => {
    const queryFn = successQueryFn({ status: 'ok' })

    renderHook(() => useQuery(queryFn, { enabled: false }))

    expect(queryFn).not.toHaveBeenCalled()
  })

  it('calls onSuccess callback', async () => {
    const onSuccess = vi.fn()
    const data = { status: 'healthy' }
    const queryFn = successQueryFn(data)

    renderHook(() => useQuery(queryFn, { onSuccess }))

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith(data)
    })
  })

  it('calls onError callback', async () => {
    const onError = vi.fn()
    const queryFn = failingQueryFn(500, 'Server Error')

    renderHook(() => useQuery(queryFn, { onError }))

    await waitFor(() => {
      expect(onError).toHaveBeenCalled()
    })

    expect(onError.mock.calls[0][0]).toBeInstanceOf(APIError)
  })

  it('provides refetch function that re-calls queryFn', async () => {
    const queryFn = successQueryFn({ count: 1 })

    const { result } = renderHook(() => useQuery(queryFn))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    })

    expect(queryFn).toHaveBeenCalledTimes(1)

    await act(async () => {
      await result.current.refetch()
    })

    expect(queryFn).toHaveBeenCalledTimes(2)
  })

  it('retries on 502 Bad Gateway (transient)', async () => {
    const queryFn = vi.fn()
    queryFn
      .mockRejectedValueOnce(new APIError(502, 'Bad Gateway'))
      .mockResolvedValueOnce({ success: true, data: { status: 'ok' } })

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    // Should succeed after 1 retry
    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    }, { timeout: 5000 })

    expect(queryFn).toHaveBeenCalledTimes(2)
    expect(result.current.data).toEqual({ status: 'ok' })
  })

  it('retries on 503 Service Unavailable (transient)', async () => {
    const queryFn = vi.fn()
    queryFn
      .mockRejectedValueOnce(new APIError(503, 'Service Unavailable'))
      .mockResolvedValueOnce({ success: true, data: { recovered: true } })

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    }, { timeout: 5000 })

    expect(result.current.data).toEqual({ recovered: true })
  })

  it('retries on 504 Gateway Timeout (transient)', async () => {
    const queryFn = vi.fn()
    queryFn
      .mockRejectedValueOnce(new APIError(504, 'Gateway Timeout'))
      .mockResolvedValueOnce({ success: true, data: { ok: true } })

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    }, { timeout: 5000 })

    expect(result.current.data).toEqual({ ok: true })
  })

  it('retries on network failure (TypeError)', async () => {
    const queryFn = vi.fn()
    queryFn
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce({ success: true, data: { back: true } })

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    }, { timeout: 5000 })

    expect(result.current.data).toEqual({ back: true })
  })

  it('exhausts retries and sets error', async () => {
    const queryFn = vi.fn()
    queryFn
      .mockRejectedValueOnce(new APIError(502, 'Bad Gateway'))
      .mockRejectedValueOnce(new APIError(502, 'Bad Gateway'))
      .mockRejectedValueOnce(new APIError(502, 'Bad Gateway'))
      .mockRejectedValueOnce(new APIError(502, 'Bad Gateway'))

    const { result } = renderHook(() => useQuery(queryFn, { retryCount: 3 }))

    // Need enough timeout for all 3 retries with their delays (1s + 2s + 4s = 7s)
    await waitFor(() => {
      expect(result.current.isLoading).toBe(false)
    }, { timeout: 15000 })

    // Initial attempt + 3 retries = 4 calls
    expect(queryFn).toHaveBeenCalledTimes(4)
    expect(result.current.error).toBeInstanceOf(APIError)
    expect((result.current.error as APIError).status).toBe(502)
  }, 20000)
})

describe('useMutation', () => {
  it('executes mutation and returns data', async () => {
    const mutationFn = vi.fn(() => Promise.resolve({ message: 'ok' }))

    const { result } = renderHook(() => useMutation(mutationFn))

    let mutationResult: any
    await act(async () => {
      mutationResult = await result.current.mutate({ pool: 'web', id: 'b1' })
    })

    expect(mutationFn).toHaveBeenCalledWith({ pool: 'web', id: 'b1' })
    expect(mutationResult).toEqual({ message: 'ok' })
    expect(result.current.data).toEqual({ message: 'ok' })
    expect(result.current.isLoading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('sets error on mutation failure', async () => {
    const mutationFn = vi.fn(() => Promise.reject(new APIError(400, 'Bad Request')))

    const { result } = renderHook(() => useMutation(mutationFn))

    await act(async () => {
      try {
        await result.current.mutate({ bad: true })
      } catch (err) {
        // Expected
      }
    })

    expect(result.current.error).toBeInstanceOf(APIError)
    expect((result.current.error as APIError).status).toBe(400)
    expect(result.current.isLoading).toBe(false)
  })

  it('starts with loading false (mutations are lazy)', () => {
    const mutationFn = vi.fn(() => Promise.resolve({}))
    const { result } = renderHook(() => useMutation(mutationFn))
    expect(result.current.isLoading).toBe(false)
  })

  it('calls onSuccess callback', async () => {
    const onSuccess = vi.fn()
    const mutationFn = vi.fn(() => Promise.resolve({ id: 'b1' }))

    const { result } = renderHook(() => useMutation(mutationFn, { onSuccess }))

    await act(async () => {
      await result.current.mutate({ id: 'b1' })
    })

    expect(onSuccess).toHaveBeenCalledWith({ id: 'b1' }, { id: 'b1' })
  })

  it('calls onError callback', async () => {
    const onError = vi.fn()
    const mutationFn = vi.fn(() => Promise.reject(new Error('Network error')))

    const { result } = renderHook(() => useMutation(mutationFn, { onError }))

    await act(async () => {
      try {
        await result.current.mutate({})
      } catch {
        // Expected
      }
    })

    expect(onError).toHaveBeenCalled()
    expect(onError.mock.calls[0][0].message).toBe('Network error')
  })
})

describe('useToastMutation', () => {
  beforeEach(() => {
    vi.mocked(toast.success).mockClear()
    vi.mocked(toast.error).mockClear()
  })

  it('shows success toast on success', async () => {
    const mutationFn = vi.fn(() => Promise.resolve({ message: 'done' }))

    const { result } = renderHook(() =>
      useToastMutation(mutationFn, { successMessage: 'Operation succeeded' })
    )

    await act(async () => {
      await result.current.mutate({})
    })

    expect(toast.success).toHaveBeenCalledWith('Operation succeeded')
  })

  it('shows dynamic success message from result', async () => {
    const mutationFn = vi.fn(() => Promise.resolve({ name: 'backend-1' }))

    const { result } = renderHook(() =>
      useToastMutation(mutationFn, {
        successMessage: (data: any) => `Created ${data.name}`,
      })
    )

    await act(async () => {
      await result.current.mutate({})
    })

    expect(toast.success).toHaveBeenCalledWith('Created backend-1')
  })

  it('shows error toast on failure', async () => {
    const mutationFn = vi.fn(() => Promise.reject(new Error('Something went wrong')))

    const { result } = renderHook(() =>
      useToastMutation(mutationFn, { errorMessage: 'Operation failed' })
    )

    await act(async () => {
      try {
        await result.current.mutate({})
      } catch {
        // Expected
      }
    })

    expect(toast.error).toHaveBeenCalledWith('Operation failed')
  })

  it('shows dynamic error message from error', async () => {
    const mutationFn = vi.fn(() => Promise.reject(new Error('Connection refused')))

    const { result } = renderHook(() =>
      useToastMutation(mutationFn, {
        errorMessage: (err: Error) => `Failed: ${err.message}`,
      })
    )

    await act(async () => {
      try {
        await result.current.mutate({})
      } catch {
        // Expected
      }
    })

    expect(toast.error).toHaveBeenCalledWith('Failed: Connection refused')
  })

  it('falls back to error.message when no errorMessage provided', async () => {
    const mutationFn = vi.fn(() => Promise.reject(new Error('Default error message')))

    const { result } = renderHook(() => useToastMutation(mutationFn))

    await act(async () => {
      try {
        await result.current.mutate({})
      } catch {
        // Expected
      }
    })

    expect(toast.error).toHaveBeenCalledWith('Default error message')
  })
})
