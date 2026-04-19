/**
 * Stale Outage Digest Tests
 *
 * Covers the digest detection SQL shape, the formatter, and the
 * webhook poster's failure-is-not-fatal contract.
 */

jest.mock('@/lib/db', () => ({
  query: jest.fn(),
}))

import {
  detectStaleOutages,
  formatDigest,
  postDigestToWebhook,
} from '@/lib/stale-digest'
import { query } from '@/lib/db'

const mockQuery = query as jest.MockedFunction<typeof query>

beforeEach(() => {
  jest.clearAllMocks()
})

describe('detectStaleOutages', () => {
  it('returns empty list when db returns no rows', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'SELECT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })
    const result = await detectStaleOutages({ thresholdHours: 24 })
    expect(result).toEqual([])
  })

  it('returns empty list when db layer returns null (unconfigured)', async () => {
    mockQuery.mockResolvedValueOnce(null as unknown as Awaited<
      ReturnType<typeof query>
    >)
    const result = await detectStaleOutages()
    expect(result).toEqual([])
  })

  it('maps db rows to StaleOutage entries with hoursInOutage', async () => {
    const now = Date.now()
    const fortyHoursAgo = new Date(now - 40 * 60 * 60 * 1000).toISOString()
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          service: 'Cotiza API',
          url: 'https://api.cotiza.studio/health',
          group_name: 'DigiFab',
          last_operational: fortyHoursAgo,
          last_status: 'outage',
          last_checked_at: new Date(now).toISOString(),
          bad_count: '1440',
          total_count: '1440',
        },
        {
          service: 'Avala API',
          url: 'https://api.avala.studio/health',
          group_name: 'Avala',
          last_operational: null,
          last_status: 'outage',
          last_checked_at: new Date(now).toISOString(),
          bad_count: '1440',
          total_count: '1440',
        },
      ],
      command: 'SELECT',
      rowCount: 2,
      oid: 0,
      fields: [],
    })

    const result = await detectStaleOutages({ thresholdHours: 24 })
    expect(result).toHaveLength(2)
    expect(result[0].service).toBe('Cotiza API')
    expect(result[0].lastStatus).toBe('outage')
    // ~40h since lastOperational; rounding tolerance.
    expect(result[0].hoursInOutage).toBeGreaterThanOrEqual(39.5)
    expect(result[0].hoursInOutage).toBeLessThanOrEqual(40.5)
    // Never observed operational — capped at retention window.
    expect(result[1].lastOperational).toBeNull()
    expect(result[1].hoursInOutage).toBe(25)
  })

  it('passes threshold and retention to SQL as string intervals', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'SELECT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })
    await detectStaleOutages({ thresholdHours: 48, retentionHours: 72 })
    const call = mockQuery.mock.calls[0]
    expect(call[1]).toEqual([72, 48])
  })
})

describe('formatDigest', () => {
  it('returns a no-stale message when the list is empty', () => {
    expect(formatDigest([], 24)).toMatch(/No services have been in outage/)
  })

  it('renders a multi-line summary with service name, group, and URL', () => {
    const out = formatDigest(
      [
        {
          service: 'Cotiza API',
          url: 'https://api.cotiza.studio/health',
          group: 'DigiFab',
          lastStatus: 'outage',
          lastOperational: new Date().toISOString(),
          hoursInOutage: 42.5,
          badChecks: 1440,
          totalChecks: 1440,
        },
      ],
      24,
    )
    expect(out).toContain('1 service has been in outage for >24h')
    expect(out).toContain('Cotiza API')
    expect(out).toContain('[DigiFab]')
    expect(out).toContain('outage for 42.5h')
    expect(out).toContain('https://api.cotiza.studio/health')
  })

  it('pluralises and handles never-operational services', () => {
    const out = formatDigest(
      [
        {
          service: 'A',
          url: 'u1',
          group: 'g',
          lastStatus: 'outage',
          lastOperational: null,
          hoursInOutage: 99,
          badChecks: 10,
          totalChecks: 10,
        },
        {
          service: 'B',
          url: 'u2',
          group: 'g',
          lastStatus: 'degraded',
          lastOperational: new Date().toISOString(),
          hoursInOutage: 30,
          badChecks: 10,
          totalChecks: 10,
        },
      ],
      24,
    )
    expect(out).toContain('2 services have been in outage for >24h')
    expect(out).toContain('never observed operational in window')
  })
})

describe('postDigestToWebhook', () => {
  const originalFetch = global.fetch

  afterEach(() => {
    global.fetch = originalFetch
  })

  it('returns false without calling fetch when url is empty', async () => {
    const spy = jest.fn()
    global.fetch = spy as unknown as typeof fetch
    const result = await postDigestToWebhook('', 'x')
    expect(result).toBe(false)
    expect(spy).not.toHaveBeenCalled()
  })

  it('returns true on 2xx response', async () => {
    global.fetch = jest.fn().mockResolvedValueOnce({
      ok: true,
    }) as unknown as typeof fetch
    const result = await postDigestToWebhook('https://hooks/slack', 'x')
    expect(result).toBe(true)
  })

  it('swallows fetch errors rather than throwing', async () => {
    global.fetch = jest
      .fn()
      .mockRejectedValueOnce(new Error('network')) as unknown as typeof fetch
    const errSpy = jest.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const result = await postDigestToWebhook('https://hooks/slack', 'x')
      expect(result).toBe(false)
    } finally {
      errSpy.mockRestore()
    }
  })
})
