/**
 * Status History Library
 *
 * Database-backed status recording, timeline queries, and aggregation.
 * Falls back to no-op behavior when DATABASE_URL is not configured.
 */

import { query } from './db'
import type {
  HealthCheckResult,
  ServiceStatus,
  ServiceTimeline,
  TimelineResponse,
  TimelineSlot,
} from './types'

// -- Row types for pg results --

interface StatusCheckRow extends Record<string, unknown> {
  service: string
  url: string
  group_name: string
  status: ServiceStatus
  response_ms: number | null
  status_code: number | null
  error: string | null
  checked_at: string
}

interface TimelineWindowRow extends Record<string, unknown> {
  service: string
  url: string
  group_name: string
  window_start: string
  total_checks: string
  operational: string
  degraded: string
  outage: string
  maintenance: string
  avg_response_ms: string | null
}

// -- Schema Management --

let schemaEnsured = false

/**
 * Create tables if they don't exist (idempotent).
 */
export async function ensureSchema(): Promise<void> {
  if (schemaEnsured) return

  await query(`
    CREATE TABLE IF NOT EXISTS status_checks (
      id          BIGSERIAL PRIMARY KEY,
      service     TEXT NOT NULL,
      url         TEXT NOT NULL,
      group_name  TEXT NOT NULL,
      status      TEXT NOT NULL,
      response_ms INTEGER,
      status_code INTEGER,
      error       TEXT,
      checked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
    )
  `)

  await query(`
    CREATE INDEX IF NOT EXISTS idx_status_checks_service_time
      ON status_checks (service, checked_at DESC)
  `)

  await query(`
    CREATE TABLE IF NOT EXISTS status_hourly (
      id           BIGSERIAL PRIMARY KEY,
      service      TEXT NOT NULL,
      hour         TIMESTAMPTZ NOT NULL,
      total_checks INTEGER NOT NULL DEFAULT 0,
      operational  INTEGER NOT NULL DEFAULT 0,
      degraded     INTEGER NOT NULL DEFAULT 0,
      outage       INTEGER NOT NULL DEFAULT 0,
      avg_response INTEGER,
      uptime_pct   NUMERIC(5,2),
      UNIQUE(service, hour)
    )
  `)

  await query(`
    CREATE TABLE IF NOT EXISTS status_daily (
      id           BIGSERIAL PRIMARY KEY,
      service      TEXT NOT NULL,
      day          DATE NOT NULL,
      total_checks INTEGER NOT NULL DEFAULT 0,
      operational  INTEGER NOT NULL DEFAULT 0,
      degraded     INTEGER NOT NULL DEFAULT 0,
      outage       INTEGER NOT NULL DEFAULT 0,
      avg_response INTEGER,
      uptime_pct   NUMERIC(5,2),
      UNIQUE(service, day)
    )
  `)

  // Incident management tables
  await query(`
    CREATE TABLE IF NOT EXISTS incidents (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      title TEXT NOT NULL,
      status TEXT NOT NULL DEFAULT 'investigating',
      severity TEXT NOT NULL DEFAULT 'minor',
      affected_services TEXT[] NOT NULL DEFAULT '{}',
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      resolved_at TIMESTAMPTZ
    )
  `)

  await query(`
    CREATE TABLE IF NOT EXISTS incident_updates (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
      message TEXT NOT NULL,
      status TEXT,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    )
  `)

  await query(`
    CREATE TABLE IF NOT EXISTS scheduled_maintenance (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      title TEXT NOT NULL,
      description TEXT,
      affected_services TEXT[] NOT NULL DEFAULT '{}',
      scheduled_start TIMESTAMPTZ NOT NULL,
      scheduled_end TIMESTAMPTZ NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    )
  `)

  schemaEnsured = true
}

// -- Recording --

/**
 * Persist a batch of health check results to status_checks.
 */
export async function recordStatusSnapshot(
  results: HealthCheckResult[],
): Promise<number> {
  await ensureSchema()

  if (results.length === 0) return 0

  // Build a bulk INSERT with parameterized values
  const values: unknown[] = []
  const placeholders: string[] = []
  let idx = 1

  for (const r of results) {
    placeholders.push(
      `($${idx++}, $${idx++}, $${idx++}, $${idx++}, $${idx++}, $${idx++}, $${idx++})`,
    )
    values.push(
      r.service,
      r.url,
      r.group,
      r.status,
      r.responseTime !== null ? Math.round(r.responseTime) : null,
      r.statusCode ?? null,
      r.error ?? null,
    )
  }

  const res = await query(
    `INSERT INTO status_checks (service, url, group_name, status, response_ms, status_code, error)
     VALUES ${placeholders.join(', ')}`,
    values,
  )

  return res?.rowCount ?? 0
}

// -- Timeline Query --

/**
 * Determine the worst status in a window using priority ordering.
 */
