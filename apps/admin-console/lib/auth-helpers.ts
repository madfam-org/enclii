/**
 * Pure authentication helper functions.
 * Extracted from middleware.ts for testability.
 *
 * These functions are stateless and accept their configuration
 * as parameters, making them easy to unit test without relying
 * on module-level constants or environment variables.
 */

/**
 * Check if an email belongs to an allowed domain.
 */
export function isAllowedDomain(email: string, allowedDomains: string[]): boolean {
  return allowedDomains.some((domain) => email.toLowerCase().endsWith(domain.toLowerCase()))
}

/**
 * Check if any of the user's roles match the allowed roles.
 */
export function hasAllowedRole(roles: string[], allowedRoles: string[]): boolean {
  return roles.some((role) => allowedRoles.includes(role))
}

/**
 * Check if a pathname is a public path that doesn't require authentication.
 */
export function isPublicPath(pathname: string, publicPaths: string[]): boolean {
  return publicPaths.some(
    (path) => pathname === path || pathname.startsWith(path + '/')
  )
}

/**
 * Read the ADR-003 platform rank from a VERIFIED JWT payload.
 *
 * Returns `true` / `false` when the token states the rank, and `null` when it
 * says nothing about it. The three-way answer is the whole point: the rank
 * lives in the API's database (`users.is_platform_admin`), the console never
 * decides it, and a token minted before the issuer knew about the claim must
 * not be read as a denial.
 *
 * `platform_admin` as a ROLE STRING is deliberately NOT accepted. ADR-003
 * normalises that string down to tenant_admin at the API precisely because an
 * API token's scopes are copied verbatim into the caller's roles, so a rank
 * assertable by string is a rank a tenant administrator can mint for itself.
 * Honouring it here would put the console back on the wrong side of that.
 */
export function platformRankFromClaims(payload: Record<string, unknown>): boolean | null {
  const claim = payload.is_platform_admin ?? payload['enclii_is_platform_admin']
  if (typeof claim === 'boolean') {
    return claim
  }
  if (claim === 'true' || claim === 'false') {
    return claim === 'true'
  }
  return null
}

/**
 * Extract roles from a JWT payload.
 * Supports array, comma-separated string, and enclii_roles field formats.
 */
export function extractRoles(payload: Record<string, unknown>): string[] {
  const rolesRaw = payload.roles || payload.role || payload['enclii_roles']
  if (Array.isArray(rolesRaw)) {
    return rolesRaw.filter((r): r is string => typeof r === 'string')
  }
  if (typeof rolesRaw === 'string') {
    return rolesRaw.split(',').map((r) => r.trim())
  }
  return []
}
