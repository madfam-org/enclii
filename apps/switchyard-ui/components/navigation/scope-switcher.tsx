'use client';

import * as React from 'react';
import { Check, ChevronDown, Plus, User, Users } from 'lucide-react';
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

export type ScopeType = 'personal' | 'team';
export type PlanTier = 'Hobby' | 'Pro' | 'Team' | 'Enterprise';

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
}: ScopeSwitcherProps) {
  const [open, setOpen] = React.useState(false);

  // Separate personal and team scopes
  const personalScopes = scopes.filter((s) => s.type === 'personal');
  const teamScopes = scopes.filter((s) => s.type === 'team');

  const handleScopeSelect = (scope: Scope) => {
    onScopeChange(scope);
    setOpen(false);
  };

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <button
          className={cn(
            'flex items-center gap-2 px-2 py-1.5 rounded-md',
            'text-sm font-medium text-foreground',
            'hover:bg-accent transition-colors',
            'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
            className
          )}
        >
          <ScopeAvatar scope={currentScope} />
          <span className="max-w-[120px] truncate">{currentScope.name}</span>
          <ChevronDown
            className={cn(
              'h-4 w-4 text-muted-foreground transition-transform',
              open && 'rotate-180'
            )}
          />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        {/* Personal Accounts Section */}
        {personalScopes.length > 0 && (
          <>
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
