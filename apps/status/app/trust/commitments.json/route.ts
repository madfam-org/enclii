import { NextResponse } from 'next/server'
import { buildCommitmentsJson } from '@/lib/trust-commitments'

/**
 * Machine-readable snapshot of the commitments published at /trust.
 *
 * Served at the literal path /trust/commitments.json so that the URL matches
 * the filename customers might link to in contracts or integrations.
 *
 * This is a public endpoint. Do not include any tenant-scoped data.
 */
export const revalidate = 3600 // 1 hour — commitments change on the order of weeks

export async function GET() {
  return NextResponse.json(buildCommitmentsJson(), {
    headers: {
      'Cache-Control': 'public, max-age=600, s-maxage=3600',
    },
  })
}
