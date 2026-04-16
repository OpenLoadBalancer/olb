interface ImportMetaEnv {
  VITE_API_URL?: string
}

interface ViteImportMeta {
  readonly env: ImportMetaEnv
}

const API_BASE = (import.meta as unknown as ViteImportMeta).env?.VITE_API_URL || ''

export class APIError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

async function fetchAPI<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}/api/v1${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })

  if (!response.ok) {
    throw new APIError(response.status, await response.text())
  }

  return response.json()
}

// Types matching server-side admin/types.go and handler responses
export interface APIResponse<T> {
  success: boolean
  data?: T
  error?: { code: string; message: string }
}

export interface SystemInfo {
  version: string
  commit: string
  build_date: string
  uptime: string
  state: string
  go_version: string
}

export interface HealthCheck {
  status: string
  message?: string
  count?: number
  total?: number
}

export interface HealthStatus {
  status: string
  checks: Record<string, HealthCheck>
  timestamp: string
}

export interface PoolInfo {
  name: string
  algorithm: string
  backends: BackendInfo[]
  healthy_count?: number
  health_check?: {
    type: string
    path: string
    interval: string
    timeout: string
  }
}

export interface BackendInfo {
  id: string
  address: string
  weight: number
  state: string
  healthy: boolean
  requests: number
  errors: number
}

export interface BackendDetail {
  id: string
  address: string
  weight: number
  max_conns: number
  state: string
  healthy: boolean
  active_conns: number
  total_requests: number
  total_errors: number
  total_bytes: number
  avg_latency: number
  last_latency: number
  metadata: Record<string, string>
}

export interface RouteInfo {
  name: string
  host: string
  path: string
  methods: string[]
  headers: Record<string, string>
  backend_pool: string
  priority: number
}

export interface CertificateInfo {
  names: string[]
  expiry: string
  is_wildcard: boolean
}

export interface WAFStatus {
  enabled: boolean
  mode?: string
  layers?: Record<string, boolean>
  stats?: {
    total_blocked?: number
    blocked?: number
    total_challenges?: number
    challenges?: number
    total_requests?: number
  }
  rules?: Record<string, unknown>
  detections?: Record<string, number>
  ip_acl?: { enabled: boolean; rules: string[] }
  rate_limit?: { enabled: boolean; requests_per_second: number; rules?: Array<Record<string, unknown>> }
  sanitizer?: { enabled: boolean }
  detection?: { enabled: boolean }
  bot_detection?: { enabled: boolean }
  response?: { security_headers?: { enabled: boolean } }
}

export interface ClusterStatus {
  node_id: string
  state: string
  leader: string
  peers: string[]
  applied_index: number
  commit_index: number
  term: number
  vote: string
}

export interface ClusterMember {
  id: string
  address: string
  state: string
}

export interface MetricsData {
  requests_total?: number
  errors_total?: number
  active_connections?: number
  bytes_in?: number
  bytes_out?: number
  avg_latency_ms?: number
  p99_latency_ms?: number
  pools?: Record<string, { requests: number; errors: number }>
  backends?: Record<string, { requests: number; errors: number; latency: number }>
  [key: string]: unknown
}

export interface BackendHealth {
  backend_id: string
  status: string
  last_check: string
  latency: number
  error: string
}

export interface AddBackendRequest {
  id: string
  address: string
  weight?: number
}

export interface UpdateBackendRequest {
  weight?: number
  max_conns?: number
}

export interface MiddlewareStatusItem {
  id: string
  name: string
  description: string
  enabled: boolean
  category: string
}

export interface EventItem {
  id: string
  type: string
  message: string
  timestamp: string
}

