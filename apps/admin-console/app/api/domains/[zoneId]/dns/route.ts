import { NextResponse } from 'next/server'
import { listDNSRecords, createDNSRecord } from '@/lib/cloudflare-service'

/**
 * GET /api/domains/[zoneId]/dns - List DNS records for a zone
 */
export async function GET(
  request: Request,
  { params }: { params: Promise<{ zoneId: string }> }
) {
  try {
    const { zoneId } = await params
    const records = await listDNSRecords(zoneId)
    return NextResponse.json({ success: true, data: records.result })
  } catch (error) {
    console.error('[Dispatch API] Error fetching DNS records:', error)
    return NextResponse.json(
      {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch DNS records',
      },
      { status: 500 }
    )
  }
}

/**
 * POST /api/domains/[zoneId]/dns - Create a DNS record
 */
export async function POST(
  request: Request,
  { params }: { params: Promise<{ zoneId: string }> }
) {
  try {
    const { zoneId } = await params
    const body = await request.json()

    if (!body.type || !body.name || !body.content) {
      return NextResponse.json(
        { success: false, error: 'type, name, and content are required' },
        { status: 400 }
      )
    }

    const record = await createDNSRecord(zoneId, {
      type: body.type,
      name: body.name,
      content: body.content,
      ttl: body.ttl,
      proxied: body.proxied,
      comment: body.comment,
    })

    return NextResponse.json({ success: true, data: record })
  } catch (error) {
    console.error('[Dispatch API] Error creating DNS record:', error)
    return NextResponse.json(
      {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to create DNS record',
      },
      { status: 500 }
    )
  }
}
