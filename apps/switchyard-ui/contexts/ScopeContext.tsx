'use client';

import * as React from 'react';
import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react';
import { useAuth } from './AuthContext';
import { apiGet, apiPost } from '@/lib/api';
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

interface ScopeContextType {
  // State
  scopes: Scope[];
  currentScope: Scope | null;
  isLoading: boolean;
  error: string | null;

  // Actions
  switchScope: (scope: Scope) => void;
  refreshScopes: () => Promise<void>;
  createTeam: (data: CreateTeamInput) => Promise<Team>;
}

interface CreateTeamInput {
  name: string;
  slug: string;
  description?: string;
  billing_email?: string;
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
    const isMasterAdmin = !!roles?.includes('admin');
    if (isMasterAdmin) {
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

  // Fetch all available scopes
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

      // Fetch teams from API
      const response = await apiGet<{ teams: Team[] }>('/v1/teams');
      const teams = response.teams || [];

      // Build scopes list: personal + teams
      const personalScope = createOwnScope(user.id, user.email, user.name, user.roles);
      const teamScopes = teams.map(teamToScope);
      const allScopes = [personalScope, ...teamScopes];

      setScopes(allScopes);

      // Restore selected scope or default to personal
      const savedScopeId = scopeStorage.get();
      const savedScope = savedScopeId
        ? allScopes.find(s => s.id === savedScopeId)
        : null;

      setCurrentScope(savedScope || personalScope);
    } catch (err) {
      console.error('Failed to fetch scopes:', err);
      setError(err instanceof Error ? err.message : 'Failed to load teams');

      // Fallback to personal scope only
      if (user) {
        const personalScope = createOwnScope(user.id, user.email, user.name, user.roles);
        setScopes([personalScope]);
        setCurrentScope(personalScope);
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

  // Clear state on logout
  useEffect(() => {
    if (!isAuthenticated) {
      setScopes([]);
      setCurrentScope(null);
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
    switchScope,
    refreshScopes: fetchScopes,
    createTeam,
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
