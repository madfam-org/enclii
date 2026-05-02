import { Spinner } from '@/components/ui/spinner';

interface RouteLoaderProps {
  rows?: number;
  showHeader?: boolean;
}

/**
 * A skeleton loading state for full-page route transitions.
 * Used by Next.js loading.tsx files at the route segment level.
 */
export function RouteLoader({ rows = 4, showHeader = true }: RouteLoaderProps) {
  return (
    <div className="animate-pulse space-y-6" aria-busy="true" aria-label="Loading content">
      {showHeader && (
        <div className="space-y-2">
          <div className="h-7 w-48 bg-muted rounded-md" />
          <div className="h-4 w-72 bg-muted/60 rounded-md" />
        </div>
      )}

      {/* Stat cards row */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="h-20 bg-muted rounded-lg" />
        ))}
      </div>

      {/* Content rows */}
      <div className="space-y-3">
        {Array.from({ length: rows }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 p-4 rounded-lg border border-border bg-card/30">
            <div className="h-8 w-8 rounded-md bg-muted shrink-0" />
            <div className="flex-1 space-y-1.5">
              <div className="h-3.5 w-1/3 bg-muted rounded" />
              <div className="h-3 w-1/2 bg-muted/60 rounded" />
            </div>
            <div className="h-5 w-16 bg-muted rounded-full shrink-0" />
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * Minimal centered spinner — used in simple loading pages
 */
export function CenteredLoader() {
  return (
    <div className="flex items-center justify-center py-24">
      <Spinner size="lg" />
    </div>
  );
}
