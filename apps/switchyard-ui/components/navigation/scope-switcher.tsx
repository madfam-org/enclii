'use client';

import * as React from 'react';
import { Check, ChevronDown, LogOut, Plus, Shield, User, Users } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu";
import { Badge } from "@enclii/ui-components/badge";

// =============================================================================
// TYPES
// =============================================================================

export type ScopeType = 'personal' | 'team' | 'admin';
export type PlanTier = 'Hobby' | 'Pro' | 'Team' | 'Enterprise' | 'Admin';

export interface Scope {
  id: string;
  type: ScopeType;
  name: string;
  slug: string;
  plan: PlanTier;
  avatarUrl?: string;
}

interface ScopeSwitcherProps {
  scopes: Scope[];
  currentScope: Scope;
  onScopeChange: (scope: Scope) => void;
  onCreateTeam?: () => void;
  className?: string;
  // Master-admin acting-as wiring. When the current scope is `type: 'admin'`
  // and the operator picks a team, we open an acting-as session via
  // `onEnterTenant` instead of just locally switching scope. When `isActing`
  // is true we render the chip in amber and expose the "End acting-as
  // session" affordance below the team list.
  isAdminViewer?: boolean;
  isActing?: boolean;
  actingTenant?: Scope | null;
  onEnterTenant?: (scope: Scope) => void;
  onExitActingSession?: () => void;
}

// =============================================================================
// AVATAR COMPONENT
// =============================================================================

function ScopeAvatar({ scope, size = 'sm' }: { scope: Scope; size?: 'sm' | 'md' }) {
  const sizeClasses = size === 'sm' ? 'h-5 w-5 text-[10px]' : 'h-6 w-6 text-xs';

  // Generate initials from name
  const initials = scope.name
    .split(' ')
    .map((word) => word[0])
    .join('')
    .toUpperCase()
    .slice(0, 2);

  // Generate consistent background color based on name
  const colors = [
    'bg-blue-500',
    'bg-green-500',
    'bg-purple-500',
    'bg-orange-500',
    'bg-pink-500',
    'bg-cyan-500',
    'bg-indigo-500',
    'bg-teal-500',
  ];
  const colorIndex = scope.name.charCodeAt(0) % colors.length;
  const bgColor = colors[colorIndex];

  if (scope.avatarUrl) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        src={scope.avatarUrl}
        alt={scope.name}
        className={cn(sizeClasses, 'rounded-full object-cover')}
      />
    );
  }

  // Master-admin scope shows a distinct shield (white-glove operator).
  if (scope.type === 'admin') {
    return (
      <div
        className={cn(
          sizeClasses,
          'rounded-full flex items-center justify-center',
          'bg-amber-500/15 text-amber-500'
        )}
      >
        <Shield className="h-3 w-3" />
      </div>
    );
  }

  // Personal scope shows user icon, team shows initials
  if (scope.type === 'personal') {
    return (
      <div
        className={cn(
          sizeClasses,
          'rounded-full flex items-center justify-center',
          'bg-muted text-muted-foreground'
        )}
      >
        <User className="h-3 w-3" />
      </div>
    );
  }

  return (
    <div
      className={cn(
        sizeClasses,
        'rounded-full flex items-center justify-center font-medium text-white',
        bgColor
      )}
    >
      {initials}
    </div>
  );
}

// =============================================================================
// PLAN BADGE COMPONENT
// =============================================================================

function PlanBadge({ plan }: { plan: PlanTier }) {
  const variants: Record<PlanTier, { className: string; label: string }> = {
    Hobby: {
      className: 'bg-muted text-muted-foreground border-transparent',
      label: 'Hobby',
    },
    Pro: {
      className: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
      label: 'Pro',
    },
    Team: {
      className: 'bg-purple-500/10 text-purple-500 border-purple-500/20',
      label: 'Team',
    },
    Enterprise: {
      className: 'bg-orange-500/10 text-orange-500 border-orange-500/20',
      label: 'Enterprise',
    },
    Admin: {
      className: 'bg-amber-500/10 text-amber-500 border-amber-500/20',
      label: 'Master',
    },
  };

  const variant = variants[plan];

  return (
    <Badge
      variant="outline"
      className={cn('text-[10px] px-1.5 py-0 h-4 font-medium', variant.className)}
    >
      {variant.label}
    </Badge>
  );
}

// =============================================================================
// SCOPE SWITCHER COMPONENT
// =============================================================================

