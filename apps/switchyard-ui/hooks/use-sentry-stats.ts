'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiGet } from '@/lib/api';
import { POLLING_IDLE } from '@/lib/constants';
import { usePolling } from './use-polling';

/**
 * Shape of the GET /v1/observability/sentry response.
 *
 * The backend uses 200 OK for the unconfigured case (env vars missing),
 * so we can natively handle the `configured: false` payload without
 * throwing and catching errors, preventing 503 console spam.
 *
 * `error_count` is null in two cases:
 *   1. configured=true + reason=no_sentry_project (slug not in the org)
 *   2. configured=false (UI must hide the badge entirely)
 */
export interface SentryStats {
  configured: boolean;
  reason?: 'sentry_unconfigured' | 'no_sentry_project' | string;
  service_id?: string;
  sentry_project_slug?: string;
  stats_period?: string;
  error_count: number | null;
  issue_count?: number | null;
  org_slug?: string;
  fetched_at?: string;
}

export interface UseSentryStatsResult {
  data: SentryStats | null;
  loading: boolean;
  /** True when the backend explicitly told us Sentry isn't configured. */
  hidden: boolean;
}

/**
 * Polls /v1/observability/sentry every 60s for a single service.
 *
 * The hook handles the "operator hasn't dropped the token in yet" state
 * seamlessly since the backend returns a 200 OK with configured=false.
 * Returning hidden=true lets the consumer render nothing (the parity-audit ask)
 * instead of an error chip.
 *
 * Other failure modes (502 from upstream Sentry, network error) just leave
 * the previous data in place — the badge will show stale data rather than
 * vanishing, which matches the pattern in HealthBadge.
 */
export function useSentryStats(serviceId: string | undefined): UseSentryStatsResult {
  const [data, setData] = useState<SentryStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [hidden, setHidden] = useState(false);

  const fetchStats = useCallback(async () => {
    if (!serviceId) return;
    try {
      const stats = await apiGet<SentryStats>(
        `/v1/observability/sentry?service=${encodeURIComponent(serviceId)}`,
      );
      // 200 path — including configured=true + no_sentry_project.
      if (!stats.configured) {
        setHidden(true);
      } else {
        setHidden(false);
      }
      setData(stats);
    } catch (err: unknown) {
      // Non-200 errors (502 upstream, network) — leave previous data in
      // place and don't toggle hidden. Caller can use loading=false +
      // data=null to detect first-load failures if needed.
    } finally {
      setLoading(false);
    }
  }, [serviceId]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  usePolling(fetchStats, POLLING_IDLE, { enabled: !!serviceId });

  return { data, loading, hidden };
}
