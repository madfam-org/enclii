'use client';

import { cn } from '@/lib/utils';

/**
 * Color buckets for TLS-cert expiry. Matches the convention used elsewhere
 * in the dashboard (status-success / status-warning / status-error / muted).
 */
export type CertExpiryTone = 'critical' | 'warning' | 'ok' | 'unknown';

export interface CertExpiryDescription {
  /** Short label shown in the table cell, e.g. "in 5d", "expired", "unknown". */
  label: string;
  /** Tone used to color the cell text + icon. */
  tone: CertExpiryTone;
  /** Tailwind class for the dot/text. */
  toneClass: string;
}

/**
 * Pure, deterministic helper. Exported for unit tests.
 *
 * Buckets:
 *  - null/invalid             -> unknown (gray)
 *  - already expired (<= 0d)  -> critical (red)
 *  - < 7d to expiry           -> critical (red)
 *  - 7d - 30d to expiry       -> warning  (amber)
 *  - > 30d to expiry          -> ok       (green)
 *
 * The `nowMs` argument is injectable so tests don't need to mock Date.
 */
export function describeCertExpiry(
  expiresAt: string | null | undefined,
  nowMs: number = Date.now(),
): CertExpiryDescription {
  if (!expiresAt) {
    return {
      label: 'unknown',
      tone: 'unknown',
      toneClass: 'text-muted-foreground',
    };
  }
  const t = new Date(expiresAt).getTime();
  if (!Number.isFinite(t)) {
    return {
      label: 'unknown',
      tone: 'unknown',
      toneClass: 'text-muted-foreground',
    };
  }

  const diffMs = t - nowMs;
  const diffDays = Math.floor(diffMs / 86_400_000);

  if (diffMs <= 0) {
    return {
      label: 'expired',
      tone: 'critical',
      toneClass: 'text-status-error',
    };
  }
  if (diffDays < 7) {
    return {
      label: diffDays === 0 ? 'in <1d' : `in ${diffDays}d`,
      tone: 'critical',
      toneClass: 'text-status-error',
    };
  }
  if (diffDays < 30) {
    return {
      label: `in ${diffDays}d`,
      tone: 'warning',
      toneClass: 'text-status-warning',
    };
  }
  // For very long-lived certs, prefer "in Nd" for <90d, "in Nmo" beyond that.
  if (diffDays < 90) {
    return {
      label: `in ${diffDays}d`,
      tone: 'ok',
      toneClass: 'text-status-success',
    };
  }
  const months = Math.floor(diffDays / 30);
  return {
    label: `in ${months}mo`,
    tone: 'ok',
    toneClass: 'text-status-success',
  };
}

interface CertExpiryIndicatorProps {
  expiresAt: string | null | undefined;
  className?: string;
}

/**
 * Compact relative-time indicator for TLS cert expiry. Renders a colored dot
 * + label with an aria-label describing the absolute expiry timestamp for
 * screen readers.
 */
export function CertExpiryIndicator({
  expiresAt,
  className,
}: CertExpiryIndicatorProps) {
  const { label, tone, toneClass } = describeCertExpiry(expiresAt);

  const ariaLabel = expiresAt
    ? `Certificate expires ${label} (${new Date(expiresAt).toLocaleString()})`
    : 'Certificate expiry unknown';

  return (
    <span
      className={cn('inline-flex items-center gap-1.5 text-xs', className)}
      aria-label={ariaLabel}
      data-testid="cert-expiry-indicator"
      data-tone={tone}
    >
      <span
        aria-hidden="true"
        className={cn(
          'inline-block h-2 w-2 rounded-full',
          tone === 'critical' && 'bg-status-error',
          tone === 'warning' && 'bg-status-warning',
          tone === 'ok' && 'bg-status-success',
          tone === 'unknown' && 'bg-muted-foreground/50',
        )}
      />
      <span className={cn('font-medium', toneClass)}>{label}</span>
    </span>
  );
}
