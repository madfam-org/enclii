/**
 * Auto-Incident Detection Tests
 *
 * Tests the detectAndManageIncidents function which bridges
 * health checks with the incident management system.
 */

jest.mock('@/lib/db', () => ({
  query: jest.fn(),
}))

jest.mock('@/lib/incidents', () => ({
  createIncident: jest.fn(),
  updateIncident: jest.fn(),
}))

import { detectAndManageIncidents } from '@/lib/auto-incidents'
import { query } from '@/lib/db'
import { createIncident, updateIncident } from '@/lib/incidents'
import type { HealthCheckResult } from '@/lib/types'

const mockQuery = query as jest.MockedFunction<typeof query>
const mockCreateIncident = createIncident as jest.MockedFunction<typeof createIncident>
const mockUpdateIncident = updateIncident as jest.MockedFunction<typeof updateIncident>

// Helper to build a HealthCheckResult
function makeResult(
  service: string,
  status: HealthCheckResult['status'],
  overrides?: Partial<HealthCheckResult>,
): HealthCheckResult {
  return {
    service,
    url: `https://${service}.test.com/health`,
    group: 'Test',
    status,
    responseTime: 100,
    lastChecked: new Date().toISOString(),
    statusCode: status === 'operational' ? 200 : 500,
    ...overrides,
  }
}

beforeEach(() => {
  jest.clearAllMocks()
})

