/**
 * Incident Management Library Tests
 *
 * Tests all public functions in lib/incidents.ts.
 * Mocks the db module's query function and config's getDatabaseUrl.
 */

jest.mock('@/lib/db', () => ({
  query: jest.fn(),
}))

jest.mock('@/lib/config', () => ({
  getDatabaseUrl: jest.fn(),
}))

import {
  isDatabaseConfigured,
  createIncident,
  getIncidents,
  getIncident,
  updateIncident,
  deleteIncident,
  getIncidentMetrics,
} from '@/lib/incidents'
import { query } from '@/lib/db'
import { getDatabaseUrl } from '@/lib/config'

const mockQuery = query as jest.MockedFunction<typeof query>
const mockGetDatabaseUrl = getDatabaseUrl as jest.MockedFunction<typeof getDatabaseUrl>

beforeEach(() => {
  jest.clearAllMocks()
})

describe('isDatabaseConfigured', () => {
  it('returns true when DATABASE_URL is set', () => {
    mockGetDatabaseUrl.mockReturnValue('postgresql://localhost/status')
    expect(isDatabaseConfigured()).toBe(true)
  })

  it('returns false when DATABASE_URL is null', () => {
    mockGetDatabaseUrl.mockReturnValue(null)
    expect(isDatabaseConfigured()).toBe(false)
  })
})

describe('createIncident', () => {
  it('inserts correct data and returns incident', async () => {
    const now = new Date().toISOString()

    // INSERT INTO incidents query
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'uuid-1',
        title: 'API Down',
        status: 'investigating',
        severity: 'major',
        affected_services: ['API'],
        created_at: now,
        resolved_at: null,
      }],
      command: 'INSERT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const incident = await createIncident({
      title: 'API Down',
      severity: 'major',
      affectedServices: ['API'],
    })

    expect(incident.id).toBe('uuid-1')
    expect(incident.title).toBe('API Down')
    expect(incident.status).toBe('investigating')
    expect(incident.severity).toBe('major')
    expect(incident.affectedServices).toEqual(['API'])
    expect(incident.updates).toEqual([])

    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining('INSERT INTO incidents'),
      ['API Down', 'major', ['API']],
    )
  })

  it('includes initial message as update when provided', async () => {
    const now = new Date().toISOString()

    // INSERT into incidents
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'uuid-2',
        title: 'Web Degraded',
        status: 'investigating',
        severity: 'minor',
        affected_services: ['Web'],
        created_at: now,
        resolved_at: null,
      }],
      command: 'INSERT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // INSERT into incident_updates (addIncidentUpdate called internally)
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'upd-1',
        incident_id: 'uuid-2',
        message: 'Health check failing',
        status: 'investigating',
        created_at: now,
      }],
      command: 'INSERT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const incident = await createIncident({
      title: 'Web Degraded',
      severity: 'minor',
      affectedServices: ['Web'],
      initialMessage: 'Health check failing',
    })

    expect(incident.updates).toHaveLength(1)
    expect(incident.updates[0].message).toBe('Health check failing')
    expect(incident.updates[0].status).toBe('investigating')
  })

  it('throws on null result from query', async () => {
    mockQuery.mockResolvedValueOnce(null)

    await expect(
      createIncident({
        title: 'Test',
        severity: 'minor',
        affectedServices: [],
      }),
    ).rejects.toThrow('Failed to create incident')
  })

  it('throws on empty rows result', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'INSERT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    await expect(
      createIncident({
        title: 'Test',
        severity: 'minor',
        affectedServices: [],
      }),
    ).rejects.toThrow('Failed to create incident')
  })
})

