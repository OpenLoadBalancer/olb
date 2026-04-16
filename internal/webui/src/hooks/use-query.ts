import { useState, useEffect, useCallback, useRef } from 'react'
import { api, APIError, MetricsData, BackendHealth } from '@/lib/api'
import { toast } from 'sonner'
import type {
  APIPoolInfo,
  APIRouteInfo,
  APICertificateInfo,
  APIWAFStatus,
  APIClusterStatus,
  APIClusterMember,
  APIMiddlewareStatusItem,
  APIEventItem,
} from '@/types'

interface UseQueryOptions<T> {
  onSuccess?: (data: T) => void
  onError?: (error: Error) => void
  enabled?: boolean
  refetchInterval?: number
  retryCount?: number
}

interface QueryResult<T> {
  data: T | null
  isLoading: boolean
  error: Error | null
  refetch: () => Promise<void>
}

const MAX_RETRIES = 3
const RETRY_DELAYS = [1000, 2000, 4000]
const TRANSIENT_STATUS_CODES = new Set([0, 502, 503, 504])

function isTransientError(err: unknown): boolean {
  if (err instanceof APIError) {
    return TRANSIENT_STATUS_CODES.has(err.status)
  }
  // Network failures (no response) manifest as TypeError
  if (err instanceof TypeError) {
    return true
  }
  return false
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms)
    signal?.addEventListener('abort', () => {
      clearTimeout(timer)
      reject(new DOMException('Aborted', 'AbortError'))
    }, { once: true })
  })
}

export function useQuery<T>(
  queryFn: (signal?: AbortSignal) => Promise<{ success: boolean; data?: T }>,
  options: UseQueryOptions<T> = {}
): QueryResult<T> {
  const { onSuccess, onError, enabled = true, refetchInterval, retryCount = MAX_RETRIES } = options
  const [data, setData] = useState<T | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const controllerRef = useRef<AbortController | null>(null)

  const fetch = useCallback(async () => {
    if (!enabled) return

    // Abort any previous in-flight request
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller

    try {
      setIsLoading(true)
      setError(null)

      let lastError: Error | null = null

      for (let attempt = 0; attempt <= retryCount; attempt++) {
        if (controller.signal.aborted) return

        try {
          const response = await queryFn(controller.signal)
          if (controller.signal.aborted) return
          if (response.success && response.data !== undefined) {
            setData(response.data)
            onSuccess?.(response.data)
            return
          }
        } catch (err) {
          if (controller.signal.aborted) return
          lastError = err instanceof Error ? err : new Error('Unknown error')

          // Only retry transient errors, and only if we have retries left
          if (attempt < retryCount && isTransientError(err)) {
            await sleep(RETRY_DELAYS[attempt] ?? 4000, controller.signal)
            continue
          }

          // Non-transient error or out of retries: stop retrying
          break
        }
      }

      // All attempts exhausted
      if (lastError && !controller.signal.aborted) {
        setError(lastError)
        onError?.(lastError)
      }
    } finally {
      if (!controller.signal.aborted) {
        setIsLoading(false)
      }
    }
  }, [queryFn, enabled, onSuccess, onError, retryCount])

  useEffect(() => {
    fetch()
    return () => { controllerRef.current?.abort() }
  }, [fetch])

  useEffect(() => {
    if (!refetchInterval || !enabled) return
    const interval = setInterval(fetch, refetchInterval)
    return () => clearInterval(interval)
  }, [fetch, refetchInterval, enabled])

  return { data, isLoading, error, refetch: fetch }
}

interface UseMutationOptions<T, V> {
  onSuccess?: (data: T, variables: V) => void
  onError?: (error: Error, variables: V) => void
}

interface MutationResult<T, V> {
  mutate: (variables: V) => Promise<T | undefined>
  isLoading: boolean
  error: Error | null
  data: T | null
}

export function useMutation<T, V = void>(
  mutationFn: (variables: V, signal?: AbortSignal) => Promise<T>,
  options: UseMutationOptions<T, V> = {}
): MutationResult<T, V> {
  const { onSuccess, onError } = options
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [data, setData] = useState<T | null>(null)
  const controllerRef = useRef<AbortController | null>(null)

  // Abort on unmount
  useEffect(() => {
    return () => { controllerRef.current?.abort() }
  }, [])

  const mutate = useCallback(async (variables: V): Promise<T | undefined> => {
    // Abort previous in-flight request
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller

    try {
      setIsLoading(true)
      setError(null)
      const result = await mutationFn(variables, controller.signal)
      if (controller.signal.aborted) return undefined
      setData(result)
      onSuccess?.(result, variables)
      return result
    } catch (err) {
      if (controller.signal.aborted) return undefined
      const error = err instanceof Error ? err : new Error('Unknown error')
      setError(error)
      onError?.(error, variables)
      throw error
    } finally {
      if (!controller.signal.aborted) {
        setIsLoading(false)
      }
    }
  }, [mutationFn, onSuccess, onError])

  return { mutate, isLoading, error, data }
}

