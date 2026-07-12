/**
 * Unit tests for lib/tiers.ts
 *
 * Tests all pure helper functions for the tier/RBAC system.
 * These functions have no React or browser dependencies.
 */

import {
  type FoundryTier,
  TIER_CONFIG,
  getTierConfig,
  isPaidTier,
  hasReachedProjectLimit,
  hasReachedServiceLimit,
  getUpgradeMessage,
  getCheckoutUrl,
} from './tiers';

// ---------------------------------------------------------------------------
// TIER_CONFIG shape validation
// ---------------------------------------------------------------------------

describe('TIER_CONFIG', () => {
  it('defines configs for all expected tier keys', () => {
    const expectedKeys = ['null', 'community', 'essentials', 'pro', 'madfam', 'sovereign', 'ecosystem'];
    for (const key of expectedKeys) {
      expect(TIER_CONFIG[key]).toBeDefined();
      expect(TIER_CONFIG[key].name).toBeTruthy();
    }
  });

  it('every config has required boolean capability fields', () => {
    for (const [, config] of Object.entries(TIER_CONFIG)) {
      expect(typeof config.canCreateProject).toBe('boolean');
      expect(typeof config.canDeploy).toBe('boolean');
      expect(typeof config.canUseCustomDomains).toBe('boolean');
      expect(typeof config.canManageTeams).toBe('boolean');
    }
  });

  it('every config has numeric limits', () => {
    for (const [, config] of Object.entries(TIER_CONFIG)) {
      expect(typeof config.projectLimit).toBe('number');
      expect(typeof config.serviceLimit).toBe('number');
    }
  });

  it('guest (null) tier cannot create projects or deploy', () => {
    const guest = TIER_CONFIG['null'];
    expect(guest.canCreateProject).toBe(false);
    expect(guest.canDeploy).toBe(false);
    expect(guest.projectLimit).toBe(0);
    expect(guest.serviceLimit).toBe(0);
  });

  it('pro tier has unlimited services (-1)', () => {
    expect(TIER_CONFIG.pro.serviceLimit).toBe(-1);
  });

  it('madfam tier has unlimited projects and services', () => {
    expect(TIER_CONFIG.madfam.projectLimit).toBe(-1);
    expect(TIER_CONFIG.madfam.serviceLimit).toBe(-1);
  });
});

// ---------------------------------------------------------------------------
// getTierConfig
// ---------------------------------------------------------------------------

describe('getTierConfig', () => {
  it('returns the correct config for each tier', () => {
    expect(getTierConfig('community').name).toBe('Community');
    // essentials shares Community limits, so it displays as Community
    expect(getTierConfig('essentials').name).toBe('Community');
    expect(getTierConfig('pro').name).toBe('Sovereign');
    expect(getTierConfig('madfam').name).toBe('Ecosystem');
  });

  it('returns guest config for null tier', () => {
    expect(getTierConfig(null).name).toBe('Guest');
  });

  it('returns a config for legacy "sovereign" tier', () => {
    const config = getTierConfig('sovereign');
    expect(config).toBeDefined();
    expect(config.name).toBe('Sovereign');
  });

  it('returns a config for legacy "ecosystem" tier', () => {
    const config = getTierConfig('ecosystem');
    expect(config).toBeDefined();
    expect(config.name).toBe('Ecosystem');
  });

  it('falls back to guest config for unknown tier values', () => {
    // Force an unknown value through the type system
    const config = getTierConfig('nonexistent' as FoundryTier);
    expect(config.name).toBe('Guest');
  });
});

// ---------------------------------------------------------------------------
// isPaidTier
// ---------------------------------------------------------------------------

