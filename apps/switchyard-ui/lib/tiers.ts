/**
 * Tier Configuration for Enclii Platform
 *
 * Defines the foundry tier system for RBAC and feature gating.
 * The `foundry_tier` claim comes from Janua SSO after Dhanam purchase.
 */

// =============================================================================
// TYPES
// =============================================================================

export type FoundryTier = 'community' | 'sovereign' | 'ecosystem' | null;

export interface TierConfig {
  name: string;
  description: string;
  canCreateProject: boolean;
  canDeploy: boolean;
  canUseCustomDomains: boolean;
  canManageTeams: boolean;
  projectLimit: number;  // -1 = unlimited
  serviceLimit: number;  // -1 = unlimited
  price: string;
  cta: {
    label: string;
    href: string;
    disabled?: boolean;
  };
}

export type BlockedAction = 'project' | 'deploy' | 'custom-domain' | 'team';

// =============================================================================
// TIER CONFIGURATION
// =============================================================================

export const TIER_CONFIG: Record<NonNullable<FoundryTier> | 'null', TierConfig> = {
  null: {
    name: 'Guest',
    description: 'Sign in to start building',
    canCreateProject: false,
    canDeploy: false,
    canUseCustomDomains: false,
    canManageTeams: false,
    projectLimit: 0,
    serviceLimit: 0,
    price: '-',
    cta: {
      label: 'Sign In',
      href: '/login',
    },
  },
  community: {
    name: 'Community',
    description: 'Self-host with AGPL-3.0 source',
    canCreateProject: true,
    canDeploy: true,
    canUseCustomDomains: false,
    canManageTeams: false,
    projectLimit: 1,
    serviceLimit: 3,
    price: 'Free',
    cta: {
      label: 'View on GitHub',
      href: 'https://github.com/madfam-org/enclii',
    },
  },
  sovereign: {
    name: 'Sovereign',
    description: 'Managed hosting with auto SSL',
    canCreateProject: true,
    canDeploy: true,
    canUseCustomDomains: true,
    canManageTeams: false,
    projectLimit: 10,
    serviceLimit: -1,
    price: '$20/mo',
    cta: {
      label: 'Start Building',
      href: 'https://app.enclii.dev',
    },
  },
  ecosystem: {
    name: 'Ecosystem',
    description: 'Full bundle with team management',
    canCreateProject: true,
    canDeploy: true,
    canUseCustomDomains: true,
    canManageTeams: true,
    projectLimit: -1,
    serviceLimit: -1,
    price: 'Coming Soon',
    cta: {
      label: 'Join Waitlist',
      href: '#',
      disabled: true,
    },
  },
};

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

/**
 * Get the tier config for a given foundry_tier claim
 */
export function getTierConfig(tier: FoundryTier): TierConfig {
  return TIER_CONFIG[tier ?? 'null'];
}

/**
 * Check if a tier is a paid tier (sovereign or ecosystem)
 */
export function isPaidTier(tier: FoundryTier): boolean {
  return tier === 'sovereign' || tier === 'ecosystem';
}

/**
 * Check if user has reached their project limit
 */
export function hasReachedProjectLimit(tier: FoundryTier, currentProjectCount: number): boolean {
  const config = getTierConfig(tier);
  if (config.projectLimit === -1) return false;
  return currentProjectCount >= config.projectLimit;
}

/**
 * Check if user has reached their service limit for a project
 */
export function hasReachedServiceLimit(tier: FoundryTier, currentServiceCount: number): boolean {
  const config = getTierConfig(tier);
  if (config.serviceLimit === -1) return false;
  return currentServiceCount >= config.serviceLimit;
}

/**
 * Get the upgrade message for a blocked action
 */
export function getUpgradeMessage(action: BlockedAction, tier: FoundryTier): string {
  const config = getTierConfig(tier);

  switch (action) {
    case 'project':
      if (!config.canCreateProject) {
        return 'Sign in to create projects';
      }
      return `You've reached your limit of ${config.projectLimit} project${config.projectLimit !== 1 ? 's' : ''}. Upgrade to create more.`;

    case 'deploy':
      if (!config.canDeploy) {
        return 'Sign in to deploy services';
      }
      return `You've reached your limit of ${config.serviceLimit} service${config.serviceLimit !== 1 ? 's' : ''}. Upgrade to deploy more.`;

    case 'custom-domain':
      return 'Custom domains are available on Sovereign tier and above.';

    case 'team':
      return 'Team management is available on Ecosystem tier.';

    default:
      return 'Upgrade your plan to access this feature.';
  }
}

/**
 * Get the checkout URL for upgrading
 */
export function getCheckoutUrl(userId?: string, returnUrl?: string): string {
  const baseUrl = process.env.NEXT_PUBLIC_DHANAM_CHECKOUT_URL || 'https://dhanam.madfam.io/checkout';
  const params = new URLSearchParams();
  params.set('plan', 'enclii_sovereign');
  if (userId) {
    params.set('user_id', userId);
  }
  if (returnUrl) {
    params.set('return_url', returnUrl);
  }
  return `${baseUrl}?${params.toString()}`;
}