// Health query hook
export function useHealth(options?: UseQueryOptions<{ status: string; checks: Record<string, { status: string; message?: string }>; timestamp: string }>) {
  return useQuery((signal) => api.getHealth(signal), {
    refetchInterval: 30000,
    ...options
  })
}

// System info query hook
export function useSystemInfo(options?: UseQueryOptions<{ version: string; commit: string; build_date: string; uptime: string; state: string; go_version: string }>) {
  return useQuery((signal) => api.getInfo(signal), {
    refetchInterval: 60000,
    ...options
  })
}

// Pools query hook
export function usePools(options?: UseQueryOptions<APIPoolInfo[]>) {
  return useQuery((signal) => api.getPools(signal), {
    refetchInterval: 10000,
    ...options
  })
}

// Routes query hook
export function useRoutes(options?: UseQueryOptions<APIRouteInfo[]>) {
  return useQuery((signal) => api.getRoutes(signal), {
    refetchInterval: 30000,
    ...options
  })
}

// Certificates query hook
export function useCertificates(options?: UseQueryOptions<APICertificateInfo[]>) {
  return useQuery((signal) => api.getCertificates(signal), {
    refetchInterval: 60000,
    ...options
  })
}

// WAF status query hook
export function useWAFStatus(options?: UseQueryOptions<APIWAFStatus>) {
  return useQuery((signal) => api.getWAFStatus(signal), {
    refetchInterval: 30000,
    ...options
  })
}

// Cluster status query hook
export function useClusterStatus(options?: UseQueryOptions<APIClusterStatus>) {
  return useQuery((signal) => api.getClusterStatus(signal), {
    refetchInterval: 10000,
    ...options
  })
}

// Cluster members query hook
export function useClusterMembers(options?: UseQueryOptions<APIClusterMember[]>) {
  return useQuery((signal) => api.getClusterMembers(signal), {
    refetchInterval: 10000,
    ...options
  })
}

// Config query hook
export function useConfig(options?: UseQueryOptions<Record<string, unknown>>) {
  return useQuery((signal) => api.getConfig(signal), {
    refetchInterval: 60000,
    ...options
  })
}

// Metrics query hook
export function useMetrics(options?: UseQueryOptions<MetricsData>) {
  return useQuery((signal) => api.getMetrics(signal), {
    refetchInterval: 15000,
    ...options
  })
}

// Health status (per-backend) query hook
export function useBackendHealth(options?: UseQueryOptions<BackendHealth[]>) {
  return useQuery((signal) => api.getHealthStatus(signal), {
    refetchInterval: 10000,
    ...options
  })
}

// Toast notifications for mutations
export function useToastMutation<T, V = void>(
  mutationFn: (variables: V) => Promise<T>,
  options: {
    loadingMessage?: string
    successMessage?: string | ((data: T) => string)
    errorMessage?: string | ((error: Error) => string)
  } & UseMutationOptions<T, V> = {}
): MutationResult<T, V> {
  const { loadingMessage: _loadingMessage, successMessage, errorMessage, ...rest } = options

  return useMutation(mutationFn, {
    ...rest,
    onSuccess: (data, variables) => {
      if (successMessage) {
        const message = typeof successMessage === 'function' ? successMessage(data) : successMessage
        toast.success(message)
      }
      options.onSuccess?.(data, variables)
    },
    onError: (error, variables) => {
      if (errorMessage) {
        const message = typeof errorMessage === 'function' ? errorMessage(error) : errorMessage
        toast.error(message)
      } else {
        toast.error(error.message || 'An error occurred')
      }
      options.onError?.(error, variables)
    },
  })
}

// Middleware status query hook
export function useMiddlewareStatus(options?: UseQueryOptions<APIMiddlewareStatusItem[]>) {
  return useQuery((signal) => api.getMiddlewareStatus(signal), {
    refetchInterval: 30000,
    ...options
  })
}

// Events query hook
export function useEvents(options?: UseQueryOptions<APIEventItem[]>) {
  return useQuery((signal) => api.getEvents(signal), {
    refetchInterval: 15000,
    ...options
  })
}