describe('isPaidTier', () => {
  it('returns false for null (guest)', () => {
    expect(isPaidTier(null)).toBe(false);
  });

  it('returns false for community', () => {
    expect(isPaidTier('community')).toBe(false);
  });

  it('returns false for essentials', () => {
    expect(isPaidTier('essentials')).toBe(false);
  });

  it('returns true for pro', () => {
    expect(isPaidTier('pro')).toBe(true);
  });

  it('returns true for madfam', () => {
    expect(isPaidTier('madfam')).toBe(true);
  });

  it('returns true for legacy sovereign (maps to pro)', () => {
    expect(isPaidTier('sovereign')).toBe(true);
  });

  it('returns true for legacy ecosystem (maps to madfam)', () => {
    expect(isPaidTier('ecosystem')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// hasReachedProjectLimit
// ---------------------------------------------------------------------------

describe('hasReachedProjectLimit', () => {
  it('returns true when count equals the limit', () => {
    // community has projectLimit = 1
    expect(hasReachedProjectLimit('community', 1)).toBe(true);
  });

  it('returns true when count exceeds the limit', () => {
    expect(hasReachedProjectLimit('community', 5)).toBe(true);
  });

  it('returns false when count is below the limit', () => {
    expect(hasReachedProjectLimit('community', 0)).toBe(false);
  });

  it('returns false for unlimited tiers (-1)', () => {
    // madfam has projectLimit = -1
    expect(hasReachedProjectLimit('madfam', 100)).toBe(false);
    expect(hasReachedProjectLimit('madfam', 0)).toBe(false);
  });

  it('returns true at zero for guest tier (limit = 0)', () => {
    expect(hasReachedProjectLimit(null, 0)).toBe(true);
  });

  it('respects pro tier limit of 10', () => {
    expect(hasReachedProjectLimit('pro', 9)).toBe(false);
    expect(hasReachedProjectLimit('pro', 10)).toBe(true);
    expect(hasReachedProjectLimit('pro', 11)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// hasReachedServiceLimit
// ---------------------------------------------------------------------------

describe('hasReachedServiceLimit', () => {
  it('returns true when count equals the limit', () => {
    // community has serviceLimit = 3
    expect(hasReachedServiceLimit('community', 3)).toBe(true);
  });

  it('returns false when count is below the limit', () => {
    expect(hasReachedServiceLimit('community', 2)).toBe(false);
  });

  it('returns false for unlimited tiers (-1)', () => {
    // pro has serviceLimit = -1
    expect(hasReachedServiceLimit('pro', 999)).toBe(false);
  });

  it('returns true at zero for guest tier (limit = 0)', () => {
    expect(hasReachedServiceLimit(null, 0)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// getUpgradeMessage
// ---------------------------------------------------------------------------

describe('getUpgradeMessage', () => {
  it('returns sign-in message for guest attempting project creation', () => {
    const msg = getUpgradeMessage('project', null);
    expect(msg).toBe('Sign in to create projects');
  });

  it('returns sign-in message for guest attempting deploy', () => {
    const msg = getUpgradeMessage('deploy', null);
    expect(msg).toBe('Sign in to deploy services');
  });

  it('returns project limit message for community tier', () => {
    const msg = getUpgradeMessage('project', 'community');
    expect(msg).toContain('limit');
    expect(msg).toContain('1 project');
    expect(msg).toContain('Upgrade to Sovereign');
  });

  it('returns service limit message for community tier deploy', () => {
    const msg = getUpgradeMessage('deploy', 'community');
    expect(msg).toContain('limit');
    expect(msg).toContain('3 services');
  });

  it('returns custom domain message', () => {
    const msg = getUpgradeMessage('custom-domain', 'community');
    expect(msg).toContain('Custom domains');
    expect(msg).toContain('Sovereign');
  });

  it('returns team management message', () => {
    const msg = getUpgradeMessage('team', 'community');
    expect(msg).toContain('Team management');
    expect(msg).toContain('Ecosystem');
  });

  it('returns generic upgrade message for unknown action', () => {
    // Force an unknown action
    const msg = getUpgradeMessage('unknown-action' as never, 'community');
    expect(msg).toContain('Upgrade');
  });

  it('uses correct pluralization for single vs multiple limits', () => {
    // community: projectLimit = 1 (singular)
    const singleMsg = getUpgradeMessage('project', 'community');
    expect(singleMsg).toContain('1 project');
    expect(singleMsg).not.toContain('projects');

    // pro: projectLimit = 10 (plural)
    const pluralMsg = getUpgradeMessage('project', 'pro');
    expect(pluralMsg).toContain('10 projects');
  });
});

// ---------------------------------------------------------------------------
// getCheckoutUrl
// ---------------------------------------------------------------------------

describe('getCheckoutUrl', () => {
  it('returns a URL with the correct base and plan parameter', () => {
    const url = getCheckoutUrl();
    expect(url).toContain('plan=enclii_pro');
    expect(url).toContain('product=enclii');
  });

  it('includes user_id when provided', () => {
    const url = getCheckoutUrl('user-123');
    expect(url).toContain('user_id=user-123');
  });

  it('excludes user_id when not provided', () => {
    const url = getCheckoutUrl();
    expect(url).not.toContain('user_id');
  });

  it('includes return_url when provided', () => {
    const url = getCheckoutUrl('user-123', 'https://app.enclii.dev/dashboard');
    expect(url).toContain('return_url=');
    expect(url).toContain('app.enclii.dev');
  });

  it('excludes return_url when not provided', () => {
    const url = getCheckoutUrl('user-123');
    expect(url).not.toContain('return_url');
  });

  it('uses the enclii upgrade page (not the dead dhanam host) when NEXT_PUBLIC_UPGRADE_URL is not set', () => {
    const url = getCheckoutUrl();
    expect(url).toContain('app.enclii.dev/upgrade');
    expect(url).not.toContain('dhanam.madfam.io');
  });

  it('encodes special characters in return_url', () => {
    const url = getCheckoutUrl(undefined, 'https://app.enclii.dev/settings?tab=billing&ref=upgrade');
    // URLSearchParams encodes & and = in values
    expect(url).toContain('return_url=');
    // The URL should be properly encoded
    const parsed = new URL(url);
    const returnUrl = parsed.searchParams.get('return_url');
    expect(returnUrl).toBe('https://app.enclii.dev/settings?tab=billing&ref=upgrade');
  });
});
