'use client';

import { useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  Loader2,
  XCircle,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatRelativeTime, formatFullTimestamp, formatDuration } from '@/lib/formatting';
import {
  lifecycleEventCategory,
  lifecycleEventLabel,
  type LifecycleEvent,
} from '@/types/lifecycle';

interface TimelineEventProps {
  event: LifecycleEvent;
  /**
   * The next-older event in the same group (chronologically earlier).
   * Used to show step-to-step duration (e.g. "+2m 14s").
   */
  previousEvent?: LifecycleEvent;
  /**
   * Whether this event is the visually-first row in its group (for
   * connector line styling — the line above the first row is hidden).
   */
  isFirst?: boolean;
  /** Whether it's the last row (hide connector line below). */
  isLast?: boolean;
}

const categoryStyle: Record<
  ReturnType<typeof lifecycleEventCategory>,
  { dot: string; iconColor: string; ring: string }
> = {
  success: {
    dot: 'bg-status-success',
    iconColor: 'text-status-success',
    ring: 'ring-status-success/30',
  },
  failure: {
    dot: 'bg-status-error',
    iconColor: 'text-status-error',
    ring: 'ring-status-error/30',
  },
  in_progress: {
    dot: 'bg-status-warning animate-pulse',
    iconColor: 'text-status-warning',
    ring: 'ring-status-warning/30',
  },
  neutral: {
    dot: 'bg-muted-foreground',
    iconColor: 'text-muted-foreground',
    ring: 'ring-muted-foreground/30',
  },
};

function CategoryIcon({
  category,
}: {
  category: ReturnType<typeof lifecycleEventCategory>;
}) {
  const c = categoryStyle[category].iconColor;
  switch (category) {
    case 'success':
      return <CheckCircle2 aria-hidden="true" className={cn('h-4 w-4', c)} />;
    case 'failure':
      return <XCircle aria-hidden="true" className={cn('h-4 w-4', c)} />;
    case 'in_progress':
      return (
        <Loader2 aria-hidden="true" className={cn('h-4 w-4 animate-spin', c)} />
      );
    default:
      return <Circle aria-hidden="true" className={cn('h-4 w-4', c)} />;
  }
}

function durationBetween(later: string, earlier: string): string | null {
  const a = new Date(later).getTime();
  const b = new Date(earlier).getTime();
  if (!Number.isFinite(a) || !Number.isFinite(b) || a <= b) return null;
  const seconds = Math.round((a - b) / 1000);
  if (seconds < 1) return null;
  return formatDuration(seconds);
}

/**
 * Single row in a deployment timeline group.
 *
 * Accessibility:
 *   - Outer element uses role="listitem" (the parent uses role="list").
 *   - Status icons are aria-hidden, the visible text label provides the
 *     announcement for screen readers ("Deploy healthy, 3m ago").
 *   - The expand/collapse button is a real <button> with aria-expanded.
 */
export function TimelineEvent({
  event,
  previousEvent,
  isFirst,
  isLast,
}: TimelineEventProps) {
  const category = lifecycleEventCategory(event.event_type);
  const styles = categoryStyle[category];
  const [expanded, setExpanded] = useState(category === 'failure');

  // Pull common metadata fields if present.
  const errorMessage =
    typeof event.metadata?.error_message === 'string'
      ? (event.metadata.error_message as string)
      : null;
  const message = event.message || null;
  const expandable = Boolean(errorMessage || message);

  const stepDuration = previousEvent
    ? durationBetween(event.created_at, previousEvent.created_at)
    : null;

  return (
    <li
      className="group relative flex gap-3 pb-4"
      data-event-type={event.event_type}
    >
      {/* Vertical connector */}
      <div className="relative flex w-5 shrink-0 flex-col items-center">
        {!isFirst && (
          <div
            aria-hidden="true"
            className="absolute -top-2 left-1/2 h-3 w-px -translate-x-1/2 bg-border"
          />
        )}
        <span
          className={cn(
            'relative z-10 mt-1 flex h-3 w-3 items-center justify-center rounded-full ring-4',
            styles.dot,
            styles.ring,
          )}
        />
        {!isLast && (
          <div
            aria-hidden="true"
            className="absolute left-1/2 top-4 h-full w-px -translate-x-1/2 bg-border"
          />
        )}
      </div>

      {/* Body */}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="flex items-center gap-1.5 text-sm font-medium text-foreground">
            <CategoryIcon category={category} />
            {lifecycleEventLabel(event.event_type)}
          </span>
          <time
            className="text-xs text-muted-foreground"
            dateTime={event.created_at}
            title={formatFullTimestamp(event.created_at)}
          >
            {formatRelativeTime(event.created_at)}
          </time>
          {stepDuration && (
            <span
              className="text-xs text-muted-foreground/80"
              aria-label={`Duration since previous step: ${stepDuration}`}
              title={`+${stepDuration} since previous step`}
            >
              +{stepDuration}
            </span>
          )}
          {event.target_env && (
            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
              {event.target_env}
            </span>
          )}
        </div>

        {/* Optional inline message */}
        {message && !expandable && (
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {message}
          </p>
        )}

        {expandable && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            aria-expanded={expanded}
            className="mt-1 inline-flex items-center gap-1 rounded text-xs text-muted-foreground hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1"
          >
            {expanded ? (
              <ChevronDown aria-hidden="true" className="h-3 w-3" />
            ) : (
              <ChevronRight aria-hidden="true" className="h-3 w-3" />
            )}
            {expanded ? 'Hide details' : 'Show details'}
          </button>
        )}

        {expandable && expanded && (
          <div className="mt-1.5 space-y-1 rounded border border-border/60 bg-muted/30 p-2 text-xs">
            {message && (
              <p className="text-muted-foreground">{message}</p>
            )}
            {errorMessage && (
              <p className="flex items-start gap-1.5 text-status-error">
                <AlertTriangle
                  aria-hidden="true"
                  className="mt-0.5 h-3 w-3 shrink-0"
                />
                <span className="break-words font-mono leading-relaxed">
                  {errorMessage}
                </span>
              </p>
            )}
          </div>
        )}
      </div>
    </li>
  );
}