export function ScopeSwitcher({
  scopes,
  currentScope,
  onScopeChange,
  onCreateTeam,
  className,
  isAdminViewer = false,
  isActing = false,
  actingTenant = null,
  onEnterTenant,
  onExitActingSession,
}: ScopeSwitcherProps) {
  const [open, setOpen] = React.useState(false);

  // Separate scopes by type. Admin scopes get their own header so master-admin
  // operators are never labelled as a "Personal Account".
  const adminScopes = scopes.filter((s) => s.type === 'admin');
  const personalScopes = scopes.filter((s) => s.type === 'personal');
  const teamScopes = scopes.filter((s) => s.type === 'team');

  const handleScopeSelect = (scope: Scope) => {
    // When an admin viewer picks a team scope from the menu, route through
    // `onEnterTenant` so the backend acting-as session is opened (and the
    // ax_acting_as cookie is set) instead of just doing a local UI swap.
    if (isAdminViewer && scope.type === 'team' && onEnterTenant) {
      onEnterTenant(scope);
    } else {
      onScopeChange(scope);
    }
    setOpen(false);
  };

  // The chip displays the acting tenant when an acting-as session is open;
  // otherwise it shows the operator's own scope. The amber tint mirrors the
  // master-admin avatar so it's obvious at a glance the operator is in a
  // temporary impersonation context.
  const chipScope = isActing && actingTenant ? actingTenant : currentScope;
  const chipSubtitle = isActing && actingTenant ? `Acting as ${actingTenant.name}` : null;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <button
          className={cn(
            'flex items-center gap-2 px-2 py-1.5 rounded-md',
            'text-sm font-medium text-foreground',
            'hover:bg-accent transition-colors',
            'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
            isActing && 'bg-amber-500/15 hover:bg-amber-500/20',
            className
          )}
        >
          <ScopeAvatar scope={chipScope} />
          <div className="flex flex-col items-start min-w-0">
            <span className="max-w-[140px] truncate leading-tight">{chipScope.name}</span>
            {chipSubtitle && (
              <span className="max-w-[140px] truncate text-[10px] leading-tight text-amber-600 dark:text-amber-400">
                {chipSubtitle}
              </span>
            )}
          </div>
          <ChevronDown
            className={cn(
              'h-4 w-4 text-muted-foreground transition-transform',
              open && 'rotate-180'
            )}
          />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        {/* Master-admin Section */}
        {adminScopes.length > 0 && (
          <>
            <DropdownMenuLabel className="text-xs text-muted-foreground font-normal flex items-center gap-1">
              <Shield className="h-3 w-3" />
              Master Admin
            </DropdownMenuLabel>
            {adminScopes.map((scope) => (
              <DropdownMenuItem
                key={scope.id}
                onClick={() => handleScopeSelect(scope)}
                className="flex items-center gap-2 cursor-pointer"
              >
                <ScopeAvatar scope={scope} size="md" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm">{scope.name}</span>
                    <PlanBadge plan={scope.plan} />
                  </div>
                  <span className="text-xs text-muted-foreground truncate block">
                    {scope.slug}
                  </span>
                </div>
                {currentScope.id === scope.id && (
                  <Check className="h-4 w-4 text-enclii-blue flex-shrink-0" />
                )}
              </DropdownMenuItem>
            ))}
          </>
        )}

        {/* Personal Accounts Section */}
        {personalScopes.length > 0 && (
          <>
            {adminScopes.length > 0 && <DropdownMenuSeparator />}
            <DropdownMenuLabel className="text-xs text-muted-foreground font-normal">
              Personal Account
            </DropdownMenuLabel>
            {personalScopes.map((scope) => (
              <DropdownMenuItem
                key={scope.id}
                onClick={() => handleScopeSelect(scope)}
                className="flex items-center gap-2 cursor-pointer"
              >
                <ScopeAvatar scope={scope} size="md" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm">{scope.name}</span>
                    <PlanBadge plan={scope.plan} />
                  </div>
                  <span className="text-xs text-muted-foreground truncate block">
                    {scope.slug}
                  </span>
                </div>
                {currentScope.id === scope.id && (
                  <Check className="h-4 w-4 text-enclii-blue flex-shrink-0" />
                )}
              </DropdownMenuItem>
            ))}
          </>
        )}

        {/* Teams Section */}
        {teamScopes.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="text-xs text-muted-foreground font-normal flex items-center gap-1">
              <Users className="h-3 w-3" />
              Teams
            </DropdownMenuLabel>
            {teamScopes.map((scope) => (
              <DropdownMenuItem
                key={scope.id}
                onClick={() => handleScopeSelect(scope)}
                className="flex items-center gap-2 cursor-pointer"
              >
                <ScopeAvatar scope={scope} size="md" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm">{scope.name}</span>
                    <PlanBadge plan={scope.plan} />
                  </div>
                  <span className="text-xs text-muted-foreground truncate block">
                    {scope.slug}
                  </span>
                </div>
                {currentScope.id === scope.id && (
                  <Check className="h-4 w-4 text-enclii-blue flex-shrink-0" />
                )}
              </DropdownMenuItem>
            ))}
          </>
        )}

        {/* End acting-as session — only visible to admins currently in an
            acting-as session. Sits below the tenant list per design. */}
        {isAdminViewer && isActing && onExitActingSession && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => {
                onExitActingSession();
                setOpen(false);
              }}
              className="flex items-center gap-2 cursor-pointer text-amber-600 dark:text-amber-400 focus:text-amber-600"
            >
              <div className="h-6 w-6 rounded-full bg-amber-500/15 flex items-center justify-center">
                <LogOut className="h-3 w-3" />
              </div>
              <span className="text-sm font-medium">End acting-as session</span>
            </DropdownMenuItem>
          </>
        )}

        {/* Create Team CTA */}
        {onCreateTeam && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => {
                onCreateTeam();
                setOpen(false);
              }}
              className="flex items-center gap-2 cursor-pointer text-muted-foreground hover:text-foreground"
            >
              <div className="h-6 w-6 rounded-full border-2 border-dashed border-muted-foreground/50 flex items-center justify-center">
                <Plus className="h-3 w-3" />
              </div>
              <span className="text-sm">Create Team</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// =============================================================================
// RE-EXPORT HOOK FROM CONTEXT (FOR BACKWARDS COMPATIBILITY)
// =============================================================================

// The actual scope state hook is now in contexts/ScopeContext.tsx
// This re-export maintains backwards compatibility with existing imports
export { useScope as useScopeState } from '@/contexts/ScopeContext';