describe('detectAndManageIncidents', () => {
  it('creates incident when all recent checks are bad and no active incident', async () => {
    const results = [makeResult('API', 'outage', { error: 'HTTP 502', statusCode: 502 })]

    // First query: last N status_checks -- all outage
    mockQuery
      .mockResolvedValueOnce({
        rows: [{ status: 'outage' }, { status: 'outage' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      // Second query: active incidents for this service -- none
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })

    mockCreateIncident.mockResolvedValueOnce({
      id: 'inc-1',
      title: '[Auto] API Outage',
      status: 'investigating',
      severity: 'major',
      affectedServices: ['API'],
      createdAt: new Date().toISOString(),
      updates: [],
    })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.created).toEqual(['inc-1'])
    expect(result.resolved).toEqual([])
    expect(mockCreateIncident).toHaveBeenCalledWith({
      title: '[Auto] API Outage',
      severity: 'major',
      affectedServices: ['API'],
      initialMessage: 'HTTP 502',
    })
  })

  it('does not create incident when less than threshold checks exist', async () => {
    const results = [makeResult('API', 'outage')]

    // Only 1 row when threshold is 2
    mockQuery.mockResolvedValueOnce({
      rows: [{ status: 'outage' }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.created).toEqual([])
    expect(result.resolved).toEqual([])
    expect(mockCreateIncident).not.toHaveBeenCalled()
  })

  it('does not create incident when active incident already exists', async () => {
    const results = [makeResult('API', 'outage')]

    mockQuery
      // Recent checks -- all outage
      .mockResolvedValueOnce({
        rows: [{ status: 'outage' }, { status: 'outage' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      // Active incident exists
      .mockResolvedValueOnce({
        rows: [{ id: 'existing-inc', title: '[Auto] API Outage' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.created).toEqual([])
    expect(mockCreateIncident).not.toHaveBeenCalled()
  })

  it('resolves auto-incident when all checks are good', async () => {
    const results = [makeResult('API', 'operational')]

    mockQuery
      // Recent checks -- resolveThreshold=4 operational rows needed for resolve
      .mockResolvedValueOnce({
        rows: [
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
        ],
        command: 'SELECT',
        rowCount: 4,
        oid: 0,
        fields: [],
      })
      // Active auto-incident exists
      .mockResolvedValueOnce({
        rows: [{ id: 'auto-inc-1', title: '[Auto] API Outage' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    mockUpdateIncident.mockResolvedValueOnce(null)

    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual(['auto-inc-1'])
    expect(result.created).toEqual([])
    expect(mockUpdateIncident).toHaveBeenCalledWith('auto-inc-1', {
      status: 'resolved',
      message: 'Service recovered automatically',
    })
  })

  it('does not resolve manual (non-[Auto]) incidents', async () => {
    const results = [makeResult('API', 'operational')]

    mockQuery
      // Recent checks -- all operational
      .mockResolvedValueOnce({
        rows: [{ status: 'operational' }, { status: 'operational' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      // Active manual incident (no [Auto] prefix)
      .mockResolvedValueOnce({
        rows: [{ id: 'manual-inc', title: 'API Investigation' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual([])
    expect(mockUpdateIncident).not.toHaveBeenCalled()
  })

  it('returns created and resolved arrays', async () => {
    // Two services: one needs creation, one needs resolution
    const results = [
      makeResult('API', 'outage', { error: 'HTTP 500', statusCode: 500 }),
      makeResult('Web', 'operational'),
    ]

    mockQuery
      // API: recent checks all bad
      .mockResolvedValueOnce({
        rows: [{ status: 'outage' }, { status: 'outage' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      // API: no active incident
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })
      // Web: recent checks all good (resolveThreshold=4 rows)
      .mockResolvedValueOnce({
        rows: [
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
        ],
        command: 'SELECT',
        rowCount: 4,
        oid: 0,
        fields: [],
      })
      // Web: active auto-incident exists
      .mockResolvedValueOnce({
        rows: [{ id: 'web-inc', title: '[Auto] Web Degraded' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    mockCreateIncident.mockResolvedValueOnce({
      id: 'new-inc',
      title: '[Auto] API Outage',
      status: 'investigating',
      severity: 'major',
      affectedServices: ['API'],
      createdAt: new Date().toISOString(),
      updates: [],
    })
    mockUpdateIncident.mockResolvedValueOnce(null)

    const result = await detectAndManageIncidents(results, 2)

    expect(result.created).toEqual(['new-inc'])
    expect(result.resolved).toEqual(['web-inc'])
  })

  it('sets severity to major for outage, minor for degraded', async () => {
    const results = [makeResult('API', 'degraded', { error: 'HTTP 404', statusCode: 404 })]

    mockQuery
      // Recent checks -- all degraded
      .mockResolvedValueOnce({
        rows: [{ status: 'degraded' }, { status: 'degraded' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      // No active incident
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })

    mockCreateIncident.mockResolvedValueOnce({
      id: 'deg-inc',
      title: '[Auto] API Degraded',
      status: 'investigating',
      severity: 'minor',
      affectedServices: ['API'],
      createdAt: new Date().toISOString(),
      updates: [],
    })

    await detectAndManageIncidents(results, 2)

    expect(mockCreateIncident).toHaveBeenCalledWith(
      expect.objectContaining({
        severity: 'minor',
        title: '[Auto] API Degraded',
      }),
    )
  })

  it('handles createIncident errors gracefully', async () => {
    const results = [makeResult('API', 'outage', { error: 'HTTP 500', statusCode: 500 })]

    mockQuery
      .mockResolvedValueOnce({
        rows: [{ status: 'outage' }, { status: 'outage' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })

    mockCreateIncident.mockRejectedValueOnce(new Error('DB connection lost'))

    const consoleSpy = jest.spyOn(console, 'error').mockImplementation()
    const result = await detectAndManageIncidents(results, 2)

    expect(result.created).toEqual([])
    expect(consoleSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to create auto-incident for API'),
      expect.any(Error),
    )
    consoleSpy.mockRestore()
  })

  it('handles updateIncident errors gracefully', async () => {
    const results = [makeResult('API', 'operational')]

    mockQuery
      .mockResolvedValueOnce({
        rows: [
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
        ],
        command: 'SELECT',
        rowCount: 4,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [{ id: 'auto-inc', title: '[Auto] API Outage' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    mockUpdateIncident.mockRejectedValueOnce(new Error('DB write failed'))

    const consoleSpy = jest.spyOn(console, 'error').mockImplementation()
    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual([])
    expect(consoleSpy).toHaveBeenCalledWith(
      expect.stringContaining('Failed to resolve auto-incident auto-inc'),
      expect.any(Error),
    )
    consoleSpy.mockRestore()
  })

  it('does NOT resolve when only `threshold` good checks exist (hysteresis)', async () => {
    // Flapping service with only 2 good rows must not auto-resolve when
    // resolveThreshold = threshold*2 = 4 rows are required.
    const results = [makeResult('API', 'operational')]

    mockQuery
      .mockResolvedValueOnce({
        rows: [{ status: 'operational' }, { status: 'operational' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [{ id: 'auto-inc-1', title: '[Auto] API Outage' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual([])
    expect(mockUpdateIncident).not.toHaveBeenCalled()
  })

  it('skips when results are operational with no active incident', async () => {
    const results = [makeResult('API', 'operational')]

    mockQuery
      .mockResolvedValueOnce({
        rows: [{ status: 'operational' }, { status: 'operational' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      // No active incident
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.created).toEqual([])
    expect(result.resolved).toEqual([])
    expect(mockCreateIncident).not.toHaveBeenCalled()
    expect(mockUpdateIncident).not.toHaveBeenCalled()
  })

  it('resolves orphaned auto-incidents for services no longer in the catalog', async () => {
    const results = [makeResult('API', 'operational')]

    mockQuery
      // API: recent checks all good, no active incident
      .mockResolvedValueOnce({
        rows: [
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
        ],
        command: 'SELECT',
        rowCount: 4,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })
      // Active auto incident references a retired/renamed service.
      .mockResolvedValueOnce({
        rows: [
          {
            id: 'old-inc',
            title: '[Auto] Old Service Outage',
            affected_services: ['Old Service'],
          },
        ],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    mockUpdateIncident.mockResolvedValueOnce(null)

    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual(['old-inc'])
    expect(mockUpdateIncident).toHaveBeenCalledWith('old-inc', {
      status: 'resolved',
      message:
        'Auto-resolved stale incident: affected service is no longer in the active status catalog',
    })
  })

  it('does not resolve auto-incidents that still affect a current service', async () => {
    const results = [makeResult('PhyneCRM App', 'outage')]

    mockQuery
      // Current service is still bad enough to keep its auto incident active.
      .mockResolvedValueOnce({
        rows: [{ status: 'outage' }, { status: 'outage' }],
        command: 'SELECT',
        rowCount: 2,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [{ id: 'current-inc', title: '[Auto] PhyneCRM App Outage' }],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [
          {
            id: 'current-inc',
            title: '[Auto] PhyneCRM App Outage',
            affected_services: ['PhyneCRM App'],
          },
        ],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual([])
    expect(mockUpdateIncident).not.toHaveBeenCalled()
  })

  it('does not resolve orphaned manual incidents', async () => {
    const results = [makeResult('API', 'operational')]

    mockQuery
      .mockResolvedValueOnce({
        rows: [
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
          { status: 'operational' },
        ],
        command: 'SELECT',
        rowCount: 4,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [],
        command: 'SELECT',
        rowCount: 0,
        oid: 0,
        fields: [],
      })
      .mockResolvedValueOnce({
        rows: [
          {
            id: 'manual-inc',
            title: 'Old Service Investigation',
            affected_services: ['Old Service'],
          },
        ],
        command: 'SELECT',
        rowCount: 1,
        oid: 0,
        fields: [],
      })

    const result = await detectAndManageIncidents(results, 2)

    expect(result.resolved).toEqual([])
    expect(mockUpdateIncident).not.toHaveBeenCalled()
  })
})
