/**
 * Shared constants for the Switchyard UI
 */

/** Single source of truth for the API base URL */
export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL || 'http://localhost:4200';

/** Auth mode (local or oidc) */
export const AUTH_MODE = process.env.NEXT_PUBLIC_AUTH_MODE || 'local';

// ---------------------------------------------------------------------------
// Polling intervals
// ---------------------------------------------------------------------------

/** Fast polling for actively changing resources (5s) */
export const POLLING_FAST = 5_000;

/** Normal polling for dashboards and lists (15s) */
export const POLLING_NORMAL = 15_000;

/** Slow polling for health checks and background tasks (30s) */
export const POLLING_SLOW = 30_000;

/** Idle polling for rarely changing data (60s) */
export const POLLING_IDLE = 60_000;

// ---------------------------------------------------------------------------
// Status color maps (reusable across badge / indicator components)
// ---------------------------------------------------------------------------

export const SERVICE_STATUS_COLORS: Record<string, string> = {
  running: 'bg-status-success',
  stopped: 'bg-muted-foreground',
  deploying: 'bg-status-info animate-pulse',
  failed: 'bg-status-error',
  pending: 'bg-status-warning',
};

export const DEPLOYMENT_STATUS_COLORS: Record<string, string> = {
  success: 'text-status-success',
  failed: 'text-status-error',
  pending: 'text-status-warning',
  building: 'text-status-info',
};

export const HEALTH_STATUS_COLORS: Record<string, string> = {
  healthy: 'bg-status-success-muted text-status-success-foreground',
  unhealthy: 'bg-status-error-muted text-status-error-foreground',
  unknown: 'bg-muted text-muted-foreground',
};
