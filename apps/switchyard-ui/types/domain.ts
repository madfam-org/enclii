/**
 * Types for the Domains page (parity audit gap #4 — per-domain visibility).
 *
 * Mirrors the `DomainWithContext` shape from
 * `apps/switchyard-api/internal/api/global_domains_handlers.go`.
 *
 * Backend lives at GET /v1/domains (paginated, filterable) and
 * GET /v1/domains/stats. Cert-expiry / cloudflare-tunnel-route fields are
 * surfaced when the domain_sync service has populated them; missing values
 * render as "Unknown" so the UI stays graceful even when the sync hasn't
 * caught up yet.
 */

export type DomainSyncStatus =
  | 'pending'
  | 'verifying'
  | 'active'
  | 'error'
  | string; // future-proof: backend may add new statuses

/** UI-visible health bucket derived from server-side fields. */
export type DomainHealthStatus =
  | 'active'
  | 'provisioning'
  | 'failed'
  | 'orphaned'
  | 'unknown';

/** UI-visible Cloudflare tunnel route bucket. */
export type DomainTunnelMode = 'tunneled' | 'direct' | 'unknown';

export interface Domain {
  id: string;
  service_id: string;
  environment_id: string;
  domain: string;
  verified: boolean;
  tls_enabled: boolean;
  tls_issuer?: string;
  tls_provider?: string;
  status: DomainSyncStatus;
  is_platform_domain: boolean;
  zero_trust_enabled: boolean;
  cloudflare_tunnel_id?: string | null;
  dns_cname?: string;
  created_at: string;
  updated_at: string;
  verified_at?: string | null;

  // Enrichment fields (joined from services / projects / environments).
  service_name?: string;
  environment_name?: string;
  project_slug?: string;

  // Optional fields the backend may surface as the domain_sync service grows.
  // The page treats missing values as "Unknown" so it doesn't crash before
  // those columns are wired all the way through.
  tls_expires_at?: string | null;
  last_verified_at?: string | null;
  service_id_label?: string; // human-friendly fallback when service join fails
}

export interface DomainsListResponse {
  domains: Domain[];
  total: number;
  limit: number;
  offset: number;
}

export interface DomainStats {
  total_domains: number;
  verified_domains: number;
  pending_domains: number;
  tls_enabled: number;
  platform_domains: number;
  custom_domains: number;
}
