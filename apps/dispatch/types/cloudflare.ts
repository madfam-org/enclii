/**
 * Cloudflare API Types for Dispatch
 *
 * Type definitions for Cloudflare Zone and DNS management.
 */

// =============================================================================
// ZONE TYPES
// =============================================================================

export type ZoneStatus = 'active' | 'pending' | 'initializing' | 'moved' | 'deleted' | 'deactivated'

export interface CloudflareZone {
  id: string
  name: string
  status: ZoneStatus
  paused: boolean
  type: 'full' | 'partial' | 'secondary'
  development_mode: number
  name_servers: string[]
  original_name_servers: string[]
  original_registrar: string | null
  original_dnshost: string | null
  modified_on: string
  created_on: string
  activated_on: string | null
  meta: {
    step: number
    custom_certificate_quota: number
    page_rule_quota: number
    phishing_detected: boolean
    multiple_railguns_allowed: boolean
  }
  owner: {
    id: string
    type: string
    email: string
  }
  account: {
    id: string
    name: string
  }
  tenant: {
    id: string | null
    name: string | null
  }
  tenant_unit: {
    id: string | null
  }
  permissions: string[]
  plan: {
    id: string
    name: string
    price: number
    currency: string
    frequency: string
    is_subscribed: boolean
    can_subscribe: boolean
    legacy_id: string
    legacy_discount: boolean
    externally_managed: boolean
  }
}

// =============================================================================
// DNS RECORD TYPES
// =============================================================================

export type DNSRecordType = 'A' | 'AAAA' | 'CNAME' | 'TXT' | 'MX' | 'NS' | 'SRV' | 'CAA'

export interface CloudflareDNSRecord {
  id: string
  zone_id: string
  zone_name: string
  name: string
  type: DNSRecordType
  content: string
  proxiable: boolean
  proxied: boolean
  ttl: number
  locked: boolean
  meta: {
    auto_added: boolean
    managed_by_apps: boolean
    managed_by_argo_tunnel: boolean
    source: string
  }
  comment: string | null
  tags: string[]
  created_on: string
  modified_on: string
}

// =============================================================================
// TUNNEL TYPES
// =============================================================================

export type TunnelStatus = 'healthy' | 'degraded' | 'down' | 'inactive'

export interface CloudflareTunnel {
  id: string
  account_tag: string
  created_at: string
  deleted_at: string | null
  name: string
  connections: TunnelConnection[]
  conns_active_at: string | null
  conns_inactive_at: string | null
  tun_type: 'cfd_tunnel' | 'warp_connector'
  metadata: Record<string, unknown>
  status: TunnelStatus
  remote_config: boolean
}

export interface TunnelConnection {
  colo_name: string
  id: string
  is_pending_reconnect: boolean
  client_id: string
  client_version: string
  opened_at: string
  origin_ip: string
}

// =============================================================================
// API RESPONSE TYPES
// =============================================================================

export interface CloudflareAPIResponse<T> {
  success: boolean
  errors: CloudflareError[]
  messages: string[]
  result: T
  result_info?: {
    page: number
    per_page: number
    total_pages: number
    count: number
    total_count: number
  }
}

export interface CloudflareError {
  code: number
  message: string
}

// =============================================================================
// DISPATCH DOMAIN TYPES (Unified View)
// =============================================================================

export type EcosystemTenant = 'madfam' | 'janua' | 'enclii' | 'other'

export interface DispatchDomain {
  id: string
  domain: string
  tenant: EcosystemTenant
  status: ZoneStatus
  sslStatus: 'active' | 'pending' | 'inactive' | 'error'
  dnsStatus: 'healthy' | 'warning' | 'error'
  nameservers: string[]
  activatedAt: string | null
  createdAt: string
  tunnelId?: string
  tunnelName?: string
}

// =============================================================================
// COMMISSION FLOW TYPES
// =============================================================================

export interface CommissionDomainRequest {
  domain: string
  tenant: EcosystemTenant
  setupDns?: boolean
  enableProxy?: boolean
}

export interface CommissionDomainResponse {
  zone: CloudflareZone
  nameservers: string[]
  instructions: string[]
}

// =============================================================================
// ROUTING FLOW TYPES
// =============================================================================

export interface RouteSubdomainRequest {
  zoneId: string
  subdomain: string
  tunnelId: string
  proxied?: boolean
}

export interface RouteSubdomainResponse {
  record: CloudflareDNSRecord
  tunnelRoute: {
    hostname: string
    service: string
  }
}
