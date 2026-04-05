// Mock API responses for development
// This file provides mock data when the real API is not available

import type { Backend, Pool, Listener, Route, Metrics, Certificate } from '@/types'

export const mockBackends: Backend[] = [
  {
    id: 'backend-1',
    address: 'localhost:3001',
    weight: 1,
    max_connections: 1000,
    current_connections: 45,
    active_connections: 45,
    status: 'up',
    health: 'healthy',
    last_check: new Date().toISOString(),
    response_time_ms: 12,
    response_time: 12,
    error_rate: 0.01,
    bytes_sent: 1024000,
    bytes_received: 2048000,
    requests_total: 15234
  },
  {
    id: 'backend-2',
    address: 'localhost:3002',
    weight: 1,
    max_connections: 1000,
    current_connections: 38,
    active_connections: 38,
    status: 'up',
    health: 'healthy',
    last_check: new Date().toISOString(),
    response_time_ms: 15,
    response_time: 15,
    error_rate: 0.02,
    bytes_sent: 890000,
    bytes_received: 1780000,
    requests_total: 12890
  },
  {
    id: 'backend-3',
    address: 'localhost:3003',
    weight: 2,
    max_connections: 2000,
    current_connections: 0,
    active_connections: 0,
    status: 'down',
    health: 'unhealthy',
    last_check: new Date(Date.now() - 300000).toISOString(),
    response_time_ms: 0,
    response_time: 0,
    error_rate: 1,
    bytes_sent: 0,
    bytes_received: 0,
    requests_total: 5000
  }
]

export const mockPools: Pool[] = [
  {
    id: 'pool-1',
    name: 'web-servers',
    algorithm: 'round_robin',
    backends: ['backend-1', 'backend-2', 'backend-3'],
    health_check: {
      type: 'http',
      path: '/health',
      interval: '10s',
      timeout: '5s',
      healthy_threshold: 2,
      unhealthy_threshold: 3
    },
    total_requests: 28124,
    active_connections: 83,
    avg_response_time: 13.5,
    error_rate: 0.015
  },
  {
    id: 'pool-2',
    name: 'api-servers',
    algorithm: 'least_connections',
    backends: ['backend-1', 'backend-2'],
    health_check: {
      type: 'http',
      path: '/api/health',
      interval: '5s',
      timeout: '3s',
      healthy_threshold: 2,
      unhealthy_threshold: 3
    },
    total_requests: 15234,
    active_connections: 45,
    avg_response_time: 8.2,
    error_rate: 0.005
  }
]

export const mockListeners: Listener[] = [
  {
    id: 'listener-1',
    name: 'http-public',
    address: ':8080',
    protocol: 'http',
    routes: [
      {
        id: 'route-1',
        path: '/api/*',
        pool: 'api-servers',
        methods: ['GET', 'POST', 'PUT', 'DELETE'],
        strip_prefix: false,
        rewrite: [],
        middleware: ['rate_limit', 'cors'],
        priority: 10
      },
      {
        id: 'route-2',
        path: '/*',
        pool: 'web-servers',
        methods: ['GET', 'HEAD'],
        strip_prefix: false,
        rewrite: [],
        middleware: ['compression'],
        priority: 1
      }
    ],
    enabled: true
  },
  {
    id: 'listener-2',
    name: 'https-public',
    address: ':8443',
    protocol: 'https',
    routes: [
      {
        id: 'route-3',
        path: '/*',
        pool: 'web-servers',
        methods: ['GET', 'POST', 'PUT', 'DELETE'],
        strip_prefix: false,
        rewrite: [],
        middleware: ['compression', 'security_headers'],
        priority: 1
      }
    ],
    tls: {
      cert_file: '/etc/olb/certs/cert.pem',
      key_file: '/etc/olb/certs/key.pem',
      client_auth: 'none'
    },
    enabled: true
  }
]

export const mockRoutes: Route[] = [
  {
    id: 'route-1',
    path: '/api/*',
    pool: 'api-servers',
    methods: ['GET', 'POST', 'PUT', 'DELETE'],
    strip_prefix: false,
    rewrite: [],
    middleware: ['rate_limit', 'cors'],
    priority: 10,
    enabled: true,
    request_count: 15234
  },
  {
    id: 'route-2',
    path: '/*',
    pool: 'web-servers',
    methods: ['GET', 'HEAD'],
    strip_prefix: false,
    rewrite: [],
    middleware: ['compression'],
    priority: 1,
    enabled: true,
    request_count: 28124
  }
]

