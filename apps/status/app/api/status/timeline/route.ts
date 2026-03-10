import { NextResponse } from 'next/server'
import { getTimeline } from '@/lib/status-history'
import { getSiteConfig } from '@/lib/config'

/**
 * GET /api/status/timeline?hours=24
 *
 * Returns per-service status in 5-minute windows for timeline visualization.
 * Max 168 hours (7 days).
 */
export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url)
    const hours = Math.min(
      Math.max(parseInt(searchParams.get('hours') || '24', 10) || 24, 1),
      168,
    )

    const timeline = await getTimeline(hours)

    // Enrich timeline services with href and filter out stale DB entries
    // whose service names no longer match the current configmap.
    const config = getSiteConfig()
    const configNames = new Set(config.services.map(s => s.name))
    const hrefMap = new Map<string, string>()
    for (const svc of config.services) {
      if (svc.href) {
        hrefMap.set(svc.name, svc.href)
      }
    }

    timeline.services = timeline.services.filter(svc => {
      if (!configNames.has(svc.service)) return false
      const href = hrefMap.get(svc.service)
      if (href) svc.href = href
      return true
    })

    return NextResponse.json(timeline, {
      headers: {
        'Cache-Control': 'public, s-maxage=60, stale-while-revalidate=120',
      },
    })
  } catch (error) {
    console.error('Timeline API error:', error)
    return NextResponse.json(
      { error: 'Failed to fetch timeline' },
      { status: 500 },
    )
  }
}
