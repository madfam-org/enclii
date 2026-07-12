/**
 * Tier Configuration for Enclii Platform
 *
 * Defines the foundry tier system for RBAC and feature gating.
 * The `foundry_tier` claim comes from Janua SSO after Dhanam purchase.
 *
 * Canonical customer-facing catalog (matches the landing page and
 * docs/faq/billing.md): Community (free) / Sovereign $20/mo / Ecosystem
 * (coming soon). Display names only — the internal claim slugs
 * (community/essentials/pro/madfam) mirror API enforcement in
 * apps/switchyard-api/internal/middleware/tier.go and MUST NOT change here.
 * Sovereign is the display name for slug `pro` (entitlement `enclii_pro`);
 * Ecosystem is the display name for slug `madfam`.
 *
 * Community/Essentials have identical feature limits (1 project / 3
 * services), so the `essentials` claim displays as Community.
 *
 * Legacy claim values (sovereign/ecosystem) are supported for old JWTs and
 * map to pro/madfam respectively.
 */

// =============================================================================
// TYPES
// =============================================================================

export type FoundryTier = 'community' | 'essentials' | 'pro' | 'madfam' | 'sovereign' | 'ecosystem' | null;

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

export const TIER_CONFIG: Record<string, TierConfig> = {
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
    price: 'Free (self-hosted)',
    cta: {
      label: 'View on GitHub',
      href: 'https://github.com/madfam-org/enclii',
    },
  },
  // Legacy claim with Community-equivalent limits — displays as Community.
  // Slug kept because the API still accepts/enforces it (see tier.go).
  essentials: {
    name: 'Community',
    description: 'Managed hosting with Community plan limits',
    canCreateProject: true,
    canDeploy: true,
    canUseCustomDomains: false,
    canManageTeams: false,
    projectLimit: 1,
    serviceLimit: 3,
    price: 'Free',
    cta: {
      label: 'Start Building',
      href: 'https://app.enclii.dev',
    },
  },
  // Slug `pro` displays as Sovereign (canonical paid self-serve tier)
  pro: {
    name: 'Sovereign',
    description: 'Managed hosting with auto SSL & custom domains',
    canCreateProject: true,
    canDeploy: true,
    canUseCustomDomains: true,
    canManageTeams: false,
    projectLimit: 10,
    serviceLimit: -1,
    price: '$20/mo',
    cta: {
      label: 'Upgrade to Sovereign',
      href: '#checkout',
    },
  },
  // Slug `madfam` displays as Ecosystem (coming soon / waitlist)
  madfam: {
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
  // Legacy compat — old JWTs during transition
  sovereign: {
    name: 'Sovereign',
    description: 'Managed hosting with auto SSL & custom domains',
    canCreateProject: true,
    canDeploy: true,
    canUseCustomDomains: true,
    canManageTeams: false,
    projectLimit: 10,
    serviceLimit: -1,
    price: '$20/mo',
    cta: {
      label: 'Current Plan',
      href: '#',
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
      label: 'Current Plan',
      href: '#',
    },
  },
};

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

/**
 * Normalize legacy tier names to new names
 */
function normalizeTier(tier: FoundryTier): string {
  if (tier === 'sovereign') return 'pro';
  if (tier === 'ecosystem') return 'madfam';
  return tier ?? 'null';
}

/**
 * Get the tier config for a given foundry_tier claim
 */
export function getTierConfig(tier: FoundryTier): TierConfig {
  return TIER_CONFIG[tier ?? 'null'] ?? TIER_CONFIG['null'];
}

/**
 * Check if a tier is a paid tier (pro or madfam)
 */
export function isPaidTier(tier: FoundryTier): boolean {
  const n = normalizeTier(tier);
  return n === 'pro' || n === 'madfam';
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
      return `You've reached your limit of ${config.projectLimit} project${config.projectLimit !== 1 ? 's' : ''}. Upgrade to Sovereign to create more.`;

    case 'deploy':
      if (!config.canDeploy) {
        return 'Sign in to deploy services';
      }
      return `You've reached your limit of ${config.serviceLimit} service${config.serviceLimit !== 1 ? 's' : ''}. Upgrade to Sovereign to deploy more.`;

    case 'custom-domain':
      return 'Custom domains are available on the Sovereign tier and above.';

    case 'team':
      return 'Team management is available on the Ecosystem tier.';

    default:
      return 'Upgrade your plan to access this feature.';
  }
}

/**
 * Get the upgrade URL for upgrading to Sovereign (slug `pro`,
 * entitlement `enclii_pro`).
 *
 * Points at enclii's OWN upgrade page (app.enclii.dev/upgrade), which POSTs to
 * /v1/billing/checkout and redirects to Dhanam's hosted checkout. It must never
 * point at the historical dhanam.madfam.io host, which is NXDOMAIN and serves
 * no checkout route — a tier-blocked user would otherwise dead-end. Override the
 * base with NEXT_PUBLIC_UPGRADE_URL.
 */
export function getCheckoutUrl(userId?: string, returnUrl?: string): string {
  const baseUrl = process.env.NEXT_PUBLIC_UPGRADE_URL || 'https://app.enclii.dev/upgrade';
  const params = new URLSearchParams();
  params.set('plan', 'enclii_pro');
  params.set('product', 'enclii');
  if (userId) {
    params.set('user_id', userId);
  }
  if (returnUrl) {
    params.set('return_url', returnUrl);
  }
  return `${baseUrl}?${params.toString()}`;
}
