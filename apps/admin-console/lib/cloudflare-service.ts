/**
 * Cloudflare API Service for Dispatch
 *
 * Provides a type-safe wrapper around the Cloudflare API for:
 * - Zone management (list, create, delete)
 * - DNS record management
 * - Tunnel management
 *
 * SECURITY: This service should only be called from server-side code (API routes).
 * The API token is never exposed to the client.
 */

import type {
  CloudflareZone,
  CloudflareDNSRecord,
  CloudflareTunnel,
  CloudflareAPIResponse,
  CommissionDomainRequest,
  CommissionDomainResponse,
  RouteSubdomainRequest,
  RouteSubdomainResponse,
  DispatchDomain,
  EcosystemTenant,
} from '@/types/cloudflare'

const CLOUDFLARE_API_BASE = 'https://api.cloudflare.com/client/v4'

// Get credentials from environment (server-side only)
function getCredentials() {
  const apiToken = process.env.CLOUDFLARE_API_TOKEN
  const accountId = process.env.CLOUDFLARE_ACCOUNT_ID

  if (!apiToken) {
    throw new Error('CLOUDFLARE_API_TOKEN is not configured')
  }
  if (!accountId) {
    throw new Error('CLOUDFLARE_ACCOUNT_ID is not configured')
  }

  return { apiToken, accountId }
}

// Base fetch wrapper with authentication
async function cfFetch<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<CloudflareAPIResponse<T>> {
  const { apiToken } = getCredentials()

  const response = await fetch(`${CLOUDFLARE_API_BASE}${endpoint}`, {
    ...options,
    headers: {
      Authorization: `Bearer ${apiToken}`,
      'Content-Type': 'application/json',
      ...options.headers,
    },
  })

  const data = await response.json()

  if (!data.success) {
    const errorMessage = data.errors?.map((e: { message: string }) => e.message).join(', ') || 'Unknown error'
    throw new Error(`Cloudflare API Error: ${errorMessage}`)
  }

  return data as CloudflareAPIResponse<T>
}

// =============================================================================
// ZONE MANAGEMENT
// =============================================================================

/**
 * List all zones in the account
 */
export async function listZones(params?: {
  name?: string
  status?: string
  page?: number
  perPage?: number
}): Promise<CloudflareAPIResponse<CloudflareZone[]>> {
  const searchParams = new URLSearchParams()

  if (params?.name) searchParams.set('name', params.name)
  if (params?.status) searchParams.set('status', params.status)
  if (params?.page) searchParams.set('page', params.page.toString())
  if (params?.perPage) searchParams.set('per_page', params.perPage.toString())

  const query = searchParams.toString()
  return cfFetch<CloudflareZone[]>(`/zones${query ? `?${query}` : ''}`)
}

/**
 * Get a single zone by ID
 */
export async function getZone(zoneId: string): Promise<CloudflareZone> {
  const response = await cfFetch<CloudflareZone>(`/zones/${zoneId}`)
  return response.result
}

/**
 * Create a new zone (Commission a domain)
 */
export async function createZone(
  domain: string,
  options?: { jumpStart?: boolean; type?: 'full' | 'partial' }
): Promise<CloudflareZone> {
  const { accountId } = getCredentials()

  const response = await cfFetch<CloudflareZone>('/zones', {
    method: 'POST',
    body: JSON.stringify({
      name: domain,
      account: { id: accountId },
      jump_start: options?.jumpStart ?? true,
      type: options?.type ?? 'full',
    }),
  })

  return response.result
}

/**
 * Delete a zone
 */
export async function deleteZone(zoneId: string): Promise<{ id: string }> {
  const response = await cfFetch<{ id: string }>(`/zones/${zoneId}`, {
    method: 'DELETE',
  })
  return response.result
}

// =============================================================================
// DNS RECORD MANAGEMENT
// =============================================================================

/**
 * List DNS records for a zone
 */
export async function listDNSRecords(
  zoneId: string,
  params?: {
    type?: string
    name?: string
    content?: string
    page?: number
    perPage?: number
  }
): Promise<CloudflareAPIResponse<CloudflareDNSRecord[]>> {
  const searchParams = new URLSearchParams()

  if (params?.type) searchParams.set('type', params.type)
  if (params?.name) searchParams.set('name', params.name)
  if (params?.content) searchParams.set('content', params.content)
  if (params?.page) searchParams.set('page', params.page.toString())
  if (params?.perPage) searchParams.set('per_page', params.perPage.toString())

  const query = searchParams.toString()
  return cfFetch<CloudflareDNSRecord[]>(`/zones/${zoneId}/dns_records${query ? `?${query}` : ''}`)
}

/**
 * Create a DNS record
 */
export async function createDNSRecord(
  zoneId: string,
  record: {
    type: string
    name: string
    content: string
    ttl?: number
    proxied?: boolean
    comment?: string
  }
): Promise<CloudflareDNSRecord> {
  const response = await cfFetch<CloudflareDNSRecord>(`/zones/${zoneId}/dns_records`, {
    method: 'POST',
    body: JSON.stringify({
      ...record,
      ttl: record.ttl ?? 1, // 1 = automatic
      proxied: record.proxied ?? true,
    }),
  })
  return response.result
}

/**
 * Update a DNS record
 */
export async function updateDNSRecord(
  zoneId: string,
  recordId: string,
  record: {
    type?: string
    name?: string
    content?: string
    ttl?: number
    proxied?: boolean
    comment?: string
  }
): Promise<CloudflareDNSRecord> {
  const response = await cfFetch<CloudflareDNSRecord>(
    `/zones/${zoneId}/dns_records/${recordId}`,
    {
      method: 'PATCH',
      body: JSON.stringify(record),
    }
  )
  return response.result
}

