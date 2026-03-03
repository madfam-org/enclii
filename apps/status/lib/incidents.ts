/**
 * Incident Management Library
 *
 * Database-backed incident CRUD operations for the status page.
 * Falls back to no-op behavior when DATABASE_URL is not configured.
 */

import { query } from './db'
import { getDatabaseUrl } from './config'
import type {
  Incident,
  IncidentUpdate,
  ScheduledMaintenance,
  IncidentStatus,
  IncidentSeverity,
} from './types'

/**
 * Check if database is configured
 */
export function isDatabaseConfigured(): boolean {
  return !!getDatabaseUrl()
}

/**
 * Incident query options
 */
export interface IncidentQueryOptions {
  status?: IncidentStatus
  severity?: IncidentSeverity
  affectedService?: string
  limit?: number
  offset?: number
  since?: Date
  until?: Date
}

// -- Row types for pg results --

interface IncidentRow {
  id: string
  title: string
  status: IncidentStatus
  severity: IncidentSeverity
  affected_services: string[]
  created_at: string
  resolved_at: string | null
}

interface UpdateRow {
  id: string
  incident_id: string
  message: string
  status: IncidentStatus | null
  created_at: string
}

interface MaintenanceRow {
  id: string
  title: string
  description: string | null
  affected_services: string[]
  scheduled_start: string
  scheduled_end: string
  created_at: string
}

// -- Helpers --

function rowToIncident(row: IncidentRow, updates: IncidentUpdate[] = []): Incident {
  return {
    id: row.id,
    title: row.title,
    status: row.status,
    severity: row.severity,
    affectedServices: row.affected_services,
    createdAt: row.created_at,
    resolvedAt: row.resolved_at ?? undefined,
    updates,
  }
}

function rowToUpdate(row: UpdateRow): IncidentUpdate {
  return {
    id: row.id,
    incidentId: row.incident_id,
    message: row.message,
    status: row.status ?? undefined,
    createdAt: row.created_at,
  }
}

function rowToMaintenance(row: MaintenanceRow): ScheduledMaintenance {
  return {
    id: row.id,
    title: row.title,
    description: row.description ?? undefined,
    affectedServices: row.affected_services,
    scheduledStart: row.scheduled_start,
    scheduledEnd: row.scheduled_end,
    createdAt: row.created_at,
  }
}

/**
 * Fetch updates for a set of incident IDs, grouped by incident.
 */
async function fetchUpdatesForIncidents(ids: string[]): Promise<Map<string, IncidentUpdate[]>> {
  const map = new Map<string, IncidentUpdate[]>()
  if (ids.length === 0) return map

  const res = await query<UpdateRow>(
    `SELECT id, incident_id, message, status, created_at
     FROM incident_updates
     WHERE incident_id = ANY($1)
     ORDER BY created_at ASC`,
    [ids],
  )
  if (!res) return map

  for (const row of res.rows) {
    const list = map.get(row.incident_id) ?? []
    list.push(rowToUpdate(row))
    map.set(row.incident_id, list)
  }
  return map
}

// -- Public API --

/**
 * Create a new incident
 */
export async function createIncident(data: {
  title: string
  severity: IncidentSeverity
  affectedServices: string[]
  initialMessage?: string
}): Promise<Incident> {
  const res = await query<IncidentRow>(
    `INSERT INTO incidents (title, severity, affected_services)
     VALUES ($1, $2, $3)
     RETURNING *`,
    [data.title, data.severity, data.affectedServices],
  )

  if (!res || res.rows.length === 0) {
    throw new Error('Failed to create incident (database may not be configured)')
  }

  const incident = rowToIncident(res.rows[0])

  if (data.initialMessage) {
    const upd = await addIncidentUpdate(incident.id, {
      message: data.initialMessage,
      status: 'investigating',
    })
    if (upd) incident.updates.push(upd)
  }

  return incident
}

/**
 * Get incidents with filtering and pagination
 */