describe('getIncidents', () => {
  it('returns paginated results', async () => {
    const now = new Date().toISOString()

    // COUNT query
    mockQuery.mockResolvedValueOnce({
      rows: [{ count: '3' }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // Data query
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: 'inc-1',
          title: 'Incident 1',
          status: 'investigating',
          severity: 'major',
          affected_services: ['API'],
          created_at: now,
          resolved_at: null,
        },
        {
          id: 'inc-2',
          title: 'Incident 2',
          status: 'resolved',
          severity: 'minor',
          affected_services: ['Web'],
          created_at: now,
          resolved_at: now,
        },
      ],
      command: 'SELECT',
      rowCount: 2,
      oid: 0,
      fields: [],
    })

    // Updates query (fetchUpdatesForIncidents)
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'SELECT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    const result = await getIncidents({ limit: 2, offset: 0 })

    expect(result.total).toBe(3)
    expect(result.incidents).toHaveLength(2)
    expect(result.incidents[0].id).toBe('inc-1')
    expect(result.incidents[1].id).toBe('inc-2')
  })

  it('applies status filter', async () => {
    mockQuery
      .mockResolvedValueOnce({ rows: [{ count: '0' }], command: 'SELECT', rowCount: 1, oid: 0, fields: [] })
      .mockResolvedValueOnce({ rows: [], command: 'SELECT', rowCount: 0, oid: 0, fields: [] })

    await getIncidents({ status: 'investigating' })

    // Count query should include status filter
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining('status = $1'),
      ['investigating'],
    )
  })

  it('applies severity filter', async () => {
    mockQuery
      .mockResolvedValueOnce({ rows: [{ count: '0' }], command: 'SELECT', rowCount: 1, oid: 0, fields: [] })
      .mockResolvedValueOnce({ rows: [], command: 'SELECT', rowCount: 0, oid: 0, fields: [] })

    await getIncidents({ severity: 'critical' })

    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining('severity = $1'),
      ['critical'],
    )
  })

  it('applies service filter', async () => {
    mockQuery
      .mockResolvedValueOnce({ rows: [{ count: '0' }], command: 'SELECT', rowCount: 1, oid: 0, fields: [] })
      .mockResolvedValueOnce({ rows: [], command: 'SELECT', rowCount: 0, oid: 0, fields: [] })

    await getIncidents({ affectedService: 'API' })

    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining('ANY(affected_services)'),
      ['API'],
    )
  })

  it('applies date range filter', async () => {
    const since = new Date('2025-01-01')
    const until = new Date('2025-12-31')

    mockQuery
      .mockResolvedValueOnce({ rows: [{ count: '0' }], command: 'SELECT', rowCount: 1, oid: 0, fields: [] })
      .mockResolvedValueOnce({ rows: [], command: 'SELECT', rowCount: 0, oid: 0, fields: [] })

    await getIncidents({ since, until })

    const countCall = mockQuery.mock.calls[0]
    expect(countCall[0]).toContain('created_at >=')
    expect(countCall[0]).toContain('created_at <=')
    expect(countCall[1]).toContain(since.toISOString())
    expect(countCall[1]).toContain(until.toISOString())
  })

  it('returns empty when no data results', async () => {
    mockQuery
      .mockResolvedValueOnce({ rows: [{ count: '0' }], command: 'SELECT', rowCount: 1, oid: 0, fields: [] })
      .mockResolvedValueOnce(null)

    const result = await getIncidents()

    expect(result.total).toBe(0)
    expect(result.incidents).toEqual([])
  })
})

describe('getIncident', () => {
  it('returns single incident with updates', async () => {
    const now = new Date().toISOString()

    // Incident query
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'inc-1',
        title: 'Test Incident',
        status: 'investigating',
        severity: 'major',
        affected_services: ['API', 'Web'],
        created_at: now,
        resolved_at: null,
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // Updates query
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'upd-1',
        incident_id: 'inc-1',
        message: 'Looking into it',
        status: 'investigating',
        created_at: now,
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const incident = await getIncident('inc-1')

    expect(incident).not.toBeNull()
    expect(incident!.id).toBe('inc-1')
    expect(incident!.title).toBe('Test Incident')
    expect(incident!.affectedServices).toEqual(['API', 'Web'])
    expect(incident!.updates).toHaveLength(1)
    expect(incident!.updates[0].message).toBe('Looking into it')
  })

  it('returns null for non-existent incident', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'SELECT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    const incident = await getIncident('nonexistent')
    expect(incident).toBeNull()
  })

  it('returns null when query returns null', async () => {
    mockQuery.mockResolvedValueOnce(null)

    const incident = await getIncident('no-db')
    expect(incident).toBeNull()
  })
})

