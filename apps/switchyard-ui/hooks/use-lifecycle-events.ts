'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiGet } from '@/lib/api';
import { POLLING_SLOW } from '@/lib/constants';
import { usePolling } from '@/hooks/use-polling';
import type {
  LifecycleEvent,
  LifecycleSinceOption,
  LifecycleTimelineResponse,
} from '@/types/lifecycle';

export interface UseLifecycleEventsOptions {
  /** GitHub `owner/repo` — required to build the API path. */
  repoFullName?: string;
  /** Comma-separated event_type filter, or empty for all. */
  eventTypes?: string[];
  /** "24h" / "7d" / "30d" / "all". Translated into ?since=ISO. */
  since?: LifecycleSinceOption;
  /** Hard cap on rows. Defaults to 50 (matches the audit recommendation). */
  limit?: number;
  /** Polling interval in ms. Defaults to POLLING_SLOW (30s). */
  pollMs?: number;
  /** When false the hook fetches once but doesn't poll. */
  enabled?: boolean;
}

export interface UseLifecycleEventsResult {
  events: LifecycleEvent[];
  loading: boolean;
  error: string | null;
  /** ISO timestamp of the last successful fetch, or null. */
  lastSyncedAt: string | null;
  /** Trigger an immediate refetch (bypasses polling cadence). */
  refresh: () => Promise<void>;
}

function sinceToISO(since: LifecycleSinceOption | undefined): string | null {
  if (!since || since === 'all') return null;
  const ms: Record<Exclude<LifecycleSinceOption, 'all'>, number> = {
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    '30d': 30 * 24 * 60 * 60 * 1000,
  };
  return new Date(Date.now() - ms[since]).toISOString();
}

/**
 * Fetch lifecycle events for a repo with auto-polling.
 *
 * Reuses the existing apiGet client (auth, CSRF, refresh) and the
 * generic usePolling hook so we don't introduce a new fetch lib or
 * polling pattern.
 */
export function useLifecycleEvents({
  repoFullName,
  eventTypes,
  since,
  limit = 50,
  pollMs = POLLING_SLOW,
  enabled = true,
}: UseLifecycleEventsOptions): UseLifecycleEventsResult {
  const [events, setEvents] = useState<LifecycleEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastSyncedAt, setLastSyncedAt] = useState<string | null>(null);

  const eventTypeKey = (eventTypes ?? []).slice().sort().join(',');

  const fetchEvents = useCallback(async () => {
    if (!repoFullName) {
      setEvents([]);
      setLoading(false);
      return;
    }
    try {
      setError(null);
      const params = new URLSearchParams();
      if (limit) params.append('limit', String(limit));
      if (eventTypeKey) params.append('event_type', eventTypeKey);
      const sinceISO = sinceToISO(since);
      if (sinceISO) params.append('since', sinceISO);

      const path =
        `/v1/lifecycle/timeline/${encodeURIComponent(
          repoFullName.split('/')[0] || '',
        )}/${encodeURIComponent(repoFullName.split('/')[1] || '')}` +
        (params.toString() ? `?${params.toString()}` : '');

      const data = await apiGet<LifecycleTimelineResponse>(path);
      setEvents(data.events || []);
      setLastSyncedAt(new Date().toISOString());
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load events';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [repoFullName, eventTypeKey, since, limit]);

  useEffect(() => {
    fetchEvents();
  }, [fetchEvents]);

  usePolling(fetchEvents, pollMs, { enabled: enabled && !!repoFullName });

  return { events, loading, error, lastSyncedAt, refresh: fetchEvents };
}
