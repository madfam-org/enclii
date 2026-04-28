'use client';

import { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface LastSyncBadgeProps {
  /** ISO timestamp of last successful fetch, or null when never synced. */
  lastSyncedAt: string | null;
  /** Triggered when the user clicks "Refresh". */
  onRefresh?: () => void | Promise<void>;
  /** Show the spinning icon (e.g. while a fetch is in flight). */
  refreshing?: boolean;
  /**
   * Threshold (seconds) above which the badge turns "stale".
   * Defaults to 60s — consistent with POLLING_SLOW (30s) plus a grace period.
   */
  staleAfterSeconds?: number;
  className?: string;
}

interface FreshnessState {
  label: string;
  toneClass: string;
}

function describeFreshness(
  lastSyncedAt: string | null,
  staleAfterSeconds: number,
  nowMs: number,
): FreshnessState {
  if (!lastSyncedAt) {
    return {
      label: 'never synced',
      toneClass: 'text-muted-foreground',
    };
  }
  const t = new Date(lastSyncedAt).getTime();
  if (!Number.isFinite(t)) {
    return { label: 'never synced', toneClass: 'text-muted-foreground' };
  }
  const diffSec = Math.max(0, Math.floor((nowMs - t) / 1000));

  let phrasing: string;
  if (diffSec < 5) phrasing = 'just now';
  else if (diffSec < 60) phrasing = `${diffSec}s ago`;
  else if (diffSec < 3600) phrasing = `${Math.floor(diffSec / 60)}m ago`;
  else phrasing = `${Math.floor(diffSec / 3600)}h ago`;

  const stale = diffSec >= staleAfterSeconds;
  return {
    label: stale ? `stale ${phrasing}` : `synced ${phrasing}`,
    toneClass: stale ? 'text-status-warning' : 'text-status-success',
  };
}

/**
 * Small "last synced" badge with a manual refresh button.
 *
 * Re-renders every 5s so the relative time stays accurate without
 * coupling to the parent's polling cadence.
 *
 * Accessibility:
 *  - The status text is wrapped in role="status" with aria-live="polite"
 *    so changes (e.g. transitioning from "synced 50s ago" → "stale 1m ago")
 *    are announced to screen readers.
 *  - The refresh control is a real <button> with an aria-label.
 */
export function LastSyncBadge({
  lastSyncedAt,
  onRefresh,
  refreshing = false,
  staleAfterSeconds = 60,
  className,
}: LastSyncBadgeProps) {
  // nowMs is stored in state so the React Compiler treats this component
  // as pure — Date.now() is impure and cannot be called during render.
  // The interval below ticks it forward so relative-time labels stay fresh.
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 5_000);
    return () => clearInterval(id);
  }, []);

  const { label, toneClass } = describeFreshness(
    lastSyncedAt,
    staleAfterSeconds,
    nowMs,
  );

  return (
    <div
      className={cn(
        'inline-flex items-center gap-2 text-xs',
        className,
      )}
      data-testid="last-sync-badge"
    >
      <span
        role="status"
        aria-live="polite"
        className={cn('font-medium', toneClass)}
      >
        {label}
      </span>
      {onRefresh && (
        <button
          type="button"
          onClick={() => {
            void onRefresh();
          }}
          disabled={refreshing}
          aria-label="Refresh now"
          className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
        >
          <RefreshCw
            aria-hidden="true"
            className={cn('h-3 w-3', refreshing && 'animate-spin')}
          />
          Refresh
        </button>
      )}
    </div>
  );
}

// Exported for unit tests.
export { describeFreshness };
