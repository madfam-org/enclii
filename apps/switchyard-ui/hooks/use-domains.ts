'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiGet } from '@/lib/api';
import { POLLING_SLOW } from '@/lib/constants';
import { usePolling } from '@/hooks/use-polling';
import type {
  Domain,
  DomainCoverage,
  DomainInventoryExclusionsResponse,
  DomainInventoryExclusion,
  DomainReconcileResponse,
  DomainsListResponse,
  DomainStats,
} from '@/types/domain';

interface UseDomainsResult {
  domains: Domain[];
  stats: DomainStats | null;
  reconcile: DomainReconcileResponse | null;
  exclusions: DomainInventoryExclusion[] | null;
  /**
   * Coverage metadata returned by /v1/domains, or null when the backend
   * predates the field (older API builds). The page uses this to render
   * the "partial inventory" / "verifier stale" banners.
   */
  coverage: DomainCoverage | null;
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  /** True when the backend endpoint returned 404 (not yet deployed). */
  endpointMissing: boolean;
  /**
   * Timestamp of the last successful FETCH from /v1/domains. This is NOT
   * the timestamp of the last verifier run — see `coverage` for that.
   * The page deliberately surfaces both so operators can see when the API
   * was last reached vs. when domains were last actually verified against
   * Cloudflare.
   */
  lastSyncedAt: string | null;
  refresh: () => Promise<void>;
}

/**
 * Fetches all custom domains across the ecosystem and polls every
 * POLLING_SLOW (30s) for updates. Pauses when the tab is hidden, via
 * `usePolling`'s Page Visibility integration.
 *
 * Why 30s: domains change on the order of minutes/hours (DNS propagation,
 * cert renewal). POLLING_SLOW matches the project-card freshness budget so
 * the "last synced" badge stays meaningful, without hammering the API.
 *
 * Endpoint shape mirrors `apps/switchyard-api/internal/api/global_domains_handlers.go`.
 * If the endpoint 404s the hook surfaces `endpointMissing=true` so the page
 * can render a "backend pending" placeholder rather than an error toast.
 */
export function useDomains(limit = 200): UseDomainsResult {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [stats, setStats] = useState<DomainStats | null>(null);
  const [reconcile, setReconcile] = useState<DomainReconcileResponse | null>(null);
  const [exclusions, setExclusions] = useState<DomainInventoryExclusion[] | null>(null);
  const [coverage, setCoverage] = useState<DomainCoverage | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [endpointMissing, setEndpointMissing] = useState(false);
  const [lastSyncedAt, setLastSyncedAt] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    setRefreshing(true);
    try {
      // Pull a generous page so the client-side filter/sort works without
      // round-tripping. The backend accepts up to 500; 200 covers the
      // current ecosystem several times over without hiding rows behind the
      // previous API cap fallback.
      const params = new URLSearchParams({
        limit: limit.toString(),
        offset: '0',
      });

      const [list, statsResp, reconcileResp, exclusionsResp] = await Promise.all([
        apiGet<DomainsListResponse>(`/v1/domains?${params.toString()}`),
        apiGet<DomainStats>('/v1/domains/stats').catch((e) => {
          // Stats endpoint is auxiliary; don't fail the whole hook over it.
          console.warn('Failed to fetch domain stats:', e);
          return null;
        }),
        apiGet<DomainReconcileResponse>('/v1/domains/reconcile').catch((e) => {
          // Admin-only auxiliary endpoint; non-admin users should still get
          // the domain table, just without route-drift metadata.
          console.warn('Failed to fetch domain reconciliation:', e);
          return null;
        }),
        apiGet<DomainInventoryExclusionsResponse>('/v1/domains/exclusions').catch((e) => {
          // Admin-only auxiliary endpoint; non-admin users should still get
          // the domain table and route drift warning without exclusion registry
          // details.
          console.warn('Failed to fetch domain inventory exclusions:', e);
          return null;
        }),
      ]);

      setDomains(list?.domains ?? []);
      setStats(statsResp);
      setReconcile(reconcileResp);
      setExclusions(exclusionsResp?.exclusions ?? null);
      // Coverage is optional — older API builds won't ship it. Keep the
      // null sentinel so the UI suppresses banners rather than showing
      // misleading defaults like "0 of 0 projects covered".
      setCoverage(list?.coverage ?? null);
      setError(null);
      setEndpointMissing(false);
      setLastSyncedAt(new Date().toISOString());
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to load domains';
      // Heuristic: treat "404" or "Not Found" as endpoint-missing so the page
      // can render the placeholder instead of an error banner.
      if (/404|not found/i.test(message)) {
        setEndpointMissing(true);
        setError(null);
         
        console.warn(
          '[useDomains] /v1/domains endpoint not yet available — rendering placeholder',
        );
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [limit]);

  // Initial load
  useEffect(() => {
    void fetchAll();
  }, [fetchAll]);

  // Background polling — pauses on hidden tab
  usePolling(fetchAll, POLLING_SLOW, { enabled: !endpointMissing });

  const refresh = useCallback(async () => {
    await fetchAll();
  }, [fetchAll]);

  return {
    domains,
    stats,
    reconcile,
    exclusions,
    coverage,
    loading,
    refreshing,
    error,
    endpointMissing,
    lastSyncedAt,
    refresh,
  };
}