function worstStatus(counts: {
  operational: number
  degraded: number
  outage: number
  maintenance: number
}): ServiceStatus {
  // Priority order: outage < degraded < maintenance < operational
  const statusCounts: [ServiceStatus, number][] = [
    ['outage', counts.outage],
    ['degraded', counts.degraded],
    ['maintenance', counts.maintenance],
    ['operational', counts.operational],
  ]
  for (const [status, count] of statusCounts) {
    if (count > 0) return status
  }
  return 'unknown'
}

/**
 * Get timeline data for all services over the given number of hours.
 * Returns data in 15-minute windows.
 */
export async function getTimeline(hours: number = 24): Promise<TimelineResponse> {
  await ensureSchema()

  const windowMinutes = 5
  const now = new Date()
  const from = new Date(now.getTime() - hours * 60 * 60 * 1000)

  // Query status_checks aggregated into 15-min windows
  const res = await query<TimelineWindowRow>(
    `SELECT
       service,
       url,
       group_name,
       date_trunc('hour', checked_at) +
         (EXTRACT(minute FROM checked_at)::int / $1) * ($1 || ' minutes')::interval
         AS window_start,
       COUNT(*)::text AS total_checks,
       COUNT(*) FILTER (WHERE status = 'operational')::text AS operational,
       COUNT(*) FILTER (WHERE status = 'degraded')::text AS degraded,
       COUNT(*) FILTER (WHERE status = 'outage')::text AS outage,
       COUNT(*) FILTER (WHERE status = 'maintenance')::text AS maintenance,
       ROUND(AVG(response_ms))::text AS avg_response_ms
     FROM status_checks
     WHERE checked_at >= $2
     GROUP BY service, url, group_name, window_start
     ORDER BY service, window_start`,
    [windowMinutes, from.toISOString()],
  )

  // Build a map of service -> window data
  const serviceMap = new Map<
    string,
    {
      url: string
      group: string
      windows: Map<string, TimelineWindowRow>
    }
  >()

  if (res) {
    for (const row of res.rows) {
      let entry = serviceMap.get(row.service)
      if (!entry) {
        entry = { url: row.url, group: row.group_name, windows: new Map() }
        serviceMap.set(row.service, entry)
      }
      entry.windows.set(new Date(row.window_start).toISOString(), row)
    }
  }

  // Generate all expected time slots
  const totalSlots = Math.ceil((hours * 60) / windowMinutes)
  // Align "from" to the nearest 15-min boundary
  const alignedFrom = new Date(from)
  alignedFrom.setMinutes(
    Math.floor(alignedFrom.getMinutes() / windowMinutes) * windowMinutes,
    0,
    0,
  )

  const services: ServiceTimeline[] = []

  for (const [service, data] of serviceMap) {
    const slots: TimelineSlot[] = []
    let totalChecks = 0
    let operationalChecks = 0

    for (let i = 0; i < totalSlots; i++) {
      const slotStart = new Date(
        alignedFrom.getTime() + i * windowMinutes * 60 * 1000,
      )
      const slotEnd = new Date(
        slotStart.getTime() + windowMinutes * 60 * 1000,
      )

      // Match window row by comparing ISO strings (PostgreSQL returns timestamptz)
      const windowRow = data.windows.get(slotStart.toISOString())

      if (windowRow) {
        const counts = {
          operational: parseInt(windowRow.operational, 10),
          degraded: parseInt(windowRow.degraded, 10),
          outage: parseInt(windowRow.outage, 10),
          maintenance: parseInt(windowRow.maintenance, 10),
        }
        const checks = parseInt(windowRow.total_checks, 10)
        totalChecks += checks
        operationalChecks += counts.operational

        slots.push({
          start: slotStart.toISOString(),
          end: slotEnd.toISOString(),
          status: worstStatus(counts),
          checks,
          avgResponseMs: windowRow.avg_response_ms
            ? parseInt(windowRow.avg_response_ms, 10)
            : null,
        })
      } else {
        slots.push({
          start: slotStart.toISOString(),
          end: slotEnd.toISOString(),
          status: 'unknown',
          checks: 0,
          avgResponseMs: null,
        })
      }
    }

    const uptime24h =
      totalChecks > 0
        ? Math.round((operationalChecks / totalChecks) * 10000) / 100
        : 100

    services.push({
      service,
      group: data.group,
      url: data.url,
      slots,
      uptime24h,
    })
  }

  // Sort by group then service name
  services.sort((a, b) =>
    a.group === b.group
      ? a.service.localeCompare(b.service)
      : a.group.localeCompare(b.group),
  )

  return {
    services,
    from: alignedFrom.toISOString(),
    to: now.toISOString(),
    windowMinutes,
  }
}

// -- Aggregation --

/**
 * Roll up raw status_checks into status_hourly.
 */
