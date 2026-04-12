import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { api, APIError } from '@/lib/api'

// Helper to create a mock successful Response
function mockOkResponse(data: any): Response {
  return {
    ok: true,
    status: 200,
    json: () => Promise.resolve({ success: true, data }),
    text: () => Promise.resolve(''),
  } as Response
}

// Helper to create a mock error Response
function mockErrorResponse(status: number, message: string): Response {
  return {
    ok: false,
    status,
    json: () => Promise.resolve({ success: false, error: { code: 'ERROR', message } }),
    text: () => Promise.resolve(message),
  } as Response
}

describe('API client', () => {
  let mockFetch: ReturnType<typeof vi.fn>

  beforeEach(() => {
    mockFetch = vi.fn()
    vi.stubGlobal('fetch', mockFetch)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // --- System endpoints ---

  describe('system endpoints', () => {
    it('fetches system health', async () => {
      const healthData = { status: 'healthy', checks: {}, timestamp: '2026-04-11T00:00:00Z' }
      mockFetch.mockResolvedValueOnce(mockOkResponse(healthData))

      const result = await api.getHealth()
      expect(result.success).toBe(true)
      expect(result.data?.status).toBe('healthy')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/system/health'),
        expect.objectContaining({ headers: expect.objectContaining({ 'Content-Type': 'application/json' }) }),
      )
    })

    it('fetches system info', async () => {
      const infoData = { version: '0.1.0', commit: 'abc123', build_date: '2026-04-11', uptime: '1h', state: 'running', go_version: '1.26' }
      mockFetch.mockResolvedValueOnce(mockOkResponse(infoData))

      const result = await api.getInfo()
      expect(result.success).toBe(true)
      expect(result.data?.version).toBe('0.1.0')
      expect(result.data?.go_version).toBe('1.26')
    })

    it('fetches version', async () => {
      const versionData = { version: '0.1.0', commit: 'deadbeef', build_date: '2026-04-11', uptime: '2h', state: 'running', go_version: '1.26' }
      mockFetch.mockResolvedValueOnce(mockOkResponse(versionData))

      const result = await api.getVersion()
      expect(result.success).toBe(true)
      expect(result.data?.version).toBe('0.1.0')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/version'),
        expect.anything(),
      )
    })

    it('sends POST request for reload', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ message: 'Configuration reloaded' }))

      await api.reload()
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/system/reload'),
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  // --- Pool endpoints ---

  describe('pool endpoints', () => {
    it('fetches all pools', async () => {
      const poolsData = [
        { name: 'web', algorithm: 'round_robin', backends: [] },
        { name: 'api', algorithm: 'least_connections', backends: [] },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(poolsData))

      const result = await api.getPools()
      expect(result.success).toBe(true)
      expect(result.data).toHaveLength(2)
      expect(result.data?.[0].name).toBe('web')
    })

    it('fetches a single pool by name', async () => {
      const poolData = { name: 'web', algorithm: 'round_robin', backends: [{ id: 'b1', address: '10.0.0.1:8080' }] }
      mockFetch.mockResolvedValueOnce(mockOkResponse(poolData))

      const result = await api.getPool('web')
      expect(result.success).toBe(true)
      expect(result.data?.name).toBe('web')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/pools/web'),
        expect.anything(),
      )
    })

    it('URL-encodes pool name with special characters', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ name: 'api/v2', algorithm: 'round_robin', backends: [] }))

      await api.getPool('api/v2')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/pools/api%2Fv2'),
        expect.anything(),
      )
    })
  })

  // --- Backend endpoints ---

  describe('backend endpoints', () => {
    it('fetches backends for a pool', async () => {
      const poolData = { name: 'web', algorithm: 'round_robin', backends: [{ id: 'b1', address: '10.0.0.1:8080' }] }
      mockFetch.mockResolvedValueOnce(mockOkResponse(poolData))

      const result = await api.getBackends('web')
      expect(result.success).toBe(true)
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web'),
        expect.anything(),
      )
    })

    it('fetches a single backend detail', async () => {
      const detail = {
        id: 'b1', address: '10.0.0.1:8080', weight: 100, max_conns: 0,
        state: 'up', healthy: true, active_conns: 5, total_requests: 1000,
        total_errors: 3, total_bytes: 50000, avg_latency: 2.5, last_latency: 1.8,
        metadata: {},
      }
      mockFetch.mockResolvedValueOnce(mockOkResponse(detail))

      const result = await api.getBackend('web', 'b1')
      expect(result.success).toBe(true)
      expect(result.data?.address).toBe('10.0.0.1:8080')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web/b1'),
        expect.anything(),
      )
    })

    it('adds a backend with POST request', async () => {
      const newBackend = { id: 'new-1', address: '10.0.0.5:8080', weight: 100, state: 'up', healthy: true, requests: 0, errors: 0 }
      mockFetch.mockResolvedValueOnce(mockOkResponse(newBackend))

      await api.addBackend('web', { id: 'new-1', address: '10.0.0.5:8080' })
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web'),
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('10.0.0.5:8080'),
        }),
      )
    })

    it('updates a backend with PATCH request', async () => {
      const updated = { id: 'b1', address: '10.0.0.1:8080', weight: 50, state: 'up', healthy: true, requests: 0, errors: 0 }
      mockFetch.mockResolvedValueOnce(mockOkResponse(updated))

      await api.updateBackend('web', 'b1', { weight: 50, max_conns: 100 })
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web/b1'),
        expect.objectContaining({
          method: 'PATCH',
          body: expect.stringContaining('"weight":50'),
        }),
      )
    })

    it('removes a backend with DELETE request', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ message: 'Backend removed' }))

      await api.removeBackend('web', 'b1')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web/b1'),
        expect.objectContaining({ method: 'DELETE' }),
      )
    })

    it('drains a backend with POST request', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ message: 'Backend draining' }))

      await api.drainBackend('web', 'b1')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web/b1/drain'),
        expect.objectContaining({ method: 'POST' }),
      )
    })

    it('URL-encodes backend IDs with special characters', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ message: 'ok' }))

      await api.removeBackend('web', 'backend-1/special')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/backends/web/backend-1%2Fspecial'),
        expect.anything(),
      )
    })
  })

  // --- Route endpoints ---

  describe('route endpoints', () => {
    it('fetches all routes', async () => {
      const routesData = [
        { name: 'default', host: '', path: '/', methods: ['GET'], headers: {}, backend_pool: 'web', priority: 0 },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(routesData))

      const result = await api.getRoutes()
      expect(result.success).toBe(true)
      expect(result.data).toHaveLength(1)
      expect(result.data?.[0].backend_pool).toBe('web')
    })
  })

  // --- Health status endpoints ---

  describe('health endpoints', () => {
    it('fetches backend health status', async () => {
      const healthData = [
        { backend_id: 'b1', status: 'healthy', last_check: '2026-04-11T00:00:00Z', latency: 2.5, error: '' },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(healthData))

      const result = await api.getHealthStatus()
      expect(result.success).toBe(true)
      expect(result.data?.[0].backend_id).toBe('b1')
    })
  })

  // --- Metrics & config ---

  describe('metrics and config endpoints', () => {
    it('fetches metrics', async () => {
      const metricsData = { requests_total: 10000, requests_per_second: 150 }
      mockFetch.mockResolvedValueOnce(mockOkResponse(metricsData))

      const result = await api.getMetrics()
      expect(result.success).toBe(true)
      expect(result.data?.requests_total).toBe(10000)
    })

    it('fetches config', async () => {
      const configData = { listeners: [], pools: [], middleware: {} }
      mockFetch.mockResolvedValueOnce(mockOkResponse(configData))

      const result = await api.getConfig()
      expect(result.success).toBe(true)
      expect(result.data?.pools).toBeDefined()
    })
  })

  // --- Certificate endpoints ---

  describe('certificate endpoints', () => {
    it('fetches certificates', async () => {
      const certsData = [
        { names: ['example.com'], expiry: '2027-04-11T00:00:00Z', is_wildcard: false },
        { names: ['*.example.com'], expiry: '2027-06-11T00:00:00Z', is_wildcard: true },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(certsData))

      const result = await api.getCertificates()
      expect(result.success).toBe(true)
      expect(result.data).toHaveLength(2)
      expect(result.data?.[1].is_wildcard).toBe(true)
    })
  })

  // --- WAF endpoints ---

  describe('WAF endpoints', () => {
    it('fetches WAF status', async () => {
      const wafData = { enabled: true, mode: 'detect' }
      mockFetch.mockResolvedValueOnce(mockOkResponse(wafData))

      const result = await api.getWAFStatus()
      expect(result.success).toBe(true)
      expect(result.data?.enabled).toBe(true)
    })
  })

  // --- Cluster endpoints ---

  describe('cluster endpoints', () => {
    it('fetches cluster status', async () => {
      const clusterData = {
        node_id: 'node-1', state: 'leader', leader: 'node-1',
        peers: ['node-2', 'node-3'], applied_index: 42, commit_index: 42,
        term: 3, vote: 'node-1',
      }
      mockFetch.mockResolvedValueOnce(mockOkResponse(clusterData))

      const result = await api.getClusterStatus()
      expect(result.success).toBe(true)
      expect(result.data?.state).toBe('leader')
      expect(result.data?.peers).toHaveLength(2)
    })

    it('fetches cluster members', async () => {
      const membersData = [
        { id: 'node-1', address: '10.0.0.1:9090', state: 'leader' },
        { id: 'node-2', address: '10.0.0.2:9090', state: 'follower' },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(membersData))

      const result = await api.getClusterMembers()
      expect(result.success).toBe(true)
      expect(result.data).toHaveLength(2)
    })

    it('joins cluster with seed addresses', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ message: 'joined' }))

      await api.joinCluster(['10.0.0.1:9090', '10.0.0.2:9090'])
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/cluster/join'),
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('seed_addrs'),
        }),
      )
    })

    it('leaves cluster', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ message: 'left' }))

      await api.leaveCluster()
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/cluster/leave'),
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  // --- Middleware & events ---

  describe('middleware and events endpoints', () => {
    it('fetches middleware status', async () => {
      const mwData = [
        { id: 'rate_limit', name: 'Rate Limiter', description: 'Limits request rates', enabled: true, category: 'security' },
        { id: 'cors', name: 'CORS', description: 'Cross-origin headers', enabled: false, category: 'headers' },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(mwData))

      const result = await api.getMiddlewareStatus()
      expect(result.success).toBe(true)
      expect(result.data).toHaveLength(2)
      expect(result.data?.[0].enabled).toBe(true)
    })

    it('fetches events', async () => {
      const eventsData = [
        { id: 'evt-1', type: 'backend_down', message: 'Backend b1 is down', timestamp: '2026-04-11T00:00:00Z' },
      ]
      mockFetch.mockResolvedValueOnce(mockOkResponse(eventsData))

      const result = await api.getEvents()
      expect(result.success).toBe(true)
      expect(result.data?.[0].type).toBe('backend_down')
    })
  })

  // --- Error handling ---

  describe('error handling', () => {
    it('throws APIError with correct status on 401', async () => {
      mockFetch.mockResolvedValueOnce(mockErrorResponse(401, 'Unauthorized'))

      try {
        await api.getHealth()
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(APIError)
        expect((err as APIError).status).toBe(401)
        expect((err as APIError).message).toBe('Unauthorized')
      }
    })

    it('throws APIError with correct status on 404', async () => {
      mockFetch.mockResolvedValueOnce(mockErrorResponse(404, 'Pool not found'))

      try {
        await api.getPool('nonexistent')
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(APIError)
        expect((err as APIError).status).toBe(404)
        expect((err as APIError).message).toBe('Pool not found')
      }
    })

    it('throws APIError with correct status on 500', async () => {
      mockFetch.mockResolvedValueOnce(mockErrorResponse(500, 'Internal Server Error'))

      try {
        await api.getPools()
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(APIError)
        expect((err as APIError).status).toBe(500)
        expect((err as APIError).message).toBe('Internal Server Error')
      }
    })

    it('throws APIError on 502 Bad Gateway', async () => {
      mockFetch.mockResolvedValueOnce(mockErrorResponse(502, 'Bad Gateway'))

      try {
        await api.getHealth()
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(APIError)
        expect((err as APIError).status).toBe(502)
      }
    })

    it('throws APIError on 503 Service Unavailable', async () => {
      mockFetch.mockResolvedValueOnce(mockErrorResponse(503, 'Service Unavailable'))

      try {
        await api.getInfo()
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(APIError)
        expect((err as APIError).status).toBe(503)
      }
    })

    it('propagates network failure as TypeError', async () => {
      mockFetch.mockRejectedValue(new TypeError('Failed to fetch'))

      try {
        await api.getHealth()
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(TypeError)
        expect((err as TypeError).message).toBe('Failed to fetch')
      }
    })

    it('APIError has correct name property', async () => {
      mockFetch.mockResolvedValueOnce(mockErrorResponse(422, 'Validation failed'))

      try {
        await api.addBackend('web', { id: 'bad', address: '' })
        expect.fail('Should have thrown')
      } catch (err) {
        expect(err).toBeInstanceOf(APIError)
        expect((err as APIError).name).toBe('APIError')
      }
    })
  })

  // --- Request construction ---

  describe('request construction', () => {
    it('always sends Content-Type application/json header', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({}))

      await api.getHealth()
      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
        }),
      )
    })

    it('sends request body as JSON string for mutations', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({ id: 'b1', address: '10.0.0.1:8080', weight: 100, state: 'up', healthy: true, requests: 0, errors: 0 }))

      await api.addBackend('web', { id: 'b1', address: '10.0.0.1:8080', weight: 50 })
      const call = mockFetch.mock.calls[0]
      const body = JSON.parse(call[1].body)
      expect(body.id).toBe('b1')
      expect(body.address).toBe('10.0.0.1:8080')
      expect(body.weight).toBe(50)
    })

    it('constructs correct API base path', async () => {
      mockFetch.mockResolvedValueOnce(mockOkResponse({}))

      await api.getPools()
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/pools',
        expect.anything(),
      )
    })
  })
})
