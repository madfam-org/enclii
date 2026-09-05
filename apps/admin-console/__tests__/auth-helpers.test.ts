import {
  isAllowedDomain,
  hasAllowedRole,
  isPublicPath,
  extractRoles,
  platformRankFromClaims,
} from '@/lib/auth-helpers'

// =============================================================================
// isAllowedDomain
// =============================================================================

describe('isAllowedDomain', () => {
  const allowedDomains = ['@madfam.io', '@example.org']

  it('returns true for an exact domain match', () => {
    expect(isAllowedDomain('admin@madfam.io', allowedDomains)).toBe(true)
  })

  it('returns true when email matches the second allowed domain', () => {
    expect(isAllowedDomain('user@example.org', allowedDomains)).toBe(true)
  })

  it('is case insensitive for the email', () => {
    expect(isAllowedDomain('Admin@MADFAM.IO', allowedDomains)).toBe(true)
  })

  it('is case insensitive for the allowed domains list', () => {
    const upperDomains = ['@MADFAM.IO']
    expect(isAllowedDomain('user@madfam.io', upperDomains)).toBe(true)
  })

  it('returns false when email domain does not match any allowed domain', () => {
    expect(isAllowedDomain('attacker@evil.com', allowedDomains)).toBe(false)
  })

  it('returns false for an empty email string', () => {
    expect(isAllowedDomain('', allowedDomains)).toBe(false)
  })

  it('returns false when the allowed domains list is empty', () => {
    expect(isAllowedDomain('admin@madfam.io', [])).toBe(false)
  })

  it('handles subdomain-like emails that partially match', () => {
    // "notmadfam.io" should NOT match "@madfam.io" because "@" is part of the suffix
    expect(isAllowedDomain('user@notmadfam.io', allowedDomains)).toBe(false)
  })

  it('handles email with subdomain after @', () => {
    // "sub.madfam.io" ends with "madfam.io" but "@sub.madfam.io" does NOT end with "@madfam.io"
    expect(isAllowedDomain('user@sub.madfam.io', allowedDomains)).toBe(false)
  })
})

// =============================================================================
// hasAllowedRole
// =============================================================================

describe('hasAllowedRole', () => {
  const allowedRoles = ['superadmin', 'admin', 'operator']

  it('returns true when user has one matching role', () => {
    expect(hasAllowedRole(['admin'], allowedRoles)).toBe(true)
  })

  it('returns true when user has multiple roles and one matches', () => {
    expect(hasAllowedRole(['viewer', 'operator'], allowedRoles)).toBe(true)
  })

  it('returns true for superadmin role', () => {
    expect(hasAllowedRole(['superadmin'], allowedRoles)).toBe(true)
  })

  it('returns false when no roles match', () => {
    expect(hasAllowedRole(['viewer', 'guest'], allowedRoles)).toBe(false)
  })

  it('returns false for an empty user roles array', () => {
    expect(hasAllowedRole([], allowedRoles)).toBe(false)
  })

  it('returns false when the allowed roles list is empty', () => {
    expect(hasAllowedRole(['admin'], [])).toBe(false)
  })

  it('returns false when both arrays are empty', () => {
    expect(hasAllowedRole([], [])).toBe(false)
  })
})

// =============================================================================
// isPublicPath
// =============================================================================

describe('isPublicPath', () => {
  const publicPaths = [
    '/login',
    '/auth/callback',
    '/api/auth',
    '/api/health',
    '/_next',
    '/favicon.ico',
    '/public',
    '/access-denied',
  ]

  it('returns true for an exact path match', () => {
    expect(isPublicPath('/login', publicPaths)).toBe(true)
  })

  it('returns true for a nested path under a public prefix', () => {
    expect(isPublicPath('/api/auth/session', publicPaths)).toBe(true)
  })

  it('returns true for /_next/static/chunk.js', () => {
    expect(isPublicPath('/_next/static/chunk.js', publicPaths)).toBe(true)
  })

  it('returns true for /access-denied', () => {
    expect(isPublicPath('/access-denied', publicPaths)).toBe(true)
  })

  it('returns false for a protected path', () => {
    expect(isPublicPath('/dashboard', publicPaths)).toBe(false)
  })

  it('returns false for /api/admin (not in public list)', () => {
    expect(isPublicPath('/api/admin', publicPaths)).toBe(false)
  })

  it('returns false for a path that merely contains a public path substring', () => {
    // "/loginpage" should NOT match "/login" because it does not have "/" after it
    expect(isPublicPath('/loginpage', publicPaths)).toBe(false)
  })

  it('returns false when public paths list is empty', () => {
    expect(isPublicPath('/login', [])).toBe(false)
  })
})