export const api = {
  // System
  getHealth: (signal?: AbortSignal) => fetchAPI<APIResponse<HealthStatus>>('/system/health', { signal }),
  getInfo: (signal?: AbortSignal) => fetchAPI<APIResponse<SystemInfo>>('/system/info', { signal }),
  reload: (signal?: AbortSignal) => fetchAPI<APIResponse<{ message: string }>>('/system/reload', { method: 'POST', signal }),

  // Version
  getVersion: (signal?: AbortSignal) => fetchAPI<APIResponse<SystemInfo>>('/version', { signal }),

  // Pools
  getPools: (signal?: AbortSignal) => fetchAPI<APIResponse<PoolInfo[]>>('/pools', { signal }),
  getPool: (name: string, signal?: AbortSignal) => fetchAPI<APIResponse<PoolInfo>>(`/pools/${encodeURIComponent(name)}`, { signal }),

  // Backends
  getBackends: (pool: string, signal?: AbortSignal) => fetchAPI<APIResponse<PoolInfo>>(`/backends/${encodeURIComponent(pool)}`, { signal }),
  getBackend: (pool: string, id: string, signal?: AbortSignal) => fetchAPI<APIResponse<BackendDetail>>(`/backends/${encodeURIComponent(pool)}/${encodeURIComponent(id)}`, { signal }),
  addBackend: (pool: string, req: AddBackendRequest, signal?: AbortSignal) =>
    fetchAPI<APIResponse<BackendInfo>>(`/backends/${encodeURIComponent(pool)}`, {
      method: 'POST',
      body: JSON.stringify(req),
      signal,
    }),
  updateBackend: (pool: string, id: string, req: UpdateBackendRequest, signal?: AbortSignal) =>
    fetchAPI<APIResponse<BackendInfo>>(`/backends/${encodeURIComponent(pool)}/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(req),
      signal,
    }),
  removeBackend: (pool: string, id: string, signal?: AbortSignal) =>
    fetchAPI<APIResponse<{ message: string }>>(`/backends/${encodeURIComponent(pool)}/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    }),
  drainBackend: (pool: string, id: string, signal?: AbortSignal) =>
    fetchAPI<APIResponse<{ message: string }>>(`/backends/${encodeURIComponent(pool)}/${encodeURIComponent(id)}/drain`, {
      method: 'POST',
      signal,
    }),

  // Routes
  getRoutes: (signal?: AbortSignal) => fetchAPI<APIResponse<RouteInfo[]>>('/routes', { signal }),

  // Health
  getHealthStatus: (signal?: AbortSignal) => fetchAPI<APIResponse<BackendHealth[]>>('/health', { signal }),

  // Metrics
  getMetrics: (signal?: AbortSignal) => fetchAPI<APIResponse<MetricsData>>('/metrics', { signal }),

  // Config
  getConfig: (signal?: AbortSignal) => fetchAPI<APIResponse<Record<string, unknown>>>('/config', { signal }),

  // Certificates
  getCertificates: (signal?: AbortSignal) => fetchAPI<APIResponse<CertificateInfo[]>>('/certificates', { signal }),

  // WAF
  getWAFStatus: (signal?: AbortSignal) => fetchAPI<APIResponse<WAFStatus>>('/waf/status', { signal }),

  // Cluster
  getClusterStatus: (signal?: AbortSignal) => fetchAPI<APIResponse<ClusterStatus>>('/cluster/status', { signal }),
  getClusterMembers: (signal?: AbortSignal) => fetchAPI<APIResponse<ClusterMember[]>>('/cluster/members', { signal }),
  joinCluster: (seedAddrs: string[], signal?: AbortSignal) =>
    fetchAPI<APIResponse<{ message: string }>>('/cluster/join', {
      method: 'POST',
      body: JSON.stringify({ seed_addrs: seedAddrs }),
      signal,
    }),
  leaveCluster: (signal?: AbortSignal) =>
    fetchAPI<APIResponse<{ message: string }>>('/cluster/leave', { method: 'POST', signal }),

  // Middleware status
  getMiddlewareStatus: (signal?: AbortSignal) => fetchAPI<APIResponse<MiddlewareStatusItem[]>>('/middleware/status', { signal }),

  // Events
  getEvents: (signal?: AbortSignal) => fetchAPI<APIResponse<EventItem[]>>('/events', { signal }),
}
