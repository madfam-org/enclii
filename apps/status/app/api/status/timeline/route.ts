import { NextResponse } from 'next/server'
import { getTimeline } from '@/lib/status-history'

/**
 * GET /api/status/timeline?hours=24
 *
 * Returns per-service status in 15-minute windows for timeline visualization.
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
