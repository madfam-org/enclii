'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { Activity, AlertTriangle, CheckCircle2, XCircle } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { cn } from '@/lib/utils';
import { apiGet } from '@/lib/api';
import { POLLING_IDLE } from '@/lib/constants';
import { usePolling } from '@/hooks/use-polling';
import type { ServiceHealthResponse } from '@/app/(protected)/observability/observability-types';

interface SystemHealthSummaryProps {
  className?: string;
}

interface CountRow {
  label: string;
  value: number;
  toneClass: string;
  icon: React.ReactNode;
}

/**
 * Hard upper-bound on how long the System Health card can sit in the
 * "Loading…" state before we give up and show a "System unhealthy"
 * surface. The /v1/observability/health endpoint has a 25s server budget
 * and apiGet has a 35s fetch timeout — so under any normal failure mode
 * the await resolves or rejects inside that window. This guard exists for
 * the genuinely-pathological case where the fetch is somehow neither
 * resolving nor rejecting (e.g., a service-worker swallowing the request,
 * or a long-lived TCP RST never delivered to the JS runtime).
 *
 * 40s = max(35s apiGet timeout, 25s server budget) + 5s headroom.
 *
 * Truthfulness audit (2026-05-04): the dashboard widget was observed
 * stuck on "Loading…" indefinitely. Whatever the real cause, an
 * indefinite spinner is the worst possible state — operators read it as
 * "the page is alive but the API is gone", which is unactionable. The
 * guard converts unbounded waits into a definite "Health check timed out"
 * error state.
 */
export const SYSTEM_HEALTH_LOAD_TIMEOUT_MS = 40_000;

/**
 * Pure helper: format the timeout-triggered error message.
 *
 * Extracted so the truthfulness contract ("Loading… must always resolve
 * within bounded time, even on pathological no-resolve fetches") can be
 * exercised by jest unit tests without pulling in a React renderer. The
 * test is the source of truth for the error text the operator sees.
 */
export function systemHealthTimeoutMessage(timeoutMs: number): string {
  const seconds = Math.round(timeoutMs / 1000);
  return `Health check timed out after ${seconds}s — system may be unhealthy`;
}

/**
 * Pure helper: choose the visible widget state given the current data
 * shape. Encodes the truthfulness contract:
 *
 *   - error set        → 'error'   (always wins; operator-actionable)
 *   - data null + load → 'loading' (initial fetch in flight)
 *   - data set         → 'data'    (steady state — render counts)
 *   - data null !load  → 'empty'   (first fetch finished but produced no data)
 *
 * Centralised here so the JSX branches in render() match the tests and
 * a future refactor of the visual states stays consistent.
 */
export type SystemHealthRenderState = 'error' | 'loading' | 'data' | 'empty';

export function systemHealthRenderState(args: {
  error: string | null;
  loading: boolean;
  hasData: boolean;
}): SystemHealthRenderState {
  if (args.error) return 'error';
  if (args.loading) return 'loading';
  if (args.hasData) return 'data';
  return 'empty';
}

/**
 * Dashboard sidebar widget summarising ecosystem health.
 *
 * Uses the ecosystem rollup `/v1/observability/health` (no service
 * filter). Shows total / healthy / degraded / unhealthy counts.
 *
 * The API doesn't currently expose a time-series for service health,
 * so we render a current snapshot only — no sparkline. (Per the audit
 * prompt: "if not, just current snapshot".)
 *
 * Accessibility:
 *  - Each count row is a <dl> entry so screen readers can read pairs.
 *  - Aggregate status announced via aria-live on the loading state.
 */
export function SystemHealthSummary({ className }: SystemHealthSummaryProps) {
  const [data, setData] = useState<ServiceHealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Track whether we've ever observed a successful resolution. Used by the
  // failsafe timeout below — once we've had at least one resolved fetch we
  // don't need the timeout to fire (loading=false already).
  const hasResolvedRef = useRef(false);

  const fetchHealth = useCallback(async () => {
    try {
      setError(null);
      const res = await apiGet<ServiceHealthResponse>(
        '/v1/observability/health',
      );
      setData(res);
      hasResolvedRef.current = true;
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load health');
      hasResolvedRef.current = true;
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchHealth();
  }, [fetchHealth]);

  // Failsafe: if neither resolution nor rejection happens inside the
  // bounded window, surface a definite error rather than the
  // indistinguishable-from-broken "Loading…" state. We tear this down on
  // unmount AND on first successful resolution so a healthy poll cycle
  // doesn't trip the timer on a slow first load.
  useEffect(() => {
    const id = setTimeout(() => {
      if (!hasResolvedRef.current) {
        setLoading(false);
        setError(systemHealthTimeoutMessage(SYSTEM_HEALTH_LOAD_TIMEOUT_MS));
      }
    }, SYSTEM_HEALTH_LOAD_TIMEOUT_MS);
    return () => clearTimeout(id);
  }, []);

  usePolling(fetchHealth, POLLING_IDLE);

  const total = data?.services?.length || 0;
  const healthy = data?.healthy_count || 0;
  const degraded = data?.degraded_count || 0;
  const unhealthy = data?.unhealthy_count || 0;

  const rows: CountRow[] = [
    {
      label: 'Healthy',
      value: healthy,
      toneClass: 'text-status-success',
      icon: (
        <CheckCircle2
          aria-hidden="true"
          className="h-3.5 w-3.5 text-status-success"
        />
      ),
    },
    {
      label: 'Degraded',
      value: degraded,
      toneClass: 'text-status-warning',
      icon: (
        <AlertTriangle
          aria-hidden="true"
          className="h-3.5 w-3.5 text-status-warning"
        />
      ),
    },
    {
      label: 'Failed',
      value: unhealthy,
      toneClass: 'text-status-error',
      icon: (
        <XCircle
          aria-hidden="true"
          className="h-3.5 w-3.5 text-status-error"
        />
      ),
    },
  ];

  return (
    <Card className={cn('p-4', className)} data-testid="system-health-summary">
      <div className="mb-2 flex items-center gap-2">
        <Activity
          aria-hidden="true"
          className="h-4 w-4 text-muted-foreground"
        />
        <h3 className="text-sm font-medium">System Health</h3>
      </div>

      {error ? (
        <p className="text-xs text-status-error">{error}</p>
      ) : loading ? (
        <p
          aria-live="polite"
          className="text-xs text-muted-foreground"
        >
          Loading…
        </p>
      ) : (
        <>
          <p className="text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{total}</span>{' '}
            service{total === 1 ? '' : 's'} tracked
          </p>
          <dl className="mt-3 space-y-1.5">
            {rows.map((row) => (
              <div
                key={row.label}
                className="flex items-center justify-between gap-2"
              >
                <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  {row.icon}
                  {row.label}
                </dt>
                <dd
                  className={cn(
                    'tabular-nums text-sm font-semibold',
                    row.toneClass,
                  )}
                >
                  {row.value}
                </dd>
              </div>
            ))}
          </dl>
        </>
      )}
    </Card>
  );
}