export async function getIncidents(options: IncidentQueryOptions = {}): Promise<{
  incidents: Incident[]
  total: number
}> {
  const conditions: string[] = []
  const params: unknown[] = []
  let idx = 1

  if (options.status) {
    conditions.push(`status = $${idx++}`)
    params.push(options.status)
  }
  if (options.severity) {
    conditions.push(`severity = $${idx++}`)
    params.push(options.severity)
  }
  if (options.affectedService) {
    conditions.push(`$${idx++} = ANY(affected_services)`)
    params.push(options.affectedService)
  }
  if (options.since) {
    conditions.push(`created_at >= $${idx++}`)
    params.push(options.since.toISOString())
  }
  if (options.until) {
    conditions.push(`created_at <= $${idx++}`)
    params.push(options.until.toISOString())
  }

  const where = conditions.length > 0 ? `WHERE ${conditions.join(' AND ')}` : ''
  const limit = options.limit ?? 50
  const offset = options.offset ?? 0

  // Count total
  const countRes = await query<{ count: string }>(`SELECT count(*) FROM incidents ${where}`, params)
  const total = countRes ? parseInt(countRes.rows[0].count, 10) : 0

  // Fetch page
  const dataRes = await query<IncidentRow>(
    `SELECT * FROM incidents ${where} ORDER BY created_at DESC LIMIT $${idx++} OFFSET $${idx++}`,
    [...params, limit, offset],
  )

  if (!dataRes || dataRes.rows.length === 0) {
    return { incidents: [], total }
  }

  const ids = dataRes.rows.map((r) => r.id)
  const updatesMap = await fetchUpdatesForIncidents(ids)

  const incidents = dataRes.rows.map((row) =>
    rowToIncident(row, updatesMap.get(row.id) ?? []),
  )

  return { incidents, total }
}

/**
 * Get a single incident by ID
 */
export async function getIncident(id: string): Promise<Incident | null> {
  const res = await query<IncidentRow>(`SELECT * FROM incidents WHERE id = $1`, [id])
  if (!res || res.rows.length === 0) return null

  const updatesMap = await fetchUpdatesForIncidents([id])
  return rowToIncident(res.rows[0], updatesMap.get(id) ?? [])
}

/**
 * Update an incident
 */
export async function updateIncident(
  id: string,
  data: {
    status?: IncidentStatus
    message?: string
  },
): Promise<Incident | null> {
  if (data.status) {
    const resolvedClause = data.status === 'resolved' ? ', resolved_at = NOW()' : ''
    await query(
      `UPDATE incidents SET status = $1${resolvedClause} WHERE id = $2`,
      [data.status, id],
    )
  }

  if (data.message) {
    await addIncidentUpdate(id, {
      message: data.message,
      status: data.status,
    })
  }

  return getIncident(id)
}

/**
 * Delete an incident
 */
export async function deleteIncident(id: string): Promise<boolean> {
  const res = await query(`DELETE FROM incidents WHERE id = $1`, [id])
  return res ? res.rowCount !== null && res.rowCount > 0 : false
}

/**
 * Get active (unresolved) incidents
 */
export async function getActiveIncidents(): Promise<Incident[]> {
  const { incidents } = await getIncidents({
    limit: 100,
  })
  return incidents.filter((i) => i.status !== 'resolved')
}

/**
 * Get recent incidents (last 30 days)
 */
export async function getRecentIncidents(): Promise<Incident[]> {
  const thirtyDaysAgo = new Date()
  thirtyDaysAgo.setDate(thirtyDaysAgo.getDate() - 30)

  const { incidents } = await getIncidents({
    since: thirtyDaysAgo,
    limit: 100,
  })

  return incidents
}

/**
 * Create scheduled maintenance
 */
