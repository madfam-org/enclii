'use client';

import * as React from 'react';
import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react';
import { useAuth } from './AuthContext';
import { apiGet, apiPost } from '@/lib/api';
import {
  enterTenantSession,
  exitTenantSession,
  fetchActiveActingSession,
  fetchAdminTenants,
} from '@/lib/admin-tenants';
import type { Scope, ScopeType, PlanTier } from '@/components/navigation/scope-switcher';

// =============================================================================
// TYPES
// =============================================================================

interface Team {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  avatar_url: string | null;
  billing_email: string | null;
  member_count: number;
  user_role: string;
  plan?: PlanTier;
  created_at: string;
  updated_at: string;
}

// Shape returned by GET /v1/admin/tenants. Keep in sync with
// switchyard-api/internal/api/admin_tenants_handlers.go (TenantListResponse).
// Admins receive the global tenant list; non-admins continue to use the
// existing /v1/teams endpoint, which is membership-scoped.
export interface AdminTenant {
  id: string;
  name: string;
  slug: string;
  description?: string | null;
  avatar_url?: string | null;
  billing_email?: string | null;
  member_count?: number;
  project_count?: number;
  last_deploy_at?: string | null;
  created_at: string;
}

// Shape returned by GET /v1/admin/tenants/active and the enter/exit endpoints.
// `active=false` means no acting-as session; in that case `tenant` is absent.
export interface ActiveActingSession {
  active: boolean;
  tenant?: AdminTenant;
  started_at?: string;
  expires_at?: string;
  reason?: string;
}

interface ScopeContextType {
  // State
  scopes: Scope[];
  currentScope: Scope | null;
  isLoading: boolean;
  error: string | null;

  // Acting-as state (master-admin only)
  actingTenant: Scope | null;
  actingExpiresAt: string | null;
  isActing: boolean;

  // Actions
  switchScope: (scope: Scope) => void;
  refreshScopes: () => Promise<void>;
  createTeam: (data: CreateTeamInput) => Promise<Team>;

  // Acting-as actions
  enterTenant: (slug: string, reason?: string) => Promise<void>;
  exitActingSession: () => Promise<void>;
  refreshActingSession: () => Promise<void>;
}

interface CreateTeamInput {
  name: string;
  slug: string;
  description?: string;
  billing_email?: string;
}

// =============================================================================
// PURE HELPERS (exported for unit testing)
// =============================================================================

/**
 * Translate a TenantListResponse from /v1/admin/tenants into a Scope.
 * Admin tenants are surfaced under the existing "Teams" section so the menu
 * reads: Master Admin → Teams (one per tenant). The `plan` defaults to
 * `'Team'` because the admin endpoint does not expose subscription tier.
 */
export function adminTenantToScope(tenant: AdminTenant): Scope {
  return {
    id: tenant.id,
    type: 'team' as ScopeType,
    name: tenant.name,
    slug: tenant.slug,
    plan: 'Team' as PlanTier,
    avatarUrl: tenant.avatar_url || undefined,
  };
}

/**
 * Translate an ActiveActingSession payload into a Scope, or `null` if no
 * session is active. Used to drive the "Acting as <tenant>" UI.
 */
export function activeSessionToScope(session: ActiveActingSession | null | undefined): Scope | null {
  if (!session || !session.active || !session.tenant) return null;
  return adminTenantToScope(session.tenant);
}

// =============================================================================
// STORAGE HELPERS
// =============================================================================

const SCOPE_STORAGE_KEY = 'enclii-current-scope';

const scopeStorage = {
  get(): string | null {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(SCOPE_STORAGE_KEY);
  },

  set(scopeId: string): void {
    if (typeof window === 'undefined') return;
    localStorage.setItem(SCOPE_STORAGE_KEY, scopeId);
  },

  clear(): void {
    if (typeof window === 'undefined') return;
    localStorage.removeItem(SCOPE_STORAGE_KEY);
  },
};

// =============================================================================
// CONTEXT
// =============================================================================

const ScopeContext = createContext<ScopeContextType | undefined>(undefined);

// =============================================================================
// PROVIDER
// =============================================================================

interface ScopeProviderProps {
  children: ReactNode;
}

