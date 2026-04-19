import { NextResponse } from 'next/server'
import { timingSafeBearer } from '@/lib/auth'
import {
  detectStaleOutages,
  formatDigest,
  postDigestToWebhook,
} from '@/lib/stale-digest'

/**
 * POST /api/status/stale-digest
 *
 * Daily digest: scan status history for services that have been in outage or
 * degraded continuously for more than THRESHOLD_HOURS (default 24), and POST
 * the summary to STALE_DIGEST_WEBHOOK_URL (Slack-compatible). Silently no-op
 * if nothing is stale.
 *
 * Protected by the same CRON_SECRET bearer as /api/status/record so we can
 * reuse the existing secret pipeline.
 *
 * When STALE_DIGEST_WEBHOOK_URL is unset we still return the digest in the
 * JSON response and log it — Loki scrapes stdout so the on-call operator
 * can still see the signal via logs alone.
 */
export async function POST(request: Request) {
  const auth = request.headers.get('authorization')
  const cronSecret = process.env.CRON_SECRET
  if (!cronSecret) {
    return NextResponse.json(
      { error: 'CRON_SECRET not configured' },
      { status: 500 },
    )
  }
  if (!timingSafeBearer(auth, cronSecret)) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const thresholdHours = parseInt(
    process.env.STALE_DIGEST_THRESHOLD_HOURS ?? '24',
    10,
  )
  const webhookUrl = process.env.STALE_DIGEST_WEBHOOK_URL ?? ''

  try {
    const offenders = await detectStaleOutages({ thresholdHours })

    // Silently bail when nothing is stale — no spam.
    if (offenders.length === 0) {
      return NextResponse.json({ stale: 0, threshold_hours: thresholdHours })
    }

    const summary = formatDigest(offenders, thresholdHours)
    let posted = false
    if (webhookUrl) {
      posted = await postDigestToWebhook(webhookUrl, summary)
    } else {
      // Structured log line for Loki in the no-webhook path.
      console.log(
        JSON.stringify({
          msg: 'stale_outage_digest',
          threshold_hours: thresholdHours,
          stale_count: offenders.length,
          offenders,
        }),
      )
    }

    return NextResponse.json({
      stale: offenders.length,
      threshold_hours: thresholdHours,
      webhook_posted: posted,
      summary,
      offenders,
    })
  } catch (error) {
    console.error('Stale digest error:', error)
    return NextResponse.json(
      { error: 'Failed to compute stale digest' },
      { status: 500 },
    )
  }
}
