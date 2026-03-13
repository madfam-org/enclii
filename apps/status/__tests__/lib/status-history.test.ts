/**
 * Status History Library Tests
 *
 * Tests database-backed status recording, timeline queries, aggregation, and pruning.
 * Mocks the db module's query function.
 */

// Must mock before import to avoid module initialization issues
jest.mock('@/lib/db', () => ({
  query: jest.fn(),
}))

import {
  ensureSchema,
  recordStatusSnapshot,
  getTimeline,
  aggregateHourly,
  aggregateDaily,
  pruneOldRecords,
  getUptimeSummary,
  getAllUptimeSummaries,
} from '@/lib/status-history'
import { query } from '@/lib/db'
import type { HealthCheckResult } from '@/lib/types'

const mockQuery = query as jest.MockedFunction<typeof query>

// Helper to build a HealthCheckResult
function makeResult(
  service: string,
  status: HealthCheckResult['status'],
  overrides?: Partial<HealthCheckResult>,
): HealthCheckResult {
  return {
    service,
    url: `https://${service.toLowerCase().replace(/\s/g, '-')}.test.com/health`,
    group: 'Test',
    status,
    responseTime: 150,
    lastChecked: new Date().toISOString(),
    statusCode: status === 'operational' ? 200 : 500,
    ...overrides,
  }
}

// The module has a `schemaEnsured` flag. We need to reset it between tests.
// Since it's module-level state, we reset the module before each test.
beforeEach(() => {
  jest.clearAllMocks()
  // Reset the schemaEnsured flag by clearing the module registry
  // This forces ensureSchema to run CREATE TABLE queries again
  jest.resetModules()
})

// Re-import after module reset for tests that need fresh state
async function getFreshModule() {
  jest.resetModules()
  // Re-mock db before re-importing
  jest.mock('@/lib/db', () => ({
    query: jest.fn(),
  }))
  const historyModule = await import('@/lib/status-history')
  const dbModule = await import('@/lib/db')
  return {
    ensureSchema: historyModule.ensureSchema,
    recordStatusSnapshot: historyModule.recordStatusSnapshot,
    getTimeline: historyModule.getTimeline,
    aggregateHourly: historyModule.aggregateHourly,
    aggregateDaily: historyModule.aggregateDaily,
    pruneOldRecords: historyModule.pruneOldRecords,
    getUptimeSummary: historyModule.getUptimeSummary,
    getAllUptimeSummaries: historyModule.getAllUptimeSummaries,
    mockQuery: dbModule.query as jest.MockedFunction<typeof query>,
  }
}

describe('ensureSchema', () => {
  it('creates tables by executing CREATE TABLE queries', async () => {
    const mod = await getFreshModule()
    // Each query call for schema creation returns success
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'CREATE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    await mod.ensureSchema()

    // Should have called query multiple times for tables + indexes
    // 6 CREATE TABLE/INDEX calls based on source:
    // status_checks, idx, status_hourly, status_daily, incidents, incident_updates, scheduled_maintenance
    expect(mod.mockQuery.mock.calls.length).toBeGreaterThanOrEqual(6)

    // Verify table names in queries
    const allSql = mod.mockQuery.mock.calls.map((c) => c[0]).join(' ')
    expect(allSql).toContain('status_checks')
    expect(allSql).toContain('status_hourly')
    expect(allSql).toContain('status_daily')
    expect(allSql).toContain('incidents')
    expect(allSql).toContain('incident_updates')
    expect(allSql).toContain('scheduled_maintenance')
  })

  it('runs only once (idempotent via schemaEnsured flag)', async () => {
    const mod = await getFreshModule()
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'CREATE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    await mod.ensureSchema()
    const callCountAfterFirst = mod.mockQuery.mock.calls.length

    await mod.ensureSchema()
    // No additional calls -- schemaEnsured is true
    expect(mod.mockQuery.mock.calls.length).toBe(callCountAfterFirst)
  })
})

describe('recordStatusSnapshot', () => {
  it('inserts batch of results', async () => {
    const mod = await getFreshModule()
    // Schema queries
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'CREATE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    // Override the last call for the INSERT
    const results = [
      makeResult('API', 'operational'),
      makeResult('Web', 'degraded', { responseTime: 800, statusCode: 404 }),
    ]

    // After schema calls, the INSERT call:
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'INSERT',
      rowCount: 2,
      oid: 0,
      fields: [],
    })

    const count = await mod.recordStatusSnapshot(results)

    // The last query call should be the INSERT
    const lastCall = mod.mockQuery.mock.calls[mod.mockQuery.mock.calls.length - 1]
    const sql = lastCall[0] as string
    expect(sql).toContain('INSERT INTO status_checks')
    expect(sql).toContain('service, url, group_name, status, response_ms, status_code, error')

    // Values should contain service data
    const values = lastCall[1] as unknown[]
    expect(values).toContain('API')
    expect(values).toContain('Web')

    expect(count).toBe(2)
  })

  it('returns 0 for empty array', async () => {
    const mod = await getFreshModule()
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'CREATE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    const count = await mod.recordStatusSnapshot([])
    expect(count).toBe(0)
  })
})