export function ScopeProvider({ children }: ScopeProviderProps) {
  const { user, isAuthenticated } = useAuth();
  const [scopes, setScopes] = useState<Scope[]>([]);
  const [currentScope, setCurrentScope] = useState<Scope | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actingTenant, setActingTenant] = useState<Scope | null>(null);
  const [actingExpiresAt, setActingExpiresAt] = useState<string | null>(null);

  const isMasterAdmin = !!user?.roles?.includes('admin');

  // Transform team to scope
  const teamToScope = useCallback((team: Team): Scope => {
    return {
      id: team.id,
      type: 'team' as ScopeType,
      name: team.name,
      slug: team.slug,
      plan: team.plan || 'Team',
      avatarUrl: team.avatar_url || undefined,
    };
  }, []);

  // Build the user's "own" scope. Master admins (users carrying the `admin`
  // role on their JWT — see SEC-007 in switchyard-api auth middleware) get an
  // explicit admin scope rather than the synthetic "Personal Account (Hobby)"
  // label, since they're white-glove operators acting across tenants, not
  // hobby-tier consumers. See claudedocs/master-admin-tenant-switching.md
  // for the full design and follow-on backend work.
  const createOwnScope = useCallback((
    userId: string,
    email: string,
    name?: string,
    roles?: string[],
  ): Scope => {
    const isAdmin = !!roles?.includes('admin');
    if (isAdmin) {
      return {
        id: `admin-${userId}`,
        type: 'admin' as ScopeType,
        name: 'Master Admin',
        slug: email.split('@')[0] || 'admin',
        plan: 'Admin' as PlanTier,
      };
    }
    return {
      id: `personal-${userId}`,
      type: 'personal' as ScopeType,
      name: name || 'Personal Account',
      slug: email.split('@')[0] || 'personal',
      plan: 'Hobby' as PlanTier,
    };
  }, []);

  // Fetch all available scopes. Admins pull the global tenant list from
  // /v1/admin/tenants; everyone else stays on /v1/teams (membership-scoped).
  const fetchScopes = useCallback(async () => {
    if (!isAuthenticated || !user) {
      setScopes([]);
      setCurrentScope(null);
      setIsLoading(false);
      return;
    }

    try {
      setError(null);
      setIsLoading(true);

      const ownScope = createOwnScope(user.id, user.email, user.name, user.roles);
      const isAdmin = !!user.roles?.includes('admin');

      let tenantScopes: Scope[];
      if (isAdmin) {
        const tenants = await fetchAdminTenants();
        tenantScopes = tenants.map(adminTenantToScope);
      } else {
        const response = await apiGet<{ teams: Team[] }>('/v1/teams');
        const teams = response.teams || [];
        tenantScopes = teams.map(teamToScope);
      }

      // Master Admin first, then teams.
      const allScopes = [ownScope, ...tenantScopes];

      setScopes(allScopes);

      // Restore selected scope or default to the user's own scope.
      const savedScopeId = scopeStorage.get();
      const savedScope = savedScopeId
        ? allScopes.find(s => s.id === savedScopeId)
        : null;

      setCurrentScope(savedScope || ownScope);
    } catch (err) {
      console.error('Failed to fetch scopes:', err);
      setError(err instanceof Error ? err.message : 'Failed to load teams');

      // Fallback to own scope only
      if (user) {
        const ownScope = createOwnScope(user.id, user.email, user.name, user.roles);
        setScopes([ownScope]);
        setCurrentScope(ownScope);
      }
    } finally {
      setIsLoading(false);
    }
  }, [isAuthenticated, user, createOwnScope, teamToScope]);

  // Initial fetch
  useEffect(() => {
    fetchScopes();
  }, [fetchScopes]);

  // Switch current scope
  const switchScope = useCallback((scope: Scope) => {
    setCurrentScope(scope);
    scopeStorage.set(scope.id);
  }, []);

  // Create a new team
  const createTeam = useCallback(async (data: CreateTeamInput): Promise<Team> => {
    const team = await apiPost<Team>('/v1/teams', {
      name: data.name,
      slug: data.slug,
      description: data.description || undefined,
      billing_email: data.billing_email || undefined,
    });

    // Refresh scopes to include new team
    await fetchScopes();

    // Switch to the new team
    const newScope = teamToScope(team);
    switchScope(newScope);

    return team;
  }, [fetchScopes, teamToScope, switchScope]);

  // ==========================================================================
  // ACTING-AS SESSION (master admin only)
  // ==========================================================================

  // Cookie is HttpOnly so we always re-derive acting state from the API.
  const refreshActingSession = useCallback(async () => {
    if (!isMasterAdmin) {
      setActingTenant(null);
      setActingExpiresAt(null);
      return;
    }
    try {
      const session = await fetchActiveActingSession();
      setActingTenant(activeSessionToScope(session));
      setActingExpiresAt(session?.active ? session.expires_at ?? null : null);
    } catch (err) {
      // A failed poll shouldn't blow up the app; just clear the banner so
      // operators don't act on stale state.
      console.error('Failed to fetch active acting session:', err);
      setActingTenant(null);
      setActingExpiresAt(null);
    }
  }, [isMasterAdmin]);

  // POST /v1/admin/tenants/:slug/enter, then hard-reload so every cached
  // page-level fetch re-runs under the new ax_acting_as cookie scope.
  const enterTenant = useCallback(async (slug: string, reason?: string) => {
    if (!isMasterAdmin) {
      setError('Only master admins can enter a tenant.');
      return;
    }
    try {
      setError(null);
      const session = await enterTenantSession(slug, reason);
      setActingTenant(activeSessionToScope(session));
      setActingExpiresAt(session?.active ? session.expires_at ?? null : null);
      // Hard reload so SWR caches, server components, and per-page fetches
      // all re-issue under the new cookie.
      if (typeof window !== 'undefined') {
        window.location.reload();
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to enter tenant';
      console.error('enterTenant failed:', err);
      setError(msg);
      throw err;
    }
  }, [isMasterAdmin]);

  // POST /v1/admin/tenants/exit, clear local state, hard-reload.
  const exitActingSession = useCallback(async () => {
    try {
      setError(null);
      await exitTenantSession();
      setActingTenant(null);
      setActingExpiresAt(null);
      if (typeof window !== 'undefined') {
        window.location.reload();
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to end acting session';
      console.error('exitActingSession failed:', err);
      setError(msg);
      throw err;
    }
  }, []);

  // Initial poll + refresh on window focus so an externally-expired or
  // server-side-revoked session updates the UI without a manual refresh.
  useEffect(() => {
    if (!isMasterAdmin) {
      setActingTenant(null);
      setActingExpiresAt(null);
      return;
    }
    refreshActingSession();
    const onFocus = () => {
      refreshActingSession();
    };
    if (typeof window !== 'undefined') {
      window.addEventListener('focus', onFocus);
      return () => window.removeEventListener('focus', onFocus);
    }
  }, [isMasterAdmin, refreshActingSession]);

  // Clear state on logout
  useEffect(() => {
    if (!isAuthenticated) {
      setScopes([]);
      setCurrentScope(null);
      setActingTenant(null);
      setActingExpiresAt(null);
      scopeStorage.clear();
    }
  }, [isAuthenticated]);

  // ==========================================================================
  // CONTEXT VALUE
  // ==========================================================================

  const value: ScopeContextType = {
    scopes,
    currentScope,
    isLoading,
    error,
    actingTenant,
    actingExpiresAt,
    isActing: !!actingTenant,
    switchScope,
    refreshScopes: fetchScopes,
    createTeam,
    enterTenant,
    exitActingSession,
    refreshActingSession,
  };

  return (
    <ScopeContext.Provider value={value}>
      {children}
    </ScopeContext.Provider>
  );
}

// =============================================================================
// HOOKS
// =============================================================================

export function useScope(): ScopeContextType {
  const context = useContext(ScopeContext);
  if (context === undefined) {
    throw new Error('useScope must be used within a ScopeProvider');
  }
  return context;
}

/**
 * Hook for getting the current scope ID for API calls
 */
export function useCurrentScopeId(): string | null {
  const { currentScope } = useScope();
  return currentScope?.id || null;
}

/**
 * Hook to check if current scope is a team (not personal)
 */
export function useIsTeamScope(): boolean {
  const { currentScope } = useScope();
  return currentScope?.type === 'team';
}

/**
 * Hook to check if the current scope is the master-admin scope.
 * Use this to gate admin-only UI affordances at the route level until the
 * full tenant-switching API ships (see master-admin-tenant-switching.md).
 */
export function useIsAdminScope(): boolean {
  const { currentScope } = useScope();
  return currentScope?.type === 'admin';
}
