import { NextResponse } from 'next/server'
import { getTimeline } from '@/lib/status-history'
import { getSiteConfig } from '@/lib/config'

const VALID_WINDOWS = new Set([5, 10, 15, 20, 30, 60])

/**
 * GET /api/status/timeline?hours=24&window=5
 *
 * Returns per-service status in configurable windows for timeline visualization.
 * `window` must evenly divide 60: 5, 10, 15, 20, 30, or 60. Defaults to 5.
 * Max 168 hours (7 days).
 */
export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url)
    const hours = Math.min(
      Math.max(parseInt(searchParams.get('hours') || '24', 10) || 24, 1),
      168,
    )
    const windowParam = parseInt(searchParams.get('window') || '5', 10)
    const windowMinutes = VALID_WINDOWS.has(windowParam) ? windowParam : 5

    const timeline = await getTimeline(hours, windowMinutes)

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
