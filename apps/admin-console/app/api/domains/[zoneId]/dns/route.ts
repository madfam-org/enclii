import { cookies } from 'next/headers'
import { NextResponse } from 'next/server'
import { switchyardProviderCall } from '@/lib/switchyard-proxy'

/**
 * GET /api/domains/[zoneId]/dns - List DNS records via Switchyard
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ zoneId: string }> }
) {
  try {
    const { zoneId } = await params
    const store = await cookies()
    const token = store.get('dispatch_auth')?.value || store.get('admin_auth')?.value
    const { ok, data, status } = await switchyardProviderCall('cloudflare', 'dns', {
      dry_run: true,
      args: { zone_id: zoneId },
    }, token)
    if (!ok) {
      return NextResponse.json(
        { success: false, error: data.summary || 'Failed to fetch DNS records' },
        { status: status >= 400 ? status : 502 }
      )
    }
    const records = (data.data as { records?: unknown[] })?.records ?? []
    return NextResponse.json({ success: true, data: records })
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
 * POST /api/domains/[zoneId]/dns - Create DNS record via Switchyard dns-apply
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

    const store = await cookies()
    const token = store.get('dispatch_auth')?.value || store.get('admin_auth')?.value
    const { ok, data, status } = await switchyardProviderCall('cloudflare', 'dns-apply', {
      dry_run: false,
      reason: body.reason?.trim() || `Dispatch DNS create in zone ${zoneId}`,
      args: {
        zone_id: zoneId,
        target: body.name,
        type: body.type,
        content: body.content,
        proxied: body.proxied ? 'true' : 'false',
      },
    }, token)

    if (!ok) {
      return NextResponse.json(
        { success: false, error: data.summary || 'Failed to create DNS record' },
        { status: status >= 400 ? status : 502 }
      )
    }

    return NextResponse.json({ success: true, data: data.data })
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