// =============================================================================
// extractRoles
// =============================================================================

describe('extractRoles', () => {
  it('extracts roles from an array in the "roles" field', () => {
    const payload = { roles: ['admin', 'operator'] }
    expect(extractRoles(payload)).toEqual(['admin', 'operator'])
  })

  it('extracts roles from the "role" field (singular)', () => {
    const payload = { role: ['superadmin'] }
    expect(extractRoles(payload)).toEqual(['superadmin'])
  })

  it('extracts roles from the "enclii_roles" field', () => {
    const payload = { enclii_roles: ['operator'] }
    expect(extractRoles(payload)).toEqual(['operator'])
  })

  it('handles comma-separated string format', () => {
    const payload = { roles: 'admin,operator' }
    expect(extractRoles(payload)).toEqual(['admin', 'operator'])
  })

  it('trims whitespace from comma-separated roles', () => {
    const payload = { roles: '  admin , operator , viewer  ' }
    expect(extractRoles(payload)).toEqual(['admin', 'operator', 'viewer'])
  })

  it('handles a single role as a string', () => {
    const payload = { role: 'admin' }
    expect(extractRoles(payload)).toEqual(['admin'])
  })

  it('filters out non-string values from an array', () => {
    const payload = { roles: ['admin', 42, null, 'operator', undefined] }
    expect(extractRoles(payload)).toEqual(['admin', 'operator'])
  })

  it('returns an empty array when no role fields are present', () => {
    const payload = { email: 'admin@example.org', sub: '123' }
    expect(extractRoles(payload)).toEqual([])
  })

  it('returns an empty array for an empty payload', () => {
    expect(extractRoles({})).toEqual([])
  })

  it('prefers "roles" over "role" when both are present', () => {
    const payload = { roles: ['admin'], role: ['viewer'] }
    // The implementation uses || so "roles" is truthy and wins
    expect(extractRoles(payload)).toEqual(['admin'])
  })
})

// =============================================================================
// platformRankFromClaims (ADR-003)
// =============================================================================

describe('platformRankFromClaims (ADR-003)', () => {
  it('reads a boolean claim', () => {
    expect(platformRankFromClaims({ is_platform_admin: true })).toBe(true)
    expect(platformRankFromClaims({ is_platform_admin: false })).toBe(false)
  })

  it('reads the namespaced claim', () => {
    expect(platformRankFromClaims({ enclii_is_platform_admin: true })).toBe(true)
  })

  it('accepts the string form some issuers emit', () => {
    expect(platformRankFromClaims({ is_platform_admin: 'true' })).toBe(true)
    expect(platformRankFromClaims({ is_platform_admin: 'false' })).toBe(false)
  })

  // The three-way answer is the point: a token minted before the issuer knew
  // about the claim must not read as a denial, or this deploy locks every
  // operator out of the console on the day it ships.
  it('returns null when the token says nothing about the rank', () => {
    expect(platformRankFromClaims({})).toBeNull()
    expect(platformRankFromClaims({ roles: ['admin'] })).toBeNull()
  })

  // ADR-003 normalises the string down to tenant_admin at the API because an
  // API token's scopes become the caller's roles. Honouring it here would let
  // a tenant administrator assert the very rank the console is checking for.
  it('never accepts platform_admin as a role string', () => {
    expect(platformRankFromClaims({ roles: ['platform_admin'] })).toBeNull()
    expect(platformRankFromClaims({ role: 'platform_admin' })).toBeNull()
  })
})
