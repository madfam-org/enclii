/**
 * Auto-Incident Detection
 *
 * Bridges the health check pipeline with the incidents system.
 * When a service fails N consecutive checks, an incident is auto-created.
 * When it recovers, the auto-incident is auto-resolved.
 *
 * Manual incidents (without "[Auto]" prefix) are never touched.
 */

import { query } from './db'
import { createIncident, updateIncident } from './incidents'
import type { HealthCheckResult } from './types'

interface StatusCheckRow {
  status: string
}

interface ActiveIncidentRow {
  id: string
  title: string
}

interface ActiveAutoIncidentRow extends ActiveIncidentRow {
  affected_services: string[] | null
}

/**
 * Detect persistent service failures and manage auto-incidents.
 *
 * Called after recordStatusSnapshot() in the /api/status/record handler.
 * For each service, checks a trailing window of status_checks:
 * - Last `threshold` all bad + no active incident → create incident.
 * - Last `threshold * 2` all good + active auto-incident → resolve.
 *
 * The asymmetric resolve window (2× create) is hysteresis: it prevents
 * flapping services from creating/resolving an incident every cycle.
 */
export async function detectAndManageIncidents(
  results: HealthCheckResult[],
  threshold: number = 2,
): Promise<{ created: string[]; resolved: string[] }> {
  const created: string[] = []
  const resolved: string[] = []
  const resolveThreshold = threshold * 2

  for (const result of results) {
    const recent = await query<StatusCheckRow>(
      `SELECT status FROM status_checks
       WHERE service = $1
       ORDER BY checked_at DESC
       LIMIT $2`,
      [result.service, resolveThreshold],
    )

    if (!recent || recent.rows.length < threshold) continue

    const createWindow = recent.rows.slice(0, threshold)
    const allBad = createWindow.every(
      (r) => r.status === 'outage' || r.status === 'degraded',
    )
    const allGood =
      recent.rows.length >= resolveThreshold &&
      recent.rows.every((r) => r.status === 'operational')

    // Check for existing active incident for this service (any kind)
    const activeAny = await query<ActiveIncidentRow>(
      `SELECT id, title FROM incidents
       WHERE $1 = ANY(affected_services)
         AND status != 'resolved'
       ORDER BY created_at DESC LIMIT 1`,
      [result.service],
    )
    const hasActiveIncident = activeAny && activeAny.rows.length > 0

    // CREATE: persistent failure + no existing incident
    if (allBad && !hasActiveIncident) {
      const worstStatus = createWindow.some((r) => r.status === 'outage')
        ? 'outage'
        : 'degraded'
      const statusLabel = worstStatus === 'outage' ? 'Outage' : 'Degraded'
      try {
        const incident = await createIncident({
          title: `[Auto] ${result.service} ${statusLabel}`,
          severity: worstStatus === 'outage' ? 'major' : 'minor',
          affectedServices: [result.service],
          initialMessage:
            result.error ||
            `Health check detected ${worstStatus} (HTTP ${result.statusCode ?? 'N/A'})`,
        })
        created.push(incident.id)
      } catch (err) {
        console.error(`Failed to create auto-incident for ${result.service}:`, err)
      }
    }

    // RESOLVE: service recovered + has auto-incident (never touch manual incidents)
    if (allGood && hasActiveIncident) {
      const row = activeAny!.rows[0]
      if (typeof row.title === 'string' && row.title.startsWith('[Auto]')) {
        try {
          await updateIncident(row.id, {
            status: 'resolved',
            message: 'Service recovered automatically',
          })
          resolved.push(row.id)
        } catch (err) {
          console.error(`Failed to resolve auto-incident ${row.id}:`, err)
        }
      }
    }
  }

  await resolveOrphanedAutoIncidents(results, resolved)

  return { created, resolved }
}

async function resolveOrphanedAutoIncidents(
  results: HealthCheckResult[],
  resolved: string[],
): Promise<void> {
  const currentServices = new Set(results.map((result) => result.service))
  const alreadyResolved = new Set(resolved)

  const activeAutoIncidents = await query<ActiveAutoIncidentRow>(
    `SELECT id, title, affected_services FROM incidents
     WHERE status != 'resolved'
       AND title LIKE '[Auto]%'
     ORDER BY created_at DESC`,
  )

  if (!activeAutoIncidents || activeAutoIncidents.rows.length === 0) return

  for (const row of activeAutoIncidents.rows) {
    if (alreadyResolved.has(row.id)) continue
    if (typeof row.title !== 'string' || !row.title.startsWith('[Auto]')) continue

    const affectedServices = Array.isArray(row.affected_services)
      ? row.affected_services
      : []
    const isOrphaned =
      affectedServices.length === 0 ||
      affectedServices.every((service) => !currentServices.has(service))

    if (!isOrphaned) continue

    try {
      await updateIncident(row.id, {
        status: 'resolved',
        message:
          'Auto-resolved stale incident: affected service is no longer in the active status catalog',
      })
      resolved.push(row.id)
      alreadyResolved.add(row.id)
    } catch (err) {
      console.error(`Failed to resolve orphaned auto-incident ${row.id}:`, err)
    }
  }
}