describe('updateIncident', () => {
  it('updates status and adds message', async () => {
    const now = new Date().toISOString()

    // UPDATE incidents query
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'UPDATE',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // INSERT incident_updates (addIncidentUpdate)
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'upd-new',
        incident_id: 'inc-1',
        message: 'Root cause found',
        status: 'identified',
        created_at: now,
      }],
      command: 'INSERT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // getIncident called internally -- incident query
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'inc-1',
        title: 'Test',
        status: 'identified',
        severity: 'major',
        affected_services: ['API'],
        created_at: now,
        resolved_at: null,
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // getIncident -- updates query
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'upd-new',
        incident_id: 'inc-1',
        message: 'Root cause found',
        status: 'identified',
        created_at: now,
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const result = await updateIncident('inc-1', {
      status: 'identified',
      message: 'Root cause found',
    })

    expect(result).not.toBeNull()
    expect(result!.status).toBe('identified')

    // Verify UPDATE SQL was called
    const updateCall = mockQuery.mock.calls[0]
    expect(updateCall[0]).toContain('UPDATE incidents SET status')
    expect(updateCall[1]).toEqual(['identified', 'inc-1'])
  })

  it('sets resolved_at for resolved status', async () => {
    const now = new Date().toISOString()

    // UPDATE incidents
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'UPDATE',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // INSERT incident_updates
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'upd-resolved',
        incident_id: 'inc-1',
        message: 'Fixed',
        status: 'resolved',
        created_at: now,
      }],
      command: 'INSERT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // getIncident -- incident
    mockQuery.mockResolvedValueOnce({
      rows: [{
        id: 'inc-1',
        title: 'Test',
        status: 'resolved',
        severity: 'minor',
        affected_services: ['Web'],
        created_at: now,
        resolved_at: now,
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    // getIncident -- updates
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'SELECT',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    await updateIncident('inc-1', { status: 'resolved', message: 'Fixed' })

    // The UPDATE SQL should include resolved_at = NOW()
    const updateSql = mockQuery.mock.calls[0][0] as string
    expect(updateSql).toContain('resolved_at = NOW()')
  })
})

describe('deleteIncident', () => {
  it('returns true on successful deletion', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'DELETE',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const result = await deleteIncident('inc-1')
    expect(result).toBe(true)
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining('DELETE FROM incidents'),
      ['inc-1'],
    )
  })

  it('returns false when incident not found', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [],
      command: 'DELETE',
      rowCount: 0,
      oid: 0,
      fields: [],
    })

    const result = await deleteIncident('nonexistent')
    expect(result).toBe(false)
  })

  it('returns false when query returns null', async () => {
    mockQuery.mockResolvedValueOnce(null)

    const result = await deleteIncident('no-db')
    expect(result).toBe(false)
  })
})

describe('getIncidentMetrics', () => {
  it('calculates correct metrics', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [{
        total: '10',
        resolved: '8',
        avg_resolution_seconds: '3600',
        minor: '5',
        major: '3',
        critical: '2',
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const metrics = await getIncidentMetrics(90)

    expect(metrics.totalIncidents).toBe(10)
    expect(metrics.resolvedIncidents).toBe(8)
    expect(metrics.averageResolutionTime).toBe(3600)
    expect(metrics.incidentsBySeverity).toEqual({
      minor: 5,
      major: 3,
      critical: 2,
    })
  })

  it('returns zeros when no data', async () => {
    mockQuery.mockResolvedValueOnce(null)

    const metrics = await getIncidentMetrics()

    expect(metrics.totalIncidents).toBe(0)
    expect(metrics.resolvedIncidents).toBe(0)
    expect(metrics.averageResolutionTime).toBeNull()
    expect(metrics.incidentsBySeverity).toEqual({
      minor: 0,
      major: 0,
      critical: 0,
    })
  })

  it('returns null averageResolutionTime when no resolved incidents', async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [{
        total: '3',
        resolved: '0',
        avg_resolution_seconds: null,
        minor: '2',
        major: '1',
        critical: '0',
      }],
      command: 'SELECT',
      rowCount: 1,
      oid: 0,
      fields: [],
    })

    const metrics = await getIncidentMetrics()

    expect(metrics.totalIncidents).toBe(3)
    expect(metrics.resolvedIncidents).toBe(0)
    expect(metrics.averageResolutionTime).toBeNull()
  })
})