export async function aggregateHourly(): Promise<void> {
  await ensureSchema()

  await query(`
    INSERT INTO status_hourly (service, hour, total_checks, operational, degraded, outage, avg_response, uptime_pct)
    SELECT
      service,
      date_trunc('hour', checked_at) AS hour,
      COUNT(*)::int,
      COUNT(*) FILTER (WHERE status = 'operational')::int,
      COUNT(*) FILTER (WHERE status = 'degraded')::int,
      COUNT(*) FILTER (WHERE status = 'outage')::int,
      ROUND(AVG(response_ms))::int,
      ROUND(
        COUNT(*) FILTER (WHERE status = 'operational')::numeric /
        NULLIF(COUNT(*), 0) * 100,
        2
      )
    FROM status_checks
    WHERE checked_at >= NOW() - INTERVAL '2 hours'
    GROUP BY service, date_trunc('hour', checked_at)
    ON CONFLICT (service, hour) DO UPDATE SET
      total_checks = EXCLUDED.total_checks,
      operational = EXCLUDED.operational,
      degraded = EXCLUDED.degraded,
      outage = EXCLUDED.outage,
      avg_response = EXCLUDED.avg_response,
      uptime_pct = EXCLUDED.uptime_pct
  `)
}

/**
 * Roll up status_hourly into status_daily.
 */
export async function aggregateDaily(): Promise<void> {
  await ensureSchema()

  await query(`
    INSERT INTO status_daily (service, day, total_checks, operational, degraded, outage, avg_response, uptime_pct)
    SELECT
      service,
      date_trunc('day', hour)::date AS day,
      SUM(total_checks)::int,
      SUM(operational)::int,
      SUM(degraded)::int,
      SUM(outage)::int,
      ROUND(AVG(avg_response))::int,
      ROUND(
        SUM(operational)::numeric / NULLIF(SUM(total_checks), 0) * 100,
        2
      )
    FROM status_hourly
    WHERE hour >= NOW() - INTERVAL '2 days'
    GROUP BY service, date_trunc('day', hour)::date
    ON CONFLICT (service, day) DO UPDATE SET
      total_checks = EXCLUDED.total_checks,
      operational = EXCLUDED.operational,
      degraded = EXCLUDED.degraded,
      outage = EXCLUDED.outage,
      avg_response = EXCLUDED.avg_response,
      uptime_pct = EXCLUDED.uptime_pct
  `)
}

// -- Pruning --

/**
 * Delete old records beyond retention windows.
 */
export async function pruneOldRecords(): Promise<{
  rawDeleted: number
  hourlyDeleted: number
  dailyDeleted: number
}> {
  await ensureSchema()

  const rawRes = await query(
    `DELETE FROM status_checks WHERE checked_at < NOW() - INTERVAL '25 hours'`,
  )
  const hourlyRes = await query(
    `DELETE FROM status_hourly WHERE hour < NOW() - INTERVAL '8 days'`,
  )
  const dailyRes = await query(
    `DELETE FROM status_daily WHERE day < NOW() - INTERVAL '91 days'`,
  )

  return {
    rawDeleted: rawRes?.rowCount ?? 0,
    hourlyDeleted: hourlyRes?.rowCount ?? 0,
    dailyDeleted: dailyRes?.rowCount ?? 0,
  }
}

// -- Uptime Summary --

/**
 * Calculate uptime percentage for a service over the given number of days.
 */
export async function getUptimeSummary(
  service: string,
  days: number = 7,
): Promise<number | null> {
  await ensureSchema()

  const res = await query<{ uptime_pct: string | null }>(
    `SELECT
       ROUND(
         SUM(operational)::numeric / NULLIF(SUM(total_checks), 0) * 100,
         2
       ) AS uptime_pct
     FROM status_daily
     WHERE service = $1 AND day >= NOW() - ($2 || ' days')::interval`,
    [service, days],
  )

  if (!res || res.rows.length === 0 || res.rows[0].uptime_pct === null) {
    return null
  }

  return parseFloat(res.rows[0].uptime_pct)
}

/**
 * Calculate uptime percentages for all services over the given number of days.
 * Returns a map of service name to uptime percentage.
 */
export async function getAllUptimeSummaries(
  days: number = 7,
): Promise<{ service: string; uptimePct: number | null }[]> {
  await ensureSchema()

  const res = await query<{ service: string; uptime_pct: string | null }>(
    `SELECT
       service,
       ROUND(
         SUM(operational)::numeric / NULLIF(SUM(total_checks), 0) * 100,
         2
       ) AS uptime_pct
     FROM status_daily
     WHERE day >= NOW() - ($1 || ' days')::interval
     GROUP BY service
     ORDER BY service`,
    [days],
  )

  if (!res) return []

  return res.rows.map(row => ({
    service: row.service,
    uptimePct: row.uptime_pct !== null ? parseFloat(row.uptime_pct) : null,
  }))
}