export async function createScheduledMaintenance(data: {
  title: string
  description?: string
  affectedServices: string[]
  scheduledStart: Date
  scheduledEnd: Date
}): Promise<ScheduledMaintenance> {
  const res = await query<MaintenanceRow>(
    `INSERT INTO scheduled_maintenance (title, description, affected_services, scheduled_start, scheduled_end)
     VALUES ($1, $2, $3, $4, $5)
     RETURNING *`,
    [data.title, data.description ?? null, data.affectedServices, data.scheduledStart.toISOString(), data.scheduledEnd.toISOString()],
  )

  if (!res || res.rows.length === 0) {
    throw new Error('Failed to create maintenance (database may not be configured)')
  }

  return rowToMaintenance(res.rows[0])
}

/**
 * Get upcoming and ongoing maintenance
 */
export async function getActiveMaintenances(): Promise<ScheduledMaintenance[]> {
  const res = await query<MaintenanceRow>(
    `SELECT * FROM scheduled_maintenance
     WHERE scheduled_end >= NOW()
     ORDER BY scheduled_start ASC`,
  )
  if (!res) return []
  return res.rows.map(rowToMaintenance)
}

/**
 * Get scheduled maintenances for the next N days
 */
export async function getUpcomingMaintenances(days: number = 7): Promise<ScheduledMaintenance[]> {
  const res = await query<MaintenanceRow>(
    `SELECT * FROM scheduled_maintenance
     WHERE scheduled_start <= NOW() + $1 * INTERVAL '1 day'
       AND scheduled_end >= NOW()
     ORDER BY scheduled_start ASC`,
    [days],
  )
  if (!res) return []
  return res.rows.map(rowToMaintenance)
}

/**
 * Delete scheduled maintenance
 */
export async function deleteScheduledMaintenance(id: string): Promise<boolean> {
  const res = await query(`DELETE FROM scheduled_maintenance WHERE id = $1`, [id])
  return res ? res.rowCount !== null && res.rowCount > 0 : false
}

/**
 * Add an update to an existing incident
 */
export async function addIncidentUpdate(
  incidentId: string,
  data: {
    message: string
    status?: IncidentStatus
  },
): Promise<IncidentUpdate | null> {
  const res = await query<UpdateRow>(
    `INSERT INTO incident_updates (incident_id, message, status)
     VALUES ($1, $2, $3)
     RETURNING *`,
    [incidentId, data.message, data.status ?? null],
  )
  if (!res || res.rows.length === 0) return null
  return rowToUpdate(res.rows[0])
}

/**
 * Calculate incident metrics
 */
export async function getIncidentMetrics(days: number = 90): Promise<{
  totalIncidents: number
  resolvedIncidents: number
  averageResolutionTime: number | null
  incidentsBySeverity: Record<IncidentSeverity, number>
}> {
  const since = new Date()
  since.setDate(since.getDate() - days)

  const res = await query<{
    total: string
    resolved: string
    avg_resolution_seconds: string | null
    minor: string
    major: string
    critical: string
  }>(
    `SELECT
       count(*) AS total,
       count(*) FILTER (WHERE status = 'resolved') AS resolved,
       EXTRACT(EPOCH FROM avg(resolved_at - created_at)) FILTER (WHERE resolved_at IS NOT NULL) AS avg_resolution_seconds,
       count(*) FILTER (WHERE severity = 'minor') AS minor,
       count(*) FILTER (WHERE severity = 'major') AS major,
       count(*) FILTER (WHERE severity = 'critical') AS critical
     FROM incidents
     WHERE created_at >= $1`,
    [since.toISOString()],
  )

  if (!res || res.rows.length === 0) {
    return {
      totalIncidents: 0,
      resolvedIncidents: 0,
      averageResolutionTime: null,
      incidentsBySeverity: { minor: 0, major: 0, critical: 0 },
    }
  }

  const row = res.rows[0]
  return {
    totalIncidents: parseInt(row.total, 10),
    resolvedIncidents: parseInt(row.resolved, 10),
    averageResolutionTime: row.avg_resolution_seconds
      ? parseFloat(row.avg_resolution_seconds)
      : null,
    incidentsBySeverity: {
      minor: parseInt(row.minor, 10),
      major: parseInt(row.major, 10),
      critical: parseInt(row.critical, 10),
    },
  }
}
