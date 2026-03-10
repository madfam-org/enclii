import { NextRequest, NextResponse } from 'next/server'
import { getUptimeSummary, getAllUptimeSummaries } from '@/lib/status-history'
import { getDatabaseUrl } from '@/lib/config'

/**
 * GET /api/status/uptime
 *
 * Returns uptime percentages from status_daily.
 * Query params:
 *   service - optional, specific service name
 *   days    - number of days (default 7, max 90)
 */
export async function GET(request: NextRequest) {
  if (!getDatabaseUrl()) {
    return NextResponse.json(
      { error: 'Database not configured' },
      { status: 503 },
    )
  }

  const { searchParams } = request.nextUrl
  const service = searchParams.get('service')
  const daysParam = searchParams.get('days')
  const days = Math.min(Math.max(parseInt(daysParam || '7', 10) || 7, 1), 90)

  try {
    if (service) {
      const uptimePct = await getUptimeSummary(service, days)
      return NextResponse.json(
        {
          services: [{ service, days, uptimePct }],
          queriedAt: new Date().toISOString(),
        },
        {
          headers: {
            'Cache-Control': 'public, s-maxage=300, stale-while-revalidate=600',
          },
        },
      )
    }

    const summaries = await getAllUptimeSummaries(days)
    return NextResponse.json(
      {
        services: summaries.map(s => ({ ...s, days })),
        queriedAt: new Date().toISOString(),
      },
      {
        headers: {
          'Cache-Control': 'public, s-maxage=300, stale-while-revalidate=600',
        },
      },
    )
  } catch (err) {
    console.error('Uptime API error:', err)
    return NextResponse.json(
      { error: 'Failed to query uptime data' },
      { status: 500 },
    )
  }
}
