import { describe, it, expect } from 'vitest'
import {
  addBackendSchema,
  addRouteSchema,
  addCertificateAcmeSchema,
  createPoolSchema,
} from '@/lib/form-schemas'

describe('form schemas', () => {
  it('coerces numeric strings from <input type="number">', () => {
    const backend = addBackendSchema.parse({ address: '10.0.1.10:8080', weight: '5' })
    expect(backend.weight).toBe(5)

    const route = addRouteSchema.parse({
      path: '/api',
      pool: 'web',
      methods: ['GET'],
      priority: '0',
      strip_prefix: false,
    })
    expect(route.priority).toBe(0)
  })

  it('rejects out-of-range and non-integer weights', () => {
    expect(addBackendSchema.safeParse({ address: 'a:1', weight: '0' }).success).toBe(false)
    expect(addBackendSchema.safeParse({ address: 'a:1', weight: '101' }).success).toBe(false)
    expect(addBackendSchema.safeParse({ address: 'a:1', weight: '1.5' }).success).toBe(false)
  })

  it('keeps custom error messages', () => {
    const res = createPoolSchema.safeParse({
      name: 'Bad Name',
      algorithm: 'round_robin',
      healthCheckEnabled: true,
      healthCheckType: 'http',
      healthCheckPath: '/health',
      healthCheckInterval: '10s',
    })
    expect(res.success).toBe(false)
    expect(res.error?.issues[0].message).toBe(
      'Pool name must contain only lowercase letters, numbers, and hyphens',
    )
  })

  it('validates ACME email addresses', () => {
    const base = { domain: '*.openloadbalancer.dev', autoRenew: true }
    expect(addCertificateAcmeSchema.safeParse({ ...base, email: 'ops@openloadbalancer.dev' }).success).toBe(true)
    const bad = addCertificateAcmeSchema.safeParse({ ...base, email: 'not-an-email' })
    expect(bad.success).toBe(false)
    expect(bad.error?.issues[0].message).toBe('Enter a valid email address')
  })
})
