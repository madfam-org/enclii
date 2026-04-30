import { NextResponse } from 'next/server'
import { getDispatchDomains, commissionDomain } from '@/lib/cloudflare-service'
import type { CommissionDomainRequest } from '@/types/cloudflare'

/**
 * GET /api/domains - List all domains in the ecosystem
 */
export async function GET() {
  try {
    const domains = await getDispatchDomains()
    return NextResponse.json({ success: true, data: domains })
  } catch (error) {
    console.error('[Dispatch API] Error fetching domains:', error)
    return NextResponse.json(
      {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch domains',
      },
      { status: 500 }
    )
  }
}

/**
 * POST /api/domains - Commission a new domain
 */
export async function POST(request: Request) {
  try {
    const body = (await request.json()) as CommissionDomainRequest

    if (!body.domain) {
      return NextResponse.json(
        { success: false, error: 'Domain is required' },
        { status: 400 }
      )
    }

    // Validate domain format
    const domainRegex = /^[a-zA-Z0-9][a-zA-Z0-9-]*\.[a-zA-Z]{2,}$/
    if (!domainRegex.test(body.domain)) {
      return NextResponse.json(
        { success: false, error: 'Invalid domain format' },
        { status: 400 }
      )
    }

    const result = await commissionDomain(body)
    return NextResponse.json({ success: true, data: result })
  } catch (error) {
    console.error('[Dispatch API] Error commissioning domain:', error)
    return NextResponse.json(
      {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to commission domain',
      },
      { status: 500 }
    )
  }
}