export const mockMetrics: Metrics = {
  timestamp: new Date().toISOString(),
  requests_total: 43358,
  requests_per_second: 45.2,
  active_connections: 83,
  bytes_sent: 52428800,
  bytes_received: 104857600,
  bytes_per_second: 102400,
  avg_response_time: 13.5,
  error_rate: 0.015,
  status_codes: {
    200: 42000,
    201: 500,
    301: 200,
    404: 150,
    500: 8
  },
  top_pools: [
    { pool_id: 'pool-1', pool_name: 'web-servers', requests: 28124, avg_response_time: 13.5, error_rate: 0.015 },
    { pool_id: 'pool-2', pool_name: 'api-servers', requests: 15234, avg_response_time: 8.2, error_rate: 0.005 }
  ],
  top_backends: [
    { backend_id: 'backend-1', backend_address: 'localhost:3001', pool_name: 'web-servers', requests: 15234, avg_response_time: 12, error_rate: 0.01, health: 'healthy' },
    { backend_id: 'backend-2', backend_address: 'localhost:3002', pool_name: 'web-servers', requests: 12890, avg_response_time: 15, error_rate: 0.02, health: 'healthy' }
  ]
}

export const mockCertificates: Certificate[] = [
  {
    id: 'cert-1',
    domain: 'openloadbalancer.dev',
    issuer: "Let's Encrypt Authority X3",
    issued_at: new Date(Date.now() - 86400000 * 30).toISOString(),
    expires_at: new Date(Date.now() + 86400000 * 60).toISOString(),
    status: 'active',
    auto_renew: true,
    type: 'letsencrypt'
  },
  {
    id: 'cert-2',
    domain: '*.openloadbalancer.dev',
    issuer: "Let's Encrypt Authority X3",
    issued_at: new Date(Date.now() - 86400000 * 45).toISOString(),
    expires_at: new Date(Date.now() + 86400000 * 45).toISOString(),
    status: 'expiring_soon',
    auto_renew: true,
    type: 'letsencrypt'
  }
]

// Mock API handlers
export function setupMockAPI() {
  const originalFetch = window.fetch

  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = input.toString()
    const method = init?.method || 'GET'

    // Simulate network delay
    await new Promise(resolve => setTimeout(resolve, 100))

    // Backends endpoints
    if (url.includes('/api/v1/backends')) {
      if (method === 'GET') {
        return createMockResponse(mockBackends)
      }
      if (method === 'POST') {
        const body = JSON.parse(init?.body as string)
        const newBackend: Backend = {
          id: `backend-${Date.now()}`,
          address: body.address,
          weight: body.weight,
          max_connections: body.max_connections,
          current_connections: 0,
          active_connections: 0,
          status: 'starting',
          health: 'unknown',
          last_check: new Date().toISOString(),
          response_time_ms: 0,
          response_time: 0,
          error_rate: 0,
          bytes_sent: 0,
          bytes_received: 0,
          requests_total: 0
        }
        mockBackends.push(newBackend)
        return createMockResponse(newBackend, 201)
      }
    }

    // Pools endpoints
    if (url.includes('/api/v1/pools')) {
      if (method === 'GET') {
        return createMockResponse(mockPools)
      }
      return createMockResponse({ error: 'Not implemented' }, 501)
    }

    // Listeners endpoints
    if (url.includes('/api/v1/listeners')) {
      if (method === 'GET') {
        return createMockResponse(mockListeners)
      }
      return createMockResponse({ error: 'Not implemented' }, 501)
    }

    // Routes endpoints
    if (url.includes('/api/v1/routes')) {
      if (method === 'GET') {
        return createMockResponse(mockRoutes)
      }
      return createMockResponse({ error: 'Not implemented' }, 501)
    }

    // Metrics endpoint
    if (url.includes('/api/v1/metrics')) {
      // Simulate changing metrics
      mockMetrics.requests_per_second = 40 + Math.random() * 20
      mockMetrics.active_connections = 70 + Math.floor(Math.random() * 30)
      return createMockResponse(mockMetrics)
    }

    // Certificates endpoint
    if (url.includes('/api/v1/certificates')) {
      return createMockResponse(mockCertificates)
    }

    // Config endpoint
    if (url.includes('/api/v1/config')) {
      return createMockResponse({
        version: '1.0.0',
        listeners: mockListeners,
        pools: mockPools,
        middleware: {}
      })
    }

    // Fallback to original fetch
    return originalFetch(input, init)
  }
}

function createMockResponse(data: any, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      'Content-Type': 'application/json'
    }
  })
}
