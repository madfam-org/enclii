import { NextResponse } from 'next/server'
import { switchyardProviderCall } from '@/lib/switchyard-proxy'
import { tenantFromDomain } from '@enclii/ecosystem-tenants'
import type { CommissionDomainRequest, DispatchDomain } from '@/types/cloudflare'

type CfZone = {
  id: string
  name: string
  status: string
  name_servers: string[]
  activated_on?: string | null
  created_on?: string
}

function mapZonesToDispatch(zones: CfZone[]): DispatchDomain[] {
  return zones.map((zone) => ({
    id: zone.id,
    domain: zone.name,
    tenant: tenantFromDomain(zone.name),
    status: zone.status,
    sslStatus: zone.status === 'active' ? 'active' : 'pending',
    dnsStatus: zone.status === 'active' ? 'healthy' : 'warning',
    nameservers: zone.name_servers ?? [],
    activatedAt: zone.activated_on ?? null,
    createdAt: zone.created_on ?? '',
  }))
}

/**
 * GET /api/domains - List ecosystem domains via Switchyard Cloudflare provider
 */
export async function GET() {
  try {
    const { ok, data } = await switchyardProviderCall('cloudflare', 'zones', {
      dry_run: true,
    })
    if (!ok) {
      return NextResponse.json(
        { success: false, error: data.summary || 'Failed to fetch zones from Switchyard' },
        { status: 502 }
      )
    }
    const zones = ((data.data as { zones?: CfZone[] })?.zones ?? []) as CfZone[]
    return NextResponse.json({ success: true, data: mapZonesToDispatch(zones) })
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
 * POST /api/domains - Commission a new domain via Switchyard zone-add-apply
 */
export async function POST(request: Request) {
  try {
    const body = (await request.json()) as CommissionDomainRequest & { reason?: string }

    if (!body.domain) {
      return NextResponse.json(
        { success: false, error: 'Domain is required' },
        { status: 400 }
      )
    }

    const domainRegex = /^[a-zA-Z0-9][a-zA-Z0-9-]*\.[a-zA-Z]{2,}$/
    if (!domainRegex.test(body.domain)) {
      return NextResponse.json(
        { success: false, error: 'Invalid domain format' },
        { status: 400 }
      )
    }

    const reason = body.reason?.trim() || `Commission domain ${body.domain} via Dispatch`
    const { ok, data, status } = await switchyardProviderCall('cloudflare', 'zone-add-apply', {
      dry_run: false,
      reason,
      args: { target: body.domain.trim() },
    })

    if (!ok) {
      return NextResponse.json(
        { success: false, error: data.summary || 'Failed to commission domain' },
        { status: status >= 400 ? status : 502 }
      )
    }

    const zone = (data.data as { zone?: CfZone })?.zone
    const nameservers =
      (data.data as { nameservers?: string[] })?.nameservers ?? zone?.name_servers ?? []

    const instructions = [
      `1. Log into your domain registrar (Porkbun, Namecheap, etc.)`,
      `2. Navigate to DNS settings for ${body.domain}`,
      `3. Update the nameservers to:`,
      ...nameservers.map((ns, i) => `   ${i + 1}. ${ns}`),
      `4. Wait 24-48 hours for propagation`,
      `5. Return to Dispatch to verify activation`,
    ]

    return NextResponse.json({
      success: true,
      data: {
        zone,
        nameservers,
        instructions,
      },
    })
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