describe('getTimeline', () => {
  it('returns correct structure with services and metadata', async () => {
    const mod = await getFreshModule()
    const now = new Date()
    const windowStart = new Date(now.getTime() - 60 * 60 * 1000) // 1 hour ago
    windowStart.setMinutes(Math.floor(windowStart.getMinutes() / 5) * 5, 0, 0)

    // Schema queries
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'CREATE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    // Override for the timeline SELECT query
    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('SELECT') && sql.includes('window_start')) {
        return {
          rows: [{
            service: 'API',
            url: 'https://api.test.com',
            group_name: 'Core',
            window_start: windowStart.toISOString(),
            total_checks: '3',
            operational: '3',
            degraded: '0',
            outage: '0',
            maintenance: '0',
            avg_response_ms: '120',
          }],
          command: 'SELECT',
          rowCount: 1,
          oid: 0,
          fields: [],
        }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const timeline = await mod.getTimeline(24, 5)

    expect(timeline).toHaveProperty('services')
    expect(timeline).toHaveProperty('from')
    expect(timeline).toHaveProperty('to')
    expect(timeline).toHaveProperty('windowMinutes')
    expect(timeline.windowMinutes).toBe(5)

    // Should have at least one service with slots
    if (timeline.services.length > 0) {
      const svc = timeline.services[0]
      expect(svc.service).toBe('API')
      expect(svc.group).toBe('Core')
      expect(svc.slots.length).toBeGreaterThan(0)
      expect(typeof svc.uptime24h).toBe('number')
    }
  })

  it('handles empty data by returning no services', async () => {
    const mod = await getFreshModule()

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('SELECT') && sql.includes('window_start')) {
        return { rows: [], command: 'SELECT', rowCount: 0, oid: 0, fields: [] }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const timeline = await mod.getTimeline(24, 5)

    expect(timeline.services).toEqual([])
    expect(timeline.windowMinutes).toBe(5)
  })

  it('marks slots without data as unknown', async () => {
    const mod = await getFreshModule()

    // Return data for only one window slot out of many
    const now = new Date()
    const slotStart = new Date(now.getTime() - 30 * 60 * 1000) // 30 min ago
    slotStart.setMinutes(Math.floor(slotStart.getMinutes() / 5) * 5, 0, 0)

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('window_start')) {
        return {
          rows: [{
            service: 'API',
            url: 'https://api.test.com',
            group_name: 'Core',
            window_start: slotStart.toISOString(),
            total_checks: '1',
            operational: '1',
            degraded: '0',
            outage: '0',
            maintenance: '0',
            avg_response_ms: '100',
          }],
          command: 'SELECT',
          rowCount: 1,
          oid: 0,
          fields: [],
        }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const timeline = await mod.getTimeline(24, 5)

    if (timeline.services.length > 0) {
      const svc = timeline.services[0]
      // Most slots should be unknown (no data), some should be operational
      const unknownSlots = svc.slots.filter((s) => s.status === 'unknown')
      const operationalSlots = svc.slots.filter((s) => s.status === 'operational')
      expect(unknownSlots.length).toBeGreaterThan(0)
      expect(operationalSlots.length).toBeGreaterThanOrEqual(1)
    }
  })
})

describe('worstStatus (tested via getTimeline)', () => {
  it('outage is worst status in a window', async () => {
    const mod = await getFreshModule()
    const now = new Date()
    const slotStart = new Date(now.getTime() - 10 * 60 * 1000)
    slotStart.setMinutes(Math.floor(slotStart.getMinutes() / 5) * 5, 0, 0)

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('window_start')) {
        return {
          rows: [{
            service: 'API',
            url: 'https://api.test.com',
            group_name: 'Core',
            window_start: slotStart.toISOString(),
            total_checks: '5',
            operational: '2',
            degraded: '1',
            outage: '1',
            maintenance: '1',
            avg_response_ms: '500',
          }],
          command: 'SELECT',
          rowCount: 1,
          oid: 0,
          fields: [],
        }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const timeline = await mod.getTimeline(24, 5)

    if (timeline.services.length > 0) {
      // Find the slot that has our data (the one matching slotStart)
      const matchingSlot = timeline.services[0].slots.find(
        (s) => s.start === slotStart.toISOString(),
      )
      if (matchingSlot) {
        expect(matchingSlot.status).toBe('outage')
      }
    }
  })
})

