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
