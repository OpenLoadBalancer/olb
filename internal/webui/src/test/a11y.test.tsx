import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { axe } from 'vitest-axe'
import { render } from '@/test/utils'
import { DashboardPage } from '@/pages/dashboard'
import { PoolsPage } from '@/pages/pools'
import { ClusterPage } from '@/pages/cluster'
import { SettingsPage } from '@/pages/settings'
import { MetricsPage } from '@/pages/metrics'
import { WAFPage } from '@/pages/waf'
import { MiddlewarePage } from '@/pages/middleware'
import { CertificatesPage } from '@/pages/certificates'
import { LogsPage } from '@/pages/logs'

// Mock API responses for pages that fetch data
function mockAllEndpoints() {
  const ok = (data: any) => ({
    ok: true,
    json: () => Promise.resolve({ success: true, data }),
    text: () => Promise.resolve(''),
  })

  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url.includes('/system/health')) return Promise.resolve(ok({ status: 'healthy', checks: {}, timestamp: new Date().toISOString() }))
    if (url.includes('/system/info')) return Promise.resolve(ok({ version: '0.1.0', commit: 'abc', build_date: '2026-04-11', uptime: '1h', state: 'running', go_version: '1.26' }))
    if (url.includes('/version')) return Promise.resolve(ok({ version: '0.1.0', commit: 'abc', build_date: '2026-04-11', uptime: '1h', state: 'running', go_version: '1.26' }))
    if (url.includes('/pools')) return Promise.resolve(ok([]))
    if (url.includes('/routes')) return Promise.resolve(ok([]))
    if (url.includes('/health')) return Promise.resolve(ok([]))
    if (url.includes('/metrics')) return Promise.resolve(ok({}))
    if (url.includes('/config')) return Promise.resolve(ok({}))
    if (url.includes('/certificates')) return Promise.resolve(ok([]))
    if (url.includes('/waf/status')) return Promise.resolve(ok({ enabled: true, mode: 'detect' }))
    if (url.includes('/cluster/status')) return Promise.resolve(ok({ node_id: 'n1', state: 'leader', leader: 'n1', peers: [], applied_index: 0, commit_index: 0, term: 1, vote: 'n1' }))
    if (url.includes('/cluster/members')) return Promise.resolve(ok([]))
    if (url.includes('/middleware/status')) return Promise.resolve(ok([]))
    if (url.includes('/events')) return Promise.resolve(ok([]))
    return Promise.resolve(ok({}))
  }))
}

// Helper to assert no a11y violations
function assertNoViolations(results: Awaited<ReturnType<typeof axe>>) {
  if (results.violations.length === 0) return

  const details = results.violations.map(v => {
    const nodes = v.nodes.map(n => `  - ${n.target.join(', ')}`).join('\n')
    return `${v.id} (${v.impact}): ${v.description}\n${nodes}`
  }).join('\n\n')

  expect.fail(`Found ${results.violations.length} accessibility violation(s):\n\n${details}`)
}

describe('Accessibility Audit', () => {
  beforeEach(() => {
    mockAllEndpoints()
    // jsdom doesn't implement scrollIntoView
    Element.prototype.scrollIntoView = vi.fn()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  async function testComponent(Component: React.ComponentType) {
    const { container } = render(<Component />)

    // Wait for async rendering to settle
    await new Promise(resolve => setTimeout(resolve, 100))

    const results = await axe(container)
    assertNoViolations(results)
  }

  it('Dashboard page has no a11y violations', async () => {
    await testComponent(DashboardPage)
  })

  it('Pools page has no a11y violations', async () => {
    await testComponent(PoolsPage)
  })

  it('Cluster page has no a11y violations', async () => {
    await testComponent(ClusterPage)
  })

  it('Settings page has no a11y violations', async () => {
    await testComponent(SettingsPage)
  })

  it('Metrics page has no a11y violations', async () => {
    await testComponent(MetricsPage)
  })

  it('WAF page has no a11y violations', async () => {
    await testComponent(WAFPage)
  })

  it('Middleware page has no a11y violations', async () => {
    await testComponent(MiddlewarePage)
  })

  it('Certificates page has no a11y violations', async () => {
    await testComponent(CertificatesPage)
  })

  it('Logs page has no a11y violations', async () => {
    await testComponent(LogsPage)
  })
})
