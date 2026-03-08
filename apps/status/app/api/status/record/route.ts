import { NextResponse } from 'next/server'
import { checkAllServices } from '@/lib/health-checker'
import { getSiteConfig } from '@/lib/config'
import {
  recordStatusSnapshot,
  aggregateHourly,
  aggregateDaily,
  pruneOldRecords,
} from '@/lib/status-history'

/**
 * POST /api/status/record
 *
 * Record a status snapshot for all configured services.
 * Called every 60s by K8s CronJob. Protected by CRON_SECRET bearer token.
 */
export async function POST(request: Request) {
  // Verify cron secret
  const auth = request.headers.get('authorization')
  const cronSecret = process.env.CRON_SECRET

  if (!cronSecret) {
    return NextResponse.json(
      { error: 'CRON_SECRET not configured' },
      { status: 500 },
    )
  }

  if (auth !== `Bearer ${cronSecret}`) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  try {
    const config = getSiteConfig()
    const results = await checkAllServices(config.services)
    const recorded = await recordStatusSnapshot(results)

    // Aggregate and prune every ~15 minutes
    const minute = new Date().getMinutes()
    let pruned = null
    if (minute % 15 === 0) {
      await aggregateHourly()
      await aggregateDaily()
      pruned = await pruneOldRecords()
    }

    return NextResponse.json({
      recorded,
      services: results.length,
      ...(pruned && { pruned }),
    })
  } catch (error) {
    console.error('Status recording error:', error)
    return NextResponse.json(
      { error: 'Failed to record status' },
      { status: 500 },
    )
  }
}
