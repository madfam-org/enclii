'use client';

import { useState, useCallback, useMemo } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import {
  type FoundryTier,
  type TierConfig,
  type BlockedAction,
  getTierConfig,
  isPaidTier,
  hasReachedProjectLimit,
  hasReachedServiceLimit,
  getUpgradeMessage,
  getCheckoutUrl,
} from '@/lib/tiers';

/**
 * Hook for tier-based RBAC and feature gating
 *
 * Usage:
 * ```tsx
 * const { requireTier, showUpgradeModal, blockedAction } = useTier();
 *
 * const handleCreateProject = () => {
 *   if (!requireTier('project')) return; // Shows modal if blocked
 *   // Proceed with creation
 * };
 * ```
 */
export function useTier() {
  const { user } = useAuth();
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [blockedAction, setBlockedAction] = useState<BlockedAction | null>(null);

  // Get the user's tier from JWT claims
  const tier: FoundryTier = user?.foundry_tier || null;
  const config: TierConfig = useMemo(() => getTierConfig(tier), [tier]);
  const isPaid = isPaidTier(tier);

  /**
   * Check if an action is allowed and show upgrade modal if not
   * Returns true if action is allowed, false if blocked
   */
  const requireTier = useCallback((
    action: BlockedAction,
    context?: { currentProjectCount?: number; currentServiceCount?: number }
  ): boolean => {
    // Admins bypass all limits
    if (user?.roles?.includes('admin') || user?.roles?.includes('superadmin')) {
      return true;
    }

    let allowed = false;

    switch (action) {
      case 'project':
        if (!config.canCreateProject) {
          allowed = false;
        } else if (context?.currentProjectCount !== undefined) {
          allowed = !hasReachedProjectLimit(tier, context.currentProjectCount);
        } else {
          allowed = true;
        }
        break;

      case 'deploy':
        if (!config.canDeploy) {
          allowed = false;
        } else if (context?.currentServiceCount !== undefined) {
          allowed = !hasReachedServiceLimit(tier, context.currentServiceCount);
        } else {
          allowed = true;
        }
        break;

      case 'custom-domain':
        allowed = config.canUseCustomDomains;
        break;

      case 'team':
        allowed = config.canManageTeams;
        break;

      default:
        allowed = false;
    }

    if (!allowed) {
      setBlockedAction(action);
      setShowUpgradeModal(true);
      return false;
    }

    return true;
  }, [config, tier]);

  /**
   * Get the upgrade message for the current blocked action
   */
  const upgradeMessage = useMemo(() => {
    if (!blockedAction) return '';
    return getUpgradeMessage(blockedAction, tier);
  }, [blockedAction, tier]);

  /**
   * Close the upgrade modal
   */
  const closeUpgradeModal = useCallback(() => {
    setShowUpgradeModal(false);
    setBlockedAction(null);
  }, []);

  /**
   * Get the checkout URL for the current context
   */
  const checkoutUrl = useMemo(() => {
    if (typeof window === 'undefined') return '';
    return getCheckoutUrl(user?.id, window.location.href);
  }, [user?.id]);

  return {
    // Current tier info
    tier,
    config,
    isPaid,
    tierName: config.name,

    // Limits
    projectLimit: config.projectLimit,
    serviceLimit: config.serviceLimit,

    // Capabilities
    canCreateProject: config.canCreateProject,
    canDeploy: config.canDeploy,
    canUseCustomDomains: config.canUseCustomDomains,
    canManageTeams: config.canManageTeams,

    // RBAC
    requireTier,

    // Modal state
    showUpgradeModal,
    setShowUpgradeModal,
    closeUpgradeModal,
    blockedAction,
    upgradeMessage,

    // Checkout
    checkoutUrl,
  };
}

export type UseTierReturn = ReturnType<typeof useTier>;
