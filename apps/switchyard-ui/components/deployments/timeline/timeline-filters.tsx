'use client';

import { Filter } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  LIFECYCLE_EVENT_TYPES,
  LIFECYCLE_RESULT_FILTERS,
  LIFECYCLE_SINCE_OPTIONS,
  lifecycleEventLabel,
  type LifecycleEventType,
  type LifecycleResultFilter,
  type LifecycleSinceOption,
} from '@/types/lifecycle';

interface TimelineFiltersProps {
  selectedEventTypes: LifecycleEventType[];
  onEventTypesChange: (next: LifecycleEventType[]) => void;
  since: LifecycleSinceOption;
  onSinceChange: (next: LifecycleSinceOption) => void;
  result: LifecycleResultFilter;
  onResultChange: (next: LifecycleResultFilter) => void;
  className?: string;
}

const sinceLabels: Record<LifecycleSinceOption, string> = {
  '24h': 'Last 24h',
  '7d': 'Last 7d',
  '30d': 'Last 30d',
  all: 'All time',
};

const resultLabels: Record<LifecycleResultFilter, string> = {
  all: 'All results',
  success: 'Success only',
  failure: 'Failures only',
};

/**
 * Filter strip rendered above the timeline.
 *
 * Accessibility:
 *  - All controls have explicit <label> elements (or aria-label).
 *  - Multi-select event types use accessible <button role="checkbox">
 *    pattern with aria-checked so keyboard users can toggle without a
 *    custom dropdown.
 */
export function TimelineFilters({
  selectedEventTypes,
  onEventTypesChange,
  since,
  onSinceChange,
  result,
  onResultChange,
  className,
}: TimelineFiltersProps) {
  const toggleType = (t: LifecycleEventType) => {
    if (selectedEventTypes.includes(t)) {
      onEventTypesChange(selectedEventTypes.filter((x) => x !== t));
    } else {
      onEventTypesChange([...selectedEventTypes, t]);
    }
  };

  return (
    <div
      className={cn(
        'space-y-3 rounded-lg border border-border bg-card p-3 sm:p-4',
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-3">
        <Filter
          aria-hidden="true"
          className="h-4 w-4 shrink-0 text-muted-foreground"
        />

        <div className="flex flex-wrap items-center gap-2">
          <label
            htmlFor="timeline-since"
            className="text-xs font-medium text-muted-foreground"
          >
            Since
          </label>
          <select
            id="timeline-since"
            value={since}
            onChange={(e) =>
              onSinceChange(e.target.value as LifecycleSinceOption)
            }
            className="rounded border border-border bg-background px-2 py-1 text-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {LIFECYCLE_SINCE_OPTIONS.map((opt) => (
              <option key={opt} value={opt}>
                {sinceLabels[opt]}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <label
            htmlFor="timeline-result"
            className="text-xs font-medium text-muted-foreground"
          >
            Result
          </label>
          <select
            id="timeline-result"
            value={result}
            onChange={(e) =>
              onResultChange(e.target.value as LifecycleResultFilter)
            }
            className="rounded border border-border bg-background px-2 py-1 text-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {LIFECYCLE_RESULT_FILTERS.map((opt) => (
              <option key={opt} value={opt}>
                {resultLabels[opt]}
              </option>
            ))}
          </select>
        </div>

        {selectedEventTypes.length > 0 && (
          <button
            type="button"
            onClick={() => onEventTypesChange([])}
            className="ml-auto text-xs text-muted-foreground hover:text-foreground focus:outline-none focus-visible:underline"
          >
            Clear event types ({selectedEventTypes.length})
          </button>
        )}
      </div>

      <div
        role="group"
        aria-label="Filter by event type"
        className="flex flex-wrap gap-1.5"
      >
        {LIFECYCLE_EVENT_TYPES.map((t) => {
          const checked = selectedEventTypes.includes(t);
          return (
            <button
              key={t}
              type="button"
              role="checkbox"
              aria-checked={checked}
              onClick={() => toggleType(t)}
              className={cn(
                'rounded-full border px-2 py-0.5 text-[11px] transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                checked
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border text-muted-foreground hover:border-foreground/40 hover:text-foreground',
              )}
            >
              {lifecycleEventLabel(t)}
            </button>
          );
        })}
      </div>
    </div>
  );
}