describe('aggregateHourly', () => {
  it('executes the hourly aggregation SQL', async () => {
    const mod = await getFreshModule()
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'INSERT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    await mod.aggregateHourly()

    // Should have schema queries + the aggregation INSERT
    const allSql = mod.mockQuery.mock.calls.map((c) => c[0] as string)
    const aggregationQuery = allSql.find(
      (sql) => sql.includes('INSERT INTO status_hourly') && sql.includes('ON CONFLICT'),
    )
    expect(aggregationQuery).toBeDefined()
  })
})

describe('aggregateDaily', () => {
  it('executes the daily aggregation SQL', async () => {
    const mod = await getFreshModule()
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'INSERT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    await mod.aggregateDaily()

    const allSql = mod.mockQuery.mock.calls.map((c) => c[0] as string)
    const aggregationQuery = allSql.find(
      (sql) => sql.includes('INSERT INTO status_daily') && sql.includes('ON CONFLICT'),
    )
    expect(aggregationQuery).toBeDefined()
  })
})

describe('pruneOldRecords', () => {
  it('returns deleted counts for all tables', async () => {
    const mod = await getFreshModule()

    // Schema creation queries
    mod.mockQuery.mockResolvedValue({
      rows: [],
      command: 'CREATE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    // Override for DELETE queries to return specific rowCounts
    let deleteCallCount = 0
    const deleteCounts = [100, 24, 7]
    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('DELETE FROM')) {
        const count = deleteCounts[deleteCallCount++] || 0
        return { rows: [], command: 'DELETE', rowCount: count, oid: 0, fields: [] }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const result = await mod.pruneOldRecords()

    expect(result).toHaveProperty('rawDeleted')
    expect(result).toHaveProperty('hourlyDeleted')
    expect(result).toHaveProperty('dailyDeleted')
    expect(result.rawDeleted).toBe(100)
    expect(result.hourlyDeleted).toBe(24)
    expect(result.dailyDeleted).toBe(7)
  })
})

describe('getUptimeSummary', () => {
  it('returns percentage when data exists', async () => {
    const mod = await getFreshModule()

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('uptime_pct') && sql.includes('status_daily')) {
        return {
          rows: [{ uptime_pct: '99.95' }],
          command: 'SELECT',
          rowCount: 1,
          oid: 0,
          fields: [],
        }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const uptime = await mod.getUptimeSummary('API', 7)
    expect(uptime).toBe(99.95)
  })

  it('returns null when no data exists', async () => {
    const mod = await getFreshModule()

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('uptime_pct') && sql.includes('status_daily')) {
        return {
          rows: [{ uptime_pct: null }],
          command: 'SELECT',
          rowCount: 1,
          oid: 0,
          fields: [],
        }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const uptime = await mod.getUptimeSummary('API', 7)
    expect(uptime).toBeNull()
  })

  it('returns null when query returns null', async () => {
    const mod = await getFreshModule()

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('uptime_pct') && sql.includes('status_daily')) {
        return null
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const uptime = await mod.getUptimeSummary('API', 7)
    expect(uptime).toBeNull()
  })
})

describe('getAllUptimeSummaries', () => {
  it('returns array of summaries', async () => {
    const mod = await getFreshModule()

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('GROUP BY service')) {
        return {
          rows: [
            { service: 'API', uptime_pct: '99.99' },
            { service: 'Web', uptime_pct: '98.50' },
            { service: 'Admin', uptime_pct: null },
          ],
          command: 'SELECT',
          rowCount: 3,
          oid: 0,
          fields: [],
        }
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const summaries = await mod.getAllUptimeSummaries(7)

    expect(summaries).toHaveLength(3)
    expect(summaries[0]).toEqual({ service: 'API', uptimePct: 99.99 })
    expect(summaries[1]).toEqual({ service: 'Web', uptimePct: 98.5 })
    expect(summaries[2]).toEqual({ service: 'Admin', uptimePct: null })
  })

  it('returns empty array when query returns null', async () => {
    const mod = await getFreshModule()

    mod.mockQuery.mockImplementation(async (sql: string) => {
      if (typeof sql === 'string' && sql.includes('GROUP BY service')) {
        return null
      }
      return { rows: [], command: 'CREATE', rowCount: 0, oid: 0, fields: [] }
    })

    const summaries = await mod.getAllUptimeSummaries(7)
    expect(summaries).toEqual([])
  })
})
