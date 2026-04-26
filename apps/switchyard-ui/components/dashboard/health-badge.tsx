'use client';

import { useCallback, useEffect, useState } from 'react';
import { Activity } from 'lucide-react';
import { cn } from '@/lib/utils';
import { apiGet } from '@/lib/api';
import { POLLING_IDLE } from '@/lib/constants';
import { usePolling } from '@/hooks/use-polling';
import type { ServiceHealthResponse } from '@/app/(protected)/observability/observability-types';

interface HealthBadgeProps {
  /** Service ID — filters the all-services response client-side. */
  serviceId?: string;
  /** Friendly name used in tooltip / aria-label. */
  serviceName?: string;
  className?: string;
}

interface HealthSummary {
  errorRate: number;
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
}

function tone(rate: number, status: HealthSummary['status']) {
  // Status from K8s/deployment trumps error-rate heuristic.
  if (status === 'unhealthy') {
    return {
      bar: 'bg-status-error',
      text: 'text-status-error',
      ring: 'ring-status-error/30',
      label: 'unhealthy',
    } as const;
  }
  if (status === 'degraded' || rate >= 0.05) {
    return {
      bar: 'bg-status-warning',
      text: 'text-status-warning',
      ring: 'ring-status-warning/30',
      label: 'degraded',
    } as const;
  }
  if (status === 'unknown') {
    return {
      bar: 'bg-muted-foreground',
      text: 'text-muted-foreground',
      ring: 'ring-muted-foreground/30',
      label: 'unknown',
    } as const;
  }
  return {
    bar: 'bg-status-success',
    text: 'text-status-success',
    ring: 'ring-status-success/30',
    label: 'healthy',
  } as const;
}

function approxErrorsLabel(errorRate: number): string {
  // The API returns a fractional rate (0..1). We don't have the
  // absolute count over 24h, so approximate buckets — clearer than
  // exposing a raw float.
  if (errorRate <= 0) return '0 errors / 24h';
  if (errorRate < 0.01) return '<1% errors / 24h';
  if (errorRate < 0.05) return `${(errorRate * 100).toFixed(1)}% errors / 24h`;
  if (errorRate < 0.5) return `${Math.round(errorRate * 100)}% errors / 24h`;
  return '50%+ errors / 24h';
}

/**
 * Compact per-service health indicator.
 *
 * Hits the ecosystem rollup `/v1/observability/health` and filters by
 * `serviceId` client-side. Polls at POLLING_IDLE (60s) so multiple
 * cards on the dashboard don't hammer the API.
 *
 * Accessibility:
 *  - Wrapped span has aria-label with the explicit health summary
 *    (e.g. "switchyard-api health: healthy, 0 errors / 24h").
 *  - Visible bar is aria-hidden; the text label is the announced value.
 */
export function HealthBadge({
  serviceId,
  serviceName,
  className,
}: HealthBadgeProps) {
  const [summary, setSummary] = useState<HealthSummary | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchHealth = useCallback(async () => {
    if (!serviceId) return;
    try {
      const data = await apiGet<ServiceHealthResponse>(
        '/v1/observability/health',
      );
      const svc = data.services?.find((s) => s.service_id === serviceId);
      if (svc) {
        const status: HealthSummary['status'] = (
          ['healthy', 'degraded', 'unhealthy'] as const
        ).includes(svc.status as HealthSummary['status'])
          ? (svc.status as HealthSummary['status'])
          : 'unknown';
        setSummary({ errorRate: svc.error_rate || 0, status });
      } else {
        setSummary({ errorRate: 0, status: 'unknown' });
      }
    } catch {
      setSummary({ errorRate: 0, status: 'unknown' });
    } finally {
      setLoading(false);
    }
  }, [serviceId]);

  useEffect(() => {
    fetchHealth();
  }, [fetchHealth]);

  usePolling(fetchHealth, POLLING_IDLE, { enabled: !!serviceId });

  if (loading || !summary) {
    return (
      <span
        className={cn(
          'inline-flex items-center gap-1 text-[10px] text-muted-foreground',
          className,
        )}
        aria-label="Loading health"
      >
        <Activity aria-hidden="true" className="h-3 w-3 animate-pulse" />
      </span>
    );
  }

  const t = tone(summary.errorRate, summary.status);
  const text = approxErrorsLabel(summary.errorRate);

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ring-1',
        t.text,
        t.ring,
        'bg-background',
        className,
      )}
      aria-label={`${serviceName || 'Service'} health: ${t.label}, ${text}`}
      title={`${t.label} — ${text}`}
      data-testid="health-badge"
    >
      <span
        aria-hidden="true"
        className={cn('h-1.5 w-1.5 rounded-full', t.bar)}
      />
      <span>{text}</span>
    </span>
  );
}
