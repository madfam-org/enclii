'use client';

import { ExternalLink, Bug } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useSentryStats, type SentryStats } from '@/hooks/use-sentry-stats';

interface SentryErrorBadgeProps {
  /** Service UUID — required; the badge calls /v1/observability/sentry?service=<id>. */
  serviceId?: string;
  /** Friendly name used in tooltip + aria-label. */
  serviceName?: string;
  className?: string;
}

/**
 * Compact error-rate indicator backed by the Sentry observability proxy.
 *
 * Visibility contract (parity audit gap #9):
 *   - configured=false (env vars missing) → render nothing.
 *   - configured=true + no_sentry_project → render a neutral chip ("no
 *     Sentry project") so operators know to set sentry_project_slug.
 *   - configured=true + count<10           → green chip
 *   - configured=true + 10<=count<100      → amber chip
 *   - configured=true + count>=100         → red chip
 *
 * Polls at POLLING_IDLE (60s) — the same cadence as HealthBadge — so a
 * dashboard with N project cards issues N requests/minute, which the API
 * layer absorbs via its own 60s in-memory cache.
 */
export function SentryErrorBadge({
  serviceId,
  serviceName,
  className,
}: SentryErrorBadgeProps) {
  const { data, loading, hidden } = useSentryStats(serviceId);

  // Hide the badge entirely when:
  //   - operator hasn't provisioned the token (hidden=true), OR
  //   - serviceId not available (parent didn't pass one yet).
  if (hidden || !serviceId) {
    return null;
  }

  if (loading) {
    return (
      <span
        className={cn(
          'inline-flex items-center gap-1 text-[10px] text-muted-foreground',
          className,
        )}
        aria-label="Loading Sentry stats"
      >
        <Bug aria-hidden="true" className="h-3 w-3 animate-pulse" />
      </span>
    );
  }

  if (!data || !data.configured) {
    // Defence in depth — should already be covered by hidden=true above.
    return null;
  }

  const tone = sentryTone(data);
  const label = sentryLabel(data);
  const sentryUrl = data.sentry_project_slug && data.org_slug
    ? `https://${data.org_slug}.sentry.io/projects/${data.sentry_project_slug}/`
    : undefined;

  const tooltip = sentryTooltip(data, serviceName);

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ring-1',
        tone.text,
        tone.ring,
        'bg-background',
        className,
      )}
      aria-label={`${serviceName || 'Service'} Sentry: ${tooltip}`}
      title={tooltip}
      data-testid="sentry-error-badge"
    >
      <span aria-hidden="true" className={cn('h-1.5 w-1.5 rounded-full', tone.dot)} />
      <span>{label}</span>
      {sentryUrl && (
        <a
          href={sentryUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="hover:text-foreground transition-colors"
          onClick={(e) => e.stopPropagation()}
          aria-label={`Open ${data.sentry_project_slug} in Sentry`}
        >
          <ExternalLink aria-hidden="true" className="h-2.5 w-2.5" />
        </a>
      )}
    </span>
  );
}

/**
 * Pure helper — chooses Tailwind tone classes based on the response shape.
 *
 * Exported for unit tests so we don't need a DOM renderer. The thresholds
 * (10, 100) match the parity-audit spec; tweaking them only requires a
 * change here + the corresponding test.
 */
export function sentryTone(stats: SentryStats): {
  text: string;
  ring: string;
  dot: string;
} {
  // No-project case: neutral muted styling.
  if (stats.reason === 'no_sentry_project' || stats.error_count === null) {
    return {
      text: 'text-muted-foreground',
      ring: 'ring-muted-foreground/30',
      dot: 'bg-muted-foreground',
    };
  }

  const count = stats.error_count;
  if (count >= 100) {
    return {
      text: 'text-status-error',
      ring: 'ring-status-error/30',
      dot: 'bg-status-error',
    };
  }
  if (count >= 10) {
    return {
      text: 'text-status-warning',
      ring: 'ring-status-warning/30',
      dot: 'bg-status-warning',
    };
  }
  return {
    text: 'text-status-success',
    ring: 'ring-status-success/30',
    dot: 'bg-status-success',
  };
}

/**
 * Pure helper — renders the visible chip text.
 *
 * Buckets the count display to keep the chip compact:
 *   - 0 errors           → "0 errors / 24h"
 *   - 1..99 errors       → "N errors / 24h"
 *   - 100+ errors        → "100+ errors / 24h"  (caps at "999+" for pathological cases)
 *   - no Sentry project  → "no Sentry project"
 */
export function sentryLabel(stats: SentryStats): string {
  if (stats.reason === 'no_sentry_project' || stats.error_count === null) {
    return 'no Sentry project';
  }
  const count = stats.error_count;
  const window = stats.stats_period || '24h';
  if (count >= 1000) return `999+ errors / ${window}`;
  if (count >= 100) return `${count}+ errors / ${window}`;
  return `${count} ${count === 1 ? 'error' : 'errors'} / ${window}`;
}

/**
 * Pure helper — composes the hover tooltip text.
 *
 * The tooltip is more verbose than the chip and includes the project link
 * target so screen readers + power users see the full destination.
 */
export function sentryTooltip(stats: SentryStats, serviceName?: string): string {
  const name = serviceName || stats.sentry_project_slug || 'service';
  if (stats.reason === 'no_sentry_project') {
    return `Sentry project not found for ${name}. Set services.sentry_project_slug to override.`;
  }
  if (stats.error_count === null) {
    return 'Sentry data unavailable';
  }
  const window = stats.stats_period || '24h';
  return `${stats.error_count} error${stats.error_count === 1 ? '' : 's'} in last ${window} (${stats.sentry_project_slug || name})`;
}
