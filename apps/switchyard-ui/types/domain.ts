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

export type PublicDNSStatus = 'resolved' | 'missing' | 'error' | 'unknown' | string;
export type PublicTLSStatus = 'valid' | 'invalid' | 'skipped' | 'unknown' | string;

export interface DomainPublicEvidence {
  source: string;
  checked_at: string;
  public_dns_status: PublicDNSStatus;
  public_tls_status: PublicTLSStatus;
  public_http_status?: number;
  public_http_reachable: boolean;
  error?: string;
}

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
  /**
   * Optional public DNS/TLS/HTTP evidence from the API. This is independent of
   * persisted DB verifier fields and exists specifically to expose drift, e.g.
   * a domain that is publicly reachable over valid HTTPS while custom_domains
   * still says verified=false / tls_enabled=false.
   */
  evidence?: DomainPublicEvidence | null;
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

export interface DomainReconcileSummary {
  db_domains: number;
  routed_domains: number;
  matched: number;
  db_only: number;
  route_only: number;
  actionable_route_only?: number;
  excluded_route_only?: number;
  drift_detected: boolean;
  inventory_closed: boolean;
}

export interface DomainReconcileItem {
  domain: string;
  db_present: boolean;
  route_present: boolean;
  sources?: string[];
  route_targets?: string[];
  service_id?: string;
  environment_id?: string;
  service_name?: string;
  environment_name?: string;
  project_slug?: string;
  verified?: boolean;
  tls_enabled?: boolean;
  classification?: string;
  excluded?: boolean;
  exclusion_reason?: string;
  actionable?: boolean;
}

export interface DomainReconcileResponse {
  generated_at: string;
  dry_run: boolean;
  sources: string[];
  warnings?: string[];
  summary: DomainReconcileSummary;
  matched: DomainReconcileItem[];
  db_only: DomainReconcileItem[];
  route_only: DomainReconcileItem[];
  actionable_route_only?: DomainReconcileItem[];
  excluded_route_only?: DomainReconcileItem[];
}

export interface DomainInventoryExclusion {
  id?: string;
  hostname_pattern: string;
  source: string;
  route_target: string;
  classification: string;
  reason: string;
  active: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface DomainInventoryExclusionsResponse {
  generated_at: string;
  warnings?: string[];
  exclusions: DomainInventoryExclusion[];
}

export interface DomainStats {
  total_domains: number;
  verified_domains: number;
  pending_domains: number;
  tls_enabled: number;
  platform_domains: number;
  custom_domains: number;
}
