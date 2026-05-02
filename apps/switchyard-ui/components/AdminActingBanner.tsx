'use client';

/**
 * Sticky banner that surfaces the master-admin "Acting as <tenant>" state on
 * every protected page. Renders nothing for non-admin users or when no
 * acting-as session is open.
 *
 * Truth source: ScopeContext (which hydrates from GET /v1/admin/tenants/active).
 * The cookie is HttpOnly, so we never read it directly here.
 */

import * as React from 'react';
import { ShieldAlert } from 'lucide-react';
import { useScope } from '@/contexts/ScopeContext';
import { cn } from '@/lib/utils';

interface AdminActingBannerProps {
  /** When true, renders even on small screens; defaults to true. */
  alwaysVisible?: boolean;
  className?: string;
}

/**
 * Render the time remaining as a coarse-grained, human-friendly label.
 * Exported for unit testing.
 */
export function formatTimeRemaining(expiresAt: string | null | undefined, now: Date = new Date()): string {
  if (!expiresAt) return '';
  const expires = new Date(expiresAt);
  if (Number.isNaN(expires.getTime())) return '';
  const ms = expires.getTime() - now.getTime();
  if (ms <= 0) return 'expired';
  const totalMinutes = Math.floor(ms / 60_000);
  if (totalMinutes < 1) return 'less than 1 minute remaining';
  if (totalMinutes < 60) {
    return `${totalMinutes} minute${totalMinutes === 1 ? '' : 's'} remaining`;
  }
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (minutes === 0) return `${hours} hour${hours === 1 ? '' : 's'} remaining`;
  return `${hours}h ${minutes}m remaining`;
}

export function AdminActingBanner({ alwaysVisible = true, className }: AdminActingBannerProps) {
  const { isActing, actingTenant, actingExpiresAt, exitActingSession } = useScope();
  // Update once a minute so the countdown stays roughly current without
  // hammering renders.
  const [tick, setTick] = React.useState(0);
  React.useEffect(() => {
    if (!isActing) return;
    const id = setInterval(() => setTick((t) => t + 1), 60_000);
    return () => clearInterval(id);
  }, [isActing]);

  if (!isActing || !actingTenant) return null;

  const timeRemaining = formatTimeRemaining(actingExpiresAt, new Date(Date.now() + tick * 0));

  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        'sticky top-0 z-[60] w-full bg-amber-500/15 border-b border-amber-500/30',
        'text-amber-900 dark:text-amber-100',
        !alwaysVisible && 'hidden md:block',
        className,
      )}
    >
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2 min-w-0">
          <ShieldAlert className="h-4 w-4 flex-shrink-0 text-amber-600 dark:text-amber-400" />
          <p className="text-xs sm:text-sm truncate">
            <span className="font-semibold">Acting as {actingTenant.name}</span>
            {timeRemaining && (
              <span className="text-amber-700 dark:text-amber-300/80"> — {timeRemaining}.</span>
            )}
            <span className="hidden sm:inline text-amber-700 dark:text-amber-300/80">
              {' '}All actions are recorded against this tenant.
            </span>
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            void exitActingSession();
          }}
          className={cn(
            'flex-shrink-0 px-3 py-1 rounded-md text-xs font-medium',
            'bg-amber-500/25 hover:bg-amber-500/35',
            'text-amber-900 dark:text-amber-100',
            'focus:outline-none focus:ring-2 focus:ring-amber-500/50',
            'transition-colors',
          )}
        >
          End session
        </button>
      </div>
    </div>
  );
}
