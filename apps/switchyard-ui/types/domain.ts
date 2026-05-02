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

/**
 * DomainCoverage — best-effort metadata about how complete the domain
 * inventory is and how fresh the verifier is. Mirrors the Go
 * `DomainCoverage` struct in `global_domains_handlers.go`.
 *
 * The page uses this to:
 *  - Show a "partial inventory" banner when projects_with_domains <
 *    projects_total (operators need to run `enclii domains add`).
 *  - Show a "verification stale" banner and badge rows as "Stale" when
 *    oldest_unverified_age_seconds > 24h.
 *  - Show a "Cloudflare integration not configured" banner when
 *    sync_configured is false (rows will never auto-verify).
 */
export interface DomainCoverage {
  sync_configured: boolean;
  projects_total: number;
  projects_with_domains: number;
  domains_total: number;
  /** -1 sentinel = no unverified rows, otherwise wall-clock age in seconds. */
  oldest_unverified_age_seconds: number;
}

export interface DomainsListResponse {
  domains: Domain[];
  total: number;
  limit: number;
  offset: number;
  /**
   * Optional for backwards compatibility — older API builds (pre-coverage)
   * won't return this field. The hook treats absence as "unknown" and the
   * UI suppresses the coverage banner in that case.
   */
  coverage?: DomainCoverage;
}

export interface DomainStats {
  total_domains: number;
  verified_domains: number;
  pending_domains: number;
  tls_enabled: number;
  platform_domains: number;
  custom_domains: number;
}