/**
 * Delete a DNS record
 */
export async function deleteDNSRecord(
  zoneId: string,
  recordId: string
): Promise<{ id: string }> {
  const response = await cfFetch<{ id: string }>(
    `/zones/${zoneId}/dns_records/${recordId}`,
    { method: 'DELETE' }
  )
  return response.result
}

// =============================================================================
// TUNNEL MANAGEMENT
// =============================================================================

/**
 * List all tunnels in the account
 */
export async function listTunnels(): Promise<CloudflareAPIResponse<CloudflareTunnel[]>> {
  const { accountId } = getCredentials()
  return cfFetch<CloudflareTunnel[]>(`/accounts/${accountId}/cfd_tunnel`)
}

/**
 * Get tunnel details
 */
export async function getTunnel(tunnelId: string): Promise<CloudflareTunnel> {
  const { accountId } = getCredentials()
  const response = await cfFetch<CloudflareTunnel>(
    `/accounts/${accountId}/cfd_tunnel/${tunnelId}`
  )
  return response.result
}

// =============================================================================
// DISPATCH UNIFIED OPERATIONS
// =============================================================================

/**
 * Extract tenant from domain name
 */
function extractTenant(domain: string): EcosystemTenant {
  const tenantMap: Record<string, EcosystemTenant> = {
    'madfam.io': 'madfam',
    'madfam.dev': 'madfam',
    'suluna.mx': 'suluna',
    'suluna.app': 'suluna',
    'primavera.mx': 'primavera',
    'janua.dev': 'janua',
    'enclii.dev': 'enclii',
  }

  for (const [suffix, tenant] of Object.entries(tenantMap)) {
    if (domain.endsWith(suffix)) {
      return tenant
    }
  }
  return 'other'
}

/**
 * Get unified domain list for Dispatch Domain Matrix
 */
export async function getDispatchDomains(): Promise<DispatchDomain[]> {
  const zonesResponse = await listZones({ perPage: 100 })
  const zones = zonesResponse.result

  // Get tunnel list for mapping
  let tunnels: CloudflareTunnel[] = []
  try {
    const tunnelsResponse = await listTunnels()
    tunnels = tunnelsResponse.result
  } catch {
    console.warn('Could not fetch tunnels')
  }

  return zones.map((zone) => {
    // Check for associated tunnel via DNS records (CNAME to tunnel)
    const tunnelMatch = tunnels.find((t) =>
      zone.name_servers.some((ns) => ns.includes(t.name))
    )

    return {
      id: zone.id,
      domain: zone.name,
      tenant: extractTenant(zone.name),
      status: zone.status,
      sslStatus: zone.status === 'active' ? 'active' : 'pending',
      dnsStatus: zone.status === 'active' ? 'healthy' : 'warning',
      nameservers: zone.name_servers,
      activatedAt: zone.activated_on,
      createdAt: zone.created_on,
      tunnelId: tunnelMatch?.id,
      tunnelName: tunnelMatch?.name,
    }
  })
}

/**
 * Commission a new domain (Sovereign Registrar flow)
 */
export async function commissionDomain(
  request: CommissionDomainRequest
): Promise<CommissionDomainResponse> {
  // Create the zone
  const zone = await createZone(request.domain, { jumpStart: true })

  // Generate instructions for the user
  const instructions = [
    `1. Log into your domain registrar (Porkbun, Namecheap, etc.)`,
    `2. Navigate to DNS settings for ${request.domain}`,
    `3. Update the nameservers to:`,
    ...zone.name_servers.map((ns, i) => `   ${i + 1}. ${ns}`),
    `4. Wait 24-48 hours for propagation`,
    `5. Return to Dispatch to verify activation`,
  ]

  return {
    zone,
    nameservers: zone.name_servers,
    instructions,
  }
}

/**
 * Route a subdomain to a tunnel (Routing flow)
 */
export async function routeSubdomain(
  request: RouteSubdomainRequest
): Promise<RouteSubdomainResponse> {
  const { accountId } = getCredentials()
  const zone = await getZone(request.zoneId)
  const tunnel = await getTunnel(request.tunnelId)

  // Create CNAME record pointing to tunnel
  const fullHostname = `${request.subdomain}.${zone.name}`
  const tunnelCname = `${tunnel.id}.cfargotunnel.com`

  const record = await createDNSRecord(request.zoneId, {
    type: 'CNAME',
    name: request.subdomain,
    content: tunnelCname,
    proxied: request.proxied ?? true,
    comment: `Routed via Dispatch to tunnel: ${tunnel.name}`,
  })

  return {
    record,
    tunnelRoute: {
      hostname: fullHostname,
      service: `http://localhost:${request.subdomain.includes('api') ? '8080' : '3000'}`,
    },
  }
}

/**
 * Check SSL certificate status for a zone
 */
export async function getSSLStatus(zoneId: string): Promise<{
  status: 'active' | 'pending' | 'inactive' | 'error'
  issuer?: string
  expiresOn?: string
}> {
  try {
    const response = await cfFetch<{
      certificate_status: string
      issuer?: string
      expires_on?: string
    }>(`/zones/${zoneId}/ssl/certificate_packs`)

    const cert = response.result
    return {
      status: cert.certificate_status === 'active' ? 'active' : 'pending',
      issuer: cert.issuer,
      expiresOn: cert.expires_on,
    }
  } catch {
    return { status: 'inactive' }
  }
}
