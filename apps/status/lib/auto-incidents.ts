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

/**
 * Detect persistent service failures and manage auto-incidents.
 *
 * Called after recordStatusSnapshot() in the /api/status/record handler.
 * For each service, checks the last `threshold` status_checks rows:
 * - All bad + no active incident → create "[Auto] {service} {Outage|Degraded}"
 * - All good + active auto-incident → resolve with "recovered automatically"
 */
export async function detectAndManageIncidents(
  results: HealthCheckResult[],
  threshold: number = 2,
): Promise<{ created: string[]; resolved: string[] }> {
  const created: string[] = []
  const resolved: string[] = []

  for (const result of results) {
    const recent = await query<StatusCheckRow>(
      `SELECT status FROM status_checks
       WHERE service = $1
       ORDER BY checked_at DESC
       LIMIT $2`,
      [result.service, threshold],
    )

    if (!recent || recent.rows.length < threshold) continue

    const allBad = recent.rows.every(
      (r) => r.status === 'outage' || r.status === 'degraded',
    )
    const allGood = recent.rows.every((r) => r.status === 'operational')

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
      const worstStatus = recent.rows.some((r) => r.status === 'outage')
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

  return { created, resolved }
}
