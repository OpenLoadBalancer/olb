export interface Backend {
  id: string
  address: string
  weight: number
  status: 'up' | 'down' | 'draining' | 'starting'
  health: 'healthy' | 'unhealthy' | 'unknown'
  response_time_ms: number
  active_connections: number
  total_requests: number
}

export interface Pool {
  id: string
  name: string
  algorithm: string
  backends: Backend[]
  health_check?: {
    enabled?: boolean
    type: string
    path: string
    interval: string
  }
  total_requests: number
  active_connections: number
}

export interface Listener {
  id: string
  name: string
  address: string
  protocol: 'http' | 'https' | 'tcp' | 'udp'
  routes: Route[]
  enabled: boolean
}

export interface Route {
  id: string
  path: string
  pool: string
  methods: string[]
  strip_prefix: boolean
  priority: number
}

export interface SystemStatus {
  version: string
  commit: string
  build_date: string
  uptime: string
  state: 'stopped' | 'starting' | 'running' | 'stopping'
  go_version: string
}

export interface HealthStatus {
  status: 'healthy' | 'unhealthy'
  checks: Record<string, {
    status: string
    message: string
  }>
  timestamp: string
}

// API response types matching server-side admin/types.go
export interface APIPoolInfo {
  name: string
  algorithm: string
  backends: APIBackendInfo[]
  healthy_count?: number
  health_check?: {
    type: string
    path: string
    interval: string
    timeout: string
  }
}

export interface APIBackendInfo {
  id: string
  address: string
  weight: number
  state: string
  healthy: boolean
  requests: number
  errors: number
}

export interface APIRouteInfo {
  name: string
  host: string
  path: string
  methods: string[]
  headers: Record<string, string>
  backend_pool: string
  priority: number
}

export interface APICertificateInfo {
  names: string[]
  expiry: string
  is_wildcard: boolean
}

export interface APIWAFStatus {
  enabled: boolean
  mode?: string
  [key: string]: any
}

export interface APIClusterStatus {
  node_id: string
  state: string
  leader: string
  peers: string[]
  applied_index: number
  commit_index: number
  term: number
  vote: string
}

export interface APIClusterMember {
  id: string
  address: string
  state: string
}

export interface APIMiddlewareStatusItem {
  id: string
  name: string
  description: string
  enabled: boolean
  category: string
}

export interface APIEventItem {
  id: string
  type: string
  message: string
  timestamp: string
}
