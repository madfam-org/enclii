'use client';

import { useCallback, useEffect, useState } from 'react';
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

  const fetchHealth = useCallback(async () => {
    try {
      setError(null);
      const res = await apiGet<ServiceHealthResponse>(
        '/v1/observability/health',
      );
      setData(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load health');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchHealth();
  }, [fetchHealth]);

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
