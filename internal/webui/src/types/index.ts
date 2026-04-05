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
