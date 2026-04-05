export interface Backend {
  id: string
  address: string
  weight: number
  max_connections: number
  current_connections: number
  active_connections?: number
  status: 'up' | 'down' | 'draining' | 'starting'
  health: 'healthy' | 'unhealthy' | 'unknown'
  last_check: string
  response_time_ms: number
  response_time?: number
  error_rate: number
  bytes_sent: number
  bytes_received: number
  requests_total: number
}

export interface Pool {
  id: string
  name: string
  algorithm: 'round_robin' | 'least_connections' | 'ip_hash' | 'weighted_round_robin' | 'random'
  backends: Backend[]
  health_check: HealthCheck
  total_requests: number
  active_connections: number
  avg_response_time: number
  error_rate: number
}

export interface HealthCheck {
  type: 'http' | 'tcp'
  path: string
  interval: string
  timeout: string
  healthy_threshold: number
  unhealthy_threshold: number
}

export interface Route {
  id: string
  path: string
  pool: string
  pool_name?: string
  methods: string[]
  strip_prefix: boolean
  rewrite: RewriteRule[]
  middleware: string[]
  priority: number
  enabled?: boolean
  request_count?: number
}

export interface RewriteRule {
  pattern: string
  replacement: string
  flag: string
}

export interface Listener {
  id: string
  name: string
  address: string
  protocol: 'http' | 'https' | 'tcp' | 'udp'
  routes: Route[]
  tls?: TLSConfig
  enabled: boolean
}

export interface TLSConfig {
  cert_file: string
  key_file: string
  client_auth: 'none' | 'request' | 'require'
  client_ca_file?: string
}

export interface Metrics {
  timestamp: string
  requests_total: number
  requests_per_second: number
  active_connections: number
  bytes_sent: number
  bytes_received: number
  bytes_per_second?: number
  avg_response_time: number
  error_rate: number
  status_codes: Record<number, number>
  top_pools: PoolMetrics[]
  top_backends: BackendMetrics[]
}

export interface PoolMetrics {
  pool_id: string
  pool_name: string
  requests: number
  avg_response_time: number
  error_rate: number
}

export interface BackendMetrics {
  backend_id: string
  backend_address: string
  pool_name: string
  requests: number
  avg_response_time: number
  error_rate: number
  health: string
}

export interface LogEntry {
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  message: string
  fields: Record<string, unknown>
}

export interface Config {
  version: string
  listeners: Listener[]
  pools: Pool[]
  middleware: MiddlewareConfig
  tls: TLSConfig2
  admin: AdminConfig
}

export interface TLSConfig2 {
  certificates: Certificate[]
  auto_cert: boolean
  acme_email?: string
  acme_directory?: string
}

export interface Certificate {
  id: string
  domain: string
  issuer: string
  not_before: string
  not_after: string
  days_until_expiry: number
  auto_renew: boolean
}

export interface AdminConfig {
  address: string
  enabled: boolean
  auth: AuthConfig
}

export interface AuthConfig {
  type: 'none' | 'basic' | 'token'
  username?: string
  token_hash?: string
}

export interface MiddlewareConfig {
  rate_limit?: RateLimitConfig
  cors?: CORSConfig
  cache?: CacheConfig
  [key: string]: unknown
}

export interface RateLimitConfig {
  enabled: boolean
  requests_per_second: number
  burst_size: number
}

export interface CORSConfig {
  enabled: boolean
  allowed_origins: string[]
  allowed_methods: string[]
  allowed_headers: string[]
}

export interface CacheConfig {
  enabled: boolean
  ttl: string
  max_entries: number
}

export interface WAFStats {
  enabled: boolean
  mode: 'enforce' | 'monitor' | 'disabled'
  total_requests: number
  blocked_requests: number
  flagged_requests: number
  top_threats: Threat[]
  top_countries: CountryStat[]
}

export interface Threat {
  type: string
  count: number
  severity: 'low' | 'medium' | 'high' | 'critical'
}

export interface CountryStat {
  country: string
  requests: number
  blocked: number
}

export interface EngineStatus {
  state: 'stopped' | 'starting' | 'running' | 'stopping' | 'reloading'
  uptime: string
  version: string
  go_version: string
  listeners: number
  pools: number
  backends: number
  active_connections: number
  memory_usage: MemoryUsage
}

export interface MemoryUsage {
  alloc: number
  total_alloc: number
  sys: number
  num_gc: number
}
