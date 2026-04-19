/**
 * Stale Outage Digest
 *
 * Identifies services that have been in a non-operational state continuously
 * for more than `thresholdHours` hours. Designed to be called by a daily
 * CronJob so the operator notices drift like "6 services silently in outage
 * for 4+ days" before it rots further.
 *
 * Returns the list of offenders plus a human-readable summary. Callers may
 * POST the summary to a webhook (Slack-compatible) via STALE_DIGEST_WEBHOOK_URL.
 *
 * Runs against the same `status_checks` table that auto-incidents uses, so no
 * new schema is introduced.
 */

import { query } from './db'

interface StaleServiceRow {
  service: string
  url: string
  group_name: string
  last_operational: string | null
  last_status: string
  last_checked_at: string
  bad_count: string
  total_count: string
}

export interface StaleOutage {
  service: string
  url: string
  group: string
  /** Most recent status observed (outage|degraded). */
  lastStatus: 'outage' | 'degraded'
  /**
   * Timestamp of the most recent operational check, or null if we have never
   * observed the service operational in the retention window.
   */
  lastOperational: string | null
  /** Hours since the last operational check (capped at retentionHours). */
  hoursInOutage: number
  /** How many of the sampled checks were bad. */
  badChecks: number
  /** Total checks sampled. */
  totalChecks: number
}

export interface StaleDigestOptions {
  /** Minimum continuous outage duration to report. Default 24h. */
  thresholdHours?: number
  /**
   * How far back to look. We inherit the 25h raw-check retention window from
   * status_history.pruneOldRecords, so asking for >25h only gains signal via
   * the status_hourly rollup; we use raw checks and cap reporting at the
   * retention window for simplicity.
   */
  retentionHours?: number
}

/**
 * Find every service whose recent check stream has been non-operational for
 * at least `thresholdHours`.
 *
 * Implementation: for each service seen in the last retention window, find
 * the most recent `operational` check. If there isn't one (or it was more
 * than `thresholdHours` ago) AND the latest check is non-operational, the
 * service is considered stale.
 */
export async function detectStaleOutages(
  options: StaleDigestOptions = {},
): Promise<StaleOutage[]> {
  const thresholdHours = options.thresholdHours ?? 24
  const retentionHours = options.retentionHours ?? 25

  const res = await query<StaleServiceRow>(
    `WITH recent AS (
       SELECT service, url, group_name, status, checked_at
       FROM status_checks
       WHERE checked_at >= NOW() - ($1 || ' hours')::interval
     ),
     last_good AS (
       SELECT service, MAX(checked_at) AS last_operational
       FROM recent
       WHERE status = 'operational'
       GROUP BY service
     ),
     latest AS (
       SELECT DISTINCT ON (service)
         service, url, group_name, status, checked_at
       FROM recent
       ORDER BY service, checked_at DESC
     ),
     counts AS (
       SELECT
         service,
         COUNT(*) FILTER (WHERE status IN ('outage','degraded'))::text AS bad_count,
         COUNT(*)::text AS total_count
       FROM recent
       GROUP BY service
     )
     SELECT
       l.service,
       l.url,
       l.group_name,
       lg.last_operational::text AS last_operational,
       l.status AS last_status,
       l.checked_at::text AS last_checked_at,
       c.bad_count,
       c.total_count
     FROM latest l
     LEFT JOIN last_good lg USING (service)
     LEFT JOIN counts c USING (service)
     WHERE l.status IN ('outage', 'degraded')
       AND (
         lg.last_operational IS NULL
         OR lg.last_operational < NOW() - ($2 || ' hours')::interval
       )
     ORDER BY l.service`,
    [retentionHours, thresholdHours],
  )

  if (!res) return []

  const now = Date.now()
  return res.rows.map((row) => {
    const lastOpMs = row.last_operational
      ? new Date(row.last_operational).getTime()
      : null
    const hoursInOutage =
      lastOpMs === null
        ? retentionHours
        : Math.max(0, Math.round(((now - lastOpMs) / (1000 * 60 * 60)) * 10) / 10)

    return {
      service: row.service,
      url: row.url,
      group: row.group_name,
      lastStatus: (row.last_status === 'outage' ? 'outage' : 'degraded') as
        | 'outage'
        | 'degraded',
      lastOperational: row.last_operational,
      hoursInOutage,
      badChecks: parseInt(row.bad_count ?? '0', 10),
      totalChecks: parseInt(row.total_count ?? '0', 10),
    }
  })
}

/**
 * Human-readable Slack/webhook body for a set of stale outages.
 */
export function formatDigest(
  offenders: StaleOutage[],
  thresholdHours: number,
): string {
  if (offenders.length === 0) {
    return `No services have been in outage continuously for >${thresholdHours}h.`
  }
  const lines = offenders.map((o) => {
    const age =
      o.lastOperational === null
        ? `>${Math.round(o.hoursInOutage)}h (never observed operational in window)`
        : `${o.hoursInOutage}h`
    return `- ${o.service} [${o.group}] — ${o.lastStatus} for ${age} — ${o.url}`
  })
  const header =
    offenders.length === 1
      ? `1 service has been in outage for >${thresholdHours}h:`
      : `${offenders.length} services have been in outage for >${thresholdHours}h:`
  return [header, ...lines].join('\n')
}

/**
 * POST the digest to a Slack-compatible incoming webhook.
 * Returns true iff the webhook returned a 2xx.
 *
 * If `webhookUrl` is falsy, returns false without posting (caller falls back
 * to stdout). We do NOT throw on HTTP failure — digest runs shouldn't cascade
 * a CronJob failure because a webhook was slow.
 */
export async function postDigestToWebhook(
  webhookUrl: string,
  text: string,
): Promise<boolean> {
  if (!webhookUrl) return false
  try {
    const res = await fetch(webhookUrl, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ text }),
    })
    return res.ok
  } catch (err) {
    console.error('Failed to post stale digest to webhook:', err)
    return false
  }
}
