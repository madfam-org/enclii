/**
 * Thin client-side wrappers around the master-admin tenant-switching API.
 *
 * Backend contract lives in
 *   apps/switchyard-api/internal/api/admin_tenants_handlers.go
 *
 * The cookie set by /enter (`ax_acting_as`) is HttpOnly, so the SPA never
 * reads it directly — these helpers always re-source acting state from the
 * /active endpoint. See claudedocs/master-admin-tenant-switching.md.
 */

import { apiGet, apiPost } from '@/lib/api';
import type { ActiveActingSession, AdminTenant } from '@/contexts/ScopeContext';

/** GET /v1/admin/tenants — global tenant list (admin only). */
export async function fetchAdminTenants(): Promise<AdminTenant[]> {
  const response = await apiGet<{ tenants: AdminTenant[] }>('/v1/admin/tenants');
  return response.tenants || [];
}

/** GET /v1/admin/tenants/active — current acting-as session for caller. */
export async function fetchActiveActingSession(): Promise<ActiveActingSession> {
  return apiGet<ActiveActingSession>('/v1/admin/tenants/active');
}

/**
 * POST /v1/admin/tenants/:slug/enter — open an acting-as session and set
 * the ax_acting_as HttpOnly cookie.
 */
export async function enterTenantSession(
  slug: string,
  reason?: string,
  durationSeconds?: number,
): Promise<ActiveActingSession> {
  const body: { reason?: string; duration_seconds?: number } = {};
  if (reason) body.reason = reason;
  if (durationSeconds && durationSeconds > 0) body.duration_seconds = durationSeconds;
  return apiPost<ActiveActingSession>(
    `/v1/admin/tenants/${encodeURIComponent(slug)}/enter`,
    body,
  );
}

/** POST /v1/admin/tenants/exit — close every open acting-as session. */
export async function exitTenantSession(): Promise<ActiveActingSession> {
  return apiPost<ActiveActingSession>('/v1/admin/tenants/exit', {});
}
