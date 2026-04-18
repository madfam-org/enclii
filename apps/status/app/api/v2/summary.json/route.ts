import { NextResponse } from 'next/server'
import { checkAllServices } from '@/lib/health-checker'
import { getSiteConfig } from '@/lib/config'
import { getActiveIncidents, getActiveMaintenances, isDatabaseConfigured } from '@/lib/incidents'
import { buildSummary } from '@/lib/statuspage-v2'

/**
 * Statuspage-compatible `/api/v2/summary.json` shim (RFC 0002 S2).
 *
 * Read-only projection over our existing health-check + incident data into
 * the canonical Atlassian Statuspage schema. See `lib/statuspage-v2.ts` for
 * the mapping.
 *
 * Downstream consumers auto-detect this shape:
 *   - Better Uptime
 *   - Datadog status page integration
 *   - Slack /statuspage bots
 *   - DataStation / Notion widgets
 */
export async function GET() {
  try {
    const config = getSiteConfig()
    const services = await checkAllServices(config.services)

    // Pull active incidents + maintenances if DB is wired up; otherwise
    // the shim still returns the correct skeleton (empty arrays) so that
    // downstream consumers don't blow up during bootstrap.
    let incidents: Awaited<ReturnType<typeof getActiveIncidents>> = []
    let scheduledMaintenances: Awaited<ReturnType<typeof getActiveMaintenances>> = []
    if (isDatabaseConfigured()) {
      try {
        incidents = await getActiveIncidents()
      } catch (err) {
        console.error('v2 summary: failed to load incidents, returning empty', err)
      }
      try {
        scheduledMaintenances = await getActiveMaintenances()
      } catch (err) {
        console.error('v2 summary: failed to load maintenances, returning empty', err)
      }
    }

    const summary = buildSummary({
      pageName: config.name,
      pageUrl: config.url,
      services,
      incidents,
      scheduledMaintenances,
    })

    return NextResponse.json(summary, {
      headers: {
        'Cache-Control': 'public, s-maxage=30, stale-while-revalidate=60',
        // Advertise the Statuspage shape so consumers can auto-detect.
        'X-Statuspage-Compat': 'v2',
      },
    })
  } catch (error) {
    console.error('v2 summary error:', error)
    return NextResponse.json(
      { error: 'Failed to build summary' },
      { status: 500 },
    )
  }
}
